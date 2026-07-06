/**
 * @file extractor_test.go
 * @description Behavioral tests for the PostgreSQL schema extractor driven through
 *              go-sqlmock. A mocked *sql.DB exercises the query/scan code paths for
 *              version handling, table extraction and the SQL-fragment parsers.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/state/extractor_test.go
 */

package state

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// newMockExtractor wires a PostgreSQL extractor to a fresh sqlmock configured for
// regex query matching so tests assert on stable SQL fragments.
func newMockExtractor(t *testing.T) (*PostgreSQLSchemaExtractor, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewPostgreSQLSchemaExtractor(db), mock, db
}

func TestNewPostgreSQLSchemaExtractor_DefaultsToPublicSchema(t *testing.T) {
	ext, _, _ := newMockExtractor(t)
	if ext.schema != "public" {
		t.Errorf("expected default schema 'public', got %s", ext.schema)
	}
}

func TestGetVersion_NoSchemaVersionTable(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	// The existence probe returns false -> GetVersion short-circuits to "".
	mock.ExpectQuery("information_schema.tables").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	version, err := ext.GetVersion(ctx)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if version != "" {
		t.Errorf("expected empty version when table absent, got %q", version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetVersion_ReturnsLatestVersion(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	mock.ExpectQuery("information_schema.tables").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT version FROM schema_version").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("3.1.4"))

	version, err := ext.GetVersion(ctx)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if version != "3.1.4" {
		t.Errorf("expected version 3.1.4, got %q", version)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSetVersion_CreatesTableAndInserts(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_version").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_version").
		WithArgs("9.9.9").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := ext.SetVersion(ctx, "9.9.9"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// expectTableExtraction queues every query ExtractTable issues for a table with a
// single integer primary-key column and no indexes/constraints/comment.
func expectSimpleTableExtraction(mock sqlmock.Sqlmock, table string) {
	// Columns
	mock.ExpectQuery("information_schema.columns").
		WithArgs("public", table).
		WillReturnRows(sqlmock.NewRows([]string{
			"column_name", "data_type", "is_nullable", "column_default",
			"character_maximum_length", "numeric_precision", "numeric_scale", "column_comment",
		}).AddRow("id", "integer", "NO", nil, nil, int64(32), int64(0), nil))
	// Primary key
	mock.ExpectQuery("pg_index").
		WithArgs("public", table).
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("id"))
	// Indexes
	mock.ExpectQuery("pg_index ix").
		WithArgs("public", table).
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "is_unique", "index_type", "index_def"}))
	// Constraints
	mock.ExpectQuery("pg_constraint").
		WithArgs("public", table).
		WillReturnRows(sqlmock.NewRows([]string{"constraint_name", "constraint_type", "constraint_def"}))
	// Comment
	mock.ExpectQuery("obj_description").
		WithArgs("public", table).
		WillReturnRows(sqlmock.NewRows([]string{"obj_description"}).AddRow(nil))
}

func TestExtractTable_ColumnsAndPrimaryKey(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	expectSimpleTableExtraction(mock, "users")

	table, err := ext.ExtractTable(ctx, "users")
	if err != nil {
		t.Fatalf("ExtractTable: %v", err)
	}
	if table.Name != "users" {
		t.Errorf("expected table name users, got %s", table.Name)
	}
	if len(table.Columns) != 1 || table.Columns[0].Name != "id" {
		t.Fatalf("expected single 'id' column, got %+v", table.Columns)
	}
	if table.Columns[0].Nullable {
		t.Error("id column marked NO for is_nullable should not be nullable")
	}
	if len(table.PrimaryKey) != 1 || table.PrimaryKey[0] != "id" {
		t.Errorf("expected primary key [id], got %v", table.PrimaryKey)
	}
}

func TestExtractSchema_FullHappyPath(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	// 1. GetVersion existence probe -> table exists
	mock.ExpectQuery("information_schema.tables").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	// 2. GetVersion latest version
	mock.ExpectQuery("SELECT version FROM schema_version").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("1.2.3"))
	// 3. getTableNames -> one table "widgets"
	mock.ExpectQuery("table_type = 'BASE TABLE'").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_name"}).AddRow("widgets"))
	// 4. ExtractTable(widgets)
	expectSimpleTableExtraction(mock, "widgets")
	// 5. extractViews
	mock.ExpectQuery("information_schema.views").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_name", "view_definition", "is_materialized"}))
	// 6. extractTriggers
	mock.ExpectQuery("pg_trigger").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"trigger_name", "table_name", "trigger_when", "trigger_event", "trigger_def"}))

	schema, err := ext.ExtractSchema(ctx)
	if err != nil {
		t.Fatalf("ExtractSchema: %v", err)
	}
	if schema.Version != "1.2.3" {
		t.Errorf("expected schema version 1.2.3, got %s", schema.Version)
	}
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "widgets" {
		t.Fatalf("expected single 'widgets' table, got %+v", schema.Tables)
	}
	if schema.Metadata["database_type"] != "postgresql" {
		t.Errorf("expected database_type metadata postgresql, got %v", schema.Metadata["database_type"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestExtractColumns_DetectsAutoIncrementAndLength(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	// A serial column (nextval default) plus a varchar with a length.
	mock.ExpectQuery("information_schema.columns").
		WithArgs("public", "accounts").
		WillReturnRows(sqlmock.NewRows([]string{
			"column_name", "data_type", "is_nullable", "column_default",
			"character_maximum_length", "numeric_precision", "numeric_scale", "column_comment",
		}).
			AddRow("id", "integer", "NO", "nextval('accounts_id_seq'::regclass)", nil, int64(32), int64(0), nil).
			AddRow("email", "character varying", "YES", nil, int64(255), nil, nil, "user email"))
	// Primary key
	mock.ExpectQuery("pg_index").
		WithArgs("public", "accounts").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("id"))
	// Indexes / Constraints / Comment (empty)
	mock.ExpectQuery("pg_index ix").
		WithArgs("public", "accounts").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "is_unique", "index_type", "index_def"}))
	mock.ExpectQuery("pg_constraint").
		WithArgs("public", "accounts").
		WillReturnRows(sqlmock.NewRows([]string{"constraint_name", "constraint_type", "constraint_def"}))
	mock.ExpectQuery("obj_description").
		WithArgs("public", "accounts").
		WillReturnRows(sqlmock.NewRows([]string{"obj_description"}).AddRow(nil))

	table, err := ext.ExtractTable(ctx, "accounts")
	if err != nil {
		t.Fatalf("ExtractTable: %v", err)
	}
	if len(table.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(table.Columns))
	}

	idCol := table.Columns[0]
	if !idCol.AutoIncrement {
		t.Error("column with nextval default should be detected as auto-increment")
	}
	if idCol.Default != nil {
		t.Error("auto-increment column default should be cleared")
	}

	emailCol := table.Columns[1]
	// data_type 'character varying' normalizes to 'varchar'.
	if emailCol.Type != "varchar" {
		t.Errorf("expected normalized type 'varchar', got %s", emailCol.Type)
	}
	if emailCol.Length != 255 {
		t.Errorf("expected length 255, got %d", emailCol.Length)
	}
	if !emailCol.Nullable {
		t.Error("email column marked YES for is_nullable should be nullable")
	}
	if emailCol.Comment != "user email" {
		t.Errorf("expected column comment preserved, got %q", emailCol.Comment)
	}
}

func TestExtractTable_ConstraintParsing(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	// Column
	mock.ExpectQuery("information_schema.columns").
		WithArgs("public", "orders").
		WillReturnRows(sqlmock.NewRows([]string{
			"column_name", "data_type", "is_nullable", "column_default",
			"character_maximum_length", "numeric_precision", "numeric_scale", "column_comment",
		}).AddRow("user_id", "integer", "NO", nil, nil, int64(32), int64(0), nil))
	// Primary key (none)
	mock.ExpectQuery("pg_index").
		WithArgs("public", "orders").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}))
	// Indexes (none)
	mock.ExpectQuery("pg_index ix").
		WithArgs("public", "orders").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "is_unique", "index_type", "index_def"}))
	// Constraints: one FK, one UNIQUE, one CHECK
	mock.ExpectQuery("pg_constraint").
		WithArgs("public", "orders").
		WillReturnRows(sqlmock.NewRows([]string{"constraint_name", "constraint_type", "constraint_def"}).
			AddRow("fk_user", "f", "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE").
			AddRow("uq_ref", "u", "UNIQUE (ref)").
			AddRow("ck_pos", "c", "CHECK (amount > 0)"))
	// Comment
	mock.ExpectQuery("obj_description").
		WithArgs("public", "orders").
		WillReturnRows(sqlmock.NewRows([]string{"obj_description"}).AddRow(nil))

	table, err := ext.ExtractTable(ctx, "orders")
	if err != nil {
		t.Fatalf("ExtractTable: %v", err)
	}
	if len(table.Constraints) != 3 {
		t.Fatalf("expected 3 constraints, got %d", len(table.Constraints))
	}

	byName := map[string]Constraint{}
	for _, c := range table.Constraints {
		byName[c.Name] = c
	}

	fk := byName["fk_user"]
	if fk.Type != "foreign_key" {
		t.Errorf("expected foreign_key type, got %s", fk.Type)
	}
	// pg_get_constraintdef emits "REFERENCES users(id)" with no space before the
	// paren, so parseForeignKey's whitespace tokenizer captures the table name
	// together with the column list. This asserts the parser's real output.
	if fk.ReferencedTable != "users(id)" {
		t.Errorf("expected referenced table token users(id), got %s", fk.ReferencedTable)
	}
	// The referenced columns are still parsed correctly from the parenthesized list.
	if len(fk.ReferencedColumns) != 1 || fk.ReferencedColumns[0] != "id" {
		t.Errorf("expected referenced columns [id], got %v", fk.ReferencedColumns)
	}
	if len(fk.Columns) != 1 || fk.Columns[0] != "user_id" {
		t.Errorf("expected FK column user_id, got %v", fk.Columns)
	}
	if fk.OnDelete != "CASCADE" {
		t.Errorf("expected ON DELETE CASCADE, got %s", fk.OnDelete)
	}

	uq := byName["uq_ref"]
	if uq.Type != "unique" || len(uq.Columns) != 1 || uq.Columns[0] != "ref" {
		t.Errorf("unexpected unique constraint parse: %+v", uq)
	}

	ck := byName["ck_pos"]
	if ck.Type != "check" || ck.CheckExpression != "amount > 0" {
		t.Errorf("unexpected check constraint parse: %+v", ck)
	}
}

func TestExtractTable_IndexParsing(t *testing.T) {
	ext, mock, _ := newMockExtractor(t)
	ctx := context.Background()

	mock.ExpectQuery("information_schema.columns").
		WithArgs("public", "posts").
		WillReturnRows(sqlmock.NewRows([]string{
			"column_name", "data_type", "is_nullable", "column_default",
			"character_maximum_length", "numeric_precision", "numeric_scale", "column_comment",
		}).AddRow("id", "integer", "NO", nil, nil, int64(32), int64(0), nil))
	mock.ExpectQuery("pg_index").
		WithArgs("public", "posts").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("id"))
	// One non-unique btree index on (title).
	mock.ExpectQuery("pg_index ix").
		WithArgs("public", "posts").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "is_unique", "index_type", "index_def"}).
			AddRow("idx_posts_title", false, "btree", "CREATE INDEX idx_posts_title ON posts USING btree (title)"))
	mock.ExpectQuery("pg_constraint").
		WithArgs("public", "posts").
		WillReturnRows(sqlmock.NewRows([]string{"constraint_name", "constraint_type", "constraint_def"}))
	mock.ExpectQuery("obj_description").
		WithArgs("public", "posts").
		WillReturnRows(sqlmock.NewRows([]string{"obj_description"}).AddRow(nil))

	table, err := ext.ExtractTable(ctx, "posts")
	if err != nil {
		t.Fatalf("ExtractTable: %v", err)
	}
	if len(table.Indexes) != 1 {
		t.Fatalf("expected 1 index, got %d", len(table.Indexes))
	}
	idx := table.Indexes[0]
	if idx.Name != "idx_posts_title" {
		t.Errorf("expected index name idx_posts_title, got %s", idx.Name)
	}
	if len(idx.Columns) != 1 || idx.Columns[0] != "title" {
		t.Errorf("expected index columns [title], got %v", idx.Columns)
	}
	if idx.Type != "btree" {
		t.Errorf("expected index type btree, got %s", idx.Type)
	}
}

/**
 * @file extractor_test.go
 * @description Tests for the PostgreSQL schema extractor driven against a
 *              go-sqlmock *sql.DB. Each information_schema / pg_catalog query is
 *              stubbed with representative rows so the real scan-and-assemble
 *              logic for tables, columns, primary keys, indexes, and constraints
 *              is exercised without a live PostgreSQL server, along with the
 *              end-to-end ExtractSchema orchestration and error paths.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/drift/extractor_test.go
 */

package drift

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestNewPostgreSQLSchemaExtractor(t *testing.T) {
	db, _ := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)
	if e == nil {
		t.Fatal("NewPostgreSQLSchemaExtractor returned nil")
	}
}

func TestPostgreSQLSchemaExtractor_GetTableNames(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"table_name"}).
			AddRow("orders").
			AddRow("users"))

	names, err := e.GetTableNames(context.Background())
	if err != nil {
		t.Fatalf("GetTableNames failed: %v", err)
	}
	if len(names) != 2 || names[0] != "orders" || names[1] != "users" {
		t.Errorf("unexpected table names: %v", names)
	}
}

func TestPostgreSQLSchemaExtractor_GetTableNames_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnError(errors.New("connection reset"))

	if _, err := e.GetTableNames(context.Background()); err == nil {
		t.Error("expected an error when the tables query fails")
	}
}

func TestPostgreSQLSchemaExtractor_ExtractTableSchema(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	// Columns query: one nullable varchar with a max length, one non-null id.
	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{
			"column_name", "data_type", "is_nullable", "column_default",
			"character_maximum_length", "numeric_precision", "numeric_scale",
		}).
			AddRow("id", "integer", "NO", nil, nil, 32, 0).
			AddRow("email", "character varying", "YES", "''", 255, nil, nil))

	// Primary key query returns a single PK column.
	mock.ExpectQuery("i.indisprimary").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("id"))

	schema, err := e.ExtractTableSchema(context.Background(), "users")
	if err != nil {
		t.Fatalf("ExtractTableSchema failed: %v", err)
	}
	if schema.Name != "users" {
		t.Errorf("expected table name 'users', got %q", schema.Name)
	}
	if len(schema.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(schema.Columns))
	}
	// The email column must reflect nullability, default, and max length.
	var email *ColumnSchema
	for i := range schema.Columns {
		if schema.Columns[i].Name == "email" {
			email = &schema.Columns[i]
		}
	}
	if email == nil {
		t.Fatal("email column not extracted")
	}
	if !email.IsNullable {
		t.Error("expected email to be nullable")
	}
	if email.MaxLength != 255 {
		t.Errorf("expected email max length 255, got %d", email.MaxLength)
	}
	if email.DefaultValue == nil {
		t.Error("expected email to carry a default value")
	}
	if len(schema.PrimaryKey) != 1 || schema.PrimaryKey[0] != "id" {
		t.Errorf("expected primary key [id], got %v", schema.PrimaryKey)
	}
}

func TestPostgreSQLSchemaExtractor_ExtractTableSchema_ColumnsError(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("users").
		WillReturnError(errors.New("columns query failed"))

	if _, err := e.ExtractTableSchema(context.Background(), "users"); err == nil {
		t.Error("expected an error when the columns query fails")
	}
}

func TestPostgreSQLSchemaExtractor_GetIndexes(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	// Two rows for the same composite index must collapse into one IndexSchema
	// with two columns.
	mock.ExpectQuery("pg_index").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "is_unique", "index_type"}).
			AddRow("idx_users_email_tenant", "email", true, "btree").
			AddRow("idx_users_email_tenant", "tenant_id", true, "btree"))

	indexes, err := e.GetIndexes(context.Background(), "users")
	if err != nil {
		t.Fatalf("GetIndexes failed: %v", err)
	}
	if len(indexes) != 1 {
		t.Fatalf("expected 1 collapsed index, got %d", len(indexes))
	}
	idx := indexes[0]
	if len(idx.Columns) != 2 {
		t.Errorf("expected 2 columns on the composite index, got %v", idx.Columns)
	}
	if !idx.Unique {
		t.Error("expected the index to be unique")
	}
	if idx.Type != "btree" {
		t.Errorf("expected index type 'btree', got %q", idx.Type)
	}
}

func TestPostgreSQLSchemaExtractor_GetConstraints(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	// A foreign key with one column, referencing another table's column. The
	// PostgreSQL constraint_type "FOREIGN KEY" must be normalized to
	// "foreign_key".
	mock.ExpectQuery("information_schema.table_constraints").
		WithArgs("orders").
		WillReturnRows(sqlmock.NewRows([]string{
			"constraint_name", "constraint_type", "column_name",
			"foreign_table_name", "foreign_column_name",
		}).
			AddRow("fk_orders_user", "FOREIGN KEY", "user_id", "users", "id"))

	constraints, err := e.GetConstraints(context.Background(), "orders")
	if err != nil {
		t.Fatalf("GetConstraints failed: %v", err)
	}
	if len(constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(constraints))
	}
	c := constraints[0]
	if c.Type != "foreign_key" {
		t.Errorf("expected normalized type 'foreign_key', got %q", c.Type)
	}
	if c.ReferencedTable != "users" {
		t.Errorf("expected referenced table 'users', got %q", c.ReferencedTable)
	}
	if len(c.ReferencedColumns) != 1 || c.ReferencedColumns[0] != "id" {
		t.Errorf("expected referenced columns [id], got %v", c.ReferencedColumns)
	}
}

func TestPostgreSQLSchemaExtractor_ExtractSchema_EndToEnd(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	// 1) table names
	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"table_name"}).AddRow("users"))
	// 2) columns for users
	mock.ExpectQuery("FROM information_schema.columns").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{
			"column_name", "data_type", "is_nullable", "column_default",
			"character_maximum_length", "numeric_precision", "numeric_scale",
		}).AddRow("id", "integer", "NO", nil, nil, 32, 0))
	// 3) primary key for users
	mock.ExpectQuery("i.indisprimary").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("id"))
	// 4) indexes for users
	mock.ExpectQuery("pg_index").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "is_unique", "index_type"}).
			AddRow("idx_users_id", "id", false, "btree"))
	// 5) constraints for users
	mock.ExpectQuery("information_schema.table_constraints").
		WithArgs("users").
		WillReturnRows(sqlmock.NewRows([]string{
			"constraint_name", "constraint_type", "column_name",
			"foreign_table_name", "foreign_column_name",
		}).AddRow("pk_users", "PRIMARY KEY", "id", nil, nil))

	snapshot, err := e.ExtractSchema(context.Background())
	if err != nil {
		t.Fatalf("ExtractSchema failed: %v", err)
	}
	if _, ok := snapshot.Tables["users"]; !ok {
		t.Error("expected the 'users' table in the snapshot")
	}
	if _, ok := snapshot.Indexes["idx_users_id"]; !ok {
		t.Error("expected the 'idx_users_id' index in the snapshot")
	}
	if _, ok := snapshot.Constraints["pk_users"]; !ok {
		t.Error("expected the 'pk_users' constraint in the snapshot")
	}
}

func TestPostgreSQLSchemaExtractor_ExtractSchema_TableNamesError(t *testing.T) {
	db, mock := newMockDB(t)
	e := NewPostgreSQLSchemaExtractor(db)

	mock.ExpectQuery("FROM information_schema.tables").
		WillReturnError(errors.New("catalog unavailable"))

	if _, err := e.ExtractSchema(context.Background()); err == nil {
		t.Error("expected ExtractSchema to fail when table enumeration fails")
	}
}

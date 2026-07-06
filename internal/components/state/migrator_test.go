/**
 * @file migrator_test.go
 * @description Behavioral tests for the state-based migrator orchestration
 *              (Plan/Preview/Apply/Sync) driven through go-sqlmock plus on-disk
 *              declarative schema files.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/state/migrator_test.go
 */

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// newMockMigrator builds a state migrator backed by a mocked *sql.DB.
func newMockMigrator(t *testing.T, cfg *StateBasedConfig) (*DefaultStateBasedMigrator, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewDefaultStateBasedMigrator(db, cfg), mock
}

// writeSchemaFile writes a declarative YAML schema and returns its path.
func writeSchemaFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return path
}

// expectEmptyDatabaseExtraction queues the queries ExtractSchema issues against a
// database that has no schema_version table and no tables.
func expectEmptyDatabaseExtraction(mock sqlmock.Sqlmock) {
	// GetVersion existence probe -> false
	mock.ExpectQuery("information_schema.tables").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	// getTableNames -> none
	mock.ExpectQuery("table_type = 'BASE TABLE'").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_name"}))
	// extractViews -> none
	mock.ExpectQuery("information_schema.views").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_name", "view_definition", "is_materialized"}))
	// extractTriggers -> none
	mock.ExpectQuery("pg_trigger").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"trigger_name", "table_name", "trigger_when", "trigger_event", "trigger_def"}))
}

const validSchemaYAML = `
version: "1.0.0"
name: "app"
tables:
  - name: users
    columns:
      - name: id
        type: integer
        nullable: false
      - name: email
        type: varchar
        length: 255
    primary_key:
      - id
`

func TestNewDefaultStateBasedMigrator_AppliesDefaultConfig(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	if m.config == nil {
		t.Fatal("expected a default config to be applied")
	}
	if !m.config.Enabled {
		t.Error("default config should be enabled")
	}
	if m.parser == nil || m.extractor == nil || m.differ == nil || m.generator == nil || m.validator == nil {
		t.Error("migrator left a component unwired")
	}
}

func TestPlan_GeneratesMigrationForNewTable(t *testing.T) {
	m, mock := newMockMigrator(t, &StateBasedConfig{AllowDestructive: false})
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, validSchemaYAML)

	expectEmptyDatabaseExtraction(mock)

	mig, err := m.Plan(ctx, schemaPath)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if mig == nil {
		t.Fatal("expected a generated migration for a brand-new table")
	}
	if mig.SourceSchema != schemaPath {
		t.Errorf("expected source schema recorded, got %s", mig.SourceSchema)
	}
	if mig.UpSQL == "" || mig.DownSQL == "" {
		t.Error("generated migration missing UP or DOWN SQL")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestPlan_NoChangesReturnsNil(t *testing.T) {
	// Desired schema is empty (no tables), and the database is empty too, so
	// there are no changes and Plan returns (nil, nil).
	m, mock := newMockMigrator(t, nil)
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, "version: \"1.0.0\"\nname: \"empty\"\ntables: []\n")

	expectEmptyDatabaseExtraction(mock)

	mig, err := m.Plan(ctx, schemaPath)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if mig != nil {
		t.Errorf("expected nil migration when no changes, got %+v", mig)
	}
}

func TestPlan_InvalidSchemaFileFails(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	ctx := context.Background()
	if _, err := m.Plan(ctx, "does-not-exist.yaml"); err == nil {
		t.Fatal("expected error for a missing schema file")
	}
}

func TestPreview_ReturnsDiffWithChanges(t *testing.T) {
	m, mock := newMockMigrator(t, nil)
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, validSchemaYAML)

	expectEmptyDatabaseExtraction(mock)

	diff, err := m.Preview(ctx, schemaPath)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !diff.HasChanges {
		t.Error("expected changes for a new table against an empty database")
	}
	if len(diff.TablesAdded) != 1 || diff.TablesAdded[0].Name != "users" {
		t.Errorf("expected users table added, got %+v", diff.TablesAdded)
	}
}

func TestApply_NilMigrationRejected(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	if err := m.Apply(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil migration")
	}
}

func TestApply_DestructiveBlockedWhenNotAllowed(t *testing.T) {
	m, _ := newMockMigrator(t, &StateBasedConfig{AllowDestructive: false})
	mig := &GeneratedMigration{
		UpSQL:           "DROP TABLE users;",
		SafeToAutoApply: false,
	}
	if err := m.Apply(context.Background(), mig); err == nil {
		t.Fatal("expected destructive migration to be blocked")
	}
}

func TestApply_ExecutesSafeMigrationInTransaction(t *testing.T) {
	m, mock := newMockMigrator(t, &StateBasedConfig{AllowDestructive: false})
	ctx := context.Background()

	mig := &GeneratedMigration{
		Version:         "1.0.0",
		Description:     "add users",
		UpSQL:           "CREATE TABLE users (id INTEGER);",
		SafeToAutoApply: true,
	}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE users").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_version").
		WithArgs("1.0.0", "add users").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := m.Apply(ctx, mig); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSync_DryRunReportsPendingWithoutApplying(t *testing.T) {
	m, mock := newMockMigrator(t, &StateBasedConfig{AllowDestructive: false})
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, validSchemaYAML)

	// Sync -> Plan -> ExtractSchema (empty DB). Dry run stops after planning.
	expectEmptyDatabaseExtraction(mock)

	status, err := m.Sync(ctx, schemaPath, true)
	if err != nil {
		t.Fatalf("Sync dry-run: %v", err)
	}
	if status.PendingCount != 1 {
		t.Errorf("expected 1 pending migration, got %d", status.PendingCount)
	}
	if status.ExecutedCount != 0 {
		t.Errorf("dry-run should execute nothing, got %d executed", status.ExecutedCount)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSync_NoChangesReturnsEmptyStatus(t *testing.T) {
	m, mock := newMockMigrator(t, nil)
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, "version: \"1.0.0\"\nname: \"empty\"\ntables: []\n")

	expectEmptyDatabaseExtraction(mock)

	status, err := m.Sync(ctx, schemaPath, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if status.TotalMigrations != 0 || status.PendingCount != 0 {
		t.Errorf("expected empty status when in sync, got %+v", status)
	}
}

func TestSync_RequireReviewBlocksApply(t *testing.T) {
	// AutoApply=false + RequireReview=true forces a manual-review error even for
	// a safe change.
	m, mock := newMockMigrator(t, &StateBasedConfig{
		AutoApply:     false,
		RequireReview: true,
	})
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, validSchemaYAML)

	expectEmptyDatabaseExtraction(mock)

	_, err := m.Sync(ctx, schemaPath, false)
	if err == nil {
		t.Fatal("expected Sync to require manual review")
	}
}

func TestValidateSchemaFile_ReportsBestPracticeWarnings(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	ctx := context.Background()
	// A table with no primary key / id / timestamps triggers best-practice warnings.
	schemaPath := writeSchemaFile(t, `
version: "1.0.0"
name: "app"
tables:
  - name: logs
    columns:
      - name: message
        type: text
`)

	result, err := m.ValidateSchemaFile(ctx, schemaPath)
	if err != nil {
		t.Fatalf("ValidateSchemaFile: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected best-practice warnings for a bare table")
	}
}

func TestCheckSync_TrueWhenNoDiff(t *testing.T) {
	m, mock := newMockMigrator(t, nil)
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, "version: \"1.0.0\"\nname: \"empty\"\ntables: []\n")

	expectEmptyDatabaseExtraction(mock)

	inSync, err := m.CheckSync(ctx, schemaPath)
	if err != nil {
		t.Fatalf("CheckSync: %v", err)
	}
	if !inSync {
		t.Error("expected database to be reported in sync when there is no diff")
	}
}

func TestFormatDiff_NoChangesMessage(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	if out := m.FormatDiff(nil); out != "No changes detected" {
		t.Errorf("expected no-changes message, got %q", out)
	}
	if out := m.FormatDiff(&SchemaDiff{HasChanges: false}); out != "No changes detected" {
		t.Errorf("expected no-changes message for empty diff, got %q", out)
	}
}

func TestFormatDiff_SummarizesChanges(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	diff := &SchemaDiff{
		HasChanges: true,
		TablesAdded: []Table{
			{Name: "users", Columns: []Column{{Name: "id", Type: "integer"}}},
		},
		TablesDropped: []Table{
			{Name: "legacy"},
		},
		TablesModified: []TableDiff{
			{
				TableName:      "orders",
				ColumnsAdded:   []Column{{Name: "note", Type: "text"}},
				ColumnsDropped: []Column{{Name: "old", Type: "text"}},
				ColumnsModified: []ColumnDiff{
					{ColumnName: "total", Changes: []string{"type changed"}},
				},
			},
		},
		Summary: DiffSummary{TotalChanges: 5, DestructiveChanges: 2, SafeToApply: false},
	}
	out := m.FormatDiff(diff)
	for _, want := range []string{"Tables Added", "Tables Dropped", "Tables Modified", "DATA LOSS", "destructive"} {
		if !contains(out, want) {
			t.Errorf("FormatDiff output missing %q:\n%s", want, out)
		}
	}
}

// contains is a tiny substring helper local to this test file.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

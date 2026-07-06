/**
 * @file state_extras_test.go
 * @description Supplementary behavioral tests covering differ helpers, naming
 *              conventions, parser validation, and the migrator's file-producing
 *              workflows (Import/Export/GenerateMigrationFile).
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/state/state_extras_test.go
 */

package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDiff_DestructiveTypeChangeMarksModification(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	// text -> integer is a non-widening, destructive type change.
	actual := &DatabaseSchema{
		Tables: []Table{
			{Name: "t", Columns: []Column{{Name: "c", Type: "text"}}},
		},
	}
	desired := &DatabaseSchema{
		Tables: []Table{
			{Name: "t", Columns: []Column{{Name: "c", Type: "integer"}}},
		},
	}

	diff, err := differ.Diff(ctx, desired, actual)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	destructive := differ.GetDestructiveChanges(ctx, diff)
	found := false
	for _, d := range destructive {
		if strings.Contains(d, "truncation") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a data-truncation destructive change, got %v", destructive)
	}
}

func TestDiff_WideningTypeChangeIsNotDestructive(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	// smallint -> integer is a safe widening conversion.
	actual := &DatabaseSchema{
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "c", Type: "smallint"}}}},
	}
	desired := &DatabaseSchema{
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "c", Type: "integer"}}}},
	}

	diff, err := differ.Diff(ctx, desired, actual)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// The column is modified but the change is not flagged as truncation.
	for _, d := range differ.GetDestructiveChanges(ctx, diff) {
		if strings.Contains(d, "truncation") {
			t.Errorf("widening conversion should not be destructive: %s", d)
		}
	}
}

func TestDiff_IndexAndConstraintKeysDistinguishChanges(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	// Same table, but a global index and constraint are added on the desired side.
	actual := &DatabaseSchema{
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "id", Type: "integer"}}}},
	}
	desired := &DatabaseSchema{
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "id", Type: "integer"}}}},
		Indexes: []Index{
			{Name: "idx_t_id", Table: "t", Columns: []string{"id"}},
		},
		Constraints: []Constraint{
			{Name: "uq_t_id", Table: "t", Type: "unique", Columns: []string{"id"}},
		},
	}

	diff, err := differ.Diff(ctx, desired, actual)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff.IndexesAdded) != 1 {
		t.Errorf("expected 1 index added, got %d", len(diff.IndexesAdded))
	}
	if len(diff.ConstraintsAdded) != 1 {
		t.Errorf("expected 1 constraint added, got %d", len(diff.ConstraintsAdded))
	}
}

func TestDiff_DefaultValueChangeDetected(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	actual := &DatabaseSchema{
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "c", Type: "integer", Default: 0}}}},
	}
	desired := &DatabaseSchema{
		Tables: []Table{{Name: "t", Columns: []Column{{Name: "c", Type: "integer", Default: 5}}}},
	}

	diff, err := differ.Diff(ctx, desired, actual)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff.TablesModified) != 1 {
		t.Fatalf("expected 1 modified table, got %d", len(diff.TablesModified))
	}
	mods := diff.TablesModified[0].ColumnsModified
	if len(mods) != 1 {
		t.Fatalf("expected 1 modified column, got %d", len(mods))
	}
	joined := strings.Join(mods[0].Changes, " ")
	if !strings.Contains(joined, "default changed") {
		t.Errorf("expected default-change description, got %v", mods[0].Changes)
	}
}

func TestCheckNamingConventions_FlagsBadIdentifiers(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Name: "app",
		Tables: []Table{
			{
				Name: "Users", // not snake_case -> table naming warning
				Columns: []Column{
					{Name: "ID", Type: "integer"}, // not snake_case -> column warning
				},
				Indexes: []Index{
					{Name: "bad_index_name", Columns: []string{"ID"}}, // fails idx_ pattern
				},
				Constraints: []Constraint{
					{Name: "weird", Type: "unique", Columns: []string{"ID"}}, // fails constraint pattern
				},
			},
		},
	}

	warnings := validator.CheckNamingConventions(ctx, schema)
	if len(warnings) < 3 {
		t.Errorf("expected multiple naming-convention warnings, got %d: %+v", len(warnings), warnings)
	}
}

func TestValidateSchema_InvalidGlobalIndexReference(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Name: "app",
		Tables: []Table{
			{Name: "users", Columns: []Column{{Name: "id", Type: "integer"}}, PrimaryKey: []string{"id"}},
		},
		Indexes: []Index{
			// References a table that does not exist.
			{Name: "idx_ghost", Table: "ghost", Columns: []string{"x"}},
		},
	}

	result, err := validator.ValidateSchema(ctx, schema)
	if err != nil {
		t.Fatalf("ValidateSchema: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid schema for an index referencing a missing table")
	}
}

func TestValidateSchema_ForeignKeyToMissingTable(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Name: "app",
		Tables: []Table{
			{Name: "orders", Columns: []Column{{Name: "id", Type: "integer"}}, PrimaryKey: []string{"id"}},
		},
		Constraints: []Constraint{
			{
				Name: "fk_missing", Table: "orders", Type: "FOREIGN_KEY",
				Columns: []string{"id"}, ReferencedTable: "nope", ReferencedColumns: []string{"id"},
			},
		},
	}

	result, err := validator.ValidateSchema(ctx, schema)
	if err != nil {
		t.Fatalf("ValidateSchema: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid schema for a foreign key to a missing table")
	}
}

func TestParser_ValidateWithoutValidatorSkips(t *testing.T) {
	// A parser without a validator reports valid with a skip warning.
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	result, err := parser.Validate(ctx, &DatabaseSchema{Name: "x"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Error("expected valid result when no validator is configured")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a skip warning when no validator is configured")
	}
}

func TestParser_ValidateDelegatesToValidator(t *testing.T) {
	parser := NewDefaultSchemaParser(NewDefaultSchemaValidator(nil))
	ctx := context.Background()

	// nil schema -> validator reports invalid.
	result, err := parser.Validate(ctx, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for a nil schema via the configured validator")
	}
}

func TestParser_DetectFormatJSON(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	body := `{"version":"1.0.0","name":"app","tables":[{"name":"t","columns":[{"name":"id","type":"integer"}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write json schema: %v", err)
	}

	schema, err := parser.Parse(ctx, path)
	if err != nil {
		t.Fatalf("Parse json: %v", err)
	}
	if schema.Name != "app" || len(schema.Tables) != 1 {
		t.Errorf("unexpected parsed json schema: %+v", schema)
	}
}

func TestGenerateMigrationFile_WritesToOutputDir(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "generated")
	m, mock := newMockMigrator(t, &StateBasedConfig{
		AutoGenerate:       true,
		MigrationOutputDir: outDir,
	})
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, validSchemaYAML)

	expectEmptyDatabaseExtraction(mock)

	path, err := m.GenerateMigrationFile(ctx, schemaPath)
	if err != nil {
		t.Fatalf("GenerateMigrationFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected generated migration file to exist: %v", err)
	}
}

func TestImport_GeneratesFileWithoutApplying(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "gen")
	m, mock := newMockMigrator(t, &StateBasedConfig{
		AutoGenerate:       true,
		MigrationOutputDir: outDir,
	})
	ctx := context.Background()
	schemaPath := writeSchemaFile(t, validSchemaYAML)

	// Import -> Plan -> ExtractSchema. autoApply=false so no exec expectations.
	expectEmptyDatabaseExtraction(mock)

	mig, err := m.Import(ctx, schemaPath, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if mig == nil {
		t.Fatal("expected a migration from Import")
	}
	// The generated file path is recorded on the migration's SourceSchema.
	if !strings.Contains(mig.SourceSchema, outDir) {
		t.Errorf("expected generated file path under %s, got %s", outDir, mig.SourceSchema)
	}
}

func TestExport_SerializesExtractedSchema(t *testing.T) {
	m, mock := newMockMigrator(t, nil)
	ctx := context.Background()

	// Export -> ExtractSchema (empty DB) then GetVersion again for the version.
	expectEmptyDatabaseExtraction(mock)
	// Export calls GetVersion a second time after extraction.
	mock.ExpectQuery("information_schema.tables").
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	outPath := filepath.Join(t.TempDir(), "exported.yaml")
	if err := m.Export(ctx, outPath, FormatYAML); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("expected exported schema file to exist: %v", err)
	}
}

func TestExport_EmptyPathRejected(t *testing.T) {
	m, _ := newMockMigrator(t, nil)
	if err := m.Export(context.Background(), "", FormatYAML); err == nil {
		t.Fatal("expected error for an empty output path")
	}
}

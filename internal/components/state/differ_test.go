/**
 * @file differ_test.go
 * @description Unit tests for schema differ
 * @author Claude Code
 * @date 2025-01-01
 */

package state

import (
	"context"
	"testing"
)

func TestNewDefaultSchemaDiffer(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	if differ == nil {
		t.Fatal("NewDefaultSchemaDiffer returned nil")
	}

	if differ.config == nil {
		t.Error("Config should not be nil")
	}
}

func TestDiff_NoChanges(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Version: "1.0.0",
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer", Nullable: false},
				},
			},
		},
	}

	diff, err := differ.Diff(ctx, schema, schema)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if diff.HasChanges {
		t.Error("Expected no changes for identical schemas")
	}

	if diff.Summary.TotalChanges != 0 {
		t.Errorf("Expected 0 total changes, got %d", diff.Summary.TotalChanges)
	}
}

func TestDiff_TableAdded(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	baseline := &DatabaseSchema{
		Version: "1.0.0",
		Tables:  []Table{},
	}

	desired := &DatabaseSchema{
		Version: "2.0.0",
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer"},
				},
			},
		},
	}

	diff, err := differ.Diff(ctx, desired, baseline)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !diff.HasChanges {
		t.Error("Expected changes detected")
	}

	if len(diff.TablesAdded) != 1 {
		t.Errorf("Expected 1 table added, got %d", len(diff.TablesAdded))
	}

	if diff.TablesAdded[0].Name != "users" {
		t.Errorf("Expected users table, got %s", diff.TablesAdded[0].Name)
	}
}

func TestDiff_TableDropped(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	baseline := &DatabaseSchema{
		Version: "1.0.0",
		Tables: []Table{
			{
				Name: "old_table",
				Columns: []Column{
					{Name: "id", Type: "integer"},
				},
			},
		},
	}

	desired := &DatabaseSchema{
		Version: "2.0.0",
		Tables:  []Table{},
	}

	diff, err := differ.Diff(ctx, desired, baseline)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !diff.HasChanges {
		t.Error("Expected changes detected")
	}

	if len(diff.TablesDropped) != 1 {
		t.Errorf("Expected 1 table dropped, got %d", len(diff.TablesDropped))
	}

	if diff.Summary.DestructiveChanges != 1 {
		t.Errorf("Expected 1 destructive change, got %d", diff.Summary.DestructiveChanges)
	}
}

func TestDiff_ColumnAdded(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	baseline := &DatabaseSchema{
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer"},
				},
			},
		},
	}

	desired := &DatabaseSchema{
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer"},
					{Name: "email", Type: "varchar", Length: 255},
				},
			},
		},
	}

	diff, err := differ.Diff(ctx, desired, baseline)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !diff.HasChanges {
		t.Error("Expected changes detected")
	}

	if len(diff.TablesModified) != 1 {
		t.Fatalf("Expected 1 table modified, got %d", len(diff.TablesModified))
	}

	tableDiff := diff.TablesModified[0]
	if len(tableDiff.ColumnsAdded) != 1 {
		t.Errorf("Expected 1 column added, got %d", len(tableDiff.ColumnsAdded))
	}

	if tableDiff.ColumnsAdded[0].Name != "email" {
		t.Errorf("Expected email column, got %s", tableDiff.ColumnsAdded[0].Name)
	}
}

func TestDiff_ColumnDropped(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	baseline := &DatabaseSchema{
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer"},
					{Name: "old_field", Type: "varchar"},
				},
			},
		},
	}

	desired := &DatabaseSchema{
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer"},
				},
			},
		},
	}

	diff, err := differ.Diff(ctx, desired, baseline)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !diff.HasChanges {
		t.Error("Expected changes detected")
	}

	tableDiff := diff.TablesModified[0]
	if len(tableDiff.ColumnsDropped) != 1 {
		t.Errorf("Expected 1 column dropped, got %d", len(tableDiff.ColumnsDropped))
	}

	if diff.Summary.DestructiveChanges != 1 {
		t.Errorf("Expected 1 destructive change, got %d", diff.Summary.DestructiveChanges)
	}
}

func TestDiff_ColumnModified(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	baseline := &DatabaseSchema{
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "email", Type: "varchar", Length: 100, Nullable: true},
				},
			},
		},
	}

	desired := &DatabaseSchema{
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "email", Type: "varchar", Length: 255, Nullable: false},
				},
			},
		},
	}

	diff, err := differ.Diff(ctx, desired, baseline)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}

	if !diff.HasChanges {
		t.Error("Expected changes detected")
	}

	tableDiff := diff.TablesModified[0]
	if len(tableDiff.ColumnsModified) != 1 {
		t.Errorf("Expected 1 column modified, got %d", len(tableDiff.ColumnsModified))
	}

	colDiff := tableDiff.ColumnsModified[0]
	if len(colDiff.Changes) == 0 {
		t.Error("Expected change descriptions")
	}
}

func TestCalculateSafety_Safe(t *testing.T) {
	differ := NewDefaultSchemaDiffer(&StateBasedConfig{
		AllowDestructive: false,
	})
	ctx := context.Background()

	diff := &SchemaDiff{
		TablesAdded: []Table{
			{Name: "new_table"},
		},
		Summary: DiffSummary{
			DestructiveChanges: 0,
		},
	}

	safe := differ.CalculateSafety(ctx, diff)
	if !safe {
		t.Error("Expected safe for additive changes")
	}
}

func TestCalculateSafety_Unsafe_TableDropped(t *testing.T) {
	differ := NewDefaultSchemaDiffer(&StateBasedConfig{
		AllowDestructive: false,
	})
	ctx := context.Background()

	diff := &SchemaDiff{
		TablesDropped: []Table{
			{Name: "old_table"},
		},
		Summary: DiffSummary{
			DestructiveChanges: 1,
		},
	}

	safe := differ.CalculateSafety(ctx, diff)
	if safe {
		t.Error("Expected unsafe for table drops")
	}
}

func TestGetDestructiveChanges(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	diff := &SchemaDiff{
		TablesDropped: []Table{
			{Name: "users"},
		},
		TablesModified: []TableDiff{
			{
				TableName: "posts",
				ColumnsDropped: []Column{
					{Name: "content"},
				},
			},
		},
	}

	destructive := differ.GetDestructiveChanges(ctx, diff)

	if len(destructive) < 2 {
		t.Errorf("Expected at least 2 destructive changes, got %d", len(destructive))
	}

	hasTableDrop := false
	hasColumnDrop := false

	for _, change := range destructive {
		if containsSubstring(change, "DROP TABLE") {
			hasTableDrop = true
		}
		if containsSubstring(change, "DROP COLUMN") {
			hasColumnDrop = true
		}
	}

	if !hasTableDrop {
		t.Error("Expected table drop in destructive changes")
	}

	if !hasColumnDrop {
		t.Error("Expected column drop in destructive changes")
	}
}

func TestDiffTables_PrimaryKeyChanged(t *testing.T) {
	differ := NewDefaultSchemaDiffer(nil)
	ctx := context.Background()

	baseline := &Table{
		Name:       "users",
		Columns:    []Column{{Name: "id", Type: "integer"}},
		PrimaryKey: []string{"id"},
	}

	desired := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "integer"},
			{Name: "email", Type: "varchar"},
		},
		PrimaryKey: []string{"id", "email"},
	}

	tableDiff, err := differ.DiffTables(ctx, desired, baseline)
	if err != nil {
		t.Fatalf("DiffTables failed: %v", err)
	}

	if !tableDiff.PrimaryKeyChanged {
		t.Error("Expected primary key change to be detected")
	}
}

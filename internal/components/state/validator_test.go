/**
 * @file validator_test.go
 * @description Unit tests for schema validator
 * @author Claude Code
 * @date 2025-01-01
 */

package state

import (
	"context"
	"testing"
)

func TestNewDefaultSchemaValidator(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	if validator == nil {
		t.Fatal("NewDefaultSchemaValidator returned nil")
	}

	if validator.namingConventions == nil {
		t.Error("Naming conventions should not be nil")
	}
}

func TestValidateSchema_Valid(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Name:    "test_db",
		Version: "1.0.0",
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer", Nullable: false},
					{Name: "email", Type: "varchar", Length: 255},
				},
				PrimaryKey: []string{"id"},
			},
		},
	}

	result, err := validator.ValidateSchema(ctx, schema)
	if err != nil {
		t.Fatalf("ValidateSchema failed: %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected valid schema, got errors: %v", result.Errors)
	}
}

func TestValidateSchema_NilSchema(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	result, err := validator.ValidateSchema(ctx, nil)
	if err != nil {
		t.Fatalf("ValidateSchema failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for nil schema")
	}

	if len(result.Errors) == 0 {
		t.Error("Expected errors for nil schema")
	}
}

func TestValidateSchema_DuplicateTableNames(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Name:    "test_db",
		Version: "1.0.0",
		Tables: []Table{
			{
				Name:       "users",
				Columns:    []Column{{Name: "id", Type: "integer"}},
				PrimaryKey: []string{"id"},
			},
			{
				Name:       "users", // Duplicate
				Columns:    []Column{{Name: "id", Type: "integer"}},
				PrimaryKey: []string{"id"},
			},
		},
	}

	result, err := validator.ValidateSchema(ctx, schema)
	if err != nil {
		t.Fatalf("ValidateSchema failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for duplicate table names")
	}
}

func TestValidateTable_Valid(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "integer", Nullable: false},
			{Name: "email", Type: "varchar", Length: 255},
		},
		PrimaryKey: []string{"id"},
	}

	result, err := validator.ValidateTable(ctx, table)
	if err != nil {
		t.Fatalf("ValidateTable failed: %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected valid table, got errors: %v", result.Errors)
	}
}

func TestValidateTable_NoColumns(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	table := &Table{
		Name:    "users",
		Columns: []Column{},
	}

	result, err := validator.ValidateTable(ctx, table)
	if err != nil {
		t.Fatalf("ValidateTable failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for table with no columns")
	}
}

func TestValidateTable_DuplicateColumnNames(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "integer"},
			{Name: "id", Type: "integer"}, // Duplicate
		},
	}

	result, err := validator.ValidateTable(ctx, table)
	if err != nil {
		t.Fatalf("ValidateTable failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for duplicate column names")
	}
}

func TestValidateTable_PrimaryKeyNonExistentColumn(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	table := &Table{
		Name: "users",
		Columns: []Column{
			{Name: "id", Type: "integer"},
		},
		PrimaryKey: []string{"nonexistent"},
	}

	result, err := validator.ValidateTable(ctx, table)
	if err != nil {
		t.Fatalf("ValidateTable failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for PK referencing non-existent column")
	}
}

func TestValidateColumn_Valid(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	column := &Column{
		Name: "email",
		Type: "varchar",
		Length: 255,
	}

	result, err := validator.ValidateColumn(ctx, column)
	if err != nil {
		t.Fatalf("ValidateColumn failed: %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected valid column, got errors: %v", result.Errors)
	}
}

func TestValidateColumn_NoName(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	column := &Column{
		Name: "",
		Type: "integer",
	}

	result, err := validator.ValidateColumn(ctx, column)
	if err != nil {
		t.Fatalf("ValidateColumn failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for column with no name")
	}
}

func TestValidateColumn_NoType(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	column := &Column{
		Name: "id",
		Type: "",
	}

	result, err := validator.ValidateColumn(ctx, column)
	if err != nil {
		t.Fatalf("ValidateColumn failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for column with no type")
	}
}

func TestValidateColumn_AutoIncrementNonInteger(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	column := &Column{
		Name:          "id",
		Type:          "varchar",
		AutoIncrement: true,
	}

	result, err := validator.ValidateColumn(ctx, column)
	if err != nil {
		t.Fatalf("ValidateColumn failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for auto-increment on non-integer type")
	}
}

func TestValidateConstraint_Valid(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	constraint := &Constraint{
		Name:    "fk_user_id",
		Type:    "foreign_key",
		Columns: []string{"user_id"},
		ReferencedTable: "users",
		ReferencedColumns: []string{"id"},
	}

	result, err := validator.ValidateConstraint(ctx, constraint)
	if err != nil {
		t.Fatalf("ValidateConstraint failed: %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected valid constraint, got errors: %v", result.Errors)
	}
}

func TestValidateConstraint_NoName(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	constraint := &Constraint{
		Name:    "",
		Type:    "unique",
		Columns: []string{"email"},
	}

	result, err := validator.ValidateConstraint(ctx, constraint)
	if err != nil {
		t.Fatalf("ValidateConstraint failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for constraint with no name")
	}
}

func TestValidateConstraint_InvalidType(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	constraint := &Constraint{
		Name:    "test",
		Type:    "invalid_type",
		Columns: []string{"id"},
	}

	result, err := validator.ValidateConstraint(ctx, constraint)
	if err != nil {
		t.Fatalf("ValidateConstraint failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for invalid constraint type")
	}
}

func TestValidateConstraint_ForeignKeyNoReferencedTable(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	constraint := &Constraint{
		Name:    "fk_test",
		Type:    "foreign_key",
		Columns: []string{"user_id"},
		ReferencedTable: "",
		ReferencedColumns: []string{"id"},
	}

	result, err := validator.ValidateConstraint(ctx, constraint)
	if err != nil {
		t.Fatalf("ValidateConstraint failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for FK without referenced table")
	}
}

func TestValidateConstraint_CheckNoExpression(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	constraint := &Constraint{
		Name:            "ck_test",
		Type:            "check",
		Columns:         []string{},
		CheckExpression: "",
	}

	result, err := validator.ValidateConstraint(ctx, constraint)
	if err != nil {
		t.Fatalf("ValidateConstraint failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected invalid result for CHECK constraint without expression")
	}
}

func TestCheckBestPractices(t *testing.T) {
	validator := NewDefaultSchemaValidator(nil)
	ctx := context.Background()

	schema := &DatabaseSchema{
		Name:    "test",
		Version: "1.0.0",
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "username", Type: "varchar"},
				},
				// No primary key, no id, no timestamps
			},
		},
	}

	warnings := validator.CheckBestPractices(ctx, schema)

	if len(warnings) == 0 {
		t.Error("Expected warnings for table without best practices")
	}

	hasPKWarning := false
	hasIDWarning := false
	hasCreatedAtWarning := false

	for _, warning := range warnings {
		if containsString(warning.Message, "primary key") {
			hasPKWarning = true
		}
		if containsString(warning.Message, "id") {
			hasIDWarning = true
		}
		if containsString(warning.Message, "created_at") {
			hasCreatedAtWarning = true
		}
	}

	if !hasPKWarning {
		t.Error("Expected warning about missing primary key")
	}

	if !hasIDWarning {
		t.Error("Expected warning about missing id column")
	}

	if !hasCreatedAtWarning {
		t.Error("Expected warning about missing created_at column")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr ||
		 s[len(s)-len(substr):] == substr ||
		 containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

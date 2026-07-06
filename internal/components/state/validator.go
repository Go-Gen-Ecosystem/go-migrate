/**
 * @file validator.go
 * @description Schema validation implementation
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/validator.go
 */

package state

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// createValidationError creates a ValidationError struct
func createValidationError(migration, errorType, message, code string) ValidationError {
	return ValidationError{
		Migration: migration,
		Type:      errorType,
		Message:   message,
		Severity:  "error",
		Code:      code,
	}
}

// createValidationWarning creates a ValidationWarning with consistent fields
func createValidationWarning(migration, warningType, message, code string) ValidationWarning {
	return ValidationWarning{
		Migration: migration,
		Type:      warningType,
		Message:   message,
		Code:      code,
	}
}

// DefaultSchemaValidator implements SchemaValidator interface
type DefaultSchemaValidator struct {
	namingConventions *NamingConventions
}

// NamingConventions defines naming rules
type NamingConventions struct {
	TableNamePattern      string
	ColumnNamePattern     string
	IndexNamePattern      string
	ConstraintNamePattern string
	EnforceSnakeCase      bool
	MaxIdentifierLength   int
}

// NewDefaultSchemaValidator creates a new schema validator
func NewDefaultSchemaValidator(conventions *NamingConventions) *DefaultSchemaValidator {
	if conventions == nil {
		conventions = &NamingConventions{
			TableNamePattern:      "^[a-z][a-z0-9_]*$",
			ColumnNamePattern:     "^[a-z][a-z0-9_]*$",
			IndexNamePattern:      "^idx_[a-z0-9_]+$",
			ConstraintNamePattern: "^(pk|fk|uq|ck)_[a-z0-9_]+$",
			EnforceSnakeCase:      true,
			MaxIdentifierLength:   63, // PostgreSQL limit
		}
	}

	return &DefaultSchemaValidator{
		namingConventions: conventions,
	}
}

// ValidateSchema validates entire schema definition
func (v *DefaultSchemaValidator) ValidateSchema(ctx context.Context, schema *DatabaseSchema) (*SchemaValidationResult, error) {
	result := &SchemaValidationResult{
		Valid:    true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	if schema == nil {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			"", "nil_schema", "schema cannot be nil", "VAL004"))
		return result, nil
	}

	// Validate basic schema properties
	if schema.Name == "" {
		result.Warnings = append(result.Warnings, createValidationWarning(
			"", "empty_schema_name", "schema name is empty", "VAL005"))
	}

	if schema.Version == "" {
		result.Warnings = append(result.Warnings, createValidationWarning(
			schema.Name, "empty_schema_version", "schema version is empty", "VAL006"))
	}

	// Check for duplicate table names
	tableNames := make(map[string]bool)
	for _, table := range schema.Tables {
		if tableNames[table.Name] {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Migration: schema.Name,
				Type:      "duplicate_table",
				Message:   fmt.Sprintf("duplicate table name: %s", table.Name),
				Severity:  "error",
				Code:      "DUPLICATE_TABLE",
			})
		}
		tableNames[table.Name] = true

		// Validate each table
		tableResult, err := v.ValidateTable(ctx, &table)
		if err != nil {
			return nil, fmt.Errorf("failed to validate table %s: %w", table.Name, err)
		}

		if !tableResult.Valid {
			result.Valid = false
			result.Errors = append(result.Errors, tableResult.Errors...)
		}
		result.Warnings = append(result.Warnings, tableResult.Warnings...)
	}

	// Validate global indexes
	for _, idx := range schema.Indexes {
		if idx.Table == "" {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Migration: schema.Name,
				Type:      "missing_table_reference",
				Message:   fmt.Sprintf("index %s has no table reference", idx.Name),
				Severity:  "error",
				Code:      "MISSING_TABLE_REF",
			})
		}

		if !tableNames[idx.Table] {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				schema.Name, "invalid_index_table_reference",
				fmt.Sprintf("index %s references non-existent table: %s", idx.Name, idx.Table),
				"INVALID_INDEX_TABLE_REF"))
		}
	}

	// Validate global constraints
	for _, con := range schema.Constraints {
		if con.Table == "" {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				schema.Name, "missing_constraint_table_reference",
				fmt.Sprintf("constraint %s has no table reference", con.Name),
				"MISSING_CONSTRAINT_TABLE_REF"))
		}

		if !tableNames[con.Table] {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				schema.Name, "invalid_constraint_table_reference",
				fmt.Sprintf("constraint %s references non-existent table: %s", con.Name, con.Table),
				"INVALID_CONSTRAINT_TABLE_REF"))
		}
	}

	// Validate foreign key references
	for _, con := range schema.Constraints {
		if strings.ToUpper(con.Type) == "FOREIGN_KEY" {
			if !tableNames[con.ReferencedTable] {
				result.Valid = false
				result.Errors = append(result.Errors, createValidationError(
					schema.Name, "invalid_foreign_key_reference",
					fmt.Sprintf("foreign key %s references non-existent table: %s", con.Name, con.ReferencedTable),
					"INVALID_FK_REF"))
			}
		}
	}

	return result, nil
}

// ValidateTable validates table definition
func (v *DefaultSchemaValidator) ValidateTable(ctx context.Context, table *Table) (*SchemaValidationResult, error) {
	result := &SchemaValidationResult{
		Valid:    true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	if table == nil {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			"", "nil_table", "table cannot be nil", "VAL001"))
		return result, nil
	}

	// Validate table name
	if table.Name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			"", "empty_table_name", "table name cannot be empty", "VAL002"))
		return result, nil
	}

	if err := v.validateIdentifier(table.Name, "table", v.namingConventions.TableNamePattern); err != nil {
		result.Warnings = append(result.Warnings, createValidationWarning(
			table.Name, "naming_convention", err.Error(), "VAL003"))
	}

	// Check if table has columns
	if len(table.Columns) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			table.Name, "no_columns",
			fmt.Sprintf("table %s has no columns", table.Name),
			"NO_COLUMNS"))
		return result, nil
	}

	// Validate columns
	columnNames := make(map[string]bool)
	hasAutoIncrement := false

	for _, col := range table.Columns {
		// Check for duplicate column names
		if columnNames[col.Name] {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				table.Name, "duplicate_column",
				fmt.Sprintf("table %s has duplicate column: %s", table.Name, col.Name),
				"DUPLICATE_COLUMN"))
		}
		columnNames[col.Name] = true

		// Validate column
		colResult, err := v.ValidateColumn(ctx, &col)
		if err != nil {
			return nil, fmt.Errorf("failed to validate column %s: %w", col.Name, err)
		}

		if !colResult.Valid {
			result.Valid = false
			for _, e := range colResult.Errors {
				result.Errors = append(result.Errors, createValidationError(
					table.Name, "column_validation_error",
					fmt.Sprintf("table %s: %s", table.Name, e.Message),
					"COLUMN_ERROR"))
			}
		}

		for _, w := range colResult.Warnings {
			result.Warnings = append(result.Warnings, createValidationWarning(
				table.Name, "column_validation_warning",
				fmt.Sprintf("table %s: %s", table.Name, w.Message),
				"COLUMN_WARNING"))
		}

		// Check auto-increment
		if col.AutoIncrement {
			if hasAutoIncrement {
				result.Valid = false
				result.Errors = append(result.Errors, createValidationError(
					table.Name, "multiple_auto_increment",
					fmt.Sprintf("table %s has multiple auto-increment columns", table.Name),
					"MULTIPLE_AUTO_INC"))
			}
			hasAutoIncrement = true
		}
	}

	// Validate primary key
	if len(table.PrimaryKey) > 0 {
		for _, pkCol := range table.PrimaryKey {
			if !columnNames[pkCol] {
				result.Valid = false
				result.Errors = append(result.Errors, createValidationError(
					table.Name, "invalid_primary_key_column",
					fmt.Sprintf("table %s primary key references non-existent column: %s", table.Name, pkCol),
					"INVALID_PK_COLUMN"))
			}
		}
	} else {
		result.Warnings = append(result.Warnings, createValidationWarning(
			table.Name, "no_primary_key",
			fmt.Sprintf("table %s has no primary key", table.Name),
			"NO_PRIMARY_KEY"))
	}

	// Validate indexes
	for _, idx := range table.Indexes {
		if idx.Table == "" {
			idx.Table = table.Name
		}

		for _, col := range idx.Columns {
			if !columnNames[col] {
				result.Valid = false
				result.Errors = append(result.Errors, createValidationError(
					table.Name, "invalid_index_column",
					fmt.Sprintf("table %s index %s references non-existent column: %s", table.Name, idx.Name, col),
					"INVALID_INDEX_COLUMN"))
			}
		}
	}

	// Validate constraints
	for _, con := range table.Constraints {
		conResult, err := v.ValidateConstraint(ctx, &con)
		if err != nil {
			return nil, fmt.Errorf("failed to validate constraint %s: %w", con.Name, err)
		}

		if !conResult.Valid {
			result.Valid = false
			for _, e := range conResult.Errors {
				result.Errors = append(result.Errors, createValidationError(
					table.Name, "constraint_validation_error",
					fmt.Sprintf("table %s: %s", table.Name, e.Message),
					"CONSTRAINT_ERROR"))
			}
		}
		for _, w := range conResult.Warnings {
			result.Warnings = append(result.Warnings, createValidationWarning(
				table.Name, "constraint_validation_warning",
				fmt.Sprintf("table %s: %s", table.Name, w.Message),
				"CONSTRAINT_WARNING"))
		}

		// Validate constraint columns exist
		for _, col := range con.Columns {
			if !columnNames[col] {
				result.Valid = false
				result.Errors = append(result.Errors, createValidationError(
					table.Name, "invalid_constraint_column",
					fmt.Sprintf("table %s constraint %s references non-existent column: %s", table.Name, con.Name, col),
					"INVALID_CONSTRAINT_COLUMN"))
			}
		}
	}

	return result, nil
}

// ValidateColumn validates column definition
func (v *DefaultSchemaValidator) ValidateColumn(ctx context.Context, column *Column) (*SchemaValidationResult, error) {
	result := &SchemaValidationResult{
		Valid:    true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	if column == nil {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			"", "nil_column", "column cannot be nil", "NIL_COLUMN"))
		return result, nil
	}

	// Validate column name
	if column.Name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			column.Name, "empty_column_name", "column name cannot be empty", "EMPTY_NAME"))
		return result, nil
	}

	if err := v.validateIdentifier(column.Name, "column", v.namingConventions.ColumnNamePattern); err != nil {
		result.Warnings = append(result.Warnings, createValidationWarning(
			column.Name, "naming_convention", err.Error(), "NAMING_CONVENTION"))
	}

	// Validate column type
	if column.Type == "" {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			column.Name, "no_column_type",
			fmt.Sprintf("column %s has no type", column.Name),
			"NO_TYPE"))
		return result, nil
	}

	if !v.isValidColumnType(column.Type) {
		result.Warnings = append(result.Warnings, createValidationWarning(
			column.Name, "unusual_type",
			fmt.Sprintf("column %s has unusual type: %s", column.Name, column.Type),
			"UNUSUAL_TYPE"))
	}

	// Validate auto-increment
	if column.AutoIncrement {
		if !v.isIntegerType(column.Type) {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				column.Name, "invalid_auto_increment",
				fmt.Sprintf("column %s: auto-increment only valid for integer types", column.Name),
				"INVALID_AUTO_INC"))
		}

		if column.Nullable {
			result.Warnings = append(result.Warnings, createValidationWarning(
				column.Name, "auto_increment_nullable",
				fmt.Sprintf("column %s: auto-increment columns should not be nullable", column.Name),
				"AUTO_INC_NULLABLE"))
		}
	}

	// Validate length for string types
	if v.isStringType(column.Type) && column.Length == 0 {
		if !strings.EqualFold(column.Type, "text") {
			result.Warnings = append(result.Warnings, createValidationWarning(
				column.Name, "string_without_length",
				fmt.Sprintf("column %s: string type without length specified", column.Name),
				"STRING_NO_LENGTH"))
		}
	}

	// Validate precision/scale for numeric types
	if v.isDecimalType(column.Type) {
		if column.Precision == 0 {
			result.Warnings = append(result.Warnings, createValidationWarning(
				column.Name, "decimal_without_precision",
				fmt.Sprintf("column %s: decimal type without precision specified", column.Name),
				"DECIMAL_NO_PRECISION"))
		}
	}

	// Validate default value consistency
	if column.Default != nil && !column.Nullable {
		// Having a default on a NOT NULL column is good practice
	}

	return result, nil
}

// ValidateConstraint validates constraint definition
func (v *DefaultSchemaValidator) ValidateConstraint(ctx context.Context, constraint *Constraint) (*SchemaValidationResult, error) {
	result := &SchemaValidationResult{
		Valid:    true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	if constraint == nil {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			"", "nil_constraint", "constraint cannot be nil", "NIL_CONSTRAINT"))
		return result, nil
	}

	// Validate constraint name
	if constraint.Name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			constraint.Name, "empty_constraint_name", "constraint name cannot be empty", "EMPTY_NAME"))
		return result, nil
	}

	// Validate constraint type
	validTypes := []string{"PRIMARY_KEY", "FOREIGN_KEY", "UNIQUE", "CHECK"}
	typeUpper := strings.ToUpper(constraint.Type)
	isValid := false
	for _, t := range validTypes {
		if typeUpper == t {
			isValid = true
			break
		}
	}

	if !isValid {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			constraint.Name, "invalid_constraint_type",
			fmt.Sprintf("constraint %s has invalid type: %s", constraint.Name, constraint.Type),
			"INVALID_TYPE"))
		return result, nil
	}

	// Validate columns
	if len(constraint.Columns) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, createValidationError(
			constraint.Name, "no_constraint_columns",
			fmt.Sprintf("constraint %s has no columns", constraint.Name),
			"NO_COLUMNS"))
	}

	// Type-specific validation
	switch typeUpper {
	case "FOREIGN_KEY":
		if constraint.ReferencedTable == "" {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				constraint.Name, "no_referenced_table",
				fmt.Sprintf("foreign key %s has no referenced table", constraint.Name),
				"NO_REF_TABLE"))
		}

		if len(constraint.ReferencedColumns) == 0 {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				constraint.Name, "no_referenced_columns",
				fmt.Sprintf("foreign key %s has no referenced columns", constraint.Name),
				"NO_REF_COLUMNS"))
		}

		if len(constraint.Columns) != len(constraint.ReferencedColumns) {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				constraint.Name, "column_count_mismatch",
				fmt.Sprintf("foreign key %s: column count mismatch", constraint.Name),
				"COLUMN_MISMATCH"))
		}

	case "CHECK":
		if constraint.CheckExpression == "" {
			result.Valid = false
			result.Errors = append(result.Errors, createValidationError(
				constraint.Name, "no_check_expression",
				fmt.Sprintf("check constraint %s has no expression", constraint.Name),
				"NO_EXPRESSION"))
		}
	}

	return result, nil
}

// CheckNamingConventions validates naming conventions
func (v *DefaultSchemaValidator) CheckNamingConventions(ctx context.Context, schema *DatabaseSchema) []ValidationWarning {
	warnings := make([]ValidationWarning, 0)

	// Check tables
	for _, table := range schema.Tables {
		if err := v.validateIdentifier(table.Name, "table", v.namingConventions.TableNamePattern); err != nil {
			warnings = append(warnings, createValidationWarning(
				table.Name, "naming_convention", err.Error(), "VAL007"))
		}

		// Check columns
		for _, col := range table.Columns {
			if err := v.validateIdentifier(col.Name, "column", v.namingConventions.ColumnNamePattern); err != nil {
				warnings = append(warnings, createValidationWarning(
					table.Name, "naming_convention", 
					fmt.Sprintf("table %s: %s", table.Name, err.Error()), "VAL008"))
			}
		}

		// Check indexes
		for _, idx := range table.Indexes {
			if err := v.validateIdentifier(idx.Name, "index", v.namingConventions.IndexNamePattern); err != nil {
				warnings = append(warnings, createValidationWarning(
					table.Name, "naming_convention", err.Error(), "VAL009"))
			}
		}

		// Check constraints
		for _, con := range table.Constraints {
			if err := v.validateIdentifier(con.Name, "constraint", v.namingConventions.ConstraintNamePattern); err != nil {
				warnings = append(warnings, createValidationWarning(
					table.Name, "naming_convention", err.Error(), "VAL010"))
			}
		}
	}

	return warnings
}

// CheckBestPractices validates database best practices
func (v *DefaultSchemaValidator) CheckBestPractices(ctx context.Context, schema *DatabaseSchema) []ValidationWarning {
	warnings := make([]ValidationWarning, 0)

	for _, table := range schema.Tables {
		// Check for primary key
		if len(table.PrimaryKey) == 0 {
			warnings = append(warnings, createValidationWarning(
				table.Name, "best_practice", 
				fmt.Sprintf("table %s has no primary key", table.Name), "VAL011"))
		}

		// Check for id column
		hasIDColumn := false
		for _, col := range table.Columns {
			if col.Name == "id" {
				hasIDColumn = true
				break
			}
		}

		if !hasIDColumn {
			warnings = append(warnings, createValidationWarning(
				table.Name, "best_practice", 
				fmt.Sprintf("table %s has no 'id' column (common convention)", table.Name), "VAL012"))
		}

		// Check for timestamp columns
		hasCreatedAt := false
		hasUpdatedAt := false

		for _, col := range table.Columns {
			if col.Name == "created_at" {
				hasCreatedAt = true
			}
			if col.Name == "updated_at" {
				hasUpdatedAt = true
			}
		}

		if !hasCreatedAt {
			warnings = append(warnings, createValidationWarning(
				table.Name, "best_practice", 
				fmt.Sprintf("table %s has no 'created_at' column (audit best practice)", table.Name), "VAL013"))
		}

		if !hasUpdatedAt {
			warnings = append(warnings, createValidationWarning(
				table.Name, "best_practice", 
				fmt.Sprintf("table %s has no 'updated_at' column (audit best practice)", table.Name), "VAL014"))
		}

		// Check for indexes on foreign keys
		fkColumns := make(map[string]bool)
		for _, con := range table.Constraints {
			if strings.ToUpper(con.Type) == "FOREIGN_KEY" {
				for _, col := range con.Columns {
					fkColumns[col] = true
				}
			}
		}

		indexedColumns := make(map[string]bool)
		for _, idx := range table.Indexes {
			if len(idx.Columns) > 0 {
				indexedColumns[idx.Columns[0]] = true
			}
		}

		for fkCol := range fkColumns {
			if !indexedColumns[fkCol] {
				warnings = append(warnings, createValidationWarning(
					table.Name, "performance", 
					fmt.Sprintf("table %s: foreign key column %s has no index (performance)", table.Name, fkCol), "VAL015"))
			}
		}
	}

	return warnings
}

// Helper methods

func (v *DefaultSchemaValidator) validateIdentifier(name string, objectType string, pattern string) error {
	if len(name) > v.namingConventions.MaxIdentifierLength {
		return fmt.Errorf("%s name '%s' exceeds maximum length of %d", objectType, name, v.namingConventions.MaxIdentifierLength)
	}

	if pattern != "" {
		matched, err := regexp.MatchString(pattern, name)
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}

		if !matched {
			return fmt.Errorf("%s name '%s' does not match pattern %s", objectType, name, pattern)
		}
	}

	if v.namingConventions.EnforceSnakeCase && !v.isSnakeCase(name) {
		return fmt.Errorf("%s name '%s' is not in snake_case", objectType, name)
	}

	return nil
}

func (v *DefaultSchemaValidator) isSnakeCase(s string) bool {
	// snake_case: lowercase letters, digits, underscores
	matched, _ := regexp.MatchString("^[a-z][a-z0-9_]*$", s)
	return matched
}

func (v *DefaultSchemaValidator) isValidColumnType(t string) bool {
	validTypes := []string{
		"integer", "int", "bigint", "smallint", "tinyint",
		"varchar", "char", "text",
		"boolean", "bool",
		"date", "time", "timestamp", "datetime",
		"decimal", "numeric", "float", "double", "real",
		"json", "jsonb",
		"uuid",
		"bytea", "blob",
	}

	tLower := strings.ToLower(t)
	for _, valid := range validTypes {
		if strings.HasPrefix(tLower, valid) {
			return true
		}
	}

	return false
}

func (v *DefaultSchemaValidator) isIntegerType(t string) bool {
	intTypes := []string{"integer", "int", "bigint", "smallint", "tinyint", "serial", "bigserial"}
	tLower := strings.ToLower(t)

	for _, it := range intTypes {
		if strings.Contains(tLower, it) {
			return true
		}
	}

	return false
}

func (v *DefaultSchemaValidator) isStringType(t string) bool {
	stringTypes := []string{"varchar", "char", "text", "string"}
	tLower := strings.ToLower(t)

	for _, st := range stringTypes {
		if strings.Contains(tLower, st) {
			return true
		}
	}

	return false
}

func (v *DefaultSchemaValidator) isDecimalType(t string) bool {
	decimalTypes := []string{"decimal", "numeric", "number"}
	tLower := strings.ToLower(t)

	for _, dt := range decimalTypes {
		if strings.Contains(tLower, dt) {
			return true
		}
	}

	return false
}

/**
 * @file auto_generator.go
 * @description Enhanced automatic rollback script generation for SQL migrations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/rollback/auto_generator.go
 */

package rollback

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// AutoRollbackGenerator generates automatic rollback scripts for SQL migrations
type AutoRollbackGenerator struct {
	dialect string
	config  *AutoRollbackConfig
}

// AutoRollbackConfig contains configuration for automatic rollback generation
type AutoRollbackConfig struct {
	// Whether to generate rollback for data operations (INSERT, UPDATE, DELETE)
	GenerateDataRollbacks bool

	// Whether to include warnings for potentially dangerous operations
	IncludeWarnings bool

	// Whether to generate backup statements before destructive operations
	GenerateBackupStatements bool

	// Maximum number of rows to backup for data operations
	MaxBackupRows int

	// Whether to validate generated rollback scripts
	ValidateGenerated bool

	// Custom rollback templates for specific operations
	CustomTemplates map[string]string
}

// RollbackOperation represents a single rollback operation
type RollbackOperation struct {
	Type           string            `json:"type"`
	SQL            string            `json:"sql"`
	OriginalSQL    string            `json:"original_sql"`
	TableName      string            `json:"table_name"`
	IsReversible   bool              `json:"is_reversible"`
	RequiresBackup bool              `json:"requires_backup"`
	RiskLevel      string            `json:"risk_level"`
	Warnings       []string          `json:"warnings"`
	Metadata       map[string]string `json:"metadata"`
}

// GeneratedRollback represents a complete generated rollback script
type GeneratedRollback struct {
	MigrationID    string              `json:"migration_id"`
	Version        string              `json:"version"`
	Operations     []RollbackOperation `json:"operations"`
	FullScript     string              `json:"full_script"`
	IsComplete     bool                `json:"is_complete"`
	RequiresManual bool                `json:"requires_manual"`
	Warnings       []string            `json:"warnings"`
	Errors         []string            `json:"errors"`
	GeneratedAt    time.Time           `json:"generated_at"`
	Metadata       map[string]string   `json:"metadata"`
}

// NewAutoRollbackGenerator creates a new automatic rollback generator
func NewAutoRollbackGenerator(dialect string, config *AutoRollbackConfig) *AutoRollbackGenerator {
	if config == nil {
		config = &AutoRollbackConfig{
			GenerateDataRollbacks:    false, // Conservative default
			IncludeWarnings:          true,
			GenerateBackupStatements: true,
			MaxBackupRows:            1000,
			ValidateGenerated:        true,
			CustomTemplates:          make(map[string]string),
		}
	}

	return &AutoRollbackGenerator{
		dialect: dialect,
		config:  config,
	}
}

// GenerateRollback generates a complete rollback script for a migration
func (g *AutoRollbackGenerator) GenerateRollback(ctx context.Context, migration *types.Migration) (*GeneratedRollback, error) {
	rollback := &GeneratedRollback{
		MigrationID: migration.ID,
		Version:     migration.Version,
		Operations:  make([]RollbackOperation, 0),
		Warnings:    make([]string, 0),
		Errors:      make([]string, 0),
		GeneratedAt: time.Now(),
		Metadata:    make(map[string]string),
	}

	// Parse SQL statements from the migration
	statements, err := g.parseSQL(migration.Script)
	if err != nil {
		rollback.Errors = append(rollback.Errors, fmt.Sprintf("Failed to parse SQL: %v", err))
		return rollback, nil
	}

	// Generate rollback operations for each statement (in reverse order)
	for i := len(statements) - 1; i >= 0; i-- {
		stmt := statements[i]
		operation, err := g.generateRollbackOperation(stmt)
		if err != nil {
			rollback.Errors = append(rollback.Errors, fmt.Sprintf("Failed to generate rollback for statement: %v", err))
			continue
		}

		if operation != nil {
			rollback.Operations = append(rollback.Operations, *operation)
		}
	}

	// Build the full rollback script
	rollback.FullScript = g.buildFullScript(rollback.Operations)

	// Determine if rollback is complete and requires manual intervention
	rollback.IsComplete = g.isRollbackComplete(rollback.Operations)
	rollback.RequiresManual = g.requiresManualIntervention(rollback.Operations)

	// Collect warnings from operations
	for _, op := range rollback.Operations {
		rollback.Warnings = append(rollback.Warnings, op.Warnings...)
	}

	// Validate generated rollback if enabled
	if g.config.ValidateGenerated {
		if err := g.validateRollback(rollback); err != nil {
			rollback.Errors = append(rollback.Errors, fmt.Sprintf("Validation failed: %v", err))
		}
	}

	return rollback, nil
}

// parseSQL parses SQL script into individual statements
func (g *AutoRollbackGenerator) parseSQL(script string) ([]string, error) {
	// Remove comments and normalize whitespace
	cleaned := g.removeComments(script)

	// Split by semicolons, but be careful about semicolons in string literals
	statements := make([]string, 0)
	current := ""
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(cleaned); i++ {
		char := cleaned[i]

		if !inString && (char == '\'' || char == '"') {
			inString = true
			stringChar = char
		} else if inString && char == stringChar {
			// Check for escaped quotes
			if i > 0 && cleaned[i-1] != '\\' {
				inString = false
			}
		} else if !inString && char == ';' {
			stmt := strings.TrimSpace(current)
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current = ""
			continue
		}

		current += string(char)
	}

	// Add the last statement if it doesn't end with semicolon
	if stmt := strings.TrimSpace(current); stmt != "" {
		statements = append(statements, stmt)
	}

	return statements, nil
}

// removeComments removes SQL comments from the script
func (g *AutoRollbackGenerator) removeComments(script string) string {
	// Remove multi-line comments (/* comment */) first
	multiLineComment := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	script = multiLineComment.ReplaceAllString(script, "")

	// Remove single-line comments (-- comment) line by line
	lines := strings.Split(script, "\n")
	var cleanLines []string

	for _, line := range lines {
		// Remove single-line comments
		if commentIndex := strings.Index(line, "--"); commentIndex != -1 {
			// Check if the -- is inside a string literal
			beforeComment := line[:commentIndex]
			singleQuotes := strings.Count(beforeComment, "'")
			doubleQuotes := strings.Count(beforeComment, "\"")

			// If we have an even number of quotes, the -- is not inside a string
			if singleQuotes%2 == 0 && doubleQuotes%2 == 0 {
				line = line[:commentIndex]
			}
		}

		// Only add non-empty lines
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			cleanLines = append(cleanLines, trimmed)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// generateRollbackOperation generates a rollback operation for a single SQL statement
func (g *AutoRollbackGenerator) generateRollbackOperation(statement string) (*RollbackOperation, error) {
	stmt := strings.TrimSpace(strings.ToUpper(statement))
	originalStmt := strings.TrimSpace(statement)

	operation := &RollbackOperation{
		OriginalSQL: originalStmt,
		Warnings:    make([]string, 0),
		Metadata:    make(map[string]string),
	}

	switch {
	case strings.HasPrefix(stmt, "CREATE TABLE"):
		return g.generateDropTableRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "DROP TABLE"):
		return g.generateCreateTableRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "ALTER TABLE") && strings.Contains(stmt, "ADD COLUMN"):
		return g.generateDropColumnRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "ALTER TABLE") && strings.Contains(stmt, "DROP COLUMN"):
		return g.generateAddColumnRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "CREATE INDEX"):
		return g.generateDropIndexRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "DROP INDEX"):
		return g.generateCreateIndexRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "ALTER TABLE") && strings.Contains(stmt, "ADD CONSTRAINT"):
		return g.generateDropConstraintRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "ALTER TABLE") && strings.Contains(stmt, "DROP CONSTRAINT"):
		return g.generateAddConstraintRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "INSERT"):
		return g.generateDeleteRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "UPDATE"):
		return g.generateUpdateRollback(originalStmt, operation)
	case strings.HasPrefix(stmt, "DELETE"):
		return g.generateInsertRollback(originalStmt, operation)
	default:
		operation.Type = "UNSUPPORTED"
		operation.IsReversible = false
		operation.RiskLevel = "HIGH"
		operation.Warnings = append(operation.Warnings, "Unsupported operation type - manual rollback required")
		return operation, nil
	}
}

// generateDropTableRollback generates rollback for CREATE TABLE
func (g *AutoRollbackGenerator) generateDropTableRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	tableName := g.extractTableName(statement, "CREATE TABLE")
	if tableName == "" {
		return nil, fmt.Errorf("could not extract table name from CREATE TABLE statement")
	}

	operation.Type = "DROP_TABLE"
	operation.TableName = tableName
	operation.SQL = fmt.Sprintf("DROP TABLE IF EXISTS %s;", tableName)
	operation.IsReversible = true
	operation.RequiresBackup = false
	operation.RiskLevel = "LOW"

	return operation, nil
}

// generateCreateTableRollback generates rollback for DROP TABLE
func (g *AutoRollbackGenerator) generateCreateTableRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	tableName := g.extractTableName(statement, "DROP TABLE")
	if tableName == "" {
		return nil, fmt.Errorf("could not extract table name from DROP TABLE statement")
	}

	operation.Type = "CREATE_TABLE"
	operation.TableName = tableName
	operation.IsReversible = false
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, "Cannot automatically recreate dropped table - manual intervention required")
	operation.SQL = fmt.Sprintf("-- Cannot automatically recreate table %s - manual intervention required", tableName)

	return operation, nil
}

// generateDropColumnRollback generates rollback for ADD COLUMN
func (g *AutoRollbackGenerator) generateDropColumnRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	tableName, columnName := g.extractTableAndColumn(statement, "ADD COLUMN")
	if tableName == "" || columnName == "" {
		return nil, fmt.Errorf("could not extract table and column names from ADD COLUMN statement")
	}

	operation.Type = "DROP_COLUMN"
	operation.TableName = tableName
	operation.SQL = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, columnName)
	operation.IsReversible = true
	operation.RequiresBackup = false
	operation.RiskLevel = "MEDIUM"
	operation.Metadata["column_name"] = columnName

	return operation, nil
}

// generateAddColumnRollback generates rollback for DROP COLUMN
func (g *AutoRollbackGenerator) generateAddColumnRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	tableName, columnName := g.extractTableAndColumn(statement, "DROP COLUMN")
	if tableName == "" || columnName == "" {
		return nil, fmt.Errorf("could not extract table and column names from DROP COLUMN statement")
	}

	operation.Type = "ADD_COLUMN"
	operation.TableName = tableName
	operation.IsReversible = false
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, fmt.Sprintf("Cannot automatically recreate dropped column %s - manual intervention required", columnName))
	operation.SQL = fmt.Sprintf("-- Cannot automatically recreate column %s in table %s - manual intervention required", columnName, tableName)
	operation.Metadata["column_name"] = columnName

	return operation, nil
}

// generateDropIndexRollback generates rollback for CREATE INDEX
func (g *AutoRollbackGenerator) generateDropIndexRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	indexName := g.extractIndexName(statement)
	if indexName == "" {
		return nil, fmt.Errorf("could not extract index name from CREATE INDEX statement")
	}

	operation.Type = "DROP_INDEX"
	operation.SQL = fmt.Sprintf("DROP INDEX IF EXISTS %s;", indexName)
	operation.IsReversible = true
	operation.RequiresBackup = false
	operation.RiskLevel = "LOW"
	operation.Metadata["index_name"] = indexName

	return operation, nil
}

// generateCreateIndexRollback generates rollback for DROP INDEX
func (g *AutoRollbackGenerator) generateCreateIndexRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	indexName := g.extractIndexName(statement)
	if indexName == "" {
		return nil, fmt.Errorf("could not extract index name from DROP INDEX statement")
	}

	operation.Type = "CREATE_INDEX"
	operation.IsReversible = false
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, fmt.Sprintf("Cannot automatically recreate dropped index %s - manual intervention required", indexName))
	operation.SQL = fmt.Sprintf("-- Cannot automatically recreate index %s - manual intervention required", indexName)
	operation.Metadata["index_name"] = indexName

	return operation, nil
}

// generateDropConstraintRollback generates rollback for ADD CONSTRAINT
func (g *AutoRollbackGenerator) generateDropConstraintRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	tableName, constraintName := g.extractTableAndConstraint(statement, "ADD CONSTRAINT")
	if tableName == "" || constraintName == "" {
		return nil, fmt.Errorf("could not extract table and constraint names from ADD CONSTRAINT statement")
	}

	operation.Type = "DROP_CONSTRAINT"
	operation.TableName = tableName
	operation.SQL = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", tableName, constraintName)
	operation.IsReversible = true
	operation.RequiresBackup = false
	operation.RiskLevel = "MEDIUM"
	operation.Metadata["constraint_name"] = constraintName

	return operation, nil
}

// generateAddConstraintRollback generates rollback for DROP CONSTRAINT
func (g *AutoRollbackGenerator) generateAddConstraintRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	tableName, constraintName := g.extractTableAndConstraint(statement, "DROP CONSTRAINT")
	if tableName == "" || constraintName == "" {
		return nil, fmt.Errorf("could not extract table and constraint names from DROP CONSTRAINT statement")
	}

	operation.Type = "ADD_CONSTRAINT"
	operation.TableName = tableName
	operation.IsReversible = false
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, fmt.Sprintf("Cannot automatically recreate dropped constraint %s - manual intervention required", constraintName))
	operation.SQL = fmt.Sprintf("-- Cannot automatically recreate constraint %s on table %s - manual intervention required", constraintName, tableName)
	operation.Metadata["constraint_name"] = constraintName

	return operation, nil
}

// generateDeleteRollback generates rollback for INSERT
func (g *AutoRollbackGenerator) generateDeleteRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	if !g.config.GenerateDataRollbacks {
		operation.Type = "DELETE_DATA"
		operation.IsReversible = false
		operation.RiskLevel = "HIGH"
		operation.Warnings = append(operation.Warnings, "Data rollbacks disabled - manual intervention required")
		operation.SQL = "-- Data rollbacks disabled - manual intervention required"
		return operation, nil
	}

	tableName := g.extractTableName(statement, "INSERT INTO")
	if tableName == "" {
		return nil, fmt.Errorf("could not extract table name from INSERT statement")
	}

	operation.Type = "DELETE_DATA"
	operation.TableName = tableName
	operation.IsReversible = false // Complex to implement safely
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, "Data rollback requires manual implementation - consider using WHERE clause based on inserted data")
	operation.SQL = fmt.Sprintf("-- DELETE FROM %s WHERE <condition based on inserted data>;", tableName)

	return operation, nil
}

// generateUpdateRollback generates rollback for UPDATE
func (g *AutoRollbackGenerator) generateUpdateRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	if !g.config.GenerateDataRollbacks {
		operation.Type = "UPDATE_DATA"
		operation.IsReversible = false
		operation.RiskLevel = "HIGH"
		operation.Warnings = append(operation.Warnings, "Data rollbacks disabled - manual intervention required")
		operation.SQL = "-- Data rollbacks disabled - manual intervention required"
		return operation, nil
	}

	tableName := g.extractTableName(statement, "UPDATE")
	if tableName == "" {
		return nil, fmt.Errorf("could not extract table name from UPDATE statement")
	}

	operation.Type = "UPDATE_DATA"
	operation.TableName = tableName
	operation.IsReversible = false // Requires backup of original values
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, "Data rollback requires backup of original values - manual intervention required")
	operation.SQL = fmt.Sprintf("-- UPDATE %s SET <original_values> WHERE <condition>;", tableName)

	return operation, nil
}

// generateInsertRollback generates rollback for DELETE
func (g *AutoRollbackGenerator) generateInsertRollback(statement string, operation *RollbackOperation) (*RollbackOperation, error) {
	if !g.config.GenerateDataRollbacks {
		operation.Type = "INSERT_DATA"
		operation.IsReversible = false
		operation.RiskLevel = "HIGH"
		operation.Warnings = append(operation.Warnings, "Data rollbacks disabled - manual intervention required")
		operation.SQL = "-- Data rollbacks disabled - manual intervention required"
		return operation, nil
	}

	tableName := g.extractTableName(statement, "DELETE FROM")
	if tableName == "" {
		return nil, fmt.Errorf("could not extract table name from DELETE statement")
	}

	operation.Type = "INSERT_DATA"
	operation.TableName = tableName
	operation.IsReversible = false // Requires backup of deleted data
	operation.RequiresBackup = true
	operation.RiskLevel = "HIGH"
	operation.Warnings = append(operation.Warnings, "Data rollback requires backup of deleted data - manual intervention required")
	operation.SQL = fmt.Sprintf("-- INSERT INTO %s <backed_up_data>;", tableName)

	return operation, nil
}

// Helper methods for extracting names from SQL statements
func (g *AutoRollbackGenerator) extractTableName(statement, operation string) string {
	stmt := strings.ToUpper(statement)
	opUpper := strings.ToUpper(operation)

	// Find the operation keyword
	index := strings.Index(stmt, opUpper)
	if index == -1 {
		return ""
	}

	// Extract the part after the operation
	remaining := strings.TrimSpace(stmt[index+len(opUpper):])

	// Handle different cases
	switch opUpper {
	case "CREATE TABLE", "DROP TABLE":
		// Handle "IF EXISTS" or "IF NOT EXISTS"
		if strings.HasPrefix(remaining, "IF") {
			parts := strings.Fields(remaining)
			// For "IF NOT EXISTS table_name" we need parts[3]
			// For "IF EXISTS table_name" we need parts[2]
			if strings.HasPrefix(remaining, "IF NOT EXISTS") && len(parts) >= 4 {
				return strings.Trim(parts[3], "();")
			} else if strings.HasPrefix(remaining, "IF EXISTS") && len(parts) >= 3 {
				return strings.Trim(parts[2], "();")
			}
		} else {
			parts := strings.Fields(remaining)
			if len(parts) >= 1 {
				return strings.Trim(parts[0], "();")
			}
		}
	case "INSERT INTO":
		parts := strings.Fields(remaining)
		if len(parts) >= 1 {
			return strings.Trim(parts[0], "();")
		}
	case "UPDATE":
		parts := strings.Fields(remaining)
		if len(parts) >= 1 {
			return strings.Trim(parts[0], "();")
		}
	case "DELETE FROM":
		parts := strings.Fields(remaining)
		if len(parts) >= 1 {
			return strings.Trim(parts[0], "();")
		}
	}

	return ""
}

func (g *AutoRollbackGenerator) extractTableAndColumn(statement, operation string) (string, string) {
	stmt := strings.ToUpper(statement)
	opUpper := strings.ToUpper(operation)

	// Find the operation keyword
	index := strings.Index(stmt, opUpper)
	if index == -1 {
		return "", ""
	}

	// Extract table name (should be before the operation)
	beforeOp := strings.TrimSpace(stmt[:index])
	tableParts := strings.Fields(beforeOp)
	var tableName string
	if len(tableParts) >= 2 && strings.ToUpper(tableParts[len(tableParts)-2]) == "TABLE" {
		tableName = tableParts[len(tableParts)-1]
	}

	// Extract column name (should be after the operation)
	afterOp := strings.TrimSpace(stmt[index+len(opUpper):])
	columnParts := strings.Fields(afterOp)
	var columnName string
	if len(columnParts) >= 1 {
		columnName = strings.Trim(columnParts[0], "();")
	}

	return tableName, columnName
}

func (g *AutoRollbackGenerator) extractTableAndConstraint(statement, operation string) (string, string) {
	stmt := strings.ToUpper(statement)
	opUpper := strings.ToUpper(operation)

	// Find the operation keyword
	index := strings.Index(stmt, opUpper)
	if index == -1 {
		return "", ""
	}

	// Extract table name (should be before the operation)
	beforeOp := strings.TrimSpace(stmt[:index])
	tableParts := strings.Fields(beforeOp)
	var tableName string
	if len(tableParts) >= 2 && strings.ToUpper(tableParts[len(tableParts)-2]) == "TABLE" {
		tableName = tableParts[len(tableParts)-1]
	}

	// Extract constraint name (should be after the operation)
	afterOp := strings.TrimSpace(stmt[index+len(opUpper):])
	constraintParts := strings.Fields(afterOp)
	var constraintName string
	if len(constraintParts) >= 1 {
		constraintName = strings.Trim(constraintParts[0], "();")
	}

	return tableName, constraintName
}

func (g *AutoRollbackGenerator) extractIndexName(statement string) string {
	stmt := strings.ToUpper(statement)

	// Look for "INDEX" keyword
	indexKeyword := "INDEX"
	index := strings.Index(stmt, indexKeyword)
	if index == -1 {
		return ""
	}

	// Extract the part after "INDEX"
	remaining := strings.TrimSpace(stmt[index+len(indexKeyword):])

	// Handle "IF EXISTS" or "IF NOT EXISTS"
	if strings.HasPrefix(remaining, "IF") {
		parts := strings.Fields(remaining)
		if len(parts) >= 3 {
			return strings.Trim(parts[2], "();")
		}
	} else {
		parts := strings.Fields(remaining)
		if len(parts) >= 1 {
			return strings.Trim(parts[0], "();")
		}
	}

	return ""
}

// buildFullScript builds the complete rollback script from operations
func (g *AutoRollbackGenerator) buildFullScript(operations []RollbackOperation) string {
	if len(operations) == 0 {
		return "-- No rollback operations generated"
	}

	var script strings.Builder

	script.WriteString("-- Automatic Rollback Script\n")
	script.WriteString(fmt.Sprintf("-- Generated at: %s\n", time.Now().Format(time.RFC3339)))
	script.WriteString("-- WARNING: Review this script carefully before execution\n\n")

	for i, op := range operations {
		script.WriteString(fmt.Sprintf("-- Operation %d: %s\n", i+1, op.Type))
		if op.TableName != "" {
			script.WriteString(fmt.Sprintf("-- Table: %s\n", op.TableName))
		}
		script.WriteString(fmt.Sprintf("-- Risk Level: %s\n", op.RiskLevel))
		script.WriteString(fmt.Sprintf("-- Reversible: %t\n", op.IsReversible))

		if len(op.Warnings) > 0 {
			script.WriteString("-- Warnings:\n")
			for _, warning := range op.Warnings {
				script.WriteString(fmt.Sprintf("--   %s\n", warning))
			}
		}

		script.WriteString(op.SQL)
		script.WriteString("\n\n")
	}

	return script.String()
}

// isRollbackComplete checks if the rollback is complete
func (g *AutoRollbackGenerator) isRollbackComplete(operations []RollbackOperation) bool {
	for _, op := range operations {
		if !op.IsReversible {
			return false
		}
	}
	return true
}

// requiresManualIntervention checks if manual intervention is required
func (g *AutoRollbackGenerator) requiresManualIntervention(operations []RollbackOperation) bool {
	for _, op := range operations {
		if !op.IsReversible || op.RequiresBackup || op.RiskLevel == "HIGH" {
			return true
		}
	}
	return false
}

// validateRollback validates the generated rollback script
func (g *AutoRollbackGenerator) validateRollback(rollback *GeneratedRollback) error {
	if rollback.FullScript == "" {
		return fmt.Errorf("empty rollback script")
	}

	if len(rollback.Operations) == 0 {
		return fmt.Errorf("no rollback operations generated")
	}

	// Check for potential issues
	highRiskCount := 0
	for _, op := range rollback.Operations {
		if op.RiskLevel == "HIGH" {
			highRiskCount++
		}
	}

	if highRiskCount > 0 {
		rollback.Warnings = append(rollback.Warnings,
			fmt.Sprintf("Rollback contains %d high-risk operations - manual review required", highRiskCount))
	}

	return nil
}

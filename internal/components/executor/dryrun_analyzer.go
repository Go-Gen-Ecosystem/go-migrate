/**
 * @file dryrun_analyzer.go
 * @description Dry-run analyzer that performs static analysis without executing migrations
 * @author Admilson B. F. Cossa
 * @date 2025-01-31
 * @path pkg/core/database/migrations/internal/components/executor/dryrun_analyzer.go
 */

package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DryRunAnalyzer performs static analysis of migrations without executing them
type DryRunAnalyzer struct {
	dialect string
	parser  *SQLParser
}

// DetailedExecutionPlan contains detailed analysis of a migration
type DetailedExecutionPlan struct {
	MigrationID string
	Operations  []OperationAnalysis
	Summary     PlanSummary
	Warnings    []string
	Errors      []string
	Risks       []RiskAssessment
}

// OperationAnalysis contains analysis of a single SQL operation
type OperationAnalysis struct {
	Type          string        // CREATE, ALTER, DROP, INSERT, UPDATE, DELETE
	Target        string        // Table/Index/Column name
	SQL           string        // Original SQL
	EstimatedTime time.Duration
	Risk          string        // LOW, MEDIUM, HIGH
	Reversible    bool
	Impact        string        // Description of impact
}

// PlanSummary summarizes the execution plan
type PlanSummary struct {
	TotalOperations  int
	EstimatedDuration time.Duration
	OverallRisk      string
	TablesAffected   []string
	IndexesCreated   int
	IndexesDropped   int
	DataModified     bool
}

// RiskAssessment describes potential risks
type RiskAssessment struct {
	Level       string // LOW, MEDIUM, HIGH, CRITICAL
	Category    string // PERFORMANCE, DATA_LOSS, LOCKING, COMPATIBILITY
	Description string
	Mitigation  string
}

// SQLParser parses SQL statements
type SQLParser struct {
	dialect string
}

// ParsedStatement represents a parsed SQL statement
type ParsedStatement struct {
	Type       string
	Action     string
	Target     string
	SQL        string
	Tables     []string
	Columns    []string
	Reversible bool
}

// NewDryRunAnalyzer creates a new dry-run analyzer
func NewDryRunAnalyzer(dialect string) *DryRunAnalyzer {
	return &DryRunAnalyzer{
		dialect: dialect,
		parser:  NewSQLParser(dialect),
	}
}

// AnalyzeMigration performs comprehensive analysis of a migration
func (a *DryRunAnalyzer) AnalyzeMigration(ctx context.Context, migration *types.Migration) (*DetailedExecutionPlan, error) {
	plan := &DetailedExecutionPlan{
		MigrationID: migration.ID,
		Operations:  make([]OperationAnalysis, 0),
		Warnings:    make([]string, 0),
		Errors:      make([]string, 0),
		Risks:       make([]RiskAssessment, 0),
		Summary: PlanSummary{
			TablesAffected: make([]string, 0),
		},
	}

	// Parse SQL into statements
	statements, err := a.parser.Parse(migration.Script)
	if err != nil {
		plan.Errors = append(plan.Errors, fmt.Sprintf("Parse error: %v", err))
		return plan, err
	}

	tablesMap := make(map[string]bool)

	// Analyze each statement
	for _, stmt := range statements {
		op := a.analyzeStatement(stmt)
		plan.Operations = append(plan.Operations, op)
		plan.Summary.TotalOperations++
		plan.Summary.EstimatedDuration += op.EstimatedTime

		// Track affected tables
		if stmt.Target != "" {
			tablesMap[stmt.Target] = true
		}
		for _, table := range stmt.Tables {
			tablesMap[table] = true
		}

		// Track indexes
		if strings.Contains(strings.ToUpper(stmt.Action), "CREATE INDEX") {
			plan.Summary.IndexesCreated++
		} else if strings.Contains(strings.ToUpper(stmt.Action), "DROP INDEX") {
			plan.Summary.IndexesDropped++
		}

		// Check for data modification
		if stmt.Type == "DML" {
			plan.Summary.DataModified = true
		}

		// Assess risks
		risks := a.assessRisks(stmt)
		plan.Risks = append(plan.Risks, risks...)

		// Generate warnings
		warnings := a.generateWarnings(stmt)
		plan.Warnings = append(plan.Warnings, warnings...)
	}

	// Convert tables map to slice
	for table := range tablesMap {
		plan.Summary.TablesAffected = append(plan.Summary.TablesAffected, table)
	}

	// Calculate overall risk
	plan.Summary.OverallRisk = a.calculateOverallRisk(plan.Risks)

	return plan, nil
}

// analyzeStatement analyzes a single SQL statement
func (a *DryRunAnalyzer) analyzeStatement(stmt *ParsedStatement) OperationAnalysis {
	op := OperationAnalysis{
		Type:       stmt.Action,
		Target:     stmt.Target,
		SQL:        stmt.SQL,
		Reversible: stmt.Reversible,
	}

	// Estimate execution time based on operation type
	switch strings.ToUpper(stmt.Type) {
	case "DDL":
		if strings.Contains(strings.ToUpper(stmt.Action), "CREATE INDEX") {
			op.EstimatedTime = time.Second * 30 // Index creation can be slow
			op.Risk = "MEDIUM"
			op.Impact = "Creates index, may lock table briefly"
		} else if strings.Contains(strings.ToUpper(stmt.Action), "ALTER TABLE") {
			op.EstimatedTime = time.Second * 60 // ALTER can be very slow
			op.Risk = "HIGH"
			op.Impact = "Modifies table structure, may lock table for duration"
		} else if strings.Contains(strings.ToUpper(stmt.Action), "CREATE TABLE") {
			op.EstimatedTime = time.Millisecond * 100
			op.Risk = "LOW"
			op.Impact = "Creates new table"
		} else if strings.Contains(strings.ToUpper(stmt.Action), "DROP") {
			op.EstimatedTime = time.Millisecond * 500
			op.Risk = "CRITICAL"
			op.Impact = "DESTRUCTIVE: Removes database object permanently"
		}
	case "DML":
		op.EstimatedTime = time.Second * 5
		op.Risk = "MEDIUM"
		op.Impact = "Modifies data in table"
	default:
		op.EstimatedTime = time.Millisecond * 100
		op.Risk = "LOW"
		op.Impact = "Basic operation"
	}

	return op
}

// assessRisks evaluates potential risks of a statement
func (a *DryRunAnalyzer) assessRisks(stmt *ParsedStatement) []RiskAssessment {
	risks := make([]RiskAssessment, 0)

	upperSQL := strings.ToUpper(stmt.SQL)

	// Check for destructive operations
	if strings.Contains(upperSQL, "DROP TABLE") {
		risks = append(risks, RiskAssessment{
			Level:       "CRITICAL",
			Category:    "DATA_LOSS",
			Description: "DROP TABLE will permanently delete table and all data",
			Mitigation:  "Ensure backups exist and this is intentional",
		})
	}

	// Check for ALTER TABLE without IF EXISTS
	if strings.Contains(upperSQL, "ALTER TABLE") && !strings.Contains(upperSQL, "IF EXISTS") {
		risks = append(risks, RiskAssessment{
			Level:       "MEDIUM",
			Category:    "LOCKING",
			Description: "ALTER TABLE may lock table for extended period",
			Mitigation:  "Consider using IF EXISTS or running during maintenance window",
		})
	}

	// Check for missing indexes on foreign keys
	if strings.Contains(upperSQL, "FOREIGN KEY") && !strings.Contains(stmt.SQL, "INDEX") {
		risks = append(risks, RiskAssessment{
			Level:       "MEDIUM",
			Category:    "PERFORMANCE",
			Description: "Foreign key without index may cause slow queries",
			Mitigation:  "Consider adding index on foreign key column",
		})
	}

	// Check for DELETE/UPDATE without WHERE
	if (strings.Contains(upperSQL, "DELETE FROM") || strings.Contains(upperSQL, "UPDATE ")) &&
		!strings.Contains(upperSQL, "WHERE") {
		risks = append(risks, RiskAssessment{
			Level:       "HIGH",
			Category:    "DATA_LOSS",
			Description: "DELETE/UPDATE without WHERE will affect all rows",
			Mitigation:  "Add WHERE clause to limit scope",
		})
	}

	return risks
}

// generateWarnings generates warnings for a statement
func (a *DryRunAnalyzer) generateWarnings(stmt *ParsedStatement) []string {
	warnings := make([]string, 0)

	upperSQL := strings.ToUpper(stmt.SQL)

	// Warn about SELECT *
	if strings.Contains(upperSQL, "SELECT *") {
		warnings = append(warnings, "SELECT * can be inefficient, consider specifying columns")
	}

	// Warn about TRUNCATE
	if strings.Contains(upperSQL, "TRUNCATE") {
		warnings = append(warnings, "TRUNCATE is irreversible and bypasses transaction log")
	}

	// Warn about large batch operations
	if strings.Contains(upperSQL, "INSERT INTO") && strings.Count(upperSQL, "VALUES") > 100 {
		warnings = append(warnings, "Large batch insert may cause performance issues")
	}

	return warnings
}

// calculateOverallRisk determines the overall risk level
func (a *DryRunAnalyzer) calculateOverallRisk(risks []RiskAssessment) string {
	if len(risks) == 0 {
		return "LOW"
	}

	hasCritical := false
	hasHigh := false
	hasMedium := false

	for _, risk := range risks {
		switch risk.Level {
		case "CRITICAL":
			hasCritical = true
		case "HIGH":
			hasHigh = true
		case "MEDIUM":
			hasMedium = true
		}
	}

	if hasCritical {
		return "CRITICAL"
	}
	if hasHigh {
		return "HIGH"
	}
	if hasMedium {
		return "MEDIUM"
	}
	return "LOW"
}

// FormatDetailedPlan formats the plan for display
func (a *DryRunAnalyzer) FormatDetailedPlan(plan *DetailedExecutionPlan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== DRY RUN ANALYSIS: %s ===\n\n", plan.MigrationID))

	// Summary
	sb.WriteString("SUMMARY:\n")
	sb.WriteString(fmt.Sprintf("  Total Operations: %d\n", plan.Summary.TotalOperations))
	sb.WriteString(fmt.Sprintf("  Estimated Duration: %v\n", plan.Summary.EstimatedDuration))
	sb.WriteString(fmt.Sprintf("  Overall Risk: %s\n", plan.Summary.OverallRisk))
	sb.WriteString(fmt.Sprintf("  Tables Affected: %v\n", plan.Summary.TablesAffected))
	sb.WriteString(fmt.Sprintf("  Indexes Created: %d\n", plan.Summary.IndexesCreated))
	sb.WriteString(fmt.Sprintf("  Indexes Dropped: %d\n", plan.Summary.IndexesDropped))
	sb.WriteString(fmt.Sprintf("  Data Modified: %v\n\n", plan.Summary.DataModified))

	// Operations
	sb.WriteString("OPERATIONS:\n")
	for i, op := range plan.Operations {
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s %s\n", i+1, op.Risk, op.Type, op.Target))
		sb.WriteString(fmt.Sprintf("     SQL: %s\n", truncateSQL(op.SQL, 100)))
		sb.WriteString(fmt.Sprintf("     Impact: %s\n", op.Impact))
		sb.WriteString(fmt.Sprintf("     Estimated Time: %v\n", op.EstimatedTime))
		sb.WriteString(fmt.Sprintf("     Reversible: %v\n\n", op.Reversible))
	}

	// Risks
	if len(plan.Risks) > 0 {
		sb.WriteString("RISKS:\n")
		for i, risk := range plan.Risks {
			sb.WriteString(fmt.Sprintf("  %d. [%s] %s: %s\n", i+1, risk.Level, risk.Category, risk.Description))
			sb.WriteString(fmt.Sprintf("     Mitigation: %s\n\n", risk.Mitigation))
		}
	}

	// Warnings
	if len(plan.Warnings) > 0 {
		sb.WriteString("WARNINGS:\n")
		for i, warning := range plan.Warnings {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, warning))
		}
		sb.WriteString("\n")
	}

	// Errors
	if len(plan.Errors) > 0 {
		sb.WriteString("ERRORS:\n")
		for i, err := range plan.Errors {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("=== END DRY RUN ANALYSIS ===\n")

	return sb.String()
}

// NewSQLParser creates a new SQL parser
func NewSQLParser(dialect string) *SQLParser {
	return &SQLParser{dialect: dialect}
}

// Parse parses SQL script into statements
func (p *SQLParser) Parse(script string) ([]*ParsedStatement, error) {
	statements := make([]*ParsedStatement, 0)

	// Split by semicolon (basic parsing)
	rawStatements := p.splitStatements(script)

	for _, raw := range rawStatements {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "--") {
			continue
		}

		stmt := p.parseStatement(raw)
		statements = append(statements, stmt)
	}

	return statements, nil
}

// splitStatements splits SQL into individual statements
func (p *SQLParser) splitStatements(script string) []string {
	var statements []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool

	for i := 0; i < len(script); i++ {
		char := script[i]

		switch char {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
			current.WriteByte(char)
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
			current.WriteByte(char)
		case ';':
			if !inSingleQuote && !inDoubleQuote {
				stmt := current.String()
				if strings.TrimSpace(stmt) != "" {
					statements = append(statements, stmt)
				}
				current.Reset()
			} else {
				current.WriteByte(char)
			}
		default:
			current.WriteByte(char)
		}
	}

	// Add remaining statement
	if current.Len() > 0 {
		stmt := current.String()
		if strings.TrimSpace(stmt) != "" {
			statements = append(statements, stmt)
		}
	}

	return statements
}

// parseStatement parses a single SQL statement
func (p *SQLParser) parseStatement(sql string) *ParsedStatement {
	stmt := &ParsedStatement{
		SQL:     sql,
		Tables:  make([]string, 0),
		Columns: make([]string, 0),
	}

	upperSQL := strings.ToUpper(strings.TrimSpace(sql))

	// Determine statement type and action
	switch {
	case strings.HasPrefix(upperSQL, "CREATE TABLE"):
		stmt.Type = "DDL"
		stmt.Action = "CREATE TABLE"
		stmt.Target = p.extractTableName(sql, "CREATE TABLE")
		stmt.Reversible = true

	case strings.HasPrefix(upperSQL, "CREATE INDEX"):
		stmt.Type = "DDL"
		stmt.Action = "CREATE INDEX"
		stmt.Target = p.extractIndexName(sql)
		stmt.Reversible = true

	case strings.HasPrefix(upperSQL, "ALTER TABLE"):
		stmt.Type = "DDL"
		stmt.Action = "ALTER TABLE"
		stmt.Target = p.extractTableName(sql, "ALTER TABLE")
		stmt.Reversible = false

	case strings.HasPrefix(upperSQL, "DROP TABLE"):
		stmt.Type = "DDL"
		stmt.Action = "DROP TABLE"
		stmt.Target = p.extractTableName(sql, "DROP TABLE")
		stmt.Reversible = false

	case strings.HasPrefix(upperSQL, "DROP INDEX"):
		stmt.Type = "DDL"
		stmt.Action = "DROP INDEX"
		stmt.Target = p.extractIndexName(sql)
		stmt.Reversible = false

	case strings.HasPrefix(upperSQL, "INSERT INTO"):
		stmt.Type = "DML"
		stmt.Action = "INSERT"
		stmt.Target = p.extractTableName(sql, "INSERT INTO")
		stmt.Reversible = false

	case strings.HasPrefix(upperSQL, "UPDATE"):
		stmt.Type = "DML"
		stmt.Action = "UPDATE"
		stmt.Target = p.extractTableName(sql, "UPDATE")
		stmt.Reversible = false

	case strings.HasPrefix(upperSQL, "DELETE FROM"):
		stmt.Type = "DML"
		stmt.Action = "DELETE"
		stmt.Target = p.extractTableName(sql, "DELETE FROM")
		stmt.Reversible = false

	default:
		stmt.Type = "OTHER"
		stmt.Action = "UNKNOWN"
	}

	return stmt
}

// extractTableName extracts table name from SQL statement
func (p *SQLParser) extractTableName(sql, prefix string) string {
	upperSQL := strings.ToUpper(sql)
	upperPrefix := strings.ToUpper(prefix)

	idx := strings.Index(upperSQL, upperPrefix)
	if idx == -1 {
		return ""
	}

	remainder := strings.TrimSpace(sql[idx+len(prefix):])

	// Extract first word (table name)
	re := regexp.MustCompile(`^[\w.]+`)
	match := re.FindString(remainder)

	// Remove quotes if present
	match = strings.Trim(match, "`\"'")

	return match
}

// extractIndexName extracts index name from SQL statement
func (p *SQLParser) extractIndexName(sql string) string {
	// Pattern: CREATE [UNIQUE] INDEX index_name
	re := regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+([\w.]+)`)
	matches := re.FindStringSubmatch(sql)
	if len(matches) > 1 {
		return strings.Trim(matches[1], "`\"'")
	}

	// Pattern: DROP INDEX index_name
	re = regexp.MustCompile(`(?i)DROP\s+INDEX\s+([\w.]+)`)
	matches = re.FindStringSubmatch(sql)
	if len(matches) > 1 {
		return strings.Trim(matches[1], "`\"'")
	}

	return ""
}

// truncateSQL truncates SQL for display
func truncateSQL(sql string, maxLen int) string {
	if len(sql) <= maxLen {
		return sql
	}
	return sql[:maxLen] + "..."
}

/**
 * @file detector.go
 * @description Core drift detection implementation
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/drift/detector.go
 */

package drift

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DefaultDriftDetector implements DriftDetector interface
type DefaultDriftDetector struct {
	config *DriftDetectionConfig
}

// NewDriftDetector creates a new drift detector
func NewDriftDetector(config *DriftDetectionConfig) *DefaultDriftDetector {
	if config == nil {
		config = &DriftDetectionConfig{
			Enabled:        true,
			AutoFix:        false,
			FailOnDrift:    false,
			FailOnCritical: true,
		}
	}

	return &DefaultDriftDetector{
		config: config,
	}
}

// DetectDrift compares current schema against baseline
func (d *DefaultDriftDetector) DetectDrift(ctx context.Context, baseline *SchemaSnapshot, current *SchemaSnapshot) (*DriftReport, error) {
	startTime := time.Now()

	if baseline == nil {
		return nil, fmt.Errorf("baseline snapshot is required")
	}
	if current == nil {
		return nil, fmt.Errorf("current snapshot is required")
	}

	report := &DriftReport{
		Timestamp:       time.Now(),
		BaselineVersion: baseline.Version,
		Drifts:          make([]Drift, 0),
		Summary: DriftSummary{
			TotalDrifts:      0,
			InfoCount:        0,
			WarningCount:     0,
			CriticalCount:    0,
			AutoFixableCount: 0,
		},
	}

	// Detect table drifts
	tableDrifts := d.detectTableDrifts(baseline, current)
	report.Drifts = append(report.Drifts, tableDrifts...)

	// Detect column drifts
	columnDrifts := d.detectColumnDrifts(baseline, current)
	report.Drifts = append(report.Drifts, columnDrifts...)

	// Detect index drifts
	indexDrifts := d.detectIndexDrifts(baseline, current)
	report.Drifts = append(report.Drifts, indexDrifts...)

	// Detect constraint drifts
	constraintDrifts := d.detectConstraintDrifts(baseline, current)
	report.Drifts = append(report.Drifts, constraintDrifts...)

	// Filter ignored drifts
	filteredDrifts := make([]Drift, 0)
	for _, drift := range report.Drifts {
		if !d.ShouldIgnoreDrift(&drift, d.config.IgnorePatterns) {
			filteredDrifts = append(filteredDrifts, drift)
		}
	}
	report.Drifts = filteredDrifts

	// Calculate summary
	report.Summary = d.calculateSummary(report.Drifts)
	report.DurationMs = time.Since(startTime).Milliseconds()

	return report, nil
}

// detectTableDrifts detects table-level drifts
func (d *DefaultDriftDetector) detectTableDrifts(baseline *SchemaSnapshot, current *SchemaSnapshot) []Drift {
	drifts := make([]Drift, 0)

	// Check for added tables
	for tableName := range current.Tables {
		if _, exists := baseline.Tables[tableName]; !exists {
			drift := Drift{
				Type:        DriftTableAdded,
				Severity:    SeverityWarning,
				ObjectType:  "table",
				ObjectName:  tableName,
				TableName:   tableName,
				Description: fmt.Sprintf("Table '%s' was added outside migration system", tableName),
				DetectedAt:  time.Now(),
				CanAutoFix:  true,
				FixSuggestion: fmt.Sprintf("DROP TABLE %s;", tableName),
			}
			drift.Severity = d.GetDriftSeverity(&drift)
			drifts = append(drifts, drift)
		}
	}

	// Check for dropped tables
	for tableName := range baseline.Tables {
		if _, exists := current.Tables[tableName]; !exists {
			drift := Drift{
				Type:        DriftTableDropped,
				Severity:    SeverityCritical,
				ObjectType:  "table",
				ObjectName:  tableName,
				TableName:   tableName,
				Description: fmt.Sprintf("Table '%s' was dropped outside migration system", tableName),
				DetectedAt:  time.Now(),
				CanAutoFix:  false,
				FixSuggestion: "Manual recreation required - data loss has occurred",
			}
			drift.Severity = d.GetDriftSeverity(&drift)
			drifts = append(drifts, drift)
		}
	}

	return drifts
}

// detectColumnDrifts detects column-level drifts
func (d *DefaultDriftDetector) detectColumnDrifts(baseline *SchemaSnapshot, current *SchemaSnapshot) []Drift {
	drifts := make([]Drift, 0)

	for tableName, currentTable := range current.Tables {
		baselineTable, exists := baseline.Tables[tableName]
		if !exists {
			continue // Skip new tables (handled by table drift)
		}

		// Build maps for easier lookup
		baselineColsMap := make(map[string]ColumnSchema)
		for _, col := range baselineTable.Columns {
			baselineColsMap[col.Name] = col
		}

		currentColsMap := make(map[string]ColumnSchema)
		for _, col := range currentTable.Columns {
			currentColsMap[col.Name] = col
		}

		// Check for added columns
		for _, currentCol := range currentTable.Columns {
			if _, exists := baselineColsMap[currentCol.Name]; !exists {
				drift := Drift{
					Type:        DriftColumnAdded,
					Severity:    SeverityWarning,
					ObjectType:  "column",
					ObjectName:  currentCol.Name,
					TableName:   tableName,
					Actual:      d.columnToMap(&currentCol),
					Description: fmt.Sprintf("Column '%s.%s' was added outside migration system", tableName, currentCol.Name),
					DetectedAt:  time.Now(),
					CanAutoFix:  true,
					FixSuggestion: fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", tableName, currentCol.Name),
				}
				drift.Severity = d.GetDriftSeverity(&drift)
				drifts = append(drifts, drift)
			}
		}

		// Check for dropped columns
		for _, baselineCol := range baselineTable.Columns {
			if _, exists := currentColsMap[baselineCol.Name]; !exists {
				drift := Drift{
					Type:        DriftColumnDropped,
					Severity:    SeverityCritical,
					ObjectType:  "column",
					ObjectName:  baselineCol.Name,
					TableName:   tableName,
					Expected:    d.columnToMap(&baselineCol),
					Description: fmt.Sprintf("Column '%s.%s' was dropped outside migration system", tableName, baselineCol.Name),
					DetectedAt:  time.Now(),
					CanAutoFix:  false,
					FixSuggestion: "Manual recreation required - data loss may have occurred",
				}
				drift.Severity = d.GetDriftSeverity(&drift)
				drifts = append(drifts, drift)
			}
		}

		// Check for modified columns
		for _, currentCol := range currentTable.Columns {
			if baselineCol, exists := baselineColsMap[currentCol.Name]; exists {
				if !d.columnsMatch(&baselineCol, &currentCol) {
					drift := Drift{
						Type:        DriftColumnModified,
						Severity:    SeverityWarning,
						ObjectType:  "column",
						ObjectName:  currentCol.Name,
						TableName:   tableName,
						Expected:    d.columnToMap(&baselineCol),
						Actual:      d.columnToMap(&currentCol),
						Description: fmt.Sprintf("Column '%s.%s' was modified outside migration system", tableName, currentCol.Name),
						DetectedAt:  time.Now(),
						CanAutoFix:  true,
						FixSuggestion: d.generateColumnModificationSQL(tableName, currentCol.Name, &baselineCol),
					}
					drift.Severity = d.GetDriftSeverity(&drift)
					drifts = append(drifts, drift)
				}
			}
		}
	}

	return drifts
}

// detectIndexDrifts detects index-level drifts
func (d *DefaultDriftDetector) detectIndexDrifts(baseline *SchemaSnapshot, current *SchemaSnapshot) []Drift {
	drifts := make([]Drift, 0)

	// Check for added indexes
	for indexName, currentIndex := range current.Indexes {
		if _, exists := baseline.Indexes[indexName]; !exists {
			drift := Drift{
				Type:        DriftIndexAdded,
				Severity:    SeverityInfo,
				ObjectType:  "index",
				ObjectName:  indexName,
				TableName:   currentIndex.TableName,
				Actual:      d.indexToMap(currentIndex),
				Description: fmt.Sprintf("Index '%s' was added outside migration system", indexName),
				DetectedAt:  time.Now(),
				CanAutoFix:  true,
				FixSuggestion: fmt.Sprintf("DROP INDEX %s;", indexName),
			}
			drift.Severity = d.GetDriftSeverity(&drift)
			drifts = append(drifts, drift)
		}
	}

	// Check for dropped indexes
	for indexName, baselineIndex := range baseline.Indexes {
		if _, exists := current.Indexes[indexName]; !exists {
			drift := Drift{
				Type:        DriftIndexDropped,
				Severity:    SeverityWarning,
				ObjectType:  "index",
				ObjectName:  indexName,
				TableName:   baselineIndex.TableName,
				Expected:    d.indexToMap(baselineIndex),
				Description: fmt.Sprintf("Index '%s' was dropped outside migration system", indexName),
				DetectedAt:  time.Now(),
				CanAutoFix:  true,
				FixSuggestion: d.generateCreateIndexSQL(baselineIndex),
			}
			drift.Severity = d.GetDriftSeverity(&drift)
			drifts = append(drifts, drift)
		}
	}

	return drifts
}

// detectConstraintDrifts detects constraint-level drifts
func (d *DefaultDriftDetector) detectConstraintDrifts(baseline *SchemaSnapshot, current *SchemaSnapshot) []Drift {
	drifts := make([]Drift, 0)

	// Check for added constraints
	for constraintName, currentConstraint := range current.Constraints {
		if _, exists := baseline.Constraints[constraintName]; !exists {
			drift := Drift{
				Type:        DriftConstraintAdded,
				Severity:    SeverityWarning,
				ObjectType:  "constraint",
				ObjectName:  constraintName,
				TableName:   currentConstraint.TableName,
				Actual:      d.constraintToMap(currentConstraint),
				Description: fmt.Sprintf("Constraint '%s' was added outside migration system", constraintName),
				DetectedAt:  time.Now(),
				CanAutoFix:  true,
				FixSuggestion: fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s;", currentConstraint.TableName, constraintName),
			}
			drift.Severity = d.GetDriftSeverity(&drift)
			drifts = append(drifts, drift)
		}
	}

	// Check for dropped constraints
	for constraintName, baselineConstraint := range baseline.Constraints {
		if _, exists := current.Constraints[constraintName]; !exists {
			drift := Drift{
				Type:        DriftConstraintDropped,
				Severity:    SeverityCritical,
				ObjectType:  "constraint",
				ObjectName:  constraintName,
				TableName:   baselineConstraint.TableName,
				Expected:    d.constraintToMap(baselineConstraint),
				Description: fmt.Sprintf("Constraint '%s' was dropped outside migration system", constraintName),
				DetectedAt:  time.Now(),
				CanAutoFix:  true,
				FixSuggestion: d.generateAddConstraintSQL(baselineConstraint),
			}
			drift.Severity = d.GetDriftSeverity(&drift)
			drifts = append(drifts, drift)
		}
	}

	return drifts
}

// GetDriftSeverity calculates severity for a drift
func (d *DefaultDriftDetector) GetDriftSeverity(drift *Drift) DriftSeverity {
	// Severity rules based on drift type
	switch drift.Type {
	case DriftTableDropped, DriftColumnDropped, DriftConstraintDropped:
		return SeverityCritical
	case DriftTableAdded, DriftColumnAdded, DriftConstraintAdded:
		return SeverityWarning
	case DriftColumnModified, DriftConstraintModified:
		return SeverityWarning
	case DriftIndexAdded, DriftIndexDropped:
		return SeverityInfo
	default:
		return SeverityInfo
	}
}

// GenerateCompensatingMigration creates a migration to fix drift
func (d *DefaultDriftDetector) GenerateCompensatingMigration(ctx context.Context, drift *Drift) (*types.Migration, error) {
	if !drift.CanAutoFix {
		return nil, fmt.Errorf("drift '%s' cannot be automatically fixed", drift.Description)
	}

	now := time.Now()
	migration := &types.Migration{
		ID:          fmt.Sprintf("DRIFT_FIX_%d", now.Unix()),
		Description: fmt.Sprintf("Fix drift: %s", drift.Description),
		Type:        "sql",
		Script:      drift.FixSuggestion,
		ExecutedAt:  &now,
	}

	return migration, nil
}

// ShouldIgnoreDrift checks if drift should be ignored
func (d *DefaultDriftDetector) ShouldIgnoreDrift(drift *Drift, ignorePatterns []string) bool {
	for _, pattern := range ignorePatterns {
		matched, err := regexp.MatchString(pattern, drift.ObjectName)
		if err == nil && matched {
			return true
		}

		// Also check table name
		matched, err = regexp.MatchString(pattern, drift.TableName)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// Helper methods

func (d *DefaultDriftDetector) calculateSummary(drifts []Drift) DriftSummary {
	summary := DriftSummary{
		TotalDrifts: len(drifts),
	}

	tablesAffected := make(map[string]bool)

	for _, drift := range drifts {
		switch drift.Severity {
		case SeverityInfo:
			summary.InfoCount++
		case SeverityWarning:
			summary.WarningCount++
		case SeverityCritical:
			summary.CriticalCount++
		}

		if drift.CanAutoFix {
			summary.AutoFixableCount++
		}

		if drift.TableName != "" {
			tablesAffected[drift.TableName] = true
		}
	}

	summary.TablesAffected = len(tablesAffected)

	return summary
}

func (d *DefaultDriftDetector) columnsMatch(col1, col2 *ColumnSchema) bool {
	if col1.DataType != col2.DataType {
		return false
	}
	if col1.IsNullable != col2.IsNullable {
		return false
	}
	if col1.MaxLength != col2.MaxLength {
		return false
	}
	return true
}

func (d *DefaultDriftDetector) columnToMap(col *ColumnSchema) map[string]interface{} {
	return map[string]interface{}{
		"name":          col.Name,
		"type":          col.DataType,
		"nullable":      col.IsNullable,
		"default_value": col.DefaultValue,
		"max_length":    col.MaxLength,
	}
}

func (d *DefaultDriftDetector) indexToMap(idx *IndexSchema) map[string]interface{} {
	return map[string]interface{}{
		"name":       idx.Name,
		"table_name": idx.TableName,
		"columns":    idx.Columns,
		"unique":     idx.Unique,
	}
}

func (d *DefaultDriftDetector) constraintToMap(c *ConstraintSchema) map[string]interface{} {
	return map[string]interface{}{
		"name":       c.Name,
		"table_name": c.TableName,
		"type":       c.Type,
		"columns":    c.Columns,
	}
}

func (d *DefaultDriftDetector) generateColumnModificationSQL(tableName, colName string, expectedCol *ColumnSchema) string {
	nullable := "NOT NULL"
	if expectedCol.IsNullable {
		nullable = "NULL"
	}

	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s %s;",
		tableName, colName, expectedCol.DataType, nullable)
}

func (d *DefaultDriftDetector) generateCreateIndexSQL(idx *IndexSchema) string {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}

	columns := strings.Join(idx.Columns, ", ")
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s);",
		unique, idx.Name, idx.TableName, columns)
}

func (d *DefaultDriftDetector) generateAddConstraintSQL(constraint *ConstraintSchema) string {
	columns := strings.Join(constraint.Columns, ", ")

	switch constraint.Type {
	case "primary_key":
		return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s PRIMARY KEY (%s);",
			constraint.TableName, constraint.Name, columns)
	case "foreign_key":
		refCols := strings.Join(constraint.ReferencedColumns, ", ")
		return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s);",
			constraint.TableName, constraint.Name, columns, constraint.ReferencedTable, refCols)
	case "unique":
		return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s);",
			constraint.TableName, constraint.Name, columns)
	default:
		if constraint.Definition != "" {
			return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;",
				constraint.TableName, constraint.Name, constraint.Definition)
		}
		return fmt.Sprintf("-- Cannot auto-generate constraint: %s", constraint.Name)
	}
}

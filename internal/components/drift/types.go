/**
 * @file types.go
 * @description Type definitions for schema drift detection
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/drift/types.go
 */

package drift

import (
	"time"
)

// DriftType represents the type of schema drift
type DriftType string

const (
	DriftTableAdded        DriftType = "table_added"
	DriftTableDropped      DriftType = "table_dropped"
	DriftTableModified     DriftType = "table_modified"
	DriftColumnAdded       DriftType = "column_added"
	DriftColumnDropped     DriftType = "column_dropped"
	DriftColumnModified    DriftType = "column_modified"
	DriftIndexAdded        DriftType = "index_added"
	DriftIndexDropped      DriftType = "index_dropped"
	DriftConstraintAdded   DriftType = "constraint_added"
	DriftConstraintDropped DriftType = "constraint_dropped"
	DriftConstraintModified DriftType = "constraint_modified"
)

// DriftSeverity represents the severity level of drift
type DriftSeverity string

const (
	SeverityInfo     DriftSeverity = "info"
	SeverityWarning  DriftSeverity = "warning"
	SeverityCritical DriftSeverity = "critical"
)

// Drift represents a single schema drift item
type Drift struct {
	Type         DriftType              `json:"type"`
	Severity     DriftSeverity          `json:"severity"`
	ObjectType   string                 `json:"object_type"`   // "table", "column", "index", "constraint"
	ObjectName   string                 `json:"object_name"`
	TableName    string                 `json:"table_name,omitempty"`
	Expected     map[string]interface{} `json:"expected,omitempty"`
	Actual       map[string]interface{} `json:"actual,omitempty"`
	Description  string                 `json:"description"`
	DetectedAt   time.Time              `json:"detected_at"`
	CanAutoFix   bool                   `json:"can_auto_fix"`
	FixSuggestion string                `json:"fix_suggestion,omitempty"`
}

// DriftReport represents a complete drift detection report
type DriftReport struct {
	DatabaseName    string          `json:"database_name"`
	Timestamp       time.Time       `json:"timestamp"`
	BaselineVersion string          `json:"baseline_version"`
	CurrentVersion  string          `json:"current_version"`
	Drifts          []Drift         `json:"drifts"`
	Summary         DriftSummary    `json:"summary"`
	DurationMs      int64           `json:"duration_ms"`
}

// DriftSummary provides summary statistics for drift report
type DriftSummary struct {
	TotalDrifts      int `json:"total_drifts"`
	InfoCount        int `json:"info_count"`
	WarningCount     int `json:"warning_count"`
	CriticalCount    int `json:"critical_count"`
	AutoFixableCount int `json:"auto_fixable_count"`
	TablesAffected   int `json:"tables_affected"`
}

// SchemaSnapshot represents a snapshot of database schema
type SchemaSnapshot struct {
	Version    string                  `json:"version"`
	Timestamp  time.Time               `json:"timestamp"`
	Tables     map[string]*TableSchema `json:"tables"`
	Indexes    map[string]*IndexSchema `json:"indexes"`
	Constraints map[string]*ConstraintSchema `json:"constraints"`
}

// TableSchema represents a table's schema
type TableSchema struct {
	Name        string                   `json:"name"`
	Columns     []ColumnSchema           `json:"columns"`
	PrimaryKey  []string                 `json:"primary_key,omitempty"`
	Indexes     []IndexSchema            `json:"indexes,omitempty"`
	Constraints []ConstraintSchema       `json:"constraints,omitempty"`
}

// ColumnSchema represents a column's schema
type ColumnSchema struct {
	Name         string      `json:"name"`
	DataType     string      `json:"data_type"`
	IsNullable   bool        `json:"is_nullable"`
	DefaultValue interface{} `json:"default_value,omitempty"`
	MaxLength    int         `json:"max_length,omitempty"`
	Precision    int         `json:"precision,omitempty"`
	Scale        int         `json:"scale,omitempty"`
}

// IndexSchema represents an index's schema
type IndexSchema struct {
	Name       string   `json:"name"`
	TableName  string   `json:"table_name"`
	Columns    []string `json:"columns"`
	Unique     bool     `json:"unique"`
	Type       string   `json:"type,omitempty"` // "btree", "hash", etc.
}

// ConstraintSchema represents a constraint's schema
type ConstraintSchema struct {
	Name          string   `json:"name"`
	TableName     string   `json:"table_name"`
	Type          string   `json:"type"` // "primary_key", "foreign_key", "unique", "check"
	Columns       []string `json:"columns"`
	ReferencedTable string `json:"referenced_table,omitempty"`
	ReferencedColumns []string `json:"referenced_columns,omitempty"`
	Definition    string   `json:"definition,omitempty"`
}

// DriftDetectionConfig represents configuration for drift detection
type DriftDetectionConfig struct {
	Enabled              bool          `json:"enabled"`
	BaselineVersion      string        `json:"baseline_version"`
	AutoFix              bool          `json:"auto_fix"`
	IgnorePatterns       []string      `json:"ignore_patterns"`
	CheckInterval        time.Duration `json:"check_interval"`
	FailOnDrift          bool          `json:"fail_on_drift"`
	FailOnCritical       bool          `json:"fail_on_critical"`
	GenerateCompensation bool          `json:"generate_compensation"`
}

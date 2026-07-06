/**
 * @file types.go
 * @description Type definitions for state-based migrations (declarative schema)
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/types.go
 */

package state

import (
	"time"
)

// SchemaFormat represents the declarative schema file format
type SchemaFormat string

const (
	FormatYAML SchemaFormat = "yaml"
	FormatJSON SchemaFormat = "json"
	FormatHCL  SchemaFormat = "hcl"
)

// DatabaseSchema represents a complete declarative database schema
type DatabaseSchema struct {
	Version     string                 `json:"version" yaml:"version" hcl:"version"`
	Name        string                 `json:"name" yaml:"name" hcl:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty" hcl:"description,optional"`
	Tables      []Table                `json:"tables" yaml:"tables" hcl:"table,block"`
	Indexes     []Index                `json:"indexes,omitempty" yaml:"indexes,omitempty" hcl:"index,block"`
	Constraints []Constraint           `json:"constraints,omitempty" yaml:"constraints,omitempty" hcl:"constraint,block"`
	Triggers    []Trigger              `json:"triggers,omitempty" yaml:"triggers,omitempty" hcl:"trigger,block"`
	Views       []View                 `json:"views,omitempty" yaml:"views,omitempty" hcl:"view,block"`
	Metadata    map[string]interface{} `json:"metadata,omitempty" yaml:"metadata,omitempty" hcl:"metadata,optional"`
}

// Table represents a declarative table definition
type Table struct {
	Name        string                 `json:"name" yaml:"name" hcl:"name,label"`
	Comment     string                 `json:"comment,omitempty" yaml:"comment,omitempty" hcl:"comment,optional"`
	Columns     []Column               `json:"columns" yaml:"columns" hcl:"column,block"`
	PrimaryKey  []string               `json:"primary_key,omitempty" yaml:"primary_key,omitempty" hcl:"primary_key,optional"`
	Indexes     []Index                `json:"indexes,omitempty" yaml:"indexes,omitempty" hcl:"index,block"`
	Constraints []Constraint           `json:"constraints,omitempty" yaml:"constraints,omitempty" hcl:"constraint,block"`
	Options     map[string]interface{} `json:"options,omitempty" yaml:"options,omitempty" hcl:"options,optional"`
}

// Column represents a declarative column definition
type Column struct {
	Name         string                 `json:"name" yaml:"name" hcl:"name,label"`
	Type         string                 `json:"type" yaml:"type" hcl:"type"`
	Nullable     bool                   `json:"nullable,omitempty" yaml:"nullable,omitempty" hcl:"nullable,optional"`
	Default      interface{}            `json:"default,omitempty" yaml:"default,omitempty" hcl:"default,optional"`
	AutoIncrement bool                  `json:"auto_increment,omitempty" yaml:"auto_increment,omitempty" hcl:"auto_increment,optional"`
	Unique       bool                   `json:"unique,omitempty" yaml:"unique,omitempty" hcl:"unique,optional"`
	Comment      string                 `json:"comment,omitempty" yaml:"comment,omitempty" hcl:"comment,optional"`
	Length       int                    `json:"length,omitempty" yaml:"length,omitempty" hcl:"length,optional"`
	Precision    int                    `json:"precision,omitempty" yaml:"precision,omitempty" hcl:"precision,optional"`
	Scale        int                    `json:"scale,omitempty" yaml:"scale,omitempty" hcl:"scale,optional"`
	Charset      string                 `json:"charset,omitempty" yaml:"charset,omitempty" hcl:"charset,optional"`
	Collation    string                 `json:"collation,omitempty" yaml:"collation,omitempty" hcl:"collation,optional"`
	Options      map[string]interface{} `json:"options,omitempty" yaml:"options,omitempty" hcl:"options,optional"`
}

// Index represents a declarative index definition
type Index struct {
	Name    string   `json:"name" yaml:"name" hcl:"name,label"`
	Table   string   `json:"table,omitempty" yaml:"table,omitempty" hcl:"table,optional"`
	Columns []string `json:"columns" yaml:"columns" hcl:"columns"`
	Unique  bool     `json:"unique,omitempty" yaml:"unique,omitempty" hcl:"unique,optional"`
	Type    string   `json:"type,omitempty" yaml:"type,omitempty" hcl:"type,optional"` // btree, hash, gin, gist, etc.
	Where   string   `json:"where,omitempty" yaml:"where,omitempty" hcl:"where,optional"` // partial index condition
	Comment string   `json:"comment,omitempty" yaml:"comment,omitempty" hcl:"comment,optional"`
}

// Constraint represents a declarative constraint definition
type Constraint struct {
	Name             string   `json:"name" yaml:"name" hcl:"name,label"`
	Table            string   `json:"table,omitempty" yaml:"table,omitempty" hcl:"table,optional"`
	Type             string   `json:"type" yaml:"type" hcl:"type"` // primary_key, foreign_key, unique, check
	Columns          []string `json:"columns" yaml:"columns" hcl:"columns"`
	ReferencedTable  string   `json:"referenced_table,omitempty" yaml:"referenced_table,omitempty" hcl:"referenced_table,optional"`
	ReferencedColumns []string `json:"referenced_columns,omitempty" yaml:"referenced_columns,omitempty" hcl:"referenced_columns,optional"`
	OnDelete         string   `json:"on_delete,omitempty" yaml:"on_delete,omitempty" hcl:"on_delete,optional"` // CASCADE, SET NULL, RESTRICT, etc.
	OnUpdate         string   `json:"on_update,omitempty" yaml:"on_update,omitempty" hcl:"on_update,optional"`
	CheckExpression  string   `json:"check_expression,omitempty" yaml:"check_expression,omitempty" hcl:"check_expression,optional"`
	Comment          string   `json:"comment,omitempty" yaml:"comment,omitempty" hcl:"comment,optional"`
}

// Trigger represents a declarative trigger definition
type Trigger struct {
	Name      string `json:"name" yaml:"name" hcl:"name,label"`
	Table     string `json:"table" yaml:"table" hcl:"table"`
	When      string `json:"when" yaml:"when" hcl:"when"`       // BEFORE, AFTER, INSTEAD OF
	Event     string `json:"event" yaml:"event" hcl:"event"`    // INSERT, UPDATE, DELETE
	ForEachRow bool  `json:"for_each_row" yaml:"for_each_row" hcl:"for_each_row"`
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty" hcl:"condition,optional"`
	Body      string `json:"body" yaml:"body" hcl:"body"`
	Comment   string `json:"comment,omitempty" yaml:"comment,omitempty" hcl:"comment,optional"`
}

// View represents a declarative view definition
type View struct {
	Name       string                 `json:"name" yaml:"name" hcl:"name,label"`
	Query      string                 `json:"query" yaml:"query" hcl:"query"`
	Materialized bool                 `json:"materialized,omitempty" yaml:"materialized,omitempty" hcl:"materialized,optional"`
	Comment    string                 `json:"comment,omitempty" yaml:"comment,omitempty" hcl:"comment,optional"`
	Options    map[string]interface{} `json:"options,omitempty" yaml:"options,omitempty" hcl:"options,optional"`
}

// SchemaDiff represents differences between desired and actual schema
type SchemaDiff struct {
	HasChanges       bool              `json:"has_changes"`
	TablesAdded      []Table           `json:"tables_added,omitempty"`
	TablesDropped    []Table           `json:"tables_dropped,omitempty"`
	TablesModified   []TableDiff       `json:"tables_modified,omitempty"`
	IndexesAdded     []Index           `json:"indexes_added,omitempty"`
	IndexesDropped   []Index           `json:"indexes_dropped,omitempty"`
	ConstraintsAdded []Constraint      `json:"constraints_added,omitempty"`
	ConstraintsDropped []Constraint    `json:"constraints_dropped,omitempty"`
	TriggersAdded    []Trigger         `json:"triggers_added,omitempty"`
	TriggersDropped  []Trigger         `json:"triggers_dropped,omitempty"`
	ViewsAdded       []View            `json:"views_added,omitempty"`
	ViewsDropped     []View            `json:"views_dropped,omitempty"`
	Summary          DiffSummary       `json:"summary"`
}

// TableDiff represents differences in a table
type TableDiff struct {
	TableName         string         `json:"table_name"`
	ColumnsAdded      []Column       `json:"columns_added,omitempty"`
	ColumnsDropped    []Column       `json:"columns_dropped,omitempty"`
	ColumnsModified   []ColumnDiff   `json:"columns_modified,omitempty"`
	PrimaryKeyChanged bool           `json:"primary_key_changed,omitempty"`
	OldPrimaryKey     []string       `json:"old_primary_key,omitempty"`
	NewPrimaryKey     []string       `json:"new_primary_key,omitempty"`
}

// ColumnDiff represents differences in a column
type ColumnDiff struct {
	ColumnName   string      `json:"column_name"`
	OldType      string      `json:"old_type,omitempty"`
	NewType      string      `json:"new_type,omitempty"`
	OldNullable  bool        `json:"old_nullable,omitempty"`
	NewNullable  bool        `json:"new_nullable,omitempty"`
	OldDefault   interface{} `json:"old_default,omitempty"`
	NewDefault   interface{} `json:"new_default,omitempty"`
	Changes      []string    `json:"changes"` // List of change descriptions
}

// DiffSummary provides statistics about schema differences
type DiffSummary struct {
	TotalChanges        int `json:"total_changes"`
	TablesAdded         int `json:"tables_added"`
	TablesDropped       int `json:"tables_dropped"`
	TablesModified      int `json:"tables_modified"`
	ColumnsAdded        int `json:"columns_added"`
	ColumnsDropped      int `json:"columns_dropped"`
	ColumnsModified     int `json:"columns_modified"`
	IndexesAdded        int `json:"indexes_added"`
	IndexesDropped      int `json:"indexes_dropped"`
	ConstraintsAdded    int `json:"constraints_added"`
	ConstraintsDropped  int `json:"constraints_dropped"`
	DestructiveChanges  int `json:"destructive_changes"` // Drops that may cause data loss
	SafeToApply         bool `json:"safe_to_apply"`
}

// GeneratedMigration represents an auto-generated migration from schema diff
type GeneratedMigration struct {
	ID              string    `json:"id"`
	Version         string    `json:"version"`
	Description     string    `json:"description"`
	UpSQL           string    `json:"up_sql"`
	DownSQL         string    `json:"down_sql"`
	Diff            *SchemaDiff `json:"diff"`
	GeneratedAt     time.Time `json:"generated_at"`
	SourceSchema    string    `json:"source_schema"` // Path to declarative schema file
	SafeToAutoApply bool      `json:"safe_to_auto_apply"`
	Warnings        []string  `json:"warnings,omitempty"`
}

// SchemaValidationResult represents the result of schema validation
type SchemaValidationResult struct {
	Valid    bool                `json:"valid"`
	Errors   []ValidationError   `json:"errors,omitempty"`
	Warnings []ValidationWarning `json:"warnings,omitempty"`
	Summary  string              `json:"summary"`
}

// StateBasedConfig represents configuration for state-based migrations
type StateBasedConfig struct {
	Enabled            bool     `json:"enabled"`
	SchemaPath         string   `json:"schema_path"`         // Path to schema file/directory
	SchemaFormat       SchemaFormat `json:"schema_format"`   // yaml, json, hcl
	AutoGenerate       bool     `json:"auto_generate"`       // Auto-generate migrations
	AutoApply          bool     `json:"auto_apply"`          // Auto-apply safe migrations
	AllowDestructive   bool     `json:"allow_destructive"`   // Allow destructive changes
	RequireReview      bool     `json:"require_review"`      // Require manual review
	GitOpsEnabled      bool     `json:"gitops_enabled"`      // Enable GitOps workflows
	MigrationOutputDir string   `json:"migration_output_dir"` // Where to save generated migrations
}

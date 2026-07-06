/**
 * @file interfaces.go
 * @description Interface definitions for state-based migration system
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/interfaces.go
 */

package state

import (
	"context"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// SchemaParser parses declarative schema files into DatabaseSchema
type SchemaParser interface {
	// Parse loads and parses a schema file
	Parse(ctx context.Context, path string) (*DatabaseSchema, error)

	// ParseContent parses schema from string content
	ParseContent(ctx context.Context, content string, format SchemaFormat) (*DatabaseSchema, error)

	// Validate validates schema syntax and structure
	Validate(ctx context.Context, schema *DatabaseSchema) (*SchemaValidationResult, error)

	// SupportedFormats returns list of supported schema formats
	SupportedFormats() []SchemaFormat
}

// SchemaExtractor extracts current database schema into DatabaseSchema format
type SchemaExtractor interface {
	// ExtractSchema extracts current database schema
	ExtractSchema(ctx context.Context) (*DatabaseSchema, error)

	// ExtractTable extracts specific table schema
	ExtractTable(ctx context.Context, tableName string) (*Table, error)

	// GetVersion returns current schema version from database
	GetVersion(ctx context.Context) (string, error)

	// SetVersion updates schema version in database
	SetVersion(ctx context.Context, version string) error
}

// SchemaDiffer compares desired schema with actual database schema
type SchemaDiffer interface {
	// Diff compares desired schema with actual schema
	Diff(ctx context.Context, desired *DatabaseSchema, actual *DatabaseSchema) (*SchemaDiff, error)

	// DiffTables compares specific tables
	DiffTables(ctx context.Context, desired *Table, actual *Table) (*TableDiff, error)

	// CalculateSafety determines if changes are safe to auto-apply
	CalculateSafety(ctx context.Context, diff *SchemaDiff) bool

	// GetDestructiveChanges returns list of potentially destructive changes
	GetDestructiveChanges(ctx context.Context, diff *SchemaDiff) []string
}

// MigrationGenerator generates SQL migrations from schema differences
type MigrationGenerator interface {
	// Generate creates migration SQL from schema diff
	Generate(ctx context.Context, diff *SchemaDiff, desired *DatabaseSchema) (*GeneratedMigration, error)

	// GenerateUp generates only UP migration SQL
	GenerateUp(ctx context.Context, diff *SchemaDiff, desired *DatabaseSchema) (string, error)

	// GenerateDown generates only DOWN migration SQL
	GenerateDown(ctx context.Context, diff *SchemaDiff, actual *DatabaseSchema) (string, error)

	// GenerateFile writes migration to file in standard format
	GenerateFile(ctx context.Context, migration *GeneratedMigration, outputDir string) (string, error)

	// SetDialect configures SQL dialect for generation
	SetDialect(dialect string)
}

// SchemaValidator validates declarative schemas for correctness
type SchemaValidator interface {
	// ValidateSchema validates entire schema definition
	ValidateSchema(ctx context.Context, schema *DatabaseSchema) (*SchemaValidationResult, error)

	// ValidateTable validates table definition
	ValidateTable(ctx context.Context, table *Table) (*SchemaValidationResult, error)

	// ValidateColumn validates column definition
	ValidateColumn(ctx context.Context, column *Column) (*SchemaValidationResult, error)

	// ValidateConstraint validates constraint definition
	ValidateConstraint(ctx context.Context, constraint *Constraint) (*SchemaValidationResult, error)

	// CheckNamingConventions validates naming conventions
	CheckNamingConventions(ctx context.Context, schema *DatabaseSchema) []string

	// CheckBestPractices validates database best practices
	CheckBestPractices(ctx context.Context, schema *DatabaseSchema) []string
}

// StateBasedMigrator orchestrates state-based migration workflow
type StateBasedMigrator interface {
	// Plan creates migration plan from schema file
	Plan(ctx context.Context, schemaPath string) (*GeneratedMigration, error)

	// Apply applies generated migration
	Apply(ctx context.Context, migration *GeneratedMigration) error

	// Sync synchronizes database to match schema file
	Sync(ctx context.Context, schemaPath string, dryRun bool) (*types.MigrationStatus, error)

	// Preview shows what would change without applying
	Preview(ctx context.Context, schemaPath string) (*SchemaDiff, error)

	// Export exports current database schema to file
	Export(ctx context.Context, outputPath string, format SchemaFormat) error

	// Import imports schema from file and applies changes
	Import(ctx context.Context, schemaPath string, autoApply bool) (*GeneratedMigration, error)

	// ValidateSchema validates a schema file
	ValidateSchema(ctx context.Context, schemaPath string) (*SchemaValidationResult, error)
}

// GitOpsManager manages GitOps workflows for schema changes
type GitOpsManager interface {
	// DetectSchemaChanges detects schema file changes in Git
	DetectSchemaChanges(ctx context.Context, gitRef string) ([]string, error)

	// ValidateSchemaInCI validates schema changes in CI pipeline
	ValidateSchemaInCI(ctx context.Context, schemaPath string) (*SchemaValidationResult, error)

	// GeneratePullRequestComment generates PR comment with migration preview
	GeneratePullRequestComment(ctx context.Context, diff *SchemaDiff) (string, error)

	// ApproveChange marks schema change as approved
	ApproveChange(ctx context.Context, changeID string, approver string) error

	// RejectChange marks schema change as rejected
	RejectChange(ctx context.Context, changeID string, reason string) error
}

// SchemaVersionManager manages schema versioning
type SchemaVersionManager interface {
	// GetCurrentVersion returns current schema version
	GetCurrentVersion(ctx context.Context) (string, error)

	// SetVersion updates schema version
	SetVersion(ctx context.Context, version string) error

	// GetVersionHistory returns history of schema versions
	GetVersionHistory(ctx context.Context) ([]SchemaVersion, error)

	// CompareVersions compares two schema versions
	CompareVersions(ctx context.Context, fromVersion string, toVersion string) (*SchemaDiff, error)
}

// SchemaVersion represents a schema version record
type SchemaVersion struct {
	Version     string    `json:"version"`
	Description string    `json:"description"`
	AppliedAt   string    `json:"applied_at"`
	AppliedBy   string    `json:"applied_by,omitempty"`
	SchemaHash  string    `json:"schema_hash"`
}

// SchemaSerializer serializes/deserializes schemas
type SchemaSerializer interface {
	// Serialize converts DatabaseSchema to bytes
	Serialize(ctx context.Context, schema *DatabaseSchema, format SchemaFormat) ([]byte, error)

	// Deserialize converts bytes to DatabaseSchema
	Deserialize(ctx context.Context, data []byte, format SchemaFormat) (*DatabaseSchema, error)

	// SerializeToFile writes schema to file
	SerializeToFile(ctx context.Context, schema *DatabaseSchema, path string, format SchemaFormat) error

	// DeserializeFromFile loads and deserializes schema from file
	DeserializeFromFile(ctx context.Context, path string) (*DatabaseSchema, error)
}

// ValidationError represents a validation error
type ValidationError struct {
	Migration       string            `json:"migration"`
	Type            string            `json:"type"`
	Message         string            `json:"message"`
	Line            int               `json:"line"`
	Column          int               `json:"column"`
	Severity        string            `json:"severity"`
	Code            string            `json:"code"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Migration       string            `json:"migration"`
	Type            string            `json:"type"`
	Message         string            `json:"message"`
	Line            int               `json:"line"`
	Column          int               `json:"column"`
	Code            string            `json:"code"`
}

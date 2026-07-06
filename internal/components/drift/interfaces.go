/**
 * @file interfaces.go
 * @description Interface definitions for drift detection following goMigrate architecture
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/drift/interfaces.go
 */

package drift

import (
	"context"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// DriftDetector defines the interface for detecting schema drift
type DriftDetector interface {
	// DetectDrift compares current schema against baseline and returns drift report
	DetectDrift(ctx context.Context, baseline *SchemaSnapshot, current *SchemaSnapshot) (*DriftReport, error)

	// GetDriftSeverity calculates severity for a drift
	GetDriftSeverity(drift *Drift) DriftSeverity

	// GenerateCompensatingMigration creates a migration to fix drift
	GenerateCompensatingMigration(ctx context.Context, drift *Drift) (*types.Migration, error)

	// ShouldIgnoreDrift checks if drift should be ignored based on patterns
	ShouldIgnoreDrift(drift *Drift, ignorePatterns []string) bool
}

// SchemaExtractor defines the interface for extracting schema snapshots
type SchemaExtractor interface {
	// ExtractSchema extracts current database schema
	ExtractSchema(ctx context.Context) (*SchemaSnapshot, error)

	// ExtractTableSchema extracts schema for a specific table
	ExtractTableSchema(ctx context.Context, tableName string) (*TableSchema, error)

	// GetTableNames returns all table names in database
	GetTableNames(ctx context.Context) ([]string, error)

	// GetIndexes returns all indexes for a table
	GetIndexes(ctx context.Context, tableName string) ([]IndexSchema, error)

	// GetConstraints returns all constraints for a table
	GetConstraints(ctx context.Context, tableName string) ([]ConstraintSchema, error)
}

// BaselineManager defines the interface for managing schema baselines
type BaselineManager interface {
	// SaveBaseline saves a schema snapshot as baseline
	SaveBaseline(ctx context.Context, version string, snapshot *SchemaSnapshot) error

	// LoadBaseline loads a baseline by version
	LoadBaseline(ctx context.Context, version string) (*SchemaSnapshot, error)

	// GetLatestBaseline returns the most recent baseline
	GetLatestBaseline(ctx context.Context) (*SchemaSnapshot, error)

	// ListBaselines returns all available baselines
	ListBaselines(ctx context.Context) ([]string, error)

	// DeleteBaseline deletes a baseline
	DeleteBaseline(ctx context.Context, version string) error
}

// DriftReporter defines the interface for generating drift reports
type DriftReporter interface {
	// GenerateReport creates a formatted drift report
	GenerateReport(ctx context.Context, report *DriftReport) (string, error)

	// GenerateJSON creates a JSON drift report
	GenerateJSON(ctx context.Context, report *DriftReport) ([]byte, error)

	// GenerateHTML creates an HTML drift report
	GenerateHTML(ctx context.Context, report *DriftReport) (string, error)

	// ExportReport exports report to file
	ExportReport(ctx context.Context, report *DriftReport, path string) error
}

// DriftFixer defines the interface for automatically fixing drift
type DriftFixer interface {
	// CanFix determines if drift can be automatically fixed
	CanFix(drift *Drift) bool

	// GenerateFix generates SQL to fix drift
	GenerateFix(ctx context.Context, drift *Drift) (string, error)

	// ApplyFix applies fix and validates result
	ApplyFix(ctx context.Context, drift *Drift) error

	// ValidateFix verifies fix was successful
	ValidateFix(ctx context.Context, drift *Drift) error
}

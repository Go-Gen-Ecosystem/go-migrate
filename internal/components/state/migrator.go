/**
 * @file migrator.go
 * @description Orchestrates state-based migration workflow (plan → apply → sync)
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/migrator.go
 */

package state

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DefaultStateBasedMigrator implements StateBasedMigrator interface
type DefaultStateBasedMigrator struct {
	db        *sql.DB
	config    *StateBasedConfig
	parser    SchemaParser
	extractor SchemaExtractor
	differ    SchemaDiffer
	generator MigrationGenerator
	validator SchemaValidator
}

// NewDefaultStateBasedMigrator creates a new state-based migrator
func NewDefaultStateBasedMigrator(db *sql.DB, config *StateBasedConfig) *DefaultStateBasedMigrator {
	if config == nil {
		config = &StateBasedConfig{
			Enabled:            true,
			SchemaPath:         "schema.yaml",
			SchemaFormat:       FormatYAML,
			AutoGenerate:       true,
			AutoApply:          false,
			AllowDestructive:   false,
			RequireReview:      true,
			GitOpsEnabled:      false,
			MigrationOutputDir: "migrations/generated",
		}
	}

	// Initialize components
	validator := NewDefaultSchemaValidator(nil)
	parser := NewDefaultSchemaParser(validator)
	extractor := NewPostgreSQLSchemaExtractor(db)
	differ := NewDefaultSchemaDiffer(config)
	generator := NewDefaultMigrationGenerator("postgresql")

	return &DefaultStateBasedMigrator{
		db:        db,
		config:    config,
		parser:    parser,
		extractor: extractor,
		differ:    differ,
		generator: generator,
		validator: validator,
	}
}

// Plan creates migration plan from schema file
func (m *DefaultStateBasedMigrator) Plan(ctx context.Context, schemaPath string) (*GeneratedMigration, error) {
	if schemaPath == "" {
		schemaPath = m.config.SchemaPath
	}

	// 1. Parse desired schema
	desired, err := m.parser.Parse(ctx, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema file: %w", err)
	}

	// 2. Validate desired schema
	result, err := m.validator.ValidateSchema(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("schema validation failed: %w", err)
	}

	if !result.Valid {
		return nil, fmt.Errorf("invalid schema: %v", result.Errors)
	}

	// 3. Extract current schema
	actual, err := m.extractor.ExtractSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract current schema: %w", err)
	}

	// 4. Calculate differences
	diff, err := m.differ.Diff(ctx, desired, actual)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate schema diff: %w", err)
	}

	// 5. Check if there are changes
	if !diff.HasChanges {
		return nil, nil // No migration needed
	}

	// 6. Generate migration
	migration, err := m.generator.Generate(ctx, diff, desired)
	if err != nil {
		return nil, fmt.Errorf("failed to generate migration: %w", err)
	}

	// 7. Set source schema path
	migration.SourceSchema = schemaPath

	return migration, nil
}

// Apply applies generated migration
func (m *DefaultStateBasedMigrator) Apply(ctx context.Context, migration *GeneratedMigration) error {
	if migration == nil {
		return fmt.Errorf("migration cannot be nil")
	}

	// Safety check
	if !migration.SafeToAutoApply && !m.config.AllowDestructive {
		return fmt.Errorf("migration contains destructive changes and auto-apply is not allowed")
	}

	// Begin transaction
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Execute migration SQL
	_, err = tx.ExecContext(ctx, migration.UpSQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Update schema version
	if migration.Version != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO schema_version (version, applied_at, description)
			VALUES ($1, CURRENT_TIMESTAMP, $2)
		`, migration.Version, migration.Description)

		if err != nil {
			return fmt.Errorf("failed to update schema version: %w", err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Sync synchronizes database to match schema file
func (m *DefaultStateBasedMigrator) Sync(ctx context.Context, schemaPath string, dryRun bool) (*types.MigrationStatus, error) {
	status := &types.MigrationStatus{
		TotalMigrations:   0,
		ExecutedCount:     0,
		PendingCount:      0,
		FailedCount:       0,
		PendingMigrations: make([]types.MigrationRecord, 0),
		FailedMigrations:  make([]types.MigrationRecord, 0),
	}

	// 1. Create migration plan
	migration, err := m.Plan(ctx, schemaPath)
	if err != nil {
		status.FailedCount = 1
		return status, fmt.Errorf("failed to create migration plan: %w", err)
	}

	// 2. No changes needed
	if migration == nil {
		return status, nil
	}

	status.TotalMigrations = 1
	status.PendingCount = 1
	status.PendingMigrations = []types.MigrationRecord{
		{
			ID:          migration.ID,
			Version:     migration.Version,
			Description: migration.Description,
		},
	}

	// 3. Dry run - just return the plan
	if dryRun {
		return status, nil
	}

	// 4. Check if auto-apply is allowed
	if m.config.AutoApply && migration.SafeToAutoApply {
		// Auto-apply safe migration
		err = m.Apply(ctx, migration)
		if err != nil {
			status.FailedCount = 1
			status.PendingCount = 0
			status.FailedMigrations = status.PendingMigrations
			status.PendingMigrations = nil
			return status, fmt.Errorf("failed to apply migration: %w", err)
		}

		// Move to executed
		status.ExecutedCount = 1
		status.PendingCount = 0
		status.PendingMigrations = nil
	} else {
		// Require manual approval
		if m.config.RequireReview {
			return status, fmt.Errorf("migration requires manual review and approval")
		}

		// Apply with confirmation
		err = m.Apply(ctx, migration)
		if err != nil {
			status.FailedCount = 1
			status.PendingCount = 0
			status.FailedMigrations = status.PendingMigrations
			status.PendingMigrations = nil
			return status, fmt.Errorf("failed to apply migration: %w", err)
		}

		status.ExecutedCount = 1
		status.PendingCount = 0
		status.PendingMigrations = nil
	}

	return status, nil
}

// Preview shows what would change without applying
func (m *DefaultStateBasedMigrator) Preview(ctx context.Context, schemaPath string) (*SchemaDiff, error) {
	if schemaPath == "" {
		schemaPath = m.config.SchemaPath
	}

	// 1. Parse desired schema
	desired, err := m.parser.Parse(ctx, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema file: %w", err)
	}

	// 2. Extract current schema
	actual, err := m.extractor.ExtractSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract current schema: %w", err)
	}

	// 3. Calculate differences
	diff, err := m.differ.Diff(ctx, desired, actual)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate schema diff: %w", err)
	}

	return diff, nil
}

// Export exports current database schema to file
func (m *DefaultStateBasedMigrator) Export(ctx context.Context, outputPath string, format SchemaFormat) error {
	if outputPath == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	// 1. Extract current schema
	schema, err := m.extractor.ExtractSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to extract schema: %w", err)
	}

	// 2. Set version from database
	version, err := m.extractor.GetVersion(ctx)
	if err == nil && version != "" {
		schema.Version = version
	}

	// 3. Serialize to file
	serializer := NewDefaultSchemaSerializer()
	err = serializer.SerializeToFile(ctx, schema, outputPath, format)
	if err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	return nil
}

// Import imports schema from file and applies changes
func (m *DefaultStateBasedMigrator) Import(ctx context.Context, schemaPath string, autoApply bool) (*GeneratedMigration, error) {
	// 1. Create migration plan
	migration, err := m.Plan(ctx, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration plan: %w", err)
	}

	// 2. No changes needed
	if migration == nil {
		return nil, nil
	}

	// 3. Save migration file
	if m.config.AutoGenerate {
		filepath, err := m.generator.GenerateFile(ctx, migration, m.config.MigrationOutputDir)
		if err != nil {
			return migration, fmt.Errorf("failed to save migration file: %w", err)
		}

		migration.SourceSchema = filepath
	}

	// 4. Apply if requested
	if autoApply {
		err = m.Apply(ctx, migration)
		if err != nil {
			return migration, fmt.Errorf("failed to apply migration: %w", err)
		}
	}

	return migration, nil
}

// FormatDiff formats schema diff as human-readable text
func (m *DefaultStateBasedMigrator) FormatDiff(diff *SchemaDiff) string {
	if diff == nil || !diff.HasChanges {
		return "No changes detected"
	}

	var output string

	output += fmt.Sprintf("Schema Changes Summary:\n")
	output += fmt.Sprintf("  Total changes: %d\n", diff.Summary.TotalChanges)
	output += fmt.Sprintf("  Safe to apply: %t\n\n", diff.Summary.SafeToApply)

	if len(diff.TablesAdded) > 0 {
		output += fmt.Sprintf("Tables Added (%d):\n", len(diff.TablesAdded))
		for _, table := range diff.TablesAdded {
			output += fmt.Sprintf("  + %s (%d columns)\n", table.Name, len(table.Columns))
		}
		output += "\n"
	}

	if len(diff.TablesDropped) > 0 {
		output += fmt.Sprintf("Tables Dropped (%d):\n", len(diff.TablesDropped))
		for _, table := range diff.TablesDropped {
			output += fmt.Sprintf("  - %s ⚠️ DATA LOSS\n", table.Name)
		}
		output += "\n"
	}

	if len(diff.TablesModified) > 0 {
		output += fmt.Sprintf("Tables Modified (%d):\n", len(diff.TablesModified))
		for _, tableDiff := range diff.TablesModified {
			output += fmt.Sprintf("  ~ %s\n", tableDiff.TableName)

			for _, col := range tableDiff.ColumnsAdded {
				output += fmt.Sprintf("    + column: %s (%s)\n", col.Name, col.Type)
			}

			for _, col := range tableDiff.ColumnsDropped {
				output += fmt.Sprintf("    - column: %s ⚠️ DATA LOSS\n", col.Name)
			}

			for _, colDiff := range tableDiff.ColumnsModified {
				output += fmt.Sprintf("    ~ column: %s (%s)\n", colDiff.ColumnName, colDiff.Changes[0])
			}
		}
		output += "\n"
	}

	if diff.Summary.DestructiveChanges > 0 {
		output += fmt.Sprintf("⚠️  Warning: %d destructive change(s) detected\n", diff.Summary.DestructiveChanges)
		destructive := m.differ.GetDestructiveChanges(context.Background(), diff)
		for _, change := range destructive {
			output += fmt.Sprintf("  - %s\n", change)
		}
	}

	return output
}

// ValidateSchemaFile validates a schema file without applying
func (m *DefaultStateBasedMigrator) ValidateSchemaFile(ctx context.Context, schemaPath string) (*SchemaValidationResult, error) {
	// Parse schema
	schema, err := m.parser.Parse(ctx, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Validate
	result, err := m.validator.ValidateSchema(ctx, schema)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Check naming conventions
	namingWarnings := m.validator.CheckNamingConventions(ctx, schema)
	result.Warnings = append(result.Warnings, namingWarnings...)

	// Check best practices
	bestPractices := m.validator.CheckBestPractices(ctx, schema)
	result.Warnings = append(result.Warnings, bestPractices...)

	return result, nil
}

// GenerateMigrationFile generates migration file from schema without applying
func (m *DefaultStateBasedMigrator) GenerateMigrationFile(ctx context.Context, schemaPath string) (string, error) {
	// Create migration plan
	migration, err := m.Plan(ctx, schemaPath)
	if err != nil {
		return "", fmt.Errorf("failed to create migration plan: %w", err)
	}

	if migration == nil {
		return "", fmt.Errorf("no changes to generate migration for")
	}

	// Ensure output directory exists
	if err := os.MkdirAll(m.config.MigrationOutputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate file
	filepath, err := m.generator.GenerateFile(ctx, migration, m.config.MigrationOutputDir)
	if err != nil {
		return "", fmt.Errorf("failed to generate migration file: %w", err)
	}

	return filepath, nil
}

// CheckSync checks if database is in sync with schema file
func (m *DefaultStateBasedMigrator) CheckSync(ctx context.Context, schemaPath string) (bool, error) {
	// Get diff
	diff, err := m.Preview(ctx, schemaPath)
	if err != nil {
		return false, err
	}

	// In sync if no changes
	return !diff.HasChanges, nil
}

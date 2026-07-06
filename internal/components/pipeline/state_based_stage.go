/**
 * @file state_based_stage.go
 * @description Pipeline stage for state-based migrations
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/pipeline/state_based_stage.go
 */

package pipeline

import (
	"context"
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/state"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// StateBasedMigrationStage handles state-based (declarative) migrations
type StateBasedMigrationStage struct {
	config   *state.StateBasedConfig
	migrator state.StateBasedMigrator
}

// NewStateBasedMigrationStage creates a new state-based migration stage
func NewStateBasedMigrationStage(config *state.StateBasedConfig, migrator state.StateBasedMigrator) *StateBasedMigrationStage {
	return &StateBasedMigrationStage{
		config:   config,
		migrator: migrator,
	}
}

// Name returns the stage name
func (s *StateBasedMigrationStage) Name() string {
	return "state-based-migration"
}

// Run executes the state-based migration stage
func (s *StateBasedMigrationStage) Run(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error {
	// Skip if not enabled
	if !s.config.Enabled {
		return nil
	}

	// Skip if no schema path configured
	if s.config.SchemaPath == "" {
		return nil
	}

	// 1. Plan migration from schema file
	migration, err := s.migrator.Plan(ctx, s.config.SchemaPath)
	if err != nil {
		pctx.ValidationErrors = append(pctx.ValidationErrors, fmt.Sprintf("state-based migration planning failed: %v", err))
		pctx.HadError = true
		return fmt.Errorf("failed to plan state-based migration: %w", err)
	}

	// 2. No changes needed
	if migration == nil {
		return nil
	}

	// 3. Convert to standard migration format
	standardMigration := &types.Migration{
		ID:          migration.ID,
		Version:     migration.Version,
		Description: "State-based migration: " + migration.ID,
		Type:        "sql",
		Script:      migration.UpSQL,
		RollbackSQL: migration.DownSQL,
		CanRollback: true,
	}

	// 4. Add to pending migrations
	if pctx.Migrations == nil {
		pctx.Migrations = make([]*types.Migration, 0)
	}

	// Check if auto-apply is allowed
	if s.config.AutoApply && migration.SafeToAutoApply {
		// Add to migrations to be executed
		pctx.Migrations = append(pctx.Migrations, standardMigration)
	} else {
		// Add to pending (requires manual approval)
		if pctx.Pending == nil {
			pctx.Pending = make([]*types.Migration, 0)
		}
		pctx.Pending = append(pctx.Pending, standardMigration)

		// Add validation warning
		if !migration.SafeToAutoApply {
			pctx.ValidationErrors = append(pctx.ValidationErrors,
				fmt.Sprintf("state-based migration '%s' contains destructive changes and requires manual approval", migration.ID))
		}

		// If require review is set, block execution
		if s.config.RequireReview {
			return fmt.Errorf("state-based migration requires manual review and approval")
		}
	}

	return nil
}

// PreviewStateBasedChanges returns a preview of what would change
func PreviewStateBasedChanges(ctx context.Context, config *state.StateBasedConfig, migrator state.StateBasedMigrator) (string, error) {
	if !config.Enabled || config.SchemaPath == "" {
		return "State-based migrations not enabled", nil
	}

	// Get diff
	diff, err := migrator.Preview(ctx, config.SchemaPath)
	if err != nil {
		return "", fmt.Errorf("failed to preview changes: %w", err)
	}

	// No changes
	if !diff.HasChanges {
		return "✓ Database is in sync with schema file", nil
	}

	// Format diff
	var output string
	output += fmt.Sprintf("State-Based Migration Preview:\n\n")
	output += fmt.Sprintf("Schema File: %s\n", config.SchemaPath)
	output += fmt.Sprintf("Total Changes: %d\n", diff.Summary.TotalChanges)
	output += fmt.Sprintf("Safe to Auto-Apply: %t\n\n", diff.Summary.SafeToApply)

	if len(diff.TablesAdded) > 0 {
		output += fmt.Sprintf("Tables to Add (%d):\n", len(diff.TablesAdded))
		for _, table := range diff.TablesAdded {
			output += fmt.Sprintf("  + %s\n", table.Name)
		}
		output += "\n"
	}

	if len(diff.TablesDropped) > 0 {
		output += fmt.Sprintf("Tables to Drop (%d): ⚠️  DATA LOSS\n", len(diff.TablesDropped))
		for _, table := range diff.TablesDropped {
			output += fmt.Sprintf("  - %s\n", table.Name)
		}
		output += "\n"
	}

	if len(diff.TablesModified) > 0 {
		output += fmt.Sprintf("Tables to Modify (%d):\n", len(diff.TablesModified))
		for _, tableDiff := range diff.TablesModified {
			output += fmt.Sprintf("  ~ %s\n", tableDiff.TableName)

			if len(tableDiff.ColumnsAdded) > 0 {
				output += fmt.Sprintf("    Columns to add: %d\n", len(tableDiff.ColumnsAdded))
			}
			if len(tableDiff.ColumnsDropped) > 0 {
				output += fmt.Sprintf("    Columns to drop: %d ⚠️  DATA LOSS\n", len(tableDiff.ColumnsDropped))
			}
			if len(tableDiff.ColumnsModified) > 0 {
				output += fmt.Sprintf("    Columns to modify: %d\n", len(tableDiff.ColumnsModified))
			}
		}
		output += "\n"
	}

	if diff.Summary.DestructiveChanges > 0 {
		output += fmt.Sprintf("⚠️  WARNING: %d destructive change(s) detected\n", diff.Summary.DestructiveChanges)
		output += "Manual review recommended before applying\n"
	}

	return output, nil
}

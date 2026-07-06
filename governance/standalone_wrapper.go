/**
 * @file standalone_wrapper.go
 * @description Governance wrapper for standalone migration tool
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/standalone_wrapper.go
 */

package governance

import (
	"context"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// GovernanceStandaloneTool wraps a standalone migration tool with governance
type GovernanceStandaloneTool struct {
	wrapped           types.StandaloneMigrationTool
	governanceManager *GovernanceManager
	currentUser       string
	currentUserName   string
	logger            logging.ComponentLogger
}

// NewGovernanceStandaloneTool creates a new governance-wrapped standalone tool
func NewGovernanceStandaloneTool(
	tool types.StandaloneMigrationTool,
	manager *GovernanceManager,
	userID string,
	userName string,
) *GovernanceStandaloneTool {
	return &GovernanceStandaloneTool{
		wrapped:           tool,
		governanceManager: manager,
		currentUser:       userID,
		currentUserName:   userName,
		logger:            logging.GetComponentLogger("governance-standalone"),
	}
}

// Status returns migration status
func (t *GovernanceStandaloneTool) Status(ctx context.Context) (*types.MigrationStatus, error) {
	return t.wrapped.Status(ctx)
}

// Migrate runs pending migrations with governance checks
func (t *GovernanceStandaloneTool) Migrate(ctx context.Context, target string, dryRun bool) error {
	t.logger.WithContext(ctx).Info(fmt.Sprintf("Starting migration with governance - user: %s, target: %s, dry_run: %v", t.currentUser, target, dryRun))

	// Get pending migrations
	status, err := t.wrapped.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	// Validate each pending migration against policies
	for _, pending := range status.PendingMigrations {
		migration := &types.Migration{
			ID:          pending.ID,
			Version:     pending.Version,
			Description: pending.Description,
			Script:      pending.Script,
			Author:      "", // MigrationRecord doesn't have Author field
			CanRollback: pending.CanRollback,
			RollbackSQL: pending.RollbackSQL,
		}

		// Policy validation
		t.logger.WithContext(ctx).Info(fmt.Sprintf("Validating migration against policies: %s", migration.ID))
		policyResult, err := t.governanceManager.ValidateMigration(ctx, migration)
		if err != nil {
			return fmt.Errorf("policy validation failed for migration %s: %w", migration.ID, err)
		}

		if !policyResult.Passed {
			t.logger.WithContext(ctx).Info(fmt.Sprintf("Migration %s has %d policy violations", migration.ID, len(policyResult.Violations)))

			if policyResult.Action == PolicyActionBlock {
				return fmt.Errorf("migration %s blocked by policy - %d violations", migration.ID, len(policyResult.Violations))
			}

			if policyResult.RequiresApproval {
				approved, err := t.governanceManager.CheckMigrationApproval(ctx, migration.ID)
				if err != nil {
					return fmt.Errorf("failed to check approval for migration %s: %w", migration.ID, err)
				}

				if !approved {
					return fmt.Errorf("migration %s requires approval - use 'approve' command", migration.ID)
				}

				t.logger.WithContext(ctx).Info(fmt.Sprintf("Migration %s approved - proceeding", migration.ID))
			}
		}

		// Permission check
		if !dryRun {
			err = t.governanceManager.ExecuteMigration(ctx, migration, t.currentUser, t.currentUserName)
			if err != nil {
				return fmt.Errorf("governance check failed for migration %s: %w", migration.ID, err)
			}
		}
	}

	t.logger.WithContext(ctx).Info("All governance checks passed - executing migrations")

	// Execute with wrapped tool
	err = t.wrapped.Migrate(ctx, target, dryRun)
	if err != nil {
		return err
	}

	// Record successful execution in audit trail
	if !dryRun {
		for _, pending := range status.PendingMigrations {
			migration := &types.Migration{ID: pending.ID}
			if err := t.governanceManager.complianceReporter.RecordMigrationAudit(
				ctx,
				migration.ID,
				"migration_executed",
				t.currentUser,
				t.currentUserName,
				true,
				"Migration completed successfully",
			); err != nil {
				t.logger.WithContext(ctx).Error("Failed to record audit entry", err)
			}
		}
	}

	return nil
}

// Rollback rolls back migrations with governance checks
func (t *GovernanceStandaloneTool) Rollback(ctx context.Context, steps int) error {
	t.logger.WithContext(ctx).Info(fmt.Sprintf("Starting rollback with governance - user: %s, steps: %d", t.currentUser, steps))

	// Get applied migrations to determine which will be rolled back
	status, err := t.wrapped.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	// Check rollback permission for the most recent migration
	if status.LastMigration != nil {
		migration := &types.Migration{
			ID:          status.LastMigration.ID,
			Version:     status.LastMigration.Version,
			Description: status.LastMigration.Description,
		}

		err = t.governanceManager.RollbackMigration(ctx, migration, t.currentUser, t.currentUserName)
		if err != nil {
			return fmt.Errorf("rollback permission denied: %w", err)
		}
	}

	t.logger.WithContext(ctx).Info("Governance checks passed - executing rollback")

	// Execute rollback
	err = t.wrapped.Rollback(ctx, steps)
	if err != nil {
		return err
	}

	// Record in audit trail
	if err := t.governanceManager.complianceReporter.RecordMigrationAudit(
		ctx,
		"rollback",
		"migration_rolledback",
		t.currentUser,
		t.currentUserName,
		true,
		fmt.Sprintf("Rolled back %d step(s)", steps),
	); err != nil {
		t.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return nil
}

// Validate validates all migrations
func (t *GovernanceStandaloneTool) Validate(ctx context.Context) error {
	return t.wrapped.Validate(ctx)
}

// Info returns migration information
func (t *GovernanceStandaloneTool) Info(ctx context.Context) (*types.MigrationInfo, error) {
	return t.wrapped.Info(ctx)
}

// Close closes the migration tool
func (t *GovernanceStandaloneTool) Close() error {
	return t.wrapped.Close()
}

// LoadConfig loads configuration
func (t *GovernanceStandaloneTool) LoadConfig(configPath string) error {
	return t.wrapped.LoadConfig(configPath)
}

// SaveConfig saves configuration
func (t *GovernanceStandaloneTool) SaveConfig(configPath string) error {
	return t.wrapped.SaveConfig(configPath)
}

// GetConfig returns configuration
func (t *GovernanceStandaloneTool) GetConfig() *types.StandaloneConfig {
	return t.wrapped.GetConfig()
}

// SetEnvironment sets environment configuration
func (t *GovernanceStandaloneTool) SetEnvironment(env *types.EnvironmentConfig) {
	t.wrapped.SetEnvironment(env)
}

// GetEnvironment returns environment configuration
func (t *GovernanceStandaloneTool) GetEnvironment() *types.EnvironmentConfig {
	return t.wrapped.GetEnvironment()
}

// SetPlugins sets plugin configurations
func (t *GovernanceStandaloneTool) SetPlugins(plugins []types.PluginConfig) {
	t.wrapped.SetPlugins(plugins)
}

// GetPlugins returns plugin configurations
func (t *GovernanceStandaloneTool) GetPlugins() []types.PluginConfig {
	return t.wrapped.GetPlugins()
}

// GetGovernanceManager returns the governance manager for external access
func (t *GovernanceStandaloneTool) GetGovernanceManager() *GovernanceManager {
	return t.governanceManager
}
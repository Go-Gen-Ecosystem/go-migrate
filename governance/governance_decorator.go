/**
 * @file governance_decorator.go
 * @description Pipeline decorator for integrating governance into migration execution
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/governance_decorator.go
 */

package governance

import (
	"context"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// GovernanceExecutorDecorator wraps a migration executor with governance checks
type GovernanceExecutorDecorator struct {
	wrapped          types.MigrationExecutor
	governanceManager *GovernanceManager
	currentUserID    string
	currentUserName  string
	logger           logging.ComponentLogger
}

// NewGovernanceExecutorDecorator creates a new governance executor decorator
func NewGovernanceExecutorDecorator(executor types.MigrationExecutor, manager *GovernanceManager, userID string, userName string) *GovernanceExecutorDecorator {
	return &GovernanceExecutorDecorator{
		wrapped:          executor,
		governanceManager: manager,
		currentUserID:    userID,
		currentUserName:  userName,
		logger:           logging.GetComponentLogger("governance-executor"),
	}
}

// Execute executes a migration with governance checks
func (d *GovernanceExecutorDecorator) Execute(ctx context.Context, migration *types.Migration) error {
	d.logger.WithContext(ctx).Info("Executing migration with governance",
		"migration_id", migration.ID,
		"user_id", d.currentUserID)

	// Perform governance checks before execution
	if err := d.governanceManager.ExecuteMigration(ctx, migration, d.currentUserID, d.currentUserName); err != nil {
		return fmt.Errorf("governance check failed: %w", err)
	}

	// Execute the migration
	err := d.wrapped.Execute(ctx, migration)

	// Record audit entry for execution result
	success := err == nil
	details := "Migration executed successfully"
	if !success {
		details = fmt.Sprintf("Migration execution failed: %v", err)
	}

	if auditErr := d.governanceManager.complianceReporter.RecordMigrationAudit(
		ctx, migration.ID, "migration_executed", d.currentUserID, d.currentUserName, success, details,
	); auditErr != nil {
		d.logger.WithContext(ctx).Error("Failed to record audit entry", auditErr)
	}

	return err
}

// ExecuteDryRun executes a dry run of a migration
func (d *GovernanceExecutorDecorator) ExecuteDryRun(ctx context.Context, migration *types.Migration) (*types.ExecutionPlan, error) {
	// Dry runs don't require governance approval
	return d.wrapped.ExecuteDryRun(ctx, migration)
}

// Rollback rolls back a migration with governance checks
func (d *GovernanceExecutorDecorator) Rollback(ctx context.Context, migration *types.Migration) error {
	d.logger.WithContext(ctx).Info("Rolling back migration with governance",
		"migration_id", migration.ID,
		"user_id", d.currentUserID)

	// Perform governance checks before rollback
	if err := d.governanceManager.RollbackMigration(ctx, migration, d.currentUserID, d.currentUserName); err != nil {
		return fmt.Errorf("governance check failed: %w", err)
	}

	// Execute the rollback
	err := d.wrapped.Rollback(ctx, migration)

	// Record audit entry for rollback result
	success := err == nil
	details := "Migration rolled back successfully"
	if !success {
		details = fmt.Sprintf("Migration rollback failed: %v", err)
	}

	if auditErr := d.governanceManager.complianceReporter.RecordMigrationAudit(
		ctx, migration.ID, "migration_rolledback", d.currentUserID, d.currentUserName, success, details,
	); auditErr != nil {
		d.logger.WithContext(ctx).Error("Failed to record audit entry", auditErr)
	}

	return err
}

// ValidateRollback validates that a migration can be rolled back
func (d *GovernanceExecutorDecorator) ValidateRollback(ctx context.Context, migration *types.Migration) error {
	return d.wrapped.ValidateRollback(ctx, migration)
}

// ValidateMigration validates a migration
func (d *GovernanceExecutorDecorator) ValidateMigration(ctx context.Context, migration *types.Migration) error {
	// Add policy validation on top of regular validation
	policyResult, err := d.governanceManager.ValidateMigration(ctx, migration)
	if err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// Check if blocked by policy
	if policyResult.Action == PolicyActionBlock {
		return fmt.Errorf("migration blocked by policy: %d violations", len(policyResult.Violations))
	}

	// Execute wrapped validation
	return d.wrapped.ValidateMigration(ctx, migration)
}

// GetExecutionPlan gets the execution plan for migrations
func (d *GovernanceExecutorDecorator) GetExecutionPlan(migrations []*types.Migration) ([]string, error) {
	return d.wrapped.GetExecutionPlan(migrations)
}

// GovernanceValidatorDecorator wraps a migration validator with policy checks
type GovernanceValidatorDecorator struct {
	wrapped          types.MigrationValidator
	governanceManager *GovernanceManager
	logger           logging.ComponentLogger
}

// NewGovernanceValidatorDecorator creates a new governance validator decorator
func NewGovernanceValidatorDecorator(validator types.MigrationValidator, manager *GovernanceManager) *GovernanceValidatorDecorator {
	return &GovernanceValidatorDecorator{
		wrapped:          validator,
		governanceManager: manager,
		logger:           logging.GetComponentLogger("governance-validator"),
	}
}

// ValidateMigrations validates multiple migrations with policy checks
func (d *GovernanceValidatorDecorator) ValidateMigrations(migrations []types.Migration) error {
	ctx := context.Background()

	// First, run standard validation
	if err := d.wrapped.ValidateMigrations(migrations); err != nil {
		return err
	}

	// Then, validate each migration against policies
	for i := range migrations {
		policyResult, err := d.governanceManager.ValidateMigration(ctx, &migrations[i])
		if err != nil {
			return fmt.Errorf("policy validation failed for migration %s: %w", migrations[i].ID, err)
		}

		// In strict mode or if blocked, fail validation
		if policyResult.Action == PolicyActionBlock {
			return fmt.Errorf("migration %s blocked by policy: %d violations", migrations[i].ID, len(policyResult.Violations))
		}
	}

	return nil
}

// ValidateMigration validates a single migration with policy checks
func (d *GovernanceValidatorDecorator) ValidateMigration(migration *types.Migration) error {
	ctx := context.Background()

	// First, run standard validation
	if err := d.wrapped.ValidateMigration(migration); err != nil {
		return err
	}

	// Then, validate against policies
	policyResult, err := d.governanceManager.ValidateMigration(ctx, migration)
	if err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// In strict mode or if blocked, fail validation
	if policyResult.Action == PolicyActionBlock {
		return fmt.Errorf("migration blocked by policy: %d violations", len(policyResult.Violations))
	}

	return nil
}

// CheckDependencies checks migration dependencies
func (d *GovernanceValidatorDecorator) CheckDependencies(migrations []types.Migration) error {
	return d.wrapped.CheckDependencies(migrations)
}

// CheckCircularDependencies checks for circular dependencies
func (d *GovernanceValidatorDecorator) CheckCircularDependencies(migrations []types.Migration) error {
	return d.wrapped.CheckCircularDependencies(migrations)
}

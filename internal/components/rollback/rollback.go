/**
 * @file rollback.go
 * @description Migration rollback management implementations
 * @author Admilson B. F. Cossa
 * @date 2025-01-22
 * @path pkg/core/database/migrations/internal/rollback/rollback.go
 */

package rollback

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/constants"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// RollbackManager interface for managing rollbacks
type RollbackManager interface {
	CanRollback(ctx context.Context, migrationID string) (bool, []string, error)
	RollbackToVersion(ctx context.Context, version string) (*types.RollbackResult, error)
	RollbackCount(ctx context.Context, count int) (*types.RollbackResult, error)
	RollbackMigration(ctx context.Context, migrationID string) (*types.RollbackResult, error)
	GetRollbackPlan(ctx context.Context, migrationIDs []string) (*types.RollbackPlan, error)

	// Advanced capabilities - optional, can be nil if not supported
	GetAdvancedManager() AdvancedRollbackManager
}

// DefaultRollbackManager implements RollbackManager with comprehensive rollback capabilities
type DefaultRollbackManager struct {
	tracker         tracker.MigrationTracker
	executor        types.MigrationExecutor
	validator       types.MigrationValidator
	config          *RollbackConfig
	advancedManager AdvancedRollbackManager
}

// RollbackConfig contains configuration for rollback operations
type RollbackConfig struct {
	// Maximum number of migrations to rollback in a single operation
	MaxRollbackCount int

	// Whether to validate rollback scripts before execution
	ValidateBeforeRollback bool

	// Whether to create backup before rollback
	CreateBackup bool

	// Backup directory path
	BackupDirectory string

	// Whether to use transactions for rollback operations
	UseTransactions bool

	// Timeout for rollback operations
	Timeout time.Duration

	// Whether to stop on first rollback error
	StopOnError bool

	// Whether to dry run rollback operations
	DryRun bool

	// Custom rollback hooks
	PreRollbackHooks  []RollbackHook
	PostRollbackHooks []RollbackHook
}

// RollbackHook represents a function that can be executed before or after rollback
type RollbackHook func(ctx context.Context, migration *types.Migration) error

// NewDefaultRollbackManager creates a new default rollback manager
func NewDefaultRollbackManager(
	tracker tracker.MigrationTracker,
	executor types.MigrationExecutor,
	validator types.MigrationValidator,
	config *RollbackConfig,
) *DefaultRollbackManager {
	if config == nil {
		config = &RollbackConfig{
			MaxRollbackCount:       10,
			ValidateBeforeRollback: true,
			CreateBackup:           true,
			UseTransactions:        true,
			Timeout:                30 * time.Minute,
			StopOnError:            true,
			DryRun:                 false,
		}
	}

	manager := &DefaultRollbackManager{
		tracker:   tracker,
		executor:  executor,
		validator: validator,
		config:    config,
	}

	// Note: Advanced manager will be initialized separately via SetAdvancedManager
	// to avoid circular dependencies during initialization

	return manager
}

// SetAdvancedManager sets the advanced rollback manager
func (r *DefaultRollbackManager) SetAdvancedManager(advancedManager AdvancedRollbackManager) {
	r.advancedManager = advancedManager
}

// GetAdvancedManager returns the advanced rollback manager
func (r *DefaultRollbackManager) GetAdvancedManager() AdvancedRollbackManager {
	return r.advancedManager
}

// RollbackToVersion rolls back migrations to a specific version
func (r *DefaultRollbackManager) RollbackToVersion(ctx context.Context, version string) (*types.RollbackResult, error) {
	// Get target migration
	targetMigration, err := r.tracker.GetMigrationByVersion(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("failed to find target migration version %s: %w", version, err)
	}

	// Get applied migrations
	appliedMigrations, err := r.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Find migrations to rollback (those applied after the target version)
	migrationsToRollback := r.findMigrationsToRollback(appliedMigrations, targetMigration)

	// Create rollback plan
	plan, err := r.createRollbackPlan(ctx, migrationsToRollback)
	if err != nil {
		return nil, fmt.Errorf("failed to create rollback plan: %w", err)
	}

	if !plan.CanProceed {
		return &types.RollbackResult{
			Success:  false,
			Errors:   []string{"rollback plan validation failed"},
			Warnings: plan.Warnings,
		}, nil
	}

	// Execute rollback
	return r.executeRollbackPlan(ctx, plan)
}

// RollbackCount rolls back a specific number of migrations
func (r *DefaultRollbackManager) RollbackCount(ctx context.Context, count int) (*types.RollbackResult, error) {
	if count <= 0 {
		return nil, fmt.Errorf("rollback count must be positive")
	}

	if count > r.config.MaxRollbackCount {
		return nil, fmt.Errorf("rollback count %d exceeds maximum allowed %d", count, r.config.MaxRollbackCount)
	}

	// Get applied migrations
	appliedMigrations, err := r.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Sort by execution time (most recent first)
	sort.Slice(appliedMigrations, func(i, j int) bool {
		if appliedMigrations[i].ExecutedAt == nil {
			return false
		}
		if appliedMigrations[j].ExecutedAt == nil {
			return true
		}
		return appliedMigrations[i].ExecutedAt.After(*appliedMigrations[j].ExecutedAt)
	})

	// Take the requested number of migrations
	if count > len(appliedMigrations) {
		count = len(appliedMigrations)
	}

	migrationsToRollback := appliedMigrations[:count]

	// Create rollback plan
	plan, err := r.createRollbackPlan(ctx, migrationsToRollback)
	if err != nil {
		return nil, fmt.Errorf("failed to create rollback plan: %w", err)
	}

	if !plan.CanProceed {
		return &types.RollbackResult{
			Success:  false,
			Errors:   []string{"rollback plan validation failed"},
			Warnings: plan.Warnings,
		}, nil
	}

	// Execute rollback
	return r.executeRollbackPlan(ctx, plan)
}

// RollbackMigration rolls back a specific migration
func (r *DefaultRollbackManager) RollbackMigration(ctx context.Context, migrationID string) (*types.RollbackResult, error) {
	// Get migration
	migration, err := r.tracker.GetMigration(ctx, migrationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get migration %s: %w", migrationID, err)
	}

	// Check if migration is applied
	isApplied, err := r.tracker.IsMigrationApplied(ctx, migrationID)
	if err != nil {
		return nil, fmt.Errorf("failed to check migration status: %w", err)
	}

	if !isApplied {
		return &types.RollbackResult{
			Success:      false,
			MigrationID:  migrationID,
			ErrorMessage: fmt.Sprintf("migration %s is not applied", migrationID),
			Duration:     0,
			RolledBackAt: time.Now(),
		}, nil
	}

	// Check if migration can be rolled back
	canRollback, reasons, err := r.CanRollback(ctx, migrationID)
	if err != nil {
		return &types.RollbackResult{
			Success:      false,
			MigrationID:  migrationID,
			ErrorMessage: fmt.Sprintf("failed to check rollback capability: %v", err),
			Duration:     0,
			RolledBackAt: time.Now(),
		}, nil
	}

	if !canRollback {
		return &types.RollbackResult{
			Success:      false,
			MigrationID:  migrationID,
			ErrorMessage: fmt.Sprintf("migration cannot be rolled back: %s", strings.Join(reasons, ", ")),
			Duration:     0,
			RolledBackAt: time.Now(),
		}, nil
	}

	// Validate migration before rollback
	if r.config.ValidateBeforeRollback {
		if err := r.validator.ValidateMigration(migration); err != nil {
			return &types.RollbackResult{
				Success:      false,
				MigrationID:  migrationID,
				ErrorMessage: fmt.Sprintf("migration validation failed: %v", err),
				Duration:     0,
				RolledBackAt: time.Now(),
			}, nil
		}
	}

	// Create rollback plan
	plan, err := r.createRollbackPlan(ctx, []*types.Migration{migration})
	if err != nil {
		return &types.RollbackResult{
			Success:      false,
			MigrationID:  migrationID,
			ErrorMessage: fmt.Sprintf("failed to create rollback plan: %v", err),
			Duration:     0,
			RolledBackAt: time.Now(),
		}, nil
	}

	if !plan.CanProceed {
		return &types.RollbackResult{
			Success:      false,
			MigrationID:  migrationID,
			ErrorMessage: "rollback plan validation failed",
			Duration:     0,
			RolledBackAt: time.Now(),
		}, nil
	}

	// Execute rollback
	return r.executeRollbackPlan(ctx, plan)
}

// CanRollback checks if a migration can be rolled back
func (r *DefaultRollbackManager) CanRollback(ctx context.Context, migrationID string) (bool, []string, error) {

	// Get migration
	migration, err := r.tracker.GetMigration(ctx, migrationID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get migration %s: %w", migrationID, err)
	}

	var warnings []string

	// Check if migration is applied
	isApplied, err := r.tracker.IsMigrationApplied(ctx, migrationID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to check migration status: %w", err)
	}

	if !isApplied {
		warnings = append(warnings, "Migration is not applied")
		return false, warnings, nil
	}

	// Check if migration can be rolled back
	if !migration.CanRollback {
		warnings = append(warnings, "Migration is marked as non-rollbackable")
		return false, warnings, nil
	}

	// Check if rollback SQL is available
	if migration.RollbackSQL == "" {
		warnings = append(warnings, "No rollback SQL available")
		return false, warnings, nil
	}

	// Check for dependent migrations
	dependentMigrations, err := r.findDependentMigrations(ctx, migration)
	if err != nil {
		return false, nil, fmt.Errorf("failed to find dependent migrations: %w", err)
	}

	if len(dependentMigrations) > 0 {
		var dependentIDs []string
		for _, dep := range dependentMigrations {
			dependentIDs = append(dependentIDs, dep.ID)
		}
		warnings = append(warnings, fmt.Sprintf("Has dependent migrations: %s", strings.Join(dependentIDs, ", ")))
		return false, warnings, nil
	}

	// Validate rollback SQL if configured
	if r.config.ValidateBeforeRollback && r.validator != nil {
		rollbackMigration := &types.Migration{
			ID:     migration.ID + "_rollback",
			Script: migration.RollbackSQL,
			Type:   constants.MigrationTypeRollback,
		}

		if err := r.validator.ValidateMigration(rollbackMigration); err != nil {
			warnings = append(warnings, fmt.Sprintf("Rollback validation failed: %s", err.Error()))
			return false, warnings, nil
		}
	}

	return true, warnings, nil
}

// GetRollbackPlan creates a rollback plan without executing it
func (r *DefaultRollbackManager) GetRollbackPlan(ctx context.Context, migrationIDs []string) (*types.RollbackPlan, error) {
	var migrations []*types.Migration

	for _, id := range migrationIDs {
		migration, err := r.tracker.GetMigration(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get migration %s: %w", id, err)
		}
		migrations = append(migrations, migration)
	}

	return r.createRollbackPlan(ctx, migrations)
}

// Helper methods

// findMigrationsToRollback finds migrations that need to be rolled back to reach target version
func (r *DefaultRollbackManager) findMigrationsToRollback(appliedMigrations []*types.Migration, targetMigration *types.Migration) []*types.Migration {
	var migrationsToRollback []*types.Migration

	// Sort applied migrations by execution time (most recent first)
	sort.Slice(appliedMigrations, func(i, j int) bool {
		if appliedMigrations[i].ExecutedAt == nil {
			return false
		}
		if appliedMigrations[j].ExecutedAt == nil {
			return true
		}
		return appliedMigrations[i].ExecutedAt.After(*appliedMigrations[j].ExecutedAt)
	})

	// Find migrations applied after the target migration
	for _, migration := range appliedMigrations {
		if migration.ExecutedAt != nil && targetMigration.ExecutedAt != nil {
			if migration.ExecutedAt.After(*targetMigration.ExecutedAt) {
				migrationsToRollback = append(migrationsToRollback, migration)
			}
		}
	}

	return migrationsToRollback
}

// findDependentMigrations finds migrations that depend on the given migration
func (r *DefaultRollbackManager) findDependentMigrations(ctx context.Context, migration *types.Migration) ([]*types.Migration, error) {
	// Get all applied migrations
	appliedMigrations, err := r.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	var dependentMigrations []*types.Migration

	// Find migrations that depend on this migration
	for _, applied := range appliedMigrations {
		for _, dependency := range applied.Dependencies {
			if dependency == migration.ID || dependency == migration.Version {
				dependentMigrations = append(dependentMigrations, applied)
				break
			}
		}
	}

	return dependentMigrations, nil
}

// createRollbackPlan creates a detailed rollback plan
func (r *DefaultRollbackManager) createRollbackPlan(ctx context.Context, migrations []*types.Migration) (*types.RollbackPlan, error) {
	plan := &types.RollbackPlan{
		Migrations: migrations,
		TotalCount: len(migrations),
		Warnings:   []string{},
		Errors:     []string{},
		CanProceed: true,
	}

	var estimatedTime time.Duration

	for _, migration := range migrations {
		// Check if migration can be rolled back
		canRollback, warnings, err := r.CanRollback(ctx, migration.ID)
		if err != nil {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Error checking rollback for %s: %v", migration.ID, err))
			plan.CanProceed = false
			continue
		}

		if !canRollback {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Cannot rollback migration %s", migration.ID))
			plan.CanProceed = false
		}

		plan.Warnings = append(plan.Warnings, warnings...)

		// Estimate execution time based on previous execution time
		if migration.ExecutionTime > 0 {
			// Assume rollback takes similar time as forward migration
			estimatedTime += migration.ExecutionTime
		} else {
			// Default estimate
			estimatedTime += 5 * time.Second
		}
	}

	plan.EstimatedTime = estimatedTime

	// Additional validations
	if plan.TotalCount > r.config.MaxRollbackCount {
		plan.Errors = append(plan.Errors, fmt.Sprintf("Rollback count %d exceeds maximum allowed %d", plan.TotalCount, r.config.MaxRollbackCount))
		plan.CanProceed = false
	}

	return plan, nil
}

// executeRollbackPlan executes a rollback plan
func (r *DefaultRollbackManager) executeRollbackPlan(ctx context.Context, plan *types.RollbackPlan) (*types.RollbackResult, error) {
	result := &types.RollbackResult{
		Success:      true,
		RolledBackAt: time.Now(),
		Errors:       []string{},
		Warnings:     []string{},
	}

	startTime := time.Now()

	// Create context with timeout
	if r.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.Timeout)
		defer cancel()
	}

	// Execute pre-rollback hooks
	for _, migration := range plan.Migrations {
		for _, hook := range r.config.PreRollbackHooks {
			if err := hook(ctx, migration); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("pre-rollback hook failed for %s: %v", migration.ID, err))
				if r.config.StopOnError {
					result.Success = false
					result.Duration = time.Since(startTime)
					return result, nil
				}
			}
		}
	}

	// Execute rollbacks in reverse order (most recent first)
	for _, migration := range plan.Migrations {
		if err := r.rollbackSingleMigration(ctx, migration); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to rollback %s: %v", migration.ID, err))
			result.Success = false

			if r.config.StopOnError {
				break
			}
		} else {
			// Migration rolled back successfully
		}
	}

	// Execute post-rollback hooks
	for _, migration := range plan.Migrations {
		for _, hook := range r.config.PostRollbackHooks {
			if err := hook(ctx, migration); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("post-rollback hook warning for %s: %v", migration.ID, err))
			}
		}
	}

	result.Duration = time.Since(startTime)

	return result, nil
}

// rollbackSingleMigration rolls back a single migration
func (r *DefaultRollbackManager) rollbackSingleMigration(ctx context.Context, migration *types.Migration) error {
	// Create rollback migration
	rollbackMigration := &types.Migration{
		ID:          migration.ID + "_rollback",
		Version:     migration.Version + "_rollback",
		Description: fmt.Sprintf("Rollback of %s", migration.Description),
		Script:      migration.RollbackSQL,
		Type:        constants.MigrationTypeRollback,
		Author:      migration.Author,
		CanRollback: false, // Rollback migrations themselves cannot be rolled back
	}

	// Execute rollback if not dry run
	if !r.config.DryRun {
		if err := r.executor.Execute(ctx, rollbackMigration); err != nil {
			return fmt.Errorf("failed to execute rollback: %w", err)
		}
	}

	// Update migration state
	migration.State = constants.MigrationStateRolledBack
	migration.RolledBackAt = &time.Time{}
	*migration.RolledBackAt = time.Now()

	// Record rollback in tracker
	if err := r.tracker.RecordMigration(ctx, migration); err != nil {
		return fmt.Errorf("failed to record rollback: %w", err)
	}

	// Record changelog entry
	changelog := &tracker.ChangelogEntry{
		MigrationID:   migration.ID,
		Operation:     constants.OperationRollback,
		Status:        constants.StatusSuccess,
		ExecutedBy:    migration.Author,
		ExecutedAt:    time.Now(),
		ExecutionTime: 0, // Will be updated by executor
		Context: map[string]interface{}{
			"version":     migration.Version,
			"description": migration.Description,
			"dry_run":     r.config.DryRun,
		},
	}

	if err := r.tracker.RecordChangelog(ctx, changelog); err != nil {
		// Log warning but don't fail the rollback
		result := fmt.Errorf("failed to record changelog: %w", err)
		return result
	}

	return nil
}

/**
 * @file concrete_engine.go
 * @description ConcreteMigrationEngine implementation constructed by the builder
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/builder/concrete_engine.go
 */

package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// ConcreteMigrationEngine is the concrete implementation of MigrationEngine
// It orchestrates all components to provide migration functionality
type ConcreteMigrationEngine struct {
	engineType string
	config     types.Config
	backend    types.EngineBackend
	executor   types.MigrationExecutor
	tracker    types.MigrationTracker
	lockMgr    types.LockManager
	validator  types.MigrationValidator
	resolver   types.DependencyResolver
	options    map[string]interface{}
	connected  bool
}

// Connect establishes a connection to the database
func (e *ConcreteMigrationEngine) Connect(ctx context.Context) error {
	if e.connected {
		return nil
	}

	// Acquire lock for connection
	lockID := fmt.Sprintf("connect_%s", e.engineType)
	if err := e.lockMgr.AcquireLock(ctx, lockID, 30*time.Second); err != nil {
		return fmt.Errorf("failed to acquire connection lock: %w", err)
	}
	defer func() {
		_ = e.lockMgr.ReleaseLock(ctx, lockID)
	}()

	// Connect backend
	if err := e.backend.AcquireLock(ctx); err != nil {
		return fmt.Errorf("failed to connect backend: %w", err)
	}

	e.connected = true
	return nil
}

// Disconnect closes the database connection
func (e *ConcreteMigrationEngine) Disconnect(ctx context.Context) error {
	if !e.connected {
		return nil
	}

	// Release backend lock
	if err := e.backend.ReleaseLock(ctx); err != nil {
		return fmt.Errorf("failed to disconnect backend: %w", err)
	}

	e.connected = false
	return nil
}

// Up executes migrations in the forward direction
func (e *ConcreteMigrationEngine) Up(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	startTime := time.Now()
	result := types.MigrationResult{
		ExecutedScripts: make([]types.MigrationScript, 0),
		FailedScripts:   make([]types.FailedMigrationScript, 0),
		Warnings:        make([]string, 0),
		Errors:          make([]string, 0),
	}

	if !e.connected {
		if err := e.Connect(ctx); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to connect: %v", err))
			return result, err
		}
	}

	// Acquire migration lock
	lockID := fmt.Sprintf("migration_%s", e.engineType)
	if err := e.lockMgr.AcquireLock(ctx, lockID, 5*time.Minute); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to acquire migration lock: %v", err))
		return result, err
	}
	defer func() {
		_ = e.lockMgr.ReleaseLock(ctx, lockID)
	}()

	// Validate migrations
	validationResult, err := e.validator.Validate(ctx, migrations)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation failed: %v", err))
		return result, err
	}

	if !validationResult.Valid {
		for _, validationError := range validationResult.ValidationErrors {
			result.Errors = append(result.Errors, validationError.Message)
		}
		return result, fmt.Errorf("migration validation failed")
	}

	// Resolve dependencies
	executionPlan, err := e.resolver.ResolveDependencies(ctx, migrations)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("dependency resolution failed: %v", err))
		return result, err
	}

	// Execute migrations
	for _, migration := range executionPlan.Migrations {
		if err := e.executeSingleMigration(ctx, migration, &result); err != nil {
			result.FailedCount++
			result.FailedScripts = append(result.FailedScripts, types.FailedMigrationScript{
				Script:      migration,
				Error:       err.Error(),
				ErrorCode:   "EXECUTION_FAILED",
				FailureTime: time.Now(),
				CanRetry:    true,
			})
			
			// Stop on first failure unless configured otherwise
			if !e.getBoolOption("continue_on_failure", false) {
				break
			}
		} else {
			result.ExecutedCount++
			result.ExecutedScripts = append(result.ExecutedScripts, migration)
		}
	}

	result.TotalTime = time.Since(startTime)
	result.Success = result.FailedCount == 0

	return result, nil
}

// Down executes rollback to a specific version
func (e *ConcreteMigrationEngine) Down(ctx context.Context, version string) (types.MigrationResult, error) {
	startTime := time.Now()
	result := types.MigrationResult{
		ExecutedScripts: make([]types.MigrationScript, 0),
		FailedScripts:   make([]types.FailedMigrationScript, 0),
		Warnings:        make([]string, 0),
		Errors:          make([]string, 0),
	}

	if !e.connected {
		if err := e.Connect(ctx); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to connect: %v", err))
			return result, err
		}
	}

	// Get current status
	statusList, err := e.tracker.GetStatus(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get status: %v", err))
		return result, err
	}

	// Find migrations to rollback
	migrationsToRollback := e.findMigrationsToRollback(statusList, version)
	
	// Execute rollbacks in reverse order
	for i := len(migrationsToRollback) - 1; i >= 0; i-- {
		migration := migrationsToRollback[i]
		if err := e.executeRollback(ctx, migration, &result); err != nil {
			result.FailedCount++
			result.FailedScripts = append(result.FailedScripts, types.FailedMigrationScript{
				Script:      migration,
				Error:       err.Error(),
				ErrorCode:   "ROLLBACK_FAILED",
				FailureTime: time.Now(),
				CanRetry:    false,
			})
			break // Stop on rollback failure
		} else {
			result.ExecutedCount++
			result.ExecutedScripts = append(result.ExecutedScripts, migration)
		}
	}

	result.TotalTime = time.Since(startTime)
	result.Success = result.FailedCount == 0

	return result, nil
}

// Status returns the current migration status
func (e *ConcreteMigrationEngine) Status(ctx context.Context) ([]types.MigrationStatus, error) {
	if !e.connected {
		if err := e.Connect(ctx); err != nil {
			return nil, fmt.Errorf("failed to connect: %v", err)
		}
	}

	return e.backend.ReadStatus(ctx)
}

// DryRun performs a dry run of migrations without executing them
func (e *ConcreteMigrationEngine) DryRun(ctx context.Context, migrations []types.MigrationScript) (types.DryRunResult, error) {
	result := types.DryRunResult{
		WouldExecute:     make([]types.MigrationScript, 0),
		WouldSkip:        make([]types.MigrationScript, 0),
		Warnings:         make([]string, 0),
		Errors:           make([]string, 0),
		ValidationErrors: make([]types.ValidationError, 0),
	}

	// Validate migrations
	validationResult, err := e.validator.Validate(ctx, migrations)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation failed: %v", err))
		return result, err
	}

	result.ValidationErrors = validationResult.ValidationErrors
	result.Warnings = validationResult.Warnings

	if !validationResult.Valid {
		return result, nil
	}

	// Resolve dependencies
	executionPlan, err := e.resolver.ResolveDependencies(ctx, migrations)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("dependency resolution failed: %v", err))
		return result, err
	}

	// Determine what would be executed
	for _, migration := range executionPlan.Migrations {
		// Check if migration is already applied
		if e.isMigrationApplied(ctx, migration) {
			result.WouldSkip = append(result.WouldSkip, migration)
		} else {
			result.WouldExecute = append(result.WouldExecute, migration)
		}
	}

	// Estimate execution time
	result.EstimatedTime = e.estimateExecutionTime(result.WouldExecute)

	return result, nil
}

// Validate validates migrations without executing them
func (e *ConcreteMigrationEngine) Validate(ctx context.Context, migrations []types.MigrationScript) (types.ValidationResult, error) {
	return e.validator.Validate(ctx, migrations)
}

// GetEngineType returns the engine type
func (e *ConcreteMigrationEngine) GetEngineType() string {
	return e.engineType
}

// GetSupportedFeatures returns the supported features
func (e *ConcreteMigrationEngine) GetSupportedFeatures() []string {
	features := []string{"up", "down", "status", "dry-run", "validate"}
	
	if e.getBoolOption("supports_transactions", false) {
		features = append(features, "transactions")
	}
	
	if e.getBoolOption("supports_rollback", false) {
		features = append(features, "rollback")
	}
	
	if e.getBoolOption("supports_schemas", false) {
		features = append(features, "schemas")
	}
	
	return features
}

// IsConnected returns the connection status
func (e *ConcreteMigrationEngine) IsConnected() bool {
	return e.connected
}

// Helper methods

func (e *ConcreteMigrationEngine) executeSingleMigration(ctx context.Context, migration types.MigrationScript, result *types.MigrationResult) error {
	// Check if already applied
	if e.isMigrationApplied(ctx, migration) {
		result.SkippedCount++
		return nil
	}

	// Execute the migration
	if err := e.executor.Execute(ctx, migration); err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", migration.ID, err)
	}

	// Update status
	if err := e.backend.UpdateStatus(ctx, migration, "applied"); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to update status for %s: %v", migration.ID, err))
	}

	return nil
}

func (e *ConcreteMigrationEngine) executeRollback(ctx context.Context, migration types.MigrationScript, result *types.MigrationResult) error {
	// Execute rollback
	if err := e.backend.Rollback(ctx, migration); err != nil {
		return fmt.Errorf("failed to rollback migration %s: %w", migration.ID, err)
	}

	// Update status
	if err := e.backend.UpdateStatus(ctx, migration, "rolled_back"); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to update status for %s: %v", migration.ID, err))
	}

	return nil
}

func (e *ConcreteMigrationEngine) isMigrationApplied(ctx context.Context, migration types.MigrationScript) bool {
	status, err := e.backend.ReadStatus(ctx)
	if err != nil {
		return false
	}

	for _, s := range status {
		if s.ID == migration.ID && s.Status == "applied" {
			return true
		}
	}

	return false
}

func (e *ConcreteMigrationEngine) findMigrationsToRollback(statusList []types.MigrationStatus, targetVersion string) []types.MigrationScript {
	// This is a simplified implementation
	// In a real implementation, this would be more sophisticated
	migrations := make([]types.MigrationScript, 0)
	
	for _, status := range statusList {
		if status.Status == "applied" && status.Version > targetVersion {
			// Convert status to migration script (simplified)
			migration := types.MigrationScript{
				ID:      status.ID,
				Version: status.Version,
			}
			migrations = append(migrations, migration)
		}
	}
	
	return migrations
}

func (e *ConcreteMigrationEngine) estimateExecutionTime(migrations []types.MigrationScript) time.Duration {
	// Simple estimation: 1 second per migration
	return time.Duration(len(migrations)) * time.Second
}

func (e *ConcreteMigrationEngine) getBoolOption(key string, defaultValue bool) bool {
	if value, exists := e.options[key]; exists {
		if boolValue, ok := value.(bool); ok {
			return boolValue
		}
	}
	return defaultValue
}

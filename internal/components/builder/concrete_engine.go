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

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
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

// scriptToMigration converts a MigrationScript (the engine's external contract type)
// into the *Migration domain type expected by the validator, resolver, and executor.
func scriptToMigration(s types.MigrationScript) *types.Migration {
	return &types.Migration{
		ID:           s.ID,
		Version:      s.Version,
		Description:  s.Description,
		Type:         s.Type,
		Script:       s.Content,
		Checksum:     s.Checksum,
		Dependencies: s.Dependencies,
		Tags:         s.Tags,
		Contexts:     s.Contexts,
		Labels:       s.Labels,
		Metadata:     s.Metadata,
		CreatedAt:    s.CreatedAt,
	}
}

// migrationToScript converts a *Migration domain type back into a MigrationScript,
// which is the engine's external contract type used in result payloads.
func migrationToScript(m *types.Migration) types.MigrationScript {
	if m == nil {
		return types.MigrationScript{}
	}
	return types.MigrationScript{
		ID:           m.ID,
		Version:      m.Version,
		Description:  m.Description,
		Type:         m.Type,
		Content:      m.Script,
		Checksum:     m.Checksum,
		Dependencies: m.Dependencies,
		Tags:         m.Tags,
		Contexts:     m.Contexts,
		Labels:       m.Labels,
		Metadata:     m.Metadata,
		CreatedAt:    m.CreatedAt,
	}
}

// scriptsToMigrationValues converts a slice of MigrationScript into a slice of
// Migration values, as required by the MigrationValidator interface.
func scriptsToMigrationValues(scripts []types.MigrationScript) []types.Migration {
	migrations := make([]types.Migration, 0, len(scripts))
	for _, s := range scripts {
		migrations = append(migrations, *scriptToMigration(s))
	}
	return migrations
}

// scriptsToMigrationPointers converts a slice of MigrationScript into a slice of
// *Migration, as required by the DependencyResolver interface.
func scriptsToMigrationPointers(scripts []types.MigrationScript) []*types.Migration {
	migrations := make([]*types.Migration, 0, len(scripts))
	for _, s := range scripts {
		migrations = append(migrations, scriptToMigration(s))
	}
	return migrations
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

	// Validate migrations. The validator returns only an error; a non-nil error
	// means the batch is invalid and execution must not proceed.
	if err := e.validator.ValidateMigrations(scriptsToMigrationValues(migrations)); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation failed: %v", err))
		return result, fmt.Errorf("migration validation failed: %w", err)
	}

	// Resolve dependencies. The resolver returns an ordered slice of *Migration.
	ordered, err := e.resolver.ResolveDependencies(ctx, scriptsToMigrationPointers(migrations))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("dependency resolution failed: %v", err))
		return result, err
	}

	// Execute migrations
	for _, migration := range ordered {
		script := migrationToScript(migration)
		if err := e.executeSingleMigration(ctx, script, &result); err != nil {
			result.FailedCount++
			result.FailedScripts = append(result.FailedScripts, types.FailedMigrationScript{
				Script:      script,
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
			result.ExecutedScripts = append(result.ExecutedScripts, script)
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

	// Get the migrations already applied to the database.
	appliedMigrations, err := e.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to get applied migrations: %v", err))
		return result, err
	}

	// Find migrations to rollback
	migrationsToRollback := e.findMigrationsToRollback(appliedMigrations, version)

	// Execute rollbacks in reverse order
	for i := len(migrationsToRollback) - 1; i >= 0; i-- {
		script := migrationToScript(migrationsToRollback[i])
		if err := e.executeRollback(ctx, script, &result); err != nil {
			result.FailedCount++
			result.FailedScripts = append(result.FailedScripts, types.FailedMigrationScript{
				Script:      script,
				Error:       err.Error(),
				ErrorCode:   "ROLLBACK_FAILED",
				FailureTime: time.Now(),
				CanRetry:    false,
			})
			break // Stop on rollback failure
		} else {
			result.ExecutedCount++
			result.ExecutedScripts = append(result.ExecutedScripts, script)
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

	// Validate migrations. A non-nil error means the batch is invalid; surface it
	// as a validation error and stop before planning execution.
	if err := e.validator.ValidateMigrations(scriptsToMigrationValues(migrations)); err != nil {
		result.ValidationErrors = append(result.ValidationErrors, types.ValidationError{
			Field:   "migrations",
			Message: err.Error(),
			Cause:   err,
		})
		return result, nil
	}

	// Resolve dependencies. The resolver returns an ordered slice of *Migration.
	ordered, err := e.resolver.ResolveDependencies(ctx, scriptsToMigrationPointers(migrations))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("dependency resolution failed: %v", err))
		return result, err
	}

	// Determine what would be executed
	for _, migration := range ordered {
		script := migrationToScript(migration)
		// Check if migration is already applied
		if e.isMigrationApplied(ctx, script) {
			result.WouldSkip = append(result.WouldSkip, script)
		} else {
			result.WouldExecute = append(result.WouldExecute, script)
		}
	}

	// Estimate execution time
	result.EstimatedTime = e.estimateExecutionTime(result.WouldExecute)

	return result, nil
}

// Validate validates migrations without executing them
func (e *ConcreteMigrationEngine) Validate(ctx context.Context, migrations []types.MigrationScript) (types.ValidationResult, error) {
	result := types.ValidationResult{
		ValidationErrors: make([]types.ValidationError, 0),
		Warnings:         make([]string, 0),
	}

	if err := e.validator.ValidateMigrations(scriptsToMigrationValues(migrations)); err != nil {
		result.Valid = false
		result.ErrorCount = 1
		result.ValidationErrors = append(result.ValidationErrors, types.ValidationError{
			Field:   "migrations",
			Message: err.Error(),
			Cause:   err,
		})
		return result, nil
	}

	result.Valid = true
	result.ValidatedCount = len(migrations)
	return result, nil
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
	if err := e.executor.Execute(ctx, scriptToMigration(migration)); err != nil {
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
	applied, err := e.tracker.IsMigrationApplied(ctx, migration.ID)
	if err != nil {
		return false
	}
	return applied
}

func (e *ConcreteMigrationEngine) findMigrationsToRollback(appliedMigrations []*types.Migration, targetVersion string) []*types.Migration {
	// This is a simplified implementation
	// In a real implementation, this would be more sophisticated
	migrations := make([]*types.Migration, 0)

	for _, migration := range appliedMigrations {
		if migration != nil && migration.Version > targetVersion {
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

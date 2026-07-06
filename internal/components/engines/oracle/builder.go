//go:build drifted_backend
// +build drifted_backend

/**
 * @file builder.go
 * @description Oracle-specific migration engine builder
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/oracle/builder.go
 */

package oracle

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// OracleEngineBuilder builds Oracle-optimized migration engines
type OracleEngineBuilder struct {
	config         types.Config
	db             *sql.DB
	backend        types.EngineBackend
	executor       types.MigrationExecutor
	tracker        types.MigrationTracker
	lockManager    types.LockManager
	validator      types.MigrationValidator
	resolver       types.DependencyResolver
	decorators     []types.MigrationDecorator
	options        map[string]interface{}
}

// NewOracleEngineBuilder creates a new Oracle engine builder
func NewOracleEngineBuilder() *OracleEngineBuilder {
	return &OracleEngineBuilder{
		decorators: make([]types.MigrationDecorator, 0),
		options:    make(map[string]interface{}),
	}
}

// SetConfig sets the configuration
func (b *OracleEngineBuilder) SetConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// SetDatabase sets the database connection
func (b *OracleEngineBuilder) SetDatabase(db *sql.DB) types.EngineBuilder {
	b.db = db
	return b
}

// SetBackend sets the engine backend
func (b *OracleEngineBuilder) SetBackend(backend types.EngineBackend) types.EngineBuilder {
	b.backend = backend
	return b
}

// SetExecutor sets the migration executor
func (b *OracleEngineBuilder) SetExecutor(executor types.MigrationExecutor) types.EngineBuilder {
	b.executor = executor
	return b
}

// SetTracker sets the migration tracker
func (b *OracleEngineBuilder) SetTracker(tracker types.MigrationTracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// SetLockManager sets the lock manager
func (b *OracleEngineBuilder) SetLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// SetValidator sets the migration validator
func (b *OracleEngineBuilder) SetValidator(validator types.MigrationValidator) types.EngineBuilder {
	b.validator = validator
	return b
}

// SetResolver sets the dependency resolver
func (b *OracleEngineBuilder) SetResolver(resolver types.DependencyResolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// AddDecorator adds a migration decorator
func (b *OracleEngineBuilder) AddDecorator(decorator types.MigrationDecorator) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// SetOption sets a builder option
func (b *OracleEngineBuilder) SetOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// Build constructs the Oracle migration engine
func (b *OracleEngineBuilder) Build() (*engine.MigrationEngine, error) {
	// Create Oracle-specific components if not provided
	if b.backend == nil {
		b.backend = &OracleBackend{
			config:  b.config,
			db:      b.db,
			options: b.options,
		}
	}

	if b.executor == nil {
		b.executor = &OracleExecutor{
			config:  b.config,
			db:      b.db,
			options: b.options,
		}
	}

	if b.tracker == nil {
		b.tracker = &OracleTracker{
			config:    b.config,
			db:        b.db,
			tableName: b.getTrackingTableName(),
			options:   b.options,
		}
	}

	if b.lockManager == nil {
		b.lockManager = &OracleLockManager{
			config:    b.config,
			db:        b.db,
			tableName: b.getLockTableName(),
			options:   b.options,
		}
	}

	if b.validator == nil {
		b.validator = &OracleValidator{
			config:  b.config,
			db:      b.db,
			options: b.options,
		}
	}

	if b.resolver == nil {
		b.resolver = &OracleResolver{
			config:  b.config,
			options: b.options,
		}
	}

	return engine.NewMigrationEngine(
		b.config,
		b.backend,
		b.executor,
		b.tracker,
		b.lockManager,
		b.validator,
		b.resolver,
	), nil
}

// getTrackingTableName returns the migration tracking table name
func (b *OracleEngineBuilder) getTrackingTableName() string {
	if tableName, ok := b.options["tracking_table"].(string); ok {
		return tableName
	}
	return "SCHEMA_MIGRATIONS"
}

// getLockTableName returns the migration lock table name
func (b *OracleEngineBuilder) getLockTableName() string {
	if tableName, ok := b.options["lock_table"].(string); ok {
		return tableName
	}
	return "MIGRATION_LOCKS"
}

// OracleMigrationEngine implements MigrationEngine for Oracle
type OracleMigrationEngine struct {
	config      types.Config
	db          *sql.DB
	backend     types.EngineBackend
	executor    types.MigrationExecutor
	tracker     types.MigrationTracker
	lockManager types.LockManager
	validator   types.MigrationValidator
	resolver    types.DependencyResolver
	decorators  []types.MigrationDecorator
	options     map[string]interface{}
	connected   bool
}

// Connect establishes connection to Oracle database
func (e *OracleMigrationEngine) Connect(ctx context.Context) error {
	if e.connected {
		return nil
	}

	if err := e.backend.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to Oracle database: %w", err)
	}

	e.connected = true
	return nil
}

// Disconnect closes connection to Oracle database
func (e *OracleMigrationEngine) Disconnect(ctx context.Context) error {
	if !e.connected {
		return nil
	}

	if err := e.backend.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect from Oracle database: %w", err)
	}

	e.connected = false
	return nil
}

// Up executes pending migrations
func (e *OracleMigrationEngine) Up(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	if !e.connected {
		return types.MigrationResult{}, fmt.Errorf("not connected to database")
	}

	// Acquire migration lock
	lockInfo := types.LockInfo{
		ID:        "migration_up",
		Owner:     "oracle_engine",
		Operation: "up",
	}

	if err := e.lockManager.AcquireLock(ctx, lockInfo); err != nil {
		return types.MigrationResult{}, fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	defer e.lockManager.ReleaseLock(ctx, lockInfo.ID)

	// Validate migrations
	validationResult, err := e.validator.Validate(ctx, migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("migration validation failed: %w", err)
	}

	if !validationResult.Valid {
		return types.MigrationResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("validation failed: %v", validationResult.Errors),
		}, nil
	}

	// Resolve dependencies
	plan, err := e.resolver.ResolveDependencies(ctx, migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Execute migrations
	result, err := e.executor.Execute(ctx, plan.Migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("migration execution failed: %w", err)
	}

	return result, nil
}

// Down executes rollback migrations
func (e *OracleMigrationEngine) Down(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	if !e.connected {
		return types.MigrationResult{}, fmt.Errorf("not connected to database")
	}

	// Acquire migration lock
	lockInfo := types.LockInfo{
		ID:        "migration_down",
		Owner:     "oracle_engine",
		Operation: "down",
	}

	if err := e.lockManager.AcquireLock(ctx, lockInfo); err != nil {
		return types.MigrationResult{}, fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	defer e.lockManager.ReleaseLock(ctx, lockInfo.ID)

	// Execute rollbacks in reverse order
	result, err := e.executor.Rollback(ctx, migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("migration rollback failed: %w", err)
	}

	return result, nil
}

// Status returns the current migration status
func (e *OracleMigrationEngine) Status(ctx context.Context) ([]types.MigrationStatus, error) {
	if !e.connected {
		return nil, fmt.Errorf("not connected to database")
	}

	applied, err := e.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	pending, err := e.tracker.GetPendingMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending migrations: %w", err)
	}

	var status []types.MigrationStatus

	// Add applied migrations
	for _, migration := range applied {
		status = append(status, types.MigrationStatus{
			Version:     migration.Version,
			Description: migration.Description,
			Applied:     true,
			AppliedAt:   &migration.AppliedAt,
		})
	}

	// Add pending migrations
	for _, migration := range pending {
		status = append(status, types.MigrationStatus{
			Version:     migration.Version,
			Description: migration.Description,
			Applied:     false,
		})
	}

	return status, nil
}

// DryRun simulates migration execution without applying changes
func (e *OracleMigrationEngine) DryRun(ctx context.Context, migrations []types.MigrationScript) (types.DryRunResult, error) {
	if !e.connected {
		return types.DryRunResult{}, fmt.Errorf("not connected to database")
	}

	// Validate migrations first
	validationResult, err := e.validator.Validate(ctx, migrations)
	if err != nil {
		return types.DryRunResult{}, fmt.Errorf("validation failed: %w", err)
	}

	// Perform dry run
	result, err := e.executor.DryRun(ctx, migrations)
	if err != nil {
		return types.DryRunResult{}, fmt.Errorf("dry run failed: %w", err)
	}

	// Include validation results
	result.ValidationResult = validationResult

	return result, nil
}

// Validate validates migrations without executing them
func (e *OracleMigrationEngine) Validate(ctx context.Context, migrations []types.MigrationScript) (types.ValidationResult, error) {
	return e.validator.Validate(ctx, migrations)
}

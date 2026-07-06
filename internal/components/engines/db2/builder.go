//go:build drifted_backend
// +build drifted_backend

/**
 * @file builder.go
 * @description DB2-specific engine builder implementation with optimized components for IBM DB2 database
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/db2/builder.go
 */

package db2

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DB2EngineBuilder builds migration engines optimized for IBM DB2
type DB2EngineBuilder struct {
	config         types.Config
	db             *sql.DB
	lockManager    types.LockManager
	tracker        types.MigrationTracker
	executor       types.MigrationExecutor
	validator      types.MigrationValidator
	resolver       types.DependencyResolver
	backend        types.EngineBackend
	decorators     []interface{}
	options        map[string]interface{}
	dryRun         bool
	observability  bool
}

// NewDB2EngineBuilder creates a new DB2 engine builder
func NewDB2EngineBuilder() types.EngineBuilder {
	return &DB2EngineBuilder{
		options:    make(map[string]interface{}),
		decorators: make([]interface{}, 0),
	}
}

// WithConfig sets the migration configuration
func (b *DB2EngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the engine backend
func (b *DB2EngineBuilder) WithBackend(backend types.EngineBackend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithExecutor sets the migration executor
func (b *DB2EngineBuilder) WithExecutor(executor types.MigrationExecutor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithTracker sets the migration tracker
func (b *DB2EngineBuilder) WithTracker(tracker types.MigrationTracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithLockManager sets the lock manager
func (b *DB2EngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithValidator sets the migration validator
func (b *DB2EngineBuilder) WithValidator(validator types.MigrationValidator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *DB2EngineBuilder) WithResolver(resolver types.DependencyResolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// AddDecorator adds a decorator to the engine
func (b *DB2EngineBuilder) AddDecorator(decorator interface{}) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets a custom option
func (b *DB2EngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// Build constructs the MigrationEngine with DB2-optimized components
func (b *DB2EngineBuilder) Build() (types.MigrationEngine, error) {
	if b.config.Database.Driver == "" {
		b.config.Database.Driver = "db2"
	}

	// Create DB2-specific backend if not provided
	if b.backend == nil {
		backend, err := b.createDB2Backend()
		if err != nil {
			return nil, fmt.Errorf("failed to create DB2 backend: %w", err)
		}
		b.backend = backend
	}

	// Create default components if not provided
	if b.executor == nil {
		b.executor = b.createDB2Executor()
	}
	if b.tracker == nil {
		b.tracker = b.createDB2Tracker()
	}
	if b.lockManager == nil {
		b.lockManager = b.createDB2LockManager()
	}
	if b.validator == nil {
		b.validator = b.createDB2Validator()
	}
	if b.resolver == nil {
		b.resolver = b.createDB2Resolver()
	}

	// Create the base engine using the common engine package
	baseEngine := &engine.MigrationEngine{
		Config:      b.config,
		Backend:     b.backend,
		Executor:    b.executor,
		Tracker:     b.tracker,
		LockManager: b.lockManager,
		Validator:   b.validator,
		Resolver:    b.resolver,
		Options:     b.options,
		DryRun:      b.dryRun,
	}

	// Apply decorators (extension point; wired once typed decorators exist).
	for _, decorator := range b.decorators {
		_ = decorator
	}

	return baseEngine, nil
}

// createDB2Backend creates a DB2-specific backend
func (b *DB2EngineBuilder) createDB2Backend() (types.EngineBackend, error) {
	return &DB2Backend{
		config:  b.config,
		db:      b.db,
		options: b.options,
	}, nil
}

// createDB2Executor creates a DB2-specific executor
func (b *DB2EngineBuilder) createDB2Executor() types.MigrationExecutor {
	return &DB2Executor{
		config:  b.config,
		db:      b.db,
		dryRun:  b.dryRun,
		options: b.options,
	}
}

// createDB2Tracker creates a DB2-specific tracker
func (b *DB2EngineBuilder) createDB2Tracker() types.MigrationTracker {
	return &DB2Tracker{
		config:    b.config,
		db:        b.db,
		tableName: b.getTrackingTableName(),
		options:   b.options,
	}
}

// createDB2LockManager creates a DB2-specific lock manager
func (b *DB2EngineBuilder) createDB2LockManager() types.LockManager {
	return &DB2LockManager{
		config:    b.config,
		db:        b.db,
		tableName: b.getLockTableName(),
		options:   b.options,
	}
}

// createDB2Validator creates a DB2-specific validator
func (b *DB2EngineBuilder) createDB2Validator() types.MigrationValidator {
	return &DB2Validator{
		config:  b.config,
		db:      b.db,
		options: b.options,
	}
}

// createDB2Resolver creates a DB2-specific resolver
func (b *DB2EngineBuilder) createDB2Resolver() types.DependencyResolver {
	return &DB2Resolver{
		config:  b.config,
		options: b.options,
	}
}

// getTrackingTableName returns the migration tracking table name
func (b *DB2EngineBuilder) getTrackingTableName() string {
	if tableName, ok := b.options["tracking_table"].(string); ok {
		return tableName
	}
	return "MIGRATION_HISTORY"
}

// getLockTableName returns the migration lock table name
func (b *DB2EngineBuilder) getLockTableName() string {
	if tableName, ok := b.options["lock_table"].(string); ok {
		return tableName
	}
	return "MIGRATION_LOCK"
}

// DB2MigrationEngine implements the MigrationEngine interface for DB2
type DB2MigrationEngine struct {
	config      types.Config
	backend     types.EngineBackend
	executor    types.MigrationExecutor
	tracker     types.MigrationTracker
	lockManager types.LockManager
	validator   types.MigrationValidator
	resolver    types.DependencyResolver
	options     map[string]interface{}
	connected   bool
}

// Connect establishes connection to DB2 database
func (e *DB2MigrationEngine) Connect(ctx context.Context) error {
	if e.connected {
		return nil
	}

	if err := e.backend.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to DB2: %w", err)
	}

	e.connected = true
	return nil
}

// Disconnect closes connection to DB2 database
func (e *DB2MigrationEngine) Disconnect(ctx context.Context) error {
	if !e.connected {
		return nil
	}

	if err := e.backend.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect from DB2: %w", err)
	}

	e.connected = false
	return nil
}

// Up executes pending migrations
func (e *DB2MigrationEngine) Up(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	if !e.connected {
		if err := e.Connect(ctx); err != nil {
			return types.MigrationResult{}, err
		}
	}

	// Acquire lock
	lockInfo := types.LockInfo{
		ID:        "migration_up",
		Owner:     "db2_engine",
		Operation: "up",
	}
	
	if err := e.lockManager.AcquireLock(ctx, lockInfo); err != nil {
		return types.MigrationResult{}, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer e.lockManager.ReleaseLock(ctx, lockInfo.ID)

	// Validate migrations
	validationResult, err := e.validator.Validate(ctx, migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("validation failed: %w", err)
	}
	if !validationResult.Valid {
		return types.MigrationResult{}, fmt.Errorf("migrations are invalid: %v", validationResult.Errors)
	}

	// Resolve dependencies
	plan, err := e.resolver.ResolveDependencies(ctx, migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Execute migrations
	result, err := e.executor.Execute(ctx, plan.Migrations)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("execution failed: %w", err)
	}

	return result, nil
}

// Down executes rollback migrations
func (e *DB2MigrationEngine) Down(ctx context.Context, target string) (types.MigrationResult, error) {
	if !e.connected {
		if err := e.Connect(ctx); err != nil {
			return types.MigrationResult{}, err
		}
	}

	// Acquire lock
	lockInfo := types.LockInfo{
		ID:        "migration_down",
		Owner:     "db2_engine",
		Operation: "down",
	}
	
	if err := e.lockManager.AcquireLock(ctx, lockInfo); err != nil {
		return types.MigrationResult{}, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer e.lockManager.ReleaseLock(ctx, lockInfo.ID)

	// Get applied migrations
	appliedMigrations, err := e.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Find migrations to rollback
	var migrationsToRollback []types.MigrationScript
	for i := len(appliedMigrations) - 1; i >= 0; i-- {
		migration := appliedMigrations[i]
		migrationsToRollback = append(migrationsToRollback, migration)
		if migration.Version == target {
			break
		}
	}

	// Execute rollbacks
	result, err := e.executor.Rollback(ctx, migrationsToRollback)
	if err != nil {
		return types.MigrationResult{}, fmt.Errorf("rollback failed: %w", err)
	}

	return result, nil
}

// Status returns the current migration status
func (e *DB2MigrationEngine) Status(ctx context.Context) ([]types.MigrationScript, error) {
	if !e.connected {
		if err := e.Connect(ctx); err != nil {
			return nil, err
		}
	}

	return e.tracker.GetAppliedMigrations(ctx)
}

// DryRun simulates migration execution without applying changes
func (e *DB2MigrationEngine) DryRun(ctx context.Context, migrations []types.MigrationScript) (types.DryRunResult, error) {
	// Create a copy of the executor with dry run enabled
	dryRunExecutor := &DB2Executor{
		config:  e.config,
		db:      nil, // No actual DB connection for dry run
		dryRun:  true,
		options: e.options,
	}

	// Validate migrations
	validationResult, err := e.validator.Validate(ctx, migrations)
	if err != nil {
		return types.DryRunResult{}, fmt.Errorf("validation failed: %w", err)
	}

	// Resolve dependencies
	plan, err := e.resolver.ResolveDependencies(ctx, migrations)
	if err != nil {
		return types.DryRunResult{}, fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Simulate execution
	result, err := dryRunExecutor.Execute(ctx, plan.Migrations)
	if err != nil {
		return types.DryRunResult{}, fmt.Errorf("dry run execution failed: %w", err)
	}

	return types.DryRunResult{
		WouldExecute:     plan.Migrations,
		ValidationResult: validationResult,
		ExecutionPlan:    plan,
		EstimatedTime:    result.Duration,
		Changes:          e.generateChangesSummary(plan.Migrations),
	}, nil
}

// Validate validates migrations without executing them
func (e *DB2MigrationEngine) Validate(ctx context.Context, migrations []types.MigrationScript) (types.ValidationResult, error) {
	return e.validator.Validate(ctx, migrations)
}

// generateChangesSummary generates a summary of changes for dry run
func (e *DB2MigrationEngine) generateChangesSummary(migrations []types.MigrationScript) []string {
	var changes []string
	for _, migration := range migrations {
		changes = append(changes, fmt.Sprintf("Would execute migration %s: %s", 
			migration.Version, migration.Description))
		
		// Analyze SQL for DB2-specific changes
		if strings.Contains(strings.ToUpper(migration.UpScript), "CREATE TABLE") {
			changes = append(changes, "  - Would create new table(s)")
		}
		if strings.Contains(strings.ToUpper(migration.UpScript), "ALTER TABLE") {
			changes = append(changes, "  - Would alter existing table(s)")
		}
		if strings.Contains(strings.ToUpper(migration.UpScript), "CREATE INDEX") {
			changes = append(changes, "  - Would create new index(es)")
		}
		if strings.Contains(strings.ToUpper(migration.UpScript), "INSERT") {
			changes = append(changes, "  - Would insert data")
		}
	}
	return changes
}

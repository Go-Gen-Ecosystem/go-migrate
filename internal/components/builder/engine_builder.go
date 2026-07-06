/**
 * @file engine_builder.go
 * @description Concrete MigrationEngineBuilder implementation using the Builder pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/builder/engine_builder.go
 */

package builder

import (
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ConcreteMigrationEngineBuilder implements the EngineBuilder interface
// It uses the Builder pattern to construct migration engines with all required components
type ConcreteMigrationEngineBuilder struct {
	engineType        string
	config            types.Config
	backend           types.EngineBackend
	executor          types.MigrationExecutor
	tracker           types.MigrationTracker
	lockMgr           types.LockManager
	validator         types.MigrationValidator
	resolver          types.DependencyResolver
	rollbackManager   types.RollbackManager
	advancedRollback  types.AdvancedRollbackManager
	rollbackConfig    *types.RollbackConfig
	stateBasedMigrator interface{} // state.StateBasedMigrator
	decorators        []EngineDecorator
	options           map[string]interface{}
}

// NewMigrationEngineBuilder creates a new migration engine builder
func NewMigrationEngineBuilder(engineType string) types.EngineBuilder {
	return &ConcreteMigrationEngineBuilder{
		engineType: engineType,
		decorators: make([]EngineDecorator, 0),
		options:    make(map[string]interface{}),
	}
}

// WithConfig sets the configuration for the engine
func (b *ConcreteMigrationEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the backend for the engine
func (b *ConcreteMigrationEngineBuilder) WithBackend(backend types.EngineBackend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithExecutor sets the executor for the engine
func (b *ConcreteMigrationEngineBuilder) WithExecutor(executor types.MigrationExecutor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithTracker sets the tracker for the engine
func (b *ConcreteMigrationEngineBuilder) WithTracker(tracker types.MigrationTracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithLockManager sets the lock manager for the engine
func (b *ConcreteMigrationEngineBuilder) WithLockManager(lockMgr types.LockManager) types.EngineBuilder {
	b.lockMgr = lockMgr
	return b
}

// WithValidator sets the validator for the engine
func (b *ConcreteMigrationEngineBuilder) WithValidator(validator types.MigrationValidator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver for the engine
func (b *ConcreteMigrationEngineBuilder) WithResolver(resolver types.DependencyResolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithRollbackManager sets the rollback manager for the engine
func (b *ConcreteMigrationEngineBuilder) WithRollbackManager(rollbackManager types.RollbackManager) *ConcreteMigrationEngineBuilder {
	b.rollbackManager = rollbackManager
	return b
}

// WithAdvancedRollback sets the advanced rollback manager for the engine
func (b *ConcreteMigrationEngineBuilder) WithAdvancedRollback(advancedRollback types.AdvancedRollbackManager) *ConcreteMigrationEngineBuilder {
	b.advancedRollback = advancedRollback
	return b
}

// WithRollbackConfig sets the rollback configuration for the engine
func (b *ConcreteMigrationEngineBuilder) WithRollbackConfig(config *types.RollbackConfig) *ConcreteMigrationEngineBuilder {
	b.rollbackConfig = config
	return b
}

// WithDecorator adds a decorator to the engine
func (b *ConcreteMigrationEngineBuilder) WithDecorator(decorator EngineDecorator) *ConcreteMigrationEngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets a custom option for the engine
func (b *ConcreteMigrationEngineBuilder) WithOption(key string, value interface{}) *ConcreteMigrationEngineBuilder {
	b.options[key] = value
	return b
}

// WithStateBasedMigrator sets the state-based migrator for the engine
func (b *ConcreteMigrationEngineBuilder) WithStateBasedMigrator(migrator interface{}) *ConcreteMigrationEngineBuilder {
	b.stateBasedMigrator = migrator
	return b
}

// Build constructs the migration engine with all configured components
func (b *ConcreteMigrationEngineBuilder) Build() (*engine.MigrationEngine, error) {
	// Validate required components
	if err := b.validate(); err != nil {
		return nil, fmt.Errorf("builder validation failed: %w", err)
	}

	// Create the base engine using the engine package constructor
	migrationEngine := engine.NewMigrationEngine(
		b.config,
		b.backend,
		b.executor,
		b.tracker,
		b.lockMgr,
		b.validator,
		b.resolver,
	)

	// Wire rollback components if provided
	if b.rollbackManager != nil {
		migrationEngine.SetRollbackManager(b.rollbackManager)

		// Link advanced rollback to base rollback manager if both exist
		if b.advancedRollback != nil && b.rollbackManager != nil {
			if setter, ok := b.rollbackManager.(interface{ SetAdvancedManager(types.AdvancedRollbackManager) }); ok {
				setter.SetAdvancedManager(b.advancedRollback)
			}
		}
	}

	// Apply decorators
	var decoratedEngine *engine.MigrationEngine = migrationEngine
	for _, decorator := range b.decorators {
		decoratedEngine = decorator.Decorate(decoratedEngine)
	}

	return decoratedEngine, nil
}

// validate ensures all required components are provided
func (b *ConcreteMigrationEngineBuilder) validate() error {
	if b.config.DatabaseConfig.Driver == "" {
		return fmt.Errorf("database driver is required")
	}

	if b.backend == nil {
		return fmt.Errorf("backend is required")
	}

	if b.executor == nil {
		return fmt.Errorf("executor is required")
	}

	if b.tracker == nil {
		return fmt.Errorf("tracker is required")
	}

	if b.lockMgr == nil {
		return fmt.Errorf("lock manager is required")
	}

	if b.validator == nil {
		return fmt.Errorf("validator is required")
	}

	return nil
}

// EngineDecorator defines the interface for decorating migration engines
type EngineDecorator interface {
	Decorate(engine *engine.MigrationEngine) *engine.MigrationEngine
}

// DatabaseSpecificEngineBuilder provides database-specific builder implementations
type DatabaseSpecificEngineBuilder struct {
	*ConcreteMigrationEngineBuilder
	dbType string
}

// NewPostgreSQLEngineBuilder creates a PostgreSQL-specific engine builder
func NewPostgreSQLEngineBuilder() types.EngineBuilder {
	builder := &DatabaseSpecificEngineBuilder{
		ConcreteMigrationEngineBuilder: &ConcreteMigrationEngineBuilder{
			engineType: "postgresql",
			decorators: make([]EngineDecorator, 0),
			options:    make(map[string]interface{}),
		},
		dbType: "postgresql",
	}
	
	// Set PostgreSQL-specific defaults
	builder.WithOption("supports_transactions", true)
	builder.WithOption("supports_rollback", true)
	builder.WithOption("supports_schemas", true)
	
	return builder
}

// NewMySQLEngineBuilder creates a MySQL-specific engine builder
func NewMySQLEngineBuilder() types.EngineBuilder {
	builder := &DatabaseSpecificEngineBuilder{
		ConcreteMigrationEngineBuilder: &ConcreteMigrationEngineBuilder{
			engineType: "mysql",
			decorators: make([]EngineDecorator, 0),
			options:    make(map[string]interface{}),
		},
		dbType: "mysql",
	}
	
	// Set MySQL-specific defaults
	builder.WithOption("supports_transactions", true)
	builder.WithOption("supports_rollback", true)
	builder.WithOption("supports_schemas", false)
	
	return builder
}

// NewSQLiteEngineBuilder creates a SQLite-specific engine builder
func NewSQLiteEngineBuilder() types.EngineBuilder {
	builder := &DatabaseSpecificEngineBuilder{
		ConcreteMigrationEngineBuilder: &ConcreteMigrationEngineBuilder{
			engineType: "sqlite",
			decorators: make([]EngineDecorator, 0),
			options:    make(map[string]interface{}),
		},
		dbType: "sqlite",
	}
	
	// Set SQLite-specific defaults
	builder.WithOption("supports_transactions", true)
	builder.WithOption("supports_rollback", false)
	builder.WithOption("supports_schemas", false)
	builder.WithOption("file_based", true)
	
	return builder
}

// NewMongoDBEngineBuilder creates a MongoDB-specific engine builder
func NewMongoDBEngineBuilder() types.EngineBuilder {
	builder := &DatabaseSpecificEngineBuilder{
		ConcreteMigrationEngineBuilder: &ConcreteMigrationEngineBuilder{
			engineType: "mongodb",
			decorators: make([]EngineDecorator, 0),
			options:    make(map[string]interface{}),
		},
		dbType: "mongodb",
	}
	
	// Set MongoDB-specific defaults
	builder.WithOption("supports_transactions", true) // MongoDB 4.0+
	builder.WithOption("supports_rollback", false)
	builder.WithOption("document_based", true)
	builder.WithOption("supports_collections", true)
	
	return builder
}

// NewRedisEngineBuilder creates a Redis-specific engine builder
func NewRedisEngineBuilder() types.EngineBuilder {
	builder := &DatabaseSpecificEngineBuilder{
		ConcreteMigrationEngineBuilder: &ConcreteMigrationEngineBuilder{
			engineType: "redis",
			decorators: make([]EngineDecorator, 0),
			options:    make(map[string]interface{}),
		},
		dbType: "redis",
	}
	
	// Set Redis-specific defaults
	builder.WithOption("supports_transactions", false)
	builder.WithOption("supports_rollback", false)
	builder.WithOption("key_value_based", true)
	builder.WithOption("supports_lua_scripts", true)
	
	return builder
}

// BuilderChain allows chaining multiple builders for complex engine construction
type BuilderChain struct {
	builders []types.EngineBuilder
	config   types.Config
}

// NewBuilderChain creates a new builder chain
func NewBuilderChain(config types.Config) *BuilderChain {
	return &BuilderChain{
		builders: make([]types.EngineBuilder, 0),
		config:   config,
	}
}

// AddBuilder adds a builder to the chain
func (bc *BuilderChain) AddBuilder(builder types.EngineBuilder) *BuilderChain {
	bc.builders = append(bc.builders, builder)
	return bc
}

// BuildAll builds all engines in the chain
func (bc *BuilderChain) BuildAll() ([]*engine.MigrationEngine, error) {
	engines := make([]*engine.MigrationEngine, 0, len(bc.builders))
	
	for i, builder := range bc.builders {
		engine, err := builder.WithConfig(bc.config).Build()
		if err != nil {
			return nil, fmt.Errorf("failed to build engine at index %d: %w", i, err)
		}
		engines = append(engines, engine)
	}
	
	return engines, nil
}

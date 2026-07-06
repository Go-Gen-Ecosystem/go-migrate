/**
 * @file builder.go
 * @description SQL Server-specific migration engine builder with optimized configurations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/sqlserver/builder.go
 */

package sqlserver

import (
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// SQLServerConfig holds SQL Server-specific configuration
type SQLServerConfig struct {
	ConnectionString string
	Options          map[string]interface{}
}

// SQLServerEngineBuilder builds migration engines optimized for SQL Server
type SQLServerEngineBuilder struct {
	config      types.Config
	backend     types.Backend
	lockManager types.LockManager
	tracker     types.Tracker
	executor    types.Executor
	validator   types.Validator
	resolver    types.Resolver
	decorators  []types.Decorator
	options     map[string]interface{}
}

// NewSQLServerEngineBuilder creates a new SQL Server engine builder
func NewSQLServerEngineBuilder() *SQLServerEngineBuilder {
	return &SQLServerEngineBuilder{
		options: make(map[string]interface{}),
	}
}

// WithConfig sets the migration configuration
func (b *SQLServerEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the database backend
func (b *SQLServerEngineBuilder) WithBackend(backend types.Backend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithLockManager sets the lock manager
func (b *SQLServerEngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithTracker sets the migration tracker
func (b *SQLServerEngineBuilder) WithTracker(tracker types.Tracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithExecutor sets the migration executor
func (b *SQLServerEngineBuilder) WithExecutor(executor types.Executor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithValidator sets the migration validator
func (b *SQLServerEngineBuilder) WithValidator(validator types.Validator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *SQLServerEngineBuilder) WithResolver(resolver types.Resolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithDecorator adds a migration decorator
func (b *SQLServerEngineBuilder) WithDecorator(decorator types.Decorator) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets a configuration option
func (b *SQLServerEngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// WithDryRun enables dry run mode
func (b *SQLServerEngineBuilder) WithDryRun(dryRun bool) types.EngineBuilder {
	b.options["dry_run"] = dryRun
	return b
}

// Build constructs the MigrationEngine with SQL Server-optimized components
func (b *SQLServerEngineBuilder) Build() (types.MigrationEngine, error) {
	if b.backend == nil {
		return nil, fmt.Errorf("backend is required")
	}

	// Create the migration engine directly using the builder's components
	migrationEngine := engine.NewMigrationEngine(
		b.config,
		b.backend,
		b.lockManager,
		b.tracker,
		b.executor,
		b.validator,
		b.resolver,
		b.decorators,
		b.options,
	)

	return migrationEngine, nil
}

/*
@file builder.go
@description SQLite-specific engine builder implementation with optimized components for SQLite database
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/components/engines/sqlite/builder.go
*/

package sqlite

import (
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// SQLiteConfig holds SQLite-specific configuration
type SQLiteConfig struct {
	DatabasePath string
	Options      map[string]interface{}
}

// SQLiteEngineBuilder builds migration engines optimized for SQLite
type SQLiteEngineBuilder struct {
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

// NewSQLiteEngineBuilder creates a new SQLite engine builder
func NewSQLiteEngineBuilder() *SQLiteEngineBuilder {
	return &SQLiteEngineBuilder{
		options: make(map[string]interface{}),
	}
}

// WithConfig sets the migration configuration
func (b *SQLiteEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the database backend
func (b *SQLiteEngineBuilder) WithBackend(backend types.Backend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithLockManager sets the lock manager
func (b *SQLiteEngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithTracker sets the migration tracker
func (b *SQLiteEngineBuilder) WithTracker(tracker types.Tracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithExecutor sets the migration executor
func (b *SQLiteEngineBuilder) WithExecutor(executor types.Executor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithValidator sets the migration validator
func (b *SQLiteEngineBuilder) WithValidator(validator types.Validator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *SQLiteEngineBuilder) WithResolver(resolver types.Resolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithDecorator adds a migration decorator
func (b *SQLiteEngineBuilder) WithDecorator(decorator types.Decorator) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets a configuration option
func (b *SQLiteEngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// WithDryRun enables dry run mode
func (b *SQLiteEngineBuilder) WithDryRun(dryRun bool) types.EngineBuilder {
	b.options["dry_run"] = dryRun
	return b
}

// Build constructs the MigrationEngine with SQLite-optimized components
func (b *SQLiteEngineBuilder) Build() (types.MigrationEngine, error) {
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

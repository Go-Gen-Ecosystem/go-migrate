/*
@file builder.go
@description PostgreSQL-specific engine builder implementation with optimized components for PostgreSQL database
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/components/engines/postgres/builder.go
*/

package postgres

import (
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// PostgreSQLConfig represents PostgreSQL-specific configuration
type PostgreSQLConfig struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
	Options  map[string]interface{}
}

// PostgresEngineBuilder builds migration engines optimized for PostgreSQL
type PostgresEngineBuilder struct {
	config      types.Config
	backend     types.Backend
	lockManager types.LockManager
	tracker     types.Tracker
	executor    types.Executor
	validator   types.Validator
	resolver    types.Resolver
	decorators  []types.Decorator
	options     map[string]interface{}
	dryRun      bool
}

// NewPostgresEngineBuilder creates a new PostgreSQL engine builder
func NewPostgresEngineBuilder() *PostgresEngineBuilder {
	return &PostgresEngineBuilder{
		options: make(map[string]interface{}),
	}
}

// WithConfig sets the migration configuration
func (b *PostgresEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the backend
func (b *PostgresEngineBuilder) WithBackend(backend types.Backend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithLockManager sets the lock manager
func (b *PostgresEngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithTracker sets the migration tracker
func (b *PostgresEngineBuilder) WithTracker(tracker types.Tracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithExecutor sets the migration executor
func (b *PostgresEngineBuilder) WithExecutor(executor types.Executor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithValidator sets the migration validator
func (b *PostgresEngineBuilder) WithValidator(validator types.Validator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *PostgresEngineBuilder) WithResolver(resolver types.Resolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithDecorator adds a decorator
func (b *PostgresEngineBuilder) WithDecorator(decorator types.Decorator) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets an option
func (b *PostgresEngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// WithDryRun enables dry run mode
func (b *PostgresEngineBuilder) WithDryRun(dryRun bool) types.EngineBuilder {
	b.dryRun = dryRun
	return b
}

// Build constructs the MigrationEngine with PostgreSQL-optimized components
func (b *PostgresEngineBuilder) Build() (types.MigrationEngine, error) {
	if b.backend == nil {
		return nil, fmt.Errorf("backend is required")
	}

	return &engine.MigrationEngine{
		Config:      b.config,
		Backend:     b.backend,
		LockManager: b.lockManager,
		Tracker:     b.tracker,
		Executor:    b.executor,
		Validator:   b.validator,
		Resolver:    b.resolver,
		Decorators:  b.decorators,
		Options:     b.options,
		DryRun:      b.dryRun,
	}, nil
}

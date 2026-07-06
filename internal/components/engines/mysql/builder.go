/*
@file builder.go
@description MySQL-specific engine builder implementation with optimized components for MySQL database
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/components/engines/mysql/builder.go
*/

package mysql

import (
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// MySQLConfig represents MySQL-specific configuration
type MySQLConfig struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Options  map[string]interface{}
}

// MySQLEngineBuilder builds migration engines optimized for MySQL
type MySQLEngineBuilder struct {
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

// NewMySQLEngineBuilder creates a new MySQL engine builder
func NewMySQLEngineBuilder() *MySQLEngineBuilder {
	return &MySQLEngineBuilder{
		options: make(map[string]interface{}),
	}
}

// WithConfig sets the migration configuration
func (b *MySQLEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the backend
func (b *MySQLEngineBuilder) WithBackend(backend types.Backend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithLockManager sets the lock manager
func (b *MySQLEngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithTracker sets the migration tracker
func (b *MySQLEngineBuilder) WithTracker(tracker types.Tracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithExecutor sets the migration executor
func (b *MySQLEngineBuilder) WithExecutor(executor types.Executor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithValidator sets the migration validator
func (b *MySQLEngineBuilder) WithValidator(validator types.Validator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *MySQLEngineBuilder) WithResolver(resolver types.Resolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithDecorator adds a decorator
func (b *MySQLEngineBuilder) WithDecorator(decorator types.Decorator) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets an option
func (b *MySQLEngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// WithDryRun enables dry run mode
func (b *MySQLEngineBuilder) WithDryRun(dryRun bool) types.EngineBuilder {
	b.dryRun = dryRun
	return b
}

// Build builds a MySQL-optimized migration engine
func (b *MySQLEngineBuilder) Build() (types.MigrationEngine, error) {
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

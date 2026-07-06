/**
 * @file builder.go
 * @description Cassandra migration engine builder implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/cassandra/builder.go
 */

package cassandra

import (
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// CassandraConfig holds Cassandra connection configuration
type CassandraConfig struct {
	Hosts    []string
	Port     int
	Keyspace string
	Username string
	Password string
	SSL      bool
}

// CassandraEngineBuilder builds Cassandra-specific migration engines
type CassandraEngineBuilder struct {
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

// NewCassandraEngineBuilder creates a new Cassandra engine builder
func NewCassandraEngineBuilder() types.EngineBuilder {
	return &CassandraEngineBuilder{
		decorators: make([]types.Decorator, 0),
		options:    make(map[string]interface{}),
	}
}

// WithConfig sets the migration configuration
func (b *CassandraEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the database backend
func (b *CassandraEngineBuilder) WithBackend(backend types.Backend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithExecutor sets the migration executor
func (b *CassandraEngineBuilder) WithExecutor(executor types.Executor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithTracker sets the migration tracker
func (b *CassandraEngineBuilder) WithTracker(tracker types.Tracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithLockManager sets the lock manager
func (b *CassandraEngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithValidator sets the migration validator
func (b *CassandraEngineBuilder) WithValidator(validator types.Validator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *CassandraEngineBuilder) WithResolver(resolver types.Resolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithDecorator adds a decorator to the engine
func (b *CassandraEngineBuilder) WithDecorator(decorator types.Decorator) types.EngineBuilder {
	b.decorators = append(b.decorators, decorator)
	return b
}

// WithOption sets an option for the engine
func (b *CassandraEngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// WithDryRun sets the dry run mode
func (b *CassandraEngineBuilder) WithDryRun(dryRun bool) types.EngineBuilder {
	b.dryRun = dryRun
	return b
}

// Build creates the final migration engine
func (b *CassandraEngineBuilder) Build() (types.MigrationEngine, error) {
	if b.config.Database.Driver == "" {
		return nil, fmt.Errorf("migration config is required")
	}

	if b.backend == nil {
		return nil, fmt.Errorf("backend is required")
	}

	if b.executor == nil {
		return nil, fmt.Errorf("executor is required")
	}

	if b.tracker == nil {
		return nil, fmt.Errorf("tracker is required")
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

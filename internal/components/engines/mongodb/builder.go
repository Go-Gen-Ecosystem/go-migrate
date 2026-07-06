/**
 * @file builder.go
 * @description MongoDB migration engine builder implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/mongodb/builder.go
 */

package mongodb

import (
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// MongoDBConfig holds MongoDB connection configuration
type MongoDBConfig struct {
	Hosts        []string
	Port         int
	Database     string
	AuthDatabase string
	Username     string
	Password     string
	SSL          bool
}

// MongoDBEngineBuilder builds MongoDB-specific migration engines
type MongoDBEngineBuilder struct {
	config      types.Config
	backend     types.EngineBackend
	lockManager types.LockManager
	tracker     types.MigrationTracker
	executor    types.MigrationExecutor
	validator   types.MigrationValidator
	resolver    types.DependencyResolver
	options     map[string]interface{}
	dryRun      bool
}

// NewMongoDBEngineBuilder creates a new MongoDB engine builder
func NewMongoDBEngineBuilder() types.EngineBuilder {
	return &MongoDBEngineBuilder{
		options: make(map[string]interface{}),
	}
}

// WithConfig sets the migration configuration
func (b *MongoDBEngineBuilder) WithConfig(config types.Config) types.EngineBuilder {
	b.config = config
	return b
}

// WithBackend sets the database backend
func (b *MongoDBEngineBuilder) WithBackend(backend types.EngineBackend) types.EngineBuilder {
	b.backend = backend
	return b
}

// WithExecutor sets the migration executor
func (b *MongoDBEngineBuilder) WithExecutor(executor types.MigrationExecutor) types.EngineBuilder {
	b.executor = executor
	return b
}

// WithTracker sets the migration tracker
func (b *MongoDBEngineBuilder) WithTracker(tracker types.MigrationTracker) types.EngineBuilder {
	b.tracker = tracker
	return b
}

// WithLockManager sets the lock manager
func (b *MongoDBEngineBuilder) WithLockManager(lockManager types.LockManager) types.EngineBuilder {
	b.lockManager = lockManager
	return b
}

// WithValidator sets the migration validator
func (b *MongoDBEngineBuilder) WithValidator(validator types.MigrationValidator) types.EngineBuilder {
	b.validator = validator
	return b
}

// WithResolver sets the dependency resolver
func (b *MongoDBEngineBuilder) WithResolver(resolver types.DependencyResolver) types.EngineBuilder {
	b.resolver = resolver
	return b
}

// WithOption sets an option for the engine
func (b *MongoDBEngineBuilder) WithOption(key string, value interface{}) types.EngineBuilder {
	b.options[key] = value
	return b
}

// WithDryRun sets the dry run mode
func (b *MongoDBEngineBuilder) WithDryRun(dryRun bool) types.EngineBuilder {
	b.dryRun = dryRun
	return b
}

// Build creates the final migration engine
func (b *MongoDBEngineBuilder) Build() (types.MigrationEngineInterface, error) {
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

	// Create a MongoDB-specific migration engine implementation
	// This would need to be implemented based on the actual engine structure
	return nil, fmt.Errorf("MongoDB engine implementation not yet complete")
}

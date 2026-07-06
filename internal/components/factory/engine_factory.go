/**
 * @file engine_factory.go
 * @description MigrationEngineFactory implementation with builder registration system
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/factory/engine_factory.go
 */

package factory

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DefaultMigrationEngineFactory implements the MigrationEngineFactory interface
// It uses the Abstract Factory pattern to create database-specific migration engines
type DefaultMigrationEngineFactory struct {
	builders map[string]types.EngineBuilder
	mutex    sync.RWMutex
}

// NewMigrationEngineFactory creates a new instance of the migration engine factory
func NewMigrationEngineFactory() types.MigrationEngineFactory {
	factory := &DefaultMigrationEngineFactory{
		builders: make(map[string]types.EngineBuilder),
	}
	
	// Register default builders
	factory.registerDefaultBuilders()
	
	return factory
}

// CreateEngine creates a migration engine for the specified database type
func (f *DefaultMigrationEngineFactory) CreateEngine(dbType string, config types.Config) (types.MigrationEngineInterface, error) {
	f.mutex.RLock()
	builder, exists := f.builders[strings.ToLower(dbType)]
	f.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unsupported database type: %s. Supported types: %v",
			dbType, f.GetSupportedTypes())
	}

	// Use the builder to create the engine
	builtEngine, err := builder.WithConfig(config).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create engine for %s: %w", dbType, err)
	}

	return builtEngine, nil
}

// RegisterBuilder registers a new engine builder for a specific database type
func (f *DefaultMigrationEngineFactory) RegisterBuilder(dbType string, builder types.EngineBuilder) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	
	f.builders[strings.ToLower(dbType)] = builder
}

// GetSupportedTypes returns a list of supported database types
func (f *DefaultMigrationEngineFactory) GetSupportedTypes() []string {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	
	types := make([]string, 0, len(f.builders))
	for dbType := range f.builders {
		types = append(types, dbType)
	}
	
	return types
}

// IsSupported checks if a database type is supported
func (f *DefaultMigrationEngineFactory) IsSupported(dbType string) bool {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	
	_, exists := f.builders[strings.ToLower(dbType)]
	return exists
}

// registerDefaultBuilders registers the default engine builders for supported databases
func (f *DefaultMigrationEngineFactory) registerDefaultBuilders() {
	// Note: These builders will be implemented in the builder/ directory
	// For now, we register placeholders that will be replaced with actual implementations
	
	// SQL Databases
	f.RegisterBuilder("postgresql", NewPostgreSQLEngineBuilder())
	f.RegisterBuilder("postgres", NewPostgreSQLEngineBuilder())
	f.RegisterBuilder("mysql", NewMySQLEngineBuilder())
	f.RegisterBuilder("sqlite", NewSQLiteEngineBuilder())
	f.RegisterBuilder("sqlite3", NewSQLiteEngineBuilder())
	f.RegisterBuilder("sqlserver", NewSQLServerEngineBuilder())
	f.RegisterBuilder("mssql", NewSQLServerEngineBuilder())
	f.RegisterBuilder("oracle", NewOracleEngineBuilder())
	f.RegisterBuilder("db2", NewDB2EngineBuilder())
	
	// NoSQL Databases
	f.RegisterBuilder("mongodb", NewMongoDBEngineBuilder())
	f.RegisterBuilder("mongo", NewMongoDBEngineBuilder())
	f.RegisterBuilder("cassandra", NewCassandraEngineBuilder())
	f.RegisterBuilder("redis", NewRedisEngineBuilder())
}

// ComponentsFactoryRegistry manages the registration of components factories
type ComponentsFactoryRegistry struct {
	factories map[string]types.ComponentsFactory
	mutex     sync.RWMutex
}

// NewComponentsFactoryRegistry creates a new components factory registry
func NewComponentsFactoryRegistry() *ComponentsFactoryRegistry {
	registry := &ComponentsFactoryRegistry{
		factories: make(map[string]types.ComponentsFactory),
	}
	
	// Register default component factories
	registry.registerDefaultFactories()
	
	return registry
}

// RegisterFactory registers a components factory for a specific dialect
func (r *ComponentsFactoryRegistry) RegisterFactory(dialect string, factory types.ComponentsFactory) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	r.factories[strings.ToLower(dialect)] = factory
}

// GetFactory retrieves a components factory for a specific dialect
func (r *ComponentsFactoryRegistry) GetFactory(dialect string) (types.ComponentsFactory, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	
	factory, exists := r.factories[strings.ToLower(dialect)]
	if !exists {
		return nil, fmt.Errorf("no components factory registered for dialect: %s", dialect)
	}
	
	return factory, nil
}

// GetSupportedDialects returns a list of supported dialects
func (r *ComponentsFactoryRegistry) GetSupportedDialects() []string {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	
	dialects := make([]string, 0, len(r.factories))
	for dialect := range r.factories {
		dialects = append(dialects, dialect)
	}
	
	return dialects
}

// registerDefaultFactories registers the default components factories
func (r *ComponentsFactoryRegistry) registerDefaultFactories() {
	// Note: These factories will be implemented in their respective engine directories
	// For now, we register placeholders
	
	// SQL Database Factories
	r.RegisterFactory("postgresql", NewPostgreSQLComponentsFactory())
	r.RegisterFactory("mysql", NewMySQLComponentsFactory())
	r.RegisterFactory("sqlite", NewSQLiteComponentsFactory())
	r.RegisterFactory("sqlserver", NewSQLServerComponentsFactory())
	r.RegisterFactory("oracle", NewOracleComponentsFactory())
	r.RegisterFactory("db2", NewDB2ComponentsFactory())
	
	// NoSQL Database Factories
	r.RegisterFactory("mongodb", NewMongoDBComponentsFactory())
	r.RegisterFactory("cassandra", NewCassandraComponentsFactory())
	r.RegisterFactory("redis", NewRedisComponentsFactory())
}

// Global registry instance
var (
	globalFactoryRegistry     *ComponentsFactoryRegistry
	globalFactoryRegistryOnce sync.Once
)

// GetGlobalComponentsFactoryRegistry returns the global components factory registry
func GetGlobalComponentsFactoryRegistry() *ComponentsFactoryRegistry {
	globalFactoryRegistryOnce.Do(func() {
		globalFactoryRegistry = NewComponentsFactoryRegistry()
	})
	return globalFactoryRegistry
}

// FactoryBuilder provides a fluent interface for building factories with dependencies
type FactoryBuilder struct {
	factoryType string
	config      types.Config
	registry    *ComponentsFactoryRegistry
	options     map[string]interface{}
}

// NewFactoryBuilder creates a new factory builder
func NewFactoryBuilder(factoryType string) *FactoryBuilder {
	return &FactoryBuilder{
		factoryType: factoryType,
		registry:    GetGlobalComponentsFactoryRegistry(),
		options:     make(map[string]interface{}),
	}
}

// WithConfig sets the configuration for the factory
func (fb *FactoryBuilder) WithConfig(config types.Config) *FactoryBuilder {
	fb.config = config
	return fb
}

// WithRegistry sets a custom registry for the factory
func (fb *FactoryBuilder) WithRegistry(registry *ComponentsFactoryRegistry) *FactoryBuilder {
	fb.registry = registry
	return fb
}

// WithOption sets a custom option for the factory
func (fb *FactoryBuilder) WithOption(key string, value interface{}) *FactoryBuilder {
	fb.options[key] = value
	return fb
}

// Build creates the components factory
func (fb *FactoryBuilder) Build() (types.ComponentsFactory, error) {
	if fb.registry == nil {
		return nil, fmt.Errorf("no registry provided")
	}
	
	factory, err := fb.registry.GetFactory(fb.factoryType)
	if err != nil {
		return nil, fmt.Errorf("failed to get factory for type %s: %w", fb.factoryType, err)
	}
	
	return factory, nil
}

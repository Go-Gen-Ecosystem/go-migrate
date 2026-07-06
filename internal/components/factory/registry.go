/**
 * @file registry.go
 * @description Factory registry for managing global factory instances and plugin registration
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/factory/registry.go
 */

package factory

import (
	"fmt"
	"sync"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// GlobalFactoryRegistry manages all factory instances and provides plugin registration
type GlobalFactoryRegistry struct {
	engineFactory     types.MigrationEngineFactory
	componentsRegistry *ComponentsFactoryRegistry
	pluginBuilders    map[string]types.EngineBuilder
	pluginFactories   map[string]types.ComponentsFactory
	mutex             sync.RWMutex
}

var (
	globalRegistry     *GlobalFactoryRegistry
	globalRegistryOnce sync.Once
)

// GetGlobalFactoryRegistry returns the singleton global factory registry
func GetGlobalFactoryRegistry() *GlobalFactoryRegistry {
	globalRegistryOnce.Do(func() {
		globalRegistry = &GlobalFactoryRegistry{
			engineFactory:      NewMigrationEngineFactory(),
			componentsRegistry: NewComponentsFactoryRegistry(),
			pluginBuilders:     make(map[string]types.EngineBuilder),
			pluginFactories:    make(map[string]types.ComponentsFactory),
		}
	})
	return globalRegistry
}

// GetEngineFactory returns the global migration engine factory
func (gr *GlobalFactoryRegistry) GetEngineFactory() types.MigrationEngineFactory {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()
	return gr.engineFactory
}

// GetComponentsRegistry returns the global components factory registry
func (gr *GlobalFactoryRegistry) GetComponentsRegistry() *ComponentsFactoryRegistry {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()
	return gr.componentsRegistry
}

// RegisterPluginBuilder registers a plugin engine builder
func (gr *GlobalFactoryRegistry) RegisterPluginBuilder(dbType string, builder types.EngineBuilder) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	if _, exists := gr.pluginBuilders[dbType]; exists {
		return fmt.Errorf("plugin builder for database type %s already registered", dbType)
	}

	gr.pluginBuilders[dbType] = builder
	
	// Also register with the engine factory
	gr.engineFactory.RegisterBuilder(dbType, builder)
	
	return nil
}

// RegisterPluginComponentsFactory registers a plugin components factory
func (gr *GlobalFactoryRegistry) RegisterPluginComponentsFactory(dialect string, factory types.ComponentsFactory) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	if _, exists := gr.pluginFactories[dialect]; exists {
		return fmt.Errorf("plugin components factory for dialect %s already registered", dialect)
	}

	gr.pluginFactories[dialect] = factory
	
	// Also register with the components registry
	gr.componentsRegistry.RegisterFactory(dialect, factory)
	
	return nil
}

// UnregisterPluginBuilder unregisters a plugin engine builder
func (gr *GlobalFactoryRegistry) UnregisterPluginBuilder(dbType string) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	if _, exists := gr.pluginBuilders[dbType]; !exists {
		return fmt.Errorf("no plugin builder registered for database type %s", dbType)
	}

	delete(gr.pluginBuilders, dbType)
	return nil
}

// UnregisterPluginComponentsFactory unregisters a plugin components factory
func (gr *GlobalFactoryRegistry) UnregisterPluginComponentsFactory(dialect string) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	if _, exists := gr.pluginFactories[dialect]; !exists {
		return fmt.Errorf("no plugin components factory registered for dialect %s", dialect)
	}

	delete(gr.pluginFactories, dialect)
	return nil
}

// GetRegisteredPluginBuilders returns a list of registered plugin builders
func (gr *GlobalFactoryRegistry) GetRegisteredPluginBuilders() []string {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	builders := make([]string, 0, len(gr.pluginBuilders))
	for dbType := range gr.pluginBuilders {
		builders = append(builders, dbType)
	}
	return builders
}

// GetRegisteredPluginFactories returns a list of registered plugin factories
func (gr *GlobalFactoryRegistry) GetRegisteredPluginFactories() []string {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	factories := make([]string, 0, len(gr.pluginFactories))
	for dialect := range gr.pluginFactories {
		factories = append(factories, dialect)
	}
	return factories
}

// IsPluginRegistered checks if a plugin is registered for the given database type
func (gr *GlobalFactoryRegistry) IsPluginRegistered(dbType string) bool {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	_, exists := gr.pluginBuilders[dbType]
	return exists
}

// CreateEngineWithPlugin creates an engine using a plugin builder if available
func (gr *GlobalFactoryRegistry) CreateEngineWithPlugin(dbType string, config types.Config) (types.MigrationEngineInterface, error) {
	gr.mutex.RLock()
	builder, exists := gr.pluginBuilders[dbType]
	gr.mutex.RUnlock()

	if !exists {
		// Fall back to the standard factory
		return gr.engineFactory.CreateEngine(dbType, config)
	}

	// Use the plugin builder
	builtEngine, err := builder.WithConfig(config).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create engine using plugin for %s: %w", dbType, err)
	}

	return builtEngine, nil
}

// FactoryInfo provides information about registered factories
type FactoryInfo struct {
	EngineTypes         []string `json:"engine_types"`
	ComponentDialects   []string `json:"component_dialects"`
	PluginBuilders      []string `json:"plugin_builders"`
	PluginFactories     []string `json:"plugin_factories"`
	TotalRegistrations  int      `json:"total_registrations"`
}

// GetFactoryInfo returns information about all registered factories
func (gr *GlobalFactoryRegistry) GetFactoryInfo() FactoryInfo {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	return FactoryInfo{
		EngineTypes:        gr.engineFactory.GetSupportedTypes(),
		ComponentDialects:  gr.componentsRegistry.GetSupportedDialects(),
		PluginBuilders:     gr.GetRegisteredPluginBuilders(),
		PluginFactories:    gr.GetRegisteredPluginFactories(),
		TotalRegistrations: len(gr.pluginBuilders) + len(gr.pluginFactories),
	}
}

// Reset clears all registrations (useful for testing)
func (gr *GlobalFactoryRegistry) Reset() {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	gr.pluginBuilders = make(map[string]types.EngineBuilder)
	gr.pluginFactories = make(map[string]types.ComponentsFactory)
	
	// Recreate the factories
	gr.engineFactory = NewMigrationEngineFactory()
	gr.componentsRegistry = NewComponentsFactoryRegistry()
}

// Convenience functions for global access

// RegisterGlobalPluginBuilder registers a plugin builder globally
func RegisterGlobalPluginBuilder(dbType string, builder types.EngineBuilder) error {
	return GetGlobalFactoryRegistry().RegisterPluginBuilder(dbType, builder)
}

// RegisterGlobalPluginComponentsFactory registers a plugin components factory globally
func RegisterGlobalPluginComponentsFactory(dialect string, factory types.ComponentsFactory) error {
	return GetGlobalFactoryRegistry().RegisterPluginComponentsFactory(dialect, factory)
}

// CreateGlobalEngine creates an engine using the global factory registry
func CreateGlobalEngine(dbType string, config types.Config) (types.MigrationEngineInterface, error) {
	return GetGlobalFactoryRegistry().CreateEngineWithPlugin(dbType, config)
}

// GetGlobalFactoryInfo returns information about the global factory registry
func GetGlobalFactoryInfo() FactoryInfo {
	return GetGlobalFactoryRegistry().GetFactoryInfo()
}

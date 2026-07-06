/**
 * @file factory.go
 * @description Cache factory implementation for creating and managing cache instances
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/factory.go
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CacheSystemFactory creates and manages the entire cache system
type CacheSystemFactory struct {
	cacheManager      CacheManager
	serializerFactory *SerializerFactory
	configs           map[string]*CacheConfig
	defaultConfig     *CacheConfig
	mutex             sync.RWMutex
	initialized       bool
}

// CacheSystemConfig holds configuration for the entire cache system
type CacheSystemConfig struct {
	DefaultCacheType     CacheType
	DefaultEvictionPolicy EvictionPolicyType
	DefaultSerializationType SerializationType
	DefaultMaxSize       int64
	DefaultTTL           time.Duration
	EnableMetrics        bool
	EnableCompression    bool
	StorageBasePath      string
	MemoryLimitMB        int64
	CleanupInterval      time.Duration
	PreloadCaches        []string
	CacheConfigs         map[string]*CacheConfig
}

// NewCacheSystemFactory creates a new cache system factory
func NewCacheSystemFactory(config *CacheSystemConfig) *CacheSystemFactory {
	if config == nil {
		config = GetDefaultCacheSystemConfig()
	}

	factory := &CacheSystemFactory{
		cacheManager:      NewCacheManager(),
		serializerFactory: NewSerializerFactory(),
		configs:           make(map[string]*CacheConfig),
		defaultConfig:     createDefaultCacheConfig(config),
	}

	// Copy provided cache configurations
	if config.CacheConfigs != nil {
		for name, cacheConfig := range config.CacheConfigs {
			factory.configs[name] = cacheConfig
		}
	}

	return factory
}

// Initialize initializes the cache system
func (f *CacheSystemFactory) Initialize(ctx context.Context) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.initialized {
		return nil
	}

	// Create default caches
	defaultCaches := []string{
		"migration_scripts",
		"migration_metadata",
		"schema_cache",
		"query_cache",
		"template_cache",
	}

	for _, cacheName := range defaultCaches {
		config := f.getConfigForCache(cacheName)
		if _, err := f.cacheManager.CreateCache(cacheName, config); err != nil {
			return fmt.Errorf("failed to create cache %s: %w", cacheName, err)
		}
	}

	f.initialized = true
	return nil
}

// GetMigrationScriptCache returns the migration script cache
func (f *CacheSystemFactory) GetMigrationScriptCache() (MigrationScriptCache, error) {
	cache, err := f.cacheManager.GetCache("migration_scripts")
	if err != nil {
		return nil, fmt.Errorf("failed to get migration script cache: %w", err)
	}

	if scriptCache, ok := cache.(MigrationScriptCache); ok {
		return scriptCache, nil
	}

	// Create a new migration script cache if the existing one is not the right type
	config := f.getConfigForCache("migration_scripts")
	config.Type = CacheTypeFlyweight // Ensure it's a flyweight cache

	scriptCache, err := NewMigrationScriptCache(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration script cache: %w", err)
	}
	
	// Replace the existing cache
	if err := f.cacheManager.RemoveCache("migration_scripts"); err != nil {
		return nil, fmt.Errorf("failed to remove existing cache: %w", err)
	}

	if _, err := f.cacheManager.CreateCache("migration_scripts", config); err != nil {
		return nil, fmt.Errorf("failed to create migration script cache: %w", err)
	}

	return scriptCache, nil
}

// GetCache returns a cache by name
func (f *CacheSystemFactory) GetCache(name string) (Cache, error) {
	return f.cacheManager.GetCache(name)
}

// CreateCache creates a new cache with the specified configuration
func (f *CacheSystemFactory) CreateCache(name string, config *CacheConfig) (Cache, error) {
	if config == nil {
		config = f.getConfigForCache(name)
	}

	return f.cacheManager.CreateCache(name, config)
}

// CreateFlyweightCache creates a new flyweight cache
func (f *CacheSystemFactory) CreateFlyweightCache(name string, config *CacheConfig) (FlyweightCache, error) {
	if config == nil {
		config = f.getConfigForCache(name)
	}

	config.Type = CacheTypeFlyweight

	cache, err := f.cacheManager.CreateCache(name, config)
	if err != nil {
		return nil, err
	}

	if flyweightCache, ok := cache.(FlyweightCache); ok {
		return flyweightCache, nil
	}

	return nil, fmt.Errorf("created cache is not a flyweight cache")
}

// CreateMigrationScriptCache creates a new migration script cache
func (f *CacheSystemFactory) CreateMigrationScriptCache(name string, config *CacheConfig) (MigrationScriptCache, error) {
	if config == nil {
		config = f.getConfigForCache(name)
	}

	config.Type = CacheTypeFlyweight
	
	scriptCache, err := NewMigrationScriptCache(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration script cache: %w", err)
	}
	
	// Register with cache manager
	if _, err := f.cacheManager.CreateCache(name, config); err != nil {
		return nil, fmt.Errorf("failed to register cache with manager: %w", err)
	}

	return scriptCache, nil
}

// GetCacheManager returns the cache manager
func (f *CacheSystemFactory) GetCacheManager() CacheManager {
	return f.cacheManager
}

// GetSerializerFactory returns the serializer factory
func (f *CacheSystemFactory) GetSerializerFactory() *SerializerFactory {
	return f.serializerFactory
}

// RegisterCacheConfig registers a cache configuration
func (f *CacheSystemFactory) RegisterCacheConfig(name string, config *CacheConfig) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.configs[name] = config
}

// GetCacheConfig returns the configuration for a cache
func (f *CacheSystemFactory) GetCacheConfig(name string) (*CacheConfig, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	if config, exists := f.configs[name]; exists {
		configCopy := *config
		return &configCopy, nil
	}

	return nil, fmt.Errorf("no configuration found for cache: %s", name)
}

// ListCaches returns all available cache names
func (f *CacheSystemFactory) ListCaches() []string {
	return f.cacheManager.ListCaches()
}

// GetStatistics returns system-wide cache statistics
func (f *CacheSystemFactory) GetStatistics() *CacheManagerStats {
	return f.cacheManager.GetStatistics()
}

// OptimizeAllCaches optimizes all caches
func (f *CacheSystemFactory) OptimizeAllCaches(ctx context.Context) error {
	return f.cacheManager.OptimizeAllCaches(ctx)
}

// Shutdown gracefully shuts down the cache system
func (f *CacheSystemFactory) Shutdown(ctx context.Context) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if !f.initialized {
		return nil
	}

	if err := f.cacheManager.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown cache manager: %w", err)
	}

	f.initialized = false
	return nil
}

// CreateCacheSystem creates a complete cache system with all components
func (f *CacheSystemFactory) CreateCacheSystem(ctx context.Context) (*CacheSystem, error) {
	if err := f.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize cache system: %w", err)
	}

	migrationCache, err := f.GetMigrationScriptCache()
	if err != nil {
		return nil, fmt.Errorf("failed to get migration script cache: %w", err)
	}

	metadataCache, err := f.GetCache("migration_metadata")
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata cache: %w", err)
	}

	schemaCache, err := f.GetCache("schema_cache")
	if err != nil {
		return nil, fmt.Errorf("failed to get schema cache: %w", err)
	}

	queryCache, err := f.GetCache("query_cache")
	if err != nil {
		return nil, fmt.Errorf("failed to get query cache: %w", err)
	}

	templateCache, err := f.GetCache("template_cache")
	if err != nil {
		return nil, fmt.Errorf("failed to get template cache: %w", err)
	}

	return &CacheSystem{
		Manager:           f.cacheManager,
		MigrationCache:    migrationCache,
		MetadataCache:     metadataCache,
		SchemaCache:       schemaCache,
		QueryCache:        queryCache,
		TemplateCache:     templateCache,
		SerializerFactory: f.serializerFactory,
		Factory:           f,
	}, nil
}

// Private methods

func (f *CacheSystemFactory) getConfigForCache(name string) *CacheConfig {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	if config, exists := f.configs[name]; exists {
		configCopy := *config
		return &configCopy
	}

	// Return a copy of default config
	defaultCopy := *f.defaultConfig
	defaultCopy.Name = name
	
	// Customize based on cache name
	switch name {
	case "migration_scripts":
		defaultCopy.Type = CacheTypeFlyweight
		defaultCopy.MaxSize = 1000
		defaultCopy.EvictionPolicy = EvictionPolicyTypeLRU
		defaultCopy.SerializationType = SerializationTypeGOB
	case "migration_metadata":
		defaultCopy.Type = CacheTypeMemory
		defaultCopy.MaxSize = 500
		defaultCopy.EvictionPolicy = EvictionPolicyTypeLFU
		defaultCopy.SerializationType = SerializationTypeJSON
	case "schema_cache":
		defaultCopy.Type = CacheTypeHybrid
		defaultCopy.MaxSize = 200
		defaultCopy.EvictionPolicy = EvictionPolicyTypeLRU
		defaultCopy.SerializationType = SerializationTypeJSON
	case "query_cache":
		defaultCopy.Type = CacheTypeMemory
		defaultCopy.MaxSize = 1000
		defaultCopy.EvictionPolicy = EvictionPolicyTypeTTL
		defaultCopy.TTL = 30 * time.Minute
		defaultCopy.SerializationType = SerializationTypeBinary
	case "template_cache":
		defaultCopy.Type = CacheTypeFile
		defaultCopy.MaxSize = 100
		defaultCopy.EvictionPolicy = EvictionPolicyTypeFIFO
		defaultCopy.SerializationType = SerializationTypeJSON
	}

	return &defaultCopy
}

func createDefaultCacheConfig(systemConfig *CacheSystemConfig) *CacheConfig {
	return &CacheConfig{
		Type:               systemConfig.DefaultCacheType,
		MaxSize:            systemConfig.DefaultMaxSize,
		TTL:                systemConfig.DefaultTTL,
		EvictionPolicy:     systemConfig.DefaultEvictionPolicy,
		SerializationType:  systemConfig.DefaultSerializationType,
		MetricsEnabled:     systemConfig.EnableMetrics,
		CompressionEnabled: systemConfig.EnableCompression,
		StoragePath:        systemConfig.StorageBasePath,
		CleanupInterval:    systemConfig.CleanupInterval,
	}
}

// CacheSystem represents a complete cache system
type CacheSystem struct {
	Manager           CacheManager
	MigrationCache    MigrationScriptCache
	MetadataCache     Cache
	SchemaCache       Cache
	QueryCache        Cache
	TemplateCache     Cache
	SerializerFactory *SerializerFactory
	Factory           *CacheSystemFactory
}

// GetCache returns a cache by name from the system
func (s *CacheSystem) GetCache(name string) (Cache, error) {
	return s.Manager.GetCache(name)
}

// CreateCache creates a new cache in the system
func (s *CacheSystem) CreateCache(name string, config *CacheConfig) (Cache, error) {
	return s.Manager.CreateCache(name, config)
}

// GetStatistics returns system statistics
func (s *CacheSystem) GetStatistics() *CacheManagerStats {
	return s.Manager.GetStatistics()
}

// Optimize optimizes all caches in the system
func (s *CacheSystem) Optimize(ctx context.Context) error {
	return s.Manager.OptimizeAllCaches(ctx)
}

// Shutdown shuts down the cache system
func (s *CacheSystem) Shutdown(ctx context.Context) error {
	return s.Manager.Shutdown(ctx)
}

// Helper functions

// GetDefaultCacheSystemConfig returns default cache system configuration
func GetDefaultCacheSystemConfig() *CacheSystemConfig {
	return &CacheSystemConfig{
		DefaultCacheType:         CacheTypeMemory,
		DefaultEvictionPolicy:    EvictionPolicyTypeLRU,
		DefaultSerializationType: SerializationTypeJSON,
		DefaultMaxSize:           1000,
		DefaultTTL:               1 * time.Hour,
		EnableMetrics:            true,
		EnableCompression:        false,
		StorageBasePath:          "./cache",
		MemoryLimitMB:            512,
		CleanupInterval:          5 * time.Minute,
		PreloadCaches:            []string{"migration_scripts", "migration_metadata"},
		CacheConfigs:             make(map[string]*CacheConfig),
	}
}

// GetProductionCacheSystemConfig returns production-optimized configuration
func GetProductionCacheSystemConfig() *CacheSystemConfig {
	return &CacheSystemConfig{
		DefaultCacheType:         CacheTypeHybrid,
		DefaultEvictionPolicy:    EvictionPolicyTypeLRU,
		DefaultSerializationType: SerializationTypeBinary,
		DefaultMaxSize:           5000,
		DefaultTTL:               4 * time.Hour,
		EnableMetrics:            true,
		EnableCompression:        true,
		StorageBasePath:          "/var/cache/migrations",
		MemoryLimitMB:            2048,
		CleanupInterval:          10 * time.Minute,
		PreloadCaches:            []string{"migration_scripts", "migration_metadata", "schema_cache"},
		CacheConfigs:             make(map[string]*CacheConfig),
	}
}

// GetDevelopmentCacheSystemConfig returns development-optimized configuration
func GetDevelopmentCacheSystemConfig() *CacheSystemConfig {
	return &CacheSystemConfig{
		DefaultCacheType:         CacheTypeMemory,
		DefaultEvictionPolicy:    EvictionPolicyTypeLRU,
		DefaultSerializationType: SerializationTypeJSON,
		DefaultMaxSize:           500,
		DefaultTTL:               30 * time.Minute,
		EnableMetrics:            true,
		EnableCompression:        false,
		StorageBasePath:          "./dev-cache",
		MemoryLimitMB:            256,
		CleanupInterval:          2 * time.Minute,
		PreloadCaches:            []string{"migration_scripts"},
		CacheConfigs:             make(map[string]*CacheConfig),
	}
}

// GetTestingCacheSystemConfig returns testing-optimized configuration
func GetTestingCacheSystemConfig() *CacheSystemConfig {
	return &CacheSystemConfig{
		DefaultCacheType:         CacheTypeMemory,
		DefaultEvictionPolicy:    EvictionPolicyTypeFIFO,
		DefaultSerializationType: SerializationTypeJSON,
		DefaultMaxSize:           100,
		DefaultTTL:               5 * time.Minute,
		EnableMetrics:            false,
		EnableCompression:        false,
		StorageBasePath:          "./test-cache",
		MemoryLimitMB:            64,
		CleanupInterval:          30 * time.Second,
		PreloadCaches:            []string{},
		CacheConfigs:             make(map[string]*CacheConfig),
	}
}

// CreateCacheSystemForEnvironment creates a cache system for a specific environment
func CreateCacheSystemForEnvironment(environment string) (*CacheSystemFactory, error) {
	var config *CacheSystemConfig

	switch environment {
	case "production", "prod":
		config = GetProductionCacheSystemConfig()
	case "development", "dev":
		config = GetDevelopmentCacheSystemConfig()
	case "testing", "test":
		config = GetTestingCacheSystemConfig()
	default:
		config = GetDefaultCacheSystemConfig()
	}

	return NewCacheSystemFactory(config), nil
}

// Global cache system factory instance
var DefaultCacheSystemFactory *CacheSystemFactory

// InitializeDefaultCacheSystem initializes the default cache system
func InitializeDefaultCacheSystem(config *CacheSystemConfig) error {
	DefaultCacheSystemFactory = NewCacheSystemFactory(config)
	return DefaultCacheSystemFactory.Initialize(context.Background())
}

// GetDefaultCacheSystem returns the default cache system
func GetDefaultCacheSystem() (*CacheSystem, error) {
	if DefaultCacheSystemFactory == nil {
		if err := InitializeDefaultCacheSystem(nil); err != nil {
			return nil, fmt.Errorf("failed to initialize default cache system: %w", err)
		}
	}

	return DefaultCacheSystemFactory.CreateCacheSystem(context.Background())
}

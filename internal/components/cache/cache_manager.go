/**
 * @file cache_manager.go
 * @description Cache manager implementation to orchestrate multiple cache instances with different strategies
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/cache_manager.go
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DefaultCacheManager implements CacheManager interface
type DefaultCacheManager struct {
	caches       map[string]Cache
	configs      map[string]*CacheConfig
	factories    map[CacheType]CacheFactory
	stats        *CacheManagerStats
	mutex        sync.RWMutex
	shutdownChan chan struct{}
	running      bool
}

// CacheFactory creates cache instances
type CacheFactory interface {
	CreateCache(config *CacheConfig) (Cache, error)
	GetCacheType() CacheType
	ValidateConfig(config *CacheConfig) error
}

// NewCacheManager creates a new cache manager
func NewCacheManager() *DefaultCacheManager {
	manager := &DefaultCacheManager{
		caches:       make(map[string]Cache),
		configs:      make(map[string]*CacheConfig),
		factories:    make(map[CacheType]CacheFactory),
		stats: &CacheManagerStats{
			CacheStats: make(map[string]*CacheStats),
			CreatedAt:  time.Now(),
		},
		shutdownChan: make(chan struct{}),
		running:      true,
	}

	// Register default factories
	manager.registerDefaultFactories()

	// Start monitoring routine
	go manager.monitoringRoutine()

	return manager
}

// GetCache returns a cache instance by name
func (m *DefaultCacheManager) GetCache(name string) (Cache, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	cache, exists := m.caches[name]
	if !exists {
		return nil, fmt.Errorf("cache not found: %s", name)
	}

	return cache, nil
}

// CreateCache creates a new cache instance with specified configuration
func (m *DefaultCacheManager) CreateCache(name string, config *CacheConfig) (Cache, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if cache already exists
	if _, exists := m.caches[name]; exists {
		return nil, fmt.Errorf("cache already exists: %s", name)
	}

	// Validate configuration
	if config == nil {
		return nil, fmt.Errorf("cache configuration cannot be nil")
	}

	// Set cache name
	config.Name = name

	// Get factory for cache type
	factory, exists := m.factories[config.Type]
	if !exists {
		return nil, fmt.Errorf("no factory registered for cache type: %s", config.Type)
	}

	// Validate configuration with factory
	if err := factory.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid cache configuration: %w", err)
	}

	// Create cache
	cache, err := factory.CreateCache(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	// Store cache and config
	m.caches[name] = cache
	m.configs[name] = config

	// Update stats
	m.stats.TotalCaches++
	m.stats.UpdatedAt = time.Now()

	return cache, nil
}

// RemoveCache removes a cache instance
func (m *DefaultCacheManager) RemoveCache(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	cache, exists := m.caches[name]
	if !exists {
		return fmt.Errorf("cache not found: %s", name)
	}

	// Close cache
	if err := cache.Close(); err != nil {
		return fmt.Errorf("failed to close cache: %w", err)
	}

	// Remove from maps
	delete(m.caches, name)
	delete(m.configs, name)
	delete(m.stats.CacheStats, name)

	// Update stats
	m.stats.TotalCaches--
	m.stats.UpdatedAt = time.Now()

	return nil
}

// ListCaches returns all available cache names
func (m *DefaultCacheManager) ListCaches() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	names := make([]string, 0, len(m.caches))
	for name := range m.caches {
		names = append(names, name)
	}

	return names
}

// GetStatistics returns cache manager statistics
func (m *DefaultCacheManager) GetStatistics() *CacheManagerStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Update cache statistics
	totalMemoryUsage := int64(0)
	for name, cache := range m.caches {
		stats := cache.GetStatistics()
		m.stats.CacheStats[name] = stats
		totalMemoryUsage += stats.MemoryUsage
	}

	m.stats.TotalMemoryUsage = totalMemoryUsage
	m.stats.UpdatedAt = time.Now()

	// Create a copy to avoid race conditions
	statsCopy := *m.stats
	statsCopy.CacheStats = make(map[string]*CacheStats)
	for name, stats := range m.stats.CacheStats {
		statsCopy.CacheStats[name] = stats
	}

	return &statsCopy
}

// Shutdown gracefully shuts down all caches
func (m *DefaultCacheManager) Shutdown(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.running {
		return nil
	}

	// Signal shutdown
	close(m.shutdownChan)
	m.running = false

	var errors []error

	// Close all caches
	for name, cache := range m.caches {
		if err := cache.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close cache %s: %w", name, err))
		}
	}

	// Clear maps
	m.caches = make(map[string]Cache)
	m.configs = make(map[string]*CacheConfig)
	m.stats.CacheStats = make(map[string]*CacheStats)
	m.stats.TotalCaches = 0
	m.stats.TotalMemoryUsage = 0

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	return nil
}

// RegisterCacheFactory registers a cache factory
func (m *DefaultCacheManager) RegisterCacheFactory(factory CacheFactory) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	cacheType := factory.GetCacheType()
	m.factories[cacheType] = factory

	return nil
}

// GetCacheConfig returns the configuration for a cache
func (m *DefaultCacheManager) GetCacheConfig(name string) (*CacheConfig, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	config, exists := m.configs[name]
	if !exists {
		return nil, fmt.Errorf("cache not found: %s", name)
	}

	// Return a copy
	configCopy := *config
	return &configCopy, nil
}

// UpdateCacheConfig updates the configuration for a cache
func (m *DefaultCacheManager) UpdateCacheConfig(name string, config *CacheConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.caches[name]; !exists {
		return fmt.Errorf("cache not found: %s", name)
	}

	// Validate configuration
	factory, exists := m.factories[config.Type]
	if !exists {
		return fmt.Errorf("no factory registered for cache type: %s", config.Type)
	}

	if err := factory.ValidateConfig(config); err != nil {
		return fmt.Errorf("invalid cache configuration: %w", err)
	}

	// Update configuration
	config.Name = name
	m.configs[name] = config

	return nil
}

// OptimizeAllCaches performs optimization on all caches
func (m *DefaultCacheManager) OptimizeAllCaches(ctx context.Context) error {
	m.mutex.RLock()
	caches := make(map[string]Cache)
	for name, cache := range m.caches {
		caches[name] = cache
	}
	m.mutex.RUnlock()

	var errors []error

	for name, cache := range caches {
		// Check if cache supports optimization
		if flyweightCache, ok := cache.(FlyweightCache); ok {
			if err := flyweightCache.OptimizeMemory(ctx); err != nil {
				errors = append(errors, fmt.Errorf("failed to optimize cache %s: %w", name, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("optimization errors: %v", errors)
	}

	return nil
}

// Private methods

func (m *DefaultCacheManager) registerDefaultFactories() {
	// Register memory cache factory
	m.factories[CacheTypeMemory] = &MemoryCacheFactory{}
	
	// Register flyweight cache factory
	m.factories[CacheTypeFlyweight] = &FlyweightCacheFactory{}
	
	// Register file cache factory
	m.factories[CacheTypeFile] = &FileCacheFactory{}
	
	// Register hybrid cache factory
	m.factories[CacheTypeHybrid] = &HybridCacheFactory{}
}

func (m *DefaultCacheManager) monitoringRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.updateStatistics()
		case <-m.shutdownChan:
			return
		}
	}
}

func (m *DefaultCacheManager) updateStatistics() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	totalMemoryUsage := int64(0)
	for name, cache := range m.caches {
		stats := cache.GetStatistics()
		m.stats.CacheStats[name] = stats
		totalMemoryUsage += stats.MemoryUsage
	}

	m.stats.TotalMemoryUsage = totalMemoryUsage
	m.stats.UpdatedAt = time.Now()
}

// Cache factory implementations

// MemoryCacheFactory creates memory caches
type MemoryCacheFactory struct{}

func (f *MemoryCacheFactory) CreateCache(config *CacheConfig) (Cache, error) {
	return NewMemoryCache(config)
}

func (f *MemoryCacheFactory) GetCacheType() CacheType {
	return CacheTypeMemory
}

func (f *MemoryCacheFactory) ValidateConfig(config *CacheConfig) error {
	if config.MaxSize <= 0 {
		return fmt.Errorf("max size must be positive")
	}
	return nil
}

// FlyweightCacheFactory creates flyweight caches
type FlyweightCacheFactory struct{}

func (f *FlyweightCacheFactory) CreateCache(config *CacheConfig) (Cache, error) {
	return NewFlyweightCache(config)
}

func (f *FlyweightCacheFactory) GetCacheType() CacheType {
	return CacheTypeFlyweight
}

func (f *FlyweightCacheFactory) ValidateConfig(config *CacheConfig) error {
	if config.MaxSize <= 0 {
		return fmt.Errorf("max size must be positive")
	}
	if config.EvictionPolicy == "" {
		return fmt.Errorf("eviction policy must be specified")
	}
	return nil
}

// FileCacheFactory creates file-based caches
type FileCacheFactory struct{}

func (f *FileCacheFactory) CreateCache(config *CacheConfig) (Cache, error) {
	return NewFileCache(config)
}

func (f *FileCacheFactory) GetCacheType() CacheType {
	return CacheTypeFile
}

func (f *FileCacheFactory) ValidateConfig(config *CacheConfig) error {
	if config.StoragePath == "" {
		return fmt.Errorf("storage path must be specified for file cache")
	}
	return nil
}

// HybridCacheFactory creates hybrid caches
type HybridCacheFactory struct{}

func (f *HybridCacheFactory) CreateCache(config *CacheConfig) (Cache, error) {
	return NewHybridCache(config)
}

func (f *HybridCacheFactory) GetCacheType() CacheType {
	return CacheTypeHybrid
}

func (f *HybridCacheFactory) ValidateConfig(config *CacheConfig) error {
	if config.MaxSize <= 0 {
		return fmt.Errorf("max size must be positive")
	}
	return nil
}

// Placeholder cache implementations

// NewMemoryCache creates a new memory cache
func NewMemoryCache(config *CacheConfig) (Cache, error) {
	return &MemoryCache{
		config: config,
		items:  make(map[string]*CacheItem),
		stats: &CacheStats{
			Name:      config.Name,
			Type:      CacheTypeMemory,
			MaxSize:   config.MaxSize,
			CreatedAt: time.Now(),
		},
	}, nil
}

// NewFileCache creates a new file cache
func NewFileCache(config *CacheConfig) (Cache, error) {
	return &FileCache{
		config: config,
		stats: &CacheStats{
			Name:      config.Name,
			Type:      CacheTypeFile,
			MaxSize:   config.MaxSize,
			CreatedAt: time.Now(),
		},
	}, nil
}

// NewHybridCache creates a new hybrid cache
func NewHybridCache(config *CacheConfig) (Cache, error) {
	return &HybridCache{
		config: config,
		stats: &CacheStats{
			Name:      config.Name,
			Type:      CacheTypeHybrid,
			MaxSize:   config.MaxSize,
			CreatedAt: time.Now(),
		},
	}, nil
}

// Placeholder cache implementations

type MemoryCache struct {
	config *CacheConfig
	items  map[string]*CacheItem
	stats  *CacheStats
	mutex  sync.RWMutex
}

func (c *MemoryCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	item, exists := c.items[key]
	if !exists || item.IsExpired() {
		c.stats.MissCount++
		return nil, false, nil
	}

	item.UpdateAccess()
	c.stats.HitCount++
	c.stats.CalculateHitRatio()
	return item.Value, true, nil
}

func (c *MemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	item := &CacheItem{
		Key:       key,
		Value:     value,
		CreatedAt: time.Now(),
		Size:      int64(len(fmt.Sprintf("%v", value))), // Simple size calculation
	}
	item.SetTTL(ttl)

	c.items[key] = item
	c.stats.ItemCount++
	c.stats.Size += item.Size
	c.stats.UpdatedAt = time.Now()

	return nil
}

func (c *MemoryCache) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if item, exists := c.items[key]; exists {
		delete(c.items, key)
		c.stats.ItemCount--
		c.stats.Size -= item.Size
		c.stats.UpdatedAt = time.Now()
	}

	return nil
}

func (c *MemoryCache) Clear(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items = make(map[string]*CacheItem)
	c.stats.ItemCount = 0
	c.stats.Size = 0
	c.stats.UpdatedAt = time.Now()

	return nil
}

func (c *MemoryCache) Exists(ctx context.Context, key string) (bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	item, exists := c.items[key]
	return exists && !item.IsExpired(), nil
}

func (c *MemoryCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}

	return keys, nil
}

func (c *MemoryCache) Size(ctx context.Context) (int64, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.stats.ItemCount, nil
}

func (c *MemoryCache) GetStatistics() *CacheStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	statsCopy := *c.stats
	return &statsCopy
}

func (c *MemoryCache) GetConfig() *CacheConfig {
	return c.config
}

func (c *MemoryCache) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items = nil
	return nil
}

// Placeholder implementations for FileCache and HybridCache
type FileCache struct {
	config *CacheConfig
	stats  *CacheStats
}

func (c *FileCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	// File-based cache implementation
	return nil, false, nil
}

func (c *FileCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return nil
}

func (c *FileCache) Delete(ctx context.Context, key string) error {
	return nil
}

func (c *FileCache) Clear(ctx context.Context) error {
	return nil
}

func (c *FileCache) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}

func (c *FileCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	return []string{}, nil
}

func (c *FileCache) Size(ctx context.Context) (int64, error) {
	return 0, nil
}

func (c *FileCache) GetStatistics() *CacheStats {
	return c.stats
}

func (c *FileCache) GetConfig() *CacheConfig {
	return c.config
}

func (c *FileCache) Close() error {
	return nil
}

type HybridCache struct {
	config *CacheConfig
	stats  *CacheStats
}

func (c *HybridCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	// Hybrid cache implementation
	return nil, false, nil
}

func (c *HybridCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return nil
}

func (c *HybridCache) Delete(ctx context.Context, key string) error {
	return nil
}

func (c *HybridCache) Clear(ctx context.Context) error {
	return nil
}

func (c *HybridCache) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}

func (c *HybridCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	return []string{}, nil
}

func (c *HybridCache) Size(ctx context.Context) (int64, error) {
	return 0, nil
}

func (c *HybridCache) GetStatistics() *CacheStats {
	return c.stats
}

func (c *HybridCache) GetConfig() *CacheConfig {
	return c.config
}

func (c *HybridCache) Close() error {
	return nil
}

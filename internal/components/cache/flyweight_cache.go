/**
 * @file flyweight_cache.go
 * @description Flyweight cache implementation for migration scripts with memory optimization
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/flyweight_cache.go
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DefaultFlyweightCache implements FlyweightCache interface
type DefaultFlyweightCache struct {
	config      *CacheConfig
	flyweights  map[string]Flyweight
	intrinsics  map[interface{}]string // Maps intrinsic state to key
	stats       *CacheStats
	mutex       sync.RWMutex
	metrics     CacheMetrics
	serializer  CacheSerializer
	eviction    CacheEvictionPolicy
	lastCleanup time.Time
	closed      bool
}

// NewFlyweightCache creates a new flyweight cache
func NewFlyweightCache(config *CacheConfig) (*DefaultFlyweightCache, error) {
	if config == nil {
		config = DefaultFlyweightCacheConfig()
	}

	cache := &DefaultFlyweightCache{
		config:     config,
		flyweights: make(map[string]Flyweight),
		intrinsics: make(map[interface{}]string),
		stats: &CacheStats{
			Name:      config.Name,
			Type:      CacheTypeFlyweight,
			MaxSize:   config.MaxSize,
			CreatedAt: time.Now(),
		},
		lastCleanup: time.Now(),
	}

	// Initialize components
	if err := cache.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize cache components: %w", err)
	}

	// Start cleanup routine
	if config.CleanupInterval > 0 {
		go cache.cleanupRoutine()
	}

	return cache, nil
}

// Get retrieves a value from cache
func (c *DefaultFlyweightCache) Get(ctx context.Context, key string) (interface{}, bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.closed {
		return nil, false, fmt.Errorf("cache is closed")
	}

	flyweight, exists := c.flyweights[key]
	if !exists {
		c.stats.MissCount++
		if c.metrics != nil {
			c.metrics.RecordMiss(c.config.Name)
		}
		return nil, false, nil
	}

	flyweight.UpdateLastAccessed()
	c.stats.HitCount++
	c.stats.CalculateHitRatio()

	if c.metrics != nil {
		c.metrics.RecordHit(c.config.Name)
	}

	return flyweight, true, nil
}

// Set stores a value in cache
func (c *DefaultFlyweightCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return fmt.Errorf("cache is closed")
	}

	flyweight, ok := value.(Flyweight)
	if !ok {
		return fmt.Errorf("value must implement Flyweight interface")
	}

	// Check if we need to evict items
	if err := c.ensureCapacity(); err != nil {
		return fmt.Errorf("failed to ensure capacity: %w", err)
	}

	// Store flyweight
	c.flyweights[key] = flyweight

	// Store intrinsic state mapping
	intrinsicState := flyweight.GetIntrinsicState()
	c.intrinsics[intrinsicState] = key

	// Update stats
	c.stats.ItemCount++
	c.stats.Size += flyweight.GetSize()
	c.stats.UpdatedAt = time.Now()

	return nil
}

// Delete removes a value from cache
func (c *DefaultFlyweightCache) Delete(ctx context.Context, key string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return fmt.Errorf("cache is closed")
	}

	flyweight, exists := c.flyweights[key]
	if !exists {
		return nil
	}

	// Remove intrinsic state mapping
	intrinsicState := flyweight.GetIntrinsicState()
	delete(c.intrinsics, intrinsicState)

	// Remove flyweight
	delete(c.flyweights, key)

	// Update stats
	c.stats.ItemCount--
	c.stats.Size -= flyweight.GetSize()
	c.stats.UpdatedAt = time.Now()

	return nil
}

// Clear removes all values from cache
func (c *DefaultFlyweightCache) Clear(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return fmt.Errorf("cache is closed")
	}

	c.flyweights = make(map[string]Flyweight)
	c.intrinsics = make(map[interface{}]string)

	c.stats.ItemCount = 0
	c.stats.Size = 0
	c.stats.UpdatedAt = time.Now()

	return nil
}

// Exists checks if a key exists in cache
func (c *DefaultFlyweightCache) Exists(ctx context.Context, key string) (bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.closed {
		return false, fmt.Errorf("cache is closed")
	}

	_, exists := c.flyweights[key]
	return exists, nil
}

// Keys returns all keys in cache
func (c *DefaultFlyweightCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("cache is closed")
	}

	keys := make([]string, 0, len(c.flyweights))
	for key := range c.flyweights {
		// Simple pattern matching (could be enhanced with regex)
		if pattern == "" || pattern == "*" {
			keys = append(keys, key)
		}
		// Add more sophisticated pattern matching if needed
	}

	return keys, nil
}

// Size returns the number of items in cache
func (c *DefaultFlyweightCache) Size(ctx context.Context) (int64, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if c.closed {
		return 0, fmt.Errorf("cache is closed")
	}

	return c.stats.ItemCount, nil
}

// GetStatistics returns cache statistics
func (c *DefaultFlyweightCache) GetStatistics() *CacheStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Create a copy to avoid race conditions
	statsCopy := *c.stats
	statsCopy.CalculateHitRatio()
	return &statsCopy
}

// GetConfig returns cache configuration
func (c *DefaultFlyweightCache) GetConfig() *CacheConfig {
	return c.config
}

// Close closes the cache
func (c *DefaultFlyweightCache) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.flyweights = nil
	c.intrinsics = nil

	return nil
}

// GetFlyweight retrieves or creates a flyweight object
func (c *DefaultFlyweightCache) GetFlyweight(ctx context.Context, key string, factory FlyweightFactory) (Flyweight, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return nil, fmt.Errorf("cache is closed")
	}

	// Check if flyweight already exists
	if flyweight, exists := c.flyweights[key]; exists {
		flyweight.UpdateLastAccessed()
		c.stats.HitCount++
		if c.metrics != nil {
			c.metrics.RecordHit(c.config.Name)
		}
		return flyweight, nil
	}

	c.stats.MissCount++
	if c.metrics != nil {
		c.metrics.RecordMiss(c.config.Name)
	}

	return nil, fmt.Errorf("flyweight not found for key: %s", key)
}

// GetOrCreateFlyweight gets existing flyweight or creates new one
func (c *DefaultFlyweightCache) GetOrCreateFlyweight(ctx context.Context, key string, intrinsicState interface{}, factory FlyweightFactory) (Flyweight, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return nil, fmt.Errorf("cache is closed")
	}

	// Check if flyweight with this intrinsic state already exists
	if existingKey, exists := c.intrinsics[intrinsicState]; exists {
		if flyweight, exists := c.flyweights[existingKey]; exists {
			flyweight.UpdateLastAccessed()
			c.stats.HitCount++
			if c.metrics != nil {
				c.metrics.RecordHit(c.config.Name)
			}
			return flyweight, nil
		}
	}

	// Check if flyweight exists by key
	if flyweight, exists := c.flyweights[key]; exists {
		flyweight.UpdateLastAccessed()
		c.stats.HitCount++
		if c.metrics != nil {
			c.metrics.RecordHit(c.config.Name)
		}
		return flyweight, nil
	}

	// Create new flyweight
	flyweight, err := factory.CreateFlyweight(intrinsicState)
	if err != nil {
		return nil, fmt.Errorf("failed to create flyweight: %w", err)
	}

	// Ensure capacity
	if err := c.ensureCapacity(); err != nil {
		return nil, fmt.Errorf("failed to ensure capacity: %w", err)
	}

	// Store flyweight
	c.flyweights[key] = flyweight
	c.intrinsics[intrinsicState] = key

	// Update stats
	c.stats.ItemCount++
	c.stats.Size += flyweight.GetSize()
	c.stats.MissCount++
	c.stats.UpdatedAt = time.Now()

	if c.metrics != nil {
		c.metrics.RecordMiss(c.config.Name)
	}

	return flyweight, nil
}

// GetFlyweightCount returns the number of flyweight objects
func (c *DefaultFlyweightCache) GetFlyweightCount() int64 {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return int64(len(c.flyweights))
}

// GetIntrinsicStates returns all intrinsic states
func (c *DefaultFlyweightCache) GetIntrinsicStates() []interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	states := make([]interface{}, 0, len(c.intrinsics))
	for state := range c.intrinsics {
		states = append(states, state)
	}

	return states
}

// OptimizeMemory performs memory optimization
func (c *DefaultFlyweightCache) OptimizeMemory(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.closed {
		return fmt.Errorf("cache is closed")
	}

	// Remove expired items
	var removedCount int64
	var removedSize int64

	for key, flyweight := range c.flyweights {
		// Check if flyweight should be evicted based on policy
		if c.eviction != nil {
			item := &CacheItem{
				Key:          key,
				Value:        flyweight,
				CreatedAt:    flyweight.GetCreatedAt(),
				LastAccessed: flyweight.GetLastAccessed(),
				Size:         flyweight.GetSize(),
			}

			if c.eviction.ShouldEvict(item, c.stats) {
				intrinsicState := flyweight.GetIntrinsicState()
				delete(c.intrinsics, intrinsicState)
				delete(c.flyweights, key)

				removedCount++
				removedSize += flyweight.GetSize()
			}
		}
	}

	// Update stats
	c.stats.ItemCount -= removedCount
	c.stats.Size -= removedSize
	c.stats.EvictionCount += removedCount
	c.stats.UpdatedAt = time.Now()

	if c.metrics != nil {
		for i := int64(0); i < removedCount; i++ {
			c.metrics.RecordEviction(c.config.Name, "optimization")
		}
	}

	return nil
}

// Private methods

func (c *DefaultFlyweightCache) initializeComponents() error {
	// Initialize serializer
	if c.config.SerializationType != "" {
		serializer, err := NewSerializer(c.config.SerializationType)
		if err != nil {
			return fmt.Errorf("failed to create serializer: %w", err)
		}
		c.serializer = serializer
	}

	// Initialize eviction policy
	if c.config.EvictionPolicy != "" {
		policy := NewEvictionPolicy(c.config.EvictionPolicy, c.config.MaxSize)
		c.eviction = policy
	}

	// Initialize metrics if enabled
	if c.config.MetricsEnabled {
		c.metrics = NewCacheMetrics(nil) // Use default config
	}

	return nil
}

func (c *DefaultFlyweightCache) ensureCapacity() error {
	if c.config.MaxSize <= 0 {
		return nil // No size limit
	}

	if c.stats.ItemCount >= c.config.MaxSize {
		// Need to evict items
		return c.evictItems()
	}

	return nil
}

func (c *DefaultFlyweightCache) evictItems() error {
	if c.eviction == nil {
		return fmt.Errorf("no eviction policy configured")
	}

	// Collect items for eviction consideration
	items := make([]*CacheItem, 0, len(c.flyweights))
	for key, flyweight := range c.flyweights {
		item := &CacheItem{
			Key:          key,
			Value:        flyweight,
			CreatedAt:    flyweight.GetCreatedAt(),
			LastAccessed: flyweight.GetLastAccessed(),
			Size:         flyweight.GetSize(),
		}
		items = append(items, item)
	}

	// Get eviction candidates
	candidates := c.eviction.GetEvictionCandidates(items, c.stats)

	// Evict items
	batchSize := c.config.EvictionBatchSize
	if batchSize <= 0 {
		batchSize = 10 // Default batch size
	}

	evictedCount := 0
	for _, item := range candidates {
		if evictedCount >= batchSize {
			break
		}

		flyweight := item.Value.(Flyweight)
		intrinsicState := flyweight.GetIntrinsicState()

		delete(c.intrinsics, intrinsicState)
		delete(c.flyweights, item.Key)

		c.stats.ItemCount--
		c.stats.Size -= item.Size
		c.stats.EvictionCount++

		if c.metrics != nil {
			c.metrics.RecordEviction(c.config.Name, "capacity")
		}

		evictedCount++
	}

	c.stats.UpdatedAt = time.Now()
	return nil
}

func (c *DefaultFlyweightCache) cleanupRoutine() {
	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if c.closed {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.OptimizeMemory(ctx); err != nil {
				// Log error (would need logger interface)
			}
			cancel()

			c.mutex.Lock()
			c.lastCleanup = time.Now()
			c.stats.LastCleanup = c.lastCleanup
			c.mutex.Unlock()
		}
	}
}

// DefaultFlyweightCacheConfig returns default configuration for flyweight cache
func DefaultFlyweightCacheConfig() *CacheConfig {
	return &CacheConfig{
		Name:               "default_flyweight",
		Type:               CacheTypeFlyweight,
		MaxSize:            1000,
		TTL:                30 * time.Minute,
		CleanupInterval:    5 * time.Minute,
		EvictionPolicy:     EvictionPolicyLRU,
		EvictionBatchSize:  10,
		SerializationType:  SerializationTypeJSON,
		CompressionEnabled: false,
		EncryptionEnabled:  false,
		ShardCount:         1,
		BufferSize:         100,
		AsyncWrites:        false,
		PersistentStorage:  false,
		MetricsEnabled:     true,
		StatisticsEnabled:  true,
		Properties:         make(map[string]interface{}),
	}
}

// Placeholder implementations for dependencies

// NewSerializer creates a serializer based on type
func NewSerializer(serializationType SerializationType) (CacheSerializer, error) {
	switch serializationType {
	case SerializationTypeJSON:
		return &JSONSerializer{}, nil
	case SerializationTypeGOB:
		return &GOBSerializer{}, nil
	default:
		return &JSONSerializer{}, nil
	}
}

type GOBSerializer struct{}

func (s *GOBSerializer) Serialize(obj interface{}) ([]byte, error) {
	// Implementation would use gob encoding
	return nil, nil
}

func (s *GOBSerializer) Deserialize(data []byte, target interface{}) error {
	// Implementation would use gob decoding
	return nil
}

func (s *GOBSerializer) GetContentType() string {
	return "application/gob"
}

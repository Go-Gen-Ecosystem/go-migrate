/**
 * @file interfaces.go
 * @description Cache interfaces for Flyweight pattern implementation for migration scripts
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/cache/interfaces.go
 */

package cache

import (
	"context"
	"time"
)

// CacheManager manages multiple cache instances with different strategies
type CacheManager interface {
	// GetCache returns a cache instance by name
	GetCache(name string) (Cache, error)

	// CreateCache creates a new cache instance with specified configuration
	CreateCache(name string, config *CacheConfig) (Cache, error)

	// RemoveCache removes a cache instance
	RemoveCache(name string) error

	// ListCaches returns all available cache names
	ListCaches() []string

	// GetStatistics returns cache manager statistics
	GetStatistics() *CacheManagerStats

	// Shutdown gracefully shuts down all caches
	Shutdown(ctx context.Context) error

	// OptimizeAllCaches performs optimization on all caches
	OptimizeAllCaches(ctx context.Context) error
}

// Cache represents a generic cache interface
type Cache interface {
	// Get retrieves a value from cache
	Get(ctx context.Context, key string) (interface{}, bool, error)

	// Set stores a value in cache
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Delete removes a value from cache
	Delete(ctx context.Context, key string) error

	// Clear removes all values from cache
	Clear(ctx context.Context) error

	// Exists checks if a key exists in cache
	Exists(ctx context.Context, key string) (bool, error)

	// Keys returns all keys in cache (use with caution for large caches)
	Keys(ctx context.Context, pattern string) ([]string, error)

	// Size returns the number of items in cache
	Size(ctx context.Context) (int64, error)

	// GetStatistics returns cache statistics
	GetStatistics() *CacheStats

	// GetConfig returns cache configuration
	GetConfig() *CacheConfig

	// Close closes the cache
	Close() error
}

// FlyweightCache specialized cache for Flyweight pattern implementation
type FlyweightCache interface {
	Cache

	// GetFlyweight retrieves or creates a flyweight object
	GetFlyweight(ctx context.Context, key string, factory FlyweightFactory) (Flyweight, error)

	// GetOrCreateFlyweight gets existing flyweight or creates new one
	GetOrCreateFlyweight(ctx context.Context, key string, intrinsicState interface{}, factory FlyweightFactory) (Flyweight, error)

	// GetFlyweightCount returns the number of flyweight objects
	GetFlyweightCount() int64

	// GetIntrinsicStates returns all intrinsic states
	GetIntrinsicStates() []interface{}

	// OptimizeMemory performs memory optimization
	OptimizeMemory(ctx context.Context) error
}

// MigrationScriptCache specialized cache for migration scripts
type MigrationScriptCache interface {
	FlyweightCache

	// GetScript retrieves a migration script
	GetScript(ctx context.Context, scriptID string) (*MigrationScript, error)

	// CacheScript caches a migration script
	CacheScript(ctx context.Context, script *MigrationScript) error

	// GetScriptsByVersion retrieves scripts by version pattern
	GetScriptsByVersion(ctx context.Context, versionPattern string) ([]*MigrationScript, error)

	// GetScriptsByType retrieves scripts by type
	GetScriptsByType(ctx context.Context, scriptType ScriptType) ([]*MigrationScript, error)

	// InvalidateScript invalidates a cached script
	InvalidateScript(ctx context.Context, scriptID string) error

	// PreloadScripts preloads scripts based on criteria
	PreloadScripts(ctx context.Context, criteria *PreloadCriteria) error

	// GetScriptStatistics returns script cache statistics
	GetScriptStatistics() *ScriptCacheStats
}

// Flyweight represents a flyweight object in the pattern
type Flyweight interface {
	// GetIntrinsicState returns the intrinsic state
	GetIntrinsicState() interface{}

	// Operation performs an operation with extrinsic state
	Operation(extrinsicState interface{}) error

	// GetSize returns the memory size of the flyweight
	GetSize() int64

	// GetCreatedAt returns when the flyweight was created
	GetCreatedAt() time.Time

	// GetLastAccessed returns when the flyweight was last accessed
	GetLastAccessed() time.Time

	// UpdateLastAccessed updates the last accessed time
	UpdateLastAccessed()
}

// FlyweightFactory creates flyweight objects
type FlyweightFactory interface {
	// CreateFlyweight creates a new flyweight with intrinsic state
	CreateFlyweight(intrinsicState interface{}) (Flyweight, error)

	// GetFlyweightType returns the type of flyweight this factory creates
	GetFlyweightType() string

	// ValidateIntrinsicState validates the intrinsic state
	ValidateIntrinsicState(state interface{}) error
}

// CacheEvictionPolicy defines cache eviction strategies
type CacheEvictionPolicy interface {
	// ShouldEvict determines if an item should be evicted
	ShouldEvict(item *CacheItem, stats *CacheStats) bool

	// GetEvictionCandidates returns items that should be evicted
	GetEvictionCandidates(items []*CacheItem, stats *CacheStats) []*CacheItem

	// GetPolicyName returns the policy name
	GetPolicyName() string
}

// CacheSerializer handles serialization/deserialization of cache items
type CacheSerializer interface {
	// Serialize converts an object to bytes
	Serialize(obj interface{}) ([]byte, error)

	// Deserialize converts bytes to an object
	Deserialize(data []byte, target interface{}) error

	// GetContentType returns the content type
	GetContentType() string
}

// CacheMetrics collects cache metrics
type CacheMetrics interface {
	// RecordHit records a cache hit
	RecordHit(cacheName string)

	// RecordMiss records a cache miss
	RecordMiss(cacheName string)

	// RecordEviction records a cache eviction
	RecordEviction(cacheName string, reason string)

	// RecordOperation records a cache operation
	RecordOperation(cacheName string, operation string, duration time.Duration)

	// GetMetrics returns current metrics
	GetMetrics(cacheName string) *CacheMetrics
}

// Data structures

// CacheConfig configuration for cache instances
type CacheConfig struct {
	// Basic configuration
	Name            string        `json:"name"`
	Type            CacheType     `json:"type"`
	MaxSize         int64         `json:"max_size"`
	TTL             time.Duration `json:"ttl"`
	CleanupInterval time.Duration `json:"cleanup_interval"`

	// Eviction policy
	EvictionPolicy    EvictionPolicyType `json:"eviction_policy"`
	EvictionBatchSize int                `json:"eviction_batch_size"`

	// Serialization
	SerializationType  SerializationType `json:"serialization_type"`
	CompressionEnabled bool              `json:"compression_enabled"`
	EncryptionEnabled  bool              `json:"encryption_enabled"`
	EncryptionKey      string            `json:"encryption_key"`

	// Performance
	ShardCount  int  `json:"shard_count"`
	BufferSize  int  `json:"buffer_size"`
	AsyncWrites bool `json:"async_writes"`

	// Persistence (for persistent caches)
	PersistentStorage bool          `json:"persistent_storage"`
	StoragePath       string        `json:"storage_path"`
	SyncInterval      time.Duration `json:"sync_interval"`

	// Monitoring
	MetricsEnabled    bool `json:"metrics_enabled"`
	StatisticsEnabled bool `json:"statistics_enabled"`

	// Custom properties
	Properties map[string]interface{} `json:"properties"`
}

// CacheItem represents an item in the cache
type CacheItem struct {
	Key          string                 `json:"key"`
	Value        interface{}            `json:"value"`
	CreatedAt    time.Time              `json:"created_at"`
	LastAccessed time.Time              `json:"last_accessed"`
	AccessCount  int64                  `json:"access_count"`
	TTL          time.Duration          `json:"ttl"`
	ExpiresAt    time.Time              `json:"expires_at"`
	Size         int64                  `json:"size"`
	Tags         []string               `json:"tags"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// CacheStats cache statistics
type CacheStats struct {
	Name          string    `json:"name"`
	Type          CacheType `json:"type"`
	Size          int64     `json:"size"`
	MaxSize       int64     `json:"max_size"`
	ItemCount     int64     `json:"item_count"`
	HitCount      int64     `json:"hit_count"`
	MissCount     int64     `json:"miss_count"`
	EvictionCount int64     `json:"eviction_count"`
	HitRatio      float64   `json:"hit_ratio"`
	MemoryUsage   int64     `json:"memory_usage"`
	LastCleanup   time.Time `json:"last_cleanup"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CacheManagerStats cache manager statistics
type CacheManagerStats struct {
	TotalCaches      int                    `json:"total_caches"`
	TotalMemoryUsage int64                  `json:"total_memory_usage"`
	CacheStats       map[string]*CacheStats `json:"cache_stats"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// MigrationScript represents a migration script flyweight
type MigrationScript struct {
	// Intrinsic state (shared)
	ID       string     `json:"id"`
	Version  string     `json:"version"`
	Type     ScriptType `json:"type"`
	Content  string     `json:"content"`
	Checksum string     `json:"checksum"`
	Size     int64      `json:"size"`

	// Metadata
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Tags       []string               `json:"tags"`
	Properties map[string]interface{} `json:"properties"`

	// Flyweight metadata
	flyweightCreatedAt    time.Time
	flyweightLastAccessed time.Time
}

// ScriptCacheStats migration script cache statistics
type ScriptCacheStats struct {
	CacheStats
	ScriptCount       int64                `json:"script_count"`
	ScriptsByType     map[ScriptType]int64 `json:"scripts_by_type"`
	AverageScriptSize int64                `json:"average_script_size"`
	TotalScriptSize   int64                `json:"total_script_size"`
	PreloadedScripts  int64                `json:"preloaded_scripts"`
}

// PreloadCriteria criteria for preloading scripts
type PreloadCriteria struct {
	VersionPattern string                 `json:"version_pattern"`
	ScriptTypes    []ScriptType           `json:"script_types"`
	Tags           []string               `json:"tags"`
	MaxCount       int                    `json:"max_count"`
	SortBy         string                 `json:"sort_by"`
	SortOrder      string                 `json:"sort_order"`
	Properties     map[string]interface{} `json:"properties"`
}

// Enums

// CacheType defines the type of cache
type CacheType string

const (
	CacheTypeMemory    CacheType = "memory"
	CacheTypeRedis     CacheType = "redis"
	CacheTypeFile      CacheType = "file"
	CacheTypeHybrid    CacheType = "hybrid"
	CacheTypeFlyweight CacheType = "flyweight"
)

// EvictionPolicyType defines eviction policy types
type EvictionPolicyType string

const (
	EvictionPolicyLRU    EvictionPolicyType = "lru"
	EvictionPolicyLFU    EvictionPolicyType = "lfu"
	EvictionPolicyFIFO   EvictionPolicyType = "fifo"
	EvictionPolicyTTL    EvictionPolicyType = "ttl"
	EvictionPolicyRandom EvictionPolicyType = "random"
	EvictionPolicySize   EvictionPolicyType = "size"

	// Alternative naming convention used in some parts of the codebase
	EvictionPolicyTypeLRU    = EvictionPolicyLRU
	EvictionPolicyTypeLFU    = EvictionPolicyLFU
	EvictionPolicyTypeFIFO   = EvictionPolicyFIFO
	EvictionPolicyTypeTTL    = EvictionPolicyTTL
	EvictionPolicyTypeRandom = EvictionPolicyRandom
	EvictionPolicyTypeSize   = EvictionPolicySize
)

// SerializationType defines serialization types
type SerializationType string

const (
	SerializationTypeJSON     SerializationType = "json"
	SerializationTypeGOB      SerializationType = "gob"
	SerializationTypeBinary   SerializationType = "binary"
	SerializationTypeMsgPack  SerializationType = "msgpack"
	SerializationTypeProtobuf SerializationType = "protobuf"
)

// ScriptType defines migration script types
type ScriptType string

const (
	ScriptTypeUp       ScriptType = "up"
	ScriptTypeDown     ScriptType = "down"
	ScriptTypeSeed     ScriptType = "seed"
	ScriptTypeRollback ScriptType = "rollback"
	ScriptTypeSchema   ScriptType = "schema"
	ScriptTypeData     ScriptType = "data"
	ScriptTypeCustom   ScriptType = "custom"
)

// Implement Flyweight interface for MigrationScript
func (ms *MigrationScript) GetIntrinsicState() interface{} {
	return map[string]interface{}{
		"id":       ms.ID,
		"version":  ms.Version,
		"type":     ms.Type,
		"content":  ms.Content,
		"checksum": ms.Checksum,
		"size":     ms.Size,
	}
}

func (ms *MigrationScript) Operation(extrinsicState interface{}) error {
	// Extrinsic state could include execution context, database connection, etc.
	ms.UpdateLastAccessed()
	return nil
}

func (ms *MigrationScript) GetSize() int64 {
	return ms.Size
}

func (ms *MigrationScript) GetCreatedAt() time.Time {
	return ms.flyweightCreatedAt
}

func (ms *MigrationScript) GetLastAccessed() time.Time {
	return ms.flyweightLastAccessed
}

func (ms *MigrationScript) UpdateLastAccessed() {
	ms.flyweightLastAccessed = time.Now()
}

// Helper methods for CacheItem
func (ci *CacheItem) IsExpired() bool {
	if ci.TTL <= 0 {
		return false
	}
	return time.Now().After(ci.ExpiresAt)
}

func (ci *CacheItem) UpdateAccess() {
	ci.LastAccessed = time.Now()
	ci.AccessCount++
}

func (ci *CacheItem) SetTTL(ttl time.Duration) {
	ci.TTL = ttl
	if ttl > 0 {
		ci.ExpiresAt = time.Now().Add(ttl)
	}
}

// Helper methods for CacheStats
func (cs *CacheStats) CalculateHitRatio() {
	total := cs.HitCount + cs.MissCount
	if total > 0 {
		cs.HitRatio = float64(cs.HitCount) / float64(total)
	} else {
		cs.HitRatio = 0.0
	}
}

func (cs *CacheStats) GetUtilization() float64 {
	if cs.MaxSize > 0 {
		return float64(cs.Size) / float64(cs.MaxSize)
	}
	return 0.0
}

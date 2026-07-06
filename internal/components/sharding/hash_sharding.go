/**
 * @file hash_sharding.go
 * @description Hash-based sharding strategy with consistent hashing
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/hash_sharding.go
 */

package sharding

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
)

// HashShardingStrategy implements consistent hashing for horizontal sharding
type HashShardingStrategy struct {
	registry       TenantRegistry
	shards         map[string]*Shard // ShardID -> Shard
	hashRing       *ConsistentHashRing
	config         *ShardingConfig
	mu             sync.RWMutex
	healthCache    map[string]*ShardHealth
	healthCacheMu  sync.RWMutex
	metricsCache   map[string]*ShardMetrics
	metricsCacheMu sync.RWMutex
}

// ConsistentHashRing implements a consistent hash ring for shard distribution
type ConsistentHashRing struct {
	nodes          map[uint32]string // Hash -> ShardID
	sortedHashes   []uint32
	virtualNodes   int
	hashFunc       func(string) uint32
	mu             sync.RWMutex
}

// NewHashShardingStrategy creates a new hash-based sharding strategy
func NewHashShardingStrategy(registry TenantRegistry, config *ShardingConfig) *HashShardingStrategy {
	if config == nil {
		config = DefaultShardingConfig()
	}
	config.Strategy = ShardTypeHashBased

	// Determine hash function
	var hashFunc func(string) uint32
	switch config.TenantIDHashAlgorithm {
	case "xxhash":
		hashFunc = xxhashFunc
	case "fnv":
		hashFunc = fnvHashFunc
	case "md5":
		hashFunc = md5HashFunc
	default:
		hashFunc = xxhashFunc // Default to xxhash
	}

	return &HashShardingStrategy{
		registry: registry,
		shards:   make(map[string]*Shard),
		hashRing: &ConsistentHashRing{
			nodes:        make(map[uint32]string),
			sortedHashes: make([]uint32, 0),
			virtualNodes: 150, // Default virtual nodes per physical node
			hashFunc:     hashFunc,
		},
		config:       config,
		healthCache:  make(map[string]*ShardHealth),
		metricsCache: make(map[string]*ShardMetrics),
	}
}

// Hash functions

func xxhashFunc(key string) uint32 {
	h := xxhash.Sum64String(key)
	return uint32(h & 0xFFFFFFFF)
}

func fnvHashFunc(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

func md5HashFunc(key string) uint32 {
	hash := md5.Sum([]byte(key))
	return binary.BigEndian.Uint32(hash[:4])
}

// ConsistentHashRing methods

// Add adds a shard to the hash ring with virtual nodes
func (r *ConsistentHashRing) Add(shardID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Add virtual nodes
	for i := 0; i < r.virtualNodes; i++ {
		virtualKey := fmt.Sprintf("%s#%d", shardID, i)
		hash := r.hashFunc(virtualKey)
		r.nodes[hash] = shardID
		r.sortedHashes = append(r.sortedHashes, hash)
	}

	// Sort hashes for binary search
	sort.Slice(r.sortedHashes, func(i, j int) bool {
		return r.sortedHashes[i] < r.sortedHashes[j]
	})
}

// Remove removes a shard from the hash ring
func (r *ConsistentHashRing) Remove(shardID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove all virtual nodes
	newHashes := make([]uint32, 0)
	for hash, sid := range r.nodes {
		if sid == shardID {
			delete(r.nodes, hash)
		} else {
			newHashes = append(newHashes, hash)
		}
	}

	r.sortedHashes = newHashes
	sort.Slice(r.sortedHashes, func(i, j int) bool {
		return r.sortedHashes[i] < r.sortedHashes[j]
	})
}

// Get returns the shard ID for a given key
func (r *ConsistentHashRing) Get(key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sortedHashes) == 0 {
		return "", fmt.Errorf("no shards available in hash ring")
	}

	hash := r.hashFunc(key)

	// Binary search for the first hash >= key hash
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= hash
	})

	// Wrap around to the beginning if needed
	if idx >= len(r.sortedHashes) {
		idx = 0
	}

	shardID := r.nodes[r.sortedHashes[idx]]
	return shardID, nil
}

// GetN returns N shards for a key (for replication)
func (r *ConsistentHashRing) GetN(key string, n int) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sortedHashes) == 0 {
		return nil, fmt.Errorf("no shards available in hash ring")
	}

	if n <= 0 {
		n = 1
	}

	hash := r.hashFunc(key)
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= hash
	})

	if idx >= len(r.sortedHashes) {
		idx = 0
	}

	// Collect unique shards
	shardSet := make(map[string]bool)
	shards := make([]string, 0, n)

	for len(shards) < n && len(shardSet) < len(r.nodes)/r.virtualNodes {
		shardID := r.nodes[r.sortedHashes[idx]]
		if !shardSet[shardID] {
			shardSet[shardID] = true
			shards = append(shards, shardID)
		}

		idx = (idx + 1) % len(r.sortedHashes)
	}

	return shards, nil
}

// GetDistribution returns tenant distribution across shards
func (r *ConsistentHashRing) GetDistribution(tenantIDs []string) map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	distribution := make(map[string]int)

	for _, tenantID := range tenantIDs {
		shardID, err := r.Get(tenantID)
		if err == nil {
			distribution[shardID]++
		}
	}

	return distribution
}

// HashShardingStrategy methods

// GetShards returns all available shards
func (s *HashShardingStrategy) GetShards(ctx context.Context) ([]Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shards := make([]Shard, 0, len(s.shards))
	for _, shard := range s.shards {
		shards = append(shards, *shard)
	}

	return shards, nil
}

// GetShard returns a specific shard by ID
func (s *HashShardingStrategy) GetShard(ctx context.Context, shardID string) (*Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return nil, fmt.Errorf("shard %s not found", shardID)
	}

	shardCopy := *shard
	return &shardCopy, nil
}

// RouteToShard determines which shard a tenant should use via consistent hashing
func (s *HashShardingStrategy) RouteToShard(ctx context.Context, tenantID string) (*Shard, error) {
	// Use hash ring to find shard
	shardID, err := s.hashRing.Get(tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to route tenant %s: %w", tenantID, err)
	}

	s.mu.RLock()
	shard, exists := s.shards[shardID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("shard %s not found for tenant %s", shardID, tenantID)
	}

	// Validate shard is active
	if shard.Status != ShardStatusActive {
		// Try to find next available shard
		return s.findNextAvailableShard(ctx, tenantID)
	}

	shardCopy := *shard
	return &shardCopy, nil
}

// findNextAvailableShard finds the next available shard in the ring
func (s *HashShardingStrategy) findNextAvailableShard(ctx context.Context, tenantID string) (*Shard, error) {
	// Get multiple candidate shards
	candidates, err := s.hashRing.GetN(tenantID, 3)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find first active shard
	for _, shardID := range candidates {
		if shard, exists := s.shards[shardID]; exists {
			if shard.Status == ShardStatusActive {
				shardCopy := *shard
				return &shardCopy, nil
			}
		}
	}

	return nil, fmt.Errorf("no available shard found for tenant %s", tenantID)
}

// ValidateShardState checks if shard is in valid state for migrations
func (s *HashShardingStrategy) ValidateShardState(ctx context.Context, shard *Shard) error {
	if shard.Status == ShardStatusFailed {
		return fmt.Errorf("shard %s is in failed state", shard.ID)
	}

	if shard.Status == ShardStatusInactive {
		return fmt.Errorf("shard %s is inactive", shard.ID)
	}

	health, err := s.GetShardHealth(ctx, shard.ID)
	if err != nil {
		return fmt.Errorf("failed to check shard %s health: %w", shard.ID, err)
	}

	if !health.Healthy {
		return fmt.Errorf("shard %s is unhealthy: %s", shard.ID, health.ErrorMessage)
	}

	return nil
}

// RegisterShard adds a new shard to the strategy and hash ring
func (s *HashShardingStrategy) RegisterShard(ctx context.Context, shard *Shard) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if shard.ID == "" {
		return fmt.Errorf("shard ID cannot be empty")
	}

	if shard.DatabaseDSN == "" {
		return fmt.Errorf("shard database DSN cannot be empty")
	}

	if _, exists := s.shards[shard.ID]; exists {
		return fmt.Errorf("shard %s already registered", shard.ID)
	}

	// Set defaults
	if shard.Type == "" {
		shard.Type = ShardTypeHashBased
	}

	if shard.Status == "" {
		shard.Status = ShardStatusActive
	}

	if shard.TenantIDs == nil {
		shard.TenantIDs = make([]string, 0)
	}

	// Calculate hash range for this shard
	if shard.HashRangeStart == 0 && shard.HashRangeEnd == 0 {
		// Will be populated by hash ring
		shard.HashRangeStart = 0
		shard.HashRangeEnd = 0xFFFFFFFF
	}

	shard.CreatedAt = time.Now()
	shard.UpdatedAt = time.Now()

	// Add to shards map
	s.shards[shard.ID] = shard

	// Add to hash ring
	s.hashRing.Add(shard.ID)

	return nil
}

// UnregisterShard removes a shard from the strategy and hash ring
func (s *HashShardingStrategy) UnregisterShard(ctx context.Context, shardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return fmt.Errorf("shard %s not found", shardID)
	}

	if len(shard.TenantIDs) > 0 {
		return fmt.Errorf("cannot unregister shard %s: still has %d tenants (use rebalancer)", shardID, len(shard.TenantIDs))
	}

	// Remove from hash ring
	s.hashRing.Remove(shardID)

	// Remove from shards map
	delete(s.shards, shardID)

	// Clean up caches
	s.healthCacheMu.Lock()
	delete(s.healthCache, shardID)
	s.healthCacheMu.Unlock()

	s.metricsCacheMu.Lock()
	delete(s.metricsCache, shardID)
	s.metricsCacheMu.Unlock()

	return nil
}

// UpdateShard updates shard information
func (s *HashShardingStrategy) UpdateShard(ctx context.Context, shard *Shard) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.shards[shard.ID]
	if !exists {
		return fmt.Errorf("shard %s not found", shard.ID)
	}

	shard.CreatedAt = existing.CreatedAt
	shard.UpdatedAt = time.Now()

	s.shards[shard.ID] = shard

	return nil
}

// GetShardHealth checks health of a specific shard
func (s *HashShardingStrategy) GetShardHealth(ctx context.Context, shardID string) (*ShardHealth, error) {
	// Check cache
	s.healthCacheMu.RLock()
	if cached, ok := s.healthCache[shardID]; ok {
		if time.Since(cached.CheckedAt) < s.config.Discovery.HealthCheckInterval {
			s.healthCacheMu.RUnlock()
			return cached, nil
		}
	}
	s.healthCacheMu.RUnlock()

	shard, err := s.GetShard(ctx, shardID)
	if err != nil {
		return nil, err
	}

	health := &ShardHealth{
		ShardID:   shardID,
		CheckedAt: time.Now(),
		Metrics:   make(map[string]float64),
	}

	startTime := time.Now()

	db, err := sql.Open(shard.DatabaseType, shard.DatabaseDSN)
	if err != nil {
		health.Healthy = false
		health.ErrorMessage = fmt.Sprintf("failed to open connection: %v", err)
		s.cacheHealth(health)
		return health, nil
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, s.config.Discovery.ConnectionTimeout)
	defer cancel()

	err = db.PingContext(ctx)
	health.ResponseTime = time.Since(startTime)

	if err != nil {
		health.Healthy = false
		health.ErrorMessage = fmt.Sprintf("ping failed: %v", err)
	} else {
		health.Healthy = true
		health.Metrics["response_time_ms"] = float64(health.ResponseTime.Milliseconds())
		health.Metrics["load_factor"] = shard.LoadFactor
		health.Metrics["tenant_count"] = float64(len(shard.TenantIDs))
		health.Metrics["hash_range_size"] = float64(shard.HashRangeEnd - shard.HashRangeStart)
	}

	s.cacheHealth(health)

	return health, nil
}

// cacheHealth stores health check result
func (s *HashShardingStrategy) cacheHealth(health *ShardHealth) {
	s.healthCacheMu.Lock()
	defer s.healthCacheMu.Unlock()
	s.healthCache[health.ShardID] = health
}

// GetAllShardHealth checks health of all shards
func (s *HashShardingStrategy) GetAllShardHealth(ctx context.Context) (map[string]*ShardHealth, error) {
	shards, err := s.GetShards(ctx)
	if err != nil {
		return nil, err
	}

	results := make(map[string]*ShardHealth)
	var wg sync.WaitGroup
	var mu sync.Mutex
	errChan := make(chan error, len(shards))

	for _, shard := range shards {
		wg.Add(1)
		go func(shardID string) {
			defer wg.Done()

			health, err := s.GetShardHealth(ctx, shardID)
			if err != nil {
				errChan <- err
				return
			}

			mu.Lock()
			results[shardID] = health
			mu.Unlock()
		}(shard.ID)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// GetShardMetrics returns performance metrics for a shard
func (s *HashShardingStrategy) GetShardMetrics(ctx context.Context, shardID string) (*ShardMetrics, error) {
	s.metricsCacheMu.RLock()
	if cached, ok := s.metricsCache[shardID]; ok {
		if time.Since(cached.Timestamp) < time.Minute {
			s.metricsCacheMu.RUnlock()
			return cached, nil
		}
	}
	s.metricsCacheMu.RUnlock()

	shard, err := s.GetShard(ctx, shardID)
	if err != nil {
		return nil, err
	}

	metrics := &ShardMetrics{
		ShardID:                 shardID,
		TenantCount:             len(shard.TenantIDs),
		QPS:                     shard.QPS,
		ConnectionPoolUsage:     shard.LoadFactor,
		Timestamp:               time.Now(),
	}

	s.metricsCacheMu.Lock()
	s.metricsCache[shardID] = metrics
	s.metricsCacheMu.Unlock()

	return metrics, nil
}

// GetTenantDistribution returns tenant distribution across shards
func (s *HashShardingStrategy) GetTenantDistribution(ctx context.Context) (map[string]int, error) {
	// Get all tenants
	tenants, err := s.registry.ListTenants(ctx)
	if err != nil {
		return nil, err
	}

	tenantIDs := make([]string, len(tenants))
	for i, tenant := range tenants {
		tenantIDs[i] = tenant.ID
	}

	// Get distribution from hash ring
	distribution := s.hashRing.GetDistribution(tenantIDs)

	return distribution, nil
}

// ReassignTenant moves a tenant to a different shard (for rebalancing)
func (s *HashShardingStrategy) ReassignTenant(ctx context.Context, tenantID, newShardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	newShard, exists := s.shards[newShardID]
	if !exists {
		return fmt.Errorf("shard %s not found", newShardID)
	}

	// Get tenant's current shard
	currentShardID, err := s.hashRing.Get(tenantID)
	if err != nil {
		return err
	}

	if currentShard, exists := s.shards[currentShardID]; exists {
		// Remove from current shard
		newTenants := make([]string, 0)
		for _, tid := range currentShard.TenantIDs {
			if tid != tenantID {
				newTenants = append(newTenants, tid)
			}
		}
		currentShard.TenantIDs = newTenants
	}

	// Add to new shard
	newShard.TenantIDs = append(newShard.TenantIDs, tenantID)

	// Update tenant in registry
	tenant, err := s.registry.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}

	tenant.ShardID = newShardID
	return s.registry.UpdateTenant(ctx, tenant)
}

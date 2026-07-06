/**
 * @file tenant_per_database.go
 * @description Tenant-per-database sharding strategy implementation
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/tenant_per_database.go
 */

package sharding

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// TenantPerDatabaseStrategy implements sharding where each tenant has its own database
type TenantPerDatabaseStrategy struct {
	registry       TenantRegistry
	shards         map[string]*Shard // ShardID -> Shard
	tenantToShard  map[string]string // TenantID -> ShardID
	config         *ShardingConfig
	mu             sync.RWMutex
	healthCache    map[string]*ShardHealth
	healthCacheMu  sync.RWMutex
	metricsCache   map[string]*ShardMetrics
	metricsCacheMu sync.RWMutex
}

// NewTenantPerDatabaseStrategy creates a new tenant-per-database strategy
func NewTenantPerDatabaseStrategy(registry TenantRegistry, config *ShardingConfig) *TenantPerDatabaseStrategy {
	if config == nil {
		config = DefaultShardingConfig()
	}
	config.Strategy = ShardTypeTenantPerDatabase

	return &TenantPerDatabaseStrategy{
		registry:      registry,
		shards:        make(map[string]*Shard),
		tenantToShard: make(map[string]string),
		config:        config,
		healthCache:   make(map[string]*ShardHealth),
		metricsCache:  make(map[string]*ShardMetrics),
	}
}

// GetShards returns all available shards
func (s *TenantPerDatabaseStrategy) GetShards(ctx context.Context) ([]Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shards := make([]Shard, 0, len(s.shards))
	for _, shard := range s.shards {
		shards = append(shards, *shard)
	}

	return shards, nil
}

// GetShard returns a specific shard by ID
func (s *TenantPerDatabaseStrategy) GetShard(ctx context.Context, shardID string) (*Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return nil, fmt.Errorf("shard %s not found", shardID)
	}

	// Return a copy to prevent external modifications
	shardCopy := *shard
	return &shardCopy, nil
}

// RouteToShard determines which shard a tenant should use
func (s *TenantPerDatabaseStrategy) RouteToShard(ctx context.Context, tenantID string) (*Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if tenant already has a shard assigned
	if shardID, exists := s.tenantToShard[tenantID]; exists {
		if shard, ok := s.shards[shardID]; ok {
			shardCopy := *shard
			return &shardCopy, nil
		}
	}

	// Get tenant from registry
	tenant, err := s.registry.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant %s: %w", tenantID, err)
	}

	// If tenant has a shard assigned in registry, use that
	if tenant.ShardID != "" {
		if shard, ok := s.shards[tenant.ShardID]; ok {
			// Cache the mapping
			s.mu.RUnlock()
			s.mu.Lock()
			s.tenantToShard[tenantID] = tenant.ShardID
			s.mu.Unlock()
			s.mu.RLock()

			shardCopy := *shard
			return &shardCopy, nil
		}
	}

	// No shard assigned, find best available shard
	return s.findBestAvailableShard(ctx, tenantID)
}

// findBestAvailableShard finds the best available shard for a new tenant
func (s *TenantPerDatabaseStrategy) findBestAvailableShard(ctx context.Context, tenantID string) (*Shard, error) {
	// Must be called with read lock held
	var bestShard *Shard
	lowestLoad := 2.0 // Start above maximum load (1.0)

	for _, shard := range s.shards {
		// Skip inactive or failed shards
		if shard.Status != ShardStatusActive {
			continue
		}

		// Check if shard is at capacity
		if len(shard.TenantIDs) >= shard.Capacity {
			continue
		}

		// Find shard with lowest load
		if shard.LoadFactor < lowestLoad {
			lowestLoad = shard.LoadFactor
			bestShard = shard
		}
	}

	if bestShard == nil {
		// Try default shard if configured
		if s.config.DefaultShardID != "" {
			if shard, ok := s.shards[s.config.DefaultShardID]; ok {
				shardCopy := *shard
				return &shardCopy, nil
			}
		}
		return nil, fmt.Errorf("no available shard found for tenant %s", tenantID)
	}

	shardCopy := *bestShard
	return &shardCopy, nil
}

// ValidateShardState checks if shard is in valid state for migrations
func (s *TenantPerDatabaseStrategy) ValidateShardState(ctx context.Context, shard *Shard) error {
	// Check shard status
	if shard.Status == ShardStatusFailed {
		return fmt.Errorf("shard %s is in failed state", shard.ID)
	}

	if shard.Status == ShardStatusInactive {
		return fmt.Errorf("shard %s is inactive", shard.ID)
	}

	// Check health
	health, err := s.GetShardHealth(ctx, shard.ID)
	if err != nil {
		return fmt.Errorf("failed to check shard %s health: %w", shard.ID, err)
	}

	if !health.Healthy {
		return fmt.Errorf("shard %s is unhealthy: %s", shard.ID, health.ErrorMessage)
	}

	// Check if DSN is valid
	if shard.DatabaseDSN == "" {
		return fmt.Errorf("shard %s has no database DSN configured", shard.ID)
	}

	return nil
}

// RegisterShard adds a new shard to the strategy
func (s *TenantPerDatabaseStrategy) RegisterShard(ctx context.Context, shard *Shard) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate shard
	if shard.ID == "" {
		return fmt.Errorf("shard ID cannot be empty")
	}

	if shard.DatabaseDSN == "" {
		return fmt.Errorf("shard database DSN cannot be empty")
	}

	// Check if shard already exists
	if _, exists := s.shards[shard.ID]; exists {
		return fmt.Errorf("shard %s already registered", shard.ID)
	}

	// Set defaults
	if shard.Type == "" {
		shard.Type = ShardTypeTenantPerDatabase
	}

	if shard.Status == "" {
		shard.Status = ShardStatusActive
	}

	if shard.Capacity == 0 {
		shard.Capacity = s.config.MaxTenantsPerShard
	}

	if shard.TenantIDs == nil {
		shard.TenantIDs = make([]string, 0)
	}

	shard.CreatedAt = time.Now()
	shard.UpdatedAt = time.Now()

	// Store shard
	s.shards[shard.ID] = shard

	return nil
}

// UnregisterShard removes a shard from the strategy
func (s *TenantPerDatabaseStrategy) UnregisterShard(ctx context.Context, shardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return fmt.Errorf("shard %s not found", shardID)
	}

	// Check if shard has tenants
	if len(shard.TenantIDs) > 0 {
		return fmt.Errorf("cannot unregister shard %s: still has %d tenants", shardID, len(shard.TenantIDs))
	}

	// Remove shard
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
func (s *TenantPerDatabaseStrategy) UpdateShard(ctx context.Context, shard *Shard) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.shards[shard.ID]
	if !exists {
		return fmt.Errorf("shard %s not found", shard.ID)
	}

	// Preserve creation time
	shard.CreatedAt = existing.CreatedAt
	shard.UpdatedAt = time.Now()

	// Update shard
	s.shards[shard.ID] = shard

	return nil
}

// GetShardHealth checks health of a specific shard
func (s *TenantPerDatabaseStrategy) GetShardHealth(ctx context.Context, shardID string) (*ShardHealth, error) {
	// Check cache first
	s.healthCacheMu.RLock()
	if cached, ok := s.healthCache[shardID]; ok {
		// Cache valid for health check interval
		if time.Since(cached.CheckedAt) < s.config.Discovery.HealthCheckInterval {
			s.healthCacheMu.RUnlock()
			return cached, nil
		}
	}
	s.healthCacheMu.RUnlock()

	// Get shard
	shard, err := s.GetShard(ctx, shardID)
	if err != nil {
		return nil, err
	}

	// Perform health check
	health := &ShardHealth{
		ShardID:   shardID,
		CheckedAt: time.Now(),
		Metrics:   make(map[string]float64),
	}

	startTime := time.Now()

	// Try to connect to database
	db, err := sql.Open(shard.DatabaseType, shard.DatabaseDSN)
	if err != nil {
		health.Healthy = false
		health.ErrorMessage = fmt.Sprintf("failed to open connection: %v", err)
		s.cacheHealth(health)
		return health, nil
	}
	defer db.Close()

	// Set connection timeout
	ctx, cancel := context.WithTimeout(ctx, s.config.Discovery.ConnectionTimeout)
	defer cancel()

	// Execute ping
	err = db.PingContext(ctx)
	health.ResponseTime = time.Since(startTime)

	if err != nil {
		health.Healthy = false
		health.ErrorMessage = fmt.Sprintf("ping failed: %v", err)
	} else {
		health.Healthy = true

		// Get additional metrics
		health.Metrics["response_time_ms"] = float64(health.ResponseTime.Milliseconds())
		health.Metrics["load_factor"] = shard.LoadFactor
		health.Metrics["tenant_count"] = float64(len(shard.TenantIDs))
		health.Metrics["capacity"] = float64(shard.Capacity)
	}

	// Cache health result
	s.cacheHealth(health)

	return health, nil
}

// cacheHealth stores health check result in cache
func (s *TenantPerDatabaseStrategy) cacheHealth(health *ShardHealth) {
	s.healthCacheMu.Lock()
	defer s.healthCacheMu.Unlock()
	s.healthCache[health.ShardID] = health
}

// GetAllShardHealth checks health of all shards
func (s *TenantPerDatabaseStrategy) GetAllShardHealth(ctx context.Context) (map[string]*ShardHealth, error) {
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

	// Check if any errors occurred
	for err := range errChan {
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// GetShardMetrics returns performance metrics for a shard
func (s *TenantPerDatabaseStrategy) GetShardMetrics(ctx context.Context, shardID string) (*ShardMetrics, error) {
	// Check cache first
	s.metricsCacheMu.RLock()
	if cached, ok := s.metricsCache[shardID]; ok {
		// Cache valid for 1 minute
		if time.Since(cached.Timestamp) < time.Minute {
			s.metricsCacheMu.RUnlock()
			return cached, nil
		}
	}
	s.metricsCacheMu.RUnlock()

	// Get shard
	shard, err := s.GetShard(ctx, shardID)
	if err != nil {
		return nil, err
	}

	// Build metrics
	metrics := &ShardMetrics{
		ShardID:                 shardID,
		TenantCount:             len(shard.TenantIDs),
		AverageMigrationTimeMs:  0, // Would be populated from tracking data
		QPS:                     shard.QPS,
		ErrorRate:               0,
		CPUUsage:                0,
		MemoryUsage:             0,
		DiskUsage:               0,
		ConnectionPoolUsage:     shard.LoadFactor,
		Timestamp:               time.Now(),
	}

	// Cache metrics
	s.metricsCacheMu.Lock()
	s.metricsCache[shardID] = metrics
	s.metricsCacheMu.Unlock()

	return metrics, nil
}

// AssignTenantToShard assigns a tenant to a specific shard
func (s *TenantPerDatabaseStrategy) AssignTenantToShard(ctx context.Context, tenantID, shardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return fmt.Errorf("shard %s not found", shardID)
	}

	// Check capacity
	if len(shard.TenantIDs) >= shard.Capacity {
		return fmt.Errorf("shard %s is at capacity", shardID)
	}

	// Check if tenant already assigned
	for _, tid := range shard.TenantIDs {
		if tid == tenantID {
			return nil // Already assigned
		}
	}

	// Add tenant to shard
	shard.TenantIDs = append(shard.TenantIDs, tenantID)
	s.tenantToShard[tenantID] = shardID

	// Update tenant in registry
	tenant, err := s.registry.GetTenant(ctx, tenantID)
	if err != nil {
		return err
	}

	tenant.ShardID = shardID
	return s.registry.UpdateTenant(ctx, tenant)
}

// RemoveTenantFromShard removes a tenant from a shard
func (s *TenantPerDatabaseStrategy) RemoveTenantFromShard(ctx context.Context, tenantID, shardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return fmt.Errorf("shard %s not found", shardID)
	}

	// Find and remove tenant
	found := false
	newTenants := make([]string, 0, len(shard.TenantIDs))
	for _, tid := range shard.TenantIDs {
		if tid != tenantID {
			newTenants = append(newTenants, tid)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("tenant %s not found on shard %s", tenantID, shardID)
	}

	shard.TenantIDs = newTenants
	delete(s.tenantToShard, tenantID)

	return nil
}

/**
 * @file tenant_per_schema.go
 * @description Tenant-per-schema sharding strategy implementation
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/tenant_per_schema.go
 */

package sharding

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TenantPerSchemaStrategy implements sharding where tenants share databases but have separate schemas
type TenantPerSchemaStrategy struct {
	registry       TenantRegistry
	shards         map[string]*Shard // ShardID -> Shard (databases)
	tenantToShard  map[string]string // TenantID -> ShardID
	tenantSchemas  map[string]string // TenantID -> SchemaName
	config         *ShardingConfig
	mu             sync.RWMutex
	healthCache    map[string]*ShardHealth
	healthCacheMu  sync.RWMutex
}

// NewTenantPerSchemaStrategy creates a new tenant-per-schema strategy
func NewTenantPerSchemaStrategy(registry TenantRegistry, config *ShardingConfig) *TenantPerSchemaStrategy {
	if config == nil {
		config = DefaultShardingConfig()
	}
	config.Strategy = ShardTypeTenantPerSchema

	return &TenantPerSchemaStrategy{
		registry:      registry,
		shards:        make(map[string]*Shard),
		tenantToShard: make(map[string]string),
		tenantSchemas: make(map[string]string),
		config:        config,
		healthCache:   make(map[string]*ShardHealth),
	}
}

// GetShards returns all available shards (databases)
func (s *TenantPerSchemaStrategy) GetShards(ctx context.Context) ([]Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shards := make([]Shard, 0, len(s.shards))
	for _, shard := range s.shards {
		shards = append(shards, *shard)
	}

	return shards, nil
}

// GetShard returns a specific shard by ID
func (s *TenantPerSchemaStrategy) GetShard(ctx context.Context, shardID string) (*Shard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return nil, fmt.Errorf("shard %s not found", shardID)
	}

	shardCopy := *shard
	return &shardCopy, nil
}

// RouteToShard determines which shard (database) a tenant should use
func (s *TenantPerSchemaStrategy) RouteToShard(ctx context.Context, tenantID string) (*Shard, error) {
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

	// If tenant has a shard assigned, use that
	if tenant.ShardID != "" {
		if shard, ok := s.shards[tenant.ShardID]; ok {
			s.mu.RUnlock()
			s.mu.Lock()
			s.tenantToShard[tenantID] = tenant.ShardID
			if tenant.SchemaName != "" {
				s.tenantSchemas[tenantID] = tenant.SchemaName
			}
			s.mu.Unlock()
			s.mu.RLock()

			shardCopy := *shard
			return &shardCopy, nil
		}
	}

	// No shard assigned, find best available
	return s.findBestAvailableShard(ctx, tenantID)
}

// findBestAvailableShard finds the best available shard for a new tenant
func (s *TenantPerSchemaStrategy) findBestAvailableShard(ctx context.Context, tenantID string) (*Shard, error) {
	var bestShard *Shard
	lowestLoad := 2.0

	for _, shard := range s.shards {
		if shard.Status != ShardStatusActive {
			continue
		}

		// Check capacity (number of schemas/tenants on this database)
		if len(shard.TenantIDs) >= shard.Capacity {
			continue
		}

		if shard.LoadFactor < lowestLoad {
			lowestLoad = shard.LoadFactor
			bestShard = shard
		}
	}

	if bestShard == nil {
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
func (s *TenantPerSchemaStrategy) ValidateShardState(ctx context.Context, shard *Shard) error {
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

	if shard.DatabaseDSN == "" {
		return fmt.Errorf("shard %s has no database DSN configured", shard.ID)
	}

	return nil
}

// RegisterShard adds a new shard (database) to the strategy
func (s *TenantPerSchemaStrategy) RegisterShard(ctx context.Context, shard *Shard) error {
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

	if shard.Type == "" {
		shard.Type = ShardTypeTenantPerSchema
	}

	if shard.Status == "" {
		shard.Status = ShardStatusActive
	}

	if shard.Capacity == 0 {
		// For schema-based, capacity can be higher
		shard.Capacity = s.config.MaxTenantsPerShard * 10
	}

	if shard.TenantIDs == nil {
		shard.TenantIDs = make([]string, 0)
	}

	shard.CreatedAt = time.Now()
	shard.UpdatedAt = time.Now()

	s.shards[shard.ID] = shard

	return nil
}

// UnregisterShard removes a shard from the strategy
func (s *TenantPerSchemaStrategy) UnregisterShard(ctx context.Context, shardID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	shard, exists := s.shards[shardID]
	if !exists {
		return fmt.Errorf("shard %s not found", shardID)
	}

	if len(shard.TenantIDs) > 0 {
		return fmt.Errorf("cannot unregister shard %s: still has %d tenants", shardID, len(shard.TenantIDs))
	}

	delete(s.shards, shardID)

	s.healthCacheMu.Lock()
	delete(s.healthCache, shardID)
	s.healthCacheMu.Unlock()

	return nil
}

// UpdateShard updates shard information
func (s *TenantPerSchemaStrategy) UpdateShard(ctx context.Context, shard *Shard) error {
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
func (s *TenantPerSchemaStrategy) GetShardHealth(ctx context.Context, shardID string) (*ShardHealth, error) {
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
		health.Metrics["schema_count"] = float64(len(shard.TenantIDs))
		health.Metrics["capacity"] = float64(shard.Capacity)
	}

	s.cacheHealth(health)

	return health, nil
}

// cacheHealth stores health check result in cache
func (s *TenantPerSchemaStrategy) cacheHealth(health *ShardHealth) {
	s.healthCacheMu.Lock()
	defer s.healthCacheMu.Unlock()
	s.healthCache[health.ShardID] = health
}

// GetAllShardHealth checks health of all shards
func (s *TenantPerSchemaStrategy) GetAllShardHealth(ctx context.Context) (map[string]*ShardHealth, error) {
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
func (s *TenantPerSchemaStrategy) GetShardMetrics(ctx context.Context, shardID string) (*ShardMetrics, error) {
	shard, err := s.GetShard(ctx, shardID)
	if err != nil {
		return nil, err
	}

	metrics := &ShardMetrics{
		ShardID:                 shardID,
		TenantCount:             len(shard.TenantIDs),
		AverageMigrationTimeMs:  0,
		QPS:                     shard.QPS,
		ErrorRate:               0,
		ConnectionPoolUsage:     shard.LoadFactor,
		Timestamp:               time.Now(),
	}

	return metrics, nil
}

// GetTenantSchema returns the schema name for a tenant
func (s *TenantPerSchemaStrategy) GetTenantSchema(ctx context.Context, tenantID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check cache
	if schema, ok := s.tenantSchemas[tenantID]; ok {
		return schema, nil
	}

	// Get from registry
	tenant, err := s.registry.GetTenant(ctx, tenantID)
	if err != nil {
		return "", err
	}

	if tenant.SchemaName == "" {
		// Generate schema name from tenant ID
		schemaName := s.generateSchemaName(tenantID)
		tenant.SchemaName = schemaName

		// Update tenant in registry
		if err := s.registry.UpdateTenant(ctx, tenant); err != nil {
			return "", err
		}
	}

	// Cache schema name
	s.mu.RUnlock()
	s.mu.Lock()
	s.tenantSchemas[tenantID] = tenant.SchemaName
	s.mu.Unlock()
	s.mu.RLock()

	return tenant.SchemaName, nil
}

// generateSchemaName generates a schema name from tenant ID
func (s *TenantPerSchemaStrategy) generateSchemaName(tenantID string) string {
	// Sanitize tenant ID for use as schema name
	schemaName := strings.ToLower(tenantID)
	schemaName = strings.ReplaceAll(schemaName, "-", "_")
	schemaName = strings.ReplaceAll(schemaName, ".", "_")

	// Add prefix
	return fmt.Sprintf("tenant_%s", schemaName)
}

// CreateTenantSchema creates a schema for a tenant
func (s *TenantPerSchemaStrategy) CreateTenantSchema(ctx context.Context, db *sql.DB, tenantID string, dbType string) error {
	schemaName, err := s.GetTenantSchema(ctx, tenantID)
	if err != nil {
		return err
	}

	// Database-specific schema creation
	var createSQL string
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		createSQL = fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION CURRENT_USER`, schemaName)
	case "mysql", "mariadb":
		createSQL = fmt.Sprintf(`CREATE DATABASE IF NOT EXISTS %s`, schemaName)
	case "sqlserver", "mssql":
		createSQL = fmt.Sprintf(`IF NOT EXISTS (SELECT * FROM sys.schemas WHERE name = '%s') EXEC('CREATE SCHEMA %s')`, schemaName, schemaName)
	case "oracle":
		// Oracle uses users as schemas
		createSQL = fmt.Sprintf(`CREATE USER %s IDENTIFIED BY generated_password`, schemaName)
	default:
		return fmt.Errorf("unsupported database type for schema creation: %s", dbType)
	}

	_, err = db.ExecContext(ctx, createSQL)
	return err
}

// SetSearchPath sets the database search path to the tenant's schema
func (s *TenantPerSchemaStrategy) SetSearchPath(ctx context.Context, db *sql.DB, tenantID string, dbType string) error {
	schemaName, err := s.GetTenantSchema(ctx, tenantID)
	if err != nil {
		return err
	}

	var setPathSQL string
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		setPathSQL = fmt.Sprintf(`SET search_path TO %s`, schemaName)
	case "mysql", "mariadb":
		setPathSQL = fmt.Sprintf(`USE %s`, schemaName)
	case "sqlserver", "mssql":
		// SQL Server uses schema prefix in queries
		return nil // No global setting needed
	case "oracle":
		setPathSQL = fmt.Sprintf(`ALTER SESSION SET CURRENT_SCHEMA = %s`, schemaName)
	default:
		return fmt.Errorf("unsupported database type for search path: %s", dbType)
	}

	if setPathSQL != "" {
		_, err = db.ExecContext(ctx, setPathSQL)
		return err
	}

	return nil
}

// AssignTenantToShard assigns a tenant to a specific shard with schema creation
func (s *TenantPerSchemaStrategy) AssignTenantToShard(ctx context.Context, tenantID, shardID string) error {
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
			return nil
		}
	}

	// Open database connection
	db, err := sql.Open(shard.DatabaseType, shard.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to shard: %w", err)
	}
	defer db.Close()

	// Create schema for tenant
	if err := s.CreateTenantSchema(ctx, db, tenantID, shard.DatabaseType); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
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
	schemaName, _ := s.GetTenantSchema(ctx, tenantID)
	tenant.SchemaName = schemaName

	return s.registry.UpdateTenant(ctx, tenant)
}

/**
 * @file types.go
 * @description Core types for multi-tenant sharding support
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/types.go
 */

package sharding

import (
	"context"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ShardStatus represents the operational status of a shard
type ShardStatus string

const (
	ShardStatusActive      ShardStatus = "active"       // Shard is active and accepting migrations
	ShardStatusInactive    ShardStatus = "inactive"     // Shard is inactive
	ShardStatusMaintenance ShardStatus = "maintenance"  // Shard is under maintenance
	ShardStatusRebalancing ShardStatus = "rebalancing"  // Shard is being rebalanced
	ShardStatusDegraded    ShardStatus = "degraded"     // Shard is degraded but operational
	ShardStatusFailed      ShardStatus = "failed"       // Shard has failed
)

// ShardType represents the type of sharding strategy
type ShardType string

const (
	ShardTypeTenantPerDatabase ShardType = "tenant_per_database" // Each tenant gets own database
	ShardTypeTenantPerSchema   ShardType = "tenant_per_schema"   // Tenants share database, separate schemas
	ShardTypeHashBased         ShardType = "hash_based"          // Hash-based horizontal sharding
	ShardTypeRangeBased        ShardType = "range_based"         // Range-based horizontal sharding
	ShardTypeGeographical      ShardType = "geographical"        // Geography-based sharding
)

// Shard represents a single database shard
type Shard struct {
	// Identity
	ID   string `json:"id"`
	Name string `json:"name"`
	Type ShardType `json:"type"`

	// Connection
	DatabaseDSN    string            `json:"database_dsn"`
	DatabaseType   string            `json:"database_type"` // postgres, mysql, etc.
	ConnectionPool int               `json:"connection_pool"`
	Metadata       map[string]string `json:"metadata"`

	// Tenant Assignment
	TenantIDs []string `json:"tenant_ids"` // Tenants assigned to this shard
	Capacity  int      `json:"capacity"`   // Max tenants for this shard

	// Geographical
	Region         string   `json:"region"`           // e.g., "us-east-1", "eu-west-1"
	DataCenter     string   `json:"data_center"`      // Physical data center
	AvailableZones []string `json:"available_zones"`

	// Hash Range (for hash-based sharding)
	HashRangeStart uint32 `json:"hash_range_start"`
	HashRangeEnd   uint32 `json:"hash_range_end"`

	// Status
	Status       ShardStatus `json:"status"`
	Health       float64     `json:"health"`        // 0.0-1.0 health score
	LastHealthCheck time.Time `json:"last_health_check"`

	// Performance Metrics
	LoadFactor     float64 `json:"load_factor"`      // 0.0-1.0 current load
	QPS            float64 `json:"qps"`              // Queries per second
	ResponseTimeMs float64 `json:"response_time_ms"` // Average response time

	// Migration State
	LastMigration     string    `json:"last_migration"`      // Last applied migration ID
	MigrationsPending int       `json:"migrations_pending"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Tenant represents a tenant in a multi-tenant system
type Tenant struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	ShardID        string            `json:"shard_id"`        // Which shard this tenant is on
	SchemaName     string            `json:"schema_name"`     // Schema name (for tenant-per-schema)
	DatabaseName   string            `json:"database_name"`   // Database name (for tenant-per-database)
	Status         string            `json:"status"`          // active, suspended, etc.
	Metadata       map[string]string `json:"metadata"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	MigrationState string            `json:"migration_state"` // Current migration state
}

// ShardMigrationRecord tracks migration execution per shard
type ShardMigrationRecord struct {
	ID          string    `json:"id"`
	MigrationID string    `json:"migration_id"`
	ShardID     string    `json:"shard_id"`
	TenantID    string    `json:"tenant_id,omitempty"` // Optional, for tenant-specific tracking
	Status      string    `json:"status"`              // pending, running, success, failed
	StartedAt   time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMs  int64     `json:"duration_ms"`
	Error       string    `json:"error,omitempty"`
	Checksum    string    `json:"checksum"`
	AppliedBy   string    `json:"applied_by"`
}

// ShardMigrationStatus represents aggregated migration status across shards
type ShardMigrationStatus struct {
	MigrationID     string                      `json:"migration_id"`
	TotalShards     int                         `json:"total_shards"`
	SuccessCount    int                         `json:"success_count"`
	FailedCount     int                         `json:"failed_count"`
	PendingCount    int                         `json:"pending_count"`
	InProgressCount int                         `json:"in_progress_count"`
	ShardStatuses   map[string]*ShardMigrationRecord `json:"shard_statuses"` // ShardID -> Record
	StartedAt       time.Time                   `json:"started_at"`
	CompletedAt     *time.Time                  `json:"completed_at,omitempty"`
	OverallStatus   string                      `json:"overall_status"` // success, partial, failed, in_progress
}

// RebalanceOperation represents a shard rebalancing operation
type RebalanceOperation struct {
	ID               string    `json:"id"`
	Type             string    `json:"type"` // add_shard, remove_shard, redistribute
	SourceShardID    string    `json:"source_shard_id"`
	TargetShardID    string    `json:"target_shard_id"`
	TenantsToMove    []string  `json:"tenants_to_move"`
	Status           string    `json:"status"` // pending, in_progress, completed, failed
	Progress         float64   `json:"progress"` // 0.0-1.0
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Error            string    `json:"error,omitempty"`
}

// ShardDiscoveryConfig configures how shards are discovered
type ShardDiscoveryConfig struct {
	Type             string            `json:"type"` // static, dynamic, consul, etcd
	RefreshInterval  time.Duration     `json:"refresh_interval"`
	HealthCheckInterval time.Duration  `json:"health_check_interval"`
	ConnectionTimeout time.Duration    `json:"connection_timeout"`
	Endpoints        []string          `json:"endpoints"` // For dynamic discovery
	Metadata         map[string]string `json:"metadata"`
}

// ShardingConfig holds configuration for sharding behavior
type ShardingConfig struct {
	// Strategy Configuration
	Strategy              ShardType `json:"strategy"`
	DefaultShardID        string    `json:"default_shard_id"`        // Fallback shard
	TenantIDHashAlgorithm string    `json:"tenant_id_hash_algorithm"` // "xxhash", "fnv", "murmur3"

	// Parallelism
	MaxParallelShards     int `json:"max_parallel_shards"`      // Max concurrent shard migrations
	MaxParallelPerShard   int `json:"max_parallel_per_shard"`   // Max concurrent migrations per shard
	BatchSize             int `json:"batch_size"`               // Batch size for tenant migrations

	// Failure Handling
	ContinueOnShardFailure bool          `json:"continue_on_shard_failure"` // Continue if some shards fail
	MaxRetries            int           `json:"max_retries"`
	RetryDelay            time.Duration `json:"retry_delay"`
	FailureThreshold      float64       `json:"failure_threshold"` // Max % of shards that can fail (0.0-1.0)

	// Rebalancing
	AutoRebalance         bool    `json:"auto_rebalance"`          // Enable automatic rebalancing
	RebalanceThreshold    float64 `json:"rebalance_threshold"`     // Load difference triggering rebalance
	MinTenantsPerShard    int     `json:"min_tenants_per_shard"`
	MaxTenantsPerShard    int     `json:"max_tenants_per_shard"`

	// Discovery
	Discovery ShardDiscoveryConfig `json:"discovery"`

	// Monitoring
	EnableMetrics bool `json:"enable_metrics"`
	MetricsPrefix string `json:"metrics_prefix"`

	// Migration Configuration
	MigrationConfig *types.MigrationConfig `json:"migration_config"`
}

// DefaultShardingConfig returns default sharding configuration
func DefaultShardingConfig() *ShardingConfig {
	return &ShardingConfig{
		Strategy:               ShardTypeTenantPerDatabase,
		TenantIDHashAlgorithm:  "xxhash",
		MaxParallelShards:      10,
		MaxParallelPerShard:    1,
		BatchSize:              100,
		ContinueOnShardFailure: false,
		MaxRetries:             3,
		RetryDelay:             5 * time.Second,
		FailureThreshold:       0.1, // Allow 10% shard failures
		AutoRebalance:          false,
		RebalanceThreshold:     0.3, // 30% load difference
		MinTenantsPerShard:     1,
		MaxTenantsPerShard:     1000,
		EnableMetrics:          true,
		MetricsPrefix:          "migration.sharding",
		Discovery: ShardDiscoveryConfig{
			Type:                "static",
			RefreshInterval:     5 * time.Minute,
			HealthCheckInterval: 30 * time.Second,
			ConnectionTimeout:   10 * time.Second,
		},
	}
}

// ShardHealth represents health check result for a shard
type ShardHealth struct {
	ShardID       string    `json:"shard_id"`
	Healthy       bool      `json:"healthy"`
	ResponseTime  time.Duration `json:"response_time"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
	Metrics       map[string]float64 `json:"metrics"`
}

// ShardMetrics holds performance metrics for a shard
type ShardMetrics struct {
	ShardID            string    `json:"shard_id"`
	TenantCount        int       `json:"tenant_count"`
	MigrationsApplied  int       `json:"migrations_applied"`
	MigrationsFailed   int       `json:"migrations_failed"`
	AverageMigrationTimeMs float64 `json:"average_migration_time_ms"`
	QPS                float64   `json:"qps"`
	ErrorRate          float64   `json:"error_rate"`
	CPUUsage           float64   `json:"cpu_usage"`
	MemoryUsage        float64   `json:"memory_usage"`
	DiskUsage          float64   `json:"disk_usage"`
	ConnectionPoolUsage float64  `json:"connection_pool_usage"`
	Timestamp          time.Time `json:"timestamp"`
}

// ShardingStrategy defines the interface for different sharding strategies
type ShardingStrategy interface {
	// GetShards returns all available shards
	GetShards(ctx context.Context) ([]Shard, error)

	// GetShard returns a specific shard by ID
	GetShard(ctx context.Context, shardID string) (*Shard, error)

	// RouteToShard determines which shard a tenant should use
	RouteToShard(ctx context.Context, tenantID string) (*Shard, error)

	// ValidateShardState checks if shard is in valid state for migrations
	ValidateShardState(ctx context.Context, shard *Shard) error

	// RegisterShard adds a new shard to the strategy
	RegisterShard(ctx context.Context, shard *Shard) error

	// UnregisterShard removes a shard from the strategy
	UnregisterShard(ctx context.Context, shardID string) error

	// UpdateShard updates shard information
	UpdateShard(ctx context.Context, shard *Shard) error

	// GetShardHealth checks health of a specific shard
	GetShardHealth(ctx context.Context, shardID string) (*ShardHealth, error)

	// GetAllShardHealth checks health of all shards
	GetAllShardHealth(ctx context.Context) (map[string]*ShardHealth, error)

	// GetShardMetrics returns performance metrics for a shard
	GetShardMetrics(ctx context.Context, shardID string) (*ShardMetrics, error)
}

// TenantRegistry manages tenant-to-shard mappings
type TenantRegistry interface {
	// GetTenant retrieves tenant information
	GetTenant(ctx context.Context, tenantID string) (*Tenant, error)

	// ListTenants returns all tenants
	ListTenants(ctx context.Context) ([]*Tenant, error)

	// ListTenantsByShard returns tenants on a specific shard
	ListTenantsByShard(ctx context.Context, shardID string) ([]*Tenant, error)

	// RegisterTenant registers a new tenant
	RegisterTenant(ctx context.Context, tenant *Tenant) error

	// UpdateTenant updates tenant information
	UpdateTenant(ctx context.Context, tenant *Tenant) error

	// MoveTenant moves a tenant to a different shard
	MoveTenant(ctx context.Context, tenantID string, newShardID string) error

	// DeleteTenant removes a tenant
	DeleteTenant(ctx context.Context, tenantID string) error
}

// ShardedMigrationTracker tracks migrations across shards
type ShardedMigrationTracker interface {
	// RecordShardMigration records a migration execution for a shard
	RecordShardMigration(ctx context.Context, record *ShardMigrationRecord) error

	// GetShardMigrationStatus gets migration status for a specific shard
	GetShardMigrationStatus(ctx context.Context, shardID string, migrationID string) (*ShardMigrationRecord, error)

	// GetOverallMigrationStatus gets aggregated status across all shards
	GetOverallMigrationStatus(ctx context.Context, migrationID string) (*ShardMigrationStatus, error)

	// GetShardMigrationHistory gets migration history for a shard
	GetShardMigrationHistory(ctx context.Context, shardID string) ([]*ShardMigrationRecord, error)

	// GetAppliedMigrations returns list of applied migrations for a shard
	GetAppliedMigrations(ctx context.Context, shardID string) ([]string, error)

	// IsMigrationApplied checks if migration is applied to a shard
	IsMigrationApplied(ctx context.Context, shardID string, migrationID string) (bool, error)
}

// ShardCoordinator coordinates migrations across multiple shards
type ShardCoordinator interface {
	// MigrateAllShards applies a migration to all shards
	MigrateAllShards(ctx context.Context, migration *types.Migration) (*ShardMigrationStatus, error)

	// MigrateShards applies a migration to specific shards
	MigrateShards(ctx context.Context, migration *types.Migration, shardIDs []string) (*ShardMigrationStatus, error)

	// RollbackAllShards rolls back a migration from all shards
	RollbackAllShards(ctx context.Context, migrationID string) (*ShardMigrationStatus, error)

	// RollbackShards rolls back a migration from specific shards
	RollbackShards(ctx context.Context, migrationID string, shardIDs []string) (*ShardMigrationStatus, error)

	// ValidateAllShards validates migrations on all shards
	ValidateAllShards(ctx context.Context, migrations []*types.Migration) error

	// GetOverallStatus gets overall migration status across shards
	GetOverallStatus(ctx context.Context) (map[string]*ShardMigrationStatus, error)
}

// ShardRebalancer handles shard rebalancing operations
type ShardRebalancer interface {
	// AddShard adds a new shard and migrates existing data if needed
	AddShard(ctx context.Context, shard *Shard) (*RebalanceOperation, error)

	// RemoveShard removes a shard and migrates data to other shards
	RemoveShard(ctx context.Context, shardID string) (*RebalanceOperation, error)

	// Rebalance redistributes tenants across shards for optimal load
	Rebalance(ctx context.Context) ([]*RebalanceOperation, error)

	// GetRebalanceStatus returns status of a rebalance operation
	GetRebalanceStatus(ctx context.Context, operationID string) (*RebalanceOperation, error)

	// CancelRebalance cancels an ongoing rebalance operation
	CancelRebalance(ctx context.Context, operationID string) error

	// CalculateOptimalDistribution calculates optimal tenant distribution
	CalculateOptimalDistribution(ctx context.Context) (map[string][]string, error) // ShardID -> TenantIDs
}

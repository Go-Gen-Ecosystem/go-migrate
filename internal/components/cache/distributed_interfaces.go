/**
 * @file distributed_interfaces.go
 * @description Interfaces para cache distribuído multi-nó com coordenação Redis/gRPC
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/distributed_interfaces.go
 */

package cache

import (
	"context"
	"sync"
	"time"
)

// NodeInfo representa informações de um nó na rede distribuída
type NodeInfo struct {
	ID          string            `json:"id"`
	Address     string            `json:"address"`
	Port        int               `json:"port"`
	Status      NodeStatus        `json:"status"`
	LastSeen    time.Time         `json:"last_seen"`
	Metadata    map[string]string `json:"metadata"`
	CacheStats  *CacheStats       `json:"cache_stats,omitempty"`
	Version     string            `json:"version"`
	Region      string            `json:"region,omitempty"`
	Zone        string            `json:"zone,omitempty"`
}

// NodeStatus define os status possíveis de um nó
type NodeStatus string

const (
	NodeStatusActive      NodeStatus = "active"
	NodeStatusInactive    NodeStatus = "inactive"
	NodeStatusSynchronizing NodeStatus = "synchronizing"
	NodeStatusFailed      NodeStatus = "failed"
	NodeStatusMaintenance NodeStatus = "maintenance"
)

// SyncOperation representa uma operação de sincronização
type SyncOperation struct {
	ID        string                 `json:"id"`
	Type      SyncOperationType      `json:"type"`
	Key       string                 `json:"key"`
	Value     interface{}            `json:"value,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	NodeID    string                 `json:"node_id"`
	Version   int64                  `json:"version"`
	TTL       time.Duration          `json:"ttl,omitempty"`
}

// SyncOperationType define os tipos de operações de sincronização
type SyncOperationType string

const (
	SyncOpSet    SyncOperationType = "set"
	SyncOpDelete SyncOperationType = "delete"
	SyncOpClear  SyncOperationType = "clear"
	SyncOpEvict  SyncOperationType = "evict"
	SyncOpExpire SyncOperationType = "expire"
	SyncOpBatch  SyncOperationType = "batch"
)

// ConsistencyLevel define os níveis de consistência
type ConsistencyLevel string

const (
	ConsistencyEventual ConsistencyLevel = "eventual"
	ConsistencyStrong   ConsistencyLevel = "strong"
	ConsistencySession  ConsistencyLevel = "session"
	ConsistencyQuorum   ConsistencyLevel = "quorum"
)

// DistributedCacheConfig configuração para cache distribuído
type DistributedCacheConfig struct {
	// Configuração básica
	NodeID           string           `json:"node_id"`
	ClusterName      string           `json:"cluster_name"`
	ConsistencyLevel ConsistencyLevel `json:"consistency_level"`
	Enabled          bool             `json:"enabled"`
	
	// Coordenação
	CoordinatorType  CoordinatorType `json:"coordinator_type"`
	RedisConfig      *RedisConfig    `json:"redis_config,omitempty"`
	GRPCConfig       *GRPCConfig     `json:"grpc_config,omitempty"`
	
	// Sincronização
	SyncInterval     time.Duration `json:"sync_interval"`
	SyncBatchSize    int           `json:"sync_batch_size"`
	SyncTimeout      time.Duration `json:"sync_timeout"`
	ConflictStrategy ConflictStrategy `json:"conflict_strategy"`
	SyncBufferSize   int           `json:"sync_buffer_size"`
	
	// Resolução de conflitos
	ConflictResolution *ConflictResolutionConfig `json:"conflict_resolution,omitempty"`
	ConflictResolutionInterval time.Duration `json:"conflict_resolution_interval"`
	
	// Descoberta de nós
	DiscoveryEnabled  bool          `json:"discovery_enabled"`
	DiscoveryInterval time.Duration `json:"discovery_interval"`
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	
	// Performance
	MaxNodes         int           `json:"max_nodes"`
	ReplicationFactor int          `json:"replication_factor"`
	PartitionCount   int           `json:"partition_count"`
	
	// Cache remoto
	CrossNodeLookup  bool          `json:"cross_node_lookup"`
	RemoteCacheTTL   time.Duration `json:"remote_cache_ttl"`
	
	// Tolerância a falhas
	FailureThreshold  int           `json:"failure_threshold"`
	RecoveryTimeout   time.Duration `json:"recovery_timeout"`
	RetryAttempts     int           `json:"retry_attempts"`
	RetryBackoff      time.Duration `json:"retry_backoff"`
}

// CoordinatorType define os tipos de coordenadores
type CoordinatorType string

const (
	CoordinatorRedis CoordinatorType = "redis"
	CoordinatorGRPC  CoordinatorType = "grpc"
	CoordinatorHybrid CoordinatorType = "hybrid"
)

// ConflictStrategy define estratégias de resolução de conflitos
type ConflictStrategy string

const (
	ConflictLastWrite    ConflictStrategy = "last_write_wins"
	ConflictFirstWrite   ConflictStrategy = "first_write_wins"
	ConflictVersionBased ConflictStrategy = "version_based"
	ConflictCustom       ConflictStrategy = "custom"
)

// ConflictResolutionType tipo de estratégia de resolução
type ConflictResolutionType int

const (
	ResolutionLastWriteWins ConflictResolutionType = iota
	ResolutionFirstWriteWins
	ResolutionTimestampBased
	ResolutionVectorClock
	ResolutionQuorum
	ResolutionManual
	ResolutionMerge
	ResolutionCustom
)

// RedisConfig configuração para coordenação via Redis
type RedisConfig struct {
	Address     string        `json:"address"`
	Password    string        `json:"password"`
	DB          int           `json:"db"`
	PoolSize    int           `json:"pool_size"`
	Timeout     time.Duration `json:"timeout"`
	KeyPrefix   string        `json:"key_prefix"`
	Channel     string        `json:"channel"`
	ClusterMode bool          `json:"cluster_mode"`
	Addresses   []string      `json:"addresses,omitempty"`
}

// GRPCConfig configuração para coordenação via gRPC
type GRPCConfig struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	MaxConnections  int           `json:"max_connections"`
	MaxMessageSize  int           `json:"max_message_size"`
	Timeout         time.Duration `json:"timeout"`
	KeepAlive       time.Duration `json:"keep_alive"`
	TLSEnabled      bool          `json:"tls_enabled"`
	TLS             *TLSConfig    `json:"tls,omitempty"`
	CertFile        string        `json:"cert_file,omitempty"`
	KeyFile         string        `json:"key_file,omitempty"`
	CAFile          string        `json:"ca_file,omitempty"`
	ServerName      string        `json:"server_name,omitempty"`
	KnownNodes      []string      `json:"known_nodes,omitempty"`
}

// TLSConfig configuração TLS para gRPC
type TLSConfig struct {
	Enabled    bool   `json:"enabled"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file,omitempty"`
	ServerName string `json:"server_name,omitempty"`
}

// DistributedCache interface principal para cache distribuído
type DistributedCache interface {
	Cache // Herda interface básica de cache
	
	// Operações distribuídas
	SetDistributed(ctx context.Context, key string, value interface{}, ttl time.Duration, consistency ConsistencyLevel) error
	GetDistributed(ctx context.Context, key string, consistency ConsistencyLevel) (interface{}, bool, error)
	DeleteDistributed(ctx context.Context, key string, consistency ConsistencyLevel) error
	
	// Sincronização
	Sync(ctx context.Context) error
	SyncKey(ctx context.Context, key string) error
	GetSyncStatus() *SyncStatus
	
	// Gerenciamento de nós
	GetNodes() []*NodeInfo
	GetNodeInfo(nodeID string) (*NodeInfo, error)
	RegisterNode(ctx context.Context, node *NodeInfo) error
	UnregisterNode(ctx context.Context, nodeID string) error
	
	// Monitoramento
	GetDistributedStats() *DistributedStats
	GetClusterHealth() *ClusterHealth
	
	// Configuração
	UpdateConfig(config *DistributedCacheConfig) error
	GetDistributedConfig() *DistributedCacheConfig
}

// NodeCoordinator interface para coordenação entre nós
type NodeCoordinator interface {
	// Inicialização
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool
	
	// Descoberta de nós
	DiscoverNodes(ctx context.Context) ([]*NodeInfo, error)
	RegisterNode(ctx context.Context, node *NodeInfo) error
	UnregisterNode(ctx context.Context, nodeID string) error
	UpdateNodeStatus(ctx context.Context, nodeID string, status NodeStatus) error
	
	// Comunicação
	BroadcastOperation(ctx context.Context, op *SyncOperation) error
	SendToNode(ctx context.Context, nodeID string, op *SyncOperation) error
	ReceiveOperations(ctx context.Context) (<-chan *SyncOperation, error)
	
	// Heartbeat
	SendHeartbeat(ctx context.Context, nodeInfo *NodeInfo) error
	GetNodeHealth(nodeID string) (*NodeHealth, error)
	
	// Configuração
	GetCoordinatorType() CoordinatorType
	UpdateConfig(config interface{}) error
}

// SyncManager interface para gerenciamento de sincronização
type SyncManager interface {
	// Sincronização
	StartSync(ctx context.Context) error
	StopSync(ctx context.Context) error
	SyncOperation(ctx context.Context, op *SyncOperation) error
	
	// Resolução de conflitos
	ResolveConflict(ctx context.Context, local, remote *CacheItem) (*CacheItem, error)
	SetConflictStrategy(strategy ConflictStrategy)
	
	// Monitoramento
	GetSyncStatus() *SyncStatus
	GetSyncMetrics() *SyncMetrics
	
	// Configuração
	SetSyncInterval(interval time.Duration)
	SetBatchSize(size int)
}

// SyncStatus representa o status de sincronização
type SyncStatus struct {
	IsActive        bool                     `json:"is_active"`
	LastSync        time.Time                `json:"last_sync"`
	NextSync        time.Time                `json:"next_sync"`
	SyncCount       int64                    `json:"sync_count"`
	ErrorCount      int64                    `json:"error_count"`
	LastError       string                   `json:"last_error,omitempty"`
	PendingOps      int                      `json:"pending_ops"`
	NodeStatuses    map[string]*NodeHealth   `json:"node_statuses"`
	ConflictCount   int64                    `json:"conflict_count"`
	ResolvedCount   int64                    `json:"resolved_count"`
}

// NodeHealth representa a saúde de um nó
type NodeHealth struct {
	NodeID          string        `json:"node_id"`
	Status          NodeStatus    `json:"status"`
	LastHeartbeat   time.Time     `json:"last_heartbeat"`
	ResponseTime    time.Duration `json:"response_time"`
	ErrorCount      int64         `json:"error_count"`
	IsReachable     bool          `json:"is_reachable"`
	Version         string        `json:"version"`
	Load            float64       `json:"load"`
	MemoryUsage     int64         `json:"memory_usage"`
	CacheSize       int64         `json:"cache_size"`
}

// DistributedStats estatísticas do cache distribuído
type DistributedStats struct {
	*CacheStats // Herda estatísticas básicas
	
	// Estatísticas distribuídas
	NodeCount       int                    `json:"node_count"`
	ActiveNodes     int                    `json:"active_nodes"`
	TotalCacheSize  int64                  `json:"total_cache_size"`
	ReplicationRate float64                `json:"replication_rate"`
	SyncOperations  int64                  `json:"sync_operations"`
	ConflictRate    float64                `json:"conflict_rate"`
	NetworkLatency  time.Duration          `json:"network_latency"`
	NodeStats       map[string]*CacheStats `json:"node_stats"`
}

// ClusterHealth representa a saúde do cluster
type ClusterHealth struct {
	Status          ClusterStatus          `json:"status"`
	NodeCount       int                    `json:"node_count"`
	HealthyNodes    int                    `json:"healthy_nodes"`
	UnhealthyNodes  int                    `json:"unhealthy_nodes"`
	PartitionCount  int                    `json:"partition_count"`
	ReplicationRate float64                `json:"replication_rate"`
	LastCheck       time.Time              `json:"last_check"`
	Issues          []string               `json:"issues,omitempty"`
	NodeHealth      map[string]*NodeHealth `json:"node_health"`
}

// ClusterStatus define os status do cluster
type ClusterStatus string

const (
	ClusterHealthy   ClusterStatus = "healthy"
	ClusterDegraded  ClusterStatus = "degraded"
	ClusterUnhealthy ClusterStatus = "unhealthy"
	ClusterSplit     ClusterStatus = "split_brain"
)

// SyncMetrics métricas de sincronização
type SyncMetrics struct {
	TotalOperations     int64         `json:"total_operations"`
	OperationsQueued    int64         `json:"operations_queued"`
	OperationsProcessed int64         `json:"operations_processed"`
	OperationsFailed    int64         `json:"operations_failed"`
	OperationsRetried   int64         `json:"operations_retried"`
	BatchesProcessed    int64         `json:"batches_processed"`
	SuccessfulSyncs     int64         `json:"successful_syncs"`
	FailedSyncs         int64         `json:"failed_syncs"`
	AverageLatency      time.Duration `json:"average_latency"`
	MaxLatency          time.Duration `json:"max_latency"`
	MinLatency          time.Duration `json:"min_latency"`
	ConflictsResolved   int64         `json:"conflicts_resolved"`
	DataTransferred     int64         `json:"data_transferred"`
	LastSyncDuration    time.Duration `json:"last_sync_duration"`
	mutex               sync.RWMutex  `json:"-"`
}

// PartitionManager interface para gerenciamento de partições
type PartitionManager interface {
	// Particionamento
	GetPartition(key string) int
	GetPartitionNodes(partition int) []*NodeInfo
	AssignPartition(partition int, nodeID string) error
	
	// Rebalanceamento
	Rebalance(ctx context.Context) error
	GetRebalanceStatus() *RebalanceStatus
	
	// Configuração
	SetPartitionCount(count int) error
	SetReplicationFactor(factor int) error
}

// RebalanceStatus status do rebalanceamento
type RebalanceStatus struct {
	IsActive      bool      `json:"is_active"`
	Progress      float64   `json:"progress"`
	StartTime     time.Time `json:"start_time"`
	EstimatedEnd  time.Time `json:"estimated_end"`
	MovedKeys     int64     `json:"moved_keys"`
	TotalKeys     int64     `json:"total_keys"`
	ErrorCount    int64     `json:"error_count"`
	LastError     string    `json:"last_error,omitempty"`
}

// CoordinatorMetrics métricas do coordenador
type CoordinatorMetrics struct {
	ConnectedNodes    int64     `json:"connected_nodes"`
	TotalOperations   int64     `json:"total_operations"`
	FailedOperations  int64     `json:"failed_operations"`
	LastUpdate        time.Time `json:"last_update"`
	ResponseTime      time.Duration `json:"response_time"`
	NetworkLatency    time.Duration `json:"network_latency"`
	CustomMetrics     map[string]interface{} `json:"custom_metrics"`
}

// RecordCustomMetric adiciona uma métrica customizada
func (c *CoordinatorMetrics) RecordCustomMetric(key string, value interface{}) {
	if c.CustomMetrics == nil {
		c.CustomMetrics = make(map[string]interface{})
	}
	c.CustomMetrics[key] = value
	c.LastUpdate = time.Now()
}

// PartitionInfo informações da partição
type PartitionInfo struct {
	ID           int       `json:"id"`
	NodeID       string    `json:"node_id"`
	KeyRange     KeyRange  `json:"key_range"`
	KeyCount     int64     `json:"key_count"`
	Size         int64     `json:"size"`
	LastAccess   time.Time `json:"last_access"`
	IsReplica    bool      `json:"is_replica"`
	ReplicaNodes []string  `json:"replica_nodes"`
}

// KeyRange representa um intervalo de chaves
type KeyRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// CompressionType tipo de compressão
type CompressionType int

const (
	CompressionNone CompressionType = iota
	CompressionGzip
	CompressionLZ4
	CompressionSnappy
	CompressionZstd
)

// EncryptionType tipo de criptografia
type EncryptionType int

const (
	EncryptionNone EncryptionType = iota
	EncryptionAES128
	EncryptionAES256
	EncryptionChaCha20
)
/**
 * @file distributed_factory.go
 * @description Factory para instanciar cache distribuído com injeção de dependências
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/distributed_factory.go
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DistributedCacheFactory factory para criar instâncias de cache distribuído
type DistributedCacheFactory struct {
	// Container de DI
	container *DIContainer

	// Configurações padrão
	defaultConfig *DistributedCacheConfig

	// Logger injetado
	logger Logger
}

// DIContainer container de injeção de dependências
type DIContainer struct {
	// Singletons
	singletons map[string]interface{}

	// Factories para criação de instâncias
	factories map[string]func() (interface{}, error)

	// Configurações
	configs map[string]interface{}

	// Mutex para thread safety
	mutex sync.RWMutex
}

// DistributedCacheBuilder builder para configurar cache distribuído
type DistributedCacheBuilder struct {
	config       *DistributedCacheConfig
	container    *DIContainer
	logger       Logger
	nodeID       string
	clusterName  string

	// Componentes opcionais
	coordinator      NodeCoordinator
	syncManager      SyncManager
	partitionMgr     PartitionManager
	conflictResolver *ConflictResolver
	discoveryService *NodeDiscoveryService
}

// CacheCluster representa um cluster de caches distribuídos
type CacheCluster struct {
	name      string
	nodes     map[string]*DistributedFlyweightCache
	factory   *DistributedCacheFactory
	config    *ClusterConfig
	isRunning bool

	// Coordenação do cluster
	coordinator NodeCoordinator

	// Métricas do cluster
	metrics *ClusterMetrics

	// Logger
	logger Logger

	mutex sync.RWMutex
}

// ClusterConfig configuração do cluster
type ClusterConfig struct {
	Name                string                             `json:"name"`
	MinNodes            int                                `json:"min_nodes"`
	MaxNodes            int                                `json:"max_nodes"`
	ReplicationFactor   int                                `json:"replication_factor"`
	ConsistencyLevel    ConsistencyLevel                   `json:"consistency_level"`
	AutoScaling         bool                               `json:"auto_scaling"`
	HealthCheckInterval time.Duration                      `json:"health_check_interval"`
	NodeConfigs         map[string]*DistributedCacheConfig `json:"node_configs"`
}

// ClusterMetrics métricas do cluster
type ClusterMetrics struct {
	ActiveNodes     int                     `json:"active_nodes"`
	TotalNodes      int                     `json:"total_nodes"`
	TotalOperations int64                   `json:"total_operations"`
	TotalHits       int64                   `json:"total_hits"`
	TotalMisses     int64                   `json:"total_misses"`
	AverageLatency  time.Duration           `json:"average_latency"`
	NodeMetrics     map[string]*NodeMetrics `json:"node_metrics"`

	mutex sync.RWMutex
}

// NodeMetrics métricas de um nó específico
type NodeMetrics struct {
	NodeID        string                 `json:"node_id"`
	Status        NodeStatus             `json:"status"`
	Operations    int64                  `json:"operations"`
	Hits          int64                  `json:"hits"`
	Misses        int64                  `json:"misses"`
	Latency       time.Duration          `json:"latency"`
	LastUpdate    time.Time              `json:"last_update"`
	CustomMetrics map[string]interface{} `json:"custom_metrics"`
}

// NewDistributedCacheFactory cria nova factory
func NewDistributedCacheFactory(logger Logger) *DistributedCacheFactory {
	if logger == nil {
		logger = &MockLogger{}
	}

	factory := &DistributedCacheFactory{
		container: NewDIContainer(),
		logger:    logger,
		defaultConfig: &DistributedCacheConfig{
			Enabled:           true,
			SyncInterval:      1 * time.Second,
			HeartbeatInterval: 30 * time.Second,
		},
	}

	// Registrar componentes padrão no container
	factory.registerDefaultComponents()

	return factory
}

// NewDIContainer cria novo container de DI
func NewDIContainer() *DIContainer {
	return &DIContainer{
		singletons: make(map[string]interface{}),
		factories:  make(map[string]func() (interface{}, error)),
		configs:    make(map[string]interface{}),
	}
}

// CreateDistributedCache cria instância de cache distribuído
func (f *DistributedCacheFactory) CreateDistributedCache(
	nodeID, clusterName string,
	config *DistributedCacheConfig,
) (*DistributedFlyweightCache, error) {

	if nodeID == "" {
		nodeID = f.generateNodeID()
	}

	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}

	// Usar configuração padrão se não fornecida
	if config == nil {
		config = f.defaultConfig
	} else {
		config = f.mergeWithDefaults(config)
	}

	// Atualizar configuração com IDs
	config.NodeID = nodeID
	config.ClusterName = clusterName

	// Resolver dependências
	coordinator, err := f.resolveCoordinator(config)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve coordinator: %w", err)
	}

	syncManager, err := f.resolveSyncManager(config, coordinator)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve sync manager: %w", err)
	}

	partitionManager, err := f.resolvePartitionManager(config, coordinator)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve partition manager: %w", err)
	}

	// Criar cache distribuído
	baseConfig := &CacheConfig{
		Name:    "distributed_cache",
		Type:    CacheTypeMemory,
		MaxSize: 1000,
		TTL:     5 * time.Minute,
	}

	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		config,
		nodeID,
		clusterName,
		coordinator,
		syncManager,
		partitionManager,
		f.logger,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create distributed cache: %w", err)
	}

	f.logger.Info("Distributed cache created", map[string]interface{}{
		"node_id":      nodeID,
		"cluster_name": clusterName,
		"coordinator":  coordinator.GetCoordinatorType(),
	})

	return cache, nil
}

// CreateBuilder cria builder para configuração avançada
func (f *DistributedCacheFactory) CreateBuilder() *DistributedCacheBuilder {
	return &DistributedCacheBuilder{
		config:      f.copyDefaultConfig(),
		container:   f.container,
		logger:      f.logger,
		nodeID:      f.copyDefaultConfig().NodeID,
		clusterName: f.copyDefaultConfig().ClusterName,
	}
}

// CreateCluster cria cluster de caches distribuídos
func (f *DistributedCacheFactory) CreateCluster(config *ClusterConfig) (*CacheCluster, error) {
	if config == nil {
		return nil, fmt.Errorf("cluster config cannot be nil")
	}

	if config.Name == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}

	if config.MinNodes <= 0 {
		config.MinNodes = 1
	}

	if config.MaxNodes <= 0 {
		config.MaxNodes = 10
	}

	if config.ReplicationFactor <= 0 {
		config.ReplicationFactor = 2
	}

	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = 30 * time.Second
	}

	// Criar coordenador do cluster
	coordinatorNodeID := f.generateNodeID()
	coordinator, err := f.resolveCoordinator(&DistributedCacheConfig{
		NodeID:      coordinatorNodeID,
		ClusterName: config.Name,
		RedisConfig: &RedisConfig{
			Address: "localhost:6379",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster coordinator: %w", err)
	}

	cluster := &CacheCluster{
		name:        config.Name,
		nodes:       make(map[string]*DistributedFlyweightCache),
		factory:     f,
		config:      config,
		coordinator: coordinator,
		metrics: &ClusterMetrics{
			NodeMetrics: make(map[string]*NodeMetrics),
		},
		logger: f.logger,
	}

	// Criar nós iniciais
	for i := 0; i < config.MinNodes; i++ {
		nodeID := fmt.Sprintf("%s-node-%d", config.Name, i+1)

		var nodeConfig *DistributedCacheConfig
		if config.NodeConfigs != nil && config.NodeConfigs[nodeID] != nil {
			nodeConfig = config.NodeConfigs[nodeID]
		}

		cache, err := f.CreateDistributedCache(nodeID, config.Name, nodeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create node %s: %w", nodeID, err)
		}

		cluster.nodes[nodeID] = cache
	}

	f.logger.Info("Cache cluster created", map[string]interface{}{
		"cluster_name":  config.Name,
		"initial_nodes": len(cluster.nodes),
		"min_nodes":     config.MinNodes,
		"max_nodes":     config.MaxNodes,
	})

	return cluster, nil
}

// Builder methods

// WithNodeID configura node ID
func (b *DistributedCacheBuilder) WithNodeID(nodeID string) *DistributedCacheBuilder {
	b.config.NodeID = nodeID
	return b
}

// WithClusterName configura cluster name
func (b *DistributedCacheBuilder) WithClusterName(clusterName string) *DistributedCacheBuilder {
	b.config.ClusterName = clusterName
	return b
}

// WithRedisCoordinator configura coordenador Redis
func (b *DistributedCacheBuilder) WithRedisCoordinator(config *RedisConfig) *DistributedCacheBuilder {
	b.config.RedisConfig = config
	return b
}

// WithGRPCCoordinator configura coordenador gRPC
func (b *DistributedCacheBuilder) WithGRPCCoordinator(config *GRPCConfig) *DistributedCacheBuilder {
	b.config.GRPCConfig = config
	return b
}

// WithConflictResolution configura resolução de conflitos
func (b *DistributedCacheBuilder) WithConflictResolution(config *ConflictResolutionConfig) *DistributedCacheBuilder {
	b.config.ConflictResolution = config
	return b
}

// WithCustomCoordinator injeta coordenador customizado
func (b *DistributedCacheBuilder) WithCustomCoordinator(coordinator NodeCoordinator) *DistributedCacheBuilder {
	b.coordinator = coordinator
	return b
}

// WithCustomSyncManager injeta sync manager customizado
func (b *DistributedCacheBuilder) WithCustomSyncManager(syncManager SyncManager) *DistributedCacheBuilder {
	b.syncManager = syncManager
	return b
}

// WithCustomPartitionManager injeta partition manager customizado
func (b *DistributedCacheBuilder) WithCustomPartitionManager(partitionMgr PartitionManager) *DistributedCacheBuilder {
	b.partitionMgr = partitionMgr
	return b
}

// WithSyncInterval configura intervalo de sincronização
func (b *DistributedCacheBuilder) WithSyncInterval(interval time.Duration) *DistributedCacheBuilder {
	b.config.SyncInterval = interval
	return b
}

// WithHeartbeatInterval configura intervalo de heartbeat
func (b *DistributedCacheBuilder) WithHeartbeatInterval(interval time.Duration) *DistributedCacheBuilder {
	b.config.HeartbeatInterval = interval
	return b
}

// WithConsistencyLevel configura nível de consistência
func (b *DistributedCacheBuilder) WithConsistencyLevel(level ConsistencyLevel) *DistributedCacheBuilder {
	if b.config.ConflictResolution == nil {
		b.config.ConflictResolution = &ConflictResolutionConfig{}
	}
	b.config.ConflictResolution.ConsistencyLevel = level
	return b
}

// Build constrói o cache distribuído
func (b *DistributedCacheBuilder) Build() (*DistributedFlyweightCache, error) {
	// Validar configuração
	if b.config.NodeID == "" {
		b.config.NodeID = uuid.New().String()
	}

	if b.config.ClusterName == "" {
		return nil, fmt.Errorf("cluster name is required")
	}

	// Resolver dependências se não foram injetadas
	var coordinator NodeCoordinator
	var syncManager SyncManager
	var partitionMgr PartitionManager
	var err error

	if b.coordinator != nil {
		coordinator = b.coordinator
	} else {
		coordinator, err = b.resolveCoordinator()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve coordinator: %w", err)
		}
	}

	if b.syncManager != nil {
		syncManager = b.syncManager
	} else {
		syncManager, err = b.resolveSyncManager(coordinator)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve sync manager: %w", err)
		}
	}

	if b.partitionMgr != nil {
		partitionMgr = b.partitionMgr
	} else {
		partitionMgr, err = b.resolvePartitionManager(coordinator)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve partition manager: %w", err)
		}
	}

	// Criar cache
	baseConfig := &CacheConfig{
		Name:    "distributed_cache",
		Type:    CacheTypeMemory,
		MaxSize: 1000,
		TTL:     5 * time.Minute,
	}
	
	return NewDistributedFlyweightCache(
		baseConfig,
		b.config,
		b.nodeID,
		b.clusterName,
		coordinator,
		syncManager,
		partitionMgr,
		b.logger,
	)
}

// Cluster methods

// Start inicia o cluster
func (c *CacheCluster) Start(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.isRunning {
		return fmt.Errorf("cluster is already running")
	}

	// Iniciar coordenador
	if err := c.coordinator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start cluster coordinator: %w", err)
	}

	// Iniciar todos os nós
	for nodeID, cache := range c.nodes {
		if err := cache.Start(ctx); err != nil {
			c.logger.Error("Failed to start node", err, map[string]interface{}{
				"node_id": nodeID,
			})
			continue
		}
	}

	// Iniciar monitoramento de saúde
	go c.healthMonitor(ctx)

	c.isRunning = true

	c.logger.Info("Cache cluster started", map[string]interface{}{
		"cluster_name": c.name,
		"active_nodes": len(c.nodes),
	})

	return nil
}

// Stop para o cluster
func (c *CacheCluster) Stop(ctx context.Context) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.isRunning {
		return nil
	}

	// Parar todos os nós
	for nodeID, cache := range c.nodes {
		if err := cache.Stop(ctx); err != nil {
			c.logger.Error("Failed to stop node", err, map[string]interface{}{
				"node_id": nodeID,
			})
		}
	}

	// Parar coordenador
	if err := c.coordinator.Stop(ctx); err != nil {
		c.logger.Error("Failed to stop cluster coordinator", err, nil)
	}

	c.isRunning = false

	c.logger.Info("Cache cluster stopped", map[string]interface{}{
		"cluster_name": c.name,
	})

	return nil
}

// AddNode adiciona nó ao cluster
func (c *CacheCluster) AddNode(nodeID string, config *DistributedCacheConfig) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(c.nodes) >= c.config.MaxNodes {
		return fmt.Errorf("cluster has reached maximum nodes: %d", c.config.MaxNodes)
	}

	if _, exists := c.nodes[nodeID]; exists {
		return fmt.Errorf("node %s already exists", nodeID)
	}

	// Criar novo nó
	cache, err := c.factory.CreateDistributedCache(nodeID, c.name, config)
	if err != nil {
		return fmt.Errorf("failed to create node %s: %w", nodeID, err)
	}

	// Iniciar se cluster estiver rodando
	if c.isRunning {
		if err := cache.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start node %s: %w", nodeID, err)
		}
	}

	c.nodes[nodeID] = cache

	c.logger.Info("Node added to cluster", map[string]interface{}{
		"cluster_name": c.name,
		"node_id":      nodeID,
		"total_nodes":  len(c.nodes),
	})

	return nil
}

// RemoveNode remove nó do cluster
func (c *CacheCluster) RemoveNode(nodeID string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(c.nodes) <= c.config.MinNodes {
		return fmt.Errorf("cluster cannot have less than minimum nodes: %d", c.config.MinNodes)
	}

	cache, exists := c.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Parar nó
	if err := cache.Stop(context.Background()); err != nil {
		c.logger.Error("Failed to stop node during removal", err, map[string]interface{}{
			"node_id": nodeID,
		})
	}

	delete(c.nodes, nodeID)

	c.logger.Info("Node removed from cluster", map[string]interface{}{
		"cluster_name": c.name,
		"node_id":      nodeID,
		"total_nodes":  len(c.nodes),
	})

	return nil
}

// GetNodes retorna todos os nós do cluster
func (c *CacheCluster) GetNodes() map[string]*DistributedFlyweightCache {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	nodes := make(map[string]*DistributedFlyweightCache)
	for k, v := range c.nodes {
		nodes[k] = v
	}

	return nodes
}

// GetClusterMetrics retorna métricas do cluster
func (c *CacheCluster) GetClusterMetrics() *ClusterMetrics {
	c.metrics.mutex.RLock()
	defer c.metrics.mutex.RUnlock()

	// Copiar métricas
	nodeMetrics := make(map[string]*NodeMetrics)
	for k, v := range c.metrics.NodeMetrics {
		nodeMetrics[k] = v
	}

	return &ClusterMetrics{
		ActiveNodes:     c.metrics.ActiveNodes,
		TotalNodes:      c.metrics.TotalNodes,
		TotalOperations: c.metrics.TotalOperations,
		TotalHits:       c.metrics.TotalHits,
		TotalMisses:     c.metrics.TotalMisses,
		AverageLatency:  c.metrics.AverageLatency,
		NodeMetrics:     nodeMetrics,
	}
}

// Métodos auxiliares privados

func (f *DistributedCacheFactory) registerDefaultComponents() {
	// Redis Coordinator
	f.container.RegisterFactory("redis_coordinator", func() (interface{}, error) {
		config := f.container.GetConfig("redis_config").(*RedisConfig)
		nodeID := f.defaultConfig.NodeID
		clusterName := f.defaultConfig.ClusterName
		metrics := &CoordinatorMetrics{}
		return NewRedisCoordinator(config, nodeID, clusterName, f.logger, metrics)
	})

	// gRPC Coordinator
	f.container.RegisterFactory("grpc_coordinator", func() (interface{}, error) {
		config := f.container.GetConfig("grpc_config").(*GRPCConfig)
		nodeID := f.defaultConfig.NodeID
		clusterName := f.defaultConfig.ClusterName
		metrics := &CoordinatorMetrics{}
		return NewGRPCCoordinator(config, nodeID, clusterName, f.logger, metrics)
	})

	// Sync Manager
	f.container.RegisterFactory("sync_manager", func() (interface{}, error) {
		config := f.container.GetConfig("sync_config").(*SyncConfig)
		coordinator := f.container.GetSingleton("coordinator").(NodeCoordinator)
		return NewDefaultSyncManager(config, "default-node", "default-cluster", coordinator, f.logger)
	})

	// Partition Manager
	f.container.RegisterFactory("partition_manager", func() (interface{}, error) {
		config := f.container.GetConfig("partition_config").(*PartitionConfig)
		coordinator := f.container.GetSingleton("coordinator").(NodeCoordinator)
		return NewConsistentHashPartitionManager(config, "default-node", "default-cluster", coordinator, f.logger)
	})
}

func (f *DistributedCacheFactory) generateNodeID() string {
	return fmt.Sprintf("node-%s", uuid.New().String()[:8])
}

func (f *DistributedCacheFactory) mergeWithDefaults(config *DistributedCacheConfig) *DistributedCacheConfig {
	merged := *f.defaultConfig

	// Sobrescrever com valores fornecidos
	if config.NodeID != "" {
		merged.NodeID = config.NodeID
	}
	if config.ClusterName != "" {
		merged.ClusterName = config.ClusterName
	}
	if config.SyncInterval > 0 {
		merged.SyncInterval = config.SyncInterval
	}
	if config.HeartbeatInterval > 0 {
		merged.HeartbeatInterval = config.HeartbeatInterval
	}
	if config.RedisConfig != nil {
		merged.RedisConfig = config.RedisConfig
	}
	if config.GRPCConfig != nil {
		merged.GRPCConfig = config.GRPCConfig
	}
	if config.ConflictResolution != nil {
		merged.ConflictResolution = config.ConflictResolution
	}

	return &merged
}

func (f *DistributedCacheFactory) copyDefaultConfig() *DistributedCacheConfig {
	config := *f.defaultConfig

	// Deep copy de structs aninhadas
	if f.defaultConfig.ConflictResolution != nil {
		conflictConfig := *f.defaultConfig.ConflictResolution
		config.ConflictResolution = &conflictConfig
	}

	if f.defaultConfig.RedisConfig != nil {
		redisConfig := *f.defaultConfig.RedisConfig
		config.RedisConfig = &redisConfig
	}

	if f.defaultConfig.GRPCConfig != nil {
		grpcConfig := *f.defaultConfig.GRPCConfig
		config.GRPCConfig = &grpcConfig
	}

	return &config
}

func (f *DistributedCacheFactory) resolveCoordinator(config *DistributedCacheConfig) (NodeCoordinator, error) {
	if config.RedisConfig != nil {
		return NewRedisCoordinator(config.RedisConfig, config.NodeID, config.ClusterName, f.logger, &CoordinatorMetrics{})
	}

	if config.GRPCConfig != nil {
		return NewGRPCCoordinator(config.GRPCConfig, config.NodeID, config.ClusterName, f.logger, &CoordinatorMetrics{})
	}

	// Padrão Redis
	defaultRedisConfig := &RedisConfig{
		Address: "localhost:6379",
		DB:      0,
	}

	return NewRedisCoordinator(defaultRedisConfig, config.NodeID, config.ClusterName, f.logger, &CoordinatorMetrics{})
}

func (f *DistributedCacheFactory) resolveSyncManager(config *DistributedCacheConfig, coordinator NodeCoordinator) (SyncManager, error) {
	syncConfig := &SyncConfig{
		BatchSize:      100,
		BatchTimeout:   1 * time.Second,
		RetryAttempts:  3,
		RetryDelay:     500 * time.Millisecond,
		WorkerCount:    2,
		QueueSize:      1000,
		EnableBatching: true,
		EnableRetry:    true,
	}

	return NewDefaultSyncManager(syncConfig, config.NodeID, config.ClusterName, coordinator, f.logger)
}

func (f *DistributedCacheFactory) resolvePartitionManager(config *DistributedCacheConfig, coordinator NodeCoordinator) (PartitionManager, error) {
	partitionConfig := &PartitionConfig{
		ReplicationFactor:   2,
		VirtualNodes:        150,
		RebalanceThreshold:  0.1,
		RebalanceInterval:   5 * time.Minute,
		HashFunction:        "sha256",
	}

	return NewConsistentHashPartitionManager(partitionConfig, config.NodeID, config.ClusterName, coordinator, f.logger)
}

func (b *DistributedCacheBuilder) resolveCoordinator() (NodeCoordinator, error) {
	if b.config.RedisConfig != nil {
		return NewRedisCoordinator(b.config.RedisConfig, b.nodeID, b.clusterName, b.logger, &CoordinatorMetrics{})
	}

	if b.config.GRPCConfig != nil {
		return NewGRPCCoordinator(b.config.GRPCConfig, b.nodeID, b.clusterName, b.logger, &CoordinatorMetrics{})
	}

	// Padrão Redis
	defaultConfig := &RedisConfig{
		Address: "localhost:6379",
		DB:      0,
	}

	return NewRedisCoordinator(defaultConfig, b.nodeID, b.clusterName, b.logger, &CoordinatorMetrics{})
}

func (b *DistributedCacheBuilder) resolveSyncManager(coordinator NodeCoordinator) (SyncManager, error) {
	config := &SyncConfig{
		BatchSize:      100,
		BatchTimeout:   1 * time.Second,
		RetryAttempts:  3,
		RetryDelay:     500 * time.Millisecond,
		WorkerCount:    2,
		QueueSize:      1000,
		EnableBatching: true,
		EnableRetry:    true,
	}

	return NewDefaultSyncManager(config, b.nodeID, b.clusterName, coordinator, b.logger)
}

func (b *DistributedCacheBuilder) resolvePartitionManager(coordinator NodeCoordinator) (PartitionManager, error) {
	config := &PartitionConfig{
		ReplicationFactor:   2,
		VirtualNodes:        150,
		RebalanceThreshold:  0.1,
		RebalanceInterval:   5 * time.Minute,
		HashFunction:        "sha256",
	}

	return NewConsistentHashPartitionManager(config, b.nodeID, b.clusterName, coordinator, b.logger)
}

func (c *CacheCluster) healthMonitor(ctx context.Context) {
	ticker := time.NewTicker(c.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkNodeHealth()
		}
	}
}

func (c *CacheCluster) checkNodeHealth() {
	c.mutex.RLock()
	nodes := make(map[string]*DistributedFlyweightCache)
	for k, v := range c.nodes {
		nodes[k] = v
	}
	c.mutex.RUnlock()

	activeNodes := 0

	for nodeID, cache := range nodes {
		isHealthy := cache.isRunning

		if isHealthy {
			activeNodes++
		}

		// Atualizar métricas do nó
		c.metrics.mutex.Lock()
		if c.metrics.NodeMetrics[nodeID] == nil {
			c.metrics.NodeMetrics[nodeID] = &NodeMetrics{
				NodeID:        nodeID,
				CustomMetrics: make(map[string]interface{}),
			}
		}

		nodeMetric := c.metrics.NodeMetrics[nodeID]
		if isHealthy {
			nodeMetric.Status = NodeStatusActive
		} else {
			nodeMetric.Status = NodeStatusFailed
		}
		nodeMetric.LastUpdate = time.Now()

		// Obter estatísticas do cache
		if isHealthy {
			stats := cache.GetDistributedStats()
			if stats != nil {
				nodeMetric.Operations = stats.SyncOperations
				if stats.CacheStats != nil {
					nodeMetric.Hits = stats.CacheStats.HitCount
					nodeMetric.Misses = stats.CacheStats.MissCount
				}
			}
		}
		c.metrics.mutex.Unlock()
	}

	// Atualizar métricas do cluster
	c.metrics.mutex.Lock()
	c.metrics.ActiveNodes = activeNodes
	c.metrics.TotalNodes = len(nodes)
	c.metrics.mutex.Unlock()

	// Auto-scaling se habilitado
	if c.config.AutoScaling {
		c.handleAutoScaling(activeNodes)
	}
}

func (c *CacheCluster) handleAutoScaling(activeNodes int) {
	// Lógica básica de auto-scaling
	if activeNodes < c.config.MinNodes {
		// Adicionar nós se necessário
		needed := c.config.MinNodes - activeNodes
		for i := 0; i < needed; i++ {
			nodeID := fmt.Sprintf("%s-auto-%d", c.name, time.Now().Unix())
			if err := c.AddNode(nodeID, nil); err != nil {
				c.logger.Error("Failed to auto-scale up", err, map[string]interface{}{
					"cluster_name": c.name,
					"node_id":      nodeID,
				})
			}
		}
	}
}

// DIContainer methods

// RegisterSingleton registra singleton
func (d *DIContainer) RegisterSingleton(name string, instance interface{}) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.singletons[name] = instance
}

// RegisterFactory registra factory
func (d *DIContainer) RegisterFactory(name string, factory func() (interface{}, error)) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.factories[name] = factory
}

// GetSingleton obtém singleton
func (d *DIContainer) GetSingleton(name string) interface{} {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.singletons[name]
}

// GetInstance obtém instância (singleton ou nova via factory)
func (d *DIContainer) GetInstance(name string) (interface{}, error) {
	d.mutex.RLock()

	// Verificar singleton primeiro
	if instance, exists := d.singletons[name]; exists {
		d.mutex.RUnlock()
		return instance, nil
	}

	// Verificar factory
	if factory, exists := d.factories[name]; exists {
		d.mutex.RUnlock()
		return factory()
	}

	d.mutex.RUnlock()
	return nil, fmt.Errorf("no registration found for: %s", name)
}

// SetConfig define configuração
func (d *DIContainer) SetConfig(name string, config interface{}) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.configs[name] = config
}

// GetConfig obtém configuração
func (d *DIContainer) GetConfig(name string) interface{} {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.configs[name]
}

/**
 * @file redis_coordinator.go
 * @description Coordenador Redis para sincronização distribuída seguindo arquitetura hexagonal
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/redis_coordinator.go
 */

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisCoordinator implementa NodeCoordinator usando Redis como backend
type RedisCoordinator struct {
	client        redis.UniversalClient
	config        *RedisConfig
	nodeID        string
	clusterName   string
	isRunning     bool
	stopChan      chan struct{}
	opsChan       chan *SyncOperation
	mutex         sync.RWMutex
	logger        Logger
	metrics       *CoordinatorMetrics
	
	// Canais para pub/sub
	pubsub        *redis.PubSub
	
	// Gerenciamento de nós
	nodes         map[string]*NodeInfo
	nodesMutex    sync.RWMutex
	
	// Heartbeat
	heartbeatTicker *time.Ticker
	
	// Configuração
	keyPrefix     string
	channel       string
}

// NewRedisCoordinator cria novo coordenador Redis seguindo padrão Factory
func NewRedisCoordinator(config *RedisConfig, nodeID, clusterName string, logger Logger, metrics *CoordinatorMetrics) (*RedisCoordinator, error) {
	if config == nil {
		return nil, fmt.Errorf("redis config cannot be nil")
	}
	
	if nodeID == "" {
		return nil, fmt.Errorf("node ID cannot be empty")
	}
	
	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	
	// Criar cliente Redis baseado na configuração
	var client redis.UniversalClient
	
	if config.ClusterMode && len(config.Addresses) > 0 {
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    config.Addresses,
			Password: config.Password,
			PoolSize: config.PoolSize,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:     config.Address,
			Password: config.Password,
			DB:       config.DB,
			PoolSize: config.PoolSize,
		})
	}
	
	// Testar conexão
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	keyPrefix := config.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = fmt.Sprintf("cache:%s", clusterName)
	}
	
	channel := config.Channel
	if channel == "" {
		channel = fmt.Sprintf("%s:sync", keyPrefix)
	}
	
	coordinator := &RedisCoordinator{
		client:      client,
		config:      config,
		nodeID:      nodeID,
		clusterName: clusterName,
		stopChan:    make(chan struct{}),
		opsChan:     make(chan *SyncOperation, 1000),
		logger:      logger,
		metrics:     metrics,
		nodes:       make(map[string]*NodeInfo),
		keyPrefix:   keyPrefix,
		channel:     channel,
	}
	
	return coordinator, nil
}

// Start inicia o coordenador seguindo padrão Command
func (r *RedisCoordinator) Start(ctx context.Context) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	if r.isRunning {
		return fmt.Errorf("coordinator is already running")
	}
	
	// Inicializar pub/sub
	r.pubsub = r.client.Subscribe(ctx, r.channel)
	
	// Registrar este nó
	nodeInfo := &NodeInfo{
		ID:       r.nodeID,
		Status:   NodeStatusActive,
		LastSeen: time.Now(),
		Version:  "1.0.0",
		Metadata: map[string]string{
			"cluster": r.clusterName,
			"type":    "redis_coordinator",
		},
	}
	
	if err := r.registerNodeInternal(ctx, nodeInfo); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	
	r.isRunning = true
	
	// Iniciar goroutines de gerenciamento
	go r.subscriptionHandler(ctx)
	go r.heartbeatHandler(ctx)
	go r.nodeCleanupHandler(ctx)
	
	r.logger.Info("Redis coordinator started", map[string]interface{}{
		"node_id":      r.nodeID,
		"cluster_name": r.clusterName,
		"channel":      r.channel,
	})
	
	return nil
}

// Stop para o coordenador
func (r *RedisCoordinator) Stop(ctx context.Context) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	if !r.isRunning {
		return nil
	}
	
	// Desregistrar nó
	if err := r.UnregisterNode(ctx, r.nodeID); err != nil {
		r.logger.Error("Failed to unregister node", err, map[string]interface{}{
			"node_id": r.nodeID,
		})
	}
	
	// Parar heartbeat
	if r.heartbeatTicker != nil {
		r.heartbeatTicker.Stop()
	}
	
	// Fechar pub/sub
	if r.pubsub != nil {
		r.pubsub.Close()
	}
	
	// Sinalizar parada
	close(r.stopChan)
	
	r.isRunning = false
	
	r.logger.Info("Redis coordinator stopped", map[string]interface{}{
		"node_id": r.nodeID,
	})
	
	return nil
}

// IsRunning verifica se o coordenador está ativo
func (r *RedisCoordinator) IsRunning() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.isRunning
}

// DiscoverNodes descobre nós ativos no cluster
func (r *RedisCoordinator) DiscoverNodes(ctx context.Context) ([]*NodeInfo, error) {
	pattern := fmt.Sprintf("%s:nodes:*", r.keyPrefix)
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to discover nodes: %w", err)
	}
	
	var nodes []*NodeInfo
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		
		var node NodeInfo
		if err := json.Unmarshal([]byte(data), &node); err != nil {
			continue
		}
		
		// Verificar se o nó ainda está ativo (heartbeat recente)
		if time.Since(node.LastSeen) > 2*time.Minute {
			node.Status = NodeStatusInactive
		}
		
		nodes = append(nodes, &node)
	}
	
	// Atualizar cache local
	r.nodesMutex.Lock()
	r.nodes = make(map[string]*NodeInfo)
	for _, node := range nodes {
		r.nodes[node.ID] = node
	}
	r.nodesMutex.Unlock()
	
	return nodes, nil
}

// RegisterNode registra um novo nó no cluster
func (r *RedisCoordinator) RegisterNode(ctx context.Context, node *NodeInfo) error {
	return r.registerNodeInternal(ctx, node)
}

// registerNodeInternal implementação interna de registro
func (r *RedisCoordinator) registerNodeInternal(ctx context.Context, node *NodeInfo) error {
	if node == nil {
		return fmt.Errorf("node info cannot be nil")
	}
	
	node.LastSeen = time.Now()
	
	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}
	
	key := fmt.Sprintf("%s:nodes:%s", r.keyPrefix, node.ID)
	
	// Registrar com TTL para limpeza automática
	if err := r.client.Set(ctx, key, data, 5*time.Minute).Err(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	
	// Atualizar cache local
	r.nodesMutex.Lock()
	r.nodes[node.ID] = node
	r.nodesMutex.Unlock()
	
	r.logger.Info("Node registered", map[string]interface{}{
		"node_id": node.ID,
		"status":  node.Status,
	})
	
	return nil
}

// UnregisterNode remove um nó do cluster
func (r *RedisCoordinator) UnregisterNode(ctx context.Context, nodeID string) error {
	key := fmt.Sprintf("%s:nodes:%s", r.keyPrefix, nodeID)
	
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to unregister node: %w", err)
	}
	
	// Remover do cache local
	r.nodesMutex.Lock()
	delete(r.nodes, nodeID)
	r.nodesMutex.Unlock()
	
	r.logger.Info("Node unregistered", map[string]interface{}{
		"node_id": nodeID,
	})
	
	return nil
}

// UpdateNodeStatus atualiza o status de um nó
func (r *RedisCoordinator) UpdateNodeStatus(ctx context.Context, nodeID string, status NodeStatus) error {
	key := fmt.Sprintf("%s:nodes:%s", r.keyPrefix, nodeID)
	
	// Buscar nó atual
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get node info: %w", err)
	}
	
	var node NodeInfo
	if err := json.Unmarshal([]byte(data), &node); err != nil {
		return fmt.Errorf("failed to unmarshal node info: %w", err)
	}
	
	// Atualizar status
	node.Status = status
	node.LastSeen = time.Now()
	
	// Salvar de volta
	updatedData, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal updated node info: %w", err)
	}
	
	if err := r.client.Set(ctx, key, updatedData, 5*time.Minute).Err(); err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}
	
	// Atualizar cache local
	r.nodesMutex.Lock()
	if localNode, exists := r.nodes[nodeID]; exists {
		localNode.Status = status
		localNode.LastSeen = time.Now()
	}
	r.nodesMutex.Unlock()
	
	return nil
}

// BroadcastOperation envia operação para todos os nós
func (r *RedisCoordinator) BroadcastOperation(ctx context.Context, op *SyncOperation) error {
	if op == nil {
		return fmt.Errorf("sync operation cannot be nil")
	}
	
	op.NodeID = r.nodeID
	op.Timestamp = time.Now()
	
	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal sync operation: %w", err)
	}
	
	if err := r.client.Publish(ctx, r.channel, data).Err(); err != nil {
		return fmt.Errorf("failed to broadcast operation: %w", err)
	}
	
	r.metrics.RecordCustomMetric("sync_operations_sent", 1)
	
	return nil
}

// SendToNode envia operação para nó específico
func (r *RedisCoordinator) SendToNode(ctx context.Context, nodeID string, op *SyncOperation) error {
	if op == nil {
		return fmt.Errorf("sync operation cannot be nil")
	}
	
	op.NodeID = r.nodeID
	op.Timestamp = time.Now()
	
	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal sync operation: %w", err)
	}
	
	// Canal específico do nó
	nodeChannel := fmt.Sprintf("%s:node:%s", r.keyPrefix, nodeID)
	
	if err := r.client.Publish(ctx, nodeChannel, data).Err(); err != nil {
		return fmt.Errorf("failed to send operation to node: %w", err)
	}
	
	return nil
}

// ReceiveOperations retorna canal para receber operações
func (r *RedisCoordinator) ReceiveOperations(ctx context.Context) (<-chan *SyncOperation, error) {
	if !r.isRunning {
		return nil, fmt.Errorf("coordinator is not running")
	}
	
	return r.opsChan, nil
}

// SendHeartbeat envia heartbeat do nó
func (r *RedisCoordinator) SendHeartbeat(ctx context.Context, nodeInfo *NodeInfo) error {
	return r.registerNodeInternal(ctx, nodeInfo)
}

// GetNodeHealth obtém saúde de um nó
func (r *RedisCoordinator) GetNodeHealth(nodeID string) (*NodeHealth, error) {
	r.nodesMutex.RLock()
	node, exists := r.nodes[nodeID]
	r.nodesMutex.RUnlock()
	
	if !exists {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}
	
	health := &NodeHealth{
		NodeID:        node.ID,
		Status:        node.Status,
		LastHeartbeat: node.LastSeen,
		IsReachable:   time.Since(node.LastSeen) < 2*time.Minute,
		Version:       node.Version,
	}
	
	// Calcular tempo de resposta (simulado)
	if health.IsReachable {
		health.ResponseTime = 50 * time.Millisecond
	} else {
		health.ResponseTime = time.Duration(0)
	}
	
	return health, nil
}

// GetCoordinatorType retorna o tipo do coordenador
func (r *RedisCoordinator) GetCoordinatorType() CoordinatorType {
	return CoordinatorRedis
}

// UpdateConfig atualiza configuração do coordenador
func (r *RedisCoordinator) UpdateConfig(config interface{}) error {
	redisConfig, ok := config.(*RedisConfig)
	if !ok {
		return fmt.Errorf("invalid config type, expected *RedisConfig")
	}
	
	r.mutex.Lock()
	defer r.mutex.Unlock()
	
	r.config = redisConfig
	
	return nil
}

// subscriptionHandler gerencia mensagens do pub/sub
func (r *RedisCoordinator) subscriptionHandler(ctx context.Context) {
	ch := r.pubsub.Channel()
	
	for {
		select {
		case <-r.stopChan:
			return
		case <-ctx.Done():
			return
		case msg := <-ch:
			if msg == nil {
				continue
			}
			
			var op SyncOperation
			if err := json.Unmarshal([]byte(msg.Payload), &op); err != nil {
				r.logger.Error("Failed to unmarshal sync operation", err, nil)
				continue
			}
			
			// Ignorar operações do próprio nó
			if op.NodeID == r.nodeID {
				continue
			}
			
			// Enviar para canal de operações
			select {
			case r.opsChan <- &op:
				r.metrics.RecordCustomMetric("sync_operations_received", 1)
			default:
				r.logger.Warn("Operations channel full, dropping operation", map[string]interface{}{
					"operation_id": op.ID,
					"type":         op.Type,
				})
			}
		}
	}
}

// heartbeatHandler gerencia heartbeats periódicos
func (r *RedisCoordinator) heartbeatHandler(ctx context.Context) {
	r.heartbeatTicker = time.NewTicker(30 * time.Second)
	defer r.heartbeatTicker.Stop()
	
	for {
		select {
		case <-r.stopChan:
			return
		case <-ctx.Done():
			return
		case <-r.heartbeatTicker.C:
			nodeInfo := &NodeInfo{
				ID:       r.nodeID,
				Status:   NodeStatusActive,
				LastSeen: time.Now(),
				Version:  "1.0.0",
				Metadata: map[string]string{
					"cluster": r.clusterName,
					"type":    "redis_coordinator",
				},
			}
			
			if err := r.registerNodeInternal(ctx, nodeInfo); err != nil {
				r.logger.Error("Failed to send heartbeat", err, map[string]interface{}{
					"node_id": r.nodeID,
				})
			}
		}
	}
}

// nodeCleanupHandler limpa nós inativos
func (r *RedisCoordinator) nodeCleanupHandler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-r.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Descobrir e limpar nós inativos
			if _, err := r.DiscoverNodes(ctx); err != nil {
				r.logger.Error("Failed to discover nodes during cleanup", err, nil)
			}
		}
	}
}

// Logger interface para logging estruturado
type Logger interface {
	Info(message string, fields map[string]interface{})
	Error(message string, err error, fields map[string]interface{})
	Warn(message string, fields map[string]interface{})
	Debug(message string, fields map[string]interface{})
}
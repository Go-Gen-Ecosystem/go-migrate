/**
 * @file distributed_flyweight_cache.go
 * @description Extensão distribuída do FlyweightCache seguindo arquitetura hexagonal
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/distributed_flyweight_cache.go
 */

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DistributedFlyweightCache estende FlyweightCache com capacidades distribuídas
type DistributedFlyweightCache struct {
	flyweightCache   FlyweightCache
	
	// Configuração distribuída
	config           *DistributedCacheConfig
	nodeID           string
	clusterName      string
	
	// Coordenadores injetados via DI
	coordinator      NodeCoordinator
	syncManager      SyncManager
	partitionManager PartitionManager
	
	// Estado distribuído
	isDistributed    bool
	isRunning        bool
	
	// Canais de sincronização
	syncChan         chan *SyncOperation
	stopChan         chan struct{}
	
	// Controle de versão para resolução de conflitos
	versionVector    map[string]int64
	versionMutex     sync.RWMutex
	
	// Buffer de operações pendentes
	pendingOps       map[string]*SyncOperation
	pendingMutex     sync.RWMutex
	
	// Métricas distribuídas
	distributedMetrics *DistributedMetrics
	
	// Logger injetado
	logger           Logger
	
	mutex            sync.RWMutex
}

// DistributedMetrics métricas específicas para operações distribuídas
type DistributedMetrics struct {
	SyncOperationsSent     int64
	SyncOperationsReceived int64
	ConflictsResolved      int64
	PartitionMigrations    int64
	NetworkLatency         time.Duration
	
	mutex sync.RWMutex
}

// NewDistributedFlyweightCache cria cache distribuído seguindo padrão Factory
func NewDistributedFlyweightCache(
	baseConfig *CacheConfig,
	distributedConfig *DistributedCacheConfig,
	nodeID, clusterName string,
	coordinator NodeCoordinator,
	syncManager SyncManager,
	partitionManager PartitionManager,
	logger Logger,
) (*DistributedFlyweightCache, error) {
	
	if baseConfig == nil {
		return nil, fmt.Errorf("base cache config cannot be nil")
	}
	
	if distributedConfig == nil {
		return nil, fmt.Errorf("distributed cache config cannot be nil")
	}
	
	if nodeID == "" {
		return nil, fmt.Errorf("node ID cannot be empty")
	}
	
	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	
	if coordinator == nil {
		return nil, fmt.Errorf("coordinator cannot be nil")
	}
	
	if syncManager == nil {
		return nil, fmt.Errorf("sync manager cannot be nil")
	}
	
	if partitionManager == nil {
		return nil, fmt.Errorf("partition manager cannot be nil")
	}
	
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}
	
	// Criar cache base
	baseCache, err := NewFlyweightCache(baseConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create base cache: %w", err)
	}
	
	// Configurar cache distribuído
	distributedCache := &DistributedFlyweightCache{
		flyweightCache:     baseCache,
		config:             distributedConfig,
		nodeID:             nodeID,
		clusterName:        clusterName,
		coordinator:        coordinator,
		syncManager:        syncManager,
		partitionManager:   partitionManager,
		isDistributed:      distributedConfig.Enabled,
		syncChan:           make(chan *SyncOperation, distributedConfig.SyncBufferSize),
		stopChan:           make(chan struct{}),
		versionVector:      make(map[string]int64),
		pendingOps:         make(map[string]*SyncOperation),
		distributedMetrics: &DistributedMetrics{},
		logger:             logger,
	}
	
	return distributedCache, nil
}

// Start inicia o cache distribuído
func (d *DistributedFlyweightCache) Start(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if d.isRunning {
		return fmt.Errorf("distributed cache is already running")
	}
	
	if !d.isDistributed {
		d.logger.Info("Cache running in standalone mode", map[string]interface{}{
			"node_id": d.nodeID,
		})
		d.isRunning = true
		return nil
	}
	
	// Iniciar coordenador
	if err := d.coordinator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start coordinator: %w", err)
	}
	
	// Iniciar sync manager
	if err := d.syncManager.StartSync(ctx); err != nil {
		return fmt.Errorf("failed to start sync manager: %w", err)
	}
	
	// Iniciar partition manager se configurado
	if d.partitionManager != nil {
		// Implementação básica - não há método Start na interface PartitionManager
		d.logger.Info("Partition manager initialized", map[string]interface{}{
			"node_id": d.nodeID,
		})
	}
	
	// Iniciar handlers de sincronização
	go d.syncOperationHandler(ctx)
	go d.conflictResolutionHandler(ctx)
	go d.heartbeatHandler(ctx)
	
	// Descobrir nós existentes
	if err := d.discoverAndJoinCluster(ctx); err != nil {
		d.logger.Error("Failed to join cluster", err, map[string]interface{}{
			"cluster_name": d.clusterName,
		})
	}
	
	d.isRunning = true
	
	d.logger.Info("Distributed cache started", map[string]interface{}{
		"node_id":      d.nodeID,
		"cluster_name": d.clusterName,
		"coordinator":  d.coordinator.GetCoordinatorType(),
	})
	
	return nil
}

// IsRunning retorna se o cache distribuído está em execução
func (d *DistributedFlyweightCache) IsRunning() bool {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.isRunning
}

// Stop para o cache distribuído
func (d *DistributedFlyweightCache) Stop(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if !d.isRunning {
		return nil
	}
	
	// Sinalizar parada
	close(d.stopChan)
	
	// Parar componentes distribuídos
	if d.isDistributed {
		if d.partitionManager != nil {
			// Implementação básica - não há método Stop na interface PartitionManager
			d.logger.Info("Partition manager stopped", map[string]interface{}{
				"node_id": d.nodeID,
			})
		}
		
		if d.syncManager != nil {
			d.syncManager.StopSync(ctx)
		}
		
		if d.coordinator != nil {
			d.coordinator.Stop(ctx)
		}
	}
	
	d.isRunning = false
	
	d.logger.Info("Distributed cache stopped", map[string]interface{}{
		"node_id": d.nodeID,
	})
	
	return nil
}

// Set sobrescreve método base para incluir sincronização distribuída
func (d *DistributedFlyweightCache) Set(key string, value interface{}, ttl time.Duration) error {
	// Executar operação local primeiro
	if err := d.flyweightCache.Set(context.Background(), key, value, ttl); err != nil {
		return err
	}
	
	// Se não estiver em modo distribuído, retornar
	if !d.isDistributed || !d.isRunning {
		return nil
	}
	
	// Criar operação de sincronização
	op := &SyncOperation{
		ID:        uuid.New().String(),
		Type:      SyncOpSet,
		NodeID:    d.nodeID,
		Timestamp: time.Now(),
		Key:       key,
		TTL:       ttl,
		Version:   d.incrementVersion(d.nodeID),
		Metadata:  make(map[string]interface{}),
	}
	
	// Serializar valor
	if serializedValue, err := d.serializeValue(value); err == nil {
		op.Value = serializedValue
	} else {
		d.logger.Error("Failed to serialize value for sync", err, map[string]interface{}{
			"key": key,
		})
		return nil // Não falhar a operação local por erro de sincronização
	}
	
	// Enviar para sincronização
	return d.broadcastOperation(context.Background(), op)
}

// Delete sobrescreve método base para incluir sincronização distribuída
func (d *DistributedFlyweightCache) Delete(key string) error {
	// Executar operação local primeiro
	if err := d.flyweightCache.Delete(context.Background(), key); err != nil {
		return err
	}
	
	// Se não estiver em modo distribuído, retornar
	if !d.isDistributed || !d.isRunning {
		return nil
	}
	
	// Criar operação de sincronização
	op := &SyncOperation{
		ID:        uuid.New().String(),
		Type:      SyncOpDelete,
		NodeID:    d.nodeID,
		Timestamp: time.Now(),
		Key:       key,
		Version:   d.incrementVersion(d.nodeID),
		Metadata:  make(map[string]interface{}),
	}
	
	// Enviar para sincronização
	return d.broadcastOperation(context.Background(), op)
}

// Clear sobrescreve método base para incluir sincronização distribuída
func (d *DistributedFlyweightCache) Clear() error {
	// Executar operação local primeiro
	if err := d.flyweightCache.Clear(context.Background()); err != nil {
		return err
	}
	
	// Se não estiver em modo distribuído, retornar
	if !d.isDistributed || !d.isRunning {
		return nil
	}
	
	// Criar operação de sincronização
	op := &SyncOperation{
		ID:        uuid.New().String(),
		Type:      SyncOpClear,
		NodeID:    d.nodeID,
		Timestamp: time.Now(),
		Version:   d.incrementVersion(d.nodeID),
		Metadata:  make(map[string]interface{}),
	}
	
	// Enviar para sincronização
	return d.broadcastOperation(context.Background(), op)
}

// Get mantém comportamento base mas adiciona métricas distribuídas
func (d *DistributedFlyweightCache) Get(key string) (interface{}, bool) {
	value, found, err := d.flyweightCache.Get(context.Background(), key)
	if err != nil {
		d.logger.Error("Error getting value from flyweight cache", err, map[string]interface{}{"key": key})
		return nil, false
	}
	
	// Se não encontrado localmente e estiver em modo distribuído, tentar buscar em outros nós
	if !found && d.isDistributed && d.isRunning && d.config.CrossNodeLookup {
		if remoteValue, remoteFound := d.getFromRemoteNodes(key); remoteFound {
			// Armazenar localmente para cache
			if ttl := d.config.RemoteCacheTTL; ttl > 0 {
				if err := d.flyweightCache.Set(context.Background(), key, remoteValue, ttl); err != nil {
					d.logger.Error("Error setting remote value in local cache", err, map[string]interface{}{"key": key})
				}
			}
			return remoteValue, true
		}
	}
	
	return value, found
}

// GetDistributedStats retorna estatísticas distribuídas
func (d *DistributedFlyweightCache) GetDistributedStats() *DistributedStats {
	d.distributedMetrics.mutex.RLock()
	defer d.distributedMetrics.mutex.RUnlock()
	
	stats := &DistributedStats{
		NodeCount:              1, // Este nó
		ActiveNodes:            1, // Este nó está ativo
		TotalCacheSize:         0, // Será calculado abaixo
		ReplicationRate:        0.0,
		SyncOperations:         d.distributedMetrics.SyncOperationsSent + d.distributedMetrics.SyncOperationsReceived,
		ConflictRate:           0.0,
		NetworkLatency:         d.distributedMetrics.NetworkLatency,
		NodeStats:              make(map[string]*CacheStats),
	}
	
	// Adicionar informações dos nós
	if d.coordinator != nil {
		if nodes, err := d.coordinator.DiscoverNodes(context.Background()); err == nil {
			stats.NodeCount = len(nodes)
			stats.ActiveNodes = 0
			for _, node := range nodes {
				if node.Status == NodeStatusActive {
					stats.ActiveNodes++
				}
			}
		}
	}
	
	return stats
}

// Métodos privados de sincronização

func (d *DistributedFlyweightCache) broadcastOperation(ctx context.Context, op *SyncOperation) error {
	if d.coordinator == nil {
		return fmt.Errorf("coordinator not available")
	}
	
	// Enviar via coordenador
	if err := d.coordinator.BroadcastOperation(ctx, op); err != nil {
		d.logger.Error("Failed to broadcast operation", err, map[string]interface{}{
			"operation_id": op.ID,
			"type":         op.Type,
		})
		return err
	}
	
	// Atualizar métricas
	d.distributedMetrics.mutex.Lock()
	d.distributedMetrics.SyncOperationsSent++
	d.distributedMetrics.mutex.Unlock()
	
	return nil
}

func (d *DistributedFlyweightCache) syncOperationHandler(ctx context.Context) {
	if d.coordinator == nil {
		return
	}
	
	opsChan, err := d.coordinator.ReceiveOperations(ctx)
	if err != nil {
		d.logger.Error("Failed to get operations channel", err, nil)
		return
	}
	
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case op := <-opsChan:
			if op == nil {
				continue
			}
			
			// Ignorar operações do próprio nó
			if op.NodeID == d.nodeID {
				continue
			}
			
			// Processar operação
			if err := d.processSyncOperation(op); err != nil {
				d.logger.Error("Failed to process sync operation", err, map[string]interface{}{
					"operation_id": op.ID,
					"type":         op.Type,
					"source_node":  op.NodeID,
				})
			} else {
				// Atualizar métricas
				d.distributedMetrics.mutex.Lock()
				d.distributedMetrics.SyncOperationsReceived++
				d.distributedMetrics.mutex.Unlock()
			}
		}
	}
}

func (d *DistributedFlyweightCache) processSyncOperation(op *SyncOperation) error {
	// Verificar se a operação já foi processada
	if d.isDuplicateOperation(op) {
		return nil
	}
	
	// Verificar conflitos de versão
	if d.hasVersionConflict(op) {
		return d.resolveConflict(op)
	}
	
	// Aplicar operação localmente
	switch op.Type {
	case SyncOpSet:
		value, ok := op.Value.([]byte)
		if !ok {
			return fmt.Errorf("invalid value type for deserialization")
		}
		
		deserializedValue, err := d.deserializeValue(value)
		if err != nil {
			return fmt.Errorf("failed to deserialize value: %w", err)
		}
		
		ttl := time.Duration(op.TTL)
		if ttl <= 0 {
			ttl = d.config.RemoteCacheTTL
		}
		
		return d.flyweightCache.Set(context.Background(), op.Key, deserializedValue, ttl)
		
	case SyncOpDelete:
		return d.flyweightCache.Delete(context.Background(), op.Key)
		
	case SyncOpClear:
		return d.flyweightCache.Clear(context.Background())
		
	case SyncOpExpire:
		// Implementar expiração específica se necessário
		return d.flyweightCache.Delete(context.Background(), op.Key)
		
	default:
		return fmt.Errorf("unknown sync operation type: %v", op.Type)
	}
}

func (d *DistributedFlyweightCache) conflictResolutionHandler(ctx context.Context) {
	ticker := time.NewTicker(d.config.ConflictResolutionInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.processPendingConflicts()
		}
	}
}

func (d *DistributedFlyweightCache) heartbeatHandler(ctx context.Context) {
	if d.coordinator == nil {
		return
	}
	
	ticker := time.NewTicker(d.config.HeartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodeInfo := &NodeInfo{
				ID:       d.nodeID,
				Status:   NodeStatusActive,
				LastSeen: time.Now(),
				Version:  "1.0.0",
				Metadata: map[string]string{
					"cluster":    d.clusterName,
					"cache_name": d.config.ClusterName,
					"cache_type": "distributed_flyweight",
				},
			}
			
			if err := d.coordinator.SendHeartbeat(ctx, nodeInfo); err != nil {
				d.logger.Error("Failed to send heartbeat", err, map[string]interface{}{
					"node_id": d.nodeID,
				})
			}
		}
	}
}

func (d *DistributedFlyweightCache) discoverAndJoinCluster(ctx context.Context) error {
	if d.coordinator == nil {
		return fmt.Errorf("coordinator not available")
	}
	
	// Descobrir nós existentes
	nodes, err := d.coordinator.DiscoverNodes(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover nodes: %w", err)
	}
	
	d.logger.Info("Discovered cluster nodes", map[string]interface{}{
		"node_count": len(nodes),
		"cluster":    d.clusterName,
	})
	
	// Registrar este nó
	nodeInfo := &NodeInfo{
		ID:       d.nodeID,
		Status:   NodeStatusSynchronizing,
		LastSeen: time.Now(),
		Version:  "1.0.0",
		Metadata: map[string]string{
			"cluster":    d.clusterName,
			"cache_name": d.config.ClusterName,
			"cache_type": "distributed_flyweight",
		},
	}
	
	if err := d.coordinator.RegisterNode(ctx, nodeInfo); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	
	// Atualizar status para ativo
	if err := d.coordinator.UpdateNodeStatus(ctx, d.nodeID, NodeStatusActive); err != nil {
		d.logger.Error("Failed to update node status to active", err, nil)
	}
	
	return nil
}

func (d *DistributedFlyweightCache) getFromRemoteNodes(key string) (interface{}, bool) {
	// Implementação básica - pode ser expandida para consultar nós específicos
	// Por enquanto, retorna não encontrado
	return nil, false
}

func (d *DistributedFlyweightCache) incrementVersion(nodeID string) int64 {
	d.versionMutex.Lock()
	defer d.versionMutex.Unlock()
	
	d.versionVector[nodeID]++
	return d.versionVector[nodeID]
}

func (d *DistributedFlyweightCache) isDuplicateOperation(op *SyncOperation) bool {
	d.pendingMutex.RLock()
	defer d.pendingMutex.RUnlock()
	
	_, exists := d.pendingOps[op.ID]
	return exists
}

func (d *DistributedFlyweightCache) hasVersionConflict(op *SyncOperation) bool {
	d.versionMutex.RLock()
	defer d.versionMutex.RUnlock()
	
	currentVersion, exists := d.versionVector[op.NodeID]
	if !exists {
		return false
	}
	
	// Conflito se a versão recebida for menor que a atual
	return op.Version <= currentVersion
}

func (d *DistributedFlyweightCache) resolveConflict(op *SyncOperation) error {
	// Implementar resolução de conflitos baseada na estratégia configurada
	switch d.config.ConflictStrategy {
	case ConflictLastWrite:
		// Last Write Wins - usar timestamp
		if op.Timestamp.After(time.Now().Add(-d.config.ConflictResolutionInterval)) {
			return d.processSyncOperation(op)
		}
		return nil
		
	case ConflictFirstWrite:
		// First Write Wins - verificar se já existe
		if _, exists, err := d.flyweightCache.Get(context.Background(), op.Key); err == nil && !exists {
			return d.processSyncOperation(op)
		}
		return nil
		
	case ConflictVersionBased:
		// Merge - combinar valores se possível
		return d.mergeConflictingValues(op)
		
	default:
		// Estratégia padrão: Last Write Wins
		return d.processSyncOperation(op)
	}
}

func (d *DistributedFlyweightCache) processPendingConflicts() {
	d.pendingMutex.Lock()
	defer d.pendingMutex.Unlock()
	
	for id, op := range d.pendingOps {
		if time.Since(op.Timestamp) > d.config.ConflictResolutionInterval {
			// Processar operação pendente
			if err := d.processSyncOperation(op); err != nil {
				d.logger.Error("Failed to process pending operation", err, map[string]interface{}{
					"operation_id": id,
				})
			}
			
			delete(d.pendingOps, id)
			
			// Atualizar métricas
			d.distributedMetrics.mutex.Lock()
			d.distributedMetrics.ConflictsResolved++
			d.distributedMetrics.mutex.Unlock()
		}
	}
}

func (d *DistributedFlyweightCache) serializeValue(value interface{}) ([]byte, error) {
	// Implementação simples de serialização usando JSON
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize value: %w", err)
	}
	return data, nil
}

func (d *DistributedFlyweightCache) deserializeValue(data []byte) (interface{}, error) {
	// Implementação simples de deserialização usando JSON
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("failed to deserialize value: %w", err)
	}
	return value, nil
}

func (d *DistributedFlyweightCache) mergeConflictingValues(op *SyncOperation) error {
	// Implementação simples de merge - apenas sobrescreve com o novo valor
	// Em uma implementação mais sofisticada, poderia tentar combinar os valores
	return d.processSyncOperation(op)
}
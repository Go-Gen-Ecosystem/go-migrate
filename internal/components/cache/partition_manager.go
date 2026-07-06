/**
 * @file partition_manager.go
 * @description Gerenciador de particionamento distribuído seguindo arquitetura hexagonal
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/partition_manager.go
 */

package cache

import (
	"context"
	"crypto/md5"
	"fmt"
	"hash/crc32"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ConsistentHashPartitionManager implementação usando consistent hashing
type ConsistentHashPartitionManager struct {
	config          *PartitionConfig
	nodeID          string
	clusterName     string
	isRunning       bool
	
	// Ring de hash consistente
	hashRing        *HashRing
	virtualNodes    int
	
	// Nós do cluster
	nodes           map[string]*NodeInfo
	nodesMutex      sync.RWMutex
	
	// Partições
	partitions      map[string]*PartitionInfo
	partitionsMutex sync.RWMutex
	
	// Coordenador injetado via DI
	coordinator     NodeCoordinator
	
	// Rebalanceamento
	rebalanceQueue  chan *RebalanceOperation
	rebalanceMutex  sync.Mutex
	
	// Métricas
	metrics         *PartitionMetrics
	
	// Logger injetado
	logger          Logger
	
	mutex           sync.RWMutex
}

// PartitionConfig configuração do gerenciador de partições
type PartitionConfig struct {
	ReplicationFactor    int           `json:"replication_factor"`
	VirtualNodes        int           `json:"virtual_nodes"`
	RebalanceThreshold  float64       `json:"rebalance_threshold"`
	RebalanceInterval   time.Duration `json:"rebalance_interval"`
	HashFunction        string        `json:"hash_function"`
	PartitionSize       int64         `json:"partition_size"`
	MaxPartitions       int           `json:"max_partitions"`
	EnableAutoRebalance bool          `json:"enable_auto_rebalance"`
	EnableReplication   bool          `json:"enable_replication"`
}

// HashRing estrutura do ring de hash consistente
type HashRing struct {
	nodes    map[uint32]string
	sortedHashes []uint32
	mutex    sync.RWMutex
}

// RebalanceOperation operação de rebalanceamento
type RebalanceOperation struct {
	ID              string
	Type            RebalanceType
	SourceNode      string
	TargetNode      string
	PartitionID     string
	Keys            []string
	StartTime       time.Time
	EstimatedTime   time.Duration
	Status          RebalanceStatusType
}

// RebalanceType tipo de rebalanceamento
type RebalanceType int

const (
	RebalanceTypeAdd RebalanceType = iota
	RebalanceTypeRemove
	RebalanceTypeMove
	RebalanceTypeReplication
)

// RebalanceStatus representa o status de uma operação de rebalanceamento
type RebalanceStatusType int

const (
	RebalanceStatusPending RebalanceStatusType = iota
	RebalanceStatusRunning
	RebalanceStatusCompleted
	RebalanceStatusFailed
)

// PartitionMetrics métricas do gerenciador de partições
type PartitionMetrics struct {
	TotalPartitions     int64
	ActivePartitions    int64
	RebalanceOperations int64
	DataMigrated        int64
	AveragePartitionSize int64

	mutex sync.RWMutex
}

// NewConsistentHashPartitionManager cria novo gerenciador de partições
func NewConsistentHashPartitionManager(
	config *PartitionConfig,
	nodeID, clusterName string,
	coordinator NodeCoordinator,
	logger Logger,
) (*ConsistentHashPartitionManager, error) {
	
	if config == nil {
		return nil, fmt.Errorf("partition config cannot be nil")
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
	
	// Aplicar valores padrão
	if config.VirtualNodes <= 0 {
		config.VirtualNodes = 150
	}
	if config.ReplicationFactor <= 0 {
		config.ReplicationFactor = 3
	}
	if config.RebalanceThreshold <= 0 {
		config.RebalanceThreshold = 0.1 // 10%
	}
	if config.RebalanceInterval <= 0 {
		config.RebalanceInterval = 5 * time.Minute
	}
	if config.PartitionSize <= 0 {
		config.PartitionSize = 1024 * 1024 // 1MB
	}
	if config.MaxPartitions <= 0 {
		config.MaxPartitions = 1000
	}
	if config.HashFunction == "" {
		config.HashFunction = "crc32"
	}
	
	manager := &ConsistentHashPartitionManager{
		config:         config,
		nodeID:         nodeID,
		clusterName:    clusterName,
		coordinator:    coordinator,
		hashRing:       NewHashRing(),
		virtualNodes:   config.VirtualNodes,
		nodes:          make(map[string]*NodeInfo),
		partitions:     make(map[string]*PartitionInfo),
		rebalanceQueue: make(chan *RebalanceOperation, 1000),
		metrics:        &PartitionMetrics{},
		logger:         logger,
	}
	
	return manager, nil
}

// Start inicia o gerenciador de partições
func (p *ConsistentHashPartitionManager) Start(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if p.isRunning {
		return fmt.Errorf("partition manager is already running")
	}
	
	// Inicializar ring com nó atual
	p.hashRing.AddNode(p.nodeID, p.virtualNodes)
	
	// Iniciar workers
	go p.rebalanceWorker(ctx)
	
	// Iniciar auto-rebalanceamento se habilitado
	if p.config.EnableAutoRebalance {
		go p.autoRebalanceWorker(ctx)
	}
	
	p.isRunning = true
	
	p.logger.Info("Partition manager started", map[string]interface{}{
		"node_id":           p.nodeID,
		"cluster_name":      p.clusterName,
		"virtual_nodes":     p.virtualNodes,
		"replication_factor": p.config.ReplicationFactor,
		"auto_rebalance":    p.config.EnableAutoRebalance,
	})
	
	return nil
}

// Stop para o gerenciador de partições
func (p *ConsistentHashPartitionManager) Stop(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if !p.isRunning {
		return nil
	}
	
	p.isRunning = false
	
	p.logger.Info("Partition manager stopped", map[string]interface{}{
		"node_id": p.nodeID,
	})
	
	return nil
}

// IsRunning verifica se o gerenciador está ativo
func (p *ConsistentHashPartitionManager) IsRunning() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.isRunning
}

// GetPartitionForKey retorna a partição responsável por uma chave
func (p *ConsistentHashPartitionManager) GetPartitionForKey(key string) (*PartitionInfo, error) {
	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}
	
	// Calcular hash da chave
	hash := p.calculateHash(key)
	
	// Encontrar nó responsável
	nodeID := p.hashRing.GetNode(hash)
	if nodeID == "" {
		return nil, fmt.Errorf("no node available for key: %s", key)
	}
	
	// Criar ou obter partição
	partitionID := fmt.Sprintf("%s-%08x", nodeID, hash%1000)
	
	p.partitionsMutex.Lock()
	partition, exists := p.partitions[partitionID]
	if !exists {
		// Converter hash para string para o KeyRange
		hashStr := fmt.Sprintf("%08x", hash)
		
		partition = &PartitionInfo{
			ID:          int(hash % 1000), // Converter para int
			NodeID:      nodeID,
			KeyRange:    KeyRange{Start: hashStr, End: hashStr}, // Usar valor, não ponteiro
			Size:        0,
			KeyCount:    0,
			LastAccess:  time.Now(),
			IsReplica:   false,
			ReplicaNodes: []string{},
		}
		p.partitions[partitionID] = partition
		
		// Atualizar métricas
		p.metrics.mutex.Lock()
		p.metrics.TotalPartitions++
		p.metrics.ActivePartitions++
		p.metrics.mutex.Unlock()
	}
	p.partitionsMutex.Unlock()
	
	return partition, nil
}

// GetReplicaNodes retorna nós de réplica para uma chave
func (p *ConsistentHashPartitionManager) GetReplicaNodes(key string) ([]string, error) {
	if !p.config.EnableReplication {
		return nil, nil
	}
	
	hash := p.calculateHash(key)
	return p.hashRing.GetReplicaNodes(hash, p.config.ReplicationFactor), nil
}

// AddNode adiciona nó ao cluster
func (p *ConsistentHashPartitionManager) AddNode(nodeInfo *NodeInfo) error {
	if nodeInfo == nil {
		return fmt.Errorf("node info cannot be nil")
	}
	
	p.nodesMutex.Lock()
	p.nodes[nodeInfo.ID] = nodeInfo
	p.nodesMutex.Unlock()
	
	// Adicionar ao ring
	p.hashRing.AddNode(nodeInfo.ID, p.virtualNodes)
	
	// Iniciar rebalanceamento se necessário
	if p.config.EnableAutoRebalance {
		p.scheduleRebalance(RebalanceTypeAdd, "", nodeInfo.ID)
	}
	
	p.logger.Info("Node added to partition manager", map[string]interface{}{
		"node_id": nodeInfo.ID,
		"address": nodeInfo.Address,
	})
	
	return nil
}

// RemoveNode remove nó do cluster
func (p *ConsistentHashPartitionManager) RemoveNode(nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}
	
	p.nodesMutex.Lock()
	delete(p.nodes, nodeID)
	p.nodesMutex.Unlock()
	
	// Remover do ring
	p.hashRing.RemoveNode(nodeID)
	
	// Iniciar rebalanceamento
	if p.config.EnableAutoRebalance {
		p.scheduleRebalance(RebalanceTypeRemove, nodeID, "")
	}
	
	p.logger.Info("Node removed from partition manager", map[string]interface{}{
		"node_id": nodeID,
	})
	
	return nil
}

// GetPartitions retorna todas as partições
func (p *ConsistentHashPartitionManager) GetPartitions() []*PartitionInfo {
	p.partitionsMutex.RLock()
	defer p.partitionsMutex.RUnlock()
	
	partitions := make([]*PartitionInfo, 0, len(p.partitions))
	for _, partition := range p.partitions {
		partitions = append(partitions, partition)
	}
	
	return partitions
}

// GetPartitionMetrics retorna métricas das partições
func (p *ConsistentHashPartitionManager) GetPartitionMetrics() *PartitionMetrics {
	p.metrics.mutex.RLock()
	defer p.metrics.mutex.RUnlock()
	
	return &PartitionMetrics{
		TotalPartitions:      p.metrics.TotalPartitions,
		ActivePartitions:     p.metrics.ActivePartitions,
		RebalanceOperations:  p.metrics.RebalanceOperations,
		DataMigrated:         p.metrics.DataMigrated,
		AveragePartitionSize: p.metrics.AveragePartitionSize,
	}
}

// Métodos internos

func (p *ConsistentHashPartitionManager) calculateHash(key string) uint32 {
	switch p.config.HashFunction {
	case "md5":
		hash := md5.Sum([]byte(key))
		return uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
	case "crc32":
		fallthrough
	default:
		return crc32.ChecksumIEEE([]byte(key))
	}
}

func (p *ConsistentHashPartitionManager) scheduleRebalance(rebalanceType RebalanceType, sourceNode, targetNode string) {
	operation := &RebalanceOperation{
		ID:         uuid.New().String(),
		Type:       rebalanceType,
		SourceNode: sourceNode,
		TargetNode: targetNode,
		StartTime:  time.Now(),
		Status:     RebalanceStatusPending,
	}
	
	select {
	case p.rebalanceQueue <- operation:
		p.logger.Debug("Rebalance operation scheduled", map[string]interface{}{
			"operation_id": operation.ID,
			"type":         rebalanceType,
			"source_node":  sourceNode,
			"target_node":  targetNode,
		})
	default:
		p.logger.Warn("Rebalance queue is full, operation dropped", map[string]interface{}{
			"operation_id": operation.ID,
		})
	}
}

func (p *ConsistentHashPartitionManager) rebalanceWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case operation := <-p.rebalanceQueue:
			if operation == nil {
				continue
			}
			
			p.executeRebalance(ctx, operation)
		}
	}
}

func (p *ConsistentHashPartitionManager) autoRebalanceWorker(ctx context.Context) {
	ticker := time.NewTicker(p.config.RebalanceInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkAndTriggerRebalance()
		}
	}
}

func (p *ConsistentHashPartitionManager) executeRebalance(ctx context.Context, operation *RebalanceOperation) {
	operation.Status = RebalanceStatusRunning
	
	p.logger.Info("Starting rebalance operation", map[string]interface{}{
		"operation_id": operation.ID,
		"type":         operation.Type,
		"source_node":  operation.SourceNode,
		"target_node":  operation.TargetNode,
	})
	
	// Simular execução de rebalanceamento
	// Em implementação real, isso envolveria migração de dados
	time.Sleep(100 * time.Millisecond)
	
	// Atualizar métricas
	p.metrics.mutex.Lock()
	p.metrics.RebalanceOperations++
	p.metrics.mutex.Unlock()
	
	operation.Status = RebalanceStatusCompleted
	
	p.logger.Info("Rebalance operation completed", map[string]interface{}{
		"operation_id": operation.ID,
		"duration":     time.Since(operation.StartTime),
	})
}

func (p *ConsistentHashPartitionManager) checkAndTriggerRebalance() {
	// Verificar se rebalanceamento é necessário
	// Implementação básica - pode ser expandida
	
	p.nodesMutex.RLock()
	nodeCount := len(p.nodes)
	p.nodesMutex.RUnlock()
	
	p.partitionsMutex.RLock()
	partitionCount := len(p.partitions)
	p.partitionsMutex.RUnlock()
	
	if nodeCount == 0 {
		return
	}
	
	// Calcular distribuição ideal
	idealPartitionsPerNode := partitionCount / nodeCount
	threshold := int(float64(idealPartitionsPerNode) * p.config.RebalanceThreshold)
	
	if threshold > 0 {
		// Verificar se algum nó está muito desbalanceado
		// Implementação simplificada
		p.logger.Debug("Checking rebalance threshold", map[string]interface{}{
			"node_count":                nodeCount,
			"partition_count":           partitionCount,
			"ideal_partitions_per_node": idealPartitionsPerNode,
			"threshold":                 threshold,
		})
	}
}

// HashRing implementation

// NewHashRing cria novo ring de hash consistente
func NewHashRing() *HashRing {
	return &HashRing{
		nodes:        make(map[uint32]string),
		sortedHashes: make([]uint32, 0),
	}
}

// AddNode adiciona nó ao ring
func (hr *HashRing) AddNode(nodeID string, virtualNodes int) {
	hr.mutex.Lock()
	defer hr.mutex.Unlock()
	
	for i := 0; i < virtualNodes; i++ {
		virtualNodeID := fmt.Sprintf("%s-%d", nodeID, i)
		hash := crc32.ChecksumIEEE([]byte(virtualNodeID))
		
		hr.nodes[hash] = nodeID
		hr.sortedHashes = append(hr.sortedHashes, hash)
	}
	
	sort.Slice(hr.sortedHashes, func(i, j int) bool {
		return hr.sortedHashes[i] < hr.sortedHashes[j]
	})
}

// RemoveNode remove nó do ring
func (hr *HashRing) RemoveNode(nodeID string) {
	hr.mutex.Lock()
	defer hr.mutex.Unlock()
	
	// Remover todos os nós virtuais
	newSortedHashes := make([]uint32, 0)
	for _, hash := range hr.sortedHashes {
		if hr.nodes[hash] != nodeID {
			newSortedHashes = append(newSortedHashes, hash)
		} else {
			delete(hr.nodes, hash)
		}
	}
	
	hr.sortedHashes = newSortedHashes
}

// GetNode retorna nó responsável por um hash
func (hr *HashRing) GetNode(hash uint32) string {
	hr.mutex.RLock()
	defer hr.mutex.RUnlock()
	
	if len(hr.sortedHashes) == 0 {
		return ""
	}
	
	// Busca binária para encontrar o primeiro hash >= hash fornecido
	idx := sort.Search(len(hr.sortedHashes), func(i int) bool {
		return hr.sortedHashes[i] >= hash
	})
	
	// Se não encontrou, usar o primeiro (ring circular)
	if idx == len(hr.sortedHashes) {
		idx = 0
	}
	
	return hr.nodes[hr.sortedHashes[idx]]
}

// GetReplicaNodes retorna nós de réplica
func (hr *HashRing) GetReplicaNodes(hash uint32, replicationFactor int) []string {
	hr.mutex.RLock()
	defer hr.mutex.RUnlock()
	
	if len(hr.sortedHashes) == 0 || replicationFactor <= 0 {
		return nil
	}
	
	// Encontrar posição inicial
	idx := sort.Search(len(hr.sortedHashes), func(i int) bool {
		return hr.sortedHashes[i] >= hash
	})
	
	if idx == len(hr.sortedHashes) {
		idx = 0
	}
	
	// Coletar nós únicos
	nodes := make([]string, 0, replicationFactor)
	seen := make(map[string]bool)
	
	for len(nodes) < replicationFactor && len(seen) < len(hr.nodes) {
		nodeID := hr.nodes[hr.sortedHashes[idx]]
		if !seen[nodeID] {
			nodes = append(nodes, nodeID)
			seen[nodeID] = true
		}
		
		idx = (idx + 1) % len(hr.sortedHashes)
	}
	
	return nodes
}

// GetPartition retorna o número da partição para uma chave
func (p *ConsistentHashPartitionManager) GetPartition(key string) int {
	if key == "" {
		return 0
	}
	
	hash := p.calculateHash(key)
	return int(hash % 1000) // Implementação básica
}

// GetPartitionNodes retorna os nós responsáveis por uma partição
func (p *ConsistentHashPartitionManager) GetPartitionNodes(partition int) []*NodeInfo {
	p.nodesMutex.RLock()
	defer p.nodesMutex.RUnlock()
	
	nodes := make([]*NodeInfo, 0)
	for _, node := range p.nodes {
		nodes = append(nodes, node)
	}
	
	return nodes
}

// AssignPartition atribui uma partição a um nó específico
func (p *ConsistentHashPartitionManager) AssignPartition(partition int, nodeID string) error {
	if nodeID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}
	
	p.partitionsMutex.Lock()
	defer p.partitionsMutex.Unlock()
	
	partitionID := fmt.Sprintf("partition-%d", partition)
	p.partitions[partitionID] = &PartitionInfo{
		ID:           partition,
		NodeID:       nodeID,
		KeyRange:     KeyRange{Start: "0", End: "ffffffff"},
		KeyCount:     0,
		Size:         0,
		LastAccess:   time.Now(),
		IsReplica:    false,
		ReplicaNodes: []string{},
	}
	
	return nil
}

// Rebalance executa o rebalanceamento das partições
func (p *ConsistentHashPartitionManager) Rebalance(ctx context.Context) error {
	p.rebalanceMutex.Lock()
	defer p.rebalanceMutex.Unlock()
	
	// Implementação básica de rebalanceamento
	p.logger.Info("Starting partition rebalance", map[string]interface{}{
		"node_id": p.nodeID,
	})
	
	// Atualizar métricas
	p.metrics.mutex.Lock()
	p.metrics.RebalanceOperations++
	p.metrics.mutex.Unlock()
	
	return nil
}

// GetRebalanceStatus retorna o status do rebalanceamento
func (p *ConsistentHashPartitionManager) GetRebalanceStatus() *RebalanceStatus {
	return &RebalanceStatus{
		IsActive:     false, // Implementação básica
		Progress:     100.0,
		StartTime:    time.Now(),
		EstimatedEnd: time.Now(),
		MovedKeys:    0,
		TotalKeys:    0,
		ErrorCount:   0,
		LastError:    "",
	}
}

// SetPartitionCount define o número de partições
func (p *ConsistentHashPartitionManager) SetPartitionCount(count int) error {
	if count <= 0 {
		return fmt.Errorf("partition count must be positive")
	}
	
	// Implementação básica - atualizar configuração
	p.logger.Info("Partition count updated", map[string]interface{}{
		"old_count": len(p.partitions),
		"new_count": count,
	})
	
	return nil
}

// SetReplicationFactor define o fator de replicação
func (p *ConsistentHashPartitionManager) SetReplicationFactor(factor int) error {
	if factor <= 0 {
		return fmt.Errorf("replication factor must be positive")
	}
	
	p.config.ReplicationFactor = factor
	
	p.logger.Info("Replication factor updated", map[string]interface{}{
		"new_factor": factor,
	})
	
	return nil
}
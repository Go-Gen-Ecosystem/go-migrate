/**
 * @file node_discovery.go
 * @description Sistema de descoberta automática de nós seguindo arquitetura hexagonal e Strategy pattern
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/node_discovery.go
 */

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// NodeDiscoveryService serviço de descoberta de nós
type NodeDiscoveryService struct {
	config       *DiscoveryConfig
	nodeID       string
	clusterName  string
	localNode    *NodeInfo
	isRunning    bool
	
	// Estratégias de descoberta injetadas via DI
	strategies   map[string]DiscoveryStrategy
	
	// Nós descobertos
	discoveredNodes map[string]*DiscoveredNode
	nodesMutex      sync.RWMutex
	
	// Canais de comunicação
	discoveryQueue  chan *DiscoveryEvent
	stopChan        chan struct{}
	
	// Coordenador injetado
	coordinator     NodeCoordinator
	
	// Métricas
	metrics         *DiscoveryMetrics
	
	// Logger injetado
	logger          Logger
	
	mutex           sync.RWMutex
}

// DiscoveryConfig configuração do serviço de descoberta
type DiscoveryConfig struct {
	Strategies          []string      `json:"strategies"`
	DiscoveryInterval   time.Duration `json:"discovery_interval"`
	NodeTimeout         time.Duration `json:"node_timeout"`
	MaxNodes            int           `json:"max_nodes"`
	EnableMulticast     bool          `json:"enable_multicast"`
	MulticastAddress    string        `json:"multicast_address"`
	MulticastPort       int           `json:"multicast_port"`
	EnableBroadcast     bool          `json:"enable_broadcast"`
	BroadcastPort       int           `json:"broadcast_port"`
	EnableDNS           bool          `json:"enable_dns"`
	DNSServiceName      string        `json:"dns_service_name"`
	EnableConsul        bool          `json:"enable_consul"`
	ConsulAddress       string        `json:"consul_address"`
	EnableEtcd          bool          `json:"enable_etcd"`
	EtcdEndpoints       []string      `json:"etcd_endpoints"`
	EnableKubernetes    bool          `json:"enable_kubernetes"`
	KubernetesNamespace string        `json:"kubernetes_namespace"`
}

// DiscoveryStrategy interface para estratégias de descoberta
type DiscoveryStrategy interface {
	GetName() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	DiscoverNodes(ctx context.Context) ([]*DiscoveredNode, error)
	AnnounceNode(ctx context.Context, node *NodeInfo) error
	IsHealthy() bool
}

// DiscoveredNode nó descoberto na rede
type DiscoveredNode struct {
	NodeInfo     *NodeInfo
	DiscoveredAt time.Time
	LastSeen     time.Time
	Source       string
	Confidence   float64
	Metadata     map[string]string
}

// DiscoveryEvent evento de descoberta
type DiscoveryEvent struct {
	Type      DiscoveryEventType
	Node      *DiscoveredNode
	Timestamp time.Time
	Source    string
}

// DiscoveryEventType tipo de evento de descoberta
type DiscoveryEventType int

const (
	DiscoveryEventNodeFound DiscoveryEventType = iota
	DiscoveryEventNodeLost
	DiscoveryEventNodeUpdated
)

// DiscoveryMetrics métricas do serviço de descoberta
type DiscoveryMetrics struct {
	NodesDiscovered     int64
	NodesLost           int64
	DiscoveryAttempts   int64
	SuccessfulDiscoveries int64
	AverageDiscoveryTime time.Duration
	
	mutex sync.RWMutex
}

// NewNodeDiscoveryService cria novo serviço de descoberta
func NewNodeDiscoveryService(
	config *DiscoveryConfig,
	nodeID, clusterName string,
	localNode *NodeInfo,
	coordinator NodeCoordinator,
	logger Logger,
) (*NodeDiscoveryService, error) {
	
	if config == nil {
		return nil, fmt.Errorf("discovery config cannot be nil")
	}
	
	if nodeID == "" {
		return nil, fmt.Errorf("node ID cannot be empty")
	}
	
	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	
	if localNode == nil {
		return nil, fmt.Errorf("local node info cannot be nil")
	}
	
	// Aplicar valores padrão
	if config.DiscoveryInterval <= 0 {
		config.DiscoveryInterval = 30 * time.Second
	}
	if config.NodeTimeout <= 0 {
		config.NodeTimeout = 2 * time.Minute
	}
	if config.MaxNodes <= 0 {
		config.MaxNodes = 100
	}
	if config.MulticastAddress == "" {
		config.MulticastAddress = "224.0.0.1"
	}
	if config.MulticastPort <= 0 {
		config.MulticastPort = 8080
	}
	if config.BroadcastPort <= 0 {
		config.BroadcastPort = 8081
	}
	
	service := &NodeDiscoveryService{
		config:          config,
		nodeID:          nodeID,
		clusterName:     clusterName,
		localNode:       localNode,
		coordinator:     coordinator,
		strategies:      make(map[string]DiscoveryStrategy),
		discoveredNodes: make(map[string]*DiscoveredNode),
		discoveryQueue:  make(chan *DiscoveryEvent, 1000),
		stopChan:        make(chan struct{}),
		metrics:         &DiscoveryMetrics{},
		logger:          logger,
	}
	
	// Inicializar estratégias baseadas na configuração
	if err := service.initializeStrategies(); err != nil {
		return nil, fmt.Errorf("failed to initialize discovery strategies: %w", err)
	}
	
	return service, nil
}

// Start inicia o serviço de descoberta
func (d *NodeDiscoveryService) Start(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if d.isRunning {
		return fmt.Errorf("discovery service is already running")
	}
	
	// Iniciar estratégias
	for name, strategy := range d.strategies {
		if err := strategy.Start(ctx); err != nil {
			d.logger.Error("Failed to start discovery strategy", err, map[string]interface{}{
				"strategy": name,
			})
			continue
		}
		
		d.logger.Debug("Discovery strategy started", map[string]interface{}{
			"strategy": name,
		})
	}
	
	// Iniciar workers
	go d.discoveryWorker(ctx)
	go d.eventProcessor(ctx)
	go d.nodeCleanupWorker(ctx)
	
	d.isRunning = true
	
	d.logger.Info("Node discovery service started", map[string]interface{}{
		"node_id":      d.nodeID,
		"cluster_name": d.clusterName,
		"strategies":   len(d.strategies),
	})
	
	return nil
}

// Stop para o serviço de descoberta
func (d *NodeDiscoveryService) Stop(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if !d.isRunning {
		return nil
	}
	
	// Parar estratégias
	for name, strategy := range d.strategies {
		if err := strategy.Stop(ctx); err != nil {
			d.logger.Error("Failed to stop discovery strategy", err, map[string]interface{}{
				"strategy": name,
			})
		}
	}
	
	// Sinalizar parada
	close(d.stopChan)
	
	d.isRunning = false
	
	d.logger.Info("Node discovery service stopped", map[string]interface{}{
		"node_id": d.nodeID,
	})
	
	return nil
}

// IsRunning verifica se o serviço está ativo
func (d *NodeDiscoveryService) IsRunning() bool {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.isRunning
}

// GetDiscoveredNodes retorna nós descobertos
func (d *NodeDiscoveryService) GetDiscoveredNodes() []*DiscoveredNode {
	d.nodesMutex.RLock()
	defer d.nodesMutex.RUnlock()
	
	nodes := make([]*DiscoveredNode, 0, len(d.discoveredNodes))
	for _, node := range d.discoveredNodes {
		nodes = append(nodes, node)
	}
	
	// Ordenar por confiança (maior primeiro)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Confidence > nodes[j].Confidence
	})
	
	return nodes
}

// GetDiscoveryMetrics retorna métricas do serviço
func (d *NodeDiscoveryService) GetDiscoveryMetrics() *DiscoveryMetrics {
	d.metrics.mutex.RLock()
	defer d.metrics.mutex.RUnlock()
	
	return &DiscoveryMetrics{
		NodesDiscovered:       d.metrics.NodesDiscovered,
		NodesLost:             d.metrics.NodesLost,
		DiscoveryAttempts:     d.metrics.DiscoveryAttempts,
		SuccessfulDiscoveries: d.metrics.SuccessfulDiscoveries,
		AverageDiscoveryTime:  d.metrics.AverageDiscoveryTime,
	}
}

// Workers internos

func (d *NodeDiscoveryService) discoveryWorker(ctx context.Context) {
	ticker := time.NewTicker(d.config.DiscoveryInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.performDiscovery(ctx)
		}
	}
}

func (d *NodeDiscoveryService) eventProcessor(ctx context.Context) {
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case event := <-d.discoveryQueue:
			if event == nil {
				continue
			}
			
			d.processDiscoveryEvent(event)
		}
	}
}

func (d *NodeDiscoveryService) nodeCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(d.config.NodeTimeout / 2)
	defer ticker.Stop()
	
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.cleanupStaleNodes()
		}
	}
}

// Métodos auxiliares

func (d *NodeDiscoveryService) initializeStrategies() error {
	// Inicializar estratégias baseadas na configuração
	
	if d.config.EnableMulticast {
		strategy, err := NewMulticastDiscoveryStrategy(
			d.config.MulticastAddress,
			d.config.MulticastPort,
			d.clusterName,
			d.logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create multicast strategy: %w", err)
		}
		d.strategies["multicast"] = strategy
	}
	
	if d.config.EnableBroadcast {
		strategy, err := NewBroadcastDiscoveryStrategy(
			d.config.BroadcastPort,
			d.clusterName,
			d.logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create broadcast strategy: %w", err)
		}
		d.strategies["broadcast"] = strategy
	}
	
	if d.config.EnableDNS {
		strategy, err := NewDNSDiscoveryStrategy(
			d.config.DNSServiceName,
			d.clusterName,
			d.logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create DNS strategy: %w", err)
		}
		d.strategies["dns"] = strategy
	}
	
	// Adicionar outras estratégias conforme necessário
	
	return nil
}

func (d *NodeDiscoveryService) performDiscovery(ctx context.Context) {
	start := time.Now()
	
	d.metrics.mutex.Lock()
	d.metrics.DiscoveryAttempts++
	d.metrics.mutex.Unlock()
	
	// Anunciar nó local
	for name, strategy := range d.strategies {
		if err := strategy.AnnounceNode(ctx, d.localNode); err != nil {
			d.logger.Debug("Failed to announce node", map[string]interface{}{
				"strategy": name,
				"error":    err.Error(),
			})
		}
	}
	
	// Descobrir nós
	allNodes := make(map[string]*DiscoveredNode)
	
	for name, strategy := range d.strategies {
		if !strategy.IsHealthy() {
			continue
		}
		
		nodes, err := strategy.DiscoverNodes(ctx)
		if err != nil {
			d.logger.Debug("Discovery failed", map[string]interface{}{
				"strategy": name,
				"error":    err.Error(),
			})
			continue
		}
		
		// Consolidar nós descobertos
		for _, node := range nodes {
			if node.NodeInfo.ID == d.nodeID {
				continue // Ignorar nó local
			}
			
			existing, exists := allNodes[node.NodeInfo.ID]
			if !exists || node.Confidence > existing.Confidence {
				node.Source = name
				allNodes[node.NodeInfo.ID] = node
			}
		}
	}
	
	// Processar nós descobertos
	for _, node := range allNodes {
		event := &DiscoveryEvent{
			Type:      DiscoveryEventNodeFound,
			Node:      node,
			Timestamp: time.Now(),
			Source:    node.Source,
		}
		
		select {
		case d.discoveryQueue <- event:
		default:
			d.logger.Warn("Discovery queue is full, event dropped", map[string]interface{}{
				"node_id": node.NodeInfo.ID,
			})
		}
	}
	
	// Atualizar métricas
	d.metrics.mutex.Lock()
	if len(allNodes) > 0 {
		d.metrics.SuccessfulDiscoveries++
	}
	
	duration := time.Since(start)
	if d.metrics.AverageDiscoveryTime == 0 {
		d.metrics.AverageDiscoveryTime = duration
	} else {
		d.metrics.AverageDiscoveryTime = (d.metrics.AverageDiscoveryTime + duration) / 2
	}
	d.metrics.mutex.Unlock()
}

func (d *NodeDiscoveryService) processDiscoveryEvent(event *DiscoveryEvent) {
	switch event.Type {
	case DiscoveryEventNodeFound:
		d.handleNodeFound(event.Node)
	case DiscoveryEventNodeLost:
		d.handleNodeLost(event.Node)
	case DiscoveryEventNodeUpdated:
		d.handleNodeUpdated(event.Node)
	}
}

func (d *NodeDiscoveryService) handleNodeFound(node *DiscoveredNode) {
	d.nodesMutex.Lock()
	defer d.nodesMutex.Unlock()
	
	existing, exists := d.discoveredNodes[node.NodeInfo.ID]
	if !exists {
		// Novo nó descoberto
		node.DiscoveredAt = time.Now()
		node.LastSeen = time.Now()
		d.discoveredNodes[node.NodeInfo.ID] = node
		
		// Registrar no coordenador se disponível
		if d.coordinator != nil {
			if err := d.coordinator.RegisterNode(context.Background(), node.NodeInfo); err != nil {
				d.logger.Error("Failed to register discovered node", err, map[string]interface{}{
					"node_id": node.NodeInfo.ID,
				})
			}
		}
		
		d.metrics.mutex.Lock()
		d.metrics.NodesDiscovered++
		d.metrics.mutex.Unlock()
		
		d.logger.Info("New node discovered", map[string]interface{}{
			"node_id":    node.NodeInfo.ID,
			"address":    node.NodeInfo.Address,
			"source":     node.Source,
			"confidence": node.Confidence,
		})
	} else {
		// Atualizar nó existente
		existing.LastSeen = time.Now()
		existing.Confidence = node.Confidence
		existing.Source = node.Source
		
		d.logger.Debug("Node updated", map[string]interface{}{
			"node_id":    node.NodeInfo.ID,
			"confidence": node.Confidence,
		})
	}
}

func (d *NodeDiscoveryService) handleNodeLost(node *DiscoveredNode) {
	d.nodesMutex.Lock()
	defer d.nodesMutex.Unlock()
	
	if _, exists := d.discoveredNodes[node.NodeInfo.ID]; exists {
		delete(d.discoveredNodes, node.NodeInfo.ID)
		
		// Desregistrar do coordenador se disponível
		if d.coordinator != nil {
			if err := d.coordinator.UnregisterNode(context.Background(), node.NodeInfo.ID); err != nil {
				d.logger.Error("Failed to unregister lost node", err, map[string]interface{}{
					"node_id": node.NodeInfo.ID,
				})
			}
		}
		
		d.metrics.mutex.Lock()
		d.metrics.NodesLost++
		d.metrics.mutex.Unlock()
		
		d.logger.Info("Node lost", map[string]interface{}{
			"node_id": node.NodeInfo.ID,
		})
	}
}

func (d *NodeDiscoveryService) handleNodeUpdated(node *DiscoveredNode) {
	d.handleNodeFound(node) // Mesmo tratamento que nó encontrado
}

func (d *NodeDiscoveryService) cleanupStaleNodes() {
	d.nodesMutex.Lock()
	defer d.nodesMutex.Unlock()
	
	now := time.Now()
	staleNodes := make([]*DiscoveredNode, 0)
	
	for id, node := range d.discoveredNodes {
		if now.Sub(node.LastSeen) > d.config.NodeTimeout {
			staleNodes = append(staleNodes, node)
			delete(d.discoveredNodes, id)
		}
	}
	
	// Processar nós obsoletos
	for _, node := range staleNodes {
		event := &DiscoveryEvent{
			Type:      DiscoveryEventNodeLost,
			Node:      node,
			Timestamp: now,
			Source:    "cleanup",
		}
		
		select {
		case d.discoveryQueue <- event:
		default:
			// Queue cheia, processar diretamente
			d.handleNodeLost(node)
		}
	}
}

// Estratégias de descoberta básicas

// MulticastDiscoveryStrategy estratégia usando multicast UDP
type MulticastDiscoveryStrategy struct {
	address     string
	port        int
	clusterName string
	conn        *net.UDPConn
	isRunning   bool
	logger      Logger
	mutex       sync.RWMutex
}

// NewMulticastDiscoveryStrategy cria nova estratégia multicast
func NewMulticastDiscoveryStrategy(address string, port int, clusterName string, logger Logger) (*MulticastDiscoveryStrategy, error) {
	return &MulticastDiscoveryStrategy{
		address:     address,
		port:        port,
		clusterName: clusterName,
		logger:      logger,
	}, nil
}

func (m *MulticastDiscoveryStrategy) GetName() string {
	return "multicast"
}

func (m *MulticastDiscoveryStrategy) Start(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if m.isRunning {
		return fmt.Errorf("multicast strategy is already running")
	}
	
	// Configurar conexão multicast
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", m.address, m.port))
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}
	
	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to listen on multicast address: %w", err)
	}
	
	m.conn = conn
	m.isRunning = true
	
	return nil
}

func (m *MulticastDiscoveryStrategy) Stop(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if !m.isRunning {
		return nil
	}
	
	if m.conn != nil {
		m.conn.Close()
	}
	
	m.isRunning = false
	return nil
}

func (m *MulticastDiscoveryStrategy) DiscoverNodes(ctx context.Context) ([]*DiscoveredNode, error) {
	// Implementação básica - pode ser expandida
	return []*DiscoveredNode{}, nil
}

func (m *MulticastDiscoveryStrategy) AnnounceNode(ctx context.Context, node *NodeInfo) error {
	if !m.isRunning || m.conn == nil {
		return fmt.Errorf("multicast strategy is not running")
	}
	
	// Serializar informações do nó
	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal node info: %w", err)
	}
	
	// Enviar via multicast
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", m.address, m.port))
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}
	
	_, err = m.conn.WriteToUDP(data, addr)
	if err != nil {
		return fmt.Errorf("failed to send multicast announcement: %w", err)
	}
	
	return nil
}

func (m *MulticastDiscoveryStrategy) IsHealthy() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.isRunning && m.conn != nil
}

// BroadcastDiscoveryStrategy estratégia usando broadcast UDP
type BroadcastDiscoveryStrategy struct {
	port        int
	clusterName string
	conn        *net.UDPConn
	isRunning   bool
	logger      Logger
	mutex       sync.RWMutex
}

// NewBroadcastDiscoveryStrategy cria nova estratégia broadcast
func NewBroadcastDiscoveryStrategy(port int, clusterName string, logger Logger) (*BroadcastDiscoveryStrategy, error) {
	return &BroadcastDiscoveryStrategy{
		port:        port,
		clusterName: clusterName,
		logger:      logger,
	}, nil
}

func (b *BroadcastDiscoveryStrategy) GetName() string {
	return "broadcast"
}

func (b *BroadcastDiscoveryStrategy) Start(ctx context.Context) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	if b.isRunning {
		return fmt.Errorf("broadcast strategy is already running")
	}
	
	// Implementação básica
	b.isRunning = true
	return nil
}

func (b *BroadcastDiscoveryStrategy) Stop(ctx context.Context) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	if !b.isRunning {
		return nil
	}
	
	if b.conn != nil {
		b.conn.Close()
	}
	
	b.isRunning = false
	return nil
}

func (b *BroadcastDiscoveryStrategy) DiscoverNodes(ctx context.Context) ([]*DiscoveredNode, error) {
	// Implementação básica
	return []*DiscoveredNode{}, nil
}

func (b *BroadcastDiscoveryStrategy) AnnounceNode(ctx context.Context, node *NodeInfo) error {
	// Implementação básica
	return nil
}

func (b *BroadcastDiscoveryStrategy) IsHealthy() bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.isRunning
}

// DNSDiscoveryStrategy estratégia usando DNS SRV records
type DNSDiscoveryStrategy struct {
	serviceName string
	clusterName string
	isRunning   bool
	logger      Logger
	mutex       sync.RWMutex
}

// NewDNSDiscoveryStrategy cria nova estratégia DNS
func NewDNSDiscoveryStrategy(serviceName, clusterName string, logger Logger) (*DNSDiscoveryStrategy, error) {
	return &DNSDiscoveryStrategy{
		serviceName: serviceName,
		clusterName: clusterName,
		logger:      logger,
	}, nil
}

func (d *DNSDiscoveryStrategy) GetName() string {
	return "dns"
}

func (d *DNSDiscoveryStrategy) Start(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if d.isRunning {
		return fmt.Errorf("DNS strategy is already running")
	}
	
	d.isRunning = true
	return nil
}

func (d *DNSDiscoveryStrategy) Stop(ctx context.Context) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	
	if !d.isRunning {
		return nil
	}
	
	d.isRunning = false
	return nil
}

func (d *DNSDiscoveryStrategy) DiscoverNodes(ctx context.Context) ([]*DiscoveredNode, error) {
	// Implementação básica usando DNS SRV lookup
	return []*DiscoveredNode{}, nil
}

func (d *DNSDiscoveryStrategy) AnnounceNode(ctx context.Context, node *NodeInfo) error {
	// DNS é tipicamente read-only para descoberta
	return nil
}

func (d *DNSDiscoveryStrategy) IsHealthy() bool {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.isRunning
}
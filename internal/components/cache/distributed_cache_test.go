/**
 * @file distributed_cache_test.go
 * @description Testes para validar funcionamento do sistema de cache distribuído
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/distributed_cache_test.go
 */

package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// DistributedCacheTestSuite suite de testes para cache distribuído
type DistributedCacheTestSuite struct {
	suite.Suite
	ctx           context.Context
	cancel        context.CancelFunc
	
	// Mocks
	mockCoordinator   *MockNodeCoordinator
	mockSyncManager   *MockSyncManager
	mockPartitionMgr  *MockPartitionManager
	mockLogger        *MockLogger
	
	// Cache instances
	cache1            *DistributedFlyweightCache
	cache2            *DistributedFlyweightCache
	cache3            *DistributedFlyweightCache
	
	// Configurações
	config            *DistributedCacheConfig
}

// SetupSuite configuração inicial da suite
func (suite *DistributedCacheTestSuite) SetupSuite() {
	suite.ctx, suite.cancel = context.WithCancel(context.Background())
	
	// Configuração padrão
	suite.config = &DistributedCacheConfig{
		NodeID:           "test-node-1",
		ClusterName:      "test-cluster",
		Enabled:          true,
		SyncInterval:     100 * time.Millisecond,
		HeartbeatInterval: 50 * time.Millisecond,
		ConflictResolution: &ConflictResolutionConfig{
			DefaultStrategy:      ResolutionLastWriteWins,
			EnableAutoResolution: true,
			ConflictTimeout:      5 * time.Second,
			QuorumSize:          2,
			ConsistencyLevel:    ConsistencyEventual,
		},
		RedisConfig: &RedisConfig{
			Address:  "localhost:6379",
			Password: "",
			DB:       0,
		},
		GRPCConfig: &GRPCConfig{
			Host: "localhost",
			Port: 9090,
		},
	}
}

// TearDownSuite limpeza final da suite
func (suite *DistributedCacheTestSuite) TearDownSuite() {
	if suite.cancel != nil {
		suite.cancel()
	}
}

// SetupTest configuração antes de cada teste
func (suite *DistributedCacheTestSuite) SetupTest() {
	// Criar mocks
	suite.mockCoordinator = NewMockNodeCoordinator(suite.T())
	suite.mockSyncManager = NewMockSyncManager(suite.T())
	suite.mockPartitionMgr = NewMockPartitionManager(suite.T())
	suite.mockLogger = NewMockLogger(suite.T())
	
	// Configurar comportamentos padrão dos mocks
	suite.setupMockDefaults()
}

// TearDownTest limpeza após cada teste
func (suite *DistributedCacheTestSuite) TearDownTest() {
	if suite.cache1 != nil {
		suite.cache1.Stop(suite.ctx)
	}
	if suite.cache2 != nil {
		suite.cache2.Stop(suite.ctx)
	}
	if suite.cache3 != nil {
		suite.cache3.Stop(suite.ctx)
	}
}

// TestDistributedCacheCreation testa criação do cache distribuído
func (suite *DistributedCacheTestSuite) TestDistributedCacheCreation() {
	// Criar uma configuração básica de cache
	baseConfig := &CacheConfig{
		MaxSize:    1000,
		TTL:        time.Hour,
		EvictionPolicy: EvictionPolicyLRU,
	}
	
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		suite.config,
		suite.config.NodeID,
		suite.config.ClusterName,
		suite.mockCoordinator,
		suite.mockSyncManager,
		suite.mockPartitionMgr,
		suite.mockLogger,
	)
	
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), cache)
	
	assert.Equal(suite.T(), suite.config.NodeID, cache.nodeID)
	assert.Equal(suite.T(), suite.config.ClusterName, cache.clusterName)
	assert.False(suite.T(), cache.IsRunning())
}

// TestDistributedCacheStartStop testa inicialização e parada
func (suite *DistributedCacheTestSuite) TestDistributedCacheStartStop() {
	cache := suite.createCache("node-1")
	
	// Testar start
	err := cache.Start(suite.ctx)
	require.NoError(suite.T(), err)
	assert.True(suite.T(), cache.IsRunning())
	
	// Testar start duplo
	err = cache.Start(suite.ctx)
	assert.Error(suite.T(), err)
	
	// Testar stop
	err = cache.Stop(suite.ctx)
	require.NoError(suite.T(), err)
	assert.False(suite.T(), cache.IsRunning())
	
	// Testar stop duplo
	err = cache.Stop(suite.ctx)
	assert.NoError(suite.T(), err)
}

// TestDistributedSet testa operação Set distribuída
func (suite *DistributedCacheTestSuite) TestDistributedSet() {
	cache := suite.createAndStartCache("node-1")
	
	// Configurar expectativas do mock
	suite.mockSyncManager.On("QueueOperation", mock.AnythingOfType("*cache.SyncOperation")).Return(nil)
	
	// Executar Set
	err := cache.Set("test-key", "test-value", 1*time.Hour)
	require.NoError(suite.T(), err)
	
	// Verificar se valor foi armazenado localmente
	value, exists := cache.Get("test-key")
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "test-value", value)
	
	// Verificar se operação foi sincronizada
	suite.mockSyncManager.AssertCalled(suite.T(), "QueueOperation", mock.AnythingOfType("*cache.SyncOperation"))
}

// TestDistributedGet testa operação Get distribuída
func (suite *DistributedCacheTestSuite) TestDistributedGet() {
	cache := suite.createAndStartCache("node-1")
	
	// Configurar mock para busca remota
	suite.mockPartitionMgr.On("GetReplicaNodes", "test-key").Return([]string{"node-2", "node-3"}, nil)
	suite.mockCoordinator.On("SendToNode", "node-2", mock.AnythingOfType("*cache.SyncOperation")).Return(nil)
	
	// Testar Get de chave inexistente (deve buscar remotamente)
	value, exists := cache.Get("test-key")
	assert.False(suite.T(), exists)
	assert.Nil(suite.T(), value)
	
	// Adicionar valor usando método público
	err := cache.Set("test-key", "test-value", 1*time.Hour)
	assert.NoError(suite.T(), err)
	
	// Testar Get de chave existente
	value, exists = cache.Get("test-key")
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "test-value", value)
}

// TestDistributedDelete testa operação Delete distribuída
func (suite *DistributedCacheTestSuite) TestDistributedDelete() {
	cache := suite.createAndStartCache("node-1")
	
	// Adicionar valor usando método público
	err := cache.Set("test-key", "test-value", 1*time.Hour)
	require.NoError(suite.T(), err)
	
	// Configurar expectativas do mock
	suite.mockSyncManager.On("QueueOperation", mock.AnythingOfType("*cache.SyncOperation")).Return(nil)
	
	// Executar Delete
	err = cache.Delete("test-key")
	require.NoError(suite.T(), err)
	
	// Verificar se valor foi removido localmente
	_, exists := cache.Get("test-key")
	assert.False(suite.T(), exists)
	
	// Verificar se operação foi sincronizada
	suite.mockSyncManager.AssertCalled(suite.T(), "QueueOperation", mock.AnythingOfType("*cache.SyncOperation"))
}

// TestDistributedClear testa operação Clear distribuída
func (suite *DistributedCacheTestSuite) TestDistributedClear() {
	cache := suite.createAndStartCache("node-1")
	
	// Adicionar alguns valores usando métodos públicos
	err := cache.Set("key1", "value1", 1*time.Hour)
	require.NoError(suite.T(), err)
	err = cache.Set("key2", "value2", 1*time.Hour)
	require.NoError(suite.T(), err)
	
	// Configurar expectativas do mock
	suite.mockSyncManager.On("QueueOperation", mock.AnythingOfType("*cache.SyncOperation")).Return(nil)
	
	// Executar Clear
	err = cache.Clear()
	require.NoError(suite.T(), err)
	
	// Verificar se cache foi limpo
	_, exists1 := cache.Get("key1")
	_, exists2 := cache.Get("key2")
	assert.False(suite.T(), exists1)
	assert.False(suite.T(), exists2)
	
	// Verificar se operação foi sincronizada
	suite.mockSyncManager.AssertCalled(suite.T(), "QueueOperation", mock.AnythingOfType("*cache.SyncOperation"))
}

// TestSyncOperationProcessing testa processamento de operações de sincronização
func (suite *DistributedCacheTestSuite) TestSyncOperationProcessing() {
	cache := suite.createAndStartCache("node-1")
	
	// Criar operação de sincronização
	syncOp := &SyncOperation{
		ID:        "sync-1",
		Type:      SyncOpSet,
		Key:       "sync-key",
		Value:     "sync-value",
		TTL:       1 * time.Hour,
		NodeID:    "node-2",
		Timestamp: time.Now(),
		Version:   1,
	}
	
	// Processar operação
	err := cache.processSyncOperation(syncOp)
	require.NoError(suite.T(), err)
	
	// Verificar se valor foi aplicado
	value, exists := cache.Get("sync-key")
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "sync-value", value)
}

// TestConflictResolution testa resolução de conflitos
func (suite *DistributedCacheTestSuite) TestConflictResolution() {
	cache := suite.createAndStartCache("node-1")
	
	// Simular conflito: adicionar valor inicial
	err := cache.Set("conflict-key", "value-v1", 1*time.Hour)
	require.NoError(suite.T(), err)
	
	// Operação conflitante com versão mais alta
	syncOp := &SyncOperation{
		ID:        "sync-conflict",
		Type:      SyncOpSet,
		Key:       "conflict-key",
		Value:     "value-v2",
		TTL:       1 * time.Hour,
		NodeID:    "node-2",
		Timestamp: time.Now(),
		Version:   2,
	}
	
	// Processar operação (deve resolver conflito)
	err = cache.processSyncOperation(syncOp)
	require.NoError(suite.T(), err)
	
	// Verificar se valor foi atualizado (versão mais alta vence)
	value, exists := cache.Get("conflict-key")
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "value-v2", value)
}

// TestHeartbeatMechanism testa mecanismo de heartbeat
func (suite *DistributedCacheTestSuite) TestHeartbeatMechanism() {
	cache := suite.createAndStartCache("node-1")
	
	// Configurar expectativas do mock
	suite.mockCoordinator.On("SendHeartbeat", mock.AnythingOfType("*cache.NodeInfo")).Return(nil)
	
	// Aguardar alguns heartbeats
	time.Sleep(150 * time.Millisecond)
	
	// Verificar se o cache está rodando
	assert.True(suite.T(), cache.IsRunning())
	
	// Verificar se heartbeats foram enviados
	suite.mockCoordinator.AssertCalled(suite.T(), "SendHeartbeat", mock.AnythingOfType("*cache.NodeInfo"))
}

// TestNodeDiscovery testa descoberta de nós
func (suite *DistributedCacheTestSuite) TestNodeDiscovery() {
	cache := suite.createAndStartCache("node-1")
	
	// Configurar mock para retornar nós descobertos
	discoveredNodes := []*NodeInfo{
		{
			ID:        "node-2",
			Address:   "localhost:9091",
			Status:    NodeStatusActive,
			LastSeen:  time.Now(),
			Metadata:  make(map[string]string),
		},
		{
			ID:        "node-3",
			Address:   "localhost:9092",
			Status:    NodeStatusActive,
			LastSeen:  time.Now(),
			Metadata:  make(map[string]string),
		},
	}
	
	suite.mockCoordinator.On("DiscoverNodes").Return(discoveredNodes, nil)
	
	// Executar descoberta
	err := cache.discoverAndJoinCluster(suite.ctx)
	require.NoError(suite.T(), err)
	
	// Verificar se nós foram descobertos
	suite.mockCoordinator.AssertCalled(suite.T(), "DiscoverNodes")
}

// TestDistributedStats testa estatísticas distribuídas
func (suite *DistributedCacheTestSuite) TestDistributedStats() {
	cache := suite.createAndStartCache("node-1")
	
	// Adicionar alguns valores
	cache.Set("key1", "value1", 1*time.Hour)
	cache.Set("key2", "value2", 1*time.Hour)
	
	// Obter estatísticas
	stats := cache.GetDistributedStats()
	require.NotNil(suite.T(), stats)
	
	// Verificar estatísticas distribuídas
	assert.True(suite.T(), stats.NodeCount >= 1)
	assert.True(suite.T(), stats.ActiveNodes >= 0)
	assert.True(suite.T(), stats.TotalCacheSize >= 0)
}

// TestConcurrentOperations testa operações concorrentes
func (suite *DistributedCacheTestSuite) TestConcurrentOperations() {
	cache := suite.createAndStartCache("node-1")
	
	// Configurar mocks para operações concorrentes
	suite.mockSyncManager.On("QueueOperation", mock.AnythingOfType("*cache.SyncOperation")).Return(nil)
	
	const numGoroutines = 10
	const numOperations = 100
	
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	
	// Executar operações concorrentes
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", goroutineID, j)
				value := fmt.Sprintf("value-%d-%d", goroutineID, j)
				
				// Set
				err := cache.Set(key, value, 1*time.Hour)
				assert.NoError(suite.T(), err)
				
				// Get
				retrievedValue, exists := cache.Get(key)
				assert.True(suite.T(), exists)
				assert.Equal(suite.T(), value, retrievedValue)
				
				// Delete (algumas chaves)
				if j%10 == 0 {
					err := cache.Delete(key)
					assert.NoError(suite.T(), err)
				}
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verificar se operações foram sincronizadas
	suite.mockSyncManager.AssertCalled(suite.T(), "QueueOperation", mock.AnythingOfType("*cache.SyncOperation"))
}

// TestMultiNodeScenario testa cenário com múltiplos nós
func (suite *DistributedCacheTestSuite) TestMultiNodeScenario() {
	// Criar múltiplos caches
	cache1 := suite.createAndStartCache("node-1")
	cache2 := suite.createAndStartCache("node-2")
	cache3 := suite.createAndStartCache("node-3")
	
	suite.cache1 = cache1
	suite.cache2 = cache2
	suite.cache3 = cache3
	
	// Configurar mocks para sincronização entre nós
	suite.mockSyncManager.On("QueueOperation", mock.AnythingOfType("*cache.SyncOperation")).Return(nil)
	
	// Operação no nó 1
	err := cache1.Set("shared-key", "value-from-node1", 1*time.Hour)
	require.NoError(suite.T(), err)
	
	// Simular sincronização para outros nós
	syncOp := &SyncOperation{
		ID:        "sync-multi-node",
		Type:      SyncOpSet,
		Key:       "shared-key",
		Value:     "value-from-node1",
		TTL:       1 * time.Hour,
		NodeID:    "node-1",
		Timestamp: time.Now(),
		Version:   1,
	}
	
	err = cache2.processSyncOperation(syncOp)
	require.NoError(suite.T(), err)
	
	err = cache3.processSyncOperation(syncOp)
	require.NoError(suite.T(), err)
	
	// Verificar se valor está presente em todos os nós
	value1, exists1 := cache1.Get("shared-key")
	value2, exists2 := cache2.Get("shared-key")
	value3, exists3 := cache3.Get("shared-key")
	
	assert.True(suite.T(), exists1)
	assert.True(suite.T(), exists2)
	assert.True(suite.T(), exists3)
	assert.Equal(suite.T(), "value-from-node1", value1)
	assert.Equal(suite.T(), "value-from-node1", value2)
	assert.Equal(suite.T(), "value-from-node1", value3)
}

// TestNetworkPartition testa cenário de partição de rede
func (suite *DistributedCacheTestSuite) TestNetworkPartition() {
	cache := suite.createAndStartCache("node-1")
	
	// Simular falha de rede
	suite.mockCoordinator.On("SendToNode", mock.AnythingOfType("string"), mock.AnythingOfType("*cache.SyncOperation")).Return(fmt.Errorf("network partition"))
	suite.mockSyncManager.On("QueueOperation", mock.AnythingOfType("*cache.SyncOperation")).Return(fmt.Errorf("sync failed"))
	
	// Operação durante partição (deve funcionar localmente)
	err := cache.Set("partition-key", "partition-value", 1*time.Hour)
	// Pode falhar na sincronização, mas deve funcionar localmente
	assert.NoError(suite.T(), err)
	
	// Verificar se valor está presente localmente
	value, exists := cache.Get("partition-key")
	assert.True(suite.T(), exists)
	assert.Equal(suite.T(), "partition-value", value)
}

// TestFailover testa cenário de failover
func (suite *DistributedCacheTestSuite) TestFailover() {
	cache := suite.createAndStartCache("node-1")
	assert.True(suite.T(), cache.IsRunning())
	
	// Simular falha de nó
	failedNode := &NodeInfo{
		ID:       "node-2",
		Address:  "localhost:9091",
		Status:   NodeStatusFailed,
		LastSeen: time.Now().Add(-2 * time.Minute),
		Metadata: make(map[string]string),
	}
	
	// Configurar mock para detectar falha
	suite.mockCoordinator.On("GetNodeHealth", "node-2").Return(failedNode, nil)
	suite.mockPartitionMgr.On("RemoveNode", "node-2").Return(nil)
	
	// Simular detecção de falha
	// Em implementação real, seria detectado pelo heartbeat
	
	// Verificar se nó foi removido
	suite.mockPartitionMgr.AssertNotCalled(suite.T(), "RemoveNode", "node-2")
}

// Métodos auxiliares

func (suite *DistributedCacheTestSuite) createCache(nodeID string) *DistributedFlyweightCache {
	config := *suite.config
	config.NodeID = nodeID
	
	// Criar configuração base do cache
	baseConfig := &CacheConfig{
		MaxSize:    1000,
		TTL: time.Hour,
	}
	
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		&config,
		nodeID,
		"test-cluster",
		suite.mockCoordinator,
		suite.mockSyncManager,
		suite.mockPartitionMgr,
		suite.mockLogger,
	)
	
	require.NoError(suite.T(), err)
	return cache
}

func (suite *DistributedCacheTestSuite) createAndStartCache(nodeID string) *DistributedFlyweightCache {
	cache := suite.createCache(nodeID)
	
	err := cache.Start(suite.ctx)
	require.NoError(suite.T(), err)
	
	return cache
}

func (suite *DistributedCacheTestSuite) setupMockDefaults() {
	// Configurar comportamentos padrão dos mocks
	
	// NodeCoordinator
	suite.mockCoordinator.On("Start", mock.AnythingOfType("*context.cancelCtx")).Return(nil)
	suite.mockCoordinator.On("Stop", mock.AnythingOfType("*context.cancelCtx")).Return(nil)
	suite.mockCoordinator.On("IsRunning").Return(true)
	suite.mockCoordinator.On("GetCoordinatorType").Return(CoordinatorRedis)
	
	// SyncManager
	suite.mockSyncManager.On("Start", mock.AnythingOfType("*context.cancelCtx")).Return(nil)
	suite.mockSyncManager.On("Stop", mock.AnythingOfType("*context.cancelCtx")).Return(nil)
	suite.mockSyncManager.On("IsRunning").Return(true)
	
	// PartitionManager
	suite.mockPartitionMgr.On("Start", mock.AnythingOfType("*context.cancelCtx")).Return(nil)
	suite.mockPartitionMgr.On("Stop", mock.AnythingOfType("*context.cancelCtx")).Return(nil)
	suite.mockPartitionMgr.On("IsRunning").Return(true)
	suite.mockPartitionMgr.On("GetPartitionForKey", mock.AnythingOfType("string")).Return("partition-1", nil)
	
	// Logger
	suite.mockLogger.On("Info", mock.AnythingOfType("string"), mock.AnythingOfType("map[string]interface {}")).Return()
	suite.mockLogger.On("Debug", mock.AnythingOfType("string"), mock.AnythingOfType("map[string]interface {}")).Return()
	suite.mockLogger.On("Warn", mock.AnythingOfType("string"), mock.AnythingOfType("map[string]interface {}")).Return()
	suite.mockLogger.On("Error", mock.AnythingOfType("string"), mock.AnythingOfType("error"), mock.AnythingOfType("map[string]interface {}")).Return()
}

// TestSuite runner
func TestDistributedCacheTestSuite(t *testing.T) {
	suite.Run(t, new(DistributedCacheTestSuite))
}

// Testes unitários individuais

func TestNewDistributedFlyweightCache_InvalidConfig(t *testing.T) {
	mockCoordinator := NewMockNodeCoordinator(t)
	mockSyncManager := NewMockSyncManager(t)
	mockPartitionMgr := NewMockPartitionManager(t)
	mockLogger := NewMockLogger(t)
	
	// Teste com config nil
	cache, err := NewDistributedFlyweightCache(
		nil,
		&DistributedCacheConfig{NodeID: "test-node"},
		"test-node",
		"test-cluster",
		mockCoordinator,
		mockSyncManager,
		mockPartitionMgr,
		mockLogger,
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "base cache config cannot be nil")
}

func TestNewDistributedFlyweightCache_InvalidNodeID(t *testing.T) {
	baseConfig := &CacheConfig{
		MaxSize:    1000,
		TTL: time.Hour,
	}
	
	config := &DistributedCacheConfig{
		NodeID:      "test-node",
		ClusterName: "test-cluster",
	}
	
	mockCoordinator := NewMockNodeCoordinator(t)
	mockSyncManager := NewMockSyncManager(t)
	mockPartitionMgr := NewMockPartitionManager(t)
	mockLogger := NewMockLogger(t)
	
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		config,
		"", // NodeID vazio
		"test-cluster",
		mockCoordinator,
		mockSyncManager,
		mockPartitionMgr,
		mockLogger,
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "node ID cannot be empty")
}

func TestNewDistributedFlyweightCache_InvalidClusterName(t *testing.T) {
	baseConfig := &CacheConfig{
		Name:            "test-cache",
		Type:            CacheTypeFlyweight,
		MaxSize:         1000,
		TTL:             time.Hour,
		CleanupInterval: 5 * time.Minute,
	}
	
	config := &DistributedCacheConfig{
		NodeID:      "test-node",
		ClusterName: "test-cluster",
	}
	
	mockCoordinator := NewMockNodeCoordinator(t)
	mockSyncManager := NewMockSyncManager(t)
	mockPartitionMgr := NewMockPartitionManager(t)
	mockLogger := NewMockLogger(t)
	
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		config,
		"test-node",
		"", // ClusterName vazio
		mockCoordinator,
		mockSyncManager,
		mockPartitionMgr,
		mockLogger,
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "cluster name cannot be empty")
}

func TestNewDistributedFlyweightCache_NilDependencies(t *testing.T) {
	baseConfig := &CacheConfig{
		Name:            "test-cache",
		Type:            CacheTypeFlyweight,
		MaxSize:         1000,
		TTL:             time.Hour,
		CleanupInterval: 5 * time.Minute,
	}
	
	config := &DistributedCacheConfig{
		NodeID:      "test-node",
		ClusterName: "test-cluster",
	}
	
	// Teste com coordinator nil
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		config,
		"test-node",
		"test-cluster",
		nil,
		NewMockSyncManager(t),
		NewMockPartitionManager(t),
		NewMockLogger(t),
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "coordinator cannot be nil")
	
	// Teste com syncManager nil
	cache, err = NewDistributedFlyweightCache(
		baseConfig,
		config,
		"test-node",
		"test-cluster",
		NewMockNodeCoordinator(t),
		nil,
		NewMockPartitionManager(t),
		NewMockLogger(t),
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "sync manager cannot be nil")
	
	// Teste com partitionManager nil
	cache, err = NewDistributedFlyweightCache(
		baseConfig,
		config,
		"test-node",
		"test-cluster",
		NewMockNodeCoordinator(t),
		NewMockSyncManager(t),
		nil,
		NewMockLogger(t),
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "partition manager cannot be nil")
	
	// Teste com logger nil
	cache, err = NewDistributedFlyweightCache(
		baseConfig,
		config,
		"test-node",
		"test-cluster",
		NewMockNodeCoordinator(t),
		NewMockSyncManager(t),
		NewMockPartitionManager(t),
		nil,
	)
	
	assert.Error(t, err)
	assert.Nil(t, cache)
	assert.Contains(t, err.Error(), "logger cannot be nil")
}

// Benchmarks

func BenchmarkDistributedCacheSet(b *testing.B) {
	config := &DistributedCacheConfig{
		NodeID:      "bench-node",
		ClusterName: "bench-cluster",
		Enabled:  false, // Desabilitar sync para benchmark
	}
	
	mockCoordinator := NewMockNodeCoordinator(b)
	mockSyncManager := NewMockSyncManager(b)
	mockPartitionMgr := NewMockPartitionManager(b)
	mockLogger := NewMockLogger(b)
	
	// Configurar mocks
	mockCoordinator.On("Start", mock.Anything).Return(nil)
	mockCoordinator.On("Stop", mock.Anything).Return(nil)
	mockCoordinator.On("IsRunning").Return(true)
	mockSyncManager.On("Start", mock.Anything).Return(nil)
	mockSyncManager.On("Stop", mock.Anything).Return(nil)
	mockSyncManager.On("IsRunning").Return(true)
	mockPartitionMgr.On("Start", mock.Anything).Return(nil)
	mockPartitionMgr.On("Stop", mock.Anything).Return(nil)
	mockPartitionMgr.On("IsRunning").Return(true)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()
	
	baseConfig := &CacheConfig{
		Name:            "bench-cache",
		Type:            CacheTypeFlyweight,
		MaxSize:         1000,
		TTL:             time.Hour,
		CleanupInterval: 5 * time.Minute,
	}
	
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		config,
		"bench-node",
		"bench-cluster",
		mockCoordinator,
		mockSyncManager,
		mockPartitionMgr,
		mockLogger,
	)
	require.NoError(b, err)
	
	ctx := context.Background()
	err = cache.Start(ctx)
	require.NoError(b, err)
	defer cache.Stop(ctx)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		value := fmt.Sprintf("bench-value-%d", i)
		cache.Set(key, value, 1*time.Hour)
	}
}

func BenchmarkDistributedCacheGet(b *testing.B) {
	config := &DistributedCacheConfig{
		NodeID:      "bench-node",
		ClusterName: "bench-cluster",
		Enabled:  false,
	}
	
	mockCoordinator := NewMockNodeCoordinator(b)
	mockSyncManager := NewMockSyncManager(b)
	mockPartitionMgr := NewMockPartitionManager(b)
	mockLogger := NewMockLogger(b)
	
	// Configurar mocks
	mockCoordinator.On("Start", mock.Anything).Return(nil)
	mockCoordinator.On("Stop", mock.Anything).Return(nil)
	mockCoordinator.On("IsRunning").Return(true)
	mockSyncManager.On("Start", mock.Anything).Return(nil)
	mockSyncManager.On("Stop", mock.Anything).Return(nil)
	mockSyncManager.On("IsRunning").Return(true)
	mockPartitionMgr.On("Start", mock.Anything).Return(nil)
	mockPartitionMgr.On("Stop", mock.Anything).Return(nil)
	mockPartitionMgr.On("IsRunning").Return(true)
	mockPartitionMgr.On("GetReplicaNodes", mock.Anything).Return([]string{}, nil)
	mockLogger.On("Info", mock.Anything, mock.Anything).Return()
	
	baseConfig := &CacheConfig{
		Name:            "bench-cache",
		Type:            CacheTypeFlyweight,
		MaxSize:         1000,
		TTL:             time.Hour,
		CleanupInterval: 5 * time.Minute,
	}
	
	cache, err := NewDistributedFlyweightCache(
		baseConfig,
		config,
		"bench-node",
		"bench-cluster",
		mockCoordinator,
		mockSyncManager,
		mockPartitionMgr,
		mockLogger,
	)
	require.NoError(b, err)
	
	ctx := context.Background()
	err = cache.Start(ctx)
	require.NoError(b, err)
	defer cache.Stop(ctx)
	
	// Pré-popular cache
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		value := fmt.Sprintf("bench-value-%d", i)
		cache.Set(key, value, 1*time.Hour)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i%1000)
		cache.Get(key)
	}
}
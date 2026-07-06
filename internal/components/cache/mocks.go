/**
 * @file mocks.go
 * @description Mocks para testes do sistema de cache distribuído
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/mocks.go
 */

package cache

import (
	"context"
	"time"

	"github.com/stretchr/testify/mock"
)

// MockNodeCoordinator mock para NodeCoordinator
type MockNodeCoordinator struct {
	mock.Mock
}

func NewMockNodeCoordinator(t mock.TestingT) *MockNodeCoordinator {
	m := &MockNodeCoordinator{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockNodeCoordinator) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockNodeCoordinator) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockNodeCoordinator) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockNodeCoordinator) DiscoverNodes(ctx context.Context) ([]*NodeInfo, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*NodeInfo), args.Error(1)
}

func (m *MockNodeCoordinator) RegisterNode(ctx context.Context, node *NodeInfo) error {
	args := m.Called(ctx, node)
	return args.Error(0)
}

func (m *MockNodeCoordinator) UnregisterNode(ctx context.Context, nodeID string) error {
	args := m.Called(ctx, nodeID)
	return args.Error(0)
}

func (m *MockNodeCoordinator) UpdateNodeStatus(ctx context.Context, nodeID string, status NodeStatus) error {
	args := m.Called(ctx, nodeID, status)
	return args.Error(0)
}

func (m *MockNodeCoordinator) BroadcastOperation(ctx context.Context, operation *SyncOperation) error {
	args := m.Called(ctx, operation)
	return args.Error(0)
}

func (m *MockNodeCoordinator) SendToNode(ctx context.Context, nodeID string, operation *SyncOperation) error {
	args := m.Called(ctx, nodeID, operation)
	return args.Error(0)
}

func (m *MockNodeCoordinator) ReceiveOperations(ctx context.Context) (<-chan *SyncOperation, error) {
	args := m.Called(ctx)
	return args.Get(0).(<-chan *SyncOperation), args.Error(1)
}

func (m *MockNodeCoordinator) SendHeartbeat(ctx context.Context, node *NodeInfo) error {
	args := m.Called(ctx, node)
	return args.Error(0)
}

func (m *MockNodeCoordinator) GetNodeHealth(nodeID string) (*NodeHealth, error) {
	args := m.Called(nodeID)
	return args.Get(0).(*NodeHealth), args.Error(1)
}

func (m *MockNodeCoordinator) GetCoordinatorType() CoordinatorType {
	args := m.Called()
	return args.Get(0).(CoordinatorType)
}

func (m *MockNodeCoordinator) UpdateConfig(config interface{}) error {
	args := m.Called(config)
	return args.Error(0)
}

// MockSyncManager mock para SyncManager
type MockSyncManager struct {
	mock.Mock
}

func NewMockSyncManager(t mock.TestingT) *MockSyncManager {
	m := &MockSyncManager{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockSyncManager) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockSyncManager) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockSyncManager) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockSyncManager) QueueOperation(operation *SyncOperation) error {
	args := m.Called(operation)
	return args.Error(0)
}

func (m *MockSyncManager) ProcessOperation(operation *SyncOperation) error {
	args := m.Called(operation)
	return args.Error(0)
}

func (m *MockSyncManager) GetPendingOperations() []*SyncOperation {
	args := m.Called()
	return args.Get(0).([]*SyncOperation)
}

func (m *MockSyncManager) GetSyncMetrics() *SyncMetrics {
	args := m.Called()
	return args.Get(0).(*SyncMetrics)
}

func (m *MockSyncManager) GetSyncStatus() *SyncStatus {
	args := m.Called()
	return args.Get(0).(*SyncStatus)
}

func (m *MockSyncManager) ResolveConflict(ctx context.Context, local, remote *CacheItem) (*CacheItem, error) {
	args := m.Called(ctx, local, remote)
	return args.Get(0).(*CacheItem), args.Error(1)
}

func (m *MockSyncManager) SetBatchSize(size int) {
	m.Called(size)
}

func (m *MockSyncManager) SetConflictStrategy(strategy ConflictStrategy) {
	m.Called(strategy)
}

func (m *MockSyncManager) SetSyncInterval(interval time.Duration) {
	m.Called(interval)
}

func (m *MockSyncManager) StartSync(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockSyncManager) StopSync(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockSyncManager) SyncOperation(ctx context.Context, op *SyncOperation) error {
	args := m.Called(ctx, op)
	return args.Error(0)
}

// MockPartitionManager mock para PartitionManager
type MockPartitionManager struct {
	mock.Mock
}

func NewMockPartitionManager(t mock.TestingT) *MockPartitionManager {
	m := &MockPartitionManager{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockPartitionManager) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockPartitionManager) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockPartitionManager) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockPartitionManager) GetPartitionForKey(key string) (string, error) {
	args := m.Called(key)
	return args.String(0), args.Error(1)
}

func (m *MockPartitionManager) GetReplicaNodes(key string) ([]string, error) {
	args := m.Called(key)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockPartitionManager) AddNode(nodeID string) error {
	args := m.Called(nodeID)
	return args.Error(0)
}

func (m *MockPartitionManager) RemoveNode(nodeID string) error {
	args := m.Called(nodeID)
	return args.Error(0)
}

func (m *MockPartitionManager) GetPartitions() ([]*PartitionInfo, error) {
	args := m.Called()
	return args.Get(0).([]*PartitionInfo), args.Error(1)
}

func (m *MockPartitionManager) GetPartitionMetrics() *PartitionMetrics {
	args := m.Called()
	return args.Get(0).(*PartitionMetrics)
}

func (m *MockPartitionManager) AssignPartition(partition int, nodeID string) error {
	args := m.Called(partition, nodeID)
	return args.Error(0)
}

func (m *MockPartitionManager) GetPartition(key string) int {
	args := m.Called(key)
	return args.Int(0)
}

func (m *MockPartitionManager) GetPartitionNodes(partition int) []*NodeInfo {
	args := m.Called(partition)
	return args.Get(0).([]*NodeInfo)
}

func (m *MockPartitionManager) GetRebalanceStatus() *RebalanceStatus {
	args := m.Called()
	return args.Get(0).(*RebalanceStatus)
}

func (m *MockPartitionManager) Rebalance(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockPartitionManager) SetPartitionCount(count int) error {
	args := m.Called(count)
	return args.Error(0)
}

func (m *MockPartitionManager) SetReplicationFactor(factor int) error {
	args := m.Called(factor)
	return args.Error(0)
}

// MockLogger mock para Logger
type MockLogger struct {
	mock.Mock
}

func NewMockLogger(t mock.TestingT) *MockLogger {
	m := &MockLogger{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockLogger) Debug(message string, fields map[string]interface{}) {
	m.Called(message, fields)
}

func (m *MockLogger) Info(message string, fields map[string]interface{}) {
	m.Called(message, fields)
}

func (m *MockLogger) Warn(message string, fields map[string]interface{}) {
	m.Called(message, fields)
}

func (m *MockLogger) Error(message string, err error, fields map[string]interface{}) {
	m.Called(message, err, fields)
}

// MockConflictResolver mock para ConflictResolver
type MockConflictResolver struct {
	mock.Mock
}

func NewMockConflictResolver(t mock.TestingT) *MockConflictResolver {
	m := &MockConflictResolver{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockConflictResolver) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockConflictResolver) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockConflictResolver) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockConflictResolver) DetectConflict(conflictType ConflictType, key string, values []*ConflictValue) (*ConflictInfo, error) {
	args := m.Called(conflictType, key, values)
	return args.Get(0).(*ConflictInfo), args.Error(1)
}

func (m *MockConflictResolver) ResolveConflict(ctx context.Context, conflictID string) (*ConflictResolution, error) {
	args := m.Called(ctx, conflictID)
	return args.Get(0).(*ConflictResolution), args.Error(1)
}

func (m *MockConflictResolver) GetPendingConflicts() []*ConflictInfo {
	args := m.Called()
	return args.Get(0).([]*ConflictInfo)
}

func (m *MockConflictResolver) GetConflictMetrics() *ConflictMetrics {
	args := m.Called()
	return args.Get(0).(*ConflictMetrics)
}

// MockNodeDiscoveryService mock para NodeDiscoveryService
type MockNodeDiscoveryService struct {
	mock.Mock
}

func NewMockNodeDiscoveryService(t mock.TestingT) *MockNodeDiscoveryService {
	m := &MockNodeDiscoveryService{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockNodeDiscoveryService) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockNodeDiscoveryService) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockNodeDiscoveryService) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockNodeDiscoveryService) GetDiscoveredNodes() []*DiscoveredNode {
	args := m.Called()
	return args.Get(0).([]*DiscoveredNode)
}

func (m *MockNodeDiscoveryService) GetDiscoveryMetrics() *DiscoveryMetrics {
	args := m.Called()
	return args.Get(0).(*DiscoveryMetrics)
}

// MockDiscoveryStrategy mock para DiscoveryStrategy
type MockDiscoveryStrategy struct {
	mock.Mock
}

func NewMockDiscoveryStrategy(t mock.TestingT) *MockDiscoveryStrategy {
	m := &MockDiscoveryStrategy{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockDiscoveryStrategy) GetName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockDiscoveryStrategy) Discover(ctx context.Context) ([]*DiscoveredNode, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*DiscoveredNode), args.Error(1)
}

func (m *MockDiscoveryStrategy) IsSupported() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockDiscoveryStrategy) GetConfig() interface{} {
	args := m.Called()
	return args.Get(0)
}

func (m *MockDiscoveryStrategy) UpdateConfig(config interface{}) error {
	args := m.Called(config)
	return args.Error(0)
}

// MockConflictResolutionStrategy mock para ConflictResolutionStrategy
type MockConflictResolutionStrategy struct {
	mock.Mock
}

func NewMockConflictResolutionStrategy(t mock.TestingT) *MockConflictResolutionStrategy {
	m := &MockConflictResolutionStrategy{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockConflictResolutionStrategy) GetName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConflictResolutionStrategy) GetType() ConflictResolutionType {
	args := m.Called()
	return args.Get(0).(ConflictResolutionType)
}

func (m *MockConflictResolutionStrategy) CanResolve(conflict *ConflictInfo) bool {
	args := m.Called(conflict)
	return args.Bool(0)
}

func (m *MockConflictResolutionStrategy) Resolve(ctx context.Context, conflict *ConflictInfo) (*ConflictResolution, error) {
	args := m.Called(ctx, conflict)
	return args.Get(0).(*ConflictResolution), args.Error(1)
}

func (m *MockConflictResolutionStrategy) GetConfidence(conflict *ConflictInfo) float64 {
	args := m.Called(conflict)
	return args.Get(0).(float64)
}

// MockConflictHandler mock para ConflictHandler
type MockConflictHandler struct {
	mock.Mock
	next ConflictHandler
}

func NewMockConflictHandler(t mock.TestingT) *MockConflictHandler {
	m := &MockConflictHandler{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockConflictHandler) CanHandle(conflict *ConflictInfo) bool {
	args := m.Called(conflict)
	return args.Bool(0)
}

func (m *MockConflictHandler) Handle(ctx context.Context, conflict *ConflictInfo) (*ConflictResolution, error) {
	args := m.Called(ctx, conflict)
	return args.Get(0).(*ConflictResolution), args.Error(1)
}

func (m *MockConflictHandler) SetNext(handler ConflictHandler) {
	m.next = handler
	m.Called(handler)
}

func (m *MockConflictHandler) GetNext() ConflictHandler {
	args := m.Called()
	return args.Get(0).(ConflictHandler)
}

// MockFlyweightCache mock para FlyweightCache (cache local)
type MockFlyweightCache struct {
	mock.Mock
	data map[string]interface{}
}

func NewMockFlyweightCache(t mock.TestingT) *MockFlyweightCache {
	m := &MockFlyweightCache{
		data: make(map[string]interface{}),
	}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockFlyweightCache) Set(key string, value interface{}, ttl time.Duration) error {
	args := m.Called(key, value, ttl)
	if args.Error(0) == nil {
		m.data[key] = value
	}
	return args.Error(0)
}

func (m *MockFlyweightCache) Get(key string) (interface{}, bool) {
	args := m.Called(key)
	if args.Bool(1) {
		return m.data[key], true
	}
	return args.Get(0), args.Bool(1)
}

func (m *MockFlyweightCache) Delete(key string) error {
	args := m.Called(key)
	if args.Error(0) == nil {
		delete(m.data, key)
	}
	return args.Error(0)
}

func (m *MockFlyweightCache) Clear() error {
	args := m.Called()
	if args.Error(0) == nil {
		m.data = make(map[string]interface{})
	}
	return args.Error(0)
}

func (m *MockFlyweightCache) GetStats() *CacheStats {
	args := m.Called()
	return args.Get(0).(*CacheStats)
}

func (m *MockFlyweightCache) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockFlyweightCache) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockFlyweightCache) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

// MockEvictionPolicy mock para EvictionPolicy
type MockEvictionPolicy struct {
	mock.Mock
}

func NewMockEvictionPolicy(t mock.TestingT) *MockEvictionPolicy {
	m := &MockEvictionPolicy{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockEvictionPolicy) ShouldEvict(key string, value interface{}, metadata map[string]interface{}) bool {
	args := m.Called(key, value, metadata)
	return args.Bool(0)
}

func (m *MockEvictionPolicy) OnAccess(key string) {
	m.Called(key)
}

func (m *MockEvictionPolicy) OnSet(key string, value interface{}) {
	m.Called(key, value)
}

func (m *MockEvictionPolicy) OnDelete(key string) {
	m.Called(key)
}

func (m *MockEvictionPolicy) GetEvictionCandidates(count int) []string {
	args := m.Called(count)
	return args.Get(0).([]string)
}

func (m *MockEvictionPolicy) Clear() {
	m.Called()
}

func (m *MockEvictionPolicy) GetStats() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

// MockSerializer mock para Serializer
type MockSerializer struct {
	mock.Mock
}

func NewMockSerializer(t mock.TestingT) *MockSerializer {
	m := &MockSerializer{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockSerializer) Serialize(value interface{}) ([]byte, error) {
	args := m.Called(value)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockSerializer) Deserialize(data []byte, target interface{}) error {
	args := m.Called(data, target)
	return args.Error(0)
}

func (m *MockSerializer) GetType() SerializationType {
	args := m.Called()
	return args.Get(0).(SerializationType)
}

// MockCompressor mock para Compressor
type MockCompressor struct {
	mock.Mock
}

func NewMockCompressor(t mock.TestingT) *MockCompressor {
	m := &MockCompressor{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockCompressor) Compress(data []byte) ([]byte, error) {
	args := m.Called(data)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockCompressor) Decompress(data []byte) ([]byte, error) {
	args := m.Called(data)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockCompressor) GetType() CompressionType {
	args := m.Called()
	return args.Get(0).(CompressionType)
}

// MockEncryptor mock para Encryptor
type MockEncryptor struct {
	mock.Mock
}

func NewMockEncryptor(t mock.TestingT) *MockEncryptor {
	m := &MockEncryptor{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockEncryptor) Encrypt(data []byte) ([]byte, error) {
	args := m.Called(data)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockEncryptor) Decrypt(data []byte) ([]byte, error) {
	args := m.Called(data)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockEncryptor) GetType() EncryptionType {
	args := m.Called()
	return args.Get(0).(EncryptionType)
}

// MockCacheMetrics mock para CacheMetrics
type MockCacheMetrics struct {
	mock.Mock
}

func NewMockCacheMetrics(t mock.TestingT) *MockCacheMetrics {
	m := &MockCacheMetrics{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockCacheMetrics) RecordHit(cacheName string) {
	m.Called(cacheName)
}

func (m *MockCacheMetrics) RecordMiss(cacheName string) {
	m.Called(cacheName)
}

func (m *MockCacheMetrics) RecordEviction(cacheName string, reason string) {
	m.Called(cacheName, reason)
}

func (m *MockCacheMetrics) RecordOperation(cacheName string, operation string, duration time.Duration) {
	m.Called(cacheName, operation, duration)
}

func (m *MockCacheMetrics) GetMetrics(cacheName string) *CacheMetrics {
	args := m.Called(cacheName)
	return args.Get(0).(*CacheMetrics)
}

// MockPersistentStorage mock para PersistentStorage
type MockPersistentStorage struct {
	mock.Mock
}

func NewMockPersistentStorage(t mock.TestingT) *MockPersistentStorage {
	m := &MockPersistentStorage{}
	if t != nil {
		m.Test(t)
	}
	return m
}

func (m *MockPersistentStorage) Save(key string, value []byte) error {
	args := m.Called(key, value)
	return args.Error(0)
}

func (m *MockPersistentStorage) Load(key string) ([]byte, error) {
	args := m.Called(key)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockPersistentStorage) Delete(key string) error {
	args := m.Called(key)
	return args.Error(0)
}

func (m *MockPersistentStorage) Exists(key string) bool {
	args := m.Called(key)
	return args.Bool(0)
}

func (m *MockPersistentStorage) Clear() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockPersistentStorage) GetKeys() ([]string, error) {
	args := m.Called()
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockPersistentStorage) GetStats() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

// Helper functions para criar instâncias de teste

// CreateTestNodeInfo cria NodeInfo para testes
func CreateTestNodeInfo(id, address string, status NodeStatus) *NodeInfo {
	return &NodeInfo{
		ID:       id,
		Address:  address,
		Status:   status,
		LastSeen: time.Now(),
		Metadata: make(map[string]string),
	}
}

// CreateTestSyncOperation cria SyncOperation para testes
func CreateTestSyncOperation(id, key string, value interface{}, opType SyncOperationType) *SyncOperation {
	return &SyncOperation{
		ID:        id,
		Type:      opType,
		Key:       key,
		Value:     value,
		TTL:       1 * time.Hour,
		NodeID:    "test-node",
		Timestamp: time.Now(),
		Version:   1,
		Metadata:  make(map[string]interface{}),
	}
}

// CreateTestConflictInfo cria ConflictInfo para testes
func CreateTestConflictInfo(id, key string, values []*ConflictValue) *ConflictInfo {
	return &ConflictInfo{
		ID:                id,
		Type:              ConflictTypeWrite,
		Key:               key,
		ConflictingValues: values,
		DetectedAt:        time.Now(),
		Status:            ConflictStatusPending,
		Priority:          ConflictPriorityMedium,
		Metadata:          make(map[string]string),
		Context:           context.Background(),
		Timeout:           time.Now().Add(30 * time.Second),
		MaxRetries:        3,
	}
}

// CreateTestConflictValue cria ConflictValue para testes
func CreateTestConflictValue(value interface{}, nodeID string, version int64) *ConflictValue {
	return &ConflictValue{
		Value:       value,
		NodeID:      nodeID,
		Timestamp:   time.Now(),
		Version:     version,
		VectorClock: make(map[string]int64),
		Checksum:    "test-checksum",
		Size:        100,
		Metadata:    make(map[string]string),
	}
}

// CreateTestDistributedCacheConfig cria configuração para testes
func CreateTestDistributedCacheConfig() *DistributedCacheConfig {
	return &DistributedCacheConfig{
		NodeID:           "test-node-1",
		ClusterName:      "test-cluster",
		ConsistencyLevel: ConsistencyEventual,
		Enabled:          true,
		
		CoordinatorType:  CoordinatorRedis,
		RedisConfig: &RedisConfig{
			Address:   "localhost:6379",
			Password:  "",
			DB:        0,
			PoolSize:  10,
			Timeout:   time.Second * 5,
			KeyPrefix: "test:",
			Channel:   "test-sync",
		},
		GRPCConfig: &GRPCConfig{
			Host:           "localhost",
			Port:           9090,
			MaxConnections: 100,
			Timeout:        time.Second * 30,
			KnownNodes:     []string{"localhost:9091", "localhost:9092"},
		},
		
		SyncInterval:     time.Second * 30,
		SyncBatchSize:    100,
		SyncTimeout:      time.Second * 10,
		ConflictStrategy: ConflictLastWrite,
		SyncBufferSize:   1000,
		
		ConflictResolution: &ConflictResolutionConfig{
			DefaultStrategy:      ResolutionLastWriteWins,
			ConsistencyLevel:     ConsistencyEventual,
			EnableVectorClock:    false,
			EnableTimestamp:      true,
			EnableVersioning:     true,
			ConflictTimeout:      time.Second * 5,
			MaxConflictHistory:   100,
			ResolutionTimeout:    time.Second * 10,
			EnableAutoResolution: true,
			QuorumSize:          2,
		},
		ConflictResolutionInterval: time.Second * 60,
		
		DiscoveryEnabled:  true,
		DiscoveryInterval: time.Second * 30,
		HeartbeatInterval: time.Second * 10,
		
		MaxNodes:         10,
		ReplicationFactor: 2,
		PartitionCount:   16,
		
		CrossNodeLookup:  true,
		RemoteCacheTTL:   time.Minute * 5,
		
		FailureThreshold:  3,
		RecoveryTimeout:   time.Minute * 2,
		RetryAttempts:     3,
		RetryBackoff:      time.Second * 2,
	}
}
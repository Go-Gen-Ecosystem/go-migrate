# Distributed Cache Architecture

## Overview

Esta documentação descreve a arquitetura de cache distribuído implementada seguindo padrões enterprise-grade, incluindo Hexagonal Architecture, CQRS, Event-Driven Design, e múltiplos design patterns para escalabilidade e manutenibilidade.

## Architecture Principles

### 1. Hexagonal Architecture (Ports & Adapters)
- **Domain Layer**: Interfaces e modelos de negócio (`DistributedCache`, `NodeCoordinator`, `SyncManager`)
- **Application Layer**: Casos de uso e orquestração (`DistributedFlyweightCache`)
- **Infrastructure Layer**: Implementações concretas (`RedisCoordinator`, `GRPCCoordinator`)
- **Adapters**: Pontes entre camadas com inversão de dependência

### 2. Design Patterns Implementados

#### Creational Patterns
- **Factory Pattern**: `DistributedCacheFactory` para criação de instâncias
- **Builder Pattern**: `DistributedCacheBuilder` para configuração fluente
- **Singleton Pattern**: Gerenciamento de instâncias no `DIContainer`

#### Structural Patterns
- **Adapter Pattern**: Coordenadores Redis/gRPC adaptam protocolos externos
- **Facade Pattern**: `CacheCluster` simplifica gerenciamento de múltiplos nós
- **Flyweight Pattern**: Cache base otimizado para uso de memória

#### Behavioral Patterns
- **Strategy Pattern**: Múltiplas estratégias de resolução de conflitos
- **Chain of Responsibility**: Pipeline de resolução de conflitos
- **Observer Pattern**: Sistema de eventos para sincronização
- **Command Pattern**: Operações de sincronização encapsuladas

### 3. CQRS & Event-Driven
- **Commands**: Operações de escrita (`Set`, `Delete`, `Clear`)
- **Queries**: Operações de leitura (`Get`, `GetStats`)
- **Events**: Sincronização entre nós via eventos assíncronos

## Core Components

### 1. Distributed Interfaces (`distributed_interfaces.go`)

```go
// Interface principal do cache distribuído
type DistributedCache interface {
    Set(key string, value interface{}, ttl time.Duration) error
    Get(key string) (interface{}, bool)
    Delete(key string) error
    Clear() error
    GetDistributedStats() *DistributedStats
}

// Coordenação entre nós
type NodeCoordinator interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    DiscoverNodes(ctx context.Context) ([]*NodeInfo, error)
    BroadcastOperation(op *SyncOperation) error
    // ... outros métodos
}
```

### 2. Node Coordinators

#### Redis Coordinator (`redis_coordinator.go`)
- **Protocolo**: Redis Pub/Sub para comunicação
- **Descoberta**: Redis Sets para registro de nós
- **Heartbeat**: Chaves com TTL para monitoramento
- **Características**:
  - Alta performance para clusters pequenos/médios
  - Simplicidade de configuração
  - Dependência externa (Redis)

#### gRPC Coordinator (`grpc_coordinator.go`)
- **Protocolo**: gRPC com Protocol Buffers
- **Descoberta**: Registro direto entre nós
- **Streaming**: Comunicação bidirecional
- **Características**:
  - Baixa latência
  - Type safety
  - Suporte a TLS nativo

### 3. Synchronization Manager (`sync_manager.go`)

```go
type DefaultSyncManager struct {
    // Configuração e identificação
    config      *SyncConfig
    nodeID      string
    clusterName string
    
    // Injeção de dependência
    coordinator NodeCoordinator
    
    // Processamento assíncrono
    syncQueue       chan *SyncOperation
    batchQueue      chan []*SyncOperation
    retryQueue      chan *SyncOperation
    
    // Controle de operações
    pendingOps      map[string]*SyncOperation
    completedOps    map[string]time.Time
    
    // Workers e métricas
    workers         int
    metrics         *SyncMetrics
}
```

**Características**:
- **Batching**: Agrupa operações para eficiência
- **Retry Logic**: Reprocessamento automático de falhas
- **Backpressure**: Controle de fluxo para evitar sobrecarga
- **Metrics**: Monitoramento detalhado de performance

### 4. Partition Manager (`partition_manager.go`)

Implementa **Consistent Hashing** para distribuição de dados:

```go
type ConsistentHashPartitionManager struct {
    config          *PartitionConfig
    hashRing        *ConsistentHashRing
    nodeList        []*NodeInfo
    partitionMap    map[uint32]*PartitionInfo
    rebalanceTimer  *time.Timer
}
```

**Algoritmo**:
1. Hash da chave usando SHA-256
2. Localização no ring consistente
3. Seleção de nós réplica baseada em fator de replicação
4. Rebalanceamento automático quando nós entram/saem

### 5. Conflict Resolution (`conflict_resolution.go`)

#### Estratégias Implementadas

1. **Last Write Wins (LWW)**
   ```go
   type LastWriteWinsStrategy struct{}
   // Resolve baseado em timestamp mais recente
   ```

2. **Vector Clock**
   ```go
   type VectorClockStrategy struct{}
   // Resolve usando relógios vetoriais para causalidade
   ```

3. **Quorum-Based**
   ```go
   type QuorumBasedStrategy struct{}
   // Resolve baseado em consenso de maioria
   ```

4. **Timestamp-Based**
   ```go
   type TimestampBasedStrategy struct{}
   // Resolve usando timestamps com tie-breaking
   ```

#### Chain of Responsibility
```go
type ConflictHandler interface {
    SetNext(handler ConflictHandler)
    Handle(conflict *ConflictInfo) (*ConflictResolution, error)
}
```

### 6. Node Discovery (`node_discovery.go`)

#### Estratégias de Descoberta

1. **Multicast Discovery**
   - UDP multicast para descoberta local
   - Ideal para redes locais

2. **Broadcast Discovery**
   - UDP broadcast para descoberta em subnet
   - Funciona em redes sem multicast

3. **DNS Discovery**
   - Descoberta via DNS SRV records
   - Ideal para ambientes cloud

### 7. Factory & DI Container (`distributed_factory.go`)

#### Dependency Injection Container
```go
type DIContainer struct {
    singletons map[string]interface{}
    factories  map[string]func() (interface{}, error)
    configs    map[string]interface{}
}
```

#### Factory Pattern
```go
type DistributedCacheFactory struct {
    container     *DIContainer
    defaultConfig *DistributedCacheConfig
    logger        Logger
}
```

#### Builder Pattern
```go
cache, err := factory.CreateBuilder().
    WithNodeID("app-node-1").
    WithClusterName("production").
    WithRedisCoordinator(&RedisConfig{...}).
    WithConflictResolution(&ConflictResolutionConfig{...}).
    Build()
```

### 8. Cluster Management (`distributed_factory.go`)

```go
type CacheCluster struct {
    name        string
    nodes       map[string]*DistributedFlyweightCache
    config      *ClusterConfig
    coordinator NodeCoordinator
    metrics     *ClusterMetrics
}
```

**Funcionalidades**:
- **Auto-scaling**: Adição/remoção automática de nós
- **Health Monitoring**: Monitoramento contínuo de saúde
- **Load Balancing**: Distribuição inteligente de carga
- **Failover**: Recuperação automática de falhas

## Configuration

### Basic Configuration
```go
config := &DistributedCacheConfig{
    NodeID:            "node-1",
    ClusterName:       "my-cluster",
    SyncInterval:      1 * time.Second,
    HeartbeatInterval: 30 * time.Second,
    NodeTimeout:       2 * time.Minute,
    EnableSync:        true,
}
```

### Redis Coordinator
```go
redisConfig := &RedisConfig{
    Address:        "localhost:6379",
    Password:       "secure-password",
    DB:             0,
    MaxRetries:     3,
    RetryDelay:     100 * time.Millisecond,
    DialTimeout:    5 * time.Second,
    ReadTimeout:    3 * time.Second,
    WriteTimeout:   3 * time.Second,
    PoolSize:       10,
    MinIdleConns:   5,
}
```

### gRPC Coordinator
```go
grpcConfig := &GRPCConfig{
    Address:           "localhost:50051",
    TLS:              true,
    MaxMessageSize:   4 * 1024 * 1024,
    ConnectionTimeout: 10 * time.Second,
    KeepAlive: &KeepAliveConfig{
        Time:    30 * time.Second,
        Timeout: 5 * time.Second,
    },
}
```

### Conflict Resolution
```go
conflictConfig := &ConflictResolutionConfig{
    DefaultStrategy:      ResolutionVectorClock,
    EnableAutoResolution: true,
    ConflictTimeout:      30 * time.Second,
    QuorumSize:          3,
    ConsistencyLevel:    ConsistencyStrong,
    MaxConflictAge:      5 * time.Minute,
    ConflictBatchSize:   50,
}
```

## Usage Examples

### 1. Simple Distributed Cache
```go
factory := NewDistributedCacheFactory(logger)

cache, err := factory.CreateDistributedCache(
    "node-1", 
    "my-cluster", 
    config,
)

ctx := context.Background()
cache.Start(ctx)
defer cache.Stop(ctx)

// Operations
cache.Set("key", "value", 5*time.Minute)
value, exists := cache.Get("key")
cache.Delete("key")
```

### 2. Cluster Setup
```go
clusterConfig := &ClusterConfig{
    Name:                "production-cluster",
    MinNodes:            3,
    MaxNodes:            10,
    ReplicationFactor:   2,
    ConsistencyLevel:    ConsistencyStrong,
    AutoScaling:         true,
    HealthCheckInterval: 30 * time.Second,
}

cluster, err := factory.CreateCluster(clusterConfig)
cluster.Start(ctx)
defer cluster.Stop(ctx)

// Dynamic node management
cluster.AddNode("new-node", nodeConfig)
cluster.RemoveNode("old-node")
```

### 3. Advanced Configuration
```go
cache, err := factory.CreateBuilder().
    WithNodeID("advanced-node").
    WithClusterName("advanced-cluster").
    WithRedisCoordinator(redisConfig).
    WithConflictResolution(conflictConfig).
    WithSyncInterval(500 * time.Millisecond).
    WithConsistencyLevel(ConsistencyStrong).
    Build()
```

## Testing Strategy

### 1. Unit Tests (`distributed_cache_test.go`)
- Testes isolados de componentes individuais
- Mocks para todas as dependências externas
- Cobertura de casos de erro e edge cases

### 2. Integration Tests (`distributed_integration_test.go`)
- Testes end-to-end com múltiplos nós
- Cenários de falha e recuperação
- Testes de performance e concorrência

### 3. Benchmarks
- Performance de operações individuais
- Throughput de cluster
- Latência de sincronização

## Monitoring & Metrics

### Cluster Metrics
```go
type ClusterMetrics struct {
    ActiveNodes     int
    TotalNodes      int
    TotalOperations int64
    TotalHits       int64
    TotalMisses     int64
    AverageLatency  time.Duration
    NodeMetrics     map[string]*NodeMetrics
}
```

### Node Metrics
```go
type NodeMetrics struct {
    NodeID          string
    Status          NodeStatus
    Operations      int64
    Hits            int64
    Misses          int64
    Latency         time.Duration
    LastUpdate      time.Time
    CustomMetrics   map[string]interface{}
}
```

### Distributed Stats
```go
type DistributedStats struct {
    LocalStats       *CacheStats
    TotalOperations  int64
    SyncOperations   int64
    SyncErrors       int64
    ConnectedNodes   int
    LastSyncTime     time.Time
    ConflictCount    int64
    ResolvedConflicts int64
}
```

## Performance Characteristics

### Scalability
- **Horizontal**: Suporte a centenas de nós
- **Vertical**: Otimizado para uso eficiente de recursos
- **Network**: Minimização de tráfego via batching

### Consistency Models
1. **Eventual Consistency**: Performance máxima, convergência garantida
2. **Strong Consistency**: Consistência imediata, maior latência
3. **Causal Consistency**: Ordem causal preservada

### Fault Tolerance
- **Network Partitions**: Detecção e recuperação automática
- **Node Failures**: Failover transparente
- **Split-Brain**: Prevenção via quorum

## Security Considerations

### Network Security
- **TLS**: Criptografia em trânsito para gRPC
- **Authentication**: Suporte a autenticação Redis
- **Network Isolation**: Configuração de interfaces específicas

### Data Security
- **Encryption**: Suporte a criptografia de dados
- **Access Control**: Controle de acesso baseado em nós
- **Audit Logging**: Log detalhado de operações

## Deployment Patterns

### 1. Single Datacenter
```
Node-1 ←→ Node-2 ←→ Node-3
   ↑         ↑         ↑
   └─── Redis Cluster ──┘
```

### 2. Multi-Datacenter
```
DC-1: Node-1 ←→ Node-2
        ↓
      WAN Link
        ↓
DC-2: Node-3 ←→ Node-4
```

### 3. Cloud Native
```
K8s Pod-1 ←→ K8s Pod-2 ←→ K8s Pod-3
     ↑           ↑           ↑
     └─── Service Mesh ──────┘
```

## Future Enhancements

### Planned Features
1. **Compression**: Compressão automática de dados grandes
2. **Persistence**: Backup e restore distribuído
3. **Analytics**: Métricas avançadas e insights
4. **Multi-Region**: Replicação entre regiões

### Performance Optimizations
1. **Memory Pooling**: Reutilização de buffers
2. **Connection Pooling**: Pool de conexões otimizado
3. **Batch Compression**: Compressão de batches de sincronização
4. **Adaptive Algorithms**: Algoritmos que se adaptam à carga

## Troubleshooting

### Common Issues

1. **Split-Brain Scenarios**
   - Sintoma: Nós com dados inconsistentes
   - Solução: Verificar configuração de quorum

2. **High Sync Latency**
   - Sintoma: Demora na propagação de mudanças
   - Solução: Ajustar `SyncInterval` e `BatchSize`

3. **Memory Leaks**
   - Sintoma: Uso crescente de memória
   - Solução: Verificar TTL e limpeza de operações antigas

4. **Network Partitions**
   - Sintoma: Nós desconectados
   - Solução: Verificar conectividade e timeouts

### Debug Tools
- **Metrics Dashboard**: Monitoramento em tempo real
- **Log Analysis**: Análise de logs estruturados
- **Health Checks**: Verificação automática de saúde
- **Trace Analysis**: Rastreamento de operações distribuídas

---

Esta arquitetura distribuída foi projetada seguindo princípios enterprise-grade, garantindo escalabilidade, confiabilidade e manutenibilidade para sistemas de produção críticos.
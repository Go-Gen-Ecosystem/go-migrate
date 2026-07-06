/**
 * @file example_usage.go
 * @description Exemplos de uso do sistema de cache distribuído
 * @author Admilson B. F. Cossa
 * @date 2024-12-19
 * @path pkg/core/database/migrations/internal/components/cache/example_usage.go
 */

package cache

import (
	"context"
	"fmt"
	"log"
	"time"
)

// SimpleLogger implementa uma versão simples do Logger para exemplos
type SimpleLogger struct{}

func (l *SimpleLogger) Info(msg string, fields map[string]interface{}) {
	log.Printf("INFO: %s %v", msg, fields)
}

func (l *SimpleLogger) Error(msg string, err error, fields map[string]interface{}) {
	log.Printf("ERROR: %s - %v - %v", msg, err, fields)
}

func (l *SimpleLogger) Debug(msg string, fields map[string]interface{}) {
	log.Printf("DEBUG: %s %v", msg, fields)
}

func (l *SimpleLogger) Warn(msg string, fields map[string]interface{}) {
	log.Printf("WARN: %s %v", msg, fields)
}

// ExampleBasicDistributedCache demonstra uso básico do cache distribuído
func ExampleBasicDistributedCache() {
	// Criar logger
	logger := &SimpleLogger{}
	
	// Criar factory
	factory := NewDistributedCacheFactory(logger)
	
	// Criar cache distribuído usando builder pattern
	cache, err := factory.CreateBuilder().
		WithNodeID("app-node-1").
		WithClusterName("my-app-cluster").
		WithSyncInterval(2 * time.Second).
		WithHeartbeatInterval(10 * time.Second).
		WithConsistencyLevel(ConsistencyEventual).
		WithRedisCoordinator(&RedisConfig{
			Address:  "localhost:6379",
			Password: "",
			DB:       0,
		}).
		Build()
	
	if err != nil {
		log.Fatal("Failed to create distributed cache:", err)
	}
	
	// Iniciar cache
	ctx := context.Background()
	if err := cache.Start(ctx); err != nil {
		log.Fatal("Failed to start cache:", err)
	}
	defer cache.Stop(ctx)
	
	// Operações básicas
	fmt.Println("=== Operações Básicas ===")
	
	// Set
	err = cache.Set("user:123", "John Doe", 5*time.Minute)
	if err != nil {
		log.Printf("Error setting value: %v", err)
	} else {
		fmt.Println("✓ Set user:123 = John Doe")
	}
	
	// Get
	value, exists := cache.Get("user:123")
	if exists {
		fmt.Printf("✓ Get user:123 = %v\n", value)
	} else {
		fmt.Println("✗ Key not found")
	}
	
	// Delete
	err = cache.Delete("user:123")
	if err != nil {
		log.Printf("Error deleting key: %v", err)
	} else {
		fmt.Println("✓ Deleted user:123")
	}
	
	// Verificar estatísticas
	stats := cache.GetDistributedStats()
	if stats != nil {
		fmt.Printf("✓ Sync operations: %d\n", stats.SyncOperations)
		fmt.Printf("✓ Cache hits: %d\n", stats.CacheStats.HitCount)
		fmt.Printf("✓ Cache misses: %d\n", stats.CacheStats.MissCount)
	}
}

// ExampleClusterSetup demonstra configuração de um cluster
func ExampleClusterSetup() {
	logger := &SimpleLogger{}
	factory := NewDistributedCacheFactory(logger)
	
	// Configuração do cluster
	clusterConfig := &ClusterConfig{
		Name:                "production-cluster",
		MinNodes:            3,
		MaxNodes:            10,
		ReplicationFactor:   2,
		ConsistencyLevel:    ConsistencyStrong,
		AutoScaling:         true,
		HealthCheckInterval: 30 * time.Second,
		NodeConfigs: map[string]*DistributedCacheConfig{
			"prod-node-1": {
				SyncInterval:      1 * time.Second,
				HeartbeatInterval: 15 * time.Second,
				SyncTimeout:       1 * time.Minute,
				Enabled:           true,
				RedisConfig: &RedisConfig{
					Address:  "redis-1:6379",
					Password: "secure-password",
					DB:       0,
					PoolSize: 10,
				},
				ConflictStrategy:         ConflictLastWrite,
			},
			"prod-node-2": {
				SyncInterval:      1 * time.Second,
				HeartbeatInterval: 15 * time.Second,
				SyncTimeout:       1 * time.Minute,
				Enabled:           true,
				GRPCConfig: &GRPCConfig{
					Port:       50051,
					TLSEnabled: true,
				},
			},
			"prod-node-3": {
				SyncInterval:      1 * time.Second,
				HeartbeatInterval: 15 * time.Second,
				SyncTimeout:       1 * time.Minute,
				Enabled:           true,
				RedisConfig: &RedisConfig{
					Address:  "redis-3:6379",
					Password: "secure-password",
					DB:       0,
					PoolSize: 10,
				},
			},
		},
	}
	
	// Criar cluster
	cluster, err := factory.CreateCluster(clusterConfig)
	if err != nil {
		log.Fatal("Failed to create cluster:", err)
	}
	
	// Iniciar cluster
	ctx := context.Background()
	if err := cluster.Start(ctx); err != nil {
		log.Fatal("Failed to start cluster:", err)
	}
	defer cluster.Stop(ctx)
	
	fmt.Println("=== Cluster Operations ===")
	
	// Aguardar inicialização
	time.Sleep(5 * time.Second)
	
	// Obter nós do cluster
	nodes := cluster.GetNodes()
	fmt.Printf("✓ Cluster started with %d nodes\n", len(nodes))
	
	// Testar operações distribuídas
	var firstNode *DistributedFlyweightCache
	for _, node := range nodes {
		firstNode = node
		break
	}
	
	if firstNode != nil {
		// Set em um nó
		err = firstNode.Set("distributed-key", "distributed-value", 0)
		if err != nil {
			log.Printf("Error setting distributed key: %v", err)
		} else {
			fmt.Println("✓ Set distributed-key on first node")
		}
		
		// Aguardar sincronização
		time.Sleep(3 * time.Second)
		
		// Verificar em outros nós
		for nodeID, node := range nodes {
			if node != firstNode {
				value, exists := node.Get("distributed-key")
				if exists {
					fmt.Printf("✓ Found distributed-key on %s = %v\n", nodeID, value)
				} else {
					fmt.Printf("✗ distributed-key not found on %s\n", nodeID)
				}
			}
		}
	}
	
	// Adicionar nó dinamicamente
	fmt.Println("\n=== Dynamic Node Management ===")
	
	newNodeConfig := &DistributedCacheConfig{
		SyncInterval:      1 * time.Second,
		HeartbeatInterval: 15 * time.Second,
		SyncTimeout:       1 * time.Minute,
		Enabled:           true,
		RedisConfig: &RedisConfig{
			Address: "redis-4:6379",
			DB:      0,
		},
	}
	
	err = cluster.AddNode("prod-node-4", newNodeConfig)
	if err != nil {
		log.Printf("Error adding node: %v", err)
	} else {
		fmt.Println("✓ Added new node prod-node-4")
	}
	
	// Verificar métricas do cluster
	time.Sleep(2 * time.Second)
	metrics := cluster.GetClusterMetrics()
	fmt.Printf("✓ Cluster metrics - Active: %d, Total: %d\n", 
		metrics.ActiveNodes, metrics.TotalNodes)
}

// ExampleAdvancedConfiguration demonstra configuração avançada
func ExampleAdvancedConfiguration() {
	logger := &SimpleLogger{}
	factory := NewDistributedCacheFactory(logger)
	
	fmt.Println("=== Advanced Configuration ===")
	
	// Configuração com múltiplos coordenadores
	cache1, err := factory.CreateBuilder().
		WithNodeID("hybrid-node-1").
		WithClusterName("hybrid-cluster").
		WithRedisCoordinator(&RedisConfig{
			Address:  "localhost:6379",
			DB:       1,
			PoolSize: 10,
		}).
		WithConflictResolution(&ConflictResolutionConfig{
			DefaultStrategy:      ResolutionVectorClock,
			EnableAutoResolution: true,
			ConflictTimeout:      15 * time.Second,
			QuorumSize:          3,
			ConsistencyLevel:    ConsistencyStrong,
			MaxConflictHistory:  1000,
			ResolutionTimeout:   10 * time.Second,
		}).
		Build()
	
	if err != nil {
		log.Fatal("Failed to create hybrid cache:", err)
	}
	
	// Configuração com gRPC
	cache2, err := factory.CreateBuilder().
		WithNodeID("grpc-node-1").
		WithClusterName("grpc-cluster").
		WithGRPCCoordinator(&GRPCConfig{
			Port:           50051,
			MaxConnections: 1000,
			Timeout:        30 * time.Second,
			KeepAlive:      60 * time.Second,
			TLSEnabled:     false,
		}).
		WithSyncInterval(500 * time.Millisecond).
		WithHeartbeatInterval(5 * time.Second).
		Build()
	
	if err != nil {
		log.Fatal("Failed to create gRPC cache:", err)
	}
	
	ctx := context.Background()
	
	// Iniciar ambos os caches
	if err := cache1.Start(ctx); err != nil {
		log.Fatal("Failed to start cache1:", err)
	}
	defer cache1.Stop(ctx)
	
	if err := cache2.Start(ctx); err != nil {
		log.Fatal("Failed to start cache2:", err)
	}
	defer cache2.Stop(ctx)
	
	fmt.Println("✓ Started hybrid and gRPC caches")
	
	// Testar operações
	testKey := "advanced-test-key"
	testValue := "advanced-test-value"
	
	// Set no cache Redis
	err = cache1.Set(testKey, testValue, 1*time.Minute)
	if err != nil {
		log.Printf("Error setting on Redis cache: %v", err)
	} else {
		fmt.Println("✓ Set on Redis cache")
	}
	
	// Set no cache gRPC
	err = cache2.Set(testKey+"_grpc", testValue+"_grpc", 1*time.Minute)
	if err != nil {
		log.Printf("Error setting on gRPC cache: %v", err)
	} else {
		fmt.Println("✓ Set on gRPC cache")
	}
	
	// Aguardar e verificar
	time.Sleep(2 * time.Second)
	
	value1, exists1 := cache1.Get(testKey)
	value2, exists2 := cache2.Get(testKey + "_grpc")
	
	if exists1 {
		fmt.Printf("✓ Redis cache value: %v\n", value1)
	}
	if exists2 {
		fmt.Printf("✓ gRPC cache value: %v\n", value2)
	}
}

// ExampleErrorHandlingAndRecovery demonstra tratamento de erros e recuperação
func ExampleErrorHandlingAndRecovery() {
	logger := &SimpleLogger{}
	factory := NewDistributedCacheFactory(logger)
	
	fmt.Println("=== Error Handling and Recovery ===")
	
	// Configuração com timeout agressivo para simular falhas
	cache, err := factory.CreateBuilder().
		WithNodeID("resilient-node").
		WithClusterName("resilient-cluster").
		WithSyncInterval(1 * time.Second).
		WithHeartbeatInterval(2 * time.Second).
		WithRedisCoordinator(&RedisConfig{
			Address:      "localhost:6379",
			DB:       2,
			PoolSize: 10,
		}).
		Build()
	
	if err != nil {
		log.Fatal("Failed to create resilient cache:", err)
	}
	
	ctx := context.Background()
	if err := cache.Start(ctx); err != nil {
		log.Printf("Expected error starting cache: %v", err)
		fmt.Println("✓ Handled startup error gracefully")
	}
	
	// Tentar operações mesmo com falhas de rede
	fmt.Println("\n=== Testing Resilience ===")
	
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("resilient-key-%d", i)
		value := fmt.Sprintf("resilient-value-%d", i)
		
		err = cache.Set(key, value, 30*time.Second)
		if err != nil {
			fmt.Printf("✗ Set failed for %s: %v\n", key, err)
		} else {
			fmt.Printf("✓ Set succeeded for %s\n", key)
		}
		
		retrievedValue, exists := cache.Get(key)
		if exists {
			fmt.Printf("✓ Get succeeded for %s = %v\n", key, retrievedValue)
		} else {
			fmt.Printf("✗ Get failed for %s\n", key)
		}
		
		time.Sleep(500 * time.Millisecond)
	}
	
	// Verificar estatísticas de erro
	stats := cache.GetDistributedStats()
	if stats != nil {
		fmt.Printf("✓ Final stats - Operations: %d, Conflict Rate: %.2f%%\n", 
			stats.SyncOperations, stats.ConflictRate)
	}
	
	cache.Stop(ctx)
}

// ExamplePerformanceTesting demonstra teste de performance
func ExamplePerformanceTesting() {
	logger := &SimpleLogger{}
	factory := NewDistributedCacheFactory(logger)
	
	fmt.Println("=== Performance Testing ===")
	
	// Criar cache otimizado para performance
	cache, err := factory.CreateBuilder().
		WithNodeID("perf-node").
		WithClusterName("perf-cluster").
		WithSyncInterval(5 * time.Second). // Sync menos frequente
		WithHeartbeatInterval(30 * time.Second).
		WithRedisCoordinator(&RedisConfig{
			Address:  "localhost:6379",
			DB:       3,
			PoolSize: 20,
		}).
		Build()
	
	if err != nil {
		log.Fatal("Failed to create performance cache:", err)
	}
	
	ctx := context.Background()
	if err := cache.Start(ctx); err != nil {
		log.Fatal("Failed to start performance cache:", err)
	}
	defer cache.Stop(ctx)
	
	// Teste de throughput
	numOperations := 1000
	startTime := time.Now()
	
	fmt.Printf("Executing %d operations...\n", numOperations)
	
	for i := 0; i < numOperations; i++ {
		key := fmt.Sprintf("perf-key-%d", i)
		value := fmt.Sprintf("perf-value-%d", i)
		
		// Set
		cache.Set(key, value, 5*time.Minute)
		
		// Get ocasional
		if i%10 == 0 {
			cache.Get(key)
		}
	}
	
	duration := time.Since(startTime)
	opsPerSecond := float64(numOperations) / duration.Seconds()
	
	fmt.Printf("✓ Completed %d operations in %v\n", numOperations, duration)
	fmt.Printf("✓ Throughput: %.2f ops/sec\n", opsPerSecond)
	
	// Verificar estatísticas finais
	stats := cache.GetDistributedStats()
	if stats != nil {
		hitRate := float64(stats.CacheStats.HitCount) / float64(stats.CacheStats.HitCount+stats.CacheStats.MissCount) * 100
		fmt.Printf("✓ Hit rate: %.2f%%\n", hitRate)
		fmt.Printf("✓ Sync operations: %d\n", stats.SyncOperations)
	}
}

// ExampleMonitoringAndMetrics demonstra monitoramento e métricas
func ExampleMonitoringAndMetrics() {
	logger := &SimpleLogger{}
	factory := NewDistributedCacheFactory(logger)
	
	fmt.Println("=== Monitoring and Metrics ===")
	
	// Criar cluster para monitoramento
	clusterConfig := &ClusterConfig{
		Name:                "monitoring-cluster",
		MinNodes:            2,
		MaxNodes:            5,
		ReplicationFactor:   1,
		ConsistencyLevel:    ConsistencyEventual,
		AutoScaling:         false,
		HealthCheckInterval: 2 * time.Second,
	}
	
	cluster, err := factory.CreateCluster(clusterConfig)
	if err != nil {
		log.Fatal("Failed to create monitoring cluster:", err)
	}
	
	ctx := context.Background()
	if err := cluster.Start(ctx); err != nil {
		log.Fatal("Failed to start monitoring cluster:", err)
	}
	defer cluster.Stop(ctx)
	
	// Executar operações para gerar métricas
	nodes := cluster.GetNodes()
	nodeList := make([]*DistributedFlyweightCache, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, node)
	}
	
	fmt.Printf("✓ Monitoring cluster with %d nodes\n", len(nodeList))
	
	// Gerar atividade
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("monitor-key-%d", i)
		value := fmt.Sprintf("monitor-value-%d", i)
		
		nodeIndex := i % len(nodeList)
		nodeList[nodeIndex].Set(key, value, 1*time.Minute)
		
		// Algumas operações de Get
		if i%5 == 0 {
			otherNodeIndex := (nodeIndex + 1) % len(nodeList)
			nodeList[otherNodeIndex].Get(key)
		}
	}
	
	// Aguardar processamento
	time.Sleep(5 * time.Second)
	
	// Coletar métricas do cluster
	fmt.Println("\n=== Cluster Metrics ===")
	clusterMetrics := cluster.GetClusterMetrics()
	
	fmt.Printf("Active Nodes: %d/%d\n", clusterMetrics.ActiveNodes, clusterMetrics.TotalNodes)
	fmt.Printf("Total Operations: %d\n", clusterMetrics.TotalOperations)
	fmt.Printf("Total Hits: %d\n", clusterMetrics.TotalHits)
	fmt.Printf("Total Misses: %d\n", clusterMetrics.TotalMisses)
	fmt.Printf("Average Latency: %v\n", clusterMetrics.AverageLatency)
	
	// Métricas por nó
	fmt.Println("\n=== Node Metrics ===")
	for nodeID, nodeMetric := range clusterMetrics.NodeMetrics {
		fmt.Printf("Node %s:\n", nodeID)
		fmt.Printf("  Status: %v\n", nodeMetric.Status)
		fmt.Printf("  Operations: %d\n", nodeMetric.Operations)
		fmt.Printf("  Hits: %d\n", nodeMetric.Hits)
		fmt.Printf("  Misses: %d\n", nodeMetric.Misses)
		fmt.Printf("  Latency: %v\n", nodeMetric.Latency)
		fmt.Printf("  Last Update: %v\n", nodeMetric.LastUpdate.Format(time.RFC3339))
	}
	
	// Métricas distribuídas individuais
	fmt.Println("\n=== Individual Node Stats ===")
	for nodeID, node := range nodes {
		stats := node.GetDistributedStats()
		if stats != nil {
			fmt.Printf("Node %s:\n", nodeID)
			fmt.Printf("  Cache Hits: %d\n", stats.CacheStats.HitCount)
			fmt.Printf("  Cache Misses: %d\n", stats.CacheStats.MissCount)
			fmt.Printf("  Sync Operations: %d\n", stats.SyncOperations)
			fmt.Printf("  Conflict Rate: %.2f\n", stats.ConflictRate)
			fmt.Printf("  Active Nodes: %d\n", stats.ActiveNodes)
			fmt.Printf("  Network Latency: %v\n", stats.NetworkLatency)
		}
	}
}

// main função principal para executar exemplos
func main() {
	fmt.Println("Distributed Cache System Examples")
	fmt.Println("=================================")
	
	// Executar exemplos (descomente conforme necessário)
	
	// ExampleBasicDistributedCache()
	// ExampleClusterSetup()
	// ExampleAdvancedConfiguration()
	// ExampleErrorHandlingAndRecovery()
	// ExamplePerformanceTesting()
	// ExampleMonitoringAndMetrics()
	
	fmt.Println("\nAll examples completed successfully!")
}
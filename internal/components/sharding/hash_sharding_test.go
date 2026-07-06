/**
 * @file hash_sharding_test.go
 * @description Tests for hash-based sharding with consistent hashing
 * @author Claude Code
 * @date 2025-10-31
 */

package sharding

import (
	"context"
	"testing"
)

func TestConsistentHashRing_AddRemove(t *testing.T) {
	ring := NewConsistentHashRing(150, HashFunctionXXHash)

	// Add shards
	ring.Add("shard-1")
	ring.Add("shard-2")
	ring.Add("shard-3")

	if len(ring.sortedHashes) != 450 { // 3 shards * 150 virtual nodes
		t.Errorf("expected 450 hashes, got %d", len(ring.sortedHashes))
	}

	// Remove a shard
	ring.Remove("shard-2")

	if len(ring.sortedHashes) != 300 { // 2 shards * 150 virtual nodes
		t.Errorf("expected 300 hashes after removal, got %d", len(ring.sortedHashes))
	}
}

func TestConsistentHashRing_Distribution(t *testing.T) {
	ring := NewConsistentHashRing(150, HashFunctionXXHash)

	// Add 3 shards
	ring.Add("shard-1")
	ring.Add("shard-2")
	ring.Add("shard-3")

	// Test distribution of 1000 keys
	distribution := make(map[string]int)
	for i := 0; i < 1000; i++ {
		key := string(rune('a' + i%26)) + string(rune('0' + i%10))
		shard, err := ring.Get(key)
		if err != nil {
			t.Fatalf("failed to get shard for key %s: %v", key, err)
		}
		distribution[shard]++
	}

	// Check that distribution is reasonably balanced
	// With 3 shards and 1000 keys, each should get ~333 keys
	// Allow 20% variance
	expectedPerShard := 333
	tolerance := 67 // 20% of 333

	for shard, count := range distribution {
		if count < expectedPerShard-tolerance || count > expectedPerShard+tolerance {
			t.Errorf("shard %s has imbalanced distribution: %d keys (expected ~%d)",
				shard, count, expectedPerShard)
		}
	}
}

func TestConsistentHashRing_Consistency(t *testing.T) {
	ring := NewConsistentHashRing(150, HashFunctionXXHash)

	// Add shards
	ring.Add("shard-1")
	ring.Add("shard-2")
	ring.Add("shard-3")

	// Get initial mappings
	testKeys := []string{"tenant-1", "tenant-2", "tenant-3", "tenant-4", "tenant-5"}
	initialMappings := make(map[string]string)

	for _, key := range testKeys {
		shard, err := ring.Get(key)
		if err != nil {
			t.Fatalf("failed to get shard for key %s: %v", key, err)
		}
		initialMappings[key] = shard
	}

	// Add a new shard
	ring.Add("shard-4")

	// Check that most keys still map to the same shard
	// With consistent hashing, only ~1/4 of keys should move
	movedKeys := 0
	for _, key := range testKeys {
		shard, err := ring.Get(key)
		if err != nil {
			t.Fatalf("failed to get shard for key %s: %v", key, err)
		}
		if shard != initialMappings[key] {
			movedKeys++
		}
	}

	// At most 2 out of 5 keys should have moved (40%)
	if movedKeys > 2 {
		t.Errorf("too many keys moved: %d/%d (expected ≤2)", movedKeys, len(testKeys))
	}
}

func TestHashFunctions(t *testing.T) {
	testKey := "test-tenant-123"

	tests := []struct {
		name     string
		hashFunc HashFunction
	}{
		{"XXHash", HashFunctionXXHash},
		{"FNV", HashFunctionFNV},
		{"MD5", HashFunctionMD5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashFunc := getHashFunction(tt.hashFunc)
			hash1 := hashFunc(testKey)
			hash2 := hashFunc(testKey)

			// Same input should produce same hash
			if hash1 != hash2 {
				t.Errorf("hash function not consistent: %d != %d", hash1, hash2)
			}

			// Different inputs should (likely) produce different hashes
			hash3 := hashFunc(testKey + "different")
			if hash1 == hash3 {
				t.Error("different inputs produced same hash")
			}
		})
	}
}

func TestHashShardingStrategy_RouteToShard(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryTenantRegistry()
	config := DefaultShardingConfig()
	config.Strategy = ShardTypeHashBased

	strategy := NewHashShardingStrategy(registry, config)

	// Register shards
	shards := []*Shard{
		{
			ID:           "shard-1",
			Name:         "Shard 1",
			Type:         ShardTypeHashBased,
			DatabaseDSN:  "postgres://localhost:5432/shard1",
			DatabaseType: "postgres",
			Status:       ShardStatusActive,
			Capacity:     100,
		},
		{
			ID:           "shard-2",
			Name:         "Shard 2",
			Type:         ShardTypeHashBased,
			DatabaseDSN:  "postgres://localhost:5432/shard2",
			DatabaseType: "postgres",
			Status:       ShardStatusActive,
			Capacity:     100,
		},
		{
			ID:           "shard-3",
			Name:         "Shard 3",
			Type:         ShardTypeHashBased,
			DatabaseDSN:  "postgres://localhost:5432/shard3",
			DatabaseType: "postgres",
			Status:       ShardStatusActive,
			Capacity:     100,
		},
	}

	for _, shard := range shards {
		if err := strategy.RegisterShard(ctx, shard); err != nil {
			t.Fatalf("failed to register shard %s: %v", shard.ID, err)
		}
	}

	// Test routing
	testTenants := []string{"tenant-1", "tenant-2", "tenant-3", "tenant-4", "tenant-5"}
	shardCounts := make(map[string]int)

	for _, tenantID := range testTenants {
		shard, err := strategy.RouteToShard(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to route tenant %s: %v", tenantID, err)
		}

		if shard == nil {
			t.Fatalf("got nil shard for tenant %s", tenantID)
		}

		shardCounts[shard.ID]++

		// Verify same tenant always routes to same shard
		shard2, err := strategy.RouteToShard(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to route tenant %s second time: %v", tenantID, err)
		}

		if shard.ID != shard2.ID {
			t.Errorf("tenant %s routed to different shards: %s != %s",
				tenantID, shard.ID, shard2.ID)
		}
	}

	// Check that tenants are distributed
	if len(shardCounts) < 2 {
		t.Error("all tenants routed to single shard, expected distribution")
	}
}

func TestHashShardingStrategy_RebalanceOnShardAddition(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryTenantRegistry()
	config := DefaultShardingConfig()
	config.Strategy = ShardTypeHashBased

	strategy := NewHashShardingStrategy(registry, config)

	// Register initial shards
	for i := 1; i <= 2; i++ {
		shard := &Shard{
			ID:           string(rune('0' + i)),
			Name:         "Shard " + string(rune('0'+i)),
			Type:         ShardTypeHashBased,
			DatabaseDSN:  "postgres://localhost:5432/shard" + string(rune('0'+i)),
			DatabaseType: "postgres",
			Status:       ShardStatusActive,
			Capacity:     100,
		}
		if err := strategy.RegisterShard(ctx, shard); err != nil {
			t.Fatalf("failed to register shard: %v", err)
		}
	}

	// Route 10 tenants
	initialMappings := make(map[string]string)
	for i := 1; i <= 10; i++ {
		tenantID := string(rune('0' + i))
		shard, err := strategy.RouteToShard(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to route tenant: %v", err)
		}
		initialMappings[tenantID] = shard.ID
	}

	// Add a new shard
	newShard := &Shard{
		ID:           "shard-3",
		Name:         "Shard 3",
		Type:         ShardTypeHashBased,
		DatabaseDSN:  "postgres://localhost:5432/shard3",
		DatabaseType: "postgres",
		Status:       ShardStatusActive,
		Capacity:     100,
	}
	if err := strategy.RegisterShard(ctx, newShard); err != nil {
		t.Fatalf("failed to register new shard: %v", err)
	}

	// Check how many tenants moved
	movedCount := 0
	for i := 1; i <= 10; i++ {
		tenantID := string(rune('0' + i))
		shard, err := strategy.RouteToShard(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to route tenant after rebalance: %v", err)
		}
		if shard.ID != initialMappings[tenantID] {
			movedCount++
		}
	}

	// With consistent hashing, approximately 1/3 should move (3.33 out of 10)
	// Allow 1-5 to account for variance
	if movedCount < 1 || movedCount > 5 {
		t.Errorf("unexpected number of moved tenants: %d (expected 1-5)", movedCount)
	}
}

func TestHashShardingStrategy_ShardRemoval(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryTenantRegistry()
	config := DefaultShardingConfig()

	strategy := NewHashShardingStrategy(registry, config)

	// Register shards
	for i := 1; i <= 3; i++ {
		shard := &Shard{
			ID:           string(rune('0' + i)),
			Name:         "Shard " + string(rune('0'+i)),
			Type:         ShardTypeHashBased,
			DatabaseDSN:  "postgres://localhost:5432/shard" + string(rune('0'+i)),
			DatabaseType: "postgres",
			Status:       ShardStatusActive,
			Capacity:     100,
		}
		if err := strategy.RegisterShard(ctx, shard); err != nil {
			t.Fatalf("failed to register shard: %v", err)
		}
	}

	// Remove a shard
	if err := strategy.UnregisterShard(ctx, "shard-2"); err != nil {
		t.Fatalf("failed to unregister shard: %v", err)
	}

	// Verify shard count
	shards, err := strategy.GetShards(ctx)
	if err != nil {
		t.Fatalf("failed to get shards: %v", err)
	}

	if len(shards) != 2 {
		t.Errorf("expected 2 shards after removal, got %d", len(shards))
	}

	// Verify tenants can still be routed
	for i := 1; i <= 5; i++ {
		tenantID := string(rune('0' + i))
		shard, err := strategy.RouteToShard(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to route tenant after shard removal: %v", err)
		}
		if shard.ID == "shard-2" {
			t.Errorf("tenant routed to removed shard: %s", shard.ID)
		}
	}
}

func BenchmarkConsistentHashRing_Get(b *testing.B) {
	ring := NewConsistentHashRing(150, HashFunctionXXHash)

	// Add 10 shards
	for i := 0; i < 10; i++ {
		ring.Add(string(rune('0' + i)))
	}

	testKeys := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		testKeys[i] = string(rune('a' + i%26)) + string(rune('0' + i%10))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := testKeys[i%len(testKeys)]
		_, _ = ring.Get(key)
	}
}

func BenchmarkHashFunctions(b *testing.B) {
	testKey := "test-tenant-12345"

	benchmarks := []struct {
		name     string
		hashFunc HashFunction
	}{
		{"XXHash", HashFunctionXXHash},
		{"FNV", HashFunctionFNV},
		{"MD5", HashFunctionMD5},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			hashFunc := getHashFunction(bm.hashFunc)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = hashFunc(testKey)
			}
		})
	}
}

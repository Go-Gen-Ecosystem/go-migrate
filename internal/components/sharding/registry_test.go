/**
 * @file registry_test.go
 * @description Tests for tenant registry implementations
 * @author Claude Code
 * @date 2025-10-31
 */

package sharding

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryTenantRegistry_RegisterTenant(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	err := registry.RegisterTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Try to register again (should fail)
	err = registry.RegisterTenant(ctx, tenant)
	if err == nil {
		t.Error("expected error when registering duplicate tenant, got nil")
	}
}

func TestInMemoryTenantRegistry_GetTenant(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	// Register tenant
	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Get tenant
	retrieved, err := registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("failed to get tenant: %v", err)
	}

	if retrieved.ID != tenant.ID {
		t.Errorf("expected ID %s, got %s", tenant.ID, retrieved.ID)
	}

	if retrieved.Name != tenant.Name {
		t.Errorf("expected name %s, got %s", tenant.Name, retrieved.Name)
	}

	if retrieved.ShardID != tenant.ShardID {
		t.Errorf("expected shard ID %s, got %s", tenant.ShardID, retrieved.ShardID)
	}

	// Try to get non-existent tenant
	_, err = registry.GetTenant(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent tenant, got nil")
	}
}

func TestInMemoryTenantRegistry_UpdateTenant(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	// Register tenant
	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Update tenant
	tenant.Name = "Updated Tenant"
	tenant.ShardID = "shard-2"

	if err := registry.UpdateTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to update tenant: %v", err)
	}

	// Verify update
	retrieved, err := registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("failed to get updated tenant: %v", err)
	}

	if retrieved.Name != "Updated Tenant" {
		t.Errorf("expected name 'Updated Tenant', got '%s'", retrieved.Name)
	}

	if retrieved.ShardID != "shard-2" {
		t.Errorf("expected shard ID 'shard-2', got '%s'", retrieved.ShardID)
	}
}

func TestInMemoryTenantRegistry_MoveTenant(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	// Register tenant
	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Move tenant
	if err := registry.MoveTenant(ctx, "tenant-1", "shard-2"); err != nil {
		t.Fatalf("failed to move tenant: %v", err)
	}

	// Verify move
	retrieved, err := registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("failed to get moved tenant: %v", err)
	}

	if retrieved.ShardID != "shard-2" {
		t.Errorf("expected shard ID 'shard-2', got '%s'", retrieved.ShardID)
	}
}

func TestInMemoryTenantRegistry_DeleteTenant(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	// Register tenant
	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Delete tenant
	if err := registry.DeleteTenant(ctx, "tenant-1"); err != nil {
		t.Fatalf("failed to delete tenant: %v", err)
	}

	// Verify deletion
	_, err := registry.GetTenant(ctx, "tenant-1")
	if err == nil {
		t.Error("expected error for deleted tenant, got nil")
	}
}

func TestInMemoryTenantRegistry_ListTenants(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	// Register multiple tenants
	tenants := []*Tenant{
		{ID: "tenant-1", Name: "Tenant 1", ShardID: "shard-1", Status: "active", Metadata: make(map[string]string)},
		{ID: "tenant-2", Name: "Tenant 2", ShardID: "shard-1", Status: "active", Metadata: make(map[string]string)},
		{ID: "tenant-3", Name: "Tenant 3", ShardID: "shard-2", Status: "active", Metadata: make(map[string]string)},
	}

	for _, tenant := range tenants {
		if err := registry.RegisterTenant(ctx, tenant); err != nil {
			t.Fatalf("failed to register tenant %s: %v", tenant.ID, err)
		}
	}

	// List all tenants
	allTenants, err := registry.ListTenants(ctx)
	if err != nil {
		t.Fatalf("failed to list tenants: %v", err)
	}

	if len(allTenants) != 3 {
		t.Errorf("expected 3 tenants, got %d", len(allTenants))
	}
}

func TestInMemoryTenantRegistry_ListTenantsByShard(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	// Register multiple tenants
	tenants := []*Tenant{
		{ID: "tenant-1", Name: "Tenant 1", ShardID: "shard-1", Status: "active", Metadata: make(map[string]string)},
		{ID: "tenant-2", Name: "Tenant 2", ShardID: "shard-1", Status: "active", Metadata: make(map[string]string)},
		{ID: "tenant-3", Name: "Tenant 3", ShardID: "shard-2", Status: "active", Metadata: make(map[string]string)},
	}

	for _, tenant := range tenants {
		if err := registry.RegisterTenant(ctx, tenant); err != nil {
			t.Fatalf("failed to register tenant %s: %v", tenant.ID, err)
		}
	}

	// List tenants by shard
	shard1Tenants, err := registry.ListTenantsByShard(ctx, "shard-1")
	if err != nil {
		t.Fatalf("failed to list tenants by shard: %v", err)
	}

	if len(shard1Tenants) != 2 {
		t.Errorf("expected 2 tenants for shard-1, got %d", len(shard1Tenants))
	}

	shard2Tenants, err := registry.ListTenantsByShard(ctx, "shard-2")
	if err != nil {
		t.Fatalf("failed to list tenants by shard: %v", err)
	}

	if len(shard2Tenants) != 1 {
		t.Errorf("expected 1 tenant for shard-2, got %d", len(shard2Tenants))
	}
}

func TestInMemoryTenantRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	// Register initial tenant
	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Perform concurrent reads and writes
	done := make(chan bool)
	iterations := 100

	// Concurrent readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				_, _ = registry.GetTenant(ctx, "tenant-1")
			}
			done <- true
		}()
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < iterations; j++ {
				updatedTenant := &Tenant{
					ID:       "tenant-1",
					Name:     "Test Tenant " + string(rune('0'+id)),
					ShardID:  "shard-1",
					Status:   "active",
					Metadata: make(map[string]string),
				}
				_ = registry.UpdateTenant(ctx, updatedTenant)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Verify tenant still exists
	_, err := registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Errorf("tenant corrupted after concurrent access: %v", err)
	}
}

func TestInMemoryTenantRegistry_Metadata(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	tenant := &Tenant{
		ID:      "tenant-1",
		Name:    "Test Tenant",
		ShardID: "shard-1",
		Status:  "active",
		Metadata: map[string]string{
			"region":      "us-east-1",
			"tier":        "premium",
			"created_by":  "admin",
		},
	}

	// Register tenant
	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	// Get tenant and verify metadata
	retrieved, err := registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("failed to get tenant: %v", err)
	}

	if len(retrieved.Metadata) != 3 {
		t.Errorf("expected 3 metadata entries, got %d", len(retrieved.Metadata))
	}

	if retrieved.Metadata["region"] != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got '%s'", retrieved.Metadata["region"])
	}

	if retrieved.Metadata["tier"] != "premium" {
		t.Errorf("expected tier 'premium', got '%s'", retrieved.Metadata["tier"])
	}
}

func TestInMemoryTenantRegistry_Timestamps(t *testing.T) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	beforeRegister := time.Now()

	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}

	// Register tenant
	if err := registry.RegisterTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to register tenant: %v", err)
	}

	afterRegister := time.Now()

	// Get tenant and verify timestamps
	retrieved, err := registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("failed to get tenant: %v", err)
	}

	if retrieved.CreatedAt.Before(beforeRegister) || retrieved.CreatedAt.After(afterRegister) {
		t.Errorf("CreatedAt timestamp out of range: %v", retrieved.CreatedAt)
	}

	if retrieved.UpdatedAt.Before(beforeRegister) || retrieved.UpdatedAt.After(afterRegister) {
		t.Errorf("UpdatedAt timestamp out of range: %v", retrieved.UpdatedAt)
	}

	// Update and check UpdatedAt changes
	time.Sleep(10 * time.Millisecond)
	beforeUpdate := time.Now()

	tenant.Name = "Updated Tenant"
	if err := registry.UpdateTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to update tenant: %v", err)
	}

	afterUpdate := time.Now()

	retrieved, err = registry.GetTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("failed to get updated tenant: %v", err)
	}

	if retrieved.UpdatedAt.Before(beforeUpdate) || retrieved.UpdatedAt.After(afterUpdate) {
		t.Errorf("UpdatedAt not updated correctly: %v", retrieved.UpdatedAt)
	}

	if !retrieved.UpdatedAt.After(retrieved.CreatedAt) {
		t.Error("UpdatedAt should be after CreatedAt")
	}
}

func BenchmarkInMemoryTenantRegistry_GetTenant(b *testing.B) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	// Register tenant
	tenant := &Tenant{
		ID:       "tenant-1",
		Name:     "Test Tenant",
		ShardID:  "shard-1",
		Status:   "active",
		Metadata: make(map[string]string),
	}
	registry.RegisterTenant(ctx, tenant)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.GetTenant(ctx, "tenant-1")
	}
}

func BenchmarkInMemoryTenantRegistry_ListTenants(b *testing.B) {
	registry := NewInMemoryTenantRegistry()
	ctx := context.Background()

	// Register 100 tenants
	for i := 0; i < 100; i++ {
		tenant := &Tenant{
			ID:       string(rune('0' + i%10)) + string(rune('a' + i/10)),
			Name:     "Tenant " + string(rune('0'+i)),
			ShardID:  "shard-" + string(rune('0'+i%5)),
			Status:   "active",
			Metadata: make(map[string]string),
		}
		registry.RegisterTenant(ctx, tenant)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.ListTenants(ctx)
	}
}

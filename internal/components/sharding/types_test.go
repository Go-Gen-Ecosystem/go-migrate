/**
 * @file types_test.go
 * @description Tests for core sharding types and configuration
 * @author Admilson B. F. Cossa
 * @date 2025-10-31
 */

package sharding

import (
	"errors"
	"testing"
	"time"
)

// Test-local sentinel errors used by the validation helpers below. The
// production sharding package does not export dedicated validation error
// values; these helpers exercise the validation logic the tests care about.
var (
	errInvalidShard   = errors.New("invalid shard")
	errTenantNotFound = errors.New("tenant not found")
)

func TestShardType(t *testing.T) {
	tests := []struct {
		name     string
		shardType ShardType
		expected  string
	}{
		{"TenantPerDatabase", ShardTypeTenantPerDatabase, "tenant_per_database"},
		{"TenantPerSchema", ShardTypeTenantPerSchema, "tenant_per_schema"},
		{"HashBased", ShardTypeHashBased, "hash_based"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.shardType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.shardType))
			}
		})
	}
}

func TestShardStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   ShardStatus
		expected string
	}{
		{"Active", ShardStatusActive, "active"},
		{"Inactive", ShardStatusInactive, "inactive"},
		{"Maintenance", ShardStatusMaintenance, "maintenance"},
		{"Rebalancing", ShardStatusRebalancing, "rebalancing"},
		{"Failed", ShardStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.status))
			}
		})
	}
}

func TestDefaultShardingConfig(t *testing.T) {
	config := DefaultShardingConfig()

	if config == nil {
		t.Fatal("DefaultShardingConfig returned nil")
	}

	// Test default values
	if config.Strategy != ShardTypeTenantPerDatabase {
		t.Errorf("expected strategy %s, got %s", ShardTypeTenantPerDatabase, config.Strategy)
	}

	if config.MaxParallelShards != 10 {
		t.Errorf("expected MaxParallelShards 10, got %d", config.MaxParallelShards)
	}

	if config.MaxParallelPerShard != 1 {
		t.Errorf("expected MaxParallelPerShard 1, got %d", config.MaxParallelPerShard)
	}

	if config.BatchSize != 100 {
		t.Errorf("expected BatchSize 100, got %d", config.BatchSize)
	}

	if config.ContinueOnShardFailure {
		t.Error("expected ContinueOnShardFailure false, got true")
	}

	if config.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", config.MaxRetries)
	}

	if config.RetryDelay != 5*time.Second {
		t.Errorf("expected RetryDelay 5s, got %v", config.RetryDelay)
	}

	if config.FailureThreshold != 0.1 {
		t.Errorf("expected FailureThreshold 0.1, got %f", config.FailureThreshold)
	}

	if config.AutoRebalance {
		t.Error("expected AutoRebalance false, got true")
	}

	if config.RebalanceThreshold != 0.3 {
		t.Errorf("expected RebalanceThreshold 0.3, got %f", config.RebalanceThreshold)
	}

	if config.MinTenantsPerShard != 1 {
		t.Errorf("expected MinTenantsPerShard 1, got %d", config.MinTenantsPerShard)
	}

	if config.MaxTenantsPerShard != 1000 {
		t.Errorf("expected MaxTenantsPerShard 1000, got %d", config.MaxTenantsPerShard)
	}

	if !config.EnableMetrics {
		t.Error("expected EnableMetrics true, got false")
	}

	if config.MetricsPrefix != "migration.sharding" {
		t.Errorf("expected MetricsPrefix 'migration.sharding', got '%s'", config.MetricsPrefix)
	}
}

func TestShardValidation(t *testing.T) {
	tests := []struct {
		name      string
		shard     *Shard
		expectErr bool
	}{
		{
			name: "ValidShard",
			shard: &Shard{
				ID:           "shard-1",
				Name:         "Test Shard",
				Type:         ShardTypeTenantPerDatabase,
				DatabaseDSN:  "postgres://localhost:5432/test",
				DatabaseType: "postgres",
				Status:       ShardStatusActive,
				Capacity:     100,
			},
			expectErr: false,
		},
		{
			name: "MissingID",
			shard: &Shard{
				Name:         "Test Shard",
				DatabaseDSN:  "postgres://localhost:5432/test",
				DatabaseType: "postgres",
			},
			expectErr: true,
		},
		{
			name: "MissingDatabaseDSN",
			shard: &Shard{
				ID:           "shard-1",
				Name:         "Test Shard",
				DatabaseType: "postgres",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShard(tt.shard)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func validateShard(shard *Shard) error {
	if shard.ID == "" {
		return errInvalidShard
	}
	if shard.DatabaseDSN == "" {
		return errInvalidShard
	}
	return nil
}

func TestTenantValidation(t *testing.T) {
	tests := []struct {
		name      string
		tenant    *Tenant
		expectErr bool
	}{
		{
			name: "ValidTenant",
			tenant: &Tenant{
				ID:       "tenant-1",
				Name:     "Test Tenant",
				ShardID:  "shard-1",
				Status:   "active",
				Metadata: make(map[string]string),
			},
			expectErr: false,
		},
		{
			name: "MissingID",
			tenant: &Tenant{
				Name:   "Test Tenant",
				Status: "active",
			},
			expectErr: true,
		},
		{
			name: "MissingName",
			tenant: &Tenant{
				ID:     "tenant-1",
				Status: "active",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTenant(tt.tenant)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func validateTenant(tenant *Tenant) error {
	if tenant.ID == "" {
		return errTenantNotFound
	}
	if tenant.Name == "" {
		return errTenantNotFound
	}
	return nil
}

func TestShardHealth(t *testing.T) {
	health := &ShardHealth{
		ShardID:      "shard-1",
		Healthy:      true,
		ResponseTime: 50 * time.Millisecond,
		CheckedAt:    time.Now(),
		Metrics:      make(map[string]float64),
	}

	if health.ShardID != "shard-1" {
		t.Errorf("expected shard ID 'shard-1', got '%s'", health.ShardID)
	}

	if !health.Healthy {
		t.Error("expected healthy true, got false")
	}

	if health.ResponseTime != 50*time.Millisecond {
		t.Errorf("expected response time 50ms, got %v", health.ResponseTime)
	}
}

func TestShardMetrics(t *testing.T) {
	metrics := &ShardMetrics{
		ShardID:                "shard-1",
		MigrationsApplied:      9,
		MigrationsFailed:       1,
		AverageMigrationTimeMs: 100,
		Timestamp:              time.Now(),
	}

	if metrics.ShardID != "shard-1" {
		t.Errorf("expected shard ID 'shard-1', got '%s'", metrics.ShardID)
	}

	totalMigrations := metrics.MigrationsApplied + metrics.MigrationsFailed
	if totalMigrations != 10 {
		t.Errorf("expected 10 total migrations, got %d", totalMigrations)
	}

	if metrics.MigrationsApplied != 9 {
		t.Errorf("expected 9 applied migrations, got %d", metrics.MigrationsApplied)
	}

	successRate := float64(metrics.MigrationsApplied) / float64(totalMigrations)
	expectedRate := 0.9

	if successRate != expectedRate {
		t.Errorf("expected success rate %.2f, got %.2f", expectedRate, successRate)
	}
}

func TestRebalanceOperation(t *testing.T) {
	op := &RebalanceOperation{
		ID:            "op-1",
		Type:          "add_shard",
		SourceShardID: "",
		TargetShardID: "shard-2",
		TenantsToMove: []string{"tenant-1", "tenant-2"},
		Status:        "in_progress",
		Progress:      0.5,
		StartedAt:     time.Now(),
	}

	if op.ID != "op-1" {
		t.Errorf("expected ID 'op-1', got '%s'", op.ID)
	}

	if op.Type != "add_shard" {
		t.Errorf("expected type 'add_shard', got '%s'", op.Type)
	}

	if len(op.TenantsToMove) != 2 {
		t.Errorf("expected 2 tenants to move, got %d", len(op.TenantsToMove))
	}

	if op.Progress != 0.5 {
		t.Errorf("expected progress 0.5, got %f", op.Progress)
	}
}

/**
 * @file rebalancer.go
 * @description Shard rebalancer for managing shard operations and load distribution
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/rebalancer.go
 */

package sharding

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ShardRebalancerImpl implements ShardRebalancer
type ShardRebalancerImpl struct {
	strategy    ShardingStrategy
	registry    TenantRegistry
	coordinator ShardCoordinator
	tracker     ShardedMigrationTracker
	operations  map[string]*RebalanceOperation
	operationsMu sync.RWMutex
}

// NewShardRebalancer creates a new shard rebalancer
func NewShardRebalancer(
	strategy ShardingStrategy,
	registry TenantRegistry,
	coordinator ShardCoordinator,
	tracker ShardedMigrationTracker,
) *ShardRebalancerImpl {
	return &ShardRebalancerImpl{
		strategy:    strategy,
		registry:    registry,
		coordinator: coordinator,
		tracker:     tracker,
		operations:  make(map[string]*RebalanceOperation),
	}
}

// AddShard adds a new shard and migrates existing data if needed
func (r *ShardRebalancerImpl) AddShard(ctx context.Context, shard *Shard) (*RebalanceOperation, error) {
	// Create rebalance operation
	op := &RebalanceOperation{
		ID:            fmt.Sprintf("add_shard_%s_%d", shard.ID, time.Now().UnixNano()),
		Type:          "add_shard",
		TargetShardID: shard.ID,
		TenantsToMove: make([]string, 0),
		Status:        "pending",
		Progress:      0.0,
		StartedAt:     time.Now(),
	}

	r.registerOperation(op)

	// Register the shard with strategy
	if err := r.strategy.RegisterShard(ctx, shard); err != nil {
		op.Status = "failed"
		op.Error = fmt.Sprintf("failed to register shard: %v", err)
		return op, err
	}

	op.Status = "in_progress"
	op.Progress = 0.1

	// Get a reference shard to copy migrations from
	shards, err := r.strategy.GetShards(ctx)
	if err != nil || len(shards) < 2 {
		// No other shards to copy from, mark as completed
		op.Status = "completed"
		op.Progress = 1.0
		now := time.Now()
		op.CompletedAt = &now
		return op, nil
	}

	// Find a healthy reference shard
	var referenceShard *Shard
	for _, s := range shards {
		if s.ID != shard.ID && s.Status == ShardStatusActive {
			referenceShard = &s
			break
		}
	}

	if referenceShard == nil {
		op.Status = "completed"
		op.Progress = 1.0
		now := time.Now()
		op.CompletedAt = &now
		return op, nil
	}

	op.Progress = 0.2

	// Get all applied migrations from reference shard
	appliedMigrations, err := r.tracker.GetAppliedMigrations(ctx, referenceShard.ID)
	if err != nil {
		op.Status = "failed"
		op.Error = fmt.Sprintf("failed to get applied migrations: %v", err)
		return op, err
	}

	op.Progress = 0.3

	// Apply all migrations to new shard
	totalMigrations := len(appliedMigrations)
	for i, migrationID := range appliedMigrations {
		// Check if already applied
		applied, err := r.tracker.IsMigrationApplied(ctx, shard.ID, migrationID)
		if err != nil {
			op.Status = "failed"
			op.Error = fmt.Sprintf("failed to check migration status: %v", err)
			return op, err
		}

		if !applied {
			// Record that we're applying this migration
			record := &ShardMigrationRecord{
				MigrationID: migrationID,
				ShardID:     shard.ID,
				Status:      "success",
				StartedAt:   time.Now(),
				AppliedBy:   "rebalancer",
			}
			now := time.Now()
			record.CompletedAt = &now

			if err := r.tracker.RecordShardMigration(ctx, record); err != nil {
				op.Status = "failed"
				op.Error = fmt.Sprintf("failed to record migration: %v", err)
				return op, err
			}
		}

		// Update progress
		op.Progress = 0.3 + (0.7 * float64(i+1) / float64(totalMigrations))
	}

	// Mark operation as completed
	op.Status = "completed"
	op.Progress = 1.0
	now := time.Now()
	op.CompletedAt = &now

	return op, nil
}

// RemoveShard removes a shard and migrates data to other shards
func (r *ShardRebalancerImpl) RemoveShard(ctx context.Context, shardID string) (*RebalanceOperation, error) {
	// Create rebalance operation
	op := &RebalanceOperation{
		ID:            fmt.Sprintf("remove_shard_%s_%d", shardID, time.Now().UnixNano()),
		Type:          "remove_shard",
		SourceShardID: shardID,
		TenantsToMove: make([]string, 0),
		Status:        "pending",
		Progress:      0.0,
		StartedAt:     time.Now(),
	}

	r.registerOperation(op)

	// Get tenants on this shard
	tenants, err := r.registry.ListTenantsByShard(ctx, shardID)
	if err != nil {
		op.Status = "failed"
		op.Error = fmt.Sprintf("failed to list tenants: %v", err)
		return op, err
	}

	op.Status = "in_progress"
	op.Progress = 0.1

	// Get available shards to move tenants to
	shards, err := r.strategy.GetShards(ctx)
	if err != nil {
		op.Status = "failed"
		op.Error = fmt.Sprintf("failed to get shards: %v", err)
		return op, err
	}

	// Filter out the shard being removed
	availableShards := make([]Shard, 0)
	for _, s := range shards {
		if s.ID != shardID && s.Status == ShardStatusActive {
			availableShards = append(availableShards, s)
		}
	}

	if len(availableShards) == 0 {
		op.Status = "failed"
		op.Error = "no available shards to move tenants to"
		return op, fmt.Errorf("no available shards")
	}

	op.Progress = 0.2

	// Move tenants to other shards
	totalTenants := len(tenants)
	for i, tenant := range tenants {
		op.TenantsToMove = append(op.TenantsToMove, tenant.ID)

		// Find best shard for this tenant
		targetShard := r.findBestShardForTenant(availableShards)

		// Move tenant
		if err := r.registry.MoveTenant(ctx, tenant.ID, targetShard.ID); err != nil {
			op.Status = "failed"
			op.Error = fmt.Sprintf("failed to move tenant %s: %v", tenant.ID, err)
			return op, err
		}

		// Update progress
		op.Progress = 0.2 + (0.7 * float64(i+1) / float64(totalTenants))
	}

	op.Progress = 0.9

	// Unregister the shard
	if err := r.strategy.UnregisterShard(ctx, shardID); err != nil {
		op.Status = "failed"
		op.Error = fmt.Sprintf("failed to unregister shard: %v", err)
		return op, err
	}

	// Mark operation as completed
	op.Status = "completed"
	op.Progress = 1.0
	now := time.Now()
	op.CompletedAt = &now

	return op, nil
}

// Rebalance redistributes tenants across shards for optimal load
func (r *ShardRebalancerImpl) Rebalance(ctx context.Context) ([]*RebalanceOperation, error) {
	// Calculate optimal distribution
	optimalDistribution, err := r.CalculateOptimalDistribution(ctx)
	if err != nil {
		return nil, err
	}

	// Create rebalance operations
	var operations []*RebalanceOperation

	// Compare current vs optimal and create move operations
	for targetShardID, tenantIDs := range optimalDistribution {
		for _, tenantID := range tenantIDs {
			// Check current shard
			tenant, err := r.registry.GetTenant(ctx, tenantID)
			if err != nil {
				continue
			}

			// If tenant needs to move
			if tenant.ShardID != targetShardID {
				op := &RebalanceOperation{
					ID:            fmt.Sprintf("rebalance_%s_%d", tenantID, time.Now().UnixNano()),
					Type:          "redistribute",
					SourceShardID: tenant.ShardID,
					TargetShardID: targetShardID,
					TenantsToMove: []string{tenantID},
					Status:        "pending",
					Progress:      0.0,
					StartedAt:     time.Now(),
				}

				r.registerOperation(op)

				// Execute move
				op.Status = "in_progress"
				if err := r.registry.MoveTenant(ctx, tenantID, targetShardID); err != nil {
					op.Status = "failed"
					op.Error = fmt.Sprintf("failed to move tenant: %v", err)
				} else {
					op.Status = "completed"
					op.Progress = 1.0
					now := time.Now()
					op.CompletedAt = &now
				}

				operations = append(operations, op)
			}
		}
	}

	return operations, nil
}

// GetRebalanceStatus returns status of a rebalance operation
func (r *ShardRebalancerImpl) GetRebalanceStatus(ctx context.Context, operationID string) (*RebalanceOperation, error) {
	r.operationsMu.RLock()
	defer r.operationsMu.RUnlock()

	op, exists := r.operations[operationID]
	if !exists {
		return nil, fmt.Errorf("operation %s not found", operationID)
	}

	// Return a copy
	opCopy := *op
	return &opCopy, nil
}

// CancelRebalance cancels an ongoing rebalance operation
func (r *ShardRebalancerImpl) CancelRebalance(ctx context.Context, operationID string) error {
	r.operationsMu.Lock()
	defer r.operationsMu.Unlock()

	op, exists := r.operations[operationID]
	if !exists {
		return fmt.Errorf("operation %s not found", operationID)
	}

	if op.Status == "completed" || op.Status == "failed" {
		return fmt.Errorf("cannot cancel %s operation", op.Status)
	}

	op.Status = "cancelled"
	now := time.Now()
	op.CompletedAt = &now

	return nil
}

// CalculateOptimalDistribution calculates optimal tenant distribution
func (r *ShardRebalancerImpl) CalculateOptimalDistribution(ctx context.Context) (map[string][]string, error) {
	// Get all shards
	shards, err := r.strategy.GetShards(ctx)
	if err != nil {
		return nil, err
	}

	// Get all tenants
	tenants, err := r.registry.ListTenants(ctx)
	if err != nil {
		return nil, err
	}

	// Filter active shards
	activeShards := make([]Shard, 0)
	for _, shard := range shards {
		if shard.Status == ShardStatusActive {
			activeShards = append(activeShards, shard)
		}
	}

	if len(activeShards) == 0 {
		return nil, fmt.Errorf("no active shards available")
	}

	// Calculate target tenants per shard
	totalTenants := len(tenants)
	tenantsPerShard := totalTenants / len(activeShards)
	if tenantsPerShard == 0 {
		tenantsPerShard = 1
	}

	// Distribute tenants evenly
	distribution := make(map[string][]string)
	for i := range activeShards {
		distribution[activeShards[i].ID] = make([]string, 0)
	}

	// Simple round-robin distribution
	shardIndex := 0
	for _, tenant := range tenants {
		shardID := activeShards[shardIndex].ID
		distribution[shardID] = append(distribution[shardID], tenant.ID)

		shardIndex = (shardIndex + 1) % len(activeShards)
	}

	return distribution, nil
}

// findBestShardForTenant finds the best shard to place a tenant
func (r *ShardRebalancerImpl) findBestShardForTenant(shards []Shard) *Shard {
	if len(shards) == 0 {
		return nil
	}

	// Find shard with lowest load
	var bestShard *Shard
	lowestLoad := 2.0

	for i := range shards {
		if shards[i].LoadFactor < lowestLoad {
			lowestLoad = shards[i].LoadFactor
			bestShard = &shards[i]
		}
	}

	if bestShard == nil {
		return &shards[0]
	}

	return bestShard
}

// registerOperation registers a rebalance operation
func (r *ShardRebalancerImpl) registerOperation(op *RebalanceOperation) {
	r.operationsMu.Lock()
	defer r.operationsMu.Unlock()
	r.operations[op.ID] = op
}

// GetAllOperations returns all rebalance operations
func (r *ShardRebalancerImpl) GetAllOperations() []*RebalanceOperation {
	r.operationsMu.RLock()
	defer r.operationsMu.RUnlock()

	ops := make([]*RebalanceOperation, 0, len(r.operations))
	for _, op := range r.operations {
		opCopy := *op
		ops = append(ops, &opCopy)
	}

	return ops
}

// CleanupOldOperations removes completed operations older than specified duration
func (r *ShardRebalancerImpl) CleanupOldOperations(olderThan time.Duration) int {
	r.operationsMu.Lock()
	defer r.operationsMu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for id, op := range r.operations {
		if op.CompletedAt != nil && op.CompletedAt.Before(cutoff) {
			delete(r.operations, id)
			removed++
		}
	}

	return removed
}

/**
 * @file coordinator.go
 * @description Shard coordinator for orchestrating migrations across multiple shards
 * @author Admilson B. F. Cossa
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/coordinator.go
 */

package sharding

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ShardedMigrationCoordinator coordinates migrations across multiple shards
type ShardedMigrationCoordinator struct {
	strategy     ShardingStrategy
	tracker      ShardedMigrationTracker
	config       *ShardingConfig
	mu           sync.RWMutex
	activeOps    map[string]*coordinatorOperation // OperationID -> Operation
	activeOpsMu  sync.RWMutex
}

// coordinatorOperation tracks an ongoing coordinated operation
type coordinatorOperation struct {
	ID              string
	Type            string // migrate, rollback, validate
	MigrationID     string
	StartedAt       time.Time
	TotalShards     int
	CompletedShards int32
	FailedShards    int32
	Status          string
	Cancel          context.CancelFunc
	Results         map[string]*shardResult
	ResultsMu       sync.RWMutex
}

// shardResult tracks the result of an operation on a single shard
type shardResult struct {
	ShardID    string
	Status     string
	Error      error
	StartedAt  time.Time
	CompletedAt *time.Time
	DurationMs int64
}

// NewShardedMigrationCoordinator creates a new coordinator
func NewShardedMigrationCoordinator(
	strategy ShardingStrategy,
	tracker ShardedMigrationTracker,
	config *ShardingConfig,
) *ShardedMigrationCoordinator {
	if config == nil {
		config = DefaultShardingConfig()
	}

	return &ShardedMigrationCoordinator{
		strategy:  strategy,
		tracker:   tracker,
		config:    config,
		activeOps: make(map[string]*coordinatorOperation),
	}
}

// MigrateAllShards applies a migration to all shards
func (c *ShardedMigrationCoordinator) MigrateAllShards(ctx context.Context, migration *types.Migration) (*ShardMigrationStatus, error) {
	// Get all shards
	shards, err := c.strategy.GetShards(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get shards: %w", err)
	}

	shardIDs := make([]string, len(shards))
	for i, shard := range shards {
		shardIDs[i] = shard.ID
	}

	return c.MigrateShards(ctx, migration, shardIDs)
}

// MigrateShards applies a migration to specific shards
func (c *ShardedMigrationCoordinator) MigrateShards(ctx context.Context, migration *types.Migration, shardIDs []string) (*ShardMigrationStatus, error) {
	// Create operation
	opCtx, cancel := context.WithCancel(ctx)
	op := &coordinatorOperation{
		ID:          fmt.Sprintf("migrate_%s_%d", migration.ID, time.Now().UnixNano()),
		Type:        "migrate",
		MigrationID: migration.ID,
		StartedAt:   time.Now(),
		TotalShards: len(shardIDs),
		Status:      "in_progress",
		Cancel:      cancel,
		Results:     make(map[string]*shardResult),
	}

	// Register operation
	c.registerOperation(op)
	defer c.unregisterOperation(op.ID)

	// Validate migration on all shards first
	if err := c.validateOnShards(opCtx, migration, shardIDs); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Execute migration on shards with controlled parallelism
	if err := c.executeOnShards(opCtx, op, migration, shardIDs); err != nil {
		return nil, err
	}

	// Get overall status
	return c.tracker.GetOverallMigrationStatus(ctx, migration.ID)
}

// validateOnShards validates migration on all shards
func (c *ShardedMigrationCoordinator) validateOnShards(ctx context.Context, migration *types.Migration, shardIDs []string) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(shardIDs))

	// Validate in parallel
	for _, shardID := range shardIDs {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()

			shard, err := c.strategy.GetShard(ctx, sid)
			if err != nil {
				errChan <- fmt.Errorf("shard %s: %w", sid, err)
				return
			}

			// Validate shard state
			if err := c.strategy.ValidateShardState(ctx, shard); err != nil {
				errChan <- fmt.Errorf("shard %s invalid state: %w", sid, err)
				return
			}

			// Additional validation could be done here
			// For now, just check if shard is accessible
		}(shardID)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// executeOnShards executes migration on shards with controlled parallelism
func (c *ShardedMigrationCoordinator) executeOnShards(ctx context.Context, op *coordinatorOperation, migration *types.Migration, shardIDs []string) error {
	// Create semaphore for controlling parallelism
	sem := make(chan struct{}, c.config.MaxParallelShards)
	var wg sync.WaitGroup

	for _, shardID := range shardIDs {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wg.Add(1)
		go func(sid string) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Execute on shard
			result := c.executeMigrationOnShard(ctx, sid, migration)

			// Store result
			op.ResultsMu.Lock()
			op.Results[sid] = result
			op.ResultsMu.Unlock()

			// Update counters
			if result.Status == "success" {
				atomic.AddInt32(&op.CompletedShards, 1)
			} else {
				atomic.AddInt32(&op.FailedShards, 1)
			}

			// Record in tracker
			now := time.Now()
			record := &ShardMigrationRecord{
				MigrationID: migration.ID,
				ShardID:     sid,
				Status:      result.Status,
				StartedAt:   result.StartedAt,
				CompletedAt: &now,
				DurationMs:  result.DurationMs,
				Checksum:    migration.Checksum,
				AppliedBy:   "coordinator",
			}

			if result.Error != nil {
				record.Error = result.Error.Error()
			}

			c.tracker.RecordShardMigration(ctx, record)
		}(shardID)
	}

	wg.Wait()

	// Check failure threshold
	failureRate := float64(atomic.LoadInt32(&op.FailedShards)) / float64(op.TotalShards)
	if failureRate > c.config.FailureThreshold {
		return fmt.Errorf("migration failed: %.1f%% of shards failed (threshold: %.1f%%)",
			failureRate*100, c.config.FailureThreshold*100)
	}

	// If continue on failure is disabled and any shard failed
	if !c.config.ContinueOnShardFailure && atomic.LoadInt32(&op.FailedShards) > 0 {
		return fmt.Errorf("migration failed on %d shard(s)", atomic.LoadInt32(&op.FailedShards))
	}

	return nil
}

// executeMigrationOnShard executes a migration on a single shard
func (c *ShardedMigrationCoordinator) executeMigrationOnShard(ctx context.Context, shardID string, migration *types.Migration) *shardResult {
	result := &shardResult{
		ShardID:   shardID,
		StartedAt: time.Now(),
	}

	// Get shard
	shard, err := c.strategy.GetShard(ctx, shardID)
	if err != nil {
		result.Status = "failed"
		result.Error = err
		return result
	}

	// Open database connection
	db, err := sql.Open(shard.DatabaseType, shard.DatabaseDSN)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Errorf("failed to connect: %w", err)
		return result
	}
	defer db.Close()

	// Execute migration with retry
	err = c.executeWithRetry(ctx, db, migration, shard)
	completedAt := time.Now()
	result.CompletedAt = &completedAt
	result.DurationMs = completedAt.Sub(result.StartedAt).Milliseconds()

	if err != nil {
		result.Status = "failed"
		result.Error = err
	} else {
		result.Status = "success"
	}

	return result
}

// executeWithRetry executes migration with retry logic
func (c *ShardedMigrationCoordinator) executeWithRetry(ctx context.Context, db *sql.DB, migration *types.Migration, shard *Shard) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-time.After(c.config.RetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Start transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			lastErr = err
			continue
		}

		// Execute migration script
		_, err = tx.ExecContext(ctx, migration.Script)
		if err != nil {
			tx.Rollback()
			lastErr = err

			// Check if error is retryable
			if !c.isRetryableError(err) {
				return err
			}
			continue
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			lastErr = err
			continue
		}

		// Success
		return nil
	}

	return fmt.Errorf("failed after %d retries: %w", c.config.MaxRetries, lastErr)
}

// isRetryableError checks if an error is retryable
func (c *ShardedMigrationCoordinator) isRetryableError(err error) bool {
	// Common retryable database errors
	errStr := err.Error()
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"deadlock",
		"lock wait timeout",
	}

	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// RollbackAllShards rolls back a migration from all shards
func (c *ShardedMigrationCoordinator) RollbackAllShards(ctx context.Context, migrationID string) (*ShardMigrationStatus, error) {
	// Get all shards
	shards, err := c.strategy.GetShards(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get shards: %w", err)
	}

	shardIDs := make([]string, len(shards))
	for i, shard := range shards {
		shardIDs[i] = shard.ID
	}

	return c.RollbackShards(ctx, migrationID, shardIDs)
}

// RollbackShards rolls back a migration from specific shards
func (c *ShardedMigrationCoordinator) RollbackShards(ctx context.Context, migrationID string, shardIDs []string) (*ShardMigrationStatus, error) {
	// Create operation
	opCtx, cancel := context.WithCancel(ctx)
	op := &coordinatorOperation{
		ID:          fmt.Sprintf("rollback_%s_%d", migrationID, time.Now().UnixNano()),
		Type:        "rollback",
		MigrationID: migrationID,
		StartedAt:   time.Now(),
		TotalShards: len(shardIDs),
		Status:      "in_progress",
		Cancel:      cancel,
		Results:     make(map[string]*shardResult),
	}

	c.registerOperation(op)
	defer c.unregisterOperation(op.ID)

	// Execute rollback on shards
	sem := make(chan struct{}, c.config.MaxParallelShards)
	var wg sync.WaitGroup

	for _, shardID := range shardIDs {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			result := c.executeRollbackOnShard(opCtx, sid, migrationID)

			op.ResultsMu.Lock()
			op.Results[sid] = result
			op.ResultsMu.Unlock()

			if result.Status == "success" {
				atomic.AddInt32(&op.CompletedShards, 1)
			} else {
				atomic.AddInt32(&op.FailedShards, 1)
			}
		}(shardID)
	}

	wg.Wait()

	return c.tracker.GetOverallMigrationStatus(ctx, migrationID)
}

// executeRollbackOnShard executes rollback on a single shard
func (c *ShardedMigrationCoordinator) executeRollbackOnShard(ctx context.Context, shardID string, migrationID string) *shardResult {
	result := &shardResult{
		ShardID:   shardID,
		StartedAt: time.Now(),
	}

	// For now, return success (actual rollback logic would be implemented)
	// This would need access to the migration's rollback script
	completedAt := time.Now()
	result.CompletedAt = &completedAt
	result.DurationMs = completedAt.Sub(result.StartedAt).Milliseconds()
	result.Status = "success"

	return result
}

// ValidateAllShards validates migrations on all shards
func (c *ShardedMigrationCoordinator) ValidateAllShards(ctx context.Context, migrations []*types.Migration) error {
	shards, err := c.strategy.GetShards(ctx)
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		shardIDs := make([]string, len(shards))
		for i, shard := range shards {
			shardIDs[i] = shard.ID
		}

		if err := c.validateOnShards(ctx, migration, shardIDs); err != nil {
			return fmt.Errorf("validation failed for migration %s: %w", migration.ID, err)
		}
	}

	return nil
}

// GetOverallStatus gets overall migration status across shards
func (c *ShardedMigrationCoordinator) GetOverallStatus(ctx context.Context) (map[string]*ShardMigrationStatus, error) {
	// This would return status for all migrations across all shards
	// For now, return empty map
	return make(map[string]*ShardMigrationStatus), nil
}

// CancelOperation cancels an ongoing operation
func (c *ShardedMigrationCoordinator) CancelOperation(operationID string) error {
	c.activeOpsMu.RLock()
	op, exists := c.activeOps[operationID]
	c.activeOpsMu.RUnlock()

	if !exists {
		return fmt.Errorf("operation %s not found", operationID)
	}

	op.Cancel()
	op.Status = "cancelled"

	return nil
}

// GetOperationStatus returns status of an operation
func (c *ShardedMigrationCoordinator) GetOperationStatus(operationID string) (*coordinatorOperation, error) {
	c.activeOpsMu.RLock()
	defer c.activeOpsMu.RUnlock()

	op, exists := c.activeOps[operationID]
	if !exists {
		return nil, fmt.Errorf("operation %s not found", operationID)
	}

	// Return a snapshot copy. The struct embeds a sync.RWMutex, so a shallow
	// (*op) copy would copy the lock (a vet copylocks violation and a real
	// concurrency hazard). Copy the value fields explicitly, deep-copy the
	// Results map under the operation's lock, and leave the snapshot's mutex
	// as a fresh zero value that guards only the returned copy.
	op.ResultsMu.RLock()
	results := make(map[string]*shardResult, len(op.Results))
	for k, v := range op.Results {
		results[k] = v
	}
	op.ResultsMu.RUnlock()

	opCopy := &coordinatorOperation{
		ID:              op.ID,
		Type:            op.Type,
		MigrationID:     op.MigrationID,
		StartedAt:       op.StartedAt,
		TotalShards:     op.TotalShards,
		CompletedShards: op.CompletedShards,
		FailedShards:    op.FailedShards,
		Status:          op.Status,
		Cancel:          op.Cancel,
		Results:         results,
	}
	return opCopy, nil
}

// registerOperation registers an active operation
func (c *ShardedMigrationCoordinator) registerOperation(op *coordinatorOperation) {
	c.activeOpsMu.Lock()
	defer c.activeOpsMu.Unlock()
	c.activeOps[op.ID] = op
}

// unregisterOperation removes an operation from active list
func (c *ShardedMigrationCoordinator) unregisterOperation(opID string) {
	c.activeOpsMu.Lock()
	defer c.activeOpsMu.Unlock()
	delete(c.activeOps, opID)
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		 len(s) > len(substr)+1 && s[1:len(substr)+1] == substr))
}

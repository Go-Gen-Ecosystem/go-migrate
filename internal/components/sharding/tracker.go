/**
 * @file tracker.go
 * @description Sharded migration tracker for tracking migrations across multiple shards
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/tracker.go
 */

package sharding

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// DatabaseShardedMigrationTracker implements ShardedMigrationTracker using database backend
type DatabaseShardedMigrationTracker struct {
	db        *sql.DB
	tableName string
	mu        sync.RWMutex
}

// NewDatabaseShardedMigrationTracker creates a new database-backed sharded migration tracker
func NewDatabaseShardedMigrationTracker(db *sql.DB, tableName string) *DatabaseShardedMigrationTracker {
	if tableName == "" {
		tableName = "shard_migration_tracking"
	}

	return &DatabaseShardedMigrationTracker{
		db:        db,
		tableName: tableName,
	}
}

// EnsureTable creates the migration tracking table if it doesn't exist
func (t *DatabaseShardedMigrationTracker) EnsureTable(ctx context.Context) error {
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			migration_id VARCHAR(255) NOT NULL,
			shard_id VARCHAR(255) NOT NULL,
			tenant_id VARCHAR(255),
			status VARCHAR(50) NOT NULL,
			started_at TIMESTAMP NOT NULL,
			completed_at TIMESTAMP,
			duration_ms BIGINT,
			error TEXT,
			checksum VARCHAR(64),
			applied_by VARCHAR(255),
			INDEX idx_migration_id (migration_id),
			INDEX idx_shard_id (shard_id),
			INDEX idx_status (status),
			INDEX idx_composite (migration_id, shard_id),
			UNIQUE KEY uk_migration_shard (migration_id, shard_id, tenant_id)
		)
	`, t.tableName)

	_, err := t.db.ExecContext(ctx, createSQL)
	return err
}

// RecordShardMigration records a migration execution for a shard
func (t *DatabaseShardedMigrationTracker) RecordShardMigration(ctx context.Context, record *ShardMigrationRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Generate ID if not provided
	if record.ID == "" {
		record.ID = fmt.Sprintf("%s_%s_%d", record.MigrationID, record.ShardID, time.Now().UnixNano())
	}

	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, migration_id, shard_id, tenant_id, status,
		                started_at, completed_at, duration_ms, error, checksum, applied_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			completed_at = VALUES(completed_at),
			duration_ms = VALUES(duration_ms),
			error = VALUES(error)
	`, t.tableName)

	var completedAt sql.NullTime
	if record.CompletedAt != nil {
		completedAt = sql.NullTime{Time: *record.CompletedAt, Valid: true}
	}

	var errorMsg sql.NullString
	if record.Error != "" {
		errorMsg = sql.NullString{String: record.Error, Valid: true}
	}

	var tenantID sql.NullString
	if record.TenantID != "" {
		tenantID = sql.NullString{String: record.TenantID, Valid: true}
	}

	_, err := t.db.ExecContext(ctx, insertSQL,
		record.ID,
		record.MigrationID,
		record.ShardID,
		tenantID,
		record.Status,
		record.StartedAt,
		completedAt,
		record.DurationMs,
		errorMsg,
		record.Checksum,
		record.AppliedBy,
	)

	return err
}

// GetShardMigrationStatus gets migration status for a specific shard
func (t *DatabaseShardedMigrationTracker) GetShardMigrationStatus(ctx context.Context, shardID string, migrationID string) (*ShardMigrationRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	query := fmt.Sprintf(`
		SELECT id, migration_id, shard_id, tenant_id, status,
		       started_at, completed_at, duration_ms, error, checksum, applied_by
		FROM %s
		WHERE shard_id = ? AND migration_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, t.tableName)

	var record ShardMigrationRecord
	var completedAt sql.NullTime
	var errorMsg sql.NullString
	var tenantID sql.NullString

	err := t.db.QueryRowContext(ctx, query, shardID, migrationID).Scan(
		&record.ID,
		&record.MigrationID,
		&record.ShardID,
		&tenantID,
		&record.Status,
		&record.StartedAt,
		&completedAt,
		&record.DurationMs,
		&errorMsg,
		&record.Checksum,
		&record.AppliedBy,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("migration %s not found on shard %s", migrationID, shardID)
		}
		return nil, err
	}

	if completedAt.Valid {
		record.CompletedAt = &completedAt.Time
	}

	if errorMsg.Valid {
		record.Error = errorMsg.String
	}

	if tenantID.Valid {
		record.TenantID = tenantID.String
	}

	return &record, nil
}

// GetOverallMigrationStatus gets aggregated status across all shards
func (t *DatabaseShardedMigrationTracker) GetOverallMigrationStatus(ctx context.Context, migrationID string) (*ShardMigrationStatus, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Get all records for this migration
	query := fmt.Sprintf(`
		SELECT id, migration_id, shard_id, tenant_id, status,
		       started_at, completed_at, duration_ms, error, checksum, applied_by
		FROM %s
		WHERE migration_id = ?
		ORDER BY started_at
	`, t.tableName)

	rows, err := t.db.QueryContext(ctx, query, migrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	status := &ShardMigrationStatus{
		MigrationID:   migrationID,
		ShardStatuses: make(map[string]*ShardMigrationRecord),
	}

	var earliestStart time.Time
	var latestCompletion *time.Time

	for rows.Next() {
		var record ShardMigrationRecord
		var completedAt sql.NullTime
		var errorMsg sql.NullString
		var tenantID sql.NullString

		err := rows.Scan(
			&record.ID,
			&record.MigrationID,
			&record.ShardID,
			&tenantID,
			&record.Status,
			&record.StartedAt,
			&completedAt,
			&record.DurationMs,
			&errorMsg,
			&record.Checksum,
			&record.AppliedBy,
		)

		if err != nil {
			return nil, err
		}

		if completedAt.Valid {
			record.CompletedAt = &completedAt.Time
		}

		if errorMsg.Valid {
			record.Error = errorMsg.String
		}

		if tenantID.Valid {
			record.TenantID = tenantID.String
		}

		// Track statuses
		switch record.Status {
		case "success", "completed":
			status.SuccessCount++
		case "failed", "error":
			status.FailedCount++
		case "pending":
			status.PendingCount++
		case "running", "in_progress":
			status.InProgressCount++
		}

		status.ShardStatuses[record.ShardID] = &record
		status.TotalShards++

		// Track overall timing
		if earliestStart.IsZero() || record.StartedAt.Before(earliestStart) {
			earliestStart = record.StartedAt
		}

		if record.CompletedAt != nil {
			if latestCompletion == nil || record.CompletedAt.After(*latestCompletion) {
				latestCompletion = record.CompletedAt
			}
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	status.StartedAt = earliestStart
	status.CompletedAt = latestCompletion

	// Determine overall status
	if status.FailedCount > 0 {
		status.OverallStatus = "failed"
	} else if status.InProgressCount > 0 || status.PendingCount > 0 {
		status.OverallStatus = "in_progress"
	} else if status.SuccessCount == status.TotalShards {
		status.OverallStatus = "success"
	} else {
		status.OverallStatus = "partial"
	}

	return status, nil
}

// GetShardMigrationHistory gets migration history for a shard
func (t *DatabaseShardedMigrationTracker) GetShardMigrationHistory(ctx context.Context, shardID string) ([]*ShardMigrationRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	query := fmt.Sprintf(`
		SELECT id, migration_id, shard_id, tenant_id, status,
		       started_at, completed_at, duration_ms, error, checksum, applied_by
		FROM %s
		WHERE shard_id = ?
		ORDER BY started_at DESC
	`, t.tableName)

	rows, err := t.db.QueryContext(ctx, query, shardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*ShardMigrationRecord

	for rows.Next() {
		var record ShardMigrationRecord
		var completedAt sql.NullTime
		var errorMsg sql.NullString
		var tenantID sql.NullString

		err := rows.Scan(
			&record.ID,
			&record.MigrationID,
			&record.ShardID,
			&tenantID,
			&record.Status,
			&record.StartedAt,
			&completedAt,
			&record.DurationMs,
			&errorMsg,
			&record.Checksum,
			&record.AppliedBy,
		)

		if err != nil {
			return nil, err
		}

		if completedAt.Valid {
			record.CompletedAt = &completedAt.Time
		}

		if errorMsg.Valid {
			record.Error = errorMsg.String
		}

		if tenantID.Valid {
			record.TenantID = tenantID.String
		}

		records = append(records, &record)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

// GetAppliedMigrations returns list of applied migrations for a shard
func (t *DatabaseShardedMigrationTracker) GetAppliedMigrations(ctx context.Context, shardID string) ([]string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	query := fmt.Sprintf(`
		SELECT DISTINCT migration_id
		FROM %s
		WHERE shard_id = ? AND status IN ('success', 'completed')
		ORDER BY migration_id
	`, t.tableName)

	rows, err := t.db.QueryContext(ctx, query, shardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrationIDs []string

	for rows.Next() {
		var migrationID string
		if err := rows.Scan(&migrationID); err != nil {
			return nil, err
		}
		migrationIDs = append(migrationIDs, migrationID)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return migrationIDs, nil
}

// IsMigrationApplied checks if migration is applied to a shard
func (t *DatabaseShardedMigrationTracker) IsMigrationApplied(ctx context.Context, shardID string, migrationID string) (bool, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s
		WHERE shard_id = ? AND migration_id = ? AND status IN ('success', 'completed')
	`, t.tableName)

	var count int
	err := t.db.QueryRowContext(ctx, query, shardID, migrationID).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetPendingMigrations returns migrations that haven't been applied to a shard
func (t *DatabaseShardedMigrationTracker) GetPendingMigrations(ctx context.Context, shardID string, allMigrations []string) ([]string, error) {
	applied, err := t.GetAppliedMigrations(ctx, shardID)
	if err != nil {
		return nil, err
	}

	appliedSet := make(map[string]bool)
	for _, migID := range applied {
		appliedSet[migID] = true
	}

	var pending []string
	for _, migID := range allMigrations {
		if !appliedSet[migID] {
			pending = append(pending, migID)
		}
	}

	return pending, nil
}

// GetMigrationStats returns migration statistics across all shards
func (t *DatabaseShardedMigrationTracker) GetMigrationStats(ctx context.Context, migrationID string) (map[string]interface{}, error) {
	status, err := t.GetOverallMigrationStatus(ctx, migrationID)
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"total_shards":      status.TotalShards,
		"success_count":     status.SuccessCount,
		"failed_count":      status.FailedCount,
		"pending_count":     status.PendingCount,
		"in_progress_count": status.InProgressCount,
		"overall_status":    status.OverallStatus,
		"started_at":        status.StartedAt,
		"completed_at":      status.CompletedAt,
	}

	// Calculate average duration for completed migrations
	var totalDuration int64
	completedCount := 0

	for _, record := range status.ShardStatuses {
		if record.Status == "success" || record.Status == "completed" {
			totalDuration += record.DurationMs
			completedCount++
		}
	}

	if completedCount > 0 {
		stats["avg_duration_ms"] = totalDuration / int64(completedCount)
	}

	return stats, nil
}

// CleanupOldRecords removes migration records older than specified duration
func (t *DatabaseShardedMigrationTracker) CleanupOldRecords(ctx context.Context, olderThan time.Duration) (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoffTime := time.Now().Add(-olderThan)

	deleteSQL := fmt.Sprintf(`
		DELETE FROM %s
		WHERE completed_at < ? AND status IN ('success', 'completed')
	`, t.tableName)

	result, err := t.db.ExecContext(ctx, deleteSQL, cutoffTime)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}

// InMemoryShardedMigrationTracker implements ShardedMigrationTracker using in-memory storage
type InMemoryShardedMigrationTracker struct {
	records map[string]*ShardMigrationRecord // RecordID -> Record
	mu      sync.RWMutex
}

// NewInMemoryShardedMigrationTracker creates a new in-memory tracker
func NewInMemoryShardedMigrationTracker() *InMemoryShardedMigrationTracker {
	return &InMemoryShardedMigrationTracker{
		records: make(map[string]*ShardMigrationRecord),
	}
}

// RecordShardMigration records a migration execution
func (t *InMemoryShardedMigrationTracker) RecordShardMigration(ctx context.Context, record *ShardMigrationRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.ID == "" {
		record.ID = fmt.Sprintf("%s_%s_%d", record.MigrationID, record.ShardID, time.Now().UnixNano())
	}

	// Store a copy
	recordCopy := *record
	t.records[record.ID] = &recordCopy

	return nil
}

// GetShardMigrationStatus gets migration status for a specific shard
func (t *InMemoryShardedMigrationTracker) GetShardMigrationStatus(ctx context.Context, shardID string, migrationID string) (*ShardMigrationRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, record := range t.records {
		if record.ShardID == shardID && record.MigrationID == migrationID {
			recordCopy := *record
			return &recordCopy, nil
		}
	}

	return nil, fmt.Errorf("migration %s not found on shard %s", migrationID, shardID)
}

// GetOverallMigrationStatus gets aggregated status across all shards
func (t *InMemoryShardedMigrationTracker) GetOverallMigrationStatus(ctx context.Context, migrationID string) (*ShardMigrationStatus, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := &ShardMigrationStatus{
		MigrationID:   migrationID,
		ShardStatuses: make(map[string]*ShardMigrationRecord),
	}

	var earliestStart time.Time
	var latestCompletion *time.Time

	for _, record := range t.records {
		if record.MigrationID != migrationID {
			continue
		}

		switch record.Status {
		case "success", "completed":
			status.SuccessCount++
		case "failed", "error":
			status.FailedCount++
		case "pending":
			status.PendingCount++
		case "running", "in_progress":
			status.InProgressCount++
		}

		recordCopy := *record
		status.ShardStatuses[record.ShardID] = &recordCopy
		status.TotalShards++

		if earliestStart.IsZero() || record.StartedAt.Before(earliestStart) {
			earliestStart = record.StartedAt
		}

		if record.CompletedAt != nil {
			if latestCompletion == nil || record.CompletedAt.After(*latestCompletion) {
				latestCompletion = record.CompletedAt
			}
		}
	}

	status.StartedAt = earliestStart
	status.CompletedAt = latestCompletion

	if status.FailedCount > 0 {
		status.OverallStatus = "failed"
	} else if status.InProgressCount > 0 || status.PendingCount > 0 {
		status.OverallStatus = "in_progress"
	} else if status.SuccessCount == status.TotalShards {
		status.OverallStatus = "success"
	} else {
		status.OverallStatus = "partial"
	}

	return status, nil
}

// GetShardMigrationHistory gets migration history for a shard
func (t *InMemoryShardedMigrationTracker) GetShardMigrationHistory(ctx context.Context, shardID string) ([]*ShardMigrationRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var records []*ShardMigrationRecord

	for _, record := range t.records {
		if record.ShardID == shardID {
			recordCopy := *record
			records = append(records, &recordCopy)
		}
	}

	return records, nil
}

// GetAppliedMigrations returns list of applied migrations for a shard
func (t *InMemoryShardedMigrationTracker) GetAppliedMigrations(ctx context.Context, shardID string) ([]string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	migrationSet := make(map[string]bool)

	for _, record := range t.records {
		if record.ShardID == shardID && (record.Status == "success" || record.Status == "completed") {
			migrationSet[record.MigrationID] = true
		}
	}

	var migrations []string
	for migID := range migrationSet {
		migrations = append(migrations, migID)
	}

	return migrations, nil
}

// IsMigrationApplied checks if migration is applied to a shard
func (t *InMemoryShardedMigrationTracker) IsMigrationApplied(ctx context.Context, shardID string, migrationID string) (bool, error) {
	applied, err := t.GetAppliedMigrations(ctx, shardID)
	if err != nil {
		return false, err
	}

	for _, migID := range applied {
		if migID == migrationID {
			return true, nil
		}
	}

	return false, nil
}

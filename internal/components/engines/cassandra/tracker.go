/**
 * @file tracker.go
 * @description Cassandra migration tracker implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/cassandra/tracker.go
 */

package cassandra

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// CassandraMigrationTracker implements NoSQLMigrationTracker for Cassandra
type CassandraMigrationTracker struct {
	session   *gocql.Session
	config    *types.NoSQLConnectionConfig
	tableName string
	lockTable string
}

// NewCassandraMigrationTracker creates a new Cassandra migration tracker
func NewCassandraMigrationTracker(session *gocql.Session, config *types.NoSQLConnectionConfig) *CassandraMigrationTracker {
	return &CassandraMigrationTracker{
		session:   session,
		config:    config,
		tableName: "schema_migrations",
		lockTable: "schema_migrations_lock",
	}
}

// Connect establishes connection to Cassandra
func (t *CassandraMigrationTracker) Connect(ctx context.Context) error {
	// Connection is managed by the executor, so this is a no-op
	return nil
}

// Disconnect closes the Cassandra connection
func (t *CassandraMigrationTracker) Disconnect(ctx context.Context) error {
	// Connection is managed by the executor, so this is a no-op
	return nil
}

// EnsureSchemaCollection creates the migration tracking table if it doesn't exist
func (t *CassandraMigrationTracker) EnsureSchemaCollection(ctx context.Context) error {
	if t.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	// Create migrations table
	createMigrationsTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			id text PRIMARY KEY,
			version text,
			description text,
			type text,
			script text,
			checksum text,
			author text,
			changeset_id text,
			tags set<text>,
			contexts set<text>,
			labels set<text>,
			dependencies set<text>,
			state text,
			executed_at timestamp,
			executed_by text,
			execution_time bigint,
			error_message text,
			can_rollback boolean,
			rollback_sql text,
			rolled_back_at timestamp,
			rolled_back_by text,
			run_always boolean,
			run_on_change boolean,
			fail_on_error boolean,
			comments text,
			metadata map<text, text>,
			created_at timestamp,
			updated_at timestamp
		)`, t.config.Keyspace, t.tableName)

	if err := t.session.Query(createMigrationsTable).Exec(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Create lock table
	createLockTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			id text PRIMARY KEY,
			acquired_by text,
			acquired_at timestamp,
			expires_at timestamp,
			hostname text,
			process_id int,
			operation text,
			description text,
			metadata map<text, text>
		)`, t.config.Keyspace, t.lockTable)

	if err := t.session.Query(createLockTable).Exec(); err != nil {
		return fmt.Errorf("failed to create lock table: %w", err)
	}

	return nil
}

// GetExecutedMigrations returns all executed migrations
func (t *CassandraMigrationTracker) GetExecutedMigrations(ctx context.Context) ([]types.MigrationRecord, error) {
	if t.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT id, version, description, type, script, checksum, executed_at, execution_time, 
		       error_message, can_rollback, rollback_sql
		FROM %s.%s 
		WHERE state = 'applied'
		ALLOW FILTERING`, t.config.Keyspace, t.tableName)

	iter := t.session.Query(query).Iter()
	defer iter.Close()

	var records []types.MigrationRecord
	var record types.MigrationRecord
	var executedAt time.Time

	for iter.Scan(&record.ID, &record.Version, &record.Description, &record.Type, 
		&record.Script, &record.Checksum, &executedAt, &record.ExecutionTime, 
		&record.ErrorMessage, &record.CanRollback, &record.RollbackSQL) {
		
		record.ExecutedAt = executedAt
		record.Success = true
		records = append(records, record)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to get executed migrations: %w", err)
	}

	return records, nil
}

// GetFailedMigrations returns all failed migrations
func (t *CassandraMigrationTracker) GetFailedMigrations(ctx context.Context) ([]types.MigrationRecord, error) {
	if t.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT id, version, description, type, script, checksum, executed_at, execution_time, 
		       error_message, can_rollback, rollback_sql
		FROM %s.%s 
		WHERE state = 'failed'
		ALLOW FILTERING`, t.config.Keyspace, t.tableName)

	iter := t.session.Query(query).Iter()
	defer iter.Close()

	var records []types.MigrationRecord
	var record types.MigrationRecord
	var executedAt time.Time

	for iter.Scan(&record.ID, &record.Version, &record.Description, &record.Type, 
		&record.Script, &record.Checksum, &executedAt, &record.ExecutionTime, 
		&record.ErrorMessage, &record.CanRollback, &record.RollbackSQL) {
		
		record.ExecutedAt = executedAt
		record.Success = false
		records = append(records, record)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to get failed migrations: %w", err)
	}

	return records, nil
}

// RecordMigrationSuccess records a successful migration
func (t *CassandraMigrationTracker) RecordMigrationSuccess(ctx context.Context, migration types.MigrationRecord, executionTime int64) error {
	if t.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.%s (id, version, description, type, script, checksum, state, 
		                   executed_at, execution_time, can_rollback, rollback_sql, 
		                   created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'applied', ?, ?, ?, ?, ?, ?)`,
		t.config.Keyspace, t.tableName)

	now := time.Now()
	return t.session.Query(query,
		migration.ID, migration.Version, migration.Description, migration.Type,
		migration.Script, migration.Checksum, migration.ExecutedAt, executionTime,
		migration.CanRollback, migration.RollbackSQL, now, now).Exec()
}

// RecordMigrationFailure records a failed migration
func (t *CassandraMigrationTracker) RecordMigrationFailure(ctx context.Context, migration types.MigrationRecord, errorMsg string) error {
	if t.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.%s (id, version, description, type, script, checksum, state, 
		                   executed_at, error_message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'failed', ?, ?, ?, ?)`,
		t.config.Keyspace, t.tableName)

	now := time.Now()
	return t.session.Query(query,
		migration.ID, migration.Version, migration.Description, migration.Type,
		migration.Script, migration.Checksum, migration.ExecutedAt, errorMsg, now, now).Exec()
}

// RemoveMigration removes a migration record
func (t *CassandraMigrationTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	if t.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf("DELETE FROM %s.%s WHERE id = ?", t.config.Keyspace, t.tableName)
	return t.session.Query(query, migrationID).Exec()
}

// IsLocked checks if migrations are locked
func (t *CassandraMigrationTracker) IsLocked(ctx context.Context) (bool, error) {
	if t.session == nil {
		return false, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.%s 
		WHERE expires_at > ? 
		ALLOW FILTERING`, t.config.Keyspace, t.lockTable)

	var count int
	if err := t.session.Query(query, time.Now()).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// AcquireLock acquires a migration lock
func (t *CassandraMigrationTracker) AcquireLock(ctx context.Context) error {
	if t.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	lockID := "migration_lock"
	hostname, _ := getHostname()
	processID := getProcessID()
	now := time.Now()
	expiresAt := now.Add(30 * time.Minute) // 30 minute lock timeout

	query := fmt.Sprintf(`
		INSERT INTO %s.%s (id, acquired_by, acquired_at, expires_at, hostname, process_id, operation)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		IF NOT EXISTS`, t.config.Keyspace, t.lockTable)

	applied, err := t.session.Query(query, lockID, "migration_tool", now, expiresAt, 
		hostname, processID, "migration").ScanCAS()
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !applied {
		return fmt.Errorf("migration is already locked")
	}

	return nil
}

// ReleaseLock releases a migration lock
func (t *CassandraMigrationTracker) ReleaseLock(ctx context.Context) error {
	if t.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	lockID := "migration_lock"
	query := fmt.Sprintf("DELETE FROM %s.%s WHERE id = ?", t.config.Keyspace, t.lockTable)
	return t.session.Query(query, lockID).Exec()
}

// GetCurrentVersion returns the current migration version
func (t *CassandraMigrationTracker) GetCurrentVersion(ctx context.Context) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT version FROM %s.%s 
		WHERE state = 'applied' 
		ORDER BY executed_at DESC 
		LIMIT 1 
		ALLOW FILTERING`, t.config.Keyspace, t.tableName)

	var version string
	if err := t.session.Query(query).Scan(&version); err != nil {
		if err == gocql.ErrNotFound {
			return "", nil // No migrations applied yet
		}
		return "", fmt.Errorf("failed to get current version: %w", err)
	}

	return version, nil
}

// IsMigrationApplied checks if a migration is already applied
func (t *CassandraMigrationTracker) IsMigrationApplied(ctx context.Context, id string) (bool, error) {
	if t.session == nil {
		return false, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.%s 
		WHERE id = ? AND state = 'applied'`, t.config.Keyspace, t.tableName)

	var count int
	if err := t.session.Query(query, id).Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	return count > 0, nil
}

// GetMigration returns a specific migration by ID
func (t *CassandraMigrationTracker) GetMigration(ctx context.Context, id string) (*types.Migration, error) {
	if t.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT id, version, description, type, script, checksum, state, executed_at, 
		       execution_time, error_message, can_rollback, rollback_sql, created_at, updated_at
		FROM %s.%s 
		WHERE id = ?`, t.config.Keyspace, t.tableName)

	var migration types.Migration
	var executedAt, createdAt, updatedAt time.Time
	var executedAtPtr *time.Time

	err := t.session.Query(query, id).Scan(
		&migration.ID, &migration.Version, &migration.Description, &migration.Type,
		&migration.Script, &migration.Checksum, &migration.State, &executedAt,
		&migration.ExecutionTime, &migration.ErrorMessage, &migration.CanRollback,
		&migration.RollbackSQL, &createdAt, &updatedAt)

	if err != nil {
		if err == gocql.ErrNotFound {
			return nil, types.ErrMigrationNotFound
		}
		return nil, fmt.Errorf("failed to get migration: %w", err)
	}

	if !executedAt.IsZero() {
		executedAtPtr = &executedAt
	}
	migration.ExecutedAt = executedAtPtr
	migration.CreatedAt = createdAt
	migration.UpdatedAt = updatedAt

	return &migration, nil
}

// GetMigrationByVersion returns a migration by version
func (t *CassandraMigrationTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	if t.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT id, version, description, type, script, checksum, state, executed_at, 
		       execution_time, error_message, can_rollback, rollback_sql, created_at, updated_at
		FROM %s.%s 
		WHERE version = ? 
		ALLOW FILTERING`, t.config.Keyspace, t.tableName)

	var migration types.Migration
	var executedAt, createdAt, updatedAt time.Time
	var executedAtPtr *time.Time

	err := t.session.Query(query, version).Scan(
		&migration.ID, &migration.Version, &migration.Description, &migration.Type,
		&migration.Script, &migration.Checksum, &migration.State, &executedAt,
		&migration.ExecutionTime, &migration.ErrorMessage, &migration.CanRollback,
		&migration.RollbackSQL, &createdAt, &updatedAt)

	if err != nil {
		if err == gocql.ErrNotFound {
			return nil, types.ErrMigrationNotFound
		}
		return nil, fmt.Errorf("failed to get migration by version: %w", err)
	}

	if !executedAt.IsZero() {
		executedAtPtr = &executedAt
	}
	migration.ExecutedAt = executedAtPtr
	migration.CreatedAt = createdAt
	migration.UpdatedAt = updatedAt

	return &migration, nil
}

// GetAppliedMigrations returns all applied migrations
func (t *CassandraMigrationTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	query := fmt.Sprintf(`
		SELECT id, version, description, type, script, checksum, state, executed_at, 
		       execution_time, error_message, can_rollback, rollback_sql, created_at, updated_at
		FROM %s.%s 
		WHERE state = 'applied'
		ALLOW FILTERING`, t.config.Keyspace, t.tableName)

	iter := t.session.Query(query).Iter()
	defer iter.Close()

	var migrations []*types.Migration
	var migration types.Migration
	var executedAt, createdAt, updatedAt time.Time

	for iter.Scan(&migration.ID, &migration.Version, &migration.Description, &migration.Type,
		&migration.Script, &migration.Checksum, &migration.State, &executedAt,
		&migration.ExecutionTime, &migration.ErrorMessage, &migration.CanRollback,
		&migration.RollbackSQL, &createdAt, &updatedAt) {

		migrationCopy := migration
		if !executedAt.IsZero() {
			migrationCopy.ExecutedAt = &executedAt
		}
		migrationCopy.CreatedAt = createdAt
		migrationCopy.UpdatedAt = updatedAt

		migrations = append(migrations, &migrationCopy)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	return migrations, nil
}

// GetCollectionInfo returns information about the migration collection
func (t *CassandraMigrationTracker) GetCollectionInfo(ctx context.Context) (*types.NoSQLCollectionInfo, error) {
	if t.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	// Get table information
	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.%s`, t.config.Keyspace, t.tableName)

	var count int64
	if err := t.session.Query(query).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to get collection info: %w", err)
	}

	info := &types.NoSQLCollectionInfo{
		Name:          t.tableName,
		Type:          "table",
		DocumentCount: count,
		CreatedAt:     time.Now(), // Cassandra doesn't track table creation time
		UpdatedAt:     time.Now(),
	}

	return info, nil
}

// Helper functions
func getHostname() (string, error) {
	// Implementation would get hostname
	return "localhost", nil
}

func getProcessID() int {
	// Implementation would get process ID
	return 12345
}

/*
@file postgres_lock.go
@description PostgreSQL-specific lock manager implementation using advisory locks and FOR UPDATE
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/lock/postgres_lock.go
*/

package lock

import (
	"database/sql"
	"fmt"
	"time"
)

// PostgresLockManager implements LockManager for PostgreSQL databases
// Uses PostgreSQL advisory locks for better performance and reliability
type PostgresLockManager struct {
	db        *sql.DB
	tableName string
	lockID    int64
	acquired  bool
}

// NewPostgresLockManager creates a new Postgres lock manager
// Note: This is a legacy implementation that doesn't match the current LockManager interface
// Use NewDatabaseLockManager instead for consistent behavior
func NewPostgresLockManager(db *sql.DB, tableName string) *PostgresLockManager {
	return &PostgresLockManager{
		db:        db,
		tableName: tableName,
		lockID:    hashString("migration_lock"), // Convert string to int64 for advisory lock
		acquired:  false,
	}
}

// AcquireLock attempts to acquire a migration lock using PostgreSQL advisory locks
func (l *PostgresLockManager) AcquireLock() error {
	// Try to acquire advisory lock (non-blocking)
	var acquired bool
	err := l.db.QueryRow("SELECT pg_try_advisory_lock($1)", l.lockID).Scan(&acquired)
	if err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	if !acquired {
		return fmt.Errorf("migration lock is already held by another process")
	}

	// Also create a record in the lock table for visibility
	if err := l.ensureLockTable(); err != nil {
		// Release advisory lock if table creation fails
		l.releaseAdvisoryLock()
		return fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Insert lock record
	query := fmt.Sprintf(`
		INSERT INTO %s (lock_name, lock_id, acquired_at, acquired_by) 
		VALUES ('migration_lock', $1, $2, $3)
		ON CONFLICT (lock_name) DO UPDATE SET
			lock_id = EXCLUDED.lock_id,
			acquired_at = EXCLUDED.acquired_at,
			acquired_by = EXCLUDED.acquired_by
	`, l.tableName)

	_, err = l.db.Exec(query, fmt.Sprintf("%d", l.lockID), time.Now(), "migration_engine")
	if err != nil {
		// Release advisory lock if record insertion fails
		l.releaseAdvisoryLock()
		return fmt.Errorf("failed to insert lock record: %w", err)
	}

	l.acquired = true
	return nil
}

// ReleaseLock releases the migration lock
func (l *PostgresLockManager) ReleaseLock() error {
	if !l.acquired {
		return nil // Nothing to release
	}

	// Remove lock record
	query := fmt.Sprintf(`DELETE FROM %s WHERE lock_name = 'migration_lock'`, l.tableName)
	_, err := l.db.Exec(query)
	if err != nil {
		// Continue with advisory lock release even if record deletion fails
		fmt.Printf("Warning: failed to delete lock record: %v\n", err)
	}

	// Release advisory lock
	if err := l.releaseAdvisoryLock(); err != nil {
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}

	l.acquired = false
	return nil
}

// IsLocked checks if a migration lock is currently held
func (l *PostgresLockManager) IsLocked() (bool, error) {
	// Check both advisory lock and table record
	var advisoryLocked bool
	err := l.db.QueryRow("SELECT pg_try_advisory_lock($1)", l.lockID).Scan(&advisoryLocked)
	if err != nil {
		return false, fmt.Errorf("failed to check advisory lock status: %w", err)
	}

	if advisoryLocked {
		// We acquired it, so it wasn't locked - release it immediately
		l.releaseAdvisoryLock()
		return false, nil
	}

	// Advisory lock is held, double-check with table record
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE lock_name = 'migration_lock'`, l.tableName)
	var count int
	err = l.db.QueryRow(query).Scan(&count)
	if err != nil {
		// If table doesn't exist, only rely on advisory lock
		return true, nil
	}

	return count > 0, nil
}

// releaseAdvisoryLock releases the PostgreSQL advisory lock
func (l *PostgresLockManager) releaseAdvisoryLock() error {
	var released bool
	err := l.db.QueryRow("SELECT pg_advisory_unlock($1)", l.lockID).Scan(&released)
	if err != nil {
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}

	if !released {
		return fmt.Errorf("advisory lock was not held by this session")
	}

	return nil
}

// ensureLockTable creates the lock table if it doesn't exist
func (l *PostgresLockManager) ensureLockTable() error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			lock_name TEXT PRIMARY KEY,
			lock_id TEXT NOT NULL,
			acquired_at TIMESTAMP NOT NULL,
			acquired_by TEXT NOT NULL
		)
	`, l.tableName)

	_, err := l.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create lock table: %w", err)
	}

	return nil
}

// hashString converts a string to int64 for use with PostgreSQL advisory locks
func hashString(s string) int64 {
	var hash int64 = 5381
	for _, c := range s {
		hash = ((hash << 5) + hash) + int64(c)
	}
	return hash
}

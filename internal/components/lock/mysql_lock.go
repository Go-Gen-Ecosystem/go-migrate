/*
@file mysql_lock.go
@description MySQL-specific lock manager implementation using GET_LOCK() and FOR UPDATE
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/lock/mysql_lock.go
*/

package lock

import (
	"database/sql"
	"fmt"
	"time"
)

// MySQLLockManager implements LockManager for MySQL databases
// Uses MySQL GET_LOCK() function for named locks
type MySQLLockManager struct {
	db        *sql.DB
	tableName string
	lockName  string
	acquired  bool
}

// NewMySQLLockManager creates a new MySQL lock manager
// Note: This is a legacy implementation that doesn't match the current LockManager interface
// Use NewDatabaseLockManager instead for consistent behavior
func NewMySQLLockManager(db *sql.DB, tableName string) *MySQLLockManager {
	return &MySQLLockManager{
		db:        db,
		tableName: tableName,
		lockName:  "migration_lock",
		acquired:  false,
	}
}

// AcquireLock attempts to acquire a migration lock using MySQL GET_LOCK()
func (l *MySQLLockManager) AcquireLock() error {
	// Try to acquire named lock (timeout of 0 means non-blocking)
	var result sql.NullInt64
	err := l.db.QueryRow("SELECT GET_LOCK(?, 0)", l.lockName).Scan(&result)
	if err != nil {
		return fmt.Errorf("failed to acquire named lock: %w", err)
	}

	if !result.Valid || result.Int64 != 1 {
		if result.Valid && result.Int64 == 0 {
			return fmt.Errorf("migration lock is already held by another process")
		}
		return fmt.Errorf("failed to acquire lock: unexpected result %v", result)
	}

	// Also create a record in the lock table for visibility
	if err := l.ensureLockTable(); err != nil {
		// Release named lock if table creation fails
		l.releaseNamedLock()
		return fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Insert lock record
	query := fmt.Sprintf(`
		INSERT INTO %s (lock_name, lock_id, acquired_at, acquired_by) 
		VALUES ('migration_lock', ?, NOW(), ?)
		ON DUPLICATE KEY UPDATE
			lock_id = VALUES(lock_id),
			acquired_at = VALUES(acquired_at),
			acquired_by = VALUES(acquired_by)
	`, l.tableName)

	_, err = l.db.Exec(query, fmt.Sprintf("mysql_%d", time.Now().UnixNano()), "migration_engine")
	if err != nil {
		// Release named lock if record insertion fails
		l.releaseNamedLock()
		return fmt.Errorf("failed to insert lock record: %w", err)
	}

	l.acquired = true
	return nil
}

// ReleaseLock releases the migration lock
func (l *MySQLLockManager) ReleaseLock() error {
	if !l.acquired {
		return nil // Nothing to release
	}

	// Remove lock record
	query := fmt.Sprintf(`DELETE FROM %s WHERE lock_name = 'migration_lock'`, l.tableName)
	_, err := l.db.Exec(query)
	if err != nil {
		// Continue with named lock release even if record deletion fails
		fmt.Printf("Warning: failed to delete lock record: %v\n", err)
	}

	// Release named lock
	if err := l.releaseNamedLock(); err != nil {
		return fmt.Errorf("failed to release named lock: %w", err)
	}

	l.acquired = false
	return nil
}

// IsLocked checks if a migration lock is currently held
func (l *MySQLLockManager) IsLocked() (bool, error) {
	// Check if named lock is free
	var result sql.NullInt64
	err := l.db.QueryRow("SELECT IS_FREE_LOCK(?)", l.lockName).Scan(&result)
	if err != nil {
		return false, fmt.Errorf("failed to check named lock status: %w", err)
	}

	if !result.Valid {
		return false, fmt.Errorf("failed to check lock status: null result")
	}

	// IS_FREE_LOCK returns 1 if lock is free, 0 if in use, NULL on error
	isLocked := result.Int64 == 0

	// Double-check with table record if lock appears to be held
	if isLocked {
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE lock_name = 'migration_lock'`, l.tableName)
		var count int
		err = l.db.QueryRow(query).Scan(&count)
		if err != nil {
			// If table doesn't exist, only rely on named lock
			return isLocked, nil
		}
		return count > 0, nil
	}

	return false, nil
}

// releaseNamedLock releases the MySQL named lock
func (l *MySQLLockManager) releaseNamedLock() error {
	var result sql.NullInt64
	err := l.db.QueryRow("SELECT RELEASE_LOCK(?)", l.lockName).Scan(&result)
	if err != nil {
		return fmt.Errorf("failed to release named lock: %w", err)
	}

	if !result.Valid {
		return fmt.Errorf("named lock was not held by this session")
	}

	if result.Int64 != 1 {
		return fmt.Errorf("failed to release named lock: unexpected result %d", result.Int64)
	}

	return nil
}

// ensureLockTable creates the lock table if it doesn't exist
func (l *MySQLLockManager) ensureLockTable() error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			lock_name VARCHAR(255) PRIMARY KEY,
			lock_id VARCHAR(255) NOT NULL,
			acquired_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			acquired_by VARCHAR(255) NOT NULL
		)
	`, l.tableName)

	_, err := l.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create lock table: %w", err)
	}

	return nil
}

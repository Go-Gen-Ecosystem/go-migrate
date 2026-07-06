/*
@file sqlite_lock.go
@description SQLite-specific lock manager implementation with dual-locking strategy for enhanced concurrency control
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/lock/sqlite_lock.go
*/

package lock

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SQLiteLockManager implements LockManager for SQLite databases with dual-locking strategy
// Combines database-level locking with file-based locking for improved reliability
type SQLiteLockManager struct {
	db           *sql.DB
	tableName    string
	lockID       string
	acquired     bool
	fileLockPath string
	fileLock     *os.File
	mutex        sync.Mutex
	lockTimeout  time.Duration
}

// NewSQLiteLockManager creates a new SQLite lock manager with dual-locking strategy
// Combines database-level locking with file-based locking for enhanced reliability
func NewSQLiteLockManager(db *sql.DB, tableName string) *SQLiteLockManager {
	// Create file lock path based on database file location
	fileLockPath := filepath.Join(os.TempDir(), fmt.Sprintf("sqlite_migration_%s.lock", tableName))
	
	return &SQLiteLockManager{
		db:           db,
		tableName:    tableName,
		lockID:       "migration_lock",
		acquired:     false,
		fileLockPath: fileLockPath,
		lockTimeout:  30 * time.Second, // Default 30 second timeout
	}
}

// NewSQLiteLockManagerWithTimeout creates a new SQLite lock manager with custom timeout
func NewSQLiteLockManagerWithTimeout(db *sql.DB, tableName string, timeout time.Duration) *SQLiteLockManager {
	manager := NewSQLiteLockManager(db, tableName)
	manager.lockTimeout = timeout
	return manager
}

// AcquireLock attempts to acquire a migration lock using dual-locking strategy
// First acquires file-based lock, then database lock for enhanced reliability
func (l *SQLiteLockManager) AcquireLock() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.acquired {
		return nil // Already acquired
	}

	// Step 1: Acquire file-based lock with timeout
	if err := l.acquireFileLock(); err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}

	// Step 2: Acquire database lock
	if err := l.acquireDatabaseLock(); err != nil {
		// Release file lock if database lock fails
		l.releaseFileLock()
		return fmt.Errorf("failed to acquire database lock: %w", err)
	}

	l.acquired = true
	return nil
}

// acquireFileLock attempts to acquire a file-based lock with timeout
func (l *SQLiteLockManager) acquireFileLock() error {
	timeout := time.Now().Add(l.lockTimeout)
	
	for time.Now().Before(timeout) {
		// Try to create exclusive lock file
		file, err := os.OpenFile(l.fileLockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			if os.IsExist(err) {
				// Lock file exists, check if it's stale
				if l.isLockStale() {
					// Remove stale lock and retry
					os.Remove(l.fileLockPath)
					continue
				}
				// Wait and retry
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return fmt.Errorf("failed to create lock file: %w", err)
		}
		
		// Write process info to lock file
		fmt.Fprintf(file, "pid:%d\ntime:%s\nlockID:%s\n", 
			os.Getpid(), time.Now().Format(time.RFC3339), l.lockID)
		
		l.fileLock = file
		return nil
	}
	
	return fmt.Errorf("timeout waiting for file lock after %v", l.lockTimeout)
}

// acquireDatabaseLock attempts to acquire database-level lock
func (l *SQLiteLockManager) acquireDatabaseLock() error {
	// First, ensure the lock table exists
	if err := l.ensureLockTable(); err != nil {
		return fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Try to acquire the lock by inserting a record
	// If another process already has the lock, this will fail due to UNIQUE constraint
	query := fmt.Sprintf(`
		INSERT INTO %s (lock_name, lock_id, acquired_at, acquired_by) 
		VALUES ('migration_lock', ?, datetime('now'), ?)
	`, l.tableName)

	result, err := l.db.Exec(query, l.lockID, fmt.Sprintf("migration_engine_pid_%d", os.Getpid()))
	if err != nil {
		// Check if it's a constraint violation (lock already exists)
		if isConstraintError(err) {
			return fmt.Errorf("migration lock is already held by another process")
		}
		return fmt.Errorf("failed to acquire database lock: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("migration lock is already held by another process")
	}

	return nil
}

// isLockStale checks if the file lock is stale (process no longer exists)
func (l *SQLiteLockManager) isLockStale() bool {
	info, err := os.Stat(l.fileLockPath)
	if err != nil {
		return true // File doesn't exist, consider stale
	}
	
	// Consider lock stale if older than 5 minutes
	return time.Since(info.ModTime()) > 5*time.Minute
}

// ReleaseLock releases both file and database locks
func (l *SQLiteLockManager) ReleaseLock() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if !l.acquired {
		return nil // Nothing to release
	}

	var errors []error

	// Release database lock
	if err := l.releaseDatabaseLock(); err != nil {
		errors = append(errors, fmt.Errorf("failed to release database lock: %w", err))
	}

	// Release file lock
	if err := l.releaseFileLock(); err != nil {
		errors = append(errors, fmt.Errorf("failed to release file lock: %w", err))
	}

	l.acquired = false

	// Return combined errors if any
	if len(errors) > 0 {
		return fmt.Errorf("lock release errors: %v", errors)
	}

	return nil
}

// releaseDatabaseLock releases the database-level lock
func (l *SQLiteLockManager) releaseDatabaseLock() error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE lock_name = 'migration_lock' AND lock_id = ?`, l.tableName)
	_, err := l.db.Exec(query, l.lockID)
	if err != nil {
		return fmt.Errorf("failed to execute database lock release: %w", err)
	}
	return nil
}

// releaseFileLock releases the file-based lock
func (l *SQLiteLockManager) releaseFileLock() error {
	if l.fileLock != nil {
		l.fileLock.Close()
		l.fileLock = nil
	}
	
	// Remove lock file
	if err := os.Remove(l.fileLockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}
	
	return nil
}

// IsLocked checks if a migration lock is currently held
func (l *SQLiteLockManager) IsLocked() (bool, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE lock_name = 'migration_lock'`, l.tableName)
	
	var count int
	err := l.db.QueryRow(query).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// ensureLockTable creates the lock table if it doesn't exist
func (l *SQLiteLockManager) ensureLockTable() error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			lock_name TEXT PRIMARY KEY,
			lock_id TEXT NOT NULL,
			acquired_at TEXT NOT NULL,
			acquired_by TEXT NOT NULL
		)
	`, l.tableName)

	_, err := l.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create lock table: %w", err)
	}

	return nil
}

// isConstraintError checks if the error is a constraint violation
func isConstraintError(err error) bool {
	// SQLite constraint errors typically contain "UNIQUE constraint failed"
	return err != nil && (
		containsString(err.Error(), "UNIQUE constraint failed") ||
		containsString(err.Error(), "constraint failed"))
}

// containsString checks if a string contains a substring (case-insensitive)
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && 
		   (s == substr || 
		    (len(s) > len(substr) && 
		     (s[:len(substr)] == substr || 
		      s[len(s)-len(substr):] == substr ||
		      containsSubstring(s, substr))))
}

// containsSubstring is a helper function for substring checking
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

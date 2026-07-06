/**
 * @file sqlserver_lock.go
 * @description SQL Server-specific lock manager implementation using sp_getapplock and sp_releaseapplock
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/lock/sqlserver_lock.go
 */

package lock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
)

// SQLServerLockManager implements LockManager for SQL Server databases
// Uses SQL Server application locks (sp_getapplock/sp_releaseapplock) for better performance and reliability
type SQLServerLockManager struct {
	db        *gorm.DB
	tableName string
	lockName  string
	acquired  bool
}

// NewSQLServerLockManager creates a new SQL Server lock manager
func NewSQLServerLockManager(db *gorm.DB, tableName string) *SQLServerLockManager {
	if tableName == "" {
		tableName = DefaultMigrationLockTableName
	}

	return &SQLServerLockManager{
		db:        db,
		tableName: tableName,
		lockName:  "migration_lock",
		acquired:  false,
	}
}

// AcquireLock attempts to acquire a migration lock using SQL Server application locks
func (l *SQLServerLockManager) AcquireLock(ctx context.Context, lockID string, timeout time.Duration) (*MigrationLock, error) {
	// Convert timeout to milliseconds for SQL Server
	timeoutMs := int(timeout.Milliseconds())
	if timeoutMs <= 0 {
		timeoutMs = 30000 // Default 30 seconds
	}

	// Try to acquire application lock using sp_getapplock
	var result int
	err := l.db.WithContext(ctx).Raw("EXEC sp_getapplock @Resource = ?, @LockMode = 'Exclusive', @LockOwner = 'Session', @LockTimeout = ?", 
		l.lockName, timeoutMs).Scan(&result).Error
	if err != nil {
		return nil, fmt.Errorf("failed to acquire SQL Server application lock: %w", err)
	}

	// Check the result code
	// 0 = success, 1 = granted after waiting, -1 = timeout, -2 = canceled, -3 = deadlock victim, -999 = parameter/other error
	if result < 0 {
		switch result {
		case -1:
			return nil, fmt.Errorf("migration lock acquisition timed out after %d ms", timeoutMs)
		case -2:
			return nil, fmt.Errorf("migration lock acquisition was canceled")
		case -3:
			return nil, fmt.Errorf("migration lock acquisition failed due to deadlock")
		default:
			return nil, fmt.Errorf("migration lock acquisition failed with code %d", result)
		}
	}

	// Create lock record for visibility and tracking
	if err := l.ensureLockTable(ctx); err != nil {
		// Release application lock if table creation fails
		l.releaseApplicationLock(ctx)
		return nil, fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Create the migration lock object
	lock := &MigrationLock{
		ID:          lockID,
		AcquiredBy:  "migration_engine",
		AcquiredAt:  time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
		Hostname:    getHostname(),
		ProcessID:   getProcessID(),
		Operation:   "migration",
		Description: "SQL Server migration lock",
		Metadata:    serializeMetadata(map[string]interface{}{
			"lock_mode": "Exclusive",
			"lock_owner": "Session",
			"timeout_ms": timeoutMs,
		}),
	}

	// Insert lock record
	lockRecord := l.lockToRecord(lock)
	err = l.db.WithContext(ctx).Table(l.tableName).Create(lockRecord).Error
	if err != nil {
		// Release application lock if record insertion fails
		l.releaseApplicationLock(ctx)
		return nil, fmt.Errorf("failed to insert lock record: %w", err)
	}

	l.acquired = true
	return lock, nil
}

// ReleaseLock releases the migration lock
func (l *SQLServerLockManager) ReleaseLock(ctx context.Context, lock *MigrationLock) error {
	if !l.acquired {
		return nil // Nothing to release
	}

	// Remove lock record
	err := l.db.WithContext(ctx).Table(l.tableName).Where("id = ?", lock.ID).Delete(&LockRecord{}).Error
	if err != nil {
		// Continue with application lock release even if record deletion fails
		fmt.Printf("Warning: failed to delete lock record: %v\n", err)
	}

	// Release application lock
	if err := l.releaseApplicationLock(ctx); err != nil {
		return fmt.Errorf("failed to release SQL Server application lock: %w", err)
	}

	l.acquired = false
	return nil
}

// RefreshLock refreshes the lock timeout
func (l *SQLServerLockManager) RefreshLock(ctx context.Context, lock *MigrationLock, timeout time.Duration) error {
	if !l.acquired {
		return fmt.Errorf("cannot refresh lock that is not acquired")
	}

	// Update the lock record
	lock.ExpiresAt = time.Now().Add(timeout)
	lockRecord := l.lockToRecord(lock)
	
	err := l.db.WithContext(ctx).Table(l.tableName).Where("id = ?", lock.ID).Updates(lockRecord).Error
	if err != nil {
		return fmt.Errorf("failed to refresh lock record: %w", err)
	}

	return nil
}

// IsLocked checks if a lock is currently held
func (l *SQLServerLockManager) IsLocked(ctx context.Context, lockID string) (bool, error) {
	var count int64
	err := l.db.WithContext(ctx).Table(l.tableName).Where("id = ? AND expires_at > ?", lockID, time.Now()).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// GetActiveLocks returns all currently active locks
func (l *SQLServerLockManager) GetActiveLocks(ctx context.Context) ([]*MigrationLock, error) {
	var records []LockRecord
	err := l.db.WithContext(ctx).Table(l.tableName).Where("expires_at > ?", time.Now()).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get active locks: %w", err)
	}

	locks := make([]*MigrationLock, 0, len(records))
	for _, record := range records {
		lock, err := l.recordToLock(&record)
		if err != nil {
			continue // Skip invalid records
		}
		locks = append(locks, lock)
	}

	return locks, nil
}

// CleanupExpiredLocks removes expired lock records
func (l *SQLServerLockManager) CleanupExpiredLocks(ctx context.Context) error {
	err := l.db.WithContext(ctx).Table(l.tableName).Where("expires_at <= ?", time.Now()).Delete(&LockRecord{}).Error
	if err != nil {
		return fmt.Errorf("failed to cleanup expired locks: %w", err)
	}

	return nil
}

// releaseApplicationLock releases the SQL Server application lock
func (l *SQLServerLockManager) releaseApplicationLock(ctx context.Context) error {
	var result int
	err := l.db.WithContext(ctx).Raw("EXEC sp_releaseapplock @Resource = ?, @LockOwner = 'Session'", l.lockName).Scan(&result).Error
	if err != nil {
		return fmt.Errorf("failed to release application lock: %w", err)
	}

	// Check the result code
	// 0 = success, -999 = parameter/other error
	if result != 0 {
		return fmt.Errorf("failed to release application lock with code %d", result)
	}

	return nil
}

// ensureLockTable creates the lock table if it doesn't exist
func (l *SQLServerLockManager) ensureLockTable(ctx context.Context) error {
	// Check if table exists
	var exists bool
	err := l.db.WithContext(ctx).Raw(`
		SELECT CASE WHEN EXISTS (
			SELECT 1 FROM INFORMATION_SCHEMA.TABLES 
			WHERE TABLE_NAME = ? AND TABLE_SCHEMA = SCHEMA_NAME()
		) THEN 1 ELSE 0 END
	`, l.tableName).Scan(&exists).Error
	
	if err != nil {
		return fmt.Errorf("failed to check if lock table exists: %w", err)
	}

	if !exists {
		// Create the lock table with SQL Server specific syntax
		createTableSQL := fmt.Sprintf(`
			CREATE TABLE [%s] (
				[id] NVARCHAR(255) NOT NULL PRIMARY KEY,
				[acquired_by] NVARCHAR(255) NOT NULL,
				[acquired_at] DATETIME2 NOT NULL,
				[expires_at] DATETIME2 NOT NULL,
				[hostname] NVARCHAR(255) NOT NULL,
				[process_id] INT NOT NULL,
				[operation] NVARCHAR(255) NOT NULL,
				[description] NVARCHAR(MAX),
				[metadata] NVARCHAR(MAX),
				[created_at] DATETIME2 NOT NULL DEFAULT GETDATE(),
				[updated_at] DATETIME2 NOT NULL DEFAULT GETDATE()
			);
			CREATE INDEX [IX_%s_expires_at] ON [%s] ([expires_at]);
		`, l.tableName, l.tableName, l.tableName)

		err = l.db.WithContext(ctx).Exec(createTableSQL).Error
		if err != nil {
			return fmt.Errorf("failed to create lock table: %w", err)
		}
	}

	return nil
}

// lockToRecord converts a MigrationLock to a LockRecord
func (l *SQLServerLockManager) lockToRecord(lock *MigrationLock) *LockRecord {
	return &LockRecord{
		ID:          lock.ID,
		AcquiredBy:  lock.AcquiredBy,
		AcquiredAt:  lock.AcquiredAt,
		ExpiresAt:   lock.ExpiresAt,
		Hostname:    lock.Hostname,
		ProcessID:   lock.ProcessID,
		Operation:   lock.Operation,
		Description: lock.Description,
		Metadata:    lock.Metadata,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// recordToLock converts a LockRecord to a MigrationLock
func (l *SQLServerLockManager) recordToLock(record *LockRecord) (*MigrationLock, error) {
	return &MigrationLock{
		ID:          record.ID,
		AcquiredBy:  record.AcquiredBy,
		AcquiredAt:  record.AcquiredAt,
		ExpiresAt:   record.ExpiresAt,
		Hostname:    record.Hostname,
		ProcessID:   record.ProcessID,
		Operation:   record.Operation,
		Description: record.Description,
		Metadata:    record.Metadata,
	}, nil
}

// Helper functions

// getHostname returns the current hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// getProcessID returns the current process ID
func getProcessID() int {
	return os.Getpid()
}

// serializeMetadata converts metadata map to JSON string
func serializeMetadata(metadata map[string]interface{}) string {
	if metadata == nil {
		return "{}"
	}
	
	data, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	
	return string(data)
}

// deserializeMetadata converts JSON string to metadata map
func deserializeMetadata(data string) (map[string]interface{}, error) {
	if data == "" {
		return make(map[string]interface{}), nil
	}
	
	var metadata map[string]interface{}
	err := json.Unmarshal([]byte(data), &metadata)
	if err != nil {
		return make(map[string]interface{}), err
	}
	
	return metadata, nil
}

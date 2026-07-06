/**
 * @file lock.go
 * @description Migration lock management implementations for preventing concurrent migrations
 * @author Admilson B. F. Cossa
 * @date 2025-01-22
 * @path pkg/core/database/migrations/internal/lock/lock.go
 */

package lock

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// DefaultLockWaitTimeout is the fallback applied when a caller requests a lock
// with a zero (i.e. unconfigured) timeout. Without it, a zero timeout makes the
// wait deadline equal to "now" (database manager) or the lock TTL zero
// (in-memory manager), so acquisition fails or the lock expires the instant it
// is created. A negative timeout is treated as deliberate ("already past") and
// left untouched.
const DefaultLockWaitTimeout = 5 * time.Minute

// Constants for table names
const (
	DefaultMigrationLockTableName = "migration_locks"
)

// MigrationLock represents a migration lock
type MigrationLock struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	AcquiredBy  string    `json:"acquired_by"`
	AcquiredAt  time.Time `json:"acquired_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Hostname    string    `json:"hostname"`
	ProcessID   int       `json:"process_id"`
	Operation   string    `json:"operation"`
	Description string    `json:"description"`
	Metadata    string    `json:"metadata" gorm:"type:text"`
}

// LockManager interface for managing migration locks
type LockManager interface {
	AcquireLock(ctx context.Context, lockID string, timeout time.Duration) (*MigrationLock, error)
	ReleaseLock(ctx context.Context, lock *MigrationLock) error
	IsLocked(ctx context.Context, lockID string) (bool, error)
	RefreshLock(ctx context.Context, lock *MigrationLock, timeout time.Duration) error
	GetActiveLocks(ctx context.Context) ([]*MigrationLock, error)
	CleanupExpiredLocks(ctx context.Context) error
}

// DatabaseLockManager implements LockManager using database storage
type DatabaseLockManager struct {
	db        *gorm.DB
	tableName string
	lockID    string
	mu        sync.RWMutex
}

// InMemoryLockManager implements LockManager using in-memory storage
type InMemoryLockManager struct {
	locks map[string]*MigrationLock
	mu    sync.RWMutex
}

// FileLockManager implements LockManager using file-based locking
type FileLockManager struct {
	lockDir string
	locks   map[string]*fileLock
	mu      sync.RWMutex
}

// fileLock represents a file-based lock
type fileLock struct {
	filePath string
	acquired bool
	lock     *MigrationLock
}

// LockRecord represents a lock record in the database
type LockRecord struct {
	ID          string    `gorm:"primaryKey;column:id" json:"id"`
	AcquiredBy  string    `gorm:"column:acquired_by" json:"acquired_by"`
	AcquiredAt  time.Time `gorm:"column:acquired_at" json:"acquired_at"`
	ExpiresAt   time.Time `gorm:"column:expires_at;index" json:"expires_at"`
	Hostname    string    `gorm:"column:hostname" json:"hostname"`
	ProcessID   int       `gorm:"column:process_id" json:"process_id"`
	Operation   string    `gorm:"column:operation" json:"operation"`
	Description string    `gorm:"column:description" json:"description"`
	Metadata    string    `gorm:"column:metadata;type:text" json:"metadata"` // JSON
	CreatedAt   time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// TableName returns the table name for LockRecord
func (LockRecord) TableName() string {
	return DefaultMigrationLockTableName
}

// Database Lock Manager Implementation

// NewDatabaseLockManager creates a new database lock manager
func NewDatabaseLockManager(db *gorm.DB, tableName string) *DatabaseLockManager {
	return NewDatabaseLockManagerWithDryRun(db, tableName, false)
}

// NewDatabaseLockManagerWithDryRun creates a new database lock manager with dry run support
func NewDatabaseLockManagerWithDryRun(db *gorm.DB, tableName string, dryRun bool) *DatabaseLockManager {
	if tableName == "" {
		tableName = DefaultMigrationLockTableName
	}

	manager := &DatabaseLockManager{
		db:        db,
		tableName: tableName,
		lockID:    generateLockID(),
	}

	// Only auto-migrate lock table if not in dry run mode
	if !dryRun {
		if err := manager.initializeTable(); err != nil {
			// Log and continue - the table will be created on first use.
			logging.GetComponentLogger("lock").Warn(
				"failed to initialize lock table; it will be created on first use", "error", err)
		}
	}

	return manager
}

// initializeTable creates the lock table
func (l *DatabaseLockManager) initializeTable() error {
	return l.db.AutoMigrate(&LockRecord{})
}

// AcquireLock acquires a migration lock
// AcquireLock acquires a migration lock with transactional safety
func (l *DatabaseLockManager) AcquireLock(ctx context.Context, lockID string, timeout time.Duration) (*MigrationLock, error) {
	// A zero (unconfigured) timeout would make the wait deadline equal to "now",
	// so acquisition would fail before the first attempt even for an uncontended
	// lock. Fall back to a sane bounded wait.
	if timeout == 0 {
		timeout = DefaultLockWaitTimeout
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Ensure the lock table exists before attempting to acquire a lock
	if err := l.initializeTable(); err != nil {
		return nil, fmt.Errorf("failed to initialize lock table: %w", err)
	}

	// Detect database dialect for SQLite-specific handling
	dialect := l.db.Dialector.Name()
	
	// Use database transaction for atomic lock acquisition
	var lock *MigrationLock
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// For SQLite, use INSERT OR IGNORE for atomic lock creation
		if dialect == "sqlite" {
			return l.acquireLockSQLite(ctx, tx, lockID, timeout, &lock)
		}
		
		// For other databases, use SELECT FOR UPDATE
		return l.acquireLockWithSelectForUpdate(ctx, tx, lockID, timeout, &lock)
	})

	if err != nil {
		return nil, err
	}

	return lock, nil
}

// acquireLockSQLite implements SQLite-specific lock acquisition using INSERT OR IGNORE
func (l *DatabaseLockManager) acquireLockSQLite(ctx context.Context, tx *gorm.DB, lockID string, timeout time.Duration, lock **MigrationLock) error {
	now := time.Now()
	lockExpiration := time.Hour // Lock expires in 1 hour by default
	expiresAt := now.Add(lockExpiration)
	acquiredBy := fmt.Sprintf("process-%d", os.Getpid())

	// Use the timeout parameter for how long to wait for the lock
	deadline := now.Add(timeout)
	pollInterval := 50 * time.Millisecond // Poll every 50ms

	for {
		// Check if we've exceeded the timeout
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for lock %s after %v", lockID, timeout)
		}

		// First, try to clean up any expired locks
		tx.Where("id = ? AND expires_at <= ?", lockID, time.Now()).Delete(&LockRecord{})

		// Try to insert a new lock record using SQLite's INSERT OR IGNORE
		lockRecord := LockRecord{
			ID:         lockID,
			AcquiredBy: acquiredBy,
			AcquiredAt: time.Now(),
			ExpiresAt:  expiresAt,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		// Use raw SQL for INSERT OR IGNORE to ensure atomicity
		result := tx.Exec(`
			INSERT OR IGNORE INTO `+l.tableName+` (id, acquired_by, acquired_at, expires_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, lockRecord.ID, lockRecord.AcquiredBy, lockRecord.AcquiredAt, lockRecord.ExpiresAt, lockRecord.CreatedAt, lockRecord.UpdatedAt)

		if result.Error != nil {
			return fmt.Errorf("failed to acquire lock: %w", result.Error)
		}

		// Check if the insert was successful (RowsAffected > 0 means we got the lock)
		if result.RowsAffected > 0 {
			// Successfully acquired the lock
			*lock = &MigrationLock{
				ID:         lockID,
				AcquiredBy: acquiredBy,
				AcquiredAt: lockRecord.AcquiredAt,
				ExpiresAt:  expiresAt,
			}
			return nil
		}

		// Lock already exists, wait and retry
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
			// Continue to next iteration
		}
	}
}

// acquireLockWithSelectForUpdate implements lock acquisition using SELECT FOR UPDATE for PostgreSQL/MySQL
func (l *DatabaseLockManager) acquireLockWithSelectForUpdate(ctx context.Context, tx *gorm.DB, lockID string, timeout time.Duration, lock **MigrationLock) error {
	// Use SELECT FOR UPDATE to prevent race conditions
	var existingLock LockRecord
	result := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", lockID).First(&existingLock)

	if result.Error == nil {
		// Lock exists, check if it's expired
		if time.Now().Before(existingLock.ExpiresAt) {
			return fmt.Errorf("lock %s is already acquired by %s", lockID, existingLock.AcquiredBy)
		}

		// Lock is expired, update it atomically
		newLock, err := l.updateExistingLockInTx(tx, &existingLock, timeout)
		if err != nil {
			return err
		}
		*lock = newLock
		return nil
	}

	if result.Error != gorm.ErrRecordNotFound {
		return fmt.Errorf("failed to check existing lock: %w", result.Error)
	}

	// Create new lock atomically
	newLock, err := l.createNewLockInTx(tx, lockID, timeout)
	if err != nil {
		return err
	}
	*lock = newLock
	return nil
}

// ReleaseLock releases a migration lock
// ReleaseLock releases a migration lock with transactional safety
func (l *DatabaseLockManager) ReleaseLock(ctx context.Context, lock *MigrationLock) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Use database transaction for atomic lock release
	return l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Verify we own the lock with SELECT FOR UPDATE
		var existingLock LockRecord
		result := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("id = ? AND acquired_by = ?", lock.ID, lock.AcquiredBy).
			First(&existingLock)
		
		if result.Error != nil {
			if result.Error == gorm.ErrRecordNotFound {
				return fmt.Errorf("lock not found or not owned by this process")
			}
			return fmt.Errorf("failed to verify lock ownership: %w", result.Error)
		}

		// Delete the lock atomically
		result = tx.Delete(&existingLock)
		if result.Error != nil {
			return fmt.Errorf("failed to release lock: %w", result.Error)
		}

		return nil
	})
}

// RefreshLock refreshes a migration lock
func (l *DatabaseLockManager) RefreshLock(ctx context.Context, lock *MigrationLock, timeout time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Update expiration time
	newExpiresAt := time.Now().Add(timeout)

	result := l.db.WithContext(ctx).Model(&LockRecord{}).
		Where("id = ? AND acquired_by = ?", lock.ID, lock.AcquiredBy).
		Updates(map[string]interface{}{
			"expires_at": newExpiresAt,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to refresh lock: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("lock not found or not owned by this process")
	}

	// Update local lock object
	lock.ExpiresAt = newExpiresAt

	return nil
}

// IsLocked checks if a migration lock exists and is active
func (l *DatabaseLockManager) IsLocked(ctx context.Context, lockID string) (bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var count int64
	result := l.db.WithContext(ctx).Model(&LockRecord{}).
		Where("id = ? AND expires_at > ?", lockID, time.Now()).
		Count(&count)

	if result.Error != nil {
		return false, fmt.Errorf("failed to check lock status: %w", result.Error)
	}

	return count > 0, nil
}

// GetActiveLocks retrieves all active locks
func (l *DatabaseLockManager) GetActiveLocks(ctx context.Context) ([]*MigrationLock, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var records []LockRecord
	result := l.db.WithContext(ctx).Where("expires_at > ?", time.Now()).Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get active locks: %w", result.Error)
	}

	locks := make([]*MigrationLock, len(records))
	for i, record := range records {
		lock, err := l.recordToLock(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record to lock: %w", err)
		}
		locks[i] = lock
	}

	return locks, nil
}

// CleanupExpiredLocks removes expired locks
func (l *DatabaseLockManager) CleanupExpiredLocks(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	result := l.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&LockRecord{})
	if result.Error != nil {
		return fmt.Errorf("failed to cleanup expired locks: %w", result.Error)
	}

	return nil
}

// Helper methods for database lock manager

// createNewLock creates a new lock record
func (l *DatabaseLockManager) createNewLock(ctx context.Context, lockID string, timeout time.Duration) (*MigrationLock, error) {
	hostname, _ := os.Hostname()
	processID := os.Getpid()

	lock := &MigrationLock{
		ID:          lockID,
		AcquiredBy:  l.lockID,
		AcquiredAt:  time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
		Hostname:    hostname,
		ProcessID:   processID,
		Operation:   "migration",
		Description: "Database migration lock",
	}

	record := l.lockToRecord(lock)

	result := l.db.WithContext(ctx).Create(record)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to create lock: %w", result.Error)
	}

	return lock, nil
}

// updateExistingLock updates an existing expired lock
func (l *DatabaseLockManager) updateExistingLock(ctx context.Context, existingLock *LockRecord, timeout time.Duration) (*MigrationLock, error) {
	hostname, _ := os.Hostname()
	processID := os.Getpid()

	now := time.Now()
	existingLock.AcquiredBy = l.lockID
	existingLock.AcquiredAt = now
	existingLock.ExpiresAt = now.Add(timeout)
	existingLock.Hostname = hostname
	existingLock.ProcessID = processID
	existingLock.UpdatedAt = now

	result := l.db.WithContext(ctx).Save(existingLock)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to update lock: %w", result.Error)
	}

	return l.recordToLock(existingLock)
}

// lockToRecord converts MigrationLock to LockRecord
func (l *DatabaseLockManager) lockToRecord(lock *MigrationLock) *LockRecord {
	return &LockRecord{
		ID:          lock.ID,
		AcquiredBy:  lock.AcquiredBy,
		AcquiredAt:  lock.AcquiredAt,
		ExpiresAt:   lock.ExpiresAt,
		Hostname:    lock.Hostname,
		ProcessID:   lock.ProcessID,
		Operation:   lock.Operation,
		Description: lock.Description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// recordToLock converts LockRecord to MigrationLock
func (l *DatabaseLockManager) recordToLock(record *LockRecord) (*MigrationLock, error) {
	return &MigrationLock{
		ID:          record.ID,
		AcquiredBy:  record.AcquiredBy,
		AcquiredAt:  record.AcquiredAt,
		ExpiresAt:   record.ExpiresAt,
		Hostname:    record.Hostname,
		ProcessID:   record.ProcessID,
		Operation:   record.Operation,
		Description: record.Description,
	}, nil
}

// In-Memory Lock Manager Implementation

// NewInMemoryLockManager creates a new in-memory lock manager
func NewInMemoryLockManager() *InMemoryLockManager {
	return &InMemoryLockManager{
		locks: make(map[string]*MigrationLock),
	}
}

// AcquireLock acquires a migration lock
func (l *InMemoryLockManager) AcquireLock(ctx context.Context, lockID string, timeout time.Duration) (*MigrationLock, error) {
	// This manager uses the timeout as the lock's TTL; a zero (unconfigured)
	// value would create an already-expired lock. Fall back to a sane default.
	// A negative TTL is left as-is so callers can deliberately create an
	// expired lock.
	if timeout == 0 {
		timeout = DefaultLockWaitTimeout
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if lock exists and is not expired
	if existingLock, exists := l.locks[lockID]; exists {
		if time.Now().Before(existingLock.ExpiresAt) {
			return nil, fmt.Errorf("lock %s is already acquired by %s", lockID, existingLock.AcquiredBy)
		}
		// Lock is expired, remove it
		delete(l.locks, lockID)
	}

	// Create new lock
	hostname, _ := os.Hostname()
	processID := os.Getpid()

	lock := &MigrationLock{
		ID:          lockID,
		AcquiredBy:  generateLockID(),
		AcquiredAt:  time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
		Hostname:    hostname,
		ProcessID:   processID,
		Operation:   "migration",
		Description: "In-memory migration lock",
	}

	l.locks[lockID] = lock

	return lock, nil
}

// ReleaseLock releases a migration lock
func (l *InMemoryLockManager) ReleaseLock(ctx context.Context, lock *MigrationLock) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	existingLock, exists := l.locks[lock.ID]
	if !exists {
		return fmt.Errorf("lock not found")
	}

	if existingLock.AcquiredBy != lock.AcquiredBy {
		return fmt.Errorf("lock not owned by this process")
	}

	delete(l.locks, lock.ID)
	return nil
}

// RefreshLock refreshes a migration lock
func (l *InMemoryLockManager) RefreshLock(ctx context.Context, lock *MigrationLock, timeout time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	existingLock, exists := l.locks[lock.ID]
	if !exists {
		return fmt.Errorf("lock not found")
	}

	if existingLock.AcquiredBy != lock.AcquiredBy {
		return fmt.Errorf("lock not owned by this process")
	}

	existingLock.ExpiresAt = time.Now().Add(timeout)
	lock.ExpiresAt = existingLock.ExpiresAt

	return nil
}

// IsLocked checks if a migration lock exists and is active
func (l *InMemoryLockManager) IsLocked(ctx context.Context, lockID string) (bool, error) {
	l.mu.RLock()
	lock, exists := l.locks[lockID]
	l.mu.RUnlock()

	if !exists {
		return false, nil
	}

	if time.Now().After(lock.ExpiresAt) {
		// Lock is expired, remove it
		l.mu.Lock()
		delete(l.locks, lockID)
		l.mu.Unlock()
		return false, nil
	}

	return true, nil
}

// GetActiveLocks retrieves all active locks
func (l *InMemoryLockManager) GetActiveLocks(ctx context.Context) ([]*MigrationLock, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var activeLocks []*MigrationLock
	var expiredLockIDs []string
	now := time.Now()

	// First pass: identify active and expired locks
	for lockID, lock := range l.locks {
		if now.Before(lock.ExpiresAt) {
			// Create a copy to prevent external modifications
			lockCopy := *lock
			activeLocks = append(activeLocks, &lockCopy)
		} else {
			// Mark for deletion
			expiredLockIDs = append(expiredLockIDs, lockID)
		}
	}

	// Second pass: remove expired locks
	for _, lockID := range expiredLockIDs {
		delete(l.locks, lockID)
	}

	return activeLocks, nil
}

// CleanupExpiredLocks removes expired locks
func (l *InMemoryLockManager) CleanupExpiredLocks(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for lockID, lock := range l.locks {
		if now.After(lock.ExpiresAt) {
			delete(l.locks, lockID)
		}
	}

	return nil
}

// File Lock Manager Implementation

// NewFileLockManager creates a new file-based lock manager
func NewFileLockManager(lockDir string) *FileLockManager {
	if lockDir == "" {
		lockDir = os.TempDir()
	}

	return &FileLockManager{
		lockDir: lockDir,
		locks:   make(map[string]*fileLock),
	}
}

// AcquireLock acquires a migration lock using file system
func (l *FileLockManager) AcquireLock(ctx context.Context, lockID string, timeout time.Duration) (*MigrationLock, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	lockFilePath := fmt.Sprintf("%s/migration_%s.lock", l.lockDir, lockID)

	// Check if lock file exists
	if _, err := os.Stat(lockFilePath); err == nil {
		// Lock file exists, check if it's expired
		// For simplicity, we'll use file modification time
		fileInfo, err := os.Stat(lockFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat lock file: %w", err)
		}

		if time.Since(fileInfo.ModTime()) < timeout {
			return nil, fmt.Errorf("lock %s is already acquired", lockID)
		}

		// Lock is expired, remove it
		os.Remove(lockFilePath)
	}

	// Create lock file
	file, err := os.Create(lockFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}
	file.Close()

	// Create lock object
	hostname, _ := os.Hostname()
	processID := os.Getpid()

	lock := &MigrationLock{
		ID:          lockID,
		AcquiredBy:  generateLockID(),
		AcquiredAt:  time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
		Hostname:    hostname,
		ProcessID:   processID,
		Operation:   "migration",
		Description: "File-based migration lock",
	}

	l.locks[lockID] = &fileLock{
		filePath: lockFilePath,
		acquired: true,
		lock:     lock,
	}

	return lock, nil
}

// ReleaseLock releases a migration lock
func (l *FileLockManager) ReleaseLock(ctx context.Context, lock *MigrationLock) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	fileLock, exists := l.locks[lock.ID]
	if !exists {
		return fmt.Errorf("lock not found")
	}

	if fileLock.lock.AcquiredBy != lock.AcquiredBy {
		return fmt.Errorf("lock not owned by this process")
	}

	// Remove lock file
	if err := os.Remove(fileLock.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}

	delete(l.locks, lock.ID)

	return nil
}

// RefreshLock refreshes a migration lock
func (l *FileLockManager) RefreshLock(ctx context.Context, lock *MigrationLock, timeout time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	fileLock, exists := l.locks[lock.ID]
	if !exists {
		return fmt.Errorf("lock not found")
	}

	if fileLock.lock.AcquiredBy != lock.AcquiredBy {
		return fmt.Errorf("lock not owned by this process")
	}

	// Update file modification time
	now := time.Now()
	if err := os.Chtimes(fileLock.filePath, now, now); err != nil {
		return fmt.Errorf("failed to update lock file time: %w", err)
	}

	// Update lock expiration
	fileLock.lock.ExpiresAt = now.Add(timeout)
	lock.ExpiresAt = fileLock.lock.ExpiresAt

	return nil
}

// IsLocked checks if a migration lock exists and is active
func (l *FileLockManager) IsLocked(ctx context.Context, lockID string) (bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	lockFilePath := fmt.Sprintf("%s/migration_%s.lock", l.lockDir, lockID)

	fileInfo, err := os.Stat(lockFilePath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check lock file: %w", err)
	}

	// Check if lock is expired (using default timeout of 30 minutes)
	if time.Since(fileInfo.ModTime()) > 30*time.Minute {
		// Lock is expired, remove it
		os.Remove(lockFilePath)
		return false, nil
	}

	return true, nil
}

// GetActiveLocks retrieves all active locks
func (l *FileLockManager) GetActiveLocks(ctx context.Context) ([]*MigrationLock, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var activeLocks []*MigrationLock

	for _, fileLock := range l.locks {
		if fileLock.acquired && time.Now().Before(fileLock.lock.ExpiresAt) {
			// Create a copy to prevent external modifications
			lockCopy := *fileLock.lock
			activeLocks = append(activeLocks, &lockCopy)
		}
	}

	return activeLocks, nil
}

// CleanupExpiredLocks removes expired locks
func (l *FileLockManager) CleanupExpiredLocks(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Clean up in-memory locks
	now := time.Now()
	for lockID, fileLock := range l.locks {
		if now.After(fileLock.lock.ExpiresAt) {
			// Remove lock file
			os.Remove(fileLock.filePath)
			delete(l.locks, lockID)
		}
	}

	return nil
}

// Utility functions

// generateLockID generates a unique lock ID
func generateLockID() string {
	hostname, _ := os.Hostname()
	processID := os.Getpid()
	timestamp := time.Now().UnixNano()

	return fmt.Sprintf("%s_%d_%d", hostname, processID, timestamp)
}

// createNewLockInTx creates a new lock within a transaction
func (l *DatabaseLockManager) createNewLockInTx(tx *gorm.DB, lockID string, timeout time.Duration) (*MigrationLock, error) {
	hostname, _ := os.Hostname()
	processID := os.Getpid()

	lock := &MigrationLock{
		ID:          lockID,
		AcquiredBy:  l.lockID,
		AcquiredAt:  time.Now(),
		ExpiresAt:   time.Now().Add(timeout),
		Hostname:    hostname,
		ProcessID:   processID,
		Operation:   "migration",
		Description: "Migration lock acquired",
	}

	record := l.lockToRecord(lock)
	result := tx.Create(record)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to create lock: %w", result.Error)
	}

	return lock, nil
}

// updateExistingLockInTx updates an existing lock within a transaction
func (l *DatabaseLockManager) updateExistingLockInTx(tx *gorm.DB, existingLock *LockRecord, timeout time.Duration) (*MigrationLock, error) {
	hostname, _ := os.Hostname()
	processID := os.Getpid()

	// Update the existing lock record
	existingLock.AcquiredBy = l.lockID
	existingLock.AcquiredAt = time.Now()
	existingLock.ExpiresAt = time.Now().Add(timeout)
	existingLock.Hostname = hostname
	existingLock.ProcessID = processID
	existingLock.UpdatedAt = time.Now()

	result := tx.Save(existingLock)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to update lock: %w", result.Error)
	}

	return l.recordToLock(existingLock)
}

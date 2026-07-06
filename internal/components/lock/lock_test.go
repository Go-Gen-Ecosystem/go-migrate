/**
 * @file lock_test.go
 * @description Behavior tests for the in-memory, file-based, GORM database and
 *              SQLite lock managers covering acquire/release/double-lock/expiry/
 *              refresh semantics against real backends (temp files + sqlite).
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/lock/lock_test.go
 */

package lock

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newGormDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Unique shared-cache in-memory DB per test to avoid cross-test bleed.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	// Keep a single connection so the shared in-memory DB stays alive.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// ---------------- InMemoryLockManager ----------------

func TestInMemoryLock_AcquireReleaseCycle(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	lock, err := mgr.AcquireLock(ctx, "migrate", time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if lock == nil || lock.ID != "migrate" {
		t.Fatalf("unexpected lock: %+v", lock)
	}

	locked, err := mgr.IsLocked(ctx, "migrate")
	if err != nil || !locked {
		t.Fatalf("IsLocked = %v, %v; want true", locked, err)
	}

	if err := mgr.ReleaseLock(ctx, lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	locked, err = mgr.IsLocked(ctx, "migrate")
	if err != nil || locked {
		t.Fatalf("IsLocked after release = %v; want false", locked)
	}
}

func TestInMemoryLock_DoubleAcquireFails(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	if _, err := mgr.AcquireLock(ctx, "k", time.Minute); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := mgr.AcquireLock(ctx, "k", time.Minute); err == nil {
		t.Fatalf("second acquire of held lock should fail")
	}
}

func TestInMemoryLock_ExpiredLockReacquired(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	// Acquire with a lock that is already expired.
	if _, err := mgr.AcquireLock(ctx, "k", -time.Second); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// IsLocked should report false and purge the expired entry.
	locked, err := mgr.IsLocked(ctx, "k")
	if err != nil || locked {
		t.Fatalf("expired lock should not be locked: %v %v", locked, err)
	}
	// A fresh acquire must now succeed because the old lock expired.
	if _, err := mgr.AcquireLock(ctx, "k", time.Minute); err != nil {
		t.Fatalf("reacquire after expiry: %v", err)
	}
}

func TestInMemoryLock_ReleaseErrors(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	// Release of a non-existent lock.
	if err := mgr.ReleaseLock(ctx, &MigrationLock{ID: "ghost"}); err == nil {
		t.Errorf("releasing a missing lock should error")
	}

	// Release by a different owner must fail.
	lock, _ := mgr.AcquireLock(ctx, "k", time.Minute)
	imposter := &MigrationLock{ID: lock.ID, AcquiredBy: "someone-else"}
	if err := mgr.ReleaseLock(ctx, imposter); err == nil {
		t.Errorf("releasing a lock owned by another process should error")
	}
}

func TestInMemoryLock_RefreshExtendsExpiry(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	lock, _ := mgr.AcquireLock(ctx, "k", time.Second)
	before := lock.ExpiresAt

	if err := mgr.RefreshLock(ctx, lock, time.Hour); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}
	if !lock.ExpiresAt.After(before) {
		t.Errorf("RefreshLock did not extend expiry: before=%v after=%v", before, lock.ExpiresAt)
	}

	// Refresh of an unknown lock must error.
	if err := mgr.RefreshLock(ctx, &MigrationLock{ID: "ghost"}, time.Hour); err == nil {
		t.Errorf("refreshing a missing lock should error")
	}
}

func TestInMemoryLock_GetActiveLocksAndCleanup(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	if _, err := mgr.AcquireLock(ctx, "active", time.Hour); err != nil {
		t.Fatalf("acquire active: %v", err)
	}
	// Directly inject an expired lock to exercise cleanup pruning.
	mgr.locks["expired"] = &MigrationLock{ID: "expired", ExpiresAt: time.Now().Add(-time.Hour)}

	active, err := mgr.GetActiveLocks(ctx)
	if err != nil {
		t.Fatalf("GetActiveLocks: %v", err)
	}
	if len(active) != 1 || active[0].ID != "active" {
		t.Fatalf("GetActiveLocks returned %+v, want only 'active'", active)
	}
	// GetActiveLocks also prunes expired entries.
	if _, exists := mgr.locks["expired"]; exists {
		t.Errorf("expired lock should have been pruned by GetActiveLocks")
	}

	// CleanupExpiredLocks should be a no-op-safe operation.
	mgr.locks["expired2"] = &MigrationLock{ID: "expired2", ExpiresAt: time.Now().Add(-time.Hour)}
	if err := mgr.CleanupExpiredLocks(ctx); err != nil {
		t.Fatalf("CleanupExpiredLocks: %v", err)
	}
	if _, exists := mgr.locks["expired2"]; exists {
		t.Errorf("CleanupExpiredLocks did not remove expired lock")
	}
}

// ---------------- FileLockManager ----------------

func TestFileLock_AcquireReleaseCycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mgr := NewFileLockManager(dir)

	lock, err := mgr.AcquireLock(ctx, "job", time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	locked, err := mgr.IsLocked(ctx, "job")
	if err != nil || !locked {
		t.Fatalf("IsLocked = %v %v; want true", locked, err)
	}

	if err := mgr.ReleaseLock(ctx, lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	locked, err = mgr.IsLocked(ctx, "job")
	if err != nil || locked {
		t.Fatalf("IsLocked after release = %v; want false", locked)
	}
}

func TestFileLock_DoubleAcquireFails(t *testing.T) {
	ctx := context.Background()
	mgr := NewFileLockManager(t.TempDir())

	if _, err := mgr.AcquireLock(ctx, "job", time.Minute); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := mgr.AcquireLock(ctx, "job", time.Minute); err == nil {
		t.Fatalf("second acquire while held should fail")
	}
}

func TestFileLock_ReleaseAndRefreshErrors(t *testing.T) {
	ctx := context.Background()
	mgr := NewFileLockManager(t.TempDir())

	if err := mgr.ReleaseLock(ctx, &MigrationLock{ID: "ghost"}); err == nil {
		t.Errorf("releasing a missing file lock should error")
	}
	if err := mgr.RefreshLock(ctx, &MigrationLock{ID: "ghost"}, time.Minute); err == nil {
		t.Errorf("refreshing a missing file lock should error")
	}

	lock, _ := mgr.AcquireLock(ctx, "job", time.Minute)
	imposter := &MigrationLock{ID: lock.ID, AcquiredBy: "other"}
	if err := mgr.ReleaseLock(ctx, imposter); err == nil {
		t.Errorf("releasing a file lock owned by another process should error")
	}
}

func TestFileLock_RefreshAndActiveLocks(t *testing.T) {
	ctx := context.Background()
	mgr := NewFileLockManager(t.TempDir())

	lock, _ := mgr.AcquireLock(ctx, "job", time.Minute)
	before := lock.ExpiresAt
	if err := mgr.RefreshLock(ctx, lock, time.Hour); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}
	if !lock.ExpiresAt.After(before) {
		t.Errorf("RefreshLock did not extend expiry")
	}

	active, err := mgr.GetActiveLocks(ctx)
	if err != nil {
		t.Fatalf("GetActiveLocks: %v", err)
	}
	if len(active) != 1 || active[0].ID != "job" {
		t.Errorf("GetActiveLocks = %+v", active)
	}

	if err := mgr.CleanupExpiredLocks(ctx); err != nil {
		t.Fatalf("CleanupExpiredLocks: %v", err)
	}
}

func TestFileLock_EmptyDirUsesTempDir(t *testing.T) {
	mgr := NewFileLockManager("")
	if mgr.lockDir == "" {
		t.Errorf("empty lock dir should fall back to os.TempDir()")
	}
}

// ---------------- DatabaseLockManager (GORM/sqlite) ----------------

// TestDatabaseLock_ZeroTimeoutAcquiresUncontendedLock guards against a footgun
// where a non-positive wait timeout made the deadline equal to "now", so
// acquisition failed before the first attempt even when the lock was free — the
// exact case a caller who forgets to configure LockTimeout hits.
func TestDatabaseLock_ZeroTimeoutAcquiresUncontendedLock(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	mgr := NewDatabaseLockManager(db, "")

	lock, err := mgr.AcquireLock(ctx, "migrate", 0)
	if err != nil {
		t.Fatalf("zero timeout must fall back to a sane wait, got: %v", err)
	}
	if lock == nil {
		t.Fatal("expected an uncontended lock to be acquired with a zero timeout")
	}
	if err := mgr.ReleaseLock(ctx, lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
}

// TestInMemoryLock_ZeroTimeoutDoesNotExpireImmediately guards the in-memory
// manager's variant of the same footgun: it uses the timeout as the lock TTL,
// so a zero timeout would create an already-expired lock.
func TestInMemoryLock_ZeroTimeoutDoesNotExpireImmediately(t *testing.T) {
	ctx := context.Background()
	mgr := NewInMemoryLockManager()

	if _, err := mgr.AcquireLock(ctx, "migrate", 0); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	// A second acquire must be rejected: the first lock must not have expired
	// the instant it was created.
	if _, err := mgr.AcquireLock(ctx, "migrate", 0); err == nil {
		t.Fatal("second acquire should fail; the zero-timeout lock expired immediately")
	}
}

func TestDatabaseLock_AcquireReleaseCycle(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	mgr := NewDatabaseLockManager(db, "")

	lock, err := mgr.AcquireLock(ctx, "migrate", time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if lock == nil || lock.ID != "migrate" {
		t.Fatalf("unexpected lock: %+v", lock)
	}

	locked, err := mgr.IsLocked(ctx, "migrate")
	if err != nil || !locked {
		t.Fatalf("IsLocked = %v %v; want true", locked, err)
	}

	if err := mgr.ReleaseLock(ctx, lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	locked, err = mgr.IsLocked(ctx, "migrate")
	if err != nil || locked {
		t.Fatalf("IsLocked after release = %v; want false", locked)
	}
}

func TestDatabaseLock_DoubleAcquireTimesOut(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	// Empty table name resolves to the default (migration_locks), which is the
	// table the manager AutoMigrates; a custom name would not be created.
	mgr := NewDatabaseLockManager(db, "")

	if _, err := mgr.AcquireLock(ctx, "k", time.Minute); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// A second acquire of the still-valid lock should not succeed within a
	// short timeout (SQLite path polls until deadline).
	start := time.Now()
	_, err := mgr.AcquireLock(ctx, "k", 150*time.Millisecond)
	if err == nil {
		t.Fatalf("acquiring a held lock should fail")
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Errorf("expected the acquire to wait until the timeout, returned too fast")
	}
}

func TestDatabaseLock_GetActiveLocksAndCleanup(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	mgr := NewDatabaseLockManager(db, "")

	if _, err := mgr.AcquireLock(ctx, "a", time.Hour); err != nil {
		t.Fatalf("acquire a: %v", err)
	}

	active, err := mgr.GetActiveLocks(ctx)
	if err != nil {
		t.Fatalf("GetActiveLocks: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("GetActiveLocks len = %d, want 1", len(active))
	}

	// Insert an already-expired lock row directly, then clean it up.
	expired := &Record{
		ID:        "expired",
		ExpiresAt: time.Now().Add(-time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.WithContext(ctx).Create(expired).Error; err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	if err := mgr.CleanupExpiredLocks(ctx); err != nil {
		t.Fatalf("CleanupExpiredLocks: %v", err)
	}
	locked, err := mgr.IsLocked(ctx, "expired")
	if err != nil || locked {
		t.Errorf("expired lock should be gone: locked=%v err=%v", locked, err)
	}
}

func TestDatabaseLock_RefreshExtendsExpiry(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	mgr := NewDatabaseLockManager(db, "")

	lock, err := mgr.AcquireLock(ctx, "k", time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	before := lock.ExpiresAt

	// The SQLite acquire path pins expiry to now+1h regardless of the requested
	// timeout, so refresh with a clearly-longer window to assert extension.
	if err := mgr.RefreshLock(ctx, lock, 24*time.Hour); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}
	if !lock.ExpiresAt.After(before) {
		t.Errorf("RefreshLock did not extend expiry: before=%v after=%v", before, lock.ExpiresAt)
	}

	// Refreshing a lock not owned by this process must not affect any rows.
	err = mgr.RefreshLock(ctx, &MigrationLock{ID: "k", AcquiredBy: "nobody"}, time.Hour)
	if err == nil {
		t.Errorf("refreshing an unowned lock should error")
	}
}

func TestDatabaseLock_ExpiredLockReacquired(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	mgr := NewDatabaseLockManager(db, "")

	// Seed an expired lock row for the id we want to acquire.
	if err := db.WithContext(ctx).Create(&Record{
		ID:        "k",
		ExpiresAt: time.Now().Add(-time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed expired: %v", err)
	}

	// SQLite path deletes expired rows and inserts fresh; acquire must succeed.
	lock, err := mgr.AcquireLock(ctx, "k", time.Minute)
	if err != nil {
		t.Fatalf("reacquire over expired lock: %v", err)
	}
	if lock == nil {
		t.Fatalf("expected a lock after reacquire")
	}
}

func TestDatabaseLock_HelperConversionsAndCreateUpdate(t *testing.T) {
	ctx := context.Background()
	db := newGormDB(t)
	mgr := NewDatabaseLockManager(db, "")

	// createNewLock inserts a fresh row and returns the lock.
	created, err := mgr.createNewLock(ctx, "H1", time.Hour)
	if err != nil {
		t.Fatalf("createNewLock: %v", err)
	}
	if created.ID != "H1" || created.AcquiredBy != mgr.lockID {
		t.Errorf("createNewLock returned %+v", created)
	}
	locked, err := mgr.IsLocked(ctx, "H1")
	if err != nil || !locked {
		t.Fatalf("row from createNewLock not visible: %v %v", locked, err)
	}

	// updateExistingLock refreshes an existing expired row in place.
	var rec Record
	if err := db.WithContext(ctx).Where("id = ?", "H1").First(&rec).Error; err != nil {
		t.Fatalf("load record: %v", err)
	}
	updated, err := mgr.updateExistingLock(ctx, &rec, 2*time.Hour)
	if err != nil {
		t.Fatalf("updateExistingLock: %v", err)
	}
	if updated.ID != "H1" {
		t.Errorf("updateExistingLock returned %+v", updated)
	}

	// lockToRecord / recordToLock round-trip preserves identity fields.
	orig := &MigrationLock{ID: "R1", AcquiredBy: "me", Operation: "migration", ProcessID: 7}
	back, err := mgr.recordToLock(mgr.lockToRecord(orig))
	if err != nil {
		t.Fatalf("recordToLock: %v", err)
	}
	if back.ID != "R1" || back.ProcessID != 7 || back.Operation != "migration" {
		t.Errorf("round trip mismatch: %+v", back)
	}
}

// ---------------- SQLiteLockManager (database/sql) ----------------

func newSQLDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "lock.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteLockManager_AcquireReleaseCycle(t *testing.T) {
	db := newSQLDB(t)
	mgr := NewSQLiteLockManager(db, "mig_locks")

	if err := mgr.AcquireLock(); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	locked, err := mgr.IsLocked()
	if err != nil || !locked {
		t.Fatalf("IsLocked = %v %v; want true", locked, err)
	}

	// Acquiring again on the same manager is idempotent (already acquired).
	if err := mgr.AcquireLock(); err != nil {
		t.Fatalf("re-AcquireLock on same manager should be a no-op: %v", err)
	}

	if err := mgr.ReleaseLock(); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}

	locked, err = mgr.IsLocked()
	if err != nil || locked {
		t.Fatalf("IsLocked after release = %v; want false", locked)
	}

	// Releasing again is a safe no-op.
	if err := mgr.ReleaseLock(); err != nil {
		t.Fatalf("double ReleaseLock should be safe: %v", err)
	}
}

func TestSQLiteLockManager_MutualExclusionAcrossManagers(t *testing.T) {
	db := newSQLDB(t)
	tableName := "mig_locks_mx"

	// Use very short timeouts so the second acquire fails quickly rather than
	// waiting on the file lock; both share the same table + file lock path.
	m1 := NewSQLiteLockManagerWithTimeout(db, tableName, 200*time.Millisecond)
	m2 := NewSQLiteLockManagerWithTimeout(db, tableName, 200*time.Millisecond)

	if err := m1.AcquireLock(); err != nil {
		t.Fatalf("m1 AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = m1.ReleaseLock() })

	// m2 must fail because m1 holds both the file lock and the DB row.
	if err := m2.AcquireLock(); err == nil {
		_ = m2.ReleaseLock()
		t.Fatalf("m2 should not acquire a lock already held by m1")
	}
}

func TestSQLiteLockManager_WithTimeoutConstructor(t *testing.T) {
	db := newSQLDB(t)
	mgr := NewSQLiteLockManagerWithTimeout(db, "t", 3*time.Second)
	if mgr.lockTimeout != 3*time.Second {
		t.Errorf("lockTimeout = %v, want 3s", mgr.lockTimeout)
	}
}

// ---------------- Pure helpers (KAT) ----------------

func TestIsConstraintError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unique constraint", errString("UNIQUE constraint failed: locks.id"), true},
		{"generic constraint", errString("constraint failed"), true},
		{"unrelated", errString("disk I/O error"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isConstraintError(c.err); got != c.want {
				t.Errorf("isConstraintError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestContainsSubstring(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"hello world", "o w", true},
		{"hello", "hello", true},
		{"hello", "xyz", false},
		{"abc", "abcd", false},
		{"UNIQUE constraint failed", "constraint", true},
	}
	for _, c := range cases {
		if got := containsSubstring(c.s, c.sub); got != c.want {
			t.Errorf("containsSubstring(%q,%q) = %v, want %v", c.s, c.sub, got, c.want)
		}
	}
}

func TestGenerateLockID_Format(t *testing.T) {
	id := generateLockID()
	if id == "" {
		t.Fatalf("generateLockID returned empty")
	}
	// Format is "<hostname>_<pid>_<unixnano>".
	parts := strings.Split(id, "_")
	if len(parts) < 3 {
		t.Fatalf("generateLockID format = %q, want at least 3 underscore-separated parts", id)
	}
	// The trailing timestamp component must be numeric.
	if _, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err != nil {
		t.Errorf("trailing component of %q should be a numeric timestamp: %v", id, err)
	}
}

// errString is a tiny error helper for KAT tables.
type errString string

func (e errString) Error() string { return string(e) }

/**
 * @file db_specific_lock_test.go
 * @description Tests for the MySQL, PostgreSQL and SQL Server lock managers.
 *              MySQL/Postgres named/advisory-lock flows are driven with
 *              go-sqlmock; the SQL Server table-based operations run against an
 *              in-memory GORM sqlite DB. Pure helpers are covered via KATs.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/lock/db_specific_lock_test.go
 */

package lock

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ---------------- MySQLLockManager (sqlmock) ----------------

func TestMySQLLock_AcquireSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, 0)")).
		WithArgs("migration_lock").
		WillReturnRows(sqlmock.NewRows([]string{"GET_LOCK"}).AddRow(1))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mgr := NewMySQLLockManager(db, "migration_locks")
	if err := mgr.AcquireLock(); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if !mgr.acquired {
		t.Errorf("manager should be marked acquired")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestMySQLLock_AcquireContention(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// GET_LOCK returns 0 -> already held by another process.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(?, 0)")).
		WithArgs("migration_lock").
		WillReturnRows(sqlmock.NewRows([]string{"GET_LOCK"}).AddRow(0))

	mgr := NewMySQLLockManager(db, "migration_locks")
	if err := mgr.AcquireLock(); err == nil {
		t.Fatalf("AcquireLock should fail when GET_LOCK returns 0")
	}
	if mgr.acquired {
		t.Errorf("manager should not be marked acquired on contention")
	}
}

func TestMySQLLock_ReleaseNotAcquiredIsNoOp(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()
	mgr := NewMySQLLockManager(db, "migration_locks")
	if err := mgr.ReleaseLock(); err != nil {
		t.Errorf("ReleaseLock without acquire should be a no-op: %v", err)
	}
}

func TestMySQLLock_ReleaseSuccess(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mgr := NewMySQLLockManager(db, "migration_locks")
	mgr.acquired = true

	mock.ExpectExec("DELETE FROM").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT RELEASE_LOCK(?)")).
		WithArgs("migration_lock").
		WillReturnRows(sqlmock.NewRows([]string{"RELEASE_LOCK"}).AddRow(1))

	if err := mgr.ReleaseLock(); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	if mgr.acquired {
		t.Errorf("manager should be cleared after release")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestMySQLLock_IsLocked(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// IS_FREE_LOCK returns 0 -> lock in use; table count confirms it.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT IS_FREE_LOCK(?)")).
		WithArgs("migration_lock").
		WillReturnRows(sqlmock.NewRows([]string{"IS_FREE_LOCK"}).AddRow(0))
	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mgr := NewMySQLLockManager(db, "migration_locks")
	locked, err := mgr.IsLocked()
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if !locked {
		t.Errorf("expected locked=true")
	}
}

func TestMySQLLock_IsFree(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// IS_FREE_LOCK returns 1 -> free.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT IS_FREE_LOCK(?)")).
		WithArgs("migration_lock").
		WillReturnRows(sqlmock.NewRows([]string{"IS_FREE_LOCK"}).AddRow(1))

	mgr := NewMySQLLockManager(db, "migration_locks")
	locked, err := mgr.IsLocked()
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if locked {
		t.Errorf("expected locked=false when lock is free")
	}
}

// ---------------- PostgresLockManager (sqlmock) ----------------

func TestPostgresLock_AcquireSuccess(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO").
		WillReturnResult(sqlmock.NewResult(1, 1))

	mgr := NewPostgresLockManager(db, "migration_locks")
	if err := mgr.AcquireLock(); err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if !mgr.acquired {
		t.Errorf("manager should be acquired")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestPostgresLock_AcquireContention(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

	mgr := NewPostgresLockManager(db, "migration_locks")
	if err := mgr.AcquireLock(); err == nil {
		t.Fatalf("AcquireLock should fail when advisory lock is not granted")
	}
}

func TestPostgresLock_IsLockedWhenAdvisoryHeld(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// pg_try_advisory_lock returns false -> someone else holds it.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))
	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mgr := NewPostgresLockManager(db, "migration_locks")
	locked, err := mgr.IsLocked()
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if !locked {
		t.Errorf("expected locked=true")
	}
}

func TestPostgresLock_IsLockedWhenFree(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// We could acquire it -> it was free; manager releases immediately.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

	mgr := NewPostgresLockManager(db, "migration_locks")
	locked, err := mgr.IsLocked()
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if locked {
		t.Errorf("expected locked=false when advisory lock is free")
	}
}

func TestHashString(t *testing.T) {
	// hashString is a deterministic DJB2 variant; equal inputs -> equal output,
	// and it must be stable for the well-known "migration_lock" resource.
	a := hashString("migration_lock")
	b := hashString("migration_lock")
	if a != b {
		t.Errorf("hashString not deterministic: %d != %d", a, b)
	}
	if hashString("other") == a {
		t.Errorf("distinct inputs unexpectedly collided")
	}
	// DJB2 seed of 5381 for the empty string.
	if got := hashString(""); got != 5381 {
		t.Errorf("hashString(\"\") = %d, want 5381", got)
	}
}

// ---------------- SQLServerLockManager (gorm sqlite for table ops) ----------------

func newSQLServerGormDB(t *testing.T, tableName string) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Create the lock table using a sqlite-compatible schema matching Record.
	if err := db.Table(tableName).AutoMigrate(&Record{}); err != nil {
		t.Fatalf("automigrate lock table %q: %v", tableName, err)
	}
	return db
}

func TestSQLServerLock_TableOperations(t *testing.T) {
	ctx := context.Background()
	table := "mig_locks_ss"
	db := newSQLServerGormDB(t, table)
	mgr := NewSQLServerLockManager(db, table)

	// Seed an active lock row directly.
	active := mgr.lockToRecord(&MigrationLock{
		ID:         "L1",
		AcquiredBy: "engine",
		AcquiredAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	})
	if err := db.WithContext(ctx).Table(table).Create(active).Error; err != nil {
		t.Fatalf("seed active lock: %v", err)
	}

	locked, err := mgr.IsLocked(ctx, "L1")
	if err != nil || !locked {
		t.Fatalf("IsLocked(L1) = %v %v; want true", locked, err)
	}

	locks, err := mgr.GetActiveLocks(ctx)
	if err != nil {
		t.Fatalf("GetActiveLocks: %v", err)
	}
	if len(locks) != 1 || locks[0].ID != "L1" {
		t.Fatalf("GetActiveLocks = %+v", locks)
	}

	// RefreshLock updates the record (requires acquired flag).
	mgr.acquired = true
	l := &MigrationLock{ID: "L1", AcquiredBy: "engine"}
	if err := mgr.RefreshLock(ctx, l, 2*time.Hour); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}

	// Seed an expired lock and clean it up.
	expired := mgr.lockToRecord(&MigrationLock{
		ID:        "L2",
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	if err := db.WithContext(ctx).Table(table).Create(expired).Error; err != nil {
		t.Fatalf("seed expired lock: %v", err)
	}
	if err := mgr.CleanupExpiredLocks(ctx); err != nil {
		t.Fatalf("CleanupExpiredLocks: %v", err)
	}
	locked, err = mgr.IsLocked(ctx, "L2")
	if err != nil || locked {
		t.Errorf("expired L2 should be gone: %v %v", locked, err)
	}
}

func TestSQLServerLock_RefreshRequiresAcquired(t *testing.T) {
	ctx := context.Background()
	table := "mig_locks_ss2"
	db := newSQLServerGormDB(t, table)
	mgr := NewSQLServerLockManager(db, table)

	if err := mgr.RefreshLock(ctx, &MigrationLock{ID: "x"}, time.Hour); err == nil {
		t.Errorf("RefreshLock on a non-acquired manager should error")
	}
}

func TestSQLServerLock_ReleaseNotAcquiredIsNoOp(t *testing.T) {
	ctx := context.Background()
	table := "mig_locks_ss3"
	db := newSQLServerGormDB(t, table)
	mgr := NewSQLServerLockManager(db, table)

	if err := mgr.ReleaseLock(ctx, &MigrationLock{ID: "x"}); err != nil {
		t.Errorf("ReleaseLock without acquire should be a no-op: %v", err)
	}
}

func TestSQLServerLock_DefaultTableName(t *testing.T) {
	mgr := NewSQLServerLockManager(nil, "")
	if mgr.tableName != DefaultMigrationLockTableName {
		t.Errorf("tableName = %q, want %q", mgr.tableName, DefaultMigrationLockTableName)
	}
}

// ---------------- Pure helpers (SQL Server) ----------------

func TestSerializeDeserializeMetadata(t *testing.T) {
	t.Run("nil serializes to empty object", func(t *testing.T) {
		if got := serializeMetadata(nil); got != "{}" {
			t.Errorf("serializeMetadata(nil) = %q, want {}", got)
		}
	})

	t.Run("round trip", func(t *testing.T) {
		in := map[string]interface{}{"k": "v", "n": float64(3)}
		s := serializeMetadata(in)

		// Validate it is real JSON.
		var check map[string]interface{}
		if err := json.Unmarshal([]byte(s), &check); err != nil {
			t.Fatalf("serialized metadata is not valid JSON: %v", err)
		}

		out, err := deserializeMetadata(s)
		if err != nil {
			t.Fatalf("deserializeMetadata: %v", err)
		}
		if out["k"] != "v" || out["n"] != float64(3) {
			t.Errorf("round trip mismatch: %+v", out)
		}
	})

	t.Run("empty string deserializes to empty map", func(t *testing.T) {
		out, err := deserializeMetadata("")
		if err != nil || out == nil || len(out) != 0 {
			t.Errorf("deserializeMetadata(\"\") = %+v, %v", out, err)
		}
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		if _, err := deserializeMetadata("{not-json"); err == nil {
			t.Errorf("expected error for malformed JSON")
		}
	})
}

func TestGetHostnameAndPID(t *testing.T) {
	if got := getHostname(); got == "" {
		t.Errorf("getHostname returned empty")
	}
	if got := getProcessID(); got != os.Getpid() {
		t.Errorf("getProcessID = %d, want %d", got, os.Getpid())
	}
}

func TestSQLServerLock_RecordRoundTrip(t *testing.T) {
	mgr := NewSQLServerLockManager(nil, "t")
	orig := &MigrationLock{
		ID:          "L",
		AcquiredBy:  "engine",
		AcquiredAt:  time.Now().Truncate(time.Second),
		ExpiresAt:   time.Now().Add(time.Hour).Truncate(time.Second),
		Hostname:    "host",
		ProcessID:   42,
		Operation:   "migration",
		Description: "d",
		Metadata:    "{}",
	}
	rec := mgr.lockToRecord(orig)
	back, err := mgr.recordToLock(rec)
	if err != nil {
		t.Fatalf("recordToLock: %v", err)
	}
	if back.ID != orig.ID || back.ProcessID != orig.ProcessID || back.Operation != orig.Operation {
		t.Errorf("round trip mismatch: %+v vs %+v", back, orig)
	}
}

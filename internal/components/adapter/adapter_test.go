/**
 * @file adapter_test.go
 * @description Behaviour tests for the loader and tracker adapters. The tracker
 *              adapter is driven against a real in-memory SQLite database via
 *              GORM and a real DatabaseMigrationTracker.
 * @author Admilson B. F. Cossa
 */

package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	loaderpkg "github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newMemoryDB opens a per-test in-memory SQLite database. Using a name derived
// from the test keeps tables isolated between tests, and pinning to a single
// connection guarantees writes are visible to subsequent reads on the same
// shared-cache in-memory database.
func newMemoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// newTrackerAdapter builds a real DatabaseMigrationTracker over the given DB and
// wraps it in the TrackerAdapter under test.
func newTrackerAdapter(t *testing.T, db *gorm.DB) types.MigrationTracker {
	t.Helper()
	dbTracker, err := tracker.NewDatabaseMigrationTracker(db, "migration_history")
	require.NoError(t, err)
	return NewTrackerAdapter(dbTracker, db)
}

// --- LoaderAdapter ------------------------------------------------------------------

func TestLoaderAdapter_LoadMigrations(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "001_create_users.sql", "CREATE TABLE users (id INTEGER PRIMARY KEY);")
	writeSQL(t, dir, "002_create_orders.sql", "CREATE TABLE orders (id INTEGER PRIMARY KEY);")

	adapter := NewLoaderAdapter(loaderpkg.NewFileMigrationLoader())

	migs, err := adapter.LoadMigrations(dir)
	require.NoError(t, err)
	require.Len(t, migs, 2)

	// LoadMigrations dereferences pointers into values; check the content survived.
	assert.Contains(t, migs[0].Script, "CREATE TABLE")
	assert.NotEmpty(t, migs[0].ID)
	assert.NotEmpty(t, migs[0].Checksum)
}

func TestLoaderAdapter_LoadMigration_Single(t *testing.T) {
	dir := t.TempDir()
	path := writeSQL(t, dir, "001_create_users.sql", "CREATE TABLE users (id INTEGER PRIMARY KEY);")

	adapter := NewLoaderAdapter(loaderpkg.NewFileMigrationLoader())

	mig, err := adapter.LoadMigration(path)
	require.NoError(t, err)
	require.NotNil(t, mig)
	// The simple-numeric filename pattern captures the version literally ("001").
	assert.Equal(t, "001", mig.Version)
	assert.Contains(t, mig.Script, "users")
}

func TestLoaderAdapter_LoadMigration_MissingFile(t *testing.T) {
	adapter := NewLoaderAdapter(loaderpkg.NewFileMigrationLoader())
	_, err := adapter.LoadMigration(filepath.Join(t.TempDir(), "nope.sql"))
	assert.Error(t, err)
}

func TestLoaderAdapter_ValidateFile(t *testing.T) {
	dir := t.TempDir()
	good := writeSQL(t, dir, "001_ok.sql", "CREATE TABLE t (id INTEGER);")

	adapter := NewLoaderAdapter(loaderpkg.NewFileMigrationLoader())
	assert.NoError(t, adapter.ValidateFile(good))

	// A dangerous statement is rejected by the SQL validator.
	bad := writeSQL(t, dir, "002_drop.sql", "DROP DATABASE production;")
	assert.Error(t, adapter.ValidateFile(bad))
}

func TestLoaderAdapter_ValidateFile_Missing(t *testing.T) {
	adapter := NewLoaderAdapter(loaderpkg.NewFileMigrationLoader())
	assert.Error(t, adapter.ValidateFile(filepath.Join(t.TempDir(), "missing.sql")))
}

// --- TrackerAdapter: real SQLite round-trips ---------------------------------------

func TestTrackerAdapter_EnsureSchemaTable_ThenEmpty(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	adapter := newTrackerAdapter(t, db)

	require.NoError(t, adapter.EnsureSchemaTable(ctx))

	// No migrations recorded yet.
	executed, err := adapter.GetExecutedMigrations(ctx)
	require.NoError(t, err)
	assert.Empty(t, executed)

	applied, err := adapter.GetAppliedMigrations(ctx)
	require.NoError(t, err)
	assert.Empty(t, applied)

	// Current version of an empty schema is the zero sentinel.
	ver, err := adapter.GetCurrentVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0", ver)
}

func TestTrackerAdapter_RecordSuccess_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	adapter := newTrackerAdapter(t, db)
	require.NoError(t, adapter.EnsureSchemaTable(ctx))

	rec := types.MigrationRecord{
		ID:          "mig-1",
		Version:     "1.0.0",
		Description: "create users",
		Type:        "sql",
		Checksum:    "abc123",
		CanRollback: true,
		RollbackSQL: "DROP TABLE users;",
	}
	require.NoError(t, adapter.RecordMigrationSuccess(ctx, rec, 42))

	// It is now reported as applied.
	applied, err := adapter.IsMigrationApplied(ctx, "mig-1")
	require.NoError(t, err)
	assert.True(t, applied)

	// And retrievable via the executed list and by version.
	executed, err := adapter.GetExecutedMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, executed, 1)
	assert.Equal(t, "1.0.0", executed[0].Version)
	assert.True(t, executed[0].CanRollback)

	byVer, err := adapter.GetMigrationByVersion(ctx, "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, byVer)
	assert.Equal(t, "mig-1", byVer.ID)

	got, err := adapter.GetMigration(ctx, "mig-1")
	require.NoError(t, err)
	assert.Equal(t, "create users", got.Description)

	// Current version now reflects the applied migration.
	ver, err := adapter.GetCurrentVersion(ctx)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", ver)

	appliedList, err := adapter.GetAppliedMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, appliedList, 1)
	assert.Equal(t, "mig-1", appliedList[0].ID)
}

func TestTrackerAdapter_RemoveMigration(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	adapter := newTrackerAdapter(t, db)
	require.NoError(t, adapter.EnsureSchemaTable(ctx))

	require.NoError(t, adapter.RecordMigrationSuccess(ctx, types.MigrationRecord{
		ID: "mig-x", Version: "2.0.0", Type: "sql",
	}, 1))

	applied, err := adapter.IsMigrationApplied(ctx, "mig-x")
	require.NoError(t, err)
	require.True(t, applied)

	require.NoError(t, adapter.RemoveMigration(ctx, "mig-x"))

	applied, err = adapter.IsMigrationApplied(ctx, "mig-x")
	require.NoError(t, err)
	assert.False(t, applied)
}

func TestTrackerAdapter_RecordFailure(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	adapter := newTrackerAdapter(t, db)
	require.NoError(t, adapter.EnsureSchemaTable(ctx))

	require.NoError(t, adapter.RecordMigrationFailure(ctx, types.MigrationRecord{
		ID: "mig-fail", Version: "3.0.0", Type: "sql",
	}, "boom"))

	// A failed migration is not "applied".
	applied, err := adapter.IsMigrationApplied(ctx, "mig-fail")
	require.NoError(t, err)
	assert.False(t, applied)

	// GetFailedMigrations queries state='failed' directly and returns it.
	failed, err := adapter.GetFailedMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, failed, 1)
	assert.Equal(t, "mig-fail", failed[0].ID)
	assert.Equal(t, "boom", failed[0].ErrorMessage)
	assert.False(t, failed[0].Success)
}

func TestTrackerAdapter_DryRunShortCircuits(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	dbTracker, err := tracker.NewDatabaseMigrationTracker(db, "migration_history")
	require.NoError(t, err)

	// The concrete adapter exposes SetDryRun.
	a := NewTrackerAdapter(dbTracker, db).(*TrackerAdapter)
	require.NoError(t, a.EnsureSchemaTable(ctx))
	require.NoError(t, a.RecordMigrationSuccess(ctx, types.MigrationRecord{
		ID: "mig-dry", Version: "4.0.0", Type: "sql",
	}, 1))

	a.SetDryRun(true)

	// In dry-run, reads report empty / not-applied regardless of the record.
	executed, err := a.GetExecutedMigrations(ctx)
	require.NoError(t, err)
	assert.Empty(t, executed)

	failed, err := a.GetFailedMigrations(ctx)
	require.NoError(t, err)
	assert.Empty(t, failed)

	applied, err := a.IsMigrationApplied(ctx, "mig-dry")
	require.NoError(t, err)
	assert.False(t, applied)

	// EnsureSchemaTable is a no-op in dry-run.
	assert.NoError(t, a.EnsureSchemaTable(ctx))
}

func TestTrackerAdapter_GetMigration_NotFound(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	adapter := newTrackerAdapter(t, db)
	require.NoError(t, adapter.EnsureSchemaTable(ctx))

	_, err := adapter.GetMigration(ctx, "ghost")
	assert.Error(t, err)
}

func TestTrackerAdapter_ExecutedAtPreserved(t *testing.T) {
	ctx := context.Background()
	db := newMemoryDB(t)
	adapter := newTrackerAdapter(t, db)
	require.NoError(t, adapter.EnsureSchemaTable(ctx))

	before := time.Now().Add(-time.Second)
	require.NoError(t, adapter.RecordMigrationSuccess(ctx, types.MigrationRecord{
		ID: "mig-time", Version: "5.0.0", Type: "sql",
	}, 1))

	executed, err := adapter.GetExecutedMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, executed, 1)
	assert.True(t, executed[0].ExecutedAt.After(before))
}

// --- helpers ------------------------------------------------------------------------

func writeSQL(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

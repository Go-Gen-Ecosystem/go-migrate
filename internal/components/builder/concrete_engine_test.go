/**
 * @file concrete_engine_test.go
 * @description Behaviour tests for ConcreteMigrationEngine. A real up-migration
 *              is driven through the engine against an in-memory SQLite database:
 *              the real DatabaseMigrationExecutor applies the SQL and the real
 *              DatabaseMigrationTracker records applied state.
 * @author Admilson B. F. Cossa
 */

package builder

import (
	"context"
	"testing"

	adapters "github.com/go-gen-ecosystem/go-migrate/internal/components/adapter"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/executor"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// realBackend is a thin types.EngineBackend used purely to satisfy the engine's
// backend contract. Lock and status operations are no-ops; the executor performs
// the real database work.
type realBackend struct {
	dialect  string
	statuses map[string]string
}

func newRealBackend() *realBackend {
	return &realBackend{dialect: "sqlite", statuses: map[string]string{}}
}

func (b *realBackend) AcquireLock(context.Context) error                           { return nil }
func (b *realBackend) ReleaseLock(context.Context) error                           { return nil }
func (b *realBackend) Apply(context.Context, types.MigrationScript) error          { return nil }
func (b *realBackend) Rollback(context.Context, types.MigrationScript) error       { return nil }
func (b *realBackend) ReadStatus(context.Context) ([]types.MigrationStatus, error) { return nil, nil }
func (b *realBackend) UpdateStatus(_ context.Context, m types.MigrationScript, status string) error {
	b.statuses[m.ID] = status
	return nil
}
func (b *realBackend) GetDialect() string         { return b.dialect }
func (b *realBackend) SupportsTransactions() bool { return true }
func (b *realBackend) SupportsRollback() bool     { return false }

func newConcreteEngineDB(t *testing.T) *gorm.DB {
	t.Helper()
	// A per-test named in-memory database keeps tables isolated between tests
	// while still allowing the shared-cache handle GORM's driver needs.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	// Pin to a single connection: a shared-cache in-memory SQLite database is
	// only visible across connections while at least one is open, and a single
	// connection guarantees writes are visible to subsequent reads.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// newConcreteEngine wires a ConcreteMigrationEngine with a real executor and
// tracker over the given DB, plus no-op backend/lock and pass-through
// validator/resolver.
func newConcreteEngine(t *testing.T, db *gorm.DB, backend *realBackend) *ConcreteMigrationEngine {
	t.Helper()
	cfg := types.MigrationConfig{TableName: "migration_history"}
	dbTracker, err := tracker.NewDatabaseMigrationTracker(db, "migration_history")
	require.NoError(t, err)
	trk := adapters.NewTrackerAdapter(dbTracker, db)
	require.NoError(t, trk.EnsureSchemaTable(context.Background()))

	return &ConcreteMigrationEngine{
		engineType: "sqlite",
		config:     types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}, Migration: cfg},
		backend:    backend,
		executor:   executor.NewDatabaseMigrationExecutor(db, &cfg),
		tracker:    trk,
		lockMgr:    fakeLockMgr{},
		validator:  fakeValidator{},
		resolver:   sortingResolver{},
		options:    map[string]interface{}{"supports_transactions": true},
	}
}

func TestConcreteEngine_Up_AppliesRealMigration(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	backend := newRealBackend()
	eng := newConcreteEngine(t, db, backend)

	scripts := []types.MigrationScript{
		{ID: "m1", Version: "1", Type: types.MigrationTypeSQL, Content: "CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT);"},
	}

	result, err := eng.Up(ctx, scripts)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 1, result.ExecutedCount)
	assert.Empty(t, result.FailedScripts)

	// The migration actually created the table in the real database.
	var count int64
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='widgets'",
	).Scan(&count).Error)
	assert.Equal(t, int64(1), count, "widgets table must exist after Up")

	// The backend received the status update.
	assert.Equal(t, "applied", backend.statuses["m1"])

	// The engine is connected after Up.
	assert.True(t, eng.IsConnected())
}

func TestConcreteEngine_Up_SkipsAppliedMigration(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())

	scripts := []types.MigrationScript{
		{ID: "m2", Version: "1", Type: types.MigrationTypeSQL, Content: "CREATE TABLE gizmos (id INTEGER PRIMARY KEY);"},
	}

	// Record the migration as already applied so Up should skip it.
	require.NoError(t, eng.tracker.RecordMigrationSuccess(ctx, types.MigrationRecord{
		ID: "m2", Version: "1", Type: "sql",
	}, 1))

	result, err := eng.Up(ctx, scripts)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SkippedCount)
	assert.Equal(t, 0, result.ExecutedCount)

	// Because it was skipped, the CREATE TABLE was never executed.
	var count int64
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='gizmos'",
	).Scan(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestConcreteEngine_Up_ExecutionFailureRecorded(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())

	scripts := []types.MigrationScript{
		{ID: "bad", Version: "1", Type: types.MigrationTypeSQL, Content: "THIS IS NOT VALID SQL;"},
	}

	result, err := eng.Up(ctx, scripts)
	require.NoError(t, err) // Up returns a result, not an error, for exec failures
	assert.False(t, result.Success)
	assert.Equal(t, 1, result.FailedCount)
	require.Len(t, result.FailedScripts, 1)
	assert.Equal(t, "EXECUTION_FAILED", result.FailedScripts[0].ErrorCode)
}

func TestConcreteEngine_Validate(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())

	res, err := eng.Validate(ctx, []types.MigrationScript{
		{ID: "v1", Version: "1", Type: types.MigrationTypeSQL, Content: "CREATE TABLE t (id INT);"},
	})
	require.NoError(t, err)
	assert.True(t, res.Valid)
	assert.Equal(t, 1, res.ValidatedCount)
}

func TestConcreteEngine_Validate_Invalid(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())
	eng.validator = rejectingValidator{}

	res, err := eng.Validate(ctx, []types.MigrationScript{
		{ID: "v2", Version: "1", Type: types.MigrationTypeSQL, Content: "x"},
	})
	require.NoError(t, err)
	assert.False(t, res.Valid)
	assert.Equal(t, 1, res.ErrorCount)
	require.Len(t, res.ValidationErrors, 1)
}

func TestConcreteEngine_DryRun(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())

	scripts := []types.MigrationScript{
		{ID: "d1", Version: "1", Type: types.MigrationTypeSQL, Content: "CREATE TABLE d (id INT);"},
	}
	res, err := eng.DryRun(ctx, scripts)
	require.NoError(t, err)
	assert.Len(t, res.WouldExecute, 1)
	assert.Empty(t, res.WouldSkip)
	assert.Greater(t, int64(res.EstimatedTime), int64(0))

	// DryRun must not create the table.
	var count int64
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='d'",
	).Scan(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestConcreteEngine_ConnectDisconnect(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())

	assert.False(t, eng.IsConnected())
	require.NoError(t, eng.Connect(ctx))
	assert.True(t, eng.IsConnected())
	// Idempotent connect.
	require.NoError(t, eng.Connect(ctx))
	require.NoError(t, eng.Disconnect(ctx))
	assert.False(t, eng.IsConnected())
	// Idempotent disconnect.
	require.NoError(t, eng.Disconnect(ctx))
}

func TestConcreteEngine_GetEngineTypeAndFeatures(t *testing.T) {
	db := newConcreteEngineDB(t)
	eng := newConcreteEngine(t, db, newRealBackend())
	assert.Equal(t, "sqlite", eng.GetEngineType())

	features := eng.GetSupportedFeatures()
	assert.Contains(t, features, "up")
	assert.Contains(t, features, "down")
	assert.Contains(t, features, "transactions") // options enabled it
}

func TestConcreteEngine_Down_RollsBackByVersion(t *testing.T) {
	ctx := context.Background()
	db := newConcreteEngineDB(t)
	backend := newRealBackend()
	eng := newConcreteEngine(t, db, backend)

	// Seed two applied migrations at versions 1 and 2.
	require.NoError(t, eng.tracker.RecordMigrationSuccess(ctx, types.MigrationRecord{ID: "r1", Version: "1", Type: "sql"}, 1))
	require.NoError(t, eng.tracker.RecordMigrationSuccess(ctx, types.MigrationRecord{ID: "r2", Version: "2", Type: "sql"}, 1))

	// Roll back everything with a version greater than "1" (i.e. r2).
	result, err := eng.Down(ctx, "1")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 1, result.ExecutedCount)
	assert.Equal(t, "rolled_back", backend.statuses["r2"])
}

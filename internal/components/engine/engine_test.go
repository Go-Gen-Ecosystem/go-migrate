/**
 * @file engine_test.go
 * @description Behavioral tests for the migration engine against in-memory/file-backed sqlite.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/engine/engine_test.go
 */

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// newTestConfig builds a sqlite-backed config pointing at a fresh temp DB and
// migrations directory. Each call gets an isolated on-disk sqlite file so that
// tests never share tracking state.
func newTestConfig(t *testing.T) (types.Config, string) {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "engine.db")
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	cfg := types.Config{
		Environment: "test",
		Database:    types.DatabaseConfig{Driver: "sqlite", DSN: dsn},
		Migration: types.MigrationConfig{
			TableName:        "migration_history",
			LockTableName:    "migration_locks",
			MigrationsPath:   migDir,
			MaxRollbackDepth: 10,
			StopOnError:      true,
			// A positive lock timeout is required: AcquireLock treats a zero
			// timeout as an already-expired deadline and fails immediately.
			LockTimeout: 30 * time.Second,
		},
	}
	return cfg, migDir
}

// writeSQLMigration writes a +migrate formatted SQL file so the loader assigns a
// version, description and rollback SQL exactly like a real migration.
func writeSQLMigration(t *testing.T, dir, filename, upSQL, downSQL string) {
	t.Helper()
	content := "-- +migrate Up\n" + upSQL + "\n-- +migrate Down\n" + downSQL + "\n"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write migration %s: %v", filename, err)
	}
}

func TestNewMigrationEngine_EmptyDriverRejected(t *testing.T) {
	_, err := NewMigrationEngine(types.Config{})
	if err == nil {
		t.Fatal("expected error for config without a database driver")
	}
}

func TestNewMigrationEngine_ValidNotInitialized(t *testing.T) {
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("NewMigrationEngine: %v", err)
	}
	if eng == nil {
		t.Fatal("engine is nil")
	}
	if eng.initialized {
		t.Error("engine should not be initialized until Initialize is called")
	}
}

func TestNewMigrationEngineWithFactory_EmptyDriverRejected(t *testing.T) {
	_, err := NewMigrationEngineWithFactory(types.Config{}, false)
	if err == nil {
		t.Fatal("expected error for config without a database driver")
	}
}

func TestNewMigrationEngineWithFactory_WiresComponents(t *testing.T) {
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngineWithFactory(cfg, false)
	if err != nil {
		t.Fatalf("NewMigrationEngineWithFactory: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if !eng.initialized {
		t.Error("factory engine should be initialized")
	}
	if eng.loader == nil || eng.tracker == nil || eng.validator == nil || eng.executor == nil || eng.lockMgr == nil {
		t.Error("factory engine left a core component unwired")
	}
}

func TestInitialize_Idempotent(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("first Initialize: %v", err)
	}
	if !eng.initialized {
		t.Fatal("engine not marked initialized")
	}
	// A second call must be a no-op and not error.
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
}

func TestStatus_EmptyDatabase(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Status auto-initializes.
	status, err := eng.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.TotalMigrations != 0 {
		t.Errorf("expected 0 total migrations, got %d", status.TotalMigrations)
	}
	if status.PendingCount != 0 {
		t.Errorf("expected 0 pending, got %d", status.PendingCount)
	}
	if status.IsLocked {
		t.Error("fresh database should not report a held lock")
	}
}

func TestStatus_WithPendingMigrations(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_create_users.sql",
		"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		"DROP TABLE users;")
	writeSQLMigration(t, migDir, "002_create_orders.sql",
		"CREATE TABLE orders (id INTEGER PRIMARY KEY);",
		"DROP TABLE orders;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	status, err := eng.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.TotalMigrations != 2 {
		t.Errorf("expected 2 total migrations, got %d", status.TotalMigrations)
	}
	// Nothing applied yet, so both are pending.
	if status.PendingCount != 2 {
		t.Errorf("expected 2 pending migrations, got %d", status.PendingCount)
	}
	if status.ExecutedCount != 0 {
		t.Errorf("expected 0 executed migrations, got %d", status.ExecutedCount)
	}
}

func TestStatus_ReflectsAppliedMigration(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_create_users.sql",
		"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		"DROP TABLE users;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Seed the tracker with the applied record for this migration id. The loader
	// derives the id deterministically from the filename, so recompute it the
	// same way to mark it applied.
	migrations, err := eng.loader.LoadMigrations(migDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected 1 migration loaded, got %d", len(migrations))
	}
	now := time.Now()
	rec := &tracker.MigrationRecord{
		ID:         migrations[0].ID,
		Version:    migrations[0].Version,
		State:      "applied",
		ExecutedAt: &now,
	}
	if err := eng.db.Table("migration_history").Create(rec).Error; err != nil {
		t.Fatalf("seed applied record: %v", err)
	}

	status, err := eng.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.PendingCount != 0 {
		t.Errorf("expected 0 pending after applying, got %d", status.PendingCount)
	}
	if status.ExecutedCount != 1 {
		t.Errorf("expected 1 executed, got %d", status.ExecutedCount)
	}
}

func TestStatusWithDryRun_DoesNotCreateTrackingTable(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Drop the tracking table that Initialize created so we can verify dry-run
	// does not recreate it.
	if err := eng.db.Exec("DROP TABLE IF EXISTS migration_history;").Error; err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if _, err := eng.StatusWithDryRun(ctx, true); err != nil {
		t.Fatalf("StatusWithDryRun: %v", err)
	}

	var n int64
	eng.db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='migration_history'").Scan(&n)
	if n != 0 {
		t.Error("dry-run status must not create the tracking table")
	}
}

func TestMigrate_DryRunAcquiresNoLock(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_create_widgets.sql",
		"CREATE TABLE widgets (id INTEGER PRIMARY KEY);",
		"DROP TABLE widgets;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	if err := eng.Migrate(ctx, "", true); err != nil {
		t.Fatalf("dry-run Migrate: %v", err)
	}

	// The dry-run pipeline skips the acquire_lock step, so no lock is ever held.
	locked, err := eng.lockMgr.IsLocked(ctx, cfg.Migration.LockTableName)
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if locked {
		t.Error("dry-run migrate must not acquire a lock")
	}
}

func TestMigrate_ValidMigrationsPipelineSucceeds(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_create_a.sql",
		"CREATE TABLE a (id INTEGER PRIMARY KEY);",
		"DROP TABLE a;")
	writeSQLMigration(t, migDir, "002_create_b.sql",
		"CREATE TABLE b (id INTEGER PRIMARY KEY);",
		"DROP TABLE b;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Migrate runs the full pipeline (schema table, lock, load, filter, resolve,
	// validate) and releases the lock afterwards.
	if err := eng.Migrate(ctx, "", false); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// The lock must be released after the run completes.
	locked, err := eng.lockMgr.IsLocked(ctx, cfg.Migration.LockTableName)
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if locked {
		t.Error("migration lock should be released after Migrate returns")
	}
}

// TestMigrate_AppliesAndRecordsMigrations proves the primary up-migration path
// actually executes the migration SQL and records each migration as applied —
// not merely that the pipeline returns without error. It also asserts
// idempotency: a second run applies nothing new and does not error.
func TestMigrate_AppliesAndRecordsMigrations(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_create_a.sql",
		"CREATE TABLE a (id INTEGER PRIMARY KEY);", "DROP TABLE a;")
	writeSQLMigration(t, migDir, "002_create_b.sql",
		"CREATE TABLE b (id INTEGER PRIMARY KEY);", "DROP TABLE b;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if err := eng.Migrate(ctx, "", false); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// The migrations must have actually run: both tables now exist.
	for _, table := range []string{"a", "b"} {
		if !eng.db.Migrator().HasTable(table) {
			t.Errorf("table %q was not created — Migrate did not execute the migration SQL", table)
		}
	}

	// Both migrations must be recorded as applied.
	applied, err := eng.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied migrations recorded, got %d", len(applied))
	}

	// Idempotency: a second run re-loads the same migrations, filters out the
	// already-applied ones, and executes nothing new (re-running CREATE TABLE
	// would otherwise fail).
	if err := eng.Migrate(ctx, "", false); err != nil {
		t.Fatalf("second Migrate must be a clean no-op: %v", err)
	}
	applied2, err := eng.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations (2nd): %v", err)
	}
	if len(applied2) != 2 {
		t.Errorf("second run must not re-record; expected 2 applied, got %d", len(applied2))
	}
}

func TestMigrate_TargetFiltersToVersion(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_a.sql", "CREATE TABLE a (id INTEGER);", "DROP TABLE a;")
	writeSQLMigration(t, migDir, "002_b.sql", "CREATE TABLE b (id INTEGER);", "DROP TABLE b;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// Targeting version 001 must complete without error (target filtering path).
	if err := eng.Migrate(ctx, "001", false); err != nil {
		t.Fatalf("Migrate with target: %v", err)
	}
}

func TestMigrate_InvalidSQLFailsValidation(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	// StopOnError is true. The up-script has unbalanced parentheses, which the
	// composite validator's syntax check rejects inside the validate_migrations
	// pipeline step, aborting the run.
	writeSQLMigration(t, migDir, "001_bad.sql",
		"CREATE TABLE broken (id INTEGER PRIMARY KEY;",
		"DROP TABLE broken;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	err = eng.Migrate(ctx, "", false)
	if err == nil {
		t.Fatal("expected validation failure for a migration with unbalanced parentheses")
	}

	// Even on failure the lock must not remain held.
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	locked, lerr := eng.lockMgr.IsLocked(ctx, cfg.Migration.LockTableName)
	if lerr != nil {
		t.Fatalf("IsLocked: %v", lerr)
	}
	if locked {
		t.Error("lock must be released even when the pipeline fails")
	}
}

func TestRollback_ExecutesRollbackSQLAndRemovesRecord(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Create a real table and an applied record whose rollback drops it.
	if err := eng.db.Exec("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);").Error; err != nil {
		t.Fatalf("create gadgets: %v", err)
	}
	now := time.Now()
	rec := &tracker.MigrationRecord{
		ID:          "mg1",
		Version:     "0001",
		State:       "applied",
		CanRollback: true,
		RollbackSQL: "DROP TABLE gadgets;",
		ExecutedAt:  &now,
	}
	if err := eng.db.Table("migration_history").Create(rec).Error; err != nil {
		t.Fatalf("seed record: %v", err)
	}

	if err := eng.Rollback(ctx, 1); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// The rollback SQL must have actually dropped the table.
	var n int64
	eng.db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='gadgets'").Scan(&n)
	if n != 0 {
		t.Error("rollback did not execute the DROP TABLE rollback SQL")
	}

	// And the tracker record must be gone.
	applied, err := eng.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("expected 0 applied migrations after rollback, got %d", len(applied))
	}
}

func TestRollback_ZeroStepsIsNoop(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	// steps <= 0 returns early with no error, even before touching the tracker.
	if err := eng.Rollback(ctx, 0); err != nil {
		t.Fatalf("Rollback(0): %v", err)
	}
	if err := eng.Rollback(ctx, -3); err != nil {
		t.Fatalf("Rollback(-3): %v", err)
	}
}

func TestRollback_DryRunDoesNotRemoveRecords(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	if err := eng.db.Exec("CREATE TABLE keepme (id INTEGER PRIMARY KEY);").Error; err != nil {
		t.Fatalf("create keepme: %v", err)
	}
	now := time.Now()
	rec := &tracker.MigrationRecord{
		ID:          "keep1",
		Version:     "0001",
		State:       "applied",
		CanRollback: true,
		RollbackSQL: "DROP TABLE keepme;",
		ExecutedAt:  &now,
	}
	if err := eng.db.Table("migration_history").Create(rec).Error; err != nil {
		t.Fatalf("seed record: %v", err)
	}

	if err := eng.RollbackWithDryRun(ctx, 1, true); err != nil {
		t.Fatalf("RollbackWithDryRun: %v", err)
	}

	// Dry-run must not drop the table.
	var n int64
	eng.db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='keepme'").Scan(&n)
	if n == 0 {
		t.Error("dry-run rollback must not execute rollback SQL")
	}
	// And the record must remain.
	applied, err := eng.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations: %v", err)
	}
	if len(applied) != 1 {
		t.Errorf("dry-run rollback must keep the record, got %d applied", len(applied))
	}
}

func TestRollback_RespectsMaxRollbackDepth(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	// Cap rollback depth at 1: even asking for 5 steps only rolls back the most
	// recent applied migration.
	cfg.Migration.MaxRollbackDepth = 1

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	for i := 1; i <= 3; i++ {
		table := fmt.Sprintf("t%d", i)
		if err := eng.db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY);").Error; err != nil {
			t.Fatalf("create %s: %v", table, err)
		}
		now := time.Now().Add(time.Duration(i) * time.Second)
		rec := &tracker.MigrationRecord{
			ID:          fmt.Sprintf("m%d", i),
			Version:     fmt.Sprintf("000%d", i),
			State:       "applied",
			CanRollback: true,
			RollbackSQL: "DROP TABLE " + table + ";",
			ExecutedAt:  &now,
		}
		if err := eng.db.Table("migration_history").Create(rec).Error; err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}

	if err := eng.Rollback(ctx, 5); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	applied, err := eng.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations: %v", err)
	}
	// Depth cap of 1 means exactly one migration was removed, leaving two.
	if len(applied) != 2 {
		t.Errorf("expected 2 remaining applied migrations (depth capped at 1), got %d", len(applied))
	}
}

func TestValidate_LoadsAndValidatesMigrations(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	writeSQLMigration(t, migDir, "001_ok.sql",
		"CREATE TABLE ok (id INTEGER PRIMARY KEY);",
		"DROP TABLE ok;")

	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if err := eng.Validate(ctx); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestInfo_ReportsConfiguredMetadata(t *testing.T) {
	ctx := context.Background()
	cfg, migDir := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	info, err := eng.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.DatabaseType != "sqlite" {
		t.Errorf("expected database type sqlite, got %s", info.DatabaseType)
	}
	if info.MigrationsPath != migDir {
		t.Errorf("expected migrations path %s, got %s", migDir, info.MigrationsPath)
	}
	if info.TableName != "migration_history" {
		t.Errorf("expected table name migration_history, got %s", info.TableName)
	}
	if info.Environment != "test" {
		t.Errorf("expected environment test, got %s", info.Environment)
	}
}

func TestClose_MarksUninitialized(t *testing.T) {
	ctx := context.Background()
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if eng.initialized {
		t.Error("engine should be uninitialized after Close")
	}
	if eng.db != nil {
		t.Error("db handle should be cleared after Close")
	}
	// Close on an already-closed engine must remain safe.
	if err := eng.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRollbackManagerAccessors(t *testing.T) {
	cfg, _ := newTestConfig(t)
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer func() { _ = eng.Close() }()

	if eng.GetRollbackManager() != nil {
		t.Error("expected nil rollback manager by default")
	}
	rm := &fakeRollbackManager{}
	eng.SetRollbackManager(rm)
	if eng.GetRollbackManager() != rm {
		t.Error("SetRollbackManager/GetRollbackManager round-trip failed")
	}
}

// fakeRollbackManager is a minimal stub used only to verify the accessor
// round-trip; its methods are never invoked by these tests.
type fakeRollbackManager struct{}

func (f *fakeRollbackManager) CanRollback(ctx context.Context, migrationID string) (bool, []string, error) {
	return false, nil, nil
}
func (f *fakeRollbackManager) RollbackToVersion(ctx context.Context, version string) (*types.RollbackResult, error) {
	return nil, nil
}
func (f *fakeRollbackManager) RollbackCount(ctx context.Context, count int) (*types.RollbackResult, error) {
	return nil, nil
}
func (f *fakeRollbackManager) RollbackMigration(ctx context.Context, migrationID string) (*types.RollbackResult, error) {
	return nil, nil
}
func (f *fakeRollbackManager) GetRollbackPlan(ctx context.Context, migrationIDs []string) (*types.RollbackPlan, error) {
	return nil, nil
}
func (f *fakeRollbackManager) GetAdvancedManager() types.AdvancedRollbackManager { return nil }

func TestPipeline_RunsStepsInOrder(t *testing.T) {
	ctx := context.Background()
	var order []string
	p := NewPipeline(
		NewStepFunc("first", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
			order = append(order, "first")
			return nil
		}),
		NewStepFunc("second", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
			order = append(order, "second")
			return nil
		}),
	)
	if err := p.Run(ctx, nil, &PipelineContext{}); err != nil {
		t.Fatalf("pipeline Run: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("steps ran out of order: %v", order)
	}
}

func TestPipeline_FailsFastOnStepError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	ran := 0
	p := NewPipeline(
		NewStepFunc("ok", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
			ran++
			return nil
		}),
		NewStepFunc("fail", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
			ran++
			return boom
		}),
		NewStepFunc("never", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
			ran++
			return nil
		}),
	)
	err := p.Run(ctx, nil, &PipelineContext{})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom error, got %v", err)
	}
	if ran != 2 {
		t.Errorf("expected pipeline to stop after the failing step (2 runs), got %d", ran)
	}
}

func TestPipeline_AddAppendsSteps(t *testing.T) {
	ctx := context.Background()
	count := 0
	p := NewPipeline()
	p.Add(NewStepFunc("a", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
		count++
		return nil
	}))
	p.Add(NewStepFunc("b", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
		count++
		return nil
	}))
	if err := p.Run(ctx, nil, &PipelineContext{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 appended steps to run, got %d", count)
	}
}

func TestStepFunc_NameAndRun(t *testing.T) {
	s := NewStepFunc("named", func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
		return nil
	})
	if s.Name() != "named" {
		t.Errorf("expected step name 'named', got %q", s.Name())
	}
	if err := s.Run(context.Background(), nil, &PipelineContext{}); err != nil {
		t.Errorf("Run returned error: %v", err)
	}
}

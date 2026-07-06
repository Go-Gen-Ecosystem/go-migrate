package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestProbeRollback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "engine.db")
	migDir := filepath.Join(dir, "migrations")
	_ = os.MkdirAll(migDir, 0o755)
	cfg := types.Config{
		Environment: "test",
		Database:    types.DatabaseConfig{Driver: "sqlite", DSN: dsn},
		Migration: types.MigrationConfig{
			TableName:        "migration_history",
			LockTableName:    "migration_locks",
			MigrationsPath:   migDir,
			MaxRollbackDepth: 10,
		},
	}
	eng, err := NewMigrationEngine(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Initialize(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer eng.Close()

	// Create a real table and seed an applied migration record whose rollback drops it.
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

	var n int64
	eng.db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='gadgets'").Scan(&n)
	t.Logf("before rollback gadgets exists=%v", n > 0)

	if err := eng.Rollback(ctx, 1); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	eng.db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='gadgets'").Scan(&n)
	t.Logf("after rollback gadgets exists=%v", n > 0)

	applied, err := eng.tracker.GetAppliedMigrations(ctx)
	t.Logf("applied after rollback: %d err=%v", len(applied), err)
}

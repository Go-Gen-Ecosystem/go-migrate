// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunCallbackFailureBlocksReplay(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE audit (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	m := newM(t, db, files("beforeMigrate.sql", "INSERT INTO audit VALUES (1); INSERT INTO missing VALUES (2);"))
	if _, err := m.Up(ctx); err == nil {
		t.Fatal("first Up unexpectedly succeeded")
	}
	_, retryErr := m.Up(ctx)
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 || !errors.Is(retryErr, ErrDirty) {
		t.Fatalf("after retry audit has %d rows, Up = %v; want one row and ErrDirty", n, retryErr)
	}
}

func TestAfterMigrateFailureNeedsScopedCallbackRepair(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "callback.db")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f := files(
		"1_a.up.sql", "CREATE TABLE audit (id INTEGER);",
		"1_a.down.sql", "DROP TABLE audit;",
		"afterMigrate.sql", "INSERT INTO audit VALUES (1); INSERT INTO missing VALUES (2);",
	)
	m := newM(t, db, f)
	result, err := m.Up(ctx)
	if !errors.Is(err, ErrCallbackRecoveryRequired) || !errors.Is(err, ErrDirty) || count(t, db, "SELECT COUNT(*) FROM audit") != 1 {
		t.Fatalf("partial afterMigrate: %v", err)
	}
	eq(t, versions(result), []string{"1"})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// A separate pool (as in a new invocation) sees the durable marker even
	// when all migrations have committed and the callback file is removed.
	db2, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	delete(f, "afterMigrate.sql")
	m = newM(t, db2, f)
	assertCallbackRecovery(t, m, "1", 1)
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"up", func() error { _, err := m.Up(ctx); return err }},
		{"down", func() error { _, err := m.Down(ctx, DownTarget{}); return err }},
		{"redo", func() error { _, err := m.Redo(ctx); return err }},
		{"plan up", func() error { _, err := m.Plan(ctx, ""); return err }},
		{"plan down", func() error { _, err := m.PlanDown(ctx, DownTarget{}); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, ErrCallbackRecoveryRequired) {
				t.Fatalf("got %v; want callback recovery error", err)
			}
		})
	}
	if err := m.MarkApplied(ctx, RunCallbacksRepairKey); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("MarkApplied(marker) = %v", err)
	}
	if _, err := m.Repair(ctx, RepairTarget{Migration: RunCallbacksRepairKey, Action: RepairAcceptChecksum}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("accepting marker checksum = %v", err)
	}
	if err := m.Tag(ctx, "release"); err != nil {
		t.Fatal(err)
	}
	st, err := m.Status(ctx)
	if err != nil || st.Migrations[0].Tag != "release" || !st.CallbackRecoveryRequired {
		t.Fatalf("status after tag = %+v, %v", st, err)
	}
	if _, err := db2.Exec("DELETE FROM audit WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	clearCallbackRecovery(t, m)
	if _, err := m.Up(ctx); err != nil || !tableExists(t, db2, "audit") || count(t, db2, "SELECT COUNT(*) FROM audit") != 0 {
		t.Fatalf("Up after reconciling the callback = %v", err)
	}
}

func TestRunCallbackSurvivesMigrationOnlyRepair(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
	}{
		{name: "before succeeds then migration fails", file: "beforeMigrate.sql"},
		{name: "only failure callback", file: "afterMigrateError.sql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := openDB(t)
			if _, err := db.Exec("CREATE TABLE audit (id INTEGER)"); err != nil {
				t.Fatal(err)
			}
			m := newM(t, db, files(
				"1_bad.sql", "-- +migrate Up notransaction\nCREATE TABLE half (id INTEGER); INSERT INTO missing VALUES (1);",
				tc.file, "INSERT INTO audit VALUES (1);",
			))
			if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) || !tableExists(t, db, "half") {
				t.Fatalf("Up = %v", err)
			}
			assertCallbackRecovery(t, m, "", 1)
			if _, err := db.Exec("DROP TABLE half"); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Repair(ctx, RepairTarget{Migration: "1", Action: RepairClearFailed}); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) || tableExists(t, db, "half") {
				t.Fatalf("Up after migration-only repair = %v", err)
			}
			if got := count(t, db, "SELECT COUNT(*) FROM audit"); got != 1 {
				t.Fatalf("callback ran %d times, want one", got)
			}
			if _, err := db.Exec("DELETE FROM audit WHERE id = 1"); err != nil {
				t.Fatal(err)
			}
			clearCallbackRecovery(t, m)
			if err := m.Validate(ctx); err != nil {
				t.Fatalf("Validate after both repairs = %v", err)
			}
		})
	}
}

func TestRunCallbackRecoveryOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE audit (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	m := newM(t, db, files(
		"1_noop.sql", "SELECT 1;",
		"beforeMigrate.sql", "INSERT INTO audit VALUES (1);",
		"afterMigrateError.sql", "INSERT INTO audit VALUES (2);",
	), WithHooks(Hooks{Before: func(context.Context, Event) error { cancel(); return nil }}))
	if _, err := m.Up(ctx); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrCallbackRecoveryRequired) {
		t.Fatalf("cancelled Up = %v", err)
	}
	assertCallbackRecovery(t, m, "", 1)
	if n := count(t, db, "SELECT COUNT(*) FROM audit"); n != 2 {
		t.Fatalf("detached afterMigrateError did not run, audit count %d", n)
	}
}

func TestRunCallbackRecoveryOnFinalizeFailure(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE audit (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	m := newM(t, db, files("1_init.sql", "SELECT 1;"))
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	// Fail the marker deletion in the real history table after a callback
	// has already written successfully. Neither a failed delete nor a retry
	// can hide/replay the callback's effect.
	if _, err := db.Exec(`CREATE TRIGGER block_cleanup BEFORE DELETE ON gomigrate_history
		WHEN OLD.kind = 'callbacks' BEGIN SELECT RAISE(ABORT, 'cannot finalize callback'); END`); err != nil {
		t.Fatal(err)
	}
	m = newM(t, db, files("1_init.sql", "SELECT 1;", "afterMigrate.sql", "INSERT INTO audit VALUES (1);"))
	if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) {
		t.Fatalf("Up with a failed finalization = %v", err)
	}
	assertCallbackRecovery(t, m, "1", 1)
	if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) || count(t, db, "SELECT COUNT(*) FROM audit") != 1 {
		t.Fatalf("Up after failed finalization = %v", err)
	}
}

func TestRunCallbackRejectsAbandonedSQLiteTransaction(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE audit (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	m := newM(t, db, files("beforeMigrate.sql", "INSERT INTO audit VALUES (1); SAVEPOINT dangling; INSERT INTO audit VALUES (2);",
		"1_migration.sql", "CREATE TABLE not_attempted (id INTEGER);"))
	if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) {
		t.Fatalf("callback started an unfinished SQLite transaction: %v", err)
	}
	assertCallbackRecovery(t, m, "", 1)
	if tableExists(t, db, "not_attempted") || count(t, db, "SELECT COUNT(*) FROM audit") != 1 {
		t.Fatal("run proceeded or committed the abandoned transaction")
	}
}

func TestRunCallbackPreservesSQLiteSessionState(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	db.SetMaxOpenConns(1)
	m := newM(t, db, files(
		"beforeMigrate.sql", "PRAGMA foreign_keys = ON; CREATE TEMP TABLE callback_config (id INTEGER); INSERT INTO callback_config VALUES (7);",
		"1_read_config.sql", "CREATE TABLE audit AS SELECT id FROM callback_config;",
		"afterMigrate.sql", "INSERT INTO audit SELECT id FROM callback_config; DROP TABLE callback_config;",
	))
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM audit WHERE id = 7"); got != 2 {
		t.Fatalf("callback session config not shared, got %d rows", got)
	}
}

func assertCallbackRecovery(t *testing.T, m *Migrator, current string, migrations int) {
	t.Helper()
	ctx := context.Background()
	st, err := m.Status(ctx)
	if err != nil || !st.Dirty || !st.CallbackRecoveryRequired || st.Current != current || len(st.Migrations) != migrations {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	if err := m.Validate(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) || !errors.Is(err, ErrDirty) ||
		errors.Is(err, ErrMissingMigration) || !slices.Equal(ErrorCodes(err), []string{"callback_recovery_required", "dirty"}) {
		t.Fatalf("Validate = %v (%v)", err, ErrorCodes(err))
	}
}

func clearCallbackRecovery(t *testing.T, m *Migrator) {
	t.Helper()
	ctx := context.Background()
	target := RepairTarget{Migration: RunCallbacksRepairKey, Action: RepairClearFailed}
	p, err := m.PlanRepair(ctx, target)
	if err != nil || p.RepairTarget != target || p.State != StateFailed || p.StoredKind != "callbacks" {
		t.Fatalf("callback repair preview = %+v, %v", p, err)
	}
	if err := m.Validate(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) {
		t.Fatalf("preview changed callback history: %v", err)
	}
	result, err := m.Repair(ctx, target)
	if err != nil || result.RemovedFailed != 1 || len(result.Realigned) != 0 {
		t.Fatalf("callback repair = %+v, %v", result, err)
	}
}

func conformRunCallbackRecovery(t *testing.T, tg target) {
	t.Helper()
	ctx := context.Background()
	db := tg.open(t)
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE callback_audit (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	newCallbackMigrator := func(sql string) *Migrator {
		t.Helper()
		m, err := New(db, tg.dialect, WithFS(files("afterMigrate.sql", sql)))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := newCallbackMigrator("INSERT INTO callback_audit VALUES (1);")
	for want := 1; want <= 2; want++ {
		// Approval includes callbacks, even with an empty migration plan. Every
		// acknowledged run leaves no marker and may deliberately repeat.
		plan, id, err := m.PlanWithID(ctx, "")
		if err != nil || len(plan) != 0 || id == "" {
			t.Fatalf("callback-only plan = %v, %q, %v", plan, id, err)
		}
		if result, err := m.Up(ctx, ExpectTargetPlan(id)); err != nil || len(result.Applied) != 0 {
			t.Fatalf("callback-only Up = %+v, %v", result, err)
		}
		if got := count(t, db, "SELECT COUNT(*) FROM callback_audit"); got != want {
			t.Fatalf("got %d committed callback writes, want %d", got, want)
		}
		if err := m.Validate(ctx); err != nil {
			t.Fatalf("completed callback left recovery pending: %v", err)
		}
	}
	m = newCallbackMigrator("INSERT INTO callback_audit VALUES (3); INSERT INTO no_such_table VALUES (9);")
	for range 2 {
		if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) {
			t.Fatalf("Up = %v", err)
		}
		assertCallbackRecovery(t, m, "", 0)
		if got := count(t, db, "SELECT COUNT(*) FROM callback_audit"); got != 3 {
			t.Fatalf("failed callback commits once, count = %d", got)
		}
	}
	if _, err := db.Exec("DELETE FROM callback_audit WHERE id = 3"); err != nil {
		t.Fatal(err)
	}
	clearCallbackRecovery(t, m)
}

func TestMySQLRunCallbackAutocommit(t *testing.T) {
	t.Run("autocommit disabled", mysqlCallbackAutocommitDisabled)
	t.Run("explicit transaction", mysqlCallbackExplicitTransaction)
}

func mysqlCallbackAutocommitDisabled(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := openMySQL(t)
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE callback_audit (id INTEGER) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	m, err := New(db, "mysql", WithFS(files(
		"afterMigrate.sql", "INSERT INTO callback_audit VALUES (1); SET autocommit = 0; INSERT INTO callback_audit VALUES (2);",
	)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); !errors.Is(err, ErrCallbackRecoveryRequired) {
		t.Fatalf("callback left autocommit disabled: %v", err)
	}
	assertCallbackRecovery(t, m, "", 0)
	if committed := count(t, db, "SELECT COUNT(*) FROM callback_audit"); committed != 1 {
		t.Fatalf("expected one committed write and one rolled back, got %d", committed)
	}
}

func mysqlCallbackExplicitTransaction(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	db := openMySQL(t)
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE callback_audit (id INTEGER) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	failedMigration := errors.New("no-tx migration failed inside a transaction")
	m, err := New(db, "mysql", WithFS(files(
		"beforeMigrate.sql", "INSERT INTO callback_audit VALUES (1);",
		// If this runs in the abandoned transaction, it would commit the
		// failed migration's pending data as a side effect of the DDL.
		"afterMigrateError.sql", "CREATE TABLE cleanup_should_not_run (id INTEGER);",
	)), WithGoMigrations(GoMigration{Version: "1", NoTx: true, Up: func(ctx context.Context, ex Executor) error {
		if _, err := ex.ExecContext(ctx, "START TRANSACTION"); err != nil {
			return err
		}
		if _, err := ex.ExecContext(ctx, "INSERT INTO callback_audit VALUES (2)"); err != nil {
			return err
		}
		return failedMigration
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); !errors.Is(err, failedMigration) || !errors.Is(err, ErrCallbackRecoveryRequired) ||
		!strings.Contains(err.Error(), "MySQL session is inside a transaction") {
		t.Fatalf("error callback encountered an open transaction: %v", err)
	}
	assertCallbackRecovery(t, m, "", 1)
	if committed := count(t, db, "SELECT COUNT(*) FROM callback_audit"); committed != 1 {
		t.Fatalf("expected one committed write and one rolled back, got %d", committed)
	}
	if got := count(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'cleanup_should_not_run'"); got != 0 {
		t.Fatal("afterMigrateError ran inside an abandoned user transaction")
	}
}

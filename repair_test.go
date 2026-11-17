// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
)

const repairTable = "repair_history"

func repairFixture(t *testing.T, db *sql.DB, driver string) *Migrator {
	t.Helper()
	fsys := files("1_a.sql", "CREATE TABLE a (id INT);", "2_b.sql", "CREATE TABLE b (id INT);",
		"3_c.sql", "CREATE TABLE c (id INT);", "4_d.sql", "CREATE TABLE d (id INT);")
	m, err := New(db, driver, WithFS(fsys), WithTable(repairTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Seed two dirty history rows with the schema already restored. Up stops
	// after the first failure, so it cannot produce this mixed history itself.
	ctx := t.Context()
	if err := m.historySession(ctx, func(conn *sql.Conn, table dialect.Table) error {
		store := history.New(m.dialect, table)
		for _, fixture := range []struct{ version, table string }{{"3", "c"}, {"4", "d"}} {
			if _, err := conn.ExecContext(ctx, "DROP TABLE "+fixture.table); err != nil {
				return err
			}
			if err := store.MarkDirty(ctx, conn, fixture.version); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fsys["1_a.sql"].Data = []byte("CREATE TABLE a (id INT NOT NULL);")
	fsys["2_b.sql"].Data = []byte("CREATE TABLE b (id INT NOT NULL);")
	m, err = New(db, driver, WithFS(fsys), WithTable(repairTable))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func repairHistory(t *testing.T, m *Migrator) []history.Record {
	t.Helper()
	var rows []history.Record
	if err := m.historySession(t.Context(), func(conn *sql.Conn, table dialect.Table) error {
		var err error
		rows, err = history.New(m.dialect, table).List(t.Context(), conn)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestBareRepairDoesNotWriteHistory(t *testing.T) {
	db := openDB(t)
	m := repairFixture(t, db, dialect.SQLite)
	before := repairHistory(t, m)
	res, err := m.Repair(t.Context())
	if after := repairHistory(t, m); !reflect.DeepEqual(before, after) || !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("bare Repair = %+v, %v; history rows before/after = %d/%d, want invalid argument and unchanged history", res, err, len(before), len(after))
	}
}

func conformScopedRepair(t *testing.T, tg target) {
	db := tg.open(t)
	db.SetMaxOpenConns(1) // the apply must use only the pinned session
	m := repairFixture(t, db, tg.dialect)
	ctx := t.Context()
	before := repairHistory(t, m)
	clear := RepairTarget{Migration: "0003.0", Action: RepairClearFailed}
	accept := RepairTarget{Migration: "0001", Action: RepairAcceptChecksum}
	if _, err := m.Repair(ctx, clear, accept); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Repair with two targets = %v, want ErrInvalidArgument", err)
	}
	for _, tc := range []struct {
		target RepairTarget
		want   error
	}{
		{RepairTarget{Migration: "3"}, ErrInvalidArgument},
		{RepairTarget{Action: RepairClearFailed}, ErrInvalidArgument},
		{RepairTarget{Migration: "1", Action: RepairClearFailed}, ErrInvalidArgument},
		{RepairTarget{Migration: "3", Action: RepairAcceptChecksum}, ErrInvalidArgument},
		{RepairTarget{Migration: "99", Action: RepairClearFailed}, ErrHistoryRowNotFound},
	} {
		if _, err := m.Repair(ctx, tc.target); !errors.Is(err, tc.want) {
			t.Fatalf("Repair(%+v) = %v, want %v", tc.target, err, tc.want)
		}
	}
	plan, err := m.PlanRepair(ctx, clear)
	if err != nil || plan.Migration != "3" || plan.State != StateFailed || plan.StoredChecksum != before[2].Checksum {
		t.Fatalf("PlanRepair clear failed = %+v, %v", plan, err)
	}
	plan, err = m.PlanRepair(ctx, accept)
	if err != nil || plan.Migration != "1" || plan.State != StateModified || plan.StoredChecksum != before[0].Checksum ||
		plan.NewChecksum == "" || plan.NewChecksum == plan.StoredChecksum {
		t.Fatalf("PlanRepair accept checksum = %+v, %v", plan, err)
	}
	assertRepairHistory(t, m, before)

	// Accept one edit without modifying the other edit or either dirty row.
	if res, err := m.Repair(ctx, accept); err != nil || res.RemovedFailed != 0 || !slices.Equal(res.Realigned, []string{"1"}) {
		t.Fatalf("Repair accept checksum = %+v, %v", res, err)
	}
	want := slices.Clone(before)
	want[0].Checksum = plan.NewChecksum
	assertRepairHistory(t, m, want)
	if _, err := m.Repair(ctx, accept); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Repair without drift = %v, want ErrInvalidArgument", err)
	}

	// Clear one failed row without changing the other one or accepting drift.
	if res, err := m.Repair(ctx, clear); err != nil || res.RemovedFailed != 1 || len(res.Realigned) != 0 {
		t.Fatalf("Repair clear failed = %+v, %v", res, err)
	}
	want = slices.Delete(want, 2, 3)
	assertRepairHistory(t, m, want)
	if _, err := m.Repair(ctx, clear); !errors.Is(err, ErrHistoryRowNotFound) {
		t.Fatalf("Repair after removing row = %v, want ErrHistoryRowNotFound", err)
	}
	assertRepairHistory(t, m, want)
}

func assertRepairHistory(t *testing.T, m *Migrator, want []history.Record) {
	t.Helper()
	if got := repairHistory(t, m); !reflect.DeepEqual(want, got) {
		t.Fatalf("history changed unexpectedly:\nwant %+v\n got %+v", want, got)
	}
}

func TestRepairRollsBackFailedHistoryWrite(t *testing.T) {
	db := openDB(t)
	m := repairFixture(t, db, dialect.SQLite)
	before := repairHistory(t, m)
	// SQLite's RAISE(FAIL) does not back out changes made by the failing
	// statement. Only rolling back the repair transaction restores the row.
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER reject_repair AFTER UPDATE OF checksum ON repair_history
		WHEN NEW.version = '1' BEGIN SELECT RAISE(FAIL, 'injected history failure'); END`); err != nil {
		t.Fatal(err)
	}
	if res, err := m.Repair(t.Context(), RepairTarget{Migration: "1", Action: RepairAcceptChecksum}); err == nil ||
		res.RemovedFailed != 0 || len(res.Realigned) != 0 {
		t.Fatalf("Repair with failing history write = %+v, %v", res, err)
	}
	assertRepairHistory(t, m, before)
}

func TestPlanRepairDoesNotCreateHistoryOrLock(t *testing.T) {
	db := openDB(t)
	m := newM(t, db, nil, WithTable(repairTable))
	if _, err := m.PlanRepair(t.Context(), RepairTarget{Migration: "1", Action: RepairClearFailed}); !errors.Is(err, ErrHistoryRowNotFound) {
		t.Fatalf("PlanRepair with no history = %v, want ErrHistoryRowNotFound", err)
	}
	if tableExists(t, db, repairTable) || tableExists(t, db, repairTable+"_lock") {
		t.Fatal("PlanRepair created a history or lock table")
	}
}

func TestClearFailedRepeatableWithoutSource(t *testing.T) {
	ctx := t.Context()
	db := openDB(t)
	m := newM(t, db, files("R__Refresh_Views.sql", "-- +migrate Up notransaction\nINSERT INTO missing VALUES (1);"))
	if _, err := m.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Fatalf("Up = %v, want ErrDirty", err)
	}
	m = newM(t, db, nil)
	st, err := m.Status(ctx)
	if err != nil || !st.Dirty || len(st.Migrations) != 1 {
		t.Fatalf("Status without repeatable source = %+v, %v", st, err)
	}
	target := RepairTarget{Migration: st.Migrations[0].Label(), Action: RepairClearFailed}
	if plan, err := m.PlanRepair(ctx, target); err != nil || plan.Migration != "R:refresh views" {
		t.Fatalf("PlanRepair for %q = %+v, %v", target.Migration, plan, err)
	}
	if res, err := m.Repair(ctx, target); err != nil || res.RemovedFailed != 1 {
		t.Fatalf("Repair for %q = %+v, %v", target.Migration, res, err)
	}
	if st, err := m.Status(ctx); err != nil || st.Dirty || len(st.Migrations) != 0 {
		t.Fatalf("Status after removing failed repeatable = %+v, %v", st, err)
	}
}

func TestRepairDoesNotChangeBaselineOrAcceptMissingSource(t *testing.T) {
	ctx := t.Context()
	db := openDB(t)
	m := newM(t, db, files("1_a.sql", "CREATE TABLE a (id INTEGER);", "2_b.sql", "CREATE TABLE b (id INTEGER);"))
	if err := m.Baseline(ctx, "1", "existing schema"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Repair(ctx, RepairTarget{Migration: "1", Action: RepairAcceptChecksum}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Repair baseline = %v, want ErrInvalidArgument", err)
	}
	m = newM(t, db, nil)
	if _, err := m.Repair(ctx, RepairTarget{Migration: "2", Action: RepairAcceptChecksum}); !errors.Is(err, ErrMissingMigration) {
		t.Fatalf("Repair missing source = %v, want ErrMissingMigration", err)
	}
}

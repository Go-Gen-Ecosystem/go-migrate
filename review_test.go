// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

// Regression tests for the second independent review: each reproduces a
// finding that was proven with a probe before the fix.

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
)

func TestMarkRanRollbackKeepsPreexistingObject(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE legacy (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO legacy VALUES (42)"); err != nil {
		t.Fatal(err)
	}
	m := newM(t, db, files("1_legacy.sql",
		"-- +migrate Precondition onFail=MARK_RAN expect=0 SELECT COUNT(*) FROM sqlite_master WHERE name = 'legacy'\n"+
			"-- +migrate Up\nCREATE TABLE legacy (id INTEGER);\n-- +migrate Down\nDROP TABLE legacy;"))
	res, err := m.Up(ctx)
	if err != nil || !res.Applied[0].MarkedRan {
		t.Fatalf("Up = %+v, %v", res, err)
	}
	if st, _ := m.Status(ctx); st.Migrations[0].State != StateMarkedRan {
		t.Fatalf("state = %s, want %s", st.Migrations[0].State, StateMarkedRan)
	}
	res, err = m.Down(ctx, DownTarget{})
	if err != nil || !res.Applied[0].MarkedRan {
		t.Fatalf("Down = %+v, %v", res, err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM legacy").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rolling back a MARK_RAN migration must not run its Down SQL: rows=%d err=%v", n, err)
	}
	if st, _ := m.Status(ctx); st.Pending != 1 {
		t.Fatalf("the migration must be pending again: %+v", st)
	}
	// Redo of a marked migration re-evaluates the precondition instead of running Up.
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if res, err := m.Redo(ctx); err != nil || !res.Applied[0].MarkedRan {
		t.Fatalf("Redo = %+v, %v", res, err)
	}
}

func TestContinuePreconditionDoesNotBreakLaterRuns(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_maybe.sql", "-- +migrate Precondition onFail=CONTINUE expect=1 SELECT COUNT(*) FROM sqlite_master WHERE name = 'gate'\nCREATE TABLE maybe (id INTEGER);",
		"2_b.sql", "CREATE TABLE b (id INTEGER);",
	)
	res, err := newM(t, db, fsys).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"2"})
	if _, err := newM(t, db, fsys).Up(ctx); err != nil {
		t.Fatalf("a CONTINUE-skipped migration must not become out_of_order: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE gate (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	res, err = newM(t, db, fsys).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1"})
}

func TestRepairNeverRealignsRepeatables(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files("R__v.sql", "DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT 1 AS x;")
	if _, err := newM(t, db, fsys).Up(ctx); err != nil {
		t.Fatal(err)
	}
	fsys["R__v.sql"] = &fstest.MapFile{Data: []byte("DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT 2 AS x;")}
	m := newM(t, db, fsys)
	if rr, err := m.Repair(ctx); err != nil || len(rr.Realigned) != 0 {
		t.Fatalf("Repair = %+v, %v", rr, err)
	}
	if res, err := m.Up(ctx); err != nil || len(res.Applied) != 1 {
		t.Fatalf("the changed repeatable must still be applied: %+v, %v", res, err)
	}
}

func TestDownToTagUsesApplicationOrder(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	two := "-- +migrate Up\nCREATE TABLE b (id INTEGER);\n-- +migrate Down\nDROP TABLE b;"
	three := "-- +migrate Up\nCREATE TABLE c (id INTEGER);\n-- +migrate Down\nDROP TABLE c;"
	if _, err := newM(t, db, files("1_a.sql", "CREATE TABLE a (id INTEGER);", "3_c.sql", three)).Up(ctx); err != nil {
		t.Fatal(err)
	}
	m := newM(t, db, files("1_a.sql", "CREATE TABLE a (id INTEGER);", "2_b.sql", two, "3_c.sql", three), WithAllowOutOfOrder(true))
	if _, err := m.Up(ctx); err != nil { // applies 2 after 3
		t.Fatal(err)
	}
	if err := m.Tag(ctx, "rel1"); err != nil { // tags 2, the latest applied
		t.Fatal(err)
	}
	plan, err := m.PlanDown(ctx, DownTarget{Tag: "rel1"})
	if err != nil || len(plan) != 0 {
		t.Fatalf("nothing was applied after the tag, plan = %v, %v", plan, err)
	}
}

func TestPlanSimulatesBaselineOnMigrate(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE a (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	fsys := files("1_a.sql", "CREATE TABLE a (id INTEGER);", "2_b.sql", "CREATE TABLE b (id INTEGER);", "3_c.sql", "CREATE TABLE c (id INTEGER);")
	m := newM(t, db, fsys, WithBaselineOnMigrate("2"))
	plan, err := m.Plan(ctx, "")
	if err != nil || len(plan) != 1 || plan[0].Version != "3" {
		t.Fatalf("the dry run must show what Up will really do: %v, %v", plan, err)
	}
	if _, err := New(db, "sqlite", WithBaselineOnMigrate("abc")); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("an invalid baseline version must fail New: %v", err)
	}
}

func TestRunAlwaysIsNotPending(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files("1_t.sql", "CREATE TABLE hits (n INTEGER);", "R__touch.sql", "-- +migrate RunAlways\nINSERT INTO hits VALUES (1);")
	m := newM(t, db, fsys)
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := m.Status(ctx)
	if st.Pending != 0 || st.Migrations[1].State != StateRunAlways {
		t.Fatalf("RunAlways must not count as pending: %+v", st)
	}
}

func TestDownRefusesDriftAndPartialRollbackInvalidates(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_a.sql", "-- +migrate Up\nCREATE TABLE a (id INTEGER);\n-- +migrate Down\nDROP TABLE nope;",
		"2_b.sql", "-- +migrate Up\nCREATE TABLE b (id INTEGER);\n-- +migrate Down\nDROP TABLE b;",
		"R__v.sql", "DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT id FROM a;",
	)
	if _, err := newM(t, db, fsys).Up(ctx); err != nil {
		t.Fatal(err)
	}
	// Down of 2 succeeds, down of 1 fails: repeatables must still be invalidated.
	res, err := newM(t, db, fsys).Down(ctx, DownTarget{Steps: 2})
	if err == nil || len(res.Applied) != 1 {
		t.Fatalf("Down = %+v, %v", res, err)
	}
	if st, _ := newM(t, db, fsys).Status(ctx); st.Migrations[2].State != StateOutdated {
		t.Fatalf("repeatable state after partial rollback = %s", st.Migrations[2].State)
	}
	fsys["1_a.sql"] = &fstest.MapFile{Data: []byte("-- +migrate Up\nCREATE TABLE a (id BIGINT);\n-- +migrate Down\nDROP TABLE a;")}
	if _, err := newM(t, db, fsys).Down(ctx, DownTarget{}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("rolling back an edited migration must fail: %v", err)
	}
}

func TestPlanIDBindsApprovalToExecution(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files("1_a.sql", "CREATE TABLE a (id INTEGER);")
	m := newM(t, db, fsys)
	plan, err := m.Plan(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	id := m.PlanID(Up, plan)
	// The pending set changes after approval: the run must refuse.
	fsys["2_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE b (id INTEGER);")}
	if _, err := newM(t, db, fsys).Up(ctx, ExpectPlan(id)); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("Up with a stale plan = %v, want ErrPlanChanged", err)
	}
	if tableExists(t, db, "a") {
		t.Fatal("nothing may run when the plan changed")
	}
	if _, err := m.Up(ctx, ExpectPlan(id)); err != nil {
		t.Fatalf("Up with the approved plan: %v", err)
	}
	if _, err := m.Down(ctx, DownTarget{Steps: 1, To: "0"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("conflicting down selectors = %v", err)
	}
}

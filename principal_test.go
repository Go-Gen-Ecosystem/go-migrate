// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

// Regression tests for the principal engineering review: each test pins one
// finding through the public API.

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// #5: callbacks are part of what a run does, so they are part of the plan.
func TestCallbacksArePartOfThePlan(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_a.sql", "CREATE TABLE a (id INTEGER);",
		"afterMigrate.sql", "INSERT INTO a VALUES (1);",
		"beforeEachMigrate__log.sql", "SELECT 1;",
	)
	m := newM(t, db, fsys)
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	plan, err := m.Plan(ctx, "")
	if err != nil || len(plan) != 0 {
		t.Fatalf("Plan = %v, %v", plan, err)
	}
	cbs := m.Callbacks(Up)
	if len(cbs) != 2 || cbs[0].Event != "beforeEachMigrate" || cbs[1].Event != "afterMigrate" ||
		!slices.Equal(cbs[1].Statements, []string{"INSERT INTO a VALUES (1)"}) {
		t.Fatalf("Callbacks(Up) = %+v", cbs)
	}
	if got := m.Callbacks(Down); len(got) != 0 {
		t.Fatalf("rollbacks run no callbacks, got %+v", got)
	}

	// An edited callback changes the plan ID, so an old approval is refused.
	approved := m.PlanID(Up, plan)
	fsys["afterMigrate.sql"].Data = []byte("DELETE FROM a;")
	edited := newM(t, db, fsys)
	if edited.PlanID(Up, plan) == approved {
		t.Fatal("plan ID must cover callback SQL")
	}
	if _, err := edited.Up(ctx, ExpectPlan(approved)); !errors.Is(err, ErrPlanChanged) {
		t.Fatalf("Up with a stale approval = %v, want ErrPlanChanged", err)
	}
	// Plans for another history table never share an ID.
	other := newM(t, db, fsys, WithTable("other_history"))
	if other.PlanID(Up, plan) == edited.PlanID(Up, plan) {
		t.Fatal("plan ID must cover the history table")
	}
}

// #5: migrations with preconditions are flagged as conditional in plans.
func TestPreconditionMigrationsAreConditional(t *testing.T) {
	m := newM(t, openDB(t), files(
		"1_a.sql", "-- +migrate Precondition onFail=MARK_RAN expect=0 SELECT COUNT(*) FROM sqlite_master WHERE name = 'a'\nCREATE TABLE a (id INTEGER);",
		"2_b.sql", "CREATE TABLE b (id INTEGER);",
	))
	ms := m.Migrations()
	if !ms[0].Conditional || ms[1].Conditional {
		t.Fatalf("Conditional = %v, %v", ms[0].Conditional, ms[1].Conditional)
	}
}

// #5: callback SQL is linted like migration SQL.
func TestCallbacksAreLinted(t *testing.T) {
	findings, err := Lint("sqlite", WithFS(files("1_a.sql", "CREATE TABLE a (id INTEGER);", "afterMigrate.sql", "DELETE FROM a;")))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Rule == "unbounded-write" && f.Event == "afterMigrate" && f.Source == "afterMigrate.sql" && f.Version == "" {
			return
		}
	}
	t.Fatalf("callback finding missing: %+v", findings)
}

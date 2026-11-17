// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Direction is the direction a migration runs in.
type Direction int

// Migration directions.
const (
	Up Direction = iota
	Down
)

func (d Direction) valid() bool { return d == Up || d == Down }

func (d Direction) String() string {
	if d == Down {
		return "down"
	}
	if d == Up {
		return "up"
	}
	return fmt.Sprintf("invalid direction (%d)", d)
}

// Executor runs SQL. It is satisfied by *sql.DB, *sql.Tx and *sql.Conn. Go
// migrations receive a *sql.Tx on databases with transactional DDL
// (PostgreSQL, SQLite) unless they set NoTx; on MySQL, or with NoTx, they
// receive the run's *sql.Conn and follow the dirty-row protocol (see ErrDirty).
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GoMigration is a migration written in Go, for changes SQL alone cannot
// express (data backfills that call application code, conditional logic).
type GoMigration struct {
	Version     string
	Description string
	Up          func(ctx context.Context, ex Executor) error
	// Down is optional; without it the migration is irreversible.
	Down func(ctx context.Context, ex Executor) error
	// NoTx runs the migration outside a transaction.
	NoTx bool
}

// Migration describes one migration known to the Migrator.
type Migration struct {
	// Version is the canonical version ("" for repeatable migrations).
	Version     string
	Description string
	Repeatable  bool
	// Source is the file name(s), or "go" for a Go migration.
	Source string
	// Checksum is the SHA-256 of the normalized file content ("" for Go).
	Checksum   string
	NoTx       bool
	Reversible bool
	// Conditional is set when the migration has preconditions: a run may
	// skip it or record it without executing its SQL.
	Conditional bool
}

// Callback is an SQL callback file and the statements it runs, after
// placeholder substitution. Event is its Flyway event name.
type Callback struct {
	Event      string
	Source     string
	Statements []string
}

// Label names the migration in logs and output: its version, or
// "R__<description>" for a repeatable migration.
func (m Migration) Label() string {
	if m.Repeatable {
		return "R__" + m.Description
	}
	return m.Version
}

// State is the state of a migration relative to the database.
type State string

// Migration states reported by Status.
const (
	StatePending    State = "pending"
	StateApplied    State = "applied"
	StateFailed     State = "failed"   // dirty: failed part-way, needs repair
	StateModified   State = "modified" // applied, but the file changed since
	StateMissing    State = "missing"  // applied, but no longer in the source
	StateOutdated   State = "outdated" // repeatable whose content changed
	StateBaseline   State = "baseline" // the baseline marker row
	StateBelowBase  State = "below baseline"
	StateOutOfOrder State = "out of order" // pending, older than the latest applied
	StateSkipped    State = "skipped"      // excluded by DBMS, Context or Labels
	StateMarkedRan  State = "marked ran"   // recorded by a MARK_RAN precondition, never executed
	StateRunAlways  State = "run always"   // RunAlways repeatable: re-applied on every Up
)

// Info is a migration together with its state and history row (if any).
type Info struct {
	Migration
	State State
	// The following are set only for migrations with a history row.
	InstalledOn   time.Time
	InstalledBy   string
	ExecutionTime time.Duration
	// Tag is the release tag on this migration, if any (see Migrator.Tag).
	Tag string
}

// Status is a read-only snapshot of the database's migration state.
type Status struct {
	// Current is the highest applied version ("" if none).
	Current string
	// Dirty is true when a migration or run-level SQL callback needs recovery.
	Dirty bool
	// CallbackRecoveryRequired is true when a run-level callback may have
	// changed the database. See ErrCallbackRecoveryRequired.
	CallbackRecoveryRequired bool
	// Pending counts migrations Up would apply.
	Pending    int
	Migrations []Info
}

// Result reports what a run did, in execution order.
type Result struct {
	Direction Direction
	Applied   []Applied
}

// Applied is one migration executed by a run.
type Applied struct {
	Migration
	Duration time.Duration
	// MarkedRan is true when a precondition with onFail=MARK_RAN failed: the
	// migration was recorded as applied without running.
	MarkedRan bool
}

// Event is passed to hooks around each migration.
type Event struct {
	Migration Migration
	Direction Direction
	// Duration and Err are set only for the After hook.
	Duration time.Duration
	Err      error
}

// Hooks are optional callbacks around each migration. Before may veto a
// migration by returning an error. Hooks run while the lock is held.
type Hooks struct {
	Before func(ctx context.Context, e Event) error
	After  func(ctx context.Context, e Event)
}

// DownTarget selects what a rollback undoes. Set at most one field; the
// zero value rolls back the most recently applied migration.
type DownTarget struct {
	// Steps rolls back the last Steps applied migrations, newest first.
	Steps int
	// To rolls back every migration with a version above To ("0": all).
	To string
	// Tag rolls back every migration applied after the one tagged Tag.
	Tag string
}

func (t DownTarget) validate() (DownTarget, error) {
	set := 0
	for _, ok := range []bool{t.Steps != 0, t.To != "", t.Tag != ""} {
		if ok {
			set++
		}
	}
	switch {
	case set > 1:
		return t, fmt.Errorf("set only one of Steps, To and Tag: %w", ErrInvalidArgument)
	case t.Steps < 0:
		return t, fmt.Errorf("steps must be positive, got %d: %w", t.Steps, ErrInvalidArgument)
	case set == 0:
		t.Steps = 1
	}
	return t, nil
}

// RunOption adjusts one run (Up, UpTo, Down, Redo).
type RunOption func(*runOptions)

type runOptions struct {
	expectPlan    string
	expectPlanSet bool
	expectTarget  bool
}

// ExpectPlan makes the run verify, under the migration lock, that it is about
// to execute exactly the plan identified by id (see Migrator.PlanID) and to
// fail with ErrPlanChanged otherwise. It binds a human's or agent's approval
// of a dry run to what actually runs.
func ExpectPlan(id string) RunOption {
	return func(o *runOptions) { o.expectPlan, o.expectPlanSet, o.expectTarget = id, true, false }
}

// ExpectTargetPlan only permits the SQL target and exact plan approved with
// PlanWithID or PlanDownWithID. Legacy content-only PlanID values are rejected.
func ExpectTargetPlan(id string) RunOption {
	return func(o *runOptions) { o.expectPlan, o.expectPlanSet, o.expectTarget = id, true, true }
}

func applyRunOptions(opts []RunOption) runOptions {
	var o runOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

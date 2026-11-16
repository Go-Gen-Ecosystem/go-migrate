package migrate

import (
	"context"
	"database/sql"
	"time"
)

// Direction is the direction a migration runs in.
type Direction int

// Migration directions.
const (
	Up Direction = iota
	Down
)

func (d Direction) String() string {
	if d == Down {
		return "down"
	}
	return "up"
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
)

// Info is a migration together with its state and history row (if any).
type Info struct {
	Migration
	State State
	// The following are set only for migrations with a history row.
	InstalledOn   time.Time
	InstalledBy   string
	ExecutionTime time.Duration
}

// Status is a read-only snapshot of the database's migration state.
type Status struct {
	// Current is the highest applied version ("" if none).
	Current string
	// Dirty is true when a migration failed part-way (see ErrDirty).
	Dirty bool
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

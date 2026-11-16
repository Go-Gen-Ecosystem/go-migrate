// Package migrate applies versioned SQL and Go migrations to PostgreSQL,
// MySQL/MariaDB and SQLite.
//
// A Migrator is built once from a *sql.DB and a migration source and is then
// immutable and safe for concurrent use. Runs that change the database (Up,
// Down, Repair, Baseline) hold a cross-process lock: a session advisory lock
// on PostgreSQL and MySQL (released by the server if the process dies) and a
// heartbeat-renewed lease on SQLite. On databases with transactional DDL each
// migration and its history row commit atomically; otherwise a failed
// migration leaves a "dirty" row that blocks further runs until Repair.
//
// The package itself imports no SQL driver; register the one you use, e.g.
//
//	import _ "github.com/jackc/pgx/v5/stdlib" // "pgx"
//	import _ "github.com/go-sql-driver/mysql" // "mysql"
//	import _ "modernc.org/sqlite"             // "sqlite", pure Go
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/user"
	"regexp"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Migrator applies migrations to one database. Create it with New.
type Migrator struct {
	db      *sql.DB
	dialect *dialect.Dialect
	table   dialect.Table
	store   *history.Store
	opts    options
	units   []*unit          // versioned in version order, then repeatables
	byKey   map[string]*unit // history key -> unit, for O(1) lookups
}

// unit is a migration plus the means to execute it.
type unit struct {
	Migration
	key   string
	kind  history.Kind
	stmts [2][]string
	fns   [2]func(ctx context.Context, ex Executor) error
}

var goVersion = regexp.MustCompile(`^\d+(\.\d+)*$`)

// New builds a Migrator for db. dialectName is "postgres", "mysql" or
// "sqlite" (aliases such as "pgx", "mariadb" and "sqlite3" are accepted).
// The migration source is loaded and validated immediately, so malformed
// files and duplicate versions fail here rather than mid-run.
func New(db *sql.DB, dialectName string, opts ...Option) (*Migrator, error) {
	if db == nil {
		return nil, errors.New("migrate: nil *sql.DB")
	}
	d, err := dialect.Lookup(dialectName)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	o := defaults()
	for _, opt := range opts {
		opt(&o)
	}
	if o.lockWait <= 0 {
		o.lockWait = DefaultLockWait
	}
	if o.installedBy == "" {
		o.installedBy = currentUser()
	}
	tbl, err := dialect.ParseTable(o.table)
	if err != nil {
		return nil, fmt.Errorf("migrate: history table: %w", err)
	}

	m := &Migrator{db: db, dialect: d, table: tbl, store: history.New(d, tbl), opts: o, byKey: map[string]*unit{}}
	if err := m.load(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return m, nil
}

// Migrations returns every known migration in execution order.
func (m *Migrator) Migrations() []Migration {
	out := make([]Migration, len(m.units))
	for i, u := range m.units {
		out[i] = u.Migration
	}
	return out
}

func (m *Migrator) load() error {
	var files []*source.File
	if m.opts.fsys != nil {
		var err error
		if files, err = source.Load(m.opts.fsys); err != nil {
			return err
		}
	}
	lx := m.dialect.Lexical()
	for _, f := range files {
		u := &unit{
			Migration: Migration{
				Version: f.Version, Description: f.Description, Repeatable: f.Repeatable,
				Source: f.Name, Checksum: f.Checksum, NoTx: f.Script.NoTx,
				Reversible: !f.Script.Down.Empty(),
			},
			kind:  history.KindSQL,
			stmts: [2][]string{f.Script.Up.Statements(lx), f.Script.Down.Statements(lx)},
		}
		if f.Repeatable {
			u.kind = history.KindRepeatable
		}
		if err := m.add(u); err != nil {
			return err
		}
	}
	for _, g := range m.opts.goMigrations {
		if !goVersion.MatchString(g.Version) || g.Up == nil {
			return fmt.Errorf("go migration %q: needs a numeric version and an Up func", g.Version)
		}
		u := &unit{
			Migration: Migration{
				Version: source.Canonical(g.Version), Description: g.Description,
				Source: "go", NoTx: g.NoTx, Reversible: g.Down != nil,
			},
			kind: history.KindGo,
			fns:  [2]func(context.Context, Executor) error{g.Up, g.Down},
		}
		if err := m.add(u); err != nil {
			return err
		}
	}
	sortUnits(m.units)
	return nil
}

func (m *Migrator) add(u *unit) error {
	u.key = u.Version
	if u.Repeatable {
		u.key = history.RepeatablePrefix + strings.ToLower(u.Description)
	}
	if prev, ok := m.byKey[u.key]; ok {
		return fmt.Errorf("%w: %s and %s", source.ErrDuplicateVersion, prev.Source, u.Source)
	}
	m.byKey[u.key] = u
	m.units = append(m.units, u)
	return nil
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}

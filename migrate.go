// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

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
	"database/sql"
	"errors"
	"fmt"
	"os/user"
	"regexp"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

// Migrator applies migrations to one database. Create it with New.
//
// Migrator is a facade: each method validates its arguments and delegates to
// a focused component (catalog: what exists; planner: what to do; runner and
// maintenance: doing it under the lock; applier: one migration at a time).
type Migrator struct {
	db      *sql.DB
	dialect *dialect.Dialect
	table   dialect.Table
	opts    options
	cat     *catalog
	plan    planner
}

var goVersion = regexp.MustCompile(`^\d+(\.\d+)*$`)

// Identifier limits derived from the history DDL (version VARCHAR(128)) and
// the 63/64-character identifier limits of PostgreSQL and MySQL.
const (
	maxTableName  = 48
	maxVersionKey = 128
)

// New builds a Migrator for db. dialectName is "postgres", "mysql" or
// "sqlite" (aliases such as "pgx", "mariadb" and "sqlite3" are accepted).
// The migration source is loaded and validated immediately, so malformed
// files and duplicate versions fail here rather than mid-run.
func New(db *sql.DB, dialectName string, opts ...Option) (*Migrator, error) {
	if db == nil {
		return nil, errors.New("migrate: nil *sql.DB")
	}
	return build(db, dialectName, opts)
}

// build constructs a Migrator; db may be nil for database-free analysis (Lint).
func build(db *sql.DB, dialectName string, opts []Option) (*Migrator, error) {
	d, err := dialect.Lookup(dialectName)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	o := defaults()
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.installedBy == "" {
		o.installedBy = currentUser()
	}
	tbl, err := dialect.ParseTable(o.table)
	if err != nil {
		return nil, fmt.Errorf("migrate: history table: %w", err)
	}
	if len(tbl.Name) > maxTableName {
		// Derived names (<table>_version_uk, <table>_lock) must stay within
		// MySQL's and PostgreSQL's 64/63-character identifier limits.
		return nil, fmt.Errorf("migrate: history table name %q is longer than %d characters", tbl.Name, maxTableName)
	}

	if o.baselineOnUp != "" && !goVersion.MatchString(o.baselineOnUp) {
		return nil, fmt.Errorf("migrate: baseline-on-migrate version %q: %w", o.baselineOnUp, ErrInvalidVersion)
	}
	if o.placeholders != nil {
		builtins := map[string]string{"migrate:dialect": d.Name(), "migrate:table": tbl.String(), "migrate:user": o.installedBy}
		for k, v := range builtins {
			if _, set := o.placeholders[k]; !set {
				o.placeholders[k] = v
			}
		}
	}
	cat, err := loadCatalog(d, &o)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	cat.scope = d.Name() + "/" + tbl.String()
	return &Migrator{
		db: db, dialect: d, table: tbl, opts: o, cat: cat,
		plan: planner{cat: cat, allowOOO: o.allowOOO, allowMissing: o.allowMissing},
	}, nil
}

// Migrations returns every known migration in execution order.
func (m *Migrator) Migrations() []Migration { return migrationsOf(m.cat.units) }

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "unknown"
}

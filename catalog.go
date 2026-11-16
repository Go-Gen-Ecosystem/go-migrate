// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// unit is a migration plus everything needed to execute it.
type unit struct {
	Migration
	key   string
	kind  history.Kind
	stmts [2][]string
	fns   [2]func(ctx context.Context, ex Executor) error
	// skipped: excluded by DBMS/Context/Labels for this Migrator;
	// otherDBMS: excluded by a DBMS directive (never linted for this dialect).
	skipped    bool
	otherDBMS  bool
	retryable  bool // has a CONTINUE precondition: may run later than its order
	runAlways  bool
	pre        []precondition
	lintIgnore []string
}

// exec runs the unit's SQL statements (or Go func) for dir on ex. before, if
// set, runs ahead of every statement (lock fencing for non-transactional runs).
func (u *unit) exec(ctx context.Context, ex Executor, dir Direction, before func(context.Context) error) error {
	if fn := u.fns[dir]; fn != nil {
		if err := fn(ctx, ex); err != nil {
			return &MigrationError{Version: u.Label(), Direction: dir, Err: err}
		}
		return nil
	}
	for i, stmt := range u.stmts[dir] {
		if before != nil {
			if err := before(ctx); err != nil {
				return &MigrationError{Version: u.Label(), Direction: dir, Statement: i + 1, SQL: truncateSQL(stmt), Err: err}
			}
		}
		if _, err := ex.ExecContext(ctx, stmt); err != nil {
			return &MigrationError{Version: u.Label(), Direction: dir, Statement: i + 1, SQL: truncateSQL(stmt), Err: err}
		}
	}
	return nil
}

// catalog is the immutable set of migrations and callbacks known to a
// Migrator: versioned migrations in version order, then repeatables, indexed
// by history key for O(1) lookups. It is built once by load and only read
// afterwards, so it is safe for concurrent use.
type catalog struct {
	units     []*unit
	byKey     map[string]*unit
	callbacks map[string][]callback
	// scope identifies where plans run (dialect and history table), so a
	// plan approved for one target never matches another.
	scope string
}

// historyKey is the version column value for a migration.
func historyKey(m Migration) string {
	if m.Repeatable {
		return history.RepeatablePrefix + strings.ToLower(m.Description)
	}
	return m.Version
}

func (c *catalog) add(u *unit) error {
	u.key = historyKey(u.Migration)
	if len(u.key) > maxVersionKey {
		return fmt.Errorf("%s: version or repeatable description longer than %d characters", u.Source, maxVersionKey)
	}
	if prev, ok := c.byKey[u.key]; ok {
		return fmt.Errorf("%w: %s and %s", source.ErrDuplicateVersion, prev.Source, u.Source)
	}
	c.byKey[u.key] = u
	c.units = append(c.units, u)
	return nil
}

// sort orders versioned migrations by version, then repeatables by
// description; Go and SQL migrations share one ordering.
func (c *catalog) sort() {
	sort.SliceStable(c.units, func(i, j int) bool {
		a, b := c.units[i], c.units[j]
		if a.Repeatable != b.Repeatable {
			return !a.Repeatable
		}
		if a.Repeatable {
			return strings.ToLower(a.Description) < strings.ToLower(b.Description)
		}
		return source.Compare(a.Version, b.Version) < 0
	})
}

// lookupVersion canonicalises v and checks that a versioned migration has it.
func (c *catalog) lookupVersion(v string) (string, error) {
	if !goVersion.MatchString(v) {
		return "", fmt.Errorf("version %q: %w", v, ErrVersionNotFound)
	}
	t := source.Canonical(v)
	if u, ok := c.byKey[t]; !ok || u.Repeatable {
		return "", fmt.Errorf("version %s: %w", v, ErrVersionNotFound)
	}
	return t, nil
}

// statements returns a copy of the SQL mig runs in dir (nil if unknown or Go).
func (c *catalog) statements(mig Migration, dir Direction) []string {
	if u, ok := c.byKey[historyKey(mig)]; ok {
		return slices.Clone(u.stmts[dir])
	}
	return nil
}

func migrationsOf(us []*unit) []Migration {
	out := make([]Migration, len(us))
	for i, u := range us {
		out[i] = u.Migration
	}
	return out
}

// planID fingerprints a plan: direction, then each migration's key, checksum
// and exact statements. Any change to what would run changes the ID.
func (c *catalog) planID(dir Direction, plan []*unit) string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte{0})
			h.Write([]byte(p))
		}
	}
	write(c.scope, dir.String())
	for _, u := range plan {
		write(u.key, u.Checksum)
		write(u.stmts[dir]...)
	}
	for _, cb := range c.callbacksFor(dir) {
		write(cb.Event, cb.Source)
		write(cb.Statements...)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// callbackEvents lists the events a run executes, in execution order.
// Rollbacks run none (Flyway defines callbacks for migrate only).
var callbackEvents = [2][]string{
	Up: {source.BeforeMigrate, source.BeforeEachMigrate, source.AfterEachMigrate, source.AfterMigrate, source.AfterMigrateError},
}

// callbacksFor returns the callbacks a run in dir executes, in event order.
func (c *catalog) callbacksFor(dir Direction) []Callback {
	var out []Callback
	for _, ev := range callbackEvents[dir] {
		for _, cb := range c.callbacks[ev] {
			out = append(out, Callback{Event: ev, Source: cb.name, Statements: slices.Clone(cb.stmts)})
		}
	}
	return out
}

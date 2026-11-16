// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/halolog"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/script"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// cleanupTimeout bounds work that must finish even after ctx is cancelled:
// releasing the lock and recording a change that was already applied.
const cleanupTimeout = 30 * time.Second

// History column limits (see dialect.HistoryDDL).
const (
	maxDescription = 255
	maxScript      = 1024
)

// Pre-declared log keys: halolog emits them without per-call escaping.
var (
	logVersion   = halolog.Key("version")
	logDirection = halolog.Key("direction")
	logDuration  = halolog.Key("duration_ms")
	logCount     = halolog.Key("count")
	logSource    = halolog.Key("source")
)

// Up applies every pending migration, then any new or changed repeatable
// migrations. Nothing pending is not an error: the Result is simply empty.
func (m *Migrator) Up(ctx context.Context) (*Result, error) { return m.up(ctx, "") }

// UpTo applies pending migrations up to and including version. Repeatable
// migrations are not run.
func (m *Migrator) UpTo(ctx context.Context, version string) (*Result, error) {
	if version == "" {
		return nil, fmt.Errorf("empty target: %w", ErrVersionNotFound)
	}
	return m.up(ctx, version)
}

// Down rolls back the last steps applied migrations, newest first. It fails
// before changing anything if fewer than steps are applied or any of them is
// irreversible.
func (m *Migrator) Down(ctx context.Context, steps int) (*Result, error) {
	if steps <= 0 {
		return nil, fmt.Errorf("steps must be positive, got %d", steps)
	}
	return m.down(ctx, downSelector{steps: steps})
}

// DownTo rolls back every applied migration with a version above version.
// DownTo(ctx, "0") rolls back everything above the baseline (if any).
func (m *Migrator) DownTo(ctx context.Context, version string) (*Result, error) {
	if version == "" {
		return nil, fmt.Errorf("empty target: %w", ErrVersionNotFound)
	}
	return m.down(ctx, downSelector{to: version})
}

// DownToTag rolls back every migration applied after the one carrying tag
// (Liquibase "rollback to tag").
func (m *Migrator) DownToTag(ctx context.Context, tag string) (*Result, error) {
	if tag == "" {
		return nil, fmt.Errorf("empty tag: %w", ErrTagNotFound)
	}
	return m.down(ctx, downSelector{tag: tag})
}

// Redo rolls back the most recently applied migration and applies it again,
// under a single lock so no other run can interleave. The Result lists the
// re-applied migration.
func (m *Migrator) Redo(ctx context.Context) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.locked(ctx, func(ctx context.Context, a *applier) error { return m.runner(a).redo(ctx, res) })
	return res, err
}

func (m *Migrator) up(ctx context.Context, target string) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.locked(ctx, func(ctx context.Context, a *applier) error { return m.runner(a).up(ctx, target, res) })
	return res, err
}

func (m *Migrator) down(ctx context.Context, sel downSelector) (*Result, error) {
	res := &Result{Direction: Down}
	err := m.locked(ctx, func(ctx context.Context, a *applier) error { return m.runner(a).down(ctx, sel, res) })
	return res, err
}

// locked runs fn while holding the cross-process migration lock. Every
// statement of fn runs on the lock's own connection, so losing a session
// lock fails the work and a one-connection pool cannot deadlock; the history
// table is qualified with the schema resolved before the run, so a
// migration's SET search_path or USE cannot redirect history writes. The
// lock is released even if fn panics or ctx is cancelled.
func (m *Migrator) locked(ctx context.Context, fn func(context.Context, *applier) error) (err error) {
	schema, err := m.dialect.CurrentSchema(ctx, m.db)
	if err != nil {
		return err
	}
	table := m.table.Qualify(schema)
	lock, err := m.dialect.Locker().Lock(ctx, m.db, dialect.LockKey(table), m.opts.lockWait)
	if err != nil {
		return err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if rerr := lock.Release(cctx); rerr != nil {
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", rerr))
		}
	}()
	return fn(ctx, &applier{conn: lock.Conn, table: table, store: history.New(m.dialect, table), lock: lock,
		d: m.dialect, cat: m.cat, opts: &m.opts})
}

func (m *Migrator) runner(a *applier) runner { return runner{a: a, plan: m.plan} }

// runner orchestrates applying and rolling back migrations under one lock.
type runner struct {
	a    *applier
	plan planner
}

func (r runner) snapshot(ctx context.Context) (*snapshot, error) {
	return readSnapshot(ctx, r.a.conn, r.a.store)
}

func (r runner) up(ctx context.Context, target string, res *Result) error {
	a := r.a
	if err := a.store.Ensure(ctx, a.conn); err != nil {
		return err
	}
	s, err := r.snapshot(ctx)
	if err != nil {
		return err
	}
	if s, err = r.baselineOnMigrate(ctx, s); err != nil {
		return err
	}
	plan, err := r.plan.up(s, target)
	if err != nil {
		return err
	}
	if err := a.callbacks(ctx, a.conn, source.BeforeMigrate); err != nil {
		return err
	}
	if err := r.execute(ctx, plan, Up, s, res); err != nil {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		return errors.Join(err, a.callbacks(cctx, a.conn, source.AfterMigrateError))
	}
	return a.callbacks(ctx, a.conn, source.AfterMigrate)
}

func (r runner) down(ctx context.Context, sel downSelector, res *Result) error {
	s, err := r.snapshot(ctx)
	if err != nil {
		return err
	}
	plan, err := r.plan.down(s, sel)
	if err != nil {
		return err
	}
	if err := r.execute(ctx, plan, Down, s, res); err != nil || len(res.Applied) == 0 {
		return err
	}
	return r.invalidateRepeatables(ctx, s)
}

func (r runner) redo(ctx context.Context, res *Result) error {
	s, err := r.snapshot(ctx)
	if err != nil {
		return err
	}
	plan, err := r.plan.down(s, downSelector{steps: 1})
	if err != nil {
		return err
	}
	u := plan[0]
	if _, err := r.a.apply(ctx, u, Down, s); err != nil {
		return err
	}
	if err := r.invalidateRepeatables(ctx, s); err != nil {
		return err
	}
	if s, err = r.snapshot(ctx); err != nil {
		return err
	}
	d, err := r.a.apply(ctx, u, Up, s)
	if err != nil {
		return err
	}
	res.Applied = append(res.Applied, Applied{Migration: u.Migration, Duration: d})
	return nil
}

// execute runs plan in order, appending each success to res, and stops at
// the first failure.
func (r runner) execute(ctx context.Context, plan []*unit, dir Direction, s *snapshot, res *Result) error {
	for _, u := range plan {
		run, err := r.precondition(ctx, u, dir, s, res)
		if err != nil {
			return err
		}
		if !run {
			continue
		}
		d, err := r.a.apply(ctx, u, dir, s)
		if err != nil {
			return err
		}
		res.Applied = append(res.Applied, Applied{Migration: u.Migration, Duration: d})
	}
	r.a.opts.logger.Typed().Int(logCount, len(res.Applied)).Str(logDirection, dir.String()).Info("migration run complete")
	return nil
}

// precondition evaluates u's preconditions (Up only) and reports whether u
// should run; MARK_RAN records it and CONTINUE skips it for this run.
func (r runner) precondition(ctx context.Context, u *unit, dir Direction, s *snapshot, res *Result) (bool, error) {
	if dir != Up || len(u.pre) == 0 {
		return true, nil
	}
	outcome, err := checkPreconditions(ctx, r.a.conn, u.pre)
	if err != nil {
		return false, &MigrationError{Version: u.label(), Direction: dir, Err: err}
	}
	switch outcome {
	case script.Halt:
		return false, fmt.Errorf("migration %s: %w", u.label(), ErrPreconditionFailed)
	case script.Continue:
		r.a.opts.logger.Typed().Str(logVersion, u.label()).Info("precondition not met: skipped for this run")
		return false, nil
	case script.MarkRan:
		if err := r.a.markRan(ctx, u, s); err != nil {
			return false, err
		}
		res.Applied = append(res.Applied, Applied{Migration: u.Migration, MarkedRan: true})
		return false, nil
	default:
		return true, nil
	}
}

// invalidateRepeatables clears the stored checksum of every repeatable
// migration after a rollback, so the next Up re-applies them: a rollback can
// drop the objects they create (a view over a dropped table).
func (r runner) invalidateRepeatables(ctx context.Context, s *snapshot) error {
	for _, rec := range s.records {
		if rec.Kind != history.KindRepeatable {
			continue
		}
		if err := r.a.store.Realign(ctx, r.a.conn, rec.Version, "", history.KindRepeatable); err != nil {
			return err
		}
	}
	return nil
}

// baselineOnMigrate inserts the configured baseline when the history is
// empty but the schema already holds tables (WithBaselineOnMigrate).
func (r runner) baselineOnMigrate(ctx context.Context, s *snapshot) (*snapshot, error) {
	a, version := r.a, r.a.opts.baselineOnUp
	if version == "" || len(s.records) > 0 {
		return s, nil
	}
	n, err := a.d.CountTables(ctx, a.conn, a.table.Schema, a.table.Name, a.table.Name+"_lock")
	if err != nil || n == 0 {
		return s, err
	}
	if err := insertBaseline(ctx, a, version, "baseline on migrate", s.nextRank); err != nil {
		return nil, err
	}
	a.opts.logger.Typed().Str(logVersion, version).Info("baselined existing schema")
	return r.snapshot(ctx)
}

// insertBaseline records the baseline marker row at version.
func insertBaseline(ctx context.Context, a *applier, version, description string, rank int64) error {
	if !goVersion.MatchString(version) {
		return fmt.Errorf("baseline version %q: %w", version, ErrInvalidVersion)
	}
	u := &unit{
		Migration: Migration{Description: description, Source: "baseline"},
		key:       source.Canonical(version),
		kind:      history.KindBaseline,
	}
	_, err := a.store.Insert(ctx, a.conn, a.record(u, rank, 0, true))
	return err
}

// readSnapshot reads the history table, or returns an empty snapshot if the
// table does not exist yet (so read-only calls never issue DDL).
func readSnapshot(ctx context.Context, q history.Querier, store *history.Store) (*snapshot, error) {
	ok, err := store.Exists(ctx, q)
	if err != nil || !ok {
		return newSnapshot(nil), err
	}
	rs, err := store.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return newSnapshot(rs), nil
}

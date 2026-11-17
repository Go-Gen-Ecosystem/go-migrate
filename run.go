// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/halolog"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/script"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// cleanupTimeout bounds history updates and failure callbacks after cancellation.
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
func (m *Migrator) Up(ctx context.Context, opts ...RunOption) (*Result, error) {
	return m.up(ctx, "", opts)
}

// UpTo applies pending migrations up to and including version. Repeatable
// migrations are not run.
func (m *Migrator) UpTo(ctx context.Context, version string, opts ...RunOption) (*Result, error) {
	if version == "" {
		return nil, fmt.Errorf("empty target: %w", ErrVersionNotFound)
	}
	return m.up(ctx, version, opts)
}

// Down rolls back the migrations selected by t, newest application first. It
// fails before changing anything if the selection is invalid, a selected
// migration was edited or is irreversible, or fewer migrations are applied
// than requested.
func (m *Migrator) Down(ctx context.Context, t DownTarget, opts ...RunOption) (*Result, error) {
	t, err := t.validate()
	if err != nil {
		return nil, err
	}
	res := &Result{Direction: Down}
	err = m.runLocked(ctx, opts, func(ctx context.Context, r runner) error { return r.down(ctx, t, res) })
	return res, err
}

// Redo rolls back the most recently applied migration and applies it again,
// under a single lock so no other run can interleave. The Result lists the
// re-applied migration.
func (m *Migrator) Redo(ctx context.Context) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.runLocked(ctx, nil, func(ctx context.Context, r runner) error { return r.redo(ctx, res) })
	return res, err
}

func (m *Migrator) up(ctx context.Context, target string, opts []RunOption) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.runLocked(ctx, opts, func(ctx context.Context, r runner) error { return r.up(ctx, target, res) })
	return res, err
}

// locked runs fn while holding the cross-process migration lock. Every
// statement of fn runs on the lock's own connection, so losing a session
// lock fails the work and a one-connection pool cannot deadlock; the history
// table is qualified with the schema resolved before the run, so a
// migration's SET search_path or USE cannot redirect history writes. The
// lock is released even if fn panics or ctx is cancelled.
func (m *Migrator) locked(ctx context.Context, fn func(context.Context, *applier) error) (err error) {
	return m.withLock(ctx, nil, fn)
}

func (m *Migrator) withLock(ctx context.Context, check func(context.Context, *sql.Conn, dialect.Table) error, fn func(context.Context, *applier) error) (err error) {
	lock, err := m.dialect.LockChecked(ctx, m.db, m.table, m.opts.lockWait, check)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := lock.Release(ctx); rerr != nil {
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", rerr))
		}
	}()
	return fn(ctx, &applier{conn: lock.Conn, table: lock.Table, store: history.New(m.dialect, lock.Table), lock: lock,
		d: m.dialect, cat: m.cat, opts: &m.opts})
}

func (m *Migrator) runLocked(ctx context.Context, opts []RunOption, fn func(context.Context, runner) error) error {
	o := applyRunOptions(opts)
	if o.expectPlanSet && o.expectPlan == "" {
		return ErrPlanChanged
	}
	var check func(context.Context, *sql.Conn, dialect.Table) error
	if o.expectTarget {
		check = func(ctx context.Context, conn *sql.Conn, table dialect.Table) error {
			_, err := m.checkApprovalTarget(ctx, conn, table, o.expectPlan)
			return err
		}
	}
	return m.withLock(ctx, check, func(ctx context.Context, a *applier) error {
		return fn(ctx, runner{a: a, m: m, opts: o})
	})
}

// runner orchestrates applying and rolling back migrations under one lock.
type runner struct {
	a    *applier
	m    *Migrator
	opts runOptions
}

// verify enforces ExpectPlan: the plan computed under the lock must be the
// one that was approved.
func (r runner) verify(ctx context.Context, dir Direction, plan []*unit, s *snapshot, baseline string) error {
	if r.opts.expectTarget {
		target, err := r.m.checkApprovalTarget(ctx, r.a.conn, r.a.table, r.opts.expectPlan)
		if err != nil {
			return err
		}
		if approvedID(target, r.a.cat, dir, plan, s, baseline) != r.opts.expectPlan {
			return ErrPlanChanged
		}
	} else if r.opts.expectPlanSet && r.a.cat.planID(dir, plan) != r.opts.expectPlan {
		return ErrPlanChanged
	}
	return nil
}

func (r runner) snapshot(ctx context.Context) (*snapshot, error) {
	return readSnapshot(ctx, r.a.conn, r.a.store)
}

func (r runner) up(ctx context.Context, target string, res *Result) error {
	a := r.a
	s, err := r.snapshot(ctx)
	if err != nil {
		return err
	}
	s, baseline, err := r.m.simulateBaseline(ctx, a.conn, a.table, s)
	if err != nil {
		return err
	}
	plan, err := r.m.plan.up(s, target)
	if err != nil {
		return err
	}
	if err := r.verify(ctx, Up, plan, s, baseline); err != nil {
		return err
	}
	if err := a.store.Ensure(ctx, a.conn); err != nil {
		return err
	}
	if baseline != "" {
		if err := insertBaseline(ctx, a, baseline, "baseline on migrate", s.records[0].Rank); err != nil {
			return err
		}
		a.opts.logger.Typed().Str(logVersion, baseline).Info("baselined existing schema")
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

func (r runner) down(ctx context.Context, t DownTarget, res *Result) error {
	s, err := r.snapshot(ctx)
	if err != nil {
		return err
	}
	plan, err := r.m.plan.down(s, t)
	if err != nil {
		return err
	}
	if err := r.verify(ctx, Down, plan, s, ""); err != nil {
		return err
	}
	return r.execute(ctx, plan, Down, s, res)
}

func (r runner) redo(ctx context.Context, res *Result) error {
	s, err := r.snapshot(ctx)
	if err != nil {
		return err
	}
	plan, err := r.m.plan.down(s, DownTarget{Steps: 1})
	if err != nil {
		return err
	}
	if err := r.execute(ctx, plan, Down, s, &Result{Direction: Down}); err != nil {
		return err
	}
	if s, err = r.snapshot(ctx); err != nil {
		return err
	}
	// Re-apply through execute so preconditions are evaluated again.
	return r.execute(ctx, plan, Up, s, res)
}

// execute runs plan in order, appending each success to res, and stops at
// the first failure.
func (r runner) execute(ctx context.Context, plan []*unit, dir Direction, s *snapshot, res *Result) error {
	for _, u := range plan {
		bypassed, err := r.bypass(ctx, u, dir, s, res)
		if err != nil {
			return err
		}
		if bypassed {
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

// bypass handles migrations whose SQL must not run: on Up, a failed
// precondition (MARK_RAN records it, CONTINUE skips it, HALT fails); on Down,
// a row recorded by MARK_RAN, whose rollback only forgets the row because the
// migration never created anything.
func (r runner) bypass(ctx context.Context, u *unit, dir Direction, s *snapshot, res *Result) (bool, error) {
	if dir == Down {
		if rec := s.byKey[u.key]; rec != nil && rec.Kind == history.KindMarked {
			res.Applied = append(res.Applied, Applied{Migration: u.Migration, MarkedRan: true})
			return true, r.a.forget(ctx, u)
		}
		return false, nil
	}
	if len(u.pre) == 0 {
		return false, nil
	}
	outcome, err := checkPreconditions(ctx, r.a.conn, u.pre)
	if err != nil {
		return true, &MigrationError{Version: u.Label(), Direction: dir, Err: err}
	}
	switch outcome {
	case script.Halt:
		return true, fmt.Errorf("migration %s: %w", u.Label(), ErrPreconditionFailed)
	case script.Continue:
		r.a.opts.logger.Typed().Str(logVersion, u.Label()).Info("precondition not met: skipped for this run")
		return true, nil
	case script.MarkRan:
		res.Applied = append(res.Applied, Applied{Migration: u.Migration, MarkedRan: true})
		return true, r.a.markRan(ctx, u, s)
	default:
		return false, nil
	}
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

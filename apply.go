// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// applier executes one migration at a time on the run's pinned connection,
// together with its history bookkeeping, hooks, callbacks and logging.
type applier struct {
	conn  *sql.Conn
	table dialect.Table // schema-qualified history table
	store *history.Store
	lock  *dialect.Lock
	d     *dialect.Dialect
	cat   *catalog
	opts  *options
}

// step is the history context of one application.
type step struct {
	existing *history.Record // current row (repeatables), or nil
	rank     int64           // installed_rank for a new row
	start    time.Time
}

// strategy applies u in dir: atomically, or with the dirty-row protocol.
type strategy func(a *applier, ctx context.Context, u *unit, dir Direction, st step) error

// strategyFor picks how u is applied: in one transaction with its history
// row where the database can roll DDL back, otherwise with the dirty-row
// protocol so a partial failure is detectable.
func (a *applier) strategyFor(u *unit) strategy {
	if !u.NoTx && a.d.TransactionalDDL() {
		return (*applier).atomic
	}
	return (*applier).dirtyProtocol
}

// transition is a history-row change for one direction.
type transition func(a *applier, ctx context.Context, q history.Querier, u *unit, st step) error

// transitions are the history-row changes a direction needs: done for an
// atomic application, begin/finish around a non-transactional one.
var transitions = [2]struct{ done, begin, finish transition }{
	Up: {
		done: func(a *applier, ctx context.Context, q history.Querier, u *unit, st step) error {
			return a.save(ctx, q, u, st, true)
		},
		begin: func(a *applier, ctx context.Context, q history.Querier, u *unit, st step) error {
			return a.save(ctx, q, u, st, false)
		},
		finish: func(a *applier, ctx context.Context, q history.Querier, u *unit, st step) error {
			return a.store.Complete(ctx, q, u.key, ms(st.start))
		},
	},
	Down: {
		done: func(a *applier, ctx context.Context, q history.Querier, u *unit, _ step) error {
			return a.store.Delete(ctx, q, u.key)
		},
		begin: func(a *applier, ctx context.Context, q history.Querier, u *unit, _ step) error {
			return a.store.MarkDirty(ctx, q, u.key)
		},
		finish: func(a *applier, ctx context.Context, q history.Querier, u *unit, _ step) error {
			return a.store.Delete(ctx, q, u.key)
		},
	},
}

// eachEvents are the per-migration callback events by direction (Flyway
// defines them for migrate only).
var eachEvents = [2]struct{ before, after string }{
	Up: {before: source.BeforeEachMigrate, after: source.AfterEachMigrate},
}

// apply runs u in dir with hooks and logging, and advances the snapshot's
// next rank when it adds a history row (safe: the lock is held).
func (a *applier) apply(ctx context.Context, u *unit, dir Direction, s *snapshot) (time.Duration, error) {
	ev := Event{Migration: u.Migration, Direction: dir}
	if h := a.opts.hooks.Before; h != nil {
		if err := h(ctx, ev); err != nil {
			return 0, fmt.Errorf("before hook for %s: %w", u.Label(), err)
		}
	}
	log := a.opts.logger
	log.Typed().Str(logVersion, u.Label()).Str(logDirection, dir.String()).Str(logSource, u.Source).Info("running migration")

	st := step{existing: s.byKey[u.key], rank: s.nextRank, start: time.Now()}
	err := a.strategyFor(u)(a, ctx, u, dir, st)
	d := time.Since(st.start)
	if err == nil && dir == Up && st.existing == nil {
		s.nextRank++
	}
	if h := a.opts.hooks.After; h != nil {
		ev.Duration, ev.Err = d, err
		h(ctx, ev)
	}
	if err != nil {
		log.Typed().Str(logVersion, u.Label()).Str(logDirection, dir.String()).WithError(err).Error("migration failed")
		return d, err
	}
	log.Typed().Str(logVersion, u.Label()).Str(logDirection, dir.String()).Int64(logDuration, d.Milliseconds()).Info("migration done")
	return d, nil
}

// atomic runs callbacks, the migration, the lock fence and the history change
// in one transaction: on any failure nothing is applied and nothing recorded.
func (a *applier) atomic(ctx context.Context, u *unit, dir Direction, st step) error {
	tx, err := a.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", u.Label(), err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	ev := eachEvents[dir]
	steps := []func() error{
		func() error { return a.callbacks(ctx, tx, ev.before) },
		func() error { return u.exec(ctx, tx, dir, nil) },
		func() error { return a.callbacks(ctx, tx, ev.after) },
		func() error { return a.lock.Fence(ctx, tx) },
		func() error { return transitions[dir].done(a, ctx, tx, u, st) },
	}
	for _, fn := range steps {
		if err := fn(); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", u.Label(), err)
	}
	return nil
}

// dirtyProtocol runs a migration that cannot be transactional. The history
// row is marked in progress first, so a failure or crash part-way is detected
// by the next run (ErrDirty) instead of being silently re-applied; lock
// ownership is re-checked before every statement.
func (a *applier) dirtyProtocol(ctx context.Context, u *unit, dir Direction, st step) error {
	q, tr, ev := a.conn, transitions[dir], eachEvents[dir]
	fence := func(ctx context.Context) error { return a.lock.Fence(ctx, q) }
	if err := fence(ctx); err != nil {
		return err
	}
	if err := tr.begin(a, ctx, q, u, st); err != nil {
		return err
	}
	if err := a.callbacks(ctx, q, ev.before); err != nil {
		return fmt.Errorf("%w: %w", err, ErrDirty)
	}
	if err := u.exec(ctx, q, dir, fence); err != nil {
		var me *MigrationError
		if errors.As(err, &me) {
			me.Dirty = true
		}
		return err
	}
	if err := a.callbacks(ctx, q, ev.after); err != nil {
		return fmt.Errorf("%w: %w", err, ErrDirty)
	}
	// The change is fully applied: record it even if ctx was cancelled after
	// the last statement, rather than leaving a completed migration dirty.
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := tr.finish(a, cctx, q, u, st); err != nil {
		return fmt.Errorf("migration %s %s applied but its history row could not be updated: %w: %w", u.Label(), dir, err, ErrDirty)
	}
	return nil
}

// markRan records u as applied without executing it (precondition
// onFail=MARK_RAN), with the "marked" kind so a rollback never runs its Down.
func (a *applier) markRan(ctx context.Context, u *unit, s *snapshot) error {
	if err := a.lock.Fence(ctx, a.conn); err != nil {
		return err
	}
	rec := a.record(u, s.nextRank, 0, true)
	rec.Kind = history.KindMarked
	if s.byKey[u.key] != nil {
		return a.store.Reapply(ctx, a.conn, rec)
	}
	if _, err := a.store.Insert(ctx, a.conn, rec); err != nil {
		return err
	}
	s.nextRank++
	return nil
}

// forget removes u's history row without running SQL (rollback of a
// migration that was only marked as run).
func (a *applier) forget(ctx context.Context, u *unit) error {
	if err := a.lock.Fence(ctx, a.conn); err != nil {
		return err
	}
	return a.store.Delete(ctx, a.conn, u.key)
}

// save writes u's history row: a new row, or an update of a repeatable's.
func (a *applier) save(ctx context.Context, q history.Querier, u *unit, st step, success bool) error {
	elapsed := time.Duration(0)
	if success && !st.start.IsZero() {
		elapsed = time.Since(st.start)
	}
	rec := a.record(u, st.rank, elapsed, success)
	if st.existing != nil {
		return a.store.Reapply(ctx, q, rec)
	}
	_, err := a.store.Insert(ctx, q, rec)
	return err
}

// record builds a history row for u, clipped to the column limits.
func (a *applier) record(u *unit, rank int64, elapsed time.Duration, success bool) history.Record {
	return history.Record{
		Rank:        rank,
		Version:     u.key,
		Description: clip(u.Description, maxDescription),
		Kind:        u.kind,
		Script:      clip(u.Source, maxScript),
		Checksum:    u.Checksum,
		InstalledBy: clip(a.opts.installedBy, maxDescription),
		ExecutionMS: elapsed.Milliseconds(),
		Success:     success,
	}
}

// callbacks executes every SQL callback file registered for event on q; an
// empty event (no callbacks for this direction) is a no-op.
func (a *applier) callbacks(ctx context.Context, q Executor, event string) error {
	for _, cb := range a.cat.callbacks[event] {
		for i, stmt := range cb.stmts {
			if _, err := q.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("%s callback %s statement %d (%s): %w", event, cb.name, i+1, truncateSQL(stmt), err)
			}
		}
	}
	return nil
}

func ms(start time.Time) int64 { return time.Since(start).Milliseconds() }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

package migrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/halolog"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
)

// releaseTimeout bounds lock release, which runs on a non-cancelled context
// so that Ctrl-C or a request deadline never leaves the lock held.
const releaseTimeout = 30 * time.Second

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
func (m *Migrator) Up(ctx context.Context) (*Result, error) {
	return m.up(ctx, "")
}

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
// DownTo(ctx, "0") rolls back everything.
func (m *Migrator) DownTo(ctx context.Context, version string) (*Result, error) {
	if version == "" {
		return nil, fmt.Errorf("empty target: %w", ErrVersionNotFound)
	}
	return m.down(ctx, downSelector{to: version})
}

func (m *Migrator) up(ctx context.Context, target string) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.locked(ctx, func(ctx context.Context) error {
		if err := m.store.Ensure(ctx, m.db); err != nil {
			return err
		}
		s, err := m.snapshot(ctx)
		if err != nil {
			return err
		}
		plan, err := m.planUp(s, target)
		if err != nil {
			return err
		}
		return m.execute(ctx, plan, Up, s, res)
	})
	return res, err
}

func (m *Migrator) down(ctx context.Context, sel downSelector) (*Result, error) {
	res := &Result{Direction: Down}
	err := m.locked(ctx, func(ctx context.Context) error {
		s, err := m.snapshot(ctx)
		if err != nil {
			return err
		}
		plan, err := m.planDown(s, sel)
		if err != nil {
			return err
		}
		return m.execute(ctx, plan, Down, s, res)
	})
	return res, err
}

// execute runs plan in order, appending each success to res, and stops at
// the first failure.
func (m *Migrator) execute(ctx context.Context, plan []*unit, dir Direction, s *snapshot, res *Result) error {
	for _, u := range plan {
		d, err := m.run(ctx, u, dir, s.byKey[u.key])
		if err != nil {
			return err
		}
		res.Applied = append(res.Applied, Applied{Migration: u.Migration, Duration: d})
	}
	m.opts.logger.Typed().Int(logCount, len(res.Applied)).Str(logDirection, dir.String()).Info("migration run complete")
	return nil
}

// locked runs fn while holding the cross-process migration lock. The lock is
// released even if fn panics or ctx is cancelled.
func (m *Migrator) locked(ctx context.Context, fn func(context.Context) error) (err error) {
	lockCtx, release, err := m.dialect.Locker().Lock(ctx, m.db, dialect.LockKey(m.table), m.opts.lockWait)
	if err != nil {
		return err
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		if rerr := release(rctx); rerr != nil {
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", rerr))
		}
	}()
	return fn(lockCtx)
}

// snapshot reads the history table, or returns an empty snapshot if the
// table does not exist yet (so read-only calls never issue DDL).
func (m *Migrator) snapshot(ctx context.Context) (*snapshot, error) {
	ok, err := m.store.Exists(ctx, m.db)
	if err != nil {
		return nil, err
	}
	if !ok {
		return newSnapshot(nil), nil
	}
	rs, err := m.store.List(ctx, m.db)
	if err != nil {
		return nil, err
	}
	return newSnapshot(rs), nil
}

// run executes one migration in one direction, with hooks and logging.
func (m *Migrator) run(ctx context.Context, u *unit, dir Direction, existing *history.Record) (time.Duration, error) {
	ev := Event{Migration: u.Migration, Direction: dir}
	if h := m.opts.hooks.Before; h != nil {
		if err := h(ctx, ev); err != nil {
			return 0, fmt.Errorf("before hook for %s: %w", u.label(), err)
		}
	}
	log := m.opts.logger
	log.Typed().Str(logVersion, u.label()).Str(logDirection, dir.String()).Str(logSource, u.Source).Info("running migration")

	start := time.Now()
	var err error
	if !u.NoTx && m.dialect.TransactionalDDL() {
		err = m.runTx(ctx, u, dir, existing, start)
	} else {
		err = m.runNoTx(ctx, u, dir, existing, start)
	}
	d := time.Since(start)

	if h := m.opts.hooks.After; h != nil {
		ev.Duration, ev.Err = d, err
		h(ctx, ev)
	}
	if err != nil {
		log.Typed().Str(logVersion, u.label()).Str(logDirection, dir.String()).WithError(err).Error("migration failed")
		return d, err
	}
	log.Typed().Str(logVersion, u.label()).Str(logDirection, dir.String()).Int64(logDuration, d.Milliseconds()).Info("migration done")
	return d, nil
}

// runTx executes the migration and its history change in one transaction:
// on failure nothing is applied and nothing is recorded.
func (m *Migrator) runTx(ctx context.Context, u *unit, dir Direction, existing *history.Record, start time.Time) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", u.label(), err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	if err := u.exec(ctx, tx, dir); err != nil {
		return err
	}
	if dir == Down {
		err = m.store.Delete(ctx, tx, u.key)
	} else {
		err = m.saveRecord(ctx, tx, m.newRecord(u, time.Since(start), true), existing)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", u.label(), err)
	}
	return nil
}

// runNoTx executes a migration that cannot be transactional. The history row
// is marked dirty first, so a failure or crash part-way is detected by the
// next run (ErrDirty) instead of being silently re-applied. All statements
// share one connection, so session settings (SET search_path, ...) carry over.
func (m *Migrator) runNoTx(ctx context.Context, u *unit, dir Direction, existing *history.Record, start time.Time) error {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve connection for %s: %w", u.label(), err)
	}
	defer func() { _ = conn.Close() }()

	if dir == Down {
		err = m.store.MarkDirty(ctx, conn, u.key)
	} else {
		err = m.saveRecord(ctx, conn, m.newRecord(u, 0, false), existing)
	}
	if err != nil {
		return err
	}
	if err := u.exec(ctx, conn, dir); err != nil {
		var me *MigrationError
		if errors.As(err, &me) {
			me.Dirty = true
		}
		return err
	}
	if dir == Down {
		return m.store.Delete(ctx, conn, u.key)
	}
	return m.store.Complete(ctx, conn, u.key, time.Since(start).Milliseconds())
}

func (m *Migrator) saveRecord(ctx context.Context, q history.Querier, r history.Record, existing *history.Record) error {
	if existing != nil {
		return m.store.Reapply(ctx, q, r)
	}
	_, err := m.store.Insert(ctx, q, r)
	return err
}

func (m *Migrator) newRecord(u *unit, elapsed time.Duration, success bool) history.Record {
	return history.Record{
		Version:     u.key,
		Description: clip(u.Description, maxDescription),
		Kind:        u.kind,
		Script:      clip(u.Source, maxScript),
		Checksum:    u.Checksum,
		InstalledBy: clip(m.opts.installedBy, maxDescription),
		ExecutionMS: elapsed.Milliseconds(),
		Success:     success,
	}
}

// exec runs the unit's SQL statements (or Go func) for dir on ex.
func (u *unit) exec(ctx context.Context, ex Executor, dir Direction) error {
	if fn := u.fns[dir]; fn != nil {
		if err := fn(ctx, ex); err != nil {
			return &MigrationError{Version: u.label(), Direction: dir, Err: err}
		}
		return nil
	}
	for i, stmt := range u.stmts[dir] {
		if _, err := ex.ExecContext(ctx, stmt); err != nil {
			return &MigrationError{Version: u.label(), Direction: dir, Statement: i + 1, SQL: truncateSQL(stmt), Err: err}
		}
	}
	return nil
}

// label is how a migration is named in logs and errors.
func (u *unit) label() string {
	if u.Repeatable {
		return "R__" + u.Description
	}
	return u.Version
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

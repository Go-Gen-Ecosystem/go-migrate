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

// Tag labels the most recently applied migration with tag (for example a
// release name), so DownToTag can return to this point later. Tags are
// unique: tagging again moves the tag.
func (m *Migrator) Tag(ctx context.Context, tag string) error {
	if tag == "" || len(tag) > maxDescription {
		return fmt.Errorf("tag must be 1-%d characters", maxDescription)
	}
	return m.locked(ctx, func(ctx context.Context, r *runner) error {
		s, err := r.snapshot(ctx)
		if err != nil {
			return err
		}
		for i := len(s.records) - 1; i >= 0; i-- {
			rec := &s.records[i]
			if rec.Success && rec.Kind != history.KindRepeatable {
				tx, err := r.conn.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer func() { _ = tx.Rollback() }()
				if err := r.store.Tag(ctx, tx, rec.Version, tag); err != nil {
					return err
				}
				return tx.Commit()
			}
		}
		return fmt.Errorf("nothing applied to tag: %w", ErrNotEnoughApplied)
	})
}

// Redo rolls back the most recently applied migration and applies it again,
// under a single lock so no other run can interleave. The Result lists the
// re-applied migration.
func (m *Migrator) Redo(ctx context.Context) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.locked(ctx, func(ctx context.Context, r *runner) error {
		s, err := r.snapshot(ctx)
		if err != nil {
			return err
		}
		plan, err := m.planDown(s, downSelector{steps: 1})
		if err != nil {
			return err
		}
		u := plan[0]
		if _, err := r.run(ctx, u, Down, s); err != nil {
			return err
		}
		if err := r.invalidateRepeatables(ctx, s); err != nil {
			return err
		}
		if s, err = r.snapshot(ctx); err != nil {
			return err
		}
		d, err := r.run(ctx, u, Up, s)
		if err != nil {
			return err
		}
		res.Applied = append(res.Applied, Applied{Migration: u.Migration, Duration: d})
		return nil
	})
	return res, err
}

func (m *Migrator) up(ctx context.Context, target string) (*Result, error) {
	res := &Result{Direction: Up}
	err := m.locked(ctx, func(ctx context.Context, r *runner) error {
		if err := r.store.Ensure(ctx, r.conn); err != nil {
			return err
		}
		s, err := r.snapshot(ctx)
		if err != nil {
			return err
		}
		if s, err = r.baselineOnMigrate(ctx, s); err != nil {
			return err
		}
		plan, err := m.planUp(s, target)
		if err != nil {
			return err
		}
		if err := m.runCallbacks(ctx, r.conn, source.BeforeMigrate); err != nil {
			return err
		}
		if err := r.execute(ctx, plan, Up, s, res); err != nil {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			defer cancel()
			return errors.Join(err, m.runCallbacks(cctx, r.conn, source.AfterMigrateError))
		}
		return m.runCallbacks(ctx, r.conn, source.AfterMigrate)
	})
	return res, err
}

// baselineOnMigrate inserts the configured baseline when the history is
// empty but the schema already holds tables (WithBaselineOnMigrate).
func (r *runner) baselineOnMigrate(ctx context.Context, s *snapshot) (*snapshot, error) {
	m := r.m
	if m.opts.baselineOnUp == "" || len(s.records) > 0 {
		return s, nil
	}
	n, err := m.dialect.CountTables(ctx, r.conn, r.table.Schema, r.table.Name, r.table.Name+"_lock")
	if err != nil || n == 0 {
		return s, err
	}
	if !goVersion.MatchString(m.opts.baselineOnUp) {
		return nil, fmt.Errorf("baseline version %q: %w", m.opts.baselineOnUp, ErrInvalidVersion)
	}
	rec := m.newRecord(&unit{
		Migration: Migration{Description: "baseline on migrate", Source: "baseline"},
		key:       source.Canonical(m.opts.baselineOnUp),
		kind:      history.KindBaseline,
	}, s.nextRank, 0, true)
	if _, err := r.store.Insert(ctx, r.conn, rec); err != nil {
		return nil, err
	}
	m.opts.logger.Typed().Str(logVersion, rec.Version).Info("baselined existing schema")
	return r.snapshot(ctx)
}

func (m *Migrator) down(ctx context.Context, sel downSelector) (*Result, error) {
	res := &Result{Direction: Down}
	err := m.locked(ctx, func(ctx context.Context, r *runner) error {
		s, err := r.snapshot(ctx)
		if err != nil {
			return err
		}
		plan, err := m.planDown(s, sel)
		if err != nil {
			return err
		}
		if err := r.execute(ctx, plan, Down, s, res); err != nil || len(res.Applied) == 0 {
			return err
		}
		return r.invalidateRepeatables(ctx, s)
	})
	return res, err
}

// invalidateRepeatables clears the stored checksum of every repeatable
// migration after a rollback, so the next Up re-applies them: a rollback can
// drop the objects they create (a view over a dropped table).
func (r *runner) invalidateRepeatables(ctx context.Context, s *snapshot) error {
	for _, rec := range s.records {
		if rec.Kind == history.KindRepeatable {
			if err := r.store.Realign(ctx, r.conn, rec.Version, "", history.KindRepeatable); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarkApplied records version as applied without running it: for a dirty
// row whose migration you completed by hand (see ErrDirty), or to adopt a
// single change that already exists in the database. It is idempotent.
func (m *Migrator) MarkApplied(ctx context.Context, version string) error {
	key, err := m.lookupVersion(version)
	if err != nil {
		return err
	}
	u := m.byKey[key]
	return m.locked(ctx, func(ctx context.Context, r *runner) error {
		if err := r.store.Ensure(ctx, r.conn); err != nil {
			return err
		}
		s, err := r.snapshot(ctx)
		if err != nil {
			return err
		}
		switch rec := s.byKey[key]; {
		case rec == nil:
			_, err = r.store.Insert(ctx, r.conn, m.newRecord(u, s.nextRank, 0, true))
		case !rec.Success:
			err = r.store.Complete(ctx, r.conn, key, rec.ExecutionMS)
		}
		return err
	})
}

// runner is the state of one locked run. Every statement runs on conn, the
// lock's own connection, so losing a session lock fails the work and a
// one-connection pool cannot deadlock. The history table is qualified with
// the schema resolved before the run, so a migration's SET search_path or
// USE cannot redirect history writes.
type runner struct {
	m     *Migrator
	conn  *sql.Conn
	table dialect.Table
	store *history.Store
	lock  *dialect.Lock
}

// locked runs fn while holding the cross-process migration lock. The lock is
// released, and its connection discarded, even if fn panics or ctx is
// cancelled.
func (m *Migrator) locked(ctx context.Context, fn func(context.Context, *runner) error) (err error) {
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
	return fn(ctx, &runner{m: m, conn: lock.Conn, table: table, store: history.New(m.dialect, table), lock: lock})
}

func (r *runner) snapshot(ctx context.Context) (*snapshot, error) {
	return readSnapshot(ctx, r.conn, r.store)
}

// readSnapshot reads the history table, or returns an empty snapshot if the
// table does not exist yet (so read-only calls never issue DDL).
func readSnapshot(ctx context.Context, q history.Querier, store *history.Store) (*snapshot, error) {
	ok, err := store.Exists(ctx, q)
	if err != nil {
		return nil, err
	}
	if !ok {
		return newSnapshot(nil), nil
	}
	rs, err := store.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return newSnapshot(rs), nil
}

// execute runs plan in order, appending each success to res, and stops at
// the first failure.
func (r *runner) execute(ctx context.Context, plan []*unit, dir Direction, s *snapshot, res *Result) error {
	for _, u := range plan {
		if dir == Up && len(u.pre) > 0 {
			outcome, err := checkPreconditions(ctx, r.conn, u.pre)
			if err != nil {
				return &MigrationError{Version: u.label(), Direction: dir, Err: err}
			}
			switch outcome {
			case script.Halt:
				return fmt.Errorf("migration %s: %w", u.label(), ErrPreconditionFailed)
			case script.Continue:
				r.m.opts.logger.Typed().Str(logVersion, u.label()).Info("precondition not met: skipped for this run")
				continue
			case script.MarkRan:
				if err := r.markRan(ctx, u, s); err != nil {
					return err
				}
				res.Applied = append(res.Applied, Applied{Migration: u.Migration, MarkedRan: true})
				continue
			}
		}
		d, err := r.run(ctx, u, dir, s)
		if err != nil {
			return err
		}
		res.Applied = append(res.Applied, Applied{Migration: u.Migration, Duration: d})
	}
	r.m.opts.logger.Typed().Int(logCount, len(res.Applied)).Str(logDirection, dir.String()).Info("migration run complete")
	return nil
}

// markRan records u as applied without executing it (precondition
// onFail=MARK_RAN).
func (r *runner) markRan(ctx context.Context, u *unit, s *snapshot) error {
	if err := r.lock.Fence(ctx, r.conn); err != nil {
		return err
	}
	existing := s.byKey[u.key]
	if err := r.saveRecord(ctx, r.conn, r.m.newRecord(u, s.nextRank, 0, true), existing); err != nil {
		return err
	}
	if existing == nil {
		s.nextRank++
	}
	return nil
}

// run executes one migration in one direction, with hooks and logging. New
// history rows take the snapshot's next rank (safe: the lock is held), which
// advances on success; a rollback removes the row from the snapshot.
func (r *runner) run(ctx context.Context, u *unit, dir Direction, s *snapshot) (time.Duration, error) {
	m := r.m
	ev := Event{Migration: u.Migration, Direction: dir}
	if h := m.opts.hooks.Before; h != nil {
		if err := h(ctx, ev); err != nil {
			return 0, fmt.Errorf("before hook for %s: %w", u.label(), err)
		}
	}
	log := m.opts.logger
	log.Typed().Str(logVersion, u.label()).Str(logDirection, dir.String()).Str(logSource, u.Source).Info("running migration")

	existing := s.byKey[u.key]
	start := time.Now()
	var err error
	if !u.NoTx && m.dialect.TransactionalDDL() {
		err = r.runTx(ctx, u, dir, existing, s.nextRank, start)
	} else {
		err = r.runNoTx(ctx, u, dir, existing, s.nextRank, start)
	}
	d := time.Since(start)
	if err == nil && dir == Up && existing == nil {
		s.nextRank++
	}

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

// runTx executes the migration, re-checks lock ownership and writes its
// history change in one transaction: on any failure nothing is applied and
// nothing is recorded.
func (r *runner) runTx(ctx context.Context, u *unit, dir Direction, existing *history.Record, rank int64, start time.Time) error {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", u.label(), err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	if err := r.eachCallback(ctx, tx, dir, source.BeforeEachMigrate); err != nil {
		return err
	}
	if err := u.exec(ctx, tx, dir, nil); err != nil {
		return err
	}
	if err := r.eachCallback(ctx, tx, dir, source.AfterEachMigrate); err != nil {
		return err
	}
	if err := r.lock.Fence(ctx, tx); err != nil {
		return err
	}
	if dir == Down {
		err = r.store.Delete(ctx, tx, u.key)
	} else {
		err = r.saveRecord(ctx, tx, r.m.newRecord(u, rank, time.Since(start), true), existing)
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
// next run (ErrDirty) instead of being silently re-applied. Lock ownership
// is re-checked before every statement.
func (r *runner) runNoTx(ctx context.Context, u *unit, dir Direction, existing *history.Record, rank int64, start time.Time) error {
	q := r.conn
	fence := func(ctx context.Context) error { return r.lock.Fence(ctx, q) }
	if err := fence(ctx); err != nil {
		return err
	}
	var err error
	if dir == Down {
		err = r.store.MarkDirty(ctx, q, u.key)
	} else {
		err = r.saveRecord(ctx, q, r.m.newRecord(u, rank, 0, false), existing)
	}
	if err != nil {
		return err
	}
	if err := r.eachCallback(ctx, q, dir, source.BeforeEachMigrate); err != nil {
		return err
	}
	if err := u.exec(ctx, q, dir, fence); err != nil {
		var me *MigrationError
		if errors.As(err, &me) {
			me.Dirty = true
		}
		return err
	}
	if err := r.eachCallback(ctx, q, dir, source.AfterEachMigrate); err != nil {
		return fmt.Errorf("%w: %w", err, ErrDirty)
	}
	// The change is fully applied: record it even if ctx was cancelled after
	// the last statement, rather than leaving a completed migration dirty.
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if dir == Down {
		err = r.store.Delete(cctx, q, u.key)
	} else {
		err = r.store.Complete(cctx, q, u.key, time.Since(start).Milliseconds())
	}
	if err != nil {
		return fmt.Errorf("migration %s %s applied but its history row could not be updated: %w: %w", u.label(), dir, err, ErrDirty)
	}
	return nil
}

func (r *runner) saveRecord(ctx context.Context, q history.Querier, rec history.Record, existing *history.Record) error {
	if existing != nil {
		return r.store.Reapply(ctx, q, rec)
	}
	_, err := r.store.Insert(ctx, q, rec)
	return err
}

func (m *Migrator) newRecord(u *unit, rank int64, elapsed time.Duration, success bool) history.Record {
	return history.Record{
		Rank:        rank,
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

// exec runs the unit's SQL statements (or Go func) for dir on ex. before, if
// set, runs ahead of every statement (lock fencing for non-transactional runs).
func (u *unit) exec(ctx context.Context, ex Executor, dir Direction, before func(context.Context) error) error {
	if fn := u.fns[dir]; fn != nil {
		if err := fn(ctx, ex); err != nil {
			return &MigrationError{Version: u.label(), Direction: dir, Err: err}
		}
		return nil
	}
	for i, stmt := range u.stmts[dir] {
		if before != nil {
			if err := before(ctx); err != nil {
				return &MigrationError{Version: u.label(), Direction: dir, Statement: i + 1, SQL: truncateSQL(stmt), Err: err}
			}
		}
		if _, err := ex.ExecContext(ctx, stmt); err != nil {
			return &MigrationError{Version: u.label(), Direction: dir, Statement: i + 1, SQL: truncateSQL(stmt), Err: err}
		}
	}
	return nil
}

// label is how a migration is named in logs and errors.
func (u *unit) label() string { return u.Label() }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// eachCallback runs a per-migration callback event for Up runs.
func (r *runner) eachCallback(ctx context.Context, q Executor, dir Direction, event string) error {
	if dir != Up {
		return nil
	}
	return r.m.runCallbacks(ctx, q, event)
}

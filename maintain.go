// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// RepairAction selects one type of history repair.
type RepairAction string

const (
	// RepairClearFailed removes a failed row after restoring the schema by hand.
	RepairClearFailed RepairAction = "clear-failed"
	// RepairAcceptChecksum accepts deliberate drift for an applied version.
	RepairAcceptChecksum RepairAction = "accept-checksum"
)

// RepairTarget identifies exactly one migration and action.
type RepairTarget struct {
	// Migration is a version or, for RepairClearFailed, a repeatable label
	// (R__<description>) or history key (R:<description>).
	Migration string
	Action    RepairAction
}

// RepairPlan describes a proposed history change. Migration is the canonical
// history key and State is the current state. Checksum and kind fields show
// the existing row and, for RepairAcceptChecksum, the proposed values.
type RepairPlan struct {
	RepairTarget
	State                       State
	StoredChecksum, NewChecksum string
	StoredKind, NewKind         string
}

// RepairResult reports what Repair changed.
type RepairResult struct {
	// RemovedFailed is 1 when the selected failed row was removed.
	RemovedFailed int64
	// Realigned contains the selected version when its checksum was realigned.
	Realigned []string
}

// PlanRepair previews the selected history repair without taking a migration
// lock or making any writes. Repair checks the current state again when run.
func (m *Migrator) PlanRepair(ctx context.Context, target RepairTarget) (*RepairPlan, error) {
	target, err := target.validate()
	if err != nil {
		return nil, err
	}
	s, err := m.readHistory(ctx)
	if err != nil {
		return nil, err
	}
	return m.cat.planRepair(s, target)
}

// Repair applies exactly one explicit history change. Clear a failed row
// only after restoring the schema by hand; if you completed the migration,
// use MarkApplied instead. Repair never runs migration SQL. A call without
// exactly one target returns ErrInvalidArgument without writing history.
func (m *Migrator) Repair(ctx context.Context, targets ...RepairTarget) (*RepairResult, error) {
	res := &RepairResult{}
	if len(targets) != 1 {
		return res, fmt.Errorf("repair requires one migration and one action: %w", ErrInvalidArgument)
	}
	t, err := targets[0].validate()
	if err != nil {
		return res, err
	}
	err = m.locked(ctx, func(ctx context.Context, a *applier) error { return maintenance{a}.repair(ctx, t, res) })
	return res, err
}

func (t RepairTarget) validate() (RepairTarget, error) {
	if t.Action != RepairClearFailed && t.Action != RepairAcceptChecksum {
		return t, fmt.Errorf("repair requires clear-failed or accept-checksum: %w", ErrInvalidArgument)
	}
	if goVersion.MatchString(t.Migration) {
		t.Migration = source.Canonical(t.Migration)
		return t, nil
	}
	name, repeatable := strings.CutPrefix(t.Migration, "R__")
	if !repeatable {
		name, repeatable = strings.CutPrefix(t.Migration, history.RepeatablePrefix)
	}
	if repeatable && name != "" {
		if t.Action == RepairAcceptChecksum {
			return t, fmt.Errorf("repeatable migration %q must be reapplied, not realigned: %w", t.Migration, ErrInvalidArgument)
		}
		t.Migration = history.RepeatablePrefix + strings.ToLower(name)
		return t, nil
	}
	return t, fmt.Errorf("repair migration %q must be a version or repeatable label: %w", t.Migration, ErrInvalidArgument)
}

func (c *catalog) planRepair(s *snapshot, t RepairTarget) (*RepairPlan, error) {
	rec := s.byKey[t.Migration]
	if rec == nil {
		return nil, fmt.Errorf("migration %q has no history row: %w", t.Migration, ErrHistoryRowNotFound)
	}
	p := &RepairPlan{RepairTarget: t, StoredChecksum: rec.Checksum, StoredKind: string(rec.Kind)}
	if t.Action == RepairClearFailed {
		if rec.Success || (rec.Kind != history.KindSQL && rec.Kind != history.KindGo && rec.Kind != history.KindRepeatable) {
			return nil, fmt.Errorf("migration %q is not a failed migration: %w", t.Migration, ErrInvalidArgument)
		}
		p.State = StateFailed
		return p, nil
	}
	if !rec.Success || (rec.Kind != history.KindSQL && rec.Kind != history.KindGo && rec.Kind != history.KindMarked) {
		return nil, fmt.Errorf("migration %q is not an applied versioned migration: %w", t.Migration, ErrInvalidArgument)
	}
	u := c.byKey[t.Migration]
	if u == nil {
		return nil, fmt.Errorf("migration %q: %w", t.Migration, ErrMissingMigration)
	}
	if !drifted(u, rec) {
		return nil, fmt.Errorf("migration %q has no checksum or kind drift: %w", t.Migration, ErrInvalidArgument)
	}
	kind := u.kind
	if rec.Kind == history.KindMarked {
		if kind != history.KindSQL {
			return nil, fmt.Errorf("migration %q was marked as run; restore its SQL source: %w", t.Migration, ErrInvalidArgument)
		}
		kind = history.KindMarked // stays "recorded without running"
	}
	p.State, p.NewChecksum, p.NewKind = StateModified, u.Checksum, string(kind)
	return p, nil
}

// MarkApplied records version as applied without running it: for a dirty
// row whose migration you completed by hand (see ErrDirty), or to adopt a
// single change that already exists in the database. It is idempotent.
func (m *Migrator) MarkApplied(ctx context.Context, version string) error {
	key, err := m.cat.lookupVersion(version)
	if err != nil {
		return err
	}
	return m.locked(ctx, func(ctx context.Context, a *applier) error { return maintenance{a}.markApplied(ctx, m.cat.byKey[key]) })
}

// Baseline marks an existing database as being at version, so migrations up
// to and including it are treated as already applied. It requires an empty
// history table.
func (m *Migrator) Baseline(ctx context.Context, version, description string) error {
	if !goVersion.MatchString(version) {
		return fmt.Errorf("baseline version %q: %w", version, ErrInvalidVersion)
	}
	if description == "" {
		description = "baseline"
	}
	return m.locked(ctx, func(ctx context.Context, a *applier) error { return maintenance{a}.baseline(ctx, version, description) })
}

// Tag labels the most recently applied migration with tag (for example a
// release name), so DownToTag can return to this point later. Tags are
// unique: tagging again moves the tag.
func (m *Migrator) Tag(ctx context.Context, tag string) error {
	if tag == "" || len(tag) > maxDescription {
		return fmt.Errorf("tag must be 1-%d characters", maxDescription)
	}
	return m.locked(ctx, func(ctx context.Context, a *applier) error { return maintenance{a}.tag(ctx, tag) })
}

// maintenance performs history-only administration under the lock; it never
// runs migration SQL.
type maintenance struct{ a *applier }

func (mt maintenance) repair(ctx context.Context, t RepairTarget, res *RepairResult) error {
	a := mt.a
	tx, err := a.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin repair transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	s, err := readSnapshot(ctx, tx, a.store)
	if err != nil {
		return err
	}
	p, err := a.cat.planRepair(s, t)
	if err != nil {
		return err
	}
	if err := a.lock.Fence(ctx, tx); err != nil {
		return err
	}
	if p.Action == RepairClearFailed {
		err = a.store.DeleteFailed(ctx, tx, p.Migration)
	} else {
		err = a.store.Realign(ctx, tx, p.Migration, p.NewChecksum, history.Kind(p.NewKind))
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit repair for %q: %w", p.Migration, err)
	}
	if p.Action == RepairClearFailed {
		res.RemovedFailed = 1
	} else {
		res.Realigned = []string{p.Migration}
	}
	a.opts.logger.Typed().Str(logVersion, p.Migration).Info("repair complete")
	return nil
}

func (mt maintenance) markApplied(ctx context.Context, u *unit) error {
	a := mt.a
	if err := a.store.Ensure(ctx, a.conn); err != nil {
		return err
	}
	s, err := readSnapshot(ctx, a.conn, a.store)
	if err != nil {
		return err
	}
	switch rec := s.byKey[u.key]; {
	case rec == nil:
		return a.save(ctx, a.conn, u, step{rank: s.nextRank}, true)
	case !rec.Success:
		return a.store.Complete(ctx, a.conn, u.key, rec.ExecutionMS)
	default:
		return nil // already applied
	}
}

func (mt maintenance) baseline(ctx context.Context, version, description string) error {
	a := mt.a
	if err := a.store.Ensure(ctx, a.conn); err != nil {
		return err
	}
	rs, err := a.store.List(ctx, a.conn)
	if err != nil {
		return err
	}
	if len(rs) > 0 {
		return ErrBaselineNotEmpty
	}
	return insertBaseline(ctx, a, version, description, 1)
}

func (mt maintenance) tag(ctx context.Context, tag string) error {
	a := mt.a
	s, err := readSnapshot(ctx, a.conn, a.store)
	if err != nil {
		return err
	}
	rec := s.latestApplied()
	if rec == nil {
		return fmt.Errorf("nothing applied to tag: %w", ErrNotEnoughApplied)
	}
	tx, err := a.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := a.store.Tag(ctx, tx, rec.Version, tag); err != nil {
		return err
	}
	return tx.Commit()
}

// latestApplied returns the most recently applied successful non-repeatable
// row (by installed_rank), or nil.
func (s *snapshot) latestApplied() *history.Record {
	for i := len(s.records) - 1; i >= 0; i-- {
		if r := &s.records[i]; r.Success && r.Kind != history.KindRepeatable {
			return r
		}
	}
	return nil
}

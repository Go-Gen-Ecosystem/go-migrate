// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
)

// RepairResult reports what Repair changed.
type RepairResult struct {
	// RemovedFailed is the number of dirty (failed) history rows removed.
	RemovedFailed int64
	// Realigned lists versions whose stored checksum was updated to match
	// the current file.
	Realigned []string
}

// Repair clears failed history rows (after you have fixed the schema by
// hand) and realigns stored checksums of applied migrations with the current
// files, accepting deliberate edits. It never touches the schema itself.
func (m *Migrator) Repair(ctx context.Context) (*RepairResult, error) {
	res := &RepairResult{}
	err := m.locked(ctx, func(ctx context.Context, a *applier) error { return maintenance{a}.repair(ctx, res) })
	return res, err
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

func (mt maintenance) repair(ctx context.Context, res *RepairResult) error {
	a := mt.a
	s, err := readSnapshot(ctx, a.conn, a.store)
	if err != nil || len(s.records) == 0 {
		return err
	}
	if res.RemovedFailed, err = a.store.DeleteFailed(ctx, a.conn); err != nil {
		return err
	}
	for _, rec := range s.records {
		u := a.cat.byKey[rec.Version]
		if !rec.Success || u == nil || !drifted(u, &rec) {
			continue
		}
		kind := u.kind
		if rec.Kind == history.KindMarked {
			kind = rec.Kind // stays "recorded without running"
		}
		if err := a.store.Realign(ctx, a.conn, rec.Version, u.Checksum, kind); err != nil {
			return err
		}
		res.Realigned = append(res.Realigned, rec.Version)
	}
	a.opts.logger.Typed().Int64(logCount, res.RemovedFailed).Info("repair complete")
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

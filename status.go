package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Status reports every migration's state. It is read-only: it takes no lock
// and never creates the history table.
func (m *Migrator) Status(ctx context.Context) (*Status, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	in := m.inspect(s)
	return &Status{
		Current:    s.latest,
		Dirty:      len(s.dirty) > 0,
		Pending:    len(in.pending),
		Migrations: in.infos,
	}, nil
}

// Validate reports, without changing anything, every problem that would make
// Up fail: dirty rows, modified or missing applied migrations, and
// out-of-order pending ones. It returns nil when Up could run.
func (m *Migrator) Validate(ctx context.Context) error {
	s, err := m.snapshot(ctx)
	if err != nil {
		return err
	}
	return errors.Join(m.inspect(s).problems...)
}

// Plan returns what Up (target == "") or UpTo(target) would apply, without
// applying it. Use it for dry runs.
func (m *Migrator) Plan(ctx context.Context, target string) ([]Migration, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := m.planUp(s, target)
	if err != nil {
		return nil, err
	}
	return migrationsOf(plan), nil
}

// PlanDown returns what Down(steps) would roll back, without doing it.
func (m *Migrator) PlanDown(ctx context.Context, steps int) ([]Migration, error) {
	if steps <= 0 {
		return nil, fmt.Errorf("steps must be positive, got %d", steps)
	}
	return m.planDownRead(ctx, downSelector{steps: steps})
}

// PlanDownTo returns what DownTo(version) would roll back, without doing it.
func (m *Migrator) PlanDownTo(ctx context.Context, version string) ([]Migration, error) {
	return m.planDownRead(ctx, downSelector{to: version})
}

func (m *Migrator) planDownRead(ctx context.Context, sel downSelector) ([]Migration, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := m.planDown(s, sel)
	if err != nil {
		return nil, err
	}
	return migrationsOf(plan), nil
}

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
	err := m.locked(ctx, func(ctx context.Context) error {
		s, err := m.snapshot(ctx)
		if err != nil || len(s.records) == 0 {
			return err
		}
		if res.RemovedFailed, err = m.store.DeleteFailed(ctx, m.db); err != nil {
			return err
		}
		for _, r := range s.records {
			u := m.byKey[r.Version]
			if !r.Success || r.Kind != history.KindSQL || u == nil || u.Checksum == r.Checksum {
				continue
			}
			if err := m.store.SetChecksum(ctx, m.db, r.Version, u.Checksum); err != nil {
				return err
			}
			res.Realigned = append(res.Realigned, r.Version)
		}
		m.opts.logger.Typed().Int64(logCount, res.RemovedFailed).Info("repair complete")
		return nil
	})
	return res, err
}

// Baseline marks an existing database as being at version, so migrations up
// to and including it are treated as already applied. It requires an empty
// history table.
func (m *Migrator) Baseline(ctx context.Context, version, description string) error {
	if !goVersion.MatchString(version) {
		return fmt.Errorf("baseline version %q: %w", version, ErrVersionNotFound)
	}
	if description == "" {
		description = "baseline"
	}
	return m.locked(ctx, func(ctx context.Context) error {
		if err := m.store.Ensure(ctx, m.db); err != nil {
			return err
		}
		rs, err := m.store.List(ctx, m.db)
		if err != nil {
			return err
		}
		if len(rs) > 0 {
			return ErrBaselineNotEmpty
		}
		_, err = m.store.Insert(ctx, m.db, history.Record{
			Version: source.Canonical(version), Description: clip(description, maxDescription),
			Kind: history.KindBaseline, Script: "baseline", InstalledBy: m.opts.installedBy, Success: true,
		})
		return err
	})
}

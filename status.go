// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"errors"
	"fmt"
)

// Status reports every migration's state. It is read-only: it takes no lock
// and never creates the history table.
func (m *Migrator) Status(ctx context.Context) (*Status, error) {
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return nil, err
	}
	in := m.plan.inspect(s)
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
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return err
	}
	return errors.Join(m.plan.inspect(s).problems...)
}

// Plan returns what Up (target == "") or UpTo(target) would apply, without
// applying it. Use it for dry runs.
func (m *Migrator) Plan(ctx context.Context, target string) ([]Migration, error) {
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return nil, err
	}
	plan, err := m.plan.up(s, target)
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
	if version == "" {
		return nil, fmt.Errorf("empty target: %w", ErrVersionNotFound)
	}
	return m.planDownRead(ctx, downSelector{to: version})
}

// PlanDownToTag returns what DownToTag(tag) would roll back, without doing it.
func (m *Migrator) PlanDownToTag(ctx context.Context, tag string) ([]Migration, error) {
	if tag == "" {
		return nil, fmt.Errorf("empty tag: %w", ErrTagNotFound)
	}
	return m.planDownRead(ctx, downSelector{tag: tag})
}

// Statements returns the SQL that mig runs in direction dir, after
// placeholder substitution, exactly as it would be executed (Liquibase
// updateSQL). Go migrations and unknown migrations return nil.
func (m *Migrator) Statements(mig Migration, dir Direction) []string {
	return m.cat.statements(mig, dir)
}

func (m *Migrator) planDownRead(ctx context.Context, sel downSelector) ([]Migration, error) {
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return nil, err
	}
	plan, err := m.plan.down(s, sel)
	if err != nil {
		return nil, err
	}
	return migrationsOf(plan), nil
}

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"errors"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Status reports every migration's state. It is read-only: it takes no lock
// and never creates the history table. Pending counts what Up would apply,
// excluding RunAlways repeatables (they run every time and are never "behind").
func (m *Migrator) Status(ctx context.Context) (*Status, error) {
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return nil, err
	}
	in := m.plan.inspect(s)
	return &Status{
		Current:    s.latest,
		Dirty:      len(s.dirty) > 0,
		Pending:    len(in.pending) - in.always,
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
// applying it, including the effect of WithBaselineOnMigrate. Use it for dry
// runs and bind an approval to it with PlanID and ExpectPlan.
func (m *Migrator) Plan(ctx context.Context, target string) ([]Migration, error) {
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return nil, err
	}
	if s, err = m.simulateBaseline(ctx, s); err != nil {
		return nil, err
	}
	plan, err := m.plan.up(s, target)
	if err != nil {
		return nil, err
	}
	return migrationsOf(plan), nil
}

// PlanDown returns what Down(t) would roll back, without doing it.
func (m *Migrator) PlanDown(ctx context.Context, t DownTarget) ([]Migration, error) {
	t, err := t.validate()
	if err != nil {
		return nil, err
	}
	s, err := readSnapshot(ctx, m.db, m.store)
	if err != nil {
		return nil, err
	}
	plan, err := m.plan.down(s, t)
	if err != nil {
		return nil, err
	}
	return migrationsOf(plan), nil
}

// PlanID fingerprints a plan returned by Plan or PlanDown: its direction and
// each migration's identity, checksum and exact statements. Pass it to
// ExpectPlan so the run fails with ErrPlanChanged unless it executes exactly
// what was approved.
func (m *Migrator) PlanID(dir Direction, plan []Migration) string {
	units := make([]*unit, 0, len(plan))
	for _, mig := range plan {
		if u, ok := m.cat.byKey[historyKey(mig)]; ok {
			units = append(units, u)
		} else {
			units = append(units, &unit{Migration: mig, key: historyKey(mig)})
		}
	}
	return m.cat.planID(dir, units)
}

// Callbacks returns the SQL callbacks a run in direction dir executes, in
// order: beforeMigrate once, beforeEachMigrate and afterEachMigrate around
// every migration, then afterMigrate on success or afterMigrateError on
// failure. They run even when no migration is pending. Rollbacks run none.
// PlanID covers them, so an edited callback invalidates an approval.
func (m *Migrator) Callbacks(dir Direction) []Callback {
	if dir != Up && dir != Down {
		return nil
	}
	return m.cat.callbacksFor(dir)
}

// Statements returns the SQL that mig runs in direction dir, after
// placeholder substitution, exactly as it would be executed (Liquibase
// updateSQL). Go migrations and unknown migrations return nil.
func (m *Migrator) Statements(mig Migration, dir Direction) []string {
	return m.cat.statements(mig, dir)
}

// simulateBaseline makes a read-only snapshot reflect WithBaselineOnMigrate:
// Up would baseline an existing, untracked schema first, so dry runs must too.
func (m *Migrator) simulateBaseline(ctx context.Context, s *snapshot) (*snapshot, error) {
	if m.opts.baselineOnUp == "" || len(s.records) > 0 {
		return s, nil
	}
	schema, err := m.dialect.CurrentSchema(ctx, m.db)
	if err != nil {
		return nil, err
	}
	table := m.table.Qualify(schema)
	n, err := m.dialect.CountTables(ctx, m.db, table.Schema, table.Name, table.Name+"_lock")
	if err != nil || n == 0 {
		return s, err
	}
	return newSnapshot([]history.Record{{Rank: 1, Version: source.Canonical(m.opts.baselineOnUp),
		Kind: history.KindBaseline, Success: true}}), nil
}

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Status reports every migration's state. It is read-only: it takes no lock
// and never creates the history table. Pending counts what Up would apply,
// excluding RunAlways repeatables (they run every time and are never "behind").
func (m *Migrator) Status(ctx context.Context) (*Status, error) {
	s, err := m.readHistory(ctx)
	if err != nil {
		return nil, err
	}
	in := m.plan.inspect(s, true)
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
	s, err := m.readHistory(ctx)
	if err != nil {
		return err
	}
	return errors.Join(m.plan.inspect(s, false).problems...)
}

// Plan returns what Up (target == "") or UpTo(target) would apply, without
// applying it, including the effect of WithBaselineOnMigrate. Use it for dry
// runs and bind an approval to it with PlanID and ExpectPlan.
func (m *Migrator) Plan(ctx context.Context, target string) ([]Migration, error) {
	plan, _, err := m.planUp(ctx, target, false)
	return plan, err
}

// PlanDown returns what Down(t) would roll back, without doing it.
func (m *Migrator) PlanDown(ctx context.Context, t DownTarget) ([]Migration, error) {
	plan, _, err := m.planDown(ctx, t, false)
	return plan, err
}

// PlanWithID returns the dry run and its target-bound approval ID. Both the
// plan and target are read on one session. Use the ID with ExpectTargetPlan.
func (m *Migrator) PlanWithID(ctx context.Context, target string) ([]Migration, string, error) {
	return m.planUp(ctx, target, true)
}

// PlanDownWithID returns the rollback plan and its target-bound approval ID
// on one session. Use the ID with ExpectTargetPlan.
func (m *Migrator) PlanDownWithID(ctx context.Context, t DownTarget) ([]Migration, string, error) {
	return m.planDown(ctx, t, true)
}

func (m *Migrator) planUp(ctx context.Context, target string, bound bool) ([]Migration, string, error) {
	return m.readPlan(ctx, Up, bound, func(s *snapshot) ([]*unit, error) { return m.plan.up(s, target) })
}

func (m *Migrator) planDown(ctx context.Context, t DownTarget, bound bool) ([]Migration, string, error) {
	t, err := t.validate()
	if err != nil {
		return nil, "", err
	}
	return m.readPlan(ctx, Down, bound, func(s *snapshot) ([]*unit, error) { return m.plan.down(s, t) })
}

// PlanID fingerprints a plan returned by Plan or PlanDown: its direction and
// each migration's identity, checksum and exact statements. Pass it to
// ExpectPlan so the run fails with ErrPlanChanged unless it executes exactly
// what was approved. It returns an empty ID for an invalid direction.
func (m *Migrator) PlanID(dir Direction, plan []Migration) string {
	if !dir.valid() {
		return ""
	}
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
	if !dir.valid() {
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

func (m *Migrator) historySession(ctx context.Context, read func(*sql.Conn, dialect.Table) error) error {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve history connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	table, err := m.dialect.ResolveTable(ctx, conn, m.table)
	if err != nil {
		return err
	}
	return read(conn, table)
}

func (m *Migrator) readHistory(ctx context.Context) (s *snapshot, err error) {
	err = m.historySession(ctx, func(conn *sql.Conn, table dialect.Table) error {
		var err error
		s, err = readSnapshot(ctx, conn, history.New(m.dialect, table))
		return err
	})
	return s, err
}

func (m *Migrator) readPlan(ctx context.Context, dir Direction, bound bool, selectPlan func(*snapshot) ([]*unit, error)) (ms []Migration, id string, err error) {
	err = m.historySession(ctx, func(conn *sql.Conn, table dialect.Table) error {
		var target string
		if bound {
			var err error
			target, err = m.approvalTarget(ctx, conn, table)
			if err != nil {
				return err
			}
		}
		s, err := readSnapshot(ctx, conn, history.New(m.dialect, table))
		if err != nil {
			return err
		}
		var baseline string
		if dir == Up {
			s, baseline, err = m.simulateBaseline(ctx, conn, table, s)
			if err != nil {
				return err
			}
		}
		units, err := selectPlan(s)
		if err != nil {
			return err
		}
		if bound {
			id = approvedID(target, m.cat, dir, units, s, baseline)
		}
		ms = migrationsOf(units)
		return nil
	})
	return ms, id, err
}

// simulateBaseline projects the marker Up would insert on an existing schema.
func (m *Migrator) simulateBaseline(ctx context.Context, q history.Querier, table dialect.Table, s *snapshot) (*snapshot, string, error) {
	if m.opts.baselineOnUp == "" || len(s.records) > 0 {
		return s, "", nil
	}
	n, err := m.dialect.CountTables(ctx, q, table.Schema, table.Name, table.Name+"_lock")
	if err != nil || n == 0 {
		return s, "", err
	}
	return newSnapshot([]history.Record{{Rank: 1, Version: source.Canonical(m.opts.baselineOnUp),
		Kind: history.KindBaseline, Success: true}}), m.opts.baselineOnUp, nil
}

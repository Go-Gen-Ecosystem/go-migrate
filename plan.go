// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// snapshot is the history table, read with one query and indexed by key.
type snapshot struct {
	records  []history.Record
	byKey    map[string]*history.Record
	baseline string   // version of the baseline row, if any
	latest   string   // highest successfully applied version (incl. baseline)
	dirty    []string // versions with success = false
	nextRank int64    // installed_rank for the next inserted row
}

func newSnapshot(rs []history.Record) *snapshot {
	s := &snapshot{records: rs, byKey: make(map[string]*history.Record, len(rs)), nextRank: 1}
	for i := range rs {
		r := &rs[i]
		s.byKey[r.Version] = r
		s.nextRank = max(s.nextRank, r.Rank+1)
		switch {
		case !r.Success:
			s.dirty = append(s.dirty, r.Version)
		case r.Kind == history.KindRepeatable:
		default:
			if r.Kind == history.KindBaseline {
				s.baseline = r.Version
			}
			if s.latest == "" || source.Compare(r.Version, s.latest) > 0 {
				s.latest = r.Version
			}
		}
	}
	return s
}

// inspection classifies every migration against a snapshot.
type inspection struct {
	always   int // RunAlways repeatables in pending (not "behind")
	infos    []Info
	pending  []*unit // what Up would apply, in order
	problems []error // what blocks Up
}

// planner makes every decision about what to run as a pure function of the
// catalog and a history snapshot: O(n) with map lookups and no I/O, so it is
// trivially testable and safe for concurrent use.
type planner struct {
	cat          *catalog
	allowOOO     bool
	allowMissing bool
}

// inspect classifies every migration against s, collecting display rows only
// for Status.
func (p planner) inspect(s *snapshot, withStatus bool) inspection {
	var in inspection
	if withStatus {
		in.infos = make([]Info, 0, len(p.cat.units)+len(s.records))
	}
	for _, v := range s.dirty {
		in.problems = append(in.problems, fmt.Errorf("version %s: %w", v, ErrDirty))
	}
	for _, u := range p.cat.units {
		rec := s.byKey[u.key]
		var state State
		switch {
		case rec != nil && !rec.Success:
			state = StateFailed
		case rec != nil && rec.Kind == history.KindBaseline:
			state = StateBaseline
		case u.skipped && (rec == nil || u.Repeatable):
			state = StateSkipped
		case u.Repeatable && u.runAlways && rec != nil:
			state = StateRunAlways
			in.pending = append(in.pending, u)
			in.always++
		case u.Repeatable && rec == nil:
			state = StatePending
			in.pending = append(in.pending, u)
		case u.Repeatable && rec.Checksum != u.Checksum:
			state = StateOutdated
			in.pending = append(in.pending, u)
		case rec != nil && drifted(u, rec):
			state = StateModified
			in.problems = append(in.problems, fmt.Errorf("version %s (%s): %w", u.Version, u.Source, ErrChecksumMismatch))
		case rec != nil && rec.Kind == history.KindMarked:
			state = StateMarkedRan
		case rec != nil:
			state = StateApplied
		case s.baseline != "" && source.Compare(u.Version, s.baseline) <= 0:
			state = StateBelowBase
		case s.latest != "" && source.Compare(u.Version, s.latest) < 0 && !p.allowOOO && !u.retryable:
			state = StateOutOfOrder
			in.problems = append(in.problems, fmt.Errorf("version %s (latest applied %s): %w", u.Version, s.latest, ErrOutOfOrder))
		default:
			state = StatePending
			in.pending = append(in.pending, u)
		}
		if withStatus {
			in.addInfo(u.Migration, state, rec)
		}
	}
	for i := range s.records {
		r := &s.records[i]
		if _, known := p.cat.byKey[r.Version]; known || r.Kind == history.KindBaseline {
			continue
		}
		if withStatus {
			migration := Migration{Version: r.Version, Description: r.Description, Source: r.Script,
				Checksum: r.Checksum, Repeatable: r.Kind == history.KindRepeatable}
			if migration.Repeatable {
				migration.Version = ""
			}
			in.addInfo(migration, StateMissing, r)
		}
		if !p.allowMissing {
			in.problems = append(in.problems, fmt.Errorf("version %s (%s): %w", r.Version, r.Script, ErrMissingMigration))
		}
	}
	return in
}

func (in *inspection) addInfo(m Migration, state State, r *history.Record) {
	info := Info{Migration: m, State: state}
	if r != nil {
		info.InstalledOn = r.InstalledOn
		info.InstalledBy = r.InstalledBy
		info.ExecutionTime = time.Duration(r.ExecutionMS) * time.Millisecond
		info.Tag = r.Tag
	}
	in.infos = append(in.infos, info)
}

// up returns the migrations Up (or UpTo target) would apply.
func (p planner) up(s *snapshot, target string) ([]*unit, error) {
	in := p.inspect(s, false)
	if len(in.problems) > 0 {
		return nil, errors.Join(in.problems...)
	}
	if target == "" {
		return in.pending, nil
	}
	t, err := p.cat.lookupVersion(target)
	if err != nil {
		return nil, err
	}
	var plan []*unit
	for _, u := range in.pending {
		if !u.Repeatable && source.Compare(u.Version, t) <= 0 {
			plan = append(plan, u)
		}
	}
	return plan, nil
}

// down returns the migrations to roll back, newest application first.
// Every one is checked for drift and reversibility before anything runs.
func (p planner) down(s *snapshot, t DownTarget) ([]*unit, error) {
	if len(s.dirty) > 0 {
		return nil, fmt.Errorf("version %s: %w", s.dirty[0], ErrDirty)
	}
	applied := make([]*history.Record, 0, len(s.records))
	for i := len(s.records) - 1; i >= 0; i-- {
		if r := &s.records[i]; r.Kind == history.KindSQL || r.Kind == history.KindGo || r.Kind == history.KindMarked {
			applied = append(applied, r)
		}
	}
	chosen, err := p.selectDown(s, applied, t)
	if err != nil {
		return nil, err
	}
	plan := make([]*unit, 0, len(chosen))
	var problems []error
	for _, r := range chosen {
		u := p.cat.byKey[r.Version]
		switch {
		case u == nil:
			problems = append(problems, fmt.Errorf("version %s (%s): %w", r.Version, r.Script, ErrMissingMigration))
		case drifted(u, r):
			problems = append(problems, fmt.Errorf("version %s (%s): %w", u.Version, u.Source, ErrChecksumMismatch))
		case !u.Reversible && r.Kind != history.KindMarked:
			problems = append(problems, fmt.Errorf("version %s (%s): %w", u.Version, u.Source, ErrIrreversible))
		default:
			plan = append(plan, u)
		}
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return plan, nil
}

// selectDown picks the history rows t refers to from applied (newest first).
func (p planner) selectDown(s *snapshot, applied []*history.Record, t DownTarget) ([]*history.Record, error) {
	switch {
	case t.Tag != "":
		// "Applied after the tag" is about application order, not version
		// order (they differ once out-of-order runs happened).
		var tagged *history.Record
		for i := range s.records {
			if s.records[i].Tag == t.Tag {
				tagged = &s.records[i]
			}
		}
		if tagged == nil {
			return nil, fmt.Errorf("tag %q: %w", t.Tag, ErrTagNotFound)
		}
		var chosen []*history.Record
		for _, r := range applied {
			if r.Rank > tagged.Rank {
				chosen = append(chosen, r)
			}
		}
		return chosen, nil
	case t.To != "":
		v := source.Canonical(t.To)
		if v != "0" && v != s.baseline {
			var err error
			if v, err = p.cat.lookupVersion(t.To); err != nil {
				return nil, err
			}
		}
		if v != "0" && s.baseline != "" && source.Compare(v, s.baseline) < 0 {
			return nil, fmt.Errorf("version %s (baseline %s): %w", t.To, s.baseline, ErrBelowBaseline)
		}
		var chosen []*history.Record
		for _, r := range applied {
			if source.Compare(r.Version, v) > 0 {
				chosen = append(chosen, r)
			}
		}
		return chosen, nil
	default:
		if t.Steps > len(applied) {
			return nil, fmt.Errorf("%w: asked to roll back %d, %d applied", ErrNotEnoughApplied, t.Steps, len(applied))
		}
		return applied[:t.Steps], nil
	}
}

// drifted reports whether an applied, versioned migration no longer matches
// its source: the content changed, or it switched between SQL and Go.
// Repeatables are never "drifted": a change simply makes them outdated.
func drifted(u *unit, rec *history.Record) bool {
	kind := rec.Kind
	switch kind {
	case history.KindBaseline, history.KindRepeatable:
		return false
	case history.KindMarked:
		kind = history.KindSQL
	}
	return rec.Checksum != u.Checksum || kind != u.kind
}

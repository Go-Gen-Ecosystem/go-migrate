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

// inspect classifies every migration against s.
func (p planner) inspect(s *snapshot) inspection {
	in := inspection{infos: make([]Info, 0, len(p.cat.units)+len(s.records))}
	for _, v := range s.dirty {
		in.problems = append(in.problems, fmt.Errorf("version %s: %w", v, ErrDirty))
	}
	for _, u := range p.cat.units {
		info := Info{Migration: u.Migration}
		rec := s.byKey[u.key]
		if rec != nil {
			fillApplied(&info, rec)
		}
		switch {
		case rec != nil && !rec.Success:
			info.State = StateFailed
		case rec != nil && rec.Kind == history.KindBaseline:
			info.State = StateBaseline
		case u.skipped && (rec == nil || u.Repeatable):
			info.State = StateSkipped
		case u.Repeatable && u.runAlways:
			info.State = StatePending
			in.pending = append(in.pending, u)
		case u.Repeatable && rec == nil:
			info.State = StatePending
			in.pending = append(in.pending, u)
		case u.Repeatable && rec.Checksum != u.Checksum:
			info.State = StateOutdated
			in.pending = append(in.pending, u)
		case rec != nil && drifted(u, rec):
			info.State = StateModified
			in.problems = append(in.problems, fmt.Errorf("version %s (%s): %w", u.Version, u.Source, ErrChecksumMismatch))
		case rec != nil:
			info.State = StateApplied
		case s.baseline != "" && source.Compare(u.Version, s.baseline) <= 0:
			info.State = StateBelowBase
		case s.latest != "" && source.Compare(u.Version, s.latest) < 0 && !p.allowOOO:
			info.State = StateOutOfOrder
			in.problems = append(in.problems, fmt.Errorf("version %s (latest applied %s): %w", u.Version, s.latest, ErrOutOfOrder))
		default:
			info.State = StatePending
			in.pending = append(in.pending, u)
		}
		in.infos = append(in.infos, info)
	}
	for i := range s.records {
		r := &s.records[i]
		if _, known := p.cat.byKey[r.Version]; known || r.Kind == history.KindBaseline {
			continue
		}
		info := Info{
			Migration: Migration{Version: r.Version, Description: r.Description, Source: r.Script,
				Checksum: r.Checksum, Repeatable: r.Kind == history.KindRepeatable},
			State: StateMissing,
		}
		if info.Repeatable {
			info.Version = ""
		}
		fillApplied(&info, r)
		in.infos = append(in.infos, info)
		if !p.allowMissing {
			in.problems = append(in.problems, fmt.Errorf("version %s (%s): %w", r.Version, r.Script, ErrMissingMigration))
		}
	}
	return in
}

func fillApplied(info *Info, r *history.Record) {
	info.InstalledOn = r.InstalledOn
	info.InstalledBy = r.InstalledBy
	info.ExecutionTime = time.Duration(r.ExecutionMS) * time.Millisecond
	info.Tag = r.Tag
}

// up returns the migrations Up (or UpTo target) would apply.
func (p planner) up(s *snapshot, target string) ([]*unit, error) {
	in := p.inspect(s)
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

// downSelector chooses what to roll back: the last n, or everything above a version.
type downSelector struct {
	steps int
	to    string
	tag   string
}

// down returns the migrations to roll back, newest application first.
// Every one is checked for reversibility before anything runs.
func (p planner) down(s *snapshot, sel downSelector) ([]*unit, error) {
	if len(s.dirty) > 0 {
		return nil, fmt.Errorf("version %s: %w", s.dirty[0], ErrDirty)
	}
	applied := make([]*history.Record, 0, len(s.records))
	for i := len(s.records) - 1; i >= 0; i-- {
		if r := &s.records[i]; r.Kind == history.KindSQL || r.Kind == history.KindGo {
			applied = append(applied, r)
		}
	}

	if sel.tag != "" {
		sel.to = ""
		for i := range s.records {
			if s.records[i].Tag == sel.tag {
				sel.to = s.records[i].Version
			}
		}
		if sel.to == "" {
			return nil, fmt.Errorf("tag %q: %w", sel.tag, ErrTagNotFound)
		}
	}
	var chosen []*history.Record
	if sel.to != "" {
		t := source.Canonical(sel.to)
		if t != "0" && t != s.baseline {
			var err error
			if t, err = p.cat.lookupVersion(sel.to); err != nil {
				return nil, err
			}
		}
		if t != "0" && s.baseline != "" && source.Compare(t, s.baseline) < 0 {
			return nil, fmt.Errorf("version %s (baseline %s): %w", sel.to, s.baseline, ErrBelowBaseline)
		}
		for _, r := range applied {
			if source.Compare(r.Version, t) > 0 {
				chosen = append(chosen, r)
			}
		}
	} else {
		if sel.steps > len(applied) {
			return nil, fmt.Errorf("%w: asked to roll back %d, %d applied", ErrNotEnoughApplied, sel.steps, len(applied))
		}
		chosen = applied[:sel.steps]
	}

	plan := make([]*unit, 0, len(chosen))
	var problems []error
	for _, r := range chosen {
		u := p.cat.byKey[r.Version]
		switch {
		case u == nil:
			problems = append(problems, fmt.Errorf("version %s (%s): %w", r.Version, r.Script, ErrMissingMigration))
		case !u.Reversible:
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

// drifted reports whether an applied, non-repeatable migration no longer
// matches its source: the content changed, or it switched between SQL and Go.
func drifted(u *unit, rec *history.Record) bool {
	return rec.Kind != history.KindBaseline && (rec.Checksum != u.Checksum || rec.Kind != u.kind)
}

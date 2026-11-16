package migrate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// sortUnits orders versioned migrations by version, then repeatables by
// description. It runs once, in New.
func sortUnits(us []*unit) {
	sort.SliceStable(us, func(i, j int) bool {
		a, b := us[i], us[j]
		if a.Repeatable != b.Repeatable {
			return !a.Repeatable
		}
		if a.Repeatable {
			return strings.ToLower(a.Description) < strings.ToLower(b.Description)
		}
		return source.Compare(a.Version, b.Version) < 0
	})
}

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

// inspect is a pure function of the units and the snapshot: O(n) with map
// lookups, no I/O.
func (m *Migrator) inspect(s *snapshot) inspection {
	in := inspection{infos: make([]Info, 0, len(m.units)+len(s.records))}
	for _, v := range s.dirty {
		in.problems = append(in.problems, fmt.Errorf("version %s: %w", v, ErrDirty))
	}
	for _, u := range m.units {
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
		case s.latest != "" && source.Compare(u.Version, s.latest) < 0 && !m.opts.allowOOO:
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
		if _, known := m.byKey[r.Version]; known || r.Kind == history.KindBaseline {
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
		if !m.opts.allowMissing {
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

// planUp returns the migrations Up (or UpTo target) would apply.
func (m *Migrator) planUp(s *snapshot, target string) ([]*unit, error) {
	in := m.inspect(s)
	if len(in.problems) > 0 {
		return nil, errors.Join(in.problems...)
	}
	if target == "" {
		return in.pending, nil
	}
	t, err := m.lookupVersion(target)
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

// planDown returns the migrations to roll back, newest application first.
// Every one is checked for reversibility before anything runs.
func (m *Migrator) planDown(s *snapshot, sel downSelector) ([]*unit, error) {
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
			if t, err = m.lookupVersion(sel.to); err != nil {
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
		u := m.byKey[r.Version]
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

// lookupVersion canonicalises v and checks that a versioned migration has it.
func (m *Migrator) lookupVersion(v string) (string, error) {
	if !goVersion.MatchString(v) {
		return "", fmt.Errorf("version %q: %w", v, ErrVersionNotFound)
	}
	t := source.Canonical(v)
	if u, ok := m.byKey[t]; !ok || u.Repeatable {
		return "", fmt.Errorf("version %s: %w", v, ErrVersionNotFound)
	}
	return t, nil
}

func migrationsOf(us []*unit) []Migration {
	out := make([]Migration, len(us))
	for i, u := range us {
		out[i] = u.Migration
	}
	return out
}

// drifted reports whether an applied, non-repeatable migration no longer
// matches its source: the content changed, or it switched between SQL and Go.
func drifted(u *unit, rec *history.Record) bool {
	return rec.Kind != history.KindBaseline && (rec.Checksum != u.Checksum || rec.Kind != u.kind)
}

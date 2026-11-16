// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/script"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// loader builds a catalog from the configured sources: it splits SQL for the
// dialect, substitutes placeholders, resolves targeting and preconditions,
// and validates everything up front so problems fail New, not a run.
type loader struct {
	d    *dialect.Dialect
	opts *options
	cat  *catalog
}

func loadCatalog(d *dialect.Dialect, opts *options) (*catalog, error) {
	l := loader{d: d, opts: opts, cat: &catalog{byKey: map[string]*unit{}, callbacks: map[string][]callback{}}}
	set := &source.Set{}
	if opts.fsys != nil {
		var err error
		if set, err = source.LoadAll(opts.fsys); err != nil {
			return nil, err
		}
	}
	for _, f := range set.Files {
		u, err := l.sqlUnit(f)
		if err != nil {
			return nil, err
		}
		if err := l.cat.add(u); err != nil {
			return nil, err
		}
	}
	for _, cb := range set.Callbacks {
		if err := l.addCallback(cb); err != nil {
			return nil, err
		}
	}
	for _, g := range opts.goMigrations {
		u, err := goUnit(g)
		if err != nil {
			return nil, err
		}
		if err := l.cat.add(u); err != nil {
			return nil, err
		}
	}
	l.cat.sort()
	return l.cat, nil
}

func goUnit(g GoMigration) (*unit, error) {
	if !goVersion.MatchString(g.Version) || g.Up == nil {
		return nil, fmt.Errorf("go migration %q: needs a numeric version and an Up func", g.Version)
	}
	return &unit{
		Migration: Migration{
			Version: source.Canonical(g.Version), Description: g.Description,
			Source: "go", NoTx: g.NoTx, Reversible: g.Down != nil,
		},
		kind: history.KindGo,
		fns:  [2]func(context.Context, Executor) error{g.Up, g.Down},
	}, nil
}

// sqlUnit turns a loaded file into an executable unit.
func (l *loader) sqlUnit(f *source.File) (*unit, error) {
	lx := l.d.Lexical()
	stmts := [2][]string{f.Script.Up.Statements(lx), f.Script.Down.Statements(lx)}
	if len(stmts[Up]) == 0 {
		return nil, fmt.Errorf("%s: %w", f.Name, source.ErrEmptyUp)
	}
	if err := l.checkTxControl(f, stmts); err != nil {
		return nil, err
	}
	for dir := range stmts {
		var err error
		if stmts[dir], err = expandAll(stmts[dir], l.opts.placeholders); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	meta := f.Script.Meta
	if meta.RunAlways && !f.Repeatable {
		return nil, fmt.Errorf("%s: RunAlways is only valid on repeatable (R__) migrations", f.Name)
	}
	selected, err := l.selected(meta)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	pre, err := resolvePreconditions(meta.Preconditions, l.opts.placeholders)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	u := &unit{
		Migration: Migration{
			Version: f.Version, Description: f.Description, Repeatable: f.Repeatable,
			Source: f.Name, Checksum: f.Checksum, NoTx: f.Script.NoTx,
			Reversible: len(stmts[Down]) > 0,
		},
		kind:       history.KindSQL,
		stmts:      stmts,
		skipped:    !selected,
		runAlways:  meta.RunAlways,
		pre:        pre,
		lintIgnore: meta.LintIgnore,
	}
	if f.Repeatable {
		u.kind = history.KindRepeatable
	}
	return u, nil
}

// checkTxControl rejects BEGIN/COMMIT/ROLLBACK/... in migrations that run
// inside a transaction: such a statement would commit part of the migration
// and silently break atomicity. Mark the migration notransaction instead.
func (l *loader) checkTxControl(f *source.File, stmts [2][]string) error {
	if f.Script.NoTx || !l.d.TransactionalDDL() {
		return nil
	}
	for dir, list := range stmts {
		for i, s := range list {
			if script.IsTransactionControl(s) {
				return fmt.Errorf("%s: %s statement %d (%s): %w", f.Name, Direction(dir), i+1, truncateSQL(s), ErrTransactionControl)
			}
		}
	}
	return nil
}

// selected reports whether a migration's targeting directives (DBMS,
// Context, Labels) match this Migrator. Unset filters on either side match.
func (l *loader) selected(meta script.Meta) (bool, error) {
	if len(meta.DBMS) > 0 {
		names := make([]string, 0, len(meta.DBMS))
		for _, n := range meta.DBMS {
			d, err := dialect.Lookup(n)
			if err != nil {
				return false, fmt.Errorf("DBMS directive: %w", err)
			}
			names = append(names, d.Name())
		}
		if !slices.Contains(names, l.d.Name()) {
			return false, nil
		}
	}
	return matches(meta.Contexts, l.opts.contexts) && matches(meta.Labels, l.opts.labels), nil
}

func (l *loader) addCallback(cb *source.Callback) error {
	stmts, err := expandAll(cb.Script.Up.Statements(l.d.Lexical()), l.opts.placeholders)
	if err != nil {
		return fmt.Errorf("%s: %w", cb.Name, err)
	}
	l.cat.callbacks[cb.Event] = append(l.cat.callbacks[cb.Event], callback{name: cb.Name, stmts: stmts})
	return nil
}

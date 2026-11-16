// Package source discovers migration files in an fs.FS.
//
// Accepted file names (case-insensitive prefix and extension):
//
//	001_create_users.sql          versioned, Up/Down markers in one file
//	V1.2__add_index.sql           Flyway style, dotted version
//	20240101120000_seed.up.sql    golang-migrate style pair with ...down.sql
//	R__refresh_views.sql          repeatable: re-applied whenever it changes
//
// Versions compare numerically per dot-separated segment, so 9 < 10 and
// 1.9 < 1.10, and leading zeros are not significant (001 == 1).
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/script"
)

// Errors returned by Load.
var (
	ErrBadFileName      = errors.New("migration file name does not match <version>_<description>.sql or R__<description>.sql")
	ErrDuplicateVersion = errors.New("duplicate migration version")
	ErrMissingUp        = errors.New("down migration has no matching up migration")
	ErrEmptyUp          = errors.New("migration has no up SQL")
	ErrUpInDownFile     = errors.New("a .down.sql file must not contain an Up section")
)

// File is one discovered migration (a single file or an up/down pair).
type File struct {
	// Version is the canonical version ("" for repeatable migrations).
	Version     string
	Description string
	Repeatable  bool
	// Name is the file name, or "up.sql | down.sql" for a pair.
	Name     string
	Checksum string
	Script   script.Script
}

// flywayName matches Flyway's V<version>__<description>.sql, where '_' in
// the version is a segment separator (V1_2__x.sql is version 1.2).
var flywayName = regexp.MustCompile(`(?i)^v(\d+(?:[._]\d+)*)__(.+?)(\.up|\.down)?\.sql$`)

var fileName = regexp.MustCompile(`(?i)^(?:v?(\d+(?:\.\d+)*)|(r))(?:_{1,2}(.*?))?(\.up|\.down)?\.sql$`)

type pair struct {
	name       string
	desc       string
	repeatable bool
	up, down   []byte
	upName     string
	downName   string
	single     bool
}

// Load reads every *.sql file under fsys (recursively, skipping hidden
// directories) and returns versioned migrations in ascending version order
// followed by repeatable migrations ordered by description.
func Load(fsys fs.FS) ([]*File, error) {
	set, err := LoadAll(fsys)
	if err != nil {
		return nil, err
	}
	return set.Files, nil
}

// Callback events, named as in Flyway so existing callback files load as-is.
const (
	BeforeMigrate     = "beforeMigrate"
	BeforeEachMigrate = "beforeEachMigrate"
	AfterEachMigrate  = "afterEachMigrate"
	AfterMigrate      = "afterMigrate"
	AfterMigrateError = "afterMigrateError"
)

var callbackName = regexp.MustCompile(`(?i)^(beforeMigrate|beforeEachMigrate|afterEachMigrate|afterMigrate|afterMigrateError)(?:__(.+?))?\.sql$`)

// canonicalEvent maps a case-insensitively matched event to its constant.
var canonicalEvent = map[string]string{
	"beforemigrate": BeforeMigrate, "beforeeachmigrate": BeforeEachMigrate,
	"aftereachmigrate": AfterEachMigrate, "aftermigrate": AfterMigrate, "aftermigrateerror": AfterMigrateError,
}

// Callback is an SQL file run around migrations (beforeMigrate.sql,
// afterEachMigrate__grants.sql, ...). Several files may share an event; they
// run in file-name order.
type Callback struct {
	Event  string
	Name   string
	Script script.Script
}

// Set is everything found in a migration source.
type Set struct {
	Files     []*File
	Callbacks []*Callback
}

// LoadAll is Load plus SQL callback files.
func LoadAll(fsys fs.FS) (*Set, error) {
	groups := map[string]*pair{}
	set := &Set{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		// Skip non-SQL and hidden files (e.g. macOS "._001_init.sql").
		if !strings.EqualFold(path.Ext(p), ".sql") || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if m := callbackName.FindStringSubmatch(d.Name()); m != nil {
			cb, err := loadCallback(fsys, p, canonicalEvent[strings.ToLower(m[1])])
			if err != nil {
				return err
			}
			set.Callbacks = append(set.Callbacks, cb)
			return nil
		}
		return addFile(fsys, p, groups)
	})
	if err != nil {
		return nil, err
	}

	set.Files = make([]*File, 0, len(groups))
	for _, g := range groups {
		f, err := g.build()
		if err != nil {
			return nil, err
		}
		set.Files = append(set.Files, f)
	}
	sort.SliceStable(set.Files, func(i, j int) bool { return less(set.Files[i], set.Files[j]) })
	sort.SliceStable(set.Callbacks, func(i, j int) bool { return set.Callbacks[i].Name < set.Callbacks[j].Name })
	return set, nil
}

func loadCallback(fsys fs.FS, p, event string) (*Callback, error) {
	content, err := fs.ReadFile(fsys, p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	sc, err := script.Parse(content, script.Up)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &Callback{Event: event, Name: p, Script: sc}, nil
}

func addFile(fsys fs.FS, p string, groups map[string]*pair) error {
	base := path.Base(p)
	var m []string
	if fm := flywayName.FindStringSubmatch(base); fm != nil {
		m = []string{fm[0], strings.ReplaceAll(fm[1], "_", "."), "", fm[2], fm[3]}
	} else if m = fileName.FindStringSubmatch(base); m == nil {
		return fmt.Errorf("%s: %w", p, ErrBadFileName)
	}
	content, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	repeatable := m[2] != ""
	desc := strings.TrimSpace(strings.ReplaceAll(m[3], "_", " "))
	key := "v:" + Canonical(m[1])
	if repeatable {
		if desc == "" {
			return fmt.Errorf("%s: repeatable migration needs a description: %w", p, ErrBadFileName)
		}
		key = "r:" + strings.ToLower(desc)
	}

	g := groups[key]
	if g == nil {
		g = &pair{name: key, desc: desc, repeatable: repeatable}
		groups[key] = g
	}
	switch strings.ToLower(m[4]) {
	case ".up":
		if g.up != nil {
			return dup(p, g.upName)
		}
		g.up, g.upName = content, p
	case ".down":
		if g.down != nil || g.single {
			return dup(p, g.downName+g.upName)
		}
		g.down, g.downName = content, p
	default:
		if g.up != nil || g.down != nil {
			return dup(p, g.upName+g.downName)
		}
		g.up, g.upName, g.single = content, p, true
	}
	if g.desc == "" {
		g.desc = desc
	}
	return nil
}

func dup(a, b string) error {
	return fmt.Errorf("%w: %s and %s", ErrDuplicateVersion, a, b)
}

func (g *pair) build() (*File, error) {
	if g.up == nil {
		return nil, fmt.Errorf("%s: %w", g.downName, ErrMissingUp)
	}
	f := &File{Description: g.desc, Repeatable: g.repeatable, Name: g.upName}
	if !g.repeatable {
		f.Version = strings.TrimPrefix(g.name, "v:")
	}
	var err error
	if g.single {
		f.Script, err = script.Parse(g.up, script.Up)
	} else {
		f.Script, err = parsePair(g.up, g.down)
		if g.down != nil {
			f.Name = g.upName + " | " + g.downName
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	if f.Script.Up.Empty(anyComment) {
		return nil, fmt.Errorf("%s: %w", f.Name, ErrEmptyUp)
	}
	// Only the Up section is checksummed: fixing a Down section or adding a
	// missing .down.sql to an applied migration is not drift.
	f.Checksum = script.Checksum([]byte(f.Script.Up.Text()))
	return f, nil
}

func parsePair(up, down []byte) (script.Script, error) {
	u, err := script.Parse(up, script.Up)
	if err != nil {
		return script.Script{}, err
	}
	if down != nil {
		d, err := script.Parse(down, script.Down)
		if err != nil {
			return script.Script{}, err
		}
		if !d.Up.Empty(anyComment) {
			return script.Script{}, ErrUpInDownFile
		}
		u.Down = d.Down
		u.NoTx = u.NoTx || d.NoTx
	}
	return u, nil
}

func less(a, b *File) bool {
	if a.Repeatable != b.Repeatable {
		return !a.Repeatable
	}
	if a.Repeatable {
		return strings.ToLower(a.Description) < strings.ToLower(b.Description)
	}
	if c := Compare(a.Version, b.Version); c != 0 {
		return c < 0
	}
	return a.Name < b.Name
}

// Canonical strips insignificant zeros so that versions that compare equal
// have one spelling: leading zeros per segment and trailing ".0" segments go
// ("001" -> "1", "1.02" -> "1.2", "1.0" -> "1", "0.0" -> "0").
func Canonical(v string) string {
	segs := strings.Split(v, ".")
	for i, s := range segs {
		if t := strings.TrimLeft(s, "0"); t != "" {
			segs[i] = t
		} else {
			segs[i] = "0"
		}
	}
	for len(segs) > 1 && segs[len(segs)-1] == "0" {
		segs = segs[:len(segs)-1]
	}
	return strings.Join(segs, ".")
}

// anyComment recognises every comment style; used for structural checks
// that do not depend on the target dialect.
var anyComment = script.Lexical{HashComments: true}

// Compare orders two canonical versions numerically per segment without
// integer conversion, so arbitrarily long timestamps never overflow.
// Missing trailing segments compare as zero (1 == 1.0).
//
// It walks both strings segment by segment without allocating, because it
// runs O(n log n) times while sorting.
func Compare(a, b string) int {
	for a != "" || b != "" {
		var x, y string
		x, a, _ = strings.Cut(a, ".")
		y, b, _ = strings.Cut(b, ".")
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "0"
		}
		if len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}

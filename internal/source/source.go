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
	groups := map[string]*pair{}
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
		if !strings.EqualFold(path.Ext(p), ".sql") {
			return nil
		}
		return addFile(fsys, p, groups)
	})
	if err != nil {
		return nil, err
	}

	files := make([]*File, 0, len(groups))
	for _, g := range groups {
		f, err := g.build()
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool { return less(files[i], files[j]) })
	return files, nil
}

func addFile(fsys fs.FS, p string, groups map[string]*pair) error {
	base := path.Base(p)
	m := fileName.FindStringSubmatch(base)
	if m == nil {
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
		if g.down != nil {
			return dup(p, g.downName)
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
		f.Checksum = script.Checksum(g.up)
	} else {
		f.Script, err = parsePair(g.up, g.down)
		f.Checksum = script.Checksum(g.up, g.down)
		if g.down != nil {
			f.Name = g.upName + " | " + g.downName
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	if f.Script.Up.Empty() {
		return nil, fmt.Errorf("%s: %w", f.Name, ErrEmptyUp)
	}
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
		u.Down = d.Down
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
	return Compare(a.Version, b.Version) < 0
}

// Canonical strips insignificant leading zeros from every dot-separated
// segment: "001" -> "1", "1.02" -> "1.2", "0" -> "0".
func Canonical(v string) string {
	segs := strings.Split(v, ".")
	for i, s := range segs {
		if t := strings.TrimLeft(s, "0"); t != "" {
			segs[i] = t
		} else {
			segs[i] = "0"
		}
	}
	return strings.Join(segs, ".")
}

// Compare orders two canonical versions numerically per segment without
// integer conversion, so arbitrarily long timestamps never overflow.
// Missing trailing segments compare as zero (1 == 1.0).
func Compare(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := "0", "0"
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
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

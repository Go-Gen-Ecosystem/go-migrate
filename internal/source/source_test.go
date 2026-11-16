package source

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"9", "10", -1},
		{"10", "9", 1},
		{"1.9", "1.10", -1},
		{"2", "10.1", -1},
		{"1", "1.0", 0},
		{"20240101120000", "20240101120001", -1},
		{"99999999999999999999999", "100000000000000000000000", -1},
	}
	for _, tc := range tests {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%s,%s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCanonical(t *testing.T) {
	for in, want := range map[string]string{"001": "1", "0": "0", "000": "0", "1.02": "1.2", "10": "10"} {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoad_OrderAndFormats(t *testing.T) {
	fsys := mapFS(map[string]string{
		"10_ten.sql":                        "-- +migrate Up\nSELECT 10;\n-- +migrate Down\nSELECT -10;",
		"9_nine.sql":                        "SELECT 9;",
		"V1.10__dotted.sql":                 "SELECT 1.10;",
		"V1.9__dotted.sql":                  "SELECT 1.9;",
		"sub/20_pair.up.sql":                "SELECT 20;",
		"sub/20_pair.down.sql":              "SELECT -20;",
		"R__views.sql":                      "CREATE VIEW v AS SELECT 1;",
		"README.md":                         "ignored",
		".hidden/99_skipped.sql":            "SELECT 99;",
		"r__apples.sql":                     "SELECT 'a';",
		"0003_leading_zero_and_desc.sql":    "SELECT 3;",
		"sub/nested/30_deep_in_tree.sql":    "SELECT 30;",
		"sub/nested/notes.txt":              "ignored",
		"sub/nested/40_only_up_pair.up.sql": "SELECT 40;",
	})
	files, err := Load(fsys)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var order []string
	for _, f := range files {
		if f.Repeatable {
			order = append(order, "R:"+f.Description)
		} else {
			order = append(order, f.Version)
		}
	}
	want := []string{"1.9", "1.10", "3", "9", "10", "20", "30", "40", "R:apples", "R:views"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}

	byVersion := map[string]*File{}
	for _, f := range files {
		byVersion[f.Version] = f
	}
	if got := byVersion["3"].Description; got != "leading zero and desc" {
		t.Errorf("description = %q", got)
	}
	pair := byVersion["20"]
	if pair.Script.Down.Empty() || pair.Name != "sub/20_pair.up.sql | sub/20_pair.down.sql" {
		t.Errorf("pair = %+v", pair)
	}
	if !byVersion["9"].Script.Down.Empty() {
		t.Error("unmarked file must have no down section")
	}
}

func TestLoad_Errors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  error
	}{
		{"same version different padding", map[string]string{"001_a.sql": "SELECT 1;", "1_b.sql": "SELECT 1;"}, ErrDuplicateVersion},
		{"single and pair", map[string]string{"1_a.sql": "SELECT 1;", "1_a.up.sql": "SELECT 1;"}, ErrDuplicateVersion},
		{"down without up", map[string]string{"1_a.down.sql": "SELECT 1;"}, ErrMissingUp},
		{"bad name", map[string]string{"create_users.sql": "SELECT 1;"}, ErrBadFileName},
		{"repeatable without description", map[string]string{"R__.sql": "SELECT 1;"}, ErrBadFileName},
		{"empty up", map[string]string{"1_a.sql": "-- +migrate Up\n-- +migrate Down\nSELECT 1;"}, ErrEmptyUp},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(mapFS(tc.files)); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestLoad_ParseErrorNamesFile(t *testing.T) {
	_, err := Load(mapFS(map[string]string{"1_a.sql": "-- +migrate Up\n-- +migrate Up\n"}))
	if err == nil || !strings.Contains(err.Error(), "1_a.sql") {
		t.Fatalf("err = %v, want it to name the file", err)
	}
}

func TestChecksumIgnoresLineEndings(t *testing.T) {
	a, err := Load(mapFS(map[string]string{"1_a.sql": "SELECT 1;\n"}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(mapFS(map[string]string{"1_a.sql": "SELECT 1;\r\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Checksum != b[0].Checksum {
		t.Fatal("checksum must not depend on line endings")
	}
}

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package script

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

var (
	pg    = Lexical{DollarQuotes: true}
	mysql = Lexical{Backticks: true, HashComments: true, BackslashEscapes: true}
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		lx   Lexical
		want []string
	}{
		{"simple", "CREATE TABLE a (id int);\nCREATE TABLE b (id int);", Lexical{},
			[]string{"CREATE TABLE a (id int)", "CREATE TABLE b (id int)"}},
		{"no trailing semicolon", "SELECT 1", Lexical{}, []string{"SELECT 1"}},
		{"semicolon in string", "INSERT INTO t VALUES ('a;b');SELECT 2", Lexical{},
			[]string{"INSERT INTO t VALUES ('a;b')", "SELECT 2"}},
		{"doubled quote escape", "SELECT 'it''s;fine'; SELECT 2", Lexical{},
			[]string{"SELECT 'it''s;fine'", "SELECT 2"}},
		{"semicolon in quoted ident", `SELECT "a;b" FROM t; SELECT 2`, Lexical{},
			[]string{`SELECT "a;b" FROM t`, "SELECT 2"}},
		{"line comment with apostrophe and semicolon", "-- don't; split\nSELECT 1; SELECT 2", Lexical{},
			[]string{"-- don't; split\nSELECT 1", "SELECT 2"}},
		{"block comment", "/* a; b */ SELECT 1; SELECT 2", Lexical{},
			[]string{"/* a; b */ SELECT 1", "SELECT 2"}},
		{"comment-only statements dropped", "-- header\n;\n/* x */;SELECT 1;", Lexical{},
			[]string{"SELECT 1"}},
		{"pg dollar body", "CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql; SELECT 2", pg,
			[]string{"CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql", "SELECT 2"}},
		{"pg tagged dollar body", "DO $body$ BEGIN PERFORM 1; END $body$; SELECT 2", pg,
			[]string{"DO $body$ BEGIN PERFORM 1; END $body$", "SELECT 2"}},
		{"pg positional param is not a tag", "SELECT $1; SELECT $2", pg,
			[]string{"SELECT $1", "SELECT $2"}},
		{"pg E string backslash", `SELECT E'it\'s;x'; SELECT 2`, pg,
			[]string{`SELECT E'it\'s;x'`, "SELECT 2"}},
		{"standard string backslash is literal in pg", `SELECT 'a\'; SELECT 2`, pg,
			[]string{`SELECT 'a\'`, "SELECT 2"}},
		{"mysql backtick", "SELECT `a;b` FROM t; SELECT 2", mysql,
			[]string{"SELECT `a;b` FROM t", "SELECT 2"}},
		{"mysql hash comment", "# x; y\nSELECT 1; SELECT 2", mysql,
			[]string{"# x; y\nSELECT 1", "SELECT 2"}},
		{"mysql backslash escape", `SELECT 'a\';b'; SELECT 2`, mysql,
			[]string{`SELECT 'a\';b'`, "SELECT 2"}},
		{"dollar in mysql identifier", "SELECT a$b; SELECT 2", mysql,
			[]string{"SELECT a$b", "SELECT 2"}},
		{"unterminated string runs to end", "SELECT 'abc; SELECT 2", Lexical{},
			[]string{"SELECT 'abc; SELECT 2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Split(tc.sql, tc.lx); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Split() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func FuzzSplit(f *testing.F) {
	for _, s := range []string{"SELECT 1;", "$$;$$", "'a''b';", "/*;*/", "E'\\'';", "$a$ $a$;"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		for _, lx := range []Lexical{{}, pg, mysql} {
			for _, stmt := range Split(sql, lx) {
				if stmt == "" || stmt != strings.TrimSpace(stmt) {
					t.Fatalf("bad statement %q", stmt)
				}
				if !strings.Contains(sql, stmt) {
					t.Fatalf("statement %q not a substring of input", stmt)
				}
			}
		}
	})
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		def      Direction
		wantUp   []string
		wantDown []string
		wantNoTx bool
	}{
		{name: "markers", in: "-- +migrate Up\nCREATE TABLE t (id int);\n-- +migrate Down\nDROP TABLE t;\n",
			wantUp: []string{"CREATE TABLE t (id int)"}, wantDown: []string{"DROP TABLE t"}},
		{name: "case-insensitive and goose", in: "-- +goose up\nSELECT 1;\n--+GOOSE DOWN\nSELECT 2;",
			wantUp: []string{"SELECT 1"}, wantDown: []string{"SELECT 2"}},
		{name: "header comments before marker allowed", in: "-- author: me\n\n-- +migrate Up\nSELECT 1;",
			wantUp: []string{"SELECT 1"}},
		{name: "no markers is all up", in: "CREATE TABLE t (id int);\nDROP TABLE x;",
			wantUp: []string{"CREATE TABLE t (id int)", "DROP TABLE x"}},
		{name: "no markers paired down file", in: "DROP TABLE t;", def: Down,
			wantDown: []string{"DROP TABLE t"}},
		{name: "statement block kept whole", in: "-- +migrate Up\n-- +migrate StatementBegin\nCREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a=1; END;\n-- +migrate StatementEnd\nSELECT 1;",
			wantUp: []string{"CREATE TRIGGER tr AFTER INSERT ON t BEGIN UPDATE t SET a=1; END", "SELECT 1"}},
		{name: "notransaction option", in: "-- +migrate Up notransaction\nCREATE INDEX CONCURRENTLY i ON t (a);",
			wantUp: []string{"CREATE INDEX CONCURRENTLY i ON t (a)"}, wantNoTx: true},
		{name: "goose no transaction", in: "-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;",
			wantUp: []string{"SELECT 1"}, wantNoTx: true},
		{name: "crlf and bom", in: "\xEF\xBB\xBF-- +migrate Up\r\nSELECT 1;\r\n-- +migrate Down\r\nSELECT 2;\r\n",
			wantUp: []string{"SELECT 1"}, wantDown: []string{"SELECT 2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc, err := Parse([]byte(tc.in), tc.def, Lexical{})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := sc.Up.Statements(pg); !reflect.DeepEqual(got, tc.wantUp) {
				t.Errorf("Up = %q, want %q", got, tc.wantUp)
			}
			if got := sc.Down.Statements(pg); !reflect.DeepEqual(got, tc.wantDown) {
				t.Errorf("Down = %q, want %q", got, tc.wantDown)
			}
			if sc.NoTx != tc.wantNoTx {
				t.Errorf("NoTx = %v, want %v", sc.NoTx, tc.wantNoTx)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"sql before marker", "DROP TABLE t;\n-- +migrate Up\nSELECT 1;", ErrContentBeforeMarker},
		{"duplicate up", "-- +migrate Up\nSELECT 1;\n-- +migrate Up\nSELECT 2;", ErrDuplicateMarker},
		{"end without begin", "-- +migrate Up\n-- +migrate StatementEnd", ErrUnbalancedBlock},
		{"unclosed block", "-- +migrate Up\n-- +migrate StatementBegin\nSELECT 1;", ErrUnbalancedBlock},
		{"marker inside block", "-- +migrate Up\n-- +migrate StatementBegin\n-- +migrate Down", ErrUnbalancedBlock},
		{"unknown directive", "-- +migrate Sideways\nSELECT 1;", ErrUnknownDirective},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.in), Up, Lexical{}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSectionEmpty(t *testing.T) {
	sc, err := Parse([]byte("-- +migrate Up\nSELECT 1;\n-- +migrate Down\n-- nothing to undo\n"), Up, Lexical{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Up.Empty(Lexical{}) || !sc.Down.Empty(Lexical{}) {
		t.Fatalf("Up.Empty=%v Down.Empty=%v", sc.Up.Empty(Lexical{}), sc.Down.Empty(Lexical{}))
	}
}

func upChecksum(t *testing.T, src string, lx Lexical) string {
	t.Helper()
	sc, err := Parse([]byte(src), Up, lx)
	if err != nil {
		t.Fatal(err)
	}
	return sc.Up.Checksum()
}

func TestChecksumIgnoresOnlyInsignificantWhitespace(t *testing.T) {
	base := upChecksum(t, "SELECT 1;\nSELECT 2;\n", Lexical{})
	if got := upChecksum(t, "\xEF\xBB\xBFSELECT 1;  \r\nSELECT 2;\r\n\r\n", Lexical{}); got != base {
		t.Fatal("line endings, BOM and trailing whitespace outside literals must not change the checksum")
	}
	if upChecksum(t, "SELECT 1;\nSELECT 3;\n", Lexical{}) == base {
		t.Fatal("different SQL produced the same checksum")
	}
	// #10: whitespace inside a multi-line literal is SQL-significant.
	if upChecksum(t, "INSERT INTO t VALUES ('a  \nb');", Lexical{}) == upChecksum(t, "INSERT INTO t VALUES ('a\nb');", Lexical{}) {
		t.Fatal("trailing whitespace inside a literal must change the checksum")
	}
	// #10: block structure is part of what executes.
	plain := upChecksum(t, "SELECT 1;\nSELECT 2;", Lexical{})
	block := upChecksum(t, "-- +migrate StatementBegin\nSELECT 1;\nSELECT 2;\n-- +migrate StatementEnd", Lexical{})
	if plain == block {
		t.Fatal("verbatim block boundaries must change the checksum")
	}
}

// #9: directive-shaped text inside SQL is SQL, not a directive.
func TestDirectivesOnlyOutsideSQLText(t *testing.T) {
	src := "-- +migrate Up\nINSERT INTO notes VALUES ('first\n-- +migrate Down\nlast');\n" +
		"CREATE FUNCTION f() RETURNS int AS $$\n-- +migrate StatementEnd\nSELECT 1 $$ LANGUAGE sql;\n" +
		"/*\n-- +migrate Down\n*/\nSELECT 2;\n-- +migrate Down\nDELETE FROM notes;"
	sc, err := Parse([]byte(src), Up, pg)
	if err != nil {
		t.Fatal(err)
	}
	up := sc.Up.Statements(pg)
	if len(up) != 3 || !strings.Contains(up[0], "-- +migrate Down\nlast'") || !strings.Contains(up[1], "-- +migrate StatementEnd") {
		t.Fatalf("Up = %q", up)
	}
	if down := sc.Down.Statements(pg); len(down) != 1 || down[0] != "DELETE FROM notes" {
		t.Fatalf("Down = %q", down)
	}
	// MySQL: a backslash-escaped quote does not end the literal.
	my := Lexical{BackslashEscapes: true, HashComments: true}
	sc, err = Parse([]byte(`INSERT INTO t VALUES ('it\'s`+"\n-- +migrate Down\nok');"), Up, my)
	if err != nil || len(sc.Up.Statements(my)) != 1 || !sc.Down.Empty(my) {
		t.Fatalf("mysql escaped quote: %v %q", err, sc.Up.Statements(my))
	}
}

func TestParseExecutesOriginalText(t *testing.T) {
	sc, err := Parse([]byte("INSERT INTO t VALUES ('line1   \r\nline2');\r\n"), Up, Lexical{})
	if err != nil {
		t.Fatal(err)
	}
	got := sc.Up.Statements(Lexical{})
	if want := "INSERT INTO t VALUES ('line1   \nline2')"; len(got) != 1 || got[0] != want {
		t.Fatalf("statement = %q, want %q (trailing spaces in literals must survive)", got, want)
	}
}

func TestParseAllowsBlockCommentPreamble(t *testing.T) {
	for _, pre := range []string{"/* header */\n", "# mysql header\n", "-- header\n\n"} {
		sc, err := Parse([]byte(pre+"-- +migrate Up\nSELECT 1;"), Up, Lexical{})
		if err != nil || sc.Up.Empty(Lexical{}) {
			t.Errorf("preamble %q: %v", pre, err)
		}
	}
}

func TestSplitMySQLComments(t *testing.T) {
	lx := Lexical{Backticks: true, HashComments: true, BackslashEscapes: true, MySQLComments: true}
	got := Split("UPDATE t SET a=a--1;\nUPDATE u SET b=1;\n/*!40014 SET FOREIGN_KEY_CHECKS=0 */;\n/* plain */;", lx)
	want := []string{"UPDATE t SET a=a--1", "UPDATE u SET b=1", "/*!40014 SET FOREIGN_KEY_CHECKS=0 */"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Split = %q, want %q", got, want)
	}
}

func TestSectionEmptyUsesDialect(t *testing.T) {
	sc, err := Parse([]byte("-- +migrate Up\nSELECT 1;\n-- +migrate Down\n# only a mysql comment\n"), Up, Lexical{})
	if err != nil {
		t.Fatal(err)
	}
	if !sc.Down.Empty(Lexical{HashComments: true}) {
		t.Error("a '#' comment is not a statement on MySQL")
	}
}

func TestIsTransactionControl(t *testing.T) {
	for stmt, want := range map[string]bool{
		"BEGIN": true, "begin transaction": true, "START TRANSACTION": true, "COMMIT": true,
		"END": true, "ROLLBACK": true, "-- note\nCOMMIT": true, "/* x */ commit work": true, "ABORT": true,
		"ROLLBACK TO SAVEPOINT s": false, "SAVEPOINT s": false, "CREATE TABLE begin_log (id INT)": false,
		"DO $$ BEGIN PERFORM 1; END $$": false, "START REPLICA": false, "": false,
		"BEGIN/* no space */": true, "COMMIT;": true, "SELECT 'COMMIT'": false,
	} {
		if got := IsTransactionControl(stmt, pg); got != want {
			t.Errorf("IsTransactionControl(%q) = %v, want %v", stmt, got, want)
		}
	}
}

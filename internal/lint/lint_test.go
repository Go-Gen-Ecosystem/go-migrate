// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package lint

import (
	"reflect"
	"slices"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/script"
)

var lexicals = map[string]script.Lexical{
	"postgres": {DollarQuotes: true},
	"mysql":    {Backticks: true, HashComments: true, BackslashEscapes: true, MySQLComments: true},
	"sqlite":   {Backticks: true},
}

func rulesOf(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.Rule)
	}
	return out
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		dialect string
		up      []string
		noTx    bool
		ignore  []string
		want    []string
	}{
		{name: "safe additive change", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE users ADD COLUMN bio text"}},
		{name: "drop table and column", dialect: "mysql", up: []string{"DROP TABLE legacy", "ALTER TABLE users DROP COLUMN age"},
			want: []string{"drop-table", "drop-column"}},
		{name: "unbounded writes", dialect: "sqlite", up: []string{"UPDATE users SET active = 1", "DELETE FROM sessions", "DELETE FROM t WHERE id = 1"},
			want: []string{"unbounded-write", "unbounded-write"}},
		{name: "blocking index on existing table", dialect: "postgres", up: []string{"CREATE INDEX idx ON users (email)"},
			want: []string{"index-not-concurrent", "missing-lock-timeout"}},
		{name: "index on table created here is fine", dialect: "postgres",
			up: []string{"CREATE TABLE posts (id int)", "CREATE INDEX idx ON posts (id)"}},
		{name: "concurrently inside transaction", dialect: "postgres", up: []string{"SET lock_timeout = '1s'", "CREATE INDEX CONCURRENTLY idx ON users (email)"},
			want: []string{"concurrently-in-transaction"}},
		{name: "concurrently with notransaction", dialect: "postgres", noTx: true, up: []string{"SET lock_timeout = '1s'", "CREATE INDEX CONCURRENTLY idx ON users (email)"}},
		{name: "not null without default", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE users ADD COLUMN tier text NOT NULL"},
			want: []string{"add-not-null-without-default"}},
		{name: "not null with default", dialect: "sqlite", up: []string{"ALTER TABLE users ADD COLUMN tier TEXT NOT NULL DEFAULT 'free'"}},
		{name: "mysql implicit default is a warning", dialect: "mysql", up: []string{"ALTER TABLE users ADD COLUMN tier INT NOT NULL"},
			want: []string{"add-not-null-without-default"}},
		{name: "type change and constraints", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'",
			"ALTER TABLE users ALTER COLUMN id TYPE bigint", "ALTER TABLE posts ADD CONSTRAINT fk FOREIGN KEY (user_id) REFERENCES users (id)",
			"ALTER TABLE posts ADD CONSTRAINT fk2 FOREIGN KEY (x) REFERENCES t (id) NOT VALID", "ALTER TABLE users ADD CONSTRAINT c CHECK (age > 0)",
			"ALTER TABLE users ALTER COLUMN email SET NOT NULL"},
			want: []string{"column-type-change", "foreign-key-not-valid", "check-not-valid", "set-not-null"}},
		{name: "mysql modify", dialect: "mysql", up: []string{"ALTER TABLE users MODIFY COLUMN name VARCHAR(500)"}, want: []string{"column-type-change"}},
		{name: "rename", dialect: "sqlite", up: []string{"ALTER TABLE users RENAME COLUMN name TO full_name"}, want: []string{"rename"}},
		{name: "sqlite unsupported alter", dialect: "sqlite", up: []string{"ALTER TABLE users ALTER COLUMN name SET NOT NULL"},
			want: []string{"sqlite-unsupported-alter"}},
		{name: "literals and comments never match", dialect: "postgres",
			up: []string{"INSERT INTO audit (msg) VALUES ('DROP TABLE users; UPDATE x SET y = 1') -- DROP TABLE t"}},
		{name: "suppressed", dialect: "mysql", ignore: []string{"drop-table"}, up: []string{"DROP TABLE legacy"}},
		{name: "vacuum full", dialect: "postgres", up: []string{"VACUUM FULL users"}, want: []string{"full-table-rewrite"}},
		// Second review: false positives.
		{name: "recommended NOT VALID check is not a NOT NULL column", dialect: "postgres",
			up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE t ADD CONSTRAINT c_nn CHECK (c IS NOT NULL) NOT VALID"}},
		{name: "serial and identity columns have defaults", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'",
			"ALTER TABLE t ADD COLUMN id bigserial NOT NULL", "ALTER TABLE t ADD COLUMN n int GENERATED ALWAYS AS IDENTITY NOT NULL"}},
		{name: "refresh concurrently is legal in a transaction", dialect: "postgres", up: []string{"REFRESH MATERIALIZED VIEW CONCURRENTLY mv"}},
		{name: "quoted table created here", dialect: "postgres", up: []string{`CREATE TABLE "users" (id int)`, "CREATE INDEX i ON users (id)"}},
		{name: "schema-qualified table created here", dialect: "postgres", up: []string{"CREATE TABLE app.users (id int)", "CREATE INDEX i ON app.users (id)"}},
		// Second review: false negatives.
		{name: "not null with parenthesised type", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE t ADD COLUMN price numeric(10,2) NOT NULL"},
			want: []string{"add-not-null-without-default"}},
		{name: "multi-action alter", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE t ADD COLUMN a int DEFAULT 0, ADD COLUMN b int NOT NULL"},
			want: []string{"add-not-null-without-default"}},
		{name: "unnamed index", dialect: "postgres", up: []string{"CREATE INDEX ON t (a)"}, want: []string{"index-not-concurrent", "missing-lock-timeout"}},
		{name: "drop without COLUMN keyword", dialect: "mysql", up: []string{"ALTER TABLE t DROP c"}, want: []string{"drop-column"}},
		{name: "drop constraint is not a dropped column", dialect: "mysql", up: []string{"ALTER TABLE t DROP INDEX i", "ALTER TABLE t DROP FOREIGN KEY f"}},
		{name: "drop schema", dialect: "postgres", up: []string{"DROP SCHEMA app CASCADE"}, want: []string{"drop-schema"}},
		{name: "where only inside a subquery", dialect: "sqlite", up: []string{"UPDATE t SET a = (SELECT b FROM u WHERE u.id = 1)"}, want: []string{"unbounded-write"}},
		{name: "rename with IF EXISTS", dialect: "postgres", up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE IF EXISTS t RENAME TO t2"}, want: []string{"rename"}},
		{name: "alter only with parenthesized target", dialect: "postgres", up: []string{"ALTER TABLE ONLY (app.t) ADD COLUMN c int NOT NULL"},
			want: []string{"add-not-null-without-default", "missing-lock-timeout"}},
		{name: "SET LOCAL does nothing outside a transaction", dialect: "postgres", noTx: true,
			up: []string{"SET LOCAL lock_timeout = '5s'", "ALTER TABLE t ADD COLUMN a int"}, want: []string{"missing-lock-timeout"}},
		{name: "sqlite multi-action alter is unsupported", dialect: "sqlite", up: []string{"ALTER TABLE t ADD COLUMN a INT, ADD COLUMN b INT"},
			want: []string{"sqlite-unsupported-alter"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rulesOf(Check(Migration{Dialect: tc.dialect, Up: tc.up, NoTx: tc.noTx, Ignore: tc.ignore,
				Reversible: true, Versioned: true, Lexical: lexicals[tc.dialect]}))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("rules = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIrreversibleAndStatementNumbers(t *testing.T) {
	issues := Check(Migration{Dialect: "sqlite", Up: []string{"CREATE TABLE a (id INT)", "DROP TABLE b"}, Versioned: true, Lexical: lexicals["sqlite"]})
	if got := rulesOf(issues); !reflect.DeepEqual(got, []string{"drop-table", "irreversible"}) {
		t.Fatalf("rules = %v", got)
	}
	if issues[0].Statement != 2 || issues[0].Severity != Warning || issues[1].Statement != 0 || issues[1].Severity != Info {
		t.Fatalf("issues = %+v", issues)
	}
	if ids := RuleIDs(); len(ids) < 15 || ids[0] > ids[len(ids)-1] {
		t.Fatalf("RuleIDs = %v", ids)
	}
}

func TestPostgresIndexConcurrency(t *testing.T) {
	tests := []struct {
		name string
		stmt string
		want []string
	}{
		{name: "quoted index and target", stmt: `CREATE INDEX "name ON CONCURRENTLY" ON "concurrently" (id)`, want: []string{"index-not-concurrent"}},
		{name: "identifier suffix", stmt: `CREATE INDEX concurrently$archive ON t (id)`, want: []string{"index-not-concurrent"}},
		{name: "column and tablespace", stmt: `CREATE INDEX ON t ("CONCURRENTLY") TABLESPACE "CONCURRENTLY"`, want: []string{"index-not-concurrent"}},
		{name: "comments and literal", stmt: `CREATE INDEX /* concurrently */ i ON t (id) WHERE label = 'CONCURRENTLY'`, want: []string{"index-not-concurrent"}},
		{name: "quoted drop target", stmt: `DROP INDEX IF EXISTS "CONCURRENTLY"`},
		{name: "quoted reindex target", stmt: `REINDEX INDEX "CONCURRENTLY"`},
		{name: "reindex tablespace containing separators", stmt: `REINDEX (TABLESPACE "), CONCURRENTLY (") INDEX i`},
		{name: "concurrent unique create", stmt: `CREATE UNIQUE INDEX /* note */ CONCURRENTLY IF NOT EXISTS i ON ONLY t (id)`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent drop", stmt: `DROP INDEX CONCURRENTLY IF EXISTS "concurrently"`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex", stmt: `REINDEX (VERBOSE) TABLE CONCURRENTLY t`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex with implicit true", stmt: `REINDEX (CONCURRENTLY) INDEX i`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex with true", stmt: `REINDEX (VERBOSE, CONCURRENTLY true) INDEX i`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex with on", stmt: `REINDEX (CONCURRENTLY on) INDEX i`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex with one", stmt: `REINDEX (CONCURRENTLY 1) INDEX i`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex with signed one", stmt: `REINDEX (CONCURRENTLY +01) INDEX i`, want: []string{"concurrently-in-transaction"}},
		{name: "concurrent reindex with quoted option and value", stmt: `REINDEX ("concurrently" 'ON') INDEX i`, want: []string{"concurrently-in-transaction"}},
		{name: "reindex with false", stmt: `REINDEX (CONCURRENTLY false) TABLE t`},
		{name: "reindex with off", stmt: `REINDEX (CONCURRENTLY off) TABLE t`},
		{name: "reindex with zero", stmt: `REINDEX (CONCURRENTLY 0, TABLESPACE 'concurrently') TABLE t`},
		{name: "reindex with quoted off", stmt: `REINDEX (CONCURRENTLY 'off') TABLE t`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := Migration{Dialect: "postgres", Lexical: lexicals["postgres"], Up: []string{"SET lock_timeout = '1s'", tc.stmt}}
			if got := rulesOf(Check(m)); !slices.Equal(got, tc.want) {
				t.Fatalf("transactional rules = %v, want %v", got, tc.want)
			}
			if slices.Contains(tc.want, "concurrently-in-transaction") {
				m.NoTx = true
				if got := rulesOf(Check(m)); len(got) != 0 {
					t.Fatalf("nontransactional rules = %v, want none", got)
				}
			}
		})
	}
}

func TestPostgresIndexesOnNewTables(t *testing.T) {
	m := Migration{Dialect: "postgres", Lexical: lexicals["postgres"], Up: []string{
		`CREATE TABLE app."fresh table" (id int)`,
		`CREATE TABLE IF NOT EXISTS app."fresh table" (id int)`,
		`CREATE INDEX "name ON existing" ON app . "fresh table" (id)`,
		`ALTER TABLE app."fresh table" ADD COLUMN name text NOT NULL`,
	}}
	if got := rulesOf(Check(m)); len(got) != 0 {
		t.Fatalf("rules for the new table = %v, want none", got)
	}
	m.Up = append(m.Up, `CREATE INDEX CONCURRENTLY i ON app."fresh table" (id)`)
	if got := rulesOf(Check(m)); !slices.Equal(got, []string{"concurrently-in-transaction"}) {
		t.Fatalf("rules for concurrent index on the new table = %v", got)
	}
}

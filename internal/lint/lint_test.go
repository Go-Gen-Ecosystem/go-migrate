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

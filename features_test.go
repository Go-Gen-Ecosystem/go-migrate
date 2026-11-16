// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
)

func TestPlaceholders(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files("1_t.sql", "CREATE TABLE ${table_name} (id INTEGER, d TEXT DEFAULT '${migrate:dialect}');")
	m := newM(t, db, fsys, WithPlaceholders(map[string]string{"table_name": "accounts"}))
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, db, "accounts") {
		t.Fatal("placeholder not substituted")
	}
	// Different values are not drift: checksums are taken before substitution.
	if err := newM(t, db, fsys, WithPlaceholders(map[string]string{"table_name": "other"})).Validate(ctx); err != nil {
		t.Fatalf("placeholder values must not change the checksum: %v", err)
	}
	if _, err := New(db, "sqlite", WithFS(fsys), WithPlaceholders(map[string]string{})); !errors.Is(err, ErrUnresolvedPlaceholder) {
		t.Fatalf("unresolved placeholder = %v", err)
	}
}

func TestTargeting(t *testing.T) {
	ctx := context.Background()
	fsys := files(
		"1_base.sql", "CREATE TABLE base (id INTEGER);",
		"2_seed_test.sql", "-- +migrate Context test,dev\nCREATE TABLE seed (id INTEGER);",
		"3_pg_only.sql", "-- +migrate DBMS postgresql\nCREATE TABLE pg (id INTEGER);",
		"4_billing.sql", "-- +migrate Labels billing\nCREATE TABLE billing (id INTEGER);",
	)
	db := openDB(t)
	m := newM(t, db, fsys, WithContexts("prod"), WithLabels("billing"))
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1", "4"})
	st, _ := m.Status(ctx)
	if st.Migrations[1].State != StateSkipped || st.Migrations[2].State != StateSkipped || st.Pending != 0 {
		t.Fatalf("states = %+v", st.Migrations)
	}
	// Enabling the context later applies the skipped migration (out of order).
	res, err = newM(t, db, fsys, WithContexts("test"), WithAllowOutOfOrder(true)).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"2"})
	// Without context/label selection every non-DBMS-filtered migration runs.
	if res, err := newM(t, openDB(t), fsys).Up(ctx); err != nil || len(res.Applied) != 3 {
		t.Fatalf("no selection = %v, %v", versions(res), err)
	}
	if _, err := New(db, "sqlite", WithFS(files("1_x.sql", "-- +migrate DBMS oracle\nSELECT 1;"))); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("unknown DBMS = %v", err)
	}
}

func TestRunAlways(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_t.sql", "CREATE TABLE hits (n INTEGER);",
		"R__touch.sql", "-- +migrate RunAlways\nINSERT INTO hits VALUES (1);",
	)
	for range 3 {
		if _, err := newM(t, db, fsys).Up(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM hits").Scan(&n); err != nil || n != 3 {
		t.Fatalf("hits = %d, %v; RunAlways must run on every Up", n, err)
	}
	if _, err := New(db, "sqlite", WithFS(files("5_x.sql", "-- +migrate RunAlways\nSELECT 1;"))); err == nil {
		t.Fatal("RunAlways on a versioned migration must be rejected")
	}
}

func TestPreconditions(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE legacy (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	exists := "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'legacy'"
	fsys := files(
		"1_create_legacy.sql", "-- +migrate Precondition onFail=MARK_RAN expect=0 "+exists+"\nCREATE TABLE legacy (id INTEGER);",
		"2_maybe.sql", "-- +migrate Precondition onFail=CONTINUE expect=1 SELECT 0\nCREATE TABLE maybe (id INTEGER);",
		"3_ok.sql", "-- +migrate Precondition expect=1 "+exists+"\nCREATE TABLE ok (id INTEGER);",
	)
	m := newM(t, db, fsys, WithAllowOutOfOrder(true))
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1", "3"})
	if !res.Applied[0].MarkedRan || res.Applied[1].MarkedRan {
		t.Fatalf("MarkedRan flags = %+v", res.Applied)
	}
	if tableExists(t, db, "maybe") || !tableExists(t, db, "ok") {
		t.Fatal("CONTINUE must skip; a holding precondition must run")
	}
	if st, _ := m.Status(ctx); st.Pending != 1 {
		t.Fatalf("CONTINUE migration must stay pending: %+v", st)
	}

	halt := files("1_h.sql", "-- +migrate Precondition onFail=HALT expect=42 SELECT 1\nCREATE TABLE h (id INTEGER);")
	if _, err := newM(t, openDB(t), halt).Up(ctx); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("HALT = %v", err)
	}
	bad := files("1_b.sql", "-- +migrate Precondition onFail=MAYBE expect=1 SELECT 1\nSELECT 1;")
	if _, err := New(db, "sqlite", WithFS(bad)); !errors.Is(err, ErrBadPrecondition) {
		t.Fatalf("malformed precondition = %v", err)
	}
}

func TestSQLCallbacks(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := db.Exec("CREATE TABLE audit (event TEXT)"); err != nil {
		t.Fatal(err)
	}
	fsys := files(
		"beforeMigrate.sql", "INSERT INTO audit VALUES ('before');",
		"afterEachMigrate__count.sql", "INSERT INTO audit VALUES ('each');",
		"afterMigrate.sql", "INSERT INTO audit VALUES ('after');",
		"afterMigrateError.sql", "INSERT INTO audit VALUES ('error');",
		"1_a.sql", "CREATE TABLE a (id INTEGER);",
		"2_b.sql", "CREATE TABLE b (id INTEGER);",
	)
	if _, err := newM(t, db, fsys).Up(ctx); err != nil {
		t.Fatal(err)
	}
	fsys["3_bad.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO nope VALUES (1);")}
	if _, err := newM(t, db, fsys).Up(ctx); err == nil {
		t.Fatal("expected failure")
	}
	rows, err := db.Query("SELECT event FROM audit")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	eq(t, got, []string{"before", "each", "each", "after", "before", "error"})
}

func TestTagsAndStatements(t *testing.T) {
	ctx := context.Background()
	m := newM(t, openDB(t), basic, WithPlaceholders(map[string]string{}))
	if _, err := m.UpTo(ctx, "9"); err != nil {
		t.Fatal(err)
	}
	if err := m.Tag(ctx, "release-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := m.Status(ctx)
	if st.Migrations[0].Tag != "release-1" || st.Migrations[1].Tag != "" {
		t.Fatalf("tags = %q, %q", st.Migrations[0].Tag, st.Migrations[1].Tag)
	}
	plan, err := m.PlanDown(ctx, DownTarget{Tag: "release-1"})
	if err != nil || len(plan) != 2 {
		t.Fatalf("PlanDownToTag = %v, %v", plan, err)
	}
	stmts := m.Statements(plan[0], Down)
	if len(stmts) != 1 || stmts[0] != "DROP TABLE eleven" {
		t.Fatalf("Statements = %q", stmts)
	}
	res, err := m.Down(ctx, DownTarget{Tag: "release-1"})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"11", "10"})
	if _, err := m.Down(ctx, DownTarget{Tag: "nope"}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("unknown tag = %v", err)
	}
	if err := m.Tag(ctx, "release-1"); err != nil {
		t.Fatalf("re-tagging moves the tag: %v", err)
	}
	if got := m.Statements(Migration{Version: "404"}, Up); got != nil {
		t.Fatalf("unknown migration = %q", got)
	}
}

func TestLintWithoutDatabase(t *testing.T) {
	fsys := files(
		"1_users.sql", "-- +migrate Up\nCREATE TABLE users (id INTEGER);\n-- +migrate Down\nDROP TABLE users;",
		"2_risky.sql", "ALTER TABLE users ADD COLUMN tier TEXT NOT NULL;\nDROP TABLE legacy;",
		"3_quiet.sql", "-- +migrate Lint ignore=drop-table,irreversible\nDROP TABLE old;",
	)
	findings, err := Lint("sqlite", WithFS(fsys))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		got = append(got, f.Version+":"+f.Rule)
	}
	eq(t, got, []string{"2:add-not-null-without-default", "2:drop-table", "2:irreversible"})
	if findings[0].Severity != "error" || findings[0].Statement != 1 || findings[0].Source != "2_risky.sql" {
		t.Fatalf("finding = %+v", findings[0])
	}
	if len(LintRules()) == 0 {
		t.Fatal("LintRules must list the rules")
	}
	if _, err := Lint("oracle"); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("Lint(oracle) = %v", err)
	}
}

func TestBaselineOnMigrate(t *testing.T) {
	ctx := context.Background()
	fsys := files("1_a.sql", "CREATE TABLE a (id INTEGER);", "2_b.sql", "CREATE TABLE b (id INTEGER);", "3_c.sql", "CREATE TABLE c (id INTEGER);")

	existing := openDB(t)
	if _, err := existing.Exec("CREATE TABLE a (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := existing.Exec("CREATE TABLE b (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	res, err := newM(t, existing, fsys, WithBaselineOnMigrate("2")).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"3"})

	res, err = newM(t, openDB(t), fsys, WithBaselineOnMigrate("2")).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1", "2", "3"})
}

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/config"
	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

const oracleFixtureEnv = "GOMIGRATE_TEST_ORACLE_DSN"

func oracleTarget() target {
	return target{
		dialect: dialect.Oracle,
		open:    openOracle,
		failing: "CREATE TABLE half (id INT);\nINSERT INTO nope VALUES (1);",
		special: map[string]string{
			"5_proc.sql": `-- +migrate Up
CREATE TABLE ora_audit (n INT, message VARCHAR2(40));
-- +migrate StatementBegin
CREATE OR REPLACE PROCEDURE write_audit AS
BEGIN
    INSERT INTO ora_audit VALUES (1, q'[it's;fine]');
    INSERT INTO ora_audit VALUES (2, q'{a;b}');
END;
-- +migrate StatementEnd
-- +migrate StatementBegin
BEGIN write_audit; END;
-- +migrate StatementEnd
-- +migrate Down
DROP PROCEDURE write_audit;
DROP TABLE ora_audit;`,
		},
		specialCheck: "SELECT COUNT(*) FROM ora_audit WHERE message IN (q'[it's;fine]', q'{a;b}')",
	}
}

func oracleFixtureURL(t *testing.T) *url.URL {
	t.Helper()
	base := os.Getenv(oracleFixtureEnv)
	if base == "" {
		t.Skip(oracleFixtureEnv + " not set")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "oracle" || u.Hostname() == "" {
		t.Fatal("invalid Oracle test fixture URL")
	}
	return u
}

func oracleFixtureDB(t *testing.T, u *url.URL) *sql.DB {
	t.Helper()
	db, name, err := config.Open(t.Context(), config.Database{DSN: u.String()})
	if err != nil || name != dialect.Oracle {
		t.Fatalf("open isolated Oracle fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openOracle(t *testing.T) *sql.DB {
	t.Helper()
	u := oracleFixtureURL(t)
	admin := oracleFixtureDB(t, u)
	name := strings.ToUpper(randName("gomigrate_"))
	password := randName("Gm_26AI_")
	if _, err := admin.ExecContext(t.Context(), `CREATE USER `+name+` IDENTIFIED BY "`+password+`" QUOTA UNLIMITED ON USERS`); err != nil {
		t.Fatalf("create disposable user %s: %s", name, strings.ReplaceAll(err.Error(), password, "[redacted]"))
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP USER "+name+" CASCADE"); err != nil {
			t.Errorf("drop isolated Oracle user %s: %v", name, err)
		}
	})
	for _, grant := range []string{
		"GRANT CREATE SESSION, CREATE TABLE, CREATE PROCEDURE, CREATE VIEW TO ",
		"GRANT EXECUTE ON SYS.DBMS_LOCK TO ",
	} {
		if _, err := admin.ExecContext(t.Context(), grant+name); err != nil {
			t.Fatal(err)
		}
	}
	u.User = url.UserPassword(name, password)
	return oracleFixtureDB(t, u)
}

func TestOracleScriptComposition(t *testing.T) {
	db, err := sql.Open("oracledb", "build/unused@tcp://localhost:1521/unreachable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const quoted = "INSERT INTO audit VALUES (q'[it's; fine\n-- +migrate Down\n]', nq'{more; text}')"
	const block = "BEGIN INSERT INTO audit VALUES (q'!it's; okay!'); END;"
	src := "-- +migrate Up\n" + quoted + ";\n-- +migrate StatementBegin\n" + block + "\n-- +migrate StatementEnd"
	m, err := New(db, "oracledb", WithFS(files("1_blocks.sql", src, "beforeMigrate.sql", "-- +migrate StatementBegin\nBEGIN NULL; END;\n-- +migrate StatementEnd")))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, m.Statements(m.Migrations()[0], Up), []string{quoted, block})
	for _, text := range []string{
		"BEGIN NULL; END;",
		"-- +migrate StatementBegin\nBEGIN NULL; END\n-- +migrate StatementEnd",
		"CREATE OR REPLACE PROCEDURE p AS BEGIN NULL; END;",
		"-- +migrate StatementBegin\nBEGIN NULL; END;\n/\n-- +migrate StatementEnd",
	} {
		if _, err := New(db, "oracle", WithFS(files("1_invalid.sql", text))); !errors.Is(err, ErrPLSQLBlock) {
			t.Fatalf("unwrapped or incomplete PL/SQL = %v", err)
		}
	}
}

func TestOracleCancellationBeforeWritingHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d, _ := dialect.Lookup(dialect.Oracle)
	o := defaults()
	var after error
	o.hooks.Before = func(context.Context, Event) error { cancel(); return nil }
	o.hooks.After = func(_ context.Context, e Event) { after = e.Err }
	// Cancellation in the before hook must stop at dispatch, before Oracle
	// could receive a dirty marker or any migration SQL.
	a := applier{d: d, opts: &o}
	s := newSnapshot(nil)
	_, err := a.apply(ctx, &unit{key: "1"}, Up, s)
	if !errors.Is(err, context.Canceled) || !errors.Is(after, context.Canceled) || s.nextRank != 1 {
		t.Fatalf("cancelled step = %v, after hook = %v, next rank = %d", err, after, s.nextRank)
	}
}

func conformOracleVersion(t *testing.T) {
	db := oracleFixtureDB(t, oracleFixtureURL(t))
	var banner string
	if err := db.QueryRowContext(t.Context(), "SELECT BANNER_FULL FROM V$VERSION WHERE BANNER LIKE 'Oracle%'").Scan(&banner); err != nil {
		t.Fatal(err)
	}
	t.Log(strings.Join(strings.Fields(banner), " "))
}

func conformOracleLock(t *testing.T) {
	db := openOracle(t)
	d, _ := dialect.Lookup(dialect.Oracle)
	ctx := t.Context()
	held, err := d.Lock(ctx, db, dialect.Table{Name: "ChangeHistory"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release(ctx) })
	if _, err := held.Conn.ExecContext(ctx, "CREATE TABLE locked_ddl (n INT)"); err != nil {
		t.Fatal(err)
	}
	for _, wait := range []time.Duration{0, 150 * time.Millisecond} {
		second, err := d.Lock(ctx, db, dialect.Table{Name: "changehistory"}, wait)
		if second != nil {
			_ = second.Release(ctx)
		}
		if !errors.Is(err, ErrLockTimeout) {
			t.Fatalf("competing Oracle lock after implicit DDL commit = %v, want timeout", err)
		}
	}
	if err := held.Release(ctx); err != nil {
		t.Fatal(err)
	}
	held, err = d.Lock(ctx, db, dialect.Table{Name: "CHAngEHistory"}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func conformOracleSession(t *testing.T, tg target) {
	ctx := t.Context()
	db := tg.open(t)
	db.SetMaxOpenConns(1)
	m, err := New(db, "oracle", WithFS(files(
		"1_switch.sql", "ALTER SESSION SET CURRENT_SCHEMA = SYSTEM;",
		"2_still_original.sql", "-- +migrate StatementBegin\nBEGIN NULL; END;\n-- +migrate StatementEnd",
	)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if st, err := m.Status(ctx); err != nil || st.Current != "2" || st.Dirty {
		t.Fatalf("history lost after CURRENT_SCHEMA change: %+v, %v", st, err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM "+historyTable(tg)); n != 2 {
		t.Fatalf("own history from the refreshed pool = %d", n)
	}
}

func conformOracleEmptyChecksum(t *testing.T) {
	db := openOracle(t)
	ctx := t.Context()
	m, err := New(db, "oracle", WithGoMigrations(GoMigration{
		Version: "2",
		Up: func(ctx context.Context, ex Executor) error {
			_, err := ex.ExecContext(ctx, "CREATE TABLE via_go (n INT)")
			return err
		},
		Down: func(ctx context.Context, ex Executor) error {
			_, err := ex.ExecContext(ctx, "DROP TABLE via_go")
			return err
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Baseline(ctx, "1", "initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := m.Status(ctx)
	if err != nil || st.Current != "2" || st.Dirty {
		t.Fatalf("empty Go and baseline checksums: %+v, %v", st, err)
	}
	for _, row := range st.Migrations {
		if row.Checksum != "" {
			t.Fatalf("empty checksum = %q", row.Checksum)
		}
	}
	if _, err := m.Down(ctx, DownTarget{Steps: 1}); err != nil {
		t.Fatal(err)
	}
}

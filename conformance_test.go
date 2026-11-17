// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

// Dialect conformance suite: the same scenarios run against every database.
// SQLite always runs; other engines run when their fixture DSNs are set, e.g.
//
//	GOMIGRATE_TEST_POSTGRES_DSN=postgres://postgres:pw@localhost:5432/postgres?sslmode=disable
//	GOMIGRATE_TEST_MYSQL_DSN=root:pw@tcp(localhost:3306)/
//	GOMIGRATE_TEST_SQLSERVER_DSN=sqlserver://sa:<password>@localhost:1433?database=master&trustservercertificate=true
//	GOMIGRATE_TEST_ORACLE_DSN=oracle://fixture_admin:<password>@localhost:1521/FREEPDB1?protocol=tcp
//	GOMIGRATE_TEST_DB2_DSN=db2://fixture_admin:<password>@localhost:50001/GTMTEST?ssl_ca=/path/to/test-ca.pem
//	GOMIGRATE_TEST_DB2_APP_DSN=db2://fixture_app:<password>@localhost:50001/GTMTEST?ssl_ca=/path/to/test-ca.pem
//
// (see `make test-integration`). Each test gets its own schema/database.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

type target struct {
	dialect string
	open    func(t *testing.T) *sql.DB
	// failing returns a script whose second statement fails.
	failing string
	// special exercises dialect-specific lexical features.
	special map[string]string
	// specialCheck verifies the special migration ran correctly.
	specialCheck string
}

func randName(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func targets() []target {
	return []target{
		{
			dialect: "sqlite",
			open:    func(t *testing.T) *sql.DB { return openDB(t) },
			failing: "CREATE TABLE half (id INTEGER);\nINSERT INTO nope VALUES (1);",
			special: map[string]string{
				"5_trigger.sql": "-- +migrate Up\nCREATE TABLE audit (n INTEGER);\nCREATE TABLE items (id INTEGER);\n" +
					"-- +migrate StatementBegin\nCREATE TRIGGER items_ai AFTER INSERT ON items BEGIN INSERT INTO audit VALUES (1); INSERT INTO audit VALUES (2); END;\n" +
					"-- +migrate StatementEnd\nINSERT INTO items VALUES (1);\n-- +migrate Down\nDROP TABLE items;\nDROP TABLE audit;",
			},
			specialCheck: "SELECT COUNT(*) FROM audit",
		},
		{
			dialect: "postgres",
			open:    openPostgres,
			failing: "CREATE TABLE half (id INT);\nINSERT INTO nope VALUES (1);",
			special: map[string]string{
				"5_fn.sql": "-- +migrate Up\nCREATE TABLE audit (n INT);\n" +
					"CREATE FUNCTION two() RETURNS INT AS $$ BEGIN INSERT INTO audit VALUES (1); RETURN 2; END; $$ LANGUAGE plpgsql;\n" +
					"INSERT INTO audit VALUES (two());\n-- +migrate Down\nDROP FUNCTION two();\nDROP TABLE audit;",
				"6_concurrent_index.sql": "-- +migrate Up notransaction\nCREATE INDEX CONCURRENTLY audit_n ON audit (n);\n-- +migrate Down notransaction\nDROP INDEX CONCURRENTLY audit_n;",
			},
			specialCheck: "SELECT COUNT(*) FROM audit",
		},
		{
			dialect: "mysql",
			open:    openMySQL,
			failing: "CREATE TABLE half (id INT);\nINSERT INTO nope VALUES (1);",
			special: map[string]string{
				"5_lexical.sql": "-- +migrate Up\n# hash comment; not a statement end\nCREATE TABLE `audit` (`n;x` INT, s VARCHAR(20));\n" +
					"INSERT INTO `audit` VALUES (1, 'it\\'s;fine');\nINSERT INTO `audit` VALUES (2, 'a;b');\n-- +migrate Down\nDROP TABLE `audit`;",
			},
			specialCheck: "SELECT COUNT(*) FROM `audit`",
		},
		sqlServerTarget(),
		oracleTarget(),
		db2Target(),
	}
}

func openPostgres(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("GOMIGRATE_TEST_POSTGRES_DSN")
	if base == "" {
		t.Skip("GOMIGRATE_TEST_POSTGRES_DSN not set")
	}
	admin := mustOpen(t, "pgx", base)
	schema := randName("t_")
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return mustOpen(t, "pgx", base+sep+"search_path="+schema)
}

func openMySQL(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv("GOMIGRATE_TEST_MYSQL_DSN")
	if base == "" {
		t.Skip("GOMIGRATE_TEST_MYSQL_DSN not set")
	}
	admin := mustOpen(t, "mysql", base)
	name := randName("t_")
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name) })
	// Deliberately no parseTime=true: the library must not depend on it.
	return mustOpen(t, "mysql", base+name)
}

func mustOpen(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		if err = db.PingContext(ctx); err == nil {
			return db
		}
		select {
		case <-ctx.Done():
			t.Fatalf("database not reachable: %v", err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

var lifecycle = files(
	"1_users.sql", "-- +migrate Up\nCREATE TABLE users (id INT NOT NULL PRIMARY KEY, name VARCHAR(50));\n-- +migrate Down\nDROP TABLE users;",
	"2_posts.up.sql", "CREATE TABLE posts (id INT NOT NULL PRIMARY KEY, user_id INT);",
	"2_posts.down.sql", "DROP TABLE posts;",
	"10_seed.sql", "-- +migrate Up\nINSERT INTO users VALUES (1, 'ada; lovelace');\n-- +migrate Down\nDELETE FROM users;",
)

func TestConformance(t *testing.T) {
	for _, tg := range targets() {
		t.Run(tg.dialect, func(t *testing.T) {
			t.Run("lifecycle", func(t *testing.T) { conformLifecycle(t, tg) })
			t.Run("failure", func(t *testing.T) { conformFailure(t, tg, false) })
			t.Run("repair_scoped", func(t *testing.T) { conformScopedRepair(t, tg) })
			t.Run("dialect SQL", func(t *testing.T) { conformSpecial(t, tg) })
			t.Run("concurrent runs", func(t *testing.T) { conformConcurrent(t, tg) })
			t.Run("status is read-only", func(t *testing.T) { conformReadOnly(t, tg) })
			t.Run("shipped examples", func(t *testing.T) { conformExamples(t, tg) })
			t.Run("one-connection pool", func(t *testing.T) { conformSingleConn(t, tg) })
			t.Run("session state isolation", func(t *testing.T) { conformSessionState(t, tg) })
			t.Run("baseline on migrate", func(t *testing.T) { conformBaselineOnMigrate(t, tg) })
			t.Run("history identity and rollbacks", func(t *testing.T) { conformIdentity(t, tg) })
			if tg.dialect == dialect.SQLServer {
				t.Run("engine version", conformSQLServerVersion)
				t.Run("application lock identity", conformSQLServerLockIdentity)
				t.Run("nontransactional failure", conformSQLServerDirty)
				t.Run("TLS defaults", conformSQLServerTLS)
			}
			if tg.dialect == dialect.Oracle {
				t.Run("engine version", conformOracleVersion)
				t.Run("session lock across DDL", conformOracleLock)
				t.Run("Go checksums and baseline", conformOracleEmptyChecksum)
			}
			if tg.dialect == dialect.DB2 {
				t.Run("engine version", conformDB2Version)
				t.Run("session lock across commit", conformDB2Lock)
				t.Run("TLS verification", conformDB2TLS)
				t.Run("nontransactional failure", func(t *testing.T) { conformFailure(t, tg, true) })
			}
		})
	}
}

func conformLifecycle(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	m, err := New(db, tg.dialect, WithFS(lifecycle))
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	eq(t, versions(res), []string{"1", "2", "10"})
	if res, err := m.Up(ctx); err != nil || len(res.Applied) != 0 {
		t.Fatalf("idempotent Up = %v, %v", res, err)
	}
	st, err := m.Status(ctx)
	if err != nil || st.Current != "10" || st.Pending != 0 {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	if !st.Migrations[0].InstalledOn.After(time.Now().Add(-time.Hour)) {
		t.Fatalf("installed_on not round-tripped in UTC: %v", st.Migrations[0].InstalledOn)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM users"); n != 1 {
		t.Fatalf("users = %d", n)
	}
	res, err = m.Down(ctx, DownTarget{Steps: 2})
	if err != nil {
		t.Fatalf("Down: %v", err)
	}
	eq(t, versions(res), []string{"10", "2"})
	if res, err = m.Down(ctx, DownTarget{To: "0"}); err != nil {
		t.Fatalf("DownTo 0: %v", err)
	}
	eq(t, versions(res), []string{"1"})
}

func conformFailure(t *testing.T, tg target, noTx bool) {
	ctx := context.Background()
	db := tg.open(t)
	failing := tg.failing
	if noTx {
		failing = "-- +migrate Up notransaction\n" + failing
	}
	fsys := files("1_ok.sql", "CREATE TABLE ok (id INT);", "2_bad.sql", failing)
	m, err := New(db, tg.dialect, WithFS(fsys))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Up(ctx)
	var me *MigrationError
	if !errors.As(err, &me) || me.Version != "2" || me.Statement != 2 {
		t.Fatalf("err = %v", err)
	}
	st, statusErr := m.Status(ctx)
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if noTx || tg.dialect == dialect.MySQL || tg.dialect == dialect.Oracle {
		// No transactional DDL: the first statement stuck, the row is dirty.
		if !errors.Is(err, ErrDirty) || !st.Dirty {
			t.Fatalf("%s failure must be dirty: err=%v status=%+v", tg.dialect, err, st)
		}
		if _, err := db.Exec("DROP TABLE half"); err != nil {
			t.Fatal(err)
		}
		if rr, err := m.Repair(ctx, RepairTarget{Migration: "2", Action: RepairClearFailed}); err != nil || rr.RemovedFailed != 1 {
			t.Fatalf("Repair = %+v, %v", rr, err)
		}
	} else {
		if errors.Is(err, ErrDirty) || st.Dirty || st.Current != "1" {
			t.Fatalf("transactional failure must leave no trace: err=%v status=%+v", err, st)
		}
		if exists, err := m.dialect.TableExists(ctx, db, dialect.Table{Name: "half"}); err != nil || exists {
			t.Fatalf("transactional failure left a partial object: exists=%v err=%v", exists, err)
		}
	}
	if n := count(t, db, "SELECT COUNT(*) FROM "+historyTable(tg)+" WHERE version = '2'"); n != 0 {
		t.Fatalf("failed migration recorded: %d rows", n)
	}
}

func historyTable(tg target) string {
	d, _ := dialect.Lookup(tg.dialect)
	return d.Quote(dialect.Table{Name: DefaultTable})
}

func conformSpecial(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	m, err := New(db, tg.dialect, WithFS(files(flatten(tg.special)...)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if n := count(t, db, tg.specialCheck); n != 2 {
		t.Fatalf("%s = %d, want 2", tg.specialCheck, n)
	}
	if _, err := m.Down(ctx, DownTarget{To: "0"}); err != nil {
		t.Fatalf("DownTo 0: %v", err)
	}
}

func flatten(m map[string]string) []string {
	out := make([]string, 0, 2*len(m))
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}

func conformConcurrent(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	fsys := files(
		"1_counter.sql", "CREATE TABLE counter (n INT);",
		"2_bump.sql", "INSERT INTO counter VALUES (1);",
		"3_bump.sql", "INSERT INTO counter VALUES (2);",
	)
	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := New(db, tg.dialect, WithFS(fsys), WithLockWait(time.Minute))
			if err == nil {
				_, err = m.Up(ctx)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Up: %v", err)
		}
	}
	if n := count(t, db, "SELECT COUNT(*) FROM counter"); n != 2 {
		t.Fatalf("counter rows = %d; each migration must run exactly once", n)
	}

	// A cancelled run must release the lock for the next one.
	cctx, cancel := context.WithCancel(ctx)
	m, _ := New(db, tg.dialect, WithFS(files("4_x.sql", "CREATE TABLE x (id INT);")), WithAllowMissing(true),
		WithHooks(Hooks{Before: func(context.Context, Event) error { cancel(); return nil }}))
	if _, err := m.Up(cctx); err == nil {
		t.Fatal("expected cancellation")
	}
	m2, _ := New(db, tg.dialect, WithFS(fsys), WithLockWait(5*time.Second))
	if _, err := m2.Up(ctx); err != nil {
		t.Fatalf("lock not released after cancellation: %v", err)
	}
}

// conformSingleConn: all locked work runs on the lock's own connection, so a
// pool of one connection must not deadlock (a common migration-pool setup).
func conformSingleConn(t *testing.T, tg target) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := tg.open(t)
	db.SetMaxOpenConns(1)
	m, err := New(db, tg.dialect, WithFS(lifecycle))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up with one connection: %v", err)
	}
	if _, err := m.Redo(ctx); err != nil {
		t.Fatalf("Redo with one connection: %v", err)
	}
	if st, err := m.Status(ctx); err != nil || st.Pending != 0 || st.Current != "10" {
		t.Fatalf("Status = %+v, %v", st, err)
	}
}

// conformSessionState: a migration that switches schema/database must not
// redirect history writes, and its session must not leak into the pool.
func conformSessionState(t *testing.T, tg target) {
	var switchTo, cleanup string
	switch tg.dialect {
	case "postgres":
		switchTo = "CREATE SCHEMA elsewhere_" + "%s;\nSET search_path TO elsewhere_%s;\nCREATE TABLE moved (id INT);"
		cleanup = "DROP SCHEMA IF EXISTS elsewhere_%s CASCADE"
	case "mysql":
		switchTo = "CREATE DATABASE elsewhere_%s;\nUSE elsewhere_%s;\nCREATE TABLE moved (id INT);"
		cleanup = "DROP DATABASE IF EXISTS elsewhere_%s"
	case dialect.SQLServer:
		switchTo = "CREATE DATABASE elsewhere_%s;\nGO\nUSE elsewhere_%s;\nGO\nCREATE TABLE moved (id INT);"
		cleanup = "DROP DATABASE IF EXISTS elsewhere_%s"
	case dialect.Oracle:
		conformOracleSession(t, tg)
		return
	case dialect.DB2:
		conformDB2Session(t, tg)
		return
	default:
		t.Skip("no session-level schema switch on SQLite")
	}
	ctx := context.Background()
	db := tg.open(t)
	suffix := randName("")
	t.Cleanup(func() { _, _ = db.Exec(fmt.Sprintf(cleanup, suffix)) })
	fsys := files(
		"1_switch.sql", "-- +migrate Up notransaction\n"+fmt.Sprintf(switchTo, suffix, suffix),
		"2_after.sql", "CREATE TABLE after_switch (id INT);",
	)
	m, err := New(db, tg.dialect, WithFS(fsys))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	st, err := m.Status(ctx)
	if err != nil || st.Current != "2" || st.Dirty {
		t.Fatalf("history must stay in the original schema: %+v, %v", st, err)
	}
	// A fresh pooled connection must still resolve unqualified names in the
	// original schema (the switched session was discarded, not pooled).
	if n := count(t, db, "SELECT COUNT(*) FROM "+historyTable(tg)); n != 2 {
		t.Fatalf("history rows visible from the pool = %d", n)
	}
}

// conformBaselineOnMigrate: an existing schema is detected through the
// dialect's catalog and baselined before pending migrations run.
func conformBaselineOnMigrate(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	if _, err := db.Exec("CREATE TABLE a (id INT)"); err != nil {
		t.Fatal(err)
	}
	fsys := files("1_a.sql", "CREATE TABLE a (id INT);", "2_b.sql", "CREATE TABLE b (id INT);")
	m, err := New(db, tg.dialect, WithFS(fsys), WithBaselineOnMigrate("1"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	eq(t, versions(res), []string{"2"})
}

// conformIdentity pins history semantics that databases differ on by
// default: keys and tags compare byte for byte as in Go (MySQL's default
// collation folds case and accents), repeated rollbacks succeed although
// MySQL reports unchanged rows as unaffected, and every rollback re-arms
// the repeatable migrations.
func conformIdentity(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	repeat := "CREATE TABLE IF NOT EXISTS %s (id INT);"
	if tg.dialect == dialect.SQLServer {
		repeat = "IF OBJECT_ID(N'%[1]s', N'U') IS NULL CREATE TABLE %[1]s (id INT);"
	}
	if tg.dialect == dialect.DB2 {
		repeat = "CREATE OR REPLACE VIEW %s AS SELECT 1 AS ID FROM SYSIBM.SYSDUMMY1;"
	}
	fsys := files(
		"1_a.sql", "-- +migrate Up\nCREATE TABLE a (id INT);\n-- +migrate Down\nDROP TABLE a;",
		"2_b.sql", "-- +migrate Up\nCREATE TABLE b (id INT);\n-- +migrate Down\nDROP TABLE b;",
		"R__cafe.sql", fmt.Sprintf(repeat, "r_plain"),
		"R__café.sql", fmt.Sprintf(repeat, "r_accent"),
	)
	m, err := New(db, tg.dialect, WithFS(fsys))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.UpTo(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Tag(ctx, "rel"); err != nil {
		t.Fatal(err)
	}
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("Up with accent-distinct repeatables: %v", err)
	}
	eq(t, versions(res), []string{"2", "R:cafe", "R:café"})
	if err := m.Tag(ctx, "Rel"); err != nil { // a different tag: must not move "rel"
		t.Fatal(err)
	}
	if tg.dialect == dialect.SQLServer {
		if err := m.Tag(ctx, "rel "); err != nil { // SQL Server pads strings in comparisons.
			t.Fatal(err)
		}
	}
	if res, err = m.Down(ctx, DownTarget{Tag: "rel"}); err != nil {
		t.Fatalf("Down to tag rel: %v", err)
	}
	eq(t, versions(res), []string{"2"})
	if res, err = m.Down(ctx, DownTarget{Steps: 1}); err != nil {
		t.Fatalf("second consecutive rollback: %v", err)
	}
	eq(t, versions(res), []string{"1"})
	if res, err = m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1", "2", "R:cafe", "R:café"})
}

// conformExamples applies and fully reverts the shipped example for the target.
func conformExamples(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	dir := "examples/migrations"
	if tg.dialect == dialect.SQLServer {
		dir = "examples/sqlserver"
	}
	if tg.dialect == dialect.Oracle {
		dir = "examples/oracle"
	}
	if tg.dialect == dialect.DB2 {
		dir = "examples/db2"
	}
	m, err := New(db, tg.dialect, WithFS(os.DirFS(dir)))
	if err != nil {
		t.Fatal(err)
	}
	if res, err := m.Up(ctx); err != nil || len(res.Applied) != 4 {
		t.Fatalf("Up = %+v, %v", res, err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM user_post_counts"); n != 0 {
		t.Fatalf("view rows = %d", n)
	}
	if res, err := m.Down(ctx, DownTarget{To: "0"}); err != nil || len(res.Applied) != 3 {
		t.Fatalf("DownTo 0 = %+v, %v", res, err)
	}
}

func conformReadOnly(t *testing.T, tg target) {
	ctx := context.Background()
	db := tg.open(t)
	m, err := New(db, tg.dialect, WithFS(lifecycle))
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Status(ctx)
	if err != nil || st.Pending != 3 {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	if plan, err := m.Plan(ctx, "2"); err != nil || len(plan) != 2 {
		t.Fatalf("Plan = %v, %v", plan, err)
	}
	if err := m.Validate(ctx); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	ok, err := m.dialect.TableExists(ctx, db, m.table)
	if err != nil || ok {
		t.Fatalf("read-only calls created the history table: %v, %v", ok, err)
	}
}

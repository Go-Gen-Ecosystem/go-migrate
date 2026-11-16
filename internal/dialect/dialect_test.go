// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLookup(t *testing.T) {
	for in, want := range map[string]string{
		"postgres": Postgres, "PostgreSQL": Postgres, "pgx": Postgres,
		"mysql": MySQL, "mariadb": MySQL, "sqlite": SQLite, "sqlite3": SQLite,
	} {
		d, err := Lookup(in)
		if err != nil || d.Name() != want {
			t.Errorf("Lookup(%q) = %v, %v", in, d, err)
		}
	}
	if _, err := Lookup("oracle"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v", err)
	}
}

func TestParseTableAndQuote(t *testing.T) {
	pg, _ := Lookup(Postgres)
	my, _ := Lookup(MySQL)
	tb, err := ParseTable("app.schema_history")
	if err != nil {
		t.Fatal(err)
	}
	if got := pg.Quote(tb); got != `"app"."schema_history"` {
		t.Errorf("pg quote = %s", got)
	}
	if got := my.Quote(tb); got != "`app`.`schema_history`" {
		t.Errorf("mysql quote = %s", got)
	}
	for _, bad := range []string{"", "a.b.c", "x; DROP TABLE users--", `a"b`, "1abc", strings.Repeat("a", 64)} {
		if _, err := ParseTable(bad); !errors.Is(err, ErrInvalidIdentifier) {
			t.Errorf("ParseTable(%q) err = %v", bad, err)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	pg, _ := Lookup(Postgres)
	my, _ := Lookup(MySQL)
	if got := pg.Placeholders(3); got != "$1, $2, $3" {
		t.Errorf("pg = %s", got)
	}
	if got := my.Placeholders(2); got != "?, ?" {
		t.Errorf("mysql = %s", got)
	}
	if pg.TransactionalDDL() != true || my.TransactionalDDL() != false {
		t.Error("transactional DDL flags wrong")
	}
}

func TestHistoryDDLAndExistsOnSQLite(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	d, _ := Lookup(SQLite)
	tb, _ := ParseTable("schema_history")

	ok, err := d.TableExists(ctx, db, tb)
	if err != nil || ok {
		t.Fatalf("exists before create = %v, %v", ok, err)
	}
	for range 2 { // idempotent
		if _, err := db.ExecContext(ctx, d.HistoryDDL(tb)); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	if ok, err := d.TableExists(ctx, db, tb); err != nil || !ok {
		t.Fatalf("exists after create = %v, %v", ok, err)
	}
}

func TestSQLiteLockMutualExclusion(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	d := dialects[SQLite]
	table := Table{Schema: "main", Name: "schema_history"}

	held, err := d.Lock(ctx, db, table, time.Second)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	for _, wait := range []time.Duration{0, 150 * time.Millisecond} {
		if _, err := d.Lock(ctx, db, table, wait); !errors.Is(err, ErrLockTimeout) {
			t.Fatalf("second lock with wait %s: %v, want ErrLockTimeout", wait, err)
		}
	}
	// A different history table is an independent lock.
	other, err := d.Lock(ctx, db, Table{Name: "other_history"}, 0)
	if err != nil {
		t.Fatalf("independent lock: %v", err)
	}
	if err := other.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := held.Fence(ctx, held.Conn); err != nil {
		t.Fatalf("fence while held: %v", err)
	}
	if err := held.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := held.Release(ctx); err != nil {
		t.Fatalf("second release must be a no-op: %v", err)
	}
	again, err := d.Lock(ctx, db, table, time.Second)
	if err != nil {
		t.Fatalf("relock after release: %v", err)
	}
	_ = again.Release(ctx)
}

func TestSQLiteLockTakesOverExpiredLease(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	if _, err := db.ExecContext(ctx, `CREATE TABLE "h_lock" (id INTEGER PRIMARY KEY CHECK (id = 1), owner TEXT NOT NULL, expires_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO "h_lock" VALUES (1, 'crashed-host', ?)`, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	l, err := dialects[SQLite].Lock(ctx, db, Table{Name: "h"}, time.Second)
	if err != nil {
		t.Fatalf("expected takeover of expired lease: %v", err)
	}
	_ = l.Release(ctx)
}

func TestSQLiteFenceDetectsStolenLease(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	l, err := dialects[SQLite].Lock(ctx, db, Table{Name: "h"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Conn.ExecContext(ctx, `UPDATE "h_lock" SET owner = 'thief'`); err != nil {
		t.Fatal(err)
	}
	if err := l.Fence(ctx, l.Conn); !errors.Is(err, ErrLockLost) {
		t.Fatalf("Fence = %v, want ErrLockLost", err)
	}
	if err := l.Release(ctx); !errors.Is(err, ErrLockLost) {
		t.Fatalf("Release = %v, want ErrLockLost", err)
	}
}

func TestSQLiteWaiterKeepsWaitingWhileDatabaseIsBusy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy.db")
	// No busy timeout: every contended statement returns SQLITE_BUSY at once.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE "h_lock" (id INTEGER PRIMARY KEY CHECK (id = 1), owner TEXT NOT NULL, expires_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	writer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = writer.ExecContext(ctx, "COMMIT")
		_ = writer.Close()
	}()
	l, err := dialects[SQLite].Lock(ctx, db, Table{Name: "h"}, 5*time.Second)
	if err != nil {
		t.Fatalf("waiter must poll through SQLITE_BUSY, got %v", err)
	}
	_ = l.Release(ctx)
}

func TestReleaseFreesConnection(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	l, err := dialects[SQLite].Lock(ctx, db, Table{Name: "h"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.Conn.PingContext(ctx); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("pinned *sql.Conn must be done after release, ping = %v", err)
	}
	discarded := &Lock{Conn: mustConn(t, db), discard: true, unlock: func(context.Context) error { return nil }}
	before := db.Stats().OpenConnections
	if err := discarded.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if after := db.Stats().OpenConnections; after != before-1 {
		t.Fatalf("session-lock connection must be closed, not pooled: open %d -> %d", before, after)
	}
}

func TestLockReleasesAcquisitionCompletedAfterCancellation(t *testing.T) {
	db := openSQLite(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	table := Table{Name: "history"}

	d := *dialects[SQLite]
	d.locker = func(d *Dialect, ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error) {
		held, err := d.sqliteLock(ctx, conn, table, once)
		cancel()
		return held, err
	}
	if _, err := d.Lock(ctx, db, table, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("late acquisition: %v, want context.Canceled", err)
	}
	held, err := dialects[SQLite].Lock(t.Context(), db, table, 0)
	if err != nil {
		t.Fatalf("cancelled acquisition left a lease behind: %v", err)
	}
	if err := held.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func mustConn(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()
	c, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestTableExistsWithSchemaAndCurrentSchema(t *testing.T) {
	ctx := context.Background()
	db := openSQLite(t)
	db.SetMaxOpenConns(1) // ATTACH is per connection
	d, _ := Lookup(SQLite)
	if _, err := db.Exec("ATTACH DATABASE ':memory:' AS aux"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE aux.hist (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.TableExists(ctx, db, Table{Schema: "aux", Name: "hist"}); err != nil || !ok {
		t.Fatalf("aux.hist exists = %v, %v", ok, err)
	}
	if ok, _ := d.TableExists(ctx, db, Table{Name: "hist"}); ok {
		t.Fatal("unqualified lookup must not see aux.hist")
	}
	if s, err := d.CurrentSchema(ctx, db); err != nil || s != "main" {
		t.Fatalf("CurrentSchema = %q, %v", s, err)
	}
	conn := mustConn(t, db)
	if q, err := d.ResolveTable(ctx, conn, Table{Name: "t"}); err != nil || q.String() != "main.t" {
		t.Fatalf("ResolveTable = %s, %v", q, err)
	}
	// SQLite skips the current-schema query even for an unqualified table.
	// Use MySQL to check that an explicitly qualified name skips it too.
	explicit := Table{Schema: "x", Name: "t"}
	if q, err := dialects[MySQL].ResolveTable(ctx, nil, explicit); err != nil || q != explicit {
		t.Fatalf("ResolveTable must keep an explicit schema: %s, %v", q, err)
	}
}

func TestPollHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := poll(ctx, false, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	boom := errors.New("boom")
	if err := poll(context.Background(), false, func(context.Context) (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestLockKeyAndAdvisoryID(t *testing.T) {
	tb, _ := ParseTable("app.hist")
	if got := LockKey(tb); got != "go-migrate:app.hist" {
		t.Errorf("LockKey = %s", got)
	}
	if advisoryID("a") == advisoryID("b") {
		t.Error("distinct keys should hash differently")
	}
	if transient(errors.New("database is locked (5) (SQLITE_BUSY)")) != nil || transient(errors.New("boom")) == nil {
		t.Error("transient must swallow only busy/locked errors")
	}
	if a, b := ownerID(), ownerID(); a == b {
		t.Error("owner IDs must be unique per call")
	}
}

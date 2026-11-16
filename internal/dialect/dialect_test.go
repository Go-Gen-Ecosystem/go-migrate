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
	l := sqliteLocker{}

	_, release, err := l.Lock(ctx, db, "go-migrate:schema_history", time.Second)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, _, err := l.Lock(ctx, db, "go-migrate:schema_history", 150*time.Millisecond); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("second lock err = %v, want ErrLockTimeout", err)
	}
	// A different history table is an independent lock.
	_, releaseOther, err := l.Lock(ctx, db, "go-migrate:other_history", time.Second)
	if err != nil {
		t.Fatalf("independent lock: %v", err)
	}
	if err := releaseOther(ctx); err != nil {
		t.Fatal(err)
	}
	if err := release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := release(ctx); err != nil {
		t.Fatalf("second release must be a no-op: %v", err)
	}
	_, release2, err := l.Lock(ctx, db, "go-migrate:schema_history", time.Second)
	if err != nil {
		t.Fatalf("relock after release: %v", err)
	}
	_ = release2(ctx)
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
	_, release, err := sqliteLocker{}.Lock(ctx, db, "go-migrate:h", time.Second)
	if err != nil {
		t.Fatalf("expected takeover of expired lease: %v", err)
	}
	_ = release(ctx)
}

func TestSQLiteLockLostCancelsContext(t *testing.T) {
	oldTTL, oldHB := leaseTTL, leaseHeartbeat
	leaseTTL, leaseHeartbeat = time.Second, 20*time.Millisecond
	t.Cleanup(func() { leaseTTL, leaseHeartbeat = oldTTL, oldHB })

	ctx := context.Background()
	db := openSQLite(t)
	lockCtx, release, err := sqliteLocker{}.Lock(ctx, db, "go-migrate:h", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// Another process steals the lease.
	if _, err := db.ExecContext(ctx, `UPDATE "h_lock" SET owner = 'thief'`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lockCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("lock context was not cancelled after the lease was lost")
	}
	if err := release(ctx); !errors.Is(err, ErrLockLost) {
		t.Fatalf("release err = %v, want ErrLockLost", err)
	}
}

func TestPollHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := poll(ctx, time.Minute, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	boom := errors.New("boom")
	if err := poll(context.Background(), time.Minute, func(context.Context) (bool, error) { return false, boom }); !errors.Is(err, boom) {
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
	if leaseTableName("go-migrate:app.hist") != "hist_lock" {
		t.Errorf("leaseTableName = %s", leaseTableName("go-migrate:app.hist"))
	}
	if a, b := ownerID(), ownerID(); a == b {
		t.Error("owner IDs must be unique per call")
	}
}

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"
)

func openDB(t testing.TB) *sql.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "app.db") + "?_pragma=busy_timeout(10000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func files(kv ...string) fstest.MapFS {
	fs := fstest.MapFS{}
	for i := 0; i < len(kv); i += 2 {
		fs[kv[i]] = &fstest.MapFile{Data: []byte(kv[i+1])}
	}
	return fs
}

func newM(t testing.TB, db *sql.DB, fsys fstest.MapFS, opts ...Option) *Migrator {
	t.Helper()
	m, err := New(db, "sqlite", append([]Option{WithFS(fsys)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func versions(r *Result) []string {
	var out []string
	for _, a := range r.Applied {
		if a.Repeatable {
			out = append(out, "R:"+a.Description)
		} else {
			out = append(out, a.Version)
		}
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

var basic = files(
	"9_nine.sql", "-- +migrate Up\nCREATE TABLE nine (id INTEGER);\n-- +migrate Down\nDROP TABLE nine;",
	"10_ten.sql", "-- +migrate Up\nCREATE TABLE ten (id INTEGER, v TEXT DEFAULT 'a;b');\n-- +migrate Down\nDROP TABLE ten;",
	"11_eleven.up.sql", "CREATE TABLE eleven (id INTEGER);",
	"11_eleven.down.sql", "DROP TABLE eleven;",
)

func TestUpNumericOrderAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, basic)

	res, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	eq(t, versions(res), []string{"9", "10", "11"})

	res, err = m.Up(ctx)
	if err != nil || len(res.Applied) != 0 {
		t.Fatalf("second Up = %v, %v; want no-op without error", versions(res), err)
	}
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != "11" || st.Pending != 0 || st.Dirty {
		t.Fatalf("status = %+v", st)
	}
	for _, mi := range st.Migrations {
		if mi.State != StateApplied || mi.InstalledOn.IsZero() || mi.InstalledBy == "" {
			t.Fatalf("migration info = %+v", mi)
		}
	}
}

func TestReadOnlyCallsNeverCreateTables(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, basic)

	st, err := m.Status(ctx)
	if err != nil || st.Pending != 3 {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	plan, err := m.Plan(ctx, "")
	if err != nil || len(plan) != 3 {
		t.Fatalf("Plan = %v, %v", plan, err)
	}
	if err := m.Validate(ctx); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if tableExists(t, db, DefaultTable) {
		t.Fatal("read-only calls created the history table")
	}
}

func TestUpToAndDown(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, basic)

	res, err := m.UpTo(ctx, "010")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"9", "10"})
	if _, err := m.UpTo(ctx, "99"); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("UpTo unknown = %v", err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Down(ctx, 4); !errors.Is(err, ErrNotEnoughApplied) {
		t.Fatalf("Down(4) = %v, want ErrNotEnoughApplied", err)
	}
	plan, err := m.PlanDown(ctx, 2)
	if err != nil || len(plan) != 2 || plan[0].Version != "11" {
		t.Fatalf("PlanDown = %v, %v", plan, err)
	}
	res, err = m.Down(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"11", "10"})
	if tableExists(t, db, "ten") || !tableExists(t, db, "nine") {
		t.Fatal("down did not drop exactly the last two tables")
	}
	res, err = m.DownTo(ctx, "0")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"9"})
	if _, err := m.Down(ctx, 0); err == nil {
		t.Fatal("Down(0) must error")
	}
}

func TestIrreversibleCheckedBeforeAnything(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, files(
		"1_a.sql", "-- +migrate Up\nCREATE TABLE a (id INTEGER);\n-- +migrate Down\nDROP TABLE a;",
		"2_b.sql", "CREATE TABLE b (id INTEGER);", // no down
	))
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Down(ctx, 2); !errors.Is(err, ErrIrreversible) {
		t.Fatalf("err = %v, want ErrIrreversible", err)
	}
	if !tableExists(t, db, "a") || !tableExists(t, db, "b") {
		t.Fatal("nothing should have been rolled back")
	}
}

func TestFailedMigrationIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, files(
		"1_ok.sql", "CREATE TABLE ok (id INTEGER);",
		"2_bad.sql", "CREATE TABLE half (id INTEGER);\nINSERT INTO nope VALUES (1);",
		"3_never.sql", "CREATE TABLE never (id INTEGER);",
	))
	res, err := m.Up(ctx)
	var me *MigrationError
	if !errors.As(err, &me) || me.Version != "2" || me.Statement != 2 || me.Dirty {
		t.Fatalf("err = %#v", err)
	}
	eq(t, versions(res), []string{"1"})
	if tableExists(t, db, "half") || tableExists(t, db, "never") {
		t.Fatal("failed migration must roll back completely and stop the run")
	}
	st, _ := m.Status(ctx)
	if st.Dirty || st.Current != "1" || st.Pending != 2 {
		t.Fatalf("status = %+v", st)
	}
}

func TestNoTxFailureIsDirtyUntilRepair(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_notx.sql", "-- +migrate Up notransaction\nCREATE TABLE part (id INTEGER);\nINSERT INTO nope VALUES (1);",
	)
	m := newM(t, db, fsys)
	_, err := m.Up(ctx)
	if !errors.Is(err, ErrDirty) {
		t.Fatalf("err = %v, want ErrDirty", err)
	}
	if !tableExists(t, db, "part") {
		t.Fatal("non-transactional statements before the failure stay applied")
	}
	if _, err := m.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Fatalf("second Up = %v, want ErrDirty", err)
	}
	if st, _ := m.Status(ctx); !st.Dirty || st.Migrations[0].State != StateFailed {
		t.Fatalf("status = %+v", st)
	}

	// Operator fixes the schema and the file, then repairs.
	if _, err := db.Exec("DROP TABLE part"); err != nil {
		t.Fatal(err)
	}
	fsys["1_notx.sql"] = &fstest.MapFile{Data: []byte("-- +migrate Up notransaction\nCREATE TABLE part (id INTEGER);")}
	m = newM(t, db, fsys)
	rr, err := m.Repair(ctx)
	if err != nil || rr.RemovedFailed != 1 {
		t.Fatalf("Repair = %+v, %v", rr, err)
	}
	if res, err := m.Up(ctx); err != nil || len(res.Applied) != 1 {
		t.Fatalf("Up after repair = %v, %v", res, err)
	}
}

func TestChecksumDriftAndRepair(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files("1_a.sql", "CREATE TABLE a (id INTEGER);")
	if _, err := newM(t, db, fsys).Up(ctx); err != nil {
		t.Fatal(err)
	}
	// CRLF-only edits are not drift.
	fsys["1_a.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE a (id INTEGER);\r\n")}
	if err := newM(t, db, fsys).Validate(ctx); err != nil {
		t.Fatalf("line-ending change flagged as drift: %v", err)
	}
	fsys["1_a.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE a (id BIGINT);")}
	fsys["2_b.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE b (id INTEGER);")}
	m := newM(t, db, fsys)
	if _, err := m.Up(ctx); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Up = %v, want ErrChecksumMismatch", err)
	}
	if tableExists(t, db, "b") {
		t.Fatal("drift must block the whole run")
	}
	rr, err := m.Repair(ctx)
	if err != nil || len(rr.Realigned) != 1 {
		t.Fatalf("Repair = %+v, %v", rr, err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("Up after repair: %v", err)
	}
}

func TestMissingAndOutOfOrder(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	if _, err := newM(t, db, files("1_a.sql", "CREATE TABLE a (id INTEGER);", "3_c.sql", "CREATE TABLE c (id INTEGER);")).Up(ctx); err != nil {
		t.Fatal(err)
	}
	const b = "-- +migrate Up\nCREATE TABLE b (id INTEGER);\n-- +migrate Down\nDROP TABLE b;"
	late := files("1_a.sql", "CREATE TABLE a (id INTEGER);", "2_b.sql", b, "3_c.sql", "CREATE TABLE c (id INTEGER);")
	if _, err := newM(t, db, late).Up(ctx); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("Up = %v, want ErrOutOfOrder", err)
	}
	res, err := newM(t, db, late, WithAllowOutOfOrder(true)).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"2"})

	gone := files("1_a.sql", "CREATE TABLE a (id INTEGER);", "2_b.sql", b)
	if err := newM(t, db, gone).Validate(ctx); !errors.Is(err, ErrMissingMigration) {
		t.Fatalf("Validate = %v, want ErrMissingMigration", err)
	}
	if err := newM(t, db, gone, WithAllowMissing(true)).Validate(ctx); err != nil {
		t.Fatalf("Validate with allow-missing: %v", err)
	}
	st, _ := newM(t, db, gone, WithAllowMissing(true)).Status(ctx)
	if last := st.Migrations[len(st.Migrations)-1]; last.State != StateMissing || last.Version != "3" {
		t.Fatalf("missing info = %+v", last)
	}
	// Out-of-order application is rolled back in reverse application order.
	res, err = newM(t, db, late).Down(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"2"})
}

func TestRepeatable(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_t.sql", "CREATE TABLE t (id INTEGER);",
		"R__view.sql", "DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT id FROM t;",
	)
	res, err := newM(t, db, fsys).Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1", "R:view"})
	if res, _ := newM(t, db, fsys).Up(ctx); len(res.Applied) != 0 {
		t.Fatalf("unchanged repeatable re-ran: %v", versions(res))
	}
	fsys["R__view.sql"] = &fstest.MapFile{Data: []byte("DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT id, id AS id2 FROM t;")}
	m := newM(t, db, fsys)
	if st, _ := m.Status(ctx); st.Pending != 1 || st.Migrations[1].State != StateOutdated {
		t.Fatalf("status = %+v", st)
	}
	res, err = m.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"R:view"})
}

func TestGoMigrations(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	seed := GoMigration{
		Version: "2", Description: "seed",
		Up: func(ctx context.Context, ex Executor) error {
			_, err := ex.ExecContext(ctx, "INSERT INTO users (name) VALUES ('ada')")
			return err
		},
		Down: func(ctx context.Context, ex Executor) error {
			_, err := ex.ExecContext(ctx, "DELETE FROM users")
			return err
		},
	}
	fsys := files("1_users.sql", "CREATE TABLE users (name TEXT);")
	m := newM(t, db, fsys, WithGoMigrations(seed))
	if res, err := m.Up(ctx); err != nil || len(res.Applied) != 2 || res.Applied[1].Source != "go" {
		t.Fatalf("Up = %+v, %v", res, err)
	}
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	if n != 1 {
		t.Fatalf("rows = %d", n)
	}
	if _, err := m.Down(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	if n != 0 {
		t.Fatalf("rows after down = %d", n)
	}

	if _, err := New(db, "sqlite", WithFS(fsys), WithGoMigrations(GoMigration{Version: "1", Up: seed.Up})); err == nil {
		t.Fatal("Go migration colliding with a file version must fail New")
	}
	if _, err := New(db, "sqlite", WithGoMigrations(GoMigration{Version: "x"})); err == nil {
		t.Fatal("invalid Go migration must fail New")
	}
}

func TestBaseline(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, basic)
	if err := m.Baseline(ctx, "10", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Baseline(ctx, "10", ""); !errors.Is(err, ErrBaselineNotEmpty) {
		t.Fatalf("second Baseline = %v", err)
	}
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"11"})
	st, _ := m.Status(ctx)
	if st.Migrations[0].State != StateBelowBase || st.Migrations[1].State != StateBaseline {
		t.Fatalf("states = %v, %v", st.Migrations[0].State, st.Migrations[1].State)
	}
	if _, err := m.Down(ctx, 2); !errors.Is(err, ErrNotEnoughApplied) {
		t.Fatalf("baseline must not be rolled back: %v", err)
	}
}

func TestHooks(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	var after []string
	veto := errors.New("veto")
	m := newM(t, db, basic, WithHooks(Hooks{
		Before: func(_ context.Context, e Event) error {
			if e.Migration.Version == "11" {
				return veto
			}
			return nil
		},
		After: func(_ context.Context, e Event) { after = append(after, e.Migration.Version) },
	}))
	if _, err := m.Up(ctx); !errors.Is(err, veto) {
		t.Fatalf("err = %v, want veto", err)
	}
	eq(t, after, []string{"9", "10"})
}

func TestConcurrentUpAppliesEachMigrationOnce(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files(
		"1_counter.sql", "CREATE TABLE counter (n INTEGER);",
		"2_bump.sql", "INSERT INTO counter VALUES (1);",
	)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := New(db, "sqlite", WithFS(fsys), WithLockWait(time.Minute))
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
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM counter").Scan(&n); err != nil || n != 1 {
		t.Fatalf("counter rows = %d, %v; want exactly 1", n, err)
	}
	var locks int
	_ = db.QueryRow(`SELECT COUNT(*) FROM "gomigrate_history_lock"`).Scan(&locks)
	if locks != 0 {
		t.Fatal("lock row left behind")
	}
}

func TestCancelledContextReleasesLock(t *testing.T) {
	db := openDB(t)
	m := newM(t, db, basic)
	ctx, cancel := context.WithCancel(context.Background())
	m2 := newM(t, db, basic, WithHooks(Hooks{Before: func(context.Context, Event) error { cancel(); return nil }}))
	if _, err := m2.Up(ctx); err == nil {
		t.Fatal("expected cancellation error")
	}
	// The lock must be free for the next run.
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up after cancelled run: %v", err)
	}
}

func TestNewErrors(t *testing.T) {
	db := openDB(t)
	if _, err := New(nil, "sqlite"); err == nil {
		t.Error("nil db")
	}
	if _, err := New(db, "oracle"); err == nil {
		t.Error("bad dialect")
	}
	if _, err := New(db, "sqlite", WithTable("x; DROP TABLE y")); err == nil {
		t.Error("bad table")
	}
	if _, err := New(db, "sqlite", WithFS(files("1_a.sql", "SELECT 1;", "001_b.sql", "SELECT 2;"))); err == nil {
		t.Error("duplicate versions")
	}
	m, err := New(db, "sqlite", WithLogger(nil), WithInstalledBy("ci"), WithTable("main_history"))
	if err != nil || m.opts.lockWait != DefaultLockWait || m.opts.installedBy != "ci" || len(m.Migrations()) != 0 {
		t.Fatalf("defaults not applied: %+v, %v", m, err)
	}
}

func TestMigrationErrorMessage(t *testing.T) {
	e := &MigrationError{Version: "3", Direction: Down, Statement: 2, SQL: truncateSQL(string(make([]byte, 500))), Dirty: true, Err: errors.New("boom")}
	msg := e.Error()
	if !errors.Is(e, ErrDirty) || len(msg) > 400 {
		t.Fatalf("message = %q", msg)
	}
}

func TestTransactionControlRejectedInTransactionalMigration(t *testing.T) {
	db := openDB(t)
	_, err := New(db, "sqlite", WithFS(files("1_a.sql", "BEGIN;\nCREATE TABLE a (id INTEGER);\nCOMMIT;")))
	if !errors.Is(err, ErrTransactionControl) {
		t.Fatalf("err = %v, want ErrTransactionControl", err)
	}
	if _, err := New(db, "sqlite", WithFS(files("1_a.sql", "-- +migrate Up notransaction\nBEGIN;\nCREATE TABLE a (id INTEGER);\nCOMMIT;"))); err != nil {
		t.Fatalf("notransaction migrations may manage their own transaction: %v", err)
	}
}

func TestCommentOnlyDownIsIrreversible(t *testing.T) {
	m := newM(t, openDB(t), files("1_a.sql", "-- +migrate Up\nCREATE TABLE a (id INTEGER);\n-- +migrate Down\n-- nothing to undo\n"))
	if m.Migrations()[0].Reversible {
		t.Fatal("a Down section with only comments must not count as reversible")
	}
}

func TestDownToBelowBaselineFails(t *testing.T) {
	ctx := context.Background()
	m := newM(t, openDB(t), basic)
	if err := m.Baseline(ctx, "10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DownTo(ctx, "9"); !errors.Is(err, ErrBelowBaseline) {
		t.Fatalf("DownTo below baseline = %v, want ErrBelowBaseline", err)
	}
	if err := m.Baseline(ctx, "x", ""); !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("Baseline(x) = %v, want ErrInvalidVersion", err)
	}
}

func TestRollbackReappliesRepeatables(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	m := newM(t, db, files(
		"1_t.sql", "-- +migrate Up\nCREATE TABLE t (id INTEGER);\n-- +migrate Down\nDROP VIEW IF EXISTS v;\nDROP TABLE t;",
		"R__view.sql", "DROP VIEW IF EXISTS v;\nCREATE VIEW v AS SELECT id FROM t;",
	))
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DownTo(ctx, "0"); err != nil {
		t.Fatal(err)
	}
	res, err := m.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, versions(res), []string{"1", "R:view"})
	if !tableExists(t, db, "t") {
		t.Fatal("table not recreated")
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM v").Scan(&n); err != nil {
		t.Fatalf("view must exist again after re-applying repeatables: %v", err)
	}
}

func TestMarkAppliedCompletesDirtyRow(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	fsys := files("1_notx.sql", "-- +migrate Up notransaction\nCREATE TABLE part (id INTEGER);\nINSERT INTO nope VALUES (1);")
	m := newM(t, db, fsys)
	if _, err := m.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Fatalf("Up = %v, want ErrDirty", err)
	}
	// The operator finished the migration by hand.
	if err := m.MarkApplied(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkApplied(ctx, "1"); err != nil {
		t.Fatalf("MarkApplied must be idempotent: %v", err)
	}
	st, err := m.Status(ctx)
	if err != nil || st.Dirty || st.Current != "1" || st.Migrations[0].State != StateApplied {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if err := m.MarkApplied(ctx, "42"); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("MarkApplied(unknown) = %v", err)
	}
}

func TestReexportedErrorsAndOptions(t *testing.T) {
	db := openDB(t)
	if _, err := New(db, "sqlite", WithFS(files("1_a.sql", "SELECT 1;", "001_b.sql", "SELECT 2;"))); !errors.Is(err, ErrDuplicateVersion) {
		t.Errorf("duplicate = %v", err)
	}
	if _, err := New(db, "oracle"); !errors.Is(err, ErrUnsupportedDialect) {
		t.Errorf("dialect = %v", err)
	}
	if _, err := New(db, "sqlite", WithTable("a;b")); !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("table = %v", err)
	}
	m, err := New(db, "sqlite", nil, WithLockWait(0))
	if err != nil || m.opts.lockWait != 0 {
		t.Fatalf("WithLockWait(0) must mean no wait: %+v, %v", m, err)
	}
	if _, err := m.PlanDownTo(context.Background(), ""); !errors.Is(err, ErrVersionNotFound) {
		t.Errorf("PlanDownTo empty = %v", err)
	}
	if got := (Migration{Repeatable: true, Description: "views"}).Label(); got != "R__views" {
		t.Errorf("Label = %s", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"testing/fstest"

	"modernc.org/sqlite"
)

func TestMigrationCommitOutcomeUnknown(t *testing.T) {
	for _, dir := range []Direction{Up, Down} {
		for _, native := range []struct {
			name    string
			commits bool
		}{{"commit", true}, {"rollback", false}} {
			t.Run(dir.String()+"/"+native.name, func(t *testing.T) {
				ctx := t.Context()
				dsn := filepath.Join(t.TempDir(), "migration.db")
				f := files(
					"1_first.up.sql", "CREATE TABLE first_step (id INTEGER);",
					"1_first.down.sql", "DROP TABLE first_step;",
					"2_uncertain.up.sql", "CREATE TABLE uncertain_step (id INTEGER);",
					"2_uncertain.down.sql", "DROP TABLE uncertain_step;",
					"3_last.up.sql", "CREATE TABLE last_step (id INTEGER);",
					"3_last.down.sql", "DROP TABLE last_step;",
				)
				if dir == Down {
					seed := openLockTestDB(t, dsn)
					if _, err := newM(t, seed, f).Up(ctx); err != nil {
						t.Fatal(err)
					}
				}
				f["afterMigrate.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE success_callback (id INTEGER);")}
				f["afterMigrateError.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE error_callback (id INTEGER);")}
				fault := &sqliteCommitFault{commits: native.commits}
				m := newM(t, openCommitFaultDB(t, dsn, fault), f, fault.armOn(dir, "2"))
				var res *Result
				var err error
				if dir == Up {
					res, err = m.Up(ctx)
					eq(t, versions(res), []string{"1"})
				} else {
					res, err = m.Down(ctx, DownTarget{To: "0"})
					eq(t, versions(res), []string{"3"})
				}
				assertUnknownCommit(t, fault, err, dir, "2", "commit_outcome_unknown")
				if errors.Is(err, ErrDirty) {
					t.Fatalf("uncertain commit reported as dirty: %v", err)
				}

				// Read both the schema and history from a separate, unwrapped
				// pool on the file to see what actually survived the run.
				observed := openLockTestDB(t, dsn)
				if !tableExists(t, observed, "first_step") || tableExists(t, observed, "last_step") ||
					tableExists(t, observed, "uncertain_step") != (native.commits == (dir == Up)) {
					t.Fatal("schema does not match the native transaction outcome or a later step ran")
				}
				for _, name := range []string{"success_callback", "error_callback"} {
					if tableExists(t, observed, name) {
						t.Fatalf("run-level callback %s ran despite unknown outcome", name)
					}
				}
				fresh := newM(t, observed, f)
				st, err := fresh.Status(ctx)
				wantUncertainState := StatePending
				if native.commits == (dir == Up) {
					wantUncertainState = StateApplied
				}
				if err != nil || st.Dirty || len(st.Migrations) != 3 ||
					st.Migrations[0].State != StateApplied || st.Migrations[2].State != StatePending ||
					st.Migrations[1].State != wantUncertainState {
					t.Fatalf("history after unknown commit = %+v, %v", st, err)
				}
				plan, last := []Migration(nil), "3"
				if dir == Up {
					plan, err = fresh.Plan(ctx, "")
				} else {
					plan, err = fresh.PlanDown(ctx, DownTarget{To: "0"})
					last = "1"
				}
				if err != nil {
					t.Fatal(err)
				}
				pending := make([]string, 0, len(plan))
				for _, mig := range plan {
					pending = append(pending, mig.Version)
				}
				want := []string{last}
				if !native.commits {
					want = []string{"2", last}
				}
				eq(t, pending, want)
			})
		}
	}
}

func TestUnknownCommitPreservesExistingCallbackMarker(t *testing.T) {
	ctx := t.Context()
	dsn := filepath.Join(t.TempDir(), "migration.db")
	f := files(
		"1_change.sql", "CREATE TABLE uncertain_step (id INTEGER);",
		"beforeMigrate.sql", "CREATE TABLE before_callback (id INTEGER);",
		"afterMigrateError.sql", "DROP TABLE before_callback;",
	)
	fault := &sqliteCommitFault{commits: true}
	m := newM(t, openCommitFaultDB(t, dsn, fault), f, fault.armOn(Up, "1"))
	_, err := m.Up(ctx)
	assertUnknownCommit(t, fault, err, Up, "1", "commit_outcome_unknown", "callback_recovery_required", "dirty")
	observed := openLockTestDB(t, dsn)
	fresh := newM(t, observed, f)
	if !tableExists(t, observed, "before_callback") || !tableExists(t, observed, "uncertain_step") {
		t.Fatal("committed work was not preserved or afterMigrateError ran")
	}
	assertCallbackRecovery(t, fresh, "1", 1)
	if _, err = fresh.Plan(ctx, ""); !errors.Is(err, ErrCallbackRecoveryRequired) {
		t.Fatalf("Plan with unresolved callback effects = %v", err)
	}
}

func assertUnknownCommit(t *testing.T, fault *sqliteCommitFault, err error, dir Direction, version string, codes ...string) {
	t.Helper()
	if !fault.fired || !errors.Is(err, io.EOF) {
		t.Fatalf("expected commit fault and original cause, fired = %v, error = %v", fault.fired, err)
	}
	if !errors.Is(err, ErrCommitOutcomeUnknown) {
		t.Fatalf("expected ErrCommitOutcomeUnknown, got %v", err)
	}
	eq(t, ErrorCodes(err), codes)
	var me *MigrationError
	if !errors.As(err, &me) || me.Version != version || me.Direction != dir || me.Statement != 0 || me.Dirty {
		t.Fatalf("unknown commit migration details = %+v", me)
	}
}

// Only the selected migration's transaction receives the fault. SQLite
// still executes its normal queries, DDL, lock protocol, and history writes.
type sqliteCommitFault struct {
	armed, fired, commits bool
}

func (f *sqliteCommitFault) armOn(dir Direction, version string) Option {
	return WithHooks(Hooks{Before: func(_ context.Context, ev Event) error {
		f.armed = ev.Direction == dir && ev.Migration.Version == version
		return nil
	}})
}

func openCommitFaultDB(t *testing.T, dsn string, fault *sqliteCommitFault) *sql.DB {
	t.Helper()
	t.Logf("go-migrate commit outcome fixture: %s (removed after SQL pools close)", dsn)
	db := sql.OpenDB(sqliteCommitConnector{base: new(sqlite.Driver), dsn: dsn, fault: fault})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

type sqliteCommitConnector struct {
	base  driver.Driver
	dsn   string
	fault *sqliteCommitFault
}

func (c sqliteCommitConnector) Driver() driver.Driver { return c.base }

func (c sqliteCommitConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.base.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return sqliteCommitConn{sqliteDriverConn: conn.(sqliteDriverConn), fault: c.fault}, nil
}

//nolint:interfacebloat // The wrapper forwards the native driver's optional database/sql methods.
type sqliteDriverConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

type sqliteCommitConn struct {
	sqliteDriverConn
	fault *sqliteCommitFault
}

func (c sqliteCommitConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.sqliteDriverConn.BeginTx(ctx, opts)
	if err == nil && c.fault.armed {
		c.fault.armed = false
		return sqliteCommitTx{Tx: tx, fault: c.fault}, nil
	}
	return tx, err
}

type sqliteCommitTx struct {
	driver.Tx
	fault *sqliteCommitFault
}

func (tx sqliteCommitTx) Commit() error {
	apply := tx.Rollback
	if tx.fault.commits {
		apply = tx.Tx.Commit
	}
	if err := apply(); err != nil {
		return err
	}
	tx.fault.fired = true
	return io.EOF
}

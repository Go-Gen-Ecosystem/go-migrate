// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	sqliteLockTestDB  = "GOMIGRATE_LOCK_TEST_DB"
	sqliteHolderReady = "SQLite holder ready"
	sqliteHolderCrash = 17
)

func openLockTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteNoTxHolderProcess(t *testing.T) {
	dsn := os.Getenv(sqliteLockTestDB)
	if dsn == "" {
		return
	}
	db := openLockTestDB(t, dsn)
	m := newM(t, db, nil, WithGoMigrations(GoMigration{
		Version: "1", NoTx: true,
		Up: func(ctx context.Context, ex Executor) error {
			conn, ok := ex.(*sql.Conn)
			if !ok {
				return fmt.Errorf("NoTx executor = %T, want *sql.Conn", ex)
			}
			if _, err := conn.ExecContext(ctx, "VACUUM; BEGIN; CREATE TABLE partial (id INTEGER); COMMIT;"); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout, sqliteHolderReady); err != nil {
				return err
			}
			if _, err := bufio.NewReader(os.Stdin).ReadByte(); err == nil {
				os.Exit(sqliteHolderCrash)
			}
			return nil
		},
	}))
	if _, err := m.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteNoTxExcludesLiveProcessAndRecoversCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "test.db")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestSQLiteNoTxHolderProcess$")
	cmd.Env = append(cmd.Environ(), sqliteLockTestDB+"="+path)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Logf("go-migrate SQLite test helper PID %d started at %s for %s", cmd.Process.Pid, started, path)
	ready := make(chan bool, 1)
	done := make(chan error, 1)
	go func() {
		scanned := bufio.NewScanner(stdout)
		for scanned.Scan() {
			if scanned.Text() == sqliteHolderReady {
				ready <- true
			}
		}
		close(ready)
		done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		if done != nil {
			if err := <-done; err != nil {
				t.Errorf("stop SQLite test holder: %v", err)
			}
		}
	})
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("holder exited before migration started")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	db := openLockTestDB(t, path)
	if _, err := db.ExecContext(ctx, "UPDATE main."+DefaultTable+"_lock SET expires_at = 0"); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, db, "partial") {
		t.Fatal("independent reader could not see the holder's committed SQLite transaction")
	}
	other := newM(t, db, nil, WithTable("other_history"), WithLockWait(0))
	if _, err := other.Up(ctx); err != nil {
		t.Fatalf("independent history could not migrate: %v", err)
	}
	aliasDB := openLockTestDB(t, filepath.Join(filepath.Dir(path), "alias.db"))
	aliasDB.SetMaxOpenConns(1) // ATTACH belongs to one connection.
	if _, err := aliasDB.ExecContext(ctx, "ATTACH DATABASE ? AS attached", path); err != nil {
		t.Fatal(err)
	}
	contender := newM(t, aliasDB, nil, WithTable("attached.GOMIGRATE_HISTORY"), WithLockWait(0))
	if _, err := contender.Repair(ctx); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Repair while live holder is past lease expiry = %v, want ErrLockTimeout", err)
	}
	if _, err := stdin.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		done = nil
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != sqliteHolderCrash {
			t.Fatalf("holder exit = %v, want abrupt exit", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := contender.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Fatalf("Up after crashed holder = %v, want ErrDirty", err)
	}
}

func TestSQLiteNoTxExcludesLiveSharedMemoryHolder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	dsn := "file:gomigrate_live_holder?mode=memory&cache=shared"
	owner, peer := openLockTestDB(t, dsn), openLockTestDB(t, dsn)
	entered, resume, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	first := newM(t, owner, nil, WithGoMigrations(GoMigration{
		Version: "1", NoTx: true,
		Up: func(ctx context.Context, ex Executor) error {
			close(entered)
			<-resume
			_, err := ex.ExecContext(ctx, "CREATE TABLE finished (id INTEGER)")
			return err
		},
	}))
	go func() {
		_, err := first.Up(ctx)
		finished <- err
	}()
	defer func() {
		close(resume)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := peer.ExecContext(ctx, "UPDATE main."+DefaultTable+"_lock SET expires_at = 0"); err != nil {
		t.Fatal(err)
	}
	contender := newM(t, peer, nil, WithLockWait(0))
	if _, err := contender.Repair(ctx); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("Repair while shared memory holder is past lease expiry = %v, want ErrLockTimeout", err)
	}
}

func TestSQLiteNoTxAbandonedTransactionRollsBack(t *testing.T) {
	ctx := t.Context()
	db := openDB(t)
	db.SetMaxOpenConns(1)
	m := newM(t, db, files("1_partial.sql", "-- +migrate Up notransaction\nBEGIN; CREATE TABLE partial (id INTEGER);"))
	if result, err := m.Up(ctx); !errors.Is(err, ErrDirty) || len(result.Applied) != 0 {
		t.Fatalf("Up with an open transaction = %+v, %v, want ErrDirty", result, err)
	}
	if tableExists(t, db, "partial") {
		t.Fatal("the connection was pooled with an open migration transaction")
	}
	if status, err := m.Status(ctx); err != nil || !status.Dirty {
		t.Fatalf("Status = %+v, %v, want a committed dirty row", status, err)
	}
}

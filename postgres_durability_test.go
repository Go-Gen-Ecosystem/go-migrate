// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// Exercise the public Up path without a PostgreSQL DSN. The fixture models
// transaction-local values and observes which writes reach the pinned session.
func TestPostgresNoTxHistoryTransitions(t *testing.T) {
	for _, tc := range []struct {
		name, script                 string
		openAtStart, cancelDuringSQL bool
		wantMarker, wantSuccess      bool
	}{
		{name: "preexisting transaction", script: "SELECT 42;", openAtStart: true},
		{name: "unfinished transaction", script: "BEGIN; SELECT 42;", wantMarker: true},
		{name: "completed transaction", script: "BEGIN; SELECT 42; COMMIT;", wantMarker: true, wantSuccess: true},
		{name: "cancel after last statement", script: "SELECT 42;", cancelDuringSQL: true, wantMarker: true, wantSuccess: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := &postgresProbeFixture{inTx: tc.openAtStart, value: "0"}
			if tc.cancelDuringSQL {
				f.cancel = cancel
			}
			db := sql.OpenDB(f)
			t.Cleanup(func() { _ = db.Close() })
			m := newPostgresNoTx(t, db, "1_probe.sql", "-- +migrate Up notransaction\n"+tc.script)
			result, err := m.Up(ctx)
			if (err == nil) != tc.wantSuccess || (len(result.Applied) == 1) != tc.wantSuccess ||
				errors.Is(err, ErrDirty) != (tc.wantMarker && !tc.wantSuccess) {
				t.Errorf("Up = %+v, %v; want success = %t, dirty = %t", result, err, tc.wantSuccess, tc.wantMarker && !tc.wantSuccess)
			}
			if f.marker != tc.wantMarker || f.completed != tc.wantSuccess || f.ranSQL != tc.wantMarker {
				t.Errorf("sent dirty marker = %t, sent history completion = %t, ran migration = %t", f.marker, f.completed, f.ranSQL)
			}
		})
	}
}

type postgresProbeFixture struct {
	driver.Conn                     // No prepared statements or Go-managed transactions in these scripts.
	inTx, marker, completed, ranSQL bool
	value                           string
	cancel                          context.CancelFunc
}

func (f *postgresProbeFixture) Connect(context.Context) (driver.Conn, error) { return f, nil }
func (*postgresProbeFixture) Driver() driver.Driver                          { return nil }
func (*postgresProbeFixture) Close() error                                   { return nil }

func (f *postgresProbeFixture) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	var value driver.Value
	switch {
	case q == "SELECT current_schema()":
		value = "public"
	case strings.HasPrefix(q, "SELECT pg_try_advisory_lock("):
		value = true
	case strings.HasPrefix(q, "SELECT COUNT(*) FROM information_schema.tables"):
		value = int64(0)
	case strings.HasPrefix(q, "SELECT pg_catalog.set_config("):
		sentinel := "0"
		if f.value == sentinel {
			sentinel = "1"
		}
		if f.inTx {
			f.value = sentinel
		}
		value = sentinel
	case strings.HasPrefix(q, "SELECT pg_catalog.current_setting("):
		value = args[1].Value == f.value
	default:
		return nil, fmt.Errorf("unexpected fixture query %q", q)
	}
	return &postgresValueRow{value: value}, nil
}

func (f *postgresProbeFixture) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS "), strings.HasPrefix(q, "SELECT pg_advisory_unlock("):
	case strings.HasPrefix(q, "INSERT INTO "):
		f.marker = true
	case strings.HasPrefix(q, "UPDATE "):
		f.completed = true
	case strings.TrimSuffix(q, ";") == "BEGIN":
		f.inTx = true
	case strings.TrimSuffix(q, ";") == "COMMIT":
		f.inTx = false
	case strings.TrimSuffix(q, ";") == "SELECT 42":
		f.ranSQL = true
		if f.cancel != nil {
			f.cancel()
		}
	default:
		return nil, fmt.Errorf("unexpected fixture statement %q", q)
	}
	return driver.RowsAffected(1), nil
}

type postgresValueRow struct{ value driver.Value }

func (*postgresValueRow) Columns() []string { return []string{"result"} }
func (*postgresValueRow) Close() error      { return nil }
func (r *postgresValueRow) Next(dest []driver.Value) error {
	if r.value == nil {
		return io.EOF
	}
	dest[0], r.value = r.value, nil
	return nil
}

func TestPostgresNoTxConformance(t *testing.T) {
	t.Run("unfinished Up", func(t *testing.T) {
		db := openPostgres(t)
		m := newPostgresNoTx(t, db,
			"1_item.sql", "-- +migrate Up notransaction\nCREATE TABLE kept (n int);\nBEGIN;\nCREATE TABLE abandoned (n int);",
			"afterEachMigrate.sql", "CREATE TABLE callback_output (n int);",
		)
		result, err := m.Up(t.Context())
		checkPostgresDirty(t, m, result, err)
		checkPostgresRelation(t, db, "kept", true)
		checkPostgresRelation(t, db, "abandoned", false)
		checkPostgresRelation(t, db, "callback_output", false)
	})
	t.Run("unfinished Down", func(t *testing.T) {
		db := openPostgres(t)
		m := newPostgresNoTx(t, db,
			"1_item.sql", "-- +migrate Up notransaction\nCREATE TABLE kept (n int);\n"+
				"-- +migrate Down notransaction\nBEGIN;\nDROP TABLE kept;",
		)
		if _, err := m.Up(t.Context()); err != nil {
			t.Fatal(err)
		}
		result, err := m.Down(t.Context(), DownTarget{Steps: 1})
		checkPostgresDirty(t, m, result, err)
		checkPostgresRelation(t, db, "kept", true)
	})
	t.Run("Go migration leaves an aborted transaction", func(t *testing.T) {
		db := openPostgres(t)
		m, err := New(db, "postgres", WithGoMigrations(GoMigration{Version: "1", NoTx: true,
			Up: func(ctx context.Context, ex Executor) error {
				if _, err := ex.ExecContext(ctx, "BEGIN"); err != nil {
					return err
				}
				_, err := ex.ExecContext(ctx, "SELECT 1/0")
				var state interface{ SQLState() string }
				if errors.As(err, &state) && state.SQLState() == "22012" {
					return nil // Swallow the error but leave the transaction aborted.
				}
				return errors.New("fixture did not see a division by zero")
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
		result, err := m.Up(t.Context())
		checkPostgresDirty(t, m, result, err)
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != "25P02" {
			t.Errorf("error = %v; want PostgreSQL's aborted transaction error", err)
		}
	})
	t.Run("Go migration leaves database/sql transaction open", func(t *testing.T) {
		conformPostgresGoNoTx(t, false)
	})
	t.Run("Go migration cancels database/sql transaction", func(t *testing.T) {
		conformPostgresGoNoTx(t, true)
	})
	t.Run("completed explicit transaction and concurrent index", func(t *testing.T) {
		db := openPostgres(t)
		m := newPostgresNoTx(t, db, "1_item.sql", "-- +migrate Up notransaction\n"+
			"BEGIN;\nCREATE TABLE item (n int);\nCOMMIT;\nCREATE INDEX CONCURRENTLY item_n ON item (n);\n"+
			"-- +migrate Down notransaction\nDROP INDEX CONCURRENTLY item_n;\nBEGIN;\nDROP TABLE item;\nCOMMIT;")
		if result, err := m.Up(t.Context()); err != nil || len(result.Applied) != 1 {
			t.Fatalf("Up = %+v, %v; want one applied migration", result, err)
		}
		checkPostgresRelation(t, db, "item", true)
		checkPostgresRelation(t, db, "item_n", true)
		if count(t, db, "SELECT COUNT(*) FROM "+DefaultTable+" WHERE success") != 1 {
			t.Fatal("successful migration did not leave durable history")
		}
		if result, err := m.Down(t.Context(), DownTarget{Steps: 1}); err != nil || len(result.Applied) != 1 {
			t.Fatalf("Down = %+v, %v; want one rolled-back migration", result, err)
		}
		checkPostgresRelation(t, db, "item", false)
		if count(t, db, "SELECT COUNT(*) FROM "+DefaultTable) != 0 {
			t.Fatal("successful rollback did not remove history")
		}
	})
}

func conformPostgresGoNoTx(t *testing.T, cancelRun bool) {
	t.Helper()
	db := openPostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	m, err := New(db, "postgres", WithGoMigrations(GoMigration{Version: "1", NoTx: true,
		Up: func(ctx context.Context, ex Executor) error {
			conn, ok := ex.(*sql.Conn)
			if !ok {
				return errors.New("NoTx executor is not a pinned connection")
			}
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "CREATE TABLE abandoned (n int)"); err != nil {
				return err
			}
			if cancelRun {
				cancel()
			}
			return nil
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Up(ctx)
	checkPostgresDirty(t, m, result, err)
	if cancelRun && !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v; want context.Canceled", err)
	}
	if !cancelRun && ctx.Err() != nil {
		t.Fatal("open database/sql transaction stalled the run until its deadline")
	}
	checkPostgresRelation(t, db, "abandoned", false)
}

func newPostgresNoTx(t *testing.T, db *sql.DB, scripts ...string) *Migrator {
	t.Helper()
	m, err := New(db, "postgres", WithFS(files(scripts...)))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func checkPostgresDirty(t *testing.T, m *Migrator, result *Result, err error) {
	t.Helper()
	if !errors.Is(err, ErrDirty) || len(result.Applied) != 0 {
		t.Errorf("result = %+v, error = %v; want no success and ErrDirty", result, err)
	}
	if status, err := m.Status(t.Context()); err != nil || !status.Dirty {
		t.Errorf("durable status = %+v, error = %v; want dirty history", status, err)
	}
}

func checkPostgresRelation(t *testing.T, db *sql.DB, name string, want bool) {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(t.Context(), "SELECT pg_catalog.to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil || exists != want {
		t.Errorf("relation %s exists = %t, error = %v; want %t", name, exists, err, want)
	}
}

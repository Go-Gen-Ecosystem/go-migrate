// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package history

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

func newStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d, _ := dialect.Lookup(dialect.SQLite)
	tb, _ := dialect.ParseTable("schema_history")
	return New(d, tb), db
}

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	s, db := newStore(t)

	if ok, err := s.Exists(ctx, db); err != nil || ok {
		t.Fatalf("Exists before Ensure = %v, %v", ok, err)
	}
	if err := s.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := s.Ensure(ctx, db); err != nil {
		t.Fatalf("Ensure must be idempotent: %v", err)
	}

	when := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	r1, err := s.Insert(ctx, db, Record{Version: "1", Description: "init", Kind: KindSQL, Script: "1_init.sql",
		Checksum: "abc", InstalledBy: "me", InstalledOn: when, Success: true, ExecutionMS: 5})
	if err != nil || r1 != 1 {
		t.Fatalf("Insert = %d, %v", r1, err)
	}
	r2, err := s.Insert(ctx, db, Record{Version: "2", Description: "two", Kind: KindSQL, Script: "2.sql", Checksum: "def", InstalledBy: "me"})
	if err != nil || r2 != 2 {
		t.Fatalf("Insert = %d, %v", r2, err)
	}
	if _, err := s.Insert(ctx, db, Record{Version: "2", Kind: KindSQL}); err == nil {
		t.Fatal("duplicate version must violate the unique constraint")
	}

	if err := s.Complete(ctx, db, "2", 42); err != nil {
		t.Fatal(err)
	}
	if err := s.Realign(ctx, db, "1", "zzz", KindSQL); err != nil {
		t.Fatal(err)
	}
	rs, err := s.List(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Version != "1" || rs[1].Version != "2" {
		t.Fatalf("List = %+v", rs)
	}
	if !rs[0].InstalledOn.Equal(when) || rs[0].Checksum != "zzz" || !rs[0].Success {
		t.Errorf("row 1 = %+v", rs[0])
	}
	if !rs[1].Success || rs[1].ExecutionMS != 42 || rs[1].InstalledOn.IsZero() {
		t.Errorf("row 2 = %+v", rs[1])
	}

	rep := Record{Version: RepeatablePrefix + "views", Description: "views", Kind: KindRepeatable, Checksum: "c1", Success: true}
	if _, err := s.Insert(ctx, db, rep); err != nil {
		t.Fatal(err)
	}
	rep.Checksum = "c2"
	if err := s.Reapply(ctx, db, rep); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Insert(ctx, db, Record{Version: "3", Kind: KindSQL, Success: false}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.DeleteFailed(ctx, db); err != nil || n != 1 {
		t.Fatalf("DeleteFailed = %d, %v", n, err)
	}
	if err := s.Delete(ctx, db, "2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, db, "2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing = %v, want ErrNotFound", err)
	}
	rs, _ = s.List(ctx, db)
	if len(rs) != 2 || rs[1].Checksum != "c2" {
		t.Fatalf("after deletes = %+v", rs)
	}
}

func TestStoreTransactionRollback(t *testing.T) {
	ctx := context.Background()
	s, db := newStore(t)
	if err := s.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Insert(ctx, tx, Record{Version: "1", Kind: KindSQL, Success: true}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if rs, _ := s.List(ctx, db); len(rs) != 0 {
		t.Fatalf("rolled-back insert persisted: %+v", rs)
	}
}

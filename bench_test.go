// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
)

func manyFiles(n int) fstest.MapFS {
	fs := fstest.MapFS{}
	for i := 1; i <= n; i++ {
		fs[fmt.Sprintf("%06d_step_%d.sql", i, i)] = &fstest.MapFile{Data: []byte(fmt.Sprintf(
			"-- +migrate Up\nCREATE TABLE t%d (id INTEGER PRIMARY KEY, v TEXT DEFAULT 'a;b');\nINSERT INTO t%d VALUES (1, 'x');\n-- +migrate Down\nDROP TABLE t%d;\n", i, i, i))}
	}
	return fs
}

// BenchmarkNew measures loading, parsing, checksumming and ordering 1,000
// migration files.
func BenchmarkNew(b *testing.B) {
	db := openDB(b)
	fsys := manyFiles(1000)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := New(db, "sqlite", WithFS(fsys)); err != nil {
			b.Fatal(err)
		}
	}
}

// A large SQL literal must not cause a file-sized allocation at every stage
// of loading. Measure public New, including the FS read and transaction checks.
func TestNewLargeLiteralAllocationBudget(t *testing.T) {
	const payload = 128 << 10
	data := []byte("-- +goose Up\nINSERT INTO t VALUES ('" + strings.Repeat("x", payload) + "');\n-- +goose Down\nDELETE FROM t;\n")
	files := fstest.MapFS{"001_seed.sql": &fstest.MapFile{Data: data}}
	db := openDB(t)
	var loadErr error
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			if _, loadErr = New(db, "sqlite", WithFS(files)); loadErr != nil {
				return
			}
		}
	})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	// Allow four source-sized buffers plus headroom for metadata and allocator rounding.
	if got, limit := result.AllocedBytesPerOp(), int64(4*len(data)+(32<<10)); got > limit {
		t.Fatalf("New allocated %d bytes for %d bytes of SQL, budget %d", got, len(data), limit)
	}
}

// BenchmarkPlan measures classifying 10,000 migrations against 10,000
// history rows: one map-indexed pass, O(n).
func BenchmarkPlan(b *testing.B) {
	const n = 10000
	gms := make([]GoMigration, n)
	recs := make([]history.Record, n)
	noop := func(context.Context, Executor) error { return nil }
	for i := range gms {
		v := fmt.Sprint(i + 1)
		gms[i] = GoMigration{Version: v, Up: noop}
		recs[i] = history.Record{Rank: int64(i + 1), Version: v, Kind: history.KindGo, Success: true, InstalledOn: time.Now()}
	}
	m, err := New(openDB(b), "sqlite", WithGoMigrations(gms...))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		plan, err := m.plan.up(newSnapshot(recs), "")
		if err != nil || len(plan) != 0 {
			b.Fatalf("plan = %d, %v", len(plan), err)
		}
	}
}

// BenchmarkUp100 applies 100 migrations (200 statements) to a fresh SQLite
// file per iteration: end-to-end cost including locking and history writes.
func BenchmarkUp100(b *testing.B) {
	fsys := manyFiles(100)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		m, err := New(openDB(b), "sqlite", WithFS(fsys))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if res, err := m.Up(ctx); err != nil || len(res.Applied) != 100 {
			b.Fatalf("Up = %v", err)
		}
	}
}

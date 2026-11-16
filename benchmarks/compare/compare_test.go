// Package compare benchmarks go-migrate against pressly/goose and
// golang-migrate applying the same migrations to the same pure-Go SQLite
// database. It is a separate module so competitors never become
// dependencies of go-migrate itself. Run:
//
//	cd benchmarks/compare && go test -run '^$' -bench . -benchmem
//
// SQLite runs with WAL and synchronous=OFF so the numbers measure tool
// overhead (parsing, planning, history bookkeeping, locking), not fsync.
package compare

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	gm "github.com/golang-migrate/migrate/v4"
	gmsqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	migrate "github.com/go-gen-ecosystem/go-migrate"
)

const n = 100

func stmt(i int) string {
	return fmt.Sprintf("CREATE TABLE t%d (id INTEGER PRIMARY KEY, v TEXT DEFAULT 'a;b');\nINSERT INTO t%d VALUES (1, 'x');\n", i, i)
}

// gooseStyle is understood by goose and go-migrate (it reads +goose markers).
func gooseStyle() fstest.MapFS {
	m := fstest.MapFS{}
	for i := 1; i <= n; i++ {
		m[fmt.Sprintf("%05d_t%d.sql", i, i)] = &fstest.MapFile{Data: []byte(
			"-- +goose Up\n" + stmt(i) + "-- +goose Down\n" + fmt.Sprintf("DROP TABLE t%d;\n", i))}
	}
	return m
}

// pairStyle is understood by golang-migrate (and go-migrate).
func pairStyle() fstest.MapFS {
	m := fstest.MapFS{}
	for i := 1; i <= n; i++ {
		m[fmt.Sprintf("%05d_t%d.up.sql", i, i)] = &fstest.MapFile{Data: []byte(stmt(i))}
		m[fmt.Sprintf("%05d_t%d.down.sql", i, i)] = &fstest.MapFile{Data: []byte(fmt.Sprintf("DROP TABLE t%d;\n", i))}
	}
	return m
}

func freshDB(b *testing.B) *sql.DB {
	b.Helper()
	dsn := filepath.Join(b.TempDir(), "bench.db") + "?_pragma=journal_mode(WAL)&_pragma=synchronous(OFF)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func bench(b *testing.B, fsys fs.FS, up func(*sql.DB, fs.FS) error) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		db := freshDB(b)
		b.StartTimer()
		if err := up(db, fsys); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUp100_GoMigrate(b *testing.B) {
	bench(b, gooseStyle(), func(db *sql.DB, fsys fs.FS) error {
		m, err := migrate.New(db, "sqlite", migrate.WithFS(fsys))
		if err != nil {
			return err
		}
		_, err = m.Up(context.Background())
		return err
	})
}

func BenchmarkUp100_Goose(b *testing.B) {
	bench(b, gooseStyle(), func(db *sql.DB, fsys fs.FS) error {
		p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
		if err != nil {
			return err
		}
		_, err = p.Up(context.Background())
		return err
	})
}

func BenchmarkUp100_GolangMigrate(b *testing.B) {
	bench(b, pairStyle(), func(db *sql.DB, fsys fs.FS) error {
		src, err := iofs.New(fsys, ".")
		if err != nil {
			return err
		}
		drv, err := gmsqlite.WithInstance(db, &gmsqlite.Config{})
		if err != nil {
			return err
		}
		m, err := gm.NewWithInstance("iofs", src, "sqlite", drv)
		if err != nil {
			return err
		}
		return m.Up()
	})
}

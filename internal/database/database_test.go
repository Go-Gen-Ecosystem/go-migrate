package database

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestParseDialect(t *testing.T) {
	cases := map[string]Dialect{
		"sqlite": SQLite, "SQLite3": SQLite,
		"postgres": Postgres, "postgresql": Postgres, "pgx": Postgres,
		"mysql": MySQL, "mariadb": MySQL,
	}
	for in, want := range cases {
		got, err := ParseDialect(in)
		if err != nil || got != want {
			t.Errorf("ParseDialect(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseDialect("informix"); !errors.Is(err, ErrUnsupportedDriver) {
		t.Errorf("ParseDialect(informix) err = %v, want ErrUnsupportedDriver", err)
	}
}

func TestTransactionalDDL(t *testing.T) {
	if !SQLite.TransactionalDDL() || !Postgres.TransactionalDDL() || MySQL.TransactionalDDL() {
		t.Fatal("only MySQL lacks transactional DDL")
	}
}

func TestResolveDSN(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		cfg     types.DatabaseConfig
		want    []string // substrings that must appear
		wantErr error
	}{
		{name: "sqlite from database path", dialect: SQLite,
			cfg:  types.DatabaseConfig{Database: "app.db"},
			want: []string{"app.db?_busy_timeout=5000"}},
		{name: "sqlite keeps explicit busy timeout", dialect: SQLite,
			cfg:  types.DatabaseConfig{DSN: "app.db?_busy_timeout=10"},
			want: []string{"app.db?_busy_timeout=10"}},
		{name: "sqlite appends to existing params", dialect: SQLite,
			cfg:  types.DatabaseConfig{DSN: "file:app.db?mode=rwc"},
			want: []string{"file:app.db?mode=rwc&_busy_timeout=5000"}},
		{name: "sqlite needs a database", dialect: SQLite, wantErr: ErrMissingDatabase},
		{name: "postgres from fields escapes credentials", dialect: Postgres,
			cfg: types.DatabaseConfig{Host: "db", Port: 6543, Database: "app",
				Username: "u", Password: "p@ss/word", SSLMode: "disable"},
			want: []string{"postgres://u:p%40ss%2Fword@db:6543/app", "sslmode=disable"}},
		{name: "postgres explicit dsn wins", dialect: Postgres,
			cfg:  types.DatabaseConfig{DSN: "host=x dbname=y", Database: "ignored"},
			want: []string{"host=x dbname=y"}},
		{name: "postgres needs a database", dialect: Postgres, wantErr: ErrMissingDatabase},
		{name: "mysql from fields forces parseTime", dialect: MySQL,
			cfg:  types.DatabaseConfig{Database: "app", Username: "root", Password: "pw", Charset: "utf8mb4"},
			want: []string{"root:pw@tcp(localhost:3306)/app", "parseTime=true", "charset=utf8mb4"}},
		{name: "mysql explicit dsn gets parseTime", dialect: MySQL,
			cfg:  types.DatabaseConfig{DSN: "u:p@tcp(h:1)/d"},
			want: []string{"u:p@tcp(h:1)/d", "parseTime=true"}},
		{name: "mysql needs a database", dialect: MySQL, wantErr: ErrMissingDatabase},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveDSN(tc.dialect, tc.cfg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("DSN %q missing %q", got, w)
				}
			}
		})
	}
}

func TestResolveDSN_InvalidMySQLDSNDoesNotLeak(t *testing.T) {
	_, err := ResolveDSN(MySQL, types.DatabaseConfig{DSN: "secret-password-no-slash"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v; want an error that does not echo the DSN", err)
	}
}

func TestConnect_SQLiteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := Connect(context.Background(), types.DatabaseConfig{Driver: "sqlite", Database: path, MaxConnections: 3})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	if DialectOf(db) != SQLite {
		t.Errorf("DialectOf = %q, want sqlite", DialectOf(db))
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 3 {
		t.Errorf("MaxOpenConnections = %d, want 3", got)
	}
	// A persisted file proves we did not fall back to a temporary database.
	if err := db.Exec("CREATE TABLE t (id INTEGER)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = sqlDB.Close()
	db2, err := Connect(context.Background(), types.DatabaseConfig{Driver: "sqlite", Database: path})
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	sqlDB2, _ := db2.DB()
	defer func() { _ = sqlDB2.Close() }()
	if !db2.Migrator().HasTable("t") {
		t.Error("table not persisted to the sqlite file")
	}
}

func TestConnect_SQLiteMemoryPinsOneConnection(t *testing.T) {
	db, err := Connect(context.Background(), types.DatabaseConfig{Driver: "sqlite", DSN: ":memory:", MaxConnections: 10})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	sqlDB, _ := db.DB()
	defer func() { _ = sqlDB.Close() }()
	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1 for :memory:", got)
	}
}

func TestConnect_Errors(t *testing.T) {
	ctx := context.Background()
	if _, err := Connect(ctx, types.DatabaseConfig{Driver: "informix", DSN: "x"}); !errors.Is(err, ErrUnsupportedDriver) {
		t.Errorf("unsupported driver err = %v", err)
	}
	if _, err := Connect(ctx, types.DatabaseConfig{Driver: "sqlite"}); !errors.Is(err, ErrMissingDatabase) {
		t.Errorf("missing database err = %v", err)
	}
	// Unreachable host: the ping must honour the connect timeout and the
	// error must not contain the password.
	_, err := Connect(ctx, types.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: 1,
		Database: "x", Username: "u", Password: "hunter2", ConnectionTimeout: 2 * time.Second})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks password: %v", err)
	}
}

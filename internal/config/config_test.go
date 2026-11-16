// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package config

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestExpandEnv(t *testing.T) {
	look := env(map[string]string{"PASS": "s3cr$t", "EMPTY": ""})
	tests := []struct{ in, want string }{
		{"dsn: ${PASS}", "dsn: s3cr$t"},
		{"x: ${MISSING:-fallback}", "x: fallback"},
		{"x: ${EMPTY:-def}", "x: def"},
		{"x: ${EMPTY}", "x: "},
		{"literal $dollar and $$ stay", "literal $dollar and $$ stay"},
	}
	for _, tc := range tests {
		got, err := ExpandEnv(tc.in, look)
		if err != nil || got != tc.want {
			t.Errorf("ExpandEnv(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ExpandEnv("${MISSING}", look); err == nil {
		t.Error("unset variable without default must error")
	}
	if _, err := ExpandEnv("${OPEN", look); err == nil {
		t.Error("unterminated must error")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	t.Setenv("GM_TEST_PW", "pw")
	body := "database:\n  driver: postgres\n  name: app\n  password: ${GM_TEST_PW}\nmigrations:\n  dir: db/migrations\n  lock_timeout: 90s\n  allow_out_of_order: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != "pw" || cfg.Migrations.Dir != "db/migrations" ||
		cfg.Migrations.LockTimeout != 90*time.Second || !cfg.Migrations.AllowOutOfOrder {
		t.Fatalf("cfg = %+v", cfg)
	}

	if _, err := Load(filepath.Join(dir, "typo.yaml"), true); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("explicit missing config = %v, want not-exist error", err)
	}
	cfg, err = Load(filepath.Join(dir, "absent.yaml"), false)
	if err != nil || cfg.Migrations.Dir != "migrations" {
		t.Fatalf("implicit missing config = %+v, %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte("database: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, true); err == nil {
		t.Fatal("invalid yaml must error")
	}
}

func TestApplyPrecedence(t *testing.T) {
	cfg := Default()
	cfg.Database.DSN = "from-file"
	cfg.Apply(Overrides{}, false, env(map[string]string{EnvDatabaseURL: "from-database-url"}))
	if cfg.Database.DSN != "from-database-url" {
		t.Fatalf("DATABASE_URL should override file: %s", cfg.Database.DSN)
	}
	cfg.Apply(Overrides{}, false, env(map[string]string{EnvDatabaseURL: "x", EnvDSN: "from-gomigrate-dsn", EnvDriver: "mysql"}))
	if cfg.Database.DSN != "from-gomigrate-dsn" || cfg.Database.Driver != "mysql" {
		t.Fatalf("GOMIGRATE_* should win over DATABASE_URL: %+v", cfg.Database)
	}
	cfg.Apply(Overrides{DSN: "flag", Driver: "sqlite", Dir: "d", Table: "t"}, false, env(map[string]string{EnvDSN: "env"}))
	if cfg.Database.DSN != "flag" || cfg.Database.Driver != "sqlite" || cfg.Migrations.Dir != "d" || cfg.Migrations.Table != "t" {
		t.Fatalf("flags should win: %+v", cfg)
	}
}

func TestApplyDriverFollowsOverridingDSN(t *testing.T) {
	cfg := Default()
	cfg.Database.Driver, cfg.Database.DSN = "sqlite", "app.db" // as written by `gomigrate init`
	cfg.Apply(Overrides{}, false, env(map[string]string{EnvDatabaseURL: "postgres://u:p@db/prod"}))
	if cfg.Database.Driver != "postgres" {
		t.Fatalf("driver = %s; a postgres:// DATABASE_URL must not be opened with the file's sqlite driver", cfg.Database.Driver)
	}
	cfg = Default()
	cfg.Apply(Overrides{DSN: "postgres://x/y", Driver: "pgx"}, false, env(nil))
	if cfg.Database.Driver != "pgx" {
		t.Fatalf("a driver given at the same level as the DSN must be kept: %s", cfg.Database.Driver)
	}
}

func TestApplyExplicitConfigIgnoresDatabaseURL(t *testing.T) {
	cfg := Default()
	cfg.Database.DSN = "prod-from-file"
	cfg.Apply(Overrides{}, true, env(map[string]string{EnvDatabaseURL: "postgres://ambient/dev"}))
	if cfg.Database.DSN != "prod-from-file" {
		t.Fatalf("explicit --config must not be retargeted by DATABASE_URL: %s", cfg.Database.DSN)
	}
}

func TestLoadExpandsAfterParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	t.Setenv("GM_ODD_PW", "*abc: x #y")
	body := `# ${GM_NOT_SET} in a comment is fine
database:
  password: ${GM_ODD_PW}
  params:
    application_name: ${GM_APP:-gomigrate}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != "*abc: x #y" || cfg.Database.Params["application_name"] != "gomigrate" {
		t.Fatalf("cfg = %+v", cfg.Database)
	}
}

func TestPostgresDSNMergesParams(t *testing.T) {
	_, dsn, err := Resolve(Database{Driver: "postgres", DSN: "postgres://u@h/db?sslmode=require", SSLMode: "disable",
		Params: map[string]string{"application_name": "gm"}})
	if err != nil || !strings.Contains(dsn, "sslmode=require") || !strings.Contains(dsn, "application_name=gm") {
		t.Fatalf("URL DSN = %s, %v (explicit DSN values win, missing params are added)", dsn, err)
	}
	_, dsn, err = Resolve(Database{Driver: "postgres", DSN: "host=h dbname=d", SSLMode: "disable"})
	if err != nil || dsn != "host=h dbname=d sslmode=disable" {
		t.Fatalf("keyword DSN = %q, %v", dsn, err)
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name     string
		db       Database
		dialect  string
		contains []string
		err      bool
	}{
		{name: "infer postgres url", db: Database{DSN: "postgres://u:p@h/db"}, dialect: "postgres", contains: []string{"postgres://u:p@h/db"}},
		{name: "postgres fields escaped", db: Database{Driver: "postgresql", Host: "h", Name: "app", User: "u", Password: "p@ss/w", SSLMode: "disable"},
			dialect: "postgres", contains: []string{"postgres://u:p%40ss%2Fw@h:5432/app", "sslmode=disable"}},
		{name: "mysql url converted", db: Database{DSN: "mysql://root:pw@db:3307/app?charset=utf8mb4"},
			dialect: "mysql", contains: []string{"root:pw@tcp(db:3307)/app", "parseTime=true", "charset=utf8mb4"}},
		{name: "mysql url default port", db: Database{DSN: "mysql://root@db/app"}, dialect: "mysql", contains: []string{"tcp(db:3306)/app"}},
		{name: "mysql native dsn", db: Database{Driver: "mariadb", DSN: "u:p@tcp(h:1)/d"}, dialect: "mysql", contains: []string{"u:p@tcp(h:1)/d", "parseTime=true"}},
		{name: "mysql fields", db: Database{Driver: "mysql", Name: "app", User: "root"}, dialect: "mysql", contains: []string{"root@tcp(localhost:3306)/app"}},
		{name: "sqlite path", db: Database{DSN: "app.db"}, dialect: "sqlite", contains: []string{"app.db?_pragma=busy_timeout(10000)&_txlock=immediate"}},
		{name: "sqlite url", db: Database{DSN: "sqlite://data/app.sqlite"}, dialect: "sqlite", contains: []string{"data/app.sqlite?"}},
		{name: "sqlite name field", db: Database{Driver: "sqlite3", Name: "x.db"}, dialect: "sqlite", contains: []string{"x.db?"}},
		{name: "nothing configured", db: Database{}, err: true},
		{name: "cannot infer", db: Database{DSN: "host=x"}, err: true},
		{name: "unknown driver", db: Database{Driver: "oracle", DSN: "x"}, err: true},
		{name: "bad mysql dsn", db: Database{Driver: "mysql", DSN: "no-slash"}, err: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, dsn, err := Resolve(tc.db)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error, got %s %s", d, dsn)
				}
				return
			}
			if err != nil || d != tc.dialect {
				t.Fatalf("Resolve = %s, %v", d, err)
			}
			for _, c := range tc.contains {
				if !strings.Contains(dsn, c) {
					t.Errorf("dsn %q missing %q", dsn, c)
				}
			}
		})
	}
}

func TestOpenSQLiteAndErrors(t *testing.T) {
	ctx := context.Background()
	db, name, err := Open(ctx, Database{DSN: filepath.Join(t.TempDir(), "a.db")})
	if err != nil || name != "sqlite" {
		t.Fatalf("Open = %s, %v", name, err)
	}
	_ = db.Close()

	mem, _, err := Open(ctx, Database{Driver: "sqlite", DSN: ":memory:", MaxOpenConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	if got := mem.Stats().MaxOpenConnections; got != 1 {
		t.Errorf(":memory: MaxOpenConnections = %d, want 1", got)
	}
	_ = mem.Close()

	_, _, err = Open(ctx, Database{Driver: "postgres", Host: "127.0.0.1", Port: 1, Name: "x", User: "u",
		Password: "hunter2", ConnectTimeout: 2 * time.Second})
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("err = %v; want a connection error without the password", err)
	}
}

func TestScrub(t *testing.T) {
	if got := scrub(errors.New("bad pw hunter2"), "hunter2").Error(); got != "bad pw ****" {
		t.Errorf("scrub = %q", got)
	}
	base := errors.New("x")
	if scrub(base, "") != base {
		t.Error("no password: error must be returned as is")
	}
}

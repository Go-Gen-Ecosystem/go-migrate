package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

// run executes gomigrate with args in the current directory.
func run(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := newRoot("test", &errOut, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("output missing %q:\n%s", sub, s)
		}
	}
}

func TestEndToEnd(t *testing.T) {
	t.Chdir(t.TempDir())
	none := map[string]string{}

	out, err := run(t, none, "init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	mustContain(t, out, config.DefaultPath, "migrations/")
	if fi, err := os.Stat(config.DefaultPath); err != nil || fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("config not created owner-only: %v", err)
	}
	if _, err := run(t, none, "init"); err == nil {
		t.Fatal("second init must refuse to overwrite the config")
	}

	if out, err = run(t, none, "create", "Create Users!", "--seq"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "0001_create_users.sql")
	if out, err = run(t, none, "create", "add_posts", "--seq", "--pair"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "0002_add_posts.up.sql", "0002_add_posts.down.sql")
	write(t, "migrations/0001_create_users.sql", "-- +migrate Up\nCREATE TABLE users (id INTEGER PRIMARY KEY);\n-- +migrate Down\nDROP TABLE users;\n")
	write(t, "migrations/0002_add_posts.up.sql", "CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));\n")
	write(t, "migrations/0002_add_posts.down.sql", "DROP TABLE posts;\n")
	write(t, "migrations/0003_seed.sql", "-- +migrate Up\nINSERT INTO users (id) VALUES (1);\n-- +migrate Down\nDELETE FROM users;\n")

	if out, err = run(t, none, "up", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Would apply 3 migration(s)", "0002_add_posts.up.sql")
	if _, err = run(t, none, "status", "--fail-on-pending"); !errors.Is(err, ErrPending) {
		t.Fatalf("status --fail-on-pending = %v, want ErrPending", err)
	}

	if out, err = run(t, none, "up", "--to", "2"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Applied 2 migration(s)")
	if out, err = run(t, none, "up"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Applied 3 ", "Applied 1 migration(s)")
	if out, err = run(t, none, "up"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Nothing to do.")

	if out, err = run(t, none, "status", "--json"); err != nil {
		t.Fatal(err)
	}
	var st migrate.Status
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("status --json is not JSON: %v\n%s", err, out)
	}
	if st.Current != "3" || st.Pending != 0 || len(st.Migrations) != 3 {
		t.Fatalf("status = %+v", st)
	}
	if out, err = run(t, none, "status"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "VERSION", "applied", "Current version: 3 | Pending: 0 | Dirty: false")

	if out, err = run(t, none, "down", "--steps", "2", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Would roll back 2 migration(s)")
	if out, err = run(t, none, "down", "--steps", "2"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Rolled back 3 ", "Rolled back 2 ", "Rolled back 2 migration(s)")
	if _, err = run(t, none, "down", "--steps", "5"); !errors.Is(err, migrate.ErrNotEnoughApplied) {
		t.Fatalf("down --steps 5 = %v", err)
	}
	if _, err = run(t, none, "down", "--steps", "1", "--all"); err == nil {
		t.Fatal("--steps with --all must be rejected")
	}

	if out, err = run(t, none, "redo"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Redid 1 ", "Redid 1 migration(s).")
	if out, err = run(t, none, "validate"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "OK")
	if out, err = run(t, none, "repair"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Removed 0 failed row(s)")
	if out, err = run(t, none, "down", "--all"); err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Rolled back 1 migration(s)")
}

func TestDSNFromEnvironmentWithoutConfigFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_a.sql", "CREATE TABLE a (id INTEGER);")
	env := map[string]string{config.EnvDSN: filepath.Join(".", "env.db")}
	if out, err := run(t, env, "up"); err != nil || !strings.Contains(out, "Applied 1 migration(s)") {
		t.Fatalf("up = %q, %v", out, err)
	}
	if out, err := run(t, env, "baseline", "--version", "5"); err == nil {
		t.Fatalf("baseline on non-empty history must fail: %s", out)
	}
	if _, err := os.Stat("env.db"); err != nil {
		t.Fatal("database file not created at the env DSN")
	}
}

func TestBaselineCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_a.sql", "CREATE TABLE a (id INTEGER);")
	write(t, "migrations/2_b.sql", "CREATE TABLE b (id INTEGER);")
	if out, err := run(t, nil, "--dsn", "legacy.db", "baseline", "--version", "1"); err != nil || !strings.Contains(out, "Baselined at version 1") {
		t.Fatalf("baseline = %q, %v", out, err)
	}
	if out, err := run(t, nil, "--dsn", "legacy.db", "up"); err != nil || !strings.Contains(out, "Applied 1 migration(s)") {
		t.Fatalf("up after baseline = %q, %v", out, err)
	}
	if _, err := run(t, nil, "--dsn", "legacy.db", "baseline"); err == nil {
		t.Fatal("baseline without --version must fail")
	}
}

func TestErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := run(t, nil, "--config", "typo.yaml", "status"); err == nil {
		t.Fatal("explicit missing config must fail")
	}
	if _, err := run(t, nil, "--dsn", "x.db", "status"); err == nil || !strings.Contains(err.Error(), "migrations directory") {
		t.Fatalf("missing migrations dir = %v", err)
	}
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, nil, "status"); !errors.Is(err, config.ErrNoDatabase) {
		t.Fatalf("no database = %v", err)
	}
	if _, err := run(t, nil, "--dsn", "x.db", "--log-level", "loud", "status"); err == nil {
		t.Fatal("invalid log level must fail")
	}
	if _, err := run(t, nil, "create", "!!!"); err == nil {
		t.Fatal("empty sanitized name must fail")
	}
	write(t, "migrations/bad name.sql", "SELECT 1;")
	if _, err := run(t, nil, "--dsn", "x.db", "status"); err == nil {
		t.Fatal("malformed migration file name must fail")
	}
}

func TestReviewFixes(t *testing.T) {
	t.Chdir(t.TempDir())
	none := map[string]string{}

	// init quotes values, so a DSN with YAML-significant characters survives.
	if _, err := run(t, none, "init", "--init-dsn", "odd #name: *x.db"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.DefaultPath, true)
	if err != nil || cfg.Database.DSN != "odd #name: *x.db" {
		t.Fatalf("round-tripped DSN = %q, %v", cfg.Database.DSN, err)
	}
	if err := os.Remove(config.DefaultPath); err != nil {
		t.Fatal(err)
	}

	// Two timestamped creates in the same second get distinct versions.
	for range 2 {
		if _, err := run(t, none, "--dsn", "x.db", "create", "same"); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir("migrations")
	if len(entries) != 2 || entries[0].Name()[:14] == entries[1].Name()[:14] {
		t.Fatalf("timestamp collision: %v", entries)
	}
	for _, e := range entries {
		write(t, filepath.Join("migrations", e.Name()), "-- +migrate Up\nCREATE TABLE t_"+e.Name()[:14]+" (id INTEGER);\n-- +migrate Down\nDROP TABLE t_"+e.Name()[:14]+";")
	}

	if _, err := run(t, none, "--dsn", "x.db", "up"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, none, "--dsn", "x.db", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, `"current":`, `"installed_on":`, `"execution_ms":`, `"state": "applied"`)
	if strings.Contains(out, "InstalledOn") || strings.Contains(out, "ExecutionTime") {
		t.Fatalf("status --json must use the snake_case contract:\n%s", out)
	}

	if _, err := run(t, none, "--dsn", "x.db", "down", "--to", "0", "--all"); err == nil {
		t.Fatal("--to with --all must be rejected")
	}
	out, err = run(t, none, "--dsn", "x.db", "mark-applied", entries[0].Name()[:14])
	if err != nil || !strings.Contains(out, "Marked") {
		t.Fatalf("mark-applied = %q, %v", out, err)
	}
}

func TestFailedRunDoesNotSayNothingToDo(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_bad.sql", "INSERT INTO missing VALUES (1);")
	out, err := run(t, nil, "--dsn", "x.db", "up")
	if err == nil || strings.Contains(out, "Nothing to do") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

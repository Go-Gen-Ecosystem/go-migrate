// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"testing"

	migrate "github.com/go-gen-ecosystem/go-migrate"
)

type testEnvelope struct {
	OK      bool            `json:"ok"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code      string   `json:"code"`
		Codes     []string `json:"codes"`
		Message   string   `json:"message"`
		Migration *struct {
			Version   string `json:"version"`
			Direction string `json:"direction"`
			Statement int    `json:"statement"`
			Dirty     bool   `json:"dirty"`
		} `json:"migration"`
	} `json:"error"`
}

func decode(t *testing.T, out string) testEnvelope {
	t.Helper()
	var e testEnvelope
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("output is not one JSON document: %v\n%s", err, out)
	}
	return e
}

func TestUnknownCommitJSONError(t *testing.T) {
	err := &migrate.MigrationError{
		Version: "2", Direction: migrate.Up,
		Err: fmt.Errorf("%w: %w", migrate.ErrCommitOutcomeUnknown, io.EOF),
	}
	result := &migrate.Result{Direction: migrate.Up, Applied: []migrate.Applied{{Migration: migrate.Migration{Version: "1"}}}}
	out, marshalErr := json.Marshal(errEnvelope("up", withData(err, newRun(result))))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	e := decode(t, string(out))
	if e.OK || e.Error == nil || e.Error.Code != "commit_outcome_unknown" ||
		!slices.Equal(e.Error.Codes, []string{"commit_outcome_unknown"}) {
		t.Fatalf("unknown commit envelope = %s", out)
	}
	m := e.Error.Migration
	if m == nil || m.Version != "2" || m.Direction != "up" || m.Statement != 0 || m.Dirty {
		t.Fatalf("unknown commit migration = %s", out)
	}
	mustContain(t, e.Error.Message, "inspect the schema and migration history", "generate a fresh plan", io.EOF.Error())
	var run runJSON
	if err := json.Unmarshal(e.Data, &run); err != nil || len(run.Applied) != 1 || run.Applied[0].Version != "1" {
		t.Fatalf("unknown commit partial result = %s (%v)", out, err)
	}
}

func TestRepairCommandRequiresOneTargetAndAction(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_a.sql", "CREATE TABLE a (id INT);")
	write(t, "migrations/2_bad.sql", "-- +migrate Up notransaction\nINSERT INTO missing VALUES (1);")
	env := map[string]string{"GOMIGRATE_DSN": "app.db"}
	if out, code := runCLI(t, env, "-o", "json", "up"); code != ExitError {
		t.Fatalf("seed failed migration = %d %s", code, out)
	} else if e := decode(t, out); e.Error == nil || e.Error.Migration == nil || e.Error.Migration.Version != "2" || !e.Error.Migration.Dirty {
		t.Fatalf("seed failed migration = %s", out)
	}
	write(t, "migrations/1_a.sql", "CREATE TABLE a (id BIGINT);")
	for _, args := range [][]string{
		{"repair"},
		{"repair", "2"},
		{"repair", "--clear-failed"},
		{"repair", "1", "2", "--accept-checksum"},
		{"repair", "2", "--clear-failed", "--accept-checksum"},
	} {
		out, code := runCLI(t, env, append([]string{"-o", "json"}, args...)...)
		e := decode(t, out)
		if code != ExitError || e.Error == nil || e.Error.Code != "invalid_argument" {
			t.Fatalf("%v = %d %s", args, code, out)
		}
	}
	out, code := runCLI(t, env, "-o", "json", "repair", "0001", "--accept-checksum", "--dry-run")
	var preview struct {
		DryRun         bool   `json:"dry_run"`
		Migration      string `json:"migration"`
		State          string `json:"state"`
		StoredChecksum string `json:"stored_checksum"`
		NewChecksum    string `json:"new_checksum"`
	}
	if err := json.Unmarshal(decode(t, out).Data, &preview); code != ExitOK || err != nil || !preview.DryRun || preview.Migration != "1" ||
		preview.State != "modified" || preview.StoredChecksum == "" || preview.NewChecksum == "" || preview.StoredChecksum == preview.NewChecksum {
		t.Fatalf("repair dry run = %d %s (%v)", code, out, err)
	}
	out, code = runCLI(t, env, "repair", "2", "--clear-failed", "--dry-run")
	if code != ExitOK {
		t.Fatalf("clear dry run = %d %s", code, out)
	}
	mustContain(t, out, "Would remove history row for 2 (state: failed)")
	out, code = runCLI(t, env, "status")
	if code != ExitOK {
		t.Fatalf("status after previews = %d %s", code, out)
	}
	mustContain(t, out, "modified", "failed", "Dirty: true")
	if out, code = runCLI(t, env, "repair", "2", "--clear-failed"); code != ExitOK {
		t.Fatalf("clear failed = %d %s", code, out)
	}
	mustContain(t, out, "Removed failed history row for 2")
	if out, code = runCLI(t, env, "status"); code != ExitOK {
		t.Fatalf("status after clear = %d %s", code, out)
	}
	mustContain(t, out, "modified", "pending", "Dirty: false")
	if out, code = runCLI(t, env, "repair", "1", "--accept-checksum"); code != ExitOK {
		t.Fatalf("accept checksum = %d %s", code, out)
	}
	mustContain(t, out, "Accepted checksum drift for version 1")
}

func TestJSONOutputContract(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_a.sql", "-- +migrate Up\nCREATE TABLE ${t} (id INTEGER);\n-- +migrate Down\nDROP TABLE ${t};")
	write(t, "migrations/2_b.sql", "-- +migrate Context test\nCREATE TABLE b (id INTEGER);")
	env := map[string]string{"GOMIGRATE_DSN": "app.db"}
	base := []string{"-o", "json", "--placeholder", "t=accounts"}

	out, code := runCLI(t, env, append(base, "up", "--sql")...)
	e := decode(t, out)
	if code != ExitOK || !e.OK || e.Command != "up" {
		t.Fatalf("up --sql = %d %+v", code, e)
	}
	var plan struct {
		DryRun     bool `json:"dry_run"`
		Migrations []struct {
			Version    string   `json:"version"`
			Statements []string `json:"statements"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal(e.Data, &plan); err != nil || !plan.DryRun || len(plan.Migrations) != 2 ||
		plan.Migrations[0].Statements[0] != "CREATE TABLE accounts (id INTEGER)" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}

	out, code = runCLI(t, env, append(base, "--context", "prod", "up")...)
	e = decode(t, out)
	var res struct {
		Applied []struct {
			Version    string `json:"version"`
			DurationMS *int64 `json:"duration_ms"`
		} `json:"applied"`
	}
	if err := json.Unmarshal(e.Data, &res); code != ExitOK || err != nil || len(res.Applied) != 1 || res.Applied[0].DurationMS == nil {
		t.Fatalf("up = %d %s", code, out)
	}

	out, code = runCLI(t, env, append(base, "tag", "v1")...)
	if e = decode(t, out); code != ExitOK || !e.OK {
		t.Fatalf("tag = %d %s", code, out)
	}

	// --fail-on-pending: status document on stdout, exit code 2, no error document.
	out, code = runCLI(t, env, append(base, "status", "--fail-on-pending")...)
	if e = decode(t, out); code != ExitPending || !e.OK {
		t.Fatalf("status --fail-on-pending = %d %s", code, out)
	}

	// Errors are a JSON document with stable codes.
	out, code = runCLI(t, env, append(base, "down", "--to-tag", "nope")...)
	e = decode(t, out)
	if code != ExitError || e.OK || e.Error == nil || e.Error.Code != "tag_not_found" {
		t.Fatalf("down --to-tag nope = %d %s", code, out)
	}
	write(t, "migrations/3_ok.sql", "CREATE TABLE okay (id INTEGER);")
	write(t, "migrations/4_bad.sql", "CREATE TABLE ok (id INTEGER);\nINSERT INTO missing VALUES (1);")
	out, code = runCLI(t, env, append(base, "--context", "prod", "up")...)
	e = decode(t, out)
	if code != ExitError || e.Error == nil || e.Error.Code != "migration_failed" || e.Error.Migration == nil ||
		e.Error.Migration.Version != "4" || e.Error.Migration.Statement != 2 {
		t.Fatalf("failing migration = %d %s", code, out)
	}
	// The error envelope still reports what completed before the failure.
	var partial struct {
		Applied []struct {
			Version string `json:"version"`
		} `json:"applied"`
	}
	if err := json.Unmarshal(e.Data, &partial); err != nil || len(partial.Applied) != 1 || partial.Applied[0].Version != "3" {
		t.Fatalf("partial run data = %s, %v", e.Data, err)
	}
}

func TestTextErrorsGoToStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	out, code := runCLI(t, nil, "--config", "missing.yaml", "status")
	if code != ExitError || out != "" {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
	if _, code := runCLI(t, nil, "-o", "yaml", "--dsn", "x.db", "status"); code != ExitError {
		t.Fatal("invalid --output must fail")
	}
}

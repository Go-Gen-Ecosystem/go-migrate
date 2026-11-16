// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"encoding/json"
	"os"
	"testing"
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
	write(t, "migrations/3_bad.sql", "CREATE TABLE ok (id INTEGER);\nINSERT INTO missing VALUES (1);")
	out, code = runCLI(t, env, append(base, "--context", "prod", "up")...)
	e = decode(t, out)
	if code != ExitError || e.Error == nil || e.Error.Code != "migration_failed" || e.Error.Migration == nil ||
		e.Error.Migration.Version != "3" || e.Error.Migration.Statement != 2 {
		t.Fatalf("failing migration = %d %s", code, out)
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

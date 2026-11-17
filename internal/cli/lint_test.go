// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLintCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_users.sql", "-- +migrate Up\nCREATE TABLE users (id INT);\n-- +migrate Down\nDROP TABLE users;")
	write(t, "migrations/2_index.sql", "-- +migrate Up\nCREATE INDEX users_id ON users (id);\n-- +migrate Down\nDROP INDEX users_id;")
	pg := []string{"--driver", "postgres"} // no DSN: lint never connects

	out, code := runCLI(t, nil, append(pg, "lint")...)
	if code != ExitOK || !strings.Contains(out, "index-not-concurrent") || !strings.Contains(out, "hint:") {
		t.Fatalf("text lint = %d\n%s", code, out)
	}
	if _, code = runCLI(t, nil, append(pg, "lint", "--fail-on", "warning")...); code != ExitLint {
		t.Fatalf("--fail-on warning exit = %d, want %d", code, ExitLint)
	}

	out, code = runCLI(t, nil, append(pg, "-o", "json", "lint", "--fail-on", "warning")...)
	e := decode(t, out)
	var data struct {
		Findings []struct {
			Rule     string `json:"rule"`
			Severity string `json:"severity"`
			Source   string `json:"source"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(e.Data, &data); code != ExitLint || !e.OK || err != nil || len(data.Findings) == 0 {
		t.Fatalf("json lint = %d\n%s", code, out)
	}

	out, code = runCLI(t, nil, append(pg, "-o", "github", "lint")...)
	if code != ExitOK || !strings.Contains(out, "::warning file=migrations/2_index.sql,title=gomigrate index-not-concurrent::") {
		t.Fatalf("github lint = %d\n%s", code, out)
	}

	write(t, "migrations/3_bad.sql", "ALTER TABLE users ADD COLUMN tier text NOT NULL;")
	if _, code = runCLI(t, nil, append(pg, "lint")...); code != ExitLint {
		t.Fatalf("error finding must fail by default, exit = %d", code)
	}
	if _, code = runCLI(t, nil, append(pg, "lint", "--fail-on", "never")...); code != ExitOK {
		t.Fatalf("--fail-on never exit = %d", code)
	}
	if _, code = runCLI(t, nil, append(pg, "lint", "--fail-on", "sometimes")...); code != ExitError {
		t.Fatal("invalid --fail-on must be an error")
	}
	if _, code = runCLI(t, nil, append(pg, "-o", "yaml", "lint")...); code != ExitError {
		t.Fatal("invalid output must be an error")
	}
	if _, code = runCLI(t, nil, "lint"); code != ExitError {
		t.Fatal("lint without a driver or DSN must be an error")
	}
}

func TestGitHubEscaping(t *testing.T) {
	if got := ghProperty("a:b,c%\n"); got != "a%3Ab%2Cc%25%0A" {
		t.Fatalf("ghProperty = %q", got)
	}
	if got := ghData("50% done\r\n"); got != "50%25 done%0D%0A" {
		t.Fatalf("ghData = %q", got)
	}
}

func TestPostgresLintIndexLocks(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	const file = "migrations/1_change.sql"
	pg := []string{"--driver", "postgres", "lint"}
	t.Run("conditional create does not prove the table is empty", func(t *testing.T) {
		write(t, file, `-- +migrate Up
CREATE TABLE IF NOT EXISTS users (id int);
CREATE INDEX users_id ON users (id);
ALTER TABLE users ADD COLUMN email text NOT NULL;
-- +migrate Down
ALTER TABLE users DROP COLUMN email;`)
		out, code := runCLI(t, nil, pg...)
		if code != ExitLint || !strings.Contains(out, "index-not-concurrent") ||
			!strings.Contains(out, "add-not-null-without-default") || !strings.Contains(out, "missing-lock-timeout") {
			t.Fatalf("lint = %d\n%s", code, out)
		}
	})
	t.Run("quoted identifiers are not modifiers", func(t *testing.T) {
		write(t, file, `-- +migrate Up
CREATE INDEX "name ON CONCURRENTLY" ON users ("CONCURRENTLY");
DROP INDEX "CONCURRENTLY";
REINDEX INDEX "CONCURRENTLY";
-- +migrate Down
DROP INDEX "name ON CONCURRENTLY";`)
		out, code := runCLI(t, nil, pg...)
		if code != ExitOK || !strings.Contains(out, "index-not-concurrent") || strings.Contains(out, "concurrently-in-transaction") {
			t.Fatalf("lint = %d\n%s", code, out)
		}
	})
}

func TestSQLServerCLIReportsBatchFindings(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_change.sql", "DECLARE @value int = 42;\nDELETE FROM [items];\nGO\nSELECT 1;")
	out, exit := runCLI(t, map[string]string{"GOMIGRATE_DSN": "sqlserver://localhost?database=unused"}, "lint")
	if exit != ExitOK || !strings.Contains(out, "unbounded-write") {
		t.Fatalf("SQL Server lint = %d\n%s", exit, out)
	}
}

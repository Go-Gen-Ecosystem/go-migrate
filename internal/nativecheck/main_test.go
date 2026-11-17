// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestVerifyComplete(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres", "mysql", "sqlserver", "oracle", "db2"} {
		t.Run(engine, func(t *testing.T) {
			stream := eventStream(t, engine, nil)
			if err := verify(engine, stream, io.Discard, strings.NewReplacer()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVerifyIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name, engine, test, action, want string
	}{
		{"missing package", "sqlite", "", "", conformancePackage + ": missing"},
		{"wrong package", "sqlite", "", "other-package", conformancePackage + ": missing"},
		{"missing root", "sqlite", "TestConformance", "", "TestConformance: missing"},
		{"missing engine parent", "sqlite", "TestConformance/sqlite", "", "TestConformance/sqlite: missing"},
		{"missing lifecycle", "sqlite", "TestConformance/sqlite/lifecycle", "", "lifecycle: missing"},
		{"wrong test name", "sqlite", "TestConformance/sqlite/lifecycle", "wrong-name", "lifecycle: missing"},
		{"package skip", "sqlite", "", "skip", conformancePackage + ": skip"},
		{"duplicate package result", "sqlite", "", "duplicate", "multiple terminal events"},
		{"SQLite surprise skip", "sqlite", "TestConformance/sqlite/failure", "skip", "failure: skip"},
		{"non-SQLite session skip", "oracle", "TestConformance/oracle/session_state_isolation", "skip", "session_state_isolation: skip"},
		{"missing PostgreSQL durability case", "postgres", "TestPostgresNoTxConformance/unfinished_Down", "", "unfinished_Down: missing"},
		{"missing MySQL callback case", "mysql", "TestMySQLRunCallbackAutocommit/explicit_transaction", "", "explicit_transaction: missing"},
		{"SQL Server TLS skip", "sqlserver", "TestConformance/sqlserver/TLS_defaults", "skip", "TLS_defaults: skip"},
		{"Db2 TLS child missing", "db2", "TestConformance/db2/TLS_verification/wrong_hostname", "", "wrong_hostname: missing"},
		{"Db2 TLS child skipped", "db2", "TestConformance/db2/TLS_verification/untrusted_CA", "skip", "untrusted_CA: skip"},
		{"Oracle test failure", "oracle", "TestConformance/oracle/engine_version", "fail", "engine_version: fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := eventStream(t, tc.engine, func(event testEvent) []testEvent {
				if event.Test != tc.test {
					return []testEvent{event}
				}
				switch tc.action {
				case "":
					return nil
				case "other-package":
					event.Package += "/other"
				case "wrong-name":
					event.Test += "X"
				case "duplicate":
					return []testEvent{event, event}
				default:
					event.Action = tc.action
				}
				return []testEvent{event}
			})
			if err := verify(tc.engine, stream, io.Discard, strings.NewReplacer()); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("verify = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVerifyRedactsFixtureOutput(t *testing.T) {
	const dsn = "db2://dbadmin:Private%2FPass@fixture:50001/TEST?ssl_ca=/tmp/test.pem"
	redact := fixtureRedactor(func(key string) string {
		if key == "GOMIGRATE_TEST_DB2_DSN" {
			return dsn
		}
		return ""
	})
	var stream, output strings.Builder
	stream.WriteString("go: downloading dependencies\n")
	encoder := json.NewEncoder(&stream)
	for _, chunk := range []string{"native diagnostic for dbadmin: Private/", "Pass or Private%2FPass\n", "dsn " + dsn + "\n"} {
		if err := encoder.Encode(testEvent{Action: "output", Package: conformancePackage, Output: chunk}); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Encode(testEvent{Action: "build-fail"}); err != nil {
		t.Fatal(err)
	}
	input := io.MultiReader(strings.NewReader(stream.String()), eventStream(t, "db2", nil))
	err := verify("db2", input, &output, redact)
	if err == nil || err.Error() != "conformance build failed" {
		t.Errorf("verify = %v, want build failure", err)
	}
	const want = "go: downloading dependencies\nnative diagnostic for [redacted]: [redacted] or [redacted]\ndsn [redacted]\n"
	if output.String() != want {
		t.Errorf("unredacted or missing fixture output")
	}
}

func TestVerifyRejectsUnsupportedEngine(t *testing.T) {
	if err := verify("postgresql", eventStream(t, "postgres", nil), io.Discard, strings.NewReplacer()); err == nil {
		t.Error("accepted unsupported engine")
	}
}

func TestNativeWorkflowReplaysReleasedSource(t *testing.T) {
	var workflow struct {
		Env  map[string]string
		Jobs map[string]struct {
			If          string
			Environment string
			Strategy    struct{ Matrix struct{ Engine []string } }
			Steps       []struct {
				Uses, Run string
				With, Env map[string]string
			}
		}
	}
	path := filepath.Join("..", "..", ".github", "workflows", "native-conformance.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	const source = "GOMIGRATE_NATIVE_SOURCE_SHA"
	if !strings.Contains(workflow.Env[source], "inputs.source == 'v1.0.1' && '7650c2b6b8aa04a767ad7fa5d7684183f56eeca5'") {
		t.Fatal("native workflow lost the exact v1.0.1 source replay")
	}
	for id, engines := range map[string][]string{
		"local-fixtures":      {"sqlite", "postgres", "mysql", "sqlserver"},
		"disposable-fixtures": {"oracle", "db2"},
	} {
		job := workflow.Jobs[id]
		if job.If != "github.ref == 'refs/heads/main'" || !slices.Equal(job.Strategy.Matrix.Engine, engines) {
			t.Fatalf("job %s lost its main-only engine matrix", id)
		}
		checkedSource := false
		for _, step := range job.Steps {
			checkedSource = checkedSource || (strings.HasPrefix(step.Uses, "actions/checkout@") &&
				step.With["ref"] == "${{ env."+source+" }}" && step.With["path"] == "")
		}
		if !checkedSource {
			t.Fatalf("job %s does not check out the selected source", id)
		}
	}
	job := workflow.Jobs["disposable-fixtures"]
	if job.Environment != "native-conformance-${{ matrix.engine }}" {
		t.Fatal("Oracle and Db2 must use separate fixture environments")
	}
	for _, step := range job.Steps {
		if strings.Contains(step.Run, `.github/scripts/native-fixture.sh "$ENGINE"`) &&
			step.Env["GOMIGRATE_NATIVE_FIXTURE_SEED"] == "${{ secrets.GOMIGRATE_NATIVE_FIXTURE_SEED }}" {
			return
		}
	}
	t.Fatal("native workflow does not run the disposable fixtures with environment-scoped credentials")
}

func TestSQLServerServiceHealthCheckQuoting(t *testing.T) {
	for file, job := range map[string]string{"ci.yml": "sqlserver-conformance", "native-conformance.yml": "local-fixtures"} {
		t.Run(file, func(t *testing.T) {
			var workflow struct {
				Jobs map[string]struct {
					Services map[string]struct {
						Env     map[string]string
						Options string
					}
				}
			}
			raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", file))
			if err != nil {
				t.Fatal(err)
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			service := workflow.Jobs[job].Services["sqlserver"]
			if !strings.Contains(service.Options, `--health-cmd "`) || !strings.Contains(service.Options, `-Q 'SELECT 1'`) {
				t.Fatal("SQL Server health command must survive GitHub runner Docker argument parsing")
			}
			if service.Env["SQLCMDPASSWORD"] == "" || service.Env["SQLCMDPASSWORD"] != service.Env["MSSQL_SA_PASSWORD"] {
				t.Fatal("sqlcmd must receive the fixture password through the service environment")
			}
		})
	}
}

func eventStream(t *testing.T, engine string, modify func(testEvent) []testEvent) io.Reader {
	t.Helper()
	var stream strings.Builder
	encoder := json.NewEncoder(&stream)
	for _, name := range requiredTests(engine) {
		event := testEvent{Action: "pass", Package: conformancePackage, Test: name}
		if name == sqliteSkip {
			event.Action = "skip"
		}
		events := []testEvent{event}
		if modify != nil {
			events = modify(event)
		}
		for _, result := range events {
			if err := encoder.Encode(result); err != nil {
				t.Fatal(err)
			}
		}
	}
	return strings.NewReader(stream.String())
}

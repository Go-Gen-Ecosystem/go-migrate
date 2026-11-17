// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
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

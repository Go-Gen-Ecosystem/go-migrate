// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// nativecheck verifies the terminal events from a single engine's go test -json run.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
)

const (
	conformancePackage = "github.com/go-gen-ecosystem/go-migrate"
	sqliteSkip         = "TestConformance/sqlite/session_state_isolation"
)

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: nativecheck <sqlite|postgres|mysql|sqlserver|oracle|db2>")
		os.Exit(2)
	}
	os.Exit(run(os.Args[1]))
}

func run(engine string) int {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		// Treat native driver and test logs as text even if they contain workflow commands.
		token := rand.Text()
		if _, err := fmt.Fprintf(os.Stdout, "::stop-commands::%s\n", token); err != nil {
			return 1
		}
		defer func() { _, _ = fmt.Fprintf(os.Stdout, "\n::%s::\n", token) }()
	}
	redact := fixtureRedactor(os.Getenv)
	if err := verify(engine, os.Stdin, os.Stdout, redact); err != nil {
		_, _ = fmt.Fprintln(os.Stdout, redact.Replace(err.Error()))
		return 1
	}
	return 0
}

func requiredTests(engine string) []string {
	var dialectTests, extra []string
	switch engine {
	case "sqlite":
	case "mysql":
		const callbacks = "TestMySQLRunCallbackAutocommit"
		extra = []string{callbacks, callbacks + "/autocommit_disabled", callbacks + "/explicit_transaction"}
	case "postgres":
		const durability = "TestPostgresNoTxConformance"
		extra = []string{
			durability,
			durability + "/unfinished_Up",
			durability + "/unfinished_Down",
			durability + "/Go_migration_leaves_an_aborted_transaction",
			durability + "/Go_migration_leaves_database/sql_transaction_open",
			durability + "/Go_migration_cancels_database/sql_transaction",
			durability + "/completed_explicit_transaction_and_concurrent_index",
		}
	case "sqlserver":
		dialectTests = []string{"engine_version", "application_lock_identity", "nontransactional_failure", "TLS_defaults"}
	case "oracle":
		dialectTests = []string{"engine_version", "session_lock_across_DDL", "Go_checksums_and_baseline"}
	case "db2":
		dialectTests = []string{
			"engine_version", "session_lock_across_commit", "TLS_verification", "nontransactional_failure",
			"TLS_verification/untrusted_CA", "TLS_verification/wrong_hostname",
		}
	default:
		return nil
	}

	const root = "TestConformance"
	parent := root + "/" + engine
	tests := []string{"", root, parent} // An empty Test is the package result.
	common := []string{
		"lifecycle", "failure", "repair_scoped", "run_callback_recovery", "dialect_SQL",
		"concurrent_runs", "status_is_read-only", "shipped_examples", "one-connection_pool",
		"session_state_isolation", "baseline_on_migrate", "history_identity_and_rollbacks",
	}
	for _, name := range append(common, dialectTests...) {
		tests = append(tests, parent+"/"+name)
	}
	return append(tests, extra...)
}

func verify(engine string, input io.Reader, output io.Writer, redact *strings.Replacer) error {
	required := requiredTests(engine)
	if required == nil {
		return errors.New("unsupported conformance engine")
	}

	results := make(map[string]string)
	var problems []error
	var pending strings.Builder
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 4096), 1024*1024)
	for scan.Scan() {
		var event testEvent
		if err := json.Unmarshal(scan.Bytes(), &event); err != nil || event.Action == "" {
			// The Go command also writes plain stderr (such as dependency downloads).
			if err := writeOutput(&pending, output, redact, scan.Text()+"\n"); err != nil {
				return err
			}
			continue
		}
		if err := writeOutput(&pending, output, redact, event.Output); err != nil {
			return err
		}
		if event.Action == "build-fail" {
			problems = append(problems, errors.New("conformance build failed"))
		}
		if event.Package != conformancePackage {
			continue
		}
		switch event.Action {
		case "pass", "fail", "skip":
			if _, duplicate := results[event.Test]; duplicate {
				problems = append(problems, fmt.Errorf("multiple terminal events for %s", resultName(event.Test)))
			}
			results[event.Test] = event.Action
			if event.Action == "fail" || (event.Action == "skip" && (engine != "sqlite" || event.Test != sqliteSkip)) {
				problems = append(problems, fmt.Errorf("%s: %s", resultName(event.Test), event.Action))
			}
		}
	}
	if _, err := fmt.Fprint(output, redact.Replace(pending.String())); err != nil {
		return err
	}
	if err := scan.Err(); err != nil {
		problems = append(problems, fmt.Errorf("read conformance events: %w", err))
	}
	for _, test := range required {
		if results[test] == "" {
			problems = append(problems, fmt.Errorf("%s: missing terminal event", resultName(test)))
		}
	}
	return errors.Join(problems...)
}

func resultName(test string) string {
	if test == "" {
		return conformancePackage
	}
	return test
}

func writeOutput(pending *strings.Builder, output io.Writer, redact *strings.Replacer, chunk string) error {
	// test2json can distribute a long line across multiple events. Redact after
	// joining the pieces so a credential split between events is still hidden.
	if end := strings.LastIndexByte(chunk, '\n'); end >= 0 {
		pending.WriteString(chunk[:end+1])
		if _, err := fmt.Fprint(output, redact.Replace(pending.String())); err != nil {
			return err
		}
		pending.Reset()
		chunk = chunk[end+1:]
	}
	pending.WriteString(chunk)
	return nil
}

func fixtureRedactor(getenv func(string) string) *strings.Replacer {
	var secrets []string
	for _, key := range []string{
		"GOMIGRATE_TEST_POSTGRES_DSN", "GOMIGRATE_TEST_MYSQL_DSN", "GOMIGRATE_TEST_SQLSERVER_DSN",
		"GOMIGRATE_TEST_ORACLE_DSN", "GOMIGRATE_TEST_DB2_DSN", "GOMIGRATE_TEST_DB2_APP_DSN",
		"GOMIGRATE_TEST_DB2_TLS_MISMATCH_HOST",
	} {
		value := getenv(key)
		secrets = append(secrets, value)
		u, err := url.Parse(value)
		if err != nil || u.User == nil {
			continue
		}
		if key == "GOMIGRATE_TEST_ORACLE_DSN" || key == "GOMIGRATE_TEST_DB2_DSN" || key == "GOMIGRATE_TEST_DB2_APP_DSN" {
			secrets = append(secrets, u.User.Username(), url.User(u.User.Username()).String())
		}
		if password, ok := u.User.Password(); ok {
			encoded := strings.TrimPrefix(url.UserPassword("", password).String(), ":")
			secrets = append(secrets, password, encoded, url.QueryEscape(password))
		}
	}
	// Prefer complete DSNs and longer forms before shorter encoded credentials.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	var pairs []string
	for _, value := range secrets {
		if value != "" {
			pairs = append(pairs, value, "[redacted]")
		}
	}
	return strings.NewReplacer(pairs...)
}

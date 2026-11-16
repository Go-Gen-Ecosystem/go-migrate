// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/lint"
)

// Finding is one lint result: a risky or unsupported operation in a
// migration. Rule IDs are stable and can be suppressed per file with
// "-- +migrate Lint ignore=<rule>[,<rule>]".
type Finding struct {
	Rule     string
	Severity string // "error", "warning" or "info"
	Message  string
	Hint     string
	// Version is the migration's Label; Source its file. For a callback
	// file, Version is empty and Event names the callback event.
	Version string
	Source  string
	Event   string
	// Statement is the 1-based Up statement, or 0 for the whole migration.
	Statement int
	SQL       string
}

// Lint statically analyzes every SQL migration for dialectName without a
// database: long exclusive locks, table rewrites, data loss, changes that
// break running application versions and unsupported syntax. Use it as a CI
// gate or to let an AI agent review a migration before it is applied.
func Lint(dialectName string, opts ...Option) ([]Finding, error) {
	m, err := build(nil, dialectName, opts)
	if err != nil {
		return nil, err
	}
	return m.Lint(), nil
}

// Lint analyzes this Migrator's SQL migrations (see the package-level Lint).
func (m *Migrator) Lint() []Finding {
	var out []Finding
	for _, u := range m.cat.units {
		if u.kind == history.KindGo || u.otherDBMS {
			continue // Go code is not SQL; other-DBMS files never run here
		}
		issues := lint.Check(lint.Migration{
			Dialect: m.dialect.Name(), Up: u.stmts[Up], Down: u.stmts[Down], NoTx: u.NoTx,
			Versioned: !u.Repeatable, Reversible: u.Reversible, Ignore: u.lintIgnore, Lexical: m.dialect.Lexical(),
		})
		out = appendFindings(out, issues, Finding{Version: u.Label(), Source: u.Source})
	}
	for _, cb := range m.cat.callbacksFor(Up) {
		issues := lint.Check(lint.Migration{Dialect: m.dialect.Name(), Up: cb.Statements, Lexical: m.dialect.Lexical()})
		out = appendFindings(out, issues, Finding{Source: cb.Source, Event: cb.Event})
	}
	return out
}

// appendFindings converts lint issues into Findings located by at.
func appendFindings(out []Finding, issues []lint.Issue, at Finding) []Finding {
	for _, is := range issues {
		f := at
		f.Rule, f.Severity, f.Message, f.Hint = is.Rule, string(is.Severity), is.Message, is.Hint
		f.Statement, f.SQL = is.Statement, truncateSQL(is.SQL)
		out = append(out, f)
	}
	return out
}

// LintRules lists every lint rule ID.
func LintRules() []string { return lint.RuleIDs() }

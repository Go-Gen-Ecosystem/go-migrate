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
	// Version is the migration's Label; Source its file.
	Version string
	Source  string
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
		for _, is := range issues {
			out = append(out, Finding{
				Rule: is.Rule, Severity: string(is.Severity), Message: is.Message, Hint: is.Hint,
				Version: u.Label(), Source: u.Source, Statement: is.Statement, SQL: truncateSQL(is.SQL),
			})
		}
	}
	return out
}

// LintRules lists every lint rule ID.
func LintRules() []string { return lint.RuleIDs() }

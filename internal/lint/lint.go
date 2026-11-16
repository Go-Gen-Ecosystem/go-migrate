// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// Package lint finds risky operations in migration SQL without a database:
// statements that take long exclusive locks, rewrite tables, lose data, break
// running application versions or are unsupported by the target dialect.
//
// Rules run on normalized code (comments removed, literals blanked,
// upper-cased, whitespace collapsed), so text inside strings and comments
// never triggers a finding. Rule IDs are stable kebab-case names that can be
// suppressed per file with "-- +migrate Lint ignore=<id>[,<id>]".
package lint

import (
	"regexp"
	"slices"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/script"
)

// Severity of a finding.
type Severity string

// Severities, from most to least serious.
const (
	Error   Severity = "error"   // will fail, or is unsafe on any non-empty table
	Warning Severity = "warning" // risky in production: locks, rewrites, data loss
	Info    Severity = "info"    // best practice
)

// Issue is one finding. Statement is 1-based; 0 means the whole migration.
type Issue struct {
	Rule      string
	Severity  Severity
	Message   string
	Hint      string
	Statement int
	SQL       string
}

// Migration is the input to Check.
type Migration struct {
	Dialect    string // postgres | mysql | sqlite
	Up, Down   []string
	NoTx       bool
	Versioned  bool
	Reversible bool
	Ignore     []string
	Lexical    script.Lexical
}

// stmtCtx is what a statement rule sees.
type stmtCtx struct {
	dialect string
	norm    string          // normalized statement
	noTx    bool            // runs outside a transaction
	created map[string]bool // tables created earlier in this migration
}

type rule struct {
	id       string
	severity Severity
	dialects []string // empty: all
	message  string
	hint     string
	match    func(c *stmtCtx) bool
}

var (
	reCreateTable = regexp.MustCompile(`^CREATE (?:TEMPORARY |TEMP |UNLOGGED )?TABLE (?:IF NOT EXISTS )?([^\s(]+)`)
	reAlterTable  = regexp.MustCompile(`^ALTER TABLE (?:IF EXISTS )?(?:ONLY )?([^\s(]+)`)
	reIndexOn     = regexp.MustCompile(`^CREATE (?:UNIQUE )?INDEX .*? ON (?:ONLY )?([^\s(]+)`)
	reAddNotNull  = regexp.MustCompile(`\bADD (?:COLUMN )?(?:IF NOT EXISTS )?\S+ [^,]*\bNOT NULL\b`)
	reAddWithDef  = regexp.MustCompile(`\bADD (?:COLUMN )?(?:IF NOT EXISTS )?\S+ [^,]*\bDEFAULT\b`)
	reAlterType   = regexp.MustCompile(`\bALTER (?:COLUMN )?\S+ (?:SET DATA )?TYPE\b`)
	reMySQLModify = regexp.MustCompile(`^ALTER TABLE \S+ .*\b(?:MODIFY|CHANGE)\b`)
	reAddFK       = regexp.MustCompile(`\bADD (?:CONSTRAINT \S+ )?FOREIGN KEY\b`)
	reAddCheck    = regexp.MustCompile(`\bADD (?:CONSTRAINT \S+ )?CHECK\b`)
	reSetNotNull  = regexp.MustCompile(`\bALTER (?:COLUMN )?\S+ SET NOT NULL\b`)
	reRename      = regexp.MustCompile(`^ALTER TABLE \S+ RENAME\b|^RENAME TABLE\b`)
	reSQLiteAlter = regexp.MustCompile(`^ALTER TABLE \S+ (?:ALTER|MODIFY|ADD CONSTRAINT|DROP CONSTRAINT)\b`)
	reLockTimeout = regexp.MustCompile(`^SET (?:LOCAL |SESSION )?LOCK_TIMEOUT\b`)
	reDDL         = regexp.MustCompile(`^(?:ALTER TABLE|CREATE (?:UNIQUE )?INDEX|DROP |TRUNCATE)`)
)

// alterTarget returns the table an ALTER TABLE or CREATE INDEX statement acts on.
func alterTarget(norm string) string {
	if m := reAlterTable.FindStringSubmatch(norm); m != nil {
		return m[1]
	}
	if m := reIndexOn.FindStringSubmatch(norm); m != nil {
		return m[1]
	}
	return ""
}

// existing reports whether the statement acts on a table that already
// existed before this migration (so locks and rewrites matter).
func (c *stmtCtx) existing() bool { return !c.created[alterTarget(c.norm)] }

var rules = []rule{
	{id: "drop-table", severity: Warning, message: "DROP TABLE permanently deletes the table and its data",
		hint:  "make sure no deployed version still reads it; back up first",
		match: func(c *stmtCtx) bool { return strings.HasPrefix(c.norm, "DROP TABLE") }},
	{id: "drop-column", severity: Warning, message: "dropping a column deletes its data and breaks app versions still reading it",
		hint: "stop using the column in a release first (expand/contract), then drop it",
		match: func(c *stmtCtx) bool {
			return strings.HasPrefix(c.norm, "ALTER TABLE") && strings.Contains(c.norm, " DROP COLUMN ")
		}},
	{id: "truncate", severity: Warning, message: "TRUNCATE deletes every row",
		hint:  "intended only for seed or reference data",
		match: func(c *stmtCtx) bool { return strings.HasPrefix(c.norm, "TRUNCATE") }},
	{id: "unbounded-write", severity: Warning, message: "UPDATE or DELETE without WHERE touches every row",
		hint: "add a WHERE clause, or batch large backfills to avoid long locks",
		match: func(c *stmtCtx) bool {
			return (strings.HasPrefix(c.norm, "UPDATE ") || strings.HasPrefix(c.norm, "DELETE ")) && !strings.Contains(c.norm, " WHERE ")
		}},
	{id: "index-not-concurrent", severity: Warning, dialects: []string{"postgres"},
		message: "CREATE INDEX without CONCURRENTLY blocks writes to the table while it builds",
		hint:    "use CREATE INDEX CONCURRENTLY in a migration marked '-- +migrate Up notransaction'",
		match: func(c *stmtCtx) bool {
			return reIndexOn.MatchString(c.norm) && !strings.Contains(c.norm, "CONCURRENTLY") && c.existing()
		}},
	{id: "concurrently-in-transaction", severity: Error, dialects: []string{"postgres"},
		message: "CONCURRENTLY cannot run inside a transaction block; this migration will fail",
		hint:    "mark the migration '-- +migrate Up notransaction'",
		match:   func(c *stmtCtx) bool { return strings.Contains(c.norm, " CONCURRENTLY ") && !c.noTx }},
	{id: "add-not-null-without-default", severity: Error, dialects: []string{"postgres", "sqlite"},
		message: "adding a NOT NULL column without DEFAULT fails on a table that has rows",
		hint:    "add it nullable or with a DEFAULT, backfill, then SET NOT NULL",
		match: func(c *stmtCtx) bool {
			return reAddNotNull.MatchString(c.norm) && !reAddWithDef.MatchString(c.norm) && c.existing()
		}},
	{id: "add-not-null-without-default", severity: Warning, dialects: []string{"mysql"},
		message: "adding a NOT NULL column without DEFAULT silently fills existing rows with an implicit zero value",
		hint:    "give the column an explicit DEFAULT",
		match: func(c *stmtCtx) bool {
			return reAddNotNull.MatchString(c.norm) && !reAddWithDef.MatchString(c.norm) && c.existing()
		}},
	{id: "column-type-change", severity: Warning, dialects: []string{"postgres", "mysql"},
		message: "changing a column type usually rewrites the whole table under an exclusive lock",
		hint:    "add a new column, backfill in batches, switch reads, then drop the old one",
		match: func(c *stmtCtx) bool {
			return (reAlterType.MatchString(c.norm) || (c.dialect == "mysql" && reMySQLModify.MatchString(c.norm))) && c.existing()
		}},
	{id: "foreign-key-not-valid", severity: Warning, dialects: []string{"postgres"},
		message: "adding a FOREIGN KEY validates every row while locking both tables",
		hint:    "add it with NOT VALID, then run ALTER TABLE ... VALIDATE CONSTRAINT in a later migration",
		match: func(c *stmtCtx) bool {
			return reAddFK.MatchString(c.norm) && !strings.Contains(c.norm, "NOT VALID") && c.existing()
		}},
	{id: "check-not-valid", severity: Warning, dialects: []string{"postgres"},
		message: "adding a CHECK constraint scans the whole table under an exclusive lock",
		hint:    "add it with NOT VALID, then VALIDATE CONSTRAINT separately",
		match: func(c *stmtCtx) bool {
			return reAddCheck.MatchString(c.norm) && !strings.Contains(c.norm, "NOT VALID") && c.existing()
		}},
	{id: "set-not-null", severity: Warning, dialects: []string{"postgres"},
		message: "SET NOT NULL scans the whole table under an exclusive lock",
		hint:    "add CHECK (col IS NOT NULL) NOT VALID, VALIDATE it, then SET NOT NULL (PostgreSQL 12+ skips the scan)",
		match:   func(c *stmtCtx) bool { return reSetNotNull.MatchString(c.norm) && c.existing() }},
	{id: "rename", severity: Warning, message: "renaming breaks application versions that still use the old name",
		hint:  "add the new name, migrate readers and writers, then remove the old one",
		match: func(c *stmtCtx) bool { return reRename.MatchString(c.norm) && c.existing() }},
	{id: "sqlite-unsupported-alter", severity: Error, dialects: []string{"sqlite"},
		message: "SQLite does not support this ALTER TABLE form",
		hint:    "create a new table, copy the data, drop the old table and rename",
		match:   func(c *stmtCtx) bool { return reSQLiteAlter.MatchString(c.norm) }},
	{id: "full-table-rewrite", severity: Warning, dialects: []string{"postgres"},
		message: "VACUUM FULL and CLUSTER rewrite the table under an exclusive lock",
		hint:    "use pg_repack or schedule a maintenance window",
		match: func(c *stmtCtx) bool {
			return strings.HasPrefix(c.norm, "VACUUM FULL") || strings.HasPrefix(c.norm, "CLUSTER")
		}},
}

// Normalize prepares a statement for rule matching.
func Normalize(stmt string, lx script.Lexical) string {
	return strings.ToUpper(strings.Join(strings.Fields(script.Code(stmt, lx)), " "))
}

// Check returns the issues found in one migration, in statement order.
func Check(m Migration) []Issue {
	var out []Issue
	add := func(r rule, stmt int, sql string) {
		if slices.Contains(m.Ignore, r.id) {
			return
		}
		out = append(out, Issue{Rule: r.id, Severity: r.severity, Message: r.message, Hint: r.hint, Statement: stmt, SQL: sql})
	}

	c := &stmtCtx{dialect: m.Dialect, noTx: m.NoTx, created: map[string]bool{}}
	hasDDL, hasTimeout := false, false
	for i, stmt := range m.Up {
		c.norm = Normalize(stmt, m.Lexical)
		if mt := reCreateTable.FindStringSubmatch(c.norm); mt != nil {
			c.created[mt[1]] = true
			continue
		}
		hasTimeout = hasTimeout || reLockTimeout.MatchString(c.norm)
		hasDDL = hasDDL || (reDDL.MatchString(c.norm) && c.existing())
		for _, r := range rules {
			if (len(r.dialects) == 0 || slices.Contains(r.dialects, m.Dialect)) && r.match(c) {
				add(r, i+1, stmt)
			}
		}
	}
	if m.Dialect == "postgres" && hasDDL && !hasTimeout {
		add(rule{id: "missing-lock-timeout", severity: Info,
			message: "DDL on existing tables without lock_timeout can queue behind a long query and block all traffic",
			hint:    "start the migration with SET LOCAL lock_timeout = '5s' (or SET lock_timeout for notransaction)"}, 0, "")
	}
	if m.Versioned && !m.Reversible {
		add(rule{id: "irreversible", severity: Info,
			message: "migration has no Down section and cannot be rolled back",
			hint:    "add a '-- +migrate Down' section, or accept that rollback means restore"}, 0, "")
	}
	return out
}

// RuleIDs lists every rule ID (for documentation and ignore validation).
func RuleIDs() []string {
	ids := []string{"missing-lock-timeout", "irreversible"}
	for _, r := range rules {
		if !slices.Contains(ids, r.id) {
			ids = append(ids, r.id)
		}
	}
	slices.Sort(ids)
	return ids
}

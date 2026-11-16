// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// Package lint finds risky operations in migration SQL without a database:
// statements that take long exclusive locks, rewrite tables, lose data, break
// running application versions or are unsupported by the target dialect.
//
// Rules run on normalized code (comments removed, literals blanked,
// upper-cased, whitespace collapsed), so text inside strings and comments
// never triggers a finding. ALTER TABLE statements are split into their
// individual actions first, so each action is judged on its own. Rule IDs are
// stable kebab-case names that can be suppressed per file with
// "-- +migrate Lint ignore=<id>[,<id>]".
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

// stmtCtx is what a rule sees for one statement.
type stmtCtx struct {
	dialect string
	norm    string          // normalized statement
	noTx    bool            // runs outside a transaction
	created map[string]bool // tables created earlier in this migration
	table   string          // ALTER TABLE target (normalized identifier), if any
	actions []string        // ALTER TABLE actions, split at top-level commas
}

// existing reports whether the statement acts on a table that already
// existed before this migration (so locks and rewrites matter).
func (c *stmtCtx) existing() bool { return !c.created[c.table] }

// rule matches a whole statement; actionRule matches one ALTER TABLE action.
type rule struct {
	id       string
	severity Severity
	dialects []string // empty: all
	message  string
	hint     string
	match    func(c *stmtCtx) bool
	action   func(c *stmtCtx, action string) bool
}

func (r rule) appliesTo(dialect string) bool {
	return len(r.dialects) == 0 || slices.Contains(r.dialects, dialect)
}

var (
	reCreateTable = regexp.MustCompile(`^CREATE (?:TEMPORARY |TEMP |UNLOGGED )?TABLE (?:IF NOT EXISTS )?([^\s(]+)`)
	reAlterTable  = regexp.MustCompile(`^ALTER TABLE (?:IF EXISTS )?(?:ONLY )?(\S+) (.+)$`)
	reIndexOn     = regexp.MustCompile(`^CREATE (?:UNIQUE )?INDEX\b.*?\bON (?:ONLY )?([^\s(]+)`)
	reConcurrent  = regexp.MustCompile(`^(?:CREATE (?:UNIQUE )?INDEX|DROP INDEX|REINDEX)\b.*\bCONCURRENTLY\b`)
	reAddColumn   = regexp.MustCompile(`^ADD (?:COLUMN )?(?:IF NOT EXISTS )?(\S+) (\S+)`)
	reDropColumn  = regexp.MustCompile(`^DROP (?:COLUMN )?(?:IF EXISTS )?(\S+)`)
	reAlterType   = regexp.MustCompile(`^ALTER (?:COLUMN )?\S+ (?:SET DATA )?TYPE\b`)
	reMySQLModify = regexp.MustCompile(`^(?:MODIFY|CHANGE)\b`)
	reAddFK       = regexp.MustCompile(`^ADD (?:CONSTRAINT \S+ )?FOREIGN KEY\b`)
	reAddCheck    = regexp.MustCompile(`^ADD (?:CONSTRAINT \S+ )?CHECK\b`)
	reSetNotNull  = regexp.MustCompile(`^ALTER (?:COLUMN )?\S+ SET NOT NULL\b`)
	reSQLiteAlter = regexp.MustCompile(`^(?:ALTER|MODIFY|CHANGE|ADD CONSTRAINT|DROP CONSTRAINT)\b`)
	reLockTimeout = regexp.MustCompile(`^SET (LOCAL |SESSION )?LOCK_TIMEOUT\b`)
	reDDL         = regexp.MustCompile(`^(?:ALTER TABLE|CREATE (?:UNIQUE )?INDEX|DROP (?:TABLE|INDEX)|TRUNCATE)`)
)

// notColumns are words after ADD/DROP that introduce something other than a column.
var notColumns = []string{"CONSTRAINT", "CHECK", "PRIMARY", "UNIQUE", "FOREIGN", "INDEX", "KEY",
	"FULLTEXT", "SPATIAL", "PARTITION", "DEFAULT", "NOT"}

// selfDefaulting are column types or clauses that supply a value for every row.
var selfDefaulting = []string{"SERIAL", "BIGSERIAL", "SMALLSERIAL", "SERIAL2", "SERIAL4", "SERIAL8"}

// addsNotNullWithoutDefault reports an ADD COLUMN ... NOT NULL action that
// gives existing rows no value.
func addsNotNullWithoutDefault(action string) bool {
	m := reAddColumn.FindStringSubmatch(action)
	if m == nil || slices.Contains(notColumns, m[1]) || slices.Contains(selfDefaulting, m[2]) {
		return false
	}
	return strings.Contains(action, " NOT NULL") && !strings.Contains(action, " DEFAULT ") &&
		!strings.Contains(action, " GENERATED ") && !strings.Contains(action, " AUTO_INCREMENT")
}

var statementRules = []rule{
	{id: "drop-table", severity: Warning, message: "DROP TABLE permanently deletes the table and its data",
		hint:  "make sure no deployed version still reads it; back up first",
		match: func(c *stmtCtx) bool { return strings.HasPrefix(c.norm, "DROP TABLE") }},
	{id: "drop-schema", severity: Warning, message: "DROP SCHEMA / DROP DATABASE deletes every object in it",
		hint: "intended only for test or temporary schemas",
		match: func(c *stmtCtx) bool {
			return strings.HasPrefix(c.norm, "DROP SCHEMA") || strings.HasPrefix(c.norm, "DROP DATABASE")
		}},
	{id: "truncate", severity: Warning, message: "TRUNCATE deletes every row",
		hint:  "intended only for seed or reference data",
		match: func(c *stmtCtx) bool { return strings.HasPrefix(c.norm, "TRUNCATE") }},
	{id: "unbounded-write", severity: Warning, message: "UPDATE or DELETE without WHERE touches every row",
		hint: "add a WHERE clause, or batch large backfills to avoid long locks",
		match: func(c *stmtCtx) bool {
			return (strings.HasPrefix(c.norm, "UPDATE ") || strings.HasPrefix(c.norm, "DELETE ")) && !topLevel(c.norm, " WHERE ")
		}},
	{id: "index-not-concurrent", severity: Warning, dialects: []string{"postgres"},
		message: "CREATE INDEX without CONCURRENTLY blocks writes to the table while it builds",
		hint:    "use CREATE INDEX CONCURRENTLY in a migration marked '-- +migrate Up notransaction'",
		match: func(c *stmtCtx) bool {
			return reIndexOn.MatchString(c.norm) && !strings.Contains(c.norm, "CONCURRENTLY") && c.existing()
		}},
	{id: "concurrently-in-transaction", severity: Error, dialects: []string{"postgres"},
		message: "CREATE/DROP INDEX CONCURRENTLY and REINDEX CONCURRENTLY cannot run inside a transaction block; this migration will fail",
		hint:    "mark the migration '-- +migrate Up notransaction'",
		match:   func(c *stmtCtx) bool { return reConcurrent.MatchString(c.norm) && !c.noTx }},
	{id: "rename", severity: Warning, dialects: []string{"mysql"},
		message: "renaming breaks application versions that still use the old name",
		hint:    "add the new name, migrate readers and writers, then remove the old one",
		match:   func(c *stmtCtx) bool { return strings.HasPrefix(c.norm, "RENAME TABLE") }},
	{id: "full-table-rewrite", severity: Warning, dialects: []string{"postgres"},
		message: "VACUUM FULL and CLUSTER rewrite the table under an exclusive lock",
		hint:    "use pg_repack or schedule a maintenance window",
		match: func(c *stmtCtx) bool {
			return strings.HasPrefix(c.norm, "VACUUM FULL") || strings.HasPrefix(c.norm, "CLUSTER")
		}},
	{id: "sqlite-unsupported-alter", severity: Error, dialects: []string{"sqlite"},
		message: "SQLite allows one action per ALTER TABLE statement",
		hint:    "split the statement into one ALTER TABLE per change",
		match:   func(c *stmtCtx) bool { return len(c.actions) > 1 }},
}

var actionRules = []rule{
	{id: "drop-column", severity: Warning, message: "dropping a column deletes its data and breaks app versions still reading it",
		hint: "stop using the column in a release first (expand/contract), then drop it",
		action: func(_ *stmtCtx, a string) bool {
			m := reDropColumn.FindStringSubmatch(a)
			return m != nil && !slices.Contains(notColumns, m[1])
		}},
	{id: "add-not-null-without-default", severity: Error, dialects: []string{"postgres"},
		message: "adding a NOT NULL column without DEFAULT fails on a table that has rows",
		hint:    "add it nullable or with a DEFAULT, backfill, then SET NOT NULL",
		action:  func(c *stmtCtx, a string) bool { return c.existing() && addsNotNullWithoutDefault(a) }},
	{id: "add-not-null-without-default", severity: Error, dialects: []string{"sqlite"},
		message: "SQLite rejects adding a NOT NULL column without a DEFAULT, even to an empty table",
		hint:    "give the column a DEFAULT",
		action:  func(c *stmtCtx, a string) bool { return c.existing() && addsNotNullWithoutDefault(a) }},
	{id: "add-not-null-without-default", severity: Warning, dialects: []string{"mysql"},
		message: "adding a NOT NULL column without DEFAULT silently fills existing rows with an implicit zero value",
		hint:    "give the column an explicit DEFAULT",
		action:  func(c *stmtCtx, a string) bool { return c.existing() && addsNotNullWithoutDefault(a) }},
	{id: "column-type-change", severity: Warning, dialects: []string{"postgres", "mysql"},
		message: "changing a column type usually rewrites the whole table under an exclusive lock",
		hint:    "add a new column, backfill in batches, switch reads, then drop the old one",
		action: func(c *stmtCtx, a string) bool {
			return c.existing() && (reAlterType.MatchString(a) || (c.dialect == "mysql" && reMySQLModify.MatchString(a)))
		}},
	{id: "foreign-key-not-valid", severity: Warning, dialects: []string{"postgres"},
		message: "adding a FOREIGN KEY validates every row while locking both tables",
		hint:    "add it with NOT VALID, then run ALTER TABLE ... VALIDATE CONSTRAINT in a later migration",
		action: func(c *stmtCtx, a string) bool {
			return c.existing() && reAddFK.MatchString(a) && !strings.Contains(a, "NOT VALID")
		}},
	{id: "check-not-valid", severity: Warning, dialects: []string{"postgres"},
		message: "adding a CHECK constraint scans the whole table under an exclusive lock",
		hint:    "add it with NOT VALID, then VALIDATE CONSTRAINT separately",
		action: func(c *stmtCtx, a string) bool {
			return c.existing() && reAddCheck.MatchString(a) && !strings.Contains(a, "NOT VALID")
		}},
	{id: "set-not-null", severity: Warning, dialects: []string{"postgres"},
		message: "SET NOT NULL scans the whole table under an exclusive lock",
		hint:    "add CHECK (col IS NOT NULL) NOT VALID, VALIDATE it, then SET NOT NULL (PostgreSQL 12+ skips the scan)",
		action:  func(c *stmtCtx, a string) bool { return c.existing() && reSetNotNull.MatchString(a) }},
	{id: "rename", severity: Warning, message: "renaming breaks application versions that still use the old name",
		hint:   "add the new name, migrate readers and writers, then remove the old one",
		action: func(c *stmtCtx, a string) bool { return c.existing() && strings.HasPrefix(a, "RENAME") }},
	{id: "sqlite-unsupported-alter", severity: Error, dialects: []string{"sqlite"},
		message: "SQLite does not support this ALTER TABLE form",
		hint:    "create a new table, copy the data, drop the old table and rename",
		action:  func(_ *stmtCtx, a string) bool { return reSQLiteAlter.MatchString(a) }},
}

// Normalize prepares a statement for rule matching.
func Normalize(stmt string, lx script.Lexical) string {
	return strings.ToUpper(strings.Join(strings.Fields(script.Code(stmt, lx)), " "))
}

// Check returns the issues found in one migration, in statement order. A
// rule reports at most once per statement.
func Check(m Migration) []Issue {
	var out []Issue
	add := func(r rule, stmt int, sql string) {
		if slices.Contains(m.Ignore, r.id) {
			return
		}
		for _, prev := range out {
			if prev.Rule == r.id && prev.Statement == stmt && stmt > 0 {
				return
			}
		}
		out = append(out, Issue{Rule: r.id, Severity: r.severity, Message: r.message, Hint: r.hint, Statement: stmt, SQL: sql})
	}

	created := map[string]bool{}
	hasDDL, hasTimeout := false, false
	for i, stmt := range m.Up {
		c := newStmtCtx(m, stmt, created)
		if mt := reCreateTable.FindStringSubmatch(c.norm); mt != nil {
			created[ident(mt[1])] = true
			continue
		}
		if tm := reLockTimeout.FindStringSubmatch(c.norm); tm != nil && (!m.NoTx || tm[1] != "LOCAL ") {
			hasTimeout = true // SET LOCAL does nothing outside a transaction
		}
		hasDDL = hasDDL || (reDDL.MatchString(c.norm) && c.existing())
		for _, r := range statementRules {
			if r.appliesTo(m.Dialect) && r.match(c) {
				add(r, i+1, stmt)
			}
		}
		for _, a := range c.actions {
			for _, r := range actionRules {
				if r.appliesTo(m.Dialect) && r.action(c, a) {
					add(r, i+1, stmt)
				}
			}
		}
	}
	if m.Dialect == "postgres" && hasDDL && !hasTimeout {
		add(rule{id: "missing-lock-timeout", severity: Info,
			message: "DDL on existing tables without lock_timeout can queue behind a long query and block all traffic",
			hint:    "start the migration with SET LOCAL lock_timeout = '5s' (SET lock_timeout for notransaction)"}, 0, "")
	}
	if m.Versioned && !m.Reversible {
		add(rule{id: "irreversible", severity: Info,
			message: "migration has no Down section and cannot be rolled back",
			hint:    "add a '-- +migrate Down' section, or accept that rollback means restore"}, 0, "")
	}
	return out
}

func newStmtCtx(m Migration, stmt string, created map[string]bool) *stmtCtx {
	c := &stmtCtx{dialect: m.Dialect, norm: Normalize(stmt, m.Lexical), noTx: m.NoTx, created: created}
	if am := reAlterTable.FindStringSubmatch(c.norm); am != nil {
		c.table, c.actions = ident(am[1]), splitTopLevel(am[2])
	} else if im := reIndexOn.FindStringSubmatch(c.norm); im != nil {
		c.table = ident(im[1])
	}
	return c
}

// ident normalizes a table reference: schema dropped, quotes removed.
func ident(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return strings.Trim(s, "\"`[]")
}

// splitTopLevel splits s at commas outside parentheses.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// topLevel reports whether sub occurs in s outside parentheses.
func topLevel(s, sub string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && strings.HasPrefix(s[i:], sub) {
				return true
			}
		}
	}
	return false
}

// RuleIDs lists every rule ID (for documentation and ignore validation).
func RuleIDs() []string {
	ids := []string{"missing-lock-timeout", "irreversible"}
	for _, rs := range [][]rule{statementRules, actionRules} {
		for _, r := range rs {
			if !slices.Contains(ids, r.id) {
				ids = append(ids, r.id)
			}
		}
	}
	slices.Sort(ids)
	return ids
}

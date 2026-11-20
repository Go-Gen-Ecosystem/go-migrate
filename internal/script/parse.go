// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package script

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"regexp"
	"strings"
	"unicode"
)

// Direction selects the up or down half of a migration.
type Direction int

// Migration directions.
const (
	Up Direction = iota
	Down
)

// Parse errors. They are wrapped with the offending line number.
var (
	ErrContentBeforeMarker = errors.New("SQL appears before the first Up/Down marker")
	ErrDuplicateMarker     = errors.New("duplicate Up/Down marker")
	ErrUnbalancedBlock     = errors.New("unbalanced StatementBegin/StatementEnd")
	ErrUnknownDirective    = errors.New("unknown directive")
	ErrTSQLBatch           = errors.New("expected GO on its own line outside StatementBegin; GO counts are unsupported")
	ErrPLSQLBlock          = errors.New("wrap Oracle PL/SQL in StatementBegin/StatementEnd and omit the SQL*Plus slash")
	ErrSQLPLBlock          = errors.New("wrap Db2 SQL PL in StatementBegin/StatementEnd and omit CLP terminators")
)

// Section is one direction of a migration: an ordered list of chunks. A chunk
// is either ordinary SQL to be split, or a verbatim block delimited by
// StatementBegin/StatementEnd that is executed as a single statement.
type Section struct {
	chunks []chunk
}

type chunk struct {
	sql      string
	verbatim bool
	// canon is sql with insignificant whitespace removed: trailing
	// whitespace on lines that end in code, and leading and trailing blank
	// lines. It is the input to the checksum.
	canon string
}

// Empty reports whether the section has no executable SQL under lx.
func (s Section) Empty(lx Lexical) bool {
	if lx.TSQL {
		return len(s.Statements(lx)) == 0
	}
	for _, c := range s.chunks {
		if c.verbatim && strings.TrimSpace(c.sql) != "" {
			return false
		}
		if !c.verbatim && HasStatement(c.sql, lx) {
			return false
		}
	}
	return true
}

// checksumFormat versions the checksum input, so a future change to it can
// be told apart from drift.
const checksumFormat = "gomigrate/checksum/v1"

// Checksum fingerprints what the section executes: its SQL with only
// insignificant whitespace normalized (line endings, a BOM, trailing
// whitespace outside literals, surrounding blank lines) and the boundaries
// between plain and verbatim (StatementBegin/End) blocks. Any change to a
// literal, a statement or block structure changes it.
func (s Section) Checksum() string {
	h := sha256.New()
	h.Write([]byte(checksumFormat))
	for _, c := range s.chunks {
		if c.canon == "" {
			continue
		}
		kind := byte('p')
		if c.verbatim {
			kind = 'v'
		}
		h.Write([]byte{0, kind})
		writeChecksumText(h, c.canon)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Hash large SQL from bounded scratch space. Converting the entire string to
// []byte for hash.Hash.Write would allocate another copy of each large chunk.
func writeChecksumText(h hash.Hash, text string) {
	const block = 4 << 10
	if len(text) <= block {
		h.Write([]byte(text))
		return
	}
	var buf [block]byte
	for len(text) > 0 {
		n := copy(buf[:], text)
		h.Write(buf[:n])
		text = text[n:]
	}
}

// Statements returns the section's statements in execution order.
func (s Section) Statements(lx Lexical) []string {
	var out []string
	for _, c := range s.chunks {
		if c.verbatim {
			if stmt := strings.TrimSpace(c.sql); stmt != "" {
				if !lx.TSQL && (!lx.Oracle || !startsPLSQL(stmt, lx)) {
					stmt = strings.TrimSuffix(stmt, ";")
				}
				out = append(out, stmt)
			}
			continue
		}
		if lx.TSQL {
			out = appendTSQLBatches(out, c.sql, lx)
			continue
		}
		out = append(out, Split(c.sql, lx)...)
	}
	return out
}

// Script is a parsed migration file.
type Script struct {
	Up   Section
	Down Section
	// NoTx disables the per-migration transaction (needed for statements such
	// as PostgreSQL CREATE INDEX CONCURRENTLY).
	NoTx bool
	Meta Meta
}

// Meta holds the targeting and control directives of a migration file
// (Liquibase-style):
//
//	-- +migrate Context dev,test          run only in these contexts
//	-- +migrate Labels billing            run only when a label is selected
//	-- +migrate DBMS postgres,sqlite      run only on these databases
//	-- +migrate RunAlways                 repeatable: re-run on every Up
//	-- +migrate Precondition onFail=MARK_RAN expect=0 SELECT COUNT(*) FROM t
type Meta struct {
	Contexts      []string
	Labels        []string
	DBMS          []string
	RunAlways     bool
	Preconditions []Precondition
	// LintIgnore lists lint rule IDs suppressed for this file
	// ("-- +migrate Lint ignore=drop-table,rename").
	LintIgnore []string
}

// Empty reports whether no directive was set.
func (m Meta) Empty() bool {
	return len(m.Contexts) == 0 && len(m.Labels) == 0 && len(m.DBMS) == 0 && !m.RunAlways &&
		len(m.Preconditions) == 0 && len(m.LintIgnore) == 0
}

// OnFail says what happens when a precondition does not hold.
type OnFail string

// Precondition outcomes (Liquibase semantics).
const (
	// Halt fails the run (default).
	Halt OnFail = "HALT"
	// MarkRan records the migration as applied without running it.
	MarkRan OnFail = "MARK_RAN"
	// Continue skips the migration for this run; it stays pending.
	Continue OnFail = "CONTINUE"
)

// Precondition is a single-value SQL check: the migration runs only if the
// query's result equals Expect.
type Precondition struct {
	SQL    string
	Expect string
	OnFail OnFail
}

// ErrBadPrecondition is returned for a malformed Precondition directive.
var ErrBadPrecondition = errors.New("malformed precondition (want: Precondition [onFail=HALT|MARK_RAN|CONTINUE] expect=<value> <SQL>)")

// directive matches "-- +migrate <word> [opts]" and "-- +goose <word> [opts]",
// case-insensitively, so files written for sql-migrate or goose load as-is.
var directive = regexp.MustCompile(`(?i)^\s*--\s*\+(?:migrate|goose)\s+(\S+)(?:\s+(.*?))?\s*$`)

// Parse parses a migration file with the dialect's lexical rules lx. Files
// without any Up/Down marker belong entirely to direction def (Up for a
// single-file migration, or the direction encoded in a paired *.up.sql /
// *.down.sql file name). A directive is recognized only on a line that
// starts outside any literal, quoted identifier or comment.
func Parse(content []byte, def Direction, lx Lexical) (Script, error) {
	// Only line endings and the BOM are normalized for execution; trailing
	// whitespace inside string literals must reach the database unchanged.
	text := lineEndings(content)
	var (
		sc        Script
		cur       *Section
		seen      [2]bool
		markers   bool
		inBlock   bool
		buf       chunkBuilder
		blockLine int
		st        lexState
	)
	flush := func(verbatim bool) {
		if buf.sql.Len() > 0 {
			cur.chunks = append(cur.chunks, buf.chunk(verbatim))
		}
		buf = chunkBuilder{}
	}
	sectionFor := func(d Direction) *Section {
		if d == Down {
			return &sc.Down
		}
		return &sc.Up
	}
	cur = sectionFor(def)
	preamble := preambleLexical
	preamble.TSQL = lx.TSQL

	lineNo := 0
	for line := range strings.SplitSeq(text, "\n") {
		lineNo++
		if lx.TSQL && st.inCode() {
			if goLine, valid := tsqlGoLine(line); goLine && (!valid || inBlock) {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrTSQLBatch)
			}
		}
		if lx.Oracle && st.inCode() && strings.TrimSpace(line) == "/" {
			return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrPLSQLBlock)
		}
		var m []string
		if st.inCode() && mayBeDirective(line) {
			m = directive.FindStringSubmatch(line)
		}
		if m == nil {
			st = st.line(line, lx)
			buf.appendLine(line, st.inCode())
			st = st.newline()
			continue
		}
		word := strings.ToLower(m[1])
		opts := strings.ToLower(strings.TrimSpace(m[2]))
		switch word {
		case "up", "down":
			if inBlock {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrUnbalancedBlock)
			}
			d := Up
			if word == "down" {
				d = Down
			}
			if seen[d] {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrDuplicateMarker)
			}
			if !markers && HasStatement(buf.sql.String(), preamble) {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrContentBeforeMarker)
			}
			if !markers {
				buf = chunkBuilder{} // leading comments/whitespace belong to no section
			} else {
				flush(false)
			}
			markers, seen[d] = true, true
			cur = sectionFor(d)
			if strings.Contains(opts, "notransaction") {
				sc.NoTx = true
			}
		case "statementbegin":
			if inBlock {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrUnbalancedBlock)
			}
			flush(false)
			inBlock, blockLine = true, lineNo
		case "statementend":
			if !inBlock {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrUnbalancedBlock)
			}
			flush(true)
			inBlock = false
		case "context", "contexts":
			sc.Meta.Contexts = append(sc.Meta.Contexts, list(opts)...)
		case "label", "labels":
			sc.Meta.Labels = append(sc.Meta.Labels, list(opts)...)
		case "dbms":
			sc.Meta.DBMS = append(sc.Meta.DBMS, list(opts)...)
		case "runalways":
			sc.Meta.RunAlways = true
		case "lint":
			key, val, _ := strings.Cut(opts, "=")
			if key != "ignore" || val == "" {
				return Script{}, fmt.Errorf("line %d: %w %q (want: Lint ignore=<rule>[,<rule>])", lineNo, ErrUnknownDirective, strings.TrimSpace(m[1]+" "+m[2]))
			}
			sc.Meta.LintIgnore = append(sc.Meta.LintIgnore, list(val)...)
		case "precondition":
			pc, err := parsePrecondition(m[2])
			if err != nil {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, err)
			}
			sc.Meta.Preconditions = append(sc.Meta.Preconditions, pc)
		case "no":
			if opts == "transaction" {
				sc.NoTx = true
				continue
			}
			return Script{}, fmt.Errorf("line %d: %w %q", lineNo, ErrUnknownDirective, strings.TrimSpace(m[1]+" "+m[2]))
		default:
			return Script{}, fmt.Errorf("line %d: %w %q", lineNo, ErrUnknownDirective, m[1])
		}
	}
	if inBlock {
		return Script{}, fmt.Errorf("line %d: %w", blockLine, ErrUnbalancedBlock)
	}
	flush(false)
	if lx.Oracle || lx.SQLPL {
		for _, section := range []Section{sc.Up, sc.Down} {
			for _, chunk := range section.chunks {
				if err := checkCompoundChunk(chunk, lx); err != nil {
					return Script{}, err
				}
			}
		}
	}
	return sc, nil
}

var oraPLSQLStart = regexp.MustCompile(`(?is)^\s*(?:BEGIN|DECLARE|CREATE\s+(?:OR\s+REPLACE\s+)?(?:(?:NON)?EDITIONABLE\s+)?(?:FUNCTION|PROCEDURE|PACKAGE|TRIGGER|TYPE\s+BODY))\b`)
var db2SQLPLStart = regexp.MustCompile(`(?is)^\s*(?:BEGIN|CREATE\s+(?:OR\s+REPLACE\s+)?(?:FUNCTION|PROCEDURE|TRIGGER|MODULE))\b`)

func startsPLSQL(sql string, lx Lexical) bool { return oraPLSQLStart.MatchString(Code(sql, lx)) }

func checkCompoundChunk(c chunk, lx Lexical) error {
	if c.verbatim {
		if lx.Oracle && startsPLSQL(c.sql, lx) && !strings.HasSuffix(strings.TrimSpace(Code(c.sql, lx)), ";") {
			return ErrPLSQLBlock
		}
		return nil
	}
	for _, stmt := range Split(c.sql, lx) {
		if lx.SQLPL && db2SQLPLStart.MatchString(Code(stmt, lx)) {
			return ErrSQLPLBlock
		}
		if lx.Oracle && startsPLSQL(stmt, lx) {
			return ErrPLSQLBlock
		}
	}
	return nil
}

// tsqlGoLine recognizes a standalone client-side batch separator, optionally
// followed by a line comment. Flag unsupported count or semicolon forms at
// load time so they cannot be confused with a single execution.
func tsqlGoLine(line string) (goLine, valid bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || !strings.EqualFold(line[:2], "GO") {
		return false, false
	}
	if len(line) > 2 && !isSpace(line[2]) && line[2] != ';' && !strings.HasPrefix(line[2:], "--") {
		return false, false
	}
	tail := strings.TrimSpace(line[2:])
	return true, tail == "" || strings.HasPrefix(tail, "--")
}

func appendTSQLBatches(out []string, sql string, lx Lexical) []string {
	var st lexState
	start, offset := 0, 0
	for line := range strings.SplitSeq(sql, "\n") {
		if st.inCode() {
			if goLine, valid := tsqlGoLine(line); goLine && valid {
				out = appendTSQLBatch(out, sql[start:offset], lx)
				start = min(offset+len(line)+1, len(sql))
			}
		}
		st = st.line(line, lx).newline()
		offset += len(line) + 1
	}
	return appendTSQLBatch(out, sql[start:], lx)
}

func appendTSQLBatch(out []string, batch string, lx Lexical) []string {
	if HasStatement(batch, lx) {
		return append(out, strings.TrimSpace(batch))
	}
	return out
}

// mayBeDirective is a cheap prefilter so the directive regex only runs on
// comment lines that contain '+', not on every line of SQL.
func mayBeDirective(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "--") && strings.IndexByte(t, '+') >= 0
}

// Most SQL already has canonical whitespace. Share the immutable SQL in that
// case, and build separate checksum text only from the first changed line.
type chunkBuilder struct {
	sql, canon strings.Builder
	changed    bool
}

func (b *chunkBuilder) appendLine(line string, inCode bool) {
	canonical := line
	if inCode {
		canonical = strings.TrimRight(line, " \t")
	}
	if !b.changed && len(canonical) != len(line) {
		b.canon.WriteString(b.sql.String())
		b.changed = true
	}
	b.sql.WriteString(line)
	b.sql.WriteByte('\n')
	if b.changed {
		b.canon.WriteString(canonical)
		b.canon.WriteByte('\n')
	}
}

func (b *chunkBuilder) chunk(verbatim bool) chunk {
	sql := b.sql.String()
	canonical := sql
	if b.changed {
		canonical = b.canon.String()
	}
	return chunk{sql: sql, verbatim: verbatim, canon: strings.Trim(canonical, "\n")}
}

// preambleLexical recognises every comment style, for checking that only
// comments precede the first Up/Down marker.
var preambleLexical = Lexical{HashComments: true}

// lineEndings strips a UTF-8 BOM and converts CRLF/CR to LF.
func lineEndings(content []byte) string {
	text := strings.TrimPrefix(string(content), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// IsTransactionControl reports whether stmt starts a, or ends the current,
// transaction (BEGIN, START TRANSACTION, COMMIT, END, ROLLBACK, ABORT). Such
// a statement inside a migration that runs in a transaction would silently
// commit part of it and break atomicity. ROLLBACK TO a savepoint is allowed.
// Keywords are read from the code under lx, so comments anywhere (BEGIN/**/)
// and literals never hide or fake one.
func IsTransactionControl(stmt string, lx Lexical) bool {
	if lx.TSQL {
		return tsqlTransactionControl.MatchString(code(stmt, lx, false))
	}
	if lx.Oracle {
		return oraTransactionControl.MatchString(code(stmt, lx, false))
	}
	if lx.SQLPL {
		return db2TransactionControl.MatchString(code(stmt, lx, false))
	}
	words := strings.FieldsFunc(strings.ToUpper(Code(stmt, lx)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "BEGIN", "COMMIT", "END", "ABORT":
		return true
	case "START":
		return len(words) > 1 && words[1] == "TRANSACTION"
	case "ROLLBACK":
		for _, w := range words[1:] {
			if w == "TO" {
				return false
			}
		}
		return true
	}
	return false
}

// T-SQL batches can contain several statements, even without semicolons.
// BEGIN / END of a regular code block is distinct from BEGIN TRANSACTION.
var tsqlTransactionControl = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_@$#])(?:COMMIT|ROLLBACK|BEGIN\s+(?:DISTRIBUTED\s+)?TRAN(?:SACTION)?|SET\s+IMPLICIT_TRANSACTIONS\s+ON)\b`)

var oraTransactionControl = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_@$#])(?:COMMIT|ROLLBACK|SET\s+TRANSACTION)\b`)
var db2TransactionControl = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_@$#])(?:COMMIT|ROLLBACK|CONNECT|DISCONNECT|SET\s+(?:CONNECTION|TRANSACTION))\b`)

// list splits a comma- or space-separated directive value (lower-cased).
func list(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}

// parsePrecondition parses "[onFail=X] expect=V <SQL>" (options in any order,
// before the SQL). The SQL keeps its original case.
func parsePrecondition(raw string) (Precondition, error) {
	pc := Precondition{OnFail: Halt}
	rest := strings.TrimSpace(raw)
	hasExpect := false
	for {
		tok, tail, _ := strings.Cut(rest, " ")
		key, val, ok := strings.Cut(tok, "=")
		if !ok {
			break
		}
		switch strings.ToLower(key) {
		case "onfail":
			switch OnFail(strings.ToUpper(val)) {
			case Halt, MarkRan, Continue:
				pc.OnFail = OnFail(strings.ToUpper(val))
			default:
				return Precondition{}, ErrBadPrecondition
			}
		case "expect":
			pc.Expect, hasExpect = val, true
		default:
			return Precondition{}, ErrBadPrecondition
		}
		rest = strings.TrimSpace(tail)
	}
	pc.SQL = rest
	if !hasExpect || pc.SQL == "" {
		return Precondition{}, ErrBadPrecondition
	}
	return pc, nil
}

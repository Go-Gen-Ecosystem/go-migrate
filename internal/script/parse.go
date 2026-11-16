package script

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
}

// Empty reports whether the section has no executable SQL under lx.
func (s Section) Empty(lx Lexical) bool {
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

// Text returns the section's source text (markers excluded), the input to
// the migration checksum.
func (s Section) Text() string {
	var b strings.Builder
	for _, c := range s.chunks {
		b.WriteString(c.sql)
	}
	return b.String()
}

// Statements returns the section's statements in execution order.
func (s Section) Statements(lx Lexical) []string {
	var out []string
	for _, c := range s.chunks {
		if c.verbatim {
			if stmt := strings.TrimSpace(c.sql); stmt != "" {
				out = append(out, strings.TrimSuffix(stmt, ";"))
			}
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

// Parse parses a migration file. Files without any Up/Down marker are treated
// as entirely belonging to direction def (Up for a single-file migration, or
// the direction encoded in a paired *.up.sql / *.down.sql file name).
func Parse(content []byte, def Direction) (Script, error) {
	// Only line endings and the BOM are normalized for execution; trailing
	// whitespace inside string literals must reach the database unchanged.
	text := string(lineEndings(content))
	var (
		sc        Script
		cur       *Section
		seen      [2]bool
		markers   bool
		inBlock   bool
		buf       strings.Builder
		blockLine int
	)
	flush := func(verbatim bool) {
		if buf.Len() > 0 && cur != nil {
			cur.chunks = append(cur.chunks, chunk{sql: buf.String(), verbatim: verbatim})
		}
		buf.Reset()
	}
	sectionFor := func(d Direction) *Section {
		if d == Down {
			return &sc.Down
		}
		return &sc.Up
	}
	cur = sectionFor(def)

	for n, line := range strings.Split(text, "\n") {
		lineNo := n + 1
		var m []string
		if mayBeDirective(line) {
			m = directive.FindStringSubmatch(line)
		}
		if m == nil {
			buf.WriteString(line)
			buf.WriteByte('\n')
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
			if !markers && HasStatement(buf.String(), preambleLexical) {
				return Script{}, fmt.Errorf("line %d: %w", lineNo, ErrContentBeforeMarker)
			}
			if !markers {
				buf.Reset() // leading comments/whitespace belong to no section
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
	return sc, nil
}

// mayBeDirective is a cheap prefilter so the directive regex only runs on
// comment lines that contain '+', not on every line of SQL.
func mayBeDirective(line string) bool {
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "--") && strings.IndexByte(t, '+') >= 0
}

var bom = []byte("\xEF\xBB\xBF")

// preambleLexical recognises every comment style, for checking that only
// comments precede the first Up/Down marker.
var preambleLexical = Lexical{HashComments: true}

// lineEndings strips a UTF-8 BOM and converts CRLF/CR to LF.
func lineEndings(content []byte) []byte {
	content = bytes.TrimPrefix(content, bom)
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
}

// Normalize strips a UTF-8 BOM, converts CRLF/CR line endings to LF and trims
// trailing whitespace from every line, so that the same file checked out on
// Windows and Linux parses and checksums identically.
func Normalize(content []byte) []byte {
	content = bytes.TrimPrefix(content, bom)
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
	lines := bytes.Split(content, []byte("\n"))
	for i, l := range lines {
		lines[i] = bytes.TrimRight(l, " \t")
	}
	return bytes.TrimRight(bytes.Join(lines, []byte("\n")), "\n")
}

// Checksum returns the hex SHA-256 of the normalized contents of all parts,
// in order. It is stable across line-ending and BOM differences.
func Checksum(parts ...[]byte) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		writeNormalized(h, p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeNormalized streams Normalize(p) into w without building the
// normalized copy: one pass, no allocations. Line breaks are buffered and
// only written before the next non-empty line, which drops trailing blank
// lines exactly as Normalize does.
func writeNormalized(w io.Writer, p []byte) {
	p = bytes.TrimPrefix(p, bom)
	pending := 0
	for len(p) > 0 {
		end := bytes.IndexAny(p, "\r\n")
		line, rest := p, []byte(nil)
		if end >= 0 {
			line, rest = p[:end], p[end+1:]
			if p[end] == '\r' && len(rest) > 0 && rest[0] == '\n' {
				rest = rest[1:]
			}
		}
		if line = bytes.TrimRight(line, " \t"); len(line) > 0 {
			for ; pending > 0; pending-- {
				_, _ = w.Write(newline)
			}
			_, _ = w.Write(line)
		}
		if end < 0 {
			break
		}
		pending++
		p = rest
	}
}

var newline = []byte{'\n'}

// IsTransactionControl reports whether stmt starts a, or ends the current,
// transaction (BEGIN, START TRANSACTION, COMMIT, END, ROLLBACK, ABORT). Such
// a statement inside a migration that runs in a transaction would silently
// commit part of it and break atomicity. ROLLBACK TO a savepoint is allowed.
func IsTransactionControl(stmt string) bool {
	words := strings.Fields(strings.ToUpper(skipLeadingComments(stmt)))
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

func skipLeadingComments(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		switch {
		case strings.HasPrefix(s, "--"), strings.HasPrefix(s, "#"):
			_, rest, ok := strings.Cut(s, "\n")
			if !ok {
				return ""
			}
			s = rest
		case strings.HasPrefix(s, "/*"):
			_, rest, ok := strings.Cut(s[2:], "*/")
			if !ok {
				return ""
			}
			s = rest
		default:
			return s
		}
	}
}

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

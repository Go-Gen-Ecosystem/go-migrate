package script

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
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

// Empty reports whether the section has no executable SQL.
func (s Section) Empty() bool {
	return len(s.Statements(Lexical{})) == 0
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
}

// directive matches "-- +migrate <word> [opts]" and "-- +goose <word> [opts]",
// case-insensitively, so files written for sql-migrate or goose load as-is.
var directive = regexp.MustCompile(`(?i)^\s*--\s*\+(?:migrate|goose)\s+(\S+)(?:\s+(.*?))?\s*$`)

// Parse parses a migration file. Files without any Up/Down marker are treated
// as entirely belonging to direction def (Up for a single-file migration, or
// the direction encoded in a paired *.up.sql / *.down.sql file name).
func Parse(content []byte, def Direction) (Script, error) {
	text := string(Normalize(content))
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
		m := directive.FindStringSubmatch(line)
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
			if !markers && hasCode(buf.String()) {
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

// hasCode reports whether text contains anything other than whitespace and
// '--' line comments.
func hasCode(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "--") {
			return true
		}
	}
	return false
}

var bom = []byte("\xEF\xBB\xBF")

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
		h.Write(Normalize(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

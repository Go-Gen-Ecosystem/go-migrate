// Package script parses migration files and splits SQL into statements.
//
// The splitter is a single-pass, O(n) lexer that understands string literals,
// quoted identifiers, line and block comments, and (per dialect) PostgreSQL
// dollar quotes, MySQL backticks, '#' comments and backslash escapes, so a ';'
// inside any of those never ends a statement.
package script

import "strings"

// Lexical describes the dialect-specific lexical rules the splitter honours.
type Lexical struct {
	// DollarQuotes enables PostgreSQL $tag$...$tag$ bodies and E'...' strings.
	DollarQuotes bool
	// Backticks treats `...` as a quoted identifier (MySQL).
	Backticks bool
	// HashComments treats '#' as a line comment (MySQL).
	HashComments bool
	// BackslashEscapes lets '\' escape the next character inside strings (MySQL default mode).
	BackslashEscapes bool
}

// Split returns the statements in sql, trimmed, without the terminating ';'.
// Statements that contain only whitespace and comments are dropped.
func Split(sql string, lx Lexical) []string {
	s := splitter{src: sql, lx: lx}
	return s.run()
}

// HasStatement reports whether sql contains at least one executable
// statement. It stops at the first non-comment token and never allocates.
func HasStatement(sql string, lx Lexical) bool {
	s := splitter{src: sql, lx: lx, probe: true}
	s.run()
	return s.hasCode
}

type splitter struct {
	src     string
	lx      Lexical
	start   int
	hasCode bool
	probe   bool // stop at the first code token (HasStatement)
	out     []string
}

func (s *splitter) run() []string {
	src := s.src
	for i := 0; i < len(src); {
		if s.probe && s.hasCode {
			return nil
		}
		c := src[i]
		switch {
		case c == ';':
			s.emit(i)
			i++
			s.start = i
		case c == '\'':
			s.hasCode = true
			backslash := s.lx.BackslashEscapes || (s.lx.DollarQuotes && isEStringPrefix(src, i))
			i = skipQuoted(src, i, '\'', backslash)
		case c == '"':
			s.hasCode = true
			i = skipQuoted(src, i, '"', s.lx.BackslashEscapes)
		case c == '`' && s.lx.Backticks:
			s.hasCode = true
			i = skipQuoted(src, i, '`', false)
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			i = skipLine(src, i)
		case c == '#' && s.lx.HashComments:
			i = skipLine(src, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipBlock(src, i)
		case c == '$' && s.lx.DollarQuotes:
			s.hasCode = true
			i = skipDollar(src, i)
		default:
			if !isSpace(c) {
				s.hasCode = true
			}
			i++
		}
	}
	s.emit(len(src))
	return s.out
}

func (s *splitter) emit(end int) {
	if s.hasCode {
		if stmt := strings.TrimSpace(s.src[s.start:end]); stmt != "" {
			s.out = append(s.out, stmt)
		}
	}
	s.hasCode = false
}

// skipQuoted returns the index just past the closing quote that matches the
// one at src[i]. A doubled quote is an escaped quote; with backslash enabled,
// '\' escapes the next byte. An unterminated literal runs to end of input.
func skipQuoted(src string, i int, q byte, backslash bool) int {
	for i++; i < len(src); i++ {
		switch src[i] {
		case '\\':
			if backslash {
				i++
			}
		case q:
			if i+1 < len(src) && src[i+1] == q {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(src)
}

func skipLine(src string, i int) int {
	if j := strings.IndexByte(src[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(src)
}

func skipBlock(src string, i int) int {
	if j := strings.Index(src[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 2
	}
	return len(src)
}

// skipDollar handles a PostgreSQL dollar-quoted body starting at src[i]=='$'.
// If src[i:] is not an opening tag ($$ or $name$), or the '$' continues an
// identifier or a positional parameter, only the '$' is consumed.
func skipDollar(src string, i int) int {
	if i > 0 && isIdentByte(src[i-1]) {
		return i + 1
	}
	j := i + 1
	for j < len(src) && isIdentByte(src[j]) {
		j++
	}
	if j >= len(src) || src[j] != '$' || (j > i+1 && isDigit(src[i+1])) {
		return i + 1
	}
	tag := src[i : j+1]
	if k := strings.Index(src[j+1:], tag); k >= 0 {
		return j + 1 + k + len(tag)
	}
	return len(src)
}

// isEStringPrefix reports whether the quote at src[i] opens a PostgreSQL
// E'...' escape string (an 'E' or 'e' that is not part of a longer word).
func isEStringPrefix(src string, i int) bool {
	if i == 0 || (src[i-1] != 'E' && src[i-1] != 'e') {
		return false
	}
	return i == 1 || !isIdentByte(src[i-2])
}

func isIdentByte(c byte) bool {
	return c == '_' || isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'z') || c >= 0x80
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

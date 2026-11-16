// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

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
	// MySQLComments makes "--" a comment only when followed by whitespace,
	// and treats /*! ... */ and /*+ ... */ as executable (MySQL rules).
	MySQLComments bool
	// TSQL enables bracketed identifiers, nested comments, and GO batches.
	TSQL bool
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
		case c == '[' && s.lx.TSQL:
			s.hasCode = true
			i = skipQuoted(src, i, ']', false)
		case c == '-' && i+1 < len(src) && src[i+1] == '-' &&
			(!s.lx.MySQLComments || i+2 >= len(src) || isSpace(src[i+2])):
			i = skipLine(src, i)
		case c == '#' && s.lx.HashComments:
			i = skipLine(src, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			if s.lx.MySQLComments && i+2 < len(src) && (src[i+2] == '!' || src[i+2] == '+') {
				s.hasCode = true // executable comment or optimizer hint
			}
			i = skipBlock(src, i, s.lx.TSQL)
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

func skipBlock(src string, i int, nested bool) int {
	if nested {
		end, _ := blockComment(true).inside(src, i+2)
		return end
	}
	if j := strings.Index(src[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 2
	}
	return len(src)
}

// skipDollar handles a PostgreSQL dollar-quoted body starting at src[i]=='$'.
// If src[i:] is not an opening tag, only the '$' is consumed.
func skipDollar(src string, i int) int {
	tag := dollarTag(src, i)
	if tag == "" {
		return i + 1
	}
	if k := strings.Index(src[i+len(tag):], tag); k >= 0 {
		return i + len(tag) + k + len(tag)
	}
	return len(src)
}

// dollarTag returns the opening tag ($$ or $name$) at src[i], or "" when the
// '$' continues an identifier or starts a positional parameter ($1).
func dollarTag(src string, i int) string {
	if i > 0 && isIdentByte(src[i-1]) {
		return ""
	}
	j := i + 1
	for j < len(src) && isIdentByte(src[j]) {
		j++
	}
	if j >= len(src) || src[j] != '$' || (j > i+1 && isDigit(src[i+1])) {
		return ""
	}
	return src[i : j+1]
}

// lexState is the lexer state at a line boundary: in code, or inside a
// string literal, quoted identifier, block comment or dollar-quoted body.
// Parse carries it from line to line, so text inside SQL (a literal that
// contains "-- +migrate Down") is never taken for a directive, and trailing
// whitespace is known to be significant or not.
type lexState struct {
	closer    string // what ends the open token; "" in code
	backslash bool   // '\' escapes the next byte inside the open quote
	escaped   bool   // the previous byte was an escaping '\'
	depth     int    // nested block comment depth, or 0 for flat comments
}

func (st lexState) inCode() bool { return st.closer == "" }

func blockComment(nested bool) lexState {
	st := lexState{closer: "*/"}
	if nested {
		st.depth = 1
	}
	return st
}

// line advances st over one line, given without its line break.
func (st lexState) line(s string, lx Lexical) lexState {
	for i := 0; i < len(s); {
		if !st.inCode() {
			i, st = st.inside(s, i)
			continue
		}
		c := s[i]
		switch {
		case c == '\'':
			st = lexState{closer: "'", backslash: lx.BackslashEscapes || (lx.DollarQuotes && isEStringPrefix(s, i))}
			i++
		case c == '"':
			st = lexState{closer: `"`, backslash: lx.BackslashEscapes}
			i++
		case c == '`' && lx.Backticks:
			st = lexState{closer: "`"}
			i++
		case c == '[' && lx.TSQL:
			st = lexState{closer: "]"}
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-' && (!lx.MySQLComments || i+2 >= len(s) || isSpace(s[i+2])),
			c == '#' && lx.HashComments:
			return st // a line comment ends with the line
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			st = blockComment(lx.TSQL)
			i += 2
		case c == '$' && lx.DollarQuotes:
			if tag := dollarTag(s, i); tag != "" {
				st = lexState{closer: tag}
				i += len(tag)
				continue
			}
			i++
		default:
			i++
		}
	}
	return st
}

// inside consumes s from i while a token is open and returns where code
// resumes (len(s) if the token continues on the next line).
func (st lexState) inside(s string, i int) (int, lexState) {
	if st.depth > 0 {
		return st.nestedComment(s, i)
	}
	if len(st.closer) > 1 { // block comment or dollar-quoted body
		if j := strings.Index(s[i:], st.closer); j >= 0 {
			return i + j + len(st.closer), lexState{}
		}
		return len(s), st
	}
	q := st.closer[0]
	for ; i < len(s); i++ {
		if st.escaped {
			st.escaped = false
			continue
		}
		switch s[i] {
		case '\\':
			st.escaped = st.backslash
		case q:
			if i+1 < len(s) && s[i+1] == q {
				i++
				continue
			}
			return i + 1, lexState{}
		}
	}
	return len(s), st
}

func (st lexState) nestedComment(s string, i int) (int, lexState) {
	for i+1 < len(s) {
		switch s[i : i+2] {
		case "/*":
			st.depth++
			i += 2
		case "*/":
			st.depth--
			i += 2
			if st.depth == 0 {
				return i, lexState{}
			}
		default:
			i++
		}
	}
	return len(s), st
}

// newline advances st over a line break (which a pending '\' escapes).
func (st lexState) newline() lexState {
	st.escaped = false
	return st
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

// Code returns sql with comments removed and string-literal contents
// blanked (quotes kept), for pattern analysis that must never match text
// inside comments or literals. Quoted identifiers are kept as written.
func Code(sql string, lx Lexical) string { return code(sql, lx, true) }

func code(sql string, lx Lexical, identifiers bool) string {
	var b strings.Builder
	b.Grow(len(sql))
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '\'':
			backslash := lx.BackslashEscapes || (lx.DollarQuotes && isEStringPrefix(sql, i))
			i = skipQuoted(sql, i, '\'', backslash)
			b.WriteString("''")
		case c == '"' || (c == '`' && lx.Backticks) || (c == '[' && lx.TSQL):
			if c == '[' {
				c = ']'
			}
			j := skipQuoted(sql, i, c, false)
			if identifiers {
				b.WriteString(sql[i:j])
			} else {
				b.WriteByte(' ')
			}
			i = j
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-' &&
			(!lx.MySQLComments || i+2 >= len(sql) || isSpace(sql[i+2])):
			i = skipLine(sql, i)
			b.WriteByte(' ')
		case c == '#' && lx.HashComments:
			i = skipLine(sql, i)
			b.WriteByte(' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i = skipBlock(sql, i, lx.TSQL)
			b.WriteByte(' ')
		case c == '$' && lx.DollarQuotes:
			j := skipDollar(sql, i)
			if j > i+1 {
				b.WriteString("$$")
			} else {
				b.WriteByte('$')
			}
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package lint

import (
	"strconv"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/script"
)

// pgHeader reads only the part of a DDL statement needed by the lock rules.
// Tokens retain their quotes, so only bare keywords can consume modifiers.
type pgHeader struct{ sql string }

func (h *pgHeader) next() string {
	token, rest := script.PostgresToken(h.sql)
	h.sql = rest
	return token
}

func (h *pgHeader) take(keyword string) bool {
	token, rest := script.PostgresToken(h.sql)
	if !strings.EqualFold(token, keyword) {
		return false
	}
	h.sql = rest
	return true
}

func (h *pgHeader) relation() string {
	name := pgName(h.next())
	for name != "" && h.take(".") {
		part := pgName(h.next())
		if part == "" {
			return ""
		}
		// Keep qualification and distinguish a quoted dot from a separator.
		name += "\x00" + part
	}
	return name
}

func pgName(token string) string {
	if token == "" {
		return ""
	}
	if token[0] == '"' && len(token) > 1 && token[len(token)-1] == '"' {
		return strings.ReplaceAll(token[1:len(token)-1], `""`, `"`)
	}
	if len(token) >= 4 && token[0]|0x20 == 'u' && token[1:3] == `&"` {
		return "\x00" + token // keep escape spelling distinct from ordinary names
	}
	c := token[0] | 0x20
	if (c < 'a' || c > 'z') && token[0] != '_' && token[0] < 0x80 {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, token)
}

func (c *stmtCtx) parsePostgresDDL(sql string, lx script.Lexical) {
	h := pgHeader{sql}
	switch strings.ToUpper(h.next()) {
	case "CREATE":
		c.parsePostgresCreate(&h)
	case "ALTER":
		if h.take("TABLE") {
			if h.take("IF") {
				h.take("EXISTS")
			}
			parenthesized := h.take("ONLY") && h.take("(")
			c.table = h.relation()
			if parenthesized && !h.take(")") {
				return
			}
			h.take("*")
			if action := Normalize(h.sql, lx); c.table != "" && action != "" {
				c.actions = splitTopLevel(action)
			}
		}
	case "DROP":
		c.concurrent = h.take("INDEX") && h.take("CONCURRENTLY")
	case "REINDEX":
		option := h.reindexConcurrently()
		switch strings.ToUpper(h.next()) {
		case "INDEX", "TABLE", "SCHEMA", "DATABASE", "SYSTEM":
			c.concurrent = option || h.take("CONCURRENTLY")
		}
	}
}

func (c *stmtCtx) parsePostgresCreate(h *pgHeader) {
	unique := h.take("UNIQUE")
	if h.take("INDEX") {
		c.parsePostgresIndex(h)
		return
	}
	if unique {
		return
	}
	switch {
	case h.take("LOCAL"), h.take("GLOBAL"):
		if !h.take("TEMP") && !h.take("TEMPORARY") {
			return
		}
	case h.take("TEMP"), h.take("TEMPORARY"), h.take("UNLOGGED"):
	}
	if h.take("TABLE") && !h.take("IF") {
		// IF NOT EXISTS can leave an existing table untouched.
		c.table = h.relation()
		c.createsTable = c.table != ""
	}
}

func (c *stmtCtx) parsePostgresIndex(h *pgHeader) {
	c.concurrent = h.take("CONCURRENTLY")
	if h.take("IF") && (!h.take("NOT") || !h.take("EXISTS")) {
		return
	}
	if !h.take("ON") {
		if pgName(h.next()) == "" || !h.take("ON") {
			return
		}
	}
	h.take("ONLY")
	c.table = h.relation()
	c.createsIndex = c.table != ""
}

func (h *pgHeader) reindexConcurrently() bool {
	if !h.take("(") {
		return false
	}
	var enabled bool
	for {
		name := pgName(h.next())
		if name == "" {
			return false
		}
		value, rest := script.PostgresToken(h.sql)
		if value == "," || value == ")" {
			value = ""
		} else {
			h.sql = rest
			if value == "+" || value == "-" {
				value += h.next()
			}
		}
		if name == "concurrently" {
			enabled = pgEnabled(value)
		}
		if h.take(")") {
			return enabled
		}
		if !h.take(",") {
			return false
		}
	}
}

func pgEnabled(value string) bool {
	if value == "" {
		return true
	}
	if n, err := strconv.ParseInt(value, 0, 64); err == nil {
		return n == 1
	}
	if len(value) > 2 && value[0]|0x20 == 'e' && value[1] == '\'' {
		value = value[1:]
	}
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	} else if strings.HasPrefix(value, "$") {
		if tag, _, ok := strings.Cut(value[1:], "$"); ok {
			delim := "$" + tag + "$"
			value = strings.TrimSuffix(strings.TrimPrefix(value, delim), delim)
		}
	}
	return strings.EqualFold(value, "true") || strings.EqualFold(value, "on")
}

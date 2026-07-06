/**
 * @file strategy_sql_test.go
 * @description Tests covering the dialect strategy SQL generators and capability
 *              flags that the existing suite leaves unexercised: unlock/insert/
 *              select SQL, transactional-DDL and concurrent-migration support,
 *              and identifier-length limits across the SQLite, PostgreSQL, and
 *              MySQL strategies, plus the context Config accessors. Assertions
 *              target dialect-specific behavior (placeholder style, DDL
 *              transactionality, identifier limits) rather than mere non-empty
 *              output.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/dialect/strategy_sql_test.go
 */

package dialect

import (
	"strings"
	"testing"
)

func TestSQLiteStrategy_SQLAndCapabilities(t *testing.T) {
	ctx := NewDatabaseDialectContext("sqlite", "3.0")
	s := NewSQLiteStrategy(ctx)

	if s.GetUnlockSQL() != "COMMIT" {
		t.Errorf("expected SQLite unlock 'COMMIT', got %q", s.GetUnlockSQL())
	}

	// SQLite uses positional '?' placeholders.
	ins := s.GetInsertSQL("users", []string{"id", "email"})
	if !strings.Contains(ins, "INSERT INTO users (id, email)") {
		t.Errorf("unexpected insert SQL: %q", ins)
	}
	if !strings.Contains(ins, "VALUES (?, ?)") {
		t.Errorf("expected '?' placeholders, got %q", ins)
	}

	sel := s.GetSelectSQL("users", []string{"id", "email"}, "id = 1")
	if !strings.Contains(sel, "SELECT id, email FROM users") {
		t.Errorf("unexpected select SQL: %q", sel)
	}
	if !strings.Contains(sel, "WHERE id = 1") {
		t.Errorf("expected WHERE clause, got %q", sel)
	}

	// No WHERE clause when the predicate is empty.
	selNoWhere := s.GetSelectSQL("users", []string{"id"}, "")
	if strings.Contains(selNoWhere, "WHERE") {
		t.Errorf("did not expect a WHERE clause, got %q", selNoWhere)
	}

	if !s.SupportsTransactionalDDL() {
		t.Error("SQLite should support transactional DDL")
	}
	if s.SupportsConcurrentMigrations() {
		t.Error("SQLite should not support concurrent migrations")
	}
	if s.GetMaxIdentifierLength() != 1000 {
		t.Errorf("expected SQLite identifier limit 1000, got %d", s.GetMaxIdentifierLength())
	}
}

func TestPostgresStrategy_SQLAndCapabilities(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "15.0")
	s := NewPostgresStrategy(ctx)

	if !strings.Contains(s.GetUnlockSQL(), "pg_advisory_unlock") {
		t.Errorf("expected pg_advisory_unlock, got %q", s.GetUnlockSQL())
	}

	// PostgreSQL uses ordinal $1, $2 placeholders.
	ins := s.GetInsertSQL("users", []string{"id", "email"})
	if !strings.Contains(ins, "VALUES ($1, $2)") {
		t.Errorf("expected ordinal placeholders, got %q", ins)
	}

	sel := s.GetSelectSQL("users", []string{"id"}, "email = 'a@b.c'")
	if !strings.Contains(sel, "WHERE email = 'a@b.c'") {
		t.Errorf("expected WHERE clause, got %q", sel)
	}

	if !s.SupportsTransactionalDDL() {
		t.Error("PostgreSQL should support transactional DDL")
	}
	if !s.SupportsConcurrentMigrations() {
		t.Error("PostgreSQL should support concurrent migrations")
	}
	if s.GetMaxIdentifierLength() != 63 {
		t.Errorf("expected PostgreSQL identifier limit 63, got %d", s.GetMaxIdentifierLength())
	}
}

func TestMySQLStrategy_SQLAndCapabilities(t *testing.T) {
	ctx := NewDatabaseDialectContext("mysql", "8.0")
	s := NewMySQLStrategy(ctx)

	if !strings.Contains(s.GetUnlockSQL(), "RELEASE_LOCK") {
		t.Errorf("expected RELEASE_LOCK, got %q", s.GetUnlockSQL())
	}

	// MySQL uses positional '?' placeholders.
	ins := s.GetInsertSQL("users", []string{"id", "email"})
	if !strings.Contains(ins, "VALUES (?, ?)") {
		t.Errorf("expected '?' placeholders, got %q", ins)
	}

	sel := s.GetSelectSQL("users", []string{"id", "email"}, "")
	if strings.Contains(sel, "WHERE") {
		t.Errorf("did not expect a WHERE clause, got %q", sel)
	}

	// MySQL DDL is not transactional.
	if s.SupportsTransactionalDDL() {
		t.Error("MySQL should not support transactional DDL")
	}
	if !s.SupportsConcurrentMigrations() {
		t.Error("MySQL should support concurrent migrations")
	}
	if s.GetMaxIdentifierLength() != 64 {
		t.Errorf("expected MySQL identifier limit 64, got %d", s.GetMaxIdentifierLength())
	}
}

func TestDatabaseDialectContext_ConfigAccessors(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "15.0")

	// A missing key must report absence.
	if _, ok := ctx.GetConfig("statement_timeout"); ok {
		t.Error("did not expect statement_timeout to exist before it is set")
	}

	ctx.SetConfig("statement_timeout", "30s")
	value, ok := ctx.GetConfig("statement_timeout")
	if !ok {
		t.Fatal("expected statement_timeout to exist after SetConfig")
	}
	if value != "30s" {
		t.Errorf("expected statement_timeout '30s', got %v", value)
	}
}

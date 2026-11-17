// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/config"
	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

const sqlServerFixtureEnv = "GOMIGRATE_TEST_SQLSERVER_DSN"

func sqlServerTarget() target {
	return target{
		dialect: dialect.SQLServer,
		open:    openSQLServer,
		failing: "CREATE TABLE half (id INT);\nGO\nINSERT INTO nope VALUES (1);",
		special: map[string]string{
			"5_proc.sql": `-- +migrate Up
CREATE TABLE audit ([n;x]]y] INT, s NVARCHAR(30));
GO
CREATE PROCEDURE write_audit AS
BEGIN
    /* outer /* nested */ still comment */
    SET NOCOUNT ON;
    DECLARE @n INT = 1;
    INSERT INTO audit VALUES (@n, N'it''s;fine');
    INSERT INTO audit VALUES (@n + 1, N'a;b');
END;
GO
EXEC write_audit;
-- +migrate Down
SET NOCOUNT ON;
DROP PROCEDURE write_audit;
DROP TABLE audit;`,
		},
		specialCheck: "SELECT COUNT(*) FROM audit",
	}
}

// sqlServerFixtureURL accepts only the explicitly configured test server.
func sqlServerFixtureURL(t *testing.T) *url.URL {
	t.Helper()
	base := os.Getenv(sqlServerFixtureEnv)
	if base == "" {
		t.Skip(sqlServerFixtureEnv + " not set")
	}
	u, err := url.Parse(base)
	if err != nil || !strings.EqualFold(u.Scheme, "sqlserver") || u.Hostname() == "" {
		t.Fatal("invalid SQL Server test fixture URL")
	}
	return u
}

func sqlServerFixtureDB(t *testing.T, u *url.URL, database string) *sql.DB {
	t.Helper()
	q := u.Query()
	q.Set("database", database)
	dsn := *u
	dsn.RawQuery = q.Encode()
	db, name, err := config.Open(t.Context(), config.Database{DSN: dsn.String()})
	if err != nil || name != dialect.SQLServer {
		t.Fatalf("open isolated SQL Server fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openSQLServer(t *testing.T) *sql.DB {
	t.Helper()
	u := sqlServerFixtureURL(t)
	admin := sqlServerFixtureDB(t, u, "master")
	name := randName("gomigrate_")
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name+" COLLATE Latin1_General_100_CI_AI"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+name); err != nil {
			t.Errorf("drop isolated SQL Server fixture %s: %v", name, err)
		}
	})
	return sqlServerFixtureDB(t, u, name)
}

func conformSQLServerVersion(t *testing.T) {
	db := sqlServerFixtureDB(t, sqlServerFixtureURL(t), "master")
	var version, edition string
	if err := db.QueryRowContext(t.Context(), `SELECT CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)),
CAST(SERVERPROPERTY('Edition') AS nvarchar(128))`).Scan(&version, &edition); err != nil {
		t.Fatal(err)
	}
	t.Logf("SQL Server %s (%s)", version, edition)
}

func TestSQLServerScriptComposition(t *testing.T) {
	db, err := sql.Open("sqlserver", "sqlserver://localhost?database=unused&encrypt=true")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const implicitBatch = "DECLARE @n int = 1;\nSELECT @n AS [GO];"
	const explicitBatch = "CREATE PROCEDURE p AS BEGIN SELECT N'GO' AS [x]]y]; END;"
	m, err := New(db, "mssql", WithFS(files("1_batches.sql", implicitBatch+"\nGo -- separate procedure\n"+explicitBatch)))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, m.Statements(m.Migrations()[0], Up), []string{implicitBatch, explicitBatch})
	m, err = New(db, "sqlserver", WithFS(files("1_single.sql", implicitBatch)))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, m.Statements(m.Migrations()[0], Up), []string{implicitBatch})
	const batchWithProtectedGO = "/* outer /* nested */\nGO\n*/\nSELECT N'line 1\nGO\nline 3' AS [GO];"
	m, err = New(db, "sqlserver", WithFS(files("1_comments.sql", "/* intro /* nested */ ok */\n-- +migrate Up\n"+batchWithProtectedGO)))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, m.Statements(m.Migrations()[0], Up), []string{batchWithProtectedGO})
	for _, tc := range []struct {
		sql  string
		code string
	}{
		{"SELECT 1;\nGO 2", "invalid_tsql_batch"},
		{"-- +migrate StatementBegin\nSELECT 1;\nGO\n-- +migrate StatementEnd", "invalid_tsql_batch"},
		{"SELECT 1;\nBEGIN/*note*/ TRANSACTION", "transaction_control"},
		{"SELECT 1\nCOMMIT", "transaction_control"},
		{"SELECT 1;\nGO\nSET IMPLICIT_TRANSACTIONS ON;", "transaction_control"},
	} {
		_, err := New(db, "sqlserver", WithFS(files("1_invalid.sql", tc.sql)))
		if !slices.Contains(ErrorCodes(err), tc.code) {
			t.Fatalf("%q = %v (%v), want %q", tc.sql, err, ErrorCodes(err), tc.code)
		}
	}
}

func conformSQLServerLockIdentity(t *testing.T) {
	db := openSQLServer(t)
	d, _ := dialect.Lookup(dialect.SQLServer)
	ctx := t.Context()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA Sales"); err != nil {
		t.Fatal(err)
	}
	initial := dialect.Table{Schema: "Sales", Name: "History"}
	alias := dialect.Table{Schema: "sales", Name: "History"}
	held, err := d.Lock(ctx, db, initial, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release(ctx) })
	if second, err := d.Lock(ctx, db, alias, 0); !errors.Is(err, ErrLockTimeout) {
		if second != nil {
			_ = second.Release(ctx)
		}
		t.Fatalf("case alias lock = %v, want lock timeout", err)
	}
	if err := held.Release(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"Sales.History", "sales.history"} {
		m, err := New(db, "sqlserver", WithTable(table), WithBaselineOnMigrate("1"), WithFS(files("1_us.sql", "CREATE TABLE activity (id int);")))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Up(ctx); err != nil {
			t.Fatal(err)
		}
		if st, err := m.Status(ctx); err != nil || st.Current != "1" || st.Migrations[0].State != StateApplied {
			t.Fatalf("history under %s = %+v, %v", table, st, err)
		}
	}
}

func conformSQLServerDirty(t *testing.T) {
	db := openSQLServer(t)
	ctx := t.Context()
	m, err := New(db, "sqlserver", WithFS(files("1_bad.sql", "-- +migrate Up notransaction\n"+sqlServerTarget().failing)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Fatalf("Up = %v, want dirty", err)
	}
	if st, err := m.Status(ctx); err != nil || !st.Dirty {
		t.Fatalf("Status = %+v, %v", st, err)
	}
	exists, err := m.dialect.TableExists(ctx, db, dialect.Table{Name: "half"})
	if err != nil || !exists {
		t.Fatalf("nontransactional first batch must remain applied: %v, %v", exists, err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE half"); err != nil {
		t.Fatal(err)
	}
	if res, err := m.Repair(ctx, RepairTarget{Migration: "1", Action: RepairClearFailed}); err != nil || res.RemovedFailed != 1 {
		t.Fatalf("Repair = %+v, %v", res, err)
	}
}

func conformSQLServerTLS(t *testing.T) {
	u := sqlServerFixtureURL(t)
	if os.Getenv("GOMIGRATE_TEST_SQLSERVER_SELF_SIGNED") != "1" {
		t.Skip("fixture not marked as self-signed")
	}
	q := u.Query()
	for key := range q {
		if strings.EqualFold(key, "trustservercertificate") {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	db, _, err := config.Open(t.Context(), config.Database{DSN: u.String()})
	if err == nil {
		_ = db.Close()
		t.Fatal("default TLS accepted the fixture's untrusted certificate")
	}
	if password, ok := u.User.Password(); ok && password != "" && strings.Contains(err.Error(), password) {
		t.Fatal("TLS error disclosed the password")
	}
}

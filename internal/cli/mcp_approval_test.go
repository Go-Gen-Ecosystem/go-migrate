// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"modernc.org/sqlite"
)

// Have SQLite attach a real fixture on every fresh MCP connection, just as
// application code can attach a database in a connection hook.
func init() {
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
		_, query, _ := strings.Cut(dsn, "?")
		if !strings.Contains(query, "_mcp_test_attachment=") {
			return nil
		}
		q, err := url.ParseQuery(query)
		if err != nil {
			return err
		}
		alias := q.Get("_mcp_test_schema")
		if alias == "side_a" || alias == "side_b" {
			_, err = conn.ExecContext(context.Background(), "ATTACH DATABASE ? AS "+alias,
				[]driver.NamedValue{{Ordinal: 1, Value: q.Get("_mcp_test_attachment")}})
		}
		return err
	})
}

func TestMCPApprovalTargetSQLite(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	for _, scope := range []string{"database", "schema"} {
		t.Run(scope, func(t *testing.T) {
			mainA, mainB := scope+"_a.db", scope+"_b.db"
			dbA := approvalDB(t, "sqlite", mainA)
			dbB := dbA
			dsnA, dsnB := mainA, mainB
			if scope == "database" {
				dbB = approvalDB(t, "sqlite", mainB)
				execApprovalSQL(t, dbB, "CREATE TABLE seed (id INTEGER)")
			} else {
				side, err := filepath.Abs("attachment.db")
				if err != nil {
					t.Fatal(err)
				}
				dsn := "file:" + mainA + "?_mcp_test_attachment=" + url.QueryEscape(side) + "&_mcp_test_schema="
				dsnA, dsnB = dsn+"side_a", dsn+"side_b"
			}
			execApprovalSQL(t, dbA, "CREATE TABLE seed (id INTEGER)")
			a := approvalClient(t, dsnA, "main.gomigrate_history", "1")
			b := approvalClient(t, dsnB, "main.gomigrate_history", "1")
			assertTables := func(t *testing.T, db *sql.DB, want int) {
				t.Helper()
				assertApprovalCount(t, db, `SELECT count(*) FROM sqlite_schema WHERE name IN
					('gomigrate_history','gomigrate_history_lock','approved_widget')`, want)
			}
			guard := approvalGuard{a: a, b: b,
				assertRejectedApply: func(t *testing.T) { assertTables(t, dbB, 0) },
				assertRejectedDown: func(t *testing.T) {
					assertTables(t, dbB, 3)
					assertApprovalCount(t, dbB, "SELECT COUNT(*) FROM gomigrate_history", 2)
				},
				assertSuccess: func(t *testing.T) {
					assertTables(t, dbA, 2)
					assertApprovalCount(t, dbA, "SELECT COUNT(*) FROM gomigrate_history", 1)
				},
				setupRollbackTarget: scope == "database",
			}
			guard.run(t)
		})
	}
}

func TestMCPApprovalTargetPostgres(t *testing.T) {
	dsnA, dsnB := os.Getenv("GOMIGRATE_TEST_MCP_POSTGRES_A"), os.Getenv("GOMIGRATE_TEST_MCP_POSTGRES_B")
	if dsnA == "" || dsnB == "" {
		t.Skip("set GOMIGRATE_TEST_MCP_POSTGRES_A and GOMIGRATE_TEST_MCP_POSTGRES_B to private PostgreSQL databases")
	}
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	for _, scope := range []string{"database", "schema"} {
		t.Run(scope, func(t *testing.T) {
			var random [6]byte
			if _, err := rand.Read(random[:]); err != nil {
				t.Fatal(err)
			}
			prefix := "mcp_approval_" + hex.EncodeToString(random[:])
			history, schemaA, schemaB := prefix+"_history", prefix+"_a", prefix+"_b"
			makeSchema := func(db *sql.DB, schema string) {
				execApprovalSQL(t, db, "CREATE SCHEMA "+schema)
				t.Cleanup(func() {
					if _, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
						t.Error(err)
					}
				})
			}
			dbA := approvalDB(t, "pgx", dsnA)
			makeSchema(dbA, history)
			makeSchema(dbA, schemaA)
			makeSchema(dbA, schemaB)
			dbB, baseB, runtimeB := dbA, dsnA, schemaB
			if scope == "database" {
				dbB, baseB, runtimeB = approvalDB(t, "pgx", dsnB), dsnB, schemaA
				makeSchema(dbB, history)
				makeSchema(dbB, schemaA)
			}
			targetDSN := func(base, schema string) string {
				u, err := url.Parse(base)
				if err != nil {
					t.Fatal("cannot parse PostgreSQL fixture URL")
				}
				q := u.Query()
				q.Set("search_path", schema)
				u.RawQuery = q.Encode()
				return u.String()
			}
			qualified := history + ".gomigrate_history"
			a := approvalClient(t, targetDSN(dsnA, schemaA), qualified, "")
			b := approvalClient(t, targetDSN(baseB, runtimeB), qualified, "")
			assertTable := func(t *testing.T, db *sql.DB, schema, table string, want int) {
				t.Helper()
				assertApprovalCount(t, db, "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = $1 AND tablename = $2",
					want, schema, table)
			}
			guard := approvalGuard{a: a, b: b,
				assertRejectedApply: func(t *testing.T) {
					assertTable(t, dbB, history, "gomigrate_history", 0)
					assertTable(t, dbB, runtimeB, "approved_widget", 0)
				},
				beforeRollback: func(t *testing.T) {
					if scope == "schema" {
						execApprovalSQL(t, dbB, "CREATE TABLE "+schemaB+".approved_widget (id INTEGER)")
					}
				},
				assertRejectedDown: func(t *testing.T) {
					assertTable(t, dbB, runtimeB, "approved_widget", 1)
					assertApprovalCount(t, dbB, "SELECT COUNT(*) FROM "+qualified, 1)
				},
				assertSuccess: func(t *testing.T) {
					assertTable(t, dbA, schemaA, "approved_widget", 0)
					assertApprovalCount(t, dbA, "SELECT COUNT(*) FROM "+qualified, 0)
				},
				setupRollbackTarget: scope == "database",
			}
			guard.run(t)
		})
	}
}

func TestMCPApprovalTargetUnavailable(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	a := approvalClient(t, "durable.db", "", "")
	b := approvalClient(t, ":memory:", "", "")
	bound := getApproval(t, a, "plan", nil)
	assertApprovalError(t, b, "plan", nil, "approval_target_unavailable")
	assertApprovalError(t, b, "apply", approvalArgs(bound.PlanID), "plan_changed")
	assertApprovalError(t, b, "rollback", approvalArgs(bound.PlanID), "plan_changed")
}

func TestCLIApprovalRejectsChangedBaselineBeforeHistoryWrites(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	const dsn = "baseline.db"
	env := map[string]string{"GOMIGRATE_DSN": dsn}
	args := []string{"-o", "json", "--baseline-on-migrate", "1", "up"}
	out, code := runCLI(t, env, append(args, "--sql")...)
	if code != ExitOK {
		t.Fatal(out)
	}
	id := planID(t, decode(t, out))
	db := approvalDB(t, "sqlite", dsn)
	// The SQL plan stays the same, but it now includes an implicit baseline.
	execApprovalSQL(t, db, "CREATE TABLE seed (id INTEGER)")
	out, code = runCLI(t, env, append(args, "--expect-plan", id)...)
	e := decode(t, out)
	if code != ExitError || e.Error == nil || e.Error.Code != "plan_changed" {
		t.Fatalf("apply with stale approval: exit %d, %s", code, out)
	}
	assertApprovalCount(t, db, `SELECT count(*) FROM sqlite_schema WHERE name IN
		('gomigrate_history', 'approved_widget')`, 0)
	a := approvalClient(t, dsn, "", "1")
	assertSuccessfulApproval(t, a, "apply", getApproval(t, a, "plan", nil).PlanID)
	assertApprovalCount(t, db, "SELECT COUNT(*) FROM gomigrate_history", 2)
}

type approvalGuard struct {
	a, b                *mcp.ClientSession
	assertRejectedApply func(*testing.T)
	beforeRollback      func(*testing.T)
	assertRejectedDown  func(*testing.T)
	assertSuccess       func(*testing.T)
	setupRollbackTarget bool
}

func (g approvalGuard) run(t *testing.T) {
	up := getApproval(t, g.a, "plan", nil)
	if preview := getApproval(t, g.a, "apply", nil); !reflect.DeepEqual(up, preview) {
		t.Fatal("apply preview differs from MCP plan")
	}
	otherUp := getApproval(t, g.b, "plan", nil)
	assertSameApprovalSQL(t, up, otherUp)
	assertApprovalError(t, g.b, "apply", approvalArgs(up.PlanID), "plan_changed")
	g.assertRejectedApply(t)
	assertSuccessfulApproval(t, g.a, "apply", up.PlanID)
	if g.setupRollbackTarget {
		assertSuccessfulApproval(t, g.b, "apply", otherUp.PlanID)
	}
	if g.beforeRollback != nil {
		g.beforeRollback(t)
	}
	down := map[string]any{"direction": "down"}
	rollback := getApproval(t, g.a, "plan", down)
	if preview := getApproval(t, g.a, "rollback", nil); !reflect.DeepEqual(rollback, preview) {
		t.Fatal("rollback preview differs from MCP plan")
	}
	assertSameApprovalSQL(t, rollback, getApproval(t, g.b, "plan", down))
	assertApprovalError(t, g.b, "rollback", approvalArgs(rollback.PlanID), "plan_changed")
	g.assertRejectedDown(t)
	assertSuccessfulApproval(t, g.a, "rollback", rollback.PlanID)
	g.assertSuccess(t)
}

func assertSameApprovalSQL(t *testing.T, a, b planJSON) {
	t.Helper()
	if len(a.Migrations) != 1 || len(a.Migrations[0].Statements) != 1 || !reflect.DeepEqual(a.Migrations, b.Migrations) {
		t.Fatalf("expected identical single-statement plans: %+v vs %+v", a.Migrations, b.Migrations)
	}
}

func writeApprovalMigration(t *testing.T) {
	t.Helper()
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/2_approved.sql", `-- +migrate Up
CREATE TABLE approved_widget (id INTEGER);
-- +migrate Down
DROP TABLE approved_widget;
`)
}

func approvalClient(t *testing.T, dsn, table, baseline string) *mcp.ClientSession {
	t.Helper()
	g := mcpGlobals(map[string]string{"GOMIGRATE_DSN": dsn})
	g.overrides.Table, g.overrides.BaselineOnMigrate = table, baseline
	return connectMCP(t, g)
}

func getApproval(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) planJSON {
	t.Helper()
	e, isErr := call(t, cs, tool, args)
	if tool == "plan" && (isErr || !e.OK) || tool != "plan" && (!isErr || e.Error == nil || e.Error.Code != "confirmation_required") {
		t.Fatalf("%s did not return a plan: %+v", tool, e)
	}
	var p planJSON
	if err := json.Unmarshal(e.Data, &p); err != nil || p.PlanID == "" {
		t.Fatalf("%s returned no plan ID", tool)
	}
	return p
}

func assertApprovalError(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, want string) {
	t.Helper()
	e, isErr := call(t, cs, tool, args)
	if !isErr || e.Error == nil || e.Error.Code != want {
		t.Fatalf("%s returned %+v; want %s", tool, e.Error, want)
	}
}

func approvalArgs(id string) map[string]any {
	return map[string]any{"confirm": true, "plan_id": id}
}

func assertSuccessfulApproval(t *testing.T, cs *mcp.ClientSession, tool, id string) {
	t.Helper()
	if e, isErr := call(t, cs, tool, approvalArgs(id)); isErr || !e.OK {
		t.Fatalf("%s rejected its own approval: %+v", tool, e.Error)
	}
}

func approvalDB(t *testing.T, driverName, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatal("cannot open approval fixture")
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func execApprovalSQL(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), stmt); err != nil {
		t.Fatal(err)
	}
}

func assertApprovalCount(t *testing.T, db *sql.DB, stmt string, want int, args ...any) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(t.Context(), stmt, args...).Scan(&count); err != nil || count != want {
		t.Fatalf("%s: count=%d, err=%v, want %d", stmt, count, err, want)
	}
}

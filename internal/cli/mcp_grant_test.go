// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpGrantProcessSwitch = "GOMIGRATE_TEST_MCP_GRANT_PROCESS"

// This child runs the real Cobra/stdio path, rather than constructing a server.
func TestMCPOperatorGrantProcess(t *testing.T) {
	if os.Getenv(mcpGrantProcessSwitch) != "1" {
		return
	}
	i := slices.Index(os.Args, "--")
	if i < 0 || i+1 >= len(os.Args) {
		t.Fatal("missing MCP subprocess arguments")
	}
	os.Exit(Run(t.Context(), "test", os.Args[i+1:], os.Stdout, os.Stderr, envFunc(nil)))
}

func mcpGrantProcess(t *testing.T, grantFlags ...string) *mcp.ClientSession {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// t.Context is cancelled before cleanup. Let Close shut down the stdio
	// server before cancelling its command, with a deadline if it hangs.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	t.Cleanup(cancel)
	args := append([]string{"-test.run=^TestMCPOperatorGrantProcess$", "--", "--dsn", "stdio.db", "mcp"}, grantFlags...)
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(cmd.Environ(), mcpGrantProcessSwitch+"=1")
	cmd.Stderr = os.Stderr
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "operator-grant-test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("project=go-migrate owner=TestMCPOperatorGrantStdio pid=%d started=%s; cleanup closes stdin and waits via CommandTransport", cmd.Process.Pid, time.Now().UTC().Format(time.RFC3339))
	t.Cleanup(func() {
		_ = cs.Close()
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			t.Errorf("owned MCP process %d did not exit", cmd.Process.Pid)
		}
	})
	return cs
}

func TestMCPOperatorGrantStdio(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	preview := mcpGrantProcess(t)
	id := getApproval(t, preview, "plan", nil).PlanID
	assertApprovalError(t, preview, "apply", approvalArgs(id), "operator_approval_required")
	db := approvalDB(t, "sqlite", "stdio.db")
	assertApprovalCount(t, db, "SELECT count(*) FROM sqlite_schema WHERE name IN ('approved_widget','gomigrate_history','gomigrate_history_lock')", 0)
	approved := mcpGrantProcess(t, "--approve-apply", id)
	assertSuccessfulApproval(t, approved, "apply", id)
	assertApprovalCount(t, db, "SELECT count(*) FROM sqlite_schema WHERE name = 'approved_widget'", 1)
	assertApprovalCount(t, db, "SELECT count(*) FROM gomigrate_history WHERE version = '2' AND success = true", 1)
	assertApprovalError(t, approved, "apply", approvalArgs(id), "operator_approval_required")
	down := getApproval(t, preview, "plan", map[string]any{"direction": "down"})
	assertApprovalError(t, preview, "rollback", approvalArgs(down.PlanID), "operator_approval_required")
	assertSuccessfulApproval(t, mcpGrantProcess(t, "--approve-rollback", down.PlanID), "rollback", down.PlanID)
	assertApprovalCount(t, db, "SELECT count(*) FROM sqlite_schema WHERE name = 'approved_widget'", 0)
}

// An agent can obtain a valid plan ID itself. That is not operator permission.
func TestMCPDefaultCannotSelfApproveApply(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	cs := mcpClient(t, map[string]string{"GOMIGRATE_DSN": "unapproved.db"})
	p := getApproval(t, cs, "plan", nil)
	assertApprovalError(t, cs, "apply", approvalArgs(p.PlanID), "operator_approval_required")
	db := approvalDB(t, "sqlite", "unapproved.db")
	assertApprovalCount(t, db, `SELECT count(*) FROM sqlite_schema WHERE name IN
		('approved_widget', 'gomigrate_history', 'gomigrate_history_lock')`, 0)
}

func TestMCPDefaultCannotSelfApproveRollback(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	g := approvalGlobals("rollback.db", "", "")
	cs := connectMCP(t, g)
	up := getApproval(t, cs, "plan", nil)
	assertSuccessfulApproval(t, connectMCP(t, g, newMCPGrant("apply", up.PlanID, false)), "apply", up.PlanID)
	db := approvalDB(t, "sqlite", "rollback.db")
	execApprovalSQL(t, db, "INSERT INTO approved_widget VALUES (42)")
	down := getApproval(t, cs, "plan", map[string]any{"direction": "down"})
	assertApprovalError(t, cs, "rollback", approvalArgs(down.PlanID), "operator_approval_required")
	assertApprovalCount(t, db, "SELECT count(*) FROM approved_widget WHERE id = 42", 1)
	assertApprovalCount(t, db, "SELECT count(*) FROM gomigrate_history WHERE version = '2' AND success = true", 1)
	assertSuccessfulApproval(t, connectMCP(t, g, newMCPGrant("rollback", down.PlanID, false)), "rollback", down.PlanID)
	assertApprovalCount(t, db, "SELECT count(*) FROM sqlite_schema WHERE name = 'approved_widget'", 0)
	assertApprovalCount(t, db, "SELECT count(*) FROM gomigrate_history", 0)
}

func TestMCPGrantRejectsCallerEscalationWithoutSpending(t *testing.T) {
	for _, kind := range []string{"operation", "plan", "lint override"} {
		t.Run(kind, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeApprovalMigration(t)
			g := approvalGlobals("pinned.db", "", "")
			id := getApproval(t, connectMCP(t, g), "plan", nil).PlanID
			cs := connectMCP(t, g, newMCPGrant("apply", id, false))
			action, args := "apply", approvalArgs(id)
			switch kind {
			case "operation":
				action = "rollback"
			case "plan":
				replacement := "0"
				if id[len(id)-1] == '0' {
					replacement = "1"
				}
				args["plan_id"] = id[:len(id)-1] + replacement
			case "lint override":
				args["allow_lint_errors"] = true
			}
			assertApprovalError(t, cs, action, args, "operator_approval_required")
			db := approvalDB(t, "sqlite", "pinned.db")
			assertApprovalCount(t, db, "SELECT count(*) FROM sqlite_schema WHERE name IN ('approved_widget','gomigrate_history','gomigrate_history_lock')", 0)
			// A rejected caller cannot burn the operator's legitimate grant.
			assertSuccessfulApproval(t, cs, "apply", id)
			assertApprovalCount(t, db, "SELECT count(*) FROM gomigrate_history WHERE version = '2' AND success = true", 1)
		})
	}
}

func TestMCPGrantSharedAcrossClientsAndPreviewsDoNotSpend(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	g := approvalGlobals("shared.db", "", "")
	id := getApproval(t, connectMCP(t, g), "plan", nil).PlanID
	server := newMCPServer(g, "test", newMCPGrant("apply", id, false))
	a, b := connectMCPServer(t, server), connectMCPServer(t, server)
	if preview := getApproval(t, a, "apply", nil); preview.PlanID != id {
		t.Fatal("unconfirmed preview changed the plan")
	}
	getApproval(t, b, "plan", nil)
	assertSuccessfulApproval(t, a, "apply", id)
	// Reuse is denied before the changed live plan can mask a spent grant.
	assertApprovalError(t, b, "apply", approvalArgs(id), "operator_approval_required")
	db := approvalDB(t, "sqlite", "shared.db")
	assertApprovalCount(t, db, "SELECT count(*) FROM gomigrate_history", 1)
}

func TestMCPGrantSpentAfterExecutionFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	db := approvalDB(t, "sqlite", "failure.db")
	execApprovalSQL(t, db, "CREATE TABLE approved_widget (id INTEGER)")
	execApprovalSQL(t, db, "INSERT INTO approved_widget VALUES (42)")
	g := approvalGlobals("failure.db", "", "")
	id := getApproval(t, connectMCP(t, g), "plan", nil).PlanID
	cs := connectMCP(t, g, newMCPGrant("apply", id, false))
	e, isErr := call(t, cs, "apply", approvalArgs(id))
	if !isErr || e.Error == nil || e.Error.Code == "operator_approval_required" {
		t.Fatalf("expected authorized SQL execution to fail on existing table: %+v", e)
	}
	assertApprovalError(t, cs, "apply", approvalArgs(id), "operator_approval_required")
	assertApprovalCount(t, db, "SELECT count(*) FROM approved_widget WHERE id = 42", 1)
}

func TestMCPGrantPinsLintOverrideAndSpendsOnLintBlock(t *testing.T) {
	t.Chdir(t.TempDir())
	writeApprovalMigration(t)
	g := approvalGlobals("lint.db", "", "")
	id := getApproval(t, connectMCP(t, g), "plan", nil).PlanID
	assertSuccessfulApproval(t, connectMCP(t, g, newMCPGrant("apply", id, false)), "apply", id)
	write(t, "migrations/3_risky.sql", "-- +migrate Up\nALTER TABLE approved_widget ADD COLUMN tier TEXT NOT NULL;\n")
	id = getApproval(t, connectMCP(t, g), "plan", nil).PlanID
	blocked := connectMCP(t, g, newMCPGrant("apply", id, false))
	assertApprovalError(t, blocked, "apply", approvalArgs(id), "lint_blocked")
	assertApprovalError(t, blocked, "apply", approvalArgs(id), "operator_approval_required")
	allowed := connectMCP(t, g, newMCPGrant("apply", id, true))
	args := approvalArgs(id)
	args["allow_lint_errors"] = true
	if e, isErr := call(t, allowed, "apply", args); isErr || !e.OK {
		t.Fatalf("operator-authorized lint override: %+v", e)
	}
	assertApprovalError(t, allowed, "apply", args, "operator_approval_required")
}

func TestMCPGrantConcurrentConsumeHasOneWinner(t *testing.T) {
	grant := newMCPGrant("apply", "reviewed-plan", false)
	const callers = 32
	start, results := make(chan struct{}), make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			results <- grant.consume("apply", "reviewed-plan", false)
		})
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, errOperatorApprovalRequired) {
			t.Fatalf("unexpected grant error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("grant consumption winners = %d, want 1", winners)
	}
}

func TestMCPCommandRejectsAmbiguousOperatorGrants(t *testing.T) {
	for _, args := range [][]string{
		{"mcp", "--approve-apply", "reviewed-plan", "--approve-rollback", "reviewed-plan"},
		{"mcp", "--allow-lint-errors"},
		{"mcp", "--approve-rollback", "reviewed-plan", "--allow-lint-errors"},
		{"mcp", "--approve-apply", ""},
		{"mcp", "--approve-rollback", "   "},
	} {
		t.Run(args[1], func(t *testing.T) {
			if _, err := run(t, nil, args...); err == nil {
				t.Fatalf("invalid operator grant flags accepted: %v", args)
			}
		})
	}
	cmd := newMCPCmd(mcpGlobals(nil), "test")
	for _, name := range []string{"approve-apply", "approve-rollback", "allow-lint-errors"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("operator grant flag missing: %s", name)
		}
	}
}

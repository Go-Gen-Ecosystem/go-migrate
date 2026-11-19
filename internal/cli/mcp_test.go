// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpClient starts the gomigrate MCP server in memory and returns a client
// session, as an agent would see it.
func mcpClient(t *testing.T, env map[string]string) *mcp.ClientSession {
	t.Helper()
	return connectMCP(t, mcpGlobals(env))
}

func mcpGlobals(env map[string]string) *globals {
	return &globals{output: "text", logLevel: "error", logFormat: "text", stderr: &bytes.Buffer{}, getenv: envFunc(env)}
}

func connectMCP(t *testing.T, g *globals, grants ...*mcpGrant) *mcp.ClientSession {
	t.Helper()
	var grant *mcpGrant
	if len(grants) > 0 {
		grant = grants[0]
	}
	return connectMCPServer(t, newMCPServer(g, "test", grant))
}

func connectMCPServer(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool and decodes the shared envelope.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (testEnvelope, bool) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var e testEnvelope
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("%s: structured content is not the envelope: %s", name, raw)
	}
	return e, res.IsError
}

func TestMCPAgentWorkflow(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	g := mcpGlobals(map[string]string{"GOMIGRATE_DSN": "agent.db"})
	cs := connectMCP(t, g)

	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*mcp.Tool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = tool
	}
	for _, want := range []string{"status", "validate", "plan", "lint", "create_migration", "apply", "rollback"} {
		if names[want] == nil {
			t.Fatalf("tool %s not listed", want)
		}
	}
	if !names["status"].Annotations.ReadOnlyHint || !*names["apply"].Annotations.DestructiveHint {
		t.Fatal("tool annotations must tell agents which tools change the database")
	}

	// 1. The agent authors a migration; it is validated and linted at once.
	e, isErr := call(t, cs, "create_migration", map[string]any{
		"name": "create users", "up": "CREATE TABLE users (id INTEGER PRIMARY KEY);", "down": "DROP TABLE users;", "sequential": true,
	})
	if isErr || !e.OK || !strings.Contains(string(e.Data), "0001_create_users.sql") {
		t.Fatalf("create_migration = %+v", e)
	}

	// 2. apply without confirmation returns the exact SQL and a plan_id for the human to approve.
	e, isErr = call(t, cs, "apply", map[string]any{})
	if !isErr || e.Error == nil || e.Error.Code != "confirmation_required" ||
		!strings.Contains(string(e.Data), "CREATE TABLE users (id INTEGER PRIMARY KEY)") {
		t.Fatalf("unconfirmed apply = %+v", e)
	}
	approved := planID(t, e)
	// The operator pins this reviewed ID when starting a new server.
	approvedSession := connectMCP(t, g, newMCPGrant("apply", approved, false))
	if e, _ = call(t, cs, "apply", map[string]any{"confirm": true}); e.Error == nil || e.Error.Code != "confirmation_required" {
		t.Fatalf("confirm without plan_id must not run: %+v", e)
	}
	if e, _ = call(t, cs, "status", nil); !strings.Contains(string(e.Data), `"pending":1`) {
		t.Fatalf("nothing may run without an approved plan: %s", e.Data)
	}

	// 3. If the pending set changes after approval, the approved plan_id is refused.
	if e, isErr = call(t, cs, "create_migration", map[string]any{"name": "later", "up": "CREATE TABLE later (id INTEGER);",
		"down": "DROP TABLE later;", "sequential": true}); isErr {
		t.Fatalf("create = %+v", e)
	}
	if e, _ = call(t, approvedSession, "apply", map[string]any{"confirm": true, "plan_id": approved}); e.Error == nil || e.Error.Code != "plan_changed" {
		t.Fatalf("stale plan = %+v", e)
	}
	e, _ = call(t, cs, "plan", nil)
	current := planID(t, e)
	if e, isErr = call(t, connectMCP(t, g, newMCPGrant("apply", current, false)), "apply", approvalArgs(current)); isErr || !e.OK {
		t.Fatalf("confirmed apply = %+v", e)
	}

	// 4. A migration with lint errors is blocked unless explicitly allowed.
	if e, isErr = call(t, cs, "create_migration", map[string]any{
		"name": "add tier", "up": "ALTER TABLE users ADD COLUMN tier TEXT NOT NULL;", "sequential": true,
	}); isErr || !strings.Contains(string(e.Data), "add-not-null-without-default") {
		t.Fatalf("create with risky SQL must report findings: %+v", e)
	}
	e, _ = call(t, cs, "plan", nil)
	current = planID(t, e)
	if e, _ = call(t, connectMCP(t, g, newMCPGrant("apply", current, false)), "apply", approvalArgs(current)); e.Error == nil || e.Error.Code != "lint_blocked" {
		t.Fatalf("lint gate = %+v", e)
	}
	if err := os.Remove("migrations/0003_add_tier.sql"); err != nil {
		t.Fatal(err)
	}

	// 5. Agents cannot inject directives (e.g. to suppress lint); typed inputs instead.
	if e, isErr = call(t, cs, "create_migration", map[string]any{"name": "sneaky",
		"up": "-- +migrate Lint ignore=add-not-null-without-default\nSELECT 1;"}); !isErr || e.Error.Code != "directive_not_allowed" {
		t.Fatalf("directive injection = %+v", e)
	}
	if e, isErr = call(t, cs, "create_migration", map[string]any{"name": "idx", "up": "CREATE INDEX later_id ON later (id);",
		"down": "DROP INDEX later_id;", "sequential": true, "no_transaction": true}); isErr || !e.OK {
		t.Fatalf("no_transaction create = %+v", e)
	}
	if e, _ = call(t, cs, "plan", nil); !strings.Contains(string(e.Data), `"no_transaction":true`) {
		t.Fatalf("plans must show the transaction mode: %s", e.Data)
	}

	// 6. Read-only tools, argument validation and rollback confirmation.
	if e, isErr = call(t, cs, "lint", nil); isErr || !e.OK {
		t.Fatalf("lint = %+v", e)
	}
	if e, isErr = call(t, cs, "plan", map[string]any{"direction": "Down"}); !isErr || e.Error.Code != "invalid_argument" {
		t.Fatalf("unknown direction = %+v", e)
	}
	if e, isErr = call(t, cs, "rollback", map[string]any{"steps": 1, "to": "0"}); !isErr || e.Error.Code != "invalid_argument" {
		t.Fatalf("conflicting selectors = %+v", e)
	}
	e, isErr = call(t, cs, "plan", map[string]any{"direction": "down", "steps": 2})
	if isErr || !strings.Contains(string(e.Data), "DROP TABLE later") {
		t.Fatalf("plan down = %+v", e)
	}
	if e, _ = call(t, cs, "rollback", map[string]any{"steps": 2}); e.Error == nil || e.Error.Code != "confirmation_required" {
		t.Fatalf("unconfirmed rollback = %+v", e)
	}
	current = planID(t, e)
	if e, isErr = call(t, connectMCP(t, g, newMCPGrant("rollback", current, false)), "rollback", map[string]any{"steps": 2, "confirm": true, "plan_id": current}); isErr || !e.OK {
		t.Fatalf("confirmed rollback = %+v", e)
	}
	if e, isErr = call(t, cs, "validate", nil); isErr || !e.OK {
		t.Fatalf("validate = %+v", e)
	}

	// 7. Errors carry stable codes.
	if e, isErr = call(t, cs, "rollback", map[string]any{"to_tag": "nope"}); !isErr || e.Error.Code != "tag_not_found" {
		t.Fatalf("rollback to unknown tag = %+v", e)
	}
	if e, isErr = call(t, cs, "create_migration", map[string]any{"name": "!!!", "up": "SELECT 1;"}); !isErr || e.OK {
		t.Fatalf("invalid name must fail: %+v", e)
	}
}

// planID extracts plan_id from a plan-carrying response.
func planID(t *testing.T, e testEnvelope) string {
	t.Helper()
	var p struct {
		PlanID string `json:"plan_id"`
	}
	if err := json.Unmarshal(e.Data, &p); err != nil || p.PlanID == "" {
		t.Fatalf("no plan_id in %s", e.Data)
	}
	return p.PlanID
}

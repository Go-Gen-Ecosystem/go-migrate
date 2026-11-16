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
	g := &globals{output: "text", logLevel: "error", logFormat: "text", stderr: &bytes.Buffer{}, getenv: envFunc(env)}
	server := newMCPServer(g, "test")
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
	cs := mcpClient(t, map[string]string{"GOMIGRATE_DSN": "agent.db"})

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

	// 2. apply without confirmation returns the exact SQL for the human to approve.
	e, isErr = call(t, cs, "apply", map[string]any{})
	if !isErr || e.Error == nil || e.Error.Code != "confirmation_required" ||
		!strings.Contains(string(e.Data), "CREATE TABLE users (id INTEGER PRIMARY KEY)") {
		t.Fatalf("unconfirmed apply = %+v", e)
	}
	if e, _ = call(t, cs, "status", nil); !strings.Contains(string(e.Data), `"pending":1`) {
		t.Fatalf("nothing may run without confirmation: %s", e.Data)
	}

	// 3. Confirmed apply runs it.
	if e, isErr = call(t, cs, "apply", map[string]any{"confirm": true}); isErr || !e.OK {
		t.Fatalf("confirmed apply = %+v", e)
	}

	// 4. A migration with lint errors is blocked unless explicitly allowed.
	if e, isErr = call(t, cs, "create_migration", map[string]any{
		"name": "add tier", "up": "ALTER TABLE users ADD COLUMN tier TEXT NOT NULL;", "sequential": true,
	}); isErr || !strings.Contains(string(e.Data), "add-not-null-without-default") {
		t.Fatalf("create with risky SQL must report findings: %+v", e)
	}
	if e, _ = call(t, cs, "apply", map[string]any{"confirm": true}); e.Error == nil || e.Error.Code != "lint_blocked" {
		t.Fatalf("lint gate = %+v", e)
	}

	// 5. Read-only tools and rollback confirmation.
	if e, isErr = call(t, cs, "lint", nil); isErr || !e.OK {
		t.Fatalf("lint = %+v", e)
	}
	if e, isErr = call(t, cs, "plan", map[string]any{"direction": "down"}); isErr || !strings.Contains(string(e.Data), "DROP TABLE users") {
		t.Fatalf("plan down = %+v", e)
	}
	if e, _ = call(t, cs, "rollback", map[string]any{}); e.Error == nil || e.Error.Code != "confirmation_required" {
		t.Fatalf("unconfirmed rollback = %+v", e)
	}
	if e, isErr = call(t, cs, "rollback", map[string]any{"confirm": true}); isErr || !e.OK {
		t.Fatalf("confirmed rollback = %+v", e)
	}
	if e, isErr = call(t, cs, "validate", nil); isErr || !e.OK {
		t.Fatalf("validate = %+v", e)
	}

	// 6. Errors carry stable codes.
	if e, isErr = call(t, cs, "rollback", map[string]any{"to_tag": "nope", "confirm": true}); !isErr || e.Error.Code != "tag_not_found" {
		t.Fatalf("rollback to unknown tag = %+v", e)
	}
	if e, isErr = call(t, cs, "create_migration", map[string]any{"name": "!!!", "up": "SELECT 1;"}); !isErr || e.OK {
		t.Fatalf("invalid name must fail: %+v", e)
	}
}

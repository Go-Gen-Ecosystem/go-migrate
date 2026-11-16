// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Errors specific to the agent workflow. Their codes are part of the contract.
var (
	// errConfirmationRequired: a changing tool was called without confirm=true
	// and the plan_id of the plan the user approved; the response carries
	// that plan, its exact SQL and its plan_id.
	errConfirmationRequired = errors.New("confirmation required: show the plan to the user, then call again with confirm=true and its plan_id")
	// errLintBlocked: apply refused because pending migrations have lint errors.
	errLintBlocked = errors.New("pending migrations have lint errors; fix them or call again with allow_lint_errors=true")
	// errDirectiveNotAllowed: agent-supplied SQL may not carry -- +migrate or
	// -- +goose directives (they could suppress lint, change transaction mode
	// or add preconditions invisibly); use the typed tool inputs instead.
	errDirectiveNotAllowed = errors.New("SQL passed to create_migration may not contain -- +migrate or -- +goose directives; use the tool's typed inputs")
)

var directiveLine = regexp.MustCompile(`(?im)^\s*--\s*\+(?:migrate|goose)\b`)

const mcpInstructions = `gomigrate manages versioned database migrations (PostgreSQL, MySQL, SQLite, SQL Server).
Safe workflow: call status; author changes with create_migration; check them with lint;
call plan to get the exact SQL and its plan_id; show that plan to the user; only after the
user approves, call apply (or rollback) with confirm=true and that plan_id. If anything
changed since the plan, the call fails with plan_changed: plan again. Every tool returns
{ok, data | error{code, message}}; branch on error.code.`

func newMCPCmd(g *globals, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve gomigrate to AI agents over the Model Context Protocol (stdio)",
		Long: "mcp runs a Model Context Protocol server on stdin/stdout, using the same config,\n" +
			"flags and environment as every other command. Read-only tools: status, validate, plan,\n" +
			"lint. Changing tools (create_migration, apply, rollback) are explicit; apply and\n" +
			"rollback require confirm=true plus the plan_id of an approved plan.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return newMCPServer(g, version).Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
}

// tools binds MCP tool handlers to the CLI's configuration.
type tools struct{ g *globals }

func newMCPServer(g *globals, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "gomigrate", Version: version}, &mcp.ServerOptions{Instructions: mcpInstructions})
	t := tools{g: g}
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}
	destructive := true
	mcp.AddTool(s, &mcp.Tool{Name: "status", Annotations: readOnly,
		Description: "State of every migration (applied, pending, failed, modified, ...), current version and dirty flag."}, t.status)
	mcp.AddTool(s, &mcp.Tool{Name: "validate", Annotations: readOnly,
		Description: "Check for drift (edited applied files), missing, out-of-order and failed migrations without changing anything."}, t.validate)
	mcp.AddTool(s, &mcp.Tool{Name: "plan", Annotations: readOnly,
		Description: "Dry run: the migrations, exact SQL and plan_id that apply (direction=up) or rollback (direction=down) would execute."}, t.plan)
	mcp.AddTool(s, &mcp.Tool{Name: "lint", Annotations: readOnly,
		Description: "Static risk analysis of migration SQL: blocking locks, table rewrites, data loss, unsupported syntax. Needs no database."}, t.lint)
	mcp.AddTool(s, &mcp.Tool{Name: "create_migration", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(bool)},
		Description: "Write a new migration file with the given up and down SQL, then validate and lint it. Does not touch the database."}, t.create)
	mcp.AddTool(s, &mcp.Tool{Name: "apply", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
		Description: "Apply pending migrations (all, or up to target). Requires confirm=true and the plan_id of the plan the user approved; refuses while pending migrations have lint errors unless allow_lint_errors=true."}, t.apply)
	mcp.AddTool(s, &mcp.Tool{Name: "rollback", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
		Description: "Roll back the last steps migrations, everything above version to, or back to tag to_tag (set one). Requires confirm=true and the plan_id of the plan the user approved."}, t.rollback)
	return s
}

type noInput struct{}

type planInput struct {
	Direction string `json:"direction,omitempty" jsonschema:"up (default) or down"`
	Target    string `json:"target,omitempty" jsonschema:"up: apply up to this version; down: roll back above this version"`
	Steps     int    `json:"steps,omitempty" jsonschema:"down: number of migrations to roll back (default 1)"`
	ToTag     string `json:"to_tag,omitempty" jsonschema:"down: roll back to the migration with this tag"`
}

type createInput struct {
	Name          string `json:"name" jsonschema:"short description, e.g. add_users_email_index"`
	Up            string `json:"up" jsonschema:"SQL applied by the migration (no -- +migrate directives)"`
	Down          string `json:"down,omitempty" jsonschema:"SQL that reverts it (empty: irreversible)"`
	Sequential    bool   `json:"sequential,omitempty" jsonschema:"use the next sequence number instead of a UTC timestamp"`
	NoTransaction bool   `json:"no_transaction,omitempty" jsonschema:"run outside a transaction, e.g. for CREATE INDEX CONCURRENTLY"`
}

type applyInput struct {
	Target          string `json:"target,omitempty" jsonschema:"apply up to and including this version (default: all pending)"`
	Confirm         bool   `json:"confirm,omitempty" jsonschema:"must be true; set it only after the user approved the plan"`
	PlanID          string `json:"plan_id,omitempty" jsonschema:"plan_id from the plan the user approved"`
	AllowLintErrors bool   `json:"allow_lint_errors,omitempty" jsonschema:"apply even if pending migrations have lint errors"`
}

type rollbackInput struct {
	Steps   int    `json:"steps,omitempty" jsonschema:"number of migrations to roll back (default 1)"`
	To      string `json:"to,omitempty" jsonschema:"roll back every migration above this version (0 for all)"`
	ToTag   string `json:"to_tag,omitempty" jsonschema:"roll back to the migration with this tag"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true; set it only after the user approved the plan"`
	PlanID  string `json:"plan_id,omitempty" jsonschema:"plan_id from the plan the user approved"`
}

// respond turns (data, err) into the shared envelope; IsError tells the agent
// the call did not succeed while the envelope says why.
func respond(command string, data any, err error) (*mcp.CallToolResult, envelope, error) {
	env := okEnvelope(command, data)
	if err != nil {
		env = errEnvelope(command, err)
		env.Data = data // e.g. the plan to approve, or the blocking findings
	}
	return &mcp.CallToolResult{IsError: err != nil}, env, nil
}

// withSession runs fn on a database session and responds with its result.
func (t tools) withSession(ctx context.Context, command string, fn func(context.Context, *session) (any, error)) (*mcp.CallToolResult, envelope, error) {
	var data any
	err := t.g.session(ctx, func(ctx context.Context, s *session) error {
		var err error
		data, err = fn(ctx, s)
		return err
	})
	return respond(command, data, err)
}

func (t tools) status(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, envelope, error) {
	return t.withSession(ctx, "status", func(ctx context.Context, s *session) (any, error) {
		st, err := s.m.Status(ctx)
		if err != nil {
			return nil, err
		}
		return newStatus(st), nil
	})
}

func (t tools) validate(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, envelope, error) {
	return t.withSession(ctx, "validate", func(ctx context.Context, s *session) (any, error) {
		if err := s.m.Validate(ctx); err != nil {
			return nil, err
		}
		return map[string]bool{"valid": true}, nil
	})
}

func (t tools) plan(ctx context.Context, _ *mcp.CallToolRequest, in planInput) (*mcp.CallToolResult, envelope, error) {
	return t.withSession(ctx, "plan", func(ctx context.Context, s *session) (any, error) {
		switch in.Direction {
		case "", "up":
			if in.Steps != 0 || in.ToTag != "" {
				return nil, fmt.Errorf("steps and to_tag apply to direction=down: %w", migrate.ErrInvalidArgument)
			}
			ms, err := s.m.Plan(ctx, in.Target)
			if err != nil {
				return nil, err
			}
			return newPlan(s.m, migrate.Up, ms, true), nil
		case "down":
			return planDown(ctx, s, migrate.DownTarget{Steps: in.Steps, To: in.Target, Tag: in.ToTag})
		default:
			return nil, fmt.Errorf("direction %q (want up or down): %w", in.Direction, migrate.ErrInvalidArgument)
		}
	})
}

func planDown(ctx context.Context, s *session, target migrate.DownTarget) (any, error) {
	ms, err := s.m.PlanDown(ctx, target)
	if err != nil {
		return nil, err
	}
	return newPlan(s.m, migrate.Down, ms, true), nil
}

func (t tools) lint(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, envelope, error) {
	findings, err := t.lintAll()
	return respond("lint", newFindings(findings), err)
}

// lintAll lints the configured migrations without a database connection.
func (t tools) lintAll() ([]migrate.Finding, error) {
	cfg, err := t.g.loadConfig()
	if err != nil {
		return nil, err
	}
	d, err := config.Dialect(cfg.Database)
	if err != nil {
		return nil, err
	}
	return migrate.Lint(d, migratorOptions(cfg, nil)...)
}

func (t tools) create(_ context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, envelope, error) {
	if directiveLine.MatchString(in.Up) || directiveLine.MatchString(in.Down) {
		return respond("create_migration", nil, errDirectiveNotAllowed)
	}
	cfg, err := t.g.loadConfig()
	if err != nil {
		return respond("create_migration", nil, err)
	}
	version, files, err := createMigration(cfg.Migrations.Dir, in.Name, in.Sequential, false, in.NoTransaction, in.Up, in.Down)
	if err != nil {
		return respond("create_migration", nil, err)
	}
	// Validate by loading and linting the whole set; a broken file is removed.
	findings, err := t.lintAll()
	if err != nil {
		removeAll(files)
		return respond("create_migration", nil, err)
	}
	own := make([]migrate.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Version == source.Canonical(version) {
			own = append(own, f)
		}
	}
	return respond("create_migration", map[string]any{"version": version, "files": files, "findings": newFindings(own).Findings}, nil)
}

func (t tools) apply(ctx context.Context, _ *mcp.CallToolRequest, in applyInput) (*mcp.CallToolResult, envelope, error) {
	return t.withSession(ctx, "apply", func(ctx context.Context, s *session) (any, error) {
		pending, err := s.m.Plan(ctx, in.Target)
		if err != nil {
			return nil, err
		}
		if !in.Confirm || in.PlanID == "" {
			return newPlan(s.m, migrate.Up, pending, true), errConfirmationRequired
		}
		if blocking := lintErrors(s.m.Lint(), pending); len(blocking) > 0 && !in.AllowLintErrors {
			return newFindings(blocking), errLintBlocked
		}
		var res *migrate.Result
		if in.Target != "" {
			res, err = s.m.UpTo(ctx, in.Target, migrate.ExpectPlan(in.PlanID))
		} else {
			res, err = s.m.Up(ctx, migrate.ExpectPlan(in.PlanID))
		}
		return newRun(res), err
	})
}

func (t tools) rollback(ctx context.Context, _ *mcp.CallToolRequest, in rollbackInput) (*mcp.CallToolResult, envelope, error) {
	return t.withSession(ctx, "rollback", func(ctx context.Context, s *session) (any, error) {
		target := migrate.DownTarget{Steps: in.Steps, To: in.To, Tag: in.ToTag}
		plan, err := planDown(ctx, s, target)
		if err != nil {
			return nil, err
		}
		if !in.Confirm || in.PlanID == "" {
			return plan, errConfirmationRequired
		}
		res, err := s.m.Down(ctx, target, migrate.ExpectPlan(in.PlanID))
		return newRun(res), err
	})
}

// lintErrors returns error-severity findings for migrations in plan.
func lintErrors(findings []migrate.Finding, plan []migrate.Migration) []migrate.Finding {
	inPlan := make(map[string]bool, len(plan))
	for _, m := range plan {
		inPlan[m.Label()] = true
	}
	var out []migrate.Finding
	for _, f := range findings {
		if f.Severity == "error" && (inPlan[f.Version] || f.Event != "") { // callbacks run on every apply
			out = append(out, f)
		}
	}
	return out
}

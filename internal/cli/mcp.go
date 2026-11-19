// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Errors specific to the agent workflow. Their codes are part of the contract.
var (
	// Return the preview when the caller omits its acknowledgment or plan ID.
	errConfirmationRequired     = errors.New("confirmation required: review the plan before requesting execution with confirm=true and its plan_id")
	errOperatorApprovalRequired = errors.New("operator approval required: the server needs an unused startup grant for this operation, plan and requested lint override")
	// errLintBlocked: apply refused because pending migrations have lint errors.
	errLintBlocked = errors.New("pending migrations have lint errors; fix them or obtain an apply grant that permits a lint override")
	// errDirectiveNotAllowed: agent-supplied SQL may not carry -- +migrate or
	// -- +goose directives (they could suppress lint, change transaction mode
	// or add preconditions invisibly); use the typed tool inputs instead.
	errDirectiveNotAllowed = errors.New("SQL passed to create_migration may not contain -- +migrate or -- +goose directives; use the tool's typed inputs")
)

var directiveLine = regexp.MustCompile(`(?im)^\s*--\s*\+(?:migrate|goose)\b`)

const mcpInstructions = `gomigrate manages versioned database migrations (PostgreSQL, MySQL, SQLite, SQL Server, Oracle, Db2).
Safe workflow: call status; author changes with create_migration; check them with lint;
call plan to get the SQL and target-bound plan_id for review. Only the operator who starts
the server can grant one apply or rollback attempt for that ID. The client acknowledges
the request with confirm=true and the same plan_id. Without a matching startup grant,
execution fails with operator_approval_required. If the live plan or target changed,
the call fails with plan_changed: plan again and obtain a fresh grant. Every tool returns
{ok, data | error{code, message}}; branch on error.code.`

func newMCPCmd(g *globals, version string) *cobra.Command {
	var applyID, rollbackID string
	var allowLintErrors bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve gomigrate to AI agents over the Model Context Protocol (stdio)",
		Long: "mcp runs a Model Context Protocol server on stdin/stdout, using the same config,\n" +
			"flags and environment as every other command. Read-only tools: status, validate, plan,\n" +
			"lint. Changing tools (create_migration, apply, rollback) are explicit; apply and\n" +
			"rollback require a matching one-use grant from the server operator.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var grant *mcpGrant
			switch {
			case cmd.Flags().Changed("approve-apply"):
				grant = newMCPGrant("apply", applyID, allowLintErrors)
			case cmd.Flags().Changed("approve-rollback"):
				grant = newMCPGrant("rollback", rollbackID, false)
			}
			if grant != nil && strings.TrimSpace(grant.planID) == "" {
				return fmt.Errorf("an approval requires a nonempty plan ID: %w", migrate.ErrInvalidArgument)
			}
			if allowLintErrors && applyID == "" {
				return fmt.Errorf("--allow-lint-errors requires --approve-apply: %w", migrate.ErrInvalidArgument)
			}
			return newMCPServer(g, version, grant).Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
	cmd.Flags().StringVar(&applyID, "approve-apply", "", "grant one apply attempt for this reviewed, target-bound plan ID")
	cmd.Flags().StringVar(&rollbackID, "approve-rollback", "", "grant one rollback attempt for this reviewed, target-bound plan ID")
	cmd.Flags().BoolVar(&allowLintErrors, "allow-lint-errors", false, "permit the approved apply to request a lint override")
	cmd.MarkFlagsMutuallyExclusive("approve-apply", "approve-rollback")
	return cmd
}

// A grant belongs to this server process and is shared by all its sessions.
type mcpGrant struct {
	action, planID  string
	allowLintErrors bool
	used            atomic.Bool
}

func newMCPGrant(action, planID string, allowLintErrors bool) *mcpGrant {
	return &mcpGrant{action: action, planID: planID, allowLintErrors: allowLintErrors}
}

// Spend a matching grant before opening the execution session. An error,
// cancellation or lost COMMIT reply must never make the grant reusable.
func (g *mcpGrant) consume(action, planID string, allowLintErrors bool) error {
	if g == nil || g.action != action || g.planID != planID || (allowLintErrors && !g.allowLintErrors) ||
		!g.used.CompareAndSwap(false, true) {
		return errOperatorApprovalRequired
	}
	return nil
}

// tools binds MCP tool handlers to the CLI's configuration.
type tools struct {
	g     *globals
	grant *mcpGrant
}

func newMCPServer(g *globals, version string, grant *mcpGrant) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "gomigrate", Version: version}, &mcp.ServerOptions{Instructions: mcpInstructions})
	t := tools{g: g, grant: grant}
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
		Description: "Apply pending migrations (all, or up to target). Requires confirm=true, the plan_id and a matching operator startup grant. A lint override also requires operator permission."}, t.apply)
	mcp.AddTool(s, &mcp.Tool{Name: "rollback", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
		Description: "Roll back the last steps migrations, everything above version to, or back to tag to_tag (set one). Requires confirm=true, the plan_id and a matching operator startup grant."}, t.rollback)
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
	Confirm         bool   `json:"confirm,omitempty" jsonschema:"caller acknowledgment to execute the reviewed plan"`
	PlanID          string `json:"plan_id,omitempty" jsonschema:"target-bound plan ID matching the operator's apply grant"`
	AllowLintErrors bool   `json:"allow_lint_errors,omitempty" jsonschema:"request a lint override permitted by the operator's apply grant"`
}

type rollbackInput struct {
	Steps   int    `json:"steps,omitempty" jsonschema:"number of migrations to roll back (default 1)"`
	To      string `json:"to,omitempty" jsonschema:"roll back every migration above this version (0 for all)"`
	ToTag   string `json:"to_tag,omitempty" jsonschema:"roll back to the migration with this tag"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"caller acknowledgment to execute the reviewed plan"`
	PlanID  string `json:"plan_id,omitempty" jsonschema:"target-bound plan ID matching the operator's rollback grant"`
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
			return planUp(ctx, s, in.Target)
		case "down":
			return planDown(ctx, s, migrate.DownTarget{Steps: in.Steps, To: in.Target, Tag: in.ToTag})
		default:
			return nil, fmt.Errorf("direction %q (want up or down): %w", in.Direction, migrate.ErrInvalidArgument)
		}
	})
}

func planUp(ctx context.Context, s *session, target string) (planJSON, error) {
	ms, id, err := s.m.PlanWithID(ctx, target)
	if err != nil {
		return planJSON{}, err
	}
	return newPlan(s.m, migrate.Up, ms, id, true), nil
}

func planDown(ctx context.Context, s *session, target migrate.DownTarget) (planJSON, error) {
	ms, id, err := s.m.PlanDownWithID(ctx, target)
	if err != nil {
		return planJSON{}, err
	}
	return newPlan(s.m, migrate.Down, ms, id, true), nil
}

func (t tools) lint(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, envelope, error) {
	cfg, err := t.g.loadConfig(config.ForLint)
	if err != nil {
		return respond("lint", newFindings(nil), err)
	}
	findings, err := lintMigrations(cfg)
	return respond("lint", newFindings(findings), err)
}

func (t tools) create(ctx context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, envelope, error) {
	if directiveLine.MatchString(in.Up) || directiveLine.MatchString(in.Down) {
		return respond("create_migration", nil, errDirectiveNotAllowed)
	}
	cfg, err := t.g.loadConfig(config.ForLint)
	if err != nil {
		return respond("create_migration", nil, err)
	}
	// Validate by loading and linting the whole set; a broken file is removed.
	draft := migrationDraft{name: in.Name, seq: in.Sequential, noTx: in.NoTransaction, up: in.Up, down: in.Down}
	var findings []migrate.Finding
	version, files, err := createMigration(ctx, cfg.Migrations.Dir, draft, func() error {
		var lintErr error
		findings, lintErr = lintMigrations(cfg)
		return lintErr
	})
	if err != nil {
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
	confirmed := in.Confirm && in.PlanID != ""
	if confirmed {
		if err := t.grant.consume("apply", in.PlanID, in.AllowLintErrors); err != nil {
			return respond("apply", nil, err)
		}
	}
	return t.withSession(ctx, "apply", func(ctx context.Context, s *session) (any, error) {
		pending, id, err := s.m.PlanWithID(ctx, in.Target)
		if err != nil {
			if confirmed && errors.Is(err, migrate.ErrApprovalTarget) {
				return nil, migrate.ErrPlanChanged
			}
			return nil, err
		}
		if !confirmed {
			return newPlan(s.m, migrate.Up, pending, id, true), errConfirmationRequired
		}
		if id != in.PlanID {
			return nil, migrate.ErrPlanChanged
		}
		if blocking := lintErrors(s.m.Lint(), pending); len(blocking) > 0 && !in.AllowLintErrors {
			return newFindings(blocking), errLintBlocked
		}
		var res *migrate.Result
		if in.Target != "" {
			res, err = s.m.UpTo(ctx, in.Target, migrate.ExpectTargetPlan(in.PlanID))
		} else {
			res, err = s.m.Up(ctx, migrate.ExpectTargetPlan(in.PlanID))
		}
		return newRun(res), err
	})
}

func (t tools) rollback(ctx context.Context, _ *mcp.CallToolRequest, in rollbackInput) (*mcp.CallToolResult, envelope, error) {
	confirmed := in.Confirm && in.PlanID != ""
	if confirmed {
		if err := t.grant.consume("rollback", in.PlanID, false); err != nil {
			return respond("rollback", nil, err)
		}
	}
	return t.withSession(ctx, "rollback", func(ctx context.Context, s *session) (any, error) {
		target := migrate.DownTarget{Steps: in.Steps, To: in.To, Tag: in.ToTag}
		if !confirmed {
			plan, err := planDown(ctx, s, target)
			if err != nil {
				return nil, err
			}
			return plan, errConfirmationRequired
		}
		res, err := s.m.Down(ctx, target, migrate.ExpectTargetPlan(in.PlanID))
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

// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"errors"
	"time"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

// This file defines the machine-readable contract shared by `--output json`
// and the MCP server: one set of types and builders, so the CLI and agents
// always see identical shapes. Changing a JSON name is a breaking change.

// envelope wraps every result: {ok, command, data} or {ok:false, error}.
type envelope struct {
	OK      bool       `json:"ok"`
	Command string     `json:"command"`
	Data    any        `json:"data,omitempty"`
	Error   *errorJSON `json:"error,omitempty"`
}

type errorJSON struct {
	// Code is the most specific stable code; Codes lists all that apply.
	Code      string         `json:"code"`
	Codes     []string       `json:"codes"`
	Message   string         `json:"message"`
	Migration *migrationFail `json:"migration,omitempty"`
}

type migrationFail struct {
	Version   string `json:"version"`
	Direction string `json:"direction"`
	Statement int    `json:"statement,omitempty"`
	SQL       string `json:"sql,omitempty"`
	Dirty     bool   `json:"dirty"`
}

// cliCodes are the stable codes of errors defined by the CLI and MCP layers.
var cliCodes = []struct {
	err  error
	code string
}{
	{errConfirmationRequired, "confirmation_required"},
	{errOperatorApprovalRequired, "operator_approval_required"},
	{errLintBlocked, "lint_blocked"},
	{errDirectiveNotAllowed, "directive_not_allowed"},
	{config.ErrNoDatabase, "no_database"},
	{errNoMigrationsDir, "no_migrations_dir"},
	{ErrPending, "pending"},
	{ErrLintFailed, "lint_failed"},
}

func errorCodes(err error) []string {
	for _, c := range cliCodes {
		if errors.Is(err, c.err) {
			return []string{c.code}
		}
	}
	return migrate.ErrorCodes(err)
}

func newErrorJSON(err error) *errorJSON {
	codes := errorCodes(err)
	ej := &errorJSON{Code: codes[0], Codes: codes, Message: err.Error()}
	var me *migrate.MigrationError
	if errors.As(err, &me) {
		ej.Migration = &migrationFail{Version: me.Version, Direction: me.Direction.String(),
			Statement: me.Statement, SQL: me.SQL, Dirty: me.Dirty}
	}
	return ej
}

// dataError carries a partial result alongside an error, so a failed run's
// JSON envelope still reports what completed (e.g. migrations applied before
// the failing one).
type dataError struct {
	err  error
	data any
}

func (e dataError) Error() string { return e.err.Error() }
func (e dataError) Unwrap() error { return e.err }

// withData attaches data to a non-nil err.
func withData(err error, data any) error {
	if err == nil {
		return nil
	}
	return dataError{err: err, data: data}
}

func okEnvelope(command string, data any) envelope {
	return envelope{OK: true, Command: command, Data: data}
}

func errEnvelope(command string, err error) envelope {
	env := envelope{OK: false, Command: command, Error: newErrorJSON(err)}
	var de dataError
	if errors.As(err, &de) {
		env.Data = de.data
	}
	return env
}

// migrationJSON describes one migration in plans, results and status.
type migrationJSON struct {
	Version     string     `json:"version,omitempty"`
	Description string     `json:"description"`
	Repeatable  bool       `json:"repeatable,omitempty"`
	Source      string     `json:"source"`
	State       string     `json:"state,omitempty"`
	Checksum    string     `json:"checksum,omitempty"`
	Tag         string     `json:"tag,omitempty"`
	InstalledOn *time.Time `json:"installed_on,omitempty"`
	InstalledBy string     `json:"installed_by,omitempty"`
	ExecutionMS *int64     `json:"execution_ms,omitempty"`
	DurationMS  *int64     `json:"duration_ms,omitempty"`
	MarkedRan   bool       `json:"marked_ran,omitempty"`
	// Conditional is set when preconditions decide at run time whether the
	// migration executes, is recorded without running, or is skipped.
	Conditional bool `json:"conditional,omitempty"`
	// NoTransaction is set when the migration runs outside a transaction
	// (a partial failure leaves the database dirty).
	NoTransaction bool     `json:"no_transaction,omitempty"`
	Statements    []string `json:"statements,omitempty"`
}

func describe(m migrate.Migration) migrationJSON {
	return migrationJSON{Version: m.Version, Description: m.Description, Repeatable: m.Repeatable, Source: m.Source,
		NoTransaction: m.NoTx, Conditional: m.Conditional}
}

func (m migrationJSON) label() string {
	return migrate.Migration{Version: m.Version, Description: m.Description, Repeatable: m.Repeatable}.Label()
}

type planJSON struct {
	DryRun    bool   `json:"dry_run"`
	Direction string `json:"direction"`
	// PlanID binds an approval to execution: pass it to --expect-plan (CLI)
	// or plan_id (MCP) and the run fails with plan_changed if anything differs.
	PlanID     string          `json:"plan_id"`
	Migrations []migrationJSON `json:"migrations"`
	// Callbacks run around the migrations, even when none is pending.
	Callbacks []callbackJSON `json:"callbacks,omitempty"`
}

// callbackJSON is one SQL callback file in a plan.
type callbackJSON struct {
	Event      string   `json:"event"`
	Source     string   `json:"source"`
	Statements []string `json:"statements,omitempty"`
}

// newPlan describes a dry run; with sql each migration carries its exact statements.
func newPlan(m *migrate.Migrator, dir migrate.Direction, ms []migrate.Migration, id string, sql bool) planJSON {
	out := planJSON{DryRun: true, Direction: dir.String(), PlanID: id, Migrations: make([]migrationJSON, 0, len(ms))}
	for _, mig := range ms {
		mj := describe(mig)
		if sql {
			mj.Statements = m.Statements(mig, dir)
		}
		out.Migrations = append(out.Migrations, mj)
	}
	for _, cb := range m.Callbacks(dir) {
		cj := callbackJSON{Event: cb.Event, Source: cb.Source}
		if sql {
			cj.Statements = cb.Statements
		}
		out.Callbacks = append(out.Callbacks, cj)
	}
	return out
}

type runJSON struct {
	Direction string          `json:"direction"`
	Applied   []migrationJSON `json:"applied"`
}

func newRun(r *migrate.Result) runJSON {
	out := runJSON{Applied: []migrationJSON{}}
	if r == nil {
		return out
	}
	out.Direction = r.Direction.String()
	for _, a := range r.Applied {
		mj := describe(a.Migration)
		ms := a.Duration.Milliseconds()
		mj.DurationMS, mj.MarkedRan = &ms, a.MarkedRan
		out.Applied = append(out.Applied, mj)
	}
	return out
}

type statusJSON struct {
	Current                  string          `json:"current,omitempty"`
	Dirty                    bool            `json:"dirty"`
	CallbackRecoveryRequired bool            `json:"callback_recovery_required,omitempty"`
	Pending                  int             `json:"pending"`
	Migrations               []migrationJSON `json:"migrations"`
}

func newStatus(st *migrate.Status) statusJSON {
	out := statusJSON{Current: st.Current, Dirty: st.Dirty, CallbackRecoveryRequired: st.CallbackRecoveryRequired,
		Pending: st.Pending, Migrations: make([]migrationJSON, 0, len(st.Migrations))}
	for _, mi := range st.Migrations {
		mj := describe(mi.Migration)
		mj.State, mj.Checksum, mj.Tag, mj.InstalledBy = string(mi.State), mi.Checksum, mi.Tag, mi.InstalledBy
		if !mi.InstalledOn.IsZero() {
			on, ms := mi.InstalledOn, mi.ExecutionTime.Milliseconds()
			mj.InstalledOn, mj.ExecutionMS = &on, &ms
		}
		out.Migrations = append(out.Migrations, mj)
	}
	return out
}

type findingJSON struct {
	Rule      string `json:"rule"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Hint      string `json:"hint"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	Event     string `json:"event,omitempty"` // set for callback files
	Statement int    `json:"statement,omitempty"`
	SQL       string `json:"sql,omitempty"`
}

type findingsJSON struct {
	Findings []findingJSON `json:"findings"`
}

func newFindings(fs []migrate.Finding) findingsJSON {
	out := findingsJSON{Findings: make([]findingJSON, 0, len(fs))}
	for _, f := range fs {
		out.Findings = append(out.Findings, findingJSON(f))
	}
	return out
}

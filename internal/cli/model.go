// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"errors"
	"time"

	migrate "github.com/go-gen-ecosystem/go-migrate"
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
	{errLintBlocked, "lint_blocked"},
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

func okEnvelope(command string, data any) envelope {
	return envelope{OK: true, Command: command, Data: data}
}

func errEnvelope(command string, err error) envelope {
	return envelope{OK: false, Command: command, Error: newErrorJSON(err)}
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
	Statements  []string   `json:"statements,omitempty"`
}

func describe(m migrate.Migration) migrationJSON {
	return migrationJSON{Version: m.Version, Description: m.Description, Repeatable: m.Repeatable, Source: m.Source}
}

func (m migrationJSON) label() string {
	return migrate.Migration{Version: m.Version, Description: m.Description, Repeatable: m.Repeatable}.Label()
}

type planJSON struct {
	DryRun     bool            `json:"dry_run"`
	Direction  string          `json:"direction"`
	Migrations []migrationJSON `json:"migrations"`
}

// newPlan describes a dry run; with sql each migration carries its exact statements.
func newPlan(m *migrate.Migrator, dir migrate.Direction, ms []migrate.Migration, sql bool) planJSON {
	out := planJSON{DryRun: true, Direction: dir.String(), Migrations: make([]migrationJSON, 0, len(ms))}
	for _, mig := range ms {
		mj := describe(mig)
		if sql {
			mj.Statements = m.Statements(mig, dir)
		}
		out.Migrations = append(out.Migrations, mj)
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
	Current    string          `json:"current,omitempty"`
	Dirty      bool            `json:"dirty"`
	Pending    int             `json:"pending"`
	Migrations []migrationJSON `json:"migrations"`
}

func newStatus(st *migrate.Status) statusJSON {
	out := statusJSON{Current: st.Current, Dirty: st.Dirty, Pending: st.Pending, Migrations: make([]migrationJSON, 0, len(st.Migrations))}
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

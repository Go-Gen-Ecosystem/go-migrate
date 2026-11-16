package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	migrate "github.com/go-gen-ecosystem/go-migrate"
)

// printer writes a command's result as text or as the JSON envelope, and
// remembers the first write error ("errors are values").
type printer struct {
	w       io.Writer
	err     error
	json    bool
	command string
}

func newPrinter(cmd *cobra.Command, g *globals) *printer {
	return &printer{w: cmd.OutOrStdout(), json: g.json(), command: cmd.Name()}
}

func (p *printer) printf(format string, a ...any) {
	if p.err == nil && !p.json {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// emit writes data as the JSON envelope, or runs text in text mode.
func (p *printer) emit(data any, text func()) {
	if !p.json {
		text()
		return
	}
	if p.err == nil {
		p.err = encode(p.w, envelope{OK: true, Command: p.command, Data: data})
	}
}

// result combines the command's error with any output error.
func (p *printer) result(err error) error { return errors.Join(err, p.err) }

// envelope is the stable --output json document for every command.
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

func encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeErrorJSON(w io.Writer, command string, err error) {
	codes := migrate.ErrorCodes(err)
	ej := &errorJSON{Code: codes[0], Codes: codes, Message: err.Error()}
	var me *migrate.MigrationError
	if errors.As(err, &me) {
		ej.Migration = &migrationFail{Version: me.Version, Direction: me.Direction.String(),
			Statement: me.Statement, SQL: me.SQL, Dirty: me.Dirty}
	}
	_ = encode(w, envelope{OK: false, Command: command, Error: ej})
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

// plan renders a dry run; sql adds the exact statements of each migration.
func (p *printer) plan(s *session, dir migrate.Direction, ms []migrate.Migration, sql bool) {
	data := struct {
		DryRun     bool            `json:"dry_run"`
		Direction  string          `json:"direction"`
		Migrations []migrationJSON `json:"migrations"`
	}{DryRun: true, Direction: dir.String(), Migrations: make([]migrationJSON, 0, len(ms))}
	for _, m := range ms {
		mj := describe(m)
		if sql {
			mj.Statements = s.m.Statements(m, dir)
		}
		data.Migrations = append(data.Migrations, mj)
	}
	p.emit(data, func() {
		verb := map[migrate.Direction]string{migrate.Up: "apply", migrate.Down: "roll back"}[dir]
		if len(ms) == 0 {
			p.printf("Nothing to do.\n")
			return
		}
		p.printf("Would %s %d migration(s):\n", verb, len(ms))
		for _, mj := range data.Migrations {
			p.printf("  %s\t%s\n", label(mj), mj.Source)
			for _, stmt := range mj.Statements {
				p.printf("    %s;\n", stmt)
			}
		}
	})
}

// run renders what a run did. It is also called when the run failed part-way,
// so the user sees which migrations completed; in JSON mode a failed run is
// reported by the error envelope instead.
func (p *printer) run(verb string, r *migrate.Result, runErr error) {
	if p.json && runErr != nil {
		return
	}
	data := struct {
		Direction string          `json:"direction"`
		Applied   []migrationJSON `json:"applied"`
	}{Applied: []migrationJSON{}}
	if r != nil {
		data.Direction = r.Direction.String()
		for _, a := range r.Applied {
			mj := describe(a.Migration)
			ms := a.Duration.Milliseconds()
			mj.DurationMS, mj.MarkedRan = &ms, a.MarkedRan
			data.Applied = append(data.Applied, mj)
		}
	}
	p.emit(data, func() {
		if len(data.Applied) == 0 {
			if runErr == nil {
				p.printf("Nothing to do.\n")
			}
			return
		}
		for _, a := range r.Applied {
			note := ""
			if a.MarkedRan {
				note = " [marked ran: precondition]"
			}
			p.printf("%s %s (%s) in %s%s\n", verb, a.Label(), a.Description, a.Duration.Round(time.Millisecond), note)
		}
		p.printf("%s %d migration(s).\n", verb, len(r.Applied))
	})
}

func (p *printer) status(st *migrate.Status) {
	data := struct {
		Current    string          `json:"current,omitempty"`
		Dirty      bool            `json:"dirty"`
		Pending    int             `json:"pending"`
		Migrations []migrationJSON `json:"migrations"`
	}{Current: st.Current, Dirty: st.Dirty, Pending: st.Pending, Migrations: make([]migrationJSON, 0, len(st.Migrations))}
	for _, mi := range st.Migrations {
		mj := describe(mi.Migration)
		mj.State, mj.Checksum, mj.Tag, mj.InstalledBy = string(mi.State), mi.Checksum, mi.Tag, mi.InstalledBy
		if !mi.InstalledOn.IsZero() {
			on, ms := mi.InstalledOn, mi.ExecutionTime.Milliseconds()
			mj.InstalledOn, mj.ExecutionMS = &on, &ms
		}
		data.Migrations = append(data.Migrations, mj)
	}
	p.emit(data, func() {
		tab := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
		tw := &printer{w: tab}
		tw.printf("VERSION\tDESCRIPTION\tSTATE\tINSTALLED ON\tTAG\tSOURCE\n")
		for _, mj := range data.Migrations {
			on := ""
			if mj.InstalledOn != nil {
				on = mj.InstalledOn.Format(time.RFC3339)
			}
			tw.printf("%s\t%s\t%s\t%s\t%s\t%s\n", label(mj), mj.Description, mj.State, on, mj.Tag, mj.Source)
		}
		if tw.err == nil {
			tw.err = tab.Flush()
		}
		if p.err = tw.err; p.err != nil {
			return
		}
		current := st.Current
		if current == "" {
			current = "none"
		}
		p.printf("\nCurrent version: %s | Pending: %d | Dirty: %t\n", current, st.Pending, st.Dirty)
	})
}

func label(m migrationJSON) string {
	return migrate.Migration{Version: m.Version, Description: m.Description, Repeatable: m.Repeatable}.Label()
}

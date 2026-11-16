// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

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

// printer renders a command's result (see model.go) as text or as the JSON
// envelope, and remembers the first write error ("errors are values").
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
		p.err = encode(p.w, okEnvelope(p.command, data))
	}
}

// result combines the command's error with any output error.
func (p *printer) result(err error) error { return errors.Join(err, p.err) }

func encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeErrorJSON(w io.Writer, command string, err error) {
	_ = encode(w, errEnvelope(command, err))
}

func (p *printer) plan(s *session, dir migrate.Direction, ms []migrate.Migration, sql bool) {
	data := newPlan(s.m, dir, ms, sql)
	p.emit(data, func() {
		if len(ms) == 0 {
			p.printf("Nothing to do.\n")
			return
		}
		verb := map[migrate.Direction]string{migrate.Up: "apply", migrate.Down: "roll back"}[dir]
		p.printf("Would %s %d migration(s):\n", verb, len(ms))
		for _, mj := range data.Migrations {
			p.printf("  %s\t%s\n", mj.label(), mj.Source)
			for _, stmt := range mj.Statements {
				p.printf("    %s;\n", stmt)
			}
		}
		p.printf("Plan ID: %s (pass --expect-plan to run exactly this plan)\n", data.PlanID)
	})
}

// run renders what a run did. It is also called when the run failed part-way,
// so the user sees which migrations completed; in JSON mode a failed run is
// reported by the error envelope instead.
func (p *printer) run(verb string, r *migrate.Result, runErr error) {
	if p.json && runErr != nil {
		return
	}
	data := newRun(r)
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
	data := newStatus(st)
	p.emit(data, func() {
		tab := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
		tw := &printer{w: tab}
		tw.printf("VERSION\tDESCRIPTION\tSTATE\tINSTALLED ON\tTAG\tSOURCE\n")
		for _, mj := range data.Migrations {
			on := ""
			if mj.InstalledOn != nil {
				on = mj.InstalledOn.Format(time.RFC3339)
			}
			tw.printf("%s\t%s\t%s\t%s\t%s\t%s\n", mj.label(), mj.Description, mj.State, on, mj.Tag, mj.Source)
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

func (p *printer) findings(findings []migrate.Finding) {
	p.emit(newFindings(findings), func() {
		if len(findings) == 0 {
			p.printf("No findings.\n")
			return
		}
		for _, f := range findings {
			where := f.Source
			if f.Statement > 0 {
				where = fmt.Sprintf("%s statement %d", f.Source, f.Statement)
			}
			p.printf("%s [%s] %s: %s\n    hint: %s\n", where, f.Severity, f.Rule, f.Message, f.Hint)
		}
		p.printf("%d finding(s).\n", len(findings))
	})
}

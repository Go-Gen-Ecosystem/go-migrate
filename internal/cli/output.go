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

// printer writes user-facing output and remembers the first write error
// ("errors are values"), so a closed pipe is reported once instead of being
// silently ignored at every call site.
type printer struct {
	w   io.Writer
	err error
}

func newPrinter(cmd *cobra.Command) *printer { return &printer{w: cmd.OutOrStdout()} }

func (p *printer) printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// result combines the command's error with any output error.
func (p *printer) result(err error) error { return errors.Join(err, p.err) }

func (p *printer) plan(verb string, ms []migrate.Migration) {
	if len(ms) == 0 {
		p.printf("Nothing to do.\n")
		return
	}
	p.printf("Would %s %d migration(s):\n", verb, len(ms))
	for _, m := range ms {
		p.printf("  %s\t%s\n", label(m), m.Source)
	}
}

// run prints what a (possibly partial) run did; it is called even when the
// run failed part-way, so the user sees which migrations did complete.
func (p *printer) run(verb string, r *migrate.Result) {
	if r == nil || len(r.Applied) == 0 {
		p.printf("Nothing to do.\n")
		return
	}
	for _, a := range r.Applied {
		p.printf("%s %s (%s) in %s\n", verb, label(a.Migration), a.Description, a.Duration.Round(time.Millisecond))
	}
	p.printf("%s %d migration(s).\n", verb, len(r.Applied))
}

func (p *printer) status(st *migrate.Status, asJSON bool) {
	if p.err != nil {
		return
	}
	if asJSON {
		enc := json.NewEncoder(p.w)
		enc.SetIndent("", "  ")
		p.err = enc.Encode(st)
		return
	}
	tab := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
	tw := &printer{w: tab}
	tw.printf("VERSION\tDESCRIPTION\tSTATE\tINSTALLED ON\tSOURCE\n")
	for _, mi := range st.Migrations {
		on := ""
		if !mi.InstalledOn.IsZero() {
			on = mi.InstalledOn.Format(time.RFC3339)
		}
		tw.printf("%s\t%s\t%s\t%s\t%s\n", label(mi.Migration), mi.Description, mi.State, on, mi.Source)
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
}

func label(m migrate.Migration) string {
	if m.Repeatable {
		return "R__" + m.Description
	}
	return m.Version
}

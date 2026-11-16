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
		p.printf("  %s\t%s\n", m.Label(), m.Source)
	}
}

// run prints what a run did. It is also called when the run failed part-way,
// so the user sees which migrations did complete; "Nothing to do." is only
// printed for a successful run.
func (p *printer) run(verb string, r *migrate.Result, runErr error) {
	if r == nil || len(r.Applied) == 0 {
		if runErr == nil {
			p.printf("Nothing to do.\n")
		}
		return
	}
	for _, a := range r.Applied {
		p.printf("%s %s (%s) in %s\n", verb, a.Label(), a.Description, a.Duration.Round(time.Millisecond))
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
		p.err = enc.Encode(newStatusJSON(st))
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
		tw.printf("%s\t%s\t%s\t%s\t%s\n", mi.Label(), mi.Description, mi.State, on, mi.Source)
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

// statusJSON is the stable `status --json` contract: snake_case keys,
// durations in milliseconds, and absent fields omitted rather than zero.
type statusJSON struct {
	Current    string          `json:"current,omitempty"`
	Dirty      bool            `json:"dirty"`
	Pending    int             `json:"pending"`
	Migrations []migrationJSON `json:"migrations"`
}

type migrationJSON struct {
	Version     string     `json:"version,omitempty"`
	Description string     `json:"description"`
	Repeatable  bool       `json:"repeatable,omitempty"`
	State       string     `json:"state"`
	Source      string     `json:"source"`
	Checksum    string     `json:"checksum,omitempty"`
	InstalledOn *time.Time `json:"installed_on,omitempty"`
	InstalledBy string     `json:"installed_by,omitempty"`
	ExecutionMS *int64     `json:"execution_ms,omitempty"`
}

func newStatusJSON(st *migrate.Status) statusJSON {
	out := statusJSON{Current: st.Current, Dirty: st.Dirty, Pending: st.Pending, Migrations: make([]migrationJSON, 0, len(st.Migrations))}
	for _, mi := range st.Migrations {
		mj := migrationJSON{
			Version: mi.Version, Description: mi.Description, Repeatable: mi.Repeatable,
			State: string(mi.State), Source: mi.Source, Checksum: mi.Checksum, InstalledBy: mi.InstalledBy,
		}
		if !mi.InstalledOn.IsZero() {
			on, ms := mi.InstalledOn, mi.ExecutionTime.Milliseconds()
			mj.InstalledOn, mj.ExecutionMS = &on, &ms
		}
		out.Migrations = append(out.Migrations, mj)
	}
	return out
}

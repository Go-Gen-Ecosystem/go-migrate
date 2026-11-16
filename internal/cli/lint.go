// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/spf13/cobra"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

// ExitLint is the exit code when lint findings reach the --fail-on level.
const ExitLint = 3

// ErrLintFailed is returned by lint when findings reach the --fail-on level.
var ErrLintFailed = errors.New("lint findings at or above the --fail-on level")

// severityRank orders severities; a threshold of 0 ("never") never fails.
var severityRank = map[string]int{"never": 0, "info": 1, "warning": 2, "error": 3}

// githubLevel maps severities to GitHub Actions annotation commands.
var githubLevel = map[string]string{"error": "error", "warning": "warning", "info": "notice"}

func newLintCmd(g *globals) *cobra.Command {
	var failOn string
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Find risky operations (locks, rewrites, data loss, unsupported SQL) without a database",
		Long: "lint analyzes every SQL migration for the configured dialect without connecting to a database.\n" +
			"Use -o github in GitHub Actions for inline PR annotations, or -o json for agents.\n" +
			"Suppress a rule in a file with: -- +migrate Lint ignore=<rule>[,<rule>]\n" +
			"Rules: " + strings.Join(migrate.LintRules(), ", "),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			threshold, ok := severityRank[failOn]
			if !ok {
				return fmt.Errorf("invalid --fail-on %q (error, warning, info, never)", failOn)
			}
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			dialectName, err := config.Dialect(cfg.Database)
			if err != nil {
				return err
			}
			findings, err := migrate.Lint(dialectName, migratorOptions(cfg, nil)...)
			if err != nil {
				return err
			}
			p := newPrinter(cmd, g)
			switch strings.ToLower(g.output) {
			case "github":
				p.json = false
				writeGitHub(p, cfg.Migrations.Dir, findings)
			case "text", "json":
				writeFindings(p, findings)
			default:
				return fmt.Errorf("invalid --output %q for lint (text, json, github)", g.output)
			}
			for _, f := range findings {
				if threshold > 0 && severityRank[f.Severity] >= threshold {
					return p.result(fmt.Errorf("%w: %s", ErrLintFailed, failOn))
				}
			}
			return p.result(nil)
		},
	}
	cmd.Flags().StringVar(&failOn, "fail-on", "error", "exit with code 3 when a finding has at least this severity: error | warning | info | never")
	return cmd
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

func writeFindings(p *printer, findings []migrate.Finding) {
	data := struct {
		Findings []findingJSON `json:"findings"`
	}{Findings: make([]findingJSON, 0, len(findings))}
	for _, f := range findings {
		data.Findings = append(data.Findings, findingJSON(f))
	}
	p.emit(data, func() {
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

// writeGitHub prints GitHub Actions workflow commands, which render as
// inline annotations on the pull request's changed files.
func writeGitHub(p *printer, dir string, findings []migrate.Finding) {
	for _, f := range findings {
		file, _, _ := strings.Cut(f.Source, " | ") // first file of an up/down pair
		p.printf("::%s file=%s,title=%s::%s\n", githubLevel[f.Severity],
			ghProperty(path.Join(dir, file)), ghProperty("gomigrate "+f.Rule), ghData(f.Message+" Hint: "+f.Hint))
	}
}

// ghData and ghProperty escape values as GitHub's workflow command parser requires.
func ghData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func ghProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

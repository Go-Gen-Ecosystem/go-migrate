// Package cli implements the gomigrate command line.
//
// Every command has two renderings of the same result: human text (default)
// and a JSON envelope (--output json) with a stable schema and stable error
// codes, so CI pipelines and AI agents act on structure, never on prose.
// Progress logs go to stderr through halolog in both modes.
package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-gen-ecosystem/halolog"
	jsonfmt "github.com/go-gen-ecosystem/halolog/adapters/formatters/json"
	textfmt "github.com/go-gen-ecosystem/halolog/adapters/formatters/text"
	"github.com/go-gen-ecosystem/halolog/adapters/outputs/console"
	"github.com/go-gen-ecosystem/halolog/core"
	"github.com/go-gen-ecosystem/halolog/types"
	"github.com/spf13/cobra"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitPending = 2 // status --fail-on-pending found pending migrations
)

// ErrPending is returned by `status --fail-on-pending` when migrations are pending.
var ErrPending = errors.New("pending migrations")

// globals holds the persistent flags shared by every command.
type globals struct {
	configPath string
	overrides  config.Overrides
	logLevel   string
	logFormat  string
	output     string
	stderr     io.Writer
	getenv     func(string) (string, bool)
}

func (g *globals) json() bool { return strings.EqualFold(g.output, "json") }

// Run executes gomigrate with args and returns the process exit code. In JSON
// mode errors are reported as a JSON envelope on stdout as well.
func Run(ctx context.Context, version string, args []string, stdout, stderr io.Writer, getenv func(string) (string, bool)) int {
	root, g := newRoot(version, stdout, stderr, getenv)
	root.SetArgs(args)
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		return ExitOK
	}
	pending := errors.Is(err, ErrPending)
	switch {
	case g.json() && !pending: // with --fail-on-pending the status is already on stdout
		writeErrorJSON(stdout, cmd.Name(), err)
	case !g.json():
		_, _ = fmt.Fprintln(stderr, "Error:", err)
	}
	if pending {
		return ExitPending
	}
	return ExitError
}

// newRoot builds the command tree with its environment passed in, so tests
// need no global state.
func newRoot(version string, stdout, stderr io.Writer, getenv func(string) (string, bool)) (*cobra.Command, *globals) {
	g := &globals{stderr: stderr, getenv: getenv}
	root := &cobra.Command{
		Use:           "gomigrate",
		Short:         "Versioned SQL and Go migrations for PostgreSQL, MySQL and SQLite",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	pf := root.PersistentFlags()
	pf.StringVarP(&g.configPath, "config", "c", "", "config file (default "+config.DefaultPath+")")
	pf.StringVar(&g.overrides.DSN, "dsn", "", "database DSN or URL (prefer GOMIGRATE_DSN: command lines are visible to other processes)")
	pf.StringVar(&g.overrides.Driver, "driver", "", "postgres | mysql | sqlite (inferred from the DSN when omitted)")
	pf.StringVar(&g.overrides.Dir, "dir", "", "migrations directory")
	pf.StringVar(&g.overrides.Table, "table", "", "history table (name or schema.name)")
	pf.StringSliceVar(&g.overrides.Contexts, "context", nil, "active contexts for Context-targeted migrations (repeatable)")
	pf.StringSliceVar(&g.overrides.Labels, "label", nil, "active labels for Labels-targeted migrations (repeatable)")
	pf.StringToStringVar(&g.overrides.Placeholders, "placeholder", nil, "placeholder value name=value for ${name} in SQL (repeatable)")
	pf.StringVar(&g.overrides.BaselineOnMigrate, "baseline-on-migrate", "", "baseline an existing, untracked schema at this version before up")
	pf.StringVarP(&g.output, "output", "o", "text", "text | json (stable schema for CI and agents)")
	pf.StringVar(&g.logLevel, "log-level", "info", "debug | info | warn | error")
	pf.StringVar(&g.logFormat, "log-format", "text", "text | json (logs go to stderr)")

	root.AddCommand(
		newInitCmd(g), newCreateCmd(g), newUpCmd(g), newDownCmd(g), newRedoCmd(g),
		newStatusCmd(g), newValidateCmd(g), newRepairCmd(g), newMarkAppliedCmd(g),
		newBaselineCmd(g), newTagCmd(g),
	)
	return root, g
}

// session is an opened database plus a Migrator for one command.
type session struct {
	db  *sql.DB
	m   *migrate.Migrator
	cfg config.Config
}

func (s *session) Close() error { return s.db.Close() }

func (g *globals) loadConfig() (config.Config, error) {
	path, explicit := g.configPath, g.configPath != ""
	if !explicit {
		path = config.DefaultPath
	}
	cfg, err := config.Load(path, explicit)
	if err != nil {
		return config.Config{}, err
	}
	cfg.Apply(g.overrides, explicit, g.getenv)
	return cfg, nil
}

func (g *globals) open(ctx context.Context) (*session, error) {
	if out := strings.ToLower(g.output); out != "text" && out != "json" {
		return nil, fmt.Errorf("invalid --output %q (text, json)", g.output)
	}
	cfg, err := g.loadConfig()
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(cfg.Migrations.Dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("migrations directory %q not found (run `gomigrate init` or pass --dir)", cfg.Migrations.Dir)
	}
	db, dialectName, err := config.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	logger, err := g.logger()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	m, err := migrate.New(db, dialectName, migratorOptions(cfg, logger)...)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &session{db: db, m: m, cfg: cfg}, nil
}

func migratorOptions(cfg config.Config, logger halolog.Logger) []migrate.Option {
	mc := cfg.Migrations
	opts := []migrate.Option{
		migrate.WithFS(os.DirFS(mc.Dir)),
		migrate.WithLogger(logger),
		migrate.WithAllowOutOfOrder(mc.AllowOutOfOrder),
		migrate.WithAllowMissing(mc.AllowMissing),
		migrate.WithContexts(mc.Contexts...),
		migrate.WithLabels(mc.Labels...),
	}
	if mc.Table != "" {
		opts = append(opts, migrate.WithTable(mc.Table))
	}
	if mc.LockTimeout > 0 {
		opts = append(opts, migrate.WithLockWait(mc.LockTimeout))
	}
	if mc.Placeholders != nil {
		opts = append(opts, migrate.WithPlaceholders(mc.Placeholders))
	}
	if mc.BaselineOnMigrate != "" {
		opts = append(opts, migrate.WithBaselineOnMigrate(mc.BaselineOnMigrate))
	}
	return opts
}

func (g *globals) logger() (halolog.Logger, error) {
	levels := map[string]types.LogLevel{
		"debug": types.DebugLevel, "info": types.InfoLevel, "warn": types.WarnLevel, "error": types.ErrorLevel,
	}
	lvl, ok := levels[strings.ToLower(g.logLevel)]
	if !ok {
		return nil, fmt.Errorf("invalid --log-level %q (debug, info, warn, error)", g.logLevel)
	}
	var formatter types.Formatter
	switch strings.ToLower(g.logFormat) {
	case "text":
		formatter = textfmt.NewTextFormatter()
	case "json":
		formatter = jsonfmt.NewJsonFormatter()
	default:
		return nil, fmt.Errorf("invalid --log-format %q (text, json)", g.logFormat)
	}
	return core.New().Component("gomigrate").Level(lvl).Adapter(console.NewWithWriter(g.stderr, formatter)).Build()
}

// withSession opens a session, runs fn, and always closes the database.
func (g *globals) withSession(cmd *cobra.Command, fn func(context.Context, *session) error) (err error) {
	ctx := cmd.Context()
	s, err := g.open(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := s.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	return fn(ctx, s)
}

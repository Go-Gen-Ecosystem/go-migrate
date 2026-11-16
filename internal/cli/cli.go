// Package cli implements the gomigrate command line.
//
// Results go to stdout (machine-readable with --json); progress logs go to
// stderr through halolog, so `gomigrate status --json | jq` stays clean.
package cli

import (
	"context"
	"database/sql"
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

// globals holds the persistent flags shared by every command.
type globals struct {
	configPath string
	overrides  config.Overrides
	logLevel   string
	logFormat  string
	stderr     io.Writer
	getenv     func(string) (string, bool)
}

// NewRoot builds the gomigrate command tree. version is injected at build time.
func NewRoot(version string) *cobra.Command {
	return newRoot(version, os.Stderr, os.LookupEnv)
}

// newRoot takes its environment explicitly so tests need no global state.
func newRoot(version string, stderr io.Writer, getenv func(string) (string, bool)) *cobra.Command {
	g := &globals{stderr: stderr, getenv: getenv}
	root := &cobra.Command{
		Use:           "gomigrate",
		Short:         "Versioned SQL and Go migrations for PostgreSQL, MySQL and SQLite",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&g.configPath, "config", "c", "", "config file (default "+config.DefaultPath+")")
	pf.StringVar(&g.overrides.DSN, "dsn", "", "database DSN or URL (overrides config and env)")
	pf.StringVar(&g.overrides.Driver, "driver", "", "postgres | mysql | sqlite (inferred from the DSN when omitted)")
	pf.StringVar(&g.overrides.Dir, "dir", "", "migrations directory")
	pf.StringVar(&g.overrides.Table, "table", "", "history table (name or schema.name)")
	pf.StringVar(&g.logLevel, "log-level", "info", "debug | info | warn | error")
	pf.StringVar(&g.logFormat, "log-format", "text", "text | json (logs go to stderr)")

	root.AddCommand(
		newInitCmd(g), newCreateCmd(g), newUpCmd(g), newDownCmd(g), newRedoCmd(g),
		newStatusCmd(g), newValidateCmd(g), newRepairCmd(g), newMarkAppliedCmd(g), newBaselineCmd(g),
	)
	return root
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
	opts := []migrate.Option{
		migrate.WithFS(os.DirFS(cfg.Migrations.Dir)),
		migrate.WithLogger(logger),
		migrate.WithAllowOutOfOrder(cfg.Migrations.AllowOutOfOrder),
		migrate.WithAllowMissing(cfg.Migrations.AllowMissing),
	}
	if cfg.Migrations.Table != "" {
		opts = append(opts, migrate.WithTable(cfg.Migrations.Table))
	}
	if cfg.Migrations.LockTimeout > 0 {
		opts = append(opts, migrate.WithLockWait(cfg.Migrations.LockTimeout))
	}
	m, err := migrate.New(db, dialectName, opts...)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &session{db: db, m: m, cfg: cfg}, nil
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

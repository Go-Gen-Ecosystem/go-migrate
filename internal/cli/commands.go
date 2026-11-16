package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

// File permissions: config may hold credentials, so it is owner-only.
const (
	configPerm    = 0o600
	dirPerm       = 0o755
	migrationPerm = 0o644
	seqWidth      = 4
	timestampFmt  = "20060102150405"
)

// ErrPending is returned by `status --fail-on-pending` when migrations are pending.
var ErrPending = errors.New("pending migrations")

func newInitCmd(g *globals) *cobra.Command {
	var driver, dsn string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config file and create the migrations directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := g.configPath
			if path == "" {
				path = config.DefaultPath
			}
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("config file %q already exists", path)
			}
			dir := g.overrides.Dir
			if dir == "" {
				dir = config.Default().Migrations.Dir
			}
			body := fmt.Sprintf(config.Template, driver, dsn, dir)
			if err := os.WriteFile(path, []byte(body), configPerm); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			if err := os.MkdirAll(dir, dirPerm); err != nil {
				return fmt.Errorf("create migrations directory: %w", err)
			}
			p := newPrinter(cmd)
			p.printf("Created %s and %s/\nNext: gomigrate create add_users_table\n", path, dir)
			return p.result(nil)
		},
	}
	cmd.Flags().StringVar(&driver, "init-driver", "sqlite", "driver written to the new config")
	cmd.Flags().StringVar(&dsn, "init-dsn", "app.db", "DSN written to the new config (use ${ENV_VAR} for secrets)")
	return cmd
}

var nameSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

func newCreateCmd(g *globals) *cobra.Command {
	var seq, pair bool
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a new migration file (timestamp version by default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := g.loadConfig()
			if err != nil {
				return err
			}
			name := strings.Trim(nameSanitizer.ReplaceAllString(strings.ToLower(args[0]), "_"), "_")
			if name == "" {
				return fmt.Errorf("invalid migration name %q", args[0])
			}
			dir := cfg.Migrations.Dir
			if err := os.MkdirAll(dir, dirPerm); err != nil {
				return err
			}
			version := time.Now().UTC().Format(timestampFmt)
			if seq {
				if version, err = nextSequence(dir); err != nil {
					return err
				}
			}
			files := map[string]string{
				version + "_" + name + ".sql": "-- +migrate Up\n\n\n-- +migrate Down\n\n",
			}
			if pair {
				files = map[string]string{
					version + "_" + name + ".up.sql":   "\n",
					version + "_" + name + ".down.sql": "\n",
				}
			}
			p := newPrinter(cmd)
			for fname, body := range files {
				path := filepath.Join(dir, fname)
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, migrationPerm)
				if err != nil {
					return fmt.Errorf("create %s: %w", path, err)
				}
				_, werr := f.WriteString(body)
				if cerr := f.Close(); werr == nil {
					werr = cerr
				}
				if werr != nil {
					return werr
				}
				p.printf("Created %s\n", path)
			}
			return p.result(nil)
		},
	}
	cmd.Flags().BoolVar(&seq, "seq", false, "use the next sequential number instead of a UTC timestamp")
	cmd.Flags().BoolVar(&pair, "pair", false, "create separate .up.sql and .down.sql files")
	return cmd
}

// seqPrefix captures the leading integer version of a migration file name.
var seqPrefix = regexp.MustCompile(`(?i)^v?(\d+)[._]`)

// nextSequence returns max(existing integer versions)+1, zero-padded. It
// only reads file names, so freshly scaffolded (still empty) migrations do
// not block creating the next one.
func nextSequence(dir string) (string, error) {
	next := 1
	err := fs.WalkDir(os.DirFS(dir), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if m := seqPrefix.FindStringSubmatch(d.Name()); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= next {
				next = n + 1
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("scan %s: %w", dir, err)
	}
	return fmt.Sprintf("%0*d", seqWidth, next), nil
}

func newUpCmd(g *globals) *cobra.Command {
	var to string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply pending migrations (all, or up to --to)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				out := newPrinter(cmd)
				if dryRun {
					plan, err := s.m.Plan(ctx, to)
					if err != nil {
						return err
					}
					out.plan("apply", plan)
					return out.result(nil)
				}
				var res *migrate.Result
				var err error
				if to != "" {
					res, err = s.m.UpTo(ctx, to)
				} else {
					res, err = s.m.Up(ctx)
				}
				out.run("Applied", res)
				return out.result(err)
			})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "apply up to and including this version")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be applied without changing anything")
	return cmd
}

func newDownCmd(g *globals) *cobra.Command {
	var steps int
	var to string
	var all, dryRun bool
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Roll back migrations (last one by default, --steps N, --to V or --all)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all {
				to = "0"
			}
			if to != "" && cmd.Flags().Changed("steps") {
				return errors.New("use either --steps or --to/--all, not both")
			}
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				out := newPrinter(cmd)
				if dryRun {
					var plan []migrate.Migration
					var err error
					if to != "" {
						plan, err = s.m.PlanDownTo(ctx, to)
					} else {
						plan, err = s.m.PlanDown(ctx, steps)
					}
					if err != nil {
						return err
					}
					out.plan("roll back", plan)
					return out.result(nil)
				}
				var res *migrate.Result
				var err error
				if to != "" {
					res, err = s.m.DownTo(ctx, to)
				} else {
					res, err = s.m.Down(ctx, steps)
				}
				out.run("Rolled back", res)
				return out.result(err)
			})
		},
	}
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "number of migrations to roll back")
	cmd.Flags().StringVar(&to, "to", "", "roll back every migration above this version")
	cmd.Flags().BoolVar(&all, "all", false, "roll back every migration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be rolled back without changing anything")
	return cmd
}

func newRedoCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "redo",
		Short: "Roll back the last migration and apply it again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				out := newPrinter(cmd)
				res, err := s.m.Down(ctx, 1)
				out.run("Rolled back", res)
				if err != nil {
					return out.result(err)
				}
				res, err = s.m.UpTo(ctx, res.Applied[0].Version)
				out.run("Applied", res)
				return out.result(err)
			})
		},
	}
}

func newStatusCmd(g *globals) *cobra.Command {
	var asJSON, failOnPending bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show every migration's state (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				st, err := s.m.Status(ctx)
				if err != nil {
					return err
				}
				out := newPrinter(cmd)
				out.status(st, asJSON)
				if out.err != nil {
					return out.err
				}
				if failOnPending && st.Pending > 0 {
					return fmt.Errorf("%w: %d", ErrPending, st.Pending)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&failOnPending, "fail-on-pending", false, "exit non-zero when migrations are pending (for CI)")
	return cmd
}

func newValidateCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check for drift, missing, out-of-order and failed migrations (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				if err := s.m.Validate(ctx); err != nil {
					return err
				}
				p := newPrinter(cmd)
				p.printf("OK: migrations are valid.\n")
				return p.result(nil)
			})
		},
	}
}

func newRepairCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Remove failed history rows and realign checksums of edited migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				res, err := s.m.Repair(ctx)
				if err != nil {
					return err
				}
				p := newPrinter(cmd)
				p.printf("Removed %d failed row(s); realigned %d checksum(s) %v\n",
					res.RemovedFailed, len(res.Realigned), res.Realigned)
				return p.result(nil)
			})
		},
	}
}

func newBaselineCmd(g *globals) *cobra.Command {
	var version, description string
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Mark an existing database as already at --version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if version == "" {
				return errors.New("--version is required")
			}
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				if err := s.m.Baseline(ctx, version, description); err != nil {
					return err
				}
				p := newPrinter(cmd)
				p.printf("Baselined at version %s\n", version)
				return p.result(nil)
			})
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "version the database already matches")
	cmd.Flags().StringVar(&description, "description", "baseline", "description recorded for the baseline")
	return cmd
}

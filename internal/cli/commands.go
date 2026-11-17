// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

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

	"github.com/gofrs/flock"
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

	createLockName  = ".gomigrate-create.lock"
	createLockPerm  = 0o660
	createLockRetry = 10 * time.Millisecond
)

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
			dir := g.overrides.Dir
			if dir == "" {
				dir = config.Default().Migrations.Dir
			}
			// Quote values so a DSN containing ':', '#' or '*' stays one YAML scalar.
			body := fmt.Sprintf(config.Template, strconv.Quote(driver), strconv.Quote(dsn), strconv.Quote(dir))
			if err := writeNew(path, body, configPerm); err != nil {
				if errors.Is(err, fs.ErrExist) {
					return fmt.Errorf("config file %q already exists", path)
				}
				return fmt.Errorf("write config: %w", err)
			}
			if err := os.MkdirAll(dir, dirPerm); err != nil {
				return fmt.Errorf("create migrations directory: %w", err)
			}
			p := newPrinter(cmd, g)
			p.emit(map[string]string{"config": path, "dir": dir}, func() {
				p.printf("Created %s and %s/\nNext: gomigrate create add_users_table\n", path, dir)
			})
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
			cfg, err := g.loadConfig(config.ForCreate)
			if err != nil {
				return err
			}
			draft := migrationDraft{name: args[0], seq: seq, pair: pair}
			version, written, err := createMigration(cmd.Context(), cfg.Migrations.Dir, draft, nil)
			if err != nil {
				return err
			}
			p := newPrinter(cmd, g)
			p.emit(map[string]any{"version": version, "files": written}, func() {
				for _, w := range written {
					p.printf("Created %s\n", w)
				}
			})
			return p.result(nil)
		},
	}
	cmd.Flags().BoolVar(&seq, "seq", false, "use the next sequential number instead of a UTC timestamp")
	cmd.Flags().BoolVar(&pair, "pair", false, "create separate .up.sql and .down.sql files")
	return cmd
}

type migrationDraft struct {
	name            string
	seq, pair, noTx bool
	up, down        string
}

// createMigration reserves a version, writes its files and optionally validates
// the result under one lock. A write or validation failure removes the new files.
func createMigration(ctx context.Context, dir string, draft migrationDraft, validate func() error) (string, []string, error) {
	name := strings.Trim(nameSanitizer.ReplaceAllString(strings.ToLower(draft.name), "_"), "_")
	if name == "" {
		return "", nil, errors.New("invalid migration name")
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", nil, err
	}
	// Keep the empty lock file in place so current holders and future contenders
	// always lock the same file. It is ignored by the migration catalog.
	guard := flock.New(filepath.Join(dir, createLockName), flock.SetPermissions(createLockPerm))
	if _, err := guard.TryLockContext(ctx, createLockRetry); err != nil {
		return "", nil, fmt.Errorf("lock migrations directory: %w", err)
	}
	defer func() { _ = guard.Close() }()

	used, maxSeq, err := scanVersions(dir)
	if err != nil {
		return "", nil, err
	}
	version := fmt.Sprintf("%0*d", seqWidth, maxSeq+1)
	if !draft.seq {
		version = uniqueTimestamp(time.Now().UTC(), used)
	}
	upMarker := "-- +migrate Up\n"
	if draft.noTx {
		upMarker = "-- +migrate Up notransaction\n"
	}
	type file struct{ name, body string }
	files := []file{{version + "_" + name + ".sql", upMarker + draft.up + "\n\n-- +migrate Down\n" + draft.down + "\n"}}
	if draft.pair {
		files = []file{{version + "_" + name + ".up.sql", upMarker + draft.up + "\n"}, {version + "_" + name + ".down.sql", draft.down + "\n"}}
	}
	written := make([]string, 0, len(files))
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := writeNew(path, f.body, migrationPerm); err != nil {
			removeAll(written)
			return "", nil, fmt.Errorf("create %s: %w", path, err)
		}
		written = append(written, path)
	}
	if validate != nil {
		if err := validate(); err != nil {
			removeAll(written)
			return "", nil, err
		}
	}
	return version, written, nil
}

func removeAll(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// seqPrefix captures the leading integer version of a migration file name.
var seqPrefix = regexp.MustCompile(`(?i)^v?(\d+)[._]`)

// scanVersions returns the leading integer versions used by files in dir
// and the largest one that fits an int. It only reads file names, so freshly
// scaffolded (still empty) migrations never block creating the next one.
func scanVersions(dir string) (used map[string]bool, maxSeq int, err error) {
	used = map[string]bool{}
	err = fs.WalkDir(os.DirFS(dir), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if m := seqPrefix.FindStringSubmatch(d.Name()); m != nil {
			used[strings.TrimLeft(m[1], "0")] = true
			if n, err := strconv.Atoi(m[1]); err == nil && n > maxSeq {
				maxSeq = n
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("scan %s: %w", dir, err)
	}
	return used, maxSeq, nil
}

// uniqueTimestamp formats now as a version, moving forward a second at a
// time past versions already in use (two creates within one second).
func uniqueTimestamp(now time.Time, used map[string]bool) string {
	for {
		v := now.Format(timestampFmt)
		if !used[v] {
			return v
		}
		now = now.Add(time.Second)
	}
}

// writeNew creates path exclusively (fs.ErrExist if it exists) and removes
// it again if the write fails, so no partial file is left behind.
func writeNew(path, body string, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, err = f.WriteString(body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

func newUpCmd(g *globals) *cobra.Command {
	var to, expectPlan string
	var dryRun, showSQL bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply pending migrations (all, or up to --to)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				p := newPrinter(cmd, g)
				if dryRun || showSQL {
					plan, id, err := s.m.PlanWithID(ctx, to)
					if err != nil {
						return err
					}
					p.plan(s, migrate.Up, plan, id, showSQL)
					return p.result(nil)
				}
				var res *migrate.Result
				var err error
				var opts []migrate.RunOption
				if cmd.Flags().Changed("expect-plan") {
					opts = append(opts, migrate.ExpectTargetPlan(expectPlan))
				}
				if to != "" {
					res, err = s.m.UpTo(ctx, to, opts...)
				} else {
					res, err = s.m.Up(ctx, opts...)
				}
				p.run("Applied", res, err)
				return p.result(withData(err, newRun(res)))
			})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "apply up to and including this version")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be applied without changing anything")
	cmd.Flags().BoolVar(&showSQL, "sql", false, "dry run that also prints the exact SQL of every migration")
	cmd.Flags().StringVar(&expectPlan, "expect-plan", "", "plan_id from an approved dry run; fail with plan_changed if the plan differs")
	return cmd
}

func newDownCmd(g *globals) *cobra.Command {
	var steps int
	var to, toTag, expectPlan string
	var all, dryRun, showSQL bool
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Roll back migrations (last one by default, --steps N, --to V, --to-tag T or --all)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if n := countSet(cmd, "steps", "to", "to-tag", "all"); n > 1 {
				return errors.New("use only one of --steps, --to, --to-tag and --all")
			}
			target := migrate.DownTarget{To: to, Tag: toTag}
			if all {
				target.To = "0"
			}
			if cmd.Flags().Changed("steps") {
				target.Steps = steps
			}
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				p := newPrinter(cmd, g)
				if dryRun || showSQL {
					plan, id, err := s.m.PlanDownWithID(ctx, target)
					if err != nil {
						return err
					}
					p.plan(s, migrate.Down, plan, id, showSQL)
					return p.result(nil)
				}
				var opts []migrate.RunOption
				if cmd.Flags().Changed("expect-plan") {
					opts = append(opts, migrate.ExpectTargetPlan(expectPlan))
				}
				res, err := s.m.Down(ctx, target, opts...)
				p.run("Rolled back", res, err)
				return p.result(withData(err, newRun(res)))
			})
		},
	}
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "number of migrations to roll back")
	cmd.Flags().StringVar(&to, "to", "", "roll back every migration above this version")
	cmd.Flags().StringVar(&toTag, "to-tag", "", "roll back every migration applied after the one tagged with this name")
	cmd.Flags().BoolVar(&all, "all", false, "roll back every migration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be rolled back without changing anything")
	cmd.Flags().BoolVar(&showSQL, "sql", false, "dry run that also prints the exact SQL of every rollback")
	cmd.Flags().StringVar(&expectPlan, "expect-plan", "", "plan_id from an approved dry run; fail with plan_changed if the plan differs")
	return cmd
}

func newRedoCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "redo",
		Short: "Roll back the last migration and apply it again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				p := newPrinter(cmd, g)
				res, err := s.m.Redo(ctx)
				p.run("Redid", res, err)
				return p.result(withData(err, newRun(res)))
			})
		},
	}
}

func newStatusCmd(g *globals) *cobra.Command {
	var failOnPending bool
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
				p := newPrinter(cmd, g)
				p.status(st)
				if p.err != nil {
					return p.err
				}
				if failOnPending && st.Pending > 0 {
					return fmt.Errorf("%w: %d", ErrPending, st.Pending)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&failOnPending, "fail-on-pending", false, "exit with code 2 when migrations are pending (for CI)")
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
				p := newPrinter(cmd, g)
				p.emit(map[string]bool{"valid": true}, func() { p.printf("OK: migrations are valid.\n") })
				return p.result(nil)
			})
		},
	}
}

func newRepairCmd(g *globals) *cobra.Command {
	var clearFailed, acceptChecksum, dryRun bool
	cmd := &cobra.Command{
		Use:     "repair MIGRATION (--clear-failed | --accept-checksum) [--dry-run]",
		Short:   "Repair one migration's history row",
		Example: "  gomigrate repair 42 --accept-checksum --dry-run\n  gomigrate repair 'R__refresh views' --clear-failed",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 || clearFailed == acceptChecksum {
				return fmt.Errorf("repair requires one migration and exactly one of --clear-failed or --accept-checksum: %w", migrate.ErrInvalidArgument)
			}
			target := migrate.RepairTarget{Migration: args[0], Action: migrate.RepairAcceptChecksum}
			if clearFailed {
				target.Action = migrate.RepairClearFailed
			}
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				p := newPrinter(cmd, g)
				if dryRun {
					plan, err := s.m.PlanRepair(ctx, target)
					if err != nil {
						return err
					}
					p.repairPlan(plan)
					return p.result(nil)
				}
				res, err := s.m.Repair(ctx, target)
				if err != nil {
					return err
				}
				realigned := res.Realigned
				if realigned == nil {
					realigned = []string{}
				}
				p.emit(map[string]any{"removed_failed": res.RemovedFailed, "realigned": realigned}, func() {
					if clearFailed {
						p.printf("Removed failed history row for %s.\n", target.Migration)
					} else {
						p.printf("Accepted checksum drift for version %s.\n", target.Migration)
					}
				})
				return p.result(nil)
			})
		},
	}
	cmd.Flags().BoolVar(&clearFailed, "clear-failed", false, "remove a failed row after restoring the schema by hand")
	cmd.Flags().BoolVar(&acceptChecksum, "accept-checksum", false, "accept deliberate drift of an applied versioned migration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the proposed history change without writing")
	return cmd
}

func newMarkAppliedCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "mark-applied VERSION",
		Short: "Record a migration as applied without running it (e.g. after finishing a dirty one by hand)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				if err := s.m.MarkApplied(ctx, args[0]); err != nil {
					return err
				}
				p := newPrinter(cmd, g)
				p.emit(map[string]string{"version": args[0]}, func() { p.printf("Marked %s as applied\n", args[0]) })
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
				p := newPrinter(cmd, g)
				p.emit(map[string]string{"version": version}, func() { p.printf("Baselined at version %s\n", version) })
				return p.result(nil)
			})
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "version the database already matches")
	cmd.Flags().StringVar(&description, "description", "baseline", "description recorded for the baseline")
	return cmd
}

func newTagCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "tag NAME",
		Short: "Tag the most recently applied migration (roll back to it later with down --to-tag)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return g.withSession(cmd, func(ctx context.Context, s *session) error {
				if err := s.m.Tag(ctx, args[0]); err != nil {
					return err
				}
				p := newPrinter(cmd, g)
				p.emit(map[string]string{"tag": args[0]}, func() { p.printf("Tagged current version as %s\n", args[0]) })
				return p.result(nil)
			})
		},
	}
}

// countSet returns how many of the named flags were set explicitly.
func countSet(cmd *cobra.Command, names ...string) int {
	n := 0
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			n++
		}
	}
	return n
}

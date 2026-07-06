/**
 * @file main.go
 * @description gomigrate — the standalone command-line interface for the
 * go-migrate SQL migration engine. Thin cobra wrapper over the public
 * migrations API (init / status / up / down / validate).
 * @author Admilson B. F. Cossa
 */

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	migrations "github.com/go-gen-ecosystem/go-migrate"
)

// version is the CLI version, overridable at build time via -ldflags.
var version = "dev"

// toolOpener lazily constructs a migration tool from the resolved config path.
type toolOpener func() (migrations.StandaloneMigrationTool, error)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the gomigrate command tree. It is a constructor (no package
// global state) so tests can execute it with arbitrary arguments.
func newRootCmd() *cobra.Command {
	var (
		configPath string
		dryRun     bool
		steps      int
	)

	root := &cobra.Command{
		Use:     "gomigrate",
		Short:   "gomigrate — a lightweight SQL database migration tool",
		Long:    "gomigrate applies, rolls back and validates versioned SQL migrations against\nSQLite, MySQL and PostgreSQL, driven by a single config file.",
		Version: version,
		// Errors are printed once by main; do not let cobra also dump usage.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&configPath, "config", "c", ".gomigrate.yaml",
		"path to the migration config file (.yaml, .yml or .json)")

	open := func() (migrations.StandaloneMigrationTool, error) {
		tool, err := migrations.NewStandaloneMigrationToolFromPath(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load migration tool: %w", err)
		}
		return tool, nil
	}

	root.AddCommand(
		newInitCmd(&configPath),
		newUpCmd(open, &dryRun),
		newDownCmd(open, &steps),
		newStatusCmd(open),
		newValidateCmd(open),
	)
	return root
}

func newInitCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Write a starter config and create the migrations directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := migrations.CreateDefaultStandaloneConfig()
			if cfg.Migration.MigrationsPath == "" {
				cfg.Migration.MigrationsPath = "migrations"
			}
			if err := os.MkdirAll(cfg.Migration.MigrationsPath, 0o755); err != nil {
				return fmt.Errorf("failed to create migrations directory: %w", err)
			}
			if _, err := os.Stat(*configPath); err == nil {
				return fmt.Errorf("config file %q already exists", *configPath)
			}
			if err := migrations.SaveStandaloneConfig(cfg, *configPath); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}
			cmd.Printf("Initialized gomigrate: wrote %s and %s/\n", *configPath, cfg.Migration.MigrationsPath)
			return nil
		},
	}
}

func newUpCmd(open toolOpener, dryRun *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tool, err := open()
			if err != nil {
				return err
			}
			defer func() { _ = tool.Close() }()

			if err := tool.Migrate(cmd.Context(), "", *dryRun); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}
			if *dryRun {
				cmd.Println("Dry run complete: no changes applied")
			} else {
				cmd.Println("Migrations applied successfully")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(dryRun, "dry-run", false, "resolve and validate pending migrations without applying them")
	return cmd
}

func newDownCmd(open toolOpener, steps *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Roll back the most recently applied migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if *steps <= 0 {
				return fmt.Errorf("--steps must be a positive integer, got %d", *steps)
			}
			tool, err := open()
			if err != nil {
				return err
			}
			defer func() { _ = tool.Close() }()

			if err := tool.Rollback(cmd.Context(), *steps); err != nil {
				return fmt.Errorf("rollback failed: %w", err)
			}
			cmd.Printf("Rolled back %d migration(s)\n", *steps)
			return nil
		},
	}
	cmd.Flags().IntVar(steps, "steps", 1, "number of migrations to roll back")
	return cmd
}

func newStatusCmd(open toolOpener) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tool, err := open()
			if err != nil {
				return err
			}
			defer func() { _ = tool.Close() }()

			status, err := tool.Status(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to read status: %w", err)
			}
			cmd.Printf("Database version: %s\n", status.DatabaseVersion)
			cmd.Printf("Total:    %d\n", status.TotalMigrations)
			cmd.Printf("Applied:  %d\n", status.ExecutedCount)
			cmd.Printf("Pending:  %d\n", status.PendingCount)
			cmd.Printf("Failed:   %d\n", status.FailedCount)
			if status.IsLocked {
				cmd.Println("Locked:   yes")
			}
			return nil
		},
	}
}

func newValidateCmd(open toolOpener) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate all migration files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tool, err := open()
			if err != nil {
				return err
			}
			defer func() { _ = tool.Close() }()

			if err := tool.Validate(cmd.Context()); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}
			cmd.Println("All migrations are valid")
			return nil
		},
	}
}

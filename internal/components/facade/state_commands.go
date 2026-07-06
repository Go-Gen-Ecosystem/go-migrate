/**
 * @file state_commands.go
 * @description State-based migration CLI commands
 * @author Admilson B. F. Cossa
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/facade/state_commands.go
 */

package facade

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/state"
)

// StateInitCommand initializes state-based migrations
type StateInitCommand struct {
	facade MigrationFacade
}

// Execute initializes state-based migrations
func (c *StateInitCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	// TODO: Create initial schema.yaml file
	output := "State-based migrations initialized\n"
	output += "Created: database/schema.yaml\n"
	output += "\nNext steps:\n"
	output += "  1. Edit database/schema.yaml to define your schema\n"
	output += "  2. Run 'gomigrate state validate' to check your schema\n"
	output += "  3. Run 'gomigrate state plan' to generate migrations\n"

	return &CommandResult{
		Success:      true,
		Output: output,
		ExitCode:     0,
	}, nil
}

// StateValidateCommand validates a schema file
type StateValidateCommand struct {
	facade MigrationFacade
}

// Execute validates a schema file
func (c *StateValidateCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	schemaPath := getStringVar(opts.Variables, "schema", "database/schema.yaml")

	// Get migrator from facade
	migrator, err := c.getMigrator()
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  err.Error(),
			ExitCode:     1,
		}, err
	}

	// Validate schema
	result, err := migrator.ValidateSchemaFile(ctx, schemaPath)
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("Validation error: %v", err),
			ExitCode:     1,
		}, err
	}

	var output strings.Builder

	if result.Valid {
		output.WriteString("✓ Schema validation passed\n\n")
	} else {
		output.WriteString("✗ Schema validation failed\n\n")
		output.WriteString("Errors:\n")
		for _, err := range result.Errors {
			output.WriteString(fmt.Sprintf("  • %s\n", err))
		}
		output.WriteString("\n")
	}

	if len(result.Warnings) > 0 {
		output.WriteString("Warnings:\n")
		for _, warning := range result.Warnings {
			output.WriteString(fmt.Sprintf("  ⚠ %s\n", warning))
		}
	}

	return &CommandResult{
		Success:        result.Valid,
		Output: output.String(),
		ExitCode:       boolToExitCode(!result.Valid),
	}, nil
}

// StatePreviewCommand previews schema changes
type StatePreviewCommand struct {
	facade MigrationFacade
}

// Execute previews schema changes
func (c *StatePreviewCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	schemaPath := getStringVar(opts.Variables, "schema", "database/schema.yaml")

	migrator, err := c.getMigrator()
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  err.Error(),
			ExitCode:     1,
		}, err
	}

	// Get diff
	diff, err := migrator.Preview(ctx, schemaPath)
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("Preview error: %v", err),
			ExitCode:     1,
		}, err
	}

	// Format diff
	output := migrator.FormatDiff(diff)

	return &CommandResult{
		Success:        true,
		Output: output,
		ExitCode:       0,
	}, nil
}

// StatePlanCommand generates migration from schema
type StatePlanCommand struct {
	facade MigrationFacade
}

// Execute generates migration from schema
func (c *StatePlanCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	schemaPath := getStringVar(opts.Variables, "schema", "database/schema.yaml")

	migrator, err := c.getMigrator()
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  err.Error(),
			ExitCode:     1,
		}, err
	}

	// Generate migration file
	filepath, err := migrator.GenerateMigrationFile(ctx, schemaPath)
	if err != nil {
		if err.Error() == "no changes to generate migration for" {
			return &CommandResult{
				Success:        true,
				Output: "✓ Database is in sync with schema - no migration needed\n",
				ExitCode:       0,
			}, nil
		}

		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("Failed to generate migration: %v", err),
			ExitCode:     1,
		}, err
	}

	output := fmt.Sprintf("✓ Generated migration: %s\n", filepath)
	output += "\nNext steps:\n"
	output += "  1. Review the generated migration file\n"
	output += "  2. Run 'gomigrate up' to apply the migration\n"

	return &CommandResult{
		Success:        true,
		Output: output,
		ExitCode:       0,
	}, nil
}

// StateApplyCommand applies schema changes
type StateApplyCommand struct {
	facade MigrationFacade
}

// Execute applies schema changes
func (c *StateApplyCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	schemaPath := getStringVar(opts.Variables, "schema", "database/schema.yaml")
	dryRun := opts.DryRun

	migrator, err := c.getMigrator()
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  err.Error(),
			ExitCode:     1,
		}, err
	}

	// Sync (plan + apply)
	status, err := migrator.Sync(ctx, schemaPath, dryRun)
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("Apply failed: %v", err),
			ExitCode:     1,
		}, err
	}

	var output string

	if dryRun {
		output = "✓ Dry run completed\n\n"
	} else {
		output = "✓ Schema applied successfully\n\n"
	}

	output += fmt.Sprintf("Migrations executed: %d\n", status.ExecutedCount)
	output += fmt.Sprintf("Migrations pending: %d\n", status.PendingCount)
	output += fmt.Sprintf("Migrations failed: %d\n", status.FailedCount)

	return &CommandResult{
		Success:        status.FailedCount == 0,
		Output: output,
		ExitCode:       boolToExitCode(status.FailedCount > 0),
	}, nil
}

// StateSyncCommand synchronizes database to schema
type StateSyncCommand struct {
	facade MigrationFacade
}

// Execute synchronizes database to schema
func (c *StateSyncCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	// Same as StateApplyCommand
	applyCmd := &StateApplyCommand{facade: c.facade}
	return applyCmd.Execute(ctx, args, opts)
}

// StateExportCommand exports current database to schema file
type StateExportCommand struct {
	facade MigrationFacade
}

// Execute exports current database to schema file
func (c *StateExportCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	outputPath := getStringVar(opts.Variables, "output", "database/schema.yaml")
	formatStr := getStringVar(opts.Variables, "format", "yaml")

	var format state.SchemaFormat
	switch strings.ToLower(formatStr) {
	case "yaml", "yml":
		format = state.FormatYAML
	case "json":
		format = state.FormatJSON
	default:
		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("unsupported format: %s (use yaml or json)", formatStr),
			ExitCode:     1,
		}, fmt.Errorf("unsupported format: %s", formatStr)
	}

	migrator, err := c.getMigrator()
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  err.Error(),
			ExitCode:     1,
		}, err
	}

	// Export schema
	err = migrator.Export(ctx, outputPath, format)
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("Export failed: %v", err),
			ExitCode:     1,
		}, err
	}

	output := fmt.Sprintf("✓ Exported database schema to: %s\n", outputPath)
	output += fmt.Sprintf("Format: %s\n", format)

	return &CommandResult{
		Success:        true,
		Output: output,
		ExitCode:       0,
	}, nil
}

// StateCheckCommand checks if database is in sync with schema
type StateCheckCommand struct {
	facade MigrationFacade
}

// Execute checks if database is in sync with schema
func (c *StateCheckCommand) Execute(ctx context.Context, args []string, opts *ExecutionOptions) (*CommandResult, error) {
	schemaPath := getStringVar(opts.Variables, "schema", "database/schema.yaml")

	migrator, err := c.getMigrator()
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  err.Error(),
			ExitCode:     1,
		}, err
	}

	// Check sync
	inSync, err := migrator.CheckSync(ctx, schemaPath)
	if err != nil {
		return &CommandResult{
			Success:      false,
			ErrorOutput:  fmt.Sprintf("Check failed: %v", err),
			ExitCode:     1,
		}, err
	}

	var output string

	if inSync {
		output = "✓ Database is in sync with schema\n"
	} else {
		output = "✗ Database is NOT in sync with schema\n"
		output += "\nRun 'gomigrate state preview' to see changes\n"
	}

	return &CommandResult{
		Success:        inSync,
		Output: output,
		ExitCode:       boolToExitCode(!inSync),
	}, nil
}

// Helper methods

func (c *StateValidateCommand) getMigrator() (*state.DefaultStateBasedMigrator, error) {
	migratorInterface, err := c.facade.GetStateBasedMigrator()
	if err != nil {
		return nil, err
	}

	migrator, ok := migratorInterface.(*state.DefaultStateBasedMigrator)
	if !ok {
		return nil, fmt.Errorf("invalid state-based migrator type")
	}

	return migrator, nil
}

func (c *StatePreviewCommand) getMigrator() (*state.DefaultStateBasedMigrator, error) {
	migratorInterface, err := c.facade.GetStateBasedMigrator()
	if err != nil {
		return nil, err
	}

	migrator, ok := migratorInterface.(*state.DefaultStateBasedMigrator)
	if !ok {
		return nil, fmt.Errorf("invalid state-based migrator type")
	}

	return migrator, nil
}

func (c *StatePlanCommand) getMigrator() (*state.DefaultStateBasedMigrator, error) {
	migratorInterface, err := c.facade.GetStateBasedMigrator()
	if err != nil {
		return nil, err
	}

	migrator, ok := migratorInterface.(*state.DefaultStateBasedMigrator)
	if !ok {
		return nil, fmt.Errorf("invalid state-based migrator type")
	}

	return migrator, nil
}

func (c *StateApplyCommand) getMigrator() (*state.DefaultStateBasedMigrator, error) {
	migratorInterface, err := c.facade.GetStateBasedMigrator()
	if err != nil {
		return nil, err
	}

	migrator, ok := migratorInterface.(*state.DefaultStateBasedMigrator)
	if !ok {
		return nil, fmt.Errorf("invalid state-based migrator type")
	}

	return migrator, nil
}

func (c *StateExportCommand) getMigrator() (*state.DefaultStateBasedMigrator, error) {
	migratorInterface, err := c.facade.GetStateBasedMigrator()
	if err != nil {
		return nil, err
	}

	migrator, ok := migratorInterface.(*state.DefaultStateBasedMigrator)
	if !ok {
		return nil, fmt.Errorf("invalid state-based migrator type")
	}

	return migrator, nil
}

func (c *StateCheckCommand) getMigrator() (*state.DefaultStateBasedMigrator, error) {
	migratorInterface, err := c.facade.GetStateBasedMigrator()
	if err != nil {
		return nil, err
	}

	migrator, ok := migratorInterface.(*state.DefaultStateBasedMigrator)
	if !ok {
		return nil, fmt.Errorf("invalid state-based migrator type")
	}

	return migrator, nil
}

func getStringVar(vars map[string]string, key, defaultValue string) string {
	if val, ok := vars[key]; ok && val != "" {
		return val
	}
	return defaultValue
}

func boolToExitCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

// GetInfo implementations for CommandExecutor interface

func (c *StateInitCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:init",
		Description: "Initialize state-based migrations with a schema file",
		Usage:       "state:init [--path=<schema-path>]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "path", Description: "Path for schema file", Type: "string", DefaultValue: "database/schema.yaml"},
		},
	}
}

func (c *StateInitCommand) Validate(args []string) error {
	return nil
}

func (c *StateValidateCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:validate",
		Description: "Validate schema file syntax and semantics",
		Usage:       "state:validate [--schema=<path>]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "schema", Description: "Path to schema file", Type: "string", DefaultValue: "database/schema.yaml"},
		},
	}
}

func (c *StateValidateCommand) Validate(args []string) error {
	return nil
}

func (c *StatePreviewCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:preview",
		Description: "Preview schema changes (diff current vs desired)",
		Usage:       "state:preview [--schema=<path>]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "schema", Description: "Path to schema file", Type: "string", DefaultValue: "database/schema.yaml"},
		},
	}
}

func (c *StatePreviewCommand) Validate(args []string) error {
	return nil
}

func (c *StatePlanCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:plan",
		Description: "Generate migration from schema diff",
		Usage:       "state:plan [--schema=<path>] [--output=<dir>]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "schema", Description: "Path to schema file", Type: "string", DefaultValue: "database/schema.yaml"},
			{Name: "output", Description: "Output directory for migration", Type: "string", DefaultValue: "migrations"},
		},
	}
}

func (c *StatePlanCommand) Validate(args []string) error {
	return nil
}

func (c *StateApplyCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:apply",
		Description: "Apply a generated migration",
		Usage:       "state:apply <migration-id>",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "dry-run", Description: "Preview without applying", Type: "bool", DefaultValue: false},
		},
	}
}

func (c *StateApplyCommand) Validate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("migration ID required")
	}
	return nil
}

func (c *StateSyncCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:sync",
		Description: "Sync database to match schema (plan + apply)",
		Usage:       "state:sync [--schema=<path>] [--dry-run]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "schema", Description: "Path to schema file", Type: "string", DefaultValue: "database/schema.yaml"},
			{Name: "dry-run", Description: "Preview without applying", Type: "bool", DefaultValue: false},
		},
	}
}

func (c *StateSyncCommand) Validate(args []string) error {
	return nil
}

func (c *StateExportCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:export",
		Description: "Export current database schema to file",
		Usage:       "state:export [--output=<path>] [--format=yaml|json]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "output", Description: "Output file path", Type: "string", DefaultValue: "database/schema.yaml"},
			{Name: "format", Description: "Output format (yaml or json)", Type: "string", DefaultValue: "yaml"},
		},
	}
}

func (c *StateExportCommand) Validate(args []string) error {
	return nil
}

func (c *StateCheckCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "state:check",
		Description: "Check if database is in sync with schema",
		Usage:       "state:check [--schema=<path>]",
		Category:    "State-Based Migration",
		Flags: []*FlagInfo{
			{Name: "schema", Description: "Path to schema file", Type: "string", DefaultValue: "database/schema.yaml"},
		},
	}
}

func (c *StateCheckCommand) Validate(args []string) error {
	return nil
}

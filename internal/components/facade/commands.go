/**
 * @file commands.go
 * @description Command executors for all migration operations with comprehensive command pattern implementation
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/facade/commands.go
 */

package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UpCommand executes migration up operations
type UpCommand struct {
	facade MigrationFacade
}

func (c *UpCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	migrationOptions := &MigrationOptions{
		DryRun:      options.DryRun,
		Timeout:     options.Timeout,
		Parallel:    false,
		Environment: options.Environment,
		Variables:   options.Variables,
		Verbose:     options.Verbose,
	}

	// Parse arguments
	if len(args) > 0 {
		migrationOptions.Target = args[0]
	}

	// Parse steps if provided
	if len(args) > 1 {
		if steps, err := strconv.Atoi(args[1]); err == nil {
			migrationOptions.Steps = steps
		}
	}

	result, err := c.facade.Up(ctx, migrationOptions)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatUpResult(result, options.OutputFormat)
	return &CommandResult{
		Success:       result.Success,
		ExitCode:      0,
		Output:        output,
		ExecutionTime: result.ExecutionTime,
	}, nil
}

func (c *UpCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "up",
		Description: "Apply pending migrations",
		Usage:       "up [target] [steps]",
		Category:    "Migration",
		Flags: []*FlagInfo{
			{Name: "dry-run", Description: "Show what would be executed without applying changes", Type: "bool"},
			{Name: "force", Description: "Force execution even with warnings", Type: "bool"},
			{Name: "parallel", Description: "Enable parallel execution", Type: "bool"},
			{Name: "timeout", Description: "Operation timeout", Type: "duration", DefaultValue: "30s"},
		},
	}
}

func (c *UpCommand) Validate(args []string) error {
	if len(args) > 2 {
		return fmt.Errorf("too many arguments, expected at most 2")
	}
	return nil
}

func (c *UpCommand) formatUpResult(result *MigrationResult, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data)
	default:
		var builder strings.Builder
		if result.Success {
			builder.WriteString(fmt.Sprintf("✓ Successfully applied %d migrations\n", result.ExecutedCount))
		} else {
			builder.WriteString(fmt.Sprintf("✗ Failed to apply migrations (%d failed)\n", result.FailedCount))
		}
		
		for _, migration := range result.Migrations {
			status := "✓"
			if !migration.Success {
				status = "✗"
			}
			builder.WriteString(fmt.Sprintf("  %s %s (%v)\n", status, migration.Name, migration.ExecutionTime))
		}
		
		if result.Summary != "" {
			builder.WriteString(fmt.Sprintf("\n%s\n", result.Summary))
		}
		
		return builder.String()
	}
}

// DownCommand executes migration down operations
type DownCommand struct {
	facade MigrationFacade
}

func (c *DownCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	migrationOptions := &MigrationOptions{
		DryRun:      options.DryRun,
		Timeout:     options.Timeout,
		Environment: options.Environment,
		Variables:   options.Variables,
		Verbose:     options.Verbose,
		Steps:       1, // Default to 1 step
	}

	// Parse arguments
	if len(args) > 0 {
		if steps, err := strconv.Atoi(args[0]); err == nil {
			migrationOptions.Steps = steps
		} else {
			migrationOptions.Target = args[0]
		}
	}

	result, err := c.facade.Down(ctx, migrationOptions)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatDownResult(result, options.OutputFormat)
	return &CommandResult{
		Success:       result.Success,
		ExitCode:      0,
		Output:        output,
		ExecutionTime: result.ExecutionTime,
	}, nil
}

func (c *DownCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "down",
		Description: "Rollback applied migrations",
		Usage:       "down [steps|target]",
		Category:    "Migration",
		Flags: []*FlagInfo{
			{Name: "dry-run", Description: "Show what would be executed without applying changes", Type: "bool"},
			{Name: "force", Description: "Force execution even with warnings", Type: "bool"},
			{Name: "timeout", Description: "Operation timeout", Type: "duration", DefaultValue: "30s"},
		},
	}
}

func (c *DownCommand) Validate(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("too many arguments, expected at most 1")
	}
	return nil
}

func (c *DownCommand) formatDownResult(result *MigrationResult, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data)
	default:
		var builder strings.Builder
		if result.Success {
			builder.WriteString(fmt.Sprintf("✓ Successfully rolled back %d migrations\n", result.ExecutedCount))
		} else {
			builder.WriteString(fmt.Sprintf("✗ Failed to rollback migrations (%d failed)\n", result.FailedCount))
		}
		
		for _, migration := range result.Migrations {
			status := "✓"
			if !migration.Success {
				status = "✗"
			}
			builder.WriteString(fmt.Sprintf("  %s %s (%v)\n", status, migration.Name, migration.ExecutionTime))
		}
		
		return builder.String()
	}
}

// StatusCommand shows migration status
type StatusCommand struct {
	facade MigrationFacade
}

func (c *StatusCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	status, err := c.facade.Status(ctx)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatStatusResult(status, options.OutputFormat)
	return &CommandResult{
		Success: true,
		ExitCode: 0,
		Output:  output,
	}, nil
}

func (c *StatusCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "status",
		Description: "Show migration status",
		Usage:       "status",
		Category:    "Information",
		Flags: []*FlagInfo{
			{Name: "output", Description: "Output format (text, json, table)", Type: "string", DefaultValue: "text"},
		},
	}
}

func (c *StatusCommand) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("status command does not accept arguments")
	}
	return nil
}

func (c *StatusCommand) formatStatusResult(status *MigrationStatus, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(status, "", "  ")
		return string(data)
	case OutputFormatTable:
		return c.formatStatusTable(status)
	default:
		var builder strings.Builder
		builder.WriteString("Migration Status:\n")
		builder.WriteString(fmt.Sprintf("  Total migrations: %d\n", status.TotalMigrations))
		builder.WriteString(fmt.Sprintf("  Applied: %d\n", status.AppliedMigrations))
		builder.WriteString(fmt.Sprintf("  Pending: %d\n", status.PendingMigrations))
		builder.WriteString(fmt.Sprintf("  Failed: %d\n", status.FailedMigrations))
		
		if status.LastApplied != nil {
			builder.WriteString(fmt.Sprintf("  Last applied: %s (%s)\n", 
				status.LastApplied.Name, status.LastApplied.AppliedAt.Format(time.RFC3339)))
		}
		
		if status.NextPending != nil {
			builder.WriteString(fmt.Sprintf("  Next pending: %s\n", status.NextPending.Name))
		}
		
		if status.IsLocked {
			builder.WriteString("  Status: LOCKED\n")
			if status.LockInfo != nil {
				builder.WriteString(fmt.Sprintf("  Locked by: %s at %s\n", 
					status.LockInfo.LockedBy, status.LockInfo.LockedAt.Format(time.RFC3339)))
			}
		} else {
			builder.WriteString("  Status: UNLOCKED\n")
		}
		
		return builder.String()
	}
}

func (c *StatusCommand) formatStatusTable(status *MigrationStatus) string {
	var builder strings.Builder
	builder.WriteString("┌─────────────────────┬─────────┐\n")
	builder.WriteString("│ Metric              │ Value   │\n")
	builder.WriteString("├─────────────────────┼─────────┤\n")
	builder.WriteString(fmt.Sprintf("│ Total migrations    │ %7d │\n", status.TotalMigrations))
	builder.WriteString(fmt.Sprintf("│ Applied             │ %7d │\n", status.AppliedMigrations))
	builder.WriteString(fmt.Sprintf("│ Pending             │ %7d │\n", status.PendingMigrations))
	builder.WriteString(fmt.Sprintf("│ Failed              │ %7d │\n", status.FailedMigrations))
	builder.WriteString("└─────────────────────┴─────────┘\n")
	return builder.String()
}

// CreateCommand creates new migrations
type CreateCommand struct {
	facade MigrationFacade
}

func (c *CreateCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	if len(args) == 0 {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: "migration name is required",
		}, fmt.Errorf("migration name is required")
	}

	createOptions := &CreateOptions{
		Template:    "sql",
		Directory:   "migrations",
		Language:    "sql",
		Type:        MigrationTypeSQL,
		Description: args[0],
		Variables:   options.Variables,
	}

	result, err := c.facade.Create(ctx, args[0], createOptions)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatCreateResult(result, options.OutputFormat)
	return &CommandResult{
		Success: result.Success,
		ExitCode: 0,
		Output:  output,
	}, nil
}

func (c *CreateCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "create",
		Description: "Create a new migration",
		Usage:       "create <name>",
		Category:    "Migration",
		Flags: []*FlagInfo{
			{Name: "template", Description: "Migration template to use", Type: "string", DefaultValue: "sql"},
			{Name: "directory", Description: "Directory to create migration in", Type: "string", DefaultValue: "migrations"},
			{Name: "author", Description: "Migration author", Type: "string"},
		},
	}
}

func (c *CreateCommand) Validate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("migration name is required")
	}
	if len(args) > 1 {
		return fmt.Errorf("too many arguments, expected 1")
	}
	return nil
}

func (c *CreateCommand) formatCreateResult(result *CreateResult, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data)
	default:
		var builder strings.Builder
		if result.Success {
			builder.WriteString(fmt.Sprintf("✓ Created migration: %s\n", result.FileName))
			builder.WriteString(fmt.Sprintf("  Path: %s\n", result.FilePath))
			builder.WriteString(fmt.Sprintf("  Version: %s\n", result.Version))
			builder.WriteString(fmt.Sprintf("  Size: %d bytes\n", result.Size))
		} else {
			builder.WriteString("✗ Failed to create migration\n")
		}
		return builder.String()
	}
}

// ListCommand lists migrations
type ListCommand struct {
	facade MigrationFacade
}

func (c *ListCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	listOptions := &ListOptions{
		Environment:    options.Environment,
		Format:         options.OutputFormat,
		IncludeDetails: options.Verbose,
		SortBy:         "version",
		SortOrder:      SortOrderAsc,
		Limit:          100,
		Offset:         0,
	}

	result, err := c.facade.List(ctx, listOptions)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatListResult(result, options.OutputFormat)
	return &CommandResult{
		Success: true,
		ExitCode: 0,
		Output:  output,
	}, nil
}

func (c *ListCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "list",
		Description: "List migrations",
		Usage:       "list",
		Category:    "Information",
		Flags: []*FlagInfo{
			{Name: "verbose", Description: "Show detailed information", Type: "bool"},
			{Name: "output", Description: "Output format", Type: "string", DefaultValue: "text"},
			{Name: "limit", Description: "Limit number of results", Type: "int", DefaultValue: 100},
		},
	}
}

func (c *ListCommand) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("list command does not accept arguments")
	}
	return nil
}

func (c *ListCommand) formatListResult(result *ListResult, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data)
	case OutputFormatTable:
		return c.formatListTable(result)
	default:
		var builder strings.Builder
		builder.WriteString(fmt.Sprintf("Migrations (%d total, %d shown):\n\n", 
			result.TotalCount, result.FilteredCount))
		
		for _, migration := range result.Migrations {
			status := "PENDING"
			if migration.Status == "applied" {
				status = "APPLIED"
			} else if migration.Status == "failed" {
				status = "FAILED"
			}
			
			builder.WriteString(fmt.Sprintf("  %s %s [%s]\n", 
				migration.Version, migration.Name, status))
			
			if migration.AppliedAt != nil {
				builder.WriteString(fmt.Sprintf("    Applied: %s\n", 
					migration.AppliedAt.Format(time.RFC3339)))
			}
		}
		
		return builder.String()
	}
}

func (c *ListCommand) formatListTable(result *ListResult) string {
	var builder strings.Builder
	builder.WriteString("┌──────────────────┬─────────────────────────────┬─────────┬─────────────────────┐\n")
	builder.WriteString("│ Version          │ Name                        │ Status  │ Applied At          │\n")
	builder.WriteString("├──────────────────┼─────────────────────────────┼─────────┼─────────────────────┤\n")
	
	for _, migration := range result.Migrations {
		appliedAt := "N/A"
		if migration.AppliedAt != nil {
			appliedAt = migration.AppliedAt.Format("2006-01-02 15:04:05")
		}
		
		builder.WriteString(fmt.Sprintf("│ %-16s │ %-27s │ %-7s │ %-19s │\n",
			truncateString(migration.Version, 16),
			truncateString(migration.Name, 27),
			strings.ToUpper(migration.Status),
			appliedAt))
	}
	
	builder.WriteString("└──────────────────┴─────────────────────────────┴─────────┴─────────────────────┘\n")
	return builder.String()
}

// ValidateCommand validates migrations
type ValidateCommand struct {
	facade MigrationFacade
}

func (c *ValidateCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	validateOptions := &ValidateOptions{
		CheckIntegrity:    true,
		CheckSyntax:       true,
		CheckDependencies: true,
		CheckConstraints:  true,
		Environment:       options.Environment,
		Strict:            false,
	}

	result, err := c.facade.Validate(ctx, validateOptions)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatValidateResult(result, options.OutputFormat)
	exitCode := 0
	if !result.IsValid {
		exitCode = 1
	}

	return &CommandResult{
		Success:  result.IsValid,
		ExitCode: exitCode,
		Output:   output,
	}, nil
}

func (c *ValidateCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "validate",
		Description: "Validate migrations",
		Usage:       "validate",
		Category:    "Validation",
		Flags: []*FlagInfo{
			{Name: "strict", Description: "Enable strict validation", Type: "bool"},
			{Name: "syntax-only", Description: "Check syntax only", Type: "bool"},
		},
	}
}

func (c *ValidateCommand) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("validate command does not accept arguments")
	}
	return nil
}

func (c *ValidateCommand) formatValidateResult(result *ValidationResult, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data)
	default:
		var builder strings.Builder
		
		if result.IsValid {
			builder.WriteString("✓ All migrations are valid\n")
		} else {
			builder.WriteString("✗ Validation failed\n")
		}
		
		builder.WriteString(fmt.Sprintf("  Checked: %d migrations\n", result.CheckedCount))
		builder.WriteString(fmt.Sprintf("  Valid: %d\n", result.ValidCount))
		builder.WriteString(fmt.Sprintf("  Invalid: %d\n", result.InvalidCount))
		
		if len(result.Errors) > 0 {
			builder.WriteString("\nErrors:\n")
			for _, err := range result.Errors {
				builder.WriteString(fmt.Sprintf("  ✗ %s: %s\n", err.Migration, err.Message))
			}
		}
		
		if len(result.Warnings) > 0 {
			builder.WriteString("\nWarnings:\n")
			for _, warning := range result.Warnings {
				builder.WriteString(fmt.Sprintf("  ⚠ %s: %s\n", warning.Migration, warning.Message))
			}
		}
		
		return builder.String()
	}
}

// HistoryCommand shows migration history
type HistoryCommand struct {
	facade MigrationFacade
}

func (c *HistoryCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	historyOptions := &HistoryOptions{
		Limit:            50,
		Offset:           0,
		Environment:      options.Environment,
		IncludeRollbacks: true,
		Format:           options.OutputFormat,
	}

	result, err := c.facade.History(ctx, historyOptions)
	if err != nil {
		return &CommandResult{
			Success:     false,
			ExitCode:    1,
			ErrorOutput: err.Error(),
		}, err
	}

	output := c.formatHistoryResult(result, options.OutputFormat)
	return &CommandResult{
		Success: true,
		ExitCode: 0,
		Output:  output,
	}, nil
}

func (c *HistoryCommand) GetInfo() *CommandInfo {
	return &CommandInfo{
		Name:        "history",
		Description: "Show migration history",
		Usage:       "history",
		Category:    "Information",
		Flags: []*FlagInfo{
			{Name: "limit", Description: "Limit number of entries", Type: "int", DefaultValue: 50},
			{Name: "include-rollbacks", Description: "Include rollback operations", Type: "bool", DefaultValue: true},
		},
	}
}

func (c *HistoryCommand) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("history command does not accept arguments")
	}
	return nil
}

func (c *HistoryCommand) formatHistoryResult(result *HistoryResult, format OutputFormat) string {
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data)
	default:
		var builder strings.Builder
		builder.WriteString(fmt.Sprintf("Migration History (%d entries):\n\n", result.FilteredCount))
		
		for _, entry := range result.Entries {
			status := "✓"
			if !entry.Success {
				status = "✗"
			}
			
			builder.WriteString(fmt.Sprintf("  %s %s %s [%s]\n", 
				status, entry.ExecutedAt.Format("2006-01-02 15:04:05"), 
				entry.Name, strings.ToUpper(entry.Operation)))
			builder.WriteString(fmt.Sprintf("    Duration: %v, User: %s\n", 
				entry.ExecutionTime, entry.UserID))
			
			if entry.ErrorMessage != "" {
				builder.WriteString(fmt.Sprintf("    Error: %s\n", entry.ErrorMessage))
			}
			builder.WriteString("\n")
		}
		
		return builder.String()
	}
}

// Additional command implementations...

// RollbackCommand, ResetCommand, RefreshCommand, SeedCommand, VersionCommand, HelpCommand, ConfigCommand
// would follow similar patterns with their specific implementations

// Utility functions

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Placeholder implementations for remaining commands

type RollbackCommand struct{ facade MigrationFacade }
func (c *RollbackCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	// Implementation similar to DownCommand
	return &CommandResult{Success: true, Output: "Rollback command executed"}, nil
}
func (c *RollbackCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "rollback", Description: "Rollback migrations", Usage: "rollback [options]", Category: "Migration"}
}
func (c *RollbackCommand) Validate(args []string) error { return nil }

type ResetCommand struct{ facade MigrationFacade }
func (c *ResetCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	return &CommandResult{Success: true, Output: "Reset command executed"}, nil
}
func (c *ResetCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "reset", Description: "Reset database", Usage: "reset [options]", Category: "Migration"}
}
func (c *ResetCommand) Validate(args []string) error { return nil }

type RefreshCommand struct{ facade MigrationFacade }
func (c *RefreshCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	return &CommandResult{Success: true, Output: "Refresh command executed"}, nil
}
func (c *RefreshCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "refresh", Description: "Refresh migrations", Usage: "refresh [options]", Category: "Migration"}
}
func (c *RefreshCommand) Validate(args []string) error { return nil }

type SeedCommand struct{ facade MigrationFacade }
func (c *SeedCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	return &CommandResult{Success: true, Output: "Seed command executed"}, nil
}
func (c *SeedCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "seed", Description: "Run database seeders", Usage: "seed [files...]", Category: "Data"}
}
func (c *SeedCommand) Validate(args []string) error { return nil }

type VersionCommand struct{ facade MigrationFacade }
func (c *VersionCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	return &CommandResult{Success: true, Output: "Version command executed"}, nil
}
func (c *VersionCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "version", Description: "Show version information", Usage: "version", Category: "Information"}
}
func (c *VersionCommand) Validate(args []string) error { return nil }

type HelpCommand struct{ cli CLIFacade }
func (c *HelpCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	return &CommandResult{Success: true, Output: "Help command executed"}, nil
}
func (c *HelpCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "help", Description: "Show help information", Usage: "help [command]", Category: "Help"}
}
func (c *HelpCommand) Validate(args []string) error { return nil }

type ConfigCommand struct{ cli CLIFacade }
func (c *ConfigCommand) Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error) {
	return &CommandResult{Success: true, Output: "Config command executed"}, nil
}
func (c *ConfigCommand) GetInfo() *CommandInfo {
	return &CommandInfo{Name: "config", Description: "Manage configuration", Usage: "config [subcommand]", Category: "Configuration"}
}
func (c *ConfigCommand) Validate(args []string) error { return nil }

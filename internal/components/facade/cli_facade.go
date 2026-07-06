/**
 * @file cli_facade.go
 * @description CLI facade implementation with command parsing, execution, and comprehensive CLI operations
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/facade/cli_facade.go
 */

package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultCLIFacade implements CLIFacade interface
type DefaultCLIFacade struct {
	config          *CLIConfig
	commands        map[string]CommandExecutor
	migrationFacade MigrationFacade
	output          io.Writer
	errorOutput     io.Writer
	history         []*CommandExecution
	sessionID       string
	userID          string
}

// NewDefaultCLIFacade creates a new CLI facade instance
func NewDefaultCLIFacade(migrationFacade MigrationFacade, config *CLIConfig) *DefaultCLIFacade {
	if config == nil {
		config = DefaultCLIConfig()
	}

	facade := &DefaultCLIFacade{
		config:          config,
		commands:        make(map[string]CommandExecutor),
		migrationFacade: migrationFacade,
		output:          os.Stdout,
		errorOutput:     os.Stderr,
		history:         make([]*CommandExecution, 0),
		sessionID:       generateSessionID(),
		userID:          getCurrentUser(),
	}

	// Register built-in commands
	facade.registerBuiltinCommands()

	return facade
}

// ExecuteCommand executes a parsed command
func (f *DefaultCLIFacade) ExecuteCommand(ctx context.Context, command *Command) (*CommandResult, error) {
	startTime := time.Now()

	// Validate command
	if err := f.ValidateCommand(command); err != nil {
		return &CommandResult{
			Success:       false,
			ExitCode:      1,
			ErrorOutput:   err.Error(),
			ExecutionTime: time.Since(startTime),
			Command:       command,
		}, err
	}

	// Get command executor
	executor, exists := f.commands[command.Name]
	if !exists {
		err := fmt.Errorf("unknown command: %s", command.Name)
		return &CommandResult{
			Success:       false,
			ExitCode:      1,
			ErrorOutput:   err.Error(),
			ExecutionTime: time.Since(startTime),
			Command:       command,
		}, err
	}

	// Prepare execution options
	options := &ExecutionOptions{
		DryRun:       getBoolFlag(command.Flags, "dry-run", false),
		Verbose:      getBoolFlag(command.Flags, "verbose", false) || f.config.Verbosity >= VerbosityVerbose,
		Timeout:      command.Timeout,
		Environment:  command.Environment,
		Variables:    convertFlagsToVariables(command.Flags),
		OutputFormat: f.config.OutputFormat,
		Interactive:  f.config.Interactive,
	}

	if options.Timeout == 0 {
		options.Timeout = f.config.Timeout
	}

	// Execute command
	result, err := executor.Execute(ctx, command.Args, options)
	if result == nil {
		result = &CommandResult{
			Success:       false,
			ExitCode:      1,
			ExecutionTime: time.Since(startTime),
			Command:       command,
		}
	}

	result.Command = command
	if result.ExecutionTime == 0 {
		result.ExecutionTime = time.Since(startTime)
	}

	// Record execution in history
	execution := &CommandExecution{
		ID:        generateExecutionID(),
		Command:   command,
		Result:    result,
		StartTime: startTime,
		EndTime:   time.Now(),
		Duration:  result.ExecutionTime,
		UserID:    f.userID,
		SessionID: f.sessionID,
	}
	f.history = append(f.history, execution)

	// Format and output result
	f.outputResult(result, options.OutputFormat)

	return result, err
}

// ParseCommand parses command line arguments into a Command structure
func (f *DefaultCLIFacade) ParseCommand(args []string) (*Command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	command := &Command{
		Name:        args[0],
		Args:        make([]string, 0),
		Flags:       make(map[string]string),
		Environment: f.config.Environment,
		WorkingDir:  getCurrentWorkingDir(),
		Timeout:     f.config.Timeout,
	}

	// Parse arguments and flags
	for i := 1; i < len(args); i++ {
		arg := args[i]

		if strings.HasPrefix(arg, "--") {
			// Long flag
			flagName := strings.TrimPrefix(arg, "--")
			if strings.Contains(flagName, "=") {
				parts := strings.SplitN(flagName, "=", 2)
				command.Flags[parts[0]] = parts[1]
			} else {
				// Check if next argument is the value
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					command.Flags[flagName] = args[i]
				} else {
					command.Flags[flagName] = "true"
				}
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			// Short flag
			flagName := strings.TrimPrefix(arg, "-")
			if len(flagName) == 1 {
				// Single character flag
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					command.Flags[flagName] = args[i]
				} else {
					command.Flags[flagName] = "true"
				}
			} else {
				// Multiple character short flag or combined flags
				command.Flags[flagName] = "true"
			}
		} else {
			// Regular argument
			command.Args = append(command.Args, arg)
		}
	}

	// Apply flag aliases and defaults
	f.applyFlagAliases(command)
	f.applyFlagDefaults(command)

	return command, nil
}

// ValidateCommand validates a command structure
func (f *DefaultCLIFacade) ValidateCommand(command *Command) error {
	if command == nil {
		return fmt.Errorf("command cannot be nil")
	}

	if command.Name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	// Check if command exists
	executor, exists := f.commands[command.Name]
	if !exists {
		return fmt.Errorf("unknown command: %s", command.Name)
	}

	// Validate command arguments
	if err := executor.Validate(command.Args); err != nil {
		return fmt.Errorf("command validation failed: %w", err)
	}

	// Validate flags
	if err := f.validateFlags(command); err != nil {
		return fmt.Errorf("flag validation failed: %w", err)
	}

	return nil
}

// GetHelp returns help information for a command
func (f *DefaultCLIFacade) GetHelp(command string) (*HelpInfo, error) {
	if command == "" {
		return f.getGeneralHelp(), nil
	}

	executor, exists := f.commands[command]
	if !exists {
		return nil, fmt.Errorf("unknown command: %s", command)
	}

	info := executor.GetInfo()
	help := &HelpInfo{
		Command:     info.Name,
		Description: info.Description,
		Usage:       info.Usage,
		Flags:       info.Flags,
		Examples:    f.getCommandExamples(command),
		SeeAlso:     f.getRelatedCommands(command),
	}

	return help, nil
}

// GetUsage returns usage string for a command
func (f *DefaultCLIFacade) GetUsage(command string) string {
	if command == "" {
		return "migration-tool <command> [options] [arguments]"
	}

	executor, exists := f.commands[command]
	if !exists {
		return fmt.Sprintf("Unknown command: %s", command)
	}

	info := executor.GetInfo()
	return info.Usage
}

// ListCommands returns a list of available commands
func (f *DefaultCLIFacade) ListCommands() []*CommandInfo {
	commands := make([]*CommandInfo, 0, len(f.commands))

	for _, executor := range f.commands {
		info := executor.GetInfo()
		if !info.IsHidden {
			commands = append(commands, info)
		}
	}

	// Sort commands by category and name
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].Category != commands[j].Category {
			return commands[i].Category < commands[j].Category
		}
		return commands[i].Name < commands[j].Name
	})

	return commands
}

// LoadConfig loads configuration from a file
func (f *DefaultCLIFacade) LoadConfig(path string) error {
	if path == "" {
		path = f.config.ConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config CLIConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	f.config = &config
	return nil
}

// SaveConfig saves configuration to a file
func (f *DefaultCLIFacade) SaveConfig(path string) error {
	if path == "" {
		path = f.config.ConfigPath
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(f.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetConfig returns the current configuration
func (f *DefaultCLIFacade) GetConfig() *CLIConfig {
	return f.config
}

// SetOutputFormat sets the output format
func (f *DefaultCLIFacade) SetOutputFormat(format OutputFormat) error {
	switch format {
	case OutputFormatText, OutputFormatJSON, OutputFormatYAML, OutputFormatXML, OutputFormatCSV, OutputFormatTable:
		f.config.OutputFormat = format
		return nil
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}

// SetVerbosity sets the verbosity level
func (f *DefaultCLIFacade) SetVerbosity(level VerbosityLevel) error {
	if level < VerbosityQuiet || level > VerbosityDebug {
		return fmt.Errorf("invalid verbosity level: %d", level)
	}
	f.config.Verbosity = level
	return nil
}

// SetOutput sets the output writer
func (f *DefaultCLIFacade) SetOutput(writer io.Writer) error {
	if writer == nil {
		return fmt.Errorf("output writer cannot be nil")
	}
	f.output = writer
	return nil
}

// RegisterCommand registers a new command executor
func (f *DefaultCLIFacade) RegisterCommand(name string, executor CommandExecutor) error {
	if name == "" {
		return fmt.Errorf("command name cannot be empty")
	}
	if executor == nil {
		return fmt.Errorf("command executor cannot be nil")
	}

	f.commands[name] = executor
	return nil
}

// UnregisterCommand removes a command executor
func (f *DefaultCLIFacade) UnregisterCommand(name string) error {
	if _, exists := f.commands[name]; !exists {
		return fmt.Errorf("command not found: %s", name)
	}
	delete(f.commands, name)
	return nil
}

// GetHistory returns command execution history
func (f *DefaultCLIFacade) GetHistory() []*CommandExecution {
	return f.history
}

// ClearHistory clears command execution history
func (f *DefaultCLIFacade) ClearHistory() error {
	f.history = make([]*CommandExecution, 0)
	return nil
}

// Private methods

// registerBuiltinCommands registers built-in migration commands
func (f *DefaultCLIFacade) registerBuiltinCommands() {
	// Traditional migration commands
	f.commands["up"] = &UpCommand{facade: f.migrationFacade}
	f.commands["down"] = &DownCommand{facade: f.migrationFacade}
	f.commands["status"] = &StatusCommand{facade: f.migrationFacade}
	f.commands["reset"] = &ResetCommand{facade: f.migrationFacade}
	f.commands["refresh"] = &RefreshCommand{facade: f.migrationFacade}
	f.commands["create"] = &CreateCommand{facade: f.migrationFacade}
	f.commands["rollback"] = &RollbackCommand{facade: f.migrationFacade}
	f.commands["seed"] = &SeedCommand{facade: f.migrationFacade}
	f.commands["list"] = &ListCommand{facade: f.migrationFacade}
	f.commands["validate"] = &ValidateCommand{facade: f.migrationFacade}
	f.commands["history"] = &HistoryCommand{facade: f.migrationFacade}
	f.commands["version"] = &VersionCommand{facade: f.migrationFacade}
	f.commands["help"] = &HelpCommand{cli: f}
	f.commands["config"] = &ConfigCommand{cli: f}

	// State-based migration commands
	f.commands["state:init"] = &StateInitCommand{facade: f.migrationFacade}
	f.commands["state:validate"] = &StateValidateCommand{facade: f.migrationFacade}
	f.commands["state:preview"] = &StatePreviewCommand{facade: f.migrationFacade}
	f.commands["state:plan"] = &StatePlanCommand{facade: f.migrationFacade}
	f.commands["state:apply"] = &StateApplyCommand{facade: f.migrationFacade}
	f.commands["state:sync"] = &StateSyncCommand{facade: f.migrationFacade}
	f.commands["state:export"] = &StateExportCommand{facade: f.migrationFacade}
	f.commands["state:check"] = &StateCheckCommand{facade: f.migrationFacade}
}

// applyFlagAliases applies flag aliases
func (f *DefaultCLIFacade) applyFlagAliases(command *Command) {
	aliases := map[string]string{
		"v": "verbose",
		"h": "help",
		"q": "quiet",
		"f": "force",
		"d": "dry-run",
		"e": "environment",
		"t": "timeout",
		"o": "output",
	}

	for alias, fullName := range aliases {
		if value, exists := command.Flags[alias]; exists {
			command.Flags[fullName] = value
			delete(command.Flags, alias)
		}
	}
}

// applyFlagDefaults applies default flag values
func (f *DefaultCLIFacade) applyFlagDefaults(command *Command) {
	defaults := map[string]string{
		"environment": f.config.Environment,
		"output":      string(f.config.OutputFormat),
		"timeout":     f.config.Timeout.String(),
	}

	for flag, defaultValue := range defaults {
		if _, exists := command.Flags[flag]; !exists && defaultValue != "" {
			command.Flags[flag] = defaultValue
		}
	}
}

// validateFlags validates command flags
func (f *DefaultCLIFacade) validateFlags(command *Command) error {
	// Validate timeout
	if timeoutStr, exists := command.Flags["timeout"]; exists {
		if _, err := time.ParseDuration(timeoutStr); err != nil {
			return fmt.Errorf("invalid timeout format: %s", timeoutStr)
		}
	}

	// Validate output format
	if outputStr, exists := command.Flags["output"]; exists {
		format := OutputFormat(outputStr)
		switch format {
		case OutputFormatText, OutputFormatJSON, OutputFormatYAML, OutputFormatXML, OutputFormatCSV, OutputFormatTable:
			// Valid format
		default:
			return fmt.Errorf("invalid output format: %s", outputStr)
		}
	}

	return nil
}

// outputResult formats and outputs command result
func (f *DefaultCLIFacade) outputResult(result *CommandResult, format OutputFormat) {
	if f.config.Verbosity == VerbosityQuiet && result.Success {
		return
	}

	var output string
	switch format {
	case OutputFormatJSON:
		data, _ := json.MarshalIndent(result, "", "  ")
		output = string(data)
	case OutputFormatText:
		output = f.formatTextOutput(result)
	default:
		output = f.formatTextOutput(result)
	}

	if result.Success {
		fmt.Fprint(f.output, output)
	} else {
		fmt.Fprint(f.errorOutput, output)
	}
}

// formatTextOutput formats result as text
func (f *DefaultCLIFacade) formatTextOutput(result *CommandResult) string {
	var builder strings.Builder

	if result.Output != "" {
		builder.WriteString(result.Output)
		builder.WriteString("\n")
	}

	if result.ErrorOutput != "" {
		builder.WriteString("Error: ")
		builder.WriteString(result.ErrorOutput)
		builder.WriteString("\n")
	}

	if f.config.Verbosity >= VerbosityVerbose {
		builder.WriteString(fmt.Sprintf("Execution time: %v\n", result.ExecutionTime))
		builder.WriteString(fmt.Sprintf("Exit code: %d\n", result.ExitCode))
	}

	return builder.String()
}

// getGeneralHelp returns general help information
func (f *DefaultCLIFacade) getGeneralHelp() *HelpInfo {
	return &HelpInfo{
		Command:     "",
		Description: "Database migration tool",
		Usage:       "migration-tool <command> [options] [arguments]",
		Examples: []string{
			"migration-tool up",
			"migration-tool down --steps 1",
			"migration-tool status",
			"migration-tool create add_users_table",
		},
		SubCommands: f.ListCommands(),
	}
}

// getCommandExamples returns examples for a command
func (f *DefaultCLIFacade) getCommandExamples(command string) []string {
	examples := map[string][]string{
		"up": {
			"migration-tool up",
			"migration-tool up --target 20231201_120000",
			"migration-tool up --dry-run",
		},
		"down": {
			"migration-tool down",
			"migration-tool down --steps 1",
			"migration-tool down --target 20231201_120000",
		},
		"create": {
			"migration-tool create add_users_table",
			"migration-tool create add_users_table --template sql",
			"migration-tool create add_users_table --author 'John Doe'",
		},
		"status": {
			"migration-tool status",
			"migration-tool status --output json",
		},
	}

	return examples[command]
}

// getRelatedCommands returns related commands
func (f *DefaultCLIFacade) getRelatedCommands(command string) []string {
	related := map[string][]string{
		"up":       {"down", "status", "rollback"},
		"down":     {"up", "status", "rollback"},
		"create":   {"up", "list", "validate"},
		"status":   {"up", "down", "list"},
		"rollback": {"up", "down", "status"},
	}

	return related[command]
}

// Utility functions

// getBoolFlag gets a boolean flag value
func getBoolFlag(flags map[string]string, name string, defaultValue bool) bool {
	if value, exists := flags[name]; exists {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
		// If value is present but not parseable, treat as true
		return true
	}
	return defaultValue
}

// convertFlagsToVariables converts flags to variables map
func convertFlagsToVariables(flags map[string]string) map[string]string {
	variables := make(map[string]string)
	for key, value := range flags {
		if strings.HasPrefix(key, "var-") {
			varName := strings.TrimPrefix(key, "var-")
			variables[varName] = value
		}
	}
	return variables
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

// generateExecutionID generates a unique execution ID
func generateExecutionID() string {
	return fmt.Sprintf("exec_%d", time.Now().UnixNano())
}

// getCurrentUser gets the current user
func getCurrentUser() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}
	return "unknown"
}

// getCurrentWorkingDir gets the current working directory
func getCurrentWorkingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// DefaultCLIConfig returns default CLI configuration
func DefaultCLIConfig() *CLIConfig {
	return &CLIConfig{
		OutputFormat:  OutputFormatText,
		Verbosity:     VerbosityNormal,
		ColorOutput:   true,
		Interactive:   true,
		Timeout:       30 * time.Second,
		Environment:   "development",
		ConfigPath:    filepath.Join(os.Getenv("HOME"), ".migration-tool", "config.json"),
		LogLevel:      "info",
		LogFile:       "",
	}
}

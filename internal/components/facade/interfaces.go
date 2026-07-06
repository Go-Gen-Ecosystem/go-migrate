/**
 * @file interfaces.go
 * @description Facade interfaces for CLI and migration operations with comprehensive command patterns
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/facade/interfaces.go
 */

package facade

import (
	"context"
	"io"
	"time"
)

// MigrationFacade provides a simplified interface for migration operations
type MigrationFacade interface {
	// Migration execution
	Up(ctx context.Context, options *MigrationOptions) (*MigrationResult, error)
	Down(ctx context.Context, options *MigrationOptions) (*MigrationResult, error)
	Status(ctx context.Context) (*MigrationStatus, error)
	Reset(ctx context.Context, options *ResetOptions) (*MigrationResult, error)
	Refresh(ctx context.Context, options *RefreshOptions) (*MigrationResult, error)

	// Migration management
	Create(ctx context.Context, name string, options *CreateOptions) (*CreateResult, error)
	Rollback(ctx context.Context, options *RollbackOptions) (*MigrationResult, error)
	Seed(ctx context.Context, options *SeedOptions) (*SeedResult, error)

	// Information and validation
	List(ctx context.Context, options *ListOptions) (*ListResult, error)
	Validate(ctx context.Context, options *ValidateOptions) (*ValidationResult, error)
	History(ctx context.Context, options *HistoryOptions) (*HistoryResult, error)

	// Utility operations
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
	IsLocked(ctx context.Context) (bool, error)
	Version(ctx context.Context) (*VersionInfo, error)

	// State-based migrations
	GetStateBasedMigrator() (interface{}, error)
}

// CLIFacade provides command-line interface operations
type CLIFacade interface {
	// Command execution
	ExecuteCommand(ctx context.Context, command *Command) (*CommandResult, error)
	ParseCommand(args []string) (*Command, error)
	ValidateCommand(command *Command) error

	// Help and documentation
	GetHelp(command string) (*HelpInfo, error)
	GetUsage(command string) string
	ListCommands() []*CommandInfo

	// Configuration
	LoadConfig(path string) error
	SaveConfig(path string) error
	GetConfig() *CLIConfig

	// Output formatting
	SetOutputFormat(format OutputFormat) error
	SetVerbosity(level VerbosityLevel) error
	SetOutput(writer io.Writer) error
}

// CommandFacade provides command pattern implementation
type CommandFacade interface {
	// Command management
	RegisterCommand(name string, command CommandExecutor) error
	UnregisterCommand(name string) error
	GetCommand(name string) (CommandExecutor, error)
	ListCommands() []string

	// Command execution
	Execute(ctx context.Context, name string, args []string) (*CommandResult, error)
	ExecuteWithOptions(ctx context.Context, name string, options *ExecutionOptions) (*CommandResult, error)

	// Command history
	GetHistory() []*CommandExecution
	ClearHistory() error
	UndoLastCommand(ctx context.Context) error
}

// DatabaseFacade provides database operation abstractions
type DatabaseFacade interface {
	// Connection management
	Connect(ctx context.Context, config *DatabaseConfig) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
	IsConnected() bool

	// Schema operations
	CreateSchema(ctx context.Context, name string) error
	DropSchema(ctx context.Context, name string) error
	SchemaExists(ctx context.Context, name string) (bool, error)

	// Table operations
	CreateTable(ctx context.Context, definition *TableDefinition) error
	DropTable(ctx context.Context, name string) error
	TableExists(ctx context.Context, name string) (bool, error)
	GetTableInfo(ctx context.Context, name string) (*TableInfo, error)

	// Migration tracking
	CreateMigrationTable(ctx context.Context) error
	GetAppliedMigrations(ctx context.Context) ([]*AppliedMigration, error)
	RecordMigration(ctx context.Context, migration *MigrationRecord) error
	RemoveMigrationRecord(ctx context.Context, version string) error
}

// ConfigurationFacade provides configuration management
type ConfigurationFacade interface {
	// Configuration loading
	Load(path string) error
	LoadFromEnvironment() error
	LoadFromDefaults() error

	// Configuration access
	Get(key string) (interface{}, error)
	GetString(key string) (string, error)
	GetInt(key string) (int, error)
	GetBool(key string) (bool, error)
	GetDuration(key string) (time.Duration, error)

	// Configuration modification
	Set(key string, value interface{}) error
	Unset(key string) error
	Save(path string) error

	// Validation
	Validate() error
	GetValidationErrors() []error
}

// Data structures

// MigrationOptions configuration for migration operations
type MigrationOptions struct {
	Target      string            // Target version or migration name
	Steps       int               // Number of steps to execute
	DryRun      bool              // Execute without applying changes
	Force       bool              // Force execution even with warnings
	Timeout     time.Duration     // Operation timeout
	Parallel    bool              // Enable parallel execution
	Environment string            // Target environment
	Variables   map[string]string // Variable substitutions
	Verbose     bool              // Enable verbose output
}

// MigrationResult result of migration operation
type MigrationResult struct {
	Success         bool                    `json:"success"`
	ExecutedCount   int                     `json:"executed_count"`
	SkippedCount    int                     `json:"skipped_count"`
	FailedCount     int                     `json:"failed_count"`
	ExecutionTime   time.Duration           `json:"execution_time"`
	Migrations      []*ExecutedMigration    `json:"migrations"`
	Errors          []error                 `json:"errors"`
	Warnings        []string                `json:"warnings"`
	Summary         string                  `json:"summary"`
	Metadata        map[string]interface{}  `json:"metadata"`
}

// MigrationStatus current status of migrations
type MigrationStatus struct {
	TotalMigrations     int                     `json:"total_migrations"`
	AppliedMigrations   int                     `json:"applied_migrations"`
	PendingMigrations   int                     `json:"pending_migrations"`
	FailedMigrations    int                     `json:"failed_migrations"`
	LastApplied         *AppliedMigration       `json:"last_applied"`
	NextPending         *PendingMigration       `json:"next_pending"`
	DatabaseVersion     string                  `json:"database_version"`
	SchemaVersion       string                  `json:"schema_version"`
	IsLocked            bool                    `json:"is_locked"`
	LockInfo            *LockInfo               `json:"lock_info"`
	Migrations          []*MigrationInfo        `json:"migrations"`
}

// ResetOptions configuration for reset operations
type ResetOptions struct {
	ConfirmReset    bool              `json:"confirm_reset"`
	DropTables      bool              `json:"drop_tables"`
	DropSchema      bool              `json:"drop_schema"`
	Timeout         time.Duration     `json:"timeout"`
	BackupFirst     bool              `json:"backup_first"`
	BackupPath      string            `json:"backup_path"`
}

// RefreshOptions configuration for refresh operations
type RefreshOptions struct {
	SeedAfter       bool              `json:"seed_after"`
	SeedFiles       []string          `json:"seed_files"`
	Timeout         time.Duration     `json:"timeout"`
	Parallel        bool              `json:"parallel"`
	SkipValidation  bool              `json:"skip_validation"`
}

// CreateOptions configuration for creating migrations
type CreateOptions struct {
	Template        string            `json:"template"`
	Directory       string            `json:"directory"`
	Language        string            `json:"language"`
	Type            MigrationType     `json:"type"`
	Description     string            `json:"description"`
	Author          string            `json:"author"`
	Tags            []string          `json:"tags"`
	Variables       map[string]string `json:"variables"`
}

// CreateResult result of migration creation
type CreateResult struct {
	Success         bool              `json:"success"`
	FilePath        string            `json:"file_path"`
	FileName        string            `json:"file_name"`
	Version         string            `json:"version"`
	Template        string            `json:"template"`
	GeneratedAt     time.Time         `json:"generated_at"`
	Size            int64             `json:"size"`
	Checksum        string            `json:"checksum"`
}

// RollbackOptions configuration for rollback operations
type RollbackOptions struct {
	Steps           int               `json:"steps"`
	Target          string            `json:"target"`
	DryRun          bool              `json:"dry_run"`
	Force           bool              `json:"force"`
	Timeout         time.Duration     `json:"timeout"`
	ConfirmEach     bool              `json:"confirm_each"`
}

// SeedOptions configuration for seeding operations
type SeedOptions struct {
	Files           []string          `json:"files"`
	Directory       string            `json:"directory"`
	Environment     string            `json:"environment"`
	Force           bool              `json:"force"`
	Timeout         time.Duration     `json:"timeout"`
	Parallel        bool              `json:"parallel"`
	Variables       map[string]string `json:"variables"`
}

// SeedResult result of seeding operation
type SeedResult struct {
	Success         bool              `json:"success"`
	ExecutedFiles   []string          `json:"executed_files"`
	SkippedFiles    []string          `json:"skipped_files"`
	FailedFiles     []string          `json:"failed_files"`
	ExecutionTime   time.Duration     `json:"execution_time"`
	RecordsInserted int64             `json:"records_inserted"`
	Errors          []error           `json:"errors"`
	Summary         string            `json:"summary"`
}

// ListOptions configuration for listing migrations
type ListOptions struct {
	Status          MigrationStatus   `json:"status"`
	Environment     string            `json:"environment"`
	Format          OutputFormat      `json:"format"`
	IncludeDetails  bool              `json:"include_details"`
	SortBy          string            `json:"sort_by"`
	SortOrder       SortOrder         `json:"sort_order"`
	Limit           int               `json:"limit"`
	Offset          int               `json:"offset"`
}

// ListResult result of listing migrations
type ListResult struct {
	Migrations      []*MigrationInfo  `json:"migrations"`
	TotalCount      int               `json:"total_count"`
	FilteredCount   int               `json:"filtered_count"`
	HasMore         bool              `json:"has_more"`
	Summary         *ListSummary      `json:"summary"`
}

// ValidateOptions configuration for validation operations
type ValidateOptions struct {
	CheckIntegrity  bool              `json:"check_integrity"`
	CheckSyntax     bool              `json:"check_syntax"`
	CheckDependencies bool            `json:"check_dependencies"`
	CheckConstraints bool             `json:"check_constraints"`
	Environment     string            `json:"environment"`
	Strict          bool              `json:"strict"`
}

// ValidationResult result of validation operation
type ValidationResult struct {
	IsValid         bool              `json:"is_valid"`
	Errors          []ValidationError `json:"errors"`
	Warnings        []ValidationWarning `json:"warnings"`
	CheckedCount    int               `json:"checked_count"`
	ValidCount      int               `json:"valid_count"`
	InvalidCount    int               `json:"invalid_count"`
	Summary         string            `json:"summary"`
	Details         map[string]interface{} `json:"details"`
}

// HistoryOptions configuration for history operations
type HistoryOptions struct {
	Limit           int               `json:"limit"`
	Offset          int               `json:"offset"`
	StartDate       *time.Time        `json:"start_date"`
	EndDate         *time.Time        `json:"end_date"`
	Environment     string            `json:"environment"`
	IncludeRollbacks bool             `json:"include_rollbacks"`
	Format          OutputFormat      `json:"format"`
}

// HistoryResult result of history operation
type HistoryResult struct {
	Entries         []*HistoryEntry   `json:"entries"`
	TotalCount      int               `json:"total_count"`
	FilteredCount   int               `json:"filtered_count"`
	HasMore         bool              `json:"has_more"`
	Summary         *HistorySummary   `json:"summary"`
}

// Command represents a CLI command
type Command struct {
	Name            string            `json:"name"`
	Args            []string          `json:"args"`
	Flags           map[string]string `json:"flags"`
	Options         *CommandOptions   `json:"options"`
	Environment     string            `json:"environment"`
	WorkingDir      string            `json:"working_dir"`
	Timeout         time.Duration     `json:"timeout"`
}

// CommandResult result of command execution
type CommandResult struct {
	Success         bool              `json:"success"`
	ExitCode        int               `json:"exit_code"`
	Output          string            `json:"output"`
	ErrorOutput     string            `json:"error_output"`
	ExecutionTime   time.Duration     `json:"execution_time"`
	Command         *Command          `json:"command"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// CommandExecutor interface for command execution
type CommandExecutor interface {
	Execute(ctx context.Context, args []string, options *ExecutionOptions) (*CommandResult, error)
	GetInfo() *CommandInfo
	Validate(args []string) error
}

// ExecutionOptions options for command execution
type ExecutionOptions struct {
	DryRun          bool              `json:"dry_run"`
	Verbose         bool              `json:"verbose"`
	Timeout         time.Duration     `json:"timeout"`
	Environment     string            `json:"environment"`
	Variables       map[string]string `json:"variables"`
	OutputFormat    OutputFormat      `json:"output_format"`
	Interactive     bool              `json:"interactive"`
}

// CommandExecution represents a command execution record
type CommandExecution struct {
	ID              string            `json:"id"`
	Command         *Command          `json:"command"`
	Result          *CommandResult    `json:"result"`
	StartTime       time.Time         `json:"start_time"`
	EndTime         time.Time         `json:"end_time"`
	Duration        time.Duration     `json:"duration"`
	UserID          string            `json:"user_id"`
	SessionID       string            `json:"session_id"`
}

// HelpInfo provides help information for commands
type HelpInfo struct {
	Command         string            `json:"command"`
	Description     string            `json:"description"`
	Usage           string            `json:"usage"`
	Examples        []string          `json:"examples"`
	Flags           []*FlagInfo       `json:"flags"`
	SubCommands     []*CommandInfo    `json:"sub_commands"`
	SeeAlso         []string          `json:"see_also"`
}

// CommandInfo provides information about a command
type CommandInfo struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Usage           string            `json:"usage"`
	Category        string            `json:"category"`
	Aliases         []string          `json:"aliases"`
	Flags           []*FlagInfo       `json:"flags"`
	IsHidden        bool              `json:"is_hidden"`
	IsDeprecated    bool              `json:"is_deprecated"`
}

// FlagInfo provides information about command flags
type FlagInfo struct {
	Name            string            `json:"name"`
	ShortName       string            `json:"short_name"`
	Description     string            `json:"description"`
	Type            string            `json:"type"`
	DefaultValue    interface{}       `json:"default_value"`
	Required        bool              `json:"required"`
	Hidden          bool              `json:"hidden"`
}

// CLIConfig configuration for CLI operations
type CLIConfig struct {
	OutputFormat    OutputFormat      `json:"output_format"`
	Verbosity       VerbosityLevel    `json:"verbosity"`
	ColorOutput     bool              `json:"color_output"`
	Interactive     bool              `json:"interactive"`
	Timeout         time.Duration     `json:"timeout"`
	Environment     string            `json:"environment"`
	ConfigPath      string            `json:"config_path"`
	LogLevel        string            `json:"log_level"`
	LogFile         string            `json:"log_file"`
}

// DatabaseConfig configuration for database connections
type DatabaseConfig struct {
	Driver          string            `json:"driver"`
	Host            string            `json:"host"`
	Port            int               `json:"port"`
	Database        string            `json:"database"`
	Username        string            `json:"username"`
	Password        string            `json:"password"`
	SSLMode         string            `json:"ssl_mode"`
	Timeout         time.Duration     `json:"timeout"`
	MaxConnections  int               `json:"max_connections"`
	MaxIdleTime     time.Duration     `json:"max_idle_time"`
	Parameters      map[string]string `json:"parameters"`
}

// TableDefinition defines a database table structure
type TableDefinition struct {
	Name            string            `json:"name"`
	Schema          string            `json:"schema"`
	Columns         []*ColumnDefinition `json:"columns"`
	Indexes         []*IndexDefinition `json:"indexes"`
	Constraints     []*ConstraintDefinition `json:"constraints"`
	Options         map[string]interface{} `json:"options"`
}

// ColumnDefinition defines a table column
type ColumnDefinition struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Size            int               `json:"size"`
	Precision       int               `json:"precision"`
	Scale           int               `json:"scale"`
	Nullable        bool              `json:"nullable"`
	DefaultValue    interface{}       `json:"default_value"`
	AutoIncrement   bool              `json:"auto_increment"`
	PrimaryKey      bool              `json:"primary_key"`
	Unique          bool              `json:"unique"`
	Comment         string            `json:"comment"`
}

// IndexDefinition defines a table index
type IndexDefinition struct {
	Name            string            `json:"name"`
	Columns         []string          `json:"columns"`
	Unique          bool              `json:"unique"`
	Type            string            `json:"type"`
	Where           string            `json:"where"`
	Options         map[string]interface{} `json:"options"`
}

// ConstraintDefinition defines a table constraint
type ConstraintDefinition struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Columns         []string          `json:"columns"`
	ReferencedTable string            `json:"referenced_table"`
	ReferencedColumns []string        `json:"referenced_columns"`
	OnUpdate        string            `json:"on_update"`
	OnDelete        string            `json:"on_delete"`
	CheckExpression string            `json:"check_expression"`
}

// TableInfo provides information about a table
type TableInfo struct {
	Name            string            `json:"name"`
	Schema          string            `json:"schema"`
	RowCount        int64             `json:"row_count"`
	Size            int64             `json:"size"`
	Columns         []*ColumnInfo     `json:"columns"`
	Indexes         []*IndexInfo      `json:"indexes"`
	Constraints     []*ConstraintInfo `json:"constraints"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// ColumnInfo provides information about a column
type ColumnInfo struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Size            int               `json:"size"`
	Nullable        bool              `json:"nullable"`
	DefaultValue    interface{}       `json:"default_value"`
	Position        int               `json:"position"`
	Comment         string            `json:"comment"`
}

// IndexInfo provides information about an index
type IndexInfo struct {
	Name            string            `json:"name"`
	Columns         []string          `json:"columns"`
	Unique          bool              `json:"unique"`
	Type            string            `json:"type"`
	Size            int64             `json:"size"`
}

// ConstraintInfo provides information about a constraint
type ConstraintInfo struct {
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Columns         []string          `json:"columns"`
	ReferencedTable string            `json:"referenced_table"`
	ReferencedColumns []string        `json:"referenced_columns"`
}

// AppliedMigration represents an applied migration
type AppliedMigration struct {
	Version         string            `json:"version"`
	Name            string            `json:"name"`
	AppliedAt       time.Time         `json:"applied_at"`
	ExecutionTime   time.Duration     `json:"execution_time"`
	Checksum        string            `json:"checksum"`
	Success         bool              `json:"success"`
	ErrorMessage    string            `json:"error_message"`
}

// MigrationRecord represents a migration record for tracking
type MigrationRecord struct {
	Version         string            `json:"version"`
	Name            string            `json:"name"`
	Checksum        string            `json:"checksum"`
	AppliedAt       time.Time         `json:"applied_at"`
	ExecutionTime   time.Duration     `json:"execution_time"`
	Success         bool              `json:"success"`
	ErrorMessage    string            `json:"error_message"`
	RollbackSQL     string            `json:"rollback_sql"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// Enums and constants

// MigrationType defines the type of migration
type MigrationType string

const (
	MigrationTypeSQL        MigrationType = "sql"
	MigrationTypeGo         MigrationType = "go"
	MigrationTypeJavaScript MigrationType = "javascript"
	MigrationTypePython     MigrationType = "python"
)

// OutputFormat defines the output format for commands
type OutputFormat string

const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
	OutputFormatYAML OutputFormat = "yaml"
	OutputFormatXML  OutputFormat = "xml"
	OutputFormatCSV  OutputFormat = "csv"
	OutputFormatTable OutputFormat = "table"
)

// VerbosityLevel defines the verbosity level for output
type VerbosityLevel int

const (
	VerbosityQuiet VerbosityLevel = iota
	VerbosityNormal
	VerbosityVerbose
	VerbosityDebug
)

// SortOrder defines the sort order
type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// Additional supporting types

// ExecutedMigration represents a migration that was executed
type ExecutedMigration struct {
	Version         string            `json:"version"`
	Name            string            `json:"name"`
	Direction       string            `json:"direction"` // up or down
	ExecutionTime   time.Duration     `json:"execution_time"`
	Success         bool              `json:"success"`
	ErrorMessage    string            `json:"error_message"`
	SQL             string            `json:"sql"`
	Checksum        string            `json:"checksum"`
}

// PendingMigration represents a migration that is pending execution
type PendingMigration struct {
	Version         string            `json:"version"`
	Name            string            `json:"name"`
	FilePath        string            `json:"file_path"`
	Size            int64             `json:"size"`
	Checksum        string            `json:"checksum"`
	Dependencies    []string          `json:"dependencies"`
}

// MigrationInfo provides comprehensive information about a migration
type MigrationInfo struct {
	Version         string            `json:"version"`
	Name            string            `json:"name"`
	Status          string            `json:"status"` // applied, pending, failed
	FilePath        string            `json:"file_path"`
	Size            int64             `json:"size"`
	Checksum        string            `json:"checksum"`
	AppliedAt       *time.Time        `json:"applied_at"`
	ExecutionTime   *time.Duration    `json:"execution_time"`
	ErrorMessage    string            `json:"error_message"`
	Dependencies    []string          `json:"dependencies"`
	Tags            []string          `json:"tags"`
	Author          string            `json:"author"`
	Description     string            `json:"description"`
}

// LockInfo provides information about migration locks
type LockInfo struct {
	IsLocked        bool              `json:"is_locked"`
	LockedAt        time.Time         `json:"locked_at"`
	LockedBy        string            `json:"locked_by"`
	LockReason      string            `json:"lock_reason"`
	ProcessID       int               `json:"process_id"`
	Hostname        string            `json:"hostname"`
}

// VersionInfo provides version information
type VersionInfo struct {
	Version         string            `json:"version"`
	BuildDate       string            `json:"build_date"`
	GitCommit       string            `json:"git_commit"`
	GoVersion       string            `json:"go_version"`
	Platform        string            `json:"platform"`
	DatabaseVersion string            `json:"database_version"`
	SchemaVersion   string            `json:"schema_version"`
}

// ListSummary provides summary information for list operations
type ListSummary struct {
	TotalMigrations   int             `json:"total_migrations"`
	AppliedMigrations int             `json:"applied_migrations"`
	PendingMigrations int             `json:"pending_migrations"`
	FailedMigrations  int             `json:"failed_migrations"`
	LastApplied       string          `json:"last_applied"`
	NextPending       string          `json:"next_pending"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Migration       string            `json:"migration"`
	Type            string            `json:"type"`
	Message         string            `json:"message"`
	Line            int               `json:"line"`
	Column          int               `json:"column"`
	Severity        string            `json:"severity"`
	Code            string            `json:"code"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Migration       string            `json:"migration"`
	Type            string            `json:"type"`
	Message         string            `json:"message"`
	Line            int               `json:"line"`
	Column          int               `json:"column"`
	Code            string            `json:"code"`
}

// HistoryEntry represents a history entry
type HistoryEntry struct {
	ID              string            `json:"id"`
	Version         string            `json:"version"`
	Name            string            `json:"name"`
	Operation       string            `json:"operation"` // apply, rollback
	ExecutedAt      time.Time         `json:"executed_at"`
	ExecutionTime   time.Duration     `json:"execution_time"`
	Success         bool              `json:"success"`
	ErrorMessage    string            `json:"error_message"`
	UserID          string            `json:"user_id"`
	Environment     string            `json:"environment"`
	Checksum        string            `json:"checksum"`
}

// HistorySummary provides summary information for history operations
type HistorySummary struct {
	TotalOperations   int             `json:"total_operations"`
	SuccessfulOps     int             `json:"successful_ops"`
	FailedOps         int             `json:"failed_ops"`
	LastOperation     *time.Time      `json:"last_operation"`
	MostActiveUser    string          `json:"most_active_user"`
	AverageExecTime   time.Duration   `json:"average_exec_time"`
}

// CommandOptions provides options for command configuration
type CommandOptions struct {
	Timeout         time.Duration     `json:"timeout"`
	Retries         int               `json:"retries"`
	RetryDelay      time.Duration     `json:"retry_delay"`
	FailFast        bool              `json:"fail_fast"`
	ContinueOnError bool              `json:"continue_on_error"`
	Parallel        bool              `json:"parallel"`
	MaxParallel     int               `json:"max_parallel"`
}

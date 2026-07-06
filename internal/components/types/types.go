/**
 * @file types.go
 * @description Internal type definitions for the migration package
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/types/types.go
 */
package types

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Note: MigrationEngine is now a concrete struct in the engine package

// MigrationTool provides the main interface for database migrations
type MigrationTool interface {
	// Status returns the current migration status
	Status(ctx context.Context) (*MigrationStatus, error)

	// Migrate runs pending migrations
	Migrate(ctx context.Context, target string, dryRun bool) error

	// Rollback rolls back migrations
	Rollback(ctx context.Context, steps int) error

	// Validate validates all migrations
	Validate(ctx context.Context) error

	// Info returns migration information
	Info(ctx context.Context) (*MigrationInfo, error)

	// Close closes the migration tool and releases resources
	Close() error
}

// StandaloneMigrationTool provides additional methods for standalone migration tools.
// The embedded MigrationTool verbs plus config/environment/plugin management form the
// tool's single public surface, used together by the CLI.
//
//nolint:interfacebloat // Cohesive standalone-tool facade (see doc comment above).
type StandaloneMigrationTool interface {
	MigrationTool

	// LoadConfig loads configuration from file
	LoadConfig(configPath string) error

	// SaveConfig saves configuration to file
	SaveConfig(configPath string) error

	// GetConfig returns the current configuration
	GetConfig() *StandaloneConfig

	// SetEnvironment sets the environment configuration
	SetEnvironment(env *EnvironmentConfig)

	// GetEnvironment returns the current environment configuration
	GetEnvironment() *EnvironmentConfig

	// SetPlugins sets the plugin configurations
	SetPlugins(plugins []PluginConfig)

	// GetPlugins returns the current plugin configurations
	GetPlugins() []PluginConfig
}

// MigrationTracker defines the interface for tracking migration state.
// History queries, success/failure recording, applied-version checks and
// schema-table setup form one transactional responsibility (as in Flyway/Liquibase
// trackers); splitting them would fracture that boundary.
//
//nolint:interfacebloat // Cohesive migration-state store (see doc comment above).
type MigrationTracker interface {
	GetExecutedMigrations(ctx context.Context) ([]MigrationRecord, error)
	GetFailedMigrations(ctx context.Context) ([]MigrationRecord, error)
	RecordMigrationSuccess(ctx context.Context, migration MigrationRecord, executionTime int64) error
	RecordMigrationFailure(ctx context.Context, migration MigrationRecord, errorMsg string) error
	RemoveMigration(ctx context.Context, migrationID string) error
	IsLocked(ctx context.Context) (bool, error)
	AcquireLock(ctx context.Context) error
	ReleaseLock(ctx context.Context) error
	EnsureSchemaTable(ctx context.Context) error
	GetCurrentVersion(ctx context.Context) (string, error)
	IsMigrationApplied(ctx context.Context, id string) (bool, error)
	GetMigration(ctx context.Context, id string) (*Migration, error)
	GetMigrationByVersion(ctx context.Context, version string) (*Migration, error)
	GetAppliedMigrations(ctx context.Context) ([]*Migration, error)
}

// MigrationExecutor defines the interface for executing migrations
type MigrationExecutor interface {
	Execute(ctx context.Context, migration *Migration) error
	ExecuteDryRun(ctx context.Context, migration *Migration) (*ExecutionPlan, error)
	Rollback(ctx context.Context, migration *Migration) error
	ValidateRollback(ctx context.Context, migration *Migration) error
	ValidateMigration(ctx context.Context, migration *Migration) error
	GetExecutionPlan(migrations []*Migration) ([]string, error)
}

// MigrationValidator defines the interface for validating migrations
type MigrationValidator interface {
	ValidateMigrations(migrations []Migration) error
	ValidateMigration(migration *Migration) error
	CheckDependencies(migrations []Migration) error
	CheckCircularDependencies(migrations []Migration) error
}

// DependencyResolver interface for resolving migration dependencies
type DependencyResolver interface {
	ResolveDependencies(ctx context.Context, migrations []*Migration) ([]*Migration, error)
}

// Config represents the migration configuration
type Config struct {
	Environment string          `json:"environment" yaml:"environment"`
	Database    DatabaseConfig  `json:"database" yaml:"database"`
	Migration   MigrationConfig `json:"migration" yaml:"migration"`
	Logging     LoggingConfig   `json:"logging" yaml:"logging"`
	Features    FeatureConfig   `json:"features" yaml:"features"`
}

// DatabaseConfig represents database connection configuration
type DatabaseConfig struct {
	Driver string `json:"driver" yaml:"driver"`
	DSN    string `json:"dsn" yaml:"dsn"`

	// Individual connection fields for DSN construction
	Host     string            `json:"host" yaml:"host"`
	Port     int               `json:"port" yaml:"port"`
	Database string            `json:"database" yaml:"database"`
	Username string            `json:"username" yaml:"username"`
	Password string            `json:"password" yaml:"password"`
	Charset  string            `json:"charset" yaml:"charset"`
	Timezone string            `json:"timezone" yaml:"timezone"`
	SSLMode  string            `json:"ssl_mode" yaml:"ssl_mode"`
	Options  map[string]string `json:"options" yaml:"options"`

	// Connection pool settings
	MaxConnections    int           `json:"max_connections" yaml:"max_connections"`
	ConnectionTimeout time.Duration `json:"connection_timeout" yaml:"connection_timeout"`
	IdleTimeout       time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
	MaxLifetime       time.Duration `json:"max_lifetime" yaml:"max_lifetime"`
}

// MigrationConfig represents migration-specific configuration
type MigrationConfig struct {
	// General settings
	TableName          string `json:"table_name" yaml:"table_name"`
	LockTableName      string `json:"lock_table_name" yaml:"lock_table_name"`
	ChangelogTableName string `json:"changelog_table_name" yaml:"changelog_table_name"`
	Schema             string `json:"schema" yaml:"schema"`
	MigrationsPath     string `json:"migrations_path" yaml:"migrations_path"`
	Environment        string `json:"environment" yaml:"environment"`

	// Execution settings
	AutoMigrate             bool `json:"auto_migrate" yaml:"auto_migrate"`
	AutoCreateTables        bool `json:"auto_create_tables" yaml:"auto_create_tables"`
	ValidateOnMigrate       bool `json:"validate_on_migrate" yaml:"validate_on_migrate"`
	ValidateOnLoad          bool `json:"validate_on_load" yaml:"validate_on_load"`
	StrictMode              bool `json:"strict_mode" yaml:"strict_mode"` // When true, performance validator fails on warnings
	OutOfOrder              bool `json:"out_of_order" yaml:"out_of_order"`
	IgnoreMissingMigrations bool `json:"ignore_missing_migrations" yaml:"ignore_missing_migrations"`
	IgnoreIgnoredMigrations bool `json:"ignore_ignored_migrations" yaml:"ignore_ignored_migrations"`
	IgnorePendingDrops      bool `json:"ignore_pending_drops" yaml:"ignore_pending_drops"`
	CleanOnValidationError  bool `json:"clean_on_validation_error" yaml:"clean_on_validation_error"`
	StopOnError             bool `json:"stop_on_error" yaml:"stop_on_error"`
	ValidateChecksums       bool `json:"validate_checksums" yaml:"validate_checksums"`

	// Transaction settings
	UseTransactions bool `json:"use_transactions" yaml:"use_transactions"`

	// Timeout settings
	LockTimeout      time.Duration `json:"lock_timeout" yaml:"lock_timeout"`
	CommandTimeout   time.Duration `json:"command_timeout" yaml:"command_timeout"`
	StatementTimeout time.Duration `json:"statement_timeout" yaml:"statement_timeout"`

	// Rollback settings
	EnableRollback    bool `json:"enable_rollback" yaml:"enable_rollback"`
	RollbackOnFailure bool `json:"rollback_on_failure" yaml:"rollback_on_failure"`
	MaxRollbackDepth  int  `json:"max_rollback_depth" yaml:"max_rollback_depth"`

	// GoMigrate changeset settings
	EnableChangelog bool   `json:"enable_changelog" yaml:"enable_changelog"`
	ChangelogFormat string `json:"changelog_format" yaml:"changelog_format"` // xml, yaml, json, sql
	EnableContexts  bool   `json:"enable_contexts" yaml:"enable_contexts"`
	EnableLabels    bool   `json:"enable_labels" yaml:"enable_labels"`

	// Advanced features
	EnableDryRun      bool   `json:"enable_dry_run" yaml:"enable_dry_run"`
	EnableBackup      bool   `json:"enable_backup" yaml:"enable_backup"`
	BackupPath        string `json:"backup_path" yaml:"backup_path"`
	EnableCompression bool   `json:"enable_compression" yaml:"enable_compression"`
	Verbose           bool   `json:"verbose" yaml:"verbose"`

	// Executor selection
	ExecutionMode string `json:"execution_mode" yaml:"execution_mode"` // sequential, batch, parallel
	BatchSize     int    `json:"batch_size" yaml:"batch_size"`
	Concurrency   int    `json:"concurrency" yaml:"concurrency"`

	// State-based migration settings
	StateSchemaPath         string `json:"state_schema_path" yaml:"state_schema_path"`
	StateSchemaFormat       string `json:"state_schema_format" yaml:"state_schema_format"` // yaml, json
	StateAutoGenerate       bool   `json:"state_auto_generate" yaml:"state_auto_generate"`
	StateAutoApply          bool   `json:"state_auto_apply" yaml:"state_auto_apply"`
	StateAllowDestructive   bool   `json:"state_allow_destructive" yaml:"state_allow_destructive"`
	StateRequireReview      bool   `json:"state_require_review" yaml:"state_require_review"`
	StateGitOpsEnabled      bool   `json:"state_gitops_enabled" yaml:"state_gitops_enabled"`
	StateMigrationOutputDir string `json:"state_migration_output_dir" yaml:"state_migration_output_dir"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level  string `json:"level" yaml:"level"`   // debug, info, warn, error
	Format string `json:"format" yaml:"format"` // json, text
	Output string `json:"output" yaml:"output"` // stdout, stderr, file
	File   string `json:"file" yaml:"file"`     // log file path when output=file
}

// FeatureConfig represents feature flags configuration
type FeatureConfig struct {
	AutoBackup          bool `json:"auto_backup" yaml:"auto_backup"`
	DryRunMode          bool `json:"dry_run_mode" yaml:"dry_run_mode"`
	ParallelExecution   bool `json:"parallel_execution" yaml:"parallel_execution"`
	HealthChecks        bool `json:"health_checks" yaml:"health_checks"`
	Monitoring          bool `json:"monitoring" yaml:"monitoring"`
	Notifications       bool `json:"notifications" yaml:"notifications"`
	StateBasedMigration bool `json:"state_based_migration" yaml:"state_based_migration"`
}

// StandaloneConfig represents configuration for the standalone migration tool
type StandaloneConfig struct {
	Database    DatabaseConfig    `json:"database" yaml:"database"`
	Migration   MigrationConfig   `json:"migration" yaml:"migration"`
	Logging     LoggingConfig     `json:"logging" yaml:"logging"`
	Features    FeatureConfig     `json:"features" yaml:"features"`
	Environment EnvironmentConfig `json:"environment" yaml:"environment"`
	Plugins     []PluginConfig    `json:"plugins" yaml:"plugins"`
}

// EnvironmentConfig represents environment-specific settings
type EnvironmentConfig struct {
	Name         string            `json:"name" yaml:"name"`
	Variables    map[string]string `json:"variables" yaml:"variables"`
	Profiles     []string          `json:"profiles" yaml:"profiles"`
	Contexts     []string          `json:"contexts" yaml:"contexts"`
	Labels       []string          `json:"labels" yaml:"labels"`
	Restrictions map[string]bool   `json:"restrictions" yaml:"restrictions"`
}

// PluginConfig represents plugin configuration
type PluginConfig struct {
	Name    string                 `json:"name" yaml:"name"`
	Enabled bool                   `json:"enabled" yaml:"enabled"`
	Config  map[string]interface{} `json:"config" yaml:"config"`
}

// MigrationType defines the type of migration (SQL, Go, XML, YAML, JSON, etc.)
type MigrationType string

// Precondition represents a condition that must be met before executing a migration
type Precondition struct {
	Type      string                 `json:"type"`
	SQL       string                 `json:"sql,omitempty"`
	Condition map[string]interface{} `json:"condition"`
	OnFail    string                 `json:"on_fail"` // HALT, CONTINUE, MARK_RAN, WARN
}

// RollbackInfo stores structured rollback metadata
type RollbackInfo struct {
	Script      string        `json:"script" gorm:"type:text"`
	ExecutedAt  *time.Time    `json:"executed_at,omitempty"`
	ExecutedBy  string        `json:"executed_by,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
	Description string        `json:"description,omitempty"`
	Type        string        `json:"type"`
	Automatic   bool          `json:"automatic"`
}

// Migration represents a single database migration and its full lifecycle metadata.
type Migration struct {
	// Basic identification
	ID          string        `json:"id" gorm:"primaryKey"`
	Version     string        `json:"version" gorm:"index"`
	Description string        `json:"description"`
	Type        MigrationType `json:"type"` // sql, go, xml, yaml, json

	// File information
	FileName string `json:"file_name"`
	FilePath string `json:"file_path"`
	Script   string `json:"script" gorm:"type:text"`
	Checksum string `json:"checksum"`

	// GoMigrate changeset attributes
	Author        string         `json:"author"`
	ChangesetID   string         `json:"changeset_id,omitempty"`
	Tags          []string       `json:"tags" gorm:"serializer:json"`
	Contexts      []string       `json:"contexts" gorm:"serializer:json"`
	Labels        []string       `json:"labels" gorm:"serializer:json"`
	Dependencies  []string       `json:"dependencies" gorm:"serializer:json"`
	Preconditions []Precondition `json:"preconditions" gorm:"serializer:json"`

	// Execution details
	State         string        `json:"state" gorm:"index"` // pending, running, success, failed, rolled_back
	ExecutedAt    *time.Time    `json:"executed_at,omitempty"`
	ExecutedBy    string        `json:"executed_by,omitempty"`
	ExecutionTime time.Duration `json:"execution_time,omitempty"`
	ErrorMessage  string        `json:"error_message,omitempty" gorm:"type:text"`

	// Rollback details
	CanRollback  bool          `json:"can_rollback"`
	RollbackSQL  string        `json:"rollback_sql,omitempty" gorm:"type:text"`
	Rollback     *RollbackInfo `json:"rollback,omitempty" gorm:"serializer:json"`
	RolledBackAt *time.Time    `json:"rolled_back_at,omitempty"`
	RolledBackBy string        `json:"rolled_back_by,omitempty"`

	// Control flags
	RunAlways   bool `json:"run_always"`
	RunOnChange bool `json:"run_on_change"`
	FailOnError bool `json:"fail_on_error"`

	// Metadata
	Comments  string            `json:"comments,omitempty"`
	Metadata  map[string]string `json:"metadata" gorm:"serializer:json"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Supported migration source formats.
const (
	MigrationTypeSQL  MigrationType = "sql"
	MigrationTypeGo   MigrationType = "go"
	MigrationTypeXML  MigrationType = "xml"
	MigrationTypeYAML MigrationType = "yaml"
	MigrationTypeJSON MigrationType = "json"
)

// ExecutionPlan represents a migration execution plan
type ExecutionPlan struct {
	Migrations    []*Migration  `json:"migrations"`
	TotalCount    int           `json:"total_count"`
	EstimatedTime time.Duration `json:"estimated_time"`
	Warnings      []string      `json:"warnings"`
	Errors        []string      `json:"errors"`
}

// ChangeSet represents a GoMigrate changeset: a named, ordered group of changes
// with optional preconditions, rollback and execution controls.
type ChangeSet struct {
	ID            string               `json:"id" yaml:"id" xml:"id,attr"`
	Author        string               `json:"author" yaml:"author" xml:"author,attr"`
	Comment       string               `json:"comment,omitempty" yaml:"comment,omitempty" xml:"comment,attr,omitempty"`
	FilePath      string               `json:"filePath" yaml:"filePath" xml:"filePath,attr"`
	Context       string               `json:"context,omitempty" yaml:"context,omitempty" xml:"context,attr,omitempty"`
	Labels        string               `json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,attr,omitempty"`
	RunAlways     bool                 `json:"runAlways,omitempty" yaml:"runAlways,omitempty" xml:"runAlways,attr,omitempty"`
	RunOnChange   bool                 `json:"runOnChange,omitempty" yaml:"runOnChange,omitempty" xml:"runOnChange,attr,omitempty"`
	FailOnError   bool                 `json:"failOnError,omitempty" yaml:"failOnError,omitempty" xml:"failOnError,attr,omitempty"`
	Changes       []Change             `json:"changes" yaml:"changes" xml:"changes"`
	Rollback      *ChangeRollback      `json:"rollback,omitempty" yaml:"rollback,omitempty" xml:"rollback,omitempty"`
	Preconditions []ChangePrecondition `json:"preconditions,omitempty" yaml:"preconditions,omitempty" xml:"preconditions,omitempty"`
	RunAfter      []string             `json:"runAfter,omitempty" yaml:"runAfter,omitempty" xml:"runAfter,omitempty"`
}

// Changelog represents a GoMigrate changelog document containing an ordered set of changesets.
type Changelog struct {
	XMLName           xml.Name    `json:"-" yaml:"-" xml:"databaseChangeLog"`
	Xmlns             string      `json:"xmlns,omitempty" yaml:"xmlns,omitempty" xml:"xmlns,attr,omitempty"`
	XmlnsXsi          string      `json:"xmlnsXsi,omitempty" yaml:"xmlnsXsi,omitempty" xml:"xmlns:xsi,attr,omitempty"`
	XsiLocation       string      `json:"xsiLocation,omitempty" yaml:"xsiLocation,omitempty" xml:"xsi:schemaLocation,attr,omitempty"`
	DatabaseChangeLog []ChangeSet `json:"databaseChangeLog" yaml:"databaseChangeLog" xml:"databaseChangeLog"`
	ChangeSets        []ChangeSet `json:"changeSets,omitempty" yaml:"changeSets,omitempty" xml:"changeSets,omitempty"`
}

// ChangePrecondition represents a condition that must hold before a changeset is applied.
type ChangePrecondition struct {
	OnFail    string                 `json:"onFail,omitempty" yaml:"onFail,omitempty" xml:"onFail,attr,omitempty"`
	OnError   string                 `json:"onError,omitempty" yaml:"onError,omitempty" xml:"onError,attr,omitempty"`
	Condition map[string]interface{} `json:"condition" yaml:"condition" xml:",any"`
}

// ChangeRollback holds the SQL used to undo a changeset.
type ChangeRollback struct {
	SQL string `json:"sql,omitempty" yaml:"sql,omitempty" xml:",chardata"`
}

// RollbackResult represents the result of a rollback operation.
type RollbackResult struct {
	Success      bool          `json:"success"`
	MigrationID  string        `json:"migration_id"`
	ErrorMessage string        `json:"error_message,omitempty"`
	Duration     time.Duration `json:"duration"`
	RolledBackAt time.Time     `json:"rolled_back_at"`
	Errors       []string      `json:"errors,omitempty"`
	Warnings     []string      `json:"warnings,omitempty"`
}

// RollbackPlan represents a rollback execution plan
type RollbackPlan struct {
	Migrations    []*Migration  `json:"migrations"`
	TotalCount    int           `json:"total_count"`
	EstimatedTime time.Duration `json:"estimated_time"`
	Warnings      []string      `json:"warnings"`
	Errors        []string      `json:"errors"`
	CanProceed    bool          `json:"can_proceed"`
}

// RollbackConfig contains configuration for rollback operations
type RollbackConfig struct {
	MaxRollbackCount       int
	ValidateBeforeRollback bool
	CreateBackup           bool
	BackupDirectory        string
	UseTransactions        bool
	Timeout                time.Duration
	StopOnError            bool
	DryRun                 bool
}

// RollbackManager interface for managing rollbacks (forward declaration for builder)
type RollbackManager interface {
	CanRollback(ctx context.Context, migrationID string) (bool, []string, error)
	RollbackToVersion(ctx context.Context, version string) (*RollbackResult, error)
	RollbackCount(ctx context.Context, count int) (*RollbackResult, error)
	RollbackMigration(ctx context.Context, migrationID string) (*RollbackResult, error)
	GetRollbackPlan(ctx context.Context, migrationIDs []string) (*RollbackPlan, error)
	GetAdvancedManager() AdvancedRollbackManager
}

// AdvancedRollbackManager interface for advanced rollback features
type AdvancedRollbackManager interface {
	// Selective rollback - rollback specific migration in middle of chain
	RollbackSelective(ctx context.Context, migrationID string, options *SelectiveRollbackOptions) (*SelectiveRollbackResult, error)

	// Impact analysis - analyze rollback impact before execution
	AnalyzeRollbackImpact(ctx context.Context, migrationIDs []string) (*RollbackImpactAnalysis, error)

	// Test rollback - test rollback in staging environment
	TestRollback(ctx context.Context, migrationIDs []string, environment string) (*RollbackTestResult, error)

	// Get rollback graph - visualize migration dependencies
	GetRollbackDependencyGraph(ctx context.Context, migrationID string) (*DependencyGraph, error)
}

// SelectiveRollbackOptions contains options for selective rollback
type SelectiveRollbackOptions struct {
	// Force rollback even if dependencies exist
	Force bool
	// Automatically rollback dependent migrations
	RollbackDependents bool
	// Create savepoint before rollback
	CreateSavepoint bool
	// Dry run mode
	DryRun bool
	// Skip impact analysis
	SkipImpactAnalysis bool
	// Maximum dependency depth
	MaxDepth int
	// Operation timeout
	Timeout time.Duration
	// Create backup before rollback
	CreateBackup bool
	// Custom validator function
	CustomValidator func(context.Context, *Migration) error
}

// SelectiveRollbackResult contains the result of selective rollback
type SelectiveRollbackResult struct {
	Success              bool                    `json:"success"`
	MigrationID          string                  `json:"migration_id"`
	RolledBackMigrations []*Migration            `json:"rolled_back_migrations"`
	ImpactAnalysis       *RollbackImpactAnalysis `json:"impact_analysis"`
	Errors               []string                `json:"errors"`
	Warnings             []string                `json:"warnings"`
	Duration             time.Duration           `json:"duration"`
	Savepoints           []string                `json:"savepoints,omitempty"`
	BackupLocation       string                  `json:"backup_location,omitempty"`
	ExecutedAt           time.Time               `json:"executed_at"`
}

// RollbackImpactAnalysis contains analysis of rollback impact
type RollbackImpactAnalysis struct {
	MigrationID         string              `json:"migration_id"`
	ImpactLevel         string              `json:"impact_level"`
	AffectedMigrations  []*MigrationImpact  `json:"affected_migrations"`
	AffectedTables      []string            `json:"affected_tables"`
	AffectedColumns     []string            `json:"affected_columns"`
	AffectedIndexes     []string            `json:"affected_indexes"`
	AffectedConstraints []string            `json:"affected_constraints"`
	AffectedObjects     []string            `json:"affected_objects"`
	DataLossRisk        bool                `json:"data_loss_risk"`
	EstimatedDataLoss   *DataLossEstimate   `json:"estimated_data_loss,omitempty"`
	EstimatedDuration   time.Duration       `json:"estimated_duration"`
	Conflicts           []*RollbackConflict `json:"conflicts"`
	Recommendations     []string            `json:"recommendations"`
	Warnings            []string            `json:"warnings"`
	CanProceedSafely    bool                `json:"can_proceed_safely"`
	RequiresManual      bool                `json:"requires_manual"`
	AnalyzedAt          time.Time           `json:"analyzed_at"`
}

// MigrationImpact describes the impact of a migration rollback
type MigrationImpact struct {
	Migration       *Migration `json:"migration"`
	ImpactType      string     `json:"impact_type"` // direct, dependent, cascading
	ImpactLevel     string     `json:"impact_level"`
	MustRollback    bool       `json:"must_rollback"`
	CanRollback     bool       `json:"can_rollback"`
	Reason          string     `json:"reason"`
	Dependencies    []string   `json:"dependencies"`
	AffectedObjects []string   `json:"affected_objects"`
}

// DataLossEstimate estimates potential data loss
type DataLossEstimate struct {
	TablesAffected  []string         `json:"tables_affected"`
	EstimatedRows   map[string]int64 `json:"estimated_rows"`
	TotalEstimate   int64            `json:"total_estimate"`
	RecoverableFrom string           `json:"recoverable_from,omitempty"`
	BackupRequired  bool             `json:"backup_required"`
}

// RollbackConflict describes a conflict during rollback
type RollbackConflict struct {
	Type            string   `json:"type"`     // dependency, data, schema, sequence
	Severity        string   `json:"severity"` // low, medium, high, critical
	Description     string   `json:"description"`
	AffectedObjects []string `json:"affected_objects"`
	Resolution      string   `json:"resolution"`
	CanAutoResolve  bool     `json:"can_auto_resolve"`
	RequiresManual  bool     `json:"requires_manual"`
}

// RollbackTestResult contains the result of rollback testing
type RollbackTestResult struct {
	Success        bool                `json:"success"`
	Environment    string              `json:"environment"`
	TestedAt       time.Time           `json:"tested_at"`
	Duration       time.Duration       `json:"duration"`
	TestCases      []*RollbackTestCase `json:"test_cases"`
	TotalTests     int                 `json:"total_tests"`
	PassedTests    int                 `json:"passed_tests"`
	FailedTests    int                 `json:"failed_tests"`
	SkippedTests   int                 `json:"skipped_tests"`
	Coverage       float64             `json:"coverage"`
	Errors         []string            `json:"errors"`
	Warnings       []string            `json:"warnings"`
	Recommendation string              `json:"recommendation"`
}

// RollbackTestCase represents a single test case
type RollbackTestCase struct {
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	MigrationID     string        `json:"migration_id"`
	Status          string        `json:"status"` // passed, failed, skipped
	Duration        time.Duration `json:"duration"`
	Error           string        `json:"error,omitempty"`
	Assertions      []string      `json:"assertions"`
	VerifiedObjects []string      `json:"verified_objects"`
}

// DependencyGraph represents migration dependencies
type DependencyGraph struct {
	RootMigration *Migration                 `json:"root_migration"`
	Nodes         map[string]*DependencyNode `json:"nodes"`
	Edges         []*DependencyEdge          `json:"edges"`
	Levels        [][]string                 `json:"levels"` // Migrations grouped by dependency level
	MaxDepth      int                        `json:"max_depth"`
	TotalNodes    int                        `json:"total_nodes"`
	HasCycles     bool                       `json:"has_cycles"`
	CyclicPaths   [][]string                 `json:"cyclic_paths,omitempty"`
}

// DependencyNode represents a node in the dependency graph
type DependencyNode struct {
	Migration      *Migration `json:"migration"`
	Level          int        `json:"level"`        // Distance from root
	Dependencies   []string   `json:"dependencies"` // Migration IDs this depends on
	Dependents     []string   `json:"dependents"`   // Migration IDs that depend on this
	IsRollbackable bool       `json:"is_rollbackable"`
}

// DependencyEdge represents an edge in the dependency graph
type DependencyEdge struct {
	From       string `json:"from"`        // Migration ID
	To         string `json:"to"`          // Migration ID
	EdgeType   string `json:"edge_type"`   // explicit, implicit, data
	IsBlocking bool   `json:"is_blocking"` // Whether this dependency blocks rollback
}

// ChangelogEntry represents an entry in the changelog
type ChangelogEntry struct {
	ID           string    `json:"id"`
	Version      string    `json:"version"`
	Description  string    `json:"description"`
	Author       string    `json:"author"`
	ExecutedAt   time.Time `json:"executed_at"`
	Checksum     string    `json:"checksum"`
	Success      bool      `json:"success"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// Change represents a single change in a changeset
type Change struct {
	Type       string                 `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
	Content    string                 `json:"content,omitempty"`
	SQL        string                 `json:"sql,omitempty" yaml:"sql,omitempty" xml:",chardata"`
}

// FileMigrationLoader represents a file-based migration loader
type FileMigrationLoader struct {
	BasePath string
	Pattern  string
}

// StandaloneMigrationToolImpl represents the standalone migration tool implementation
type StandaloneMigrationToolImpl struct {
	Config *StandaloneConfig
	engine MigrationTool
}

// NewStandaloneMigrationToolImpl creates a new instance of StandaloneMigrationToolImpl
func NewStandaloneMigrationToolImpl() *StandaloneMigrationToolImpl {
	return &StandaloneMigrationToolImpl{
		Config: &StandaloneConfig{},
	}
}

// Status returns the current migration status
func (s *StandaloneMigrationToolImpl) Status(ctx context.Context) (*MigrationStatus, error) {
	if s.engine == nil {
		return nil, NewConfigurationError("engine", "engine", "nil", "migration engine not initialized", nil)
	}
	return s.engine.Status(ctx)
}

// Migrate runs pending migrations
func (s *StandaloneMigrationToolImpl) Migrate(ctx context.Context, target string, dryRun bool) error {
	if s.engine == nil {
		return NewConfigurationError("engine", "engine", "nil", "migration engine not initialized", nil)
	}
	return s.engine.Migrate(ctx, target, dryRun)
}

// Rollback rolls back migrations
func (s *StandaloneMigrationToolImpl) Rollback(ctx context.Context, steps int) error {
	if s.engine == nil {
		return NewConfigurationError("engine", "engine", "nil", "migration engine not initialized", nil)
	}
	return s.engine.Rollback(ctx, steps)
}

// Validate validates all migrations
func (s *StandaloneMigrationToolImpl) Validate(ctx context.Context) error {
	if s.engine == nil {
		return NewConfigurationError("engine", "engine", "nil", "migration engine not initialized", nil)
	}
	return s.engine.Validate(ctx)
}

// Info returns migration information
func (s *StandaloneMigrationToolImpl) Info(ctx context.Context) (*MigrationInfo, error) {
	if s.engine == nil {
		return nil, NewConfigurationError("engine", "engine", "nil", "migration engine not initialized", nil)
	}
	return s.engine.Info(ctx)
}

// Close closes the migration tool and releases resources
func (s *StandaloneMigrationToolImpl) Close() error {
	if s.engine == nil {
		return nil
	}
	return s.engine.Close()
}

// LoadConfig loads configuration from file
func (s *StandaloneMigrationToolImpl) LoadConfig(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return NewConfigurationError("config", "file", configPath, "failed to read config file", err)
	}

	if s.Config == nil {
		s.Config = &StandaloneConfig{}
	}

	ext := filepath.Ext(configPath)
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, s.Config); err != nil {
			return NewConfigurationError("config", "parse", "json", "failed to unmarshal JSON", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, s.Config); err != nil {
			return NewConfigurationError("config", "parse", "yaml", "failed to unmarshal YAML", err)
		}
	default:
		return NewConfigurationError("config", "format", ext, "unsupported config format", nil)
	}

	// TODO: Initialize engine based on loaded config if needed
	// For now, assume engine is set externally

	return nil
}

// SaveConfig saves configuration to file
func (s *StandaloneMigrationToolImpl) SaveConfig(configPath string) error {
	if s.Config == nil {
		return NewConfigurationError("config", "config", "nil", "no configuration to save", nil)
	}

	ext := filepath.Ext(configPath)
	var data []byte
	var err error
	switch ext {
	case ".json":
		data, err = json.MarshalIndent(s.Config, "", "  ")
		if err != nil {
			return NewConfigurationError("config", "marshal", "json", "failed to marshal JSON", err)
		}
	case ".yaml", ".yml":
		data, err = yaml.Marshal(s.Config)
		if err != nil {
			return NewConfigurationError("config", "marshal", "yaml", "failed to marshal YAML", err)
		}
	default:
		return NewConfigurationError("config", "format", ext, "unsupported config format", nil)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return NewConfigurationError("config", "file", configPath, "failed to write config file", err)
	}

	return nil
}

// GetConfig returns the current configuration
func (s *StandaloneMigrationToolImpl) GetConfig() *StandaloneConfig {
	if s.Config == nil {
		s.Config = &StandaloneConfig{}
	}
	return s.Config
}

// SetEnvironment sets the environment configuration
func (s *StandaloneMigrationToolImpl) SetEnvironment(env *EnvironmentConfig) {
	if env != nil {
		s.Config.Environment = *env
	}
}

// GetEnvironment returns the current environment configuration
func (s *StandaloneMigrationToolImpl) GetEnvironment() *EnvironmentConfig {
	return &s.Config.Environment
}

// SetPlugins sets the plugin configurations
func (s *StandaloneMigrationToolImpl) SetPlugins(plugins []PluginConfig) {
	s.Config.Plugins = plugins
}

// GetPlugins returns the current plugin configurations
func (s *StandaloneMigrationToolImpl) GetPlugins() []PluginConfig {
	return s.Config.Plugins
}

// SetEngine sets the internal migration engine (added for flexibility)
func (s *StandaloneMigrationToolImpl) SetEngine(engine MigrationTool) {
	s.engine = engine
}

// MigrationStatus represents the current state of migrations
type MigrationStatus struct {
	TotalMigrations   int               `json:"total_migrations"`
	ExecutedCount     int               `json:"executed_count"`
	PendingCount      int               `json:"pending_count"`
	FailedCount       int               `json:"failed_count"`
	DatabaseVersion   string            `json:"database_version"`
	SchemaVersion     string            `json:"schema_version"`
	IsLocked          bool              `json:"is_locked"`
	LastMigration     *MigrationRecord  `json:"last_migration,omitempty"`
	AppliedMigrations []MigrationRecord `json:"applied_migrations"`
	PendingMigrations []MigrationRecord `json:"pending_migrations"`
	FailedMigrations  []MigrationRecord `json:"failed_migrations"`
}

// MigrationInfo provides detailed information about the migration system
type MigrationInfo struct {
	Version        string    `json:"version"`
	DatabaseType   string    `json:"database_type"`
	MigrationsPath string    `json:"migrations_path"`
	TableName      string    `json:"table_name"`
	LockTableName  string    `json:"lock_table_name"`
	Environment    string    `json:"environment"`
	Features       []string  `json:"features"`
	LastCheck      time.Time `json:"last_check"`
}

// MigrationRecord represents a single migration record
type MigrationRecord struct {
	ID            string    `json:"id"`
	Version       string    `json:"version"`
	Description   string    `json:"description"`
	Type          string    `json:"type"`
	Script        string    `json:"script"`
	Checksum      string    `json:"checksum"`
	Author        string    `json:"author,omitempty"`
	ExecutedAt    time.Time `json:"executed_at,omitempty"`
	ExecutionTime int64     `json:"execution_time,omitempty"`
	Success       bool      `json:"success"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	// Rollback fields
	CanRollback bool   `json:"can_rollback"`
	RollbackSQL string `json:"rollback_sql"`
}

// Common migration errors
var (
	ErrMigrationNotFound    = errors.New("migration not found")
	ErrMigrationExists      = errors.New("migration already exists")
	ErrInvalidMigration     = errors.New("invalid migration")
	ErrMigrationLocked      = errors.New("migration is locked")
	ErrDatabaseNotSupported = errors.New("database not supported")
	ErrInvalidConfiguration = errors.New("invalid configuration")
	ErrMigrationFailed      = errors.New("migration failed")
	ErrRollbackFailed       = errors.New("rollback failed")
	ErrValidationFailed     = errors.New("validation failed")
	ErrChecksumMismatch     = errors.New("checksum mismatch")
	ErrCircularDependency   = errors.New("circular dependency detected")
	ErrPreconditionFailed   = errors.New("precondition failed")
	ErrBackupFailed         = errors.New("backup failed")
	ErrConnectionFailed     = errors.New("database connection failed")
	ErrTimeout              = errors.New("operation timeout")
)

// MigrationError represents a migration-specific error
type MigrationError struct {
	Type        string `json:"type"`
	Message     string `json:"message"`
	MigrationID string `json:"migration_id,omitempty"`
	Version     string `json:"version,omitempty"`
	Cause       error  `json:"cause,omitempty"`
}

func (e *MigrationError) Error() string {
	if e.MigrationID != "" {
		return fmt.Sprintf("migration %s: %s", e.MigrationID, e.Message)
	}
	return e.Message
}

func (e *MigrationError) Unwrap() error {
	return e.Cause
}

// NewMigrationError creates a new migration error
func NewMigrationError(errorType, message string, migrationID string, cause error) *MigrationError {
	return &MigrationError{
		Type:        errorType,
		Message:     message,
		MigrationID: migrationID,
		Cause:       cause,
	}
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// NewValidationError creates a new validation error
func NewValidationError(field, value, message string, cause error) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
		Cause:   cause,
	}
}

// ConfigurationError represents a configuration error
type ConfigurationError struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *ConfigurationError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("configuration error in section '%s', key '%s': %s", e.Section, e.Key, e.Message)
	}
	return fmt.Sprintf("configuration error in section '%s': %s", e.Section, e.Message)
}

func (e *ConfigurationError) Unwrap() error {
	return e.Cause
}

// NewConfigurationError creates a new configuration error
func NewConfigurationError(section, key, value, message string, cause error) *ConfigurationError {
	return &ConfigurationError{
		Section: section,
		Key:     key,
		Value:   value,
		Message: message,
		Cause:   cause,
	}
}

// DatabaseError represents a database-specific error
type DatabaseError struct {
	Driver    string `json:"driver"`
	Operation string `json:"operation"`
	Query     string `json:"query,omitempty"`
	Message   string `json:"message"`
	Cause     error  `json:"cause,omitempty"`
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("database error (%s) during %s: %s", e.Driver, e.Operation, e.Message)
}

func (e *DatabaseError) Unwrap() error {
	return e.Cause
}

// NewDatabaseError creates a new database error
func NewDatabaseError(driver, operation, query, message string, cause error) *DatabaseError {
	return &DatabaseError{
		Driver:    driver,
		Operation: operation,
		Query:     query,
		Message:   message,
		Cause:     cause,
	}
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific retryable errors
	switch {
	case errors.Is(err, ErrConnectionFailed):
		return true
	case errors.Is(err, ErrTimeout):
		return true
	case errors.Is(err, ErrMigrationLocked):
		return true
	default:
		return false
	}
}

// DefaultMigrationConfig returns a default migration configuration
func DefaultMigrationConfig() *MigrationConfig {
	return &MigrationConfig{
		TableName:               "schema_migrations",
		LockTableName:           "schema_migrations_lock",
		ChangelogTableName:      "migration_changelog",
		Schema:                  "",
		MigrationsPath:          "migrations",
		Environment:             "development",
		AutoMigrate:             true,
		AutoCreateTables:        true,
		ValidateOnMigrate:       true,
		ValidateOnLoad:          false,
		StrictMode:              false, // Lenient by default - performance warnings don't fail migrations
		OutOfOrder:              false,
		IgnoreMissingMigrations: false,
		IgnoreIgnoredMigrations: false,
		IgnorePendingDrops:      false,
		CleanOnValidationError:  false,
		StopOnError:             true,
		ValidateChecksums:       true,
		UseTransactions:         true,
		LockTimeout:             time.Minute * 5,
		CommandTimeout:          time.Minute * 30,
		StatementTimeout:        time.Minute * 10,
		EnableRollback:          true,
		RollbackOnFailure:       false,
		MaxRollbackDepth:        10,
		EnableChangelog:         true,
		ChangelogFormat:         "yaml",
		EnableContexts:          false,
		EnableLabels:            false,
		EnableDryRun:            false,
		EnableBackup:            false,
		BackupPath:              "backups",
		EnableCompression:       false,
		Verbose:                 false,
		ExecutionMode:           "sequential",
		BatchSize:               10,
		Concurrency:             4,
	}
}

// StandaloneLogger interface for logging
type StandaloneLogger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Fatal(msg string, fields ...interface{})
}

// ConfigManager manages configuration loading and validation
type ConfigManager interface {
	LoadConfig(path string) (*StandaloneConfig, error)
	SaveConfig(config *StandaloneConfig, path string) error
	ValidateConfig(config *StandaloneConfig) error
	MergeConfigs(base, override *StandaloneConfig) *StandaloneConfig
}

// PluginManager manages migration plugins
type PluginManager interface {
	LoadPlugins(path string) error
	RegisterPlugin(name string, plugin MigrationPlugin) error
	GetPlugin(name string) (MigrationPlugin, error)
	ListPlugins() []string
}

// MigrationPlugin interface for migration plugins. The methods form a cohesive
// plugin lifecycle contract (identity/init/hooks/teardown); a plugin implements
// the whole set as one unit.
//
//nolint:interfacebloat // Cohesive plugin lifecycle contract (see doc comment above).
type MigrationPlugin interface {
	Name() string
	Version() string
	Description() string
	Initialize(config map[string]interface{}) error
	BeforeMigration(ctx context.Context, migration *Migration) error
	AfterMigration(ctx context.Context, migration *Migration, result *MigrationResult) error
	OnError(ctx context.Context, migration *Migration, err error) error
}

// ChangelogEntryRecord represents a changelog entry record
type ChangelogEntryRecord struct {
	ID            string    `json:"id" gorm:"primaryKey"`
	Author        string    `json:"author"`
	Filename      string    `json:"filename"`
	DateExecuted  time.Time `json:"date_executed"`
	OrderExecuted int       `json:"order_executed"`
	ExecType      string    `json:"exec_type"` // EXECUTED, FAILED, SKIPPED, RERAN, MARK_RAN
	MD5Sum        string    `json:"md5sum"`
	Description   string    `json:"description"`
	Comments      string    `json:"comments"`
	Tag           string    `json:"tag"`
	GoMigrate     string    `json:"go_migrate"`
	Contexts      string    `json:"contexts"`
	Labels        string    `json:"labels"`
	DeploymentID  string    `json:"deployment_id"`
}

// IsFatalError checks if an error is fatal and should stop migration
func IsFatalError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific fatal errors
	switch {
	case errors.Is(err, ErrInvalidMigration):
		return true
	case errors.Is(err, ErrChecksumMismatch):
		return true
	case errors.Is(err, ErrCircularDependency):
		return true
	case errors.Is(err, ErrDatabaseNotSupported):
		return true
	case errors.Is(err, ErrInvalidConfiguration):
		return true
	default:
		return false
	}
}

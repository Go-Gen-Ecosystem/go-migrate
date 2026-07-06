/**
 * @file constants.go
 * @description Constants for migration module to ensure consistency and prevent model/interface mismatches
 * @author Admilson B. F. Cossa
 * @date 2024-01-11
 * @path pkg/core/database/migrations/constants.go
 */

package constants

import (
	"time"
)

// ValidationSeverity represents the severity level of a validation issue
type ValidationSeverity string

// Migration States
const (
	MigrationStatePending    = "pending"
	MigrationStateRunning    = "running"
	MigrationStateApplied    = "applied"
	MigrationStateSuccess    = "success"
	MigrationStateFailed     = "failed"
	MigrationStateRolledBack = "rolled_back"
)

// Migration Types
const (
	MigrationTypeSQL      = "sql"
	MigrationTypeGo       = "go"
	MigrationTypeXML      = "xml"
	MigrationTypeYAML     = "yaml"
	MigrationTypeJSON     = "json"
	MigrationTypeRollback = "rollback"
)

// Operation Types
const (
	OperationExecute  = "execute"
	OperationRollback = "rollback"
	OperationValidate = "validate"
	OperationInit     = "init"
)

// Execution Types
const (
	ExecTypeExecuted = "EXECUTED"
	ExecTypeFailed   = "FAILED"
	ExecTypeSkipped  = "SKIPPED"
	ExecTypeReran    = "RERAN"
	ExecTypeMarkRan  = "MARK_RAN"
)

// Status Types
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusPending = "pending"
	StatusRunning = "running"
)

// Validation Severity Levels
const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
	SeverityInfo    ValidationSeverity = "info"
)

// Validation Error Codes
const (
	ValidationErrorCode   = "VALIDATION_ERROR"
	SelectStarCode        = "SELECT_STAR"
	MissingSemicolonCode  = "MISSING_SEMICOLON"
	HardcodedPasswordCode = "HARDCODED_PASSWORD"
	WeakPermissionsCode   = "WEAK_PERMISSIONS"
)

// Migration Error Codes
const (
	ErrorCodeValidation   = "MIGRATION_VALIDATION_ERROR"
	ErrorCodeExecution    = "MIGRATION_EXECUTION_ERROR"
	ErrorCodeRollback     = "MIGRATION_ROLLBACK_ERROR"
	ErrorCodeLock         = "MIGRATION_LOCK_ERROR"
	ErrorCodeDependency   = "MIGRATION_DEPENDENCY_ERROR"
	ErrCodeInvalidRequest = "INVALID_REQUEST_ERROR"
)

// Validation Rule Names
const (
	RuleBasicValidation               = "basic_validation"
	RuleSyntaxBestPractices           = "syntax_best_practices"
	RuleSyntaxFormatting              = "syntax_formatting"
	RuleSecurityCredentials           = "security_credentials"
	RuleSecurityPermissions           = "security_permissions"
	RuleRequireDescription            = "require_description"
	RuleRequireAuthor                 = "require_author"
	RuleRequireRollbackForDestructive = "require_rollback_for_destructive"
)

// RollbackStrategy defines different rollback strategies
type RollbackStrategy string

// Rollback Strategies
const (
	RollbackStrategySequential  RollbackStrategy = "sequential"
	RollbackStrategyParallel    RollbackStrategy = "parallel"
	RollbackStrategyBatch       RollbackStrategy = "batch"
	RollbackStrategyConditional RollbackStrategy = "conditional"
	RollbackStrategySelective   RollbackStrategy = "selective"
)

// RollbackImpactLevel defines the impact level of a rollback operation
type RollbackImpactLevel string

// Rollback Impact Levels
const (
	ImpactLevelMinimal  RollbackImpactLevel = "minimal"
	ImpactLevelLow      RollbackImpactLevel = "low"
	ImpactLevelMedium   RollbackImpactLevel = "medium"
	ImpactLevelHigh     RollbackImpactLevel = "high"
	ImpactLevelCritical RollbackImpactLevel = "critical"
)

// Rollback Test Status
const (
	TestStatusPending    = "pending"
	TestStatusRunning    = "running"
	TestStatusPassed     = "passed"
	TestStatusFailed     = "failed"
	TestStatusSkipped    = "skipped"
	TestStatusInProgress = "in_progress"
)

// Rollback Operation Types
const (
	RollbackOpTypeComplete  = "complete"
	RollbackOpTypePartial   = "partial"
	RollbackOpTypeSelective = "selective"
	RollbackOpTypeSimulated = "simulated"
)

// Rollback Conflict Types
const (
	ConflictTypeDependency = "dependency"
	ConflictTypeData       = "data"
	ConflictTypeSchema     = "schema"
	ConflictTypeSequence   = "sequence"
)

// Note: RollbackPlan and RollbackResult structs have been moved to the types package
// to avoid circular dependencies. They can be accessed as types.RollbackPlan and types.RollbackResult.

// Default Table Names
const (
	DefaultMigrationsTable = "migration_history"
	DefaultChangelogTable  = "migration_changelog"
	DefaultLockTable       = "migration_locks"
	DefaultSchemaTable     = "schema_migrations"
)

// Default Paths
const (
	DefaultMigrationsPath = "./migrations"
	DefaultBackupPath     = "./backups"
)

// Default Timeouts
const (
	DefaultLockTimeout    = 30 * time.Minute
	DefaultCommandTimeout = 5 * time.Minute
)

// Default Limits
const (
	DefaultMaxRollbackDepth = 10
	DefaultHistoryLimit     = 100
)

// Environment Types
const (
	EnvironmentProduction  = "production"
	EnvironmentStaging     = "staging"
	EnvironmentDevelopment = "development"
	EnvironmentTest        = "test"
)

// Changelog Formats
const (
	ChangelogFormatXML  = "xml"
	ChangelogFormatYAML = "yaml"
	ChangelogFormatJSON = "json"
	ChangelogFormatSQL  = "sql"
)

// Lock Operations
const (
	LockOperationMigrationInit      = "migration_init"
	LockOperationMigrationExecution = "migration_execution"
	LockOperationMigrationRollback  = "migration_rollback"
)

// SQL Keywords and Patterns
var (
	// Dangerous SQL Keywords
	DangerousKeywords = []string{
		"DROP DATABASE", "DROP SCHEMA", "TRUNCATE", "DELETE FROM",
		"ALTER USER", "CREATE USER", "DROP USER", "GRANT ALL",
	}

	// SQL Injection Patterns
	SQLInjectionPatterns = []string{
		`(?i)(union\s+select)`, `(?i)(or\s+1\s*=\s*1)`, `(?i)(and\s+1\s*=\s*1)`,
		`(?i)(exec\s*\()`, `(?i)(execute\s*\()`, `(?i)(sp_executesql)`,
	}

	// Privilege Escalation Patterns
	PrivilegeEscalationPatterns = []string{
		`(?i)(grant\s+all)`, `(?i)(grant\s+.*\s+to\s+public)`,
		`(?i)(alter\s+user.*admin)`, `(?i)(create\s+user.*admin)`,
	}

	// Destructive Operations
	DestructiveOperations = []string{
		"DROP", "DELETE", "TRUNCATE", "ALTER",
	}

	// SQL Statement Keywords
	SQLStatementKeywords = []string{
		"CREATE", "DROP", "ALTER", "INSERT", "UPDATE", "DELETE",
		"GRANT", "REVOKE", "SET", "USE",
	}

	// SQL Query Keywords
	SQLQueryKeywords = []string{
		"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP",
		"GRANT", "REVOKE", "TRUNCATE", "COMMIT", "ROLLBACK",
	}
)

// Error Messages (as string constants for messages)
const (
	MsgMigrationNotFound            = "migration not found"
	MsgMigrationValidationFailed    = "migration validation failed"
	MsgMigrationExecutionFailed     = "migration execution failed"
	MsgMigrationFailed              = "migration failed"
	MsgRollbackFailed               = "rollback failed"
	MsgLockAcquisitionFailed        = "failed to acquire lock"
	MsgDependencyNotApplied         = "dependency is not applied"
	MsgCircularDependency           = "circular dependency detected"
	MsgInvalidMigrationState        = "invalid migration state"
	MsgInvalidRequest               = "invalid request"
	MsgMigrationAlreadyApplied      = "migration already applied"
	MsgNoRollbackSQL                = "no rollback SQL available"
	MsgDestructiveWithoutRollback   = "destructive operation detected but no rollback SQL provided"
	MsgMigrationMustHaveDescription = "migration must have a description"
	MsgMigrationShouldHaveAuthor    = "migration should have an author"
)

// Success Messages
const (
	MsgMigrationExecutedSuccessfully = "migration executed successfully"
	MsgRollbackCompletedSuccessfully = "rollback completed successfully"
	MsgValidationPassed              = "validation passed"
	MsgLockAcquiredSuccessfully      = "lock acquired successfully"
)

// Warning Messages
const (
	WarnMigrationNotApplied      = "migration is not applied"
	WarnMigrationNotRollbackable = "migration is marked as non-rollbackable"
	WarnNoRollbackSQL            = "no rollback SQL available"
	WarnHasDependentMigrations   = "has dependent migrations"
	WarnPotentiallySlowOperation = "potentially slow operation detected"
	WarnSelectStarUsage          = "avoid using SELECT * in migrations"
	WarnMissingSemicolon         = "statement may be missing semicolon"
	WarnHardcodedPassword        = "hardcoded password detected in migration"
	WarnWeakPermissions          = "granting ALL PRIVILEGES may be too permissive"
)

// Suggestions
const (
	SuggestSpecifyColumnNames  = "specify column names explicitly"
	SuggestAddSemicolon        = "add semicolon at the end of the statement"
	SuggestUseEnvironmentVars  = "use environment variables or secure configuration"
	SuggestGrantNecessaryPrivs = "grant only necessary privileges"
)

// JSON Field Names
const (
	JSONFieldMigrations  = "migrations"
	JSONFieldChangelog   = "changelog"
	JSONFieldMetadata    = "metadata"
	JSONFieldUpdatedAt   = "updated_at"
	JSONFieldVersion     = "version"
	JSONFieldDescription = "description"
	JSONFieldType        = "type"
	JSONFieldDryRun      = "dry_run"
)

// Database Column Names
const (
	ColumnID            = "id"
	ColumnVersion       = "version"
	ColumnDescription   = "description"
	ColumnFilename      = "filename"
	ColumnFilepath      = "filepath"
	ColumnContent       = "content"
	ColumnChecksum      = "checksum"
	ColumnType          = "type"
	ColumnAuthor        = "author"
	ColumnChangesetID   = "changeset_id"
	ColumnState         = "state"
	ColumnContexts      = "contexts"
	ColumnLabels        = "labels"
	ColumnDependsOn     = "depends_on"
	ColumnPreconditions = "preconditions"
	ColumnRollbackSQL   = "rollback_sql"
	ColumnCanRollback   = "can_rollback"
	ColumnRunAlways     = "run_always"
	ColumnRunOnChange   = "run_on_change"
	ColumnFailOnError   = "fail_on_error"
	ColumnExecutedAt    = "executed_at"
	ColumnRolledBackAt  = "rolled_back_at"
	ColumnExecutionTime = "execution_time"
	ColumnErrorMessage  = "error_message"
	ColumnMetadata      = "metadata"
	ColumnCreatedAt     = "created_at"
	ColumnUpdatedAt     = "updated_at"
	ColumnMigrationID   = "migration_id"
	ColumnOperation     = "operation"
	ColumnStatus        = "status"
	ColumnExecutedBy    = "executed_by"
	ColumnContext       = "context"
	ColumnLockedBy      = "locked_by"
	ColumnLockedAt      = "locked_at"
	ColumnExpiresAt     = "expires_at"
)

// Regular Expression Patterns
const (
	// PasswordPattern matches hardcoded passwords in both the assignment form
	// (`password = 'secret'`) and the SQL DDL form (`CREATE USER ... WITH PASSWORD 'secret'`),
	// where the keyword and the quoted value are separated by whitespace rather than `=`.
	PasswordPattern      = `(?i)(password|pwd)\s*(?:=\s*)?['"][^'"]+['"]`
	AllPrivilegesPattern = `(?i)grant\s+all\s+privileges`
)

// File Extensions
const (
	ExtensionSQL  = ".sql"
	ExtensionGo   = ".go"
	ExtensionXML  = ".xml"
	ExtensionYAML = ".yaml"
	ExtensionYML  = ".yml"
	ExtensionJSON = ".json"
)

// Comment Prefixes
const (
	CommentPrefixSQL       = "--"
	CommentPrefixMultiLine = "/*"
)

// Migration ID Suffixes
const (
	SuffixRollback = "_rollback"
)

// Execution Modes
const (
	ExecutionModeSequential = "sequential"
	ExecutionModeBatch      = "batch"
	ExecutionModeParallel   = "parallel"
)

// Database Drivers
const (
	DriverSQLite     = "sqlite"
	DriverPostgreSQL = "postgres"
	DriverMySQL      = "mysql"
	DriverSQLServer  = "sqlserver"
	DriverOracle     = "oracle"
	DriverDB2        = "db2"
	DriverMongoDB    = "mongodb"
	DriverCassandra  = "cassandra"
	DriverRedis      = "redis"
)

// Default Configuration Values
const (
	DefaultSchemaName        = "public"
	DefaultBatchSize         = 10
	DefaultConcurrency       = 4
	DefaultMaxConnections    = 100
	DefaultConnectionTimeout = 30 * time.Second
	DefaultIdleTimeout       = 5 * time.Minute
	DefaultMaxLifetime       = 1 * time.Hour
	DefaultStatementTimeout  = 10 * time.Minute
)

// Lock-related Constants
const (
	DefaultMigrationLockTableName = "migration_locks"
	LockExpirationTime            = 1 * time.Hour
	LockPollInterval              = 50 * time.Millisecond
)

// Performance Thresholds
const (
	MaxMigrationSizeBytes  = 10 * 1024 * 1024 // 10MB
	SlowQueryThreshold     = 1 * time.Second
	LongMigrationThreshold = 10 * time.Minute
	MaxIndexNameLength     = 63 // PostgreSQL limit
	MaxTableNameLength     = 64 // MySQL limit
)

// Retry Configuration
const (
	DefaultMaxRetries    = 3
	DefaultRetryDelay    = 1 * time.Second
	DefaultRetryBackoff  = 2.0 // exponential backoff multiplier
	DefaultRetryMaxDelay = 30 * time.Second
)

// Logging Levels
const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
	LogLevelFatal = "fatal"
)

// Logging Formats
const (
	LogFormatJSON = "json"
	LogFormatText = "text"
)

// Output Destinations
const (
	OutputStdout = "stdout"
	OutputStderr = "stderr"
	OutputFile   = "file"
)

// Checksum Algorithms
const (
	ChecksumAlgorithmMD5    = "md5"
	ChecksumAlgorithmSHA1   = "sha1"
	ChecksumAlgorithmSHA256 = "sha256"
)

// Migration File Name Patterns
const (
	// V{version}__{description}.sql (versioned migrations)
	PatternVersionedMigration = `^V(\d+(?:\.\d+)*)__(.+)\.sql$`
	// R__{description}.sql (repeatable migrations)
	PatternRepeatableMigration = `^R__(.+)\.sql$`
	// {timestamp}_{description}.sql
	PatternTimestamped = `^(\d{14})_(.+)\.sql$`
	// {version}_{description}.sql
	PatternVersioned = `^(\d+)_(.+)\.sql$`
)

// Precondition Types
const (
	PreconditionTypeTableExists       = "tableExists"
	PreconditionTypeColumnExists      = "columnExists"
	PreconditionTypeIndexExists       = "indexExists"
	PreconditionTypeViewExists        = "viewExists"
	PreconditionTypeSQLCheck          = "sqlCheck"
	PreconditionTypeChangeSetExecuted = "changeSetExecuted"
)

// Precondition OnFail Actions
const (
	OnFailHalt     = "HALT"
	OnFailContinue = "CONTINUE"
	OnFailMarkRan  = "MARK_RAN"
	OnFailWarn     = "WARN"
)

// Context and Label Operators
const (
	OperatorAND = "AND"
	OperatorOR  = "OR"
	OperatorNOT = "NOT"
)

// Configuration File Names
const (
	ConfigFileNameYAML       = "migration.yaml"
	ConfigFileNameJSON       = "migration.json"
	ConfigFileNameProperties = "migration.properties"
	ConfigFileNameGoMigrate  = "gomigrate.conf"
	ConfigFileNameChangeset  = "changeset.properties"
)

// Environment Variable Prefixes
const (
	EnvPrefixDefault   = "MIGRATION"
	EnvPrefixGoMigrate = "GOMIGRATE"
	EnvPrefixChangeset = "CHANGESET"
)

// HTTP Status-like Migration Codes
const (
	CodeSuccess           = 200
	CodePartialSuccess    = 206
	CodeBadRequest        = 400
	CodeLockAcquisition   = 423 // Locked
	CodeDependencyMissing = 424 // Failed Dependency
	CodeInternalError     = 500
	CodeTimeout           = 504
)

// Dry Run Constants
const (
	DryRunPrefix = "[DRY RUN]"
	DryRunSuffix = " (simulated)"
)

// Date/Time Formats
const (
	TimestampFormat    = "2006-01-02 15:04:05"
	DateFormat         = "2006-01-02"
	FilenameTimeFormat = "20060102_150405"
	VersionTimeFormat  = "20060102150405"
)

// Separator Characters
const (
	SeparatorUnderscore = "_"
	SeparatorDash       = "-"
	SeparatorDot        = "."
	SeparatorSlash      = "/"
)

// File Permissions
const (
	FilePermissionDefault      = 0644
	DirectoryPermissionDefault = 0755
)

// Buffer Sizes
const (
	DefaultBufferSize = 4096
	LargeBufferSize   = 65536
)

// Cache Settings
const (
	DefaultCacheTTL     = 5 * time.Minute
	DefaultCacheMaxSize = 1000
)

// Metrics and Monitoring
const (
	MetricMigrationsExecuted   = "migrations.executed.total"
	MetricMigrationsFailed     = "migrations.failed.total"
	MetricMigrationsRolledBack = "migrations.rolledback.total"
	MetricMigrationDuration    = "migration.duration"
	MetricLockWaitTime         = "migration.lock.wait.duration"
	MetricValidationDuration   = "migration.validation.duration"
)

// XML Namespace Constants
const (
	XMLNamespaceGoMigrate      = "http://www.gomigrate.org/xml/ns/dbchangelog"
	XMLNamespaceXSI            = "http://www.w3.org/2001/XMLSchema-instance"
	XMLSchemaLocationGoMigrate = "http://www.gomigrate.org/xml/ns/dbchangelog http://www.gomigrate.org/xml/ns/dbchangelog/dbchangelog-1.0.xsd"
)

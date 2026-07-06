/**
 * @file constants.go
 * @description Public constants for migration system
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/constants.go
 */

package migrations

// Migration States
const (
	MigrationStatePending   = "pending"
	MigrationStateRunning   = "running"
	MigrationStateCompleted = "completed"
	MigrationStateFailed    = "failed"
	MigrationStateRolledBack = "rolled_back"
)

// Migration Types
const (
	MigrationTypeSchema = "schema"
	MigrationTypeData   = "data"
	MigrationTypeSeed   = "seed"
	MigrationTypeCustom = "custom"
)

// Rollback Strategies
const (
	RollbackStrategyAutomatic   = "automatic"
	RollbackStrategyManual      = "manual"
	RollbackStrategyConditional = "conditional"
	RollbackStrategySelective   = "selective"
)

// Rollback Impact Levels
const (
	ImpactLevelMinimal  = "minimal"
	ImpactLevelLow      = "low"
	ImpactLevelMedium   = "medium"
	ImpactLevelHigh     = "high"
	ImpactLevelCritical = "critical"
)

// Test Status
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

// Conflict Types
const (
	ConflictTypeDependency = "dependency"
	ConflictTypeData       = "data"
	ConflictTypeSchema     = "schema"
	ConflictTypeSequence   = "sequence"
)

// Default Table Names
const (
	DefaultMigrationsTable = "migration_history"
	DefaultChangelogTable  = "migration_changelog"
	DefaultLockTable       = "migration_locks"
)

// Approval Status
const (
	ApprovalStatusPending  = "pending"
	ApprovalStatusApproved = "approved"
	ApprovalStatusRejected = "rejected"
	ApprovalStatusExpired  = "expired"
)

// Governance Policy Severity
const (
	PolicySeverityInfo    = "info"
	PolicySeverityWarning = "warning"
	PolicySeverityError   = "error"
)

// Log Levels
const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// Default Values
const (
	DefaultMigrationTimeout = 300 // 5 minutes in seconds
	DefaultMaxRetries       = 3
	DefaultBatchSize        = 100
)
/**
 * @file engine_interfaces.go
 * @description Core engine interfaces for the migration system following the design pattern document
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/types/engine_interfaces.go
 */

package types

import (
	"context"
	"time"
)

// Note: MigrationEngine is now a concrete struct in the engine package

// EngineBackend defines the Bridge implementor interface for DB-specific operations
// This decouples engine orchestration from database-specific implementations
type EngineBackend interface {
	// Lock management
	AcquireLock(ctx context.Context) error
	ReleaseLock(ctx context.Context) error

	// Migration execution
	Apply(ctx context.Context, migration MigrationScript) error
	Rollback(ctx context.Context, migration MigrationScript) error

	// Status tracking
	ReadStatus(ctx context.Context) ([]MigrationStatus, error)
	UpdateStatus(ctx context.Context, migration MigrationScript, status string) error

	// Backend information
	GetDialect() string
	SupportsTransactions() bool
	SupportsRollback() bool
}

// ComponentsFactory defines the Abstract Factory interface for creating component families
// Each database type has its own factory that produces cohesive component sets
type ComponentsFactory interface {
	// Core components
	NewBackend(cfg Config) (EngineBackend, error)
	NewExecutor(cfg Config) (MigrationExecutor, error)
	NewRollbackExecutor(cfg Config) (RollbackExecutor, error)
	NewTracker(cfg Config) (MigrationTracker, error)
	NewLockManager(cfg Config) (LockManager, error)
	NewValidator(cfg Config) (MigrationValidator, error)

	// Factory information
	GetSupportedDialect() string
	GetComponentVersion() string
}

// MigrationEngineInterface defines the interface for migration engines
type MigrationEngineInterface interface {
	Status(ctx context.Context) (*MigrationStatus, error)
	Migrate(ctx context.Context, target string, dryRun bool) error
	Rollback(ctx context.Context, steps int) error
	Validate(ctx context.Context) error
	Info(ctx context.Context) (*MigrationInfo, error)
	Close() error
	Initialize(ctx context.Context) error
}

// MigrationEngineFactory defines the main factory interface for creating engines
type MigrationEngineFactory interface {
	CreateEngine(dbType string, config Config) (MigrationEngineInterface, error)
	RegisterBuilder(dbType string, builder EngineBuilder)
	GetSupportedTypes() []string
	IsSupported(dbType string) bool
}

// EngineBuilder defines the Builder interface for constructing migration engines
type EngineBuilder interface {
	WithConfig(config Config) EngineBuilder
	WithBackend(backend EngineBackend) EngineBuilder
	WithExecutor(executor MigrationExecutor) EngineBuilder
	WithTracker(tracker MigrationTracker) EngineBuilder
	WithLockManager(lockMgr LockManager) EngineBuilder
	WithValidator(validator MigrationValidator) EngineBuilder
	WithResolver(resolver DependencyResolver) EngineBuilder
	Build() (MigrationEngineInterface, error)
}

// RollbackExecutor defines the interface for rollback operations
type RollbackExecutor interface {
	CanRollback(ctx context.Context, migration MigrationScript) bool
	GenerateRollback(ctx context.Context, migration MigrationScript) (string, error)
	ExecuteRollback(ctx context.Context, migration MigrationScript) error
	ValidateRollback(ctx context.Context, migration MigrationScript) error
}

// LockManager defines the interface for managing migration locks
type LockManager interface {
	AcquireLock(ctx context.Context, lockID string, timeout time.Duration) error
	ReleaseLock(ctx context.Context, lockID string) error
	IsLocked(ctx context.Context, lockID string) (bool, error)
	GetLockInfo(ctx context.Context, lockID string) (*LockInfo, error)
	RefreshLock(ctx context.Context, lockID string, timeout time.Duration) error
}

// MigrationScript represents a migration script with metadata
type MigrationScript struct {
	ID           string            `json:"id"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Type         MigrationType     `json:"type"`
	Content      string            `json:"content"`
	Checksum     string            `json:"checksum"`
	Dependencies []string          `json:"dependencies"`
	Tags         []string          `json:"tags"`
	Contexts     []string          `json:"contexts"`
	Labels       []string          `json:"labels"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"created_at"`
}

// MigrationResult represents the result of a migration operation
type MigrationResult struct {
	Success          bool                    `json:"success"`
	ExecutedCount    int                     `json:"executed_count"`
	FailedCount      int                     `json:"failed_count"`
	SkippedCount     int                     `json:"skipped_count"`
	TotalTime        time.Duration           `json:"total_time"`
	ExecutedScripts  []MigrationScript       `json:"executed_scripts"`
	FailedScripts    []FailedMigrationScript `json:"failed_scripts"`
	Warnings         []string                `json:"warnings"`
	Errors           []string                `json:"errors"`
}

// DryRunResult represents the result of a dry run operation
type DryRunResult struct {
	WouldExecute     []MigrationScript `json:"would_execute"`
	WouldSkip        []MigrationScript `json:"would_skip"`
	EstimatedTime    time.Duration     `json:"estimated_time"`
	Warnings         []string          `json:"warnings"`
	Errors           []string          `json:"errors"`
	ValidationErrors []ValidationError `json:"validation_errors"`
}

// ValidationResult represents the result of migration validation
type ValidationResult struct {
	Valid            bool              `json:"valid"`
	ValidatedCount   int               `json:"validated_count"`
	ErrorCount       int               `json:"error_count"`
	WarningCount     int               `json:"warning_count"`
	ValidationErrors []ValidationError `json:"validation_errors"`
	Warnings         []string          `json:"warnings"`
}

// FailedMigrationScript represents a failed migration with error details
type FailedMigrationScript struct {
	Script       MigrationScript `json:"script"`
	Error        string          `json:"error"`
	ErrorCode    string          `json:"error_code"`
	FailureTime  time.Time       `json:"failure_time"`
	CanRetry     bool            `json:"can_retry"`
	RetryCount   int             `json:"retry_count"`
	StackTrace   string          `json:"stack_trace,omitempty"`
}

// LockInfo represents information about a migration lock
type LockInfo struct {
	ID          string    `json:"id"`
	AcquiredBy  string    `json:"acquired_by"`
	AcquiredAt  time.Time `json:"acquired_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Hostname    string    `json:"hostname"`
	ProcessID   int       `json:"process_id"`
	Operation   string    `json:"operation"`
	Description string    `json:"description"`
	IsExpired   bool      `json:"is_expired"`
}

// OrderedExecutionPlan represents a dependency-resolved execution plan
type OrderedExecutionPlan struct {
	Phases           []ExecutionPhase  `json:"phases"`
	TotalMigrations  int               `json:"total_migrations"`
	EstimatedTime    time.Duration     `json:"estimated_time"`
	ParallelBatches  [][]MigrationScript `json:"parallel_batches"`
	Dependencies     map[string][]string `json:"dependencies"`
	Constraints      []DependencyConstraint `json:"constraints"`
}

// ExecutionPhase represents a phase in the execution plan
type ExecutionPhase struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Migrations   []MigrationScript `json:"migrations"`
	CanParallel  bool              `json:"can_parallel"`
	Dependencies []string          `json:"dependencies"`
	EstimatedTime time.Duration    `json:"estimated_time"`
}

// DependencyConstraint represents a constraint in dependency resolution
type DependencyConstraint struct {
	Type        string   `json:"type"` // hard, soft, conflict, optional
	Source      string   `json:"source"`
	Target      string   `json:"target"`
	Description string   `json:"description"`
	Weight      int      `json:"weight"`
}

// CompoundMigration represents a group of migrations treated as a unit
type CompoundMigration struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Migrations   []MigrationScript `json:"migrations"`
	Dependencies []string          `json:"dependencies"`
	Atomic       bool              `json:"atomic"`
	Metadata     map[string]string `json:"metadata"`
}

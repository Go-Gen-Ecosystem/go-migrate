package migrations

import (
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/constants"
)

// StatusResponse represents migration status response
type StatusResponse struct {
	CurrentVersion string              `json:"currentVersion"`
	Applied        int                 `json:"applied"`
	Pending        int                 `json:"pending"`
	Failed         int                 `json:"failed"`
	Migrations     []MigrationStatus   `json:"migrations"`
	LastApplied    *time.Time          `json:"lastApplied,omitempty"`
	Timestamp      time.Time           `json:"timestamp"`
}

// MigrationStatus represents individual migration status
type MigrationStatus struct {
	ID              string    `json:"id"`
	Version         string    `json:"version"`
	Description     string    `json:"description"`
	Type            string    `json:"type"`
	Status          string    `json:"status"`
	AppliedAt       *time.Time `json:"appliedAt,omitempty"`
	ExecutionTime   int64     `json:"executionTime,omitempty"`
	Checksum        string    `json:"checksum"`
	Author          string    `json:"author,omitempty"`
	ErrorMessage    string    `json:"errorMessage,omitempty"`
}

// ExecuteRequest represents migration execution request
type ExecuteRequest struct {
	Target     string   `json:"target,omitempty"`
	DryRun     bool     `json:"dryRun"`
	Migrations []string `json:"migrations,omitempty"`
	Context    string   `json:"context,omitempty"`
	Labels     []string `json:"labels,omitempty"`
}

// ExecuteResponse represents migration execution response
type ExecuteResponse struct {
	Success       bool                    `json:"success"`
	Applied       int                     `json:"applied"`
	Failed        int                     `json:"failed"`
	Results       []MigrationResult       `json:"results"`
	TotalDuration int64                   `json:"totalDuration"`
	Timestamp     time.Time               `json:"timestamp"`
}

// MigrationResult represents individual migration execution result
type MigrationResult struct {
	ID            string    `json:"id"`
	Version       string    `json:"version"`
	Description   string    `json:"description"`
	Success       bool      `json:"success"`
	Duration      int64     `json:"duration"`
	ErrorMessage  string    `json:"errorMessage,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// RollbackRequest represents rollback request
type RollbackRequest struct {
	Steps   int    `json:"steps,omitempty"`
	Target  string `json:"target,omitempty"`
	Version string `json:"version,omitempty"`
	DryRun  bool   `json:"dryRun"`
}

// RollbackResponse represents rollback response
type RollbackResponse struct {
	Success       bool                    `json:"success"`
	RolledBack    int                     `json:"rolledBack"`
	Results       []MigrationResult       `json:"results"`
	TotalDuration int64                   `json:"totalDuration"`
	Timestamp     time.Time               `json:"timestamp"`
}

// ValidateRequest represents validation request
type ValidateRequest struct {
	Migrations []string `json:"migrations,omitempty"`
	Strict     bool     `json:"strict"`
}

// ValidateResponse represents validation response
type ValidateResponse struct {
	Valid      bool               `json:"valid"`
	Errors     []ValidationError  `json:"errors,omitempty"`
	Warnings   []ValidationError  `json:"warnings,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
}

// ValidationError represents a validation error or warning
type ValidationError struct {
	MigrationID string                        `json:"migrationId"`
	Severity    constants.ValidationSeverity  `json:"severity"`
	Message     string                        `json:"message"`
	Line        int                           `json:"line,omitempty"`
	Column      int                           `json:"column,omitempty"`
}

// HistoryRequest represents history query request
type HistoryRequest struct {
	Limit      int       `json:"limit,omitempty"`
	Offset     int       `json:"offset,omitempty"`
	Status     string    `json:"status,omitempty"`
	From       time.Time `json:"from,omitempty"`
	To         time.Time `json:"to,omitempty"`
	Author     string    `json:"author,omitempty"`
}

// HistoryResponse represents migration history response
type HistoryResponse struct {
	Migrations []MigrationHistory `json:"migrations"`
	Total      int                `json:"total"`
	Limit      int                `json:"limit"`
	Offset     int                `json:"offset"`
	Timestamp  time.Time          `json:"timestamp"`
}

// MigrationHistory represents historical migration record
type MigrationHistory struct {
	ID            string    `json:"id"`
	Version       string    `json:"version"`
	Description   string    `json:"description"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	Author        string    `json:"author,omitempty"`
	AppliedAt     *time.Time `json:"appliedAt,omitempty"`
	ExecutionTime int64     `json:"executionTime,omitempty"`
	Checksum      string    `json:"checksum"`
	ErrorMessage  string    `json:"errorMessage,omitempty"`
	RolledBackAt  *time.Time `json:"rolledBackAt,omitempty"`
}

// DryRunRequest represents dry run request
type DryRunRequest struct {
	Migrations []string `json:"migrations,omitempty"`
	Target     string   `json:"target,omitempty"`
}

// DryRunResponse represents dry run response
type DryRunResponse struct {
	WillExecute   []MigrationPlan `json:"willExecute"`
	Dependencies  []Dependency    `json:"dependencies"`
	Warnings      []string        `json:"warnings,omitempty"`
	EstimatedTime int64           `json:"estimatedTime"`
	Timestamp     time.Time       `json:"timestamp"`
}

// MigrationPlan represents planned migration execution
type MigrationPlan struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Description   string   `json:"description"`
	Type          string   `json:"type"`
	DependsOn     []string `json:"dependsOn,omitempty"`
	EstimatedTime int64    `json:"estimatedTime"`
	SQL           string   `json:"sql,omitempty"`
}

// Dependency represents migration dependency
type Dependency struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// DiffRequest represents schema diff request
type DiffRequest struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// DiffResponse represents schema diff response
type DiffResponse struct {
	Changes   []SchemaChange `json:"changes"`
	Additions int            `json:"additions"`
	Deletions int            `json:"deletions"`
	Modifications int         `json:"modifications"`
	Timestamp time.Time      `json:"timestamp"`
}

// SchemaChange represents a schema change
type SchemaChange struct {
	Type        string      `json:"type"`
	Object      string      `json:"object"`
	ObjectType  string      `json:"objectType"`
	Action      string      `json:"action"`
	OldValue    interface{} `json:"oldValue,omitempty"`
	NewValue    interface{} `json:"newValue,omitempty"`
	Description string      `json:"description"`
}

// ErrorResponse represents API error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      string    `json:"code"`
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

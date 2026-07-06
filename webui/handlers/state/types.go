package state

import (
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/state"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error     string    `json:"error"`
	Code      string    `json:"code"`
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ValidateRequest represents a schema validation request
type ValidateRequest struct {
	SchemaPath string `json:"schema_path" binding:"required"`
}

// ValidationResponse represents a schema validation response
type ValidationResponse struct {
	Valid        bool                               `json:"valid"`
	Errors       []state.ValidationError           `json:"errors,omitempty"`
	Warnings     []state.ValidationWarning         `json:"warnings,omitempty"`
	Summary      string                             `json:"summary"`
	SchemaPath   string                             `json:"schema_path"`
	ValidatedAt  time.Time                          `json:"validated_at"`
}

// PreviewResponse represents a schema preview response
type PreviewResponse struct {
	HasChanges        bool               `json:"has_changes"`
	TablesAdded       int                `json:"tables_added"`
	TablesDropped     int                `json:"tables_dropped"`
	TablesModified    int                `json:"tables_modified"`
	Summary           state.DiffSummary  `json:"summary"`
	Diff              *state.SchemaDiff  `json:"diff"`
	PreviewedAt       time.Time          `json:"previewed_at"`
}

// PlanRequest represents a migration plan request
type PlanRequest struct {
	SchemaPath string `json:"schema_path" binding:"required"`
}

// PlanResponse represents a migration plan response
type PlanResponse struct {
	HasMigration    bool                       `json:"has_migration"`
	Migration       *state.GeneratedMigration  `json:"migration,omitempty"`
	MigrationID     string                     `json:"migration_id,omitempty"`
	Version         string                     `json:"version,omitempty"`
	SafeToAutoApply bool                       `json:"safe_to_auto_apply"`
	Warnings        []string                   `json:"warnings,omitempty"`
	UpSQL           string                     `json:"up_sql,omitempty"`
	DownSQL         string                     `json:"down_sql,omitempty"`
	Message         string                     `json:"message,omitempty"`
	PlannedAt       time.Time                  `json:"planned_at"`
}

// ApplyRequest represents a migration application request
type ApplyRequest struct {
	Migration *state.GeneratedMigration `json:"migration" binding:"required"`
}

// ApplyResponse represents a migration application response
type ApplyResponse struct {
	Success       bool      `json:"success"`
	MigrationID   string    `json:"migration_id"`
	ExecutionTime int64     `json:"execution_time_ms"`
	AppliedAt     time.Time `json:"applied_at"`
	Message       string    `json:"message"`
}

// SyncRequest represents a database sync request
type SyncRequest struct {
	SchemaPath string `json:"schema_path" binding:"required"`
	DryRun     bool   `json:"dry_run"`
}

// SyncResponse represents a database sync response
type SyncResponse struct {
	Success       bool                  `json:"success"`
	Status        *types.MigrationStatus `json:"status"`
	DryRun        bool                  `json:"dry_run"`
	ExecutionTime int64                 `json:"execution_time_ms"`
	SyncedAt      time.Time             `json:"synced_at"`
	Message       string                `json:"message"`
}

// ExportRequest represents a schema export request
type ExportRequest struct {
	OutputPath string `json:"output_path" binding:"required"`
	Format     string `json:"format" binding:"required,oneof=yaml json"`
}

// ExportResponse represents a schema export response
type ExportResponse struct {
	Success    bool      `json:"success"`
	OutputPath string    `json:"output_path"`
	Format     string    `json:"format"`
	ExportedAt time.Time `json:"exported_at"`
	Message    string    `json:"message"`
}

// CheckSyncResponse represents a sync check response
type CheckSyncResponse struct {
	InSync      bool              `json:"in_sync"`
	HasChanges  bool              `json:"has_changes"`
	ChangeCount int               `json:"change_count"`
	Summary     state.DiffSummary `json:"summary"`
	CheckedAt   time.Time         `json:"checked_at"`
	Message     string            `json:"message"`
}

// DiffResponse represents a detailed diff response
type DiffResponse struct {
	Diff        *state.SchemaDiff `json:"diff"`
	SchemaPath  string            `json:"schema_path"`
	RetrievedAt time.Time         `json:"retrieved_at"`
}

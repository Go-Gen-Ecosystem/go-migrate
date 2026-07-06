/**
 * @file advanced_rollback_types.go
 * @description Request and response types for advanced rollback WebUI handlers
 * @author Admilson B. F. Cossa
 * @date 2025-01-29
 * @path pkg/core/database/migrations/webui/handlers/migrations/advanced_rollback_types.go
 */

package migrations

import "time"

// SelectiveRollbackRequest represents the request body for selective rollback
type SelectiveRollbackRequest struct {
	MigrationID        string `json:"migration_id" binding:"required"`
	Force              bool   `json:"force"`
	RollbackDependents bool   `json:"rollback_dependents"`
	CreateSavepoint    bool   `json:"create_savepoint"`
	DryRun             bool   `json:"dry_run"`
	MaxDepth           int    `json:"max_depth"`
	CreateBackup       bool   `json:"create_backup"`
}

// SelectiveRollbackResponse represents the response for selective rollback
type SelectiveRollbackResponse struct {
	Success               bool                   `json:"success"`
	MigrationID           string                 `json:"migration_id"`
	RolledBackMigrations  []MigrationSummary     `json:"rolled_back_migrations"`
	Errors                []string               `json:"errors,omitempty"`
	Warnings              []string               `json:"warnings,omitempty"`
	Duration              string                 `json:"duration"`
	Savepoints            []string               `json:"savepoints,omitempty"`
	BackupLocation        string                 `json:"backup_location,omitempty"`
	ExecutedAt            time.Time              `json:"executed_at"`
	ImpactAnalysis        ImpactAnalysisResponse `json:"impact_analysis,omitempty"`
}

// AnalyzeRollbackRequest represents the request body for impact analysis
type AnalyzeRollbackRequest struct {
	MigrationIDs []string `json:"migration_ids" binding:"required,min=1"`
}

// ImpactAnalysisResponse represents the response for impact analysis
type ImpactAnalysisResponse struct {
	MigrationID         string                   `json:"migration_id"`
	ImpactLevel         string                   `json:"impact_level"`
	AffectedMigrations  []AffectedMigrationInfo  `json:"affected_migrations"`
	AffectedTables      []string                 `json:"affected_tables"`
	AffectedColumns     []string                 `json:"affected_columns"`
	AffectedIndexes     []string                 `json:"affected_indexes"`
	AffectedConstraints []string                 `json:"affected_constraints"`
	Conflicts           []ConflictInfo           `json:"conflicts"`
	DataLossRisk        bool                     `json:"data_loss_risk"`
	EstimatedDataLoss   *DataLossEstimateInfo    `json:"estimated_data_loss,omitempty"`
	EstimatedDuration   string                   `json:"estimated_duration"`
	Recommendations     []string                 `json:"recommendations"`
	Warnings            []string                 `json:"warnings"`
	CanProceedSafely    bool                     `json:"can_proceed_safely"`
	RequiresManual      bool                     `json:"requires_manual"`
	AnalyzedAt          time.Time                `json:"analyzed_at"`
}

// TestRollbackRequest represents the request body for rollback testing
type TestRollbackRequest struct {
	MigrationIDs []string `json:"migration_ids" binding:"required,min=1"`
	Environment  string   `json:"environment" binding:"required"`
}

// TestRollbackResponse represents the response for rollback testing
type TestRollbackResponse struct {
	Success        bool           `json:"success"`
	Environment    string         `json:"environment"`
	TestCases      []TestCaseInfo `json:"test_cases"`
	TestedAt       time.Time      `json:"tested_at"`
	Duration       string         `json:"duration"`
	TotalTests     int            `json:"total_tests"`
	PassedTests    int            `json:"passed_tests"`
	FailedTests    int            `json:"failed_tests"`
	SkippedTests   int            `json:"skipped_tests"`
	Coverage       float64        `json:"coverage"`
	Errors         []string       `json:"errors,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
	Recommendation string         `json:"recommendation"`
}

// DependencyGraphResponse represents the response for dependency graph
type DependencyGraphResponse struct {
	RootMigration MigrationSummary              `json:"root_migration"`
	Nodes         map[string]DependencyNodeInfo `json:"nodes"`
	Edges         []DependencyEdgeInfo          `json:"edges"`
	Levels        [][]string                    `json:"levels"`
	MaxDepth      int                           `json:"max_depth"`
	TotalNodes    int                           `json:"total_nodes"`
	HasCycles     bool                          `json:"has_cycles"`
	CyclicPaths   [][]string                    `json:"cyclic_paths,omitempty"`
}

// MigrationSummary represents a summary of a migration
type MigrationSummary struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// AffectedMigrationInfo represents information about an affected migration
type AffectedMigrationInfo struct {
	Migration   MigrationSummary `json:"migration"`
	ImpactType  string           `json:"impact_type"`
	ImpactLevel string           `json:"impact_level"`
	CanRollback bool             `json:"can_rollback"`
	Reason      string           `json:"reason,omitempty"`
}

// ConflictInfo represents information about a conflict
type ConflictInfo struct {
	Type            string   `json:"type"`
	Severity        string   `json:"severity"`
	Description     string   `json:"description"`
	AffectedObjects []string `json:"affected_objects"`
	Resolution      string   `json:"resolution"`
	CanAutoResolve  bool     `json:"can_auto_resolve"`
	RequiresManual  bool     `json:"requires_manual"`
}

// DataLossEstimateInfo represents estimated data loss information
type DataLossEstimateInfo struct {
	TablesAffected []string `json:"tables_affected"`
	EstimatedRows  map[string]int64 `json:"estimated_rows"`
	TotalEstimate  int64    `json:"total_estimate"`
	BackupRequired bool     `json:"backup_required"`
}

// TestCaseInfo represents information about a test case
type TestCaseInfo struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	MigrationID     string   `json:"migration_id"`
	Status          string   `json:"status"`
	Duration        string   `json:"duration"`
	Error           string   `json:"error,omitempty"`
	Assertions      []string `json:"assertions"`
	VerifiedObjects []string `json:"verified_objects"`
}

// DependencyNodeInfo represents information about a dependency graph node
type DependencyNodeInfo struct {
	Migration      MigrationSummary `json:"migration"`
	Level          int              `json:"level"`
	Dependencies   []string         `json:"dependencies"`
	Dependents     []string         `json:"dependents"`
	IsRollbackable bool             `json:"is_rollbackable"`
}

// DependencyEdgeInfo represents information about a dependency graph edge
type DependencyEdgeInfo struct {
	From       string `json:"from"`
	To         string `json:"to"`
	EdgeType   string `json:"edge_type"`
	IsBlocking bool   `json:"is_blocking"`
}

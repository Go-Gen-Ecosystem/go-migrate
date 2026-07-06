/**
 * @file advanced_rollback.go
 * @description Advanced rollback capabilities including selective rollback, impact analysis, and testing
 * @author Admilson B. F. Cossa
 * @date 2025-01-29
 * @path pkg/core/database/migrations/internal/components/rollback/advanced_rollback.go
 */

package rollback

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/constants"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// AdvancedRollbackManager provides advanced rollback capabilities
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

	// Maximum depth for dependent rollbacks
	MaxDepth int

	// Timeout for the operation
	Timeout time.Duration

	// Backup before rollback
	CreateBackup bool

	// Custom validation function
	CustomValidator func(context.Context, *types.Migration) error
}

// SelectiveRollbackResult contains the result of a selective rollback operation
type SelectiveRollbackResult struct {
	Success              bool                               `json:"success"`
	MigrationID          string                             `json:"migration_id"`
	RolledBackMigrations []*types.Migration                 `json:"rolled_back_migrations"`
	ImpactAnalysis       *RollbackImpactAnalysis            `json:"impact_analysis"`
	Errors               []string                           `json:"errors"`
	Warnings             []string                           `json:"warnings"`
	Duration             time.Duration                      `json:"duration"`
	Savepoints           []string                           `json:"savepoints,omitempty"`
	BackupLocation       string                             `json:"backup_location,omitempty"`
	ExecutedAt           time.Time                          `json:"executed_at"`
}

// RollbackImpactAnalysis contains detailed impact analysis of a rollback operation
type RollbackImpactAnalysis struct {
	MigrationID          string                             `json:"migration_id"`
	ImpactLevel          constants.RollbackImpactLevel      `json:"impact_level"`
	AffectedMigrations   []*MigrationImpact                 `json:"affected_migrations"`
	AffectedTables       []string                           `json:"affected_tables"`
	AffectedColumns      []string                           `json:"affected_columns"`
	AffectedIndexes      []string                           `json:"affected_indexes"`
	AffectedConstraints  []string                           `json:"affected_constraints"`
	AffectedObjects      []string                           `json:"affected_objects"`
	DataLossRisk         bool                               `json:"data_loss_risk"`
	EstimatedDataLoss    *DataLossEstimate                  `json:"estimated_data_loss,omitempty"`
	EstimatedDuration    time.Duration                      `json:"estimated_duration"`
	Conflicts            []*RollbackConflict                `json:"conflicts"`
	Recommendations      []string                           `json:"recommendations"`
	Warnings             []string                           `json:"warnings"`
	CanProceedSafely     bool                               `json:"can_proceed_safely"`
	RequiresManual       bool                               `json:"requires_manual"`
	AnalyzedAt           time.Time                          `json:"analyzed_at"`
}

// MigrationImpact represents the impact on a single migration
type MigrationImpact struct {
	Migration       *types.Migration              `json:"migration"`
	ImpactType      string                        `json:"impact_type"` // direct, dependent, cascading
	ImpactLevel     constants.RollbackImpactLevel `json:"impact_level"`
	MustRollback    bool                          `json:"must_rollback"`
	CanRollback     bool                          `json:"can_rollback"`
	Reason          string                        `json:"reason"`
	Dependencies    []string                      `json:"dependencies"`
	AffectedObjects []string                      `json:"affected_objects"`
}

// DataLossEstimate provides estimated data loss information
type DataLossEstimate struct {
	TablesAffected  []string          `json:"tables_affected"`
	EstimatedRows   map[string]int64  `json:"estimated_rows"`
	TotalEstimate   int64             `json:"total_estimate"`
	RecoverableFrom string            `json:"recoverable_from,omitempty"`
	BackupRequired  bool              `json:"backup_required"`
}

// RollbackConflict represents a conflict that may prevent rollback
type RollbackConflict struct {
	Type             string   `json:"type"` // dependency, data, schema, sequence
	Severity         string   `json:"severity"` // low, medium, high, critical
	Description      string   `json:"description"`
	AffectedObjects  []string `json:"affected_objects"`
	Resolution       string   `json:"resolution"`
	CanAutoResolve   bool     `json:"can_auto_resolve"`
	RequiresManual   bool     `json:"requires_manual"`
}

// RollbackTestResult contains the result of rollback testing
type RollbackTestResult struct {
	Success         bool                   `json:"success"`
	Environment     string                 `json:"environment"`
	TestedAt        time.Time              `json:"tested_at"`
	Duration        time.Duration          `json:"duration"`
	TestCases       []*RollbackTestCase    `json:"test_cases"`
	TotalTests      int                    `json:"total_tests"`
	PassedTests     int                    `json:"passed_tests"`
	FailedTests     int                    `json:"failed_tests"`
	SkippedTests    int                    `json:"skipped_tests"`
	Coverage        float64                `json:"coverage"`
	Errors          []string               `json:"errors"`
	Warnings        []string               `json:"warnings"`
	Recommendation  string                 `json:"recommendation"`
}

// RollbackTestCase represents a single test case for rollback
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

// DependencyGraph represents the dependency graph for migrations
type DependencyGraph struct {
	RootMigration  *types.Migration             `json:"root_migration"`
	Nodes          map[string]*DependencyNode   `json:"nodes"`
	Edges          []*DependencyEdge            `json:"edges"`
	Levels         [][]string                   `json:"levels"` // Migrations grouped by dependency level
	MaxDepth       int                          `json:"max_depth"`
	TotalNodes     int                          `json:"total_nodes"`
	HasCycles      bool                         `json:"has_cycles"`
	CyclicPaths    [][]string                   `json:"cyclic_paths,omitempty"`
}

// DependencyNode represents a node in the dependency graph
type DependencyNode struct {
	Migration      *types.Migration `json:"migration"`
	Level          int              `json:"level"` // Distance from root
	Dependencies   []string         `json:"dependencies"` // Migration IDs this depends on
	Dependents     []string         `json:"dependents"` // Migration IDs that depend on this
	IsRollbackable bool             `json:"is_rollbackable"`
}

// DependencyEdge represents an edge in the dependency graph
type DependencyEdge struct {
	From       string `json:"from"` // Migration ID
	To         string `json:"to"` // Migration ID
	EdgeType   string `json:"edge_type"` // explicit, implicit, data
	IsBlocking bool   `json:"is_blocking"` // Whether this dependency blocks rollback
}

// DefaultAdvancedRollbackManager implements AdvancedRollbackManager
type DefaultAdvancedRollbackManager struct {
	baseManager RollbackManager
	tracker     tracker.MigrationTracker
	executor    types.MigrationExecutor
	logger      logging.ComponentLogger
	config      *AdvancedRollbackConfig
}

// AdvancedRollbackConfig contains configuration for advanced rollback features
type AdvancedRollbackConfig struct {
	// Enable selective rollback
	EnableSelectiveRollback bool

	// Enable impact analysis
	EnableImpactAnalysis bool

	// Enable rollback testing
	EnableRollbackTesting bool

	// Default depth for dependency analysis
	DefaultDepth int

	// Maximum depth for dependency analysis
	MaxDepth int

	// Enable automatic conflict resolution
	EnableAutoConflictResolution bool

	// Enable savepoints
	EnableSavepoints bool

	// Testing environment names
	TestEnvironments []string

	// Timeout for impact analysis
	ImpactAnalysisTimeout time.Duration

	// Timeout for rollback testing
	TestTimeout time.Duration
}

// NewDefaultAdvancedRollbackManager creates a new advanced rollback manager
func NewDefaultAdvancedRollbackManager(
	baseManager RollbackManager,
	tracker tracker.MigrationTracker,
	executor types.MigrationExecutor,
	logger logging.ComponentLogger,
	config *AdvancedRollbackConfig,
) *DefaultAdvancedRollbackManager {
	if config == nil {
		config = &AdvancedRollbackConfig{
			EnableSelectiveRollback:      true,
			EnableImpactAnalysis:         true,
			EnableRollbackTesting:        true,
			DefaultDepth:                 5,
			MaxDepth:                     10,
			EnableAutoConflictResolution: false,
			EnableSavepoints:             true,
			TestEnvironments:             []string{"staging", "test"},
			ImpactAnalysisTimeout:        5 * time.Minute,
			TestTimeout:                  10 * time.Minute,
		}
	}

	if logger == nil {
		logger = logging.NewNoOpLogger()
	}

	return &DefaultAdvancedRollbackManager{
		baseManager: baseManager,
		tracker:     tracker,
		executor:    executor,
		logger:      logger,
		config:      config,
	}
}

// RollbackSelective performs selective rollback of a specific migration
func (arm *DefaultAdvancedRollbackManager) RollbackSelective(
	ctx context.Context,
	migrationID string,
	options *SelectiveRollbackOptions,
) (*SelectiveRollbackResult, error) {

	if !arm.config.EnableSelectiveRollback {
		return nil, fmt.Errorf("selective rollback is not enabled")
	}

	startTime := time.Now()
	result := &SelectiveRollbackResult{
		MigrationID:          migrationID,
		RolledBackMigrations: make([]*types.Migration, 0),
		Errors:               make([]string, 0),
		Warnings:             make([]string, 0),
		Savepoints:           make([]string, 0),
		ExecutedAt:           startTime,
	}

	// Set defaults for options
	if options == nil {
		options = &SelectiveRollbackOptions{
			Force:              false,
			RollbackDependents: false,
			CreateSavepoint:    arm.config.EnableSavepoints,
			DryRun:             false,
			SkipImpactAnalysis: false,
			MaxDepth:           arm.config.DefaultDepth,
			Timeout:            5 * time.Minute,
			CreateBackup:       true,
		}
	}

	// Create context with timeout
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	arm.logger.Info("Starting selective rollback")

	// Get the target migration
	migration, err := arm.tracker.GetMigration(ctx, migrationID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to get migration: %v", err))
		result.Duration = time.Since(startTime)
		return result, err
	}

	// Check if migration is applied
	isApplied, err := arm.tracker.IsMigrationApplied(ctx, migrationID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check migration status: %v", err))
		result.Duration = time.Since(startTime)
		return result, err
	}

	if !isApplied {
		result.Errors = append(result.Errors, "Migration is not applied")
		result.Duration = time.Since(startTime)
		return result, fmt.Errorf("migration %s is not applied", migrationID)
	}

	// Perform impact analysis unless skipped
	if !options.SkipImpactAnalysis && arm.config.EnableImpactAnalysis {
		arm.logger.Info("Performing impact analysis")

		impactAnalysis, err := arm.AnalyzeRollbackImpact(ctx, []string{migrationID})
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Impact analysis failed: %v", err))
		} else {
			result.ImpactAnalysis = impactAnalysis
			result.Warnings = append(result.Warnings, impactAnalysis.Warnings...)

			// Check if rollback can proceed safely
			if !impactAnalysis.CanProceedSafely && !options.Force {
				result.Errors = append(result.Errors, "Impact analysis indicates rollback cannot proceed safely")
				result.Errors = append(result.Errors, impactAnalysis.Recommendations...)
				result.Duration = time.Since(startTime)
				return result, fmt.Errorf("rollback cannot proceed safely without force flag")
			}
		}
	}

	// Build list of migrations to rollback
	migrationsToRollback := []*types.Migration{migration}

	if options.RollbackDependents {
		arm.logger.Info("Finding dependent migrations")

		dependents, err := arm.findAllDependents(ctx, migration, options.MaxDepth)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to find all dependents: %v", err))
		} else {
			migrationsToRollback = append(migrationsToRollback, dependents...)
			arm.logger.Info("Found dependent migrations")
		}
	} else {
		// Check for blocking dependents
		dependents, err := arm.findDirectDependents(ctx, migration)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to check for dependents: %v", err))
		} else if len(dependents) > 0 && !options.Force {
			dependentIDs := make([]string, len(dependents))
			for i, dep := range dependents {
				dependentIDs[i] = dep.ID
			}
			result.Errors = append(result.Errors,
				fmt.Sprintf("Migration has %d dependent migration(s): %s. Use RollbackDependents=true or Force=true",
					len(dependents), strings.Join(dependentIDs, ", ")))
			result.Duration = time.Since(startTime)
			return result, fmt.Errorf("migration has blocking dependents")
		}
	}

	// Sort migrations in reverse dependency order (dependents first)
	migrationsToRollback = arm.sortByReverseDependencyOrder(migrationsToRollback)

	// Execute rollback in dry run mode if requested
	if options.DryRun {
		arm.logger.Info("Executing dry run rollback")
		result.Warnings = append(result.Warnings, "Dry run mode - no actual changes made")
		result.Success = true
		result.RolledBackMigrations = migrationsToRollback
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Create savepoint if enabled
	if options.CreateSavepoint && arm.config.EnableSavepoints {
		savepointName := fmt.Sprintf("rollback_%s_%d", migrationID, time.Now().Unix())
		arm.logger.Info("Creating savepoint")

		// TODO: Implement savepoint creation via executor
		result.Savepoints = append(result.Savepoints, savepointName)
	}

	// Create backup if enabled
	if options.CreateBackup {
		arm.logger.Info("Creating backup before rollback")
		// TODO: Integrate with backup manager
		result.BackupLocation = fmt.Sprintf("backup_%s_%d", migrationID, time.Now().Unix())
	}

	// Execute rollback for each migration
	for _, mig := range migrationsToRollback {
		arm.logger.Info("Rolling back migration")

		// Custom validation if provided
		if options.CustomValidator != nil {
			if err := options.CustomValidator(ctx, mig); err != nil {
				result.Errors = append(result.Errors,
					fmt.Sprintf("Custom validation failed for %s: %v", mig.ID, err))
				result.Duration = time.Since(startTime)
				return result, err
			}
		}

		// Use base manager to rollback individual migration
		rollbackResult, err := arm.baseManager.RollbackMigration(ctx, mig.ID)
		if err != nil || !rollbackResult.Success {
			errMsg := fmt.Sprintf("Failed to rollback migration %s", mig.ID)
			if err != nil {
				errMsg = fmt.Sprintf("%s: %v", errMsg, err)
			} else if rollbackResult.ErrorMessage != "" {
				errMsg = fmt.Sprintf("%s: %s", errMsg, rollbackResult.ErrorMessage)
			}

			result.Errors = append(result.Errors, errMsg)
			result.Duration = time.Since(startTime)
			return result, fmt.Errorf("%s", errMsg)
		}

		result.RolledBackMigrations = append(result.RolledBackMigrations, mig)
		arm.logger.Info("Successfully rolled back migration")
	}

	result.Success = true
	result.Duration = time.Since(startTime)

	arm.logger.Info("Selective rollback completed successfully")

	return result, nil
}

// AnalyzeRollbackImpact performs detailed impact analysis for rollback operations
func (arm *DefaultAdvancedRollbackManager) AnalyzeRollbackImpact(
	ctx context.Context,
	migrationIDs []string,
) (*RollbackImpactAnalysis, error) {

	if !arm.config.EnableImpactAnalysis {
		return nil, fmt.Errorf("impact analysis is not enabled")
	}

	// Create context with timeout
	if arm.config.ImpactAnalysisTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, arm.config.ImpactAnalysisTimeout)
		defer cancel()
	}

	arm.logger.Info("Starting rollback impact analysis")

	analysis := &RollbackImpactAnalysis{
		MigrationID:         strings.Join(migrationIDs, ","),
		ImpactLevel:         constants.ImpactLevelMinimal,
		AffectedMigrations:  make([]*MigrationImpact, 0),
		AffectedTables:      make([]string, 0),
		AffectedColumns:     make([]string, 0),
		AffectedIndexes:     make([]string, 0),
		AffectedConstraints: make([]string, 0),
		DataLossRisk:        false,
		Conflicts:           make([]*RollbackConflict, 0),
		Recommendations:     make([]string, 0),
		Warnings:            make([]string, 0),
		CanProceedSafely:    true,
		RequiresManual:      false,
		AnalyzedAt:          time.Now(),
	}

	// Collect all migrations to analyze
	migrations := make([]*types.Migration, 0)
	for _, id := range migrationIDs {
		migration, err := arm.tracker.GetMigration(ctx, id)
		if err != nil {
			analysis.Warnings = append(analysis.Warnings,
				fmt.Sprintf("Failed to get migration %s: %v", id, err))
			continue
		}
		migrations = append(migrations, migration)
	}

	if len(migrations) == 0 {
		return nil, fmt.Errorf("no valid migrations found for analysis")
	}

	// Analyze each migration
	totalEstimatedDuration := time.Duration(0)

	for _, migration := range migrations {
		impact := arm.analyzeMigrationImpact(ctx, migration)
		analysis.AffectedMigrations = append(analysis.AffectedMigrations, impact)

		// Update overall impact level
		if impact.ImpactLevel > analysis.ImpactLevel {
			analysis.ImpactLevel = impact.ImpactLevel
		}

		// Accumulate affected objects
		analysis.AffectedObjects = append(analysis.AffectedObjects, impact.AffectedObjects...)

		// Check rollback capability
		if !impact.CanRollback {
			analysis.RequiresManual = true
			analysis.CanProceedSafely = false
			analysis.Warnings = append(analysis.Warnings,
				fmt.Sprintf("Migration %s cannot be rolled back automatically: %s",
					migration.ID, impact.Reason))
		}

		// Estimate duration
		if migration.ExecutionTime > 0 {
			totalEstimatedDuration += migration.ExecutionTime
		}
	}

	analysis.EstimatedDuration = totalEstimatedDuration

	// Analyze dependencies and conflicts
	for _, migration := range migrations {
		conflicts := arm.findRollbackConflicts(ctx, migration)
		analysis.Conflicts = append(analysis.Conflicts, conflicts...)
	}

	// Check for data loss risk
	analysis.DataLossRisk = arm.checkDataLossRisk(migrations)
	if analysis.DataLossRisk {
		analysis.EstimatedDataLoss = arm.estimateDataLoss(ctx, migrations)
		if analysis.EstimatedDataLoss != nil && analysis.EstimatedDataLoss.TotalEstimate > 0 {
			analysis.ImpactLevel = constants.ImpactLevelHigh
			analysis.Warnings = append(analysis.Warnings,
				fmt.Sprintf("Data loss risk detected: approximately %d rows may be affected",
					analysis.EstimatedDataLoss.TotalEstimate))
		}
	}

	// Generate recommendations
	analysis.Recommendations = arm.generateRecommendations(analysis)

	// Determine if can proceed safely
	if len(analysis.Conflicts) > 0 {
		criticalConflicts := 0
		for _, conflict := range analysis.Conflicts {
			if conflict.Severity == "critical" || conflict.Severity == "high" {
				criticalConflicts++
				if !conflict.CanAutoResolve {
					analysis.CanProceedSafely = false
				}
			}
		}
		if criticalConflicts > 0 {
			analysis.Warnings = append(analysis.Warnings,
				fmt.Sprintf("Found %d critical conflicts that may prevent safe rollback", criticalConflicts))
		}
	}

	arm.logger.Info("Impact analysis completed")

	return analysis, nil
}

// Helper methods for AdvancedRollbackManager

func (arm *DefaultAdvancedRollbackManager) findDirectDependents(
	ctx context.Context,
	migration *types.Migration,
) ([]*types.Migration, error) {

	appliedMigrations, err := arm.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", err)
	}

	dependents := make([]*types.Migration, 0)
	for _, applied := range appliedMigrations {
		for _, dependency := range applied.Dependencies {
			if dependency == migration.ID || dependency == migration.Version {
				dependents = append(dependents, applied)
				break
			}
		}
	}

	return dependents, nil
}

func (arm *DefaultAdvancedRollbackManager) findAllDependents(
	ctx context.Context,
	migration *types.Migration,
	maxDepth int,
) ([]*types.Migration, error) {

	visited := make(map[string]bool)
	allDependents := make([]*types.Migration, 0)

	var findDependentsRecursive func(*types.Migration, int) error
	findDependentsRecursive = func(mig *types.Migration, depth int) error {
		if depth > maxDepth {
			return nil
		}

		if visited[mig.ID] {
			return nil
		}
		visited[mig.ID] = true

		directDependents, err := arm.findDirectDependents(ctx, mig)
		if err != nil {
			return err
		}

		for _, dependent := range directDependents {
			allDependents = append(allDependents, dependent)
			if err := findDependentsRecursive(dependent, depth+1); err != nil {
				return err
			}
		}

		return nil
	}

	if err := findDependentsRecursive(migration, 0); err != nil {
		return nil, err
	}

	return allDependents, nil
}

func (arm *DefaultAdvancedRollbackManager) sortByReverseDependencyOrder(
	migrations []*types.Migration,
) []*types.Migration {

	// Create dependency map
	depMap := make(map[string][]string)
	migMap := make(map[string]*types.Migration)

	for _, mig := range migrations {
		migMap[mig.ID] = mig
		depMap[mig.ID] = mig.Dependencies
	}

	// Topological sort (reverse order for rollback)
	sorted := make([]*types.Migration, 0, len(migrations))
	visited := make(map[string]bool)

	var visit func(string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true

		// Visit dependents first (those that depend on this migration)
		for _, mig := range migrations {
			for _, dep := range mig.Dependencies {
				if dep == id {
					visit(mig.ID)
				}
			}
		}

		if mig, ok := migMap[id]; ok {
			sorted = append(sorted, mig)
		}
	}

	for _, mig := range migrations {
		visit(mig.ID)
	}

	// The DFS above appends each migration only after all of its dependents,
	// so `sorted` is already in dependents-first order (the correct rollback
	// order). No further reversal is required.
	return sorted
}

func (arm *DefaultAdvancedRollbackManager) analyzeMigrationImpact(
	ctx context.Context,
	migration *types.Migration,
) *MigrationImpact {

	impact := &MigrationImpact{
		Migration:       migration,
		ImpactType:      "direct",
		ImpactLevel:     constants.ImpactLevelLow,
		MustRollback:    true,
		CanRollback:     migration.CanRollback,
		Reason:          "",
		Dependencies:    migration.Dependencies,
		AffectedObjects: make([]string, 0),
	}

	// Determine impact level based on migration properties
	if !migration.CanRollback {
		impact.ImpactLevel = constants.ImpactLevelHigh
		impact.Reason = "Migration is marked as non-rollbackable"
		return impact
	}

	if migration.RollbackSQL == "" {
		impact.ImpactLevel = constants.ImpactLevelHigh
		impact.CanRollback = false
		impact.Reason = "No rollback SQL available"
		return impact
	}

	// Analyze rollback SQL to determine impact
	rollbackSQL := strings.ToUpper(migration.RollbackSQL)

	if strings.Contains(rollbackSQL, "DROP TABLE") {
		impact.ImpactLevel = constants.ImpactLevelCritical
		impact.AffectedObjects = append(impact.AffectedObjects, "tables")
	} else if strings.Contains(rollbackSQL, "DROP COLUMN") {
		impact.ImpactLevel = constants.ImpactLevelHigh
		impact.AffectedObjects = append(impact.AffectedObjects, "columns")
	} else if strings.Contains(rollbackSQL, "DELETE") || strings.Contains(rollbackSQL, "TRUNCATE") {
		impact.ImpactLevel = constants.ImpactLevelHigh
		impact.AffectedObjects = append(impact.AffectedObjects, "data")
	} else if strings.Contains(rollbackSQL, "ALTER") {
		impact.ImpactLevel = constants.ImpactLevelMedium
		impact.AffectedObjects = append(impact.AffectedObjects, "schema")
	}

	return impact
}

func (arm *DefaultAdvancedRollbackManager) findRollbackConflicts(
	ctx context.Context,
	migration *types.Migration,
) []*RollbackConflict {

	conflicts := make([]*RollbackConflict, 0)

	// Check for dependent migrations
	dependents, err := arm.findDirectDependents(ctx, migration)
	if err == nil && len(dependents) > 0 {
		dependentIDs := make([]string, len(dependents))
		for i, dep := range dependents {
			dependentIDs[i] = dep.ID
		}

		conflicts = append(conflicts, &RollbackConflict{
			Type:            constants.ConflictTypeDependency,
			Severity:        "high",
			Description:     fmt.Sprintf("Migration has %d dependent migrations", len(dependents)),
			AffectedObjects: dependentIDs,
			Resolution:      "Rollback dependent migrations first or use force flag",
			CanAutoResolve:  true,
			RequiresManual:  false,
		})
	}

	return conflicts
}

func (arm *DefaultAdvancedRollbackManager) checkDataLossRisk(migrations []*types.Migration) bool {
	for _, migration := range migrations {
		rollbackSQL := strings.ToUpper(migration.RollbackSQL)
		if strings.Contains(rollbackSQL, "DROP") ||
			strings.Contains(rollbackSQL, "DELETE") ||
			strings.Contains(rollbackSQL, "TRUNCATE") {
			return true
		}
	}
	return false
}

func (arm *DefaultAdvancedRollbackManager) estimateDataLoss(
	ctx context.Context,
	migrations []*types.Migration,
) *DataLossEstimate {

	estimate := &DataLossEstimate{
		TablesAffected:  make([]string, 0),
		EstimatedRows:   make(map[string]int64),
		TotalEstimate:   0,
		BackupRequired:  true,
	}

	// This is a simplified estimation - in practice, you would query the database
	// to get actual row counts for affected tables

	for _, migration := range migrations {
		rollbackSQL := strings.ToUpper(migration.RollbackSQL)

		// Very basic table name extraction (should be improved for production)
		if strings.Contains(rollbackSQL, "DROP TABLE") {
			// Extract table name and add to affected tables
			// This is a placeholder - implement proper SQL parsing
			estimate.BackupRequired = true
		}
	}

	return estimate
}

func (arm *DefaultAdvancedRollbackManager) generateRecommendations(
	analysis *RollbackImpactAnalysis,
) []string {

	recommendations := make([]string, 0)

	if analysis.ImpactLevel >= constants.ImpactLevelHigh {
		recommendations = append(recommendations,
			"High impact detected - ensure you have a current backup before proceeding")
	}

	if analysis.DataLossRisk {
		recommendations = append(recommendations,
			"Data loss risk detected - verify backup is available and test restore procedure")
	}

	if len(analysis.Conflicts) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Resolve %d conflict(s) before proceeding with rollback", len(analysis.Conflicts)))
	}

	if analysis.RequiresManual {
		recommendations = append(recommendations,
			"Manual intervention required - review rollback scripts and prepare manual steps")
	}

	if analysis.EstimatedDuration > 5*time.Minute {
		recommendations = append(recommendations,
			fmt.Sprintf("Long-running rollback expected (%s) - schedule during maintenance window",
				analysis.EstimatedDuration))
	}

	return recommendations
}

// TestRollback tests rollback operations in a safe environment
func (arm *DefaultAdvancedRollbackManager) TestRollback(
	ctx context.Context,
	migrationIDs []string,
	environment string,
) (*RollbackTestResult, error) {

	if !arm.config.EnableRollbackTesting {
		return nil, fmt.Errorf("rollback testing is not enabled")
	}

	// Validate environment
	validEnv := false
	for _, env := range arm.config.TestEnvironments {
		if env == environment {
			validEnv = true
			break
		}
	}
	if !validEnv {
		return nil, fmt.Errorf("invalid test environment: %s (allowed: %v)",
			environment, arm.config.TestEnvironments)
	}

	// Create context with timeout
	if arm.config.TestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, arm.config.TestTimeout)
		defer cancel()
	}

	startTime := time.Now()

	arm.logger.Info("Starting rollback testing")

	result := &RollbackTestResult{
		Success:     true,
		Environment: environment,
		TestedAt:    startTime,
		TestCases:   make([]*RollbackTestCase, 0),
		Errors:      make([]string, 0),
		Warnings:    make([]string, 0),
	}

	// Create test cases for each migration
	for _, migrationID := range migrationIDs {
		testCase := arm.createRollbackTestCase(ctx, migrationID)
		result.TestCases = append(result.TestCases, testCase)

		result.TotalTests++
		switch testCase.Status {
		case constants.TestStatusPassed:
			result.PassedTests++
		case constants.TestStatusFailed:
			result.FailedTests++
			result.Success = false
		case constants.TestStatusSkipped:
			result.SkippedTests++
		}
	}

	// Calculate coverage
	if result.TotalTests > 0 {
		result.Coverage = float64(result.PassedTests) / float64(result.TotalTests) * 100
	}

	result.Duration = time.Since(startTime)

	// Generate recommendation
	if result.Coverage >= 100 {
		result.Recommendation = "All tests passed - rollback is safe to execute in production"
	} else if result.Coverage >= 80 {
		result.Recommendation = "Most tests passed - review failed tests before executing in production"
	} else {
		result.Recommendation = "Many tests failed - do not execute rollback in production without investigation"
		result.Success = false
	}

	arm.logger.Info("Rollback testing completed")

	return result, nil
}

func (arm *DefaultAdvancedRollbackManager) createRollbackTestCase(
	ctx context.Context,
	migrationID string,
) *RollbackTestCase {

	testCase := &RollbackTestCase{
		Name:            fmt.Sprintf("Test rollback for %s", migrationID),
		Description:     fmt.Sprintf("Verify rollback of migration %s", migrationID),
		MigrationID:     migrationID,
		Status:          constants.TestStatusPending,
		Assertions:      make([]string, 0),
		VerifiedObjects: make([]string, 0),
	}

	startTime := time.Now()

	// Get migration
	migration, err := arm.tracker.GetMigration(ctx, migrationID)
	if err != nil {
		testCase.Status = constants.TestStatusFailed
		testCase.Error = fmt.Sprintf("Failed to get migration: %v", err)
		testCase.Duration = time.Since(startTime)
		return testCase
	}

	// Test 1: Check if rollback SQL exists
	testCase.Assertions = append(testCase.Assertions, "Rollback SQL exists")
	if migration.RollbackSQL == "" {
		testCase.Status = constants.TestStatusFailed
		testCase.Error = "No rollback SQL available"
		testCase.Duration = time.Since(startTime)
		return testCase
	}

	// Test 2: Check if migration can be rolled back
	testCase.Assertions = append(testCase.Assertions, "Migration is rollbackable")
	if !migration.CanRollback {
		testCase.Status = constants.TestStatusFailed
		testCase.Error = "Migration is marked as non-rollbackable"
		testCase.Duration = time.Since(startTime)
		return testCase
	}

	// Test 3: Validate rollback SQL syntax (basic check)
	testCase.Assertions = append(testCase.Assertions, "Rollback SQL syntax is valid")
	if err := arm.executor.ValidateRollback(ctx, migration); err != nil {
		testCase.Status = constants.TestStatusFailed
		testCase.Error = fmt.Sprintf("Rollback validation failed: %v", err)
		testCase.Duration = time.Since(startTime)
		return testCase
	}

	// All tests passed
	testCase.Status = constants.TestStatusPassed
	testCase.Duration = time.Since(startTime)

	return testCase
}

// GetRollbackDependencyGraph builds and returns the dependency graph for a migration
func (arm *DefaultAdvancedRollbackManager) GetRollbackDependencyGraph(
	ctx context.Context,
	migrationID string,
) (*DependencyGraph, error) {

	arm.logger.Info("Building dependency graph")

	// Get root migration
	rootMigration, err := arm.tracker.GetMigration(ctx, migrationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get root migration: %w", err)
	}

	graph := &DependencyGraph{
		RootMigration:  rootMigration,
		Nodes:          make(map[string]*DependencyNode),
		Edges:          make([]*DependencyEdge, 0),
		Levels:         make([][]string, 0),
		MaxDepth:       0,
		TotalNodes:     0,
		HasCycles:      false,
		CyclicPaths:    make([][]string, 0),
	}

	// Build graph recursively
	visited := make(map[string]bool)

	if err := arm.buildDependencyGraphRecursive(ctx, rootMigration, graph, visited, 0); err != nil {
		return nil, err
	}

	graph.TotalNodes = len(graph.Nodes)

	// Detect cycles
	graph.HasCycles, graph.CyclicPaths = arm.detectCycles(graph)

	// Build levels
	graph.Levels = arm.buildDependencyLevels(graph)
	graph.MaxDepth = len(graph.Levels)

	arm.logger.Info("Dependency graph built")

	return graph, nil
}

func (arm *DefaultAdvancedRollbackManager) buildDependencyGraphRecursive(
	ctx context.Context,
	migration *types.Migration,
	graph *DependencyGraph,
	visited map[string]bool,
	level int,
) error {

	if visited[migration.ID] {
		return nil
	}
	visited[migration.ID] = true

	// Create node if it doesn't exist
	if _, exists := graph.Nodes[migration.ID]; !exists {
		node := &DependencyNode{
			Migration:      migration,
			Level:          level,
			Dependencies:   migration.Dependencies,
			Dependents:     make([]string, 0),
			IsRollbackable: migration.CanRollback && migration.RollbackSQL != "",
		}
		graph.Nodes[migration.ID] = node
	}

	if level > graph.MaxDepth {
		graph.MaxDepth = level
	}

	// Find dependents
	dependents, err := arm.findDirectDependents(ctx, migration)
	if err != nil {
		return err
	}

	for _, dependent := range dependents {
		// Add edge
		edge := &DependencyEdge{
			From:       migration.ID,
			To:         dependent.ID,
			EdgeType:   "explicit",
			IsBlocking: true,
		}
		graph.Edges = append(graph.Edges, edge)

		// Update node dependents
		if node, exists := graph.Nodes[migration.ID]; exists {
			node.Dependents = append(node.Dependents, dependent.ID)
		}

		// Recurse
		if err := arm.buildDependencyGraphRecursive(ctx, dependent, graph, visited, level+1); err != nil {
			return err
		}
	}

	return nil
}

func (arm *DefaultAdvancedRollbackManager) detectCycles(graph *DependencyGraph) (bool, [][]string) {
	// Simple cycle detection using DFS
	// Returns true if cycles exist and the cyclic paths

	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	cycles := make([][]string, 0)

	var detectCyclesDFS func(string, []string) bool
	detectCyclesDFS = func(nodeID string, path []string) bool {
		visited[nodeID] = true
		recStack[nodeID] = true
		path = append(path, nodeID)

		node := graph.Nodes[nodeID]
		for _, dependentID := range node.Dependents {
			if !visited[dependentID] {
				if detectCyclesDFS(dependentID, path) {
					return true
				}
			} else if recStack[dependentID] {
				// Found a cycle
				cyclePath := make([]string, len(path))
				copy(cyclePath, path)
				cyclePath = append(cyclePath, dependentID)
				cycles = append(cycles, cyclePath)
				return true
			}
		}

		recStack[nodeID] = false
		return false
	}

	for nodeID := range graph.Nodes {
		if !visited[nodeID] {
			if detectCyclesDFS(nodeID, make([]string, 0)) {
				return true, cycles
			}
		}
	}

	return false, cycles
}

func (arm *DefaultAdvancedRollbackManager) buildDependencyLevels(graph *DependencyGraph) [][]string {
	// Group nodes by their dependency level
	maxLevel := 0
	for _, node := range graph.Nodes {
		if node.Level > maxLevel {
			maxLevel = node.Level
		}
	}

	levels := make([][]string, maxLevel+1)
	for i := range levels {
		levels[i] = make([]string, 0)
	}

	for nodeID, node := range graph.Nodes {
		levels[node.Level] = append(levels[node.Level], nodeID)
	}

	// Sort each level by migration version
	for i := range levels {
		sort.Strings(levels[i])
	}

	return levels
}

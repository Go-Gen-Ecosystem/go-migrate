/**
 * @file advanced_rollback_test.go
 * @description Comprehensive tests for advanced rollback capabilities
 * @author Admilson B. F. Cossa
 * @date 2025-01-29
 * @path pkg/core/database/migrations/internal/components/rollback/advanced_rollback_test.go
 */

package rollback

import (
	"context"
	"testing"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/constants"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// Mock implementations for testing

type mockTracker struct {
	migrations        map[string]*types.Migration
	appliedMigrations []*types.Migration
}

func newMockTracker() *mockTracker {
	return &mockTracker{
		migrations:        make(map[string]*types.Migration),
		appliedMigrations: make([]*types.Migration, 0),
	}
}

func (m *mockTracker) GetMigration(ctx context.Context, id string) (*types.Migration, error) {
	if mig, ok := m.migrations[id]; ok {
		return mig, nil
	}
	return nil, nil
}

func (m *mockTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	for _, mig := range m.migrations {
		if mig.Version == version {
			return mig, nil
		}
	}
	return nil, nil
}

func (m *mockTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	return m.appliedMigrations, nil
}

func (m *mockTracker) IsMigrationApplied(ctx context.Context, id string) (bool, error) {
	for _, mig := range m.appliedMigrations {
		if mig.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockTracker) RecordMigration(ctx context.Context, migration *types.Migration) error {
	return nil
}

func (m *mockTracker) RecordChangelog(ctx context.Context, entry interface{}) error {
	return nil
}

func (m *mockTracker) GetExecutedMigrations(ctx context.Context) ([]types.MigrationRecord, error) {
	return nil, nil
}

func (m *mockTracker) GetFailedMigrations(ctx context.Context) ([]types.MigrationRecord, error) {
	return nil, nil
}

func (m *mockTracker) RecordMigrationSuccess(ctx context.Context, migration types.MigrationRecord, executionTime int64) error {
	return nil
}

func (m *mockTracker) RecordMigrationFailure(ctx context.Context, migration types.MigrationRecord, errorMsg string) error {
	return nil
}

func (m *mockTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	return nil
}

func (m *mockTracker) IsLocked(ctx context.Context) (bool, error) {
	return false, nil
}

func (m *mockTracker) AcquireLock(ctx context.Context) error {
	return nil
}

func (m *mockTracker) ReleaseLock(ctx context.Context) error {
	return nil
}

func (m *mockTracker) EnsureSchemaTable(ctx context.Context) error {
	return nil
}

func (m *mockTracker) GetCurrentVersion(ctx context.Context) (string, error) {
	return "", nil
}

type mockExecutor struct{}

func (m *mockExecutor) Execute(ctx context.Context, migration *types.Migration) error {
	return nil
}

func (m *mockExecutor) ExecuteDryRun(ctx context.Context, migration *types.Migration) (*types.ExecutionPlan, error) {
	return &types.ExecutionPlan{}, nil
}

func (m *mockExecutor) Rollback(ctx context.Context, migration *types.Migration) error {
	return nil
}

func (m *mockExecutor) ValidateRollback(ctx context.Context, migration *types.Migration) error {
	return nil
}

func (m *mockExecutor) ValidateMigration(ctx context.Context, migration *types.Migration) error {
	return nil
}

func (m *mockExecutor) GetExecutionPlan(migrations []*types.Migration) ([]string, error) {
	return nil, nil
}

type mockValidator struct{}

func (m *mockValidator) ValidateMigrations(migrations []types.Migration) error {
	return nil
}

func (m *mockValidator) ValidateMigration(migration *types.Migration) error {
	return nil
}

func (m *mockValidator) CheckDependencies(migrations []types.Migration) error {
	return nil
}

func (m *mockValidator) CheckCircularDependencies(migrations []types.Migration) error {
	return nil
}

type mockBaseRollbackManager struct {
	callCount int
}

func (m *mockBaseRollbackManager) CanRollback(ctx context.Context, migrationID string) (bool, []string, error) {
	return true, []string{}, nil
}

func (m *mockBaseRollbackManager) RollbackToVersion(ctx context.Context, version string) (*types.RollbackResult, error) {
	return &types.RollbackResult{Success: true}, nil
}

func (m *mockBaseRollbackManager) RollbackCount(ctx context.Context, count int) (*types.RollbackResult, error) {
	return &types.RollbackResult{Success: true}, nil
}

func (m *mockBaseRollbackManager) RollbackMigration(ctx context.Context, migrationID string) (*types.RollbackResult, error) {
	m.callCount++
	return &types.RollbackResult{
		Success:      true,
		MigrationID:  migrationID,
		RolledBackAt: time.Now(),
	}, nil
}

func (m *mockBaseRollbackManager) GetRollbackPlan(ctx context.Context, migrationIDs []string) (*types.RollbackPlan, error) {
	return &types.RollbackPlan{CanProceed: true}, nil
}

func (m *mockBaseRollbackManager) GetAdvancedManager() AdvancedRollbackManager {
	return nil
}

// Test helper functions

func createTestMigration(id, version string, canRollback bool, dependencies []string) *types.Migration {
	now := time.Now()
	return &types.Migration{
		ID:           id,
		Version:      version,
		Description:  "Test migration",
		Type:         types.MigrationTypeSQL,
		Script:       "CREATE TABLE test (id INT);",
		CanRollback:  canRollback,
		RollbackSQL:  "DROP TABLE test;",
		State:        constants.MigrationStateApplied,
		ExecutedAt:   &now,
		Dependencies: dependencies,
	}
}

// Tests

func TestSelectiveRollback_Success(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration := createTestMigration("001", "1.0.0", true, []string{})
	tracker.migrations["001"] = migration
	tracker.appliedMigrations = []*types.Migration{migration}

	config := &AdvancedRollbackConfig{
		EnableSelectiveRollback: true,
		EnableImpactAnalysis:    true,
		DefaultDepth:            5,
		MaxDepth:                10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	options := &SelectiveRollbackOptions{
		Force:              false,
		RollbackDependents: false,
		DryRun:             false,
	}

	result, err := advManager.RollbackSelective(ctx, "001", options)

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Error("Expected success=true")
	}

	if len(result.RolledBackMigrations) != 1 {
		t.Errorf("Expected 1 rolled back migration, got %d", len(result.RolledBackMigrations))
	}

	if baseManager.callCount != 1 {
		t.Errorf("Expected base manager to be called once, got %d calls", baseManager.callCount)
	}
}

func TestSelectiveRollback_WithDependents(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration1 := createTestMigration("001", "1.0.0", true, []string{})
	migration2 := createTestMigration("002", "2.0.0", true, []string{"001"})
	migration3 := createTestMigration("003", "3.0.0", true, []string{"002"})

	tracker.migrations["001"] = migration1
	tracker.migrations["002"] = migration2
	tracker.migrations["003"] = migration3
	tracker.appliedMigrations = []*types.Migration{migration1, migration2, migration3}

	config := &AdvancedRollbackConfig{
		EnableSelectiveRollback: true,
		EnableImpactAnalysis:    true,
		DefaultDepth:            5,
		MaxDepth:                10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute - rollback migration 001 with dependents
	options := &SelectiveRollbackOptions{
		Force:              false,
		RollbackDependents: true,
		DryRun:             false,
		MaxDepth:           5,
	}

	result, err := advManager.RollbackSelective(ctx, "001", options)

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Error("Expected success=true")
	}

	// Should rollback all 3 migrations (001 + 2 dependents)
	if len(result.RolledBackMigrations) != 3 {
		t.Errorf("Expected 3 rolled back migrations, got %d", len(result.RolledBackMigrations))
	}

	// Verify order: dependents should be rolled back first
	if result.RolledBackMigrations[0].ID != "003" {
		t.Errorf("Expected first rollback to be 003, got %s", result.RolledBackMigrations[0].ID)
	}
}

func TestSelectiveRollback_DryRun(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration := createTestMigration("001", "1.0.0", true, []string{})
	tracker.migrations["001"] = migration
	tracker.appliedMigrations = []*types.Migration{migration}

	config := &AdvancedRollbackConfig{
		EnableSelectiveRollback: true,
		DefaultDepth:            5,
		MaxDepth:                10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	options := &SelectiveRollbackOptions{
		DryRun: true,
	}

	result, err := advManager.RollbackSelective(ctx, "001", options)

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Error("Expected success=true for dry run")
	}

	// Base manager should not be called in dry run mode
	if baseManager.callCount != 0 {
		t.Errorf("Expected base manager not to be called in dry run, got %d calls", baseManager.callCount)
	}

	// Should have warnings about dry run
	foundDryRunWarning := false
	for _, warning := range result.Warnings {
		if warning == "Dry run mode - no actual changes made" {
			foundDryRunWarning = true
			break
		}
	}
	if !foundDryRunWarning {
		t.Error("Expected dry run warning")
	}
}

func TestAnalyzeRollbackImpact_Basic(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration := createTestMigration("001", "1.0.0", true, []string{})
	tracker.migrations["001"] = migration
	tracker.appliedMigrations = []*types.Migration{migration}

	config := &AdvancedRollbackConfig{
		EnableImpactAnalysis: true,
		DefaultDepth:         5,
		MaxDepth:             10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	analysis, err := advManager.AnalyzeRollbackImpact(ctx, []string{"001"})

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if analysis == nil {
		t.Fatal("Expected analysis result")
	}

	if len(analysis.AffectedMigrations) != 1 {
		t.Errorf("Expected 1 affected migration, got %d", len(analysis.AffectedMigrations))
	}

	if analysis.AffectedMigrations[0].Migration.ID != "001" {
		t.Errorf("Expected migration 001, got %s", analysis.AffectedMigrations[0].Migration.ID)
	}
}

func TestAnalyzeRollbackImpact_DataLossRisk(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	// Create migration with DROP TABLE in rollback (data loss risk)
	migration := createTestMigration("001", "1.0.0", true, []string{})
	migration.RollbackSQL = "DROP TABLE users;"

	tracker.migrations["001"] = migration
	tracker.appliedMigrations = []*types.Migration{migration}

	config := &AdvancedRollbackConfig{
		EnableImpactAnalysis: true,
		DefaultDepth:         5,
		MaxDepth:             10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	analysis, err := advManager.AnalyzeRollbackImpact(ctx, []string{"001"})

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !analysis.DataLossRisk {
		t.Error("Expected data loss risk to be detected")
	}

	if analysis.ImpactLevel < constants.ImpactLevelHigh {
		t.Errorf("Expected high impact level for DROP TABLE, got %s", analysis.ImpactLevel)
	}
}

func TestAnalyzeRollbackImpact_NonRollbackable(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration := createTestMigration("001", "1.0.0", false, []string{})
	migration.RollbackSQL = "" // No rollback SQL

	tracker.migrations["001"] = migration
	tracker.appliedMigrations = []*types.Migration{migration}

	config := &AdvancedRollbackConfig{
		EnableImpactAnalysis: true,
		DefaultDepth:         5,
		MaxDepth:             10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	analysis, err := advManager.AnalyzeRollbackImpact(ctx, []string{"001"})

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !analysis.RequiresManual {
		t.Error("Expected requires manual flag for non-rollbackable migration")
	}

	if analysis.CanProceedSafely {
		t.Error("Expected can_proceed_safely=false for non-rollbackable migration")
	}
}

func TestTestRollback_Success(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration := createTestMigration("001", "1.0.0", true, []string{})
	tracker.migrations["001"] = migration
	tracker.appliedMigrations = []*types.Migration{migration}

	config := &AdvancedRollbackConfig{
		EnableRollbackTesting: true,
		TestEnvironments:      []string{"staging", "test"},
		TestTimeout:           1 * time.Minute,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	result, err := advManager.TestRollback(ctx, []string{"001"}, "staging")

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !result.Success {
		t.Error("Expected success=true")
	}

	if result.TotalTests != 1 {
		t.Errorf("Expected 1 test, got %d", result.TotalTests)
	}

	if result.PassedTests != 1 {
		t.Errorf("Expected 1 passed test, got %d", result.PassedTests)
	}

	if result.Coverage != 100.0 {
		t.Errorf("Expected 100%% coverage, got %.2f%%", result.Coverage)
	}
}

func TestTestRollback_InvalidEnvironment(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	config := &AdvancedRollbackConfig{
		EnableRollbackTesting: true,
		TestEnvironments:      []string{"staging", "test"},
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute with invalid environment
	_, err := advManager.TestRollback(ctx, []string{"001"}, "production")

	// Assert
	if err == nil {
		t.Fatal("Expected error for invalid environment")
	}
}

func TestGetRollbackDependencyGraph_Basic(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	migration1 := createTestMigration("001", "1.0.0", true, []string{})
	migration2 := createTestMigration("002", "2.0.0", true, []string{"001"})

	tracker.migrations["001"] = migration1
	tracker.migrations["002"] = migration2
	tracker.appliedMigrations = []*types.Migration{migration1, migration2}

	config := &AdvancedRollbackConfig{
		DefaultDepth: 5,
		MaxDepth:     10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	graph, err := advManager.GetRollbackDependencyGraph(ctx, "001")

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if graph.TotalNodes != 2 {
		t.Errorf("Expected 2 nodes, got %d", graph.TotalNodes)
	}

	if len(graph.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(graph.Edges))
	}

	if graph.HasCycles {
		t.Error("Expected no cycles in simple dependency graph")
	}
}

func TestGetRollbackDependencyGraph_Complex(t *testing.T) {
	ctx := context.Background()

	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	// Create complex dependency structure
	migration1 := createTestMigration("001", "1.0.0", true, []string{})
	migration2 := createTestMigration("002", "2.0.0", true, []string{"001"})
	migration3 := createTestMigration("003", "3.0.0", true, []string{"001"})
	migration4 := createTestMigration("004", "4.0.0", true, []string{"002", "003"})

	tracker.migrations["001"] = migration1
	tracker.migrations["002"] = migration2
	tracker.migrations["003"] = migration3
	tracker.migrations["004"] = migration4
	tracker.appliedMigrations = []*types.Migration{migration1, migration2, migration3, migration4}

	config := &AdvancedRollbackConfig{
		DefaultDepth: 5,
		MaxDepth:     10,
	}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	// Execute
	graph, err := advManager.GetRollbackDependencyGraph(ctx, "001")

	// Assert
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if graph.TotalNodes < 4 {
		t.Errorf("Expected at least 4 nodes, got %d", graph.TotalNodes)
	}

	if graph.MaxDepth < 2 {
		t.Errorf("Expected max depth >= 2, got %d", graph.MaxDepth)
	}
}

func TestSortByReverseDependencyOrder(t *testing.T) {
	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}

	config := &AdvancedRollbackConfig{}
	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	migration1 := createTestMigration("001", "1.0.0", true, []string{})
	migration2 := createTestMigration("002", "2.0.0", true, []string{"001"})
	migration3 := createTestMigration("003", "3.0.0", true, []string{"002"})

	migrations := []*types.Migration{migration1, migration2, migration3}

	// Execute
	sorted := advManager.sortByReverseDependencyOrder(migrations)

	// Assert - for rollback, dependents should come first
	if sorted[0].ID != "003" {
		t.Errorf("Expected first migration to be 003, got %s", sorted[0].ID)
	}

	if sorted[len(sorted)-1].ID != "001" {
		t.Errorf("Expected last migration to be 001, got %s", sorted[len(sorted)-1].ID)
	}
}

func TestAnalyzeMigrationImpact_DropTable(t *testing.T) {
	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}
	config := &AdvancedRollbackConfig{}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	migration := createTestMigration("001", "1.0.0", true, []string{})
	migration.RollbackSQL = "DROP TABLE users;"

	// Execute
	impact := advManager.analyzeMigrationImpact(context.Background(), migration)

	// Assert
	if impact.ImpactLevel != constants.ImpactLevelCritical {
		t.Errorf("Expected critical impact for DROP TABLE, got %s", impact.ImpactLevel)
	}

	foundTableImpact := false
	for _, obj := range impact.AffectedObjects {
		if obj == "tables" {
			foundTableImpact = true
			break
		}
	}
	if !foundTableImpact {
		t.Error("Expected 'tables' in affected objects")
	}
}

func TestCheckDataLossRisk(t *testing.T) {
	// Setup
	tracker := newMockTracker()
	executor := &mockExecutor{}
	baseManager := &mockBaseRollbackManager{}
	config := &AdvancedRollbackConfig{}

	advManager := NewDefaultAdvancedRollbackManager(baseManager, tracker, executor, nil, config)

	tests := []struct {
		name         string
		rollbackSQL  string
		expectedRisk bool
	}{
		{
			name:         "DROP TABLE - has risk",
			rollbackSQL:  "DROP TABLE users;",
			expectedRisk: true,
		},
		{
			name:         "DELETE - has risk",
			rollbackSQL:  "DELETE FROM users WHERE id > 100;",
			expectedRisk: true,
		},
		{
			name:         "TRUNCATE - has risk",
			rollbackSQL:  "TRUNCATE TABLE logs;",
			expectedRisk: true,
		},
		{
			name:         "ALTER TABLE - no risk",
			rollbackSQL:  "ALTER TABLE users ADD COLUMN email VARCHAR(255);",
			expectedRisk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := createTestMigration("001", "1.0.0", true, []string{})
			migration.RollbackSQL = tt.rollbackSQL

			hasRisk := advManager.checkDataLossRisk([]*types.Migration{migration})

			if hasRisk != tt.expectedRisk {
				t.Errorf("Expected data loss risk=%v for %s, got %v",
					tt.expectedRisk, tt.name, hasRisk)
			}
		})
	}
}

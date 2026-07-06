/**
 * @file engine_helpers_test.go
 * @description Tests for engine ordering, cycle detection, and executor wrappers.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/engine/engine_helpers_test.go
 */

package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestPlanExecutionOrder_SortsByVersion(t *testing.T) {
	migrations := []*types.Migration{
		{ID: "c", Version: "003"},
		{ID: "a", Version: "001"},
		{ID: "b", Version: "002"},
	}
	ordered, err := planExecutionOrder(migrations, types.Config{})
	if err != nil {
		t.Fatalf("planExecutionOrder: %v", err)
	}
	got := []string{ordered[0].Version, ordered[1].Version, ordered[2].Version}
	want := []string{"001", "002", "003"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, got)
		}
	}
}

func TestPlanExecutionOrder_DetectsCircularDependency(t *testing.T) {
	// a depends on b, b depends on a -> cycle.
	migrations := []*types.Migration{
		{ID: "a", Version: "001", Dependencies: []string{"b"}},
		{ID: "b", Version: "002", Dependencies: []string{"a"}},
	}
	_, err := planExecutionOrder(migrations, types.Config{})
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
}

func TestPlanExecutionOrder_AcyclicDependenciesOK(t *testing.T) {
	// a -> b -> c is a valid chain.
	migrations := []*types.Migration{
		{ID: "a", Version: "001", Dependencies: []string{"b"}},
		{ID: "b", Version: "002", Dependencies: []string{"c"}},
		{ID: "c", Version: "003"},
	}
	ordered, err := planExecutionOrder(migrations, types.Config{})
	if err != nil {
		t.Fatalf("planExecutionOrder: %v", err)
	}
	if len(ordered) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(ordered))
	}
}

func TestPlanExecutionOrder_ParallelModeStillSorts(t *testing.T) {
	cfg := types.Config{Migration: types.MigrationConfig{ExecutionMode: "parallel"}}
	migrations := []*types.Migration{
		{ID: "b", Version: "002"},
		{ID: "a", Version: "001"},
	}
	ordered, err := planExecutionOrder(migrations, cfg)
	if err != nil {
		t.Fatalf("planExecutionOrder: %v", err)
	}
	if ordered[0].Version != "001" {
		t.Errorf("parallel mode should still sort by version, got %s first", ordered[0].Version)
	}
}

// recordingExecutor is a fake base executor that records which methods were
// invoked and returns configurable errors.
type recordingExecutor struct {
	executeCalled  bool
	rollbackCalled bool
	validateCalled bool
	planCalled     bool
	dryRunCalled   bool
	validateRBCall bool
	executeErr     error
}

func (r *recordingExecutor) Execute(ctx context.Context, m *types.Migration) error {
	r.executeCalled = true
	return r.executeErr
}
func (r *recordingExecutor) ExecuteDryRun(ctx context.Context, m *types.Migration) (*types.ExecutionPlan, error) {
	r.dryRunCalled = true
	return &types.ExecutionPlan{TotalCount: 1}, nil
}
func (r *recordingExecutor) Rollback(ctx context.Context, m *types.Migration) error {
	r.rollbackCalled = true
	return nil
}
func (r *recordingExecutor) ValidateRollback(ctx context.Context, m *types.Migration) error {
	r.validateRBCall = true
	return nil
}
func (r *recordingExecutor) ValidateMigration(ctx context.Context, m *types.Migration) error {
	r.validateCalled = true
	return nil
}
func (r *recordingExecutor) GetExecutionPlan(ms []*types.Migration) ([]string, error) {
	r.planCalled = true
	return []string{"plan"}, nil
}

// recordingMonitor records start/end calls made by the monitoring wrapper.
type recordingMonitor struct {
	starts int
	ends   int
	lastID string
	lastOK bool
}

func (m *recordingMonitor) RecordMigrationStart(ctx context.Context, id string) error {
	m.starts++
	m.lastID = id
	return nil
}
func (m *recordingMonitor) RecordMigrationEnd(ctx context.Context, id string, success bool, duration int64) error {
	m.ends++
	m.lastOK = success
	return nil
}
func (m *recordingMonitor) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func TestWrapWithMonitoring_RecordsStartAndEnd(t *testing.T) {
	ctx := context.Background()
	base := &recordingExecutor{}
	mon := &recordingMonitor{}
	wrapped := wrapWithMonitoring(base, mon)

	mig := &types.Migration{ID: "mig-1"}
	if err := wrapped.Execute(ctx, mig); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !base.executeCalled {
		t.Error("wrapper must delegate Execute to the base executor")
	}
	if mon.starts != 1 {
		t.Errorf("expected 1 RecordMigrationStart, got %d", mon.starts)
	}
	if mon.ends != 1 {
		t.Errorf("expected 1 RecordMigrationEnd, got %d", mon.ends)
	}
	if mon.lastID != "mig-1" {
		t.Errorf("monitor recorded wrong migration id: %s", mon.lastID)
	}
}

func TestWrapWithMonitoring_PropagatesExecuteError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("exec failed")
	base := &recordingExecutor{executeErr: boom}
	wrapped := wrapWithMonitoring(base, &recordingMonitor{})

	if err := wrapped.Execute(ctx, &types.Migration{ID: "x"}); !errors.Is(err, boom) {
		t.Fatalf("expected base execute error to propagate, got %v", err)
	}
}

func TestWrapWithMonitoring_NilMonitorSkipsRecording(t *testing.T) {
	ctx := context.Background()
	base := &recordingExecutor{}
	// A nil monitor must not panic and must still execute the migration.
	wrapped := wrapWithMonitoring(base, nil)
	if err := wrapped.Execute(ctx, &types.Migration{ID: "y"}); err != nil {
		t.Fatalf("Execute with nil monitor: %v", err)
	}
	if !base.executeCalled {
		t.Error("Execute should still delegate to base when monitor is nil")
	}
}

func TestWrapWithMonitoring_DelegatesAllMethods(t *testing.T) {
	ctx := context.Background()
	base := &recordingExecutor{}
	wrapped := wrapWithMonitoring(base, &recordingMonitor{})
	mig := &types.Migration{ID: "z"}

	if _, err := wrapped.ExecuteDryRun(ctx, mig); err != nil {
		t.Fatalf("ExecuteDryRun: %v", err)
	}
	if err := wrapped.Rollback(ctx, mig); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := wrapped.ValidateRollback(ctx, mig); err != nil {
		t.Fatalf("ValidateRollback: %v", err)
	}
	if err := wrapped.ValidateMigration(ctx, mig); err != nil {
		t.Fatalf("ValidateMigration: %v", err)
	}
	if _, err := wrapped.GetExecutionPlan([]*types.Migration{mig}); err != nil {
		t.Fatalf("GetExecutionPlan: %v", err)
	}

	if !base.dryRunCalled || !base.rollbackCalled || !base.validateRBCall ||
		!base.validateCalled || !base.planCalled {
		t.Error("monitoring wrapper failed to delegate one or more methods to the base executor")
	}
}

func TestWrapInTransactionAndDryRun_AreIdentityPassthroughs(t *testing.T) {
	base := &recordingExecutor{}
	if got := wrapInTransaction(base, nil); got != types.MigrationExecutor(base) {
		t.Error("wrapInTransaction should return the same executor (placeholder passthrough)")
	}
	if got := wrapInDryRun(base); got != types.MigrationExecutor(base) {
		t.Error("wrapInDryRun should return the same executor (placeholder passthrough)")
	}
}

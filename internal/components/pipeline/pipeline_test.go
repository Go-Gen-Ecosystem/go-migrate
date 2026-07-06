/**
 * @file pipeline_test.go
 * @description Behaviour tests for the composable migration pipeline: Step,
 *              StepFunc, Pipeline sequencing/fail-fast, the conditional
 *              PipelineBuilder assembly and the state-based migration stage
 *              short-circuits.
 * @author Admilson B. F. Cossa
 */

package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/state"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Step / StepFunc / Pipeline -----------------------------------------------------

func TestStepFunc_NameAndRun(t *testing.T) {
	ran := false
	step := NewStepFunc("greet", func(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error {
		ran = true
		return nil
	})
	assert.Equal(t, "greet", step.Name())
	require.NoError(t, step.Run(context.Background(), nil, &PipelineContext{}))
	assert.True(t, ran)
}

func TestPipeline_Run_ExecutesStepsInOrder(t *testing.T) {
	var order []string
	p := NewPipeline(
		NewStepFunc("first", func(_ context.Context, _ *engine.MigrationEngine, pctx *PipelineContext) error {
			order = append(order, "first")
			pctx.Target = "v1"
			return nil
		}),
		NewStepFunc("second", func(_ context.Context, _ *engine.MigrationEngine, pctx *PipelineContext) error {
			order = append(order, "second")
			// Second step observes state set by the first.
			assert.Equal(t, "v1", pctx.Target)
			return nil
		}),
	)

	pctx := &PipelineContext{}
	require.NoError(t, p.Run(context.Background(), nil, pctx))
	assert.Equal(t, []string{"first", "second"}, order)
}

func TestPipeline_Run_FailsFast(t *testing.T) {
	var order []string
	p := NewPipeline(
		NewStepFunc("ok", func(_ context.Context, _ *engine.MigrationEngine, _ *PipelineContext) error {
			order = append(order, "ok")
			return nil
		}),
		NewStepFunc("boom", func(_ context.Context, _ *engine.MigrationEngine, _ *PipelineContext) error {
			order = append(order, "boom")
			return errors.New("failed")
		}),
		NewStepFunc("never", func(_ context.Context, _ *engine.MigrationEngine, _ *PipelineContext) error {
			order = append(order, "never")
			return nil
		}),
	)

	err := p.Run(context.Background(), nil, &PipelineContext{})
	require.Error(t, err)
	// The third step must not have run after the second failed.
	assert.Equal(t, []string{"ok", "boom"}, order)
}

func TestPipeline_Add_AppendsSteps(t *testing.T) {
	count := 0
	inc := func(_ context.Context, _ *engine.MigrationEngine, _ *PipelineContext) error {
		count++
		return nil
	}
	p := NewPipeline(NewStepFunc("a", inc))
	p.Add(NewStepFunc("b", inc), NewStepFunc("c", inc))

	require.NoError(t, p.Run(context.Background(), nil, &PipelineContext{}))
	assert.Equal(t, 3, count)
}

func TestPipeline_Run_Empty(t *testing.T) {
	// An empty pipeline succeeds trivially.
	require.NoError(t, NewPipeline().Run(context.Background(), nil, &PipelineContext{}))
}

// --- PipelineBuilder (conditional assembly of engine.Pipeline) ----------------------

func TestPipelineBuilder_BuildMigrationPipeline_IncludesLockSteps(t *testing.T) {
	cfg := types.DefaultMigrationConfig()
	// Not a dry run: lock steps are included.
	p := NewPipelineBuilder(cfg, "sqlite").WithDryRun(false).BuildMigrationPipeline()
	require.NotNil(t, p)
	// The assembled (no-op) engine pipeline runs cleanly.
	require.NoError(t, p.Run(context.Background(), &engine.MigrationEngine{}, &engine.PipelineContext{}))
}

func TestPipelineBuilder_BuildMigrationPipeline_DryRunSkipsLock(t *testing.T) {
	cfg := types.DefaultMigrationConfig()
	p := NewPipelineBuilder(cfg, "sqlite").WithDryRun(true).WithEnableDryRun(true).BuildMigrationPipeline()
	require.NotNil(t, p)
	require.NoError(t, p.Run(context.Background(), &engine.MigrationEngine{}, &engine.PipelineContext{}))
}

func TestPipelineBuilder_BuildRollbackPipeline(t *testing.T) {
	cfg := types.DefaultMigrationConfig()
	p := NewPipelineBuilder(cfg, "sqlite").BuildRollbackPipeline()
	require.NotNil(t, p)
	require.NoError(t, p.Run(context.Background(), &engine.MigrationEngine{}, &engine.PipelineContext{}))
}

func TestPipelineBuilder_AddStepAndConditional(t *testing.T) {
	cfg := types.DefaultMigrationConfig()
	b := NewPipelineBuilder(cfg, "sqlite")

	called := 0
	b.AddStep(engine.NewStepFunc("extra", func(_ context.Context, _ *engine.MigrationEngine, _ *engine.PipelineContext) error {
		called++
		return nil
	}))
	// Conditional step added only when the condition is true.
	b.AddConditionalStep(true, engine.NewStepFunc("cond-yes", func(_ context.Context, _ *engine.MigrationEngine, _ *engine.PipelineContext) error {
		called++
		return nil
	}))
	b.AddConditionalStep(false, engine.NewStepFunc("cond-no", func(_ context.Context, _ *engine.MigrationEngine, _ *engine.PipelineContext) error {
		called += 100
		return nil
	}))

	p := b.BuildMigrationPipeline()
	require.NoError(t, p.Run(context.Background(), &engine.MigrationEngine{}, &engine.PipelineContext{}))
	// The two manually added steps ran; the false-conditional step did not.
	assert.Equal(t, 2, called)
}

// --- StateBasedMigrationStage -------------------------------------------------------

func TestStateBasedStage_Name(t *testing.T) {
	stage := NewStateBasedMigrationStage(&state.StateBasedConfig{}, nil)
	assert.Equal(t, "state-based-migration", stage.Name())
}

func TestStateBasedStage_Disabled_NoOp(t *testing.T) {
	// Disabled config: the stage returns nil without touching the migrator
	// (nil migrator is safe because it is never dereferenced).
	stage := NewStateBasedMigrationStage(&state.StateBasedConfig{Enabled: false, SchemaPath: "schema.yaml"}, nil)
	pctx := &PipelineContext{}
	require.NoError(t, stage.Run(context.Background(), nil, pctx))
	assert.Empty(t, pctx.Migrations)
	assert.Empty(t, pctx.Pending)
}

func TestStateBasedStage_EnabledButNoSchemaPath_NoOp(t *testing.T) {
	stage := NewStateBasedMigrationStage(&state.StateBasedConfig{Enabled: true, SchemaPath: ""}, nil)
	pctx := &PipelineContext{}
	require.NoError(t, stage.Run(context.Background(), nil, pctx))
	assert.Empty(t, pctx.Migrations)
}

// --- PreviewStateBasedChanges -------------------------------------------------------

func TestPreviewStateBasedChanges_Disabled(t *testing.T) {
	out, err := PreviewStateBasedChanges(context.Background(),
		&state.StateBasedConfig{Enabled: false, SchemaPath: "schema.yaml"}, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "not enabled")
}

func TestPreviewStateBasedChanges_NoSchemaPath(t *testing.T) {
	out, err := PreviewStateBasedChanges(context.Background(),
		&state.StateBasedConfig{Enabled: true, SchemaPath: ""}, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "not enabled")
}

// --- fake migrator driving the stage's Plan/Preview branches -----------------------

// fakeMigrator implements state.StateBasedMigrator; only Plan and Preview are
// exercised by the stage, so the rest are left to the embedded (nil) interface.
type fakeMigrator struct {
	state.StateBasedMigrator
	plan       *state.GeneratedMigration
	planErr    error
	preview    *state.SchemaDiff
	previewErr error
}

func (m *fakeMigrator) Plan(context.Context, string) (*state.GeneratedMigration, error) {
	return m.plan, m.planErr
}
func (m *fakeMigrator) Preview(context.Context, string) (*state.SchemaDiff, error) {
	return m.preview, m.previewErr
}

func TestStateBasedStage_Run_NoChanges(t *testing.T) {
	// Plan returns nil migration -> no changes -> no-op.
	stage := NewStateBasedMigrationStage(
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml"},
		&fakeMigrator{plan: nil},
	)
	pctx := &PipelineContext{}
	require.NoError(t, stage.Run(context.Background(), nil, pctx))
	assert.Empty(t, pctx.Migrations)
	assert.Empty(t, pctx.Pending)
}

func TestStateBasedStage_Run_PlanError(t *testing.T) {
	stage := NewStateBasedMigrationStage(
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml"},
		&fakeMigrator{planErr: errors.New("plan boom")},
	)
	pctx := &PipelineContext{}
	err := stage.Run(context.Background(), nil, pctx)
	require.Error(t, err)
	assert.True(t, pctx.HadError)
	require.NotEmpty(t, pctx.ValidationErrors)
}

func TestStateBasedStage_Run_AutoApplySafe(t *testing.T) {
	stage := NewStateBasedMigrationStage(
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml", AutoApply: true},
		&fakeMigrator{plan: &state.GeneratedMigration{
			ID: "gm1", Version: "1", UpSQL: "CREATE TABLE t (id INT);",
			DownSQL: "DROP TABLE t;", SafeToAutoApply: true,
		}},
	)
	pctx := &PipelineContext{}
	require.NoError(t, stage.Run(context.Background(), nil, pctx))
	// Safe + auto-apply -> queued for execution.
	require.Len(t, pctx.Migrations, 1)
	assert.Equal(t, "gm1", pctx.Migrations[0].ID)
	assert.Equal(t, "CREATE TABLE t (id INT);", pctx.Migrations[0].Script)
	assert.Empty(t, pctx.Pending)
}

func TestStateBasedStage_Run_DestructivePending(t *testing.T) {
	// Not safe to auto-apply and review not required -> goes to Pending with a warning.
	stage := NewStateBasedMigrationStage(
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml", AutoApply: true, RequireReview: false},
		&fakeMigrator{plan: &state.GeneratedMigration{
			ID: "gm2", Version: "2", UpSQL: "DROP TABLE users;", SafeToAutoApply: false,
		}},
	)
	pctx := &PipelineContext{}
	require.NoError(t, stage.Run(context.Background(), nil, pctx))
	require.Len(t, pctx.Pending, 1)
	assert.Empty(t, pctx.Migrations)
	require.NotEmpty(t, pctx.ValidationErrors)
	assert.Contains(t, pctx.ValidationErrors[0], "destructive")
}

func TestStateBasedStage_Run_RequireReviewBlocks(t *testing.T) {
	stage := NewStateBasedMigrationStage(
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml", AutoApply: false, RequireReview: true},
		&fakeMigrator{plan: &state.GeneratedMigration{
			ID: "gm3", Version: "3", UpSQL: "ALTER TABLE t ADD COLUMN c INT;", SafeToAutoApply: true,
		}},
	)
	pctx := &PipelineContext{}
	err := stage.Run(context.Background(), nil, pctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manual review")
	require.Len(t, pctx.Pending, 1)
}

func TestPreviewStateBasedChanges_InSync(t *testing.T) {
	out, err := PreviewStateBasedChanges(context.Background(),
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml"},
		&fakeMigrator{preview: &state.SchemaDiff{HasChanges: false}})
	require.NoError(t, err)
	assert.Contains(t, out, "in sync")
}

func TestPreviewStateBasedChanges_WithChanges(t *testing.T) {
	diff := &state.SchemaDiff{
		HasChanges:    true,
		TablesAdded:   []state.Table{{Name: "new_table"}},
		TablesDropped: []state.Table{{Name: "old_table"}},
		TablesModified: []state.TableDiff{{
			TableName:      "users",
			ColumnsAdded:   []state.Column{{Name: "age"}},
			ColumnsDropped: []state.Column{{Name: "legacy"}},
		}},
		Summary: state.DiffSummary{TotalChanges: 4, DestructiveChanges: 2, SafeToApply: false},
	}
	out, err := PreviewStateBasedChanges(context.Background(),
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml"},
		&fakeMigrator{preview: diff})
	require.NoError(t, err)
	assert.Contains(t, out, "new_table")
	assert.Contains(t, out, "old_table")
	assert.Contains(t, out, "DATA LOSS")
	assert.Contains(t, out, "users")
	assert.Contains(t, out, "Total Changes: 4")
}

func TestPreviewStateBasedChanges_PreviewError(t *testing.T) {
	_, err := PreviewStateBasedChanges(context.Background(),
		&state.StateBasedConfig{Enabled: true, SchemaPath: "schema.yaml"},
		&fakeMigrator{previewErr: errors.New("preview boom")})
	require.Error(t, err)
}

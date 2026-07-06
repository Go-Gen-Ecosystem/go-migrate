/**
 * @file pipeline_builder_test.go
 * @description Behaviour tests for the migration processing PipelineBuilder and
 *              the ConcretePipelineProcessor, driving real stages end-to-end.
 * @author Admilson B. F. Cossa
 */

package builder

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingStage is a real PipelineStage that records invocation and can fail.
type recordingStage struct {
	name    string
	calls   *int
	failErr error
}

func (s *recordingStage) GetName() string { return s.name }
func (s *recordingStage) Process(_ context.Context, migs []types.MigrationScript) ([]types.MigrationScript, error) {
	*s.calls++
	if s.failErr != nil {
		return nil, s.failErr
	}
	return migs, nil
}
func (s *recordingStage) CanParallel() bool         { return false }
func (s *recordingStage) GetDependencies() []string { return nil }

func sampleScripts() []types.MigrationScript {
	return []types.MigrationScript{
		{ID: "b", Version: "2", Type: types.MigrationTypeSQL, Content: "CREATE TABLE b (id INT);"},
		{ID: "a", Version: "1", Type: types.MigrationTypeSQL, Content: "CREATE TABLE a (id INT);"},
	}
}

func TestPipelineBuilder_Build_NoStages(t *testing.T) {
	_, err := NewPipelineBuilder(types.Config{}).Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one stage")
}

func TestPipelineBuilder_Process_RunsStagesInOrder(t *testing.T) {
	calls := 0
	proc, err := NewPipelineBuilder(types.Config{}).
		AddStage(&recordingStage{name: "s1", calls: &calls}).
		AddStage(&recordingStage{name: "s2", calls: &calls}).
		WithTimeout(5 * time.Second).
		Build()
	require.NoError(t, err)

	scripts := sampleScripts()
	out, err := proc.Process(context.Background(), PipelineInput{Migrations: scripts})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "both stages must run")
	assert.Len(t, out.Migrations, len(scripts))
	assert.Empty(t, out.Errors)

	// Metrics reflect both stages.
	m := proc.GetMetrics()
	assert.Len(t, m.StageMetrics, 2)
	assert.Equal(t, len(scripts), m.TotalMigrations)
}

func TestPipelineBuilder_Process_StageErrorSurfaces(t *testing.T) {
	calls := 0
	proc, err := NewPipelineBuilder(types.Config{}).
		AddStage(&recordingStage{name: "boom", calls: &calls, failErr: errors.New("stage failed")}).
		WithRetryPolicy(RetryPolicy{MaxRetries: 0}).
		Build()
	require.NoError(t, err)

	out, err := proc.Process(context.Background(), PipelineInput{Migrations: sampleScripts()})
	require.Error(t, err)
	assert.NotEmpty(t, out.Errors)
	assert.GreaterOrEqual(t, calls, 1)
}

func TestPipelineBuilder_ValidationStage_RealValidator(t *testing.T) {
	stage := NewValidationStage(fakeValidator{})
	out, err := stage.Process(context.Background(), sampleScripts())
	require.NoError(t, err)
	assert.Len(t, out, 2)
	assert.True(t, stage.CanParallel())
	assert.Equal(t, "validation", stage.GetName())
}

func TestPipelineBuilder_ValidationStage_RejectsInvalid(t *testing.T) {
	stage := NewValidationStage(rejectingValidator{})
	_, err := stage.Process(context.Background(), sampleScripts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestPipelineBuilder_DependencyResolutionStage_Orders(t *testing.T) {
	stage := NewDependencyResolutionStage(sortingResolver{})
	out, err := stage.Process(context.Background(), sampleScripts())
	require.NoError(t, err)
	require.Len(t, out, 2)
	// Resolver sorts ascending by version: "1" then "2".
	assert.Equal(t, "1", out[0].Version)
	assert.Equal(t, "2", out[1].Version)
	assert.False(t, stage.CanParallel())
	assert.Equal(t, []string{"validation"}, stage.GetDependencies())
}

func TestConcretePipelineProcessor_GetStages(t *testing.T) {
	proc, err := NewPipelineBuilder(types.Config{}).
		AddStage(NewValidationStage(fakeValidator{})).
		Build()
	require.NoError(t, err)
	assert.Len(t, proc.GetStages(), 1)
}

// rejectingValidator always fails validation.
type rejectingValidator struct{}

func (rejectingValidator) ValidateMigrations([]types.Migration) error        { return errors.New("nope") }
func (rejectingValidator) ValidateMigration(*types.Migration) error          { return errors.New("nope") }
func (rejectingValidator) CheckDependencies([]types.Migration) error         { return nil }
func (rejectingValidator) CheckCircularDependencies([]types.Migration) error { return nil }

// sortingResolver returns migrations ordered ascending by version.
type sortingResolver struct{}

func (sortingResolver) ResolveDependencies(_ context.Context, migs []*types.Migration) ([]*types.Migration, error) {
	out := make([]*types.Migration, len(migs))
	copy(out, migs)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Version < out[i].Version {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

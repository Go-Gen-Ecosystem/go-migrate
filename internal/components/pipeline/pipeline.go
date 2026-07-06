/**
 * @file pipeline.go
 * @description Composable pipeline and steps for migration orchestration to avoid excessive if-else
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/pipeline/pipeline.go
 */

package pipeline

import (
    "context"

    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/lock"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// Step represents a unit of work in the pipeline
type Step interface {
    Name() string
    Run(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error
}

// StepFunc is a functional adapter for Step
type StepFunc struct {
    name string
    fn   func(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error
}

// NewStepFunc creates a new StepFunc with the given name and function
func NewStepFunc(name string, fn func(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error) StepFunc {
    return StepFunc{
        name: name,
        fn:   fn,
    }
}

func (s StepFunc) Name() string { return s.name }
func (s StepFunc) Run(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error { return s.fn(ctx, eng, pctx) }

// Pipeline composes steps and runs them sequentially
type Pipeline struct {
    steps []Step
}

// NewPipeline creates a new pipeline
func NewPipeline(steps ...Step) *Pipeline {
    p := &Pipeline{steps: make([]Step, 0)}
    return p.Add(steps...)
}

// Add appends steps to the pipeline
func (p *Pipeline) Add(steps ...Step) *Pipeline {
    p.steps = append(p.steps, steps...)
    return p
}

// Run executes all steps sequentially, failing fast on error
func (p *Pipeline) Run(ctx context.Context, eng *engine.MigrationEngine, pctx *PipelineContext) error {
    for _, step := range p.steps {
        if err := step.Run(ctx, eng, pctx); err != nil {
            return err
        }
    }
    return nil
}

// PipelineContext holds ephemeral state shared across steps
type PipelineContext struct {
    Target     string
    DryRun     bool
    Lock       *lock.MigrationLock
    Migrations []*types.Migration
    Pending    []*types.Migration
    Planned    []*types.Migration
    
    // Execution results
    ExecutedMigrations []*types.Migration
    ExecutionPlans     []*types.ExecutionPlan
    
    // Error tracking
    HadError         bool
    ValidationErrors []string
    ExecutionErrors  []string
    
    // Optional integration state
    BackupName string
    HealthPre  map[string]interface{}
    HealthPost map[string]interface{}
}

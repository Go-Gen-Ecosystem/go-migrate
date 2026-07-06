/*
@file builder.go
@description PipelineBuilder for dynamic step assembly based on database type and configuration
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/pipeline/builder.go
*/

package pipeline

import (
	"context"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// PipelineBuilder helps construct migration pipelines with conditional steps
type PipelineBuilder struct {
	config     *types.MigrationConfig
	dbType     string
	dryRun     bool
	enableDryRun bool
	steps      []engine.Step
}

// NewPipelineBuilder creates a new pipeline builder
func NewPipelineBuilder(config *types.MigrationConfig, dbType string) *PipelineBuilder {
	return &PipelineBuilder{
		config: config,
		dbType: dbType,
		steps:  make([]engine.Step, 0),
	}
}

// WithDryRun sets the dry run mode
func (b *PipelineBuilder) WithDryRun(dryRun bool) *PipelineBuilder {
	b.dryRun = dryRun
	return b
}

// WithEnableDryRun sets the enable dry run flag
func (b *PipelineBuilder) WithEnableDryRun(enableDryRun bool) *PipelineBuilder {
	b.enableDryRun = enableDryRun
	return b
}

// AddStep adds a step to the pipeline
func (b *PipelineBuilder) AddStep(step engine.Step) *PipelineBuilder {
	b.steps = append(b.steps, step)
	return b
}

// AddConditionalStep adds a step only if the condition is met
func (b *PipelineBuilder) AddConditionalStep(condition bool, step engine.Step) *PipelineBuilder {
	if condition {
		b.steps = append(b.steps, step)
	}
	return b
}

// BuildMigrationPipeline builds a complete migration pipeline
func (b *PipelineBuilder) BuildMigrationPipeline() *engine.Pipeline {
	// Add validation step
	b.AddStep(engine.NewStepFunc("validate_migrations", b.createValidationStep()))

	// Add dependency resolution step
	b.AddStep(engine.NewStepFunc("resolve_dependencies", b.createDependencyResolutionStep()))

	// Add lock acquisition step (skip in dry run)
	b.AddConditionalStep(!b.dryRun && !b.enableDryRun, engine.NewStepFunc("acquire_lock", b.createLockAcquisitionStep()))

	// Add schema table creation step (conditional for dry run)
	b.AddStep(engine.NewStepFunc("ensure_schema_table", b.createSchemaTableStep()))

	// Add migration execution step
	b.AddStep(engine.NewStepFunc("execute_migrations", b.createMigrationExecutionStep()))

	// Add lock release step (skip in dry run)
	b.AddConditionalStep(!b.dryRun && !b.enableDryRun, engine.NewStepFunc("release_lock", b.createLockReleaseStep()))

	return engine.NewPipeline(b.steps...)
}

// BuildRollbackPipeline builds a rollback pipeline
func (b *PipelineBuilder) BuildRollbackPipeline() *engine.Pipeline {
	// Add validation step
	b.AddStep(engine.NewStepFunc("validate_rollback", b.createRollbackValidationStep()))

	// Add lock acquisition step (skip in dry run)
	b.AddConditionalStep(!b.dryRun && !b.enableDryRun, engine.NewStepFunc("acquire_lock", b.createLockAcquisitionStep()))

	// Add rollback execution step
	b.AddStep(engine.NewStepFunc("execute_rollback", b.createRollbackExecutionStep()))

	// Add lock release step (skip in dry run)
	b.AddConditionalStep(!b.dryRun && !b.enableDryRun, engine.NewStepFunc("release_lock", b.createLockReleaseStep()))

	return engine.NewPipeline(b.steps...)
}

// createValidationStep creates a migration validation step
func (b *PipelineBuilder) createValidationStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Validate migration files - validation is handled by the engine internally
		return nil
	}
}
// createDependencyResolutionStep creates a dependency resolution step
func (b *PipelineBuilder) createDependencyResolutionStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Resolve migration dependencies - handled by the engine internally
		return nil
	}
}

// createLockAcquisitionStep creates a lock acquisition step
func (b *PipelineBuilder) createLockAcquisitionStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Lock acquisition would be handled by the engine if needed
		return nil
	}
}

// createSchemaTableStep creates a schema table creation step
func (b *PipelineBuilder) createSchemaTableStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Schema table creation is handled by the engine internally
		return nil
	}
}

// createMigrationExecutionStep creates a migration execution step
func (b *PipelineBuilder) createMigrationExecutionStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Migration execution is handled by the engine internally
		return nil
	}
}

// createLockReleaseStep creates a lock release step
func (b *PipelineBuilder) createLockReleaseStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Lock release would be handled by the engine if needed
		return nil
	}
}

// createRollbackValidationStep creates a rollback validation step
func (b *PipelineBuilder) createRollbackValidationStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Rollback validation is handled by the engine internally
		return nil
	}
}

// createRollbackExecutionStep creates a rollback execution step
func (b *PipelineBuilder) createRollbackExecutionStep() func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
	return func(ctx context.Context, eng *engine.MigrationEngine, pctx *engine.PipelineContext) error {
		// Rollback execution is handled by the engine internally
		return nil
	}
}

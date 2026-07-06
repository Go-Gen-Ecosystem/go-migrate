/**
 * @file pipeline_builder.go
 * @description PipelineBuilder for constructing migration pipelines with stages and processors
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/builder/pipeline_builder.go
 */

package builder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// PipelineStage represents a stage in the migration pipeline
type PipelineStage interface {
	GetName() string
	Process(ctx context.Context, migrations []types.MigrationScript) ([]types.MigrationScript, error)
	CanParallel() bool
	GetDependencies() []string
}

// PipelineProcessor defines the interface for processing migrations in a pipeline
type PipelineProcessor interface {
	Process(ctx context.Context, input PipelineInput) (PipelineOutput, error)
	GetStages() []PipelineStage
	GetMetrics() PipelineMetrics
}

// PipelineInput represents input to a pipeline stage
type PipelineInput struct {
	Migrations []types.MigrationScript `json:"migrations"`
	Context    map[string]interface{}  `json:"context"`
	Config     types.Config            `json:"config"`
	Metadata   map[string]string       `json:"metadata"`
}

// PipelineOutput represents output from a pipeline stage
type PipelineOutput struct {
	Migrations []types.MigrationScript `json:"migrations"`
	Context    map[string]interface{}  `json:"context"`
	Errors     []string                `json:"errors"`
	Warnings   []string                `json:"warnings"`
	Metadata   map[string]string       `json:"metadata"`
	Metrics    StageMetrics            `json:"metrics"`
}

// StageMetrics represents metrics for a pipeline stage
type StageMetrics struct {
	StageName     string        `json:"stage_name"`
	Duration      time.Duration `json:"duration"`
	InputCount    int           `json:"input_count"`
	OutputCount   int           `json:"output_count"`
	ErrorCount    int           `json:"error_count"`
	WarningCount  int           `json:"warning_count"`
	ProcessedAt   time.Time     `json:"processed_at"`
}

// PipelineMetrics represents overall pipeline metrics
type PipelineMetrics struct {
	TotalDuration    time.Duration  `json:"total_duration"`
	StageMetrics     []StageMetrics `json:"stage_metrics"`
	TotalMigrations  int            `json:"total_migrations"`
	ProcessedCount   int            `json:"processed_count"`
	ErrorCount       int            `json:"error_count"`
	WarningCount     int            `json:"warning_count"`
	StartTime        time.Time      `json:"start_time"`
	EndTime          time.Time      `json:"end_time"`
}

// PipelineBuilder builds migration processing pipelines
type PipelineBuilder struct {
	stages      []PipelineStage
	config      types.Config
	parallel    bool
	maxWorkers  int
	timeout     time.Duration
	retryPolicy RetryPolicy
	middleware  []PipelineMiddleware
	options     map[string]interface{}
}

// RetryPolicy defines retry behavior for pipeline stages
type RetryPolicy struct {
	MaxRetries    int           `json:"max_retries"`
	RetryDelay    time.Duration `json:"retry_delay"`
	BackoffFactor float64       `json:"backoff_factor"`
	MaxDelay      time.Duration `json:"max_delay"`
}

// PipelineMiddleware defines middleware for pipeline processing
type PipelineMiddleware interface {
	Before(ctx context.Context, input PipelineInput) (PipelineInput, error)
	After(ctx context.Context, output PipelineOutput) (PipelineOutput, error)
	OnError(ctx context.Context, err error, input PipelineInput) error
}

// NewPipelineBuilder creates a new pipeline builder
func NewPipelineBuilder(config types.Config) *PipelineBuilder {
	return &PipelineBuilder{
		stages:     make([]PipelineStage, 0),
		config:     config,
		parallel:   false,
		maxWorkers: 4,
		timeout:    30 * time.Minute,
		retryPolicy: RetryPolicy{
			MaxRetries:    3,
			RetryDelay:    1 * time.Second,
			BackoffFactor: 2.0,
			MaxDelay:      30 * time.Second,
		},
		middleware: make([]PipelineMiddleware, 0),
		options:    make(map[string]interface{}),
	}
}

// AddStage adds a stage to the pipeline
func (pb *PipelineBuilder) AddStage(stage PipelineStage) *PipelineBuilder {
	pb.stages = append(pb.stages, stage)
	return pb
}

// WithParallel enables parallel processing
func (pb *PipelineBuilder) WithParallel(parallel bool) *PipelineBuilder {
	pb.parallel = parallel
	return pb
}

// WithMaxWorkers sets the maximum number of workers for parallel processing
func (pb *PipelineBuilder) WithMaxWorkers(maxWorkers int) *PipelineBuilder {
	pb.maxWorkers = maxWorkers
	return pb
}

// WithTimeout sets the pipeline timeout
func (pb *PipelineBuilder) WithTimeout(timeout time.Duration) *PipelineBuilder {
	pb.timeout = timeout
	return pb
}

// WithRetryPolicy sets the retry policy
func (pb *PipelineBuilder) WithRetryPolicy(policy RetryPolicy) *PipelineBuilder {
	pb.retryPolicy = policy
	return pb
}

// AddMiddleware adds middleware to the pipeline
func (pb *PipelineBuilder) AddMiddleware(middleware PipelineMiddleware) *PipelineBuilder {
	pb.middleware = append(pb.middleware, middleware)
	return pb
}

// WithOption sets a custom option
func (pb *PipelineBuilder) WithOption(key string, value interface{}) *PipelineBuilder {
	pb.options[key] = value
	return pb
}

// Build creates the pipeline processor
func (pb *PipelineBuilder) Build() (PipelineProcessor, error) {
	if len(pb.stages) == 0 {
		return nil, fmt.Errorf("pipeline must have at least one stage")
	}

	processor := &ConcretePipelineProcessor{
		stages:      pb.stages,
		config:      pb.config,
		parallel:    pb.parallel,
		maxWorkers:  pb.maxWorkers,
		timeout:     pb.timeout,
		retryPolicy: pb.retryPolicy,
		middleware:  pb.middleware,
		options:     pb.options,
		metrics:     PipelineMetrics{},
	}

	return processor, nil
}

// ConcretePipelineProcessor implements PipelineProcessor
type ConcretePipelineProcessor struct {
	stages      []PipelineStage
	config      types.Config
	parallel    bool
	maxWorkers  int
	timeout     time.Duration
	retryPolicy RetryPolicy
	middleware  []PipelineMiddleware
	options     map[string]interface{}
	metrics     PipelineMetrics
	mutex       sync.RWMutex
}

// Process executes the pipeline
func (cpp *ConcretePipelineProcessor) Process(ctx context.Context, input PipelineInput) (PipelineOutput, error) {
	startTime := time.Now()
	
	cpp.mutex.Lock()
	cpp.metrics = PipelineMetrics{
		StartTime:       startTime,
		StageMetrics:    make([]StageMetrics, 0),
		TotalMigrations: len(input.Migrations),
	}
	cpp.mutex.Unlock()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, cpp.timeout)
	defer cancel()

	// Initialize output
	output := PipelineOutput{
		Migrations: input.Migrations,
		Context:    input.Context,
		Errors:     make([]string, 0),
		Warnings:   make([]string, 0),
		Metadata:   input.Metadata,
	}

	// Process stages
	for _, stage := range cpp.stages {
		stageOutput, err := cpp.processStage(ctx, stage, PipelineInput{
			Migrations: output.Migrations,
			Context:    output.Context,
			Config:     input.Config,
			Metadata:   output.Metadata,
		})

		if err != nil {
			output.Errors = append(output.Errors, fmt.Sprintf("stage %s failed: %v", stage.GetName(), err))
			cpp.updateMetrics(startTime, output)
			return output, err
		}

		// Update output with stage results
		output.Migrations = stageOutput.Migrations
		output.Context = stageOutput.Context
		output.Errors = append(output.Errors, stageOutput.Errors...)
		output.Warnings = append(output.Warnings, stageOutput.Warnings...)
		
		// Merge metadata
		for k, v := range stageOutput.Metadata {
			output.Metadata[k] = v
		}

		// Record stage metrics
		cpp.mutex.Lock()
		cpp.metrics.StageMetrics = append(cpp.metrics.StageMetrics, stageOutput.Metrics)
		cpp.mutex.Unlock()
	}

	cpp.updateMetrics(startTime, output)
	return output, nil
}

// processStage processes a single stage with retry logic
func (cpp *ConcretePipelineProcessor) processStage(ctx context.Context, stage PipelineStage, input PipelineInput) (PipelineOutput, error) {
	stageStartTime := time.Now()
	
	// Apply middleware before
	for _, middleware := range cpp.middleware {
		modifiedInput, err := middleware.Before(ctx, input)
		if err != nil {
			return PipelineOutput{}, fmt.Errorf("middleware before failed: %w", err)
		}
		input = modifiedInput
	}

	var output PipelineOutput
	var err error

	// Retry logic
	for attempt := 0; attempt <= cpp.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := cpp.calculateRetryDelay(attempt)
			select {
			case <-ctx.Done():
				return PipelineOutput{}, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Process stage
		migrations, stageErr := stage.Process(ctx, input.Migrations)
		if stageErr == nil {
			output = PipelineOutput{
				Migrations: migrations,
				Context:    input.Context,
				Errors:     make([]string, 0),
				Warnings:   make([]string, 0),
				Metadata:   input.Metadata,
				Metrics: StageMetrics{
					StageName:   stage.GetName(),
					Duration:    time.Since(stageStartTime),
					InputCount:  len(input.Migrations),
					OutputCount: len(migrations),
					ProcessedAt: time.Now(),
				},
			}
			break
		}

		err = stageErr
		
		// Apply middleware on error
		for _, middleware := range cpp.middleware {
			if middlewareErr := middleware.OnError(ctx, stageErr, input); middlewareErr != nil {
				return PipelineOutput{}, fmt.Errorf("middleware error handler failed: %w", middlewareErr)
			}
		}
	}

	if err != nil {
		return PipelineOutput{}, fmt.Errorf("stage %s failed after %d retries: %w", stage.GetName(), cpp.retryPolicy.MaxRetries, err)
	}

	// Apply middleware after
	for _, middleware := range cpp.middleware {
		modifiedOutput, err := middleware.After(ctx, output)
		if err != nil {
			return PipelineOutput{}, fmt.Errorf("middleware after failed: %w", err)
		}
		output = modifiedOutput
	}

	return output, nil
}

// calculateRetryDelay calculates the delay for retry attempts
func (cpp *ConcretePipelineProcessor) calculateRetryDelay(attempt int) time.Duration {
	delay := time.Duration(float64(cpp.retryPolicy.RetryDelay) * 
		(cpp.retryPolicy.BackoffFactor * float64(attempt-1)))
	
	if delay > cpp.retryPolicy.MaxDelay {
		delay = cpp.retryPolicy.MaxDelay
	}
	
	return delay
}

// updateMetrics updates the pipeline metrics
func (cpp *ConcretePipelineProcessor) updateMetrics(startTime time.Time, output PipelineOutput) {
	cpp.mutex.Lock()
	defer cpp.mutex.Unlock()
	
	cpp.metrics.EndTime = time.Now()
	cpp.metrics.TotalDuration = cpp.metrics.EndTime.Sub(startTime)
	cpp.metrics.ProcessedCount = len(output.Migrations)
	cpp.metrics.ErrorCount = len(output.Errors)
	cpp.metrics.WarningCount = len(output.Warnings)
}

// GetStages returns the pipeline stages
func (cpp *ConcretePipelineProcessor) GetStages() []PipelineStage {
	return cpp.stages
}

// GetMetrics returns the pipeline metrics
func (cpp *ConcretePipelineProcessor) GetMetrics() PipelineMetrics {
	cpp.mutex.RLock()
	defer cpp.mutex.RUnlock()
	return cpp.metrics
}

// Common pipeline stages

// ValidationStage validates migrations
type ValidationStage struct {
	validator types.MigrationValidator
}

// NewValidationStage creates a new validation stage
func NewValidationStage(validator types.MigrationValidator) PipelineStage {
	return &ValidationStage{validator: validator}
}

func (vs *ValidationStage) GetName() string {
	return "validation"
}

func (vs *ValidationStage) Process(ctx context.Context, migrations []types.MigrationScript) ([]types.MigrationScript, error) {
	// The validator operates on Migration values and returns only an error; a
	// non-nil error means the batch is invalid and processing must not proceed.
	if err := vs.validator.ValidateMigrations(scriptsToMigrationValues(migrations)); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return migrations, nil
}

func (vs *ValidationStage) CanParallel() bool {
	return true
}

func (vs *ValidationStage) GetDependencies() []string {
	return []string{}
}

// DependencyResolutionStage resolves migration dependencies
type DependencyResolutionStage struct {
	resolver types.DependencyResolver
}

// NewDependencyResolutionStage creates a new dependency resolution stage
func NewDependencyResolutionStage(resolver types.DependencyResolver) PipelineStage {
	return &DependencyResolutionStage{resolver: resolver}
}

func (drs *DependencyResolutionStage) GetName() string {
	return "dependency_resolution"
}

func (drs *DependencyResolutionStage) Process(ctx context.Context, migrations []types.MigrationScript) ([]types.MigrationScript, error) {
	// The resolver operates on *Migration and returns an ordered *Migration slice.
	ordered, err := drs.resolver.ResolveDependencies(ctx, scriptsToMigrationPointers(migrations))
	if err != nil {
		return nil, err
	}

	// Convert the ordered domain migrations back into the script contract type.
	orderedScripts := make([]types.MigrationScript, 0, len(ordered))
	for _, migration := range ordered {
		orderedScripts = append(orderedScripts, migrationToScript(migration))
	}

	return orderedScripts, nil
}

func (drs *DependencyResolutionStage) CanParallel() bool {
	return false
}

func (drs *DependencyResolutionStage) GetDependencies() []string {
	return []string{"validation"}
}

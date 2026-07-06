/**
 * @file resolver_factory.go
 * @description Comprehensive resolver factory integrating all resolver components
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/resolver/resolver_factory.go
 */

package resolver

import (
	"context"
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// ResolverType represents different types of resolvers
type ResolverType string

const (
	ResolverTypeDependency  ResolverType = "dependency"
	ResolverTypeConstraint  ResolverType = "constraint"
	ResolverTypeGraph       ResolverType = "graph"
	ResolverTypePrecondition ResolverType = "precondition"
	ResolverTypeComposite   ResolverType = "composite"
)

// ResolverFactory creates and manages different types of resolvers
type ResolverFactory interface {
	CreateDependencyResolver(config *ResolverConfig) (DependencyResolver, error)
	CreateConstraintResolver(config *ConstraintResolverConfig) (ConstraintResolver, error)
	CreateGraphResolver(config *GraphResolverConfig) (GraphResolver, error)
	CreatePreconditionResolver(db interface{}, config *ResolverConfig) (*PreconditionResolver, error)
	CreateCompositeResolver(config *CompositeResolverConfig) (CompositeResolver, error)
	GetAvailableResolvers() []ResolverType
	RegisterCustomResolver(resolverType ResolverType, factory func(interface{}) (interface{}, error)) error
}

// DefaultResolverFactory implements ResolverFactory
type DefaultResolverFactory struct {
	customFactories map[ResolverType]func(interface{}) (interface{}, error)
}

// NewDefaultResolverFactory creates a new resolver factory
func NewDefaultResolverFactory() *DefaultResolverFactory {
	return &DefaultResolverFactory{
		customFactories: make(map[ResolverType]func(interface{}) (interface{}, error)),
	}
}

// CreateDependencyResolver creates a dependency resolver
func (f *DefaultResolverFactory) CreateDependencyResolver(config *ResolverConfig) (DependencyResolver, error) {
	if config == nil {
		config = &ResolverConfig{
			AllowCircularDependencies: false,
			ValidatePreconditions:     true,
			MaxDepth:                  100,
			StrictMode:                true,
		}
	}

	return NewDefaultDependencyResolver(config), nil
}

// CreateConstraintResolver creates a constraint resolver
func (f *DefaultResolverFactory) CreateConstraintResolver(config *ConstraintResolverConfig) (ConstraintResolver, error) {
	if config == nil {
		config = &ConstraintResolverConfig{
			StrictMode:         false,
			FailOnWarnings:     false,
			MaxViolations:      100,
			EnabledConstraints: []ConstraintType{ConstraintTypeVersion, ConstraintTypeTime, ConstraintTypeConflict},
		}
	}

	resolver := NewDefaultConstraintResolver(config)

	// Add default constraints
	defaultConstraints := f.createDefaultConstraints()
	for _, constraint := range defaultConstraints {
		if err := resolver.AddConstraint(constraint); err != nil {
			return nil, fmt.Errorf("failed to add default constraint %s: %w", constraint.Name, err)
		}
	}

	return resolver, nil
}

// CreateGraphResolver creates a graph resolver
func (f *DefaultResolverFactory) CreateGraphResolver(config *GraphResolverConfig) (GraphResolver, error) {
	if config == nil {
		config = &GraphResolverConfig{
			EnableWeights:           true,
			EnableParallelAnalysis:  true,
			EnableComplexityAnalysis: true,
			MaxParallelism:          4,
			WeightCalculationMethod: "sql_complexity",
			OptimizationStrategy:    "critical_path",
			DefaultMigrationTime:    1000,
			ComplexityThreshold:     0.7,
		}
	}

	return NewDefaultGraphResolver(config), nil
}

// CreatePreconditionResolver creates a precondition resolver
func (f *DefaultResolverFactory) CreatePreconditionResolver(db interface{}, config *ResolverConfig) (*PreconditionResolver, error) {
	if config == nil {
		config = &ResolverConfig{
			ValidatePreconditions: true,
			StrictMode:           true,
		}
	}

	return NewPreconditionResolver(db, config), nil
}

// CreateCompositeResolver creates a composite resolver
func (f *DefaultResolverFactory) CreateCompositeResolver(config *CompositeResolverConfig) (CompositeResolver, error) {
	if config == nil {
		config = &CompositeResolverConfig{
			EnableDependencyResolution: true,
			EnableConstraintValidation: true,
			EnableGraphAnalysis:        true,
			EnablePreconditionCheck:    true,
			FailFast:                   false,
			ParallelValidation:         true,
		}
	}

	return NewDefaultCompositeResolver(config, f)
}

// GetAvailableResolvers returns available resolver types
func (f *DefaultResolverFactory) GetAvailableResolvers() []ResolverType {
	resolvers := []ResolverType{
		ResolverTypeDependency,
		ResolverTypeConstraint,
		ResolverTypeGraph,
		ResolverTypePrecondition,
		ResolverTypeComposite,
	}

	for resolverType := range f.customFactories {
		resolvers = append(resolvers, resolverType)
	}

	return resolvers
}

// RegisterCustomResolver registers a custom resolver factory
func (f *DefaultResolverFactory) RegisterCustomResolver(resolverType ResolverType, factory func(interface{}) (interface{}, error)) error {
	if factory == nil {
		return fmt.Errorf("factory function cannot be nil")
	}

	f.customFactories[resolverType] = factory
	return nil
}

// createDefaultConstraints creates default constraints
func (f *DefaultResolverFactory) createDefaultConstraints() []*Constraint {
	return []*Constraint{
		{
			Type:        ConstraintTypeVersion,
			Name:        "version_format",
			Description: "Validates migration version format",
			Rule:        "semantic_version",
			Severity:    SeverityError,
			ErrorMsg:    "Migration version must follow semantic versioning format",
		},
		{
			Type:        ConstraintTypeTime,
			Name:        "timestamp_required",
			Description: "Ensures migration has a valid timestamp",
			Rule:        "timestamp_not_zero",
			Severity:    SeverityError,
			ErrorMsg:    "Migration must have a valid timestamp",
		},
		{
			Type:        ConstraintTypeConflict,
			Name:        "dangerous_operations",
			Description: "Detects potentially dangerous SQL operations",
			Rule:        "pattern_match",
			Parameters: map[string]interface{}{
				"conflict_patterns": []string{
					`DROP\s+TABLE`,
					`TRUNCATE\s+TABLE`,
					`DELETE\s+FROM\s+\w+\s*;?\s*$`,
				},
			},
			Severity: SeverityWarning,
			ErrorMsg: "Migration contains potentially dangerous operations",
		},
		{
			Type:        ConstraintTypeResource,
			Name:        "sql_size_limit",
			Description: "Limits the size of migration SQL",
			Rule:        "max_size",
			Parameters: map[string]interface{}{
				"max_sql_size": 1000000, // 1MB
			},
			Severity: SeverityWarning,
			ErrorMsg: "Migration SQL exceeds recommended size limit",
		},
	}
}

// CompositeResolver combines multiple resolvers
type CompositeResolver interface {
	ResolveAll(ctx context.Context, migrations []*types.Migration) (*CompositeResolverResult, error)
	ValidateAll(ctx context.Context, migrations []*types.Migration) (*CompositeValidationResult, error)
	AnalyzeComplexity(ctx context.Context, migrations []*types.Migration) (*CompositeComplexityResult, error)
	GetExecutionPlan(ctx context.Context, migrations []*types.Migration) (*ExecutionPlan, error)
}

// CompositeResolverConfig configures the composite resolver
type CompositeResolverConfig struct {
	EnableDependencyResolution bool `json:"enable_dependency_resolution"`
	EnableConstraintValidation bool `json:"enable_constraint_validation"`
	EnableGraphAnalysis        bool `json:"enable_graph_analysis"`
	EnablePreconditionCheck    bool `json:"enable_precondition_check"`
	FailFast                   bool `json:"fail_fast"`
	ParallelValidation         bool `json:"parallel_validation"`
	
	DependencyConfig  *ResolverConfig           `json:"dependency_config"`
	ConstraintConfig  *ConstraintResolverConfig `json:"constraint_config"`
	GraphConfig       *GraphResolverConfig      `json:"graph_config"`
	PreconditionConfig *ResolverConfig          `json:"precondition_config"`
}

// CompositeResolverResult contains results from all resolvers
type CompositeResolverResult struct {
	ResolvedMigrations    []*types.Migration         `json:"resolved_migrations"`
	ExecutionOrder        []string                   `json:"execution_order"`
	ConstraintViolations  []*ConstraintViolation     `json:"constraint_violations"`
	DependencyGraph       *AdvancedDependencyGraph   `json:"dependency_graph"`
	ComplexityAnalysis    *GraphComplexity           `json:"complexity_analysis"`
	ParallelGroups        [][]string                 `json:"parallel_groups"`
	CriticalPath          []string                   `json:"critical_path"`
	Recommendations       []string                   `json:"recommendations"`
	Success               bool                       `json:"success"`
	Errors                []string                   `json:"errors"`
	Warnings              []string                   `json:"warnings"`
}

// CompositeValidationResult contains validation results
type CompositeValidationResult struct {
	IsValid              bool                   `json:"is_valid"`
	DependencyErrors     []string               `json:"dependency_errors"`
	ConstraintViolations []*ConstraintViolation `json:"constraint_violations"`
	PreconditionErrors   []string               `json:"precondition_errors"`
	GraphErrors          []string               `json:"graph_errors"`
	Warnings             []string               `json:"warnings"`
	Recommendations      []string               `json:"recommendations"`
}

// CompositeComplexityResult contains complexity analysis results
type CompositeComplexityResult struct {
	OverallComplexity    *GraphComplexity           `json:"overall_complexity"`
	DependencyGraph      *AdvancedDependencyGraph   `json:"dependency_graph"`
	CriticalPath         []string                   `json:"critical_path"`
	ParallelGroups       [][]string                 `json:"parallel_groups"`
	OptimizationSuggestions []string                `json:"optimization_suggestions"`
	PerformanceMetrics   *PerformanceMetrics        `json:"performance_metrics"`
}

// ExecutionPlan contains the complete execution plan
type ExecutionPlan struct {
	Migrations           []*types.Migration         `json:"migrations"`
	ExecutionOrder       []string                   `json:"execution_order"`
	ParallelGroups       [][]string                 `json:"parallel_groups"`
	EstimatedDuration    int                        `json:"estimated_duration"`
	CriticalPath         []string                   `json:"critical_path"`
	Dependencies         map[string][]string        `json:"dependencies"`
	Constraints          []*Constraint              `json:"constraints"`
	Preconditions        map[string][]string        `json:"preconditions"`
	RiskAssessment       *RiskAssessment            `json:"risk_assessment"`
	Recommendations      []string                   `json:"recommendations"`
}

// PerformanceMetrics contains performance-related metrics
type PerformanceMetrics struct {
	TotalMigrations      int     `json:"total_migrations"`
	EstimatedDuration    int     `json:"estimated_duration"`
	ParallelismFactor    float64 `json:"parallelism_factor"`
	CriticalPathLength   int     `json:"critical_path_length"`
	AverageComplexity    float64 `json:"average_complexity"`
	MaxDependencyDepth   int     `json:"max_dependency_depth"`
	OptimizationPotential float64 `json:"optimization_potential"`
}

// RiskAssessment contains risk analysis
type RiskAssessment struct {
	OverallRisk          string                 `json:"overall_risk"`
	RiskFactors          []string               `json:"risk_factors"`
	HighRiskMigrations   []string               `json:"high_risk_migrations"`
	MitigationStrategies []string               `json:"mitigation_strategies"`
	ConstraintViolations []*ConstraintViolation `json:"constraint_violations"`
	DependencyIssues     []string               `json:"dependency_issues"`
}

// DefaultCompositeResolver implements CompositeResolver
type DefaultCompositeResolver struct {
	config             *CompositeResolverConfig
	factory            ResolverFactory
	dependencyResolver DependencyResolver
	constraintResolver ConstraintResolver
	graphResolver      GraphResolver
	preconditionResolver *PreconditionResolver
}

// NewDefaultCompositeResolver creates a new composite resolver
func NewDefaultCompositeResolver(config *CompositeResolverConfig, factory ResolverFactory) (*DefaultCompositeResolver, error) {
	resolver := &DefaultCompositeResolver{
		config:  config,
		factory: factory,
	}

	// Initialize sub-resolvers based on configuration
	if config.EnableDependencyResolution {
		depResolver, err := factory.CreateDependencyResolver(config.DependencyConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create dependency resolver: %w", err)
		}
		resolver.dependencyResolver = depResolver
	}

	if config.EnableConstraintValidation {
		constResolver, err := factory.CreateConstraintResolver(config.ConstraintConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create constraint resolver: %w", err)
		}
		resolver.constraintResolver = constResolver
	}

	if config.EnableGraphAnalysis {
		graphResolver, err := factory.CreateGraphResolver(config.GraphConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create graph resolver: %w", err)
		}
		resolver.graphResolver = graphResolver
	}

	return resolver, nil
}

// ResolveAll performs complete resolution using all enabled resolvers
func (r *DefaultCompositeResolver) ResolveAll(ctx context.Context, migrations []*types.Migration) (*CompositeResolverResult, error) {
	result := &CompositeResolverResult{
		Success:      true,
		Errors:       make([]string, 0),
		Warnings:     make([]string, 0),
		Recommendations: make([]string, 0),
	}

	// Dependency resolution
	if r.config.EnableDependencyResolution && r.dependencyResolver != nil {
		resolved, err := r.dependencyResolver.ResolveDependencies(ctx, migrations)
		if err != nil {
			if r.config.FailFast {
				return nil, fmt.Errorf("dependency resolution failed: %w", err)
			}
			result.Success = false
			result.Errors = append(result.Errors, fmt.Sprintf("Dependency resolution failed: %v", err))
		} else {
			result.ResolvedMigrations = resolved
			
			executionOrder, err := r.dependencyResolver.GetExecutionOrder(ctx, resolved)
			if err == nil {
				result.ExecutionOrder = make([]string, len(executionOrder))
				for i, migration := range executionOrder {
					result.ExecutionOrder[i] = migration.ID
				}
			}
		}
	}

	// Constraint validation
	if r.config.EnableConstraintValidation && r.constraintResolver != nil {
		violations, err := r.constraintResolver.ValidateConstraints(ctx, migrations)
		if err != nil {
			if r.config.FailFast {
				return nil, fmt.Errorf("constraint validation failed: %w", err)
			}
			result.Errors = append(result.Errors, fmt.Sprintf("Constraint validation failed: %v", err))
		} else {
			result.ConstraintViolations = violations
			
			// Check for error-level violations
			for _, violation := range violations {
				if violation.Severity == SeverityError {
					result.Success = false
					result.Errors = append(result.Errors, violation.Message)
				} else if violation.Severity == SeverityWarning {
					result.Warnings = append(result.Warnings, violation.Message)
				}
			}
		}
	}

	// Graph analysis
	if r.config.EnableGraphAnalysis && r.graphResolver != nil {
		graph, err := r.graphResolver.BuildGraph(migrations)
		if err != nil {
			if r.config.FailFast {
				return nil, fmt.Errorf("graph analysis failed: %w", err)
			}
			result.Errors = append(result.Errors, fmt.Sprintf("Graph analysis failed: %v", err))
		} else {
			result.DependencyGraph = graph
			
			// Find critical path
			criticalPath, err := r.graphResolver.FindCriticalPath(graph)
			if err == nil {
				result.CriticalPath = criticalPath
			}
			
			// Find parallel groups
			parallelGroups, err := r.graphResolver.FindParallelExecutionGroups(graph)
			if err == nil {
				result.ParallelGroups = parallelGroups
			}
			
			// Analyze complexity
			complexity, err := r.graphResolver.AnalyzeComplexity(graph)
			if err == nil {
				result.ComplexityAnalysis = complexity
				result.Recommendations = append(result.Recommendations, complexity.Recommendations...)
			}
		}
	}

	return result, nil
}

// ValidateAll performs comprehensive validation
func (r *DefaultCompositeResolver) ValidateAll(ctx context.Context, migrations []*types.Migration) (*CompositeValidationResult, error) {
	result := &CompositeValidationResult{
		IsValid:              true,
		DependencyErrors:     make([]string, 0),
		ConstraintViolations: make([]*ConstraintViolation, 0),
		PreconditionErrors:   make([]string, 0),
		GraphErrors:          make([]string, 0),
		Warnings:             make([]string, 0),
		Recommendations:      make([]string, 0),
	}

	// Validate dependencies
	if r.dependencyResolver != nil {
		if err := r.dependencyResolver.ValidateDependencies(ctx, migrations); err != nil {
			result.IsValid = false
			result.DependencyErrors = append(result.DependencyErrors, err.Error())
		}
	}

	// Validate constraints
	if r.constraintResolver != nil {
		violations, err := r.constraintResolver.ValidateConstraints(ctx, migrations)
		if err != nil {
			result.GraphErrors = append(result.GraphErrors, err.Error())
		} else {
			result.ConstraintViolations = violations
			for _, violation := range violations {
				if violation.Severity == SeverityError {
					result.IsValid = false
				}
			}
		}
	}

	// Validate preconditions
	if r.preconditionResolver != nil {
		for _, migration := range migrations {
			if err := r.preconditionResolver.ValidatePreconditions(ctx, migration); err != nil {
				result.IsValid = false
				result.PreconditionErrors = append(result.PreconditionErrors, err.Error())
			}
		}
	}

	return result, nil
}

// AnalyzeComplexity performs comprehensive complexity analysis
func (r *DefaultCompositeResolver) AnalyzeComplexity(ctx context.Context, migrations []*types.Migration) (*CompositeComplexityResult, error) {
	result := &CompositeComplexityResult{
		OptimizationSuggestions: make([]string, 0),
	}

	if r.graphResolver != nil {
		graph, err := r.graphResolver.BuildGraph(migrations)
		if err != nil {
			return nil, fmt.Errorf("failed to build graph: %w", err)
		}
		result.DependencyGraph = graph

		complexity, err := r.graphResolver.AnalyzeComplexity(graph)
		if err != nil {
			return nil, fmt.Errorf("failed to analyze complexity: %w", err)
		}
		result.OverallComplexity = complexity

		criticalPath, err := r.graphResolver.FindCriticalPath(graph)
		if err == nil {
			result.CriticalPath = criticalPath
		}

		parallelGroups, err := r.graphResolver.FindParallelExecutionGroups(graph)
		if err == nil {
			result.ParallelGroups = parallelGroups
		}

		// Generate performance metrics
		result.PerformanceMetrics = &PerformanceMetrics{
			TotalMigrations:       len(migrations),
			EstimatedDuration:     r.calculateTotalDuration(graph),
			ParallelismFactor:     graph.Metadata.ParallelismFactor,
			CriticalPathLength:    len(criticalPath),
			AverageComplexity:     complexity.ComplexityScore,
			MaxDependencyDepth:    graph.Metadata.MaxDepth,
			OptimizationPotential: r.calculateOptimizationPotential(graph),
		}

		// Generate optimization suggestions
		result.OptimizationSuggestions = r.generateOptimizationSuggestions(graph, complexity)
	}

	return result, nil
}

// GetExecutionPlan creates a comprehensive execution plan
func (r *DefaultCompositeResolver) GetExecutionPlan(ctx context.Context, migrations []*types.Migration) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{
		Migrations:      migrations,
		Dependencies:    make(map[string][]string),
		Preconditions:   make(map[string][]string),
		Recommendations: make([]string, 0),
	}

	// Get execution order
	if r.dependencyResolver != nil {
		ordered, err := r.dependencyResolver.GetExecutionOrder(ctx, migrations)
		if err != nil {
			return nil, fmt.Errorf("failed to get execution order: %w", err)
		}
		
		plan.ExecutionOrder = make([]string, len(ordered))
		for i, migration := range ordered {
			plan.ExecutionOrder[i] = migration.ID
			plan.Dependencies[migration.ID] = migration.Dependencies
		}
	}

	// Get parallel groups and critical path
	if r.graphResolver != nil {
		graph, err := r.graphResolver.BuildGraph(migrations)
		if err == nil {
			parallelGroups, _ := r.graphResolver.FindParallelExecutionGroups(graph)
			plan.ParallelGroups = parallelGroups
			
			criticalPath, _ := r.graphResolver.FindCriticalPath(graph)
			plan.CriticalPath = criticalPath
			
			plan.EstimatedDuration = r.calculateTotalDuration(graph)
		}
	}

	// Get constraints
	if r.constraintResolver != nil {
		plan.Constraints = r.constraintResolver.GetConstraints()
	}

	// Perform risk assessment
	plan.RiskAssessment = r.performRiskAssessment(ctx, migrations)

	return plan, nil
}

// Helper methods

func (r *DefaultCompositeResolver) calculateTotalDuration(graph *AdvancedDependencyGraph) int {
	totalDuration := 0
	for _, node := range graph.Nodes {
		totalDuration += node.EstimatedTime
	}
	return totalDuration
}

func (r *DefaultCompositeResolver) calculateOptimizationPotential(graph *AdvancedDependencyGraph) float64 {
	if len(graph.ExecutionGroups) == 0 {
		return 0.0
	}
	
	sequentialTime := 0
	for _, node := range graph.Nodes {
		sequentialTime += node.EstimatedTime
	}
	
	parallelTime := 0
	for _, group := range graph.ExecutionGroups {
		groupMaxTime := 0
		for _, nodeID := range group {
			if node, exists := graph.Nodes[nodeID]; exists && node.EstimatedTime > groupMaxTime {
				groupMaxTime = node.EstimatedTime
			}
		}
		parallelTime += groupMaxTime
	}
	
	if sequentialTime > 0 {
		return float64(sequentialTime-parallelTime) / float64(sequentialTime)
	}
	
	return 0.0
}

func (r *DefaultCompositeResolver) generateOptimizationSuggestions(graph *AdvancedDependencyGraph, complexity *GraphComplexity) []string {
	suggestions := make([]string, 0)
	
	if complexity.ComplexityScore > 0.7 {
		suggestions = append(suggestions, "Consider breaking down complex migrations into smaller, more manageable parts")
	}
	
	if graph.Metadata.MaxDepth > 10 {
		suggestions = append(suggestions, "Deep dependency chains detected - consider flattening dependencies where possible")
	}
	
	if len(graph.ExecutionGroups) > 1 {
		suggestions = append(suggestions, "Parallel execution opportunities detected - consider running migrations in parallel")
	}
	
	if complexity.CouplingIndex > 0.5 {
		suggestions = append(suggestions, "High coupling detected - review migration dependencies for optimization opportunities")
	}
	
	return suggestions
}

func (r *DefaultCompositeResolver) performRiskAssessment(ctx context.Context, migrations []*types.Migration) *RiskAssessment {
	assessment := &RiskAssessment{
		OverallRisk:          "LOW",
		RiskFactors:          make([]string, 0),
		HighRiskMigrations:   make([]string, 0),
		MitigationStrategies: make([]string, 0),
		ConstraintViolations: make([]*ConstraintViolation, 0),
		DependencyIssues:     make([]string, 0),
	}

	// Analyze constraint violations
	if r.constraintResolver != nil {
		violations, err := r.constraintResolver.ValidateConstraints(ctx, migrations)
		if err == nil {
			assessment.ConstraintViolations = violations
			
			errorCount := 0
			warningCount := 0
			for _, violation := range violations {
				if violation.Severity == SeverityError {
					errorCount++
					assessment.HighRiskMigrations = append(assessment.HighRiskMigrations, violation.Migration.ID)
				} else if violation.Severity == SeverityWarning {
					warningCount++
				}
			}
			
			if errorCount > 0 {
				assessment.OverallRisk = "HIGH"
				assessment.RiskFactors = append(assessment.RiskFactors, fmt.Sprintf("%d constraint violations with error severity", errorCount))
			} else if warningCount > 5 {
				assessment.OverallRisk = "MEDIUM"
				assessment.RiskFactors = append(assessment.RiskFactors, fmt.Sprintf("%d constraint violations with warning severity", warningCount))
			}
		}
	}

	// Analyze dependency issues
	if r.dependencyResolver != nil {
		cycles, err := r.dependencyResolver.DetectCycles(ctx, migrations)
		if err == nil && len(cycles) > 0 {
			assessment.OverallRisk = "HIGH"
			assessment.RiskFactors = append(assessment.RiskFactors, "Circular dependencies detected")
			assessment.DependencyIssues = append(assessment.DependencyIssues, "Circular dependencies found")
		}
	}

	// Generate mitigation strategies
	if assessment.OverallRisk == "HIGH" {
		assessment.MitigationStrategies = append(assessment.MitigationStrategies,
			"Review and resolve all error-level constraint violations",
			"Test migrations in a staging environment",
			"Create rollback plans for all high-risk migrations",
			"Consider breaking down complex migrations")
	} else if assessment.OverallRisk == "MEDIUM" {
		assessment.MitigationStrategies = append(assessment.MitigationStrategies,
			"Review warning-level constraint violations",
			"Test migrations in a development environment",
			"Monitor migration execution closely")
	}

	return assessment
}

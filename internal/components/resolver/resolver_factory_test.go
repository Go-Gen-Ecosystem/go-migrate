/**
 * @file resolver_factory_test.go
 * @description Behavioral tests for the resolver factory and the composite
 * resolver: construction of sub-resolvers, ResolveAll / ValidateAll flows,
 * execution planning, and risk assessment.
 * @author Admilson B. F. Cossa
 */

package resolver

import (
	"context"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactory_CreateResolvers(t *testing.T) {
	f := NewDefaultResolverFactory()

	dep, err := f.CreateDependencyResolver(nil)
	require.NoError(t, err)
	assert.NotNil(t, dep)

	cons, err := f.CreateConstraintResolver(nil)
	require.NoError(t, err)
	assert.NotNil(t, cons)
	// The factory seeds default constraints.
	assert.NotEmpty(t, cons.GetConstraints())

	graph, err := f.CreateGraphResolver(nil)
	require.NoError(t, err)
	assert.NotNil(t, graph)

	pre, err := f.CreatePreconditionResolver(nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, pre)
}

func TestFactory_GetAvailableResolvers(t *testing.T) {
	f := NewDefaultResolverFactory()
	available := f.GetAvailableResolvers()
	assert.Contains(t, available, ResolverTypeDependency)
	assert.Contains(t, available, ResolverTypeConstraint)
	assert.Contains(t, available, ResolverTypeGraph)
	assert.Contains(t, available, ResolverTypeComposite)
}

func TestFactory_RegisterCustomResolver(t *testing.T) {
	f := NewDefaultResolverFactory()

	assert.Error(t, f.RegisterCustomResolver("custom", nil), "nil factory rejected")

	err := f.RegisterCustomResolver("custom", func(interface{}) (interface{}, error) {
		return struct{}{}, nil
	})
	require.NoError(t, err)
	assert.Contains(t, f.GetAvailableResolvers(), ResolverType("custom"))
}

func TestFactory_CreateCompositeResolver(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(nil)
	require.NoError(t, err)
	assert.NotNil(t, composite)
}

func TestCompositeResolver_ResolveAll_Success(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(&CompositeResolverConfig{
		EnableDependencyResolution: true,
		EnableGraphAnalysis:        true,
		// Constraint validation disabled to avoid default version/time
		// violations dominating this success-path check.
		EnableConstraintValidation: false,
		FailFast:                   false,
	})
	require.NoError(t, err)

	migrations := []*types.Migration{
		{ID: "a", Version: "v1.0.0", Script: "CREATE TABLE a (id INT);"},
		{ID: "b", Version: "v1.0.1", Script: "CREATE TABLE b (id INT);", Dependencies: []string{"a"}},
	}

	result, err := composite.ResolveAll(context.Background(), migrations)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	require.Len(t, result.ResolvedMigrations, 2)
	// Execution order respects the dependency: a before b.
	assert.Equal(t, []string{"a", "b"}, result.ExecutionOrder)
	assert.NotNil(t, result.DependencyGraph)
}

func TestCompositeResolver_ResolveAll_DependencyFailure(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(&CompositeResolverConfig{
		EnableDependencyResolution: true,
		FailFast:                   false,
	})
	require.NoError(t, err)

	// b depends on a missing migration.
	migrations := []*types.Migration{
		{ID: "b", Version: "v1.0.0", Dependencies: []string{"ghost"}},
	}

	result, err := composite.ResolveAll(context.Background(), migrations)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestCompositeResolver_ResolveAll_FailFast(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(&CompositeResolverConfig{
		EnableDependencyResolution: true,
		FailFast:                   true,
	})
	require.NoError(t, err)

	migrations := []*types.Migration{
		{ID: "b", Version: "v1.0.0", Dependencies: []string{"ghost"}},
	}

	_, err = composite.ResolveAll(context.Background(), migrations)
	require.Error(t, err, "fail-fast surfaces the dependency error directly")
}

func TestCompositeResolver_ValidateAll(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(&CompositeResolverConfig{
		EnableDependencyResolution: true,
		EnableConstraintValidation: true,
	})
	require.NoError(t, err)

	// Valid, resolvable set.
	valid := []*types.Migration{
		{ID: "a", Version: "v1.0.0", Script: "CREATE TABLE a (id INT);"},
	}
	result, err := composite.ValidateAll(context.Background(), valid)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Missing dependency invalidates the set.
	invalid := []*types.Migration{
		{ID: "b", Version: "v1.0.0", Dependencies: []string{"ghost"}},
	}
	result, err = composite.ValidateAll(context.Background(), invalid)
	require.NoError(t, err)
	assert.False(t, result.IsValid)
	assert.NotEmpty(t, result.DependencyErrors)
}

func TestCompositeResolver_GetExecutionPlan(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(nil)
	require.NoError(t, err)

	migrations := []*types.Migration{
		{ID: "a", Version: "v1.0.0", Script: "CREATE TABLE a (id INT);"},
		{ID: "b", Version: "v1.0.1", Script: "ALTER TABLE a ADD c INT;", Dependencies: []string{"a"}},
	}

	plan, err := composite.GetExecutionPlan(context.Background(), migrations)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, []string{"a", "b"}, plan.ExecutionOrder)
	assert.Equal(t, []string{"a"}, plan.Dependencies["b"])
	assert.NotNil(t, plan.RiskAssessment)
	assert.NotEmpty(t, plan.Constraints, "default constraints attached to plan")
}

func TestCompositeResolver_AnalyzeComplexity(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(&CompositeResolverConfig{
		EnableGraphAnalysis: true,
	})
	require.NoError(t, err)

	migrations := []*types.Migration{
		{ID: "a", Version: "v1.0.0", Script: "CREATE TABLE a (id INT);"},
		{ID: "b", Version: "v1.0.1", Script: "CREATE TABLE b (id INT);", Dependencies: []string{"a"}},
	}

	result, err := composite.AnalyzeComplexity(context.Background(), migrations)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.OverallComplexity)
	require.NotNil(t, result.PerformanceMetrics)
	assert.Equal(t, 2, result.PerformanceMetrics.TotalMigrations)
}

func TestCompositeResolver_RiskAssessment_Cycle(t *testing.T) {
	f := NewDefaultResolverFactory()
	composite, err := f.CreateCompositeResolver(&CompositeResolverConfig{
		EnableDependencyResolution: true,
		EnableConstraintValidation: true,
	})
	require.NoError(t, err)

	// A cycle drives the risk assessment to HIGH.
	migrations := []*types.Migration{
		{ID: "a", Version: "v1.0.0", Dependencies: []string{"b"}},
		{ID: "b", Version: "v1.0.1", Dependencies: []string{"a"}},
	}

	plan, err := composite.GetExecutionPlan(context.Background(), migrations)
	// A cycle makes GetExecutionOrder fail, which surfaces as an error here.
	if err != nil {
		assert.Contains(t, err.Error(), "execution order")
		return
	}
	// If planning still returns, the risk must reflect the cycle.
	require.NotNil(t, plan.RiskAssessment)
	assert.Equal(t, "HIGH", plan.RiskAssessment.OverallRisk)
}

/**
 * @file graph_resolver_test.go
 * @description Behavioral tests for the advanced graph resolver: graph building,
 * strongly connected components, critical path, complexity, parallel groups,
 * and execution-order optimization strategies.
 * @author Admilson B. F. Cossa
 */

package resolver

import (
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gmig builds a migration with an ID, script, and dependencies for graph tests.
func gmig(id, script string, deps ...string) *types.Migration {
	return &types.Migration{ID: id, Script: script, Dependencies: deps}
}

func TestNewDefaultGraphResolver_Defaults(t *testing.T) {
	r := NewDefaultGraphResolver(nil)
	require.NotNil(t, r)
	assert.Equal(t, 4, r.config.MaxParallelism)
	assert.Equal(t, "critical_path", r.config.OptimizationStrategy)
}

func TestBuildGraph_NodesEdgesLevels(t *testing.T) {
	migrations := []*types.Migration{
		gmig("a", "CREATE TABLE a (id INT);"),
		gmig("b", "ALTER TABLE a ADD c INT;", "a"),
	}

	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(migrations)
	require.NoError(t, err)

	require.Len(t, graph.Nodes, 2)
	// a is a dependency of b, so a's dependents include b.
	assert.Contains(t, graph.Nodes["a"].Dependents, "b")
	// Levels: a at level 0, b at level 1.
	assert.Equal(t, 0, graph.Nodes["a"].Level)
	assert.Equal(t, 1, graph.Nodes["b"].Level)
	assert.Equal(t, 1, graph.Metadata.MaxDepth)
	assert.Equal(t, 2, graph.Metadata.NodeCount)
	assert.Equal(t, 1, graph.Metadata.EdgeCount)
	// Weight reflects SQL complexity: CREATE TABLE adds weight.
	assert.Greater(t, graph.Nodes["a"].Weight, 1)
}

func TestFindStronglyConnectedComponents_NoCycle(t *testing.T) {
	migrations := []*types.Migration{
		gmig("a", "CREATE TABLE a (id INT);"),
		gmig("b", "CREATE TABLE b (id INT);", "a"),
	}

	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(migrations)
	require.NoError(t, err)

	components, err := r.FindStronglyConnectedComponents(graph)
	require.NoError(t, err)
	// Two acyclic nodes => two singleton SCCs.
	require.Len(t, components, 2)
	for _, c := range components {
		assert.Len(t, c, 1)
	}
}

func TestFindStronglyConnectedComponents_WithCycle(t *testing.T) {
	// a -> b -> a forms one SCC of size 2.
	migrations := []*types.Migration{
		gmig("a", "SELECT 1;", "b"),
		gmig("b", "SELECT 1;", "a"),
	}

	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(migrations)
	require.NoError(t, err)

	components, err := r.FindStronglyConnectedComponents(graph)
	require.NoError(t, err)

	var found bool
	for _, c := range components {
		if len(c) == 2 {
			found = true
			assert.ElementsMatch(t, []string{"a", "b"}, c)
		}
	}
	assert.True(t, found, "the a<->b cycle must form a 2-node SCC")
}

func TestFindCriticalPath(t *testing.T) {
	// Linear chain a -> b -> c; critical path is the whole chain.
	migrations := []*types.Migration{
		gmig("a", "CREATE TABLE a (id INT);"),
		gmig("b", "CREATE TABLE b (id INT);", "a"),
		gmig("c", "CREATE TABLE c (id INT);", "b"),
	}

	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(migrations)
	require.NoError(t, err)

	path, err := r.FindCriticalPath(graph)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, path)
	assert.True(t, graph.Nodes["c"].IsCritical)
}

func TestAnalyzeComplexity(t *testing.T) {
	migrations := []*types.Migration{
		gmig("a", "CREATE TABLE a (id INT);"),
		gmig("b", "ALTER TABLE a ADD col INT;", "a"),
	}

	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(migrations)
	require.NoError(t, err)

	complexity, err := r.AnalyzeComplexity(graph)
	require.NoError(t, err)
	require.NotNil(t, complexity)
	assert.NotEmpty(t, complexity.TimeComplexity)
	assert.NotEmpty(t, complexity.SpaceComplexity)
	assert.Equal(t, 1, complexity.DependencyDepth)
	// 1 edge / 2 nodes = 0.5 branching factor.
	assert.InDelta(t, 0.5, complexity.BranchingFactor, 0.001)
}

func TestFindParallelExecutionGroups(t *testing.T) {
	// Two independent, parallelizable migrations at level 0.
	migrations := []*types.Migration{
		gmig("a", "CREATE TABLE a (id INT);"),
		gmig("b", "CREATE TABLE b (id INT);"),
	}

	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(migrations)
	require.NoError(t, err)

	groups, err := r.FindParallelExecutionGroups(graph)
	require.NoError(t, err)
	require.NotEmpty(t, groups)

	// Both independent CREATE TABLE migrations touch different tables and can
	// share a parallel group.
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	assert.Equal(t, 2, total)
}

func TestFindParallelExecutionGroups_Disabled(t *testing.T) {
	r := NewDefaultGraphResolver(&GraphResolverConfig{EnableParallelAnalysis: false})
	graph, err := r.BuildGraph([]*types.Migration{gmig("a", "SELECT 1;")})
	require.NoError(t, err)

	groups, err := r.FindParallelExecutionGroups(graph)
	require.NoError(t, err)
	assert.Nil(t, groups)
}

func TestOptimizeExecutionOrder_Strategies(t *testing.T) {
	migrations := []*types.Migration{
		gmig("a", "CREATE TABLE a (id INT);"),
		gmig("b", "CREATE TABLE b (id INT);", "a"),
		gmig("c", "INSERT INTO b VALUES (1);", "b"),
	}

	strategies := []string{"critical_path", "priority", "time", "unknown_default"}
	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			r := NewDefaultGraphResolver(&GraphResolverConfig{
				EnableWeights:        true,
				OptimizationStrategy: strategy,
				DefaultMigrationTime: 1000,
			})
			graph, err := r.BuildGraph(migrations)
			require.NoError(t, err)

			order, err := r.OptimizeExecutionOrder(graph)
			require.NoError(t, err)
			assert.Len(t, order, 3)
			// Regardless of strategy, dependency a must never come after b.
			ai, bi := indexOfStr(order, "a"), indexOfStr(order, "b")
			assert.Less(t, ai, bi)
		})
	}
}

func TestBuildGraph_Empty(t *testing.T) {
	r := NewDefaultGraphResolver(nil)
	graph, err := r.BuildGraph(nil)
	require.NoError(t, err)
	assert.Empty(t, graph.Nodes)
	assert.Equal(t, 0, graph.Metadata.NodeCount)
}

// indexOfStr returns the position of id in a string slice, or -1.
func indexOfStr(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

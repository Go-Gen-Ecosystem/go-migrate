/**
 * @file graph_resolver.go
 * @description Advanced graph-based dependency resolver with sophisticated algorithms
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/resolver/graph_resolver.go
 */

package resolver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// GraphResolver provides advanced graph-based dependency resolution
type GraphResolver interface {
	BuildGraph(migrations []*types.Migration) (*AdvancedDependencyGraph, error)
	FindStronglyConnectedComponents(graph *AdvancedDependencyGraph) ([][]string, error)
	FindCriticalPath(graph *AdvancedDependencyGraph) ([]string, error)
	AnalyzeComplexity(graph *AdvancedDependencyGraph) (*GraphComplexity, error)
	OptimizeExecutionOrder(graph *AdvancedDependencyGraph) ([]string, error)
	FindParallelExecutionGroups(graph *AdvancedDependencyGraph) ([][]string, error)
}

// AdvancedDependencyGraph represents an enhanced dependency graph
type AdvancedDependencyGraph struct {
	Nodes           map[string]*AdvancedDependencyNode `json:"nodes"`
	Edges           map[string][]string                `json:"edges"`
	ReverseEdges    map[string][]string                `json:"reverse_edges"`
	Weights         map[string]map[string]int          `json:"weights"`
	Metadata        *GraphMetadata                     `json:"metadata"`
	Components      [][]string                         `json:"components"`
	CriticalPath    []string                           `json:"critical_path"`
	ExecutionGroups [][]string                         `json:"execution_groups"`
}

// AdvancedDependencyNode represents an enhanced dependency node
type AdvancedDependencyNode struct {
	Migration       *types.Migration `json:"migration"`
	Dependencies    []string         `json:"dependencies"`
	Dependents      []string         `json:"dependents"`
	Level           int              `json:"level"`
	Weight          int              `json:"weight"`
	EstimatedTime   int              `json:"estimated_time"`
	Priority        int              `json:"priority"`
	Visited         bool             `json:"visited"`
	InStack         bool             `json:"in_stack"`
	ComponentID     int              `json:"component_id"`
	IsCritical      bool             `json:"is_critical"`
	CanRunInParallel bool            `json:"can_run_in_parallel"`
}

// GraphMetadata contains metadata about the graph
type GraphMetadata struct {
	NodeCount           int     `json:"node_count"`
	EdgeCount           int     `json:"edge_count"`
	MaxDepth            int     `json:"max_depth"`
	AverageDepth        float64 `json:"average_depth"`
	CyclomaticComplexity int     `json:"cyclomatic_complexity"`
	Density             float64 `json:"density"`
	HasCycles           bool    `json:"has_cycles"`
	ComponentCount      int     `json:"component_count"`
	CriticalPathLength  int     `json:"critical_path_length"`
	ParallelismFactor   float64 `json:"parallelism_factor"`
}

// GraphComplexity represents the complexity analysis of the graph
type GraphComplexity struct {
	TimeComplexity   string  `json:"time_complexity"`
	SpaceComplexity  string  `json:"space_complexity"`
	DependencyDepth  int     `json:"dependency_depth"`
	BranchingFactor  float64 `json:"branching_factor"`
	CouplingIndex    float64 `json:"coupling_index"`
	CohesionIndex    float64 `json:"cohesion_index"`
	ComplexityScore  float64 `json:"complexity_score"`
	Recommendations  []string `json:"recommendations"`
}

// DefaultGraphResolver implements GraphResolver
type DefaultGraphResolver struct {
	config *GraphResolverConfig
}

// GraphResolverConfig configures the graph resolver
type GraphResolverConfig struct {
	EnableWeights           bool    `json:"enable_weights"`
	EnableParallelAnalysis  bool    `json:"enable_parallel_analysis"`
	EnableComplexityAnalysis bool   `json:"enable_complexity_analysis"`
	MaxParallelism          int     `json:"max_parallelism"`
	WeightCalculationMethod string  `json:"weight_calculation_method"`
	OptimizationStrategy    string  `json:"optimization_strategy"`
	DefaultMigrationTime    int     `json:"default_migration_time"`
	ComplexityThreshold     float64 `json:"complexity_threshold"`
}

// NewDefaultGraphResolver creates a new graph resolver
func NewDefaultGraphResolver(config *GraphResolverConfig) *DefaultGraphResolver {
	if config == nil {
		config = &GraphResolverConfig{
			EnableWeights:           true,
			EnableParallelAnalysis:  true,
			EnableComplexityAnalysis: true,
			MaxParallelism:          4,
			WeightCalculationMethod: "sql_complexity",
			OptimizationStrategy:    "critical_path",
			DefaultMigrationTime:    1000, // milliseconds
			ComplexityThreshold:     0.7,
		}
	}

	return &DefaultGraphResolver{
		config: config,
	}
}

// BuildGraph builds an advanced dependency graph
func (r *DefaultGraphResolver) BuildGraph(migrations []*types.Migration) (*AdvancedDependencyGraph, error) {
	graph := &AdvancedDependencyGraph{
		Nodes:        make(map[string]*AdvancedDependencyNode),
		Edges:        make(map[string][]string),
		ReverseEdges: make(map[string][]string),
		Weights:      make(map[string]map[string]int),
		Metadata:     &GraphMetadata{},
	}

	// Create nodes
	for _, migration := range migrations {
		node := &AdvancedDependencyNode{
			Migration:       migration,
			Dependencies:    migration.Dependencies,
			Dependents:      make([]string, 0),
			Weight:          r.calculateNodeWeight(migration),
			EstimatedTime:   r.estimateMigrationTime(migration),
			Priority:        r.calculatePriority(migration),
			CanRunInParallel: r.canRunInParallel(migration),
		}

		graph.Nodes[migration.ID] = node
		graph.Edges[migration.ID] = migration.Dependencies

		if graph.Weights[migration.ID] == nil {
			graph.Weights[migration.ID] = make(map[string]int)
		}
	}

	// Build reverse edges and edge weights
	for nodeID, dependencies := range graph.Edges {
		for _, depID := range dependencies {
			if depNode, exists := graph.Nodes[depID]; exists {
				depNode.Dependents = append(depNode.Dependents, nodeID)
				
				if graph.ReverseEdges[depID] == nil {
					graph.ReverseEdges[depID] = make([]string, 0)
				}
				graph.ReverseEdges[depID] = append(graph.ReverseEdges[depID], nodeID)

				// Calculate edge weight
				if r.config.EnableWeights {
					weight := r.calculateEdgeWeight(graph.Nodes[depID], graph.Nodes[nodeID])
					graph.Weights[depID][nodeID] = weight
				}
			}
		}
	}

	// Calculate levels
	r.calculateLevels(graph)

	// Update metadata
	r.updateMetadata(graph)

	return graph, nil
}

// FindStronglyConnectedComponents finds SCCs using Tarjan's algorithm
func (r *DefaultGraphResolver) FindStronglyConnectedComponents(graph *AdvancedDependencyGraph) ([][]string, error) {
	index := 0
	stack := make([]string, 0)
	indices := make(map[string]int)
	lowlinks := make(map[string]int)
	onStack := make(map[string]bool)
	components := make([][]string, 0)

	var strongConnect func(string)
	strongConnect = func(nodeID string) {
		indices[nodeID] = index
		lowlinks[nodeID] = index
		index++
		stack = append(stack, nodeID)
		onStack[nodeID] = true

		// Consider successors
		for _, successor := range graph.Edges[nodeID] {
			if _, visited := indices[successor]; !visited {
				strongConnect(successor)
				if lowlinks[successor] < lowlinks[nodeID] {
					lowlinks[nodeID] = lowlinks[successor]
				}
			} else if onStack[successor] {
				if indices[successor] < lowlinks[nodeID] {
					lowlinks[nodeID] = indices[successor]
				}
			}
		}

		// If nodeID is a root node, pop the stack and create SCC
		if lowlinks[nodeID] == indices[nodeID] {
			component := make([]string, 0)
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				component = append(component, w)
				if w == nodeID {
					break
				}
			}
			components = append(components, component)
		}
	}

	// Run algorithm for all unvisited nodes
	for nodeID := range graph.Nodes {
		if _, visited := indices[nodeID]; !visited {
			strongConnect(nodeID)
		}
	}

	graph.Components = components
	return components, nil
}

// FindCriticalPath finds the critical path in the dependency graph
func (r *DefaultGraphResolver) FindCriticalPath(graph *AdvancedDependencyGraph) ([]string, error) {
	// Use longest path algorithm for DAG
	distances := make(map[string]int)
	predecessors := make(map[string]string)

	// Initialize distances
	for nodeID := range graph.Nodes {
		distances[nodeID] = 0
	}

	// Topological sort
	sorted, err := r.topologicalSort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to perform topological sort: %w", err)
	}

	// Calculate longest paths
	for _, nodeID := range sorted {
		node := graph.Nodes[nodeID]
		for _, dependentID := range node.Dependents {
			newDistance := distances[nodeID] + node.EstimatedTime
			if newDistance > distances[dependentID] {
				distances[dependentID] = newDistance
				predecessors[dependentID] = nodeID
			}
		}
	}

	// Find the node with maximum distance
	maxDistance := 0
	endNode := ""
	for nodeID, distance := range distances {
		if distance > maxDistance {
			maxDistance = distance
			endNode = nodeID
		}
	}

	// Reconstruct critical path
	path := make([]string, 0)
	current := endNode
	for current != "" {
		path = append([]string{current}, path...)
		graph.Nodes[current].IsCritical = true
		current = predecessors[current]
	}

	graph.CriticalPath = path
	return path, nil
}

// AnalyzeComplexity analyzes the complexity of the dependency graph
func (r *DefaultGraphResolver) AnalyzeComplexity(graph *AdvancedDependencyGraph) (*GraphComplexity, error) {
	complexity := &GraphComplexity{
		Recommendations: make([]string, 0),
	}

	nodeCount := len(graph.Nodes)
	edgeCount := graph.Metadata.EdgeCount

	// Time and space complexity
	complexity.TimeComplexity = fmt.Sprintf("O(%d + %d)", nodeCount, edgeCount)
	complexity.SpaceComplexity = fmt.Sprintf("O(%d)", nodeCount)

	// Dependency depth
	complexity.DependencyDepth = graph.Metadata.MaxDepth

	// Branching factor
	if nodeCount > 0 {
		complexity.BranchingFactor = float64(edgeCount) / float64(nodeCount)
	}

	// Coupling index (how interconnected the graph is)
	maxPossibleEdges := nodeCount * (nodeCount - 1)
	if maxPossibleEdges > 0 {
		complexity.CouplingIndex = float64(edgeCount) / float64(maxPossibleEdges)
	}

	// Cohesion index (how well-grouped the components are)
	if len(graph.Components) > 0 {
		complexity.CohesionIndex = 1.0 - (float64(len(graph.Components)) / float64(nodeCount))
	}

	// Overall complexity score
	complexity.ComplexityScore = (complexity.CouplingIndex + 
		float64(complexity.DependencyDepth)/10.0 + 
		complexity.BranchingFactor/10.0) / 3.0

	// Generate recommendations
	if complexity.ComplexityScore > r.config.ComplexityThreshold {
		complexity.Recommendations = append(complexity.Recommendations,
			"Consider breaking down complex migrations into smaller parts")
	}

	if complexity.DependencyDepth > 10 {
		complexity.Recommendations = append(complexity.Recommendations,
			"Deep dependency chains detected - consider flattening dependencies")
	}

	if complexity.CouplingIndex > 0.5 {
		complexity.Recommendations = append(complexity.Recommendations,
			"High coupling detected - review migration dependencies")
	}

	if len(graph.Components) > nodeCount/2 {
		complexity.Recommendations = append(complexity.Recommendations,
			"Many isolated components - consider consolidating related migrations")
	}

	return complexity, nil
}

// OptimizeExecutionOrder optimizes the execution order based on strategy
func (r *DefaultGraphResolver) OptimizeExecutionOrder(graph *AdvancedDependencyGraph) ([]string, error) {
	switch r.config.OptimizationStrategy {
	case "critical_path":
		return r.optimizeByCriticalPath(graph)
	case "priority":
		return r.optimizeByPriority(graph)
	case "time":
		return r.optimizeByTime(graph)
	default:
		return r.topologicalSort(graph)
	}
}

// FindParallelExecutionGroups finds groups of migrations that can run in parallel
func (r *DefaultGraphResolver) FindParallelExecutionGroups(graph *AdvancedDependencyGraph) ([][]string, error) {
	if !r.config.EnableParallelAnalysis {
		return nil, nil
	}

	groups := make([][]string, 0)
	processed := make(map[string]bool)

	// Group by levels first
	levelGroups := make(map[int][]string)
	for nodeID, node := range graph.Nodes {
		if node.CanRunInParallel {
			levelGroups[node.Level] = append(levelGroups[node.Level], nodeID)
		}
	}

	// Create parallel groups within each level
	for level := 0; level <= graph.Metadata.MaxDepth; level++ {
		if nodes, exists := levelGroups[level]; exists {
			parallelGroups := r.createParallelGroups(nodes, graph, r.config.MaxParallelism)
			groups = append(groups, parallelGroups...)
			
			for _, group := range parallelGroups {
				for _, nodeID := range group {
					processed[nodeID] = true
				}
			}
		}
	}

	// Add remaining nodes as individual groups
	for nodeID := range graph.Nodes {
		if !processed[nodeID] {
			groups = append(groups, []string{nodeID})
		}
	}

	graph.ExecutionGroups = groups
	return groups, nil
}

// Helper methods

func (r *DefaultGraphResolver) calculateNodeWeight(migration *types.Migration) int {
	switch r.config.WeightCalculationMethod {
	case "sql_complexity":
		return r.calculateSQLComplexity(migration.SQL)
	case "sql_length":
		return len(migration.SQL)
	case "dependency_count":
		return len(migration.Dependencies)
	default:
		return 1
	}
}

func (r *DefaultGraphResolver) calculateSQLComplexity(sql string) int {
	complexity := 1
	sql = strings.ToUpper(sql)

	// Add complexity for different SQL operations
	if strings.Contains(sql, "CREATE TABLE") {
		complexity += 3
	}
	if strings.Contains(sql, "ALTER TABLE") {
		complexity += 2
	}
	if strings.Contains(sql, "DROP TABLE") {
		complexity += 2
	}
	if strings.Contains(sql, "CREATE INDEX") {
		complexity += 1
	}
	if strings.Contains(sql, "FOREIGN KEY") {
		complexity += 2
	}
	if strings.Contains(sql, "TRIGGER") {
		complexity += 3
	}

	return complexity
}

func (r *DefaultGraphResolver) estimateMigrationTime(migration *types.Migration) int {
	baseTime := r.config.DefaultMigrationTime
	complexity := r.calculateSQLComplexity(migration.SQL)
	return baseTime * complexity
}

func (r *DefaultGraphResolver) calculatePriority(migration *types.Migration) int {
	// Higher priority for structural changes
	sql := strings.ToUpper(migration.SQL)
	if strings.Contains(sql, "CREATE TABLE") {
		return 10
	}
	if strings.Contains(sql, "ALTER TABLE") {
		return 8
	}
	if strings.Contains(sql, "CREATE INDEX") {
		return 6
	}
	if strings.Contains(sql, "INSERT") || strings.Contains(sql, "UPDATE") {
		return 4
	}
	return 5
}

func (r *DefaultGraphResolver) canRunInParallel(migration *types.Migration) bool {
	sql := strings.ToUpper(migration.SQL)
	// Avoid parallel execution for potentially conflicting operations
	conflictingOps := []string{"ALTER TABLE", "DROP TABLE", "CREATE TRIGGER", "DROP TRIGGER"}
	for _, op := range conflictingOps {
		if strings.Contains(sql, op) {
			return false
		}
	}
	return true
}

func (r *DefaultGraphResolver) calculateEdgeWeight(from, to *AdvancedDependencyNode) int {
	return from.Weight + to.Weight
}

func (r *DefaultGraphResolver) calculateLevels(graph *AdvancedDependencyGraph) {
	// Calculate levels using BFS
	queue := make([]string, 0)
	inDegree := make(map[string]int)

	// Initialize in-degrees
	for nodeID := range graph.Nodes {
		inDegree[nodeID] = len(graph.Nodes[nodeID].Dependencies)
		if inDegree[nodeID] == 0 {
			queue = append(queue, nodeID)
			graph.Nodes[nodeID].Level = 0
		}
	}

	maxLevel := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentLevel := graph.Nodes[current].Level

		for _, dependent := range graph.Nodes[current].Dependents {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				graph.Nodes[dependent].Level = currentLevel + 1
				if currentLevel + 1 > maxLevel {
					maxLevel = currentLevel + 1
				}
				queue = append(queue, dependent)
			}
		}
	}

	graph.Metadata.MaxDepth = maxLevel
}

func (r *DefaultGraphResolver) updateMetadata(graph *AdvancedDependencyGraph) {
	graph.Metadata.NodeCount = len(graph.Nodes)
	
	edgeCount := 0
	totalDepth := 0
	for _, node := range graph.Nodes {
		edgeCount += len(node.Dependencies)
		totalDepth += node.Level
	}
	
	graph.Metadata.EdgeCount = edgeCount
	if graph.Metadata.NodeCount > 0 {
		graph.Metadata.AverageDepth = float64(totalDepth) / float64(graph.Metadata.NodeCount)
		graph.Metadata.Density = float64(edgeCount) / float64(graph.Metadata.NodeCount * (graph.Metadata.NodeCount - 1))
	}
}

func (r *DefaultGraphResolver) topologicalSort(graph *AdvancedDependencyGraph) ([]string, error) {
	inDegree := make(map[string]int)
	for nodeID := range graph.Nodes {
		inDegree[nodeID] = len(graph.Nodes[nodeID].Dependencies)
	}

	queue := make([]string, 0)
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}

	result := make([]string, 0, len(graph.Nodes))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, dependent := range graph.Nodes[current].Dependents {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(result) != len(graph.Nodes) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}

func (r *DefaultGraphResolver) optimizeByCriticalPath(graph *AdvancedDependencyGraph) ([]string, error) {
	// First find critical path
	_, err := r.FindCriticalPath(graph)
	if err != nil {
		return nil, err
	}

	// Sort by critical path priority, then by regular topological order
	sorted, err := r.topologicalSort(graph)
	if err != nil {
		return nil, err
	}

	// Prioritize critical path nodes
	sort.SliceStable(sorted, func(i, j int) bool {
		nodeI := graph.Nodes[sorted[i]]
		nodeJ := graph.Nodes[sorted[j]]
		
		if nodeI.IsCritical && !nodeJ.IsCritical {
			return true
		}
		if !nodeI.IsCritical && nodeJ.IsCritical {
			return false
		}
		
		return nodeI.Level < nodeJ.Level
	})

	return sorted, nil
}

func (r *DefaultGraphResolver) optimizeByPriority(graph *AdvancedDependencyGraph) ([]string, error) {
	sorted, err := r.topologicalSort(graph)
	if err != nil {
		return nil, err
	}

	// Sort by priority within each level
	sort.SliceStable(sorted, func(i, j int) bool {
		nodeI := graph.Nodes[sorted[i]]
		nodeJ := graph.Nodes[sorted[j]]
		
		if nodeI.Level != nodeJ.Level {
			return nodeI.Level < nodeJ.Level
		}
		
		return nodeI.Priority > nodeJ.Priority
	})

	return sorted, nil
}

func (r *DefaultGraphResolver) optimizeByTime(graph *AdvancedDependencyGraph) ([]string, error) {
	sorted, err := r.topologicalSort(graph)
	if err != nil {
		return nil, err
	}

	// Sort by estimated time within each level (shortest first)
	sort.SliceStable(sorted, func(i, j int) bool {
		nodeI := graph.Nodes[sorted[i]]
		nodeJ := graph.Nodes[sorted[j]]
		
		if nodeI.Level != nodeJ.Level {
			return nodeI.Level < nodeJ.Level
		}
		
		return nodeI.EstimatedTime < nodeJ.EstimatedTime
	})

	return sorted, nil
}

func (r *DefaultGraphResolver) createParallelGroups(nodes []string, graph *AdvancedDependencyGraph, maxParallelism int) [][]string {
	groups := make([][]string, 0)
	remaining := make([]string, len(nodes))
	copy(remaining, nodes)

	for len(remaining) > 0 {
		group := make([]string, 0, maxParallelism)
		used := make(map[int]bool)

		for i, nodeID := range remaining {
			if used[i] || len(group) >= maxParallelism {
				continue
			}

			// Check if this node can run in parallel with nodes already in the group
			canAdd := true
			for _, groupNodeID := range group {
				if r.hasConflict(nodeID, groupNodeID, graph) {
					canAdd = false
					break
				}
			}

			if canAdd {
				group = append(group, nodeID)
				used[i] = true
			}
		}

		if len(group) > 0 {
			groups = append(groups, group)
			
			// Remove used nodes from remaining
			newRemaining := make([]string, 0)
			for i, nodeID := range remaining {
				if !used[i] {
					newRemaining = append(newRemaining, nodeID)
				}
			}
			remaining = newRemaining
		} else {
			// If no nodes can be added, add the first remaining node individually
			groups = append(groups, []string{remaining[0]})
			remaining = remaining[1:]
		}
	}

	return groups
}

func (r *DefaultGraphResolver) hasConflict(nodeID1, nodeID2 string, graph *AdvancedDependencyGraph) bool {
	node1 := graph.Nodes[nodeID1]
	node2 := graph.Nodes[nodeID2]

	// Check for direct dependencies
	for _, dep := range node1.Dependencies {
		if dep == nodeID2 {
			return true
		}
	}
	for _, dep := range node2.Dependencies {
		if dep == nodeID1 {
			return true
		}
	}

	// Check for table conflicts (simplified)
	sql1 := strings.ToUpper(node1.Migration.SQL)
	sql2 := strings.ToUpper(node2.Migration.SQL)

	// Extract table names (simplified pattern matching)
	tables1 := r.extractTableNames(sql1)
	tables2 := r.extractTableNames(sql2)

	for table1 := range tables1 {
		if _, exists := tables2[table1]; exists {
			return true
		}
	}

	return false
}

func (r *DefaultGraphResolver) extractTableNames(sql string) map[string]bool {
	tables := make(map[string]bool)
	
	// Simple pattern matching for table names
	// This is a simplified implementation - in practice, you'd want more sophisticated SQL parsing
	words := strings.Fields(sql)
	for i, word := range words {
		if (word == "TABLE" || word == "FROM" || word == "JOIN" || word == "UPDATE") && i+1 < len(words) {
			tableName := strings.Trim(words[i+1], "(),;")
			tables[tableName] = true
		}
	}
	
	return tables
}

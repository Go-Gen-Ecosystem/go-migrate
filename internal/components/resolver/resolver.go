/**
 * @file resolver.go
 * @description Dependency resolution for migrations with topological sort and cycle detection
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/resolver/resolver.go
 */

package resolver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DependencyResolver interface for resolving migration dependencies
type DependencyResolver interface {
	ResolveDependencies(ctx context.Context, migrations []*types.Migration) ([]*types.Migration, error)
	ValidateDependencies(ctx context.Context, migrations []*types.Migration) error
	DetectCycles(ctx context.Context, migrations []*types.Migration) ([][]string, error)
	GetExecutionOrder(ctx context.Context, migrations []*types.Migration) ([]*types.Migration, error)
}

// DefaultDependencyResolver implements DependencyResolver with topological sorting
type DefaultDependencyResolver struct {
	config *ResolverConfig
}

// ResolverConfig contains configuration for dependency resolution
type ResolverConfig struct {
	// Whether to allow circular dependencies (not recommended)
	AllowCircularDependencies bool

	// Whether to validate preconditions before resolving dependencies
	ValidatePreconditions bool

	// Maximum depth for dependency resolution
	MaxDepth int

	// Whether to use strict dependency checking
	StrictMode bool
}

// DependencyGraph represents a directed graph of migration dependencies
type DependencyGraph struct {
	nodes map[string]*DependencyNode
	edges map[string][]string
}

// DependencyNode represents a migration in the dependency graph
type DependencyNode struct {
	Migration    *types.Migration
	Dependencies []string
	Dependents   []string
	Visited      bool
	InStack      bool
	Order        int
}

// NewDefaultDependencyResolver creates a new dependency resolver
func NewDefaultDependencyResolver(config *ResolverConfig) *DefaultDependencyResolver {
	if config == nil {
		config = &ResolverConfig{
			AllowCircularDependencies: false,
			ValidatePreconditions:     true,
			MaxDepth:                  100,
			StrictMode:                true,
		}
	}

	return &DefaultDependencyResolver{
		config: config,
	}
}

// ResolveDependencies resolves migration dependencies and returns them in execution order
func (r *DefaultDependencyResolver) ResolveDependencies(ctx context.Context, migrations []*types.Migration) ([]*types.Migration, error) {
	// First validate dependencies
	if err := r.ValidateDependencies(ctx, migrations); err != nil {
		return nil, fmt.Errorf("dependency validation failed: %w", err)
	}

	// Get execution order using topological sort
	return r.GetExecutionOrder(ctx, migrations)
}

// ValidateDependencies validates that all dependencies are satisfied
func (r *DefaultDependencyResolver) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	migrationMap := make(map[string]*types.Migration)
	for _, m := range migrations {
		migrationMap[m.ID] = m
	}

	// Check that all dependencies exist
	for _, migration := range migrations {
		for _, dep := range migration.Dependencies {
			if _, exists := migrationMap[dep]; !exists {
				return fmt.Errorf("migration %s depends on %s which does not exist", migration.ID, dep)
			}
		}
	}

	// Check for circular dependencies
	cycles, err := r.DetectCycles(ctx, migrations)
	if err != nil {
		return fmt.Errorf("failed to detect cycles: %w", err)
	}

	if len(cycles) > 0 && !r.config.AllowCircularDependencies {
		return fmt.Errorf("circular dependencies detected: %v", cycles)
	}

	return nil
}

// DetectCycles detects circular dependencies using DFS
func (r *DefaultDependencyResolver) DetectCycles(ctx context.Context, migrations []*types.Migration) ([][]string, error) {
	graph := r.buildDependencyGraph(migrations)
	cycles := make([][]string, 0)

	for nodeID := range graph.nodes {
		if !graph.nodes[nodeID].Visited {
			cycle := r.dfsDetectCycle(graph, nodeID, make([]string, 0))
			if len(cycle) > 0 {
				cycles = append(cycles, cycle)
			}
		}
	}

	return cycles, nil
}

// GetExecutionOrder returns migrations in topological order
func (r *DefaultDependencyResolver) GetExecutionOrder(ctx context.Context, migrations []*types.Migration) ([]*types.Migration, error) {
	graph := r.buildDependencyGraph(migrations)

	// Perform topological sort using Kahn's algorithm
	ordered, err := r.topologicalSort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to sort dependencies: %w", err)
	}

	// Convert back to migration slice
	result := make([]*types.Migration, 0, len(ordered))
	for _, nodeID := range ordered {
		if node, exists := graph.nodes[nodeID]; exists {
			result = append(result, node.Migration)
		}
	}

	return result, nil
}

// buildDependencyGraph creates a dependency graph from migrations
func (r *DefaultDependencyResolver) buildDependencyGraph(migrations []*types.Migration) *DependencyGraph {
	graph := &DependencyGraph{
		nodes: make(map[string]*DependencyNode),
		edges: make(map[string][]string),
	}

	// Create nodes
	for _, migration := range migrations {
		graph.nodes[migration.ID] = &DependencyNode{
			Migration:    migration,
			Dependencies: migration.Dependencies,
			Dependents:   make([]string, 0),
			Visited:      false,
			InStack:      false,
			Order:        -1,
		}
		graph.edges[migration.ID] = migration.Dependencies
	}

	// Build reverse dependencies (dependents)
	for migrationID, dependencies := range graph.edges {
		for _, depID := range dependencies {
			if node, exists := graph.nodes[depID]; exists {
				node.Dependents = append(node.Dependents, migrationID)
			}
		}
	}

	return graph
}

// dfsDetectCycle performs DFS to detect cycles
func (r *DefaultDependencyResolver) dfsDetectCycle(graph *DependencyGraph, nodeID string, path []string) []string {
	node := graph.nodes[nodeID]
	if node.InStack {
		// Found a cycle - return the cycle path
		cycleStart := -1
		for i, id := range path {
			if id == nodeID {
				cycleStart = i
				break
			}
		}
		if cycleStart >= 0 {
			return append(path[cycleStart:], nodeID)
		}
		return []string{nodeID}
	}

	if node.Visited {
		return nil
	}

	node.Visited = true
	node.InStack = true
	newPath := append(path, nodeID)

	// Visit dependencies
	for _, depID := range node.Dependencies {
		if cycle := r.dfsDetectCycle(graph, depID, newPath); len(cycle) > 0 {
			return cycle
		}
	}

	node.InStack = false
	return nil
}

// topologicalSort performs topological sorting using Kahn's algorithm
func (r *DefaultDependencyResolver) topologicalSort(graph *DependencyGraph) ([]string, error) {
	// Calculate in-degrees
	inDegree := make(map[string]int)
	for nodeID := range graph.nodes {
		inDegree[nodeID] = len(graph.nodes[nodeID].Dependencies)
	}

	// Find nodes with no incoming edges
	queue := make([]string, 0)
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}

	// Sort queue for deterministic ordering
	sort.Strings(queue)

	result := make([]string, 0, len(graph.nodes))

	for len(queue) > 0 {
		// Remove node with no incoming edges
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// For each dependent of current node
		node := graph.nodes[current]
		for _, dependentID := range node.Dependents {
			// Remove edge from current to dependent
			inDegree[dependentID]--

			// If dependent has no more incoming edges, add to queue
			if inDegree[dependentID] == 0 {
				queue = append(queue, dependentID)
				sort.Strings(queue) // Keep queue sorted for deterministic ordering
			}
		}
	}

	// Check if all nodes were processed (no cycles)
	if len(result) != len(graph.nodes) {
		return nil, fmt.Errorf("circular dependency detected - cannot perform topological sort")
	}

	return result, nil
}

// PreconditionResolver handles migration preconditions
type PreconditionResolver struct {
	db     interface{} // Database connection for checking preconditions
	config *ResolverConfig
}

// NewPreconditionResolver creates a new precondition resolver
func NewPreconditionResolver(db interface{}, config *ResolverConfig) *PreconditionResolver {
	return &PreconditionResolver{
		db:     db,
		config: config,
	}
}

// ValidatePreconditions validates that all preconditions are met
func (p *PreconditionResolver) ValidatePreconditions(ctx context.Context, migration *types.Migration) error {
	if len(migration.Preconditions) == 0 {
		return nil
	}

	for _, precondition := range migration.Preconditions {
		if err := p.validateSinglePrecondition(ctx, precondition.Type+":"+precondition.SQL); err != nil {
			return fmt.Errorf("precondition failed for migration %s: %w", migration.ID, err)
		}
	}

	return nil
}

// validateSinglePrecondition validates a single precondition
func (p *PreconditionResolver) validateSinglePrecondition(ctx context.Context, precondition string) error {
	// Parse precondition type and parameters
	parts := strings.SplitN(precondition, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid precondition format: %s", precondition)
	}

	preconditionType := strings.TrimSpace(parts[0])
	parameters := strings.TrimSpace(parts[1])

	switch preconditionType {
	case "table_exists":
		return p.validateTableExists(ctx, parameters)
	case "column_exists":
		return p.validateColumnExists(ctx, parameters)
	case "index_exists":
		return p.validateIndexExists(ctx, parameters)
	case "data_exists":
		return p.validateDataExists(ctx, parameters)
	case "schema_version":
		return p.validateSchemaVersion(ctx, parameters)
	default:
		return fmt.Errorf("unknown precondition type: %s", preconditionType)
	}
}

// validateTableExists checks if a table exists
func (p *PreconditionResolver) validateTableExists(ctx context.Context, tableName string) error {
	// This would need to be implemented based on the specific database type
	// For now, return nil (assume table exists)
	return nil
}

// validateColumnExists checks if a column exists in a table
func (p *PreconditionResolver) validateColumnExists(ctx context.Context, tableColumn string) error {
	// Parse table.column format
	parts := strings.SplitN(tableColumn, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid table.column format: %s", tableColumn)
	}

	// This would need to be implemented based on the specific database type
	// For now, return nil (assume column exists)
	return nil
}

// validateIndexExists checks if an index exists
func (p *PreconditionResolver) validateIndexExists(ctx context.Context, indexName string) error {
	// This would need to be implemented based on the specific database type
	// For now, return nil (assume index exists)
	return nil
}

// validateDataExists checks if specific data exists
func (p *PreconditionResolver) validateDataExists(ctx context.Context, query string) error {
	// This would need to be implemented to execute the query and check results
	// For now, return nil (assume data exists)
	return nil
}

// validateSchemaVersion checks if the schema is at a specific version
func (p *PreconditionResolver) validateSchemaVersion(ctx context.Context, version string) error {
	// This would need to be implemented to check the current schema version
	// For now, return nil (assume version matches)
	return nil
}

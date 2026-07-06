/**
 * @file changeset.go
 * @description Changeset-based migration support for XML/YAML/JSON formats (GoMigrate-style)
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/changeset/changeset.go
 */

package changeset

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ChangesetMigrationLoader handles GoMigrate changeset migrations
type ChangesetMigrationLoader struct {
	*loader.FileMigrationLoader
	changesetValidator  ChangesetValidator
	dependencyResolver  DependencyResolver
	preconditionChecker PreconditionChecker
}

// ChangesetValidator validates changeset migrations
type ChangesetValidator interface {
	ValidateChangeset(ctx context.Context, changeset *types.ChangeSet) error
	ValidateChangelog(ctx context.Context, changelog *types.Changelog) error
	ValidateDependencies(ctx context.Context, changesets []types.ChangeSet) error
}

// DependencyResolver resolves changeset dependencies
type DependencyResolver interface {
	ResolveDependencies(ctx context.Context, changesets []types.ChangeSet) ([]types.ChangeSet, error)
	BuildDependencyGraph(changesets []types.ChangeSet) (*DependencyGraph, error)
}

// PreconditionChecker checks changeset preconditions
type PreconditionChecker interface {
	CheckPreconditions(ctx context.Context, db *gorm.DB, preconditions []types.ChangePrecondition) error
	EvaluatePrecondition(ctx context.Context, db *gorm.DB, precondition types.ChangePrecondition) (bool, error)
}

// DependencyGraph represents changeset dependencies
type DependencyGraph struct {
	Nodes map[string]*DependencyNode `json:"nodes"`
	Edges []DependencyEdge           `json:"edges"`
}

// DependencyNode represents a changeset in the dependency graph
type DependencyNode struct {
	ID           string                    `json:"id"`
	Changeset    *types.ChangeSet `json:"changeset"`
	Dependencies []string                  `json:"dependencies"`
	Dependents   []string                  `json:"dependents"`
	Level        int                       `json:"level"`
	Metadata     map[string]interface{}    `json:"metadata"`
}

// DependencyEdge represents a dependency relationship
type DependencyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"` // "depends", "runBefore", "runAfter"
}

// ChangesetExecutionPlan represents an execution plan for changesets
type ChangesetExecutionPlan struct {
	ID             string                `json:"id"`
	CreatedAt      time.Time             `json:"created_at"`
	TotalChangesets int                  `json:"total_changesets"`
	Changesets     []types.ChangeSet     `json:"changesets"`
	Dependencies   map[string][]string   `json:"dependencies"`
	ExecutionOrder []string              `json:"execution_order"`
	Errors         []string              `json:"errors"`
	EstimatedTime  time.Duration         `json:"estimated_time"`
	DryRun         bool                  `json:"dry_run"`
}

// ChangesetExecutionResult represents the result of changeset execution
type ChangesetExecutionResult struct {
	ID             string                `json:"id"`
	Status         string                `json:"status"`
	ExecutedAt     time.Time             `json:"executed_at"`
	Duration       time.Duration         `json:"duration"`
	Changeset      *types.ChangeSet      `json:"changeset"`
	Error          error                 `json:"error,omitempty"`
	RollbackScript string                `json:"rollback_script,omitempty"`
	Warnings       []string              `json:"warnings,omitempty"`
}

// ChangesetMigrationConfig holds configuration for changeset migrations
type ChangesetMigrationConfig struct {
	ValidateChecksums     bool          `json:"validate_checksums"`
	AllowDuplicateIDs     bool          `json:"allow_duplicate_ids"`
	StrictDependencies    bool          `json:"strict_dependencies"`
	MaxDependencyDepth    int           `json:"max_dependency_depth"`
	PreconditionTimeout   time.Duration `json:"precondition_timeout"`
	EnableRollbackTags    bool          `json:"enable_rollback_tags"`
	AutoGenerateRollback  bool          `json:"auto_generate_rollback"`
	ValidateBeforeExecute bool          `json:"validate_before_execute"`
}

// DefaultChangesetMigrationConfig returns default configuration
func DefaultChangesetMigrationConfig() *ChangesetMigrationConfig {
	return &ChangesetMigrationConfig{
		ValidateChecksums:     true,
		AllowDuplicateIDs:     false,
		StrictDependencies:    true,
		MaxDependencyDepth:    10,
		PreconditionTimeout:   time.Minute * 2,
		EnableRollbackTags:    true,
		AutoGenerateRollback:  true,
		ValidateBeforeExecute: true,
	}
}

// NewChangesetMigrationLoader creates a new changeset migration loader
func NewChangesetMigrationLoader(config *ChangesetMigrationConfig) *ChangesetMigrationLoader {
	if config == nil {
		config = DefaultChangesetMigrationConfig()
	}

	return &ChangesetMigrationLoader{
		FileMigrationLoader: loader.NewFileMigrationLoader(),
		changesetValidator:  NewDefaultChangesetValidator(config),
		dependencyResolver:  NewDefaultDependencyResolver(config),
		preconditionChecker: NewDefaultPreconditionChecker(config),
	}
}

// LoadChangesetMigrations loads changeset-based migrations
func (l *ChangesetMigrationLoader) LoadChangesetMigrations(path string) ([]*types.Migration, error) {
	migrations, err := l.LoadMigrations(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	// Filter changeset migrations (XML, YAML, JSON)
	var changesetMigrations []*types.Migration
	for _, migration := range migrations {
		if l.isChangesetMigration(migration.FileName) {
			enhanced, err := l.enhanceChangesetMigration(migration)
			if err != nil {
				return nil, fmt.Errorf("failed to enhance changeset migration %s: %w", migration.FileName, err)
			}
			changesetMigrations = append(changesetMigrations, enhanced)
		}
	}

	return changesetMigrations, nil
}

// isChangesetMigration checks if a migration is changeset-based
func (l *ChangesetMigrationLoader) isChangesetMigration(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".xml" || ext == ".yaml" || ext == ".yml" || ext == ".json"
}

// enhanceChangesetMigration enhances a changeset migration
func (l *ChangesetMigrationLoader) enhanceChangesetMigration(migration *types.Migration) (*types.Migration, error) {
	// Parse changelog from content
	changelog, err := l.parseChangelog(migration.Script, string(migration.Type))
	if err != nil {
		return nil, fmt.Errorf("failed to parse changelog: %w", err)
	}

	// Validate changelog
	if err := l.changesetValidator.ValidateChangelog(context.Background(), changelog); err != nil {
		return nil, fmt.Errorf("changelog validation failed: %w", err)
	}

	// Extract metadata from changelog
	migration.Metadata = map[string]string{
		"changelog":    "parsed",
		"changesets":   fmt.Sprintf("%d", len(changelog.DatabaseChangeLog)),
		"has_rollback": fmt.Sprintf("%t", l.hasRollbackInfo(changelog)),
	}

	// Generate rollback if needed
	if l.changesetValidator.(*DefaultChangesetValidator).config.AutoGenerateRollback {
		rollback, err := l.generateChangesetRollback(migration, changelog)
		if err != nil {
			// Add warning to description instead of non-existent Comments field
			if migration.Description != "" {
				migration.Description += "; "
			}
			migration.Description += fmt.Sprintf("Failed to generate rollback: %v", err)
		} else {
			migration.RollbackSQL = rollback
			migration.CanRollback = true
		}
	}

	return migration, nil
}

func (l *ChangesetMigrationLoader) parseChangelog(content, format string) (*types.Changelog, error) {
	var changelog types.Changelog

	switch format {
	case "xml":
		if err := xml.Unmarshal([]byte(content), &changelog); err != nil {
			return nil, fmt.Errorf("failed to parse XML changelog: %w", err)
		}
	case "yaml", "yml":
		if err := yaml.Unmarshal([]byte(content), &changelog); err != nil {
			return nil, fmt.Errorf("failed to parse YAML changelog: %w", err)
		}
	case "json":
		if err := json.Unmarshal([]byte(content), &changelog); err != nil {
			return nil, fmt.Errorf("failed to parse JSON changelog: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	return &changelog, nil
}

func (l *ChangesetMigrationLoader) hasRollbackInfo(changelog *types.Changelog) bool {
	for _, changeset := range changelog.ChangeSets {
		if changeset.Rollback != nil {
			return true
		}
	}
	return false
}

func (l *ChangesetMigrationLoader) generateChangesetRollback(migration *types.Migration, changelog *types.Changelog) (string, error) {
	// Create rollback changelog
	rollbackChangelog := &types.Changelog{
		ChangeSets: make([]types.ChangeSet, 0, len(changelog.ChangeSets)),
	}

	// Generate rollback changesets in reverse order
	for i := len(changelog.ChangeSets) - 1; i >= 0; i-- {
		changeset := changelog.ChangeSets[i]
		rollbackChangeset := types.ChangeSet{
			ID:     fmt.Sprintf("rollback-%s", changeset.ID),
			Author: changeset.Author,
		}

		// Generate rollback changes
		for j := len(changeset.Changes) - 1; j >= 0; j-- {
			change := changeset.Changes[j]
			if rollbackChange := l.generateRollbackChange(change); rollbackChange != nil {
				rollbackChangeset.Changes = append(rollbackChangeset.Changes, *rollbackChange)
			}
		}

		if len(rollbackChangeset.Changes) > 0 {
			rollbackChangelog.ChangeSets = append(rollbackChangelog.ChangeSets, rollbackChangeset)
		}
	}

	// Marshal back to original format
	switch migration.Type {
	case types.MigrationTypeXML:
		content, err := xml.MarshalIndent(rollbackChangelog, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal rollback XML: %w", err)
		}
		return string(content), nil
	case types.MigrationTypeYAML:
		content, err := yaml.Marshal(rollbackChangelog)
		if err != nil {
			return "", fmt.Errorf("failed to marshal rollback YAML: %w", err)
		}
		return string(content), nil
	case types.MigrationTypeJSON:
		content, err := json.MarshalIndent(rollbackChangelog, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal rollback JSON: %w", err)
		}
		return string(content), nil
	default:
		return "", fmt.Errorf("unsupported migration type: %s", migration.Type)
	}
}

func (l *ChangesetMigrationLoader) generateRollbackChange(change types.Change) *types.Change {
	rollbackChange := &types.Change{
		Type: "rollback",
	}

	switch change.Type {
	case "createTable":
		rollbackChange.Type = "dropTable"
		if tableName, ok := change.Attributes["tableName"]; ok {
			rollbackChange.Attributes["tableName"] = tableName
		}
	case "addColumn":
		rollbackChange.Type = "dropColumn"
		if tableName, ok := change.Attributes["tableName"]; ok {
			rollbackChange.Attributes["tableName"] = tableName
		}
		if columnName, ok := change.Attributes["columnName"]; ok {
			rollbackChange.Attributes["columnName"] = columnName
		}
	case "createIndex":
		rollbackChange.Type = "dropIndex"
		if indexName, ok := change.Attributes["indexName"]; ok {
			rollbackChange.Attributes["indexName"] = indexName
		}
	case "insert":
		// Insert rollback is complex - would need to track inserted data
		return nil
	case "dropTable":
		// Cannot rollback drop table without schema backup
		return nil
	default:
		// Unknown change type - cannot generate automatic rollback
		return nil
	}

	return rollbackChange
}

// CreateExecutionPlan creates an execution plan for changesets
func (l *ChangesetMigrationLoader) CreateExecutionPlan(ctx context.Context, migrations []*types.Migration) (*ChangesetExecutionPlan, error) {
	plan := &ChangesetExecutionPlan{
		Changesets:     make([]types.ChangeSet, 0),
		Dependencies:   make(map[string][]string),
		EstimatedTime:  0,
		DryRun:         false,
	}

	// Parse all changeset migrations
	var allChangesets []types.ChangeSet
	for _, migration := range migrations {
		// Parse the migration script to get changesets
		changelog, err := l.parseChangelog(migration.Script, string(migration.Type))
		if err != nil {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Failed to parse migration %s: %v", migration.ID, err))
			continue
		}
		allChangesets = append(allChangesets, changelog.DatabaseChangeLog...)
	}

	// Resolve dependencies
	orderedChangesets, err := l.dependencyResolver.ResolveDependencies(ctx, allChangesets)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	// Build dependency graph
	graph, err := l.dependencyResolver.BuildDependencyGraph(orderedChangesets)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	plan.Changesets = orderedChangesets
	
	// Convert dependency graph to map format
	dependencies := make(map[string][]string)
	for nodeID, node := range graph.Nodes {
		dependencies[nodeID] = node.Dependencies
	}
	plan.Dependencies = dependencies

	// Create execution order
	for _, changeset := range orderedChangesets {
		plan.ExecutionOrder = append(plan.ExecutionOrder, changeset.ID)
	}

	return plan, nil
}

// DefaultChangesetValidator implements changeset validation
type DefaultChangesetValidator struct {
	config *ChangesetMigrationConfig
}

// NewDefaultChangesetValidator creates a new default changeset validator
func NewDefaultChangesetValidator(config *ChangesetMigrationConfig) *DefaultChangesetValidator {
	return &DefaultChangesetValidator{
		config: config,
	}
}

// ValidateChangeset validates a single changeset
func (v *DefaultChangesetValidator) ValidateChangeset(ctx context.Context, changeset *types.ChangeSet) error {
	if changeset.ID == "" {
		return fmt.Errorf("changeset ID is required")
	}

	if changeset.Author == "" {
		return fmt.Errorf("changeset author is required")
	}

	if len(changeset.Changes) == 0 {
		return fmt.Errorf("changeset must contain at least one change")
	}

	// Validate each change
	for i, change := range changeset.Changes {
		if err := v.validateChange(change); err != nil {
			return fmt.Errorf("invalid change at index %d: %w", i, err)
		}
	}

	return nil
}

func (v *DefaultChangesetValidator) ValidateChangelog(ctx context.Context, changelog *types.Changelog) error {
	if len(changelog.ChangeSets) == 0 {
		return fmt.Errorf("changelog must contain at least one changeset")
	}

	// Check for duplicate changeset IDs
	seenIDs := make(map[string]bool)
	for _, changeset := range changelog.ChangeSets {
		if seenIDs[changeset.ID] {
			return fmt.Errorf("duplicate changeset ID: %s", changeset.ID)
		}
		seenIDs[changeset.ID] = true

		// Validate each changeset
		if err := v.ValidateChangeset(ctx, &changeset); err != nil {
			return fmt.Errorf("invalid changeset %s: %w", changeset.ID, err)
		}
	}

	return nil
}

func (v *DefaultChangesetValidator) ValidateDependencies(ctx context.Context, changesets []types.ChangeSet) error {
	// Build a map of changeset IDs for quick lookup
	changesetIDs := make(map[string]bool)
	for _, changeset := range changesets {
		changesetIDs[changeset.ID] = true
	}

	// Validate dependencies
	for _, changeset := range changesets {
		for _, depID := range changeset.RunAfter {
			if !changesetIDs[depID] {
				return fmt.Errorf("changeset %s depends on non-existent changeset %s", changeset.ID, depID)
			}
		}
	}

	return nil
}

func (v *DefaultChangesetValidator) validateChange(change types.Change) error {
	if change.Type == "" {
		return fmt.Errorf("change type is required")
	}

	// Validate required attributes based on change type
	switch change.Type {
	case "createTable":
		if _, ok := change.Attributes["tableName"]; !ok {
			return fmt.Errorf("createTable requires tableName attribute")
		}
	case "addColumn":
		if _, ok := change.Attributes["tableName"]; !ok {
			return fmt.Errorf("addColumn requires tableName attribute")
		}
		if _, ok := change.Attributes["columnName"]; !ok {
			return fmt.Errorf("addColumn requires columnName attribute")
		}
	case "createIndex":
		if _, ok := change.Attributes["indexName"]; !ok {
			return fmt.Errorf("createIndex requires indexName attribute")
		}
		if _, ok := change.Attributes["tableName"]; !ok {
			return fmt.Errorf("createIndex requires tableName attribute")
		}
	}

	return nil
}

// DefaultDependencyResolver implements dependency resolution
type DefaultDependencyResolver struct {
	config *ChangesetMigrationConfig
}

// NewDefaultDependencyResolver creates a new default dependency resolver
func NewDefaultDependencyResolver(config *ChangesetMigrationConfig) *DefaultDependencyResolver {
	return &DefaultDependencyResolver{
		config: config,
	}
}

// ResolveDependencies resolves changeset dependencies and returns ordered changesets
func (r *DefaultDependencyResolver) ResolveDependencies(ctx context.Context, changesets []types.ChangeSet) ([]types.ChangeSet, error) {
	// Build dependency graph
	graph, err := r.BuildDependencyGraph(changesets)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Perform topological sort
	sorted, err := r.topologicalSort(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	return sorted, nil
}

func (r *DefaultDependencyResolver) BuildDependencyGraph(changesets []types.ChangeSet) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*DependencyNode),
	}

	// Create nodes
	for _, changeset := range changesets {
		nodeID := fmt.Sprintf("%s:%s", changeset.Author, changeset.ID)
		graph.Nodes[nodeID] = &DependencyNode{
			ID:           nodeID,
			Changeset:    &changeset,
			Dependencies: []string{}, // TODO: Extract dependencies from Context/Labels
			Dependents:   []string{},
			Metadata:     make(map[string]interface{}),
		}
	}

	// Create edges - currently disabled until dependency extraction is implemented
	// TODO: Implement dependency extraction from Context/Labels fields
	for nodeID, node := range graph.Nodes {
		for _, depID := range node.Dependencies {
			if depNode, exists := graph.Nodes[depID]; exists {
				// Add edge from dependency to dependent
				graph.Edges = append(graph.Edges, DependencyEdge{
					From: depID,
					To:   nodeID,
					Type: "depends",
				})
				// Update dependents list
				depNode.Dependents = append(depNode.Dependents, nodeID)
			}
		}
	}

	return graph, nil
}

// topologicalSort performs topological sort on the dependency graph
func (r *DefaultDependencyResolver) topologicalSort(graph *DependencyGraph) ([]types.ChangeSet, error) {
	var result []types.ChangeSet
	inDegree := make(map[string]int)

	// Calculate in-degrees
	for nodeID := range graph.Nodes {
		inDegree[nodeID] = len(graph.Nodes[nodeID].Dependencies)
	}

	// Find nodes with no dependencies
	var queue []string
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}

	// Process queue
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]

		node := graph.Nodes[nodeID]
		result = append(result, *node.Changeset)

		// Reduce in-degree of dependents
		for _, dependentID := range node.Dependents {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				queue = append(queue, dependentID)
			}
		}
	}

	// Check if all nodes were processed
	if len(result) != len(graph.Nodes) {
		return nil, fmt.Errorf("dependency resolution failed - possible circular dependency")
	}

	return result, nil
}

// DefaultPreconditionChecker implements precondition checking
type DefaultPreconditionChecker struct {
	config *ChangesetMigrationConfig
}

// NewDefaultPreconditionChecker creates a new default precondition checker
func NewDefaultPreconditionChecker(config *ChangesetMigrationConfig) *DefaultPreconditionChecker {
	return &DefaultPreconditionChecker{
		config: config,
	}
}

// CheckPreconditions checks all preconditions
func (c *DefaultPreconditionChecker) CheckPreconditions(ctx context.Context, db *gorm.DB, preconditions []types.ChangePrecondition) error {
	for _, precondition := range preconditions {
		passed, err := c.EvaluatePrecondition(ctx, db, precondition)
		if err != nil {
			return fmt.Errorf("failed to evaluate precondition: %w", err)
		}
		if !passed {
			// Extract condition type from the condition map
			conditionType := "unknown"
			for key := range precondition.Condition {
				conditionType = key
				break
			}
			return fmt.Errorf("precondition failed: %s", conditionType)
		}
	}
	return nil
}

// EvaluatePrecondition evaluates a single precondition
func (c *DefaultPreconditionChecker) EvaluatePrecondition(ctx context.Context, db *gorm.DB, precondition types.ChangePrecondition) (bool, error) {
	// Handle different precondition types based on the condition map
	for conditionType, conditionValue := range precondition.Condition {
		switch conditionType {
		case "tableExists":
			if tableMap, ok := conditionValue.(map[string]interface{}); ok {
				if tableName, exists := tableMap["tableName"].(string); exists {
					return c.checkTableExists(db, tableName)
				}
			}
		case "columnExists":
			if columnMap, ok := conditionValue.(map[string]interface{}); ok {
				tableName, _ := columnMap["tableName"].(string)
				columnName, _ := columnMap["columnName"].(string)
				if tableName != "" && columnName != "" {
					return c.checkColumnExists(db, tableName, columnName)
				}
			}
		case "indexExists":
			if indexMap, ok := conditionValue.(map[string]interface{}); ok {
				if indexName, exists := indexMap["indexName"].(string); exists {
					return c.checkIndexExists(db, indexName)
				}
			}
		case "sqlCheck":
			if sqlMap, ok := conditionValue.(map[string]interface{}); ok {
				sql, _ := sqlMap["sql"].(string)
				expectedResult, _ := sqlMap["expectedResult"].(string)
				if sql != "" {
					return c.checkSQL(db, sql, expectedResult)
				}
			}
		default:
			return false, fmt.Errorf("unsupported precondition type: %s", conditionType)
		}
	}

	return true, nil
}

// Helper methods for precondition checking
func (c *DefaultPreconditionChecker) checkTableExists(db *gorm.DB, tableName string) (bool, error) {
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = ?", tableName).Scan(&count).Error
	return count > 0, err
}

func (c *DefaultPreconditionChecker) checkColumnExists(db *gorm.DB, tableName, columnName string) (bool, error) {
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_name = ? AND column_name = ?",
		tableName, columnName).Scan(&count).Error
	return count > 0, err
}

func (c *DefaultPreconditionChecker) checkIndexExists(db *gorm.DB, indexName string) (bool, error) {
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE index_name = ?", indexName).Scan(&count).Error
	return count > 0, err
}

func (c *DefaultPreconditionChecker) checkSQL(db *gorm.DB, sql, expectedResult string) (bool, error) {
	var result string
	err := db.Raw(sql).Scan(&result).Error
	if err != nil {
		return false, err
	}
	return result == expectedResult, nil
}

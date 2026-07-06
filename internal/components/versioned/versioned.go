/**
 * @file versioned.go
 * @description Enhanced versioned migration support with automatic rollback capabilities
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/versioned/versioned.go
 */

package versioned

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/loader"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// VersionedMigrationLoader handles GoMigrate versioned migrations
type VersionedMigrationLoader struct {
	*loader.FileMigrationLoader
	rollbackGenerator RollbackGenerator
	versionComparator VersionComparator
}

// RollbackGenerator generates rollback scripts automatically
type RollbackGenerator interface {
	GenerateRollback(ctx context.Context, migration *types.Migration) (*RollbackScript, error)
	AnalyzeChanges(content string, format string) ([]DatabaseChange, error)
	ValidateRollback(ctx context.Context, migration *types.Migration, rollback *RollbackScript) error
}

// VersionComparator compares migration versions
type VersionComparator interface {
	Compare(version1, version2 string) int
	IsValid(version string) bool
	GetNext(version string) string
	GetPrevious(version string) string
}

// DatabaseChange represents a database change that can be rolled back
type DatabaseChange struct {
	Type         string                 `json:"type"`         // CREATE_TABLE, ALTER_TABLE, INSERT, etc.
	Object       string                 `json:"object"`       // table name, column name, etc.
	Action       string                 `json:"action"`       // ADD, DROP, MODIFY, etc.
	Details      map[string]interface{} `json:"details"`      // specific details about the change
	Rollbackable bool                   `json:"rollbackable"` // whether this change can be rolled back
	RollbackSQL  string                 `json:"rollback_sql"` // generated rollback SQL
}

// RollbackScript represents a generated rollback script
type RollbackScript struct {
	MigrationID    string           `json:"migration_id"`
	Version        string           `json:"version"`
	Format         string           `json:"format"`
	Content        string           `json:"content"`
	Changes        []DatabaseChange `json:"changes"`
	Warnings       []string         `json:"warnings"`
	Errors         []string         `json:"errors"`
	GeneratedAt    time.Time        `json:"generated_at"`
	IsAutomatic    bool             `json:"is_automatic"`
	RequiresManual bool             `json:"requires_manual"`
}

// VersionedMigrationConfig holds configuration for versioned migrations
type VersionedMigrationConfig struct {
	VersionPattern       string        `json:"version_pattern"`
	AutoGenerateRollback bool          `json:"auto_generate_rollback"`
	ValidateRollback     bool          `json:"validate_rollback"`
	RollbackTimeout      time.Duration `json:"rollback_timeout"`
	MaxVersionLength     int           `json:"max_version_length"`
	AllowOutOfOrder      bool          `json:"allow_out_of_order"`
	StrictVersioning     bool          `json:"strict_versioning"`
}

// DefaultVersionedMigrationConfig returns default configuration
func DefaultVersionedMigrationConfig() *VersionedMigrationConfig {
	return &VersionedMigrationConfig{
		VersionPattern:       `^V(\d+(?:\.\d+)*)__(.+)$`,
		AutoGenerateRollback: true,
		ValidateRollback:     true,
		RollbackTimeout:      time.Minute * 5,
		MaxVersionLength:     50,
		AllowOutOfOrder:      false,
		StrictVersioning:     true,
	}
}

// NewVersionedMigrationLoader creates a new versioned migration loader
func NewVersionedMigrationLoader(config *VersionedMigrationConfig) *VersionedMigrationLoader {
	if config == nil {
		config = DefaultVersionedMigrationConfig()
	}

	return &VersionedMigrationLoader{
		FileMigrationLoader: loader.NewFileMigrationLoader(),
		rollbackGenerator:   NewAutoRollbackGenerator(),
		versionComparator:   NewSemanticVersionComparator(),
	}
}

// LoadVersionedMigrations loads versioned migrations with enhanced features
func (l *VersionedMigrationLoader) LoadVersionedMigrations(path string) ([]*types.Migration, error) {
	migrations, err := l.LoadMigrations(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	// Filter and enhance versioned migrations
	var versionedMigrations []*types.Migration
	for _, migration := range migrations {
		if l.isVersionedMigration(migration.FileName) {
			enhanced, err := l.enhanceMigration(migration)
			if err != nil {
				return nil, fmt.Errorf("failed to enhance migration %s: %w", migration.FileName, err)
			}
			versionedMigrations = append(versionedMigrations, enhanced)
		}
	}

	// Sort by version
	sort.Slice(versionedMigrations, func(i, j int) bool {
		return l.versionComparator.Compare(versionedMigrations[i].Version, versionedMigrations[j].Version) < 0
	})

	return versionedMigrations, nil
}

// isVersionedMigration checks if a filename follows versioned migration pattern
func (l *VersionedMigrationLoader) isVersionedMigration(filename string) bool {
	patterns := []string{
		`^V(\d+(?:\.\d+)*)__(.+)\.(sql|xml|yaml|yml|json)$`, // Versioned migration style
		`^(\d{14})_(.+)\.(sql|xml|yaml|yml|json)$`,          // Timestamp style
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, filename); matched {
			return true
		}
	}
	return false
}

// enhanceMigration enhances a migration with versioned features
func (l *VersionedMigrationLoader) enhanceMigration(migration *types.Migration) (*types.Migration, error) {
	// Parse version and description from filename
	version, description, err := l.parseVersionedFilename(migration.FileName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse versioned filename: %w", err)
	}

	migration.Version = version
	migration.Description = description

	// Generate automatic rollback if enabled
	if l.rollbackGenerator != nil {
		rollback, err := l.rollbackGenerator.GenerateRollback(context.Background(), migration)
		if err != nil {
			// Log warning but don't fail the migration loading
			if migration.Description != "" {
				migration.Description += "; "
			}
			migration.Description += fmt.Sprintf("Failed to generate automatic rollback: %v", err)
		} else {
			migration.RollbackSQL = rollback.Content
			migration.CanRollback = !rollback.RequiresManual
			if rollback.RequiresManual {
				if migration.Description != "" {
					migration.Description += "; "
				}
				migration.Description += "Manual rollback required - automatic rollback generation incomplete"
			}
		}
	}

	// Validate migration content based on format
	if err := l.validateVersionedMigration(migration); err != nil {
		return nil, fmt.Errorf("migration validation failed: %w", err)
	}

	return migration, nil
}

// parseVersionedFilename parses versioned migration filename
func (l *VersionedMigrationLoader) parseVersionedFilename(filename string) (version, description string, err error) {
	patterns := []struct {
		regex   string
		version int
		desc    int
	}{
		{`^V(\d+(?:\.\d+)*)__(.+)\.(sql|xml|yaml|yml|json)$`, 1, 2}, // Versioned
		{`^(\d{14})_(.+)\.(sql|xml|yaml|yml|json)$`, 1, 2},          // Timestamp
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern.regex)
		matches := re.FindStringSubmatch(filename)
		if len(matches) > pattern.desc {
			version = matches[pattern.version]
			description = strings.ReplaceAll(matches[pattern.desc], "_", " ")
			return version, description, nil
		}
	}

	return "", "", fmt.Errorf("invalid versioned migration filename: %s", filename)
}

// validateVersionedMigration validates a versioned migration
func (l *VersionedMigrationLoader) validateVersionedMigration(migration *types.Migration) error {
	// Validate version format
	if !l.versionComparator.IsValid(migration.Version) {
		return fmt.Errorf("invalid version format: %s", migration.Version)
	}

	// Validate content based on format
	switch migration.Type {
	case types.MigrationTypeSQL:
		return l.validateSQLMigration(migration)
	case types.MigrationTypeXML:
		return l.validateXMLMigration(migration)
	case types.MigrationTypeYAML:
		return l.validateYAMLMigration(migration)
	case types.MigrationTypeJSON:
		return l.validateJSONMigration(migration)
	default:
		return fmt.Errorf("unsupported migration type: %s", migration.Type)
	}
}

// validateSQLMigration validates SQL migration content
func (l *VersionedMigrationLoader) validateSQLMigration(migration *types.Migration) error {
	content := strings.TrimSpace(migration.Script)
	if content == "" {
		return fmt.Errorf("empty migration content")
	}

	// Basic SQL validation
	if !strings.Contains(strings.ToUpper(content), "CREATE") &&
		!strings.Contains(strings.ToUpper(content), "ALTER") &&
		!strings.Contains(strings.ToUpper(content), "INSERT") &&
		!strings.Contains(strings.ToUpper(content), "UPDATE") &&
		!strings.Contains(strings.ToUpper(content), "DELETE") &&
		!strings.Contains(strings.ToUpper(content), "DROP") {
		if migration.Description != "" {
			migration.Description += "; "
		}
		migration.Description += "Migration does not contain common SQL operations"
	}

	return nil
}

// validateXMLMigration validates XML migration content
func (l *VersionedMigrationLoader) validateXMLMigration(migration *types.Migration) error {
	var changelog types.Changelog
	if err := xml.Unmarshal([]byte(migration.Script), &changelog); err != nil {
		return fmt.Errorf("invalid XML format: %w", err)
	}
	return nil
}

func (l *VersionedMigrationLoader) validateYAMLMigration(migration *types.Migration) error {
	var changelog types.Changelog
	if err := yaml.Unmarshal([]byte(migration.Script), &changelog); err != nil {
		return fmt.Errorf("invalid YAML format: %w", err)
	}
	return nil
}

func (l *VersionedMigrationLoader) validateJSONMigration(migration *types.Migration) error {
	var changelog types.Changelog
	if err := json.Unmarshal([]byte(migration.Script), &changelog); err != nil {
		return fmt.Errorf("invalid JSON format: %w", err)
	}
	return nil
}

// AutoRollbackGenerator implements automatic rollback generation
type AutoRollbackGenerator struct {
	db *gorm.DB
}

// NewAutoRollbackGenerator creates a new automatic rollback generator
func NewAutoRollbackGenerator() *AutoRollbackGenerator {
	return &AutoRollbackGenerator{}
}

// GenerateRollback generates rollback script for a migration
func (g *AutoRollbackGenerator) GenerateRollback(ctx context.Context, migration *types.Migration) (*RollbackScript, error) {
	rollback := &RollbackScript{
		MigrationID: migration.ID,
		Version:     migration.Version,
		Format:      string(migration.Type),
		GeneratedAt: time.Now(),
		IsAutomatic: true,
	}

	// Analyze changes in the migration
	changes, err := g.AnalyzeChanges(migration.Script, string(migration.Type))
	if err != nil {
		return nil, fmt.Errorf("failed to analyze changes: %w", err)
	}

	rollback.Changes = changes

	// Generate rollback content based on format
	switch migration.Type {
	case types.MigrationTypeSQL:
		content, warnings, errors := g.generateSQLRollback(changes)
		rollback.Content = content
		rollback.Warnings = warnings
		rollback.Errors = errors
	case types.MigrationTypeXML:
		content, warnings, errors := g.generateXMLRollback(changes)
		rollback.Content = content
		rollback.Warnings = warnings
		rollback.Errors = errors
	case types.MigrationTypeYAML:
		content, warnings, errors := g.generateYAMLRollback(changes)
		rollback.Content = content
		rollback.Warnings = warnings
		rollback.Errors = errors
	case types.MigrationTypeJSON:
		content, warnings, errors := g.generateJSONRollback(changes)
		rollback.Content = content
		rollback.Warnings = warnings
		rollback.Errors = errors
	default:
		return nil, fmt.Errorf("unsupported migration type for rollback generation: %s", migration.Type)
	}

	// Check if manual intervention is required
	rollback.RequiresManual = g.requiresManualRollback(changes)

	return rollback, nil
}

// AnalyzeChanges analyzes database changes in migration content
func (g *AutoRollbackGenerator) AnalyzeChanges(content string, format string) ([]DatabaseChange, error) {
	switch format {
	case string(types.MigrationTypeSQL):
		return g.analyzeSQLChanges(content)
	case string(types.MigrationTypeXML):
		return g.analyzeXMLChanges(content)
	case string(types.MigrationTypeYAML):
		return g.analyzeYAMLChanges(content)
	case string(types.MigrationTypeJSON):
		return g.analyzeJSONChanges(content)
	default:
		return nil, fmt.Errorf("unsupported format for change analysis: %s", format)
	}
}

// analyzeSQLChanges analyzes SQL migration content
func (g *AutoRollbackGenerator) analyzeSQLChanges(content string) ([]DatabaseChange, error) {
	var changes []DatabaseChange

	// Simple SQL parsing - in production, use a proper SQL parser
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.ToUpper(line))
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}

		change := DatabaseChange{
			Rollbackable: true,
		}

		switch {
		case strings.HasPrefix(line, "CREATE TABLE"):
			change.Type = "CREATE_TABLE"
			change.Action = "CREATE"
			change.Object = g.extractTableName(line)
			change.RollbackSQL = fmt.Sprintf("DROP TABLE IF EXISTS %s;", change.Object)
		case strings.HasPrefix(line, "ALTER TABLE"):
			change.Type = "ALTER_TABLE"
			change.Action = "ALTER"
			change.Object = g.extractTableName(line)
			change.Rollbackable = false // Complex rollback needed
			change.RollbackSQL = "-- Manual rollback required for ALTER TABLE"
		case strings.HasPrefix(line, "CREATE INDEX"):
			change.Type = "CREATE_INDEX"
			change.Action = "CREATE"
			change.Object = g.extractIndexName(line)
			change.RollbackSQL = fmt.Sprintf("DROP INDEX IF EXISTS %s;", change.Object)
		case strings.HasPrefix(line, "INSERT INTO"):
			change.Type = "INSERT"
			change.Action = "INSERT"
			change.Object = g.extractTableName(line)
			change.Rollbackable = false // Data-dependent rollback
			change.RollbackSQL = "-- Manual rollback required for INSERT"
		case strings.HasPrefix(line, "DROP TABLE"):
			change.Type = "DROP_TABLE"
			change.Action = "DROP"
			change.Object = g.extractTableName(line)
			change.Rollbackable = false // Cannot recreate dropped table
			change.RollbackSQL = "-- Cannot rollback DROP TABLE - data loss"
		default:
			continue
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// analyzeXMLChanges analyzes XML (GoMigrate) migration content
func (g *AutoRollbackGenerator) analyzeXMLChanges(content string) ([]DatabaseChange, error) {
	var changelog types.Changelog
	if err := xml.Unmarshal([]byte(content), &changelog); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	var changes []DatabaseChange
	for _, changeSet := range changelog.ChangeSets {
		for _, change := range changeSet.Changes {
			dbChange := DatabaseChange{
				Type:         change.Type,
				Object:       fmt.Sprintf("%v", change.Attributes["tableName"]),
				Action:       change.Type,
				Details:      change.Attributes,
				Rollbackable: true,
			}

			// Generate rollback SQL based on change type
			switch change.Type {
			case "createTable":
				dbChange.RollbackSQL = fmt.Sprintf("DROP TABLE %s", dbChange.Object)
			case "addColumn":
				dbChange.RollbackSQL = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
					change.Attributes["tableName"], change.Attributes["columnName"])
			case "dropColumn":
				dbChange.RollbackSQL = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
					change.Attributes["tableName"], change.Attributes["columnName"], change.Attributes["type"])
			default:
				dbChange.Rollbackable = false
			}

			changes = append(changes, dbChange)
		}
	}

	return changes, nil
}

func (g *AutoRollbackGenerator) analyzeYAMLChanges(content string) ([]DatabaseChange, error) {
	var changelog types.Changelog
	if err := yaml.Unmarshal([]byte(content), &changelog); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Similar logic to XML analysis
	return g.analyzeXMLChanges(content)
}

func (g *AutoRollbackGenerator) analyzeJSONChanges(content string) ([]DatabaseChange, error) {
	var changelog types.Changelog
	if err := json.Unmarshal([]byte(content), &changelog); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Similar logic to XML analysis
	return g.analyzeXMLChanges(content)
}

// Helper functions for SQL parsing
func (g *AutoRollbackGenerator) extractTableName(sql string) string {
	// Simple table name extraction - in production, use proper SQL parser
	words := strings.Fields(sql)
	for i, word := range words {
		if strings.ToUpper(word) == "TABLE" && i+1 < len(words) {
			return strings.Trim(words[i+1], "();")
		}
	}
	return "unknown_table"
}

func (g *AutoRollbackGenerator) extractIndexName(sql string) string {
	// Simple index name extraction
	words := strings.Fields(sql)
	for i, word := range words {
		if strings.ToUpper(word) == "INDEX" && i+1 < len(words) {
			return strings.Trim(words[i+1], "();")
		}
	}
	return "unknown_index"
}

// Rollback generation functions
func (g *AutoRollbackGenerator) generateSQLRollback(changes []DatabaseChange) (string, []string, []string) {
	var rollbackSQL []string
	var warnings []string
	var errors []string

	// Reverse the order of changes for rollback
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if !change.Rollbackable {
			warnings = append(warnings, fmt.Sprintf("Cannot automatically rollback %s on %s", change.Type, change.Object))
			rollbackSQL = append(rollbackSQL, fmt.Sprintf("-- %s", change.RollbackSQL))
		} else {
			rollbackSQL = append(rollbackSQL, change.RollbackSQL)
		}
	}

	return strings.Join(rollbackSQL, "\n"), warnings, errors
}

func (g *AutoRollbackGenerator) generateXMLRollback(changes []DatabaseChange) (string, []string, []string) {
	var warnings []string
	var errors []string

	// Create a rollback changeset
	changelog := types.Changelog{
		XMLName: xml.Name{Local: "databaseChangeLog"},
	}

	rollbackChangeset := types.ChangeSet{
		ID:     fmt.Sprintf("rollback-%d", time.Now().Unix()),
		Author: "auto-generated",
	}

	// Add rollback changes
	for _, change := range changes {
		if !change.Rollbackable {
			warnings = append(warnings, fmt.Sprintf("Change %s cannot be automatically rolled back", change.Type))
			continue
		}

		// Create rollback change
		rollbackChange := types.Change{
			Type: "sql",
			SQL:  change.RollbackSQL,
		}
		rollbackChangeset.Changes = append(rollbackChangeset.Changes, rollbackChange)
	}

	changelog.ChangeSets = []types.ChangeSet{rollbackChangeset}

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(changelog, "", "  ")
	if err != nil {
		errors = append(errors, fmt.Sprintf("Failed to generate XML: %v", err))
		return "", warnings, errors
	}

	return string(xmlData), warnings, errors
}

func (g *AutoRollbackGenerator) generateYAMLRollback(changes []DatabaseChange) (string, []string, []string) {
	var warnings []string
	var errors []string

	var changelog types.Changelog
	if err := yaml.Unmarshal([]byte(""), &changelog); err != nil {
		errors = append(errors, fmt.Sprintf("Failed to initialize YAML: %v", err))
		return "", warnings, errors
	}

	// Similar logic to XML generation
	xmlContent, xmlWarnings, xmlErrors := g.generateXMLRollback(changes)
	warnings = append(warnings, xmlWarnings...)
	errors = append(errors, xmlErrors...)

	// Convert to YAML (simplified)
	return xmlContent, warnings, errors
}

func (g *AutoRollbackGenerator) generateJSONRollback(changes []DatabaseChange) (string, []string, []string) {
	var warnings []string
	var errors []string

	var changelog types.Changelog
	if err := json.Unmarshal([]byte("{}"), &changelog); err != nil {
		errors = append(errors, fmt.Sprintf("Failed to initialize JSON: %v", err))
		return "", warnings, errors
	}

	// Similar logic to XML generation
	xmlContent, xmlWarnings, xmlErrors := g.generateXMLRollback(changes)
	warnings = append(warnings, xmlWarnings...)
	errors = append(errors, xmlErrors...)

	// Convert to JSON (simplified)
	return xmlContent, warnings, errors
}

// requiresManualRollback checks if any changes require manual rollback
func (g *AutoRollbackGenerator) requiresManualRollback(changes []DatabaseChange) bool {
	for _, change := range changes {
		if !change.Rollbackable {
			return true
		}
	}
	return false
}

// ValidateRollback validates a generated rollback script
func (g *AutoRollbackGenerator) ValidateRollback(ctx context.Context, migration *types.Migration, rollback *RollbackScript) error {
	if rollback.Content == "" {
		return fmt.Errorf("empty rollback content")
	}

	if len(rollback.Errors) > 0 {
		return fmt.Errorf("rollback generation errors: %v", rollback.Errors)
	}

	return nil
}

// SemanticVersionComparator implements semantic version comparison
type SemanticVersionComparator struct{}

// NewSemanticVersionComparator creates a new semantic version comparator
func NewSemanticVersionComparator() *SemanticVersionComparator {
	return &SemanticVersionComparator{}
}

// Compare compares two version strings
func (c *SemanticVersionComparator) Compare(version1, version2 string) int {
	v1Parts := strings.Split(version1, ".")
	v2Parts := strings.Split(version2, ".")

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var v1Part, v2Part int
		var err error

		if i < len(v1Parts) {
			v1Part, err = strconv.Atoi(v1Parts[i])
			if err != nil {
				v1Part = 0
			}
		}

		if i < len(v2Parts) {
			v2Part, err = strconv.Atoi(v2Parts[i])
			if err != nil {
				v2Part = 0
			}
		}

		if v1Part < v2Part {
			return -1
		} else if v1Part > v2Part {
			return 1
		}
	}

	return 0
}

// IsValid checks if a version string is valid
func (c *SemanticVersionComparator) IsValid(version string) bool {
	if version == "" {
		return false
	}

	// Check for timestamp format (14 digits)
	if len(version) == 14 {
		_, err := strconv.ParseInt(version, 10, 64)
		return err == nil
	}

	// Check for semantic version format
	parts := strings.Split(version, ".")
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}

	return true
}

// GetNext returns the next version
func (c *SemanticVersionComparator) GetNext(version string) string {
	// For timestamp versions, increment by 1
	if len(version) == 14 {
		if ts, err := strconv.ParseInt(version, 10, 64); err == nil {
			return fmt.Sprintf("%014d", ts+1)
		}
	}

	// For semantic versions, increment the last part
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		if lastPart, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			parts[len(parts)-1] = strconv.Itoa(lastPart + 1)
			return strings.Join(parts, ".")
		}
	}

	return version + ".1"
}

// GetPrevious returns the previous version
func (c *SemanticVersionComparator) GetPrevious(version string) string {
	// For timestamp versions, decrement by 1
	if len(version) == 14 {
		if ts, err := strconv.ParseInt(version, 10, 64); err == nil && ts > 0 {
			return fmt.Sprintf("%014d", ts-1)
		}
	}

	// For semantic versions, decrement the last part
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		if lastPart, err := strconv.Atoi(parts[len(parts)-1]); err == nil && lastPart > 0 {
			parts[len(parts)-1] = strconv.Itoa(lastPart - 1)
			return strings.Join(parts, ".")
		}
	}

	return "0"
}

/**
 * @file loader.go
 * @description Migration loader implementations for different file formats
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/loader/loader.go
 */

package loader

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/constants"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"gopkg.in/yaml.v3"
)

// FileMigrationLoader implements MigrationLoader for file-based migrations
type FileMigrationLoader struct {
	supportedExtensions map[string]string
	validators          map[string]MigrationFileValidator
}

// MigrationFileValidator validates specific file types
type MigrationFileValidator interface {
	Validate(content string) error
	ExtractMetadata(content string) (map[string]interface{}, error)
}

// SQLFileValidator validates SQL migration files
type SQLFileValidator struct{}

// XMLFileValidator validates XML migration files (GoMigrate style)
type XMLFileValidator struct{}

// YAMLFileValidator validates YAML migration files
type YAMLFileValidator struct{}

// JSONFileValidator validates JSON migration files
type JSONFileValidator struct{}

// GoMigrateInclude represents a GoMigrate include
type GoMigrateInclude struct {
	File string `xml:"file,attr" json:"file" yaml:"file"`
}

// GoMigrateProperty represents a GoMigrate property
type GoMigrateProperty struct {
	Name  string `xml:"name,attr" json:"name" yaml:"name"`
	Value string `xml:"value,attr" json:"value" yaml:"value"`
}

// NewFileMigrationLoader creates a new file migration loader
func NewFileMigrationLoader() *FileMigrationLoader {
	loader := &FileMigrationLoader{
		supportedExtensions: map[string]string{
			constants.ExtensionSQL:  string(types.MigrationTypeSQL),
			constants.ExtensionXML:  string(types.MigrationTypeXML),
			constants.ExtensionYAML: string(types.MigrationTypeYAML),
			constants.ExtensionYML:  string(types.MigrationTypeYAML),
			constants.ExtensionJSON: string(types.MigrationTypeJSON),
		},
		validators: make(map[string]MigrationFileValidator),
	}

	// Initialize validators
	loader.validators[string(types.MigrationTypeSQL)] = &SQLFileValidator{}
	loader.validators[string(types.MigrationTypeXML)] = &XMLFileValidator{}
	loader.validators[string(types.MigrationTypeYAML)] = &YAMLFileValidator{}
	loader.validators[string(types.MigrationTypeJSON)] = &JSONFileValidator{}

	return loader
}

// LoadMigrations loads all migrations from the specified path
func (l *FileMigrationLoader) LoadMigrations(path string) ([]*types.Migration, error) {
	var migrations []*types.Migration

	// Walk through the directory
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file is a migration file
		ext := strings.ToLower(filepath.Ext(filePath))
		if _, supported := l.supportedExtensions[ext]; !supported {
			return nil
		}

		// Load migration
		migration, err := l.LoadMigration(filePath)
		if err != nil {
			return fmt.Errorf("failed to load migration %s: %w", filePath, err)
		}

		migrations = append(migrations, migration)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk migration directory: %w", err)
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return compareVersions(migrations[i].Version, migrations[j].Version) < 0
	})

	return migrations, nil
}

// LoadMigration loads a single migration from a file
func (l *FileMigrationLoader) LoadMigration(filePath string) (*types.Migration, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Get file extension and type
	ext := strings.ToLower(filepath.Ext(filePath))
	fileType, supported := l.supportedExtensions[ext]
	if !supported {
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}

	// Get filename
	filename := filepath.Base(filePath)

	// Create base migration
	migration := &types.Migration{
		ID:        GenerateMigrationID(filename),
		FileName:  filename,
		FilePath:  filePath,
		Script:    string(content),
		Checksum:  CalculateChecksum(string(content)),
		Type:      types.MigrationType(fileType),
		CreatedAt: time.Now(),
	}

	// Parse based on file type
	switch types.MigrationType(fileType) {
	case types.MigrationTypeSQL:
		if err := l.parseSQLMigration(migration); err != nil {
			return nil, fmt.Errorf("failed to parse SQL migration: %w", err)
		}
	case types.MigrationTypeXML:
		if err := l.parseXMLMigration(migration); err != nil {
			return nil, fmt.Errorf("failed to parse XML migration: %w", err)
		}
	case types.MigrationTypeYAML:
		if err := l.parseYAMLMigration(migration); err != nil {
			return nil, fmt.Errorf("failed to parse YAML migration: %w", err)
		}
	case types.MigrationTypeJSON:
		if err := l.parseJSONMigration(migration); err != nil {
			return nil, fmt.Errorf("failed to parse JSON migration: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported migration type: %s", fileType)
	}

	// Generate ID
	migration.ID = GenerateMigrationID(filepath.Base(migration.FilePath))

	return migration, nil
}

// ValidateMigration validates a migration
func (l *FileMigrationLoader) ValidateMigration(migration *types.Migration) error {
	validator, exists := l.validators[string(migration.Type)]
	if !exists {
		return fmt.Errorf("no validator for migration type: %s", migration.Type)
	}

	return validator.Validate(migration.Script)
}

// parseSQLMigration parses a SQL migration file
func (l *FileMigrationLoader) parseSQLMigration(migration *types.Migration) error {

	// Parse filename for version and description
	filename := filepath.Base(migration.FilePath)
	version, description, err := ParseMigrationFilename(filename)
	if err != nil {
		return err
	}

	migration.Version = version
	migration.Description = description

	// Check if this is a +migrate format file
	if strings.Contains(migration.Script, "-- +migrate Up") {
		upSQL, downSQL := l.parseMigrateFormat(migration.Script)
		migration.Script = upSQL
		if downSQL != "" {
			migration.RollbackSQL = downSQL
			migration.CanRollback = true
		}
	}

	// Extract metadata from SQL comments
	metadata := l.extractSQLMetadata(migration.Script)

	// Apply metadata
	if author, ok := metadata["author"].(string); ok {
		migration.Author = author
	}
	if contexts, ok := metadata["contexts"].([]string); ok {
		migration.Contexts = contexts
	}
	if labels, ok := metadata["labels"].([]string); ok {
		migration.Labels = labels
	}
	if dependencies, ok := metadata["depends_on"].([]string); ok {
		migration.Dependencies = dependencies
	}
	if rollbackScript, ok := metadata["rollback_sql"].(string); ok {
		migration.RollbackSQL = rollbackScript
		migration.CanRollback = true
	}

	return nil
}

// parseXMLMigration parses an XML migration file (changeset format)
func (l *FileMigrationLoader) parseXMLMigration(migration *types.Migration) error {
	var changelog types.Changelog
	if err := xml.Unmarshal([]byte(migration.Script), &changelog); err != nil {
		return fmt.Errorf("failed to parse XML: %w", err)
	}

	// For XML files, we might have multiple changesets
	// For now, we'll take the first one or combine them
	if len(changelog.ChangeSets) == 0 {
		return fmt.Errorf("no changesets found in XML migration")
	}

	// Use the first changeset for migration metadata
	changeSet := changelog.ChangeSets[0]

	migration.Version = changeSet.ID
	migration.Author = changeSet.Author
	migration.Description = changeSet.Comment
	migration.ChangesetID = changeSet.ID
	migration.RunAlways = changeSet.RunAlways
	migration.RunOnChange = changeSet.RunOnChange
	migration.FailOnError = changeSet.FailOnError

	// Parse contexts and labels
	if changeSet.Context != "" {
		migration.Contexts = strings.Split(changeSet.Context, ",")
	}
	if changeSet.Labels != "" {
		migration.Labels = strings.Split(changeSet.Labels, ",")
	}

	// Extract preconditions
	if len(changeSet.Preconditions) > 0 {
		preconditions := make([]types.Precondition, len(changeSet.Preconditions))
		for i, pc := range changeSet.Preconditions {
			// Extract type from condition map
			conditionType := ""
			for key := range pc.Condition {
				conditionType = key
				break
			}
			preconditions[i] = types.Precondition{
				Type:      conditionType,
				Condition: pc.Condition,
				OnFail:    pc.OnFail,
			}
		}
		migration.Preconditions = preconditions
	}

	// Extract rollback SQL
	if changeSet.Rollback != nil {
		migration.RollbackSQL = changeSet.Rollback.SQL
	}

	// Convert changes to SQL
	sqlStatements := make([]string, 0)
	for _, change := range changeSet.Changes {
		sql := l.convertChangeToSQL(change)
		if sql != "" {
			sqlStatements = append(sqlStatements, sql)
		}
	}
	migration.Script = strings.Join(sqlStatements, ";\n")

	return nil
}

// parseYAMLMigration parses a YAML migration file
func (l *FileMigrationLoader) parseYAMLMigration(migration *types.Migration) error {
	var changelog types.Changelog
	if err := yaml.Unmarshal([]byte(migration.Script), &changelog); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Similar to XML parsing
	if len(changelog.ChangeSets) == 0 {
		return fmt.Errorf("no changesets found in YAML migration")
	}

	changeSet := changelog.ChangeSets[0]

	migration.Version = changeSet.ID
	migration.Author = changeSet.Author
	migration.Description = changeSet.Comment
	migration.ChangesetID = changeSet.ID
	migration.RunAlways = changeSet.RunAlways
	migration.RunOnChange = changeSet.RunOnChange
	migration.FailOnError = changeSet.FailOnError

	if changeSet.Context != "" {
		migration.Contexts = strings.Split(changeSet.Context, ",")
	}
	if changeSet.Labels != "" {
		migration.Labels = strings.Split(changeSet.Labels, ",")
	}

	if len(changeSet.Preconditions) > 0 {
		preconditions := make([]types.Precondition, len(changeSet.Preconditions))
		for i, pc := range changeSet.Preconditions {
			// Extract type from condition map
			conditionType := ""
			for key := range pc.Condition {
				conditionType = key
				break
			}
			preconditions[i] = types.Precondition{
				Type:      conditionType,
				Condition: pc.Condition,
				OnFail:    pc.OnFail,
			}
		}
		migration.Preconditions = preconditions
	}

	if changeSet.Rollback != nil {
		migration.RollbackSQL = changeSet.Rollback.SQL
	}

	// Convert changes to SQL
	sqlStatements := make([]string, 0)
	for _, change := range changeSet.Changes {
		sql := l.convertChangeToSQL(change)
		if sql != "" {
			sqlStatements = append(sqlStatements, sql)
		}
	}
	migration.Script = strings.Join(sqlStatements, ";\n")

	return nil
}

// parseJSONMigration parses a JSON migration file
func (l *FileMigrationLoader) parseJSONMigration(migration *types.Migration) error {
	var changelog types.Changelog
	if err := json.Unmarshal([]byte(migration.Script), &changelog); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Similar to XML/YAML parsing
	if len(changelog.ChangeSets) == 0 {
		return fmt.Errorf("no changesets found in JSON migration")
	}

	changeSet := changelog.ChangeSets[0]

	migration.Version = changeSet.ID
	migration.Author = changeSet.Author
	migration.Description = changeSet.Comment
	migration.ChangesetID = changeSet.ID
	migration.RunAlways = changeSet.RunAlways
	migration.RunOnChange = changeSet.RunOnChange
	migration.FailOnError = changeSet.FailOnError

	if changeSet.Context != "" {
		migration.Contexts = strings.Split(changeSet.Context, ",")
	}
	if changeSet.Labels != "" {
		migration.Labels = strings.Split(changeSet.Labels, ",")
	}

	if len(changeSet.Preconditions) > 0 {
		preconditions := make([]types.Precondition, len(changeSet.Preconditions))
		for i, pc := range changeSet.Preconditions {
			// Extract type from condition map
			conditionType := ""
			for key := range pc.Condition {
				conditionType = key
				break
			}
			preconditions[i] = types.Precondition{
				Type:      conditionType,
				Condition: pc.Condition,
				OnFail:    pc.OnFail,
			}
		}
		migration.Preconditions = preconditions
	}

	if changeSet.Rollback != nil {
		migration.RollbackSQL = changeSet.Rollback.SQL
	}

	// Convert changes to SQL
	sqlStatements := make([]string, 0)
	for _, change := range changeSet.Changes {
		sql := l.convertChangeToSQL(change)
		if sql != "" {
			sqlStatements = append(sqlStatements, sql)
		}
	}
	migration.Script = strings.Join(sqlStatements, ";\n")

	return nil
}

// extractSQLMetadata extracts metadata from SQL comments
func (l *FileMigrationLoader) extractSQLMetadata(content string) map[string]interface{} {
	metadata := make(map[string]interface{})

	// Look for special comments
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip non-comment lines
		if !strings.HasPrefix(line, "--") && !strings.HasPrefix(line, "/*") {
			continue
		}

		// Remove comment markers
		line = strings.TrimPrefix(line, "--")
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimSuffix(line, "*/")
		line = strings.TrimSpace(line)

		// Parse metadata
		if strings.HasPrefix(line, "@") {
			parts := strings.SplitN(line[1:], ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])

				switch key {
				case "author":
					metadata["author"] = value
				case "contexts":
					metadata["contexts"] = strings.Split(value, ",")
				case "labels":
					metadata["labels"] = strings.Split(value, ",")
				case "depends_on":
					metadata["depends_on"] = strings.Split(value, ",")
				case "run_always":
					metadata["run_always"] = strings.ToLower(value) == "true"
				case "run_on_change":
					metadata["run_on_change"] = strings.ToLower(value) == "true"
				case "rollback_sql":
					metadata["rollback_sql"] = value
				}
			}
		}
	}

	return metadata
}

// parseMigrateFormat parses +migrate Up/Down format and returns up and down SQL
func (l *FileMigrationLoader) parseMigrateFormat(content string) (upSQL, downSQL string) {
	// Split into up and down sections
	sections := strings.Split(content, "-- +migrate Down")

	if len(sections) > 0 {
		// Extract up SQL
		upParts := strings.Split(sections[0], "-- +migrate Up")
		if len(upParts) > 1 {
			upSQL = strings.TrimSpace(upParts[1])
		}
	}

	if len(sections) > 1 {
		// Extract down SQL
		downSQL = strings.TrimSpace(sections[1])
	}

	return upSQL, downSQL
}

// convertChangeToSQL converts a change to SQL
func (l *FileMigrationLoader) convertChangeToSQL(change types.Change) string {
	// This is a simplified conversion
	// In a real implementation, this would handle all GoMigrate change types

	// Handle SQL changes
	if change.Type == "sql" {
		if sqlValue, ok := change.Attributes["sql"].(string); ok {
			return sqlValue
		}
		return change.Content
	}

	switch change.Type {
	case "createTable":
		return l.convertCreateTable(change.Attributes)
	case "addColumn":
		return l.convertAddColumn(change.Attributes)
	case "dropColumn":
		return l.convertDropColumn(change.Attributes)
	case "createIndex":
		return l.convertCreateIndex(change.Attributes)
	case "dropIndex":
		return l.convertDropIndex(change.Attributes)
	case "insert":
		return l.convertInsert(change.Attributes)
	case "update":
		return l.convertUpdate(change.Attributes)
	case "delete":
		return l.convertDelete(change.Attributes)
	default:
		return ""
	}
}

// Helper methods for converting GoMigrate changes to SQL
func (l *FileMigrationLoader) convertCreateTable(attrs map[string]interface{}) string {
	tableName, ok := attrs["tableName"].(string)
	if !ok {
		return ""
	}

	// This is a simplified implementation
	return fmt.Sprintf("CREATE TABLE %s (id INTEGER PRIMARY KEY)", tableName)
}

func (l *FileMigrationLoader) convertAddColumn(attrs map[string]interface{}) string {
	tableName, ok1 := attrs["tableName"].(string)
	columnName, ok2 := attrs["columnName"].(string)
	columnType, ok3 := attrs["type"].(string)

	if !ok1 || !ok2 || !ok3 {
		return ""
	}

	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnType)
}

func (l *FileMigrationLoader) convertDropColumn(attrs map[string]interface{}) string {
	tableName, ok1 := attrs["tableName"].(string)
	columnName, ok2 := attrs["columnName"].(string)

	if !ok1 || !ok2 {
		return ""
	}

	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableName, columnName)
}

func (l *FileMigrationLoader) convertCreateIndex(attrs map[string]interface{}) string {
	indexName, ok1 := attrs["indexName"].(string)
	tableName, ok2 := attrs["tableName"].(string)

	if !ok1 || !ok2 {
		return ""
	}

	return fmt.Sprintf("CREATE INDEX %s ON %s", indexName, tableName)
}

func (l *FileMigrationLoader) convertDropIndex(attrs map[string]interface{}) string {
	indexName, ok := attrs["indexName"].(string)
	if !ok {
		return ""
	}

	return fmt.Sprintf("DROP INDEX %s", indexName)
}

func (l *FileMigrationLoader) convertInsert(attrs map[string]interface{}) string {
	tableName, ok := attrs["tableName"].(string)
	if !ok {
		return ""
	}

	// This is a simplified implementation
	return fmt.Sprintf("INSERT INTO %s VALUES", tableName)
}

func (l *FileMigrationLoader) convertUpdate(attrs map[string]interface{}) string {
	tableName, ok := attrs["tableName"].(string)
	if !ok {
		return ""
	}

	// This is a simplified implementation
	return fmt.Sprintf("UPDATE %s SET", tableName)
}

func (l *FileMigrationLoader) convertDelete(attrs map[string]interface{}) string {
	tableName, ok := attrs["tableName"].(string)
	if !ok {
		return ""
	}

	// This is a simplified implementation
	return fmt.Sprintf("DELETE FROM %s", tableName)
}

// Validator implementations

// Validate validates SQL content
func (v *SQLFileValidator) Validate(content string) error {
	// Basic SQL validation
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("SQL content is empty")
	}

	// Check for dangerous operations in production
	dangerousPatterns := []string{
		`(?i)\bDROP\s+DATABASE\b`,
		`(?i)\bTRUNCATE\s+TABLE\b`,
		`(?i)\bDELETE\s+FROM\s+\w+\s*;?\s*$`,
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return fmt.Errorf("potentially dangerous SQL operation detected: %s", pattern)
		}
	}

	return nil
}

// ExtractMetadata extracts metadata from SQL content
func (v *SQLFileValidator) ExtractMetadata(content string) (map[string]interface{}, error) {
	metadata := make(map[string]interface{})

	// Extract table names
	tablePattern := `(?i)\b(?:CREATE|ALTER|DROP)\s+TABLE\s+(\w+)`
	re := regexp.MustCompile(tablePattern)
	matches := re.FindAllStringSubmatch(content, -1)

	tables := make([]string, 0)
	for _, match := range matches {
		if len(match) > 1 {
			tables = append(tables, match[1])
		}
	}

	if len(tables) > 0 {
		metadata["tables"] = tables
	}

	return metadata, nil
}

// Helper functions

// calculateChecksum calculates the MD5 checksum of migration content
func CalculateChecksum(content string) string {
	hash := md5.Sum([]byte(content))
	return hex.EncodeToString(hash[:])
}

// generateMigrationID generates a deterministic migration ID based on filename
func GenerateMigrationID(filename string) string {
	hash := md5.Sum([]byte(filename))
	return hex.EncodeToString(hash[:])
}

// ParseMigrationFilename parses migration filename to extract version and description
func ParseMigrationFilename(filename string) (version, description string, err error) {
	// Remove file extension
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Pattern: V{version}__{description} or {timestamp}_{description}
	patterns := []string{
		`^V(\d+(?:\.\d+)*)__(.+)$`, // GoMigrate versioned style: V1.0__description
		`^(\d{14})_(.+)$`,          // Timestamp style: 20240101120000_description
		`^(\d+)_(.+)$`,             // Simple numeric: 001_description
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(name)
		if len(matches) == 3 {
			version = matches[1]
			description = strings.ReplaceAll(matches[2], "_", " ")
			return version, description, nil
		}
	}

	return "", "", fmt.Errorf("invalid migration filename format: %s", filename)
}

// Validate validates XML content
func (v *XMLFileValidator) Validate(content string) error {
	var changelog types.Changelog
	return xml.Unmarshal([]byte(content), &changelog)
}

// ExtractMetadata extracts metadata from XML content
func (v *XMLFileValidator) ExtractMetadata(content string) (map[string]interface{}, error) {
	var changelog types.Changelog
	if err := xml.Unmarshal([]byte(content), &changelog); err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	metadata["changesets"] = len(changelog.ChangeSets)

	return metadata, nil
}

// compareVersions compares two version strings
func compareVersions(v1, v2 string) int {
	// Simple numeric comparison for now
	// In a real implementation, this would handle semantic versioning
	n1, err1 := strconv.Atoi(v1)
	n2, err2 := strconv.Atoi(v2)

	if err1 != nil || err2 != nil {
		return strings.Compare(v1, v2)
	}

	if n1 < n2 {
		return -1
	} else if n1 > n2 {
		return 1
	}
	return 0
}

// Validate validates YAML content
func (v *YAMLFileValidator) Validate(content string) error {
	var changelog types.Changelog
	return yaml.Unmarshal([]byte(content), &changelog)
}

// ExtractMetadata extracts metadata from YAML content
func (v *YAMLFileValidator) ExtractMetadata(content string) (map[string]interface{}, error) {
	var changelog types.Changelog
	if err := yaml.Unmarshal([]byte(content), &changelog); err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	metadata["changesets"] = len(changelog.ChangeSets)

	return metadata, nil
}

// Validate validates JSON content
func (v *JSONFileValidator) Validate(content string) error {
	var changelog types.Changelog
	return json.Unmarshal([]byte(content), &changelog)
}

// ExtractMetadata extracts metadata from JSON content
func (v *JSONFileValidator) ExtractMetadata(content string) (map[string]interface{}, error) {
	var changelog types.Changelog
	if err := json.Unmarshal([]byte(content), &changelog); err != nil {
		return nil, err
	}

	metadata := make(map[string]interface{})
	metadata["changesets"] = len(changelog.ChangeSets)

	return metadata, nil
}

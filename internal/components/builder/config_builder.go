/**
 * @file config_builder.go
 * @description Configuration builder with support for multiple sources (env, YAML, Maven, properties)
 * @author Admilson B. F. Cossa
 * @date 2025-01-31
 * @path pkg/core/database/migrations/internal/components/builder/config_builder.go
 */

package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"gopkg.in/yaml.v3"
)

// ConfigBuilder provides a fluent API for building migration configuration
type ConfigBuilder struct {
	config *types.MigrationConfig
	errors []error
}

// NewConfigBuilder creates a new configuration builder with defaults
func NewConfigBuilder() *ConfigBuilder {
	return &ConfigBuilder{
		config: types.DefaultMigrationConfig(),
		errors: make([]error, 0),
	}
}

// FromDefaults starts with default configuration
func (b *ConfigBuilder) FromDefaults() *ConfigBuilder {
	b.config = types.DefaultMigrationConfig()
	return b
}

// FromYAML loads configuration from a YAML file
func (b *ConfigBuilder) FromYAML(path string) *ConfigBuilder {
	data, err := os.ReadFile(path)
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to read YAML file %s: %w", path, err))
		return b
	}

	var yamlConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to parse YAML: %w", err))
		return b
	}

	b.applyMapConfig(yamlConfig)
	return b
}

// FromJSON loads configuration from a JSON file
func (b *ConfigBuilder) FromJSON(path string) *ConfigBuilder {
	data, err := os.ReadFile(path)
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to read JSON file %s: %w", path, err))
		return b
	}

	var jsonConfig map[string]interface{}
	if err := json.Unmarshal(data, &jsonConfig); err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to parse JSON: %w", err))
		return b
	}

	b.applyMapConfig(jsonConfig)
	return b
}

// FromProperties loads configuration from a properties file
func (b *ConfigBuilder) FromProperties(path string) *ConfigBuilder {
	data, err := os.ReadFile(path)
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to read properties file %s: %w", path, err))
		return b
	}

	props := parseProperties(string(data))
	b.applyPropertiesConfig(props)
	return b
}

// FromEnv loads configuration from environment variables with a prefix
func (b *ConfigBuilder) FromEnv(prefix string) *ConfigBuilder {
	if prefix == "" {
		prefix = "MIGRATION"
	}

	envVars := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		if strings.HasPrefix(key, prefix+"_") {
			// Remove prefix and convert to config key
			configKey := strings.TrimPrefix(key, prefix+"_")
			envVars[configKey] = value
		}
	}

	b.applyEnvConfig(envVars)
	return b
}

// FromMavenPlugin loads configuration from Maven plugin format
// Expects XML-like structure converted to map[string]interface{}
func (b *ConfigBuilder) FromMavenPlugin(config map[string]interface{}) *ConfigBuilder {
	b.applyMavenConfig(config)
	return b
}

// FromPropertiesFormat loads configuration from properties format
func (b *ConfigBuilder) FromPropertiesFormat(path string) *ConfigBuilder {
	// Properties format configuration
	return b.FromProperties(path)
}

// FromMultiFormat loads configuration from multiple supported formats
func (b *ConfigBuilder) FromMultiFormat(path string) *ConfigBuilder {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return b.FromYAML(path)
	case ".json":
		return b.FromJSON(path)
	case ".properties":
		return b.FromProperties(path)
	default:
		b.errors = append(b.errors, fmt.Errorf("unsupported config format: %s", ext))
		return b
	}
}

// Fluent setters for common properties

// WithDatabase sets database configuration
func (b *ConfigBuilder) WithDatabase(driver, dsn string) *ConfigBuilder {
	// Note: Database config is in types.Config, not MigrationConfig
	// This is a placeholder - would need to extend to support full Config
	return b
}

// WithMigrationsPath sets the migrations directory path
func (b *ConfigBuilder) WithMigrationsPath(path string) *ConfigBuilder {
	b.config.MigrationsPath = path
	return b
}

// WithTableName sets the schema migrations table name
func (b *ConfigBuilder) WithTableName(name string) *ConfigBuilder {
	b.config.TableName = name
	return b
}

// WithEnvironment sets the environment (development, staging, production)
func (b *ConfigBuilder) WithEnvironment(env string) *ConfigBuilder {
	b.config.Environment = env
	return b
}

// WithStrictMode enables or disables strict validation mode
func (b *ConfigBuilder) WithStrictMode(strict bool) *ConfigBuilder {
	b.config.StrictMode = strict
	return b
}

// WithTransactions enables or disables transaction wrapping
func (b *ConfigBuilder) WithTransactions(enabled bool) *ConfigBuilder {
	b.config.UseTransactions = enabled
	return b
}

// WithDryRun enables or disables dry-run mode
func (b *ConfigBuilder) WithDryRun(enabled bool) *ConfigBuilder {
	b.config.EnableDryRun = enabled
	return b
}

// WithBackup enables or disables automatic backups
func (b *ConfigBuilder) WithBackup(enabled bool, backupPath string) *ConfigBuilder {
	b.config.EnableBackup = enabled
	if backupPath != "" {
		b.config.BackupPath = backupPath
	}
	return b
}

// WithRollback configures rollback settings
func (b *ConfigBuilder) WithRollback(enabled bool, maxDepth int) *ConfigBuilder {
	b.config.EnableRollback = enabled
	if maxDepth > 0 {
		b.config.MaxRollbackDepth = maxDepth
	}
	return b
}

// WithTimeouts sets timeout durations
func (b *ConfigBuilder) WithTimeouts(lockTimeout, commandTimeout, statementTimeout time.Duration) *ConfigBuilder {
	if lockTimeout > 0 {
		b.config.LockTimeout = lockTimeout
	}
	if commandTimeout > 0 {
		b.config.CommandTimeout = commandTimeout
	}
	if statementTimeout > 0 {
		b.config.StatementTimeout = statementTimeout
	}
	return b
}

// WithValidation configures validation settings
func (b *ConfigBuilder) WithValidation(onMigrate, onLoad, checksums bool) *ConfigBuilder {
	b.config.ValidateOnMigrate = onMigrate
	b.config.ValidateOnLoad = onLoad
	b.config.ValidateChecksums = checksums
	return b
}

// WithChangelog enables GoMigrate changelog
func (b *ConfigBuilder) WithChangelog(enabled bool, format string) *ConfigBuilder {
	b.config.EnableChangelog = enabled
	if format != "" {
		b.config.ChangelogFormat = format
	}
	return b
}

// WithExecutionMode sets execution mode (sequential, batch, parallel)
func (b *ConfigBuilder) WithExecutionMode(mode string, batchSize, concurrency int) *ConfigBuilder {
	b.config.ExecutionMode = mode
	if batchSize > 0 {
		b.config.BatchSize = batchSize
	}
	if concurrency > 0 {
		b.config.Concurrency = concurrency
	}
	return b
}

// WithOutOfOrder allows out-of-order migration execution
func (b *ConfigBuilder) WithOutOfOrder(enabled bool) *ConfigBuilder {
	b.config.OutOfOrder = enabled
	return b
}

// Build returns the configured MigrationConfig or error
func (b *ConfigBuilder) Build() (*types.MigrationConfig, error) {
	if len(b.errors) > 0 {
		return nil, fmt.Errorf("configuration build failed with %d errors: %v", len(b.errors), b.errors)
	}

	// Validate the configuration
	if err := b.validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return b.config, nil
}

// MustBuild returns the configured MigrationConfig or panics on error
// Deprecated: Use Build() instead for proper error handling
func (b *ConfigBuilder) MustBuild() *types.MigrationConfig {
	config, err := b.Build()
	if err != nil {
		// Log the error before panicking for better debugging
		fmt.Fprintf(os.Stderr, "Configuration build failed: %v\n", err)
		panic(fmt.Errorf("configuration build failed: %w", err))
	}
	return config
}

// BuildOrDefault returns the configured MigrationConfig or a default config on error
func (b *ConfigBuilder) BuildOrDefault() *types.MigrationConfig {
	config, err := b.Build()
	if err != nil {
		// Log the error and return default configuration
		fmt.Fprintf(os.Stderr, "Configuration build failed, using defaults: %v\n", err)
		return types.DefaultMigrationConfig()
	}
	return config
}

// Private helper methods

func (b *ConfigBuilder) applyMapConfig(config map[string]interface{}) {
	if v, ok := config["table_name"].(string); ok {
		b.config.TableName = v
	}
	if v, ok := config["lock_table_name"].(string); ok {
		b.config.LockTableName = v
	}
	if v, ok := config["migrations_path"].(string); ok {
		b.config.MigrationsPath = v
	}
	if v, ok := config["environment"].(string); ok {
		b.config.Environment = v
	}
	if v, ok := config["strict_mode"].(bool); ok {
		b.config.StrictMode = v
	}
	if v, ok := config["use_transactions"].(bool); ok {
		b.config.UseTransactions = v
	}
	if v, ok := config["enable_dry_run"].(bool); ok {
		b.config.EnableDryRun = v
	}
	if v, ok := config["enable_backup"].(bool); ok {
		b.config.EnableBackup = v
	}
	if v, ok := config["backup_path"].(string); ok {
		b.config.BackupPath = v
	}
	if v, ok := config["validate_on_migrate"].(bool); ok {
		b.config.ValidateOnMigrate = v
	}
	if v, ok := config["out_of_order"].(bool); ok {
		b.config.OutOfOrder = v
	}
	if v, ok := config["execution_mode"].(string); ok {
		b.config.ExecutionMode = v
	}
	if v, ok := config["batch_size"].(float64); ok {
		b.config.BatchSize = int(v)
	} else if v, ok := config["batch_size"].(int); ok {
		b.config.BatchSize = v
	}
	if v, ok := config["concurrency"].(float64); ok {
		b.config.Concurrency = int(v)
	} else if v, ok := config["concurrency"].(int); ok {
		b.config.Concurrency = v
	}

	// Handle duration fields
	if v, ok := config["lock_timeout"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			b.config.LockTimeout = d
		}
	}
	if v, ok := config["command_timeout"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			b.config.CommandTimeout = d
		}
	}
}

func (b *ConfigBuilder) applyPropertiesConfig(props map[string]string) {
	if v, ok := props["table_name"]; ok {
		b.config.TableName = v
	}
	if v, ok := props["migrations_path"]; ok {
		b.config.MigrationsPath = v
	}
	if v, ok := props["environment"]; ok {
		b.config.Environment = v
	}
	if v, ok := props["strict_mode"]; ok {
		b.config.StrictMode = parseBool(v)
	}
	if v, ok := props["use_transactions"]; ok {
		b.config.UseTransactions = parseBool(v)
	}
	if v, ok := props["enable_dry_run"]; ok {
		b.config.EnableDryRun = parseBool(v)
	}
	if v, ok := props["out_of_order"]; ok {
		b.config.OutOfOrder = parseBool(v)
	}
	if v, ok := props["execution_mode"]; ok {
		b.config.ExecutionMode = v
	}
	if v, ok := props["batch_size"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			b.config.BatchSize = i
		}
	}
	if v, ok := props["lock_timeout"]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			b.config.LockTimeout = d
		}
	}
}

func (b *ConfigBuilder) applyEnvConfig(envVars map[string]string) {
	// Environment variables use UPPER_CASE with underscores
	if v, ok := envVars["TABLE_NAME"]; ok {
		b.config.TableName = v
	}
	if v, ok := envVars["MIGRATIONS_PATH"]; ok {
		b.config.MigrationsPath = v
	}
	if v, ok := envVars["ENVIRONMENT"]; ok {
		b.config.Environment = v
	}
	if v, ok := envVars["STRICT_MODE"]; ok {
		b.config.StrictMode = parseBool(v)
	}
	if v, ok := envVars["USE_TRANSACTIONS"]; ok {
		b.config.UseTransactions = parseBool(v)
	}
	if v, ok := envVars["ENABLE_DRY_RUN"]; ok {
		b.config.EnableDryRun = parseBool(v)
	}
	if v, ok := envVars["OUT_OF_ORDER"]; ok {
		b.config.OutOfOrder = parseBool(v)
	}
	if v, ok := envVars["EXECUTION_MODE"]; ok {
		b.config.ExecutionMode = v
	}
	if v, ok := envVars["BATCH_SIZE"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			b.config.BatchSize = i
		}
	}
	if v, ok := envVars["LOCK_TIMEOUT"]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			b.config.LockTimeout = d
		}
	}
}

func (b *ConfigBuilder) applyMavenConfig(config map[string]interface{}) {
	// Maven plugin typically uses camelCase
	if v, ok := config["tableName"].(string); ok {
		b.config.TableName = v
	}
	if v, ok := config["migrationsPath"].(string); ok {
		b.config.MigrationsPath = v
	}
	if v, ok := config["environment"].(string); ok {
		b.config.Environment = v
	}
	if v, ok := config["strictMode"].(bool); ok {
		b.config.StrictMode = v
	}
	if v, ok := config["useTransactions"].(bool); ok {
		b.config.UseTransactions = v
	}
	if v, ok := config["enableDryRun"].(bool); ok {
		b.config.EnableDryRun = v
	}
	if v, ok := config["outOfOrder"].(bool); ok {
		b.config.OutOfOrder = v
	}
	if v, ok := config["executionMode"].(string); ok {
		b.config.ExecutionMode = v
	}

	// Maven plugin may also use snake_case or properties style
	b.applyMapConfig(config)
}

func (b *ConfigBuilder) validate() error {
	if b.config.MigrationsPath == "" {
		return fmt.Errorf("migrations_path is required")
	}
	if b.config.TableName == "" {
		return fmt.Errorf("table_name is required")
	}
	if b.config.ExecutionMode != "sequential" && b.config.ExecutionMode != "batch" && b.config.ExecutionMode != "parallel" {
		return fmt.Errorf("execution_mode must be one of: sequential, batch, parallel")
	}
	return nil
}

// Utility functions

func parseProperties(content string) map[string]string {
	props := make(map[string]string)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			props[key] = value
		}
	}

	return props
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes" || s == "on"
}

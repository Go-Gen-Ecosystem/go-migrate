/**
 * @file manager.go
 * @description Configuration management hierarchy with proper validation and defaults
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/config/manager.go
 */

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigSource represents the source of configuration
type ConfigSource string

const (
	ConfigSourceDefault     ConfigSource = "default"
	ConfigSourceFile        ConfigSource = "file"
	ConfigSourceEnvironment ConfigSource = "environment"
	ConfigSourceOverride    ConfigSource = "override"
)

// ConfigValue holds a configuration value with its source and metadata
type ConfigValue struct {
	Value    interface{}            `json:"value"`
	Source   ConfigSource           `json:"source"`
	Path     string                 `json:"path"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// MigrationConfig represents the complete migration configuration
type MigrationConfig struct {
	Database     DatabaseConfig     `json:"database" yaml:"database"`
	Migration    MigrationSettings  `json:"migration" yaml:"migration"`
	Backup       BackupConfig       `json:"backup" yaml:"backup"`
	Health       HealthConfig       `json:"health" yaml:"health"`
	Observability ObservabilityConfig `json:"observability" yaml:"observability"`
	Security     SecurityConfig     `json:"security" yaml:"security"`
	Performance  PerformanceConfig  `json:"performance" yaml:"performance"`
}

// DatabaseConfig holds database connection and dialect configuration
type DatabaseConfig struct {
	Type            string            `json:"type" yaml:"type" validate:"required,oneof=sqlite postgres mysql" default:"sqlite"`
	ConnectionString string           `json:"connection_string" yaml:"connection_string" validate:"required" default:":memory:"`
	MaxConnections  int               `json:"max_connections" yaml:"max_connections" default:"10"`
	MaxIdleConns    int               `json:"max_idle_conns" yaml:"max_idle_conns" default:"5"`
	ConnMaxLifetime time.Duration     `json:"conn_max_lifetime" yaml:"conn_max_lifetime" default:"1h"`
	Timeout         time.Duration     `json:"timeout" yaml:"timeout" default:"30s"`
	Options         map[string]string `json:"options,omitempty" yaml:"options,omitempty"`
}

// MigrationSettings holds migration-specific configuration
type MigrationSettings struct {
	Directory       string        `json:"directory" yaml:"directory" default:"./migrations"`
	TableName       string        `json:"table_name" yaml:"table_name" default:"schema_migrations"`
	LockTableName   string        `json:"lock_table_name" yaml:"lock_table_name" default:"schema_migrations_lock"`
	LockTimeout     time.Duration `json:"lock_timeout" yaml:"lock_timeout" default:"15m"`
	DryRun          bool          `json:"dry_run" yaml:"dry_run" default:"false"`
	AllowDirty      bool          `json:"allow_dirty" yaml:"allow_dirty" default:"false"`
	SkipValidation  bool          `json:"skip_validation" yaml:"skip_validation" default:"false"`
	Concurrent      bool          `json:"concurrent" yaml:"concurrent" default:"false"`
	BatchSize       int           `json:"batch_size" yaml:"batch_size" default:"100"`
	RetryAttempts   int           `json:"retry_attempts" yaml:"retry_attempts" default:"3"`
	RetryDelay      time.Duration `json:"retry_delay" yaml:"retry_delay" default:"1s"`
}

// BackupConfig holds backup configuration
type BackupConfig struct {
	Enabled         bool          `json:"enabled" yaml:"enabled" default:"true"`
	Directory       string        `json:"directory" yaml:"directory" default:"./backups"`
	Compression     bool          `json:"compression" yaml:"compression" default:"true"`
	IncludeData     bool          `json:"include_data" yaml:"include_data" default:"true"`
	IncludeSchema   bool          `json:"include_schema" yaml:"include_schema" default:"true"`
	RetentionDays   int           `json:"retention_days" yaml:"retention_days" default:"30"`
	MaxBackups      int           `json:"max_backups" yaml:"max_backups" default:"10"`
	MinBackups      int           `json:"min_backups" yaml:"min_backups" default:"1"`
	BackupTimeout   time.Duration `json:"backup_timeout" yaml:"backup_timeout" default:"30m"`
}

// HealthConfig holds health check configuration
type HealthConfig struct {
	Enabled                bool          `json:"enabled" yaml:"enabled" default:"true"`
	Interval               time.Duration `json:"interval" yaml:"interval" default:"30s"`
	Timeout                time.Duration `json:"timeout" yaml:"timeout" default:"10s"`
	ConnectivityTimeout    time.Duration `json:"connectivity_timeout" yaml:"connectivity_timeout" default:"5s"`
	PerformanceTimeout     time.Duration `json:"performance_timeout" yaml:"performance_timeout" default:"10s"`
	IntegrityTimeout       time.Duration `json:"integrity_timeout" yaml:"integrity_timeout" default:"15s"`
	ResourcesTimeout       time.Duration `json:"resources_timeout" yaml:"resources_timeout" default:"5s"`
	MigrationStateTimeout  time.Duration `json:"migration_state_timeout" yaml:"migration_state_timeout" default:"5s"`
	PerformanceThreshold   time.Duration `json:"performance_threshold" yaml:"performance_threshold" default:"1s"`
}

// ObservabilityConfig holds observability configuration
type ObservabilityConfig struct {
	Metrics MetricsConfig `json:"metrics" yaml:"metrics"`
	Tracing TracingConfig `json:"tracing" yaml:"tracing"`
	Logging LoggingConfig `json:"logging" yaml:"logging"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled   bool   `json:"enabled" yaml:"enabled" default:"true"`
	Endpoint  string `json:"endpoint" yaml:"endpoint" default:""`
	Namespace string `json:"namespace" yaml:"namespace" default:"migration"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// TracingConfig holds tracing configuration
type TracingConfig struct {
	Enabled     bool    `json:"enabled" yaml:"enabled" default:"false"`
	Endpoint    string  `json:"endpoint" yaml:"endpoint" default:""`
	ServiceName string  `json:"service_name" yaml:"service_name" default:"migration-service"`
	SampleRate  float64 `json:"sample_rate" yaml:"sample_rate" default:"0.1"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level      string `json:"level" yaml:"level" default:"info" validate:"oneof=debug info warn error"`
	Format     string `json:"format" yaml:"format" default:"json" validate:"oneof=json text"`
	Output     string `json:"output" yaml:"output" default:"stdout"`
	Structured bool   `json:"structured" yaml:"structured" default:"true"`
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	EncryptionKey    string `json:"encryption_key,omitempty" yaml:"encryption_key,omitempty"`
	TLSEnabled       bool   `json:"tls_enabled" yaml:"tls_enabled" default:"false"`
	TLSCertFile      string `json:"tls_cert_file,omitempty" yaml:"tls_cert_file,omitempty"`
	TLSKeyFile       string `json:"tls_key_file,omitempty" yaml:"tls_key_file,omitempty"`
	TLSCAFile        string `json:"tls_ca_file,omitempty" yaml:"tls_ca_file,omitempty"`
	ValidateSSL      bool   `json:"validate_ssl" yaml:"validate_ssl" default:"true"`
}

// PerformanceConfig holds performance tuning configuration
type PerformanceConfig struct {
	MaxConcurrentMigrations int           `json:"max_concurrent_migrations" yaml:"max_concurrent_migrations" default:"1"`
	QueryTimeout            time.Duration `json:"query_timeout" yaml:"query_timeout" default:"30s"`
	LockWaitTimeout         time.Duration `json:"lock_wait_timeout" yaml:"lock_wait_timeout" default:"10s"`
	MemoryLimit             int64         `json:"memory_limit" yaml:"memory_limit" default:"1073741824"` // 1GB
	BufferSize              int           `json:"buffer_size" yaml:"buffer_size" default:"8192"`
}

// ConfigManager manages configuration loading, validation, and hierarchy
type ConfigManager struct {
	config     *MigrationConfig
	values     map[string]*ConfigValue
	validators map[string]func(interface{}) error
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() *ConfigManager {
	cm := &ConfigManager{
		config:     &MigrationConfig{},
		values:     make(map[string]*ConfigValue),
		validators: make(map[string]func(interface{}) error),
	}
	
	// Register default validators
	cm.registerDefaultValidators()
	
	return cm
}



// LoadConfig loads configuration from multiple sources with proper hierarchy
func (cm *ConfigManager) LoadConfig(configPaths ...string) error {
	// 1. Load defaults
	if err := cm.loadDefaults(); err != nil {
		return fmt.Errorf("failed to load defaults: %w", err)
	}
	
	// 2. Load from configuration files
	for _, path := range configPaths {
		if err := cm.loadFromFile(path); err != nil {
			return fmt.Errorf("failed to load config from %s: %w", path, err)
		}
	}
	
	// 3. Load from environment variables
	if err := cm.loadFromEnvironment(); err != nil {
		return fmt.Errorf("failed to load from environment: %w", err)
	}
	
	// 4. Validate final configuration
	if err := cm.validateConfig(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	
	return nil
}

// loadDefaults sets default values for all configuration fields
func (cm *ConfigManager) loadDefaults() error {
	cm.setDefaults(reflect.ValueOf(cm.config).Elem(), "")
	return nil
}

// setDefaults recursively sets default values using struct tags
func (cm *ConfigManager) setDefaults(v reflect.Value, prefix string) {
	t := v.Type()
	
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		
		// Build field path
		fieldPath := fieldType.Name
		if prefix != "" {
			fieldPath = prefix + "." + fieldPath
		}
		
		// Handle nested structs
		if field.Kind() == reflect.Struct {
			cm.setDefaults(field, fieldPath)
			continue
		}
		
		// Get default value from tag
		defaultTag := fieldType.Tag.Get("default")
		if defaultTag == "" {
			continue
		}
		
		// Set default value
		if err := cm.setFieldValue(field, defaultTag); err == nil {
			cm.values[fieldPath] = &ConfigValue{
				Value:  field.Interface(),
				Source: ConfigSourceDefault,
				Path:   fieldPath,
			}
		}
	}
}

// setFieldValue sets a field value from string
func (cm *ConfigManager) setFieldValue(field reflect.Value, value string) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}
	
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		if b, err := strconv.ParseBool(value); err == nil {
			field.SetBool(b)
		} else {
			return err
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			if d, err := time.ParseDuration(value); err == nil {
				field.SetInt(int64(d))
			} else {
				return err
			}
		} else {
			if i, err := strconv.ParseInt(value, 10, 64); err == nil {
				field.SetInt(i)
			} else {
				return err
			}
		}
	case reflect.Float32, reflect.Float64:
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			field.SetFloat(f)
		} else {
			return err
		}
	default:
		return fmt.Errorf("unsupported field type: %v", field.Kind())
	}
	
	return nil
}

// loadFromFile loads configuration from a file (JSON or YAML)
func (cm *ConfigManager) loadFromFile(path string) error {
	if path == "" {
		return nil
	}
	
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // File doesn't exist, skip
	}
	
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	var fileConfig MigrationConfig
	
	// Determine file format by extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &fileConfig); err != nil {
			return fmt.Errorf("failed to parse JSON config: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &fileConfig); err != nil {
			return fmt.Errorf("failed to parse YAML config: %w", err)
		}
	default:
		return fmt.Errorf("unsupported config file format: %s", ext)
	}
	
	// Merge file config into main config
	cm.mergeConfig(&fileConfig, ConfigSourceFile, path)
	
	return nil
}

// loadFromEnvironment loads configuration from environment variables
func (cm *ConfigManager) loadFromEnvironment() error {
	cm.loadEnvVars(reflect.ValueOf(cm.config).Elem(), "MIGRATION")
	return nil
}

// loadEnvVars recursively loads environment variables
func (cm *ConfigManager) loadEnvVars(v reflect.Value, prefix string) {
	t := v.Type()
	
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		
		// Build environment variable name - convert CamelCase to SNAKE_CASE
		envName := prefix + "_" + cm.camelToSnakeCase(fieldType.Name)
		
		// Handle nested structs
		if field.Kind() == reflect.Struct {
			cm.loadEnvVars(field, envName)
			continue
		}
		
		// Check for environment variable
		if envValue := os.Getenv(envName); envValue != "" {
			if !field.CanSet() {
				continue
			}
			
			if err := cm.setFieldValue(field, envValue); err == nil {
				fieldPath := cm.getFieldPath(fieldType, prefix)
				cm.values[fieldPath] = &ConfigValue{
					Value:  field.Interface(),
					Source: ConfigSourceEnvironment,
					Path:   envName,
				}
			}
		}
	}
}

// camelToSnakeCase converts CamelCase to SNAKE_CASE
func (cm *ConfigManager) camelToSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToUpper(result.String())
}

// getFieldPath builds the field path for tracking
func (cm *ConfigManager) getFieldPath(field reflect.StructField, prefix string) string {
	parts := strings.Split(strings.ToLower(prefix), "_")
	if len(parts) > 1 {
		parts = parts[1:] // Remove "MIGRATION" prefix
	}
	parts = append(parts, field.Name)
	return strings.Join(parts, ".")
}

// mergeConfig merges configuration from a source into the main config
func (cm *ConfigManager) mergeConfig(source *MigrationConfig, configSource ConfigSource, path string) {
	cm.mergeStruct(reflect.ValueOf(cm.config).Elem(), reflect.ValueOf(source).Elem(), "", configSource, path)
}

// mergeStruct recursively merges struct values
func (cm *ConfigManager) mergeStruct(dest, src reflect.Value, prefix string, source ConfigSource, path string) {
	t := dest.Type()
	
	for i := 0; i < dest.NumField(); i++ {
		destField := dest.Field(i)
		srcField := src.Field(i)
		fieldType := t.Field(i)
		
		// Build field path
		fieldPath := fieldType.Name
		if prefix != "" {
			fieldPath = prefix + "." + fieldPath
		}
		
		// Handle nested structs
		if destField.Kind() == reflect.Struct {
			cm.mergeStruct(destField, srcField, fieldPath, source, path)
			continue
		}
		
		// Check if source field should be merged
		// For boolean fields, we need to merge even if the value is false (zero value)
		shouldMerge := false
		if srcField.Kind() == reflect.Bool {
			// For booleans, always merge if the field can be set
			shouldMerge = destField.CanSet()
		} else {
			// For other types, only merge non-zero values
			shouldMerge = !srcField.IsZero() && destField.CanSet()
		}
		
		if shouldMerge {
			destField.Set(srcField)
			cm.values[fieldPath] = &ConfigValue{
				Value:  destField.Interface(),
				Source: source,
				Path:   path,
			}
		}
	}
}

// validateConfig validates the final configuration
func (cm *ConfigManager) validateConfig() error {
	return cm.validateStruct(reflect.ValueOf(cm.config).Elem(), "")
}

// validateStruct recursively validates struct fields
func (cm *ConfigManager) validateStruct(v reflect.Value, prefix string) error {
	t := v.Type()
	
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		
		// Build field path
		fieldPath := fieldType.Name
		if prefix != "" {
			fieldPath = prefix + "." + fieldPath
		}
		
		// Handle nested structs
		if field.Kind() == reflect.Struct {
			if err := cm.validateStruct(field, fieldPath); err != nil {
				return err
			}
			continue
		}
		
		// Check required fields
		if validateTag := fieldType.Tag.Get("validate"); validateTag != "" {
			if err := cm.validateField(field, validateTag, fieldPath); err != nil {
				return err
			}
		}
		
		// Run custom validators
		if validator, exists := cm.validators[fieldPath]; exists {
			if err := validator(field.Interface()); err != nil {
				return fmt.Errorf("validation failed for %s: %w", fieldPath, err)
			}
		}
	}
	
	return nil
}

// validateField validates a single field based on validation tags
func (cm *ConfigManager) validateField(field reflect.Value, validateTag, fieldPath string) error {
	rules := strings.Split(validateTag, ",")
	
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		
		switch {
		case rule == "required":
			if field.IsZero() {
				return fmt.Errorf("field %s is required", fieldPath)
			}
		case strings.HasPrefix(rule, "oneof="):
			values := strings.Split(rule[6:], " ")
			fieldStr := fmt.Sprintf("%v", field.Interface())
			valid := false
			for _, value := range values {
				if fieldStr == value {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("field %s must be one of: %s", fieldPath, strings.Join(values, ", "))
			}
		}
	}
	
	return nil
}

// registerDefaultValidators registers built-in validators
func (cm *ConfigManager) registerDefaultValidators() {
	// Database type validator
	cm.validators["Database.Type"] = func(value interface{}) error {
		dbType, ok := value.(string)
		if !ok {
			return fmt.Errorf("database type must be a string")
		}
		validTypes := []string{"sqlite", "postgres", "mysql"}
		for _, valid := range validTypes {
			if dbType == valid {
				return nil
			}
		}
		return fmt.Errorf("unsupported database type: %s", dbType)
	}
	
	// Connection string validator
	cm.validators["Database.ConnectionString"] = func(value interface{}) error {
		connStr, ok := value.(string)
		if !ok || connStr == "" {
			return fmt.Errorf("connection string cannot be empty")
		}
		return nil
	}
	
	// Directory validators
	cm.validators["Migration.Directory"] = func(value interface{}) error {
		dir, ok := value.(string)
		if !ok || dir == "" {
			return fmt.Errorf("migration directory cannot be empty")
		}
		return nil
	}
	
	cm.validators["Backup.Directory"] = func(value interface{}) error {
		dir, ok := value.(string)
		if !ok || dir == "" {
			return fmt.Errorf("backup directory cannot be empty")
		}
		return nil
	}
}

// RegisterValidator registers a custom validator for a field path
func (cm *ConfigManager) RegisterValidator(fieldPath string, validator func(interface{}) error) {
	cm.validators[fieldPath] = validator
}

// GetConfig returns the loaded configuration
func (cm *ConfigManager) GetConfig() *MigrationConfig {
	return cm.config
}

// GetConfigValue returns the configuration value with metadata
func (cm *ConfigManager) GetConfigValue(path string) (*ConfigValue, bool) {
	value, exists := cm.values[path]
	return value, exists
}

// SetOverride sets an override value for a configuration field
func (cm *ConfigManager) SetOverride(path string, value interface{}) error {
	// Find the field by path
	field, err := cm.getFieldByPath(path)
	if err != nil {
		return err
	}
	
	// Set the value
	if err := cm.setFieldValueFromInterface(field, value); err != nil {
		return err
	}
	
	// Track the override
	cm.values[path] = &ConfigValue{
		Value:  value,
		Source: ConfigSourceOverride,
		Path:   path,
	}
	
	return nil
}

// getFieldByPath finds a field by its dot-separated path
func (cm *ConfigManager) getFieldByPath(path string) (reflect.Value, error) {
	parts := strings.Split(path, ".")
	current := reflect.ValueOf(cm.config).Elem()
	
	for _, part := range parts {
		field := current.FieldByName(part)
		if !field.IsValid() {
			return reflect.Value{}, fmt.Errorf("field not found: %s", path)
		}
		current = field
	}
	
	return current, nil
}

// setFieldValueFromInterface sets a field value from an interface{}
func (cm *ConfigManager) setFieldValueFromInterface(field reflect.Value, value interface{}) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}
	
	valueReflect := reflect.ValueOf(value)
	if !valueReflect.Type().ConvertibleTo(field.Type()) {
		return fmt.Errorf("value type %v not convertible to field type %v", valueReflect.Type(), field.Type())
	}
	
	field.Set(valueReflect.Convert(field.Type()))
	return nil
}

// GetConfigSummary returns a summary of configuration sources
func (cm *ConfigManager) GetConfigSummary() map[string]ConfigValue {
	summary := make(map[string]ConfigValue)
	for path, value := range cm.values {
		summary[path] = *value
	}
	return summary
}

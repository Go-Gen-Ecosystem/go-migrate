/**
 * @file configuration_facade.go
 * @description Configuration facade implementation for managing application configuration with validation and environment support
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/facade/configuration_facade.go
 */

package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultConfigurationFacade implements ConfigurationFacade interface
type DefaultConfigurationFacade struct {
	config         *ConfigurationConfig
	configData     map[string]interface{}
	environments   map[string]*EnvironmentConfig
	validators     map[string]ConfigValidator
	watchers       map[string]ConfigWatcher
	mutex          sync.RWMutex
	loaded         bool
	lastModified   time.Time
	configSources  []ConfigSource
	logger         Logger
}

// ConfigurationConfig configuration for the configuration facade
type ConfigurationConfig struct {
	ConfigPaths        []string          `json:"config_paths"`
	Environment        string            `json:"environment"`
	DefaultEnvironment string            `json:"default_environment"`
	AutoReload         bool              `json:"auto_reload"`
	ReloadInterval     time.Duration     `json:"reload_interval"`
	ValidateOnLoad     bool              `json:"validate_on_load"`
	EncryptionKey      string            `json:"encryption_key"`
	BackupOnChange     bool              `json:"backup_on_change"`
	BackupPath         string            `json:"backup_path"`
	FileFormat         string            `json:"file_format"` // json, yaml, toml
	EnvPrefix          string            `json:"env_prefix"`
	CaseSensitive      bool              `json:"case_sensitive"`
	AllowOverrides     bool              `json:"allow_overrides"`
	Variables          map[string]string `json:"variables"`
}

// EnvironmentConfig configuration for specific environment
type EnvironmentConfig struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Variables   map[string]interface{} `json:"variables"`
	Overrides   map[string]interface{} `json:"overrides"`
	Secrets     map[string]string      `json:"secrets"`
	Active      bool                   `json:"active"`
	Priority    int                    `json:"priority"`
}

// ConfigSource represents a configuration source
type ConfigSource interface {
	Load(ctx context.Context) (map[string]interface{}, error)
	Save(ctx context.Context, data map[string]interface{}) error
	Watch(ctx context.Context, callback func(map[string]interface{})) error
	GetLastModified() time.Time
	GetSourceType() string
	GetSourcePath() string
}

// ConfigValidator validates configuration values
type ConfigValidator interface {
	Validate(key string, value interface{}) error
	GetValidationRules() map[string]ValidationRule
}

// ConfigWatcher watches for configuration changes
type ConfigWatcher interface {
	Start(ctx context.Context) error
	Stop() error
	OnChange(callback func(key string, oldValue, newValue interface{}))
}

// ValidationRule defines validation rules for configuration values
type ValidationRule struct {
	Required    bool        `json:"required"`
	Type        string      `json:"type"`
	MinValue    interface{} `json:"min_value"`
	MaxValue    interface{} `json:"max_value"`
	AllowedValues []interface{} `json:"allowed_values"`
	Pattern     string      `json:"pattern"`
	Custom      func(interface{}) error `json:"-"`
}

// NewDefaultConfigurationFacade creates a new configuration facade instance
func NewDefaultConfigurationFacade(config *ConfigurationConfig) (*DefaultConfigurationFacade, error) {
	if config == nil {
		config = DefaultConfigurationConfig()
	}

	facade := &DefaultConfigurationFacade{
		config:        config,
		configData:    make(map[string]interface{}),
		environments:  make(map[string]*EnvironmentConfig),
		validators:    make(map[string]ConfigValidator),
		watchers:      make(map[string]ConfigWatcher),
		configSources: make([]ConfigSource, 0),
	}

	// Initialize configuration sources
	if err := facade.initializeConfigSources(); err != nil {
		return nil, fmt.Errorf("failed to initialize config sources: %w", err)
	}

	return facade, nil
}

// LoadConfiguration loads configuration from all sources
func (f *DefaultConfigurationFacade) LoadConfiguration(ctx context.Context) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// Load from all sources
	mergedConfig := make(map[string]interface{})

	for _, source := range f.configSources {
		sourceData, err := source.Load(ctx)
		if err != nil {
			f.logger.Warn(fmt.Sprintf("Failed to load from source %s: %v", source.GetSourcePath(), err))
			continue
		}

		// Merge configuration data
		f.mergeConfigData(mergedConfig, sourceData)
	}

	// Load environment variables
	envData := f.loadEnvironmentVariables()
	f.mergeConfigData(mergedConfig, envData)

	// Apply environment-specific overrides
	if envConfig, exists := f.environments[f.config.Environment]; exists {
		f.mergeConfigData(mergedConfig, envConfig.Overrides)
	}

	// Validate configuration if enabled
	if f.config.ValidateOnLoad {
		if err := f.validateConfiguration(mergedConfig); err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	// Apply variable substitution
	f.applyVariableSubstitution(mergedConfig)

	f.configData = mergedConfig
	f.loaded = true
	f.lastModified = time.Now()

	return nil
}

// SaveConfiguration saves configuration to primary source
func (f *DefaultConfigurationFacade) SaveConfiguration(ctx context.Context) error {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	if !f.loaded {
		return fmt.Errorf("configuration not loaded")
	}

	// Create backup if enabled
	if f.config.BackupOnChange {
		if err := f.createBackup(ctx); err != nil {
			f.logger.Warn(fmt.Sprintf("Failed to create backup: %v", err))
		}
	}

	// Save to primary source
	if len(f.configSources) > 0 {
		primarySource := f.configSources[0]
		if err := primarySource.Save(ctx, f.configData); err != nil {
			return fmt.Errorf("failed to save configuration: %w", err)
		}
	}

	return nil
}

// GetValue retrieves a configuration value
func (f *DefaultConfigurationFacade) GetValue(key string) (interface{}, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	if !f.loaded {
		return nil, fmt.Errorf("configuration not loaded")
	}

	// Handle nested keys (e.g., "database.host")
	keys := strings.Split(key, ".")
	current := f.configData

	for i, k := range keys {
		if !f.config.CaseSensitive {
			k = strings.ToLower(k)
		}

		if i == len(keys)-1 {
			// Last key, return the value
			if value, exists := current[k]; exists {
				return value, nil
			}
			return nil, fmt.Errorf("configuration key not found: %s", key)
		}

		// Navigate to next level
		if nextLevel, exists := current[k]; exists {
			if nextMap, ok := nextLevel.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return nil, fmt.Errorf("invalid configuration structure at key: %s", strings.Join(keys[:i+1], "."))
			}
		} else {
			return nil, fmt.Errorf("configuration key not found: %s", key)
		}
	}

	return nil, fmt.Errorf("configuration key not found: %s", key)
}

// SetValue sets a configuration value
func (f *DefaultConfigurationFacade) SetValue(key string, value interface{}) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if !f.loaded {
		return fmt.Errorf("configuration not loaded")
	}

	// Validate value if validator exists
	if validator, exists := f.validators[key]; exists {
		if err := validator.Validate(key, value); err != nil {
			return fmt.Errorf("validation failed for key %s: %w", key, err)
		}
	}

	// Handle nested keys
	keys := strings.Split(key, ".")
	current := f.configData

	for i, k := range keys {
		if !f.config.CaseSensitive {
			k = strings.ToLower(k)
		}

		if i == len(keys)-1 {
			// Last key, set the value
			oldValue := current[k]
			current[k] = value

			// Notify watchers
			f.notifyWatchers(key, oldValue, value)
			return nil
		}

		// Navigate or create next level
		if nextLevel, exists := current[k]; exists {
			if nextMap, ok := nextLevel.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return fmt.Errorf("cannot set value: existing value at %s is not a map", strings.Join(keys[:i+1], "."))
			}
		} else {
			// Create new map
			newMap := make(map[string]interface{})
			current[k] = newMap
			current = newMap
		}
	}

	return nil
}

// GetString retrieves a string configuration value
func (f *DefaultConfigurationFacade) GetString(key string) (string, error) {
	value, err := f.GetValue(key)
	if err != nil {
		return "", err
	}

	if str, ok := value.(string); ok {
		return str, nil
	}

	return fmt.Sprintf("%v", value), nil
}

// GetInt retrieves an integer configuration value
func (f *DefaultConfigurationFacade) GetInt(key string) (int, error) {
	value, err := f.GetValue(key)
	if err != nil {
		return 0, err
	}

	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		return strconv.Atoi(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to int", value)
	}
}

// GetBool retrieves a boolean configuration value
func (f *DefaultConfigurationFacade) GetBool(key string) (bool, error) {
	value, err := f.GetValue(key)
	if err != nil {
		return false, err
	}

	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(v)
	default:
		return false, fmt.Errorf("cannot convert %T to bool", value)
	}
}

// GetDuration retrieves a duration configuration value
func (f *DefaultConfigurationFacade) GetDuration(key string) (time.Duration, error) {
	value, err := f.GetValue(key)
	if err != nil {
		return 0, err
	}

	switch v := value.(type) {
	case time.Duration:
		return v, nil
	case string:
		return time.ParseDuration(v)
	case int64:
		return time.Duration(v), nil
	case float64:
		return time.Duration(v), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to duration", value)
	}
}

// GetStringSlice retrieves a string slice configuration value
func (f *DefaultConfigurationFacade) GetStringSlice(key string) ([]string, error) {
	value, err := f.GetValue(key)
	if err != nil {
		return nil, err
	}

	switch v := value.(type) {
	case []string:
		return v, nil
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result, nil
	case string:
		// Split by comma
		return strings.Split(v, ","), nil
	default:
		return nil, fmt.Errorf("cannot convert %T to []string", value)
	}
}

// HasKey checks if a configuration key exists
func (f *DefaultConfigurationFacade) HasKey(key string) bool {
	_, err := f.GetValue(key)
	return err == nil
}

// GetAllKeys returns all configuration keys
func (f *DefaultConfigurationFacade) GetAllKeys() []string {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	return f.getAllKeysRecursive("", f.configData)
}

// GetEnvironment returns current environment configuration
func (f *DefaultConfigurationFacade) GetEnvironment() (*EnvironmentConfig, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	if env, exists := f.environments[f.config.Environment]; exists {
		return env, nil
	}

	return nil, fmt.Errorf("environment not found: %s", f.config.Environment)
}

// SetEnvironment sets the current environment
func (f *DefaultConfigurationFacade) SetEnvironment(name string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if _, exists := f.environments[name]; !exists {
		return fmt.Errorf("environment not found: %s", name)
	}

	f.config.Environment = name
	return nil
}

// AddValidator adds a configuration validator
func (f *DefaultConfigurationFacade) AddValidator(key string, validator ConfigValidator) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.validators[key] = validator
}

// AddWatcher adds a configuration watcher
func (f *DefaultConfigurationFacade) AddWatcher(key string, watcher ConfigWatcher) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.watchers[key] = watcher
}

// ValidateConfiguration validates the entire configuration
func (f *DefaultConfigurationFacade) ValidateConfiguration() (*ValidationResult, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	result := &ValidationResult{
		Valid:    true,
		IsValid:  true,
		Errors:   make([]ValidationError, 0),
		Warnings: make([]ValidationWarning, 0),
	}

	// Validate using registered validators
	for key, validator := range f.validators {
		if value, err := f.GetValue(key); err == nil {
			if validationErr := validator.Validate(key, value); validationErr != nil {
				result.Valid = false
				result.IsValid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:     key,
					Message: validationErr.Error(),
				})
			}
		} else {
			// Check if key is required
			rules := validator.GetValidationRules()
			if rule, exists := rules[key]; exists && rule.Required {
				result.Valid = false
				result.IsValid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:     key,
					Message: "required configuration key is missing",
				})
			}
		}
	}

	return result, nil
}

// ReloadConfiguration reloads configuration from sources
func (f *DefaultConfigurationFacade) ReloadConfiguration(ctx context.Context) error {
	return f.LoadConfiguration(ctx)
}

// GetConfigurationInfo returns configuration information
func (f *DefaultConfigurationFacade) GetConfigurationInfo() (*ConfigurationInfo, error) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	sources := make([]ConfigSourceInfo, len(f.configSources))
	for i, source := range f.configSources {
		sources[i] = ConfigSourceInfo{
			Type:         source.GetSourceType(),
			Path:         source.GetSourcePath(),
			LastModified: source.GetLastModified(),
		}
	}

	environments := make([]string, 0, len(f.environments))
	for name := range f.environments {
		environments = append(environments, name)
	}

	info := &ConfigurationInfo{
		Loaded:            f.loaded,
		LastModified:      f.lastModified,
		CurrentEnvironment: f.config.Environment,
		Environments:      environments,
		Sources:           sources,
		KeyCount:          len(f.GetAllKeys()),
		ValidatorCount:    len(f.validators),
		WatcherCount:      len(f.watchers),
	}

	return info, nil
}

// Private helper methods

func (f *DefaultConfigurationFacade) initializeConfigSources() error {
	for _, configPath := range f.config.ConfigPaths {
		source, err := f.createConfigSource(configPath)
		if err != nil {
			f.logger.Warn(fmt.Sprintf("Failed to create config source for %s: %v", configPath, err))
			continue
		}
		f.configSources = append(f.configSources, source)
	}

	if len(f.configSources) == 0 {
		return fmt.Errorf("no valid configuration sources found")
	}

	return nil
}

func (f *DefaultConfigurationFacade) createConfigSource(path string) (ConfigSource, error) {
	// Determine source type based on path
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return NewHTTPConfigSource(path), nil
	}

	if strings.HasPrefix(path, "env://") {
		return NewEnvironmentConfigSource(f.config.EnvPrefix), nil
	}

	// File-based source
	return NewFileConfigSource(path, f.config.FileFormat), nil
}

func (f *DefaultConfigurationFacade) mergeConfigData(target, source map[string]interface{}) {
	for key, value := range source {
		if !f.config.CaseSensitive {
			key = strings.ToLower(key)
		}

		existingValue, exists := target[key]
		if exists {
			// If both values are maps, merge recursively
			if existingMap, ok := existingValue.(map[string]interface{}); ok {
				if sourceMap, ok := value.(map[string]interface{}); ok {
					f.mergeConfigData(existingMap, sourceMap)
					continue
				}
			}
		}

		// Override or set new value
		if f.config.AllowOverrides || !exists {
			target[key] = value
		}
	}
}

func (f *DefaultConfigurationFacade) loadEnvironmentVariables() map[string]interface{} {
	envData := make(map[string]interface{})
	
	if f.config.EnvPrefix == "" {
		return envData
	}

	prefix := f.config.EnvPrefix + "_"
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		// Remove prefix and convert to config key
		configKey := strings.TrimPrefix(key, prefix)
		configKey = strings.ToLower(configKey)
		configKey = strings.ReplaceAll(configKey, "_", ".")

		envData[configKey] = value
	}

	return envData
}

func (f *DefaultConfigurationFacade) applyVariableSubstitution(data map[string]interface{}) {
	f.applyVariableSubstitutionRecursive(data, f.config.Variables)
}

func (f *DefaultConfigurationFacade) applyVariableSubstitutionRecursive(data map[string]interface{}, variables map[string]string) {
	for key, value := range data {
		switch v := value.(type) {
		case string:
			data[key] = f.substituteVariables(v, variables)
		case map[string]interface{}:
			f.applyVariableSubstitutionRecursive(v, variables)
		case []interface{}:
			for i, item := range v {
				if str, ok := item.(string); ok {
					v[i] = f.substituteVariables(str, variables)
				}
			}
		}
	}
}

func (f *DefaultConfigurationFacade) substituteVariables(value string, variables map[string]string) string {
	result := value
	for varName, varValue := range variables {
		placeholder := fmt.Sprintf("${%s}", varName)
		result = strings.ReplaceAll(result, placeholder, varValue)
	}
	return result
}

func (f *DefaultConfigurationFacade) validateConfiguration(data map[string]interface{}) error {
	// Basic validation logic
	return nil
}

func (f *DefaultConfigurationFacade) createBackup(ctx context.Context) error {
	if f.config.BackupPath == "" {
		return fmt.Errorf("backup path not configured")
	}

	backupFile := filepath.Join(f.config.BackupPath, fmt.Sprintf("config_backup_%d.json", time.Now().Unix()))
	
	data, err := json.MarshalIndent(f.configData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config data: %w", err)
	}

	return os.WriteFile(backupFile, data, 0644)
}

func (f *DefaultConfigurationFacade) getAllKeysRecursive(prefix string, data map[string]interface{}) []string {
	keys := make([]string, 0)
	
	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		keys = append(keys, fullKey)

		if subMap, ok := value.(map[string]interface{}); ok {
			subKeys := f.getAllKeysRecursive(fullKey, subMap)
			keys = append(keys, subKeys...)
		}
	}

	return keys
}

func (f *DefaultConfigurationFacade) notifyWatchers(key string, oldValue, newValue interface{}) {
	for watcherKey, watcher := range f.watchers {
		if strings.HasPrefix(key, watcherKey) {
			watcher.OnChange(func(k string, old, new interface{}) {
				// Watcher callback implementation
			})
		}
	}
}

// DefaultConfigurationConfig returns default configuration
func DefaultConfigurationConfig() *ConfigurationConfig {
	return &ConfigurationConfig{
		ConfigPaths:        []string{"config.json", "config.yaml"},
		Environment:        "development",
		DefaultEnvironment: "development",
		AutoReload:         false,
		ReloadInterval:     30 * time.Second,
		ValidateOnLoad:     true,
		BackupOnChange:     true,
		BackupPath:         "backups",
		FileFormat:         "json",
		EnvPrefix:          "APP",
		CaseSensitive:      false,
		AllowOverrides:     true,
		Variables:          make(map[string]string),
	}
}

// Supporting types
// ValidationError is declared canonically in interfaces.go (union of validation
// fields incl. Key and Migration). See interfaces.go for the definition.

type ConfigSourceInfo struct {
	Type         string    `json:"type"`
	Path         string    `json:"path"`
	LastModified time.Time `json:"last_modified"`
}

// ConfigurationInfo summarizes the current state of the configuration facade.
type ConfigurationInfo struct {
	Loaded             bool               `json:"loaded"`
	LastModified       time.Time          `json:"last_modified"`
	CurrentEnvironment string             `json:"current_environment"`
	Environments       []string           `json:"environments"`
	Sources            []ConfigSourceInfo `json:"sources"`
	KeyCount           int                `json:"key_count"`
	ValidatorCount     int                `json:"validator_count"`
	WatcherCount       int                `json:"watcher_count"`
}

// Placeholder implementations for config sources
type FileConfigSource struct {
	path   string
	format string
}

func NewFileConfigSource(path, format string) *FileConfigSource {
	return &FileConfigSource{path: path, format: format}
}

func (s *FileConfigSource) Load(ctx context.Context) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

func (s *FileConfigSource) Save(ctx context.Context, data map[string]interface{}) error {
	return nil
}

func (s *FileConfigSource) Watch(ctx context.Context, callback func(map[string]interface{})) error {
	return nil
}

func (s *FileConfigSource) GetLastModified() time.Time {
	return time.Now()
}

func (s *FileConfigSource) GetSourceType() string {
	return "file"
}

func (s *FileConfigSource) GetSourcePath() string {
	return s.path
}

type HTTPConfigSource struct {
	url string
}

func NewHTTPConfigSource(url string) *HTTPConfigSource {
	return &HTTPConfigSource{url: url}
}

func (s *HTTPConfigSource) Load(ctx context.Context) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

func (s *HTTPConfigSource) Save(ctx context.Context, data map[string]interface{}) error {
	return nil
}

func (s *HTTPConfigSource) Watch(ctx context.Context, callback func(map[string]interface{})) error {
	return nil
}

func (s *HTTPConfigSource) GetLastModified() time.Time {
	return time.Now()
}

func (s *HTTPConfigSource) GetSourceType() string {
	return "http"
}

func (s *HTTPConfigSource) GetSourcePath() string {
	return s.url
}

type EnvironmentConfigSource struct {
	prefix string
}

func NewEnvironmentConfigSource(prefix string) *EnvironmentConfigSource {
	return &EnvironmentConfigSource{prefix: prefix}
}

func (s *EnvironmentConfigSource) Load(ctx context.Context) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

func (s *EnvironmentConfigSource) Save(ctx context.Context, data map[string]interface{}) error {
	return nil
}

func (s *EnvironmentConfigSource) Watch(ctx context.Context, callback func(map[string]interface{})) error {
	return nil
}

func (s *EnvironmentConfigSource) GetLastModified() time.Time {
	return time.Now()
}

func (s *EnvironmentConfigSource) GetSourceType() string {
	return "environment"
}

func (s *EnvironmentConfigSource) GetSourcePath() string {
	return "env://" + s.prefix
}

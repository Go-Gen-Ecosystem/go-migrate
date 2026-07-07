/**
 * @file standalone.go
 * @description Standalone migration package that works independently of gogen
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/standalone/standalone.go
 */

package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// Tool represents a standalone migration tool
type Tool struct {
	config        *types.StandaloneConfig
	db            *gorm.DB
	engine        *engine.MigrationEngine
	logger        types.StandaloneLogger
	configManager types.ConfigManager
	pluginManager types.PluginManager
}

// NewStandaloneMigrationToolFromPath creates a new standalone migration tool from config path
func NewStandaloneMigrationToolFromPath(configPath string) (*Tool, error) {
	tool := &Tool{
		configManager: NewDefaultConfigManager(),
		pluginManager: NewDefaultPluginManager(),
		logger:        NewDefaultLogger(),
	}

	// Load configuration
	config, err := tool.configManager.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	tool.config = config

	// Initialize logger
	if err := tool.initializeLogger(); err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Initialize migration engine
	if err := tool.initializeMigrationEngine(false); err != nil {
		return nil, fmt.Errorf("failed to initialize migration engine: %w", err)
	}

	// Load plugins
	if err := tool.loadPlugins(); err != nil {
		tool.logger.Warn("Failed to load plugins", "error", err)
	}

	return tool, nil
}

// initializeLogger initializes the logger based on configuration
func (t *Tool) initializeLogger() error {
	// Implementation would depend on chosen logging library
	// For now, use a simple implementation
	t.logger = NewDefaultLogger()
	return nil
}

func (t *Tool) initializeMigrationEngine(dryRun bool) error {
	engine, err := engine.NewMigrationEngineWithFactory(types.Config{
		Database:  t.config.Database,
		Migration: t.config.Migration,
	}, dryRun)
	if err != nil {
		return fmt.Errorf("failed to create migration engine: %w", err)
	}

	t.engine = engine
	return nil
}

// loadPlugins loads migration plugins
func (t *Tool) loadPlugins() error {
	// Initialize enabled plugins
	for _, pluginConfig := range t.config.Plugins {
		if !pluginConfig.Enabled {
			continue
		}

		plugin, err := t.pluginManager.GetPlugin(pluginConfig.Name)
		if err != nil {
			t.logger.Warn("Failed to get plugin", "plugin", pluginConfig.Name, "error", err)
			continue
		}

		if err := plugin.Initialize(pluginConfig.Config); err != nil {
			t.logger.Warn("Failed to initialize plugin", "plugin", pluginConfig.Name, "error", err)
		} else {
			t.logger.Info("Plugin initialized", "plugin", pluginConfig.Name)
		}
	}

	return nil
}

// MigrateWithOptions runs migrations with options
func (t *Tool) MigrateWithOptions(ctx context.Context, options MigrateOptions) error {
	t.logger.Info("Starting migration", "target", options.Target, "dry_run", options.DryRun)

	// Create backup if enabled
	if t.config.Features.AutoBackup && !options.DryRun {
		if err := t.createBackup(ctx); err != nil {
			t.logger.Warn("Failed to create backup", "error", err)
		}
	}

	// Run migration
	err := t.engine.Migrate(ctx, options.Target, options.DryRun)
	if err != nil {
		t.logger.Error("Migration failed", "error", err)
		return err
	}

	t.logger.Info("Migration completed")

	return nil
}

// RollbackWithOptions rolls back migrations with options
func (t *Tool) RollbackWithOptions(ctx context.Context, options RollbackOptions) error {
	t.logger.Info("Starting rollback", "target", options.Target, "dry_run", options.DryRun)

	// Run rollback based on options
	var err error
	// Convert target to steps (simplified logic)
	steps := 1
	if options.Target != "" {
		// In a real implementation, you'd calculate steps based on target version
		steps = 1
	}
	err = t.engine.Rollback(ctx, steps)

	if err != nil {
		t.logger.Error("Rollback failed", "error", err)
		return err
	}

	t.logger.Info("Rollback completed")
	return nil
}

// Status returns migration status
func (t *Tool) Status(ctx context.Context) (*types.MigrationStatus, error) {
	return t.engine.Status(ctx)
}

// ValidateWithPath validates migrations with path
func (t *Tool) ValidateWithPath(ctx context.Context, path string) error {
	return t.engine.Validate(ctx)
}

// createBackup creates a database backup
func (t *Tool) createBackup(ctx context.Context) error {
	backupPath := filepath.Join("backups", fmt.Sprintf("backup_%s_%d.sql",
		t.config.Database.Database, time.Now().Unix()))

	// Backups contain full database contents, so the directory is owner-only.
	if err := os.MkdirAll(filepath.Dir(backupPath), 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Implementation would depend on database driver
	// This is a simplified example
	t.logger.Info("Creating backup", "path", backupPath)

	return nil
}

// Close releases the tool's resources. The migration engine owns the live
// database connection, so closing it is what actually releases the handle (and,
// on Windows, unlocks a SQLite file); t.db is closed too when set.
func (t *Tool) Close() error {
	var errs []error
	if t.engine != nil {
		if err := t.engine.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close migration engine: %w", err))
		}
	}
	if t.db != nil {
		if sqlDB, err := t.db.DB(); err != nil {
			errs = append(errs, fmt.Errorf("failed to obtain underlying database handle: %w", err))
		} else if err := sqlDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close database connection: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Interface compatibility methods for types.StandaloneMigrationTool

// Migrate implements the types.MigrationTool interface
func (t *Tool) Migrate(ctx context.Context, target string, dryRun bool) error {
	options := MigrateOptions{
		Target: target,
		DryRun: dryRun,
	}
	return t.MigrateWithOptions(ctx, options)
}

// Rollback implements the types.MigrationTool interface
func (t *Tool) Rollback(ctx context.Context, steps int) error {
	options := RollbackOptions{
		Target: "", // Will rollback by steps
	}
	return t.RollbackWithOptions(ctx, options)
}

// Validate implements the types.MigrationTool interface
func (t *Tool) Validate(ctx context.Context) error {
	return t.ValidateWithPath(ctx, "")
}

// Info implements the types.MigrationTool interface
func (t *Tool) Info(ctx context.Context) (*types.MigrationInfo, error) {
	return &types.MigrationInfo{
		Version:        "1.0.0",
		DatabaseType:   t.config.Database.Driver,
		MigrationsPath: t.config.Migration.MigrationsPath,
		TableName:      t.config.Migration.TableName,
		LockTableName:  t.config.Migration.LockTableName,
		Environment:    t.config.Environment.Name,
		Features:       []string{},
		LastCheck:      time.Now(),
	}, nil
}

// LoadConfig implements the types.StandaloneMigrationTool interface
func (t *Tool) LoadConfig(configPath string) error {
	config, err := t.configManager.LoadConfig(configPath)
	if err != nil {
		return err
	}
	t.config = config
	return nil
}

// SaveConfig implements the types.StandaloneMigrationTool interface
func (t *Tool) SaveConfig(configPath string) error {
	return t.configManager.SaveConfig(t.config, configPath)
}

// GetConfig implements the types.StandaloneMigrationTool interface
func (t *Tool) GetConfig() *types.StandaloneConfig {
	return t.config
}

// SetEnvironment implements the types.StandaloneMigrationTool interface
func (t *Tool) SetEnvironment(env *types.EnvironmentConfig) {
	t.config.Environment = *env
}

// GetEnvironment implements the types.StandaloneMigrationTool interface
func (t *Tool) GetEnvironment() *types.EnvironmentConfig {
	return &t.config.Environment
}

// SetPlugins implements the types.StandaloneMigrationTool interface
func (t *Tool) SetPlugins(plugins []types.PluginConfig) {
	t.config.Plugins = plugins
}

// GetPlugins implements the types.StandaloneMigrationTool interface
func (t *Tool) GetPlugins() []types.PluginConfig {
	return t.config.Plugins
}

// MigrateOptions holds options for migration
type MigrateOptions struct {
	Target string `json:"target"`
	DryRun bool   `json:"dry_run"`
	Force  bool   `json:"force"`
	Path   string `json:"path"`
	Format string `json:"format"`
	Author string `json:"author"`
	Tag    string `json:"tag"`
}

// RollbackOptions holds options for rollback
type RollbackOptions struct {
	Target string `json:"target"`
	DryRun bool   `json:"dry_run"`
	Force  bool   `json:"force"`
	Tag    string `json:"tag"`
}

// ConfigManager manages configuration loading and validation
type ConfigManager interface {
	LoadConfig(path string) (*types.StandaloneConfig, error)
	SaveConfig(config *types.StandaloneConfig, path string) error
	ValidateConfig(config *types.StandaloneConfig) error
	MergeConfigs(base, override *types.StandaloneConfig) *types.StandaloneConfig
}

// PluginManager manages migration plugins
type PluginManager interface {
	LoadPlugins(path string) error
	RegisterPlugin(name string, plugin types.MigrationPlugin) error
	GetPlugin(name string) (types.MigrationPlugin, error)
	ListPlugins() []string
}

// DefaultConfigManager implements configuration management
type DefaultConfigManager struct{}

// NewDefaultConfigManager creates a new default config manager
func NewDefaultConfigManager() *DefaultConfigManager {
	return &DefaultConfigManager{}
}

// LoadConfig loads configuration from file
func (m *DefaultConfigManager) LoadConfig(path string) (*types.StandaloneConfig, error) {
	if path == "" {
		return m.getDefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m.getDefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config types.StandaloneConfig
	if err := unmarshalConfig(path, data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate and set defaults
	if err := m.ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// isYAMLPath reports whether the config path uses a YAML extension.
func isYAMLPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// unmarshalConfig decodes config data as YAML for .yaml/.yml paths, JSON otherwise.
func unmarshalConfig(path string, data []byte, config *types.StandaloneConfig) error {
	if isYAMLPath(path) {
		return yaml.Unmarshal(data, config)
	}
	return json.Unmarshal(data, config)
}

// SaveConfig saves configuration to file, choosing YAML or JSON by extension.
func (m *DefaultConfigManager) SaveConfig(config *types.StandaloneConfig, path string) error {
	var (
		data []byte
		err  error
	)
	if isYAMLPath(path) {
		data, err = yaml.Marshal(config)
	} else {
		data, err = json.MarshalIndent(config, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Config embeds plaintext database credentials (Username/Password/DSN), so
	// restrict the file and its parent directory to the owner only (least privilege).
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ValidateConfig validates configuration
func (m *DefaultConfigManager) ValidateConfig(config *types.StandaloneConfig) error {
	if config.Database.Driver == "" {
		config.Database.Driver = "sqlite"
	}

	if config.Database.Database == "" {
		config.Database.Database = "migrations.db"
	}

	// Set connection pool defaults
	if config.Database.MaxConnections == 0 {
		config.Database.MaxConnections = 25
	}
	if config.Database.ConnectionTimeout == 0 {
		config.Database.ConnectionTimeout = time.Second * 30
	}
	if config.Database.MaxLifetime == 0 {
		config.Database.MaxLifetime = time.Hour
	}
	if config.Database.IdleTimeout == 0 {
		config.Database.IdleTimeout = time.Minute * 15
	}

	// Set migration defaults
	if config.Migration.MigrationsPath == "" {
		config.Migration.MigrationsPath = "migrations"
	}

	return nil
}

// MergeConfigs merges two configurations
func (m *DefaultConfigManager) MergeConfigs(base, override *types.StandaloneConfig) *types.StandaloneConfig {
	// Simple merge implementation - in production, use a proper merge library
	result := *base

	if override.Database.Driver != "" {
		result.Database.Driver = override.Database.Driver
	}
	if override.Database.Host != "" {
		result.Database.Host = override.Database.Host
	}
	// ... merge other fields as needed

	return &result
}

// getDefaultConfig returns default configuration
func (m *DefaultConfigManager) getDefaultConfig() *types.StandaloneConfig {
	return &types.StandaloneConfig{
		Database: types.DatabaseConfig{
			Driver:            "sqlite",
			Database:          "migrations.db",
			MaxConnections:    25,
			ConnectionTimeout: time.Second * 30,
			MaxLifetime:       time.Hour,
			IdleTimeout:       time.Minute * 15,
		},
		Migration: *types.DefaultMigrationConfig(),
		Logging: types.LoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
		Features: types.FeatureConfig{
			AutoBackup:    true,
			HealthChecks:  true,
			Monitoring:    false,
			Notifications: false,
		},
	}
}

// DefaultPluginManager implements plugin management
type DefaultPluginManager struct {
	plugins map[string]types.MigrationPlugin
}

// NewDefaultPluginManager creates a new default plugin manager
func NewDefaultPluginManager() *DefaultPluginManager {
	return &DefaultPluginManager{
		plugins: make(map[string]types.MigrationPlugin),
	}
}

// LoadPlugins loads plugins from directory
func (m *DefaultPluginManager) LoadPlugins(path string) error {
	// Plugin loading implementation would go here
	// For now, just return nil
	return nil
}

// RegisterPlugin registers a plugin
func (m *DefaultPluginManager) RegisterPlugin(name string, plugin types.MigrationPlugin) error {
	m.plugins[name] = plugin
	return nil
}

// GetPlugin gets a plugin by name
func (m *DefaultPluginManager) GetPlugin(name string) (types.MigrationPlugin, error) {
	plugin, exists := m.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", name)
	}
	return plugin, nil
}

// ListPlugins lists all registered plugins
func (m *DefaultPluginManager) ListPlugins() []string {
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// DefaultLogger adapts the shared structured logger to the StandaloneLogger
// contract, so the standalone tool logs through internal/logging like the rest
// of the module instead of printing to stdout.
type DefaultLogger struct {
	log logging.Logger
}

// NewDefaultLogger creates a structured logger for the standalone tool.
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{log: logging.GetComponentLogger("standalone")}
}

// Debug logs a debug message.
func (l *DefaultLogger) Debug(msg string, fields ...interface{}) { l.log.Debug(msg, fields...) }

// Info logs an info message.
func (l *DefaultLogger) Info(msg string, fields ...interface{}) { l.log.Info(msg, fields...) }

// Warn logs a warning message.
func (l *DefaultLogger) Warn(msg string, fields ...interface{}) { l.log.Warn(msg, fields...) }

// Error logs an error message.
func (l *DefaultLogger) Error(msg string, fields ...interface{}) { l.log.Error(msg, fields...) }

// Fatal logs a fatal message and exits the process.
func (l *DefaultLogger) Fatal(msg string, fields ...interface{}) { l.log.Fatal(msg, fields...) }

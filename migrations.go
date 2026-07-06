/**
 * @file migrations.go
 * @description Main migrations package that exports public interfaces and constructors
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/migrations.go
 */

package migrations

import (
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/governance"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/standalone"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// Public type aliases for external use
type (
	// Core types
	Config                  = types.Config
	Migration               = types.Migration
	MigrationStatus         = types.MigrationStatus
	MigrationInfo           = types.MigrationInfo
	MigrationTool           = types.MigrationTool
	StandaloneMigrationTool = types.StandaloneMigrationTool
	EnvironmentConfig       = types.EnvironmentConfig
	PluginConfig            = types.PluginConfig
	StandaloneConfig        = types.StandaloneConfig
	MigrationRecord         = types.MigrationRecord

	// Database and configuration types
	DatabaseConfig   = types.DatabaseConfig
	MigrationConfig  = types.MigrationConfig
	LoggingConfig    = types.LoggingConfig
	FeatureConfig    = types.FeatureConfig

	// Options types
	MigrateOptions  = standalone.MigrateOptions
	RollbackOptions = standalone.RollbackOptions

	// Rollback types
	RollbackResult              = types.RollbackResult
	SelectiveRollbackResult     = types.SelectiveRollbackResult
	RollbackImpactAnalysis      = types.RollbackImpactAnalysis
	RollbackTestResult          = types.RollbackTestResult
	DependencyGraph             = types.DependencyGraph
	SelectiveRollbackOptions    = types.SelectiveRollbackOptions

	// Governance types (imported from governance package)
	ApprovalRequest             = governance.ApprovalRequest
	ComplianceReport            = governance.ComplianceReport
	PolicyValidationResult      = governance.PolicyResult
)

// NewMigrationTool creates a new migration tool instance
func NewMigrationTool(config *Config) (MigrationTool, error) {
	engine, err := engine.NewMigrationEngine(*config)
	if err != nil {
		return nil, err
	}
	return engine, nil
}

// NewStandaloneMigrationTool creates a new standalone migration tool instance from config
func NewStandaloneMigrationTool(config *StandaloneConfig) (StandaloneMigrationTool, error) {
	return types.NewStandaloneMigrationToolImpl(), nil
}

// NewStandaloneMigrationToolFromPath creates a new standalone migration tool from config path
func NewStandaloneMigrationToolFromPath(configPath string) (*standalone.StandaloneMigrationTool, error) {
	return standalone.NewStandaloneMigrationToolFromPath(configPath)
}

// LoadStandaloneConfig loads a standalone configuration from file
func LoadStandaloneConfig(configPath string) (*StandaloneConfig, error) {
	configManager := standalone.NewDefaultConfigManager()
	return configManager.LoadConfig(configPath)
}

// SaveStandaloneConfig saves a standalone configuration to file
func SaveStandaloneConfig(config *StandaloneConfig, configPath string) error {
	configManager := standalone.NewDefaultConfigManager()
	return configManager.SaveConfig(config, configPath)
}

// NewDefaultConfigManager creates a new default config manager
func NewDefaultConfigManager() standalone.ConfigManager {
	return standalone.NewDefaultConfigManager()
}

// CreateDefaultStandaloneConfig creates a default standalone configuration
func CreateDefaultStandaloneConfig() *StandaloneConfig {
	return &StandaloneConfig{
		Database: types.DatabaseConfig{
			Driver:   "sqlite",
			Database: "migrations.db",
			Host:     "localhost",
			Port:     5432,
		},
		Migration: types.MigrationConfig{
			TableName:          "schema_migrations",
			LockTableName:      "migration_locks",
			ChangelogTableName: "migration_changelog",
			MigrationsPath:     "./migrations",
			Environment:        "development",
			AutoMigrate:        true,
			AutoCreateTables:   true,
			ValidateOnMigrate:  true,
			ValidateOnLoad:     true,
			StopOnError:        true,
		},
		Logging: types.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Features: types.FeatureConfig{
			AutoBackup:        true,
			DryRunMode:        true,
			ParallelExecution: false,
			HealthChecks:      true,
			Monitoring:        true,
			Notifications:     false,
		},
		Environment: types.EnvironmentConfig{
			Name:         "development",
			Variables:    make(map[string]string),
			Profiles:     []string{"dev"},
			Contexts:     []string{},
			Labels:       []string{},
			Restrictions: make(map[string]bool),
		},
		Plugins: []types.PluginConfig{},
	}
}

// Public interfaces for external use
type (
	// MigrationTracker interface for tracking migrations
	MigrationTracker interface {
		GetMigration(migrationID string) (*Migration, error)
		GetAppliedMigrations() ([]*Migration, error)
		GetPendingMigrations() ([]*Migration, error)
		GetMigrationHistory() ([]*Migration, error)
	}

	// Logger interface for migration logging
	Logger interface {
		Debug(msg string, fields ...interface{})
		Info(msg string, fields ...interface{})
		Warn(msg string, fields ...interface{})
		Error(msg string, fields ...interface{})
	}
)

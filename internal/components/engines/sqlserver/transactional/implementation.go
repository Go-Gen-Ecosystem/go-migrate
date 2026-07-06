/*
 * @file implementation.go
 * @description Main implementation of the migration tool using the new engine
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/implementation/implementation.go
 */

package implementation

import (
	"context"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// migrationTool implements the MigrationTool interface using the new engine
type migrationTool struct {
	engine *engine.MigrationEngine
}

// NewMigrationTool creates a new migration tool instance
func NewMigrationTool(config *types.Config) (types.MigrationTool, error) {
	migrationEngine, err := engine.NewMigrationEngine(*config)
	if err != nil {
		return nil, err
	}

	return &migrationTool{
		engine: migrationEngine,
	}, nil
}

// Status returns the current migration status
func (mt *migrationTool) Status(ctx context.Context) (*types.MigrationStatus, error) {
	return mt.engine.Status(ctx)
}

// Migrate runs pending migrations
func (mt *migrationTool) Migrate(ctx context.Context, target string, dryRun bool) error {
	return mt.engine.Migrate(ctx, target, dryRun)
}

// Rollback rolls back migrations
func (mt *migrationTool) Rollback(ctx context.Context, steps int) error {
	return mt.engine.Rollback(ctx, steps)
}

// Validate validates all migrations
func (mt *migrationTool) Validate(ctx context.Context) error {
	return mt.engine.Validate(ctx)
}

// Info returns migration information
func (mt *migrationTool) Info(ctx context.Context) (*types.MigrationInfo, error) {
	return mt.engine.Info(ctx)
}

// Close closes the migration tool and releases resources
func (mt *migrationTool) Close() error {
	return mt.engine.Close()
}

// standaloneMigrationTool implements the standalone migration tool
type standaloneMigrationTool struct {
	*migrationTool
	config *types.StandaloneConfig
}

// NewStandaloneMigrationTool creates a new standalone migration tool
func NewStandaloneMigrationTool(config *types.StandaloneConfig) (types.StandaloneMigrationTool, error) {
	// Convert standalone config to regular config
	migrationConfig := &types.Config{
		Database:  config.Database,
		Migration: config.Migration,
		Logging:   config.Logging,
		Features:  config.Features,
	}

	tool, err := NewMigrationTool(migrationConfig)
	if err != nil {
		return nil, err
	}

	return &standaloneMigrationTool{
		migrationTool: tool.(*migrationTool),
		config:        config,
	}, nil
}

// LoadConfig loads configuration from file
func (smt *standaloneMigrationTool) LoadConfig(configPath string) error {
	// Implementation for loading configuration from file
	// This would parse YAML/JSON configuration files
	return nil
}

// SaveConfig saves configuration to file
func (smt *standaloneMigrationTool) SaveConfig(configPath string) error {
	// Implementation for saving configuration to file
	// This would serialize the configuration to YAML/JSON
	return nil
}

// GetConfig returns the current configuration
func (smt *standaloneMigrationTool) GetConfig() *types.StandaloneConfig {
	return smt.config
}

// SetEnvironment sets the environment configuration
func (smt *standaloneMigrationTool) SetEnvironment(env *types.EnvironmentConfig) {
	if env != nil {
		smt.config.Environment = *env
	}
}

// GetEnvironment returns the current environment configuration
func (smt *standaloneMigrationTool) GetEnvironment() *types.EnvironmentConfig {
	return &smt.config.Environment
}

// SetPlugins sets the plugin configuration
func (smt *standaloneMigrationTool) SetPlugins(plugins []types.PluginConfig) {
	smt.config.Plugins = plugins
}

// GetPlugins returns the current plugin configuration
func (smt *standaloneMigrationTool) GetPlugins() []types.PluginConfig {
	return smt.config.Plugins
}

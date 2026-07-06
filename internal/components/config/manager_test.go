/**
 * @file manager_test.go
 * @description Unit tests for configuration management hierarchy
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/config/manager_test.go
 */

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewConfigManager(t *testing.T) {
	cm := NewConfigManager()
	
	if cm == nil {
		t.Fatal("NewConfigManager returned nil")
	}
	
	if cm.config == nil {
		t.Error("config is nil")
	}
	
	if cm.values == nil {
		t.Error("values map is nil")
	}
	
	if cm.validators == nil {
		t.Error("validators map is nil")
	}
	
	// Check that default validators are registered
	if len(cm.validators) == 0 {
		t.Error("no default validators registered")
	}
}

func TestLoadDefaults(t *testing.T) {
	cm := NewConfigManager()
	
	err := cm.loadDefaults()
	if err != nil {
		t.Fatalf("loadDefaults failed: %v", err)
	}
	
	// Check that defaults are set
	config := cm.GetConfig()
	
	// Database defaults
	if config.Database.MaxConnections != 10 {
		t.Errorf("expected MaxConnections=10, got %d", config.Database.MaxConnections)
	}
	
	if config.Database.MaxIdleConns != 5 {
		t.Errorf("expected MaxIdleConns=5, got %d", config.Database.MaxIdleConns)
	}
	
	if config.Database.ConnMaxLifetime != time.Hour {
		t.Errorf("expected ConnMaxLifetime=1h, got %v", config.Database.ConnMaxLifetime)
	}
	
	// Verify database type default
	if config.Database.Type != "sqlite" {
		t.Errorf("expected default database type 'sqlite', got '%s'", config.Database.Type)
	}
	
	// Migration defaults
	if config.Migration.Directory != "./migrations" {
		t.Errorf("expected Directory=./migrations, got %s", config.Migration.Directory)
	}
	
	if config.Migration.TableName != "schema_migrations" {
		t.Errorf("expected TableName=schema_migrations, got %s", config.Migration.TableName)
	}
	
	if config.Migration.LockTimeout != 15*time.Minute {
		t.Errorf("expected LockTimeout=15m, got %v", config.Migration.LockTimeout)
	}
	
	// Backup defaults
	if !config.Backup.Enabled {
		t.Error("expected Backup.Enabled=true")
	}
	
	if config.Backup.RetentionDays != 30 {
		t.Errorf("expected RetentionDays=30, got %d", config.Backup.RetentionDays)
	}
	
	// Health defaults
	if !config.Health.Enabled {
		t.Error("expected Health.Enabled=true")
	}
	
	if config.Health.Interval != 30*time.Second {
		t.Errorf("expected Interval=30s, got %v", config.Health.Interval)
	}
	
	// Observability defaults
	if !config.Observability.Metrics.Enabled {
		t.Error("expected Metrics.Enabled=true")
	}
	
	if config.Observability.Logging.Level != "info" {
		t.Errorf("expected Logging.Level=info, got %s", config.Observability.Logging.Level)
	}
}

func TestLoadFromJSONFile(t *testing.T) {
	// Create temporary JSON config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	
	jsonContent := `{
		"database": {
			"type": "postgres",
			"connection_string": "postgres://user:pass@localhost/db",
			"max_connections": 20
		},
		"migration": {
			"directory": "./custom-migrations",
			"dry_run": true
		}
	}`
	
	err := os.WriteFile(configFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	
	cm := NewConfigManager()
	err = cm.LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	
	config := cm.GetConfig()
	
	// Check loaded values
	if config.Database.Type != "postgres" {
		t.Errorf("expected Type=postgres, got %s", config.Database.Type)
	}
	
	if config.Database.ConnectionString != "postgres://user:pass@localhost/db" {
		t.Errorf("expected connection string, got %s", config.Database.ConnectionString)
	}
	
	if config.Database.MaxConnections != 20 {
		t.Errorf("expected MaxConnections=20, got %d", config.Database.MaxConnections)
	}
	
	if config.Migration.Directory != "./custom-migrations" {
		t.Errorf("expected Directory=./custom-migrations, got %s", config.Migration.Directory)
	}
	
	if !config.Migration.DryRun {
		t.Error("expected DryRun=true")
	}
	
	// Check that defaults are still applied for unspecified values
	if config.Database.MaxIdleConns != 5 {
		t.Errorf("expected MaxIdleConns=5 (default), got %d", config.Database.MaxIdleConns)
	}
}

func TestLoadFromYAMLFile(t *testing.T) {
	// Create temporary YAML config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	
	yamlContent := `
database:
  type: mysql
  connection_string: "mysql://user:pass@localhost/db"
  max_connections: 15

migration:
  directory: "./yaml-migrations"
  table_name: "custom_migrations"
  lock_timeout: "10m"

backup:
  enabled: false
  directory: "./custom-backups"
`
	
	err := os.WriteFile(configFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	
	cm := NewConfigManager()
	err = cm.LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	
	config := cm.GetConfig()
	
	// Check loaded values
	if config.Database.Type != "mysql" {
		t.Errorf("expected Type=mysql, got %s", config.Database.Type)
	}
	
	if config.Database.MaxConnections != 15 {
		t.Errorf("expected MaxConnections=15, got %d", config.Database.MaxConnections)
	}
	
	if config.Migration.TableName != "custom_migrations" {
		t.Errorf("expected TableName=custom_migrations, got %s", config.Migration.TableName)
	}
	
	if config.Migration.LockTimeout != 10*time.Minute {
		t.Errorf("expected LockTimeout=10m, got %v", config.Migration.LockTimeout)
	}
	
	if config.Backup.Enabled {
		t.Error("expected Backup.Enabled=false")
	}
	
	if config.Backup.Directory != "./custom-backups" {
		t.Errorf("expected Backup.Directory=./custom-backups, got %s", config.Backup.Directory)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	// Set environment variables
	envVars := map[string]string{
		"MIGRATION_DATABASE_TYPE":             "sqlite",
		"MIGRATION_DATABASE_CONNECTION_STRING": "sqlite:///test.db",
		"MIGRATION_DATABASE_MAX_CONNECTIONS":   "25",
		"MIGRATION_MIGRATION_DIRECTORY":        "./env-migrations",
		"MIGRATION_MIGRATION_DRY_RUN":          "true",
		"MIGRATION_BACKUP_ENABLED":             "false",
		"MIGRATION_HEALTH_INTERVAL":            "60s",
	}
	
	// Set environment variables
	for key, value := range envVars {
		os.Setenv(key, value)
	}
	
	// Clean up after test
	defer func() {
		for key := range envVars {
			os.Unsetenv(key)
		}
	}()
	
	cm := NewConfigManager()
	err := cm.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	
	config := cm.GetConfig()
	
	// Check environment values
	if config.Database.Type != "sqlite" {
		t.Errorf("expected Type=sqlite, got %s", config.Database.Type)
	}
	
	if config.Database.ConnectionString != "sqlite:///test.db" {
		t.Errorf("expected connection string, got %s", config.Database.ConnectionString)
	}
	
	if config.Database.MaxConnections != 25 {
		t.Errorf("expected MaxConnections=25, got %d", config.Database.MaxConnections)
	}
	
	if config.Migration.Directory != "./env-migrations" {
		t.Errorf("expected Directory=./env-migrations, got %s", config.Migration.Directory)
	}
	
	if !config.Migration.DryRun {
		t.Error("expected DryRun=true")
	}
	
	if config.Backup.Enabled {
		t.Error("expected Backup.Enabled=false")
	}
	
	if config.Health.Interval != 60*time.Second {
		t.Errorf("expected Interval=60s, got %v", config.Health.Interval)
	}
}

func TestConfigurationHierarchy(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	
	jsonContent := `{
		"database": {
			"type": "postgres",
			"connection_string": "postgres://localhost/test",
			"max_connections": 20
		},
		"migration": {
			"directory": "./file-migrations"
		}
	}`
	
	err := os.WriteFile(configFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	
	// Set environment variables (should override file)
	os.Setenv("MIGRATION_DATABASE_MAX_CONNECTIONS", "30")
	os.Setenv("MIGRATION_MIGRATION_DIRECTORY", "./env-migrations")
	defer func() {
		os.Unsetenv("MIGRATION_DATABASE_MAX_CONNECTIONS")
		os.Unsetenv("MIGRATION_MIGRATION_DIRECTORY")
	}()
	
	cm := NewConfigManager()
	err = cm.LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	
	config := cm.GetConfig()
	
	// Environment should override file
	if config.Database.MaxConnections != 30 {
		t.Errorf("expected MaxConnections=30 (env override), got %d", config.Database.MaxConnections)
	}
	
	if config.Migration.Directory != "./env-migrations" {
		t.Errorf("expected Directory=./env-migrations (env override), got %s", config.Migration.Directory)
	}
	
	// File should override defaults
	if config.Database.Type != "postgres" {
		t.Errorf("expected Type=postgres (file), got %s", config.Database.Type)
	}
	
	// Defaults should still apply for unspecified values
	if config.Database.MaxIdleConns != 5 {
		t.Errorf("expected MaxIdleConns=5 (default), got %d", config.Database.MaxIdleConns)
	}
}

func TestSetOverride(t *testing.T) {
	cm := NewConfigManager()
	
	// Load defaults first
	err := cm.loadDefaults()
	if err != nil {
		t.Fatalf("loadDefaults failed: %v", err)
	}
	
	// Set override
	err = cm.SetOverride("Database.MaxConnections", 50)
	if err != nil {
		t.Fatalf("SetOverride failed: %v", err)
	}
	
	config := cm.GetConfig()
	if config.Database.MaxConnections != 50 {
		t.Errorf("expected MaxConnections=50 (override), got %d", config.Database.MaxConnections)
	}
	
	// Check that override is tracked
	value, exists := cm.GetConfigValue("Database.MaxConnections")
	if !exists {
		t.Error("override value not tracked")
	}
	
	if value.Source != ConfigSourceOverride {
		t.Errorf("expected source=override, got %s", value.Source)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      func(*MigrationConfig)
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: func(c *MigrationConfig) {
				c.Database.Type = "postgres"
				c.Database.ConnectionString = "postgres://localhost/db"
			},
			expectError: false,
		},
		{
			name: "invalid database type",
			config: func(c *MigrationConfig) {
				c.Database.Type = "invalid"
				c.Database.ConnectionString = "test"
			},
			expectError: true,
			errorMsg:    "must be one of: sqlite, postgres, mysql",
		},
		{
			name: "empty connection string",
			config: func(c *MigrationConfig) {
				c.Database.Type = "postgres"
				c.Database.ConnectionString = ""
			},
			expectError: true,
			errorMsg:    "is required",
		},
		{
			name: "invalid log level",
			config: func(c *MigrationConfig) {
				c.Database.Type = "postgres"
				c.Database.ConnectionString = "postgres://localhost/db"
				c.Observability.Logging.Level = "invalid"
			},
			expectError: true,
			errorMsg:    "must be one of",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewConfigManager()
			
			// Load defaults first
			err := cm.loadDefaults()
			if err != nil {
				t.Fatalf("loadDefaults failed: %v", err)
			}
			
			// Apply test configuration
			tt.config(cm.config)
			
			// Validate
			err = cm.validateConfig()
			
			if tt.expectError {
				if err == nil {
					t.Error("expected validation error, got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

func TestCustomValidator(t *testing.T) {
	cm := NewConfigManager()
	
	// Load defaults first
	err := cm.loadDefaults()
	if err != nil {
		t.Fatalf("loadDefaults failed: %v", err)
	}
	
	// Register custom validator
	cm.RegisterValidator("Migration.BatchSize", func(value interface{}) error {
		batchSize, ok := value.(int)
		if !ok {
			return fmt.Errorf("batch size must be an integer")
		}
		if batchSize <= 0 || batchSize > 1000 {
			return fmt.Errorf("batch size must be between 1 and 1000")
		}
		return nil
	})
	
	// Test valid batch size
	err = cm.SetOverride("Migration.BatchSize", 500)
	if err != nil {
		t.Fatalf("SetOverride failed: %v", err)
	}
	
	err = cm.validateConfig()
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
	
	// Test invalid batch size
	err = cm.SetOverride("Migration.BatchSize", 2000)
	if err != nil {
		t.Fatalf("SetOverride failed: %v", err)
	}
	
	err = cm.validateConfig()
	if err == nil {
		t.Error("expected validation error for invalid batch size")
	}
}

func TestGetConfigSummary(t *testing.T) {
	cm := NewConfigManager()
	
	// Load defaults first
	err := cm.loadDefaults()
	if err != nil {
		t.Fatalf("loadDefaults failed: %v", err)
	}
	
	summary := cm.GetConfigSummary()
	
	if len(summary) == 0 {
		t.Error("expected non-empty config summary")
	}
	
	// Check that summary contains expected entries
	found := false
	for path, value := range summary {
		if path == "Database.MaxConnections" {
			found = true
			if value.Source != ConfigSourceDefault {
				t.Errorf("expected source=default, got %s", value.Source)
			}
			if value.Value != 10 {
				t.Errorf("expected value=10, got %v", value.Value)
			}
		}
	}
	
	if !found {
		t.Error("expected to find Database.MaxConnections in summary")
	}
}

func TestNonExistentConfigFile(t *testing.T) {
	cm := NewConfigManager()
	
	// Try to load non-existent file (should not error)
	err := cm.LoadConfig("/non/existent/file.json")
	if err != nil {
		t.Errorf("unexpected error for non-existent file: %v", err)
	}
	
	// Should still have defaults
	config := cm.GetConfig()
	if config.Database.MaxConnections != 10 {
		t.Errorf("expected default MaxConnections=10, got %d", config.Database.MaxConnections)
	}
}

func TestInvalidConfigFile(t *testing.T) {
	// Create temporary invalid JSON file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.json")
	
	invalidContent := `{ "invalid": json }`
	
	err := os.WriteFile(configFile, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	
	cm := NewConfigManager()
	err = cm.LoadConfig(configFile)
	if err == nil {
		t.Error("expected error for invalid JSON file")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())))
}

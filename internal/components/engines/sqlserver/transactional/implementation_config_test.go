/**
 * @file implementation_config_test.go
 * @description Behavioral tests for the migration tool implementation covering
 *              tool construction, the standalone tool's config/environment/
 *              plugin accessors, and the engine-backed Info/Close operations.
 *              Construction and the standalone accessors require no database;
 *              Info/Close are exercised against a real in-memory SQLite engine
 *              (the tool's underlying engine connects lazily on first use).
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/engines/sqlserver/transactional/implementation_config_test.go
 */

package implementation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// sqliteStandaloneConfig returns a standalone configuration wired to a
// throwaway file-based SQLite database under the test's temp directory.
// NewMigrationTool (invoked transitively by NewStandaloneMigrationTool) only
// requires a non-empty driver and does not connect eagerly, so this
// configuration constructs successfully and supports the engine-backed
// operations that connect lazily. gorm.io/driver/sqlite bundles the driver, so
// no explicit driver import is required.
func sqliteStandaloneConfig(t *testing.T) *types.StandaloneConfig {
	t.Helper()
	return &types.StandaloneConfig{
		Database: types.DatabaseConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(t.TempDir(), "standalone.db"),
		},
		Migration: types.MigrationConfig{
			TableName:      "schema_migrations",
			LockTableName:  "schema_migrations_lock",
			MigrationsPath: "./migrations",
		},
		Environment: types.EnvironmentConfig{
			Name: "test",
		},
	}
}

func TestNewMigrationTool_RequiresDriver(t *testing.T) {
	// NewMigrationEngine (called by NewMigrationTool) rejects an empty driver.
	_, err := NewMigrationTool(&types.Config{})
	if err == nil {
		t.Fatal("expected error when database driver is empty")
	}
}

func TestNewMigrationTool_Success(t *testing.T) {
	cfg := &types.Config{
		Database: types.DatabaseConfig{Driver: "sqlite", DSN: "file::memory:?cache=shared"},
		Migration: types.MigrationConfig{
			TableName:      "schema_migrations",
			MigrationsPath: "./migrations",
		},
	}

	tool, err := NewMigrationTool(cfg)
	if err != nil {
		t.Fatalf("NewMigrationTool failed: %v", err)
	}
	if tool == nil {
		t.Fatal("NewMigrationTool returned nil tool")
	}
}

func TestNewStandaloneMigrationTool_Success(t *testing.T) {
	tool, err := NewStandaloneMigrationTool(sqliteStandaloneConfig(t))
	if err != nil {
		t.Fatalf("NewStandaloneMigrationTool failed: %v", err)
	}
	if tool == nil {
		t.Fatal("NewStandaloneMigrationTool returned nil tool")
	}
}

func TestNewStandaloneMigrationTool_RequiresDriver(t *testing.T) {
	// An empty driver propagates through to NewMigrationEngine and fails.
	_, err := NewStandaloneMigrationTool(&types.StandaloneConfig{})
	if err == nil {
		t.Fatal("expected error when database driver is empty")
	}
}

func TestStandaloneMigrationTool_GetConfig(t *testing.T) {
	original := sqliteStandaloneConfig(t)
	tool, err := NewStandaloneMigrationTool(original)
	if err != nil {
		t.Fatalf("NewStandaloneMigrationTool failed: %v", err)
	}

	got := tool.GetConfig()
	if got == nil {
		t.Fatal("GetConfig returned nil")
	}
	// The tool stores the exact config pointer it was built with.
	if got != original {
		t.Error("GetConfig did not return the stored configuration")
	}
	if got.Database.Driver != "sqlite" {
		t.Errorf("expected driver 'sqlite', got %q", got.Database.Driver)
	}
}

func TestStandaloneMigrationTool_SetAndGetEnvironment(t *testing.T) {
	tool, err := NewStandaloneMigrationTool(sqliteStandaloneConfig(t))
	if err != nil {
		t.Fatalf("NewStandaloneMigrationTool failed: %v", err)
	}

	env := &types.EnvironmentConfig{
		Name:      "production",
		Variables: map[string]string{"REGION": "eu"},
		Profiles:  []string{"prod"},
	}
	tool.SetEnvironment(env)

	got := tool.GetEnvironment()
	if got == nil {
		t.Fatal("GetEnvironment returned nil")
	}
	if got.Name != "production" {
		t.Errorf("expected environment name 'production', got %q", got.Name)
	}
	if got.Variables["REGION"] != "eu" {
		t.Errorf("expected variable REGION='eu', got %q", got.Variables["REGION"])
	}
}

func TestStandaloneMigrationTool_SetEnvironment_NilIsNoOp(t *testing.T) {
	tool, err := NewStandaloneMigrationTool(sqliteStandaloneConfig(t))
	if err != nil {
		t.Fatalf("NewStandaloneMigrationTool failed: %v", err)
	}

	// The initial environment name is "test"; a nil argument must not mutate it.
	tool.SetEnvironment(nil)

	got := tool.GetEnvironment()
	if got == nil {
		t.Fatal("GetEnvironment returned nil")
	}
	if got.Name != "test" {
		t.Errorf("nil SetEnvironment changed the environment; got name %q", got.Name)
	}
}

func TestStandaloneMigrationTool_SetAndGetPlugins(t *testing.T) {
	tool, err := NewStandaloneMigrationTool(sqliteStandaloneConfig(t))
	if err != nil {
		t.Fatalf("NewStandaloneMigrationTool failed: %v", err)
	}

	plugins := []types.PluginConfig{
		{Name: "audit", Enabled: true, Config: map[string]interface{}{"level": "verbose"}},
		{Name: "metrics", Enabled: false},
	}
	tool.SetPlugins(plugins)

	got := tool.GetPlugins()
	if len(got) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(got))
	}
	if got[0].Name != "audit" || !got[0].Enabled {
		t.Errorf("unexpected first plugin: %+v", got[0])
	}
	if got[1].Name != "metrics" || got[1].Enabled {
		t.Errorf("unexpected second plugin: %+v", got[1])
	}
}

func TestStandaloneMigrationTool_LoadAndSaveConfig(t *testing.T) {
	tool, err := NewStandaloneMigrationTool(sqliteStandaloneConfig(t))
	if err != nil {
		t.Fatalf("NewStandaloneMigrationTool failed: %v", err)
	}

	// LoadConfig and SaveConfig are currently no-ops that must not error.
	if err := tool.LoadConfig("/tmp/does-not-matter.yaml"); err != nil {
		t.Errorf("LoadConfig returned unexpected error: %v", err)
	}
	if err := tool.SaveConfig("/tmp/does-not-matter.yaml"); err != nil {
		t.Errorf("SaveConfig returned unexpected error: %v", err)
	}
}

// TestMigrationTool_InfoAndClose exercises the engine-backed Info operation and
// resource cleanup. Info triggers lazy engine initialization, which connects to
// the in-memory SQLite database, so a successful call proves the full
// construct-then-initialize path works end to end without a live server.
func TestMigrationTool_InfoAndClose(t *testing.T) {
	cfg := &types.Config{
		Environment: "test",
		Database:    types.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "info.db")},
		Migration: types.MigrationConfig{
			TableName:      "schema_migrations",
			LockTableName:  "schema_migrations_lock",
			MigrationsPath: "./migrations",
		},
	}

	tool, err := NewMigrationTool(cfg)
	if err != nil {
		t.Fatalf("NewMigrationTool failed: %v", err)
	}

	ctx := context.Background()
	info, err := tool.Info(ctx)
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}
	if info == nil {
		t.Fatal("Info returned nil")
	}
	if info.DatabaseType != "sqlite" {
		t.Errorf("expected database type 'sqlite', got %q", info.DatabaseType)
	}
	if info.MigrationsPath != cfg.Migration.MigrationsPath {
		t.Errorf("expected migrations path %q, got %q", cfg.Migration.MigrationsPath, info.MigrationsPath)
	}

	if err := tool.Close(); err != nil {
		t.Errorf("Close returned unexpected error: %v", err)
	}
}

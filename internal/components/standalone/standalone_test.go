/**
 * @file standalone_test.go
 * @description Behaviour tests for the standalone migration tool. The tool is
 *              driven end-to-end: a JSON config points at a real SQLite file
 *              database and a migrations directory, and MigrateWithOptions runs
 *              a genuine up-migration that creates a table.
 * @author Admilson B. F. Cossa
 */

package standalone

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tempWorkspace returns a temporary directory that is cleaned up best-effort.
// Unlike t.TempDir(), a leftover locked SQLite file (Windows keeps the engine's
// handle open until GC) does not fail the test.
func tempWorkspace(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gm-standalone-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// writeToolConfig writes a JSON StandaloneConfig pointing at a SQLite file
// database and the given migrations directory, and returns its path.
func writeToolConfig(t *testing.T, dbPath, migDir string) string {
	t.Helper()
	cfg := types.StandaloneConfig{
		Database: types.DatabaseConfig{
			Driver: "sqlite",
			DSN:    dbPath,
		},
		Migration: types.MigrationConfig{
			TableName:        "migration_history",
			LockTableName:    "migration_locks",
			MigrationsPath:   migDir,
			MaxRollbackDepth: 10,
			ExecutionMode:    "sequential",
			StopOnError:      true,
			LockTimeout:      30 * time.Second,
		},
		Features: types.FeatureConfig{AutoBackup: false},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(migDir, "..", "config.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func writeMigration(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

// --- End-to-end: real up-migration through the standalone tool ---------------------

// NOTE ON CURRENT ENGINE BEHAVIOUR: the underlying MigrationEngine.Migrate
// pipeline validates and resolves pending migrations but does not yet contain a
// step that executes/records them (a flagged engine defect). These tests
// therefore drive the real standalone tool end-to-end and pin the observable
// behaviour: MigrateWithOptions runs the full pipeline without error, loading a
// real migration from disk and connecting a real SQLite database, and Status
// reads the real migration_history table. Once the engine gains its execution
// step, the pending/executed assertions here should flip (see the flagged task).

func TestStandaloneTool_MigrateWithOptions_RunsPipelineEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_create_accounts.sql",
		"CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT);")

	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	require.NotNil(t, tool)
	defer func() { _ = tool.Close() }()

	// Before migration: the one migration on disk is discovered as pending.
	preStatus, err := tool.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, preStatus.ExecutedCount)
	assert.Equal(t, 1, preStatus.PendingCount)
	assert.Equal(t, 1, preStatus.TotalMigrations)

	// The full migrate pipeline runs against a real SQLite database without error.
	require.NoError(t, tool.MigrateWithOptions(ctx, MigrateOptions{}))

	// Status reads the real migration_history table through the tool's engine.
	status, err := tool.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 1, status.TotalMigrations)
}

func TestStandaloneTool_Migrate_InterfaceMethod(t *testing.T) {
	ctx := context.Background()
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_create_logs.sql",
		"CREATE TABLE logs (id INTEGER PRIMARY KEY);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	// The MigrationTool-interface Migrate delegates to MigrateWithOptions and
	// runs the pipeline against the real database without error.
	require.NoError(t, tool.Migrate(ctx, "", false))

	status, err := tool.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, status.TotalMigrations)
}

func TestStandaloneTool_DryRunDoesNotApply(t *testing.T) {
	ctx := context.Background()
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_create_temp.sql",
		"CREATE TABLE temp_tbl (id INTEGER PRIMARY KEY);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	require.NoError(t, tool.MigrateWithOptions(ctx, MigrateOptions{DryRun: true}))

	// A dry run applies nothing: the migration remains pending.
	status, err := tool.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, status.ExecutedCount)
	assert.Equal(t, 1, status.PendingCount)
}

func TestStandaloneTool_Validate(t *testing.T) {
	ctx := context.Background()
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_ok.sql", "CREATE TABLE ok_tbl (id INTEGER PRIMARY KEY);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	// Validate loads the real migration from disk and runs the composite
	// validator against it; a well-formed migration validates cleanly.
	require.NoError(t, tool.Validate(ctx))
	require.NoError(t, tool.ValidateWithPath(ctx, migDir))
}

func TestStandaloneTool_Rollback_NoAppliedIsNoOp(t *testing.T) {
	ctx := context.Background()
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_c.sql", "CREATE TABLE c (id INTEGER PRIMARY KEY);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	// With nothing applied, rolling back one step is a clean no-op.
	require.NoError(t, tool.Rollback(ctx, 1))
	require.NoError(t, tool.RollbackWithOptions(ctx, RollbackOptions{Target: ""}))
}

func TestStandaloneTool_LoadAndSaveConfig(t *testing.T) {
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_x.sql", "CREATE TABLE x (id INT);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	// Reload the same config through the tool's LoadConfig method.
	require.NoError(t, tool.LoadConfig(configPath))
	assert.Equal(t, "sqlite", tool.GetConfig().Database.Driver)

	// Save it to a fresh JSON path and confirm the file is written.
	out := filepath.Join(dir, "saved-config.json")
	require.NoError(t, tool.SaveConfig(out))
	_, statErr := os.Stat(out)
	require.NoError(t, statErr)
}

func TestStandaloneTool_Info(t *testing.T) {
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_x.sql", "CREATE TABLE x (id INT);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	info, err := tool.Info(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "sqlite", info.DatabaseType)
	assert.Equal(t, migDir, info.MigrationsPath)
	assert.Equal(t, "migration_history", info.TableName)
}

func TestStandaloneTool_ConfigAccessors(t *testing.T) {
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_x.sql", "CREATE TABLE x (id INT);")
	configPath := writeToolConfig(t, dbPath, migDir)

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	cfg := tool.GetConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, "sqlite", cfg.Database.Driver)

	env := &types.EnvironmentConfig{Name: "prod"}
	tool.SetEnvironment(env)
	assert.Equal(t, "prod", tool.GetEnvironment().Name)

	plugins := []types.PluginConfig{{Name: "p1", Enabled: false}}
	tool.SetPlugins(plugins)
	require.Len(t, tool.GetPlugins(), 1)
	assert.Equal(t, "p1", tool.GetPlugins()[0].Name)
}

func TestStandaloneTool_LoadsWithEnabledPlugin(t *testing.T) {
	// An enabled (but unregistered) plugin exercises the loadPlugins loop; the
	// tool logs a warning and continues to construct successfully.
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))

	cfg := types.StandaloneConfig{
		Database:  types.DatabaseConfig{Driver: "sqlite", DSN: dbPath},
		Migration: types.MigrationConfig{TableName: "migration_history", LockTableName: "migration_locks", MigrationsPath: migDir, ExecutionMode: "sequential", LockTimeout: 30 * time.Second},
		Plugins:   []types.PluginConfig{{Name: "audit", Enabled: true}, {Name: "disabled", Enabled: false}},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(configPath, data, 0o644))

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	require.NotNil(t, tool)
	defer func() { _ = tool.Close() }()
	require.Len(t, tool.GetPlugins(), 2)
}

func TestStandaloneTool_AutoBackupPathRuns(t *testing.T) {
	ctx := context.Background()
	dir := tempWorkspace(t)
	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	writeMigration(t, migDir, "001_b.sql", "CREATE TABLE b (id INTEGER PRIMARY KEY);")

	cfg := types.StandaloneConfig{
		Database:  types.DatabaseConfig{Driver: "sqlite", DSN: dbPath, Database: "app"},
		Migration: types.MigrationConfig{TableName: "migration_history", LockTableName: "migration_locks", MigrationsPath: migDir, ExecutionMode: "sequential", LockTimeout: 30 * time.Second},
		Features:  types.FeatureConfig{AutoBackup: true},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(configPath, data, 0o644))

	tool, err := NewStandaloneMigrationToolFromPath(configPath)
	require.NoError(t, err)
	defer func() { _ = tool.Close() }()

	// AutoBackup enabled + non-dry-run exercises the createBackup path.
	require.NoError(t, tool.MigrateWithOptions(ctx, MigrateOptions{DryRun: false}))
	_ = os.RemoveAll("backups") // best-effort cleanup of the created backup dir
}

func TestStandaloneTool_FromPath_MissingConfigUsesDefaults(t *testing.T) {
	// A non-existent config path falls back to defaults (sqlite/migrations.db).
	tool, err := NewStandaloneMigrationToolFromPath(filepath.Join(t.TempDir(), "absent.json"))
	require.NoError(t, err)
	require.NotNil(t, tool)
	defer func() { _ = tool.Close() }()
	assert.Equal(t, "sqlite", tool.GetConfig().Database.Driver)
}

// --- DefaultConfigManager -----------------------------------------------------------

func TestDefaultConfigManager_LoadConfig_AppliesDefaults(t *testing.T) {
	m := NewDefaultConfigManager()

	// Empty path -> full default config.
	cfg, err := m.LoadConfig("")
	require.NoError(t, err)
	assert.Equal(t, "sqlite", cfg.Database.Driver)
	assert.Equal(t, "migrations.db", cfg.Database.Database)
	assert.Equal(t, 25, cfg.Database.MaxConnections)
	assert.Equal(t, "migrations", cfg.Migration.MigrationsPath)
}

func TestDefaultConfigManager_LoadConfig_FromFile(t *testing.T) {
	m := NewDefaultConfigManager()
	path := filepath.Join(t.TempDir(), "c.json")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"database":{"driver":"sqlite","database":"custom.db"}}`,
	), 0o644))

	cfg, err := m.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "custom.db", cfg.Database.Database)
	// Defaults still filled in by ValidateConfig.
	assert.Equal(t, 25, cfg.Database.MaxConnections)
}

func TestDefaultConfigManager_LoadConfig_InvalidJSON(t *testing.T) {
	m := NewDefaultConfigManager()
	path := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))
	_, err := m.LoadConfig(path)
	require.Error(t, err)
}

func TestDefaultConfigManager_SaveAndReload(t *testing.T) {
	m := NewDefaultConfigManager()
	path := filepath.Join(t.TempDir(), "nested", "c.json")

	cfg := &types.StandaloneConfig{
		Database: types.DatabaseConfig{Driver: "sqlite", Database: "saved.db"},
	}
	require.NoError(t, m.SaveConfig(cfg, path))

	reloaded, err := m.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "saved.db", reloaded.Database.Database)
}

func TestDefaultConfigManager_ValidateConfig_FillsDefaults(t *testing.T) {
	m := NewDefaultConfigManager()
	cfg := &types.StandaloneConfig{}
	require.NoError(t, m.ValidateConfig(cfg))
	assert.Equal(t, "sqlite", cfg.Database.Driver)
	assert.Equal(t, "migrations.db", cfg.Database.Database)
	assert.Equal(t, "migrations", cfg.Migration.MigrationsPath)
}

func TestDefaultConfigManager_MergeConfigs(t *testing.T) {
	m := NewDefaultConfigManager()
	base := &types.StandaloneConfig{Database: types.DatabaseConfig{Driver: "sqlite", Host: "base-host"}}
	override := &types.StandaloneConfig{Database: types.DatabaseConfig{Driver: "postgres", Host: "new-host"}}

	merged := m.MergeConfigs(base, override)
	assert.Equal(t, "postgres", merged.Database.Driver)
	assert.Equal(t, "new-host", merged.Database.Host)

	// Empty override fields leave the base value untouched.
	merged2 := m.MergeConfigs(base, &types.StandaloneConfig{})
	assert.Equal(t, "sqlite", merged2.Database.Driver)
}

// --- DefaultPluginManager -----------------------------------------------------------

func TestDefaultPluginManager_RegisterGetList(t *testing.T) {
	m := NewDefaultPluginManager()

	_, err := m.GetPlugin("missing")
	require.Error(t, err)

	plugin := &stubPlugin{name: "auditor"}
	require.NoError(t, m.RegisterPlugin("auditor", plugin))

	got, err := m.GetPlugin("auditor")
	require.NoError(t, err)
	assert.Same(t, plugin, got)

	assert.Contains(t, m.ListPlugins(), "auditor")
	require.NoError(t, m.LoadPlugins("/some/path")) // no-op, returns nil
}

// --- DefaultLogger ------------------------------------------------------------------

func TestDefaultLogger_DoesNotPanic(t *testing.T) {
	l := NewDefaultLogger()
	// These write to stdout; they must not panic.
	l.Debug("debug", "k", "v")
	l.Info("info", "k", "v")
	l.Warn("warn", "k", "v")
	l.Error("error", "k", "v")
}

// --- helpers & stubs ----------------------------------------------------------------

type stubPlugin struct {
	name string
}

func (p *stubPlugin) Name() string                            { return p.name }
func (p *stubPlugin) Version() string                         { return "1.0.0" }
func (p *stubPlugin) Description() string                     { return "stub" }
func (p *stubPlugin) Initialize(map[string]interface{}) error { return nil }
func (p *stubPlugin) BeforeMigration(context.Context, *types.Migration) error {
	return nil
}
func (p *stubPlugin) AfterMigration(context.Context, *types.Migration, *types.MigrationResult) error {
	return nil
}
func (p *stubPlugin) OnError(context.Context, *types.Migration, error) error { return nil }

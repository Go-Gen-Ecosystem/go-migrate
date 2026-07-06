/**
 * @file config_builder_test.go
 * @description Behaviour tests for the fluent ConfigBuilder, covering the
 *              fluent setters, multi-source loading, validation and the
 *              Build / MustBuild / BuildOrDefault outcomes.
 * @author Admilson B. F. Cossa
 */

package builder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigBuilder_FluentSettersBuild(t *testing.T) {
	cfg, err := NewConfigBuilder().
		WithMigrationsPath("db/migrations").
		WithTableName("schema_history").
		WithEnvironment("staging").
		WithStrictMode(true).
		WithTransactions(false).
		WithDryRun(true).
		WithBackup(true, "/var/backups").
		WithRollback(true, 25).
		WithTimeouts(time.Minute, 2*time.Minute, 3*time.Minute).
		WithValidation(true, true, true).
		WithChangelog(true, "json").
		WithExecutionMode("batch", 50, 8).
		WithOutOfOrder(true).
		Build()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "db/migrations", cfg.MigrationsPath)
	assert.Equal(t, "schema_history", cfg.TableName)
	assert.Equal(t, "staging", cfg.Environment)
	assert.True(t, cfg.StrictMode)
	assert.False(t, cfg.UseTransactions)
	assert.True(t, cfg.EnableDryRun)
	assert.True(t, cfg.EnableBackup)
	assert.Equal(t, "/var/backups", cfg.BackupPath)
	assert.True(t, cfg.EnableRollback)
	assert.Equal(t, 25, cfg.MaxRollbackDepth)
	assert.Equal(t, time.Minute, cfg.LockTimeout)
	assert.Equal(t, 2*time.Minute, cfg.CommandTimeout)
	assert.Equal(t, 3*time.Minute, cfg.StatementTimeout)
	assert.True(t, cfg.ValidateOnMigrate)
	assert.True(t, cfg.ValidateOnLoad)
	assert.True(t, cfg.ValidateChecksums)
	assert.True(t, cfg.EnableChangelog)
	assert.Equal(t, "json", cfg.ChangelogFormat)
	assert.Equal(t, "batch", cfg.ExecutionMode)
	assert.Equal(t, 50, cfg.BatchSize)
	assert.Equal(t, 8, cfg.Concurrency)
	assert.True(t, cfg.OutOfOrder)
}

func TestConfigBuilder_FromDefaults(t *testing.T) {
	cfg, err := NewConfigBuilder().FromDefaults().Build()
	require.NoError(t, err)
	// Defaults are valid out of the box.
	assert.Equal(t, "schema_migrations", cfg.TableName)
	assert.Equal(t, "migrations", cfg.MigrationsPath)
	assert.Equal(t, "sequential", cfg.ExecutionMode)
}

func TestConfigBuilder_Validation_MissingMigrationsPath(t *testing.T) {
	_, err := NewConfigBuilder().WithMigrationsPath("").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrations_path")
}

func TestConfigBuilder_Validation_MissingTableName(t *testing.T) {
	_, err := NewConfigBuilder().WithTableName("").Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "table_name")
}

func TestConfigBuilder_Validation_BadExecutionMode(t *testing.T) {
	_, err := NewConfigBuilder().WithExecutionMode("turbo", 0, 0).Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "execution_mode")
}

func TestConfigBuilder_FromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"table_name: yaml_table\nmigrations_path: yaml/migrations\nstrict_mode: true\nbatch_size: 7\nexecution_mode: batch\nlock_timeout: 90s\n",
	), 0o644))

	cfg, err := NewConfigBuilder().FromYAML(path).Build()
	require.NoError(t, err)
	assert.Equal(t, "yaml_table", cfg.TableName)
	assert.Equal(t, "yaml/migrations", cfg.MigrationsPath)
	assert.True(t, cfg.StrictMode)
	assert.Equal(t, 7, cfg.BatchSize)
	assert.Equal(t, "batch", cfg.ExecutionMode)
	assert.Equal(t, 90*time.Second, cfg.LockTimeout)
}

func TestConfigBuilder_FromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"table_name":"json_table","migrations_path":"json/migrations","batch_size":3}`,
	), 0o644))

	cfg, err := NewConfigBuilder().FromJSON(path).Build()
	require.NoError(t, err)
	assert.Equal(t, "json_table", cfg.TableName)
	assert.Equal(t, "json/migrations", cfg.MigrationsPath)
	assert.Equal(t, 3, cfg.BatchSize)
}

func TestConfigBuilder_FromProperties(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.properties")
	require.NoError(t, os.WriteFile(path, []byte(
		"# a comment\ntable_name = props_table\nmigrations_path=props/migrations\nstrict_mode=true\nexecution_mode=parallel\n",
	), 0o644))

	cfg, err := NewConfigBuilder().FromProperties(path).Build()
	require.NoError(t, err)
	assert.Equal(t, "props_table", cfg.TableName)
	assert.Equal(t, "props/migrations", cfg.MigrationsPath)
	assert.True(t, cfg.StrictMode)
	assert.Equal(t, "parallel", cfg.ExecutionMode)
}

func TestConfigBuilder_FromMultiFormat(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "c.yml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("table_name: multi\nmigrations_path: m\n"), 0o644))

	cfg, err := NewConfigBuilder().FromMultiFormat(yamlPath).Build()
	require.NoError(t, err)
	assert.Equal(t, "multi", cfg.TableName)
}

func TestConfigBuilder_FromMultiFormat_Unsupported(t *testing.T) {
	_, err := NewConfigBuilder().FromMultiFormat("config.toml").Build()
	assert.Error(t, err)
}

func TestConfigBuilder_FromMissingFile_RecordsError(t *testing.T) {
	_, err := NewConfigBuilder().FromYAML(filepath.Join(t.TempDir(), "absent.yaml")).Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration build failed")
}

func TestConfigBuilder_FromEnv(t *testing.T) {
	t.Setenv("GMTEST_TABLE_NAME", "env_table")
	t.Setenv("GMTEST_MIGRATIONS_PATH", "env/migrations")
	t.Setenv("GMTEST_STRICT_MODE", "true")
	t.Setenv("GMTEST_BATCH_SIZE", "12")
	t.Setenv("GMTEST_EXECUTION_MODE", "batch")

	cfg, err := NewConfigBuilder().FromEnv("GMTEST").Build()
	require.NoError(t, err)
	assert.Equal(t, "env_table", cfg.TableName)
	assert.Equal(t, "env/migrations", cfg.MigrationsPath)
	assert.True(t, cfg.StrictMode)
	assert.Equal(t, 12, cfg.BatchSize)
	assert.Equal(t, "batch", cfg.ExecutionMode)
}

func TestConfigBuilder_FromMavenPlugin(t *testing.T) {
	cfg, err := NewConfigBuilder().FromMavenPlugin(map[string]interface{}{
		"tableName":      "maven_table",
		"migrationsPath": "maven/migrations",
		"strictMode":     true,
		"executionMode":  "batch",
	}).Build()
	require.NoError(t, err)
	assert.Equal(t, "maven_table", cfg.TableName)
	assert.Equal(t, "maven/migrations", cfg.MigrationsPath)
	assert.True(t, cfg.StrictMode)
	assert.Equal(t, "batch", cfg.ExecutionMode)
}

func TestConfigBuilder_MustBuild_Succeeds(t *testing.T) {
	cfg := NewConfigBuilder().MustBuild()
	require.NotNil(t, cfg)
	assert.Equal(t, "schema_migrations", cfg.TableName)
}

func TestConfigBuilder_MustBuild_PanicsOnError(t *testing.T) {
	assert.Panics(t, func() {
		NewConfigBuilder().WithTableName("").MustBuild()
	})
}

func TestConfigBuilder_BuildOrDefault_ReturnsDefaultOnError(t *testing.T) {
	cfg := NewConfigBuilder().WithExecutionMode("nonsense", 0, 0).BuildOrDefault()
	require.NotNil(t, cfg)
	// Falls back to a valid default configuration.
	assert.Equal(t, "sequential", cfg.ExecutionMode)
	assert.Equal(t, "schema_migrations", cfg.TableName)
}

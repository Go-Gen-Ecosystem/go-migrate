/**
 * @file main_test.go
 * @description End-to-end acceptance test for the gomigrate CLI: it drives the
 * real command tree (up / status / down) against a real SQLite database and a
 * real migration file, asserting the schema is actually changed.
 * @author Admilson B. F. Cossa
 */

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	migrations "github.com/go-gen-ecosystem/go-migrate"
)

// runCLI executes a fresh command tree with the given args, capturing all output.
func runCLI(args ...string) (string, error) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestCLI_EndToEnd_UpStatusDown(t *testing.T) {
	// os.MkdirTemp (not t.TempDir) so a briefly-held SQLite handle on Windows
	// cannot fail cleanup.
	dir, err := os.MkdirTemp("", "gomigrate-cli-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "app.db")
	migDir := filepath.Join(dir, "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))
	migration := "-- +migrate Up\n" +
		"CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT);\n" +
		"-- +migrate Down\n" +
		"DROP TABLE widgets;\n"
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "001_widgets.sql"), []byte(migration), 0o644))

	cfg := &migrations.StandaloneConfig{
		Database: migrations.DatabaseConfig{Driver: "sqlite", DSN: dbPath},
		Migration: migrations.MigrationConfig{
			MigrationsPath:   migDir,
			TableName:        "migration_history",
			LockTableName:    "migration_locks",
			MaxRollbackDepth: 10,
			ExecutionMode:    "sequential",
			StopOnError:      true,
			LockTimeout:      30 * time.Second,
		},
	}
	configPath := filepath.Join(dir, "gomigrate.yaml")
	require.NoError(t, migrations.SaveStandaloneConfig(cfg, configPath))

	hasWidgets := func() bool {
		db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		})
		require.NoError(t, err)
		defer func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}()
		return db.Migrator().HasTable("widgets")
	}

	require.False(t, hasWidgets(), "table must not exist before migrating")

	// up — applies the pending migration.
	out, err := runCLI("--config", configPath, "up")
	require.NoError(t, err, out)
	assert.Contains(t, out, "applied successfully")
	assert.True(t, hasWidgets(), "up must create the widgets table")

	// status — reports one applied, none pending.
	out, err = runCLI("--config", configPath, "status")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Applied:  1")
	assert.Contains(t, out, "Pending:  0")

	// down — rolls the migration back, dropping the table.
	out, err = runCLI("--config", configPath, "down", "--steps", "1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Rolled back 1")
	assert.False(t, hasWidgets(), "down must drop the widgets table")
}

func TestCLI_Validate(t *testing.T) {
	dir, err := os.MkdirTemp("", "gomigrate-cli-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	migDir := filepath.Join(dir, "migrations")
	require.NoError(t, os.MkdirAll(migDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(migDir, "001_ok.sql"),
		[]byte("-- +migrate Up\nCREATE TABLE ok_tbl (id INTEGER PRIMARY KEY);\n"), 0o644))

	cfg := &migrations.StandaloneConfig{
		Database:  migrations.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "app.db")},
		Migration: migrations.MigrationConfig{MigrationsPath: migDir, LockTimeout: 30 * time.Second},
	}
	configPath := filepath.Join(dir, "gomigrate.json")
	require.NoError(t, migrations.SaveStandaloneConfig(cfg, configPath))

	out, err := runCLI("--config", configPath, "validate")
	require.NoError(t, err, out)
	assert.Contains(t, out, "valid")
}

func TestCLI_DownRejectsNonPositiveSteps(t *testing.T) {
	out, err := runCLI("--config", "unused.yaml", "down", "--steps", "0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive integer")
	_ = out
}

func TestCLI_UnknownConfigSurfacesNoCrash(t *testing.T) {
	// A missing config resolves to defaults (sqlite in-memory-ish); status must
	// not panic and must return through the normal error/So path.
	_, err := runCLI("--config", filepath.Join(t.TempDir(), "nope.yaml"), "status")
	// Either succeeds against the default config or returns a clean error, never panics.
	_ = err
}

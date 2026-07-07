/*
@file comprehensive_test.go
@description Comprehensive test suite to verify core migration functionality against the current component APIs
@author Admilson B. F. Cossa
@date 2025-01-28
@path pkg/core/database/migrations/comprehensive_test.go
*/

package migrations

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/executor"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/validator"
)

// TestCompilationFixes verifies that the executor, validator and loader components
// compile and operate correctly against their current public APIs.
func TestCompilationFixes(t *testing.T) {
	ctx := context.Background()

	t.Run("TestExecutorCompilation", func(t *testing.T) {
		testExecutorCompilation(t, ctx)
	})

	t.Run("TestValidatorCompilation", func(t *testing.T) {
		testValidatorCompilation(t, ctx)
	})

	t.Run("TestLoaderCompilation", func(t *testing.T) {
		testLoaderCompilation(t, ctx)
	})
}

// testExecutorCompilation tests that the executor component compiles and works correctly.
func testExecutorCompilation(t *testing.T, ctx context.Context) {
	gdb := createTestGormDatabase(t)

	// Create executor instance with a minimal development configuration.
	config := &types.MigrationConfig{
		TableName:   "schema_migrations",
		Environment: "development",
	}
	exec := executor.NewDatabaseMigrationExecutor(gdb, config)
	require.NotNil(t, exec, "Executor should be created successfully")

	// Build a real migration matching the current Migration schema.
	migration := &types.Migration{
		ID:          "test_001",
		Version:     "1.0.0",
		Description: "Test migration",
		Type:        types.MigrationTypeSQL,
		Script:      "CREATE TABLE test_table (id INTEGER PRIMARY KEY);",
		RollbackSQL: "DROP TABLE test_table;",
		CanRollback: true,
		Checksum:    "test_checksum",
		CreatedAt:   time.Now(),
	}

	// Execute the migration; it should apply cleanly.
	err := exec.Execute(ctx, migration)
	assert.NoError(t, err, "Migration execution should not error")
	assert.True(t, gdb.Migrator().HasTable("test_table"), "Executor should have created the table")
}

// testValidatorCompilation tests that the validator component compiles and works correctly.
func testValidatorCompilation(t *testing.T, ctx context.Context) {
	gdb := createTestGormDatabase(t)

	// Create validator instance backed by the gorm connection.
	val := validator.NewSyntaxMigrationValidator(gdb)
	require.NotNil(t, val, "Validator should be created successfully")

	// A syntactically valid migration should pass validation.
	migration := &types.Migration{
		ID:          "test_002",
		Version:     "1.0.1",
		Description: "Test validation migration",
		Type:        types.MigrationTypeSQL,
		Script:      "CREATE TABLE validation_test (id INTEGER);",
		Checksum:    "validation_checksum",
		CreatedAt:   time.Now(),
	}

	err := val.ValidateMigrationCtx(ctx, migration)
	assert.NoError(t, err, "Valid migration should pass syntax validation")
}

// testLoaderCompilation tests that the loader component compiles and works correctly.
func testLoaderCompilation(t *testing.T, _ context.Context) {
	// Create temporary migration files for testing
	tempDir := t.TempDir()

	// Create a test SQL migration file
	sqlFile := filepath.Join(tempDir, "V1__test_migration.sql")
	sqlContent := `-- Migration: Test migration
-- Description: Create test table

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT UNIQUE
);`

	err := os.WriteFile(sqlFile, []byte(sqlContent), 0644)
	require.NoError(t, err, "Should create test SQL file")

	// Create a test YAML changeset file. The loader reads the `changeSets`
	// collection, so the fixture uses that key.
	yamlFile := filepath.Join(tempDir, "changeset_001.yaml")
	yamlContent := `changeSets:
  - id: "001"
    author: "test"
    comment: "create products table"
    changes:
      - createTable:
          tableName: "products"
          columns:
            - column:
                name: "id"
                type: "INTEGER"
                constraints:
                  primaryKey: true
            - column:
                name: "name"
                type: "TEXT"
                constraints:
                  nullable: false`

	err = os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	require.NoError(t, err, "Should create test YAML file")

	// Create loader instance
	ldr := loader.NewFileMigrationLoader()
	require.NotNil(t, ldr, "Loader should be created successfully")

	// Test loading migrations from the directory (current API takes a path).
	migrations, err := ldr.LoadMigrations(tempDir)
	assert.NoError(t, err, "Should load migrations without error")
	assert.NotEmpty(t, migrations, "Should load at least one migration")
}

// createTestGormDatabase creates a temporary gorm-backed SQLite database for testing.
func createTestGormDatabase(t testing.TB) *gorm.DB {
	tempFile := filepath.Join(t.TempDir(), "gorm_test.db")

	gdb, err := gorm.Open(gormsqlite.Open(tempFile), &gorm.Config{})
	require.NoError(t, err, "Should open gorm test database")

	t.Cleanup(func() {
		if sqlDB, derr := gdb.DB(); derr == nil {
			_ = sqlDB.Close()
		}
		_ = os.Remove(tempFile)
	})

	return gdb
}

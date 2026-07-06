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
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/executor"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/lock"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/validator"
)

// TestCompilationFixes verifies that the executor, validator and loader components
// compile and operate correctly against their current public APIs.
func TestCompilationFixes(t *testing.T) {
	ctx := context.Background()

	// Create a test database
	db, cleanup := createTestDatabase(t)
	defer cleanup()

	t.Run("TestExecutorCompilation", func(t *testing.T) {
		testExecutorCompilation(t, ctx)
	})

	t.Run("TestValidatorCompilation", func(t *testing.T) {
		testValidatorCompilation(t, ctx)
	})

	t.Run("TestLoaderCompilation", func(t *testing.T) {
		testLoaderCompilation(t, ctx)
	})

	t.Run("TestSQLiteLockEnhancement", func(t *testing.T) {
		testSQLiteLockEnhancement(t, db)
	})

	t.Run("TestDualLockingStrategy", func(t *testing.T) {
		testDualLockingStrategy(t, db)
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

// testSQLiteLockEnhancement tests the enhanced SQLite lock functionality
func testSQLiteLockEnhancement(t *testing.T, db *sql.DB) {
	// Create SQLite lock manager
	lockManager := lock.NewSQLiteLockManager(db, "migration_locks")
	require.NotNil(t, lockManager, "Lock manager should be created successfully")

	// Test acquiring lock
	err := lockManager.AcquireLock()
	assert.NoError(t, err, "Should acquire lock successfully")

	// Test that lock is acquired
	isLocked, err := lockManager.IsLocked()
	assert.NoError(t, err, "Should check lock status without error")
	assert.True(t, isLocked, "Lock should be acquired")

	// Test releasing lock
	err = lockManager.ReleaseLock()
	assert.NoError(t, err, "Should release lock successfully")

	// Test that lock is released
	isLocked, err = lockManager.IsLocked()
	assert.NoError(t, err, "Should check lock status without error")
	assert.False(t, isLocked, "Lock should be released")
}

// testDualLockingStrategy tests the dual-locking strategy implementation
func testDualLockingStrategy(t *testing.T, db *sql.DB) {
	// Create two lock managers to simulate concurrent access. A short file-lock
	// timeout keeps the mutual-exclusion assertion fast without changing behavior.
	lockManager1 := lock.NewSQLiteLockManagerWithTimeout(db, "dual_lock_test", 1*time.Second)
	lockManager2 := lock.NewSQLiteLockManagerWithTimeout(db, "dual_lock_test", 1*time.Second)

	// First manager acquires lock
	err := lockManager1.AcquireLock()
	assert.NoError(t, err, "First manager should acquire lock successfully")

	// Second manager should fail to acquire lock
	err = lockManager2.AcquireLock()
	assert.Error(t, err, "Second manager should fail to acquire lock")

	// Release lock from first manager
	err = lockManager1.ReleaseLock()
	assert.NoError(t, err, "First manager should release lock successfully")

	// Now second manager should be able to acquire lock
	err = lockManager2.AcquireLock()
	assert.NoError(t, err, "Second manager should now acquire lock successfully")

	// Clean up
	err = lockManager2.ReleaseLock()
	assert.NoError(t, err, "Second manager should release lock successfully")
}

// TestLockTimeout tests the lock timeout functionality
func TestLockTimeout(t *testing.T) {
	db, cleanup := createTestDatabase(t)
	defer cleanup()

	// Create lock manager with short timeout
	lockManager := lock.NewSQLiteLockManagerWithTimeout(db, "timeout_test", 1*time.Second)

	// Acquire lock
	err := lockManager.AcquireLock()
	assert.NoError(t, err, "Should acquire lock successfully")

	// Create second manager with same timeout
	lockManager2 := lock.NewSQLiteLockManagerWithTimeout(db, "timeout_test", 1*time.Second)

	// Measure time for timeout
	start := time.Now()
	err = lockManager2.AcquireLock()
	duration := time.Since(start)

	assert.Error(t, err, "Should timeout when trying to acquire held lock")
	assert.Contains(t, err.Error(), "timeout", "Error should mention timeout")
	assert.GreaterOrEqual(t, duration, 1*time.Second, "Should wait at least the timeout duration")

	// Clean up
	err = lockManager.ReleaseLock()
	assert.NoError(t, err, "Should release lock successfully")
}

// createTestDatabase creates a temporary SQLite database for testing
func createTestDatabase(t testing.TB) (*sql.DB, func()) {
	// Create temporary database file
	tempFile := filepath.Join(t.TempDir(), "test.db")

	db, err := sql.Open("sqlite3", tempFile)
	require.NoError(t, err, "Should open test database")

	// Test connection
	err = db.Ping()
	require.NoError(t, err, "Should ping test database")

	cleanup := func() {
		db.Close()
		os.Remove(tempFile)
	}

	return db, cleanup
}

// createTestGormDatabase creates a temporary gorm-backed SQLite database for testing.
func createTestGormDatabase(t testing.TB) *gorm.DB {
	tempFile := filepath.Join(t.TempDir(), "gorm_test.db")

	gdb, err := gorm.Open(gormsqlite.Open(tempFile), &gorm.Config{})
	require.NoError(t, err, "Should open gorm test database")

	t.Cleanup(func() {
		if sqlDB, derr := gdb.DB(); derr == nil {
			sqlDB.Close()
		}
		os.Remove(tempFile)
	})

	return gdb
}

// BenchmarkDualLocking benchmarks the dual-locking strategy performance
func BenchmarkDualLocking(b *testing.B) {
	db, cleanup := createTestDatabase(b)
	defer cleanup()

	lockManager := lock.NewSQLiteLockManager(db, "benchmark_test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := lockManager.AcquireLock()
		if err != nil {
			b.Fatalf("Failed to acquire lock: %v", err)
		}

		err = lockManager.ReleaseLock()
		if err != nil {
			b.Fatalf("Failed to release lock: %v", err)
		}
	}
}

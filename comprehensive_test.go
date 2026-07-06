/*
@file comprehensive_test.go
@description Comprehensive test suite to verify all compilation fixes and core migration functionality
@author Admilson B. F. Cossa
@date 2025-01-28
@path pkg/core/database/migrations/comprehensive_test.go
*/

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/executor"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/lock"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/validator"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// TestCompilationFixes verifies that all compilation fixes work correctly
func TestCompilationFixes(t *testing.T) {
	ctx := context.Background()
	
	// Create a test database
	db, cleanup := createTestDatabase(t)
	defer cleanup()

	// Create logger
	testLogger := logging.NewComponentLogger("test", logging.LevelInfo)

	t.Run("TestExecutorCompilation", func(t *testing.T) {
		testExecutorCompilation(t, ctx, db, testLogger)
	})

	t.Run("TestValidatorCompilation", func(t *testing.T) {
		testValidatorCompilation(t, ctx, db, testLogger)
	})

	t.Run("TestLoaderCompilation", func(t *testing.T) {
		testLoaderCompilation(t, ctx, testLogger)
	})

	t.Run("TestSQLiteLockEnhancement", func(t *testing.T) {
		testSQLiteLockEnhancement(t, db)
	})

	t.Run("TestDualLockingStrategy", func(t *testing.T) {
		testDualLockingStrategy(t, db)
	})
}

// testExecutorCompilation tests that the executor component compiles and works correctly
func testExecutorCompilation(t *testing.T, ctx context.Context, db *sql.DB, testLogger logger.ComponentLogger) {
	// Create executor instance
	exec := executor.NewDatabaseExecutor(db, testLogger)
	require.NotNil(t, exec, "Executor should be created successfully")

	// Test basic functionality
	migration := &Migration{
		ID:          "test_001",
		Version:     "1.0.0",
		Description: "Test migration",
		Type:        MigrationTypeVersioned,
		UpSQL:       "CREATE TABLE test_table (id INTEGER PRIMARY KEY);",
		DownSQL:     "DROP TABLE test_table;",
		Checksum:    "test_checksum",
		CreatedAt:   time.Now(),
	}

	// Test execution (this should compile without errors)
	result := exec.ExecuteMigration(ctx, migration)
	assert.NotNil(t, result, "Execution result should not be nil")
}

// testValidatorCompilation tests that the validator component compiles and works correctly
func testValidatorCompilation(t *testing.T, ctx context.Context, db *sql.DB, testLogger logger.ComponentLogger) {
	// Create validator instance
	val := validator.NewDatabaseValidator(db, testLogger)
	require.NotNil(t, val, "Validator should be created successfully")

	// Test basic functionality
	migration := &Migration{
		ID:          "test_002",
		Version:     "1.0.1",
		Description: "Test validation migration",
		Type:        MigrationTypeVersioned,
		UpSQL:       "CREATE TABLE validation_test (id INTEGER);",
		DownSQL:     "DROP TABLE validation_test;",
		Checksum:    "validation_checksum",
		CreatedAt:   time.Now(),
	}

	// Test validation (this should compile without errors)
	result := val.ValidateMigration(ctx, migration)
	assert.NotNil(t, result, "Validation result should not be nil")
}

// testLoaderCompilation tests that the loader component compiles and works correctly
func testLoaderCompilation(t *testing.T, ctx context.Context, testLogger logger.ComponentLogger) {
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

	// Create a test YAML changeset file
	yamlFile := filepath.Join(tempDir, "changeset_001.yaml")
	yamlContent := `databaseChangeLog:
  - changeSet:
      id: "001"
      author: "test"
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
	ldr := loader.NewFileSystemLoader(tempDir, testLogger)
	require.NotNil(t, ldr, "Loader should be created successfully")

	// Test loading migrations (this should compile without errors)
	migrations, err := ldr.LoadMigrations(ctx)
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
	// Create two lock managers to simulate concurrent access
	lockManager1 := lock.NewSQLiteLockManager(db, "dual_lock_test")
	lockManager2 := lock.NewSQLiteLockManager(db, "dual_lock_test")

	// First manager acquires lock
	err := lockManager1.AcquireLock()
	assert.NoError(t, err, "First manager should acquire lock successfully")

	// Second manager should fail to acquire lock
	err = lockManager2.AcquireLock()
	assert.Error(t, err, "Second manager should fail to acquire lock")
	assert.Contains(t, err.Error(), "lock is already held", "Error should indicate lock is held")

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
func createTestDatabase(t *testing.T) (*sql.DB, func()) {
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
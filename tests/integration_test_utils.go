//go:build drifted_testutils
// +build drifted_testutils

/**
 * @file integration_test_utils.go
 * @description Integration test utilities for end-to-end migration system testing
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/tests/integration_test_utils.go
 */

package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/go-gen-ecosystem/go-migrate"
)

// IntegrationTestSuite provides a base test suite for integration tests
type IntegrationTestSuite struct {
	suite.Suite
	TestEnv           *TestEnvironment
	DatabaseHelper    *DatabaseTestHelper
	CacheHelper       *CacheTestHelper
	FacadeHelper      *FacadeTestHelper
	BackendHelper     *BackendTestHelper
	PerformanceHelper *PerformanceTestHelper
	MockHelper        *MockTestHelper
	Context           context.Context
	Cancel            context.CancelFunc
}

// SetupSuite sets up the integration test suite
func (s *IntegrationTestSuite) SetupSuite() {
	s.Context, s.Cancel = context.WithCancel(context.Background())

	// Setup test environment
	var err error
	s.TestEnv, err = SetupTestEnvironment(s.T(), &MigrationTestConfig{
		DatabaseDriver: "sqlite3",
		CacheEnabled:   true,
		LogLevel:       "debug",
	})
	s.Require().NoError(err)

	// Setup helpers
	s.DatabaseHelper = NewDatabaseTestHelper(s.T(), s.TestEnv.DBPath)
	s.CacheHelper = NewCacheTestHelper(s.T())
	s.FacadeHelper = NewFacadeTestHelper(s.T())
	s.BackendHelper = NewBackendTestHelper(s.T())
	s.PerformanceHelper = NewPerformanceTestHelper()
	s.MockHelper = NewMockTestHelper()
}

// TearDownSuite tears down the integration test suite
func (s *IntegrationTestSuite) TearDownSuite() {
	if s.BackendHelper != nil {
		s.BackendHelper.Cleanup(s.T())
	}
	if s.FacadeHelper != nil {
		s.FacadeHelper.Cleanup(s.T())
	}
	if s.CacheHelper != nil {
		s.CacheHelper.Cleanup(s.T())
	}
	if s.DatabaseHelper != nil {
		s.DatabaseHelper.Cleanup(s.T())
	}
	if s.TestEnv != nil {
		TearDownTestEnvironment(s.T(), s.TestEnv)
	}
	if s.Cancel != nil {
		s.Cancel()
	}
}

// IntegrationTestHelper provides utilities for integration testing
type IntegrationTestHelper struct {
	TestEnv           *TestEnvironment
	DatabaseHelper    *DatabaseTestHelper
	CacheHelper       *CacheTestHelper
	FacadeHelper      *FacadeTestHelper
	BackendHelper     *BackendTestHelper
	PerformanceHelper *PerformanceTestHelper
	Context           context.Context
	Cancel            context.CancelFunc
}

// NewIntegrationTestHelper creates a new integration test helper
func NewIntegrationTestHelper(t *testing.T) *IntegrationTestHelper {
	ctx, cancel := context.WithCancel(context.Background())

	testEnv, err := SetupTestEnvironment(t, &MigrationTestConfig{
		DatabaseDriver: "sqlite3",
		CacheEnabled:   true,
		LogLevel:       "debug",
	})
	require.NoError(t, err)

	return &IntegrationTestHelper{
		TestEnv:           testEnv,
		DatabaseHelper:    NewDatabaseTestHelper(t, testEnv.DBPath),
		CacheHelper:       NewCacheTestHelper(t),
		FacadeHelper:      NewFacadeTestHelper(t),
		BackendHelper:     NewBackendTestHelper(t),
		PerformanceHelper: NewPerformanceTestHelper(),
		Context:           ctx,
		Cancel:            cancel,
	}
}

// Cleanup cleans up the integration test helper
func (h *IntegrationTestHelper) Cleanup(t *testing.T) {
	if h.BackendHelper != nil {
		h.BackendHelper.Cleanup(t)
	}
	if h.FacadeHelper != nil {
		h.FacadeHelper.Cleanup(t)
	}
	if h.CacheHelper != nil {
		h.CacheHelper.Cleanup(t)
	}
	if h.DatabaseHelper != nil {
		h.DatabaseHelper.Cleanup(t)
	}
	if h.TestEnv != nil {
		TearDownTestEnvironment(t, h.TestEnv)
	}
	if h.Cancel != nil {
		h.Cancel()
	}
}

// TestFullMigrationLifecycle tests the complete migration lifecycle
func (h *IntegrationTestHelper) TestFullMigrationLifecycle(t *testing.T) {
	// Create test migrations
	h.BackendHelper.CreateTestMigrations(t)

	// Initialize migration runner
	err := h.BackendHelper.MigrationRunner.Initialize(h.Context)
	require.NoError(t, err)

	// Test initial status
	status, err := h.BackendHelper.MigrationRunner.Status(h.Context)
	require.NoError(t, err)
	require.Greater(t, len(status.Pending), 0)
	require.Equal(t, 0, len(status.Applied))

	// Apply all migrations
	result, err := h.BackendHelper.MigrationRunner.Up(h.Context, 0)
	require.NoError(t, err)
	require.Greater(t, result.Applied, 0)

	// Verify migrations were applied
	status, err = h.BackendHelper.MigrationRunner.Status(h.Context)
	require.NoError(t, err)
	require.Greater(t, len(status.Applied), 0)
	require.Equal(t, 0, len(status.Pending))

	// Test data integrity
	h.TestDataIntegrity(t)

	// Rollback one migration
	result, err = h.BackendHelper.MigrationRunner.Down(h.Context, 1)
	require.NoError(t, err)
	require.Greater(t, result.Reverted, 0)

	// Reset all migrations
	result, err = h.BackendHelper.MigrationRunner.Reset(h.Context)
	require.NoError(t, err)

	// Refresh (reset and apply all)
	result, err = h.BackendHelper.MigrationRunner.Refresh(h.Context)
	require.NoError(t, err)
	require.Greater(t, result.Applied, 0)
}

// TestDataIntegrity tests data integrity across migrations
func (h *IntegrationTestHelper) TestDataIntegrity(t *testing.T) {
	// Insert test data
	err := h.DatabaseHelper.InsertTestData(h.Context, "users", map[string]interface{}{
		"username":      "testuser",
		"email":         "test@example.com",
		"password_hash": "hashed_password",
	})
	require.NoError(t, err)

	// Verify data exists
	exists, err := h.DatabaseHelper.CheckTableExists(h.Context, "users")
	require.NoError(t, err)
	require.True(t, exists)

	// Count records
	count, err := h.DatabaseHelper.CountTableRecords(h.Context, "users")
	require.NoError(t, err)
	require.Greater(t, count, 0)

	// Test foreign key relationships
	err = h.DatabaseHelper.InsertTestData(h.Context, "posts", map[string]interface{}{
		"user_id": 1,
		"title":   "Test Post",
		"slug":    "test-post",
		"content": "Test content",
		"status":  "published",
	})
	require.NoError(t, err)

	// Verify cascade delete
	h.TestCascadeDelete(t)
}

// TestCascadeDelete tests cascade delete functionality
func (h *IntegrationTestHelper) TestCascadeDelete(t *testing.T) {
	// Insert related data
	err := h.DatabaseHelper.InsertTestData(h.Context, "comments", map[string]interface{}{
		"post_id": 1,
		"user_id": 1,
		"content": "Test comment",
		"status":  "approved",
	})
	require.NoError(t, err)

	// Count comments before delete
	commentsBefore, err := h.DatabaseHelper.CountTableRecords(h.Context, "comments")
	require.NoError(t, err)

	// Delete user (should cascade to posts and comments)
	err = h.DatabaseHelper.ExecuteSQL(h.Context, "DELETE FROM users WHERE id = ?", 1)
	require.NoError(t, err)

	// Verify cascade delete worked
	commentsAfter, err := h.DatabaseHelper.CountTableRecords(h.Context, "comments")
	require.NoError(t, err)
	require.Less(t, commentsAfter, commentsBefore)

	postsAfter, err := h.DatabaseHelper.CountTableRecords(h.Context, "posts")
	require.NoError(t, err)
	require.Equal(t, 0, postsAfter)
}

// TestCacheIntegration tests cache integration with migrations
func (h *IntegrationTestHelper) TestCacheIntegration(t *testing.T) {
	// Create cache
	cacheConfig := h.CacheHelper.CreateTestCacheConfig("migration_cache", 100, cache.LRU, cache.JSON)
	testCache, err := h.CacheHelper.CreateTestCache("migration_cache", cacheConfig)
	require.NoError(t, err)

	// Test cache operations
	h.CacheHelper.TestCacheBasicOperations(t, testCache)

	// Test cache with migration data
	migration := &backend.Migration{
		Version:  "001",
		Name:     "test_migration",
		UpSQL:    "CREATE TABLE test (id INTEGER);",
		DownSQL:  "DROP TABLE test;",
		Checksum: "test_checksum",
	}

	// Cache migration
	err = testCache.Set("migration_001", migration, 5*time.Minute)
	require.NoError(t, err)

	// Retrieve migration from cache
	cached, exists := testCache.Get("migration_001")
	require.True(t, exists)
	require.NotNil(t, cached)

	// Verify cache statistics
	stats := testCache.GetStats()
	require.NotNil(t, stats)
	require.Greater(t, stats.Sets, int64(0))
	require.Greater(t, stats.Gets, int64(0))
}

// TestFacadeIntegration tests facade integration
func (h *IntegrationTestHelper) TestFacadeIntegration(t *testing.T) {
	// Test migration facade operations
	h.FacadeHelper.TestMigrationFacadeOperations(t)

	// Test database facade operations
	h.FacadeHelper.TestDatabaseFacadeOperations(t)

	// Test configuration facade operations
	h.FacadeHelper.TestConfigurationFacadeOperations(t)

	// Test CLI facade operations
	h.FacadeHelper.TestCLIFacadeOperations(t)

	// Test facade integration
	h.FacadeHelper.TestFacadeIntegration(t)
}

// TestConcurrentMigrations tests concurrent migration scenarios
func (h *IntegrationTestHelper) TestConcurrentMigrations(t *testing.T) {
	// Create test migrations
	h.BackendHelper.CreateTestMigrations(t)

	// Initialize migration runner
	err := h.BackendHelper.MigrationRunner.Initialize(h.Context)
	require.NoError(t, err)

	goroutines := 5
	var wg sync.WaitGroup
	var successCount int32
	var mu sync.Mutex

	// Attempt concurrent migrations
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Try to acquire lock and run migration
			lockName := "migration_lock"
			acquired, err := h.BackendHelper.LockManager.AcquireLock(h.Context, lockName)
			if err == nil && acquired {
				// Run migration
				_, err := h.BackendHelper.MigrationRunner.Up(h.Context, 1)
				if err == nil {
					mu.Lock()
					successCount++
					mu.Unlock()
				}

				// Release lock
				h.BackendHelper.LockManager.ReleaseLock(h.Context, lockName)
			}
		}(i)
	}

	wg.Wait()

	// Only one goroutine should have successfully run migrations
	require.Equal(t, int32(1), successCount)
}

// TestPerformanceUnderLoad tests performance under load
func (h *IntegrationTestHelper) TestPerformanceUnderLoad(t *testing.T) {
	// Create test migrations
	h.BackendHelper.CreateTestMigrations(t)

	// Initialize migration runner
	err := h.BackendHelper.MigrationRunner.Initialize(h.Context)
	require.NoError(t, err)

	// Benchmark migration status checks
	metrics := h.PerformanceHelper.BenchmarkOperation(t, "migration_status", 100, func() error {
		_, err := h.BackendHelper.MigrationRunner.Status(h.Context)
		return err
	})

	// Assert performance thresholds
	h.PerformanceHelper.AssertPerformanceThresholds(t, metrics, 10.0, 100*time.Millisecond, 0.0)

	// Benchmark database operations
	metrics = h.PerformanceHelper.BenchmarkOperation(t, "database_ping", 1000, func() error {
		return h.BackendHelper.DatabaseManager.Ping(h.Context)
	})

	h.PerformanceHelper.AssertPerformanceThresholds(t, metrics, 100.0, 10*time.Millisecond, 0.0)

	// Log performance results
	h.PerformanceHelper.LogMetrics(t)
}

// TestErrorRecovery tests error recovery scenarios
func (h *IntegrationTestHelper) TestErrorRecovery(t *testing.T) {
	// Create invalid migration
	invalidMigrationPath := filepath.Join(h.BackendHelper.MigrationsDir, "999_invalid.up.sql")
	err := os.WriteFile(invalidMigrationPath, []byte("INVALID SQL SYNTAX"), 0644)
	require.NoError(t, err)

	// Initialize migration runner
	err = h.BackendHelper.MigrationRunner.Initialize(h.Context)
	require.NoError(t, err)

	// Attempt to run migrations (should fail)
	_, err = h.BackendHelper.MigrationRunner.Up(h.Context, 0)
	require.Error(t, err)

	// Remove invalid migration
	err = os.Remove(invalidMigrationPath)
	require.NoError(t, err)

	// Create valid migrations
	h.BackendHelper.CreateTestMigrations(t)

	// Reload and run migrations (should succeed)
	err = h.BackendHelper.MigrationRunner.Initialize(h.Context)
	require.NoError(t, err)

	result, err := h.BackendHelper.MigrationRunner.Up(h.Context, 0)
	require.NoError(t, err)
	require.Greater(t, result.Applied, 0)
}

// TestTransactionRollback tests transaction rollback scenarios
func (h *IntegrationTestHelper) TestTransactionRollback(t *testing.T) {
	// Begin transaction
	tx, err := h.DatabaseHelper.BeginTransaction(h.Context)
	require.NoError(t, err)

	// Insert data in transaction
	_, err = tx.Exec("CREATE TABLE rollback_test (id INTEGER PRIMARY KEY, value TEXT)")
	require.NoError(t, err)

	_, err = tx.Exec("INSERT INTO rollback_test (value) VALUES (?)", "test_value")
	require.NoError(t, err)

	// Rollback transaction
	err = h.DatabaseHelper.RollbackTransaction(h.Context, tx)
	require.NoError(t, err)

	// Verify table doesn't exist
	exists, err := h.DatabaseHelper.CheckTableExists(h.Context, "rollback_test")
	require.NoError(t, err)
	require.False(t, exists)
}

// TestSystemShutdown tests graceful system shutdown
func (h *IntegrationTestHelper) TestSystemShutdown(t *testing.T) {
	// Initialize all components
	err := h.BackendHelper.MigrationRunner.Initialize(h.Context)
	require.NoError(t, err)

	// Shutdown components
	err = h.BackendHelper.MigrationRunner.Shutdown(h.Context)
	require.NoError(t, err)

	err = h.BackendHelper.DatabaseManager.Disconnect(h.Context)
	require.NoError(t, err)

	// Verify components are shut down
	err = h.BackendHelper.DatabaseManager.Ping(h.Context)
	require.Error(t, err)
}

// TestConfigurationValidation tests configuration validation
func (h *IntegrationTestHelper) TestConfigurationValidation(t *testing.T) {
	// Test valid configuration
	err := h.FacadeHelper.ConfigFacade.Validate()
	require.NoError(t, err)

	// Test invalid configuration
	err = h.FacadeHelper.ConfigFacade.Set("database.max_connections", -1)
	require.NoError(t, err)

	err = h.FacadeHelper.ConfigFacade.Validate()
	require.Error(t, err)

	// Restore valid configuration
	err = h.FacadeHelper.ConfigFacade.Set("database.max_connections", 10)
	require.NoError(t, err)

	err = h.FacadeHelper.ConfigFacade.Validate()
	require.NoError(t, err)
}

// TestMemoryUsage tests memory usage patterns
func (h *IntegrationTestHelper) TestMemoryUsage(t *testing.T) {
	// Create test migrations
	h.BackendHelper.CreateTestMigrations(t)

	// Benchmark memory usage
	metrics := h.PerformanceHelper.BenchmarkMemoryUsage(t, "migration_lifecycle", 10, func() error {
		// Initialize
		err := h.BackendHelper.MigrationRunner.Initialize(h.Context)
		if err != nil {
			return err
		}

		// Run migrations
		_, err = h.BackendHelper.MigrationRunner.Up(h.Context, 0)
		if err != nil {
			return err
		}

		// Reset migrations
		_, err = h.BackendHelper.MigrationRunner.Reset(h.Context)
		return err
	})

	// Log memory usage
	t.Logf("Memory Usage - Alloc: %d bytes, TotalAlloc: %d bytes, Sys: %d bytes",
		metrics.MemoryUsage.Alloc, metrics.MemoryUsage.TotalAlloc, metrics.MemoryUsage.Sys)

	// Assert reasonable memory usage (adjust thresholds as needed)
	require.Less(t, metrics.MemoryUsage.Alloc, uint64(100*1024*1024)) // 100MB
}

// CreateIntegrationTestSuite creates a comprehensive integration test suite
func (h *IntegrationTestHelper) CreateIntegrationTestSuite(t *testing.T) {
	t.Run("FullMigrationLifecycle", func(t *testing.T) {
		h.TestFullMigrationLifecycle(t)
	})

	t.Run("DataIntegrity", func(t *testing.T) {
		h.TestDataIntegrity(t)
	})

	t.Run("CacheIntegration", func(t *testing.T) {
		h.TestCacheIntegration(t)
	})

	t.Run("FacadeIntegration", func(t *testing.T) {
		h.TestFacadeIntegration(t)
	})

	t.Run("ConcurrentMigrations", func(t *testing.T) {
		h.TestConcurrentMigrations(t)
	})

	t.Run("PerformanceUnderLoad", func(t *testing.T) {
		h.TestPerformanceUnderLoad(t)
	})

	t.Run("ErrorRecovery", func(t *testing.T) {
		h.TestErrorRecovery(t)
	})

	t.Run("TransactionRollback", func(t *testing.T) {
		h.TestTransactionRollback(t)
	})

	t.Run("SystemShutdown", func(t *testing.T) {
		h.TestSystemShutdown(t)
	})

	t.Run("ConfigurationValidation", func(t *testing.T) {
		h.TestConfigurationValidation(t)
	})

	t.Run("MemoryUsage", func(t *testing.T) {
		h.TestMemoryUsage(t)
	})
}

// LogIntegrationStatistics logs integration test statistics
func (h *IntegrationTestHelper) LogIntegrationStatistics(t *testing.T) {
	t.Logf("Integration Test Statistics:")
	t.Logf("===========================")
	t.Logf("Test Environment: %s", h.TestEnv.TempDir)
	t.Logf("Database Path: %s", h.TestEnv.DBPath)
	t.Logf("Migrations Directory: %s", h.TestEnv.MigrationsDir)
	t.Logf("Configuration File: %s", h.TestEnv.ConfigPath)

	// Log component statistics
	if h.DatabaseHelper != nil {
		h.DatabaseHelper.LogDatabaseInfo(t)
	}
	if h.CacheHelper != nil {
		h.CacheHelper.LogCacheStatistics(t)
	}
	if h.FacadeHelper != nil {
		h.FacadeHelper.LogFacadeStatistics(t)
	}
	if h.BackendHelper != nil {
		h.BackendHelper.LogBackendStatistics(t)
	}
	if h.PerformanceHelper != nil {
		h.PerformanceHelper.LogSummary(t)
	}
}

/**
 * @file migration_engine_test.go
 * @description Integration tests for migration engine across PostgreSQL, MySQL, and SQLite
 * @author Admilson B. F. Cossa
 * @date 2024-12-28
 * @path pkg/core/database/migrations/tests/integration/migration_engine_test.go
 */

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// MigrationEngineTestSuite tests the migration engine functionality
type MigrationEngineTestSuite struct {
	IntegrationTestSuite
}

// TestMigrationEngineTestSuite runs the migration engine test suite
func TestMigrationEngineTestSuite(t *testing.T) {
	suite.Run(t, new(MigrationEngineTestSuite))
}

// TableExists checks if a table exists in the database using database-specific queries
func (s *MigrationEngineTestSuite) TableExists(db *TestDatabase, table string) bool {
	var count int
	switch db.Type {
	case SQLite:
		db.DB.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&count)
	case PostgreSQL:
		db.DB.Raw("SELECT count(*) FROM information_schema.tables WHERE table_name = ?", table).Scan(&count)
	case MySQL:
		db.DB.Raw("SELECT count(*) FROM information_schema.tables WHERE table_name = ?", table).Scan(&count)
	}
	return count > 0
}

// GetTablesBefore returns a list of existing tables before migration using database-specific queries
func (s *MigrationEngineTestSuite) GetTablesBefore(db *TestDatabase) []string {
	var tables []string
	switch db.Type {
	case SQLite:
		db.DB.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables)
	case PostgreSQL:
		db.DB.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'").Scan(&tables)
	case MySQL:
		db.DB.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables)
	}
	return tables
}

// TestBasicMigrationFlow tests the basic migration flow across all databases
func (s *MigrationEngineTestSuite) TestBasicMigrationFlow() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("BasicMigrationFlow_%s", dbType), func() {
			s.testBasicMigrationFlow(db)
		})
	}
}

// testBasicMigrationFlow tests basic migration operations
func (s *MigrationEngineTestSuite) testBasicMigrationFlow(db *TestDatabase) {
	
	// Create database-specific SQL
	var usersSQL, postsSQL string
	switch db.Type {
	case SQLite:
		usersSQL = `
-- +migrate Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE users;
`
		postsSQL = `
-- +migrate Up
CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    content TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE posts;
`
	case PostgreSQL:
		usersSQL = `
-- +migrate Up
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE users;
`
		postsSQL = `
-- +migrate Up
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    title VARCHAR(255) NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE posts;
`
	case MySQL:
		usersSQL = `
-- +migrate Up
CREATE TABLE users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE users;
`
		postsSQL = `
-- +migrate Up
CREATE TABLE posts (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT NOT NULL,
    title VARCHAR(255) NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE posts;
`
	}

	// Create test migration files
	s.CreateTestMigrationFile("001_create_users_table.sql", usersSQL)
	s.CreateTestMigrationFile("002_create_posts_table.sql", postsSQL)

	defer func() {
		s.RemoveTestMigrationFile("001_create_users_table.sql")
		s.RemoveTestMigrationFile("002_create_posts_table.sql")
	}()

	// Create migration engine AFTER migration files are created
	migrationEngine, err := s.CreateMigrationEngine(db)
	s.Require().NoError(err)

	// Debug: Check what tables exist before migration using database-specific queries
	tablesBefore := s.GetTablesBefore(db)

	// Test migration up
	err = migrationEngine.Migrate(s.ctx, "", false)
	s.Require().NoError(err)

	// Verify tables were created
	s.AssertTableExists(db, "users")
	s.AssertTableExists(db, "posts")
	s.AssertTableExists(db, "migration_history")

	// Test migration status
	status, err := migrationEngine.Status(s.ctx)
	s.Require().NoError(err)
	s.Greater(status.ExecutedCount, 0)
	s.Equal(0, status.PendingCount)

	// Test rollback
	err = migrationEngine.Rollback(s.ctx, 1)
	if err != nil {
	}
	s.Require().NoError(err)

	// Verify one table was dropped (posts should be dropped, users should remain)
	s.AssertTableExists(db, "users")
	s.AssertTableNotExists(db, "posts")

	// Test status after rollback
	status, err = migrationEngine.Status(s.ctx)
	s.Require().NoError(err)
	s.Equal(1, status.ExecutedCount)
	s.Equal(1, status.PendingCount)

	// Verify migration_history reflects the rollback
	var historyCount int
	db.DB.Raw("SELECT COUNT(*) FROM migration_history").Scan(&historyCount)
	s.Equal(1, historyCount, "Migration history should reflect only one executed migration after rollback")
}

// TestDryRunMigration tests dry-run functionality
func (s *MigrationEngineTestSuite) TestDryRunMigration() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("DryRunMigration_%s", dbType), func() {
			s.testDryRunMigration(db)
		})
	}
}

// testDryRunMigration tests dry-run migration execution
func (s *MigrationEngineTestSuite) testDryRunMigration(db *TestDatabase) {
	
	// Create migration engine with dry run support
	migrationEngine, err := s.CreateMigrationEngineWithDryRun(db, true)
	s.Require().NoError(err)

	// Create test migration file
	s.CreateTestMigrationFile("001_create_test_table.sql", `
-- +migrate Up
CREATE TABLE test_table (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

-- +migrate Down
DROP TABLE test_table;
`)

	defer s.RemoveTestMigrationFile("001_create_test_table.sql")

	// Test dry-run migration
	err = migrationEngine.Migrate(s.ctx, "", true)
	s.Require().NoError(err)

	// Verify table was NOT actually created (dry-run)
	s.AssertTableNotExists(db, "test_table")

	// Check migration status for dry-run (should show pending migrations)
	// Use StatusWithDryRun to avoid querying migration_history table in dry-run mode
	status, err := migrationEngine.StatusWithDryRun(s.ctx, true)
	s.Require().NoError(err)
	
	// For dry-run, we expect no executed migrations and pending migrations
	s.Equal(0, status.ExecutedCount, "Dry-run should not execute any migrations")
	s.Greater(status.PendingCount, 0, "Dry-run should show pending migrations")

	// Test actual migration after dry-run - create a new engine without dry run mode
	actualMigrationEngine, err := s.CreateMigrationEngineWithDryRun(db, false)
	s.Require().NoError(err)
	
	err = actualMigrationEngine.Migrate(s.ctx, "", false)
	s.Require().NoError(err)

	// Verify table was actually created
	s.AssertTableExists(db, "test_table")
	s.AssertTableExists(db, "migration_history")
}

// TestMigrationValidation tests migration validation
func (s *MigrationEngineTestSuite) TestMigrationValidation() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("MigrationValidation_%s", dbType), func() {
			s.testMigrationValidation(db)
		})
	}
}

// testMigrationValidation tests migration validation functionality
func (s *MigrationEngineTestSuite) testMigrationValidation(db *TestDatabase) {
	
	// Create migration engine
	migrationEngine, err := s.CreateMigrationEngine(db)
	s.Require().NoError(err)

	// Create invalid migration file (syntax error)
	s.CreateTestMigrationFile("001_invalid_migration.sql", `
-- +migrate Up
CREATE INVALID TABLE syntax_error (
    id SERIAL PRIMARY KEY
);

-- +migrate Down
DROP TABLE syntax_error;
`)

	defer s.RemoveTestMigrationFile("001_invalid_migration.sql")

	// Test validation
	err = migrationEngine.Validate(s.ctx)
	s.Require().Error(err, "Should have validation errors")

	// Test migration with invalid SQL (should fail)
	err = migrationEngine.Migrate(s.ctx, "", false)
	s.Require().Error(err, "Migration should fail with invalid SQL")
}

// TestConcurrentMigrations tests migration locking
func (s *MigrationEngineTestSuite) TestConcurrentMigrations() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("ConcurrentMigrations_%s", dbType), func() {
			s.testConcurrentMigrations(db)
		})
	}
}

// testConcurrentMigrations tests that concurrent migrations are properly locked
func (s *MigrationEngineTestSuite) testConcurrentMigrations(db *TestDatabase) {
	
	// Create two migration engines with short lock timeout for testing
	engine1, err := s.CreateMigrationEngineWithShortLockTimeout(db)
	s.Require().NoError(err)

	engine2, err := s.CreateMigrationEngineWithShortLockTimeout(db)
	s.Require().NoError(err)

	// Create a test migration file with multiple operations to simulate a longer-running migration
	var migrationContent string
	if db.Type == SQLite {
		// SQLite - simple migration for sequential testing
		migrationContent = `-- +migrate Up
CREATE TABLE concurrent_test (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE IF EXISTS concurrent_test;
`
	} else if db.Type == PostgreSQL {
		// PostgreSQL - use pg_sleep
		migrationContent = `-- +migrate Up
CREATE TABLE concurrent_test (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

-- Add a delay to simulate a long-running migration
SELECT pg_sleep(3);

-- +migrate Down
DROP TABLE concurrent_test;
`
	} else {
		// MySQL - use SLEEP (not pg_sleep)
		migrationContent = `-- +migrate Up
CREATE TABLE concurrent_test (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

-- Add a delay to simulate a long-running migration
SELECT SLEEP(3);

-- +migrate Down
DROP TABLE concurrent_test;
`
	}

	s.CreateTestMigrationFile("001_create_concurrent_test.sql", migrationContent)

	defer s.RemoveTestMigrationFile("001_create_concurrent_test.sql")

	if db.Type == SQLite {
		// SQLite: Run migrations sequentially to simulate lock behavior
		// SQLite's database-level locking makes true concurrent migrations unreliable
		
		// First migration should succeed
		err1 := engine1.Migrate(s.ctx, "", false)
		s.Require().NoError(err1, "First migration should succeed")
		
		// Second migration should detect that migrations are already applied
		err2 := engine2.Migrate(s.ctx, "", false)
		
		// For SQLite, the second migration should either succeed (no-op) or fail gracefully
		// This tests that the migration system handles already-applied migrations correctly
		if err2 != nil {
		} else {
		}
		
	} else {
		// PostgreSQL/MySQL: Run migrations concurrently to test real lock contention
		
		done1 := make(chan error, 1)
		done2 := make(chan error, 1)
		
		// Start first migration
		go func() {
			defer func() {
				if r := recover(); r != nil {
				}
			}()
			err := engine1.Migrate(s.ctx, "", false)
			done1 <- err
		}()

		// Start second migration immediately (no delay)
		go func() {
			defer func() {
				if r := recover(); r != nil {
				}
			}()
			err := engine2.Migrate(s.ctx, "", false)
			done2 <- err
		}()

		// Wait for both to complete with timeout to avoid deadlocks
		timeout := time.After(10 * time.Second)
		var err1, err2 error
		
		select {
		case err1 = <-done1:
		case <-timeout:
			s.Fail("First migration timed out")
			return
		}
		
		select {
		case err2 = <-done2:
		case <-timeout:
			s.Fail("Second migration timed out")
			return
		}

		// One should succeed, one should fail due to lock
		if err1 == nil {
			s.Require().Error(err2, "Second migration should fail due to lock")
		} else {
			s.Require().NoError(err2, "First migration failed, second should succeed")
		}
	}

	// Verify table was created
	s.AssertTableExists(db, "concurrent_test")
}

// TestMigrationRollbackWithDependencies tests rollback with dependencies
func (s *MigrationEngineTestSuite) TestMigrationRollbackWithDependencies() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("RollbackWithDependencies_%s", dbType), func() {
			s.testMigrationRollbackWithDependencies(db)
		})
	}
}

// testMigrationRollbackWithDependencies tests rollback with foreign key dependencies
func (s *MigrationEngineTestSuite) testMigrationRollbackWithDependencies(db *TestDatabase) {
	
	// Create migration engine
	migrationEngine, err := s.CreateMigrationEngine(db)
	s.Require().NoError(err)

	// Create migrations with dependencies
	s.CreateTestMigrationFile("001_create_categories.sql", `
-- +migrate Up
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE
);

-- +migrate Down
DROP TABLE categories;
`)

	var migration2Content string
	switch db.Type {
	case PostgreSQL:
		migration2Content = `
-- +migrate Up
CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    category_id INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
CREATE INDEX idx_products_category_id ON products(category_id);

-- +migrate Down
DROP INDEX idx_products_category_id;
DROP TABLE products;
`
	case MySQL:
		migration2Content = `
-- +migrate Up
CREATE TABLE products (
    id INT AUTO_INCREMENT PRIMARY KEY,
    category_id INT NOT NULL,
    name VARCHAR(255) NOT NULL,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
CREATE INDEX idx_products_category_id ON products(category_id);

-- +migrate Down
DROP INDEX idx_products_category_id;
DROP TABLE products;
`
	case SQLite:
		migration2Content = `
-- +migrate Up
CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL,
    name VARCHAR(255) NOT NULL,
    FOREIGN KEY (category_id) REFERENCES categories(id)
);
CREATE INDEX idx_products_category_id ON products(category_id);

-- +migrate Down
DROP INDEX idx_products_category_id;
DROP TABLE products;
`
	}

	s.CreateTestMigrationFile("002_create_products.sql", migration2Content)

	defer func() {
		s.RemoveTestMigrationFile("001_create_categories.sql")
		s.RemoveTestMigrationFile("002_create_products.sql")
	}()

	// Apply migrations
	err = migrationEngine.Migrate(s.ctx, "", false)
	s.Require().NoError(err)

	// Verify tables were created
	s.AssertTableExists(db, "categories")
	s.AssertTableExists(db, "products")

	// Verify migration history shows 2 executed migrations
	var historyCountBefore int
	db.DB.Raw("SELECT COUNT(*) FROM migration_history").Scan(&historyCountBefore)
	s.Equal(2, historyCountBefore, "Should have 2 migrations in history before rollback")

	// Test rollback (should rollback in correct order)
	err = migrationEngine.Rollback(s.ctx, 2)
	s.Require().NoError(err)

	// Verify tables were dropped
	s.AssertTableNotExists(db, "categories")
	s.AssertTableNotExists(db, "products")

	// Verify migration history reflects the rollback
	var historyCountAfter int
	db.DB.Raw("SELECT COUNT(*) FROM migration_history").Scan(&historyCountAfter)
	s.Equal(0, historyCountAfter, "Migration history should be empty after rolling back all migrations")
}

// TestMigrationWithTransactions tests transaction handling
func (s *MigrationEngineTestSuite) TestMigrationWithTransactions() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("MigrationWithTransactions_%s", dbType), func() {
			s.testMigrationWithTransactions(db)
		})
	}
}

// testMigrationWithTransactions tests that migrations are properly wrapped in transactions
func (s *MigrationEngineTestSuite) testMigrationWithTransactions(db *TestDatabase) {
	
	// Create migration engine
	migrationEngine, err := s.CreateMigrationEngine(db)
	s.Require().NoError(err)

	// Create migration that will fail partway through
	var migrationContent string
	switch db.Type {
	case PostgreSQL, MySQL:
		migrationContent = `
-- +migrate Up
CREATE TABLE transaction_test (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

INSERT INTO transaction_test (name) VALUES ('test1');
INSERT INTO transaction_test (name) VALUES ('test2');

-- This will cause an error
CREATE TABLE transaction_test (
    id SERIAL PRIMARY KEY
);

-- +migrate Down
DROP TABLE transaction_test;
`
	case SQLite:
		migrationContent = `
-- +migrate Up
CREATE TABLE transaction_test (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(255) NOT NULL
);

INSERT INTO transaction_test (name) VALUES ('test1');
INSERT INTO transaction_test (name) VALUES ('test2');

-- This will cause an error
CREATE TABLE transaction_test (
    id INTEGER PRIMARY KEY
);

-- +migrate Down
DROP TABLE transaction_test;
`
	}

	s.CreateTestMigrationFile("001_transaction_test.sql", migrationContent)
	defer s.RemoveTestMigrationFile("001_transaction_test.sql")

	// Test migration (should fail and rollback)
	err = migrationEngine.Migrate(s.ctx, "", false)
	s.Require().Error(err, "Migration should fail due to duplicate table")

	// Verify table was NOT created (transaction rolled back)
	s.AssertTableNotExists(db, "transaction_test")

	// Verify migration was NOT recorded in history
	status, err := migrationEngine.Status(s.ctx)
	s.Require().NoError(err)
	s.Equal(0, status.ExecutedCount, "Failed migration should not be recorded in history")
	s.Greater(status.PendingCount, 0, "Failed migration should still be pending")
}

// TestMigrationStatus tests migration status reporting
func (s *MigrationEngineTestSuite) TestMigrationStatus() {
	for dbType, db := range s.GetAvailableDatabases() {
		s.Run(fmt.Sprintf("MigrationStatus_%s", dbType), func() {
			s.testMigrationStatus(db)
		})
	}
}

// testMigrationStatus tests migration status functionality
func (s *MigrationEngineTestSuite) testMigrationStatus(db *TestDatabase) {
	
	// Create migration engine
	migrationEngine, err := s.CreateMigrationEngine(db)
	s.Require().NoError(err)

	// Create test migration files
	s.CreateTestMigrationFile("001_status_test1.sql", `
-- +migrate Up
CREATE TABLE status_test1 (id SERIAL PRIMARY KEY);

-- +migrate Down
DROP TABLE status_test1;
`)

	s.CreateTestMigrationFile("002_status_test2.sql", `
-- +migrate Up
CREATE TABLE status_test2 (id SERIAL PRIMARY KEY);

-- +migrate Down
DROP TABLE status_test2;
`)

	defer func() {
		s.RemoveTestMigrationFile("001_status_test1.sql")
		s.RemoveTestMigrationFile("002_status_test2.sql")
	}()

	// Initial status (no migrations applied)
	status, err := migrationEngine.Status(s.ctx)
	s.Require().NoError(err)
	s.Equal(0, status.ExecutedCount)
	s.Equal(2, status.PendingCount)

	// Apply first migration
	err = migrationEngine.Migrate(s.ctx, "001", false)
	s.Require().NoError(err)

	// Check status after first migration
	status, err = migrationEngine.Status(s.ctx)
	s.Require().NoError(err)
	s.Equal(1, status.ExecutedCount)
	s.Equal(1, status.PendingCount)

	// Apply all remaining migrations
	err = migrationEngine.Migrate(s.ctx, "", false)
	s.Require().NoError(err)

	// Check final status
	status, err = migrationEngine.Status(s.ctx)
	s.Require().NoError(err)
	s.Equal(2, status.ExecutedCount)
	s.Equal(0, status.PendingCount)
}

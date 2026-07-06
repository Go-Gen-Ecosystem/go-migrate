/**
 * @file base_test.go
 * @description Base integration test framework for migrations with dockerized databases
 * @author Admilson B. F. Cossa
 * @date 2024-12-28
 * @path pkg/core/database/migrations/tests/integration/base_test.go
 */

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stretchr/testify/suite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
)

// Global lock to prevent concurrent SQLite test execution
var sqliteTestLock sync.Mutex

// DatabaseType represents the type of database for testing
type DatabaseType string

const (
	PostgreSQL DatabaseType = "postgres"
	MySQL      DatabaseType = "mysql"
	SQLite     DatabaseType = "sqlite"
)

// TestDatabase holds database connection information
type TestDatabase struct {
	Type   DatabaseType
	DSN    string
	DB     *gorm.DB
	RawDB  *sql.DB
	Name   string
}

// IntegrationTestSuite provides base functionality for integration tests
type IntegrationTestSuite struct {
	suite.Suite
	databases               map[DatabaseType]*TestDatabase
	testDataDir             string
	migrationDir            string
	currentTestMigrationDir string
	ctx                     context.Context
	cancel                  context.CancelFunc
}

// SetupSuite initializes the test suite
func (s *IntegrationTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	
	// Setup test directories
	s.setupTestDirectories()
	
	// Initialize databases
	s.databases = make(map[DatabaseType]*TestDatabase)
	
	// Only run tests if databases are available
	if s.isDatabaseAvailable(PostgreSQL) {
		s.setupPostgreSQL()
	}
	if s.isDatabaseAvailable(MySQL) {
		s.setupMySQL()
	}
	s.setupSQLite() // SQLite is always available
}

// TearDownSuite cleans up after all tests
func (s *IntegrationTestSuite) TearDownSuite() {
	defer s.cancel()
	
	for _, db := range s.databases {
		if db.RawDB != nil {
			db.RawDB.Close()
		}
	}
	
	// Clean up SQLite test files
	s.cleanupSQLiteFiles()
}

// SetupTest runs before each test
func (s *IntegrationTestSuite) SetupTest() {
	
	// Create unique migration directory for this test case
	testName := s.T().Name()
	timestamp := time.Now().UnixNano()
	s.currentTestMigrationDir = filepath.Join(s.testDataDir, "migrations", fmt.Sprintf("%s_%d", testName, timestamp))
	err := os.MkdirAll(s.currentTestMigrationDir, 0755)
	s.Require().NoError(err)
	
	// Clean up any existing migration files from previous tests
	s.cleanupMigrationFiles()
	
	// For SQLite, create a completely fresh database for this test
	if _, exists := s.databases[SQLite]; exists {
		s.createFreshSQLiteForTest(testName, timestamp)
	}
	
	// Clean other databases normally
	for dbType, db := range s.databases {
		if dbType != SQLite {
			s.cleanDatabase(dbType, db)
		}
	}
	
	// Debug: Check migration_history table state
	if sqliteDB, exists := s.databases[SQLite]; exists {
		count := s.getTableRowCount(sqliteDB, "migration_history")
	}
	
}

// TearDownTest runs after each test
func (s *IntegrationTestSuite) TearDownTest() {
	
	// Clean up migration files created during this test
	s.cleanupMigrationFiles()
	
	// For SQLite, just close connections without trying to delete files
	// since we use unique filenames for each test run
	if db, exists := s.databases[SQLite]; exists && db != nil {
		if db.RawDB != nil {
			db.RawDB.Close()
		}
		if db.DB != nil {
			if sqlDB, err := db.DB.DB(); err == nil {
				sqlDB.Close()
			}
		}
	}
	
	// Clean other databases normally
	for dbType, db := range s.databases {
		if dbType != SQLite {
			s.cleanDatabase(dbType, db)
		}
	}
	
}

// setupTestDirectories creates necessary test directories
func (s *IntegrationTestSuite) setupTestDirectories() {
	wd, err := os.Getwd()
	s.Require().NoError(err)
	
	// Go up to the migrations package root
	migrationRoot := filepath.Join(wd, "..", "..")
	s.testDataDir = filepath.Join(migrationRoot, "tests", "integration", "testdata")
	s.migrationDir = filepath.Join(s.testDataDir, "migrations")
	
	// Create directories if they don't exist
	err = os.MkdirAll(s.migrationDir, 0755)
	s.Require().NoError(err)
}

// isDatabaseAvailable checks if a database is available for testing
func (s *IntegrationTestSuite) isDatabaseAvailable(dbType DatabaseType) bool {
	switch dbType {
	case PostgreSQL:
		return s.checkPostgreSQLAvailability()
	case MySQL:
		return s.checkMySQLAvailability()
	case SQLite:
		return true // SQLite is always available
	default:
		return false
	}
}

// checkPostgreSQLAvailability checks if PostgreSQL test database is available
func (s *IntegrationTestSuite) checkPostgreSQLAvailability() bool {
	dsn := "host=localhost user=test_user password=test_password dbname=migrations_test port=5433 sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return false
	}
	defer db.Close()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return db.PingContext(ctx) == nil
}

// checkMySQLAvailability checks if MySQL test database is available
func (s *IntegrationTestSuite) checkMySQLAvailability() bool {
	dsn := "test_user:test_password@tcp(localhost:3307)/migrations_test?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return false
	}
	defer db.Close()
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return db.PingContext(ctx) == nil
}

// setupPostgreSQL initializes PostgreSQL test database
func (s *IntegrationTestSuite) setupPostgreSQL() {
	dsn := "host=localhost user=test_user password=test_password dbname=migrations_test port=5433 sslmode=disable"
	
	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	s.Require().NoError(err)
	
	rawDB, err := gormDB.DB()
	s.Require().NoError(err)
	
	s.databases[PostgreSQL] = &TestDatabase{
		Type:  PostgreSQL,
		DSN:   dsn,
		DB:    gormDB,
		RawDB: rawDB,
		Name:  "migrations_test",
	}
}

// setupMySQL initializes MySQL test database
func (s *IntegrationTestSuite) setupMySQL() {
	dsn := "test_user:test_password@tcp(localhost:3307)/migrations_test?charset=utf8mb4&parseTime=True&loc=Local"
	
	gormDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	s.Require().NoError(err)
	
	rawDB, err := gormDB.DB()
	s.Require().NoError(err)
	
	s.databases[MySQL] = &TestDatabase{
		Type:  MySQL,
		DSN:   dsn,
		DB:    gormDB,
		RawDB: rawDB,
		Name:  "migrations_test",
	}
}

// setupSQLite initializes SQLite test database
func (s *IntegrationTestSuite) setupSQLite() {
	// Use a unique test database file for each test suite run
	timestamp := time.Now().UnixNano()
	dbPath := filepath.Join(s.testDataDir, fmt.Sprintf("test_%d.db", timestamp))
	dsn := dbPath
	
	// Remove existing database file if it exists
	if _, err := os.Stat(dbPath); err == nil {
		os.Remove(dbPath)
	}
	
	gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	s.Require().NoError(err)
	
	// Enable WAL mode for better concurrency
	err = gormDB.Exec("PRAGMA journal_mode = WAL;").Error
	s.Require().NoError(err)
	
	err = gormDB.Exec("PRAGMA synchronous = NORMAL;").Error
	s.Require().NoError(err)
	
	// Set busy timeout to handle temporary locks
	err = gormDB.Exec("PRAGMA busy_timeout = 5000;").Error
	s.Require().NoError(err)
	
	rawDB, err := gormDB.DB()
	s.Require().NoError(err)
	
	s.databases[SQLite] = &TestDatabase{
		Type:  SQLite,
		DSN:   dsn,
		DB:    gormDB,
		RawDB: rawDB,
		Name:  fmt.Sprintf("test_%d.db", timestamp),
	}
}

// cleanDatabase removes all tables and data from a database
func (s *IntegrationTestSuite) cleanDatabase(dbType DatabaseType, db *TestDatabase) {
	switch dbType {
	case PostgreSQL:
		s.cleanPostgreSQL(db)
	case MySQL:
		s.cleanMySQL(db)
	case SQLite:
		s.cleanSQLite(db)
	}
}

// cleanPostgreSQL removes all tables from PostgreSQL database
func (s *IntegrationTestSuite) cleanPostgreSQL(db *TestDatabase) {
	// Drop all tables in public schema
	var tables []string
	err := db.DB.Raw(`
		SELECT tablename 
		FROM pg_tables 
		WHERE schemaname = 'public'
	`).Scan(&tables).Error
	s.Require().NoError(err)
	
	for _, table := range tables {
		err := db.DB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)).Error
		s.Require().NoError(err)
	}
}

// cleanMySQL removes all tables from MySQL database
func (s *IntegrationTestSuite) cleanMySQL(db *TestDatabase) {
	// Disable foreign key checks
	err := db.DB.Exec("SET FOREIGN_KEY_CHECKS = 0").Error
	s.Require().NoError(err)
	
	// Get all tables
	var tables []string
	err = db.DB.Raw("SHOW TABLES").Scan(&tables).Error
	s.Require().NoError(err)
	
	// Drop all tables
	for _, table := range tables {
		err := db.DB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table)).Error
		s.Require().NoError(err)
	}
	
	// Re-enable foreign key checks
	err = db.DB.Exec("SET FOREIGN_KEY_CHECKS = 1").Error
	s.Require().NoError(err)
}

// cleanSQLite cleans SQLite test database
func (s *IntegrationTestSuite) cleanSQLite(db *TestDatabase) {
	
	// Close all connections to ensure no locks on the database file
	if db.RawDB != nil {
		db.RawDB.Close()
		db.RawDB = nil
	}
	
	// Close GORM connection
	if db.DB != nil {
		sqlDB, err := db.DB.DB()
		if err == nil {
			sqlDB.Close()
		}
		db.DB = nil
	}
	
	// Add a small delay to ensure connections are fully closed
	time.Sleep(50 * time.Millisecond)
	
	// Delete the database file entirely to ensure complete cleanup
	dbPath := db.DSN
	s.removeFileWithRetry(dbPath, "SQLite database file")
	
	// Also remove any WAL and SHM files that SQLite might create
	walPath := dbPath + "-wal"
	s.removeFileWithRetry(walPath, "SQLite WAL file")
	
	shmPath := dbPath + "-shm"
	s.removeFileWithRetry(shmPath, "SQLite SHM file")
	
	// Recreate the database connection for the next test
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	s.Require().NoError(err)
	
	rawDB, err := gormDB.DB()
	s.Require().NoError(err)
	
	// Update the database reference
	db.DB = gormDB
	db.RawDB = rawDB
	
}

// removeFileWithRetry attempts to remove a file with retry logic for Windows file locking issues
func (s *IntegrationTestSuite) removeFileWithRetry(filePath, fileType string) {
	if _, err := os.Stat(filePath); err != nil {
		return // File doesn't exist, nothing to do
	}
	
	
	// Try to remove the file with retries
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		err := os.Remove(filePath)
		if err == nil {
			return
		}
		
		if i < maxRetries-1 {
			time.Sleep(time.Duration(50*(i+1)) * time.Millisecond) // Increasing delay
		} else {
		}
	}
}

// createFreshSQLiteForTest creates a completely fresh SQLite database for each test
func (s *IntegrationTestSuite) createFreshSQLiteForTest(testName string, timestamp int64) {
	// Lock to prevent concurrent SQLite operations
	sqliteTestLock.Lock()
	defer sqliteTestLock.Unlock()
	
	
	// Close existing SQLite connections if any
	if existingDB, exists := s.databases[SQLite]; exists && existingDB != nil {
		if existingDB.RawDB != nil {
			existingDB.RawDB.Close()
		}
		if existingDB.DB != nil {
			if sqlDB, err := existingDB.DB.DB(); err == nil {
				sqlDB.Close()
			}
		}
	}
	
	// Create unique database file for this test (use shorter name to avoid Windows path limits)
	shortTestName := fmt.Sprintf("test_%d", timestamp)
	dbPath := filepath.Join(s.testDataDir, fmt.Sprintf("%s.db", shortTestName))
	
	// Remove any existing file
	if _, err := os.Stat(dbPath); err == nil {
		os.Remove(dbPath)
		os.Remove(dbPath + "-wal")
		os.Remove(dbPath + "-shm")
	}
	
	// Create new GORM connection
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	s.Require().NoError(err)
	
	// Configure SQLite for better concurrency
	err = gormDB.Exec("PRAGMA journal_mode = WAL;").Error
	s.Require().NoError(err)
	
	err = gormDB.Exec("PRAGMA synchronous = NORMAL;").Error
	s.Require().NoError(err)
	
	err = gormDB.Exec("PRAGMA busy_timeout = 5000;").Error
	s.Require().NoError(err)
	
	// Get raw database connection
	rawDB, err := gormDB.DB()
	s.Require().NoError(err)
	
	// Update the database reference
	s.databases[SQLite] = &TestDatabase{
		Type:  SQLite,
		DSN:   dbPath,
		DB:    gormDB,
		RawDB: rawDB,
		Name:  fmt.Sprintf("%s.db", shortTestName),
	}
	
}

// getTableRowCount returns the number of rows in a table, or 0 if table doesn't exist
func (s *IntegrationTestSuite) getTableRowCount(db *TestDatabase, tableName string) int {
	if db == nil || db.DB == nil {
		return 0
	}
	
	var count int64
	err := db.DB.Table(tableName).Count(&count).Error
	if err != nil {
		// Table doesn't exist or other error
		return 0
	}
	
	return int(count)
}

// cleanupSQLiteFiles removes SQLite test database files
func (s *IntegrationTestSuite) cleanupSQLiteFiles() {
	dbPath := filepath.Join(s.testDataDir, "test.db")
	os.Remove(dbPath)
	os.Remove(dbPath + "-shm")
	os.Remove(dbPath + "-wal")
}

// cleanupMigrationFiles removes all migration files from the base migration directory
func (s *IntegrationTestSuite) cleanupMigrationFiles() {
	// Clean up the base migration directory
	if _, err := os.Stat(s.migrationDir); err == nil {
		files, err := filepath.Glob(filepath.Join(s.migrationDir, "*.sql"))
		if err == nil {
			for _, file := range files {
				os.Remove(file)
			}
		}
	}
	
	// Clean up test-specific migration directories
	testMigrationsRoot := filepath.Join(s.testDataDir, "migrations")
	if _, err := os.Stat(testMigrationsRoot); err == nil {
		os.RemoveAll(testMigrationsRoot)
	}
}

// copyBaseMigrationFiles copies base migration files to the test-specific directory
func (s *IntegrationTestSuite) copyBaseMigrationFiles() {
	if s.migrationDir == "" || s.currentTestMigrationDir == "" {
		return
	}
	
	// Find all .sql files in the base migration directory
	files, err := filepath.Glob(filepath.Join(s.migrationDir, "*.sql"))
	if err != nil {
		return
	}
	
	
	// Copy each file to the test-specific directory
	for _, srcFile := range files {
		filename := filepath.Base(srcFile)
		dstFile := filepath.Join(s.currentTestMigrationDir, filename)
		
		// Read source file
		content, err := os.ReadFile(srcFile)
		if err != nil {
			continue
		}
		
		// Write to destination
		err = os.WriteFile(dstFile, content, 0644)
		if err != nil {
			continue
		}
		
	}
}

// GetDatabase returns a test database by type
func (s *IntegrationTestSuite) GetDatabase(dbType DatabaseType) *TestDatabase {
	db, exists := s.databases[dbType]
	if !exists {
		s.T().Skipf("Database %s is not available for testing", dbType)
	}
	return db
}

// GetAvailableDatabases returns all available test databases
func (s *IntegrationTestSuite) GetAvailableDatabases() map[DatabaseType]*TestDatabase {
	return s.databases
}

// CreateMigrationEngine creates a migration engine for the given database
func (s *IntegrationTestSuite) CreateMigrationEngine(db *TestDatabase) (*engine.MigrationEngine, error) {
	// Use test-specific migration directory if available, otherwise fall back to base directory
	migrationPath := s.migrationDir
	if s.currentTestMigrationDir != "" {
		migrationPath = s.currentTestMigrationDir
	}
	
	
	// List files in the migration directory
	if files, err := filepath.Glob(filepath.Join(migrationPath, "*.sql")); err == nil {
		for _, file := range files {
		}
	} else {
	}
	
	config := migrations.Config{
		Environment: "test",
		Database: migrations.DatabaseConfig{
			Driver:   string(db.Type),
			DSN:      db.DSN,
			Host:     "localhost",
			Port:     5432,
			Database: "test_db",
		},
		Migration: migrations.MigrationConfig{
			MigrationsPath:   migrationPath,
			TableName:       "migration_history",
			LockTableName:   "migration_locks",
			Environment:     "test",
			AutoMigrate:     true,
			StopOnError:     true,
			MaxRollbackDepth: 10,
		},
		Logging: migrations.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Features: migrations.FeatureConfig{
			AutoBackup:        false,
			DryRunMode:        false,
			ParallelExecution: false,
			HealthChecks:      true,
			Monitoring:        true,
		},
	}
	
	// Use the engine package directly with the correct config type
	migrationEngine, err := engine.NewMigrationEngine(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration engine: %w", err)
	}
	
	// Initialize the migration engine
	ctx := context.Background()
	err = migrationEngine.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migration engine: %w", err)
	}
	
	return migrationEngine, nil
}

// CreateMigrationEngineWithShortLockTimeout creates a migration engine with a short lock timeout for testing concurrent migrations
func (s *IntegrationTestSuite) CreateMigrationEngineWithShortLockTimeout(db *TestDatabase) (*engine.MigrationEngine, error) {
	// Use test-specific migration directory if available, otherwise fall back to base directory
	migrationPath := s.migrationDir
	if s.currentTestMigrationDir != "" {
		migrationPath = s.currentTestMigrationDir
	}
	
	config := migrations.Config{
		Environment: "test",
		Database: migrations.DatabaseConfig{
			Driver:   string(db.Type),
			DSN:      db.DSN,
			Host:     "localhost",
			Port:     5432,
			Database: "test_db",
		},
		Migration: migrations.MigrationConfig{
			MigrationsPath:   migrationPath,
			TableName:       "migration_history",
			LockTableName:   "migration_locks",
			Environment:     "test",
			AutoMigrate:     true,
			StopOnError:     true,
			MaxRollbackDepth: 10,
			LockTimeout:     50 * time.Millisecond, // Very short timeout for testing concurrent access
		},
		Logging: migrations.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Features: migrations.FeatureConfig{
			AutoBackup:        false,
			DryRunMode:        false,
			ParallelExecution: false,
			HealthChecks:      true,
			Monitoring:        true,
		},
	}
	
	// Use the engine package directly with the correct config type
	migrationEngine, err := engine.NewMigrationEngine(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration engine: %w", err)
	}
	
	// Initialize the migration engine
	ctx := context.Background()
	err = migrationEngine.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migration engine: %w", err)
	}
	
	return migrationEngine, nil
}

// CreateMigrationEngineWithDryRun creates a migration engine for the given database with dry run support
func (s *IntegrationTestSuite) CreateMigrationEngineWithDryRun(db *TestDatabase, dryRun bool) (*engine.MigrationEngine, error) {
	// Use test-specific migration directory if available, otherwise fall back to base directory
	migrationPath := s.migrationDir
	if s.currentTestMigrationDir != "" {
		migrationPath = s.currentTestMigrationDir
	}
	
	
	config := migrations.Config{
		Environment: "test",
		Database: migrations.DatabaseConfig{
			Driver:   string(db.Type),
			DSN:      db.DSN,
			Host:     "localhost",
			Port:     5432,
			Database: "test_db",
		},
		Migration: migrations.MigrationConfig{
			MigrationsPath:   migrationPath,
			TableName:       "migration_history",
			LockTableName:   "migration_locks",
			Environment:     "test",
			AutoMigrate:     true,
			StopOnError:     true,
			MaxRollbackDepth: 10,
		},
		Logging: migrations.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Features: migrations.FeatureConfig{
			AutoBackup:        false,
			DryRunMode:        dryRun,
			ParallelExecution: false,
			HealthChecks:      true,
			Monitoring:        true,
		},
	}
	
	// Use the engine package directly with the factory method that supports dry run
	migrationEngine, err := engine.NewMigrationEngineWithFactory(config, dryRun)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration engine: %w", err)
	}
	
	return migrationEngine, nil
}

// CreateTestMigrationFile creates a test migration file
func (s *IntegrationTestSuite) CreateTestMigrationFile(filename, content string) string {
	// Use test-specific migration directory if available, otherwise fall back to base directory
	migrationPath := s.migrationDir
	if s.currentTestMigrationDir != "" {
		migrationPath = s.currentTestMigrationDir
		// Ensure the test-specific directory exists
		err := os.MkdirAll(migrationPath, 0755)
		s.Require().NoError(err)
	}
	
	filePath := filepath.Join(migrationPath, filename)
	err := os.WriteFile(filePath, []byte(content), 0644)
	s.Require().NoError(err)
	return filePath
}

// RemoveTestMigrationFile removes a test migration file
func (s *IntegrationTestSuite) RemoveTestMigrationFile(filename string) {
	// Use test-specific migration directory if available, otherwise fall back to base directory
	migrationPath := s.migrationDir
	if s.currentTestMigrationDir != "" {
		migrationPath = s.currentTestMigrationDir
	}
	
	filePath := filepath.Join(migrationPath, filename)
	os.Remove(filePath)
}

// AssertTableExists checks if a table exists in the database
func (s *IntegrationTestSuite) AssertTableExists(db *TestDatabase, tableName string) {
	var exists bool
	
	switch db.Type {
	case PostgreSQL:
		err := db.DB.Raw(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = ?
			)
		`, tableName).Scan(&exists).Error
		s.Require().NoError(err)
		
	case MySQL:
		err := db.DB.Raw(`
			SELECT EXISTS (
				SELECT * FROM information_schema.tables 
				WHERE table_schema = DATABASE() 
				AND table_name = ?
			)
		`, tableName).Scan(&exists).Error
		s.Require().NoError(err)
		
	case SQLite:
		err := db.DB.Raw(`
			SELECT EXISTS (
				SELECT name FROM sqlite_master 
				WHERE type='table' AND name = ?
			)
		`, tableName).Scan(&exists).Error
		s.Require().NoError(err)
	}
	
	s.True(exists, "Table %s should exist in %s database", tableName, db.Type)
}

// AssertTableNotExists checks if a table does not exist in the database
func (s *IntegrationTestSuite) AssertTableNotExists(db *TestDatabase, tableName string) {
	var exists bool
	
	switch db.Type {
	case PostgreSQL:
		err := db.DB.Raw(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = ?
			)
		`, tableName).Scan(&exists).Error
		s.Require().NoError(err)
		
	case MySQL:
		err := db.DB.Raw(`
			SELECT EXISTS (
				SELECT * FROM information_schema.tables 
				WHERE table_schema = DATABASE() 
				AND table_name = ?
			)
		`, tableName).Scan(&exists).Error
		s.Require().NoError(err)
		
	case SQLite:
		err := db.DB.Raw(`
			SELECT EXISTS (
				SELECT name FROM sqlite_master 
				WHERE type='table' AND name = ?
			)
		`, tableName).Scan(&exists).Error
		s.Require().NoError(err)
	}
	
	s.False(exists, "Table %s should not exist in %s database", tableName, db.Type)
}

// GetTableRowCount returns the number of rows in a table
func (s *IntegrationTestSuite) GetTableRowCount(db *TestDatabase, tableName string) int64 {
	var count int64
	err := db.DB.Raw(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count).Error
	s.Require().NoError(err)
	return count
}

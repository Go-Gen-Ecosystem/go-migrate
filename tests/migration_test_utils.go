//go:build drifted_testutils
// +build drifted_testutils

/**
 * @file migration_test_utils.go
 * @description Comprehensive test utilities for migration system testing
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/tests/migration_test_utils.go
 */

package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate"
)

// TestEnvironment represents a complete test environment for migration testing
type TestEnvironment struct {
	TempDir         string
	DBPath          string
	MigrationsDir   string
	ConfigPath      string
	BackupDir       string
	CacheDir        string
	Context         context.Context
	Cancel          context.CancelFunc
	DB              *sql.DB
	MigrationTool   migrations.MigrationTool
}

// MigrationTestConfig contains configuration for migration tests
type MigrationTestConfig struct {
	DatabaseDriver    string
	MigrationsTable   string
	LockTimeout       time.Duration
	ConnectionTimeout time.Duration
	MaxConnections    int
	EnableCache       bool
	EnableMetrics     bool
	EnableEvents      bool
	LogLevel          string
	Environment       string
}

// DefaultMigrationTestConfig returns default test configuration
func DefaultMigrationTestConfig() *MigrationTestConfig {
	return &MigrationTestConfig{
		DatabaseDriver:    "sqlite3",
		MigrationsTable:   "schema_migrations",
		LockTimeout:       30 * time.Second,
		ConnectionTimeout: 10 * time.Second,
		MaxConnections:    10,
		EnableCache:       true,
		EnableMetrics:     true,
		EnableEvents:      true,
		LogLevel:          "debug",
		Environment:       "test",
	}
}

// SetupTestEnvironment creates a complete test environment
func SetupTestEnvironment(t *testing.T, config *MigrationTestConfig) *TestEnvironment {
	if config == nil {
		config = DefaultMigrationTestConfig()
	}

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "migration_test_*")
	require.NoError(t, err)

	env := &TestEnvironment{
		TempDir:       tempDir,
		DBPath:        filepath.Join(tempDir, "test.db"),
		MigrationsDir: filepath.Join(tempDir, "migrations"),
		ConfigPath:    filepath.Join(tempDir, "config.yaml"),
		BackupDir:     filepath.Join(tempDir, "backups"),
		CacheDir:      filepath.Join(tempDir, "cache"),
	}

	env.Context, env.Cancel = context.WithCancel(context.Background())

	// Create directories
	require.NoError(t, os.MkdirAll(env.MigrationsDir, 0755))
	require.NoError(t, os.MkdirAll(env.BackupDir, 0755))
	require.NoError(t, os.MkdirAll(env.CacheDir, 0755))

	// Create configuration file
	env.createConfigFile(t, config)

	// Initialize database
	env.initializeDatabase(t, config)

	// Initialize cache system
	if config.EnableCache {
		env.initializeCacheSystem(t, config)
	}

	// Initialize facade factory
	env.initializeFacadeFactory(t, config)

	return env
}

// TearDownTestEnvironment cleans up the test environment
func (env *TestEnvironment) TearDownTestEnvironment(t *testing.T) {
	if env.MigrationSystem != nil {
		status, _ := env.MigrationSystem.CheckStatus(env.Context)
		t.Logf("Final migration status: %+v", status)
	}

	if env.Factory != nil {
		err := env.Factory.Shutdown(env.Context)
		if err != nil {
			t.Logf("Warning: Factory shutdown error: %v", err)
		}
	}

	if env.CacheSystem != nil {
		err := env.CacheSystem.Shutdown(env.Context)
		if err != nil {
			t.Logf("Warning: Cache system shutdown error: %v", err)
		}
	}

	if env.DB != nil {
		err := env.DB.Close()
		if err != nil {
			t.Logf("Warning: Database close error: %v", err)
		}
	}

	if env.Cancel != nil {
		env.Cancel()
	}

	if env.TempDir != "" {
		err := os.RemoveAll(env.TempDir)
		if err != nil {
			t.Logf("Warning: Failed to remove temp dir: %v", err)
		}
	}
}

// createConfigFile creates a test configuration file
func (env *TestEnvironment) createConfigFile(t *testing.T, config *MigrationTestConfig) {
	configContent := fmt.Sprintf(`
# Test Migration Configuration
database:
  driver: %s
  dsn: %s
  max_connections: %d
  connection_timeout: %s
  idle_timeout: 5m
  max_lifetime: 1h

migrations:
  directory: %s
  table: %s
  lock_timeout: %s
  auto_create_table: true
  validate_checksums: true
  allow_missing: false
  allow_out_of_order: false

logging:
  level: %s
  format: json
  output: stdout
  include_caller: true

metrics:
  enabled: %t
  port: 9090
  path: /metrics

events:
  enabled: %t
  buffer_size: 1000
  flush_interval: 1s

cache:
  enabled: %t
  max_size: 1000
  ttl: 1h
  cleanup_interval: 5m

security:
  enabled: true
  validate_checksums: true

performance:
  batch_size: 100
  parallel_execution: false
  connection_pool_size: %d

backup:
  enabled: true
  directory: %s
  retention_days: 7

environment: %s
`,
		config.DatabaseDriver,
		env.DBPath,
		config.MaxConnections,
		config.ConnectionTimeout.String(),
		env.MigrationsDir,
		config.MigrationsTable,
		config.LockTimeout.String(),
		config.LogLevel,
		config.EnableMetrics,
		config.EnableEvents,
		config.EnableCache,
		config.MaxConnections,
		env.BackupDir,
		config.Environment,
	)

	err := os.WriteFile(env.ConfigPath, []byte(configContent), 0644)
	require.NoError(t, err)
}

// initializeDatabase initializes the test database
func (env *TestEnvironment) initializeDatabase(t *testing.T, config *MigrationTestConfig) {
	db, err := sql.Open(config.DatabaseDriver, env.DBPath)
	require.NoError(t, err)

	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxLifetime(1 * time.Hour)

	err = db.Ping()
	require.NoError(t, err)

	env.DB = db
}

// initializeCacheSystem initializes the cache system
func (env *TestEnvironment) initializeCacheSystem(t *testing.T, config *MigrationTestConfig) {
	cacheConfig := cache.GetTestingCacheSystemConfig()
	cacheConfig.StorageBasePath = env.CacheDir
	cacheConfig.Environment = config.Environment

	cacheFactory := cache.NewCacheSystemFactory(cacheConfig)
	cacheSystem, err := cacheFactory.CreateCacheSystem(env.Context)
	require.NoError(t, err)

	env.CacheSystem = cacheSystem
}

// initializeFacadeFactory initializes the facade factory
func (env *TestEnvironment) initializeFacadeFactory(t *testing.T, config *MigrationTestConfig) {
	factoryConfig := &facade.FactoryConfig{
		Environment: config.Environment,
		Migration: facade.MigrationConfig{
			Directory:       env.MigrationsDir,
			Table:           config.MigrationsTable,
			LockTimeout:     config.LockTimeout,
			AutoCreateTable: true,
		},
		Database: facade.DatabaseConfig{
			Driver:            config.DatabaseDriver,
			DSN:               env.DBPath,
			MaxConnections:    config.MaxConnections,
			ConnectionTimeout: config.ConnectionTimeout,
		},
		Configuration: facade.ConfigurationConfig{
			ConfigFile:   env.ConfigPath,
			Environment:  config.Environment,
			WatchChanges: false,
		},
		CLI: facade.CLIConfig{
			AppName:     "migration-test",
			Version:     "1.0.0",
			Description: "Migration test application",
		},
		Logging: facade.LoggingConfig{
			Level:  config.LogLevel,
			Format: "json",
			Output: "stdout",
		},
		Metrics: facade.MetricsConfig{
			Enabled: config.EnableMetrics,
			Port:    9090,
		},
		Events: facade.EventsConfig{
			Enabled:    config.EnableEvents,
			BufferSize: 1000,
		},
		Caching: facade.CacheConfig{
			Enabled: config.EnableCache,
			MaxSize: 1000,
			TTL:     1 * time.Hour,
		},
		Security: facade.SecurityConfig{
			Enabled: true,
		},
		Performance: facade.PerformanceConfig{
			BatchSize:         100,
			ParallelExecution: false,
		},
	}

	factory := facade.NewFacadeFactory(factoryConfig)
	require.NotNil(t, factory)

	err := factory.Initialize(env.Context)
	require.NoError(t, err)

	migrationSystem, err := factory.CreateMigrationSystem()
	require.NoError(t, err)

	env.Factory = factory
	env.MigrationSystem = migrationSystem
}

// MigrationTemplate represents a migration template
type MigrationTemplate struct {
	Version   string
	Name      string
	UpSQL     string
	DownSQL   string
	Tags      []string
	DependsOn []string
}

// CreateMigrationFiles creates migration files from templates
func (env *TestEnvironment) CreateMigrationFiles(t *testing.T, templates []MigrationTemplate) {
	for _, template := range templates {
		// Create up migration
		upFilename := fmt.Sprintf("%s_%s.up.sql", template.Version, template.Name)
		upPath := filepath.Join(env.MigrationsDir, upFilename)
		err := os.WriteFile(upPath, []byte(template.UpSQL), 0644)
		require.NoError(t, err)

		// Create down migration
		downFilename := fmt.Sprintf("%s_%s.down.sql", template.Version, template.Name)
		downPath := filepath.Join(env.MigrationsDir, downFilename)
		err = os.WriteFile(downPath, []byte(template.DownSQL), 0644)
		require.NoError(t, err)
	}
}

// GetStandardMigrationTemplates returns a set of standard migration templates for testing
func GetStandardMigrationTemplates() []MigrationTemplate {
	return []MigrationTemplate{
		{
			Version: "001",
			Name:    "create_users_table",
			UpSQL: `
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    is_active BOOLEAN DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(is_active);`,
			DownSQL: `
DROP INDEX IF EXISTS idx_users_active;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_users_username;
DROP TABLE IF EXISTS users;`,
			Tags: []string{"users", "core"},
		},
		{
			Version: "002",
			Name:    "create_posts_table",
			UpSQL: `
CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    title VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    content TEXT NOT NULL,
    excerpt TEXT,
    status VARCHAR(20) DEFAULT 'draft',
    is_featured BOOLEAN DEFAULT 0,
    published_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_posts_user_id ON posts(user_id);
CREATE INDEX idx_posts_status ON posts(status);
CREATE INDEX idx_posts_published_at ON posts(published_at);
CREATE UNIQUE INDEX idx_posts_slug ON posts(slug);`,
			DownSQL: `
DROP INDEX IF EXISTS idx_posts_slug;
DROP INDEX IF EXISTS idx_posts_published_at;
DROP INDEX IF EXISTS idx_posts_status;
DROP INDEX IF EXISTS idx_posts_user_id;
DROP TABLE IF EXISTS posts;`,
			Tags:      []string{"posts", "content"},
			DependsOn: []string{"001"},
		},
		{
			Version: "003",
			Name:    "create_comments_table",
			UpSQL: `
CREATE TABLE comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id INTEGER NOT NULL,
    user_id INTEGER,
    parent_id INTEGER,
    content TEXT NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    ip_address VARCHAR(45),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (parent_id) REFERENCES comments(id) ON DELETE CASCADE
);

CREATE INDEX idx_comments_post_id ON comments(post_id);
CREATE INDEX idx_comments_user_id ON comments(user_id);
CREATE INDEX idx_comments_parent_id ON comments(parent_id);
CREATE INDEX idx_comments_status ON comments(status);`,
			DownSQL: `
DROP INDEX IF EXISTS idx_comments_status;
DROP INDEX IF EXISTS idx_comments_parent_id;
DROP INDEX IF EXISTS idx_comments_user_id;
DROP INDEX IF EXISTS idx_comments_post_id;
DROP TABLE IF EXISTS comments;`,
			Tags:      []string{"comments", "content"},
			DependsOn: []string{"001", "002"},
		},
		{
			Version: "004",
			Name:    "add_user_profile_fields",
			UpSQL: `
ALTER TABLE users ADD COLUMN bio TEXT;
ALTER TABLE users ADD COLUMN avatar_url VARCHAR(500);
ALTER TABLE users ADD COLUMN website VARCHAR(255);
ALTER TABLE users ADD COLUMN location VARCHAR(255);
ALTER TABLE users ADD COLUMN birth_date DATE;`,
			DownSQL: `
-- SQLite doesn't support DROP COLUMN, so we recreate the table
CREATE TABLE users_backup AS SELECT 
    id, username, email, password_hash, first_name, last_name, 
    is_active, created_at, updated_at 
FROM users;

DROP TABLE users;

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    is_active BOOLEAN DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO users SELECT * FROM users_backup;
DROP TABLE users_backup;

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(is_active);`,
			Tags:      []string{"users", "profile"},
			DependsOn: []string{"001"},
		},
	}
}

// AssertTableExists asserts that a table exists in the database
func (env *TestEnvironment) AssertTableExists(t *testing.T, tableName string) {
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	err := env.DB.QueryRow(query, tableName).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "Table %s should exist", tableName)
}

// AssertTableNotExists asserts that a table does not exist in the database
func (env *TestEnvironment) AssertTableNotExists(t *testing.T, tableName string) {
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	err := env.DB.QueryRow(query, tableName).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "Table %s should not exist", tableName)
}

// AssertIndexExists asserts that an index exists in the database
func (env *TestEnvironment) AssertIndexExists(t *testing.T, indexName string) {
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?"
	err := env.DB.QueryRow(query, indexName).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "Index %s should exist", indexName)
}

// AssertMigrationApplied asserts that a migration has been applied
func (env *TestEnvironment) AssertMigrationApplied(t *testing.T, version string) {
	migrationFacade := env.Factory.GetMigrationFacade()
	status, err := migrationFacade.Status(env.Context)
	require.NoError(t, err)

	for _, applied := range status.Applied {
		if applied.Version == version {
			return // Found the migration
		}
	}

	t.Fatalf("Migration %s should be applied", version)
}

// AssertMigrationNotApplied asserts that a migration has not been applied
func (env *TestEnvironment) AssertMigrationNotApplied(t *testing.T, version string) {
	migrationFacade := env.Factory.GetMigrationFacade()
	status, err := migrationFacade.Status(env.Context)
	require.NoError(t, err)

	for _, applied := range status.Applied {
		if applied.Version == version {
			t.Fatalf("Migration %s should not be applied", version)
		}
	}
}

// InsertTestData inserts test data into the database
func (env *TestEnvironment) InsertTestData(t *testing.T) map[string]int64 {
	ids := make(map[string]int64)

	// Insert test user
	result, err := env.DB.Exec(`
		INSERT INTO users (username, email, password_hash, first_name, last_name) 
		VALUES (?, ?, ?, ?, ?)`,
		"testuser", "test@example.com", "hashed_password", "Test", "User")
	require.NoError(t, err)

	userID, err := result.LastInsertId()
	require.NoError(t, err)
	ids["user"] = userID

	// Insert test post
	result, err = env.DB.Exec(`
		INSERT INTO posts (user_id, title, slug, content, status, published_at) 
		VALUES (?, ?, ?, ?, ?, ?)`,
		userID, "Test Post", "test-post", "This is a test post content",
		"published", time.Now())
	require.NoError(t, err)

	postID, err := result.LastInsertId()
	require.NoError(t, err)
	ids["post"] = postID

	// Insert test comment
	result, err = env.DB.Exec(`
		INSERT INTO comments (post_id, user_id, content, status) 
		VALUES (?, ?, ?, ?)`,
		postID, userID, "This is a test comment", "approved")
	require.NoError(t, err)

	commentID, err := result.LastInsertId()
	require.NoError(t, err)
	ids["comment"] = commentID

	return ids
}

// VerifyDataIntegrity verifies data integrity and relationships
func (env *TestEnvironment) VerifyDataIntegrity(t *testing.T) {
	// Verify user-post relationship
	var count int
	err := env.DB.QueryRow(`
		SELECT COUNT(*) 
		FROM posts p 
		JOIN users u ON p.user_id = u.id 
		WHERE u.username = ?`, "testuser").Scan(&count)
	require.NoError(t, err)
	require.Greater(t, count, 0, "Should have posts from test user")

	// Verify post-comment relationship
	err = env.DB.QueryRow(`
		SELECT COUNT(*) 
		FROM comments c 
		JOIN posts p ON c.post_id = p.id 
		JOIN users u ON p.user_id = u.id 
		WHERE u.username = ?`, "testuser").Scan(&count)
	require.NoError(t, err)
	require.Greater(t, count, 0, "Should have comments on test user's posts")
}

// TestCascadeDelete tests cascade delete functionality
func (env *TestEnvironment) TestCascadeDelete(t *testing.T, userID int64) {
	// Delete user
	_, err := env.DB.Exec("DELETE FROM users WHERE id = ?", userID)
	require.NoError(t, err)

	// Verify posts were deleted via cascade
	var count int
	err = env.DB.QueryRow("SELECT COUNT(*) FROM posts WHERE user_id = ?", userID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "Posts should be deleted via cascade")

	// Verify comments have user_id set to NULL
	err = env.DB.QueryRow("SELECT COUNT(*) FROM comments WHERE user_id = ?", userID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count, "Comments should have user_id set to NULL")
}

// MeasurePerformance measures the performance of a function
func MeasurePerformance(t *testing.T, name string, fn func() error) time.Duration {
	start := time.Now()
	err := fn()
	duration := time.Since(start)

	require.NoError(t, err, "Performance test %s should not error", name)
	t.Logf("Performance test %s took: %v", name, duration)

	return duration
}

// CreateInvalidMigration creates an invalid migration for error testing
func (env *TestEnvironment) CreateInvalidMigration(t *testing.T, version, name string) {
	filename := fmt.Sprintf("%s_%s.up.sql", version, name)
	path := filepath.Join(env.MigrationsDir, filename)

	invalidSQL := "INVALID SQL SYNTAX HERE;"
	err := os.WriteFile(path, []byte(invalidSQL), 0644)
	require.NoError(t, err)
}

// CleanupInvalidMigration removes an invalid migration file
func (env *TestEnvironment) CleanupInvalidMigration(t *testing.T, version, name string) {
	filename := fmt.Sprintf("%s_%s.up.sql", version, name)
	path := filepath.Join(env.MigrationsDir, filename)

	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		t.Logf("Warning: Failed to remove invalid migration: %v", err)
	}
}

// GetMigrationFileCount returns the number of migration files
func (env *TestEnvironment) GetMigrationFileCount(t *testing.T) int {
	files, err := os.ReadDir(env.MigrationsDir)
	require.NoError(t, err)

	count := 0
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".sql") {
			count++
		}
	}

	return count
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("Condition not met within timeout: %s", message)
}

// AssertEventuallyTrue asserts that a condition becomes true within a timeout
func AssertEventuallyTrue(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	WaitForCondition(t, timeout, condition, message)
}

// CreateTempFile creates a temporary file with content
func CreateTempFile(t *testing.T, dir, pattern, content string) string {
	file, err := os.CreateTemp(dir, pattern)
	require.NoError(t, err)

	_, err = file.WriteString(content)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	return file.Name()
}

// CompareFiles compares two files for equality
func CompareFiles(t *testing.T, file1, file2 string) {
	content1, err := os.ReadFile(file1)
	require.NoError(t, err)

	content2, err := os.ReadFile(file2)
	require.NoError(t, err)

	require.Equal(t, string(content1), string(content2), "Files should have identical content")
}

// GetDatabaseStats returns database statistics
func (env *TestEnvironment) GetDatabaseStats(t *testing.T) sql.DBStats {
	return env.DB.Stats()
}

// LogDatabaseStats logs database statistics
func (env *TestEnvironment) LogDatabaseStats(t *testing.T) {
	stats := env.GetDatabaseStats(t)
	t.Logf("Database Stats - Open: %d, InUse: %d, Idle: %d, WaitCount: %d, WaitDuration: %v",
		stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount, stats.WaitDuration)
}

/**
 * @file checker_test.go
 * @description Unit tests for health checking components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/health/checker_test.go
 */

package health

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestHealthStatus_String(t *testing.T) {
	tests := []struct {
		status   HealthStatus
		expected string
	}{
		{HealthStatusHealthy, "healthy"},
		{HealthStatusUnhealthy, "unhealthy"},
		{HealthStatusDegraded, "degraded"},
		{HealthStatusUnknown, "unknown"},
		{HealthStatus("invalid"), "unknown"},
	}
	
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.status.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestHealthCheck_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatus
		expected bool
	}{
		{"healthy status", HealthStatusHealthy, true},
		{"unhealthy status", HealthStatusUnhealthy, false},
		{"degraded status", HealthStatusDegraded, false},
		{"unknown status", HealthStatusUnknown, false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := &HealthCheck{Status: tt.status}
			result := check.IsHealthy()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHealthCheck_AddDetail(t *testing.T) {
	check := &HealthCheck{
		Status:  HealthStatusHealthy,
		Details: make(map[string]interface{}),
	}
	
	check.AddDetail("key1", "value1")
	check.AddDetail("key2", 42)
	check.AddDetail("key3", true)
	
	if len(check.Details) != 3 {
		t.Errorf("expected 3 details, got %d", len(check.Details))
	}
	
	if check.Details["key1"] != "value1" {
		t.Errorf("expected 'value1', got %v", check.Details["key1"])
	}
	
	if check.Details["key2"] != 42 {
		t.Errorf("expected 42, got %v", check.Details["key2"])
	}
	
	if check.Details["key3"] != true {
		t.Errorf("expected true, got %v", check.Details["key3"])
	}
}

func TestNewHealthCheckerFactory(t *testing.T) {
	factory := NewHealthCheckerFactory()
	
	if factory == nil {
		t.Fatal("NewHealthCheckerFactory returned nil")
	}
}

func TestHealthCheckerFactory_CreateSQLite(t *testing.T) {
	factory := NewHealthCheckerFactory()
	
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	checker, err := factory.CreateChecker("sqlite", db, nil, &HealthConfig{
		Timeout: 5 * time.Second,
		ConnectivityTimeout: 2 * time.Second,
		PerformanceTimeout: 3 * time.Second,
		IntegrityTimeout: 3 * time.Second,
		ResourcesTimeout: 3 * time.Second,
		MigrationStateTimeout: 3 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("CreateChecker failed: %v", err)
	}
	
	if checker == nil {
		t.Fatal("Create returned nil checker")
	}
	
	// Check that it's the correct type
	if _, ok := checker.(*SQLiteHealthChecker); !ok {
		t.Errorf("expected SQLiteHealthChecker, got %T", checker)
	}
}

func TestHealthCheckerFactory_CreatePostgreSQL(t *testing.T) {
	factory := NewHealthCheckerFactory()
	
	checker, err := factory.CreateChecker("postgres", nil, nil, &HealthConfig{
		Timeout: 5 * time.Second,
		ConnectivityTimeout: 2 * time.Second,
		PerformanceTimeout: 3 * time.Second,
		IntegrityTimeout: 3 * time.Second,
		ResourcesTimeout: 3 * time.Second,
		MigrationStateTimeout: 3 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("CreateChecker failed: %v", err)
	}
	
	if checker == nil {
		t.Fatal("CreateChecker returned nil checker")
	}
	
	// Check that it's the correct type
	if _, ok := checker.(*PostgresHealthChecker); !ok {
		t.Errorf("expected PostgresHealthChecker, got %T", checker)
	}
}

func TestHealthCheckerFactory_CreateMySQL(t *testing.T) {
	factory := NewHealthCheckerFactory()
	
	checker, err := factory.CreateChecker("mysql", nil, nil, &HealthConfig{
		Timeout: 5 * time.Second,
		ConnectivityTimeout: 2 * time.Second,
		PerformanceTimeout: 3 * time.Second,
		IntegrityTimeout: 3 * time.Second,
		ResourcesTimeout: 3 * time.Second,
		MigrationStateTimeout: 3 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("CreateChecker failed: %v", err)
	}
	
	if checker == nil {
		t.Fatal("CreateChecker returned nil checker")
	}
	
	// Check that it's the correct type
	if _, ok := checker.(*MySQLHealthChecker); !ok {
		t.Errorf("expected MySQLHealthChecker, got %T", checker)
	}
}

func TestHealthCheckerFactory_CreateUnsupported(t *testing.T) {
	factory := NewHealthCheckerFactory()
	
	checker, err := factory.CreateChecker("unsupported", nil, nil, &HealthConfig{
		Timeout: 5 * time.Second,
		ConnectivityTimeout: 2 * time.Second,
		PerformanceTimeout: 3 * time.Second,
		IntegrityTimeout: 3 * time.Second,
		ResourcesTimeout: 3 * time.Second,
		MigrationStateTimeout: 3 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	})
	if err == nil {
		t.Error("expected error for unsupported database type")
	}
	
	if checker != nil {
		t.Error("expected nil checker for unsupported database type")
	}
}

func TestSQLiteHealthChecker_CheckHealth(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	// Create test table
	_, err = db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	
	// Create schema_migrations table for migration state check
	_, err = db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)")
	if err != nil {
		t.Fatalf("failed to create schema_migrations table: %v", err)
	}
	
	// Verify the table was created
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to verify schema_migrations table: %v", err)
	}
	if count == 0 {
		t.Fatalf("schema_migrations table was not created")
	}
	t.Logf("schema_migrations table verified: count=%d", count)
	
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     db,
			config: config,
		},
	}
	
	ctx := context.Background()
	healthReport, err := checker.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	
	if healthReport == nil {
		t.Fatal("CheckHealth returned nil")
	}
	
	if healthReport.OverallStatus != HealthStatusHealthy {
		t.Errorf("expected healthy status, got %s", healthReport.OverallStatus.String())
	}
	
	if healthReport.Timestamp.IsZero() {
		t.Error("timestamp should be set")
	}
	
	// Check that we have some checks
	if len(healthReport.Checks) == 0 {
		t.Error("health report should contain checks")
	}
	
	// Verify we have the expected checks
	expectedChecks := []string{"connectivity", "performance", "integrity", "resources", "migration_state"}
	if len(healthReport.Checks) != len(expectedChecks) {
		t.Errorf("expected %d checks, got %d", len(expectedChecks), len(healthReport.Checks))
	}
}

func TestSQLiteHealthChecker_CheckHealthWithClosedDB(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	
	// Close the database
	db.Close()
	
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     db,
			config: config,
		},
	}
	
	ctx := context.Background()
	healthReport, err := checker.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	
	if healthReport.OverallStatus == HealthStatusHealthy {
		t.Error("expected unhealthy status for closed database")
	}
	
	if healthReport.OverallStatus != HealthStatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", healthReport.OverallStatus.String())
	}
}

func TestSQLiteHealthChecker_CheckConnectivity(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	// Create schema_migrations table for migration state check
	_, err = db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)")
	if err != nil {
		t.Fatalf("failed to create schema_migrations table: %v", err)
	}
	
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     db,
			config: config,
		},
	}
	
	ctx := context.Background()
	healthCheck, err := checker.CheckConnectivity(ctx)
	if err != nil {
		t.Errorf("CheckConnectivity failed: %v", err)
	}
	
	if healthCheck == nil {
		t.Error("expected healthCheck to not be nil")
	}
	
	if !healthCheck.IsHealthy() {
		t.Error("expected healthy connectivity")
	}
}

func TestSQLiteHealthChecker_CheckConnectivityWithClosedDB(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	
	// Close the database
	db.Close()
	
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     db,
			config: config,
		},
	}
	
	ctx := context.Background()
	healthCheck, err := checker.CheckConnectivity(ctx)
	if err != nil {
		t.Errorf("CheckConnectivity failed: %v", err)
	}
	
	if healthCheck == nil {
		t.Error("expected healthCheck to not be nil")
	}
	
	if healthCheck.IsHealthy() {
		t.Error("expected unhealthy connectivity for closed database")
	}
}



func TestPostgresHealthChecker_CheckHealth(t *testing.T) {
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &PostgresHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     nil, // Mock database for testing
			config: config,
		},
	}
	
	ctx := context.Background()
	healthReport, err := checker.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	
	if healthReport == nil {
		t.Fatal("CheckHealth returned nil")
	}
	
	// With nil database, expect unhealthy status
	if healthReport.OverallStatus == HealthStatusHealthy {
		t.Error("expected unhealthy status for nil database")
	}
}

func TestMySQLHealthChecker_CheckHealth(t *testing.T) {
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &MySQLHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     nil, // Mock database for testing
			config: config,
		},
	}
	
	ctx := context.Background()
	healthReport, err := checker.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	
	if healthReport == nil {
		t.Fatal("CheckHealth returned nil")
	}
	
	// With nil database, expect unhealthy status
	if healthReport.OverallStatus == HealthStatusHealthy {
		t.Error("expected unhealthy status for nil database")
	}
}



func TestHealthCheckWithTimeout(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	// Create test table
	_, err = db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	
	// Create schema_migrations table for migration state check
	_, err = db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)")
	if err != nil {
		t.Fatalf("failed to create schema_migrations table: %v", err)
	}
	
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     db,
			config: config,
		},
	}
	
	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	
	// Wait for context to timeout
	time.Sleep(1 * time.Millisecond)
	
	healthCheck, err := checker.CheckHealth(ctx)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	
	// Health check should still complete but might be degraded due to timeout
	if healthCheck == nil {
		t.Fatal("CheckHealth returned nil")
	}
}

func TestConcurrentHealthChecks(t *testing.T) {
	// Create test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	// Create test table
	_, err = db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	
	// Create schema_migrations table for migration state check
	_, err = db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)")
	if err != nil {
		t.Fatalf("failed to create schema_migrations table: %v", err)
	}
	
	// Verify the table was created
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to verify schema_migrations table: %v", err)
	}
	if count == 0 {
		t.Fatalf("schema_migrations table was not created")
	}
	t.Logf("schema_migrations table verified: count=%d", count)
	
	config := &HealthConfig{
		Timeout:              30 * time.Second,
		ConnectivityTimeout:  5 * time.Second,
		PerformanceTimeout:   10 * time.Second,
		IntegrityTimeout:     15 * time.Second,
		ResourcesTimeout:     5 * time.Second,
		MigrationStateTimeout: 10 * time.Second,
		PerformanceThreshold: 100 * time.Millisecond,
	}
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db:     db,
			config: config,
		},
	}
	
	ctx := context.Background()
	
	// Run multiple health checks concurrently
	const numChecks = 10
	results := make(chan *HealthReport, numChecks)
	errors := make(chan error, numChecks)
	
	for i := 0; i < numChecks; i++ {
		go func() {
			healthReport, err := checker.CheckHealth(ctx)
			if err != nil {
				errors <- err
				return
			}
			results <- healthReport
		}()
	}
	
	// Collect results
	var healthReports []*HealthReport
	for i := 0; i < numChecks; i++ {
		select {
		case healthReport := <-results:
			healthReports = append(healthReports, healthReport)
		case err := <-errors:
			t.Fatalf("concurrent health check failed: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for health checks")
		}
	}
	
	if len(healthReports) != numChecks {
		t.Errorf("expected %d health reports, got %d", numChecks, len(healthReports))
	}
	
	// All reports should be healthy
	for i, report := range healthReports {
		if report.OverallStatus != HealthStatusHealthy {
			t.Errorf("health report %d should be healthy, got %s", i, report.OverallStatus.String())
		}
	}
}

func BenchmarkSQLiteHealthChecker_CheckHealth(b *testing.B) {
	// Create test database
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db: db,
		},
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := checker.CheckHealth(ctx)
		if err != nil {
			b.Fatalf("CheckHealth failed: %v", err)
		}
	}
}

func BenchmarkSQLiteHealthChecker_CheckConnectivity(b *testing.B) {
	// Create test database
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()
	
	checker := &SQLiteHealthChecker{
		BaseHealthChecker: &BaseHealthChecker{
			db: db,
		},
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := checker.CheckConnectivity(ctx)
		if err != nil {
			b.Fatalf("CheckConnectivity failed: %v", err)
		}
	}
}

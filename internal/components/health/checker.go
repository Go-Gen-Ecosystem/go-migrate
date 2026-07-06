/**
 * @file checker.go
 * @description HealthChecker interface with database-specific implementations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/health/checker.go
 */

package health

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/dialect"
)

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// String returns the string representation of HealthStatus
func (hs HealthStatus) String() string {
	switch hs {
	case HealthStatusHealthy:
		return "healthy"
	case HealthStatusUnhealthy:
		return "unhealthy"
	case HealthStatusDegraded:
		return "degraded"
	case HealthStatusUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// HealthCheck represents a single health check result
type HealthCheck struct {
	Name        string                 `json:"name"`
	Status      HealthStatus           `json:"status"`
	Message     string                 `json:"message"`
	Duration    time.Duration          `json:"duration"`
	Timestamp   time.Time              `json:"timestamp"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Error       error                  `json:"error,omitempty"`
}

// IsHealthy returns true if the health check status is healthy
func (hc *HealthCheck) IsHealthy() bool {
	return hc.Status == HealthStatusHealthy
}

// AddDetail adds a detail to the health check
func (hc *HealthCheck) AddDetail(key string, value interface{}) {
	if hc.Details == nil {
		hc.Details = make(map[string]interface{})
	}
	hc.Details[key] = value
}

// HealthReport contains the overall health status and individual checks
type HealthReport struct {
	OverallStatus HealthStatus   `json:"overall_status"`
	Checks        []*HealthCheck `json:"checks"`
	Timestamp     time.Time      `json:"timestamp"`
	Duration      time.Duration  `json:"duration"`
}

// HealthChecker interface for database health checking
type HealthChecker interface {
	CheckHealth(ctx context.Context) (*HealthReport, error)
	CheckConnectivity(ctx context.Context) (*HealthCheck, error)
	CheckPerformance(ctx context.Context) (*HealthCheck, error)
	CheckIntegrity(ctx context.Context) (*HealthCheck, error)
	CheckResources(ctx context.Context) (*HealthCheck, error)
	CheckMigrationState(ctx context.Context) (*HealthCheck, error)
}

// BaseHealthChecker provides common health checking functionality
type BaseHealthChecker struct {
	db         *sql.DB
	dialectCtx *dialect.DatabaseDialectContext
	config     *HealthConfig
}

// HealthConfig holds health check configuration
type HealthConfig struct {
	Timeout              time.Duration `json:"timeout"`
	ConnectivityTimeout  time.Duration `json:"connectivity_timeout"`
	PerformanceTimeout   time.Duration `json:"performance_timeout"`
	IntegrityTimeout     time.Duration `json:"integrity_timeout"`
	ResourcesTimeout     time.Duration `json:"resources_timeout"`
	MigrationStateTimeout time.Duration `json:"migration_state_timeout"`
	PerformanceThreshold time.Duration `json:"performance_threshold"`
}

func NewBaseHealthChecker(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *HealthConfig) *BaseHealthChecker {
	if config == nil {
		config = &HealthConfig{
			Timeout:               30 * time.Second,
			ConnectivityTimeout:   5 * time.Second,
			PerformanceTimeout:    10 * time.Second,
			IntegrityTimeout:      15 * time.Second,
			ResourcesTimeout:      5 * time.Second,
			MigrationStateTimeout: 5 * time.Second,
			PerformanceThreshold:  1 * time.Second,
		}
	}
	
	return &BaseHealthChecker{
		db:         db,
		dialectCtx: dialectCtx,
		config:     config,
	}
}

// CheckHealth is implemented by concrete health checkers
// BaseHealthChecker provides common functionality but doesn't implement the interface directly

func (bhc *BaseHealthChecker) determineOverallStatus(checks []*HealthCheck) HealthStatus {
	hasUnhealthy := false
	hasDegraded := false
	
	for _, check := range checks {
		switch check.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
		case HealthStatusDegraded:
			hasDegraded = true
		case HealthStatusUnknown:
			hasDegraded = true
		}
	}
	
	if hasUnhealthy {
		return HealthStatusUnhealthy
	}
	if hasDegraded {
		return HealthStatusDegraded
	}
	return HealthStatusHealthy
}

func (bhc *BaseHealthChecker) runCheck(ctx context.Context, name string, timeout time.Duration, checkFunc func(context.Context) error) *HealthCheck {
	start := time.Now()
	
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	check := &HealthCheck{
		Name:      name,
		Timestamp: start,
		Metadata:  make(map[string]interface{}),
	}
	
	err := checkFunc(checkCtx)
	check.Duration = time.Since(start)
	
	if err != nil {
		check.Status = HealthStatusUnhealthy
		check.Message = err.Error()
		check.Error = err
	} else {
		check.Status = HealthStatusHealthy
		check.Message = "OK"
	}
	
	return check
}

// SQLiteHealthChecker implements health checking for SQLite
type SQLiteHealthChecker struct {
	*BaseHealthChecker
}

func NewSQLiteHealthChecker(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *HealthConfig) *SQLiteHealthChecker {
	return &SQLiteHealthChecker{
		BaseHealthChecker: NewBaseHealthChecker(db, dialectCtx, config),
	}
}

func (shc *SQLiteHealthChecker) CheckHealth(ctx context.Context) (*HealthReport, error) {
	start := time.Now()
	
	// Create context with timeout
	healthCtx, cancel := context.WithTimeout(ctx, shc.config.Timeout)
	defer cancel()
	
	var checks []*HealthCheck
	
	// Run all health checks
	connectivityCheck, _ := shc.CheckConnectivity(healthCtx)
	checks = append(checks, connectivityCheck)
	
	performanceCheck, _ := shc.CheckPerformance(healthCtx)
	checks = append(checks, performanceCheck)
	
	integrityCheck, _ := shc.CheckIntegrity(healthCtx)
	checks = append(checks, integrityCheck)
	
	resourcesCheck, _ := shc.CheckResources(healthCtx)
	checks = append(checks, resourcesCheck)
	
	migrationStateCheck, _ := shc.CheckMigrationState(healthCtx)
	checks = append(checks, migrationStateCheck)
	
	// Determine overall status
	overallStatus := shc.determineOverallStatus(checks)
	
	report := &HealthReport{
		OverallStatus: overallStatus,
		Checks:        checks,
		Timestamp:     time.Now(),
		Duration:      time.Since(start),
	}
	
	return report, nil
}

func (shc *SQLiteHealthChecker) CheckConnectivity(ctx context.Context) (*HealthCheck, error) {
	return shc.runCheck(ctx, "connectivity", shc.config.ConnectivityTimeout, func(ctx context.Context) error {
		if shc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		return shc.db.PingContext(ctx)
	}), nil
}

func (shc *SQLiteHealthChecker) CheckPerformance(ctx context.Context) (*HealthCheck, error) {
	return shc.runCheck(ctx, "performance", shc.config.PerformanceTimeout, func(ctx context.Context) error {
		if shc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		start := time.Now()
		_, err := shc.db.ExecContext(ctx, "SELECT 1")
		duration := time.Since(start)
		
		if duration > shc.config.PerformanceThreshold {
			return fmt.Errorf("query took %v, exceeds threshold of %v", duration, shc.config.PerformanceThreshold)
		}
		return err
	}), nil
}

func (shc *SQLiteHealthChecker) CheckIntegrity(ctx context.Context) (*HealthCheck, error) {
	return shc.runCheck(ctx, "integrity", shc.config.IntegrityTimeout, func(ctx context.Context) error {
		if shc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		var result string
		err := shc.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
		if err != nil {
			return err
		}
		if result != "ok" {
			return fmt.Errorf("integrity check failed: %s", result)
		}
		return nil
	}), nil
}

func (shc *SQLiteHealthChecker) CheckResources(ctx context.Context) (*HealthCheck, error) {
	return shc.runCheck(ctx, "resources", shc.config.ResourcesTimeout, func(ctx context.Context) error {
		if shc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		stats := shc.db.Stats()
		
		check := &HealthCheck{
			Name:      "resources",
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}
		
		check.AddDetail("open_connections", stats.OpenConnections)
		check.AddDetail("in_use", stats.InUse)
		check.AddDetail("idle", stats.Idle)
		
		return nil
	}), nil
}

func (shc *SQLiteHealthChecker) CheckMigrationState(ctx context.Context) (*HealthCheck, error) {
	return shc.runCheck(ctx, "migration_state", shc.config.MigrationStateTimeout, func(ctx context.Context) error {
		if shc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		var count int
		err := shc.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration table: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("migration table not found")
		}
		return nil
	}), nil
}

// PostgresHealthChecker implements health checking for PostgreSQL
type PostgresHealthChecker struct {
	*BaseHealthChecker
}

func NewPostgresHealthChecker(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *HealthConfig) *PostgresHealthChecker {
	return &PostgresHealthChecker{
		BaseHealthChecker: NewBaseHealthChecker(db, dialectCtx, config),
	}
}

func (phc *PostgresHealthChecker) CheckHealth(ctx context.Context) (*HealthReport, error) {
	start := time.Now()
	
	// Create context with timeout
	healthCtx, cancel := context.WithTimeout(ctx, phc.config.Timeout)
	defer cancel()
	
	var checks []*HealthCheck
	
	// Run all health checks
	connectivityCheck, _ := phc.CheckConnectivity(healthCtx)
	checks = append(checks, connectivityCheck)
	
	performanceCheck, _ := phc.CheckPerformance(healthCtx)
	checks = append(checks, performanceCheck)
	
	integrityCheck, _ := phc.CheckIntegrity(healthCtx)
	checks = append(checks, integrityCheck)
	
	resourcesCheck, _ := phc.CheckResources(healthCtx)
	checks = append(checks, resourcesCheck)
	
	migrationStateCheck, _ := phc.CheckMigrationState(healthCtx)
	checks = append(checks, migrationStateCheck)
	
	// Determine overall status
	overallStatus := phc.determineOverallStatus(checks)
	
	report := &HealthReport{
		OverallStatus: overallStatus,
		Checks:        checks,
		Timestamp:     time.Now(),
		Duration:      time.Since(start),
	}
	
	return report, nil
}

func (phc *PostgresHealthChecker) CheckConnectivity(ctx context.Context) (*HealthCheck, error) {
	return phc.runCheck(ctx, "connectivity", phc.config.ConnectivityTimeout, func(ctx context.Context) error {
		if phc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		return phc.db.PingContext(ctx)
	}), nil
}

func (phc *PostgresHealthChecker) CheckPerformance(ctx context.Context) (*HealthCheck, error) {
	return phc.runCheck(ctx, "performance", phc.config.PerformanceTimeout, func(ctx context.Context) error {
		if phc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		start := time.Now()
		_, err := phc.db.ExecContext(ctx, "SELECT 1")
		duration := time.Since(start)
		
		if duration > phc.config.PerformanceThreshold {
			return fmt.Errorf("query took %v, exceeds threshold of %v", duration, phc.config.PerformanceThreshold)
		}
		return err
	}), nil
}

func (phc *PostgresHealthChecker) CheckIntegrity(ctx context.Context) (*HealthCheck, error) {
	return phc.runCheck(ctx, "integrity", phc.config.IntegrityTimeout, func(ctx context.Context) error {
		if phc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		var result string
		err := phc.db.QueryRowContext(ctx, "SELECT version()").Scan(&result)
		if err != nil {
			return err
		}
		if !strings.Contains(strings.ToLower(result), "postgresql") {
			return fmt.Errorf("unexpected database version: %s", result)
		}
		return nil
	}), nil
}

func (phc *PostgresHealthChecker) CheckResources(ctx context.Context) (*HealthCheck, error) {
	return phc.runCheck(ctx, "resources", phc.config.ResourcesTimeout, func(ctx context.Context) error {
		if phc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		stats := phc.db.Stats()
		
		check := &HealthCheck{
			Name:      "resources",
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}
		
		check.AddDetail("open_connections", stats.OpenConnections)
		check.AddDetail("in_use", stats.InUse)
		check.AddDetail("idle", stats.Idle)
		
		// Check active connections
		var activeConnections int
		err := phc.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state = 'active'").Scan(&activeConnections)
		if err != nil {
			return fmt.Errorf("failed to get active connections: %w", err)
		}
		
		check.AddDetail("active_connections", activeConnections)
		
		return nil
	}), nil
}

func (phc *PostgresHealthChecker) CheckMigrationState(ctx context.Context) (*HealthCheck, error) {
	return phc.runCheck(ctx, "migration_state", phc.config.MigrationStateTimeout, func(ctx context.Context) error {
		if phc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		var count int
		err := phc.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'schema_migrations'").Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration table: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("migration table not found")
		}
		return nil
	}), nil
}

// MySQLHealthChecker implements health checking for MySQL
type MySQLHealthChecker struct {
	*BaseHealthChecker
}

func NewMySQLHealthChecker(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *HealthConfig) *MySQLHealthChecker {
	return &MySQLHealthChecker{
		BaseHealthChecker: NewBaseHealthChecker(db, dialectCtx, config),
	}
}

func (mhc *MySQLHealthChecker) CheckHealth(ctx context.Context) (*HealthReport, error) {
	start := time.Now()
	
	// Create context with timeout
	healthCtx, cancel := context.WithTimeout(ctx, mhc.config.Timeout)
	defer cancel()
	
	var checks []*HealthCheck
	
	// Run all health checks
	connectivityCheck, _ := mhc.CheckConnectivity(healthCtx)
	checks = append(checks, connectivityCheck)
	
	performanceCheck, _ := mhc.CheckPerformance(healthCtx)
	checks = append(checks, performanceCheck)
	
	integrityCheck, _ := mhc.CheckIntegrity(healthCtx)
	checks = append(checks, integrityCheck)
	
	resourcesCheck, _ := mhc.CheckResources(healthCtx)
	checks = append(checks, resourcesCheck)
	
	migrationStateCheck, _ := mhc.CheckMigrationState(healthCtx)
	checks = append(checks, migrationStateCheck)
	
	// Determine overall status
	overallStatus := mhc.determineOverallStatus(checks)
	
	report := &HealthReport{
		OverallStatus: overallStatus,
		Checks:        checks,
		Timestamp:     time.Now(),
		Duration:      time.Since(start),
	}
	
	return report, nil
}

func (mhc *MySQLHealthChecker) CheckConnectivity(ctx context.Context) (*HealthCheck, error) {
	return mhc.runCheck(ctx, "connectivity", mhc.config.ConnectivityTimeout, func(ctx context.Context) error {
		if mhc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		return mhc.db.PingContext(ctx)
	}), nil
}

func (mhc *MySQLHealthChecker) CheckPerformance(ctx context.Context) (*HealthCheck, error) {
	return mhc.runCheck(ctx, "performance", mhc.config.PerformanceTimeout, func(ctx context.Context) error {
		if mhc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		start := time.Now()
		_, err := mhc.db.ExecContext(ctx, "SELECT 1")
		duration := time.Since(start)
		
		if duration > mhc.config.PerformanceThreshold {
			return fmt.Errorf("query took %v, exceeds threshold of %v", duration, mhc.config.PerformanceThreshold)
		}
		return err
	}), nil
}

func (mhc *MySQLHealthChecker) CheckIntegrity(ctx context.Context) (*HealthCheck, error) {
	return mhc.runCheck(ctx, "integrity", mhc.config.IntegrityTimeout, func(ctx context.Context) error {
		if mhc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		var result string
		err := mhc.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&result)
		if err != nil {
			return err
		}
		if !strings.Contains(strings.ToLower(result), "mysql") && !strings.Contains(strings.ToLower(result), "mariadb") {
			return fmt.Errorf("unexpected database version: %s", result)
		}
		return nil
	}), nil
}

func (mhc *MySQLHealthChecker) CheckResources(ctx context.Context) (*HealthCheck, error) {
	return mhc.runCheck(ctx, "resources", mhc.config.ResourcesTimeout, func(ctx context.Context) error {
		if mhc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		stats := mhc.db.Stats()
		
		check := &HealthCheck{
			Name:      "resources",
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}
		
		check.AddDetail("open_connections", stats.OpenConnections)
		check.AddDetail("in_use", stats.InUse)
		check.AddDetail("idle", stats.Idle)
		
		// Check active connections
		var activeConnections int
		err := mhc.db.QueryRowContext(ctx, "SHOW STATUS LIKE 'Threads_connected'").Scan(&activeConnections)
		if err != nil {
			return fmt.Errorf("failed to get active connections: %w", err)
		}
		
		check.AddDetail("active_connections", activeConnections)
		
		return nil
	}), nil
}

func (mhc *MySQLHealthChecker) CheckMigrationState(ctx context.Context) (*HealthCheck, error) {
	return mhc.runCheck(ctx, "migration_state", mhc.config.MigrationStateTimeout, func(ctx context.Context) error {
		if mhc.db == nil {
			return fmt.Errorf("database connection is nil")
		}
		var count int
		err := mhc.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'schema_migrations'").Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration table: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("migration table not found")
		}
		return nil
	}), nil
}

// HealthCheckerFactory creates database-specific health checkers
type HealthCheckerFactory struct {
	checkers map[string]func(*sql.DB, *dialect.DatabaseDialectContext, *HealthConfig) HealthChecker
}

func NewHealthCheckerFactory() *HealthCheckerFactory {
	factory := &HealthCheckerFactory{
		checkers: make(map[string]func(*sql.DB, *dialect.DatabaseDialectContext, *HealthConfig) HealthChecker),
	}

	// Register built-in checkers
	factory.RegisterChecker("sqlite", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *HealthConfig) HealthChecker {
		return NewSQLiteHealthChecker(db, ctx, config)
	})
	factory.RegisterChecker("postgres", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *HealthConfig) HealthChecker {
		return NewPostgresHealthChecker(db, ctx, config)
	})
	factory.RegisterChecker("postgresql", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *HealthConfig) HealthChecker {
		return NewPostgresHealthChecker(db, ctx, config)
	})
	factory.RegisterChecker("mysql", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *HealthConfig) HealthChecker {
		return NewMySQLHealthChecker(db, ctx, config)
	})

	return factory
}

func (f *HealthCheckerFactory) RegisterChecker(dbType string, factory func(*sql.DB, *dialect.DatabaseDialectContext, *HealthConfig) HealthChecker) {
	f.checkers[dbType] = factory
}

func (f *HealthCheckerFactory) CreateChecker(dbType string, db *sql.DB, ctx *dialect.DatabaseDialectContext, config *HealthConfig) (HealthChecker, error) {
	factory, exists := f.checkers[dbType]
	if !exists {
		return nil, fmt.Errorf("unsupported database type for health checking: %s", dbType)
	}
	return factory(db, ctx, config), nil
}

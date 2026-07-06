/**
 * @file components_factory.go
 * @description Database-specific components factory implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/factory/components_factory.go
 */

package factory

import (
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/backend"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/executor"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/lock"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/tracker"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/validator"
)

// DatabaseComponentsFactory implements ComponentsFactory for database-specific components
type DatabaseComponentsFactory struct {
	dialect string
}

// NewDatabaseComponentsFactory creates a new database components factory
func NewDatabaseComponentsFactory(dialect string) types.ComponentsFactory {
	return &DatabaseComponentsFactory{
		dialect: dialect,
	}
}

// NewBackend creates a new backend for the specified dialect
func (dcf *DatabaseComponentsFactory) NewBackend(cfg types.Config) (types.EngineBackend, error) {
	// Convert dialect string to backend.DatabaseDialect
	var dbDialect backend.DatabaseDialect
	switch dcf.dialect {
	case "postgresql":
		dbDialect = backend.DialectPostgreSQL
	case "mysql":
		dbDialect = backend.DialectMySQL
	case "sqlite":
		dbDialect = backend.DialectSQLite
	case "sqlserver":
		dbDialect = backend.DialectSQLServer
	case "oracle":
		dbDialect = backend.DialectOracle
	case "db2":
		dbDialect = backend.DialectDB2
	case "mongodb":
		dbDialect = backend.DialectMongoDB
	case "cassandra":
		dbDialect = backend.DialectCassandra
	case "redis":
		dbDialect = backend.DialectRedis
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", dcf.dialect)
	}

	// Create backend using the registry
	dbBackend, err := backend.CreateBackend(dbDialect, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s backend: %w", dcf.dialect, err)
	}

	// Wrap the backend to implement EngineBackend interface
	return &EngineBackendAdapter{
		backend: dbBackend,
		dialect: dcf.dialect,
	}, nil
}

// NewExecutor creates a new executor for the specified dialect
func (dcf *DatabaseComponentsFactory) NewExecutor(cfg types.Config) (types.MigrationExecutor, error) {
	// Create a database-specific executor
	return executor.NewDatabaseMigrationExecutor(cfg.Database), nil
}

// NewRollbackExecutor creates a new rollback executor for the specified dialect
func (dcf *DatabaseComponentsFactory) NewRollbackExecutor(cfg types.Config) (types.RollbackExecutor, error) {
	// Create a database-specific rollback executor
	return executor.NewDatabaseRollbackExecutor(cfg.Database), nil
}

// NewTracker creates a new tracker for the specified dialect
func (dcf *DatabaseComponentsFactory) NewTracker(cfg types.Config) (types.MigrationTracker, error) {
	// Create a database-specific tracker
	return tracker.NewDatabaseMigrationTracker(cfg.Database, cfg.Migration.TableName)
}

// NewLockManager creates a new lock manager for the specified dialect
func (dcf *DatabaseComponentsFactory) NewLockManager(cfg types.Config) (types.LockManager, error) {
	// Create a database-specific lock manager
	return lock.NewDatabaseLockManager(cfg.Database, dcf.dialect), nil
}

// NewValidator creates a new validator for the specified dialect
func (dcf *DatabaseComponentsFactory) NewValidator(cfg types.Config) (types.MigrationValidator, error) {
	// Create a database-specific validator
	return validator.NewDatabaseMigrationValidator(cfg.Database, dcf.dialect), nil
}

// GetSupportedDialect returns the supported dialect
func (dcf *DatabaseComponentsFactory) GetSupportedDialect() string {
	return dcf.dialect
}

// GetComponentVersion returns the component version
func (dcf *DatabaseComponentsFactory) GetComponentVersion() string {
	return "1.0.0"
}

// EngineBackendAdapter adapts backend.DatabaseBackend to types.EngineBackend
type EngineBackendAdapter struct {
	backend backend.DatabaseBackend
	dialect string
}

// Connect establishes a connection to the database
func (eba *EngineBackendAdapter) Connect() error {
	return eba.backend.Connect()
}

// Disconnect closes the database connection
func (eba *EngineBackendAdapter) Disconnect() error {
	return eba.backend.Disconnect()
}

// IsConnected checks if the backend is connected
func (eba *EngineBackendAdapter) IsConnected() bool {
	return eba.backend.IsConnected()
}

// GetDialect returns the database dialect
func (eba *EngineBackendAdapter) GetDialect() string {
	return eba.dialect
}

// GetVersion returns the database version
func (eba *EngineBackendAdapter) GetVersion() (string, error) {
	return eba.backend.GetVersion()
}

// ExecuteSQL executes raw SQL
func (eba *EngineBackendAdapter) ExecuteSQL(sql string, args ...interface{}) error {
	return eba.backend.ExecuteSQL(sql, args...)
}

// QuerySQL executes a query and returns results
func (eba *EngineBackendAdapter) QuerySQL(sql string, args ...interface{}) ([]map[string]interface{}, error) {
	return eba.backend.QuerySQL(sql, args...)
}

// BeginTransaction starts a new transaction
func (eba *EngineBackendAdapter) BeginTransaction() (types.Transaction, error) {
	tx, err := eba.backend.BeginTransaction()
	if err != nil {
		return nil, err
	}
	return &TransactionAdapter{tx: tx}, nil
}

// TransactionAdapter adapts backend.Transaction to types.Transaction
type TransactionAdapter struct {
	tx backend.Transaction
}

// Commit commits the transaction
func (ta *TransactionAdapter) Commit() error {
	return ta.tx.Commit()
}

// Rollback rolls back the transaction
func (ta *TransactionAdapter) Rollback() error {
	return ta.tx.Rollback()
}

// ExecuteSQL executes SQL within the transaction
func (ta *TransactionAdapter) ExecuteSQL(sql string, args ...interface{}) error {
	return ta.tx.ExecuteSQL(sql, args...)
}
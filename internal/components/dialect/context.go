/**
 * @file context.go
 * @description DatabaseDialectContext and dialect-specific strategies for all database types
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/dialect/context.go
 */

package dialect

import (
	"fmt"
	"strings"
)

// DatabaseDialectContext provides database-specific context and capabilities
type DatabaseDialectContext struct {
	Type     string                 // sqlite, postgres, mysql
	Version  string                 // database version
	Features map[string]bool        // supported features
	Config   map[string]interface{} // dialect-specific configuration
}

// NewDatabaseDialectContext creates a new dialect context
func NewDatabaseDialectContext(dbType, version string) *DatabaseDialectContext {
	ctx := &DatabaseDialectContext{
		Type:     strings.ToLower(dbType),
		Version:  version,
		Features: make(map[string]bool),
		Config:   make(map[string]interface{}),
	}
	
	// Initialize features based on database type
	ctx.initializeFeatures()
	return ctx
}

// initializeFeatures sets up database-specific features
func (ctx *DatabaseDialectContext) initializeFeatures() {
	switch ctx.Type {
	case "sqlite":
		ctx.Features["transactions"] = true
		ctx.Features["foreign_keys"] = true
		ctx.Features["row_level_locking"] = false
		ctx.Features["advisory_locks"] = false
		ctx.Features["concurrent_migrations"] = false
		ctx.Features["ddl_transactions"] = true
		ctx.Features["partial_indexes"] = true
		ctx.Features["json_support"] = true
		
	case "postgres", "postgresql":
		ctx.Features["transactions"] = true
		ctx.Features["foreign_keys"] = true
		ctx.Features["row_level_locking"] = true
		ctx.Features["advisory_locks"] = true
		ctx.Features["concurrent_migrations"] = true
		ctx.Features["ddl_transactions"] = true
		ctx.Features["partial_indexes"] = true
		ctx.Features["json_support"] = true
		ctx.Features["arrays"] = true
		ctx.Features["custom_types"] = true
		
	case "mysql":
		ctx.Features["transactions"] = true
		ctx.Features["foreign_keys"] = true
		ctx.Features["row_level_locking"] = true
		ctx.Features["advisory_locks"] = false
		ctx.Features["concurrent_migrations"] = true
		ctx.Features["ddl_transactions"] = false // MySQL DDL is not transactional
		ctx.Features["partial_indexes"] = false
		ctx.Features["json_support"] = true
		
	default:
		// Default minimal feature set
		ctx.Features["transactions"] = true
		ctx.Features["foreign_keys"] = false
		ctx.Features["row_level_locking"] = false
		ctx.Features["advisory_locks"] = false
		ctx.Features["concurrent_migrations"] = false
		ctx.Features["ddl_transactions"] = false
	}
}

// HasFeature checks if a feature is supported
func (ctx *DatabaseDialectContext) HasFeature(feature string) bool {
	return ctx.Features[feature]
}

// SetFeature sets a feature flag
func (ctx *DatabaseDialectContext) SetFeature(feature string, enabled bool) {
	ctx.Features[feature] = enabled
}

// GetFeature gets a feature value
func (ctx *DatabaseDialectContext) GetFeature(feature string) (interface{}, bool) {
	value, exists := ctx.Features[feature]
	return value, exists
}

// GetConfig gets a configuration value
func (ctx *DatabaseDialectContext) GetConfig(key string) (interface{}, bool) {
	value, exists := ctx.Config[key]
	return value, exists
}

// SetConfig sets a configuration value
func (ctx *DatabaseDialectContext) SetConfig(key string, value interface{}) {
	ctx.Config[key] = value
}

// DialectStrategy defines the interface for database-specific strategies
type DialectStrategy interface {
	GetLockSQL() string
	GetUnlockSQL() string
	GetCreateTableSQL(tableName string, columns []string) string
	GetInsertSQL(tableName string, columns []string) string
	GetSelectSQL(tableName string, columns []string, where string) string
	SupportsTransactionalDDL() bool
	SupportsConcurrentMigrations() bool
	GetMaxIdentifierLength() int
}

// SQLiteStrategy implements dialect strategy for SQLite
type SQLiteStrategy struct {
	context *DatabaseDialectContext
}

func NewSQLiteStrategy(ctx *DatabaseDialectContext) *SQLiteStrategy {
	return &SQLiteStrategy{context: ctx}
}

func (s *SQLiteStrategy) GetLockSQL() string {
	return "BEGIN EXCLUSIVE TRANSACTION"
}

func (s *SQLiteStrategy) GetUnlockSQL() string {
	return "COMMIT"
}

func (s *SQLiteStrategy) GetCreateTableSQL(tableName string, columns []string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableName, strings.Join(columns, ", "))
}

func (s *SQLiteStrategy) GetInsertSQL(tableName string, columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", 
		tableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
}

func (s *SQLiteStrategy) GetSelectSQL(tableName string, columns []string, where string) string {
	sql := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), tableName)
	if where != "" {
		sql += " WHERE " + where
	}
	return sql
}

func (s *SQLiteStrategy) SupportsTransactionalDDL() bool {
	return true
}

func (s *SQLiteStrategy) SupportsConcurrentMigrations() bool {
	return false
}

func (s *SQLiteStrategy) GetMaxIdentifierLength() int {
	return 1000 // SQLite has very high limits
}

// PostgresStrategy implements dialect strategy for PostgreSQL
type PostgresStrategy struct {
	context *DatabaseDialectContext
}

func NewPostgresStrategy(ctx *DatabaseDialectContext) *PostgresStrategy {
	return &PostgresStrategy{context: ctx}
}

func (s *PostgresStrategy) GetLockSQL() string {
	return "SELECT pg_advisory_lock($1)"
}

func (s *PostgresStrategy) GetUnlockSQL() string {
	return "SELECT pg_advisory_unlock($1)"
}

func (s *PostgresStrategy) GetCreateTableSQL(tableName string, columns []string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableName, strings.Join(columns, ", "))
}

func (s *PostgresStrategy) GetInsertSQL(tableName string, columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", 
		tableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
}

func (s *PostgresStrategy) GetSelectSQL(tableName string, columns []string, where string) string {
	sql := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), tableName)
	if where != "" {
		sql += " WHERE " + where
	}
	return sql
}

func (s *PostgresStrategy) SupportsTransactionalDDL() bool {
	return true
}

func (s *PostgresStrategy) SupportsConcurrentMigrations() bool {
	return true
}

func (s *PostgresStrategy) GetMaxIdentifierLength() int {
	return 63 // PostgreSQL identifier limit
}

// MySQLStrategy implements dialect strategy for MySQL
type MySQLStrategy struct {
	context *DatabaseDialectContext
}

func NewMySQLStrategy(ctx *DatabaseDialectContext) *MySQLStrategy {
	return &MySQLStrategy{context: ctx}
}

func (s *MySQLStrategy) GetLockSQL() string {
	return "SELECT GET_LOCK(?, ?)"
}

func (s *MySQLStrategy) GetUnlockSQL() string {
	return "SELECT RELEASE_LOCK(?)"
}

func (s *MySQLStrategy) GetCreateTableSQL(tableName string, columns []string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", tableName, strings.Join(columns, ", "))
}

func (s *MySQLStrategy) GetInsertSQL(tableName string, columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", 
		tableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
}

func (s *MySQLStrategy) GetSelectSQL(tableName string, columns []string, where string) string {
	sql := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), tableName)
	if where != "" {
		sql += " WHERE " + where
	}
	return sql
}

func (s *MySQLStrategy) SupportsTransactionalDDL() bool {
	return false // MySQL DDL is not transactional
}

func (s *MySQLStrategy) SupportsConcurrentMigrations() bool {
	return true
}

func (s *MySQLStrategy) GetMaxIdentifierLength() int {
	return 64 // MySQL identifier limit
}

// DialectStrategyFactory creates dialect strategies
type DialectStrategyFactory struct {
	strategies map[string]func(*DatabaseDialectContext) DialectStrategy
}

func NewDialectStrategyFactory() *DialectStrategyFactory {
	factory := &DialectStrategyFactory{
		strategies: make(map[string]func(*DatabaseDialectContext) DialectStrategy),
	}
	
	// Register built-in strategies
	factory.RegisterStrategy("sqlite", func(ctx *DatabaseDialectContext) DialectStrategy {
		return NewSQLiteStrategy(ctx)
	})
	factory.RegisterStrategy("postgres", func(ctx *DatabaseDialectContext) DialectStrategy {
		return NewPostgresStrategy(ctx)
	})
	factory.RegisterStrategy("postgresql", func(ctx *DatabaseDialectContext) DialectStrategy {
		return NewPostgresStrategy(ctx)
	})
	factory.RegisterStrategy("mysql", func(ctx *DatabaseDialectContext) DialectStrategy {
		return NewMySQLStrategy(ctx)
	})
	
	return factory
}

func (f *DialectStrategyFactory) RegisterStrategy(dbType string, factory func(*DatabaseDialectContext) DialectStrategy) {
	f.strategies[strings.ToLower(dbType)] = factory
}

func (f *DialectStrategyFactory) CreateStrategy(ctx *DatabaseDialectContext) (DialectStrategy, error) {
	factory, exists := f.strategies[ctx.Type]
	if !exists {
		return nil, fmt.Errorf("unsupported database type: %s", ctx.Type)
	}
	return factory(ctx), nil
}

// DialectManager manages dialect contexts and strategies
type DialectManager struct {
	factory *DialectStrategyFactory
}

func NewDialectManager() *DialectManager {
	return &DialectManager{
		factory: NewDialectStrategyFactory(),
	}
}

func (dm *DialectManager) CreateContext(dbType, version string) *DatabaseDialectContext {
	return NewDatabaseDialectContext(dbType, version)
}

func (dm *DialectManager) CreateStrategy(ctx *DatabaseDialectContext) (DialectStrategy, error) {
	return dm.factory.CreateStrategy(ctx)
}

func (dm *DialectManager) RegisterStrategy(dbType string, factory func(*DatabaseDialectContext) DialectStrategy) {
	dm.factory.RegisterStrategy(dbType, factory)
}

// DialectAware interface for components that need dialect context
type DialectAware interface {
	SetDialectContext(ctx *DatabaseDialectContext)
	GetDialectContext() *DatabaseDialectContext
}

// DialectContextPropagator helps propagate dialect context through components
type DialectContextPropagator struct {
	context *DatabaseDialectContext
}

func NewDialectContextPropagator(ctx *DatabaseDialectContext) *DialectContextPropagator {
	return &DialectContextPropagator{context: ctx}
}

func (dcp *DialectContextPropagator) PropagateToComponent(component interface{}) error {
	if dialectAware, ok := component.(DialectAware); ok {
		dialectAware.SetDialectContext(dcp.context)
		return nil
	}
	return fmt.Errorf("component does not implement DialectAware interface")
}

func (dcp *DialectContextPropagator) GetContext() *DatabaseDialectContext {
	return dcp.context
}

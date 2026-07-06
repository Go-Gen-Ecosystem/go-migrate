/**
 * @file interfaces.go
 * @description Backend interfaces for Bridge pattern implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/interfaces.go
 */

package backend

import (
	"context"
	"database/sql"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// DatabaseBackend defines the Bridge interface for database-specific operations
type DatabaseBackend interface {
	// Connection management
	Connect(ctx context.Context, config types.DatabaseConfig) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
	IsConnected() bool

	// Transaction management
	BeginTransaction(ctx context.Context) (Transaction, error)
	BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error)

	// Query execution
	Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error)
	Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow

	// Schema operations
	CreateSchema(ctx context.Context, schemaName string) error
	DropSchema(ctx context.Context, schemaName string) error
	SchemaExists(ctx context.Context, schemaName string) (bool, error)
	ListSchemas(ctx context.Context) ([]string, error)

	// Table operations
	CreateTable(ctx context.Context, tableDef TableDefinition) error
	DropTable(ctx context.Context, tableName string) error
	TableExists(ctx context.Context, tableName string) (bool, error)
	ListTables(ctx context.Context) ([]string, error)
	GetTableInfo(ctx context.Context, tableName string) (TableInfo, error)

	// Index operations
	CreateIndex(ctx context.Context, indexDef IndexDefinition) error
	DropIndex(ctx context.Context, indexName string) error
	IndexExists(ctx context.Context, indexName string) (bool, error)
	ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error)

	// Constraint operations
	AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error
	DropConstraint(ctx context.Context, tableName, constraintName string) error
	ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error)

	// Database metadata
	GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error)
	GetVersion(ctx context.Context) (string, error)
	GetDialect() DatabaseDialect

	// SQL adaptation
	AdaptSQL(sql string) string
	ValidateSQL(sql string) error

	// Migration-specific operations
	CreateMigrationTable(ctx context.Context, tableName string) error
	CreateLockTable(ctx context.Context, tableName string) error
	
	// Backup and restore
	CreateBackup(ctx context.Context, backupConfig BackupConfig) error
	RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error
}

// Transaction represents a database transaction
type Transaction interface {
	Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error)
	Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow
	Commit() error
	Rollback() error
	Savepoint(name string) error
	RollbackToSavepoint(name string) error
	ReleaseSavepoint(name string) error
}

// QueryResult represents the result of a query execution
type QueryResult interface {
	RowsAffected() (int64, error)
	LastInsertId() (int64, error)
}

// QueryRows represents multiple rows returned from a query
type QueryRows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Columns() ([]string, error)
	ColumnTypes() ([]*sql.ColumnType, error)
	Close() error
	Err() error
}

// QueryRow represents a single row returned from a query
type QueryRow interface {
	Scan(dest ...interface{}) error
}

// DatabaseDialect represents different database types
type DatabaseDialect string

const (
	DialectPostgreSQL DatabaseDialect = "postgresql"
	DialectMySQL      DatabaseDialect = "mysql"
	DialectSQLite     DatabaseDialect = "sqlite"
	DialectSQLServer  DatabaseDialect = "sqlserver"
	DialectOracle     DatabaseDialect = "oracle"
	DialectDB2        DatabaseDialect = "db2"
	DialectCassandra  DatabaseDialect = "cassandra"
	DialectMongoDB    DatabaseDialect = "mongodb"
	DialectRedis      DatabaseDialect = "redis"
)

// IsolationLevel represents database transaction isolation levels
type IsolationLevel string

const (
	IsolationReadUncommitted IsolationLevel = "READ_UNCOMMITTED"
	IsolationReadCommitted   IsolationLevel = "READ_COMMITTED"
	IsolationRepeatableRead  IsolationLevel = "REPEATABLE_READ"
	IsolationSerializable    IsolationLevel = "SERIALIZABLE"
)

// TransactionOptions defines options for transaction creation
type TransactionOptions struct {
	IsolationLevel sql.IsolationLevel
	ReadOnly       bool
	Timeout        time.Duration
}

// TableDefinition defines a table structure
type TableDefinition struct {
	Name        string
	Schema      string
	Columns     []ColumnDefinition
	Constraints []ConstraintDefinition
	Indexes     []IndexDefinition
	Options     map[string]interface{}
}

// ColumnDefinition defines a table column
type ColumnDefinition struct {
	Name         string
	Type         string
	Size         int
	Precision    int
	Scale        int
	Nullable     bool
	DefaultValue interface{}
	AutoIncrement bool
	Primary      bool
	Unique       bool
	Comment      string
}

// IndexDefinition defines an index
type IndexDefinition struct {
	Name     string
	Table    string
	Columns  []string
	Unique   bool
	Type     string
	Where    string
	Options  map[string]interface{}
}

// ConstraintDefinition defines a constraint
type ConstraintDefinition struct {
	Name           string
	Type           ConstraintType
	Table          string
	Columns        []string
	ReferencedTable string
	ReferencedColumns []string
	OnUpdate       ReferentialAction
	OnDelete       ReferentialAction
	CheckExpression string
}

// ConstraintType represents different constraint types
type ConstraintType string

const (
	ConstraintPrimaryKey ConstraintType = "PRIMARY_KEY"
	ConstraintForeignKey ConstraintType = "FOREIGN_KEY"
	ConstraintUnique     ConstraintType = "UNIQUE"
	ConstraintCheck      ConstraintType = "CHECK"
	ConstraintNotNull    ConstraintType = "NOT_NULL"
)

// ReferentialAction represents foreign key actions
type ReferentialAction string

const (
	ActionCascade    ReferentialAction = "CASCADE"
	ActionRestrict   ReferentialAction = "RESTRICT"
	ActionSetNull    ReferentialAction = "SET_NULL"
	ActionSetDefault ReferentialAction = "SET_DEFAULT"
	ActionNoAction   ReferentialAction = "NO_ACTION"
)

// TableInfo contains information about a table
type TableInfo struct {
	Name        string
	Schema      string
	Type        string
	Columns     []ColumnInfo
	Constraints []ConstraintInfo
	Indexes     []IndexInfo
	RowCount    int64
	Size        int64
	Comment     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ColumnInfo contains information about a column
type ColumnInfo struct {
	Name          string
	Type          string
	Size          int
	Precision     int
	Scale         int
	Nullable      bool
	DefaultValue  interface{}
	AutoIncrement bool
	Primary       bool
	Unique        bool
	Comment       string
	Position      int
}

// IndexInfo contains information about an index
type IndexInfo struct {
	Name     string
	Table    string
	Columns  []string
	Unique   bool
	Type     string
	Size     int64
	Comment  string
}

// ConstraintInfo contains information about a constraint
type ConstraintInfo struct {
	Name              string
	Type              ConstraintType
	Table             string
	Columns           []string
	ReferencedTable   string
	ReferencedColumns []string
	OnUpdate          ReferentialAction
	OnDelete          ReferentialAction
	CheckExpression   string
	Enabled           bool
}

// DatabaseInfo contains information about the database
type DatabaseInfo struct {
	Name           string
	Version        string
	Dialect        DatabaseDialect
	CharacterSet   string
	Collation      string
	Size           int64
	MaxConnections int
	CurrentConnections int
	Uptime         time.Duration
	ServerInfo     map[string]interface{}
}

// BackupConfig defines backup configuration
type BackupConfig struct {
	Path         string
	Format       BackupFormat
	Compression  bool
	IncludeData  bool
	IncludeSchema bool
	Tables       []string
	Options      map[string]interface{}
}

// RestoreConfig defines restore configuration
type RestoreConfig struct {
	Path         string
	Format       BackupFormat
	DropExisting bool
	IgnoreErrors bool
	Tables       []string
	Options      map[string]interface{}
}

// BackupFormat represents backup file formats
type BackupFormat string

const (
	BackupFormatSQL    BackupFormat = "sql"
	BackupFormatBinary BackupFormat = "binary"
	BackupFormatJSON   BackupFormat = "json"
	BackupFormatCSV    BackupFormat = "csv"
)

// BackendFactory creates database-specific backend implementations
type BackendFactory interface {
	CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error)
	SupportedDialects() []DatabaseDialect
	GetDialectInfo(dialect DatabaseDialect) DialectInfo
}

// DialectInfo contains information about a database dialect
type DialectInfo struct {
	Name         string
	Version      string
	Features     []string
	Limitations  []string
	DefaultPort  int
	URLSchemes   []string
	FileExtensions []string
}

// ConnectionPool manages database connections
type ConnectionPool interface {
	Get(ctx context.Context) (DatabaseBackend, error)
	Put(backend DatabaseBackend) error
	Close() error
	Stats() PoolStats
}

// PoolStats contains connection pool statistics
type PoolStats struct {
	MaxConnections     int
	ActiveConnections  int
	IdleConnections    int
	WaitingConnections int
	TotalConnections   int64
	TotalAcquired      int64
	TotalReleased      int64
}

// BackendRegistry manages registered backend implementations
type BackendRegistry interface {
	Register(dialect DatabaseDialect, factory BackendFactory) error
	Unregister(dialect DatabaseDialect) error
	Get(dialect DatabaseDialect) (BackendFactory, error)
	GetDialectInfo(dialect DatabaseDialect) (DialectInfo, error)
	List() []DatabaseDialect
	IsSupported(dialect DatabaseDialect) bool
}

// BackendMiddleware provides middleware functionality for backends
type BackendMiddleware interface {
	Wrap(backend DatabaseBackend) DatabaseBackend
}

// BackendDecorator decorates backend operations
type BackendDecorator interface {
	DecorateExecute(next ExecuteFunc) ExecuteFunc
	DecorateQuery(next QueryFunc) QueryFunc
	DecorateTransaction(next TransactionFunc) TransactionFunc
}

// Function types for decoration
type ExecuteFunc func(ctx context.Context, query string, args ...interface{}) (QueryResult, error)
type QueryFunc func(ctx context.Context, query string, args ...interface{}) (QueryRows, error)
type TransactionFunc func(ctx context.Context, opts TransactionOptions) (Transaction, error)

// BackendMetrics provides metrics for backend operations
type BackendMetrics interface {
	RecordQuery(dialect DatabaseDialect, query string, duration time.Duration, err error)
	RecordTransaction(dialect DatabaseDialect, duration time.Duration, err error)
	RecordConnection(dialect DatabaseDialect, duration time.Duration, err error)
	GetMetrics() map[string]interface{}
}

// BackendLogger provides logging for backend operations
type BackendLogger interface {
	LogQuery(ctx context.Context, dialect DatabaseDialect, query string, args []interface{}, duration time.Duration, err error)
	LogTransaction(ctx context.Context, dialect DatabaseDialect, operation string, duration time.Duration, err error)
	LogConnection(ctx context.Context, dialect DatabaseDialect, operation string, duration time.Duration, err error)
}

// BackendValidator validates backend operations
type BackendValidator interface {
	ValidateConfig(config types.DatabaseConfig) error
	ValidateQuery(dialect DatabaseDialect, query string) error
	ValidateSchema(dialect DatabaseDialect, schema string) error
}

// BackendCache provides caching for backend operations
type BackendCache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, ttl time.Duration)
	Delete(key string)
	Clear()
	Stats() CacheStats
}

// CacheStats contains cache statistics
type CacheStats struct {
	Hits        int64
	Misses      int64
	Evictions   int64
	Size        int64
	MaxSize     int64
	HitRate     float64
}

// BackendHealthChecker checks backend health
type BackendHealthChecker interface {
	CheckHealth(ctx context.Context, backend DatabaseBackend) HealthStatus
	GetHealthMetrics(backend DatabaseBackend) HealthMetrics
}

// HealthStatus represents backend health status
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthMetrics contains health metrics
type HealthMetrics struct {
	Status           HealthStatus
	ResponseTime     time.Duration
	ErrorRate        float64
	ConnectionCount  int
	LastChecked      time.Time
	Details          map[string]interface{}
}

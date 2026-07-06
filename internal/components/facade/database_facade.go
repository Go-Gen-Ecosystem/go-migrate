/**
 * @file database_facade.go
 * @description Database facade implementation for database operations and schema management
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/facade/database_facade.go
 */

package facade

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DefaultDatabaseFacade implements DatabaseFacade interface
type DefaultDatabaseFacade struct {
	config     *DatabaseConfig
	db         *sql.DB
	driver     string
	connected  bool
	metadata   *DatabaseMetadata
	queryCache map[string]*sql.Stmt
	logger     Logger
}

// DatabaseConfig configuration for database connection
type DatabaseConfig struct {
	Driver          string            `json:"driver"`
	Host            string            `json:"host"`
	Port            int               `json:"port"`
	Database        string            `json:"database"`
	Username        string            `json:"username"`
	Password        string            `json:"password"`
	SSLMode         string            `json:"ssl_mode"`
	ConnectTimeout  time.Duration     `json:"connect_timeout"`
	QueryTimeout    time.Duration     `json:"query_timeout"`
	MaxConnections  int               `json:"max_connections"`
	MaxIdleConns    int               `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration     `json:"conn_max_lifetime"`
	Parameters      map[string]string `json:"parameters"`
}

// DatabaseMetadata holds database metadata information
type DatabaseMetadata struct {
	Version         string
	ProductName     string
	ProductVersion  string
	DriverName      string
	DriverVersion   string
	SupportedFeatures map[string]bool
	Schemas         []string
	CurrentSchema   string
	Charset         string
	Collation       string
	TimeZone        string
}

// NewDatabaseFacade creates a new database facade instance
func NewDatabaseFacade(config *DatabaseConfig) (*DefaultDatabaseFacade, error) {
	if config == nil {
		return nil, fmt.Errorf("database config is required")
	}

	facade := &DefaultDatabaseFacade{
		config:     config,
		driver:     config.Driver,
		queryCache: make(map[string]*sql.Stmt),
	}

	return facade, nil
}

// Connect establishes database connection
func (f *DefaultDatabaseFacade) Connect(ctx context.Context) error {
	if f.connected {
		return nil
	}

	// Build connection string
	connStr, err := f.buildConnectionString()
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// Open database connection
	db, err := sql.Open(f.driver, connStr)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(f.config.MaxConnections)
	db.SetMaxIdleConns(f.config.MaxIdleConns)
	db.SetConnMaxLifetime(f.config.ConnMaxLifetime)

	// Test connection
	ctx, cancel := context.WithTimeout(ctx, f.config.ConnectTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	f.db = db
	f.connected = true

	// Load database metadata
	if err := f.loadMetadata(ctx); err != nil {
		f.logger.Warn(fmt.Sprintf("Failed to load database metadata: %v", err))
	}

	return nil
}

// Disconnect closes database connection
func (f *DefaultDatabaseFacade) Disconnect(ctx context.Context) error {
	if !f.connected || f.db == nil {
		return nil
	}

	// Close prepared statements
	for _, stmt := range f.queryCache {
		stmt.Close()
	}
	f.queryCache = make(map[string]*sql.Stmt)

	// Close database connection
	if err := f.db.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	f.db = nil
	f.connected = false

	return nil
}

// IsConnected checks if database is connected
func (f *DefaultDatabaseFacade) IsConnected(ctx context.Context) bool {
	if !f.connected || f.db == nil {
		return false
	}

	// Test connection with ping
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return f.db.PingContext(ctx) == nil
}

// GetConnection returns the database connection
func (f *DefaultDatabaseFacade) GetConnection(ctx context.Context) (*sql.DB, error) {
	if !f.connected {
		if err := f.Connect(ctx); err != nil {
			return nil, err
		}
	}

	return f.db, nil
}

// ExecuteQuery executes a query and returns results
func (f *DefaultDatabaseFacade) ExecuteQuery(ctx context.Context, query string, args ...interface{}) (*QueryResult, error) {
	if !f.connected {
		return nil, fmt.Errorf("database not connected")
	}

	startTime := time.Now()

	// Apply query timeout
	ctx, cancel := context.WithTimeout(ctx, f.config.QueryTimeout)
	defer cancel()

	// Execute query
	rows, err := f.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Read all rows
	var data [][]interface{}
	for rows.Next() {
		// Create slice for row values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan row
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		data = append(data, values)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	result := &QueryResult{
		Columns:      columns,
		ColumnTypes:  f.convertColumnTypes(columnTypes),
		Data:         data,
		RowCount:     len(data),
		ExecutionTime: time.Since(startTime),
		Query:        query,
	}

	return result, nil
}

// ExecuteStatement executes a statement (INSERT, UPDATE, DELETE)
func (f *DefaultDatabaseFacade) ExecuteStatement(ctx context.Context, statement string, args ...interface{}) (*StatementResult, error) {
	if !f.connected {
		return nil, fmt.Errorf("database not connected")
	}

	startTime := time.Now()

	// Apply query timeout
	ctx, cancel := context.WithTimeout(ctx, f.config.QueryTimeout)
	defer cancel()

	// Execute statement
	result, err := f.db.ExecContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute statement: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		f.logger.Warn(fmt.Sprintf("Failed to get rows affected: %v", err))
		rowsAffected = -1
	}

	// Get last insert ID (if applicable)
	lastInsertID, err := result.LastInsertId()
	if err != nil {
		// Not all drivers support LastInsertId
		lastInsertID = -1
	}

	statementResult := &StatementResult{
		RowsAffected:  rowsAffected,
		LastInsertID:  lastInsertID,
		ExecutionTime: time.Since(startTime),
		Statement:     statement,
	}

	return statementResult, nil
}

// BeginTransaction starts a new transaction
func (f *DefaultDatabaseFacade) BeginTransaction(ctx context.Context, options *TransactionOptions) (*Transaction, error) {
	if !f.connected {
		return nil, fmt.Errorf("database not connected")
	}

	var txOptions *sql.TxOptions
	if options != nil {
		txOptions = &sql.TxOptions{
			Isolation: f.convertIsolationLevel(options.IsolationLevel),
			ReadOnly:  options.ReadOnly,
		}
	}

	tx, err := f.db.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	transaction := &Transaction{
		tx:        tx,
		startTime: time.Now(),
		options:   options,
	}

	return transaction, nil
}

// GetDatabaseInfo returns database information
func (f *DefaultDatabaseFacade) GetDatabaseInfo(ctx context.Context) (*DatabaseInfo, error) {
	if !f.connected {
		return nil, fmt.Errorf("database not connected")
	}

	info := &DatabaseInfo{
		Name:           f.config.Database,
		Driver:         f.driver,
		Host:           f.config.Host,
		Port:           f.config.Port,
		Version:        "",
		Charset:        "",
		Collation:      "",
		TimeZone:       "",
		MaxConnections: f.config.MaxConnections,
		CurrentSchema:  "",
	}

	// Get database version
	if version, err := f.GetDatabaseVersion(ctx); err == nil {
		info.Version = version
	}

	// Get current schema
	if schema, err := f.GetCurrentSchema(ctx); err == nil {
		info.CurrentSchema = schema
	}

	// Get additional info from metadata
	if f.metadata != nil {
		info.Charset = f.metadata.Charset
		info.Collation = f.metadata.Collation
		info.TimeZone = f.metadata.TimeZone
	}

	return info, nil
}

// GetDatabaseVersion returns database version
func (f *DefaultDatabaseFacade) GetDatabaseVersion(ctx context.Context) (string, error) {
	if !f.connected {
		return "", fmt.Errorf("database not connected")
	}

	var query string
	switch f.driver {
	case "postgres":
		query = "SELECT version()"
	case "mysql":
		query = "SELECT VERSION()"
	case "sqlite3":
		query = "SELECT sqlite_version()"
	case "sqlserver":
		query = "SELECT @@VERSION"
	default:
		return "", fmt.Errorf("unsupported driver: %s", f.driver)
	}

	var version string
	err := f.db.QueryRowContext(ctx, query).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get database version: %w", err)
	}

	return version, nil
}

// GetSchemas returns list of schemas
func (f *DefaultDatabaseFacade) GetSchemas(ctx context.Context) ([]string, error) {
	if !f.connected {
		return nil, fmt.Errorf("database not connected")
	}

	var query string
	switch f.driver {
	case "postgres":
		query = "SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')"
	case "mysql":
		query = "SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')"
	case "sqlserver":
		query = "SELECT name FROM sys.schemas WHERE name NOT IN ('dbo', 'guest', 'INFORMATION_SCHEMA', 'sys')"
	default:
		return []string{}, nil // SQLite doesn't have schemas
	}

	rows, err := f.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get schemas: %w", err)
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, fmt.Errorf("failed to scan schema: %w", err)
		}
		schemas = append(schemas, schema)
	}

	return schemas, nil
}

// GetCurrentSchema returns current schema
func (f *DefaultDatabaseFacade) GetCurrentSchema(ctx context.Context) (string, error) {
	if !f.connected {
		return "", fmt.Errorf("database not connected")
	}

	var query string
	switch f.driver {
	case "postgres":
		query = "SELECT current_schema()"
	case "mysql":
		query = "SELECT DATABASE()"
	case "sqlserver":
		query = "SELECT SCHEMA_NAME()"
	default:
		return "main", nil // SQLite default schema
	}

	var schema string
	err := f.db.QueryRowContext(ctx, query).Scan(&schema)
	if err != nil {
		return "", fmt.Errorf("failed to get current schema: %w", err)
	}

	return schema, nil
}

// GetTables returns list of tables in schema
func (f *DefaultDatabaseFacade) GetTables(ctx context.Context, schema string) ([]*TableInfo, error) {
	if !f.connected {
		return nil, fmt.Errorf("database not connected")
	}

	var query string
	var args []interface{}

	switch f.driver {
	case "postgres":
		query = `
			SELECT table_name, table_type, table_comment
			FROM information_schema.tables 
			WHERE table_schema = $1
			ORDER BY table_name`
		args = []interface{}{schema}
	case "mysql":
		query = `
			SELECT table_name, table_type, table_comment
			FROM information_schema.tables 
			WHERE table_schema = ?
			ORDER BY table_name`
		args = []interface{}{schema}
	case "sqlite3":
		query = `
			SELECT name as table_name, type as table_type, '' as table_comment
			FROM sqlite_master 
			WHERE type IN ('table', 'view')
			ORDER BY name`
	default:
		return nil, fmt.Errorf("unsupported driver: %s", f.driver)
	}

	rows, err := f.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}
	defer rows.Close()

	var tables []*TableInfo
	for rows.Next() {
		var table TableInfo
		var comment sql.NullString
		
		if err := rows.Scan(&table.Name, &table.Type, &comment); err != nil {
			return nil, fmt.Errorf("failed to scan table: %w", err)
		}
		
		table.Schema = schema
		if comment.Valid {
			table.Comment = comment.String
		}

		tables = append(tables, &table)
	}

	return tables, nil
}

// TableExists checks if table exists
func (f *DefaultDatabaseFacade) TableExists(ctx context.Context, schema, table string) (bool, error) {
	if !f.connected {
		return false, fmt.Errorf("database not connected")
	}

	var query string
	var args []interface{}

	switch f.driver {
	case "postgres":
		query = "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)"
		args = []interface{}{schema, table}
	case "mysql":
		query = "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = ? AND table_name = ?)"
		args = []interface{}{schema, table}
	case "sqlite3":
		query = "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)"
		args = []interface{}{table}
	default:
		return false, fmt.Errorf("unsupported driver: %s", f.driver)
	}

	var exists bool
	err := f.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	return exists, nil
}

// CreateMigrationTable creates the migration tracking table
func (f *DefaultDatabaseFacade) CreateMigrationTable(ctx context.Context, tableName string) error {
	if !f.connected {
		return fmt.Errorf("database not connected")
	}

	var createSQL string
	switch f.driver {
	case "postgres":
		createSQL = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				id SERIAL PRIMARY KEY,
				version VARCHAR(255) NOT NULL UNIQUE,
				name VARCHAR(255) NOT NULL,
				checksum VARCHAR(255),
				applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
				execution_time INTERVAL,
				success BOOLEAN DEFAULT TRUE,
				error_message TEXT,
				rollback_sql TEXT,
				metadata JSONB
			)`, tableName)
	case "mysql":
		createSQL = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				id INT AUTO_INCREMENT PRIMARY KEY,
				version VARCHAR(255) NOT NULL UNIQUE,
				name VARCHAR(255) NOT NULL,
				checksum VARCHAR(255),
				applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				execution_time INT,
				success BOOLEAN DEFAULT TRUE,
				error_message TEXT,
				rollback_sql TEXT,
				metadata JSON,
				INDEX idx_version (version),
				INDEX idx_applied_at (applied_at)
			)`, tableName)
	case "sqlite3":
		createSQL = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				version TEXT NOT NULL UNIQUE,
				name TEXT NOT NULL,
				checksum TEXT,
				applied_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				execution_time INTEGER,
				success BOOLEAN DEFAULT 1,
				error_message TEXT,
				rollback_sql TEXT,
				metadata TEXT
			)`, tableName)
	default:
		return fmt.Errorf("unsupported driver: %s", f.driver)
	}

	_, err := f.db.ExecContext(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	return nil
}

// Private helper methods

func (f *DefaultDatabaseFacade) buildConnectionString() (string, error) {
	switch f.driver {
	case "postgres":
		return f.buildPostgresConnectionString(), nil
	case "mysql":
		return f.buildMySQLConnectionString(), nil
	case "sqlite3":
		return f.buildSQLiteConnectionString(), nil
	default:
		return "", fmt.Errorf("unsupported driver: %s", f.driver)
	}
}

func (f *DefaultDatabaseFacade) buildPostgresConnectionString() string {
	parts := []string{
		fmt.Sprintf("host=%s", f.config.Host),
		fmt.Sprintf("port=%d", f.config.Port),
		fmt.Sprintf("user=%s", f.config.Username),
		fmt.Sprintf("password=%s", f.config.Password),
		fmt.Sprintf("dbname=%s", f.config.Database),
		fmt.Sprintf("sslmode=%s", f.config.SSLMode),
		fmt.Sprintf("connect_timeout=%d", int(f.config.ConnectTimeout.Seconds())),
	}

	for key, value := range f.config.Parameters {
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(parts, " ")
}

func (f *DefaultDatabaseFacade) buildMySQLConnectionString() string {
	connStr := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		f.config.Username,
		f.config.Password,
		f.config.Host,
		f.config.Port,
		f.config.Database,
	)

	params := make([]string, 0)
	params = append(params, "parseTime=true")
	params = append(params, fmt.Sprintf("timeout=%s", f.config.ConnectTimeout))

	for key, value := range f.config.Parameters {
		params = append(params, fmt.Sprintf("%s=%s", key, value))
	}

	if len(params) > 0 {
		connStr += "?" + strings.Join(params, "&")
	}

	return connStr
}

func (f *DefaultDatabaseFacade) buildSQLiteConnectionString() string {
	connStr := f.config.Database

	params := make([]string, 0)
	for key, value := range f.config.Parameters {
		params = append(params, fmt.Sprintf("%s=%s", key, value))
	}

	if len(params) > 0 {
		connStr += "?" + strings.Join(params, "&")
	}

	return connStr
}

func (f *DefaultDatabaseFacade) loadMetadata(ctx context.Context) error {
	// Implementation to load database metadata
	f.metadata = &DatabaseMetadata{
		SupportedFeatures: make(map[string]bool),
	}
	return nil
}

func (f *DefaultDatabaseFacade) convertColumnTypes(columnTypes []*sql.ColumnType) []string {
	types := make([]string, len(columnTypes))
	for i, ct := range columnTypes {
		types[i] = ct.DatabaseTypeName()
	}
	return types
}

func (f *DefaultDatabaseFacade) convertIsolationLevel(level string) sql.IsolationLevel {
	switch strings.ToUpper(level) {
	case "READ_UNCOMMITTED":
		return sql.LevelReadUncommitted
	case "READ_COMMITTED":
		return sql.LevelReadCommitted
	case "REPEATABLE_READ":
		return sql.LevelRepeatableRead
	case "SERIALIZABLE":
		return sql.LevelSerializable
	default:
		return sql.LevelDefault
	}
}

// Transaction wrapper
type Transaction struct {
	tx        *sql.Tx
	startTime time.Time
	options   *TransactionOptions
}

func (t *Transaction) Commit() error {
	return t.tx.Commit()
}

func (t *Transaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *Transaction) ExecuteQuery(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

func (t *Transaction) ExecuteStatement(ctx context.Context, statement string, args ...interface{}) (sql.Result, error) {
	return t.tx.ExecContext(ctx, statement, args...)
}

// DefaultDatabaseConfig returns default database configuration
func DefaultDatabaseConfig() *DatabaseConfig {
	return &DatabaseConfig{
		Driver:          "postgres",
		Host:            "localhost",
		Port:            5432,
		Database:        "migrations",
		Username:        "postgres",
		Password:        "",
		SSLMode:         "disable",
		ConnectTimeout:  30 * time.Second,
		QueryTimeout:    30 * time.Second,
		MaxConnections:  25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		Parameters:      make(map[string]string),
	}
}

/**
 * @file postgresql.go
 * @description PostgreSQL backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/postgresql.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// PostgreSQLBackend implements DatabaseBackend for PostgreSQL
type PostgreSQLBackend struct {
	db     *sql.DB
	config types.DatabaseConfig
	connected bool
}

// NewPostgreSQLBackend creates a new PostgreSQL backend
func NewPostgreSQLBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &PostgreSQLBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to PostgreSQL
func (p *PostgreSQLBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if p.connected {
		return nil
	}

	dsn := p.buildDSN(config)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Hour)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	p.db = db
	p.config = config
	p.connected = true

	return nil
}

// Disconnect closes the PostgreSQL connection
func (p *PostgreSQLBackend) Disconnect(ctx context.Context) error {
	if !p.connected || p.db == nil {
		return nil
	}

	err := p.db.Close()
	p.connected = false
	p.db = nil

	return err
}

// Ping checks the PostgreSQL connection
func (p *PostgreSQLBackend) Ping(ctx context.Context) error {
	if !p.connected || p.db == nil {
		return fmt.Errorf("not connected to PostgreSQL")
	}

	return p.db.PingContext(ctx)
}

// IsConnected returns connection status
func (p *PostgreSQLBackend) IsConnected() bool {
	return p.connected && p.db != nil
}

// BeginTransaction starts a new transaction
func (p *PostgreSQLBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return p.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (p *PostgreSQLBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !p.connected {
		return nil, fmt.Errorf("not connected to PostgreSQL")
	}

	txOpts := &sql.TxOptions{
		Isolation: opts.IsolationLevel,
		ReadOnly:  opts.ReadOnly,
	}

	tx, err := p.db.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin PostgreSQL transaction: %w", err)
	}

	return &PostgreSQLTransaction{tx: tx}, nil
}

// Execute executes a SQL statement
func (p *PostgreSQLBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !p.connected {
		return nil, fmt.Errorf("not connected to PostgreSQL")
	}

	result, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute PostgreSQL query: %w", err)
	}

	return &PostgreSQLQueryResult{result: result}, nil
}

// Query executes a query that returns rows
func (p *PostgreSQLBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !p.connected {
		return nil, fmt.Errorf("not connected to PostgreSQL")
	}

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query PostgreSQL: %w", err)
	}

	return &PostgreSQLQueryRows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row
func (p *PostgreSQLBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !p.connected {
		return &PostgreSQLQueryRow{err: fmt.Errorf("not connected to PostgreSQL")}
	}

	row := p.db.QueryRowContext(ctx, query, args...)
	return &PostgreSQLQueryRow{row: row}
}

// CreateSchema creates a new schema
func (p *PostgreSQLBackend) CreateSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pq.QuoteIdentifier(schemaName))
	_, err := p.Execute(ctx, query)
	return err
}

// DropSchema drops a schema
func (p *PostgreSQLBackend) DropSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pq.QuoteIdentifier(schemaName))
	_, err := p.Execute(ctx, query)
	return err
}

// SchemaExists checks if a schema exists
func (p *PostgreSQLBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`
	row := p.QueryRow(ctx, query, schemaName)
	
	var exists bool
	err := row.Scan(&exists)
	return exists, err
}

// ListSchemas returns all schemas
func (p *PostgreSQLBackend) ListSchemas(ctx context.Context) ([]string, error) {
	query := `SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')`
	rows, err := p.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}

	return schemas, rows.Err()
}

// CreateTable creates a new table
func (p *PostgreSQLBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	query := p.buildCreateTableSQL(tableDef)
	_, err := p.Execute(ctx, query)
	return err
}

// DropTable drops a table
func (p *PostgreSQLBackend) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", pq.QuoteIdentifier(tableName))
	_, err := p.Execute(ctx, query)
	return err
}

// TableExists checks if a table exists
func (p *PostgreSQLBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	parts := strings.Split(tableName, ".")
	var schema, table string
	
	if len(parts) == 2 {
		schema, table = parts[0], parts[1]
	} else {
		schema, table = "public", tableName
	}

	query := `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`
	row := p.QueryRow(ctx, query, schema, table)
	
	var exists bool
	err := row.Scan(&exists)
	return exists, err
}

// ListTables returns all tables
func (p *PostgreSQLBackend) ListTables(ctx context.Context) ([]string, error) {
	query := `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`
	rows, err := p.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return tables, rows.Err()
}

// GetTableInfo returns table information
func (p *PostgreSQLBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	// Implementation would fetch comprehensive table information
	// This is a simplified version
	info := TableInfo{
		Name:   tableName,
		Schema: "public",
		Type:   "BASE TABLE",
	}

	// Get columns
	columns, err := p.getTableColumns(ctx, tableName)
	if err != nil {
		return info, err
	}
	info.Columns = columns

	return info, nil
}

// CreateIndex creates an index
func (p *PostgreSQLBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	query := p.buildCreateIndexSQL(indexDef)
	_, err := p.Execute(ctx, query)
	return err
}

// DropIndex drops an index
func (p *PostgreSQLBackend) DropIndex(ctx context.Context, indexName string) error {
	query := fmt.Sprintf("DROP INDEX IF EXISTS %s", pq.QuoteIdentifier(indexName))
	_, err := p.Execute(ctx, query)
	return err
}

// IndexExists checks if an index exists
func (p *PostgreSQLBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = $1)`
	row := p.QueryRow(ctx, query, indexName)
	
	var exists bool
	err := row.Scan(&exists)
	return exists, err
}

// ListIndexes returns all indexes for a table
func (p *PostgreSQLBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	query := `SELECT indexname, indexdef FROM pg_indexes WHERE tablename = $1`
	rows, err := p.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			return nil, err
		}
		
		indexes = append(indexes, IndexInfo{
			Name:  name,
			Table: tableName,
		})
	}

	return indexes, rows.Err()
}

// AddConstraint adds a constraint
func (p *PostgreSQLBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	query := p.buildAddConstraintSQL(constraintDef)
	_, err := p.Execute(ctx, query)
	return err
}

// DropConstraint drops a constraint
func (p *PostgreSQLBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	query := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", 
		pq.QuoteIdentifier(tableName), pq.QuoteIdentifier(constraintName))
	_, err := p.Execute(ctx, query)
	return err
}

// ListConstraints returns all constraints for a table
func (p *PostgreSQLBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	// Implementation would fetch constraint information from system tables
	return []ConstraintInfo{}, nil
}

// GetDatabaseInfo returns database information
func (p *PostgreSQLBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := p.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	return DatabaseInfo{
		Name:    p.config.Database,
		Version: version,
		Dialect: DialectPostgreSQL,
	}, nil
}

// GetVersion returns PostgreSQL version
func (p *PostgreSQLBackend) GetVersion(ctx context.Context) (string, error) {
	row := p.QueryRow(ctx, "SELECT version()")
	var version string
	err := row.Scan(&version)
	return version, err
}

// GetDialect returns the database dialect
func (p *PostgreSQLBackend) GetDialect() DatabaseDialect {
	return DialectPostgreSQL
}

// AdaptSQL adapts SQL for PostgreSQL
func (p *PostgreSQLBackend) AdaptSQL(sql string) string {
	// PostgreSQL-specific adaptations
	sql = strings.ReplaceAll(sql, "`", "\"")
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "SERIAL")
	sql = strings.ReplaceAll(sql, "AUTOINCREMENT", "SERIAL")
	
	return sql
}

// ValidateSQL validates SQL for PostgreSQL
func (p *PostgreSQLBackend) ValidateSQL(sql string) error {
	// Basic SQL validation for PostgreSQL
	sql = strings.TrimSpace(strings.ToUpper(sql))
	
	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"DROP SCHEMA.*CASCADE",
		"TRUNCATE.*CASCADE",
	}
	
	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous SQL operation detected: %s", pattern)
		}
	}
	
	return nil
}

// CreateMigrationTable creates the migration tracking table
func (p *PostgreSQLBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			version VARCHAR(255) NOT NULL UNIQUE,
			description TEXT,
			type VARCHAR(50) NOT NULL DEFAULT 'migration',
			installed_by VARCHAR(100),
			installed_on TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			execution_time INTEGER,
			success BOOLEAN DEFAULT TRUE,
			checksum VARCHAR(255)
		)`, pq.QuoteIdentifier(tableName))
	
	_, err := p.Execute(ctx, query)
	return err
}

// CreateLockTable creates the migration lock table
func (p *PostgreSQLBackend) CreateLockTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			lock_key VARCHAR(255) NOT NULL UNIQUE,
			locked_by VARCHAR(255),
			locked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		)`, pq.QuoteIdentifier(tableName))
	
	_, err := p.Execute(ctx, query)
	return err
}

// CreateBackup creates a database backup
func (p *PostgreSQLBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	// Implementation would use pg_dump or similar
	return fmt.Errorf("backup not implemented for PostgreSQL backend")
}

// RestoreBackup restores a database backup
func (p *PostgreSQLBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	// Implementation would use pg_restore or similar
	return fmt.Errorf("restore not implemented for PostgreSQL backend")
}

// Helper methods

func (p *PostgreSQLBackend) buildDSN(config types.DatabaseConfig) string {
	var parts []string
	
	if config.Host != "" {
		parts = append(parts, fmt.Sprintf("host=%s", config.Host))
	}
	if config.Port > 0 {
		parts = append(parts, fmt.Sprintf("port=%d", config.Port))
	}
	if config.Database != "" {
		parts = append(parts, fmt.Sprintf("dbname=%s", config.Database))
	}
	if config.Username != "" {
		parts = append(parts, fmt.Sprintf("user=%s", config.Username))
	}
	if config.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", config.Password))
	}
	
	if config.SSLMode != "" && config.SSLMode != "disable" {
		parts = append(parts, "sslmode="+config.SSLMode)
	} else {
		parts = append(parts, "sslmode=disable")
	}
	
	if config.ConnectionTimeout > 0 {
		parts = append(parts, fmt.Sprintf("connect_timeout=%d", int(config.ConnectionTimeout.Seconds())))
	}
	
	return strings.Join(parts, " ")
}

func (p *PostgreSQLBackend) buildCreateTableSQL(tableDef TableDefinition) string {
	var parts []string
	
	// Build columns
	var columns []string
	for _, col := range tableDef.Columns {
		columns = append(columns, p.buildColumnSQL(col))
	}
	
	parts = append(parts, fmt.Sprintf("CREATE TABLE %s (%s)", 
		pq.QuoteIdentifier(tableDef.Name), strings.Join(columns, ", ")))
	
	return strings.Join(parts, " ")
}

func (p *PostgreSQLBackend) buildColumnSQL(col ColumnDefinition) string {
	var parts []string
	
	parts = append(parts, pq.QuoteIdentifier(col.Name))
	
	// Handle auto increment
	if col.AutoIncrement {
		if col.Type == "INTEGER" || col.Type == "INT" {
			parts = append(parts, "SERIAL")
		} else if col.Type == "BIGINT" {
			parts = append(parts, "BIGSERIAL")
		} else {
			parts = append(parts, col.Type)
		}
	} else {
		parts = append(parts, col.Type)
	}
	
	if !col.Nullable {
		parts = append(parts, "NOT NULL")
	}
	
	if col.DefaultValue != nil {
		parts = append(parts, fmt.Sprintf("DEFAULT %v", col.DefaultValue))
	}
	
	if col.Primary {
		parts = append(parts, "PRIMARY KEY")
	}
	
	if col.Unique {
		parts = append(parts, "UNIQUE")
	}
	
	return strings.Join(parts, " ")
}

func (p *PostgreSQLBackend) buildCreateIndexSQL(indexDef IndexDefinition) string {
	var parts []string
	
	if indexDef.Unique {
		parts = append(parts, "CREATE UNIQUE INDEX")
	} else {
		parts = append(parts, "CREATE INDEX")
	}
	
	parts = append(parts, pq.QuoteIdentifier(indexDef.Name))
	parts = append(parts, "ON")
	parts = append(parts, pq.QuoteIdentifier(indexDef.Table))
	
	columns := make([]string, len(indexDef.Columns))
	for i, col := range indexDef.Columns {
		columns[i] = pq.QuoteIdentifier(col)
	}
	parts = append(parts, fmt.Sprintf("(%s)", strings.Join(columns, ", ")))
	
	if indexDef.Where != "" {
		parts = append(parts, "WHERE", indexDef.Where)
	}
	
	return strings.Join(parts, " ")
}

func (p *PostgreSQLBackend) buildAddConstraintSQL(constraintDef ConstraintDefinition) string {
	var parts []string
	
	parts = append(parts, "ALTER TABLE")
	parts = append(parts, pq.QuoteIdentifier(constraintDef.Table))
	parts = append(parts, "ADD CONSTRAINT")
	parts = append(parts, pq.QuoteIdentifier(constraintDef.Name))
	
	switch constraintDef.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraintDef.Columns))
		for i, col := range constraintDef.Columns {
			columns[i] = pq.QuoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))
		
	case ConstraintForeignKey:
		columns := make([]string, len(constraintDef.Columns))
		for i, col := range constraintDef.Columns {
			columns[i] = pq.QuoteIdentifier(col)
		}
		refColumns := make([]string, len(constraintDef.ReferencedColumns))
		for i, col := range constraintDef.ReferencedColumns {
			refColumns[i] = pq.QuoteIdentifier(col)
		}
		
		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			pq.QuoteIdentifier(constraintDef.ReferencedTable),
			strings.Join(refColumns, ", ")))
			
		if constraintDef.OnUpdate != "" {
			parts = append(parts, "ON UPDATE", string(constraintDef.OnUpdate))
		}
		if constraintDef.OnDelete != "" {
			parts = append(parts, "ON DELETE", string(constraintDef.OnDelete))
		}
		
	case ConstraintUnique:
		columns := make([]string, len(constraintDef.Columns))
		for i, col := range constraintDef.Columns {
			columns[i] = pq.QuoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))
		
	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraintDef.CheckExpression))
	}
	
	return strings.Join(parts, " ")
}

func (p *PostgreSQLBackend) getTableColumns(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT column_name, data_type, is_nullable, column_default, ordinal_position
		FROM information_schema.columns 
		WHERE table_name = $1 AND table_schema = 'public'
		ORDER BY ordinal_position`
	
	rows, err := p.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var nullable string
		var defaultValue sql.NullString
		
		err := rows.Scan(&col.Name, &col.Type, &nullable, &defaultValue, &col.Position)
		if err != nil {
			return nil, err
		}
		
		col.Nullable = nullable == "YES"
		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}
		
		columns = append(columns, col)
	}
	
	return columns, rows.Err()
}

// PostgreSQL-specific implementations

// PostgreSQLTransaction implements Transaction for PostgreSQL
type PostgreSQLTransaction struct {
	tx *sql.Tx
}

func (t *PostgreSQLTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &PostgreSQLQueryResult{result: result}, nil
}

func (t *PostgreSQLTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &PostgreSQLQueryRows{rows: rows}, nil
}

func (t *PostgreSQLTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &PostgreSQLQueryRow{row: row}
}

func (t *PostgreSQLTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *PostgreSQLTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *PostgreSQLTransaction) Savepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("SAVEPOINT %s", pq.QuoteIdentifier(name)))
	return err
}

func (t *PostgreSQLTransaction) RollbackToSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", pq.QuoteIdentifier(name)))
	return err
}

func (t *PostgreSQLTransaction) ReleaseSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("RELEASE SAVEPOINT %s", pq.QuoteIdentifier(name)))
	return err
}

// PostgreSQLQueryResult implements QueryResult for PostgreSQL
type PostgreSQLQueryResult struct {
	result sql.Result
}

func (r *PostgreSQLQueryResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

func (r *PostgreSQLQueryResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

// PostgreSQLQueryRows implements QueryRows for PostgreSQL
type PostgreSQLQueryRows struct {
	rows *sql.Rows
}

func (r *PostgreSQLQueryRows) Next() bool {
	return r.rows.Next()
}

func (r *PostgreSQLQueryRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *PostgreSQLQueryRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *PostgreSQLQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r *PostgreSQLQueryRows) Close() error {
	return r.rows.Close()
}

func (r *PostgreSQLQueryRows) Err() error {
	return r.rows.Err()
}

// PostgreSQLQueryRow implements QueryRow for PostgreSQL
type PostgreSQLQueryRow struct {
	row *sql.Row
	err error
}

func (r *PostgreSQLQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(dest...)
}

// PostgreSQLBackendFactory creates PostgreSQL backends
type PostgreSQLBackendFactory struct{}

func NewPostgreSQLBackendFactory() BackendFactory {
	return &PostgreSQLBackendFactory{}
}

func (f *PostgreSQLBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectPostgreSQL {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}
	
	return NewPostgreSQLBackend(config)
}

func (f *PostgreSQLBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectPostgreSQL}
}

func (f *PostgreSQLBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectPostgreSQL {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register PostgreSQL backend
func init() {
	RegisterBackend(DialectPostgreSQL, NewPostgreSQLBackendFactory())
}

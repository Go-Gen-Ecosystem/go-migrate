/**
 * @file mysql.go
 * @description MySQL backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/mysql.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// MySQLBackend implements DatabaseBackend for MySQL
type MySQLBackend struct {
	db        *sql.DB
	config    types.DatabaseConfig
	connected bool
}

// NewMySQLBackend creates a new MySQL backend
func NewMySQLBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &MySQLBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to MySQL
func (m *MySQLBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if m.connected {
		return nil
	}

	dsn := m.buildDSN(config)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open MySQL connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxLifetime(time.Hour)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping MySQL: %w", err)
	}

	m.db = db
	m.config = config
	m.connected = true

	return nil
}

// Disconnect closes the MySQL connection
func (m *MySQLBackend) Disconnect(ctx context.Context) error {
	if !m.connected || m.db == nil {
		return nil
	}

	err := m.db.Close()
	m.connected = false
	m.db = nil

	return err
}

// Ping checks the MySQL connection
func (m *MySQLBackend) Ping(ctx context.Context) error {
	if !m.connected || m.db == nil {
		return fmt.Errorf("not connected to MySQL")
	}

	return m.db.PingContext(ctx)
}

// IsConnected returns connection status
func (m *MySQLBackend) IsConnected() bool {
	return m.connected && m.db != nil
}

// BeginTransaction starts a new transaction
func (m *MySQLBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return m.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (m *MySQLBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected to MySQL")
	}

	txOpts := &sql.TxOptions{
		Isolation: opts.IsolationLevel,
		ReadOnly:  opts.ReadOnly,
	}

	tx, err := m.db.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin MySQL transaction: %w", err)
	}

	return &MySQLTransaction{tx: tx}, nil
}

// Execute executes a SQL statement
func (m *MySQLBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected to MySQL")
	}

	result, err := m.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute MySQL query: %w", err)
	}

	return &MySQLQueryResult{result: result}, nil
}

// Query executes a query that returns rows
func (m *MySQLBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected to MySQL")
	}

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query MySQL: %w", err)
	}

	return &MySQLQueryRows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row
func (m *MySQLBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !m.connected {
		return &MySQLQueryRow{err: fmt.Errorf("not connected to MySQL")}
	}

	row := m.db.QueryRowContext(ctx, query, args...)
	return &MySQLQueryRow{row: row}
}

// CreateSchema creates a new schema (database in MySQL)
func (m *MySQLBackend) CreateSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", schemaName)
	_, err := m.Execute(ctx, query)
	return err
}

// DropSchema drops a schema (database in MySQL)
func (m *MySQLBackend) DropSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", schemaName)
	_, err := m.Execute(ctx, query)
	return err
}

// SchemaExists checks if a schema exists
func (m *MySQLBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	query := "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?"
	row := m.QueryRow(ctx, query, schemaName)
	
	var name string
	err := row.Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ListSchemas returns all schemas
func (m *MySQLBackend) ListSchemas(ctx context.Context) ([]string, error) {
	query := "SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')"
	rows, err := m.Query(ctx, query)
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
func (m *MySQLBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	query := m.buildCreateTableSQL(tableDef)
	_, err := m.Execute(ctx, query)
	return err
}

// DropTable drops a table
func (m *MySQLBackend) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tableName)
	_, err := m.Execute(ctx, query)
	return err
}

// TableExists checks if a table exists
func (m *MySQLBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?"
	row := m.QueryRow(ctx, query, tableName)
	
	var name string
	err := row.Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ListTables returns all tables
func (m *MySQLBackend) ListTables(ctx context.Context) ([]string, error) {
	query := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'"
	rows, err := m.Query(ctx, query)
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
func (m *MySQLBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name:   tableName,
		Schema: m.config.Database,
		Type:   "BASE TABLE",
	}

	// Get columns
	columns, err := m.getTableColumns(ctx, tableName)
	if err != nil {
		return info, err
	}
	info.Columns = columns

	return info, nil
}

// CreateIndex creates an index
func (m *MySQLBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	query := m.buildCreateIndexSQL(indexDef)
	_, err := m.Execute(ctx, query)
	return err
}

// DropIndex drops an index
func (m *MySQLBackend) DropIndex(ctx context.Context, indexName string) error {
	// MySQL requires table name for dropping index
	query := fmt.Sprintf("DROP INDEX `%s` ON `%s`", indexName, indexName) // This is simplified
	_, err := m.Execute(ctx, query)
	return err
}

// IndexExists checks if an index exists
func (m *MySQLBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	query := "SELECT INDEX_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND INDEX_NAME = ?"
	row := m.QueryRow(ctx, query, indexName)
	
	var name string
	err := row.Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ListIndexes returns all indexes for a table
func (m *MySQLBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	query := "SELECT DISTINCT INDEX_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?"
	rows, err := m.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
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
func (m *MySQLBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	query := m.buildAddConstraintSQL(constraintDef)
	_, err := m.Execute(ctx, query)
	return err
}

// DropConstraint drops a constraint
func (m *MySQLBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	query := fmt.Sprintf("ALTER TABLE `%s` DROP CONSTRAINT `%s`", tableName, constraintName)
	_, err := m.Execute(ctx, query)
	return err
}

// ListConstraints returns all constraints for a table
func (m *MySQLBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	// Implementation would fetch constraint information from system tables
	return []ConstraintInfo{}, nil
}

// GetDatabaseInfo returns database information
func (m *MySQLBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := m.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	return DatabaseInfo{
		Name:    m.config.Database,
		Version: version,
		Dialect: DialectMySQL,
	}, nil
}

// GetVersion returns MySQL version
func (m *MySQLBackend) GetVersion(ctx context.Context) (string, error) {
	row := m.QueryRow(ctx, "SELECT VERSION()")
	var version string
	err := row.Scan(&version)
	return version, err
}

// GetDialect returns the database dialect
func (m *MySQLBackend) GetDialect() DatabaseDialect {
	return DialectMySQL
}

// AdaptSQL adapts SQL for MySQL
func (m *MySQLBackend) AdaptSQL(sql string) string {
	// MySQL-specific adaptations
	sql = strings.ReplaceAll(sql, "\"", "`")
	sql = strings.ReplaceAll(sql, "SERIAL", "AUTO_INCREMENT")
	sql = strings.ReplaceAll(sql, "CURRENT_TIMESTAMP", "NOW()")
	
	// Handle LIMIT syntax
	re := regexp.MustCompile(`FETCH FIRST (\d+) ROWS ONLY`)
	sql = re.ReplaceAllString(sql, "LIMIT $1")
	
	return sql
}

// ValidateSQL validates SQL for MySQL
func (m *MySQLBackend) ValidateSQL(sql string) error {
	// Basic SQL validation for MySQL
	sql = strings.TrimSpace(strings.ToUpper(sql))
	
	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"TRUNCATE TABLE",
		"DELETE FROM .* WHERE 1=1",
	}
	
	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous SQL operation detected: %s", pattern)
		}
	}
	
	return nil
}

// CreateMigrationTable creates the migration tracking table
func (m *MySQLBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INT AUTO_INCREMENT PRIMARY KEY,
			version VARCHAR(255) NOT NULL UNIQUE,
			description TEXT,
			type VARCHAR(50) NOT NULL DEFAULT 'migration',
			installed_by VARCHAR(100),
			installed_on TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			execution_time INT,
			success BOOLEAN DEFAULT TRUE,
			checksum VARCHAR(255)
		) ENGINE=InnoDB`, m.quoteIdentifier(tableName))
	
	_, err := m.Execute(ctx, query)
	return err
}

// CreateLockTable creates the migration lock table
func (m *MySQLBackend) CreateLockTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INT AUTO_INCREMENT PRIMARY KEY,
			lock_key VARCHAR(255) NOT NULL UNIQUE,
			locked_by VARCHAR(255),
			locked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NULL
		) ENGINE=InnoDB`, m.quoteIdentifier(tableName))
	
	_, err := m.Execute(ctx, query)
	return err
}

// CreateBackup creates a database backup
func (m *MySQLBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	// Implementation would use mysqldump or similar
	return fmt.Errorf("backup not implemented for MySQL backend")
}

// RestoreBackup restores a database backup
func (m *MySQLBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	// Implementation would use mysql client or similar
	return fmt.Errorf("restore not implemented for MySQL backend")
}

// Helper methods

func (m *MySQLBackend) buildDSN(config types.DatabaseConfig) string {
	var dsn strings.Builder
	
	if config.Username != "" {
		dsn.WriteString(config.Username)
		if config.Password != "" {
			dsn.WriteString(":")
			dsn.WriteString(config.Password)
		}
		dsn.WriteString("@")
	}
	
	if config.Host != "" {
		dsn.WriteString("tcp(")
		dsn.WriteString(config.Host)
		if config.Port > 0 {
			dsn.WriteString(fmt.Sprintf(":%d", config.Port))
		}
		dsn.WriteString(")")
	}
	
	if config.Database != "" {
		dsn.WriteString("/")
		dsn.WriteString(config.Database)
	}
	
	// Add parameters
	params := []string{}
	
	if config.ConnectionTimeout > 0 {
		params = append(params, fmt.Sprintf("timeout=%ds", int(config.ConnectionTimeout.Seconds())))
	}
	
	params = append(params, "parseTime=true")
	params = append(params, "charset=utf8mb4")
	params = append(params, "collation=utf8mb4_unicode_ci")
	
	if config.SSLMode != "" && config.SSLMode != "disable" {
		params = append(params, "tls=true")
	} else {
		params = append(params, "tls=false")
	}
	
	if len(params) > 0 {
		dsn.WriteString("?")
		dsn.WriteString(strings.Join(params, "&"))
	}
	
	return dsn.String()
}

func (m *MySQLBackend) quoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func (m *MySQLBackend) buildCreateTableSQL(tableDef TableDefinition) string {
	var parts []string
	
	// Build columns
	var columns []string
	for _, col := range tableDef.Columns {
		columns = append(columns, m.buildColumnSQL(col))
	}
	
	parts = append(parts, fmt.Sprintf("CREATE TABLE %s (%s)", 
		m.quoteIdentifier(tableDef.Name), strings.Join(columns, ", ")))
	
	// Add table options
	parts = append(parts, "ENGINE=InnoDB")
	parts = append(parts, "DEFAULT CHARSET=utf8mb4")
	parts = append(parts, "COLLATE=utf8mb4_unicode_ci")
	
	return strings.Join(parts, " ")
}

func (m *MySQLBackend) buildColumnSQL(col ColumnDefinition) string {
	var parts []string
	
	parts = append(parts, m.quoteIdentifier(col.Name))
	parts = append(parts, col.Type)
	
	if col.Size > 0 {
		if col.Precision > 0 {
			parts[len(parts)-1] = fmt.Sprintf("%s(%d,%d)", col.Type, col.Size, col.Precision)
		} else {
			parts[len(parts)-1] = fmt.Sprintf("%s(%d)", col.Type, col.Size)
		}
	}
	
	if !col.Nullable {
		parts = append(parts, "NOT NULL")
	}
	
	if col.AutoIncrement {
		parts = append(parts, "AUTO_INCREMENT")
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
	
	if col.Comment != "" {
		parts = append(parts, fmt.Sprintf("COMMENT '%s'", strings.ReplaceAll(col.Comment, "'", "''")))
	}
	
	return strings.Join(parts, " ")
}

func (m *MySQLBackend) buildCreateIndexSQL(indexDef IndexDefinition) string {
	var parts []string
	
	if indexDef.Unique {
		parts = append(parts, "CREATE UNIQUE INDEX")
	} else {
		parts = append(parts, "CREATE INDEX")
	}
	
	parts = append(parts, m.quoteIdentifier(indexDef.Name))
	parts = append(parts, "ON")
	parts = append(parts, m.quoteIdentifier(indexDef.Table))
	
	columns := make([]string, len(indexDef.Columns))
	for i, col := range indexDef.Columns {
		columns[i] = m.quoteIdentifier(col)
	}
	parts = append(parts, fmt.Sprintf("(%s)", strings.Join(columns, ", ")))
	
	return strings.Join(parts, " ")
}

func (m *MySQLBackend) buildAddConstraintSQL(constraintDef ConstraintDefinition) string {
	var parts []string
	
	parts = append(parts, "ALTER TABLE")
	parts = append(parts, m.quoteIdentifier(constraintDef.Table))
	parts = append(parts, "ADD CONSTRAINT")
	parts = append(parts, m.quoteIdentifier(constraintDef.Name))
	
	switch constraintDef.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraintDef.Columns))
		for i, col := range constraintDef.Columns {
			columns[i] = m.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))
		
	case ConstraintForeignKey:
		columns := make([]string, len(constraintDef.Columns))
		for i, col := range constraintDef.Columns {
			columns[i] = m.quoteIdentifier(col)
		}
		refColumns := make([]string, len(constraintDef.ReferencedColumns))
		for i, col := range constraintDef.ReferencedColumns {
			refColumns[i] = m.quoteIdentifier(col)
		}
		
		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			m.quoteIdentifier(constraintDef.ReferencedTable),
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
			columns[i] = m.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))
		
	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraintDef.CheckExpression))
	}
	
	return strings.Join(parts, " ")
}

func (m *MySQLBackend) getTableColumns(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, ORDINAL_POSITION,
			   COLUMN_KEY, EXTRA, COLUMN_COMMENT
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`
	
	rows, err := m.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var nullable, key, extra, comment string
		var defaultValue sql.NullString
		
		err := rows.Scan(&col.Name, &col.Type, &nullable, &defaultValue, &col.Position, &key, &extra, &comment)
		if err != nil {
			return nil, err
		}
		
		col.Nullable = nullable == "YES"
		col.Primary = key == "PRI"
		col.Unique = key == "UNI"
		col.AutoIncrement = strings.Contains(extra, "auto_increment")
		col.Comment = comment
		
		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}
		
		columns = append(columns, col)
	}
	
	return columns, rows.Err()
}

// MySQL-specific implementations

// MySQLTransaction implements Transaction for MySQL
type MySQLTransaction struct {
	tx *sql.Tx
}

func (t *MySQLTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &MySQLQueryResult{result: result}, nil
}

func (t *MySQLTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &MySQLQueryRows{rows: rows}, nil
}

func (t *MySQLTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &MySQLQueryRow{row: row}
}

func (t *MySQLTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *MySQLTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *MySQLTransaction) Savepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("SAVEPOINT `%s`", name))
	return err
}

func (t *MySQLTransaction) RollbackToSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT `%s`", name))
	return err
}

func (t *MySQLTransaction) ReleaseSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("RELEASE SAVEPOINT `%s`", name))
	return err
}

// MySQLQueryResult implements QueryResult for MySQL
type MySQLQueryResult struct {
	result sql.Result
}

func (r *MySQLQueryResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

func (r *MySQLQueryResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

// MySQLQueryRows implements QueryRows for MySQL
type MySQLQueryRows struct {
	rows *sql.Rows
}

func (r *MySQLQueryRows) Next() bool {
	return r.rows.Next()
}

func (r *MySQLQueryRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *MySQLQueryRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *MySQLQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r *MySQLQueryRows) Close() error {
	return r.rows.Close()
}

func (r *MySQLQueryRows) Err() error {
	return r.rows.Err()
}

// MySQLQueryRow implements QueryRow for MySQL
type MySQLQueryRow struct {
	row *sql.Row
	err error
}

func (r *MySQLQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(dest...)
}

// MySQLBackendFactory creates MySQL backends
type MySQLBackendFactory struct{}

func NewMySQLBackendFactory() BackendFactory {
	return &MySQLBackendFactory{}
}

func (f *MySQLBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectMySQL {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}
	
	return NewMySQLBackend(config)
}

func (f *MySQLBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectMySQL}
}

func (f *MySQLBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectMySQL {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register MySQL backend
func init() {
	RegisterBackend(DialectMySQL, NewMySQLBackendFactory())
}

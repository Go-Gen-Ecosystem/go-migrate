/**
 * @file sqlserver.go
 * @description SQL Server backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/sqlserver.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	_ "github.com/denisenkom/go-mssqldb"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// SQLServerBackend implements DatabaseBackend for SQL Server
type SQLServerBackend struct {
	db        *sql.DB
	config    types.DatabaseConfig
	connected bool
}

// NewSQLServerBackend creates a new SQL Server backend
func NewSQLServerBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &SQLServerBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to SQL Server
func (s *SQLServerBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if s.connected {
		return nil
	}

	dsn := s.buildDSN(config)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return fmt.Errorf("failed to open SQL Server connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections)
	if config.MaxLifetime > 0 {
		db.SetConnMaxLifetime(config.MaxLifetime)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping SQL Server: %w", err)
	}

	s.db = db
	s.config = config
	s.connected = true

	return nil
}

// Disconnect closes the SQL Server connection
func (s *SQLServerBackend) Disconnect(ctx context.Context) error {
	if !s.connected || s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.connected = false
	s.db = nil

	return err
}

// Ping checks the SQL Server connection
func (s *SQLServerBackend) Ping(ctx context.Context) error {
	if !s.connected || s.db == nil {
		return fmt.Errorf("not connected to SQL Server")
	}

	return s.db.PingContext(ctx)
}

// IsConnected returns connection status
func (s *SQLServerBackend) IsConnected() bool {
	return s.connected && s.db != nil
}

// BeginTransaction starts a new transaction
func (s *SQLServerBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return s.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (s *SQLServerBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !s.connected {
		return nil, fmt.Errorf("not connected to SQL Server")
	}

	txOpts := &sql.TxOptions{
		Isolation: s.mapIsolationLevel(s.reverseMapIsolationLevel(opts.IsolationLevel)),
		ReadOnly:  opts.ReadOnly,
	}

	tx, err := s.db.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin SQL Server transaction: %w", err)
	}

	return &SQLServerTransaction{tx: tx}, nil
}

// Execute executes a SQL statement
func (s *SQLServerBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !s.connected {
		return nil, fmt.Errorf("not connected to SQL Server")
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL Server query: %w", err)
	}

	return &SQLServerQueryResult{result: result}, nil
}

// Query executes a query that returns rows
func (s *SQLServerBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !s.connected {
		return nil, fmt.Errorf("not connected to SQL Server")
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query SQL Server: %w", err)
	}

	return &SQLServerQueryRows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row
func (s *SQLServerBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !s.connected {
		return &SQLServerQueryRow{err: fmt.Errorf("not connected to SQL Server")}
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	return &SQLServerQueryRow{row: row}
}

// CreateSchema creates a new schema
func (s *SQLServerBackend) CreateSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("CREATE SCHEMA [%s]", schemaName)
	_, err := s.Execute(ctx, query)
	return err
}

// DropSchema drops a schema
func (s *SQLServerBackend) DropSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("DROP SCHEMA [%s]", schemaName)
	_, err := s.Execute(ctx, query)
	return err
}

// SchemaExists checks if a schema exists
func (s *SQLServerBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	query := "SELECT COUNT(*) FROM sys.schemas WHERE name = ?"
	row := s.QueryRow(ctx, query, schemaName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListSchemas returns all schemas
func (s *SQLServerBackend) ListSchemas(ctx context.Context) ([]string, error) {
	query := "SELECT name FROM sys.schemas WHERE schema_id < 16384 ORDER BY name"
	rows, err := s.Query(ctx, query)
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
func (s *SQLServerBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	query := s.buildCreateTableSQL(tableDef)
	_, err := s.Execute(ctx, query)
	return err
}

// DropTable drops a table
func (s *SQLServerBackend) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS [%s]", tableName)
	_, err := s.Execute(ctx, query)
	return err
}

// TableExists checks if a table exists
func (s *SQLServerBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := "SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = ?"
	row := s.QueryRow(ctx, query, tableName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListTables returns all tables
func (s *SQLServerBackend) ListTables(ctx context.Context) ([]string, error) {
	query := "SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME"
	rows, err := s.Query(ctx, query)
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
func (s *SQLServerBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name: tableName,
		Type: "table",
	}

	// Get schema
	schemaQuery := "SELECT TABLE_SCHEMA FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = ?"
	row := s.QueryRow(ctx, schemaQuery, tableName)
	var schema string
	if err := row.Scan(&schema); err == nil {
		info.Schema = schema
	}

	// Get columns
	columns, err := s.getTableColumns(ctx, tableName)
	if err != nil {
		return info, err
	}
	info.Columns = columns

	return info, nil
}

// CreateIndex creates an index
func (s *SQLServerBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	query := s.buildCreateIndexSQL(indexDef)
	_, err := s.Execute(ctx, query)
	return err
}

// DropIndex drops an index
func (s *SQLServerBackend) DropIndex(ctx context.Context, indexName string) error {
	// SQL Server requires table name for dropping indexes
	query := fmt.Sprintf("DROP INDEX IF EXISTS [%s] ON [%s]", indexName, indexName)
	_, err := s.Execute(ctx, query)
	return err
}

// IndexExists checks if an index exists
func (s *SQLServerBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	query := "SELECT COUNT(*) FROM sys.indexes WHERE name = ?"
	row := s.QueryRow(ctx, query, indexName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListIndexes returns all indexes for a table
func (s *SQLServerBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	query := `
		SELECT i.name, i.is_unique
		FROM sys.indexes i
		INNER JOIN sys.tables t ON i.object_id = t.object_id
		WHERE t.name = ? AND i.name IS NOT NULL
		ORDER BY i.name`

	rows, err := s.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var name string
		var unique bool
		if err := rows.Scan(&name, &unique); err != nil {
			return nil, err
		}

		indexes = append(indexes, IndexInfo{
			Name:   name,
			Table:  tableName,
			Unique: unique,
		})
	}

	return indexes, rows.Err()
}

// AddConstraint adds a constraint
func (s *SQLServerBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	query := s.buildAddConstraintSQL(constraintDef)
	_, err := s.Execute(ctx, query)
	return err
}

// DropConstraint drops a constraint
func (s *SQLServerBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	query := fmt.Sprintf("ALTER TABLE [%s] DROP CONSTRAINT [%s]", tableName, constraintName)
	_, err := s.Execute(ctx, query)
	return err
}

// ListConstraints returns all constraints for a table
func (s *SQLServerBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	query := `
		SELECT 
			tc.CONSTRAINT_NAME,
			tc.CONSTRAINT_TYPE,
			kcu.COLUMN_NAME,
			rc.REFERENCED_TABLE_NAME,
			rc.REFERENCED_COLUMN_NAME
		FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu 
			ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
		LEFT JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc 
			ON tc.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		WHERE tc.TABLE_NAME = ?
		ORDER BY tc.CONSTRAINT_NAME`

	rows, err := s.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var constraints []ConstraintInfo
	for rows.Next() {
		var constraint ConstraintInfo
		var columnName string
		var referencedTable, referencedColumn sql.NullString

		err := rows.Scan(&constraint.Name, &constraint.Type, &columnName,
			&referencedTable, &referencedColumn)
		if err != nil {
			return nil, err
		}

		constraint.Table = tableName
		constraint.Columns = []string{columnName}
		if referencedTable.Valid {
			constraint.ReferencedTable = referencedTable.String
		}
		if referencedColumn.Valid {
			constraint.ReferencedColumns = []string{referencedColumn.String}
		}

		constraints = append(constraints, constraint)
	}

	return constraints, rows.Err()
}

// GetDatabaseInfo returns database information
func (s *SQLServerBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := s.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	// Get database name
	nameQuery := "SELECT DB_NAME()"
	row := s.QueryRow(ctx, nameQuery)
	var name string
	if err := row.Scan(&name); err != nil {
		name = s.config.Database
	}

	// Get database size
	sizeQuery := `
		SELECT SUM(CAST(FILEPROPERTY(name, 'SpaceUsed') AS bigint) * 8192) 
		FROM sys.database_files`
	row = s.QueryRow(ctx, sizeQuery)
	var size int64
	if err := row.Scan(&size); err != nil {
		size = 0
	}

	return DatabaseInfo{
		Name:    name,
		Version: version,
		Dialect: DialectSQLServer,
		Size:    size,
	}, nil
}

// GetVersion returns SQL Server version
func (s *SQLServerBackend) GetVersion(ctx context.Context) (string, error) {
	row := s.QueryRow(ctx, "SELECT @@VERSION")
	var version string
	err := row.Scan(&version)
	return version, err
}

// GetDialect returns the database dialect
func (s *SQLServerBackend) GetDialect() DatabaseDialect {
	return DialectSQLServer
}

// AdaptSQL adapts SQL for SQL Server
func (s *SQLServerBackend) AdaptSQL(sql string) string {
	// SQL Server-specific adaptations
	sql = strings.ReplaceAll(sql, "`", "[")
	sql = strings.ReplaceAll(sql, "`", "]")
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "IDENTITY(1,1)")
	sql = strings.ReplaceAll(sql, "NOW()", "GETDATE()")
	sql = strings.ReplaceAll(sql, "CURRENT_TIMESTAMP", "GETDATE()")

	// Handle LIMIT syntax
	re := regexp.MustCompile(`LIMIT (\d+)`)
	sql = re.ReplaceAllString(sql, "TOP $1")

	// Handle LIMIT with OFFSET
	re = regexp.MustCompile(`LIMIT (\d+) OFFSET (\d+)`)
	sql = re.ReplaceAllString(sql, "OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY")

	// Handle FETCH FIRST syntax
	re = regexp.MustCompile(`FETCH FIRST (\d+) ROWS ONLY`)
	sql = re.ReplaceAllString(sql, "TOP $1")

	return sql
}

// ValidateSQL validates SQL for SQL Server
func (s *SQLServerBackend) ValidateSQL(sql string) error {
	// Basic SQL validation for SQL Server
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"TRUNCATE TABLE",
		"DELETE FROM .* WHERE 1=1",
		"SHUTDOWN",
		"xp_cmdshell",
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous SQL operation detected: %s", pattern)
		}
	}

	return nil
}

// CreateMigrationTable creates the migration tracking table
func (s *SQLServerBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = '%s')
		CREATE TABLE [%s] (
			id BIGINT IDENTITY(1,1) PRIMARY KEY,
			version NVARCHAR(255) NOT NULL UNIQUE,
			description NVARCHAR(1000),
			type NVARCHAR(50) NOT NULL DEFAULT 'migration',
			installed_by NVARCHAR(255),
			installed_on DATETIME2 DEFAULT GETDATE(),
			execution_time BIGINT,
			success BIT DEFAULT 1,
			checksum NVARCHAR(255)
		)`, tableName, tableName)

	_, err := s.Execute(ctx, query)
	return err
}

// CreateLockTable creates the migration lock table
func (s *SQLServerBackend) CreateLockTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		IF NOT EXISTS (SELECT * FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = '%s')
		CREATE TABLE [%s] (
			id BIGINT IDENTITY(1,1) PRIMARY KEY,
			lock_key NVARCHAR(255) NOT NULL UNIQUE,
			locked_by NVARCHAR(255),
			locked_at DATETIME2 DEFAULT GETDATE(),
			expires_at DATETIME2
		)`, tableName, tableName)

	_, err := s.Execute(ctx, query)
	return err
}

// CreateBackup creates a database backup
func (s *SQLServerBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	if backupConfig.Path == "" {
		return fmt.Errorf("backup path is required")
	}

	query := fmt.Sprintf("BACKUP DATABASE [%s] TO DISK = '%s'",
		s.config.Database, backupConfig.Path)

	if backupConfig.Compression {
		query += " WITH COMPRESSION"
	}

	_, err := s.Execute(ctx, query)
	return err
}

// RestoreBackup restores a database backup
func (s *SQLServerBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	if restoreConfig.Path == "" {
		return fmt.Errorf("restore path is required")
	}

	query := fmt.Sprintf("RESTORE DATABASE [%s] FROM DISK = '%s'",
		s.config.Database, restoreConfig.Path)

	if restoreConfig.DropExisting {
		query += " WITH REPLACE"
	}

	_, err := s.Execute(ctx, query)
	return err
}

// Helper methods

func (s *SQLServerBackend) buildDSN(config types.DatabaseConfig) string {
	var parts []string

	if config.Host != "" {
		if config.Port > 0 {
			parts = append(parts, fmt.Sprintf("server=%s,%d", config.Host, config.Port))
		} else {
			parts = append(parts, fmt.Sprintf("server=%s", config.Host))
		}
	}

	if config.Database != "" {
		parts = append(parts, fmt.Sprintf("database=%s", config.Database))
	}

	if config.Username != "" {
		parts = append(parts, fmt.Sprintf("user id=%s", config.Username))
	}

	if config.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", config.Password))
	}

	// Add connection timeout
	if config.ConnectionTimeout > 0 {
		parts = append(parts, fmt.Sprintf("connection timeout=%d", int(config.ConnectionTimeout.Seconds())))
	}

	// Add encryption settings
	parts = append(parts, "encrypt=true")
	parts = append(parts, "trustservercertificate=true")

	return strings.Join(parts, ";")
}

func (s *SQLServerBackend) mapIsolationLevel(level IsolationLevel) sql.IsolationLevel {
	switch level {
	case IsolationReadUncommitted:
		return sql.LevelReadUncommitted
	case IsolationReadCommitted:
		return sql.LevelReadCommitted
	case IsolationRepeatableRead:
		return sql.LevelRepeatableRead
	case IsolationSerializable:
		return sql.LevelSerializable
	default:
		return sql.LevelDefault
	}
}

func (s *SQLServerBackend) reverseMapIsolationLevel(level sql.IsolationLevel) IsolationLevel {
	switch level {
	case sql.LevelReadUncommitted:
		return IsolationReadUncommitted
	case sql.LevelReadCommitted:
		return IsolationReadCommitted
	case sql.LevelRepeatableRead:
		return IsolationRepeatableRead
	case sql.LevelSerializable:
		return IsolationSerializable
	default:
		return IsolationReadCommitted
	}
}

func (s *SQLServerBackend) buildCreateTableSQL(tableDef TableDefinition) string {
	var parts []string

	// Build columns
	var columns []string
	var constraints []string

	for _, col := range tableDef.Columns {
		columns = append(columns, s.buildColumnSQL(col))
	}

	// Add table constraints
	for _, constraint := range tableDef.Constraints {
		constraints = append(constraints, s.buildConstraintSQL(constraint))
	}

	allParts := append(columns, constraints...)
	parts = append(parts, fmt.Sprintf("CREATE TABLE [%s] (%s)",
		tableDef.Name, strings.Join(allParts, ", ")))

	return strings.Join(parts, " ")
}

func (s *SQLServerBackend) buildColumnSQL(col ColumnDefinition) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("[%s]", col.Name))
	parts = append(parts, col.Type)

	if col.AutoIncrement {
		parts = append(parts, "IDENTITY(1,1)")
	}

	if col.Primary {
		parts = append(parts, "PRIMARY KEY")
	}

	if !col.Nullable && !col.Primary {
		parts = append(parts, "NOT NULL")
	}

	if col.Unique && !col.Primary {
		parts = append(parts, "UNIQUE")
	}

	if col.DefaultValue != nil {
		parts = append(parts, fmt.Sprintf("DEFAULT %v", col.DefaultValue))
	}

	return strings.Join(parts, " ")
}

func (s *SQLServerBackend) buildConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", fmt.Sprintf("[%s]", constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = fmt.Sprintf("[%s]", col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = fmt.Sprintf("[%s]", col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = fmt.Sprintf("[%s]", col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES [%s] (%s)",
			strings.Join(columns, ", "),
			constraint.ReferencedTable,
			strings.Join(refColumns, ", ")))

		if constraint.OnUpdate != "" {
			parts = append(parts, "ON UPDATE", string(constraint.OnUpdate))
		}
		if constraint.OnDelete != "" {
			parts = append(parts, "ON DELETE", string(constraint.OnDelete))
		}

	case ConstraintUnique:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = fmt.Sprintf("[%s]", col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (s *SQLServerBackend) buildCreateIndexSQL(indexDef IndexDefinition) string {
	var parts []string

	if indexDef.Unique {
		parts = append(parts, "CREATE UNIQUE INDEX")
	} else {
		parts = append(parts, "CREATE INDEX")
	}

	parts = append(parts, fmt.Sprintf("[%s]", indexDef.Name))
	parts = append(parts, "ON")
	parts = append(parts, fmt.Sprintf("[%s]", indexDef.Table))

	columns := make([]string, len(indexDef.Columns))
	for i, col := range indexDef.Columns {
		columns[i] = fmt.Sprintf("[%s]", col)
	}
	parts = append(parts, fmt.Sprintf("(%s)", strings.Join(columns, ", ")))

	if indexDef.Where != "" {
		parts = append(parts, "WHERE", indexDef.Where)
	}

	return strings.Join(parts, " ")
}

func (s *SQLServerBackend) buildAddConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	parts = append(parts, "ALTER TABLE", fmt.Sprintf("[%s]", constraint.Table))
	parts = append(parts, "ADD")

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", fmt.Sprintf("[%s]", constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = fmt.Sprintf("[%s]", col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = fmt.Sprintf("[%s]", col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = fmt.Sprintf("[%s]", col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES [%s] (%s)",
			strings.Join(columns, ", "),
			constraint.ReferencedTable,
			strings.Join(refColumns, ", ")))

		if constraint.OnUpdate != "" {
			parts = append(parts, "ON UPDATE", string(constraint.OnUpdate))
		}
		if constraint.OnDelete != "" {
			parts = append(parts, "ON DELETE", string(constraint.OnDelete))
		}

	case ConstraintUnique:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = fmt.Sprintf("[%s]", col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (s *SQLServerBackend) getTableColumns(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT 
			COLUMN_NAME,
			DATA_TYPE,
			IS_NULLABLE,
			COLUMN_DEFAULT,
			ORDINAL_POSITION
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`

	rows, err := s.Query(ctx, query, tableName)
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

// SQL Server-specific implementations

// SQLServerTransaction implements Transaction for SQL Server
type SQLServerTransaction struct {
	tx *sql.Tx
}

func (t *SQLServerTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &SQLServerQueryResult{result: result}, nil
}

func (t *SQLServerTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &SQLServerQueryRows{rows: rows}, nil
}

func (t *SQLServerTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &SQLServerQueryRow{row: row}
}

func (t *SQLServerTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *SQLServerTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *SQLServerTransaction) Savepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("SAVE TRANSACTION %s", name))
	return err
}

func (t *SQLServerTransaction) RollbackToSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("ROLLBACK TRANSACTION %s", name))
	return err
}

func (t *SQLServerTransaction) ReleaseSavepoint(name string) error {
	// SQL Server doesn't have explicit savepoint release
	return nil
}

// SQLServerQueryResult implements QueryResult for SQL Server
type SQLServerQueryResult struct {
	result sql.Result
}

func (r *SQLServerQueryResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

func (r *SQLServerQueryResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

// SQLServerQueryRows implements QueryRows for SQL Server
type SQLServerQueryRows struct {
	rows *sql.Rows
}

func (r *SQLServerQueryRows) Next() bool {
	return r.rows.Next()
}

func (r *SQLServerQueryRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *SQLServerQueryRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *SQLServerQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r *SQLServerQueryRows) Close() error {
	return r.rows.Close()
}

func (r *SQLServerQueryRows) Err() error {
	return r.rows.Err()
}

// SQLServerQueryRow implements QueryRow for SQL Server
type SQLServerQueryRow struct {
	row *sql.Row
	err error
}

func (r *SQLServerQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(dest...)
}

// SQLServerBackendFactory creates SQL Server backends
type SQLServerBackendFactory struct{}

func NewSQLServerBackendFactory() BackendFactory {
	return &SQLServerBackendFactory{}
}

func (f *SQLServerBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectSQLServer {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return NewSQLServerBackend(config)
}

func (f *SQLServerBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectSQLServer}
}

func (f *SQLServerBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectSQLServer {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register SQL Server backend
func init() {
	RegisterBackend(DialectSQLServer, NewSQLServerBackendFactory())
}

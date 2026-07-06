/**
 * @file oracle.go
 * @description Oracle backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/oracle.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	_ "github.com/godror/godror"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// OracleBackend implements DatabaseBackend for Oracle
type OracleBackend struct {
	db        *sql.DB
	config    types.DatabaseConfig
	connected bool
}

// NewOracleBackend creates a new Oracle backend
func NewOracleBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &OracleBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to Oracle
func (o *OracleBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if o.connected {
		return nil
	}

	dsn := o.buildDSN(config)
	db, err := sql.Open("godror", dsn)
	if err != nil {
		return fmt.Errorf("failed to open Oracle connection: %w", err)
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
		return fmt.Errorf("failed to ping Oracle: %w", err)
	}

	o.db = db
	o.config = config
	o.connected = true

	return nil
}

// Disconnect closes the Oracle connection
func (o *OracleBackend) Disconnect(ctx context.Context) error {
	if !o.connected || o.db == nil {
		return nil
	}

	err := o.db.Close()
	o.connected = false
	o.db = nil

	return err
}

// Ping checks the Oracle connection
func (o *OracleBackend) Ping(ctx context.Context) error {
	if !o.connected || o.db == nil {
		return fmt.Errorf("not connected to Oracle")
	}

	return o.db.PingContext(ctx)
}

// IsConnected returns connection status
func (o *OracleBackend) IsConnected() bool {
	return o.connected && o.db != nil
}

// BeginTransaction starts a new transaction
func (o *OracleBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return o.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (o *OracleBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !o.connected {
		return nil, fmt.Errorf("not connected to Oracle")
	}

	txOpts := &sql.TxOptions{
		Isolation: o.mapIsolationLevel(o.reverseMapIsolationLevel(opts.IsolationLevel)),
		ReadOnly:  opts.ReadOnly,
	}

	tx, err := o.db.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin Oracle transaction: %w", err)
	}

	return &OracleTransaction{tx: tx}, nil
}

// Execute executes a SQL statement
func (o *OracleBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !o.connected {
		return nil, fmt.Errorf("not connected to Oracle")
	}

	result, err := o.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Oracle query: %w", err)
	}

	return &OracleQueryResult{result: result}, nil
}

// Query executes a query that returns rows
func (o *OracleBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !o.connected {
		return nil, fmt.Errorf("not connected to Oracle")
	}

	rows, err := o.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query Oracle: %w", err)
	}

	return &OracleQueryRows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row
func (o *OracleBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !o.connected {
		return &OracleQueryRow{err: fmt.Errorf("not connected to Oracle")}
	}

	row := o.db.QueryRowContext(ctx, query, args...)
	return &OracleQueryRow{row: row}
}

// CreateSchema creates a new schema (user in Oracle)
func (o *OracleBackend) CreateSchema(ctx context.Context, schemaName string) error {
	// In Oracle, schemas are users
	query := fmt.Sprintf("CREATE USER %s IDENTIFIED BY temp_password", o.quoteIdentifier(schemaName))
	_, err := o.Execute(ctx, query)
	return err
}

// DropSchema drops a schema (user in Oracle)
func (o *OracleBackend) DropSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("DROP USER %s CASCADE", o.quoteIdentifier(schemaName))
	_, err := o.Execute(ctx, query)
	return err
}

// SchemaExists checks if a schema (user) exists
func (o *OracleBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	query := "SELECT COUNT(*) FROM ALL_USERS WHERE USERNAME = UPPER(?)"
	row := o.QueryRow(ctx, query, schemaName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListSchemas returns all schemas (users)
func (o *OracleBackend) ListSchemas(ctx context.Context) ([]string, error) {
	query := "SELECT USERNAME FROM ALL_USERS WHERE USERNAME NOT IN ('SYS', 'SYSTEM', 'DBSNMP', 'SYSMAN', 'OUTLN') ORDER BY USERNAME"
	rows, err := o.Query(ctx, query)
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
func (o *OracleBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	query := o.buildCreateTableSQL(tableDef)
	_, err := o.Execute(ctx, query)
	return err
}

// DropTable drops a table
func (o *OracleBackend) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE %s", o.quoteIdentifier(tableName))
	_, err := o.Execute(ctx, query)
	return err
}

// TableExists checks if a table exists
func (o *OracleBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := "SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = UPPER(?)"
	row := o.QueryRow(ctx, query, tableName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListTables returns all tables
func (o *OracleBackend) ListTables(ctx context.Context) ([]string, error) {
	query := "SELECT TABLE_NAME FROM USER_TABLES ORDER BY TABLE_NAME"
	rows, err := o.Query(ctx, query)
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
func (o *OracleBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name: tableName,
		Type: "table",
	}

	// Get schema (owner)
	schemaQuery := "SELECT OWNER FROM ALL_TABLES WHERE TABLE_NAME = UPPER(?)"
	row := o.QueryRow(ctx, schemaQuery, tableName)
	var schema string
	if err := row.Scan(&schema); err == nil {
		info.Schema = schema
	}

	// Get columns
	columns, err := o.getTableColumns(ctx, tableName)
	if err != nil {
		return info, err
	}
	info.Columns = columns

	return info, nil
}

// CreateIndex creates an index
func (o *OracleBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	query := o.buildCreateIndexSQL(indexDef)
	_, err := o.Execute(ctx, query)
	return err
}

// DropIndex drops an index
func (o *OracleBackend) DropIndex(ctx context.Context, indexName string) error {
	query := fmt.Sprintf("DROP INDEX %s", o.quoteIdentifier(indexName))
	_, err := o.Execute(ctx, query)
	return err
}

// IndexExists checks if an index exists
func (o *OracleBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	query := "SELECT COUNT(*) FROM USER_INDEXES WHERE INDEX_NAME = UPPER(?)"
	row := o.QueryRow(ctx, query, indexName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListIndexes returns all indexes for a table
func (o *OracleBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	query := `
		SELECT INDEX_NAME, UNIQUENESS
		FROM USER_INDEXES 
		WHERE TABLE_NAME = UPPER(?)
		ORDER BY INDEX_NAME`

	rows, err := o.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var name, uniqueness string
		if err := rows.Scan(&name, &uniqueness); err != nil {
			return nil, err
		}

		indexes = append(indexes, IndexInfo{
			Name:   name,
			Table:  tableName,
			Unique: uniqueness == "UNIQUE",
		})
	}

	return indexes, rows.Err()
}

// AddConstraint adds a constraint
func (o *OracleBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	query := o.buildAddConstraintSQL(constraintDef)
	_, err := o.Execute(ctx, query)
	return err
}

// DropConstraint drops a constraint
func (o *OracleBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	query := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s",
		o.quoteIdentifier(tableName), o.quoteIdentifier(constraintName))
	_, err := o.Execute(ctx, query)
	return err
}

// ListConstraints returns all constraints for a table
func (o *OracleBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	query := `
		SELECT 
			CONSTRAINT_NAME,
			CONSTRAINT_TYPE,
			'' as COLUMN_NAME,
			R_OWNER,
			'' as REFERENCED_COLUMN
		FROM USER_CONSTRAINTS 
		WHERE TABLE_NAME = UPPER(?)
		ORDER BY CONSTRAINT_NAME`

	rows, err := o.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var constraints []ConstraintInfo
	for rows.Next() {
		var constraint ConstraintInfo
		var constraintType string
		var columnName, referencedTable, referencedColumn string

		err := rows.Scan(&constraint.Name, &constraintType, &columnName,
			&referencedTable, &referencedColumn)
		if err != nil {
			return nil, err
		}

		constraint.Table = tableName
		constraint.Type = o.mapConstraintType(constraintType)
		if columnName != "" {
			constraint.Columns = []string{columnName}
		}
		if referencedTable != "" {
			constraint.ReferencedTable = referencedTable
			constraint.ReferencedColumns = []string{referencedColumn}
		}

		constraints = append(constraints, constraint)
	}

	return constraints, rows.Err()
}

// GetDatabaseInfo returns database information
func (o *OracleBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := o.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	// Get database name
	nameQuery := "SELECT NAME FROM V$DATABASE"
	row := o.QueryRow(ctx, nameQuery)
	var name string
	if err := row.Scan(&name); err != nil {
		name = o.config.Database
	}

	return DatabaseInfo{
		Name:    name,
		Version: version,
		Dialect: DialectOracle,
		Size:    0, // Oracle size calculation is complex
	}, nil
}

// GetVersion returns Oracle version
func (o *OracleBackend) GetVersion(ctx context.Context) (string, error) {
	row := o.QueryRow(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1")
	var version string
	err := row.Scan(&version)
	return version, err
}

// GetDialect returns the database dialect
func (o *OracleBackend) GetDialect() DatabaseDialect {
	return DialectOracle
}

// AdaptSQL adapts SQL for Oracle
func (o *OracleBackend) AdaptSQL(sql string) string {
	// Oracle-specific adaptations
	sql = strings.ReplaceAll(sql, "`", "\"")
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "GENERATED ALWAYS AS IDENTITY")
	sql = strings.ReplaceAll(sql, "NOW()", "SYSDATE")
	sql = strings.ReplaceAll(sql, "CURRENT_TIMESTAMP", "SYSDATE")

	// Handle LIMIT syntax - Oracle uses ROWNUM
	re := regexp.MustCompile(`LIMIT (\d+)`)
	sql = re.ReplaceAllStringFunc(sql, func(match string) string {
		limit := regexp.MustCompile(`\d+`).FindString(match)
		return fmt.Sprintf("AND ROWNUM <= %s", limit)
	})

	// Handle LIMIT with OFFSET - Oracle 12c+ syntax
	re = regexp.MustCompile(`LIMIT (\d+) OFFSET (\d+)`)
	sql = re.ReplaceAllString(sql, "OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY")

	return sql
}

// ValidateSQL validates SQL for Oracle
func (o *OracleBackend) ValidateSQL(sql string) error {
	// Basic SQL validation for Oracle
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"DROP TABLESPACE",
		"DELETE FROM .* WHERE 1=1",
		"SHUTDOWN",
		"STARTUP",
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous SQL operation detected: %s", pattern)
		}
	}

	return nil
}

// CreateMigrationTable creates the migration tracking table
func (o *OracleBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	// Check if table exists first
	exists, err := o.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	query := fmt.Sprintf(`
		CREATE TABLE %s (
			id NUMBER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			version VARCHAR2(255) NOT NULL UNIQUE,
			description VARCHAR2(1000),
			type VARCHAR2(50) DEFAULT 'migration' NOT NULL,
			installed_by VARCHAR2(255),
			installed_on TIMESTAMP DEFAULT SYSDATE,
			execution_time NUMBER,
			success NUMBER(1) DEFAULT 1,
			checksum VARCHAR2(255)
		)`, o.quoteIdentifier(tableName))

	_, err = o.Execute(ctx, query)
	return err
}

// CreateLockTable creates the migration lock table
func (o *OracleBackend) CreateLockTable(ctx context.Context, tableName string) error {
	// Check if table exists first
	exists, err := o.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	query := fmt.Sprintf(`
		CREATE TABLE %s (
			id NUMBER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			lock_key VARCHAR2(255) NOT NULL UNIQUE,
			locked_by VARCHAR2(255),
			locked_at TIMESTAMP DEFAULT SYSDATE,
			expires_at TIMESTAMP
		)`, o.quoteIdentifier(tableName))

	_, err = o.Execute(ctx, query)
	return err
}

// CreateBackup creates a database backup
func (o *OracleBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	if backupConfig.Path == "" {
		return fmt.Errorf("backup path is required")
	}

	// Oracle backup using Data Pump
	query := fmt.Sprintf("CREATE DIRECTORY BACKUP_DIR AS '%s'", backupConfig.Path)
	if _, err := o.Execute(ctx, query); err != nil {
		return err
	}

	// Export schema
	exportQuery := fmt.Sprintf("EXPDP %s/%s DIRECTORY=BACKUP_DIR DUMPFILE=%s.dmp LOGFILE=%s.log",
		o.config.Username, o.config.Password, o.config.Database, o.config.Database)

	_, err := o.Execute(ctx, exportQuery)
	return err
}

// RestoreBackup restores a database backup
func (o *OracleBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	if restoreConfig.Path == "" {
		return fmt.Errorf("restore path is required")
	}

	// Oracle restore using Data Pump
	query := fmt.Sprintf("CREATE DIRECTORY RESTORE_DIR AS '%s'", restoreConfig.Path)
	if _, err := o.Execute(ctx, query); err != nil {
		return err
	}

	// Import schema
	importQuery := fmt.Sprintf("IMPDP %s/%s DIRECTORY=RESTORE_DIR DUMPFILE=%s.dmp LOGFILE=%s_restore.log",
		o.config.Username, o.config.Password, o.config.Database, o.config.Database)

	if restoreConfig.DropExisting {
		importQuery += " TABLE_EXISTS_ACTION=REPLACE"
	}

	_, err := o.Execute(ctx, importQuery)
	return err
}

// Helper methods

func (o *OracleBackend) buildDSN(config types.DatabaseConfig) string {
	// Oracle connection string format
	if config.Host != "" && config.Port > 0 {
		return fmt.Sprintf("%s/%s@%s:%d/%s",
			config.Username, config.Password, config.Host, config.Port, config.Database)
	}

	// TNS format
	return fmt.Sprintf("%s/%s@%s", config.Username, config.Password, config.Database)
}

func (o *OracleBackend) mapIsolationLevel(level IsolationLevel) sql.IsolationLevel {
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

func (o *OracleBackend) reverseMapIsolationLevel(level sql.IsolationLevel) IsolationLevel {
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

func (o *OracleBackend) mapConstraintType(dbType string) ConstraintType {
	switch dbType {
	case "P":
		return ConstraintPrimaryKey
	case "R":
		return ConstraintForeignKey
	case "U":
		return ConstraintUnique
	case "C":
		return ConstraintCheck
	default:
		return ConstraintType(dbType)
	}
}

func (o *OracleBackend) quoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

func (o *OracleBackend) buildCreateTableSQL(tableDef TableDefinition) string {
	var parts []string

	// Build columns
	var columns []string
	var constraints []string

	for _, col := range tableDef.Columns {
		columns = append(columns, o.buildColumnSQL(col))
	}

	// Add table constraints
	for _, constraint := range tableDef.Constraints {
		constraints = append(constraints, o.buildConstraintSQL(constraint))
	}

	allParts := append(columns, constraints...)
	parts = append(parts, fmt.Sprintf("CREATE TABLE %s (%s)",
		o.quoteIdentifier(tableDef.Name), strings.Join(allParts, ", ")))

	return strings.Join(parts, " ")
}

func (o *OracleBackend) buildColumnSQL(col ColumnDefinition) string {
	var parts []string

	parts = append(parts, o.quoteIdentifier(col.Name))
	parts = append(parts, col.Type)

	if col.AutoIncrement {
		parts = append(parts, "GENERATED ALWAYS AS IDENTITY")
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

func (o *OracleBackend) buildConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", o.quoteIdentifier(constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = o.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = o.quoteIdentifier(col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = o.quoteIdentifier(col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			o.quoteIdentifier(constraint.ReferencedTable),
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
			columns[i] = o.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (o *OracleBackend) buildCreateIndexSQL(indexDef IndexDefinition) string {
	var parts []string

	if indexDef.Unique {
		parts = append(parts, "CREATE UNIQUE INDEX")
	} else {
		parts = append(parts, "CREATE INDEX")
	}

	parts = append(parts, o.quoteIdentifier(indexDef.Name))
	parts = append(parts, "ON")
	parts = append(parts, o.quoteIdentifier(indexDef.Table))

	columns := make([]string, len(indexDef.Columns))
	for i, col := range indexDef.Columns {
		columns[i] = o.quoteIdentifier(col)
	}
	parts = append(parts, fmt.Sprintf("(%s)", strings.Join(columns, ", ")))

	return strings.Join(parts, " ")
}

func (o *OracleBackend) buildAddConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	parts = append(parts, "ALTER TABLE", o.quoteIdentifier(constraint.Table))
	parts = append(parts, "ADD")

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", o.quoteIdentifier(constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = o.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = o.quoteIdentifier(col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = o.quoteIdentifier(col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			o.quoteIdentifier(constraint.ReferencedTable),
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
			columns[i] = o.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (o *OracleBackend) getTableColumns(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT 
			COLUMN_NAME,
			DATA_TYPE,
			NULLABLE,
			DATA_DEFAULT,
			COLUMN_ID
		FROM USER_TAB_COLUMNS 
		WHERE TABLE_NAME = UPPER(?)
		ORDER BY COLUMN_ID`

	rows, err := o.Query(ctx, query, tableName)
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

		col.Nullable = nullable == "Y"

		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}

		columns = append(columns, col)
	}

	return columns, rows.Err()
}

// Oracle-specific implementations

// OracleTransaction implements Transaction for Oracle
type OracleTransaction struct {
	tx *sql.Tx
}

func (t *OracleTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &OracleQueryResult{result: result}, nil
}

func (t *OracleTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &OracleQueryRows{rows: rows}, nil
}

func (t *OracleTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &OracleQueryRow{row: row}
}

func (t *OracleTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *OracleTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *OracleTransaction) Savepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("SAVEPOINT %s", name))
	return err
}

func (t *OracleTransaction) RollbackToSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name))
	return err
}

func (t *OracleTransaction) ReleaseSavepoint(name string) error {
	// Oracle doesn't have explicit RELEASE SAVEPOINT
	return nil
}

// OracleQueryResult implements QueryResult for Oracle
type OracleQueryResult struct {
	result sql.Result
}

func (r *OracleQueryResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

func (r *OracleQueryResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

// OracleQueryRows implements QueryRows for Oracle
type OracleQueryRows struct {
	rows *sql.Rows
}

func (r *OracleQueryRows) Next() bool {
	return r.rows.Next()
}

func (r *OracleQueryRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *OracleQueryRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *OracleQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r *OracleQueryRows) Close() error {
	return r.rows.Close()
}

func (r *OracleQueryRows) Err() error {
	return r.rows.Err()
}

// OracleQueryRow implements QueryRow for Oracle
type OracleQueryRow struct {
	row *sql.Row
	err error
}

func (r *OracleQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(dest...)
}

// OracleBackendFactory creates Oracle backends
type OracleBackendFactory struct{}

func NewOracleBackendFactory() BackendFactory {
	return &OracleBackendFactory{}
}

func (f *OracleBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectOracle {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return NewOracleBackend(config)
}

func (f *OracleBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectOracle}
}

func (f *OracleBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectOracle {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register Oracle backend
func init() {
	RegisterBackend(DialectOracle, NewOracleBackendFactory())
}

/**
 * @file db2.go
 * @description DB2 backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/db2.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	_ "github.com/ibmdb/go_ibm_db"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DB2Backend implements DatabaseBackend for DB2
type DB2Backend struct {
	db        *sql.DB
	config    types.DatabaseConfig
	connected bool
}

// NewDB2Backend creates a new DB2 backend
func NewDB2Backend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &DB2Backend{
		config: config,
	}, nil
}

// Connect establishes a connection to DB2
func (d *DB2Backend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if d.connected {
		return nil
	}

	dsn := d.buildDSN(config)
	db, err := sql.Open("go_ibm_db", dsn)
	if err != nil {
		return fmt.Errorf("failed to open DB2 connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections) // Use MaxConnections as fallback for idle conns
	if config.MaxLifetime > 0 {
		db.SetConnMaxLifetime(config.MaxLifetime)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping DB2: %w", err)
	}

	d.db = db
	d.config = config
	d.connected = true

	return nil
}

// Disconnect closes the DB2 connection
func (d *DB2Backend) Disconnect(ctx context.Context) error {
	if !d.connected || d.db == nil {
		return nil
	}

	err := d.db.Close()
	d.connected = false
	d.db = nil

	return err
}

// Ping checks the DB2 connection
func (d *DB2Backend) Ping(ctx context.Context) error {
	if !d.connected || d.db == nil {
		return fmt.Errorf("not connected to DB2")
	}

	return d.db.PingContext(ctx)
}

// IsConnected returns connection status
func (d *DB2Backend) IsConnected() bool {
	return d.connected && d.db != nil
}

// BeginTransaction starts a new transaction
func (d *DB2Backend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return d.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (d *DB2Backend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !d.connected {
		return nil, fmt.Errorf("not connected to DB2")
	}

	txOpts := &sql.TxOptions{
		Isolation: d.mapIsolationLevel(d.reverseMapIsolationLevel(opts.IsolationLevel)),
		ReadOnly:  opts.ReadOnly,
	}

	tx, err := d.db.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin DB2 transaction: %w", err)
	}

	return &DB2Transaction{tx: tx}, nil
}

// Execute executes a SQL statement
func (d *DB2Backend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !d.connected {
		return nil, fmt.Errorf("not connected to DB2")
	}

	result, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute DB2 query: %w", err)
	}

	return &DB2QueryResult{result: result}, nil
}

// Query executes a query that returns rows
func (d *DB2Backend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !d.connected {
		return nil, fmt.Errorf("not connected to DB2")
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query DB2: %w", err)
	}

	return &DB2QueryRows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row
func (d *DB2Backend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !d.connected {
		return &DB2QueryRow{err: fmt.Errorf("not connected to DB2")}
	}

	row := d.db.QueryRowContext(ctx, query, args...)
	return &DB2QueryRow{row: row}
}

// CreateSchema creates a new schema
func (d *DB2Backend) CreateSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("CREATE SCHEMA %s", d.quoteIdentifier(schemaName))
	_, err := d.Execute(ctx, query)
	return err
}

// DropSchema drops a schema
func (d *DB2Backend) DropSchema(ctx context.Context, schemaName string) error {
	query := fmt.Sprintf("DROP SCHEMA %s RESTRICT", d.quoteIdentifier(schemaName))
	_, err := d.Execute(ctx, query)
	return err
}

// SchemaExists checks if a schema exists
func (d *DB2Backend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	query := "SELECT COUNT(*) FROM SYSCAT.SCHEMATA WHERE SCHEMANAME = UPPER(?)"
	row := d.QueryRow(ctx, query, schemaName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListSchemas returns all schemas
func (d *DB2Backend) ListSchemas(ctx context.Context) ([]string, error) {
	query := "SELECT SCHEMANAME FROM SYSCAT.SCHEMATA WHERE SCHEMANAME NOT LIKE 'SYS%' ORDER BY SCHEMANAME"
	rows, err := d.Query(ctx, query)
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
func (d *DB2Backend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	query := d.buildCreateTableSQL(tableDef)
	_, err := d.Execute(ctx, query)
	return err
}

// DropTable drops a table
func (d *DB2Backend) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE %s", d.quoteIdentifier(tableName))
	_, err := d.Execute(ctx, query)
	return err
}

// TableExists checks if a table exists
func (d *DB2Backend) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := "SELECT COUNT(*) FROM SYSCAT.TABLES WHERE TABNAME = UPPER(?)"
	row := d.QueryRow(ctx, query, tableName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListTables returns all tables
func (d *DB2Backend) ListTables(ctx context.Context) ([]string, error) {
	query := "SELECT TABNAME FROM SYSCAT.TABLES WHERE TYPE = 'T' AND TABSCHEMA NOT LIKE 'SYS%' ORDER BY TABNAME"
	rows, err := d.Query(ctx, query)
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
func (d *DB2Backend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name: tableName,
		Type: "table",
	}

	// Get schema
	schemaQuery := "SELECT TABSCHEMA FROM SYSCAT.TABLES WHERE TABNAME = UPPER(?)"
	row := d.QueryRow(ctx, schemaQuery, tableName)
	var schema string
	if err := row.Scan(&schema); err == nil {
		info.Schema = schema
	}

	// Get columns
	columns, err := d.getTableColumns(ctx, tableName)
	if err != nil {
		return info, err
	}
	info.Columns = columns

	return info, nil
}

// CreateIndex creates an index
func (d *DB2Backend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	query := d.buildCreateIndexSQL(indexDef)
	_, err := d.Execute(ctx, query)
	return err
}

// DropIndex drops an index
func (d *DB2Backend) DropIndex(ctx context.Context, indexName string) error {
	query := fmt.Sprintf("DROP INDEX %s", d.quoteIdentifier(indexName))
	_, err := d.Execute(ctx, query)
	return err
}

// IndexExists checks if an index exists
func (d *DB2Backend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	query := "SELECT COUNT(*) FROM SYSCAT.INDEXES WHERE INDNAME = UPPER(?)"
	row := d.QueryRow(ctx, query, indexName)

	var count int
	err := row.Scan(&count)
	return count > 0, err
}

// ListIndexes returns all indexes for a table
func (d *DB2Backend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	query := `
		SELECT INDNAME, UNIQUERULE
		FROM SYSCAT.INDEXES 
		WHERE TABNAME = UPPER(?)
		ORDER BY INDNAME`

	rows, err := d.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var name, uniqueRule string
		if err := rows.Scan(&name, &uniqueRule); err != nil {
			return nil, err
		}

		indexes = append(indexes, IndexInfo{
			Name:   name,
			Table:  tableName,
			Unique: uniqueRule == "U",
		})
	}

	return indexes, rows.Err()
}

// AddConstraint adds a constraint
func (d *DB2Backend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	query := d.buildAddConstraintSQL(constraintDef)
	_, err := d.Execute(ctx, query)
	return err
}

// DropConstraint drops a constraint
func (d *DB2Backend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	query := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s",
		d.quoteIdentifier(tableName), d.quoteIdentifier(constraintName))
	_, err := d.Execute(ctx, query)
	return err
}

// ListConstraints returns all constraints for a table
func (d *DB2Backend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	query := `
		SELECT 
			CONSTNAME,
			TYPE,
			'' as COLUMN_NAME,
			'' as REFERENCED_TABLE,
			'' as REFERENCED_COLUMN
		FROM SYSCAT.TABCONST 
		WHERE TABNAME = UPPER(?)
		ORDER BY CONSTNAME`

	rows, err := d.Query(ctx, query, tableName)
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
		constraint.Type = d.mapConstraintType(constraintType)
		if columnName != "" {
			constraint.Columns = []string{columnName}
		}
		if referencedTable != "" {
			constraint.ReferencedTable = referencedTable
		}
		if referencedColumn != "" {
			constraint.ReferencedColumns = []string{referencedColumn}
		}

		constraints = append(constraints, constraint)
	}

	return constraints, rows.Err()
}

// GetDatabaseInfo returns database information
func (d *DB2Backend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := d.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	// Get database name
	nameQuery := "SELECT CURRENT SERVER FROM SYSIBM.SYSDUMMY1"
	row := d.QueryRow(ctx, nameQuery)
	var name string
	if err := row.Scan(&name); err != nil {
		name = d.config.Database
	}

	return DatabaseInfo{
		Name:    name,
		Version: version,
		Dialect: DialectDB2,
		Size:    0, // DB2 size calculation is complex
	}, nil
}

// GetVersion returns DB2 version
func (d *DB2Backend) GetVersion(ctx context.Context) (string, error) {
	row := d.QueryRow(ctx, "SELECT SERVICE_LEVEL FROM SYSIBMADM.ENV_INST_INFO")
	var version string
	err := row.Scan(&version)
	return version, err
}

// GetDialect returns the database dialect
func (d *DB2Backend) GetDialect() DatabaseDialect {
	return DialectDB2
}

// AdaptSQL adapts SQL for DB2
func (d *DB2Backend) AdaptSQL(sql string) string {
	// DB2-specific adaptations
	sql = strings.ReplaceAll(sql, "`", "\"")
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "GENERATED ALWAYS AS IDENTITY")
	sql = strings.ReplaceAll(sql, "NOW()", "CURRENT TIMESTAMP")
	sql = strings.ReplaceAll(sql, "CURRENT_TIMESTAMP", "CURRENT TIMESTAMP")

	// Handle LIMIT syntax
	re := regexp.MustCompile(`LIMIT (\d+)`)
	sql = re.ReplaceAllString(sql, "FETCH FIRST $1 ROWS ONLY")

	// Handle LIMIT with OFFSET
	re = regexp.MustCompile(`LIMIT (\d+) OFFSET (\d+)`)
	sql = re.ReplaceAllString(sql, "OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY")

	return sql
}

// ValidateSQL validates SQL for DB2
func (d *DB2Backend) ValidateSQL(sql string) error {
	// Basic SQL validation for DB2
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"DROP TABLESPACE",
		"DELETE FROM .* WHERE 1=1",
		"FORCE APPLICATION",
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous SQL operation detected: %s", pattern)
		}
	}

	return nil
}

// CreateMigrationTable creates the migration tracking table
func (d *DB2Backend) CreateMigrationTable(ctx context.Context, tableName string) error {
	// Check if table exists first
	exists, err := d.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	query := fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			version VARCHAR(255) NOT NULL UNIQUE,
			description VARCHAR(1000),
			type VARCHAR(50) NOT NULL DEFAULT 'migration',
			installed_by VARCHAR(255),
			installed_on TIMESTAMP DEFAULT CURRENT TIMESTAMP,
			execution_time BIGINT,
			success SMALLINT DEFAULT 1,
			checksum VARCHAR(255)
		)`, d.quoteIdentifier(tableName))

	_, err = d.Execute(ctx, query)
	return err
}

// CreateLockTable creates the migration lock table
func (d *DB2Backend) CreateLockTable(ctx context.Context, tableName string) error {
	// Check if table exists first
	exists, err := d.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	query := fmt.Sprintf(`
		CREATE TABLE %s (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			lock_key VARCHAR(255) NOT NULL UNIQUE,
			locked_by VARCHAR(255),
			locked_at TIMESTAMP DEFAULT CURRENT TIMESTAMP,
			expires_at TIMESTAMP
		)`, d.quoteIdentifier(tableName))

	_, err = d.Execute(ctx, query)
	return err
}

// CreateBackup creates a database backup
func (d *DB2Backend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	if backupConfig.Path == "" {
		return fmt.Errorf("backup path is required")
	}

	// DB2 backup requires BACKUP DATABASE command
	query := fmt.Sprintf("BACKUP DATABASE %s TO '%s'",
		d.config.Database, backupConfig.Path)

	if backupConfig.Compression {
		query += " COMPRESS"
	}

	_, err := d.Execute(ctx, query)
	return err
}

// RestoreBackup restores a database backup
func (d *DB2Backend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	if restoreConfig.Path == "" {
		return fmt.Errorf("restore path is required")
	}

	query := fmt.Sprintf("RESTORE DATABASE %s FROM '%s'",
		d.config.Database, restoreConfig.Path)

	if restoreConfig.DropExisting {
		query += " REPLACE EXISTING"
	}

	_, err := d.Execute(ctx, query)
	return err
}

// Helper methods

func (d *DB2Backend) buildDSN(config types.DatabaseConfig) string {
	var parts []string

	if config.Host != "" {
		parts = append(parts, fmt.Sprintf("HOSTNAME=%s", config.Host))
	}

	if config.Port > 0 {
		parts = append(parts, fmt.Sprintf("PORT=%d", config.Port))
	}

	if config.Database != "" {
		parts = append(parts, fmt.Sprintf("DATABASE=%s", config.Database))
	}

	if config.Username != "" {
		parts = append(parts, fmt.Sprintf("UID=%s", config.Username))
	}

	if config.Password != "" {
		parts = append(parts, fmt.Sprintf("PWD=%s", config.Password))
	}

	// Add connection timeout
	if config.ConnectionTimeout > 0 {
		parts = append(parts, fmt.Sprintf("CONNECTTIMEOUT=%d", int(config.ConnectionTimeout.Seconds())))
	}

	// Add protocol
	parts = append(parts, "PROTOCOL=TCPIP")

	return strings.Join(parts, ";")
}

func (d *DB2Backend) mapIsolationLevel(level IsolationLevel) sql.IsolationLevel {
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

func (d *DB2Backend) reverseMapIsolationLevel(level sql.IsolationLevel) IsolationLevel {
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

func (d *DB2Backend) mapConstraintType(dbType string) ConstraintType {
	switch dbType {
	case "P":
		return ConstraintPrimaryKey
	case "F":
		return ConstraintForeignKey
	case "U":
		return ConstraintUnique
	case "C":
		return ConstraintCheck
	default:
		return ConstraintType(dbType)
	}
}

func (d *DB2Backend) quoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

func (d *DB2Backend) buildCreateTableSQL(tableDef TableDefinition) string {
	var parts []string

	// Build columns
	var columns []string
	var constraints []string

	for _, col := range tableDef.Columns {
		columns = append(columns, d.buildColumnSQL(col))
	}

	// Add table constraints
	for _, constraint := range tableDef.Constraints {
		constraints = append(constraints, d.buildConstraintSQL(constraint))
	}

	allParts := append(columns, constraints...)
	parts = append(parts, fmt.Sprintf("CREATE TABLE %s (%s)",
		d.quoteIdentifier(tableDef.Name), strings.Join(allParts, ", ")))

	return strings.Join(parts, " ")
}

func (d *DB2Backend) buildColumnSQL(col ColumnDefinition) string {
	var parts []string

	parts = append(parts, d.quoteIdentifier(col.Name))
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

func (d *DB2Backend) buildConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", d.quoteIdentifier(constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = d.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = d.quoteIdentifier(col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = d.quoteIdentifier(col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			d.quoteIdentifier(constraint.ReferencedTable),
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
			columns[i] = d.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (d *DB2Backend) buildCreateIndexSQL(indexDef IndexDefinition) string {
	var parts []string

	if indexDef.Unique {
		parts = append(parts, "CREATE UNIQUE INDEX")
	} else {
		parts = append(parts, "CREATE INDEX")
	}

	parts = append(parts, d.quoteIdentifier(indexDef.Name))
	parts = append(parts, "ON")
	parts = append(parts, d.quoteIdentifier(indexDef.Table))

	columns := make([]string, len(indexDef.Columns))
	for i, col := range indexDef.Columns {
		columns[i] = d.quoteIdentifier(col)
	}
	parts = append(parts, fmt.Sprintf("(%s)", strings.Join(columns, ", ")))

	return strings.Join(parts, " ")
}

func (d *DB2Backend) buildAddConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	parts = append(parts, "ALTER TABLE", d.quoteIdentifier(constraint.Table))
	parts = append(parts, "ADD")

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", d.quoteIdentifier(constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = d.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = d.quoteIdentifier(col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = d.quoteIdentifier(col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			d.quoteIdentifier(constraint.ReferencedTable),
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
			columns[i] = d.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (d *DB2Backend) getTableColumns(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT 
			COLNAME,
			TYPENAME,
			NULLS,
			DEFAULT,
			COLNO
		FROM SYSCAT.COLUMNS 
		WHERE TABNAME = UPPER(?)
		ORDER BY COLNO`

	rows, err := d.Query(ctx, query, tableName)
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

// DB2-specific implementations

// DB2Transaction implements Transaction for DB2
type DB2Transaction struct {
	tx *sql.Tx
}

func (t *DB2Transaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &DB2QueryResult{result: result}, nil
}

func (t *DB2Transaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &DB2QueryRows{rows: rows}, nil
}

func (t *DB2Transaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &DB2QueryRow{row: row}
}

func (t *DB2Transaction) Commit() error {
	return t.tx.Commit()
}

func (t *DB2Transaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *DB2Transaction) Savepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("SAVEPOINT %s ON ROLLBACK RETAIN CURSORS", name))
	return err
}

func (t *DB2Transaction) RollbackToSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name))
	return err
}

func (t *DB2Transaction) ReleaseSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("RELEASE SAVEPOINT %s", name))
	return err
}

// DB2QueryResult implements QueryResult for DB2
type DB2QueryResult struct {
	result sql.Result
}

func (r *DB2QueryResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

func (r *DB2QueryResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

// DB2QueryRows implements QueryRows for DB2
type DB2QueryRows struct {
	rows *sql.Rows
}

func (r *DB2QueryRows) Next() bool {
	return r.rows.Next()
}

func (r *DB2QueryRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *DB2QueryRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *DB2QueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r *DB2QueryRows) Close() error {
	return r.rows.Close()
}

func (r *DB2QueryRows) Err() error {
	return r.rows.Err()
}

// DB2QueryRow implements QueryRow for DB2
type DB2QueryRow struct {
	row *sql.Row
	err error
}

func (r *DB2QueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(dest...)
}

// DB2BackendFactory creates DB2 backends
type DB2BackendFactory struct{}

func NewDB2BackendFactory() BackendFactory {
	return &DB2BackendFactory{}
}

func (f *DB2BackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectDB2 {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return NewDB2Backend(config)
}

func (f *DB2BackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectDB2}
}

func (f *DB2BackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectDB2 {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register DB2 backend
func init() {
	RegisterBackend(DialectDB2, NewDB2BackendFactory())
}

/**
 * @file sqlite.go
 * @description SQLite backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/sqlite.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// SQLiteBackend implements DatabaseBackend for SQLite
type SQLiteBackend struct {
	db        *sql.DB
	config    types.DatabaseConfig
	connected bool
	dbPath    string
}

// NewSQLiteBackend creates a new SQLite backend
func NewSQLiteBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &SQLiteBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to SQLite
func (s *SQLiteBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if s.connected {
		return nil
	}

	dbPath := s.buildDBPath(config)

	// Ensure directory exists
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create SQLite directory: %w", err)
		}
	}

	dsn := s.buildDSN(dbPath, config)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("failed to open SQLite connection: %w", err)
	}

	// Configure connection pool (SQLite doesn't benefit from many connections)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping SQLite: %w", err)
	}

	// Enable foreign keys and other pragmas
	if err := s.configurePragmas(ctx, db); err != nil {
		db.Close()
		return fmt.Errorf("failed to configure SQLite pragmas: %w", err)
	}

	s.db = db
	s.config = config
	s.dbPath = dbPath
	s.connected = true

	return nil
}

// Disconnect closes the SQLite connection
func (s *SQLiteBackend) Disconnect(ctx context.Context) error {
	if !s.connected || s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.connected = false
	s.db = nil

	return err
}

// Ping checks the SQLite connection
func (s *SQLiteBackend) Ping(ctx context.Context) error {
	if !s.connected || s.db == nil {
		return fmt.Errorf("not connected to SQLite")
	}

	return s.db.PingContext(ctx)
}

// IsConnected returns connection status
func (s *SQLiteBackend) IsConnected() bool {
	return s.connected && s.db != nil
}

// BeginTransaction starts a new transaction
func (s *SQLiteBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return s.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (s *SQLiteBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !s.connected {
		return nil, fmt.Errorf("not connected to SQLite")
	}

	// SQLite supports limited transaction options
	txOpts := &sql.TxOptions{
		ReadOnly: opts.ReadOnly,
	}

	tx, err := s.db.BeginTx(ctx, txOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin SQLite transaction: %w", err)
	}

	return &SQLiteTransaction{tx: tx}, nil
}

// Execute executes a SQL statement
func (s *SQLiteBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !s.connected {
		return nil, fmt.Errorf("not connected to SQLite")
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQLite query: %w", err)
	}

	return &SQLiteQueryResult{result: result}, nil
}

// Query executes a query that returns rows
func (s *SQLiteBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !s.connected {
		return nil, fmt.Errorf("not connected to SQLite")
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query SQLite: %w", err)
	}

	return &SQLiteQueryRows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row
func (s *SQLiteBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !s.connected {
		return &SQLiteQueryRow{err: fmt.Errorf("not connected to SQLite")}
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	return &SQLiteQueryRow{row: row}
}

// CreateSchema creates a new schema (not supported in SQLite)
func (s *SQLiteBackend) CreateSchema(ctx context.Context, schemaName string) error {
	// SQLite doesn't support schemas in the traditional sense
	// We can attach databases instead
	if schemaName == "main" || schemaName == "temp" {
		return nil // These are built-in schemas
	}

	// For simplicity, we'll just return success
	// In a real implementation, you might attach a new database file
	return nil
}

// DropSchema drops a schema (not supported in SQLite)
func (s *SQLiteBackend) DropSchema(ctx context.Context, schemaName string) error {
	// SQLite doesn't support dropping schemas
	return nil
}

// SchemaExists checks if a schema exists
func (s *SQLiteBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	// SQLite has built-in schemas: main, temp
	builtInSchemas := []string{"main", "temp"}
	for _, schema := range builtInSchemas {
		if schema == schemaName {
			return true, nil
		}
	}

	// Check attached databases
	query := "PRAGMA database_list"
	rows, err := s.Query(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return false, err
		}
		if name == schemaName {
			return true, nil
		}
	}

	return false, rows.Err()
}

// ListSchemas returns all schemas
func (s *SQLiteBackend) ListSchemas(ctx context.Context) ([]string, error) {
	query := "PRAGMA database_list"
	rows, err := s.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return nil, err
		}
		schemas = append(schemas, name)
	}

	return schemas, rows.Err()
}

// CreateTable creates a new table
func (s *SQLiteBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	query := s.buildCreateTableSQL(tableDef)
	_, err := s.Execute(ctx, query)
	return err
}

// DropTable drops a table
func (s *SQLiteBackend) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", s.quoteIdentifier(tableName))
	_, err := s.Execute(ctx, query)
	return err
}

// TableExists checks if a table exists
func (s *SQLiteBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name=?"
	row := s.QueryRow(ctx, query, tableName)

	var name string
	err := row.Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ListTables returns all tables
func (s *SQLiteBackend) ListTables(ctx context.Context) ([]string, error) {
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
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
func (s *SQLiteBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name:   tableName,
		Schema: "main",
		Type:   "table",
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
func (s *SQLiteBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	query := s.buildCreateIndexSQL(indexDef)
	_, err := s.Execute(ctx, query)
	return err
}

// DropIndex drops an index
func (s *SQLiteBackend) DropIndex(ctx context.Context, indexName string) error {
	query := fmt.Sprintf("DROP INDEX IF EXISTS %s", s.quoteIdentifier(indexName))
	_, err := s.Execute(ctx, query)
	return err
}

// IndexExists checks if an index exists
func (s *SQLiteBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	query := "SELECT name FROM sqlite_master WHERE type='index' AND name=?"
	row := s.QueryRow(ctx, query, indexName)

	var name string
	err := row.Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// ListIndexes returns all indexes for a table
func (s *SQLiteBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	query := "SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=? AND name NOT LIKE 'sqlite_%'"
	rows, err := s.Query(ctx, query, tableName)
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

// AddConstraint adds a constraint (limited support in SQLite)
func (s *SQLiteBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	// SQLite has limited support for adding constraints after table creation
	// Most constraints must be defined during table creation
	return fmt.Errorf("adding constraints after table creation is not supported in SQLite")
}

// DropConstraint drops a constraint (not supported in SQLite)
func (s *SQLiteBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	// SQLite doesn't support dropping constraints
	return fmt.Errorf("dropping constraints is not supported in SQLite")
}

// ListConstraints returns all constraints for a table
func (s *SQLiteBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	// SQLite constraint information is embedded in table schema
	// This would require parsing the CREATE TABLE statement
	return []ConstraintInfo{}, nil
}

// GetDatabaseInfo returns database information
func (s *SQLiteBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := s.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	// Get file size
	var size int64
	if stat, err := os.Stat(s.dbPath); err == nil {
		size = stat.Size()
	}

	return DatabaseInfo{
		Name:    filepath.Base(s.dbPath),
		Version: version,
		Dialect: DialectSQLite,
		Size:    size,
	}, nil
}

// GetVersion returns SQLite version
func (s *SQLiteBackend) GetVersion(ctx context.Context) (string, error) {
	row := s.QueryRow(ctx, "SELECT sqlite_version()")
	var version string
	err := row.Scan(&version)
	return version, err
}

// GetDialect returns the database dialect
func (s *SQLiteBackend) GetDialect() DatabaseDialect {
	return DialectSQLite
}

// AdaptSQL adapts SQL for SQLite
func (s *SQLiteBackend) AdaptSQL(sql string) string {
	// SQLite-specific adaptations
	sql = strings.ReplaceAll(sql, "`", "\"")
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "AUTOINCREMENT")
	sql = strings.ReplaceAll(sql, "NOW()", "datetime('now')")
	sql = strings.ReplaceAll(sql, "CURRENT_TIMESTAMP", "datetime('now')")

	// Handle LIMIT syntax (SQLite already uses LIMIT)
	re := regexp.MustCompile(`FETCH FIRST (\d+) ROWS ONLY`)
	sql = re.ReplaceAllString(sql, "LIMIT $1")

	return sql
}

// ValidateSQL validates SQL for SQLite
func (s *SQLiteBackend) ValidateSQL(sql string) error {
	// Basic SQL validation for SQLite
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP TABLE",
		"DELETE FROM .* WHERE 1=1",
		"VACUUM",
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous SQL operation detected: %s", pattern)
		}
	}

	return nil
}

// CreateMigrationTable creates the migration tracking table
func (s *SQLiteBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			version TEXT NOT NULL UNIQUE,
			description TEXT,
			type TEXT NOT NULL DEFAULT 'migration',
			installed_by TEXT,
			installed_on DATETIME DEFAULT (datetime('now')),
			execution_time INTEGER,
			success BOOLEAN DEFAULT 1,
			checksum TEXT
		)`, s.quoteIdentifier(tableName))

	_, err := s.Execute(ctx, query)
	return err
}

// CreateLockTable creates the migration lock table
func (s *SQLiteBackend) CreateLockTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			lock_key TEXT NOT NULL UNIQUE,
			locked_by TEXT,
			locked_at DATETIME DEFAULT (datetime('now')),
			expires_at DATETIME
		)`, s.quoteIdentifier(tableName))

	_, err := s.Execute(ctx, query)
	return err
}

// CreateBackup creates a database backup
func (s *SQLiteBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	// For SQLite, backup is typically a file copy
	if backupConfig.Path == "" {
		return fmt.Errorf("backup path is required")
	}

	// Ensure backup directory exists
	if dir := filepath.Dir(backupConfig.Path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create backup directory: %w", err)
		}
	}

	// Use SQLite backup API or file copy
	// For simplicity, we'll use the VACUUM INTO command (SQLite 3.27+)
	query := fmt.Sprintf("VACUUM INTO '%s'", backupConfig.Path)
	_, err := s.Execute(ctx, query)
	return err
}

// RestoreBackup restores a database backup
func (s *SQLiteBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	// For SQLite, restore is typically replacing the current file
	if restoreConfig.Path == "" {
		return fmt.Errorf("restore path is required")
	}

	// Check if backup file exists
	if _, err := os.Stat(restoreConfig.Path); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", restoreConfig.Path)
	}

	// Close current connection
	if err := s.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect before restore: %w", err)
	}

	// Copy backup file to current database path
	if err := s.copyFile(restoreConfig.Path, s.dbPath); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	// Reconnect
	return s.Connect(ctx, s.config)
}

// Helper methods

func (s *SQLiteBackend) buildDBPath(config types.DatabaseConfig) string {
	if config.Database == "" {
		return ":memory:"
	}

	// If it's already an absolute path or relative path with extension, use as-is
	if filepath.IsAbs(config.Database) || strings.Contains(config.Database, ".") {
		return config.Database
	}

	// Otherwise, add .db extension
	return config.Database + ".db"
}

func (s *SQLiteBackend) buildDSN(dbPath string, config types.DatabaseConfig) string {
	var params []string

	// Add common parameters
	params = append(params, "_foreign_keys=on")
	params = append(params, "_journal_mode=WAL")
	params = append(params, "_synchronous=NORMAL")
	params = append(params, "_cache_size=1000")
	params = append(params, "_temp_store=memory")

	if config.ConnectionTimeout > 0 {
		params = append(params, fmt.Sprintf("_timeout=%d", config.ConnectionTimeout.Milliseconds())) // Convert to milliseconds
	}

	if len(params) > 0 {
		return fmt.Sprintf("%s?%s", dbPath, strings.Join(params, "&"))
	}

	return dbPath
}

func (s *SQLiteBackend) configurePragmas(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = 1000",
		"PRAGMA temp_store = memory",
		"PRAGMA mmap_size = 268435456", // 256MB
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("failed to execute pragma %s: %w", pragma, err)
		}
	}

	return nil
}

func (s *SQLiteBackend) quoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

func (s *SQLiteBackend) buildCreateTableSQL(tableDef TableDefinition) string {
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
	parts = append(parts, fmt.Sprintf("CREATE TABLE %s (%s)",
		s.quoteIdentifier(tableDef.Name), strings.Join(allParts, ", ")))

	return strings.Join(parts, " ")
}

func (s *SQLiteBackend) buildColumnSQL(col ColumnDefinition) string {
	var parts []string

	parts = append(parts, s.quoteIdentifier(col.Name))
	parts = append(parts, col.Type)

	if col.Primary {
		parts = append(parts, "PRIMARY KEY")
		if col.AutoIncrement {
			parts = append(parts, "AUTOINCREMENT")
		}
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

func (s *SQLiteBackend) buildConstraintSQL(constraint ConstraintDefinition) string {
	var parts []string

	if constraint.Name != "" {
		parts = append(parts, "CONSTRAINT", s.quoteIdentifier(constraint.Name))
	}

	switch constraint.Type {
	case ConstraintPrimaryKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = s.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(columns, ", ")))

	case ConstraintForeignKey:
		columns := make([]string, len(constraint.Columns))
		for i, col := range constraint.Columns {
			columns[i] = s.quoteIdentifier(col)
		}
		refColumns := make([]string, len(constraint.ReferencedColumns))
		for i, col := range constraint.ReferencedColumns {
			refColumns[i] = s.quoteIdentifier(col)
		}

		parts = append(parts, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(columns, ", "),
			s.quoteIdentifier(constraint.ReferencedTable),
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
			columns[i] = s.quoteIdentifier(col)
		}
		parts = append(parts, fmt.Sprintf("UNIQUE (%s)", strings.Join(columns, ", ")))

	case ConstraintCheck:
		parts = append(parts, fmt.Sprintf("CHECK (%s)", constraint.CheckExpression))
	}

	return strings.Join(parts, " ")
}

func (s *SQLiteBackend) buildCreateIndexSQL(indexDef IndexDefinition) string {
	var parts []string

	if indexDef.Unique {
		parts = append(parts, "CREATE UNIQUE INDEX")
	} else {
		parts = append(parts, "CREATE INDEX")
	}

	parts = append(parts, s.quoteIdentifier(indexDef.Name))
	parts = append(parts, "ON")
	parts = append(parts, s.quoteIdentifier(indexDef.Table))

	columns := make([]string, len(indexDef.Columns))
	for i, col := range indexDef.Columns {
		columns[i] = s.quoteIdentifier(col)
	}
	parts = append(parts, fmt.Sprintf("(%s)", strings.Join(columns, ", ")))

	if indexDef.Where != "" {
		parts = append(parts, "WHERE", indexDef.Where)
	}

	return strings.Join(parts, " ")
}

func (s *SQLiteBackend) getTableColumns(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", s.quoteIdentifier(tableName))
	rows, err := s.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var notNull, pk int
		var defaultValue sql.NullString

		err := rows.Scan(&col.Position, &col.Name, &col.Type, &notNull, &defaultValue, &pk)
		if err != nil {
			return nil, err
		}

		col.Nullable = notNull == 0
		col.Primary = pk == 1

		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}

		columns = append(columns, col)
	}

	return columns, rows.Err()
}

func (s *SQLiteBackend) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}

// SQLite-specific implementations

// SQLiteTransaction implements Transaction for SQLite
type SQLiteTransaction struct {
	tx *sql.Tx
}

func (t *SQLiteTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &SQLiteQueryResult{result: result}, nil
}

func (t *SQLiteTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &SQLiteQueryRows{rows: rows}, nil
}

func (t *SQLiteTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	row := t.tx.QueryRowContext(ctx, query, args...)
	return &SQLiteQueryRow{row: row}
}

func (t *SQLiteTransaction) Commit() error {
	return t.tx.Commit()
}

func (t *SQLiteTransaction) Rollback() error {
	return t.tx.Rollback()
}

func (t *SQLiteTransaction) Savepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("SAVEPOINT %s", name))
	return err
}

func (t *SQLiteTransaction) RollbackToSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", name))
	return err
}

func (t *SQLiteTransaction) ReleaseSavepoint(name string) error {
	_, err := t.tx.Exec(fmt.Sprintf("RELEASE SAVEPOINT %s", name))
	return err
}

// SQLiteQueryResult implements QueryResult for SQLite
type SQLiteQueryResult struct {
	result sql.Result
}

func (r *SQLiteQueryResult) RowsAffected() (int64, error) {
	return r.result.RowsAffected()
}

func (r *SQLiteQueryResult) LastInsertId() (int64, error) {
	return r.result.LastInsertId()
}

// SQLiteQueryRows implements QueryRows for SQLite
type SQLiteQueryRows struct {
	rows *sql.Rows
}

func (r *SQLiteQueryRows) Next() bool {
	return r.rows.Next()
}

func (r *SQLiteQueryRows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

func (r *SQLiteQueryRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *SQLiteQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return r.rows.ColumnTypes()
}

func (r *SQLiteQueryRows) Close() error {
	return r.rows.Close()
}

func (r *SQLiteQueryRows) Err() error {
	return r.rows.Err()
}

// SQLiteQueryRow implements QueryRow for SQLite
type SQLiteQueryRow struct {
	row *sql.Row
	err error
}

func (r *SQLiteQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(dest...)
}

// SQLiteBackendFactory creates SQLite backends
type SQLiteBackendFactory struct{}

func NewSQLiteBackendFactory() BackendFactory {
	return &SQLiteBackendFactory{}
}

func (f *SQLiteBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectSQLite {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return NewSQLiteBackend(config)
}

func (f *SQLiteBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectSQLite}
}

func (f *SQLiteBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectSQLite {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register SQLite backend
func init() {
	RegisterBackend(DialectSQLite, NewSQLiteBackendFactory())
}

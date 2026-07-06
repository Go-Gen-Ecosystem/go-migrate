/**
 * @file cassandra.go
 * @description Cassandra backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/cassandra.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gocql/gocql"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// CassandraBackend implements DatabaseBackend for Cassandra
type CassandraBackend struct {
	session   *gocql.Session
	cluster   *gocql.ClusterConfig
	config    types.DatabaseConfig
	connected bool
}

// NewCassandraBackend creates a new Cassandra backend
func NewCassandraBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &CassandraBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to Cassandra
func (c *CassandraBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if c.connected {
		return nil
	}

	// Create cluster configuration
	cluster := gocql.NewCluster(c.getHosts(config)...)
	cluster.Keyspace = config.Database
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second

	// Configure authentication
	if config.Username != "" && config.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: config.Username,
			Password: config.Password,
		}
	}

	// Configure connection pool
	if config.MaxConnections > 0 {
		cluster.NumConns = config.MaxConnections
	}

	// Create session
	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to connect to Cassandra: %w", err)
	}

	c.session = session
	c.cluster = cluster
	c.config = config
	c.connected = true

	return nil
}

// Disconnect closes the Cassandra connection
func (c *CassandraBackend) Disconnect(ctx context.Context) error {
	if !c.connected || c.session == nil {
		return nil
	}

	c.session.Close()
	c.connected = false
	c.session = nil
	c.cluster = nil

	return nil
}

// Ping checks the Cassandra connection
func (c *CassandraBackend) Ping(ctx context.Context) error {
	if !c.connected || c.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	// Execute a simple query to test connection
	query := c.session.Query("SELECT now() FROM system.local")
	defer query.Release()

	var now gocql.UUID
	if err := query.Scan(&now); err != nil {
		return fmt.Errorf("failed to ping Cassandra: %w", err)
	}

	return nil
}

// IsConnected returns connection status
func (c *CassandraBackend) IsConnected() bool {
	return c.connected && c.session != nil
}

// BeginTransaction starts a new transaction (Cassandra has limited transaction support)
func (c *CassandraBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return c.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (c *CassandraBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !c.connected {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	// Cassandra has limited transaction support (lightweight transactions only)
	// We'll simulate transactions using batches
	batch := c.session.NewBatch(gocql.LoggedBatch)

	return &CassandraTransaction{
		session: c.session,
		batch:   batch,
		ctx:     ctx,
	}, nil
}

// Execute executes a CQL statement
func (c *CassandraBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !c.connected {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	// Adapt SQL to CQL if needed
	cql := c.AdaptSQL(query)

	// Execute the query
	q := c.session.Query(cql, args...)
	defer q.Release()

	err := q.Exec()
	if err != nil {
		return nil, fmt.Errorf("failed to execute CQL: %w", err)
	}

	return &CassandraQueryResult{
		rowsAffected: 1, // Cassandra doesn't provide rows affected count
	}, nil
}

// Query executes a query that returns rows
func (c *CassandraBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !c.connected {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	// Adapt SQL to CQL if needed
	cql := c.AdaptSQL(query)

	// Execute the query
	q := c.session.Query(cql, args...)

	iter := q.Iter()
	return &CassandraQueryRows{
		iter:    iter,
		query:   q,
		columns: iter.Columns(),
	}, nil
}

// QueryRow executes a query that returns a single row
func (c *CassandraBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !c.connected {
		return &CassandraQueryRow{err: fmt.Errorf("not connected to Cassandra")}
	}

	// Adapt SQL to CQL if needed
	cql := c.AdaptSQL(query)

	// Execute the query
	q := c.session.Query(cql, args...)

	return &CassandraQueryRow{
		query: q,
	}
}

// CreateSchema creates a new keyspace
func (c *CassandraBackend) CreateSchema(ctx context.Context, schemaName string) error {
	cql := fmt.Sprintf(`
		CREATE KEYSPACE IF NOT EXISTS %s 
		WITH REPLICATION = {
			'class': 'SimpleStrategy',
			'replication_factor': 1
		}`, schemaName)

	return c.session.Query(cql).Exec()
}

// DropSchema drops a keyspace
func (c *CassandraBackend) DropSchema(ctx context.Context, schemaName string) error {
	cql := fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", schemaName)
	return c.session.Query(cql).Exec()
}

// SchemaExists checks if a keyspace exists
func (c *CassandraBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	cql := "SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = ?"

	var keyspace string
	err := c.session.Query(cql, schemaName).Scan(&keyspace)
	if err != nil {
		if err == gocql.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return keyspace == schemaName, nil
}

// ListSchemas returns all keyspaces
func (c *CassandraBackend) ListSchemas(ctx context.Context) ([]string, error) {
	cql := "SELECT keyspace_name FROM system_schema.keyspaces"

	iter := c.session.Query(cql).Iter()
	defer iter.Close()

	var schemas []string
	var keyspace string

	for iter.Scan(&keyspace) {
		// Skip system keyspaces
		if !strings.HasPrefix(keyspace, "system") {
			schemas = append(schemas, keyspace)
		}
	}

	return schemas, iter.Close()
}

// CreateTable creates a new table
func (c *CassandraBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	cql := c.buildCreateTableCQL(tableDef)
	return c.session.Query(cql).Exec()
}

// DropTable drops a table
func (c *CassandraBackend) DropTable(ctx context.Context, tableName string) error {
	cql := fmt.Sprintf("DROP TABLE IF EXISTS %s", c.quoteIdentifier(tableName))
	return c.session.Query(cql).Exec()
}

// TableExists checks if a table exists
func (c *CassandraBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	cql := "SELECT table_name FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?"

	var table string
	err := c.session.Query(cql, c.config.Database, tableName).Scan(&table)
	if err != nil {
		if err == gocql.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return table == tableName, nil
}

// ListTables returns all tables
func (c *CassandraBackend) ListTables(ctx context.Context) ([]string, error) {
	cql := "SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?"

	iter := c.session.Query(cql, c.config.Database).Iter()
	defer iter.Close()

	var tables []string
	var table string

	for iter.Scan(&table) {
		tables = append(tables, table)
	}

	return tables, iter.Close()
}

// GetTableInfo returns table information
func (c *CassandraBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name:   tableName,
		Type:   "table",
		Schema: c.config.Database,
	}

	// Get columns
	cql := "SELECT column_name, type FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?"
	iter := c.session.Query(cql, c.config.Database, tableName).Iter()
	defer iter.Close()

	var columnName, columnType string
	for iter.Scan(&columnName, &columnType) {
		info.Columns = append(info.Columns, ColumnInfo{
			Name: columnName,
			Type: columnType,
		})
	}

	return info, iter.Close()
}

// CreateIndex creates an index
func (c *CassandraBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	if len(indexDef.Columns) != 1 {
		return fmt.Errorf("Cassandra only supports single-column indexes")
	}

	cql := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		c.quoteIdentifier(indexDef.Name),
		c.quoteIdentifier(indexDef.Table),
		c.quoteIdentifier(indexDef.Columns[0]))

	return c.session.Query(cql).Exec()
}

// DropIndex drops an index
func (c *CassandraBackend) DropIndex(ctx context.Context, indexName string) error {
	cql := fmt.Sprintf("DROP INDEX IF EXISTS %s", c.quoteIdentifier(indexName))
	return c.session.Query(cql).Exec()
}

// IndexExists checks if an index exists
func (c *CassandraBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	cql := "SELECT index_name FROM system_schema.indexes WHERE keyspace_name = ? AND index_name = ?"

	var index string
	err := c.session.Query(cql, c.config.Database, indexName).Scan(&index)
	if err != nil {
		if err == gocql.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return index == indexName, nil
}

// ListIndexes returns all indexes for a table
func (c *CassandraBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	cql := "SELECT index_name, target FROM system_schema.indexes WHERE keyspace_name = ? AND table_name = ?"

	iter := c.session.Query(cql, c.config.Database, tableName).Iter()
	defer iter.Close()

	var indexes []IndexInfo
	var indexName, target string

	for iter.Scan(&indexName, &target) {
		indexes = append(indexes, IndexInfo{
			Name:    indexName,
			Table:   tableName,
			Columns: []string{target},
			Unique:  false, // Cassandra doesn't have unique indexes
		})
	}

	return indexes, iter.Close()
}

// AddConstraint adds a constraint (limited support in Cassandra)
func (c *CassandraBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	// Cassandra has very limited constraint support
	return fmt.Errorf("Cassandra has limited constraint support")
}

// DropConstraint drops a constraint
func (c *CassandraBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	return fmt.Errorf("Cassandra has limited constraint support")
}

// ListConstraints returns all constraints for a table
func (c *CassandraBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	// Cassandra has very limited constraint support
	return []ConstraintInfo{}, nil
}

// GetDatabaseInfo returns database information
func (c *CassandraBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := c.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	return DatabaseInfo{
		Name:    c.config.Database,
		Version: version,
		Dialect: DialectCassandra,
		Size:    0, // Cassandra doesn't provide easy size calculation
	}, nil
}

// GetVersion returns Cassandra version
func (c *CassandraBackend) GetVersion(ctx context.Context) (string, error) {
	cql := "SELECT release_version FROM system.local"

	var version string
	err := c.session.Query(cql).Scan(&version)
	if err != nil {
		return "", err
	}

	return version, nil
}

// GetDialect returns the database dialect
func (c *CassandraBackend) GetDialect() DatabaseDialect {
	return DialectCassandra
}

// AdaptSQL adapts SQL for Cassandra (converts to CQL)
func (c *CassandraBackend) AdaptSQL(sql string) string {
	sql = strings.TrimSpace(sql)

	// Basic SQL to CQL conversions
	sql = regexp.MustCompile(`(?i)AUTO_INCREMENT`).ReplaceAllString(sql, "")
	sql = regexp.MustCompile(`(?i)ENGINE\s*=\s*\w+`).ReplaceAllString(sql, "")
	sql = regexp.MustCompile(`(?i)DEFAULT\s+CHARSET\s*=\s*\w+`).ReplaceAllString(sql, "")

	// Convert data types
	sql = regexp.MustCompile(`(?i)\bINT\b`).ReplaceAllString(sql, "int")
	sql = regexp.MustCompile(`(?i)\bVARCHAR\((\d+)\)`).ReplaceAllString(sql, "text")
	sql = regexp.MustCompile(`(?i)\bTEXT\b`).ReplaceAllString(sql, "text")
	sql = regexp.MustCompile(`(?i)\bTIMESTAMP\b`).ReplaceAllString(sql, "timestamp")
	sql = regexp.MustCompile(`(?i)\bDATETIME\b`).ReplaceAllString(sql, "timestamp")
	sql = regexp.MustCompile(`(?i)\bBOOLEAN\b`).ReplaceAllString(sql, "boolean")

	// Convert functions
	sql = regexp.MustCompile(`(?i)\bNOW\(\)`).ReplaceAllString(sql, "toTimestamp(now())")
	sql = regexp.MustCompile(`(?i)\bCURRENT_TIMESTAMP\b`).ReplaceAllString(sql, "toTimestamp(now())")

	// Remove unsupported clauses
	sql = regexp.MustCompile(`(?i)\bFOREIGN\s+KEY.*?REFERENCES.*?(?:\)|,|$)`).ReplaceAllString(sql, "")
	sql = regexp.MustCompile(`(?i)\bCONSTRAINT\s+\w+\s+.*?(?:\)|,|$)`).ReplaceAllString(sql, "")

	return sql
}

// ValidateSQL validates CQL statements
func (c *CassandraBackend) ValidateSQL(sql string) error {
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP KEYSPACE",
		"TRUNCATE",
		"DELETE.*WITHOUT.*WHERE",
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous CQL operation detected: %s", pattern)
		}
	}

	// Check for unsupported operations
	unsupportedPatterns := []string{
		"JOIN",
		"UNION",
		"SUBQUERY",
		"FOREIGN KEY",
		"CONSTRAINT",
	}

	for _, pattern := range unsupportedPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("unsupported CQL operation: %s", pattern)
		}
	}

	return nil
}

// CreateMigrationTable creates the migration tracking table
func (c *CassandraBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	cql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version text PRIMARY KEY,
			description text,
			type text,
			installed_by text,
			installed_on timestamp,
			execution_time bigint,
			success boolean,
			checksum text
		)`, c.quoteIdentifier(tableName))

	return c.session.Query(cql).Exec()
}

// CreateLockTable creates the migration lock table
func (c *CassandraBackend) CreateLockTable(ctx context.Context, tableName string) error {
	cql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			lock_key text PRIMARY KEY,
			locked_by text,
			locked_at timestamp,
			expires_at timestamp
		)`, c.quoteIdentifier(tableName))

	return c.session.Query(cql).Exec()
}

// CreateBackup creates a database backup
func (c *CassandraBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	// Cassandra backup would typically use nodetool snapshot
	return fmt.Errorf("Cassandra backup requires external tools like nodetool")
}

// RestoreBackup restores a database backup
func (c *CassandraBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	// Cassandra restore would typically use sstableloader
	return fmt.Errorf("Cassandra restore requires external tools like sstableloader")
}

// Helper methods

func (c *CassandraBackend) getHosts(config types.DatabaseConfig) []string {
	if config.Host == "" {
		return []string{"127.0.0.1"}
	}

	// Support multiple hosts separated by comma
	hosts := strings.Split(config.Host, ",")
	for i, host := range hosts {
		hosts[i] = strings.TrimSpace(host)
		if config.Port > 0 && !strings.Contains(hosts[i], ":") {
			hosts[i] = fmt.Sprintf("%s:%d", hosts[i], config.Port)
		}
	}

	return hosts
}

func (c *CassandraBackend) quoteIdentifier(identifier string) string {
	// Cassandra uses double quotes for case-sensitive identifiers
	if strings.Contains(identifier, " ") || strings.ToLower(identifier) != identifier {
		return fmt.Sprintf(`"%s"`, identifier)
	}
	return identifier
}

func (c *CassandraBackend) buildCreateTableCQL(tableDef TableDefinition) string {
	var cql strings.Builder

	cql.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (", c.quoteIdentifier(tableDef.Name)))

	// Add columns
	for i, column := range tableDef.Columns {
		if i > 0 {
			cql.WriteString(", ")
		}
		cql.WriteString(fmt.Sprintf("%s %s", c.quoteIdentifier(column.Name), column.Type))
	}

	// Add primary key (required in Cassandra)
	if len(tableDef.Columns) > 0 {
		cql.WriteString(fmt.Sprintf(", PRIMARY KEY (%s)", c.quoteIdentifier(tableDef.Columns[0].Name)))
	}

	cql.WriteString(")")

	return cql.String()
}

// Cassandra-specific implementations

// CassandraTransaction implements Transaction for Cassandra
type CassandraTransaction struct {
	session *gocql.Session
	batch   *gocql.Batch
	ctx     context.Context
}

func (t *CassandraTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	// Add query to batch
	t.batch.Query(query, args...)

	return &CassandraQueryResult{
		rowsAffected: 1,
	}, nil
}

func (t *CassandraTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	// Cassandra batches don't support queries, only modifications
	return nil, fmt.Errorf("queries not supported in Cassandra transactions")
}

func (t *CassandraTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	return &CassandraQueryRow{err: fmt.Errorf("queries not supported in Cassandra transactions")}
}

func (t *CassandraTransaction) Commit() error {
	return t.session.ExecuteBatch(t.batch)
}

func (t *CassandraTransaction) Rollback() error {
	// Cassandra doesn't support rollback, just clear the batch
	t.batch = t.session.NewBatch(gocql.LoggedBatch)
	return nil
}

func (t *CassandraTransaction) Savepoint(name string) error {
	return fmt.Errorf("Cassandra doesn't support savepoints")
}

func (t *CassandraTransaction) RollbackToSavepoint(name string) error {
	return fmt.Errorf("Cassandra doesn't support savepoints")
}

func (t *CassandraTransaction) ReleaseSavepoint(name string) error {
	return fmt.Errorf("Cassandra doesn't support savepoints")
}

// CassandraQueryResult implements QueryResult for Cassandra
type CassandraQueryResult struct {
	rowsAffected int64
}

func (r *CassandraQueryResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

func (r *CassandraQueryResult) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("Cassandra doesn't support auto-increment IDs")
}

// CassandraQueryRows implements QueryRows for Cassandra
type CassandraQueryRows struct {
	iter    *gocql.Iter
	query   *gocql.Query
	columns []gocql.ColumnInfo
}

func (r *CassandraQueryRows) Next() bool {
	return !r.iter.WillSwitchPage()
}

func (r *CassandraQueryRows) Scan(dest ...interface{}) error {
	if !r.iter.Scan(dest...) {
		if err := r.iter.Close(); err != nil {
			return err
		}
		return fmt.Errorf("scan failed")
	}
	return nil
}

func (r *CassandraQueryRows) Columns() ([]string, error) {
	var names []string
	for _, col := range r.columns {
		names = append(names, col.Name)
	}
	return names, nil
}

func (r *CassandraQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	// Cassandra doesn't provide SQL column types
	return nil, fmt.Errorf("Cassandra doesn't support SQL column types")
}

func (r *CassandraQueryRows) Close() error {
	r.query.Release()
	return r.iter.Close()
}

func (r *CassandraQueryRows) Err() error {
	return r.iter.Close()
}

// CassandraQueryRow implements QueryRow for Cassandra
type CassandraQueryRow struct {
	query *gocql.Query
	err   error
}

func (r *CassandraQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}

	defer r.query.Release()
	return r.query.Scan(dest...)
}

// CassandraBackendFactory creates Cassandra backends
type CassandraBackendFactory struct{}

func NewCassandraBackendFactory() BackendFactory {
	return &CassandraBackendFactory{}
}

func (f *CassandraBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectCassandra {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return NewCassandraBackend(config)
}

func (f *CassandraBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectCassandra}
}

func (f *CassandraBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectCassandra {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register Cassandra backend
func init() {
	RegisterBackend(DialectCassandra, NewCassandraBackendFactory())
}

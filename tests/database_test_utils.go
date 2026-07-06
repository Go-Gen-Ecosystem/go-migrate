/**
 * @file database_test_utils.go
 * @description Database-specific test utilities for migration system testing
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/tests/database_test_utils.go
 */

package tests

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// DatabaseTestHelper provides utilities for database testing
type DatabaseTestHelper struct {
	DB     *sql.DB
	Driver string
	DSN    string
}

// NewDatabaseTestHelper creates a new database test helper
func NewDatabaseTestHelper(driver, dsn string) *DatabaseTestHelper {
	return &DatabaseTestHelper{
		Driver: driver,
		DSN:    dsn,
	}
}

// Connect connects to the database
func (h *DatabaseTestHelper) Connect(t *testing.T) {
	db, err := sql.Open(h.Driver, h.DSN)
	require.NoError(t, err)

	err = db.Ping()
	require.NoError(t, err)

	h.DB = db
}

// Close closes the database connection
func (h *DatabaseTestHelper) Close(t *testing.T) {
	if h.DB != nil {
		err := h.DB.Close()
		require.NoError(t, err)
	}
}

// ExecuteSQL executes SQL statements
func (h *DatabaseTestHelper) ExecuteSQL(t *testing.T, sql string, args ...interface{}) sql.Result {
	result, err := h.DB.Exec(sql, args...)
	require.NoError(t, err)
	return result
}

// QueryRow executes a query that returns a single row
func (h *DatabaseTestHelper) QueryRow(t *testing.T, sql string, args ...interface{}) *sql.Row {
	return h.DB.QueryRow(sql, args...)
}

// Query executes a query that returns multiple rows
func (h *DatabaseTestHelper) Query(t *testing.T, sql string, args ...interface{}) *sql.Rows {
	rows, err := h.DB.Query(sql, args...)
	require.NoError(t, err)
	return rows
}

// TableExists checks if a table exists
func (h *DatabaseTestHelper) TableExists(t *testing.T, tableName string) bool {
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
	err := h.DB.QueryRow(query, tableName).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

// ColumnExists checks if a column exists in a table
func (h *DatabaseTestHelper) ColumnExists(t *testing.T, tableName, columnName string) bool {
	query := fmt.Sprintf("PRAGMA table_info(%s)", tableName)
	rows, err := h.DB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString

		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		require.NoError(t, err)

		if name == columnName {
			return true
		}
	}

	return false
}

// IndexExists checks if an index exists
func (h *DatabaseTestHelper) IndexExists(t *testing.T, indexName string) bool {
	var count int
	query := "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?"
	err := h.DB.QueryRow(query, indexName).Scan(&count)
	require.NoError(t, err)
	return count > 0
}

// GetTableColumns returns the columns of a table
func (h *DatabaseTestHelper) GetTableColumns(t *testing.T, tableName string) []ColumnInfo {
	query := fmt.Sprintf("PRAGMA table_info(%s)", tableName)
	rows, err := h.DB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var defaultValue sql.NullString

		err := rows.Scan(&col.ID, &col.Name, &col.Type, &col.NotNull, &defaultValue, &col.PrimaryKey)
		require.NoError(t, err)

		if defaultValue.Valid {
			col.DefaultValue = &defaultValue.String
		}

		columns = append(columns, col)
	}

	return columns
}

// ColumnInfo represents column information
type ColumnInfo struct {
	ID           int
	Name         string
	Type         string
	NotNull      bool
	DefaultValue *string
	PrimaryKey   bool
}

// GetTableIndexes returns the indexes of a table
func (h *DatabaseTestHelper) GetTableIndexes(t *testing.T, tableName string) []IndexInfo {
	query := fmt.Sprintf("PRAGMA index_list(%s)", tableName)
	rows, err := h.DB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	var indexes []IndexInfo
	for rows.Next() {
		var idx IndexInfo
		var origin string

		err := rows.Scan(&idx.Seq, &idx.Name, &idx.Unique, &origin, &idx.Partial)
		require.NoError(t, err)

		// Get index columns
		idx.Columns = h.getIndexColumns(t, idx.Name)
		indexes = append(indexes, idx)
	}

	return indexes
}

// IndexInfo represents index information
type IndexInfo struct {
	Seq     int
	Name    string
	Unique  bool
	Partial bool
	Columns []string
}

// getIndexColumns gets the columns of an index
func (h *DatabaseTestHelper) getIndexColumns(t *testing.T, indexName string) []string {
	query := fmt.Sprintf("PRAGMA index_info(%s)", indexName)
	rows, err := h.DB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var seqno, cid int
		var name string

		err := rows.Scan(&seqno, &cid, &name)
		require.NoError(t, err)

		columns = append(columns, name)
	}

	return columns
}

// GetForeignKeys returns the foreign keys of a table
func (h *DatabaseTestHelper) GetForeignKeys(t *testing.T, tableName string) []ForeignKeyInfo {
	query := fmt.Sprintf("PRAGMA foreign_key_list(%s)", tableName)
	rows, err := h.DB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	var foreignKeys []ForeignKeyInfo
	for rows.Next() {
		var fk ForeignKeyInfo

		err := rows.Scan(&fk.ID, &fk.Seq, &fk.Table, &fk.From, &fk.To, &fk.OnUpdate, &fk.OnDelete, &fk.Match)
		require.NoError(t, err)

		foreignKeys = append(foreignKeys, fk)
	}

	return foreignKeys
}

// ForeignKeyInfo represents foreign key information
type ForeignKeyInfo struct {
	ID       int
	Seq      int
	Table    string
	From     string
	To       string
	OnUpdate string
	OnDelete string
	Match    string
}

// CountRows counts the number of rows in a table
func (h *DatabaseTestHelper) CountRows(t *testing.T, tableName string) int {
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	err := h.DB.QueryRow(query).Scan(&count)
	require.NoError(t, err)
	return count
}

// TruncateTable truncates a table
func (h *DatabaseTestHelper) TruncateTable(t *testing.T, tableName string) {
	query := fmt.Sprintf("DELETE FROM %s", tableName)
	_, err := h.DB.Exec(query)
	require.NoError(t, err)
}

// DropTable drops a table if it exists
func (h *DatabaseTestHelper) DropTable(t *testing.T, tableName string) {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
	_, err := h.DB.Exec(query)
	require.NoError(t, err)
}

// CreateTestTable creates a test table
func (h *DatabaseTestHelper) CreateTestTable(t *testing.T, tableName string) {
	query := fmt.Sprintf(`
		CREATE TABLE %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name VARCHAR(100) NOT NULL,
			value INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`, tableName)
	_, err := h.DB.Exec(query)
	require.NoError(t, err)
}

// InsertTestData inserts test data into a table
func (h *DatabaseTestHelper) InsertTestData(t *testing.T, tableName string, count int) []int64 {
	var ids []int64

	for i := 0; i < count; i++ {
		query := fmt.Sprintf("INSERT INTO %s (name, value) VALUES (?, ?)", tableName)
		result, err := h.DB.Exec(query, fmt.Sprintf("test_%d", i), i*10)
		require.NoError(t, err)

		id, err := result.LastInsertId()
		require.NoError(t, err)
		ids = append(ids, id)
	}

	return ids
}

// BeginTransaction begins a database transaction
func (h *DatabaseTestHelper) BeginTransaction(t *testing.T) *sql.Tx {
	tx, err := h.DB.Begin()
	require.NoError(t, err)
	return tx
}

// CommitTransaction commits a transaction
func (h *DatabaseTestHelper) CommitTransaction(t *testing.T, tx *sql.Tx) {
	err := tx.Commit()
	require.NoError(t, err)
}

// RollbackTransaction rolls back a transaction
func (h *DatabaseTestHelper) RollbackTransaction(t *testing.T, tx *sql.Tx) {
	err := tx.Rollback()
	require.NoError(t, err)
}

// TestTransactionRollback tests transaction rollback functionality
func (h *DatabaseTestHelper) TestTransactionRollback(t *testing.T, tableName string) {
	// Count initial rows
	initialCount := h.CountRows(t, tableName)

	// Begin transaction
	tx := h.BeginTransaction(t)

	// Insert data in transaction
	query := fmt.Sprintf("INSERT INTO %s (name, value) VALUES (?, ?)", tableName)
	_, err := tx.Exec(query, "rollback_test", 999)
	require.NoError(t, err)

	// Rollback transaction
	h.RollbackTransaction(t, tx)

	// Verify count is unchanged
	finalCount := h.CountRows(t, tableName)
	require.Equal(t, initialCount, finalCount, "Row count should be unchanged after rollback")
}

// TestTransactionCommit tests transaction commit functionality
func (h *DatabaseTestHelper) TestTransactionCommit(t *testing.T, tableName string) {
	// Count initial rows
	initialCount := h.CountRows(t, tableName)

	// Begin transaction
	tx := h.BeginTransaction(t)

	// Insert data in transaction
	query := fmt.Sprintf("INSERT INTO %s (name, value) VALUES (?, ?)", tableName)
	_, err := tx.Exec(query, "commit_test", 888)
	require.NoError(t, err)

	// Commit transaction
	h.CommitTransaction(t, tx)

	// Verify count increased
	finalCount := h.CountRows(t, tableName)
	require.Equal(t, initialCount+1, finalCount, "Row count should increase after commit")
}

// GetDatabaseVersion returns the database version
func (h *DatabaseTestHelper) GetDatabaseVersion(t *testing.T) string {
	var version string
	err := h.DB.QueryRow("SELECT sqlite_version()").Scan(&version)
	require.NoError(t, err)
	return version
}

// GetDatabaseSize returns the database size in bytes
func (h *DatabaseTestHelper) GetDatabaseSize(t *testing.T) int64 {
	var pageCount, pageSize int64

	err := h.DB.QueryRow("PRAGMA page_count").Scan(&pageCount)
	require.NoError(t, err)

	err = h.DB.QueryRow("PRAGMA page_size").Scan(&pageSize)
	require.NoError(t, err)

	return pageCount * pageSize
}

// VacuumDatabase vacuums the database
func (h *DatabaseTestHelper) VacuumDatabase(t *testing.T) {
	_, err := h.DB.Exec("VACUUM")
	require.NoError(t, err)
}

// AnalyzeDatabase analyzes the database
func (h *DatabaseTestHelper) AnalyzeDatabase(t *testing.T) {
	_, err := h.DB.Exec("ANALYZE")
	require.NoError(t, err)
}

// CheckIntegrity checks database integrity
func (h *DatabaseTestHelper) CheckIntegrity(t *testing.T) bool {
	var result string
	err := h.DB.QueryRow("PRAGMA integrity_check").Scan(&result)
	require.NoError(t, err)
	return result == "ok"
}

// EnableForeignKeys enables foreign key constraints
func (h *DatabaseTestHelper) EnableForeignKeys(t *testing.T) {
	_, err := h.DB.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
}

// DisableForeignKeys disables foreign key constraints
func (h *DatabaseTestHelper) DisableForeignKeys(t *testing.T) {
	_, err := h.DB.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
}

// GetForeignKeysStatus returns the foreign keys status
func (h *DatabaseTestHelper) GetForeignKeysStatus(t *testing.T) bool {
	var status int
	err := h.DB.QueryRow("PRAGMA foreign_keys").Scan(&status)
	require.NoError(t, err)
	return status == 1
}

// CreateBackup creates a backup of the database
func (h *DatabaseTestHelper) CreateBackup(t *testing.T, backupPath string) {
	backupDB, err := sql.Open(h.Driver, backupPath)
	require.NoError(t, err)
	defer backupDB.Close()

	// Simple backup by copying all tables
	tables := h.GetAllTables(t)
	for _, table := range tables {
		h.copyTable(t, table, backupDB)
	}
}

// GetAllTables returns all table names
func (h *DatabaseTestHelper) GetAllTables(t *testing.T) []string {
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
	rows, err := h.DB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		err := rows.Scan(&tableName)
		require.NoError(t, err)
		tables = append(tables, tableName)
	}

	return tables
}

// copyTable copies a table to another database
func (h *DatabaseTestHelper) copyTable(t *testing.T, tableName string, destDB *sql.DB) {
	// Get table schema
	var schema string
	query := "SELECT sql FROM sqlite_master WHERE type='table' AND name=?"
	err := h.DB.QueryRow(query, tableName).Scan(&schema)
	require.NoError(t, err)

	// Create table in destination
	_, err = destDB.Exec(schema)
	require.NoError(t, err)

	// Copy data
	rows, err := h.DB.Query(fmt.Sprintf("SELECT * FROM %s", tableName))
	require.NoError(t, err)
	defer rows.Close()

	columns, err := rows.Columns()
	require.NoError(t, err)

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		err := rows.Scan(valuePtrs...)
		require.NoError(t, err)

		placeholders := strings.Repeat("?,", len(columns))
		placeholders = placeholders[:len(placeholders)-1]

		insertQuery := fmt.Sprintf("INSERT INTO %s VALUES (%s)", tableName, placeholders)
		_, err = destDB.Exec(insertQuery, values...)
		require.NoError(t, err)
	}
}

// BenchmarkQuery benchmarks a query
func (h *DatabaseTestHelper) BenchmarkQuery(t *testing.T, name, query string, args ...interface{}) time.Duration {
	start := time.Now()
	rows, err := h.DB.Query(query, args...)
	require.NoError(t, err)

	count := 0
	for rows.Next() {
		count++
	}
	rows.Close()

	duration := time.Since(start)
	t.Logf("Benchmark %s: %v (returned %d rows)", name, duration, count)
	return duration
}

// BenchmarkInsert benchmarks insert operations
func (h *DatabaseTestHelper) BenchmarkInsert(t *testing.T, tableName string, count int) time.Duration {
	start := time.Now()

	tx, err := h.DB.Begin()
	require.NoError(t, err)

	query := fmt.Sprintf("INSERT INTO %s (name, value) VALUES (?, ?)", tableName)
	stmt, err := tx.Prepare(query)
	require.NoError(t, err)

	for i := 0; i < count; i++ {
		_, err := stmt.Exec(fmt.Sprintf("bench_%d", i), i)
		require.NoError(t, err)
	}

	err = stmt.Close()
	require.NoError(t, err)

	err = tx.Commit()
	require.NoError(t, err)

	duration := time.Since(start)
	t.Logf("Benchmark Insert %d rows: %v", count, duration)
	return duration
}

// SimulateConcurrentAccess simulates concurrent database access
func (h *DatabaseTestHelper) SimulateConcurrentAccess(t *testing.T, tableName string, goroutines, operationsPerGoroutine int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < operationsPerGoroutine; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					// Perform random operations
					switch j % 3 {
					case 0:
						// Insert
						query := fmt.Sprintf("INSERT INTO %s (name, value) VALUES (?, ?)", tableName)
						_, err := h.DB.Exec(query, fmt.Sprintf("concurrent_%d_%d", id, j), j)
						if err != nil {
							t.Logf("Concurrent insert error: %v", err)
						}
					case 1:
						// Select
						query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
						var count int
						err := h.DB.QueryRow(query).Scan(&count)
						if err != nil {
							t.Logf("Concurrent select error: %v", err)
						}
					case 2:
						// Update
						query := fmt.Sprintf("UPDATE %s SET value = ? WHERE name LIKE ?", tableName)
						_, err := h.DB.Exec(query, j*100, fmt.Sprintf("concurrent_%d_%%", id))
						if err != nil {
							t.Logf("Concurrent update error: %v", err)
						}
					}
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < goroutines; i++ {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("Concurrent access test timed out")
		}
	}
}

// LogDatabaseInfo logs database information
func (h *DatabaseTestHelper) LogDatabaseInfo(t *testing.T) {
	version := h.GetDatabaseVersion(t)
	size := h.GetDatabaseSize(t)
	tables := h.GetAllTables(t)
	fkStatus := h.GetForeignKeysStatus(t)
	integrity := h.CheckIntegrity(t)

	t.Logf("Database Info:")
	t.Logf("  Version: %s", version)
	t.Logf("  Size: %d bytes", size)
	t.Logf("  Tables: %v", tables)
	t.Logf("  Foreign Keys: %t", fkStatus)
	t.Logf("  Integrity: %t", integrity)
}

/**
 * @file backend.go
 * @description DB2-specific backend implementation for database operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/db2/backend.go
 */

package db2

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	_ "github.com/ibmdb/go_ibm_db" // DB2 driver
)

// DB2Backend implements EngineBackend for IBM DB2
type DB2Backend struct {
	config  types.Config
	db      *sql.DB
	options map[string]interface{}
}

// Connect establishes connection to DB2 database
func (b *DB2Backend) Connect(ctx context.Context) error {
	if b.db != nil {
		return nil // Already connected
	}

	dsn := b.buildConnectionString()
	db, err := sql.Open("go_ibm_db", dsn)
	if err != nil {
		return fmt.Errorf("failed to open DB2 connection: %w", err)
	}

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping DB2 database: %w", err)
	}

	// Configure connection pool for DB2
	db.SetMaxOpenConns(b.getMaxOpenConns())
	db.SetMaxIdleConns(b.getMaxIdleConns())
	db.SetConnMaxLifetime(b.getConnMaxLifetime())

	b.db = db
	return nil
}

// Disconnect closes the DB2 database connection
func (b *DB2Backend) Disconnect(ctx context.Context) error {
	if b.db == nil {
		return nil
	}

	err := b.db.Close()
	b.db = nil
	return err
}

// ExecuteSQL executes SQL statements on DB2
func (b *DB2Backend) ExecuteSQL(ctx context.Context, sql string, args ...interface{}) error {
	if b.db == nil {
		return fmt.Errorf("database connection not established")
	}

	// Handle DB2-specific SQL syntax
	sql = b.adaptSQLForDB2(sql)

	_, err := b.db.ExecContext(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to execute SQL on DB2: %w", err)
	}

	return nil
}

// QuerySQL executes a query on DB2 and returns rows
func (b *DB2Backend) QuerySQL(ctx context.Context, sql string, args ...interface{}) (*sql.Rows, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	// Handle DB2-specific SQL syntax
	sql = b.adaptSQLForDB2(sql)

	rows, err := b.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query DB2: %w", err)
	}

	return rows, nil
}

// BeginTransaction starts a new transaction on DB2
func (b *DB2Backend) BeginTransaction(ctx context.Context) (*sql.Tx, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	tx, err := b.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted, // DB2 default isolation level
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin DB2 transaction: %w", err)
	}

	return tx, nil
}

// GetDatabaseInfo returns DB2-specific database information
func (b *DB2Backend) GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	info := make(map[string]interface{})

	// Get DB2 version
	var version string
	err := b.db.QueryRowContext(ctx, "SELECT SERVICE_LEVEL FROM SYSIBMADM.ENV_INST_INFO").Scan(&version)
	if err != nil {
		version = "unknown"
	}
	info["version"] = version

	// Get database name
	var dbName string
	err = b.db.QueryRowContext(ctx, "SELECT CURRENT SERVER FROM SYSIBM.SYSDUMMY1").Scan(&dbName)
	if err != nil {
		dbName = "unknown"
	}
	info["database_name"] = dbName

	// Get current schema
	var schema string
	err = b.db.QueryRowContext(ctx, "SELECT CURRENT SCHEMA FROM SYSIBM.SYSDUMMY1").Scan(&schema)
	if err != nil {
		schema = "unknown"
	}
	info["current_schema"] = schema

	// Get character set
	var charset string
	err = b.db.QueryRowContext(ctx, "SELECT CODEPAGE FROM SYSCAT.DATABASES WHERE DBNAME = CURRENT SERVER").Scan(&charset)
	if err != nil {
		charset = "unknown"
	}
	info["character_set"] = charset

	info["driver"] = "db2"
	info["dialect"] = "db2"

	return info, nil
}

// CheckTableExists checks if a table exists in DB2
func (b *DB2Backend) CheckTableExists(ctx context.Context, tableName string) (bool, error) {
	if b.db == nil {
		return false, fmt.Errorf("database connection not established")
	}

	// DB2 system catalog query
	query := `
		SELECT COUNT(*) 
		FROM SYSCAT.TABLES 
		WHERE TABNAME = UPPER(?) 
		AND TABSCHEMA = CURRENT SCHEMA
	`

	var count int
	err := b.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence in DB2: %w", err)
	}

	return count > 0, nil
}

// CreateMigrationTable creates the migration tracking table for DB2
func (b *DB2Backend) CreateMigrationTable(ctx context.Context, tableName string) error {
	if b.db == nil {
		return fmt.Errorf("database connection not established")
	}

	// DB2-specific CREATE TABLE syntax
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id INTEGER NOT NULL GENERATED ALWAYS AS IDENTITY (START WITH 1 INCREMENT BY 1),
			version VARCHAR(255) NOT NULL,
			description VARCHAR(1000),
			type VARCHAR(50) NOT NULL DEFAULT 'SQL',
			script VARCHAR(1000) NOT NULL,
			checksum VARCHAR(255),
			installed_by VARCHAR(100) NOT NULL DEFAULT USER,
			installed_on TIMESTAMP NOT NULL DEFAULT CURRENT TIMESTAMP,
			execution_time INTEGER NOT NULL DEFAULT 0,
			success SMALLINT NOT NULL DEFAULT 1,
			PRIMARY KEY (id),
			UNIQUE (version)
		)
	`, tableName)

	if err := b.ExecuteSQL(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create migration table in DB2: %w", err)
	}

	// Create index for better performance
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IX_%s_VERSION ON %s (version)
	`, tableName, tableName)

	if err := b.ExecuteSQL(ctx, createIndexSQL); err != nil {
		// Index creation failure is not critical
		fmt.Printf("Warning: failed to create index on migration table: %v\n", err)
	}

	return nil
}

// buildConnectionString builds DB2 connection string
func (b *DB2Backend) buildConnectionString() string {
	host := b.config.Database.Host
	if host == "" {
		host = "localhost"
	}

	port := b.config.Database.Port
	if port == 0 {
		port = 50000 // Default DB2 port
	}

	database := b.config.Database.Database
	username := b.config.Database.Username
	password := b.config.Database.Password

	// DB2 connection string format
	dsn := fmt.Sprintf("HOSTNAME=%s;PORT=%d;DATABASE=%s;UID=%s;PWD=%s",
		host, port, database, username, password)

	// Add additional parameters
	if sslMode, ok := b.options["ssl_mode"].(string); ok && sslMode != "" {
		dsn += fmt.Sprintf(";SECURITY=%s", sslMode)
	}

	if timeout, ok := b.options["connect_timeout"].(int); ok && timeout > 0 {
		dsn += fmt.Sprintf(";CONNECTTIMEOUT=%d", timeout)
	}

	return dsn
}

// adaptSQLForDB2 adapts SQL statements for DB2 syntax
func (b *DB2Backend) adaptSQLForDB2(sql string) string {
	// Convert common SQL patterns to DB2 syntax
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "GENERATED ALWAYS AS IDENTITY")
	sql = strings.ReplaceAll(sql, "AUTOINCREMENT", "GENERATED ALWAYS AS IDENTITY")
	
	// Convert LIMIT clause to DB2 FETCH FIRST syntax
	if strings.Contains(strings.ToUpper(sql), "LIMIT") {
		sql = b.convertLimitToFetchFirst(sql)
	}

	// Convert boolean values
	sql = strings.ReplaceAll(sql, "TRUE", "1")
	sql = strings.ReplaceAll(sql, "FALSE", "0")

	// Convert NOW() to CURRENT TIMESTAMP
	sql = strings.ReplaceAll(sql, "NOW()", "CURRENT TIMESTAMP")

	return sql
}

// convertLimitToFetchFirst converts LIMIT clause to DB2 FETCH FIRST syntax
func (b *DB2Backend) convertLimitToFetchFirst(sql string) string {
	// Simple conversion for basic LIMIT clauses
	// This is a simplified implementation - a full implementation would need proper SQL parsing
	upperSQL := strings.ToUpper(sql)
	limitIndex := strings.Index(upperSQL, "LIMIT")
	
	if limitIndex == -1 {
		return sql
	}

	// Extract the limit value (simplified)
	limitPart := sql[limitIndex:]
	parts := strings.Fields(limitPart)
	if len(parts) >= 2 {
		limitValue := parts[1]
		// Remove the LIMIT clause and add FETCH FIRST
		sqlWithoutLimit := sql[:limitIndex]
		return sqlWithoutLimit + " FETCH FIRST " + limitValue + " ROWS ONLY"
	}

	return sql
}

// getMaxOpenConns returns the maximum number of open connections
func (b *DB2Backend) getMaxOpenConns() int {
	if maxConns, ok := b.options["max_open_conns"].(int); ok {
		return maxConns
	}
	return 25 // Default for DB2
}

// getMaxIdleConns returns the maximum number of idle connections
func (b *DB2Backend) getMaxIdleConns() int {
	if maxIdle, ok := b.options["max_idle_conns"].(int); ok {
		return maxIdle
	}
	return 5 // Default for DB2
}

// getConnMaxLifetime returns the connection maximum lifetime
func (b *DB2Backend) getConnMaxLifetime() int {
	if lifetime, ok := b.options["conn_max_lifetime"].(int); ok {
		return lifetime
	}
	return 3600 // 1 hour default
}

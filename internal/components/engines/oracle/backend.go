//go:build drifted_backend
// +build drifted_backend

/**
 * @file backend.go
 * @description Oracle-specific backend implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/oracle/backend.go
 */

package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// OracleBackend implements EngineBackend for Oracle Database
type OracleBackend struct {
	config  types.Config
	db      *sql.DB
	options map[string]interface{}
}

// Connect establishes connection to Oracle database
func (b *OracleBackend) Connect(ctx context.Context) error {
	if b.db == nil {
		return fmt.Errorf("database connection not provided")
	}

	// Test the connection
	if err := b.db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping Oracle database: %w", err)
	}

	// Set Oracle-specific session parameters
	if err := b.setSessionParameters(ctx); err != nil {
		return fmt.Errorf("failed to set Oracle session parameters: %w", err)
	}

	return nil
}

// Disconnect closes the Oracle database connection
func (b *OracleBackend) Disconnect(ctx context.Context) error {
	if b.db == nil {
		return nil
	}

	return b.db.Close()
}

// Execute executes a SQL statement
func (b *OracleBackend) Execute(ctx context.Context, sql string, args ...interface{}) error {
	if b.db == nil {
		return fmt.Errorf("database connection not established")
	}

	// Adapt SQL for Oracle syntax
	adaptedSQL := b.adaptSQLForOracle(sql)

	_, err := b.db.ExecContext(ctx, adaptedSQL, args...)
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}

	return nil
}

// Query executes a query and returns rows
func (b *OracleBackend) Query(ctx context.Context, sql string, args ...interface{}) (*sql.Rows, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	// Adapt SQL for Oracle syntax
	adaptedSQL := b.adaptSQLForOracle(sql)

	rows, err := b.db.QueryContext(ctx, adaptedSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return rows, nil
}

// QueryRow executes a query that returns a single row
func (b *OracleBackend) QueryRow(ctx context.Context, sql string, args ...interface{}) *sql.Row {
	// Adapt SQL for Oracle syntax
	adaptedSQL := b.adaptSQLForOracle(sql)

	return b.db.QueryRowContext(ctx, adaptedSQL, args...)
}

// BeginTransaction starts a new transaction
func (b *OracleBackend) BeginTransaction(ctx context.Context) (*sql.Tx, error) {
	if b.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	tx, err := b.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return tx, nil
}

// GetDatabaseInfo returns Oracle database information
func (b *OracleBackend) GetDatabaseInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// Get Oracle version
	var version string
	err := b.db.QueryRowContext(ctx, "SELECT BANNER FROM V$VERSION WHERE ROWNUM = 1").Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("failed to get Oracle version: %w", err)
	}
	info["version"] = version

	// Get database name
	var dbName string
	err = b.db.QueryRowContext(ctx, "SELECT NAME FROM V$DATABASE").Scan(&dbName)
	if err != nil {
		return nil, fmt.Errorf("failed to get database name: %w", err)
	}
	info["database_name"] = dbName

	// Get current schema
	var schema string
	err = b.db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get current schema: %w", err)
	}
	info["current_schema"] = schema

	// Get character set
	var charset string
	err = b.db.QueryRowContext(ctx, "SELECT VALUE FROM NLS_DATABASE_PARAMETERS WHERE PARAMETER = 'NLS_CHARACTERSET'").Scan(&charset)
	if err != nil {
		return nil, fmt.Errorf("failed to get character set: %w", err)
	}
	info["character_set"] = charset

	// Get tablespace information
	tablespaces, err := b.getTablespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tablespace info: %w", err)
	}
	info["tablespaces"] = tablespaces

	return info, nil
}

// TableExists checks if a table exists in Oracle
func (b *OracleBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*) 
		FROM USER_TABLES 
		WHERE UPPER(TABLE_NAME) = UPPER(:1)
	`

	err := b.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if table exists: %w", err)
	}

	return count > 0, nil
}

// CreateMigrationTable creates the migration tracking table
func (b *OracleBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			version VARCHAR2(255) NOT NULL,
			description VARCHAR2(1000),
			checksum VARCHAR2(64),
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			applied_by VARCHAR2(100) DEFAULT USER,
			execution_time NUMBER(10,3),
			success NUMBER(1) DEFAULT 1,
			CONSTRAINT pk_%s PRIMARY KEY (version)
		)
	`, tableName, strings.ToLower(tableName))

	if err := b.Execute(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	// Create index on applied_at for performance
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX idx_%s_applied_at ON %s (applied_at)
	`, strings.ToLower(tableName), tableName)

	if err := b.Execute(ctx, createIndexSQL); err != nil {
		// Index creation failure is not critical
		fmt.Printf("Warning: failed to create index on migration table: %v\n", err)
	}

	return nil
}

// setSessionParameters sets Oracle-specific session parameters
func (b *OracleBackend) setSessionParameters(ctx context.Context) error {
	sessionParams := []string{
		"ALTER SESSION SET NLS_DATE_FORMAT = 'YYYY-MM-DD HH24:MI:SS'",
		"ALTER SESSION SET NLS_TIMESTAMP_FORMAT = 'YYYY-MM-DD HH24:MI:SS.FF'",
		"ALTER SESSION SET CURRENT_SCHEMA = " + b.getCurrentSchema(),
	}

	for _, param := range sessionParams {
		if err := b.Execute(ctx, param); err != nil {
			// Some parameters might fail, but that's not critical
			fmt.Printf("Warning: failed to set session parameter: %s, error: %v\n", param, err)
		}
	}

	return nil
}

// getCurrentSchema returns the current schema name
func (b *OracleBackend) getCurrentSchema() string {
	if schema, ok := b.options["schema"].(string); ok {
		return schema
	}
	return "USER" // Default to current user schema
}

// adaptSQLForOracle adapts SQL statements for Oracle syntax
func (b *OracleBackend) adaptSQLForOracle(sql string) string {
	// Convert common SQL patterns to Oracle syntax
	adaptedSQL := sql

	// Replace MySQL/PostgreSQL AUTO_INCREMENT with Oracle IDENTITY
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "AUTO_INCREMENT", "GENERATED ALWAYS AS IDENTITY")

	// Replace SERIAL with NUMBER and IDENTITY
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "SERIAL", "NUMBER GENERATED ALWAYS AS IDENTITY")

	// Replace LIMIT with ROWNUM (simplified - proper implementation would need subquery)
	if strings.Contains(strings.ToUpper(adaptedSQL), "LIMIT") {
		// This is a simplified replacement - proper Oracle LIMIT requires more complex rewriting
		adaptedSQL = strings.ReplaceAll(adaptedSQL, "LIMIT", "AND ROWNUM <=")
	}

	// Replace NOW() with SYSDATE
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "NOW()", "SYSDATE")

	// Replace CURRENT_TIMESTAMP with SYSTIMESTAMP
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "CURRENT_TIMESTAMP", "SYSTIMESTAMP")

	// Replace boolean values
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "TRUE", "1")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "FALSE", "0")

	// Replace BOOLEAN type with NUMBER(1)
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "BOOLEAN", "NUMBER(1)")

	// Replace TEXT with CLOB for large text fields
	adaptedSQL = strings.ReplaceAll(adaptedSQL, " TEXT", " CLOB")

	// Replace BLOB with Oracle BLOB
	adaptedSQL = strings.ReplaceAll(adaptedSQL, " BLOB", " BLOB")

	// Handle IF NOT EXISTS for CREATE TABLE (Oracle 23c+)
	if strings.Contains(strings.ToUpper(adaptedSQL), "CREATE TABLE IF NOT EXISTS") {
		// For older Oracle versions, we'd need to check existence first
		adaptedSQL = strings.ReplaceAll(adaptedSQL, "IF NOT EXISTS", "")
	}

	return adaptedSQL
}

// getTablespaces retrieves tablespace information
func (b *OracleBackend) getTablespaces(ctx context.Context) ([]map[string]interface{}, error) {
	query := `
		SELECT 
			TABLESPACE_NAME,
			STATUS,
			CONTENTS,
			EXTENT_MANAGEMENT,
			ALLOCATION_TYPE
		FROM DBA_TABLESPACES
		ORDER BY TABLESPACE_NAME
	`

	rows, err := b.db.QueryContext(ctx, query)
	if err != nil {
		// If DBA_TABLESPACES is not accessible, try USER_TABLESPACES
		query = `
			SELECT 
				TABLESPACE_NAME,
				'ONLINE' AS STATUS,
				'PERMANENT' AS CONTENTS,
				'LOCAL' AS EXTENT_MANAGEMENT,
				'SYSTEM' AS ALLOCATION_TYPE
			FROM USER_TABLESPACES
			ORDER BY TABLESPACE_NAME
		`
		rows, err = b.db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to query tablespaces: %w", err)
		}
	}
	defer rows.Close()

	var tablespaces []map[string]interface{}

	for rows.Next() {
		var name, status, contents, extentMgmt, allocType string
		if err := rows.Scan(&name, &status, &contents, &extentMgmt, &allocType); err != nil {
			return nil, fmt.Errorf("failed to scan tablespace row: %w", err)
		}

		tablespace := map[string]interface{}{
			"name":              name,
			"status":            status,
			"contents":          contents,
			"extent_management": extentMgmt,
			"allocation_type":   allocType,
		}

		tablespaces = append(tablespaces, tablespace)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tablespace rows: %w", err)
	}

	return tablespaces, nil
}

// ValidateSQL performs basic SQL validation for Oracle
func (b *OracleBackend) ValidateSQL(sql string) error {
	upperSQL := strings.ToUpper(strings.TrimSpace(sql))

	if upperSQL == "" {
		return fmt.Errorf("empty SQL statement")
	}

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"DROP SCHEMA",
		"TRUNCATE TABLE",
		"DELETE FROM",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(upperSQL, pattern) {
			return fmt.Errorf("potentially dangerous operation detected: %s", pattern)
		}
	}

	// Check for Oracle-specific syntax issues
	if strings.Contains(upperSQL, "AUTO_INCREMENT") {
		return fmt.Errorf("use GENERATED ALWAYS AS IDENTITY instead of AUTO_INCREMENT for Oracle")
	}

	if strings.Contains(upperSQL, "SERIAL") {
		return fmt.Errorf("SERIAL type is not supported in Oracle, use NUMBER with GENERATED ALWAYS AS IDENTITY")
	}

	return nil
}

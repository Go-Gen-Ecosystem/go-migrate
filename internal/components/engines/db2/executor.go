/**
 * @file executor.go
 * @description DB2-specific migration executor implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/db2/executor.go
 */

package db2

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// DB2Executor implements MigrationExecutor for IBM DB2
type DB2Executor struct {
	config  types.Config
	db      *sql.DB
	dryRun  bool
	options map[string]interface{}
}

// Execute executes migrations on DB2 database
func (e *DB2Executor) Execute(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	startTime := time.Now()
	result := types.MigrationResult{
		ExecutedMigrations: make([]types.MigrationScript, 0),
		FailedMigrations:   make([]types.FailedMigrationScript, 0),
		StartTime:          startTime,
	}

	if e.dryRun {
		return e.executeDryRun(ctx, migrations)
	}

	if e.db == nil {
		return result, fmt.Errorf("database connection not established")
	}

	for _, migration := range migrations {
		migrationStartTime := time.Now()
		
		// Begin transaction for each migration
		tx, err := e.db.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
		})
		if err != nil {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     fmt.Sprintf("failed to begin transaction: %v", err),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Execute the migration
		err = e.executeMigrationInTransaction(ctx, tx, migration)
		if err != nil {
			tx.Rollback()
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     err.Error(),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     fmt.Sprintf("failed to commit transaction: %v", err),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Update migration with execution time
		migration.ExecutionTime = time.Since(migrationStartTime)
		result.ExecutedMigrations = append(result.ExecutedMigrations, migration)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Success = len(result.FailedMigrations) == 0

	return result, nil
}

// Rollback executes rollback migrations on DB2 database
func (e *DB2Executor) Rollback(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	startTime := time.Now()
	result := types.MigrationResult{
		ExecutedMigrations: make([]types.MigrationScript, 0),
		FailedMigrations:   make([]types.FailedMigrationScript, 0),
		StartTime:          startTime,
	}

	if e.dryRun {
		return e.rollbackDryRun(ctx, migrations)
	}

	if e.db == nil {
		return result, fmt.Errorf("database connection not established")
	}

	// Execute rollbacks in reverse order
	for i := len(migrations) - 1; i >= 0; i-- {
		migration := migrations[i]
		migrationStartTime := time.Now()

		// Check if migration has rollback script
		if migration.DownScript == "" {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     "no rollback script available",
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Begin transaction for rollback
		tx, err := e.db.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
		})
		if err != nil {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     fmt.Sprintf("failed to begin rollback transaction: %v", err),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Execute the rollback
		err = e.executeRollbackInTransaction(ctx, tx, migration)
		if err != nil {
			tx.Rollback()
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     err.Error(),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Commit the rollback transaction
		if err := tx.Commit(); err != nil {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     fmt.Sprintf("failed to commit rollback transaction: %v", err),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Update migration with execution time
		migration.ExecutionTime = time.Since(migrationStartTime)
		result.ExecutedMigrations = append(result.ExecutedMigrations, migration)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Success = len(result.FailedMigrations) == 0

	return result, nil
}

// executeMigrationInTransaction executes a single migration within a transaction
func (e *DB2Executor) executeMigrationInTransaction(ctx context.Context, tx *sql.Tx, migration types.MigrationScript) error {
	// Adapt SQL for DB2
	adaptedSQL := e.adaptSQLForDB2(migration.UpScript)

	// Split SQL into individual statements
	statements := e.splitSQLStatements(adaptedSQL)

	for i, statement := range statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}

		// Execute the statement
		_, err := tx.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("failed to execute statement %d in migration %s: %w", i+1, migration.Version, err)
		}
	}

	return nil
}

// executeRollbackInTransaction executes a rollback migration within a transaction
func (e *DB2Executor) executeRollbackInTransaction(ctx context.Context, tx *sql.Tx, migration types.MigrationScript) error {
	// Adapt SQL for DB2
	adaptedSQL := e.adaptSQLForDB2(migration.DownScript)

	// Split SQL into individual statements
	statements := e.splitSQLStatements(adaptedSQL)

	for i, statement := range statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}

		// Execute the rollback statement
		_, err := tx.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("failed to execute rollback statement %d in migration %s: %w", i+1, migration.Version, err)
		}
	}

	return nil
}

// executeDryRun simulates migration execution without making changes
func (e *DB2Executor) executeDryRun(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	startTime := time.Now()
	result := types.MigrationResult{
		ExecutedMigrations: make([]types.MigrationScript, 0),
		FailedMigrations:   make([]types.FailedMigrationScript, 0),
		StartTime:          startTime,
	}

	for _, migration := range migrations {
		// Validate SQL syntax for DB2
		if err := e.validateDB2SQL(migration.UpScript); err != nil {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     fmt.Sprintf("SQL validation failed: %v", err),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Simulate execution time
		migration.ExecutionTime = time.Millisecond * 100
		result.ExecutedMigrations = append(result.ExecutedMigrations, migration)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Success = len(result.FailedMigrations) == 0

	return result, nil
}

// rollbackDryRun simulates rollback execution without making changes
func (e *DB2Executor) rollbackDryRun(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	startTime := time.Now()
	result := types.MigrationResult{
		ExecutedMigrations: make([]types.MigrationScript, 0),
		FailedMigrations:   make([]types.FailedMigrationScript, 0),
		StartTime:          startTime,
	}

	for i := len(migrations) - 1; i >= 0; i-- {
		migration := migrations[i]

		// Check if rollback script exists
		if migration.DownScript == "" {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     "no rollback script available",
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Validate rollback SQL syntax for DB2
		if err := e.validateDB2SQL(migration.DownScript); err != nil {
			failedMigration := types.FailedMigrationScript{
				Migration: migration,
				Error:     fmt.Sprintf("rollback SQL validation failed: %v", err),
				Timestamp: time.Now(),
			}
			result.FailedMigrations = append(result.FailedMigrations, failedMigration)
			continue
		}

		// Simulate execution time
		migration.ExecutionTime = time.Millisecond * 50
		result.ExecutedMigrations = append(result.ExecutedMigrations, migration)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)
	result.Success = len(result.FailedMigrations) == 0

	return result, nil
}

// adaptSQLForDB2 adapts SQL statements for DB2 syntax
func (e *DB2Executor) adaptSQLForDB2(sql string) string {
	// Convert common SQL patterns to DB2 syntax
	sql = strings.ReplaceAll(sql, "AUTO_INCREMENT", "GENERATED ALWAYS AS IDENTITY")
	sql = strings.ReplaceAll(sql, "AUTOINCREMENT", "GENERATED ALWAYS AS IDENTITY")
	
	// Convert LIMIT clause to DB2 FETCH FIRST syntax
	if strings.Contains(strings.ToUpper(sql), "LIMIT") {
		sql = e.convertLimitToFetchFirst(sql)
	}

	// Convert boolean values
	sql = strings.ReplaceAll(sql, "TRUE", "1")
	sql = strings.ReplaceAll(sql, "FALSE", "0")

	// Convert NOW() to CURRENT TIMESTAMP
	sql = strings.ReplaceAll(sql, "NOW()", "CURRENT TIMESTAMP")

	// Convert DATETIME to TIMESTAMP
	sql = strings.ReplaceAll(sql, "DATETIME", "TIMESTAMP")

	return sql
}

// convertLimitToFetchFirst converts LIMIT clause to DB2 FETCH FIRST syntax
func (e *DB2Executor) convertLimitToFetchFirst(sql string) string {
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

// splitSQLStatements splits SQL script into individual statements
func (e *DB2Executor) splitSQLStatements(sql string) []string {
	// Simple statement splitting by semicolon
	// In a production environment, you'd want a proper SQL parser
	statements := strings.Split(sql, ";")
	
	var result []string
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			result = append(result, stmt)
		}
	}
	
	return result
}

// validateDB2SQL performs basic SQL validation for DB2
func (e *DB2Executor) validateDB2SQL(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("empty SQL script")
	}

	// Check for potentially dangerous operations in production
	upperSQL := strings.ToUpper(sql)
	
	// Check for DROP statements without proper conditions
	if strings.Contains(upperSQL, "DROP TABLE") && !strings.Contains(upperSQL, "IF EXISTS") {
		if !e.isDropAllowed() {
			return fmt.Errorf("DROP TABLE without IF EXISTS is not allowed")
		}
	}

	// Check for TRUNCATE statements
	if strings.Contains(upperSQL, "TRUNCATE") {
		if !e.isTruncateAllowed() {
			return fmt.Errorf("TRUNCATE statements are not allowed")
		}
	}

	// Check for DELETE without WHERE clause
	if strings.Contains(upperSQL, "DELETE FROM") && !strings.Contains(upperSQL, "WHERE") {
		if !e.isDeleteWithoutWhereAllowed() {
			return fmt.Errorf("DELETE without WHERE clause is not allowed")
		}
	}

	return nil
}

// isDropAllowed checks if DROP statements are allowed
func (e *DB2Executor) isDropAllowed() bool {
	if allowed, ok := e.options["allow_drop"].(bool); ok {
		return allowed
	}
	return false
}

// isTruncateAllowed checks if TRUNCATE statements are allowed
func (e *DB2Executor) isTruncateAllowed() bool {
	if allowed, ok := e.options["allow_truncate"].(bool); ok {
		return allowed
	}
	return false
}

// isDeleteWithoutWhereAllowed checks if DELETE without WHERE is allowed
func (e *DB2Executor) isDeleteWithoutWhereAllowed() bool {
	if allowed, ok := e.options["allow_delete_without_where"].(bool); ok {
		return allowed
	}
	return false
}

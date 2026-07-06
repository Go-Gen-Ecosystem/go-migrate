/**
 * @file components.go
 * @description Oracle-specific components: executor, tracker, lock manager, validator, and resolver
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/oracle/components.go
 */

package oracle

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// OracleExecutor implements MigrationExecutor for Oracle Database
type OracleExecutor struct {
	config  types.Config
	db      *sql.DB
	options map[string]interface{}
}

// Execute executes migrations on Oracle database
func (e *OracleExecutor) Execute(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	result := types.MigrationResult{
		Success:           true,
		ExecutedMigrations: make([]types.MigrationScript, 0),
		FailedMigrations:   make([]types.FailedMigrationScript, 0),
		StartTime:         time.Now(),
	}

	for _, migration := range migrations {
		migrationStart := time.Now()

		// Begin transaction for each migration
		tx, err := e.db.BeginTx(ctx, nil)
		if err != nil {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("Failed to begin transaction for migration %s: %v", migration.Version, err)
			result.EndTime = time.Now()
			return result, err
		}

		// Execute the migration
		adaptedSQL := e.adaptSQLForOracle(migration.UpScript)
		_, err = tx.ExecContext(ctx, adaptedSQL)

		if err != nil {
			tx.Rollback()
			result.Success = false
			result.FailedMigrations = append(result.FailedMigrations, types.FailedMigrationScript{
				Migration: migration,
				Error:     err.Error(),
				Timestamp: time.Now(),
			})
			result.ErrorMessage = fmt.Sprintf("Migration %s failed: %v", migration.Version, err)
			result.EndTime = time.Now()
			return result, err
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("Failed to commit migration %s: %v", migration.Version, err)
			result.EndTime = time.Now()
			return result, err
		}

		migration.ExecutionTime = time.Since(migrationStart)
		result.ExecutedMigrations = append(result.ExecutedMigrations, migration)
	}

	result.EndTime = time.Now()
	result.TotalExecutionTime = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Rollback executes rollback migrations on Oracle database
func (e *OracleExecutor) Rollback(ctx context.Context, migrations []types.MigrationScript) (types.MigrationResult, error) {
	result := types.MigrationResult{
		Success:           true,
		ExecutedMigrations: make([]types.MigrationScript, 0),
		FailedMigrations:   make([]types.FailedMigrationScript, 0),
		StartTime:         time.Now(),
	}

	// Execute rollbacks in reverse order
	for i := len(migrations) - 1; i >= 0; i-- {
		migration := migrations[i]
		migrationStart := time.Now()

		if migration.DownScript == "" {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("No rollback script for migration %s", migration.Version)
			result.EndTime = time.Now()
			return result, fmt.Errorf("no rollback script for migration %s", migration.Version)
		}

		// Begin transaction for each rollback
		tx, err := e.db.BeginTx(ctx, nil)
		if err != nil {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("Failed to begin transaction for rollback %s: %v", migration.Version, err)
			result.EndTime = time.Now()
			return result, err
		}

		// Execute the rollback
		adaptedSQL := e.adaptSQLForOracle(migration.DownScript)
		_, err = tx.ExecContext(ctx, adaptedSQL)

		if err != nil {
			tx.Rollback()
			result.Success = false
			result.FailedMigrations = append(result.FailedMigrations, types.FailedMigrationScript{
				Migration: migration,
				Error:     err.Error(),
				Timestamp: time.Now(),
			})
			result.ErrorMessage = fmt.Sprintf("Rollback %s failed: %v", migration.Version, err)
			result.EndTime = time.Now()
			return result, err
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			result.Success = false
			result.ErrorMessage = fmt.Sprintf("Failed to commit rollback %s: %v", migration.Version, err)
			result.EndTime = time.Now()
			return result, err
		}

		migration.ExecutionTime = time.Since(migrationStart)
		result.ExecutedMigrations = append(result.ExecutedMigrations, migration)
	}

	result.EndTime = time.Now()
	result.TotalExecutionTime = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// DryRun simulates migration execution without applying changes
func (e *OracleExecutor) DryRun(ctx context.Context, migrations []types.MigrationScript) (types.DryRunResult, error) {
	result := types.DryRunResult{
		WouldExecute: make([]types.MigrationScript, 0),
		Errors:       make([]string, 0),
		Warnings:     make([]string, 0),
	}

	for _, migration := range migrations {
		// Validate SQL syntax
		adaptedSQL := e.adaptSQLForOracle(migration.UpScript)
		if err := e.validateOracleSQL(adaptedSQL); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Migration %s: %v", migration.Version, err))
			continue
		}

		// Check for potential issues
		warnings := e.checkOracleSpecificIssues(migration)
		result.Warnings = append(result.Warnings, warnings...)

		result.WouldExecute = append(result.WouldExecute, migration)
	}

	return result, nil
}

// adaptSQLForOracle adapts SQL for Oracle syntax
func (e *OracleExecutor) adaptSQLForOracle(sql string) string {
	adaptedSQL := sql

	// Replace AUTO_INCREMENT with Oracle IDENTITY
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "AUTO_INCREMENT", "GENERATED ALWAYS AS IDENTITY")

	// Replace SERIAL with NUMBER and IDENTITY
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "SERIAL", "NUMBER GENERATED ALWAYS AS IDENTITY")

	// Replace LIMIT with ROWNUM (simplified)
	if strings.Contains(strings.ToUpper(adaptedSQL), "LIMIT") {
		adaptedSQL = strings.ReplaceAll(adaptedSQL, "LIMIT", "AND ROWNUM <=")
	}

	// Replace NOW() with SYSDATE
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "NOW()", "SYSDATE")

	// Replace boolean values
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "TRUE", "1")
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "FALSE", "0")

	// Replace BOOLEAN type with NUMBER(1)
	adaptedSQL = strings.ReplaceAll(adaptedSQL, "BOOLEAN", "NUMBER(1)")

	return adaptedSQL
}

// validateOracleSQL validates SQL for Oracle compatibility
func (e *OracleExecutor) validateOracleSQL(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("empty SQL script")
	}

	upperSQL := strings.ToUpper(sql)

	// Check for unsupported syntax
	if strings.Contains(upperSQL, "AUTO_INCREMENT") {
		return fmt.Errorf("use GENERATED ALWAYS AS IDENTITY instead of AUTO_INCREMENT")
	}

	if strings.Contains(upperSQL, "SERIAL") {
		return fmt.Errorf("SERIAL type not supported, use NUMBER with GENERATED ALWAYS AS IDENTITY")
	}

	return nil
}

// checkOracleSpecificIssues checks for Oracle-specific potential issues
func (e *OracleExecutor) checkOracleSpecificIssues(migration types.MigrationScript) []string {
	var warnings []string
	upperSQL := strings.ToUpper(migration.UpScript)

	// Check for missing tablespace specification
	if strings.Contains(upperSQL, "CREATE TABLE") && !strings.Contains(upperSQL, "TABLESPACE") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: Consider specifying TABLESPACE for better performance", migration.Version))
	}

	// Check for large table operations
	if strings.Contains(upperSQL, "ALTER TABLE") && strings.Contains(upperSQL, "ADD COLUMN") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: ALTER TABLE ADD COLUMN may cause performance issues on large tables", migration.Version))
	}

	return warnings
}

// OracleTracker implements MigrationTracker for Oracle Database
type OracleTracker struct {
	config    types.Config
	db        *sql.DB
	tableName string
	options   map[string]interface{}
}

// GetAppliedMigrations returns applied migrations from Oracle
func (t *OracleTracker) GetAppliedMigrations(ctx context.Context) ([]types.MigrationScript, error) {
	if err := t.ensureTrackingTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure tracking table: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT version, description, checksum, applied_at, execution_time
		FROM %s
		WHERE success = 1
		ORDER BY applied_at
	`, t.tableName)

	rows, err := t.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	var migrations []types.MigrationScript

	for rows.Next() {
		var migration types.MigrationScript
		var appliedAt time.Time
		var executionTimeMs sql.NullFloat64

		err := rows.Scan(
			&migration.Version,
			&migration.Description,
			&migration.Checksum,
			&appliedAt,
			&executionTimeMs,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration row: %w", err)
		}

		migration.AppliedAt = appliedAt
		if executionTimeMs.Valid {
			migration.ExecutionTime = time.Duration(executionTimeMs.Float64) * time.Millisecond
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// GetPendingMigrations returns pending migrations
func (t *OracleTracker) GetPendingMigrations(ctx context.Context) ([]types.MigrationScript, error) {
	// This would typically come from a migration loader
	// For now, return empty slice
	return make([]types.MigrationScript, 0), nil
}

// RecordMigration records a successful migration
func (t *OracleTracker) RecordMigration(ctx context.Context, migration types.MigrationScript) error {
	if err := t.ensureTrackingTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure tracking table: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (version, description, checksum, applied_at, applied_by, execution_time, success)
		VALUES (:1, :2, :3, SYSTIMESTAMP, USER, :4, 1)
	`, t.tableName)

	executionTimeMs := float64(migration.ExecutionTime.Nanoseconds()) / 1e6

	_, err := t.db.ExecContext(ctx, query,
		migration.Version,
		migration.Description,
		migration.Checksum,
		executionTimeMs,
	)

	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return nil
}

// RemoveMigration removes a migration record (for rollbacks)
func (t *OracleTracker) RemoveMigration(ctx context.Context, version string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE version = :1`, t.tableName)

	result, err := t.db.ExecContext(ctx, query, version)
	if err != nil {
		return fmt.Errorf("failed to remove migration: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("migration %s not found", version)
	}

	return nil
}

// ensureTrackingTable creates the tracking table if it doesn't exist
func (t *OracleTracker) ensureTrackingTable(ctx context.Context) error {
	// Check if table exists
	var count int
	checkQuery := `
		SELECT COUNT(*) 
		FROM USER_TABLES 
		WHERE UPPER(TABLE_NAME) = UPPER(:1)
	`

	err := t.db.QueryRowContext(ctx, checkQuery, t.tableName).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	if count > 0 {
		return nil // Table already exists
	}

	// Create the table
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			version VARCHAR2(255) NOT NULL,
			description VARCHAR2(1000),
			checksum VARCHAR2(64),
			applied_at TIMESTAMP DEFAULT SYSTIMESTAMP,
			applied_by VARCHAR2(100) DEFAULT USER,
			execution_time NUMBER(10,3),
			success NUMBER(1) DEFAULT 1,
			CONSTRAINT pk_%s PRIMARY KEY (version)
		)
	`, t.tableName, strings.ToLower(t.tableName))

	_, err = t.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create tracking table: %w", err)
	}

	return nil
}

// OracleLockManager implements LockManager for Oracle Database
type OracleLockManager struct {
	config    types.Config
	db        *sql.DB
	tableName string
	options   map[string]interface{}
}

// AcquireLock acquires a migration lock in Oracle
func (lm *OracleLockManager) AcquireLock(ctx context.Context, lockInfo types.LockInfo) error {
	if err := lm.ensureLockTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Use Oracle's DBMS_LOCK package for advisory locks
	query := fmt.Sprintf(`
		INSERT INTO %s (lock_id, owner, operation, acquired_at, expires_at)
		VALUES (:1, :2, :3, SYSTIMESTAMP, SYSTIMESTAMP + INTERVAL '%d' SECOND)
	`, lm.tableName, lm.getLockTimeout())

	_, err := lm.db.ExecContext(ctx, query, lockInfo.ID, lockInfo.Owner, lockInfo.Operation)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return fmt.Errorf("migration lock %s is already held", lockInfo.ID)
		}
		return fmt.Errorf("failed to acquire lock %s: %w", lockInfo.ID, err)
	}

	return nil
}

// ReleaseLock releases a migration lock in Oracle
func (lm *OracleLockManager) ReleaseLock(ctx context.Context, lockID string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE lock_id = :1`, lm.tableName)

	result, err := lm.db.ExecContext(ctx, query, lockID)
	if err != nil {
		return fmt.Errorf("failed to release lock %s: %w", lockID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for lock %s: %w", lockID, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("lock %s not found or already released", lockID)
	}

	return nil
}

// IsLocked checks if a migration lock exists in Oracle
func (lm *OracleLockManager) IsLocked(ctx context.Context, lockID string) (bool, error) {
	if err := lm.ensureLockTable(ctx); err != nil {
		return false, fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Clean up expired locks first
	lm.cleanupExpiredLocks(ctx)

	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s 
		WHERE lock_id = :1 AND expires_at > SYSTIMESTAMP
	`, lm.tableName)

	var count int
	err := lm.db.QueryRowContext(ctx, query, lockID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// ensureLockTable creates the lock table if it doesn't exist
func (lm *OracleLockManager) ensureLockTable(ctx context.Context) error {
	// Check if table exists
	var count int
	checkQuery := `
		SELECT COUNT(*) 
		FROM USER_TABLES 
		WHERE UPPER(TABLE_NAME) = UPPER(:1)
	`

	err := lm.db.QueryRowContext(ctx, checkQuery, lm.tableName).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	if count > 0 {
		return nil // Table already exists
	}

	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			lock_id VARCHAR2(255) NOT NULL PRIMARY KEY,
			owner VARCHAR2(255) NOT NULL,
			operation VARCHAR2(100) NOT NULL,
			acquired_at TIMESTAMP DEFAULT SYSTIMESTAMP,
			expires_at TIMESTAMP NOT NULL
		)
	`, lm.tableName)

	_, err = lm.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create lock table: %w", err)
	}

	return nil
}

// cleanupExpiredLocks removes expired locks
func (lm *OracleLockManager) cleanupExpiredLocks(ctx context.Context) {
	query := fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= SYSTIMESTAMP`, lm.tableName)
	lm.db.ExecContext(ctx, query)
}

// getLockTimeout returns the lock timeout in seconds
func (lm *OracleLockManager) getLockTimeout() int {
	if timeout, ok := lm.options["lock_timeout"].(int); ok {
		return timeout
	}
	return 3600 // 1 hour default
}

// OracleValidator implements MigrationValidator for Oracle Database
type OracleValidator struct {
	config  types.Config
	db      *sql.DB
	options map[string]interface{}
}

// Validate validates migrations for Oracle compatibility
func (v *OracleValidator) Validate(ctx context.Context, migrations []types.MigrationScript) (types.ValidationResult, error) {
	result := types.ValidationResult{
		Valid:        true,
		Errors:       make([]string, 0),
		Warnings:     make([]string, 0),
		ErrorCount:   0,
		WarningCount: 0,
	}

	for _, migration := range migrations {
		// Validate migration structure
		if err := v.validateMigrationStructure(migration); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Migration %s: %v", migration.Version, err))
			result.Valid = false
		}

		// Validate SQL syntax for Oracle
		if err := v.validateOracleSQL(migration.UpScript); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Migration %s up script: %v", migration.Version, err))
			result.Valid = false
		}

		// Validate rollback script if present
		if migration.DownScript != "" {
			if err := v.validateOracleSQL(migration.DownScript); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Migration %s down script: %v", migration.Version, err))
				result.Valid = false
			}
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Migration %s has no rollback script", migration.Version))
		}

		// Check for Oracle-specific best practices
		warnings := v.checkOracleBestPractices(migration)
		result.Warnings = append(result.Warnings, warnings...)
	}

	// Validate migration order and dependencies
	if err := v.validateMigrationOrder(migrations); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Migration order validation: %v", err))
		result.Valid = false
	}

	result.ErrorCount = len(result.Errors)
	result.WarningCount = len(result.Warnings)

	return result, nil
}

// validateMigrationStructure validates the basic structure of a migration
func (v *OracleValidator) validateMigrationStructure(migration types.MigrationScript) error {
	if migration.Version == "" {
		return fmt.Errorf("migration version is required")
	}

	if migration.UpScript == "" {
		return fmt.Errorf("migration up script is required")
	}

	if migration.Description == "" {
		return fmt.Errorf("migration description is required")
	}

	// Validate version format
	if !v.isValidVersionFormat(migration.Version) {
		return fmt.Errorf("invalid version format: %s", migration.Version)
	}

	return nil
}

// validateOracleSQL validates SQL for Oracle compatibility
func (v *OracleValidator) validateOracleSQL(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("empty SQL script")
	}

	upperSQL := strings.ToUpper(sql)

	// Check for unsupported syntax
	if strings.Contains(upperSQL, "AUTO_INCREMENT") {
		return fmt.Errorf("use GENERATED ALWAYS AS IDENTITY instead of AUTO_INCREMENT for Oracle")
	}

	if strings.Contains(upperSQL, "SERIAL") {
		return fmt.Errorf("SERIAL type is not supported in Oracle, use NUMBER with GENERATED ALWAYS AS IDENTITY")
	}

	// Validate Oracle identifier names
	if err := v.validateOracleNames(sql); err != nil {
		return err
	}

	return nil
}

// validateOracleNames validates table and column names for Oracle compliance
func (v *OracleValidator) validateOracleNames(sql string) error {
	// Oracle identifier rules: max 128 characters (30 in older versions), must start with letter
	identifierPattern := regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9_$#]*\b`)
	
	words := identifierPattern.FindAllString(sql, -1)
	
	for _, word := range words {
		if len(word) > 128 {
			return fmt.Errorf("identifier '%s' exceeds Oracle maximum length of 128 characters", word)
		}
		
		// Check for reserved words
		if v.isOracleReservedWord(strings.ToUpper(word)) {
			return fmt.Errorf("'%s' is an Oracle reserved word and should be quoted", word)
		}
	}
	
	return nil
}

// isOracleReservedWord checks if a word is an Oracle reserved word
func (v *OracleValidator) isOracleReservedWord(word string) bool {
	reservedWords := map[string]bool{
		"ACCESS": true, "ADD": true, "ALL": true, "ALTER": true, "AND": true,
		"ANY": true, "AS": true, "ASC": true, "AUDIT": true, "BETWEEN": true,
		"BY": true, "CHAR": true, "CHECK": true, "CLUSTER": true, "COLUMN": true,
		"COMMENT": true, "COMPRESS": true, "CONNECT": true, "CREATE": true,
		"CURRENT": true, "DATE": true, "DECIMAL": true, "DEFAULT": true,
		"DELETE": true, "DESC": true, "DISTINCT": true, "DROP": true,
		"ELSE": true, "EXCLUSIVE": true, "EXISTS": true, "FILE": true,
		"FLOAT": true, "FOR": true, "FROM": true, "GRANT": true, "GROUP": true,
		"HAVING": true, "IDENTIFIED": true, "IMMEDIATE": true, "IN": true,
		"INCREMENT": true, "INDEX": true, "INITIAL": true, "INSERT": true,
		"INTEGER": true, "INTERSECT": true, "INTO": true, "IS": true,
		"LEVEL": true, "LIKE": true, "LOCK": true, "LONG": true, "MAXEXTENTS": true,
		"MINUS": true, "MODE": true, "MODIFY": true, "NOAUDIT": true,
		"NOCOMPRESS": true, "NOT": true, "NOWAIT": true, "NULL": true,
		"NUMBER": true, "OF": true, "OFFLINE": true, "ON": true, "ONLINE": true,
		"OPTION": true, "OR": true, "ORDER": true, "PCTFREE": true,
		"PRIOR": true, "PUBLIC": true, "RAW": true, "RENAME": true,
		"RESOURCE": true, "REVOKE": true, "ROW": true, "ROWID": true,
		"ROWNUM": true, "ROWS": true, "SELECT": true, "SESSION": true,
		"SET": true, "SHARE": true, "SIZE": true, "SMALLINT": true,
		"START": true, "SUCCESSFUL": true, "SYNONYM": true, "SYSDATE": true,
		"TABLE": true, "THEN": true, "TO": true, "TRIGGER": true,
		"UID": true, "UNION": true, "UNIQUE": true, "UPDATE": true,
		"USER": true, "VALIDATE": true, "VALUES": true, "VARCHAR": true,
		"VARCHAR2": true, "VIEW": true, "WHENEVER": true, "WHERE": true,
		"WITH": true,
	}
	
	return reservedWords[word]
}

// checkOracleBestPractices checks for Oracle-specific best practices
func (v *OracleValidator) checkOracleBestPractices(migration types.MigrationScript) []string {
	var warnings []string
	upperSQL := strings.ToUpper(migration.UpScript)

	// Check for missing tablespace specification
	if strings.Contains(upperSQL, "CREATE TABLE") && !strings.Contains(upperSQL, "TABLESPACE") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: Consider specifying TABLESPACE for better performance", migration.Version))
	}

	// Check for missing indexes on foreign keys
	if strings.Contains(upperSQL, "FOREIGN KEY") && !strings.Contains(upperSQL, "CREATE INDEX") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: Consider creating indexes on foreign key columns", migration.Version))
	}

	// Check for large table operations
	if strings.Contains(upperSQL, "ALTER TABLE") && strings.Contains(upperSQL, "ADD COLUMN") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: ALTER TABLE ADD COLUMN may cause performance issues on large tables", migration.Version))
	}

	return warnings
}

// validateMigrationOrder validates that migrations are in correct order
func (v *OracleValidator) validateMigrationOrder(migrations []types.MigrationScript) error {
	if len(migrations) <= 1 {
		return nil
	}

	// Sort migrations by version to check order
	sortedMigrations := make([]types.MigrationScript, len(migrations))
	copy(sortedMigrations, migrations)
	
	sort.Slice(sortedMigrations, func(i, j int) bool {
		return sortedMigrations[i].Version < sortedMigrations[j].Version
	})

	// Check if original order matches sorted order
	for i, migration := range migrations {
		if migration.Version != sortedMigrations[i].Version {
			return fmt.Errorf("migrations are not in correct order: %s should come before %s", 
				sortedMigrations[i].Version, migration.Version)
		}
	}

	return nil
}

// isValidVersionFormat validates the version format
func (v *OracleValidator) isValidVersionFormat(version string) bool {
	// Accept semantic version (x.y.z) or timestamp (YYYYMMDDHHMMSS) format
	semverPattern := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	timestampPattern := regexp.MustCompile(`^\d{14}$`)
	
	return semverPattern.MatchString(version) || timestampPattern.MatchString(version)
}

// OracleResolver implements DependencyResolver for Oracle Database
type OracleResolver struct {
	config  types.Config
	options map[string]interface{}
}

// ResolveDependencies resolves migration dependencies for Oracle
func (r *OracleResolver) ResolveDependencies(ctx context.Context, migrations []types.MigrationScript) (types.ExecutionPlan, error) {
	plan := types.ExecutionPlan{
		Migrations:  make([]types.MigrationScript, 0),
		Phases:      make([]types.ExecutionPhase, 0),
		Constraints: make([]types.DependencyConstraint, 0),
	}

	if len(migrations) == 0 {
		return plan, nil
	}

	// Build dependency graph
	dependencyGraph := r.buildDependencyGraph(migrations)

	// Perform topological sort to resolve dependencies
	sortedMigrations, err := r.topologicalSort(migrations, dependencyGraph)
	if err != nil {
		return plan, fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	// Create execution phases
	phases := r.createExecutionPhases(sortedMigrations)

	plan.Migrations = sortedMigrations
	plan.Phases = phases
	plan.Constraints = r.extractConstraints(migrations)

	return plan, nil
}

// buildDependencyGraph builds a dependency graph from migrations
func (r *OracleResolver) buildDependencyGraph(migrations []types.MigrationScript) map[string][]string {
	graph := make(map[string][]string)

	for _, migration := range migrations {
		graph[migration.Version] = make([]string, 0)

		// Extract dependencies from migration metadata or comments
		dependencies := r.extractDependencies(migration)
		graph[migration.Version] = dependencies
	}

	return graph
}

// extractDependencies extracts dependencies from migration
func (r *OracleResolver) extractDependencies(migration types.MigrationScript) []string {
	var dependencies []string

	// Look for dependency comments in the script
	lines := strings.Split(migration.UpScript, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-- DEPENDS ON:") {
			depLine := strings.TrimPrefix(line, "-- DEPENDS ON:")
			deps := strings.Split(depLine, ",")
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					dependencies = append(dependencies, dep)
				}
			}
		}
	}

	return dependencies
}

// topologicalSort performs topological sort on the dependency graph
func (r *OracleResolver) topologicalSort(migrations []types.MigrationScript, graph map[string][]string) ([]types.MigrationScript, error) {
	// Kahn's algorithm for topological sorting
	inDegree := make(map[string]int)
	migrationMap := make(map[string]types.MigrationScript)

	// Initialize
	for _, migration := range migrations {
		inDegree[migration.Version] = 0
		migrationMap[migration.Version] = migration
	}

	// Calculate in-degrees
	for _, deps := range graph {
		for _, dep := range deps {
			if _, exists := inDegree[dep]; exists {
				inDegree[dep]++
			}
		}
	}

	// Find nodes with no incoming edges
	queue := make([]string, 0)
	for version, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, version)
		}
	}

	var result []types.MigrationScript
	for len(queue) > 0 {
		// Remove a node from queue
		current := queue[0]
		queue = queue[1:]
		
		result = append(result, migrationMap[current])

		// For each neighbor of current node
		for _, neighbor := range graph[current] {
			if _, exists := inDegree[neighbor]; exists {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					queue = append(queue, neighbor)
				}
			}
		}
	}

	// Check for cycles
	if len(result) != len(migrations) {
		return nil, fmt.Errorf("circular dependency detected in migrations")
	}

	return result, nil
}

// createExecutionPhases creates execution phases for parallel execution
func (r *OracleResolver) createExecutionPhases(migrations []types.MigrationScript) []types.ExecutionPhase {
	var phases []types.ExecutionPhase

	// For Oracle, we typically execute migrations sequentially due to schema dependencies
	currentPhase := types.ExecutionPhase{
		Name:        "sequential",
		Migrations:  make([]types.MigrationScript, 0),
		CanParallel: false,
	}

	for _, migration := range migrations {
		currentPhase.Migrations = append(currentPhase.Migrations, migration)
	}

	phases = append(phases, currentPhase)
	return phases
}

// extractConstraints extracts dependency constraints
func (r *OracleResolver) extractConstraints(migrations []types.MigrationScript) []types.DependencyConstraint {
	var constraints []types.DependencyConstraint

	for _, migration := range migrations {
		// Extract constraints from migration metadata
		if strings.Contains(migration.UpScript, "-- CONSTRAINT:") {
			constraint := types.DependencyConstraint{
				Type:        "order",
				Source:      migration.Version,
				Target:      "", // Will be filled based on constraint type
				Description: fmt.Sprintf("Migration %s has ordering constraints", migration.Version),
			}
			constraints = append(constraints, constraint)
		}
	}

	return constraints
}

// Helper function to calculate checksum for migrations
func calculateChecksum(content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", hash)
}

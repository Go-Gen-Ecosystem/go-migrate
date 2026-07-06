/**
 * @file tracker.go
 * @description DB2-specific migration tracker implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/db2/tracker.go
 */

package db2

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// DB2Tracker implements MigrationTracker for IBM DB2
type DB2Tracker struct {
	config    types.Config
	db        *sql.DB
	tableName string
	options   map[string]interface{}
}

// GetAppliedMigrations returns all applied migrations from DB2
func (t *DB2Tracker) GetAppliedMigrations(ctx context.Context) ([]types.MigrationScript, error) {
	if t.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	// Ensure migration table exists
	if err := t.ensureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migration table: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT version, description, type, script, checksum, 
			   installed_by, installed_on, execution_time, success
		FROM %s 
		WHERE success = 1
		ORDER BY installed_on ASC
	`, t.tableName)

	rows, err := t.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	var migrations []types.MigrationScript
	for rows.Next() {
		var migration types.MigrationScript
		var installedOn time.Time
		var executionTimeMs int

		err := rows.Scan(
			&migration.Version,
			&migration.Description,
			&migration.Type,
			&migration.UpScript,
			&migration.Checksum,
			&migration.InstalledBy,
			&installedOn,
			&executionTimeMs,
			&migration.Success,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration row: %w", err)
		}

		migration.InstalledOn = installedOn
		migration.ExecutionTime = time.Duration(executionTimeMs) * time.Millisecond

		migrations = append(migrations, migration)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating migration rows: %w", err)
	}

	return migrations, nil
}

// GetPendingMigrations returns migrations that haven't been applied yet
func (t *DB2Tracker) GetPendingMigrations(ctx context.Context, allMigrations []types.MigrationScript) ([]types.MigrationScript, error) {
	appliedMigrations, err := t.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	// Create a map of applied migration versions for quick lookup
	appliedVersions := make(map[string]bool)
	for _, migration := range appliedMigrations {
		appliedVersions[migration.Version] = true
	}

	// Filter out applied migrations
	var pendingMigrations []types.MigrationScript
	for _, migration := range allMigrations {
		if !appliedVersions[migration.Version] {
			pendingMigrations = append(pendingMigrations, migration)
		}
	}

	return pendingMigrations, nil
}

// RecordMigration records a successful migration execution
func (t *DB2Tracker) RecordMigration(ctx context.Context, migration types.MigrationScript) error {
	if t.db == nil {
		return fmt.Errorf("database connection not established")
	}

	// Ensure migration table exists
	if err := t.ensureMigrationTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure migration table: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (version, description, type, script, checksum, 
						installed_by, installed_on, execution_time, success)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT TIMESTAMP, ?, 1)
	`, t.tableName)

	executionTimeMs := int(migration.ExecutionTime.Milliseconds())
	installedBy := migration.InstalledBy
	if installedBy == "" {
		installedBy = "db2_migration_engine"
	}

	_, err := t.db.ExecContext(ctx, query,
		migration.Version,
		migration.Description,
		migration.Type,
		migration.UpScript,
		migration.Checksum,
		installedBy,
		executionTimeMs,
	)

	if err != nil {
		return fmt.Errorf("failed to record migration %s: %w", migration.Version, err)
	}

	return nil
}

// RemoveMigration removes a migration record (for rollback)
func (t *DB2Tracker) RemoveMigration(ctx context.Context, version string) error {
	if t.db == nil {
		return fmt.Errorf("database connection not established")
	}

	// Ensure migration table exists
	if err := t.ensureMigrationTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure migration table: %w", err)
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE version = ?`, t.tableName)

	result, err := t.db.ExecContext(ctx, query, version)
	if err != nil {
		return fmt.Errorf("failed to remove migration %s: %w", version, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for migration %s: %w", version, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("migration %s not found in tracking table", version)
	}

	return nil
}

// GetMigrationByVersion returns a specific migration by version
func (t *DB2Tracker) GetMigrationByVersion(ctx context.Context, version string) (*types.MigrationScript, error) {
	if t.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	// Ensure migration table exists
	if err := t.ensureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migration table: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT version, description, type, script, checksum, 
			   installed_by, installed_on, execution_time, success
		FROM %s 
		WHERE version = ?
	`, t.tableName)

	row := t.db.QueryRowContext(ctx, query, version)

	var migration types.MigrationScript
	var installedOn time.Time
	var executionTimeMs int

	err := row.Scan(
		&migration.Version,
		&migration.Description,
		&migration.Type,
		&migration.UpScript,
		&migration.Checksum,
		&migration.InstalledBy,
		&installedOn,
		&executionTimeMs,
		&migration.Success,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Migration not found
		}
		return nil, fmt.Errorf("failed to get migration %s: %w", version, err)
	}

	migration.InstalledOn = installedOn
	migration.ExecutionTime = time.Duration(executionTimeMs) * time.Millisecond

	return &migration, nil
}

// GetLastAppliedMigration returns the most recently applied migration
func (t *DB2Tracker) GetLastAppliedMigration(ctx context.Context) (*types.MigrationScript, error) {
	if t.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	// Ensure migration table exists
	if err := t.ensureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migration table: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT version, description, type, script, checksum, 
			   installed_by, installed_on, execution_time, success
		FROM %s 
		WHERE success = 1
		ORDER BY installed_on DESC
		FETCH FIRST 1 ROWS ONLY
	`, t.tableName)

	row := t.db.QueryRowContext(ctx, query)

	var migration types.MigrationScript
	var installedOn time.Time
	var executionTimeMs int

	err := row.Scan(
		&migration.Version,
		&migration.Description,
		&migration.Type,
		&migration.UpScript,
		&migration.Checksum,
		&migration.InstalledBy,
		&installedOn,
		&executionTimeMs,
		&migration.Success,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No migrations applied yet
		}
		return nil, fmt.Errorf("failed to get last applied migration: %w", err)
	}

	migration.InstalledOn = installedOn
	migration.ExecutionTime = time.Duration(executionTimeMs) * time.Millisecond

	return &migration, nil
}

// GetMigrationHistory returns the complete migration history
func (t *DB2Tracker) GetMigrationHistory(ctx context.Context) ([]types.MigrationScript, error) {
	if t.db == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	// Ensure migration table exists
	if err := t.ensureMigrationTable(ctx); err != nil {
		return nil, fmt.Errorf("failed to ensure migration table: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT version, description, type, script, checksum, 
			   installed_by, installed_on, execution_time, success
		FROM %s 
		ORDER BY installed_on ASC
	`, t.tableName)

	rows, err := t.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query migration history: %w", err)
	}
	defer rows.Close()

	var migrations []types.MigrationScript
	for rows.Next() {
		var migration types.MigrationScript
		var installedOn time.Time
		var executionTimeMs int

		err := rows.Scan(
			&migration.Version,
			&migration.Description,
			&migration.Type,
			&migration.UpScript,
			&migration.Checksum,
			&migration.InstalledBy,
			&installedOn,
			&executionTimeMs,
			&migration.Success,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration history row: %w", err)
		}

		migration.InstalledOn = installedOn
		migration.ExecutionTime = time.Duration(executionTimeMs) * time.Millisecond

		migrations = append(migrations, migration)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating migration history rows: %w", err)
	}

	return migrations, nil
}

// ensureMigrationTable creates the migration tracking table if it doesn't exist
func (t *DB2Tracker) ensureMigrationTable(ctx context.Context) error {
	// Check if table exists
	exists, err := t.tableExists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if migration table exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create the migration table
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id INTEGER NOT NULL GENERATED ALWAYS AS IDENTITY (START WITH 1 INCREMENT BY 1),
			version VARCHAR(255) NOT NULL,
			description VARCHAR(1000),
			type VARCHAR(50) NOT NULL DEFAULT 'SQL',
			script CLOB NOT NULL,
			checksum VARCHAR(255),
			installed_by VARCHAR(100) NOT NULL DEFAULT USER,
			installed_on TIMESTAMP NOT NULL DEFAULT CURRENT TIMESTAMP,
			execution_time INTEGER NOT NULL DEFAULT 0,
			success SMALLINT NOT NULL DEFAULT 1,
			PRIMARY KEY (id),
			UNIQUE (version)
		)
	`, t.tableName)

	_, err = t.db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	// Create index for better performance
	createIndexSQL := fmt.Sprintf(`
		CREATE INDEX IX_%s_VERSION ON %s (version)
	`, t.tableName, t.tableName)

	_, err = t.db.ExecContext(ctx, createIndexSQL)
	if err != nil {
		// Index creation failure is not critical
		fmt.Printf("Warning: failed to create index on migration table: %v\n", err)
	}

	return nil
}

// tableExists checks if the migration tracking table exists
func (t *DB2Tracker) tableExists(ctx context.Context) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM SYSCAT.TABLES 
		WHERE TABNAME = UPPER(?) 
		AND TABSCHEMA = CURRENT SCHEMA
	`

	var count int
	err := t.db.QueryRowContext(ctx, query, t.tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	return count > 0, nil
}

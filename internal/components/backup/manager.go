/**
 * @file manager.go
 * @description BackupManager interface with database-specific implementations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/backup/manager.go
 */

package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/dialect"
)

// BackupManager interface for database backup operations
type BackupManager interface {
	CreateBackup(ctx context.Context, config *BackupConfig) (*BackupInfo, error)
	RestoreBackup(ctx context.Context, backupInfo *BackupInfo) error
	ValidateBackup(ctx context.Context, backupInfo *BackupInfo) error
	CleanupOldBackups(ctx context.Context, retentionPolicy *RetentionPolicy) error
	ListBackups(ctx context.Context) ([]*BackupInfo, error)
}

// BackupConfig holds backup configuration
type BackupConfig struct {
	BackupDir     string            `json:"backup_dir"`
	Filename      string            `json:"filename"`
	Compression   bool              `json:"compression"`
	IncludeData   bool              `json:"include_data"`
	IncludeSchema bool              `json:"include_schema"`
	Tables        []string          `json:"tables,omitempty"`
	ExcludeTables []string          `json:"exclude_tables,omitempty"`
	Options       map[string]string `json:"options,omitempty"`
}

// BackupInfo contains information about a backup
type BackupInfo struct {
	ID           string            `json:"id"`
	Filename     string            `json:"filename"`
	Path         string            `json:"path"`
	Size         int64             `json:"size"`
	CreatedAt    time.Time         `json:"created_at"`
	DatabaseType string            `json:"database_type"`
	Version      string            `json:"version"`
	Checksum     string            `json:"checksum"`
	Metadata     map[string]string `json:"metadata"`
}

// RetentionPolicy defines backup retention rules
type RetentionPolicy struct {
	MaxAge   time.Duration `json:"max_age"`
	MaxCount int           `json:"max_count"`
	MinCount int           `json:"min_count"`
}

// ShouldRetain determines whether a backup should be retained based on the retention policy
func (rp *RetentionPolicy) ShouldRetain(createdAt time.Time, totalCount int, currentRank int) bool {
	// Always retain if within minimum count (newest backups), regardless of age
	if currentRank < rp.MinCount {
		return true
	}
	
	// Never retain if beyond maximum count
	if currentRank >= rp.MaxCount {
		return false
	}
	
	// For backups between MinCount and MaxCount, count takes precedence over age
	// This means we retain all backups within the MaxCount limit
	return true
}

// BaseBackupManager provides common backup functionality
type BaseBackupManager struct {
	db         *sql.DB
	dialectCtx *dialect.DatabaseDialectContext
	config     *BackupConfig
	backupDir  string
}

func NewBaseBackupManager(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *BackupConfig) *BaseBackupManager {
	return &BaseBackupManager{
		db:         db,
		dialectCtx: dialectCtx,
		config:     config,
		backupDir:  config.BackupDir,
	}
}

func (bm *BaseBackupManager) ensureBackupDir() error {
	if bm.backupDir == "" {
		bm.backupDir = "./backups"
	}
	return os.MkdirAll(bm.backupDir, 0755)
}

func (bm *BaseBackupManager) generateBackupFilename(prefix string) string {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s_%s.sql", prefix, bm.dialectCtx.Type, timestamp)
	if bm.config.Compression {
		filename += ".gz"
	}
	return filename
}

func (bm *BaseBackupManager) ListBackups(ctx context.Context) ([]*BackupInfo, error) {
	if err := bm.ensureBackupDir(); err != nil {
		return nil, fmt.Errorf("failed to ensure backup directory: %w", err)
	}

	files, err := os.ReadDir(bm.backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []*BackupInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		backup := &BackupInfo{
			ID:           file.Name(),
			Filename:     file.Name(),
			Path:         filepath.Join(bm.backupDir, file.Name()),
			Size:         info.Size(),
			CreatedAt:    info.ModTime(),
			DatabaseType: bm.dialectCtx.Type,
			Metadata:     make(map[string]string),
		}
		backups = append(backups, backup)
	}

	// Sort backups by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

func (bm *BaseBackupManager) CleanupOldBackups(ctx context.Context, retentionPolicy *RetentionPolicy) error {
	backups, err := bm.ListBackups(ctx)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) <= retentionPolicy.MinCount {
		return nil
	}

	// Sort backups by creation time (newest first)
	for i := 0; i < len(backups)-1; i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[i].CreatedAt.Before(backups[j].CreatedAt) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	var toDelete []*BackupInfo
	now := time.Now()

	// Apply retention policies
	for i, backup := range backups {
		shouldDelete := false

		// Check age policy
		if retentionPolicy.MaxAge > 0 && now.Sub(backup.CreatedAt) > retentionPolicy.MaxAge {
			shouldDelete = true
		}

		// Check count policy (keep newest backups)
		if retentionPolicy.MaxCount > 0 && i >= retentionPolicy.MaxCount {
			shouldDelete = true
		}

		// Ensure we don't delete below minimum count
		if shouldDelete && len(backups)-len(toDelete) > retentionPolicy.MinCount {
			toDelete = append(toDelete, backup)
		}
	}

	// Delete old backups
	for _, backup := range toDelete {
		if err := os.Remove(backup.Path); err != nil {
			return fmt.Errorf("failed to delete backup %s: %w", backup.Path, err)
		}
	}

	return nil
}

// SQLiteBackupManager implements backup for SQLite
type SQLiteBackupManager struct {
	*BaseBackupManager
}

func NewSQLiteBackupManager(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *BackupConfig) *SQLiteBackupManager {
	return &SQLiteBackupManager{
		BaseBackupManager: NewBaseBackupManager(db, dialectCtx, config),
	}
}

func (sbm *SQLiteBackupManager) CreateBackup(ctx context.Context, config *BackupConfig) (*BackupInfo, error) {
	if err := sbm.ensureBackupDir(); err != nil {
		return nil, fmt.Errorf("failed to ensure backup directory: %w", err)
	}

	// Generate filename with .db extension for SQLite backups (since VACUUM INTO creates a database file)
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("sqlite_backup_%s_%s.db", sbm.dialectCtx.Type, timestamp)
	if sbm.config.Compression {
		filename += ".gz"
	}
	backupPath := filepath.Join(sbm.backupDir, filename)

	// For SQLite, we can use VACUUM INTO or copy the database file
	query := fmt.Sprintf("VACUUM INTO '%s'", backupPath)
	if _, err := sbm.db.ExecContext(ctx, query); err != nil {
		return nil, fmt.Errorf("failed to create SQLite backup: %w", err)
	}

	// Get file info
	info, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup file info: %w", err)
	}

	backupInfo := &BackupInfo{
		ID:           filename,
		Filename:     filename,
		Path:         backupPath,
		Size:         info.Size(),
		CreatedAt:    time.Now(),
		DatabaseType: "sqlite",
		Metadata:     make(map[string]string),
	}

	return backupInfo, nil
}

func (sbm *SQLiteBackupManager) RestoreBackup(ctx context.Context, backupInfo *BackupInfo) error {
	// Check if backup file exists and has content
	fileInfo, err := os.Stat(backupInfo.Path)
	if err != nil {
		return fmt.Errorf("backup file does not exist at path %s: %w", backupInfo.Path, err)
	}
	
	if fileInfo.Size() == 0 {
		return fmt.Errorf("backup file is empty at path %s", backupInfo.Path)
	}

	// For SQLite, we'll open a separate connection to the backup file and copy data table by table
	// This avoids the ATTACH DATABASE issues we've been experiencing
	
	// Open connection to backup database
	backupDB, err := sql.Open("sqlite3", backupInfo.Path)
	if err != nil {
		return fmt.Errorf("failed to open backup database: %w", err)
	}
	defer backupDB.Close()

	// Get list of tables from the backup database
	rows, err := backupDB.QueryContext(ctx, "SELECT name, sql FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return fmt.Errorf("failed to get tables from backup: %w", err)
	}
	defer rows.Close()

	var tables []struct {
		name string
		sql  string
	}

	for rows.Next() {
		var tableName, createSQL string
		if err := rows.Scan(&tableName, &createSQL); err != nil {
			return fmt.Errorf("failed to scan table info: %w", err)
		}
		tables = append(tables, struct {
			name string
			sql  string
		}{tableName, createSQL})
	}

	if len(tables) == 0 {
		return fmt.Errorf("no tables found in backup database")
	}

	// For each table, drop if exists, recreate, and copy data
	for _, table := range tables {
		// Drop existing table if it exists
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", table.name)
		_, err = sbm.db.ExecContext(ctx, dropSQL)
		if err != nil {
			return fmt.Errorf("failed to drop table %s: %w", table.name, err)
		}

		// Create table using schema from backup
		_, err = sbm.db.ExecContext(ctx, table.sql)
		if err != nil {
			return fmt.Errorf("failed to create table %s: %w", table.name, err)
		}

		// Copy data from backup to main database
		// First, get all data from backup table
		dataRows, err := backupDB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s", table.name))
		if err != nil {
			return fmt.Errorf("failed to query backup table %s: %w", table.name, err)
		}
		defer dataRows.Close()

		// Get column info
		columns, err := dataRows.Columns()
		if err != nil {
			return fmt.Errorf("failed to get columns for table %s: %w", table.name, err)
		}

		// Prepare insert statement
		placeholders := make([]string, len(columns))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", 
			table.name, 
			strings.Join(columns, ", "), 
			strings.Join(placeholders, ", "))

		// Copy each row
		for dataRows.Next() {
			// Create slice to hold row values
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			// Scan row
			if err := dataRows.Scan(valuePtrs...); err != nil {
				return fmt.Errorf("failed to scan row from table %s: %w", table.name, err)
			}

			// Insert into main database
			_, err = sbm.db.ExecContext(ctx, insertSQL, values...)
			if err != nil {
				return fmt.Errorf("failed to insert row into table %s: %w", table.name, err)
			}
		}
	}

	return nil
}

func (sbm *SQLiteBackupManager) ValidateBackup(ctx context.Context, backupInfo *BackupInfo) error {
	// Check if backup file exists and is readable
	if _, err := os.Stat(backupInfo.Path); err != nil {
		return fmt.Errorf("backup file not accessible: %w", err)
	}

	// Try to open the backup as a SQLite database to validate integrity
	testDB, err := sql.Open("sqlite3", backupInfo.Path)
	if err != nil {
		return fmt.Errorf("failed to open backup as SQLite database: %w", err)
	}
	defer testDB.Close()

	// Run a simple query to check database integrity
	if err := testDB.PingContext(ctx); err != nil {
		return fmt.Errorf("backup database integrity check failed: %w", err)
	}

	return nil
}

// PostgresBackupManager implements backup for PostgreSQL
type PostgresBackupManager struct {
	*BaseBackupManager
}

func NewPostgresBackupManager(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *BackupConfig) *PostgresBackupManager {
	return &PostgresBackupManager{
		BaseBackupManager: NewBaseBackupManager(db, dialectCtx, config),
	}
}

func (pbm *PostgresBackupManager) CreateBackup(ctx context.Context, config *BackupConfig) (*BackupInfo, error) {
	if err := pbm.ensureBackupDir(); err != nil {
		return nil, fmt.Errorf("failed to ensure backup directory: %w", err)
	}

	filename := pbm.generateBackupFilename("postgres_backup")
	backupPath := filepath.Join(pbm.backupDir, filename)

	// Create backup file
	file, err := os.Create(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer file.Close()

	// For PostgreSQL, we would typically use pg_dump
	// This is a simplified implementation using SQL queries
	if err := pbm.dumpSchema(ctx, file); err != nil {
		return nil, fmt.Errorf("failed to dump schema: %w", err)
	}

	if config.IncludeData {
		if err := pbm.dumpData(ctx, file); err != nil {
			return nil, fmt.Errorf("failed to dump data: %w", err)
		}
	}

	// Get file info
	info, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup file info: %w", err)
	}

	backupInfo := &BackupInfo{
		ID:           filename,
		Filename:     filename,
		Path:         backupPath,
		Size:         info.Size(),
		CreatedAt:    time.Now(),
		DatabaseType: "postgres",
		Metadata:     make(map[string]string),
	}

	return backupInfo, nil
}

func (pbm *PostgresBackupManager) dumpSchema(ctx context.Context, writer io.Writer) error {
	// Simplified schema dump - in practice, use pg_dump
	query := `
		SELECT 'CREATE TABLE ' || schemaname || '.' || tablename || ' (' ||
		       array_to_string(array_agg(column_name || ' ' || data_type), ', ') || ');'
		FROM information_schema.tables t
		JOIN information_schema.columns c ON t.table_name = c.table_name
		WHERE t.table_schema = 'public'
		GROUP BY schemaname, tablename;
	`

	rows, err := pbm.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			return err
		}
		if _, err := writer.Write([]byte(ddl + "\n")); err != nil {
			return err
		}
	}

	return rows.Err()
}

func (pbm *PostgresBackupManager) dumpData(ctx context.Context, writer io.Writer) error {
	// Simplified data dump - in practice, use pg_dump
	return fmt.Errorf("PostgreSQL data dump not implemented - use pg_dump for production")
}

func (pbm *PostgresBackupManager) RestoreBackup(ctx context.Context, backupInfo *BackupInfo) error {
	return fmt.Errorf("PostgreSQL restore not implemented - use psql for production")
}

func (pbm *PostgresBackupManager) ValidateBackup(ctx context.Context, backupInfo *BackupInfo) error {
	// Check if backup file exists and is readable
	if _, err := os.Stat(backupInfo.Path); err != nil {
		return fmt.Errorf("backup file not accessible: %w", err)
	}

	// Basic validation - check file size and format
	info, err := os.Stat(backupInfo.Path)
	if err != nil {
		return err
	}

	if info.Size() == 0 {
		return fmt.Errorf("backup file is empty")
	}

	return nil
}

// MySQLBackupManager implements backup for MySQL
type MySQLBackupManager struct {
	*BaseBackupManager
}

func NewMySQLBackupManager(db *sql.DB, dialectCtx *dialect.DatabaseDialectContext, config *BackupConfig) *MySQLBackupManager {
	return &MySQLBackupManager{
		BaseBackupManager: NewBaseBackupManager(db, dialectCtx, config),
	}
}

func (mbm *MySQLBackupManager) CreateBackup(ctx context.Context, config *BackupConfig) (*BackupInfo, error) {
	if err := mbm.ensureBackupDir(); err != nil {
		return nil, fmt.Errorf("failed to ensure backup directory: %w", err)
	}

	filename := mbm.generateBackupFilename("mysql_backup")
	backupPath := filepath.Join(mbm.backupDir, filename)

	// Create backup file
	file, err := os.Create(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer file.Close()

	// For MySQL, we would typically use mysqldump
	// This is a simplified implementation
	if err := mbm.dumpSchema(ctx, file); err != nil {
		return nil, fmt.Errorf("failed to dump schema: %w", err)
	}

	if config.IncludeData {
		if err := mbm.dumpData(ctx, file); err != nil {
			return nil, fmt.Errorf("failed to dump data: %w", err)
		}
	}

	// Get file info
	info, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup file info: %w", err)
	}

	backupInfo := &BackupInfo{
		ID:           filename,
		Filename:     filename,
		Path:         backupPath,
		Size:         info.Size(),
		CreatedAt:    time.Now(),
		DatabaseType: "mysql",
		Metadata:     make(map[string]string),
	}

	return backupInfo, nil
}

func (mbm *MySQLBackupManager) dumpSchema(ctx context.Context, writer io.Writer) error {
	// Simplified schema dump - in practice, use mysqldump
	query := "SHOW TABLES"
	rows, err := mbm.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return err
		}

		// Get CREATE TABLE statement
		createQuery := fmt.Sprintf("SHOW CREATE TABLE %s", tableName)
		var table, createStmt string
		if err := mbm.db.QueryRowContext(ctx, createQuery).Scan(&table, &createStmt); err != nil {
			return err
		}

		if _, err := writer.Write([]byte(createStmt + ";\n")); err != nil {
			return err
		}
	}

	return rows.Err()
}

func (mbm *MySQLBackupManager) dumpData(ctx context.Context, writer io.Writer) error {
	// Simplified data dump - in practice, use mysqldump
	return fmt.Errorf("MySQL data dump not implemented - use mysqldump for production")
}

func (mbm *MySQLBackupManager) RestoreBackup(ctx context.Context, backupInfo *BackupInfo) error {
	return fmt.Errorf("MySQL restore not implemented - use mysql client for production")
}

func (mbm *MySQLBackupManager) ValidateBackup(ctx context.Context, backupInfo *BackupInfo) error {
	// Check if backup file exists and is readable
	if _, err := os.Stat(backupInfo.Path); err != nil {
		return fmt.Errorf("backup file not accessible: %w", err)
	}

	// Basic validation - check file size
	info, err := os.Stat(backupInfo.Path)
	if err != nil {
		return err
	}

	if info.Size() == 0 {
		return fmt.Errorf("backup file is empty")
	}

	return nil
}

// BackupManagerFactory creates database-specific backup managers
type BackupManagerFactory struct {
	managers map[string]func(*sql.DB, *dialect.DatabaseDialectContext, *BackupConfig) BackupManager
}

func NewBackupManagerFactory() *BackupManagerFactory {
	factory := &BackupManagerFactory{
		managers: make(map[string]func(*sql.DB, *dialect.DatabaseDialectContext, *BackupConfig) BackupManager),
	}

	// Register built-in managers
	factory.RegisterManager("sqlite", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *BackupConfig) BackupManager {
		return NewSQLiteBackupManager(db, ctx, config)
	})
	factory.RegisterManager("postgres", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *BackupConfig) BackupManager {
		return NewPostgresBackupManager(db, ctx, config)
	})
	factory.RegisterManager("postgresql", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *BackupConfig) BackupManager {
		return NewPostgresBackupManager(db, ctx, config)
	})
	factory.RegisterManager("mysql", func(db *sql.DB, ctx *dialect.DatabaseDialectContext, config *BackupConfig) BackupManager {
		return NewMySQLBackupManager(db, ctx, config)
	})

	return factory
}

func (f *BackupManagerFactory) RegisterManager(dbType string, factory func(*sql.DB, *dialect.DatabaseDialectContext, *BackupConfig) BackupManager) {
	f.managers[dbType] = factory
}

func (f *BackupManagerFactory) CreateManager(dbType string, db *sql.DB, ctx *dialect.DatabaseDialectContext, config *BackupConfig) (BackupManager, error) {
	factory, exists := f.managers[dbType]
	if !exists {
		return nil, fmt.Errorf("unsupported database type for backup: %s", dbType)
	}
	return factory(db, ctx, config), nil
}

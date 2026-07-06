/**
 * @file manager_test.go
 * @description Unit tests for backup management components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/backup/manager_test.go
 */

package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/dialect"
	_ "github.com/mattn/go-sqlite3"
)

func TestNewBackupManagerFactory(t *testing.T) {
	factory := NewBackupManagerFactory()

	if factory == nil {
		t.Fatal("NewBackupManagerFactory returned nil")
	}
}

func TestBackupManagerFactory_CreateSQLite(t *testing.T) {
	factory := NewBackupManagerFactory()

	config := &BackupConfig{
		BackupDir:     "./test-backups",
		Compression:   true,
		IncludeData:   true,
		IncludeSchema: true,
	}

	// Create a test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()

	dialectCtx := dialect.NewDatabaseDialectContext("sqlite", "3.0")
	manager, err := factory.CreateManager("sqlite", db, dialectCtx, config)
	if err != nil {
		t.Fatalf("CreateManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("CreateManager returned nil manager")
	}

	// Check that it's the correct type
	if _, ok := manager.(*SQLiteBackupManager); !ok {
		t.Errorf("expected SQLiteBackupManager, got %T", manager)
	}
}

func TestBackupManagerFactory_CreatePostgreSQL(t *testing.T) {
	factory := NewBackupManagerFactory()

	config := &BackupConfig{
		BackupDir:     "./test-backups",
		Compression:   true,
		IncludeData:   true,
		IncludeSchema: true,
	}

	dialectCtx := dialect.NewDatabaseDialectContext("postgres", "15.0")
	manager, err := factory.CreateManager("postgres", nil, dialectCtx, config)
	if err != nil {
		t.Fatalf("CreateManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("CreateManager returned nil manager")
	}

	// Check that it's the correct type
	if _, ok := manager.(*PostgresBackupManager); !ok {
		t.Errorf("expected PostgresBackupManager, got %T", manager)
	}
}

func TestBackupManagerFactory_CreateMySQL(t *testing.T) {
	factory := NewBackupManagerFactory()

	config := &BackupConfig{
		BackupDir:     "./test-backups",
		Compression:   true,
		IncludeData:   true,
		IncludeSchema: true,
	}

	dialectCtx := dialect.NewDatabaseDialectContext("mysql", "8.0")
	manager, err := factory.CreateManager("mysql", nil, dialectCtx, config)
	if err != nil {
		t.Fatalf("CreateManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("CreateManager returned nil manager")
	}

	// Check that it's the correct type
	if _, ok := manager.(*MySQLBackupManager); !ok {
		t.Errorf("expected MySQLBackupManager, got %T", manager)
	}
}

func TestBackupManagerFactory_CreateUnsupported(t *testing.T) {
	factory := NewBackupManagerFactory()

	config := &BackupConfig{
		BackupDir: "./test-backups",
	}

	dialectCtx := dialect.NewDatabaseDialectContext("unsupported", "1.0")
	manager, err := factory.CreateManager("unsupported", nil, dialectCtx, config)
	if err == nil {
		t.Error("expected error for unsupported database type")
	}

	if manager != nil {
		t.Error("expected nil manager for unsupported database type")
	}
}

func TestSQLiteBackupManager_CreateBackup(t *testing.T) {
	// Create temporary directory for backups
	tmpDir := t.TempDir()

	// Create test database
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()

	// Create test table and data
	_, err = db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	_, err = db.Exec("INSERT INTO test_table (name) VALUES ('test1'), ('test2')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	config := &BackupConfig{
		BackupDir:     filepath.Join(tmpDir, "backups"),
		Compression:   false, // Disable compression for easier testing
		IncludeData:   true,
		IncludeSchema: true,
	}

	dialectCtx := &dialect.DatabaseDialectContext{}
	manager := NewSQLiteBackupManager(db, dialectCtx, config)

	ctx := context.Background()
	backupInfo, err := manager.CreateBackup(ctx, config)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	if backupInfo == nil {
		t.Fatal("CreateBackup returned nil backup info")
	}

	// Check that backup file exists
	if _, err := os.Stat(backupInfo.Path); os.IsNotExist(err) {
		t.Errorf("backup file does not exist: %s", backupInfo.Path)
	}

	// Check backup info
	if backupInfo.Size <= 0 {
		t.Error("backup size should be greater than 0")
	}

	if backupInfo.CreatedAt.IsZero() {
		t.Error("backup created time should be set")
	}

	if backupInfo.DatabaseType != "sqlite" {
		t.Errorf("expected database type 'sqlite', got %s", backupInfo.DatabaseType)
	}
}

func TestSQLiteBackupManager_RestoreBackup(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create original database
	originalDBPath := filepath.Join(tmpDir, "original.db")
	originalDB, err := sql.Open("sqlite3", originalDBPath)
	if err != nil {
		t.Fatalf("failed to create original database: %v", err)
	}

	// Create test table and data
	_, err = originalDB.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	_, err = originalDB.Exec("INSERT INTO test_table (name) VALUES ('original1'), ('original2')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}
	originalDB.Close()

	// Create backup
	config := &BackupConfig{
		BackupDir:     filepath.Join(tmpDir, "backups"),
		Compression:   false,
		IncludeData:   true,
		IncludeSchema: true,
	}

	originalDB, err = sql.Open("sqlite3", originalDBPath)
	if err != nil {
		t.Fatalf("failed to reopen original database: %v", err)
	}
	defer originalDB.Close()

	dialectCtx := &dialect.DatabaseDialectContext{}
	manager := NewSQLiteBackupManager(originalDB, dialectCtx, config)

	ctx := context.Background()
	backupInfo, err := manager.CreateBackup(ctx, config)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Create new database to restore to
	restoreDBPath := filepath.Join(tmpDir, "restored.db")
	restoreDB, err := sql.Open("sqlite3", restoreDBPath)
	if err != nil {
		t.Fatalf("failed to create restore database: %v", err)
	}
	defer restoreDB.Close()

	restoreManager := NewSQLiteBackupManager(restoreDB, dialectCtx, config)

	// Restore backup
	err = restoreManager.RestoreBackup(ctx, backupInfo)
	if err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	// Verify restored data
	rows, err := restoreDB.Query("SELECT name FROM test_table ORDER BY name")
	if err != nil {
		t.Fatalf("failed to query restored data: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		names = append(names, name)
	}

	expectedNames := []string{"original1", "original2"}
	if len(names) != len(expectedNames) {
		t.Errorf("expected %d names, got %d", len(expectedNames), len(names))
	}

	for i, expected := range expectedNames {
		if i >= len(names) || names[i] != expected {
			t.Errorf("expected name %s at index %d, got %s", expected, i, names[i])
		}
	}
}

func TestSQLiteBackupManager_ValidateBackup(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create test database
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()

	// Create test table
	_, err = db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	config := &BackupConfig{
		BackupDir:     filepath.Join(tmpDir, "backups"),
		Compression:   false,
		IncludeData:   true,
		IncludeSchema: true,
	}

	dialectCtx := &dialect.DatabaseDialectContext{}
	manager := NewSQLiteBackupManager(db, dialectCtx, config)

	ctx := context.Background()

	// Create backup
	backupInfo, err := manager.CreateBackup(ctx, config)
	if err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Validate backup
	err = manager.ValidateBackup(ctx, backupInfo)
	if err != nil {
		t.Fatalf("ValidateBackup failed: %v", err)
	}

	// Test with invalid backup file
	invalidPath := filepath.Join(tmpDir, "invalid.db")
	err = os.WriteFile(invalidPath, []byte("invalid content"), 0644)
	if err != nil {
		t.Fatalf("failed to create invalid file: %v", err)
	}

	invalidBackupInfo := &BackupInfo{
		Path: invalidPath,
	}

	err = manager.ValidateBackup(ctx, invalidBackupInfo)
	if err == nil {
		t.Error("expected error for invalid backup file")
	}
}

func TestBaseBackupManager_ListBackups(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")

	config := &BackupConfig{
		BackupDir: backupDir,
	}

	dialectCtx := &dialect.DatabaseDialectContext{
		Type: "sqlite3",
	}

	manager := &BaseBackupManager{
		config:     config,
		backupDir:  backupDir,
		dialectCtx: dialectCtx,
	}

	ctx := context.Background()

	// Initially should be empty
	backups, err := manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != 0 {
		t.Errorf("expected 0 backups, got %d", len(backups))
	}

	// Create some test backup files
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		t.Fatalf("failed to create backup directory: %v", err)
	}

	testFiles := []string{
		"backup_20240101_120000.db",
		"backup_20240102_120000.db",
		"backup_20240103_120000.db",
	}

	for _, filename := range testFiles {
		filePath := filepath.Join(backupDir, filename)
		err = os.WriteFile(filePath, []byte("test backup content"), 0644)
		if err != nil {
			t.Fatalf("failed to create test backup file: %v", err)
		}
	}

	// List backups again
	backups, err = manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	if len(backups) != len(testFiles) {
		t.Errorf("expected %d backups, got %d", len(testFiles), len(backups))
	}

	// Check that backups are sorted by creation time (newest first)
	for i := 1; i < len(backups); i++ {
		if backups[i-1].CreatedAt.Before(backups[i].CreatedAt) {
			t.Error("backups should be sorted by creation time (newest first)")
		}
	}
}

func TestBaseBackupManager_CleanupOldBackups(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")

	config := &BackupConfig{
		BackupDir: backupDir,
	}

	retentionPolicy := &RetentionPolicy{
		MaxAge:   24 * time.Hour, // 1 day
		MaxCount: 2,
		MinCount: 1,
	}

	dialectCtx := &dialect.DatabaseDialectContext{
		Type: "sqlite3",
	}

	manager := &BaseBackupManager{
		config:     config,
		backupDir:  backupDir,
		dialectCtx: dialectCtx,
	}

	ctx := context.Background()

	// Create backup directory
	err := os.MkdirAll(backupDir, 0755)
	if err != nil {
		t.Fatalf("failed to create backup directory: %v", err)
	}

	// Create test backup files with different ages
	now := time.Now()
	testFiles := []struct {
		name string
		age  time.Duration
	}{
		{"backup_old1.db", 48 * time.Hour},   // 2 days old
		{"backup_old2.db", 36 * time.Hour},   // 1.5 days old
		{"backup_recent.db", 12 * time.Hour}, // 12 hours old
		{"backup_new.db", 1 * time.Hour},     // 1 hour old
	}

	for _, file := range testFiles {
		filePath := filepath.Join(backupDir, file.name)
		err = os.WriteFile(filePath, []byte("test backup content"), 0644)
		if err != nil {
			t.Fatalf("failed to create test backup file: %v", err)
		}

		// Set file modification time
		modTime := now.Add(-file.age)
		err = os.Chtimes(filePath, modTime, modTime)
		if err != nil {
			t.Fatalf("failed to set file time: %v", err)
		}
	}

	// Run cleanup
	err = manager.CleanupOldBackups(ctx, retentionPolicy)
	if err != nil {
		t.Fatalf("CleanupOldBackups failed: %v", err)
	}

	// Check remaining files
	backups, err := manager.ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}

	// Should keep only the 2 most recent backups (within retention policy)
	if len(backups) > retentionPolicy.MaxCount {
		t.Errorf("expected at most %d backups after cleanup, got %d",
			retentionPolicy.MaxCount, len(backups))
	}

	// Check that old files are removed
	for _, file := range testFiles {
		filePath := filepath.Join(backupDir, file.name)
		if _, err := os.Stat(filePath); err == nil {
			// File still exists, check if it should
			if file.age > retentionPolicy.MaxAge {
				t.Errorf("old file %s should have been removed", file.name)
			}
		}
	}
}

func TestRetentionPolicy_ShouldRetain(t *testing.T) {
	policy := &RetentionPolicy{
		MaxAge:   24 * time.Hour,
		MaxCount: 5,
		MinCount: 2,
	}

	now := time.Now()

	tests := []struct {
		name        string
		createdAt   time.Time
		totalCount  int
		currentRank int // 0 = newest, 1 = second newest, etc.
		expected    bool
	}{
		{
			name:        "recent backup within max age",
			createdAt:   now.Add(-12 * time.Hour),
			totalCount:  3,
			currentRank: 0,
			expected:    true,
		},
		{
			name:        "old backup beyond max age but within min backups",
			createdAt:   now.Add(-48 * time.Hour),
			totalCount:  3,
			currentRank: 1,
			expected:    true,
		},
		{
			name:        "old backup beyond max age and beyond min backups",
			createdAt:   now.Add(-48 * time.Hour),
			totalCount:  6,
			currentRank: 5,
			expected:    false,
		},
		{
			name:        "backup within max backups limit",
			createdAt:   now.Add(-36 * time.Hour),
			totalCount:  4,
			currentRank: 3,
			expected:    true,
		},
		{
			name:        "backup beyond max backups limit",
			createdAt:   now.Add(-12 * time.Hour),
			totalCount:  7,
			currentRank: 6,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := policy.ShouldRetain(tt.createdAt, tt.totalCount, tt.currentRank)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func BenchmarkSQLiteBackupManager_CreateBackup(b *testing.B) {
	// Create temporary directory
	tmpDir := b.TempDir()

	// Create test database
	dbPath := filepath.Join(tmpDir, "bench.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		b.Fatalf("failed to create test database: %v", err)
	}
	defer db.Close()

	// Create test table with some data
	_, err = db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, data TEXT)")
	if err != nil {
		b.Fatalf("failed to create test table: %v", err)
	}

	// Insert test data
	for i := 0; i < 1000; i++ {
		_, err = db.Exec("INSERT INTO test_table (data) VALUES (?)", "test data "+string(rune(i)))
		if err != nil {
			b.Fatalf("failed to insert test data: %v", err)
		}
	}

	config := &BackupConfig{
		BackupDir:     filepath.Join(tmpDir, "backups"),
		Compression:   false,
		IncludeData:   true,
		IncludeSchema: true,
	}

	dialectCtx := dialect.NewDatabaseDialectContext("sqlite", "3.0")

	manager := NewSQLiteBackupManager(db, dialectCtx, config)

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := manager.CreateBackup(ctx, config)
		if err != nil {
			b.Fatalf("CreateBackup failed: %v", err)
		}
	}
}

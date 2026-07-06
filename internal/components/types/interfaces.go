/**
 * @file interfaces.go
 * @description Interface definitions for migration engine components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/engine/interfaces.go
 */

package types

import (
	"context"
)

// MigrationLoader defines the interface for loading migrations
type MigrationLoader interface {
	LoadMigrations(path string) ([]Migration, error)
	LoadMigration(path string) (*Migration, error)
	ValidateFile(path string) error
}

// BackupManager defines the interface for managing backups
type BackupManager interface {
	CreateBackup(ctx context.Context, name string) error
	RestoreBackup(ctx context.Context, name string) error
	ListBackups(ctx context.Context) ([]string, error)
	DeleteBackup(ctx context.Context, name string) error
}

// HealthChecker defines the interface for health checking
type HealthChecker interface {
	CheckHealth(ctx context.Context) error
	GetHealthStatus(ctx context.Context) (map[string]interface{}, error)
}

// Monitor defines the interface for monitoring migration operations
type Monitor interface {
	RecordMigrationStart(ctx context.Context, migrationID string) error
	RecordMigrationEnd(ctx context.Context, migrationID string, success bool, duration int64) error
	GetMetrics(ctx context.Context) (map[string]interface{}, error)
}

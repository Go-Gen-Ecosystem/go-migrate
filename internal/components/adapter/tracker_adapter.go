/**
 * @file tracker_adapter.go
 * @description Adapter to bridge between tracker.MigrationTracker and types.MigrationTracker interfaces
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/adapters/tracker_adapter.go
 */

package adapters

import (
	"context"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"gorm.io/gorm"
)

// TrackerAdapter adapts tracker.MigrationTracker to types.MigrationTracker
type TrackerAdapter struct {
	tracker tracker.MigrationTracker
	db      *gorm.DB
	dryRun  bool
}

// NewTrackerAdapter creates a new tracker adapter
func NewTrackerAdapter(tracker tracker.MigrationTracker, db *gorm.DB) types.MigrationTracker {
	return &TrackerAdapter{
		tracker: tracker,
		db:      db,
		dryRun:  false,
	}
}

// SetDryRun sets the dry run mode for the tracker adapter
func (a *TrackerAdapter) SetDryRun(dryRun bool) {
	a.dryRun = dryRun
}

// GetExecutedMigrations returns executed migrations as MigrationRecord
func (a *TrackerAdapter) GetExecutedMigrations(ctx context.Context) ([]types.MigrationRecord, error) {
	// In dry-run mode, return empty list since no migrations have been executed
	if a.dryRun {
		return []types.MigrationRecord{}, nil
	}

	migrations, err := a.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	records := make([]types.MigrationRecord, len(migrations))
	for i, migration := range migrations {
		var executedAt time.Time
		if migration.ExecutedAt != nil {
			executedAt = *migration.ExecutedAt
		}

		records[i] = types.MigrationRecord{
			ID:          migration.ID,
			Version:     migration.Version,
			Description: migration.Description,
			Type:        string(migration.Type),
			ExecutedAt:  executedAt,
			Checksum:    migration.Checksum,
			CanRollback: migration.CanRollback,
			RollbackSQL: migration.RollbackSQL,
		}
	}

	return records, nil
}

// GetFailedMigrations returns failed migrations as MigrationRecord
func (a *TrackerAdapter) GetFailedMigrations(ctx context.Context) ([]types.MigrationRecord, error) {
	// In dry-run mode, return empty list since no migrations have been executed
	if a.dryRun {
		return []types.MigrationRecord{}, nil
	}

	// Query the database for failed migrations using the tracker's MigrationRecord type
	var trackerRecords []tracker.MigrationRecord
	err := a.db.WithContext(ctx).Where("state = ?", "failed").Find(&trackerRecords).Error
	if err != nil {
		return nil, err
	}

	// Convert tracker.MigrationRecord to types.MigrationRecord
	records := make([]types.MigrationRecord, len(trackerRecords))
	for i, record := range trackerRecords {
		var executedAt time.Time
		if record.ExecutedAt != nil {
			executedAt = *record.ExecutedAt
		}

		records[i] = types.MigrationRecord{
			ID:            record.ID,
			Version:       record.Version,
			Description:   record.Description,
			Type:          record.Type,
			Script:        record.Content,
			Checksum:      record.Checksum,
			ExecutedAt:    executedAt,
			ExecutionTime: record.ExecutionTime,
			Success:       record.State == "applied",
			ErrorMessage:  record.ErrorMessage,
			CanRollback:   record.CanRollback,
			RollbackSQL:   record.RollbackSQL,
		}
	}

	return records, nil
}

// RecordMigrationSuccess records a successful migration
func (a *TrackerAdapter) RecordMigrationSuccess(ctx context.Context, migration types.MigrationRecord, executionTime int64) error {
	// Convert MigrationRecord to Migration
	now := time.Now()
	migrationObj := &types.Migration{
		ID:          migration.ID,
		Version:     migration.Version,
		Description: migration.Description,
		Type:        types.MigrationType(migration.Type),
		State:       "applied",
		ExecutedAt:  &now,
		Checksum:    migration.Checksum,
		CanRollback: migration.CanRollback,
		RollbackSQL: migration.RollbackSQL,
	}

	return a.tracker.RecordMigration(ctx, migrationObj)
}

// RecordMigrationFailure records a failed migration
func (a *TrackerAdapter) RecordMigrationFailure(ctx context.Context, migration types.MigrationRecord, errorMsg string) error {
	// Convert MigrationRecord to Migration
	now := time.Now()
	migrationObj := &types.Migration{
		ID:           migration.ID,
		Version:      migration.Version,
		Description:  migration.Description,
		Type:         types.MigrationType(migration.Type),
		State:        "failed",
		ExecutedAt:   &now,
		Checksum:     migration.Checksum,
		ErrorMessage: errorMsg,
		CanRollback:  migration.CanRollback,
		RollbackSQL:  migration.RollbackSQL,
	}

	return a.tracker.RecordMigration(ctx, migrationObj)
}

// RemoveMigration removes a migration record
func (a *TrackerAdapter) RemoveMigration(ctx context.Context, migrationID string) error {
	return a.tracker.RemoveMigration(ctx, migrationID)
}

// IsLocked checks if migrations are locked
func (a *TrackerAdapter) IsLocked(ctx context.Context) (bool, error) {
	// Check if there's a lock record in the database
	var count int64
	err := a.db.WithContext(ctx).Table("migration_locks").Where("locked = ?", true).Count(&count).Error
	return count > 0, err
}

// AcquireLock acquires a migration lock
func (a *TrackerAdapter) AcquireLock(ctx context.Context) error {
	// Insert or update lock record
	return a.db.WithContext(ctx).Exec("INSERT INTO migration_locks (locked, locked_at) VALUES (?, ?) ON CONFLICT DO UPDATE SET locked = ?, locked_at = ?",
		true, time.Now(), true, time.Now()).Error
}

// ReleaseLock releases a migration lock
func (a *TrackerAdapter) ReleaseLock(ctx context.Context) error {
	// Update lock record
	return a.db.WithContext(ctx).Exec("UPDATE migration_locks SET locked = ?, locked_at = ?", false, time.Now()).Error
}

// EnsureSchemaTable ensures the schema table exists
func (a *TrackerAdapter) EnsureSchemaTable(ctx context.Context) error {
	// Skip table creation in dry run mode
	if a.dryRun {
		return nil
	}

	// Create migration tables if they don't exist using the tracker's MigrationRecord type
	// which has the proper State field and table structure
	err := a.db.WithContext(ctx).AutoMigrate(&tracker.MigrationRecord{}, &tracker.ChangelogRecord{})

	return err
}

// GetCurrentVersion returns the current database version
func (a *TrackerAdapter) GetCurrentVersion(ctx context.Context) (string, error) {
	var record tracker.MigrationRecord
	err := a.db.WithContext(ctx).Where("state = ?", "applied").Order("executed_at DESC").First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "0.0.0", nil
		}
		return "", err
	}
	return record.Version, nil
}

// IsMigrationApplied checks if a migration is applied
func (a *TrackerAdapter) IsMigrationApplied(ctx context.Context, id string) (bool, error) {
	// In dry-run mode, no migrations are applied
	if a.dryRun {
		return false, nil
	}

	return a.tracker.IsMigrationApplied(ctx, id)
}

// GetMigration retrieves a migration by ID
func (a *TrackerAdapter) GetMigration(ctx context.Context, id string) (*types.Migration, error) {
	return a.tracker.GetMigration(ctx, id)
}

// GetMigrationByVersion retrieves a migration by version
func (a *TrackerAdapter) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	return a.tracker.GetMigrationByVersion(ctx, version)
}

// GetAppliedMigrations returns all applied migrations
func (a *TrackerAdapter) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	return a.tracker.GetAppliedMigrations(ctx)
}

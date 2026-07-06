/**
 * @file tracker.go
 * @description Migration tracking implementations for changelog management
 * @author Admilson B. F. Cossa
 * @date 2025-01-22
 * @path pkg/core/database/migrations/tracker.go
 */

package tracker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gorm.io/gorm"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// MigrationTracker interface defines methods for tracking migration state.
//
//nolint:interfacebloat // Cohesive migration-state store contract (record,
// query, changelog and schema-table setup) implemented as one transactional
// unit by DatabaseMigrationTracker.
type MigrationTracker interface {
	RecordMigration(ctx context.Context, migration *types.Migration) error
	RemoveMigration(ctx context.Context, migrationID string) error
	GetMigration(ctx context.Context, id string) (*types.Migration, error)
	GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error)
	GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error)
	GetPendingMigrations(ctx context.Context) ([]*types.Migration, error)
	IsMigrationApplied(ctx context.Context, id string) (bool, error)
	GetMigrationHistory(ctx context.Context, limit, offset int) ([]*types.Migration, error)
	RecordChangelog(ctx context.Context, entry *ChangelogEntry) error
	GetChangelog(ctx context.Context, limit, offset int) ([]*ChangelogEntry, error)
	
	// Drift detection and validation methods
	ValidateChecksum(ctx context.Context, migration *types.Migration) (*ChecksumValidationResult, error)
	DetectDrift(ctx context.Context) ([]*DriftDetectionResult, error)
	ValidateIdempotency(ctx context.Context, migration *types.Migration) (*IdempotencyValidationResult, error)
}

// DatabaseMigrationTracker implements MigrationTracker using database storage
type DatabaseMigrationTracker struct {
	db        *gorm.DB
	tableName string
}

// InMemoryMigrationTracker implements MigrationTracker using in-memory storage
type InMemoryMigrationTracker struct {
	migrations map[string]*types.Migration
	changelog  []*ChangelogEntry
}

// FileMigrationTracker implements MigrationTracker using file storage
type FileMigrationTracker struct {
	filePath string
	data     *TrackerData
}

// TrackerData represents the structure for file-based tracking
type TrackerData struct {
	Migrations map[string]*types.Migration   `json:"migrations"`
	Changelog  []*ChangelogEntry       `json:"changelog"`
	Metadata   map[string]interface{}  `json:"metadata"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

// MigrationRecord represents a migration record in the database
type MigrationRecord struct {
	ID              string                 `gorm:"primaryKey;column:id" json:"id"`
	Version         string                 `gorm:"column:version;index" json:"version"`
	Description     string                 `gorm:"column:description" json:"description"`
	Filename        string                 `gorm:"column:filename" json:"filename"`
	Filepath        string                 `gorm:"column:filepath" json:"filepath"`
	Content         string                 `gorm:"column:content;type:text" json:"content"`
	Checksum        string                 `gorm:"column:checksum;index" json:"checksum"`
	Type            string                 `gorm:"column:type" json:"type"`
	Author          string                 `gorm:"column:author" json:"author"`
	ChangesetID     string                 `gorm:"column:changeset_id" json:"changeset_id"`
	State           string                 `gorm:"column:state;index" json:"state"`
	Contexts        string                 `gorm:"column:contexts" json:"contexts"` // JSON array
	Labels          string                 `gorm:"column:labels" json:"labels"`     // JSON array
	DependsOn       string                 `gorm:"column:depends_on" json:"depends_on"` // JSON array
	Preconditions   string                 `gorm:"column:preconditions" json:"preconditions"` // JSON array
	RollbackSQL     string                 `gorm:"column:rollback_sql;type:text" json:"rollback_sql"`
	CanRollback     bool                   `gorm:"column:can_rollback" json:"can_rollback"`
	RunAlways       bool                   `gorm:"column:run_always" json:"run_always"`
	RunOnChange     bool                   `gorm:"column:run_on_change" json:"run_on_change"`
	FailOnError     bool                   `gorm:"column:fail_on_error" json:"fail_on_error"`
	ExecutedAt      *time.Time             `gorm:"column:executed_at" json:"executed_at"`
	RolledBackAt    *time.Time             `gorm:"column:rolled_back_at" json:"rolled_back_at"`
	ExecutionTime   int64                  `gorm:"column:execution_time" json:"execution_time"` // nanoseconds
	ErrorMessage    string                 `gorm:"column:error_message" json:"error_message"`
	Metadata        string                 `gorm:"column:metadata;type:text" json:"metadata"` // JSON
	CreatedAt       time.Time              `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       time.Time              `gorm:"column:updated_at" json:"updated_at"`
}

// ChangelogRecord represents a changelog entry in the database
type ChangelogRecord struct {
	ID            uint                   `gorm:"primaryKey;autoIncrement" json:"id"`
	MigrationID   string                 `gorm:"column:migration_id;index" json:"migration_id"`
	Operation     string                 `gorm:"column:operation" json:"operation"`
	Status        string                 `gorm:"column:status" json:"status"`
	ExecutedBy    string                 `gorm:"column:executed_by" json:"executed_by"`
	ExecutedAt    time.Time              `gorm:"column:executed_at" json:"executed_at"`
	ExecutionTime int64                  `gorm:"column:execution_time" json:"execution_time"`
	ErrorMessage  string                 `gorm:"column:error_message" json:"error_message"`
	Context       string                 `gorm:"column:context;type:text" json:"context"` // JSON
	Metadata      string                 `gorm:"column:metadata;type:text" json:"metadata"` // JSON
	CreatedAt     time.Time              `gorm:"column:created_at" json:"created_at"`
}

// ChangelogEntry represents an internal changelog entry for migration tracking
type ChangelogEntry struct {
	MigrationID   string                 `json:"migration_id"`
	Operation     string                 `json:"operation"`
	Status        string                 `json:"status"`
	ExecutedBy    string                 `json:"executed_by"`
	ExecutedAt    time.Time              `json:"executed_at"`
	ExecutionTime time.Duration          `json:"execution_time"`
	ErrorMessage  string                 `json:"error_message"`
	Context       map[string]interface{} `json:"context"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// ChecksumValidationResult represents the result of checksum validation
type ChecksumValidationResult struct {
	IsValid          bool      `json:"is_valid"`
	ExpectedChecksum string    `json:"expected_checksum"`
	ActualChecksum   string    `json:"actual_checksum"`
	MigrationID      string    `json:"migration_id"`
	ValidatedAt      time.Time `json:"validated_at"`
	Message          string    `json:"message"`
}

// DriftDetectionResult represents detected drift in migration files
type DriftDetectionResult struct {
	MigrationID      string    `json:"migration_id"`
	Version          string    `json:"version"`
	DriftType        string    `json:"drift_type"` // "checksum_mismatch", "file_missing", "content_changed"
	ExpectedChecksum string    `json:"expected_checksum"`
	ActualChecksum   string    `json:"actual_checksum"`
	FilePath         string    `json:"file_path"`
	DetectedAt       time.Time `json:"detected_at"`
	Description      string    `json:"description"`
}

// IdempotencyValidationResult represents the result of idempotency validation
type IdempotencyValidationResult struct {
	IsIdempotent     bool      `json:"is_idempotent"`
	MigrationID      string    `json:"migration_id"`
	PreviousExecution *time.Time `json:"previous_execution"`
	ValidatedAt      time.Time `json:"validated_at"`
	CanRerun         bool      `json:"can_rerun"`
	Message          string    `json:"message"`
}

// TableName returns the table name for MigrationRecord
func (MigrationRecord) TableName() string {
	return "migration_history"
}

// TableName returns the table name for ChangelogRecord
func (ChangelogRecord) TableName() string {
	return "migration_changelog"
}

// Database Tracker Implementation

// NewDatabaseMigrationTracker creates a new database migration tracker
func NewDatabaseMigrationTracker(db *gorm.DB, tableName string) (*DatabaseMigrationTracker, error) {
	return NewDatabaseMigrationTrackerWithDryRun(db, tableName, false)
}

func NewDatabaseMigrationTrackerWithDryRun(db *gorm.DB, tableName string, dryRun bool) (*DatabaseMigrationTracker, error) {
	if tableName == "" {
		tableName = "migration_history"
	}
	
	tracker := &DatabaseMigrationTracker{
		db:        db,
		tableName: tableName,
	}
	
	// Auto-migrate tables and handle errors only if not in dry run mode
	if !dryRun {
		if err := tracker.initializeTables(); err != nil {
			return nil, fmt.Errorf("failed to initialize migration tracker tables: %w", err)
		}
	}
	
	return tracker, nil
}

// initializeTables creates the necessary tables
func (t *DatabaseMigrationTracker) initializeTables() error {
	// Create migration history table with configured table name
	migrationRecord := &MigrationRecord{}
	if err := t.db.Table(t.tableName).AutoMigrate(migrationRecord); err != nil {
		return fmt.Errorf("failed to create migration history table: %w", err)
	}
	
	// Create changelog table
	if err := t.db.AutoMigrate(&ChangelogRecord{}); err != nil {
		return fmt.Errorf("failed to create changelog table: %w", err)
	}
	
	// Add additional indexes for better performance
	if err := t.addPerformanceIndexes(); err != nil {
		return fmt.Errorf("failed to add performance indexes: %w", err)
	}
	
	return nil
}

// addPerformanceIndexes adds composite indexes for better query performance
func (t *DatabaseMigrationTracker) addPerformanceIndexes() error {
	// Composite index for migration queries by state and execution time
	if err := t.db.Exec("CREATE INDEX IF NOT EXISTS idx_migrations_state_executed_at ON " + 
		t.tableName + " (state, executed_at)").Error; err != nil {
		return fmt.Errorf("failed to create state_executed_at index: %w", err)
	}
	
	// Composite index for version uniqueness and state
	if err := t.db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_migrations_version_unique ON " + 
		t.tableName + " (version) WHERE state != 'rolled_back'").Error; err != nil {
		return fmt.Errorf("failed to create version uniqueness index: %w", err)
	}
	
	// Index for checksum-based drift detection
	if err := t.db.Exec("CREATE INDEX IF NOT EXISTS idx_migrations_checksum_state ON " + 
		t.tableName + " (checksum, state)").Error; err != nil {
		return fmt.Errorf("failed to create checksum_state index: %w", err)
	}
	
	// Composite index for changelog queries by migration and timestamp
	if err := t.db.Exec("CREATE INDEX IF NOT EXISTS idx_changelog_migration_executed_at ON " + 
		"migration_changelog (migration_id, executed_at DESC)").Error; err != nil {
		return fmt.Errorf("failed to create changelog migration_executed_at index: %w", err)
	}
	
	// Index for changelog status queries
	if err := t.db.Exec("CREATE INDEX IF NOT EXISTS idx_changelog_status_executed_at ON " + 
		"migration_changelog (status, executed_at DESC)").Error; err != nil {
		return fmt.Errorf("failed to create changelog status_executed_at index: %w", err)
	}
	
	return nil
}

// RecordMigration records a migration execution
func (t *DatabaseMigrationTracker) RecordMigration(ctx context.Context, migration *types.Migration) error {
	record := t.migrationToRecord(migration)
	
	// Use upsert to handle updates with configured table name
	result := t.db.WithContext(ctx).Table(t.tableName).Save(record)
	if result.Error != nil {
		return fmt.Errorf("failed to record migration: %w", result.Error)
	}
	
	// Record changelog entry
	changelog := &ChangelogEntry{
		MigrationID:   migration.ID,
		Operation:     "execute",
		Status:        migration.State,
		ExecutedBy:    migration.Author,
		ExecutedAt:    time.Now(),
		ExecutionTime: migration.ExecutionTime,
		ErrorMessage:  migration.ErrorMessage,
		Context: map[string]interface{}{
			"version":     migration.Version,
			"description": migration.Description,
			"type":        migration.Type,
		},
	}
	
	return t.RecordChangelog(ctx, changelog)
}

// RecordMigrationSuccess records a successful migration execution
func (t *DatabaseMigrationTracker) RecordMigrationSuccess(ctx context.Context, migration types.MigrationRecord, executionTime int64) error {
	now := time.Now()
	migrationObj := &types.Migration{
		ID:            migration.ID,
		Version:       migration.Version,
		Description:   migration.Description,
		Type:          types.MigrationType(migration.Type),
		State:         "applied",
		ExecutedAt:    &now,
		ExecutionTime: time.Duration(executionTime),
		Checksum:      migration.Checksum,
		Script:        migration.Script,
		// Note: types.MigrationRecord doesn't have FileName/FilePath/ExecutedBy fields
		// These would need to be added to types.MigrationRecord if needed
	}

	return t.RecordMigration(ctx, migrationObj)
}

// RecordMigrationFailure records a failed migration execution
func (t *DatabaseMigrationTracker) RecordMigrationFailure(ctx context.Context, migration types.MigrationRecord, errorMsg string) error {
	now := time.Now()
	migrationObj := &types.Migration{
		ID:            migration.ID,
		Version:       migration.Version,
		Description:   migration.Description,
		Type:          types.MigrationType(migration.Type),
		State:         "failed",
		ExecutedAt:    &now,
		Checksum:      migration.Checksum,
		Script:        migration.Script,
		ErrorMessage:  errorMsg,
		// Note: types.MigrationRecord doesn't have FileName/FilePath/ExecutedBy fields
		// These would need to be added to types.MigrationRecord if needed
	}

	return t.RecordMigration(ctx, migrationObj)
}

// GetMigration retrieves a migration by ID
func (t *DatabaseMigrationTracker) GetMigration(ctx context.Context, id string) (*types.Migration, error) {
	var record MigrationRecord
	result := t.db.WithContext(ctx).Table(t.tableName).Where("id = ?", id).First(&record)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("migration not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get migration: %w", result.Error)
	}
	
	return t.recordToMigration(&record)
}

// RemoveMigration removes a migration record from the database
func (t *DatabaseMigrationTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	// Delete the migration record
	result := t.db.WithContext(ctx).Table(t.tableName).Where("id = ?", migrationID).Delete(&MigrationRecord{})
	if result.Error != nil {
		return fmt.Errorf("failed to remove migration: %w", result.Error)
	}
	return nil
}

// GetMigrationByVersion retrieves a migration by version
func (t *DatabaseMigrationTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	var record MigrationRecord
	result := t.db.WithContext(ctx).Table(t.tableName).Where("version = ?", version).First(&record)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("migration not found: %s", version)
		}
		return nil, fmt.Errorf("failed to get migration: %w", result.Error)
	}
	
	return t.recordToMigration(&record)
}

// GetAppliedMigrations retrieves all applied migrations
func (t *DatabaseMigrationTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	var records []MigrationRecord
	result := t.db.WithContext(ctx).Table(t.tableName).Where("state = ?", "applied").Order("executed_at ASC").Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get applied migrations: %w", result.Error)
	}
	
	migrations := make([]*types.Migration, len(records))
	for i, record := range records {
		migration, err := t.recordToMigration(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record to migration: %w", err)
		}
		migrations[i] = migration
	}
	
	return migrations, nil
}

// GetPendingMigrations retrieves all pending migrations
func (t *DatabaseMigrationTracker) GetPendingMigrations(ctx context.Context) ([]*types.Migration, error) {
	var records []MigrationRecord
	result := t.db.WithContext(ctx).Table(t.tableName).Where("state = ?", "pending").Order("version ASC").Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get pending migrations: %w", result.Error)
	}
	
	migrations := make([]*types.Migration, len(records))
	for i, record := range records {
		migration, err := t.recordToMigration(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record to migration: %w", err)
		}
		migrations[i] = migration
	}
	
	return migrations, nil
}

// IsMigrationApplied checks if a migration is applied
func (t *DatabaseMigrationTracker) IsMigrationApplied(ctx context.Context, id string) (bool, error) {
	var count int64
	result := t.db.WithContext(ctx).Table(t.tableName).Where("id = ? AND state = ?", id, "applied").Count(&count)
	if result.Error != nil {
		return false, fmt.Errorf("failed to check migration status: %w", result.Error)
	}
	
	return count > 0, nil
}

// GetMigrationHistory retrieves migration history with pagination
func (t *DatabaseMigrationTracker) GetMigrationHistory(ctx context.Context, limit, offset int) ([]*types.Migration, error) {
	var records []MigrationRecord
	result := t.db.WithContext(ctx).Table(t.tableName).Order("executed_at DESC").Limit(limit).Offset(offset).Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get migration history: %w", result.Error)
	}
	
	migrations := make([]*types.Migration, len(records))
	for i, record := range records {
		migration, err := t.recordToMigration(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record to migration: %w", err)
		}
		migrations[i] = migration
	}
	
	return migrations, nil
}

// RecordChangelog records a changelog entry
func (t *DatabaseMigrationTracker) RecordChangelog(ctx context.Context, entry *ChangelogEntry) error {
	record := t.changelogToRecord(entry)
	
	result := t.db.WithContext(ctx).Create(record)
	if result.Error != nil {
		return fmt.Errorf("failed to record changelog: %w", result.Error)
	}
	
	return nil
}

// GetChangelog retrieves changelog entries
func (t *DatabaseMigrationTracker) GetChangelog(ctx context.Context, limit, offset int) ([]*ChangelogEntry, error) {
	var records []ChangelogRecord
	result := t.db.WithContext(ctx).Order("executed_at DESC").Limit(limit).Offset(offset).Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get changelog: %w", result.Error)
	}
	
	entries := make([]*ChangelogEntry, len(records))
	for i, record := range records {
		entry, err := t.recordToChangelog(&record)
		if err != nil {
			return nil, fmt.Errorf("failed to convert record to changelog: %w", err)
		}
		entries[i] = entry
	}
	
	return entries, nil
}

// Helper methods for database tracker

// migrationToRecord converts Migration to MigrationRecord
func (t *DatabaseMigrationTracker) migrationToRecord(migration *types.Migration) *MigrationRecord {
	// Convert contexts to JSON
	contextsJSON, _ := json.Marshal(migration.Contexts)
	
	// Convert labels to JSON
	labelsJSON, _ := json.Marshal(migration.Labels)
	
	// Convert dependencies to JSON
	dependenciesJSON, _ := json.Marshal(migration.Dependencies)
	
	// Convert preconditions to JSON
	preconditionsJSON, _ := json.Marshal(migration.Preconditions)
	
	// Convert metadata to JSON
	metadataJSON, _ := json.Marshal(migration.Metadata)

	record := &MigrationRecord{
		ID:              migration.ID,
		Version:         migration.Version,
		Description:     migration.Description,
		Filename:        migration.FileName,
		Filepath:        migration.FilePath,
		Content:         migration.Script,
		Checksum:        migration.Checksum,
		Type:            string(migration.Type),
		Author:          migration.Author,
		ChangesetID:     migration.ChangesetID,
		State:           migration.State,
		Contexts:        string(contextsJSON),
		Labels:          string(labelsJSON),
		DependsOn:       string(dependenciesJSON),
		Preconditions:   string(preconditionsJSON),
		RollbackSQL:     migration.RollbackSQL,
		CanRollback:     migration.CanRollback,
		RunAlways:       migration.RunAlways,
		RunOnChange:     migration.RunOnChange,
		FailOnError:     migration.FailOnError,
		ExecutedAt:      migration.ExecutedAt,
		RolledBackAt:    migration.RolledBackAt,
		ExecutionTime:   int64(migration.ExecutionTime),
		ErrorMessage:    migration.ErrorMessage,
		Metadata:        string(metadataJSON),
		CreatedAt:       migration.CreatedAt,
		UpdatedAt:       migration.UpdatedAt,
	}
	
	return record
}

func (t *DatabaseMigrationTracker) recordToMigration(record *MigrationRecord) (*types.Migration, error) {
	// Parse contexts from JSON
	var contexts []string
	if record.Contexts != "" {
		if err := json.Unmarshal([]byte(record.Contexts), &contexts); err != nil {
			return nil, fmt.Errorf("failed to parse contexts: %w", err)
		}
	}
	
	// Parse labels from JSON
	var labels []string
	if record.Labels != "" {
		if err := json.Unmarshal([]byte(record.Labels), &labels); err != nil {
			return nil, fmt.Errorf("failed to parse labels: %w", err)
		}
	}
	
	// Parse dependencies from JSON
	var dependencies []string
	if record.DependsOn != "" {
		if err := json.Unmarshal([]byte(record.DependsOn), &dependencies); err != nil {
			return nil, fmt.Errorf("failed to parse dependencies: %w", err)
		}
	}
	
	// Parse preconditions from JSON
	var preconditions []types.Precondition
	if record.Preconditions != "" {
		if err := json.Unmarshal([]byte(record.Preconditions), &preconditions); err != nil {
			return nil, fmt.Errorf("failed to parse preconditions: %w", err)
		}
	}
	
	// Parse metadata from JSON
	var metadata map[string]string
	if record.Metadata != "" {
		if err := json.Unmarshal([]byte(record.Metadata), &metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata: %w", err)
		}
	}

	migration := &types.Migration{
		ID:              record.ID,
		Version:         record.Version,
		Description:     record.Description,
		Type:            types.MigrationType(record.Type),
		FileName:        record.Filename,
		FilePath:        record.Filepath,
		Script:          record.Content,
		Checksum:        record.Checksum,
		Author:          record.Author,
		ChangesetID:     record.ChangesetID,
		Tags:            []string{}, // Initialize empty tags
		Contexts:        contexts,
		Labels:          labels,
		Dependencies:    dependencies,
		Preconditions:   preconditions,
		State:           record.State,
		ExecutedAt:      record.ExecutedAt,
		ExecutedBy:      "", // Not stored in record
		ExecutionTime:   time.Duration(record.ExecutionTime),
		ErrorMessage:    record.ErrorMessage,
		CanRollback:     record.CanRollback,
		RollbackSQL:     record.RollbackSQL,
		Rollback:        nil, // Initialize as nil
		RolledBackAt:    record.RolledBackAt,
		RolledBackBy:    "", // Not stored in record
		RunAlways:       record.RunAlways,
		RunOnChange:     record.RunOnChange,
		FailOnError:     record.FailOnError,
		Comments:        "", // Not stored in record
		Metadata:        metadata,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
	
	return migration, nil
}

// changelogToRecord converts ChangelogEntry to ChangelogRecord
func (t *DatabaseMigrationTracker) changelogToRecord(entry *ChangelogEntry) *ChangelogRecord {
	record := &ChangelogRecord{
		MigrationID:   entry.MigrationID,
		Operation:     entry.Operation,
		Status:        entry.Status,
		ExecutedBy:    entry.ExecutedBy,
		ExecutedAt:    entry.ExecutedAt,
		ErrorMessage:  entry.ErrorMessage,
		CreatedAt:     time.Now(),
	}
	
	// Convert execution time
	if entry.ExecutionTime > 0 {
		record.ExecutionTime = int64(entry.ExecutionTime)
	}
	
	// Convert context and metadata to JSON
	if entry.Context != nil {
		if data, err := json.Marshal(entry.Context); err == nil {
			record.Context = string(data)
		}
	}
	
	if entry.Metadata != nil {
		if data, err := json.Marshal(entry.Metadata); err == nil {
			record.Metadata = string(data)
		}
	}
	
	return record
}

// recordToChangelog converts ChangelogRecord to ChangelogEntry
func (t *DatabaseMigrationTracker) recordToChangelog(record *ChangelogRecord) (*ChangelogEntry, error) {
	entry := &ChangelogEntry{
		MigrationID:   record.MigrationID,
		Operation:     record.Operation,
		Status:        record.Status,
		ExecutedBy:    record.ExecutedBy,
		ExecutedAt:    record.ExecutedAt,
		ErrorMessage:  record.ErrorMessage,
	}
	
	// Convert execution time
	if record.ExecutionTime > 0 {
		entry.ExecutionTime = time.Duration(record.ExecutionTime)
	}
	
	// Convert JSON to maps
	if record.Context != "" {
		if err := json.Unmarshal([]byte(record.Context), &entry.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}
	}
	
	if record.Metadata != "" {
		if err := json.Unmarshal([]byte(record.Metadata), &entry.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}
	
	return entry, nil
}

// ValidateChecksum validates the checksum of a migration against the stored record
func (t *DatabaseMigrationTracker) ValidateChecksum(ctx context.Context, migration *types.Migration) (*ChecksumValidationResult, error) {
	var record MigrationRecord
	result := &ChecksumValidationResult{
		MigrationID:      migration.ID,
		ActualChecksum:   migration.Checksum,
		ValidatedAt:      time.Now(),
	}
	
	err := t.db.WithContext(ctx).Table(t.tableName).Where("id = ?", migration.ID).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			result.IsValid = true
			result.Message = "Migration not found in database, checksum validation skipped"
			return result, nil
		}
		return nil, fmt.Errorf("failed to get migration record: %w", err)
	}
	
	result.ExpectedChecksum = record.Checksum
	result.IsValid = record.Checksum == migration.Checksum
	
	if result.IsValid {
		result.Message = "Checksum validation passed"
	} else {
		result.Message = fmt.Sprintf("Checksum mismatch: expected %s, got %s", record.Checksum, migration.Checksum)
	}
	
	return result, nil
}

// File Tracker Implementation

// NewFileMigrationTracker creates a new file-based migration tracker
func NewFileMigrationTracker(filePath string) *FileMigrationTracker {
	return &FileMigrationTracker{
		filePath: filePath,
		data: &TrackerData{
			Migrations: make(map[string]*types.Migration),
			Changelog:  make([]*ChangelogEntry, 0),
			Metadata:   make(map[string]interface{}),
			UpdatedAt:  time.Now(),
		},
	}
}

// loadData loads tracker data from file
func (t *FileMigrationTracker) loadData() error {
	if _, err := os.Stat(t.filePath); os.IsNotExist(err) {
		// File doesn't exist, use empty data
		return nil
	}

	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return fmt.Errorf("failed to read tracker file: %w", err)
	}

	if len(data) == 0 {
		// Empty file, use empty data
		return nil
	}

	err = json.Unmarshal(data, t.data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal tracker data: %w", err)
	}

	return nil
}

// saveData saves tracker data to file
func (t *FileMigrationTracker) saveData() error {
	t.data.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(t.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tracker data: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(t.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create tracker directory: %w", err)
	}

	err = os.WriteFile(t.filePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write tracker file: %w", err)
	}

	return nil
}

// RecordMigration records a migration execution
func (t *FileMigrationTracker) RecordMigration(ctx context.Context, migration *types.Migration) error {
	if err := t.loadData(); err != nil {
		return err
	}

	// Store migration
	t.data.Migrations[migration.ID] = t.copyMigrationForFile(migration)

	// Record changelog entry
	changelog := &ChangelogEntry{
		MigrationID:   migration.ID,
		Operation:     "execute",
		Status:        migration.State,
		ExecutedBy:    migration.Author,
		ExecutedAt:    time.Now(),
		ExecutionTime: migration.ExecutionTime,
		ErrorMessage:  migration.ErrorMessage,
		Context: map[string]interface{}{
			"version":     migration.Version,
			"description": migration.Description,
			"type":        migration.Type,
		},
	}

	t.data.Changelog = append(t.data.Changelog, changelog)

	return t.saveData()
}

// RecordMigrationSuccess records a successful migration execution
func (t *FileMigrationTracker) RecordMigrationSuccess(ctx context.Context, migration types.MigrationRecord, executionTime int64) error {
	now := time.Now()
	migrationObj := &types.Migration{
		ID:            migration.ID,
		Version:       migration.Version,
		Description:   migration.Description,
		Script:        migration.Script,
		Checksum:      migration.Checksum,
		Type:          types.MigrationType(migration.Type),
		State:         "applied",
		ExecutedAt:    &now,
		ExecutionTime: time.Duration(executionTime),
		// Note: types.MigrationRecord doesn't have Filename/Filepath/Author/ChangesetID fields
		// These would need to be added to types.MigrationRecord if needed
	}
	
	return t.RecordMigration(ctx, migrationObj)
}

// RecordMigrationFailure records a failed migration execution
func (t *FileMigrationTracker) RecordMigrationFailure(ctx context.Context, migration types.MigrationRecord, errorMsg string) error {
	now := time.Now()
	migrationObj := &types.Migration{
		ID:            migration.ID,
		Version:       migration.Version,
		Description:   migration.Description,
		Script:        migration.Script,
		Checksum:      migration.Checksum,
		Type:          types.MigrationType(migration.Type),
		State:         "failed",
		ExecutedAt:    &now,
		ErrorMessage:  errorMsg,
		// Note: types.MigrationRecord doesn't have Filename/Filepath/Author/ChangesetID fields
		// These would need to be added to types.MigrationRecord if needed
	}
	
	return t.RecordMigration(ctx, migrationObj)
}

// GetMigration retrieves a migration by ID
func (t *FileMigrationTracker) GetMigration(ctx context.Context, id string) (*types.Migration, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	migration, exists := t.data.Migrations[id]
	if !exists {
		return nil, fmt.Errorf("migration not found: %s", id)
	}

	return t.copyMigrationForFile(migration), nil
}

// RemoveMigration removes a migration record from the file
func (t *FileMigrationTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	if err := t.loadData(); err != nil {
		return err
	}

	if _, exists := t.data.Migrations[migrationID]; !exists {
		return fmt.Errorf("migration not found: %s", migrationID)
	}

	delete(t.data.Migrations, migrationID)
	t.data.UpdatedAt = time.Now()

	return t.saveData()
}

// GetMigrationByVersion retrieves a migration by version
func (t *FileMigrationTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	for _, migration := range t.data.Migrations {
		if migration.Version == version {
			return t.copyMigrationForFile(migration), nil
		}
	}

	return nil, fmt.Errorf("migration not found: %s", version)
}

// GetAppliedMigrations retrieves all applied migrations
func (t *FileMigrationTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	var migrations []*types.Migration
	for _, migration := range t.data.Migrations {
		if migration.State == "applied" {
			migrations = append(migrations, t.copyMigrationForFile(migration))
		}
	}

	// Sort by executed time
	sort.Slice(migrations, func(i, j int) bool {
		if migrations[i].ExecutedAt == nil {
			return false
		}
		if migrations[j].ExecutedAt == nil {
			return true
		}
		return migrations[i].ExecutedAt.Before(*migrations[j].ExecutedAt)
	})

	return migrations, nil
}

// GetPendingMigrations retrieves all pending migrations
func (t *FileMigrationTracker) GetPendingMigrations(ctx context.Context) ([]*types.Migration, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	var migrations []*types.Migration
	for _, migration := range t.data.Migrations {
		if migration.State == "pending" || migration.State == "" {
			migrations = append(migrations, t.copyMigrationForFile(migration))
		}
	}

	return migrations, nil
}

// IsMigrationApplied checks if a migration is applied
func (t *FileMigrationTracker) IsMigrationApplied(ctx context.Context, id string) (bool, error) {
	if err := t.loadData(); err != nil {
		return false, err
	}

	migration, exists := t.data.Migrations[id]
	if !exists {
		return false, nil
	}

	return migration.State == "applied", nil
}

// GetMigrationHistory retrieves migration history with pagination
func (t *FileMigrationTracker) GetMigrationHistory(ctx context.Context, limit, offset int) ([]*types.Migration, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	var migrations []*types.Migration
	for _, migration := range t.data.Migrations {
		migrations = append(migrations, t.copyMigrationForFile(migration))
	}

	// Sort by executed time (newest first)
	sort.Slice(migrations, func(i, j int) bool {
		if migrations[i].ExecutedAt == nil {
			return false
		}
		if migrations[j].ExecutedAt == nil {
			return true
		}
		return migrations[i].ExecutedAt.After(*migrations[j].ExecutedAt)
	})

	// Apply pagination
	start := offset
	if start >= len(migrations) {
		return []*types.Migration{}, nil
	}

	end := start + limit
	if end > len(migrations) {
		end = len(migrations)
	}

	return migrations[start:end], nil
}

// RecordChangelog records a changelog entry
func (t *FileMigrationTracker) RecordChangelog(ctx context.Context, entry *ChangelogEntry) error {
	if err := t.loadData(); err != nil {
		return err
	}

	t.data.Changelog = append(t.data.Changelog, t.copyChangelogEntryForFile(entry))
	return t.saveData()
}

// GetChangelog retrieves changelog with pagination
func (t *FileMigrationTracker) GetChangelog(ctx context.Context, limit, offset int) ([]*ChangelogEntry, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	// Sort by executed time (newest first)
	sort.Slice(t.data.Changelog, func(i, j int) bool {
		return t.data.Changelog[i].ExecutedAt.After(t.data.Changelog[j].ExecutedAt)
	})

	// Apply pagination
	start := offset
	if start >= len(t.data.Changelog) {
		return []*ChangelogEntry{}, nil
	}

	end := start + limit
	if end > len(t.data.Changelog) {
		end = len(t.data.Changelog)
	}

	result := make([]*ChangelogEntry, end-start)
	for i := start; i < end; i++ {
		result[i-start] = t.copyChangelogEntryForFile(t.data.Changelog[i])
	}

	return result, nil
}

// ValidateChecksum validates the checksum of a migration (file implementation)
func (t *FileMigrationTracker) ValidateChecksum(ctx context.Context, migration *types.Migration) (*ChecksumValidationResult, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	result := &ChecksumValidationResult{
		MigrationID:    migration.ID,
		ActualChecksum: migration.Checksum,
		ValidatedAt:    time.Now(),
	}

	if existing, exists := t.data.Migrations[migration.ID]; exists {
		result.ExpectedChecksum = existing.Checksum
		result.IsValid = existing.Checksum == migration.Checksum
		if result.IsValid {
			result.Message = "Checksum validation passed"
		} else {
			result.Message = fmt.Sprintf("Checksum mismatch: expected %s, got %s", existing.Checksum, migration.Checksum)
		}
	} else {
		result.IsValid = true
		result.Message = "Migration not found in tracker, checksum validation skipped"
	}

	return result, nil
}

// DetectDrift detects drift in applied migrations (file implementation)
func (t *FileMigrationTracker) DetectDrift(ctx context.Context) ([]*DriftDetectionResult, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	var results []*DriftDetectionResult

	for _, migration := range t.data.Migrations {
		if migration.State != "applied" {
			continue
		}

		// Check if migration file still exists and has same checksum
		if migration.FilePath != "" {
			if _, err := os.Stat(migration.FilePath); os.IsNotExist(err) {
				results = append(results, &DriftDetectionResult{
					MigrationID:      migration.ID,
					Version:          migration.Version,
					DriftType:        "file_missing",
					ExpectedChecksum: migration.Checksum,
					FilePath:         migration.FilePath,
					DetectedAt:       time.Now(),
					Description:      fmt.Sprintf("Migration file %s no longer exists", migration.FilePath),
				})
				continue
			}

			// Read file and calculate checksum
			content, err := os.ReadFile(migration.FilePath)
			if err != nil {
				continue // Skip if can't read file
			}

			actualChecksum := fmt.Sprintf("%x", sha256.Sum256(content))
			if actualChecksum != migration.Checksum {
				results = append(results, &DriftDetectionResult{
					MigrationID:      migration.ID,
					Version:          migration.Version,
					DriftType:        "checksum_mismatch",
					ExpectedChecksum: migration.Checksum,
					ActualChecksum:   actualChecksum,
					FilePath:         migration.FilePath,
					DetectedAt:       time.Now(),
					Description:      fmt.Sprintf("Migration file %s has been modified", migration.FilePath),
				})
			}
		}
	}

	return results, nil
}

// ValidateIdempotency validates if a migration can be safely re-run (file implementation)
func (t *FileMigrationTracker) ValidateIdempotency(ctx context.Context, migration *types.Migration) (*IdempotencyValidationResult, error) {
	if err := t.loadData(); err != nil {
		return nil, err
	}

	result := &IdempotencyValidationResult{
		MigrationID: migration.ID,
		ValidatedAt: time.Now(),
	}

	if existing, exists := t.data.Migrations[migration.ID]; exists {
		result.PreviousExecution = existing.ExecutedAt

		// Check if migration is marked as always runnable
		if existing.RunAlways {
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration marked as run_always, safe to re-run"
			return result, nil
		}

		// Check if migration should run on change
		if existing.RunOnChange && existing.Checksum != migration.Checksum {
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration marked as run_on_change and checksum differs, safe to re-run"
			return result, nil
		}

		// Check current state
		switch existing.State {
		case "applied":
			result.IsIdempotent = false
			result.CanRerun = false
			result.Message = "Migration already applied and not marked for re-execution"
		case "failed":
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Previous execution failed, safe to retry"
		case "rolled_back":
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration was rolled back, safe to re-run"
		default:
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration in unknown state, assuming safe to run"
		}
	} else {
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Migration not previously executed, safe to run"
	}

	return result, nil
}

// Helper methods for file tracker

// copyMigrationForFile creates a deep copy of a migration for file operations
func (t *FileMigrationTracker) copyMigrationForFile(migration *types.Migration) *types.Migration {
	if migration == nil {
		return nil
	}

	// Deep copy contexts
	contexts := make([]string, len(migration.Contexts))
	copy(contexts, migration.Contexts)

	// Deep copy labels
	labels := make([]string, len(migration.Labels))
	copy(labels, migration.Labels)

	// Deep copy dependencies
	dependencies := make([]string, len(migration.Dependencies))
	copy(dependencies, migration.Dependencies)

	// Deep copy tags
	tags := make([]string, len(migration.Tags))
	copy(tags, migration.Tags)

	// Deep copy preconditions
	preconditions := make([]types.Precondition, len(migration.Preconditions))
	copy(preconditions, migration.Preconditions)

	// Deep copy metadata
	metadata := make(map[string]string)
	for k, v := range migration.Metadata {
		metadata[k] = v
	}

	return &types.Migration{
		ID:              migration.ID,
		Version:         migration.Version,
		Description:     migration.Description,
		Type:            migration.Type,
		FileName:        migration.FileName,
		FilePath:        migration.FilePath,
		Script:          migration.Script,
		Checksum:        migration.Checksum,
		Author:          migration.Author,
		ChangesetID:     migration.ChangesetID,
		Tags:            tags,
		Contexts:        contexts,
		Labels:          labels,
		Dependencies:    dependencies,
		Preconditions:   preconditions,
		State:           migration.State,
		RollbackSQL:     migration.RollbackSQL,
		CanRollback:     migration.CanRollback,
		RunAlways:       migration.RunAlways,
		RunOnChange:     migration.RunOnChange,
		FailOnError:     migration.FailOnError,
		ExecutedAt:      migration.ExecutedAt,
		RolledBackAt:    migration.RolledBackAt,
		ExecutionTime:   migration.ExecutionTime,
		ErrorMessage:    migration.ErrorMessage,
		Metadata:        metadata,
		CreatedAt:       migration.CreatedAt,
		UpdatedAt:       migration.UpdatedAt,
	}
}

// copyChangelogEntryForFile creates a deep copy of a changelog entry for file operations
func (t *FileMigrationTracker) copyChangelogEntryForFile(entry *ChangelogEntry) *ChangelogEntry {
	if entry == nil {
		return nil
	}

	// Deep copy context
	context := make(map[string]interface{})
	for k, v := range entry.Context {
		context[k] = v
	}

	// Deep copy metadata
	metadata := make(map[string]interface{})
	for k, v := range entry.Metadata {
		metadata[k] = v
	}

	return &ChangelogEntry{
		MigrationID:   entry.MigrationID,
		Operation:     entry.Operation,
		Status:        entry.Status,
		ExecutedBy:    entry.ExecutedBy,
		ExecutedAt:    entry.ExecutedAt,
		ExecutionTime: entry.ExecutionTime,
		ErrorMessage:  entry.ErrorMessage,
		Context:       context,
		Metadata:      metadata,
	}
}

// DetectDrift detects drift in applied migrations by comparing checksums
func (t *DatabaseMigrationTracker) DetectDrift(ctx context.Context) ([]*DriftDetectionResult, error) {
	var results []*DriftDetectionResult
	
	// For now, we'll return an empty result set as drift detection is a placeholder
	// In a real implementation, you'd:
	// 1. Get all applied migrations from the database
	// 2. Read the current migration files from disk
	// 3. Calculate their current checksums
	// 4. Compare with stored checksums in the database
	// 5. If different, create DriftDetectionResult and append to results
	
	return results, nil
}

// ValidateIdempotency validates if a migration can be safely re-run
func (t *DatabaseMigrationTracker) ValidateIdempotency(ctx context.Context, migration *types.Migration) (*IdempotencyValidationResult, error) {
	var record MigrationRecord
	result := &IdempotencyValidationResult{
		MigrationID: migration.ID,
		ValidatedAt: time.Now(),
	}
	
	err := t.db.WithContext(ctx).Table(t.tableName).Where("id = ?", migration.ID).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration not previously executed, safe to run"
			return result, nil
		}
		return nil, fmt.Errorf("failed to get migration record: %w", err)
	}
	
	result.PreviousExecution = record.ExecutedAt
	
	// Check if migration is marked as always runnable
	if record.RunAlways {
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Migration marked as run_always, safe to re-run"
		return result, nil
	}
	
	// Check if migration should run on change
	if record.RunOnChange && record.Checksum != migration.Checksum {
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Migration marked as run_on_change and checksum differs, safe to re-run"
		return result, nil
	}
	
	// Check current state
	switch record.State {
	case "applied":
		result.IsIdempotent = false
		result.CanRerun = false
		result.Message = "Migration already applied and not marked for re-execution"
	case "failed":
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Previous execution failed, safe to retry"
	case "rolled_back":
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Migration was rolled back, safe to re-run"
	default:
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Migration in unknown state, assuming safe to run"
	}
	
	return result, nil
}

// In-Memory Tracker Implementation

// NewInMemoryMigrationTracker creates a new in-memory migration tracker
func NewInMemoryMigrationTracker() *InMemoryMigrationTracker {
	return &InMemoryMigrationTracker{
		migrations: make(map[string]*types.Migration),
		changelog:  make([]*ChangelogEntry, 0),
	}
}

// RecordMigration records a migration execution
func (t *InMemoryMigrationTracker) RecordMigration(ctx context.Context, migration *types.Migration) error {
	// Store migration
	t.migrations[migration.ID] = migration
	
	// Record changelog entry
	changelog := &ChangelogEntry{
		MigrationID:   migration.ID,
		Operation:     "execute",
		Status:        migration.State,
		ExecutedBy:    migration.Author,
		ExecutedAt:    time.Now(),
		ExecutionTime: migration.ExecutionTime,
		ErrorMessage:  migration.ErrorMessage,
		Context: map[string]interface{}{
			"version":     migration.Version,
			"description": migration.Description,
			"type":        migration.Type,
		},
	}
	
	return t.RecordChangelog(ctx, changelog)
}

// RecordMigrationSuccess records a successful migration execution
func (t *InMemoryMigrationTracker) RecordMigrationSuccess(ctx context.Context, migration types.MigrationRecord, executionTime int64) error {
	now := time.Now()
	migrationObj := &types.Migration{
		ID:            migration.ID,
		Version:       migration.Version,
		Description:   migration.Description,
		Type:          types.MigrationType(migration.Type),
		State:         "applied",
		ExecutedAt:    &now,
		ExecutionTime: time.Duration(executionTime),
		Checksum:      migration.Checksum,
		Script:        migration.Script,
		// Note: types.MigrationRecord doesn't have FileName/FilePath/ExecutedBy fields
		// These would need to be added to types.MigrationRecord if needed
	}

	return t.RecordMigration(ctx, migrationObj)
}

// RecordMigrationFailure records a failed migration execution
func (t *InMemoryMigrationTracker) RecordMigrationFailure(ctx context.Context, migration types.MigrationRecord, errorMsg string) error {
	now := time.Now()
	migrationObj := &types.Migration{
		ID:            migration.ID,
		Version:       migration.Version,
		Description:   migration.Description,
		Type:          types.MigrationType(migration.Type),
		State:         "failed",
		ExecutedAt:    &now,
		Checksum:      migration.Checksum,
		Script:        migration.Script,
		ErrorMessage:  errorMsg,
		// Note: types.MigrationRecord doesn't have FileName/FilePath/ExecutedBy fields
		// These would need to be added to types.MigrationRecord if needed
	}

	return t.RecordMigration(ctx, migrationObj)
}

// GetMigration retrieves a migration by ID
func (t *InMemoryMigrationTracker) GetMigration(ctx context.Context, id string) (*types.Migration, error) {
	migration, exists := t.migrations[id]
	if !exists {
		return nil, fmt.Errorf("migration not found: %s", id)
	}
	
	// Return a copy to prevent external modifications
	return t.copyMigration(migration), nil
}

// RemoveMigration removes a migration record from memory
func (t *InMemoryMigrationTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	delete(t.migrations, migrationID)
	return nil
}

// GetMigrationByVersion retrieves a migration by version
func (t *InMemoryMigrationTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	for _, migration := range t.migrations {
		if migration.Version == version {
			return t.copyMigration(migration), nil
		}
	}
	
	return nil, fmt.Errorf("migration not found: %s", version)
}

// GetAppliedMigrations retrieves all applied migrations
func (t *InMemoryMigrationTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	var applied []*types.Migration
	
	for _, migration := range t.migrations {
		if migration.State == "applied" {
			applied = append(applied, t.copyMigration(migration))
		}
	}
	
	// Sort by execution time
	// Note: In a real implementation, you'd want proper sorting
	
	return applied, nil
}

// GetPendingMigrations retrieves all pending migrations
func (t *InMemoryMigrationTracker) GetPendingMigrations(ctx context.Context) ([]*types.Migration, error) {
	var pending []*types.Migration
	
	for _, migration := range t.migrations {
		if migration.State == "pending" {
			pending = append(pending, t.copyMigration(migration))
		}
	}
	
	return pending, nil
}

// IsMigrationApplied checks if a migration is applied
func (t *InMemoryMigrationTracker) IsMigrationApplied(ctx context.Context, id string) (bool, error) {
	migration, exists := t.migrations[id]
	if !exists {
		return false, nil
	}
	
	return migration.State == "applied", nil
}

// GetMigrationHistory retrieves migration history with pagination
func (t *InMemoryMigrationTracker) GetMigrationHistory(ctx context.Context, limit, offset int) ([]*types.Migration, error) {
	var history []*types.Migration
	
	for _, migration := range t.migrations {
		history = append(history, t.copyMigration(migration))
	}
	
	// Apply pagination
	start := offset
	if start > len(history) {
		start = len(history)
	}
	
	end := start + limit
	if end > len(history) {
		end = len(history)
	}
	
	return history[start:end], nil
}

// RecordChangelog records a changelog entry
func (t *InMemoryMigrationTracker) RecordChangelog(ctx context.Context, entry *ChangelogEntry) error {
	// Create a copy to prevent external modifications
	entryCopy := &ChangelogEntry{
		MigrationID:   entry.MigrationID,
		Operation:     entry.Operation,
		Status:        entry.Status,
		ExecutedBy:    entry.ExecutedBy,
		ExecutedAt:    entry.ExecutedAt,
		ExecutionTime: entry.ExecutionTime,
		ErrorMessage:  entry.ErrorMessage,
		Context:       make(map[string]interface{}),
		Metadata:      make(map[string]interface{}),
	}
	
	// Copy context and metadata
	for k, v := range entry.Context {
		entryCopy.Context[k] = v
	}
	for k, v := range entry.Metadata {
		entryCopy.Metadata[k] = v
	}
	
	t.changelog = append(t.changelog, entryCopy)
	return nil
}

// GetChangelog retrieves changelog entries
func (t *InMemoryMigrationTracker) GetChangelog(ctx context.Context, limit, offset int) ([]*ChangelogEntry, error) {
	// Apply pagination
	start := offset
	if start > len(t.changelog) {
		start = len(t.changelog)
	}
	
	end := start + limit
	if end > len(t.changelog) {
		end = len(t.changelog)
	}
	
	// Return copies
	result := make([]*ChangelogEntry, end-start)
	for i := start; i < end; i++ {
		result[i-start] = t.copyChangelogEntry(t.changelog[i])
	}
	
	return result, nil
}

// Helper methods for in-memory tracker

// copyMigration creates a deep copy of a migration
func (t *InMemoryMigrationTracker) copyMigration(migration *types.Migration) *types.Migration {
	if migration == nil {
		return nil
	}

	// Deep copy contexts
	contexts := make([]string, len(migration.Contexts))
	copy(contexts, migration.Contexts)

	// Deep copy labels
	labels := make([]string, len(migration.Labels))
	copy(labels, migration.Labels)

	// Deep copy dependencies
	dependencies := make([]string, len(migration.Dependencies))
	copy(dependencies, migration.Dependencies)

	// Deep copy tags
	tags := make([]string, len(migration.Tags))
	copy(tags, migration.Tags)

	// Deep copy preconditions
	preconditions := make([]types.Precondition, len(migration.Preconditions))
	copy(preconditions, migration.Preconditions)

	// Deep copy metadata
	metadata := make(map[string]string)
	for k, v := range migration.Metadata {
		metadata[k] = v
	}

	return &types.Migration{
		ID:              migration.ID,
		Version:         migration.Version,
		Description:     migration.Description,
		Type:            migration.Type,
		FileName:        migration.FileName,
		FilePath:        migration.FilePath,
		Script:          migration.Script,
		Checksum:        migration.Checksum,
		Author:          migration.Author,
		ChangesetID:     migration.ChangesetID,
		Tags:            tags,
		Contexts:        contexts,
		Labels:          labels,
		Dependencies:    dependencies,
		Preconditions:   preconditions,
		State:           migration.State,
		ExecutedAt:      migration.ExecutedAt,
		ExecutedBy:      migration.ExecutedBy,
		ExecutionTime:   migration.ExecutionTime,
		ErrorMessage:    migration.ErrorMessage,
		CanRollback:     migration.CanRollback,
		RollbackSQL:     migration.RollbackSQL,
		Rollback:        migration.Rollback,
		RolledBackAt:    migration.RolledBackAt,
		RolledBackBy:    migration.RolledBackBy,
		RunAlways:       migration.RunAlways,
		RunOnChange:     migration.RunOnChange,
		FailOnError:     migration.FailOnError,
		Comments:        migration.Comments,
		Metadata:        metadata,
		CreatedAt:       migration.CreatedAt,
		UpdatedAt:       migration.UpdatedAt,
	}
}

// copyChangelogEntry creates a deep copy of a changelog entry
func (t *InMemoryMigrationTracker) copyChangelogEntry(entry *ChangelogEntry) *ChangelogEntry {
	entryCopy := &ChangelogEntry{
		MigrationID:   entry.MigrationID,
		Operation:     entry.Operation,
		Status:        entry.Status,
		ExecutedBy:    entry.ExecutedBy,
		ExecutedAt:    entry.ExecutedAt,
		ExecutionTime: entry.ExecutionTime,
		ErrorMessage:  entry.ErrorMessage,
		Context:       make(map[string]interface{}),
		Metadata:      make(map[string]interface{}),
	}
	
	// Copy context and metadata
	for k, v := range entry.Context {
		entryCopy.Context[k] = v
	}
	for k, v := range entry.Metadata {
		entryCopy.Metadata[k] = v
	}
	
	return entryCopy
}

// ValidateChecksum validates the checksum of a migration (in-memory implementation)
func (t *InMemoryMigrationTracker) ValidateChecksum(ctx context.Context, migration *types.Migration) (*ChecksumValidationResult, error) {
	result := &ChecksumValidationResult{
		MigrationID:    migration.ID,
		ActualChecksum: migration.Checksum,
		ValidatedAt:    time.Now(),
	}
	
	if existing, exists := t.migrations[migration.ID]; exists {
		result.ExpectedChecksum = existing.Checksum
		result.IsValid = existing.Checksum == migration.Checksum
		if result.IsValid {
			result.Message = "Checksum validation passed"
		} else {
			result.Message = fmt.Sprintf("Checksum mismatch: expected %s, got %s", existing.Checksum, migration.Checksum)
		}
	} else {
		result.IsValid = true
		result.Message = "Migration not found in memory, checksum validation skipped"
	}
	
	return result, nil
}

// DetectDrift detects drift in applied migrations (in-memory implementation)
func (t *InMemoryMigrationTracker) DetectDrift(ctx context.Context) ([]*DriftDetectionResult, error) {
	var results []*DriftDetectionResult
	
	// In-memory implementation doesn't have file system access
	// This would be implemented if file paths were tracked
	
	return results, nil
}

// ValidateIdempotency validates if a migration can be safely re-run (in-memory implementation)
func (t *InMemoryMigrationTracker) ValidateIdempotency(ctx context.Context, migration *types.Migration) (*IdempotencyValidationResult, error) {
	result := &IdempotencyValidationResult{
		MigrationID: migration.ID,
		ValidatedAt: time.Now(),
	}
	
	if existing, exists := t.migrations[migration.ID]; exists {
		result.PreviousExecution = existing.ExecutedAt
		
		// Check if migration is marked as always runnable
		if existing.RunAlways {
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration marked as run_always, safe to re-run"
			return result, nil
		}
		
		// Check if migration should run on change
		if existing.RunOnChange && existing.Checksum != migration.Checksum {
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration marked as run_on_change and checksum differs, safe to re-run"
			return result, nil
		}
		
		// Check current state
		switch existing.State {
		case "applied":
			result.IsIdempotent = false
			result.CanRerun = false
			result.Message = "Migration already applied and not marked for re-execution"
		case "failed":
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Previous execution failed, safe to retry"
		case "rolled_back":
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration was rolled back, safe to re-run"
		default:
			result.IsIdempotent = true
			result.CanRerun = true
			result.Message = "Migration in unknown state, assuming safe to run"
		}
	} else {
		result.IsIdempotent = true
		result.CanRerun = true
		result.Message = "Migration not previously executed, safe to run"
	}
	
	return result, nil
}

/**
 * @file store.go
 * @description Checksum store implementation with multiple storage backends
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/store.go
 */

package checksum

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/backend"
)

// MemoryChecksumStore implements ChecksumStore interface using in-memory storage
type MemoryChecksumStore struct {
	records map[string]*ChecksumRecord
	mutex   sync.RWMutex
	config  *ChecksumConfig
}

// NewMemoryChecksumStore creates a new memory-based checksum store
func NewMemoryChecksumStore(config *ChecksumConfig) *MemoryChecksumStore {
	return &MemoryChecksumStore{
		records: make(map[string]*ChecksumRecord),
		config:  config,
	}
}

// Store stores a checksum record
func (s *MemoryChecksumStore) Store(ctx context.Context, record *ChecksumRecord) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if record.ID == "" {
		return fmt.Errorf("record ID cannot be empty")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Set timestamps
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	s.records[record.ID] = record
	return nil
}

// Retrieve retrieves a checksum record by ID
func (s *MemoryChecksumStore) Retrieve(ctx context.Context, id string) (*ChecksumRecord, error) {
	if id == "" {
		return nil, fmt.Errorf("ID cannot be empty")
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	record, exists := s.records[id]
	if !exists {
		return nil, fmt.Errorf("record not found: %s", id)
	}

	// Return a copy to prevent external modifications
	return s.copyRecord(record), nil
}

// RetrieveBySource retrieves checksum records by source and type
func (s *MemoryChecksumStore) RetrieveBySource(ctx context.Context, source string, checksumType ChecksumType) ([]*ChecksumRecord, error) {
	if source == "" {
		return nil, fmt.Errorf("source cannot be empty")
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []*ChecksumRecord
	for _, record := range s.records {
		if record.Source == source && record.Type == checksumType {
			results = append(results, s.copyRecord(record))
		}
	}

	return results, nil
}

// Update updates a checksum record
func (s *MemoryChecksumStore) Update(ctx context.Context, record *ChecksumRecord) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if record.ID == "" {
		return fmt.Errorf("record ID cannot be empty")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.records[record.ID]; !exists {
		return fmt.Errorf("record not found: %s", record.ID)
	}

	record.UpdatedAt = time.Now()
	s.records[record.ID] = record
	return nil
}

// Delete deletes a checksum record
func (s *MemoryChecksumStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.records[id]; !exists {
		return fmt.Errorf("record not found: %s", id)
	}

	delete(s.records, id)
	return nil
}

// List lists checksum records with filtering
func (s *MemoryChecksumStore) List(ctx context.Context, filter *ChecksumFilter) ([]*ChecksumRecord, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []*ChecksumRecord
	for _, record := range s.records {
		if s.matchesFilter(record, filter) {
			results = append(results, s.copyRecord(record))
		}
	}

	// Apply sorting and pagination
	results = s.applySorting(results, filter)
	results = s.applyPagination(results, filter)

	return results, nil
}

// Exists checks if a record exists
func (s *MemoryChecksumStore) Exists(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("ID cannot be empty")
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	_, exists := s.records[id]
	return exists, nil
}

// GetStatistics returns statistics about stored checksums
func (s *MemoryChecksumStore) GetStatistics(ctx context.Context) (*ChecksumStatistics, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := &ChecksumStatistics{
		TotalRecords:       len(s.records),
		RecordsByType:      make(map[ChecksumType]int),
		RecordsByAlgorithm: make(map[ChecksumAlgorithm]int),
	}

	var totalSize int64
	var oldestTime, newestTime *time.Time

	for _, record := range s.records {
		stats.RecordsByType[record.Type]++
		stats.RecordsByAlgorithm[record.Algorithm]++
		
		totalSize += int64(len(record.Value))

		if oldestTime == nil || record.CreatedAt.Before(*oldestTime) {
			oldestTime = &record.CreatedAt
		}
		if newestTime == nil || record.CreatedAt.After(*newestTime) {
			newestTime = &record.CreatedAt
		}
	}

	stats.OldestRecord = oldestTime
	stats.NewestRecord = newestTime
	stats.StorageUsage = totalSize

	if len(s.records) > 0 {
		stats.AverageSize = float64(totalSize) / float64(len(s.records))
	}

	return stats, nil
}

// Cleanup removes old records
func (s *MemoryChecksumStore) Cleanup(ctx context.Context, olderThan time.Time) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var deletedCount int
	for id, record := range s.records {
		if record.CreatedAt.Before(olderThan) {
			delete(s.records, id)
			deletedCount++
		}
	}

	return deletedCount, nil
}

// Helper methods for MemoryChecksumStore
func (s *MemoryChecksumStore) copyRecord(record *ChecksumRecord) *ChecksumRecord {
	if record == nil {
		return nil
	}

	// Deep copy metadata
	metadata := make(map[string]interface{})
	for k, v := range record.Metadata {
		metadata[k] = v
	}

	// Deep copy tags
	tags := make([]string, len(record.Tags))
	copy(tags, record.Tags)

	return &ChecksumRecord{
		ID:        record.ID,
		Type:      record.Type,
		Algorithm: record.Algorithm,
		Value:     record.Value,
		Source:    record.Source,
		Metadata:  metadata,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
		Version:   record.Version,
		Tags:      tags,
	}
}

func (s *MemoryChecksumStore) matchesFilter(record *ChecksumRecord, filter *ChecksumFilter) bool {
	if filter == nil {
		return true
	}

	if filter.Type != nil && record.Type != *filter.Type {
		return false
	}

	if filter.Algorithm != nil && record.Algorithm != *filter.Algorithm {
		return false
	}

	if filter.Source != nil && record.Source != *filter.Source {
		return false
	}

	if filter.Version != nil && record.Version != *filter.Version {
		return false
	}

	if filter.CreatedAfter != nil && record.CreatedAt.Before(*filter.CreatedAfter) {
		return false
	}

	if filter.CreatedBefore != nil && record.CreatedAt.After(*filter.CreatedBefore) {
		return false
	}

	if len(filter.Tags) > 0 {
		tagMap := make(map[string]bool)
		for _, tag := range record.Tags {
			tagMap[tag] = true
		}
		for _, filterTag := range filter.Tags {
			if !tagMap[filterTag] {
				return false
			}
		}
	}

	return true
}

func (s *MemoryChecksumStore) applySorting(records []*ChecksumRecord, filter *ChecksumFilter) []*ChecksumRecord {
	if filter == nil || filter.SortBy == "" {
		return records
	}

	// Simple sorting implementation
	// In a real implementation, you might want to use a more sophisticated sorting library
	return records
}

func (s *MemoryChecksumStore) applyPagination(records []*ChecksumRecord, filter *ChecksumFilter) []*ChecksumRecord {
	if filter == nil {
		return records
	}

	start := filter.Offset
	if start < 0 {
		start = 0
	}
	if start >= len(records) {
		return []*ChecksumRecord{}
	}

	end := len(records)
	if filter.Limit > 0 {
		end = start + filter.Limit
		if end > len(records) {
			end = len(records)
		}
	}

	return records[start:end]
}

// DatabaseChecksumStore implements ChecksumStore interface using database storage
type DatabaseChecksumStore struct {
	backend backend.DatabaseBackend
	config  *ChecksumConfig
	tableName string
}

// NewDatabaseChecksumStore creates a new database-based checksum store
func NewDatabaseChecksumStore(backend backend.DatabaseBackend, config *ChecksumConfig) *DatabaseChecksumStore {
	return &DatabaseChecksumStore{
		backend:   backend,
		config:    config,
		tableName: "checksum_records",
	}
}

// Initialize initializes the database store
func (s *DatabaseChecksumStore) Initialize(ctx context.Context) error {
	// Create table if it doesn't exist
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS ` + s.tableName + ` (
			id VARCHAR(255) PRIMARY KEY,
			type VARCHAR(50) NOT NULL,
			algorithm VARCHAR(50) NOT NULL,
			value TEXT NOT NULL,
			source VARCHAR(500) NOT NULL,
			metadata TEXT,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			version VARCHAR(100),
			tags TEXT,
			INDEX idx_source_type (source, type),
			INDEX idx_algorithm (algorithm),
			INDEX idx_created_at (created_at)
		)
	`

	return s.backend.Execute(ctx, createTableSQL)
}

// Store stores a checksum record in the database
func (s *DatabaseChecksumStore) Store(ctx context.Context, record *ChecksumRecord) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if record.ID == "" {
		return fmt.Errorf("record ID cannot be empty")
	}

	// Serialize metadata and tags
	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	tagsJSON, err := json.Marshal(record.Tags)
	if err != nil {
		return fmt.Errorf("failed to serialize tags: %w", err)
	}

	// Set timestamps
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	insertSQL := `
		INSERT INTO ` + s.tableName + ` 
		(id, type, algorithm, value, source, metadata, created_at, updated_at, version, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	return s.backend.Execute(ctx, insertSQL,
		record.ID, record.Type, record.Algorithm, record.Value, record.Source,
		string(metadataJSON), record.CreatedAt, record.UpdatedAt, record.Version, string(tagsJSON))
}

// Retrieve retrieves a checksum record by ID from the database
func (s *DatabaseChecksumStore) Retrieve(ctx context.Context, id string) (*ChecksumRecord, error) {
	if id == "" {
		return nil, fmt.Errorf("ID cannot be empty")
	}

	selectSQL := `
		SELECT id, type, algorithm, value, source, metadata, created_at, updated_at, version, tags
		FROM ` + s.tableName + ` WHERE id = ?
	`

	row := s.backend.QueryRow(ctx, selectSQL, id)
	return s.scanRecord(row)
}

// RetrieveBySource retrieves checksum records by source and type from the database
func (s *DatabaseChecksumStore) RetrieveBySource(ctx context.Context, source string, checksumType ChecksumType) ([]*ChecksumRecord, error) {
	if source == "" {
		return nil, fmt.Errorf("source cannot be empty")
	}

	selectSQL := `
		SELECT id, type, algorithm, value, source, metadata, created_at, updated_at, version, tags
		FROM ` + s.tableName + ` WHERE source = ? AND type = ?
		ORDER BY created_at DESC
	`

	rows, err := s.backend.Query(ctx, selectSQL, source, checksumType)
	if err != nil {
		return nil, fmt.Errorf("failed to query records: %w", err)
	}
	defer rows.Close()

	var records []*ChecksumRecord
	for rows.Next() {
		record, err := s.scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}
		records = append(records, record)
	}

	return records, nil
}

// Update updates a checksum record in the database
func (s *DatabaseChecksumStore) Update(ctx context.Context, record *ChecksumRecord) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if record.ID == "" {
		return fmt.Errorf("record ID cannot be empty")
	}

	// Serialize metadata and tags
	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	tagsJSON, err := json.Marshal(record.Tags)
	if err != nil {
		return fmt.Errorf("failed to serialize tags: %w", err)
	}

	record.UpdatedAt = time.Now()

	updateSQL := `
		UPDATE ` + s.tableName + ` 
		SET type = ?, algorithm = ?, value = ?, source = ?, metadata = ?, 
		    updated_at = ?, version = ?, tags = ?
		WHERE id = ?
	`

	return s.backend.Execute(ctx, updateSQL,
		record.Type, record.Algorithm, record.Value, record.Source,
		string(metadataJSON), record.UpdatedAt, record.Version, string(tagsJSON), record.ID)
}

// Delete deletes a checksum record from the database
func (s *DatabaseChecksumStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("ID cannot be empty")
	}

	deleteSQL := `DELETE FROM ` + s.tableName + ` WHERE id = ?`
	return s.backend.Execute(ctx, deleteSQL, id)
}

// List lists checksum records with filtering from the database
func (s *DatabaseChecksumStore) List(ctx context.Context, filter *ChecksumFilter) ([]*ChecksumRecord, error) {
	query := `SELECT id, type, algorithm, value, source, metadata, created_at, updated_at, version, tags FROM ` + s.tableName
	args := []interface{}{}
	conditions := []string{}

	// Build WHERE clause
	if filter != nil {
		if filter.Type != nil {
			conditions = append(conditions, "type = ?")
			args = append(args, *filter.Type)
		}
		if filter.Algorithm != nil {
			conditions = append(conditions, "algorithm = ?")
			args = append(args, *filter.Algorithm)
		}
		if filter.Source != nil {
			conditions = append(conditions, "source = ?")
			args = append(args, *filter.Source)
		}
		if filter.Version != nil {
			conditions = append(conditions, "version = ?")
			args = append(args, *filter.Version)
		}
		if filter.CreatedAfter != nil {
			conditions = append(conditions, "created_at > ?")
			args = append(args, *filter.CreatedAfter)
		}
		if filter.CreatedBefore != nil {
			conditions = append(conditions, "created_at < ?")
			args = append(args, *filter.CreatedBefore)
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Add sorting
	if filter != nil && filter.SortBy != "" {
		order := "ASC"
		if filter.SortOrder == "DESC" {
			order = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", filter.SortBy, order)
	}

	// Add pagination
	if filter != nil {
		if filter.Limit > 0 {
			query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		}
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := s.backend.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query records: %w", err)
	}
	defer rows.Close()

	var records []*ChecksumRecord
	for rows.Next() {
		record, err := s.scanRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan record: %w", err)
		}
		records = append(records, record)
	}

	return records, nil
}

// Exists checks if a record exists in the database
func (s *DatabaseChecksumStore) Exists(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("ID cannot be empty")
	}

	countSQL := `SELECT COUNT(*) FROM ` + s.tableName + ` WHERE id = ?`
	row := s.backend.QueryRow(ctx, countSQL, id)

	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return count > 0, nil
}

// GetStatistics returns statistics about stored checksums from the database
func (s *DatabaseChecksumStore) GetStatistics(ctx context.Context) (*ChecksumStatistics, error) {
	stats := &ChecksumStatistics{
		RecordsByType:      make(map[ChecksumType]int),
		RecordsByAlgorithm: make(map[ChecksumAlgorithm]int),
	}

	// Get total count
	countSQL := `SELECT COUNT(*) FROM ` + s.tableName
	row := s.backend.QueryRow(ctx, countSQL)
	if err := row.Scan(&stats.TotalRecords); err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get counts by type
	typeSQL := `SELECT type, COUNT(*) FROM ` + s.tableName + ` GROUP BY type`
	rows, err := s.backend.Query(ctx, typeSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to query type statistics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var checksumType ChecksumType
		var count int
		if err := rows.Scan(&checksumType, &count); err != nil {
			return nil, fmt.Errorf("failed to scan type statistics: %w", err)
		}
		stats.RecordsByType[checksumType] = count
	}

	// Get counts by algorithm
	algorithmSQL := `SELECT algorithm, COUNT(*) FROM ` + s.tableName + ` GROUP BY algorithm`
	rows, err = s.backend.Query(ctx, algorithmSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to query algorithm statistics: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var algorithm ChecksumAlgorithm
		var count int
		if err := rows.Scan(&algorithm, &count); err != nil {
			return nil, fmt.Errorf("failed to scan algorithm statistics: %w", err)
		}
		stats.RecordsByAlgorithm[algorithm] = count
	}

	// Get oldest and newest records
	timeSQL := `SELECT MIN(created_at), MAX(created_at) FROM ` + s.tableName
	row = s.backend.QueryRow(ctx, timeSQL)
	var oldestTime, newestTime sql.NullTime
	if err := row.Scan(&oldestTime, &newestTime); err != nil {
		return nil, fmt.Errorf("failed to get time statistics: %w", err)
	}

	if oldestTime.Valid {
		stats.OldestRecord = &oldestTime.Time
	}
	if newestTime.Valid {
		stats.NewestRecord = &newestTime.Time
	}

	return stats, nil
}

// Cleanup removes old records from the database
func (s *DatabaseChecksumStore) Cleanup(ctx context.Context, olderThan time.Time) (int, error) {
	deleteSQL := `DELETE FROM ` + s.tableName + ` WHERE created_at < ?`
	
	result, err := s.backend.Execute(ctx, deleteSQL, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup records: %w", err)
	}

	// This is a simplified implementation
	// In a real implementation, you would get the affected rows count from the result
	return 0, nil
}

// scanRecord scans a database row into a ChecksumRecord
func (s *DatabaseChecksumStore) scanRecord(scanner interface{}) (*ChecksumRecord, error) {
	record := &ChecksumRecord{}
	var metadataJSON, tagsJSON string

	var err error
	switch v := scanner.(type) {
	case *sql.Row:
		err = v.Scan(&record.ID, &record.Type, &record.Algorithm, &record.Value, &record.Source,
			&metadataJSON, &record.CreatedAt, &record.UpdatedAt, &record.Version, &tagsJSON)
	case *sql.Rows:
		err = v.Scan(&record.ID, &record.Type, &record.Algorithm, &record.Value, &record.Source,
			&metadataJSON, &record.CreatedAt, &record.UpdatedAt, &record.Version, &tagsJSON)
	default:
		return nil, fmt.Errorf("unsupported scanner type")
	}

	if err != nil {
		return nil, err
	}

	// Deserialize metadata
	if metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &record.Metadata); err != nil {
			return nil, fmt.Errorf("failed to deserialize metadata: %w", err)
		}
	}

	// Deserialize tags
	if tagsJSON != "" {
		if err := json.Unmarshal([]byte(tagsJSON), &record.Tags); err != nil {
			return nil, fmt.Errorf("failed to deserialize tags: %w", err)
		}
	}

	return record, nil
}

// StoreFactory creates different types of checksum stores
type StoreFactory struct{}

// NewStoreFactory creates a new store factory
func NewStoreFactory() *StoreFactory {
	return &StoreFactory{}
}

// CreateStore creates a checksum store based on the specified type
func (f *StoreFactory) CreateStore(storeType string, config *ChecksumConfig, options map[string]interface{}) (ChecksumStore, error) {
	switch storeType {
	case "memory":
		return NewMemoryChecksumStore(config), nil
	case "database":
		backend, ok := options["backend"].(backend.DatabaseBackend)
		if !ok {
			return nil, fmt.Errorf("database backend is required for database store")
		}
		return NewDatabaseChecksumStore(backend, config), nil
	default:
		return nil, fmt.Errorf("unknown store type: %s", storeType)
	}
}

// GetSupportedStoreTypes returns supported store types
func (f *StoreFactory) GetSupportedStoreTypes() []string {
	return []string{"memory", "database"}
}

// GetDefaultOptions returns default options for a store type
func (f *StoreFactory) GetDefaultOptions(storeType string) map[string]interface{} {
	switch storeType {
	case "database":
		return map[string]interface{}{
			"table_name": "checksum_records",
		}
	default:
		return map[string]interface{}{}
	}
}

/**
 * @file manager.go
 * @description Checksum manager implementation with high-level operations and caching
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/manager.go
 */

package checksum

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultChecksumManager implements ChecksumManager interface
type DefaultChecksumManager struct {
	generator ChecksumGenerator
	validator ChecksumValidator
	store     ChecksumStore
	config    *ChecksumConfig
	cache     *managerCache
	metrics   ChecksumMetrics
	auditor   ChecksumAuditor
	events    ChecksumEventHandler
	mutex     sync.RWMutex
}

// managerCache provides caching for checksum records
type managerCache struct {
	records map[string]*ChecksumRecord
	mutex   sync.RWMutex
	ttl     time.Duration
	maxSize int
	hits    int64
	misses  int64
}

// NewDefaultChecksumManager creates a new default checksum manager
func NewDefaultChecksumManager(generator ChecksumGenerator, validator ChecksumValidator, store ChecksumStore, config *ChecksumConfig) *DefaultChecksumManager {
	manager := &DefaultChecksumManager{
		generator: generator,
		validator: validator,
		store:     store,
		config:    config,
	}

	if config.EnableCaching {
		manager.cache = &managerCache{
			records: make(map[string]*ChecksumRecord),
			ttl:     config.CacheTTL,
			maxSize: config.CacheSize,
		}
	}

	return manager
}

// SetMetrics sets the metrics collector
func (m *DefaultChecksumManager) SetMetrics(metrics ChecksumMetrics) {
	m.metrics = metrics
}

// SetAuditor sets the auditor
func (m *DefaultChecksumManager) SetAuditor(auditor ChecksumAuditor) {
	m.auditor = auditor
}

// SetEventHandler sets the event handler
func (m *DefaultChecksumManager) SetEventHandler(events ChecksumEventHandler) {
	m.events = events
}

// GenerateAndStore generates a checksum and stores it
func (m *DefaultChecksumManager) GenerateAndStore(ctx context.Context, source string, data []byte, checksumType ChecksumType, algorithm ChecksumAlgorithm) (*ChecksumRecord, error) {
	startTime := time.Now()

	// Generate checksum
	checksum, err := m.generator.Generate(data, algorithm)
	if err != nil {
		m.recordMetrics("generation", algorithm, time.Since(startTime), false)
		return nil, fmt.Errorf("failed to generate checksum: %w", err)
	}

	// Create record
	record := &ChecksumRecord{
		ID:        uuid.New().String(),
		Type:      checksumType,
		Algorithm: algorithm,
		Value:     checksum,
		Source:    source,
		Metadata:  make(map[string]interface{}),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Version:   "1.0",
		Tags:      []string{},
	}

	// Add metadata
	record.Metadata["data_size"] = len(data)
	record.Metadata["generation_time"] = time.Since(startTime).Milliseconds()

	// Store record
	if err := m.store.Store(ctx, record); err != nil {
		m.recordMetrics("storage", algorithm, time.Since(startTime), false)
		return nil, fmt.Errorf("failed to store checksum record: %w", err)
	}

	// Cache record
	if m.cache != nil {
		m.cache.set(source, record)
	}

	// Record metrics
	m.recordMetrics("generation", algorithm, time.Since(startTime), true)
	m.recordMetrics("storage", algorithm, time.Since(startTime), true)

	// Log audit entry
	m.logAuditEntry(ctx, "generate_and_store", record.ID, map[string]interface{}{
		"source":          source,
		"type":            checksumType,
		"algorithm":       algorithm,
		"data_size":       len(data),
		"generation_time": time.Since(startTime).Milliseconds(),
	})

	// Emit event
	m.emitEvent(&ChecksumEvent{
		Type:      "checksum_generated",
		RecordID:  record.ID,
		Source:    source,
		Algorithm: algorithm,
		Status:    StatusValid,
		Message:   "Checksum generated and stored successfully",
		Metadata: map[string]interface{}{
			"data_size": len(data),
		},
		Timestamp: time.Now(),
	})

	return record, nil
}

// ValidateAndUpdate validates a checksum and updates if necessary
func (m *DefaultChecksumManager) ValidateAndUpdate(ctx context.Context, source string, data []byte, checksumType ChecksumType) (*ChecksumValidationResult, error) {
	startTime := time.Now()

	// Retrieve existing records
	records, err := m.store.RetrieveBySource(ctx, source, checksumType)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve checksum records: %w", err)
	}

	if len(records) == 0 {
		return &ChecksumValidationResult{
			Status:         StatusMissing,
			Message:        "No checksum records found for source",
			ValidationTime: time.Now(),
			Suggestions:    []string{"Generate and store checksum for this source"},
		}, nil
	}

	// Use the most recent record
	record := records[0]
	for _, r := range records {
		if r.CreatedAt.After(record.CreatedAt) {
			record = r
		}
	}

	// Validate checksum
	result, err := m.validator.Validate(ctx, record, data)
	if err != nil {
		m.recordMetrics("validation", record.Algorithm, time.Since(startTime), false)
		return nil, fmt.Errorf("failed to validate checksum: %w", err)
	}

	// Update record if validation failed and auto-update is enabled
	if result.Status == StatusInvalid && m.config.EnableCaching {
		// Generate new checksum
		newChecksum, err := m.generator.Generate(data, record.Algorithm)
		if err == nil {
			// Update record
			record.Value = newChecksum
			record.UpdatedAt = time.Now()
			record.Metadata["last_validation"] = time.Now()
			record.Metadata["validation_status"] = string(result.Status)

			if err := m.store.Update(ctx, record); err == nil {
				// Update cache
				if m.cache != nil {
					m.cache.set(source, record)
				}

				// Update result
				result.Status = StatusValid
				result.Message = "Checksum updated successfully"
				result.ActualValue = newChecksum

				// Log audit entry
				m.logAuditEntry(ctx, "checksum_updated", record.ID, map[string]interface{}{
					"source":      source,
					"old_value":   result.ExpectedValue,
					"new_value":   newChecksum,
					"update_time": time.Since(startTime).Milliseconds(),
				})
			}
		}
	}

	// Record metrics
	m.recordMetrics("validation", record.Algorithm, time.Since(startTime), result.Status == StatusValid)

	// Emit event
	m.emitEvent(&ChecksumEvent{
		Type:      "checksum_validated",
		RecordID:  record.ID,
		Source:    source,
		Algorithm: record.Algorithm,
		Status:    result.Status,
		Message:   result.Message,
		Metadata: map[string]interface{}{
			"validation_time": time.Since(startTime).Milliseconds(),
		},
		Timestamp: time.Now(),
	})

	return result, nil
}

// GetOrCreate gets an existing checksum or creates a new one
func (m *DefaultChecksumManager) GetOrCreate(ctx context.Context, source string, data []byte, checksumType ChecksumType, algorithm ChecksumAlgorithm) (*ChecksumRecord, error) {
	// Check cache first
	if m.cache != nil {
		if cached := m.cache.get(source); cached != nil {
			return cached, nil
		}
	}

	// Try to retrieve existing record
	records, err := m.store.RetrieveBySource(ctx, source, checksumType)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve checksum records: %w", err)
	}

	// If record exists, return it
	if len(records) > 0 {
		record := records[0]
		for _, r := range records {
			if r.Algorithm == algorithm && r.CreatedAt.After(record.CreatedAt) {
				record = r
			}
		}

		// Cache and return
		if m.cache != nil {
			m.cache.set(source, record)
		}
		return record, nil
	}

	// Create new record
	return m.GenerateAndStore(ctx, source, data, checksumType, algorithm)
}

// InvalidateCache invalidates cache for a source
func (m *DefaultChecksumManager) InvalidateCache(source string) error {
	if m.cache == nil {
		return nil
	}

	m.cache.delete(source)
	return nil
}

// GetCachedChecksum gets a cached checksum
func (m *DefaultChecksumManager) GetCachedChecksum(source string) (*ChecksumRecord, error) {
	if m.cache == nil {
		return nil, fmt.Errorf("caching is not enabled")
	}

	return m.cache.get(source), nil
}

// BulkValidate validates multiple checksums
func (m *DefaultChecksumManager) BulkValidate(ctx context.Context, sources []string, dataMap map[string][]byte, checksumType ChecksumType) ([]*ChecksumValidationResult, error) {
	if len(sources) == 0 {
		return []*ChecksumValidationResult{}, nil
	}

	// Collect all records
	var allRecords []*ChecksumRecord
	recordMap := make(map[string]*ChecksumRecord)

	for _, source := range sources {
		records, err := m.store.RetrieveBySource(ctx, source, checksumType)
		if err != nil {
			continue // Skip sources with errors
		}

		if len(records) > 0 {
			// Use most recent record
			record := records[0]
			for _, r := range records {
				if r.CreatedAt.After(record.CreatedAt) {
					record = r
				}
			}
			allRecords = append(allRecords, record)
			recordMap[source] = record
		}
	}

	// Prepare data map for validator
	validatorDataMap := make(map[string][]byte)
	for _, record := range allRecords {
		if data, exists := dataMap[record.Source]; exists {
			validatorDataMap[record.Source] = data
		}
	}

	// Validate in batch
	results, err := m.validator.ValidateBatch(ctx, allRecords, validatorDataMap)
	if err != nil {
		return nil, fmt.Errorf("bulk validation failed: %w", err)
	}

	// Emit events for each validation
	for _, result := range results {
		if result.Record != nil {
			m.emitEvent(&ChecksumEvent{
				Type:      "bulk_validation",
				RecordID:  result.Record.ID,
				Source:    result.Record.Source,
				Algorithm: result.Record.Algorithm,
				Status:    result.Status,
				Message:   result.Message,
				Timestamp: time.Now(),
			})
		}
	}

	return results, nil
}

// PerformIntegrityCheck performs a comprehensive integrity check
func (m *DefaultChecksumManager) PerformIntegrityCheck(ctx context.Context) (*IntegrityCheckResult, error) {
	startTime := time.Now()

	result := &IntegrityCheckResult{
		ValidationResults: []*ChecksumValidationResult{},
		Issues:           []string{},
		Recommendations:  []string{},
		CheckTimestamp:   time.Now(),
	}

	// Get all records
	allRecords, err := m.store.List(ctx, &ChecksumFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve all records: %w", err)
	}

	result.TotalChecked = len(allRecords)

	// Check each record
	for _, record := range allRecords {
		// This is a simplified check - in a real implementation,
		// you would need to have access to the original data
		
		// For now, just check if the record is well-formed
		if record.ID == "" {
			result.Issues = append(result.Issues, fmt.Sprintf("Record with empty ID found"))
			result.CorruptedRecords++
			continue
		}

		if record.Value == "" {
			result.Issues = append(result.Issues, fmt.Sprintf("Record %s has empty checksum value", record.ID))
			result.CorruptedRecords++
			continue
		}

		if err := m.generator.ValidateAlgorithm(record.Algorithm); err != nil {
			result.Issues = append(result.Issues, fmt.Sprintf("Record %s has unsupported algorithm: %s", record.ID, record.Algorithm))
			result.InvalidRecords++
			continue
		}

		result.ValidRecords++
	}

	// Generate recommendations
	if result.CorruptedRecords > 0 {
		result.Recommendations = append(result.Recommendations, "Remove or repair corrupted records")
	}
	if result.InvalidRecords > 0 {
		result.Recommendations = append(result.Recommendations, "Update records with unsupported algorithms")
	}
	if float64(result.ValidRecords)/float64(result.TotalChecked) < 0.9 {
		result.Recommendations = append(result.Recommendations, "Consider regenerating checksums for better data integrity")
	}

	result.CheckDuration = time.Since(startTime)

	// Log audit entry
	m.logAuditEntry(ctx, "integrity_check", "", map[string]interface{}{
		"total_checked":     result.TotalChecked,
		"valid_records":     result.ValidRecords,
		"invalid_records":   result.InvalidRecords,
		"corrupted_records": result.CorruptedRecords,
		"check_duration":    result.CheckDuration.Milliseconds(),
	})

	return result, nil
}

// Helper methods
func (m *DefaultChecksumManager) recordMetrics(operation string, algorithm ChecksumAlgorithm, duration time.Duration, success bool) {
	if m.metrics == nil {
		return
	}

	switch operation {
	case "generation":
		m.metrics.RecordGeneration(algorithm, duration, 0)
	case "validation":
		status := StatusValid
		if !success {
			status = StatusInvalid
		}
		m.metrics.RecordValidation(algorithm, status, duration)
	case "storage":
		m.metrics.RecordStorageOperation(operation, duration, success)
	}
}

func (m *DefaultChecksumManager) logAuditEntry(ctx context.Context, operation, recordID string, details map[string]interface{}) {
	if m.auditor != nil {
		m.auditor.LogOperation(ctx, operation, recordID, details)
	}
}

func (m *DefaultChecksumManager) emitEvent(event *ChecksumEvent) {
	if m.events != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m.events.HandleEvent(ctx, event)
		}()
	}
}

// managerCache methods
func (c *managerCache) get(key string) *ChecksumRecord {
	if c == nil {
		return nil
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	record, exists := c.records[key]
	if !exists {
		c.misses++
		return nil
	}

	// Check TTL
	if time.Since(record.UpdatedAt) > c.ttl {
		delete(c.records, key)
		c.misses++
		return nil
	}

	c.hits++
	return record
}

func (c *managerCache) set(key string, record *ChecksumRecord) {
	if c == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check cache size limit
	if len(c.records) >= c.maxSize {
		// Simple LRU: remove oldest entry
		var oldestKey string
		var oldestTime time.Time
		for k, r := range c.records {
			if oldestKey == "" || r.UpdatedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = r.UpdatedAt
			}
		}
		if oldestKey != "" {
			delete(c.records, oldestKey)
		}
	}

	c.records[key] = record
}

func (c *managerCache) delete(key string) {
	if c == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.records, key)
}

func (c *managerCache) clear() {
	if c == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.records = make(map[string]*ChecksumRecord)
	c.hits = 0
	c.misses = 0
}

func (c *managerCache) getStats() (hits, misses int64, hitRate float64) {
	if c == nil {
		return 0, 0, 0
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	hits = c.hits
	misses = c.misses
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return hits, misses, hitRate
}

// ManagerFactory creates different types of checksum managers
type ManagerFactory struct{}

// NewManagerFactory creates a new manager factory
func NewManagerFactory() *ManagerFactory {
	return &ManagerFactory{}
}

// CreateManager creates a checksum manager
func (f *ManagerFactory) CreateManager(generator ChecksumGenerator, validator ChecksumValidator, store ChecksumStore, config *ChecksumConfig) ChecksumManager {
	return NewDefaultChecksumManager(generator, validator, store, config)
}

// CreateDefaultConfig creates a default configuration
func (f *ManagerFactory) CreateDefaultConfig() *ChecksumConfig {
	return &ChecksumConfig{
		DefaultAlgorithm:    AlgorithmSHA256,
		EnableCaching:       true,
		CacheSize:           1000,
		CacheTTL:            time.Hour,
		ValidationTimeout:   30 * time.Second,
		BatchSize:           100,
		EnableCompression:   false,
		EnableEncryption:    false,
		RetentionPeriod:     30 * 24 * time.Hour, // 30 days
		AutoCleanup:         true,
		CleanupInterval:     24 * time.Hour, // Daily
		EnableMetrics:       true,
		EnableAuditLog:      true,
	}
}

/**
 * @file validator.go
 * @description Checksum validator implementation with comprehensive validation logic
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/validator.go
 */

package checksum

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DefaultChecksumValidator implements ChecksumValidator interface
type DefaultChecksumValidator struct {
	generator ChecksumGenerator
	store     ChecksumStore
	config    *ChecksumConfig
	cache     *validationCache
	metrics   ChecksumMetrics
	auditor   ChecksumAuditor
}

// validationCache provides caching for validation results
type validationCache struct {
	cache map[string]*ChecksumValidationResult
	mutex sync.RWMutex
	ttl   time.Duration
}

// NewDefaultChecksumValidator creates a new default checksum validator
func NewDefaultChecksumValidator(generator ChecksumGenerator, store ChecksumStore, config *ChecksumConfig) *DefaultChecksumValidator {
	validator := &DefaultChecksumValidator{
		generator: generator,
		store:     store,
		config:    config,
	}

	if config.EnableCaching {
		validator.cache = &validationCache{
			cache: make(map[string]*ChecksumValidationResult),
			ttl:   config.CacheTTL,
		}
	}

	return validator
}

// SetMetrics sets the metrics collector
func (v *DefaultChecksumValidator) SetMetrics(metrics ChecksumMetrics) {
	v.metrics = metrics
}

// SetAuditor sets the auditor
func (v *DefaultChecksumValidator) SetAuditor(auditor ChecksumAuditor) {
	v.auditor = auditor
}

// Validate validates a checksum against stored values
func (v *DefaultChecksumValidator) Validate(ctx context.Context, record *ChecksumRecord, data []byte) (*ChecksumValidationResult, error) {
	startTime := time.Now()
	
	// Check cache first
	if v.cache != nil {
		cacheKey := v.getCacheKey(record.ID, record.Algorithm)
		if cached := v.cache.get(cacheKey); cached != nil {
			return cached, nil
		}
	}

	// Generate checksum for the provided data
	actualChecksum, err := v.generator.Generate(data, record.Algorithm)
	if err != nil {
		v.recordMetrics(record.Algorithm, StatusUnknown, time.Since(startTime))
		return nil, fmt.Errorf("failed to generate checksum: %w", err)
	}

	// Create validation result
	result := &ChecksumValidationResult{
		Record:         record,
		ExpectedValue:  record.Value,
		ActualValue:    actualChecksum,
		ValidationTime: time.Now(),
	}

	// Compare checksums
	if actualChecksum == record.Value {
		result.Status = StatusValid
		result.Message = "Checksum validation successful"
	} else {
		result.Status = StatusInvalid
		result.Message = fmt.Sprintf("Checksum mismatch: expected %s, got %s", record.Value, actualChecksum)
		result.Suggestions = v.generateSuggestions(record, actualChecksum)
	}

	// Cache result if caching is enabled
	if v.cache != nil {
		cacheKey := v.getCacheKey(record.ID, record.Algorithm)
		v.cache.set(cacheKey, result)
	}

	// Record metrics
	v.recordMetrics(record.Algorithm, result.Status, time.Since(startTime))

	// Log audit entry
	v.logAuditEntry(ctx, "validate_checksum", record.ID, map[string]interface{}{
		"algorithm":       record.Algorithm,
		"status":          result.Status,
		"expected_value":  record.Value,
		"actual_value":    actualChecksum,
		"validation_time": time.Since(startTime).Milliseconds(),
	})

	return result, nil
}

// ValidateMigration validates a migration's checksum
func (v *DefaultChecksumValidator) ValidateMigration(ctx context.Context, migration *types.Migration) (*ChecksumValidationResult, error) {
	if migration == nil {
		return nil, fmt.Errorf("migration cannot be nil")
	}

	// Retrieve stored checksum records for this migration
	records, err := v.store.RetrieveBySource(ctx, migration.ID, ChecksumTypeMigration)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve checksum records: %w", err)
	}

	if len(records) == 0 {
		return &ChecksumValidationResult{
			Status:         StatusMissing,
			Message:        "No checksum records found for migration",
			ValidationTime: time.Now(),
			Suggestions:    []string{"Generate and store checksum for this migration"},
		}, nil
	}

	// Use the most recent record
	record := records[0]
	for _, r := range records {
		if r.CreatedAt.After(record.CreatedAt) {
			record = r
		}
	}

	// Generate checksum for the migration
	migrationChecksum, err := v.generator.GenerateFromMigration(migration, record.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to generate migration checksum: %w", err)
	}

	// Compare the freshly generated checksum against the stored record.
	result := &ChecksumValidationResult{
		Record:         record,
		ExpectedValue:  record.Value,
		ActualValue:    migrationChecksum,
		ValidationTime: time.Now(),
	}

	if migrationChecksum == record.Value {
		result.Status = StatusValid
		result.Message = "Migration checksum validation successful"
	} else {
		result.Status = StatusInvalid
		result.Message = fmt.Sprintf("Migration checksum mismatch: expected %s, got %s", record.Value, migrationChecksum)
		result.Suggestions = v.generateSuggestions(record, migrationChecksum)
	}

	// Record metrics and audit the validation outcome.
	v.recordMetrics(record.Algorithm, result.Status, 0)
	v.logAuditEntry(ctx, "validate_migration", record.ID, map[string]interface{}{
		"algorithm":      record.Algorithm,
		"status":         result.Status,
		"expected_value": record.Value,
		"actual_value":   migrationChecksum,
	})

	return result, nil
}

// ValidateFile validates a file's checksum
func (v *DefaultChecksumValidator) ValidateFile(ctx context.Context, filePath string, expectedChecksum string, algorithm ChecksumAlgorithm) (*ChecksumValidationResult, error) {
	startTime := time.Now()

	// Generate checksum for the file
	actualChecksum, err := v.generator.GenerateFromFile(filePath, algorithm)
	if err != nil {
		v.recordMetrics(algorithm, StatusUnknown, time.Since(startTime))
		return nil, fmt.Errorf("failed to generate file checksum: %w", err)
	}

	// Create validation result
	result := &ChecksumValidationResult{
		ExpectedValue:  expectedChecksum,
		ActualValue:    actualChecksum,
		ValidationTime: time.Now(),
	}

	// Compare checksums
	if actualChecksum == expectedChecksum {
		result.Status = StatusValid
		result.Message = "File checksum validation successful"
	} else {
		result.Status = StatusInvalid
		result.Message = fmt.Sprintf("File checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
		result.Suggestions = []string{
			"Check if the file has been modified",
			"Verify the expected checksum value",
			"Re-generate the checksum if the file is correct",
		}
	}

	// Record metrics
	v.recordMetrics(algorithm, result.Status, time.Since(startTime))

	// Log audit entry
	v.logAuditEntry(ctx, "validate_file", filePath, map[string]interface{}{
		"algorithm":       algorithm,
		"status":          result.Status,
		"expected_value":  expectedChecksum,
		"actual_value":    actualChecksum,
		"validation_time": time.Since(startTime).Milliseconds(),
	})

	return result, nil
}

// ValidateBatch validates multiple checksums in batch
func (v *DefaultChecksumValidator) ValidateBatch(ctx context.Context, records []*ChecksumRecord, dataMap map[string][]byte) ([]*ChecksumValidationResult, error) {
	if len(records) == 0 {
		return []*ChecksumValidationResult{}, nil
	}

	results := make([]*ChecksumValidationResult, 0, len(records))
	errors := make([]error, 0)

	// Process in batches to avoid overwhelming the system
	batchSize := v.config.BatchSize
	if batchSize <= 0 {
		batchSize = 10 // Default batch size
	}

	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batchResults, batchErrors := v.validateBatchChunk(ctx, records[i:end], dataMap)
		results = append(results, batchResults...)
		errors = append(errors, batchErrors...)
	}

	if len(errors) > 0 {
		return results, fmt.Errorf("batch validation completed with %d errors", len(errors))
	}

	return results, nil
}

// validateBatchChunk validates a chunk of records
func (v *DefaultChecksumValidator) validateBatchChunk(ctx context.Context, records []*ChecksumRecord, dataMap map[string][]byte) ([]*ChecksumValidationResult, []error) {
	results := make([]*ChecksumValidationResult, 0, len(records))
	errors := make([]error, 0)

	for _, record := range records {
		data, exists := dataMap[record.Source]
		if !exists {
			errors = append(errors, fmt.Errorf("data not found for source: %s", record.Source))
			continue
		}

		result, err := v.Validate(ctx, record, data)
		if err != nil {
			errors = append(errors, fmt.Errorf("validation failed for record %s: %w", record.ID, err))
			continue
		}

		results = append(results, result)
	}

	return results, errors
}

// GetValidationHistory retrieves validation history for a record
func (v *DefaultChecksumValidator) GetValidationHistory(recordID string) ([]*ChecksumValidationResult, error) {
	// This would typically be implemented with a separate validation history store
	// For now, return empty slice
	return []*ChecksumValidationResult{}, nil
}

// generateSuggestions generates suggestions for failed validations
func (v *DefaultChecksumValidator) generateSuggestions(record *ChecksumRecord, actualChecksum string) []string {
	suggestions := []string{
		"Verify that the source data has not been modified",
		"Check if the correct algorithm was used for validation",
		"Ensure the stored checksum value is correct",
	}

	// Add algorithm-specific suggestions
	switch record.Algorithm {
	case AlgorithmMD5:
		suggestions = append(suggestions, "Consider using a stronger algorithm like SHA256")
	case AlgorithmSHA1:
		suggestions = append(suggestions, "Consider upgrading to SHA256 for better security")
	case AlgorithmCRC32, AlgorithmCRC64:
		suggestions = append(suggestions, "CRC algorithms are for error detection, not security")
	}

	// Add time-based suggestions
	if time.Since(record.CreatedAt) > 24*time.Hour {
		suggestions = append(suggestions, "Consider re-generating the checksum if the data is expected to change")
	}

	return suggestions
}

// getCacheKey generates a cache key for validation results
func (v *DefaultChecksumValidator) getCacheKey(recordID string, algorithm ChecksumAlgorithm) string {
	return fmt.Sprintf("%s:%s", recordID, algorithm)
}

// recordMetrics records validation metrics
func (v *DefaultChecksumValidator) recordMetrics(algorithm ChecksumAlgorithm, status ChecksumStatus, duration time.Duration) {
	if v.metrics != nil {
		v.metrics.RecordValidation(algorithm, status, duration)
	}
}

// logAuditEntry logs an audit entry
func (v *DefaultChecksumValidator) logAuditEntry(ctx context.Context, operation, recordID string, details map[string]interface{}) {
	if v.auditor != nil {
		v.auditor.LogOperation(ctx, operation, recordID, details)
	}
}

// validationCache methods
func (c *validationCache) get(key string) *ChecksumValidationResult {
	if c == nil {
		return nil
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	result, exists := c.cache[key]
	if !exists {
		return nil
	}

	// Check TTL
	if time.Since(result.ValidationTime) > c.ttl {
		delete(c.cache, key)
		return nil
	}

	return result
}

func (c *validationCache) set(key string, result *ChecksumValidationResult) {
	if c == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cache[key] = result
}

func (c *validationCache) clear() {
	if c == nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.cache = make(map[string]*ChecksumValidationResult)
}

// ParallelChecksumValidator provides parallel validation capabilities
type ParallelChecksumValidator struct {
	*DefaultChecksumValidator
	maxWorkers int
}

// NewParallelChecksumValidator creates a new parallel checksum validator
func NewParallelChecksumValidator(generator ChecksumGenerator, store ChecksumStore, config *ChecksumConfig, maxWorkers int) *ParallelChecksumValidator {
	if maxWorkers <= 0 {
		maxWorkers = 4 // Default to 4 workers
	}

	return &ParallelChecksumValidator{
		DefaultChecksumValidator: NewDefaultChecksumValidator(generator, store, config),
		maxWorkers:               maxWorkers,
	}
}

// ValidateBatch validates multiple checksums in parallel
func (v *ParallelChecksumValidator) ValidateBatch(ctx context.Context, records []*ChecksumRecord, dataMap map[string][]byte) ([]*ChecksumValidationResult, error) {
	if len(records) == 0 {
		return []*ChecksumValidationResult{}, nil
	}

	results := make([]*ChecksumValidationResult, len(records))
	errors := make([]error, len(records))
	
	// Create worker pool
	semaphore := make(chan struct{}, v.maxWorkers)
	var wg sync.WaitGroup

	// Process records in parallel
	for i, record := range records {
		wg.Add(1)
		go func(index int, rec *ChecksumRecord) {
			defer wg.Done()
			
			semaphore <- struct{}{} // Acquire worker
			defer func() { <-semaphore }() // Release worker

			data, exists := dataMap[rec.Source]
			if !exists {
				errors[index] = fmt.Errorf("data not found for source: %s", rec.Source)
				return
			}

			result, err := v.DefaultChecksumValidator.Validate(ctx, rec, data)
			if err != nil {
				errors[index] = fmt.Errorf("validation failed for record %s: %w", rec.ID, err)
				return
			}

			results[index] = result
		}(i, record)
	}

	wg.Wait()

	// Filter out nil results and collect errors
	validResults := make([]*ChecksumValidationResult, 0, len(results))
	validationErrors := make([]error, 0)

	for i, result := range results {
		if errors[i] != nil {
			validationErrors = append(validationErrors, errors[i])
		} else if result != nil {
			validResults = append(validResults, result)
		}
	}

	if len(validationErrors) > 0 {
		return validResults, fmt.Errorf("parallel validation completed with %d errors", len(validationErrors))
	}

	return validResults, nil
}

// ValidatorConfig configures the construction of a checksum validator.
type ValidatorConfig struct {
	EnableCaching bool          `json:"enable_caching"`
	CacheSize     int           `json:"cache_size"`
	CacheTTL      time.Duration `json:"cache_ttl"`
	Workers       int           `json:"workers"`
}

// toChecksumConfig translates the validator configuration into the runtime
// ChecksumConfig consumed by the validator implementations.
func (c *ValidatorConfig) toChecksumConfig() *ChecksumConfig {
	if c == nil {
		return &ChecksumConfig{DefaultAlgorithm: AlgorithmSHA256}
	}

	return &ChecksumConfig{
		DefaultAlgorithm: AlgorithmSHA256,
		EnableCaching:    c.EnableCaching,
		CacheSize:        c.CacheSize,
		CacheTTL:         c.CacheTTL,
	}
}

// ValidatorFactory creates different types of checksum validators
type ValidatorFactory struct{}

// NewValidatorFactory creates a new validator factory
func NewValidatorFactory() *ValidatorFactory {
	return &ValidatorFactory{}
}

// CreateValidator creates a checksum validator based on the specified type.
// The validator is provisioned with a default generator and an in-memory store
// derived from the supplied configuration, producing a self-contained validator.
func (f *ValidatorFactory) CreateValidator(validatorType ValidatorType, config *ValidatorConfig) (ChecksumValidator, error) {
	checksumConfig := config.toChecksumConfig()
	generator := NewDefaultChecksumGenerator()
	store := NewMemoryChecksumStore(checksumConfig)

	switch validatorType {
	case ValidatorTypeDefault:
		return NewDefaultChecksumValidator(generator, store, checksumConfig), nil
	case ValidatorTypeParallel:
		maxWorkers := 4 // Default workers
		if config != nil && config.Workers > 0 {
			maxWorkers = config.Workers
		}
		return NewParallelChecksumValidator(generator, store, checksumConfig, maxWorkers), nil
	default:
		return nil, fmt.Errorf("unknown validator type: %s", validatorType)
	}
}

// GetSupportedValidatorTypes returns supported validator types
func (f *ValidatorFactory) GetSupportedValidatorTypes() []ValidatorType {
	return []ValidatorType{ValidatorTypeDefault, ValidatorTypeParallel}
}

// GetDefaultConfig returns default configuration for a validator type
func (f *ValidatorFactory) GetDefaultConfig(validatorType ValidatorType) *ValidatorConfig {
	switch validatorType {
	case ValidatorTypeParallel:
		return &ValidatorConfig{Workers: 4}
	default:
		return &ValidatorConfig{}
	}
}

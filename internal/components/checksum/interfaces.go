/**
 * @file interfaces.go
 * @description Interfaces for checksum algorithms and validation
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/interfaces.go
 */

package checksum

import (
	"context"
	"io"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ChecksumAlgorithm represents different checksum algorithms
type ChecksumAlgorithm string

const (
	AlgorithmMD5        ChecksumAlgorithm = "md5"
	AlgorithmSHA1       ChecksumAlgorithm = "sha1"
	AlgorithmSHA256     ChecksumAlgorithm = "sha256"
	AlgorithmSHA512     ChecksumAlgorithm = "sha512"
	AlgorithmCRC32      ChecksumAlgorithm = "crc32"
	AlgorithmCRC64      ChecksumAlgorithm = "crc64"
	AlgorithmXXHash     ChecksumAlgorithm = "xxhash"
	AlgorithmBlake2b    ChecksumAlgorithm = "blake2b"
	AlgorithmBlake3     ChecksumAlgorithm = "blake3"
	AlgorithmAdler32    ChecksumAlgorithm = "adler32"
)

// ChecksumType represents different types of checksums
type ChecksumType string

const (
	ChecksumTypeMigration ChecksumType = "migration"
	ChecksumTypeSQL       ChecksumType = "sql"
	ChecksumTypeFile      ChecksumType = "file"
	ChecksumTypeData      ChecksumType = "data"
	ChecksumTypeSchema    ChecksumType = "schema"
	ChecksumTypeComposite ChecksumType = "composite"
)

// ChecksumStatus represents the status of checksum validation
type ChecksumStatus string

const (
	StatusValid     ChecksumStatus = "valid"
	StatusInvalid   ChecksumStatus = "invalid"
	StatusMissing   ChecksumStatus = "missing"
	StatusCorrupted ChecksumStatus = "corrupted"
	StatusUnknown   ChecksumStatus = "unknown"
)

// ChecksumRecord represents a stored checksum
type ChecksumRecord struct {
	ID          string            `json:"id"`
	Type        ChecksumType      `json:"type"`
	Algorithm   ChecksumAlgorithm `json:"algorithm"`
	Value       string            `json:"value"`
	Source      string            `json:"source"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Version     string            `json:"version"`
	Tags        []string          `json:"tags"`
}

// ChecksumValidationResult represents the result of checksum validation
type ChecksumValidationResult struct {
	Record          *ChecksumRecord `json:"record"`
	Status          ChecksumStatus  `json:"status"`
	ExpectedValue   string          `json:"expected_value"`
	ActualValue     string          `json:"actual_value"`
	Message         string          `json:"message"`
	ValidationTime  time.Time       `json:"validation_time"`
	Suggestions     []string        `json:"suggestions"`
}

// ChecksumGenerator generates checksums using various algorithms
type ChecksumGenerator interface {
	Generate(data []byte, algorithm ChecksumAlgorithm) (string, error)
	GenerateFromReader(reader io.Reader, algorithm ChecksumAlgorithm) (string, error)
	GenerateFromFile(filePath string, algorithm ChecksumAlgorithm) (string, error)
	GenerateFromMigration(migration *types.Migration, algorithm ChecksumAlgorithm) (string, error)
	GenerateComposite(data [][]byte, algorithm ChecksumAlgorithm) (string, error)
	GetSupportedAlgorithms() []ChecksumAlgorithm
	ValidateAlgorithm(algorithm ChecksumAlgorithm) error
}

// ChecksumValidator validates checksums against stored values
type ChecksumValidator interface {
	Validate(ctx context.Context, record *ChecksumRecord, data []byte) (*ChecksumValidationResult, error)
	ValidateMigration(ctx context.Context, migration *types.Migration) (*ChecksumValidationResult, error)
	ValidateFile(ctx context.Context, filePath string, expectedChecksum string, algorithm ChecksumAlgorithm) (*ChecksumValidationResult, error)
	ValidateBatch(ctx context.Context, records []*ChecksumRecord, dataMap map[string][]byte) ([]*ChecksumValidationResult, error)
	GetValidationHistory(recordID string) ([]*ChecksumValidationResult, error)
}

// ChecksumStore manages checksum storage and retrieval
type ChecksumStore interface {
	Store(ctx context.Context, record *ChecksumRecord) error
	Retrieve(ctx context.Context, id string) (*ChecksumRecord, error)
	RetrieveBySource(ctx context.Context, source string, checksumType ChecksumType) ([]*ChecksumRecord, error)
	Update(ctx context.Context, record *ChecksumRecord) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *ChecksumFilter) ([]*ChecksumRecord, error)
	Exists(ctx context.Context, id string) (bool, error)
	GetStatistics(ctx context.Context) (*ChecksumStatistics, error)
	Cleanup(ctx context.Context, olderThan time.Time) (int, error)
}

// ChecksumManager provides high-level checksum management
type ChecksumManager interface {
	GenerateAndStore(ctx context.Context, source string, data []byte, checksumType ChecksumType, algorithm ChecksumAlgorithm) (*ChecksumRecord, error)
	ValidateAndUpdate(ctx context.Context, source string, data []byte, checksumType ChecksumType) (*ChecksumValidationResult, error)
	GetOrCreate(ctx context.Context, source string, data []byte, checksumType ChecksumType, algorithm ChecksumAlgorithm) (*ChecksumRecord, error)
	InvalidateCache(source string) error
	GetCachedChecksum(source string) (*ChecksumRecord, error)
	BulkValidate(ctx context.Context, sources []string, dataMap map[string][]byte, checksumType ChecksumType) ([]*ChecksumValidationResult, error)
	PerformIntegrityCheck(ctx context.Context) (*IntegrityCheckResult, error)
}

// ChecksumFilter represents filtering criteria for checksum queries
type ChecksumFilter struct {
	Type        *ChecksumType      `json:"type,omitempty"`
	Algorithm   *ChecksumAlgorithm `json:"algorithm,omitempty"`
	Source      *string            `json:"source,omitempty"`
	CreatedAfter *time.Time        `json:"created_after,omitempty"`
	CreatedBefore *time.Time       `json:"created_before,omitempty"`
	Tags        []string           `json:"tags,omitempty"`
	Version     *string            `json:"version,omitempty"`
	Limit       int                `json:"limit,omitempty"`
	Offset      int                `json:"offset,omitempty"`
	SortBy      string             `json:"sort_by,omitempty"`
	SortOrder   string             `json:"sort_order,omitempty"`
}

// ChecksumStatistics provides statistics about stored checksums
type ChecksumStatistics struct {
	TotalRecords        int                            `json:"total_records"`
	RecordsByType       map[ChecksumType]int           `json:"records_by_type"`
	RecordsByAlgorithm  map[ChecksumAlgorithm]int      `json:"records_by_algorithm"`
	OldestRecord        *time.Time                     `json:"oldest_record,omitempty"`
	NewestRecord        *time.Time                     `json:"newest_record,omitempty"`
	AverageSize         float64                        `json:"average_size"`
	StorageUsage        int64                          `json:"storage_usage"`
	ValidationHistory   *ValidationStatistics          `json:"validation_history"`
}

// ValidationStatistics provides statistics about checksum validations
type ValidationStatistics struct {
	TotalValidations    int                            `json:"total_validations"`
	SuccessfulValidations int                          `json:"successful_validations"`
	FailedValidations   int                            `json:"failed_validations"`
	ValidationsByStatus map[ChecksumStatus]int         `json:"validations_by_status"`
	AverageValidationTime float64                      `json:"average_validation_time"`
	LastValidation      *time.Time                     `json:"last_validation,omitempty"`
}

// IntegrityCheckResult represents the result of an integrity check
type IntegrityCheckResult struct {
	TotalChecked        int                            `json:"total_checked"`
	ValidRecords        int                            `json:"valid_records"`
	InvalidRecords      int                            `json:"invalid_records"`
	MissingRecords      int                            `json:"missing_records"`
	CorruptedRecords    int                            `json:"corrupted_records"`
	ValidationResults   []*ChecksumValidationResult    `json:"validation_results"`
	Issues              []string                       `json:"issues"`
	Recommendations     []string                       `json:"recommendations"`
	CheckDuration       time.Duration                  `json:"check_duration"`
	CheckTimestamp      time.Time                      `json:"check_timestamp"`
}

// ChecksumConfig configures checksum operations
type ChecksumConfig struct {
	DefaultAlgorithm    ChecksumAlgorithm `json:"default_algorithm"`
	EnableCaching       bool              `json:"enable_caching"`
	CacheSize           int               `json:"cache_size"`
	CacheTTL            time.Duration     `json:"cache_ttl"`
	ValidationTimeout   time.Duration     `json:"validation_timeout"`
	BatchSize           int               `json:"batch_size"`
	EnableCompression   bool              `json:"enable_compression"`
	EnableEncryption    bool              `json:"enable_encryption"`
	RetentionPeriod     time.Duration     `json:"retention_period"`
	AutoCleanup         bool              `json:"auto_cleanup"`
	CleanupInterval     time.Duration     `json:"cleanup_interval"`
	EnableMetrics       bool              `json:"enable_metrics"`
	EnableAuditLog      bool              `json:"enable_audit_log"`
}

// ChecksumFactory creates checksum-related components
type ChecksumFactory interface {
	CreateGenerator(config *ChecksumConfig) (ChecksumGenerator, error)
	CreateValidator(store ChecksumStore, config *ChecksumConfig) (ChecksumValidator, error)
	CreateStore(storeType string, config *ChecksumConfig) (ChecksumStore, error)
	CreateManager(generator ChecksumGenerator, validator ChecksumValidator, store ChecksumStore, config *ChecksumConfig) (ChecksumManager, error)
	GetSupportedStoreTypes() []string
	GetSupportedAlgorithms() []ChecksumAlgorithm
}

// ChecksumEvent represents events in the checksum system
type ChecksumEvent struct {
	Type        string                 `json:"type"`
	RecordID    string                 `json:"record_id"`
	Source      string                 `json:"source"`
	Algorithm   ChecksumAlgorithm      `json:"algorithm"`
	Status      ChecksumStatus         `json:"status"`
	Message     string                 `json:"message"`
	Metadata    map[string]interface{} `json:"metadata"`
	Timestamp   time.Time              `json:"timestamp"`
	UserID      string                 `json:"user_id,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
}

// ChecksumEventHandler handles checksum events
type ChecksumEventHandler interface {
	HandleEvent(ctx context.Context, event *ChecksumEvent) error
	Subscribe(eventType string, handler func(*ChecksumEvent)) error
	Unsubscribe(eventType string) error
	GetEventHistory(recordID string) ([]*ChecksumEvent, error)
}

// ChecksumMetrics provides metrics for checksum operations
type ChecksumMetrics interface {
	RecordGeneration(algorithm ChecksumAlgorithm, duration time.Duration, size int64)
	RecordValidation(algorithm ChecksumAlgorithm, status ChecksumStatus, duration time.Duration)
	RecordStorageOperation(operation string, duration time.Duration, success bool)
	GetGenerationMetrics() *GenerationMetrics
	GetValidationMetrics() *ValidationMetrics
	GetStorageMetrics() *StorageMetrics
	Reset()
}

// GenerationMetrics provides metrics for checksum generation
type GenerationMetrics struct {
	TotalGenerations    int64                          `json:"total_generations"`
	GenerationsByAlgorithm map[ChecksumAlgorithm]int64 `json:"generations_by_algorithm"`
	AverageGenerationTime map[ChecksumAlgorithm]float64 `json:"average_generation_time"`
	TotalDataProcessed  int64                          `json:"total_data_processed"`
	AverageDataSize     float64                        `json:"average_data_size"`
	LastGeneration      *time.Time                     `json:"last_generation,omitempty"`
}

// ValidationMetrics provides metrics for checksum validation
type ValidationMetrics struct {
	TotalValidations    int64                          `json:"total_validations"`
	ValidationsByStatus map[ChecksumStatus]int64       `json:"validations_by_status"`
	ValidationsByAlgorithm map[ChecksumAlgorithm]int64 `json:"validations_by_algorithm"`
	AverageValidationTime map[ChecksumAlgorithm]float64 `json:"average_validation_time"`
	SuccessRate         float64                        `json:"success_rate"`
	LastValidation      *time.Time                     `json:"last_validation,omitempty"`
}

// StorageMetrics provides metrics for checksum storage operations
type StorageMetrics struct {
	TotalOperations     int64                          `json:"total_operations"`
	OperationsByType    map[string]int64               `json:"operations_by_type"`
	AverageOperationTime map[string]float64            `json:"average_operation_time"`
	SuccessRate         float64                        `json:"success_rate"`
	StorageSize         int64                          `json:"storage_size"`
	LastOperation       *time.Time                     `json:"last_operation,omitempty"`
}

// ChecksumAuditor provides audit capabilities for checksum operations
type ChecksumAuditor interface {
	LogOperation(ctx context.Context, operation string, recordID string, details map[string]interface{}) error
	GetAuditLog(ctx context.Context, filter *AuditFilter) ([]*AuditEntry, error)
	GetAuditStatistics(ctx context.Context) (*AuditStatistics, error)
	ArchiveOldEntries(ctx context.Context, olderThan time.Time) (int, error)
}

// AuditEntry represents an audit log entry
type AuditEntry struct {
	ID          string                 `json:"id"`
	Operation   string                 `json:"operation"`
	RecordID    string                 `json:"record_id"`
	UserID      string                 `json:"user_id,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	Details     map[string]interface{} `json:"details"`
	Timestamp   time.Time              `json:"timestamp"`
	Level       AuditLogLevel          `json:"level"`
	IPAddress   string                 `json:"ip_address,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// AuditFilter represents filtering criteria for audit queries
type AuditFilter struct {
	Operation string    `json:"operation,omitempty"`
	RecordID  string    `json:"record_id,omitempty"`
	UserID    string    `json:"user_id,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Offset    int       `json:"offset,omitempty"`
}

// AuditStatistics provides statistics about audit operations
type AuditStatistics struct {
	TotalEntries         int              `json:"total_entries"`
	EntriesByOperation   map[string]int   `json:"entries_by_operation"`
	EntriesByUser        map[string]int   `json:"entries_by_user"`
	EntriesPerLevel      map[string]int64 `json:"entries_per_level"`
	OldestEntry          *time.Time       `json:"oldest_entry,omitempty"`
	NewestEntry          *time.Time       `json:"newest_entry,omitempty"`
	AverageEntriesPerDay float64          `json:"average_entries_per_day"`
	AverageEntrySize     float64          `json:"average_entry_size"`
	BufferFlushCount     int64            `json:"buffer_flush_count"`
	ErrorCount           int64            `json:"error_count"`
	LastFlushTime        time.Time        `json:"last_flush_time"`
}

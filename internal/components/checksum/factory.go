/**
 * @file factory.go
 * @description Comprehensive checksum factory for creating and configuring all checksum components
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/factory.go
 */

package checksum

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/backend"
)

// ChecksumComponentFactory creates and configures all checksum components
type ChecksumComponentFactory struct {
	config *FactoryConfig
}

// FactoryConfig configuration for the checksum factory
type FactoryConfig struct {
	// Generator settings
	GeneratorType       GeneratorType
	DefaultAlgorithm    ChecksumAlgorithm
	EnableParallel      bool
	ParallelWorkers     int
	BufferSize          int

	// Validator settings
	ValidatorType       ValidatorType
	EnableValidatorCache bool
	CacheSize           int
	CacheTTL            time.Duration

	// Store settings
	StoreType           StoreType
	DatabaseConnection  *sql.DB
	Backend             backend.DatabaseBackend
	TableName           string
	EnableStoreCache    bool

	// Manager settings
	EnableManagerCache  bool
	ManagerCacheSize    int
	ManagerCacheTTL     time.Duration

	// Auditor settings
	EnableAuditing      bool
	AuditStoreType      AuditStoreType
	AuditTableName      string
	AuditBufferSize     int
	AuditFlushInterval  time.Duration
	AuditRetention      time.Duration

	// Metrics settings
	EnableMetrics       bool
	MetricsType         MetricsType

	// Event settings
	EnableEvents        bool
	EventBufferSize     int
}

// Component types
type GeneratorType string
type ValidatorType string
type StoreType string
type AuditStoreType string
type MetricsType string

const (
	// Generator types
	GeneratorTypeDefault  GeneratorType = "default"
	GeneratorTypeFast     GeneratorType = "fast"
	GeneratorTypeParallel GeneratorType = "parallel"

	// Validator types
	ValidatorTypeDefault  ValidatorType = "default"
	ValidatorTypeParallel ValidatorType = "parallel"

	// Store types
	StoreTypeMemory   StoreType = "memory"
	StoreTypeDatabase StoreType = "database"

	// Audit store types
	AuditStoreTypeDatabase AuditStoreType = "database"

	// Metrics types
	MetricsTypeDefault MetricsType = "default"
)

// NewChecksumComponentFactory creates a new checksum component factory
func NewChecksumComponentFactory(config *FactoryConfig) *ChecksumComponentFactory {
	if config == nil {
		config = DefaultFactoryConfig()
	}
	return &ChecksumComponentFactory{config: config}
}

// CreateGenerator creates a checksum generator
func (f *ChecksumComponentFactory) CreateGenerator() (ChecksumGenerator, error) {
	generatorFactory := NewGeneratorFactory()

	switch f.config.GeneratorType {
	case GeneratorTypeDefault:
		return generatorFactory.CreateGenerator(GeneratorTypeDefault, &GeneratorConfig{
			DefaultAlgorithm: f.config.DefaultAlgorithm,
			BufferSize:       f.config.BufferSize,
		})
	case GeneratorTypeFast:
		return generatorFactory.CreateGenerator(GeneratorTypeFast, &GeneratorConfig{
			DefaultAlgorithm: f.config.DefaultAlgorithm,
			BufferSize:       f.config.BufferSize,
		})
	case GeneratorTypeParallel:
		return generatorFactory.CreateGenerator(GeneratorTypeParallel, &GeneratorConfig{
			DefaultAlgorithm: f.config.DefaultAlgorithm,
			BufferSize:       f.config.BufferSize,
			Workers:          f.config.ParallelWorkers,
		})
	default:
		return nil, fmt.Errorf("unsupported generator type: %s", f.config.GeneratorType)
	}
}

// CreateValidator creates a checksum validator
func (f *ChecksumComponentFactory) CreateValidator() (ChecksumValidator, error) {
	validatorFactory := NewValidatorFactory()

	config := &ValidatorConfig{
		EnableCaching: f.config.EnableValidatorCache,
		CacheSize:     f.config.CacheSize,
		CacheTTL:      f.config.CacheTTL,
	}

	switch f.config.ValidatorType {
	case ValidatorTypeDefault:
		return validatorFactory.CreateValidator(ValidatorTypeDefault, config)
	case ValidatorTypeParallel:
		return validatorFactory.CreateValidator(ValidatorTypeParallel, config)
	default:
		return nil, fmt.Errorf("unsupported validator type: %s", f.config.ValidatorType)
	}
}

// CreateStore creates a checksum store
func (f *ChecksumComponentFactory) CreateStore() (ChecksumStore, error) {
	storeFactory := NewStoreFactory()

	switch f.config.StoreType {
	case StoreTypeMemory:
		return storeFactory.CreateStore(StoreTypeMemory, &StoreConfig{
			EnableCaching: f.config.EnableStoreCache,
		})
	case StoreTypeDatabase:
		if f.config.Backend == nil {
			return nil, fmt.Errorf("database backend is required for database store")
		}
		return storeFactory.CreateStore(StoreTypeDatabase, &StoreConfig{
			DatabaseConnection: f.config.DatabaseConnection,
			Backend:            f.config.Backend,
			TableName:          f.config.TableName,
			EnableCaching:      f.config.EnableStoreCache,
		})
	default:
		return nil, fmt.Errorf("unsupported store type: %s", f.config.StoreType)
	}
}

// CreateAuditor creates a checksum auditor
func (f *ChecksumComponentFactory) CreateAuditor() (ChecksumAuditor, error) {
	if !f.config.EnableAuditing {
		return &NoOpAuditor{}, nil
	}

	auditorFactory := NewAuditorFactory()

	// Create audit store
	var auditStore AuditStore
	switch f.config.AuditStoreType {
	case AuditStoreTypeDatabase:
		if f.config.DatabaseConnection == nil {
			return nil, fmt.Errorf("database connection is required for database audit store")
		}
		auditStore = NewDatabaseAuditStore(f.config.DatabaseConnection, f.config.AuditTableName)
	default:
		return nil, fmt.Errorf("unsupported audit store type: %s", f.config.AuditStoreType)
	}

	// Create audit config
	auditConfig := &AuditConfig{
		EnableBuffering:   true,
		BufferSize:        f.config.AuditBufferSize,
		FlushInterval:     f.config.AuditFlushInterval,
		RetentionPeriod:   f.config.AuditRetention,
		EnableCompression: false,
		EnableEncryption:  false,
		LogLevel:          AuditLogLevelInfo,
		IncludeStackTrace: false,
		MaxEntrySize:      1024 * 1024, // 1MB
	}

	return auditorFactory.CreateAuditor(auditStore, auditConfig), nil
}

// CreateMetrics creates checksum metrics
func (f *ChecksumComponentFactory) CreateMetrics() (ChecksumMetrics, error) {
	if !f.config.EnableMetrics {
		return &NoOpMetrics{}, nil
	}

	switch f.config.MetricsType {
	case MetricsTypeDefault:
		return NewDefaultChecksumMetrics(), nil
	default:
		return nil, fmt.Errorf("unsupported metrics type: %s", f.config.MetricsType)
	}
}

// CreateEventHandler creates an event handler
func (f *ChecksumComponentFactory) CreateEventHandler() (ChecksumEventHandler, error) {
	if !f.config.EnableEvents {
		return &NoOpEventHandler{}, nil
	}

	return NewDefaultEventHandler(f.config.EventBufferSize), nil
}

// CreateManager creates a complete checksum manager with all components
func (f *ChecksumComponentFactory) CreateManager() (ChecksumManager, error) {
	// Create all components
	generator, err := f.CreateGenerator()
	if err != nil {
		return nil, fmt.Errorf("failed to create generator: %w", err)
	}

	validator, err := f.CreateValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	store, err := f.CreateStore()
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	auditor, err := f.CreateAuditor()
	if err != nil {
		return nil, fmt.Errorf("failed to create auditor: %w", err)
	}

	metrics, err := f.CreateMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	eventHandler, err := f.CreateEventHandler()
	if err != nil {
		return nil, fmt.Errorf("failed to create event handler: %w", err)
	}

	// Create manager config
	managerConfig := &ChecksumConfig{
		DefaultAlgorithm:    f.config.DefaultAlgorithm,
		EnableCaching:       f.config.EnableManagerCache,
		CacheSize:           f.config.ManagerCacheSize,
		CacheTTL:            f.config.ManagerCacheTTL,
		ValidationTimeout:   30 * time.Second,
		BatchSize:           100,
		EnableCompression:   false,
		EnableEncryption:    false,
		RetentionPeriod:     30 * 24 * time.Hour,
		AutoCleanup:         true,
		CleanupInterval:     24 * time.Hour,
		EnableMetrics:       f.config.EnableMetrics,
		EnableAuditLog:      f.config.EnableAuditing,
	}

	// Create manager
	manager := NewDefaultChecksumManager(generator, validator, store, managerConfig)
	manager.SetMetrics(metrics)
	manager.SetAuditor(auditor)
	manager.SetEventHandler(eventHandler)

	return manager, nil
}

// CreateCompleteSystem creates a complete checksum system
func (f *ChecksumComponentFactory) CreateCompleteSystem() (*ChecksumSystem, error) {
	manager, err := f.CreateManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	generator, err := f.CreateGenerator()
	if err != nil {
		return nil, fmt.Errorf("failed to create generator: %w", err)
	}

	validator, err := f.CreateValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	store, err := f.CreateStore()
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	auditor, err := f.CreateAuditor()
	if err != nil {
		return nil, fmt.Errorf("failed to create auditor: %w", err)
	}

	metrics, err := f.CreateMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	eventHandler, err := f.CreateEventHandler()
	if err != nil {
		return nil, fmt.Errorf("failed to create event handler: %w", err)
	}

	return &ChecksumSystem{
		Manager:      manager,
		Generator:    generator,
		Validator:    validator,
		Store:        store,
		Auditor:      auditor,
		Metrics:      metrics,
		EventHandler: eventHandler,
		Config:       f.config,
	}, nil
}

// ChecksumSystem represents a complete checksum system
type ChecksumSystem struct {
	Manager      ChecksumManager
	Generator    ChecksumGenerator
	Validator    ChecksumValidator
	Store        ChecksumStore
	Auditor      ChecksumAuditor
	Metrics      ChecksumMetrics
	EventHandler ChecksumEventHandler
	Config       *FactoryConfig
}

// DefaultFactoryConfig creates a default factory configuration
func DefaultFactoryConfig() *FactoryConfig {
	return &FactoryConfig{
		// Generator settings
		GeneratorType:       GeneratorTypeDefault,
		DefaultAlgorithm:    AlgorithmSHA256,
		EnableParallel:      false,
		ParallelWorkers:     4,
		BufferSize:          8192,

		// Validator settings
		ValidatorType:        ValidatorTypeDefault,
		EnableValidatorCache: true,
		CacheSize:            1000,
		CacheTTL:             time.Hour,

		// Store settings
		StoreType:        StoreTypeMemory,
		TableName:        "checksum_records",
		EnableStoreCache: true,

		// Manager settings
		EnableManagerCache: true,
		ManagerCacheSize:   1000,
		ManagerCacheTTL:    time.Hour,

		// Auditor settings
		EnableAuditing:     true,
		AuditStoreType:     AuditStoreTypeDatabase,
		AuditTableName:     "checksum_audit",
		AuditBufferSize:    100,
		AuditFlushInterval: 5 * time.Minute,
		AuditRetention:     30 * 24 * time.Hour,

		// Metrics settings
		EnableMetrics: true,
		MetricsType:   MetricsTypeDefault,

		// Event settings
		EnableEvents:    true,
		EventBufferSize: 100,
	}
}

// DatabaseFactoryConfig creates a factory configuration for database-backed components
func DatabaseFactoryConfig(db *sql.DB) *FactoryConfig {
	config := DefaultFactoryConfig()
	config.StoreType = StoreTypeDatabase
	config.DatabaseConnection = db
	return config
}

// HighPerformanceFactoryConfig creates a factory configuration optimized for high performance
func HighPerformanceFactoryConfig() *FactoryConfig {
	config := DefaultFactoryConfig()
	config.GeneratorType = GeneratorTypeParallel
	config.ValidatorType = ValidatorTypeParallel
	config.EnableParallel = true
	config.ParallelWorkers = 8
	config.BufferSize = 16384
	config.CacheSize = 5000
	config.ManagerCacheSize = 5000
	config.AuditBufferSize = 500
	return config
}

// MinimalFactoryConfig creates a minimal factory configuration with basic features
func MinimalFactoryConfig() *FactoryConfig {
	return &FactoryConfig{
		GeneratorType:        GeneratorTypeDefault,
		DefaultAlgorithm:     AlgorithmSHA256,
		ValidatorType:        ValidatorTypeDefault,
		StoreType:            StoreTypeMemory,
		EnableValidatorCache: false,
		EnableStoreCache:     false,
		EnableManagerCache:   false,
		EnableAuditing:       false,
		EnableMetrics:        false,
		EnableEvents:         false,
	}
}

// No-op implementations for disabled features

// NoOpAuditor is a no-op implementation of ChecksumAuditor
type NoOpAuditor struct{}

func (n *NoOpAuditor) LogOperation(ctx context.Context, operation, recordID string, details map[string]interface{}) error {
	return nil
}

func (n *NoOpAuditor) LogValidation(ctx context.Context, recordID string, result *ChecksumValidationResult) error {
	return nil
}

func (n *NoOpAuditor) LogError(ctx context.Context, operation, recordID string, err error, details map[string]interface{}) error {
	return nil
}

func (n *NoOpAuditor) GetAuditTrail(ctx context.Context, recordID string) ([]*AuditEntry, error) {
	return []*AuditEntry{}, nil
}

func (n *NoOpAuditor) GetAuditLog(ctx context.Context, filter *AuditFilter) ([]*AuditEntry, error) {
	return []*AuditEntry{}, nil
}

func (n *NoOpAuditor) GetAuditStatistics(ctx context.Context) (*AuditStatistics, error) {
	return &AuditStatistics{}, nil
}

func (n *NoOpAuditor) CleanupOldEntries(ctx context.Context) error {
	return nil
}

func (n *NoOpAuditor) ArchiveOldEntries(ctx context.Context, olderThan time.Time) (int, error) {
	return 0, nil
}

// NoOpMetrics is a no-op implementation of ChecksumMetrics
type NoOpMetrics struct{}

func (n *NoOpMetrics) RecordGeneration(algorithm ChecksumAlgorithm, duration time.Duration, size int64) {}
func (n *NoOpMetrics) RecordValidation(algorithm ChecksumAlgorithm, status ChecksumStatus, duration time.Duration) {}
func (n *NoOpMetrics) RecordStorageOperation(operation string, duration time.Duration, success bool) {}
func (n *NoOpMetrics) GetGenerationMetrics() *GenerationMetrics { return &GenerationMetrics{} }
func (n *NoOpMetrics) GetValidationMetrics() *ValidationMetrics { return &ValidationMetrics{} }
func (n *NoOpMetrics) GetStorageMetrics() *StorageMetrics { return &StorageMetrics{} }
func (n *NoOpMetrics) Reset() {}

// NoOpEventHandler is a no-op implementation of ChecksumEventHandler
type NoOpEventHandler struct{}

func (n *NoOpEventHandler) HandleEvent(ctx context.Context, event *ChecksumEvent) error {
	return nil
}

func (n *NoOpEventHandler) Subscribe(eventType string, handler func(*ChecksumEvent)) error {
	return nil
}

func (n *NoOpEventHandler) Unsubscribe(eventType string) error {
	return nil
}

func (n *NoOpEventHandler) GetEventHistory(recordID string) ([]*ChecksumEvent, error) {
	return []*ChecksumEvent{}, nil
}

// DefaultChecksumMetrics provides a basic implementation of ChecksumMetrics
type DefaultChecksumMetrics struct {
	generation *GenerationMetrics
	validation *ValidationMetrics
	storage    *StorageMetrics
}

func NewDefaultChecksumMetrics() *DefaultChecksumMetrics {
	return &DefaultChecksumMetrics{
		generation: &GenerationMetrics{},
		validation: &ValidationMetrics{},
		storage:    &StorageMetrics{},
	}
}

func (m *DefaultChecksumMetrics) RecordGeneration(algorithm ChecksumAlgorithm, duration time.Duration, size int64) {
	// Implementation would update generation metrics
}

func (m *DefaultChecksumMetrics) RecordValidation(algorithm ChecksumAlgorithm, status ChecksumStatus, duration time.Duration) {
	// Implementation would update validation metrics
}

func (m *DefaultChecksumMetrics) RecordStorageOperation(operation string, duration time.Duration, success bool) {
	// Implementation would update storage metrics
}

func (m *DefaultChecksumMetrics) GetGenerationMetrics() *GenerationMetrics {
	return m.generation
}

func (m *DefaultChecksumMetrics) GetValidationMetrics() *ValidationMetrics {
	return m.validation
}

func (m *DefaultChecksumMetrics) GetStorageMetrics() *StorageMetrics {
	return m.storage
}

func (m *DefaultChecksumMetrics) Reset() {
	m.generation = &GenerationMetrics{}
	m.validation = &ValidationMetrics{}
	m.storage = &StorageMetrics{}
}

// DefaultEventHandler provides a basic implementation of ChecksumEventHandler
type DefaultEventHandler struct {
	bufferSize int
	mutex      sync.RWMutex
	handlers   map[string][]func(*ChecksumEvent)
	history    map[string][]*ChecksumEvent
}

func NewDefaultEventHandler(bufferSize int) *DefaultEventHandler {
	return &DefaultEventHandler{
		bufferSize: bufferSize,
		handlers:   make(map[string][]func(*ChecksumEvent)),
		history:    make(map[string][]*ChecksumEvent),
	}
}

func (h *DefaultEventHandler) HandleEvent(ctx context.Context, event *ChecksumEvent) error {
	h.mutex.Lock()
	handlers := append([]func(*ChecksumEvent){}, h.handlers[event.Type]...)
	if event.RecordID != "" {
		h.history[event.RecordID] = append(h.history[event.RecordID], event)
	}
	h.mutex.Unlock()

	for _, handler := range handlers {
		go handler(event)
	}
	return nil
}

func (h *DefaultEventHandler) Subscribe(eventType string, handler func(*ChecksumEvent)) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.handlers[eventType] = append(h.handlers[eventType], handler)
	return nil
}

func (h *DefaultEventHandler) Unsubscribe(eventType string) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	delete(h.handlers, eventType)
	return nil
}

// GetEventHistory returns the events recorded for the supplied record ID.
func (h *DefaultEventHandler) GetEventHistory(recordID string) ([]*ChecksumEvent, error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	events := h.history[recordID]
	result := make([]*ChecksumEvent, len(events))
	copy(result, events)
	return result, nil
}

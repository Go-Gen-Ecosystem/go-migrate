/**
 * @file interfaces.go
 * @description Decorator pattern interfaces and base implementations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/decorators/interfaces.go
 */

package decorators

import (
	"context"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/backend"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// BackendDecorator defines the interface for backend decorators
type BackendDecorator interface {
	backend.DatabaseBackend
	
	// GetWrapped returns the wrapped backend
	GetWrapped() backend.DatabaseBackend
	
	// SetWrapped sets the wrapped backend
	SetWrapped(backend.DatabaseBackend)
}

// BaseBackendDecorator provides a base implementation for backend decorators
type BaseBackendDecorator struct {
	wrapped backend.DatabaseBackend
}

// NewBaseBackendDecorator creates a new base backend decorator
func NewBaseBackendDecorator(wrapped backend.DatabaseBackend) *BaseBackendDecorator {
	return &BaseBackendDecorator{
		wrapped: wrapped,
	}
}

// GetWrapped returns the wrapped backend
func (d *BaseBackendDecorator) GetWrapped() backend.DatabaseBackend {
	return d.wrapped
}

// SetWrapped sets the wrapped backend
func (d *BaseBackendDecorator) SetWrapped(wrapped backend.DatabaseBackend) {
	d.wrapped = wrapped
}

// Delegate all methods to the wrapped backend by default

func (d *BaseBackendDecorator) Connect(ctx context.Context, config types.DatabaseConfig) error {
	return d.wrapped.Connect(ctx, config)
}

func (d *BaseBackendDecorator) Disconnect(ctx context.Context) error {
	return d.wrapped.Disconnect(ctx)
}

func (d *BaseBackendDecorator) Ping(ctx context.Context) error {
	return d.wrapped.Ping(ctx)
}

func (d *BaseBackendDecorator) IsConnected() bool {
	return d.wrapped.IsConnected()
}

func (d *BaseBackendDecorator) BeginTransaction(ctx context.Context) (backend.Transaction, error) {
	return d.wrapped.BeginTransaction(ctx)
}

func (d *BaseBackendDecorator) BeginTransactionWithOptions(ctx context.Context, opts backend.TransactionOptions) (backend.Transaction, error) {
	return d.wrapped.BeginTransactionWithOptions(ctx, opts)
}

func (d *BaseBackendDecorator) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	return d.wrapped.Execute(ctx, query, args...)
}

func (d *BaseBackendDecorator) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	return d.wrapped.Query(ctx, query, args...)
}

func (d *BaseBackendDecorator) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	return d.wrapped.QueryRow(ctx, query, args...)
}

func (d *BaseBackendDecorator) CreateSchema(ctx context.Context, schemaName string) error {
	return d.wrapped.CreateSchema(ctx, schemaName)
}

func (d *BaseBackendDecorator) DropSchema(ctx context.Context, schemaName string) error {
	return d.wrapped.DropSchema(ctx, schemaName)
}

func (d *BaseBackendDecorator) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	return d.wrapped.SchemaExists(ctx, schemaName)
}

func (d *BaseBackendDecorator) ListSchemas(ctx context.Context) ([]string, error) {
	return d.wrapped.ListSchemas(ctx)
}

func (d *BaseBackendDecorator) CreateTable(ctx context.Context, tableDef backend.TableDefinition) error {
	return d.wrapped.CreateTable(ctx, tableDef)
}

func (d *BaseBackendDecorator) DropTable(ctx context.Context, tableName string) error {
	return d.wrapped.DropTable(ctx, tableName)
}

func (d *BaseBackendDecorator) TableExists(ctx context.Context, tableName string) (bool, error) {
	return d.wrapped.TableExists(ctx, tableName)
}

func (d *BaseBackendDecorator) ListTables(ctx context.Context) ([]string, error) {
	return d.wrapped.ListTables(ctx)
}

func (d *BaseBackendDecorator) GetTableInfo(ctx context.Context, tableName string) (backend.TableInfo, error) {
	return d.wrapped.GetTableInfo(ctx, tableName)
}

func (d *BaseBackendDecorator) CreateIndex(ctx context.Context, indexDef backend.IndexDefinition) error {
	return d.wrapped.CreateIndex(ctx, indexDef)
}

func (d *BaseBackendDecorator) DropIndex(ctx context.Context, indexName string) error {
	return d.wrapped.DropIndex(ctx, indexName)
}

func (d *BaseBackendDecorator) IndexExists(ctx context.Context, indexName string) (bool, error) {
	return d.wrapped.IndexExists(ctx, indexName)
}

func (d *BaseBackendDecorator) ListIndexes(ctx context.Context, tableName string) ([]backend.IndexInfo, error) {
	return d.wrapped.ListIndexes(ctx, tableName)
}

func (d *BaseBackendDecorator) AddConstraint(ctx context.Context, constraintDef backend.ConstraintDefinition) error {
	return d.wrapped.AddConstraint(ctx, constraintDef)
}

func (d *BaseBackendDecorator) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	return d.wrapped.DropConstraint(ctx, tableName, constraintName)
}

func (d *BaseBackendDecorator) ListConstraints(ctx context.Context, tableName string) ([]backend.ConstraintInfo, error) {
	return d.wrapped.ListConstraints(ctx, tableName)
}

func (d *BaseBackendDecorator) GetDatabaseInfo(ctx context.Context) (backend.DatabaseInfo, error) {
	return d.wrapped.GetDatabaseInfo(ctx)
}

func (d *BaseBackendDecorator) GetVersion(ctx context.Context) (string, error) {
	return d.wrapped.GetVersion(ctx)
}

func (d *BaseBackendDecorator) GetDialect() backend.DatabaseDialect {
	return d.wrapped.GetDialect()
}

func (d *BaseBackendDecorator) AdaptSQL(sql string) string {
	return d.wrapped.AdaptSQL(sql)
}

func (d *BaseBackendDecorator) ValidateSQL(sql string) error {
	return d.wrapped.ValidateSQL(sql)
}

func (d *BaseBackendDecorator) CreateMigrationTable(ctx context.Context, tableName string) error {
	return d.wrapped.CreateMigrationTable(ctx, tableName)
}

func (d *BaseBackendDecorator) CreateLockTable(ctx context.Context, tableName string) error {
	return d.wrapped.CreateLockTable(ctx, tableName)
}

func (d *BaseBackendDecorator) CreateBackup(ctx context.Context, backupConfig backend.BackupConfig) error {
	return d.wrapped.CreateBackup(ctx, backupConfig)
}

func (d *BaseBackendDecorator) RestoreBackup(ctx context.Context, restoreConfig backend.RestoreConfig) error {
	return d.wrapped.RestoreBackup(ctx, restoreConfig)
}

// DecoratorChain manages a chain of decorators
type DecoratorChain struct {
	decorators []BackendDecorator
	backend    backend.DatabaseBackend
}

// NewDecoratorChain creates a new decorator chain
func NewDecoratorChain(backend backend.DatabaseBackend) *DecoratorChain {
	return &DecoratorChain{
		backend:    backend,
		decorators: make([]BackendDecorator, 0),
	}
}

// AddDecorator adds a decorator to the chain
func (c *DecoratorChain) AddDecorator(decorator BackendDecorator) *DecoratorChain {
	c.decorators = append(c.decorators, decorator)
	return c
}

// Build builds the decorator chain and returns the final decorated backend
func (c *DecoratorChain) Build() backend.DatabaseBackend {
	if len(c.decorators) == 0 {
		return c.backend
	}
	
	// Chain decorators from innermost to outermost
	current := c.backend
	for _, decorator := range c.decorators {
		decorator.SetWrapped(current)
		current = decorator
	}
	
	return current
}

// DecoratorFactory creates decorators based on configuration
type DecoratorFactory interface {
	CreateDecorator(config DecoratorConfig) (BackendDecorator, error)
	SupportedTypes() []DecoratorType
}

// DecoratorType represents the type of decorator
type DecoratorType string

const (
	DecoratorTypeLogging   DecoratorType = "logging"
	DecoratorTypeMetrics   DecoratorType = "metrics"
	DecoratorTypeChecksum  DecoratorType = "checksum"
	DecoratorTypeDryRun    DecoratorType = "dry_run"
	DecoratorTypeRetry     DecoratorType = "retry"
	DecoratorTypeCache     DecoratorType = "cache"
	DecoratorTypeValidator DecoratorType = "validator"
	DecoratorTypeAudit     DecoratorType = "audit"
)

// DecoratorConfig holds configuration for decorators
type DecoratorConfig struct {
	Type     DecoratorType          `json:"type"`
	Enabled  bool                   `json:"enabled"`
	Options  map[string]interface{} `json:"options"`
	Priority int                    `json:"priority"` // Lower numbers = higher priority (applied first)
}

// LoggingConfig holds configuration for logging decorator
type LoggingConfig struct {
	Level           string        `json:"level"`
	LogQueries      bool          `json:"log_queries"`
	LogResults      bool          `json:"log_results"`
	LogDuration     bool          `json:"log_duration"`
	SlowQueryThresh time.Duration `json:"slow_query_threshold"`
	SanitizeArgs    bool          `json:"sanitize_args"`
}

// MetricsConfig holds configuration for metrics decorator
type MetricsConfig struct {
	Enabled         bool          `json:"enabled"`
	Namespace       string        `json:"namespace"`
	Subsystem       string        `json:"subsystem"`
	CollectDuration bool          `json:"collect_duration"`
	CollectErrors   bool          `json:"collect_errors"`
	CollectQueries  bool          `json:"collect_queries"`
	HistogramBuckets []float64    `json:"histogram_buckets"`
}

// ChecksumConfig holds configuration for checksum decorator
type ChecksumConfig struct {
	Algorithm    string `json:"algorithm"`    // md5, sha1, sha256, sha512
	ValidateOnly bool   `json:"validate_only"` // Only validate, don't compute
	StoreInDB    bool   `json:"store_in_db"`   // Store checksums in database
	TableName    string `json:"table_name"`    // Table to store checksums
}

// DryRunConfig holds configuration for dry-run decorator
type DryRunConfig struct {
	Enabled       bool     `json:"enabled"`
	LogOperations bool     `json:"log_operations"`
	AllowedOps    []string `json:"allowed_operations"` // Operations allowed in dry-run mode
	BlockedOps    []string `json:"blocked_operations"` // Operations blocked in dry-run mode
}

// RetryConfig holds configuration for retry decorator
type RetryConfig struct {
	MaxAttempts   int           `json:"max_attempts"`
	InitialDelay  time.Duration `json:"initial_delay"`
	MaxDelay      time.Duration `json:"max_delay"`
	BackoffFactor float64       `json:"backoff_factor"`
	RetryableErrors []string    `json:"retryable_errors"` // Error patterns that should trigger retry
}

// CacheConfig holds configuration for cache decorator
type CacheConfig struct {
	Enabled    bool          `json:"enabled"`
	TTL        time.Duration `json:"ttl"`
	MaxSize    int           `json:"max_size"`
	CacheQueries bool        `json:"cache_queries"`
	CacheResults bool        `json:"cache_results"`
	KeyPrefix  string        `json:"key_prefix"`
}

// ValidatorConfig holds configuration for validator decorator
type ValidatorConfig struct {
	Enabled         bool     `json:"enabled"`
	StrictMode      bool     `json:"strict_mode"`
	AllowedPatterns []string `json:"allowed_patterns"`
	BlockedPatterns []string `json:"blocked_patterns"`
	MaxQueryLength  int      `json:"max_query_length"`
}

// AuditConfig holds configuration for audit decorator
type AuditConfig struct {
	Enabled       bool   `json:"enabled"`
	LogFile       string `json:"log_file"`
	LogFormat     string `json:"log_format"` // json, text
	IncludeUser   bool   `json:"include_user"`
	IncludeSource bool   `json:"include_source"`
	IncludeData   bool   `json:"include_data"`
}

// DecoratorRegistry manages decorator factories
type DecoratorRegistry struct {
	factories map[DecoratorType]DecoratorFactory
}

// NewDecoratorRegistry creates a new decorator registry
func NewDecoratorRegistry() *DecoratorRegistry {
	return &DecoratorRegistry{
		factories: make(map[DecoratorType]DecoratorFactory),
	}
}

// RegisterFactory registers a decorator factory
func (r *DecoratorRegistry) RegisterFactory(decoratorType DecoratorType, factory DecoratorFactory) {
	r.factories[decoratorType] = factory
}

// CreateDecorator creates a decorator of the specified type
func (r *DecoratorRegistry) CreateDecorator(config DecoratorConfig) (BackendDecorator, error) {
	factory, exists := r.factories[config.Type]
	if !exists {
		return nil, fmt.Errorf("unknown decorator type: %s", config.Type)
	}
	
	return factory.CreateDecorator(config)
}

// GetSupportedTypes returns all supported decorator types
func (r *DecoratorRegistry) GetSupportedTypes() []DecoratorType {
	var types []DecoratorType
	for decoratorType := range r.factories {
		types = append(types, decoratorType)
	}
	return types
}

// CreateDecoratorChain creates a decorator chain from configuration
func (r *DecoratorRegistry) CreateDecoratorChain(backend backend.DatabaseBackend, configs []DecoratorConfig) (backend.DatabaseBackend, error) {
	// Sort configs by priority (lower numbers = higher priority)
	sortedConfigs := make([]DecoratorConfig, len(configs))
	copy(sortedConfigs, configs)
	
	// Simple bubble sort by priority
	for i := 0; i < len(sortedConfigs); i++ {
		for j := i + 1; j < len(sortedConfigs); j++ {
			if sortedConfigs[i].Priority > sortedConfigs[j].Priority {
				sortedConfigs[i], sortedConfigs[j] = sortedConfigs[j], sortedConfigs[i]
			}
		}
	}
	
	chain := NewDecoratorChain(backend)
	
	for _, config := range sortedConfigs {
		if !config.Enabled {
			continue
		}
		
		decorator, err := r.CreateDecorator(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create decorator %s: %w", config.Type, err)
		}
		
		chain.AddDecorator(decorator)
	}
	
	return chain.Build(), nil
}

// Global decorator registry
var GlobalDecoratorRegistry = NewDecoratorRegistry()

// Helper functions for configuration parsing

// ParseLoggingConfig parses logging configuration from options
func ParseLoggingConfig(options map[string]interface{}) LoggingConfig {
	config := LoggingConfig{
		Level:           "info",
		LogQueries:      true,
		LogResults:      false,
		LogDuration:     true,
		SlowQueryThresh: 1 * time.Second,
		SanitizeArgs:    true,
	}
	
	if level, ok := options["level"].(string); ok {
		config.Level = level
	}
	if logQueries, ok := options["log_queries"].(bool); ok {
		config.LogQueries = logQueries
	}
	if logResults, ok := options["log_results"].(bool); ok {
		config.LogResults = logResults
	}
	if logDuration, ok := options["log_duration"].(bool); ok {
		config.LogDuration = logDuration
	}
	if thresh, ok := options["slow_query_threshold"].(string); ok {
		if duration, err := time.ParseDuration(thresh); err == nil {
			config.SlowQueryThresh = duration
		}
	}
	if sanitize, ok := options["sanitize_args"].(bool); ok {
		config.SanitizeArgs = sanitize
	}
	
	return config
}

// ParseMetricsConfig parses metrics configuration from options
func ParseMetricsConfig(options map[string]interface{}) MetricsConfig {
	config := MetricsConfig{
		Enabled:         true,
		Namespace:       "migration",
		Subsystem:       "backend",
		CollectDuration: true,
		CollectErrors:   true,
		CollectQueries:  true,
		HistogramBuckets: []float64{0.001, 0.01, 0.1, 1, 10},
	}
	
	if enabled, ok := options["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if namespace, ok := options["namespace"].(string); ok {
		config.Namespace = namespace
	}
	if subsystem, ok := options["subsystem"].(string); ok {
		config.Subsystem = subsystem
	}
	if collectDuration, ok := options["collect_duration"].(bool); ok {
		config.CollectDuration = collectDuration
	}
	if collectErrors, ok := options["collect_errors"].(bool); ok {
		config.CollectErrors = collectErrors
	}
	if collectQueries, ok := options["collect_queries"].(bool); ok {
		config.CollectQueries = collectQueries
	}
	
	return config
}

// ParseChecksumConfig parses checksum configuration from options
func ParseChecksumConfig(options map[string]interface{}) ChecksumConfig {
	config := ChecksumConfig{
		Algorithm:    "sha256",
		ValidateOnly: false,
		StoreInDB:    true,
		TableName:    "migration_checksums",
	}
	
	if algorithm, ok := options["algorithm"].(string); ok {
		config.Algorithm = algorithm
	}
	if validateOnly, ok := options["validate_only"].(bool); ok {
		config.ValidateOnly = validateOnly
	}
	if storeInDB, ok := options["store_in_db"].(bool); ok {
		config.StoreInDB = storeInDB
	}
	if tableName, ok := options["table_name"].(string); ok {
		config.TableName = tableName
	}
	
	return config
}

// ParseDryRunConfig parses dry-run configuration from options
func ParseDryRunConfig(options map[string]interface{}) DryRunConfig {
	config := DryRunConfig{
		Enabled:       true,
		LogOperations: true,
		AllowedOps:    []string{"SELECT", "SHOW", "DESCRIBE", "EXPLAIN"},
		BlockedOps:    []string{"DROP", "DELETE", "TRUNCATE", "ALTER"},
	}
	
	if enabled, ok := options["enabled"].(bool); ok {
		config.Enabled = enabled
	}
	if logOps, ok := options["log_operations"].(bool); ok {
		config.LogOperations = logOps
	}
	if allowedOps, ok := options["allowed_operations"].([]interface{}); ok {
		config.AllowedOps = make([]string, len(allowedOps))
		for i, op := range allowedOps {
			if str, ok := op.(string); ok {
				config.AllowedOps[i] = str
			}
		}
	}
	if blockedOps, ok := options["blocked_operations"].([]interface{}); ok {
		config.BlockedOps = make([]string, len(blockedOps))
		for i, op := range blockedOps {
			if str, ok := op.(string); ok {
				config.BlockedOps[i] = str
			}
		}
	}
	
	return config
}

// ParseRetryConfig parses retry configuration from options
func ParseRetryConfig(options map[string]interface{}) RetryConfig {
	config := RetryConfig{
		MaxAttempts:     3,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        30 * time.Second,
		BackoffFactor:   2.0,
		RetryableErrors: []string{"connection", "timeout", "temporary"},
	}
	
	if maxAttempts, ok := options["max_attempts"].(float64); ok {
		config.MaxAttempts = int(maxAttempts)
	}
	if initialDelay, ok := options["initial_delay"].(string); ok {
		if duration, err := time.ParseDuration(initialDelay); err == nil {
			config.InitialDelay = duration
		}
	}
	if maxDelay, ok := options["max_delay"].(string); ok {
		if duration, err := time.ParseDuration(maxDelay); err == nil {
			config.MaxDelay = duration
		}
	}
	if backoffFactor, ok := options["backoff_factor"].(float64); ok {
		config.BackoffFactor = backoffFactor
	}
	if retryableErrors, ok := options["retryable_errors"].([]interface{}); ok {
		config.RetryableErrors = make([]string, len(retryableErrors))
		for i, err := range retryableErrors {
			if str, ok := err.(string); ok {
				config.RetryableErrors[i] = str
			}
		}
	}
	
	return config
}

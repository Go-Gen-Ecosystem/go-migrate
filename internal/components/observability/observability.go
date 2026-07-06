/**
 * @file observability.go
 * @description Comprehensive observability integration for migration operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-27
 * @path pkg/core/database/migrations/internal/observability/observability.go
 */

package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ObservabilityConfig configures observability features
type ObservabilityConfig struct {
	// Logging configuration
	LogLevel    string `yaml:"log_level" json:"log_level"`
	LogFormat   string `yaml:"log_format" json:"log_format"` // "json" or "text"
	LogOutput   string `yaml:"log_output" json:"log_output"` // "stdout", "stderr", or file path
	
	// Metrics configuration
	MetricsEnabled    bool   `yaml:"metrics_enabled" json:"metrics_enabled"`
	MetricsPort       int    `yaml:"metrics_port" json:"metrics_port"`
	MetricsPath       string `yaml:"metrics_path" json:"metrics_path"`
	
	// Tracing configuration
	TracingEnabled    bool   `yaml:"tracing_enabled" json:"tracing_enabled"`
	TracingEndpoint   string `yaml:"tracing_endpoint" json:"tracing_endpoint"`
	TracingSampleRate float64 `yaml:"tracing_sample_rate" json:"tracing_sample_rate"`
	ServiceName       string `yaml:"service_name" json:"service_name"`
	ServiceVersion    string `yaml:"service_version" json:"service_version"`
}

// DefaultObservabilityConfig returns default observability configuration
func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		LogLevel:          "info",
		LogFormat:         "json",
		LogOutput:         "stdout",
		MetricsEnabled:    true,
		MetricsPort:       9090,
		MetricsPath:       "/metrics",
		TracingEnabled:    false,
		TracingSampleRate: 0.1,
		ServiceName:       "go-gen-migrations",
		ServiceVersion:    "1.0.0",
	}
}

// TracerInterface defines the interface for migration tracing
type TracerInterface interface {
	StartMigrationSpan(ctx context.Context, migrationID, operation string) (context.Context, trace.Span)
	StartValidationSpan(ctx context.Context, migrationID string) (context.Context, trace.Span)
	StartLockSpan(ctx context.Context, lockID, operation string) (context.Context, trace.Span)
	StartDriftDetectionSpan(ctx context.Context) (context.Context, trace.Span)
	StartChecksumValidationSpan(ctx context.Context, migrationID string) (context.Context, trace.Span)
	StartDatabaseQuerySpan(ctx context.Context, queryType, driver, dbName string) (context.Context, trace.Span)
	StartFileOperationSpan(ctx context.Context, operation, filePath string) (context.Context, trace.Span)
	RecordMigrationSuccess(span trace.Span, migrationID, version, name, checksum, state string)
	RecordMigrationError(span trace.Span, err error, errorType string)
	RecordLockSuccess(span trace.Span, duration time.Duration)
	RecordLockError(span trace.Span, err error, timeout time.Duration)
	RecordDriftDetection(span trace.Span, driftType string, count int, migrations []string)
}

// MigrationObserver provides comprehensive observability for migration operations
type MigrationObserver struct {
	logger  *MigrationLogger
	metrics MetricsCollector
	tracer  TracerInterface
	config  *ObservabilityConfig
}

// NewMigrationObserver creates a new migration observer
func NewMigrationObserver(config *ObservabilityConfig, baseLogger logging.Logger) (*MigrationObserver, error) {
	if config == nil {
		config = DefaultObservabilityConfig()
	}
	
	// Initialize logger
	migrationLogger := NewMigrationLogger(baseLogger)
	
	// Initialize metrics collector
	var metricsCollector MetricsCollector
	if config.MetricsEnabled {
		metricsCollector = NewInMemoryMetricsCollector()
	} else {
		metricsCollector = &NoOpMetricsCollector{}
	}
	
	// Initialize tracer
	var migrationTracer TracerInterface
	if config.TracingEnabled {
		migrationTracer = NewMigrationTracer()
	} else {
		migrationTracer = &NoOpMigrationTracer{}
	}
	
	return &MigrationObserver{
		logger:  migrationLogger,
		metrics: metricsCollector,
		tracer:  migrationTracer,
		config:  config,
	}, nil
}

// Logger returns the migration logger
func (o *MigrationObserver) Logger() *MigrationLogger {
	return o.logger
}

// Metrics returns the metrics collector
func (o *MigrationObserver) Metrics() MetricsCollector {
	return o.metrics
}

// Tracer returns the migration tracer
func (o *MigrationObserver) Tracer() TracerInterface {
	return o.tracer
}

// Config returns the observability configuration
func (o *MigrationObserver) Config() *ObservabilityConfig {
	return o.config
}

// ObserveMigrationExecution provides comprehensive observability for migration execution
func (o *MigrationObserver) ObserveMigrationExecution(ctx context.Context, migrationID, operation string) *MigrationExecutionObserver {
	// Start tracing
	tracedCtx, span := o.tracer.StartMigrationSpan(ctx, migrationID, operation)
	
	// Log migration start
	o.logger.LogMigrationStart(tracedCtx, migrationID, "", operation)
	
	return &MigrationExecutionObserver{
		ctx:         tracedCtx,
		span:        span,
		observer:    o,
		migrationID: migrationID,
		operation:   operation,
		startTime:   time.Now(),
	}
}

// ObserveLockOperation provides observability for lock operations
func (o *MigrationObserver) ObserveLockOperation(ctx context.Context, lockID, operation string) *LockOperationObserver {
	// Start tracing
	tracedCtx, span := o.tracer.StartLockSpan(ctx, lockID, operation)
	
	// Log lock operation start
	o.logger.LogLockAcquisition(tracedCtx, lockID, true, 0)
	
	return &LockOperationObserver{
		ctx:       tracedCtx,
		span:      span,
		observer:  o,
		lockID:    lockID,
		operation: operation,
		startTime: time.Now(),
	}
}

// ObserveDriftDetection provides observability for drift detection
func (o *MigrationObserver) ObserveDriftDetection(ctx context.Context) *DriftDetectionObserver {
	// Start tracing
	tracedCtx, span := o.tracer.StartDriftDetectionSpan(ctx)
	
	// Log drift detection start
	o.logger.LogSystemEvent(tracedCtx, "drift_detection_started", map[string]interface{}{
		"timestamp": time.Now(),
	})
	
	return &DriftDetectionObserver{
		ctx:       tracedCtx,
		span:      span,
		observer:  o,
		startTime: time.Now(),
	}
}

// MigrationExecutionObserver observes a single migration execution
type MigrationExecutionObserver struct {
	ctx         context.Context
	span        trace.Span
	observer    *MigrationObserver
	migrationID string
	operation   string
	startTime   time.Time
}

// Context returns the traced context
func (m *MigrationExecutionObserver) Context() context.Context {
	return m.ctx
}

// Success records successful migration execution
func (m *MigrationExecutionObserver) Success(version, name, checksum, state string) {
	duration := time.Since(m.startTime)
	
	// Record metrics
	m.observer.metrics.RecordMigrationDuration(m.migrationID, m.operation, duration)
	m.observer.metrics.RecordMigrationSuccess(m.migrationID, m.operation)
	
	// Record tracing
	m.observer.tracer.RecordMigrationSuccess(m.span, m.migrationID, version, name, checksum, state)
	
	// Log success
	m.observer.logger.LogMigrationSuccess(m.ctx, m.migrationID, version, m.operation, duration)
}

// Error records migration execution error
func (m *MigrationExecutionObserver) Error(err error, errorType string) {
	duration := time.Since(m.startTime)
	
	// Record metrics
	m.observer.metrics.RecordMigrationDuration(m.migrationID, m.operation, duration)
	m.observer.metrics.RecordMigrationFailure(m.migrationID, m.operation, errorType)
	
	// Record tracing
	m.observer.tracer.RecordMigrationError(m.span, err, errorType)
	
	// Log error
	m.observer.logger.LogMigrationFailure(m.ctx, m.migrationID, "", m.operation, duration, err)
}

// AddEvent adds an event to the migration execution
func (m *MigrationExecutionObserver) AddEvent(name string, attrs map[string]interface{}) {
	// Convert to OpenTelemetry attributes
	otelAttrs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		otelAttrs = append(otelAttrs, attribute.String(k, fmt.Sprintf("%v", v)))
	}
	
	m.span.AddEvent(name, trace.WithAttributes(otelAttrs...))
	
	// Log event
	m.observer.logger.LogSystemEvent(m.ctx, fmt.Sprintf("migration_%s", name), attrs)
}

// End finishes the migration execution observation
func (m *MigrationExecutionObserver) End() {
	m.span.End()
}

// LockOperationObserver observes lock operations
type LockOperationObserver struct {
	ctx       context.Context
	span      trace.Span
	observer  *MigrationObserver
	lockID    string
	operation string
	startTime time.Time
}

// Context returns the traced context
func (l *LockOperationObserver) Context() context.Context {
	return l.ctx
}

// Success records successful lock operation
func (l *LockOperationObserver) Success() {
	duration := time.Since(l.startTime)
	
	// Record metrics
	if l.operation == "acquire" {
		l.observer.metrics.RecordLockAcquisitionTime(l.lockID, duration)
	} else {
		l.observer.metrics.RecordLockHoldTime(l.lockID, duration)
	}
	
	// Record tracing
	l.observer.tracer.RecordLockSuccess(l.span, duration)
	
	// Log success
	if l.operation == "acquire" {
		l.observer.logger.LogLockAcquisition(l.ctx, l.lockID, true, duration)
	} else {
		l.observer.logger.LogLockRelease(l.ctx, l.lockID, true, duration)
	}
}

// Error records lock operation error
func (l *LockOperationObserver) Error(err error, timeout time.Duration) {
	// Record metrics
	if l.operation == "acquire" {
		l.observer.metrics.RecordLockAcquisitionFailure(l.lockID)
	}
	
	// Record tracing
	l.observer.tracer.RecordLockError(l.span, err, timeout)
	
	// Log error
	if l.operation == "acquire" {
		l.observer.logger.LogLockAcquisition(l.ctx, l.lockID, false, time.Since(l.startTime))
	} else {
		l.observer.logger.LogLockRelease(l.ctx, l.lockID, false, time.Since(l.startTime))
	}
}

// End finishes the lock operation observation
func (l *LockOperationObserver) End() {
	l.span.End()
}

// DriftDetectionObserver observes drift detection operations
type DriftDetectionObserver struct {
	ctx       context.Context
	span      trace.Span
	observer  *MigrationObserver
	startTime time.Time
}

// Context returns the traced context
func (d *DriftDetectionObserver) Context() context.Context {
	return d.ctx
}

// RecordDrift records detected drift
func (d *DriftDetectionObserver) RecordDrift(driftType string, migrations []string) {
	// Record metrics
	for _, migrationID := range migrations {
		d.observer.metrics.RecordDriftDetection(migrationID, driftType)
	}
	
	// Record tracing
	d.observer.tracer.RecordDriftDetection(d.span, driftType, len(migrations), migrations)
	
	// Log drift
	d.observer.logger.LogSystemEvent(d.ctx, "drift_detected", map[string]interface{}{
		"drift_type":  driftType,
		"drift_count": len(migrations),
		"migrations":  migrations,
	})
}

// Success records successful drift detection (no drift found)
func (d *DriftDetectionObserver) Success() {
	// Record tracing
	d.observer.tracer.RecordDriftDetection(d.span, "none", 0, nil)
	
	// Log success
	d.observer.logger.LogSystemEvent(d.ctx, "drift_detection_completed", map[string]interface{}{
		"drift_found": false,
		"duration":    time.Since(d.startTime),
	})
}

// End finishes the drift detection observation
func (d *DriftDetectionObserver) End() {
	d.span.End()
}

// NoOpMigrationTracer is a no-op implementation of MigrationTracer
type NoOpMigrationTracer struct{}

func (n *NoOpMigrationTracer) StartMigrationSpan(ctx context.Context, migrationID, operation string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) StartValidationSpan(ctx context.Context, migrationID string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) StartLockSpan(ctx context.Context, lockID, operation string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) StartDriftDetectionSpan(ctx context.Context) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) StartChecksumValidationSpan(ctx context.Context, migrationID string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) StartDatabaseQuerySpan(ctx context.Context, queryType, driver, dbName string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) StartFileOperationSpan(ctx context.Context, operation, filePath string) (context.Context, trace.Span) {
	return ctx, trace.SpanFromContext(ctx)
}

func (n *NoOpMigrationTracer) RecordMigrationSuccess(span trace.Span, migrationID, version, name, checksum, state string) {}

func (n *NoOpMigrationTracer) RecordMigrationError(span trace.Span, err error, errorType string) {}

func (n *NoOpMigrationTracer) RecordLockSuccess(span trace.Span, duration time.Duration) {}

func (n *NoOpMigrationTracer) RecordLockError(span trace.Span, err error, timeout time.Duration) {}

func (n *NoOpMigrationTracer) RecordDriftDetection(span trace.Span, driftType string, count int, migrations []string) {}

// NoOpMetricsCollector is a no-op implementation of MetricsCollector
type NoOpMetricsCollector struct{}

func (n *NoOpMetricsCollector) RecordMigrationDuration(migrationID, operation string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordMigrationSuccess(migrationID, operation string) {}
func (n *NoOpMetricsCollector) RecordMigrationFailure(migrationID, operation string, errorType string) {}
func (n *NoOpMetricsCollector) RecordLockAcquisitionTime(lockID string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordLockAcquisitionFailure(lockID string) {}
func (n *NoOpMetricsCollector) RecordLockHoldTime(lockID string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordActiveConnections(count int) {}
func (n *NoOpMetricsCollector) RecordPendingMigrations(count int) {}
func (n *NoOpMetricsCollector) RecordAppliedMigrations(count int) {}
func (n *NoOpMetricsCollector) RecordDriftDetection(migrationID string, driftType string) {}
func (n *NoOpMetricsCollector) RecordChecksumValidation(migrationID string, valid bool) {}
func (n *NoOpMetricsCollector) RecordDatabaseQueryTime(operation string, duration time.Duration) {}
func (n *NoOpMetricsCollector) RecordMemoryUsage(bytes int64) {}
func (n *NoOpMetricsCollector) GetMetrics(ctx context.Context) (*MetricsSnapshot, error) {
	return &MetricsSnapshot{Timestamp: time.Now()}, nil
}
func (n *NoOpMetricsCollector) Reset() {}

/**
 * @file manager.go
 * @description ObservabilityManager with MetricsCollector, TracingManager, and structured Logger components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/observability/manager.go
 */

package observability

import (
	"context"
	"log/slog"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// MetricsCollector interface is defined in metrics.go

// TracingManager defines the interface for distributed tracing
type TracingManager interface {
	StartSpan(ctx context.Context, operationName string) (context.Context, func())
	AddSpanAttribute(ctx context.Context, key string, value interface{})
	RecordError(ctx context.Context, err error)
}

// Logger defines the interface for structured logging
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, err error, args ...interface{})
	Debug(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	With(args ...interface{}) Logger
}

// ObservabilityManager coordinates metrics, tracing, and logging
type ObservabilityManager struct {
	metrics MetricsCollector
	tracing TracingManager
	logger  Logger
	enabled bool
}

// NewObservabilityManager creates a new observability manager
func NewObservabilityManager(metrics MetricsCollector, tracing TracingManager, logger Logger) *ObservabilityManager {
	return &ObservabilityManager{
		metrics: metrics,
		tracing: tracing,
		logger:  logger,
		enabled: true,
	}
}

// NewDefaultObservabilityManager creates a manager with default implementations
func NewDefaultObservabilityManager() *ObservabilityManager {
	return &ObservabilityManager{
		metrics: NewDefaultMetricsCollector(),
		tracing: NewNoOpTracingManager(),
		logger:  NewSlogLogger(),
		enabled: true,
	}
}

// SetEnabled enables or disables observability
func (om *ObservabilityManager) SetEnabled(enabled bool) {
	om.enabled = enabled
}

// IsEnabled returns whether observability is enabled
func (om *ObservabilityManager) IsEnabled() bool {
	return om.enabled
}

// RecordMigrationStart records the start of a migration
func (om *ObservabilityManager) RecordMigrationStart(ctx context.Context, migration *types.Migration) (context.Context, func()) {
	if !om.enabled {
		return ctx, func() {}
	}

	om.logger.Info("Migration started", "migration_id", migration.ID, "version", migration.Version)
	return om.tracing.StartSpan(ctx, "migration.execute")
}

// RecordMigrationEnd records the end of a migration
func (om *ObservabilityManager) RecordMigrationEnd(ctx context.Context, migration *types.Migration, duration time.Duration, err error) {
	if !om.enabled {
		return
	}

	operation := "apply" // Default operation type
	if err != nil {
		errorType := "unknown"
		if err != nil {
			errorType = "execution_error"
		}
		om.metrics.RecordMigrationFailure(migration.ID, operation, errorType)
		om.tracing.RecordError(ctx, err)
		om.logger.Error("Migration failed", err, "migration_id", migration.ID, "duration", duration)
	} else {
		om.metrics.RecordMigrationSuccess(migration.ID, operation)
		om.logger.Info("Migration completed", "migration_id", migration.ID, "duration", duration)
	}
	
	om.metrics.RecordMigrationDuration(migration.ID, operation, duration)
}

// RecordLockOperation records lock acquisition/release operations
func (om *ObservabilityManager) RecordLockOperation(operation string, duration time.Duration, err error) {
	if !om.enabled {
		return
	}

	lockID := "migration_lock" // Default lock ID
	if err != nil {
		om.logger.Error("Lock operation failed", err, "operation", operation, "duration", duration)
		if operation == "acquire" {
			om.metrics.RecordLockAcquisitionFailure(lockID)
		}
	} else {
		om.logger.Debug("Lock operation completed", "operation", operation, "duration", duration)
		if operation == "acquire" {
			om.metrics.RecordLockAcquisitionTime(lockID, duration)
		} else if operation == "release" {
			om.metrics.RecordLockHoldTime(lockID, duration)
		}
	}
}

// RecordValidation records validation operations
func (om *ObservabilityManager) RecordValidation(duration time.Duration, errors []string) {
	if !om.enabled {
		return
	}

	// Record as database query time for validation operations
	om.metrics.RecordDatabaseQueryTime("validation", duration)
	if len(errors) > 0 {
		om.logger.Warn("Validation completed with errors", "error_count", len(errors), "duration", duration)
	} else {
		om.logger.Debug("Validation completed successfully", "duration", duration)
	}
}

// GetMetrics returns collected metrics
func (om *ObservabilityManager) GetMetrics(ctx context.Context) (*MetricsSnapshot, error) {
	if !om.enabled || om.metrics == nil {
		return &MetricsSnapshot{Timestamp: time.Now()}, nil
	}
	return om.metrics.GetMetrics(ctx)
}

// Logger returns the logger instance
func (om *ObservabilityManager) Logger() Logger {
	return om.logger
}

// DefaultMetricsCollector provides a simple in-memory metrics collector
// that implements the comprehensive MetricsCollector interface
type DefaultMetricsCollector struct {
	// Migration metrics
	migrationExecutions   int64
	migrationSuccesses    int64
	migrationFailures     int64
	migrationDurations    []time.Duration
	executionsByOperation map[string]int64
	failuresByErrorType   map[string]int64
	
	// Lock metrics
	lockAcquisitions        int64
	lockAcquisitionFailures int64
	lockAcquisitionTimes    []time.Duration
	lockHoldTimes           []time.Duration
	
	// System metrics
	activeConnections   int64
	pendingMigrations   int64
	appliedMigrations   int64
	driftDetections     int64
	checksumValidations int64
	checksumFailures    int64
	
	// Performance metrics
	queryTimes  map[string][]time.Duration
	memoryUsage int64
	
	startTime time.Time
}

// NewDefaultMetricsCollector creates a new default metrics collector
func NewDefaultMetricsCollector() *DefaultMetricsCollector {
	return &DefaultMetricsCollector{
		executionsByOperation: make(map[string]int64),
		failuresByErrorType:   make(map[string]int64),
		lockAcquisitionTimes:  make([]time.Duration, 0),
		lockHoldTimes:         make([]time.Duration, 0),
		migrationDurations:    make([]time.Duration, 0),
		queryTimes:            make(map[string][]time.Duration),
		startTime:             time.Now(),
	}
}

func (dmc *DefaultMetricsCollector) RecordMigrationDuration(migrationID, operation string, duration time.Duration) {
	dmc.migrationExecutions++
	dmc.migrationDurations = append(dmc.migrationDurations, duration)
	dmc.executionsByOperation[operation]++
}

func (dmc *DefaultMetricsCollector) RecordMigrationSuccess(migrationID, operation string) {
	dmc.migrationSuccesses++
}

func (dmc *DefaultMetricsCollector) RecordMigrationFailure(migrationID, operation string, errorType string) {
	dmc.migrationFailures++
	dmc.failuresByErrorType[errorType]++
}

func (dmc *DefaultMetricsCollector) RecordLockAcquisitionTime(lockID string, duration time.Duration) {
	dmc.lockAcquisitions++
	dmc.lockAcquisitionTimes = append(dmc.lockAcquisitionTimes, duration)
}

func (dmc *DefaultMetricsCollector) RecordLockAcquisitionFailure(lockID string) {
	dmc.lockAcquisitionFailures++
}

func (dmc *DefaultMetricsCollector) RecordLockHoldTime(lockID string, duration time.Duration) {
	dmc.lockHoldTimes = append(dmc.lockHoldTimes, duration)
}

func (dmc *DefaultMetricsCollector) RecordActiveConnections(count int) {
	dmc.activeConnections = int64(count)
}

func (dmc *DefaultMetricsCollector) RecordPendingMigrations(count int) {
	dmc.pendingMigrations = int64(count)
}

func (dmc *DefaultMetricsCollector) RecordAppliedMigrations(count int) {
	dmc.appliedMigrations = int64(count)
}

func (dmc *DefaultMetricsCollector) RecordDriftDetection(migrationID string, driftType string) {
	dmc.driftDetections++
}

func (dmc *DefaultMetricsCollector) RecordChecksumValidation(migrationID string, valid bool) {
	dmc.checksumValidations++
	if !valid {
		dmc.checksumFailures++
	}
}

func (dmc *DefaultMetricsCollector) RecordDatabaseQueryTime(operation string, duration time.Duration) {
	if dmc.queryTimes[operation] == nil {
		dmc.queryTimes[operation] = make([]time.Duration, 0)
	}
	dmc.queryTimes[operation] = append(dmc.queryTimes[operation], duration)
}

func (dmc *DefaultMetricsCollector) RecordMemoryUsage(bytes int64) {
	dmc.memoryUsage = bytes
}

func (dmc *DefaultMetricsCollector) GetMetrics(ctx context.Context) (*MetricsSnapshot, error) {
	// Calculate averages and aggregates
	var avgExecutionTime time.Duration
	if len(dmc.migrationDurations) > 0 {
		var total time.Duration
		for _, d := range dmc.migrationDurations {
			total += d
		}
		avgExecutionTime = total / time.Duration(len(dmc.migrationDurations))
	}
	
	var avgAcquisitionTime time.Duration
	if len(dmc.lockAcquisitionTimes) > 0 {
		var total time.Duration
		for _, d := range dmc.lockAcquisitionTimes {
			total += d
		}
		avgAcquisitionTime = total / time.Duration(len(dmc.lockAcquisitionTimes))
	}
	
	var avgHoldTime, maxHoldTime time.Duration
	if len(dmc.lockHoldTimes) > 0 {
		var total time.Duration
		for _, d := range dmc.lockHoldTimes {
			total += d
			if d > maxHoldTime {
				maxHoldTime = d
			}
		}
		avgHoldTime = total / time.Duration(len(dmc.lockHoldTimes))
	}
	
	// Calculate success rate
	var successRate float64
	if dmc.migrationExecutions > 0 {
		successRate = float64(dmc.migrationSuccesses) / float64(dmc.migrationExecutions)
	}
	
	// Calculate query time averages
	queryTimeAverages := make(map[string]time.Duration)
	for operation, times := range dmc.queryTimes {
		if len(times) > 0 {
			var total time.Duration
			for _, t := range times {
				total += t
			}
			queryTimeAverages[operation] = total / time.Duration(len(times))
		}
	}
	
	return &MetricsSnapshot{
		Timestamp: time.Now(),
		MigrationMetrics: MigrationMetrics{
			TotalExecutions:       dmc.migrationExecutions,
			SuccessfulExecutions:  dmc.migrationSuccesses,
			FailedExecutions:      dmc.migrationFailures,
			SuccessRate:           successRate,
			AverageExecutionTime:  avgExecutionTime,
			ExecutionsByOperation: dmc.executionsByOperation,
			FailuresByErrorType:   dmc.failuresByErrorType,
		},
		LockMetrics: LockMetrics{
			TotalAcquisitions:      dmc.lockAcquisitions,
			FailedAcquisitions:     dmc.lockAcquisitionFailures,
			AverageAcquisitionTime: avgAcquisitionTime,
			AverageHoldTime:        avgHoldTime,
			MaxHoldTime:            maxHoldTime,
		},
		SystemMetrics: SystemMetrics{
			ActiveConnections:   dmc.activeConnections,
			PendingMigrations:   dmc.pendingMigrations,
			AppliedMigrations:   dmc.appliedMigrations,
			DriftDetections:     dmc.driftDetections,
			ChecksumValidations: dmc.checksumValidations,
			ChecksumFailures:    dmc.checksumFailures,
		},
		PerformanceMetrics: PerformanceMetrics{
			DatabaseQueryTimes: queryTimeAverages,
			MemoryUsage:        dmc.memoryUsage,
		},
	}, nil
}

func (dmc *DefaultMetricsCollector) Reset() {
	dmc.migrationExecutions = 0
	dmc.migrationSuccesses = 0
	dmc.migrationFailures = 0
	dmc.migrationDurations = make([]time.Duration, 0)
	dmc.executionsByOperation = make(map[string]int64)
	dmc.failuresByErrorType = make(map[string]int64)
	dmc.lockAcquisitions = 0
	dmc.lockAcquisitionFailures = 0
	dmc.lockAcquisitionTimes = make([]time.Duration, 0)
	dmc.lockHoldTimes = make([]time.Duration, 0)
	dmc.activeConnections = 0
	dmc.pendingMigrations = 0
	dmc.appliedMigrations = 0
	dmc.driftDetections = 0
	dmc.checksumValidations = 0
	dmc.checksumFailures = 0
	dmc.queryTimes = make(map[string][]time.Duration)
	dmc.memoryUsage = 0
	dmc.startTime = time.Now()
}

// NoOpTracingManager provides a no-op tracing implementation
type NoOpTracingManager struct{}

func NewNoOpTracingManager() *NoOpTracingManager {
	return &NoOpTracingManager{}
}

func (nt *NoOpTracingManager) StartSpan(ctx context.Context, operationName string) (context.Context, func()) {
	return ctx, func() {}
}

func (nt *NoOpTracingManager) AddSpanAttribute(ctx context.Context, key string, value interface{}) {}

func (nt *NoOpTracingManager) RecordError(ctx context.Context, err error) {}

// SlogLogger provides a structured logger using Go's slog package
type SlogLogger struct {
	logger *slog.Logger
}

func NewSlogLogger() *SlogLogger {
	return &SlogLogger{
		logger: slog.Default(),
	}
}

func (sl *SlogLogger) Info(msg string, args ...interface{}) {
	sl.logger.Info(msg, args...)
}

func (sl *SlogLogger) Error(msg string, err error, args ...interface{}) {
	allArgs := append([]interface{}{"error", err}, args...)
	sl.logger.Error(msg, allArgs...)
}

func (sl *SlogLogger) Debug(msg string, args ...interface{}) {
	sl.logger.Debug(msg, args...)
}

func (sl *SlogLogger) Warn(msg string, args ...interface{}) {
	sl.logger.Warn(msg, args...)
}

func (sl *SlogLogger) With(args ...interface{}) Logger {
	return &SlogLogger{
		logger: sl.logger.With(args...),
	}
}

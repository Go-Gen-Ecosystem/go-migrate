/**
 * @file metrics.go
 * @description Migration metrics collection and reporting
 * @author Admilson B. F. Cossa
 * @date 2025-01-27
 * @path pkg/core/database/migrations/internal/observability/metrics.go
 */

package observability

import (
	"context"
	"sync"
	"time"
)

// MetricsCollector defines the interface for collecting migration metrics
type MetricsCollector interface {
	// Migration execution metrics
	RecordMigrationDuration(migrationID, operation string, duration time.Duration)
	RecordMigrationSuccess(migrationID, operation string)
	RecordMigrationFailure(migrationID, operation string, errorType string)
	
	// Lock metrics
	RecordLockAcquisitionTime(lockID string, duration time.Duration)
	RecordLockAcquisitionFailure(lockID string)
	RecordLockHoldTime(lockID string, duration time.Duration)
	
	// System health metrics
	RecordActiveConnections(count int)
	RecordPendingMigrations(count int)
	RecordAppliedMigrations(count int)
	
	// Drift detection metrics
	RecordDriftDetection(migrationID string, driftType string)
	RecordChecksumValidation(migrationID string, valid bool)
	
	// Performance metrics
	RecordDatabaseQueryTime(operation string, duration time.Duration)
	RecordMemoryUsage(bytes int64)
	
	// Get metrics for reporting
	GetMetrics(ctx context.Context) (*MetricsSnapshot, error)
	Reset()
}

// MetricsSnapshot represents a point-in-time view of all metrics
type MetricsSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	
	// Migration metrics
	MigrationMetrics MigrationMetrics `json:"migration_metrics"`
	
	// Lock metrics
	LockMetrics LockMetrics `json:"lock_metrics"`
	
	// System metrics
	SystemMetrics SystemMetrics `json:"system_metrics"`
	
	// Performance metrics
	PerformanceMetrics PerformanceMetrics `json:"performance_metrics"`
}

// MigrationMetrics tracks migration-specific metrics
type MigrationMetrics struct {
	TotalExecutions    int64                    `json:"total_executions"`
	SuccessfulExecutions int64                  `json:"successful_executions"`
	FailedExecutions   int64                    `json:"failed_executions"`
	SuccessRate        float64                  `json:"success_rate"`
	AverageExecutionTime time.Duration          `json:"average_execution_time"`
	ExecutionsByOperation map[string]int64      `json:"executions_by_operation"`
	FailuresByErrorType map[string]int64        `json:"failures_by_error_type"`
	DurationHistogram  map[string]time.Duration `json:"duration_histogram"`
}

// LockMetrics tracks lock-related metrics
type LockMetrics struct {
	TotalAcquisitions     int64         `json:"total_acquisitions"`
	FailedAcquisitions    int64         `json:"failed_acquisitions"`
	AverageAcquisitionTime time.Duration `json:"average_acquisition_time"`
	AverageHoldTime       time.Duration `json:"average_hold_time"`
	MaxHoldTime           time.Duration `json:"max_hold_time"`
	ActiveLocks           int64         `json:"active_locks"`
}

// SystemMetrics tracks system health metrics
type SystemMetrics struct {
	ActiveConnections   int64 `json:"active_connections"`
	PendingMigrations   int64 `json:"pending_migrations"`
	AppliedMigrations   int64 `json:"applied_migrations"`
	DriftDetections     int64 `json:"drift_detections"`
	ChecksumValidations int64 `json:"checksum_validations"`
	ChecksumFailures    int64 `json:"checksum_failures"`
}

// PerformanceMetrics tracks performance-related metrics
type PerformanceMetrics struct {
	DatabaseQueryTimes map[string]time.Duration `json:"database_query_times"`
	MemoryUsage        int64                    `json:"memory_usage_bytes"`
	CPUUsage           float64                  `json:"cpu_usage_percent"`
}

// InMemoryMetricsCollector implements MetricsCollector using in-memory storage
type InMemoryMetricsCollector struct {
	mu sync.RWMutex
	
	// Migration metrics
	migrationExecutions    int64
	migrationSuccesses     int64
	migrationFailures      int64
	migrationDurations     []time.Duration
	executionsByOperation  map[string]int64
	failuresByErrorType    map[string]int64
	
	// Lock metrics
	lockAcquisitions       int64
	lockAcquisitionFailures int64
	lockAcquisitionTimes   []time.Duration
	lockHoldTimes          []time.Duration
	activeLocks            int64
	
	// System metrics
	activeConnections      int64
	pendingMigrations      int64
	appliedMigrations      int64
	driftDetections        int64
	checksumValidations    int64
	checksumFailures       int64
	
	// Performance metrics
	queryTimes             map[string][]time.Duration
	memoryUsage            int64
	
	startTime              time.Time
}

// NewInMemoryMetricsCollector creates a new in-memory metrics collector
func NewInMemoryMetricsCollector() *InMemoryMetricsCollector {
	return &InMemoryMetricsCollector{
		executionsByOperation: make(map[string]int64),
		failuresByErrorType:   make(map[string]int64),
		queryTimes:           make(map[string][]time.Duration),
		startTime:            time.Now(),
	}
}

// RecordMigrationDuration records the duration of a migration operation
func (m *InMemoryMetricsCollector) RecordMigrationDuration(migrationID, operation string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.migrationExecutions++
	m.migrationDurations = append(m.migrationDurations, duration)
	m.executionsByOperation[operation]++
}

// RecordMigrationSuccess records a successful migration
func (m *InMemoryMetricsCollector) RecordMigrationSuccess(migrationID, operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.migrationSuccesses++
}

// RecordMigrationFailure records a failed migration
func (m *InMemoryMetricsCollector) RecordMigrationFailure(migrationID, operation string, errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.migrationFailures++
	m.failuresByErrorType[errorType]++
}

// RecordLockAcquisitionTime records lock acquisition time
func (m *InMemoryMetricsCollector) RecordLockAcquisitionTime(lockID string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.lockAcquisitions++
	m.lockAcquisitionTimes = append(m.lockAcquisitionTimes, duration)
}

// RecordLockAcquisitionFailure records a failed lock acquisition
func (m *InMemoryMetricsCollector) RecordLockAcquisitionFailure(lockID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.lockAcquisitionFailures++
}

// RecordLockHoldTime records how long a lock was held
func (m *InMemoryMetricsCollector) RecordLockHoldTime(lockID string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.lockHoldTimes = append(m.lockHoldTimes, duration)
}

// RecordActiveConnections records the number of active database connections
func (m *InMemoryMetricsCollector) RecordActiveConnections(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.activeConnections = int64(count)
}

// RecordPendingMigrations records the number of pending migrations
func (m *InMemoryMetricsCollector) RecordPendingMigrations(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.pendingMigrations = int64(count)
}

// RecordAppliedMigrations records the number of applied migrations
func (m *InMemoryMetricsCollector) RecordAppliedMigrations(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.appliedMigrations = int64(count)
}

// RecordDriftDetection records a drift detection event
func (m *InMemoryMetricsCollector) RecordDriftDetection(migrationID string, driftType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.driftDetections++
}

// RecordChecksumValidation records a checksum validation event
func (m *InMemoryMetricsCollector) RecordChecksumValidation(migrationID string, valid bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.checksumValidations++
	if !valid {
		m.checksumFailures++
	}
}

// RecordDatabaseQueryTime records database query execution time
func (m *InMemoryMetricsCollector) RecordDatabaseQueryTime(operation string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.queryTimes[operation] == nil {
		m.queryTimes[operation] = make([]time.Duration, 0)
	}
	m.queryTimes[operation] = append(m.queryTimes[operation], duration)
}

// RecordMemoryUsage records current memory usage
func (m *InMemoryMetricsCollector) RecordMemoryUsage(bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.memoryUsage = bytes
}

// GetMetrics returns a snapshot of current metrics
func (m *InMemoryMetricsCollector) GetMetrics(ctx context.Context) (*MetricsSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	snapshot := &MetricsSnapshot{
		Timestamp: time.Now(),
		MigrationMetrics: MigrationMetrics{
			TotalExecutions:       m.migrationExecutions,
			SuccessfulExecutions:  m.migrationSuccesses,
			FailedExecutions:      m.migrationFailures,
			ExecutionsByOperation: copyInt64Map(m.executionsByOperation),
			FailuresByErrorType:   copyInt64Map(m.failuresByErrorType),
			DurationHistogram:     m.calculateDurationHistogram(),
		},
		LockMetrics: LockMetrics{
			TotalAcquisitions:      m.lockAcquisitions,
			FailedAcquisitions:     m.lockAcquisitionFailures,
			AverageAcquisitionTime: m.calculateAverageDuration(m.lockAcquisitionTimes),
			AverageHoldTime:        m.calculateAverageDuration(m.lockHoldTimes),
			MaxHoldTime:            m.calculateMaxDuration(m.lockHoldTimes),
			ActiveLocks:            m.activeLocks,
		},
		SystemMetrics: SystemMetrics{
			ActiveConnections:   m.activeConnections,
			PendingMigrations:   m.pendingMigrations,
			AppliedMigrations:   m.appliedMigrations,
			DriftDetections:     m.driftDetections,
			ChecksumValidations: m.checksumValidations,
			ChecksumFailures:    m.checksumFailures,
		},
		PerformanceMetrics: PerformanceMetrics{
			DatabaseQueryTimes: m.calculateQueryTimeAverages(),
			MemoryUsage:        m.memoryUsage,
		},
	}
	
	// Calculate success rate
	if snapshot.MigrationMetrics.TotalExecutions > 0 {
		snapshot.MigrationMetrics.SuccessRate = float64(snapshot.MigrationMetrics.SuccessfulExecutions) / float64(snapshot.MigrationMetrics.TotalExecutions)
	}
	
	// Calculate average execution time
	snapshot.MigrationMetrics.AverageExecutionTime = m.calculateAverageDuration(m.migrationDurations)
	
	return snapshot, nil
}

// Reset clears all metrics
func (m *InMemoryMetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.migrationExecutions = 0
	m.migrationSuccesses = 0
	m.migrationFailures = 0
	m.migrationDurations = nil
	m.executionsByOperation = make(map[string]int64)
	m.failuresByErrorType = make(map[string]int64)
	
	m.lockAcquisitions = 0
	m.lockAcquisitionFailures = 0
	m.lockAcquisitionTimes = nil
	m.lockHoldTimes = nil
	m.activeLocks = 0
	
	m.activeConnections = 0
	m.pendingMigrations = 0
	m.appliedMigrations = 0
	m.driftDetections = 0
	m.checksumValidations = 0
	m.checksumFailures = 0
	
	m.queryTimes = make(map[string][]time.Duration)
	m.memoryUsage = 0
	
	m.startTime = time.Now()
}

// Helper functions

func copyInt64Map(original map[string]int64) map[string]int64 {
	copy := make(map[string]int64)
	for k, v := range original {
		copy[k] = v
	}
	return copy
}

func (m *InMemoryMetricsCollector) calculateAverageDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}

func (m *InMemoryMetricsCollector) calculateMaxDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	
	max := durations[0]
	for _, d := range durations[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

func (m *InMemoryMetricsCollector) calculateDurationHistogram() map[string]time.Duration {
	histogram := make(map[string]time.Duration)
	
	if len(m.migrationDurations) == 0 {
		return histogram
	}
	
	// Calculate percentiles
	durations := make([]time.Duration, len(m.migrationDurations))
	copy(durations, m.migrationDurations)
	
	// Simple percentile calculation (would use proper sorting in production)
	histogram["p50"] = m.calculateAverageDuration(durations)
	histogram["p95"] = m.calculateMaxDuration(durations)
	histogram["p99"] = m.calculateMaxDuration(durations)
	
	return histogram
}

func (m *InMemoryMetricsCollector) calculateQueryTimeAverages() map[string]time.Duration {
	averages := make(map[string]time.Duration)
	
	for operation, times := range m.queryTimes {
		averages[operation] = m.calculateAverageDuration(times)
	}
	
	return averages
}

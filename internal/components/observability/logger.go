/**
 * @file logger.go
 * @description Migration-specific structured logging with observability integration
 * @author Admilson B. F. Cossa
 * @date 2025-01-27
 * @path pkg/core/database/migrations/internal/observability/logger.go
 */

package observability

import (
	"context"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// MigrationLogger provides structured logging for migration operations
type MigrationLogger struct {
	logger    logging.Logger
	component string
}

// LogEntry represents a migration log entry with structured fields
type LogEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	Level       string                 `json:"level"`
	Message     string                 `json:"message"`
	Component   string                 `json:"component"`
	Operation   string                 `json:"operation"`
	MigrationID string                 `json:"migration_id,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Duration    time.Duration          `json:"duration,omitempty"`
	Fields      map[string]interface{} `json:"fields,omitempty"`
	Error       error                  `json:"error,omitempty"`
}

// NewMigrationLogger creates a new migration logger
func NewMigrationLogger(logger logging.Logger) *MigrationLogger {
	return &MigrationLogger{
		logger:    logger,
		component: "migrations",
	}
}

// LogMigrationStart logs the start of a migration operation
func (l *MigrationLogger) LogMigrationStart(ctx context.Context, migrationID, version, operation string) {
	l.logger.Info(ctx, "Migration operation started",
		"component", l.component,
		"operation", operation,
		"migration_id", migrationID,
		"version", version,
		"timestamp", time.Now(),
	)
}

// LogMigrationSuccess logs successful completion of a migration
func (l *MigrationLogger) LogMigrationSuccess(ctx context.Context, migrationID, version, operation string, duration time.Duration) {
	l.logger.Info(ctx, "Migration operation completed successfully",
		"component", l.component,
		"operation", operation,
		"migration_id", migrationID,
		"version", version,
		"duration_ms", duration.Milliseconds(),
		"status", "success",
		"timestamp", time.Now(),
	)
}

// LogMigrationFailure logs failed migration operation
func (l *MigrationLogger) LogMigrationFailure(ctx context.Context, migrationID, version, operation string, duration time.Duration, err error) {
	l.logger.Error(ctx, "Migration operation failed",
		err,
	)
	l.logger.Info(ctx, "Migration failure details",
		"component", l.component,
		"operation", operation,
		"migration_id", migrationID,
		"version", version,
		"duration_ms", duration.Milliseconds(),
		"status", "failed",
		"timestamp", time.Now(),
	)
}

// LogLockAcquisition logs lock acquisition attempts
func (l *MigrationLogger) LogLockAcquisition(ctx context.Context, lockID string, acquired bool, duration time.Duration) {
	status := "acquired"
	level := "info"
	if !acquired {
		status = "failed"
		level = "warn"
	}

	if level == "info" {
		l.logger.Info(ctx, "Migration lock acquisition",
			"component", l.component,
			"operation", "lock_acquisition",
			"lock_id", lockID,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"timestamp", time.Now(),
		)
	} else {
		l.logger.Warn(ctx, "Migration lock acquisition failed", nil)
		l.logger.Info(ctx, "Lock acquisition failure details",
			"component", l.component,
			"operation", "lock_acquisition",
			"lock_id", lockID,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"timestamp", time.Now(),
		)
	}
}

// LogLockRelease logs lock release operations
func (l *MigrationLogger) LogLockRelease(ctx context.Context, lockID string, released bool, duration time.Duration) {
	status := "released"
	if !released {
		status = "failed"
	}

	l.logger.Info(ctx, "Migration lock release",
		"component", l.component,
		"operation", "lock_release",
		"lock_id", lockID,
		"status", status,
		"duration_ms", duration.Milliseconds(),
		"timestamp", time.Now(),
	)
}

// LogDriftDetection logs drift detection results
func (l *MigrationLogger) LogDriftDetection(ctx context.Context, migrationID string, driftType string, detected bool) {
	status := "no_drift"
	level := "info"
	if detected {
		status = "drift_detected"
		level = "warn"
	}

	if level == "info" {
		l.logger.Info(ctx, "Migration drift detection",
			"component", l.component,
			"operation", "drift_detection",
			"migration_id", migrationID,
			"drift_type", driftType,
			"status", status,
			"timestamp", time.Now(),
		)
	} else {
		l.logger.Warn(ctx, "Migration drift detected", nil)
		l.logger.Info(ctx, "Drift detection details",
			"component", l.component,
			"operation", "drift_detection",
			"migration_id", migrationID,
			"drift_type", driftType,
			"status", status,
			"timestamp", time.Now(),
		)
	}
}

// LogChecksumValidation logs checksum validation results
func (l *MigrationLogger) LogChecksumValidation(ctx context.Context, migrationID string, valid bool, expectedChecksum, actualChecksum string) {
	status := "valid"
	level := "info"
	if !valid {
		status = "invalid"
		level = "warn"
	}

	if level == "info" {
		l.logger.Info(ctx, "Migration checksum validation",
			"component", l.component,
			"operation", "checksum_validation",
			"migration_id", migrationID,
			"status", status,
			"expected_checksum", expectedChecksum,
			"actual_checksum", actualChecksum,
			"timestamp", time.Now(),
		)
	} else {
		l.logger.Warn(ctx, "Migration checksum validation failed", nil)
		l.logger.Info(ctx, "Checksum validation details",
			"component", l.component,
			"operation", "checksum_validation",
			"migration_id", migrationID,
			"status", status,
			"expected_checksum", expectedChecksum,
			"actual_checksum", actualChecksum,
			"timestamp", time.Now(),
		)
	}
}

// LogIdempotencyValidation logs idempotency validation results
func (l *MigrationLogger) LogIdempotencyValidation(ctx context.Context, migrationID string, canRerun bool, reason string) {
	status := "can_rerun"
	if !canRerun {
		status = "cannot_rerun"
	}

	l.logger.Info(ctx, "Migration idempotency validation",
		"component", l.component,
		"operation", "idempotency_validation",
		"migration_id", migrationID,
		"status", status,
		"reason", reason,
		"timestamp", time.Now(),
	)
}

// LogSystemEvent logs general system events
func (l *MigrationLogger) LogSystemEvent(ctx context.Context, event string, fields map[string]interface{}) {
	fieldPairs := make([]interface{}, 0, len(fields)*2+6)
	fieldPairs = append(fieldPairs,
		"component", l.component,
		"operation", "system_event",
		"event", event,
	)

	for k, v := range fields {
		fieldPairs = append(fieldPairs, k, v)
	}

	l.logger.Info(ctx, "Migration system event", fieldPairs...)
}

// LogPerformanceMetrics logs performance-related metrics
func (l *MigrationLogger) LogPerformanceMetrics(ctx context.Context, operation string, metrics map[string]interface{}) {
	fieldPairs := make([]interface{}, 0, len(metrics)*2+4)
	fieldPairs = append(fieldPairs,
		"component", l.component,
		"operation", operation,
	)

	for k, v := range metrics {
		fieldPairs = append(fieldPairs, k, v)
	}

	l.logger.Info(ctx, "Migration performance metrics", fieldPairs...)
}

// Debug logs debug information
func (l *MigrationLogger) Debug(ctx context.Context, msg string, fields ...interface{}) {
	allFields := make([]interface{}, 0, len(fields)+2)
	allFields = append(allFields, "component", l.component)
	allFields = append(allFields, fields...)
	l.logger.Debug(ctx, msg, allFields...)
}

// Info logs informational messages
func (l *MigrationLogger) Info(ctx context.Context, msg string, fields ...interface{}) {
	allFields := make([]interface{}, 0, len(fields)+2)
	allFields = append(allFields, "component", l.component)
	allFields = append(allFields, fields...)
	l.logger.Info(ctx, msg, allFields...)
}

// Warn logs warning messages
func (l *MigrationLogger) Warn(ctx context.Context, msg string, err error) {
	l.logger.Warn(ctx, msg, err)
}

// Error logs error messages
func (l *MigrationLogger) Error(ctx context.Context, msg string, err error) {
	l.logger.Error(ctx, msg, err)
}

// Fatal logs fatal messages and exits
func (l *MigrationLogger) Fatal(ctx context.Context, msg string, err error) {
	l.logger.Fatal(ctx, msg, err)
}

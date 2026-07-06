/**
 * @file tracing.go
 * @description OpenTelemetry tracing for migration operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-27
 * @path pkg/core/database/migrations/internal/observability/tracing.go
 */

package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// Tracer name for migration operations
	TracerName = "github.com/gogen-framework/gogen/pkg/core/database/migrations"
	
	// Span names
	SpanMigrationExecution = "migration.execute"
	SpanMigrationValidation = "migration.validate"
	SpanLockAcquisition = "lock.acquire"
	SpanLockRelease = "lock.release"
	SpanDriftDetection = "drift.detect"
	SpanChecksumValidation = "checksum.validate"
	SpanDatabaseQuery = "database.query"
	SpanFileOperation = "file.operation"
	
	// Attribute keys
	AttrMigrationID = "migration.id"
	AttrMigrationVersion = "migration.version"
	AttrMigrationName = "migration.name"
	AttrMigrationOperation = "migration.operation"
	AttrMigrationChecksum = "migration.checksum"
	AttrMigrationState = "migration.state"
	AttrLockID = "lock.id"
	AttrLockType = "lock.type"
	AttrDriftType = "drift.type"
	AttrDriftCount = "drift.count"
	AttrDatabaseDriver = "database.driver"
	AttrDatabaseName = "database.name"
	AttrQueryType = "query.type"
	AttrQueryDuration = "query.duration_ms"
	AttrFileOperation = "file.operation"
	AttrFilePath = "file.path"
	AttrErrorType = "error.type"
	AttrErrorMessage = "error.message"
)

// MigrationTracer provides tracing capabilities for migration operations
type MigrationTracer struct {
	tracer trace.Tracer
}

// NewMigrationTracer creates a new migration tracer
func NewMigrationTracer() *MigrationTracer {
	return &MigrationTracer{
		tracer: otel.Tracer(TracerName),
	}
}

// StartMigrationSpan starts a new span for migration execution
func (t *MigrationTracer) StartMigrationSpan(ctx context.Context, migrationID, operation string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, SpanMigrationExecution,
		trace.WithAttributes(
			attribute.String(AttrMigrationID, migrationID),
			attribute.String(AttrMigrationOperation, operation),
		),
	)
}

// StartValidationSpan starts a new span for migration validation
func (t *MigrationTracer) StartValidationSpan(ctx context.Context, migrationID string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, SpanMigrationValidation,
		trace.WithAttributes(
			attribute.String(AttrMigrationID, migrationID),
		),
	)
}

// StartLockSpan starts a new span for lock operations
func (t *MigrationTracer) StartLockSpan(ctx context.Context, lockID, operation string) (context.Context, trace.Span) {
	spanName := SpanLockAcquisition
	if operation == "release" {
		spanName = SpanLockRelease
	}
	
	return t.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String(AttrLockID, lockID),
			attribute.String(AttrLockType, operation),
		),
	)
}

// StartDriftDetectionSpan starts a new span for drift detection
func (t *MigrationTracer) StartDriftDetectionSpan(ctx context.Context) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, SpanDriftDetection)
}

// StartChecksumValidationSpan starts a new span for checksum validation
func (t *MigrationTracer) StartChecksumValidationSpan(ctx context.Context, migrationID string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, SpanChecksumValidation,
		trace.WithAttributes(
			attribute.String(AttrMigrationID, migrationID),
		),
	)
}

// StartDatabaseQuerySpan starts a new span for database queries
func (t *MigrationTracer) StartDatabaseQuerySpan(ctx context.Context, queryType, driver, dbName string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, SpanDatabaseQuery,
		trace.WithAttributes(
			attribute.String(AttrQueryType, queryType),
			attribute.String(AttrDatabaseDriver, driver),
			attribute.String(AttrDatabaseName, dbName),
		),
	)
}

// StartFileOperationSpan starts a new span for file operations
func (t *MigrationTracer) StartFileOperationSpan(ctx context.Context, operation, filePath string) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, SpanFileOperation,
		trace.WithAttributes(
			attribute.String(AttrFileOperation, operation),
			attribute.String(AttrFilePath, filePath),
		),
	)
}

// RecordMigrationSuccess records a successful migration execution
func (t *MigrationTracer) RecordMigrationSuccess(span trace.Span, migrationID, version, name, checksum, state string) {
	span.SetAttributes(
		attribute.String(AttrMigrationVersion, version),
		attribute.String(AttrMigrationName, name),
		attribute.String(AttrMigrationChecksum, checksum),
		attribute.String(AttrMigrationState, state),
	)
	span.SetStatus(codes.Ok, "Migration executed successfully")
}

// RecordMigrationError records a migration execution error
func (t *MigrationTracer) RecordMigrationError(span trace.Span, err error, errorType string) {
	span.SetAttributes(
		attribute.String(AttrErrorType, errorType),
		attribute.String(AttrErrorMessage, err.Error()),
	)
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

// RecordLockSuccess records successful lock acquisition/release
func (t *MigrationTracer) RecordLockSuccess(span trace.Span, duration time.Duration) {
	span.SetAttributes(
		attribute.Int64("lock.duration_ms", duration.Milliseconds()),
	)
	span.SetStatus(codes.Ok, "Lock operation completed successfully")
}

// RecordLockError records lock operation error
func (t *MigrationTracer) RecordLockError(span trace.Span, err error, timeout time.Duration) {
	span.SetAttributes(
		attribute.String(AttrErrorType, "lock_error"),
		attribute.String(AttrErrorMessage, err.Error()),
		attribute.Int64("lock.timeout_ms", timeout.Milliseconds()),
	)
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

// RecordDriftDetection records drift detection results
func (t *MigrationTracer) RecordDriftDetection(span trace.Span, driftType string, driftCount int, migrations []string) {
	span.SetAttributes(
		attribute.String(AttrDriftType, driftType),
		attribute.Int(AttrDriftCount, driftCount),
		attribute.StringSlice("drift.migrations", migrations),
	)
	
	if driftCount > 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("Detected %d drift(s) of type %s", driftCount, driftType))
	} else {
		span.SetStatus(codes.Ok, "No drift detected")
	}
}

// RecordChecksumValidation records checksum validation results
func (t *MigrationTracer) RecordChecksumValidation(span trace.Span, migrationID, expectedChecksum, actualChecksum string, valid bool) {
	span.SetAttributes(
		attribute.String("checksum.expected", expectedChecksum),
		attribute.String("checksum.actual", actualChecksum),
		attribute.Bool("checksum.valid", valid),
	)
	
	if valid {
		span.SetStatus(codes.Ok, "Checksum validation passed")
	} else {
		span.SetStatus(codes.Error, "Checksum validation failed")
	}
}

// RecordDatabaseQuery records database query execution
func (t *MigrationTracer) RecordDatabaseQuery(span trace.Span, query string, duration time.Duration, rowsAffected int64) {
	span.SetAttributes(
		attribute.String("query.statement", query),
		attribute.Int64(AttrQueryDuration, duration.Milliseconds()),
		attribute.Int64("query.rows_affected", rowsAffected),
	)
	span.SetStatus(codes.Ok, "Database query executed successfully")
}

// RecordDatabaseQueryError records database query error
func (t *MigrationTracer) RecordDatabaseQueryError(span trace.Span, query string, err error) {
	span.SetAttributes(
		attribute.String("query.statement", query),
		attribute.String(AttrErrorType, "database_error"),
		attribute.String(AttrErrorMessage, err.Error()),
	)
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

// RecordFileOperation records file operation results
func (t *MigrationTracer) RecordFileOperation(span trace.Span, operation, filePath string, size int64, duration time.Duration) {
	span.SetAttributes(
		attribute.String(AttrFileOperation, operation),
		attribute.String(AttrFilePath, filePath),
		attribute.Int64("file.size_bytes", size),
		attribute.Int64("file.duration_ms", duration.Milliseconds()),
	)
	span.SetStatus(codes.Ok, "File operation completed successfully")
}

// RecordFileOperationError records file operation error
func (t *MigrationTracer) RecordFileOperationError(span trace.Span, operation, filePath string, err error) {
	span.SetAttributes(
		attribute.String(AttrFileOperation, operation),
		attribute.String(AttrFilePath, filePath),
		attribute.String(AttrErrorType, "file_error"),
		attribute.String(AttrErrorMessage, err.Error()),
	)
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

// AddMigrationAttributes adds common migration attributes to a span
func (t *MigrationTracer) AddMigrationAttributes(span trace.Span, migrationID, version, name, checksum string) {
	span.SetAttributes(
		attribute.String(AttrMigrationID, migrationID),
		attribute.String(AttrMigrationVersion, version),
		attribute.String(AttrMigrationName, name),
		attribute.String(AttrMigrationChecksum, checksum),
	)
}

// AddDatabaseAttributes adds database-specific attributes to a span
func (t *MigrationTracer) AddDatabaseAttributes(span trace.Span, driver, dbName, host string, port int) {
	span.SetAttributes(
		attribute.String(AttrDatabaseDriver, driver),
		attribute.String(AttrDatabaseName, dbName),
		attribute.String("database.host", host),
		attribute.Int("database.port", port),
	)
}

// TracedMigrationOperation represents a traced migration operation
type TracedMigrationOperation struct {
	ctx    context.Context
	span   trace.Span
	tracer *MigrationTracer
}

// NewTracedMigrationOperation creates a new traced migration operation
func (t *MigrationTracer) NewTracedMigrationOperation(ctx context.Context, migrationID, operation string) *TracedMigrationOperation {
	newCtx, span := t.StartMigrationSpan(ctx, migrationID, operation)
	return &TracedMigrationOperation{
		ctx:    newCtx,
		span:   span,
		tracer: t,
	}
}

// Context returns the traced context
func (op *TracedMigrationOperation) Context() context.Context {
	return op.ctx
}

// Span returns the trace span
func (op *TracedMigrationOperation) Span() trace.Span {
	return op.span
}

// Success marks the operation as successful
func (op *TracedMigrationOperation) Success(migrationID, version, name, checksum, state string) {
	op.tracer.RecordMigrationSuccess(op.span, migrationID, version, name, checksum, state)
}

// Error marks the operation as failed
func (op *TracedMigrationOperation) Error(err error, errorType string) {
	op.tracer.RecordMigrationError(op.span, err, errorType)
}

// End finishes the traced operation
func (op *TracedMigrationOperation) End() {
	op.span.End()
}

// WithAttributes adds attributes to the span
func (op *TracedMigrationOperation) WithAttributes(attrs ...attribute.KeyValue) {
	op.span.SetAttributes(attrs...)
}

// AddEvent adds an event to the span
func (op *TracedMigrationOperation) AddEvent(name string, attrs ...attribute.KeyValue) {
	op.span.AddEvent(name, trace.WithAttributes(attrs...))
}

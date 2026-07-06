/**
 * @file auditor.go
 * @description Checksum auditor implementation for tracking and logging checksum operations
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/auditor.go
 */

package checksum

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultChecksumAuditor implements ChecksumAuditor interface
type DefaultChecksumAuditor struct {
	store   AuditStore
	config  *AuditConfig
	buffer  []*AuditEntry
	mutex   sync.RWMutex
	metrics *AuditMetrics
}

// AuditStore interface for persisting audit entries
type AuditStore interface {
	Store(ctx context.Context, entry *AuditEntry) error
	Retrieve(ctx context.Context, id string) (*AuditEntry, error)
	List(ctx context.Context, filter *AuditFilter) ([]*AuditEntry, error)
	Delete(ctx context.Context, id string) error
	Cleanup(ctx context.Context, before time.Time) error
	GetStatistics(ctx context.Context) (*AuditStatistics, error)
	Initialize(ctx context.Context) error
}

// AuditConfig configuration for auditing
type AuditConfig struct {
	EnableBuffering   bool
	BufferSize        int
	FlushInterval     time.Duration
	RetentionPeriod   time.Duration
	EnableCompression bool
	EnableEncryption  bool
	LogLevel          AuditLogLevel
	IncludeStackTrace bool
	MaxEntrySize      int
}

// AuditLogLevel defines the logging level for audit entries
type AuditLogLevel int

const (
	AuditLogLevelDebug AuditLogLevel = iota
	AuditLogLevelInfo
	AuditLogLevelWarn
	AuditLogLevelError
)

// AuditMetrics tracks audit operation metrics
type AuditMetrics struct {
	TotalEntries      int64
	EntriesPerLevel   map[AuditLogLevel]int64
	AverageEntrySize  float64
	BufferFlushCount  int64
	ErrorCount        int64
	LastFlushTime     time.Time
	mutex             sync.RWMutex
}

// NewDefaultChecksumAuditor creates a new default checksum auditor
func NewDefaultChecksumAuditor(store AuditStore, config *AuditConfig) *DefaultChecksumAuditor {
	auditor := &DefaultChecksumAuditor{
		store:   store,
		config:  config,
		buffer:  make([]*AuditEntry, 0, config.BufferSize),
		metrics: &AuditMetrics{
			EntriesPerLevel: make(map[AuditLogLevel]int64),
		},
	}

	// Start buffer flushing if enabled
	if config.EnableBuffering {
		go auditor.startBufferFlushing()
	}

	return auditor
}

// LogOperation logs a checksum operation
func (a *DefaultChecksumAuditor) LogOperation(ctx context.Context, operation, recordID string, details map[string]interface{}) error {
	entry := &AuditEntry{
		ID:        uuid.New().String(),
		Operation: operation,
		RecordID:  recordID,
		Details:   details,
		Timestamp: time.Now(),
		Level:     AuditLogLevelInfo,
		UserID:    a.getUserID(ctx),
		SessionID: a.getSessionID(ctx),
	}

	// Add context information
	if entry.Details == nil {
		entry.Details = make(map[string]interface{})
	}
	entry.Details["context"] = a.extractContextInfo(ctx)

	// Add stack trace if enabled
	if a.config.IncludeStackTrace {
		entry.Details["stack_trace"] = a.getStackTrace()
	}

	return a.logEntry(ctx, entry)
}

// LogValidation logs a validation operation
func (a *DefaultChecksumAuditor) LogValidation(ctx context.Context, recordID string, result *ChecksumValidationResult) error {
	level := AuditLogLevelInfo
	if result.Status == StatusInvalid {
		level = AuditLogLevelWarn
	}

	entry := &AuditEntry{
		ID:        uuid.New().String(),
		Operation: "validation",
		RecordID:  recordID,
		Details: map[string]interface{}{
			"status":         result.Status,
			"message":        result.Message,
			"expected_value": result.ExpectedValue,
			"actual_value":   result.ActualValue,
			"suggestions":    result.Suggestions,
		},
		Timestamp: time.Now(),
		Level:     level,
		UserID:    a.getUserID(ctx),
		SessionID: a.getSessionID(ctx),
	}

	return a.logEntry(ctx, entry)
}

// LogError logs an error
func (a *DefaultChecksumAuditor) LogError(ctx context.Context, operation, recordID string, err error, details map[string]interface{}) error {
	entry := &AuditEntry{
		ID:        uuid.New().String(),
		Operation: operation,
		RecordID:  recordID,
		Details:   details,
		Timestamp: time.Now(),
		Level:     AuditLogLevelError,
		UserID:    a.getUserID(ctx),
		SessionID: a.getSessionID(ctx),
		Error:     err.Error(),
	}

	if entry.Details == nil {
		entry.Details = make(map[string]interface{})
	}
	entry.Details["error"] = err.Error()
	entry.Details["error_type"] = fmt.Sprintf("%T", err)

	return a.logEntry(ctx, entry)
}

// GetAuditTrail retrieves audit trail for a record
func (a *DefaultChecksumAuditor) GetAuditTrail(ctx context.Context, recordID string) ([]*AuditEntry, error) {
	filter := &AuditFilter{
		RecordID: recordID,
	}

	return a.store.List(ctx, filter)
}

// GetAuditLog retrieves audit entries matching the supplied filter.
func (a *DefaultChecksumAuditor) GetAuditLog(ctx context.Context, filter *AuditFilter) ([]*AuditEntry, error) {
	if filter == nil {
		filter = &AuditFilter{}
	}

	return a.store.List(ctx, filter)
}

// GetAuditStatistics retrieves audit statistics
func (a *DefaultChecksumAuditor) GetAuditStatistics(ctx context.Context) (*AuditStatistics, error) {
	// Get store statistics
	storeStats, err := a.store.GetStatistics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get store statistics: %w", err)
	}

	// Combine with metrics
	a.metrics.mutex.RLock()
	defer a.metrics.mutex.RUnlock()

	stats := &AuditStatistics{
		TotalEntries:      storeStats.TotalEntries,
		EntriesPerLevel:   make(map[string]int64),
		AverageEntrySize:  storeStats.AverageEntrySize,
		OldestEntry:       storeStats.OldestEntry,
		NewestEntry:       storeStats.NewestEntry,
		BufferFlushCount:  a.metrics.BufferFlushCount,
		ErrorCount:        a.metrics.ErrorCount,
		LastFlushTime:     a.metrics.LastFlushTime,
	}

	// Convert level map
	for level, count := range a.metrics.EntriesPerLevel {
		stats.EntriesPerLevel[a.levelToString(level)] = count
	}

	return stats, nil
}

// CleanupOldEntries removes old audit entries
func (a *DefaultChecksumAuditor) CleanupOldEntries(ctx context.Context) error {
	cutoff := time.Now().Add(-a.config.RetentionPeriod)
	return a.store.Cleanup(ctx, cutoff)
}

// ArchiveOldEntries removes audit entries older than the supplied cutoff and
// returns the number of entries that were archived. The matching entries are
// first counted via the store's filtered listing and then purged through the
// store's cleanup routine.
func (a *DefaultChecksumAuditor) ArchiveOldEntries(ctx context.Context, olderThan time.Time) (int, error) {
	entries, err := a.store.List(ctx, &AuditFilter{EndTime: olderThan})
	if err != nil {
		return 0, fmt.Errorf("failed to list entries for archival: %w", err)
	}

	count := len(entries)

	if err := a.store.Cleanup(ctx, olderThan); err != nil {
		return 0, fmt.Errorf("failed to archive entries older than %s: %w", olderThan, err)
	}

	return count, nil
}

// logEntry logs an audit entry
func (a *DefaultChecksumAuditor) logEntry(ctx context.Context, entry *AuditEntry) error {
	// Check log level
	if entry.Level < a.config.LogLevel {
		return nil
	}

	// Check entry size
	if a.config.MaxEntrySize > 0 {
		entrySize := a.calculateEntrySize(entry)
		if entrySize > a.config.MaxEntrySize {
			return fmt.Errorf("audit entry size (%d bytes) exceeds maximum allowed size (%d bytes)", entrySize, a.config.MaxEntrySize)
		}
	}

	// Update metrics
	a.updateMetrics(entry)

	// Handle buffering
	if a.config.EnableBuffering {
		return a.addToBuffer(entry)
	}

	// Store directly
	return a.store.Store(ctx, entry)
}

// addToBuffer adds entry to buffer
func (a *DefaultChecksumAuditor) addToBuffer(entry *AuditEntry) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.buffer = append(a.buffer, entry)

	// Flush if buffer is full
	if len(a.buffer) >= a.config.BufferSize {
		go a.flushBuffer()
	}

	return nil
}

// flushBuffer flushes the buffer to store
func (a *DefaultChecksumAuditor) flushBuffer() error {
	a.mutex.Lock()
	entries := make([]*AuditEntry, len(a.buffer))
	copy(entries, a.buffer)
	a.buffer = a.buffer[:0] // Clear buffer
	a.mutex.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var lastErr error
	for _, entry := range entries {
		if err := a.store.Store(ctx, entry); err != nil {
			lastErr = err
			a.metrics.mutex.Lock()
			a.metrics.ErrorCount++
			a.metrics.mutex.Unlock()
		}
	}

	// Update metrics
	a.metrics.mutex.Lock()
	a.metrics.BufferFlushCount++
	a.metrics.LastFlushTime = time.Now()
	a.metrics.mutex.Unlock()

	return lastErr
}

// startBufferFlushing starts periodic buffer flushing
func (a *DefaultChecksumAuditor) startBufferFlushing() {
	ticker := time.NewTicker(a.config.FlushInterval)
	defer ticker.Stop()

	for range ticker.C {
		a.mutex.RLock()
		hasEntries := len(a.buffer) > 0
		a.mutex.RUnlock()

		if hasEntries {
			a.flushBuffer()
		}
	}
}

// Helper methods
func (a *DefaultChecksumAuditor) getUserID(ctx context.Context) string {
	if userID := ctx.Value("user_id"); userID != nil {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return "unknown"
}

func (a *DefaultChecksumAuditor) getSessionID(ctx context.Context) string {
	if sessionID := ctx.Value("session_id"); sessionID != nil {
		if id, ok := sessionID.(string); ok {
			return id
		}
	}
	return "unknown"
}

func (a *DefaultChecksumAuditor) extractContextInfo(ctx context.Context) map[string]interface{} {
	info := make(map[string]interface{})

	if deadline, ok := ctx.Deadline(); ok {
		info["deadline"] = deadline
	}

	if value := ctx.Value("request_id"); value != nil {
		info["request_id"] = value
	}

	if value := ctx.Value("trace_id"); value != nil {
		info["trace_id"] = value
	}

	return info
}

func (a *DefaultChecksumAuditor) getStackTrace() string {
	// Simplified stack trace - in a real implementation,
	// you would use runtime.Stack() or a similar mechanism
	return "stack trace not implemented"
}

func (a *DefaultChecksumAuditor) calculateEntrySize(entry *AuditEntry) int {
	data, err := json.Marshal(entry)
	if err != nil {
		return 0
	}
	return len(data)
}

func (a *DefaultChecksumAuditor) updateMetrics(entry *AuditEntry) {
	a.metrics.mutex.Lock()
	defer a.metrics.mutex.Unlock()

	a.metrics.TotalEntries++
	a.metrics.EntriesPerLevel[entry.Level]++

	// Update average entry size
	entrySize := float64(a.calculateEntrySize(entry))
	if a.metrics.TotalEntries == 1 {
		a.metrics.AverageEntrySize = entrySize
	} else {
		a.metrics.AverageEntrySize = (a.metrics.AverageEntrySize*float64(a.metrics.TotalEntries-1) + entrySize) / float64(a.metrics.TotalEntries)
	}
}

func (a *DefaultChecksumAuditor) levelToString(level AuditLogLevel) string {
	switch level {
	case AuditLogLevelDebug:
		return "debug"
	case AuditLogLevelInfo:
		return "info"
	case AuditLogLevelWarn:
		return "warn"
	case AuditLogLevelError:
		return "error"
	default:
		return "unknown"
	}
}

// DatabaseAuditStore implements AuditStore using a database
type DatabaseAuditStore struct {
	db        *sql.DB
	tableName string
}

// NewDatabaseAuditStore creates a new database audit store
func NewDatabaseAuditStore(db *sql.DB, tableName string) *DatabaseAuditStore {
	return &DatabaseAuditStore{
		db:        db,
		tableName: tableName,
	}
}

// Initialize creates the audit table
func (s *DatabaseAuditStore) Initialize(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(36) PRIMARY KEY,
			operation VARCHAR(100) NOT NULL,
			record_id VARCHAR(36),
			details TEXT,
			timestamp TIMESTAMP NOT NULL,
			level INTEGER NOT NULL,
			user_id VARCHAR(100),
			session_id VARCHAR(100),
			error TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_record_id (record_id),
			INDEX idx_timestamp (timestamp),
			INDEX idx_operation (operation),
			INDEX idx_level (level)
		)
	`, s.tableName)

	_, err := s.db.ExecContext(ctx, query)
	return err
}

// Store stores an audit entry
func (s *DatabaseAuditStore) Store(ctx context.Context, entry *AuditEntry) error {
	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal details: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (id, operation, record_id, details, timestamp, level, user_id, session_id, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.tableName)

	_, err = s.db.ExecContext(ctx, query,
		entry.ID,
		entry.Operation,
		entry.RecordID,
		string(detailsJSON),
		entry.Timestamp,
		int(entry.Level),
		entry.UserID,
		entry.SessionID,
		entry.Error,
	)

	return err
}

// Retrieve retrieves an audit entry by ID
func (s *DatabaseAuditStore) Retrieve(ctx context.Context, id string) (*AuditEntry, error) {
	query := fmt.Sprintf(`
		SELECT id, operation, record_id, details, timestamp, level, user_id, session_id, error
		FROM %s WHERE id = ?
	`, s.tableName)

	row := s.db.QueryRowContext(ctx, query, id)

	var entry AuditEntry
	var detailsJSON string
	var level int

	err := row.Scan(
		&entry.ID,
		&entry.Operation,
		&entry.RecordID,
		&detailsJSON,
		&entry.Timestamp,
		&level,
		&entry.UserID,
		&entry.SessionID,
		&entry.Error,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	entry.Level = AuditLogLevel(level)

	if err := json.Unmarshal([]byte(detailsJSON), &entry.Details); err != nil {
		return nil, fmt.Errorf("failed to unmarshal details: %w", err)
	}

	return &entry, nil
}

// List lists audit entries with filtering
func (s *DatabaseAuditStore) List(ctx context.Context, filter *AuditFilter) ([]*AuditEntry, error) {
	query := fmt.Sprintf("SELECT id, operation, record_id, details, timestamp, level, user_id, session_id, error FROM %s", s.tableName)
	args := []interface{}{}
	conditions := []string{}

	if filter.RecordID != "" {
		conditions = append(conditions, "record_id = ?")
		args = append(args, filter.RecordID)
	}

	if filter.Operation != "" {
		conditions = append(conditions, "operation = ?")
		args = append(args, filter.Operation)
	}

	if !filter.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.StartTime)
	}

	if !filter.EndTime.IsZero() {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.EndTime)
	}

	if len(conditions) > 0 {
		query += " WHERE " + fmt.Sprintf("%s", conditions[0])
		for i := 1; i < len(conditions); i++ {
			query += " AND " + conditions[i]
		}
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var detailsJSON string
		var level int

		err := rows.Scan(
			&entry.ID,
			&entry.Operation,
			&entry.RecordID,
			&detailsJSON,
			&entry.Timestamp,
			&level,
			&entry.UserID,
			&entry.SessionID,
			&entry.Error,
		)

		if err != nil {
			return nil, err
		}

		entry.Level = AuditLogLevel(level)

		if err := json.Unmarshal([]byte(detailsJSON), &entry.Details); err != nil {
			return nil, fmt.Errorf("failed to unmarshal details: %w", err)
		}

		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// Delete deletes an audit entry
func (s *DatabaseAuditStore) Delete(ctx context.Context, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.tableName)
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

// Cleanup removes old audit entries
func (s *DatabaseAuditStore) Cleanup(ctx context.Context, before time.Time) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE timestamp < ?", s.tableName)
	_, err := s.db.ExecContext(ctx, query, before)
	return err
}

// GetStatistics retrieves audit statistics
func (s *DatabaseAuditStore) GetStatistics(ctx context.Context) (*AuditStatistics, error) {
	stats := &AuditStatistics{
		EntriesPerLevel: make(map[string]int64),
	}

	// Get total entries
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", s.tableName)
	err := s.db.QueryRowContext(ctx, query).Scan(&stats.TotalEntries)
	if err != nil {
		return nil, err
	}

	// Get entries per level
	query = fmt.Sprintf("SELECT level, COUNT(*) FROM %s GROUP BY level", s.tableName)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var level int
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			return nil, err
		}
		levelStr := (&DefaultChecksumAuditor{}).levelToString(AuditLogLevel(level))
		stats.EntriesPerLevel[levelStr] = count
	}

	// Get oldest and newest entries
	query = fmt.Sprintf("SELECT MIN(timestamp), MAX(timestamp) FROM %s", s.tableName)
	err = s.db.QueryRowContext(ctx, query).Scan(&stats.OldestEntry, &stats.NewestEntry)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return stats, nil
}

// AuditorFactory creates different types of auditors
type AuditorFactory struct{}

// NewAuditorFactory creates a new auditor factory
func NewAuditorFactory() *AuditorFactory {
	return &AuditorFactory{}
}

// CreateAuditor creates a checksum auditor
func (f *AuditorFactory) CreateAuditor(store AuditStore, config *AuditConfig) ChecksumAuditor {
	return NewDefaultChecksumAuditor(store, config)
}

// CreateDefaultConfig creates a default audit configuration
func (f *AuditorFactory) CreateDefaultConfig() *AuditConfig {
	return &AuditConfig{
		EnableBuffering:   true,
		BufferSize:        100,
		FlushInterval:     5 * time.Minute,
		RetentionPeriod:   30 * 24 * time.Hour, // 30 days
		EnableCompression: false,
		EnableEncryption:  false,
		LogLevel:          AuditLogLevelInfo,
		IncludeStackTrace: false,
		MaxEntrySize:      1024 * 1024, // 1MB
	}
}

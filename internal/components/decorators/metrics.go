/**
 * @file metrics.go
 * @description Metrics decorator for database backend operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/decorators/metrics.go
 */

package decorators

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/backend"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// MetricsCollector defines the interface for metrics collection
type MetricsCollector interface {
	// Counter metrics
	IncrementCounter(name string, labels map[string]string)
	AddToCounter(name string, value float64, labels map[string]string)
	
	// Histogram metrics
	ObserveHistogram(name string, value float64, labels map[string]string)
	
	// Gauge metrics
	SetGauge(name string, value float64, labels map[string]string)
	AddToGauge(name string, value float64, labels map[string]string)
	
	// Summary metrics
	ObserveSummary(name string, value float64, labels map[string]string)
}

// SimpleMetricsCollector provides a basic in-memory metrics collector
type SimpleMetricsCollector struct {
	mu        sync.RWMutex
	counters  map[string]float64
	gauges    map[string]float64
	histograms map[string][]float64
	summaries map[string][]float64
}

// NewSimpleMetricsCollector creates a new simple metrics collector
func NewSimpleMetricsCollector() *SimpleMetricsCollector {
	return &SimpleMetricsCollector{
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
		summaries:  make(map[string][]float64),
	}
}

// IncrementCounter increments a counter metric
func (c *SimpleMetricsCollector) IncrementCounter(name string, labels map[string]string) {
	c.AddToCounter(name, 1, labels)
}

// AddToCounter adds a value to a counter metric
func (c *SimpleMetricsCollector) AddToCounter(name string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	key := c.buildKey(name, labels)
	c.counters[key] += value
}

// ObserveHistogram observes a value in a histogram metric
func (c *SimpleMetricsCollector) ObserveHistogram(name string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	key := c.buildKey(name, labels)
	c.histograms[key] = append(c.histograms[key], value)
}

// SetGauge sets a gauge metric value
func (c *SimpleMetricsCollector) SetGauge(name string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	key := c.buildKey(name, labels)
	c.gauges[key] = value
}

// AddToGauge adds a value to a gauge metric
func (c *SimpleMetricsCollector) AddToGauge(name string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	key := c.buildKey(name, labels)
	c.gauges[key] += value
}

// ObserveSummary observes a value in a summary metric
func (c *SimpleMetricsCollector) ObserveSummary(name string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	key := c.buildKey(name, labels)
	c.summaries[key] = append(c.summaries[key], value)
}

// GetCounters returns all counter metrics
func (c *SimpleMetricsCollector) GetCounters() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make(map[string]float64)
	for k, v := range c.counters {
		result[k] = v
	}
	return result
}

// GetGauges returns all gauge metrics
func (c *SimpleMetricsCollector) GetGauges() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make(map[string]float64)
	for k, v := range c.gauges {
		result[k] = v
	}
	return result
}

// GetHistograms returns all histogram metrics
func (c *SimpleMetricsCollector) GetHistograms() map[string][]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make(map[string][]float64)
	for k, v := range c.histograms {
		result[k] = make([]float64, len(v))
		copy(result[k], v)
	}
	return result
}

// GetSummaries returns all summary metrics
func (c *SimpleMetricsCollector) GetSummaries() map[string][]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make(map[string][]float64)
	for k, v := range c.summaries {
		result[k] = make([]float64, len(v))
		copy(result[k], v)
	}
	return result
}

// buildKey builds a metric key from name and labels
func (c *SimpleMetricsCollector) buildKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	
	var parts []string
	parts = append(parts, name)
	
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	
	return strings.Join(parts, ",")
}

// MetricsDecorator adds comprehensive metrics collection to database operations
type MetricsDecorator struct {
	*BaseBackendDecorator
	config    MetricsConfig
	collector MetricsCollector
}

// NewMetricsDecorator creates a new metrics decorator
func NewMetricsDecorator(wrapped backend.DatabaseBackend, config MetricsConfig) *MetricsDecorator {
	return &MetricsDecorator{
		BaseBackendDecorator: NewBaseBackendDecorator(wrapped),
		config:               config,
		collector:            NewSimpleMetricsCollector(),
	}
}

// NewMetricsDecoratorWithCollector creates a new metrics decorator with custom collector
func NewMetricsDecoratorWithCollector(wrapped backend.DatabaseBackend, config MetricsConfig, collector MetricsCollector) *MetricsDecorator {
	return &MetricsDecorator{
		BaseBackendDecorator: NewBaseBackendDecorator(wrapped),
		config:               config,
		collector:            collector,
	}
}

// Connect collects connection metrics
func (d *MetricsDecorator) Connect(ctx context.Context, config types.DatabaseConfig) error {
	start := time.Now()
	
	labels := map[string]string{
		"driver":   config.Driver,
		"database": config.Database,
		"host":     config.Host,
	}
	
	d.collector.IncrementCounter("database_connections_attempted_total", labels)
	
	err := d.wrapped.Connect(ctx, config)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_connection_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_connection_errors_total", errorLabels)
		}
	} else {
		d.collector.IncrementCounter("database_connections_successful_total", labels)
		d.collector.SetGauge("database_connected", 1, labels)
	}
	
	return err
}

// Disconnect collects disconnection metrics
func (d *MetricsDecorator) Disconnect(ctx context.Context) error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": "disconnect",
	}
	
	err := d.wrapped.Disconnect(ctx)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_operation_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_operation_errors_total", errorLabels)
		}
	} else {
		d.collector.SetGauge("database_connected", 0, map[string]string{})
	}
	
	return err
}

// Ping collects ping metrics
func (d *MetricsDecorator) Ping(ctx context.Context) error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": "ping",
	}
	
	err := d.wrapped.Ping(ctx)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_operation_duration_seconds", duration, labels)
	}
	
	d.collector.IncrementCounter("database_pings_total", labels)
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_operation_errors_total", errorLabels)
		}
	}
	
	return err
}

// BeginTransaction collects transaction metrics
func (d *MetricsDecorator) BeginTransaction(ctx context.Context) (backend.Transaction, error) {
	start := time.Now()
	
	labels := map[string]string{
		"operation": "begin_transaction",
	}
	
	d.collector.IncrementCounter("database_transactions_started_total", labels)
	
	tx, err := d.wrapped.BeginTransaction(ctx)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_operation_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_operation_errors_total", errorLabels)
		}
	} else {
		d.collector.AddToGauge("database_active_transactions", 1, map[string]string{})
		
		// Wrap transaction with metrics
		tx = NewMetricsTransaction(tx, d.config, d.collector)
	}
	
	return tx, err
}

// BeginTransactionWithOptions collects transaction metrics with options
func (d *MetricsDecorator) BeginTransactionWithOptions(ctx context.Context, opts backend.TransactionOptions) (backend.Transaction, error) {
	start := time.Now()
	
	labels := map[string]string{
		"operation":        "begin_transaction_with_options",
		"isolation_level":  fmt.Sprintf("%d", opts.IsolationLevel),
		"read_only":        fmt.Sprintf("%t", opts.ReadOnly),
	}
	
	d.collector.IncrementCounter("database_transactions_started_total", labels)
	
	tx, err := d.wrapped.BeginTransactionWithOptions(ctx, opts)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_operation_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_operation_errors_total", errorLabels)
		}
	} else {
		d.collector.AddToGauge("database_active_transactions", 1, map[string]string{})
		
		// Wrap transaction with metrics
		tx = NewMetricsTransaction(tx, d.config, d.collector)
	}
	
	return tx, err
}

// Execute collects query execution metrics
func (d *MetricsDecorator) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	start := time.Now()
	
	queryType := d.extractQueryType(query)
	labels := map[string]string{
		"operation":  "execute",
		"query_type": queryType,
	}
	
	if d.config.CollectQueries {
		d.collector.IncrementCounter("database_queries_total", labels)
	}
	
	result, err := d.wrapped.Execute(ctx, query, args...)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_query_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_query_errors_total", errorLabels)
		}
	} else {
		if result != nil {
			if rowsAffected, err := result.RowsAffected(); err == nil {
				d.collector.ObserveHistogram("database_rows_affected", float64(rowsAffected), labels)
			}
		}
	}
	
	return result, err
}

// Query collects query metrics
func (d *MetricsDecorator) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	start := time.Now()
	
	queryType := d.extractQueryType(query)
	labels := map[string]string{
		"operation":  "query",
		"query_type": queryType,
	}
	
	if d.config.CollectQueries {
		d.collector.IncrementCounter("database_queries_total", labels)
	}
	
	rows, err := d.wrapped.Query(ctx, query, args...)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_query_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_query_errors_total", errorLabels)
		}
	}
	
	return rows, err
}

// QueryRow collects single row query metrics
func (d *MetricsDecorator) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	start := time.Now()
	
	queryType := d.extractQueryType(query)
	labels := map[string]string{
		"operation":  "query_row",
		"query_type": queryType,
	}
	
	if d.config.CollectQueries {
		d.collector.IncrementCounter("database_queries_total", labels)
	}
	
	row := d.wrapped.QueryRow(ctx, query, args...)
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_query_duration_seconds", duration, labels)
	}
	
	return row
}

// CreateSchema collects schema creation metrics
func (d *MetricsDecorator) CreateSchema(ctx context.Context, schemaName string) error {
	return d.collectSchemaOperation("create_schema", func() error {
		return d.wrapped.CreateSchema(ctx, schemaName)
	})
}

// DropSchema collects schema deletion metrics
func (d *MetricsDecorator) DropSchema(ctx context.Context, schemaName string) error {
	return d.collectSchemaOperation("drop_schema", func() error {
		return d.wrapped.DropSchema(ctx, schemaName)
	})
}

// CreateTable collects table creation metrics
func (d *MetricsDecorator) CreateTable(ctx context.Context, tableDef backend.TableDefinition) error {
	return d.collectTableOperation("create_table", func() error {
		return d.wrapped.CreateTable(ctx, tableDef)
	})
}

// DropTable collects table deletion metrics
func (d *MetricsDecorator) DropTable(ctx context.Context, tableName string) error {
	return d.collectTableOperation("drop_table", func() error {
		return d.wrapped.DropTable(ctx, tableName)
	})
}

// CreateIndex collects index creation metrics
func (d *MetricsDecorator) CreateIndex(ctx context.Context, indexDef backend.IndexDefinition) error {
	return d.collectIndexOperation("create_index", func() error {
		return d.wrapped.CreateIndex(ctx, indexDef)
	})
}

// DropIndex collects index deletion metrics
func (d *MetricsDecorator) DropIndex(ctx context.Context, indexName string) error {
	return d.collectIndexOperation("drop_index", func() error {
		return d.wrapped.DropIndex(ctx, indexName)
	})
}

// Helper methods

// collectSchemaOperation collects metrics for schema operations
func (d *MetricsDecorator) collectSchemaOperation(operation string, fn func() error) error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": operation,
		"type":      "schema",
	}
	
	d.collector.IncrementCounter("database_ddl_operations_total", labels)
	
	err := fn()
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_ddl_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_ddl_errors_total", errorLabels)
		}
	}
	
	return err
}

// collectTableOperation collects metrics for table operations
func (d *MetricsDecorator) collectTableOperation(operation string, fn func() error) error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": operation,
		"type":      "table",
	}
	
	d.collector.IncrementCounter("database_ddl_operations_total", labels)
	
	err := fn()
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_ddl_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_ddl_errors_total", errorLabels)
		}
	}
	
	return err
}

// collectIndexOperation collects metrics for index operations
func (d *MetricsDecorator) collectIndexOperation(operation string, fn func() error) error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": operation,
		"type":      "index",
	}
	
	d.collector.IncrementCounter("database_ddl_operations_total", labels)
	
	err := fn()
	duration := time.Since(start).Seconds()
	
	if d.config.CollectDuration {
		d.collector.ObserveHistogram("database_ddl_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if d.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = d.classifyError(err)
			d.collector.IncrementCounter("database_ddl_errors_total", errorLabels)
		}
	}
	
	return err
}

// extractQueryType extracts the query type from SQL
func (d *MetricsDecorator) extractQueryType(query string) string {
	query = strings.TrimSpace(strings.ToUpper(query))
	
	if strings.HasPrefix(query, "SELECT") {
		return "SELECT"
	} else if strings.HasPrefix(query, "INSERT") {
		return "INSERT"
	} else if strings.HasPrefix(query, "UPDATE") {
		return "UPDATE"
	} else if strings.HasPrefix(query, "DELETE") {
		return "DELETE"
	} else if strings.HasPrefix(query, "CREATE") {
		return "CREATE"
	} else if strings.HasPrefix(query, "DROP") {
		return "DROP"
	} else if strings.HasPrefix(query, "ALTER") {
		return "ALTER"
	} else if strings.HasPrefix(query, "TRUNCATE") {
		return "TRUNCATE"
	} else {
		return "OTHER"
	}
}

// classifyError classifies errors into categories
func (d *MetricsDecorator) classifyError(err error) string {
	errStr := strings.ToLower(err.Error())
	
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "connect") {
		return "connection"
	} else if strings.Contains(errStr, "timeout") {
		return "timeout"
	} else if strings.Contains(errStr, "syntax") {
		return "syntax"
	} else if strings.Contains(errStr, "constraint") || strings.Contains(errStr, "violation") {
		return "constraint"
	} else if strings.Contains(errStr, "permission") || strings.Contains(errStr, "access") {
		return "permission"
	} else if strings.Contains(errStr, "deadlock") {
		return "deadlock"
	} else if strings.Contains(errStr, "duplicate") {
		return "duplicate"
	} else {
		return "other"
	}
}

// MetricsTransaction wraps a transaction with metrics collection
type MetricsTransaction struct {
	wrapped   backend.Transaction
	config    MetricsConfig
	collector MetricsCollector
}

// NewMetricsTransaction creates a new metrics transaction wrapper
func NewMetricsTransaction(wrapped backend.Transaction, config MetricsConfig, collector MetricsCollector) *MetricsTransaction {
	return &MetricsTransaction{
		wrapped:   wrapped,
		config:    config,
		collector: collector,
	}
}

// Commit collects transaction commit metrics
func (t *MetricsTransaction) Commit() error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": "commit",
	}
	
	t.collector.IncrementCounter("database_transaction_operations_total", labels)
	
	err := t.wrapped.Commit()
	duration := time.Since(start).Seconds()
	
	if t.config.CollectDuration {
		t.collector.ObserveHistogram("database_transaction_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if t.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = t.classifyError(err)
			t.collector.IncrementCounter("database_transaction_errors_total", errorLabels)
		}
	} else {
		t.collector.IncrementCounter("database_transactions_committed_total", map[string]string{})
	}
	
	t.collector.AddToGauge("database_active_transactions", -1, map[string]string{})
	
	return err
}

// Rollback collects transaction rollback metrics
func (t *MetricsTransaction) Rollback() error {
	start := time.Now()
	
	labels := map[string]string{
		"operation": "rollback",
	}
	
	t.collector.IncrementCounter("database_transaction_operations_total", labels)
	
	err := t.wrapped.Rollback()
	duration := time.Since(start).Seconds()
	
	if t.config.CollectDuration {
		t.collector.ObserveHistogram("database_transaction_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if t.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = t.classifyError(err)
			t.collector.IncrementCounter("database_transaction_errors_total", errorLabels)
		}
	} else {
		t.collector.IncrementCounter("database_transactions_rolled_back_total", map[string]string{})
	}

	t.collector.AddToGauge("database_active_transactions", -1, map[string]string{})

	return err
}

// Savepoint delegates savepoint creation to the wrapped transaction.
func (t *MetricsTransaction) Savepoint(name string) error {
	return t.wrapped.Savepoint(name)
}

// RollbackToSavepoint delegates savepoint rollback to the wrapped transaction.
func (t *MetricsTransaction) RollbackToSavepoint(name string) error {
	return t.wrapped.RollbackToSavepoint(name)
}

// ReleaseSavepoint delegates savepoint release to the wrapped transaction.
func (t *MetricsTransaction) ReleaseSavepoint(name string) error {
	return t.wrapped.ReleaseSavepoint(name)
}

// Execute collects transaction query execution metrics
func (t *MetricsTransaction) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	start := time.Now()
	
	queryType := t.extractQueryType(query)
	labels := map[string]string{
		"operation":  "tx_execute",
		"query_type": queryType,
	}
	
	if t.config.CollectQueries {
		t.collector.IncrementCounter("database_transaction_queries_total", labels)
	}
	
	result, err := t.wrapped.Execute(ctx, query, args...)
	duration := time.Since(start).Seconds()
	
	if t.config.CollectDuration {
		t.collector.ObserveHistogram("database_transaction_query_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if t.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = t.classifyError(err)
			t.collector.IncrementCounter("database_transaction_query_errors_total", errorLabels)
		}
	}
	
	return result, err
}

// Query collects transaction query metrics
func (t *MetricsTransaction) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	start := time.Now()
	
	queryType := t.extractQueryType(query)
	labels := map[string]string{
		"operation":  "tx_query",
		"query_type": queryType,
	}
	
	if t.config.CollectQueries {
		t.collector.IncrementCounter("database_transaction_queries_total", labels)
	}
	
	rows, err := t.wrapped.Query(ctx, query, args...)
	duration := time.Since(start).Seconds()
	
	if t.config.CollectDuration {
		t.collector.ObserveHistogram("database_transaction_query_duration_seconds", duration, labels)
	}
	
	if err != nil {
		if t.config.CollectErrors {
			errorLabels := make(map[string]string)
			for k, v := range labels {
				errorLabels[k] = v
			}
			errorLabels["error_type"] = t.classifyError(err)
			t.collector.IncrementCounter("database_transaction_query_errors_total", errorLabels)
		}
	}
	
	return rows, err
}

// QueryRow collects transaction single row query metrics
func (t *MetricsTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	start := time.Now()
	
	queryType := t.extractQueryType(query)
	labels := map[string]string{
		"operation":  "tx_query_row",
		"query_type": queryType,
	}
	
	if t.config.CollectQueries {
		t.collector.IncrementCounter("database_transaction_queries_total", labels)
	}
	
	row := t.wrapped.QueryRow(ctx, query, args...)
	duration := time.Since(start).Seconds()
	
	if t.config.CollectDuration {
		t.collector.ObserveHistogram("database_transaction_query_duration_seconds", duration, labels)
	}
	
	return row
}

// Helper methods for transaction metrics

func (t *MetricsTransaction) extractQueryType(query string) string {
	query = strings.TrimSpace(strings.ToUpper(query))
	
	if strings.HasPrefix(query, "SELECT") {
		return "SELECT"
	} else if strings.HasPrefix(query, "INSERT") {
		return "INSERT"
	} else if strings.HasPrefix(query, "UPDATE") {
		return "UPDATE"
	} else if strings.HasPrefix(query, "DELETE") {
		return "DELETE"
	} else if strings.HasPrefix(query, "CREATE") {
		return "CREATE"
	} else if strings.HasPrefix(query, "DROP") {
		return "DROP"
	} else if strings.HasPrefix(query, "ALTER") {
		return "ALTER"
	} else if strings.HasPrefix(query, "TRUNCATE") {
		return "TRUNCATE"
	} else {
		return "OTHER"
	}
}

func (t *MetricsTransaction) classifyError(err error) string {
	errStr := strings.ToLower(err.Error())
	
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "connect") {
		return "connection"
	} else if strings.Contains(errStr, "timeout") {
		return "timeout"
	} else if strings.Contains(errStr, "syntax") {
		return "syntax"
	} else if strings.Contains(errStr, "constraint") || strings.Contains(errStr, "violation") {
		return "constraint"
	} else if strings.Contains(errStr, "permission") || strings.Contains(errStr, "access") {
		return "permission"
	} else if strings.Contains(errStr, "deadlock") {
		return "deadlock"
	} else if strings.Contains(errStr, "duplicate") {
		return "duplicate"
	} else {
		return "other"
	}
}

// MetricsDecoratorFactory creates metrics decorators
type MetricsDecoratorFactory struct{}

// CreateDecorator creates a metrics decorator from configuration
func (f *MetricsDecoratorFactory) CreateDecorator(config DecoratorConfig) (BackendDecorator, error) {
	metricsConfig := ParseMetricsConfig(config.Options)
	
	return &MetricsDecorator{
		BaseBackendDecorator: &BaseBackendDecorator{},
		config:               metricsConfig,
		collector:            NewSimpleMetricsCollector(),
	}, nil
}

// SupportedTypes returns the supported decorator types
func (f *MetricsDecoratorFactory) SupportedTypes() []DecoratorType {
	return []DecoratorType{DecoratorTypeMetrics}
}

// Register the metrics decorator factory
func init() {
	GlobalDecoratorRegistry.RegisterFactory(DecoratorTypeMetrics, &MetricsDecoratorFactory{})
}

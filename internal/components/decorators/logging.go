/**
 * @file logging.go
 * @description Logging decorator for database backend operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/decorators/logging.go
 */

package decorators

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/backend"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// LoggingDecorator adds comprehensive logging to database operations
type LoggingDecorator struct {
	*BaseBackendDecorator
	config LoggingConfig
	logger *slog.Logger
}

// NewLoggingDecorator creates a new logging decorator
func NewLoggingDecorator(wrapped backend.DatabaseBackend, config LoggingConfig) *LoggingDecorator {
	logger := slog.Default()
	
	return &LoggingDecorator{
		BaseBackendDecorator: NewBaseBackendDecorator(wrapped),
		config:               config,
		logger:               logger,
	}
}

// NewLoggingDecoratorWithLogger creates a new logging decorator with custom logger
func NewLoggingDecoratorWithLogger(wrapped backend.DatabaseBackend, config LoggingConfig, logger *slog.Logger) *LoggingDecorator {
	return &LoggingDecorator{
		BaseBackendDecorator: NewBaseBackendDecorator(wrapped),
		config:               config,
		logger:               logger,
	}
}

// Connect logs connection attempts
func (d *LoggingDecorator) Connect(ctx context.Context, config types.DatabaseConfig) error {
	start := time.Now()
	
	d.logger.Info("Connecting to database",
		"driver", config.Driver,
		"host", config.Host,
		"port", config.Port,
		"database", config.Database,
		"user", config.User,
	)
	
	err := d.wrapped.Connect(ctx, config)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to connect to database",
			"error", err,
			"duration", duration,
			"driver", config.Driver,
		)
	} else {
		d.logger.Info("Successfully connected to database",
			"duration", duration,
			"driver", config.Driver,
		)
	}
	
	return err
}

// Disconnect logs disconnection
func (d *LoggingDecorator) Disconnect(ctx context.Context) error {
	start := time.Now()
	
	d.logger.Info("Disconnecting from database")
	
	err := d.wrapped.Disconnect(ctx)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to disconnect from database",
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Successfully disconnected from database",
			"duration", duration,
		)
	}
	
	return err
}

// Ping logs ping operations
func (d *LoggingDecorator) Ping(ctx context.Context) error {
	start := time.Now()
	
	d.logger.Debug("Pinging database")
	
	err := d.wrapped.Ping(ctx)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Database ping failed",
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Debug("Database ping successful",
			"duration", duration,
		)
	}
	
	return err
}

// BeginTransaction logs transaction start
func (d *LoggingDecorator) BeginTransaction(ctx context.Context) (backend.Transaction, error) {
	start := time.Now()
	
	d.logger.Debug("Beginning transaction")
	
	tx, err := d.wrapped.BeginTransaction(ctx)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to begin transaction",
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Debug("Transaction started successfully",
			"duration", duration,
		)
		
		// Wrap transaction with logging
		tx = NewLoggingTransaction(tx, d.config, d.logger)
	}
	
	return tx, err
}

// BeginTransactionWithOptions logs transaction start with options
func (d *LoggingDecorator) BeginTransactionWithOptions(ctx context.Context, opts backend.TransactionOptions) (backend.Transaction, error) {
	start := time.Now()
	
	d.logger.Debug("Beginning transaction with options",
		"isolation_level", opts.IsolationLevel,
		"read_only", opts.ReadOnly,
		"timeout", opts.Timeout,
	)
	
	tx, err := d.wrapped.BeginTransactionWithOptions(ctx, opts)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to begin transaction with options",
			"error", err,
			"duration", duration,
			"isolation_level", opts.IsolationLevel,
		)
	} else {
		d.logger.Debug("Transaction with options started successfully",
			"duration", duration,
			"isolation_level", opts.IsolationLevel,
		)
		
		// Wrap transaction with logging
		tx = NewLoggingTransaction(tx, d.config, d.logger)
	}
	
	return tx, err
}

// Execute logs query execution
func (d *LoggingDecorator) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	start := time.Now()
	
	if d.config.LogQueries {
		sanitizedQuery := d.sanitizeQuery(query)
		sanitizedArgs := d.sanitizeArgs(args)
		
		d.logger.Debug("Executing query",
			"query", sanitizedQuery,
			"args", sanitizedArgs,
		)
	}
	
	result, err := d.wrapped.Execute(ctx, query, args...)
	duration := time.Since(start)
	
	d.logQueryResult("Execute", query, args, duration, err, result)
	
	return result, err
}

// Query logs query operations
func (d *LoggingDecorator) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	start := time.Now()
	
	if d.config.LogQueries {
		sanitizedQuery := d.sanitizeQuery(query)
		sanitizedArgs := d.sanitizeArgs(args)
		
		d.logger.Debug("Executing query",
			"query", sanitizedQuery,
			"args", sanitizedArgs,
		)
	}
	
	rows, err := d.wrapped.Query(ctx, query, args...)
	duration := time.Since(start)
	
	d.logQueryResult("Query", query, args, duration, err, nil)
	
	return rows, err
}

// QueryRow logs single row query operations
func (d *LoggingDecorator) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	start := time.Now()
	
	if d.config.LogQueries {
		sanitizedQuery := d.sanitizeQuery(query)
		sanitizedArgs := d.sanitizeArgs(args)
		
		d.logger.Debug("Executing query row",
			"query", sanitizedQuery,
			"args", sanitizedArgs,
		)
	}
	
	row := d.wrapped.QueryRow(ctx, query, args...)
	duration := time.Since(start)
	
	d.logQueryResult("QueryRow", query, args, duration, nil, nil)
	
	return row
}

// CreateSchema logs schema creation
func (d *LoggingDecorator) CreateSchema(ctx context.Context, schemaName string) error {
	start := time.Now()
	
	d.logger.Info("Creating schema",
		"schema", schemaName,
	)
	
	err := d.wrapped.CreateSchema(ctx, schemaName)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to create schema",
			"schema", schemaName,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Schema created successfully",
			"schema", schemaName,
			"duration", duration,
		)
	}
	
	return err
}

// DropSchema logs schema deletion
func (d *LoggingDecorator) DropSchema(ctx context.Context, schemaName string) error {
	start := time.Now()
	
	d.logger.Warn("Dropping schema",
		"schema", schemaName,
	)
	
	err := d.wrapped.DropSchema(ctx, schemaName)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to drop schema",
			"schema", schemaName,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Warn("Schema dropped successfully",
			"schema", schemaName,
			"duration", duration,
		)
	}
	
	return err
}

// CreateTable logs table creation
func (d *LoggingDecorator) CreateTable(ctx context.Context, tableDef backend.TableDefinition) error {
	start := time.Now()
	
	d.logger.Info("Creating table",
		"table", tableDef.Name,
		"columns", len(tableDef.Columns),
	)
	
	err := d.wrapped.CreateTable(ctx, tableDef)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to create table",
			"table", tableDef.Name,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Table created successfully",
			"table", tableDef.Name,
			"duration", duration,
		)
	}
	
	return err
}

// DropTable logs table deletion
func (d *LoggingDecorator) DropTable(ctx context.Context, tableName string) error {
	start := time.Now()
	
	d.logger.Warn("Dropping table",
		"table", tableName,
	)
	
	err := d.wrapped.DropTable(ctx, tableName)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to drop table",
			"table", tableName,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Warn("Table dropped successfully",
			"table", tableName,
			"duration", duration,
		)
	}
	
	return err
}

// CreateIndex logs index creation
func (d *LoggingDecorator) CreateIndex(ctx context.Context, indexDef backend.IndexDefinition) error {
	start := time.Now()
	
	d.logger.Info("Creating index",
		"index", indexDef.Name,
		"table", indexDef.TableName,
		"columns", indexDef.Columns,
		"unique", indexDef.Unique,
	)
	
	err := d.wrapped.CreateIndex(ctx, indexDef)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to create index",
			"index", indexDef.Name,
			"table", indexDef.TableName,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Index created successfully",
			"index", indexDef.Name,
			"table", indexDef.TableName,
			"duration", duration,
		)
	}
	
	return err
}

// DropIndex logs index deletion
func (d *LoggingDecorator) DropIndex(ctx context.Context, indexName string) error {
	start := time.Now()
	
	d.logger.Info("Dropping index",
		"index", indexName,
	)
	
	err := d.wrapped.DropIndex(ctx, indexName)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to drop index",
			"index", indexName,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Index dropped successfully",
			"index", indexName,
			"duration", duration,
		)
	}
	
	return err
}

// CreateBackup logs backup operations
func (d *LoggingDecorator) CreateBackup(ctx context.Context, backupConfig backend.BackupConfig) error {
	start := time.Now()
	
	d.logger.Info("Creating backup",
		"path", backupConfig.Path,
		"compress", backupConfig.Compress,
	)
	
	err := d.wrapped.CreateBackup(ctx, backupConfig)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to create backup",
			"path", backupConfig.Path,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Backup created successfully",
			"path", backupConfig.Path,
			"duration", duration,
		)
	}
	
	return err
}

// RestoreBackup logs restore operations
func (d *LoggingDecorator) RestoreBackup(ctx context.Context, restoreConfig backend.RestoreConfig) error {
	start := time.Now()
	
	d.logger.Info("Restoring backup",
		"path", restoreConfig.Path,
	)
	
	err := d.wrapped.RestoreBackup(ctx, restoreConfig)
	duration := time.Since(start)
	
	if err != nil {
		d.logger.Error("Failed to restore backup",
			"path", restoreConfig.Path,
			"error", err,
			"duration", duration,
		)
	} else {
		d.logger.Info("Backup restored successfully",
			"path", restoreConfig.Path,
			"duration", duration,
		)
	}
	
	return err
}

// Helper methods

// logQueryResult logs the result of a query operation
func (d *LoggingDecorator) logQueryResult(operation, query string, args []interface{}, duration time.Duration, err error, result backend.QueryResult) {
	logAttrs := []slog.Attr{
		slog.String("operation", operation),
		slog.Duration("duration", duration),
	}
	
	if d.config.LogDuration && duration > d.config.SlowQueryThresh {
		logAttrs = append(logAttrs, slog.Bool("slow_query", true))
	}
	
	if d.config.LogResults && result != nil {
		if rowsAffected, err := result.RowsAffected(); err == nil {
			logAttrs = append(logAttrs, slog.Int64("rows_affected", rowsAffected))
		}
		if lastInsertID, err := result.LastInsertId(); err == nil {
			logAttrs = append(logAttrs, slog.Int64("last_insert_id", lastInsertID))
		}
	}
	
	if err != nil {
		d.logger.Error("Query failed",
			append(logAttrs, slog.String("error", err.Error()))...,
		)
	} else {
		level := slog.LevelDebug
		if duration > d.config.SlowQueryThresh {
			level = slog.LevelWarn
		}
		
		d.logger.Log(context.Background(), level, "Query completed", logAttrs...)
	}
}

// sanitizeQuery removes sensitive information from queries for logging
func (d *LoggingDecorator) sanitizeQuery(query string) string {
	if !d.config.SanitizeArgs {
		return query
	}
	
	// Remove potential passwords, tokens, etc.
	patterns := []string{
		`(?i)(password\s*=\s*)'[^']*'`,
		`(?i)(password\s*=\s*)"[^"]*"`,
		`(?i)(token\s*=\s*)'[^']*'`,
		`(?i)(token\s*=\s*)"[^"]*"`,
		`(?i)(secret\s*=\s*)'[^']*'`,
		`(?i)(secret\s*=\s*)"[^"]*"`,
	}
	
	sanitized := query
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		sanitized = re.ReplaceAllString(sanitized, "${1}'***'")
	}
	
	return sanitized
}

// sanitizeArgs removes sensitive information from arguments for logging
func (d *LoggingDecorator) sanitizeArgs(args []interface{}) []interface{} {
	if !d.config.SanitizeArgs {
		return args
	}
	
	sanitized := make([]interface{}, len(args))
	for i, arg := range args {
		if str, ok := arg.(string); ok {
			// Check if string looks like sensitive data
			lower := strings.ToLower(str)
			if strings.Contains(lower, "password") ||
				strings.Contains(lower, "token") ||
				strings.Contains(lower, "secret") ||
				strings.Contains(lower, "key") {
				sanitized[i] = "***"
			} else {
				sanitized[i] = arg
			}
		} else {
			sanitized[i] = arg
		}
	}
	
	return sanitized
}

// LoggingTransaction wraps a transaction with logging
type LoggingTransaction struct {
	wrapped backend.Transaction
	config  LoggingConfig
	logger  *slog.Logger
}

// NewLoggingTransaction creates a new logging transaction wrapper
func NewLoggingTransaction(wrapped backend.Transaction, config LoggingConfig, logger *slog.Logger) *LoggingTransaction {
	return &LoggingTransaction{
		wrapped: wrapped,
		config:  config,
		logger:  logger,
	}
}

// Commit logs transaction commit
func (t *LoggingTransaction) Commit() error {
	start := time.Now()
	
	t.logger.Debug("Committing transaction")
	
	err := t.wrapped.Commit()
	duration := time.Since(start)
	
	if err != nil {
		t.logger.Error("Failed to commit transaction",
			"error", err,
			"duration", duration,
		)
	} else {
		t.logger.Debug("Transaction committed successfully",
			"duration", duration,
		)
	}
	
	return err
}

// Rollback logs transaction rollback
func (t *LoggingTransaction) Rollback() error {
	start := time.Now()
	
	t.logger.Debug("Rolling back transaction")
	
	err := t.wrapped.Rollback()
	duration := time.Since(start)
	
	if err != nil {
		t.logger.Error("Failed to rollback transaction",
			"error", err,
			"duration", duration,
		)
	} else {
		t.logger.Debug("Transaction rolled back successfully",
			"duration", duration,
		)
	}
	
	return err
}

// Execute logs transaction query execution
func (t *LoggingTransaction) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	start := time.Now()
	
	if t.config.LogQueries {
		sanitizedQuery := t.sanitizeQuery(query)
		sanitizedArgs := t.sanitizeArgs(args)
		
		t.logger.Debug("Executing transaction query",
			"query", sanitizedQuery,
			"args", sanitizedArgs,
		)
	}
	
	result, err := t.wrapped.Execute(ctx, query, args...)
	duration := time.Since(start)
	
	t.logQueryResult("TxExecute", query, args, duration, err, result)
	
	return result, err
}

// Query logs transaction query operations
func (t *LoggingTransaction) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	start := time.Now()
	
	if t.config.LogQueries {
		sanitizedQuery := t.sanitizeQuery(query)
		sanitizedArgs := t.sanitizeArgs(args)
		
		t.logger.Debug("Executing transaction query",
			"query", sanitizedQuery,
			"args", sanitizedArgs,
		)
	}
	
	rows, err := t.wrapped.Query(ctx, query, args...)
	duration := time.Since(start)
	
	t.logQueryResult("TxQuery", query, args, duration, err, nil)
	
	return rows, err
}

// QueryRow logs transaction single row query operations
func (t *LoggingTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	start := time.Now()
	
	if t.config.LogQueries {
		sanitizedQuery := t.sanitizeQuery(query)
		sanitizedArgs := t.sanitizeArgs(args)
		
		t.logger.Debug("Executing transaction query row",
			"query", sanitizedQuery,
			"args", sanitizedArgs,
		)
	}
	
	row := t.wrapped.QueryRow(ctx, query, args...)
	duration := time.Since(start)
	
	t.logQueryResult("TxQueryRow", query, args, duration, nil, nil)
	
	return row
}

// Helper methods for transaction logging

func (t *LoggingTransaction) logQueryResult(operation, query string, args []interface{}, duration time.Duration, err error, result backend.QueryResult) {
	logAttrs := []slog.Attr{
		slog.String("operation", operation),
		slog.Duration("duration", duration),
	}
	
	if t.config.LogDuration && duration > t.config.SlowQueryThresh {
		logAttrs = append(logAttrs, slog.Bool("slow_query", true))
	}
	
	if t.config.LogResults && result != nil {
		if rowsAffected, err := result.RowsAffected(); err == nil {
			logAttrs = append(logAttrs, slog.Int64("rows_affected", rowsAffected))
		}
		if lastInsertID, err := result.LastInsertId(); err == nil {
			logAttrs = append(logAttrs, slog.Int64("last_insert_id", lastInsertID))
		}
	}
	
	if err != nil {
		t.logger.Error("Transaction query failed",
			append(logAttrs, slog.String("error", err.Error()))...,
		)
	} else {
		level := slog.LevelDebug
		if duration > t.config.SlowQueryThresh {
			level = slog.LevelWarn
		}
		
		t.logger.Log(context.Background(), level, "Transaction query completed", logAttrs...)
	}
}

func (t *LoggingTransaction) sanitizeQuery(query string) string {
	if !t.config.SanitizeArgs {
		return query
	}
	
	// Remove potential passwords, tokens, etc.
	patterns := []string{
		`(?i)(password\s*=\s*)'[^']*'`,
		`(?i)(password\s*=\s*)"[^"]*"`,
		`(?i)(token\s*=\s*)'[^']*'`,
		`(?i)(token\s*=\s*)"[^"]*"`,
		`(?i)(secret\s*=\s*)'[^']*'`,
		`(?i)(secret\s*=\s*)"[^"]*"`,
	}
	
	sanitized := query
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		sanitized = re.ReplaceAllString(sanitized, "${1}'***'")
	}
	
	return sanitized
}

func (t *LoggingTransaction) sanitizeArgs(args []interface{}) []interface{} {
	if !t.config.SanitizeArgs {
		return args
	}
	
	sanitized := make([]interface{}, len(args))
	for i, arg := range args {
		if str, ok := arg.(string); ok {
			// Check if string looks like sensitive data
			lower := strings.ToLower(str)
			if strings.Contains(lower, "password") ||
				strings.Contains(lower, "token") ||
				strings.Contains(lower, "secret") ||
				strings.Contains(lower, "key") {
				sanitized[i] = "***"
			} else {
				sanitized[i] = arg
			}
		} else {
			sanitized[i] = arg
		}
	}
	
	return sanitized
}

// LoggingDecoratorFactory creates logging decorators
type LoggingDecoratorFactory struct{}

// CreateDecorator creates a logging decorator from configuration
func (f *LoggingDecoratorFactory) CreateDecorator(config DecoratorConfig) (BackendDecorator, error) {
	loggingConfig := ParseLoggingConfig(config.Options)
	
	return &LoggingDecorator{
		BaseBackendDecorator: &BaseBackendDecorator{},
		config:               loggingConfig,
		logger:               slog.Default(),
	}, nil
}

// SupportedTypes returns the supported decorator types
func (f *LoggingDecoratorFactory) SupportedTypes() []DecoratorType {
	return []DecoratorType{DecoratorTypeLogging}
}

// Register the logging decorator factory
func init() {
	GlobalDecoratorRegistry.RegisterFactory(DecoratorTypeLogging, &LoggingDecoratorFactory{})
}

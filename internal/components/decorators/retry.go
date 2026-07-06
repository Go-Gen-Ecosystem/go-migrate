/**
 * @file retry.go
 * @description Retry decorator for resilient database operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/decorators/retry.go
 */

package decorators

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/backend"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// RetryStrategy defines different retry strategies
type RetryStrategy string

const (
	RetryStrategyFixed       RetryStrategy = "fixed"
	RetryStrategyExponential RetryStrategy = "exponential"
	RetryStrategyLinear      RetryStrategy = "linear"
	RetryStrategyJittered    RetryStrategy = "jittered"
)

// RetryAttempt represents a single retry attempt
type RetryAttempt struct {
	AttemptNumber int           `json:"attempt_number"`
	StartTime     time.Time     `json:"start_time"`
	Duration      time.Duration `json:"duration"`
	Error         string        `json:"error,omitempty"`
	Success       bool          `json:"success"`
	DelayBefore   time.Duration `json:"delay_before"`
}

// RetryResult represents the result of a retry operation
type RetryResult struct {
	TotalAttempts    int            `json:"total_attempts"`
	TotalDuration    time.Duration  `json:"total_duration"`
	Success          bool           `json:"success"`
	FinalError       string         `json:"final_error,omitempty"`
	Attempts         []RetryAttempt `json:"attempts"`
	OperationType    string         `json:"operation_type"`
	Query            string         `json:"query,omitempty"`
}

// RetryableError defines errors that should trigger retries
type RetryableError interface {
	error
	IsRetryable() bool
	GetRetryAfter() time.Duration
}

// DefaultRetryableError implements RetryableError
type DefaultRetryableError struct {
	Err        error
	Retryable  bool
	RetryAfter time.Duration
}

// Error returns the error message
func (e *DefaultRetryableError) Error() string {
	return e.Err.Error()
}

// IsRetryable returns whether the error is retryable
func (e *DefaultRetryableError) IsRetryable() bool {
	return e.Retryable
}

// GetRetryAfter returns the suggested retry delay
func (e *DefaultRetryableError) GetRetryAfter() time.Duration {
	return e.RetryAfter
}

// ErrorClassifier classifies errors for retry decisions
type ErrorClassifier interface {
	// IsRetryable determines if an error should trigger a retry
	IsRetryable(err error) bool
	
	// GetRetryDelay returns the suggested delay before retry
	GetRetryDelay(err error, attempt int) time.Duration
	
	// ShouldStopRetrying determines if retrying should stop
	ShouldStopRetrying(err error, attempt int) bool
}

// DefaultErrorClassifier implements basic error classification
type DefaultErrorClassifier struct {
	retryablePatterns []string
	fatalPatterns     []string
}

// NewDefaultErrorClassifier creates a new default error classifier
func NewDefaultErrorClassifier() *DefaultErrorClassifier {
	return &DefaultErrorClassifier{
		retryablePatterns: []string{
			"connection refused",
			"connection reset",
			"timeout",
			"temporary failure",
			"deadlock",
			"lock wait timeout",
			"connection lost",
			"server shutdown",
			"too many connections",
		},
		fatalPatterns: []string{
			"syntax error",
			"permission denied",
			"authentication failed",
			"database does not exist",
			"table does not exist",
			"column does not exist",
			"constraint violation",
			"duplicate key",
		},
	}
}

// IsRetryable determines if an error should trigger a retry
func (c *DefaultErrorClassifier) IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	
	errMsg := strings.ToLower(err.Error())
	
	// Check for fatal patterns first
	for _, pattern := range c.fatalPatterns {
		if strings.Contains(errMsg, pattern) {
			return false
		}
	}
	
	// Check for retryable patterns
	for _, pattern := range c.retryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}
	
	return false
}

// GetRetryDelay returns the suggested delay before retry
func (c *DefaultErrorClassifier) GetRetryDelay(err error, attempt int) time.Duration {
	// Default delay based on error type
	errMsg := strings.ToLower(err.Error())
	
	if strings.Contains(errMsg, "deadlock") {
		return time.Duration(rand.Intn(1000)) * time.Millisecond
	}
	
	if strings.Contains(errMsg, "timeout") {
		return 5 * time.Second
	}
	
	if strings.Contains(errMsg, "connection") {
		return 2 * time.Second
	}
	
	return 1 * time.Second
}

// ShouldStopRetrying determines if retrying should stop
func (c *DefaultErrorClassifier) ShouldStopRetrying(err error, attempt int) bool {
	// Stop retrying for fatal errors
	return !c.IsRetryable(err)
}

// DelayCalculator calculates retry delays based on strategy
type DelayCalculator interface {
	// CalculateDelay calculates the delay for a given attempt
	CalculateDelay(attempt int, baseDelay time.Duration) time.Duration
}

// FixedDelayCalculator implements fixed delay strategy
type FixedDelayCalculator struct{}

// CalculateDelay returns a fixed delay
func (c *FixedDelayCalculator) CalculateDelay(attempt int, baseDelay time.Duration) time.Duration {
	return baseDelay
}

// ExponentialDelayCalculator implements exponential backoff strategy
type ExponentialDelayCalculator struct {
	Multiplier float64
	MaxDelay   time.Duration
}

// CalculateDelay returns exponentially increasing delay
func (c *ExponentialDelayCalculator) CalculateDelay(attempt int, baseDelay time.Duration) time.Duration {
	delay := time.Duration(float64(baseDelay) * math.Pow(c.Multiplier, float64(attempt-1)))
	if delay > c.MaxDelay {
		delay = c.MaxDelay
	}
	return delay
}

// LinearDelayCalculator implements linear delay strategy
type LinearDelayCalculator struct {
	Increment time.Duration
	MaxDelay  time.Duration
}

// CalculateDelay returns linearly increasing delay
func (c *LinearDelayCalculator) CalculateDelay(attempt int, baseDelay time.Duration) time.Duration {
	delay := baseDelay + time.Duration(attempt-1)*c.Increment
	if delay > c.MaxDelay {
		delay = c.MaxDelay
	}
	return delay
}

// JitteredDelayCalculator implements jittered delay strategy
type JitteredDelayCalculator struct {
	BaseCalculator DelayCalculator
	JitterFactor   float64 // 0.0 to 1.0
}

// CalculateDelay returns delay with random jitter
func (c *JitteredDelayCalculator) CalculateDelay(attempt int, baseDelay time.Duration) time.Duration {
	baseDelay = c.BaseCalculator.CalculateDelay(attempt, baseDelay)
	jitter := time.Duration(float64(baseDelay) * c.JitterFactor * rand.Float64())
	return baseDelay + jitter
}

// RetryTransaction wraps a transaction with retry capabilities
type RetryTransaction struct {
	backend.Transaction
	decorator *RetryDecorator
	attempts  []RetryAttempt
}

// Execute executes with retry logic
func (t *RetryTransaction) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	result, err := t.decorator.executeWithRetry(ctx, "TXN_EXECUTE", func() (interface{}, error) {
		return t.Transaction.Execute(ctx, query, args...)
	})
	if err != nil {
		return nil, err
	}
	queryResult, ok := result.(backend.QueryResult)
	if !ok {
		return nil, fmt.Errorf("unexpected result type %T from transaction execute", result)
	}
	return queryResult, nil
}

// Query queries with retry logic
func (t *RetryTransaction) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	result, err := t.decorator.executeWithRetry(ctx, "TXN_QUERY", func() (interface{}, error) {
		return t.Transaction.Query(ctx, query, args...)
	})
	if err != nil {
		return nil, err
	}
	return result.(backend.QueryRows), nil
}

// QueryRow queries single row with retry logic
func (t *RetryTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	result, err := t.decorator.executeWithRetry(ctx, "TXN_QUERY_ROW", func() (interface{}, error) {
		return t.Transaction.QueryRow(ctx, query, args...), nil
	})
	if err != nil {
		return &ErrorQueryRow{err: err}
	}
	return result.(backend.QueryRow)
}

// Commit commits with retry logic
func (t *RetryTransaction) Commit() error {
	_, err := t.decorator.executeWithRetry(context.Background(), "TXN_COMMIT", func() (interface{}, error) {
		return nil, t.Transaction.Commit()
	})
	return err
}

// Rollback rolls back (no retry needed)
func (t *RetryTransaction) Rollback() error {
	return t.Transaction.Rollback()
}

// RetryDecorator adds retry capabilities to database operations
type RetryDecorator struct {
	*BaseBackendDecorator
	config          RetryConfig
	classifier      ErrorClassifier
	delayCalculator DelayCalculator
	results         []RetryResult
}

// NewRetryDecorator creates a new retry decorator
func NewRetryDecorator(wrapped backend.DatabaseBackend, config RetryConfig) *RetryDecorator {
	decorator := &RetryDecorator{
		BaseBackendDecorator: NewBaseBackendDecorator(wrapped),
		config:               config,
		classifier:           NewDefaultErrorClassifier(),
		results:              make([]RetryResult, 0),
	}
	
	// Set up delay calculator based on strategy
	switch RetryStrategy(config.Strategy) {
	case RetryStrategyFixed:
		decorator.delayCalculator = &FixedDelayCalculator{}
	case RetryStrategyExponential:
		decorator.delayCalculator = &ExponentialDelayCalculator{
			Multiplier: config.BackoffMultiplier,
			MaxDelay:   config.MaxDelay,
		}
	case RetryStrategyLinear:
		decorator.delayCalculator = &LinearDelayCalculator{
			Increment: config.DelayIncrement,
			MaxDelay:  config.MaxDelay,
		}
	case RetryStrategyJittered:
		baseCalculator := &ExponentialDelayCalculator{
			Multiplier: config.BackoffMultiplier,
			MaxDelay:   config.MaxDelay,
		}
		decorator.delayCalculator = &JitteredDelayCalculator{
			BaseCalculator: baseCalculator,
			JitterFactor:   config.JitterFactor,
		}
	default:
		decorator.delayCalculator = &FixedDelayCalculator{}
	}
	
	return decorator
}

// Connect connects with retry logic
func (d *RetryDecorator) Connect(ctx context.Context, config types.DatabaseConfig) error {
	_, err := d.executeWithRetry(ctx, "CONNECT", func() (interface{}, error) {
		return nil, d.wrapped.Connect(ctx, config)
	})
	return err
}

// Disconnect disconnects (no retry needed)
func (d *RetryDecorator) Disconnect(ctx context.Context) error {
	return d.wrapped.Disconnect(ctx)
}

// Ping pings with retry logic
func (d *RetryDecorator) Ping(ctx context.Context) error {
	_, err := d.executeWithRetry(ctx, "PING", func() (interface{}, error) {
		return nil, d.wrapped.Ping(ctx)
	})
	return err
}

// Execute executes with retry logic
func (d *RetryDecorator) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	result, err := d.executeWithRetry(ctx, "EXECUTE", func() (interface{}, error) {
		return d.wrapped.Execute(ctx, query, args...)
	})
	if err != nil {
		return nil, err
	}
	return result.(backend.QueryResult), nil
}

// Query queries with retry logic
func (d *RetryDecorator) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	result, err := d.executeWithRetry(ctx, "QUERY", func() (interface{}, error) {
		return d.wrapped.Query(ctx, query, args...)
	})
	if err != nil {
		return nil, err
	}
	return result.(backend.QueryRows), nil
}

// QueryRow queries single row with retry logic
func (d *RetryDecorator) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	result, err := d.executeWithRetry(ctx, "QUERY_ROW", func() (interface{}, error) {
		return d.wrapped.QueryRow(ctx, query, args...), nil
	})
	if err != nil {
		return &ErrorQueryRow{err: err}
	}
	return result.(backend.QueryRow)
}

// BeginTransaction begins transaction with retry logic
func (d *RetryDecorator) BeginTransaction(ctx context.Context) (backend.Transaction, error) {
	result, err := d.executeWithRetry(ctx, "BEGIN_TRANSACTION", func() (interface{}, error) {
		return d.wrapped.BeginTransaction(ctx)
	})
	if err != nil {
		return nil, err
	}
	
	txn := result.(backend.Transaction)
	return &RetryTransaction{
		Transaction: txn,
		decorator:   d,
		attempts:    make([]RetryAttempt, 0),
	}, nil
}

// CreateTable creates table with retry logic
func (d *RetryDecorator) CreateTable(ctx context.Context, tableDef backend.TableDefinition) error {
	_, err := d.executeWithRetry(ctx, "CREATE_TABLE", func() (interface{}, error) {
		return nil, d.wrapped.CreateTable(ctx, tableDef)
	})
	return err
}

// DropTable drops table with retry logic
func (d *RetryDecorator) DropTable(ctx context.Context, tableName string) error {
	_, err := d.executeWithRetry(ctx, "DROP_TABLE", func() (interface{}, error) {
		return nil, d.wrapped.DropTable(ctx, tableName)
	})
	return err
}

// TableExists checks table existence with retry logic
func (d *RetryDecorator) TableExists(ctx context.Context, tableName string) (bool, error) {
	result, err := d.executeWithRetry(ctx, "TABLE_EXISTS", func() (interface{}, error) {
		return d.wrapped.TableExists(ctx, tableName)
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
}

// CreateIndex creates index with retry logic
func (d *RetryDecorator) CreateIndex(ctx context.Context, indexDef backend.IndexDefinition) error {
	_, err := d.executeWithRetry(ctx, "CREATE_INDEX", func() (interface{}, error) {
		return nil, d.wrapped.CreateIndex(ctx, indexDef)
	})
	return err
}

// DropIndex drops index with retry logic
func (d *RetryDecorator) DropIndex(ctx context.Context, indexName string) error {
	_, err := d.executeWithRetry(ctx, "DROP_INDEX", func() (interface{}, error) {
		return nil, d.wrapped.DropIndex(ctx, indexName)
	})
	return err
}

// executeWithRetry executes an operation with retry logic
func (d *RetryDecorator) executeWithRetry(ctx context.Context, operationType string, operation func() (interface{}, error)) (interface{}, error) {
	var attempts []RetryAttempt
	var lastErr error
	startTime := time.Now()
	
	for attempt := 1; attempt <= d.config.MaxAttempts; attempt++ {
		attemptStart := time.Now()
		
		// Calculate delay for this attempt (except first)
		var delayBefore time.Duration
		if attempt > 1 {
			delayBefore = d.delayCalculator.CalculateDelay(attempt-1, d.config.BaseDelay)
			
			// Apply error-specific delay if available
			if retryableErr, ok := lastErr.(RetryableError); ok {
				if retryAfter := retryableErr.GetRetryAfter(); retryAfter > 0 {
					delayBefore = retryAfter
				}
			}
			
			// Respect context timeout
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delayBefore):
			}
		}
		
		// Execute the operation
		result, err := operation()
		attemptDuration := time.Since(attemptStart)
		
		attemptRecord := RetryAttempt{
			AttemptNumber: attempt,
			StartTime:     attemptStart,
			Duration:      attemptDuration,
			Success:       err == nil,
			DelayBefore:   delayBefore,
		}
		
		if err != nil {
			attemptRecord.Error = err.Error()
			lastErr = err
			
			// Check if we should retry
			if !d.shouldRetry(err, attempt) {
				attempts = append(attempts, attemptRecord)
				break
			}
			
			// Check context timeout
			if ctx.Err() != nil {
				attempts = append(attempts, attemptRecord)
				lastErr = ctx.Err()
				break
			}
		} else {
			// Success
			attempts = append(attempts, attemptRecord)
			
			// Record the retry result
			d.recordRetryResult(RetryResult{
				TotalAttempts: attempt,
				TotalDuration: time.Since(startTime),
				Success:       true,
				Attempts:      attempts,
				OperationType: operationType,
			})
			
			return result, nil
		}
		
		attempts = append(attempts, attemptRecord)
	}
	
	// All attempts failed
	d.recordRetryResult(RetryResult{
		TotalAttempts: len(attempts),
		TotalDuration: time.Since(startTime),
		Success:       false,
		FinalError:    lastErr.Error(),
		Attempts:      attempts,
		OperationType: operationType,
	})
	
	return nil, fmt.Errorf("operation failed after %d attempts: %w", len(attempts), lastErr)
}

// shouldRetry determines if an operation should be retried
func (d *RetryDecorator) shouldRetry(err error, attempt int) bool {
	// Check max attempts
	if attempt >= d.config.MaxAttempts {
		return false
	}
	
	// Check if error is retryable
	if !d.classifier.IsRetryable(err) {
		return false
	}
	
	// Check if classifier says to stop
	if d.classifier.ShouldStopRetrying(err, attempt) {
		return false
	}
	
	return true
}

// recordRetryResult records a retry result
func (d *RetryDecorator) recordRetryResult(result RetryResult) {
	d.results = append(d.results, result)
	
	// Keep only recent results to prevent memory growth
	if len(d.results) > d.config.MaxResultHistory {
		d.results = d.results[len(d.results)-d.config.MaxResultHistory:]
	}
}

// GetRetryResults returns the retry results
func (d *RetryDecorator) GetRetryResults() []RetryResult {
	return d.results
}

// GetRetryStats returns retry statistics
func (d *RetryDecorator) GetRetryStats() map[string]interface{} {
	if len(d.results) == 0 {
		return map[string]interface{}{
			"total_operations": 0,
			"successful_operations": 0,
			"failed_operations": 0,
			"total_attempts": 0,
			"average_attempts": 0.0,
		}
	}
	
	totalOps := len(d.results)
	successfulOps := 0
	totalAttempts := 0
	
	for _, result := range d.results {
		if result.Success {
			successfulOps++
		}
		totalAttempts += result.TotalAttempts
	}
	
	return map[string]interface{}{
		"total_operations": totalOps,
		"successful_operations": successfulOps,
		"failed_operations": totalOps - successfulOps,
		"total_attempts": totalAttempts,
		"average_attempts": float64(totalAttempts) / float64(totalOps),
		"success_rate": float64(successfulOps) / float64(totalOps),
	}
}

// RetryDecoratorFactory creates retry decorators
type RetryDecoratorFactory struct{}

// CreateDecorator creates a retry decorator from configuration
func (f *RetryDecoratorFactory) CreateDecorator(config DecoratorConfig) (BackendDecorator, error) {
	retryConfig := ParseRetryConfig(config.Options)
	
	return &RetryDecorator{
		BaseBackendDecorator: &BaseBackendDecorator{},
		config:               retryConfig,
		classifier:           NewDefaultErrorClassifier(),
		results:              make([]RetryResult, 0),
	}, nil
}

// SupportedTypes returns the supported decorator types
func (f *RetryDecoratorFactory) SupportedTypes() []DecoratorType {
	return []DecoratorType{DecoratorTypeRetry}
}

// Register the retry decorator factory
func init() {
	GlobalDecoratorRegistry.RegisterFactory(DecoratorTypeRetry, &RetryDecoratorFactory{})
}

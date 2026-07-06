/**
 * @file handler.go
 * @description Comprehensive error handling and rollback strategy with proper error propagation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/rollback/handler.go
 */

package rollback

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrorType represents the type of error
type ErrorType string

const (
	ErrorTypeValidation    ErrorType = "validation"
	ErrorTypeExecution     ErrorType = "execution"
	ErrorTypeRollback      ErrorType = "rollback"
	ErrorTypeConfiguration ErrorType = "configuration"
	ErrorTypeConnectivity  ErrorType = "connectivity"
	ErrorTypeTimeout       ErrorType = "timeout"
	ErrorTypeIntegrity     ErrorType = "integrity"
	ErrorTypePermission    ErrorType = "permission"
	ErrorTypeResource      ErrorType = "resource"
	ErrorTypeUnknown       ErrorType = "unknown"
	ErrorTypeWildcard      ErrorType = "*"
)

// String returns the string representation of ErrorType
func (et ErrorType) String() string {
	return string(et)
}

// ErrorSeverity represents the severity of an error
type ErrorSeverity string

const (
	ErrorSeverityLow      ErrorSeverity = "low"
	ErrorSeverityMedium   ErrorSeverity = "medium"
	ErrorSeverityHigh     ErrorSeverity = "high"
	ErrorSeverityCritical ErrorSeverity = "critical"
)

// String returns the string representation of ErrorSeverity
func (es ErrorSeverity) String() string {
	return string(es)
}

// RecoveryAction represents the action to take for error recovery
type RecoveryAction string

const (
	RecoveryActionRetry    RecoveryAction = "retry"
	RecoveryActionRollback RecoveryAction = "rollback"
	RecoveryActionSkip     RecoveryAction = "skip"
	RecoveryActionAbort    RecoveryAction = "abort"
	RecoveryActionManual   RecoveryAction = "manual"
)

// String returns the string representation of RecoveryAction
func (ra RecoveryAction) String() string {
	return string(ra)
}

// MigrationError represents a migration-specific error with context
type MigrationError struct {
	Type         ErrorType                `json:"type"`
	Severity     ErrorSeverity            `json:"severity"`
	Message      string                   `json:"message"`
	Cause        error                    `json:"cause,omitempty"`
	Context      map[string]interface{}   `json:"context,omitempty"`
	Timestamp    time.Time                `json:"timestamp"`
	StackTrace   string                   `json:"stack_trace,omitempty"`
	MigrationID  string                   `json:"migration_id,omitempty"`
	Step         string                   `json:"step,omitempty"`
	Recovery     RecoveryAction           `json:"recovery"`
	Retryable    bool                     `json:"retryable"`
	RetryCount   int                      `json:"retry_count"`
	MaxRetries   int                      `json:"max_retries"`
}

func (e *MigrationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Type, e.Severity, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Type, e.Severity, e.Message)
}

func (e *MigrationError) Unwrap() error {
	return e.Cause
}

func (e *MigrationError) Is(target error) bool {
	if migErr, ok := target.(*MigrationError); ok {
		return e.Type == migErr.Type && e.Message == migErr.Message
	}
	return false
}

// NewMigrationError creates a new migration error
func NewMigrationError(errorType ErrorType, severity ErrorSeverity, message string, cause error) *MigrationError {
	err := &MigrationError{
		Type:      errorType,
		Severity:  severity,
		Message:   message,
		Cause:     cause,
		Context:   make(map[string]interface{}),
		Timestamp: time.Now(),
		Retryable: false,
		Recovery:  RecoveryActionAbort,
	}

	// Capture stack trace
	err.StackTrace = captureStackTrace()

	// Set default recovery actions based on error type
	err.setDefaultRecoveryAction()

	return err
}

func (e *MigrationError) setDefaultRecoveryAction() {
	switch e.Type {
	case ErrorTypeValidation:
		e.Recovery = RecoveryActionAbort
		e.Retryable = false
	case ErrorTypeExecution:
		e.Recovery = RecoveryActionRollback
		e.Retryable = true
		e.MaxRetries = 3
	case ErrorTypeConnectivity:
		e.Recovery = RecoveryActionRetry
		e.Retryable = true
		e.MaxRetries = 5
	case ErrorTypeTimeout:
		e.Recovery = RecoveryActionRetry
		e.Retryable = true
		e.MaxRetries = 3
	case ErrorTypeResource:
		e.Recovery = RecoveryActionRetry
		e.Retryable = true
		e.MaxRetries = 2
	default:
		e.Recovery = RecoveryActionAbort
		e.Retryable = false
	}
}

func (e *MigrationError) WithContext(key string, value interface{}) *MigrationError {
	e.Context[key] = value
	return e
}

func (e *MigrationError) WithMigrationID(id string) *MigrationError {
	e.MigrationID = id
	return e
}

func (e *MigrationError) WithStep(step string) *MigrationError {
	e.Step = step
	return e
}

func (e *MigrationError) WithRecovery(action RecoveryAction) *MigrationError {
	e.Recovery = action
	return e
}

func (e *MigrationError) WithRetryable(retryable bool, maxRetries int) *MigrationError {
	e.Retryable = retryable
	e.MaxRetries = maxRetries
	return e
}

func (e *MigrationError) CanRetry() bool {
	return e.Retryable && e.RetryCount < e.MaxRetries
}

func (e *MigrationError) IncrementRetry() {
	e.RetryCount++
}

func captureStackTrace() string {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])

	var trace strings.Builder
	for {
		frame, more := frames.Next()
		trace.WriteString(fmt.Sprintf("%s:%d %s\n", frame.File, frame.Line, frame.Function))
		if !more {
			break
		}
	}
	return trace.String()
}

// ErrorHandler handles migration errors and determines recovery actions
type ErrorHandler interface {
	HandleError(ctx context.Context, err error) (*ErrorHandlingResult, error)
	RegisterErrorRule(rule *ErrorRule)
	GetRecoveryAction(err error) RecoveryAction
}

// ErrorHandlingResult contains the result of error handling
type ErrorHandlingResult struct {
	Action       RecoveryAction           `json:"action"`
	ShouldRetry  bool                     `json:"should_retry"`
	RetryDelay   time.Duration            `json:"retry_delay"`
	Message      string                   `json:"message"`
	Context      map[string]interface{}   `json:"context"`
}

// ErrorRule defines how to handle specific types of errors
type ErrorRule struct {
	ErrorType    ErrorType      `json:"error_type"`
	Severity     ErrorSeverity  `json:"severity"`
	Pattern      string         `json:"pattern"`
	Action       RecoveryAction `json:"action"`
	MaxRetries   int            `json:"max_retries"`
	RetryDelay   time.Duration  `json:"retry_delay"`
	Condition    func(error) bool `json:"-"`
}

// Matches checks if the rule matches the given error
func (r *ErrorRule) Matches(err error) bool {
	if migErr, ok := err.(*MigrationError); ok {
		if r.ErrorType != ErrorTypeWildcard && r.ErrorType != migErr.Type {
			return false
		}
		if r.Severity != "" && r.Severity != migErr.Severity {
			return false
		}
		if r.Condition != nil {
			return r.Condition(err)
		}
		return true
	}
	return false
}

// DefaultErrorHandler implements the ErrorHandler interface
type DefaultErrorHandler struct {
	rules []ErrorRule
}

func NewDefaultErrorHandler() *DefaultErrorHandler {
	handler := &DefaultErrorHandler{
		rules: make([]ErrorRule, 0),
	}

	// Register default error rules
	handler.registerDefaultRules()

	return handler
}

func (h *DefaultErrorHandler) registerDefaultRules() {
	// Connectivity errors - retry with backoff
	h.RegisterErrorRule(&ErrorRule{
		ErrorType:  ErrorTypeConnectivity,
		Action:     RecoveryActionRetry,
		MaxRetries: 5,
		RetryDelay: 2 * time.Second,
		Condition: func(err error) bool {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "connection") ||
				   strings.Contains(msg, "network") ||
				   strings.Contains(msg, "timeout")
		},
	})

	// Validation errors - abort immediately
	h.RegisterErrorRule(&ErrorRule{
		ErrorType:  ErrorTypeValidation,
		Action:     RecoveryActionAbort,
		MaxRetries: 0,
		Condition: func(err error) bool {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "validation") ||
				   strings.Contains(msg, "invalid") ||
				   strings.Contains(msg, "constraint")
		},
	})

	// Execution errors - rollback and optionally retry
	h.RegisterErrorRule(&ErrorRule{
		ErrorType:  ErrorTypeExecution,
		Action:     RecoveryActionRollback,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
		Condition: func(err error) bool {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "execution") ||
				   strings.Contains(msg, "sql") ||
				   strings.Contains(msg, "syntax")
		},
	})

	// Resource errors - retry with longer delay
	h.RegisterErrorRule(&ErrorRule{
		ErrorType:  ErrorTypeResource,
		Action:     RecoveryActionRetry,
		MaxRetries: 3,
		RetryDelay: 5 * time.Second,
		Condition: func(err error) bool {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "resource") ||
				   strings.Contains(msg, "memory") ||
				   strings.Contains(msg, "disk") ||
				   strings.Contains(msg, "lock")
		},
	})

	// Permission errors - abort (manual intervention needed)
	h.RegisterErrorRule(&ErrorRule{
		ErrorType:  ErrorTypePermission,
		Action:     RecoveryActionManual,
		MaxRetries: 0,
		Condition: func(err error) bool {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "permission") ||
				   strings.Contains(msg, "access") ||
				   strings.Contains(msg, "denied") ||
				   strings.Contains(msg, "unauthorized")
		},
	})
}

func (h *DefaultErrorHandler) RegisterErrorRule(rule *ErrorRule) {
	// Prepend custom rules so they take precedence over default rules
	h.rules = append([]ErrorRule{*rule}, h.rules...)
}

func (h *DefaultErrorHandler) HandleError(ctx context.Context, err error) (*ErrorHandlingResult, error) {
	// Convert to MigrationError if not already
	var migErr *MigrationError
	if me, ok := err.(*MigrationError); ok {
		migErr = me
	} else {
		migErr = h.classifyError(err)
	}

	// Find matching rule
	rule := h.findMatchingRule(migErr)
	if rule == nil {
		// Default handling
		return &ErrorHandlingResult{
			Action:      RecoveryActionAbort,
			ShouldRetry: false,
			Message:     "No matching error rule found",
			Context:     make(map[string]interface{}),
		}, nil
	}

	// Apply rule
	result := &ErrorHandlingResult{
		Action:     rule.Action,
		RetryDelay: rule.RetryDelay,
		Context:    make(map[string]interface{}),
	}

	// Determine if should retry
	if rule.Action == RecoveryActionRetry && migErr.CanRetry() {
		result.ShouldRetry = true
		result.Message = fmt.Sprintf("Retrying operation (attempt %d/%d)", migErr.RetryCount+1, migErr.MaxRetries)
		migErr.IncrementRetry()
	} else {
		result.ShouldRetry = false
		result.Message = fmt.Sprintf("Applying recovery action: %s", rule.Action)
	}

	result.Context["error_type"] = migErr.Type
	result.Context["severity"] = migErr.Severity
	result.Context["migration_id"] = migErr.MigrationID
	result.Context["step"] = migErr.Step

	return result, nil
}

func (h *DefaultErrorHandler) GetRecoveryAction(err error) RecoveryAction {
	if migErr, ok := err.(*MigrationError); ok {
		return migErr.Recovery
	}

	rule := h.findMatchingRule(h.classifyError(err))
	if rule != nil {
		return rule.Action
	}

	return RecoveryActionAbort
}

func (h *DefaultErrorHandler) findMatchingRule(err *MigrationError) *ErrorRule {
	for _, rule := range h.rules {
		if rule.ErrorType == err.Type {
			if rule.Condition != nil && rule.Condition(err) {
				return &rule
			}
			if rule.Condition == nil {
				return &rule
			}
		}
	}
	return nil
}

func (h *DefaultErrorHandler) classifyError(err error) *MigrationError {
	msg := strings.ToLower(err.Error())

	// Classify based on error message patterns
	switch {
	case strings.Contains(msg, "validation") || strings.Contains(msg, "invalid"):
		return NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, err.Error(), err)
	case strings.Contains(msg, "connection") || strings.Contains(msg, "network"):
		return NewMigrationError(ErrorTypeConnectivity, ErrorSeverityMedium, err.Error(), err)
	case strings.Contains(msg, "timeout"):
		return NewMigrationError(ErrorTypeTimeout, ErrorSeverityMedium, err.Error(), err)
	case strings.Contains(msg, "permission") || strings.Contains(msg, "access"):
		return NewMigrationError(ErrorTypePermission, ErrorSeverityHigh, err.Error(), err)
	case strings.Contains(msg, "resource") || strings.Contains(msg, "memory"):
		return NewMigrationError(ErrorTypeResource, ErrorSeverityMedium, err.Error(), err)
	case strings.Contains(msg, "sql") || strings.Contains(msg, "syntax"):
		return NewMigrationError(ErrorTypeExecution, ErrorSeverityHigh, err.Error(), err)
	default:
		return NewMigrationError(ErrorTypeUnknown, ErrorSeverityMedium, err.Error(), err)
	}
}

// RollbackStrategy defines how to handle rollbacks
type RollbackStrategy interface {
	ShouldRollback(ctx context.Context, err error) bool
	ExecuteRollback(ctx context.Context, rollbackFunc func(context.Context) error) error
	GetRollbackSteps(ctx context.Context) []RollbackStep
}

// RollbackStep represents a single rollback operation
type RollbackStep struct {
	Name        string                                    `json:"name"`
	Description string                                    `json:"description"`
	Execute     func(context.Context) error               `json:"-"`
	Validate    func(context.Context) error               `json:"-"`
	Priority    int                                       `json:"priority"`
	Required    bool                                      `json:"required"`
}

// DefaultRollbackStrategy implements the RollbackStrategy interface
type DefaultRollbackStrategy struct {
	steps []RollbackStep
}

func NewDefaultRollbackStrategy() *DefaultRollbackStrategy {
	return &DefaultRollbackStrategy{
		steps: make([]RollbackStep, 0),
	}
}

func (rs *DefaultRollbackStrategy) ShouldRollback(ctx context.Context, err error) bool {
	if migErr, ok := err.(*MigrationError); ok {
		return migErr.Recovery == RecoveryActionRollback ||
			   (migErr.Severity == ErrorSeverityHigh || migErr.Severity == ErrorSeverityCritical)
	}

	// Default: rollback on execution errors
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "execution") ||
		   strings.Contains(msg, "sql") ||
		   strings.Contains(msg, "constraint")
}

func (rs *DefaultRollbackStrategy) ExecuteRollback(ctx context.Context, rollbackFunc func(context.Context) error) error {
	// Execute the provided rollback function
	if rollbackFunc != nil {
		if err := rollbackFunc(ctx); err != nil {
			return NewMigrationError(ErrorTypeRollback, ErrorSeverityCritical, 
				"Failed to execute rollback", err)
		}
	}

	// Execute additional rollback steps
	steps := rs.GetRollbackSteps(ctx)
	for _, step := range steps {
		if step.Execute != nil {
			if err := step.Execute(ctx); err != nil {
				if step.Required {
					return NewMigrationError(ErrorTypeRollback, ErrorSeverityCritical,
						fmt.Sprintf("Required rollback step '%s' failed", step.Name), err)
				}
				// Log non-required step failures but continue
				continue
			}

			// Validate step if validator is provided
			if step.Validate != nil {
				if err := step.Validate(ctx); err != nil {
					return NewMigrationError(ErrorTypeRollback, ErrorSeverityHigh,
						fmt.Sprintf("Rollback step '%s' validation failed", step.Name), err)
				}
			}
		}
	}

	return nil
}

func (rs *DefaultRollbackStrategy) GetRollbackSteps(ctx context.Context) []RollbackStep {
	// Sort steps by priority (higher priority first)
	steps := make([]RollbackStep, len(rs.steps))
	copy(steps, rs.steps)

	for i := 0; i < len(steps)-1; i++ {
		for j := i + 1; j < len(steps); j++ {
			if steps[i].Priority < steps[j].Priority {
				steps[i], steps[j] = steps[j], steps[i]
			}
		}
	}

	return steps
}

func (rs *DefaultRollbackStrategy) AddRollbackStep(step RollbackStep) {
	rs.steps = append(rs.steps, step)
}

// AddStep is a convenience method for adding a simple rollback step
func (rs *DefaultRollbackStrategy) AddStep(name string, execute func(context.Context) error) {
	step := RollbackStep{
		Name:        name,
		Description: name,
		Execute:     execute,
		Priority:    0,
		Required:    true,
	}
	rs.AddRollbackStep(step)
}

// Execute executes all rollback steps in reverse order
func (rs *DefaultRollbackStrategy) Execute(ctx context.Context) error {
	var errors []error
	
	// Execute steps in reverse order (LIFO)
	for i := len(rs.steps) - 1; i >= 0; i-- {
		step := rs.steps[i]
		if step.Execute != nil {
			if err := step.Execute(ctx); err != nil {
				if step.Required {
					errors = append(errors, NewMigrationError(ErrorTypeRollback, ErrorSeverityCritical,
						fmt.Sprintf("Required rollback step '%s' failed", step.Name), err))
				} else {
					errors = append(errors, NewMigrationError(ErrorTypeRollback, ErrorSeverityMedium,
						fmt.Sprintf("Rollback step '%s' failed", step.Name), err))
				}
				// Continue executing remaining steps
				continue
			}

			// Validate step if validator is provided
			if step.Validate != nil {
				if err := step.Validate(ctx); err != nil {
					errors = append(errors, NewMigrationError(ErrorTypeRollback, ErrorSeverityHigh,
						fmt.Sprintf("Rollback step '%s' validation failed", step.Name), err))
				}
			}
		}
	}
	
	// Return the first error if any occurred
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

// ErrorCollector collects and aggregates errors during migration
type ErrorCollector struct {
	errors []error
	fatal  bool
	mu     sync.RWMutex
}

func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: make([]error, 0),
		fatal:  false,
	}
}

func (ec *ErrorCollector) Add(err error) {
	if err != nil {
		ec.mu.Lock()
		defer ec.mu.Unlock()
		
		ec.errors = append(ec.errors, err)
		
		// Check if this is a fatal error
		if migErr, ok := err.(*MigrationError); ok {
			if migErr.Severity == ErrorSeverityCritical {
				ec.fatal = true
			}
		}
	}
}

func (ec *ErrorCollector) HasErrors() bool {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	return len(ec.errors) > 0
}

func (ec *ErrorCollector) HasFatalErrors() bool {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	return ec.fatal
}

func (ec *ErrorCollector) GetErrors() []error {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	// Return a copy to prevent external modification
	result := make([]error, len(ec.errors))
	copy(result, ec.errors)
	return result
}

func (ec *ErrorCollector) GetFirstError() error {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	if len(ec.errors) > 0 {
		return ec.errors[0]
	}
	return nil
}

func (ec *ErrorCollector) Count() int {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	return len(ec.errors)
}

func (ec *ErrorCollector) GetErrorsByType(errorType ErrorType) []error {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	var result []error
	for _, err := range ec.errors {
		if migErr, ok := err.(*MigrationError); ok {
			if migErr.Type == errorType {
				result = append(result, err)
			}
		}
	}
	return result
}

func (ec *ErrorCollector) GetErrorsBySeverity(severity ErrorSeverity) []error {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	var result []error
	for _, err := range ec.errors {
		if migErr, ok := err.(*MigrationError); ok {
			if migErr.Severity == severity {
				result = append(result, err)
			}
		}
	}
	return result
}

func (ec *ErrorCollector) Clear() {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.errors = make([]error, 0)
	ec.fatal = false
}

func (ec *ErrorCollector) Error() string {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	
	if len(ec.errors) == 0 {
		return ""
	}
	
	if len(ec.errors) == 1 {
		return ec.errors[0].Error()
	}
	
	var msgs []string
	for _, err := range ec.errors {
		msgs = append(msgs, err.Error())
	}
	
	return fmt.Sprintf("Multiple errors occurred: %s", strings.Join(msgs, "; "))
}

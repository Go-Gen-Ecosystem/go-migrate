/**
 * @file errors.go
 * @description Error definitions for the migration package
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/errors.go
 */

package errors

import (
	"errors"
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// Import common migration errors from types package
var (
	ErrMigrationNotFound     = types.ErrMigrationNotFound
	ErrMigrationExists       = types.ErrMigrationExists
	ErrInvalidMigration      = types.ErrInvalidMigration
	ErrMigrationLocked       = types.ErrMigrationLocked
	ErrDatabaseNotSupported  = types.ErrDatabaseNotSupported
	ErrInvalidConfiguration  = types.ErrInvalidConfiguration
	ErrMigrationFailed       = types.ErrMigrationFailed
	ErrRollbackFailed        = types.ErrRollbackFailed
	ErrValidationFailed      = types.ErrValidationFailed
	ErrChecksumMismatch      = types.ErrChecksumMismatch
	ErrCircularDependency    = types.ErrCircularDependency
	ErrPreconditionFailed    = types.ErrPreconditionFailed
	ErrBackupFailed          = types.ErrBackupFailed
	ErrConnectionFailed      = types.ErrConnectionFailed
	ErrTimeout               = types.ErrTimeout
)

// ErrorCode represents a standardized error code
type ErrorCode string

const (
	// Syntax and validation errors
	ErrCodeSyntaxError      ErrorCode = "SYNTAX_ERROR"
	ErrCodeValidationError  ErrorCode = "VALIDATION_ERROR"
	ErrCodeChecksumMismatch ErrorCode = "CHECKSUM_MISMATCH"

	// Lock errors
	ErrCodeLockAcquisition ErrorCode = "LOCK_ACQUISITION_FAILED"
	ErrCodeLockRelease     ErrorCode = "LOCK_RELEASE_FAILED"
	ErrCodeLockTimeout     ErrorCode = "LOCK_TIMEOUT"

	// Execution errors
	ErrCodeExecutionFailed ErrorCode = "EXECUTION_FAILED"
	ErrCodeRollbackFailed  ErrorCode = "ROLLBACK_FAILED"
	ErrCodeTimeout         ErrorCode = "TIMEOUT"

	// Database errors
	ErrCodeConnectionFailed ErrorCode = "CONNECTION_FAILED"
	ErrCodeQueryFailed      ErrorCode = "QUERY_FAILED"
	ErrCodeTransactionFailed ErrorCode = "TRANSACTION_FAILED"

	// Dependency errors
	ErrCodeDependencyMissing  ErrorCode = "DEPENDENCY_MISSING"
	ErrCodeCircularDependency ErrorCode = "CIRCULAR_DEPENDENCY"

	// Configuration errors
	ErrCodeInvalidConfig      ErrorCode = "INVALID_CONFIGURATION"
	ErrCodeMissingConfig      ErrorCode = "MISSING_CONFIGURATION"
	ErrCodeUnsupportedDatabase ErrorCode = "UNSUPPORTED_DATABASE"
)

// MigrationError represents a migration-specific error with enhanced context
type MigrationError struct {
	Code        ErrorCode              `json:"code"`
	Type        string                 `json:"type"`
	Message     string                 `json:"message"`
	MigrationID string                 `json:"migration_id,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Cause       error                  `json:"cause,omitempty"`
	Retryable   bool                   `json:"retryable"`
	Suggestion  string                 `json:"suggestion,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

func (e *MigrationError) Error() string {
	if e.MigrationID != "" {
		return fmt.Sprintf("[%s] migration %s: %s", e.Code, e.MigrationID, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *MigrationError) Unwrap() error {
	return e.Cause
}

// IsRetryable returns whether this error is retryable
func (e *MigrationError) IsRetryable() bool {
	return e.Retryable
}

// GetSuggestion returns a suggestion for fixing the error
func (e *MigrationError) GetSuggestion() string {
	return e.Suggestion
}

// NewMigrationError creates a new migration error
func NewMigrationError(errorType, message string, migrationID string, cause error) *MigrationError {
	return &MigrationError{
		Code:        ErrCodeExecutionFailed,
		Type:        errorType,
		Message:     message,
		MigrationID: migrationID,
		Cause:       cause,
		Retryable:   false,
		Context:     make(map[string]interface{}),
	}
}

// NewSyntaxError creates a syntax error with suggestion
func NewSyntaxError(migrationID string, err error) *MigrationError {
	return &MigrationError{
		Code:        ErrCodeSyntaxError,
		Type:        "syntax",
		Message:     "SQL syntax error detected",
		MigrationID: migrationID,
		Cause:       err,
		Retryable:   false,
		Suggestion:  "Review the SQL syntax and ensure it's valid for your database",
		Context:     make(map[string]interface{}),
	}
}

// NewLockError creates a lock acquisition error
func NewLockError(migrationID string, err error) *MigrationError {
	return &MigrationError{
		Code:        ErrCodeLockAcquisition,
		Type:        "lock",
		Message:     "Failed to acquire migration lock",
		MigrationID: migrationID,
		Cause:       err,
		Retryable:   true,
		Suggestion:  "Another migration may be running. Wait for it to complete or manually release the lock",
		Context:     make(map[string]interface{}),
	}
}

// NewConnectionError creates a database connection error
func NewConnectionError(driver string, err error) *MigrationError {
	return &MigrationError{
		Code:      ErrCodeConnectionFailed,
		Type:      "connection",
		Message:   fmt.Sprintf("Failed to connect to %s database", driver),
		Cause:     err,
		Retryable: true,
		Suggestion: "Check database connection string, credentials, and network connectivity",
		Context: map[string]interface{}{
			"driver": driver,
		},
	}
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(migrationID string, operation string, timeout string) *MigrationError {
	return &MigrationError{
		Code:        ErrCodeTimeout,
		Type:        "timeout",
		Message:     fmt.Sprintf("Operation timed out after %s", timeout),
		MigrationID: migrationID,
		Retryable:   true,
		Suggestion:  "Increase timeout setting or optimize the migration",
		Context: map[string]interface{}{
			"operation": operation,
			"timeout":   timeout,
		},
	}
}

// NewDependencyError creates a dependency error
func NewDependencyError(migrationID string, missingDep string) *MigrationError {
	return &MigrationError{
		Code:        ErrCodeDependencyMissing,
		Type:        "dependency",
		Message:     fmt.Sprintf("Missing dependency: %s", missingDep),
		MigrationID: migrationID,
		Retryable:   false,
		Suggestion:  "Ensure all dependent migrations exist and are applied in the correct order",
		Context: map[string]interface{}{
			"missing_dependency": missingDep,
		},
	}
}

// NewCircularDependencyError creates a circular dependency error
func NewCircularDependencyError(cycle []string) *MigrationError {
	return &MigrationError{
		Code:       ErrCodeCircularDependency,
		Type:       "dependency",
		Message:    fmt.Sprintf("Circular dependency detected: %v", cycle),
		Retryable:  false,
		Suggestion: "Remove circular dependencies from your migrations",
		Context: map[string]interface{}{
			"cycle": cycle,
		},
	}
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// NewValidationError creates a new validation error
func NewValidationError(field, value, message string, cause error) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
		Cause:   cause,
	}
}

// ConfigurationError represents a configuration error
type ConfigurationError struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *ConfigurationError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("configuration error in section '%s', key '%s': %s", e.Section, e.Key, e.Message)
	}
	return fmt.Sprintf("configuration error in section '%s': %s", e.Section, e.Message)
}

func (e *ConfigurationError) Unwrap() error {
	return e.Cause
}

// NewConfigurationError creates a new configuration error
func NewConfigurationError(section, key, value, message string, cause error) *ConfigurationError {
	return &ConfigurationError{
		Section: section,
		Key:     key,
		Value:   value,
		Message: message,
		Cause:   cause,
	}
}

// DatabaseError represents a database-specific error
type DatabaseError struct {
	Driver    string `json:"driver"`
	Operation string `json:"operation"`
	Query     string `json:"query,omitempty"`
	Message   string `json:"message"`
	Cause     error  `json:"cause,omitempty"`
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("database error (%s) during %s: %s", e.Driver, e.Operation, e.Message)
}

func (e *DatabaseError) Unwrap() error {
	return e.Cause
}

// NewDatabaseError creates a new database error
func NewDatabaseError(driver, operation, query, message string, cause error) *DatabaseError {
	return &DatabaseError{
		Driver:    driver,
		Operation: operation,
		Query:     query,
		Message:   message,
		Cause:     cause,
	}
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a MigrationError with retryable flag
	var migErr *MigrationError
	if errors.As(err, &migErr) {
		return migErr.Retryable
	}

	// Check for specific retryable errors
	switch {
	case errors.Is(err, ErrConnectionFailed):
		return true
	case errors.Is(err, ErrTimeout):
		return true
	case errors.Is(err, ErrMigrationLocked):
		return true
	default:
		return false
	}
}

// IsFatalError checks if an error is fatal and should stop migration
func IsFatalError(err error) bool {
	if err == nil {
		return false
	}
	
	// Check for specific fatal errors
	switch {
	case errors.Is(err, ErrInvalidMigration):
		return true
	case errors.Is(err, ErrChecksumMismatch):
		return true
	case errors.Is(err, ErrCircularDependency):
		return true
	case errors.Is(err, ErrDatabaseNotSupported):
		return true
	case errors.Is(err, ErrInvalidConfiguration):
		return true
	default:
		return false
	}
}

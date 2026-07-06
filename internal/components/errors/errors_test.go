/**
 * @file errors_test.go
 * @description Behavior tests for migration error types, constructors, wrapping,
 *              and retry/fatal classification helpers.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/errors/errors_test.go
 */

package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestMigrationError_ErrorFormatting(t *testing.T) {
	t.Run("with migration id", func(t *testing.T) {
		e := &MigrationError{
			Code:        ErrCodeExecutionFailed,
			Message:     "boom",
			MigrationID: "V1__init",
		}
		got := e.Error()
		want := "[EXECUTION_FAILED] migration V1__init: boom"
		if got != want {
			t.Fatalf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("without migration id", func(t *testing.T) {
		e := &MigrationError{
			Code:    ErrCodeSyntaxError,
			Message: "bad sql",
		}
		got := e.Error()
		want := "[SYNTAX_ERROR] bad sql"
		if got != want {
			t.Fatalf("Error() = %q, want %q", got, want)
		}
	})
}

func TestMigrationError_UnwrapAndErrorsIs(t *testing.T) {
	cause := stderrors.New("root cause")
	e := NewMigrationError("execution", "wrapped", "V2", cause)

	if e.Unwrap() != cause {
		t.Fatalf("Unwrap() did not return the underlying cause")
	}
	if !stderrors.Is(e, cause) {
		t.Fatalf("errors.Is failed to match the wrapped cause")
	}

	// Verify errors.As extracts the concrete type through a wrapping chain.
	chained := fmt.Errorf("outer: %w", e)
	var target *MigrationError
	if !stderrors.As(chained, &target) {
		t.Fatalf("errors.As failed to extract *MigrationError")
	}
	if target.MigrationID != "V2" {
		t.Fatalf("extracted MigrationID = %q, want V2", target.MigrationID)
	}
}

func TestNewMigrationError_Defaults(t *testing.T) {
	e := NewMigrationError("mytype", "msg", "id-1", nil)
	if e.Code != ErrCodeExecutionFailed {
		t.Errorf("Code = %q, want %q", e.Code, ErrCodeExecutionFailed)
	}
	if e.Type != "mytype" {
		t.Errorf("Type = %q, want mytype", e.Type)
	}
	if e.Retryable {
		t.Errorf("expected default Retryable=false")
	}
	if e.Context == nil {
		t.Errorf("expected Context to be initialized")
	}
	if e.IsRetryable() {
		t.Errorf("IsRetryable() should be false")
	}
}

func TestSpecializedConstructors(t *testing.T) {
	cause := stderrors.New("cause")

	t.Run("syntax error", func(t *testing.T) {
		e := NewSyntaxError("V1", cause)
		if e.Code != ErrCodeSyntaxError {
			t.Errorf("Code = %q", e.Code)
		}
		if e.Retryable {
			t.Errorf("syntax errors must not be retryable")
		}
		if e.GetSuggestion() == "" {
			t.Errorf("expected a non-empty suggestion")
		}
		if !stderrors.Is(e, cause) {
			t.Errorf("cause not wrapped")
		}
	})

	t.Run("lock error is retryable", func(t *testing.T) {
		e := NewLockError("V1", cause)
		if e.Code != ErrCodeLockAcquisition {
			t.Errorf("Code = %q", e.Code)
		}
		if !e.IsRetryable() {
			t.Errorf("lock errors must be retryable")
		}
	})

	t.Run("connection error carries driver context", func(t *testing.T) {
		e := NewConnectionError("postgres", cause)
		if e.Code != ErrCodeConnectionFailed {
			t.Errorf("Code = %q", e.Code)
		}
		if !e.Retryable {
			t.Errorf("connection errors must be retryable")
		}
		if !strings.Contains(e.Message, "postgres") {
			t.Errorf("message should mention driver: %q", e.Message)
		}
		if e.Context["driver"] != "postgres" {
			t.Errorf("driver context = %v, want postgres", e.Context["driver"])
		}
	})

	t.Run("timeout error", func(t *testing.T) {
		e := NewTimeoutError("V1", "migrate", "30s")
		if e.Code != ErrCodeTimeout {
			t.Errorf("Code = %q", e.Code)
		}
		if !e.Retryable {
			t.Errorf("timeout errors must be retryable")
		}
		if e.Context["operation"] != "migrate" || e.Context["timeout"] != "30s" {
			t.Errorf("unexpected timeout context: %v", e.Context)
		}
	})

	t.Run("dependency error", func(t *testing.T) {
		e := NewDependencyError("V2", "V1")
		if e.Code != ErrCodeDependencyMissing {
			t.Errorf("Code = %q", e.Code)
		}
		if e.Retryable {
			t.Errorf("dependency errors must not be retryable")
		}
		if e.Context["missing_dependency"] != "V1" {
			t.Errorf("missing_dependency = %v", e.Context["missing_dependency"])
		}
	})

	t.Run("circular dependency error", func(t *testing.T) {
		cycle := []string{"A", "B", "A"}
		e := NewCircularDependencyError(cycle)
		if e.Code != ErrCodeCircularDependency {
			t.Errorf("Code = %q", e.Code)
		}
		got, ok := e.Context["cycle"].([]string)
		if !ok || len(got) != 3 {
			t.Errorf("cycle context = %v", e.Context["cycle"])
		}
		if !strings.Contains(e.Message, "A B A") {
			t.Errorf("message should describe cycle: %q", e.Message)
		}
	})
}

func TestValidationError(t *testing.T) {
	cause := stderrors.New("bad")
	e := NewValidationError("email", "not-an-email", "invalid format", cause)
	if e.Field != "email" || e.Value != "not-an-email" {
		t.Errorf("unexpected fields: %+v", e)
	}
	want := "validation error for field 'email': invalid format"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
	if !stderrors.Is(e, cause) {
		t.Errorf("cause not wrapped")
	}
}

func TestConfigurationError(t *testing.T) {
	t.Run("with key", func(t *testing.T) {
		e := NewConfigurationError("database", "host", "", "required", nil)
		want := "configuration error in section 'database', key 'host': required"
		if e.Error() != want {
			t.Errorf("Error() = %q, want %q", e.Error(), want)
		}
	})
	t.Run("without key", func(t *testing.T) {
		e := NewConfigurationError("database", "", "", "malformed", nil)
		want := "configuration error in section 'database': malformed"
		if e.Error() != want {
			t.Errorf("Error() = %q, want %q", e.Error(), want)
		}
	})
	t.Run("unwrap", func(t *testing.T) {
		cause := stderrors.New("io")
		e := NewConfigurationError("s", "k", "v", "m", cause)
		if !stderrors.Is(e, cause) {
			t.Errorf("cause not wrapped")
		}
	})
}

func TestDatabaseError(t *testing.T) {
	cause := stderrors.New("conn reset")
	e := NewDatabaseError("mysql", "insert", "INSERT INTO x", "failed", cause)
	want := "database error (mysql) during insert: failed"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
	if e.Query != "INSERT INTO x" {
		t.Errorf("Query = %q", e.Query)
	}
	if !stderrors.Is(e, cause) {
		t.Errorf("cause not wrapped")
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"retryable migration error", NewLockError("V1", nil), true},
		{"non-retryable migration error", NewSyntaxError("V1", nil), false},
		{"sentinel connection failed", types.ErrConnectionFailed, true},
		{"sentinel timeout", types.ErrTimeout, true},
		{"sentinel migration locked", types.ErrMigrationLocked, true},
		{"wrapped sentinel timeout", fmt.Errorf("op: %w", types.ErrTimeout), true},
		{"unrelated error", stderrors.New("nope"), false},
		{"non-retryable sentinel", types.ErrChecksumMismatch, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryableError(tt.err); got != tt.want {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsFatalError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"invalid migration", types.ErrInvalidMigration, true},
		{"checksum mismatch", types.ErrChecksumMismatch, true},
		{"circular dependency", types.ErrCircularDependency, true},
		{"db not supported", types.ErrDatabaseNotSupported, true},
		{"invalid configuration", types.ErrInvalidConfiguration, true},
		{"wrapped fatal", fmt.Errorf("ctx: %w", types.ErrInvalidMigration), true},
		{"retryable is not fatal", types.ErrTimeout, false},
		{"unrelated", stderrors.New("nope"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsFatalError(tt.err); got != tt.want {
				t.Errorf("IsFatalError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestSentinelErrorsAliasTypes ensures the re-exported sentinels are the same
// error values as the canonical ones declared in the types package.
func TestSentinelErrorsAliasTypes(t *testing.T) {
	pairs := []struct {
		name  string
		local error
		canon error
	}{
		{"ErrMigrationNotFound", ErrMigrationNotFound, types.ErrMigrationNotFound},
		{"ErrChecksumMismatch", ErrChecksumMismatch, types.ErrChecksumMismatch},
		{"ErrConnectionFailed", ErrConnectionFailed, types.ErrConnectionFailed},
		{"ErrTimeout", ErrTimeout, types.ErrTimeout},
	}
	for _, p := range pairs {
		if p.local != p.canon {
			t.Errorf("%s: local sentinel differs from canonical types sentinel", p.name)
		}
	}
}

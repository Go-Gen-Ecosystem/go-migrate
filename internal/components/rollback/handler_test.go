/**
 * @file handler_test.go
 * @description Unit tests for error handling and rollback components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/rollback/handler_test.go
 */

package rollback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestErrorType_String(t *testing.T) {
	tests := []struct {
		errorType ErrorType
		expected  string
	}{
		{ErrorTypeValidation, "validation"},
		{ErrorTypeExecution, "execution"},
		{ErrorTypeRollback, "rollback"},
		{ErrorTypeConfiguration, "configuration"},
		{ErrorTypeConnectivity, "connectivity"},
		{ErrorTypeTimeout, "timeout"},
		{ErrorTypeIntegrity, "integrity"},
		{ErrorTypePermission, "permission"},
		{ErrorTypeResource, "resource"},
		{ErrorTypeUnknown, "unknown"},
	}
	
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.errorType.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestErrorSeverity_String(t *testing.T) {
	tests := []struct {
		severity ErrorSeverity
		expected string
	}{
		{ErrorSeverityLow, "low"},
		{ErrorSeverityMedium, "medium"},
		{ErrorSeverityHigh, "high"},
		{ErrorSeverityCritical, "critical"},
	}
	
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.severity.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestRecoveryAction_String(t *testing.T) {
	tests := []struct {
		action   RecoveryAction
		expected string
	}{
		{RecoveryActionRetry, "retry"},
		{RecoveryActionRollback, "rollback"},
		{RecoveryActionSkip, "skip"},
		{RecoveryActionAbort, "abort"},
		{RecoveryActionManual, "manual"},
	}
	
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.action.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestNewMigrationError(t *testing.T) {
	originalErr := errors.New("original error")
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", originalErr)
	
	if migErr == nil {
		t.Fatal("NewMigrationError returned nil")
	}
	
	if migErr.Type != ErrorTypeValidation {
		t.Errorf("expected type %v, got %v", ErrorTypeValidation, migErr.Type)
	}
	
	if migErr.Severity != ErrorSeverityHigh {
		t.Errorf("expected severity %v, got %v", ErrorSeverityHigh, migErr.Severity)
	}
	
	if migErr.Message != "test error" {
		t.Errorf("expected message 'test error', got %s", migErr.Message)
	}
	
	if migErr.Cause != originalErr {
		t.Errorf("expected original error %v, got %v", originalErr, migErr.Cause)
	}
	
	if migErr.Timestamp.IsZero() {
		t.Error("timestamp should be set")
	}
	
	if len(migErr.StackTrace) == 0 {
		t.Error("stack trace should not be empty")
	}
}

func TestMigrationError_Error(t *testing.T) {
	originalErr := errors.New("original error")
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", originalErr)
	
	errorStr := migErr.Error()
	
	if !strings.Contains(errorStr, "validation") {
		t.Error("error string should contain error type")
	}
	
	if !strings.Contains(errorStr, "high") {
		t.Error("error string should contain severity")
	}
	
	if !strings.Contains(errorStr, "test error") {
		t.Error("error string should contain message")
	}
	
	if !strings.Contains(errorStr, "original error") {
		t.Error("error string should contain original error")
	}
}

func TestMigrationError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", originalErr)
	
	unwrapped := migErr.Unwrap()
	if unwrapped != originalErr {
		t.Errorf("expected unwrapped error %v, got %v", originalErr, unwrapped)
	}
}

func TestMigrationError_WithContext(t *testing.T) {
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", nil)
	
	migErr.WithContext("key1", "value1")
	migErr.WithContext("key2", 42)
	
	if len(migErr.Context) != 2 {
		t.Errorf("expected 2 context items, got %d", len(migErr.Context))
	}
	
	if migErr.Context["key1"] != "value1" {
		t.Errorf("expected 'value1', got %v", migErr.Context["key1"])
	}
	
	if migErr.Context["key2"] != 42 {
		t.Errorf("expected 42, got %v", migErr.Context["key2"])
	}
}

func TestErrorRule_Matches(t *testing.T) {
	rule := &ErrorRule{
		ErrorType: ErrorTypeValidation,
		Severity:  ErrorSeverityHigh,
		Action:    RecoveryActionRollback,
	}
	
	// Test matching error
	matchingErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test", nil)
	if !rule.Matches(matchingErr) {
		t.Error("rule should match error with same type and severity")
	}
	
	// Test non-matching type
	nonMatchingTypeErr := NewMigrationError(ErrorTypeExecution, ErrorSeverityHigh, "test", nil)
	if rule.Matches(nonMatchingTypeErr) {
		t.Error("rule should not match error with different type")
	}
	
	// Test non-matching severity
	nonMatchingSeverityErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityLow, "test", nil)
	if rule.Matches(nonMatchingSeverityErr) {
		t.Error("rule should not match error with different severity")
	}
}

func TestErrorRule_MatchesWithWildcard(t *testing.T) {
	// Rule with wildcard type
	rule := &ErrorRule{
		ErrorType:     ErrorTypeWildcard, // Wildcard
		Severity: ErrorSeverityHigh,
		Action:   RecoveryActionRollback,
	}
	
	// Should match any type with matching severity
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityHigh, "test", nil)
	
	if !rule.Matches(err1) {
		t.Error("wildcard rule should match validation error")
	}
	
	if !rule.Matches(err2) {
		t.Error("wildcard rule should match execution error")
	}
	
	// Should not match different severity
	err3 := NewMigrationError(ErrorTypeValidation, ErrorSeverityLow, "test", nil)
	if rule.Matches(err3) {
		t.Error("wildcard rule should not match different severity")
	}
}

func TestNewDefaultErrorHandler(t *testing.T) {
	handler := NewDefaultErrorHandler()
	
	if handler == nil {
		t.Fatal("NewDefaultErrorHandler returned nil")
	}
	
	if len(handler.rules) == 0 {
		t.Error("default handler should have default rules")
	}
}

func TestDefaultErrorHandler_RegisterErrorRule(t *testing.T) {
	handler := NewDefaultErrorHandler()
	initialRuleCount := len(handler.rules)
	
	rule := &ErrorRule{
		ErrorType:     ErrorTypeValidation,
		Severity: ErrorSeverityHigh,
		Action:   RecoveryActionRollback,
	}
	
	handler.RegisterErrorRule(rule)
	
	if len(handler.rules) != initialRuleCount+1 {
		t.Errorf("expected %d rules, got %d", initialRuleCount+1, len(handler.rules))
	}
	
	// Check that the rule was added
	found := false
	for _, r := range handler.rules {
		if r.ErrorType == rule.ErrorType && r.Severity == rule.Severity && r.Action == rule.Action {
			found = true
			break
		}
	}
	
	if !found {
		t.Error("rule was not added to handler")
	}
}

func TestDefaultErrorHandler_HandleError(t *testing.T) {
	handler := NewDefaultErrorHandler()
	
	// Add a specific rule
	rule := &ErrorRule{
		ErrorType:       ErrorTypeValidation,
		Severity:   ErrorSeverityHigh,
		Action:     RecoveryActionRollback,
		MaxRetries: 2,
	}
	handler.RegisterErrorRule(rule)
	
	ctx := context.Background()
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "validation failed", nil)
	
	action, err := handler.HandleError(ctx, migErr)
	if err != nil {
		t.Fatalf("HandleError failed: %v", err)
	}
	
	if action.Action != RecoveryActionRollback {
		t.Errorf("expected action %v, got %v", RecoveryActionRollback, action.Action)
	}
}

func TestDefaultErrorHandler_HandleErrorWithRetry(t *testing.T) {
	handler := NewDefaultErrorHandler()
	
	// Add a retry rule
	rule := &ErrorRule{
		ErrorType:       ErrorTypeConnectivity,
		Severity:   ErrorSeverityMedium,
		Action:     RecoveryActionRetry,
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	}
	handler.RegisterErrorRule(rule)
	
	ctx := context.Background()
	migErr := NewMigrationError(ErrorTypeConnectivity, ErrorSeverityMedium, "connection failed", nil)
	
	action, err := handler.HandleError(ctx, migErr)
	if err != nil {
		t.Fatalf("HandleError failed: %v", err)
	}
	
	if action.Action != RecoveryActionRetry {
		t.Errorf("expected action %v, got %v", RecoveryActionRetry, action.Action)
	}
}

func TestDefaultErrorHandler_HandleErrorNoMatchingRule(t *testing.T) {
	handler := NewDefaultErrorHandler()
	
	// Clear default rules
	handler.rules = nil
	
	ctx := context.Background()
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "validation failed", nil)
	
	action, err := handler.HandleError(ctx, migErr)
	if err != nil {
		t.Fatalf("HandleError failed: %v", err)
	}
	
	// Should default to abort action
	if action.Action != RecoveryActionAbort {
		t.Errorf("expected default action %v, got %v", RecoveryActionAbort, action.Action)
	}
}

func TestNewDefaultRollbackStrategy(t *testing.T) {
	strategy := NewDefaultRollbackStrategy()
	
	if strategy == nil {
		t.Fatal("NewDefaultRollbackStrategy returned nil")
	}
	
	if len(strategy.steps) != 0 {
		t.Error("new strategy should have no steps initially")
	}
}

func TestDefaultRollbackStrategy_AddStep(t *testing.T) {
	strategy := NewDefaultRollbackStrategy()
	
	step := func(ctx context.Context) error {
		return nil
	}
	
	strategy.AddStep("test-step", step)
	
	if len(strategy.steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(strategy.steps))
	}
	
	if strategy.steps[0].Name != "test-step" {
		t.Errorf("expected step name 'test-step', got %s", strategy.steps[0].Name)
	}
}

func TestDefaultRollbackStrategy_Execute(t *testing.T) {
	strategy := NewDefaultRollbackStrategy()
	
	executed := make([]string, 0)
	
	// Add steps in order
	strategy.AddStep("step1", func(ctx context.Context) error {
		executed = append(executed, "step1")
		return nil
	})
	
	strategy.AddStep("step2", func(ctx context.Context) error {
		executed = append(executed, "step2")
		return nil
	})
	
	strategy.AddStep("step3", func(ctx context.Context) error {
		executed = append(executed, "step3")
		return nil
	})
	
	ctx := context.Background()
	err := strategy.Execute(ctx)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	
	// Steps should be executed in reverse order
	expectedOrder := []string{"step3", "step2", "step1"}
	if len(executed) != len(expectedOrder) {
		t.Errorf("expected %d executed steps, got %d", len(expectedOrder), len(executed))
	}
	
	for i, expected := range expectedOrder {
		if i >= len(executed) || executed[i] != expected {
			t.Errorf("expected step %s at index %d, got %s", expected, i, executed[i])
		}
	}
}

func TestDefaultRollbackStrategy_ExecuteWithError(t *testing.T) {
	strategy := NewDefaultRollbackStrategy()
	
	executed := make([]string, 0)
	
	strategy.AddStep("step1", func(ctx context.Context) error {
		executed = append(executed, "step1")
		return nil
	})
	
	strategy.AddStep("step2", func(ctx context.Context) error {
		executed = append(executed, "step2")
		return errors.New("step2 failed")
	})
	
	strategy.AddStep("step3", func(ctx context.Context) error {
		executed = append(executed, "step3")
		return nil
	})
	
	ctx := context.Background()
	err := strategy.Execute(ctx)
	if err == nil {
		t.Error("expected error from failed step")
	}
	
	// Should still execute all steps even if one fails
	expectedOrder := []string{"step3", "step2", "step1"}
	if len(executed) != len(expectedOrder) {
		t.Errorf("expected %d executed steps, got %d", len(expectedOrder), len(executed))
	}
}

func TestNewErrorCollector(t *testing.T) {
	collector := NewErrorCollector()
	
	if collector == nil {
		t.Fatal("NewErrorCollector returned nil")
	}
	
	if collector.HasErrors() {
		t.Error("new collector should not have errors")
	}
	
	if collector.Count() != 0 {
		t.Errorf("expected 0 errors, got %d", collector.Count())
	}
}

func TestErrorCollector_Add(t *testing.T) {
	collector := NewErrorCollector()
	
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "error 1", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityMedium, "error 2", nil)
	
	collector.Add(err1)
	collector.Add(err2)
	
	if !collector.HasErrors() {
		t.Error("collector should have errors")
	}
	
	if collector.Count() != 2 {
		t.Errorf("expected 2 errors, got %d", collector.Count())
	}
}

func TestErrorCollector_GetErrors(t *testing.T) {
	collector := NewErrorCollector()
	
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "error 1", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityMedium, "error 2", nil)
	
	collector.Add(err1)
	collector.Add(err2)
	
	errors := collector.GetErrors()
	
	if len(errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errors))
	}
	
	if errors[0] != err1 {
		t.Error("first error should match")
	}
	
	if errors[1] != err2 {
		t.Error("second error should match")
	}
}

func TestErrorCollector_GetErrorsByType(t *testing.T) {
	collector := NewErrorCollector()
	
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "validation error 1", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityMedium, "execution error", nil)
	err3 := NewMigrationError(ErrorTypeValidation, ErrorSeverityLow, "validation error 2", nil)
	
	collector.Add(err1)
	collector.Add(err2)
	collector.Add(err3)
	
	validationErrors := collector.GetErrorsByType(ErrorTypeValidation)
	if len(validationErrors) != 2 {
		t.Errorf("expected 2 validation errors, got %d", len(validationErrors))
	}
	
	executionErrors := collector.GetErrorsByType(ErrorTypeExecution)
	if len(executionErrors) != 1 {
		t.Errorf("expected 1 execution error, got %d", len(executionErrors))
	}
	
	dependencyErrors := collector.GetErrorsByType(ErrorTypeConfiguration)
	if len(dependencyErrors) != 0 {
		t.Errorf("expected 0 dependency errors, got %d", len(dependencyErrors))
	}
}

func TestErrorCollector_GetErrorsBySeverity(t *testing.T) {
	collector := NewErrorCollector()
	
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "high severity error 1", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityMedium, "medium severity error", nil)
	err3 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "high severity error 2", nil)
	
	collector.Add(err1)
	collector.Add(err2)
	collector.Add(err3)
	
	highSeverityErrors := collector.GetErrorsBySeverity(ErrorSeverityHigh)
	if len(highSeverityErrors) != 2 {
		t.Errorf("expected 2 high severity errors, got %d", len(highSeverityErrors))
	}
	
	mediumSeverityErrors := collector.GetErrorsBySeverity(ErrorSeverityMedium)
	if len(mediumSeverityErrors) != 1 {
		t.Errorf("expected 1 medium severity error, got %d", len(mediumSeverityErrors))
	}
	
	lowSeverityErrors := collector.GetErrorsBySeverity(ErrorSeverityLow)
	if len(lowSeverityErrors) != 0 {
		t.Errorf("expected 0 low severity errors, got %d", len(lowSeverityErrors))
	}
}

func TestErrorCollector_Clear(t *testing.T) {
	collector := NewErrorCollector()
	
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "error 1", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityMedium, "error 2", nil)
	
	collector.Add(err1)
	collector.Add(err2)
	
	if !collector.HasErrors() {
		t.Error("collector should have errors before clear")
	}
	
	collector.Clear()
	
	if collector.HasErrors() {
		t.Error("collector should not have errors after clear")
	}
	
	if collector.Count() != 0 {
		t.Errorf("expected 0 errors after clear, got %d", collector.Count())
	}
}

func TestErrorCollector_Error(t *testing.T) {
	collector := NewErrorCollector()
	
	// Empty collector should return empty string
	if collector.Error() != "" {
		t.Error("empty collector should return empty string")
	}
	
	// Add errors
	err1 := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "error 1", nil)
	err2 := NewMigrationError(ErrorTypeExecution, ErrorSeverityMedium, "error 2", nil)
	
	collector.Add(err1)
	collector.Add(err2)
	
	combinedErr := collector.Error()
	if combinedErr == "" {
		t.Error("collector with errors should return non-empty string")
	}
	
	errorStr := combinedErr
	if !strings.Contains(errorStr, "error 1") {
		t.Error("combined error should contain first error message")
	}
	
	if !strings.Contains(errorStr, "error 2") {
		t.Error("combined error should contain second error message")
	}
}

func TestConcurrentErrorHandling(t *testing.T) {
	handler := NewDefaultErrorHandler()
	collector := NewErrorCollector()
	
	ctx := context.Background()
	
	// Run concurrent error handling
	const numErrors = 100
	errChan := make(chan error, numErrors)
	
	for i := 0; i < numErrors; i++ {
		go func(index int) {
			migErr := NewMigrationError(
				ErrorTypeValidation,
				ErrorSeverityMedium,
				fmt.Sprintf("error %d", index),
				nil,
			)
			
			collector.Add(migErr)
			
			_, err := handler.HandleError(ctx, migErr)
			errChan <- err
		}(i)
	}
	
	// Collect results
	for i := 0; i < numErrors; i++ {
		select {
		case err := <-errChan:
			if err != nil {
				t.Fatalf("error handling failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for error handling")
		}
	}
	
	if collector.Count() != numErrors {
		t.Errorf("expected %d errors in collector, got %d", numErrors, collector.Count())
	}
}

func BenchmarkMigrationError_Creation(b *testing.B) {
	originalErr := errors.New("original error")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", originalErr)
	}
}

func BenchmarkErrorHandler_HandleError(b *testing.B) {
	handler := NewDefaultErrorHandler()
	ctx := context.Background()
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", nil)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := handler.HandleError(ctx, migErr)
		if err != nil {
			b.Fatalf("HandleError failed: %v", err)
		}
	}
}

func BenchmarkErrorCollector_Add(b *testing.B) {
	collector := NewErrorCollector()
	migErr := NewMigrationError(ErrorTypeValidation, ErrorSeverityHigh, "test error", nil)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		collector.Add(migErr)
	}
}

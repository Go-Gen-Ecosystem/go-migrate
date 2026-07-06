/**
 * @file manager_test.go
 * @description Unit tests for observability management components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/observability/manager_test.go
 */

package observability

import (
	"context"
	"testing"
	"time"
)

func TestNewObservabilityManager(t *testing.T) {
	metrics := NewDefaultMetricsCollector()
	tracing := NewNoOpTracingManager()
	logger := NewSlogLogger()
	
	manager := NewObservabilityManager(metrics, tracing, logger)
	
	if manager == nil {
		t.Fatal("NewObservabilityManager returned nil")
	}
	
	if manager.metrics == nil {
		t.Error("metrics collector not initialized")
	}
	
	if manager.tracing == nil {
		t.Error("tracing manager not initialized")
	}
	
	if manager.logger == nil {
		t.Error("logger not initialized")
	}
}

func TestObservabilityManagerSetEnabled(t *testing.T) {
	metrics := NewDefaultMetricsCollector()
	tracing := NewNoOpTracingManager()
	logger := NewSlogLogger()
	
	manager := NewObservabilityManager(metrics, tracing, logger)
	
	// Test enabling/disabling
	manager.SetEnabled(true)
	manager.SetEnabled(false)
	
	// Test should not panic
	t.Log("SetEnabled test completed successfully")
}

func TestObservabilityManagerGetMetrics(t *testing.T) {
	metrics := NewDefaultMetricsCollector()
	tracing := NewNoOpTracingManager()
	logger := NewSlogLogger()
	
	manager := NewObservabilityManager(metrics, tracing, logger)
	
	ctx := context.Background()
	snapshot, err := manager.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}
	
	if snapshot == nil {
		t.Error("GetMetrics returned nil snapshot")
	}
}

func TestObservabilityManagerLogger(t *testing.T) {
	metrics := NewDefaultMetricsCollector()
	tracing := NewNoOpTracingManager()
	logger := NewSlogLogger()
	
	manager := NewObservabilityManager(metrics, tracing, logger)
	
	managerLogger := manager.Logger()
	if managerLogger == nil {
		t.Error("Logger returned nil")
	}
	
	// Test that we can use the logger
	managerLogger.Info("test message")
	managerLogger.Debug("debug message")
	managerLogger.Warn("warning message")
}

func TestNoOpMetricsCollector(t *testing.T) {
	collector := &NoOpMetricsCollector{}
	
	// Test that all methods can be called without panic
	collector.RecordMigrationDuration("test-migration", "apply", time.Second)
	collector.RecordMigrationSuccess("test-migration", "apply")
	collector.RecordMigrationFailure("test-migration", "apply", "validation")
	collector.RecordLockAcquisitionTime("test-lock", time.Millisecond*100)
	collector.RecordLockAcquisitionFailure("test-lock")
	collector.RecordLockHoldTime("test-lock", time.Second*5)
	collector.RecordActiveConnections(10)
	collector.RecordPendingMigrations(5)
	collector.RecordAppliedMigrations(15)
	collector.RecordDriftDetection("test-migration", "schema")
	collector.RecordChecksumValidation("test-migration", true)
	collector.RecordDatabaseQueryTime("select", time.Millisecond*50)
	collector.RecordMemoryUsage(1024*1024)
	
	// Test GetMetrics
	ctx := context.Background()
	snapshot, err := collector.GetMetrics(ctx)
	if err != nil {
		t.Errorf("GetMetrics failed: %v", err)
	}
	if snapshot == nil {
		t.Error("GetMetrics returned nil snapshot")
	}
	
	// Test Reset
	collector.Reset()
}

func TestNoOpTracingManager(t *testing.T) {
	manager := &NoOpTracingManager{}
	ctx := context.Background()
	
	// Test span creation - StartSpan returns (context.Context, func())
	newCtx, endFunc := manager.StartSpan(ctx, "test.operation")
	if newCtx == nil {
		t.Error("StartSpan returned nil context")
	}
	if endFunc == nil {
		t.Error("StartSpan returned nil end function")
	}
	
	// Test span operations
	manager.AddSpanAttribute(ctx, "test.key", "test.value")
	manager.RecordError(ctx, nil) // Test with nil error
	
	// Call the end function
	endFunc()
}







func TestObservabilityManagerWithNoOpComponents(t *testing.T) {
	// Create no-op implementations
	metrics := &NoOpMetricsCollector{}
	tracing := NewNoOpTracingManager()
	logger := NewSlogLogger()
	
	manager := NewObservabilityManager(metrics, tracing, logger)
	
	// Should still create implementations
	if manager.metrics == nil {
		t.Error("metrics collector should be created")
	}
	
	if manager.tracing == nil {
		t.Error("tracing manager should be created")
	}
	
	if manager.logger == nil {
		t.Error("logger should be created")
	}
	
	// Test that manager is enabled by default
	if !manager.IsEnabled() {
		t.Error("manager should be enabled by default")
	}
	
	// Test disabling
	manager.SetEnabled(false)
	if manager.IsEnabled() {
		t.Error("manager should be disabled after SetEnabled(false)")
	}
}

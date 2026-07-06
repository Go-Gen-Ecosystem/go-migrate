/**
 * @file factory_test.go
 * @description Behavior tests for the checksum component factory (minimal and
 *              default configs), the no-op components used when features are
 *              disabled, the default metrics, and the default event handler.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/checksum/factory_test.go
 */

package checksum

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFactory_MinimalConfig_CreatesComponents(t *testing.T) {
	f := NewChecksumComponentFactory(MinimalFactoryConfig())

	gen, err := f.CreateGenerator()
	if err != nil || gen == nil {
		t.Fatalf("CreateGenerator: %v", err)
	}
	val, err := f.CreateValidator()
	if err != nil || val == nil {
		t.Fatalf("CreateValidator: %v", err)
	}
	store, err := f.CreateStore()
	if err != nil || store == nil {
		t.Fatalf("CreateStore: %v", err)
	}
	// Auditing disabled -> NoOpAuditor.
	auditor, err := f.CreateAuditor()
	if err != nil || auditor == nil {
		t.Fatalf("CreateAuditor: %v", err)
	}
	if _, ok := auditor.(*NoOpAuditor); !ok {
		t.Errorf("expected NoOpAuditor when auditing disabled, got %T", auditor)
	}
	// Metrics disabled -> NoOpMetrics.
	metrics, err := f.CreateMetrics()
	if err != nil || metrics == nil {
		t.Fatalf("CreateMetrics: %v", err)
	}
	if _, ok := metrics.(*NoOpMetrics); !ok {
		t.Errorf("expected NoOpMetrics when metrics disabled, got %T", metrics)
	}
	// Events disabled -> NoOpEventHandler.
	events, err := f.CreateEventHandler()
	if err != nil || events == nil {
		t.Fatalf("CreateEventHandler: %v", err)
	}
	if _, ok := events.(*NoOpEventHandler); !ok {
		t.Errorf("expected NoOpEventHandler when events disabled, got %T", events)
	}
}

func TestFactory_MinimalConfig_CreatesManagerAndSystem(t *testing.T) {
	f := NewChecksumComponentFactory(MinimalFactoryConfig())

	mgr, err := f.CreateManager()
	if err != nil || mgr == nil {
		t.Fatalf("CreateManager: %v", err)
	}

	sys, err := f.CreateCompleteSystem()
	if err != nil {
		t.Fatalf("CreateCompleteSystem: %v", err)
	}
	if sys.Manager == nil || sys.Generator == nil || sys.Store == nil {
		t.Errorf("incomplete system: %+v", sys)
	}

	// The generated manager should function end-to-end.
	rec, err := mgr.GenerateAndStore(context.Background(), "V1", []byte("x"), ChecksumTypeMigration, AlgorithmSHA256)
	if err != nil || rec == nil {
		t.Fatalf("manager GenerateAndStore: %v", err)
	}
}

func TestFactory_NilConfigUsesDefault(t *testing.T) {
	f := NewChecksumComponentFactory(nil)
	// Default config enables auditing with a database audit store but no DB
	// connection, so CreateAuditor must surface a clear error.
	if _, err := f.CreateAuditor(); err == nil {
		t.Errorf("default auditing without DB connection should error")
	}
	// Generator/store/validator do not require a DB and should succeed.
	if _, err := f.CreateGenerator(); err != nil {
		t.Errorf("CreateGenerator with default config: %v", err)
	}
}

func TestFactoryConfigPresets(t *testing.T) {
	if DefaultFactoryConfig().DefaultAlgorithm != AlgorithmSHA256 {
		t.Errorf("default algorithm should be sha256")
	}
	hp := HighPerformanceFactoryConfig()
	if hp.GeneratorType != GeneratorTypeParallel || hp.ParallelWorkers != 8 {
		t.Errorf("high performance config mismatch: %+v", hp)
	}
	min := MinimalFactoryConfig()
	if min.EnableAuditing || min.EnableMetrics || min.EnableEvents {
		t.Errorf("minimal config should disable auditing/metrics/events")
	}
}

func TestNoOpImplementationsAreInert(t *testing.T) {
	ctx := context.Background()

	auditor := &NoOpAuditor{}
	if err := auditor.LogOperation(ctx, "op", "r", nil); err != nil {
		t.Errorf("NoOpAuditor.LogOperation: %v", err)
	}
	if entries, err := auditor.GetAuditLog(ctx, nil); err != nil || len(entries) != 0 {
		t.Errorf("NoOpAuditor.GetAuditLog = %v %v", entries, err)
	}
	if n, err := auditor.ArchiveOldEntries(ctx, time.Now()); err != nil || n != 0 {
		t.Errorf("NoOpAuditor.ArchiveOldEntries = %d %v", n, err)
	}

	metrics := &NoOpMetrics{}
	metrics.RecordGeneration(AlgorithmSHA256, time.Second, 10)
	metrics.RecordValidation(AlgorithmSHA256, StatusValid, time.Second)
	metrics.RecordStorageOperation("store", time.Second, true)
	if metrics.GetGenerationMetrics() == nil || metrics.GetValidationMetrics() == nil || metrics.GetStorageMetrics() == nil {
		t.Errorf("NoOpMetrics getters returned nil")
	}
	metrics.Reset()

	events := &NoOpEventHandler{}
	if err := events.HandleEvent(ctx, &ChecksumEvent{}); err != nil {
		t.Errorf("NoOpEventHandler.HandleEvent: %v", err)
	}
	if err := events.Subscribe("t", func(*ChecksumEvent) {}); err != nil {
		t.Errorf("NoOpEventHandler.Subscribe: %v", err)
	}
	if err := events.Unsubscribe("t"); err != nil {
		t.Errorf("NoOpEventHandler.Unsubscribe: %v", err)
	}
}

func TestDefaultChecksumMetrics(t *testing.T) {
	m := NewDefaultChecksumMetrics()
	m.RecordGeneration(AlgorithmSHA256, time.Millisecond, 100)
	m.RecordValidation(AlgorithmSHA256, StatusValid, time.Millisecond)
	m.RecordStorageOperation("store", time.Millisecond, true)
	if m.GetGenerationMetrics() == nil {
		t.Errorf("GetGenerationMetrics returned nil")
	}
	m.Reset()
	if m.GetValidationMetrics() == nil {
		t.Errorf("GetValidationMetrics returned nil after reset")
	}
}

func TestDefaultEventHandler_SubscribeAndHistory(t *testing.T) {
	h := NewDefaultEventHandler(8)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	received := make(chan *ChecksumEvent, 1)
	if err := h.Subscribe("checksum_generated", func(e *ChecksumEvent) {
		received <- e
		wg.Done()
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	event := &ChecksumEvent{Type: "checksum_generated", RecordID: "R1", Message: "ok"}
	if err := h.HandleEvent(ctx, event); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	// Handler runs asynchronously; wait for it.
	wg.Wait()
	select {
	case got := <-received:
		if got.RecordID != "R1" {
			t.Errorf("handler received %+v", got)
		}
	default:
		t.Errorf("handler was not invoked")
	}

	// History is recorded for the record ID.
	history, err := h.GetEventHistory("R1")
	if err != nil {
		t.Fatalf("GetEventHistory: %v", err)
	}
	if len(history) != 1 || history[0].RecordID != "R1" {
		t.Errorf("unexpected history: %+v", history)
	}

	// Unsubscribe removes handlers.
	if err := h.Unsubscribe("checksum_generated"); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
}

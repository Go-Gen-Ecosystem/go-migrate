/*
@file builder_setters_test.go
@description Tests for the remaining MySQL engine builder fluent setters
             (lock manager, tracker, executor, validator, resolver, decorator,
             option) that are not exercised by the core builder tests. Every
             setter stores its dependency and returns the builder for chaining;
             these tests assert both, and confirm the stored components flow
             through Build into the constructed MigrationEngine.
@author Admilson B. F. Cossa
@date 2025-07-06
@path internal/components/engines/mysql/builder_setters_test.go
*/

package mysql

import (
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// The builder setters only store the supplied dependency and return the
// builder; they never invoke any method on it. Each stub therefore embeds the
// corresponding interface so it satisfies the type without a full method set.
type stubLockManager struct{ types.LockManager }
type stubTracker struct{ types.Tracker }
type stubExecutor struct{ types.Executor }
type stubValidator struct{ types.Validator }
type stubResolver struct{ types.Resolver }

// stubDecorator implements types.Decorator (MigrationDecorator). Decorate is a
// pass-through and Name identifies it; neither is invoked by the builder wiring
// under test, but both are implemented for interface satisfaction.
type stubDecorator struct{ name string }

func (d *stubDecorator) Decorate(e types.MigrationEngine) types.MigrationEngine { return e }
func (d *stubDecorator) Name() string                                          { return d.name }

func TestMySQLEngineBuilder_WithLockManager(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	lm := &stubLockManager{}

	result := builder.WithLockManager(lm)
	if result == nil {
		t.Fatal("WithLockManager returned nil")
	}
	if builder.lockManager != lm {
		t.Error("lock manager was not stored")
	}
}

func TestMySQLEngineBuilder_WithTracker(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	tr := &stubTracker{}

	result := builder.WithTracker(tr)
	if result == nil {
		t.Fatal("WithTracker returned nil")
	}
	if builder.tracker != tr {
		t.Error("tracker was not stored")
	}
}

func TestMySQLEngineBuilder_WithExecutor(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	ex := &stubExecutor{}

	result := builder.WithExecutor(ex)
	if result == nil {
		t.Fatal("WithExecutor returned nil")
	}
	if builder.executor != ex {
		t.Error("executor was not stored")
	}
}

func TestMySQLEngineBuilder_WithValidator(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	v := &stubValidator{}

	result := builder.WithValidator(v)
	if result == nil {
		t.Fatal("WithValidator returned nil")
	}
	if builder.validator != v {
		t.Error("validator was not stored")
	}
}

func TestMySQLEngineBuilder_WithResolver(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	r := &stubResolver{}

	result := builder.WithResolver(r)
	if result == nil {
		t.Fatal("WithResolver returned nil")
	}
	if builder.resolver != r {
		t.Error("resolver was not stored")
	}
}

func TestMySQLEngineBuilder_WithDecorator(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	d1 := &stubDecorator{name: "logging"}
	d2 := &stubDecorator{name: "metrics"}

	// WithDecorator appends; calling twice must accumulate both decorators in
	// order.
	builder.WithDecorator(d1)
	result := builder.WithDecorator(d2)
	if result == nil {
		t.Fatal("WithDecorator returned nil")
	}
	if len(builder.decorators) != 2 {
		t.Fatalf("expected 2 decorators, got %d", len(builder.decorators))
	}
	if builder.decorators[0] != d1 || builder.decorators[1] != d2 {
		t.Error("decorators were not appended in order")
	}
}

func TestMySQLEngineBuilder_WithOption(t *testing.T) {
	builder := NewMySQLEngineBuilder()

	result := builder.WithOption("lock_timeout", "30s")
	if result == nil {
		t.Fatal("WithOption returned nil")
	}
	if builder.options["lock_timeout"] != "30s" {
		t.Errorf("expected option 'lock_timeout' to be '30s', got %v", builder.options["lock_timeout"])
	}

	// A second option must coexist with the first.
	builder.WithOption("batch_size", 100)
	if builder.options["batch_size"] != 100 {
		t.Errorf("expected option 'batch_size' to be 100, got %v", builder.options["batch_size"])
	}
	if builder.options["lock_timeout"] != "30s" {
		t.Error("first option was lost after setting a second")
	}
}

// TestMySQLEngineBuilder_Build_WiresAllComponents verifies every component
// supplied through the fluent setters is copied into the constructed
// MigrationEngine's exported input fields.
func TestMySQLEngineBuilder_Build_WiresAllComponents(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}
	lm := &stubLockManager{}
	tr := &stubTracker{}
	ex := &stubExecutor{}
	v := &stubValidator{}
	r := &stubResolver{}
	d := &stubDecorator{name: "logging"}

	// WithConfig/WithBackend/... return the types.EngineBuilder interface, which
	// does not expose the concrete-only WithDecorator/WithOption/WithDryRun. All
	// setters mutate the same underlying builder, so the interface-typed setters
	// are chained and the concrete-only ones are invoked on the concrete builder.
	builder := NewMySQLEngineBuilder()
	builder.WithConfig(config).
		WithBackend(backend).
		WithLockManager(lm).
		WithTracker(tr).
		WithExecutor(ex).
		WithValidator(v).
		WithResolver(r)
	builder.WithDecorator(d)
	builder.WithOption("region", "eu")
	builder.WithDryRun(true)

	built, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed unexpectedly: %v", err)
	}

	me, ok := built.(*engine.MigrationEngine)
	if !ok {
		t.Fatalf("expected *engine.MigrationEngine, got %T", built)
	}

	if me.Backend != backend {
		t.Error("backend not wired into engine")
	}
	if me.LockManager != lm {
		t.Error("lock manager not wired into engine")
	}
	if me.Tracker != tr {
		t.Error("tracker not wired into engine")
	}
	if me.Executor != ex {
		t.Error("executor not wired into engine")
	}
	if me.Validator != v {
		t.Error("validator not wired into engine")
	}
	if me.Resolver != r {
		t.Error("resolver not wired into engine")
	}
	if len(me.Decorators) != 1 || me.Decorators[0] != d {
		t.Error("decorators not wired into engine")
	}
	if !me.DryRun {
		t.Error("dry run flag not wired into engine")
	}
	if me.Options["region"] != "eu" {
		t.Errorf("option not wired into engine, got %v", me.Options["region"])
	}
	if me.Config.Migration.TableName != config.Migration.TableName {
		t.Error("config not wired into engine")
	}
}

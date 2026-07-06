/**
 * @file builder_setters_test.go
 * @description Tests for the remaining SQL Server engine builder fluent setters
 *              (lock manager, tracker, executor, validator, resolver,
 *              decorator) not covered by the core builder tests. Each setter
 *              stores its dependency and returns the builder for chaining; the
 *              wiring test confirms the stored components flow through Build
 *              into the constructed MigrationEngine.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/engines/sqlserver/builder_setters_test.go
 */

package sqlserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// The setters only store the supplied dependency and return the builder; they
// never invoke any method on it. Each stub embeds the corresponding interface
// so it satisfies the type without a full method set.
type stubLockManager struct{ types.LockManager }
type stubTracker struct{ types.Tracker }
type stubExecutor struct{ types.Executor }
type stubValidator struct{ types.Validator }
type stubResolver struct{ types.Resolver }

type stubDecorator struct{ name string }

func (d *stubDecorator) Decorate(e types.MigrationEngine) types.MigrationEngine { return e }
func (d *stubDecorator) Name() string                                          { return d.name }

func TestSQLServerEngineBuilder_WithLockManager(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	lm := &stubLockManager{}

	result := builder.WithLockManager(lm)
	require.NotNil(t, result)
	assert.Same(t, lm, builder.lockManager)
}

func TestSQLServerEngineBuilder_WithTracker(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	tr := &stubTracker{}

	result := builder.WithTracker(tr)
	require.NotNil(t, result)
	assert.Same(t, tr, builder.tracker)
}

func TestSQLServerEngineBuilder_WithExecutor(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	ex := &stubExecutor{}

	result := builder.WithExecutor(ex)
	require.NotNil(t, result)
	assert.Same(t, ex, builder.executor)
}

func TestSQLServerEngineBuilder_WithValidator(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	v := &stubValidator{}

	result := builder.WithValidator(v)
	require.NotNil(t, result)
	assert.Same(t, v, builder.validator)
}

func TestSQLServerEngineBuilder_WithResolver(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	r := &stubResolver{}

	result := builder.WithResolver(r)
	require.NotNil(t, result)
	assert.Same(t, r, builder.resolver)
}

func TestSQLServerEngineBuilder_WithDecorator(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	d1 := &stubDecorator{name: "logging"}
	d2 := &stubDecorator{name: "metrics"}

	builder.WithDecorator(d1)
	result := builder.WithDecorator(d2)
	require.NotNil(t, result)
	require.Len(t, builder.decorators, 2)
	assert.Same(t, d1, builder.decorators[0])
	assert.Same(t, d2, builder.decorators[1])
}

// TestSQLServerEngineBuilder_Build_WiresAllComponents verifies every component
// supplied through the fluent setters is copied into the constructed
// MigrationEngine's exported input fields.
func TestSQLServerEngineBuilder_Build_WiresAllComponents(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}
	lm := &stubLockManager{}
	tr := &stubTracker{}
	ex := &stubExecutor{}
	v := &stubValidator{}
	r := &stubResolver{}
	d := &stubDecorator{name: "logging"}

	// WithConfig/WithBackend/... return the types.EngineBuilder interface, which
	// does not expose the concrete-only WithDecorator. All setters mutate the
	// same underlying builder, so the interface-typed setters are chained and
	// WithDecorator is invoked on the concrete builder.
	builder := NewSQLServerEngineBuilder()
	builder.WithConfig(config).
		WithBackend(backend).
		WithLockManager(lm).
		WithTracker(tr).
		WithExecutor(ex).
		WithValidator(v).
		WithResolver(r)
	builder.WithDecorator(d)

	built, err := builder.Build()
	require.NoError(t, err)

	me, ok := built.(*engine.MigrationEngine)
	require.True(t, ok)

	assert.Same(t, backend, me.Backend)
	assert.Same(t, lm, me.LockManager)
	assert.Same(t, tr, me.Tracker)
	assert.Same(t, ex, me.Executor)
	assert.Same(t, v, me.Validator)
	assert.Same(t, r, me.Resolver)
	require.Len(t, me.Decorators, 1)
	assert.Same(t, d, me.Decorators[0])
	assert.Equal(t, config.Migration.TableName, me.Config.Migration.TableName)
}

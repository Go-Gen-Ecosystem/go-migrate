/*
@file builder_test.go
@description Tests for the SQLite engine builder: fluent configuration, backend
             requirement enforcement, and end-to-end wiring of builder inputs into
             the constructed MigrationEngine.
@author Admilson B. F. Cossa
@date 2025-07-06
@path internal/components/engines/sqlite/builder_test.go
*/

package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// stubBackend is a no-op types.Backend (types.EngineBackend) satisfying the
// builder's backend dependency. Its methods are never exercised by the builder
// wiring under test, so they return zero values.
type stubBackend struct{}

func (s *stubBackend) AcquireLock(ctx context.Context) error { return nil }
func (s *stubBackend) ReleaseLock(ctx context.Context) error { return nil }
func (s *stubBackend) Apply(ctx context.Context, migration types.MigrationScript) error {
	return nil
}
func (s *stubBackend) Rollback(ctx context.Context, migration types.MigrationScript) error {
	return nil
}
func (s *stubBackend) ReadStatus(ctx context.Context) ([]types.MigrationStatus, error) {
	return nil, nil
}
func (s *stubBackend) UpdateStatus(ctx context.Context, migration types.MigrationScript, status string) error {
	return nil
}
func (s *stubBackend) GetDialect() string        { return "sqlite" }
func (s *stubBackend) SupportsTransactions() bool { return true }
func (s *stubBackend) SupportsRollback() bool     { return true }

func newTestConfig() types.Config {
	return types.Config{
		Migration: types.MigrationConfig{
			TableName:        "schema_migrations",
			LockTableName:    "schema_migrations_lock",
			MigrationsPath:   "./migrations",
			AutoCreateTables: true,
		},
	}
}

func TestNewSQLiteEngineBuilder(t *testing.T) {
	builder := NewSQLiteEngineBuilder()
	require.NotNil(t, builder)
	assert.NotNil(t, builder.options, "options map must be initialized")
}

func TestSQLiteEngineBuilder_WithConfig(t *testing.T) {
	builder := NewSQLiteEngineBuilder()
	config := newTestConfig()

	result := builder.WithConfig(config)
	require.NotNil(t, result)
	assert.Equal(t, config.Migration.TableName, builder.config.Migration.TableName)
}

func TestSQLiteEngineBuilder_WithBackend(t *testing.T) {
	builder := NewSQLiteEngineBuilder()
	backend := &stubBackend{}

	result := builder.WithBackend(backend)
	require.NotNil(t, result)
	assert.Same(t, backend, builder.backend)
}

func TestSQLiteEngineBuilder_WithOptionsAndDryRun(t *testing.T) {
	builder := NewSQLiteEngineBuilder()

	builder.WithOption("key", "value")
	builder.WithDryRun(true)

	assert.Equal(t, "value", builder.options["key"])
	assert.Equal(t, true, builder.options["dry_run"])
}

func TestSQLiteEngineBuilder_ChainedSetters(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	builder := NewSQLiteEngineBuilder()
	// WithConfig/WithBackend return the types.EngineBuilder interface (which does
	// not expose WithOption); they mutate and return the same underlying builder.
	// WithOption is invoked on the concrete builder.
	builder.WithConfig(config).WithBackend(backend)
	builder.WithOption("opt", 1)

	assert.Equal(t, config.Migration.TableName, builder.config.Migration.TableName)
	assert.Same(t, backend, builder.backend)
	assert.Equal(t, 1, builder.options["opt"])
}

func TestSQLiteEngineBuilder_BuildRequiresBackend(t *testing.T) {
	builder := NewSQLiteEngineBuilder()
	builder.WithConfig(newTestConfig())
	builder.WithDryRun(true)

	eng, err := builder.Build()
	assert.Error(t, err, "Build must fail without a backend")
	assert.Nil(t, eng, "no partially-constructed engine on failure")
}

func TestSQLiteEngineBuilder_BuildWiresComponents(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	builder := NewSQLiteEngineBuilder()
	builder.WithConfig(config).WithBackend(backend)
	builder.WithOption("cache", "on")

	eng, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, eng)

	// The builder wires its inputs onto the concrete engine's exported fields.
	me, ok := eng.(*engine.MigrationEngine)
	require.True(t, ok, "Build must return *engine.MigrationEngine")
	assert.Same(t, backend, me.Backend)
	assert.Equal(t, config.Migration.TableName, me.Config.Migration.TableName)
	assert.Equal(t, "on", me.Options["cache"])
}

// stubDecorator is a minimal types.Decorator (types.MigrationDecorator) used to
// verify WithDecorator appends the decorator and that it reaches the engine.
type stubDecorator struct{ name string }

func (d *stubDecorator) Decorate(e types.MigrationEngine) types.MigrationEngine { return e }
func (d *stubDecorator) Name() string                                          { return d.name }

func TestSQLiteEngineBuilder_RemainingSetters(t *testing.T) {
	builder := NewSQLiteEngineBuilder()

	// The remaining component setters store their argument and return the fluent
	// builder. They accept interface values (typed-nil is a valid dependency here
	// since the builder only wires them for later Initialize()).
	assert.NotNil(t, builder.WithLockManager(nil))
	assert.NotNil(t, builder.WithTracker(nil))
	assert.NotNil(t, builder.WithExecutor(nil))
	assert.NotNil(t, builder.WithValidator(nil))
	assert.NotNil(t, builder.WithResolver(nil))

	dec := &stubDecorator{name: "audit"}
	assert.NotNil(t, builder.WithDecorator(dec))
	require.Len(t, builder.decorators, 1)

	// A second decorator appends rather than replaces.
	builder.WithDecorator(&stubDecorator{name: "metrics"})
	assert.Len(t, builder.decorators, 2)

	// Decorators are wired into the built engine.
	builder.WithBackend(&stubBackend{})
	eng, err := builder.Build()
	require.NoError(t, err)
	me, ok := eng.(*engine.MigrationEngine)
	require.True(t, ok)
	assert.Len(t, me.Decorators, 2)
}

func TestSQLiteConfig_Fields(t *testing.T) {
	// SQLiteConfig is a plain value holder consumed by callers configuring the
	// SQLite-specific database path/options.
	cfg := SQLiteConfig{
		DatabasePath: "/data/app.db",
		Options:      map[string]interface{}{"journal_mode": "WAL"},
	}
	assert.Equal(t, "/data/app.db", cfg.DatabasePath)
	assert.Equal(t, "WAL", cfg.Options["journal_mode"])
}

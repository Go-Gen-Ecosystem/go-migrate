/*
@file builder_test.go
@description Tests for the PostgreSQL engine builder: fluent configuration, dry-run
             flag handling, backend requirement enforcement, and end-to-end wiring
             of builder inputs into the constructed MigrationEngine.
@author Admilson B. F. Cossa
@date 2025-07-06
@path internal/components/engines/postgres/builder_test.go
*/

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// stubBackend is a no-op types.Backend (types.EngineBackend) satisfying the
// builder's backend dependency.
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
func (s *stubBackend) GetDialect() string        { return "postgres" }
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

func TestNewPostgresEngineBuilder(t *testing.T) {
	builder := NewPostgresEngineBuilder()
	require.NotNil(t, builder)
	assert.NotNil(t, builder.options)
}

func TestPostgresEngineBuilder_WithConfig(t *testing.T) {
	builder := NewPostgresEngineBuilder()
	config := newTestConfig()

	result := builder.WithConfig(config)
	require.NotNil(t, result)
	assert.Equal(t, config.Migration.TableName, builder.config.Migration.TableName)
}

func TestPostgresEngineBuilder_WithBackend(t *testing.T) {
	builder := NewPostgresEngineBuilder()
	backend := &stubBackend{}

	result := builder.WithBackend(backend)
	require.NotNil(t, result)
	assert.Same(t, backend, builder.backend)
}

func TestPostgresEngineBuilder_WithDryRun(t *testing.T) {
	builder := NewPostgresEngineBuilder()

	result := builder.WithDryRun(true)
	require.NotNil(t, result)
	assert.True(t, builder.dryRun, "PostgreSQL builder tracks dry-run in a dedicated field")
}

func TestPostgresEngineBuilder_WithOption(t *testing.T) {
	builder := NewPostgresEngineBuilder()
	builder.WithOption("statement_timeout", "30s")
	assert.Equal(t, "30s", builder.options["statement_timeout"])
}

func TestPostgresEngineBuilder_ChainedSetters(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	builder := NewPostgresEngineBuilder()
	builder.WithConfig(config).WithBackend(backend)
	builder.WithDryRun(true)

	assert.Equal(t, config.Migration.TableName, builder.config.Migration.TableName)
	assert.Same(t, backend, builder.backend)
	assert.True(t, builder.dryRun)
}

func TestPostgresEngineBuilder_BuildRequiresBackend(t *testing.T) {
	builder := NewPostgresEngineBuilder()
	builder.WithConfig(newTestConfig())
	builder.WithDryRun(true)

	eng, err := builder.Build()
	assert.Error(t, err, "Build must fail without a backend")
	assert.Nil(t, eng)
}

func TestPostgresEngineBuilder_BuildWiresComponents(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	builder := NewPostgresEngineBuilder()
	builder.WithConfig(config).WithBackend(backend)
	builder.WithOption("app", "svc")

	eng, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, eng)

	me, ok := eng.(*engine.MigrationEngine)
	require.True(t, ok)
	assert.Same(t, backend, me.Backend)
	assert.Equal(t, config.Migration.TableName, me.Config.Migration.TableName)
	assert.Equal(t, "svc", me.Options["app"])
}

func TestPostgresEngineBuilder_BuildPropagatesDryRun(t *testing.T) {
	builder := NewPostgresEngineBuilder()
	builder.WithConfig(newTestConfig()).WithBackend(&stubBackend{})
	builder.WithDryRun(true)

	eng, err := builder.Build()
	require.NoError(t, err)

	me, ok := eng.(*engine.MigrationEngine)
	require.True(t, ok)
	assert.True(t, me.DryRun, "dry-run flag must reach the constructed engine")
}

// stubDecorator is a minimal types.Decorator used to verify WithDecorator.
type stubDecorator struct{ name string }

func (d *stubDecorator) Decorate(e types.MigrationEngine) types.MigrationEngine { return e }
func (d *stubDecorator) Name() string                                          { return d.name }

func TestPostgresEngineBuilder_RemainingSetters(t *testing.T) {
	builder := NewPostgresEngineBuilder()

	assert.NotNil(t, builder.WithLockManager(nil))
	assert.NotNil(t, builder.WithTracker(nil))
	assert.NotNil(t, builder.WithExecutor(nil))
	assert.NotNil(t, builder.WithValidator(nil))
	assert.NotNil(t, builder.WithResolver(nil))

	assert.NotNil(t, builder.WithDecorator(&stubDecorator{name: "audit"}))
	builder.WithDecorator(&stubDecorator{name: "metrics"})
	assert.Len(t, builder.decorators, 2)

	builder.WithBackend(&stubBackend{})
	eng, err := builder.Build()
	require.NoError(t, err)
	me, ok := eng.(*engine.MigrationEngine)
	require.True(t, ok)
	assert.Len(t, me.Decorators, 2)
}

func TestPostgreSQLConfig_Fields(t *testing.T) {
	cfg := PostgreSQLConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "app",
		Username: "postgres",
		Password: "secret",
		SSLMode:  "disable",
	}
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "disable", cfg.SSLMode)
}

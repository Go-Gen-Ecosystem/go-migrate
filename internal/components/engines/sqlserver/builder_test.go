/**
 * @file builder_test.go
 * @description Tests for the SQL Server engine builder: fluent configuration,
 *              backend requirement enforcement, and end-to-end wiring of builder
 *              inputs into the constructed MigrationEngine.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/engines/sqlserver/builder_test.go
 */

package sqlserver

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
func (s *stubBackend) GetDialect() string        { return "sqlserver" }
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

func TestNewSQLServerEngineBuilder(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	require.NotNil(t, builder)
	assert.NotNil(t, builder.options)
}

func TestSQLServerEngineBuilder_WithConfig(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	config := newTestConfig()

	result := builder.WithConfig(config)
	require.NotNil(t, result)
	assert.Equal(t, config.Migration.TableName, builder.config.Migration.TableName)
}

func TestSQLServerEngineBuilder_WithBackend(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	backend := &stubBackend{}

	result := builder.WithBackend(backend)
	require.NotNil(t, result)
	assert.Same(t, backend, builder.backend)
}

func TestSQLServerEngineBuilder_WithOptionAndDryRun(t *testing.T) {
	builder := NewSQLServerEngineBuilder()

	builder.WithOption("lock_timeout", "10s")
	builder.WithDryRun(true)

	assert.Equal(t, "10s", builder.options["lock_timeout"])
	assert.Equal(t, true, builder.options["dry_run"])
}

func TestSQLServerEngineBuilder_ChainedSetters(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	builder := NewSQLServerEngineBuilder()
	// WithConfig/WithBackend return the types.EngineBuilder interface (no
	// WithOption); WithOption is invoked on the concrete builder.
	builder.WithConfig(config).WithBackend(backend)
	builder.WithOption("opt", true)

	assert.Equal(t, config.Migration.TableName, builder.config.Migration.TableName)
	assert.Same(t, backend, builder.backend)
	assert.Equal(t, true, builder.options["opt"])
}

func TestSQLServerEngineBuilder_BuildRequiresBackend(t *testing.T) {
	builder := NewSQLServerEngineBuilder()
	builder.WithConfig(newTestConfig())
	builder.WithDryRun(true)

	eng, err := builder.Build()
	assert.Error(t, err, "Build must fail without a backend")
	assert.Nil(t, eng)
}

func TestSQLServerEngineBuilder_BuildWiresComponents(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	builder := NewSQLServerEngineBuilder()
	builder.WithConfig(config).WithBackend(backend)
	builder.WithOption("region", "eu")

	eng, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, eng)

	me, ok := eng.(*engine.MigrationEngine)
	require.True(t, ok)
	assert.Same(t, backend, me.Backend)
	assert.Equal(t, config.Migration.TableName, me.Config.Migration.TableName)
	assert.Equal(t, "eu", me.Options["region"])
}

func TestSQLServerConfig_Fields(t *testing.T) {
	cfg := SQLServerConfig{
		ConnectionString: "server=localhost;database=app",
		Options:          map[string]interface{}{"encrypt": true},
	}
	assert.Equal(t, "server=localhost;database=app", cfg.ConnectionString)
	assert.Equal(t, true, cfg.Options["encrypt"])
}

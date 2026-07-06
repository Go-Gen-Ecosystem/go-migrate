/*
@file builder_test.go
@description Test file for MySQL builder functionality verification
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/components/engines/mysql/builder_test.go
*/

package mysql

import (
	"context"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	_ "github.com/go-sql-driver/mysql"
)

// stubBackend is a no-op types.Backend (types.EngineBackend) used to satisfy the
// builder's backend dependency in unit tests. Its methods are never exercised by
// the builder wiring under test, so they return zero values.
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
func (s *stubBackend) GetDialect() string          { return "mysql" }
func (s *stubBackend) SupportsTransactions() bool   { return true }
func (s *stubBackend) SupportsRollback() bool       { return true }

// newTestConfig builds a representative migration configuration. The real
// EngineBuilder.WithConfig accepts a types.Config value (not *types.MigrationConfig);
// migration-specific settings live under Config.Migration.
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

func TestMySQLEngineBuilder_Basic(t *testing.T) {
	// Test basic builder creation
	builder := NewMySQLEngineBuilder()
	if builder == nil {
		t.Fatal("NewMySQLEngineBuilder() returned nil")
	}

	// Test configuration (types.Config is a value struct)
	config := newTestConfig()

	result := builder.WithConfig(config)
	if result == nil {
		t.Fatal("WithConfig() returned nil")
	}

	// Verify config was set. types.Config contains maps and is therefore not
	// ==-comparable; assert on a distinguishing field instead.
	if builder.config.Migration.TableName != config.Migration.TableName {
		t.Error("Config was not set properly")
	}
}

func TestMySQLEngineBuilder_WithBackend(t *testing.T) {
	builder := NewMySQLEngineBuilder()

	// WithDatabase/db were removed; database access is now supplied via the
	// backend dependency. Verify the builder stores an injected backend.
	backend := &stubBackend{}

	result := builder.WithBackend(backend)
	if result == nil {
		t.Fatal("WithBackend() returned nil")
	}

	// Verify backend was set
	if builder.backend != backend {
		t.Error("Backend was not set properly")
	}
}

func TestMySQLEngineBuilder_WithDryRun(t *testing.T) {
	builder := NewMySQLEngineBuilder()

	result := builder.WithDryRun(true)
	if result == nil {
		t.Fatal("WithDryRun() returned nil")
	}

	// Verify dry run was set
	if !builder.dryRun {
		t.Error("DryRun was not set properly")
	}
}

func TestMySQLEngineBuilder_ChainedCalls(t *testing.T) {
	config := newTestConfig()
	backend := &stubBackend{}

	// WithConfig/WithBackend return the types.EngineBuilder interface (which does
	// not expose the MySQL-specific WithDryRun), so the fluent interface chain is
	// exercised through the interface, while WithDryRun is invoked on the concrete
	// builder. All setters mutate and return the same underlying builder.
	mysqlBuilder := NewMySQLEngineBuilder()
	mysqlBuilder.WithConfig(config).WithBackend(backend)
	mysqlBuilder.WithDryRun(true)

	if mysqlBuilder.config.Migration.TableName != config.Migration.TableName {
		t.Error("Config was not set in chained calls")
	}
	if mysqlBuilder.backend != backend {
		t.Error("Backend was not set in chained calls")
	}
	if !mysqlBuilder.dryRun {
		t.Error("DryRun was not set in chained calls")
	}
}

// TestMySQLEngineBuilder_Build_Minimal tests the Build method with minimal setup.
// Build requires a backend, so building without one must return an error.
func TestMySQLEngineBuilder_Build_Minimal(t *testing.T) {
	config := newTestConfig()

	builder := NewMySQLEngineBuilder()
	builder.WithConfig(config)
	builder.WithDryRun(true) // Use dry run to avoid actual database operations

	// No backend is configured, so Build must fail with a clear error and
	// must not return a partially-constructed engine.
	engine, err := builder.Build()

	if err == nil {
		t.Error("Build succeeded unexpectedly - a backend is required")
	}
	if engine != nil {
		t.Error("Build returned a non-nil engine despite the missing backend")
	}

	t.Logf("Build result: engine=%v, err=%v", engine != nil, err)
}

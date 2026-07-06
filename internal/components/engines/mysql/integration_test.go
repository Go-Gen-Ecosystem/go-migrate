/*
@file integration_test.go
@description Integration test for MySQL builder with actual Build functionality
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/components/engines/mysql/integration_test.go
*/

package mysql

import (
	"testing"
)

// TestMySQLEngineBuilder_Build_WithBackend exercises the full Build path once a
// backend dependency is supplied. The builder no longer accepts a raw *sql.DB
// (WithDatabase was removed); database access is provided via a types.Backend,
// so a stub backend is sufficient to build the engine in dry-run mode.
func TestMySQLEngineBuilder_Build_WithBackend(t *testing.T) {
	config := newTestConfig()

	builder := NewMySQLEngineBuilder()
	builder.WithConfig(config).WithBackend(&stubBackend{})
	builder.WithDryRun(true) // Use dry run to avoid actual database operations

	engine, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed unexpectedly with a backend configured: %v", err)
	}
	if engine == nil {
		t.Fatal("Build returned a nil engine despite a configured backend")
	}
}

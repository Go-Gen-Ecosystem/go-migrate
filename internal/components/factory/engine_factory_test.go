/**
 * @file engine_factory_test.go
 * @description Unit tests for the migration engine factory registration and creation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 */

package factory

import (
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// TestMigrationEngineFactory_SupportedTypes verifies the default builders are
// registered, including MySQL. This test lives in the factory package because
// NewMigrationEngineFactory and the builder registry are owned here (the engine
// packages only provide their individual builders).
func TestMigrationEngineFactory_SupportedTypes(t *testing.T) {
	factory := NewMigrationEngineFactory()

	supportedTypes := factory.GetSupportedTypes()
	found := false
	for _, dbType := range supportedTypes {
		if dbType == "mysql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("MySQL builder not found in supported database types: %v", supportedTypes)
	}

	if !factory.IsSupported("mysql") {
		t.Error("IsSupported(\"mysql\") returned false")
	}
}

// TestMigrationEngineFactory_CreateEngine_MySQL exercises engine creation for the
// registered MySQL builder. CreateEngine takes a types.Config value (not a
// pointer). With no backend wired into the default builder, Build enforces its
// invariant and returns an error, so CreateEngine must surface that error and
// return no engine.
func TestMigrationEngineFactory_CreateEngine_MySQL(t *testing.T) {
	factory := NewMigrationEngineFactory()

	config := types.Config{
		Migration: types.MigrationConfig{
			TableName:        "schema_migrations",
			LockTableName:    "schema_migrations_lock",
			MigrationsPath:   "./migrations",
			AutoCreateTables: true,
		},
	}

	engine, err := factory.CreateEngine("mysql", config)
	if err == nil {
		t.Error("CreateEngine succeeded unexpectedly - the default MySQL builder has no backend")
	}
	if engine != nil {
		t.Error("CreateEngine returned a non-nil engine despite the build error")
	}
}

// TestMigrationEngineFactory_CreateEngine_Unsupported verifies unknown database
// types are rejected with an error.
func TestMigrationEngineFactory_CreateEngine_Unsupported(t *testing.T) {
	factory := NewMigrationEngineFactory()

	engine, err := factory.CreateEngine("nonexistent-db", types.Config{})
	if err == nil {
		t.Error("Expected error for unsupported database type, got nil")
	}
	if engine != nil {
		t.Error("Expected nil engine for unsupported database type")
	}
}

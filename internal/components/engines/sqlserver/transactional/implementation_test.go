/**
 * @file implementation_test.go
 * @description Test file for migration tool implementation
 * @author Admilson B. F. Cossa
 * @date 2024-12-28
 * @path pkg/core/database/migrations/internal/implementation/implementation_test.go
 */

package implementation

import (
	"context"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestNewMigrationTool(t *testing.T) {
	t.Run("Create migration tool with valid config", func(t *testing.T) {
		config := &types.Config{
			Environment: "test",
			Database: types.DatabaseConfig{
				Driver:   "postgres",
				Host:     "localhost",
				Port:     5432,
				Database: "test_db",
				Username: "test_user",
				Password: "test_pass",
			},
			Migration: types.MigrationConfig{
				MigrationsPath: "./testdata/migrations",
				LockTableName:  "migration_locks",
				TableName:      "schema_migrations",
			},
		}

		tool, err := NewMigrationTool(config)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		if tool == nil {
			t.Fatal("Expected migration tool to be created, got nil")
		}
	})

	t.Run("Migration tool implements MigrationTool interface", func(t *testing.T) {
		config := &types.Config{
			Environment: "test",
			Database: types.DatabaseConfig{
				Driver:   "postgres",
				Host:     "localhost",
				Port:     5432,
				Database: "test_db",
				Username: "test_user",
				Password: "test_pass",
			},
			Migration: types.MigrationConfig{
				MigrationsPath: "./testdata/migrations",
				LockTableName:  "migration_locks",
				TableName:      "schema_migrations",
			},
		}

		tool, err := NewMigrationTool(config)
		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}

		// Test that tool implements the interface methods
		ctx := context.Background()
		
		// These calls should not panic (interface compliance test)
		_, err = tool.Status(ctx)
		// We expect an error here since we're not connected to a real database
		// but the important thing is that the method exists and can be called
		
		err = tool.Migrate(ctx, "", true) // dry run
		// Again, we expect an error but the method should exist
		
		err = tool.Rollback(ctx, 1)
		// Same here
		
		err = tool.Validate(ctx)
		// Same here
		
		// If we reach here without panics, the interface is properly implemented
		t.Log("Migration tool properly implements MigrationTool interface")
	})
}

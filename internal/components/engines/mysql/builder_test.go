/*
@file mysql_builder_test.go
@description Test file for MySQL builder functionality verification
@author Admilson B. F. Cossa
@date 2023-10-01
@path pkg/core/database/migrations/internal/factory/mysql_builder_test.go
*/

package mysql

import (
	"database/sql"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	_ "github.com/go-sql-driver/mysql"
)

func TestMySQLEngineBuilder_Basic(t *testing.T) {
	// Test basic builder creation
	builder := NewMySQLEngineBuilder()
	if builder == nil {
		t.Fatal("NewMySQLEngineBuilder() returned nil")
	}

	// Test configuration
	config := &types.MigrationConfig{
		TableName:        "schema_migrations",
		LockTableName:    "schema_migrations_lock",
		MigrationsPath:   "./migrations",
		AutoCreateTables: true,
	}

	result := builder.WithConfig(config)
	if result == nil {
		t.Fatal("WithConfig() returned nil")
	}

	// Verify config was set
	if builder.config != config {
		t.Error("Config was not set properly")
	}
}

func TestMySQLEngineBuilder_WithDatabase(t *testing.T) {
	builder := NewMySQLEngineBuilder()
	
	// Create a mock database connection (we'll use a nil pointer for this test)
	// In a real test, you'd want to use a test database
	var db *sql.DB
	
	result := builder.WithDatabase(db)
	if result == nil {
		t.Fatal("WithDatabase() returned nil")
	}

	// Verify database was set
	if builder.db != db {
		t.Error("Database was not set properly")
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
	config := &types.MigrationConfig{
		TableName:        "schema_migrations",
		LockTableName:    "schema_migrations_lock",
		MigrationsPath:   "./migrations",
		AutoCreateTables: true,
	}

	var db *sql.DB

	// Test method chaining
	builder := NewMySQLEngineBuilder().
		WithConfig(config).
		WithDatabase(db).
		WithDryRun(true)

	if builder == nil {
		t.Fatal("Chained calls returned nil")
	}

	// Cast back to concrete type to verify internal state
	mysqlBuilder, ok := builder.(*MySQLEngineBuilder)
	if !ok {
		t.Fatal("Builder is not a MySQLEngineBuilder")
	}

	if mysqlBuilder.config != config {
		t.Error("Config was not set in chained calls")
	}
	if mysqlBuilder.db != db {
		t.Error("Database was not set in chained calls")
	}
	if !mysqlBuilder.dryRun {
		t.Error("DryRun was not set in chained calls")
	}
}

// TestMySQLEngineBuilder_Build tests the Build method with minimal setup
// Note: This test will fail if dependencies are not properly configured
func TestMySQLEngineBuilder_Build_Minimal(t *testing.T) {
	config := &types.MigrationConfig{
		TableName:        "schema_migrations",
		LockTableName:    "schema_migrations_lock",
		MigrationsPath:   "./migrations",
		AutoCreateTables: true,
	}

	builder := NewMySQLEngineBuilder().
		WithConfig(config).
		WithDryRun(true) // Use dry run to avoid actual database operations

	// Note: We're not setting a real database connection here
	// This test is mainly to check if the Build method can be called
	// without panicking and returns appropriate errors for missing dependencies

	engine, err := builder.Build()
	
	// We expect an error because we don't have a real database connection
	if err == nil {
		t.Log("Build succeeded unexpectedly - this might indicate the method doesn't validate inputs properly")
	}
	
	if engine != nil && err != nil {
		t.Error("Build returned both an engine and an error - should be one or the other")
	}

	t.Logf("Build result: engine=%v, err=%v", engine != nil, err)
}

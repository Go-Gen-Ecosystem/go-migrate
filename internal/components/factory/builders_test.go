/**
 * @file builders_test.go
 * @description Unit tests for factory builders
 * @author Claude Code
 * @date 2025-01-01
 */

package factory

import (
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestNewPostgreSQLEngineBuilder(t *testing.T) {
	builder := NewPostgreSQLEngineBuilder()

	if builder == nil {
		t.Fatal("NewPostgreSQLEngineBuilder returned nil")
	}

	// Verify it implements EngineBuilder interface
	var _ types.EngineBuilder = builder
}

func TestNewMySQLEngineBuilder(t *testing.T) {
	builder := NewMySQLEngineBuilder()

	if builder == nil {
		t.Fatal("NewMySQLEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewSQLiteEngineBuilder(t *testing.T) {
	builder := NewSQLiteEngineBuilder()

	if builder == nil {
		t.Fatal("NewSQLiteEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewSQLServerEngineBuilder(t *testing.T) {
	builder := NewSQLServerEngineBuilder()

	if builder == nil {
		t.Fatal("NewSQLServerEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewOracleEngineBuilder(t *testing.T) {
	builder := NewOracleEngineBuilder()

	if builder == nil {
		t.Fatal("NewOracleEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewDB2EngineBuilder(t *testing.T) {
	builder := NewDB2EngineBuilder()

	if builder == nil {
		t.Fatal("NewDB2EngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewMongoDBEngineBuilder(t *testing.T) {
	builder := NewMongoDBEngineBuilder()

	if builder == nil {
		t.Fatal("NewMongoDBEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewCassandraEngineBuilder(t *testing.T) {
	builder := NewCassandraEngineBuilder()

	if builder == nil {
		t.Fatal("NewCassandraEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

func TestNewRedisEngineBuilder(t *testing.T) {
	builder := NewRedisEngineBuilder()

	if builder == nil {
		t.Fatal("NewRedisEngineBuilder returned nil")
	}

	var _ types.EngineBuilder = builder
}

// Test ComponentsFactory builders

func TestNewPostgreSQLComponentsFactory(t *testing.T) {
	factory := NewPostgreSQLComponentsFactory()

	if factory == nil {
		t.Fatal("NewPostgreSQLComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "postgresql" {
		t.Errorf("Expected dialect 'postgresql', got '%s'", dialect)
	}

	version := factory.GetComponentVersion()
	if version == "" {
		t.Error("Component version should not be empty")
	}
}

func TestNewMySQLComponentsFactory(t *testing.T) {
	factory := NewMySQLComponentsFactory()

	if factory == nil {
		t.Fatal("NewMySQLComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "mysql" {
		t.Errorf("Expected dialect 'mysql', got '%s'", dialect)
	}
}

func TestNewSQLiteComponentsFactory(t *testing.T) {
	factory := NewSQLiteComponentsFactory()

	if factory == nil {
		t.Fatal("NewSQLiteComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "sqlite" {
		t.Errorf("Expected dialect 'sqlite', got '%s'", dialect)
	}
}

func TestNewSQLServerComponentsFactory(t *testing.T) {
	factory := NewSQLServerComponentsFactory()

	if factory == nil {
		t.Fatal("NewSQLServerComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "sqlserver" {
		t.Errorf("Expected dialect 'sqlserver', got '%s'", dialect)
	}
}

func TestNewOracleComponentsFactory(t *testing.T) {
	factory := NewOracleComponentsFactory()

	if factory == nil {
		t.Fatal("NewOracleComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "oracle" {
		t.Errorf("Expected dialect 'oracle', got '%s'", dialect)
	}
}

func TestNewDB2ComponentsFactory(t *testing.T) {
	factory := NewDB2ComponentsFactory()

	if factory == nil {
		t.Fatal("NewDB2ComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "db2" {
		t.Errorf("Expected dialect 'db2', got '%s'", dialect)
	}
}

func TestNewMongoDBComponentsFactory(t *testing.T) {
	factory := NewMongoDBComponentsFactory()

	if factory == nil {
		t.Fatal("NewMongoDBComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "mongodb" {
		t.Errorf("Expected dialect 'mongodb', got '%s'", dialect)
	}
}

func TestNewCassandraComponentsFactory(t *testing.T) {
	factory := NewCassandraComponentsFactory()

	if factory == nil {
		t.Fatal("NewCassandraComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "cassandra" {
		t.Errorf("Expected dialect 'cassandra', got '%s'", dialect)
	}
}

func TestNewRedisComponentsFactory(t *testing.T) {
	factory := NewRedisComponentsFactory()

	if factory == nil {
		t.Fatal("NewRedisComponentsFactory returned nil")
	}

	var _ types.ComponentsFactory = factory

	dialect := factory.GetSupportedDialect()
	if dialect != "redis" {
		t.Errorf("Expected dialect 'redis', got '%s'", dialect)
	}
}

// Test Placeholder implementations

func TestPlaceholderEngineBuilder(t *testing.T) {
	placeholder := &PlaceholderEngineBuilder{
		dbType: "test",
	}

	// Test Build returns error for placeholder
	engine, err := placeholder.Build()
	if err == nil {
		t.Error("Expected error from placeholder Build(), got nil")
	}
	if engine != nil {
		t.Error("Expected nil engine from placeholder Build()")
	}

	// Verify error message mentions it's a placeholder
	if err != nil && !contains(err.Error(), "placeholder") {
		t.Errorf("Expected error to mention 'placeholder', got: %s", err.Error())
	}
}

func TestPlaceholderComponentsFactory(t *testing.T) {
	placeholder := &PlaceholderComponentsFactory{
		dialect: "test",
	}

	// Create a minimal config
	config := &MockConfig{}

	// Test NewBackend returns error
	_, err := placeholder.NewBackend(config)
	if err == nil {
		t.Error("Expected error from NewBackend(), got nil")
	}

	// Test NewExecutor returns error
	_, err = placeholder.NewExecutor(config)
	if err == nil {
		t.Error("Expected error from NewExecutor(), got nil")
	}

	// Test NewTracker returns error
	_, err = placeholder.NewTracker(config)
	if err == nil {
		t.Error("Expected error from NewTracker(), got nil")
	}

	// Test NewLockManager returns error
	_, err = placeholder.NewLockManager(config)
	if err == nil {
		t.Error("Expected error from NewLockManager(), got nil")
	}

	// Test NewValidator returns error
	_, err = placeholder.NewValidator(config)
	if err == nil {
		t.Error("Expected error from NewValidator(), got nil")
	}

	// Test GetSupportedDialect
	dialect := placeholder.GetSupportedDialect()
	if dialect != "test" {
		t.Errorf("Expected dialect 'test', got '%s'", dialect)
	}

	// Test GetComponentVersion
	version := placeholder.GetComponentVersion()
	if version == "" {
		t.Error("Component version should not be empty")
	}
}

// Test PlaceholderEngineBuilder chainable methods

func TestPlaceholderEngineBuilder_ChainableMethods(t *testing.T) {
	placeholder := &PlaceholderEngineBuilder{
		dbType: "test",
	}

	// Test all chainable methods return the builder
	config := &MockConfig{}
	backend := &MockBackend{}
	executor := &MockExecutor{}
	tracker := &MockTracker{}
	lockMgr := &MockLockManager{}
	validator := &MockValidator{}
	resolver := &MockResolver{}

	result := placeholder.
		WithConfig(config).
		WithBackend(backend).
		WithExecutor(executor).
		WithTracker(tracker).
		WithLockManager(lockMgr).
		WithValidator(validator).
		WithResolver(resolver)

	if result == nil {
		t.Fatal("Chainable methods returned nil")
	}

	// Verify fields were set
	if placeholder.config == nil {
		t.Error("Config was not set")
	}
	if placeholder.backend == nil {
		t.Error("Backend was not set")
	}
	if placeholder.executor == nil {
		t.Error("Executor was not set")
	}
	if placeholder.tracker == nil {
		t.Error("Tracker was not set")
	}
	if placeholder.lockMgr == nil {
		t.Error("LockManager was not set")
	}
	if placeholder.validator == nil {
		t.Error("Validator was not set")
	}
	if placeholder.resolver == nil {
		t.Error("Resolver was not set")
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != "" && substr != "" &&
		   len(s) >= len(substr) && s[:len(substr)] == substr ||
		   (len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Mock implementations for testing

type MockConfig struct{}

func (m *MockConfig) GetString(key string) string                     { return "" }
func (m *MockConfig) GetInt(key string) int                           { return 0 }
func (m *MockConfig) GetBool(key string) bool                         { return false }
func (m *MockConfig) Get(key string) interface{}                      { return nil }
func (m *MockConfig) Set(key string, value interface{})               {}
func (m *MockConfig) GetAll() map[string]interface{}                  { return nil }

type MockBackend struct{}

type MockExecutor struct{}

type MockTracker struct{}

type MockLockManager struct{}

type MockValidator struct{}

type MockResolver struct{}

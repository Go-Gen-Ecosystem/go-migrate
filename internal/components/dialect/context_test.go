/**
 * @file context_test.go
 * @description Unit tests for database dialect management components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/dialect/context_test.go
 */

package dialect

import (
	"strings"
	"testing"
)

func TestNewDatabaseDialectContext(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "15.0")
	
	if ctx == nil {
		t.Fatal("NewDatabaseDialectContext returned nil")
	}
	
	if ctx.Type != "postgres" {
		t.Errorf("expected database type 'postgres', got %s", ctx.Type)
	}
	
	if ctx.Version != "15.0" {
		t.Errorf("expected version '15.0', got %s", ctx.Version)
	}
	
	if ctx.Features == nil {
		t.Error("expected Features map to be initialized")
	}
	
	if ctx.Config == nil {
		t.Error("expected Config map to be initialized")
	}
	
	// Check that default features are set for postgres
	if !ctx.Features["ddl_transactions"] {
		t.Error("expected ddl_transactions to be true for postgres")
	}
}

func TestDatabaseDialectContext_HasFeature(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "15.0")
	
	// Test default features for postgres
	if !ctx.HasFeature("ddl_transactions") {
		t.Error("should have ddl_transactions feature for postgres")
	}
	
	// Set custom features for testing
	ctx.SetFeature("supports_json", true)
	ctx.SetFeature("supports_arrays", true)
	ctx.SetFeature("supports_window", false)
	
	// Test existing boolean features
	if !ctx.HasFeature("supports_json") {
		t.Error("should have supports_json feature")
	}
	
	if !ctx.HasFeature("supports_arrays") {
		t.Error("should have supports_arrays feature")
	}
	
	if ctx.HasFeature("supports_window") {
		t.Error("should not have supports_window feature (false)")
	}
	
	// Test non-existent feature
	if ctx.HasFeature("non_existent") {
		t.Error("should not have non_existent feature")
	}
}

func TestDatabaseDialectContext_GetFeature(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "15.0")
	
	// Set features for testing
	ctx.SetFeature("supports_json", true)
	ctx.SetFeature("supports_arrays", false)
	
	// Test existing features
	supportsJson, exists := ctx.GetFeature("supports_json")
	if !exists {
		t.Error("supports_json feature should exist")
	}
	if supportsJson != true {
		t.Errorf("expected supports_json true, got %v", supportsJson)
	}
	
	supportsArrays, exists := ctx.GetFeature("supports_arrays")
	if !exists {
		t.Error("supports_arrays feature should exist")
	}
	if supportsArrays != false {
		t.Errorf("expected supports_arrays false, got %v", supportsArrays)
	}
	
	// Test default feature
	ddlTrans, exists := ctx.GetFeature("ddl_transactions")
	if !exists {
		t.Error("ddl_transactions feature should exist")
	}
	if ddlTrans != true {
		t.Errorf("expected ddl_transactions true for postgres, got %v", ddlTrans)
	}
	
	// Test non-existent feature
	_, exists = ctx.GetFeature("non_existent")
	if exists {
		t.Error("non_existent feature should not exist")
	}
}

func TestSQLiteDialectStrategy_GetLockSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("sqlite", "3.0")
	strategy := NewSQLiteStrategy(ctx)
	
	lockSQL := strategy.GetLockSQL()
	
	if lockSQL == "" {
		t.Error("lock SQL should not be empty")
	}
	
	if !strings.Contains(lockSQL, "EXCLUSIVE") {
		t.Error("SQLite lock SQL should use EXCLUSIVE TRANSACTION")
	}
}

func TestSQLiteDialectStrategy_GetUnlockSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("sqlite", "3.0")
	strategy := NewSQLiteStrategy(ctx)
	
	unlockSQL := strategy.GetUnlockSQL()
	
	if unlockSQL == "" {
		t.Error("unlock SQL should not be empty")
	}
	
	if !strings.Contains(unlockSQL, "COMMIT") {
		t.Error("SQLite unlock SQL should use COMMIT")
	}
}

func TestSQLiteDialectStrategy_GetCreateTableSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("sqlite", "3.0")
	strategy := NewSQLiteStrategy(ctx)
	
	createSQL := strategy.GetCreateTableSQL("test_table", []string{
		"id INTEGER PRIMARY KEY",
		"name TEXT NOT NULL",
		"created_at DATETIME DEFAULT CURRENT_TIMESTAMP",
	})
	
	if createSQL == "" {
		t.Error("create table SQL should not be empty")
	}
	
	if !strings.Contains(createSQL, "CREATE TABLE") {
		t.Error("should contain CREATE TABLE")
	}
	
	if !strings.Contains(createSQL, "test_table") {
		t.Error("should contain table name")
	}
	
	if !strings.Contains(createSQL, "id INTEGER PRIMARY KEY") {
		t.Error("should contain column definitions")
	}
	
	if !strings.Contains(createSQL, "IF NOT EXISTS") {
		t.Error("SQLite should use IF NOT EXISTS")
	}
}



func TestPostgreSQLDialectStrategy_GetLockSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "13.0")
	strategy := NewPostgresStrategy(ctx)
	
	lockSQL := strategy.GetLockSQL()
	
	if lockSQL == "" {
		t.Error("lock SQL should not be empty")
	}
	
	if !strings.Contains(lockSQL, "pg_advisory_lock") {
		t.Error("PostgreSQL lock SQL should use pg_advisory_lock")
	}
}

func TestPostgreSQLDialectStrategy_GetCreateTableSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("postgres", "13.0")
	strategy := NewPostgresStrategy(ctx)
	
	createSQL := strategy.GetCreateTableSQL("test_table", []string{
		"id SERIAL PRIMARY KEY",
		"name VARCHAR(255) NOT NULL",
		"created_at TIMESTAMP DEFAULT NOW()",
	})
	
	if createSQL == "" {
		t.Error("create table SQL should not be empty")
	}
	
	if !strings.Contains(createSQL, "CREATE TABLE") {
		t.Error("should contain CREATE TABLE")
	}
	
	if !strings.Contains(createSQL, "test_table") {
		t.Error("should contain table name")
	}
	
	if !strings.Contains(createSQL, "IF NOT EXISTS") {
		t.Error("PostgreSQL should use IF NOT EXISTS")
	}
}



func TestMySQLDialectStrategy_GetLockSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("mysql", "8.0")
	strategy := NewMySQLStrategy(ctx)
	
	lockSQL := strategy.GetLockSQL()
	
	if lockSQL == "" {
		t.Error("lock SQL should not be empty")
	}
	
	if !strings.Contains(lockSQL, "GET_LOCK") {
		t.Error("MySQL lock SQL should use GET_LOCK")
	}
}

func TestMySQLDialectStrategy_GetCreateTableSQL(t *testing.T) {
	ctx := NewDatabaseDialectContext("mysql", "8.0")
	strategy := NewMySQLStrategy(ctx)
	
	createSQL := strategy.GetCreateTableSQL("test_table", []string{
		"id INT AUTO_INCREMENT PRIMARY KEY",
		"name VARCHAR(255) NOT NULL",
		"created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP",
	})
	
	if createSQL == "" {
		t.Error("create table SQL should not be empty")
	}
	
	if !strings.Contains(createSQL, "CREATE TABLE") {
		t.Error("should contain CREATE TABLE")
	}
	
	if !strings.Contains(createSQL, "test_table") {
		t.Error("should contain table name")
	}
	
	if !strings.Contains(createSQL, "IF NOT EXISTS") {
		t.Error("MySQL should use IF NOT EXISTS")
	}
}



func TestNewDialectStrategyFactory(t *testing.T) {
	factory := NewDialectStrategyFactory()
	
	if factory == nil {
		t.Fatal("NewDialectStrategyFactory returned nil")
	}
}

func TestDialectStrategyFactory_CreateSQLite(t *testing.T) {
	factory := NewDialectStrategyFactory()
	ctx := NewDatabaseDialectContext("sqlite", "3.0")
	
	strategy, err := factory.CreateStrategy(ctx)
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}
	
	if strategy == nil {
		t.Fatal("CreateStrategy returned nil strategy")
	}
	
	// Check that it's the correct type
	if _, ok := strategy.(*SQLiteStrategy); !ok {
		t.Errorf("expected SQLiteStrategy, got %T", strategy)
	}
}

func TestDialectStrategyFactory_CreatePostgreSQL(t *testing.T) {
	factory := NewDialectStrategyFactory()
	ctx := NewDatabaseDialectContext("postgres", "13.0")
	
	strategy, err := factory.CreateStrategy(ctx)
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}
	
	if strategy == nil {
		t.Fatal("CreateStrategy returned nil strategy")
	}
	
	// Check that it's the correct type
	if _, ok := strategy.(*PostgresStrategy); !ok {
		t.Errorf("expected PostgresStrategy, got %T", strategy)
	}
}

func TestDialectStrategyFactory_CreateMySQL(t *testing.T) {
	factory := NewDialectStrategyFactory()
	ctx := NewDatabaseDialectContext("mysql", "8.0")
	
	strategy, err := factory.CreateStrategy(ctx)
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}
	
	if strategy == nil {
		t.Fatal("CreateStrategy returned nil strategy")
	}
	
	// Check that it's the correct type
	if _, ok := strategy.(*MySQLStrategy); !ok {
		t.Errorf("expected MySQLStrategy, got %T", strategy)
	}
}

func TestDialectStrategyFactory_CreateUnsupported(t *testing.T) {
	factory := NewDialectStrategyFactory()
	ctx := NewDatabaseDialectContext("unsupported", "1.0")
	
	strategy, err := factory.CreateStrategy(ctx)
	if err == nil {
		t.Error("expected error for unsupported database type")
	}
	
	if strategy != nil {
		t.Error("expected nil strategy for unsupported database type")
	}
}

func TestNewDialectManager(t *testing.T) {
	manager := NewDialectManager()
	
	if manager == nil {
		t.Fatal("NewDialectManager returned nil")
	}
	
	// Test that manager can create contexts
	ctx := manager.CreateContext("sqlite", "3.0")
	if ctx == nil {
		t.Error("CreateContext should return a valid context")
	}
	
	if ctx.Type != "sqlite" {
		t.Errorf("expected type 'sqlite', got %s", ctx.Type)
	}
}

func TestDialectManager_CreateContext(t *testing.T) {
	manager := NewDialectManager()
	
	ctx := manager.CreateContext("postgres", "15.0")
	
	if ctx == nil {
		t.Fatal("CreateContext returned nil")
	}
	
	if ctx.Type != "postgres" {
		t.Errorf("expected type 'postgres', got %s", ctx.Type)
	}
	
	if ctx.Version != "15.0" {
		t.Errorf("expected version '15.0', got %s", ctx.Version)
	}
}

func TestDialectManager_RegisterStrategy(t *testing.T) {
	manager := NewDialectManager()
	
	// Register a custom strategy factory
	customFactory := func(ctx *DatabaseDialectContext) DialectStrategy {
		return NewSQLiteStrategy(ctx)
	}
	
	manager.RegisterStrategy("custom_sqlite", customFactory)
	
	// Test that the custom strategy can be created
	ctx := manager.CreateContext("custom_sqlite", "3.0")
	strategy, err := manager.CreateStrategy(ctx)
	
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}
	
	if strategy == nil {
		t.Error("CreateStrategy returned nil strategy")
	}
}

func TestDialectManager_CreateStrategy(t *testing.T) {
	manager := NewDialectManager()
	
	// Test creating strategy for supported database
	ctx := manager.CreateContext("postgres", "15.0")
	strategy, err := manager.CreateStrategy(ctx)
	
	if err != nil {
		t.Fatalf("CreateStrategy failed: %v", err)
	}
	
	if strategy == nil {
		t.Error("CreateStrategy returned nil strategy")
	}
	
	// Verify it's the correct type
	if _, ok := strategy.(*PostgresStrategy); !ok {
		t.Errorf("expected PostgresStrategy, got %T", strategy)
	}
}

func TestDialectManager_CreateStrategyUnsupported(t *testing.T) {
	manager := NewDialectManager()
	
	// Test creating strategy for unsupported database
	ctx := manager.CreateContext("unsupported", "1.0")
	strategy, err := manager.CreateStrategy(ctx)
	
	if err == nil {
		t.Error("expected error for unsupported database type")
	}
	
	if strategy != nil {
		t.Error("expected nil strategy for unsupported database type")
	}
}



func TestNewDialectContextPropagator(t *testing.T) {
	dialectCtx := NewDatabaseDialectContext("postgres", "15.0")
	
	propagator := NewDialectContextPropagator(dialectCtx)
	
	if propagator == nil {
		t.Fatal("NewDialectContextPropagator returned nil")
	}
	
	// Verify context was stored
	retrievedCtx := propagator.GetContext()
	if retrievedCtx != dialectCtx {
		t.Error("retrieved context should match stored context")
	}
}

func TestDialectContextPropagator_PropagateToComponent(t *testing.T) {
	dialectCtx := NewDatabaseDialectContext("postgres", "15.0")
	propagator := NewDialectContextPropagator(dialectCtx)
	
	// Test with DialectAware component
	component := &TestDialectAware{}
	err := propagator.PropagateToComponent(component)
	
	if err != nil {
		t.Fatalf("PropagateToComponent failed: %v", err)
	}
	
	// Verify context was propagated
	if component.GetDialectContext() != dialectCtx {
		t.Error("context was not properly propagated to component")
	}
	
	// Test with non-DialectAware component
	nonAwareComponent := "not dialect aware"
	err = propagator.PropagateToComponent(nonAwareComponent)
	
	if err == nil {
		t.Error("PropagateToComponent should fail for non-DialectAware component")
	}
}

// TestDialectAware is a test implementation that implements DialectAware
type TestDialectAware struct {
	dialectCtx *DatabaseDialectContext
}

func (ta *TestDialectAware) SetDialectContext(ctx *DatabaseDialectContext) {
	ta.dialectCtx = ctx
}

func (ta *TestDialectAware) GetDialectContext() *DatabaseDialectContext {
	return ta.dialectCtx
}

func TestDialectAwareImplementation(t *testing.T) {
	// Test the implementation
	impl := &TestDialectAware{}
	
	dialectCtx := NewDatabaseDialectContext("postgres", "15.0")
	dialectCtx.SetFeature("supports_json", true)
	
	impl.SetDialectContext(dialectCtx)
	
	retrievedCtx := impl.GetDialectContext()
	if retrievedCtx != dialectCtx {
		t.Error("retrieved context should match set context")
	}
}

func TestConcurrentDialectManager(t *testing.T) {
	manager := NewDialectManager()
	
	// Test concurrent context creation and strategy creation
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer func() { done <- true }()
			
			dbType := "postgres"
			if index%2 == 0 {
				dbType = "mysql"
			}
			
			ctx := manager.CreateContext(dbType, "1.0")
			if ctx == nil {
				t.Errorf("CreateContext returned nil for %s", dbType)
				return
			}
			
			if ctx.Type != dbType {
				t.Errorf("expected database type %s, got %s", dbType, ctx.Type)
				return
			}
			
			// Test strategy creation
			strategy, err := manager.CreateStrategy(ctx)
			if err != nil {
				t.Errorf("CreateStrategy failed for %s: %v", dbType, err)
				return
			}
			
			if strategy == nil {
				t.Errorf("CreateStrategy returned nil strategy for %s", dbType)
			}
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func BenchmarkDialectManager_CreateContext(b *testing.B) {
	manager := NewDialectManager()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		ctx := manager.CreateContext("postgres", "15.0")
		if ctx == nil {
			b.Fatal("CreateContext returned nil")
		}
	}
}

func BenchmarkDialectStrategy_GetLockSQL(b *testing.B) {
	ctx := NewDatabaseDialectContext("postgres", "15.0")
	strategy := NewPostgresStrategy(ctx)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_ = strategy.GetLockSQL()
	}
}

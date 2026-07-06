/**
 * @file registry_test.go
 * @description Unit tests for the components factory registry, the fluent
 *              factory builder, and the global plugin registry. These exercise
 *              registration, retrieval, error paths, plugin lifecycle, and the
 *              global convenience wrappers — all pure in-memory logic reachable
 *              without a live database.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/factory/registry_test.go
 */

package factory

import (
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ---- ComponentsFactoryRegistry ----

func TestNewComponentsFactoryRegistry_DefaultsRegistered(t *testing.T) {
	registry := NewComponentsFactoryRegistry()
	if registry == nil {
		t.Fatal("NewComponentsFactoryRegistry returned nil")
	}

	dialects := registry.GetSupportedDialects()
	// Default registration wires postgresql, mysql, sqlite, sqlserver.
	want := map[string]bool{"postgresql": false, "mysql": false, "sqlite": false, "sqlserver": false}
	for _, d := range dialects {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for dialect, found := range want {
		if !found {
			t.Errorf("expected default dialect %q to be registered, dialects=%v", dialect, dialects)
		}
	}
}

func TestComponentsFactoryRegistry_GetFactory_Found(t *testing.T) {
	registry := NewComponentsFactoryRegistry()

	factory, err := registry.GetFactory("mysql")
	if err != nil {
		t.Fatalf("GetFactory(mysql) failed: %v", err)
	}
	if factory == nil {
		t.Fatal("GetFactory returned nil factory")
	}
	if factory.GetSupportedDialect() != "mysql" {
		t.Errorf("expected dialect 'mysql', got %q", factory.GetSupportedDialect())
	}
}

func TestComponentsFactoryRegistry_GetFactory_CaseInsensitive(t *testing.T) {
	registry := NewComponentsFactoryRegistry()

	// Lookup normalizes to lower case.
	factory, err := registry.GetFactory("MySQL")
	if err != nil {
		t.Fatalf("GetFactory(MySQL) failed: %v", err)
	}
	if factory.GetSupportedDialect() != "mysql" {
		t.Errorf("expected dialect 'mysql', got %q", factory.GetSupportedDialect())
	}
}

func TestComponentsFactoryRegistry_GetFactory_NotFound(t *testing.T) {
	registry := NewComponentsFactoryRegistry()

	factory, err := registry.GetFactory("nonexistent")
	if err == nil {
		t.Error("expected error for unregistered dialect, got nil")
	}
	if factory != nil {
		t.Error("expected nil factory for unregistered dialect")
	}
}

func TestComponentsFactoryRegistry_RegisterFactory_Custom(t *testing.T) {
	registry := NewComponentsFactoryRegistry()
	custom := &PlaceholderComponentsFactory{dialect: "customdb"}

	registry.RegisterFactory("CustomDB", custom)

	// Registration normalizes the key to lower case.
	factory, err := registry.GetFactory("customdb")
	if err != nil {
		t.Fatalf("GetFactory(customdb) failed after registration: %v", err)
	}
	if factory.GetSupportedDialect() != "customdb" {
		t.Errorf("expected dialect 'customdb', got %q", factory.GetSupportedDialect())
	}
}

// ---- FactoryBuilder ----

func TestFactoryBuilder_Build_Success(t *testing.T) {
	registry := NewComponentsFactoryRegistry()

	factory, err := NewFactoryBuilder("mysql").
		WithRegistry(registry).
		WithConfig(types.Config{Environment: "test"}).
		WithOption("cache", true).
		Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if factory == nil {
		t.Fatal("Build returned nil factory")
	}
	if factory.GetSupportedDialect() != "mysql" {
		t.Errorf("expected dialect 'mysql', got %q", factory.GetSupportedDialect())
	}
}

func TestFactoryBuilder_Build_UnknownType(t *testing.T) {
	registry := NewComponentsFactoryRegistry()

	factory, err := NewFactoryBuilder("nonexistent").
		WithRegistry(registry).
		Build()
	if err == nil {
		t.Error("expected error for unknown factory type, got nil")
	}
	if factory != nil {
		t.Error("expected nil factory for unknown type")
	}
}

func TestFactoryBuilder_Build_NilRegistry(t *testing.T) {
	// Explicitly nil out the registry to hit the guard clause.
	fb := NewFactoryBuilder("mysql").WithRegistry(nil)

	factory, err := fb.Build()
	if err == nil {
		t.Error("expected error when registry is nil, got nil")
	}
	if factory != nil {
		t.Error("expected nil factory when registry is nil")
	}
}

func TestFactoryBuilder_WithOption_Accumulates(t *testing.T) {
	fb := NewFactoryBuilder("mysql").
		WithOption("a", 1).
		WithOption("b", "two")

	if fb.options["a"] != 1 {
		t.Errorf("expected option a=1, got %v", fb.options["a"])
	}
	if fb.options["b"] != "two" {
		t.Errorf("expected option b='two', got %v", fb.options["b"])
	}
}

func TestGetGlobalComponentsFactoryRegistry_Singleton(t *testing.T) {
	r1 := GetGlobalComponentsFactoryRegistry()
	r2 := GetGlobalComponentsFactoryRegistry()

	if r1 == nil {
		t.Fatal("GetGlobalComponentsFactoryRegistry returned nil")
	}
	if r1 != r2 {
		t.Error("expected the same singleton registry instance on repeated calls")
	}
}

/**
 * @file global_registry_test.go
 * @description Unit tests for the GlobalFactoryRegistry plugin lifecycle
 *              (register/unregister engine builders and components factories),
 *              plugin-aware engine creation, factory info reporting, and the
 *              package-level global convenience wrappers. All logic is pure and
 *              in-memory; engine creation is driven through the placeholder and
 *              default builders so no live database is required.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/factory/global_registry_test.go
 */

package factory

import (
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// pluginBuilder is a minimal EngineBuilder whose Build fails deterministically.
// The plugin registration paths never invoke Build during registration; the
// error is only surfaced when CreateEngineWithPlugin drives Build, letting the
// tests assert the plugin builder was actually selected.
type pluginBuilder struct {
	types.EngineBuilder // embedded: only WithConfig/Build are exercised
	buildErr            error
	built               bool
}

func (b *pluginBuilder) WithConfig(config types.Config) types.EngineBuilder { return b }

func (b *pluginBuilder) Build() (types.MigrationEngine, error) {
	b.built = true
	return nil, b.buildErr
}

func TestGetGlobalFactoryRegistry_Singleton(t *testing.T) {
	r1 := GetGlobalFactoryRegistry()
	r2 := GetGlobalFactoryRegistry()

	if r1 == nil {
		t.Fatal("GetGlobalFactoryRegistry returned nil")
	}
	if r1 != r2 {
		t.Error("expected the same singleton instance on repeated calls")
	}
}

func TestGlobalFactoryRegistry_GetEngineAndComponentsAccessors(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	if gr.GetEngineFactory() == nil {
		t.Error("GetEngineFactory returned nil")
	}
	if gr.GetComponentsRegistry() == nil {
		t.Error("GetComponentsRegistry returned nil")
	}
}

func TestGlobalFactoryRegistry_RegisterPluginBuilder(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	pb := &pluginBuilder{}
	if err := gr.RegisterPluginBuilder("plugindb", pb); err != nil {
		t.Fatalf("RegisterPluginBuilder failed: %v", err)
	}

	if !gr.IsPluginRegistered("plugindb") {
		t.Error("expected plugindb to be registered")
	}

	// Duplicate registration must be rejected.
	if err := gr.RegisterPluginBuilder("plugindb", pb); err == nil {
		t.Error("expected error on duplicate plugin builder registration")
	}

	builders := gr.GetRegisteredPluginBuilders()
	found := false
	for _, b := range builders {
		if b == "plugindb" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected plugindb in registered builders, got %v", builders)
	}

	// A registered plugin builder must also be wired into the engine factory.
	if !gr.GetEngineFactory().IsSupported("plugindb") {
		t.Error("expected engine factory to support the plugin dbType after registration")
	}
}

func TestGlobalFactoryRegistry_RegisterPluginComponentsFactory(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	pf := &PlaceholderComponentsFactory{dialect: "plugindialect"}
	if err := gr.RegisterPluginComponentsFactory("plugindialect", pf); err != nil {
		t.Fatalf("RegisterPluginComponentsFactory failed: %v", err)
	}

	// Duplicate registration must be rejected.
	if err := gr.RegisterPluginComponentsFactory("plugindialect", pf); err == nil {
		t.Error("expected error on duplicate plugin factory registration")
	}

	factories := gr.GetRegisteredPluginFactories()
	found := false
	for _, f := range factories {
		if f == "plugindialect" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected plugindialect in registered factories, got %v", factories)
	}

	// A registered plugin factory must also be wired into the components registry.
	if _, err := gr.GetComponentsRegistry().GetFactory("plugindialect"); err != nil {
		t.Errorf("expected components registry to resolve the plugin dialect: %v", err)
	}
}

func TestGlobalFactoryRegistry_UnregisterPluginBuilder(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	// Unregistering a builder that was never registered must error.
	if err := gr.UnregisterPluginBuilder("missing"); err == nil {
		t.Error("expected error unregistering an unknown plugin builder")
	}

	pb := &pluginBuilder{}
	if err := gr.RegisterPluginBuilder("plugindb", pb); err != nil {
		t.Fatalf("RegisterPluginBuilder failed: %v", err)
	}
	if err := gr.UnregisterPluginBuilder("plugindb"); err != nil {
		t.Fatalf("UnregisterPluginBuilder failed: %v", err)
	}
	if gr.IsPluginRegistered("plugindb") {
		t.Error("expected plugindb to be unregistered")
	}
}

func TestGlobalFactoryRegistry_UnregisterPluginComponentsFactory(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	// Unregistering a factory that was never registered must error.
	if err := gr.UnregisterPluginComponentsFactory("missing"); err == nil {
		t.Error("expected error unregistering an unknown plugin factory")
	}

	pf := &PlaceholderComponentsFactory{dialect: "plugindialect"}
	if err := gr.RegisterPluginComponentsFactory("plugindialect", pf); err != nil {
		t.Fatalf("RegisterPluginComponentsFactory failed: %v", err)
	}
	if err := gr.UnregisterPluginComponentsFactory("plugindialect"); err != nil {
		t.Fatalf("UnregisterPluginComponentsFactory failed: %v", err)
	}

	factories := gr.GetRegisteredPluginFactories()
	for _, f := range factories {
		if f == "plugindialect" {
			t.Error("expected plugindialect to be removed from registered factories")
		}
	}
}

func TestGlobalFactoryRegistry_CreateEngineWithPlugin_UsesPlugin(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	pb := &pluginBuilder{buildErr: nil}
	if err := gr.RegisterPluginBuilder("plugindb", pb); err != nil {
		t.Fatalf("RegisterPluginBuilder failed: %v", err)
	}

	// The plugin builder returns (nil, nil); the important assertion is that the
	// plugin path was taken (pb.built) rather than the default factory.
	_, err := gr.CreateEngineWithPlugin("plugindb", types.Config{})
	if err != nil {
		t.Fatalf("CreateEngineWithPlugin failed: %v", err)
	}
	if !pb.built {
		t.Error("expected the plugin builder's Build to be invoked")
	}
}

func TestGlobalFactoryRegistry_CreateEngineWithPlugin_FallsBackToDefault(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	// No plugin registered for mysql; must fall back to the default engine
	// factory. The default MySQL builder has no backend, so Build errors and the
	// error is surfaced (proving the fallback path executed).
	engine, err := gr.CreateEngineWithPlugin("mysql", types.Config{})
	if err == nil {
		t.Error("expected error from default MySQL builder without a backend")
	}
	if engine != nil {
		t.Error("expected nil engine on default builder failure")
	}
}

func TestGlobalFactoryRegistry_GetFactoryInfo(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	if err := gr.RegisterPluginBuilder("plugindb", &pluginBuilder{}); err != nil {
		t.Fatalf("RegisterPluginBuilder failed: %v", err)
	}
	if err := gr.RegisterPluginComponentsFactory("plugindialect", &PlaceholderComponentsFactory{dialect: "plugindialect"}); err != nil {
		t.Fatalf("RegisterPluginComponentsFactory failed: %v", err)
	}

	info := gr.GetFactoryInfo()
	if len(info.EngineTypes) == 0 {
		t.Error("expected non-empty engine types in factory info")
	}
	if len(info.ComponentDialects) == 0 {
		t.Error("expected non-empty component dialects in factory info")
	}
	if info.TotalRegistrations != 2 {
		t.Errorf("expected 2 total plugin registrations, got %d", info.TotalRegistrations)
	}
}

func TestGlobalFactoryRegistry_Reset_ClearsPlugins(t *testing.T) {
	gr := GetGlobalFactoryRegistry()
	gr.Reset()

	if err := gr.RegisterPluginBuilder("plugindb", &pluginBuilder{}); err != nil {
		t.Fatalf("RegisterPluginBuilder failed: %v", err)
	}

	gr.Reset()

	if gr.IsPluginRegistered("plugindb") {
		t.Error("expected Reset to clear all plugin builders")
	}
	if len(gr.GetRegisteredPluginBuilders()) != 0 {
		t.Error("expected no plugin builders after Reset")
	}
	if len(gr.GetRegisteredPluginFactories()) != 0 {
		t.Error("expected no plugin factories after Reset")
	}
}

// ---- Global convenience wrappers ----

func TestGlobalConvenienceWrappers(t *testing.T) {
	GetGlobalFactoryRegistry().Reset()

	if err := RegisterGlobalPluginBuilder("gplugin", &pluginBuilder{}); err != nil {
		t.Fatalf("RegisterGlobalPluginBuilder failed: %v", err)
	}
	if err := RegisterGlobalPluginComponentsFactory("gdialect", &PlaceholderComponentsFactory{dialect: "gdialect"}); err != nil {
		t.Fatalf("RegisterGlobalPluginComponentsFactory failed: %v", err)
	}

	info := GetGlobalFactoryInfo()
	if info.TotalRegistrations != 2 {
		t.Errorf("expected 2 total registrations via global info, got %d", info.TotalRegistrations)
	}

	// CreateGlobalEngine for a dialect with no backend surfaces the build error,
	// confirming the global create path is wired to the underlying registry.
	engine, err := CreateGlobalEngine("mysql", types.Config{})
	if err == nil {
		t.Error("expected error creating a global mysql engine without a backend")
	}
	if engine != nil {
		t.Error("expected nil engine on global create failure")
	}

	// Clean up so the shared singleton does not leak plugin state to other tests.
	GetGlobalFactoryRegistry().Reset()
}

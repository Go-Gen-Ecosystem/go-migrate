/**
 * @file types_test.go
 * @description Behavior tests for the types package: default config, error types,
 *              retry/fatal classification, and the StandaloneMigrationToolImpl
 *              delegation + config persistence round-trips.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/types/types_test.go
 */

package types

import (
	"context"
	stderrors "errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultMigrationConfig(t *testing.T) {
	cfg := DefaultMigrationConfig()

	if cfg.TableName != "schema_migrations" {
		t.Errorf("TableName = %q, want schema_migrations", cfg.TableName)
	}
	if cfg.LockTableName != "schema_migrations_lock" {
		t.Errorf("LockTableName = %q", cfg.LockTableName)
	}
	if cfg.Environment != "development" {
		t.Errorf("Environment = %q, want development", cfg.Environment)
	}
	if !cfg.UseTransactions {
		t.Errorf("UseTransactions should default to true")
	}
	if cfg.StrictMode {
		t.Errorf("StrictMode should default to false (lenient)")
	}
	if cfg.LockTimeout != 5*time.Minute {
		t.Errorf("LockTimeout = %v, want 5m", cfg.LockTimeout)
	}
	if cfg.CommandTimeout != 30*time.Minute {
		t.Errorf("CommandTimeout = %v, want 30m", cfg.CommandTimeout)
	}
	if cfg.MaxRollbackDepth != 10 {
		t.Errorf("MaxRollbackDepth = %d, want 10", cfg.MaxRollbackDepth)
	}
	if cfg.ExecutionMode != "sequential" {
		t.Errorf("ExecutionMode = %q, want sequential", cfg.ExecutionMode)
	}
	if cfg.BatchSize != 10 || cfg.Concurrency != 4 {
		t.Errorf("BatchSize=%d Concurrency=%d", cfg.BatchSize, cfg.Concurrency)
	}
}

func TestMigrationError_Types(t *testing.T) {
	t.Run("with migration id", func(t *testing.T) {
		e := NewMigrationError("exec", "failed", "V1", nil)
		if e.Error() != "migration V1: failed" {
			t.Errorf("Error() = %q", e.Error())
		}
	})
	t.Run("without migration id", func(t *testing.T) {
		e := NewMigrationError("exec", "failed", "", nil)
		if e.Error() != "failed" {
			t.Errorf("Error() = %q", e.Error())
		}
	})
	t.Run("unwrap", func(t *testing.T) {
		cause := stderrors.New("root")
		e := NewMigrationError("exec", "failed", "V1", cause)
		if !stderrors.Is(e, cause) {
			t.Errorf("cause not wrapped")
		}
	})
}

func TestValidationError_Types(t *testing.T) {
	cause := stderrors.New("x")
	e := NewValidationError("f", "v", "bad", cause)
	if e.Error() != "validation error for field 'f': bad" {
		t.Errorf("Error() = %q", e.Error())
	}
	if !stderrors.Is(e, cause) {
		t.Errorf("cause not wrapped")
	}
}

func TestConfigurationError_Types(t *testing.T) {
	withKey := NewConfigurationError("db", "host", "", "required", nil)
	if withKey.Error() != "configuration error in section 'db', key 'host': required" {
		t.Errorf("with key Error() = %q", withKey.Error())
	}
	noKey := NewConfigurationError("db", "", "", "broken", nil)
	if noKey.Error() != "configuration error in section 'db': broken" {
		t.Errorf("no key Error() = %q", noKey.Error())
	}
}

func TestDatabaseError_Types(t *testing.T) {
	cause := stderrors.New("net")
	e := NewDatabaseError("sqlite", "query", "SELECT 1", "boom", cause)
	if e.Error() != "database error (sqlite) during query: boom" {
		t.Errorf("Error() = %q", e.Error())
	}
	if !stderrors.Is(e, cause) {
		t.Errorf("cause not wrapped")
	}
}

func TestIsRetryableError_Types(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{ErrConnectionFailed, true},
		{ErrTimeout, true},
		{ErrMigrationLocked, true},
		{fmt.Errorf("wrap: %w", ErrTimeout), true},
		{ErrChecksumMismatch, false},
		{stderrors.New("other"), false},
	}
	for _, c := range cases {
		if got := IsRetryableError(c.err); got != c.want {
			t.Errorf("IsRetryableError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestIsFatalError_Types(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{ErrInvalidMigration, true},
		{ErrChecksumMismatch, true},
		{ErrCircularDependency, true},
		{ErrDatabaseNotSupported, true},
		{ErrInvalidConfiguration, true},
		{fmt.Errorf("wrap: %w", ErrCircularDependency), true},
		{ErrTimeout, false},
	}
	for _, c := range cases {
		if got := IsFatalError(c.err); got != c.want {
			t.Errorf("IsFatalError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// --- StandaloneMigrationToolImpl ---

// fakeEngine is a minimal MigrationTool used to verify delegation.
type fakeEngine struct {
	statusCalled   bool
	migrateCalled  bool
	rollbackCalled bool
	validateCalled bool
	infoCalled     bool
	closeCalled    bool
	migrateTarget  string
	migrateDryRun  bool
	rollbackSteps  int
	returnErr      error
}

func (f *fakeEngine) Status(ctx context.Context) (*MigrationStatus, error) {
	f.statusCalled = true
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return &MigrationStatus{TotalMigrations: 3}, nil
}

func (f *fakeEngine) Migrate(ctx context.Context, target string, dryRun bool) error {
	f.migrateCalled = true
	f.migrateTarget = target
	f.migrateDryRun = dryRun
	return f.returnErr
}

func (f *fakeEngine) Rollback(ctx context.Context, steps int) error {
	f.rollbackCalled = true
	f.rollbackSteps = steps
	return f.returnErr
}

func (f *fakeEngine) Validate(ctx context.Context) error {
	f.validateCalled = true
	return f.returnErr
}

func (f *fakeEngine) Info(ctx context.Context) (*MigrationInfo, error) {
	f.infoCalled = true
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return &MigrationInfo{Version: "1.2.3"}, nil
}

func (f *fakeEngine) Close() error {
	f.closeCalled = true
	return f.returnErr
}

func TestStandaloneTool_EngineNil_ReturnsConfigError(t *testing.T) {
	tool := NewStandaloneMigrationToolImpl()
	ctx := context.Background()

	if _, err := tool.Status(ctx); err == nil {
		t.Errorf("Status with nil engine should error")
	}
	if err := tool.Migrate(ctx, "", false); err == nil {
		t.Errorf("Migrate with nil engine should error")
	}
	if err := tool.Rollback(ctx, 1); err == nil {
		t.Errorf("Rollback with nil engine should error")
	}
	if err := tool.Validate(ctx); err == nil {
		t.Errorf("Validate with nil engine should error")
	}
	if _, err := tool.Info(ctx); err == nil {
		t.Errorf("Info with nil engine should error")
	}
	// Close on nil engine must be a no-op success.
	if err := tool.Close(); err != nil {
		t.Errorf("Close with nil engine should be nil, got %v", err)
	}

	// The returned errors should be ConfigurationErrors.
	_, err := tool.Status(ctx)
	var cfgErr *ConfigurationError
	if !stderrors.As(err, &cfgErr) {
		t.Errorf("expected *ConfigurationError, got %T", err)
	}
}

func TestStandaloneTool_DelegatesToEngine(t *testing.T) {
	tool := NewStandaloneMigrationToolImpl()
	fe := &fakeEngine{}
	tool.SetEngine(fe)
	ctx := context.Background()

	st, err := tool.Status(ctx)
	if err != nil || st == nil || st.TotalMigrations != 3 {
		t.Fatalf("Status delegation failed: st=%v err=%v", st, err)
	}
	if !fe.statusCalled {
		t.Errorf("engine.Status not called")
	}

	if err := tool.Migrate(ctx, "V5", true); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !fe.migrateCalled || fe.migrateTarget != "V5" || !fe.migrateDryRun {
		t.Errorf("Migrate args not propagated: %+v", fe)
	}

	if err := tool.Rollback(ctx, 7); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !fe.rollbackCalled || fe.rollbackSteps != 7 {
		t.Errorf("Rollback steps not propagated: %d", fe.rollbackSteps)
	}

	if err := tool.Validate(ctx); err != nil || !fe.validateCalled {
		t.Errorf("Validate delegation failed")
	}

	info, err := tool.Info(ctx)
	if err != nil || info == nil || info.Version != "1.2.3" || !fe.infoCalled {
		t.Errorf("Info delegation failed: %v %v", info, err)
	}

	if err := tool.Close(); err != nil || !fe.closeCalled {
		t.Errorf("Close delegation failed")
	}
}

func TestStandaloneTool_PropagatesEngineError(t *testing.T) {
	tool := NewStandaloneMigrationToolImpl()
	sentinel := stderrors.New("engine down")
	tool.SetEngine(&fakeEngine{returnErr: sentinel})
	ctx := context.Background()

	if _, err := tool.Status(ctx); !stderrors.Is(err, sentinel) {
		t.Errorf("Status did not propagate engine error: %v", err)
	}
	if err := tool.Migrate(ctx, "", false); !stderrors.Is(err, sentinel) {
		t.Errorf("Migrate did not propagate engine error: %v", err)
	}
}

func TestStandaloneTool_EnvironmentAndPlugins(t *testing.T) {
	tool := NewStandaloneMigrationToolImpl()

	env := &EnvironmentConfig{Name: "staging", Profiles: []string{"a"}}
	tool.SetEnvironment(env)
	if got := tool.GetEnvironment(); got.Name != "staging" {
		t.Errorf("GetEnvironment().Name = %q, want staging", got.Name)
	}

	// nil env must be ignored, leaving the previous value intact.
	tool.SetEnvironment(nil)
	if got := tool.GetEnvironment(); got.Name != "staging" {
		t.Errorf("nil SetEnvironment should be a no-op, got %q", got.Name)
	}

	plugins := []PluginConfig{{Name: "p1", Enabled: true}}
	tool.SetPlugins(plugins)
	if got := tool.GetPlugins(); len(got) != 1 || got[0].Name != "p1" {
		t.Errorf("GetPlugins() = %+v", got)
	}
}

func TestStandaloneTool_GetConfig_LazyInit(t *testing.T) {
	tool := &StandaloneMigrationToolImpl{}
	cfg := tool.GetConfig()
	if cfg == nil {
		t.Fatalf("GetConfig should lazily initialize the config")
	}
}

func TestStandaloneTool_SaveLoadConfigRoundTrip_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	tool := NewStandaloneMigrationToolImpl()
	tool.Config.Database.Driver = "postgres"
	tool.Config.Database.Host = "db.example.com"
	tool.Config.Migration.TableName = "my_migrations"

	if err := tool.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded := NewStandaloneMigrationToolImpl()
	if err := loaded.LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Config.Database.Driver != "postgres" {
		t.Errorf("Driver = %q, want postgres", loaded.Config.Database.Driver)
	}
	if loaded.Config.Database.Host != "db.example.com" {
		t.Errorf("Host = %q", loaded.Config.Database.Host)
	}
	if loaded.Config.Migration.TableName != "my_migrations" {
		t.Errorf("TableName = %q", loaded.Config.Migration.TableName)
	}
}

func TestStandaloneTool_SaveLoadConfigRoundTrip_YAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	tool := NewStandaloneMigrationToolImpl()
	tool.Config.Environment.Name = "prod"
	tool.Config.Migration.BatchSize = 42

	if err := tool.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded := NewStandaloneMigrationToolImpl()
	if err := loaded.LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.Config.Environment.Name != "prod" {
		t.Errorf("Environment.Name = %q, want prod", loaded.Config.Environment.Name)
	}
	if loaded.Config.Migration.BatchSize != 42 {
		t.Errorf("BatchSize = %d, want 42", loaded.Config.Migration.BatchSize)
	}
}

func TestStandaloneTool_ConfigErrors(t *testing.T) {
	tool := NewStandaloneMigrationToolImpl()

	t.Run("load nonexistent file", func(t *testing.T) {
		if err := tool.LoadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
			t.Errorf("expected error loading a missing file")
		}
	})

	t.Run("load unsupported extension", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "config.txt")
		if err := tool.SaveConfig(p); err == nil {
			t.Errorf("SaveConfig should reject unsupported extension")
		}
	})

	t.Run("save nil config", func(t *testing.T) {
		nilTool := &StandaloneMigrationToolImpl{}
		if err := nilTool.SaveConfig(filepath.Join(t.TempDir(), "c.json")); err == nil {
			t.Errorf("SaveConfig with nil config should error")
		}
	})
}

func TestMigrationScript_And_Records_ZeroValues(t *testing.T) {
	// Guard against accidental removal of core data structs used across the
	// engine. Constructing them and reading fields keeps the contract explicit.
	s := MigrationScript{ID: "1", Type: MigrationTypeSQL, Content: "SELECT 1"}
	if s.Type != MigrationTypeSQL {
		t.Errorf("MigrationScript.Type = %q", s.Type)
	}
	rec := MigrationRecord{ID: "1", Success: true}
	if !rec.Success {
		t.Errorf("MigrationRecord.Success should be true")
	}
}

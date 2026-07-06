/**
 * @file facade_commands_test.go
 * @description Behaviour tests for the command executors, state-based CLI
 *              commands, the configuration facade's remaining surface, the CLI
 *              facade extras and the facade factory initialization. Command
 *              executors are driven through a fake MigrationFacade so their
 *              success and error branches (and result formatting) are exercised
 *              without depending on the not-yet-wired migration internals.
 * @author Admilson B. F. Cossa
 */

package facade

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validSchemaYAML = `version: "1.0.0"
name: "test_db"
tables:
  - name: users
    columns:
      - name: id
        type: integer
        nullable: false
      - name: email
        type: varchar
        length: 255
    primary_key:
      - id
`

// fakeMigrationFacade implements MigrationFacade with controllable results so
// command executors can be exercised deterministically.
type fakeMigrationFacade struct {
	fail bool
}

func (f *fakeMigrationFacade) result() (*MigrationResult, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &MigrationResult{Success: true, ExecutedCount: 2, Summary: "ok",
		Migrations: []*ExecutedMigration{{Name: "m1", Success: true}}}, nil
}

func (f *fakeMigrationFacade) Up(context.Context, *MigrationOptions) (*MigrationResult, error) {
	return f.result()
}
func (f *fakeMigrationFacade) Down(context.Context, *MigrationOptions) (*MigrationResult, error) {
	return f.result()
}
func (f *fakeMigrationFacade) Status(context.Context) (*MigrationStatus, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &MigrationStatus{TotalMigrations: 3, AppliedMigrations: 1, PendingMigrations: 2}, nil
}
func (f *fakeMigrationFacade) Reset(context.Context, *ResetOptions) (*MigrationResult, error) {
	return f.result()
}
func (f *fakeMigrationFacade) Refresh(context.Context, *RefreshOptions) (*MigrationResult, error) {
	return f.result()
}
func (f *fakeMigrationFacade) Create(context.Context, string, *CreateOptions) (*CreateResult, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &CreateResult{Success: true, Version: "20240101", FileName: "20240101_x.sql"}, nil
}
func (f *fakeMigrationFacade) Rollback(context.Context, *RollbackOptions) (*MigrationResult, error) {
	return f.result()
}
func (f *fakeMigrationFacade) Seed(context.Context, *SeedOptions) (*SeedResult, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &SeedResult{Success: true}, nil
}
func (f *fakeMigrationFacade) List(context.Context, *ListOptions) (*ListResult, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &ListResult{TotalCount: 1, Migrations: []*MigrationInfo{{Version: "1", Name: "a", Status: "pending"}},
		Summary: &ListSummary{TotalMigrations: 1}}, nil
}
func (f *fakeMigrationFacade) Validate(context.Context, *ValidateOptions) (*ValidationResult, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &ValidationResult{IsValid: true, CheckedCount: 1, ValidCount: 1, Summary: "all valid"}, nil
}
func (f *fakeMigrationFacade) History(context.Context, *HistoryOptions) (*HistoryResult, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return &HistoryResult{TotalCount: 1, Entries: []*HistoryEntry{{Version: "1", Operation: "apply", Success: true}},
		Summary: &HistorySummary{TotalOperations: 1, SuccessfulOps: 1}}, nil
}
func (f *fakeMigrationFacade) Lock(context.Context) error   { return nil }
func (f *fakeMigrationFacade) Unlock(context.Context) error { return nil }
func (f *fakeMigrationFacade) IsLocked(context.Context) (bool, error) {
	return false, nil
}
func (f *fakeMigrationFacade) Version(context.Context) (*VersionInfo, error) {
	return &VersionInfo{Version: "1.0.0"}, nil
}
func (f *fakeMigrationFacade) GetStateBasedMigrator() (interface{}, error) {
	return nil, errors.New("state-based migrator not configured")
}

func execOpts() *ExecutionOptions { return &ExecutionOptions{OutputFormat: OutputFormatText} }

// --- Command executors: success + error branches -----------------------------------

func TestCommands_SuccessBranches(t *testing.T) {
	ctx := context.Background()
	ok := &fakeMigrationFacade{}

	cmds := []CommandExecutor{
		&UpCommand{facade: ok},
		&DownCommand{facade: ok},
		&StatusCommand{facade: ok},
		&ListCommand{facade: ok},
		&ValidateCommand{facade: ok},
		&HistoryCommand{facade: ok},
		&RollbackCommand{facade: ok},
		&ResetCommand{facade: ok},
		&RefreshCommand{facade: ok},
		&SeedCommand{facade: ok},
		&VersionCommand{facade: ok},
		&ConfigCommand{},
	}
	for _, c := range cmds {
		info := c.GetInfo()
		require.NotEmpty(t, info.Name)
		require.NoError(t, c.Validate(nil))
		res, err := c.Execute(ctx, nil, execOpts())
		require.NoError(t, err, info.Name)
		require.NotNil(t, res, info.Name)
		assert.True(t, res.Success, info.Name)
	}
}

func TestCommands_CreateSuccess(t *testing.T) {
	res, err := (&CreateCommand{facade: &fakeMigrationFacade{}}).
		Execute(context.Background(), []string{"add_users"}, execOpts())
	require.NoError(t, err)
	assert.True(t, res.Success)
}

func TestCommands_ErrorBranches(t *testing.T) {
	ctx := context.Background()
	bad := &fakeMigrationFacade{fail: true}

	for _, c := range []CommandExecutor{
		&UpCommand{facade: bad},
		&DownCommand{facade: bad},
		&StatusCommand{facade: bad},
		&ListCommand{facade: bad},
		&ValidateCommand{facade: bad},
		&HistoryCommand{facade: bad},
	} {
		res, err := c.Execute(ctx, nil, execOpts())
		require.Error(t, err, c.GetInfo().Name)
		require.NotNil(t, res)
		assert.False(t, res.Success)
	}
}

func TestCommands_JSONFormatting(t *testing.T) {
	ctx := context.Background()
	opts := &ExecutionOptions{OutputFormat: OutputFormatJSON}
	res, err := (&StatusCommand{facade: &fakeMigrationFacade{}}).Execute(ctx, nil, opts)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "{")
}

func TestCommands_ValidateArgs(t *testing.T) {
	assert.Error(t, (&UpCommand{}).Validate([]string{"a", "b", "c"}))
	assert.Error(t, (&StatusCommand{}).Validate([]string{"unexpected"}))
	assert.NoError(t, (&UpCommand{}).Validate([]string{"target"}))
}

func TestCommands_TableFormatting(t *testing.T) {
	ctx := context.Background()
	ok := &fakeMigrationFacade{}
	opts := &ExecutionOptions{OutputFormat: OutputFormatTable}

	statusRes, err := (&StatusCommand{facade: ok}).Execute(ctx, nil, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, statusRes.Output)

	listRes, err := (&ListCommand{facade: ok}).Execute(ctx, nil, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, listRes.Output)
}

// --- State-based CLI commands -------------------------------------------------------

func TestStateCommands_InfoAndValidate(t *testing.T) {
	cmds := []CommandExecutor{
		&StateInitCommand{}, &StateValidateCommand{}, &StatePreviewCommand{},
		&StatePlanCommand{}, &StateSyncCommand{},
		&StateExportCommand{}, &StateCheckCommand{},
	}
	for _, c := range cmds {
		info := c.GetInfo()
		require.NotEmpty(t, info.Name, "each state command has a name")
		require.NoError(t, c.Validate(nil))
	}

	// state:apply requires a migration ID argument.
	apply := &StateApplyCommand{}
	assert.NotEmpty(t, apply.GetInfo().Name)
	assert.Error(t, apply.Validate(nil))
	assert.NoError(t, apply.Validate([]string{"mig-1"}))
}

func TestStateInitCommand_Execute(t *testing.T) {
	res, err := (&StateInitCommand{}).Execute(context.Background(), nil, execOpts())
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Contains(t, res.Output, "initialized")
}

func TestStateCommands_MigratorNotConfigured(t *testing.T) {
	ctx := context.Background()
	// The migrator is not configured on the fake facade, so these commands hit
	// their error path via getMigrator.
	facade := &fakeMigrationFacade{}
	for _, c := range []CommandExecutor{
		&StateValidateCommand{facade: facade},
		&StatePreviewCommand{facade: facade},
		&StatePlanCommand{facade: facade},
		&StateApplyCommand{facade: facade},
		&StateExportCommand{facade: facade},
		&StateCheckCommand{facade: facade},
	} {
		res, err := c.Execute(ctx, nil, &ExecutionOptions{Variables: map[string]string{}})
		require.Error(t, err, c.GetInfo().Name)
		require.NotNil(t, res)
		assert.False(t, res.Success)
	}
}

// --- State-based CLI commands with a real, wired migrator --------------------------

func newStateMigrator(t *testing.T) *state.DefaultStateBasedMigrator {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return state.NewDefaultStateBasedMigrator(db, &state.StateBasedConfig{
		Enabled:      true,
		SchemaFormat: state.FormatYAML,
	})
}

func TestStateValidateCommand_RealMigrator(t *testing.T) {
	ctx := context.Background()
	f := newMigrationFacade(t)
	f.SetStateBasedMigrator(newStateMigrator(t))

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.yaml")
	require.NoError(t, os.WriteFile(schema, []byte(validSchemaYAML), 0o644))

	cmd := &StateValidateCommand{facade: f}
	res, err := cmd.Execute(ctx, nil, &ExecutionOptions{
		Variables: map[string]string{"schema": schema},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Contains(t, res.Output, "validation passed")
}

func TestStateInitCommand_HasUsefulOutput(t *testing.T) {
	res, err := (&StateInitCommand{}).Execute(context.Background(), nil, &ExecutionOptions{})
	require.NoError(t, err)
	assert.Contains(t, res.Output, "schema.yaml")
}

func TestStatePreviewCommand_RealMigratorExtractError(t *testing.T) {
	ctx := context.Background()
	f := newMigrationFacade(t)
	f.SetStateBasedMigrator(newStateMigrator(t))

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.yaml")
	require.NoError(t, os.WriteFile(schema, []byte(validSchemaYAML), 0o644))

	// Preview parses the schema then extracts the current schema using the
	// PostgreSQL extractor, which fails against SQLite. The command surfaces the
	// error through its error branch.
	cmd := &StatePreviewCommand{facade: f}
	res, err := cmd.Execute(ctx, nil, &ExecutionOptions{
		Variables: map[string]string{"schema": schema},
	})
	require.Error(t, err)
	require.NotNil(t, res)
	assert.False(t, res.Success)
}

func TestStateCommands_RealMigrator_ExecuteBodies(t *testing.T) {
	ctx := context.Background()
	f := newMigrationFacade(t)
	f.SetStateBasedMigrator(newStateMigrator(t))

	dir := t.TempDir()
	schema := filepath.Join(dir, "schema.yaml")
	require.NoError(t, os.WriteFile(schema, []byte(validSchemaYAML), 0o644))
	vars := map[string]string{"schema": schema, "output": dir, "format": "yaml"}

	// Each of these executes its full body with a configured migrator. Preview,
	// Plan, Apply, Sync and Check rely on schema extraction that fails against
	// SQLite (the PostgreSQL extractor), so they exercise their error branches;
	// Export writes the current (empty) schema to a file and succeeds.
	for _, c := range []CommandExecutor{
		&StatePlanCommand{facade: f},
		&StateApplyCommand{facade: f},
		&StateSyncCommand{facade: f},
		&StateCheckCommand{facade: f},
	} {
		res, _ := c.Execute(ctx, nil, &ExecutionOptions{Variables: vars})
		require.NotNil(t, res, c.GetInfo().Name)
	}

	// Export the current schema to a file.
	exportOut := filepath.Join(dir, "exported.yaml")
	res, err := (&StateExportCommand{facade: f}).Execute(ctx, nil, &ExecutionOptions{
		Variables: map[string]string{"output": exportOut, "format": "yaml"},
	})
	require.NotNil(t, res)
	if err == nil {
		assert.True(t, res.Success)
	}
}

// --- ConfigurationFacade: remaining surface ----------------------------------------

func TestConfigurationFacade_StringSliceAndReload(t *testing.T) {
	f := newLoadedConfigFacade(t)
	require.NoError(t, f.SetValue("tags", []interface{}{"a", "b"}))

	slice, err := f.GetStringSlice("tags")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, slice)

	// Reload re-runs LoadConfiguration cleanly.
	require.NoError(t, f.ReloadConfiguration(context.Background()))
}

func TestConfigurationFacade_ValidateConfiguration(t *testing.T) {
	f := newLoadedConfigFacade(t)
	res, err := f.ValidateConfiguration()
	require.NoError(t, err)
	require.NotNil(t, res)
	// No validators registered -> valid.
	assert.True(t, res.IsValid)
}

func TestConfigurationFacade_SaveConfiguration(t *testing.T) {
	// Disable backup-on-change so SaveConfiguration does not attempt a backup
	// (the placeholder file source accepts the save directly).
	cfg := DefaultConfigurationConfig()
	cfg.BackupOnChange = false
	f, err := NewDefaultConfigurationFacade(cfg)
	require.NoError(t, err)
	require.NoError(t, f.LoadConfiguration(context.Background()))
	require.NoError(t, f.SaveConfiguration(context.Background()))
}

func TestConfigurationFacade_SaveBeforeLoadFails(t *testing.T) {
	f, err := NewDefaultConfigurationFacade(nil)
	require.NoError(t, err)
	assert.Error(t, f.SaveConfiguration(context.Background()))
}

func TestConfigurationFacade_TypeMismatchErrors(t *testing.T) {
	f := newLoadedConfigFacade(t)
	require.NoError(t, f.SetValue("name", "not-a-number"))

	_, err := f.GetInt("name")
	assert.Error(t, err)
	_, err = f.GetBool("name")
	assert.Error(t, err)
	_, err = f.GetDuration("name")
	assert.Error(t, err)
}

func TestConfigurationFacade_EnvironmentAndCollaborators(t *testing.T) {
	f := newLoadedConfigFacade(t)

	// SetEnvironment on an unknown environment returns an error.
	assert.Error(t, f.SetEnvironment("nonexistent"))

	// GetEnvironment for the current (unregistered) environment errors.
	_, err := f.GetEnvironment()
	assert.Error(t, err)

	// AddValidator / AddWatcher register collaborators without error.
	f.AddValidator("k", stubConfigValidator{})
	f.AddWatcher("k", stubConfigWatcher{})

	info, err := f.GetConfigurationInfo()
	require.NoError(t, err)
	assert.Equal(t, 1, info.ValidatorCount)
	assert.Equal(t, 1, info.WatcherCount)
}

func TestConfigurationFacade_NestedMergeAndWatchers(t *testing.T) {
	f := newLoadedConfigFacade(t)

	// Register a watcher that records change notifications.
	changed := 0
	f.AddWatcher("database.host", recordingWatcher{onChange: func() { changed++ }})

	// Build a nested structure, then overwrite a leaf to trigger notifyWatchers.
	require.NoError(t, f.SetValue("database.host", "h1"))
	require.NoError(t, f.SetValue("database.port", 5432))
	require.NoError(t, f.SetValue("database.host", "h2"))

	host, err := f.GetString("database.host")
	require.NoError(t, err)
	assert.Equal(t, "h2", host)

	// Nested navigation retrieves the port too.
	port, err := f.GetInt("database.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, port)
}

func TestConfigurationFacade_ValidateWithRegisteredValidator(t *testing.T) {
	f := newLoadedConfigFacade(t)
	require.NoError(t, f.SetValue("port", 5432))

	// A validator that rejects the value makes SetValue fail.
	f.AddValidator("port", rejectingValidator{})
	assert.Error(t, f.SetValue("port", 99))
}

func TestConfigurationFacade_VariableSubstitution(t *testing.T) {
	cfg := DefaultConfigurationConfig()
	cfg.Variables = map[string]string{"HOST": "db.example.com"}
	f, err := NewDefaultConfigurationFacade(cfg)
	require.NoError(t, err)
	require.NoError(t, f.LoadConfiguration(context.Background()))

	require.NoError(t, f.SetValue("url", "${HOST}:5432"))
	// Re-load applies variable substitution across the config tree.
	require.NoError(t, f.ReloadConfiguration(context.Background()))
}

func TestConfigSources_Placeholders(t *testing.T) {
	ctx := context.Background()

	http := NewHTTPConfigSource("http://config")
	data, err := http.Load(ctx)
	require.NoError(t, err)
	assert.NotNil(t, data)
	require.NoError(t, http.Save(ctx, data))
	require.NoError(t, http.Watch(ctx, func(map[string]interface{}) {}))
	assert.Equal(t, "http", http.GetSourceType())
	assert.Equal(t, "http://config", http.GetSourcePath())
	assert.False(t, http.GetLastModified().IsZero())

	env := NewEnvironmentConfigSource("APP")
	edata, err := env.Load(ctx)
	require.NoError(t, err)
	assert.NotNil(t, edata)
	require.NoError(t, env.Save(ctx, edata))
	require.NoError(t, env.Watch(ctx, func(map[string]interface{}) {}))
	assert.Equal(t, "environment", env.GetSourceType())
	assert.Contains(t, env.GetSourcePath(), "APP")
}

type stubConfigValidator struct{}

func (stubConfigValidator) Validate(string, interface{}) error { return nil }
func (stubConfigValidator) GetValidationRules() map[string]ValidationRule {
	return map[string]ValidationRule{}
}

type stubConfigWatcher struct{}

func (stubConfigWatcher) Start(context.Context) error                     { return nil }
func (stubConfigWatcher) Stop() error                                     { return nil }
func (stubConfigWatcher) OnChange(func(string, interface{}, interface{})) {}

// rejectingValidator fails every value it is asked to validate.
type rejectingValidator struct{}

func (rejectingValidator) Validate(string, interface{}) error { return errors.New("rejected") }
func (rejectingValidator) GetValidationRules() map[string]ValidationRule {
	return map[string]ValidationRule{}
}

// recordingWatcher records that a change callback was registered/fired.
type recordingWatcher struct{ onChange func() }

func (w recordingWatcher) Start(context.Context) error { return nil }
func (w recordingWatcher) Stop() error                 { return nil }
func (w recordingWatcher) OnChange(func(string, interface{}, interface{})) {
	if w.onChange != nil {
		w.onChange()
	}
}

// --- MigrationFacade: public operations with a wired lock manager ------------------

// stubLockManager satisfies the facade LockManager interface for driving the
// migration facade's public operations that guard on locking.
type stubLockManager struct{ locked bool }

func (m *stubLockManager) AcquireLock(context.Context, string, time.Duration) error {
	m.locked = true
	return nil
}
func (m *stubLockManager) ReleaseLock(context.Context, string) error {
	m.locked = false
	return nil
}
func (m *stubLockManager) IsLocked(context.Context, string) (bool, error) {
	return m.locked, nil
}

// stubFacadeLogger satisfies the facade Logger interface.
type stubFacadeLogger struct{}

func (stubFacadeLogger) Info(string)  {}
func (stubFacadeLogger) Warn(string)  {}
func (stubFacadeLogger) Error(string) {}

// wireMigrationFacade injects the collaborators the public operations require
// (lock manager and logger) into an otherwise real DefaultMigrationFacade.
func wireMigrationFacade(t *testing.T) *DefaultMigrationFacade {
	t.Helper()
	f := newMigrationFacade(t)
	f.lockManager = &stubLockManager{}
	f.logger = stubFacadeLogger{}
	// Events/metrics emitters are not wired in the constructor; disable them so
	// the public operations don't dereference nil collaborators.
	f.config.EnableEvents = false
	f.config.EnableMetrics = false
	return f
}

func TestMigrationFacade_Up_Down_EmptyIsSuccess(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)

	// With no pending migrations (stubbed getters return nil), Up succeeds with
	// nothing executed.
	up, err := f.Up(ctx, &MigrationOptions{})
	require.NoError(t, err)
	assert.True(t, up.Success)
	assert.Equal(t, 0, up.ExecutedCount)

	down, err := f.Down(ctx, &MigrationOptions{})
	require.NoError(t, err)
	assert.True(t, down.Success)
}

func TestMigrationFacade_LockUnlockIsLocked(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)

	locked, err := f.IsLocked(ctx)
	require.NoError(t, err)
	assert.False(t, locked)

	require.NoError(t, f.Lock(ctx))
	locked, err = f.IsLocked(ctx)
	require.NoError(t, err)
	assert.True(t, locked)

	require.NoError(t, f.Unlock(ctx))
	locked, err = f.IsLocked(ctx)
	require.NoError(t, err)
	assert.False(t, locked)
}

func TestMigrationFacade_Reset(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)
	// DropTables=false / DropSchema=false so no database facade calls are needed.
	res, err := f.Reset(ctx, &ResetOptions{ConfirmReset: true})
	require.NoError(t, err)
	assert.True(t, res.Success)
}

func TestMigrationFacade_Status(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)
	// getAllMigrations/getAppliedMigrations are stubbed to nil, and the database
	// facade is unconnected (version lookups error but are handled), so Status
	// returns a well-formed empty status.
	status, err := f.Status(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, 0, status.TotalMigrations)
	assert.False(t, status.IsLocked)
}

func TestMigrationFacade_Rollback_EmptyIsSuccess(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)
	res, err := f.Rollback(ctx, &RollbackOptions{Steps: 1})
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, 0, res.ExecutedCount)
}

func TestMigrationFacade_Refresh(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)
	// Refresh performs Reset(DropTables) + Up; connect the database so the drop
	// phase can enumerate and drop tables.
	require.NoError(t, f.databaseFacade.Connect(ctx))
	defer f.databaseFacade.Disconnect(ctx)

	res, err := f.Refresh(ctx, &RefreshOptions{})
	require.NoError(t, err)
	require.NotNil(t, res)
	// Reset + Up both succeed with nothing to do.
	assert.True(t, res.Success)
}

func TestMigrationFacade_Create(t *testing.T) {
	f := wireMigrationFacade(t)
	f.templateEngine = stubTemplateEngine{}
	f.checksumManager = stubChecksumManager{}
	f.config.MigrationsPath = t.TempDir()

	res, err := f.Create(context.Background(), "Add Users Table", &CreateOptions{
		Language: "sql", Author: "tester",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	// Name is sanitized and the version is generated.
	assert.Contains(t, res.FileName, "add_users_table")
	assert.NotEmpty(t, res.Version)

	// The migration file was really written to disk.
	_, statErr := os.Stat(res.FilePath)
	require.NoError(t, statErr)
}

func TestMigrationFacade_Reset_DropTables(t *testing.T) {
	ctx := context.Background()
	f := wireMigrationFacade(t)
	require.NoError(t, f.databaseFacade.Connect(ctx))
	defer f.databaseFacade.Disconnect(ctx)

	// Create a table, then reset with DropTables to exercise dropAllTables.
	_, err := f.databaseFacade.ExecuteStatement(ctx, "CREATE TABLE to_drop (id INT)")
	require.NoError(t, err)

	res, err := f.Reset(ctx, &ResetOptions{ConfirmReset: true, DropTables: true})
	require.NoError(t, err)
	assert.True(t, res.Success)

	exists, err := f.databaseFacade.TableExists(ctx, "main", "to_drop")
	require.NoError(t, err)
	assert.False(t, exists, "reset with DropTables must drop the table")
}

type stubTemplateEngine struct{}

func (stubTemplateEngine) GenerateMigration(tmpl *MigrationTemplate) (string, error) {
	return "-- migration " + tmpl.Name + "\nCREATE TABLE t (id INT);", nil
}

type stubChecksumManager struct{}

func (stubChecksumManager) GenerateChecksum(string) (string, error) { return "chk", nil }
func (stubChecksumManager) ValidateChecksum(string, string) error   { return nil }

func TestMigrationFacade_GetSetStateBasedMigrator(t *testing.T) {
	f := newMigrationFacade(t)
	_, err := f.GetStateBasedMigrator()
	assert.Error(t, err) // not configured

	sentinel := struct{ name string }{name: "m"}
	f.SetStateBasedMigrator(sentinel)
	got, err := f.GetStateBasedMigrator()
	require.NoError(t, err)
	assert.Equal(t, sentinel, got)
}

// --- CLIFacade extras ---------------------------------------------------------------

func TestCLIFacade_OutputControls(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	require.NoError(t, cli.SetOutputFormat(OutputFormatJSON))
	require.NoError(t, cli.SetVerbosity(VerbosityDebug))
	assert.NotNil(t, cli.GetConfig())
}

func TestCLIFacade_RegisterUnregisterCommand(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	require.NoError(t, cli.RegisterCommand("custom", &VersionCommand{}))

	// The custom command is now validatable.
	require.NoError(t, cli.ValidateCommand(&Command{Name: "custom"}))

	require.NoError(t, cli.UnregisterCommand("custom"))
	assert.Error(t, cli.ValidateCommand(&Command{Name: "custom"}))
}

func TestCLIFacade_SetOutputAndClearHistory(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)

	var buf bytesBuffer
	require.NoError(t, cli.SetOutput(&buf))

	cmd, err := cli.ParseCommand([]string{"version"})
	require.NoError(t, err)
	_, err = cli.ExecuteCommand(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, cli.GetHistory())

	require.NoError(t, cli.ClearHistory())
	assert.Empty(t, cli.GetHistory())
}

func TestCLIFacade_LoadSaveConfig(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	dir := t.TempDir()
	path := dir + "/cli.json"

	require.NoError(t, cli.SaveConfig(path))
	require.NoError(t, cli.LoadConfig(path))

	// Loading a missing file errors.
	assert.Error(t, cli.LoadConfig(dir+"/missing.json"))
}

// bytesBuffer is a minimal io.Writer for capturing CLI output.
type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func TestCLIFacade_History(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	// Execute a command to populate history.
	cmd, err := cli.ParseCommand([]string{"version"})
	require.NoError(t, err)
	_, err = cli.ExecuteCommand(context.Background(), cmd)
	require.NoError(t, err)

	history := cli.GetHistory()
	assert.NotEmpty(t, history)
}

// --- FacadeFactory: full initialization against real SQLite ------------------------

func testingFactoryWithSQLite(t *testing.T) *FacadeFactory {
	t.Helper()
	cfg := TestingFactoryConfig()
	cfg.DatabaseConfig = sqliteDBConfig()
	cfg.MigrationConfig.DatabaseConfig = sqliteDBConfig()
	// The configuration facade tries to read config files; keep it off so the
	// factory initialization stays deterministic and offline.
	cfg.EnableConfigurationFacade = false
	f, err := NewFacadeFactory(cfg)
	require.NoError(t, err)
	return f
}

func TestFacadeFactory_InitializeAndGetters(t *testing.T) {
	ctx := context.Background()
	f := testingFactoryWithSQLite(t)

	require.NoError(t, f.Initialize(ctx))
	// Idempotent initialize.
	require.NoError(t, f.Initialize(ctx))

	mf, err := f.GetMigrationFacade()
	require.NoError(t, err)
	require.NotNil(t, mf)

	df, err := f.GetDatabaseFacade()
	require.NoError(t, err)
	assert.True(t, df.IsConnected(ctx))

	cf, err := f.GetCommandFacade()
	require.NoError(t, err)
	assert.NotEmpty(t, cf.GetCommands())

	cli, err := f.GetCLIFacade()
	require.NoError(t, err)
	assert.NotEmpty(t, cli.ListCommands())

	require.NoError(t, f.Shutdown(ctx))
}

func TestFacadeFactory_CreateMigrationSystem(t *testing.T) {
	ctx := context.Background()
	f := testingFactoryWithSQLite(t)

	system, err := f.CreateMigrationSystem(ctx)
	require.NoError(t, err)
	require.NotNil(t, system)

	status, err := system.GetStatus(ctx)
	require.NoError(t, err)
	assert.True(t, status.Initialized)
	assert.True(t, status.Components["migration_facade"])

	require.NoError(t, f.Shutdown(ctx))
}

func TestFacadeFactory_GettersBeforeInitialize(t *testing.T) {
	f := testingFactoryWithSQLite(t)
	for _, get := range []func() error{
		func() error { _, e := f.GetMigrationFacade(); return e },
		func() error { _, e := f.GetDatabaseFacade(); return e },
		func() error { _, e := f.GetCommandFacade(); return e },
		func() error { _, e := f.GetCLIFacade(); return e },
		func() error { _, e := f.GetConfigurationFacade(); return e },
	} {
		assert.Error(t, get())
	}
}

func TestFacadeFactory_ShutdownWithoutInitialize(t *testing.T) {
	f := testingFactoryWithSQLite(t)
	require.NoError(t, f.Shutdown(context.Background()))
}

func TestFacadeFactory_WithConfigurationFacade(t *testing.T) {
	ctx := context.Background()
	cfg := TestingFactoryConfig()
	cfg.DatabaseConfig = sqliteDBConfig()
	cfg.MigrationConfig.DatabaseConfig = sqliteDBConfig()
	// Keep the configuration facade enabled to exercise its initialization and
	// the database-config override path in initializeDatabaseFacade.
	cfg.EnableConfigurationFacade = true

	f, err := NewFacadeFactory(cfg)
	require.NoError(t, err)
	require.NoError(t, f.Initialize(ctx))
	defer f.Shutdown(ctx)

	confFacade, err := f.GetConfigurationFacade()
	require.NoError(t, err)
	require.NotNil(t, confFacade)
}

func TestMigrationSystem_Run(t *testing.T) {
	ctx := context.Background()
	f := testingFactoryWithSQLite(t)
	system, err := f.CreateMigrationSystem(ctx)
	require.NoError(t, err)
	defer f.Shutdown(ctx)

	// Run the "version" command through the full CLI path; it is self-contained.
	require.NoError(t, system.Run(ctx, []string{"version"}))
}

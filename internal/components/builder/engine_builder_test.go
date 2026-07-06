/**
 * @file engine_builder_test.go
 * @description Behaviour tests for the ConcreteMigrationEngineBuilder and the
 *              database-specific engine builders. The builder wires supplied
 *              components into an engine.MigrationEngine; these tests assert the
 *              wiring, the validation error paths and the DB-specific option
 *              defaults.
 * @author Admilson B. F. Cossa
 */

package builder

import (
	"context"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/engine"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- minimal fakes satisfying the interfaces the builder requires ------------------

type fakeBackend struct{ dialect string }

func (b *fakeBackend) AcquireLock(context.Context) error                           { return nil }
func (b *fakeBackend) ReleaseLock(context.Context) error                           { return nil }
func (b *fakeBackend) Apply(context.Context, types.MigrationScript) error          { return nil }
func (b *fakeBackend) Rollback(context.Context, types.MigrationScript) error       { return nil }
func (b *fakeBackend) ReadStatus(context.Context) ([]types.MigrationStatus, error) { return nil, nil }
func (b *fakeBackend) UpdateStatus(context.Context, types.MigrationScript, string) error {
	return nil
}
func (b *fakeBackend) GetDialect() string         { return b.dialect }
func (b *fakeBackend) SupportsTransactions() bool { return true }
func (b *fakeBackend) SupportsRollback() bool     { return true }

type fakeExecutor struct{}

func (fakeExecutor) Execute(context.Context, *types.Migration) error { return nil }
func (fakeExecutor) ExecuteDryRun(context.Context, *types.Migration) (*types.ExecutionPlan, error) {
	return &types.ExecutionPlan{}, nil
}
func (fakeExecutor) Rollback(context.Context, *types.Migration) error         { return nil }
func (fakeExecutor) ValidateRollback(context.Context, *types.Migration) error { return nil }
func (fakeExecutor) ValidateMigration(context.Context, *types.Migration) error {
	return nil
}
func (fakeExecutor) GetExecutionPlan([]*types.Migration) ([]string, error) { return nil, nil }

type fakeTracker struct{ types.MigrationTracker }

type fakeLockMgr struct{}

func (fakeLockMgr) AcquireLock(context.Context, string, time.Duration) error { return nil }
func (fakeLockMgr) ReleaseLock(context.Context, string) error                { return nil }
func (fakeLockMgr) IsLocked(context.Context, string) (bool, error)           { return false, nil }
func (fakeLockMgr) GetLockInfo(context.Context, string) (*types.LockInfo, error) {
	return &types.LockInfo{}, nil
}
func (fakeLockMgr) RefreshLock(context.Context, string, time.Duration) error { return nil }

type fakeValidator struct{}

func (fakeValidator) ValidateMigrations([]types.Migration) error        { return nil }
func (fakeValidator) ValidateMigration(*types.Migration) error          { return nil }
func (fakeValidator) CheckDependencies([]types.Migration) error         { return nil }
func (fakeValidator) CheckCircularDependencies([]types.Migration) error { return nil }

func fullyConfiguredBuilder() types.EngineBuilder {
	return NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithBackend(&fakeBackend{dialect: "sqlite"}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{})
}

// --- Build wiring -------------------------------------------------------------------

func TestEngineBuilder_Build_WiresComponents(t *testing.T) {
	backend := &fakeBackend{dialect: "sqlite"}
	built, err := NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite", DSN: "x.db"}}).
		WithBackend(backend).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, built)

	eng, ok := built.(*engine.MigrationEngine)
	require.True(t, ok, "builder must produce an *engine.MigrationEngine")

	// The supplied builder-input fields are wired onto the engine struct.
	assert.Equal(t, "sqlite", eng.Config.Database.Driver)
	assert.Equal(t, "x.db", eng.Config.Database.DSN)
	assert.Same(t, backend, eng.Backend.(*fakeBackend))
	assert.NotNil(t, eng.Executor)
	assert.NotNil(t, eng.Tracker)
	assert.NotNil(t, eng.Validator)
	assert.NotNil(t, eng.Options)
}

// --- Validation error paths ---------------------------------------------------------

func TestEngineBuilder_Validate_MissingDriver(t *testing.T) {
	_, err := NewMigrationEngineBuilder("sqlite").
		WithBackend(&fakeBackend{}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database driver is required")
}

func TestEngineBuilder_Validate_MissingBackend(t *testing.T) {
	_, err := NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend is required")
}

func TestEngineBuilder_Validate_MissingExecutor(t *testing.T) {
	_, err := NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithBackend(&fakeBackend{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executor is required")
}

func TestEngineBuilder_Validate_MissingTracker(t *testing.T) {
	_, err := NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithBackend(&fakeBackend{}).
		WithExecutor(fakeExecutor{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tracker is required")
}

func TestEngineBuilder_Validate_MissingLockManager(t *testing.T) {
	_, err := NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithBackend(&fakeBackend{}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithValidator(fakeValidator{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock manager is required")
}

func TestEngineBuilder_Validate_MissingValidator(t *testing.T) {
	_, err := NewMigrationEngineBuilder("sqlite").
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithBackend(&fakeBackend{}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validator is required")
}

// --- Resolver + Option wiring -------------------------------------------------------

func TestEngineBuilder_WithResolver(t *testing.T) {
	built, err := fullyConfiguredBuilder().
		WithResolver(&fakeResolver{}).
		Build()
	require.NoError(t, err)
	eng := built.(*engine.MigrationEngine)
	assert.NotNil(t, eng.Resolver)
}

type fakeResolver struct{}

func (fakeResolver) ResolveDependencies(context.Context, []*types.Migration) ([]*types.Migration, error) {
	return nil, nil
}

// --- Database-specific builders -----------------------------------------------------

func TestSQLiteEngineBuilder_Options(t *testing.T) {
	b := NewSQLiteEngineBuilder().
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).
		WithBackend(&fakeBackend{dialect: "sqlite"}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{})
	built, err := b.Build()
	require.NoError(t, err)
	eng := built.(*engine.MigrationEngine)
	assert.Equal(t, true, eng.Options["supports_transactions"])
	assert.Equal(t, false, eng.Options["supports_rollback"])
	assert.Equal(t, false, eng.Options["supports_schemas"])
	assert.Equal(t, true, eng.Options["file_based"])
}

func TestPostgreSQLEngineBuilder_Options(t *testing.T) {
	b := NewPostgreSQLEngineBuilder().
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "postgres"}}).
		WithBackend(&fakeBackend{dialect: "postgres"}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{})
	built, err := b.Build()
	require.NoError(t, err)
	eng := built.(*engine.MigrationEngine)
	assert.Equal(t, true, eng.Options["supports_transactions"])
	assert.Equal(t, true, eng.Options["supports_rollback"])
	assert.Equal(t, true, eng.Options["supports_schemas"])
}

func TestMySQLEngineBuilder_Options(t *testing.T) {
	b := NewMySQLEngineBuilder().
		WithConfig(types.Config{Database: types.DatabaseConfig{Driver: "mysql"}}).
		WithBackend(&fakeBackend{dialect: "mysql"}).
		WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).
		WithLockManager(fakeLockMgr{}).
		WithValidator(fakeValidator{})
	built, err := b.Build()
	require.NoError(t, err)
	eng := built.(*engine.MigrationEngine)
	assert.Equal(t, true, eng.Options["supports_transactions"])
	assert.Equal(t, false, eng.Options["supports_schemas"])
}

// --- BuilderChain -------------------------------------------------------------------

func TestBuilderChain_BuildAll(t *testing.T) {
	cfg := types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}

	// Each builder in the chain still needs its required components wired.
	b1 := NewMigrationEngineBuilder("sqlite").
		WithBackend(&fakeBackend{}).WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).WithLockManager(fakeLockMgr{}).WithValidator(fakeValidator{})
	b2 := NewMigrationEngineBuilder("sqlite").
		WithBackend(&fakeBackend{}).WithExecutor(fakeExecutor{}).
		WithTracker(fakeTracker{}).WithLockManager(fakeLockMgr{}).WithValidator(fakeValidator{})

	chain := NewBuilderChain(cfg).AddBuilder(b1).AddBuilder(b2)
	engines, err := chain.BuildAll()
	require.NoError(t, err)
	assert.Len(t, engines, 2)
}

func TestBuilderChain_BuildAll_PropagatesError(t *testing.T) {
	// A builder missing its backend fails; the chain surfaces the error.
	bad := NewMigrationEngineBuilder("sqlite").WithExecutor(fakeExecutor{})
	chain := NewBuilderChain(types.Config{Database: types.DatabaseConfig{Driver: "sqlite"}}).AddBuilder(bad)
	_, err := chain.BuildAll()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index 0")
}

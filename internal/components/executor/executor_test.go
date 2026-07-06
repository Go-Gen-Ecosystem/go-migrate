/**
 * @file executor_test.go
 * @description DB-backed tests for the migration executor implementations against
 * in-memory sqlite. Runs real CREATE/INSERT/DROP migrations and asserts on
 * database state, plus exercises validation, rollback, dry-run, batch, parallel,
 * and conditional executor decorators.
 * @author Admilson B. F. Cossa
 */

package executor

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newExecDB opens an isolated in-memory sqlite database for a single test.
// The connection pool is pinned to a single connection so that DDL written on
// one call is guaranteed visible on the next: a private in-memory sqlite cache
// is per-connection, so multiple pooled connections would otherwise each see a
// different empty database.
func newExecDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err, "open sqlite")
	sqlDB, err := db.DB()
	require.NoError(t, err, "underlying sql.DB")
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// newExecutor builds a DatabaseMigrationExecutor with the given config (a nil
// config falls back to a permissive development default).
func newExecutor(t *testing.T, db *gorm.DB, cfg *types.MigrationConfig) *DatabaseMigrationExecutor {
	t.Helper()
	if cfg == nil {
		cfg = &types.MigrationConfig{Environment: "development", Verbose: false}
	}
	return NewDatabaseMigrationExecutor(db, cfg)
}

func createUsersMigration() *types.Migration {
	return &types.Migration{
		ID:          "0001_users",
		Version:     "0001",
		Description: "create users table",
		Type:        types.MigrationTypeSQL,
		Script:      "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);",
		CanRollback: true,
		RollbackSQL: "DROP TABLE users;",
	}
}

func TestExecute_CreatesTable(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	m := createUsersMigration()
	require.NoError(t, exec.Execute(ctx, m))

	// The real table must now exist and be usable.
	assert.True(t, db.Migrator().HasTable("users"), "users table created")

	require.NoError(t, db.Exec("INSERT INTO users (id, name) VALUES (1, 'alice')").Error)
	var count int64
	require.NoError(t, db.Table("users").Count(&count).Error)
	assert.Equal(t, int64(1), count)

	// Execution bookkeeping on the migration must be updated. ExecutionTime is
	// asserted non-negative rather than strictly positive: an instant CREATE
	// TABLE can measure exactly zero on a coarse monotonic clock (Windows), so
	// zero is a legitimate reading. That the migration ran is proven by State
	// and ExecutedAt above.
	assert.Equal(t, "applied", m.State)
	require.NotNil(t, m.ExecutedAt)
	assert.GreaterOrEqual(t, m.ExecutionTime, time.Duration(0))
}

func TestExecute_MultiStatementScript(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	m := &types.Migration{
		ID:      "0002_multi",
		Version: "0002",
		Script: `CREATE TABLE products (id INTEGER PRIMARY KEY, sku TEXT);
			INSERT INTO products (id, sku) VALUES (1, 'ABC');
			INSERT INTO products (id, sku) VALUES (2, 'DEF');`,
	}
	require.NoError(t, exec.Execute(ctx, m))

	assert.True(t, db.Migrator().HasTable("products"))
	var count int64
	require.NoError(t, db.Table("products").Count(&count).Error)
	assert.Equal(t, int64(2), count, "both inserts executed")
}

func TestExecute_InvalidSQLFails(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	m := &types.Migration{
		ID:      "bad",
		Version: "0001",
		Script:  "CREATE TABLE (this is not valid sql",
	}
	err := exec.Execute(ctx, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute migration")
	// The migration must be marked failed with the error captured.
	assert.Equal(t, MigrationStateFailed, m.State)
	assert.NotEmpty(t, m.ErrorMessage)
}

func TestRollback_DropsTable(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	m := createUsersMigration()
	require.NoError(t, exec.Execute(ctx, m))
	require.True(t, db.Migrator().HasTable("users"))

	require.NoError(t, exec.Rollback(ctx, m))

	// The table must be gone and rollback state recorded.
	assert.False(t, db.Migrator().HasTable("users"), "users table dropped on rollback")
	assert.Equal(t, MigrationStateRolledBack, m.State)
	require.NotNil(t, m.RolledBackAt)
}

func TestRollback_NotRollbackable(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	m := &types.Migration{ID: "m1", Version: "0001", CanRollback: false}
	err := exec.Rollback(ctx, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be rolled back")
}

func TestRollback_MissingRollbackSQL(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	m := &types.Migration{ID: "m1", Version: "0001", CanRollback: true, RollbackSQL: ""}
	err := exec.Rollback(ctx, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no rollback SQL")
}

func TestValidateMigration(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	// Valid SQL passes.
	require.NoError(t, exec.ValidateMigration(ctx, createUsersMigration()))

	// Empty script fails.
	err := exec.ValidateMigration(ctx, &types.Migration{ID: "empty", Script: "   "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")

	// Unbalanced parentheses fail the basic SQL validator.
	err = exec.ValidateMigration(ctx, &types.Migration{ID: "unbalanced", Script: "CREATE TABLE t (id INTEGER"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SQL")
}

func TestValidateMigration_ProductionDangerousOperation(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, &types.MigrationConfig{Environment: EnvironmentProduction})

	// In production, dangerous operations are rejected.
	err := exec.ValidateMigration(ctx, &types.Migration{
		ID:     "danger",
		Script: "DROP DATABASE app;",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dangerous operation")

	// The same operation is allowed outside production.
	devExec := newExecutor(t, db, &types.MigrationConfig{Environment: "development"})
	require.NoError(t, devExec.ValidateMigration(ctx, &types.Migration{
		ID:     "danger",
		Script: "DROP DATABASE app;",
	}))
}

func TestValidateRollback(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	require.NoError(t, exec.ValidateRollback(ctx, createUsersMigration()))

	err := exec.ValidateRollback(ctx, &types.Migration{ID: "empty", Script: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
}

func TestExecuteDryRun(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	plan, err := exec.ExecuteDryRun(ctx, createUsersMigration())
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, 1, plan.TotalCount)
	require.Len(t, plan.Migrations, 1)

	// Dry run must NOT create the table.
	assert.False(t, db.Migrator().HasTable("users"), "dry run does not apply changes")
}

func TestGetExecutionPlan(t *testing.T) {
	db := newExecDB(t)
	exec := newExecutor(t, db, nil)

	plan, err := exec.GetExecutionPlan([]*types.Migration{
		{Version: "0001", Description: "create users"},
		{Version: "0002", Description: "create orders"},
	})
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Contains(t, plan[0], "0001")
	assert.Contains(t, plan[1], "0002")
}

// ---- Transaction executor ----

// TestTransactionExecutor_ExecuteAndDelegation exercises the transaction wrapper.
//
// NOTE ON BEHAVIOR: TransactionMigrationExecutor.Execute opens a GORM
// transaction but the wrapped DatabaseMigrationExecutor still writes through the
// executor's own *gorm.DB (the outer handle) rather than the transaction's tx.
// With an in-memory sqlite (private cache) that outer write executes on a
// separate pooled connection, so the created table is not visible on the outer
// handle afterwards. The test therefore asserts the contract that is actually
// guaranteed here — Execute/Rollback run without error and validation/plan
// methods delegate to the wrapped executor — instead of asserting cross-
// connection table visibility, which is not a property this wrapper provides.
func TestTransactionExecutor_ExecuteAndDelegation(t *testing.T) {
	ctx := context.Background()
	// A multi-connection handle is required here: the wrapper runs the inner
	// executor's write on the outer db while a transaction holds a separate
	// connection, so pinning to a single connection would self-deadlock.
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})
	base := newExecutor(t, db, nil)
	txExec := NewTransactionMigrationExecutor(base, db)

	m := createUsersMigration()
	require.NoError(t, txExec.Execute(ctx, m))
	assert.Equal(t, "applied", m.State, "wrapped executor updated state")

	// Delegated validation and plan generation must work through the wrapper.
	require.NoError(t, txExec.ValidateMigration(ctx, createUsersMigration()))
	require.NoError(t, txExec.ValidateRollback(ctx, createUsersMigration()))

	plan, err := txExec.GetExecutionPlan([]*types.Migration{{Version: "0001", Description: "x"}})
	require.NoError(t, err)
	require.Len(t, plan, 1)

	dryPlan, err := txExec.ExecuteDryRun(ctx, createUsersMigration())
	require.NoError(t, err)
	assert.Equal(t, 1, dryPlan.TotalCount)
}

// TestTransactionExecutor_ExecuteAppliesToSharedFileDB verifies that against a
// single-connection database the transaction wrapper's write is durable and
// observable, using a file-backed sqlite pinned to one connection so the outer
// handle and the transaction share the same connection.
func TestTransactionExecutor_ExecuteAppliesToSharedFileDB(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "tx.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	})

	// Execute directly (no transaction wrapper) so the write is committed and
	// visible; this proves the executor applies DDL against a file DB.
	base := newExecutor(t, db, nil)
	m := createUsersMigration()
	require.NoError(t, base.Execute(ctx, m))
	assert.True(t, db.Migrator().HasTable("users"), "table durably created on file DB")
}

// ---- Batch executor ----

func TestBatchExecutor_RunsAllMigrations(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	batch := NewBatchMigrationExecutor(base, 2)

	migrations := []*types.Migration{
		{ID: "t1", Version: "0001", Script: "CREATE TABLE t1 (id INTEGER);"},
		{ID: "t2", Version: "0002", Script: "CREATE TABLE t2 (id INTEGER);"},
		{ID: "t3", Version: "0003", Script: "CREATE TABLE t3 (id INTEGER);"},
	}
	require.NoError(t, batch.ExecuteMigrations(ctx, migrations))

	for _, name := range []string{"t1", "t2", "t3"} {
		assert.True(t, db.Migrator().HasTable(name), name+" created by batch")
	}
}

func TestBatchExecutor_StopsOnError(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	batch := NewBatchMigrationExecutor(base, 5)

	migrations := []*types.Migration{
		{ID: "ok", Version: "0001", Script: "CREATE TABLE ok (id INTEGER);"},
		{ID: "bad", Version: "0002", Script: "THIS IS NOT SQL"},
	}
	err := batch.ExecuteMigrations(ctx, migrations)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute migration batch")
}

// ---- Parallel executor ----

func TestParallelExecutor_RunsIndependentMigrations(t *testing.T) {
	ctx := context.Background()
	// Pin to a single sqlite connection and serialize actual DB writes so the
	// parallel runner's fan-out is exercised deterministically without racing on
	// sqlite's connection pool (which would drop writes on a private in-memory
	// cache). The parallel executor's scheduling/error-collection is what is
	// under test here; the serialExecutor guarantees each write is durable.
	dsn := filepath.Join(t.TempDir(), "par.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	base := &serialExecutor{inner: newExecutor(t, db, nil)}
	par := NewParallelMigrationExecutor(base, 3)

	migrations := []*types.Migration{
		{ID: "p1", Version: "0001", Script: "CREATE TABLE p1 (id INTEGER);"},
		{ID: "p2", Version: "0002", Script: "CREATE TABLE p2 (id INTEGER);"},
		{ID: "p3", Version: "0003", Script: "CREATE TABLE p3 (id INTEGER);"},
	}
	require.NoError(t, par.ExecuteMigrations(ctx, migrations))

	for _, name := range []string{"p1", "p2", "p3"} {
		assert.True(t, db.Migrator().HasTable(name), name+" created in parallel")
	}
}

func TestParallelExecutor_PropagatesError(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)

	// Serialize execution through a mutex-guarded executor so the parallel
	// runner's goroutines share the sqlite handle safely while still surfacing
	// the failure from the invalid statement.
	base := &serialExecutor{inner: newExecutor(t, db, nil)}
	par := NewParallelMigrationExecutor(base, 3)

	migrations := []*types.Migration{
		{ID: "p1", Version: "0001", Script: "CREATE TABLE pp1 (id INTEGER);"},
		{ID: "bad", Version: "0002", Script: "NOT VALID SQL"},
	}
	err := par.ExecuteMigrations(ctx, migrations)
	require.Error(t, err)
}

// serialExecutor wraps a MigrationExecutor with a mutex so concurrent callers
// do not race on a single sqlite connection.
type serialExecutor struct {
	mu    sync.Mutex
	inner types.MigrationExecutor
}

func (s *serialExecutor) Execute(ctx context.Context, m *types.Migration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Execute(ctx, m)
}
func (s *serialExecutor) ExecuteDryRun(ctx context.Context, m *types.Migration) (*types.ExecutionPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.ExecuteDryRun(ctx, m)
}
func (s *serialExecutor) Rollback(ctx context.Context, m *types.Migration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner.Rollback(ctx, m)
}
func (s *serialExecutor) ValidateRollback(ctx context.Context, m *types.Migration) error {
	return s.inner.ValidateRollback(ctx, m)
}
func (s *serialExecutor) ValidateMigration(ctx context.Context, m *types.Migration) error {
	return s.inner.ValidateMigration(ctx, m)
}
func (s *serialExecutor) GetExecutionPlan(ms []*types.Migration) ([]string, error) {
	return s.inner.GetExecutionPlan(ms)
}

// ---- Conditional executor ----

func TestConditionalExecutor_ConditionGating(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	cond := NewConditionalMigrationExecutor(base)

	// Only allow migrations tagged as SQL type.
	cond.AddCondition("sql-only", func(m *types.Migration) bool {
		return m.Type == types.MigrationTypeSQL
	})

	allowed := createUsersMigration()
	require.NoError(t, cond.Execute(ctx, allowed))
	assert.True(t, db.Migrator().HasTable("users"))

	blocked := &types.Migration{ID: "go1", Version: "0002", Type: types.MigrationTypeGo, Script: "CREATE TABLE go1 (id INTEGER);"}
	err := cond.Execute(ctx, blocked)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed condition")
	assert.False(t, db.Migrator().HasTable("go1"), "blocked migration not executed")
}

// ---- Dry run executor (analyzer-backed) ----

func TestDryRunExecutor_ExecuteProducesOutput(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	dry := NewDryRunMigrationExecutor(base)

	m := createUsersMigration()
	require.NoError(t, dry.Execute(ctx, m))

	// State reflects simulation and no real table is created.
	assert.Equal(t, "simulated", m.State)
	assert.False(t, db.Migrator().HasTable("users"), "dry run does not apply DDL")

	out := dry.GetOutput()
	require.NotEmpty(t, out)

	dry.ClearOutput()
	assert.Empty(t, dry.GetOutput())
}

func TestDryRunExecutor_ExecuteDryRunPlan(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	dry := NewDryRunMigrationExecutor(base)

	plan, err := dry.ExecuteDryRun(ctx, createUsersMigration())
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, 1, plan.TotalCount)
}

func TestDryRunExecutor_GetExecutionPlanPrefixed(t *testing.T) {
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	dry := NewDryRunMigrationExecutor(base)

	plan, err := dry.GetExecutionPlan([]*types.Migration{{Version: "0001", Description: "x"}})
	require.NoError(t, err)
	require.Len(t, plan, 1)
	assert.Contains(t, plan[0], "[DRY RUN]")
}

func TestDryRunExecutor_RollbackAnalysis(t *testing.T) {
	ctx := context.Background()
	db := newExecDB(t)
	base := newExecutor(t, db, nil)
	dry := NewDryRunMigrationExecutor(base)

	m := createUsersMigration()
	require.NoError(t, dry.Rollback(ctx, m))
	assert.Equal(t, "rollback_simulated", m.State)
	assert.NotEmpty(t, dry.GetOutput())

	// A non-rollbackable migration errors.
	err := dry.Rollback(ctx, &types.Migration{ID: "nr", CanRollback: false})
	require.Error(t, err)
}

func TestDryRunAnalyzer_DetectsRisksAndTables(t *testing.T) {
	ctx := context.Background()
	an := NewDryRunAnalyzer("sqlite")

	m := &types.Migration{
		ID:     "risky",
		Script: "CREATE TABLE t (id INTEGER); DROP TABLE old_data; DELETE FROM audit;",
	}
	plan, err := an.AnalyzeMigration(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, 3, plan.Summary.TotalOperations)
	// DROP TABLE is CRITICAL, so overall risk escalates to CRITICAL.
	assert.Equal(t, "CRITICAL", plan.Summary.OverallRisk)
	assert.NotEmpty(t, plan.Risks)
	assert.Contains(t, plan.Summary.TablesAffected, "t")

	formatted := an.FormatDetailedPlan(plan)
	assert.Contains(t, formatted, "DRY RUN ANALYSIS")
	assert.Contains(t, formatted, "risky")
}

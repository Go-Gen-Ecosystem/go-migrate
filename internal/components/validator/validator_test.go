/**
 * @file validator_test.go
 * @description Behavioral tests for the migration validators: syntax (balanced
 * parens/quotes, DB EXPLAIN against in-memory sqlite), security (dangerous ops,
 * injection, privilege escalation), dependency (tracker-backed + cycles),
 * performance, business rules, checksum, and the composite validator.
 * @author Admilson B. F. Cossa
 */

package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/constants"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB opens a fresh in-memory sqlite database for a single test.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func checksumOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// --- fakeTracker implements types.MigrationTracker for dependency tests. ---

type fakeTracker struct {
	applied    map[string]bool
	migrations map[string]*types.Migration
}

func newFakeTracker() *fakeTracker {
	return &fakeTracker{applied: map[string]bool{}, migrations: map[string]*types.Migration{}}
}

func (f *fakeTracker) IsMigrationApplied(_ context.Context, id string) (bool, error) {
	return f.applied[id], nil
}
func (f *fakeTracker) GetMigration(_ context.Context, id string) (*types.Migration, error) {
	if m, ok := f.migrations[id]; ok {
		return m, nil
	}
	return &types.Migration{ID: id}, nil
}
func (f *fakeTracker) GetExecutedMigrations(context.Context) ([]types.MigrationRecord, error) {
	return nil, nil
}
func (f *fakeTracker) GetFailedMigrations(context.Context) ([]types.MigrationRecord, error) {
	return nil, nil
}
func (f *fakeTracker) RecordMigrationSuccess(context.Context, types.MigrationRecord, int64) error {
	return nil
}
func (f *fakeTracker) RecordMigrationFailure(context.Context, types.MigrationRecord, string) error {
	return nil
}
func (f *fakeTracker) RemoveMigration(context.Context, string) error { return nil }
func (f *fakeTracker) IsLocked(context.Context) (bool, error)        { return false, nil }
func (f *fakeTracker) AcquireLock(context.Context) error             { return nil }
func (f *fakeTracker) ReleaseLock(context.Context) error             { return nil }
func (f *fakeTracker) EnsureSchemaTable(context.Context) error       { return nil }
func (f *fakeTracker) GetCurrentVersion(context.Context) (string, error) {
	return "", nil
}
func (f *fakeTracker) GetMigrationByVersion(_ context.Context, _ string) (*types.Migration, error) {
	return nil, nil
}
func (f *fakeTracker) GetAppliedMigrations(context.Context) ([]*types.Migration, error) {
	return nil, nil
}

// --- Syntax validator ---

func TestSyntaxValidator_BalancedParens(t *testing.T) {
	v := NewSyntaxMigrationValidator(nil)

	ok := &types.Migration{Script: "CREATE TABLE t (id INTEGER);"}
	assert.NoError(t, v.ValidateMigration(ok))

	unbalanced := &types.Migration{Script: "CREATE TABLE t (id INTEGER;"}
	err := v.ValidateMigration(unbalanced)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parentheses")
}

func TestSyntaxValidator_ExtraClosingParen(t *testing.T) {
	v := NewSyntaxMigrationValidator(nil)
	err := v.ValidateMigration(&types.Migration{Script: "SELECT (1));"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parenthes")
}

func TestSyntaxValidator_UnbalancedQuotes(t *testing.T) {
	v := NewSyntaxMigrationValidator(nil)
	err := v.ValidateMigration(&types.Migration{Script: "INSERT INTO t VALUES ('abc);"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quote")
}

func TestSyntaxValidator_DangerousKeyword(t *testing.T) {
	v := NewSyntaxMigrationValidator(nil)
	err := v.ValidateMigration(&types.Migration{Script: "EXEC xp_cmdshell 'dir';"})
	require.Error(t, err)
}

func TestSyntaxValidator_WithDatabaseExplain(t *testing.T) {
	db := newTestDB(t)
	// Seed a real table so the EXPLAIN of a SELECT succeeds.
	require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);").Error)

	v := NewSyntaxMigrationValidator(db)

	// DDL against sqlite goes through validateSQLiteDDLSyntax (valid CREATE).
	ddl := &types.Migration{Script: "CREATE TABLE accounts (id INTEGER PRIMARY KEY);"}
	assert.NoError(t, v.ValidateMigration(ddl))

	// A DML statement is EXPLAIN-ed against the real DB and must be valid.
	dml := &types.Migration{Script: "SELECT id FROM users;"}
	assert.NoError(t, v.ValidateMigration(dml))
}

func TestSyntaxValidator_DatabaseExplainInvalid(t *testing.T) {
	db := newTestDB(t)
	v := NewSyntaxMigrationValidator(db)

	// SELECT from a non-existent table fails EXPLAIN.
	bad := &types.Migration{Script: "SELECT * FROM table_that_does_not_exist;"}
	err := v.ValidateMigration(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SQL statement")
}

func TestSyntaxValidator_DetailedWarnings(t *testing.T) {
	v := NewSyntaxMigrationValidator(nil)
	m := &types.Migration{Script: "SELECT * FROM users"}
	result := v.ValidateMigrationDetailed(context.Background(), m)
	require.NotNil(t, result)

	codes := make([]string, 0, len(result.Warnings))
	for _, w := range result.Warnings {
		codes = append(codes, w.Code)
	}
	assert.Contains(t, codes, "SELECT_STAR")
	assert.Contains(t, codes, "MISSING_SEMICOLON")
}

// --- Security validator ---

func TestSecurityValidator_DangerousOpsProductionOnly(t *testing.T) {
	prodCfg := &types.Config{Environment: constants.EnvironmentProduction}
	prod := NewSecurityMigrationValidator(prodCfg)

	dangerous := &types.Migration{Script: "DROP DATABASE app;"}
	err := prod.ValidateMigration(dangerous)
	require.Error(t, err, "dangerous op must fail in production")
	assert.Contains(t, err.Error(), "security violation")

	// The same script is allowed in development.
	devCfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	dev := NewSecurityMigrationValidator(devCfg)
	assert.NoError(t, dev.ValidateMigration(dangerous))
}

func TestSecurityValidator_SQLInjection(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewSecurityMigrationValidator(cfg)

	inj := &types.Migration{Script: "SELECT * FROM users WHERE name = '' OR '1'='1';"}
	err := v.ValidateMigration(inj)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SQL injection")
}

func TestSecurityValidator_PrivilegeEscalation(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewSecurityMigrationValidator(cfg)

	esc := &types.Migration{Script: "GRANT SUPER ON *.* TO 'x'@'%';"}
	err := v.ValidateMigration(esc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "privilege escalation")
}

func TestSecurityValidator_Clean(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentProduction}
	v := NewSecurityMigrationValidator(cfg)
	assert.NoError(t, v.ValidateMigration(&types.Migration{Script: "CREATE TABLE t (id INTEGER);"}))
}

func TestSecurityValidator_DetailedHardcodedPassword(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewSecurityMigrationValidator(cfg)

	m := &types.Migration{Script: "CREATE USER app WITH password = 'secret123';"}
	result := v.ValidateMigrationDetailed(context.Background(), m)
	require.NotNil(t, result)

	codes := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		codes = append(codes, issue.Code)
	}
	assert.Contains(t, codes, "HARDCODED_PASSWORD")
	assert.False(t, result.Valid)
}

// --- Dependency validator ---

func TestDependencyValidator_UnappliedDependencyFails(t *testing.T) {
	tracker := newFakeTracker() // nothing applied
	v := NewDependencyMigrationValidator(tracker)

	m := &types.Migration{ID: "child", Dependencies: []string{"parent"}}
	err := v.ValidateMigration(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not applied")
}

func TestDependencyValidator_AppliedDependencyPasses(t *testing.T) {
	tracker := newFakeTracker()
	tracker.applied["parent"] = true
	v := NewDependencyMigrationValidator(tracker)

	m := &types.Migration{ID: "child", Dependencies: []string{"parent"}}
	assert.NoError(t, v.ValidateMigration(m))
}

func TestDependencyValidator_NoDependencies(t *testing.T) {
	v := NewDependencyMigrationValidator(newFakeTracker())
	assert.NoError(t, v.ValidateMigration(&types.Migration{ID: "solo"}))
}

func TestDependencyValidator_CircularWithMap(t *testing.T) {
	v := NewDependencyMigrationValidator(newFakeTracker())

	// a -> b -> a batch cycle detected by CheckCircularDependencies.
	migrations := []types.Migration{
		{ID: "a", Dependencies: []string{"b"}},
		{ID: "b", Dependencies: []string{"a"}},
	}
	err := v.CheckCircularDependencies(migrations)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestDependencyValidator_NoCircular(t *testing.T) {
	v := NewDependencyMigrationValidator(newFakeTracker())
	migrations := []types.Migration{
		{ID: "a"},
		{ID: "b", Dependencies: []string{"a"}},
	}
	assert.NoError(t, v.CheckCircularDependencies(migrations))
}

// --- Performance validator ---

func TestPerformanceValidator_SlowOperation(t *testing.T) {
	v := NewPerformanceMigrationValidator(nil, nil)
	// ADD COLUMN ... NOT NULL is flagged as potentially slow.
	m := &types.Migration{Script: "ALTER TABLE users ADD COLUMN age INTEGER NOT NULL;"}
	err := v.ValidateMigration(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "performance issue")
}

func TestPerformanceValidator_Clean(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewPerformanceMigrationValidator(nil, cfg)
	assert.NoError(t, v.ValidateMigration(&types.Migration{Script: "CREATE TABLE t (id INTEGER);"}))
}

func TestPerformanceValidator_MissingIndexStrictVsLenient(t *testing.T) {
	script := "CREATE TABLE orders (id INTEGER, FOREIGN KEY (user_id) REFERENCES users(id));"

	// Lenient (default) does not fail on a missing FK index.
	lenient := NewPerformanceMigrationValidator(nil, &types.Config{
		Environment: constants.EnvironmentDevelopment,
	})
	assert.NoError(t, lenient.ValidateMigration(&types.Migration{Script: script}))

	// Strict mode turns the missing index into an error.
	strict := NewStrictPerformanceMigrationValidator(nil, &types.Config{
		Environment: constants.EnvironmentDevelopment,
	})
	err := strict.ValidateMigration(&types.Migration{Script: script})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index")
}

func TestPerformanceValidator_StrictModeFromConfig(t *testing.T) {
	cfg := &types.Config{
		Environment: constants.EnvironmentDevelopment,
		Migration:   types.MigrationConfig{StrictMode: true},
	}
	v := NewPerformanceMigrationValidator(nil, cfg)
	assert.True(t, v.strict, "StrictMode config must enable strict validation")
}

// --- Business rule validator ---

func TestBusinessRuleValidator_RequireDescription(t *testing.T) {
	v := NewBusinessRuleMigrationValidator()

	// Missing description is an error-severity rule.
	err := v.ValidateMigration(&types.Migration{ID: "m", Script: "SELECT 1;"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "description")

	// With a description it passes (author is only a warning).
	assert.NoError(t, v.ValidateMigration(&types.Migration{
		ID:          "m",
		Description: "adds a table",
		Script:      "CREATE TABLE t (id INTEGER);",
	}))
}

func TestBusinessRuleValidator_CustomRule(t *testing.T) {
	v := NewBusinessRuleMigrationValidator()
	v.AddRule(ValidationRule{
		Name:     "must_have_version",
		Severity: SeverityError,
		Validator: func(m *types.Migration) error {
			if m.Version == "" {
				return assertErr("version required")
			}
			return nil
		},
	})

	err := v.ValidateMigration(&types.Migration{Description: "d"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version required")
}

// assertErr is a tiny error helper for the custom-rule test.
type assertErr string

func (e assertErr) Error() string { return string(e) }

// --- Composite validator ---

func TestCompositeValidator_AggregatesFailures(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	tracker := newFakeTracker()

	v := NewCompositeMigrationValidator(db, cfg, tracker)

	// Missing description (business rule) fails even though SQL is valid.
	m := &types.Migration{ID: "m", Script: "CREATE TABLE t (id INTEGER);"}
	err := v.ValidateMigration(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration validation failed")
}

func TestCompositeValidator_ValidMigrationPasses(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	tracker := newFakeTracker()

	v := NewCompositeMigrationValidator(db, cfg, tracker)

	m := &types.Migration{
		ID:          "m",
		Description: "creates widgets table",
		Author:      "alice",
		Script:      "CREATE TABLE widgets (id INTEGER PRIMARY KEY);",
	}
	assert.NoError(t, v.ValidateMigration(m))
}

func TestCompositeValidator_ChecksumMatch(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())

	script := "CREATE TABLE t (id INTEGER);"
	good := &types.Migration{ID: "m", Script: script, Checksum: checksumOf(script)}
	assert.NoError(t, v.ValidateChecksum(good))

	bad := &types.Migration{ID: "m", Script: script, Checksum: "deadbeef"}
	err := v.ValidateChecksum(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")

	missing := &types.Migration{ID: "m", Script: script}
	assert.Error(t, v.ValidateChecksum(missing))
}

func TestCompositeValidator_ValidateDependencies(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())

	// Dependency present within the batch => valid.
	valid := []*types.Migration{
		{ID: "a"},
		{ID: "b", Dependencies: []string{"a"}},
	}
	assert.NoError(t, v.ValidateDependencies(context.Background(), valid))

	// Dependency on a migration not in the batch => error.
	invalid := []*types.Migration{
		{ID: "b", Dependencies: []string{"ghost"}},
	}
	err := v.ValidateDependencies(context.Background(), invalid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent migration")
}

func TestCompositeValidator_ValidateSchema(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())

	// ValidateSchema runs AutoMigrate and asserts the core tables exist.
	require.NoError(t, v.ValidateSchema(context.Background()))
	assert.True(t, db.Migrator().HasTable(&types.Migration{}))
}

func TestCompositeValidator_DetailedResult(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())

	// SELECT * plus a missing description yields warnings and an error issue.
	m := &types.Migration{ID: "m", Script: "SELECT * FROM users"}
	result := v.ValidateMigrationDetailed(context.Background(), m)
	require.NotNil(t, result)
	assert.False(t, result.Valid, "missing description makes the migration invalid")
	assert.NotEmpty(t, result.Warnings)
}

func TestSyntaxValidator_BatchAndNoOps(t *testing.T) {
	v := NewSyntaxMigrationValidator(nil)

	// Batch validation passes for balanced statements.
	good := []types.Migration{
		{Script: "CREATE TABLE a (id INTEGER);"},
		{Script: "CREATE TABLE b (id INTEGER);"},
	}
	assert.NoError(t, v.ValidateMigrations(good))

	// Batch validation fails when one migration is malformed.
	bad := []types.Migration{{Script: "CREATE TABLE a (id INTEGER;"}}
	assert.Error(t, v.ValidateMigrations(bad))

	// Context batch aggregates errors with the migration ID prefix.
	err := v.ValidateMigrationsCtx(context.Background(), []*types.Migration{
		{ID: "m1", Script: "SELECT ("},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "syntax validation failed")

	// The dependency/schema/checksum hooks on the syntax validator are no-ops.
	assert.NoError(t, v.CheckDependencies(nil))
	assert.NoError(t, v.CheckCircularDependencies(nil))
	assert.NoError(t, v.ValidateSchema(context.Background()))
	assert.NoError(t, v.ValidateChecksums(context.Background()))
	assert.NoError(t, v.ValidateDependencies(context.Background(), nil))
}

func TestSecurityValidator_BatchAndNoOps(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentProduction}
	v := NewSecurityMigrationValidator(cfg)

	// A dangerous op in the batch fails in production.
	bad := []types.Migration{{Script: "DROP DATABASE app;"}}
	assert.Error(t, v.ValidateMigrations(bad))

	// Context batch aggregates the failure.
	err := v.ValidateMigrationsCtx(context.Background(), []*types.Migration{
		{ID: "m1", Script: "DROP DATABASE app;"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security validation failed")

	assert.NoError(t, v.CheckDependencies(nil))
	assert.NoError(t, v.CheckCircularDependencies(nil))
	assert.NoError(t, v.ValidateSchema(context.Background()))
	assert.NoError(t, v.ValidateChecksums(context.Background()))
	assert.NoError(t, v.ValidateDependencies(context.Background(), nil))
}

func TestPerformanceValidator_BatchAndNoOps(t *testing.T) {
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewPerformanceMigrationValidator(nil, cfg)

	good := []types.Migration{{Script: "CREATE TABLE t (id INTEGER);"}}
	assert.NoError(t, v.ValidateMigrations(good))

	bad := []types.Migration{{Script: "ALTER TABLE users ADD COLUMN age INTEGER NOT NULL;"}}
	assert.Error(t, v.ValidateMigrations(bad))

	err := v.ValidateMigrationsCtx(context.Background(), []*types.Migration{
		{ID: "m1", Script: "ALTER TABLE users ADD COLUMN age INTEGER NOT NULL;"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "performance validation failed")

	assert.NoError(t, v.CheckDependencies(nil))
	assert.NoError(t, v.CheckCircularDependencies(nil))
	assert.NoError(t, v.ValidateSchema(context.Background()))
	assert.NoError(t, v.ValidateChecksums(context.Background()))
	assert.NoError(t, v.ValidateDependencies(context.Background(), nil))
}

func TestBusinessRuleValidator_BatchAndNoOps(t *testing.T) {
	v := NewBusinessRuleMigrationValidator()

	good := []types.Migration{{ID: "m", Description: "d", Script: "SELECT 1;"}}
	assert.NoError(t, v.ValidateMigrations(good))

	bad := []types.Migration{{ID: "m", Script: "SELECT 1;"}}
	assert.Error(t, v.ValidateMigrations(bad))

	err := v.ValidateMigrationsCtx(context.Background(), []*types.Migration{{ID: "m1", Script: "SELECT 1;"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business rule validation failed")

	assert.NoError(t, v.CheckDependencies(nil))
	assert.NoError(t, v.CheckCircularDependencies(nil))
	assert.NoError(t, v.ValidateSchema(context.Background()))
	assert.NoError(t, v.ValidateChecksums(context.Background()))
	assert.NoError(t, v.ValidateDependencies(context.Background(), nil))
}

func TestBusinessRuleValidator_RollbackForDestructive(t *testing.T) {
	v := NewBusinessRuleMigrationValidator()
	// A destructive DROP with a description and author but no rollback triggers
	// only a warning-severity rule, so ValidateMigration still returns nil.
	m := &types.Migration{
		ID:          "m",
		Description: "drops the old table",
		Author:      "alice",
		Script:      "DROP TABLE legacy;",
	}
	assert.NoError(t, v.ValidateMigration(m))
}

func TestDependencyValidator_BatchMethods(t *testing.T) {
	tracker := newFakeTracker()
	tracker.applied["parent"] = true
	v := NewDependencyMigrationValidator(tracker)

	migrations := []types.Migration{
		{ID: "child", Dependencies: []string{"parent"}},
	}
	assert.NoError(t, v.ValidateMigrations(migrations))
	assert.NoError(t, v.CheckDependencies(migrations))

	// Context batch with an unapplied dependency aggregates the error.
	err := v.ValidateMigrationsCtx(context.Background(), []*types.Migration{
		{ID: "orphan", Dependencies: []string{"never_applied"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependency validation failed")
}

func TestDependencyValidator_ValidateDependenciesMethod(t *testing.T) {
	tracker := newFakeTracker()
	v := NewDependencyMigrationValidator(tracker)

	err := v.ValidateDependencies(context.Background(), []*types.Migration{
		{ID: "child", Dependencies: []string{"missing"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependency validation failed")
}

func TestCompositeValidator_BatchAndChecksumsDB(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())

	// Prepare the migration table so ValidateChecksums can query it.
	require.NoError(t, v.ValidateSchema(context.Background()))

	// Insert a successfully-applied migration with a correct checksum.
	script := "CREATE TABLE t (id INTEGER);"
	rec := &types.Migration{
		ID:          "m1",
		Version:     "1",
		Description: "creates t",
		Author:      "alice",
		Script:      script,
		Checksum:    checksumOf(script),
		State:       constants.MigrationStateSuccess,
	}
	require.NoError(t, db.Create(rec).Error)

	// ValidateChecksums scans stored migrations and verifies their checksums.
	assert.NoError(t, v.ValidateChecksums(context.Background()))

	// Batch validation of a well-formed migration passes end to end.
	good := []types.Migration{{
		ID:          "m2",
		Description: "creates u",
		Author:      "bob",
		Script:      "CREATE TABLE u (id INTEGER);",
	}}
	assert.NoError(t, v.ValidateMigrations(good))

	// Composite dependency/cycle batch hooks fan out to sub-validators.
	assert.NoError(t, v.CheckCircularDependencies([]types.Migration{{ID: "a"}}))

	// Context batch aggregation reports the composite failure message.
	err := v.ValidateMigrationsCtx(context.Background(), []*types.Migration{{ID: "bad", Script: "SELECT 1;"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrations validation failed")
}

func TestCompositeValidator_ChecksumsDBMismatch(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())
	require.NoError(t, v.ValidateSchema(context.Background()))

	// Stored checksum does not match the current script content.
	rec := &types.Migration{
		ID:       "m1",
		Version:  "1",
		Script:   "CREATE TABLE t (id INTEGER);",
		Checksum: "deadbeefdeadbeefdeadbeefdeadbeef",
		State:    constants.MigrationStateSuccess,
	}
	require.NoError(t, db.Create(rec).Error)

	err := v.ValidateChecksums(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func TestCompositeValidator_AddValidator(t *testing.T) {
	db := newTestDB(t)
	cfg := &types.Config{Environment: constants.EnvironmentDevelopment}
	v := NewCompositeMigrationValidator(db, cfg, newFakeTracker())

	// Adding a business-rule validator that always fails makes an otherwise
	// valid migration fail.
	failing := NewBusinessRuleMigrationValidator()
	failing.AddRule(ValidationRule{
		Name:      "always_fail",
		Severity:  SeverityError,
		Validator: func(*types.Migration) error { return assertErr("nope") },
	})
	v.AddValidator(failing)

	m := &types.Migration{
		ID:          "m",
		Description: "d",
		Author:      "a",
		Script:      "CREATE TABLE t (id INTEGER);",
	}
	err := v.ValidateMigration(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestMigrationValidatorAdapter(t *testing.T) {
	v := NewBusinessRuleMigrationValidator()
	adapter := NewMigrationValidatorAdapter(v)

	// Missing description surfaces through the adapter's context method.
	err := adapter.ValidateMigration(context.Background(), &types.Migration{ID: "m"})
	require.Error(t, err)

	// Checksum validation on the adapter is a no-op (returns nil).
	assert.NoError(t, adapter.ValidateChecksum(&types.Migration{ID: "m"}))

	// Batch validation delegates to the wrapped validator: a valid batch passes.
	assert.NoError(t, adapter.ValidateMigrations(context.Background(), []*types.Migration{
		{ID: "ok", Description: "d", Script: "SELECT 1;"},
	}))

	// A batch with a missing description fails through the adapter.
	err = adapter.ValidateMigrations(context.Background(), []*types.Migration{{ID: "bad"}})
	require.Error(t, err)

	// ValidateDependencies delegates to CheckDependencies on the wrapped validator.
	assert.NoError(t, adapter.ValidateDependencies(context.Background(), []*types.Migration{
		{ID: "ok", Description: "d", Script: "SELECT 1;"},
	}))
}

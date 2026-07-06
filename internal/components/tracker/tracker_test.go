/**
 * @file tracker_test.go
 * @description DB-backed tests for the migration tracker against in-memory sqlite.
 * Exercises the DatabaseMigrationTracker record/query/history/checksum/idempotency
 * paths plus the in-memory and file trackers' drift/validation behavior.
 * @author Admilson B. F. Cossa
 */

package tracker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newTestDB opens a fresh file-backed sqlite database in a temp dir. A temp file
// (rather than a shared in-memory DSN) gives each test a fully isolated schema
// and avoids cross-test table bleed via the shared cache.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "tracker.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err, "open sqlite")
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// newTestTracker builds a DatabaseMigrationTracker with initialized tables.
func newTestTracker(t *testing.T) *DatabaseMigrationTracker {
	t.Helper()
	db := newTestDB(t)
	tr, err := NewDatabaseMigrationTracker(db, "migration_history")
	require.NoError(t, err, "new tracker")
	require.NotNil(t, tr)
	return tr
}

// sampleMigration returns a fully-populated applied migration for recording.
func sampleMigration(id, version, checksum string) *types.Migration {
	now := time.Now()
	return &types.Migration{
		ID:            id,
		Version:       version,
		Description:   "create " + id,
		Type:          types.MigrationTypeSQL,
		FileName:      id + ".sql",
		FilePath:      "/migrations/" + id + ".sql",
		Script:        "CREATE TABLE " + id + " (id INTEGER PRIMARY KEY);",
		Checksum:      checksum,
		Author:        "tester",
		ChangesetID:   "cs-" + id,
		Contexts:      []string{"prod"},
		Labels:        []string{"schema"},
		Dependencies:  []string{},
		Preconditions: []types.Precondition{},
		State:         "applied",
		ExecutedAt:    &now,
		ExecutionTime: 1500 * time.Millisecond,
		CanRollback:   true,
		RollbackSQL:   "DROP TABLE " + id + ";",
		Metadata:      map[string]string{"origin": "test"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestNewDatabaseMigrationTracker_CreatesTables(t *testing.T) {
	db := newTestDB(t)

	tr, err := NewDatabaseMigrationTracker(db, "migration_history")
	require.NoError(t, err)
	require.NotNil(t, tr)

	// Both the configured history table and the changelog table must exist.
	assert.True(t, db.Migrator().HasTable("migration_history"), "history table exists")
	assert.True(t, db.Migrator().HasTable("migration_changelog"), "changelog table exists")
}

func TestNewDatabaseMigrationTracker_DefaultTableName(t *testing.T) {
	db := newTestDB(t)

	// An empty table name must default to "migration_history".
	tr, err := NewDatabaseMigrationTrackerWithDryRun(db, "", false)
	require.NoError(t, err)
	require.NotNil(t, tr)
	assert.Equal(t, "migration_history", tr.tableName)
	assert.True(t, db.Migrator().HasTable("migration_history"))
}

func TestNewDatabaseMigrationTracker_DryRunSkipsTableCreation(t *testing.T) {
	db := newTestDB(t)

	// In dry-run mode initializeTables must be skipped, so no tables get created.
	tr, err := NewDatabaseMigrationTrackerWithDryRun(db, "migration_history", true)
	require.NoError(t, err)
	require.NotNil(t, tr)
	assert.False(t, db.Migrator().HasTable("migration_history"), "no table created in dry run")
}

func TestRecordMigration_PersistsAndIsQueryable(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	m := sampleMigration("m1", "0001", "checksum-1")
	require.NoError(t, tr.RecordMigration(ctx, m))

	// GetMigration round-trips the stored fields (including JSON columns).
	got, err := tr.GetMigration(ctx, "m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", got.ID)
	assert.Equal(t, "0001", got.Version)
	assert.Equal(t, "create m1", got.Description)
	assert.Equal(t, "applied", got.State)
	assert.Equal(t, "checksum-1", got.Checksum)
	assert.Equal(t, "tester", got.Author)
	assert.Equal(t, types.MigrationTypeSQL, got.Type)
	assert.True(t, got.CanRollback)
	assert.Equal(t, "DROP TABLE m1;", got.RollbackSQL)
	assert.Equal(t, []string{"prod"}, got.Contexts)
	assert.Equal(t, []string{"schema"}, got.Labels)
	assert.Equal(t, map[string]string{"origin": "test"}, got.Metadata)
	assert.Equal(t, 1500*time.Millisecond, got.ExecutionTime)

	// A changelog row is written as a side effect of RecordMigration.
	changelog, err := tr.GetChangelog(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, changelog, 1)
	assert.Equal(t, "m1", changelog[0].MigrationID)
	assert.Equal(t, "execute", changelog[0].Operation)
	assert.Equal(t, "applied", changelog[0].Status)
	assert.Equal(t, "tester", changelog[0].ExecutedBy)
}

func TestRecordMigration_UpsertUpdatesExisting(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	m := sampleMigration("m1", "0001", "checksum-1")
	require.NoError(t, tr.RecordMigration(ctx, m))

	// Re-record with a changed description/state — Save() must upsert, not duplicate.
	m.Description = "updated description"
	m.State = "failed"
	m.ErrorMessage = "boom"
	require.NoError(t, tr.RecordMigration(ctx, m))

	got, err := tr.GetMigration(ctx, "m1")
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Description)
	assert.Equal(t, "failed", got.State)
	assert.Equal(t, "boom", got.ErrorMessage)

	// History must still contain exactly one migration row.
	history, err := tr.GetMigrationHistory(ctx, 100, 0)
	require.NoError(t, err)
	assert.Len(t, history, 1)
}

func TestGetMigration_NotFound(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	_, err := tr.GetMigration(ctx, "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration not found")
}

func TestGetMigrationByVersion(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0007", "c7")))

	got, err := tr.GetMigrationByVersion(ctx, "0007")
	require.NoError(t, err)
	assert.Equal(t, "m1", got.ID)

	_, err = tr.GetMigrationByVersion(ctx, "9999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration not found")
}

func TestIsMigrationApplied(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	applied, err := tr.IsMigrationApplied(ctx, "m1")
	require.NoError(t, err)
	assert.False(t, applied, "unknown migration is not applied")

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "c1")))
	applied, err = tr.IsMigrationApplied(ctx, "m1")
	require.NoError(t, err)
	assert.True(t, applied, "recorded applied migration reports applied")

	// A non-applied (failed) migration must report not-applied.
	failed := sampleMigration("m2", "0002", "c2")
	failed.State = "failed"
	require.NoError(t, tr.RecordMigration(ctx, failed))
	applied, err = tr.IsMigrationApplied(ctx, "m2")
	require.NoError(t, err)
	assert.False(t, applied, "failed migration is not applied")
}

func TestGetAppliedMigrations_OrderedByExecutedAt(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	base := time.Now()
	// Insert out of chronological order; GetAppliedMigrations must sort ASC.
	m3 := sampleMigration("m3", "0003", "c3")
	t3 := base.Add(3 * time.Hour)
	m3.ExecutedAt = &t3
	m1 := sampleMigration("m1", "0001", "c1")
	t1 := base.Add(1 * time.Hour)
	m1.ExecutedAt = &t1
	m2 := sampleMigration("m2", "0002", "c2")
	t2 := base.Add(2 * time.Hour)
	m2.ExecutedAt = &t2

	require.NoError(t, tr.RecordMigration(ctx, m3))
	require.NoError(t, tr.RecordMigration(ctx, m1))
	require.NoError(t, tr.RecordMigration(ctx, m2))

	// A pending migration must be excluded from applied results.
	pending := sampleMigration("mp", "0004", "cp")
	pending.State = "pending"
	require.NoError(t, tr.RecordMigration(ctx, pending))

	applied, err := tr.GetAppliedMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, applied, 3)
	assert.Equal(t, "m1", applied[0].ID)
	assert.Equal(t, "m2", applied[1].ID)
	assert.Equal(t, "m3", applied[2].ID)
}

func TestGetPendingMigrations(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	applied := sampleMigration("m1", "0001", "c1")
	require.NoError(t, tr.RecordMigration(ctx, applied))

	pending := sampleMigration("m2", "0002", "c2")
	pending.State = "pending"
	require.NoError(t, tr.RecordMigration(ctx, pending))

	got, err := tr.GetPendingMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "m2", got[0].ID)
	assert.Equal(t, "pending", got[0].State)
}

func TestRemoveMigration(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "c1")))
	require.NoError(t, tr.RemoveMigration(ctx, "m1"))

	_, err := tr.GetMigration(ctx, "m1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration not found")

	// Removing a non-existent record is a no-op (gorm delete matches 0 rows).
	require.NoError(t, tr.RemoveMigration(ctx, "missing"))
}

func TestGetMigrationHistory_Pagination(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	base := time.Now()
	for i := 1; i <= 5; i++ {
		m := sampleMigration(fmt.Sprintf("m%d", i), fmt.Sprintf("000%d", i), fmt.Sprintf("c%d", i))
		et := base.Add(time.Duration(i) * time.Hour)
		m.ExecutedAt = &et
		require.NoError(t, tr.RecordMigration(ctx, m))
	}

	// History is ordered by executed_at DESC; first page holds the newest two.
	page1, err := tr.GetMigrationHistory(ctx, 2, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, "m5", page1[0].ID)
	assert.Equal(t, "m4", page1[1].ID)

	page2, err := tr.GetMigrationHistory(ctx, 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.Equal(t, "m3", page2[0].ID)
	assert.Equal(t, "m2", page2[1].ID)
}

func TestGetChangelog_Pagination(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	for i := 1; i <= 3; i++ {
		require.NoError(t, tr.RecordMigration(ctx, sampleMigration(fmt.Sprintf("m%d", i), fmt.Sprintf("000%d", i), fmt.Sprintf("c%d", i))))
	}

	all, err := tr.GetChangelog(ctx, 100, 0)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	limited, err := tr.GetChangelog(ctx, 1, 0)
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

func TestRecordChangelog_RoundTripsContextAndMetadata(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	entry := &ChangelogEntry{
		MigrationID:   "mX",
		Operation:     "rollback",
		Status:        "rolled_back",
		ExecutedBy:    "ops",
		ExecutedAt:    time.Now(),
		ExecutionTime: 250 * time.Millisecond,
		ErrorMessage:  "",
		Context:       map[string]interface{}{"reason": "manual"},
		Metadata:      map[string]interface{}{"ticket": "OPS-1"},
	}
	require.NoError(t, tr.RecordChangelog(ctx, entry))

	got, err := tr.GetChangelog(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "mX", got[0].MigrationID)
	assert.Equal(t, "rollback", got[0].Operation)
	assert.Equal(t, 250*time.Millisecond, got[0].ExecutionTime)
	assert.Equal(t, "manual", got[0].Context["reason"])
	assert.Equal(t, "OPS-1", got[0].Metadata["ticket"])
}

func TestValidateChecksum_Match(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "checksum-match")))

	res, err := tr.ValidateChecksum(ctx, sampleMigration("m1", "0001", "checksum-match"))
	require.NoError(t, err)
	assert.True(t, res.IsValid)
	assert.Equal(t, "checksum-match", res.ExpectedChecksum)
	assert.Equal(t, "checksum-match", res.ActualChecksum)
	assert.Equal(t, "Checksum validation passed", res.Message)
}

func TestValidateChecksum_Mismatch(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "stored-checksum")))

	res, err := tr.ValidateChecksum(ctx, sampleMigration("m1", "0001", "different-checksum"))
	require.NoError(t, err)
	assert.False(t, res.IsValid)
	assert.Equal(t, "stored-checksum", res.ExpectedChecksum)
	assert.Equal(t, "different-checksum", res.ActualChecksum)
	assert.Contains(t, res.Message, "Checksum mismatch")
}

func TestValidateChecksum_NotFoundSkips(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	// A migration with no stored record short-circuits to valid (skipped).
	res, err := tr.ValidateChecksum(ctx, sampleMigration("ghost", "9999", "any"))
	require.NoError(t, err)
	assert.True(t, res.IsValid)
	assert.Contains(t, res.Message, "checksum validation skipped")
}

func TestValidateIdempotency_DatabaseStates(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	// Not-yet-recorded migration is safe to run.
	res, err := tr.ValidateIdempotency(ctx, sampleMigration("new", "0001", "c1"))
	require.NoError(t, err)
	assert.True(t, res.CanRerun)
	assert.Contains(t, res.Message, "not previously executed")

	// An applied migration (not marked for rerun) cannot rerun.
	applied := sampleMigration("m1", "0002", "c2")
	require.NoError(t, tr.RecordMigration(ctx, applied))
	res, err = tr.ValidateIdempotency(ctx, sampleMigration("m1", "0002", "c2"))
	require.NoError(t, err)
	assert.False(t, res.CanRerun)
	assert.False(t, res.IsIdempotent)

	// A failed migration can be retried.
	failed := sampleMigration("m2", "0003", "c3")
	failed.State = "failed"
	require.NoError(t, tr.RecordMigration(ctx, failed))
	res, err = tr.ValidateIdempotency(ctx, sampleMigration("m2", "0003", "c3"))
	require.NoError(t, err)
	assert.True(t, res.CanRerun)

	// A run_always migration is always safe to rerun.
	always := sampleMigration("m3", "0004", "c4")
	always.RunAlways = true
	require.NoError(t, tr.RecordMigration(ctx, always))
	res, err = tr.ValidateIdempotency(ctx, sampleMigration("m3", "0004", "c4"))
	require.NoError(t, err)
	assert.True(t, res.CanRerun)
	assert.Contains(t, res.Message, "run_always")

	// run_on_change with a differing checksum is safe to rerun.
	onChange := sampleMigration("m4", "0005", "orig")
	onChange.RunOnChange = true
	require.NoError(t, tr.RecordMigration(ctx, onChange))
	res, err = tr.ValidateIdempotency(ctx, sampleMigration("m4", "0005", "changed"))
	require.NoError(t, err)
	assert.True(t, res.CanRerun)
	assert.Contains(t, res.Message, "run_on_change")
}

func TestValidateIdempotency_RolledBackCanRerun(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	rolled := sampleMigration("m1", "0001", "c1")
	rolled.State = "rolled_back"
	require.NoError(t, tr.RecordMigration(ctx, rolled))

	res, err := tr.ValidateIdempotency(ctx, sampleMigration("m1", "0001", "c1"))
	require.NoError(t, err)
	assert.True(t, res.CanRerun)
	assert.Contains(t, res.Message, "rolled back")
}

func TestDatabaseDetectDrift_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	// The DB tracker's DetectDrift is a documented placeholder returning no drift.
	results, err := tr.DetectDrift(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestRecordMigrationSuccessAndFailure(t *testing.T) {
	ctx := context.Background()
	tr := newTestTracker(t)

	rec := types.MigrationRecord{
		ID:          "rm1",
		Version:     "0001",
		Description: "success record",
		Type:        "sql",
		Checksum:    "sc1",
		Script:      "CREATE TABLE rm1 (id INTEGER);",
	}
	require.NoError(t, tr.RecordMigrationSuccess(ctx, rec, int64(2*time.Second)))

	applied, err := tr.IsMigrationApplied(ctx, "rm1")
	require.NoError(t, err)
	assert.True(t, applied)

	got, err := tr.GetMigration(ctx, "rm1")
	require.NoError(t, err)
	assert.Equal(t, "applied", got.State)
	assert.Equal(t, 2*time.Second, got.ExecutionTime)

	failRec := types.MigrationRecord{
		ID:      "rm2",
		Version: "0002",
		Type:    "sql",
		Script:  "CREATE TABLE rm2 (id INTEGER);",
	}
	require.NoError(t, tr.RecordMigrationFailure(ctx, failRec, "disk full"))
	failed, err := tr.GetMigration(ctx, "rm2")
	require.NoError(t, err)
	assert.Equal(t, "failed", failed.State)
	assert.Equal(t, "disk full", failed.ErrorMessage)
}

// ---- In-memory tracker ----

func TestInMemoryTracker_RecordAndQuery(t *testing.T) {
	ctx := context.Background()
	tr := NewInMemoryMigrationTracker()

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "c1")))

	got, err := tr.GetMigration(ctx, "m1")
	require.NoError(t, err)
	assert.Equal(t, "m1", got.ID)

	// Returned copies must be independent of internal state.
	got.Description = "mutated"
	again, err := tr.GetMigration(ctx, "m1")
	require.NoError(t, err)
	assert.Equal(t, "create m1", again.Description, "internal state not mutated by caller")

	applied, err := tr.IsMigrationApplied(ctx, "m1")
	require.NoError(t, err)
	assert.True(t, applied)

	byVer, err := tr.GetMigrationByVersion(ctx, "0001")
	require.NoError(t, err)
	assert.Equal(t, "m1", byVer.ID)

	list, err := tr.GetAppliedMigrations(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	changelog, err := tr.GetChangelog(ctx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, changelog, 1)
}

func TestInMemoryTracker_ValidateChecksumAndIdempotency(t *testing.T) {
	ctx := context.Background()
	tr := NewInMemoryMigrationTracker()

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "stored")))

	match, err := tr.ValidateChecksum(ctx, sampleMigration("m1", "0001", "stored"))
	require.NoError(t, err)
	assert.True(t, match.IsValid)

	mismatch, err := tr.ValidateChecksum(ctx, sampleMigration("m1", "0001", "other"))
	require.NoError(t, err)
	assert.False(t, mismatch.IsValid)

	idem, err := tr.ValidateIdempotency(ctx, sampleMigration("m1", "0001", "stored"))
	require.NoError(t, err)
	assert.False(t, idem.CanRerun, "applied in-memory migration cannot rerun")

	drift, err := tr.DetectDrift(ctx)
	require.NoError(t, err)
	assert.Empty(t, drift)
}

func TestInMemoryTracker_RemoveAndPending(t *testing.T) {
	ctx := context.Background()
	tr := NewInMemoryMigrationTracker()

	pending := sampleMigration("m1", "0001", "c1")
	pending.State = "pending"
	require.NoError(t, tr.RecordMigration(ctx, pending))

	p, err := tr.GetPendingMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, p, 1)
	assert.Equal(t, "m1", p[0].ID)

	require.NoError(t, tr.RemoveMigration(ctx, "m1"))
	_, err = tr.GetMigration(ctx, "m1")
	require.Error(t, err)
}

// ---- File tracker ----

func TestFileTracker_RecordPersistsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tracker.json")

	tr := NewFileMigrationTracker(path)
	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "c1")))

	// A fresh tracker over the same file must load the persisted migration.
	tr2 := NewFileMigrationTracker(path)
	got, err := tr2.GetMigration(ctx, "m1")
	require.NoError(t, err)
	assert.Equal(t, "0001", got.Version)

	applied, err := tr2.IsMigrationApplied(ctx, "m1")
	require.NoError(t, err)
	assert.True(t, applied)
}

func TestFileTracker_DetectDrift_ChecksumMismatchAndMissingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "tracker.json")
	tr := NewFileMigrationTracker(path)

	// Migration whose file exists but whose stored checksum will not match.
	sqlFile := filepath.Join(dir, "0001_create.sql")
	content := []byte("CREATE TABLE users (id INTEGER PRIMARY KEY);")
	require.NoError(t, os.WriteFile(sqlFile, content, 0o644))

	drifted := sampleMigration("m1", "0001", "stale-checksum")
	drifted.FilePath = sqlFile
	require.NoError(t, tr.RecordMigration(ctx, drifted))

	// Migration whose file is missing entirely.
	missing := sampleMigration("m2", "0002", "any")
	missing.FilePath = filepath.Join(dir, "not_there.sql")
	require.NoError(t, tr.RecordMigration(ctx, missing))

	results, err := tr.DetectDrift(ctx)
	require.NoError(t, err)
	require.Len(t, results, 2)

	byType := map[string]*DriftDetectionResult{}
	for _, r := range results {
		byType[r.DriftType] = r
	}
	require.Contains(t, byType, "checksum_mismatch")
	require.Contains(t, byType, "file_missing")

	// The checksum_mismatch result must carry the freshly computed checksum.
	want := fmt.Sprintf("%x", sha256.Sum256(content))
	assert.Equal(t, want, byType["checksum_mismatch"].ActualChecksum)
	assert.Equal(t, "stale-checksum", byType["checksum_mismatch"].ExpectedChecksum)
}

func TestInMemoryTracker_HistoryAndSuccessFailure(t *testing.T) {
	ctx := context.Background()
	tr := NewInMemoryMigrationTracker()

	rec := types.MigrationRecord{ID: "s1", Version: "0001", Type: "sql", Checksum: "c1", Script: "CREATE TABLE s1 (id INTEGER);"}
	require.NoError(t, tr.RecordMigrationSuccess(ctx, rec, int64(time.Second)))
	require.NoError(t, tr.RecordMigrationFailure(ctx, types.MigrationRecord{ID: "s2", Version: "0002", Type: "sql", Script: "x"}, "failed hard"))

	s1, err := tr.GetMigration(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "applied", s1.State)
	assert.Equal(t, time.Second, s1.ExecutionTime)

	s2, err := tr.GetMigration(ctx, "s2")
	require.NoError(t, err)
	assert.Equal(t, "failed", s2.State)
	assert.Equal(t, "failed hard", s2.ErrorMessage)

	history, err := tr.GetMigrationHistory(ctx, 100, 0)
	require.NoError(t, err)
	assert.Len(t, history, 2)

	// Offset beyond the end yields an empty page without error.
	empty, err := tr.GetMigrationHistory(ctx, 10, 100)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestFileTracker_HistoryPendingChangelogAndSuccessFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tracker.json")
	tr := NewFileMigrationTracker(path)

	base := time.Now()
	for i := 1; i <= 3; i++ {
		m := sampleMigration(fmt.Sprintf("m%d", i), fmt.Sprintf("000%d", i), fmt.Sprintf("c%d", i))
		et := base.Add(time.Duration(i) * time.Hour)
		m.ExecutedAt = &et
		require.NoError(t, tr.RecordMigration(ctx, m))
	}

	// History is newest-first with pagination.
	page, err := tr.GetMigrationHistory(ctx, 2, 0)
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, "m3", page[0].ID)

	// Success/failure records via the MigrationRecord API.
	require.NoError(t, tr.RecordMigrationSuccess(ctx, types.MigrationRecord{ID: "ok", Version: "0009", Type: "sql", Script: "y"}, int64(time.Second)))
	require.NoError(t, tr.RecordMigrationFailure(ctx, types.MigrationRecord{ID: "bad", Version: "0010", Type: "sql", Script: "z"}, "nope"))

	okMig, err := tr.GetMigration(ctx, "ok")
	require.NoError(t, err)
	assert.Equal(t, "applied", okMig.State)

	// File tracker's GetPendingMigrations only returns state "pending" or "".
	// A "failed" migration is therefore NOT listed as pending, and neither are
	// the "applied" ones. With only failed/applied records, the set is empty.
	pending, err := tr.GetPendingMigrations(ctx)
	require.NoError(t, err)
	for _, m := range pending {
		assert.NotEqual(t, "applied", m.State, "applied migration must not be pending")
		assert.NotEqual(t, "failed", m.State, "failed migration must not be pending")
	}

	changelog, err := tr.GetChangelog(ctx, 100, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(changelog), 3)
}

func TestFileTracker_RemoveNotFound(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tracker.json")
	tr := NewFileMigrationTracker(path)

	err := tr.RemoveMigration(ctx, "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration not found")
}

func TestFileTracker_ValidateChecksumAndIdempotency(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tracker.json")
	tr := NewFileMigrationTracker(path)

	require.NoError(t, tr.RecordMigration(ctx, sampleMigration("m1", "0001", "stored")))

	match, err := tr.ValidateChecksum(ctx, sampleMigration("m1", "0001", "stored"))
	require.NoError(t, err)
	assert.True(t, match.IsValid)

	mismatch, err := tr.ValidateChecksum(ctx, sampleMigration("m1", "0001", "changed"))
	require.NoError(t, err)
	assert.False(t, mismatch.IsValid)

	idem, err := tr.ValidateIdempotency(ctx, sampleMigration("m1", "0001", "stored"))
	require.NoError(t, err)
	assert.False(t, idem.CanRerun)
}

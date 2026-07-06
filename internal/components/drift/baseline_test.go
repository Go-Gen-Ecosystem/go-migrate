/**
 * @file baseline_test.go
 * @description Tests for the database- and file-backed baseline managers. The
 *              database manager is driven against a go-sqlmock *sql.DB so the
 *              real SQL round-trips (ensure table, upsert, load, latest, list,
 *              delete) are exercised without a live server, including the
 *              not-found and error paths. The file manager's placeholder no-ops
 *              are verified to round-trip without error.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/drift/baseline_test.go
 */

package drift

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func sampleSnapshot() *SchemaSnapshot {
	return &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Tables: map[string]*TableSchema{
			"users": {
				Name:    "users",
				Columns: []ColumnSchema{{Name: "id", DataType: "integer"}},
			},
		},
		Indexes:     map[string]*IndexSchema{},
		Constraints: map[string]*ConstraintSchema{},
	}
}

func TestNewDatabaseBaselineManager_DefaultTable(t *testing.T) {
	db, _ := newMockDB(t)

	m := NewDatabaseBaselineManager(db, "")
	if m == nil {
		t.Fatal("NewDatabaseBaselineManager returned nil")
	}
	if m.tableName != "schema_baselines" {
		t.Errorf("expected default table 'schema_baselines', got %q", m.tableName)
	}

	// A custom table name must be honored.
	m2 := NewDatabaseBaselineManager(db, "custom_baselines")
	if m2.tableName != "custom_baselines" {
		t.Errorf("expected custom table 'custom_baselines', got %q", m2.tableName)
	}
}

func TestDatabaseBaselineManager_EnsureTable(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_baselines").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := m.EnsureTable(context.Background()); err != nil {
		t.Fatalf("EnsureTable failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDatabaseBaselineManager_EnsureTable_Error(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_baselines").
		WillReturnError(errors.New("boom"))

	if err := m.EnsureTable(context.Background()); err == nil {
		t.Error("expected error from EnsureTable when the DDL fails")
	}
}

func TestDatabaseBaselineManager_SaveBaseline(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	// SaveBaseline first ensures the table, then upserts.
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_baselines").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_baselines").
		WithArgs("v1", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := m.SaveBaseline(context.Background(), "v1", sampleSnapshot()); err != nil {
		t.Fatalf("SaveBaseline failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDatabaseBaselineManager_SaveBaseline_InsertError(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_baselines").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_baselines").
		WillReturnError(errors.New("insert failed"))

	if err := m.SaveBaseline(context.Background(), "v1", sampleSnapshot()); err == nil {
		t.Error("expected error when the upsert fails")
	}
}

func TestDatabaseBaselineManager_LoadBaseline(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	// A round-tripped snapshot must unmarshal back into a usable structure.
	snapshotJSON := `{"version":"v1","tables":{"users":{"name":"users","columns":[{"name":"id","data_type":"integer"}]}}}`
	mock.ExpectQuery("SELECT snapshot_data").
		WithArgs("v1").
		WillReturnRows(sqlmock.NewRows([]string{"snapshot_data"}).AddRow([]byte(snapshotJSON)))

	snapshot, err := m.LoadBaseline(context.Background(), "v1")
	if err != nil {
		t.Fatalf("LoadBaseline failed: %v", err)
	}
	if snapshot.Version != "v1" {
		t.Errorf("expected version 'v1', got %q", snapshot.Version)
	}
	if _, ok := snapshot.Tables["users"]; !ok {
		t.Error("expected the 'users' table to be present in the loaded snapshot")
	}
}

func TestDatabaseBaselineManager_LoadBaseline_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectQuery("SELECT snapshot_data").
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	snapshot, err := m.LoadBaseline(context.Background(), "missing")
	if err == nil {
		t.Error("expected a not-found error for a missing baseline version")
	}
	if snapshot != nil {
		t.Error("expected nil snapshot for a missing baseline version")
	}
}

func TestDatabaseBaselineManager_GetLatestBaseline(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	snapshotJSON := `{"version":"v3"}`
	mock.ExpectQuery("SELECT snapshot_data").
		WillReturnRows(sqlmock.NewRows([]string{"snapshot_data"}).AddRow([]byte(snapshotJSON)))

	snapshot, err := m.GetLatestBaseline(context.Background())
	if err != nil {
		t.Fatalf("GetLatestBaseline failed: %v", err)
	}
	if snapshot.Version != "v3" {
		t.Errorf("expected version 'v3', got %q", snapshot.Version)
	}
}

func TestDatabaseBaselineManager_GetLatestBaseline_NoRows(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectQuery("SELECT snapshot_data").
		WillReturnError(sql.ErrNoRows)

	snapshot, err := m.GetLatestBaseline(context.Background())
	if err == nil {
		t.Error("expected an error when no baselines exist")
	}
	if snapshot != nil {
		t.Error("expected nil snapshot when no baselines exist")
	}
}

func TestDatabaseBaselineManager_ListBaselines(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectQuery("SELECT version").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).
			AddRow("v3").
			AddRow("v2").
			AddRow("v1"))

	versions, err := m.ListBaselines(context.Background())
	if err != nil {
		t.Fatalf("ListBaselines failed: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0] != "v3" {
		t.Errorf("expected newest version 'v3' first, got %q", versions[0])
	}
}

func TestDatabaseBaselineManager_ListBaselines_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectQuery("SELECT version").
		WillReturnError(errors.New("query failed"))

	versions, err := m.ListBaselines(context.Background())
	if err == nil {
		t.Error("expected an error when the list query fails")
	}
	if versions != nil {
		t.Error("expected nil versions on query error")
	}
}

func TestDatabaseBaselineManager_DeleteBaseline(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectExec("DELETE FROM schema_baselines").
		WithArgs("v1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := m.DeleteBaseline(context.Background(), "v1"); err != nil {
		t.Fatalf("DeleteBaseline failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDatabaseBaselineManager_DeleteBaseline_NotFound(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	// Zero rows affected must surface a not-found error.
	mock.ExpectExec("DELETE FROM schema_baselines").
		WithArgs("missing").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := m.DeleteBaseline(context.Background(), "missing"); err == nil {
		t.Error("expected a not-found error when no rows are deleted")
	}
}

func TestDatabaseBaselineManager_DeleteBaseline_ExecError(t *testing.T) {
	db, mock := newMockDB(t)
	m := NewDatabaseBaselineManager(db, "schema_baselines")

	mock.ExpectExec("DELETE FROM schema_baselines").
		WillReturnError(errors.New("delete failed"))

	if err := m.DeleteBaseline(context.Background(), "v1"); err == nil {
		t.Error("expected an error when the delete fails")
	}
}

// ---- FileBaselineManager (placeholder no-ops) ----

func TestFileBaselineManager_SaveAndLoad(t *testing.T) {
	m := NewFileBaselineManager("/tmp/baselines")
	if m == nil {
		t.Fatal("NewFileBaselineManager returned nil")
	}

	ctx := context.Background()
	if err := m.SaveBaseline(ctx, "v1", sampleSnapshot()); err != nil {
		t.Errorf("SaveBaseline returned unexpected error: %v", err)
	}

	// LoadBaseline returns a placeholder snapshot echoing the requested version.
	snapshot, err := m.LoadBaseline(ctx, "v1")
	if err != nil {
		t.Fatalf("LoadBaseline failed: %v", err)
	}
	if snapshot.Version != "v1" {
		t.Errorf("expected placeholder snapshot version 'v1', got %q", snapshot.Version)
	}
	if snapshot.Tables == nil {
		t.Error("expected placeholder snapshot to have an initialized Tables map")
	}
}

func TestFileBaselineManager_UnimplementedPaths(t *testing.T) {
	m := NewFileBaselineManager("/tmp/baselines")
	ctx := context.Background()

	if _, err := m.GetLatestBaseline(ctx); err == nil {
		t.Error("expected GetLatestBaseline to report not implemented")
	}
	if _, err := m.ListBaselines(ctx); err == nil {
		t.Error("expected ListBaselines to report not implemented")
	}
	// DeleteBaseline is a no-op placeholder that must not error.
	if err := m.DeleteBaseline(ctx, "v1"); err != nil {
		t.Errorf("DeleteBaseline returned unexpected error: %v", err)
	}
}

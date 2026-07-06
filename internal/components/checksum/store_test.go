/**
 * @file store_test.go
 * @description Behavior tests for the in-memory checksum store: CRUD, source
 *              lookup, filtering, pagination, statistics, cleanup, and the store
 *              factory.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/checksum/store_test.go
 */

package checksum

import (
	"context"
	"testing"
	"time"
)

func newRecord(id, source string, algo ChecksumAlgorithm) *ChecksumRecord {
	return &ChecksumRecord{
		ID:        id,
		Type:      ChecksumTypeMigration,
		Algorithm: algo,
		Value:     "deadbeef",
		Source:    source,
		Version:   "1.0",
		Tags:      []string{"t1"},
	}
}

func TestMemoryStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryChecksumStore(&ChecksumConfig{})

	rec := newRecord("id1", "V1", AlgorithmSHA256)
	if err := s.Store(ctx, rec); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// Timestamps must be populated by the store.
	if rec.CreatedAt.IsZero() || rec.UpdatedAt.IsZero() {
		t.Errorf("Store did not set timestamps")
	}

	got, err := s.Retrieve(ctx, "id1")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.Source != "V1" || got.Algorithm != AlgorithmSHA256 {
		t.Errorf("retrieved record mismatch: %+v", got)
	}

	// Retrieve returns a copy: mutating it must not affect the store.
	got.Value = "tampered"
	again, _ := s.Retrieve(ctx, "id1")
	if again.Value == "tampered" {
		t.Errorf("Retrieve leaked a mutable reference into the store")
	}

	exists, _ := s.Exists(ctx, "id1")
	if !exists {
		t.Errorf("Exists should report true")
	}

	// Update.
	rec.Value = "cafebabe"
	if err := s.Update(ctx, rec); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, _ := s.Retrieve(ctx, "id1")
	if updated.Value != "cafebabe" {
		t.Errorf("Update did not persist: %s", updated.Value)
	}

	// Delete.
	if err := s.Delete(ctx, "id1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists, _ := s.Exists(ctx, "id1"); exists {
		t.Errorf("record should be gone after delete")
	}
}

func TestMemoryStore_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryChecksumStore(&ChecksumConfig{})

	if err := s.Store(ctx, nil); err == nil {
		t.Errorf("Store(nil) should error")
	}
	if err := s.Store(ctx, &ChecksumRecord{}); err == nil {
		t.Errorf("Store with empty ID should error")
	}
	if _, err := s.Retrieve(ctx, ""); err == nil {
		t.Errorf("Retrieve(\"\") should error")
	}
	if _, err := s.Retrieve(ctx, "missing"); err == nil {
		t.Errorf("Retrieve of missing record should error")
	}
	if err := s.Update(ctx, newRecord("ghost", "V", AlgorithmMD5)); err == nil {
		t.Errorf("Update of missing record should error")
	}
	if err := s.Delete(ctx, "missing"); err == nil {
		t.Errorf("Delete of missing record should error")
	}
	if _, err := s.Exists(ctx, ""); err == nil {
		t.Errorf("Exists(\"\") should error")
	}
	if _, err := s.RetrieveBySource(ctx, "", ChecksumTypeMigration); err == nil {
		t.Errorf("RetrieveBySource with empty source should error")
	}
}

func TestMemoryStore_RetrieveBySource(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryChecksumStore(&ChecksumConfig{})

	_ = s.Store(ctx, newRecord("a", "V1", AlgorithmSHA256))
	_ = s.Store(ctx, newRecord("b", "V1", AlgorithmMD5))
	_ = s.Store(ctx, newRecord("c", "V2", AlgorithmSHA256))

	v1, err := s.RetrieveBySource(ctx, "V1", ChecksumTypeMigration)
	if err != nil {
		t.Fatalf("RetrieveBySource: %v", err)
	}
	if len(v1) != 2 {
		t.Errorf("expected 2 records for V1, got %d", len(v1))
	}

	// Wrong type -> no matches.
	none, _ := s.RetrieveBySource(ctx, "V1", ChecksumTypeFile)
	if len(none) != 0 {
		t.Errorf("expected 0 records for wrong type, got %d", len(none))
	}
}

func TestMemoryStore_ListFilterAndPagination(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryChecksumStore(&ChecksumConfig{})

	_ = s.Store(ctx, newRecord("a", "V1", AlgorithmSHA256))
	_ = s.Store(ctx, newRecord("b", "V2", AlgorithmMD5))
	_ = s.Store(ctx, newRecord("c", "V3", AlgorithmSHA256))

	// No filter -> all records.
	all, err := s.List(ctx, nil)
	if err != nil {
		t.Fatalf("List(nil): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List(nil) = %d records, want 3", len(all))
	}

	// Filter by algorithm.
	algo := AlgorithmSHA256
	sha, _ := s.List(ctx, &ChecksumFilter{Algorithm: &algo})
	if len(sha) != 2 {
		t.Errorf("List by sha256 = %d, want 2", len(sha))
	}

	// Filter by source.
	src := "V2"
	bySrc, _ := s.List(ctx, &ChecksumFilter{Source: &src})
	if len(bySrc) != 1 || bySrc[0].Source != "V2" {
		t.Errorf("List by source = %+v", bySrc)
	}

	// Filter by tag that all records have.
	byTag, _ := s.List(ctx, &ChecksumFilter{Tags: []string{"t1"}})
	if len(byTag) != 3 {
		t.Errorf("List by tag t1 = %d, want 3", len(byTag))
	}
	// Filter by a tag no record has.
	byMissingTag, _ := s.List(ctx, &ChecksumFilter{Tags: []string{"nope"}})
	if len(byMissingTag) != 0 {
		t.Errorf("List by missing tag = %d, want 0", len(byMissingTag))
	}

	// Pagination: limit 2.
	paged, _ := s.List(ctx, &ChecksumFilter{Limit: 2})
	if len(paged) != 2 {
		t.Errorf("List with limit 2 = %d records", len(paged))
	}
	// Offset beyond the set returns empty.
	beyond, _ := s.List(ctx, &ChecksumFilter{Offset: 100})
	if len(beyond) != 0 {
		t.Errorf("List with large offset = %d, want 0", len(beyond))
	}
}

func TestMemoryStore_StatisticsAndCleanup(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryChecksumStore(&ChecksumConfig{})

	old := newRecord("old", "V0", AlgorithmMD5)
	old.CreatedAt = time.Now().Add(-48 * time.Hour)
	// Insert directly to preserve the backdated CreatedAt (Store keeps a
	// non-zero CreatedAt).
	if err := s.Store(ctx, old); err != nil {
		t.Fatalf("store old: %v", err)
	}
	_ = s.Store(ctx, newRecord("new", "V1", AlgorithmSHA256))

	stats, err := s.GetStatistics(ctx)
	if err != nil {
		t.Fatalf("GetStatistics: %v", err)
	}
	if stats.TotalRecords != 2 {
		t.Errorf("TotalRecords = %d, want 2", stats.TotalRecords)
	}
	if stats.RecordsByAlgorithm[AlgorithmMD5] != 1 || stats.RecordsByAlgorithm[AlgorithmSHA256] != 1 {
		t.Errorf("RecordsByAlgorithm = %+v", stats.RecordsByAlgorithm)
	}
	if stats.RecordsByType[ChecksumTypeMigration] != 2 {
		t.Errorf("RecordsByType = %+v", stats.RecordsByType)
	}
	if stats.AverageSize <= 0 {
		t.Errorf("AverageSize should be > 0, got %f", stats.AverageSize)
	}

	// Cleanup records older than 24h -> removes the backdated "old" record.
	deleted, err := s.Cleanup(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Cleanup deleted %d, want 1", deleted)
	}
	if exists, _ := s.Exists(ctx, "old"); exists {
		t.Errorf("old record should have been cleaned up")
	}
}

func TestStoreFactory(t *testing.T) {
	f := NewStoreFactory()

	supported := f.GetSupportedStoreTypes()
	if len(supported) != 2 {
		t.Errorf("expected 2 store types, got %d", len(supported))
	}

	mem, err := f.CreateStore(StoreTypeMemory, nil)
	if err != nil || mem == nil {
		t.Fatalf("CreateStore(memory): %v", err)
	}

	// Database store without a backend must error.
	if _, err := f.CreateStore(StoreTypeDatabase, &StoreConfig{}); err == nil {
		t.Errorf("database store without backend should error")
	}

	// Unknown store type errors.
	if _, err := f.CreateStore(StoreType("bogus"), nil); err == nil {
		t.Errorf("unknown store type should error")
	}

	if f.GetDefaultConfig(StoreTypeDatabase).TableName != "checksum_records" {
		t.Errorf("default database config should set table name")
	}
}

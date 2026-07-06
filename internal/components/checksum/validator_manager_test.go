/**
 * @file validator_manager_test.go
 * @description Behavior tests for the checksum validator (data/migration/file/
 *              batch), the high-level checksum manager (generate+store, get-or-
 *              create, validate+update, integrity check, cache) and the audit
 *              subsystem driven through an in-memory audit store.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/checksum/validator_manager_test.go
 */

package checksum

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func newValidator(t *testing.T) (*DefaultChecksumValidator, *DefaultChecksumGenerator, *MemoryChecksumStore) {
	t.Helper()
	gen := NewDefaultChecksumGenerator()
	store := NewMemoryChecksumStore(&ChecksumConfig{})
	v := NewDefaultChecksumValidator(gen, store, &ChecksumConfig{BatchSize: 2})
	return v, gen, store
}

func TestValidator_ValidateData(t *testing.T) {
	ctx := context.Background()
	v, gen, _ := newValidator(t)

	data := []byte("payload")
	sum, _ := gen.Generate(data, AlgorithmSHA256)

	rec := &ChecksumRecord{ID: "r1", Algorithm: AlgorithmSHA256, Value: sum, Source: "s1"}

	// Matching data -> valid.
	res, err := v.Validate(ctx, rec, data)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusValid {
		t.Errorf("expected StatusValid, got %s (%s)", res.Status, res.Message)
	}
	if res.ActualValue != sum {
		t.Errorf("ActualValue = %s, want %s", res.ActualValue, sum)
	}

	// Tampered data -> invalid with suggestions.
	res2, err := v.Validate(ctx, rec, []byte("tampered"))
	if err != nil {
		t.Fatalf("Validate (tampered): %v", err)
	}
	if res2.Status != StatusInvalid {
		t.Errorf("expected StatusInvalid, got %s", res2.Status)
	}
	if len(res2.Suggestions) == 0 {
		t.Errorf("invalid result should include suggestions")
	}
}

func TestValidator_ValidateData_UnsupportedAlgorithm(t *testing.T) {
	ctx := context.Background()
	v, _, _ := newValidator(t)
	rec := &ChecksumRecord{ID: "r", Algorithm: ChecksumAlgorithm("nope"), Value: "x"}
	if _, err := v.Validate(ctx, rec, []byte("d")); err == nil {
		t.Errorf("Validate with unsupported algorithm should error")
	}
}

func TestValidator_ValidateMigration(t *testing.T) {
	ctx := context.Background()
	v, gen, store := newValidator(t)

	m := &types.Migration{ID: "V1", Version: "1", Script: "SELECT 1"}
	sum, _ := gen.GenerateFromMigration(m, AlgorithmSHA256)

	// Store the expected checksum for the migration source.
	_ = store.Store(ctx, &ChecksumRecord{
		ID:        "rec-V1",
		Type:      ChecksumTypeMigration,
		Algorithm: AlgorithmSHA256,
		Value:     sum,
		Source:    "V1",
		CreatedAt: time.Now(),
	})

	res, err := v.ValidateMigration(ctx, m)
	if err != nil {
		t.Fatalf("ValidateMigration: %v", err)
	}
	if res.Status != StatusValid {
		t.Errorf("expected valid, got %s: %s", res.Status, res.Message)
	}

	// Missing checksum record -> StatusMissing.
	unknown := &types.Migration{ID: "V999", Script: "x"}
	resMissing, err := v.ValidateMigration(ctx, unknown)
	if err != nil {
		t.Fatalf("ValidateMigration (missing): %v", err)
	}
	if resMissing.Status != StatusMissing {
		t.Errorf("expected StatusMissing, got %s", resMissing.Status)
	}

	// Nil migration -> error.
	if _, err := v.ValidateMigration(ctx, nil); err == nil {
		t.Errorf("ValidateMigration(nil) should error")
	}

	// Drifted migration -> invalid.
	drift := &types.Migration{ID: "V1", Version: "1", Script: "SELECT 2"}
	resDrift, _ := v.ValidateMigration(ctx, drift)
	if resDrift.Status != StatusInvalid {
		t.Errorf("expected invalid for drifted migration, got %s", resDrift.Status)
	}
}

func TestValidator_ValidateFile(t *testing.T) {
	ctx := context.Background()
	v, gen, _ := newValidator(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "f.sql")
	content := []byte("CREATE TABLE t(id INT);")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	sum, _ := gen.Generate(content, AlgorithmSHA256)

	res, err := v.ValidateFile(ctx, path, sum, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if res.Status != StatusValid {
		t.Errorf("expected valid file checksum, got %s", res.Status)
	}

	// Wrong expected checksum -> invalid.
	resBad, _ := v.ValidateFile(ctx, path, "wrong", AlgorithmSHA256)
	if resBad.Status != StatusInvalid {
		t.Errorf("expected invalid, got %s", resBad.Status)
	}

	// Missing file -> error.
	if _, err := v.ValidateFile(ctx, filepath.Join(dir, "missing"), sum, AlgorithmSHA256); err == nil {
		t.Errorf("ValidateFile of missing file should error")
	}
}

func TestValidator_ValidateBatch(t *testing.T) {
	ctx := context.Background()
	v, gen, _ := newValidator(t)

	d1 := []byte("one")
	d2 := []byte("two")
	s1, _ := gen.Generate(d1, AlgorithmSHA256)
	s2, _ := gen.Generate(d2, AlgorithmSHA256)

	records := []*ChecksumRecord{
		{ID: "r1", Algorithm: AlgorithmSHA256, Value: s1, Source: "one"},
		{ID: "r2", Algorithm: AlgorithmSHA256, Value: s2, Source: "two"},
	}
	dataMap := map[string][]byte{"one": d1, "two": d2}

	results, err := v.ValidateBatch(ctx, records, dataMap)
	if err != nil {
		t.Fatalf("ValidateBatch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != StatusValid {
			t.Errorf("record %s invalid: %s", r.Record.ID, r.Message)
		}
	}

	// Empty records -> empty slice, no error.
	empty, err := v.ValidateBatch(ctx, nil, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty batch = %v %v", empty, err)
	}

	// Missing data for a source -> reported as an error.
	_, err = v.ValidateBatch(ctx, records, map[string][]byte{"one": d1})
	if err == nil {
		t.Errorf("batch with missing data should return an aggregated error")
	}
}

func TestParallelValidator_ValidateBatch(t *testing.T) {
	ctx := context.Background()
	gen := NewDefaultChecksumGenerator()
	store := NewMemoryChecksumStore(&ChecksumConfig{})
	pv := NewParallelChecksumValidator(gen, store, &ChecksumConfig{}, 2)

	d := []byte("x")
	s, _ := gen.Generate(d, AlgorithmSHA256)
	records := []*ChecksumRecord{{ID: "r1", Algorithm: AlgorithmSHA256, Value: s, Source: "x"}}

	results, err := pv.ValidateBatch(ctx, records, map[string][]byte{"x": d})
	if err != nil {
		t.Fatalf("parallel ValidateBatch: %v", err)
	}
	if len(results) != 1 || results[0].Status != StatusValid {
		t.Errorf("parallel batch result mismatch: %+v", results)
	}

	empty, err := pv.ValidateBatch(ctx, nil, nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty parallel batch = %v %v", empty, err)
	}
}

func TestValidatorFactory(t *testing.T) {
	f := NewValidatorFactory()
	if len(f.GetSupportedValidatorTypes()) != 2 {
		t.Errorf("expected 2 validator types")
	}
	for _, vt := range f.GetSupportedValidatorTypes() {
		v, err := f.CreateValidator(vt, f.GetDefaultConfig(vt))
		if err != nil || v == nil {
			t.Errorf("CreateValidator(%s): %v", vt, err)
		}
	}
	if _, err := f.CreateValidator(ValidatorType("bogus"), &ValidatorConfig{}); err == nil {
		t.Errorf("unknown validator type should error")
	}
}

// --- Manager ---

func newManager(t *testing.T, caching bool) *DefaultChecksumManager {
	t.Helper()
	gen := NewDefaultChecksumGenerator()
	store := NewMemoryChecksumStore(&ChecksumConfig{})
	cfg := &ChecksumConfig{
		DefaultAlgorithm: AlgorithmSHA256,
		EnableCaching:    caching,
		CacheSize:        16,
		CacheTTL:         time.Hour,
		BatchSize:        10,
	}
	validator := NewDefaultChecksumValidator(gen, store, cfg)
	return NewDefaultChecksumManager(gen, validator, store, cfg)
}

func TestManager_GenerateAndStore(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, true)

	rec, err := m.GenerateAndStore(ctx, "V1", []byte("data"), ChecksumTypeMigration, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("GenerateAndStore: %v", err)
	}
	if rec.ID == "" || rec.Value == "" || rec.Source != "V1" {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.Metadata["data_size"] != 4 {
		t.Errorf("data_size metadata = %v, want 4", rec.Metadata["data_size"])
	}

	// Cached checksum is retrievable.
	cached, err := m.GetCachedChecksum("V1")
	if err != nil {
		t.Fatalf("GetCachedChecksum: %v", err)
	}
	if cached == nil || cached.ID != rec.ID {
		t.Errorf("cache miss for V1 after store")
	}
}

func TestManager_GetOrCreate(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, true)

	// First call creates.
	r1, err := m.GetOrCreate(ctx, "V1", []byte("data"), ChecksumTypeMigration, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("GetOrCreate (create): %v", err)
	}
	// Second call returns the cached/stored record (no duplicate).
	r2, err := m.GetOrCreate(ctx, "V1", []byte("data"), ChecksumTypeMigration, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("GetOrCreate (reuse): %v", err)
	}
	if r1.ID != r2.ID {
		t.Errorf("GetOrCreate created a duplicate: %s != %s", r1.ID, r2.ID)
	}
}

func TestManager_ValidateAndUpdate(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, true)

	data := []byte("data")
	if _, err := m.GenerateAndStore(ctx, "V1", data, ChecksumTypeMigration, AlgorithmSHA256); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Same data -> valid.
	res, err := m.ValidateAndUpdate(ctx, "V1", data, ChecksumTypeMigration)
	if err != nil {
		t.Fatalf("ValidateAndUpdate: %v", err)
	}
	if res.Status != StatusValid {
		t.Errorf("expected valid, got %s", res.Status)
	}

	// Unknown source -> missing.
	resMissing, err := m.ValidateAndUpdate(ctx, "unknown", data, ChecksumTypeMigration)
	if err != nil {
		t.Fatalf("ValidateAndUpdate (missing): %v", err)
	}
	if resMissing.Status != StatusMissing {
		t.Errorf("expected missing, got %s", resMissing.Status)
	}

	// Changed data with caching enabled -> auto-update repairs to valid.
	resUpdate, err := m.ValidateAndUpdate(ctx, "V1", []byte("changed"), ChecksumTypeMigration)
	if err != nil {
		t.Fatalf("ValidateAndUpdate (changed): %v", err)
	}
	if resUpdate.Status != StatusValid {
		t.Errorf("expected auto-update to yield valid, got %s", resUpdate.Status)
	}
}

func TestManager_InvalidateCache_NoCaching(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, false)

	if _, err := m.GenerateAndStore(ctx, "V1", []byte("x"), ChecksumTypeMigration, AlgorithmSHA256); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Without caching, InvalidateCache is a no-op success and GetCachedChecksum errors.
	if err := m.InvalidateCache("V1"); err != nil {
		t.Errorf("InvalidateCache with no cache should be nil, got %v", err)
	}
	if _, err := m.GetCachedChecksum("V1"); err == nil {
		t.Errorf("GetCachedChecksum should error when caching is disabled")
	}
}

func TestManager_InvalidateCache_WithCaching(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, true)
	if _, err := m.GenerateAndStore(ctx, "V1", []byte("x"), ChecksumTypeMigration, AlgorithmSHA256); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := m.InvalidateCache("V1"); err != nil {
		t.Fatalf("InvalidateCache: %v", err)
	}
	cached, _ := m.GetCachedChecksum("V1")
	if cached != nil {
		t.Errorf("cache entry should be gone after InvalidateCache")
	}
}

func TestManager_BulkValidate(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, true)

	d1 := []byte("one")
	d2 := []byte("two")
	_, _ = m.GenerateAndStore(ctx, "V1", d1, ChecksumTypeMigration, AlgorithmSHA256)
	_, _ = m.GenerateAndStore(ctx, "V2", d2, ChecksumTypeMigration, AlgorithmSHA256)

	results, err := m.BulkValidate(ctx, []string{"V1", "V2"},
		map[string][]byte{"V1": d1, "V2": d2}, ChecksumTypeMigration)
	if err != nil {
		t.Fatalf("BulkValidate: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != StatusValid {
			t.Errorf("bulk result invalid: %+v", r)
		}
	}

	// Empty sources -> empty slice.
	empty, err := m.BulkValidate(ctx, nil, nil, ChecksumTypeMigration)
	if err != nil || len(empty) != 0 {
		t.Errorf("empty bulk = %v %v", empty, err)
	}
}

func TestManager_PerformIntegrityCheck(t *testing.T) {
	ctx := context.Background()
	m := newManager(t, false)

	_, _ = m.GenerateAndStore(ctx, "V1", []byte("x"), ChecksumTypeMigration, AlgorithmSHA256)

	result, err := m.PerformIntegrityCheck(ctx)
	if err != nil {
		t.Fatalf("PerformIntegrityCheck: %v", err)
	}
	if result.TotalChecked != 1 {
		t.Errorf("TotalChecked = %d, want 1", result.TotalChecked)
	}
	if result.ValidRecords != 1 {
		t.Errorf("ValidRecords = %d, want 1", result.ValidRecords)
	}
	if result.CheckDuration < 0 {
		t.Errorf("CheckDuration should be non-negative")
	}
}

func TestManagerFactory(t *testing.T) {
	f := NewManagerFactory()
	cfg := f.CreateDefaultConfig()
	if cfg.DefaultAlgorithm != AlgorithmSHA256 {
		t.Errorf("default algorithm = %s", cfg.DefaultAlgorithm)
	}
	gen := NewDefaultChecksumGenerator()
	store := NewMemoryChecksumStore(cfg)
	validator := NewDefaultChecksumValidator(gen, store, cfg)
	mgr := f.CreateManager(gen, validator, store, cfg)
	if mgr == nil {
		t.Errorf("CreateManager returned nil")
	}
}

// --- Auditor via in-memory AuditStore ---

// memAuditStore is an in-memory AuditStore for exercising the auditor without a
// real database.
type memAuditStore struct {
	mu      sync.Mutex
	entries []*AuditEntry
}

func (m *memAuditStore) Store(ctx context.Context, e *AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}
func (m *memAuditStore) Retrieve(ctx context.Context, id string) (*AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, nil
}
func (m *memAuditStore) List(ctx context.Context, f *AuditFilter) ([]*AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*AuditEntry
	for _, e := range m.entries {
		if f != nil && f.RecordID != "" && e.RecordID != f.RecordID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
func (m *memAuditStore) Delete(ctx context.Context, id string) error { return nil }
func (m *memAuditStore) Cleanup(ctx context.Context, before time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.entries[:0]
	for _, e := range m.entries {
		if e.Timestamp.After(before) {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	return nil
}
func (m *memAuditStore) GetStatistics(ctx context.Context) (*AuditStatistics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &AuditStatistics{TotalEntries: len(m.entries)}, nil
}
func (m *memAuditStore) Initialize(ctx context.Context) error { return nil }

func newAuditor() (*DefaultChecksumAuditor, *memAuditStore) {
	store := &memAuditStore{}
	cfg := &AuditConfig{
		EnableBuffering: false, // synchronous store for deterministic tests
		LogLevel:        AuditLogLevelInfo,
		RetentionPeriod: time.Hour,
		MaxEntrySize:    1 << 20,
	}
	return NewDefaultChecksumAuditor(store, cfg), store
}

func TestAuditor_LogAndRetrieve(t *testing.T) {
	ctx := context.Background()
	auditor, store := newAuditor()

	if err := auditor.LogOperation(ctx, "generate", "R1", map[string]interface{}{"k": "v"}); err != nil {
		t.Fatalf("LogOperation: %v", err)
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected 1 stored entry, got %d", len(store.entries))
	}

	trail, err := auditor.GetAuditTrail(ctx, "R1")
	if err != nil {
		t.Fatalf("GetAuditTrail: %v", err)
	}
	if len(trail) != 1 || trail[0].Operation != "generate" {
		t.Errorf("unexpected trail: %+v", trail)
	}

	// LogValidation records a warn level for invalid results.
	if err := auditor.LogValidation(ctx, "R1", &ChecksumValidationResult{Status: StatusInvalid}); err != nil {
		t.Fatalf("LogValidation: %v", err)
	}
	// LogError records an error entry.
	if err := auditor.LogError(ctx, "validate", "R1", os.ErrNotExist, nil); err != nil {
		t.Fatalf("LogError: %v", err)
	}

	stats, err := auditor.GetAuditStatistics(ctx)
	if err != nil {
		t.Fatalf("GetAuditStatistics: %v", err)
	}
	if stats.TotalEntries != 3 {
		t.Errorf("TotalEntries = %d, want 3", stats.TotalEntries)
	}
}

func TestAuditor_LevelFiltering(t *testing.T) {
	ctx := context.Background()
	store := &memAuditStore{}
	// Only error-level entries should be stored.
	cfg := &AuditConfig{LogLevel: AuditLogLevelError, MaxEntrySize: 1 << 20}
	auditor := NewDefaultChecksumAuditor(store, cfg)

	// Info-level operation is below threshold -> dropped.
	_ = auditor.LogOperation(ctx, "generate", "R1", nil)
	if len(store.entries) != 0 {
		t.Errorf("info entry should be filtered out at error log level, got %d", len(store.entries))
	}

	// Error-level entry is stored.
	_ = auditor.LogError(ctx, "op", "R1", os.ErrClosed, nil)
	if len(store.entries) != 1 {
		t.Errorf("error entry should be stored, got %d", len(store.entries))
	}
}

func TestAuditor_ArchiveOldEntries(t *testing.T) {
	ctx := context.Background()
	auditor, store := newAuditor()

	// Seed one old and one recent entry directly.
	store.entries = []*AuditEntry{
		{ID: "old", Timestamp: time.Now().Add(-48 * time.Hour)},
		{ID: "new", Timestamp: time.Now()},
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	count, err := auditor.ArchiveOldEntries(ctx, cutoff)
	if err != nil {
		t.Fatalf("ArchiveOldEntries: %v", err)
	}
	// List with EndTime==cutoff returns everything in our simple store, so the
	// archived count reflects what List returned. Assert the store retained
	// only the recent entry after cleanup.
	if count < 0 {
		t.Errorf("archived count should be non-negative, got %d", count)
	}
	remaining, _ := store.List(ctx, &AuditFilter{})
	for _, e := range remaining {
		if e.ID == "old" {
			t.Errorf("old entry should have been cleaned up")
		}
	}
}

func TestAuditor_LevelToString(t *testing.T) {
	a := &DefaultChecksumAuditor{}
	cases := map[AuditLogLevel]string{
		AuditLogLevelDebug: "debug",
		AuditLogLevelInfo:  "info",
		AuditLogLevelWarn:  "warn",
		AuditLogLevelError: "error",
		AuditLogLevel(99):  "unknown",
	}
	for level, want := range cases {
		if got := a.levelToString(level); got != want {
			t.Errorf("levelToString(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestAuditorFactory(t *testing.T) {
	f := NewAuditorFactory()
	cfg := f.CreateDefaultConfig()
	if cfg.BufferSize <= 0 {
		t.Errorf("default buffer size should be positive")
	}
	auditor := f.CreateAuditor(&memAuditStore{}, cfg)
	if auditor == nil {
		t.Errorf("CreateAuditor returned nil")
	}
}

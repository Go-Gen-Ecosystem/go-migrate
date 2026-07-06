/**
 * @file detector_index_constraint_test.go
 * @description Behavioral tests for index- and constraint-level drift
 *              detection, the compensating-migration guard for non-fixable
 *              drift, column matching on length, and the SQL generation helpers
 *              for indexes and constraints. These drive real DetectDrift runs
 *              over crafted schema snapshots to exercise the added/dropped
 *              branches and the fix-suggestion generators.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/drift/detector_index_constraint_test.go
 */

package drift

import (
	"context"
	"strings"
	"testing"
	"time"
)

func baseSnapshot() *SchemaSnapshot {
	return &SchemaSnapshot{
		Version:     "v1",
		Timestamp:   time.Now(),
		Tables:      map[string]*TableSchema{},
		Indexes:     map[string]*IndexSchema{},
		Constraints: map[string]*ConstraintSchema{},
	}
}

func TestDetectDrift_IndexAdded(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := baseSnapshot()
	current := baseSnapshot()
	current.Indexes["idx_users_email"] = &IndexSchema{
		Name:      "idx_users_email",
		TableName: "users",
		Columns:   []string{"email"},
		Unique:    true,
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Fatalf("expected 1 drift, got %d", report.Summary.TotalDrifts)
	}
	drift := report.Drifts[0]
	if drift.Type != DriftIndexAdded {
		t.Errorf("expected %s, got %s", DriftIndexAdded, drift.Type)
	}
	if drift.Severity != SeverityInfo {
		t.Errorf("expected info severity for index add, got %s", drift.Severity)
	}
	// indexToMap output must surface on the Actual side.
	if drift.Actual["name"] != "idx_users_email" {
		t.Errorf("expected actual index name in drift, got %v", drift.Actual["name"])
	}
	if !strings.Contains(drift.FixSuggestion, "DROP INDEX") {
		t.Errorf("expected DROP INDEX fix suggestion, got %q", drift.FixSuggestion)
	}
}

func TestDetectDrift_IndexDropped_GeneratesCreateSQL(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := baseSnapshot()
	baseline.Indexes["idx_users_email"] = &IndexSchema{
		Name:      "idx_users_email",
		TableName: "users",
		Columns:   []string{"email", "tenant_id"},
		Unique:    true,
	}
	current := baseSnapshot()

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Fatalf("expected 1 drift, got %d", report.Summary.TotalDrifts)
	}
	drift := report.Drifts[0]
	if drift.Type != DriftIndexDropped {
		t.Errorf("expected %s, got %s", DriftIndexDropped, drift.Type)
	}
	// The literal severity is set to Warning, but detectIndexDrifts then calls
	// GetDriftSeverity, which classifies both index add and drop as Info. The
	// final, authoritative severity is therefore Info.
	if drift.Severity != SeverityInfo {
		t.Errorf("expected info severity for index drop (GetDriftSeverity override), got %s", drift.Severity)
	}
	// generateCreateIndexSQL builds a UNIQUE index with joined columns.
	fix := drift.FixSuggestion
	if !strings.Contains(fix, "CREATE UNIQUE INDEX") {
		t.Errorf("expected CREATE UNIQUE INDEX in fix, got %q", fix)
	}
	if !strings.Contains(fix, "email, tenant_id") {
		t.Errorf("expected joined columns in fix, got %q", fix)
	}
	if !strings.Contains(fix, "users") {
		t.Errorf("expected table name in fix, got %q", fix)
	}
}

func TestDetectDrift_ConstraintAdded(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := baseSnapshot()
	current := baseSnapshot()
	current.Constraints["uq_users_email"] = &ConstraintSchema{
		Name:      "uq_users_email",
		TableName: "users",
		Type:      "unique",
		Columns:   []string{"email"},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Fatalf("expected 1 drift, got %d", report.Summary.TotalDrifts)
	}
	drift := report.Drifts[0]
	if drift.Type != DriftConstraintAdded {
		t.Errorf("expected %s, got %s", DriftConstraintAdded, drift.Type)
	}
	// constraintToMap output must surface on the Actual side.
	if drift.Actual["type"] != "unique" {
		t.Errorf("expected actual constraint type 'unique', got %v", drift.Actual["type"])
	}
	if !strings.Contains(drift.FixSuggestion, "DROP CONSTRAINT") {
		t.Errorf("expected DROP CONSTRAINT fix, got %q", drift.FixSuggestion)
	}
}

func TestDetectDrift_ConstraintDropped_ForeignKeyFix(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := baseSnapshot()
	baseline.Constraints["fk_orders_user"] = &ConstraintSchema{
		Name:              "fk_orders_user",
		TableName:         "orders",
		Type:              "foreign_key",
		Columns:           []string{"user_id"},
		ReferencedTable:   "users",
		ReferencedColumns: []string{"id"},
	}
	current := baseSnapshot()

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Fatalf("expected 1 drift, got %d", report.Summary.TotalDrifts)
	}
	drift := report.Drifts[0]
	if drift.Type != DriftConstraintDropped {
		t.Errorf("expected %s, got %s", DriftConstraintDropped, drift.Type)
	}
	if drift.Severity != SeverityCritical {
		t.Errorf("expected critical severity for constraint drop, got %s", drift.Severity)
	}
	// generateAddConstraintSQL emits a FOREIGN KEY ... REFERENCES clause.
	fix := drift.FixSuggestion
	if !strings.Contains(fix, "FOREIGN KEY") || !strings.Contains(fix, "REFERENCES users") {
		t.Errorf("expected FK REFERENCES fix, got %q", fix)
	}
}

// TestGenerateAddConstraintSQL_AllBranches exercises every branch of the
// constraint-fix generator directly, since crafting drift snapshots for each
// type would be unwieldy.
func TestGenerateAddConstraintSQL_AllBranches(t *testing.T) {
	d := NewDriftDetector(nil)

	pk := d.generateAddConstraintSQL(&ConstraintSchema{
		Name: "pk_users", TableName: "users", Type: "primary_key", Columns: []string{"id"},
	})
	if !strings.Contains(pk, "PRIMARY KEY (id)") {
		t.Errorf("primary key: got %q", pk)
	}

	uq := d.generateAddConstraintSQL(&ConstraintSchema{
		Name: "uq_email", TableName: "users", Type: "unique", Columns: []string{"email"},
	})
	if !strings.Contains(uq, "UNIQUE (email)") {
		t.Errorf("unique: got %q", uq)
	}

	// Unknown type with a definition falls back to the raw definition.
	chk := d.generateAddConstraintSQL(&ConstraintSchema{
		Name: "chk_age", TableName: "users", Type: "check", Columns: []string{"age"},
		Definition: "CHECK (age >= 0)",
	})
	if !strings.Contains(chk, "CHECK (age >= 0)") {
		t.Errorf("check with definition: got %q", chk)
	}

	// Unknown type without a definition yields a commented placeholder.
	unknown := d.generateAddConstraintSQL(&ConstraintSchema{
		Name: "weird", TableName: "users", Type: "exclusion", Columns: []string{"c"},
	})
	if !strings.HasPrefix(unknown, "-- Cannot auto-generate") {
		t.Errorf("unknown without definition: got %q", unknown)
	}
}

// TestColumnsMatch_MaxLength verifies the length comparison branch of
// columnsMatch: identical type and nullability but differing max length must be
// treated as a modification.
func TestColumnsMatch_MaxLength(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name:    "users",
				Columns: []ColumnSchema{{Name: "email", DataType: "varchar", IsNullable: false, MaxLength: 255}},
			},
		},
	}
	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name:    "users",
				Columns: []ColumnSchema{{Name: "email", DataType: "varchar", IsNullable: false, MaxLength: 512}},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}
	if report.Summary.TotalDrifts != 1 {
		t.Fatalf("expected 1 drift from max-length change, got %d", report.Summary.TotalDrifts)
	}
	if report.Drifts[0].Type != DriftColumnModified {
		t.Errorf("expected %s, got %s", DriftColumnModified, report.Drifts[0].Type)
	}
}

func TestGenerateCompensatingMigration_NonFixableErrors(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:        DriftColumnDropped,
		ObjectName:  "email",
		Description: "Column dropped",
		CanAutoFix:  false,
	}

	migration, err := detector.GenerateCompensatingMigration(context.Background(), drift)
	if err == nil {
		t.Error("expected error for non-fixable drift")
	}
	if migration != nil {
		t.Error("expected nil migration for non-fixable drift")
	}
}

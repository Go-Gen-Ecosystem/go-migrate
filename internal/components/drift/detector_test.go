/**
 * @file detector_test.go
 * @description Unit tests for drift detector
 * @author Claude Code
 * @date 2025-01-01
 */

package drift

import (
	"context"
	"testing"
	"time"
)

func TestNewDriftDetector(t *testing.T) {
	detector := NewDriftDetector(nil)

	if detector == nil {
		t.Fatal("NewDriftDetector returned nil")
	}

	if detector.config == nil {
		t.Error("Detector config should not be nil")
	}
}

func TestDetectDrift_NoChanges(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
					{Name: "email", DataType: "varchar", IsNullable: false},
				},
			},
		},
	}

	current := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
					{Name: "email", DataType: "varchar", IsNullable: false},
				},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 0 {
		t.Errorf("Expected 0 drifts, got %d", report.Summary.TotalDrifts)
	}
}

func TestDetectDrift_TableAdded(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables:    map[string]*TableSchema{},
	}

	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"products": {
				Name: "products",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
				},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Errorf("Expected 1 drift, got %d", report.Summary.TotalDrifts)
	}

	if len(report.Drifts) != 1 {
		t.Fatalf("Expected 1 drift in list, got %d", len(report.Drifts))
	}

	drift := report.Drifts[0]
	if drift.Type != DriftTableAdded {
		t.Errorf("Expected drift type %s, got %s", DriftTableAdded, drift.Type)
	}

	if drift.ObjectName != "products" {
		t.Errorf("Expected object name 'products', got '%s'", drift.ObjectName)
	}
}

func TestDetectDrift_TableDropped(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"old_table": {
				Name: "old_table",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
				},
			},
		},
	}

	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables:    map[string]*TableSchema{},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Errorf("Expected 1 drift, got %d", report.Summary.TotalDrifts)
	}

	drift := report.Drifts[0]
	if drift.Type != DriftTableDropped {
		t.Errorf("Expected drift type %s, got %s", DriftTableDropped, drift.Type)
	}

	if drift.Severity != SeverityCritical {
		t.Errorf("Expected critical severity, got %s", drift.Severity)
	}
}

func TestDetectDrift_ColumnAdded(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
				},
			},
		},
	}

	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
					{Name: "email", DataType: "varchar", IsNullable: true},
				},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Errorf("Expected 1 drift, got %d", report.Summary.TotalDrifts)
	}

	drift := report.Drifts[0]
	if drift.Type != DriftColumnAdded {
		t.Errorf("Expected drift type %s, got %s", DriftColumnAdded, drift.Type)
	}

	if drift.ObjectName != "email" {
		t.Errorf("Expected column name 'email', got '%s'", drift.ObjectName)
	}
}

func TestDetectDrift_ColumnDropped(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
					{Name: "email", DataType: "varchar", IsNullable: false},
				},
			},
		},
	}

	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
				},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Errorf("Expected 1 drift, got %d", report.Summary.TotalDrifts)
	}

	drift := report.Drifts[0]
	if drift.Type != DriftColumnDropped {
		t.Errorf("Expected drift type %s, got %s", DriftColumnDropped, drift.Type)
	}

	if drift.Severity != SeverityCritical {
		t.Errorf("Expected critical severity for column drop, got %s", drift.Severity)
	}
}

func TestDetectDrift_ColumnModified(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "email", DataType: "varchar", IsNullable: false},
				},
			},
		},
	}

	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "email", DataType: "varchar", IsNullable: true},
				},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 1 {
		t.Errorf("Expected 1 drift, got %d", report.Summary.TotalDrifts)
	}

	drift := report.Drifts[0]
	if drift.Type != DriftColumnModified {
		t.Errorf("Expected drift type %s, got %s", DriftColumnModified, drift.Type)
	}
}

func TestGetDriftSeverity_TableDropped(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftTableDropped,
		ObjectType: "table",
		ObjectName: "users",
	}

	severity := detector.GetDriftSeverity(drift)
	if severity != SeverityCritical {
		t.Errorf("Expected critical severity for table drop, got %s", severity)
	}
}

func TestGetDriftSeverity_ColumnDropped(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftColumnDropped,
		ObjectType: "column",
		ObjectName: "email",
	}

	severity := detector.GetDriftSeverity(drift)
	if severity != SeverityCritical {
		t.Errorf("Expected critical severity for column drop, got %s", severity)
	}
}

func TestGetDriftSeverity_TableAdded(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftTableAdded,
		ObjectType: "table",
		ObjectName: "products",
	}

	severity := detector.GetDriftSeverity(drift)
	if severity != SeverityWarning {
		t.Errorf("Expected warning severity for table add, got %s", severity)
	}
}

func TestGetDriftSeverity_IndexAdded(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftIndexAdded,
		ObjectType: "index",
		ObjectName: "idx_email",
	}

	severity := detector.GetDriftSeverity(drift)
	if severity != SeverityInfo {
		t.Errorf("Expected info severity for index add, got %s", severity)
	}
}

func TestShouldIgnoreDrift_NoPatterns(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftTableAdded,
		ObjectType: "table",
		ObjectName: "temp_table",
	}

	shouldIgnore := detector.ShouldIgnoreDrift(drift, nil)
	if shouldIgnore {
		t.Error("Should not ignore drift with no patterns")
	}
}

func TestShouldIgnoreDrift_MatchingPattern(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftTableAdded,
		ObjectType: "table",
		ObjectName: "temp_table",
	}

	patterns := []string{"^temp_.*"}
	shouldIgnore := detector.ShouldIgnoreDrift(drift, patterns)
	if !shouldIgnore {
		t.Error("Should ignore drift matching temp_ pattern")
	}
}

func TestShouldIgnoreDrift_NoMatch(t *testing.T) {
	detector := NewDriftDetector(nil)

	drift := &Drift{
		Type:       DriftTableAdded,
		ObjectType: "table",
		ObjectName: "users",
	}

	patterns := []string{"^temp_.*"}
	shouldIgnore := detector.ShouldIgnoreDrift(drift, patterns)
	if shouldIgnore {
		t.Error("Should not ignore drift not matching temp_ pattern")
	}
}

func TestGenerateCompensatingMigration_TableAdded(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	drift := &Drift{
		Type:          DriftTableAdded,
		ObjectType:    "table",
		ObjectName:    "products",
		CanAutoFix:    true,
		FixSuggestion: "DROP TABLE products;",
	}

	migration, err := detector.GenerateCompensatingMigration(ctx, drift)
	if err != nil {
		t.Fatalf("GenerateCompensatingMigration failed: %v", err)
	}

	if migration == nil {
		t.Fatal("Migration is nil")
	}

	if migration.Script != "DROP TABLE products;" {
		t.Errorf("Expected DROP TABLE script, got: %s", migration.Script)
	}
}

func TestDetectDrift_MultipleDrifts(t *testing.T) {
	detector := NewDriftDetector(nil)
	ctx := context.Background()

	baseline := &SchemaSnapshot{
		Version:   "v1",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
					{Name: "email", DataType: "varchar", IsNullable: false},
				},
			},
		},
	}

	current := &SchemaSnapshot{
		Version:   "v2",
		Timestamp: time.Now(),
		Tables: map[string]*TableSchema{
			"users": {
				Name: "users",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
					{Name: "username", DataType: "varchar", IsNullable: false},
				},
			},
			"products": {
				Name: "products",
				Columns: []ColumnSchema{
					{Name: "id", DataType: "integer", IsNullable: false},
				},
			},
		},
	}

	report, err := detector.DetectDrift(ctx, baseline, current)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}

	if report.Summary.TotalDrifts != 3 {
		t.Errorf("Expected 3 drifts (table added, column dropped, column added), got %d", report.Summary.TotalDrifts)
	}

	if report.Summary.CriticalCount != 1 {
		t.Errorf("Expected 1 critical drift (column dropped), got %d", report.Summary.CriticalCount)
	}

	if report.Summary.WarningCount != 2 {
		t.Errorf("Expected 2 warning drifts (table added, column added), got %d", report.Summary.WarningCount)
	}

	if report.Summary.InfoCount != 0 {
		t.Errorf("Expected 0 info drifts, got %d", report.Summary.InfoCount)
	}
}

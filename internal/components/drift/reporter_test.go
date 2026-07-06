/**
 * @file reporter_test.go
 * @description Unit tests for drift reporter
 * @author Claude Code
 * @date 2025-01-01
 */

package drift

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewDefaultDriftReporter(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)

	if reporter == nil {
		t.Fatal("NewDefaultDriftReporter returned nil")
	}

	if reporter.config == nil {
		t.Error("Reporter config should not be nil")
	}

	if !reporter.config.ColorEnabled {
		t.Error("ColorEnabled should be true by default")
	}

	if !reporter.config.ShowFixSuggestions {
		t.Error("ShowFixSuggestions should be true by default")
	}
}

func TestGenerateReport_NoDrift(t *testing.T) {
	reporter := NewDefaultDriftReporter(&ReportConfig{
		ColorEnabled:       false,
		ShowFixSuggestions: true,
		GroupBySeverity:    true,
		IncludeTimestamp:   true,
	})

	ctx := context.Background()
	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v1",
		Summary: DriftSummary{
			TotalDrifts:      0,
			CriticalCount:    0,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{},
	}

	text, err := reporter.GenerateReport(ctx, report)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if !strings.Contains(text, "DATABASE SCHEMA DRIFT DETECTION REPORT") {
		t.Error("Report should contain header")
	}

	if !strings.Contains(text, "No schema drift detected") {
		t.Error("Report should indicate no drift")
	}

	if !strings.Contains(text, "testdb") {
		t.Error("Report should contain database name")
	}
}

func TestGenerateReport_WithDrifts(t *testing.T) {
	reporter := NewDefaultDriftReporter(&ReportConfig{
		ColorEnabled:       false,
		ShowFixSuggestions: true,
		GroupBySeverity:    true,
		IncludeTimestamp:   false,
	})

	ctx := context.Background()
	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v2",
		Summary: DriftSummary{
			TotalDrifts:      2,
			CriticalCount:    1,
			WarningCount:     1,
			InfoCount:        0,
			AutoFixableCount: 1,
		},
		Drifts: []Drift{
			{
				Type:          DriftTableDropped,
				Severity:      SeverityCritical,
				ObjectType:    "table",
				ObjectName:    "old_table",
				Description:   "Table 'old_table' was dropped",
				CanAutoFix:    false,
				FixSuggestion: "",
			},
			{
				Type:          DriftTableAdded,
				Severity:      SeverityWarning,
				ObjectType:    "table",
				ObjectName:    "new_table",
				Description:   "Table 'new_table' was added",
				CanAutoFix:    true,
				FixSuggestion: "DROP TABLE new_table;",
			},
		},
	}

	text, err := reporter.GenerateReport(ctx, report)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if !strings.Contains(text, "Total Drifts:     2") {
		t.Error("Report should show 2 total drifts")
	}

	if !strings.Contains(text, "old_table") {
		t.Error("Report should contain old_table drift")
	}

	if !strings.Contains(text, "new_table") {
		t.Error("Report should contain new_table drift")
	}

	if !strings.Contains(text, "AUTO-FIX AVAILABLE") {
		t.Error("Report should show auto-fix section")
	}

	if !strings.Contains(text, "DROP TABLE new_table") {
		t.Error("Report should show fix suggestion")
	}
}

func TestGenerateReport_GroupedBySeverity(t *testing.T) {
	reporter := NewDefaultDriftReporter(&ReportConfig{
		ColorEnabled:       false,
		ShowFixSuggestions: false,
		GroupBySeverity:    true,
		IncludeTimestamp:   false,
	})

	ctx := context.Background()
	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v2",
		Summary: DriftSummary{
			TotalDrifts:      3,
			CriticalCount:    1,
			WarningCount:     1,
			InfoCount:        1,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{
			{Type: DriftTableDropped, Severity: SeverityCritical, ObjectType: "table", ObjectName: "t1", Description: "Critical drift"},
			{Type: DriftTableAdded, Severity: SeverityWarning, ObjectType: "table", ObjectName: "t2", Description: "Warning drift"},
			{Type: DriftIndexAdded, Severity: SeverityInfo, ObjectType: "index", ObjectName: "idx1", Description: "Info drift"},
		},
	}

	text, err := reporter.GenerateReport(ctx, report)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if !strings.Contains(text, "CRITICAL DRIFTS") {
		t.Error("Report should have CRITICAL DRIFTS section")
	}

	if !strings.Contains(text, "WARNING DRIFTS") {
		t.Error("Report should have WARNING DRIFTS section")
	}

	if !strings.Contains(text, "INFO DRIFTS") {
		t.Error("Report should have INFO DRIFTS section")
	}
}

func TestGenerateReport_FlatListing(t *testing.T) {
	reporter := NewDefaultDriftReporter(&ReportConfig{
		ColorEnabled:       false,
		ShowFixSuggestions: false,
		GroupBySeverity:    false,
		IncludeTimestamp:   false,
	})

	ctx := context.Background()
	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v2",
		Summary: DriftSummary{
			TotalDrifts:      2,
			CriticalCount:    1,
			WarningCount:     1,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{
			{Type: DriftTableDropped, Severity: SeverityCritical, ObjectType: "table", ObjectName: "t1", Description: "Drift 1"},
			{Type: DriftTableAdded, Severity: SeverityWarning, ObjectType: "table", ObjectName: "t2", Description: "Drift 2"},
		},
	}

	text, err := reporter.GenerateReport(ctx, report)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if !strings.Contains(text, "DRIFT DETAILS") {
		t.Error("Report should have DRIFT DETAILS section for flat listing")
	}

	if strings.Contains(text, "CRITICAL DRIFTS") {
		t.Error("Report should not have severity sections in flat mode")
	}
}

func TestGenerateJSON_Valid(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v2",
		Summary: DriftSummary{
			TotalDrifts:      1,
			CriticalCount:    1,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{
			{
				Type:        DriftTableDropped,
				Severity:    SeverityCritical,
				ObjectType:  "table",
				ObjectName:  "users",
				Description: "Table dropped",
			},
		},
	}

	jsonBytes, err := reporter.GenerateJSON(ctx, report)
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	jsonStr := string(jsonBytes)

	if !strings.Contains(jsonStr, "\"database\": \"testdb\"") {
		t.Error("JSON should contain database name")
	}

	if !strings.Contains(jsonStr, "\"baseline_version\": \"v1\"") {
		t.Error("JSON should contain baseline version")
	}

	if !strings.Contains(jsonStr, "\"users\"") {
		t.Error("JSON should contain drift details")
	}

	if !strings.Contains(jsonStr, "\"total_drifts\": 1") {
		t.Error("JSON should contain total drifts count")
	}
}

func TestGenerateJSON_NilReport(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	_, err := reporter.GenerateJSON(ctx, nil)
	if err == nil {
		t.Error("GenerateJSON should error on nil report")
	}
}

func TestGenerateHTML_Valid(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v2",
		Summary: DriftSummary{
			TotalDrifts:      1,
			CriticalCount:    1,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{
			{
				Type:          DriftTableDropped,
				Severity:      SeverityCritical,
				ObjectType:    "table",
				ObjectName:    "users",
				Description:   "Table dropped",
				CanAutoFix:    false,
				FixSuggestion: "",
			},
		},
	}

	html, err := reporter.GenerateHTML(ctx, report)
	if err != nil {
		t.Fatalf("GenerateHTML failed: %v", err)
	}

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("HTML should be valid HTML5")
	}

	if !strings.Contains(html, "Schema Drift Report") {
		t.Error("HTML should contain title")
	}

	if !strings.Contains(html, "testdb") {
		t.Error("HTML should contain database name")
	}

	if !strings.Contains(html, "users") {
		t.Error("HTML should contain drift details")
	}

	if !strings.Contains(html, "<style>") {
		t.Error("HTML should contain embedded CSS")
	}
}

func TestGenerateHTML_NoDrift(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v1",
		Summary: DriftSummary{
			TotalDrifts:      0,
			CriticalCount:    0,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{},
	}

	html, err := reporter.GenerateHTML(ctx, report)
	if err != nil {
		t.Fatalf("GenerateHTML failed: %v", err)
	}

	if !strings.Contains(html, "No Schema Drift Detected") {
		t.Error("HTML should show no drift message")
	}
}

func TestExportReport_TextFormat(t *testing.T) {
	reporter := NewDefaultDriftReporter(&ReportConfig{
		ColorEnabled:       false,
		ShowFixSuggestions: false,
		GroupBySeverity:    false,
		IncludeTimestamp:   false,
	})

	ctx := context.Background()
	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v1",
		Summary: DriftSummary{
			TotalDrifts:      0,
			CriticalCount:    0,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "drift_report.txt")

	err := reporter.ExportReport(ctx, report, outputPath)
	if err != nil {
		t.Fatalf("ExportReport failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read exported file: %v", err)
	}

	if !strings.Contains(string(content), "DATABASE SCHEMA DRIFT DETECTION REPORT") {
		t.Error("Exported file should contain report header")
	}
}

func TestExportReport_JSONFormat(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v1",
		Summary: DriftSummary{
			TotalDrifts:      0,
			CriticalCount:    0,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "drift_report.json")

	err := reporter.ExportReport(ctx, report, outputPath)
	if err != nil {
		t.Fatalf("ExportReport failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read exported file: %v", err)
	}

	if !strings.Contains(string(content), "\"database\"") {
		t.Error("Exported JSON should contain database field")
	}
}

func TestExportReport_HTMLFormat(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	report := &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v1",
		Summary: DriftSummary{
			TotalDrifts:      0,
			CriticalCount:    0,
			WarningCount:     0,
			InfoCount:        0,
			AutoFixableCount: 0,
		},
		Drifts: []Drift{},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "drift_report.html")

	err := reporter.ExportReport(ctx, report, outputPath)
	if err != nil {
		t.Fatalf("ExportReport failed: %v", err)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read exported file: %v", err)
	}

	if !strings.Contains(string(content), "<!DOCTYPE html>") {
		t.Error("Exported HTML should be valid HTML5")
	}
}

func TestExportReport_NilReport(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "drift_report.txt")

	err := reporter.ExportReport(ctx, nil, outputPath)
	if err == nil {
		t.Error("ExportReport should error on nil report")
	}
}

func TestExportReport_EmptyPath(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)
	ctx := context.Background()

	report := &DriftReport{
		DatabaseName: "testdb",
		Timestamp:    time.Now(),
		Summary:      DriftSummary{},
		Drifts:       []Drift{},
	}

	err := reporter.ExportReport(ctx, report, "")
	if err == nil {
		t.Error("ExportReport should error on empty path")
	}
}

func TestGetSeverityColor(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)

	tests := []struct {
		severity DriftSeverity
		expected string
	}{
		{SeverityCritical, "31"},
		{SeverityWarning, "33"},
		{SeverityInfo, "36"},
		{DriftSeverity("unknown"), "0"},
	}

	for _, tt := range tests {
		result := reporter.getSeverityColor(tt.severity)
		if result != tt.expected {
			t.Errorf("getSeverityColor(%s) = %s, expected %s", tt.severity, result, tt.expected)
		}
	}
}

func TestColorize(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)

	result := reporter.colorize("31", true)
	if result != "\033[31m" {
		t.Errorf("colorize should return ANSI code when enabled")
	}

	result = reporter.colorize("31", false)
	if result != "" {
		t.Errorf("colorize should return empty string when disabled")
	}
}

func TestResetColor(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)

	result := reporter.resetColor(true)
	if result != "\033[0m" {
		t.Errorf("resetColor should return ANSI reset when enabled")
	}

	result = reporter.resetColor(false)
	if result != "" {
		t.Errorf("resetColor should return empty string when disabled")
	}
}

/**
 * @file reporter_maps_test.go
 * @description Reporter tests that populate the Expected/Actual maps on drifts.
 *              The existing reporter tests never set these maps, leaving the
 *              writeMap helper and the HTML Expected/Actual rendering branches
 *              unexercised. These tests drive both the text and HTML reporters
 *              with column-modification drifts that carry expected and actual
 *              column metadata.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/drift/reporter_maps_test.go
 */

package drift

import (
	"context"
	"strings"
	"testing"
	"time"
)

func reportWithExpectedActual() *DriftReport {
	return &DriftReport{
		DatabaseName:    "testdb",
		Timestamp:       time.Now(),
		BaselineVersion: "v1",
		CurrentVersion:  "v2",
		Summary: DriftSummary{
			TotalDrifts:      1,
			WarningCount:     1,
			AutoFixableCount: 1,
		},
		Drifts: []Drift{
			{
				Type:       DriftColumnModified,
				Severity:   SeverityWarning,
				ObjectType: "column",
				ObjectName: "email",
				TableName:  "users",
				Expected: map[string]interface{}{
					"type":     "varchar",
					"nullable": false,
				},
				Actual: map[string]interface{}{
					"type":     "text",
					"nullable": true,
				},
				Description:   "Column 'users.email' was modified",
				CanAutoFix:    true,
				FixSuggestion: "ALTER TABLE users ALTER COLUMN email TYPE varchar NOT NULL;",
			},
		},
	}
}

// TestGenerateReport_WithExpectedActualMaps exercises writeMap and the
// fix-suggestion rendering in the text reporter.
func TestGenerateReport_WithExpectedActualMaps(t *testing.T) {
	reporter := NewDefaultDriftReporter(&ReportConfig{
		ColorEnabled:       false,
		ShowFixSuggestions: true,
		GroupBySeverity:    false, // exercise the flat listing path
		IncludeTimestamp:   true,
	})

	text, err := reporter.GenerateReport(context.Background(), reportWithExpectedActual())
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if !strings.Contains(text, "Expected:") {
		t.Error("expected 'Expected:' block in report")
	}
	if !strings.Contains(text, "Actual:") {
		t.Error("expected 'Actual:' block in report")
	}
	// writeMap renders each key/value pair.
	if !strings.Contains(text, "type: varchar") {
		t.Errorf("expected the expected-map 'type: varchar' to render, report:\n%s", text)
	}
	if !strings.Contains(text, "type: text") {
		t.Errorf("expected the actual-map 'type: text' to render, report:\n%s", text)
	}
	// The fix suggestion (indented SQL) must appear.
	if !strings.Contains(text, "ALTER TABLE users ALTER COLUMN email") {
		t.Error("expected the fix suggestion SQL in the report")
	}
}

// TestGenerateHTML_WithExpectedActualMaps exercises the HTML Expected/Actual
// rendering branches and the auto-fix code block.
func TestGenerateHTML_WithExpectedActualMaps(t *testing.T) {
	reporter := NewDefaultDriftReporter(nil)

	html, err := reporter.GenerateHTML(context.Background(), reportWithExpectedActual())
	if err != nil {
		t.Fatalf("GenerateHTML failed: %v", err)
	}

	if !strings.Contains(html, "<dt>Expected:</dt>") {
		t.Error("expected an Expected term in the HTML output")
	}
	if !strings.Contains(html, "<dt>Actual:</dt>") {
		t.Error("expected an Actual term in the HTML output")
	}
	if !strings.Contains(html, "varchar") || !strings.Contains(html, "text") {
		t.Error("expected the expected/actual values to render in the HTML output")
	}
	if !strings.Contains(html, "Auto-Fix Available") {
		t.Error("expected the auto-fix block in the HTML output")
	}
	if !strings.Contains(html, "ALTER TABLE users ALTER COLUMN email") {
		t.Error("expected the fix SQL in the HTML output")
	}
}

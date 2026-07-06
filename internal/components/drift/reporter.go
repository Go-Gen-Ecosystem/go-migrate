/**
 * @file reporter.go
 * @description Drift report generation and formatting
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/drift/reporter.go
 */

package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultDriftReporter implements DriftReporter interface
type DefaultDriftReporter struct {
	config *ReportConfig
}

// ReportConfig configures report generation
type ReportConfig struct {
	ColorEnabled     bool
	ShowFixSuggestions bool
	GroupBySeverity   bool
	IncludeTimestamp  bool
}

// NewDefaultDriftReporter creates a new default drift reporter
func NewDefaultDriftReporter(config *ReportConfig) *DefaultDriftReporter {
	if config == nil {
		config = &ReportConfig{
			ColorEnabled:       true,
			ShowFixSuggestions: true,
			GroupBySeverity:    true,
			IncludeTimestamp:   true,
		}
	}

	return &DefaultDriftReporter{
		config: config,
	}
}

// GenerateReport creates a formatted text drift report
func (r *DefaultDriftReporter) GenerateReport(ctx context.Context, report *DriftReport) (string, error) {
	if report == nil {
		return "", fmt.Errorf("report cannot be nil")
	}

	var sb strings.Builder

	// Header
	sb.WriteString("╔════════════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║           DATABASE SCHEMA DRIFT DETECTION REPORT              ║\n")
	sb.WriteString("╚════════════════════════════════════════════════════════════════╝\n\n")

	// Metadata
	sb.WriteString(fmt.Sprintf("Database:         %s\n", report.DatabaseName))
	if r.config.IncludeTimestamp {
		sb.WriteString(fmt.Sprintf("Scan Time:        %s\n", report.Timestamp.Format(time.RFC3339)))
	}
	sb.WriteString(fmt.Sprintf("Baseline Version: %s\n", report.BaselineVersion))
	sb.WriteString(fmt.Sprintf("Current Version:  %s\n\n", report.CurrentVersion))

	// Summary
	sb.WriteString("── SUMMARY ──────────────────────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("Total Drifts:     %d\n", report.Summary.TotalDrifts))
	sb.WriteString(fmt.Sprintf("  • Critical:     %s%d%s\n",
		r.colorize("31", report.Summary.CriticalCount > 0),
		report.Summary.CriticalCount,
		r.resetColor(report.Summary.CriticalCount > 0)))
	sb.WriteString(fmt.Sprintf("  • Warning:      %s%d%s\n",
		r.colorize("33", report.Summary.WarningCount > 0),
		report.Summary.WarningCount,
		r.resetColor(report.Summary.WarningCount > 0)))
	sb.WriteString(fmt.Sprintf("  • Info:         %s%d%s\n",
		r.colorize("36", report.Summary.InfoCount > 0),
		report.Summary.InfoCount,
		r.resetColor(report.Summary.InfoCount > 0)))
	sb.WriteString(fmt.Sprintf("Auto-Fixable:     %d\n\n", report.Summary.AutoFixableCount))

	if report.Summary.TotalDrifts == 0 {
		sb.WriteString("✓ No schema drift detected - database matches baseline\n")
		return sb.String(), nil
	}

	// Drift details
	if r.config.GroupBySeverity {
		r.writeDriftsByGrouped(&sb, report)
	} else {
		r.writeDriftsFlat(&sb, report)
	}

	// Fix suggestions summary
	if r.config.ShowFixSuggestions && report.Summary.AutoFixableCount > 0 {
		sb.WriteString("\n── AUTO-FIX AVAILABLE ───────────────────────────────────────────\n")
		sb.WriteString(fmt.Sprintf("%d drift(s) can be automatically fixed.\n", report.Summary.AutoFixableCount))
		sb.WriteString("Run: gomigrate drift fix --auto\n")
	}

	return sb.String(), nil
}

// writeDriftsByGrouped writes drifts grouped by severity
func (r *DefaultDriftReporter) writeDriftsByGrouped(sb *strings.Builder, report *DriftReport) {
	severities := []DriftSeverity{SeverityCritical, SeverityWarning, SeverityInfo}

	for _, severity := range severities {
		drifts := r.filterBySeverity(report.Drifts, severity)
		if len(drifts) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("\n── %s DRIFTS ──────────────────────────────────────────────────\n",
			strings.ToUpper(string(severity))))

		for i, drift := range drifts {
			r.writeDrift(sb, &drift, i+1)
		}
	}
}

// writeDriftsFlat writes all drifts in order
func (r *DefaultDriftReporter) writeDriftsFlat(sb *strings.Builder, report *DriftReport) {
	sb.WriteString("\n── DRIFT DETAILS ────────────────────────────────────────────────\n")

	for i, drift := range report.Drifts {
		r.writeDrift(sb, &drift, i+1)
	}
}

// writeDrift writes a single drift entry
func (r *DefaultDriftReporter) writeDrift(sb *strings.Builder, drift *Drift, number int) {
	// Drift header
	severityColor := r.getSeverityColor(drift.Severity)
	sb.WriteString(fmt.Sprintf("\n%d. %s[%s]%s %s\n",
		number,
		r.colorize(severityColor, r.config.ColorEnabled),
		strings.ToUpper(string(drift.Severity)),
		r.resetColor(r.config.ColorEnabled),
		drift.Description))

	// Details
	sb.WriteString(fmt.Sprintf("   Type:       %s\n", drift.Type))
	sb.WriteString(fmt.Sprintf("   Object:     %s.%s\n", drift.ObjectType, drift.ObjectName))

	// Expected vs Actual
	if len(drift.Expected) > 0 || len(drift.Actual) > 0 {
		sb.WriteString("   Expected:   ")
		r.writeMap(sb, drift.Expected)
		sb.WriteString("   Actual:     ")
		r.writeMap(sb, drift.Actual)
	}

	// Fix suggestion
	if r.config.ShowFixSuggestions && drift.CanAutoFix && drift.FixSuggestion != "" {
		sb.WriteString(fmt.Sprintf("   %sAuto-Fix:%s\n", r.colorize("32", r.config.ColorEnabled), r.resetColor(r.config.ColorEnabled)))
		sb.WriteString(fmt.Sprintf("   %s\n", r.indentSQL(drift.FixSuggestion)))
	}
}

// writeMap writes a map in a readable format
func (r *DefaultDriftReporter) writeMap(sb *strings.Builder, m map[string]interface{}) {
	if len(m) == 0 {
		sb.WriteString("(none)\n")
		return
	}

	first := true
	for k, v := range m {
		if !first {
			sb.WriteString("               ")
		}
		sb.WriteString(fmt.Sprintf("%s: %v\n", k, v))
		first = false
	}
}

// indentSQL indents SQL for display
func (r *DefaultDriftReporter) indentSQL(sql string) string {
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		lines[i] = "     " + line
	}
	return strings.Join(lines, "\n")
}

// filterBySeverity filters drifts by severity level
func (r *DefaultDriftReporter) filterBySeverity(drifts []Drift, severity DriftSeverity) []Drift {
	var filtered []Drift
	for _, drift := range drifts {
		if drift.Severity == severity {
			filtered = append(filtered, drift)
		}
	}
	return filtered
}

// getSeverityColor returns ANSI color code for severity
func (r *DefaultDriftReporter) getSeverityColor(severity DriftSeverity) string {
	switch severity {
	case SeverityCritical:
		return "31" // Red
	case SeverityWarning:
		return "33" // Yellow
	case SeverityInfo:
		return "36" // Cyan
	default:
		return "0" // Default
	}
}

// colorize returns ANSI color code if enabled
func (r *DefaultDriftReporter) colorize(code string, enabled bool) string {
	if !enabled {
		return ""
	}
	return fmt.Sprintf("\033[%sm", code)
}

// resetColor returns ANSI reset code if enabled
func (r *DefaultDriftReporter) resetColor(enabled bool) string {
	if !enabled {
		return ""
	}
	return "\033[0m"
}

// GenerateJSON creates a JSON drift report
func (r *DefaultDriftReporter) GenerateJSON(ctx context.Context, report *DriftReport) ([]byte, error) {
	if report == nil {
		return nil, fmt.Errorf("report cannot be nil")
	}

	// Create JSON-friendly structure
	jsonReport := struct {
		Database        string        `json:"database"`
		Timestamp       string        `json:"timestamp"`
		BaselineVersion string        `json:"baseline_version"`
		CurrentVersion  string        `json:"current_version"`
		Summary         DriftSummary  `json:"summary"`
		Drifts          []Drift       `json:"drifts"`
	}{
		Database:        report.DatabaseName,
		Timestamp:       report.Timestamp.Format(time.RFC3339),
		BaselineVersion: report.BaselineVersion,
		CurrentVersion:  report.CurrentVersion,
		Summary:         report.Summary,
		Drifts:          report.Drifts,
	}

	return json.MarshalIndent(jsonReport, "", "  ")
}

// GenerateHTML creates an HTML drift report
func (r *DefaultDriftReporter) GenerateHTML(ctx context.Context, report *DriftReport) (string, error) {
	if report == nil {
		return "", fmt.Errorf("report cannot be nil")
	}

	var sb strings.Builder

	// HTML header
	sb.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Schema Drift Report - ` + report.DatabaseName + `</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            border-radius: 10px;
            margin-bottom: 30px;
        }
        .header h1 {
            margin: 0 0 10px 0;
        }
        .metadata {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 15px;
            margin-bottom: 30px;
        }
        .metadata-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .metadata-card label {
            font-size: 0.9em;
            color: #666;
            display: block;
            margin-bottom: 5px;
        }
        .metadata-card value {
            font-size: 1.2em;
            font-weight: 600;
            display: block;
        }
        .summary {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
            margin-bottom: 30px;
        }
        .summary-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            text-align: center;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .summary-card .count {
            font-size: 2.5em;
            font-weight: bold;
            display: block;
        }
        .summary-card .label {
            color: #666;
            font-size: 0.9em;
        }
        .critical { color: #dc3545; }
        .warning { color: #ffc107; }
        .info { color: #17a2b8; }
        .success { color: #28a745; }
        .drift-list {
            background: white;
            border-radius: 8px;
            padding: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .drift-item {
            border-left: 4px solid #ccc;
            padding: 15px;
            margin-bottom: 15px;
            background: #f9f9f9;
            border-radius: 4px;
        }
        .drift-item.critical { border-left-color: #dc3545; }
        .drift-item.warning { border-left-color: #ffc107; }
        .drift-item.info { border-left-color: #17a2b8; }
        .drift-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 10px;
        }
        .drift-title {
            font-weight: 600;
            font-size: 1.1em;
        }
        .drift-badge {
            padding: 4px 12px;
            border-radius: 12px;
            font-size: 0.85em;
            font-weight: 600;
            text-transform: uppercase;
        }
        .badge-critical { background: #dc3545; color: white; }
        .badge-warning { background: #ffc107; color: #333; }
        .badge-info { background: #17a2b8; color: white; }
        .drift-details {
            font-size: 0.9em;
            color: #666;
            margin: 10px 0;
        }
        .drift-details dt {
            font-weight: 600;
            margin-top: 8px;
        }
        .drift-details dd {
            margin-left: 20px;
        }
        .fix-suggestion {
            background: #e7f5e7;
            border: 1px solid #28a745;
            padding: 10px;
            border-radius: 4px;
            margin-top: 10px;
        }
        .fix-suggestion code {
            display: block;
            background: #fff;
            padding: 10px;
            border-radius: 4px;
            overflow-x: auto;
            font-family: 'Courier New', monospace;
            font-size: 0.9em;
        }
        .no-drift {
            text-align: center;
            padding: 60px 20px;
            color: #28a745;
        }
        .no-drift-icon {
            font-size: 4em;
            margin-bottom: 20px;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>Schema Drift Detection Report</h1>
        <p>Database: ` + report.DatabaseName + `</p>
    </div>
`)

	// Metadata
	sb.WriteString(`    <div class="metadata">
        <div class="metadata-card">
            <label>Scan Time</label>
            <value>` + report.Timestamp.Format("2006-01-02 15:04:05") + `</value>
        </div>
        <div class="metadata-card">
            <label>Baseline Version</label>
            <value>` + report.BaselineVersion + `</value>
        </div>
        <div class="metadata-card">
            <label>Current Version</label>
            <value>` + report.CurrentVersion + `</value>
        </div>
    </div>
`)

	// Summary
	sb.WriteString(fmt.Sprintf(`    <div class="summary">
        <div class="summary-card">
            <span class="count">%d</span>
            <span class="label">Total Drifts</span>
        </div>
        <div class="summary-card">
            <span class="count critical">%d</span>
            <span class="label">Critical</span>
        </div>
        <div class="summary-card">
            <span class="count warning">%d</span>
            <span class="label">Warning</span>
        </div>
        <div class="summary-card">
            <span class="count info">%d</span>
            <span class="label">Info</span>
        </div>
        <div class="summary-card">
            <span class="count success">%d</span>
            <span class="label">Auto-Fixable</span>
        </div>
    </div>
`,
		report.Summary.TotalDrifts,
		report.Summary.CriticalCount,
		report.Summary.WarningCount,
		report.Summary.InfoCount,
		report.Summary.AutoFixableCount))

	// Drifts
	if report.Summary.TotalDrifts == 0 {
		sb.WriteString(`    <div class="drift-list">
        <div class="no-drift">
            <div class="no-drift-icon">✓</div>
            <h2>No Schema Drift Detected</h2>
            <p>Database schema matches baseline perfectly</p>
        </div>
    </div>
`)
	} else {
		sb.WriteString(`    <div class="drift-list">
        <h2>Drift Details</h2>
`)
		for i, drift := range report.Drifts {
			severityClass := string(drift.Severity)
			badgeClass := "badge-" + severityClass

			sb.WriteString(fmt.Sprintf(`        <div class="drift-item %s">
            <div class="drift-header">
                <span class="drift-title">%d. %s</span>
                <span class="drift-badge %s">%s</span>
            </div>
            <dl class="drift-details">
                <dt>Type:</dt>
                <dd>%s</dd>
                <dt>Object:</dt>
                <dd>%s.%s</dd>
`,
				severityClass,
				i+1,
				drift.Description,
				badgeClass,
				strings.ToUpper(string(drift.Severity)),
				drift.Type,
				drift.ObjectType,
				drift.ObjectName))

			if len(drift.Expected) > 0 {
				sb.WriteString(`                <dt>Expected:</dt>
                <dd>`)
				for k, v := range drift.Expected {
					sb.WriteString(fmt.Sprintf("%s: %v<br>", k, v))
				}
				sb.WriteString(`</dd>
`)
			}

			if len(drift.Actual) > 0 {
				sb.WriteString(`                <dt>Actual:</dt>
                <dd>`)
				for k, v := range drift.Actual {
					sb.WriteString(fmt.Sprintf("%s: %v<br>", k, v))
				}
				sb.WriteString(`</dd>
`)
			}

			sb.WriteString(`            </dl>
`)

			if drift.CanAutoFix && drift.FixSuggestion != "" {
				sb.WriteString(`            <div class="fix-suggestion">
                <strong>✓ Auto-Fix Available:</strong>
                <code>` + drift.FixSuggestion + `</code>
            </div>
`)
			}

			sb.WriteString(`        </div>
`)
		}

		sb.WriteString(`    </div>
`)
	}

	// HTML footer
	sb.WriteString(`</body>
</html>`)

	return sb.String(), nil
}

// ExportReport exports report to file
func (r *DefaultDriftReporter) ExportReport(ctx context.Context, report *DriftReport, path string) error {
	if report == nil {
		return fmt.Errorf("report cannot be nil")
	}

	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Determine format from extension
	var content string
	var err error

	if strings.HasSuffix(path, ".json") {
		var jsonBytes []byte
		jsonBytes, err = r.GenerateJSON(ctx, report)
		content = string(jsonBytes)
	} else if strings.HasSuffix(path, ".html") {
		content, err = r.GenerateHTML(ctx, report)
	} else {
		content, err = r.GenerateReport(ctx, report)
	}

	if err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	// Write to file
	err = os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write report to %s: %w", path, err)
	}

	return nil
}

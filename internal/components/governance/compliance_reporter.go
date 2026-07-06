/**
 * @file compliance_reporter.go
 * @description Compliance reporting system for SOX, HIPAA, and other regulatory requirements
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/compliance_reporter.go
 */

package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// ComplianceFramework represents different compliance frameworks
type ComplianceFramework string

const (
	ComplianceFrameworkSOX    ComplianceFramework = "SOX"     // Sarbanes-Oxley
	ComplianceFrameworkHIPAA  ComplianceFramework = "HIPAA"   // Health Insurance Portability and Accountability Act
	ComplianceFrameworkGDPR   ComplianceFramework = "GDPR"    // General Data Protection Regulation
	ComplianceFrameworkPCIDSS ComplianceFramework = "PCI-DSS" // Payment Card Industry Data Security Standard
	ComplianceFrameworkISO    ComplianceFramework = "ISO-27001" // ISO 27001
	ComplianceFrameworkCustom ComplianceFramework = "CUSTOM"  // Custom compliance requirements
)

// ComplianceReport represents a compliance report
type ComplianceReport struct {
	ID               string              `json:"id"`
	Framework        ComplianceFramework `json:"framework"`
	GeneratedAt      time.Time           `json:"generated_at"`
	ReportPeriod     ReportPeriod        `json:"report_period"`
	TotalMigrations  int                 `json:"total_migrations"`
	ApprovedMigrations int               `json:"approved_migrations"`
	RejectedMigrations int               `json:"rejected_migrations"`
	PolicyViolations int                 `json:"policy_violations"`
	AuditTrail       []AuditEntry        `json:"audit_trail"`
	ComplianceScore  float64             `json:"compliance_score"`
	Findings         []ComplianceFinding `json:"findings"`
	Recommendations  []string            `json:"recommendations"`
	GeneratedBy      string              `json:"generated_by"`
	Status           string              `json:"status"`
}

// ReportPeriod defines the time period for a report
type ReportPeriod struct {
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}

// AuditEntry represents a single audit trail entry
type AuditEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	MigrationID string            `json:"migration_id"`
	Action      string            `json:"action"`
	UserID      string            `json:"user_id"`
	UserName    string            `json:"user_name"`
	Details     string            `json:"details"`
	Success     bool              `json:"success"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ComplianceFinding represents a compliance issue or violation
type ComplianceFinding struct {
	Severity    string    `json:"severity"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	MigrationID string    `json:"migration_id,omitempty"`
	Remediation string    `json:"remediation"`
	Timestamp   time.Time `json:"timestamp"`
}

// ComplianceReporter generates compliance reports
type ComplianceReporter struct {
	config       ComplianceConfig
	auditStore   AuditStore
	policyEngine *PolicyEngine
	rbacManager  *RBACManager
	logger       logging.ComponentLogger
}

// ComplianceConfig configures compliance reporting
type ComplianceConfig struct {
	Framework         ComplianceFramework `json:"framework"`
	OutputDir         string              `json:"output_dir"`
	RetentionPeriod   time.Duration       `json:"retention_period"`
	AutoGenerate      bool                `json:"auto_generate"`
	GenerationInterval time.Duration      `json:"generation_interval"`
	IncludeAuditTrail bool                `json:"include_audit_trail"`
	IncludeUserDetails bool               `json:"include_user_details"`
	MinComplianceScore float64            `json:"min_compliance_score"`
}

// AuditStore defines the interface for storing audit trail
type AuditStore interface {
	// RecordAudit records an audit entry
	RecordAudit(ctx context.Context, entry AuditEntry) error

	// GetAuditTrail retrieves audit entries for a time period
	GetAuditTrail(ctx context.Context, startDate, endDate time.Time) ([]AuditEntry, error)

	// GetAuditByMigration retrieves audit entries for a specific migration
	GetAuditByMigration(ctx context.Context, migrationID string) ([]AuditEntry, error)

	// GetAuditByUser retrieves audit entries for a specific user
	GetAuditByUser(ctx context.Context, userID string, startDate, endDate time.Time) ([]AuditEntry, error)

	// CleanupOldAudits removes audit entries older than the retention period
	CleanupOldAudits(ctx context.Context, retentionPeriod time.Duration) error
}

// NewComplianceReporter creates a new compliance reporter
func NewComplianceReporter(config ComplianceConfig, auditStore AuditStore, policyEngine *PolicyEngine, rbacManager *RBACManager) *ComplianceReporter {
	return &ComplianceReporter{
		config:       config,
		auditStore:   auditStore,
		policyEngine: policyEngine,
		rbacManager:  rbacManager,
		logger:       logging.GetComponentLogger("compliance-reporter"),
	}
}

// GenerateReport generates a compliance report for the specified period
func (r *ComplianceReporter) GenerateReport(ctx context.Context, period ReportPeriod, generatedBy string) (*ComplianceReport, error) {
	r.logger.WithContext(ctx).Info("Generating compliance report",
		"framework", r.config.Framework,
		"start_date", period.StartDate,
		"end_date", period.EndDate)

	report := &ComplianceReport{
		ID:           generateReportID(),
		Framework:    r.config.Framework,
		GeneratedAt:  time.Now(),
		ReportPeriod: period,
		GeneratedBy:  generatedBy,
		Status:       "generated",
		Findings:     make([]ComplianceFinding, 0),
		Recommendations: make([]string, 0),
	}

	// Retrieve audit trail
	auditTrail, err := r.auditStore.GetAuditTrail(ctx, period.StartDate, period.EndDate)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve audit trail: %w", err)
	}

	report.AuditTrail = auditTrail
	report.TotalMigrations = r.countUniqueMigrations(auditTrail)

	// Analyze audit trail for compliance
	r.analyzeAuditTrail(report, auditTrail)

	// Calculate compliance score
	report.ComplianceScore = r.calculateComplianceScore(report)

	// Generate findings based on framework
	r.generateFrameworkSpecificFindings(ctx, report)

	// Generate recommendations
	r.generateRecommendations(report)

	// Save report to file if configured
	if r.config.OutputDir != "" {
		if err := r.saveReportToFile(report); err != nil {
			r.logger.WithContext(ctx).Error("Failed to save report to file", err)
		}
	}

	r.logger.WithContext(ctx).Info("Compliance report generated",
		"report_id", report.ID,
		"compliance_score", report.ComplianceScore,
		"findings", len(report.Findings))

	return report, nil
}

// RecordMigrationAudit records an audit entry for a migration operation
func (r *ComplianceReporter) RecordMigrationAudit(ctx context.Context, migrationID string, action string, userID string, userName string, success bool, details string) error {
	entry := AuditEntry{
		Timestamp:   time.Now(),
		MigrationID: migrationID,
		Action:      action,
		UserID:      userID,
		UserName:    userName,
		Details:     details,
		Success:     success,
		Metadata:    make(map[string]string),
	}

	if err := r.auditStore.RecordAudit(ctx, entry); err != nil {
		return fmt.Errorf("failed to record audit entry: %w", err)
	}

	r.logger.WithContext(ctx).Debug("Audit entry recorded",
		"migration_id", migrationID,
		"action", action,
		"user_id", userID)

	return nil
}

// GetAuditTrail retrieves the audit trail for a period
func (r *ComplianceReporter) GetAuditTrail(ctx context.Context, startDate, endDate time.Time) ([]AuditEntry, error) {
	return r.auditStore.GetAuditTrail(ctx, startDate, endDate)
}

// analyzeAuditTrail analyzes the audit trail for compliance metrics
func (r *ComplianceReporter) analyzeAuditTrail(report *ComplianceReport, auditTrail []AuditEntry) {
	approvedCount := 0
	rejectedCount := 0
	violationCount := 0

	for _, entry := range auditTrail {
		switch entry.Action {
		case "migration_approved":
			approvedCount++
		case "migration_rejected":
			rejectedCount++
		case "policy_violation":
			violationCount++
		}
	}

	report.ApprovedMigrations = approvedCount
	report.RejectedMigrations = rejectedCount
	report.PolicyViolations = violationCount
}

// calculateComplianceScore calculates an overall compliance score (0-100)
func (r *ComplianceReporter) calculateComplianceScore(report *ComplianceReport) float64 {
	// Base score starts at 100
	score := 100.0

	// Deduct points for violations
	violationPenalty := float64(report.PolicyViolations) * 5.0
	score -= violationPenalty

	// Deduct points for rejections
	rejectionPenalty := float64(report.RejectedMigrations) * 2.0
	score -= rejectionPenalty

	// Deduct points for findings
	for _, finding := range report.Findings {
		switch finding.Severity {
		case "critical":
			score -= 10.0
		case "high":
			score -= 5.0
		case "medium":
			score -= 2.0
		case "low":
			score -= 1.0
		}
	}

	// Ensure score doesn't go below 0
	if score < 0 {
		score = 0
	}

	return score
}

// generateFrameworkSpecificFindings generates findings based on the compliance framework
func (r *ComplianceReporter) generateFrameworkSpecificFindings(ctx context.Context, report *ComplianceReport) {
	switch r.config.Framework {
	case ComplianceFrameworkSOX:
		r.generateSOXFindings(report)
	case ComplianceFrameworkHIPAA:
		r.generateHIPAAFindings(report)
	case ComplianceFrameworkGDPR:
		r.generateGDPRFindings(report)
	case ComplianceFrameworkPCIDSS:
		r.generatePCIDSSFindings(report)
	case ComplianceFrameworkISO:
		r.generateISOFindings(report)
	}
}

// generateSOXFindings generates SOX-specific compliance findings
func (r *ComplianceReporter) generateSOXFindings(report *ComplianceReport) {
	// SOX requirements: change control, separation of duties, audit trail

	// Check for separation of duties violations
	selfApprovals := r.findSelfApprovals(report.AuditTrail)
	if len(selfApprovals) > 0 {
		report.Findings = append(report.Findings, ComplianceFinding{
			Severity:    "high",
			Category:    "Separation of Duties",
			Description: fmt.Sprintf("Found %d instances of self-approval, violating SOX separation of duties", len(selfApprovals)),
			Remediation: "Implement mandatory dual control for migration approvals",
			Timestamp:   time.Now(),
		})
	}

	// Check for missing audit trail
	if len(report.AuditTrail) == 0 && report.TotalMigrations > 0 {
		report.Findings = append(report.Findings, ComplianceFinding{
			Severity:    "critical",
			Category:    "Audit Trail",
			Description: "Incomplete audit trail detected for database changes",
			Remediation: "Ensure all database changes are properly logged",
			Timestamp:   time.Now(),
		})
	}

	// Check approval rate
	if report.TotalMigrations > 0 {
		approvalRate := float64(report.ApprovedMigrations) / float64(report.TotalMigrations)
		if approvalRate < 0.8 {
			report.Findings = append(report.Findings, ComplianceFinding{
				Severity:    "medium",
				Category:    "Change Control",
				Description: fmt.Sprintf("Low approval rate (%.1f%%) may indicate inadequate change control", approvalRate*100),
				Remediation: "Review change control processes and approval workflows",
				Timestamp:   time.Now(),
			})
		}
	}
}

// generateHIPAAFindings generates HIPAA-specific compliance findings
func (r *ComplianceReporter) generateHIPAAFindings(report *ComplianceReport) {
	// HIPAA requirements: access control, audit controls, data integrity

	// Check for unauthorized access
	unauthorizedAccess := r.findUnauthorizedAccess(report.AuditTrail)
	if len(unauthorizedAccess) > 0 {
		report.Findings = append(report.Findings, ComplianceFinding{
			Severity:    "critical",
			Category:    "Access Control",
			Description: fmt.Sprintf("Detected %d unauthorized access attempts", len(unauthorizedAccess)),
			Remediation: "Review and strengthen access control policies",
			Timestamp:   time.Now(),
		})
	}

	// Check audit completeness
	if len(report.AuditTrail) == 0 {
		report.Findings = append(report.Findings, ComplianceFinding{
			Severity:    "critical",
			Category:    "Audit Controls",
			Description: "Missing audit trail violates HIPAA audit control requirements",
			Remediation: "Implement comprehensive audit logging for all PHI access",
			Timestamp:   time.Now(),
		})
	}
}

// generateGDPRFindings generates GDPR-specific compliance findings
func (r *ComplianceReporter) generateGDPRFindings(report *ComplianceReport) {
	// GDPR requirements: data protection, privacy by design, audit trail

	report.Findings = append(report.Findings, ComplianceFinding{
		Severity:    "info",
		Category:    "Data Protection",
		Description: "Ensure all migrations involving personal data comply with GDPR",
		Remediation: "Review data processing activities and document legal basis",
		Timestamp:   time.Now(),
	})
}

// generatePCIDSSFindings generates PCI-DSS-specific compliance findings
func (r *ComplianceReporter) generatePCIDSSFindings(report *ComplianceReport) {
	// PCI-DSS requirements: secure development, change tracking, access control

	if report.PolicyViolations > 0 {
		report.Findings = append(report.Findings, ComplianceFinding{
			Severity:    "high",
			Category:    "Secure Development",
			Description: fmt.Sprintf("Found %d policy violations affecting cardholder data security", report.PolicyViolations),
			Remediation: "Address policy violations before deploying to production",
			Timestamp:   time.Now(),
		})
	}
}

// generateISOFindings generates ISO 27001-specific compliance findings
func (r *ComplianceReporter) generateISOFindings(report *ComplianceReport) {
	// ISO 27001 requirements: information security management

	if report.ComplianceScore < r.config.MinComplianceScore {
		report.Findings = append(report.Findings, ComplianceFinding{
			Severity:    "medium",
			Category:    "Information Security",
			Description: fmt.Sprintf("Compliance score (%.1f) below minimum threshold (%.1f)", report.ComplianceScore, r.config.MinComplianceScore),
			Remediation: "Review and improve security controls and processes",
			Timestamp:   time.Now(),
		})
	}
}

// generateRecommendations generates actionable recommendations
func (r *ComplianceReporter) generateRecommendations(report *ComplianceReport) {
	if report.PolicyViolations > 0 {
		report.Recommendations = append(report.Recommendations,
			"Review and update policy definitions to prevent violations")
	}

	if report.ComplianceScore < 80 {
		report.Recommendations = append(report.Recommendations,
			"Implement stricter approval workflows for high-risk migrations")
	}

	if len(report.Findings) > 5 {
		report.Recommendations = append(report.Recommendations,
			"Conduct compliance training for database administrators and developers")
	}

	selfApprovals := r.findSelfApprovals(report.AuditTrail)
	if len(selfApprovals) > 0 {
		report.Recommendations = append(report.Recommendations,
			"Enforce mandatory separation of duties for all migrations")
	}
}

// countUniqueMigrations counts unique migrations in audit trail
func (r *ComplianceReporter) countUniqueMigrations(auditTrail []AuditEntry) int {
	migrationSet := make(map[string]bool)
	for _, entry := range auditTrail {
		if entry.MigrationID != "" {
			migrationSet[entry.MigrationID] = true
		}
	}
	return len(migrationSet)
}

// findSelfApprovals finds instances where users approved their own migrations
func (r *ComplianceReporter) findSelfApprovals(auditTrail []AuditEntry) []AuditEntry {
	selfApprovals := make([]AuditEntry, 0)
	creatorMap := make(map[string]string)

	// First pass: map migrations to creators
	for _, entry := range auditTrail {
		if entry.Action == "migration_created" {
			creatorMap[entry.MigrationID] = entry.UserID
		}
	}

	// Second pass: find self-approvals
	for _, entry := range auditTrail {
		if entry.Action == "migration_approved" {
			if creatorID, exists := creatorMap[entry.MigrationID]; exists && creatorID == entry.UserID {
				selfApprovals = append(selfApprovals, entry)
			}
		}
	}

	return selfApprovals
}

// findUnauthorizedAccess finds unauthorized access attempts
func (r *ComplianceReporter) findUnauthorizedAccess(auditTrail []AuditEntry) []AuditEntry {
	unauthorized := make([]AuditEntry, 0)
	for _, entry := range auditTrail {
		if !entry.Success && (entry.Action == "migration_execute" || entry.Action == "migration_rollback") {
			unauthorized = append(unauthorized, entry)
		}
	}
	return unauthorized
}

// saveReportToFile saves the report to a JSON file
func (r *ComplianceReporter) saveReportToFile(report *ComplianceReport) error {
	if err := os.MkdirAll(r.config.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	filename := fmt.Sprintf("compliance-report-%s-%s.json",
		report.Framework,
		time.Now().Format("20060102-150405"))
	filepath := filepath.Join(r.config.OutputDir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write report file: %w", err)
	}

	r.logger.Info("Compliance report saved", "file", filepath)
	return nil
}

// generateReportID generates a unique report ID
func generateReportID() string {
	return fmt.Sprintf("report-%d", time.Now().UnixNano())
}

// CleanupOldReports removes old compliance reports
func (r *ComplianceReporter) CleanupOldReports(ctx context.Context) error {
	if r.config.OutputDir == "" {
		return nil
	}

	cutoffDate := time.Now().Add(-r.config.RetentionPeriod)
	files, err := filepath.Glob(filepath.Join(r.config.OutputDir, "compliance-report-*.json"))
	if err != nil {
		return fmt.Errorf("failed to list report files: %w", err)
	}

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoffDate) {
			if err := os.Remove(file); err != nil {
				r.logger.WithContext(ctx).Error(fmt.Sprintf("Failed to remove old report %s", file), err)
			} else {
				r.logger.WithContext(ctx).Info(fmt.Sprintf("Removed old report %s", file))
			}
		}
	}

	return nil
}

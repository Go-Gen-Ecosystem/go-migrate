/**
 * @file governance_example.go
 * @description Example usage of the governance system
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/examples/governance_example.go
 */

package examples

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/governance"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ExampleSetupGovernance demonstrates how to set up the governance system
func ExampleSetupGovernance() {
	// Create governance configuration
	config := governance.GovernanceConfig{
		Environment: "production",
		PolicyEngineConfig: governance.PolicyEngineConfig{
			Environment:     "production",
			StrictMode:      true,
			RequireApproval: true,
		},
		ApprovalConfig: governance.ApprovalWorkflowConfig{
			RequireApproval:     true,
			MinimumApprovals:    2,
			RequireAllApprovers: false,
			ApprovalTimeout:     24 * time.Hour,
			AllowSelfApproval:   false,
			EnableNotifications: true,
		},
		ComplianceConfig: governance.ComplianceConfig{
			Framework:          governance.ComplianceFrameworkSOX,
			OutputDir:          "./compliance-reports",
			RetentionPeriod:    7 * 365 * 24 * time.Hour, // 7 years for SOX
			AutoGenerate:       true,
			IncludeAuditTrail:  true,
			IncludeUserDetails: true,
			MinComplianceScore: 80.0,
		},
		Enabled: true,
	}

	// Create governance manager
	manager, err := governance.NewGovernanceManager(config)
	if err != nil {
		log.Fatalf("Failed to create governance manager: %v", err)
	}

	fmt.Println("Governance system initialized successfully")
	fmt.Printf("Environment: %s\n", config.Environment)
	fmt.Printf("Compliance Framework: %s\n", config.ComplianceConfig.Framework)
	fmt.Printf("Registered policies: %d\n", len(manager.GetPolicyEngine().GetPolicies()))
}

// ExamplePolicyValidation demonstrates policy validation
func ExamplePolicyValidation() {
	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{
		Environment: "production",
		PolicyEngineConfig: governance.PolicyEngineConfig{
			Environment: "production",
			StrictMode:  true,
		},
	})

	ctx := context.Background()

	// Example 1: Migration with DROP TABLE in production (should be blocked)
	migration1 := &types.Migration{
		ID:          "migration-001",
		Version:     "1.0.0",
		Description: "Remove old users table",
		Script:      "DROP TABLE old_users;",
		Author:      "developer-1",
	}

	result1, _ := manager.ValidateMigration(ctx, migration1)
	fmt.Printf("Migration 1 - Passed: %v, Action: %s\n", result1.Passed, result1.Action)
	for _, violation := range result1.Violations {
		fmt.Printf("  Violation: %s - %s\n", violation.PolicyName, violation.Message)
	}

	// Example 2: Migration with hardcoded password (should be blocked)
	migration2 := &types.Migration{
		ID:          "migration-002",
		Version:     "1.0.1",
		Description: "Create admin user",
		Script:      "CREATE USER admin WITH PASSWORD 'secret123';",
		Author:      "developer-1",
	}

	result2, _ := manager.ValidateMigration(ctx, migration2)
	fmt.Printf("Migration 2 - Passed: %v, Action: %s\n", result2.Passed, result2.Action)

	// Example 3: Safe migration (should pass)
	migration3 := &types.Migration{
		ID:          "migration-003",
		Version:     "1.0.2",
		Description: "Add email index",
		Script:      "CREATE INDEX idx_users_email ON users(email);",
		Author:      "developer-1",
	}

	result3, _ := manager.ValidateMigration(ctx, migration3)
	fmt.Printf("Migration 3 - Passed: %v\n", result3.Passed)
}

// ExampleApprovalWorkflow demonstrates the approval workflow
func ExampleApprovalWorkflow() {
	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{
		ApprovalConfig: governance.ApprovalWorkflowConfig{
			RequireApproval:     true,
			MinimumApprovals:    2,
			AllowSelfApproval:   false,
			ApprovalTimeout:     24 * time.Hour,
			EnableNotifications: false,
		},
	})

	ctx := context.Background()

	// Create a migration that requires approval
	migration := &types.Migration{
		ID:          "migration-004",
		Version:     "1.0.3",
		Description: "Delete inactive users",
		Script:      "DELETE FROM users WHERE last_login < NOW() - INTERVAL '2 years';",
		Author:      "developer-1",
		CanRollback: true,
		RollbackSQL: "-- Manual restore from backup required",
	}

	// Validate and get policy result
	policyResult, _ := manager.ValidateMigration(ctx, migration)

	// Request approval
	approvers := []string{"dba-1", "manager-1"}
	request, err := manager.RequestMigrationApproval(
		ctx,
		migration,
		"developer-1",
		approvers,
		policyResult,
	)
	if err != nil {
		log.Printf("Failed to request approval: %v", err)
		return
	}

	fmt.Printf("Approval request created: %s\n", request.ID)
	fmt.Printf("Required approvers: %v\n", request.RequiredApprovers)
	fmt.Printf("Expires at: %s\n", request.ExpiresAt.Format(time.RFC3339))

	// DBA approves
	err = manager.ApproveMigration(ctx, request.ID, "dba-1", "John DBA", "Reviewed and approved")
	if err != nil {
		log.Printf("Failed to approve: %v", err)
	}
	fmt.Println("DBA approved")

	// Manager approves
	err = manager.ApproveMigration(ctx, request.ID, "manager-1", "Jane Manager", "Approved for production")
	if err != nil {
		log.Printf("Failed to approve: %v", err)
	}
	fmt.Println("Manager approved")

	// Check if approved
	approved, _ := manager.CheckMigrationApproval(ctx, migration.ID)
	fmt.Printf("Migration approved: %v\n", approved)
}

// ExampleRBAC demonstrates role-based access control
func ExampleRBAC() {
	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{})
	rbac := manager.GetRBACManager()
	ctx := context.Background()

	// Add users with different roles
	developer := &governance.User{
		ID:       "dev-1",
		Username: "john.developer",
		Email:    "john@example.com",
		Roles:    []string{"developer"},
		Active:   true,
	}
	rbac.AddUser(developer)

	dba := &governance.User{
		ID:       "dba-1",
		Username: "jane.dba",
		Email:    "jane@example.com",
		Roles:    []string{"dba"},
		Active:   true,
	}
	rbac.AddUser(dba)

	// Check permissions
	canDeveloperExecute, _ := rbac.CanExecute(ctx, "dev-1")
	canDBAExecute, _ := rbac.CanExecute(ctx, "dba-1")

	fmt.Printf("Developer can execute: %v\n", canDeveloperExecute)
	fmt.Printf("DBA can execute: %v\n", canDBAExecute)

	canDeveloperApprove, _ := rbac.CanApprove(ctx, "dev-1")
	canDBAApprove, _ := rbac.CanApprove(ctx, "dba-1")

	fmt.Printf("Developer can approve: %v\n", canDeveloperApprove)
	fmt.Printf("DBA can approve: %v\n", canDBAApprove)

	// Get user permissions
	permissions, _ := rbac.GetUserPermissions("dba-1")
	fmt.Printf("DBA permissions: %v\n", permissions)
}

// ExampleComplianceReporting demonstrates compliance reporting
func ExampleComplianceReporting() {
	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{
		ComplianceConfig: governance.ComplianceConfig{
			Framework:          governance.ComplianceFrameworkSOX,
			OutputDir:          "./compliance-reports",
			RetentionPeriod:    7 * 365 * 24 * time.Hour,
			IncludeAuditTrail:  true,
			IncludeUserDetails: true,
			MinComplianceScore: 80.0,
		},
	})

	ctx := context.Background()

	// Simulate some migrations and approvals
	// (In real usage, these would be recorded automatically)
	reporter := manager.GetComplianceReporter()
	reporter.RecordMigrationAudit(ctx, "migration-001", "migration_created", "dev-1", "John Developer", true, "Created migration")
	reporter.RecordMigrationAudit(ctx, "migration-001", "approval_requested", "dev-1", "John Developer", true, "Requested approval")
	reporter.RecordMigrationAudit(ctx, "migration-001", "migration_approved", "dba-1", "Jane DBA", true, "Approved")
	reporter.RecordMigrationAudit(ctx, "migration-001", "migration_executed", "dba-1", "Jane DBA", true, "Executed successfully")

	// Generate compliance report
	startDate := time.Now().AddDate(0, -1, 0) // Last month
	endDate := time.Now()

	report, err := manager.GenerateComplianceReport(ctx, startDate, endDate, "auditor-1")
	if err != nil {
		log.Printf("Failed to generate report: %v", err)
		return
	}

	fmt.Printf("Compliance Report (%s)\n", report.Framework)
	fmt.Printf("Report Period: %s to %s\n", report.ReportPeriod.StartDate.Format("2006-01-02"), report.ReportPeriod.EndDate.Format("2006-01-02"))
	fmt.Printf("Compliance Score: %.2f/100\n", report.ComplianceScore)
	fmt.Printf("Total Migrations: %d\n", report.TotalMigrations)
	fmt.Printf("Approved: %d\n", report.ApprovedMigrations)
	fmt.Printf("Rejected: %d\n", report.RejectedMigrations)
	fmt.Printf("Policy Violations: %d\n", report.PolicyViolations)

	fmt.Println("\nFindings:")
	for i, finding := range report.Findings {
		fmt.Printf("%d. [%s] %s: %s\n", i+1, finding.Severity, finding.Category, finding.Description)
		fmt.Printf("   Remediation: %s\n", finding.Remediation)
	}

	fmt.Println("\nRecommendations:")
	for i, rec := range report.Recommendations {
		fmt.Printf("%d. %s\n", i+1, rec)
	}
}

// ExampleSeparationOfDuties demonstrates separation of duties enforcement
func ExampleSeparationOfDuties() {
	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{
		ApprovalConfig: governance.ApprovalWorkflowConfig{
			AllowSelfApproval: false,
		},
	})

	ctx := context.Background()

	migration := &types.Migration{
		ID:          "migration-005",
		Description: "Test migration",
		Script:      "ALTER TABLE users ADD COLUMN deleted_at TIMESTAMP;",
	}

	// Request approval
	request, _ := manager.RequestMigrationApproval(
		ctx,
		migration,
		"dev-1", // Created by dev-1
		[]string{"dev-1", "dba-1"},
		nil,
	)

	// Try to approve own migration (should fail)
	err := manager.ApproveMigration(ctx, request.ID, "dev-1", "Developer", "Self-approval")
	if err != nil {
		fmt.Printf("Self-approval rejected: %v\n", err)
	}

	// Approve by different user (should succeed)
	err = manager.ApproveMigration(ctx, request.ID, "dba-1", "DBA", "Approved")
	if err == nil {
		fmt.Println("Approval by different user succeeded")
	}
}

// NoProductionDataPolicy is a custom policy that prevents INSERT statements
// containing production data from running in development environments.
type NoProductionDataPolicy struct {
	governance.BasePolicy
}

// Evaluate implements governance.Policy by flagging migrations whose scripts
// contain INSERT statements, which may carry production data into development.
func (p *NoProductionDataPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*governance.PolicyViolation, error) {
	if migration == nil {
		return nil, nil
	}

	if !strings.Contains(strings.ToUpper(migration.Script), "INSERT ") {
		return nil, nil
	}

	return &governance.PolicyViolation{
		PolicyName:  p.GetName(),
		Severity:    p.GetSeverity(),
		Message:     "migration contains INSERT statements that may include production data",
		MigrationID: migration.ID,
		Suggestion:  "seed data through dedicated fixtures rather than schema migrations",
		Timestamp:   time.Now(),
	}, nil
}

// ExampleCustomPolicy demonstrates creating a custom policy
func ExampleCustomPolicy() {
	policy := &NoProductionDataPolicy{
		BasePolicy: governance.BasePolicy{
			Name:        "no_production_data",
			Description: "Prevents INSERT statements with production data in development",
			Severity:    governance.PolicySeverityWarning,
			Action:      governance.PolicyActionWarn,
			Enabled:     true,
		},
	}

	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{})
	engine := manager.GetPolicyEngine()
	engine.AddPolicy(policy)

	fmt.Printf("Custom policy added: %s\n", policy.Name)
	fmt.Printf("Total policies: %d\n", len(engine.GetPolicies()))
}

// ExampleEndToEndWorkflow demonstrates a complete end-to-end workflow
func ExampleEndToEndWorkflow() {
	fmt.Println("=== End-to-End Migration Governance Workflow ===\n")

	// 1. Setup
	fmt.Println("1. Setting up governance...")
	manager, _ := governance.NewGovernanceManager(governance.GovernanceConfig{
		Environment: "production",
		PolicyEngineConfig: governance.PolicyEngineConfig{
			Environment:     "production",
			StrictMode:      false,
			RequireApproval: true,
		},
		ApprovalConfig: governance.ApprovalWorkflowConfig{
			RequireApproval:     true,
			MinimumApprovals:    2,
			AllowSelfApproval:   false,
			ApprovalTimeout:     24 * time.Hour,
			EnableNotifications: false,
		},
	})

	ctx := context.Background()
	rbac := manager.GetRBACManager()

	// Setup users
	rbac.AddUser(&governance.User{ID: "dev-1", Username: "developer", Roles: []string{"developer"}, Active: true})
	rbac.AddUser(&governance.User{ID: "dba-1", Username: "dba", Roles: []string{"dba"}, Active: true})
	rbac.AddUser(&governance.User{ID: "mgr-1", Username: "manager", Roles: []string{"manager"}, Active: true})

	// 2. Create migration
	fmt.Println("2. Developer creates migration...")
	migration := &types.Migration{
		ID:          "migration-100",
		Version:     "2.0.0",
		Description: "Add user preferences table",
		Script:      "CREATE TABLE user_preferences (user_id INT, preferences JSON);",
		Author:      "developer",
		CanRollback: true,
		RollbackSQL: "DROP TABLE user_preferences;",
	}

	// 3. Validate policies
	fmt.Println("3. Validating against policies...")
	policyResult, _ := manager.ValidateMigration(ctx, migration)
	fmt.Printf("   Policy validation passed: %v\n", policyResult.Passed)
	fmt.Printf("   Requires approval: %v\n", policyResult.RequiresApproval)

	// 4. Request approval (if needed)
	if !policyResult.Passed || policyResult.RequiresApproval {
		fmt.Println("4. Requesting approval...")
		request, _ := manager.RequestMigrationApproval(
			ctx, migration, "dev-1", []string{"dba-1", "mgr-1"}, policyResult,
		)
		fmt.Printf("   Approval request: %s\n", request.ID)

		// 5. Approvals
		fmt.Println("5. Getting approvals...")
		manager.ApproveMigration(ctx, request.ID, "dba-1", "DBA", "LGTM")
		fmt.Println("   DBA approved")
		manager.ApproveMigration(ctx, request.ID, "mgr-1", "Manager", "Approved")
		fmt.Println("   Manager approved")
	}

	// 6. Execute migration
	fmt.Println("6. Executing migration...")
	err := manager.ExecuteMigration(ctx, migration, "dba-1", "DBA")
	if err != nil {
		fmt.Printf("   Execution blocked: %v\n", err)
	} else {
		fmt.Println("   Migration approved for execution")
	}

	// 7. Generate compliance report
	fmt.Println("7. Generating compliance report...")
	report, _ := manager.GenerateComplianceReport(
		ctx,
		time.Now().AddDate(0, 0, -30),
		time.Now(),
		"auditor",
	)
	fmt.Printf("   Compliance score: %.2f\n", report.ComplianceScore)
	fmt.Printf("   Audit trail entries: %d\n", len(report.AuditTrail))

	fmt.Println("\n=== Workflow Complete ===")
}

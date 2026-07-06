/**
 * @file integration.go
 * @description Integration layer for governance system with migration pipeline
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/integration.go
 */

package governance

import (
	"context"
	"fmt"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// GovernanceManager coordinates all governance components
type GovernanceManager struct {
	policyEngine       *PolicyEngine
	approvalWorkflow   *ApprovalWorkflow
	rbacManager        *RBACManager
	complianceReporter *ComplianceReporter
	separationOfDuties *SeparationOfDuties
	logger             logging.ComponentLogger
}

// GovernanceConfig configures the governance manager
type GovernanceConfig struct {
	Environment        string                  `json:"environment"`
	PolicyEngineConfig PolicyEngineConfig      `json:"policy_engine"`
	ApprovalConfig     ApprovalWorkflowConfig  `json:"approval_workflow"`
	ComplianceConfig   ComplianceConfig        `json:"compliance"`
	Enabled            bool                    `json:"enabled"`
}

// NewGovernanceManager creates a new governance manager
func NewGovernanceManager(config GovernanceConfig) (*GovernanceManager, error) {
	// Create policy engine
	policyEngine := NewPolicyEngine(config.PolicyEngineConfig)
	policyEngine.RegisterDefaultPolicies(config.Environment)

	// Create RBAC manager
	rbacManager := NewRBACManager()

	// Create approval store and workflow
	approvalStore := NewInMemoryApprovalStore()
	approvalNotifier := NewNoOpApprovalNotifier()
	approvalWorkflow := NewApprovalWorkflow(config.ApprovalConfig, approvalStore, approvalNotifier)

	// Create audit store and compliance reporter
	auditStore := NewInMemoryAuditStore()
	complianceReporter := NewComplianceReporter(config.ComplianceConfig, auditStore, policyEngine, rbacManager)

	// Create separation of duties enforcer
	separationOfDuties := NewSeparationOfDuties(rbacManager)

	return &GovernanceManager{
		policyEngine:       policyEngine,
		approvalWorkflow:   approvalWorkflow,
		rbacManager:        rbacManager,
		complianceReporter: complianceReporter,
		separationOfDuties: separationOfDuties,
		logger:             logging.GetComponentLogger("governance-manager"),
	}, nil
}

// ValidateMigration validates a migration against policies
func (m *GovernanceManager) ValidateMigration(ctx context.Context, migration *types.Migration) (*PolicyResult, error) {
	m.logger.WithContext(ctx).Info("Validating migration against policies", "migration_id", migration.ID)

	result, err := m.policyEngine.Evaluate(ctx, migration)
	if err != nil {
		return nil, fmt.Errorf("policy evaluation failed: %w", err)
	}

	// Record audit entry
	if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "policy_validation", "system", "system", result.Passed, fmt.Sprintf("Violations: %d", len(result.Violations))); err != nil {
		m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return result, nil
}

// RequestMigrationApproval requests approval for a migration
func (m *GovernanceManager) RequestMigrationApproval(ctx context.Context, migration *types.Migration, requestedBy string, approvers []string, policyResult *PolicyResult) (*ApprovalRequest, error) {
	m.logger.WithContext(ctx).Info("Requesting migration approval",
		"migration_id", migration.ID,
		"requested_by", requestedBy)

	// Check if user can create migrations
	if err := m.rbacManager.RequirePermission(ctx, requestedBy, PermissionMigrationCreate); err != nil {
		return nil, fmt.Errorf("permission denied: %w", err)
	}

	violations := make([]PolicyViolation, 0)
	if policyResult != nil {
		violations = policyResult.Violations
	}

	request, err := m.approvalWorkflow.RequestApproval(ctx, migration, requestedBy, approvers, violations)
	if err != nil {
		return nil, fmt.Errorf("failed to request approval: %w", err)
	}

	// Record audit entry
	if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "approval_requested", requestedBy, requestedBy, true, fmt.Sprintf("Approval request: %s", request.ID)); err != nil {
		m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return request, nil
}

// ApproveMigration approves a migration
func (m *GovernanceManager) ApproveMigration(ctx context.Context, requestID string, approverID string, approverName string, comment string) error {
	m.logger.WithContext(ctx).Info("Approving migration",
		"request_id", requestID,
		"approver_id", approverID)

	// Check if user can approve migrations
	if err := m.rbacManager.RequirePermission(ctx, approverID, PermissionMigrationApprove); err != nil {
		return fmt.Errorf("permission denied: %w", err)
	}

	// Get the approval request to check separation of duties
	request, err := m.approvalWorkflow.GetRequestStatus(ctx, requestID)
	if err != nil {
		return fmt.Errorf("failed to get approval request: %w", err)
	}

	// Check separation of duties
	if err := m.separationOfDuties.RequireDifferentApprovers(ctx, request.RequestedBy, approverID); err != nil {
		return fmt.Errorf("separation of duties violation: %w", err)
	}

	// Approve the migration
	if err := m.approvalWorkflow.Approve(ctx, requestID, approverID, approverName, comment); err != nil {
		return fmt.Errorf("failed to approve migration: %w", err)
	}

	// Record audit entry
	if err := m.complianceReporter.RecordMigrationAudit(ctx, request.MigrationID, "migration_approved", approverID, approverName, true, comment); err != nil {
		m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return nil
}

// RejectMigration rejects a migration
func (m *GovernanceManager) RejectMigration(ctx context.Context, requestID string, rejectorID string, rejectorName string, reason string) error {
	m.logger.WithContext(ctx).Info("Rejecting migration",
		"request_id", requestID,
		"rejector_id", rejectorID)

	// Check if user can reject migrations
	if err := m.rbacManager.RequirePermission(ctx, rejectorID, PermissionMigrationReject); err != nil {
		return fmt.Errorf("permission denied: %w", err)
	}

	// Get the approval request
	request, err := m.approvalWorkflow.GetRequestStatus(ctx, requestID)
	if err != nil {
		return fmt.Errorf("failed to get approval request: %w", err)
	}

	// Reject the migration
	if err := m.approvalWorkflow.Reject(ctx, requestID, rejectorID, rejectorName, reason); err != nil {
		return fmt.Errorf("failed to reject migration: %w", err)
	}

	// Record audit entry
	if err := m.complianceReporter.RecordMigrationAudit(ctx, request.MigrationID, "migration_rejected", rejectorID, rejectorName, true, reason); err != nil {
		m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return nil
}

// CheckMigrationApproval checks if a migration has been approved
func (m *GovernanceManager) CheckMigrationApproval(ctx context.Context, migrationID string) (bool, error) {
	return m.approvalWorkflow.IsApproved(ctx, migrationID)
}

// ExecuteMigration executes a migration with governance checks
func (m *GovernanceManager) ExecuteMigration(ctx context.Context, migration *types.Migration, executorID string, executorName string) error {
	m.logger.WithContext(ctx).Info("Executing migration with governance checks",
		"migration_id", migration.ID,
		"executor_id", executorID)

	// Check if user can execute migrations
	if err := m.rbacManager.RequirePermission(ctx, executorID, PermissionMigrationExecute); err != nil {
		// Record failed audit entry
		if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "migration_execute", executorID, executorName, false, err.Error()); err != nil {
			m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
		}
		return fmt.Errorf("permission denied: %w", err)
	}

	// Validate migration policies
	policyResult, err := m.ValidateMigration(ctx, migration)
	if err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// Check if migration is blocked by policy
	if policyResult.Action == PolicyActionBlock {
		// Record blocked audit entry
		if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "migration_execute", executorID, executorName, false, "Blocked by policy"); err != nil {
			m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
		}
		return fmt.Errorf("migration blocked by policy: %d violations", len(policyResult.Violations))
	}

	// Check if approval is required
	if policyResult.RequiresApproval {
		approved, err := m.CheckMigrationApproval(ctx, migration.ID)
		if err != nil {
			return fmt.Errorf("failed to check approval status: %w", err)
		}

		if !approved {
			// Record unapproved audit entry
			if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "migration_execute", executorID, executorName, false, "Requires approval"); err != nil {
				m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
			}
			return fmt.Errorf("migration requires approval before execution")
		}
	}

	// Record successful execution start
	if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "migration_execute", executorID, executorName, true, "Execution started"); err != nil {
		m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return nil
}

// RollbackMigration rolls back a migration with governance checks
func (m *GovernanceManager) RollbackMigration(ctx context.Context, migration *types.Migration, executorID string, executorName string) error {
	m.logger.WithContext(ctx).Info("Rolling back migration with governance checks",
		"migration_id", migration.ID,
		"executor_id", executorID)

	// Check if user can rollback migrations
	if err := m.rbacManager.RequirePermission(ctx, executorID, PermissionMigrationRollback); err != nil {
		// Record failed audit entry
		if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "migration_rollback", executorID, executorName, false, err.Error()); err != nil {
			m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
		}
		return fmt.Errorf("permission denied: %w", err)
	}

	// Record successful rollback start
	if err := m.complianceReporter.RecordMigrationAudit(ctx, migration.ID, "migration_rollback", executorID, executorName, true, "Rollback started"); err != nil {
		m.logger.WithContext(ctx).Error("Failed to record audit entry", err)
	}

	return nil
}

// GenerateComplianceReport generates a compliance report
func (m *GovernanceManager) GenerateComplianceReport(ctx context.Context, startDate, endDate time.Time, generatedBy string) (*ComplianceReport, error) {
	m.logger.WithContext(ctx).Info("Generating compliance report",
		"start_date", startDate,
		"end_date", endDate)

	period := ReportPeriod{
		StartDate: startDate,
		EndDate:   endDate,
	}

	return m.complianceReporter.GenerateReport(ctx, period, generatedBy)
}

// GetPolicyEngine returns the policy engine
func (m *GovernanceManager) GetPolicyEngine() *PolicyEngine {
	return m.policyEngine
}

// GetApprovalWorkflow returns the approval workflow
func (m *GovernanceManager) GetApprovalWorkflow() *ApprovalWorkflow {
	return m.approvalWorkflow
}

// GetRBACManager returns the RBAC manager
func (m *GovernanceManager) GetRBACManager() *RBACManager {
	return m.rbacManager
}

// GetComplianceReporter returns the compliance reporter
func (m *GovernanceManager) GetComplianceReporter() *ComplianceReporter {
	return m.complianceReporter
}

// CleanupExpiredApprovals cleans up expired approval requests
func (m *GovernanceManager) CleanupExpiredApprovals(ctx context.Context) error {
	return m.approvalWorkflow.CleanupExpiredRequests(ctx)
}

// CleanupOldAudits cleans up old audit entries
func (m *GovernanceManager) CleanupOldAudits(ctx context.Context, retentionPeriod time.Duration) error {
	return m.complianceReporter.auditStore.CleanupOldAudits(ctx, retentionPeriod)
}

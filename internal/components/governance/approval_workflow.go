/**
 * @file approval_workflow.go
 * @description Approval workflow system for migration governance
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/approval_workflow.go
 */

package governance

import (
	"context"
	"fmt"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// ApprovalStatus represents the status of an approval request
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
	ApprovalStatusExpired  ApprovalStatus = "expired"
	ApprovalStatusCanceled ApprovalStatus = "canceled"
)

// ApprovalRequest represents a request for approval
type ApprovalRequest struct {
	ID              string           `json:"id"`
	MigrationID     string           `json:"migration_id"`
	RequestedBy     string           `json:"requested_by"`
	RequestedAt     time.Time        `json:"requested_at"`
	Status          ApprovalStatus   `json:"status"`
	RequiredApprovers []string       `json:"required_approvers"`
	Approvals       []Approval       `json:"approvals"`
	Rejections      []Rejection      `json:"rejections"`
	ExpiresAt       time.Time        `json:"expires_at"`
	Reason          string           `json:"reason"`
	PolicyViolations []PolicyViolation `json:"policy_violations,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Approval represents an approval decision
type Approval struct {
	ApproverID   string    `json:"approver_id"`
	ApproverName string    `json:"approver_name"`
	ApprovedAt   time.Time `json:"approved_at"`
	Comment      string    `json:"comment,omitempty"`
}

// Rejection represents a rejection decision
type Rejection struct {
	RejectorID   string    `json:"rejector_id"`
	RejectorName string    `json:"rejector_name"`
	RejectedAt   time.Time `json:"rejected_at"`
	Reason       string    `json:"reason"`
}

// ApprovalWorkflow manages approval workflows
type ApprovalWorkflow struct {
	config   ApprovalWorkflowConfig
	store    ApprovalStore
	notifier ApprovalNotifier
	logger   logging.ComponentLogger
}

// ApprovalWorkflowConfig configures the approval workflow
type ApprovalWorkflowConfig struct {
	RequireApproval      bool          `json:"require_approval"`
	MinimumApprovals     int           `json:"minimum_approvals"`
	RequireAllApprovers  bool          `json:"require_all_approvers"`
	ApprovalTimeout      time.Duration `json:"approval_timeout"`
	AutoApproveNonRisky  bool          `json:"auto_approve_non_risky"`
	EnableNotifications  bool          `json:"enable_notifications"`
	AllowSelfApproval    bool          `json:"allow_self_approval"`
}

// ApprovalStore defines the interface for storing approval requests
type ApprovalStore interface {
	// CreateRequest creates a new approval request
	CreateRequest(ctx context.Context, request *ApprovalRequest) error

	// GetRequest retrieves an approval request by ID
	GetRequest(ctx context.Context, requestID string) (*ApprovalRequest, error)

	// GetRequestByMigration retrieves approval requests for a migration
	GetRequestByMigration(ctx context.Context, migrationID string) ([]*ApprovalRequest, error)

	// UpdateRequest updates an approval request
	UpdateRequest(ctx context.Context, request *ApprovalRequest) error

	// DeleteRequest deletes an approval request
	DeleteRequest(ctx context.Context, requestID string) error

	// ListPendingRequests lists all pending approval requests
	ListPendingRequests(ctx context.Context) ([]*ApprovalRequest, error)

	// ListExpiredRequests lists all expired approval requests
	ListExpiredRequests(ctx context.Context) ([]*ApprovalRequest, error)
}

// ApprovalNotifier defines the interface for sending approval notifications
type ApprovalNotifier interface {
	// NotifyApprovalRequired notifies approvers that approval is required
	NotifyApprovalRequired(ctx context.Context, request *ApprovalRequest) error

	// NotifyApprovalGranted notifies requestor that approval was granted
	NotifyApprovalGranted(ctx context.Context, request *ApprovalRequest) error

	// NotifyApprovalRejected notifies requestor that approval was rejected
	NotifyApprovalRejected(ctx context.Context, request *ApprovalRequest) error

	// NotifyApprovalExpired notifies requestor that approval expired
	NotifyApprovalExpired(ctx context.Context, request *ApprovalRequest) error
}

// NewApprovalWorkflow creates a new approval workflow
func NewApprovalWorkflow(config ApprovalWorkflowConfig, store ApprovalStore, notifier ApprovalNotifier) *ApprovalWorkflow {
	return &ApprovalWorkflow{
		config:   config,
		store:    store,
		notifier: notifier,
		logger:   logging.GetComponentLogger("approval-workflow"),
	}
}

// RequestApproval creates an approval request for a migration
func (w *ApprovalWorkflow) RequestApproval(ctx context.Context, migration *types.Migration, requestedBy string, approvers []string, violations []PolicyViolation) (*ApprovalRequest, error) {
	w.logger.WithContext(ctx).Info("Creating approval request",
		"migration_id", migration.ID,
		"requested_by", requestedBy,
		"approvers", approvers)

	request := &ApprovalRequest{
		ID:                generateRequestID(),
		MigrationID:       migration.ID,
		RequestedBy:       requestedBy,
		RequestedAt:       time.Now(),
		Status:            ApprovalStatusPending,
		RequiredApprovers: approvers,
		Approvals:         make([]Approval, 0),
		Rejections:        make([]Rejection, 0),
		ExpiresAt:         time.Now().Add(w.config.ApprovalTimeout),
		Reason:            fmt.Sprintf("Approval required for migration: %s", migration.Description),
		PolicyViolations:  violations,
		Metadata:          make(map[string]string),
	}

	// Store the request
	if err := w.store.CreateRequest(ctx, request); err != nil {
		return nil, fmt.Errorf("failed to create approval request: %w", err)
	}

	// Send notifications if enabled
	if w.config.EnableNotifications && w.notifier != nil {
		if err := w.notifier.NotifyApprovalRequired(ctx, request); err != nil {
			w.logger.WithContext(ctx).Error("Failed to send approval notification", err)
		}
	}

	return request, nil
}

// Approve records an approval for a request
func (w *ApprovalWorkflow) Approve(ctx context.Context, requestID string, approverID string, approverName string, comment string) error {
	w.logger.WithContext(ctx).Info("Processing approval",
		"request_id", requestID,
		"approver_id", approverID)

	request, err := w.store.GetRequest(ctx, requestID)
	if err != nil {
		return fmt.Errorf("failed to get approval request: %w", err)
	}

	// Check if already decided
	if request.Status != ApprovalStatusPending {
		return fmt.Errorf("approval request is not pending (status: %s)", request.Status)
	}

	// Check if expired
	if time.Now().After(request.ExpiresAt) {
		request.Status = ApprovalStatusExpired
		if err := w.store.UpdateRequest(ctx, request); err != nil {
			return fmt.Errorf("failed to update expired request: %w", err)
		}
		return fmt.Errorf("approval request has expired")
	}

	// Check if self-approval is allowed
	if !w.config.AllowSelfApproval && approverID == request.RequestedBy {
		return fmt.Errorf("self-approval is not allowed")
	}

	// Check if approver is in the required list
	isValidApprover := false
	for _, required := range request.RequiredApprovers {
		if required == approverID {
			isValidApprover = true
			break
		}
	}
	if !isValidApprover {
		return fmt.Errorf("approver %s is not in the required approvers list", approverID)
	}

	// Check if already approved by this approver
	for _, approval := range request.Approvals {
		if approval.ApproverID == approverID {
			return fmt.Errorf("already approved by %s", approverID)
		}
	}

	// Add approval
	request.Approvals = append(request.Approvals, Approval{
		ApproverID:   approverID,
		ApproverName: approverName,
		ApprovedAt:   time.Now(),
		Comment:      comment,
	})

	// Check if approval threshold is met
	if w.isApprovalThresholdMet(request) {
		request.Status = ApprovalStatusApproved
		w.logger.WithContext(ctx).Info("Approval threshold met", "request_id", requestID)

		// Send approval granted notification
		if w.config.EnableNotifications && w.notifier != nil {
			if err := w.notifier.NotifyApprovalGranted(ctx, request); err != nil {
				w.logger.WithContext(ctx).Error("Failed to send approval granted notification", err)
			}
		}
	}

	// Update request
	if err := w.store.UpdateRequest(ctx, request); err != nil {
		return fmt.Errorf("failed to update approval request: %w", err)
	}

	return nil
}

// Reject records a rejection for a request
func (w *ApprovalWorkflow) Reject(ctx context.Context, requestID string, rejectorID string, rejectorName string, reason string) error {
	w.logger.WithContext(ctx).Info("Processing rejection",
		"request_id", requestID,
		"rejector_id", rejectorID)

	request, err := w.store.GetRequest(ctx, requestID)
	if err != nil {
		return fmt.Errorf("failed to get approval request: %w", err)
	}

	// Check if already decided
	if request.Status != ApprovalStatusPending {
		return fmt.Errorf("approval request is not pending (status: %s)", request.Status)
	}

	// Check if expired
	if time.Now().After(request.ExpiresAt) {
		request.Status = ApprovalStatusExpired
		if err := w.store.UpdateRequest(ctx, request); err != nil {
			return fmt.Errorf("failed to update expired request: %w", err)
		}
		return fmt.Errorf("approval request has expired")
	}

	// Add rejection
	request.Rejections = append(request.Rejections, Rejection{
		RejectorID:   rejectorID,
		RejectorName: rejectorName,
		RejectedAt:   time.Now(),
		Reason:       reason,
	})

	// Any rejection marks the request as rejected
	request.Status = ApprovalStatusRejected

	// Send rejection notification
	if w.config.EnableNotifications && w.notifier != nil {
		if err := w.notifier.NotifyApprovalRejected(ctx, request); err != nil {
			w.logger.WithContext(ctx).Error("Failed to send rejection notification", err)
		}
	}

	// Update request
	if err := w.store.UpdateRequest(ctx, request); err != nil {
		return fmt.Errorf("failed to update approval request: %w", err)
	}

	return nil
}

// Cancel cancels an approval request
func (w *ApprovalWorkflow) Cancel(ctx context.Context, requestID string) error {
	request, err := w.store.GetRequest(ctx, requestID)
	if err != nil {
		return fmt.Errorf("failed to get approval request: %w", err)
	}

	if request.Status != ApprovalStatusPending {
		return fmt.Errorf("cannot cancel request with status: %s", request.Status)
	}

	request.Status = ApprovalStatusCanceled

	if err := w.store.UpdateRequest(ctx, request); err != nil {
		return fmt.Errorf("failed to update approval request: %w", err)
	}

	return nil
}

// GetRequestStatus gets the status of an approval request
func (w *ApprovalWorkflow) GetRequestStatus(ctx context.Context, requestID string) (*ApprovalRequest, error) {
	return w.store.GetRequest(ctx, requestID)
}

// IsApproved checks if a migration has been approved
func (w *ApprovalWorkflow) IsApproved(ctx context.Context, migrationID string) (bool, error) {
	requests, err := w.store.GetRequestByMigration(ctx, migrationID)
	if err != nil {
		return false, fmt.Errorf("failed to get approval requests: %w", err)
	}

	// Check if there's an approved request
	for _, request := range requests {
		if request.Status == ApprovalStatusApproved {
			return true, nil
		}
	}

	return false, nil
}

// CleanupExpiredRequests marks expired pending requests as expired
func (w *ApprovalWorkflow) CleanupExpiredRequests(ctx context.Context) error {
	expiredRequests, err := w.store.ListExpiredRequests(ctx)
	if err != nil {
		return fmt.Errorf("failed to list expired requests: %w", err)
	}

	for _, request := range expiredRequests {
		if request.Status == ApprovalStatusPending {
			request.Status = ApprovalStatusExpired

			if err := w.store.UpdateRequest(ctx, request); err != nil {
				w.logger.WithContext(ctx).Error(fmt.Sprintf("Failed to update expired request %s", request.ID), err)
				continue
			}

			// Send expiration notification
			if w.config.EnableNotifications && w.notifier != nil {
				if err := w.notifier.NotifyApprovalExpired(ctx, request); err != nil {
					w.logger.WithContext(ctx).Error("Failed to send expiration notification", err)
				}
			}
		}
	}

	return nil
}

// isApprovalThresholdMet checks if the approval threshold has been met
func (w *ApprovalWorkflow) isApprovalThresholdMet(request *ApprovalRequest) bool {
	if w.config.RequireAllApprovers {
		// All required approvers must approve
		approvedSet := make(map[string]bool)
		for _, approval := range request.Approvals {
			approvedSet[approval.ApproverID] = true
		}

		for _, required := range request.RequiredApprovers {
			if !approvedSet[required] {
				return false
			}
		}
		return true
	}

	// Check minimum approval count
	return len(request.Approvals) >= w.config.MinimumApprovals
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return fmt.Sprintf("approval-%d", time.Now().UnixNano())
}

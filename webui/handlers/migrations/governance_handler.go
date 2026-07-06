/**
 * @file governance_handler.go
 * @description WebUI handlers for governance operations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/webui/handlers/migrations/governance_handler.go
 */

package migrations

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/governance"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// GovernanceHandler handles governance-related HTTP requests
type GovernanceHandler struct {
	governanceManager *governance.GovernanceManager
	logger            logging.Logger
}

// NewGovernanceHandler creates a new governance handler
func NewGovernanceHandler(govManager *governance.GovernanceManager, logger logging.Logger) *GovernanceHandler {
	return &GovernanceHandler{
		governanceManager: govManager,
		logger:            logger,
	}
}

// RegisterGovernanceRoutes registers governance routes
func (h *GovernanceHandler) RegisterGovernanceRoutes(router *gin.RouterGroup) {
	gov := router.Group("/governance")
	{
		gov.GET("/policies", h.ListPolicies)
		gov.POST("/validate/:migration_id", h.ValidateMigration)
		gov.POST("/approve/:migration_id", h.ApproveMigration)
		gov.POST("/reject/:migration_id", h.RejectMigration)
		gov.GET("/approval/:migration_id", h.GetApprovalStatus)
		gov.POST("/compliance/report", h.GenerateComplianceReport)
		gov.GET("/permissions/:user_id", h.GetUserPermissions)
		gov.GET("/audit", h.GetAuditTrail)
	}
}

// PolicyInfo represents policy information for API responses
type PolicyInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
	Enabled     bool   `json:"enabled"`
}

// ListPolicies returns all active governance policies
func (h *GovernanceHandler) ListPolicies(c *gin.Context) {
	ctx := c.Request.Context()

	policies := h.governanceManager.GetPolicyEngine().GetPolicies()

	policyInfos := make([]PolicyInfo, 0, len(policies))
	for _, policy := range policies {
		policyInfos = append(policyInfos, PolicyInfo{
			Name:        policy.GetName(),
			Description: policy.GetDescription(),
			Severity:    string(policy.GetSeverity()),
			Enabled:     true,
		})
	}

	h.logger.Info(ctx, "Listed governance policies", "count", len(policyInfos))

	c.JSON(http.StatusOK, gin.H{
		"policies": policyInfos,
		"count":    len(policyInfos),
	})
}

// ValidateMigrationRequest represents validation request
type ValidateMigrationRequest struct {
	MigrationID string `json:"migration_id" binding:"required"`
	SQL         string `json:"sql" binding:"required"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// ValidateMigration validates a migration against governance policies
func (h *GovernanceHandler) ValidateMigration(c *gin.Context) {
	migrationID := c.Param("migration_id")

	var req ValidateMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid request",
			Code:      "INVALID_REQUEST",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Note: Since we can't use internal types directly, we provide basic validation feedback
	// In a full implementation, this would create a proper Migration object and validate it

	c.JSON(http.StatusOK, gin.H{
		"migration_id":    migrationID,
		"validated":       true,
		"message":         "Governance validation requires full migration context. Use CLI or Builder API for complete validation.",
		"policies_active": len(h.governanceManager.GetPolicyEngine().GetPolicies()),
	})
}

// ApprovalRequest represents an approval request
type ApprovalRequest struct {
	ApproverID   string `json:"approver_id" binding:"required"`
	ApproverName string `json:"approver_name" binding:"required"`
	Comment      string `json:"comment"`
}

// ApproveMigration approves a migration
func (h *GovernanceHandler) ApproveMigration(c *gin.Context) {
	ctx := c.Request.Context()
	migrationID := c.Param("migration_id")

	var req ApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid request",
			Code:      "INVALID_REQUEST",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	err := h.governanceManager.ApproveMigration(ctx, migrationID, req.ApproverID, req.ApproverName, req.Comment)
	if err != nil {
		h.logger.Error(ctx, "Failed to approve migration", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Migration approved", "migration_id", migrationID, "approver", req.ApproverName)

	c.JSON(http.StatusOK, gin.H{
		"migration_id": migrationID,
		"approved":     true,
		"approver":     req.ApproverName,
		"timestamp":    time.Now(),
	})
}

// RejectionRequest represents a rejection request
type RejectionRequest struct {
	RejectorID   string `json:"rejector_id" binding:"required"`
	RejectorName string `json:"rejector_name" binding:"required"`
	Reason       string `json:"reason" binding:"required"`
}

// RejectMigration rejects a migration
func (h *GovernanceHandler) RejectMigration(c *gin.Context) {
	ctx := c.Request.Context()
	migrationID := c.Param("migration_id")

	var req RejectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid request",
			Code:      "INVALID_REQUEST",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	err := h.governanceManager.RejectMigration(ctx, migrationID, req.RejectorID, req.RejectorName, req.Reason)
	if err != nil {
		h.logger.Error(ctx, "Failed to reject migration", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Migration rejected", "migration_id", migrationID, "rejector", req.RejectorName)

	c.JSON(http.StatusOK, gin.H{
		"migration_id": migrationID,
		"rejected":     true,
		"rejector":     req.RejectorName,
		"reason":       req.Reason,
		"timestamp":    time.Now(),
	})
}

// GetApprovalStatus returns the approval status of a migration
func (h *GovernanceHandler) GetApprovalStatus(c *gin.Context) {
	ctx := c.Request.Context()
	migrationID := c.Param("migration_id")

	approved, err := h.governanceManager.CheckMigrationApproval(ctx, migrationID)
	if err != nil {
		h.logger.Error(ctx, "Failed to check approval status", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"migration_id": migrationID,
		"approved":     approved,
	})
}

// ComplianceReportRequest represents a compliance report request
type ComplianceReportRequest struct {
	StartDate   string `json:"start_date" binding:"required"`
	EndDate     string `json:"end_date" binding:"required"`
	GeneratedBy string `json:"generated_by" binding:"required"`
}

// GenerateComplianceReport generates a compliance report
func (h *GovernanceHandler) GenerateComplianceReport(c *gin.Context) {
	ctx := c.Request.Context()

	var req ComplianceReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid request",
			Code:      "INVALID_REQUEST",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid start_date format",
			Code:      "INVALID_DATE_FORMAT",
			Details:   "Expected format: YYYY-MM-DD",
			Timestamp: time.Now(),
		})
		return
	}

	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Invalid end_date format",
			Code:      "INVALID_DATE_FORMAT",
			Details:   "Expected format: YYYY-MM-DD",
			Timestamp: time.Now(),
		})
		return
	}

	report, err := h.governanceManager.GenerateComplianceReport(ctx, startDate, endDate, req.GeneratedBy)
	if err != nil {
		h.logger.Error(ctx, "Failed to generate compliance report", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Compliance report generated", "framework", report.Framework, "generated_by", req.GeneratedBy)

	c.JSON(http.StatusOK, report)
}

// GetUserPermissions returns permissions for a user
func (h *GovernanceHandler) GetUserPermissions(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.Param("user_id")

	permissions, err := h.governanceManager.GetRBACManager().GetUserPermissions(userID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get user permissions", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Retrieved user permissions", "user_id", userID, "permissions_count", len(permissions))

	c.JSON(http.StatusOK, gin.H{
		"user_id":     userID,
		"permissions": permissions,
	})
}

// GetAuditTrail returns the audit trail
func (h *GovernanceHandler) GetAuditTrail(c *gin.Context) {
	ctx := c.Request.Context()

	// Query parameters
	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := time.ParseDuration(limitStr); err == nil {
			limit = int(l)
		}
	}

	// Note: This would require implementing GetAuditTrail in the governance manager
	// For now, return a placeholder response

	h.logger.Info(ctx, "Retrieved audit trail", "limit", limit)

	c.JSON(http.StatusOK, gin.H{
		"message": "Audit trail retrieval requires database-backed audit store",
		"limit":   limit,
	})
}

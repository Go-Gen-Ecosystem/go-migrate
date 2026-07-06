package migrations

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/constants"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// WebSocketHub interface for WebSocket broadcasting (to avoid circular dependencies)
type WebSocketHub interface {
	BroadcastRollbackProgress(data interface{})
	BroadcastRollbackAnalysis(data interface{})
	BroadcastRollbackTest(data interface{})
}

// Handler handles migration-related HTTP requests
type Handler struct {
	migrationTool types.MigrationTool
	logger        logging.Logger
	wsHub         WebSocketHub
}

// NewHandler creates a new migration handler
func NewHandler(migrationTool types.MigrationTool, logger logging.Logger) *Handler {
	return &Handler{
		migrationTool: migrationTool,
		logger:        logger,
		wsHub:         nil,
	}
}

// SetWebSocketHub sets the WebSocket hub for real-time updates
func (h *Handler) SetWebSocketHub(hub WebSocketHub) {
	h.wsHub = hub
}

// RegisterRoutes registers migration routes
func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	migrations := router.Group("/migrations")
	{
		migrations.GET("/status", h.Status)
		migrations.POST("/execute", h.Execute)
		migrations.POST("/rollback", h.Rollback)
		migrations.POST("/validate", h.Validate)
		migrations.GET("/history", h.History)
		migrations.POST("/dry-run", h.DryRun)
		migrations.POST("/diff", h.Diff)

		// Advanced rollback routes
		rollback := migrations.Group("/rollback")
		{
			rollback.POST("/selective", h.RollbackSelective)
			rollback.POST("/analyze", h.AnalyzeRollback)
			rollback.POST("/test", h.TestRollback)
			rollback.GET("/graph/:migration_id", h.GetDependencyGraph)
		}
	}
}

// Status handles migration status requests
func (h *Handler) Status(c *gin.Context) {
	ctx := c.Request.Context()

	status, err := h.migrationTool.Status(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to get migration status", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := h.convertToStatusResponse(status)
	c.JSON(http.StatusOK, response)
}

// Execute handles migration execution requests
func (h *Handler) Execute(c *gin.Context) {
	ctx := c.Request.Context()

	var req ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Executing migrations",
		"target", req.Target,
		"dryRun", req.DryRun,
		"count", len(req.Migrations))

	start := time.Now()
	err := h.migrationTool.Migrate(ctx, req.Target, req.DryRun)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		h.logger.Error(ctx, "Migration execution failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgMigrationFailed,
			Code:      constants.ErrCodeMigrationFailed,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Get updated status
	status, err := h.migrationTool.Status(ctx)
	if err != nil {
		h.logger.Warn(ctx, "Failed to get status after migration", err)
	}

	response := ExecuteResponse{
		Success:       true,
		Applied:       h.getAppliedCount(status),
		Failed:        0,
		Results:       []MigrationResult{},
		TotalDuration: duration,
		Timestamp:     time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// Rollback handles migration rollback requests
func (h *Handler) Rollback(c *gin.Context) {
	ctx := c.Request.Context()

	var req RollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Rolling back migrations",
		"steps", req.Steps,
		"target", req.Target,
		"dryRun", req.DryRun)

	start := time.Now()
	err := h.migrationTool.Rollback(ctx, req.Steps)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		h.logger.Error(ctx, "Rollback failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgRollbackFailed,
			Code:      constants.ErrCodeRollbackFailed,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := RollbackResponse{
		Success:       true,
		RolledBack:    req.Steps,
		Results:       []MigrationResult{},
		TotalDuration: duration,
		Timestamp:     time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// Validate handles migration validation requests
func (h *Handler) Validate(c *gin.Context) {
	ctx := c.Request.Context()

	var req ValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Validating migrations",
		"count", len(req.Migrations),
		"strict", req.Strict)

	err := h.migrationTool.Validate(ctx)

	response := ValidateResponse{
		Valid:     err == nil,
		Errors:    []ValidationError{},
		Warnings:  []ValidationError{},
		Timestamp: time.Now(),
	}

	if err != nil {
		response.Errors = append(response.Errors, ValidationError{
			Severity: constants.SeverityError,
			Message:  err.Error(),
		})
	}

	statusCode := http.StatusOK
	if !response.Valid {
		statusCode = http.StatusBadRequest
	}

	c.JSON(statusCode, response)
}

// History handles migration history requests
func (h *Handler) History(c *gin.Context) {
	ctx := c.Request.Context()

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	status := c.Query("status")
	author := c.Query("author")

	h.logger.Info(ctx, "Fetching migration history",
		"limit", limit,
		"offset", offset,
		"status", status,
		"author", author)

	// Get migration info
	info, err := h.migrationTool.Info(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to get migration info", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgInternalError,
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := h.convertToHistoryResponse(info, limit, offset)
	c.JSON(http.StatusOK, response)
}

// DryRun handles dry run requests
func (h *Handler) DryRun(c *gin.Context) {
	ctx := c.Request.Context()

	var req DryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Executing dry run",
		"target", req.Target,
		"count", len(req.Migrations))

	// Execute dry run
	err := h.migrationTool.Migrate(ctx, req.Target, true)

	response := DryRunResponse{
		WillExecute:   []MigrationPlan{},
		Dependencies:  []Dependency{},
		Warnings:      []string{},
		EstimatedTime: 0,
		Timestamp:     time.Now(),
	}

	if err != nil {
		response.Warnings = append(response.Warnings, err.Error())
	}

	c.JSON(http.StatusOK, response)
}

// Diff handles schema diff requests
func (h *Handler) Diff(c *gin.Context) {
	ctx := c.Request.Context()

	var req DiffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Computing schema diff",
		"source", req.Source,
		"target", req.Target)

	// TODO: Implement schema diff logic
	response := DiffResponse{
		Changes:       []SchemaChange{},
		Additions:     0,
		Deletions:     0,
		Modifications: 0,
		Timestamp:     time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// Helper functions

func (h *Handler) convertToStatusResponse(status *types.MigrationStatus) StatusResponse {
	if status == nil {
		return StatusResponse{
			Migrations: []MigrationStatus{},
			Timestamp:  time.Now(),
		}
	}

	migrations := make([]MigrationStatus, 0)
	// TODO: Convert migrations from status

	return StatusResponse{
		CurrentVersion: status.SchemaVersion,
		Applied:        status.ExecutedCount,
		Pending:        status.PendingCount,
		Failed:         status.FailedCount,
		Migrations:     migrations,
		Timestamp:      time.Now(),
	}
}

func (h *Handler) convertToHistoryResponse(info *types.MigrationInfo, limit, offset int) HistoryResponse {
	migrations := make([]MigrationHistory, 0)
	// TODO: Convert migration info to history

	return HistoryResponse{
		Migrations: migrations,
		Total:      0,
		Limit:      limit,
		Offset:     offset,
		Timestamp:  time.Now(),
	}
}

func (h *Handler) getAppliedCount(status *types.MigrationStatus) int {
	if status == nil {
		return 0
	}
	return status.ExecutedCount
}

package state

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/state"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// Handler handles state-based migration HTTP requests
type Handler struct {
	migrator state.StateBasedMigrator
	logger   logging.Logger
}

// NewHandler creates a new state-based migration handler
func NewHandler(migrator state.StateBasedMigrator, logger logging.Logger) *Handler {
	return &Handler{
		migrator: migrator,
		logger:   logger,
	}
}

// RegisterRoutes registers state-based migration routes
func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	stateRoutes := router.Group("/state")
	{
		stateRoutes.POST("/validate", h.ValidateSchema)
		stateRoutes.GET("/preview", h.PreviewChanges)
		stateRoutes.POST("/plan", h.PlanMigration)
		stateRoutes.POST("/apply", h.ApplyMigration)
		stateRoutes.POST("/sync", h.SyncDatabase)
		stateRoutes.POST("/export", h.ExportSchema)
		stateRoutes.GET("/check", h.CheckSync)
		stateRoutes.GET("/diff", h.GetDiff)
	}
}

// ValidateSchema validates a schema file
func (h *Handler) ValidateSchema(c *gin.Context) {
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

	h.logger.Info(ctx, "Validating schema", "path", req.SchemaPath)

	result, err := h.migrator.ValidateSchema(ctx, req.SchemaPath)
	if err != nil {
		h.logger.Error(ctx, "Schema validation failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Schema validation failed",
			Code:      "VALIDATION_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := ValidationResponse{
		Valid:        result.Valid,
		Errors:       result.Errors,
		Warnings:     result.Warnings,
		Summary:      result.Summary,
		SchemaPath:   req.SchemaPath,
		ValidatedAt:  time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// PreviewChanges previews schema changes without applying them
func (h *Handler) PreviewChanges(c *gin.Context) {
	ctx := c.Request.Context()

	schemaPath := c.Query("schema_path")
	if schemaPath == "" {
		schemaPath = "database/schema.yaml"
	}

	h.logger.Info(ctx, "Previewing schema changes", "path", schemaPath)

	diff, err := h.migrator.Preview(ctx, schemaPath)
	if err != nil {
		h.logger.Error(ctx, "Preview failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Preview failed",
			Code:      "PREVIEW_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := PreviewResponse{
		HasChanges:        diff.HasChanges,
		TablesAdded:       len(diff.TablesAdded),
		TablesDropped:     len(diff.TablesDropped),
		TablesModified:    len(diff.TablesModified),
		Summary:           diff.Summary,
		Diff:              diff,
		PreviewedAt:       time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// PlanMigration generates a migration from schema diff
func (h *Handler) PlanMigration(c *gin.Context) {
	ctx := c.Request.Context()

	var req PlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Planning migration", "path", req.SchemaPath)

	migration, err := h.migrator.Plan(ctx, req.SchemaPath)
	if err != nil {
		h.logger.Error(ctx, "Migration planning failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Migration planning failed",
			Code:      "PLAN_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	if migration == nil {
		c.JSON(http.StatusOK, PlanResponse{
			HasMigration:    false,
			Message:         "No changes detected - database is in sync with schema",
			PlannedAt:       time.Now(),
		})
		return
	}

	response := PlanResponse{
		HasMigration:     true,
		Migration:        migration,
		MigrationID:      migration.ID,
		Version:          migration.Version,
		SafeToAutoApply:  migration.SafeToAutoApply,
		Warnings:         migration.Warnings,
		UpSQL:            migration.UpSQL,
		DownSQL:          migration.DownSQL,
		PlannedAt:        time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// ApplyMigration applies a generated migration
func (h *Handler) ApplyMigration(c *gin.Context) {
	ctx := c.Request.Context()

	var req ApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Applying migration", "migration_id", req.Migration.ID)

	start := time.Now()
	err := h.migrator.Apply(ctx, req.Migration)
	duration := time.Since(start)

	if err != nil {
		h.logger.Error(ctx, "Migration application failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgMigrationFailed,
			Code:      constants.ErrCodeMigrationFailed,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := ApplyResponse{
		Success:       true,
		MigrationID:   req.Migration.ID,
		ExecutionTime: duration.Milliseconds(),
		AppliedAt:     time.Now(),
		Message:       "Migration applied successfully",
	}

	c.JSON(http.StatusOK, response)
}

// SyncDatabase synchronizes database with schema
func (h *Handler) SyncDatabase(c *gin.Context) {
	ctx := c.Request.Context()

	var req SyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Syncing database", "path", req.SchemaPath, "dryRun", req.DryRun)

	start := time.Now()
	status, err := h.migrator.Sync(ctx, req.SchemaPath, req.DryRun)
	duration := time.Since(start)

	if err != nil {
		h.logger.Error(ctx, "Database sync failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Database sync failed",
			Code:      "SYNC_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := SyncResponse{
		Success:           true,
		Status:            status,
		DryRun:            req.DryRun,
		ExecutionTime:     duration.Milliseconds(),
		SyncedAt:          time.Now(),
		Message:           "Database synchronized successfully",
	}

	c.JSON(http.StatusOK, response)
}

// ExportSchema exports current database schema to file
func (h *Handler) ExportSchema(c *gin.Context) {
	ctx := c.Request.Context()

	var req ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Exporting schema", "path", req.OutputPath, "format", req.Format)

	format := state.SchemaFormat(req.Format)
	err := h.migrator.Export(ctx, req.OutputPath, format)
	if err != nil {
		h.logger.Error(ctx, "Schema export failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Schema export failed",
			Code:      "EXPORT_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := ExportResponse{
		Success:    true,
		OutputPath: req.OutputPath,
		Format:     req.Format,
		ExportedAt: time.Now(),
		Message:    "Schema exported successfully",
	}

	c.JSON(http.StatusOK, response)
}

// CheckSync checks if database is in sync with schema
func (h *Handler) CheckSync(c *gin.Context) {
	ctx := c.Request.Context()

	schemaPath := c.Query("schema_path")
	if schemaPath == "" {
		schemaPath = "database/schema.yaml"
	}

	h.logger.Info(ctx, "Checking sync status", "path", schemaPath)

	diff, err := h.migrator.Preview(ctx, schemaPath)
	if err != nil {
		h.logger.Error(ctx, "Sync check failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Sync check failed",
			Code:      "CHECK_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := CheckSyncResponse{
		InSync:     !diff.HasChanges,
		HasChanges: diff.HasChanges,
		ChangeCount: diff.Summary.TotalChanges,
		Summary:     diff.Summary,
		CheckedAt:   time.Now(),
	}

	if diff.HasChanges {
		response.Message = "Database is OUT OF SYNC with schema"
	} else {
		response.Message = "Database is IN SYNC with schema"
	}

	c.JSON(http.StatusOK, response)
}

// GetDiff gets the detailed diff between current and desired schema
func (h *Handler) GetDiff(c *gin.Context) {
	ctx := c.Request.Context()

	schemaPath := c.Query("schema_path")
	if schemaPath == "" {
		schemaPath = "database/schema.yaml"
	}

	h.logger.Info(ctx, "Getting schema diff", "path", schemaPath)

	diff, err := h.migrator.Preview(ctx, schemaPath)
	if err != nil {
		h.logger.Error(ctx, "Diff retrieval failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Diff retrieval failed",
			Code:      "DIFF_ERROR",
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	response := DiffResponse{
		Diff:       diff,
		SchemaPath: schemaPath,
		RetrievedAt: time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

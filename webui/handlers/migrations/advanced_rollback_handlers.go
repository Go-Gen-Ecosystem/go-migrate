/**
 * @file advanced_rollback_handlers.go
 * @description Advanced rollback HTTP handlers for WebUI
 * @author Admilson B. F. Cossa
 * @date 2025-01-29
 * @path pkg/core/database/migrations/webui/handlers/migrations/advanced_rollback_handlers.go
 */

package migrations

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/rollback"
)

// RollbackSelective handles selective rollback requests
func (h *Handler) RollbackSelective(c *gin.Context) {
	ctx := c.Request.Context()

	var req SelectiveRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Selective rollback requested",
		"migration_id", req.MigrationID,
		"force", req.Force,
		"rollback_dependents", req.RollbackDependents,
		"dry_run", req.DryRun)

	// Get advanced rollback manager from engine
	advMgr, err := h.getAdvancedRollbackManager()
	if err != nil {
		h.logger.Error(ctx, "Failed to get advanced rollback manager", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Advanced rollback not available",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Build options
	options := &rollback.SelectiveRollbackOptions{
		Force:              req.Force,
		RollbackDependents: req.RollbackDependents,
		CreateSavepoint:    req.CreateSavepoint,
		DryRun:             req.DryRun,
		SkipImpactAnalysis: false,
		MaxDepth:           req.MaxDepth,
		CreateBackup:       req.CreateBackup,
	}

	// Broadcast rollback start
	if h.wsHub != nil {
		h.wsHub.BroadcastRollbackProgress(map[string]interface{}{
			"migration_id": req.MigrationID,
			"status":       "started",
			"timestamp":    time.Now(),
		})
	}

	// Execute selective rollback
	start := time.Now()
	result, err := advMgr.RollbackSelective(ctx, req.MigrationID, options)
	duration := time.Since(start)

	// Broadcast completion or failure
	if h.wsHub != nil {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		h.wsHub.BroadcastRollbackProgress(map[string]interface{}{
			"migration_id": req.MigrationID,
			"status":       status,
			"duration":     duration.String(),
			"timestamp":    time.Now(),
		})
	}

	if err != nil {
		h.logger.Error(ctx, "Selective rollback failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     constants.MsgRollbackFailed,
			Code:      constants.ErrCodeRollbackFailed,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Convert to response
	response := h.convertToSelectiveRollbackResponse(result, duration)
	c.JSON(http.StatusOK, response)
}

// AnalyzeRollback handles rollback impact analysis requests
func (h *Handler) AnalyzeRollback(c *gin.Context) {
	ctx := c.Request.Context()

	var req AnalyzeRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Rollback impact analysis requested",
		"migration_ids", req.MigrationIDs)

	// Get advanced rollback manager
	advMgr, err := h.getAdvancedRollbackManager()
	if err != nil {
		h.logger.Error(ctx, "Failed to get advanced rollback manager", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Advanced rollback not available",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Perform impact analysis
	analysis, err := advMgr.AnalyzeRollbackImpact(ctx, req.MigrationIDs)

	// Broadcast analysis results via WebSocket
	if h.wsHub != nil && analysis != nil {
		h.wsHub.BroadcastRollbackAnalysis(map[string]interface{}{
			"migration_ids": req.MigrationIDs,
			"impact_level":  analysis.ImpactLevel,
			"can_proceed":   analysis.CanProceedSafely,
			"data_loss_risk": analysis.DataLossRisk,
			"timestamp":     time.Now(),
		})
	}

	if err != nil {
		h.logger.Error(ctx, "Impact analysis failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Impact analysis failed",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Convert to response
	response := h.convertToImpactAnalysisResponse(analysis)
	c.JSON(http.StatusOK, response)
}

// TestRollback handles rollback testing requests
func (h *Handler) TestRollback(c *gin.Context) {
	ctx := c.Request.Context()

	var req TestRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     constants.MsgInvalidRequest,
			Code:      constants.ErrCodeInvalidRequest,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Rollback testing requested",
		"migration_ids", req.MigrationIDs,
		"environment", req.Environment)

	// Get advanced rollback manager
	advMgr, err := h.getAdvancedRollbackManager()
	if err != nil {
		h.logger.Error(ctx, "Failed to get advanced rollback manager", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Advanced rollback not available",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Test rollback
	result, err := advMgr.TestRollback(ctx, req.MigrationIDs, req.Environment)

	// Broadcast test results via WebSocket
	if h.wsHub != nil && result != nil {
		h.wsHub.BroadcastRollbackTest(map[string]interface{}{
			"migration_ids": req.MigrationIDs,
			"environment":   req.Environment,
			"success":       result.Success,
			"total_tests":   result.TotalTests,
			"passed_tests":  result.PassedTests,
			"failed_tests":  result.FailedTests,
			"timestamp":     time.Now(),
		})
	}

	if err != nil {
		h.logger.Error(ctx, "Rollback testing failed", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Rollback testing failed",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Convert to response
	response := h.convertToTestRollbackResponse(result)
	c.JSON(http.StatusOK, response)
}

// GetDependencyGraph handles dependency graph requests
func (h *Handler) GetDependencyGraph(c *gin.Context) {
	ctx := c.Request.Context()

	migrationID := c.Param("migration_id")
	if migrationID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:     "Migration ID is required",
			Code:      constants.ErrCodeInvalidRequest,
			Details:   "migration_id parameter is missing",
			Timestamp: time.Now(),
		})
		return
	}

	h.logger.Info(ctx, "Dependency graph requested", "migration_id", migrationID)

	// Get advanced rollback manager
	advMgr, err := h.getAdvancedRollbackManager()
	if err != nil {
		h.logger.Error(ctx, "Failed to get advanced rollback manager", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Advanced rollback not available",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Get dependency graph
	graph, err := advMgr.GetRollbackDependencyGraph(ctx, migrationID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get dependency graph", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:     "Failed to get dependency graph",
			Code:      constants.ErrCodeInternalError,
			Details:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	// Convert to response
	response := h.convertToDependencyGraphResponse(graph)
	c.JSON(http.StatusOK, response)
}

// Helper: getAdvancedRollbackManager gets the advanced rollback manager from the migration tool
func (h *Handler) getAdvancedRollbackManager() (rollback.AdvancedRollbackManager, error) {
	// Cast migration tool to engine if possible
	if eng, ok := h.migrationTool.(*engine.MigrationEngine); ok {
		rollbackMgr := eng.GetRollbackManager()
		if rollbackMgr == nil {
			return nil, fmt.Errorf("rollback manager not configured")
		}

		advMgr := rollbackMgr.GetAdvancedManager()
		if advMgr == nil {
			return nil, fmt.Errorf("advanced rollback manager not configured")
		}

		return advMgr, nil
	}

	return nil, fmt.Errorf("migration tool does not support advanced rollback")
}

// Converter functions

func (h *Handler) convertToSelectiveRollbackResponse(result *rollback.SelectiveRollbackResult, duration time.Duration) SelectiveRollbackResponse {
	response := SelectiveRollbackResponse{
		Success:      result.Success,
		MigrationID:  result.MigrationID,
		Errors:       result.Errors,
		Warnings:     result.Warnings,
		Duration:     duration.String(),
		Savepoints:   result.Savepoints,
		ExecutedAt:   result.ExecutedAt,
	}

	if result.BackupLocation != "" {
		response.BackupLocation = result.BackupLocation
	}

	// Convert rolled back migrations
	response.RolledBackMigrations = make([]MigrationSummary, len(result.RolledBackMigrations))
	for i, mig := range result.RolledBackMigrations {
		response.RolledBackMigrations[i] = MigrationSummary{
			ID:          mig.ID,
			Version:     mig.Version,
			Description: mig.Description,
		}
	}

	// Include impact analysis if available
	if result.ImpactAnalysis != nil {
		response.ImpactAnalysis = h.convertToImpactAnalysisResponse(result.ImpactAnalysis)
	}

	return response
}

func (h *Handler) convertToImpactAnalysisResponse(analysis *rollback.RollbackImpactAnalysis) ImpactAnalysisResponse {
	response := ImpactAnalysisResponse{
		MigrationID:         analysis.MigrationID,
		ImpactLevel:         string(analysis.ImpactLevel),
		AffectedTables:      analysis.AffectedTables,
		AffectedColumns:     analysis.AffectedColumns,
		AffectedIndexes:     analysis.AffectedIndexes,
		AffectedConstraints: analysis.AffectedConstraints,
		DataLossRisk:        analysis.DataLossRisk,
		EstimatedDuration:   analysis.EstimatedDuration.String(),
		Recommendations:     analysis.Recommendations,
		Warnings:            analysis.Warnings,
		CanProceedSafely:    analysis.CanProceedSafely,
		RequiresManual:      analysis.RequiresManual,
		AnalyzedAt:          analysis.AnalyzedAt,
	}

	// Convert affected migrations
	response.AffectedMigrations = make([]AffectedMigrationInfo, len(analysis.AffectedMigrations))
	for i, impact := range analysis.AffectedMigrations {
		response.AffectedMigrations[i] = AffectedMigrationInfo{
			Migration: MigrationSummary{
				ID:          impact.Migration.ID,
				Version:     impact.Migration.Version,
				Description: impact.Migration.Description,
			},
			ImpactType:  impact.ImpactType,
			ImpactLevel: string(impact.ImpactLevel),
			CanRollback: impact.CanRollback,
			Reason:      impact.Reason,
		}
	}

	// Convert conflicts
	response.Conflicts = make([]ConflictInfo, len(analysis.Conflicts))
	for i, conflict := range analysis.Conflicts {
		response.Conflicts[i] = ConflictInfo{
			Type:            conflict.Type,
			Severity:        conflict.Severity,
			Description:     conflict.Description,
			AffectedObjects: conflict.AffectedObjects,
			Resolution:      conflict.Resolution,
			CanAutoResolve:  conflict.CanAutoResolve,
			RequiresManual:  conflict.RequiresManual,
		}
	}

	// Convert estimated data loss
	if analysis.EstimatedDataLoss != nil {
		response.EstimatedDataLoss = &DataLossEstimateInfo{
			TablesAffected:  analysis.EstimatedDataLoss.TablesAffected,
			EstimatedRows:   analysis.EstimatedDataLoss.EstimatedRows,
			TotalEstimate:   analysis.EstimatedDataLoss.TotalEstimate,
			BackupRequired:  analysis.EstimatedDataLoss.BackupRequired,
		}
	}

	return response
}

func (h *Handler) convertToTestRollbackResponse(result *rollback.RollbackTestResult) TestRollbackResponse {
	response := TestRollbackResponse{
		Success:        result.Success,
		Environment:    result.Environment,
		TestedAt:       result.TestedAt,
		Duration:       result.Duration.String(),
		TotalTests:     result.TotalTests,
		PassedTests:    result.PassedTests,
		FailedTests:    result.FailedTests,
		SkippedTests:   result.SkippedTests,
		Coverage:       result.Coverage,
		Errors:         result.Errors,
		Warnings:       result.Warnings,
		Recommendation: result.Recommendation,
	}

	// Convert test cases
	response.TestCases = make([]TestCaseInfo, len(result.TestCases))
	for i, tc := range result.TestCases {
		response.TestCases[i] = TestCaseInfo{
			Name:            tc.Name,
			Description:     tc.Description,
			MigrationID:     tc.MigrationID,
			Status:          tc.Status,
			Duration:        tc.Duration.String(),
			Error:           tc.Error,
			Assertions:      tc.Assertions,
			VerifiedObjects: tc.VerifiedObjects,
		}
	}

	return response
}

func (h *Handler) convertToDependencyGraphResponse(graph *rollback.DependencyGraph) DependencyGraphResponse {
	response := DependencyGraphResponse{
		RootMigration: MigrationSummary{
			ID:          graph.RootMigration.ID,
			Version:     graph.RootMigration.Version,
			Description: graph.RootMigration.Description,
		},
		Levels:       graph.Levels,
		MaxDepth:     graph.MaxDepth,
		TotalNodes:   graph.TotalNodes,
		HasCycles:    graph.HasCycles,
		CyclicPaths:  graph.CyclicPaths,
	}

	// Convert nodes
	response.Nodes = make(map[string]DependencyNodeInfo)
	for nodeID, node := range graph.Nodes {
		response.Nodes[nodeID] = DependencyNodeInfo{
			Migration: MigrationSummary{
				ID:          node.Migration.ID,
				Version:     node.Migration.Version,
				Description: node.Migration.Description,
			},
			Level:          node.Level,
			Dependencies:   node.Dependencies,
			Dependents:     node.Dependents,
			IsRollbackable: node.IsRollbackable,
		}
	}

	// Convert edges
	response.Edges = make([]DependencyEdgeInfo, len(graph.Edges))
	for i, edge := range graph.Edges {
		response.Edges[i] = DependencyEdgeInfo{
			From:       edge.From,
			To:         edge.To,
			EdgeType:   edge.EdgeType,
			IsBlocking: edge.IsBlocking,
		}
	}

	return response
}

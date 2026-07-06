package webui

import (
	"context"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/state"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/handlers/catalog"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/handlers/collaboration"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/handlers/designer"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/handlers/migrations"
	statehandlers "github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/handlers/state"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/integration"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/server"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/websocket"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gin-gonic/gin"
)

// WebUI represents the web UI integration
type WebUI struct {
	server *server.Server
	hub    *websocket.Hub
	logger logging.Logger
}

// NewWebUI creates a new web UI instance
func NewWebUI(cfg *config.Config, logger logging.Logger) (*WebUI, error) {
	// Create web UI server
	srv, err := server.New(cfg, logger)
	if err != nil {
		return nil, err
	}

	// Create WebSocket hub
	hub := websocket.NewHub(logger)

	return &WebUI{
		server: srv,
		hub:    hub,
		logger: logger,
	}, nil
}

// Start starts the web UI server
func (w *WebUI) Start(ctx context.Context) error {
	// Start WebSocket hub
	go w.hub.Run()

	// Start server
	return w.server.Start(ctx)
}

// Shutdown gracefully shuts down the web UI
func (w *WebUI) Shutdown(ctx context.Context) error {
	w.hub.Shutdown()
	return w.server.Shutdown(ctx)
}

// RegisterHandlers registers web UI handlers with migration tool
func (w *WebUI) RegisterHandlers(migrationTool types.MigrationTool) {
	router := w.server.Router()
	api := router.Group("/api/v1")

	// Register migration handlers
	migrationHandler := migrations.NewHandler(migrationTool, w.logger)
	migrationHandler.RegisterRoutes(api)

	// Register catalog handlers
	catalogHandler := catalog.NewHandler(w.logger)
	catalogHandler.RegisterRoutes(api)

	// Register collaboration handlers
	collaborationHandler := collaboration.NewHandler(w.logger)
	collaborationHandler.RegisterRoutes(api)

	// Register designer handlers
	designerHandler := designer.NewHandler(w.logger)
	designerHandler.RegisterRoutes(api)

	// Register WebSocket handlers
	wsHandler := websocket.NewHandler(w.hub, w.server.Config().WebSocket, w.logger)
	wsGroup := router.Group("/ws")
	wsHandler.RegisterRoutes(wsGroup)
}

// RegisterStateBasedHandlers registers state-based migration handlers
func (w *WebUI) RegisterStateBasedHandlers(migrator state.StateBasedMigrator) {
	router := w.server.Router()
	api := router.Group("/api/v1")

	// Register state-based migration handlers
	stateHandler := statehandlers.NewHandler(migrator, w.logger)
	stateHandler.RegisterRoutes(api)
}

// RegisterWithExistingRouter registers web UI routes with an existing Gin router
func RegisterWithExistingRouter(router *gin.Engine, migrationTool types.MigrationTool, cfg *config.Config, logger logging.Logger) (*websocket.Hub, error) {
	// Create WebSocket hub
	hub := websocket.NewHub(logger)
	go hub.Run()

	api := router.Group("/api/v1")

	// Register migration handlers
	migrationHandler := migrations.NewHandler(migrationTool, logger)
	migrationHandler.RegisterRoutes(api)

	// Register catalog handlers
	catalogHandler := catalog.NewHandler(logger)
	catalogHandler.RegisterRoutes(api)

	// Register collaboration handlers
	collaborationHandler := collaboration.NewHandler(logger)
	collaborationHandler.RegisterRoutes(api)

	// Register designer handlers
	designerHandler := designer.NewHandler(logger)
	designerHandler.RegisterRoutes(api)

	// Register WebSocket handlers
	wsHandler := websocket.NewHandler(hub, &cfg.WebSocket, logger)
	wsGroup := router.Group("/ws")
	wsHandler.RegisterRoutes(wsGroup)

	return hub, nil
}

// Hub returns the WebSocket hub for broadcasting messages
func (w *WebUI) Hub() *websocket.Hub {
	return w.hub
}

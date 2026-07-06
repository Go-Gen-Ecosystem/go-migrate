package websocket

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Implement proper origin checking based on config
		return true
	},
}

// Handler handles WebSocket connections
type Handler struct {
	hub    *Hub
	config *config.WebSocketConfig
	logger logging.Logger
}

// NewHandler creates a new WebSocket handler
func NewHandler(hub *Hub, cfg *config.WebSocketConfig, logger logging.Logger) *Handler {
	// Update upgrader with config
	upgrader.ReadBufferSize = cfg.ReadBufferSize
	upgrader.WriteBufferSize = cfg.WriteBufferSize
	upgrader.EnableCompression = cfg.EnableCompression

	return &Handler{
		hub:    hub,
		config: cfg,
		logger: logger,
	}
}

// RegisterRoutes registers WebSocket routes
func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/connect", h.HandleConnection)
	router.GET("/stats", h.HandleStats)
}

// HandleConnection handles WebSocket connection upgrade
func (h *Handler) HandleConnection(c *gin.Context) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error(c.Request.Context(), "Failed to upgrade WebSocket connection", err)
		return
	}

	// Generate client ID
	clientID := uuid.New().String()

	// Create client
	client := NewClient(h.hub, conn, clientID, h.config, h.logger)

	// Register client
	h.hub.register <- client

	// Start client pumps
	client.Start()

	h.logger.Info(c.Request.Context(), "WebSocket client connected", "clientID", clientID)
}

// HandleStats returns WebSocket hub statistics
func (h *Handler) HandleStats(c *gin.Context) {
	stats := h.hub.GetStats()
	c.JSON(http.StatusOK, stats)
}

package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/middleware"
)

//go:embed static/*
var staticAssets embed.FS

//go:embed templates/*
var templates embed.FS

// Server represents the web UI HTTP server
type Server struct {
	config *config.Config
	router *gin.Engine
	server *http.Server
	logger logging.Logger
}

// New creates a new web UI server
func New(cfg *config.Config, logger logging.Logger) (*Server, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Set Gin mode
	gin.SetMode(cfg.Mode)

	router := gin.New()

	s := &Server{
		config: cfg,
		router: router,
		logger: logger,
	}

	// Setup middleware
	s.setupMiddleware()

	// Setup routes
	s.setupRoutes()

	// Create HTTP server
	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s, nil
}

// setupMiddleware configures middleware stack
func (s *Server) setupMiddleware() {
	// Recovery middleware (must be first)
	s.router.Use(middleware.Recovery(s.logger))

	// Request ID middleware
	s.router.Use(middleware.RequestID())

	// Logging middleware
	s.router.Use(middleware.Logger(s.logger))

	// Security headers
	s.router.Use(middleware.SecurityHeaders())

	// CORS middleware
	if len(s.config.Security.AllowedOrigins) > 0 {
		s.router.Use(middleware.CORS(s.config.Security.AllowedOrigins))
	}

	// Rate limiting
	if s.config.Security.EnableRateLimit {
		s.router.Use(middleware.RateLimit(
			s.config.Security.RateLimitRequests,
			s.config.Security.RateLimitWindow,
		))
	}

	// Request size limiter
	s.router.Use(middleware.MaxRequestSize(s.config.MaxRequestSize))

	// Compression
	s.router.Use(middleware.Gzip())
}

// setupRoutes configures all routes
func (s *Server) setupRoutes() {
	// Health check endpoints
	s.router.GET("/health", s.healthHandler)
	s.router.GET("/ready", s.readyHandler)

	// Serve static assets
	s.setupStaticFiles()

	// API routes
	if s.config.Features.EnableMigrationCatalog ||
		s.config.Features.EnableDesigner ||
		s.config.Features.EnableCollaboration {
		s.setupAPIRoutes()
	}

	// WebSocket routes
	if s.config.WebSocket.Enabled && s.config.Features.EnableRealTimeUpdates {
		s.setupWebSocketRoutes()
	}

	// Web UI routes (SPA fallback)
	if s.config.EnableSPA {
		s.router.NoRoute(s.spaFallbackHandler)
	}
}

// setupStaticFiles configures static file serving
func (s *Server) setupStaticFiles() {
	// Determine static files source
	var staticFS fs.FS
	var err error

	// Try to use local filesystem first (for development)
	if _, err = os.Stat(s.config.StaticAssetsDir); err == nil {
		s.logger.Info(context.Background(), "Serving static files from filesystem",
			"path", s.config.StaticAssetsDir)
		s.router.Static(constants.StaticAssetsPrefix, s.config.StaticAssetsDir)
	} else {
		// Fall back to embedded assets
		s.logger.Info(context.Background(), "Serving static files from embedded assets")
		staticFS, err = fs.Sub(staticAssets, "static")
		if err != nil {
			s.logger.Error(context.Background(), "Failed to load embedded static assets", err)
			return
		}
		s.router.StaticFS(constants.StaticAssetsPrefix, http.FS(staticFS))
	}
}

// setupAPIRoutes configures API routes
func (s *Server) setupAPIRoutes() {
	api := s.router.Group(constants.APIBasePrefix)

	// Optional authentication middleware
	if s.config.Security.EnableAuth {
		api.Use(middleware.Auth())
	}

	// Optional RBAC middleware
	if s.config.Security.EnableRBAC {
		api.Use(middleware.RBAC())
	}

	// Note: Actual handlers will be registered by handler packages
	// This is just the route group setup
}

// setupWebSocketRoutes configures WebSocket routes
func (s *Server) setupWebSocketRoutes() {
	ws := s.router.Group(constants.WebSocketPrefix)

	// Optional authentication for WebSocket
	if s.config.Security.EnableAuth {
		ws.Use(middleware.Auth())
	}

	// Note: WebSocket handlers will be registered by the websocket package
}

// healthHandler handles health check requests
func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().UTC(),
	})
}

// readyHandler handles readiness check requests
func (s *Server) readyHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ready": true,
		"time":  time.Now().UTC(),
	})
}

// spaFallbackHandler serves the SPA index.html for client-side routing
func (s *Server) spaFallbackHandler(c *gin.Context) {
	// Try to serve from local filesystem first
	indexPath := filepath.Join(s.config.StaticAssetsDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		c.File(indexPath)
		return
	}

	// Fall back to embedded templates
	data, err := templates.ReadFile("templates/index.html")
	if err != nil {
		s.logger.Error(c.Request.Context(), "Failed to load index.html", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Page not found"})
		return
	}

	c.Data(http.StatusOK, constants.ContentTypeHTML, data)
}

// Start starts the web UI server
func (s *Server) Start(ctx context.Context) error {
	addr := s.server.Addr
	s.logger.Info(ctx, "Starting web UI server",
		"address", addr,
		"mode", s.config.Mode)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-errChan:
		return err
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info(ctx, "Shutting down web UI server")

	shutdownCtx, cancel := context.WithTimeout(ctx, s.config.ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	s.logger.Info(ctx, "Web UI server stopped")
	return nil
}

// Router returns the underlying Gin router for route registration
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Config returns the server configuration
func (s *Server) Config() *config.Config {
	return s.config
}

// Address returns the server address
func (s *Server) Address() string {
	return s.server.Addr
}

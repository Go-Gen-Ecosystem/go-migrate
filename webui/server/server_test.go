package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
)

func TestNew(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()

	server, err := New(cfg, logger)
	require.NoError(t, err)
	assert.NotNil(t, server)
	assert.Equal(t, cfg, server.config)
}

func TestNew_WithInvalidConfig(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := &config.Config{
		Port: 70000, // Invalid port
	}

	server, err := New(cfg, logger)
	assert.Error(t, err)
	assert.Nil(t, server)
}

func TestHealthHandler(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()

	server, err := New(cfg, logger)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestReadyHandler(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()

	server, err := New(cfg, logger)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)

	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ready")
}

func TestServer_Config(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()

	server, err := New(cfg, logger)
	require.NoError(t, err)

	assert.Equal(t, cfg, server.Config())
}

func TestServer_Router(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()

	server, err := New(cfg, logger)
	require.NoError(t, err)

	assert.NotNil(t, server.Router())
}

func TestServer_Address(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()
	cfg.Host = "localhost"
	cfg.Port = 9090

	server, err := New(cfg, logger)
	require.NoError(t, err)

	assert.Equal(t, "localhost:9090", server.Address())
}

func TestServer_Shutdown(t *testing.T) {
	logger := logging.NewSimpleLogger()
	cfg := config.DefaultConfig()
	cfg.Port = 0 // Use random port

	server, err := New(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = server.Shutdown(ctx)
	assert.NoError(t, err)
}

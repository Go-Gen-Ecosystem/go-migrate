package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.NotNil(t, cfg)
	assert.Equal(t, constants.DefaultWebUIHost, cfg.Host)
	assert.Equal(t, constants.DefaultWebUIPort, cfg.Port)
	assert.Equal(t, "release", cfg.Mode)
	assert.True(t, cfg.WebSocket.Enabled)
	assert.True(t, cfg.Collaboration.Enabled)
	assert.True(t, cfg.Catalog.Enabled)
	assert.True(t, cfg.Designer.Enabled)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr error
	}{
		{
			name:    "Valid config",
			config:  DefaultConfig(),
			wantErr: nil,
		},
		{
			name: "Invalid port - too low",
			config: &Config{
				Port: 0,
				Mode: "release",
			},
			wantErr: ErrInvalidPort,
		},
		{
			name: "Invalid port - too high",
			config: &Config{
				Port: 70000,
				Mode: "release",
			},
			wantErr: ErrInvalidPort,
		},
		{
			name: "Invalid mode",
			config: &Config{
				Port: 8080,
				Mode: "invalid",
			},
			wantErr: ErrInvalidMode,
		},
		{
			name: "Invalid read timeout",
			config: &Config{
				Port:        8080,
				Mode:        "release",
				ReadTimeout: -1 * time.Second,
			},
			wantErr: ErrInvalidReadTimeout,
		},
		{
			name: "Invalid write timeout",
			config: &Config{
				Port:         8080,
				Mode:         "release",
				ReadTimeout:  1 * time.Second,
				WriteTimeout: -1 * time.Second,
			},
			wantErr: ErrInvalidWriteTimeout,
		},
		{
			name: "Invalid min approvals",
			config: &Config{
				Port:         8080,
				Mode:         "release",
				ReadTimeout:  1 * time.Second,
				WriteTimeout: 1 * time.Second,
				Collaboration: CollaborationConfig{
					Enabled:      true,
					MinApprovals: -1,
				},
			},
			wantErr: ErrInvalidMinApprovals,
		},
		{
			name: "Invalid page size",
			config: &Config{
				Port:         8080,
				Mode:         "release",
				ReadTimeout:  1 * time.Second,
				WriteTimeout: 1 * time.Second,
				Catalog: CatalogConfig{
					Enabled:  true,
					PageSize: 0,
				},
			},
			wantErr: ErrInvalidPageSize,
		},
		{
			name: "Invalid max tables",
			config: &Config{
				Port:         8080,
				Mode:         "release",
				ReadTimeout:  1 * time.Second,
				WriteTimeout: 1 * time.Second,
				Designer: DesignerConfig{
					Enabled:             true,
					MaxTablesPerDiagram: 0,
				},
			},
			wantErr: ErrInvalidMaxTables,
		},
		{
			name: "Invalid rate limit",
			config: &Config{
				Port:         8080,
				Mode:         "release",
				ReadTimeout:  1 * time.Second,
				WriteTimeout: 1 * time.Second,
				Security: SecurityConfig{
					EnableRateLimit:   true,
					RateLimitRequests: 0,
				},
			},
			wantErr: ErrInvalidRateLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWebSocketConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.WebSocket.Enabled)
	assert.Equal(t, constants.DefaultWSPingInterval, cfg.WebSocket.PingInterval)
	assert.Equal(t, constants.DefaultWSPongWait, cfg.WebSocket.PongWait)
	assert.Equal(t, constants.DefaultWSWriteWait, cfg.WebSocket.WriteWait)
	assert.Equal(t, constants.DefaultWSMaxMessageSize, cfg.WebSocket.MaxMessageSize)
}

func TestCollaborationConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.Collaboration.Enabled)
	assert.False(t, cfg.Collaboration.RequireReview)
	assert.Equal(t, 1, cfg.Collaboration.MinApprovals)
	assert.True(t, cfg.Collaboration.EnableComments)
	assert.True(t, cfg.Collaboration.EnableNotifications)
}

func TestCatalogConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.Catalog.Enabled)
	assert.Equal(t, constants.DefaultCatalogPageSize, cfg.Catalog.PageSize)
	assert.Equal(t, constants.DefaultSearchLimit, cfg.Catalog.MaxSearchResults)
	assert.True(t, cfg.Catalog.EnableFullText)
}

func TestDesignerConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.Designer.Enabled)
	assert.Equal(t, 50, cfg.Designer.MaxTablesPerDiagram)
	assert.True(t, cfg.Designer.EnableAutoLayout)
	assert.True(t, cfg.Designer.EnableValidation)
	assert.Contains(t, cfg.Designer.SupportedFormats, "sql")
}

func TestSecurityConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.Security.EnableAuth)
	assert.True(t, cfg.Security.EnableRBAC)
	assert.True(t, cfg.Security.EnableAudit)
	assert.True(t, cfg.Security.EnableCSRF)
	assert.True(t, cfg.Security.EnableRateLimit)
	assert.Equal(t, 100, cfg.Security.RateLimitRequests)
}

func TestFeatureFlags(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.Features.EnableMigrationCatalog)
	assert.True(t, cfg.Features.EnableDesigner)
	assert.True(t, cfg.Features.EnableCollaboration)
	assert.True(t, cfg.Features.EnableRealTimeUpdates)
	assert.True(t, cfg.Features.EnableDryRun)
	assert.True(t, cfg.Features.EnableRollback)
}

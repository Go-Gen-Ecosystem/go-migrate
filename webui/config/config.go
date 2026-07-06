package config

import (
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// Config represents the web UI server configuration
type Config struct {
	// Server settings
	Host            string        `json:"host" yaml:"host" mapstructure:"host"`
	Port            int           `json:"port" yaml:"port" mapstructure:"port"`
	Mode            string        `json:"mode" yaml:"mode" mapstructure:"mode"` // debug, release, test
	ReadTimeout     time.Duration `json:"readTimeout" yaml:"readTimeout" mapstructure:"readTimeout"`
	WriteTimeout    time.Duration `json:"writeTimeout" yaml:"writeTimeout" mapstructure:"writeTimeout"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout" yaml:"shutdownTimeout" mapstructure:"shutdownTimeout"`
	MaxRequestSize  int64         `json:"maxRequestSize" yaml:"maxRequestSize" mapstructure:"maxRequestSize"`

	// Static assets
	StaticAssetsDir string `json:"staticAssetsDir" yaml:"staticAssetsDir" mapstructure:"staticAssetsDir"`
	TemplatesDir    string `json:"templatesDir" yaml:"templatesDir" mapstructure:"templatesDir"`
	EnableSPA       bool   `json:"enableSPA" yaml:"enableSPA" mapstructure:"enableSPA"`

	// WebSocket settings
	WebSocket WebSocketConfig `json:"webSocket" yaml:"webSocket" mapstructure:"webSocket"`

	// Collaboration settings
	Collaboration CollaborationConfig `json:"collaboration" yaml:"collaboration" mapstructure:"collaboration"`

	// Catalog settings
	Catalog CatalogConfig `json:"catalog" yaml:"catalog" mapstructure:"catalog"`

	// Designer settings
	Designer DesignerConfig `json:"designer" yaml:"designer" mapstructure:"designer"`

	// Security settings
	Security SecurityConfig `json:"security" yaml:"security" mapstructure:"security"`

	// Cache settings
	Cache CacheConfig `json:"cache" yaml:"cache" mapstructure:"cache"`

	// Feature flags
	Features FeatureFlags `json:"features" yaml:"features" mapstructure:"features"`
}

// WebSocketConfig represents WebSocket configuration
type WebSocketConfig struct {
	Enabled           bool          `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	PingInterval      time.Duration `json:"pingInterval" yaml:"pingInterval" mapstructure:"pingInterval"`
	PongWait          time.Duration `json:"pongWait" yaml:"pongWait" mapstructure:"pongWait"`
	WriteWait         time.Duration `json:"writeWait" yaml:"writeWait" mapstructure:"writeWait"`
	MaxMessageSize    int64         `json:"maxMessageSize" yaml:"maxMessageSize" mapstructure:"maxMessageSize"`
	ReadBufferSize    int           `json:"readBufferSize" yaml:"readBufferSize" mapstructure:"readBufferSize"`
	WriteBufferSize   int           `json:"writeBufferSize" yaml:"writeBufferSize" mapstructure:"writeBufferSize"`
	EnableCompression bool          `json:"enableCompression" yaml:"enableCompression" mapstructure:"enableCompression"`
}

// CollaborationConfig represents collaboration features configuration
type CollaborationConfig struct {
	Enabled              bool          `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	RequireReview        bool          `json:"requireReview" yaml:"requireReview" mapstructure:"requireReview"`
	MinApprovals         int           `json:"minApprovals" yaml:"minApprovals" mapstructure:"minApprovals"`
	ReviewTimeout        time.Duration `json:"reviewTimeout" yaml:"reviewTimeout" mapstructure:"reviewTimeout"`
	EnableComments       bool          `json:"enableComments" yaml:"enableComments" mapstructure:"enableComments"`
	EnableNotifications  bool          `json:"enableNotifications" yaml:"enableNotifications" mapstructure:"enableNotifications"`
	EnableTeams          bool          `json:"enableTeams" yaml:"enableTeams" mapstructure:"enableTeams"`
	EnableWorkflows      bool          `json:"enableWorkflows" yaml:"enableWorkflows" mapstructure:"enableWorkflows"`
	AllowSelfApproval    bool          `json:"allowSelfApproval" yaml:"allowSelfApproval" mapstructure:"allowSelfApproval"`
}

// CatalogConfig represents migration catalog configuration
type CatalogConfig struct {
	Enabled           bool   `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	PageSize          int    `json:"pageSize" yaml:"pageSize" mapstructure:"pageSize"`
	MaxSearchResults  int    `json:"maxSearchResults" yaml:"maxSearchResults" mapstructure:"maxSearchResults"`
	EnableFullText    bool   `json:"enableFullText" yaml:"enableFullText" mapstructure:"enableFullText"`
	EnableTags        bool   `json:"enableTags" yaml:"enableTags" mapstructure:"enableTags"`
	EnableCategories  bool   `json:"enableCategories" yaml:"enableCategories" mapstructure:"enableCategories"`
	EnableVersioning  bool   `json:"enableVersioning" yaml:"enableVersioning" mapstructure:"enableVersioning"`
	IndexPath         string `json:"indexPath" yaml:"indexPath" mapstructure:"indexPath"`
}

// DesignerConfig represents migration designer configuration
type DesignerConfig struct {
	Enabled              bool     `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	MaxTablesPerDiagram  int      `json:"maxTablesPerDiagram" yaml:"maxTablesPerDiagram" mapstructure:"maxTablesPerDiagram"`
	EnableAutoLayout     bool     `json:"enableAutoLayout" yaml:"enableAutoLayout" mapstructure:"enableAutoLayout"`
	EnableValidation     bool     `json:"enableValidation" yaml:"enableValidation" mapstructure:"enableValidation"`
	EnableTemplates      bool     `json:"enableTemplates" yaml:"enableTemplates" mapstructure:"enableTemplates"`
	SupportedFormats     []string `json:"supportedFormats" yaml:"supportedFormats" mapstructure:"supportedFormats"`
	DesignsDir           string   `json:"designsDir" yaml:"designsDir" mapstructure:"designsDir"`
	EnableReverseEngineer bool    `json:"enableReverseEngineer" yaml:"enableReverseEngineer" mapstructure:"enableReverseEngineer"`
}

// SecurityConfig represents security configuration
type SecurityConfig struct {
	EnableAuth        bool          `json:"enableAuth" yaml:"enableAuth" mapstructure:"enableAuth"`
	EnableRBAC        bool          `json:"enableRBAC" yaml:"enableRBAC" mapstructure:"enableRBAC"`
	EnableAudit       bool          `json:"enableAudit" yaml:"enableAudit" mapstructure:"enableAudit"`
	SessionTimeout    time.Duration `json:"sessionTimeout" yaml:"sessionTimeout" mapstructure:"sessionTimeout"`
	EnableCSRF        bool          `json:"enableCSRF" yaml:"enableCSRF" mapstructure:"enableCSRF"`
	EnableRateLimit   bool          `json:"enableRateLimit" yaml:"enableRateLimit" mapstructure:"enableRateLimit"`
	RateLimitRequests int           `json:"rateLimitRequests" yaml:"rateLimitRequests" mapstructure:"rateLimitRequests"`
	RateLimitWindow   time.Duration `json:"rateLimitWindow" yaml:"rateLimitWindow" mapstructure:"rateLimitWindow"`
	TrustedProxies    []string      `json:"trustedProxies" yaml:"trustedProxies" mapstructure:"trustedProxies"`
	AllowedOrigins    []string      `json:"allowedOrigins" yaml:"allowedOrigins" mapstructure:"allowedOrigins"`
}

// CacheConfig represents caching configuration for web UI
type CacheConfig struct {
	Enabled          bool          `json:"enabled" yaml:"enabled" mapstructure:"enabled"`
	Type             string        `json:"type" yaml:"type" mapstructure:"type"` // memory, redis
	DefaultTTL       time.Duration `json:"defaultTTL" yaml:"defaultTTL" mapstructure:"defaultTTL"`
	MaxSize          int           `json:"maxSize" yaml:"maxSize" mapstructure:"maxSize"`
	EnableMetrics    bool          `json:"enableMetrics" yaml:"enableMetrics" mapstructure:"enableMetrics"`
}

// FeatureFlags represents feature toggles
type FeatureFlags struct {
	EnableMigrationCatalog  bool `json:"enableMigrationCatalog" yaml:"enableMigrationCatalog" mapstructure:"enableMigrationCatalog"`
	EnableDesigner          bool `json:"enableDesigner" yaml:"enableDesigner" mapstructure:"enableDesigner"`
	EnableCollaboration     bool `json:"enableCollaboration" yaml:"enableCollaboration" mapstructure:"enableCollaboration"`
	EnableRealTimeUpdates   bool `json:"enableRealTimeUpdates" yaml:"enableRealTimeUpdates" mapstructure:"enableRealTimeUpdates"`
	EnableDryRun            bool `json:"enableDryRun" yaml:"enableDryRun" mapstructure:"enableDryRun"`
	EnableRollback          bool `json:"enableRollback" yaml:"enableRollback" mapstructure:"enableRollback"`
	EnableSchemaComparison  bool `json:"enableSchemaComparison" yaml:"enableSchemaComparison" mapstructure:"enableSchemaComparison"`
	EnableDriftDetection    bool `json:"enableDriftDetection" yaml:"enableDriftDetection" mapstructure:"enableDriftDetection"`
	EnableBackup            bool `json:"enableBackup" yaml:"enableBackup" mapstructure:"enableBackup"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Host:            constants.DefaultWebUIHost,
		Port:            constants.DefaultWebUIPort,
		Mode:            "release",
		ReadTimeout:     constants.DefaultReadTimeout,
		WriteTimeout:    constants.DefaultWriteTimeout,
		ShutdownTimeout: constants.DefaultShutdownTimeout,
		MaxRequestSize:  constants.DefaultMaxRequestSize,
		StaticAssetsDir: constants.StaticAssetsDir,
		TemplatesDir:    constants.TemplatesDir,
		EnableSPA:       true,

		WebSocket: WebSocketConfig{
			Enabled:           true,
			PingInterval:      constants.DefaultWSPingInterval,
			PongWait:          constants.DefaultWSPongWait,
			WriteWait:         constants.DefaultWSWriteWait,
			MaxMessageSize:    constants.DefaultWSMaxMessageSize,
			ReadBufferSize:    1024,
			WriteBufferSize:   1024,
			EnableCompression: true,
		},

		Collaboration: CollaborationConfig{
			Enabled:              true,
			RequireReview:        false,
			MinApprovals:         1,
			ReviewTimeout:        constants.DefaultReviewTimeout,
			EnableComments:       true,
			EnableNotifications:  true,
			EnableTeams:          true,
			EnableWorkflows:      true,
			AllowSelfApproval:    false,
		},

		Catalog: CatalogConfig{
			Enabled:          true,
			PageSize:         constants.DefaultCatalogPageSize,
			MaxSearchResults: constants.DefaultSearchLimit,
			EnableFullText:   true,
			EnableTags:       true,
			EnableCategories: true,
			EnableVersioning: true,
			IndexPath:        ".catalog/index",
		},

		Designer: DesignerConfig{
			Enabled:              true,
			MaxTablesPerDiagram:  50,
			EnableAutoLayout:     true,
			EnableValidation:     true,
			EnableTemplates:      true,
			SupportedFormats:     []string{"sql", "xml", "yaml", "json"},
			DesignsDir:           constants.DesignsDir,
			EnableReverseEngineer: true,
		},

		Security: SecurityConfig{
			EnableAuth:        true,
			EnableRBAC:        true,
			EnableAudit:       true,
			SessionTimeout:    constants.DefaultSessionTimeout,
			EnableCSRF:        true,
			EnableRateLimit:   true,
			RateLimitRequests: 100,
			RateLimitWindow:   1 * time.Minute,
			TrustedProxies:    []string{},
			AllowedOrigins:    []string{"*"},
		},

		Cache: CacheConfig{
			Enabled:       true,
			Type:          "memory",
			DefaultTTL:    5 * time.Minute,
			MaxSize:       1000,
			EnableMetrics: true,
		},

		Features: FeatureFlags{
			EnableMigrationCatalog: true,
			EnableDesigner:         true,
			EnableCollaboration:    true,
			EnableRealTimeUpdates:  true,
			EnableDryRun:           true,
			EnableRollback:         true,
			EnableSchemaComparison: true,
			EnableDriftDetection:   true,
			EnableBackup:           true,
		},
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return ErrInvalidPort
	}

	if c.Mode != "debug" && c.Mode != "release" && c.Mode != "test" {
		return ErrInvalidMode
	}

	if c.ReadTimeout < 0 {
		return ErrInvalidReadTimeout
	}

	if c.WriteTimeout < 0 {
		return ErrInvalidWriteTimeout
	}

	if c.Collaboration.Enabled && c.Collaboration.MinApprovals < 0 {
		return ErrInvalidMinApprovals
	}

	if c.Catalog.Enabled && c.Catalog.PageSize < 1 {
		return ErrInvalidPageSize
	}

	if c.Designer.Enabled && c.Designer.MaxTablesPerDiagram < 1 {
		return ErrInvalidMaxTables
	}

	if c.Security.EnableRateLimit && c.Security.RateLimitRequests < 1 {
		return ErrInvalidRateLimit
	}

	return nil
}

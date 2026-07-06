/**
 * @file factory.go
 * @description Facade factory implementation to create and configure all facade components with dependency injection
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/facade/factory.go
 */

package facade

import (
	"context"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
)

// FacadeFactory creates and manages all facade components.
//
// The factory holds the concrete facade implementations it constructs. The
// exported getters expose them via the corresponding facade interfaces so
// callers keep depending on abstractions.
type FacadeFactory struct {
	config              *FactoryConfig
	migrationFacade     *DefaultMigrationFacade
	cliFacade          *DefaultCLIFacade
	commandFacade      *DefaultCommandFacade
	databaseFacade     *DefaultDatabaseFacade
	configurationFacade *DefaultConfigurationFacade
	initialized        bool
}

// FactoryConfig configuration for facade factory
type FactoryConfig struct {
	// Migration configuration
	MigrationConfig *MigrationFacadeConfig `json:"migration_config"`
	
	// CLI configuration
	CLIConfig *CLIConfig `json:"cli_config"`
	
	// Database configuration
	DatabaseConfig *DatabaseConfig `json:"database_config"`
	
	// Configuration facade config
	ConfigurationConfig *ConfigurationConfig `json:"configuration_config"`
	
	// Factory settings
	Environment         string            `json:"environment"`
	LogLevel           string            `json:"log_level"`
	EnableMetrics      bool              `json:"enable_metrics"`
	EnableEvents       bool              `json:"enable_events"`
	EnableCaching      bool              `json:"enable_caching"`
	CacheConfig        *CacheConfig      `json:"cache_config"`
	SecurityConfig     *SecurityConfig   `json:"security_config"`
	PerformanceConfig  *PerformanceConfig `json:"performance_config"`
	
	// Component toggles
	EnableMigrationFacade     bool `json:"enable_migration_facade"`
	EnableCLIFacade          bool `json:"enable_cli_facade"`
	EnableCommandFacade      bool `json:"enable_command_facade"`
	EnableDatabaseFacade     bool `json:"enable_database_facade"`
	EnableConfigurationFacade bool `json:"enable_configuration_facade"`
}

// CacheConfig configuration for caching
type CacheConfig struct {
	Type           string        `json:"type"` // memory, redis, file
	TTL            time.Duration `json:"ttl"`
	MaxSize        int           `json:"max_size"`
	CleanupInterval time.Duration `json:"cleanup_interval"`
	RedisURL       string        `json:"redis_url"`
	FilePath       string        `json:"file_path"`
}

// SecurityConfig configuration for security features
type SecurityConfig struct {
	EnableEncryption    bool   `json:"enable_encryption"`
	EncryptionKey      string `json:"encryption_key"`
	EnableAuditLog     bool   `json:"enable_audit_log"`
	AuditLogPath       string `json:"audit_log_path"`
	EnableRateLimit    bool   `json:"enable_rate_limit"`
	RateLimitRequests  int    `json:"rate_limit_requests"`
	RateLimitWindow    time.Duration `json:"rate_limit_window"`
	EnableAuthentication bool `json:"enable_authentication"`
	AuthenticationMethod string `json:"authentication_method"`
}

// PerformanceConfig configuration for performance optimization
type PerformanceConfig struct {
	MaxConcurrentOperations int           `json:"max_concurrent_operations"`
	OperationTimeout       time.Duration `json:"operation_timeout"`
	ConnectionPoolSize     int           `json:"connection_pool_size"`
	EnableCompression      bool          `json:"enable_compression"`
	EnableProfiling        bool          `json:"enable_profiling"`
	ProfilingPort          int           `json:"profiling_port"`
}

// NewFacadeFactory creates a new facade factory instance
func NewFacadeFactory(config *FactoryConfig) (*FacadeFactory, error) {
	if config == nil {
		config = DefaultFactoryConfig()
	}

	factory := &FacadeFactory{
		config: config,
	}

	return factory, nil
}

// Initialize initializes all facade components
func (f *FacadeFactory) Initialize(ctx context.Context) error {
	if f.initialized {
		return nil
	}

	// Initialize configuration facade first (other components may depend on it)
	if f.config.EnableConfigurationFacade {
		if err := f.initializeConfigurationFacade(ctx); err != nil {
			return fmt.Errorf("failed to initialize configuration facade: %w", err)
		}
	}

	// Initialize database facade
	if f.config.EnableDatabaseFacade {
		if err := f.initializeDatabaseFacade(ctx); err != nil {
			return fmt.Errorf("failed to initialize database facade: %w", err)
		}
	}

	// Initialize command facade
	if f.config.EnableCommandFacade {
		if err := f.initializeCommandFacade(ctx); err != nil {
			return fmt.Errorf("failed to initialize command facade: %w", err)
		}
	}

	// Initialize migration facade
	if f.config.EnableMigrationFacade {
		if err := f.initializeMigrationFacade(ctx); err != nil {
			return fmt.Errorf("failed to initialize migration facade: %w", err)
		}
	}

	// Initialize CLI facade
	if f.config.EnableCLIFacade {
		if err := f.initializeCLIFacade(ctx); err != nil {
			return fmt.Errorf("failed to initialize CLI facade: %w", err)
		}
	}

	f.initialized = true
	return nil
}

// GetMigrationFacade returns the migration facade instance
func (f *FacadeFactory) GetMigrationFacade() (*DefaultMigrationFacade, error) {
	if !f.initialized {
		return nil, fmt.Errorf("factory not initialized")
	}
	
	if f.migrationFacade == nil {
		return nil, fmt.Errorf("migration facade not enabled")
	}
	
	return f.migrationFacade, nil
}

// GetCLIFacade returns the CLI facade instance
func (f *FacadeFactory) GetCLIFacade() (*DefaultCLIFacade, error) {
	if !f.initialized {
		return nil, fmt.Errorf("factory not initialized")
	}
	
	if f.cliFacade == nil {
		return nil, fmt.Errorf("CLI facade not enabled")
	}
	
	return f.cliFacade, nil
}

// GetCommandFacade returns the command facade instance
func (f *FacadeFactory) GetCommandFacade() (*DefaultCommandFacade, error) {
	if !f.initialized {
		return nil, fmt.Errorf("factory not initialized")
	}
	
	if f.commandFacade == nil {
		return nil, fmt.Errorf("command facade not enabled")
	}
	
	return f.commandFacade, nil
}

// GetDatabaseFacade returns the database facade instance
func (f *FacadeFactory) GetDatabaseFacade() (*DefaultDatabaseFacade, error) {
	if !f.initialized {
		return nil, fmt.Errorf("factory not initialized")
	}
	
	if f.databaseFacade == nil {
		return nil, fmt.Errorf("database facade not enabled")
	}
	
	return f.databaseFacade, nil
}

// GetConfigurationFacade returns the configuration facade instance
func (f *FacadeFactory) GetConfigurationFacade() (*DefaultConfigurationFacade, error) {
	if !f.initialized {
		return nil, fmt.Errorf("factory not initialized")
	}
	
	if f.configurationFacade == nil {
		return nil, fmt.Errorf("configuration facade not enabled")
	}
	
	return f.configurationFacade, nil
}

// CreateMigrationSystem creates a complete migration system
func (f *FacadeFactory) CreateMigrationSystem(ctx context.Context) (*MigrationSystem, error) {
	if err := f.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize factory: %w", err)
	}

	system := &MigrationSystem{
		MigrationFacade:     f.migrationFacade,
		CLIFacade:          f.cliFacade,
		CommandFacade:      f.commandFacade,
		DatabaseFacade:     f.databaseFacade,
		ConfigurationFacade: f.configurationFacade,
		Factory:            f,
		logger:             logging.GetComponentLogger("migration-system"),
	}

	return system, nil
}

// Shutdown gracefully shuts down all facade components
func (f *FacadeFactory) Shutdown(ctx context.Context) error {
	if !f.initialized {
		return nil
	}

	var errors []error

	// Shutdown database facade
	if f.databaseFacade != nil {
		if err := f.databaseFacade.Disconnect(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to shutdown database facade: %w", err))
		}
	}

	// Additional cleanup for other facades...

	f.initialized = false

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	return nil
}

// Private initialization methods

func (f *FacadeFactory) initializeConfigurationFacade(ctx context.Context) error {
	facade, err := NewDefaultConfigurationFacade(f.config.ConfigurationConfig)
	if err != nil {
		return err
	}

	// Load configuration
	if err := facade.LoadConfiguration(ctx); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	f.configurationFacade = facade
	return nil
}

func (f *FacadeFactory) initializeDatabaseFacade(ctx context.Context) error {
	// Use configuration from config facade if available
	dbConfig := f.config.DatabaseConfig
	if f.configurationFacade != nil {
		// Override with values from configuration facade
		if host, err := f.configurationFacade.GetString("database.host"); err == nil {
			dbConfig.Host = host
		}
		if port, err := f.configurationFacade.GetInt("database.port"); err == nil {
			dbConfig.Port = port
		}
		if database, err := f.configurationFacade.GetString("database.name"); err == nil {
			dbConfig.Database = database
		}
		if username, err := f.configurationFacade.GetString("database.username"); err == nil {
			dbConfig.Username = username
		}
		if password, err := f.configurationFacade.GetString("database.password"); err == nil {
			dbConfig.Password = password
		}
	}

	facade, err := NewDatabaseFacade(dbConfig)
	if err != nil {
		return err
	}

	// Test connection
	if err := facade.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	f.databaseFacade = facade
	return nil
}

func (f *FacadeFactory) initializeCommandFacade(ctx context.Context) error {
	facade := NewDefaultCommandFacade()
	
	// Register default commands
	if err := f.registerDefaultCommands(facade); err != nil {
		return fmt.Errorf("failed to register default commands: %w", err)
	}

	f.commandFacade = facade
	return nil
}

func (f *FacadeFactory) initializeMigrationFacade(ctx context.Context) error {
	// Ensure database facade is available
	if f.databaseFacade == nil {
		return fmt.Errorf("database facade is required for migration facade")
	}

	// Use configuration from config facade if available
	migrationConfig := f.config.MigrationConfig
	if f.configurationFacade != nil {
		// Override with values from configuration facade
		if migrationsPath, err := f.configurationFacade.GetString("migrations.path"); err == nil {
			migrationConfig.MigrationsPath = migrationsPath
		}
		if environment, err := f.configurationFacade.GetString("migrations.environment"); err == nil {
			migrationConfig.Environment = environment
		}
	}

	facade, err := NewDefaultMigrationFacade(migrationConfig)
	if err != nil {
		return err
	}

	f.migrationFacade = facade
	return nil
}

func (f *FacadeFactory) initializeCLIFacade(ctx context.Context) error {
	// Ensure migration facade is available
	if f.migrationFacade == nil {
		return fmt.Errorf("migration facade is required for CLI facade")
	}

	facade := NewDefaultCLIFacade(f.migrationFacade, f.config.CLIConfig)

	f.cliFacade = facade
	return nil
}

func (f *FacadeFactory) registerDefaultCommands(commandFacade *DefaultCommandFacade) error {
	// Register migration commands
	commands := []CommandExecutor{
		&UpCommand{},
		&DownCommand{},
		&StatusCommand{},
		&CreateCommand{},
		&ListCommand{},
		&ValidateCommand{},
		&HistoryCommand{},
		&RollbackCommand{},
		&ResetCommand{},
		&RefreshCommand{},
		&SeedCommand{},
		&VersionCommand{},
		&HelpCommand{},
		&ConfigCommand{},
	}

	for _, command := range commands {
		if err := commandFacade.RegisterCommand(command); err != nil {
			return fmt.Errorf("failed to register command %T: %w", command, err)
		}
	}

	return nil
}

// Configuration factory methods

// DefaultFactoryConfig returns default factory configuration
func DefaultFactoryConfig() *FactoryConfig {
	return &FactoryConfig{
		MigrationConfig:     DefaultMigrationFacadeConfig(),
		CLIConfig:          DefaultCLIConfig(),
		DatabaseConfig:     DefaultDatabaseConfig(),
		ConfigurationConfig: DefaultConfigurationConfig(),
		Environment:        "development",
		LogLevel:          "info",
		EnableMetrics:     true,
		EnableEvents:      true,
		EnableCaching:     true,
		CacheConfig:       DefaultCacheConfig(),
		SecurityConfig:    DefaultSecurityConfig(),
		PerformanceConfig: DefaultPerformanceConfig(),
		EnableMigrationFacade:     true,
		EnableCLIFacade:          true,
		EnableCommandFacade:      true,
		EnableDatabaseFacade:     true,
		EnableConfigurationFacade: true,
	}
}

// ProductionFactoryConfig returns production-optimized configuration
func ProductionFactoryConfig() *FactoryConfig {
	config := DefaultFactoryConfig()
	config.Environment = "production"
	config.LogLevel = "warn"
	config.SecurityConfig.EnableEncryption = true
	config.SecurityConfig.EnableAuditLog = true
	config.SecurityConfig.EnableRateLimit = true
	config.PerformanceConfig.MaxConcurrentOperations = 10
	config.PerformanceConfig.EnableCompression = true
	return config
}

// DevelopmentFactoryConfig returns development-optimized configuration
func DevelopmentFactoryConfig() *FactoryConfig {
	config := DefaultFactoryConfig()
	config.Environment = "development"
	config.LogLevel = "debug"
	config.PerformanceConfig.EnableProfiling = true
	config.SecurityConfig.EnableEncryption = false
	config.SecurityConfig.EnableRateLimit = false
	return config
}

// TestingFactoryConfig returns testing-optimized configuration
func TestingFactoryConfig() *FactoryConfig {
	config := DefaultFactoryConfig()
	config.Environment = "testing"
	config.LogLevel = "debug"
	config.EnableMetrics = false
	config.EnableEvents = false
	config.EnableCaching = false
	config.SecurityConfig.EnableEncryption = false
	config.SecurityConfig.EnableAuditLog = false
	config.SecurityConfig.EnableRateLimit = false
	return config
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		Type:            "memory",
		TTL:             30 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 5 * time.Minute,
	}
}

// DefaultSecurityConfig returns default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		EnableEncryption:     false,
		EnableAuditLog:      true,
		AuditLogPath:        "logs/audit.log",
		EnableRateLimit:     false,
		RateLimitRequests:   100,
		RateLimitWindow:     time.Minute,
		EnableAuthentication: false,
		AuthenticationMethod: "none",
	}
}

// DefaultPerformanceConfig returns default performance configuration
func DefaultPerformanceConfig() *PerformanceConfig {
	return &PerformanceConfig{
		MaxConcurrentOperations: 5,
		OperationTimeout:       30 * time.Second,
		ConnectionPoolSize:     10,
		EnableCompression:      false,
		EnableProfiling:        false,
		ProfilingPort:          6060,
	}
}

// MigrationSystem represents a complete migration system
type MigrationSystem struct {
	MigrationFacade     *DefaultMigrationFacade
	CLIFacade          *DefaultCLIFacade
	CommandFacade      *DefaultCommandFacade
	DatabaseFacade     *DefaultDatabaseFacade
	ConfigurationFacade *DefaultConfigurationFacade
	Factory            *FacadeFactory
	logger             logging.ComponentLogger
}

// Run runs the migration system with CLI interface
func (s *MigrationSystem) Run(ctx context.Context, args []string) error {
	if s.CLIFacade == nil {
		return fmt.Errorf("CLI facade not available")
	}

	command, err := s.CLIFacade.ParseCommand(args)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}

	result, err := s.CLIFacade.ExecuteCommand(ctx, command)
	if err != nil {
		return err
	}

	// Handle result output
	if result.Output != "" {
		s.logger.Info("Command execution output", "output", result.Output, "success", result.Success)
	}

	if !result.Success {
		return fmt.Errorf("command execution failed")
	}

	return nil
}

// GetStatus returns system status
func (s *MigrationSystem) GetStatus(ctx context.Context) (*SystemStatus, error) {
	status := &SystemStatus{
		Initialized: s.Factory.initialized,
		Components:  make(map[string]bool),
	}

	status.Components["migration_facade"] = s.MigrationFacade != nil
	status.Components["cli_facade"] = s.CLIFacade != nil
	status.Components["command_facade"] = s.CommandFacade != nil
	status.Components["database_facade"] = s.DatabaseFacade != nil
	status.Components["configuration_facade"] = s.ConfigurationFacade != nil

	// Check database connection
	if s.DatabaseFacade != nil {
		status.DatabaseConnected = s.DatabaseFacade.IsConnected(ctx)
	}

	return status, nil
}

// SystemStatus represents the status of the migration system
type SystemStatus struct {
	Initialized       bool            `json:"initialized"`
	DatabaseConnected bool            `json:"database_connected"`
	Components        map[string]bool `json:"components"`
}

// NewDefaultCommandFacade constructs a command facade with an empty registry.
func NewDefaultCommandFacade() *DefaultCommandFacade {
	return &DefaultCommandFacade{
		commands: make(map[string]CommandExecutor),
	}
}

type DefaultCommandFacade struct {
	commands map[string]CommandExecutor
}

func (f *DefaultCommandFacade) RegisterCommand(executor CommandExecutor) error {
	info := executor.GetInfo()
	f.commands[info.Name] = executor
	return nil
}

func (f *DefaultCommandFacade) ExecuteCommand(ctx context.Context, name string, args []string, options map[string]interface{}) (*CommandResult, error) {
	if executor, exists := f.commands[name]; exists {
		return executor.Execute(ctx, args, options)
	}
	return nil, fmt.Errorf("command not found: %s", name)
}

func (f *DefaultCommandFacade) GetCommands() []CommandInfo {
	commands := make([]CommandInfo, 0, len(f.commands))
	for _, executor := range f.commands {
		commands = append(commands, executor.GetInfo())
	}
	return commands
}

func (f *DefaultCommandFacade) GetCommand(name string) (CommandExecutor, error) {
	if executor, exists := f.commands[name]; exists {
		return executor, nil
	}
	return nil, fmt.Errorf("command not found: %s", name)
}

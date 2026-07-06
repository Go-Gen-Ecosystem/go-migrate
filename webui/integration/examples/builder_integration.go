package examples

import (
	"context"
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// BuilderAPIIntegrationExample demonstrates integrating WebUI with Builder API
func BuilderAPIIntegrationExample() error {
	ctx := context.Background()
	logger := logging.NewSimpleLogger()

	// 1. Initialize WebUI
	webuiConfig := config.DefaultConfig()
	webuiConfig.Port = 8080

	ui, err := webui.NewWebUI(webuiConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize WebUI: %w", err)
	}

	// 2. Create engine factory
	engineFactory := ui.CreateEngineFactory()

	// 3. Migration configuration
	migrationConfig := types.Config{
		Database: types.DatabaseConfig{
			Driver: "mysql",
			Host:   "localhost",
			Port:   3306,
			Database: "myapp",
			Username: "root",
			Password: "password",
		},
		Migration: types.MigrationConfig{
			MigrationsPath: "./migrations",
			TableName:      "schema_migrations",
		},
	}

	// 4. Create instrumented engine
	engine, err := engineFactory.CreateEngine(migrationConfig, false, true)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	defer engine.Close()

	// 5. Register handlers
	ui.RegisterHandlers(engine)

	// 6. Start WebUI server
	go func() {
		if err := ui.Start(ctx); err != nil {
			logger.Error(ctx, "WebUI server error", err)
		}
	}()

	// 7. Now use the engine in your application
	// All operations will broadcast to WebUI
	logger.Info(ctx, "WebUI is running at http://localhost:8080")
	logger.Info(ctx, "Builder API operations will broadcast to WebUI")

	// Example: Check status
	status, err := engine.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	logger.Info(ctx, "Migration status",
		"applied", status.Applied,
		"pending", status.Pending)

	// Example: Run migrations programmatically
	if status.Pending > 0 {
		logger.Info(ctx, "Running pending migrations")
		if err := engine.Migrate(ctx, "latest", false); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		logger.Info(ctx, "Migrations completed")
	}

	// Keep server running
	select {}
}

// BuilderAPIWithExistingEngineExample shows wrapping an existing engine
func BuilderAPIWithExistingEngineExample(existingEngine types.MigrationTool) error {
	ctx := context.Background()
	logger := logging.NewSimpleLogger()

	// 1. Initialize WebUI
	webuiConfig := config.DefaultConfig()
	ui, err := webui.NewWebUI(webuiConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize WebUI: %w", err)
	}

	// 2. Create factory
	engineFactory := ui.CreateEngineFactory()

	// 3. Wrap existing engine with instrumentation
	instrumentedEngine := engineFactory.CreateEngineFromExisting(existingEngine, true)

	// 4. Register handlers
	ui.RegisterHandlers(instrumentedEngine)

	// 5. Start WebUI
	go func() {
		if err := ui.Start(ctx); err != nil {
			logger.Error(ctx, "WebUI server error", err)
		}
	}()

	// 6. Use the instrumented engine
	logger.Info(ctx, "Existing engine now has WebUI broadcasting")

	// Now all operations on instrumentedEngine will broadcast to WebUI
	if err := instrumentedEngine.Migrate(ctx, "latest", false); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// ApplicationIntegrationExample demonstrates full application integration
func ApplicationIntegrationExample() error {
	ctx := context.Background()

	// Initialize application logger
	logger, err := logging.NewZapLogger("info", true)
	if err != nil {
		logger = logging.NewSimpleLogger()
	}

	logger.Info(ctx, "Starting application with migration WebUI")

	// 1. Setup WebUI
	webuiConfig := config.DefaultConfig()
	webuiConfig.Host = "0.0.0.0"
	webuiConfig.Port = 8080
	webuiConfig.Security.EnableAuth = false // Disable for demo
	webuiConfig.Features.EnableRealTimeUpdates = true

	ui, err := webui.NewWebUI(webuiConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize WebUI: %w", err)
	}

	// 2. Setup migration engine
	engineFactory := ui.CreateEngineFactory()

	migrationConfig := types.Config{
		Environment: "production",
		Database: types.DatabaseConfig{
			Driver:   "postgres",
			Host:     "db.example.com",
			Port:     5432,
			Database: "production_db",
			Username: "app_user",
			Password: "secure_password",
			SSLMode:  "require",
		},
		Migration: types.MigrationConfig{
			MigrationsPath:    "./migrations",
			TableName:         "schema_migrations",
			EnableRollback:    true,
			EnableBackup:      true,
			ValidateOnMigrate: true,
		},
		Features: types.FeatureConfig{
			AutoBackup:   true,
			HealthChecks: true,
			Monitoring:   true,
		},
	}

	// 3. Create engine with full instrumentation
	engine, err := engineFactory.CreateEngine(migrationConfig, false, true)
	if err != nil {
		return fmt.Errorf("failed to create migration engine: %w", err)
	}
	defer engine.Close()

	// 4. Register all handlers
	ui.RegisterHandlers(engine)

	// 5. Run initial migrations on startup
	logger.Info(ctx, "Running startup migrations")
	if err := engine.Migrate(ctx, "latest", false); err != nil {
		logger.Error(ctx, "Startup migrations failed", err)
		// Decide whether to continue or fail
	}

	// 6. Start WebUI server
	logger.Info(ctx, "Starting WebUI server", "url", fmt.Sprintf("http://%s:%d", webuiConfig.Host, webuiConfig.Port))
	go func() {
		if err := ui.Start(ctx); err != nil {
			logger.Error(ctx, "WebUI server error", err)
		}
	}()

	// 7. Application continues running
	logger.Info(ctx, "Application started successfully")
	logger.Info(ctx, "Access migration dashboard", "url", fmt.Sprintf("http://localhost:%d/static/dashboard.html", webuiConfig.Port))

	// Application logic here...
	// The engine is available for migrations at any time
	// All migration operations will broadcast to connected WebUI clients

	// Keep application running
	select {}
}
package examples

import (
	"context"
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/integration"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// CLIIntegrationExample demonstrates how to integrate WebUI with CLI commands
func CLIIntegrationExample() error {
	ctx := context.Background()

	// 1. Initialize logger
	logger, err := logging.NewZapLogger("info", true)
	if err != nil {
		logger = logging.NewSimpleLogger()
	}

	// 2. Create WebUI configuration
	webuiConfig := config.DefaultConfig()
	webuiConfig.Port = 8080
	webuiConfig.Features.EnableRealTimeUpdates = true

	// 3. Initialize WebUI
	ui, err := webui.NewWebUI(webuiConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize WebUI: %w", err)
	}

	// 4. Create engine factory with broadcasting
	engineFactory := ui.CreateEngineFactory()

	// 5. Create migration configuration
	migrationConfig := types.Config{
		Database: types.DatabaseConfig{
			Driver: "postgres",
			DSN:    "postgres://user:password@localhost:5432/dbname?sslmode=disable",
		},
		Migration: types.MigrationConfig{
			MigrationsPath: "./migrations",
			TableName:      "schema_migrations",
		},
	}

	// 6. Create instrumented engine (with WebUI broadcasting)
	engine, err := engineFactory.CreateEngine(migrationConfig, false, true)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	defer engine.Close()

	// 7. Register handlers with the engine
	ui.RegisterHandlers(engine)

	// 8. Start WebUI server in background
	go func() {
		if err := ui.Start(ctx); err != nil {
			logger.Error(ctx, "WebUI server error", err)
		}
	}()

	// 9. Now CLI commands will broadcast to WebUI
	// Example: Run migration
	logger.Info(ctx, "Running migrations with WebUI broadcasting enabled")
	if err := engine.Migrate(ctx, "latest", false); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// 10. Get status (will also broadcast)
	status, err := engine.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	logger.Info(ctx, "Migration status",
		"applied", status.Applied,
		"pending", status.Pending,
		"failed", status.Failed)

	return nil
}

// CLIWithoutWebUIExample demonstrates running CLI commands without WebUI
func CLIWithoutWebUIExample() error {
	ctx := context.Background()
	logger := logging.NewSimpleLogger()

	// Create factory without broadcaster
	engineFactory := integration.NewEngineFactory(nil, logger)

	migrationConfig := types.Config{
		Database: types.DatabaseConfig{
			Driver: "postgres",
			DSN:    "postgres://user:password@localhost:5432/dbname?sslmode=disable",
		},
		Migration: types.MigrationConfig{
			MigrationsPath: "./migrations",
			TableName:      "schema_migrations",
		},
	}

	// Create engine without WebUI (enableWebUI = false)
	engine, err := engineFactory.CreateEngine(migrationConfig, false, false)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	defer engine.Close()

	// Run migration without broadcasting
	logger.Info(ctx, "Running migrations without WebUI")
	if err := engine.Migrate(ctx, "latest", false); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// CLIWithCustomBroadcasterExample demonstrates using a custom broadcaster
func CLIWithCustomBroadcasterExample() error {
	ctx := context.Background()
	logger := logging.NewSimpleLogger()

	// Create custom broadcaster (e.g., for logging to file, Slack, etc.)
	customBroadcaster := &CustomBroadcaster{logger: logger}

	// Create factory with custom broadcaster
	engineFactory := integration.NewEngineFactory(customBroadcaster, logger)

	migrationConfig := types.Config{
		Database: types.DatabaseConfig{
			Driver: "postgres",
			DSN:    "postgres://user:password@localhost:5432/dbname?sslmode=disable",
		},
		Migration: types.MigrationConfig{
			MigrationsPath: "./migrations",
		},
	}

	// Create engine with custom broadcasting
	engine, err := engineFactory.CreateEngine(migrationConfig, false, true)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}
	defer engine.Close()

	// Run migration with custom broadcasting
	if err := engine.Migrate(ctx, "latest", false); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// CustomBroadcaster is an example custom broadcaster
type CustomBroadcaster struct {
	logger logging.Logger
}

func (c *CustomBroadcaster) BroadcastEvent(event integration.MigrationEvent) {
	c.logger.Info(context.Background(), "Custom broadcast",
		"type", event.Type,
		"message", event.Message)
}

func (c *CustomBroadcaster) BroadcastMigrationStatus(status *types.MigrationStatus) {
	c.logger.Info(context.Background(), "Status broadcast",
		"applied", status.Applied,
		"pending", status.Pending)
}

func (c *CustomBroadcaster) BroadcastMigrationLog(message string) {
	c.logger.Info(context.Background(), "Log broadcast", "message", message)
}

func (c *CustomBroadcaster) Close() {
	c.logger.Info(context.Background(), "Custom broadcaster closed")
}
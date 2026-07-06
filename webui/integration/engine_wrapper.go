package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// InstrumentedEngine wraps a MigrationEngine and broadcasts events
type InstrumentedEngine struct {
	engine      types.MigrationTool
	broadcaster Broadcaster
	logger      logging.Logger
}

// NewInstrumentedEngine creates a new instrumented engine
func NewInstrumentedEngine(engine types.MigrationTool, broadcaster Broadcaster, logger logging.Logger) *InstrumentedEngine {
	if broadcaster == nil {
		broadcaster = &NoOpBroadcaster{}
	}

	return &InstrumentedEngine{
		engine:      engine,
		broadcaster: broadcaster,
		logger:      logger,
	}
}

// Status returns the current migration status
func (i *InstrumentedEngine) Status(ctx context.Context) (*types.MigrationStatus, error) {
	i.logger.Info(ctx, "Getting migration status")

	status, err := i.engine.Status(ctx)
	if err != nil {
		i.broadcaster.BroadcastEvent(MigrationEvent{
			Type:      EventMigrationFailed,
			Message:   fmt.Sprintf("Failed to get status: %v", err),
			Timestamp: time.Now(),
		})
		return nil, err
	}

	// Broadcast status
	i.broadcaster.BroadcastMigrationStatus(status)

	return status, nil
}

// Migrate runs pending migrations
func (i *InstrumentedEngine) Migrate(ctx context.Context, target string, dryRun bool) error {
	i.logger.Info(ctx, "Starting migration",
		"target", target,
		"dryRun", dryRun)

	// Broadcast start event
	i.broadcaster.BroadcastEvent(MigrationEvent{
		Type:      EventMigrationStarted,
		Message:   "Migration started",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"target": target,
			"dryRun": dryRun,
		},
	})

	// Execute migration
	err := i.engine.Migrate(ctx, target, dryRun)

	if err != nil {
		i.broadcaster.BroadcastEvent(MigrationEvent{
			Type:      EventMigrationFailed,
			Message:   fmt.Sprintf("Migration failed: %v", err),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"error": err.Error(),
			},
		})
		return err
	}

	// Broadcast completion
	i.broadcaster.BroadcastEvent(MigrationEvent{
		Type:      EventMigrationCompleted,
		Message:   "Migration completed successfully",
		Timestamp: time.Now(),
	})

	// Get and broadcast updated status
	status, err := i.engine.Status(ctx)
	if err == nil {
		i.broadcaster.BroadcastMigrationStatus(status)
	}

	return nil
}

// Rollback rolls back migrations
func (i *InstrumentedEngine) Rollback(ctx context.Context, steps int) error {
	i.logger.Info(ctx, "Starting rollback", "steps", steps)

	// Broadcast start event
	i.broadcaster.BroadcastEvent(MigrationEvent{
		Type:      EventMigrationStarted,
		Message:   fmt.Sprintf("Rolling back %d migrations", steps),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"steps": steps,
			"type":  "rollback",
		},
	})

	// Execute rollback
	err := i.engine.Rollback(ctx, steps)

	if err != nil {
		i.broadcaster.BroadcastEvent(MigrationEvent{
			Type:      EventMigrationFailed,
			Message:   fmt.Sprintf("Rollback failed: %v", err),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"error": err.Error(),
			},
		})
		return err
	}

	// Broadcast completion
	i.broadcaster.BroadcastEvent(MigrationEvent{
		Type:      EventMigrationCompleted,
		Message:   "Rollback completed successfully",
		Timestamp: time.Now(),
	})

	// Get and broadcast updated status
	status, err := i.engine.Status(ctx)
	if err == nil {
		i.broadcaster.BroadcastMigrationStatus(status)
	}

	return nil
}

// Validate validates all migrations
func (i *InstrumentedEngine) Validate(ctx context.Context) error {
	i.logger.Info(ctx, "Starting validation")

	// Broadcast start event
	i.broadcaster.BroadcastEvent(MigrationEvent{
		Type:      EventValidationStarted,
		Message:   "Validation started",
		Timestamp: time.Now(),
	})

	// Execute validation
	err := i.engine.Validate(ctx)

	if err != nil {
		i.broadcaster.BroadcastEvent(MigrationEvent{
			Type:      EventValidationResult,
			Message:   fmt.Sprintf("Validation failed: %v", err),
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"valid": false,
				"error": err.Error(),
			},
		})
		return err
	}

	// Broadcast success
	i.broadcaster.BroadcastEvent(MigrationEvent{
		Type:      EventValidationResult,
		Message:   "Validation passed",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"valid": true,
		},
	})

	return nil
}

// Info returns migration information
func (i *InstrumentedEngine) Info(ctx context.Context) (*types.MigrationInfo, error) {
	return i.engine.Info(ctx)
}

// Close closes the migration engine
func (i *InstrumentedEngine) Close() error {
	i.broadcaster.Close()
	return i.engine.Close()
}

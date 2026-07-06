package integration

import (
	"fmt"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// EngineFactory creates migration engines with optional WebUI integration
type EngineFactory struct {
	broadcaster Broadcaster
	logger      logging.Logger
}

// NewEngineFactory creates a new engine factory
func NewEngineFactory(broadcaster Broadcaster, logger logging.Logger) *EngineFactory {
	return &EngineFactory{
		broadcaster: broadcaster,
		logger:      logger,
	}
}

// CreateEngine creates a migration engine with optional instrumentation
func (f *EngineFactory) CreateEngine(config types.Config, dryRun bool, enableWebUI bool) (types.MigrationTool, error) {
	// Create base engine
	baseEngine, err := engine.NewMigrationEngineWithFactory(config, dryRun)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration engine: %w", err)
	}

	// If WebUI is enabled, wrap with instrumented engine
	if enableWebUI && f.broadcaster != nil {
		f.logger.Info(nil, "Creating instrumented engine with WebUI broadcasting")
		return NewInstrumentedEngine(baseEngine, f.broadcaster, f.logger), nil
	}

	return baseEngine, nil
}

// CreateEngineFromExisting wraps an existing engine with instrumentation
func (f *EngineFactory) CreateEngineFromExisting(engine types.MigrationTool, enableWebUI bool) types.MigrationTool {
	if enableWebUI && f.broadcaster != nil {
		f.logger.Info(nil, "Wrapping existing engine with WebUI broadcasting")
		return NewInstrumentedEngine(engine, f.broadcaster, f.logger)
	}
	return engine
}

// SetBroadcaster sets the broadcaster for the factory
func (f *EngineFactory) SetBroadcaster(broadcaster Broadcaster) {
	f.broadcaster = broadcaster
}
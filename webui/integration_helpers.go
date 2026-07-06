package webui

import (
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/integration"
)

// CreateBroadcaster creates a broadcaster for the WebUI
func (w *WebUI) CreateBroadcaster() integration.Broadcaster {
	return integration.NewWebSocketBroadcaster(w.hub, w.logger)
}

// CreateEngineFactory creates an engine factory with WebUI broadcasting
func (w *WebUI) CreateEngineFactory() *integration.EngineFactory {
	broadcaster := w.CreateBroadcaster()
	return integration.NewEngineFactory(broadcaster, w.logger)
}
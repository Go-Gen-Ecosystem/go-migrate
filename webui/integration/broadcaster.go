package integration

import (
	"context"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// EventType represents the type of migration event
type EventType string

const (
	EventMigrationStarted   EventType = "migration_started"
	EventMigrationProgress  EventType = "migration_progress"
	EventMigrationCompleted EventType = "migration_completed"
	EventMigrationFailed    EventType = "migration_failed"
	EventMigrationLog       EventType = "migration_log"
	EventStatusChanged      EventType = "status_changed"
	EventValidationStarted  EventType = "validation_started"
	EventValidationResult   EventType = "validation_result"
)

// MigrationEvent represents a migration event
type MigrationEvent struct {
	Type        EventType              `json:"type"`
	MigrationID string                 `json:"migrationId,omitempty"`
	Message     string                 `json:"message"`
	Data        map[string]interface{} `json:"data,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
	Progress    *ProgressInfo          `json:"progress,omitempty"`
}

// ProgressInfo represents migration progress
type ProgressInfo struct {
	Current int `json:"current"`
	Total   int `json:"total"`
	Percent int `json:"percent"`
}

// Broadcaster interface for broadcasting migration events
type Broadcaster interface {
	BroadcastEvent(event MigrationEvent)
	BroadcastMigrationStatus(status *types.MigrationStatus)
	BroadcastMigrationLog(message string)
	Close()
}

// NoOpBroadcaster is a no-op implementation of Broadcaster
type NoOpBroadcaster struct{}

func (n *NoOpBroadcaster) BroadcastEvent(event MigrationEvent)                    {}
func (n *NoOpBroadcaster) BroadcastMigrationStatus(status *types.MigrationStatus) {}
func (n *NoOpBroadcaster) BroadcastMigrationLog(message string)                   {}
func (n *NoOpBroadcaster) Close()                                                  {}

// WebSocketBroadcaster broadcasts events via WebSocket
type WebSocketBroadcaster struct {
	hub    WebSocketHub
	logger logging.Logger
}

// WebSocketHub interface (to avoid circular dependency)
type WebSocketHub interface {
	Broadcast(channel string, msgType string, data interface{})
}

// NewWebSocketBroadcaster creates a new WebSocket broadcaster
func NewWebSocketBroadcaster(hub WebSocketHub, logger logging.Logger) *WebSocketBroadcaster {
	return &WebSocketBroadcaster{
		hub:    hub,
		logger: logger,
	}
}

// BroadcastEvent broadcasts a migration event
func (w *WebSocketBroadcaster) BroadcastEvent(event MigrationEvent) {
	if w.hub == nil {
		return
	}

	w.hub.Broadcast("migration:status", string(event.Type), event)
	w.logger.Debug(context.Background(), "Broadcasted migration event",
		"type", event.Type,
		"migrationId", event.MigrationID)
}

// BroadcastMigrationStatus broadcasts migration status
func (w *WebSocketBroadcaster) BroadcastMigrationStatus(status *types.MigrationStatus) {
	if w.hub == nil || status == nil {
		return
	}

	w.hub.Broadcast("migration:status", "status_update", map[string]interface{}{
		"version":   status.SchemaVersion,
		"applied":   status.ExecutedCount,
		"pending":   status.PendingCount,
		"failed":    status.FailedCount,
		"timestamp": time.Now(),
	})

	w.logger.Debug(context.Background(), "Broadcasted migration status",
		"applied", status.ExecutedCount,
		"pending", status.PendingCount,
		"failed", status.FailedCount)
}

// BroadcastMigrationLog broadcasts a log message
func (w *WebSocketBroadcaster) BroadcastMigrationLog(message string) {
	if w.hub == nil {
		return
	}

	w.hub.Broadcast("migration:logs", "log_entry", map[string]interface{}{
		"message":   message,
		"timestamp": time.Now(),
	})
}

// Close closes the broadcaster
func (w *WebSocketBroadcaster) Close() {
	w.logger.Info(context.Background(), "WebSocket broadcaster closed")
}
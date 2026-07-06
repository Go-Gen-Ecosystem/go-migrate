package websocket

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/constants"
)

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients
	broadcast chan *Message

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Channels for broadcasting to specific topics
	channels map[string]map[*Client]bool

	// Mutex for thread-safe operations
	mu sync.RWMutex

	// Logger
	logger logging.Logger

	// Context for shutdown
	ctx context.Context
	cancel context.CancelFunc
}

// Message represents a WebSocket message
type Message struct {
	Channel string      `json:"channel"`
	Type    string      `json:"type"`
	Data    interface{} `json:"data"`
}

// NewHub creates a new WebSocket hub
func NewHub(logger logging.Logger) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *Message, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		channels:   make(map[string]map[*Client]bool),
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Run starts the hub
func (h *Hub) Run() {
	h.logger.Info(context.Background(), "WebSocket hub started")

	for {
		select {
		case <-h.ctx.Done():
			h.logger.Info(context.Background(), "WebSocket hub shutting down")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.logger.Info(context.Background(), "Client registered",
				"clientID", client.id,
				"totalClients", len(h.clients))
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// Remove from all channels
				for channel, clients := range h.channels {
					delete(clients, client)
					if len(clients) == 0 {
						delete(h.channels, channel)
					}
				}

				h.logger.Info(context.Background(), "Client unregistered",
					"clientID", client.id,
					"totalClients", len(h.clients))
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.broadcastMessage(message)
		}
	}
}

// broadcastMessage sends a message to all subscribed clients
func (h *Hub) broadcastMessage(message *Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := json.Marshal(message)
	if err != nil {
		h.logger.Error(context.Background(), "Failed to marshal message", err)
		return
	}

	// If message has a channel, send only to subscribed clients
	if message.Channel != "" {
		if clients, ok := h.channels[message.Channel]; ok {
			for client := range clients {
				select {
				case client.send <- data:
				default:
					// Client's send channel is full, unregister it
					go func(c *Client) {
						h.unregister <- c
					}(client)
				}
			}
		}
		return
	}

	// Broadcast to all clients
	for client := range h.clients {
		select {
		case client.send <- data:
		default:
			go func(c *Client) {
				h.unregister <- c
			}(client)
		}
	}
}

// Subscribe subscribes a client to a channel
func (h *Hub) Subscribe(client *Client, channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.channels[channel] == nil {
		h.channels[channel] = make(map[*Client]bool)
	}
	h.channels[channel][client] = true

	h.logger.Info(context.Background(), "Client subscribed to channel",
		"clientID", client.id,
		"channel", channel,
		"subscribers", len(h.channels[channel]))
}

// Unsubscribe unsubscribes a client from a channel
func (h *Hub) Unsubscribe(client *Client, channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.channels[channel]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.channels, channel)
		}

		h.logger.Info(context.Background(), "Client unsubscribed from channel",
			"clientID", client.id,
			"channel", channel)
	}
}

// Broadcast sends a message to all clients or specific channel
func (h *Hub) Broadcast(channel string, msgType string, data interface{}) {
	message := &Message{
		Channel: channel,
		Type:    msgType,
		Data:    data,
	}

	select {
	case h.broadcast <- message:
	default:
		h.logger.Warn(context.Background(), "Broadcast channel full, dropping message", nil)
	}
}

// BroadcastMigrationStatus broadcasts migration status update
func (h *Hub) BroadcastMigrationStatus(data interface{}) {
	h.Broadcast(constants.WSChannelMigrationStatus, "status_update", data)
}

// BroadcastMigrationLog broadcasts migration log entry
func (h *Hub) BroadcastMigrationLog(data interface{}) {
	h.Broadcast(constants.WSChannelMigrationLogs, "log_entry", data)
}

// BroadcastReviewUpdate broadcasts review update
func (h *Hub) BroadcastReviewUpdate(data interface{}) {
	h.Broadcast(constants.WSChannelReviewUpdates, "review_update", data)
}

// BroadcastTeamNotification broadcasts team notification
func (h *Hub) BroadcastTeamNotification(data interface{}) {
	h.Broadcast(constants.WSChannelTeamNotifications, "notification", data)
}

// BroadcastRollbackProgress broadcasts rollback progress update
func (h *Hub) BroadcastRollbackProgress(data interface{}) {
	h.Broadcast(constants.WSChannelRollbackProgress, "rollback_progress", data)
}

// BroadcastRollbackAnalysis broadcasts rollback analysis results
func (h *Hub) BroadcastRollbackAnalysis(data interface{}) {
	h.Broadcast(constants.WSChannelRollbackAnalysis, "rollback_analysis", data)
}

// BroadcastRollbackTest broadcasts rollback test results
func (h *Hub) BroadcastRollbackTest(data interface{}) {
	h.Broadcast(constants.WSChannelRollbackTest, "rollback_test", data)
}

// GetStats returns hub statistics
func (h *Hub) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	channelStats := make(map[string]int)
	for channel, clients := range h.channels {
		channelStats[channel] = len(clients)
	}

	return map[string]interface{}{
		"totalClients":   len(h.clients),
		"totalChannels":  len(h.channels),
		"channelStats":   channelStats,
	}
}

// Shutdown gracefully shuts down the hub
func (h *Hub) Shutdown() {
	h.logger.Info(context.Background(), "Shutting down WebSocket hub")
	h.cancel()

	h.mu.Lock()
	defer h.mu.Unlock()

	// Close all client connections
	for client := range h.clients {
		close(client.send)
	}
}

package websocket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gorilla/websocket"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
)

// Client represents a WebSocket client
type Client struct {
	// Unique client ID
	id string

	// The WebSocket hub
	hub *Hub

	// The WebSocket connection
	conn *websocket.Conn

	// Buffered channel of outbound messages
	send chan []byte

	// Client subscriptions
	subscriptions map[string]bool

	// Logger
	logger logging.Logger

	// Config
	config *config.WebSocketConfig
}

// ClientMessage represents a message from client
type ClientMessage struct {
	Action  string      `json:"action"`
	Channel string      `json:"channel,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// NewClient creates a new WebSocket client
func NewClient(hub *Hub, conn *websocket.Conn, id string, cfg *config.WebSocketConfig, logger logging.Logger) *Client {
	return &Client{
		id:            id,
		hub:           hub,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]bool),
		logger:        logger,
		config:        cfg,
	}
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
		return nil
	})

	if c.config.MaxMessageSize > 0 {
		c.conn.SetReadLimit(c.config.MaxMessageSize)
	}

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error(context.Background(), "WebSocket read error", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(c.config.PingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming messages from client
func (c *Client) handleMessage(message []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		c.logger.Error(context.Background(), "Failed to unmarshal client message", err)
		return
	}

	switch msg.Action {
	case "subscribe":
		if msg.Channel != "" {
			c.subscriptions[msg.Channel] = true
			c.hub.Subscribe(c, msg.Channel)
			c.sendAck("subscribed", msg.Channel)
		}

	case "unsubscribe":
		if msg.Channel != "" {
			delete(c.subscriptions, msg.Channel)
			c.hub.Unsubscribe(c, msg.Channel)
			c.sendAck("unsubscribed", msg.Channel)
		}

	case "ping":
		c.sendAck("pong", nil)

	default:
		c.logger.Warn(context.Background(), "Unknown client action", nil)
	}
}

// sendAck sends an acknowledgment message to the client
func (c *Client) sendAck(action string, data interface{}) {
	ack := map[string]interface{}{
		"type":   "ack",
		"action": action,
		"data":   data,
	}

	message, err := json.Marshal(ack)
	if err != nil {
		c.logger.Error(context.Background(), "Failed to marshal ack", err)
		return
	}

	select {
	case c.send <- message:
	default:
		c.logger.Warn(context.Background(), "Client send buffer full", nil)
	}
}

// Start starts the client's read and write pumps
func (c *Client) Start() {
	go c.writePump()
	go c.readPump()
}

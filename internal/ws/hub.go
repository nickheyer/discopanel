package ws

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/proto"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512 * 1024 // 512KB
)

// Hub manages WebSocket connections and log subscriptions
type Hub struct {
	logStreamer *logger.LogStreamer
	authManager *auth.Manager
	store       *storage.Store
	docker      *docker.Client
	log         *logger.Logger

	upgrader websocket.Upgrader

	// Active clients
	clients   map[*Client]bool
	clientsMu sync.RWMutex

	// Register/unregister channels
	register   chan *Client
	unregister chan *Client
}

// Client represents a single WebSocket connection
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	// Authentication
	user          *storage.User
	authenticated bool

	// Subscriptions: serverId -> log channel
	subscriptions   map[string]chan *v1.LogEntry
	subscriptionsMu sync.RWMutex
}

// NewHub creates a new WebSocket hub
func NewHub(logStreamer *logger.LogStreamer, authManager *auth.Manager, store *storage.Store, docker *docker.Client, log *logger.Logger) *Hub {
	return &Hub{
		logStreamer: logStreamer,
		authManager: authManager,
		store:       store,
		docker:      docker,
		log:         log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins (CORS handled elsewhere)
			},
		},
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
			h.log.Debug("WebSocket client connected")

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.clientsMu.Unlock()
			h.log.Debug("WebSocket client disconnected")
		}
	}
}

// ServeHTTP handles WebSocket upgrade requests
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		subscriptions: make(map[string]chan *v1.LogEntry),
	}

	h.register <- client

	// Start read/write pumps
	go client.writePump()
	go client.readPump()
}

// readPump reads messages from the WebSocket connection
func (c *Client) readPump() {
	defer func() {
		c.cleanup()
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.log.Error("WebSocket read error: %v", err)
			}
			break
		}

		c.handleMessage(message)
	}
}

// writePump writes messages to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *Client) handleMessage(data []byte) {
	msg := &v1.WebSocketClientMessage{}
	if err := proto.Unmarshal(data, msg); err != nil {
		c.hub.log.Error("Failed to unmarshal WebSocket message: %v", err)
		c.sendError("invalid message format")
		return
	}

	switch msg.Type {
	case v1.WSMessageType_WS_MESSAGE_TYPE_AUTH:
		c.handleAuth(msg.GetAuth())
	case v1.WSMessageType_WS_MESSAGE_TYPE_SUBSCRIBE:
		c.handleSubscribe(msg.GetSubscribe())
	case v1.WSMessageType_WS_MESSAGE_TYPE_UNSUBSCRIBE:
		c.handleUnsubscribe(msg.GetUnsubscribe())
	case v1.WSMessageType_WS_MESSAGE_TYPE_COMMAND:
		c.handleCommand(msg.GetCommand())
	case v1.WSMessageType_WS_MESSAGE_TYPE_PING:
		c.sendPong()
	default:
		c.sendError("unknown message type")
	}
}

// handleAuth authenticates the client
func (c *Client) handleAuth(msg *v1.AuthMessage) {
	if msg == nil {
		c.sendAuthFail("missing auth message")
		return
	}

	ctx := context.Background()
	user, err := c.hub.authManager.ValidateSession(ctx, msg.Token)
	if err != nil {
		c.sendAuthFail("invalid token")
		return
	}

	c.user = user
	c.authenticated = true
	c.sendAuthOk()
}

// handleSubscribe subscribes to server logs
func (c *Client) handleSubscribe(msg *v1.SubscribeMessage) {
	if !c.authenticated {
		c.sendError("not authenticated")
		return
	}

	if msg == nil || msg.ServerId == "" {
		c.sendError("missing server_id")
		return
	}

	// Get server to find container ID
	ctx := context.Background()
	server, err := c.hub.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		c.sendError("server not found")
		return
	}

	if server.ContainerID == "" {
		c.sendError("server has no container")
		return
	}

	// Check if already subscribed
	c.subscriptionsMu.Lock()
	if _, exists := c.subscriptions[msg.ServerId]; exists {
		c.subscriptionsMu.Unlock()
		c.sendSubscribed(msg.ServerId)
		return
	}

	// Subscribe to log streamer
	ch := c.hub.logStreamer.Subscribe(server.ContainerID)
	c.subscriptions[msg.ServerId] = ch
	c.subscriptionsMu.Unlock()

	// Send initial logs
	tail := int(msg.Tail)
	if tail <= 0 {
		tail = 500
	}
	logs := c.hub.logStreamer.GetLogs(server.ContainerID, tail)
	c.sendLogs(msg.ServerId, logs)

	// Confirm subscription
	c.sendSubscribed(msg.ServerId)

	// Start forwarding logs for this subscription
	go c.forwardLogs(msg.ServerId, ch)
}

// forwardLogs forwards log entries from the log streamer to the client
func (c *Client) forwardLogs(serverId string, ch chan *v1.LogEntry) {
	for entry := range ch {
		c.sendLog(serverId, entry)
	}
}

// handleUnsubscribe unsubscribes from server logs
func (c *Client) handleUnsubscribe(msg *v1.UnsubscribeMessage) {
	if msg == nil || msg.ServerId == "" {
		c.sendError("missing server_id")
		return
	}

	// Get server to find container ID
	ctx := context.Background()
	server, err := c.hub.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		c.sendError("server not found")
		return
	}

	c.subscriptionsMu.Lock()
	if ch, exists := c.subscriptions[msg.ServerId]; exists {
		delete(c.subscriptions, msg.ServerId)
		c.hub.logStreamer.Unsubscribe(server.ContainerID, ch)
	}
	c.subscriptionsMu.Unlock()

	c.sendUnsubscribed(msg.ServerId)
}

// handleCommand executes a command on the server
func (c *Client) handleCommand(msg *v1.CommandMessage) {
	if !c.authenticated {
		c.sendError("not authenticated")
		return
	}

	if msg == nil || msg.ServerId == "" || msg.Command == "" {
		c.sendError("missing server_id or command")
		return
	}

	ctx := context.Background()
	server, err := c.hub.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		c.sendCommandResult(msg.ServerId, false, "", "server not found")
		return
	}

	if server.ContainerID == "" {
		c.sendCommandResult(msg.ServerId, false, "", "server has no container")
		return
	}

	// Check server status
	status, err := c.hub.docker.GetContainerStatus(ctx, server.ContainerID)
	if err != nil || status != storage.StatusRunning {
		c.sendCommandResult(msg.ServerId, false, "", "server is not running")
		return
	}

	// Add command to log stream
	commandTime := time.Now()
	c.hub.logStreamer.AddCommandEntry(server.ContainerID, msg.Command, commandTime)

	// Execute command
	output, err := c.hub.docker.ExecCommand(ctx, server.ContainerID, msg.Command)
	success := err == nil

	// Add output to log stream
	if output != "" || !success {
		c.hub.logStreamer.AddCommandOutput(server.ContainerID, output, success, commandTime)
	}

	if err != nil {
		c.sendCommandResult(msg.ServerId, false, "", err.Error())
		return
	}

	c.sendCommandResult(msg.ServerId, true, output, "")
}

// cleanup removes all subscriptions when client disconnects
func (c *Client) cleanup() {
	c.subscriptionsMu.Lock()
	defer c.subscriptionsMu.Unlock()

	ctx := context.Background()
	for serverId, ch := range c.subscriptions {
		server, err := c.hub.store.GetServer(ctx, serverId)
		if err == nil && server.ContainerID != "" {
			c.hub.logStreamer.Unsubscribe(server.ContainerID, ch)
		}
	}
	c.subscriptions = make(map[string]chan *v1.LogEntry)
}

// sendMessage marshals and sends a server message
func (c *Client) sendMessage(msg *v1.WebSocketServerMessage) {
	data, err := proto.Marshal(msg)
	if err != nil {
		c.hub.log.Error("Failed to marshal WebSocket message: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		// Channel full, skip
	}
}

func (c *Client) sendAuthOk() {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_AUTH_OK,
		Payload: &v1.WebSocketServerMessage_AuthOk{
			AuthOk: &v1.AuthOkMessage{
				UserId:   c.user.ID,
				Username: c.user.Username,
			},
		},
	})
}

func (c *Client) sendAuthFail(errMsg string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_AUTH_FAIL,
		Payload: &v1.WebSocketServerMessage_AuthFail{
			AuthFail: &v1.AuthFailMessage{
				Error: errMsg,
			},
		},
	})
}

func (c *Client) sendSubscribed(serverId string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_SUBSCRIBED,
		Payload: &v1.WebSocketServerMessage_Subscribed{
			Subscribed: &v1.SubscribedMessage{
				ServerId: serverId,
			},
		},
	})
}

func (c *Client) sendUnsubscribed(serverId string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_UNSUBSCRIBED,
		Payload: &v1.WebSocketServerMessage_Unsubscribed{
			Unsubscribed: &v1.UnsubscribedMessage{
				ServerId: serverId,
			},
		},
	})
}

func (c *Client) sendLogs(serverId string, logs []*v1.LogEntry) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_LOGS,
		Payload: &v1.WebSocketServerMessage_Logs{
			Logs: &v1.LogsMessage{
				ServerId: serverId,
				Logs:     logs,
			},
		},
	})
}

func (c *Client) sendLog(serverId string, log *v1.LogEntry) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_LOG,
		Payload: &v1.WebSocketServerMessage_Log{
			Log: &v1.LogMessage{
				ServerId: serverId,
				Log:      log,
			},
		},
	})
}

func (c *Client) sendCommandResult(serverId string, success bool, output, errMsg string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_COMMAND_RESULT,
		Payload: &v1.WebSocketServerMessage_CommandResult{
			CommandResult: &v1.CommandResultMessage{
				ServerId: serverId,
				Success:  success,
				Output:   output,
				Error:    errMsg,
			},
		},
	})
}

func (c *Client) sendError(errMsg string) {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_ERROR,
		Payload: &v1.WebSocketServerMessage_Error{
			Error: &v1.ErrorMessage{
				Error: errMsg,
			},
		},
	})
}

func (c *Client) sendPong() {
	c.sendMessage(&v1.WebSocketServerMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_PONG,
	})
}

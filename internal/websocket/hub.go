package websocket

import (
	"context"
	"sync"

	"github.com/nickheyer/discopanel/internal/auth"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/proto"
)

// Hub manages WebSocket clients and topic subscriptions.
type Hub struct {
	mu          sync.RWMutex
	clients     map[*Client]bool
	topics      map[string]map[*Client]bool
	authManager *auth.Manager
	log         *logger.Logger

	register   chan *Client
	unregister chan *Client
	stop       chan struct{}
}

// NewHub creates a new WebSocket hub.
func NewHub(authManager *auth.Manager, log *logger.Logger) *Hub {
	return &Hub{
		clients:     make(map[*Client]bool),
		topics:      make(map[string]map[*Client]bool),
		authManager: authManager,
		log:         log,
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		stop:        make(chan struct{}),
	}
}

// Run processes register/unregister events. Call in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.removeClient(client)
		case <-h.stop:
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			return
		}
	}
}

// Stop shuts down the hub.
func (h *Hub) Stop() {
	close(h.stop)
}

// Subscribe adds a client to a topic after validating the token.
func (h *Hub) Subscribe(client *Client, topic string, token string) {
	// Validate auth token
	ctx := context.Background()
	_, err := h.authManager.ValidateSession(ctx, token)
	if err != nil {
		h.sendError(client, topic, "authentication failed")
		return
	}

	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[*Client]bool)
	}
	h.topics[topic][client] = true
	h.mu.Unlock()

	h.log.Debug("WS: client subscribed to %s", topic)

	// Send ACK
	ack := &v1.WsMessage{
		Type:  v1.WsMessageType_WS_MESSAGE_TYPE_ACK,
		Topic: topic,
	}
	data, err := proto.Marshal(ack)
	if err != nil {
		return
	}
	select {
	case client.send <- data:
	default:
	}
}

// Unsubscribe removes a client from a topic.
func (h *Hub) Unsubscribe(client *Client, topic string) {
	h.mu.Lock()
	if subs, ok := h.topics[topic]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(h.topics, topic)
		}
	}
	h.mu.Unlock()
}

// Publish sends a payload to all clients subscribed to a topic.
func (h *Hub) Publish(topic string, payload []byte) {
	msg := &v1.WsMessage{
		Type:    v1.WsMessageType_WS_MESSAGE_TYPE_UPDATE,
		Topic:   topic,
		Payload: payload,
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		h.log.Error("WS: failed to marshal update for topic %s: %v", topic, err)
		return
	}

	h.mu.RLock()
	subs := h.topics[topic]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	// Copy subscriber set under read lock to avoid holding lock during send
	clients := make([]*Client, 0, len(subs))
	for c := range subs {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			// Client send buffer full, drop message
		}
	}
}

// HasSubscribers returns true if the topic has any subscribers.
func (h *Hub) HasSubscribers(topic string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic]) > 0
}

// removeClient removes a client from all topics and the client set.
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; !ok {
		return
	}

	// Remove from all topics
	for topic, subs := range h.topics {
		delete(subs, client)
		if len(subs) == 0 {
			delete(h.topics, topic)
		}
	}

	delete(h.clients, client)
	close(client.send)
}

func (h *Hub) sendError(client *Client, topic string, errMsg string) {
	msg := &v1.WsMessage{
		Type:  v1.WsMessageType_WS_MESSAGE_TYPE_ERROR,
		Topic: topic,
		Error: errMsg,
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case client.send <- data:
	default:
	}
}

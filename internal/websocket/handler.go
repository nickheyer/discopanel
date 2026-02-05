package websocket

import (
	"net/http"

	"github.com/gorilla/websocket"
)

// Handler upgrades HTTP connections to WebSocket.
type Handler struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

// NewHandler creates a new WebSocket handler.
func NewHandler(hub *Hub) *Handler {
	return &Handler{
		hub: hub,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow same-origin connections
				return true
			},
		},
	}
}

// ServeHTTP upgrades the connection and starts client pumps.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.hub.log.Error("WS: upgrade failed: %v", err)
		return
	}

	client := newClient(h.hub, conn)
	h.hub.register <- client

	go client.writePump()
	go client.readPump()
}

package proxy

import (
	"context"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// Proxier is the interface for all proxy types (TCP, UDP, Minecraft, HTTP)
type Proxier interface {
	Start() error
	Stop() error
	AddRoute(serverID, hostname, backendHost string, backendPort int)
	RemoveRoute(hostname string)
	UpdateRoute(hostname, backendHost string, backendPort int)
	GetRoutes() map[string]*Route
	IsRunning() bool
}

// Route represents a routing rule from hostname to backend server
type Route struct {
	ServerID    string
	Hostname    string
	BackendHost string
	BackendPort int
	Active      bool
}

// SleepingServer carries the data needed to answer a status ping for a
// paused (autopaused) server without waking it.
type SleepingServer struct {
	MOTD       string
	MaxPlayers int
}

// ServerGate lets the proxy interact with paused servers: status pings get a
// synthesized "sleeping" response, logins wake the container.
type ServerGate interface {
	// SleepingInfo returns sleeping-status data when the server is paused.
	SleepingInfo(serverID string) (*SleepingServer, bool)
	// WakeServer resumes a paused server so an incoming login can proceed.
	WakeServer(ctx context.Context, serverID string) error
}

// Config holds proxy configuration
type Config struct {
	ListenAddr string // Address to listen on (e.g., ":25565" or ":8080")
	Logger     *logger.Logger
	Gate       ServerGate
}

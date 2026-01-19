package proxy

import (
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

// Config holds proxy configuration
type Config struct {
	ListenAddr string // Address to listen on (e.g., ":25565" or ":8080")
	Logger     *logger.Logger
}

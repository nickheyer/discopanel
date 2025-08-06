package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nickheyer/discopanel/internal/config"
	db "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// Manager handles the lifecycle of the proxy and manages routes
type Manager struct {
	proxy       *Proxy
	store       *db.Store
	config      *config.ProxyConfig
	logger      *logger.Logger
	mu          sync.Mutex
	networkName string
}

// NewManager creates a new proxy manager
func NewManager(store *db.Store, cfg *config.ProxyConfig, logger *logger.Logger) *Manager {
	return &Manager{
		store:       store,
		config:      cfg,
		logger:      logger,
		networkName: "discopanel-network", // TODO: Get from main config
	}
}

// Start initializes and starts the proxy if enabled
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		m.logger.Info("Proxy is disabled in configuration")
		return nil
	}

	// Determine listen address
	listenAddr := ":25565" // Default Minecraft port
	if m.config.ListenPort > 0 {
		listenAddr = fmt.Sprintf(":%d", m.config.ListenPort)
	}

	// Create proxy instance
	m.proxy = New(&Config{
		ListenAddr: listenAddr,
		Logger:     m.logger,
	})

	// Load existing server routes
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	for _, server := range servers {
		// Add routes for servers with proxy hostname that are either running or have a container
		if server.ProxyHostname != "" && server.ContainerID != "" {
			// Get container IP address
			containerIP, err := getContainerIP(server.ContainerID, m.networkName)
			if err != nil {
				m.logger.Error("Failed to get container IP for server %s: %v", server.Name, err)
				continue
			}

			m.proxy.AddRoute(
				server.ID,
				server.ProxyHostname,
				containerIP, // Use IP address instead of container name
				25565,       // Internal Minecraft port
			)
			m.logger.Info("Added proxy route for server %s: %s -> %s:25565", server.Name, server.ProxyHostname, containerIP)
		}
	}

	// Start the proxy
	if err := m.proxy.Start(); err != nil {
		return fmt.Errorf("failed to start proxy: %w", err)
	}

	m.logger.Info("Proxy manager started")
	return nil
}

// Stop stops the proxy
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proxy == nil {
		return nil
	}

	if err := m.proxy.Stop(); err != nil {
		return fmt.Errorf("failed to stop proxy: %w", err)
	}

	m.proxy = nil
	m.logger.Info("Proxy manager stopped")
	return nil
}

// UpdateServerRoute updates or creates a route for a server
func (m *Manager) UpdateServerRoute(server *db.Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proxy == nil || !m.config.Enabled {
		return nil
	}

	hostname := m.generateHostname(server)

	// Add or update route for servers that are starting or running with proxy hostname
	if (server.Status == db.StatusRunning || server.Status == db.StatusStarting) && server.ProxyHostname != "" {
		// Get the container's IP address on the Docker network
		containerIP := ""
		if server.ContainerID != "" {
			if ip, err := getContainerIP(server.ContainerID, m.networkName); err == nil {
				containerIP = ip
			} else {
				m.logger.Error("Failed to get container IP for %s: %v", server.Name, err)
				return fmt.Errorf("failed to get container IP: %w", err)
			}
		} else {
			m.logger.Error("Server %s has no container ID", server.Name)
			return fmt.Errorf("server has no container")
		}

		routes := m.proxy.GetRoutes()
		if _, exists := routes[hostname]; exists {
			m.proxy.UpdateRoute(hostname, containerIP, 25565)
		} else {
			m.proxy.AddRoute(server.ID, hostname, containerIP, 25565)
		}
	} else if server.Status == db.StatusStopped || server.Status == db.StatusStopping {
		// Remove route if server is stopped or stopping
		m.proxy.RemoveRoute(hostname)
	}

	return nil
}

// RemoveServerRoute removes a route for a server
func (m *Manager) RemoveServerRoute(serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proxy == nil || !m.config.Enabled {
		return nil
	}

	server, err := m.store.GetServer(context.Background(), serverID)
	if err != nil {
		return err
	}

	hostname := m.generateHostname(server)
	m.proxy.RemoveRoute(hostname)

	return nil
}

// generateHostname generates the hostname for a server
func (m *Manager) generateHostname(server *db.Server) string {
	// Use custom hostname if set
	if server.ProxyHostname != "" {
		return server.ProxyHostname
	}

	// Otherwise use default pattern
	if m.config.BaseURL != "" {
		// Use server name as subdomain
		return fmt.Sprintf("%s.%s", strings.ToLower(strings.ReplaceAll(server.Name, " ", "-")), m.config.BaseURL)
	}
	// Fallback to using server ID
	return fmt.Sprintf("server-%s.minecraft.mc", server.ID)
}

// GetRoutes returns all current proxy routes
func (m *Manager) GetRoutes() map[string]*Route {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proxy == nil {
		return make(map[string]*Route)
	}

	return m.proxy.GetRoutes()
}

// IsRunning returns whether the proxy is running
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proxy == nil {
		return false
	}

	return m.proxy.IsRunning()
}

// AllocateProxyPort allocates a proxy port for a server
func (m *Manager) AllocateProxyPort(serverID string) (int, error) {
	// Get all servers to find used proxy ports
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return 0, err
	}

	usedPorts := make(map[int]bool)
	for _, server := range servers {
		if server.ProxyPort > 0 && server.ID != serverID {
			usedPorts[server.ProxyPort] = true
		}
	}

	// Find an available port in the configured range
	for port := m.config.PortRangeMin; port <= m.config.PortRangeMax; port++ {
		if !usedPorts[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available proxy ports in range %d-%d", m.config.PortRangeMin, m.config.PortRangeMax)
}

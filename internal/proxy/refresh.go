package proxy

import (
	"context"

	db "github.com/nickheyer/discopanel/internal/db"
)

// RefreshRoutes refreshes all routes based on current server states
func (m *Manager) RefreshRoutes() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.proxy == nil || !m.config.Enabled {
		return nil
	}

	// Get all servers
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return err
	}

	// Clear all existing routes
	currentRoutes := m.proxy.GetRoutes()
	for hostname := range currentRoutes {
		m.proxy.RemoveRoute(hostname)
	}

	// Re-add routes for running servers with proxy hostname
	for _, server := range servers {
		if (server.Status == db.StatusRunning || server.Status == db.StatusStarting) && server.ProxyHostname != "" && server.ContainerID != "" {
			// Get container IP address
			containerIP, err := getContainerIP(server.ContainerID, m.networkName)
			if err != nil {
				m.logger.Error("Failed to get container IP for server %s: %v", server.Name, err)
				continue
			}

			m.proxy.AddRoute(
				server.ID,
				server.ProxyHostname,
				containerIP,
				25565,
			)
			m.logger.Info("Refreshed route for server %s: %s -> %s:25565", server.Name, server.ProxyHostname, containerIP)
		}
	}

	return nil
}

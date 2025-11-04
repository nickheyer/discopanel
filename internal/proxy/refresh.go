package proxy

import (
	"context"

	db "github.com/nickheyer/discopanel/internal/db"
)

// RefreshRoutes refreshes all routes based on current server states
func (m *Manager) RefreshRoutes() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.proxies) == 0 || !m.config.Enabled {
		return nil
	}

	// Get all servers
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return err
	}

	// Get all listeners
	listeners, err := m.store.GetProxyListeners(context.Background())
	if err != nil {
		return err
	}

	// Map to track which listener each server uses
	listenerMap := make(map[string]*db.ProxyListener)
	for _, listener := range listeners {
		listenerMap[listener.ID] = listener
	}

	// Clear all existing routes from all proxies
	for _, proxy := range m.proxies {
		currentRoutes := proxy.GetRoutes()
		for hostname := range currentRoutes {
			proxy.RemoveRoute(hostname)
		}
	}

	// Re-add routes for running servers with proxy hostname
	for _, server := range servers {
		if (server.Status == db.StatusRunning || server.Status == db.StatusStarting) &&
			server.ProxyHostname != "" && server.ContainerID != "" && server.ProxyListenerID != "" {

			// Find which listener this server uses
			listener, ok := listenerMap[server.ProxyListenerID]
			if !ok || !listener.Enabled {
				m.logger.Error("Server %s has invalid or disabled listener %s", server.Name, server.ProxyListenerID)
				continue
			}

			// Get the proxy instance for this listener's port
			proxy, ok := m.proxies[listener.Port]
			if !ok {
				m.logger.Error("No proxy instance for port %d", listener.Port)
				continue
			}

			// Get container IP address
			containerIP, err := m.containerProvider.GetIP(proxy.ctx, server.ContainerID, m.networkName)
			if err != nil {
				m.logger.Error("Failed to get container IP for server %s: %v", server.Name, err)
				continue
			}

			proxy.AddRoute(
				server.ID,
				server.ProxyHostname,
				containerIP,
				25565,
			)
			m.logger.Info("Refreshed route for server %s: %s -> %s:25565 on port %d",
				server.Name, server.ProxyHostname, containerIP, listener.Port)
		}
	}

	return nil
}

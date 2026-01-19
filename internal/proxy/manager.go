package proxy

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/nickheyer/discopanel/internal/config"
	db "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// Manager handles the lifecycle of the proxy and manages routes
type Manager struct {
	proxies     map[int]Proxier // Map of port -> Proxy instance (TCP or UDP)
	store       *db.Store
	config      *config.ProxyConfig
	logger      *logger.Logger
	mu          sync.Mutex
	networkName string
}

// NewManager creates a new proxy manager
func NewManager(store *db.Store, cfg *config.Config, logger *logger.Logger) *Manager {
	return &Manager{
		proxies:     make(map[int]Proxier),
		store:       store,
		config:      &cfg.Proxy,
		logger:      logger,
		networkName: cfg.Docker.NetworkName,
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

	// Get all proxy listeners from database
	listeners, err := m.store.GetProxyListeners(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load proxy listeners: %w", err)
	}

	// Create a proxy instance for each enabled listener
	for _, listener := range listeners {
		if !listener.Enabled {
			continue
		}

		listenAddr := fmt.Sprintf(":%d", listener.Port)
		proxy := NewMinecraftProxy(&Config{
			ListenAddr: listenAddr,
			Logger:     m.logger,
		})

		m.proxies[listener.Port] = proxy
		m.logger.Info("Created Minecraft proxy for listener %s on port %d", listener.Name, listener.Port)
	}

	// Load existing server routes
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	// Map to track which listener each server uses
	listenerMap := make(map[string]*db.ProxyListener)
	for _, listener := range listeners {
		listenerMap[listener.ID] = listener
	}

	for _, server := range servers {
		// Add routes for servers with proxy hostname that are either running or have a container
		if server.ProxyHostname != "" && server.ContainerID != "" && server.ProxyListenerID != "" {
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
			containerIP, err := getContainerIP(server.ContainerID, m.networkName)
			if err != nil {
				m.logger.Error("Failed to get container IP for server %s: %v", server.Name, err)
				continue
			}

			proxy.AddRoute(
				server.ID,
				server.ProxyHostname,
				containerIP, // Use IP address instead of container name
				25565,       // Internal Minecraft port
			)
			m.logger.Info("Added proxy route for server %s: %s -> %s:25565 on listener port %d",
				server.Name, server.ProxyHostname, containerIP, listener.Port)
		}
	}

	// Start all proxy instances
	for port, proxy := range m.proxies {
		if err := proxy.Start(); err != nil {
			return fmt.Errorf("failed to start proxy on port %d: %w", port, err)
		}
	}

	m.logger.Info("Proxy manager started")
	return nil
}

// Stop stops all proxy instances
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.proxies) == 0 {
		return nil
	}

	var lastErr error
	for port, proxy := range m.proxies {
		if err := proxy.Stop(); err != nil {
			lastErr = fmt.Errorf("failed to stop proxy on port %d: %w", port, err)
			m.logger.Error("Failed to stop proxy on port %d: %v", port, err)
		}
	}

	m.proxies = make(map[int]Proxier)
	m.logger.Info("Proxy manager stopped")
	return lastErr
}

// UpdateServerRoute updates or creates a route for a server
func (m *Manager) UpdateServerRoute(server *db.Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.proxies) == 0 || !m.config.Enabled {
		return nil
	}

	// Get the listener for this server
	if server.ProxyListenerID == "" {
		return nil // No listener assigned
	}

	listener, err := m.store.GetProxyListener(context.Background(), server.ProxyListenerID)
	if err != nil {
		return fmt.Errorf("failed to get proxy listener: %w", err)
	}

	if !listener.Enabled {
		return nil // Listener is disabled
	}

	// Get the proxy instance for this listener's port
	proxy, ok := m.proxies[listener.Port]
	if !ok {
		return fmt.Errorf("no proxy instance for port %d", listener.Port)
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

		routes := proxy.GetRoutes()
		if _, exists := routes[hostname]; exists {
			proxy.UpdateRoute(hostname, containerIP, 25565)
		} else {
			proxy.AddRoute(server.ID, hostname, containerIP, 25565)
		}
		m.logger.Info("Updated route for server %s on port %d", server.Name, listener.Port)
	} else if server.Status == db.StatusStopped || server.Status == db.StatusStopping {
		// Remove route if server is stopped or stopping
		proxy.RemoveRoute(hostname)
	}

	return nil
}

// RemoveServerRoute removes a route for a server
func (m *Manager) RemoveServerRoute(serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.proxies) == 0 || !m.config.Enabled {
		return nil
	}

	server, err := m.store.GetServer(context.Background(), serverID)
	if err != nil {
		return err
	}

	hostname := m.generateHostname(server)

	// Remove from all proxies (in case it was moved between listeners)
	for _, proxy := range m.proxies {
		proxy.RemoveRoute(hostname)
	}

	return nil
}

// Removes a route using the hostname
func (m *Manager) RemoveRouteByHostname(hostname string, listenerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.proxies) == 0 || !m.config.Enabled || hostname == "" {
		return nil
	}

	// If listenerID provided, only remove from that specific listener's proxy
	if listenerID != "" {
		listener, err := m.store.GetProxyListener(context.Background(), listenerID)
		if err == nil && listener != nil {
			if proxy, ok := m.proxies[listener.Port]; ok {
				proxy.RemoveRoute(hostname)
				m.logger.Info("Removed route %s from listener port %d", hostname, listener.Port)
				return nil
			}
		}
	}

	// Otherwise remove from all proxies
	for port, proxy := range m.proxies {
		proxy.RemoveRoute(hostname)
		m.logger.Debug("Removed route %s from port %d", hostname, port)
	}

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

// GetRoutes returns all current proxy routes from all proxies
func (m *Manager) GetRoutes() map[string]*Route {
	m.mu.Lock()
	defer m.mu.Unlock()

	allRoutes := make(map[string]*Route)
	for _, proxy := range m.proxies {
		maps.Copy(allRoutes, proxy.GetRoutes())
	}

	return allRoutes
}

// IsRunning returns whether any proxy is running
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, proxy := range m.proxies {
		if proxy.IsRunning() {
			return true
		}
	}

	return false
}

// AddListener creates and starts a proxy instance for a new listener
func (m *Manager) AddListener(listener *db.ProxyListener) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled || !listener.Enabled {
		return nil
	}

	// Check if proxy already exists for this port
	if _, exists := m.proxies[listener.Port]; exists {
		return fmt.Errorf("proxy already exists for port %d", listener.Port)
	}

	// Create new proxy instance
	listenAddr := fmt.Sprintf(":%d", listener.Port)
	proxy := NewMinecraftProxy(&Config{
		ListenAddr: listenAddr,
		Logger:     m.logger,
	})

	// Start the proxy
	if err := proxy.Start(); err != nil {
		return fmt.Errorf("failed to start proxy on port %d: %w", listener.Port, err)
	}

	m.proxies[listener.Port] = proxy
	m.logger.Info("Added and started Minecraft proxy for listener %s on port %d", listener.Name, listener.Port)

	return nil
}

// RemoveListener stops and removes a proxy instance for a listener
func (m *Manager) RemoveListener(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proxy, exists := m.proxies[port]
	if !exists {
		return nil // Already removed or doesn't exist
	}

	// Stop the proxy
	if err := proxy.Stop(); err != nil {
		m.logger.Error("Failed to stop proxy on port %d: %v", port, err)
	}

	delete(m.proxies, port)
	m.logger.Info("Removed proxy for port %d", port)

	return nil
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

// AddModuleRoute adds a proxy route for a module's ports
// Modules use their own ports on the same hostname as their parent server
func (m *Manager) AddModuleRoute(module *db.Module, server *db.Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled || server.ProxyHostname == "" {
		return nil
	}

	// Get the container IP first
	if module.ContainerID == "" {
		return fmt.Errorf("module has no container ID")
	}

	containerIP, err := getContainerIP(module.ContainerID, m.networkName)
	if err != nil {
		return fmt.Errorf("failed to get module container IP: %w", err)
	}

	// Add routes for all proxy-enabled ports
	for _, port := range module.Ports {
		if port == nil || !port.ProxyEnabled || port.HostPort == 0 {
			continue
		}

		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}

		routeID := fmt.Sprintf("%s-port-%d", module.ID, port.HostPort)
		if err := m.addPortRouteUnlocked(routeID, server.ProxyHostname, containerIP,
			int(port.HostPort), int(port.ContainerPort), protocol, module.Name, port.Name); err != nil {
			m.logger.Error("Failed to add port route for %s: %v", port.Name, err)
		}
	}

	return nil
}

// addPortRouteUnlocked adds a single port route (must be called with lock held)
func (m *Manager) addPortRouteUnlocked(routeID, hostname, containerIP string, hostPort, containerPort int, protocol, moduleName, portName string) error {
	if containerPort == 0 {
		containerPort = 8081
	}

	// Check if a proxy already exists for this port
	proxy, exists := m.proxies[hostPort]
	if !exists {
		listenAddr := fmt.Sprintf(":%d", hostPort)
		cfg := &Config{
			ListenAddr: listenAddr,
			Logger:     m.logger,
		}

		// Create appropriate proxy type based on protocol
		switch protocol {
		case "udp":
			proxy = NewUDPProxy(cfg)
			m.logger.Info("Created UDP proxy for port %d", hostPort)
		case "minecraft":
			proxy = NewMinecraftProxy(cfg)
			m.logger.Info("Created Minecraft proxy for port %d", hostPort)
		case "http":
			proxy = NewHTTPProxy(cfg)
			m.logger.Info("Created HTTP proxy for port %d", hostPort)
		default:
			// Raw TCP forwarding (includes "tcp")
			proxy = NewTCPProxy(cfg)
			m.logger.Info("Created TCP proxy for port %d", hostPort)
		}

		// Start the proxy
		if err := proxy.Start(); err != nil {
			return fmt.Errorf("failed to start module proxy on port %d: %w", hostPort, err)
		}

		m.proxies[hostPort] = proxy
	}

	// Add route
	proxy.AddRoute(routeID, hostname, containerIP, containerPort)
	m.logger.Info("Added module route: %s:%d -> %s:%d (module: %s, port: %s, protocol: %s)",
		hostname, hostPort, containerIP, containerPort, moduleName, portName, protocol)

	return nil
}

// RemoveModuleRoute removes proxy routes for a module's ports
func (m *Manager) RemoveModuleRoute(moduleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		return nil
	}

	// Find the module to get its ports
	module, err := m.store.GetModule(context.Background(), moduleID)
	if err != nil {
		return err
	}

	// Get the server to find the hostname
	server, err := m.store.GetServer(context.Background(), module.ServerID)
	if err != nil {
		return err
	}

	// Remove all port routes
	for _, port := range module.Ports {
		if port == nil || port.HostPort == 0 {
			continue
		}
		m.removePortRouteUnlocked(int(port.HostPort), server.ProxyHostname, module.Name, port.Name)
	}

	return nil
}

// removePortRouteUnlocked removes a single port route and cleans up empty proxies (must be called with lock held)
func (m *Manager) removePortRouteUnlocked(hostPort int, hostname, moduleName, portName string) {
	proxy, exists := m.proxies[hostPort]
	if !exists {
		return // No proxy for this port
	}

	if hostname != "" {
		proxy.RemoveRoute(hostname)
		m.logger.Info("Removed module route: %s:%d (module: %s, port: %s)", hostname, hostPort, moduleName, portName)
	}

	// Check if this proxy has any remaining routes
	routes := proxy.GetRoutes()
	if len(routes) == 0 {
		// Stop and remove the proxy since it's no longer needed
		if err := proxy.Stop(); err != nil {
			m.logger.Error("Failed to stop module proxy on port %d: %v", hostPort, err)
		}
		delete(m.proxies, hostPort)
		m.logger.Info("Removed unused module proxy for port %d", hostPort)
	}
}

// UpdateModuleRoute updates proxy routes for a module's ports
func (m *Manager) UpdateModuleRoute(module *db.Module, server *db.Server) error {
	if !m.config.Enabled || server.ProxyHostname == "" {
		return nil
	}

	if module.ContainerID == "" {
		return nil
	}

	// Get the container IP
	containerIP, err := getContainerIP(module.ContainerID, m.networkName)
	if err != nil {
		return fmt.Errorf("failed to get module container IP: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Update routes for all proxy-enabled ports
	for _, port := range module.Ports {
		if port == nil || !port.ProxyEnabled || port.HostPort == 0 {
			continue
		}

		proxy, exists := m.proxies[int(port.HostPort)]
		if !exists {
			// Need to add it
			protocol := port.Protocol
			if protocol == "" {
				protocol = "tcp"
			}
			routeID := fmt.Sprintf("%s-port-%d", module.ID, port.HostPort)
			if err := m.addPortRouteUnlocked(routeID, server.ProxyHostname, containerIP,
				int(port.HostPort), int(port.ContainerPort), protocol, module.Name, port.Name); err != nil {
				m.logger.Error("Failed to add port route for %s: %v", port.Name, err)
			}
			continue
		}

		proxy.UpdateRoute(server.ProxyHostname, containerIP, int(port.ContainerPort))
		m.logger.Info("Updated module route: %s:%d -> %s:%d (module: %s, port: %s)",
			server.ProxyHostname, port.HostPort, containerIP, port.ContainerPort, module.Name, port.Name)
	}

	return nil
}

// GetModuleRoutes returns all module proxy routes
func (m *Manager) GetModuleRoutes() map[int]map[string]*Route {
	m.mu.Lock()
	defer m.mu.Unlock()

	moduleRoutes := make(map[int]map[string]*Route)

	// Module proxies use ports outside the standard MC port range (e.g., 8100+)
	for port, proxy := range m.proxies {
		// Skip standard Minecraft proxy ports
		if port >= 25565 && port <= 25665 {
			continue
		}
		moduleRoutes[port] = proxy.GetRoutes()
	}

	return moduleRoutes
}

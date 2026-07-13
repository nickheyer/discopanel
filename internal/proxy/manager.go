package proxy

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/nickheyer/discopanel/internal/config"
	db "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Handles proxy lifecycle and manages routes
type Manager struct {
	proxies       map[int]Proxier // Map of port -> Proxy instance (TCP or UDP)
	listenerPorts map[int]bool    // Ports serving hostname-routed server listeners
	statsBase     map[string]RouteStatsSnapshot
	statsLast     map[string]RouteStatsSnapshot
	store         *db.Store
	docker        *docker.Client
	config        *config.ProxyConfig
	logger        *logger.Logger
	mu            sync.Mutex
	gate          ServerGate
}

// Registers the wake gate, must be called before Start
func (m *Manager) SetServerGate(gate ServerGate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gate = gate
	for _, p := range m.proxies {
		if mp, ok := p.(*MinecraftProxy); ok {
			mp.SetGate(gate)
		}
	}
}

// Creates a new proxy manager
func NewManager(store *db.Store, dockerClient *docker.Client, cfg *config.Config, logger *logger.Logger) *Manager {
	return &Manager{
		proxies:       make(map[int]Proxier),
		listenerPorts: make(map[int]bool),
		statsBase:     make(map[string]RouteStatsSnapshot),
		statsLast:     make(map[string]RouteStatsSnapshot),
		store:         store,
		docker:        dockerClient,
		config:        &cfg.Proxy,
		logger:        logger,
	}
}

// Resolves a container IP on the panel network
func (m *Manager) containerIP(containerID string) (string, error) {
	return m.docker.ContainerIP(context.Background(), containerID)
}

// Initializes and starts the proxy if enabled
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		m.logger.Info("Proxy is disabled in configuration")
		return nil
	}

	// Ensure a default listener exists when proxy is enabled
	if _, err := m.ensureDefaultListenerLocked(); err != nil {
		m.logger.Error("Failed to ensure default listener: %v", err)
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
			Gate:       m.gate,
		})

		m.proxies[listener.Port] = proxy
		m.listenerPorts[listener.Port] = true
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
		// Registers routes even for stopped wakeable servers
		if server.ProxyHostname == "" || server.ProxyListenerID == "" {
			continue
		}

		listener, ok := listenerMap[server.ProxyListenerID]
		if !ok || !listener.Enabled {
			m.logger.Error("Server %s has invalid or disabled listener %s", server.Name, server.ProxyListenerID)
			continue
		}

		mp, ok := m.proxies[listener.Port].(*MinecraftProxy)
		if !ok {
			m.logger.Error("No proxy instance for port %d", listener.Port)
			continue
		}

		route, want, err := m.desiredRoute(server, m.generateHostname(server))
		if err != nil {
			m.logger.Error("Failed to build route for server %s: %v", server.Name, err)
			continue
		}
		if want {
			mp.UpsertServerRoute(route)
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

// Stops all proxy instances
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
	m.listenerPorts = make(map[int]bool)
	m.logger.Info("Proxy manager stopped")
	return lastErr
}

// Reconciles a server route with its current status
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
	mp, ok := m.proxies[listener.Port].(*MinecraftProxy)
	if !ok {
		return fmt.Errorf("no proxy instance for port %d", listener.Port)
	}

	hostname := m.generateHostname(server)
	route, want, err := m.desiredRoute(server, hostname)
	if err != nil {
		return err
	}
	if !want {
		mp.RemoveRoute(hostname)
		return nil
	}
	mp.UpsertServerRoute(route)
	return nil
}

// Derives the route a server should serve right now
func (m *Manager) desiredRoute(server *db.Server, hostname string) (route Route, want bool, err error) {
	ctx := context.Background()
	cfg, cfgErr := m.store.GetServerProperties(ctx, server.ID)
	if cfgErr != nil {
		cfg = nil
	}

	route = Route{
		ServerID:      server.ID,
		Hostname:      hostname,
		BackendPort:   docker.DefaultMinecraftPort,
		ProxyProtocol: propEnabled(cfg, func(c *db.ServerProperties) *bool { return c.EnableProxyProtocol }),
		PreserveHost:  propEnabled(cfg, func(c *db.ServerProperties) *bool { return c.ProxyPreserveHostname }),
		MaxPlayers:    server.MaxPlayers,
	}
	wakeable := propEnabled(cfg, func(c *db.ServerProperties) *bool { return c.EnableWakeOnConnect })

	switch server.Status {
	case db.StatusRunning, db.StatusPaused, db.StatusUnhealthy:
		if server.ContainerID == "" {
			return Route{}, false, fmt.Errorf("server %s has no container", server.Name)
		}
		ip, ipErr := m.containerIP(server.ContainerID)
		if ipErr != nil {
			return Route{}, false, fmt.Errorf("failed to get container IP: %w", ipErr)
		}
		route.State = RouteOnline
		route.BackendHost = ip
		return route, true, nil

	case db.StatusProvisioning, db.StatusCreating, db.StatusStarting:
		route.State = RouteStarting
		route.MOTD = bootMOTD(server, cfg)
		if server.ContainerID != "" {
			if ip, ipErr := m.containerIP(server.ContainerID); ipErr == nil {
				route.BackendHost = ip
			}
		}
		return route, true, nil

	case db.StatusStopped, db.StatusStopping, db.StatusError:
		if !wakeable {
			return Route{}, false, nil
		}
		route.State = RouteOffline
		route.Wakeable = true
		route.MOTD = offlineMOTD(server, cfg)
		return route, true, nil

	default:
		return Route{}, false, nil
	}
}

// Reads an optional bool off possibly-nil properties
func propEnabled(cfg *db.ServerProperties, field func(*db.ServerProperties) *bool) bool {
	if cfg == nil {
		return false
	}
	v := field(cfg)
	return v != nil && *v
}

// Builds the joinable-while-stopped status line
func offlineMOTD(server *db.Server, cfg *db.ServerProperties) string {
	if cfg != nil && cfg.MOTD != nil && *cfg.MOTD != "" {
		return *cfg.MOTD + " (offline - join to start it up)"
	}
	return server.Name + " is offline - join to start it up"
}

// Builds the status line shown while a server boots
func bootMOTD(server *db.Server, cfg *db.ServerProperties) string {
	phase := "starting up"
	switch server.Status {
	case db.StatusProvisioning:
		phase = "installing server files"
	case db.StatusCreating:
		phase = "preparing the container"
	}
	if cfg != nil && cfg.MOTD != nil && *cfg.MOTD != "" {
		return fmt.Sprintf("%s (%s - join in a moment)", *cfg.MOTD, phase)
	}
	return fmt.Sprintf("%s is %s - join in a moment", server.Name, phase)
}

// Removes a route for a server
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

	// Remove from all proxies since listener may have changed
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

// Generates the hostname for a server
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

// Returns all current routes from all proxies
func (m *Manager) GetRoutes() map[string]*Route {
	m.mu.Lock()
	defer m.mu.Unlock()

	allRoutes := make(map[string]*Route)
	for _, proxy := range m.proxies {
		maps.Copy(allRoutes, proxy.GetRoutes())
	}

	return allRoutes
}

// Returns whether any proxy is running
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

// Creates and starts a proxy for a new listener
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
		Gate:       m.gate,
	})

	// Start the proxy
	if err := proxy.Start(); err != nil {
		return fmt.Errorf("failed to start proxy on port %d: %w", listener.Port, err)
	}

	m.proxies[listener.Port] = proxy
	m.listenerPorts[listener.Port] = true
	m.logger.Info("Added and started Minecraft proxy for listener %s on port %d", listener.Name, listener.Port)

	return nil
}

// Stops and removes a proxy instance for a listener
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
	delete(m.listenerPorts, port)
	m.logger.Info("Removed proxy for port %d", port)

	return nil
}

// Adds proxy routes for a module's ports
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

	containerIP, err := m.containerIP(module.ContainerID)
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
			int(port.HostPort), m.moduleContainerPort(module, port), protocol, module.Name, port.Name); err != nil {
			m.logger.Error("Failed to add port route for %s: %v", port.Name, err)
		}
	}

	return nil
}

// Resolves a container port from the template when unset
func (m *Manager) moduleContainerPort(module *db.Module, port *v1.ModulePort) int {
	if port.ContainerPort != 0 {
		return int(port.ContainerPort)
	}

	template, err := m.store.GetModuleTemplate(context.Background(), module.TemplateID)
	if err != nil {
		return 0
	}
	for _, tp := range template.Ports {
		if tp != nil && tp.Name == port.Name {
			return int(tp.ContainerPort)
		}
	}
	return 0
}

// Adds a single port route, caller must hold lock
func (m *Manager) addPortRouteUnlocked(routeID, hostname, containerIP string, hostPort, containerPort int, protocol, moduleName, portName string) error {
	if containerPort == 0 {
		return fmt.Errorf("no container port declared for %s", portName)
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

// Removes proxy routes for a module's ports
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

// Removes a port route, prunes empty proxies, lock held
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

// Updates proxy routes for a module's ports
func (m *Manager) UpdateModuleRoute(module *db.Module, server *db.Server) error {
	if !m.config.Enabled || server.ProxyHostname == "" {
		return nil
	}

	if module.ContainerID == "" {
		return nil
	}

	// Get the container IP
	containerIP, err := m.containerIP(module.ContainerID)
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

		containerPort := m.moduleContainerPort(module, port)
		proxy, exists := m.proxies[int(port.HostPort)]
		if !exists {
			// Need to add it
			protocol := port.Protocol
			if protocol == "" {
				protocol = "tcp"
			}
			routeID := fmt.Sprintf("%s-port-%d", module.ID, port.HostPort)
			if err := m.addPortRouteUnlocked(routeID, server.ProxyHostname, containerIP,
				int(port.HostPort), containerPort, protocol, module.Name, port.Name); err != nil {
				m.logger.Error("Failed to add port route for %s: %v", port.Name, err)
			}
			continue
		}

		if containerPort == 0 {
			m.logger.Error("No container port declared for %s", port.Name)
			continue
		}
		proxy.UpdateRoute(server.ProxyHostname, containerIP, containerPort)
		m.logger.Info("Updated module route: %s:%d -> %s:%d (module: %s, port: %s)",
			server.ProxyHostname, port.HostPort, containerIP, containerPort, module.Name, port.Name)
	}

	return nil
}

// Returns all module proxy routes
func (m *Manager) GetModuleRoutes() map[int]map[string]*Route {
	m.mu.Lock()
	defer m.mu.Unlock()

	moduleRoutes := make(map[int]map[string]*Route)

	// Non-listener ports all belong to modules
	for port, proxy := range m.proxies {
		if m.listenerPorts[port] {
			continue
		}
		moduleRoutes[port] = proxy.GetRoutes()
	}

	return moduleRoutes
}

// Aggregates per-server route counters from every listener
func (m *Manager) GetRouteStats() map[string]RouteStatsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := make(map[string]RouteStatsSnapshot)
	for _, proxy := range m.proxies {
		mp, ok := proxy.(*MinecraftProxy)
		if !ok {
			continue
		}
		for id, raw := range mp.StatsSnapshots() {
			if countersReset(m.statsLast[id], raw) {
				m.statsBase[id] = addCounters(m.statsBase[id], m.statsLast[id])
			}
			m.statsLast[id] = raw
			stats[id] = addCounters(m.statsBase[id], raw)
		}
	}
	return stats
}

// Detects a counter restart after route removal
func countersReset(last, cur RouteStatsSnapshot) bool {
	return cur.TotalConns < last.TotalConns ||
		cur.StatusPings < last.StatusPings ||
		cur.Logins < last.Logins ||
		cur.Wakes < last.Wakes ||
		cur.BytesToBackend < last.BytesToBackend ||
		cur.BytesToClient < last.BytesToClient
}

// Adds monotonic counters onto a base, gauges pass through
func addCounters(base, cur RouteStatsSnapshot) RouteStatsSnapshot {
	return RouteStatsSnapshot{
		ActiveConns:    cur.ActiveConns,
		TotalConns:     base.TotalConns + cur.TotalConns,
		StatusPings:    base.StatusPings + cur.StatusPings,
		Logins:         base.Logins + cur.Logins,
		Wakes:          base.Wakes + cur.Wakes,
		BytesToBackend: base.BytesToBackend + cur.BytesToBackend,
		BytesToClient:  base.BytesToClient + cur.BytesToClient,
		LastProtocol:   cur.LastProtocol,
	}
}

// Creates default proxy listener if proxy is enabled
func (m *Manager) EnsureDefaultListener() (*db.ProxyListener, error) {
	if !m.config.Enabled {
		return nil, nil
	}
	return m.ensureDefaultListenerLocked()
}

// Assumes caller has already verified proxy is enabled
func (m *Manager) ensureDefaultListenerLocked() (*db.ProxyListener, error) {
	ctx := context.Background()

	// Check if any listeners exist
	listeners, err := m.store.GetProxyListeners(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy listeners: %w", err)
	}

	if len(listeners) > 0 {
		return nil, nil
	}

	// Find an available port using the store function
	port, err := m.store.FindAvailableListenerPort(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}

	// Create default listener
	defaultListener := &db.ProxyListener{
		ID:        "default",
		Port:      port,
		Name:      "Primary",
		IsDefault: true,
		Enabled:   true,
	}

	if err := m.store.CreateProxyListener(ctx, defaultListener); err != nil {
		return nil, fmt.Errorf("failed to create default listener: %w", err)
	}

	m.logger.Info("Created default proxy listener on port %d", port)
	return defaultListener, nil
}

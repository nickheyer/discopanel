package tunnel

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/nickheyer/discopanel/internal/cloudflare"
	"github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
)

const (
	cloudflaredImage = "cloudflare/cloudflared:latest"
	containerName    = "discopanel-cloudflared"
)

// Manager handles the lifecycle of the cloudflared container and Cloudflare API operations
type Manager struct {
	docker      *client.Client
	store       *db.Store
	cfClient    *cloudflare.Client
	logger      *logger.Logger
	networkName string
	mu          sync.Mutex
}

// NewManager creates a new tunnel manager
func NewManager(dockerClient *client.Client, store *db.Store, logger *logger.Logger, networkName string) *Manager {
	return &Manager{
		docker:      dockerClient,
		store:       store,
		logger:      logger,
		networkName: networkName,
	}
}

// Start initializes and starts the cloudflared container if tunnel is enabled
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get proxy configuration
	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy configuration: %w", err)
	}

	if !config.TunnelEnabled {
		m.logger.Info("Tunnel is disabled in proxy configuration")
		return nil
	}

	// Initialize Cloudflare client
	if config.CloudflareAccountID == "" || config.CloudflareAPIToken == "" {
		return fmt.Errorf("tunnel is enabled but Cloudflare credentials are not configured")
	}

	m.cfClient = cloudflare.NewClient(config.CloudflareAccountID, config.CloudflareAPIToken)

	// Prune orphaned DNS records before starting
	if err := m.pruneOrphanedDNSRecords(); err != nil {
		m.logger.Error("Failed to prune orphaned DNS records: %v", err)
	}

	// Start the cloudflared container
	if err := m.startContainer(config); err != nil {
		return fmt.Errorf("failed to start cloudflared container: %w", err)
	}

	// Update tunnel ingress with current routes
	if err := m.UpdateTunnelIngress(); err != nil {
		m.logger.Error("Failed to update tunnel ingress: %v", err)
	}

	m.logger.Info("Tunnel manager started")
	return nil
}

// Stop stops the cloudflared container
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return err
	}

	if config.TunnelContainerID == "" {
		return nil
	}

	// Stop the container
	timeout := 10
	if err := m.docker.ContainerStop(context.Background(), config.TunnelContainerID, container.StopOptions{
		Timeout: &timeout,
	}); err != nil {
		m.logger.Error("Failed to stop cloudflared container: %v", err)
	}

	// Remove the container
	if err := m.docker.ContainerRemove(context.Background(), config.TunnelContainerID, container.RemoveOptions{
		Force: true,
	}); err != nil {
		m.logger.Error("Failed to remove cloudflared container: %v", err)
	}

	// Update config
	config.TunnelContainerID = ""
	if err := m.store.SaveProxyConfig(context.Background(), config); err != nil {
		m.logger.Error("Failed to update proxy config: %v", err)
	}

	m.logger.Info("Tunnel manager stopped")
	return nil
}

// StartContainer starts the cloudflared container
func (m *Manager) StartContainer() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy configuration: %w", err)
	}

	if !config.TunnelEnabled {
		return fmt.Errorf("tunnel is not enabled")
	}

	if config.CloudflareAccountID == "" || config.CloudflareAPIToken == "" {
		return fmt.Errorf("Cloudflare credentials are not configured")
	}

	// Initialize CF client if not already
	if m.cfClient == nil {
		m.cfClient = cloudflare.NewClient(config.CloudflareAccountID, config.CloudflareAPIToken)
	}

	return m.startContainer(config)
}

// StopContainer stops the cloudflared container
func (m *Manager) StopContainer() error {
	return m.Stop()
}

// GetContainerStatus returns the current status of the cloudflared container
func (m *Manager) GetContainerStatus() (string, error) {
	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return "error", err
	}

	if !config.TunnelEnabled {
		return "not_configured", nil
	}

	if config.TunnelContainerID == "" {
		return "stopped", nil
	}

	inspect, err := m.docker.ContainerInspect(context.Background(), config.TunnelContainerID)
	if err != nil {
		return "error", nil
	}

	switch inspect.State.Status {
	case "running":
		return "running", nil
	case "restarting":
		return "restarting", nil
	case "exited", "dead":
		return "stopped", nil
	default:
		return inspect.State.Status, nil
	}
}

// ConfigureTunnel creates or updates the Cloudflare tunnel and saves credentials
func (m *Manager) ConfigureTunnel(accountID, apiToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Initialize CF client
	cfClient := cloudflare.NewClient(accountID, apiToken)

	// Check if a DiscoPanel tunnel already exists
	tunnels, err := cfClient.ListTunnels(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list tunnels: %w", err)
	}

	var tunnel *cloudflare.Tunnel
	var tunnelSecret string
	tunnelName := "discopanel-tunnel"

	// Look for existing DiscoPanel tunnel
	for _, t := range tunnels {
		if t.Name == tunnelName && t.DeletedAt == nil {
			tunnel = &t
			break
		}
	}

	// Create tunnel if it doesn't exist
	if tunnel == nil {
		m.logger.Info("Creating new Cloudflare tunnel: %s", tunnelName)
		newTunnel, secret, err := cfClient.CreateTunnel(context.Background(), tunnelName)
		if err != nil {
			return fmt.Errorf("failed to create tunnel: %w", err)
		}
		tunnel = newTunnel
		tunnelSecret = secret
		m.logger.Info("Created tunnel with ID: %s", tunnel.ID)
	} else {
		m.logger.Info("Using existing tunnel: %s (ID: %s)", tunnel.Name, tunnel.ID)
	}

	// Get tunnel token for running cloudflared
	token, err := cfClient.GetTunnelToken(context.Background(), tunnel.ID)
	if err != nil {
		return fmt.Errorf("failed to get tunnel token: %w", err)
	}

	// Get current proxy config
	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy config: %w", err)
	}

	// Update config with tunnel information
	config.CloudflareAccountID = accountID
	config.CloudflareAPIToken = apiToken
	config.TunnelID = tunnel.ID
	config.TunnelName = tunnel.Name
	if tunnelSecret != "" {
		config.TunnelSecret = tunnelSecret
	}
	config.TunnelToken = token
	config.TunnelEnabled = true

	// Save config
	if err := m.store.SaveProxyConfig(context.Background(), config); err != nil {
		return fmt.Errorf("failed to save proxy config: %w", err)
	}

	// Update CF client
	m.cfClient = cfClient

	// Fetch and save available domains
	if err := m.RefreshDomains(); err != nil {
		m.logger.Error("Failed to refresh domains: %v", err)
	}

	return nil
}

// RefreshDomains fetches available domains from Cloudflare and updates the database
func (m *Manager) RefreshDomains() error {
	if m.cfClient == nil {
		config, _, err := m.store.GetProxyConfig(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get proxy config: %w", err)
		}
		if config.CloudflareAccountID == "" || config.CloudflareAPIToken == "" {
			return fmt.Errorf("Cloudflare credentials not configured")
		}
		m.cfClient = cloudflare.NewClient(config.CloudflareAccountID, config.CloudflareAPIToken)
	}

	zones, err := m.cfClient.ListZones(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list zones: %w", err)
	}

	for _, zone := range zones {
		// Check if domain already exists
		existing, err := m.store.GetCloudflareDomainByZoneID(context.Background(), zone.ID)
		if err != nil {
			m.logger.Error("Failed to check existing domain: %v", err)
			continue
		}

		if existing == nil {
			// Create new domain entry
			domain := &db.CloudflareDomain{
				ZoneID:   zone.ID,
				ZoneName: zone.Name,
				Enabled:  true, // Enable all domains by default
			}
			if err := m.store.SaveCloudflareDomain(context.Background(), domain); err != nil {
				m.logger.Error("Failed to save domain %s: %v", zone.Name, err)
			} else {
				m.logger.Info("Added Cloudflare domain: %s", zone.Name)
			}
		}
	}

	m.logger.Info("Refreshed %d domains from Cloudflare", len(zones))
	return nil
}

// CreateDNSRecord creates a CNAME record for a tunnel route
func (m *Manager) CreateDNSRecord(hostname string) (*proxy.TunnelInfo, error) {
	if m.cfClient == nil {
		return nil, fmt.Errorf("Cloudflare client not initialized")
	}

	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy config: %w", err)
	}

	if config.TunnelID == "" {
		return nil, fmt.Errorf("tunnel not configured")
	}

	// Parse hostname to get zone
	parts := strings.SplitN(hostname, ".", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid hostname: %s", hostname)
	}

	subdomain := parts[0]
	domain := parts[1]

	// Find the zone ID
	zones, err := m.cfClient.ListZones(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list zones: %w", err)
	}

	var zoneID string
	for _, zone := range zones {
		if zone.Name == domain {
			zoneID = zone.ID
			break
		}
	}

	if zoneID == "" {
		return nil, fmt.Errorf("zone not found for domain: %s", domain)
	}

	// Check if domain is enabled for tunneling
	cfDomain, err := m.store.GetCloudflareDomainByZoneID(context.Background(), zoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to check domain: %w", err)
	}

	if cfDomain != nil && !cfDomain.Enabled {
		return nil, fmt.Errorf("domain %s is not enabled for tunneling", domain)
	}

	// Create DNS record
	dnsRecord, err := m.cfClient.CreateDNSRecord(context.Background(), zoneID, subdomain, config.TunnelID)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS record: %w", err)
	}

	return &proxy.TunnelInfo{
		ZoneID:      zoneID,
		DNSRecordID: dnsRecord.ID,
	}, nil
}

// DeleteDNSRecord deletes a DNS record
func (m *Manager) DeleteDNSRecord(tunnelInfo *proxy.TunnelInfo) error {
	if m.cfClient == nil {
		config, _, err := m.store.GetProxyConfig(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get proxy config: %w", err)
		}
		m.cfClient = cloudflare.NewClient(config.CloudflareAccountID, config.CloudflareAPIToken)
	}

	if tunnelInfo == nil || tunnelInfo.DNSRecordID == "" {
		return nil // Nothing to delete
	}

	return m.cfClient.DeleteDNSRecord(context.Background(), tunnelInfo.ZoneID, tunnelInfo.DNSRecordID)
}

// UpdateTunnelIngress updates the tunnel's ingress configuration based on current routes
func (m *Manager) UpdateTunnelIngress() error {
	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy config: %w", err)
	}

	if !config.TunnelEnabled || config.TunnelID == "" {
		return nil // Tunnel not configured
	}

	if m.cfClient == nil {
		m.cfClient = cloudflare.NewClient(config.CloudflareAccountID, config.CloudflareAPIToken)
	}

	// Get all servers with proxy routes
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}

	// Get proxy listeners to find the ports
	listeners, err := m.store.GetProxyListeners(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy listeners: %w", err)
	}

	// Build ingress rules for servers that have proxy hostnames
	var ingress []cloudflare.IngressRule
	hostnameToPort := make(map[string]int)

	for _, server := range servers {
		if server.ProxyHostname != "" && server.ContainerID != "" && server.ProxyListenerID != "" {
			// Find the listener port for this server
			var listenerPort int
			for _, listener := range listeners {
				if listener.ID == server.ProxyListenerID {
					listenerPort = listener.Port
					break
				}
			}

			if listenerPort > 0 && hostnameToPort[server.ProxyHostname] == 0 {
				// Route to DiscoPanel's proxy listener on localhost (using host network mode)
				ingress = append(ingress, cloudflare.IngressRule{
					Hostname: server.ProxyHostname,
					Service:  fmt.Sprintf("tcp://localhost:%d", listenerPort),
				})
				hostnameToPort[server.ProxyHostname] = listenerPort
			}
		}
	}

	// Add catch-all rule (required)
	ingress = append(ingress, cloudflare.IngressRule{
		Service: "http_status:404",
	})

	// Update tunnel configuration
	if err := m.cfClient.UpdateTunnelConfiguration(context.Background(), config.TunnelID, ingress); err != nil {
		return fmt.Errorf("failed to update tunnel configuration: %w", err)
	}

	m.logger.Info("Updated tunnel ingress with %d routes", len(ingress)-1) // -1 for catch-all
	return nil
}

// CleanupTunnel removes the DiscoPanel tunnel from Cloudflare
func (m *Manager) CleanupTunnel() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy config: %w", err)
	}

	if config.TunnelID == "" {
		return nil // No tunnel to cleanup
	}

	if m.cfClient == nil {
		if config.CloudflareAccountID == "" || config.CloudflareAPIToken == "" {
			return fmt.Errorf("Cloudflare credentials not configured")
		}
		m.cfClient = cloudflare.NewClient(config.CloudflareAccountID, config.CloudflareAPIToken)
	}

	// Delete the tunnel
	if err := m.cfClient.DeleteTunnel(context.Background(), config.TunnelID); err != nil {
		m.logger.Error("Failed to delete tunnel: %v", err)
	}

	// Clear tunnel configuration
	config.TunnelID = ""
	config.TunnelName = ""
	config.TunnelSecret = ""
	config.TunnelToken = ""
	config.TunnelEnabled = false

	if err := m.store.SaveProxyConfig(context.Background(), config); err != nil {
		return fmt.Errorf("failed to save proxy config: %w", err)
	}

	m.logger.Info("Cleaned up Cloudflare tunnel")
	return nil
}

// pruneOrphanedDNSRecords removes DNS records that don't have corresponding servers
func (m *Manager) pruneOrphanedDNSRecords() error {
	if m.cfClient == nil {
		return fmt.Errorf("Cloudflare client not initialized")
	}

	// Get all servers from database
	servers, err := m.store.ListServers(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}

	// Build a map of valid hostnames
	validHostnames := make(map[string]bool)
	for _, server := range servers {
		if server.ProxyHostname != "" {
			validHostnames[server.ProxyHostname] = true
		}
	}

	// Get all zones from Cloudflare
	zones, err := m.cfClient.ListZones(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list zones: %w", err)
	}

	config, _, err := m.store.GetProxyConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get proxy config: %w", err)
	}

	if config.TunnelID == "" {
		return nil // No tunnel configured
	}

	// For each zone, check DNS records pointing to our tunnel
	tunnelTarget := fmt.Sprintf("%s.cfargotunnel.com", config.TunnelID)
	prunedCount := 0

	for _, zone := range zones {
		// List all CNAME records pointing to our tunnel
		records, err := m.cfClient.ListDNSRecords(context.Background(), zone.ID, "CNAME", tunnelTarget)
		if err != nil {
			m.logger.Error("Failed to list DNS records for zone %s: %v", zone.Name, err)
			continue
		}

		// Check each record and delete if orphaned
		for _, record := range records {
			hostname := record.Name
			if !validHostnames[hostname] {
				// This is an orphaned record - delete it
				if err := m.cfClient.DeleteDNSRecord(context.Background(), zone.ID, record.ID); err != nil {
					m.logger.Error("Failed to delete orphaned DNS record %s: %v", hostname, err)
				} else {
					m.logger.Info("Pruned orphaned DNS record: %s", hostname)
					prunedCount++
				}
			}
		}
	}

	if prunedCount > 0 {
		m.logger.Info("Pruned %d orphaned DNS records", prunedCount)
	}

	return nil
}

// startContainer starts the cloudflared container
func (m *Manager) startContainer(config *db.ProxyConfig) error {
	ctx := context.Background()

	// Pull the cloudflared image
	reader, err := m.docker.ImagePull(ctx, cloudflaredImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull cloudflared image: %w", err)
	}
	defer reader.Close()

	// Wait for image pull to complete
	buf := make([]byte, 1024)
	for {
		_, err := reader.Read(buf)
		if err == io.EOF {
			break
		}
	}

	// Remove existing container if it exists
	_ = m.docker.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})

	// Create container configuration
	containerConfig := &container.Config{
		Image: cloudflaredImage,
		Cmd: []string{
			"tunnel",
			"--no-autoupdate",
			"--protocol",
			"http2",
			"run",
			"--token",
			config.TunnelToken,
		},
		Labels: map[string]string{
			"discopanel.managed": "true",
			"discopanel.type":    "cloudflared",
		},
	}

	// Host configuration - use host network mode for proper TCP routing
	hostConfig := &container.HostConfig{
		NetworkMode: "host",
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "3",
			},
		},
	}

	// Network configuration - not needed with host mode
	networkConfig := &network.NetworkingConfig{}

	// Create the container
	resp, err := m.docker.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkConfig,
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("failed to create cloudflared container: %w", err)
	}

	// Start the container
	if err := m.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start cloudflared container: %w", err)
	}

	// Update configuration with container ID
	config.TunnelContainerID = resp.ID
	if err := m.store.SaveProxyConfig(ctx, config); err != nil {
		m.logger.Error("Failed to update proxy config with container ID: %v", err)
	}

	m.logger.Info("Started cloudflared container: %s", resp.ID[:12])

	// Wait a moment for the tunnel to connect
	time.Sleep(3 * time.Second)

	return nil
}
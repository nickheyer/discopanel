package metrics

import (
	"context"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
)

type ServerMetrics struct {
	ServerID      string
	CPUPercent    float64
	MemoryUsage   float64 // MB
	DiskUsage     int64   // bytes
	DiskTotal     int64   // bytes
	PlayersOnline int
	TPS           float64
	LastUpdated   time.Time

	// SLP fields
	SLPAvailable    bool
	SLPLatencyMs    int64
	MOTD            string
	ServerVersion   string
	ProtocolVersion int
	PlayerSample    []string
	MaxPlayers      int
	Favicon         string // Base64 PNG (data:image/png;base64,...)
	SLPLastUpdated  time.Time
}

// Configuration for metrics collector
type CollectorConfig struct {
	StatsInterval time.Duration // default 5s
	RCONInterval  time.Duration // default 10s
	DiskInterval  time.Duration // default 60s
	SLPInterval   time.Duration // default 15s
	SLPTimeout    time.Duration // default 5s
	SLPEnabled    bool          // default true
}

// Get default collector configuration
func DefaultConfig() CollectorConfig {
	return CollectorConfig{
		StatsInterval: 5 * time.Second,
		RCONInterval:  10 * time.Second,
		DiskInterval:  60 * time.Second,
		SLPInterval:   15 * time.Second,
		SLPTimeout:    5 * time.Second,
		SLPEnabled:    true,
	}
}

// Collects server metrics in the background
type Collector struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
	log    *logger.Logger

	metrics map[string]*ServerMetrics
	mu      sync.RWMutex

	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup

	collectorConfig CollectorConfig

	eventBus *events.Bus
}

// SetEventBus sets the event bus for publishing metrics events
func (c *Collector) SetEventBus(bus *events.Bus) {
	c.eventBus = bus
}

func (c *Collector) publishMetricsEvent() {
	if c.eventBus != nil {
		c.eventBus.Publish(events.Event{Type: events.EventServerMetrics})
	}
}

// Creates a new metrics collector
func NewCollector(store *storage.Store, docker *docker.Client, cfg *config.Config, log *logger.Logger, collectorCfg ...CollectorConfig) *Collector {
	cc := DefaultConfig()
	if len(collectorCfg) > 0 {
		cc = collectorCfg[0]
	}

	return &Collector{
		store:           store,
		docker:          docker,
		config:          cfg,
		log:             log,
		metrics:         make(map[string]*ServerMetrics),
		collectorConfig: cc,
	}
}

// Start background metrics collection
func (c *Collector) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.stopChan = make(chan struct{})
	c.mu.Unlock()

	c.log.Info("Starting metrics collector")

	// Start collection goroutines
	loopCount := 3
	if c.collectorConfig.SLPEnabled {
		loopCount += 1
	}
	c.wg.Add(loopCount)
	go c.collectDockerStatsLoop()
	go c.collectRCONDataLoop()
	go c.collectDiskUsageLoop()
	if c.collectorConfig.SLPEnabled {
		go c.collectSLPDataLoop()
	}

	return nil
}

// Stop background metrics collection
func (c *Collector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stopChan)
	c.mu.Unlock()

	c.wg.Wait()
	c.log.Info("Metrics collector stopped")
}

// Get metrics for a specific server
func (c *Collector) GetMetrics(serverID string) *ServerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics[serverID]
}

// Gets a copy of all metrics
func (c *Collector) GetAllMetrics() map[string]*ServerMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*ServerMetrics, len(c.metrics))
	maps.Copy(result, c.metrics)
	return result
}

// Collects Docker container stats periodically
func (c *Collector) collectDockerStatsLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.collectorConfig.StatsInterval)
	defer ticker.Stop()

	// Collect immediately on start
	c.collectDockerStats()
	c.publishMetricsEvent()

	for {
		select {
		case <-ticker.C:
			c.collectDockerStats()
			c.publishMetricsEvent()
		case <-c.stopChan:
			return
		}
	}
}

// Collects RCON data (player count, TPS) periodically
func (c *Collector) collectRCONDataLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.collectorConfig.RCONInterval)
	defer ticker.Stop()

	// Collect immediately on start
	c.collectRCONData()

	for {
		select {
		case <-ticker.C:
			c.collectRCONData()
		case <-c.stopChan:
			return
		}
	}
}

// Collects disk usage periodically
func (c *Collector) collectDiskUsageLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.collectorConfig.DiskInterval)
	defer ticker.Stop()

	// Collect immediately on start
	c.collectDiskUsage()

	for {
		select {
		case <-ticker.C:
			c.collectDiskUsage()
		case <-c.stopChan:
			return
		}
	}
}

// Collects CPU and memory stats from Docker
func (c *Collector) collectDockerStats() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	servers, err := c.store.ListServers(ctx)
	if err != nil {
		c.log.Debug("Metrics collector: failed to list servers: %v", err)
		return
	}

	for _, server := range servers {
		if server.ContainerID == "" {
			continue
		}

		// Check if server is running
		status, err := c.docker.GetContainerStatus(ctx, server.ContainerID)
		if err != nil || (status != storage.StatusRunning && status != storage.StatusUnhealthy) {
			continue
		}

		// Get container stats
		stats, err := c.docker.GetContainerStats(ctx, server.ContainerID)
		if err != nil {
			c.log.Debug("Metrics collector: failed to get stats for %s: %v", server.ID, err)
			continue
		}

		c.updateMetrics(server.ID, func(m *ServerMetrics) {
			m.CPUPercent = stats.CPUPercent
			m.MemoryUsage = stats.MemoryUsage
			m.LastUpdated = time.Now()
		})
	}
}

// Collects player count and TPS via RCON
func (c *Collector) collectRCONData() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	servers, err := c.store.ListServers(ctx)
	if err != nil {
		c.log.Debug("Metrics collector: failed to list servers: %v", err)
		return
	}

	for _, server := range servers {
		if server.ContainerID == "" {
			continue
		}

		// Check if server is running
		status, err := c.docker.GetContainerStatus(ctx, server.ContainerID)
		if err != nil || status != storage.StatusRunning {
			continue
		}

		existingMetrics := c.GetMetrics(server.ID)
		slpHasPlayerData := existingMetrics != nil &&
			existingMetrics.SLPAvailable &&
			time.Since(existingMetrics.SLPLastUpdated) < c.collectorConfig.SLPInterval*2

		// Get player count from RCON
		if !slpHasPlayerData {
			output, err := c.docker.ExecCommand(ctx, server.ContainerID, "list")
			if err == nil && output != "" {
				count, _ := minecraft.ParsePlayerListFromOutput(output)
				c.updateMetrics(server.ID, func(m *ServerMetrics) {
					m.PlayersOnline = count
					m.LastUpdated = time.Now()
				})
			}
		}

		// Get TPS if configured
		if server.TPSCommand != "" {
			for _, cmd := range strings.Split(server.TPSCommand, " ?? ") {
				cmd = strings.TrimSpace(cmd)
				if cmd == "" {
					continue
				}
				output, err := c.docker.ExecCommand(ctx, server.ContainerID, cmd)
				if err == nil && output != "" {
					tps := minecraft.ParseTPSFromOutput(output)
					if tps > 0 {
						c.updateMetrics(server.ID, func(m *ServerMetrics) {
							m.TPS = tps
							m.LastUpdated = time.Now()
						})
						break
					}
				}
			}
		}
	}
}

// Collects disk usage for server worlds
func (c *Collector) collectDiskUsage() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	servers, err := c.store.ListServers(ctx)
	if err != nil {
		c.log.Debug("Metrics collector: failed to list servers: %v", err)
		return
	}

	// Get total disk space once
	diskTotal, err := files.GetDiskSpace(c.config.Storage.DataDir)
	if err != nil {
		c.log.Debug("Metrics collector: failed to get disk space: %v", err)
		diskTotal = 0
	}

	for _, server := range servers {
		if server.DataPath == "" {
			continue
		}

		// Calculate world directory size
		worldPath, err := files.FindWorldDir(server.DataPath)
		if err != nil {
			continue
		}

		totalSize, err := files.CalculateDirSize(worldPath)
		if err != nil {
			continue
		}

		c.updateMetrics(server.ID, func(m *ServerMetrics) {
			m.DiskUsage = totalSize
			m.DiskTotal = diskTotal
			m.LastUpdated = time.Now()
		})
	}
}

// Updates metrics for a server
func (c *Collector) updateMetrics(serverID string, update func(*ServerMetrics)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	metrics, exists := c.metrics[serverID]
	if !exists {
		metrics = &ServerMetrics{ServerID: serverID}
		c.metrics[serverID] = metrics
	}
	update(metrics)
}

// Removes metrics for a server on delete
func (c *Collector) RemoveMetrics(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.metrics, serverID)
}

// Collects SLP data
func (c *Collector) collectSLPDataLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.collectorConfig.SLPInterval)
	defer ticker.Stop()

	// Collect on start
	c.collectSLPData()

	for {
		select {
		case <-ticker.C:
			c.collectSLPData()
		case <-c.stopChan:
			return
		}
	}
}

func (c *Collector) collectSLPData() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	servers, err := c.store.ListServers(ctx)
	if err != nil {
		c.log.Debug("Metrics collector SLP: failed to list servers: %v", err)
		return
	}

	slpClient := minecraft.NewSLPClient(c.collectorConfig.SLPTimeout)

	for _, server := range servers {
		if server.ContainerID == "" {
			continue
		}

		// Check if server is running
		status, err := c.docker.GetContainerStatus(ctx, server.ContainerID)
		if err != nil || status != storage.StatusRunning {
			// Mark SLP as unavailable for no op
			c.updateMetrics(server.ID, func(m *ServerMetrics) {
				m.SLPAvailable = false
			})
			continue
		}

		// Get container IP
		containerIP, err := proxy.GetContainerIP(server.ContainerID, c.config.Docker.NetworkName)
		if err != nil {
			c.log.Debug("Metrics collector SLP: failed to get container IP for %s: %v", server.ID, err)
			c.updateMetrics(server.ID, func(m *ServerMetrics) {
				m.SLPAvailable = false
			})
			continue
		}

		// SLP ping w/ server version for protocol
		slpCtx, slpCancel := context.WithTimeout(ctx, c.collectorConfig.SLPTimeout)
		port := server.Port
		if port == 0 {
			port = 25565
		}
		result, err := slpClient.Ping(slpCtx, containerIP, port, server.MCVersion)
		slpCancel()

		if err != nil {
			c.log.Debug("Metrics collector SLP: failed to ping %s (%s:%d): %v", server.ID, containerIP, port, err)
			c.updateMetrics(server.ID, func(m *ServerMetrics) {
				m.SLPAvailable = false
			})
			continue
		}

		// Update
		c.updateMetrics(server.ID, func(m *ServerMetrics) {
			m.SLPAvailable = true
			m.SLPLatencyMs = result.LatencyMs
			m.MOTD = result.MOTD
			m.ServerVersion = result.Version.Name
			m.ProtocolVersion = result.Version.Protocol
			m.PlayerSample = result.PlayerNames
			m.MaxPlayers = result.Players.Max
			m.PlayersOnline = result.Players.Online
			m.Favicon = result.Favicon
			m.SLPLastUpdated = time.Now()
			m.LastUpdated = time.Now()
		})
	}
}

package metrics

import (
	"context"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/command"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

type ServerMetrics struct {
	ServerID      string
	CPUPercent    float64
	MemoryUsage   float64 // MB
	DiskUsage     int64   // bytes (total server data)
	DiskTotal     int64   // bytes
	WorldSize     int64   // bytes (world directory only)
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

// Snapshot of a servers derived lifecycle state
type lifecycleState struct {
	healthy bool            // last observed docker health (StatusRunning)
	players map[string]bool // set of online player names - nil until first sampled
}

// Collects server metrics in the background
type Collector struct {
	store  *storage.Store
	docker *docker.Client
	sender *command.Sender
	config *config.Config
	log    *logger.Logger
	bus    *events.Bus

	metrics map[string]*ServerMetrics
	mu      sync.RWMutex

	// Per-server lifecycle state for event derivation
	lifecycle   map[string]lifecycleState
	lifecycleMu sync.Mutex

	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup

	collectorConfig CollectorConfig
}

// Creates a new metrics collector
func NewCollector(store *storage.Store, docker *docker.Client, sender *command.Sender, cfg *config.Config, bus *events.Bus, log *logger.Logger, collectorCfg ...CollectorConfig) *Collector {
	cc := DefaultConfig()
	if len(collectorCfg) > 0 {
		cc = collectorCfg[0]
	}

	return &Collector{
		store:           store,
		docker:          docker,
		sender:          sender,
		config:          cfg,
		bus:             bus,
		log:             log,
		metrics:         make(map[string]*ServerMetrics),
		lifecycle:       make(map[string]lifecycleState),
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
	loopCount := 4 // stats, rcon, disk, lifecycle-events
	if c.collectorConfig.SLPEnabled {
		loopCount += 1
	}
	c.wg.Add(loopCount)
	go c.collectDockerStatsLoop()
	go c.collectRCONDataLoop()
	go c.collectDiskUsageLoop()
	go c.collectLifecycleEventsLoop()
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

	for {
		select {
		case <-ticker.C:
			c.collectDockerStats()
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

		// Get player count and roster from RCON
		if !slpHasPlayerData {
			output, err := c.sender.SendCommand(ctx, server.ID, "list")
			if err == nil && output != "" {
				count, players := minecraft.ParsePlayerListFromOutput(output)
				c.updateMetrics(server.ID, func(m *ServerMetrics) {
					m.PlayersOnline = count
					m.PlayerSample = players
					m.LastUpdated = time.Now()
				})
			}
		}

		// Get TPS if configured
		if server.TPSEnabled && server.TPSCommand != "" {
			if server.TPSExtractionMode == storage.TPSExtractionModeLegacy {
				c.legacyTpsExtraction(ctx, server)
			} else {
				c.tpsExtraction(ctx, server)
			}

		}
	}
}

func (c *Collector) tpsExtraction(ctx context.Context, server *storage.Server) {
	command, _, _ := strings.Cut(server.TPSCommand, " ?? ")
	if command == "" {
		return
	}

	output, err := c.sender.SendCommand(ctx, server.ID, command)
	if err != nil {
		return
	}

	var tps float64
	switch server.TPSExtractionMode {
	case storage.TPSExtractionModeVanilla:
		tps = minecraft.ParseTPSVanilla(output)
	case storage.TPSExtractionModeForge:
		tps = minecraft.ParseTPSForge(output)
	case storage.TPSExtractionModeSpigot:
		tps = minecraft.ParseTPSSpigot(output)
	case storage.TPSExtractionModeCustom:
		tps = minecraft.ParseTPSCustom(output, server.TPSCustomRegex)
	}

	if tps > 0 {
		c.updateMetrics(server.ID, func(m *ServerMetrics) {
			m.TPS = tps
			m.LastUpdated = time.Now()
		})
	}

}

func (c *Collector) legacyTpsExtraction(ctx context.Context, server *storage.Server) {
	for cmd := range strings.SplitSeq(server.TPSCommand, " ?? ") {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		output, err := c.sender.SendCommand(ctx, server.ID, cmd)
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

		totalSize, err := files.CalculateDirSize(server.DataPath)
		if err != nil {
			continue
		}

		// Calculate world directory size, including dimension worlds
		worldPaths, err := files.FindWorldDirs(server.DataPath)
		if err != nil {
			continue
		}

		var totalWorldSize int64
		for _, worldPath := range worldPaths {
			size, err := files.CalculateDirSize(worldPath)
			if err != nil {
				continue
			}
			totalWorldSize += size
		}

		c.updateMetrics(server.ID, func(m *ServerMetrics) {
			m.DiskUsage = totalSize
			m.DiskTotal = diskTotal
			m.WorldSize = totalWorldSize
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
	delete(c.metrics, serverID)
	c.mu.Unlock()
	c.clearLifecycle(serverID)
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
		if server.ProxyHostname != "" || port == 0 {
			port = docker.DefaultMinecraftPort // Proxy listens on default port (inside container)
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

// Derives lifecycle events (SERVER_HEALTHY, PLAYER_JOIN, PLAYER_LEAVE) from state and emits on event bus
func (c *Collector) collectLifecycleEventsLoop() {
	defer c.wg.Done()

	interval := c.collectorConfig.RCONInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Seed baselines on start so already-running servers don't emit on boot
	c.detectLifecycleEvents()

	for {
		select {
		case <-ticker.C:
			c.detectLifecycleEvents()
		case <-c.stopChan:
			return
		}
	}
}

// Compares each servers current health/player state against the previous state
func (c *Collector) detectLifecycleEvents() {
	if c.bus == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	servers, err := c.store.ListServers(ctx)
	if err != nil {
		c.log.Debug("Metrics collector lifecycle: failed to list servers: %v", err)
		return
	}

	for _, server := range servers {
		if server.ContainerID == "" {
			c.clearLifecycle(server.ID)
			continue
		}

		status, err := c.docker.GetContainerStatus(ctx, server.ContainerID)
		if err != nil {
			c.clearLifecycle(server.ID)
			continue
		}

		// Container that is fully down forgets its baseline so a later restart reseeds clean
		alive := status == storage.StatusRunning || status == storage.StatusUnhealthy || status == storage.StatusStarting
		if !alive {
			c.clearLifecycle(server.ID)
			continue
		}

		// "Healthy" == docker health check passing (StatusRunning)
		healthy := status == storage.StatusRunning

		prev, seen := c.getLifecycle(server.ID)
		if !seen {
			// First sighting while alive - establish baseline
			c.setLifecycle(server.ID, lifecycleState{
				healthy: healthy,
				players: c.currentRoster(server.ID),
			})
			continue
		}

		next := prev

		// Health transition - not-healthy -> healthy (initial pass or recovery)
		if healthy && !prev.healthy {
			c.emit(ctx, v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_HEALTHY, server.ID, nil)
		}
		next.healthy = healthy

		// Player join/leave - diff the current roster
		if healthy {
			if roster := c.currentRoster(server.ID); roster != nil {
				if prev.players != nil {
					for name := range roster {
						if !prev.players[name] {
							c.emit(ctx, v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN, server.ID, map[string]any{"player": name})
						}
					}
					for name := range prev.players {
						if !roster[name] {
							c.emit(ctx, v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_LEAVE, server.ID, map[string]any{"player": name})
						}
					}
				}
				next.players = roster
			}
			// roster == nil means the name set is momentarily unknown
		}

		c.setLifecycle(server.ID, next)
	}
}

// Emits a derived lifecycle event on the bus, optionally carrying event data
func (c *Collector) emit(ctx context.Context, t v1.TriggeredEventType, serverID string, data map[string]any) {
	if c.bus == nil {
		return
	}
	c.bus.Emit(ctx, events.Event{Type: t, ServerID: serverID, Data: data})
}

// Returns the set of online player names for a server from latest cached metrics
func (c *Collector) currentRoster(serverID string) map[string]bool {
	m := c.GetMetrics(serverID)
	if m == nil {
		return nil
	}
	if len(m.PlayerSample) < m.PlayersOnline {
		return nil
	}
	set := make(map[string]bool, len(m.PlayerSample))
	for _, name := range m.PlayerSample {
		if name != "" {
			set[name] = true
		}
	}
	return set
}

func (c *Collector) getLifecycle(serverID string) (lifecycleState, bool) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	st, ok := c.lifecycle[serverID]
	return st, ok
}

func (c *Collector) setLifecycle(serverID string, st lifecycleState) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.lifecycle[serverID] = st
}

func (c *Collector) clearLifecycle(serverID string) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	delete(c.lifecycle, serverID)
}

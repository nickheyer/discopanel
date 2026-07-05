package metrics

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

type ServerMetrics struct {
	ServerID      string
	CPUPercent    float64
	CPUCount      int
	MemoryUsage   float64 // MB
	DiskUsage     int64   // bytes (total server data)
	DiskTotal     int64   // bytes (volume total)
	DiskUsed      int64   // bytes (volume used, all data)
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

	// Agent-sourced fields (live only while the runtime agent session is up).
	// When fresh, these take precedence over the SLP sampling paths.
	AgentConnected     bool
	AgentJvmActive     bool      // javaagent is feeding JVM/tick telemetry
	AgentReady         bool      // agent reported server ready this run
	AgentTickUpdated   time.Time // freshness of TPS/MSPT below
	AgentRosterUpdated time.Time // freshness of PlayerSample/PlayersOnline
	MSPT               float64   // mean ms per tick
	MSPTMax            float64   // worst tick in the sample window
	HeapUsedMB         float64
	HeapMaxMB          float64
	ThreadCount        int
	CPUQuotaCores      float64 // cgroup CPU quota (0 = unlimited)
	CPUThrottlePercent float64 // share of CFS periods throttled (0-100)
	GCPauseCount       int64   // pauses in the last sample window
	GCPauseTotalMs     float64
	GCPauseMaxMs       float64
	StartupSeconds     float64

	// Last process exit reported by the agent (crash forensics).
	LastExitCode        int
	LastExitCrashed     bool
	LastCrashReportPath string
	LastCrashExcerpt    string
	LastExitedAt        time.Time
}

// agentRosterFresh reports whether the agent-maintained roster is recent
// enough to be authoritative over SLP samples.
func (m *ServerMetrics) agentRosterFresh() bool {
	return m != nil && m.AgentConnected && time.Since(m.AgentRosterUpdated) < 90*time.Second
}

// agentHealthProof reports whether a live agent session vouches for the
// server, so blocked SLP pings do not read as unhealthy.
func (m *ServerMetrics) agentHealthProof() bool {
	return m != nil && m.AgentConnected && m.AgentReady
}

// Configuration for metrics collector
type CollectorConfig struct {
	StatsInterval   time.Duration // default 5s
	EventsInterval  time.Duration // default 10s
	DiskInterval    time.Duration // default 60s
	SLPInterval     time.Duration // default 15s
	SLPTimeout      time.Duration // default 5s
	SLPEnabled      bool          // default true
	HistoryInterval time.Duration // default 30s

	// Panel-side health (SLP is the health source; replaces in-container checks)
	HealthStartupGrace  time.Duration // starting -> unhealthy after this without a first ping (default 15m)
	HealthFailThreshold int           // healthy -> unhealthy after this many consecutive failed pings (default 3)
}

// Get default collector configuration
func DefaultConfig() CollectorConfig {
	return CollectorConfig{
		StatsInterval:       5 * time.Second,
		EventsInterval:      10 * time.Second,
		DiskInterval:        60 * time.Second,
		SLPInterval:         15 * time.Second,
		SLPTimeout:          5 * time.Second,
		SLPEnabled:          true,
		HistoryInterval:     30 * time.Second,
		HealthStartupGrace:  15 * time.Minute,
		HealthFailThreshold: 3,
	}
}

// Fills unset config fields with their defaults
func (cc CollectorConfig) withDefaults() CollectorConfig {
	def := DefaultConfig()
	if cc.StatsInterval <= 0 {
		cc.StatsInterval = def.StatsInterval
	}
	if cc.EventsInterval <= 0 {
		cc.EventsInterval = def.EventsInterval
	}
	if cc.DiskInterval <= 0 {
		cc.DiskInterval = def.DiskInterval
	}
	if cc.SLPInterval <= 0 {
		cc.SLPInterval = def.SLPInterval
	}
	if cc.SLPTimeout <= 0 {
		cc.SLPTimeout = def.SLPTimeout
	}
	if cc.HistoryInterval <= 0 {
		cc.HistoryInterval = def.HistoryInterval
	}
	if cc.HealthStartupGrace <= 0 {
		cc.HealthStartupGrace = def.HealthStartupGrace
	}
	if cc.HealthFailThreshold <= 0 {
		cc.HealthFailThreshold = def.HealthFailThreshold
	}
	return cc
}

// containerHealth tracks SLP results for one container run.
type containerHealth struct {
	startedAt        time.Time
	everHealthy      bool
	consecutiveFails int
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
	config *config.Config
	log    *logger.Logger
	bus    *events.Bus

	metrics map[string]*ServerMetrics
	mu      sync.RWMutex

	// Per-server lifecycle state for event derivation
	lifecycle   map[string]lifecycleState
	lifecycleMu sync.Mutex

	// Per-container SLP health records (the panel-side health source)
	health   map[string]*containerHealth
	healthMu sync.Mutex

	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup

	collectorConfig CollectorConfig
}

// Creates a new metrics collector
func NewCollector(store *storage.Store, docker *docker.Client, cfg *config.Config, bus *events.Bus, log *logger.Logger, collectorCfg CollectorConfig) *Collector {
	cc := collectorCfg.withDefaults()

	return &Collector{
		store:           store,
		docker:          docker,
		config:          cfg,
		bus:             bus,
		log:             log,
		metrics:         make(map[string]*ServerMetrics),
		lifecycle:       make(map[string]lifecycleState),
		health:          make(map[string]*containerHealth),
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

	// Disk samples refresh on start and stop, not just the ticker
	if c.bus != nil {
		c.bus.Subscribe(c.onLifecycleEvent)
	}

	// Start collection goroutines
	loopCount := 4 // stats, disk, lifecycle-events, history
	if c.collectorConfig.SLPEnabled {
		loopCount += 1
	}
	c.wg.Add(loopCount)
	go c.collectDockerStatsLoop()
	go c.collectDiskUsageLoop()
	go c.collectLifecycleEventsLoop()
	go c.collectHistoryLoop()
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
		if err != nil || (status != storage.StatusRunning && status != storage.StatusUnhealthy && status != storage.StatusStarting) {
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
			m.CPUCount = stats.CPUCount
			m.MemoryUsage = stats.MemoryUsage
			m.LastUpdated = time.Now()
		})
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

	for _, server := range servers {
		c.sampleDiskUsage(server.ID, server.DataPath)
	}
}

// Samples one server data dir and the volume it lives on
func (c *Collector) sampleDiskUsage(serverID, dataPath string) {
	if dataPath == "" {
		return
	}

	diskTotal, diskUsed, err := files.GetDiskSpace(dataPath)
	if err != nil {
		c.log.Debug("Metrics collector: failed to get disk space: %v", err)
		diskTotal, diskUsed = 0, 0
	}

	totalSize, err := files.CalculateDirSize(dataPath)
	if err != nil {
		return
	}

	// A missing world means zero, other data still counts
	var totalWorldSize int64
	if worldPaths, err := files.FindWorldDirs(dataPath); err == nil {
		for _, worldPath := range worldPaths {
			size, err := files.CalculateDirSize(worldPath)
			if err != nil {
				continue
			}
			totalWorldSize += size
		}
	}

	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.DiskUsage = totalSize
		m.DiskTotal = diskTotal
		m.DiskUsed = diskUsed
		m.WorldSize = totalWorldSize
		m.LastUpdated = time.Now()
	})
}

// Refreshes disk usage promptly after provisioning and world saves
func (c *Collector) onLifecycleEvent(ctx context.Context, e events.Event) {
	switch e.Type {
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP,
		v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART:
	default:
		return
	}
	go func() {
		lookupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server, err := c.store.GetServer(lookupCtx, e.ServerID)
		if err != nil {
			return
		}
		c.sampleDiskUsage(server.ID, server.DataPath)
	}()
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

// recordHealth folds an SLP result into the container's health record.
func (c *Collector) recordHealth(containerID string, startedAt time.Time, ok bool) {
	c.healthMu.Lock()
	defer c.healthMu.Unlock()

	h := c.health[containerID]
	// A different StartedAt means the container restarted: reset the record.
	if h == nil || !h.startedAt.Equal(startedAt) {
		h = &containerHealth{startedAt: startedAt}
		c.health[containerID] = h
	}
	if ok {
		h.everHealthy = true
		h.consecutiveFails = 0
	} else {
		h.consecutiveFails++
	}
}

func (c *Collector) clearHealth(containerID string) {
	c.healthMu.Lock()
	delete(c.health, containerID)
	c.healthMu.Unlock()
}

// ContainerHealth implements docker.HealthChecker: the SLP ping record is the
// panel-side health verdict for running containers.
func (c *Collector) ContainerHealth(containerID string, startedAt time.Time) docker.HealthState {
	c.healthMu.Lock()
	h := c.health[containerID]
	var record containerHealth
	if h != nil {
		record = *h
	}
	c.healthMu.Unlock()

	grace := c.collectorConfig.HealthStartupGrace
	threshold := c.collectorConfig.HealthFailThreshold

	// No record for this run yet (collector hasn't pinged since (re)start).
	if h == nil || !record.startedAt.Equal(startedAt) {
		if time.Since(startedAt) < grace {
			return docker.HealthStarting
		}
		// Long-running container with no data (e.g. panel restart): assume
		// healthy until pings prove otherwise.
		return docker.HealthUnknown
	}

	if record.everHealthy {
		if record.consecutiveFails >= threshold {
			return docker.HealthUnhealthy
		}
		return docker.HealthHealthy
	}

	// Never answered a ping this run.
	if time.Since(startedAt) >= grace {
		return docker.HealthUnhealthy
	}
	return docker.HealthStarting
}

// PlayersOnline implements lifecycle.PlayerCounter from the agent roster
// when live, else fresh SLP data.
func (c *Collector) PlayersOnline(serverID string) (int, bool) {
	m := c.GetMetrics(serverID)
	if m == nil {
		return 0, false
	}
	if m.agentRosterFresh() {
		return m.PlayersOnline, true
	}
	if m.SLPAvailable && time.Since(m.SLPLastUpdated) < 2*c.collectorConfig.SLPInterval {
		return m.PlayersOnline, true
	}
	return 0, false
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

		// Ping any container whose raw state is running (health derives from
		// these pings, so this must not consult GetContainerStatus).
		info, err := c.docker.GetContainerRunInfo(ctx, server.ContainerID)
		if err != nil || !info.Running || info.Paused {
			c.clearHealth(server.ContainerID)
			c.updateMetrics(server.ID, func(m *ServerMetrics) {
				m.SLPAvailable = false
			})
			continue
		}

		// Get container IP
		containerIP, err := c.docker.ContainerIP(ctx, server.ContainerID)
		if err != nil {
			c.log.Debug("Metrics collector SLP: failed to get container IP for %s: %v", server.ID, err)
			// A live ready agent session vouches when SLP cannot
			c.recordHealth(server.ContainerID, info.StartedAt, c.GetMetrics(server.ID).agentHealthProof())
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
			// SLP blocked by a mod must not read unhealthy while the agent vouches
			c.recordHealth(server.ContainerID, info.StartedAt, c.GetMetrics(server.ID).agentHealthProof())
			c.updateMetrics(server.ID, func(m *ServerMetrics) {
				m.SLPAvailable = false
			})
			continue
		}

		c.recordHealth(server.ContainerID, info.StartedAt, true)

		// Update (the agent roster, when fresh, is authoritative: SLP samples
		// can truncate and must not clobber it)
		c.updateMetrics(server.ID, func(m *ServerMetrics) {
			m.SLPAvailable = true
			m.SLPLatencyMs = result.LatencyMs
			m.MOTD = result.MOTD
			m.ServerVersion = result.Version.Name
			m.ProtocolVersion = result.Version.Protocol
			if !m.agentRosterFresh() {
				m.PlayerSample = result.PlayerNames
				m.PlayersOnline = result.Players.Online
			}
			m.MaxPlayers = result.Players.Max
			m.Favicon = result.Favicon
			m.SLPLastUpdated = time.Now()
			m.LastUpdated = time.Now()
		})
	}
}

// Derives lifecycle events (SERVER_HEALTHY, PLAYER_JOIN, PLAYER_LEAVE) from state and emits on event bus
func (c *Collector) collectLifecycleEventsLoop() {
	defer c.wg.Done()

	interval := c.collectorConfig.EventsInterval
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

		// Player join/leave - diff the current roster. Agent-connected servers
		// get exact join/leave events pushed by the hub instead; diffing the
		// scraped roster too would double-emit.
		if healthy && !c.GetMetrics(server.ID).agentRosterFresh() {
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

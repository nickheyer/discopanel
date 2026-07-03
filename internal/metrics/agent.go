package metrics

import (
	"context"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// This file is the collector's intake for agent-pushed telemetry (see
// internal/agent). Agent data lands in the same ServerMetrics store the
// SLP/RCON scraping loops feed, and while fresh it takes precedence there.

// SetAgentConnected records session attach/detach for a server.
func (c *Collector) SetAgentConnected(serverID string, connected bool) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.AgentConnected = connected
		if !connected {
			m.AgentModActive = false
		}
		m.LastUpdated = time.Now()
	})
}

// SetAgentModActive records that the disco-agent mod is feeding game telemetry.
func (c *Collector) SetAgentModActive(serverID string, active bool) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.AgentModActive = active
	})
}

// ApplyAgentReady folds the agent's ready signal into the health record: a
// ready report is proof of life, so servers whose SLP is blocked (or that
// beat the first SLP poll) go healthy immediately.
func (c *Collector) ApplyAgentReady(ctx context.Context, serverID string, startupSeconds float64) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		if startupSeconds > 0 {
			m.StartupSeconds = startupSeconds
		}
		m.LastUpdated = time.Now()
	})

	server, err := c.store.GetServer(ctx, serverID)
	if err != nil || server.ContainerID == "" {
		return
	}
	info, err := c.docker.GetContainerRunInfo(ctx, server.ContainerID)
	if err != nil || !info.Running {
		return
	}
	c.recordHealth(server.ContainerID, info.StartedAt, true)
}

// ApplyAgentExit records the process exit report for crash forensics.
func (c *Collector) ApplyAgentExit(serverID string, exitCode int, crashed bool, reportPath, excerpt string) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.LastExitCode = exitCode
		m.LastExitCrashed = crashed
		m.LastCrashReportPath = reportPath
		m.LastCrashExcerpt = excerpt
		m.LastExitedAt = time.Now()
	})
}

// ApplyAgentTick stores mod-sourced tick timing (authoritative TPS/MSPT).
func (c *Collector) ApplyAgentTick(serverID string, sample *agentv1.TickSample) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.TPS = sample.GetTps()
		m.MSPT = sample.GetMsptAvg()
		m.MSPTMax = sample.GetMsptMax()
		m.AgentTickUpdated = time.Now()
		m.LastUpdated = time.Now()
	})
}

// ApplyAgentJvm stores mod-sourced in-process JVM telemetry.
func (c *Collector) ApplyAgentJvm(serverID string, sample *agentv1.JvmSample) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.HeapUsedMB = sample.GetHeapUsedMb()
		m.HeapMaxMB = sample.GetHeapMaxMb()
		m.ThreadCount = int(sample.GetThreadCount())
		if gc := sample.GetGc(); gc != nil {
			m.GCPauseCount = gc.GetCount()
			m.GCPauseTotalMs = gc.GetTotalMs()
			m.GCPauseMaxMs = gc.GetMaxMs()
		}
		m.LastUpdated = time.Now()
	})
}

// ApplyAgentProc stores supervisor-sourced cgroup/GC-log telemetry. CPU and
// memory percentages keep coming from docker stats (whole-container view);
// this adds what docker stats cannot see: CFS throttling, the CPU quota, and
// GC pauses on servers without the mod.
func (c *Collector) ApplyAgentProc(serverID string, sample *agentv1.ProcSample) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.CPUQuotaCores = sample.GetCpuQuotaCores()
		if periods := sample.GetCfsPeriods(); periods > 0 {
			m.CPUThrottlePercent = float64(sample.GetCfsThrottledPeriods()) / float64(periods) * 100
		} else {
			m.CPUThrottlePercent = 0
		}
		// The in-process JVM sample is the better GC source when present.
		if gc := sample.GetGc(); gc != nil && !m.AgentModActive {
			m.GCPauseCount = gc.GetCount()
			m.GCPauseTotalMs = gc.GetTotalMs()
			m.GCPauseMaxMs = gc.GetMaxMs()
		}
		m.LastUpdated = time.Now()
	})
}

// ApplyAgentPlayerChange applies one exact join/leave event to the roster.
func (c *Collector) ApplyAgentPlayerChange(serverID, player string, joined bool, playersOnline int) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		roster := make([]string, 0, len(m.PlayerSample)+1)
		for _, name := range m.PlayerSample {
			if name != player {
				roster = append(roster, name)
			}
		}
		if joined {
			roster = append(roster, player)
		}
		m.PlayerSample = roster
		if playersOnline >= 0 {
			m.PlayersOnline = playersOnline
		} else {
			m.PlayersOnline = len(roster)
		}
		m.AgentRosterUpdated = time.Now()
		m.LastUpdated = time.Now()
	})
}

// ApplyAgentWorldStats stores world state and the authoritative roster.
func (c *Collector) ApplyAgentWorldStats(serverID string, stats *agentv1.WorldStats) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		entities, chunks := 0, 0
		for _, d := range stats.GetDimensions() {
			entities += int(d.GetEntities())
			chunks += int(d.GetChunks())
		}
		m.TotalEntities = entities
		m.TotalChunks = chunks
		m.PlayerSample = stats.GetOnlinePlayers()
		m.PlayersOnline = len(stats.GetOnlinePlayers())
		m.AgentRosterUpdated = time.Now()
		m.LastUpdated = time.Now()
	})
}

// ApplyAgentCommands stores the server's command names for console autocomplete.
func (c *Collector) ApplyAgentCommands(serverID string, commands []string) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.AvailableCommands = commands
	})
}

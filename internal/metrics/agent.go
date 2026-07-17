package metrics

import (
	"context"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

// Collector intake for agent-pushed telemetry, takes precedence when fresh

// Records session attach or detach for a server
func (c *Collector) SetAgentConnected(serverID string, connected bool) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.AgentConnected = connected
		if connected {
			m.LastAgentSessionAt = time.Now()
		}
		if !connected {
			m.AgentJvmActive = false
			m.AgentReady = false
			m.PSIAvailable = false
			m.GCLogWindowAt = time.Time{}
		}
		m.LastUpdated = time.Now()
	})
}

// Records that the javaagent is feeding JVM telemetry
func (c *Collector) SetAgentJvmActive(serverID string, active bool) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.AgentJvmActive = active
	})
}

// Marks server healthy on agent ready signal
func (c *Collector) ApplyAgentReady(ctx context.Context, serverID string, startupSeconds float64) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.AgentReady = true
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

// Crash exits older than this stop counting toward loops
const crashExitRetention = 30 * time.Minute

// Records a process exit report, reports false for stale replays
func (c *Collector) ApplyAgentExit(serverID string, exit *agentv1.Exited) bool {
	exitedAt := time.Now()
	if ms := exit.GetExitedAtUnixMs(); ms > 0 {
		exitedAt = time.UnixMilli(ms)
	}
	fresh := false
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		if !m.LastExitedAt.IsZero() && !exitedAt.After(m.LastExitedAt) {
			return
		}
		fresh = true
		m.LastExitCode = int(exit.GetExitCode())
		m.LastExitCrashed = exit.GetCrashed()
		m.LastExitOomKilled = exit.GetOomKilled()
		m.LastExitBootFailed = exit.GetBootFailed()
		m.LastExitWasReady = exit.GetWasReady()
		m.LastCrashReportPath = exit.GetCrashReportPath()
		m.LastCrashExcerpt = exit.GetCrashReportExcerpt()
		m.LastFatalError = exit.GetFatalError()
		m.LastExitedAt = exitedAt
		if exit.GetCrashed() {
			m.CrashExits = pruneCrashExits(append(m.CrashExits, exitedAt))
		}
	})
	return fresh
}

// Drops crash times outside the retention window
func pruneCrashExits(times []time.Time) []time.Time {
	cutoff := time.Now().Add(-crashExitRetention)
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}

// Runtime errors older than this stop counting toward findings
const runtimeFatalRetention = time.Hour

// Ring cap keeps a spamming mod from growing memory
const maxRuntimeFatals = 128

// Records one post-ready error for runtime findings
func (c *Collector) RecordRuntimeFatal(serverID string, fatal *agentv1.FatalError) {
	if fatal == nil {
		return
	}
	now := time.Now()
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		kept := m.RuntimeFatals[:0]
		for _, f := range m.RuntimeFatals {
			if now.Sub(f.At) < runtimeFatalRetention {
				kept = append(kept, f)
			}
		}
		if len(kept) >= maxRuntimeFatals {
			kept = kept[len(kept)-maxRuntimeFatals+1:]
		}
		m.RuntimeFatals = append(kept, RuntimeFatal{At: now, Fatal: fatal})
	})
}

// Records a clean exit nobody requested for loop breaking
func (c *Collector) RecordUnexpectedExit(serverID string, exitedAt time.Time) {
	if exitedAt.IsZero() {
		exitedAt = time.Now()
	}
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.UnexpectedExits = pruneCrashExits(append(m.UnexpectedExits, exitedAt))
	})
}

// Marks that the panel stopped a crash-looping server
func (c *Collector) MarkCrashLoopStopped(serverID string) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.CrashLoopStoppedAt = time.Now()
	})
}

// Stores javaagent tick timing, authoritative TPS and MSPT
func (c *Collector) ApplyAgentTick(serverID string, sample *agentv1.TickSample) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.TPS = sample.GetTps()
		m.MSPT = sample.GetMsptAvg()
		m.MSPTMax = sample.GetMsptMax()
		m.AgentTickUpdated = time.Now()
		m.LastUpdated = time.Now()
	})
}

// Log windows this fresh keep MX bean GC deltas out
const gcLogWindowFresh = 45 * time.Second

// Stores JVM telemetry from the javaagent
func (c *Collector) ApplyAgentJvm(serverID string, sample *agentv1.JvmSample) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.HeapUsedMB = sample.GetHeapUsedMb()
		m.HeapMaxMB = sample.GetHeapMaxMb()
		m.ThreadCount = int(sample.GetThreadCount())
		m.ClassCount = int(sample.GetClassCount())
		// MX bean GC only speaks while no gc.log window flows
		if gc := sample.GetGc(); gc != nil && time.Since(m.GCLogWindowAt) > gcLogWindowFresh {
			m.GCPauseCount = gc.GetCount()
			m.GCPauseTotalMs = gc.GetTotalMs()
			m.GCPauseMaxMs = gc.GetMaxMs()
		}
		m.LastUpdated = time.Now()
	})
}

// Stores cgroup and GC-log telemetry docker stats cannot see
func (c *Collector) ApplyAgentProc(serverID string, sample *agentv1.ProcSample) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		// Java process attribution beats whole-container docker stats
		m.CPUPercent = sample.GetCpuPercent()
		if rss := sample.GetRssMb(); rss > 0 {
			m.MemoryUsage = rss
		}
		m.AgentProcUpdated = time.Now()
		m.CPUQuotaCores = sample.GetCpuQuotaCores()
		if periods := sample.GetCfsPeriods(); periods > 0 {
			m.CPUThrottlePercent = float64(sample.GetCfsThrottledPeriods()) / float64(periods) * 100
		} else {
			m.CPUThrottlePercent = 0
		}
		if gc := sample.GetGc(); gc != nil {
			m.GCPauseCount = gc.GetCount()
			m.GCPauseTotalMs = gc.GetTotalMs()
			m.GCPauseMaxMs = gc.GetMaxMs()
			m.GCLogWindowAt = time.Now()
		}
		if psi := sample.GetPsi(); psi != nil {
			m.PSIAvailable = true
			m.PSICpuSome = psi.GetCpuSomeAvg10()
			m.PSIMemSome = psi.GetMemSomeAvg10()
			m.PSIMemFull = psi.GetMemFullAvg10()
			m.PSIIoSome = psi.GetIoSomeAvg10()
		} else {
			m.PSIAvailable = false
		}
		m.LastUpdated = time.Now()
	})
}

// Stores static host facts from the runtime hello
func (c *Collector) ApplyAgentHello(serverID string, hello *agentv1.Hello) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		if mode := hello.GetHostThpMode(); mode != "" {
			m.HostTHPMode = mode
		}
	})
}

// Applies one exact join or leave event to roster
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

// Stores the authoritative supervisor-tracked player list
func (c *Collector) ApplyAgentRoster(serverID string, players []string) {
	c.updateMetrics(serverID, func(m *ServerMetrics) {
		m.PlayerSample = players
		m.PlayersOnline = len(players)
		m.AgentRosterUpdated = time.Now()
		m.LastUpdated = time.Now()
	})
}

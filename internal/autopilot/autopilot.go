// Package autopilot turns runtime telemetry and server configuration into a
// performance report with plain-language findings and one-click fixes.
// It is pure analysis: reading happens over the metrics collector's cached
// state, fixes mutate a ServerConfig for the caller to persist.
package autopilot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Fix IDs offered by findings and accepted by ApplyFix.
const (
	FixEnableAutoMemory = "enable_auto_memory"
	FixEnableZGC        = "enable_zgc"
	FixKeepAikarFlags   = "keep_aikar_flags"
)

// Finding is one check result in plain language.
type Finding struct {
	ID       string
	Severity v1.PerformanceSeverity
	Title    string
	Detail   string
	FixID    string
	FixLabel string
}

// Analyze runs every check against the server's configuration and its latest
// telemetry. m may be nil (server never sampled); config checks still run.
func Analyze(server *storage.Server, cfg *storage.ServerConfig, m *metrics.ServerMetrics) []Finding {
	var findings []Finding
	javaMajor, _ := strconv.Atoi(server.JavaVersion)

	findings = append(findings, checkHeapVsLimit(server, cfg)...)
	findings = append(findings, checkFlagConflict(cfg)...)
	findings = append(findings, checkGCChoice(cfg, javaMajor)...)

	if m != nil {
		findings = append(findings, checkThrottling(m)...)
		findings = append(findings, checkGCPressure(cfg, m, javaMajor)...)
		findings = append(findings, checkTickHealth(m)...)
		findings = append(findings, checkHeapPressure(m)...)
		findings = append(findings, checkCrash(m)...)
		if isRunning(server) && !m.AgentConnected {
			findings = append(findings, Finding{
				ID:       "agent_offline",
				Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
				Title:    "Limited telemetry",
				Detail:   "The DiscoPanel agent is not connected, so tick timing, GC pauses, and CPU throttling cannot be measured. Metrics fall back to slower RCON and ping sampling.",
			})
		}
	}

	if len(findings) == 0 {
		findings = append(findings, Finding{
			ID:       "all_good",
			Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_OK,
			Title:    "Looking good",
			Detail:   "No performance problems detected.",
		})
	}
	return findings
}

// ApplyFix mutates cfg according to a finding's fix. The caller persists the
// config; every fix takes effect on the next server start.
func ApplyFix(cfg *storage.ServerConfig, fixID string) (string, error) {
	t, f := true, false
	switch fixID {
	case FixEnableAutoMemory:
		cfg.AutoMemory = &t
		return "Automatic memory enabled: the Java heap is now sized from the container memory limit.", nil
	case FixEnableZGC:
		cfg.UseZGCFlags = &t
		cfg.UseAikarFlags = &f
		cfg.UseMeowiceFlags = &f
		return "ZGC enabled: the server will use generational ZGC on its next start.", nil
	case FixKeepAikarFlags:
		cfg.UseMeowiceFlags = &f
		return "MeowIce flags disabled: Aikar's flags apply on the next start.", nil
	default:
		return "", fmt.Errorf("unknown fix %q", fixID)
	}
}

// reserveMB is the container memory the JVM needs beyond the heap (metaspace,
// code cache, thread stacks, direct buffers).
func reserveMB(containerMB int) int {
	fifth := containerMB / 5
	if fifth < 512 {
		return 512
	}
	return fifth
}

func checkHeapVsLimit(server *storage.Server, cfg *storage.ServerConfig) []Finding {
	if cfg == nil || server.Memory <= 0 {
		return nil
	}
	if boolVal(cfg.AutoMemory) {
		return nil
	}
	xmx := configuredHeapMB(cfg)
	if xmx <= 0 {
		return nil
	}
	allowed := server.Memory - reserveMB(server.Memory)
	if xmx <= allowed {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	title := "Heap leaves too little headroom"
	if xmx >= server.Memory {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
		title = "Heap exceeds the container memory limit"
	}
	return []Finding{{
		ID:       "heap_headroom",
		Severity: severity,
		Title:    title,
		Detail: fmt.Sprintf(
			"The Java heap is set to %d MB but the container is limited to %d MB. The JVM needs roughly %d MB beyond the heap, so the server risks being killed or stalling under memory pressure. DiscoPanel clamps the heap at start as a safety net, but the right fix is automatic memory sizing or a higher container limit.",
			xmx, server.Memory, reserveMB(server.Memory)),
		FixID:    FixEnableAutoMemory,
		FixLabel: "Enable automatic memory",
	}}
}

func checkFlagConflict(cfg *storage.ServerConfig) []Finding {
	if cfg == nil || !boolVal(cfg.UseAikarFlags) || !boolVal(cfg.UseMeowiceFlags) {
		return nil
	}
	return []Finding{{
		ID:       "flag_conflict",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING,
		Title:    "Conflicting JVM flag sets",
		Detail:   "Both Aikar's and MeowIce's flag sets are enabled; MeowIce wins and the Aikar toggle does nothing. Pick one.",
		FixID:    FixKeepAikarFlags,
		FixLabel: "Keep Aikar's flags",
	}}
}

func checkGCChoice(cfg *storage.ServerConfig, javaMajor int) []Finding {
	if cfg == nil || javaMajor < 21 || boolVal(cfg.UseZGCFlags) {
		return nil
	}
	if configuredHeapMB(cfg) < 12288 {
		return nil
	}
	return []Finding{{
		ID:       "gc_choice",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
		Title:    "ZGC likely beats G1 at this heap size",
		Detail:   "With a 12 GB+ heap on Java 21+, generational ZGC usually delivers lower pause times than the G1 recipes for heavy modpacks.",
		FixID:    FixEnableZGC,
		FixLabel: "Switch to ZGC",
	}}
}

func checkThrottling(m *metrics.ServerMetrics) []Finding {
	if m.CPUThrottlePercent < 5 {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	if m.CPUThrottlePercent >= 20 {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
	}
	quota := "its CPU limit"
	if m.CPUQuotaCores > 0 {
		quota = fmt.Sprintf("its %.1f-core CPU limit", m.CPUQuotaCores)
	}
	return []Finding{{
		ID:       "cpu_throttling",
		Severity: severity,
		Title:    "CPU throttling detected",
		Detail: fmt.Sprintf(
			"The server was CPU-throttled in %.0f%% of recent scheduling periods because it keeps hitting %s. Throttling causes periodic freezes even when average CPU looks fine. Raise or remove the CPU limit in the server's Docker overrides.",
			m.CPUThrottlePercent, quota),
	}}
}

func checkGCPressure(cfg *storage.ServerConfig, m *metrics.ServerMetrics, javaMajor int) []Finding {
	if m.GCPauseMaxMs < 200 {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	if m.GCPauseMaxMs >= 1000 {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
	}
	f := Finding{
		ID:       "gc_pressure",
		Severity: severity,
		Title:    "Long garbage collection pauses",
		Detail: fmt.Sprintf(
			"The longest recent GC pause was %.0f ms; anything over 200 ms is felt in game as a freeze. More memory usually helps, and on Java 21+ generational ZGC brings pauses under a millisecond.",
			m.GCPauseMaxMs),
	}
	if javaMajor >= 21 && cfg != nil && !boolVal(cfg.UseZGCFlags) {
		f.FixID = FixEnableZGC
		f.FixLabel = "Switch to ZGC"
	}
	return []Finding{f}
}

func checkTickHealth(m *metrics.ServerMetrics) []Finding {
	if !m.AgentJvmActive || m.MSPT < 40 {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	title := "Ticks are running close to the limit"
	if m.MSPT >= 50 {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
		title = "Server cannot keep up with 20 TPS"
	}
	return []Finding{{
		ID:       "tick_health",
		Severity: severity,
		Title:    title,
		Detail: fmt.Sprintf(
			"Average tick time is %.1f ms (worst %.1f ms); the budget for 20 TPS is 50 ms. Heavy mods, huge farms, and too many loaded chunks are the usual causes.",
			m.MSPT, m.MSPTMax),
	}}
}

func checkHeapPressure(m *metrics.ServerMetrics) []Finding {
	if m.HeapMaxMB <= 0 || m.HeapUsedMB/m.HeapMaxMB < 0.92 {
		return nil
	}
	return []Finding{{
		ID:       "heap_pressure",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING,
		Title:    "Heap is nearly full",
		Detail: fmt.Sprintf(
			"The JVM is using %.0f of %.0f MB of heap. A consistently full heap forces constant garbage collection. Give the server more memory.",
			m.HeapUsedMB, m.HeapMaxMB),
	}}
}

func checkCrash(m *metrics.ServerMetrics) []Finding {
	if !m.LastExitCrashed || time.Since(m.LastExitedAt) > 24*time.Hour {
		return nil
	}
	detail := fmt.Sprintf("The server process crashed (exit code %d) at %s.",
		m.LastExitCode, m.LastExitedAt.Format("15:04 on Jan 2"))
	if m.LastCrashReportPath != "" {
		detail += fmt.Sprintf(" Crash report: %s (viewable in the Files tab).", m.LastCrashReportPath)
	}
	if excerpt := firstLine(m.LastCrashExcerpt); excerpt != "" {
		detail += " " + excerpt
	}
	return []Finding{{
		ID:       "recent_crash",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL,
		Title:    "Server crashed recently",
		Detail:   detail,
	}}
}

// configuredHeapMB resolves the effective -Xmx from the config fields.
func configuredHeapMB(cfg *storage.ServerConfig) int {
	if v := ParseMemoryMB(strVal(cfg.MaxMemory)); v > 0 {
		return v
	}
	return ParseMemoryMB(strVal(cfg.Memory))
}

// ParseMemoryMB parses values like "4096M", "12G", "2048" (MB assumed) to MB,
// returning 0 for empty or unparseable input.
func ParseMemoryMB(s string) int {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "G"):
		mult = 1024
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		s = strings.TrimSuffix(s, "K")
		if v, err := strconv.Atoi(s); err == nil {
			return v / 1024
		}
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v * mult
}

// ClampHeapForLimit returns the heap the container limit safely supports and
// whether the configured heap had to be clamped (the lifecycle guardrail).
func ClampHeapForLimit(containerMB, heapMB int) (int, bool) {
	if containerMB <= 0 || heapMB <= 0 {
		return heapMB, false
	}
	allowed := containerMB - reserveMB(containerMB)
	if allowed < 256 {
		allowed = 256
	}
	if heapMB <= allowed {
		return heapMB, false
	}
	return allowed, true
}

// firstLine returns the first non-empty line of a crash excerpt.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "---- Minecraft Crash Report ----"))
		if line != "" && !strings.HasPrefix(line, "//") {
			return line
		}
	}
	return ""
}

func isRunning(server *storage.Server) bool {
	switch server.Status {
	case storage.StatusRunning, storage.StatusUnhealthy, storage.StatusStarting:
		return true
	}
	return false
}

func boolVal(b *bool) bool {
	return b != nil && *b
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

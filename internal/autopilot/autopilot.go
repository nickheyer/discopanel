// Package autopilot turns telemetry and config into fixes and findings
package autopilot

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/minecraft"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// Fix IDs offered by findings and accepted by ApplyFix
const (
	FixResetHeap      = "reset_heap_sizing"
	FixEnableZGC      = "enable_zgc"
	FixKeepAikarFlags = "keep_aikar_flags"
	FixDisableMod     = "disable_mod"
)

// One check result described in plain language
type Finding struct {
	ID       string
	Severity v1.PerformanceSeverity
	Title    string
	Detail   string
	FixID    string
	FixLabel string
	FixArgs  []string // Fix targets, e.g. the mod files to disable
	Source   v1.FindingSource
	Evidence []string // Short factual lines backing the finding
	Action   string   // What automation already did, may be empty
	Epoch    string   // Dismissal scope, a change resurfaces the finding
	LedgerMs int64    // Ledger window start for View Logs, 0 when none
}

// All checks run, m may be nil
func Analyze(server *storage.Server, cfg *storage.ServerProperties, m *metrics.ServerMetrics) []Finding {
	var findings []Finding
	javaMajor, _ := strconv.Atoi(server.JavaVersion)

	config := v1.FindingSource_FINDING_SOURCE_CONFIG
	telemetry := v1.FindingSource_FINDING_SOURCE_TELEMETRY

	findings = append(findings, withSource(config, checkHeapVsLimit(server, cfg))...)
	findings = append(findings, withSource(config, checkFlagConflict(cfg))...)
	findings = append(findings, withSource(config, checkGCChoice(cfg, javaMajor))...)
	findings = append(findings, withSource(v1.FindingSource_FINDING_SOURCE_PREFLIGHT, checkPreflight(server))...)

	if m != nil {
		findings = append(findings, withSource(telemetry, checkThrottling(m))...)
		findings = append(findings, withSource(telemetry, checkGCPressure(cfg, m, javaMajor))...)
		findings = append(findings, withSource(telemetry, checkTickHealth(m))...)
		findings = append(findings, withSource(telemetry, checkHeapPressure(m))...)
		findings = append(findings, withSource(telemetry, checkMemoryStall(server, m))...)
		findings = append(findings, withSource(telemetry, checkIOStall(m))...)
		findings = append(findings, withSource(telemetry, checkHostTHP(m))...)
		findings = append(findings, withSource(v1.FindingSource_FINDING_SOURCE_CRASH_DOCTOR, checkCrash(server, m))...)
		findings = append(findings, withSource(telemetry, checkAgentLink(server, cfg, m))...)
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

func withSource(source v1.FindingSource, findings []Finding) []Finding {
	for i := range findings {
		findings[i].Source = source
	}
	return findings
}

// Mutates server and cfg for a fix, caller must persist both
func ApplyFix(server *storage.Server, cfg *storage.ServerProperties, fixID string, fixArgs []string) (string, error) {
	t, f := true, false
	switch fixID {
	case FixResetHeap:
		server.MemoryMin, server.MemoryMax = storage.DefaultHeapForMemory(server.Memory)
		cfg.SyncMemoryFromServer(server)
		return "Java memory reset to the recommended share of the server memory.", nil
	case FixEnableZGC:
		cfg.UseZGCFlags = &t
		cfg.UseAikarFlags = &f
		cfg.UseMeowiceFlags = &f
		return "ZGC enabled: the server will run generational ZGC.", nil
	case FixKeepAikarFlags:
		cfg.UseMeowiceFlags = &f
		return "MeowIce flags disabled: Aikar's flags take over.", nil
	case FixDisableMod:
		return applyDisableMods(server, cfg, fixArgs)
	default:
		return "", fmt.Errorf("unknown fix %q", fixID)
	}
}

func applyDisableMods(server *storage.Server, cfg *storage.ServerProperties, fileNames []string) (string, error) {
	if len(fileNames) == 0 {
		return "", fmt.Errorf("no mod files given")
	}
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return "", fmt.Errorf("this server type does not support mods")
	}
	for _, fileName := range fileNames {
		if fileName == "" || fileName != filepath.Base(fileName) {
			return "", fmt.Errorf("invalid mod file %q", fileName)
		}
		if !minecraft.IsValidModFile(fileName, server.ModLoader) {
			return "", fmt.Errorf("%q is not a mod file for this loader", fileName)
		}
	}
	var messages []string
	for _, fileName := range fileNames {
		msg, err := applyDisableMod(modsDir, server, cfg, fileName)
		if err != nil {
			return "", err
		}
		messages = append(messages, msg)
	}
	if len(messages) == 1 {
		return messages[0], nil
	}
	return fmt.Sprintf("%d mods disabled. Re-enable them any time from the Mods tab.", len(messages)), nil
}

func applyDisableMod(modsDir string, server *storage.Server, cfg *storage.ServerProperties, fileName string) (string, error) {
	src := filepath.Join(modsDir, fileName)
	dst := filepath.Join(modsDir+"_disabled", fileName)
	if _, err := os.Stat(src); err != nil {
		if _, derr := os.Stat(dst); derr == nil {
			return fmt.Sprintf("%s is already disabled.", fileName), nil
		}
		return "", fmt.Errorf("mod %q not found", fileName)
	}
	if err := minecraft.DisableModJar(modsDir, fileName); err != nil {
		return "", fmt.Errorf("failed to disable mod: %w", err)
	}

	minecraft.AppendPackExclude(server.ModLoader, cfg, fileName)
	return fmt.Sprintf("%s disabled. Re-enable it any time from the Mods tab.", fileName), nil
}

// Extra container memory the JVM needs beyond heap
func reserveMB(containerMB int) int {
	fifth := containerMB / 5
	if fifth < 512 {
		return 512
	}
	return fifth
}

func checkHeapVsLimit(server *storage.Server, cfg *storage.ServerProperties) []Finding {
	if server.Memory <= 0 {
		return nil
	}
	xmx := server.MemoryMax
	if xmx <= 0 {
		xmx = configuredHeapMB(cfg)
	}
	if xmx <= 0 {
		return nil
	}
	allowed := server.Memory - reserveMB(server.Memory)
	if xmx <= allowed {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	title := "Java memory limit overlaps the container's overhead reserve"
	if xmx >= server.Memory {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
		title = "Java memory exceeds the server limit"
	}
	return []Finding{{
		ID:       "heap_headroom",
		Severity: severity,
		Title:    title,
		Detail: fmt.Sprintf(
			"The Java heap is set to %d MB but the container is limited to %d MB. The JVM needs roughly %d MB beyond the heap, so the server risks being killed or stalling under memory pressure. Lower the heap or raise the server memory.",
			xmx, server.Memory, reserveMB(server.Memory)),
		FixID:    FixResetHeap,
		FixLabel: "Reset Java memory to recommended",
	}}
}

func checkFlagConflict(cfg *storage.ServerProperties) []Finding {
	if cfg == nil || !boolVal(cfg.UseAikarFlags) || !boolVal(cfg.UseMeowiceFlags) {
		return nil
	}
	return []Finding{{
		ID:       "flag_conflict",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING,
		Title:    "Conflicting JVM flag sets",
		Detail:   "Both Aikar's and MeowIce's flag sets are enabled. MeowIce wins and the Aikar toggle does nothing. Pick one.",
		FixID:    FixKeepAikarFlags,
		FixLabel: "Keep Aikar's flags",
	}}
}

func checkGCChoice(cfg *storage.ServerProperties, javaMajor int) []Finding {
	if cfg == nil || javaMajor < 21 || boolVal(cfg.UseZGCFlags) {
		return nil
	}
	if configuredHeapMB(cfg) < 12288 {
		return nil
	}
	return []Finding{{
		ID:       "gc_choice",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
		Title:    "ZGC likely beats G1 at this memory size",
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

func checkGCPressure(cfg *storage.ServerProperties, m *metrics.ServerMetrics, javaMajor int) []Finding {
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
			"The longest recent GC pause was %.0f ms - anything over 200 ms is felt in game as a freeze. More memory usually helps, and on Java 21+ generational ZGC brings pauses under a millisecond.",
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
		Title:    "Java memory is nearly full",
		Detail: fmt.Sprintf(
			"The JVM is using %.0f of %.0f MB of heap. A consistently full heap forces constant garbage collection. Give the server more memory.",
			m.HeapUsedMB, m.HeapMaxMB),
	}}
}

// True when resetting heap defaults would shrink the heap
func resetHeapWouldHelp(server *storage.Server) bool {
	if server == nil || server.Memory <= 0 || server.MemoryMax <= 0 {
		return false
	}
	_, defMax := storage.DefaultHeapForMemory(server.Memory)
	return server.MemoryMax > defMax
}

// Reads memory stall time, an early warning before OOM kill
func checkMemoryStall(server *storage.Server, m *metrics.ServerMetrics) []Finding {
	if !m.PSIAvailable {
		return nil
	}
	if m.PSIMemFull < 5 && m.PSIMemSome < 10 {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	title := "Memory pressure is building"
	if m.PSIMemFull >= 5 {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
		title = "Server is running out of memory right now"
	}
	f := Finding{
		ID:       "memory_stall",
		Severity: severity,
		Title:    title,
		Detail: fmt.Sprintf(
			"The container spent %.0f%% of the last 10 seconds stalled waiting on memory (%.0f%% with everything frozen). This is the kernel struggling to find free pages, and it usually ends in an out-of-memory kill. Raise the server's memory limit or lower the heap.",
			m.PSIMemSome, m.PSIMemFull),
	}
	if resetHeapWouldHelp(server) {
		f.FixID = FixResetHeap
		f.FixLabel = "Reset Java memory to recommended"
	}
	return []Finding{f}
}

// Reads IO stall time, shows as freezes during saves
func checkIOStall(m *metrics.ServerMetrics) []Finding {
	if !m.PSIAvailable || m.PSIIoSome < 15 {
		return nil
	}
	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
	if m.PSIIoSome >= 40 {
		severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL
	}
	return []Finding{{
		ID:       "io_stall",
		Severity: severity,
		Title:    "Disk cannot keep up with the server",
		Detail: fmt.Sprintf(
			"The container spent %.0f%% of the last 10 seconds stalled waiting on disk. World saves and chunk loading are fighting for IO, which players feel as freezes. Other heavy disk users on this host (backups, other servers) are the usual cause; faster storage for the server data directory is the durable fix.",
			m.PSIIoSome),
	}}
}

// Flags hosts with hugepages disabled, costs 5-15% on big heaps
func checkHostTHP(m *metrics.ServerMetrics) []Finding {
	if m.HostTHPMode != "never" {
		return nil
	}
	return []Finding{{
		ID:       "host_thp_off",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
		Title:    "Huge pages are disabled on the host",
		Detail:   "The Docker host has transparent hugepages set to \"never\", so the JVM cannot back its heap with huge pages (typically worth 5-15% on multi-GB heaps). On the host, set /sys/kernel/mm/transparent_hugepage/enabled to \"madvise\" to let servers opt in without affecting other workloads.",
	}}
}

func checkCrash(server *storage.Server, m *metrics.ServerMetrics) []Finding {
	if !m.LastExitCrashed || time.Since(m.LastExitedAt) > 24*time.Hour {
		return nil
	}
	when := m.LastExitedAt.Format("15:04 on Jan 2")

	if m.LastExitOomKilled {
		f := Finding{
			ID:       "oom_crash",
			Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL,
			Title:    "Server ran out of memory",
			Detail:   "The Java heap plus JVM overhead exceeded the container limit, so the kernel killed the server. Raise the server's memory, or lower the heap so it fits.",
			Evidence: []string{fmt.Sprintf("killed by the kernel OOM killer at %s (exit code %d)", when, m.LastExitCode)},
		}
		if resetHeapWouldHelp(server) {
			f.FixID = FixResetHeap
			f.FixLabel = "Reset Java memory to recommended"
		}
		return []Finding{f}
	}

	f := Finding{
		ID:       "recent_crash",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL,
		Title:    "Server crashed recently",
		Detail:   "The server process died unexpectedly.",
		Evidence: []string{fmt.Sprintf("exit code %d at %s", m.LastExitCode, when)},
	}
	// Boot failures end in a supervisor stop, the code means nothing
	if m.LastExitBootFailed {
		f.ID = "boot_failed"
		f.Title = "Server failed to start"
		f.Detail = "The server could not finish starting and was shut down."
		f.Evidence = []string{fmt.Sprintf("boot ended at %s", when)}
	}
	if crashes := m.CrashesWithin(crashLoopWindow); crashes >= 2 {
		f.ID = "crash_loop"
		f.Title = "Server is crash looping"
		f.Detail = "The server keeps crashing right after starting."
		f.Evidence = append(f.Evidence,
			fmt.Sprintf("%d crashes in the last %d minutes", crashes, int(crashLoopWindow.Minutes())))
	}
	if m.LastCrashReportPath != "" {
		f.Evidence = append(f.Evidence, fmt.Sprintf("crash report: %s (Files tab)", m.LastCrashReportPath))
	}
	fatal := effectiveFatal(server, m)
	f.Evidence = append(f.Evidence, fatalEvidence(fatal)...)

	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	diag := diagnoseFatal(fatal, modsDir)
	verdict := len(fatal.GetFailedMods()) > 0
	if diag != nil {
		if diag.Cause != "" {
			f.Detail += " " + diag.Cause
		}
		for _, mod := range diag.Mods {
			if verdict {
				f.Evidence = append(f.Evidence, "loader verdict: "+modLabel(mod)+" failed to load")
			} else {
				// Frame evidence places the crash, it does not convict
				f.Evidence = append(f.Evidence, "the crash happened inside "+modLabel(mod)+", which may be a victim of another mod or a bad config")
			}
		}
	}

	// The doctor's own trail beats re-deriving a fix button
	j := loadDoctor(server.DataPath)
	doctorActed := !m.LastAutoRepairAt.IsZero() && !m.LastAutoRepairAt.Before(m.LastExitedAt)
	if action := doctorNarration(j, m, doctorActed); action != "" {
		f.Action = action
		f.LedgerMs = incidentStartMs(j)
	}
	// A fresh crash resurfaces a dismissed finding
	f.Epoch = strconv.FormatInt(m.LastExitedAt.UnixMilli(), 10)
	if f.Action == "" && !m.CrashLoopStoppedAt.IsZero() && time.Since(m.CrashLoopStoppedAt) < crashLoopWindow {
		f.Action = "DiscoPanel stopped it to break the loop."
	}
	// A repaired crash on a running server is history, not an alarm
	repaired := doctorActed && j.Resolved != nil && j.Resolved.Outcome == "repaired"
	if repaired && server.Status == storage.StatusRunning {
		f.ID = "repaired_crash"
		f.Severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO
		f.Title = "Server crashed and was repaired"
	}
	// A disable button appears only on an unhandled loader verdict
	if verdict && !doctorActed && diag != nil {
		if files := verdictFiles(diag.Mods); len(files) > 0 {
			f.FixID = FixDisableMod
			f.FixArgs = files
			f.FixLabel = "Disable this mod"
			if len(files) > 1 {
				f.FixLabel = "Disable these mods"
			}
		}
	}
	return []Finding{f}
}

// Summarizes the fatal cause chain for the evidence list
func fatalEvidence(fatal *agentv1.FatalError) []string {
	causes := fatal.GetCauses()
	if len(causes) == 0 {
		return nil
	}
	root := causes[len(causes)-1]
	line := simpleTypeName(root.GetType())
	if msg := root.GetMessage(); msg != "" {
		if len(msg) > 140 {
			msg = msg[:140] + "..."
		}
		line += ": " + msg
	}
	return []string{"root cause: " + line}
}

// Start of the incident the narration describes, for View Logs
func incidentStartMs(j *doctorState) int64 {
	if j.Incident != nil {
		return j.Incident.OpenedAt.UnixMilli()
	}
	if j.Resolved != nil {
		return j.Resolved.OpenedAt.UnixMilli()
	}
	return 0
}

// Narrates what the doctor did or is doing right now
func doctorNarration(j *doctorState, m *metrics.ServerMetrics, acted bool) string {
	if j.Incident != nil && len(j.Incident.Actions) > 0 {
		return fmt.Sprintf("DiscoPanel is repairing this now (attempt %d of %d): %s.",
			j.Incident.Passes, maxDoctorPasses, summarizeIncident(j.Incident))
	}
	if !acted {
		return ""
	}
	if j.Resolved != nil && j.Resolved.Outcome == "gave_up" {
		return "DiscoPanel tried to repair this, undid its changes, and stopped the server: " + j.Resolved.Summary + "."
	}
	return "DiscoPanel automatically " + m.LastAutoRepairSummary + " and restarted the server."
}

func modLabel(mod crashModRef) string {
	switch {
	case mod.ModID != "" && mod.ModFile != "":
		return fmt.Sprintf("%q (%s)", mod.ModID, mod.ModFile)
	case mod.ModID != "":
		return fmt.Sprintf("%q", mod.ModID)
	}
	return mod.ModFile
}

// Collects the disable targets that resolved to jars
func verdictFiles(mods []crashModRef) []string {
	var files []string
	for _, mod := range mods {
		if mod.ModFile != "" {
			files = append(files, mod.ModFile)
		}
	}
	return files
}

const maxPreflightEvidence = 8

// Solves the installed mod graph, findings only, never actions
func checkPreflight(server *storage.Server) []Finding {
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return nil
	}
	metas := minecraft.ScanModsDir(modsDir)
	if len(metas) == 0 {
		return nil
	}
	issues := minecraft.SolveDeps(metas, serverDialects(server))
	if len(issues) == 0 {
		return nil
	}

	severity := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO
	var evidence []string
	for _, issue := range issues {
		if issue.Kind != minecraft.DepVersion {
			severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING
		}
		if len(evidence) < maxPreflightEvidence {
			evidence = append(evidence, issue.Describe())
		}
	}
	if extra := len(issues) - len(evidence); extra > 0 {
		evidence = append(evidence, fmt.Sprintf("and %d more", extra))
	}

	return []Finding{{
		ID:       "mod_dependencies",
		Severity: severity,
		Title:    "Mod dependency problems",
		Detail:   "The installed mods declare requirements that are not met, so the server may fail to start. DiscoPanel fixes what it can prove at startup and repairs the rest if a crash confirms it.",
		Evidence: evidence,
		Epoch:    strings.Join(evidence, "\n"),
	}}
}

// Rolling window for calling repeated crashes a loop
const crashLoopWindow = 10 * time.Minute

// How long a missing agent stays info before warning
const agentConnectGrace = 2 * time.Minute

// Flags a missing agent session, panel loses live telemetry
func checkAgentLink(server *storage.Server, cfg *storage.ServerProperties, m *metrics.ServerMetrics) []Finding {
	if !isRunning(server) || m.AgentConnected {
		return nil
	}
	if cfg != nil && cfg.EnableAgent != nil && !*cfg.EnableAgent {
		return []Finding{{
			ID:       "agent_disabled",
			Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
			Title:    "Limited telemetry",
			Detail:   "The DiscoPanel agent is disabled for this server, so tick timing, GC pauses, CPU throttling, and crash forensics cannot be measured. Enable it in the server settings for full telemetry.",
		}}
	}
	// Crash findings own the story while crashes are fresh
	if m.LastExitCrashed && time.Since(m.LastExitedAt) < 15*time.Minute {
		return nil
	}
	pastGrace := server.LastStarted != nil && time.Since(*server.LastStarted) > agentConnectGrace
	if !pastGrace {
		return []Finding{{
			ID:       "agent_offline",
			Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
			Title:    "Limited telemetry",
			Detail:   "The DiscoPanel agent is not connected yet. Metrics fall back to slower ping sampling until it connects.",
		}}
	}
	// A session this run proves connectivity, blame the process
	if server.LastStarted != nil && m.LastAgentSessionAt.After(*server.LastStarted) {
		return []Finding{{
			ID:       "agent_dropped",
			Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING,
			Title:    "Telemetry link dropped",
			Detail:   "The agent reached the panel after this start but is not connected now, so the server process likely exited or is restarting. The container console shows what happened.",
		}}
	}
	return []Finding{{
		ID:       "agent_unreachable",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING,
		Title:    "Live telemetry is not connecting",
		Detail: fmt.Sprintf(
			"The server has been running for %d minutes but its agent never reached the panel, so TPS, GC pauses, CPU throttling, and crash reports are missing. Usual causes: a firewall blocking container-to-host traffic when the panel runs outside Docker, or a wrong docker.agent_url setting. The container console shows the connection errors.",
			int(time.Since(*server.LastStarted).Minutes())),
	}}
}

// Resolves effective -Xmx from the config fields
func configuredHeapMB(cfg *storage.ServerProperties) int {
	if cfg == nil || cfg.MaxMemory == nil {
		return 0
	}
	return runtimespec.ParseMemoryMB(*cfg.MaxMemory)
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

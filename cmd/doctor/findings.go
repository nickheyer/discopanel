package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/nickheyer/discopanel/pkg/minecraft"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"google.golang.org/protobuf/proto"
)

// Extra container memory the JVM needs beyond heap
func reserveMB(containerMB int) int {
	fifth := containerMB / 5
	if fifth < 512 {
		return 512
	}
	return fifth
}

const maxPreflightEvidence = 8

// Computes and publishes this server's findings for the panel
func (d *doctor) publishFindings(ctx context.Context, srv *serverInfo, server *v1.Server) {
	props := d.panel.serverProperties(ctx, srv.ID)

	var findings []*v1.PerformanceFinding
	findings = append(findings, checkHeapVsLimit(server, props)...)
	findings = append(findings, checkFlagConflict(props)...)
	findings = append(findings, checkGCChoice(server, props)...)
	findings = append(findings, checkDeps(srv)...)
	findings = append(findings, d.checkCrash(srv)...)

	if len(findings) == 0 {
		findings = append(findings, &v1.PerformanceFinding{
			Id:       "all_good",
			Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_OK,
			Title:    "Looking good",
			Detail:   "The doctor found no problems.",
		})
	}

	// Unchanged findings skip the write, mtime stays honest
	prev := runtimespec.ReadFindings(srv.DataPath)
	if slices.EqualFunc(findings, prev, func(a, b *v1.PerformanceFinding) bool { return proto.Equal(a, b) }) {
		return
	}
	if err := runtimespec.WriteFindings(srv.DataPath, findings); err != nil {
		d.logf("%s: findings publish failed: %v", srv.Name, err)
	}
}

func propBool(props map[string]string, key string) bool {
	return props[key] == "true"
}

func checkHeapVsLimit(server *v1.Server, props map[string]string) []*v1.PerformanceFinding {
	limit := int(server.GetMemory())
	if limit <= 0 {
		return nil
	}
	xmx := int(server.GetMemoryMax())
	if xmx <= 0 {
		xmx = runtimespec.ParseMemoryMB(props["maxMemory"])
	}
	if xmx <= 0 || xmx <= limit-reserveMB(limit) {
		return nil
	}
	severity, title := v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING, "Java memory limit overlaps the container's overhead reserve"
	if xmx >= limit {
		severity, title = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL, "Java memory exceeds the server limit"
	}
	return []*v1.PerformanceFinding{{
		Id:       "heap_headroom",
		Severity: severity,
		Source:   v1.FindingSource_FINDING_SOURCE_CONFIG,
		Title:    title,
		Detail: fmt.Sprintf(
			"The Java heap is set to %d MB but the container is limited to %d MB. The JVM needs roughly %d MB beyond the heap, so the server risks being killed or stalling under memory pressure. Lower the heap or raise the server memory.",
			xmx, limit, reserveMB(limit)),
	}}
}

func checkFlagConflict(props map[string]string) []*v1.PerformanceFinding {
	if !propBool(props, "useAikarFlags") || !propBool(props, "useMeowiceFlags") {
		return nil
	}
	return []*v1.PerformanceFinding{{
		Id:       "flag_conflict",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING,
		Source:   v1.FindingSource_FINDING_SOURCE_CONFIG,
		Title:    "Conflicting JVM flag sets",
		Detail:   "Both Aikar's and MeowIce's flag sets are enabled. MeowIce wins and the Aikar toggle does nothing. Pick one.",
	}}
}

func checkGCChoice(server *v1.Server, props map[string]string) []*v1.PerformanceFinding {
	if propBool(props, "useZgcFlags") {
		return nil
	}
	if server.GetJavaVersion() < 21 || runtimespec.ParseMemoryMB(props["maxMemory"]) < 12288 {
		return nil
	}
	return []*v1.PerformanceFinding{{
		Id:       "gc_choice",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO,
		Source:   v1.FindingSource_FINDING_SOURCE_CONFIG,
		Title:    "ZGC likely beats G1 at this memory size",
		Detail:   "With a 12 GB+ heap on Java 21+, generational ZGC usually delivers lower pause times than the G1 recipes for heavy modpacks.",
	}}
}

// Solves the installed mod graph, reporting only, repair on crash
func checkDeps(srv *serverInfo) []*v1.PerformanceFinding {
	modsDir := minecraft.GetModsPath(srv.DataPath, srv.ModLoader)
	if modsDir == "" {
		return nil
	}
	metas := minecraft.ScanModsDir(modsDir)
	if len(metas) == 0 {
		return nil
	}
	issues := minecraft.SolveDeps(metas, serverDialects(srv))
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

	return []*v1.PerformanceFinding{{
		Id:       "mod_dependencies",
		Severity: severity,
		Source:   v1.FindingSource_FINDING_SOURCE_PREFLIGHT,
		Title:    "Mod dependency problems",
		Detail:   "The installed mods declare requirements that are not met, so the server may fail to start. The doctor repairs what a crash confirms.",
		Evidence: evidence,
		Epoch:    strconv.Itoa(len(issues)),
	}}
}

// Narrates the newest crash and what the doctor did about it
func (d *doctor) checkCrash(srv *serverInfo) []*v1.PerformanceFinding {
	history := runtimespec.ReadExitHistory(srv.DataPath)
	exit := latestExit(history)
	if exit == nil {
		return nil
	}
	exitedAt := time.UnixMilli(exit.ExitedAtUnixMs)
	if time.Since(exitedAt) > 24*time.Hour {
		return nil
	}
	j := runtimespec.LoadDoctor(srv.DataPath)
	when := exitedAt.Format("15:04 on Jan 2")

	f := &v1.PerformanceFinding{
		Id:       "recent_crash",
		Severity: v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL,
		Source:   v1.FindingSource_FINDING_SOURCE_CRASH_DOCTOR,
		Title:    "Server crashed recently",
		Detail:   "The server process died unexpectedly.",
		Evidence: []string{fmt.Sprintf("exit code %d at %s", exit.ExitCode, when)},
		Epoch:    strconv.FormatInt(exit.ExitedAtUnixMs, 10),
	}
	switch {
	case exit.OomKilled:
		f.Id = "oom_crash"
		f.Title = "Server ran out of memory"
		f.Detail = "The Java heap plus JVM overhead exceeded the container limit, so the kernel killed the server. Raise the server's memory, or lower the heap so it fits."
	case exit.BootFailed:
		f.Id = "boot_failed"
		f.Title = "Server failed to start"
		f.Detail = "The server could not finish starting and was shut down."
	case !exit.Crashed:
		if exitsWithin(history, crashLoopWindow) < crashLoopThreshold {
			return nil
		}
		f.Id = "exit_loop"
		f.Title = "Server keeps exiting right after it starts"
		f.Detail = "The server process keeps ending without a crash report, and the container restart policy boots it again each time. The console shows the last lines before each exit."
	}
	if crashes := exitsWithin(history, crashLoopWindow); crashes >= crashLoopThreshold && exit.Crashed {
		f.Id = "crash_loop"
		f.Title = "Server is crash looping"
		f.Detail = "The server keeps crashing right after starting."
		f.Evidence = append(f.Evidence, fmt.Sprintf("%d exits in the last %d minutes", crashes, int(crashLoopWindow.Minutes())))
	}
	if exit.CrashReportPath != "" {
		f.Evidence = append(f.Evidence, fmt.Sprintf("crash report: %s (Files tab)", exit.CrashReportPath))
	}
	if cause := journalCause(j, exit); cause != "" {
		f.Detail += " " + cause
	}

	resolvedCovers := j.Resolved != nil && !j.Resolved.ClosedAt.IsZero() && j.Resolved.ClosedAt.UnixMilli() >= exit.ExitedAtUnixMs
	switch {
	case j.Incident != nil && len(j.Incident.Actions) > 0:
		f.Action = fmt.Sprintf("The doctor is repairing this now (attempt %d): %s.", j.Incident.Passes, j.Incident.Summary)
		f.ActionLogStartMs = j.Incident.OpenedAt.UnixMilli()
	case resolvedCovers && j.Resolved.Outcome == "gave_up":
		f.Action = "The doctor tried to repair this, undid its changes, and stopped the server: " + j.Resolved.Summary + "."
		f.ActionLogStartMs = j.Resolved.OpenedAt.UnixMilli()
	case resolvedCovers:
		f.Action = "The doctor automatically " + j.Resolved.Summary + " and restarted the server."
		f.ActionLogStartMs = j.Resolved.OpenedAt.UnixMilli()
		if srv.Running {
			f.Id = "repaired_crash"
			f.Severity = v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO
			f.Title = "Server crashed and was repaired"
		}
	}
	return []*v1.PerformanceFinding{f}
}

// Crash classification the doctor recorded while responding
func journalCause(j *runtimespec.DoctorState, exit *agentv1.Exited) string {
	if j.Incident != nil {
		return j.Incident.Cause
	}
	if j.Resolved != nil && j.Resolved.ClosedAt.UnixMilli() >= exit.ExitedAtUnixMs {
		return j.Resolved.Cause
	}
	return ""
}

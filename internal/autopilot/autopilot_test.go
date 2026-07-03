package autopilot

import (
	"testing"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }

func TestClampHeapForLimit(t *testing.T) {
	cases := []struct {
		containerMB, heapMB int
		want                int
		clamped             bool
	}{
		{4096, 3120, 3120, false}, // default config fits (reserve = 819)
		{2048, 3120, 1536, true},  // heap above limit gets clamped
		{2048, 2048, 1536, true},  // heap == limit is still unsafe
		{0, 3120, 3120, false},    // unlimited container
		{4096, 0, 0, false},       // no configured heap
	}
	for _, c := range cases {
		got, clamped := ClampHeapForLimit(c.containerMB, c.heapMB)
		if got != c.want || clamped != c.clamped {
			t.Errorf("ClampHeapForLimit(%d, %d) = (%d, %v), want (%d, %v)",
				c.containerMB, c.heapMB, got, clamped, c.want, c.clamped)
		}
	}
}

func TestAnalyzeHeapVsLimit(t *testing.T) {
	server := &storage.Server{Memory: 2048, JavaVersion: "21", Status: storage.StatusStopped}
	cfg := &storage.ServerConfig{MaxMemory: strPtr("3120M")}

	findings := Analyze(server, cfg, nil)
	if !hasFinding(findings, "heap_headroom", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Fatalf("expected critical heap_headroom finding, got %+v", findings)
	}
	if Grade(findings) == "A" {
		t.Fatal("critical finding must not grade A")
	}

	// Auto memory silences the check.
	cfg.AutoMemory = boolPtr(true)
	findings = Analyze(server, cfg, nil)
	if hasFinding(findings, "heap_headroom", 0) {
		t.Fatalf("auto memory should suppress heap_headroom, got %+v", findings)
	}
}

func TestAnalyzeThrottlingAndGC(t *testing.T) {
	server := &storage.Server{Memory: 8192, JavaVersion: "21", Status: storage.StatusRunning}
	cfg := &storage.ServerConfig{MaxMemory: strPtr("4G")}
	m := &metrics.ServerMetrics{
		AgentConnected:     true,
		AgentModActive:     true,
		CPUThrottlePercent: 42,
		GCPauseMaxMs:       1500,
		MSPT:               55,
		MSPTMax:            120,
	}

	findings := Analyze(server, cfg, m)
	if !hasFinding(findings, "cpu_throttling", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Errorf("expected critical cpu_throttling, got %+v", findings)
	}
	if !hasFinding(findings, "gc_pressure", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Errorf("expected critical gc_pressure, got %+v", findings)
	}
	if !hasFinding(findings, "tick_health", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Errorf("expected critical tick_health, got %+v", findings)
	}
	if Grade(findings) != "F" {
		t.Errorf("expected grade F, got %s", Grade(findings))
	}

	// The GC finding on Java 21 without ZGC offers the ZGC fix.
	for _, f := range findings {
		if f.ID == "gc_pressure" && f.FixID != FixEnableZGC {
			t.Errorf("gc_pressure should offer ZGC fix, got %q", f.FixID)
		}
	}
}

func TestAnalyzeHealthyServer(t *testing.T) {
	server := &storage.Server{Memory: 8192, JavaVersion: "21", Status: storage.StatusRunning}
	cfg := &storage.ServerConfig{MaxMemory: strPtr("4G")}
	m := &metrics.ServerMetrics{
		AgentConnected: true,
		AgentModActive: true,
		MSPT:           12,
		GCPauseMaxMs:   40,
	}
	findings := Analyze(server, cfg, m)
	if Grade(findings) != "A" {
		t.Errorf("healthy server should grade A, got %s (%+v)", Grade(findings), findings)
	}
}

func TestAnalyzeCrash(t *testing.T) {
	server := &storage.Server{Memory: 4096, JavaVersion: "21", Status: storage.StatusStopped}
	m := &metrics.ServerMetrics{
		LastExitCrashed:     true,
		LastExitCode:        1,
		LastCrashReportPath: "crash-reports/crash-2026-07-02.txt",
		LastExitedAt:        time.Now().Add(-time.Hour),
	}
	findings := Analyze(server, &storage.ServerConfig{}, m)
	if !hasFinding(findings, "recent_crash", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Fatalf("expected recent_crash finding, got %+v", findings)
	}
}

func TestApplyFix(t *testing.T) {
	cfg := &storage.ServerConfig{UseAikarFlags: boolPtr(true), UseMeowiceFlags: boolPtr(true)}

	if _, err := ApplyFix(cfg, "nope"); err == nil {
		t.Fatal("unknown fix must error")
	}

	if _, err := ApplyFix(cfg, FixKeepAikarFlags); err != nil {
		t.Fatal(err)
	}
	if *cfg.UseMeowiceFlags {
		t.Fatal("keep_aikar_flags should disable meowice")
	}

	if _, err := ApplyFix(cfg, FixEnableZGC); err != nil {
		t.Fatal(err)
	}
	if !*cfg.UseZGCFlags || *cfg.UseAikarFlags {
		t.Fatal("enable_zgc should enable zgc and disable aikar")
	}

	if _, err := ApplyFix(cfg, FixEnableAutoMemory); err != nil {
		t.Fatal(err)
	}
	if !*cfg.AutoMemory {
		t.Fatal("enable_auto_memory should set AutoMemory")
	}
}

func hasFinding(findings []Finding, id string, severity v1.PerformanceSeverity) bool {
	for _, f := range findings {
		if f.ID == id && (severity == 0 || f.Severity == severity) {
			return true
		}
	}
	return false
}

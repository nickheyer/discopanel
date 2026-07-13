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

func TestAnalyzeHeapVsLimit(t *testing.T) {
	server := &storage.Server{Memory: 2048, JavaVersion: "21", Status: storage.StatusStopped}
	cfg := &storage.ServerProperties{MaxMemory: strPtr("3120M")}

	findings := Analyze(server, cfg, nil)
	if !hasFinding(findings, "heap_headroom", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Fatalf("expected critical heap_headroom finding, got %+v", findings)
	}

	// Server heap sizing wins over the config string
	server.MemoryMax = 1536
	findings = Analyze(server, cfg, nil)
	if hasFinding(findings, "heap_headroom", 0) {
		t.Fatalf("fitting heap should suppress heap_headroom, got %+v", findings)
	}
}

func TestAnalyzeThrottlingAndGC(t *testing.T) {
	server := &storage.Server{Memory: 8192, JavaVersion: "21", Status: storage.StatusRunning}
	cfg := &storage.ServerProperties{MaxMemory: strPtr("4G")}
	m := &metrics.ServerMetrics{
		AgentConnected:     true,
		AgentJvmActive:     true,
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

	// Java 21 without ZGC offers the ZGC fix
	for _, f := range findings {
		if f.ID == "gc_pressure" && f.FixID != FixEnableZGC {
			t.Errorf("gc_pressure should offer ZGC fix, got %q", f.FixID)
		}
	}
}

func TestAnalyzeHealthyServer(t *testing.T) {
	server := &storage.Server{Memory: 8192, JavaVersion: "21", Status: storage.StatusRunning}
	cfg := &storage.ServerProperties{MaxMemory: strPtr("4G")}
	m := &metrics.ServerMetrics{
		AgentConnected: true,
		AgentJvmActive: true,
		MSPT:           12,
		GCPauseMaxMs:   40,
	}
	findings := Analyze(server, cfg, m)
	for _, f := range findings {
		if f.Severity >= v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING {
			t.Errorf("healthy server should have no problems, got %+v", f)
		}
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
	findings := Analyze(server, &storage.ServerProperties{}, m)
	if !hasFinding(findings, "recent_crash", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Fatalf("expected recent_crash finding, got %+v", findings)
	}
}

func TestApplyFix(t *testing.T) {
	server := &storage.Server{Memory: 4096, MemoryMin: 4096, MemoryMax: 4096}
	cfg := &storage.ServerProperties{UseAikarFlags: boolPtr(true), UseMeowiceFlags: boolPtr(true)}

	if _, err := ApplyFix(server, cfg, "nope", nil); err == nil {
		t.Fatal("unknown fix must error")
	}

	if _, err := ApplyFix(server, cfg, FixKeepAikarFlags, nil); err != nil {
		t.Fatal(err)
	}
	if *cfg.UseMeowiceFlags {
		t.Fatal("keep_aikar_flags should disable meowice")
	}

	if _, err := ApplyFix(server, cfg, FixEnableZGC, nil); err != nil {
		t.Fatal(err)
	}
	if !*cfg.UseZGCFlags || *cfg.UseAikarFlags {
		t.Fatal("enable_zgc should enable zgc and disable aikar")
	}

	if _, err := ApplyFix(server, cfg, FixResetHeap, nil); err != nil {
		t.Fatal(err)
	}
	if server.MemoryMin != 2048 || server.MemoryMax != 3072 {
		t.Fatalf("reset_heap_sizing should restore defaults, got %d/%d", server.MemoryMin, server.MemoryMax)
	}
	if cfg.InitMemory == nil || *cfg.InitMemory != "2048M" || cfg.MaxMemory == nil || *cfg.MaxMemory != "3072M" {
		t.Fatalf("reset_heap_sizing should sync properties, got %v/%v", cfg.InitMemory, cfg.MaxMemory)
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

func TestAnalyzePressureAndTHP(t *testing.T) {
	server := &storage.Server{Memory: 4096, MemoryMax: 4096, JavaVersion: "21", Status: storage.StatusRunning}
	cfg := &storage.ServerProperties{}
	m := &metrics.ServerMetrics{
		AgentConnected: true,
		AgentReady:     true,
		PSIAvailable:   true,
		PSIMemSome:     30,
		PSIMemFull:     8,
		PSIIoSome:      50,
		HostTHPMode:    "never",
	}

	findings := Analyze(server, cfg, m)
	if !hasFinding(findings, "memory_stall", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Fatalf("expected critical memory_stall finding, got %+v", findings)
	}
	if !hasFinding(findings, "io_stall", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_CRITICAL) {
		t.Fatalf("expected critical io_stall finding, got %+v", findings)
	}
	if !hasFinding(findings, "host_thp_off", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO) {
		t.Fatalf("expected host_thp_off finding, got %+v", findings)
	}

	// Memory stall offers heap reset while heap is oversized
	for _, f := range findings {
		if f.ID == "memory_stall" && f.FixID != FixResetHeap {
			t.Fatalf("memory_stall should offer heap reset fix, got %+v", f)
		}
	}

	// Some-only memory pressure is a warning, mild io is quiet
	m.PSIMemFull = 1
	m.PSIIoSome = 10
	findings = Analyze(server, cfg, m)
	if !hasFinding(findings, "memory_stall", v1.PerformanceSeverity_PERFORMANCE_SEVERITY_WARNING) {
		t.Fatalf("expected warning memory_stall finding, got %+v", findings)
	}
	if hasFinding(findings, "io_stall", 0) {
		t.Fatalf("mild io pressure should not report, got %+v", findings)
	}

	// No PSI support means no stall findings at all
	m.PSIAvailable = false
	m.PSIMemSome = 90
	m.PSIMemFull = 90
	findings = Analyze(server, cfg, m)
	if hasFinding(findings, "memory_stall", 0) || hasFinding(findings, "io_stall", 0) {
		t.Fatalf("no PSI should mean no stall findings, got %+v", findings)
	}

	// Madvise and always modes are fine
	m.HostTHPMode = "madvise"
	findings = Analyze(server, cfg, m)
	if hasFinding(findings, "host_thp_off", 0) {
		t.Fatalf("madvise THP should not report, got %+v", findings)
	}
}

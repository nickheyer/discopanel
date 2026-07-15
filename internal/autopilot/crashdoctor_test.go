package autopilot

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickheyer/discopanel/internal/activity"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/logger"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

func writeModJar(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for path, content := range files {
		f, err := w.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnoseFatalMixinAttribution(t *testing.T) {
	modsDir := filepath.Join(t.TempDir(), "mods")
	writeModJar(t, modsDir, "entity_texture_features-forge-1.20.1.jar", map[string]string{
		"fabric.mod.json": `{"id":"entity_texture_features"}`,
	})

	fatal := &agentv1.FatalError{
		Thread: "main",
		Causes: []*agentv1.CrashCause{
			{Type: "java.lang.ExceptionInInitializerError"},
			{
				Type:    "java.lang.RuntimeException",
				Message: "Attempted to load class net/minecraft/client/gui/screens/Screen for invalid dist DEDICATED_SERVER",
				Frames: []*agentv1.CrashFrame{
					{ClassName: "net.minecraftforge.fml.loading.RuntimeDistCleaner", MethodName: "processClassWithFlags"},
					{
						ClassName:      "net.minecraft.resources.ResourceLocation",
						MethodName:     "handler$bbf000$entity_texture_features$etf$illegalPathOverride",
						SourceLocation: "union:/data/libraries/net/minecraft/server/1.20.1/server-1.20.1-srg.jar%23100!/",
					},
				},
			},
		},
	}

	d := diagnoseFatal(fatal, modsDir)
	if d == nil {
		t.Fatal("expected a diagnosis")
	}
	if !strings.Contains(d.Cause, "client") {
		t.Errorf("cause should call out client code on a server, got %q", d.Cause)
	}
	if len(d.Mods) != 1 || d.Mods[0].ModID != "entity_texture_features" || d.Mods[0].ModFile != "entity_texture_features-forge-1.20.1.jar" {
		t.Fatalf("expected mixin attribution, got %+v", d)
	}
}

func TestDiagnoseFatalCodeSourceAttribution(t *testing.T) {
	modsDir := filepath.Join(t.TempDir(), "mods")
	writeModJar(t, modsDir, "badmod-1.0.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"badmod\"\n",
	})

	fatal := &agentv1.FatalError{
		Thread: "Server thread",
		Causes: []*agentv1.CrashCause{{
			Type:    "java.lang.NullPointerException",
			Message: "boom",
			Frames: []*agentv1.CrashFrame{
				{
					ClassName:      "dev.example.badmod.BadTick",
					MethodName:     "tick",
					SourceLocation: "union:/data/mods/badmod-1.0.jar%23245%23249!/",
				},
				{
					ClassName:      "net.minecraft.server.MinecraftServer",
					MethodName:     "tickServer",
					SourceLocation: "union:/data/libraries/net/minecraft/server/1.20.1/server-1.20.1-srg.jar%23100!/",
				},
			},
		}},
	}

	d := diagnoseFatal(fatal, modsDir)
	if d == nil || len(d.Mods) != 1 || d.Mods[0].ModID != "badmod" || d.Mods[0].ModFile != "badmod-1.0.jar" {
		t.Fatalf("expected codesource attribution, got %+v", d)
	}
}

func TestDiagnoseFatalFailedMods(t *testing.T) {
	modsDir := filepath.Join(t.TempDir(), "mods")
	writeModJar(t, modsDir, "oculus-mc1.20.1-1.8.0.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"oculus\"\n",
	})
	writeModJar(t, modsDir, "citresewn-1.20.1-5.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"citresewn\"\n",
	})

	fatal := &agentv1.FatalError{
		Causes: []*agentv1.CrashCause{{Type: "net.minecraftforge.fml.LoadingFailedException"}},
		FailedMods: []*agentv1.FailedMod{
			{ModId: "oculus", FileName: "oculus-mc1.20.1-1.8.0.jar"},
			{ModId: "citresewn"},
			{ModId: "oculus", FileName: "oculus-mc1.20.1-1.8.0.jar"},
		},
	}

	d := diagnoseFatal(fatal, modsDir)
	if d == nil || len(d.Mods) != 2 {
		t.Fatalf("expected two deduped failed mods, got %+v", d)
	}
	if d.Mods[0].ModFile != "oculus-mc1.20.1-1.8.0.jar" {
		t.Errorf("file name should resolve from the loader report, got %+v", d.Mods[0])
	}
	if d.Mods[1].ModFile != "citresewn-1.20.1-5.jar" {
		t.Errorf("file name should resolve from the mod id index, got %+v", d.Mods[1])
	}
}

func TestClassifyFatalTypes(t *testing.T) {
	cases := []struct {
		typeName string
		want     string
	}{
		{"java.lang.OutOfMemoryError", "memory"},
		{"java.lang.UnsupportedClassVersionError", "Java version"},
		{"net.minecraftforge.fml.LoadingFailedException", "dedicated server"},
		{"net.neoforged.fml.ModLoadingException", "dedicated server"},
	}
	for _, tc := range cases {
		fatal := &agentv1.FatalError{Causes: []*agentv1.CrashCause{{Type: tc.typeName}}}
		if got := classifyFatal(fatal); !strings.Contains(got, tc.want) {
			t.Errorf("classifyFatal(%s) = %q, want mention of %q", tc.typeName, got, tc.want)
		}
	}
}

func TestClassifyFatalFabricEnvironment(t *testing.T) {
	fatal := &agentv1.FatalError{Causes: []*agentv1.CrashCause{{
		Type:    "java.lang.RuntimeException",
		Message: "Cannot load class net.fabricmc.fabric.api.client.event.lifecycle.v1.ClientLifecycleEvents in environment type SERVER",
	}}}
	if got := classifyFatal(fatal); !strings.Contains(got, "client-only") {
		t.Errorf("classifyFatal = %q, want client-only mention", got)
	}
}

func TestJarFromLocation(t *testing.T) {
	cases := map[string]string{
		"union:/data/mods/badmod-1.0.jar%23245!/": "badmod-1.0.jar",
		"jar:file:/data/mods/some_mod.jar!/pkg/":  "some_mod.jar",
		"file:/data/mods/plain.jar":               "plain.jar",
		"union:/data/libraries/server-srg.jar!/":  "server-srg.jar",
		"file:/data/config/":                      "",
		"":                                        "",
		"modjar://ad_astra":                       "",
	}
	for loc, want := range cases {
		if got := jarFromLocation(loc); got != want {
			t.Errorf("jarFromLocation(%q) = %q, want %q", loc, got, want)
		}
	}
}

func TestClassifyFailedMod(t *testing.T) {
	cases := []struct {
		fm   *agentv1.FailedMod
		want failReason
	}{
		{&agentv1.FailedMod{Reason: "fml.modloading.missingdependency"}, failMissingDep},
		{&agentv1.FailedMod{Reason: "fml.modloading.dupedmod"}, failDuplicate},
		{&agentv1.FailedMod{ErrorType: "java.lang.UnsupportedClassVersionError"}, failJava},
		{&agentv1.FailedMod{Reason: "fml.modloading.errorduringevent"}, failModError},
		{&agentv1.FailedMod{}, failModError},
	}
	for _, tc := range cases {
		if got := classifyFailedMod(tc.fm); got != tc.want {
			t.Errorf("classifyFailedMod(%+v) = %s, want %s", tc.fm, got, tc.want)
		}
	}
}

func TestCheckCrashFrameEvidenceHasNoFix(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "entity_texture_features-forge-1.20.1.jar", map[string]string{
		"fabric.mod.json": `{"id":"entity_texture_features"}`,
	})

	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderForge, Status: storage.StatusStopped}
	m := &metrics.ServerMetrics{
		LastExitCrashed: true,
		LastExitCode:    1,
		LastExitedAt:    time.Now().Add(-time.Minute),
		LastFatalError: &agentv1.FatalError{
			Causes: []*agentv1.CrashCause{{
				Type: "java.lang.ExceptionInInitializerError",
				Frames: []*agentv1.CrashFrame{
					{ClassName: "net.minecraftforge.fml.loading.RuntimeDistCleaner", MethodName: "processClassWithFlags"},
					{ClassName: "net.minecraft.resources.ResourceLocation", MethodName: "handler$bbf000$entity_texture_features$etf$illegalPathOverride"},
				},
			}},
		},
	}

	findings := checkCrash(server, m)
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %+v", findings)
	}
	f := findings[0]
	if f.FixID != "" || len(f.FixArgs) != 0 {
		t.Fatalf("frame evidence alone must not offer a disable fix, got %+v", f)
	}
	if !hasEvidence(f, "crash happened inside") || !hasEvidence(f, "entity_texture_features") {
		t.Errorf("evidence should place the crash without convicting, got %v", f.Evidence)
	}
}

func TestCheckCrashVerdictOffersDisableFix(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "oculus.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"oculus\"\n",
	})
	writeModJar(t, modsDir, "imblocker.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"imblocker\"\n",
	})

	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderForge, Status: storage.StatusStopped}
	m := &metrics.ServerMetrics{
		LastExitCrashed:    true,
		LastExitBootFailed: true,
		LastExitCode:       143,
		LastExitedAt:       time.Now().Add(-time.Minute),
		LastFatalError: &agentv1.FatalError{
			Causes: []*agentv1.CrashCause{{Type: "net.minecraftforge.fml.LoadingFailedException"}},
			FailedMods: []*agentv1.FailedMod{
				{ModId: "oculus", FileName: "oculus.jar"},
				{ModId: "imblocker", FileName: "imblocker.jar"},
			},
		},
	}

	findings := checkCrash(server, m)
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %+v", findings)
	}
	f := findings[0]
	if f.ID != "boot_failed" || f.Title != "Server failed to start" {
		t.Fatalf("expected boot_failed framing, got %+v", f)
	}
	if strings.Contains(f.Detail, "exit code") {
		t.Errorf("boot failure detail must not surface the exit code, got %q", f.Detail)
	}
	if f.FixID != FixDisableMod || len(f.FixArgs) != 2 {
		t.Fatalf("expected two disable targets, got %+v", f)
	}
	if f.FixLabel != "Disable these mods" {
		t.Errorf("expected plural fix label, got %q", f.FixLabel)
	}
	if !hasEvidence(f, "loader verdict") {
		t.Errorf("evidence should carry the loader verdicts, got %v", f.Evidence)
	}
}

func TestCheckCrashLoopFinding(t *testing.T) {
	server := &storage.Server{DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge, Status: storage.StatusStopped}
	now := time.Now()
	m := &metrics.ServerMetrics{
		LastExitCrashed:    true,
		LastExitCode:       1,
		LastExitedAt:       now.Add(-time.Minute),
		CrashExits:         []time.Time{now.Add(-8 * time.Minute), now.Add(-4 * time.Minute), now.Add(-time.Minute)},
		CrashLoopStoppedAt: now.Add(-time.Minute),
	}

	findings := checkCrash(server, m)
	if len(findings) != 1 || findings[0].ID != "crash_loop" {
		t.Fatalf("expected crash_loop finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Action, "stopped it to break the loop") {
		t.Errorf("action should mention the breaker, got %q", findings[0].Action)
	}
}

func TestCheckAgentLinkCrashAware(t *testing.T) {
	started := time.Now().Add(-10 * time.Minute)
	server := &storage.Server{Status: storage.StatusRunning, LastStarted: &started}

	m := &metrics.ServerMetrics{
		LastExitCrashed: true,
		LastExitedAt:    time.Now().Add(-time.Minute),
	}
	if findings := checkAgentLink(server, nil, m); len(findings) != 0 {
		t.Fatalf("recent crash should suppress agent findings, got %+v", findings)
	}

	m = &metrics.ServerMetrics{LastAgentSessionAt: time.Now().Add(-5 * time.Minute)}
	findings := checkAgentLink(server, nil, m)
	if len(findings) != 1 || findings[0].ID != "agent_dropped" {
		t.Fatalf("expected agent_dropped, got %+v", findings)
	}

	m = &metrics.ServerMetrics{}
	findings = checkAgentLink(server, nil, m)
	if len(findings) != 1 || findings[0].ID != "agent_unreachable" {
		t.Fatalf("expected agent_unreachable, got %+v", findings)
	}
}

func TestApplyFixDisableMod(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "etf.jar", map[string]string{
		"fabric.mod.json": `{"id":"entity_texture_features"}`,
	})
	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderModrinth}
	cfg := &storage.ServerProperties{}

	if _, err := ApplyFix(server, cfg, FixDisableMod, []string{"etf.jar"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modsDir, "etf.jar")); !os.IsNotExist(err) {
		t.Fatal("jar should have left the active mods dir")
	}
	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "etf.jar")); err != nil {
		t.Fatal("jar should be in the disabled dir")
	}
	if cfg.ModrinthExcludeFiles == nil || !strings.Contains(*cfg.ModrinthExcludeFiles, "etf.jar") {
		t.Fatalf("modrinth excludes should list the jar, got %v", cfg.ModrinthExcludeFiles)
	}

	if msg, err := ApplyFix(server, cfg, FixDisableMod, []string{"etf.jar"}); err != nil || !strings.Contains(msg, "already disabled") {
		t.Fatalf("expected idempotent success, got %q %v", msg, err)
	}

	if _, err := ApplyFix(server, cfg, FixDisableMod, []string{"../etf.jar"}); err == nil {
		t.Fatal("path traversal must error")
	}
	if _, err := ApplyFix(server, cfg, FixDisableMod, []string{"ghost.jar"}); err == nil {
		t.Fatal("missing mod must error")
	}
	if _, err := ApplyFix(server, cfg, FixDisableMod, nil); err == nil {
		t.Fatal("empty target list must error")
	}
}

func TestApplyFixDisablesMultipleMods(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "one.jar", map[string]string{"fabric.mod.json": `{"id":"one"}`})
	writeModJar(t, modsDir, "two.jar", map[string]string{"fabric.mod.json": `{"id":"two"}`})
	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderModrinth}
	cfg := &storage.ServerProperties{}

	msg, err := ApplyFix(server, cfg, FixDisableMod, []string{"one.jar", "two.jar"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "2 mods disabled") {
		t.Errorf("expected combined message, got %q", msg)
	}
	for _, name := range []string{"one.jar", "two.jar"} {
		if _, err := os.Stat(filepath.Join(modsDir+"_disabled", name)); err != nil {
			t.Errorf("%s should be disabled", name)
		}
	}
	if cfg.ModrinthExcludeFiles == nil || !strings.Contains(*cfg.ModrinthExcludeFiles, "one.jar") ||
		!strings.Contains(*cfg.ModrinthExcludeFiles, "two.jar") {
		t.Fatalf("excludes should list both jars, got %v", cfg.ModrinthExcludeFiles)
	}

	writeModJar(t, modsDir, "three.jar", map[string]string{"fabric.mod.json": `{"id":"three"}`})
	if _, err := ApplyFix(server, cfg, FixDisableMod, []string{"three.jar", "../evil.jar"}); err == nil {
		t.Fatal("batch with traversal must error")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "three.jar")); err != nil {
		t.Fatal("valid jar must stay when the batch fails validation")
	}
}

func hasEvidence(f Finding, substr string) bool {
	for _, e := range f.Evidence {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

type fakeStore struct {
	mu     sync.Mutex
	server *storage.Server
	cfg    *storage.ServerProperties
	saved  bool
}

func (f *fakeStore) GetServer(ctx context.Context, id string) (*storage.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.server, nil
}

func (f *fakeStore) GetServerProperties(ctx context.Context, id string) (*storage.ServerProperties, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg, nil
}

func (f *fakeStore) SaveServerProperties(ctx context.Context, cfg *storage.ServerProperties) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = true
	return nil
}

func (f *fakeStore) wasSaved() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saved
}

type fakeLifecycle struct {
	mu         sync.Mutex
	restarts   int
	stops      int
	stopSource string
	onRestart  func()
	called     chan string
}

func newFakeLifecycle() *fakeLifecycle {
	return &fakeLifecycle{called: make(chan string, 16)}
}

func (f *fakeLifecycle) Restart(ctx context.Context, id string) error {
	f.mu.Lock()
	f.restarts++
	hook := f.onRestart
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.called <- "restart"
	return nil
}

func (f *fakeLifecycle) Stop(ctx context.Context, id string) error {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
	f.called <- "stop"
	return nil
}

func (f *fakeLifecycle) StopRequestedBy(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopSource
}

func (f *fakeLifecycle) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarts, f.stops
}

func (f *fakeLifecycle) wait(t *testing.T) string {
	t.Helper()
	select {
	case action := <-f.called:
		return action
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle action never happened")
		return ""
	}
}

// Blocks until a started respond pass released the server lock
func waitDoctorIdle(t *testing.T, r *CrashResponder, serverID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	lock := r.serverLock(serverID)
	for time.Now().Before(deadline) {
		if lock.TryLock() {
			lock.Unlock()
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("doctor never went idle")
}

type fakeInstaller struct {
	mu       sync.Mutex
	installs []string
	fail     bool
	modsDir  string
}

func (f *fakeInstaller) InstallModByID(ctx context.Context, server *storage.Server, modID, versionRange string, dialects []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return "", fmt.Errorf("no such project")
	}
	name := modID + "-installed.jar"
	if f.modsDir != "" {
		if err := os.WriteFile(filepath.Join(f.modsDir, name), []byte("jar"), 0644); err != nil {
			return "", err
		}
	}
	f.installs = append(f.installs, modID)
	return name, nil
}

func testRecorder(t *testing.T) *activity.Recorder {
	t.Helper()
	tmp := t.TempDir()
	cfg, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	cfg.Database.Path = filepath.Join(tmp, "ledger.db")
	cfg.Storage.DataDir = tmp
	cfg.Storage.BackupDir = filepath.Join(tmp, "backups")
	dbStore, err := storage.NewSQLiteStore(cfg)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = dbStore.Close() })
	return activity.NewRecorder(dbStore, logger.New())
}

func testResponder(t *testing.T, store *fakeStore, lc *fakeLifecycle) (*CrashResponder, *metrics.Collector) {
	collector := metrics.NewCollector(nil, nil, nil, nil, logger.New(), metrics.CollectorConfig{})
	return &CrashResponder{
		Store:     store,
		Collector: collector,
		Lifecycle: lc,
		Rec:       testRecorder(t),
		Log:       logger.New(),
	}, collector
}

func crashExit(fatal *agentv1.FatalError) *agentv1.Exited {
	return &agentv1.Exited{
		ExitCode: 1, Crashed: true, ExitedAtUnixMs: time.Now().UnixMilli(),
		FatalError: fatal,
	}
}

func TestDoctorVerdictDisableVerifyAdopt(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "oculus.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"oculus\"\n",
	})
	writeModJar(t, modsDir, "keepme.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"keepme\"\n",
	})

	force := "keepme"
	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{ModrinthForceIncludeFiles: &force},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", crashExit(&agentv1.FatalError{
		FailedMods: []*agentv1.FailedMod{
			{ModId: "oculus", FileName: "oculus.jar", Reason: "fml.modloading.errorduringevent"},
			{ModId: "keepme", FileName: "keepme.jar"},
			{ModId: "ghost", FileName: "ghost.jar"},
		},
	}))
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "oculus.jar")); err != nil {
		t.Fatal("blamed jar should be disabled")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "keepme.jar")); err != nil {
		t.Fatal("force-included jar must stay")
	}
	if store.wasSaved() {
		t.Fatal("excludes must not persist before the boot is verified")
	}

	// The verifying boot succeeds, the repair becomes durable
	r.OnServerReady(context.Background(), "s1")
	if !store.wasSaved() {
		t.Fatal("verified repair must persist the pack excludes")
	}
	if store.cfg.ModrinthExcludeFiles == nil || !strings.Contains(*store.cfg.ModrinthExcludeFiles, "oculus.jar") {
		t.Fatalf("excludes should list the jar, got %v", store.cfg.ModrinthExcludeFiles)
	}
	if j := loadDoctor(dataPath); j.Incident != nil || j.Resolved == nil || j.Resolved.Outcome != "repaired" || j.Resolved.ClosedAt.IsZero() {
		t.Fatalf("journal should hold a closed resolved incident, got %+v", j)
	}
}

func TestDoctorMissingDepReenablesProvider(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "alpha.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"alpha\"\n[[dependencies.alpha]]\nmodId = \"beta\"\nmandatory = true\n",
	})
	writeModJar(t, modsDir+"_disabled", "beta.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"beta\"\n",
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.20.1", JavaVersion: "17"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", crashExit(&agentv1.FatalError{
		FailedMods: []*agentv1.FailedMod{
			{ModId: "alpha", FileName: "alpha.jar", Reason: "fml.modloading.missingdependency"},
		},
	}))
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir, "beta.jar")); err != nil {
		t.Fatal("disabled dependency should be re-enabled, not alpha disabled")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "alpha.jar")); err != nil {
		t.Fatal("the dependent mod must stay enabled")
	}
}

func TestDoctorMissingDepInstallsFromIndex(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "alpha.jar", map[string]string{
		"fabric.mod.json": `{"id":"alpha","version":"1.0","depends":{"beta":">=2.0"}}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.20.1", JavaVersion: "17"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)
	installer := &fakeInstaller{modsDir: modsDir}
	r.Installer = installer

	collashFatal := &agentv1.FatalError{
		FailedMods: []*agentv1.FailedMod{
			{ModId: "alpha", FileName: "alpha.jar", Reason: "fml.modloading.missingdependency"},
		},
	}
	collector.ApplyAgentExit("s1", crashExit(collashFatal))
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	installer.mu.Lock()
	installs := append([]string(nil), installer.installs...)
	installer.mu.Unlock()
	if len(installs) != 1 || installs[0] != "beta" {
		t.Fatalf("expected beta install, got %v", installs)
	}
	if _, err := os.Stat(filepath.Join(modsDir, "alpha.jar")); err != nil {
		t.Fatal("the dependent mod must stay enabled when the dep was sourced")
	}
}

const stoneblockReport = `---- Minecraft Crash Report ----
// On the bright side, I bought you a teddy bear!

Description: Mod loading failures have occurred; consult the issue messages for more details

-- Head --
Thread: main

-- Mod loading issue for: statuseffectbars --
Details:
	Mod file: /data/mods/statuseffectbars.jar
	Failure message: Status Effect Bars (statuseffectbars) has failed to load correctly
	Exception message: java.lang.RuntimeException: Attempted to load class net/minecraft/client/gui/screens/Screen for invalid dist DEDICATED_SERVER

-- Mod loading issue for: drippyloadingscreen --
Details:
	Mod file: /data/mods/drippy.jar
	Exception message: java.lang.RuntimeException: Attempted to load class net/minecraft/client/gui/screens/Screen for invalid dist DEDICATED_SERVER

-- System Details --
Details:
	Minecraft Version: 1.21.1
`

func TestParseReportMods(t *testing.T) {
	mods := parseReportMods(stoneblockReport)
	if len(mods) != 2 {
		t.Fatalf("expected 2 mods, got %+v", mods)
	}
	if mods[0].GetModId() != "statuseffectbars" || mods[0].GetFileName() != "/data/mods/statuseffectbars.jar" {
		t.Fatalf("first mod parsed wrong: %+v", mods[0])
	}
	if mods[1].GetModId() != "drippyloadingscreen" || mods[1].GetFileName() != "/data/mods/drippy.jar" {
		t.Fatalf("second mod parsed wrong: %+v", mods[1])
	}
	if !strings.Contains(mods[0].GetErrorMessage(), "invalid dist") {
		t.Fatalf("exception message missing: %+v", mods[0])
	}
}

func TestDoctorReportVerdictDisablesMods(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "statuseffectbars.jar", map[string]string{
		"fabric.mod.json": `{"id":"statuseffectbars","version":"1.0"}`,
	})
	writeModJar(t, modsDir, "drippy.jar", map[string]string{
		"fabric.mod.json": `{"id":"drippyloadingscreen","version":"3.1.2"}`,
	})
	writeModJar(t, modsDir, "keepme.jar", map[string]string{
		"fabric.mod.json": `{"id":"keepme","version":"1.0"}`,
	})
	reportDir := filepath.Join(dataPath, "crash-reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, "crash-fml.txt"), []byte(stoneblockReport), 0644); err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.21.1", JavaVersion: "21"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// No typed capture, the crash report is the verdict floor
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 0, Crashed: true, ExitedAtUnixMs: time.Now().UnixMilli(),
		CrashReportPath: "crash-reports/crash-fml.txt",
	})
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	disabled := listDir(t, modsDir+"_disabled")
	if len(disabled) != 2 {
		t.Fatalf("both report-blamed jars should be disabled, got %v", disabled)
	}
	if _, err := os.Stat(filepath.Join(modsDir, "keepme.jar")); err != nil {
		t.Fatal("unblamed jar must stay enabled")
	}

	r.OnServerReady(context.Background(), "s1")
	j := loadDoctor(dataPath)
	if j.Resolved == nil || j.Resolved.Outcome != "repaired" {
		t.Fatalf("expected resolved incident, got %+v", j)
	}
}

func TestDoctorExhaustRevertsAndStops(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "a.jar", map[string]string{
		"fabric.mod.json": `{"id":"a","version":"1.0"}`,
	})
	writeModJar(t, modsDir, "b.jar", map[string]string{
		"fabric.mod.json": `{"id":"b","version":"1.0"}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.20.1", JavaVersion: "17"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// The same verdict repeats until no untried repair remains
	fatal := &agentv1.FatalError{FailedMods: []*agentv1.FailedMod{
		{ModId: "a", FileName: "a.jar", Reason: "fml.modloading.errorduringevent"},
	}}
	base := time.Now()
	for i := range maxDoctorPasses {
		collector.ApplyAgentExit("s1", &agentv1.Exited{
			ExitCode: 1, Crashed: true,
			ExitedAtUnixMs: base.Add(time.Duration(i+1) * 10 * time.Millisecond).UnixMilli(),
			FatalError:     fatal,
		})
		r.OnCrashExit(context.Background(), "s1")
		if lc.wait(t) == "stop" {
			break
		}
		waitDoctorIdle(t, r, "s1")
	}

	if got := listDir(t, modsDir); len(got) != 2 {
		t.Fatalf("exhaustion must restore the pack, got %v", got)
	}
	j := loadDoctor(dataPath)
	if j.Incident != nil || j.Resolved == nil || j.Resolved.Outcome != "gave_up" {
		t.Fatalf("journal should record the give-up, got %+v", j)
	}
	if store.wasSaved() {
		t.Fatal("a reverted incident must not write excludes")
	}
}

func TestDoctorRuntimeCrashStaysHandsOff(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "a.jar", map[string]string{
		"fabric.mod.json": `{"id":"a","version":"1.0"}`,
	})
	writeModJar(t, modsDir, "b.jar", map[string]string{
		"fabric.mod.json": `{"id":"b","version":"1.0"}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// Three post-ready crashes, mods stay untouched, breaker fires
	base := time.Now().Add(-3 * time.Minute)
	for i := range 3 {
		collector.ApplyAgentExit("s1", &agentv1.Exited{
			ExitCode: 1, Crashed: true, WasReady: true,
			ExitedAtUnixMs: base.Add(time.Duration(i) * time.Minute).UnixMilli(),
			FatalError: &agentv1.FatalError{Causes: []*agentv1.CrashCause{{
				Type: "java.lang.NullPointerException",
				Frames: []*agentv1.CrashFrame{{
					ClassName: "mod.a.Ticker", MethodName: "tick",
					SourceLocation: "union:/data/mods/a.jar%231!/",
				}},
			}}},
		})
	}
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "stop" {
		t.Fatalf("expected the loop breaker, got %s", got)
	}
	if got := listDir(t, modsDir); len(got) != 2 {
		t.Fatalf("runtime crashes must not touch mods, got %v", got)
	}
}

func TestCrashResponderBreaksLoopWithoutProgress(t *testing.T) {
	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	base := time.Now().Add(-3 * time.Minute)
	for i := range 3 {
		collector.ApplyAgentExit("s1", &agentv1.Exited{
			ExitCode: 1, Crashed: true,
			ExitedAtUnixMs: base.Add(time.Duration(i) * time.Minute).UnixMilli(),
		})
	}
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "stop" {
		t.Fatalf("expected stop, got %s", got)
	}

	r.OnCrashExit(context.Background(), "s1")
	time.Sleep(100 * time.Millisecond)
	lc.mu.Lock()
	stops := lc.stops
	lc.mu.Unlock()
	if stops != 1 {
		t.Fatalf("breaker must fire once per window, got %d stops", stops)
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDoctorSwallowsExitsDuringRestart(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "oculus.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"oculus\"\n",
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// The policy boot crashes again while the doctor restarts
	lc.onRestart = func() {
		collector.ApplyAgentExit("s1", &agentv1.Exited{
			ExitCode: 1, Crashed: true,
			ExitedAtUnixMs: time.Now().Add(50 * time.Millisecond).UnixMilli(),
			FatalError: &agentv1.FatalError{FailedMods: []*agentv1.FailedMod{
				{ModId: "oculus", FileName: "oculus.jar", Reason: "fml.modloading.errorduringevent"},
			}},
		})
	}

	collector.ApplyAgentExit("s1", crashExit(&agentv1.FatalError{
		FailedMods: []*agentv1.FailedMod{
			{ModId: "oculus", FileName: "oculus.jar", Reason: "fml.modloading.errorduringevent"},
		},
	}))
	r.respond("s1")

	if restarts, _ := lc.counts(); restarts != 1 {
		t.Fatalf("expected one restart, got %d", restarts)
	}

	// The mid restart exit must not consume another pass
	r.respond("s1")
	restarts, stops := lc.counts()
	if restarts != 1 || stops != 0 {
		t.Fatalf("swallowed exit must not act, got %d restarts %d stops", restarts, stops)
	}
	if j := loadDoctor(dataPath); j.Incident == nil || j.Incident.Passes != 1 {
		t.Fatalf("expected a single pass, got %+v", j)
	}
}

func TestDoctorIgnoresStaleExit(t *testing.T) {
	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// A replayed exit from before a panel restart stays ignored
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 1, Crashed: true,
		ExitedAtUnixMs: time.Now().Add(-11 * time.Minute).UnixMilli(),
	})
	r.respond("s1")

	restarts, stops := lc.counts()
	if restarts != 0 || stops != 0 {
		t.Fatalf("stale exit must not act, got %d restarts %d stops", restarts, stops)
	}
	if j := loadDoctor(store.server.DataPath); j.Incident != nil {
		t.Fatalf("stale exit must not open an incident, got %+v", j.Incident)
	}
}

func TestDoctorBreaksLoopOnFailedStop(t *testing.T) {
	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	lc.stopSource = "nick"
	r, collector := testResponder(t, store, lc)

	// The user stop failed and the container keeps crash looping
	base := time.Now().Add(-3 * time.Minute)
	for i := range 3 {
		collector.ApplyAgentExit("s1", &agentv1.Exited{
			ExitCode: 1, Crashed: true,
			ExitedAtUnixMs: base.Add(time.Duration(i) * time.Minute).UnixMilli(),
		})
	}
	r.respond("s1")

	if got := lc.wait(t); got != "stop" {
		t.Fatalf("failed stop must reach the breaker, got %s", got)
	}
	if restarts, _ := lc.counts(); restarts != 0 {
		t.Fatalf("stand down must never restart, got %d", restarts)
	}
}

func TestDoctorCleanExitLoopBreaks(t *testing.T) {
	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	base := time.Now()
	for i := range 3 {
		collector.ApplyAgentExit("s1", &agentv1.Exited{
			ExitCode: 0, Crashed: false,
			ExitedAtUnixMs: base.Add(time.Duration(i) * time.Second).UnixMilli(),
		})
		r.respond("s1")
	}

	if got := lc.wait(t); got != "stop" {
		t.Fatalf("clean exit loop must break, got %s", got)
	}
	if got := collector.GetMetrics("s1").ExitsWithin(crashLoopWindow); got != 3 {
		t.Fatalf("expected 3 recorded exits, got %d", got)
	}
}

func TestDoctorCleanExitWithStopIntentNotCounted(t *testing.T) {
	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	lc.stopSource = "nick"
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 0, Crashed: false, ExitedAtUnixMs: time.Now().UnixMilli(),
	})
	r.respond("s1")

	if got := collector.GetMetrics("s1").ExitsWithin(crashLoopWindow); got != 0 {
		t.Fatalf("requested stop exits must not count, got %d", got)
	}
	restarts, stops := lc.counts()
	if restarts != 0 || stops != 0 {
		t.Fatalf("requested stop exit must not act, got %d restarts %d stops", restarts, stops)
	}
}

func TestCheckExitLoopFinding(t *testing.T) {
	server := &storage.Server{DataPath: t.TempDir(), ModLoader: storage.ModLoaderForge, Status: storage.StatusStopped}
	now := time.Now()
	last := now.Add(-time.Minute)
	m := &metrics.ServerMetrics{
		LastExitCode:       0,
		LastExitedAt:       last,
		UnexpectedExits:    []time.Time{now.Add(-3 * time.Minute), last},
		CrashLoopStoppedAt: now.Add(-time.Minute),
	}

	findings := checkCrash(server, m)
	if len(findings) != 1 || findings[0].ID != "exit_loop" {
		t.Fatalf("expected exit_loop finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Action, "break the loop") {
		t.Errorf("action should mention the breaker, got %q", findings[0].Action)
	}
	if !hasEvidence(findings[0], "unexpected exits") {
		t.Errorf("evidence should count the exits, got %v", findings[0].Evidence)
	}

	// A requested clean stop never reports a loop
	m.LastExitedAt = now
	if findings := checkCrash(server, m); len(findings) != 0 {
		t.Fatalf("requested stop must not report a loop, got %+v", findings)
	}
}

func TestCheckCrashMidIncidentHidesFix(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "oculus.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"oculus\"\n",
	})
	if err := saveDoctor(dataPath, &doctorState{Version: 1, Incident: &doctorIncident{
		OpenedAt: time.Now().Add(-time.Minute),
		Passes:   1,
		Actions:  []doctorAction{{Kind: actionDisable, File: "oculus.jar", Evidence: evidenceVerdict, AppliedAt: time.Now()}},
	}}); err != nil {
		t.Fatal(err)
	}

	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderForge, Status: storage.StatusStopped}
	m := &metrics.ServerMetrics{
		LastExitCrashed: true,
		LastExitCode:    1,
		LastExitedAt:    time.Now().Add(-time.Minute),
		LastFatalError: &agentv1.FatalError{
			FailedMods: []*agentv1.FailedMod{{ModId: "oculus", FileName: "oculus.jar"}},
		},
	}

	findings := checkCrash(server, m)
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %+v", findings)
	}
	f := findings[0]
	if f.FixID != "" || len(f.FixArgs) != 0 {
		t.Fatalf("open incident must hide the fix button, got %+v", f)
	}
	if !strings.Contains(f.Action, "repairing this now") {
		t.Errorf("action should narrate the live repair, got %q", f.Action)
	}
}

func TestCheckCrashJournalNarrationSurvivesRestart(t *testing.T) {
	dataPath := t.TempDir()
	exitedAt := time.Now().Add(-10 * time.Minute)
	if err := saveDoctor(dataPath, &doctorState{Version: 1, Resolved: &doctorIncident{
		OpenedAt: exitedAt,
		ClosedAt: exitedAt.Add(2 * time.Minute),
		Passes:   1,
		Outcome:  "repaired",
		Summary:  "disabled oculus.jar",
	}}); err != nil {
		t.Fatal(err)
	}

	// Panel memory is empty, only the journal knows the repair
	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderForge, Status: storage.StatusRunning}
	m := &metrics.ServerMetrics{
		LastExitCrashed: true,
		LastExitCode:    1,
		LastExitedAt:    exitedAt,
	}

	findings := checkCrash(server, m)
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %+v", findings)
	}
	f := findings[0]
	if f.ID != "repaired_crash" || f.Severity != v1.PerformanceSeverity_PERFORMANCE_SEVERITY_INFO {
		t.Fatalf("expected repaired_crash info finding, got %+v", f)
	}
	if f.Action != "DiscoPanel automatically disabled oculus.jar and restarted the server." {
		t.Errorf("narration should come from the journal, got %q", f.Action)
	}
	if f.FixID != "" {
		t.Errorf("handled crash must not offer a fix, got %q", f.FixID)
	}
}

func TestFindingForFix(t *testing.T) {
	findings := []Finding{
		{ID: "heap", FixID: FixResetHeap},
		{ID: "crash", FixID: FixDisableMod, FixArgs: []string{"one.jar", "two.jar"}},
	}

	if FindingForFix(findings, FixResetHeap, nil) == nil {
		t.Fatal("argless fix should match")
	}
	if FindingForFix(findings, FixResetHeap, []string{"one.jar"}) != nil {
		t.Fatal("args against an argless fix must not match")
	}
	if FindingForFix(findings, FixDisableMod, []string{"one.jar"}) == nil {
		t.Fatal("subset of offered args should match")
	}
	if FindingForFix(findings, FixDisableMod, []string{"one.jar", "ghost.jar"}) != nil {
		t.Fatal("unoffered arg must not match")
	}
	if FindingForFix(findings, FixDisableMod, nil) != nil {
		t.Fatal("empty args must not match a targeted fix")
	}
	if FindingForFix(findings, FixEnableZGC, nil) != nil {
		t.Fatal("unoffered fix must not match")
	}
}

const fabricReport = `---- Minecraft Crash Report ----
// I let you down. Sorry :(

Time: 2026-07-01 12:00:00
Description: Incompatible mods found!

net.fabricmc.loader.impl.FormattedException: Some of your mods are incompatible with the game or each other!
A potential solution has been determined, this may resolve your problem:
	 - Replace mod 'Sodium Extra' (sodiumextra) 0.4.10 with version 0.5 or later.
Unmet dependency listing:
	 - Mod 'Sodium Extra' (sodiumextra) 0.4.10 requires version 0.5 or later of 'Sodium' (sodium), but only the wrong version is present: 0.4.10!
	 - Mod 'Iris' (iris) 1.6.4 requires any version of 'Indium' (indium), which is missing!
	at net.fabricmc.loader.impl.FabricLoaderImpl.load(FabricLoaderImpl.java:190)

-- System Details --
Details:
	Minecraft Version: 1.20.1
	Fabric Mods:
		fabric-api: Fabric API 0.91.0+1.20.1
		iris: Iris 1.6.4
		sodium: Sodium 0.4.10
		sodiumextra: Sodium Extra 0.4.10
`

func TestParseFabricReportMods(t *testing.T) {
	mods := parseReportMods(fabricReport)
	if len(mods) != 2 {
		t.Fatalf("expected 2 blamed mods, got %+v", mods)
	}
	if mods[0].GetModId() != "sodiumextra" || mods[0].GetReason() != "missing_dependency" {
		t.Fatalf("first mod parsed wrong: %+v", mods[0])
	}
	if mods[1].GetModId() != "iris" || mods[1].GetReason() != "missing_dependency" {
		t.Fatalf("second mod parsed wrong: %+v", mods[1])
	}
	if !strings.Contains(mods[0].GetErrorMessage(), "requires version 0.5 or later") {
		t.Fatalf("error message missing: %+v", mods[0])
	}
	if got := classifyFailedMod(mods[0]); got != failMissingDep {
		t.Fatalf("fabric dep failure should classify as missing dep, got %s", got)
	}
}

func TestParseFabricReportIgnoresRosterOnlyText(t *testing.T) {
	healthy := `---- Minecraft Crash Report ----
Description: Ticking entity

java.lang.NullPointerException: boom
	at some.mod.Ticker.tick(Ticker.java:10)

-- System Details --
Details:
	Fabric Mods:
		fabric-api: Fabric API 0.91.0
		sodium: Sodium 0.5.3
`
	if mods := parseReportMods(healthy); len(mods) != 0 {
		t.Fatalf("roster without failure lines must not blame mods, got %+v", mods)
	}
}

func TestCollectorSnapshotIsolation(t *testing.T) {
	collector := metrics.NewCollector(nil, nil, nil, nil, logger.New(), metrics.CollectorConfig{})
	collector.ApplyAgentRoster("s1", []string{"alice", "bob"})

	m := collector.GetMetrics("s1")
	m.PlayerSample[0] = "mallory"
	m.PlayersOnline = 99

	fresh := collector.GetMetrics("s1")
	if fresh.PlayerSample[0] != "alice" || fresh.PlayersOnline != 2 {
		t.Fatalf("snapshot mutation leaked into the collector, got %+v", fresh)
	}
	if collector.GetMetrics("missing") != nil {
		t.Fatal("unknown server should read nil")
	}
}

func TestDoctorLinkageConvictsAccomplice(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "controllable.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"controllable\"\n",
	})
	writeModJar(t, modsDir, "framework.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"framework\"\n",
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.20.1", JavaVersion: "17"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// Loader blames framework, root frames run controllable's code
	verdict := &agentv1.FatalError{FailedMods: []*agentv1.FailedMod{{
		ModId:        "framework",
		FileName:     "framework.jar",
		ErrorType:    "java.lang.ClassNotFoundException",
		ErrorMessage: "Framework (framework) has failed to load correctly",
		Frames: []*agentv1.CrashFrame{
			{ClassName: "com.mrcrayfish.controllable.client.InputLibrary", MethodName: "<clinit>", SourceLocation: "union:/data/mods/controllable.jar%23744!/"},
			{ClassName: "com.mrcrayfish.framework.platform.ForgeConfigHelper", MethodName: "getAllFrameworkConfigs", SourceLocation: "union:/data/mods/framework.jar%23832!/"},
		},
	}}}
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 1, Crashed: true, ExitedAtUnixMs: time.Now().UnixMilli(),
		FatalError: verdict,
	})
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "controllable.jar")); err != nil {
		t.Fatal("the crashing frame owner should be disabled")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "framework.jar")); err != nil {
		t.Fatal("the blamed reporter must stay enabled")
	}

	// The same verdict again falls back to the reporter
	waitDoctorIdle(t, r, "s1")
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 1, Crashed: true, ExitedAtUnixMs: time.Now().UnixMilli() + 2000,
		FatalError: verdict,
	})
	r.OnCrashExit(context.Background(), "s1")
	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected second restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "framework.jar")); err != nil {
		t.Fatal("the reporter should be disabled on the retry")
	}
}

func TestAccompliceNeedsLinkageError(t *testing.T) {
	metas := []minecraft.ModJarMeta{
		{FileName: "other.jar", Mods: []minecraft.ModInfo{{ID: "other"}}},
	}
	fm := &agentv1.FailedMod{
		ModId:     "broken",
		FileName:  "broken.jar",
		ErrorType: "java.lang.IllegalStateException",
		Frames: []*agentv1.CrashFrame{
			{ClassName: "dev.other.Api", MethodName: "dispatch", SourceLocation: "file:/data/mods/other.jar"},
		},
	}
	if _, ok := accompliceAction(fm, "broken.jar", metas, nil); ok {
		t.Fatal("plain exceptions must not convict the frame owner")
	}
	fm.ErrorType = "java.lang.NoSuchMethodError"
	a, ok := accompliceAction(fm, "broken.jar", metas, nil)
	if !ok || a.File != "other.jar" {
		t.Fatalf("linkage failure should convict the frame owner, got %+v", a)
	}
	// The reporter crashing in its own code convicts itself
	fm.Frames[0].SourceLocation = "file:/data/mods/broken.jar"
	if _, ok := accompliceAction(fm, "broken.jar", metas, nil); ok {
		t.Fatal("self-owned frames must fall back to the reporter")
	}
}

func TestIncidentHeldFiles(t *testing.T) {
	dataPath := t.TempDir()
	if got := IncidentHeldFiles(dataPath); got != nil {
		t.Fatalf("no journal must hold nothing, got %v", got)
	}

	j := &doctorState{Version: 1, Incident: &doctorIncident{
		OpenedAt: time.Now(),
		Actions: []doctorAction{
			{Kind: actionDisable, File: "a.jar"},
			{Kind: actionDisable, File: "b.jar", Reverted: true},
			{Kind: actionEnable, File: "c.jar"},
		},
	}}
	if err := saveDoctor(dataPath, j); err != nil {
		t.Fatal(err)
	}
	if got := IncidentHeldFiles(dataPath); !reflect.DeepEqual(got, []string{"a.jar"}) {
		t.Fatalf("only live disables hold, got %v", got)
	}

	j.Resolved, j.Incident = j.Incident, nil
	if err := saveDoctor(dataPath, j); err != nil {
		t.Fatal(err)
	}
	if got := IncidentHeldFiles(dataPath); got != nil {
		t.Fatalf("closed incident must hold nothing, got %v", got)
	}
}

func TestDoctorStallFrameDisablesOwner(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "slowmod.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"slowmod\"\n",
	})
	writeModJar(t, modsDir, "innocent.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId = \"innocent\"\n",
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.20.1", JavaVersion: "17"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// A stall exit carries agent dump threads, root cause last
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 143, Crashed: true, BootFailed: true, ExitedAtUnixMs: time.Now().UnixMilli(),
		FatalError: &agentv1.FatalError{
			Thread: "Worker-Main-9",
			Causes: []*agentv1.CrashCause{
				{Type: "BootStall", Message: "Server thread is waiting", Frames: []*agentv1.CrashFrame{
					{ClassName: "jdk.internal.misc.Unsafe", MethodName: "park"},
					{ClassName: "net.minecraft.util.thread.BlockableEventLoop", MethodName: "waitForTasks", SourceLocation: "file:/server/libraries/server-1.20.1.jar"},
				}},
				{Type: "BootStall", Message: "Worker-Main-9 is waiting", Frames: []*agentv1.CrashFrame{
					{ClassName: "jdk.internal.misc.Unsafe", MethodName: "park"},
					{ClassName: "com.example.slowmod.worldgen.OreVeinComputer", MethodName: "awaitSeed", Line: 88, SourceLocation: "union:/data/mods/slowmod.jar%2312!/"},
				}},
			},
		},
	})
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "slowmod.jar")); err != nil {
		t.Fatal("the stalled frame owner should be disabled")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "innocent.jar")); err != nil {
		t.Fatal("uninvolved jars must stay enabled")
	}
}

func TestDoctorResolvesConnectorFoldedVerdict(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "particle-effects-1.2.jar", map[string]string{
		"fabric.mod.json": `{"id":"particle-effects","version":"1.2"}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth, MCVersion: "1.20.1", JavaVersion: "17"},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	// Connector reports the fabric id folded, with no file name
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 0, Crashed: true, BootFailed: true, ExitedAtUnixMs: time.Now().UnixMilli(),
		FatalError: &agentv1.FatalError{
			Causes: []*agentv1.CrashCause{{Type: "net.minecraftforge.fml.loading.EarlyLoadingException"}},
			FailedMods: []*agentv1.FailedMod{{
				ModId:     "particle_effects",
				ErrorType: "net.minecraftforge.fml.loading.EarlyLoadingException",
			}},
		},
	})
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "particle-effects-1.2.jar")); err != nil {
		t.Fatal("the folded verdict should map to the hyphen jar and disable it")
	}
}

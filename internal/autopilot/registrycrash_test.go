package autopilot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

const unboundLine = "Trying to access unbound value 'ResourceKey[minecraft:worldgen/structure / dungeons_arise:aviary]' from registry net.minecraft.class_2370$1@5fa52a21"

func TestParseUnboundRefs(t *testing.T) {
	refs := parseUnboundRefs(unboundLine)
	if len(refs) != 1 {
		t.Fatalf("expected one ref, got %+v", refs)
	}
	if refs[0].Registry != "minecraft:worldgen/structure" || refs[0].id() != "dungeons_arise:aviary" {
		t.Fatalf("wrong ref parsed, got %+v", refs[0])
	}

	dump := "Unbound values in registry ResourceKey[minecraft:root / minecraft:worldgen/structure]: [dungeons_arise:abandoned_temple, dungeons_arise:aviary, dungeons_arise:bathhouse]"
	refs = parseUnboundRefs(dump)
	if len(refs) != 3 {
		t.Fatalf("expected three refs from the dump, got %+v", refs)
	}
	for _, ref := range refs {
		if ref.Registry != "minecraft:worldgen/structure" || ref.Namespace != "dungeons_arise" {
			t.Fatalf("wrong dump ref, got %+v", ref)
		}
	}

	missing := "Missing element ResourceKey[minecraft:worldgen/biome / terralith:yellowstone]"
	refs = parseUnboundRefs(missing)
	if len(refs) != 1 || refs[0].id() != "terralith:yellowstone" {
		t.Fatalf("expected the missing element ref, got %+v", refs)
	}

	if refs := parseUnboundRefs("ResourceKey[minecraft:worldgen/structure / other:thing] with no cue words"); len(refs) != 0 {
		t.Fatalf("keys without cue words must not parse, got %+v", refs)
	}
}

func registryCrash() *agentv1.Exited {
	return crashExit(&agentv1.FatalError{
		Thread: "Server thread",
		Causes: []*agentv1.CrashCause{{
			Type:    "java.lang.IllegalStateException",
			Message: unboundLine,
		}},
	})
}

func TestDoctorRegistryDisablesDatapack(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "innocent.jar", map[string]string{
		"fabric.mod.json": `{"id":"innocent"}`,
	})
	packDir := filepath.Join(dataPath, "config", "paxi", "datapacks")
	writeModJar(t, packDir, "LessStructures.zip", map[string]string{
		"data/dungeons_arise/worldgen/structure_set/major_structures.json": `{"structures":[{"structure":"dungeons_arise:aviary"}]}`,
	})
	writeModJar(t, packDir, "unrelated.zip", map[string]string{
		"data/other/loot_tables/x.json": `{"pools":[]}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", registryCrash())
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(packDir+"_disabled", "LessStructures.zip")); err != nil {
		t.Fatal("referencing datapack should be disabled")
	}
	if _, err := os.Stat(filepath.Join(packDir, "unrelated.zip")); err != nil {
		t.Fatal("unrelated datapack must stay")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "innocent.jar")); err != nil {
		t.Fatal("innocent jar must stay enabled")
	}

	// The verifying boot succeeds, the exclude becomes durable
	r.OnServerReady(context.Background(), "s1")
	if store.cfg.ModrinthExcludeFiles == nil || !strings.Contains(*store.cfg.ModrinthExcludeFiles, "lessstructures.zip") {
		t.Fatalf("excludes should list the datapack, got %v", store.cfg.ModrinthExcludeFiles)
	}
	if j := loadDoctor(dataPath); j.Incident != nil || j.Resolved == nil || j.Resolved.Outcome != "repaired" {
		t.Fatalf("journal should hold a resolved incident, got %+v", j)
	}
}

func TestDoctorRegistryReenablesProvider(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "innocent.jar", map[string]string{
		"fabric.mod.json": `{"id":"innocent"}`,
	})
	writeModJar(t, modsDir+"_disabled", "DungeonsArise.jar", map[string]string{
		"fabric.mod.json": `{"id":"dungeons_arise"}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", registryCrash())
	r.OnCrashExit(context.Background(), "s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(modsDir, "DungeonsArise.jar")); err != nil {
		t.Fatal("namespace provider should be re-enabled")
	}
}

func TestDoctorVanillaDatapackRepair(t *testing.T) {
	dataPath := t.TempDir()
	packDir := filepath.Join(dataPath, "world", "datapacks")
	writeModJar(t, packDir, "BrokenPack.zip", map[string]string{
		"data/dungeons_arise/worldgen/structure_set/major.json": `{"structures":[{"structure":"dungeons_arise:aviary"}]}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderVanilla},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", registryCrash())
	r.respond("s1")

	if got := lc.wait(t); got != "restart" {
		t.Fatalf("vanilla server should get the doctor, got %s", got)
	}
	if _, err := os.Stat(filepath.Join(packDir+"_disabled", "BrokenPack.zip")); err != nil {
		t.Fatal("referencing datapack should be disabled on vanilla")
	}

	r.OnServerReady(context.Background(), "s1")
	if j := loadDoctor(dataPath); j.Incident != nil || j.Resolved == nil || j.Resolved.Outcome != "repaired" {
		t.Fatalf("journal should hold a resolved incident, got %+v", j)
	}
}

func TestDoctorStandsDownOnUserStop(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "innocent.jar", map[string]string{
		"fabric.mod.json": `{"id":"innocent"}`,
	})

	packDir := filepath.Join(dataPath, "config", "paxi", "datapacks")
	writeModJar(t, packDir, "LessStructures.zip", map[string]string{
		"data/dungeons_arise/worldgen/structure_set/major_structures.json": `{"structures":[{"structure":"dungeons_arise:aviary"}]}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	lc.stopSource = "nick"
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", registryCrash())
	r.respond("s1")

	restarts, stops := lc.counts()
	if restarts != 0 || stops != 0 {
		t.Fatalf("doctor must not act after a user stop, got %d restarts %d stops", restarts, stops)
	}
	if _, err := os.Stat(filepath.Join(packDir, "LessStructures.zip")); err != nil {
		t.Fatal("nothing may change on disk after a user stop")
	}
	if j := loadDoctor(dataPath); j.Incident != nil {
		t.Fatalf("no incident should open after a user stop, got %+v", j.Incident)
	}

	// Doctor stops stand down too, breaker owns loops
	lc.stopSource = doctorSource
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 1, Crashed: true, ExitedAtUnixMs: time.Now().Add(time.Second).UnixMilli(),
	})
	r.respond("s1")
	restarts, stops = lc.counts()
	if restarts != 0 || stops != 0 {
		t.Fatalf("doctor stop intent must stand down, got %d restarts %d stops", restarts, stops)
	}
}

func TestDoctorStandDownKeepsIncidentForNextStart(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeModJar(t, modsDir, "innocent.jar", map[string]string{
		"fabric.mod.json": `{"id":"innocent"}`,
	})
	packDir := filepath.Join(dataPath, "config", "paxi", "datapacks")
	writeModJar(t, packDir, "LessStructures.zip", map[string]string{
		"data/dungeons_arise/worldgen/structure_set/major_structures.json": `{"structures":[{"structure":"dungeons_arise:aviary"}]}`,
	})

	store := &fakeStore{
		server: &storage.Server{ID: "s1", DataPath: dataPath, ModLoader: storage.ModLoaderModrinth},
		cfg:    &storage.ServerProperties{},
	}
	lc := newFakeLifecycle()
	r, collector := testResponder(t, store, lc)

	collector.ApplyAgentExit("s1", registryCrash())
	r.respond("s1")
	if got := lc.wait(t); got != "restart" {
		t.Fatalf("expected restart, got %s", got)
	}

	// A user stop lands before the next crash gets processed
	lc.stopSource = "nick"
	collector.ApplyAgentExit("s1", &agentv1.Exited{
		ExitCode: 143, Crashed: true, ExitedAtUnixMs: time.Now().Add(time.Second).UnixMilli(),
	})
	r.respond("s1")

	restarts, _ := lc.counts()
	if restarts != 1 {
		t.Fatalf("stand down must not restart again, got %d restarts", restarts)
	}
	if j := loadDoctor(dataPath); j.Incident == nil || len(j.Incident.Actions) != 1 {
		t.Fatalf("open incident should survive the stand down, got %+v", j)
	}
}

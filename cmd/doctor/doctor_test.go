package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickheyer/discopanel/pkg/indexers"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

func writeModJar(t *testing.T, dir, name, manifest string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
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

type fakePanel struct {
	restarts, stops int
}

func (f *fakePanel) Restart(ctx context.Context, serverID string) error {
	f.restarts++
	return nil
}

func (f *fakePanel) Stop(ctx context.Context, serverID string) error {
	f.stops++
	return nil
}

func (f *fakePanel) ForcePatterns(ctx context.Context, serverID string) []string {
	return nil
}

func testServer(t *testing.T) *serverInfo {
	return &serverInfo{
		ID:        "s1",
		Name:      "s1",
		DataPath:  t.TempDir(),
		ModLoader: v1.ModLoader_MOD_LOADER_FABRIC,
		McVersion: "1.20.1",
	}
}

func crashExit(fatal *agentv1.FatalError) *agentv1.Exited {
	return &agentv1.Exited{
		ExitCode:       1,
		Crashed:        true,
		BootFailed:     true,
		ExitedAtUnixMs: time.Now().UnixMilli(),
		FatalError:     fatal,
	}
}

func TestRespondDisablesVerdictMod(t *testing.T) {
	srv := testServer(t)
	modsDir := filepath.Join(srv.DataPath, "mods")
	writeModJar(t, modsDir, "badmod.jar", `{"id":"badmod","environment":"*"}`)

	exit := crashExit(&agentv1.FatalError{FailedMods: []*agentv1.FailedMod{{
		ModId: "badmod", Reason: "mod_error", ErrorType: "java.lang.NoClassDefFoundError",
	}}})
	runtimespec.AppendExitHistory(srv.DataPath, exit)

	panel := &fakePanel{}
	e := &engine{panel: panel, logf: t.Logf}
	e.checkServer(context.Background(), srv)

	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "badmod.jar")); err != nil {
		t.Fatal("verdict mod should be disabled")
	}
	if panel.restarts != 1 {
		t.Fatalf("expected one verify restart, got %d", panel.restarts)
	}
	j := runtimespec.LoadDoctor(srv.DataPath)
	if j.Incident == nil || len(j.Incident.Actions) != 1 {
		t.Fatalf("expected one journaled action, got %+v", j.Incident)
	}
	if j.LastHandledMs != exit.ExitedAtUnixMs {
		t.Fatal("exit must be stamped handled")
	}

	// The same exit never triggers a second pass
	e.checkServer(context.Background(), srv)
	if panel.restarts != 1 {
		t.Fatalf("replayed exit must not act, got %d restarts", panel.restarts)
	}
}

func TestQuietRunningServerResolvesIncident(t *testing.T) {
	srv := testServer(t)
	srv.Running = true
	j := &runtimespec.DoctorState{Version: 1, Incident: &runtimespec.DoctorIncident{
		OpenedAt: time.Now().Add(-10 * time.Minute),
		Passes:   1,
		Actions: []runtimespec.DoctorAction{{
			Kind: runtimespec.ActionDisable, File: "badmod.jar", AppliedAt: time.Now().Add(-5 * time.Minute),
		}},
	}}
	if err := runtimespec.SaveDoctor(srv.DataPath, j); err != nil {
		t.Fatal(err)
	}

	e := &engine{panel: &fakePanel{}, logf: t.Logf}
	e.checkServer(context.Background(), srv)

	got := runtimespec.LoadDoctor(srv.DataPath)
	if got.Incident != nil {
		t.Fatal("incident should be closed after a quiet running window")
	}
	if got.Resolved == nil || got.Resolved.Outcome != "repaired" {
		t.Fatalf("expected repaired outcome, got %+v", got.Resolved)
	}
	if len(got.Excludes) != 1 || got.Excludes[0] != "badmod.jar" {
		t.Fatalf("disable should become a durable exclude, got %v", got.Excludes)
	}
}

func TestWantedStopStandsDown(t *testing.T) {
	srv := testServer(t)
	srv.Stopped = true
	runtimespec.AppendExitHistory(srv.DataPath, crashExit(nil))

	panel := &fakePanel{}
	e := &engine{panel: panel, logf: t.Logf}
	e.checkServer(context.Background(), srv)

	if panel.restarts != 0 || panel.stops != 0 {
		t.Fatalf("wanted stop must not act, got %d restarts %d stops", panel.restarts, panel.stops)
	}
}

func TestOrderSourcersPrefersPackSource(t *testing.T) {
	infos := []indexers.IndexerInfo{
		{Name: "aaa"},
		{Name: "mmm", PackSource: "source-one"},
		{Name: "zzz", PackSource: "source-two"},
	}

	got := orderSourcers(infos, "source-two")
	if len(got) != 3 || got[0].Name != "zzz" || got[1].Name != "aaa" || got[2].Name != "mmm" {
		t.Fatalf("pack source must lead, got %+v", got)
	}

	got = orderSourcers(infos, "")
	if len(got) != 3 || got[0].Name != "aaa" || got[1].Name != "mmm" || got[2].Name != "zzz" {
		t.Fatalf("no pack source keeps registry order, got %+v", got)
	}
}

func TestParseUnboundRefs(t *testing.T) {
	refs := parseUnboundRefs("Unbound values in registry ResourceKey[minecraft:root / minecraft:item]: [somemod:gadget, somemod:widget]")
	if len(refs) != 2 || refs[0].Namespace != "somemod" || refs[0].Path != "gadget" {
		t.Fatalf("unexpected refs %+v", refs)
	}
}

func TestParseForgeIssueMods(t *testing.T) {
	text := "-- Mod loading issue for: brokenmod --\n" +
		"Mod file: brokenmod-1.0.jar\n" +
		"Exception message: boom\n" +
		"-- System Details --\n"
	mods := parseReportMods(text)
	if len(mods) != 1 || mods[0].GetModId() != "brokenmod" || mods[0].GetFileName() != "brokenmod-1.0.jar" {
		t.Fatalf("unexpected mods %+v", mods)
	}
}

func TestExitHistoryRing(t *testing.T) {
	dir := t.TempDir()
	for i := range 25 {
		runtimespec.AppendExitHistory(dir, &agentv1.Exited{ExitCode: 1, ExitedAtUnixMs: int64(i + 1)})
	}
	history := runtimespec.ReadExitHistory(dir)
	if len(history) != 20 {
		t.Fatalf("ring should cap at 20, got %d", len(history))
	}
	if history[len(history)-1].ExitedAtUnixMs != 25 {
		t.Fatal("newest entry must survive")
	}
	// Duplicate stamps never append twice
	runtimespec.AppendExitHistory(dir, &agentv1.Exited{ExitCode: 1, ExitedAtUnixMs: 25})
	if got := runtimespec.ReadExitHistory(dir); len(got) != 20 {
		t.Fatalf("duplicate must not append, got %d", len(got))
	}
}

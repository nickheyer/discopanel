package provisioner

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers/modrinth"
	"github.com/nickheyer/discopanel/pkg/logger"
)

func writeClientJar(t *testing.T, dir, name, manifest string) {
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

func TestDisableClientOnlyMods(t *testing.T) {
	dataPath := t.TempDir()
	modsDir := filepath.Join(dataPath, "mods")
	writeClientJar(t, modsDir, "clientmod.jar", `{"id":"clientmod","environment":"client"}`)
	writeClientJar(t, modsDir, "servermod.jar", `{"id":"servermod","environment":"*"}`)
	writeClientJar(t, modsDir, "keepme.jar", `{"id":"keepme","environment":"client"}`)

	p := &Provisioner{log: logger.New()}
	server := &storage.Server{DataPath: dataPath, ModLoader: storage.ModLoaderModrinth}
	p.disableClientOnlyMods(context.Background(), server, []string{"keepme"})

	if _, err := os.Stat(filepath.Join(modsDir+"_disabled", "clientmod.jar")); err != nil {
		t.Fatal("client-only jar should be disabled")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "servermod.jar")); err != nil {
		t.Fatal("server-safe jar must stay")
	}
	if _, err := os.Stat(filepath.Join(modsDir, "keepme.jar")); err != nil {
		t.Fatal("force-included jar must stay")
	}
}

func TestEnsureGatesEULABeforeInstall(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.DataDir = t.TempDir()
	p := New(nil, nil, cfg, nil, logger.New())
	server := &storage.Server{ID: "s1", Name: "s1", DataPath: t.TempDir(), ModLoader: storage.ModLoaderVanilla, MCVersion: "1.21.1"}

	_, err := p.Ensure(context.Background(), server, &storage.ServerProperties{})
	if err == nil || !strings.Contains(err.Error(), "EULA") {
		t.Fatalf("expected EULA gate before install, got %v", err)
	}
}

func TestOverrideWhitelistTruncates(t *testing.T) {
	p := testProvisioner(t)
	server := &storage.Server{ID: "s1", Name: "s1", DataPath: t.TempDir()}
	path := filepath.Join(server.DataPath, "whitelist.json")
	seed := `[{"uuid":"0-0-0-0-0","name":"steve"}]`
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cfgRow := &storage.ServerProperties{}

	// Empty list without override leaves the file alone
	if err := p.writePlayerListFile(ctx, server, cfgRow, "whitelist.json", "", false, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != seed {
		t.Fatalf("merge mode must not touch the file, got %s", data)
	}

	// Explicit override with an empty list truncates
	if err := p.writePlayerListFile(ctx, server, cfgRow, "whitelist.json", "", false, true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("override with empty list must truncate, got %s", data)
	}
}

func TestManagementSecretPersists(t *testing.T) {
	p := testProvisioner(t)
	server := &storage.Server{ID: "s1", Name: "s1", DataPath: t.TempDir(), Port: 25565}
	cfgRow := &storage.ServerProperties{}

	if err := p.writeServerProperties(server, cfgRow, "1.21.9"); err != nil {
		t.Fatal(err)
	}
	first := readServerProperty(server.DataPath, "management-server-secret")
	if len(first) != 40 {
		t.Fatalf("expected a 40 char secret, got %q", first)
	}
	if err := p.writeServerProperties(server, cfgRow, "1.21.9"); err != nil {
		t.Fatal(err)
	}
	if again := readServerProperty(server.DataPath, "management-server-secret"); again != first {
		t.Fatalf("secret must persist across Ensure, %q became %q", first, again)
	}
}

func TestModrinthStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	state := readModrinthState(dir)
	if len(state.Projects) != 0 {
		t.Fatalf("fresh state should be empty, got %+v", state.Projects)
	}
	state.Projects["sodium"] = modrinthProjectState{
		VersionID:    "abc",
		FileName:     "sodium-0.5.jar",
		MCVersion:    "1.20.1",
		Loader:       "fabric",
		RequiredDeps: []string{"fabric-api"},
	}
	if err := writeModrinthState(dir, state); err != nil {
		t.Fatal(err)
	}
	again := readModrinthState(dir)
	entry, ok := again.Projects["sodium"]
	if !ok || entry.FileName != "sodium-0.5.jar" || entry.MCVersion != "1.20.1" ||
		entry.Loader != "fabric" || len(entry.RequiredDeps) != 1 {
		t.Fatalf("state round trip mismatch, got %+v", again.Projects)
	}
}

func TestPickAllowedVersion(t *testing.T) {
	versions := []modrinth.Version{
		{ID: "b1", VersionType: "beta"},
		{ID: "a1", VersionType: "alpha"},
	}
	if pick := pickAllowedVersion(versions, "release"); pick != nil {
		t.Fatalf("release channel must reject beta and alpha, got %+v", pick)
	}
	if pick := pickAllowedVersion(versions, "beta"); pick == nil || pick.ID != "b1" {
		t.Fatalf("beta channel should pick b1, got %+v", pick)
	}
	if pick := pickAllowedVersion(versions, "alpha"); pick == nil || pick.ID != "b1" {
		t.Fatalf("alpha channel allows beta too, got %+v", pick)
	}
	got := strings.Join(versionTypesOf(versions), ",")
	if got != "beta,alpha" {
		t.Fatalf("expected beta,alpha, got %s", got)
	}
}

func writeVersionJar(t *testing.T, path, versionJSON string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("version.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(versionJSON)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestServerPackMCVersionEvidence(t *testing.T) {
	if v := forgeArgsMCVersion("libraries/net/minecraftforge/forge/1.20.1-47.2.20/unix_args.txt"); v != "1.20.1" {
		t.Fatalf("forge args path should testify 1.20.1, got %q", v)
	}
	if v := forgeArgsMCVersion("libraries/net/neoforged/neoforge/20.4.237/unix_args.txt"); v != "" {
		t.Fatalf("neoforge version dirs must not testify, got %q", v)
	}

	dataPath := t.TempDir()
	writeVersionJar(t, filepath.Join(dataPath, "server.jar"), `{"id":"1.12.2","name":"1.12.2","world_version":1343}`)
	if v := jarMCVersion(filepath.Join(dataPath, "server.jar")); v != "1.12.2" {
		t.Fatalf("vanilla version.json should testify 1.12.2, got %q", v)
	}

	// Forge launch profiles lack world_version and never testify
	writeVersionJar(t, filepath.Join(dataPath, "forge.jar"), `{"id":"1.12.2-forge1.12.2-14.23.5.2860","inheritsFrom":"1.12.2"}`)
	if v := jarMCVersion(filepath.Join(dataPath, "forge.jar")); v != "" {
		t.Fatalf("forge profile must not testify, got %q", v)
	}
}

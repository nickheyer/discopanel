package provisioner

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	storage "github.com/nickheyer/discopanel/internal/db"
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

package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUnderContainment(t *testing.T) {
	base := t.TempDir()

	good := []string{".", "", "foo", "foo/bar", "foo/../baz"}
	for _, p := range good {
		if _, err := ResolveUnder(base, p); err != nil {
			t.Errorf("path %q rejected: %v", p, err)
		}
	}

	bad := []string{"..", "../x", "foo/../../x", "../" + filepath.Base(base) + "bar"}
	for _, p := range bad {
		if got, err := ResolveUnder(base, p); err == nil {
			t.Errorf("path %q escaped to %q", p, got)
		}
	}
}

func TestFindWorldDirReadsLevelName(t *testing.T) {
	dir := t.TempDir()
	worldPath := filepath.Join(dir, "myrealm")
	if err := os.MkdirAll(worldPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldPath, "level.dat"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	props := "#Minecraft server properties\nmotd=hi\nlevel-name=myrealm\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := FindWorldDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != worldPath {
		t.Errorf("got %q want %q", got, worldPath)
	}
}

func TestFindWorldDirDefaultsToWorld(t *testing.T) {
	dir := t.TempDir()
	worldPath := filepath.Join(dir, "world")
	if err := os.MkdirAll(worldPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldPath, "level.dat"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := FindWorldDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != worldPath {
		t.Errorf("got %q want %q", got, worldPath)
	}
}

func TestFindWorldDirMissingWorldErrors(t *testing.T) {
	if _, err := FindWorldDir(t.TempDir()); err == nil {
		t.Error("expected error for missing world")
	}
}

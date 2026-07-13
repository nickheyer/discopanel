package minecraft

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeTestJar(t *testing.T, dir, name string, files map[string]string) {
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
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScanModsDir(t *testing.T) {
	dir := t.TempDir()
	writeTestJar(t, dir, "etf.jar", map[string]string{
		"fabric.mod.json":                  `{"id":"entity_texture_features","environment":"client"}`,
		"traben/etf/ETFClientCommon.class": "x",
		"traben/etf/utils/ETFUtils.class":  "x",
	})
	writeTestJar(t, dir, "serverlib.jar", map[string]string{
		"META-INF/mods.toml":              "[[mods]]\nmodId = \"serverlib\"\n",
		"dev/example/serverlib/Lib.class": "x",
	})
	writeTestJar(t, dir, "clientforge.jar", map[string]string{
		"META-INF/mods.toml": "clientSideOnly = true\n[[mods]]\nmodId = \"clientforge\"\n",
	})
	// Mirrors ETF's forge build, client-only via dependency sides
	writeTestJar(t, dir, "etf-forge.jar", map[string]string{
		"META-INF/mods.toml": `modLoader = "javafml"
loaderVersion = "[33,)"
license = "LGPL-3.0"

[[mods]]
modId = "entity_texture_features"
version = "7.0.6"
description = '''
Multi-line
description
'''

[[dependencies.entity_texture_features]]
modId="forge"
mandatory=true
versionRange="[33,)"
side="CLIENT"

[[dependencies.entity_texture_features]]
modId = "minecraft"
mandatory = true
versionRange = "[1,)"
side = "CLIENT"
`,
	})
	// Server mods with client-side optional deps stay enabled
	writeTestJar(t, dir, "serverdeps.jar", map[string]string{
		"META-INF/mods.toml": `[[mods]]
modId = "serverdeps"

[[dependencies.serverdeps]]
modId = "minecraft"
mandatory = true
side = "BOTH"

[[dependencies.serverdeps]]
modId = "somoclientlib"
mandatory = false
side = "CLIENT"
`,
	})

	metas := ScanModsDir(dir)
	if len(metas) != 5 {
		t.Fatalf("expected 5 jars scanned, got %d", len(metas))
	}

	byFile := map[string]ModJarMeta{}
	for _, m := range metas {
		byFile[m.FileName] = m
	}

	etf := byFile["etf.jar"]
	if !etf.HasModID("entity_texture_features") || !etf.ClientOnly {
		t.Fatalf("etf.jar should be client-only entity_texture_features, got %+v", etf)
	}

	lib := byFile["serverlib.jar"]
	if !lib.HasModID("serverlib") || lib.ClientOnly {
		t.Fatalf("serverlib.jar should be a server-safe mod, got %+v", lib)
	}

	cf := byFile["clientforge.jar"]
	if !cf.ClientOnly {
		t.Fatalf("clientforge.jar should honor clientSideOnly, got %+v", cf)
	}

	etfForge := byFile["etf-forge.jar"]
	if !etfForge.ClientOnly || !etfForge.HasModID("entity_texture_features") {
		t.Fatalf("etf-forge.jar should be client-only via dep sides, got %+v", etfForge)
	}

	sd := byFile["serverdeps.jar"]
	if sd.ClientOnly {
		t.Fatalf("serverdeps.jar must stay server-safe, got %+v", sd)
	}

	// Cache returns identical results on unchanged directory
	again := ScanModsDir(dir)
	if len(again) != len(metas) {
		t.Fatalf("cached rescan should match, got %d", len(again))
	}

	// Missing directory scans to nothing
	if metas := ScanModsDir(filepath.Join(dir, "nope")); metas != nil {
		t.Fatalf("missing dir should scan nil, got %+v", metas)
	}
}

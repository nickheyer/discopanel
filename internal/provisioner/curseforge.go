package provisioner

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers/fuego"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// cfManifest is the manifest.json found inside CurseForge pack zips.
type cfManifest struct {
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"` // e.g. "forge-47.2.0"
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Overrides string `json:"overrides"`
	Files     []struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	} `json:"files"`
}

// BlockedMod describes a mod whose author disallows automated downloads.
type BlockedMod struct {
	Name     string `json:"name"`
	FileName string `json:"file_name"`
	URL      string `json:"url"`
}

// BlockedModsError is returned when a CurseForge pack contains mods that must
// be downloaded manually. Uploading the listed files into the mods folder and
// starting again resolves it.
type BlockedModsError struct {
	Mods []BlockedMod
}

func (e *BlockedModsError) Error() string {
	names := make([]string, 0, len(e.Mods))
	for _, m := range e.Mods {
		entry := m.FileName
		if m.URL != "" {
			entry += " (" + m.URL + ")"
		}
		names = append(names, entry)
	}
	return fmt.Sprintf(
		"%d mod(s) cannot be downloaded automatically because their authors disabled API distribution. Download them manually and upload them to the mods folder, then start the server again: %s",
		len(e.Mods), strings.Join(names, ", "))
}

// installCurseForgePack installs a CurseForge modpack from the API or a local zip.
func (p *Provisioner) installCurseForgePack(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, desired *desiredModpack, force bool) (*Result, error) {
	// Locally uploaded pack zip.
	if desired.source == "zip" {
		rel := strings.TrimPrefix(filepath.ToSlash(desired.id), "/data/")
		zipPath := joinData(server.DataPath, rel)
		if !fileExists(zipPath) {
			return nil, fmt.Errorf("modpack zip %q not found in the server data directory", rel)
		}
		return p.installCurseForgeZip(ctx, server, cfg, zipPath, nil, force)
	}

	client, err := p.curseForgeClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if desired.id == "" {
		return nil, fmt.Errorf("no CurseForge modpack configured (set the page URL or slug)")
	}

	pack, err := client.GetModBySlug(ctx, desired.id, fuego.ModpackClassID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve CurseForge pack %q: %w", desired.id, err)
	}

	file, err := p.resolveCurseForgeFile(ctx, client, pack, desired.versionID)
	if err != nil {
		return nil, err
	}
	desired.versionID = strconv.Itoa(file.ID)

	// Prefer the author-curated server pack when one exists.
	dlFile := file
	if file.ServerPackFileID != nil && *file.ServerPackFileID > 0 {
		serverFile, err := client.GetFile(ctx, pack.ID, *file.ServerPackFileID)
		if err == nil {
			dlFile = serverFile
			p.progress(server, "using server pack %s", serverFile.FileName)
		}
	}

	dlURL := dlFile.DownloadURL
	if dlURL == "" {
		dlURL, err = client.GetFileDownloadURL(ctx, pack.ID, dlFile.ID)
		if err != nil {
			return nil, err
		}
	}
	if dlURL == "" {
		return nil, fmt.Errorf("the modpack file %q is not distributable via the CurseForge API; download it manually and upload it as a modpack zip", dlFile.FileName)
	}

	p.progress(server, "downloading %s (%s)...", pack.Name, dlFile.FileName)
	zipPath := filepath.Join(installerDir(server.DataPath), "modpack.zip")
	if err := p.download(ctx, dlURL, zipPath, cfChecksum(dlFile), nil); err != nil {
		return nil, err
	}

	return p.installCurseForgeZip(ctx, server, cfg, zipPath, client, force)
}

// curseForgeClient builds a fuego client from the server or global API key.
func (p *Provisioner) curseForgeClient(ctx context.Context, cfg *storage.ServerConfig) (*fuego.Client, error) {
	apiKey := strVal(cfg.CFAPIKey)
	if apiKey == "" {
		if global, _, err := p.store.GetGlobalSettings(ctx); err == nil && global != nil {
			apiKey = strVal(global.CFAPIKey)
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("a CurseForge API key is required for CurseForge modpacks (set it in the server or global settings)")
	}
	return fuego.NewClient(apiKey, p.cfg), nil
}

// resolveCurseForgeFile picks the pack file: pinned id, main file, or newest.
func (p *Provisioner) resolveCurseForgeFile(ctx context.Context, client *fuego.Client, pack *fuego.Modpack, fileID string) (*fuego.File, error) {
	if fileID != "" {
		id, err := strconv.Atoi(fileID)
		if err != nil {
			return nil, fmt.Errorf("invalid CurseForge file id %q", fileID)
		}
		return client.GetFile(ctx, pack.ID, id)
	}
	if pack.MainFileID > 0 {
		if f, err := client.GetFile(ctx, pack.ID, pack.MainFileID); err == nil {
			return f, nil
		}
	}
	files, err := client.GetModpackFiles(ctx, pack.ID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("CurseForge pack %q has no files", pack.Slug)
	}
	newest := files[0]
	for _, f := range files[1:] {
		if f.FileDate.After(newest.FileDate) {
			newest = f
		}
	}
	return &newest, nil
}

// installCurseForgeZip installs a pack zip: manifest-driven when manifest.json
// exists, otherwise treated as a ready-made server pack.
func (p *Provisioner) installCurseForgeZip(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, zipPath string, client *fuego.Client, force bool) (*Result, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open modpack zip: %w", err)
	}
	defer reader.Close()

	manifest, manifestPrefix := readCFManifest(&reader.Reader)
	if manifest != nil {
		return p.installFromCFManifest(ctx, server, cfg, reader, manifest, manifestPrefix, client, force)
	}

	// No manifest: this is a ready-made server pack. Unpack it wholesale.
	p.progress(server, "extracting server pack...")
	if err := p.extractServerPack(reader, server.DataPath, !force); err != nil {
		return nil, err
	}
	return p.completeServerPack(ctx, server, cfg)
}

// readCFManifest finds manifest.json at the zip root or under one top-level dir.
func readCFManifest(reader *zip.Reader) (*cfManifest, string) {
	for _, f := range reader.File {
		name := f.Name
		prefix := ""
		if idx := strings.Index(name, "/"); idx >= 0 && strings.Count(name, "/") == 1 {
			prefix = name[:idx+1]
			name = name[idx+1:]
		}
		if name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		var m cfManifest
		err = json.NewDecoder(rc).Decode(&m)
		rc.Close()
		if err == nil && m.Minecraft.Version != "" {
			return &m, prefix
		}
	}
	return nil, ""
}

// installFromCFManifest performs a manifest-driven install: overrides, mod
// downloads (with blocked-mod detection), then the declared loader.
func (p *Provisioner) installFromCFManifest(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, reader *zip.ReadCloser, manifest *cfManifest, prefix string, client *fuego.Client, force bool) (*Result, error) {
	if client == nil {
		var err error
		client, err = p.curseForgeClient(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}

	p.progress(server, "installing %s %s (MC %s)...", manifest.Name, manifest.Version, manifest.Minecraft.Version)

	// Apply overrides.
	overrides := manifest.Overrides
	if overrides == "" {
		overrides = "overrides"
	}
	if err := p.extractZipPrefix(reader, prefix+overrides+"/", server.DataPath, !force); err != nil {
		return nil, fmt.Errorf("failed to apply pack overrides: %w", err)
	}

	// Bulk-fetch file and mod metadata.
	fileIDs := make([]int, 0, len(manifest.Files))
	modIDs := make([]int, 0, len(manifest.Files))
	for _, f := range manifest.Files {
		fileIDs = append(fileIDs, f.FileID)
		modIDs = append(modIDs, f.ProjectID)
	}

	files := map[int]fuego.File{}
	if len(fileIDs) > 0 {
		fetched, err := client.GetFilesByIDs(ctx, fileIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch pack file metadata: %w", err)
		}
		for _, f := range fetched {
			files[f.ID] = f
		}
	}

	mods := map[int]fuego.Modpack{}
	if len(modIDs) > 0 {
		fetched, err := client.GetModsByIDs(ctx, modIDs)
		if err != nil {
			p.progress(server, "warning: could not fetch mod metadata (%v); using defaults", err)
		} else {
			for _, m := range fetched {
				mods[m.ID] = m
			}
		}
	}

	excludes := splitPatterns(strVal(cfg.CFExcludeMods))
	forceIncludes := splitPatterns(strVal(cfg.CFForceIncludeMods))

	var blocked []BlockedMod
	done := 0
	for _, entry := range manifest.Files {
		file, ok := files[entry.FileID]
		if !ok {
			return nil, fmt.Errorf("pack references file %d of project %d which the API did not return", entry.FileID, entry.ProjectID)
		}
		mod := mods[entry.ProjectID]

		if !p.cfFileWanted(server, &file, &mod, entry.ProjectID, excludes, forceIncludes) {
			continue
		}

		destDir := cfClassDir(mod.ClassID)
		dest := joinData(server.DataPath, filepath.Join(destDir, file.FileName))
		if fileExists(dest) && !force {
			done++
			continue
		}

		dlURL := file.DownloadURL
		if dlURL == "" {
			var err error
			dlURL, err = client.GetFileDownloadURL(ctx, entry.ProjectID, entry.FileID)
			if err != nil {
				return nil, err
			}
		}
		if dlURL == "" {
			blocked = append(blocked, BlockedMod{
				Name:     mod.Name,
				FileName: file.FileName,
				URL:      modFileURL(&mod, entry.FileID),
			})
			continue
		}

		if err := p.download(ctx, dlURL, dest, cfChecksum(&file), nil); err != nil {
			return nil, fmt.Errorf("failed to download %s: %w", file.FileName, err)
		}
		done++
		if done%25 == 0 {
			p.progress(server, "downloaded %d/%d mods...", done, len(manifest.Files))
		}
	}

	if len(blocked) > 0 {
		p.writeBlockedMods(server, blocked)
		return nil, &BlockedModsError{Mods: blocked}
	}
	os.Remove(filepath.Join(server.DataPath, runtimespec.StateDir, "blocked-mods.json"))

	// Install the loader the manifest declares.
	loaderID := ""
	for _, ml := range manifest.Minecraft.ModLoaders {
		if ml.Primary || loaderID == "" {
			loaderID = ml.ID
		}
	}
	if loaderID == "" {
		return nil, fmt.Errorf("pack manifest declares no mod loader")
	}

	index := &mrpackIndex{Dependencies: map[string]string{"minecraft": manifest.Minecraft.Version}}
	name, version, _ := strings.Cut(loaderID, "-")
	switch name {
	case "forge", "fabric", "neoforge", "quilt":
		key := name
		if name == "fabric" {
			key = "fabric-loader"
		}
		if name == "quilt" {
			key = "quilt-loader"
		}
		index.Dependencies[key] = version
	default:
		return nil, fmt.Errorf("pack declares unsupported loader %q", loaderID)
	}

	return p.installPackLoader(ctx, server, cfg, index, force)
}

// cfFileWanted applies exclude/include rules and the client-only heuristic.
func (p *Provisioner) cfFileWanted(server *storage.Server, file *fuego.File, mod *fuego.Modpack, projectID int, excludes, forceIncludes []string) bool {
	idStr := strconv.Itoa(projectID)
	slug := strings.ToLower(mod.Slug)

	if slices.Contains(forceIncludes, idStr) || (slug != "" && slices.Contains(forceIncludes, slug)) {
		return true
	}
	if slices.Contains(excludes, idStr) || (slug != "" && slices.Contains(excludes, slug)) {
		p.progress(server, "skipping excluded mod %s", file.FileName)
		return false
	}

	// CurseForge marks environment support inside gameVersions.
	hasClient := slices.Contains(file.GameVersions, "Client")
	hasServer := slices.Contains(file.GameVersions, "Server")
	if hasClient && !hasServer {
		p.progress(server, "skipping client-only mod %s", file.FileName)
		return false
	}
	return true
}

// cfClassDir maps a CurseForge class to its install directory.
func cfClassDir(classID int) string {
	switch classID {
	case 12: // resource packs
		return "resourcepacks"
	case 6552: // shader packs
		return "shaderpacks"
	case 5: // bukkit plugins
		return "plugins"
	case 6945: // data packs
		return "datapacks"
	default:
		return "mods"
	}
}

func cfChecksum(file *fuego.File) *checksum {
	for _, h := range file.Hashes {
		if h.Algo == 1 {
			return &checksum{algo: "sha1", value: h.Value}
		}
	}
	for _, h := range file.Hashes {
		if h.Algo == 2 {
			return &checksum{algo: "md5", value: h.Value}
		}
	}
	return nil
}

func modFileURL(mod *fuego.Modpack, fileID int) string {
	if mod.Links.WebsiteURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/files/%d", strings.TrimSuffix(mod.Links.WebsiteURL, "/"), fileID)
}

// writeBlockedMods records the blocked mods list for later inspection.
func (p *Provisioner) writeBlockedMods(server *storage.Server, blocked []BlockedMod) {
	data, err := json.MarshalIndent(blocked, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(server.DataPath, runtimespec.StateDir, "blocked-mods.json")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, data, 0644)
}

// extractServerPack extracts a full server pack zip into the data dir,
// stripping a single wrapping directory when present.
func (p *Provisioner) extractServerPack(reader *zip.ReadCloser, dataPath string, skipExisting bool) error {
	prefix := commonZipRoot(&reader.Reader)
	return p.extractZipPrefix(reader, prefix, dataPath, skipExisting)
}

// commonZipRoot returns "dir/" when every entry lives under one top-level
// directory that isn't itself a known content dir, else "".
func commonZipRoot(reader *zip.Reader) string {
	contentDirs := map[string]bool{
		"mods": true, "config": true, "overrides": true, "world": true,
		"libraries": true, "plugins": true, "defaultconfigs": true,
		"kubejs": true, "scripts": true, "resourcepacks": true,
	}
	root := ""
	for _, f := range reader.File {
		name := strings.TrimPrefix(f.Name, "./")
		if name == "" {
			continue
		}
		idx := strings.Index(name, "/")
		if idx < 0 {
			return "" // file at the root
		}
		dir := name[:idx]
		if root == "" {
			root = dir
		} else if root != dir {
			return ""
		}
	}
	if root == "" || contentDirs[strings.ToLower(root)] {
		return ""
	}
	return root + "/"
}

// completeServerPack derives a launch spec from an extracted server pack,
// running any bundled loader installer when required.
func (p *Provisioner) completeServerPack(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig) (*Result, error) {
	dataPath := server.DataPath

	detect := func() *runtimespec.LaunchSpec {
		if spec, err := detectForgeLaunch(dataPath, "minecraftforge/forge"); err == nil {
			return spec
		}
		if spec, err := detectForgeLaunch(dataPath, "neoforged/neoforge"); err == nil {
			return spec
		}
		for _, jar := range []string{"fabric-server-launch.jar", "quilt-server-launch.jar", "server.jar"} {
			if fileExists(filepath.Join(dataPath, jar)) {
				return &runtimespec.LaunchSpec{Kind: runtimespec.LaunchKindJar, Jar: jar}
			}
		}
		return nil
	}

	if spec := detect(); spec != nil {
		return p.finishLaunch(server, spec, server.ModLoader, "", server.MCVersion)
	}

	// Some server packs ship the loader installer instead of an installed loader.
	matches, _ := filepath.Glob(filepath.Join(dataPath, "*installer*.jar"))
	if len(matches) > 0 {
		installer := filepath.Base(matches[0])
		p.progress(server, "running bundled installer %s...", installer)
		cmd := []string{"java", "-jar", installer, "--installServer"}
		if err := p.runInstallerContainer(ctx, server, cfg, cmd); err != nil {
			return nil, fmt.Errorf("bundled installer failed: %w", err)
		}
		if spec := detect(); spec != nil {
			return p.finishLaunch(server, spec, server.ModLoader, "", server.MCVersion)
		}
	}

	return nil, fmt.Errorf("could not determine how to launch this server pack: no known server jar, args file, or bundled installer found")
}

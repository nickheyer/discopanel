package provisioner

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers/modrinth"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"golang.org/x/sync/errgroup"
)

// The modrinth.index.json inside a .mrpack archive
type mrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Dependencies  map[string]string `json:"dependencies"`
	Files         []mrpackFile      `json:"files"`
}

type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
	Env       *struct {
		Client string `json:"client"`
		Server string `json:"server"`
	} `json:"env"`
}

// Downloads a Modrinth modpack and installs its loader
func (p *Provisioner) installModrinthPack(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, desired *desiredModpack, force bool) (*Result, error) {
	client := modrinth.NewClient(p.cfg)

	version, err := p.resolveModrinthVersion(ctx, client, cfg, desired)
	if err != nil {
		return nil, err
	}
	desired.versionID = version.ID

	// Pick the primary file (packs may ship auxiliary non-primary files)
	var packFile *modrinth.File
	for i := range version.Files {
		if version.Files[i].Primary {
			packFile = &version.Files[i]
			break
		}
	}
	if packFile == nil && len(version.Files) > 0 {
		packFile = &version.Files[0]
	}
	if packFile == nil {
		return nil, fmt.Errorf("Modrinth version %s has no files", version.ID)
	}

	p.progress(server, "downloading modpack %s (%s)...", desired.id, version.VersionNumber)
	packPath := filepath.Join(installerDir(server.DataPath), "modpack.mrpack")
	var sum *checksum
	if packFile.Hashes.SHA512 != "" {
		sum = &checksum{algo: "sha512", value: packFile.Hashes.SHA512}
	} else if packFile.Hashes.SHA1 != "" {
		sum = &checksum{algo: "sha1", value: packFile.Hashes.SHA1}
	}
	if err := p.download(ctx, packFile.URL, packPath, sum, nil, p.reporter(server, packFile.Filename)); err != nil {
		return nil, err
	}

	index, err := p.installMrpack(ctx, server, cfg, packPath, force)
	if err != nil {
		return nil, err
	}

	return p.installPackLoader(ctx, server, cfg, index, force)
}

// Picks the pack version to install
func (p *Provisioner) resolveModrinthVersion(ctx context.Context, client *modrinth.Client, cfg *storage.ServerProperties, desired *desiredModpack) (*modrinth.Version, error) {
	if desired.id == "" {
		return nil, fmt.Errorf("no Modrinth modpack configured")
	}

	// Explicit version id resolves directly
	if desired.versionID != "" {
		if v, err := client.GetVersion(ctx, desired.versionID); err == nil {
			return v, nil
		}
	}

	versions, err := client.GetProjectVersionsFiltered(ctx, desired.id, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions for Modrinth pack %q: %w", desired.id, err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("Modrinth pack %q has no versions", desired.id)
	}

	// Pinned by version number or id
	if desired.versionID != "" {
		for i := range versions {
			if versions[i].ID == desired.versionID || versions[i].VersionNumber == desired.versionID {
				return &versions[i], nil
			}
		}
		return nil, fmt.Errorf("version %q not found for Modrinth pack %q", desired.versionID, desired.id)
	}

	// Picks latest version within allowed release channel
	allowed := map[string]bool{"release": true}
	switch strVal(cfg.ModrinthModpackVersionType) {
	case "beta":
		allowed["beta"] = true
	case "alpha":
		allowed["beta"] = true
		allowed["alpha"] = true
	}
	for i := range versions {
		if allowed[versions[i].VersionType] {
			return &versions[i], nil
		}
	}
	return &versions[0], nil
}

// Extracts mrpack, downloads files then applies overrides
func (p *Provisioner) installMrpack(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, packPath string, force bool) (*mrpackIndex, error) {
	reader, err := zip.OpenReader(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mrpack: %w", err)
	}
	defer reader.Close()

	var index *mrpackIndex
	for _, f := range reader.File {
		if f.Name == "modrinth.index.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = json.NewDecoder(rc).Decode(&index)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("invalid modrinth.index.json: %w", err)
			}
			break
		}
	}
	if index == nil {
		return nil, fmt.Errorf("mrpack has no modrinth.index.json")
	}

	excludes := minecraft.SplitPatterns(strVal(cfg.ModrinthExcludeFiles))
	forceIncludes := minecraft.SplitPatterns(strVal(cfg.ModrinthForceIncludeFiles))

	// Resolves wanted files, then downloads concurrently, bounded
	var pending []mrpackFile
	total := 0
	for _, file := range index.Files {
		if !p.mrpackFileWanted(server, file, excludes, forceIncludes) {
			continue
		}
		total++
		if len(file.Downloads) == 0 {
			return nil, fmt.Errorf("mrpack file %q has no download URLs", file.Path)
		}
		if !force && fileExists(joinData(server.DataPath, file.Path)) {
			continue
		}
		pending = append(pending, file)
	}
	p.progress(server, "installing %s: downloading %d files (%d already present)...",
		index.Name, len(pending), total-len(pending))

	var done atomic.Int64
	done.Store(int64(total - len(pending)))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(packDownloadConcurrency)
	for _, file := range pending {
		g.Go(func() error {
			dest := joinData(server.DataPath, file.Path)
			var sum *checksum
			if v := file.Hashes["sha512"]; v != "" {
				sum = &checksum{algo: "sha512", value: v}
			} else if v := file.Hashes["sha1"]; v != "" {
				sum = &checksum{algo: "sha1", value: v}
			}

			err := retryTransient(gctx, func() error {
				var lastErr error
				for _, u := range file.Downloads {
					if lastErr = p.download(gctx, u, dest, sum, nil, nil); lastErr == nil {
						return nil
					}
				}
				return lastErr
			})
			if err != nil {
				return fmt.Errorf("failed to download %q: %w", file.Path, err)
			}
			if n := done.Add(1); n%25 == 0 {
				p.progress(server, "downloaded %d/%d files...", n, total)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		p.progress(server, "pack downloads complete (%d/%d)", done.Load(), total)
	}

	// Apply overrides then server-overrides on top
	for _, prefix := range []string{"overrides/", "server-overrides/"} {
		if err := p.extractZipPrefix(reader, prefix, server.DataPath, !force, excludes); err != nil {
			return nil, fmt.Errorf("failed to apply %s: %w", strings.TrimSuffix(prefix, "/"), err)
		}
	}

	return index, nil
}

// Applies env.server and user include/exclude rules
func (p *Provisioner) mrpackFileWanted(server *storage.Server, file mrpackFile, excludes, forceIncludes []string) bool {
	name := strings.ToLower(filepath.Base(file.Path))
	for _, pattern := range forceIncludes {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	for _, pattern := range excludes {
		if strings.Contains(name, pattern) {
			p.progress(server, "skipping excluded file %s", file.Path)
			return false
		}
	}
	if file.Env != nil && file.Env.Server == "unsupported" {
		p.progress(server, "skipping client-only file %s", file.Path)
		return false
	}
	return true
}

// Extracts entries under prefix from an open zip into destDir
func (p *Provisioner) extractZipPrefix(reader *zip.ReadCloser, prefix, destDir string, skipExisting bool, excludes []string) error {
	for _, f := range reader.File {
		if !strings.HasPrefix(f.Name, prefix) || f.Name == prefix {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
		if !f.FileInfo().IsDir() && minecraft.MatchesPatterns(path.Base(f.Name), excludes) {
			continue
		}
		target := joinData(destDir, rel)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if skipExisting && fileExists(target) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// Mrpack dependency keys and the loaders they pin
var mrpackLoaderKeys = []struct {
	key    string
	loader storage.ModLoader
}{
	{"fabric-loader", storage.ModLoaderFabric},
	{"quilt-loader", storage.ModLoaderQuilt},
	{"forge", storage.ModLoaderForge},
	{"neoforge", storage.ModLoaderNeoForge},
}

// Installs the mod loader a pack's index depends on
func (p *Provisioner) installPackLoader(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, index *mrpackIndex, force bool) (*Result, error) {
	for _, entry := range mrpackLoaderKeys {
		if version := index.Dependencies[entry.key]; version != "" {
			return p.installLoaderForPack(ctx, server, cfg, entry.loader, version, index.Dependencies["minecraft"])
		}
	}
	return nil, fmt.Errorf("modpack declares no supported loader (dependencies: %v)", index.Dependencies)
}

// Installs individual Modrinth mods, optionally resolving dependencies
func (p *Provisioner) installModrinthProjects(ctx context.Context, server *storage.Server, cfg *storage.ServerProperties, mcVersion string) error {
	projects := minecraft.SplitPatterns(strVal(cfg.ModrinthProjects))
	if len(projects) == 0 {
		return nil
	}
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return fmt.Errorf("modrinth projects need a server type with a mods directory")
	}

	// The property override wins, else the install itself testifies
	facets := []string{strVal(cfg.ModrinthLoader)}
	if facets[0] == "" {
		facets = minecraft.DialectFacets(minecraft.ResolveDialects(server.ModLoader, server.DataPath, modsDir))
	}
	if len(facets) == 0 {
		return fmt.Errorf("modrinth projects need the Modrinth Loader property set for this server")
	}
	loaderName := facets[0]

	client := modrinth.NewClient(p.cfg)
	depMode := strVal(cfg.ModrinthDownloadDependencies)
	versionType := strVal(cfg.ModrinthProjectsDefaultVersionType)
	if versionType == "" {
		versionType = "release"
	}

	installed := map[string]bool{}
	queue := append([]string{}, projects...)
	for len(queue) > 0 {
		project := queue[0]
		queue = queue[1:]
		if project == "" || installed[project] {
			continue
		}
		installed[project] = true

		versions, err := client.GetProjectVersionsFiltered(ctx, project, facets, []string{mcVersion})
		if err != nil {
			return fmt.Errorf("failed to resolve Modrinth project %q: %w", project, err)
		}
		var pick *modrinth.Version
		allowed := map[string]bool{"release": true}
		if versionType == "beta" {
			allowed["beta"] = true
		}
		if versionType == "alpha" {
			allowed["beta"] = true
			allowed["alpha"] = true
		}
		for i := range versions {
			if allowed[versions[i].VersionType] {
				pick = &versions[i]
				break
			}
		}
		if pick == nil && len(versions) > 0 {
			pick = &versions[0]
		}
		if pick == nil {
			return fmt.Errorf("Modrinth project %q has no version for %s %s", project, loaderName, mcVersion)
		}

		var file *modrinth.File
		for i := range pick.Files {
			if pick.Files[i].Primary {
				file = &pick.Files[i]
				break
			}
		}
		if file == nil && len(pick.Files) > 0 {
			file = &pick.Files[0]
		}
		if file == nil {
			continue
		}

		dest := filepath.Join(modsDir, file.Filename)
		if !fileExists(dest) {
			p.progress(server, "installing mod %s (%s)...", project, pick.VersionNumber)
			var sum *checksum
			if file.Hashes.SHA512 != "" {
				sum = &checksum{algo: "sha512", value: file.Hashes.SHA512}
			}
			if err := p.download(ctx, file.URL, dest, sum, nil, nil); err != nil {
				return fmt.Errorf("failed to download Modrinth project %q: %w", project, err)
			}
		}

		if depMode == "required" || depMode == "optional" {
			for _, dep := range pick.Dependencies {
				if dep.ProjectID == nil {
					continue
				}
				if dep.DependencyType == "required" || (depMode == "optional" && dep.DependencyType == "optional") {
					queue = append(queue, *dep.ProjectID)
				}
			}
		}
	}
	return nil
}

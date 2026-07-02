package provisioner

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers/modrinth"
)

// mrpackIndex is the modrinth.index.json inside a .mrpack archive.
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

// installModrinthPack downloads and installs a Modrinth modpack (.mrpack),
// then installs the loader the pack depends on.
func (p *Provisioner) installModrinthPack(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, desired *desiredModpack, force bool) (*Result, error) {
	client := modrinth.NewClient(p.cfg)

	version, err := p.resolveModrinthVersion(ctx, client, cfg, desired)
	if err != nil {
		return nil, err
	}
	desired.versionID = version.ID

	// Pick the primary file (packs may ship auxiliary non-primary files).
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
	if err := p.download(ctx, packFile.URL, packPath, sum, nil); err != nil {
		return nil, err
	}

	index, err := p.installMrpack(ctx, server, cfg, packPath, force)
	if err != nil {
		return nil, err
	}

	return p.installPackLoader(ctx, server, cfg, index, force)
}

// resolveModrinthVersion picks the pack version to install.
func (p *Provisioner) resolveModrinthVersion(ctx context.Context, client *modrinth.Client, cfg *storage.ServerConfig, desired *desiredModpack) (*modrinth.Version, error) {
	if desired.id == "" {
		return nil, fmt.Errorf("no Modrinth modpack configured")
	}

	// Explicit version id resolves directly.
	if desired.versionID != "" {
		if v, err := client.GetVersion(ctx, desired.versionID); err == nil {
			return v, nil
		}
	}

	versions, err := client.GetModpackVersions(ctx, desired.id)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions for Modrinth pack %q: %w", desired.id, err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("Modrinth pack %q has no versions", desired.id)
	}

	// Pinned by version number or id.
	if desired.versionID != "" {
		for i := range versions {
			if versions[i].ID == desired.versionID || versions[i].VersionNumber == desired.versionID {
				return &versions[i], nil
			}
		}
		return nil, fmt.Errorf("version %q not found for Modrinth pack %q", desired.versionID, desired.id)
	}

	// Latest by allowed release channel (release < beta < alpha permissiveness).
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

// installMrpack extracts a .mrpack archive: downloads all server-side files
// and applies overrides/ then server-overrides/.
func (p *Provisioner) installMrpack(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, packPath string, force bool) (*mrpackIndex, error) {
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

	excludes := splitPatterns(strVal(cfg.ModrinthExcludeFiles))
	forceIncludes := splitPatterns(strVal(cfg.ModrinthForceIncludeFiles))

	// Download pack files.
	total := 0
	for _, file := range index.Files {
		if !p.mrpackFileWanted(server, file, excludes, forceIncludes) {
			continue
		}
		total++
	}
	p.progress(server, "installing %s: %d files...", index.Name, total)

	done := 0
	for _, file := range index.Files {
		if !p.mrpackFileWanted(server, file, excludes, forceIncludes) {
			continue
		}
		if len(file.Downloads) == 0 {
			return nil, fmt.Errorf("mrpack file %q has no download URLs", file.Path)
		}

		dest := joinData(server.DataPath, file.Path)
		var sum *checksum
		if v := file.Hashes["sha512"]; v != "" {
			sum = &checksum{algo: "sha512", value: v}
		} else if v := file.Hashes["sha1"]; v != "" {
			sum = &checksum{algo: "sha1", value: v}
		}

		if !force && fileExists(dest) {
			done++
			continue
		}

		var lastErr error
		for _, u := range file.Downloads {
			if lastErr = p.download(ctx, u, dest, sum, nil); lastErr == nil {
				break
			}
		}
		if lastErr != nil {
			return nil, fmt.Errorf("failed to download %q: %w", file.Path, lastErr)
		}
		done++
		if done%25 == 0 {
			p.progress(server, "downloaded %d/%d files...", done, total)
		}
	}

	// Apply overrides then server-overrides on top.
	for _, prefix := range []string{"overrides/", "server-overrides/"} {
		if err := p.extractZipPrefix(reader, prefix, server.DataPath, !force); err != nil {
			return nil, fmt.Errorf("failed to apply %s: %w", strings.TrimSuffix(prefix, "/"), err)
		}
	}

	return index, nil
}

// mrpackFileWanted applies env.server and user include/exclude rules.
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

// extractZipPrefix extracts entries under prefix from an open zip into destDir.
func (p *Provisioner) extractZipPrefix(reader *zip.ReadCloser, prefix, destDir string, skipExisting bool) error {
	for _, f := range reader.File {
		if !strings.HasPrefix(f.Name, prefix) || f.Name == prefix {
			continue
		}
		rel := strings.TrimPrefix(f.Name, prefix)
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

// installPackLoader installs the mod loader a pack's index depends on.
func (p *Provisioner) installPackLoader(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, index *mrpackIndex, force bool) (*Result, error) {
	mc := index.Dependencies["minecraft"]
	if mc == "" {
		mc = server.MCVersion
	}
	// The pack's MC version is authoritative for loader installs.
	packServer := *server
	packServer.MCVersion = mc

	var result *Result
	var err error
	switch {
	case index.Dependencies["fabric-loader"] != "":
		result, err = p.installFabric(ctx, &packServer, index.Dependencies["fabric-loader"])
	case index.Dependencies["quilt-loader"] != "":
		result, err = p.installQuilt(ctx, &packServer, cfg, index.Dependencies["quilt-loader"])
	case index.Dependencies["forge"] != "":
		result, err = p.installForge(ctx, &packServer, cfg, index.Dependencies["forge"])
	case index.Dependencies["neoforge"] != "":
		result, err = p.installNeoForge(ctx, &packServer, cfg, index.Dependencies["neoforge"])
	default:
		return nil, fmt.Errorf("modpack declares no supported loader (dependencies: %v)", index.Dependencies)
	}
	if err != nil {
		return nil, err
	}

	// Report the pack's platform as the server's loader while keeping the
	// resolved loader version for the manifest.
	result.Loader = server.ModLoader
	return result, nil
}

// installModrinthProjects installs individual Modrinth projects (mods) into
// the appropriate directory, optionally resolving dependencies.
func (p *Provisioner) installModrinthProjects(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, loader storage.ModLoader, mcVersion string) error {
	projects := splitPatterns(strVal(cfg.ModrinthProjects))
	if len(projects) == 0 {
		return nil
	}

	loaderName := modrinthLoaderName(loader)
	if override := strVal(cfg.ModrinthLoader); override != "" {
		loaderName = override
	}
	if loaderName == "" {
		return fmt.Errorf("modrinth projects require a mod-capable loader (got %s)", loader)
	}

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

		versions, err := client.GetProjectVersionsFiltered(ctx, project, []string{loaderName}, []string{mcVersion})
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

		dest := joinData(server.DataPath, filepath.Join("mods", file.Filename))
		if !fileExists(dest) {
			p.progress(server, "installing mod %s (%s)...", project, pick.VersionNumber)
			var sum *checksum
			if file.Hashes.SHA512 != "" {
				sum = &checksum{algo: "sha512", value: file.Hashes.SHA512}
			}
			if err := p.download(ctx, file.URL, dest, sum, nil); err != nil {
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

func modrinthLoaderName(loader storage.ModLoader) string {
	switch loader {
	case storage.ModLoaderFabric:
		return "fabric"
	case storage.ModLoaderQuilt:
		return "quilt"
	case storage.ModLoaderForge:
		return "forge"
	case storage.ModLoaderNeoForge:
		return "neoforge"
	case storage.ModLoaderPaper, storage.ModLoaderPurpur, storage.ModLoaderFolia:
		return "paper"
	default:
		return ""
	}
}

// splitPatterns splits comma/newline/space separated pattern lists, lowercased.
func splitPatterns(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' '
	}) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, strings.ToLower(part))
		}
	}
	return out
}

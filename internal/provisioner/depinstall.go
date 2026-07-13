package provisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers/fuego"
	"github.com/nickheyer/discopanel/internal/indexers/modrinth"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

// Facet names mapped to CurseForge loader filter values
var fuegoLoaderTypes = map[string]fuego.ModLoaderType{
	"forge":    fuego.ModLoaderForge,
	"fabric":   fuego.ModLoaderFabric,
	"quilt":    fuego.ModLoaderQuilt,
	"neoforge": fuego.ModLoaderNeoForge,
}

// Sources a missing dependency, pack source orders indexers
// Downloaded jars must declare the id or get removed
func (p *Provisioner) InstallModByID(ctx context.Context, server *storage.Server, modID, versionRange string, dialects []string) (string, error) {
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		return "", fmt.Errorf("server type has no mods directory")
	}
	if len(dialects) == 0 {
		dialects = minecraft.ResolveDialects(server.ModLoader, server.DataPath, modsDir)
	}
	facets := minecraft.DialectFacets(dialects)
	if len(facets) == 0 {
		return "", fmt.Errorf("cannot tell which platform this server's mods target")
	}
	dialect := ""
	if len(dialects) > 0 {
		dialect = dialects[0]
	}

	sources := []string{"modrinth"}
	if pack := minecraft.PackPlatformFor(server.ModLoader); pack != nil && pack.Source == "curseforge" {
		sources = []string{"curseforge", "modrinth"}
	}

	var errs []error
	for _, src := range sources {
		var file string
		var err error
		switch src {
		case "curseforge":
			file, err = p.installModFromCurseForge(ctx, server, modsDir, modID, versionRange, facets, dialect)
		case "modrinth":
			file, err = p.installModFromModrinth(ctx, server, modsDir, modID, versionRange, facets, dialect)
		}
		if err == nil {
			return file, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", src, err))
	}
	return "", errors.Join(errs...)
}

func (p *Provisioner) installModFromModrinth(ctx context.Context, server *storage.Server, modsDir, modID, versionRange string, facets []string, dialect string) (string, error) {
	client := modrinth.NewClient(p.cfg)
	project, err := client.GetModpack(ctx, modID)
	if err != nil {
		return "", fmt.Errorf("no modrinth project matches %q: %w", modID, err)
	}
	if project.ProjectType != "mod" {
		return "", fmt.Errorf("modrinth project %q is a %s, not a mod", modID, project.ProjectType)
	}

	versions, err := client.GetProjectVersionsFiltered(ctx, project.ID, facets, []string{server.MCVersion})
	if err != nil {
		return "", fmt.Errorf("version lookup for %q failed: %w", modID, err)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no %s build of %q exists for MC %s", facets[0], modID, server.MCVersion)
	}

	// Newest first, a range match beats recency
	version := &versions[0]
	for i := range versions {
		if minecraft.VersionSatisfies(versions[i].VersionNumber, versionRange, dialect) {
			version = &versions[i]
			break
		}
	}
	file := primaryFile(version)
	if file == nil {
		return "", fmt.Errorf("version %s of %q ships no files", version.VersionNumber, modID)
	}

	dest := filepath.Join(modsDir, file.Filename)
	var sum *checksum
	switch {
	case file.Hashes.SHA512 != "":
		sum = &checksum{algo: "sha512", value: file.Hashes.SHA512}
	case file.Hashes.SHA1 != "":
		sum = &checksum{algo: "sha1", value: file.Hashes.SHA1}
	}
	if err := p.download(ctx, file.URL, dest, sum, nil, nil); err != nil {
		return "", err
	}
	return p.gateInstalledJar(dest, "modrinth project "+project.Slug, modID, versionRange, dialect)
}

func (p *Provisioner) installModFromCurseForge(ctx context.Context, server *storage.Server, modsDir, modID, versionRange string, facets []string, dialect string) (string, error) {
	cfg, err := p.store.GetServerProperties(ctx, server.ID)
	if err != nil {
		return "", fmt.Errorf("server config unavailable: %w", err)
	}
	client, err := p.curseForgeClient(ctx, cfg)
	if err != nil {
		return "", err
	}

	mod, err := client.GetModBySlug(ctx, modID, fuego.ModsClassID)
	if err != nil {
		return "", fmt.Errorf("no curseforge mod matches %q: %w", modID, err)
	}
	loaderType, ok := fuegoLoaderTypes[facets[0]]
	if !ok {
		return "", fmt.Errorf("curseforge has no loader filter for %q", facets[0])
	}
	files, err := client.GetModpackFiles(ctx, mod.ID, server.MCVersion, loaderType)
	if err != nil {
		return "", fmt.Errorf("file lookup for %q failed: %w", modID, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no %s build of %q exists for MC %s", facets[0], modID, server.MCVersion)
	}

	// Newest file wins, the jar gate verifies the range
	newest := &files[0]
	for i := range files {
		if files[i].FileDate.After(newest.FileDate) {
			newest = &files[i]
		}
	}
	dlURL, err := p.resolveModFileURL(ctx, client, mod.ID, newest)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(modsDir, newest.FileName)
	if err := p.download(ctx, dlURL, dest, cfChecksum(newest), nil, nil); err != nil {
		return "", err
	}
	return p.gateInstalledJar(dest, "curseforge mod "+mod.Slug, modID, versionRange, dialect)
}

// Resolves a mod file url, CDN guess covers withheld urls
func (p *Provisioner) resolveModFileURL(ctx context.Context, client *fuego.Client, modID int, file *fuego.File) (string, error) {
	dlURL := file.DownloadURL
	if dlURL == "" {
		var err error
		dlURL, err = client.GetFileDownloadURL(ctx, modID, file.ID)
		if err != nil {
			return "", err
		}
	}
	if dlURL == "" {
		dlURL = fuego.CDNDownloadURL(file.ID, file.FileName)
	}
	if dlURL == "" {
		return "", fmt.Errorf("could not resolve a download url for %q", file.FileName)
	}
	return dlURL, nil
}

// Keeps jars declaring the id inside the range
func (p *Provisioner) gateInstalledJar(dest, origin, modID, versionRange, dialect string) (string, error) {
	meta, err := minecraft.ReadModJar(dest)
	if err != nil || !meta.HasModID(modID) {
		_ = os.Remove(dest)
		return "", fmt.Errorf("%s does not provide the mod id %q", origin, modID)
	}
	if versionRange != "" && !minecraft.VersionSatisfies(meta.VersionOf(modID), versionRange, dialect) {
		_ = os.Remove(dest)
		return "", fmt.Errorf("%s ships %s %s, outside required range %s", origin, modID, meta.VersionOf(modID), versionRange)
	}
	return filepath.Base(dest), nil
}

func primaryFile(version *modrinth.Version) *modrinth.File {
	for i := range version.Files {
		if version.Files[i].Primary {
			return &version.Files[i]
		}
	}
	if len(version.Files) > 0 {
		return &version.Files[0]
	}
	return nil
}

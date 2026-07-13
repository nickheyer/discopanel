package provisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers/modrinth"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

// Sources a missing dependency from Modrinth by mod id
// The downloaded jar must declare the id or it is removed again
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

	dialect := ""
	if len(dialects) > 0 {
		dialect = dialects[0]
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

	// The id gate keeps a wrong slug guess from landing
	meta, err := minecraft.ReadModJar(dest)
	if err != nil || !meta.HasModID(modID) {
		_ = os.Remove(dest)
		return "", fmt.Errorf("modrinth project %q does not provide the mod id %q", project.Slug, modID)
	}
	return file.Filename, nil
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

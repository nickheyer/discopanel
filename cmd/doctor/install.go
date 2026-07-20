package main

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/pkg/indexers/fuego"
	"github.com/nickheyer/discopanel/pkg/indexers/modrinth"
	"github.com/nickheyer/discopanel/pkg/minecraft"
)

// Facet names mapped to CurseForge loader filter values
var fuegoLoaderTypes = map[string]fuego.ModLoaderType{
	"forge":    fuego.ModLoaderForge,
	"fabric":   fuego.ModLoaderFabric,
	"quilt":    fuego.ModLoaderQuilt,
	"neoforge": fuego.ModLoaderNeoForge,
}

// Panel surface the installer needs for CurseForge access
type keySource interface {
	CFAPIKey(ctx context.Context, serverID string) string
}

// Sources missing dependencies, pack source orders indexers
// Downloaded jars must declare the id or get removed
type depInstaller struct {
	userAgent string
	modrinth  *modrinth.Client
	keys      keySource
	http      *http.Client
}

func newDepInstaller(userAgent string, keys keySource) *depInstaller {
	return &depInstaller{
		userAgent: userAgent,
		modrinth:  modrinth.NewClient(userAgent),
		keys:      keys,
		http:      &http.Client{Timeout: 5 * time.Minute},
	}
}

// Installs a mod by id into the mods dir, returns file name
func (in *depInstaller) Install(ctx context.Context, srv *serverInfo, modsDir, modID, versionRange, dialect string) (string, error) {
	facets := minecraft.DialectFacets(serverDialects(srv))
	if len(facets) == 0 {
		return "", fmt.Errorf("no loader facet resolvable for %s", srv.Name)
	}

	sources := []string{"modrinth"}
	if pack := minecraft.PackPlatformFor(srv.ModLoader); pack != nil && pack.Source == "curseforge" {
		sources = []string{"curseforge", "modrinth"}
	}

	var errs []error
	for _, src := range sources {
		var file string
		var err error
		switch src {
		case "curseforge":
			file, err = in.fromCurseForge(ctx, srv, modsDir, modID, versionRange, facets, dialect)
		case "modrinth":
			file, err = in.fromModrinth(ctx, srv, modsDir, modID, versionRange, facets, dialect)
		}
		if err == nil {
			return file, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", src, err))
	}
	return "", errors.Join(errs...)
}

func (in *depInstaller) fromModrinth(ctx context.Context, srv *serverInfo, modsDir, modID, versionRange string, facets []string, dialect string) (string, error) {
	versions, err := in.modrinth.GetProjectVersionsFiltered(ctx, modID, facets, []string{srv.McVersion})
	if err != nil {
		return "", fmt.Errorf("modrinth lookup for %q failed: %w", modID, err)
	}
	var pick *modrinth.Version
	for i := range versions {
		if versions[i].VersionType == "release" {
			pick = &versions[i]
			break
		}
	}
	if pick == nil && len(versions) > 0 {
		pick = &versions[0]
	}
	if pick == nil {
		return "", fmt.Errorf("no %s build of %q exists for MC %s", facets[0], modID, srv.McVersion)
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
		return "", fmt.Errorf("version %s of %q ships no files", pick.VersionNumber, modID)
	}

	dest := filepath.Join(modsDir, file.Filename)
	if err := in.download(ctx, file.URL, dest, "sha512", file.Hashes.SHA512); err != nil {
		return "", err
	}
	return in.gateJar(dest, "modrinth project "+modID, modID, versionRange, dialect)
}

func (in *depInstaller) fromCurseForge(ctx context.Context, srv *serverInfo, modsDir, modID, versionRange string, facets []string, dialect string) (string, error) {
	apiKey := in.keys.CFAPIKey(ctx, srv.ID)
	if apiKey == "" {
		return "", fmt.Errorf("no CurseForge API key in server or global settings")
	}
	client := fuego.NewClient(apiKey, in.userAgent)

	mod, err := client.GetModBySlug(ctx, modID, fuego.ModsClassID)
	if err != nil {
		return "", fmt.Errorf("no curseforge mod matches %q: %w", modID, err)
	}
	loaderType, ok := fuegoLoaderTypes[facets[0]]
	if !ok {
		return "", fmt.Errorf("curseforge has no loader filter for %q", facets[0])
	}
	files, err := client.GetModpackFiles(ctx, mod.ID, srv.McVersion, loaderType)
	if err != nil {
		return "", fmt.Errorf("file lookup for %q failed: %w", modID, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no %s build of %q exists for MC %s", facets[0], modID, srv.McVersion)
	}

	// Newest file wins, the jar gate verifies the range
	newest := &files[0]
	for i := range files {
		if files[i].FileDate.After(newest.FileDate) {
			newest = &files[i]
		}
	}
	dlURL := newest.DownloadURL
	if dlURL == "" {
		dlURL, err = client.GetFileDownloadURL(ctx, mod.ID, newest.ID)
		if err != nil {
			return "", err
		}
	}
	// API withholds url when author disables distribution
	if dlURL == "" {
		dlURL = fuego.CDNDownloadURL(newest.ID, newest.FileName)
	}
	if dlURL == "" {
		return "", fmt.Errorf("could not resolve a download url for %q", newest.FileName)
	}

	algo, sum := cfHash(newest)
	dest := filepath.Join(modsDir, newest.FileName)
	if err := in.download(ctx, dlURL, dest, algo, sum); err != nil {
		return "", err
	}
	return in.gateJar(dest, "curseforge mod "+mod.Slug, modID, versionRange, dialect)
}

// Strongest hash CurseForge published for the file
func cfHash(file *fuego.File) (string, string) {
	for _, h := range file.Hashes {
		if h.Algo == 1 {
			return "sha1", h.Value
		}
	}
	for _, h := range file.Hashes {
		if h.Algo == 2 {
			return "md5", h.Value
		}
	}
	return "", ""
}

// Keeps jars declaring the id inside the range
func (in *depInstaller) gateJar(dest, origin, modID, versionRange, dialect string) (string, error) {
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

func newHasher(algo string) hash.Hash {
	switch algo {
	case "sha1":
		return sha1.New()
	case "sha512":
		return sha512.New()
	case "md5":
		return md5.New()
	default:
		return nil
	}
}

// Fetches one jar atomically with checksum verification
func (in *depInstaller) download(ctx context.Context, rawURL, dest, algo, sum string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", in.userAgent)
	resp, err := in.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed for %s: status %d", rawURL, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".doctor-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hasher := newHasher(algo)
	writers := []io.Writer{tmp}
	if sum != "" && hasher != nil {
		writers = append(writers, hasher)
	}
	if _, err := io.Copy(io.MultiWriter(writers...), resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if sum != "" && hasher != nil && !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), sum) {
		return fmt.Errorf("checksum mismatch for %s", rawURL)
	}
	return os.Rename(tmpPath, dest)
}

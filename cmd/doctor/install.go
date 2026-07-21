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

	"github.com/nickheyer/discopanel/pkg/indexers"
	"github.com/nickheyer/discopanel/pkg/minecraft"
	optionsv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/options/v1"
)

// Panel surface the installer reads properties through
type propertySource interface {
	PropertyValue(ctx context.Context, serverID, key string) string
}

// Sources missing dependencies from every registered indexer
// Downloaded jars must declare the id or get removed
type depInstaller struct {
	userAgent string
	props     propertySource
	http      *http.Client
}

func newDepInstaller(userAgent string, props propertySource) *depInstaller {
	return &depInstaller{
		userAgent: userAgent,
		props:     props,
		http:      &http.Client{Timeout: 5 * time.Minute},
	}
}

// Indexers serving the pack's source go first
func orderSourcers(infos []indexers.IndexerInfo, packSource optionsv1.PackSource) []indexers.IndexerInfo {
	none := optionsv1.PackSource_PACK_SOURCE_UNSPECIFIED
	out := make([]indexers.IndexerInfo, 0, len(infos))
	for _, info := range infos {
		if packSource != none && info.PackSource == packSource {
			out = append(out, info)
		}
	}
	for _, info := range infos {
		if packSource == none || info.PackSource != packSource {
			out = append(out, info)
		}
	}
	return out
}

// Installs a mod by id into the mods dir, returns file name
func (in *depInstaller) Install(ctx context.Context, srv *serverInfo, modsDir, modID, versionRange, dialect string) (string, error) {
	facets := minecraft.DialectFacets(serverDialects(srv))
	if len(facets) == 0 {
		return "", fmt.Errorf("no loader facet resolvable for %s", srv.Name)
	}
	q := indexers.ModQuery{ModID: modID, McVersion: srv.McVersion, Loaders: facets}

	var errs []error
	for _, info := range orderSourcers(indexers.Indexers(), minecraft.PackSourceFor(srv.ModLoader)) {
		file, err := in.fromIndexer(ctx, srv, info, modsDir, q, versionRange, dialect)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, errNotModSourcer) {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: %w", info.Name, err))
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("no registered indexer can source mods")
	}
	return "", errors.Join(errs...)
}

// Indexer registered without the mod sourcing capability
var errNotModSourcer = errors.New("indexer does not source single mods")

// Tries one indexer's candidates, first gated jar wins
func (in *depInstaller) fromIndexer(ctx context.Context, srv *serverInfo, info indexers.IndexerInfo, modsDir string, q indexers.ModQuery, versionRange, dialect string) (string, error) {
	apiKey := ""
	if info.CredentialProperty != "" {
		apiKey = in.props.PropertyValue(ctx, srv.ID, info.CredentialProperty)
	}
	ix, err := indexers.NewIndexer(info.Name, apiKey, in.userAgent)
	if err != nil {
		return "", err
	}
	src, ok := ix.(indexers.ModSourcer)
	if !ok {
		return "", errNotModSourcer
	}

	candidates, err := src.SourceMod(ctx, q)
	if err != nil {
		return "", err
	}
	var errs []error
	for i := range candidates {
		c := &candidates[i]
		dest := filepath.Join(modsDir, c.FileName)
		if err := in.download(ctx, c.URL, dest, c.HashAlgo, c.HashSum); err != nil {
			errs = append(errs, err)
			continue
		}
		file, err := in.gateJar(dest, c.Origin, q.ModID, versionRange, dialect)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return file, nil
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("no candidate files for %q", q.ModID)
	}
	return "", errors.Join(errs...)
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

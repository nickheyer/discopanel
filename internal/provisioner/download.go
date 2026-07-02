package provisioner

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// checksum describes an expected digest for a download.
type checksum struct {
	algo  string // "sha1" | "sha256" | "sha512" | "md5"
	value string
}

func newHasher(algo string) hash.Hash {
	switch algo {
	case "sha1":
		return sha1.New()
	case "sha256":
		return sha256.New()
	case "sha512":
		return sha512.New()
	case "md5":
		return md5.New()
	default:
		return nil
	}
}

var downloadClient = &http.Client{
	// No global timeout: large modpack files on slow links are legitimate.
	// Cancellation comes from the request context.
	Timeout: 0,
}

// download fetches url into dest atomically (tmp file + rename), verifying the
// checksum when provided. Parent directories are created as needed.
func (p *Provisioner) download(ctx context.Context, rawURL, dest string, sum *checksum, headers map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", p.cfg.Server.UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed for %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed for %s: status %d", rawURL, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	var writer io.Writer = tmp
	var hasher hash.Hash
	if sum != nil {
		hasher = newHasher(sum.algo)
		if hasher != nil {
			writer = io.MultiWriter(tmp, hasher)
		}
	}

	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("download interrupted for %s: %w", rawURL, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if hasher != nil {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, sum.value) {
			return fmt.Errorf("checksum mismatch for %s: expected %s %s, got %s", rawURL, sum.algo, sum.value, got)
		}
	}

	return os.Rename(tmpPath, dest)
}

// getJSON fetches and decodes a JSON document with the panel user agent.
func (p *Provisioner) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", p.cfg.Server.UserAgent)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed for %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("request failed for %s: status %d: %s", rawURL, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(dest)
}

// getText fetches a small text document (e.g. maven-metadata.xml, checksum sidecars).
func (p *Provisioner) getText(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", p.cfg.Server.UserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed for %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("request failed for %s: status %d", rawURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// installerDir returns the scratch directory for downloaded installers.
func installerDir(dataPath string) string {
	return filepath.Join(dataPath, ".discopanel", "installers")
}

// joinData joins a relative path onto the data dir, rejecting traversal outside it.
func joinData(dataPath string, rel string) string {
	joined := filepath.Join(dataPath, filepath.FromSlash(rel))
	clean := filepath.Clean(joined)
	root := filepath.Clean(dataPath)
	if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		// Traversal attempt - collapse to a name inside the data dir.
		return filepath.Join(root, filepath.Base(clean))
	}
	return clean
}

// parseCurseForgeRef normalizes the various ways a CF modpack can be referenced
// (page URL with optional pinned file, slug, explicit file id) into (slug, fileID).
func parseCurseForgeRef(pageURL, slug, fileID string) (string, string) {
	if pageURL != "" {
		if u, err := url.Parse(pageURL); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			// .../minecraft/modpacks/{slug}[/files/{fileID}]
			for i, part := range parts {
				if part == "modpacks" && i+1 < len(parts) {
					slug = parts[i+1]
				}
				if part == "files" && i+1 < len(parts) {
					fileID = parts[i+1]
				}
			}
		}
	}
	return slug, fileID
}

// parseModrinthRef normalizes Modrinth references: "project", "project:version",
// or a full modrinth.net URL, plus the explicit version field.
func parseModrinthRef(modpack, version string) (string, string) {
	project := modpack
	if strings.Contains(modpack, "://") {
		if u, err := url.Parse(modpack); err == nil {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			// modrinth.com/modpack/{slug}[/version/{version}]
			for i, part := range parts {
				if (part == "modpack" || part == "mod" || part == "project") && i+1 < len(parts) {
					project = parts[i+1]
				}
				if part == "version" && i+1 < len(parts) {
					version = parts[i+1]
				}
			}
		}
	} else if idx := strings.Index(modpack, ":"); idx > 0 {
		project = modpack[:idx]
		if version == "" {
			version = modpack[idx+1:]
		}
	}
	return project, version
}

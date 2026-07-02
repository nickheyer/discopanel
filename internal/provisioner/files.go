package provisioner

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

// applyConfigFiles writes all panel-managed configuration files into the data
// dir: server.properties, eula.txt, server-icon.png, ops.json, whitelist.json,
// and installs any configured individual Modrinth projects.
func (p *Provisioner) applyConfigFiles(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, mcVersion string) error {
	if err := p.writeServerProperties(server, cfg); err != nil {
		return err
	}
	if err := p.writeEULA(server, cfg); err != nil {
		return err
	}
	if err := p.writeServerIcon(ctx, server, cfg); err != nil {
		p.progress(server, "warning: failed to install server icon: %v", err)
	}
	if err := p.writePlayerList(ctx, server, cfg, "ops.json", strVal(cfg.Ops), true); err != nil {
		p.progress(server, "warning: failed to write ops.json: %v", err)
	}
	overwriteWhitelist := boolVal(cfg.OverrideWhitelist)
	if err := p.writePlayerListFile(ctx, server, cfg, "whitelist.json", strVal(cfg.Whitelist), false, overwriteWhitelist); err != nil {
		p.progress(server, "warning: failed to write whitelist.json: %v", err)
	}
	if err := p.installModrinthProjects(ctx, server, cfg, server.ModLoader, mcVersion); err != nil {
		return err
	}
	return nil
}

// writeServerProperties merges all prop-tagged ServerConfig fields (plus
// custom pairs) into server.properties, preserving unmanaged keys.
func (p *Provisioner) writeServerProperties(server *storage.Server, cfg *storage.ServerConfig) error {
	props := minecraft.ServerProperties{}

	value := reflect.ValueOf(cfg).Elem()
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		key := typ.Field(i).Tag.Get("prop")
		if key == "" {
			continue
		}
		field := value.Field(i)
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				continue
			}
			field = field.Elem()
		}
		switch field.Kind() {
		case reflect.String:
			props[key] = field.String()
		case reflect.Int, reflect.Int32, reflect.Int64:
			props[key] = fmt.Sprintf("%d", field.Int())
		case reflect.Bool:
			props[key] = fmt.Sprintf("%v", field.Bool())
		}
	}

	// RCON is required for the panel's console, metrics and backups: enforce
	// working defaults even when the fields were never set.
	if _, ok := props["enable-rcon"]; !ok {
		props["enable-rcon"] = "true"
	}
	if _, ok := props["rcon.port"]; !ok {
		props["rcon.port"] = "25575"
	}
	if props["rcon.password"] == "" {
		password := "discopanel_default"
		if len(server.ID) >= 8 {
			password = "discopanel_" + server.ID[:8]
		}
		props["rcon.password"] = password
	}
	if _, ok := props["server-port"]; !ok {
		props["server-port"] = fmt.Sprintf("%d", server.Port)
	}

	// Extra raw pairs.
	for _, line := range strings.Split(strVal(cfg.CustomServerProperties), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			props[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	if boolVal(cfg.ServerPropertiesEscapeUnicode) {
		for k, v := range props {
			props[k] = escapeUnicodeProperties(v)
		}
	}

	return minecraft.SaveServerProperties(server.DataPath, props)
}

// escapeUnicodeProperties escapes non-ASCII runes as \uXXXX for legacy
// versions that read server.properties as ISO-8859-1.
func escapeUnicodeProperties(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		} else {
			fmt.Fprintf(&b, "\\u%04X", r)
		}
	}
	return b.String()
}

// writeEULA writes eula.txt when the EULA has been accepted.
func (p *Provisioner) writeEULA(server *storage.Server, cfg *storage.ServerConfig) error {
	accepted := strings.EqualFold(strVal(cfg.EULA), "true")
	if !accepted {
		return fmt.Errorf("the Minecraft EULA must be accepted before the server can start")
	}
	content := "# Accepted via DiscoPanel\neula=true\n"
	return os.WriteFile(filepath.Join(server.DataPath, "eula.txt"), []byte(content), 0644)
}

// writeServerIcon downloads and converts the configured icon to a 64x64 PNG.
func (p *Provisioner) writeServerIcon(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig) error {
	iconURL := strVal(cfg.Icon)
	if iconURL == "" {
		return nil
	}
	iconPath := filepath.Join(server.DataPath, "server-icon.png")
	if fileExists(iconPath) && !boolVal(cfg.OverrideIcon) {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iconURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", p.cfg.Server.UserAgent)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("icon download failed: status %d", resp.StatusCode)
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("unsupported icon image: %w", err)
	}

	img = scaleTo64(img)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(iconPath, buf.Bytes(), 0644)
}

// scaleTo64 nearest-neighbor scales an image to the required 64x64.
func scaleTo64(src image.Image) image.Image {
	const size = 64
	bounds := src.Bounds()
	if bounds.Dx() == size && bounds.Dy() == size {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			srcX := bounds.Min.X + x*bounds.Dx()/size
			srcY := bounds.Min.Y + y*bounds.Dy()/size
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

// playerEntry is a single ops.json / whitelist.json record.
type playerEntry struct {
	UUID                string `json:"uuid"`
	Name                string `json:"name"`
	Level               int    `json:"level,omitempty"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit,omitempty"`
}

func (p *Provisioner) writePlayerList(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, filename, list string, isOps bool) error {
	return p.writePlayerListFile(ctx, server, cfg, filename, list, isOps, false)
}

// writePlayerListFile resolves player names to UUIDs and merges (or replaces)
// the target JSON list file.
func (p *Provisioner) writePlayerListFile(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, filename, list string, isOps bool, overwrite bool) error {
	names := splitList(list)
	path := filepath.Join(server.DataPath, filename)
	if len(names) == 0 {
		return nil
	}

	var entries []playerEntry
	if !overwrite {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &entries)
		}
	}

	known := map[string]bool{}
	for _, e := range entries {
		known[strings.ToLower(e.Name)] = true
	}

	onlineMode := cfg.OnlineMode == nil || *cfg.OnlineMode
	for _, name := range names {
		if known[strings.ToLower(name)] {
			continue
		}
		entry := playerEntry{Name: name}
		if isUUID(name) {
			entry.UUID = name
			entry.Name = ""
		} else {
			uuid, err := p.resolvePlayerUUID(ctx, name, onlineMode)
			if err != nil {
				p.progress(server, "warning: could not resolve player %q: %v", name, err)
				continue
			}
			entry.UUID = uuid
		}
		if isOps {
			entry.Level = 4
		}
		entries = append(entries, entry)
		known[strings.ToLower(name)] = true
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// resolvePlayerUUID resolves a username to a UUID: via the Mojang API for
// online-mode servers, or the offline-mode derivation otherwise.
func (p *Provisioner) resolvePlayerUUID(ctx context.Context, name string, onlineMode bool) (string, error) {
	if !onlineMode {
		return offlineUUID(name), nil
	}

	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	url := "https://api.mojang.com/users/profiles/minecraft/" + name
	if err := p.getJSON(ctx, url, &result); err != nil {
		return "", err
	}
	if len(result.ID) != 32 {
		return "", fmt.Errorf("unexpected profile response for %q", name)
	}
	id := result.ID
	return fmt.Sprintf("%s-%s-%s-%s-%s", id[0:8], id[8:12], id[12:16], id[16:20], id[20:32]), nil
}

// offlineUUID derives the offline-mode UUID (v3, "OfflinePlayer:" + name).
func offlineUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = (sum[6] & 0x0f) | 0x30 // version 3
	sum[8] = (sum[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// splitList splits comma/newline separated lists, preserving case.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

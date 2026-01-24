package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Template functions for arithmetic operations
var templateFuncs = template.FuncMap{
	"mul": func(a, b float64) float64 { return a * b },
	"div": func(a, b float64) float64 {
		if b == 0 {
			return 0
		}
		return a / b
	},
}

//go:embed templates/*
var templateFS embed.FS

func main() {
	serverID := os.Getenv("DISCOPANEL_SERVER_ID")
	if serverID == "" {
		fmt.Fprintln(os.Stderr, "DISCOPANEL_SERVER_ID required")
		os.Exit(1)
	}

	apiURL := env("DISCOPANEL_URL", "http://host.docker.internal:8080")
	port := envInt("PORT", 8181)
	poll := envDuration("POLL_INTERVAL", 10*time.Second)

	fmt.Printf("Status Panel: server=%s api=%s poll=%s port=%d\n", serverID, apiURL, poll, port)

	tmpl, err := template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.tmpl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "template error: %v\n", err)
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	p := &panel{
		serverClient:    discopanelv1connect.NewServerServiceClient(httpClient, apiURL),
		minecraftClient: discopanelv1connect.NewMinecraftServiceClient(httpClient, apiURL),
		configClient:    discopanelv1connect.NewConfigServiceClient(httpClient, apiURL),
		modpackClient:   discopanelv1connect.NewModpackServiceClient(httpClient, apiURL),
		serverID:        serverID,
		poll:            poll,
		tmpl:            tmpl,
		title:           os.Getenv("PANEL_TITLE"),
	}

	go p.loop()

	http.HandleFunc("/", p.handleIndex)
	http.HandleFunc("/api/status", p.handleAPI)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	fmt.Printf("Listening :%d\n", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

type panel struct {
	serverClient    discopanelv1connect.ServerServiceClient
	minecraftClient discopanelv1connect.MinecraftServiceClient
	configClient    discopanelv1connect.ConfigServiceClient
	modpackClient   discopanelv1connect.ModpackServiceClient
	serverID        string
	poll            time.Duration
	tmpl            *template.Template
	title           string

	mu             sync.RWMutex
	server         *v1.Server
	modLoaders     map[string]*v1.ModLoaderInfo
	modpack        *v1.IndexedModpack
	modpackVersion string
	err            error
}

func (p *panel) loop() {
	p.fetch()
	for range time.Tick(p.poll) {
		p.fetch()
	}
}

func (p *panel) fetch() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Fetch static data once (mod loaders, modpack info)
	p.fetchStaticOnce(ctx)

	// Fetch server status (this changes frequently)
	resp, err := p.serverClient.GetServer(ctx, connect.NewRequest(&v1.GetServerRequest{Id: p.serverID}))

	p.mu.Lock()
	defer p.mu.Unlock()

	if err != nil {
		p.err = err
		fmt.Printf("fetch error: %v\n", err)
		return
	}

	p.server = resp.Msg.GetServer()
	p.err = nil

	if p.title == "" && p.server.GetName() != "" {
		p.title = p.server.GetName()
	}

	fmt.Printf("fetched: %s status=%s players=%d tps=%.1f\n",
		p.server.GetName(), p.server.GetStatus(), p.server.GetPlayersOnline(), p.server.GetTps())
}

// Fetches data that doesn't change during runtime
func (p *panel) fetchStaticOnce(ctx context.Context) {
	p.mu.RLock()
	alreadyFetched := p.modLoaders != nil
	p.mu.RUnlock()

	if alreadyFetched {
		return
	}

	// Fetch mod loaders for display names
	loadersResp, err := p.minecraftClient.GetModLoaders(ctx, connect.NewRequest(&v1.GetModLoadersRequest{}))
	if err != nil {
		fmt.Printf("failed to fetch mod loaders: %v\n", err)
		return
	}

	p.mu.Lock()
	p.modLoaders = make(map[string]*v1.ModLoaderInfo)
	for _, info := range loadersResp.Msg.GetModloaders() {
		p.modLoaders[info.GetName()] = info
	}
	p.mu.Unlock()

	// Fetch server config to find modpack URL and version
	indexerKeys := []string{"cfPageUrl", "modrinthModpack"}
	versionKeys := []string{"cfFileId", "modrinthVersion"}
	configResp, err := p.configClient.GetServerConfig(ctx, connect.NewRequest(&v1.GetServerConfigRequest{ServerId: p.serverID}))
	if err != nil {
		fmt.Printf("failed to fetch server config: %v\n", err)
		return
	}

	var modpackURL string
	var modpackVersion string
	for _, cat := range configResp.Msg.GetCategories() {
		for _, prop := range cat.GetProperties() {
			if slices.Contains(indexerKeys, prop.GetKey()) && prop.GetValue() != "" {
				modpackURL = prop.GetValue()
			}
			if slices.Contains(versionKeys, prop.GetKey()) && prop.GetValue() != "" {
				modpackVersion = prop.GetValue()
			}
		}
	}

	if modpackURL != "" {
		modpackResp, _ := p.modpackClient.GetModpackByURL(ctx, connect.NewRequest(&v1.GetModpackByURLRequest{Url: modpackURL}))
		if modpackResp != nil && modpackResp.Msg.GetModpack() != nil {
			p.mu.Lock()
			p.modpack = modpackResp.Msg.GetModpack()
			p.modpackVersion = modpackVersion
			p.mu.Unlock()
			fmt.Printf("loaded modpack: %s (version: %s)\n", p.modpack.GetName(), modpackVersion)
		}
	}
}

func (p *panel) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	p.mu.RLock()
	server := p.server
	modpack := p.modpack
	modpackVersion := p.modpackVersion
	var modLoaderInfo *v1.ModLoaderInfo
	if server != nil && p.modLoaders != nil {
		modLoaderInfo = p.modLoaders[server.GetModLoader().String()]
	}
	err := p.err
	title := p.title
	p.mu.RUnlock()

	if title == "" {
		title = "Server Status"
	}

	// Compute derived values for display
	var memoryUsedMB int64
	var memoryPercent float64
	var diskUsedFormatted string
	var diskPercent float64

	if server != nil {
		memoryUsedMB = server.GetMemoryUsage()
		if server.GetMemory() > 0 {
			memoryPercent = float64(memoryUsedMB) / float64(server.GetMemory()) * 100
			if memoryPercent > 100 {
				memoryPercent = 100
			}
		}

		// Format disk usage
		diskUsed := server.GetDiskUsage()
		diskTotal := server.GetDiskTotal()

		diskUsedFormatted = formatBytes(diskUsed)
		if diskTotal > 0 {
			diskPercent = float64(diskUsed) / float64(diskTotal) * 100
			if diskPercent > 100 {
				diskPercent = 100
			}
		}
	}

	data := map[string]any{
		"Server":            server,
		"Modpack":           modpack,
		"ModpackVersion":    modpackVersion,
		"ModLoaderInfo":     modLoaderInfo,
		"Title":             title,
		"Error":             err,
		"MemoryUsedMB":      memoryUsedMB,
		"MemoryPercent":     fmt.Sprintf("%.0f", memoryPercent),
		"DiskUsedFormatted": diskUsedFormatted,
		"DiskPercent":       fmt.Sprintf("%.0f", diskPercent),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := p.tmpl.ExecuteTemplate(w, "index.tmpl", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (p *panel) handleAPI(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	server := p.server
	var modLoaderInfo *v1.ModLoaderInfo
	if server != nil && p.modLoaders != nil {
		modLoaderInfo = p.modLoaders[server.GetModLoader().String()]
	}
	err := p.err
	p.mu.RUnlock()

	resp := map[string]any{
		"server":          server,
		"mod_loader_info": modLoaderInfo,
	}
	if err != nil {
		resp["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return d
}

func envDuration(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if dur, err := time.ParseDuration(v); err == nil {
			return dur
		}
	}
	return d
}

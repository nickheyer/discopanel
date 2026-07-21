// Global crash doctor module, repairs servers from outside the panel
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/pkg/indexers"
	_ "github.com/nickheyer/discopanel/pkg/indexers/all"
	"github.com/nickheyer/discopanel/pkg/minecraft"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"github.com/nickheyer/discopanel/pkg/protometa"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
)

// Stamped via -ldflags at build time
var doctorVersion = "dev"

func main() {
	apiURL := env("DISCOPANEL_URL", "http://host.docker.internal:8080")
	apiToken := os.Getenv("DISCOPANEL_API_TOKEN")
	hostDataDir := os.Getenv("DISCOPANEL_DATA_DIR")
	mountDir := env("DOCTOR_DATA_MOUNT", "/data")
	poll := envDuration("POLL_INTERVAL", 15*time.Second)
	port := envInt("PORT", 8190)
	repair := env("DOCTOR_MODE", "repair") == "repair"
	installDeps := env("DOCTOR_INSTALL_DEPS", "on") != "off"

	if apiToken == "" {
		fmt.Fprintln(os.Stderr, "DISCOPANEL_API_TOKEN required")
		os.Exit(1)
	}
	if hostDataDir == "" {
		fmt.Fprintln(os.Stderr, "DISCOPANEL_DATA_DIR required to map server paths")
		os.Exit(1)
	}

	fmt.Printf("DiscoPanel Doctor %s: api=%s data=%s poll=%s mode=%s\n",
		doctorVersion, apiURL, hostDataDir, poll, env("DOCTOR_MODE", "repair"))

	httpClient := &http.Client{Timeout: 30 * time.Second}
	opts := []connect.ClientOption{connect.WithInterceptors(authInterceptor(apiToken))}
	panel := &panelClient{
		servers:    discopanelv1connect.NewServerServiceClient(httpClient, apiURL, opts...),
		properties: discopanelv1connect.NewPropertiesServiceClient(httpClient, apiURL, opts...),
	}

	d := &doctor{
		panel:       panel,
		hostDataDir: filepath.Clean(hostDataDir),
		mountDir:    filepath.Clean(mountDir),
		poll:        poll,
		repair:      repair,
	}
	d.engine = &engine{panel: panel, logf: d.logf}
	if repair && installDeps {
		d.engine.installer = newDepInstaller("discopanel-doctor/"+doctorVersion, panel)
	}

	go d.loop()

	http.HandleFunc("/", d.handleIndex)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	fmt.Printf("Listening :%d\n", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		fmt.Fprintf(os.Stderr, "listen failed: %v\n", err)
		os.Exit(1)
	}
}

// Watches every panel-managed server and repairs crashes
type doctor struct {
	panel       *panelClient
	engine      *engine
	hostDataDir string
	mountDir    string
	poll        time.Duration
	repair      bool

	mu      sync.RWMutex
	rows    []statusRow
	lastErr error
	logs    []string
}

func (d *doctor) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05") + " " + fmt.Sprintf(format, args...)
	fmt.Println("[doctor] " + line)
	d.mu.Lock()
	d.logs = append(d.logs, line)
	if len(d.logs) > 200 {
		d.logs = d.logs[len(d.logs)-200:]
	}
	d.mu.Unlock()
}

func (d *doctor) loop() {
	d.cycle()
	for range time.Tick(d.poll) {
		d.cycle()
	}
}

// One discovery and repair sweep across all servers
func (d *doctor) cycle() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	resp, err := d.panel.servers.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{}))
	if err != nil {
		d.mu.Lock()
		d.lastErr = err
		d.mu.Unlock()
		return
	}

	var rows []statusRow
	for _, s := range resp.Msg.GetServers() {
		srv := d.toInfo(s)
		if srv == nil {
			continue
		}
		if d.repair {
			d.engine.checkServer(ctx, srv)
		}
		d.publishFindings(ctx, srv, s)
		rows = append(rows, d.rowFor(srv))
	}

	d.mu.Lock()
	d.rows = rows
	d.lastErr = nil
	d.mu.Unlock()
}

// Maps a panel server onto local facts, nil when unmapped
func (d *doctor) toInfo(s *v1.Server) *serverInfo {
	hostPath := filepath.Clean(s.GetDataPath())
	rel, err := filepath.Rel(d.hostDataDir, hostPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	local := filepath.Join(d.mountDir, rel)
	if !fileExists(local) {
		return nil
	}

	status := s.GetStatus()
	return &serverInfo{
		ID:        s.GetId(),
		Name:      s.GetName(),
		DataPath:  local,
		ModLoader: s.GetModLoader(),
		McVersion: s.GetMcVersion(),
		Running:   status == v1.ServerStatus_SERVER_STATUS_RUNNING,
		Stopped:   status == v1.ServerStatus_SERVER_STATUS_STOPPED || status == v1.ServerStatus_SERVER_STATUS_STOPPING,
	}
}

// Snapshot of one server for the status page
type statusRow struct {
	Name     string
	Running  bool
	Incident string
	Resolved string
	Excludes []string
	Exits    int
}

func (d *doctor) rowFor(srv *serverInfo) statusRow {
	j := runtimespec.LoadDoctor(srv.DataPath)
	row := statusRow{
		Name:     srv.Name,
		Running:  srv.Running,
		Excludes: j.Excludes,
		Exits:    exitsWithin(runtimespec.ReadExitHistory(srv.DataPath), crashLoopWindow),
	}
	if j.Incident != nil {
		row.Incident = incidentLine(j.Incident)
	}
	if j.Resolved != nil && j.Resolved.Summary != "" {
		row.Resolved = protometa.Name(j.Resolved.Outcome) + ": " + j.Resolved.Summary
	}
	return row
}

// Panel API actor shared by the engine
type panelClient struct {
	servers    discopanelv1connect.ServerServiceClient
	properties discopanelv1connect.PropertiesServiceClient
}

func (p *panelClient) Restart(ctx context.Context, serverID string) error {
	_, err := p.servers.RestartServer(ctx, connect.NewRequest(&v1.RestartServerRequest{Id: serverID}))
	return err
}

func (p *panelClient) Stop(ctx context.Context, serverID string) error {
	_, err := p.servers.StopServer(ctx, connect.NewRequest(&v1.StopServerRequest{Id: serverID}))
	return err
}

// Files the user force-includes never get disabled
// Indexer registrations declare where those patterns live
func (p *panelClient) ForcePatterns(ctx context.Context, serverID string) []string {
	props := p.serverProperties(ctx, serverID)
	var patterns []string
	for _, info := range indexers.Indexers() {
		if info.ForceIncludeProperty == "" {
			continue
		}
		if v := props[info.ForceIncludeProperty]; v != "" {
			patterns = append(patterns, minecraft.SplitPatterns(v)...)
		}
	}
	return patterns
}

// Server property wins, panel-wide global settings are the fallback
func (p *panelClient) PropertyValue(ctx context.Context, serverID, key string) string {
	if v := p.serverProperties(ctx, serverID)[key]; v != "" {
		return v
	}
	resp, err := p.properties.GetGlobalSettings(ctx, connect.NewRequest(&v1.GetGlobalSettingsRequest{}))
	if err != nil {
		return ""
	}
	return categoriesValue(resp.Msg.GetCategories(), key)
}

// Flattens one server's property categories into a map
func (p *panelClient) serverProperties(ctx context.Context, serverID string) map[string]string {
	resp, err := p.properties.GetServerProperties(ctx, connect.NewRequest(&v1.GetServerPropertiesRequest{ServerId: serverID}))
	if err != nil {
		return nil
	}
	props := map[string]string{}
	for _, cat := range resp.Msg.GetCategories() {
		for _, prop := range cat.GetProperties() {
			props[prop.GetKey()] = prop.GetValue()
		}
	}
	return props
}

func categoriesValue(cats []*v1.PropertyCategory, key string) string {
	for _, cat := range cats {
		for _, prop := range cat.GetProperties() {
			if prop.GetKey() == key {
				return prop.GetValue()
			}
		}
	}
	return ""
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

// Injects Bearer token into every outgoing RPC request
func authInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	}
}

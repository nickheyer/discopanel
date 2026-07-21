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
	defMode := env("DOCTOR_MODE", "repair")
	defInstall := env("DOCTOR_INSTALL_DEPS", "on") != "off"

	if apiToken == "" {
		fmt.Fprintln(os.Stderr, "DISCOPANEL_API_TOKEN required")
		os.Exit(1)
	}
	if hostDataDir == "" {
		fmt.Fprintln(os.Stderr, "DISCOPANEL_DATA_DIR required to map server paths")
		os.Exit(1)
	}

	fmt.Printf("DiscoPanel Doctor %s: api=%s data=%s poll=%s mode=%s\n",
		doctorVersion, apiURL, hostDataDir, poll, defMode)

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
		defMode:     defMode,
		defInstall:  defInstall,
	}
	// Settings gate installs per server, installer stays ready
	d.engine = &engine{panel: panel, logf: d.logf, installer: newDepInstaller("discopanel-doctor/"+doctorVersion, panel)}

	go d.loop()

	http.HandleFunc("/", d.handleIndex)
	http.HandleFunc("/assets/inter.woff2", handleFont)
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
	defMode     string
	defInstall  bool

	mu        sync.RWMutex
	rows      []statusRow
	lastErr   error
	lastSweep time.Time
	logs      []string
}

// Doctor behavior for one server, settings win over env
type doctorPrefs struct {
	Enabled     bool
	Repair      bool
	InstallDeps bool
	Mode        string
}

// Resolves prefs from server props, global settings, env
func (d *doctor) resolvePrefs(props, global map[string]string) doctorPrefs {
	mode := firstNonEmpty(props["doctorMode"], global["doctorMode"], d.defMode)
	enabled := firstNonEmpty(props["doctorEnabled"], global["doctorEnabled"]) != "false"
	install := d.defInstall
	if v := firstNonEmpty(props["doctorInstallDeps"], global["doctorInstallDeps"]); v != "" {
		install = v == "true"
	}
	return doctorPrefs{
		Enabled:     enabled,
		Repair:      mode != "observe",
		InstallDeps: install,
		Mode:        mode,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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

	global := d.panel.globalProperties(ctx)
	var rows []statusRow
	for _, s := range resp.Msg.GetServers() {
		srv := d.toInfo(s)
		if srv == nil {
			continue
		}
		props := d.panel.serverProperties(ctx, srv.ID)
		prefs := d.resolvePrefs(props, global)
		srv.InstallDeps = prefs.InstallDeps
		if prefs.Enabled {
			if prefs.Repair {
				d.engine.checkServer(ctx, srv)
			}
			d.publishFindings(ctx, srv, s, props)
		} else {
			d.publishParked(srv)
		}
		rows = append(rows, d.rowFor(srv, prefs))
	}

	d.mu.Lock()
	d.rows = rows
	d.lastErr = nil
	d.lastSweep = time.Now()
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
	Enabled  bool
	Mode     string
	Exits    int
	Excludes []string
	Incident *incidentView
	Resolved *incidentView
	Findings []findingView
}

// Incident details rendered on the status page
type incidentView struct {
	Open    bool
	Passes  int
	Max     int
	PassPct int
	Budget  int
	Used    int
	Cause   string
	Summary string
	Outcome string
	Age     string
	Actions []actionView
}

// One journal action rendered on the status page
type actionView struct {
	Kind     string
	File     string
	Reason   string
	Evidence string
	Reverted bool
}

// One published finding rendered on the status page
type findingView struct {
	Severity string
	Title    string
}

func (d *doctor) rowFor(srv *serverInfo, prefs doctorPrefs) statusRow {
	j := runtimespec.LoadDoctor(srv.DataPath)
	row := statusRow{
		Name:     srv.Name,
		Running:  srv.Running,
		Enabled:  prefs.Enabled,
		Mode:     prefs.Mode,
		Excludes: j.Excludes,
		Exits:    exitsWithin(runtimespec.ReadExitHistory(srv.DataPath), crashLoopWindow),
	}
	if j.Incident != nil {
		row.Incident = incidentViewOf(j.Incident, true)
	}
	if j.Resolved != nil && j.Resolved.Summary != "" {
		row.Resolved = incidentViewOf(j.Resolved, false)
	}
	for _, f := range runtimespec.ReadFindings(srv.DataPath) {
		row.Findings = append(row.Findings, findingView{
			Severity: protometa.Name(f.Severity),
			Title:    f.Title,
		})
	}
	return row
}

func incidentViewOf(inc *v1.DoctorIncident, open bool) *incidentView {
	view := &incidentView{
		Open:    open,
		Passes:  int(inc.Passes),
		Max:     maxDoctorPasses,
		Budget:  int(inc.Budget),
		Used:    runtimespec.DisabledCount(inc),
		Cause:   inc.Cause,
		Summary: inc.Summary,
		Age:     ago(runtimespec.LastActivity(inc).AsTime()),
	}
	if view.Max > 0 {
		view.PassPct = min(view.Passes*100/view.Max, 100)
	}
	if !open {
		view.Outcome = protometa.Name(inc.Outcome)
	}
	for _, a := range inc.Actions {
		view.Actions = append(view.Actions, actionView{
			Kind:     actionVerb(a.Kind),
			File:     firstNonEmpty(a.File, a.ModId),
			Reason:   a.Reason,
			Evidence: protometa.Name(a.Evidence),
			Reverted: a.Reverted,
		})
	}
	return view
}

// Past tense verb for one journal action kind
func actionVerb(kind v1.DoctorActionKind) string {
	switch kind {
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE:
		return "disabled"
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_ENABLE:
		return "re-enabled"
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_INSTALL:
		return "installed"
	case v1.DoctorActionKind_DOCTOR_ACTION_KIND_DISABLE_PACK:
		return "pack disabled"
	}
	return protometa.Name(kind)
}

// Short relative time for the status page
func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	since := time.Since(t)
	switch {
	case since < time.Minute:
		return "just now"
	case since < time.Hour:
		return fmt.Sprintf("%dm ago", int(since.Minutes()))
	case since < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(since.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(since.Hours()/24))
	}
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
	return p.globalProperties(ctx)[key]
}

// Flattens one server's property categories into a map
func (p *panelClient) serverProperties(ctx context.Context, serverID string) map[string]string {
	resp, err := p.properties.GetServerProperties(ctx, connect.NewRequest(&v1.GetServerPropertiesRequest{ServerId: serverID}))
	if err != nil {
		return nil
	}
	return flattenCategories(resp.Msg.GetCategories())
}

// Flattens the panel-wide global settings into a map
func (p *panelClient) globalProperties(ctx context.Context) map[string]string {
	resp, err := p.properties.GetGlobalSettings(ctx, connect.NewRequest(&v1.GetGlobalSettingsRequest{}))
	if err != nil {
		return nil
	}
	return flattenCategories(resp.Msg.GetCategories())
}

func flattenCategories(cats []*v1.PropertyCategory) map[string]string {
	props := map[string]string{}
	for _, cat := range cats {
		for _, prop := range cat.GetProperties() {
			props[prop.GetKey()] = prop.GetValue()
		}
	}
	return props
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

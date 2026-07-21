package module

import (
	"context"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/config"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Template id of the global crash doctor module
const doctorTemplateID = "builtin-doctor"

// Panel user keeps doctor writes owned by the panel
const (
	doctorUID = "{{host.uid}}"
	doctorGID = "{{host.gid}}"
)

// Default web port for the seeded doctor instance
func doctorPorts(cfg *config.Config) []*v1.ModulePort {
	port := int32(8190)
	proxied := false
	if cfg != nil {
		if cfg.Module.PortRangeMax > 0 {
			port = int32(cfg.Module.PortRangeMax)
		}
		// Direct host bind keeps doctor reachable without proxy
		proxied = cfg.Proxy.Enabled
	}
	return []*v1.ModulePort{
		{Name: "Web", ContainerPort: 8190, HostPort: port, Protocol: v1.ModuleProtocol_MODULE_PROTOCOL_HTTP, ProxyEnabled: proxied},
	}
}

func doctorEnv() map[string]string {
	return map[string]string{
		"DISCOPANEL_DATA_DIR": "{{config.storage.data_dir}}",
		"POLL_INTERVAL":       "15s",
		"DOCTOR_MODE":         "repair",
		"DOCTOR_INSTALL_DEPS": "on",
		"PORT":                "8190",
	}
}

// Default access urls for the seeded doctor instance
func doctorAccessURLs() []string {
	return []string{"http://{{host.hostname}}:{{module.ports.Web.host_port}}"}
}

func doctorVolumes() []*v1.VolumeMount {
	return []*v1.VolumeMount{
		{Source: "{{config.storage.data_dir}}", Target: "/data"},
	}
}

// Seeds missing built-in templates, never touches existing rows
func InitBuiltinTemplates(store *storage.Store) error {
	ctx := context.Background()

	templates := []*v1.ModuleTemplate{
		{
			Id:             "builtin-geyser",
			Name:           "Geyser",
			Description:    "Allows Bedrock Edition players to join Java Edition servers. Requires Floodgate plugin on the server for seamless authentication.",
			Type:           v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN,
			DockerImage:    "nickheyer/discopanel-geyser:latest",
			Category:       "proxy",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "users",
			Ports: []*v1.ModulePort{
				{Name: "Bedrock", ContainerPort: 19132, HostPort: 0, Protocol: v1.ModuleProtocol_MODULE_PROTOCOL_UDP, ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Bedrock.host_port}}"},
			DefaultEnv: map[string]string{
				"PUID":               "{{host.uid}}",
				"PGID":               "{{host.gid}}",
				"OVERWRITE_CONFIG":   "false",
				"BEDROCK_ADDRESS":    "0.0.0.0",
				"BEDROCK_PORT":       "{{module.ports.Bedrock.container_port}}",
				"BEDROCK_MOTD1":      "GeyserMC",
				"BEDROCK_MOTD2":      "Minecraft Server",
				"BEDROCK_SERVERNAME": "Geyser",
				"REMOTE_ADDRESS":     "discopanel-server-{{server.id}}",
				"REMOTE_PORT":        "{{server.container_port}}",
				"REMOTE_AUTH_TYPE":   "offline",
			},
			DefaultVolumes: []*v1.VolumeMount{
				{Source: "{{server.data_path}}/modules/geyser", Target: "/data"},
			},
			Documentation:   "Geyser acts as a proxy, translating Bedrock packets to Java packets.",
			HealthCheckPort: 19132,
			DefaultMemory:   1024,
		},
		{
			Id:             "builtin-minecraft-exporter",
			Name:           "Prometheus Exporter",
			Description:    "Exports Minecraft server metrics to Prometheus for monitoring dashboards",
			Type:           v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN,
			DockerImage:    "nickheyer/discopanel-exporter:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "chart-bar",
			Ports: []*v1.ModulePort{
				{Name: "Metrics", ContainerPort: 9225, HostPort: 0, Protocol: v1.ModuleProtocol_MODULE_PROTOCOL_HTTP, ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Metrics.host_port}}/metrics"},
			DefaultEnv: map[string]string{
				"EXPORT_SERVERS": "discopanel-server-{{server.id}}:{{server.container_port}}",
				"EXPORT_PORT":    "{{module.ports.Metrics.container_port}}",
			},
			HealthCheckPath: "/metrics",
			HealthCheckPort: 9225,
			Documentation:   "Exports server status, player count, TPS, and other metrics in Prometheus format. Connect to /metrics endpoint to scrape metrics.",
			DefaultMemory:   512,
		},
		{
			Id:             "builtin-bluemap",
			Name:           "BlueMap",
			Description:    "Interactive 3D map renderer for Minecraft worlds with a web-based viewer. Renders overworld, nether, and end dimensions.",
			Type:           v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN,
			DockerImage:    "ghcr.io/bluemap-minecraft/bluemap:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "map",
			DefaultCmd:     "-r -u -w",
			Ports: []*v1.ModulePort{
				{Name: "Web", ContainerPort: 8100, HostPort: 0, Protocol: v1.ModuleProtocol_MODULE_PROTOCOL_HTTP, ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Web.host_port}}"},
			DefaultVolumes: []*v1.VolumeMount{
				{Source: "{{server.data_path}}/modules/bluemap/config", Target: "/app/config", CreateDir: true},
				{Source: "{{server.data_path}}/world", Target: "/app/world", ReadOnly: true},
				{Source: "{{server.data_path}}/modules/bluemap/data", Target: "/app/data", CreateDir: true},
				{Source: "{{server.data_path}}/modules/bluemap/web", Target: "/app/web", CreateDir: true},
			},
			HealthCheckPath:         "/",
			HealthCheckPort:         8100,
			Documentation:           "Renders 3D maps of your Minecraft worlds accessible via a web interface. Supports overworld, nether, and end dimensions. World volumes are mounted read-only from the server data path. Config, data, and web assets are stored in the bluemap module directory.",
			DefaultMemory:           2048,
			DefaultUid:              "{{host.uid}}",
			DefaultGid:              "{{host.gid}}",
			DefaultInitCommand:      `sed -i 's/accept-download: false/accept-download: true/' /app/config/core.conf`,
			DefaultInitCommandDelay: 1,
			DefaultRestartAfterInit: true,
		},
		{
			Id:             "builtin-status-panel",
			Name:           "Status Panel",
			Description:    "Real-time server status dashboard showing player count, TPS, memory usage, and server info via the DiscoPanel API.",
			Type:           v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN,
			DockerImage:    "nickheyer/discopanel-status:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "monitor",
			Ports: []*v1.ModulePort{
				{Name: "Web", ContainerPort: 8181, HostPort: 0, Protocol: v1.ModuleProtocol_MODULE_PROTOCOL_HTTP, ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Web.host_port}}"},
			DefaultEnv: map[string]string{
				"POLL_INTERVAL": "10s",
				"PORT":          "{{module.ports.Web.container_port}}",
			},
			HealthCheckPath: "/health",
			HealthCheckPort: 8181,
			Documentation:   "Displays a real-time status dashboard for the attached Minecraft server. Fetches status via the DiscoPanel API including player count, TPS, CPU/memory usage, and server configuration. Automatically refreshes every 10 seconds.",
			DefaultMemory:   512,
		},
		{
			Id:             doctorTemplateID,
			Name:           "Doctor",
			Description:    "Global crash doctor. Watches every DiscoPanel server, diagnoses crashes from structured exit reports, disables or sources mods with a full revert trail, and verifies repairs by restarting through the panel.",
			Type:           v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN,
			DockerImage:    "nickheyer/discopanel-doctor:latest",
			Category:       "automation",
			SupportsProxy:  true,
			RequiresServer: false,
			Icon:           "stethoscope",
			Ports: []*v1.ModulePort{
				{Name: "Web", ContainerPort: 8190, HostPort: 0, Protocol: v1.ModuleProtocol_MODULE_PROTOCOL_HTTP, ProxyEnabled: true},
			},
			DefaultAccessUrls: doctorAccessURLs(),
			DefaultEnv:        doctorEnv(),
			DefaultVolumes:    doctorVolumes(),
			HealthCheckPath:   "/health",
			HealthCheckPort:   8190,
			DefaultUid:        doctorUID,
			DefaultGid:        doctorGID,
			Metadata:          map[string]string{"module_role": "doctor"},
			Documentation:     "Runs as a single global module for the whole panel. Discovers servers through the DiscoPanel API, watches their exit history on the shared data volume, and repairs crash loops with reversible mod disables, re-enables, and dependency installs from CurseForge or Modrinth using the panel API keys. Configure it from the Doctor category in Settings, with per-server overrides on each server's properties page. Doctor Mode observe diagnoses without acting, and Install Missing Dependencies off disables downloads. Stop the module or turn off auto start to disable it entirely.",
			DefaultMemory:     512,
		},
	}

	// Insert only when missing so user edits survive restarts
	for _, template := range templates {
		if _, err := store.GetModuleTemplate(ctx, template.Id); err == nil {
			continue
		}
		if err := store.CreateModuleTemplate(ctx, template); err != nil {
			return err
		}
	}

	// Removes obsolete templates unless modules still use them
	for _, id := range []string{"builtin-mc-backup", "builtin-rcon-web"} {
		if _, err := store.GetModuleTemplate(ctx, id); err != nil {
			continue
		}
		if err := store.DeleteModuleTemplate(ctx, id); err != nil {
			// Modules still reference it, leave in place
			continue
		}
	}

	return nil
}

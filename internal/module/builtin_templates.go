package module

import (
	"context"

	storage "github.com/nickheyer/discopanel/internal/db"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// InitBuiltinTemplates creates/updates built-in module templates
// Only includes templates with real, working Docker images
func InitBuiltinTemplates(store *storage.Store) error {
	ctx := context.Background()

	templates := []storage.ModuleTemplate{
		{
			ID:             "builtin-geyser",
			Name:           "Geyser",
			Description:    "Allows Bedrock Edition players to join Java Edition servers. Requires Floodgate plugin on the server for seamless authentication.",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "nickheyer/discopanel-geyser:latest",
			Category:       "proxy",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "users",
			Ports: []*v1.ModulePort{
				{Name: "Bedrock", ContainerPort: 19132, HostPort: 0, Protocol: "udp", ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{server.proxy_hostname}}:{{module.ports.Bedrock.host_port}}"},
			DefaultEnv: `{
				"PUID": "{{host.uid}}",
				"PGID": "{{host.gid}}",
				"OVERWRITE_CONFIG": "false",
				"BEDROCK_ADDRESS": "0.0.0.0",
				"BEDROCK_PORT": "{{module.ports.Bedrock.container_port}}",
				"BEDROCK_MOTD1": "GeyserMC",
				"BEDROCK_MOTD2": "Minecraft Server",
				"BEDROCK_SERVERNAME": "Geyser",
				"REMOTE_ADDRESS": "discopanel-server-{{server.id}}",
				"REMOTE_PORT": "25565",
				"REMOTE_AUTH_TYPE": "offline"
			}`,
			DefaultVolumes:  `[{"source": "{{server.data_path}}/modules/geyser", "target": "/data", "read_only": false}]`,
			Documentation:   "Geyser acts as a proxy, translating Bedrock packets to Java packets.",
			HealthCheckPort: 19132,
			DefaultMemory:   1024,
		},
		{
			ID:             "builtin-mc-backup",
			Name:           "MC Backup",
			Description:    "Automated backup solution for Minecraft server worlds with RCON-coordinated saves and configurable retention policies",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "itzg/mc-backup:latest",
			Category:       "utilities",
			SupportsProxy:  false,
			RequiresServer: true,
			Icon:           "archive",
			Ports:          []*v1.ModulePort{},
			DefaultEnv: `{
				"RCON_HOST": "discopanel-server-{{server.id}}",
				"RCON_PORT": "{{server.config.rconPort}}",
				"RCON_PASSWORD": "{{server.config.rconPassword}}",
				"SRC_DIR": "/data",
				"DEST_DIR": "/backups",
				"BACKUP_NAME": "world",
				"BACKUP_METHOD": "tar",
				"BACKUP_INTERVAL": "24h",
				"INITIAL_DELAY": "2m",
				"BACKUP_ON_STARTUP": "true",
				"PRUNE_BACKUPS_DAYS": "7",
				"PAUSE_IF_NO_PLAYERS": "false",
				"EXCLUDES": "*.jar,cache,logs,*.tmp",
				"TZ": "{{server.config.tz}}"
			}`,
			DefaultVolumes: `[{"source": "{{server.data_path}}", "target": "/data", "read_only": true}, {"source": "{{config.storage.backup_dir}}", "target": "/backups", "read_only": false}]`,
			Documentation:  "Coordinates backups with the Minecraft server via RCON. Automatically flushes data, pauses writes, and resumes after backup. RCON settings are pulled from server config. Backups stored in global backup directory.",
			DefaultMemory:  256,
		},
		{
			ID:             "builtin-rcon-web",
			Name:           "RCON Web Admin",
			Description:    "Web-based RCON client for remote server management with command history and multi-server support",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "itzg/rcon:latest",
			Category:       "management",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "terminal",
			Ports: []*v1.ModulePort{
				{Name: "Web", ContainerPort: 4326, HostPort: 0, Protocol: "http", ProxyEnabled: true},
				{Name: "WS", ContainerPort: 4327, HostPort: 0, Protocol: "http", ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{server.proxy_hostname}}:{{module.ports.Web.host_port}}"},
			DefaultEnv: `{
				"RWA_ADMIN": "true",
				"RWA_PASSWORD": "admin",
				"RWA_RCON_HOST": "discopanel-server-{{server.id}}",
				"RWA_RCON_PORT": "{{server.config.rconPort}}",
				"RWA_RCON_PASSWORD": "{{server.config.rconPassword}}",
				"RWA_WEBSOCKET_URL": "ws://{{server.proxy_hostname}}:{{module.ports.WS.host_port}}"
			}`,
			DefaultVolumes:  `[]`,
			HealthCheckPath: "/",
			HealthCheckPort: 4326,
			Documentation:   "Provides a web interface for RCON commands. RCON settings are pulled from server config. Web UI on port 4326, WebSocket on port 4327 - both need to be accessible.",
			DefaultMemory:   256,
		},
		{
			ID:             "builtin-minecraft-exporter",
			Name:           "Prometheus Exporter",
			Description:    "Exports Minecraft server metrics to Prometheus for monitoring dashboards",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "itzg/mc-monitor:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "chart-bar",
			DefaultCmd:     "export-for-prometheus",
			Ports: []*v1.ModulePort{
				{Name: "Metrics", ContainerPort: 9225, HostPort: 0, Protocol: "http", ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{server.proxy_hostname}}:{{module.ports.Metrics.host_port}}/metrics"},
			DefaultEnv: `{
				"EXPORT_SERVERS": "discopanel-server-{{server.id}}:25565",
				"EXPORT_PORT": "{{module.ports.Metrics.container_port}}"
			}`,
			DefaultVolumes:  `[]`,
			HealthCheckPath: "/metrics",
			HealthCheckPort: 9225,
			Documentation:   "Exports server status, player count, TPS, and other metrics in Prometheus format. Connect to /metrics endpoint to scrape metrics.",
			DefaultMemory:   512,
		},
		{
			ID:             "builtin-status-panel",
			Name:           "Status Panel",
			Description:    "Real-time server status dashboard showing player count, TPS, memory usage, and server info via the DiscoPanel API.",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "nickheyer/discopanel-status:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "monitor",
			Ports: []*v1.ModulePort{
				{Name: "Web", ContainerPort: 8181, HostPort: 0, Protocol: "http", ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{server.proxy_hostname}}:{{module.ports.Web.host_port}}"},
			DefaultEnv: `{
				"DISCOPANEL_URL": "http://host.docker.internal:{{config.server.port}}",
				"POLL_INTERVAL": "10s",
				"PORT": "{{module.ports.Web.container_port}}"
			}`,
			DefaultVolumes:  `[]`,
			HealthCheckPath: "/health",
			HealthCheckPort: 8181,
			Documentation:   "Displays a real-time status dashboard for the attached Minecraft server. Fetches status via the DiscoPanel API including player count, TPS, CPU/memory usage, and server configuration. Automatically refreshes every 10 seconds.",
			DefaultMemory:   512,
		},
	}

	// Upsert each template
	for _, template := range templates {
		if err := store.UpsertModuleTemplate(ctx, &template); err != nil {
			return err
		}
	}

	return nil
}

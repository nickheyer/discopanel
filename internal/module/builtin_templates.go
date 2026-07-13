package module

import (
	"context"

	storage "github.com/nickheyer/discopanel/internal/db"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Creates or updates built-in templates with real Docker images
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
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Bedrock.host_port}}"},
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
			ID:             "builtin-minecraft-exporter",
			Name:           "Prometheus Exporter",
			Description:    "Exports Minecraft server metrics to Prometheus for monitoring dashboards",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "nickheyer/discopanel-exporter:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "chart-bar",
			Ports: []*v1.ModulePort{
				{Name: "Metrics", ContainerPort: 9225, HostPort: 0, Protocol: "http", ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Metrics.host_port}}/metrics"},
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
			ID:             "builtin-bluemap",
			Name:           "BlueMap",
			Description:    "Interactive 3D map renderer for Minecraft worlds with a web-based viewer. Renders overworld, nether, and end dimensions.",
			Type:           storage.ModuleTemplateTypeBuiltin,
			DockerImage:    "ghcr.io/bluemap-minecraft/bluemap:latest",
			Category:       "monitoring",
			SupportsProxy:  true,
			RequiresServer: true,
			Icon:           "map",
			DefaultCmd:     "-r -u -w",
			Ports: []*v1.ModulePort{
				{Name: "Web", ContainerPort: 8100, HostPort: 0, Protocol: "http", ProxyEnabled: true},
			},
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Web.host_port}}"},
			DefaultEnv:        `{}`,
			DefaultVolumes: `[
				{"source": "{{server.data_path}}/modules/bluemap/config", "target": "/app/config", "read_only": false, "create_dir": true},
				{"source": "{{server.data_path}}/world", "target": "/app/world", "read_only": true},
				{"source": "{{server.data_path}}/modules/bluemap/data", "target": "/app/data", "read_only": false, "create_dir": true},
				{"source": "{{server.data_path}}/modules/bluemap/web", "target": "/app/web", "read_only": false, "create_dir": true}
				]`,
			HealthCheckPath:         "/",
			HealthCheckPort:         8100,
			Documentation:           "Renders 3D maps of your Minecraft worlds accessible via a web interface. Supports overworld, nether, and end dimensions. World volumes are mounted read-only from the server data path. Config, data, and web assets are stored in the bluemap module directory.",
			DefaultMemory:           2048,
			DefaultUID:              "{{host.uid}}",
			DefaultGID:              "{{host.gid}}",
			DefaultInitCommand:      `sed -i 's/accept-download: false/accept-download: true/' /app/config/core.conf`,
			DefaultInitCommandDelay: 1,
			DefaultRestartAfterInit: true,
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
			DefaultAccessUrls: []string{"http://{{host.hostname}}:{{module.ports.Web.host_port}}"},
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

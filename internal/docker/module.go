package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	shellparse "github.com/arkady-emelyanov/go-shellparse"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/nickheyer/discopanel/internal/alias"
	"github.com/nickheyer/discopanel/internal/config"
	models "github.com/nickheyer/discopanel/internal/db"
)

// ModuleVolumeMount represents a volume mount from module configuration
type ModuleVolumeMount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Type     string `json:"type,omitempty"` // "bind" or "volume"
}

// Create container for a module w/ optional map of sibling modules by name for inter-module references
func (c *Client) CreateModuleContainer(ctx context.Context, module *models.Module, template *models.ModuleTemplate, server *models.Server, serverConfig *models.ServerConfig, cfg *config.Config, siblingModules ...map[string]*models.Module) (string, error) {
	// Determine the Docker image to use
	imageName := template.DockerImage
	if imageName == "" {
		return "", fmt.Errorf("module template has no Docker image configured")
	}

	// Try pulling the image
	if err := c.pullImage(ctx, imageName); err != nil {
		c.log.Warn("Failed to pull image %s: %v, attempting to use local", imageName, err)
	}

	// Build alias context for substitution (needed for env and volumes)
	aliasCtx := &alias.Context{
		Server:       server,
		ServerConfig: serverConfig,
		Module:       module,
		Config:       cfg,
	}
	// Add sibling modules for inter-module references
	if len(siblingModules) > 0 && siblingModules[0] != nil {
		aliasCtx.Modules = siblingModules[0]
	}

	// Build environment variables
	env := c.buildModuleEnv(module, server, aliasCtx)

	c.log.Debug("Creating container for module %s with image %s", module.ID, imageName)

	// Build exposed ports and port bindings from module.Ports
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}

	for _, port := range module.Ports {
		if port == nil || port.ContainerPort == 0 {
			continue
		}

		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}

		// Add to exposed ports (for internal Docker network access)
		exposedPorts[nat.Port(fmt.Sprintf("%d/%s", port.ContainerPort, protocol))] = struct{}{}

		// Add port binding if host port specified and proxy not enabled for this port
		// When proxy is enabled, the DiscoPanel proxy handles host port binding
		if port.HostPort > 0 && !port.ProxyEnabled {
			portBindings[nat.Port(fmt.Sprintf("%d/%s", port.ContainerPort, protocol))] = []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", port.HostPort)},
			}
		}

		c.log.Debug("Added port for module %s: %s (%d:%d/%s, proxy=%t)",
			module.ID, port.Name, port.HostPort, port.ContainerPort, protocol, port.ProxyEnabled)
	}

	// Build mounts from module configuration only (frontend sends complete config)
	mounts := c.parseVolumeMounts(module.VolumeOverrides, aliasCtx)

	config := &container.Config{
		Image:        imageName,
		Env:          env,
		Tty:          true,
		AttachStdout: true,
		AttachStderr: true,
		ExposedPorts: exposedPorts,
		Labels: map[string]string{
			"discopanel.module.id":          module.ID,
			"discopanel.module.name":        module.Name,
			"discopanel.module.server_id":   module.ServerID,
			"discopanel.module.template_id": module.TemplateID,
			"discopanel.managed":            "true",
		},
	}

	// Set container command if specified (module override takes precedence over template default)
	cmd := module.CmdOverride
	if cmd == "" {
		cmd = template.DefaultCmd
	}
	if cmd != "" {
		// Parse command string into args using shell parsing lib
		cmdArgs, err := shellparse.StringToSlice(cmd)
		if err != nil {
			c.log.Warn("Failed to parse module command %q: %v, using as single arg", cmd, err)
			cmdArgs = []string{cmd}
		}
		config.Cmd = cmdArgs
		c.log.Debug("Setting container command for module %s: %v", module.ID, config.Cmd)
	}

	// Configure resources
	memory := int64(module.Memory)
	if memory == 0 {
		memory = 512 // Default 512MB
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Mounts:       mounts,
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		Resources: container.Resources{
			Memory:     memory * 1024 * 1024,
			MemorySwap: memory * 1024 * 1024,
		},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}

	// Apply CPU limit if specified
	if module.CPULimit > 0 {
		hostConfig.Resources.NanoCPUs = int64(module.CPULimit * 1e9)
	}

	// Network configuration - same network as server for communication
	networkConfig := &network.NetworkingConfig{}
	if c.config.NetworkName != "" {
		networkConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			c.config.NetworkName: {},
		}
	}

	// Create the container
	resp, err := c.docker.ContainerCreate(
		ctx, config, hostConfig, networkConfig, nil,
		fmt.Sprintf("discopanel-module-%s", module.ID),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create module container: %w", err)
	}

	return resp.ID, nil
}

// buildModuleEnv builds environment variables for a module container
func (c *Client) buildModuleEnv(module *models.Module, server *models.Server, aliasCtx *alias.Context) []string {
	env := make([]string, 0)

	// Add DiscoPanel context variables
	env = append(env,
		fmt.Sprintf("DISCOPANEL_SERVER_ID=%s", server.ID),
		fmt.Sprintf("DISCOPANEL_SERVER_NAME=%s", server.Name),
		fmt.Sprintf("DISCOPANEL_SERVER_HOST=discopanel-server-%s", server.ID),
		fmt.Sprintf("DISCOPANEL_SERVER_PORT=%d", DefaultMinecraftPort),
		fmt.Sprintf("DISCOPANEL_MODULE_ID=%s", module.ID),
		fmt.Sprintf("DISCOPANEL_MODULE_NAME=%s", module.Name),
	)

	// Add module environment variables (frontend sends complete config with alias substitution)
	if module.EnvOverrides != "" {
		var envOverrides map[string]string
		if err := json.Unmarshal([]byte(module.EnvOverrides), &envOverrides); err == nil {
			for key, value := range envOverrides {
				resolvedValue := alias.Substitute(value, aliasCtx)
				env = append(env, fmt.Sprintf("%s=%s", key, resolvedValue))
			}
		}
	}

	return env
}

// parseVolumeMounts parses JSON volume configuration and returns mounts
func (c *Client) parseVolumeMounts(volumeJSON string, aliasCtx *alias.Context) []mount.Mount {
	if volumeJSON == "" || volumeJSON == "[]" {
		return nil
	}

	var volumes []ModuleVolumeMount
	if err := json.Unmarshal([]byte(volumeJSON), &volumes); err != nil {
		c.log.Warn("Failed to parse volume configuration: %v", err)
		return nil
	}

	var mounts []mount.Mount

	for _, vol := range volumes {
		mountType := mount.TypeBind
		if vol.Type == "volume" {
			mountType = mount.TypeVolume
		}

		// Substitute aliases in source and target paths
		source := alias.Substitute(vol.Source, aliasCtx)
		target := alias.Substitute(vol.Target, aliasCtx)

		// Skip mounts with empty source or target
		if source == "" || target == "" {
			c.log.Warn("Skipping volume mount with empty source or target: source=%q, target=%q", source, target)
			continue
		}

		// Handle path translation when DiscoPanel runs in a container
		if mountType == mount.TypeBind {
			if envHostDataPath := os.Getenv("DISCOPANEL_HOST_DATA_PATH"); envHostDataPath != "" {
				containerDataDir := os.Getenv("DISCOPANEL_DATA_DIR")
				if containerDataDir == "" {
					containerDataDir = "/app/data"
				}
				if relPath, err := filepath.Rel(containerDataDir, source); err == nil {
					source = filepath.Join(envHostDataPath, relPath)
				}
			}
		}

		// Ensure source directory exists for bind mounts
		if mountType == mount.TypeBind {
			if err := os.MkdirAll(source, 0755); err != nil {
				c.log.Warn("Failed to create volume source directory %s: %v", source, err)
			}
		}

		mounts = append(mounts, mount.Mount{
			Type:     mountType,
			Source:   source,
			Target:   target,
			ReadOnly: vol.ReadOnly,
		})
	}

	return mounts
}

// GetModuleContainerIP gets the IP address of a module container on the discopanel network
func (c *Client) GetModuleContainerIP(ctx context.Context, containerID string) (string, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}

	// Try to get IP from the configured network
	if c.config.NetworkName != "" {
		if endpoint, ok := inspect.NetworkSettings.Networks[c.config.NetworkName]; ok {
			return endpoint.IPAddress, nil
		}
	}

	// Fallback to any available network
	for _, endpoint := range inspect.NetworkSettings.Networks {
		if endpoint.IPAddress != "" {
			return endpoint.IPAddress, nil
		}
	}

	return "", fmt.Errorf("no IP address found for container")
}

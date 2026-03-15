package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
	Source    string `json:"source"`
	Target    string `json:"target"`
	ReadOnly  bool   `json:"read_only,omitempty"`
	Type      string `json:"type,omitempty"`       // "bind" or "volume"
	CreateDir bool   `json:"create_dir,omitempty"` // Pre-create source dirs
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

		dockerProto := "tcp"
		if port.Protocol == "udp" {
			dockerProto = "udp"
		}

		natPort := nat.Port(fmt.Sprintf("%d/%s", port.ContainerPort, dockerProto))
		exposedPorts[natPort] = struct{}{}

		// Bind host port when proxy is not enabled for this port
		if port.HostPort > 0 && !port.ProxyEnabled {
			portBindings[natPort] = []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", port.HostPort)},
			}
		}

		c.log.Debug("Added port for module %s: %s (%d:%d/%s, proxy=%t)",
			module.ID, port.Name, port.HostPort, port.ContainerPort, port.Protocol, port.ProxyEnabled)
	}

	// Build mounts from module configuration only (frontend sends complete config)
	vols := c.parseModuleVolumes(module.VolumeOverrides, aliasCtx)

	// Pre-create bind mounts
	for _, vol := range vols {
		if vol.CreateDir && !vol.ReadOnly && (vol.Type == "" || vol.Type == "bind") {
			if _, err := os.Stat(vol.Source); os.IsNotExist(err) {
				if err := os.MkdirAll(vol.Source, 0755); err != nil {
					c.log.Warn("Failed to pre-create mount directory %s: %v", vol.Source, err)
				}
			}
		}
	}

	mounts := c.moduleVolumesToMounts(vols)

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

	// Set uid + gid
	uid, gid := alias.Substitute(module.UID, aliasCtx), alias.Substitute(module.GID, aliasCtx)
	if uid != "" || gid != "" {
		config.User = fmt.Sprintf("%s:%s", uid, gid)
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

	// Add module API token if available
	if module.TokenPlaintext != "" {
		env = append(env, fmt.Sprintf("DISCOPANEL_API_TOKEN=%s", module.TokenPlaintext))
	}

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

// JSON volume configuration and substitutes
func (c *Client) parseModuleVolumes(volumeJSON string, aliasCtx *alias.Context) []ModuleVolumeMount {
	if volumeJSON == "" || volumeJSON == "[]" {
		return nil
	}

	var volumes []ModuleVolumeMount
	if err := json.Unmarshal([]byte(volumeJSON), &volumes); err != nil {
		c.log.Warn("Failed to parse volume configuration: %v", err)
		return nil
	}

	// Sub aliases in paths
	for i := range volumes {
		volumes[i].Source = alias.Substitute(volumes[i].Source, aliasCtx)
		volumes[i].Target = alias.Substitute(volumes[i].Target, aliasCtx)
	}

	return volumes
}

// Module volumes to Docker mount specs
func (c *Client) moduleVolumesToMounts(volumes []ModuleVolumeMount) []mount.Mount {
	var mounts []mount.Mount

	for _, vol := range volumes {
		mountType := mount.TypeBind
		if vol.Type == "volume" {
			mountType = mount.TypeVolume
		}

		// Skip mounts with empty source or target
		if vol.Source == "" || vol.Target == "" {
			c.log.Warn("Skipping volume mount with empty source or target: source=%q, target=%q", vol.Source, vol.Target)
			continue
		}

		// Translate bind mount sources to host paths when running in a container
		source := vol.Source
		if mountType == mount.TypeBind {
			source = TranslateToHostPath(source)
		}

		mounts = append(mounts, mount.Mount{
			Type:        mountType,
			Source:      source,
			Target:      vol.Target,
			ReadOnly:    vol.ReadOnly,
			BindOptions: &mount.BindOptions{CreateMountpoint: !vol.ReadOnly},
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

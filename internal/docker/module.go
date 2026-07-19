package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	shellparse "github.com/arkady-emelyanov/go-shellparse"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/nickheyer/discopanel/internal/alias"
	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/config"
	"github.com/nickheyer/discopanel/pkg/files"
)

// Represents a volume mount from module configuration
type ModuleVolumeMount struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	ReadOnly  bool   `json:"read_only,omitempty"`
	Type      string `json:"type,omitempty"`       // "bind" or "volume"
	CreateDir bool   `json:"create_dir,omitempty"` // Pre-create source dirs
}

// Creates a module container, optionally given sibling modules by name
func (c *Client) CreateModuleContainer(ctx context.Context, module *models.Module, template *models.ModuleTemplate, server *models.Server, serverConfig *models.ServerProperties, cfg *config.Config, siblingModules ...map[string]*models.Module) (string, error) {
	// Determine the Docker image to use
	imageName := template.DockerImage
	if imageName == "" {
		return "", fmt.Errorf("module template has no Docker image configured")
	}

	if err := c.ensureImage(ctx, imageName, nil); err != nil {
		return "", err
	}

	// Build alias context for substitution (needed for env and volumes)
	aliasCtx := &alias.Context{
		Server:           server,
		ServerProperties: serverConfig,
		Module:           module,
		Config:           cfg,
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

		// Binds host port only when proxy is disabled
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
	if server != nil {
		resolveWorldSources(vols, server.DataPath)
	}

	// Pre-create bind sources, read-only ones must exist before create
	for _, vol := range vols {
		if vol.Type != "" && vol.Type != "bind" {
			continue
		}
		if !vol.CreateDir && !vol.ReadOnly {
			continue
		}
		if _, err := os.Stat(vol.Source); os.IsNotExist(err) {
			if err := os.MkdirAll(vol.Source, 0755); err != nil {
				c.log.Warn("Failed to pre-create mount directory %s: %v", vol.Source, err)
			}
		}
	}

	mounts := c.moduleVolumesToMounts(vols)

	siblings := map[string]*models.Module{}
	if len(siblingModules) > 0 && siblingModules[0] != nil {
		siblings = siblingModules[0]
	}
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
			LabelModuleConfigHash:           c.DesiredModuleConfigHash(module, template, server, serverConfig, cfg, siblings),
		},
	}

	// Set uid + gid
	uid, gid := alias.Substitute(module.UID, aliasCtx), alias.Substitute(module.GID, aliasCtx)
	if uid != "" || gid != "" {
		config.User = fmt.Sprintf("%s:%s", uid, gid)
	}

	// Module command override takes precedence over template default
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

// Builds environment variables for a module container
func (c *Client) buildModuleEnv(module *models.Module, server *models.Server, aliasCtx *alias.Context) []string {
	env := make([]string, 0)

	// Add DiscoPanel context variables, global modules have no server
	if server != nil {
		env = append(env,
			fmt.Sprintf("DISCOPANEL_SERVER_ID=%s", server.ID),
			fmt.Sprintf("DISCOPANEL_SERVER_NAME=%s", server.Name),
			fmt.Sprintf("DISCOPANEL_SERVER_HOST=discopanel-server-%s", server.ID),
			fmt.Sprintf("DISCOPANEL_SERVER_PORT=%d", server.InContainerPort()),
		)
	}
	env = append(env,
		fmt.Sprintf("DISCOPANEL_MODULE_ID=%s", module.ID),
		fmt.Sprintf("DISCOPANEL_MODULE_NAME=%s", module.Name),
	)

	// Add module API token if available
	if module.TokenPlaintext != "" {
		env = append(env, fmt.Sprintf("DISCOPANEL_API_TOKEN=%s", module.TokenPlaintext))
	}

	// Adds env vars sorted for a stable config hash
	if module.EnvOverrides != "" {
		var envOverrides map[string]string
		if err := json.Unmarshal([]byte(module.EnvOverrides), &envOverrides); err == nil {
			for _, key := range slices.Sorted(maps.Keys(envOverrides)) {
				resolvedValue := alias.Substitute(envOverrides[key], aliasCtx)
				env = append(env, fmt.Sprintf("%s=%s", key, resolvedValue))
			}
		}
	}

	return env
}

// Repoints default world binds at the server's real world dir
func resolveWorldSources(vols []ModuleVolumeMount, dataPath string) {
	worldDir, err := files.FindWorldDir(dataPath)
	if err != nil || worldDir == "" {
		return
	}
	declared := filepath.Join(dataPath, "world")
	if filepath.Clean(worldDir) == declared {
		return
	}
	for i := range vols {
		if vols[i].Type != "" && vols[i].Type != "bind" {
			continue
		}
		src := filepath.Clean(vols[i].Source)
		if src == declared {
			vols[i].Source = worldDir
		} else if strings.HasPrefix(src, declared+string(filepath.Separator)) {
			vols[i].Source = filepath.Join(worldDir, src[len(declared):])
		}
	}
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

		// Translates bind mount sources to host paths in-container
		source := vol.Source
		m := mount.Mount{
			Type:     mountType,
			Source:   source,
			Target:   vol.Target,
			ReadOnly: vol.ReadOnly,
		}
		if mountType == mount.TypeBind {
			m.Source = TranslateToHostPath(source)
			m.BindOptions = &mount.BindOptions{CreateMountpoint: true}
		}

		mounts = append(mounts, m)
	}

	return mounts
}

// Gets a module container's IP on the discopanel network
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

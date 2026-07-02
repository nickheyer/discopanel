package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

const (
	// Default Minecraft server port inside containers
	DefaultMinecraftPort = 25565

	// Default RCON port inside containers
	DefaultRCONPort = 25575

	// Offset added to game port for RCON host binding
	RCONPortOffset = 10

	// DefaultStopTimeoutSeconds is the graceful-stop window when a server has
	// no explicit stop duration configured.
	DefaultStopTimeoutSeconds = 60
)

type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	CPUCount    int     `json:"cpu_count"`
	MemoryUsage float64 `json:"memory_usage"` // in MB
	MemoryLimit float64 `json:"memory_limit"` // in MB
}

// Converts a container-internal path to a host path.
// When DISCOPANEL_HOST_DATA_PATH is not set (running on host), it returns the path unchanged.
func TranslateToHostPath(path string) string {
	hostDataPath := os.Getenv("DISCOPANEL_HOST_DATA_PATH")
	if hostDataPath == "" {
		return path
	}
	containerDataDir := os.Getenv("DISCOPANEL_DATA_DIR")
	if containerDataDir == "" {
		containerDataDir = "/app/data"
	}
	relPath, err := filepath.Rel(containerDataDir, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		// Path is not under the container data dir, return as-is
		return path
	}
	return filepath.Join(hostDataPath, relPath)
}

// HealthState is the panel-side health verdict for a running container,
// derived from Server List Ping results (replacing in-container healthchecks).
type HealthState int

const (
	HealthUnknown HealthState = iota
	HealthStarting
	HealthHealthy
	HealthUnhealthy
)

// HealthChecker reports panel-side health for running containers.
type HealthChecker interface {
	ContainerHealth(containerID string, startedAt time.Time) HealthState
}

type ClientConfig struct {
	APIVersion   string
	NetworkName  string
	RegistryURL  string
	RuntimeImage string
	DNS          string
	Labels       map[string]string
}

type Client struct {
	docker        *client.Client
	config        ClientConfig
	healthChecker HealthChecker
	log           *logger.Logger

	// Background image refresh bookkeeping (see ensureImage).
	refreshMu      sync.Mutex
	imageRefreshed map[string]time.Time
}

// SetHealthChecker registers the panel-side health source consulted by
// GetContainerStatus for running containers.
func (c *Client) SetHealthChecker(hc HealthChecker) {
	c.healthChecker = hc
}

func NewClient(host string, log *logger.Logger, config ...ClientConfig) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	// Apply API version if provided
	if len(config) > 0 && config[0].APIVersion != "" {
		opts = append(opts, client.WithVersion(config[0].APIVersion))
	}

	if host != "" && host != "unix:///var/run/docker.sock" {
		opts = append(opts, client.WithHost(host))
	}

	docker, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	c := &Client{docker: docker, log: log, imageRefreshed: make(map[string]time.Time)}
	if len(config) > 0 {
		c.config = config[0]
	} else {
		// Set defaults
		c.config = ClientConfig{
			NetworkName: "discopanel-network",
		}
	}

	return c, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}

// Get the docker client instance from the client object
func (c *Client) GetDockerClient() *client.Client {
	return c.docker
}

// ApplyOverrides applies DockerOverrides to container and host configs
func ApplyOverrides(overrides *v1.DockerOverrides, config *container.Config, hostConfig *container.HostConfig) {
	if overrides == nil {
		return
	}

	// Apply environment variable overrides
	if len(overrides.GetEnvironment()) > 0 {
		for key, value := range overrides.GetEnvironment() {
			config.Env = append(config.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// Apply additional volume mounts
	for _, vol := range overrides.GetVolumes() {
		mountType := mount.Type(vol.GetType())
		if mountType == "" {
			mountType = mount.TypeBind
		}
		hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
			Type:     mountType,
			Source:   vol.GetSource(),
			Target:   vol.GetTarget(),
			ReadOnly: vol.GetReadOnly(),
		})
	}

	// Apply restart policy override
	if overrides.GetRestartPolicy() != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: container.RestartPolicyMode(overrides.GetRestartPolicy()),
		}
	}

	// Apply resource limits
	if overrides.GetCpuLimit() > 0 {
		hostConfig.Resources.NanoCPUs = int64(overrides.GetCpuLimit() * 1e9)
	}
	if overrides.GetMemoryLimit() > 0 {
		hostConfig.Resources.Memory = overrides.GetMemoryLimit() * 1024 * 1024
		hostConfig.Resources.MemorySwap = overrides.GetMemoryLimit() * 1024 * 1024
	}

	// Apply additional labels
	if len(overrides.GetLabels()) > 0 {
		maps.Copy(config.Labels, overrides.GetLabels())
	}

	// Apply capabilities
	if len(overrides.GetCapAdd()) > 0 {
		hostConfig.CapAdd = overrides.GetCapAdd()
	}
	if len(overrides.GetCapDrop()) > 0 {
		hostConfig.CapDrop = overrides.GetCapDrop()
	}

	// Apply devices
	for _, device := range overrides.GetDevices() {
		parts := strings.Split(device, ":")
		if len(parts) >= 2 {
			hostConfig.Devices = append(hostConfig.Devices, container.DeviceMapping{
				PathOnHost:        parts[0],
				PathInContainer:   parts[1],
				CgroupPermissions: "rwm",
			})
		}
	}

	// Apply extra hosts
	if len(overrides.GetExtraHosts()) > 0 {
		hostConfig.ExtraHosts = overrides.GetExtraHosts()
	}

	// Apply security settings
	hostConfig.Privileged = overrides.GetPrivileged()
	hostConfig.ReadonlyRootfs = overrides.GetReadOnly()
	if len(overrides.GetSecurityOpt()) > 0 {
		hostConfig.SecurityOpt = overrides.GetSecurityOpt()
	}

	// Apply SHM size
	if overrides.GetShmSize() > 0 {
		hostConfig.ShmSize = overrides.GetShmSize()
	}

	// Apply user
	if overrides.GetUser() != "" {
		config.User = overrides.GetUser()
	}

	// Apply working directory
	if overrides.GetWorkingDir() != "" {
		config.WorkingDir = overrides.GetWorkingDir()
	}

	// Apply entrypoint
	if len(overrides.GetEntrypoint()) > 0 {
		config.Entrypoint = overrides.GetEntrypoint()
	}

	// Apply command
	if len(overrides.GetCommand()) > 0 {
		config.Cmd = overrides.GetCommand()
	}

	// Apply network mode override
	if overrides.GetNetworkMode() != "" {
		hostConfig.NetworkMode = container.NetworkMode(overrides.GetNetworkMode())
	}

	// Apply DNS override
	if len(overrides.GetDns()) > 0 {
		hostConfig.DNS = overrides.GetDns()
	}
}

// CreateContainer creates the server container. Setup steps (image pull
// progress in particular) are reported through progress, which may be nil.
func (c *Client) CreateContainer(ctx context.Context, server *models.Server, serverConfig *models.ServerConfig, progress func(string)) (string, error) {
	imageName := c.DesiredImage(server)

	if err := c.ensureImage(ctx, imageName, progress); err != nil {
		return "", err
	}

	// Build environment variables
	env := buildEnvFromConfig(serverConfig)

	// Determine container port - proxy servers always use default port internally
	// (the provisioner writes the matching server-port into server.properties)
	useProxy := server.ProxyHostname != ""
	containerPort := server.Port
	if useProxy {
		containerPort = DefaultMinecraftPort
	}

	c.log.Debug("Creating container for server %s with image %s", server.ID, imageName)

	// Build exposed ports
	exposedPorts := nat.PortSet{
		nat.Port(fmt.Sprintf("%d/tcp", containerPort)):   struct{}{},
		nat.Port(fmt.Sprintf("%d/tcp", DefaultRCONPort)): struct{}{},
	}
	for _, port := range server.AdditionalPorts {
		protocol := port.GetProtocol()
		if protocol == "" {
			protocol = "tcp"
		}
		exposedPorts[nat.Port(fmt.Sprintf("%d/%s", port.GetContainerPort(), protocol))] = struct{}{}
	}

	// Build port bindings
	portBindings := nat.PortMap{}
	if !useProxy {
		// Bind game port to host
		portBindings[nat.Port(fmt.Sprintf("%d/tcp", containerPort))] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", server.Port)},
		}
		// Bind RCON to localhost only
		portBindings[nat.Port(fmt.Sprintf("%d/tcp", DefaultRCONPort))] = []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: fmt.Sprintf("%d", server.Port+RCONPortOffset)},
		}
	}
	// Add additional port bindings
	for _, port := range server.AdditionalPorts {
		protocol := port.GetProtocol()
		if protocol == "" {
			protocol = "tcp"
		}
		portKey := nat.Port(fmt.Sprintf("%d/%s", port.GetContainerPort(), protocol))
		portBindings[portKey] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: fmt.Sprintf("%d", port.GetHostPort())},
		}
		c.log.Debug("Additional port mapping: %s (%d:%d/%s)", port.GetName(), port.GetHostPort(), port.GetContainerPort(), protocol)
	}

	// Handle path translation when DiscoPanel runs in a container
	dataPath := TranslateToHostPath(server.DataPath)

	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create server data directory: %w", err)
	}

	config := &container.Config{
		Image:        imageName,
		Env:          env,
		Tty:          true,
		AttachStdout: true,
		AttachStderr: true,
		ExposedPorts: exposedPorts,
		Labels: map[string]string{
			"discopanel.server.id":      server.ID,
			"discopanel.server.name":    server.Name,
			"discopanel.server.loader":  string(server.ModLoader),
			"discopanel.server.version": server.MCVersion,
			"discopanel.managed":        "true",
		},
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: dataPath, Target: "/data", BindOptions: &mount.BindOptions{CreateMountpoint: true}},
		},
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Resources: container.Resources{
			Memory:     int64(server.Memory) * 1024 * 1024,
			MemorySwap: int64(server.Memory) * 1024 * 1024,
		},
		LogConfig: container.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
	}

	// Apply global DNS from config
	if c.config.DNS != "" {
		hostConfig.DNS = []string{c.config.DNS}
	}

	// Apply global labels from config
	if c.config.Labels != nil {
		maps.Copy(config.Labels, c.config.Labels)
	}

	// Apply docker overrides
	ApplyOverrides(server.DockerOverrides, config, hostConfig)

	// Network configuration
	networkConfig := &network.NetworkingConfig{}
	if c.config.NetworkName != "" && hostConfig.NetworkMode == "" {
		networkConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			c.config.NetworkName: {},
		}
	}

	resp, err := c.docker.ContainerCreate(
		ctx, config, hostConfig, networkConfig, nil,
		fmt.Sprintf("discopanel-server-%s", server.ID),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return resp.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	return c.docker.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer stops a container, allowing timeoutSeconds for a graceful
// shutdown (SIGTERM saves the world) before force-killing. Returns
// (containerFound, error); (false, nil) lets callers clean stale references.
func (c *Client) StopContainer(ctx context.Context, containerID string, timeoutSeconds int) (bool, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultStopTimeoutSeconds
	}
	err := c.docker.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeoutSeconds,
	})

	if err != nil {
		// If container non-existent on stop
		if errdefs.IsNotFound(err) {
			c.log.Debug("Container %s not found, treating as already stopped", containerID)
			return false, nil
		}
		// If graceful stop fails, force kill the container
		c.log.Warn("Graceful stop failed for container %s: %v, attempting force kill", containerID, err)
		killErr := c.docker.ContainerKill(ctx, containerID, "KILL")
		if killErr != nil {
			// If container non-existent on kill
			if errdefs.IsNotFound(killErr) {
				return false, nil
			}
			return false, fmt.Errorf("failed to stop container: graceful stop error: %v, force kill error: %v", err, killErr)
		}
	}

	return true, nil
}

func (c *Client) RemoveContainer(ctx context.Context, containerID string) error {
	return c.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

// Stops and starts a container with an optional delay between operations
func (c *Client) RestartContainer(ctx context.Context, containerID string, delay time.Duration) error {
	if _, err := c.StopContainer(ctx, containerID, DefaultStopTimeoutSeconds); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	if err := c.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// Result of a container recreation operation
type RecreateContainerResult struct {
	NewContainerID string
	WasRunning     bool
}

// Stops, removes, and creates a new container - Returns new container ID and whether it was running before
func (c *Client) RecreateContainer(ctx context.Context, oldContainerID string, server *models.Server, serverConfig *models.ServerConfig, progress func(string)) (*RecreateContainerResult, error) {
	result := &RecreateContainerResult{}

	// Check if container was running before we stop it
	if oldContainerID != "" {
		status, err := c.GetContainerStatus(ctx, oldContainerID)
		if err != nil {
			// Container may not exist, that's ok - continue with creation
			c.log.Debug("Container %s not found during recreation: %v", oldContainerID, err)
		} else if status == models.StatusRunning || status == models.StatusUnhealthy {
			result.WasRunning = true
			if _, err := c.StopContainer(ctx, oldContainerID, DefaultStopTimeoutSeconds); err != nil {
				return nil, fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove old container
		if err := c.RemoveContainer(ctx, oldContainerID); err != nil {
			// Log but continue - container may already be removed
			c.log.Debug("Could not remove old container (may not exist): %v", err)
		}
	}

	// Create new container
	newContainerID, err := c.CreateContainer(ctx, server, serverConfig, progress)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}
	result.NewContainerID = newContainerID

	// Start if it was running before
	if result.WasRunning {
		if err := c.StartContainer(ctx, newContainerID); err != nil {
			return result, fmt.Errorf("failed to start new container: %w", err)
		}
	}

	return result, nil
}

func (c *Client) GetContainerStatus(ctx context.Context, containerID string) (models.ServerStatus, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return models.StatusError, err
	}

	switch inspect.State.Status {
	case "running":
		// Health comes from the panel-side SLP checker, not the container.
		if c.healthChecker != nil {
			startedAt, _ := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
			switch c.healthChecker.ContainerHealth(containerID, startedAt) {
			case HealthHealthy:
				return models.StatusRunning, nil
			case HealthStarting:
				return models.StatusStarting, nil
			case HealthUnhealthy:
				return models.StatusUnhealthy, nil
			}
		}
		return models.StatusRunning, nil
	case "paused":
		return models.StatusPaused, nil
	case "restarting":
		return models.StatusStarting, nil
	case "exited", "dead":
		return models.StatusStopped, nil
	case "created", "removing":
		return models.StatusStopped, nil
	default:
		return models.StatusError, nil
	}
}

// PauseContainer freezes all processes in a container (autopause).
func (c *Client) PauseContainer(ctx context.Context, containerID string) error {
	return c.docker.ContainerPause(ctx, containerID)
}

// UnpauseContainer resumes a paused container (wake-on-connect).
func (c *Client) UnpauseContainer(ctx context.Context, containerID string) error {
	return c.docker.ContainerUnpause(ctx, containerID)
}

// IsContainerPaused reports whether a container is currently paused.
func (c *Client) IsContainerPaused(ctx context.Context, containerID string) (bool, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}
	return inspect.State.Status == "paused", nil
}

// ContainerImage returns the image reference a container was created from.
func (c *Client) ContainerImage(ctx context.Context, containerID string) (string, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", err
	}
	return inspect.Config.Image, nil
}

// ContainerRunInfo is the raw run state used by the panel-side health tracker.
type ContainerRunInfo struct {
	Running   bool
	Paused    bool
	StartedAt time.Time
}

// GetContainerRunInfo returns the raw container run state without health
// interpretation (the health tracker itself must not consult health).
func (c *Client) GetContainerRunInfo(ctx context.Context, containerID string) (*ContainerRunInfo, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}
	startedAt, _ := time.Parse(time.RFC3339Nano, inspect.State.StartedAt)
	return &ContainerRunInfo{
		Running:   inspect.State.Status == "running",
		Paused:    inspect.State.Status == "paused",
		StartedAt: startedAt,
	}, nil
}

func (c *Client) GetContainerStats(ctx context.Context, containerID string) (*ContainerStats, error) {
	// Get real-time stats
	statsResponse, err := c.docker.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer statsResponse.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(statsResponse.Body).Decode(&stats); err != nil {
		return nil, err
	}

	// Calculate CPU percentage (ns)
	cpuPercent := 0.0
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)

	// Number of CPU cores
	cpuCount := float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	if cpuCount == 0 {
		cpuCount = float64(stats.CPUStats.OnlineCPUs)
	}
	if cpuCount == 0 {
		cpuCount = 1.0
	}

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * cpuCount * 100.0
	}

	// Get memory usage in MB (excluding cache)
	memoryUsage := float64(stats.MemoryStats.Usage-stats.MemoryStats.Stats["cache"]) / 1024 / 1024
	memoryLimit := float64(stats.MemoryStats.Limit) / 1024 / 1024

	return &ContainerStats{
		CPUPercent:  cpuPercent,
		CPUCount:    int(cpuCount),
		MemoryUsage: memoryUsage,
		MemoryLimit: memoryLimit,
	}, nil
}

// Runs shell command, script, or executable inside the container and returns the output
func (c *Client) Exec(ctx context.Context, containerID string, execCmd []string) (string, error) {
	// Create exec configuration
	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Cmd:          execCmd,
	}

	// Create exec instance
	execResp, err := c.docker.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to exec instance
	attachResp, err := c.docker.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	// Read output using stdcopy to demultiplex the stream
	var outputBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&outputBuf, &outputBuf, attachResp.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check exec exit code
	inspectResp, err := c.docker.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return "", fmt.Errorf("command failed with exit code %d: %s", inspectResp.ExitCode, outputBuf.String())
	}

	return outputBuf.String(), nil
}

// ensureImage makes imageName available locally, pulling it only when absent
// so starts never block on the registry. A present image is refreshed in the
// background (at most once per hour) to pick up updated runtime tags. Pull
// progress is reported through progress (which may be nil) as throttled
// human-readable lines.
func (c *Client) ensureImage(ctx context.Context, imageName string, progress func(string)) error {
	if _, err := c.docker.ImageInspect(ctx, imageName); err == nil {
		c.refreshImageAsync(imageName)
		return nil
	}

	if progress != nil {
		progress(fmt.Sprintf("pulling image %s...", imageName))
	}
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Aggregate per-layer progress into one throttled line: docker reports a
	// JSON message per layer event, far too noisy for a server console. Only
	// Downloading events count - each layer also emits Extracting progress
	// that restarts at zero and would make the line run backwards.
	type layerState struct{ current, total int64 }
	layers := map[string]*layerState{}
	lastReport := time.Now()
	dec := json.NewDecoder(reader)
	for {
		var msg struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to complete image pull for %s: %w", imageName, err)
		}
		if msg.Error != "" {
			return fmt.Errorf("failed to pull image %s: %s", imageName, msg.Error)
		}
		if msg.ID != "" && msg.Status == "Downloading" && msg.ProgressDetail.Total > 0 {
			ls := layers[msg.ID]
			if ls == nil {
				ls = &layerState{}
				layers[msg.ID] = ls
			}
			ls.current = msg.ProgressDetail.Current
			ls.total = msg.ProgressDetail.Total
		}
		if progress != nil && time.Since(lastReport) >= 2*time.Second && len(layers) > 0 {
			var current, total int64
			for _, ls := range layers {
				current += ls.current
				total += ls.total
			}
			progress(fmt.Sprintf("pulling image %s: %.1f/%.1f MB",
				imageName, float64(current)/1024/1024, float64(total)/1024/1024))
			lastReport = time.Now()
		}
	}

	if progress != nil {
		progress(fmt.Sprintf("image %s ready", imageName))
	}
	return nil
}

// refreshImageAsync re-pulls an already-present image in the background so
// updated runtime tags are picked up without ever blocking a server start.
func (c *Client) refreshImageAsync(imageName string) {
	c.refreshMu.Lock()
	if time.Since(c.imageRefreshed[imageName]) < time.Hour {
		c.refreshMu.Unlock()
		return
	}
	c.imageRefreshed[imageName] = time.Now()
	c.refreshMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
		if err != nil {
			c.log.Debug("Background refresh of image %s failed: %v", imageName, err)
			return
		}
		defer reader.Close()
		if _, err := io.Copy(io.Discard, reader); err != nil {
			c.log.Debug("Background refresh of image %s interrupted: %v", imageName, err)
		}
	}()
}

// ContainerIP resolves a container's IP address on the panel network (falling
// back to any attached network) using the shared docker client.
func (c *Client) ContainerIP(ctx context.Context, containerID string) (string, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}
	if c.config.NetworkName != "" {
		if network, ok := inspect.NetworkSettings.Networks[c.config.NetworkName]; ok && network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}
	for _, network := range inspect.NetworkSettings.Networks {
		if network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}
	return "", fmt.Errorf("no IP address found for container")
}

// Creates the Docker network if it doesn't exist - attaches itself to that network when applicable
func (c *Client) EnsureNetwork() error {
	ctx := context.Background()

	// List existing networks
	networks, err := c.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Check if network already exists
	exists := false
	for _, net := range networks {
		if net.Name == c.config.NetworkName {
			exists = true
			break
		}
	}

	if !exists {
		// Create network - let Docker allocate subnet from its configured default-address-pools
		createOpts := network.CreateOptions{
			Driver: "bridge",
			Labels: map[string]string{
				"discopanel.managed": "true",
			},
		}

		if _, err = c.docker.NetworkCreate(ctx, c.config.NetworkName, createOpts); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
	}

	c.attachSelfToNetwork(ctx)
	return nil
}

// Connects discopanel to its own bridge network if running as container
// NOTE: Only really needed for bridge mode though
func (c *Client) attachSelfToNetwork(ctx context.Context) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		return
	}

	// Docker sets the container hostname to its short ID by default
	info, err := c.docker.ContainerInspect(ctx, hostname)
	if err != nil {
		c.log.Debug("Could not inspect own container %s: %v", hostname, err)
		return
	}

	if info.HostConfig != nil && info.HostConfig.NetworkMode.IsHost() {
		return
	}

	if _, ok := info.NetworkSettings.Networks[c.config.NetworkName]; ok {
		return
	}

	if err := c.docker.NetworkConnect(ctx, c.config.NetworkName, info.ID, nil); err != nil {
		c.log.Error("Failed to attach DiscoPanel container to network %s: %v", c.config.NetworkName, err)
		return
	}

	c.log.Info("Attached DiscoPanel container to network %s", c.config.NetworkName)
}

// Builds Docker environment variables from ServerConfig struct
func buildEnvFromConfig(config *models.ServerConfig) []string {
	var env []string

	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		envTag := field.Tag.Get("env")

		// Skip fields without env tags
		if envTag == "" || envTag == "-" {
			continue
		}

		fieldValue := configValue.Field(i)

		// Handle pointer types
		if fieldValue.Kind() == reflect.Pointer {
			// Skip if nil
			if fieldValue.IsNil() {
				continue
			}
			// Dereference the pointer
			fieldValue = fieldValue.Elem()
		}

		// Handle different field types
		switch fieldValue.Kind() {
		case reflect.String:
			if str := fieldValue.String(); str != "" {
				env = append(env, fmt.Sprintf("%s=%s", envTag, str))
			}
		case reflect.Int, reflect.Int32, reflect.Int64:
			// Always include int values (even 0) when the field is explicitly set
			env = append(env, fmt.Sprintf("%s=%d", envTag, fieldValue.Int()))
		case reflect.Bool:
			// Always include bool values when the field is explicitly set
			env = append(env, fmt.Sprintf("%s=%v", envTag, fieldValue.Bool()))
		}
	}

	return env
}

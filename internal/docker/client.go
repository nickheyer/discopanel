package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

// AdditionalPort represents a single additional port configuration
type AdditionalPort struct {
	Name          string `json:"name"`           // User-friendly name for the port (e.g., "BlueMap Web")
	ContainerPort int    `json:"container_port"` // Port inside the container
	HostPort      int    `json:"host_port"`      // Port on the host machine
	Protocol      string `json:"protocol"`       // Protocol: "tcp" or "udp" (defaults to "tcp" if empty)
}

// DockerOverrides represents user-defined docker container overrides
type DockerOverrides struct {
	Environment    map[string]string `json:"environment,omitempty"`     // Additional environment variables
	Volumes        []VolumeMount     `json:"volumes,omitempty"`         // Additional volume mounts
	NetworkMode    string            `json:"network_mode,omitempty"`    // Override network mode
	RestartPolicy  string            `json:"restart_policy,omitempty"`  // Override restart policy
	CPULimit       float64           `json:"cpu_limit,omitempty"`       // CPU limit (e.g., 1.5 for 1.5 cores)
	MemoryOverride int64             `json:"memory_override,omitempty"` // Override memory limit in MB
	Labels         map[string]string `json:"labels,omitempty"`          // Additional labels
	CapAdd         []string          `json:"cap_add,omitempty"`         // Linux capabilities to add
	CapDrop        []string          `json:"cap_drop,omitempty"`        // Linux capabilities to drop
	Devices        []string          `json:"devices,omitempty"`         // Device mappings (e.g., "/dev/ttyUSB0:/dev/ttyUSB0")
	ExtraHosts     []string          `json:"extra_hosts,omitempty"`     // Extra entries for /etc/hosts
	Privileged     bool              `json:"privileged,omitempty"`      // Run container in privileged mode
	ReadOnly       bool              `json:"read_only,omitempty"`       // Mount root filesystem as read-only
	SecurityOpt    []string          `json:"security_opt,omitempty"`    // Security options
	ShmSize        int64             `json:"shm_size,omitempty"`        // Size of /dev/shm in bytes
	User           string            `json:"user,omitempty"`            // User to run commands as
	WorkingDir     string            `json:"working_dir,omitempty"`     // Working directory inside container
	Entrypoint     []string          `json:"entrypoint,omitempty"`      // Override default entrypoint
	Command        []string          `json:"command,omitempty"`         // Override default command
}

// VolumeMount represents a volume mount configuration
type VolumeMount struct {
	Source   string `json:"source"`              // Host path or volume name
	Target   string `json:"target"`              // Container path
	ReadOnly bool   `json:"read_only,omitempty"` // Mount as read-only
	Type     string `json:"type,omitempty"`      // Mount type: "bind" or "volume" (defaults to "bind")
}

const (
	// Docker images manifest URL from itzg/docker-minecraft-server repo
	dockerImagesURL = "https://raw.githubusercontent.com/itzg/docker-minecraft-server/refs/heads/master/images.json"

	// Cache for 1 hour
	dockerImagesCacheDuration = time.Hour
)

type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsage float64 `json:"memory_usage"` // in MB
	MemoryLimit float64 `json:"memory_limit"` // in MB
}

type DockerImageTag struct {
	Tag           string   `json:"tag"`           // Docker tag name (e.g., "latest", "java21", etc.)
	Java          string   `json:"java"`          // Java version number
	Distribution  string   `json:"distribution"`  // Linux distribution (ubuntu, alpine, oracle)
	JVM           string   `json:"jvm"`           // JVM type (hotspot, graalvm)
	Architectures []string `json:"architectures"` // Supported architectures
	Deprecated    bool     `json:"deprecated"`    // Whether this tag is deprecated
	LTS           bool     `json:"lts"`           // Whether this is an LTS version
	JDK           bool     `json:"jdk"`           // Whether this includes JDK
	Notes         string   `json:"notes"`         // Additional notes about the tag
}

// Cached docker images data
type dockerImagesCache struct {
	mu            sync.RWMutex
	images        []DockerImageTag
	lastFetchTime time.Time
}

var dockerCache = &dockerImagesCache{}

// Fetches the docker images manifest from itzg
func fetchDockerImages() ([]DockerImageTag, error) {
	// Check cache first
	dockerCache.mu.RLock()
	if len(dockerCache.images) > 0 && time.Since(dockerCache.lastFetchTime) < dockerImagesCacheDuration {
		images := dockerCache.images
		dockerCache.mu.RUnlock()
		return images, nil
	}
	dockerCache.mu.RUnlock()

	// Fetch new manifest
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(dockerImagesURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch docker images manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch docker images manifest: status code %d", resp.StatusCode)
	}

	var images []DockerImageTag
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, fmt.Errorf("failed to decode docker images manifest: %w", err)
	}

	// Update cache
	dockerCache.mu.Lock()
	dockerCache.images = images
	dockerCache.lastFetchTime = time.Now()
	dockerCache.mu.Unlock()

	return images, nil
}

// Gets ideal docker tag for a given Minecraft version + mod loader
func GetOptimalDockerTag(mcVersion string, modLoader models.ModLoader, preferGraalVM bool) string {
	javaVersion := GetRequiredJavaVersion(mcVersion, modLoader)
	if javaVersion == "0" || javaVersion == "" {
		// Could not determine Java version, use stable
		return "stable"
	}

	// Fetch Docker images from API
	images, err := fetchDockerImages()
	if err != nil {
		// Could not fetch Docker images, use stable
		return "stable"
	}

	// Find matching tag
	for _, tag := range images {
		if tag.Java == javaVersion && !tag.Deprecated {
			if preferGraalVM && strings.Contains(tag.Tag, "graalvm") {
				return tag.Tag
			}
			// Return first matching non-special tag (not graalvm, alpine, or jdk)
			if !strings.Contains(tag.Tag, "graalvm") && !strings.Contains(tag.Tag, "alpine") && !strings.Contains(tag.Tag, "jdk") {
				return tag.Tag
			}
		}
	}

	// No matching tag found, construct one
	return fmt.Sprintf("java%s", javaVersion)
}

// Gets required Java version for a Minecraft version
func GetRequiredJavaVersion(mcVersion string, modLoader models.ModLoader) string {
	// Fetch the Java version from the Minecraft version metadata
	javaVersion, err := minecraft.GetJavaVersion(mcVersion)
	if err != nil {
		// If we can't determine the Java version, return 0 to indicate error
		return "0"
	}
	return javaVersion
}

type ClientConfig struct {
	APIVersion  string
	NetworkName string
	RegistryURL string
}

type Client struct {
	docker *client.Client
	config ClientConfig
}

func NewClient(host string, config ...ClientConfig) (*Client, error) {
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

	c := &Client{docker: docker}
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

func (c *Client) CreateContainer(ctx context.Context, server *models.Server, serverConfig *models.ServerConfig) (string, error) {
	// Use server's DockerImage if specified, otherwise determine based on version and loader
	var imageName string
	if server.DockerImage != "" {
		imageName = "itzg/minecraft-server:" + server.DockerImage
	} else {
		imageName = getDockerImage(server.ModLoader, server.MCVersion)
	}

	// Try pulling latest
	if err := c.pullImage(ctx, imageName); err != nil {
		return "", fmt.Errorf("failed to pull image: %w", err)
	}

	// Build environment variables from ServerConfig
	env := buildEnvFromConfig(serverConfig)

	// Override SERVER_PORT when using proxy - all servers should use 25565 internally
	if server.ProxyHostname != "" {
		// Remove any existing SERVER_PORT env var
		newEnv := []string{}
		for _, e := range env {
			if !strings.HasPrefix(e, "SERVER_PORT=") {
				newEnv = append(newEnv, e)
			}
		}
		// Add SERVER_PORT=25565
		newEnv = append(newEnv, "SERVER_PORT=25565")
		env = newEnv
	}

	// Log the environment variables for debugging
	for _, e := range env {
		fmt.Printf("Docker env: %s\n", e)
	}

	// Container configuration
	// Determine which port Minecraft will actually use inside the container
	minecraftPort := server.Port
	if server.ProxyHostname != "" {
		minecraftPort = 25565 // Proxy servers always use 25565 internally
	}

	// Parse additional ports
	var additionalPorts []AdditionalPort
	if server.AdditionalPorts != "" {
		if err := json.Unmarshal([]byte(server.AdditionalPorts), &additionalPorts); err != nil {
			fmt.Printf("Warning: Failed to parse additional ports: %v\n", err)
			additionalPorts = []AdditionalPort{}
		}
	}

	// Build exposed ports including additional ports
	exposedPorts := nat.PortSet{
		nat.Port(fmt.Sprintf("%d/tcp", minecraftPort)): struct{}{},
		"25575/tcp": struct{}{}, // RCON port
	}

	// Add additional ports to exposed ports
	for _, port := range additionalPorts {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp" // Default to TCP if not specified
		}
		portKey := nat.Port(fmt.Sprintf("%d/%s", port.ContainerPort, protocol))
		exposedPorts[portKey] = struct{}{}
	}

	config := &container.Config{
		Image:        imageName,
		Env:          env,
		Tty:          true,
		OpenStdin:    false,
		AttachStdin:  false,
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

	// For RCON, use a dynamic port allocation or no host binding when using proxy
	// This prevents conflicts when multiple servers are running
	var rconPortBinding []nat.PortBinding
	if server.ProxyHostname == "" {
		// Only bind RCON to a specific port if not using proxy
		rconPort := server.Port + 10
		rconPortBinding = []nat.PortBinding{
			{
				HostIP:   "127.0.0.1",
				HostPort: fmt.Sprintf("%d", rconPort),
			},
		}
	} else {
		// When using proxy, let Docker assign a random port or don't bind at all
		// The container can still be accessed via Docker network for RCON
		rconPortBinding = []nat.PortBinding{}
	}

	// Host configuration
	// If server has a proxy hostname configured, don't bind the game port to avoid conflicts
	portBindings := nat.PortMap{}

	// Only bind the game port if not using proxy (no proxy_hostname set)
	if server.ProxyHostname == "" {
		portBindings[nat.Port(fmt.Sprintf("%d/tcp", minecraftPort))] = []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: fmt.Sprintf("%d", server.Port),
			},
		}
	}

	// Set RCON port binding based on proxy configuration
	if len(rconPortBinding) > 0 {
		portBindings["25575/tcp"] = rconPortBinding
	}

	// Add additional port bindings
	for _, port := range additionalPorts {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		portKey := nat.Port(fmt.Sprintf("%d/%s", port.ContainerPort, protocol))
		portBindings[portKey] = []nat.PortBinding{
			{
				HostIP:   "0.0.0.0",
				HostPort: fmt.Sprintf("%d", port.HostPort),
			},
		}
		fmt.Printf("Adding additional port: %s - %d:%d/%s\n", port.Name, port.HostPort, port.ContainerPort, protocol)
	}

	// Handle path translation when DiscoPanel is running in a container
	dataPath := server.DataPath
	if hostDataPath := os.Getenv("DISCOPANEL_HOST_DATA_PATH"); hostDataPath != "" {
		// When running in Docker, translate container path to host path
		// Example: /app/data/servers/creative -> ./data/servers/creative
		containerDataDir := os.Getenv("DISCOPANEL_DATA_DIR")
		if containerDataDir == "" {
			containerDataDir = "/app/data"
		}

		// Replace the container path prefix with the host path
		relPath, err := filepath.Rel(containerDataDir, server.DataPath)
		if err == nil {
			dataPath = filepath.Join(hostDataPath, relPath)
		}
	}

	// Ensure the directory exists
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create server data directory: %w", err)
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: dataPath,
				Target: "/data",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
		Resources: container.Resources{
			Memory:     int64(server.Memory) * 1024 * 1024,
			MemorySwap: int64(server.Memory) * 1024 * 1024,
		},
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "3",
			},
		},
	}

	// Parse and apply docker overrides if present
	if server.DockerOverrides != "" {
		var overrides DockerOverrides
		if err := json.Unmarshal([]byte(server.DockerOverrides), &overrides); err != nil {
			fmt.Printf("Warning: Failed to parse docker overrides: %v\n", err)
		} else {
			// Apply environment variable overrides
			if len(overrides.Environment) > 0 {
				for key, value := range overrides.Environment {
					// Add or override environment variables
					env = append(env, fmt.Sprintf("%s=%s", key, value))
				}
				config.Env = env
			}

			// Apply additional volume mounts
			if len(overrides.Volumes) > 0 {
				for _, vol := range overrides.Volumes {
					mountType := mount.Type(vol.Type)
					if mountType == "" {
						mountType = mount.TypeBind
					}
					hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
						Type:     mountType,
						Source:   vol.Source,
						Target:   vol.Target,
						ReadOnly: vol.ReadOnly,
					})
				}
			}

			// Apply restart policy override
			if overrides.RestartPolicy != "" {
				hostConfig.RestartPolicy = container.RestartPolicy{
					Name: container.RestartPolicyMode(overrides.RestartPolicy),
				}
			}

			// Apply resource limits
			if overrides.CPULimit > 0 {
				hostConfig.Resources.NanoCPUs = int64(overrides.CPULimit * 1e9) // Convert cores to nanocpus
			}
			if overrides.MemoryOverride > 0 {
				hostConfig.Resources.Memory = overrides.MemoryOverride * 1024 * 1024
				hostConfig.Resources.MemorySwap = overrides.MemoryOverride * 1024 * 1024
			}

			// Apply additional labels
			if len(overrides.Labels) > 0 {
				maps.Copy(config.Labels, overrides.Labels)
			}

			// Apply capabilities
			if len(overrides.CapAdd) > 0 {
				hostConfig.CapAdd = overrides.CapAdd
			}
			if len(overrides.CapDrop) > 0 {
				hostConfig.CapDrop = overrides.CapDrop
			}

			// Apply devices
			if len(overrides.Devices) > 0 {
				hostConfig.Devices = []container.DeviceMapping{}
				for _, device := range overrides.Devices {
					parts := strings.Split(device, ":")
					if len(parts) >= 2 {
						hostConfig.Devices = append(hostConfig.Devices, container.DeviceMapping{
							PathOnHost:        parts[0],
							PathInContainer:   parts[1],
							CgroupPermissions: "rwm",
						})
					}
				}
			}

			// Apply extra hosts
			if len(overrides.ExtraHosts) > 0 {
				hostConfig.ExtraHosts = overrides.ExtraHosts
			}

			// Apply security settings
			hostConfig.Privileged = overrides.Privileged
			hostConfig.ReadonlyRootfs = overrides.ReadOnly
			if len(overrides.SecurityOpt) > 0 {
				hostConfig.SecurityOpt = overrides.SecurityOpt
			}

			// Apply SHM size
			if overrides.ShmSize > 0 {
				hostConfig.ShmSize = overrides.ShmSize
			}

			// Apply user
			if overrides.User != "" {
				config.User = overrides.User
			}

			// Apply working directory
			if overrides.WorkingDir != "" {
				config.WorkingDir = overrides.WorkingDir
			}

			// Apply entrypoint
			if len(overrides.Entrypoint) > 0 {
				config.Entrypoint = overrides.Entrypoint
			}

			// Apply command
			if len(overrides.Command) > 0 {
				config.Cmd = overrides.Command
			}

			// Apply network mode override
			if overrides.NetworkMode != "" {
				hostConfig.NetworkMode = container.NetworkMode(overrides.NetworkMode)
			}
		}
	}

	// Network configuration - use custom network if available
	networkConfig := &network.NetworkingConfig{}
	if c.config.NetworkName != "" && hostConfig.NetworkMode == "" {
		networkConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			c.config.NetworkName: {},
		}
	}

	// Create container
	resp, err := c.docker.ContainerCreate(
		ctx,
		config,
		hostConfig,
		networkConfig,
		nil,
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

func (c *Client) StopContainer(ctx context.Context, containerID string) error {
	// First try graceful stop with a short timeout
	timeout := 5 // seconds
	err := c.docker.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})

	if err != nil {
		// If graceful stop fails, force kill the container
		fmt.Printf("Graceful stop failed for container %s: %v, attempting force kill\n", containerID, err)
		killErr := c.docker.ContainerKill(ctx, containerID, "KILL")
		if killErr != nil {
			return fmt.Errorf("failed to stop container: graceful stop error: %v, force kill error: %v", err, killErr)
		}
	}

	return nil
}

func (c *Client) RemoveContainer(ctx context.Context, containerID string) error {
	return c.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	})
}

func (c *Client) GetContainerStatus(ctx context.Context, containerID string) (models.ServerStatus, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return models.StatusError, err
	}

	switch inspect.State.Status {
	case "running":
		// Check health status if available
		if inspect.State.Health != nil {
			switch inspect.State.Health.Status {
			case "healthy":
				return models.StatusRunning, nil
			case "starting":
				return models.StatusStarting, nil
			case "unhealthy":
				// Server process isn't responding
				return models.StatusUnhealthy, nil
			default:
				// No health status or unknown, assume running
				return models.StatusRunning, nil
			}
		}
		return models.StatusRunning, nil
	case "restarting":
		return models.StatusStarting, nil
	case "exited", "dead":
		return models.StatusStopped, nil
	case "created", "paused", "removing":
		return models.StatusStopped, nil
	default:
		return models.StatusError, nil
	}
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
		Cmd:          execCmd, //[]string{"rcon-cli", command},
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

// ExecCommand executes a command inside the container and returns the output
func (c *Client) ExecCommand(ctx context.Context, containerID string, command string) (string, error) {
	return c.Exec(ctx, containerID, []string{"rcon-cli", command})
}

func (c *Client) pullImage(ctx context.Context, imageName string) error {
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Read the output to ensure the pull completes
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("failed to complete image pull for %s: %w", imageName, err)
	}

	return nil
}

func (c *Client) GetDockerImages() []DockerImageTag {
	images, err := fetchDockerImages()
	if err != nil {
		fmt.Printf("Error: failed to fetch docker images: %v\n", err)
		return []DockerImageTag{}
	}

	// Filter out deprecated images
	var activeImages []DockerImageTag
	for _, img := range images {
		if !img.Deprecated {
			activeImages = append(activeImages, img)
		}
	}
	return activeImages
}

func getDockerImage(loader models.ModLoader, mcVersion string) string {
	_ = loader
	// itzg/minecraft-server supports all mod loaders through environment variables
	// We use Java version specific tags for better compatibility
	return "itzg/minecraft-server:" + GetOptimalDockerTag(mcVersion, loader, false)
}

// EnsureNetwork creates the Docker network if it doesn't exist
func (c *Client) EnsureNetwork() error {
	ctx := context.Background()

	// List existing networks
	networks, err := c.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Check if network already exists
	for _, net := range networks {
		if net.Name == c.config.NetworkName {
			return nil // Network already exists
		}
	}

	// Create network - let Docker allocate subnet from its configured default-address-pools
	createOpts := network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"discopanel.managed": "true",
		},
	}

	_, err = c.docker.NetworkCreate(ctx, c.config.NetworkName, createOpts)

	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	return nil
}

// buildEnvFromConfig builds Docker environment variables from ServerConfig struct
func buildEnvFromConfig(config *models.ServerConfig) []string {
	env := []string{
		"DUMP_SERVER_PROPERTIES=true",
	}

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

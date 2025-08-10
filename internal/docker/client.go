package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	models "github.com/nickheyer/discopanel/internal/db"
)

type DockerImageTag struct {
	Tag         string   `json:"tag"`         // Docker tag name (e.g., "latest", "java21", etc.)
	JavaVersion int      `json:"javaVersion"` // Java version number
	Linux       string   `json:"linux"`       // Linux distribution (Ubuntu, Alpine, Oracle)
	JVMType     string   `json:"jvmType"`     // JVM type (Hotspot, GraalVM, etc.)
	Archs       []string `json:"archs"`       // Supported architectures
	Deprecated  bool     `json:"deprecated"`  // Whether this tag is deprecated
	Note        string   `json:"note"`        // Additional notes about the tag
}

// MinecraftVersionRequirements maps Minecraft versions to required Java versions
type MinecraftVersionRequirements struct {
	MinecraftVersion string // Minecraft version or range
	RequiredJava     int    // Minimum required Java version
	MaxJava          int    // Maximum supported Java version (0 = no limit)
}

var (
	// DockerImageTags contains all available Docker image tags for itzg/minecraft-server
	DockerImageTags = []DockerImageTag{
		// Current/Latest tags
		{Tag: "latest", JavaVersion: 21, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64"}},
		{Tag: "stable", JavaVersion: 21, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64"}},

		// Java 24
		{Tag: "java24", JavaVersion: 24, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64"}, Note: "Short-term variant"},
		{Tag: "java24-graalvm", JavaVersion: 24, Linux: "Oracle", JVMType: "Oracle GraalVM", Archs: []string{"amd64", "arm64"}, Note: "Short-term variant"},

		// Java 21
		{Tag: "java21", JavaVersion: 21, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64"}},
		{Tag: "java21-jdk", JavaVersion: 21, Linux: "Ubuntu", JVMType: "Hotspot+JDK", Archs: []string{"amd64", "arm64"}},
		{Tag: "java21-alpine", JavaVersion: 21, Linux: "Alpine", JVMType: "Hotspot", Archs: []string{"amd64", "arm64"}},
		{Tag: "java21-graalvm", JavaVersion: 21, Linux: "Oracle", JVMType: "Oracle GraalVM", Archs: []string{"amd64", "arm64"}},

		// Java 17
		{Tag: "java17", JavaVersion: 17, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64", "armv7"}},
		{Tag: "java17-graalvm", JavaVersion: 17, Linux: "Oracle", JVMType: "Oracle GraalVM", Archs: []string{"amd64", "arm64"}},
		{Tag: "java17-alpine", JavaVersion: 17, Linux: "Alpine", JVMType: "Hotspot", Archs: []string{"amd64"}, Note: "No arm64 support"},

		// Java 16
		{Tag: "java16", JavaVersion: 16, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64", "armv7"}, Note: "Recommended for PaperMC 1.16.5"},

		// Java 11
		{Tag: "java11", JavaVersion: 11, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64", "armv7"}},

		// Java 8
		{Tag: "java8", JavaVersion: 8, Linux: "Ubuntu", JVMType: "Hotspot", Archs: []string{"amd64", "arm64", "armv7"}},
	}

	// MinecraftJavaRequirements maps Minecraft versions to Java requirements
	MinecraftJavaRequirements = []MinecraftVersionRequirements{
		// Minecraft 1.7-1.16 requires Java 8
		{MinecraftVersion: "1.7", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.8", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.9", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.10", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.11", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.12", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.13", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.14", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.15", RequiredJava: 8, MaxJava: 8},
		{MinecraftVersion: "1.16", RequiredJava: 8, MaxJava: 8},

		// Minecraft 1.17 requires Java 16
		{MinecraftVersion: "1.17", RequiredJava: 16, MaxJava: 16},

		// Minecraft 1.18-1.20.4 requires Java 17
		{MinecraftVersion: "1.18", RequiredJava: 17, MaxJava: 0},
		{MinecraftVersion: "1.19", RequiredJava: 17, MaxJava: 0},
		{MinecraftVersion: "1.20", RequiredJava: 17, MaxJava: 0},

		// Minecraft 1.21+ requires Java 21
		{MinecraftVersion: "1.21", RequiredJava: 21, MaxJava: 0},
		{MinecraftVersion: "1.22", RequiredJava: 21, MaxJava: 0},
	}

	// ForgeJavaCompatibility maps Forge/Minecraft versions to Java compatibility issues
	ForgeJavaCompatibility = map[string]string{
		"forge-1.17":  "Some mods require Java 17 and won't work with newer versions",
		"forge-<1.18": "Must use Java 8 (java8 tag)",
	}
)

// GetOptimalDockerTag returns the best Docker tag for a given Minecraft version and mod loader
func GetOptimalDockerTag(mcVersion string, modLoader models.ModLoader, preferGraalVM bool) string {
	javaVersion := GetRequiredJavaVersion(mcVersion, modLoader)

	// Handle Forge special cases
	if modLoader != models.ModLoaderVanilla {
		parts := strings.Split(mcVersion, ".")
		if len(parts) >= 2 {
			minor, _ := strconv.Atoi(parts[1])
			if minor < 18 {
				return "java8" // Forge < 1.18 requires Java 8
			}
			if minor < 21 && javaVersion > 17 {
				// Some Forge mods up to 1.21 may need Java 17
				return "java17"
			}
		}
	}

	// Find matching tag
	for _, tag := range DockerImageTags {
		if tag.JavaVersion == javaVersion && !tag.Deprecated {
			if preferGraalVM && strings.Contains(tag.Tag, "graalvm") {
				return tag.Tag
			}
			// Return first matching non-GraalVM tag
			if !strings.Contains(tag.Tag, "graalvm") && !strings.Contains(tag.Tag, "alpine") && !strings.Contains(tag.Tag, "jdk") {
				return tag.Tag
			}
		}
	}

	// Fallback to java version tag
	return fmt.Sprintf("java%d", javaVersion)
}

// GetRequiredJavaVersion returns the required Java version for a Minecraft version
func GetRequiredJavaVersion(mcVersion string, modLoader models.ModLoader) int {
	// Parse major.minor version
	parts := strings.Split(mcVersion, ".")
	if len(parts) < 2 {
		return 21 // Default to Java 21
	}

	// Find matching requirement
	versionPrefix := parts[0] + "." + parts[1]
	for _, req := range MinecraftJavaRequirements {
		if strings.HasPrefix(versionPrefix, req.MinecraftVersion) {
			if modLoader != models.ModLoaderVanilla && req.MaxJava > 0 {
				return req.MaxJava
			}
			return req.RequiredJava
		}
	}

	// Parse minor version for fallback logic
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 21
	}

	// Fallback based on minor version
	switch {
	case minor <= 16:
		return 8
	case minor == 17:
		return 16
	case minor <= 20:
		return 17
	default:
		return 21
	}
}

type ClientConfig struct {
	APIVersion    string
	NetworkName   string
	NetworkSubnet string
	RegistryURL   string
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
			NetworkName:   "discopanel-network",
			NetworkSubnet: "172.20.0.0/16",
		}
	}

	return c, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}

func (c *Client) CreateContainer(ctx context.Context, server *models.Server, serverConfig *models.ServerConfig) (string, error) {
	// Use server's DockerImage if specified, otherwise determine based on version and loader
	var imageName string
	if server.DockerImage != "" {
		imageName = "itzg/minecraft-server:" + server.DockerImage
	} else {
		imageName = getDockerImage(server.ModLoader, server.MCVersion)
	}

	// Pull image if not exists
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

	config := &container.Config{
		Image: imageName,
		Env:   env,
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", minecraftPort)): struct{}{},
			"25575/tcp": struct{}{}, // RCON port
		},
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
			Memory:     int64(server.Memory) * 1024 * 1024, // Convert MB to bytes
			MemorySwap: int64(server.Memory) * 1024 * 1024, // Prevent swap usage
		},
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "3",
			},
		},
	}

	// Network configuration - use custom network if available
	networkConfig := &network.NetworkingConfig{}
	if c.config.NetworkName != "" {
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
	timeout := 30 // seconds
	return c.docker.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})
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
		return models.StatusRunning, nil
	case "restarting":
		return models.StatusStarting, nil
	case "created", "paused", "removing", "exited", "dead":
		return models.StatusStopped, nil
	default:
		return models.StatusError, nil
	}
}

func (c *Client) GetContainerLogs(ctx context.Context, containerID string, tail int) (string, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       fmt.Sprintf("%d", tail),
		Timestamps: true,
	}

	reader, err := c.docker.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	// Docker multiplexes stdout and stderr, we need to demultiplex
	var buf bytes.Buffer
	_, err = stdcopy.StdCopy(&buf, &buf, reader)
	if err != nil {
		return "", err
	}

	// Filter out RCON spam
	lines := strings.Split(buf.String(), "\n")
	var filtered []string
	for _, line := range lines {
		if strings.Contains(line, "Thread RCON Client") && (strings.Contains(line, "started") || strings.Contains(line, "shutting down")) {
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n"), nil
}

// ExecCommand executes a command inside the container and returns the output
func (c *Client) ExecCommand(ctx context.Context, containerID string, command string) (string, error) {
	// Create exec configuration
	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Cmd:          []string{"rcon-cli", command},
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

func (c *Client) pullImage(ctx context.Context, imageName string) error {
	reader, err := c.docker.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Read the output to ensure the pull completes
	_, err = io.Copy(io.Discard, reader)
	return err
}

func (c *Client) GetDockerImages() []DockerImageTag {
	return DockerImageTags
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

	// Create network with subnet configuration
	ipamConfig := &network.IPAMConfig{}
	if c.config.NetworkSubnet != "" {
		ipamConfig.Subnet = c.config.NetworkSubnet
	}

	_, err = c.docker.NetworkCreate(ctx, c.config.NetworkName, network.CreateOptions{
		Driver: "bridge",
		IPAM: &network.IPAM{
			Config: []network.IPAMConfig{*ipamConfig},
		},
		Labels: map[string]string{
			"discopanel.managed": "true",
		},
	})

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
		if fieldValue.Kind() == reflect.Ptr {
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

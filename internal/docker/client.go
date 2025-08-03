package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

func (c *Client) CreateContainer(ctx context.Context, server *models.Server) (string, error) {
	// Determine Java version based on Minecraft version
	javaVersion := getJavaVersion(server.MCVersion)

	// Build Docker image name based on mod loader
	imageName := getDockerImage(server.ModLoader, server.MCVersion, javaVersion)

	// Pull image if not exists
	if err := c.pullImage(ctx, imageName); err != nil {
		return "", fmt.Errorf("failed to pull image: %w", err)
	}

	// Build environment variables based on mod loader
	env := []string{
		"EULA=TRUE",
		fmt.Sprintf("VERSION=%s", server.MCVersion),
		fmt.Sprintf("MEMORY=%dM", server.Memory),
		fmt.Sprintf("MAX_MEMORY=%dM", server.Memory),
		fmt.Sprintf("JVM_OPTS=-Xms%dM -Xmx%dM", server.Memory, server.Memory),
		fmt.Sprintf("MAX_PLAYERS=%d", server.MaxPlayers),
		fmt.Sprintf("SERVER_NAME=%s", server.Name),
		"ENABLE_RCON=true",
		fmt.Sprintf("RCON_PASSWORD=discopanel_%s", server.ID[:8]),
		fmt.Sprintf("RCON_PORT=%d", 25575),
	}

	// Add mod loader specific environment variables
	switch server.ModLoader {
	case models.ModLoaderVanilla:
		env = append(env, "TYPE=VANILLA")
	case models.ModLoaderForge:
		env = append(env, "TYPE=FORGE")
	case models.ModLoaderFabric:
		env = append(env, "TYPE=FABRIC")
	case models.ModLoaderNeoForge:
		env = append(env, "TYPE=NEOFORGE")
	case models.ModLoaderPaper:
		env = append(env, "TYPE=PAPER")
	case models.ModLoaderSpigot:
		env = append(env, "TYPE=SPIGOT")
	default:
		env = append(env, "TYPE=VANILLA")
	}

	// Container configuration
	config := &container.Config{
		Image: imageName,
		Env:   env,
		ExposedPorts: nat.PortSet{
			"25565/tcp": struct{}{},
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

	// Calculate RCON port based on base port + 10
	rconPort := server.Port + 10

	// Host configuration
	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"25565/tcp": []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: fmt.Sprintf("%d", server.Port),
				},
			},
			"25575/tcp": []nat.PortBinding{
				{
					HostIP:   "127.0.0.1", // Only bind RCON to localhost for security
					HostPort: fmt.Sprintf("%d", rconPort),
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: server.DataPath,
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

func getJavaVersion(mcVersion string) string {
	// Parse major.minor version
	parts := strings.Split(mcVersion, ".")
	if len(parts) < 2 {
		return "17" // Default to Java 17
	}

	major := parts[1]
	if len(major) > 0 {
		switch major {
		case "7", "8", "9", "10", "11", "12", "13", "14", "15", "16":
			return "8"
		case "17":
			return "16"
		case "18", "19":
			return "17"
		case "20":
			return "17"
		default:
			return "21" // Latest versions use Java 21
		}
	}

	return "17"
}

func getDockerImage(loader models.ModLoader, mcVersion, javaVersion string) string {
	// itzg/minecraft-server supports all mod loaders through environment variables
	// We use Java version specific tags for better compatibility
	return "itzg/minecraft-server:java" + javaVersion
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

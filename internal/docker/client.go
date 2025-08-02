package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/nickheyer/discopanel/internal/models"
)

type Client struct {
	docker *client.Client
}

func NewClient(host string) (*Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}

	if host != "" && host != "unix:///var/run/docker.sock" {
		opts = append(opts, client.WithHost(host))
	}

	docker, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Client{docker: docker}, nil
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

	// Container configuration
	config := &container.Config{
		Image: imageName,
		Env: []string{
			"EULA=TRUE",
			fmt.Sprintf("TYPE=%s", strings.ToUpper(string(server.ModLoader))),
			fmt.Sprintf("VERSION=%s", server.MCVersion),
			fmt.Sprintf("MEMORY=%dM", server.Memory),
			fmt.Sprintf("MAX_PLAYERS=%d", server.MaxPlayers),
			fmt.Sprintf("SERVER_NAME=%s", server.Name),
		},
		ExposedPorts: nat.PortSet{
			"25565/tcp": struct{}{},
		},
		Labels: map[string]string{
			"discopanel.server.id":   server.ID,
			"discopanel.server.name": server.Name,
			"discopanel.managed":     "true",
		},
	}

	// Host configuration
	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"25565/tcp": []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: fmt.Sprintf("%d", server.Port),
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
			Memory: int64(server.Memory) * 1024 * 1024, // Convert MB to bytes
		},
	}

	// Network configuration
	networkConfig := &network.NetworkingConfig{}

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
	case "created", "restarting":
		return models.StatusStarting, nil
	case "paused", "removing", "exited", "dead":
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

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
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
	_ = loader
	_ = mcVersion
	// Using itzg/minecraft-server as the base image
	// It supports various mod loaders and Minecraft versions
	return "itzg/minecraft-server:java" + javaVersion
}

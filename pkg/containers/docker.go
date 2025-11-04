package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	log "github.com/sirupsen/logrus"
)

type DockerProvider struct {
	client *client.Client
}

func (d DockerProvider) Create(ctx context.Context, cfg *ContainerConfig) (string, error) {
	dockerEnv := make([]string, 0, len(cfg.env))
	for k, v := range cfg.env {
		dockerEnv = append(dockerEnv, fmt.Sprintf("%s=%s", k, v))
	}

	_, _, err := d.client.ImageInspectWithRaw(ctx, cfg.image)
	if cfg.pullConfig != nil && cfg.pullConfig(cfg.image, err == nil) {
		log.WithField("image", cfg.image).Info("pulling...")
		reader, err := d.client.ImagePull(ctx, cfg.image, image.PullOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to pull image: %w", err)
		}
		defer reader.Close()

		_, err = io.ReadAll(reader)
		if err != nil {
			return "", fmt.Errorf("failed to pull image: %w", err)
		}
		log.WithField("image", cfg.image).Infof("pull complete")
	}

	mounts := make([]mount.Mount, len(cfg.mounts))
	for i, b := range cfg.mounts {
		mounts[i] = mount.Mount{
			Type:     mount.TypeBind,
			Source:   b.HostPath,
			Target:   b.ContainerPath,
			ReadOnly: b.ReadOnly,
		}
	}

	config := &container.Config{
		Image:        cfg.image,
		Env:          dockerEnv,
		Cmd:          cfg.command,
		Tty:          true,
		OpenStdin:    false,
		AttachStdin:  false,
		AttachStdout: true,
		AttachStderr: true,
		ExposedPorts: cfg.exposedPorts,
		Labels:       cfg.labels,
	}

	servers := make([]string, len(cfg.dnsServers))
	for i, server := range cfg.dnsServers {
		servers[i] = server.String()
	}

	extraHosts := make([]string, len(cfg.extraHosts))
	for i := range extraHosts {
		extraHosts[i] = fmt.Sprintf("%s:%s", cfg.extraHosts[i].HostName, cfg.extraHosts[i].IP)
	}

	// Disables SELinux label confinement
	// Otherwise, systems using it might have permission issues with bind mounts
	securityOpt := []string{"label=disable"}

	hostConfig := &container.HostConfig{
		NetworkMode: "host",
		Mounts:      mounts,
		DNS:         servers,
		DNSSearch:   cfg.dnsSearchDomains,
		ExtraHosts:  extraHosts,
		SecurityOpt: securityOpt,
	}

	resp, err := d.client.ContainerCreate(ctx, config, hostConfig, nil, nil, cfg.name)
	if err != nil {
		return "", fmt.Errorf("failed to create docker container: %w", err)
	}
	return resp.ID, nil
}

func (d DockerProvider) Remove(ctx context.Context, containerID string) error {
	return d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{})
}

func (d DockerProvider) Start(ctx context.Context, containerID string) error {
	return d.client.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (d DockerProvider) Stop(ctx context.Context, containerID string, timeout *time.Duration) error {
	var t *int
	if timeout != nil {
		intTimeout := int(timeout.Seconds())
		t = &intTimeout
	}
	return d.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: t})
}

func (d DockerProvider) Wait(ctx context.Context, containerID string) (<-chan int64, <-chan error) {
	msgChan := make(chan int64)
	errChan := make(chan error)
	message, err := d.client.ContainerWait(ctx, containerID, container.WaitConditionNextExit)

	go func() {
		defer close(msgChan)
		defer close(errChan)

		select {
		case <-ctx.Done():
			errChan <- ctx.Err()
		case e := <-err:
			errChan <- e
		case msg := <-message:
			if msg.Error != nil {
				errChan <- fmt.Errorf("error waiting on container end: %s", msg.Error.Message)
				return
			}
			msgChan <- msg.StatusCode
		}
	}()

	return msgChan, errChan
}

func (d DockerProvider) Logs(ctx context.Context, containerID string) (io.ReadCloser, error) {
	// Log streaming config
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "100", // Start with last 100 lines
	}

	return d.client.ContainerLogs(ctx, containerID, options)
}

func (d DockerProvider) CopyFrom(ctx context.Context, container, source, dest string) error {
	readTar, _, err := d.client.CopyFromContainer(ctx, container, source)
	if err != nil {
		return fmt.Errorf("failed to copy files from container: %w", err)
	}

	tar := exec.CommandContext(ctx, "tar", "-x", "-C", dest, "-f", "-")

	tar.Stdin = readTar

	tar.Stdout = os.Stdout
	tar.Stderr = os.Stderr

	err = tar.Run()
	if err != nil {
		return fmt.Errorf("failed to extract tar archive: %w", err)
	}
	return nil
}

func (d DockerProvider) GetContainerStatus(ctx context.Context, containerID string) (string, error) {
	inspect, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return "error", err
	}
	return inspect.State.Status, nil
}

func (d DockerProvider) GetContainerStats(ctx context.Context, containerID string) (*ContainerStats, error) {
	statsResponse, err := d.client.ContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, err
	}
	defer statsResponse.Body.Close()

	var stats container.Stats
	if err := json.NewDecoder(statsResponse.Body).Decode(&stats); err != nil {
		return nil, err
	}

	// Calculate CPU percentage
	cpuPercent := 0.0
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)

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

func (d DockerProvider) CleanupOrphanedContainers(ctx context.Context, trackedIDs map[string]bool) error {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return err
	}

	for _, cont := range containers {
		// Check for discopanel prefix
		hasPrefix := false
		for _, name := range cont.Names {
			if len(name) > 0 && len(name) > 18 && name[:18] == "/discopanel-server" {
				hasPrefix = true
				break
			}
		}

		if hasPrefix && !trackedIDs[cont.ID] {
			// Stop if running
			if cont.State == "running" {
				timeout := 30
				d.client.ContainerStop(ctx, cont.ID, container.StopOptions{Timeout: &timeout})
			}
			// Remove container
			d.client.ContainerRemove(ctx, cont.ID, container.RemoveOptions{Force: true})
		}
	}
	return nil
}

func (d DockerProvider) EnsureNetwork(ctx context.Context, networkName string) error {
	// List existing networks
	networks, err := d.client.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list networks: %w", err)
	}

	// Check if network already exists
	for _, net := range networks {
		if net.Name == networkName {
			return nil // Network already exists
		}
	}

	// Create network
	_, err = d.client.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"discopanel.managed": "true",
		},
	})

	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	return nil
}

func (d DockerProvider) Exec(ctx context.Context, containerID string, cmd []string) (string, error) {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	var outputBuf bytes.Buffer
	_, err = stdcopy.StdCopy(&outputBuf, &outputBuf, attachResp.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check exec status
	inspectResp, err := d.client.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return outputBuf.String(), fmt.Errorf("command exited with code %d", inspectResp.ExitCode)
	}

	return outputBuf.String(), nil
}

func (d DockerProvider) GetIP(ctx context.Context, containerID string, networkName string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containerInfo, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// Look for the IP on the specified network
	if networkName != "" {
		if network, ok := containerInfo.NetworkSettings.Networks[networkName]; ok && network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}

	// Fallback to any available IP
	for _, network := range containerInfo.NetworkSettings.Networks {
		if network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}

	return "", fmt.Errorf("no IP address found for container")
}

func (d DockerProvider) Command() string {
	return "docker"
}

func (d DockerProvider) Close() error {
	return d.client.Close()
}

func NewDockerProvider(ctx context.Context) (ContainerProvider, error) {
	apiclient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("could not connect to Docker: %w", err)
	}

	_, err = apiclient.ServerVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("connection check failed: %w", err)
	}

	return &DockerProvider{
		client: apiclient,
	}, nil
}

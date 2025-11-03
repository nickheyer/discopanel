package containers

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
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

	timeout := 0
	config := &container.Config{
		Image: cfg.image,
		Env:   dockerEnv,
		Cmd:   cfg.command,
		// In case we pass a 0 value to the stop API, this timeout will be used.
		// Setting this to 0 means: send SIGKILL immediately
		StopTimeout: &timeout,
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

func (d DockerProvider) Logs(ctx context.Context, containerID string) (io.ReadCloser, io.ReadCloser, error) {
	options := container.LogsOptions{
		Follow:     true,
		ShowStdout: true,
		ShowStderr: true,
	}
	combined, err := d.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get logs from docker: %w", err)
	}

	readOut, writeOut, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	readErr, writeErr, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	go func() {
		defer writeOut.Close()
		defer writeErr.Close()
		defer combined.Close()

		_, err := stdcopy.StdCopy(writeOut, writeErr, combined)
		if err != nil {
			log.WithField("err", err).Warn("failed to copy logs content to pipes")
		}
	}()

	return readOut, readErr, nil
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

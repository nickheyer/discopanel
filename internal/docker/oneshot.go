package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/pkg/stdcopy"
)

// OneShotOptions configures a single foreground command run in a container
// (used by the provisioner for loader installers that need Java).
type OneShotOptions struct {
	Image      string
	Cmd        []string
	DataPath   string // host path mounted at /data
	WorkingDir string
	User       string // "uid:gid"
	Name       string
	Labels     map[string]string
}

// RunOneShot pulls the image if needed, runs the command to completion with
// the data dir mounted at /data, forwards output lines to logFn, and removes
// the container. A non-zero exit code is returned as an error.
func (c *Client) RunOneShot(ctx context.Context, opts OneShotOptions, logFn func(line string)) error {
	if err := c.pullImage(ctx, opts.Image); err != nil {
		return fmt.Errorf("failed to pull image %s: %w", opts.Image, err)
	}

	// Remove any stale container from a previous interrupted run.
	if opts.Name != "" {
		_ = c.docker.ContainerRemove(ctx, opts.Name, container.RemoveOptions{Force: true})
	}

	config := &container.Config{
		Image:      opts.Image,
		Entrypoint: opts.Cmd,
		WorkingDir: opts.WorkingDir,
		User:       opts.User,
		Labels:     opts.Labels,
	}
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:        mount.TypeBind,
				Source:      TranslateToHostPath(opts.DataPath),
				Target:      "/data",
				BindOptions: &mount.BindOptions{CreateMountpoint: true},
			},
		},
		AutoRemove: false, // removed explicitly after log collection
	}
	if c.config.DNS != "" {
		hostConfig.DNS = []string{c.config.DNS}
	}

	resp, err := c.docker.ContainerCreate(ctx, config, hostConfig, nil, nil, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to create installer container: %w", err)
	}
	containerID := resp.ID
	defer c.docker.ContainerRemove(context.WithoutCancel(ctx), containerID, container.RemoveOptions{Force: true})

	if err := c.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start installer container: %w", err)
	}

	// Stream output while the command runs.
	logsDone := make(chan struct{})
	var lastLines []string
	go func() {
		defer close(logsDone)
		reader, err := c.docker.ContainerLogs(ctx, containerID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if err != nil {
			return
		}
		defer reader.Close()

		lw := &lineWriter{fn: func(line string) {
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				return
			}
			lastLines = append(lastLines, line)
			if len(lastLines) > 20 {
				lastLines = lastLines[1:]
			}
			if logFn != nil {
				logFn(line)
			}
		}}
		defer lw.Close()
		_, _ = stdcopy.StdCopy(lw, lw, reader)
	}()

	statusCh, errCh := c.docker.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("installer container wait failed: %w", err)
		}
	case status := <-statusCh:
		<-logsDone
		if status.StatusCode != 0 {
			tail := strings.Join(lastLines, "\n")
			return fmt.Errorf("installer exited with code %d:\n%s", status.StatusCode, tail)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// lineWriter invokes fn once per full line written to it.
type lineWriter struct {
	fn  func(string)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		w.fn(string(w.buf[:idx]))
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}

func (w *lineWriter) Close() error {
	if len(w.buf) > 0 {
		w.fn(string(w.buf))
		w.buf = nil
	}
	return nil
}

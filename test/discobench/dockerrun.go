package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// Matches the Done line every server flavor prints
var readyPattern = regexp.MustCompile(`Done \([0-9.,]+ ?n?s(?:econds)?\)`)

type dockerRunner struct {
	cli *client.Client
}

func newDockerRunner() (*dockerRunner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &dockerRunner{cli: cli}, nil
}

// Pulls image when missing, reports seconds and size
func (d *dockerRunner) ensureImage(ctx context.Context, ref string) (pullSeconds float64, sizeBytes int64, err error) {
	inspect, ierr := d.cli.ImageInspect(ctx, ref)
	if ierr == nil {
		return 0, inspect.Size, nil
	}
	start := time.Now()
	rc, err := d.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("pulling %s: %w", ref, err)
	}
	_, _ = io.Copy(io.Discard, rc)
	rc.Close()
	pullSeconds = time.Since(start).Seconds()
	inspect, ierr = d.cli.ImageInspect(ctx, ref)
	if ierr != nil {
		return pullSeconds, 0, ierr
	}
	return pullSeconds, inspect.Size, nil
}

// One running server under measurement
type benchContainer struct {
	d         *dockerRunner
	id        string
	name      string
	hostPort  string
	startedAt time.Time
}

// Builds server container with fairness limits
func (d *dockerRunner) createContainer(ctx context.Context, name, img, dataDir string, env []string, cfg *Config) (*benchContainer, error) {
	port := nat.Port("25565/tcp")
	conf := &container.Config{
		Image:        img,
		Env:          env,
		ExposedPorts: nat.PortSet{port: struct{}{}},
		Labels:       map[string]string{"discobench": "true"},
	}
	host := &container.HostConfig{
		PortBindings: nat.PortMap{port: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}}},
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: dataDir, Target: "/data", BindOptions: &mount.BindOptions{CreateMountpoint: true}},
		},
		Resources: container.Resources{
			Memory:     int64(cfg.MemoryMB) * 1024 * 1024,
			MemorySwap: int64(cfg.MemoryMB) * 1024 * 1024,
		},
	}
	if cfg.CPUs > 0 {
		host.Resources.NanoCPUs = int64(cfg.CPUs * 1e9)
	}
	resp, err := d.cli.ContainerCreate(ctx, conf, host, nil, nil, name)
	if err != nil {
		return nil, err
	}
	return &benchContainer{d: d, id: resp.ID, name: name}, nil
}

// Runs container and resolves published game port
func (b *benchContainer) start(ctx context.Context) error {
	b.startedAt = time.Now()
	if err := b.d.cli.ContainerStart(ctx, b.id, container.StartOptions{}); err != nil {
		return err
	}
	inspect, err := b.d.cli.ContainerInspect(ctx, b.id)
	if err != nil {
		return err
	}
	bindings := inspect.NetworkSettings.Ports[nat.Port("25565/tcp")]
	if len(bindings) == 0 {
		return fmt.Errorf("container %s has no published game port", b.name)
	}
	b.hostPort = bindings[0].HostPort
	return nil
}

func (b *benchContainer) addr() string {
	return "127.0.0.1:" + b.hostPort
}

// Follows logs until Done line, returns elapsed seconds
func (b *benchContainer) waitDone(ctx context.Context, since time.Time, timeout time.Duration) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rc, err := b.d.cli.ContainerLogs(ctx, b.id, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
		Since: since.Format(time.RFC3339Nano),
	})
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	inspect, err := b.d.cli.ContainerInspect(ctx, b.id)
	if err != nil {
		return 0, err
	}
	var reader io.Reader = rc
	if !inspect.Config.Tty {
		pr, pw := io.Pipe()
		go func() {
			_, err := stdcopy.StdCopy(pw, pw, rc)
			pw.CloseWithError(err)
		}()
		reader = pr
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if readyPattern.MatchString(scanner.Text()) {
			return time.Since(since).Seconds(), nil
		}
	}
	if ctx.Err() != nil {
		return 0, fmt.Errorf("no Done line within %s", timeout)
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("log stream ended before the Done line (server died?)")
}

// One docker stats reading
type statSample struct {
	CPUPercent float64
	RSSMB      float64
}

// Streams docker stats into channel until ctx ends
func (b *benchContainer) sampleStats(ctx context.Context) <-chan statSample {
	out := make(chan statSample, 64)
	go func() {
		defer close(out)
		resp, err := b.d.cli.ContainerStats(ctx, b.id, true)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		dec := json.NewDecoder(resp.Body)
		var prev *container.StatsResponse
		for {
			var s container.StatsResponse
			if err := dec.Decode(&s); err != nil {
				return
			}
			sample := statSample{RSSMB: rssMB(&s)}
			if prev != nil {
				cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - prev.CPUStats.CPUUsage.TotalUsage)
				sysDelta := float64(s.CPUStats.SystemUsage - prev.CPUStats.SystemUsage)
				if sysDelta > 0 && cpuDelta >= 0 {
					sample.CPUPercent = cpuDelta / sysDelta * float64(s.CPUStats.OnlineCPUs) * 100
				}
				select {
				case out <- sample:
				case <-ctx.Done():
					return
				}
			}
			prevCopy := s
			prev = &prevCopy
		}
	}()
	return out
}

// Approximates resident memory minus reclaimable file cache
func rssMB(s *container.StatsResponse) float64 {
	usage := float64(s.MemoryStats.Usage)
	if inactive, ok := s.MemoryStats.Stats["inactive_file"]; ok {
		usage -= float64(inactive)
	}
	return usage / 1024 / 1024
}

// Stops container gracefully, returns seconds to exit
func (b *benchContainer) stopTimed(ctx context.Context, timeout time.Duration) (float64, int, error) {
	secs := int(timeout.Seconds())
	start := time.Now()
	if err := b.d.cli.ContainerStop(ctx, b.id, container.StopOptions{Timeout: &secs}); err != nil {
		return 0, 0, err
	}
	waitCh, errCh := b.d.cli.ContainerWait(ctx, b.id, container.WaitConditionNotRunning)
	select {
	case res := <-waitCh:
		return time.Since(start).Seconds(), int(res.StatusCode), nil
	case err := <-errCh:
		return time.Since(start).Seconds(), 0, err
	}
}

func (b *benchContainer) remove(ctx context.Context) {
	_ = b.d.cli.ContainerRemove(ctx, b.id, container.RemoveOptions{Force: true})
}

// Polls server list until first successful response
func pingUntilUp(ctx context.Context, addr string, since time.Time, timeout time.Duration) (float64, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if _, _, err := slpPing(addr, 2*time.Second); err == nil {
			return time.Since(since).Seconds(), nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("no ping response within %s", timeout)
}

// Builds collision safe bench container name
func containerName(parts ...string) string {
	name := "discobench-" + strings.Join(parts, "-")
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_' || r == '.':
			return r
		}
		return '-'
	}, name)
	return name
}

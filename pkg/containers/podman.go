package containers

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/client"
)

type PodmanProvider struct {
	DockerProvider
}

func (p PodmanProvider) Command() string {
	return "podman"
}

func NewPodmanProvider(ctx context.Context) (ContainerProvider, error) {
	socketDir, ok := os.LookupEnv("XDG_RUNTIME_DIR")
	if !ok {
		socketDir = "/run"
	}
	socket := socketDir + "/podman/podman.sock"

	apiclient, err := client.NewClientWithOpts(client.WithHost("unix://"+socket), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("could not connect to Podman: %w", err)
	}

	_, err = apiclient.ServerVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("connection check failed: %w", err)
	}

	return &PodmanProvider{
		DockerProvider: DockerProvider{
			client: apiclient,
		},
	}, nil
}

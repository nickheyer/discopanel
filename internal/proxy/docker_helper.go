package proxy

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
)

// GetContainerIP gets the IP address of a container on the specified network
func GetContainerIP(containerID string, networkName string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", err
	}
	defer cli.Close()

	ctx := context.Background()
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

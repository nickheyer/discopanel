package docker

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/nickheyer/discopanel/pkg/logger"
)

// CleanupOrphanedContainers removes containers that are no longer tracked in the database
func (c *Client) CleanupOrphanedContainers(ctx context.Context, trackedContainerIDs map[string]bool, log *logger.Logger) error {
	// List all containers with the discopanel prefix
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "discopanelserver-")

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return err
	}

	for _, cont := range containers {
		// Check if this container is tracked in the database
		if !trackedContainerIDs[cont.ID] {
			log.Info("Found orphaned container %s (%s), removing...", cont.ID[:12], cont.Names[0])

			// Stop container if running
			if cont.State == "running" {
				timeout := 30
				if err := c.docker.ContainerStop(ctx, cont.ID, container.StopOptions{
					Timeout: &timeout,
				}); err != nil {
					log.Error("Failed to stop orphaned container %s: %v", cont.ID[:12], err)
				}
			}

			// Remove container
			if err := c.docker.ContainerRemove(ctx, cont.ID, container.RemoveOptions{
				Force: true,
			}); err != nil {
				log.Error("Failed to remove orphaned container %s: %v", cont.ID[:12], err)
			} else {
				log.Info("Successfully removed orphaned container %s", cont.ID[:12])
			}
		}
	}

	return nil
}

// GetDiscopanelContainers returns all containers managed by DiscoPanel
func (c *Client) GetDiscopanelContainers(ctx context.Context) ([]string, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "discopanelserver-")

	containers, err := c.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, err
	}

	containerIDs := make([]string, 0, len(containers))
	for _, cont := range containers {
		containerIDs = append(containerIDs, cont.ID)
	}

	return containerIDs, nil
}

// GetContainerInfo returns basic information about a container
func (c *Client) GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	inspect, err := c.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	info := &ContainerInfo{
		ID:      containerID,
		Name:    strings.TrimPrefix(inspect.Name, "/"),
		State:   inspect.State.Status,
		Created: inspect.Created,
	}

	// Extract server ID from container name (format: discopanelserver-{uuid})
	if after, ok := strings.CutPrefix(info.Name, "discopanelserver-"); ok {
		info.ServerID = after
	}

	return info, nil
}

type ContainerInfo struct {
	ID       string
	Name     string
	ServerID string
	State    string
	Created  string
}

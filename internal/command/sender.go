package command

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	rcon "github.com/nickheyer/discopanel/internal/rcon"
)

type Sender struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
}

func NewSender(store *storage.Store, dockerClient *docker.Client, cfg *config.Config) *Sender {
	return &Sender{
		store:  store,
		docker: dockerClient,
		config: cfg,
	}
}

func (s *Sender) SendCommand(ctx context.Context, serverID string, command string) (string, error) {
	server, err := s.store.GetServer(ctx, serverID)

	if err != nil {
		return "", fmt.Errorf("server container not found")
	}
	if server.ContainerID == "" {
		return "", fmt.Errorf("server container not found")
	}

	serverCfg, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		return "", fmt.Errorf("failed to load server config: %w", err)
	}

	if serverCfg.EnableRCON != nil && !*serverCfg.EnableRCON {
		return "", fmt.Errorf("rcon is disabled for this server")
	}

	rconPort := 25575
	if v, ok := s.config.Minecraft.GlobalConfig["rconPort"]; ok && v != nil {
		switch t := v.(type) {
		case int:
			rconPort = t
		case int64:
			rconPort = int(t)
		case float64:
			rconPort = int(t)
		case string:
			if p, err := strconv.Atoi(t); err == nil {
				rconPort = p
			}
		}
	}
	if serverCfg.RCONPort != nil {
		rconPort = *serverCfg.RCONPort
	}

	var rconPassword string
	if v, ok := s.config.Minecraft.GlobalConfig["rconPassword"]; ok && v != nil {
		if p, ok := v.(string); ok {
			rconPassword = p
		} else {
			rconPassword = fmt.Sprint(v)
		}
	}
	if serverCfg.RCONPassword != nil {
		rconPassword = *serverCfg.RCONPassword
	}
	if rconPassword == "" {
		// Mirrors the provisioner's enforced default in server.properties.
		rconPassword = "discopanel_default"
		if len(server.ID) >= 8 {
			rconPassword = "discopanel_" + server.ID[:8]
		}
	}

	ip, err := s.docker.ContainerIP(ctx, server.ContainerID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve container ip: %w", err)
	}

	// run command in dedicated context with timeout
	rconCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := rcon.SendCommand(rconCtx, ip, rconPort, rconPassword, command)
	if err != nil {
		return "", fmt.Errorf("rcon command failed: %w", err)
	}

	return output, nil
}

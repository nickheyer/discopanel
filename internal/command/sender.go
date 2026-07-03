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

// ConsoleAgent is the runtime agent hub's console path: commands written
// straight to the java process stdin. Used when RCON cannot serve a command
// (disabled, or the server is still booting).
type ConsoleAgent interface {
	Connected(serverID string) bool
	SendConsole(ctx context.Context, serverID, command string) error
}

type Sender struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
	agent  ConsoleAgent
}

func NewSender(store *storage.Store, dockerClient *docker.Client, cfg *config.Config) *Sender {
	return &Sender{
		store:  store,
		docker: dockerClient,
		config: cfg,
	}
}

// SetAgent wires the runtime agent hub (registered after construction, the
// hub depends on the metrics collector which depends on this sender).
func (s *Sender) SetAgent(agent ConsoleAgent) {
	s.agent = agent
}

// sendViaAgent falls back to the agent console channel. Stdin commands have
// no captured response; their output lands in the server console stream.
func (s *Sender) sendViaAgent(ctx context.Context, serverID, command string) (string, error) {
	if err := s.agent.SendConsole(ctx, serverID, command); err != nil {
		return "", err
	}
	return "", nil
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

	agentAvailable := s.agent != nil && s.agent.Connected(serverID)

	if serverCfg.EnableRCON != nil && !*serverCfg.EnableRCON {
		if agentAvailable {
			return s.sendViaAgent(ctx, serverID, command)
		}
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
		if agentAvailable {
			return s.sendViaAgent(ctx, serverID, command)
		}
		return "", fmt.Errorf("failed to resolve container ip: %w", err)
	}

	// run command in dedicated context with timeout
	rconCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := rcon.SendCommand(rconCtx, ip, rconPort, rconPassword, command)
	if err != nil {
		// RCON is preferred for its captured response, but a booting or
		// RCON-less server still accepts commands over the agent's stdin.
		if agentAvailable {
			return s.sendViaAgent(ctx, serverID, command)
		}
		return "", fmt.Errorf("rcon command failed: %w", err)
	}

	return output, nil
}

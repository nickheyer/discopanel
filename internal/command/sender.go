package command

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/proxy"
	rcon "github.com/nickheyer/discopanel/internal/rcon"
	"github.com/nickheyer/discopanel/pkg/logger"
)

type DockerExecutor interface {
	ExecCommand(ctx context.Context, containerID string, command string) (string, error)
}

type Sender struct {
	store   *storage.Store
	config  *config.Config
	docker  DockerExecutor
	log     *logger.Logger
	mu      sync.RWMutex
	clients map[string]*rcon.Client
}

func NewSender(store *storage.Store, cfg *config.Config, docker DockerExecutor, log *logger.Logger) *Sender {
	return &Sender{
		store:   store,
		config:  cfg,
		docker:  docker,
		log:     log,
		clients: make(map[string]*rcon.Client),
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

	// old docker exec command
	dockerExec := func(cause error) (string, error) {
		output, err := s.docker.ExecCommand(ctx, server.ContainerID, command)
		if err != nil {
			return "", fmt.Errorf("rcon path failed: %w; fallback exec failed: %v", cause, err)
		}
		return output, nil
	}

	serverCfg, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to load server config: %w", err))
	}

	if serverCfg.EnableRCON != nil && *serverCfg.EnableRCON == false {
		return dockerExec(fmt.Errorf("rcon is disabled for this server"))
	}

	var rconPort int
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

	ip, err := proxy.GetContainerIP(server.ContainerID, s.config.Docker.NetworkName)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to resolve container ip: %w", err))
	}

	client, err := s.getOrCreateClient(serverID, ip, rconPort, rconPassword)
	if err != nil {
		return dockerExec(fmt.Errorf("failed to initialise rcon connection: %w", err))
	}

	output, err := client.Execute(command)

	if err != nil {
		fmt.Print(err)
		s.Remove(serverID)
		return dockerExec(fmt.Errorf("rcon command failed: %w", err))
	}

	return output, nil
}

func (s *Sender) getOrCreateClient(serverID string, host string, port int, password string) (*rcon.Client, error) {
	s.mu.RLock()
	client, exists := s.clients[serverID]
	s.mu.RUnlock()

	if exists {
		if client.Host() == host && client.Port() == port && client.Password() == password {
			return client, nil
		}

		s.Remove(serverID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists = s.clients[serverID]; exists {
		return client, nil
	}

	client = rcon.NewClient(host, port, password, s.log)

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to server %s: %w", serverID, err)
	}

	s.clients[serverID] = client
	return client, nil
}

func (s *Sender) Remove(serverID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists := s.clients[serverID]; exists {
		_ = client.Close()
		delete(s.clients, serverID)
		s.log.Info("RCON connection removed for server %s", serverID)
	}
}

func (s *Sender) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, client := range s.clients {
		_ = client.Close()
		delete(s.clients, id)
	}
	s.log.Info("RCON All connections closed")
}

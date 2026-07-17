package command

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nickheyer/discopanel/internal/activity"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"

	"github.com/jltobler/go-rcon"
)

var (
	ErrEmptyCommand   = errors.New("command is required")
	ErrServerNotFound = errors.New("server not found")
	ErrNoContainer    = errors.New("server has no container")
	ErrNotRunning     = errors.New("server is not running")
)

// Runtime agent hub's console path, used when RCON cannot serve
type ConsoleAgent interface {
	Connected(serverID string) bool
	SendConsole(ctx context.Context, serverID, command string) error
}

type Sender struct {
	store    *storage.Store
	docker   *docker.Client
	config   *config.Config
	agent    ConsoleAgent
	rec      *activity.Recorder
	streamer *logger.LogStreamer
}

func NewSender(store *storage.Store, dockerClient *docker.Client, cfg *config.Config) *Sender {
	return &Sender{
		store:  store,
		docker: dockerClient,
		config: cfg,
	}
}

// Wires agent hub after construction due to dependency order
func (s *Sender) SetAgent(agent ConsoleAgent) {
	s.agent = agent
}

// Wires ledger and console echo after construction
func (s *Sender) SetJournal(rec *activity.Recorder, streamer *logger.LogStreamer) {
	s.rec = rec
	s.streamer = streamer
}

type rconResult struct {
	output string
	err    error
}

func SendCommand(ctx context.Context, RCONHost string, RCONPort int, RCONPassword string, command string) (string, error) {
	// initialize Client
	rconClient := rcon.NewClient(fmt.Sprintf("rcon://%s:%d", RCONHost, RCONPort), RCONPassword)

	// run Command in a goroutine to allow for timeout handling
	resultCh := make(chan rconResult, 1)
	go func() {
		output, sendErr := rconClient.Send(command)
		resultCh <- rconResult{output: output, err: sendErr}
	}()

	// wait for either the command result or a timeout
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return "", result.err
		}
		return result.output, nil
	}
}

// Gates, echoes, sends, and records one console command
func (s *Sender) Run(ctx context.Context, serverID, cmd string, silent bool) (string, error) {
	if cmd == "" {
		return "", ErrEmptyCommand
	}
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return "", ErrServerNotFound
	}
	if server.ContainerID == "" {
		return "", ErrNoContainer
	}
	status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
	if err != nil || (status != storage.StatusRunning && status != storage.StatusUnhealthy) {
		return "", ErrNotRunning
	}

	commandTime := time.Now()
	if !silent && s.streamer != nil {
		s.streamer.AddCommandEntry(server.ID, cmd, commandTime)
	}

	output, err := s.SendCommand(ctx, server.ID, cmd)
	if err == nil {
		s.rec.Record(ctx, server.ID, "command.run", activity.Attrs{"command": cmd}, "ran command %q", cmd)
	}
	if !silent && s.streamer != nil && (output != "" || err != nil) {
		s.streamer.AddCommandOutput(server.ID, output, err == nil, commandTime)
	}
	return output, err
}

// Falls back to agent console, stdin has no captured response
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

	serverCfg, err := s.store.GetServerProperties(ctx, serverID)
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

	ip, err := s.docker.ContainerIP(ctx, server.ContainerID)
	if err != nil {
		if agentAvailable {
			return s.sendViaAgent(ctx, serverID, command)
		}
		return "", fmt.Errorf("failed to resolve container ip: %w", err)
	}

	// Run command in dedicated context with timeout
	rconCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := SendCommand(rconCtx, ip, rconPort, rconPassword, command)
	if err != nil {
		// RCON preferred but a booting server falls back to stdin
		if agentAvailable {
			return s.sendViaAgent(ctx, serverID, command)
		}
		return "", fmt.Errorf("rcon command failed: %w", err)
	}

	return output, nil
}

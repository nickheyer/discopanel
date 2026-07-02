// Package lifecycle is the single owner of server lifecycle transitions:
// provisioning + start, graceful stop, restart, container recreation, and
// autopause/wake. The RPC layer, scheduler, auto-start, and idle policies all
// delegate here instead of driving the docker client directly.
package lifecycle

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/internal/command"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/provisioner"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// PlayerCounter reports the last observed online player count for a server.
type PlayerCounter interface {
	PlayersOnline(serverID string) (count int, known bool)
}

type Manager struct {
	store    *storage.Store
	docker   *docker.Client
	prov     *provisioner.Provisioner
	sender   *command.Sender
	proxy    *proxy.Manager
	bus      *events.Bus
	cfg      *config.Config
	log      *logger.Logger
	players  PlayerCounter
	streamer *logger.LogStreamer

	// Per-server start locks: rejects concurrent starts of the same server.
	startMu sync.Mutex
	starts  map[string]bool

	// Paused-server set consulted by the proxy wake gate (hot path).
	pausedMu sync.RWMutex
	paused   map[string]bool

	// Roster tracking for first/last-connect RCON commands.
	rosterMu     sync.Mutex
	roster       map[string]map[string]bool
	firstConnect map[string]bool

	// Idle tracking for autopause/autostop.
	idleMu    sync.Mutex
	idle      map[string]*idleState
	stopWatch chan struct{}
	watchWG   sync.WaitGroup
}

func NewManager(store *storage.Store, dockerClient *docker.Client, prov *provisioner.Provisioner, sender *command.Sender, proxyManager *proxy.Manager, bus *events.Bus, cfg *config.Config, log *logger.Logger) *Manager {
	return &Manager{
		store:        store,
		docker:       dockerClient,
		prov:         prov,
		sender:       sender,
		proxy:        proxyManager,
		bus:          bus,
		cfg:          cfg,
		log:          log,
		starts:       make(map[string]bool),
		paused:       make(map[string]bool),
		roster:       make(map[string]map[string]bool),
		firstConnect: make(map[string]bool),
		idle:         make(map[string]*idleState),
	}
}

// SetPlayerCounter wires the metrics collector (registered after construction
// because the collector depends on the docker client this manager also uses).
func (m *Manager) SetPlayerCounter(pc PlayerCounter) {
	m.players = pc
}

// SetLogStreamer wires the log streamer so container output follows the servers log stream
func (m *Manager) SetLogStreamer(streamer *logger.LogStreamer) {
	m.streamer = streamer
}

func (m *Manager) tryBeginStart(serverID string) bool {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.starts[serverID] {
		return false
	}
	m.starts[serverID] = true
	return true
}

func (m *Manager) endStart(serverID string) {
	m.startMu.Lock()
	delete(m.starts, serverID)
	m.startMu.Unlock()
}

// IsStarting reports whether a start/provision cycle is in flight.
func (m *Manager) IsStarting(serverID string) bool {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	return m.starts[serverID]
}

func (m *Manager) setStatus(ctx context.Context, server *storage.Server, status storage.ServerStatus) {
	server.Status = status
	if err := m.store.UpdateServer(ctx, server); err != nil {
		m.log.Error("lifecycle: failed to persist status %s for %s: %v", status, server.Name, err)
	}
}

// Start provisions the server and starts its container. It blocks until the
// container is started (or provisioning fails); run it in a goroutine from
// request handlers.
func (m *Manager) Start(ctx context.Context, serverID string) error {
	if !m.tryBeginStart(serverID) {
		return fmt.Errorf("server is already starting")
	}
	defer m.endStart(serverID)

	server, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}
	serverCfg, err := m.store.GetServerConfig(ctx, serverID)
	if err != nil {
		return fmt.Errorf("failed to get server configuration: %w", err)
	}

	// Already running? Nothing to do.
	if server.ContainerID != "" {
		if status, err := m.docker.GetContainerStatus(ctx, server.ContainerID); err == nil {
			switch status {
			case storage.StatusRunning, storage.StatusStarting:
				return nil
			case storage.StatusPaused:
				return m.Wake(ctx, serverID)
			}
		}
	}

	// Provision server files.
	m.setStatus(ctx, server, storage.StatusProvisioning)
	result, err := m.prov.Ensure(ctx, server, serverCfg)
	if err != nil {
		m.setStatus(ctx, server, storage.StatusError)
		return fmt.Errorf("provisioning failed: %w", err)
	}

	// Sync resolved facts (modpacks are authoritative for MC version).
	if result.MCVersion != "" && result.MCVersion != server.MCVersion {
		m.log.Info("lifecycle: %s resolved MC version %s (was %s)", server.Name, result.MCVersion, server.MCVersion)
		server.MCVersion = result.MCVersion
	}
	server.JavaVersion = strconv.Itoa(result.JavaMajor)

	// Ensure a container matching the desired image exists.
	m.setStatus(ctx, server, storage.StatusCreating)
	if err := m.ensureContainer(ctx, server, serverCfg); err != nil {
		m.setStatus(ctx, server, storage.StatusError)
		return err
	}

	// Start it.
	if err := m.docker.StartContainer(ctx, server.ContainerID); err != nil {
		m.log.Warn("lifecycle: start failed for %s, recreating container: %v", server.Name, err)
		recreated, rerr := m.docker.RecreateContainer(ctx, server.ContainerID, server, serverCfg)
		if rerr != nil {
			if recreated != nil && recreated.NewContainerID != "" {
				server.ContainerID = recreated.NewContainerID
			}
			m.setStatus(ctx, server, storage.StatusError)
			return fmt.Errorf("failed to start server container: %w", rerr)
		}
		server.ContainerID = recreated.NewContainerID
		if err := m.docker.StartContainer(ctx, server.ContainerID); err != nil {
			m.setStatus(ctx, server, storage.StatusError)
			return fmt.Errorf("failed to start recreated container: %w", err)
		}
	}

	// Attach the containers output to the server's log stream
	if m.streamer != nil {
		if err := m.streamer.StartStreaming(server.ID, server.ContainerID); err != nil {
			m.log.Warn("lifecycle: failed to start log streaming for %s: %v", server.Name, err)
		}
	}

	now := time.Now()
	server.Status = storage.StatusStarting
	server.LastStarted = &now
	if err := m.store.UpdateServer(ctx, server); err != nil {
		m.log.Error("lifecycle: failed to update server after start: %v", err)
	}

	if err := m.store.ClearEphemeralConfigFields(ctx, server.ID); err != nil {
		m.log.Error("lifecycle: failed to clear ephemeral config fields: %v", err)
	}

	m.setPaused(server.ID, false)
	m.resetRoster(server.ID)
	m.resetIdle(server.ID)

	if m.proxy != nil && server.ProxyHostname != "" {
		if err := m.proxy.UpdateServerRoute(server); err != nil {
			m.log.Error("lifecycle: failed to update proxy route for %s: %v", server.Name, err)
		}
	}

	if m.bus != nil {
		m.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
			ServerID: server.ID,
		})
	}

	return nil
}

// ensureContainer creates the container if missing, or recreates it when the
// desired runtime image changed (e.g. new Java requirement after MC upgrade).
func (m *Manager) ensureContainer(ctx context.Context, server *storage.Server, serverCfg *storage.ServerConfig) error {
	desired := m.docker.DesiredImage(server)

	if server.ContainerID != "" {
		current, err := m.docker.ContainerImage(ctx, server.ContainerID)
		if err == nil && current == desired {
			return nil
		}
		if err == nil && current != desired {
			m.log.Info("lifecycle: %s image changed (%s -> %s), recreating container", server.Name, current, desired)
		}
		result, err := m.docker.RecreateContainer(ctx, server.ContainerID, server, serverCfg)
		if err != nil {
			return fmt.Errorf("failed to recreate server container: %w", err)
		}
		server.ContainerID = result.NewContainerID
		return m.store.UpdateServer(ctx, server)
	}

	containerID, err := m.docker.CreateContainer(ctx, server, serverCfg)
	if err != nil {
		return fmt.Errorf("failed to create server container: %w", err)
	}
	server.ContainerID = containerID
	return m.store.UpdateServer(ctx, server)
}

// Stop gracefully stops a server: optional in-game announcement, then a
// SIGTERM with the configured stop-duration window.
func (m *Manager) Stop(ctx context.Context, serverID string) error {
	server, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}

	if server.ContainerID == "" {
		m.setStatus(ctx, server, storage.StatusStopped)
		return nil
	}

	serverCfg, _ := m.store.GetServerConfig(ctx, serverID)
	stopDuration := docker.DefaultStopTimeoutSeconds
	announceDelay := 0
	if serverCfg != nil {
		if serverCfg.StopDuration != nil && *serverCfg.StopDuration > 0 {
			stopDuration = *serverCfg.StopDuration
		}
		if serverCfg.StopServerAnnounceDelay != nil {
			announceDelay = min(*serverCfg.StopServerAnnounceDelay, 300)
		}
	}

	// A paused container cannot process signals; resume it first.
	if paused, err := m.docker.IsContainerPaused(ctx, server.ContainerID); err == nil && paused {
		if err := m.docker.UnpauseContainer(ctx, server.ContainerID); err != nil {
			m.log.Warn("lifecycle: failed to unpause %s before stop: %v", server.Name, err)
		}
		m.setPaused(server.ID, false)
	} else if announceDelay > 0 && server.Status == storage.StatusRunning {
		msg := fmt.Sprintf("say Server is shutting down in %d seconds", announceDelay)
		if _, err := m.sender.SendCommand(ctx, server.ID, msg); err == nil {
			select {
			case <-time.After(time.Duration(announceDelay) * time.Second):
			case <-ctx.Done():
			}
		}
	}

	m.setStatus(ctx, server, storage.StatusStopping)

	found, err := m.docker.StopContainer(ctx, server.ContainerID, stopDuration)
	if err != nil {
		m.setStatus(ctx, server, storage.StatusError)
		return fmt.Errorf("failed to stop server: %w", err)
	}

	if !found {
		m.log.Warn("lifecycle: container %s not found, cleaning up stale reference", server.ContainerID)
		server.ContainerID = ""
	}
	m.setStatus(ctx, server, storage.StatusStopped)

	m.setPaused(server.ID, false)
	m.resetRoster(server.ID)
	m.resetIdle(server.ID)

	if m.proxy != nil && server.ProxyHostname != "" {
		if err := m.proxy.RemoveServerRoute(server.ID); err != nil {
			m.log.Error("lifecycle: failed to remove proxy route for %s: %v", server.Name, err)
		}
	}

	if m.bus != nil {
		m.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP,
			ServerID: server.ID,
		})
	}

	return nil
}

// Restart stops the server then runs a full start (re-applying provisioned
// configuration files in the process).
func (m *Manager) Restart(ctx context.Context, serverID string) error {
	if err := m.Stop(ctx, serverID); err != nil {
		return err
	}
	if err := m.Start(ctx, serverID); err != nil {
		return err
	}
	if m.bus != nil {
		m.bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART,
			ServerID: serverID,
		})
	}
	return nil
}

// Recreate destroys the container and brings the server up from scratch.
func (m *Manager) Recreate(ctx context.Context, serverID string) error {
	server, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}

	if server.ContainerID != "" {
		if _, err := m.docker.StopContainer(ctx, server.ContainerID, docker.DefaultStopTimeoutSeconds); err != nil {
			m.log.Warn("lifecycle: failed to stop container during recreate: %v", err)
		}
		if err := m.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
			m.log.Debug("lifecycle: failed to remove container during recreate (may not exist): %v", err)
		}
		server.ContainerID = ""
		if err := m.store.UpdateServer(ctx, server); err != nil {
			return err
		}
	}

	return m.Start(ctx, serverID)
}

// Pause freezes a running server's container (autopause).
func (m *Manager) Pause(ctx context.Context, serverID string) error {
	server, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}
	if server.ContainerID == "" {
		return fmt.Errorf("server has no container")
	}
	if err := m.docker.PauseContainer(ctx, server.ContainerID); err != nil {
		return fmt.Errorf("failed to pause container: %w", err)
	}
	m.setPaused(server.ID, true)
	m.setStatus(ctx, server, storage.StatusPaused)
	m.log.Info("lifecycle: paused idle server %s", server.Name)
	return nil
}

// Wake resumes a paused server's container (wake-on-connect).
func (m *Manager) Wake(ctx context.Context, serverID string) error {
	server, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return err
	}
	if server.ContainerID == "" {
		return fmt.Errorf("server has no container")
	}
	paused, err := m.docker.IsContainerPaused(ctx, server.ContainerID)
	if err != nil {
		return err
	}
	if !paused {
		m.setPaused(server.ID, false)
		return nil
	}
	if err := m.docker.UnpauseContainer(ctx, server.ContainerID); err != nil {
		return fmt.Errorf("failed to unpause container: %w", err)
	}
	m.setPaused(server.ID, false)
	m.resetIdle(server.ID)
	m.setStatus(ctx, server, storage.StatusRunning)
	m.log.Info("lifecycle: woke server %s", server.Name)
	return nil
}

func (m *Manager) setPaused(serverID string, paused bool) {
	m.pausedMu.Lock()
	if paused {
		m.paused[serverID] = true
	} else {
		delete(m.paused, serverID)
	}
	m.pausedMu.Unlock()
}

func (m *Manager) isPausedFast(serverID string) bool {
	m.pausedMu.RLock()
	defer m.pausedMu.RUnlock()
	return m.paused[serverID]
}

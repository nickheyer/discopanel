package lifecycle

import (
	"context"
	"time"

	"github.com/nickheyer/discopanel/internal/activity"
	storage "github.com/nickheyer/discopanel/internal/db"
)

const idleCheckInterval = 30 * time.Second

// Tracks how long a server has been without players
type idleState struct {
	lastActive time.Time
	hadPlayers bool
}

// Launches the autopause/autostop policy loop
func (m *Manager) StartIdleWatcher() {
	m.idleMu.Lock()
	if m.stopWatch != nil {
		m.idleMu.Unlock()
		return
	}
	m.stopWatch = make(chan struct{})
	stop := m.stopWatch
	m.idleMu.Unlock()

	m.watchWG.Add(1)
	go func() {
		defer m.watchWG.Done()
		ticker := time.NewTicker(idleCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.checkIdleServers()
			case <-stop:
				return
			}
		}
	}()
	m.log.Info("lifecycle: idle watcher started (autopause/autostop)")
}

// Stops the autopause/autostop policy loop
func (m *Manager) StopIdleWatcher() {
	m.idleMu.Lock()
	if m.stopWatch != nil {
		close(m.stopWatch)
		m.stopWatch = nil
	}
	m.idleMu.Unlock()
	m.watchWG.Wait()
}

func (m *Manager) resetIdle(serverID string) {
	m.idleMu.Lock()
	delete(m.idle, serverID)
	m.idleMu.Unlock()
}

// Applies autopause/autostop policies to running servers
func (m *Manager) checkIdleServers() {
	ctx, cancel := context.WithTimeout(context.Background(), idleCheckInterval)
	defer cancel()

	servers, err := m.store.ListServers(ctx)
	if err != nil {
		return
	}

	for _, server := range servers {
		if server.ContainerID == "" || server.Detached || m.IsStarting(server.ID) {
			continue
		}

		cfg, err := m.store.GetServerProperties(ctx, server.ID)
		if err != nil {
			continue
		}
		autopause := cfg.EnableAutopause != nil && *cfg.EnableAutopause && server.ProxyHostname != ""
		autostop := cfg.EnableAutostop != nil && *cfg.EnableAutostop
		if !autopause && !autostop {
			m.resetIdle(server.ID)
			continue
		}

		status, err := m.docker.GetContainerStatus(ctx, server.ContainerID)
		if err != nil {
			m.resetIdle(server.ID)
			continue
		}

		// Paused servers can still be autostopped after stop timeout
		if status == storage.StatusPaused {
			if autostop && m.idleFor(server) >= m.idleTimeout(cfg, server.ID, false) {
				m.log.Info("lifecycle: autostopping paused idle server %s", server.Name)
				go m.stopIdle(server.ID)
			}
			continue
		}

		if status != storage.StatusRunning {
			continue
		}

		players := 0
		known := false
		if m.players != nil {
			players, known = m.players.PlayersOnline(server.ID)
		}
		if !known {
			// Without player data, never take idle actions
			continue
		}

		now := time.Now()
		m.idleMu.Lock()
		st, ok := m.idle[server.ID]
		if !ok {
			st = &idleState{lastActive: now}
			if server.LastStarted != nil {
				st.lastActive = *server.LastStarted
			}
			m.idle[server.ID] = st
		}
		if players > 0 {
			st.lastActive = now
			st.hadPlayers = true
		}
		idleFor := now.Sub(st.lastActive)
		hadPlayers := st.hadPlayers
		m.idleMu.Unlock()

		if players > 0 {
			continue
		}

		if autopause && idleFor >= m.timeoutFor(intOrDefault(cfg.AutopauseTimeoutEst, 3600), intOrDefault(cfg.AutopauseTimeoutInit, 600), hadPlayers) {
			if err := m.Pause(activity.WithTrace(activity.WithSource(ctx, "autopause")), server.ID); err != nil {
				m.log.Error("lifecycle: autopause failed for %s: %v", server.Name, err)
			}
			continue
		}

		if autostop && idleFor >= m.timeoutFor(intOrDefault(cfg.AutostopTimeoutEst, 3600), intOrDefault(cfg.AutostopTimeoutInit, 1800), hadPlayers) {
			m.log.Info("lifecycle: autostopping idle server %s", server.Name)
			go m.stopIdle(server.ID)
		}
	}
}

// Returns how long server has been idle per tracked state
func (m *Manager) idleFor(server *storage.Server) time.Duration {
	m.idleMu.Lock()
	defer m.idleMu.Unlock()
	if st, ok := m.idle[server.ID]; ok {
		return time.Since(st.lastActive)
	}
	if server.LastStarted != nil {
		return time.Since(*server.LastStarted)
	}
	return 0
}

// Resolves the applicable autostop timeout for a server
func (m *Manager) idleTimeout(cfg *storage.ServerProperties, serverID string, _ bool) time.Duration {
	m.idleMu.Lock()
	hadPlayers := false
	if st, ok := m.idle[serverID]; ok {
		hadPlayers = st.hadPlayers
	}
	m.idleMu.Unlock()
	return m.timeoutFor(intOrDefault(cfg.AutostopTimeoutEst, 3600), intOrDefault(cfg.AutostopTimeoutInit, 1800), hadPlayers)
}

func (m *Manager) timeoutFor(establishedSecs, initialSecs int, hadPlayers bool) time.Duration {
	if hadPlayers {
		return time.Duration(establishedSecs) * time.Second
	}
	return time.Duration(initialSecs) * time.Second
}

func (m *Manager) stopIdle(serverID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := m.Stop(activity.WithTrace(activity.WithSource(ctx, "autostop")), serverID); err != nil {
		m.log.Error("lifecycle: autostop failed for server %s: %v", serverID, err)
	}
}

func intOrDefault(v *int, def int) int {
	if v == nil || *v <= 0 {
		return def
	}
	return *v
}

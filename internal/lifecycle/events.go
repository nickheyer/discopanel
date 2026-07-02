package lifecycle

import (
	"context"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/proxy"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// HandleServerEvent runs the configured lifecycle RCON commands (startup,
// on-connect, first-connect, on-disconnect, last-disconnect) in response to
// bus events. Registered as an event bus subscriber.
func (m *Manager) HandleServerEvent(ctx context.Context, event events.Event) {
	switch event.Type {
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
		v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP:
		m.resetRoster(event.ServerID)
		return
	}

	cfg, err := m.store.GetServerConfig(ctx, event.ServerID)
	if err != nil {
		return
	}

	switch event.Type {
	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_HEALTHY:
		m.runRCONCommands(ctx, event.ServerID, cfg.RCONCmdsStartup)

	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN:
		first := m.trackJoin(event.ServerID, eventPlayer(event))
		m.runRCONCommands(ctx, event.ServerID, cfg.RCONCmdsOnConnect)
		if first {
			m.runRCONCommands(ctx, event.ServerID, cfg.RCONCmdsFirstConnect)
		}

	case v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_LEAVE:
		last := m.trackLeave(event.ServerID, eventPlayer(event))
		m.runRCONCommands(ctx, event.ServerID, cfg.RCONCmdsOnDisconnect)
		if last {
			m.runRCONCommands(ctx, event.ServerID, cfg.RCONCmdsLastDisconnect)
		}
	}
}

func eventPlayer(event events.Event) string {
	if event.Data == nil {
		return ""
	}
	if name, ok := event.Data["player"].(string); ok {
		return name
	}
	return ""
}

// runRCONCommands executes newline-delimited commands via RCON.
func (m *Manager) runRCONCommands(ctx context.Context, serverID string, commands *string) {
	if commands == nil {
		return
	}
	for _, line := range strings.Split(*commands, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := m.sender.SendCommand(ctx, serverID, line); err != nil {
			m.log.Warn("lifecycle: rcon command %q failed for server %s: %v", line, serverID, err)
		}
	}
}

func (m *Manager) resetRoster(serverID string) {
	m.rosterMu.Lock()
	delete(m.roster, serverID)
	delete(m.firstConnect, serverID)
	m.rosterMu.Unlock()
}

// trackJoin records a join; returns true when it is the first connect since start.
func (m *Manager) trackJoin(serverID, player string) bool {
	m.rosterMu.Lock()
	defer m.rosterMu.Unlock()
	if m.roster[serverID] == nil {
		m.roster[serverID] = make(map[string]bool)
	}
	if player != "" {
		m.roster[serverID][player] = true
	}
	if !m.firstConnect[serverID] {
		m.firstConnect[serverID] = true
		return true
	}
	return false
}

// trackLeave records a leave; returns true when the server is now empty.
func (m *Manager) trackLeave(serverID, player string) bool {
	m.rosterMu.Lock()
	defer m.rosterMu.Unlock()
	if player != "" && m.roster[serverID] != nil {
		delete(m.roster[serverID], player)
	}
	return len(m.roster[serverID]) == 0
}

// --- proxy wake gate -------------------------------------------------------

// Compile-time check that Manager implements the proxy's wake gate.
var _ proxy.ServerGate = (*Manager)(nil)

// SleepingInfo reports whether a server is paused, with data for the proxy's
// synthesized "sleeping" status response.
func (m *Manager) SleepingInfo(serverID string) (*proxy.SleepingServer, bool) {
	if !m.isPausedFast(serverID) {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, false
	}

	motd := server.Name + " is sleeping - join to wake it up"
	if cfg, err := m.store.GetServerConfig(ctx, serverID); err == nil && cfg.MOTD != nil && *cfg.MOTD != "" {
		motd = *cfg.MOTD + " (sleeping - join to wake)"
	}

	return &proxy.SleepingServer{
		MOTD:       motd,
		MaxPlayers: server.MaxPlayers,
	}, true
}

// WakeServer resumes a paused server for an incoming login (proxy hot path).
func (m *Manager) WakeServer(ctx context.Context, serverID string) error {
	return m.Wake(ctx, serverID)
}

// Package agent is the panel-side endpoint of the disco-agent channel: it
// tracks the live telemetry session each runtime supervisor holds open,
// fans agent telemetry into the metrics collector and the event bus, and
// carries panel-to-container messages (console commands, chat).
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/events"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/pkg/logger"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// ConsoleSink receives human-readable agent lines for a server's console
// stream (wired to the log streamer's system entries).
type ConsoleSink func(serverID string, message string)

// Session is one live agent stream. The RPC handler owns the stream; the hub
// owns the registry and the outbound queue.
type Session struct {
	ServerID string
	sendCh   chan *agentv1.PanelMessage
	closed   chan struct{}
	once     sync.Once
}

// Outbound returns the channel of panel-to-agent messages the RPC handler
// must pump into the stream.
func (s *Session) Outbound() <-chan *agentv1.PanelMessage {
	return s.sendCh
}

// Closed reports session teardown to the RPC handler's send pump.
func (s *Session) Closed() <-chan struct{} {
	return s.closed
}

func (s *Session) close() {
	s.once.Do(func() { close(s.closed) })
}

// Hub tracks live agent sessions and routes telemetry.
type Hub struct {
	store     *storage.Store
	collector *metrics.Collector
	bus       *events.Bus
	log       *logger.Logger

	mu       sync.Mutex
	sessions map[string]*Session
	sink     ConsoleSink
}

func NewHub(store *storage.Store, collector *metrics.Collector, bus *events.Bus, log *logger.Logger) *Hub {
	return &Hub{
		store:     store,
		collector: collector,
		bus:       bus,
		log:       log,
		sessions:  make(map[string]*Session),
	}
}

// SetConsoleSink wires agent lifecycle lines into the server console stream.
func (h *Hub) SetConsoleSink(sink ConsoleSink) {
	h.mu.Lock()
	h.sink = sink
	h.mu.Unlock()
}

func (h *Hub) console(serverID, format string, args ...any) {
	h.mu.Lock()
	sink := h.sink
	h.mu.Unlock()
	if sink != nil {
		sink(serverID, fmt.Sprintf(format, args...))
	}
}

// Attach registers a new live session for a server, displacing any previous
// one (a reconnect supersedes the stale stream).
func (h *Hub) Attach(serverID string, hello *agentv1.Hello) *Session {
	sess := &Session{
		ServerID: serverID,
		sendCh:   make(chan *agentv1.PanelMessage, 64),
		closed:   make(chan struct{}),
	}
	h.mu.Lock()
	old := h.sessions[serverID]
	h.sessions[serverID] = sess
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
	h.collector.SetAgentConnected(serverID, true)
	h.log.Info("agent: session attached for server %s (runtime %s, %s MC %s)",
		serverID, hello.GetVersion(), hello.GetLoader(), hello.GetMcVersion())
	return sess
}

// Detach unregisters a session (no-op when a newer session displaced it).
func (h *Hub) Detach(serverID string, sess *Session) {
	h.mu.Lock()
	current := h.sessions[serverID] == sess
	if current {
		delete(h.sessions, serverID)
	}
	h.mu.Unlock()
	sess.close()
	if current {
		h.collector.SetAgentConnected(serverID, false)
		h.log.Info("agent: session detached for server %s", serverID)
	}
}

// Connected reports whether a live agent session exists for the server.
func (h *Hub) Connected(serverID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[serverID] != nil
}

func (h *Hub) sendToAgent(serverID string, msg *agentv1.PanelMessage) error {
	h.mu.Lock()
	sess := h.sessions[serverID]
	h.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("no agent session for server %s", serverID)
	}
	select {
	case sess.sendCh <- msg:
		return nil
	case <-sess.closed:
		return fmt.Errorf("agent session for server %s is closing", serverID)
	case <-time.After(5 * time.Second):
		return fmt.Errorf("agent session for server %s is not draining", serverID)
	}
}

// SendConsole writes one command line to the server's java stdin via the
// runtime supervisor. Works during boot and with RCON disabled.
func (h *Hub) SendConsole(ctx context.Context, serverID, command string) error {
	_ = ctx
	return h.sendToAgent(serverID, &agentv1.PanelMessage{Payload: &agentv1.PanelMessage_ConsoleCommand{
		ConsoleCommand: &agentv1.ConsoleCommand{Command: command},
	}})
}

// SendChat broadcasts a chat message in game via the supervisor's tellraw.
func (h *Hub) SendChat(ctx context.Context, serverID, sender, message string) error {
	_ = ctx
	return h.sendToAgent(serverID, &agentv1.PanelMessage{Payload: &agentv1.PanelMessage_ChatMessage{
		ChatMessage: &agentv1.ChatMessage{Sender: sender, Message: message},
	}})
}

// HandleMessage routes one agent telemetry message into the collector and
// the event bus.
func (h *Hub) HandleMessage(ctx context.Context, serverID string, msg *agentv1.AgentMessage) {
	switch p := msg.GetPayload().(type) {
	case *agentv1.AgentMessage_Hello:
		// A second hello on an open session is the javaagent coming up.
		if p.Hello.GetSource() == agentv1.HelloSource_HELLO_SOURCE_JVM {
			h.collector.SetAgentJvmActive(serverID, true)
			h.console(serverID, "JVM telemetry active (disco-agent %s)", p.Hello.GetVersion())
		}

	case *agentv1.AgentMessage_Ready:
		h.collector.ApplyAgentReady(ctx, serverID, p.Ready.GetStartupSeconds())
		if secs := p.Ready.GetStartupSeconds(); secs > 0 {
			h.console(serverID, "server ready in %.1fs", secs)
		}

	case *agentv1.AgentMessage_Stopping:
		h.console(serverID, "server is shutting down")

	case *agentv1.AgentMessage_Exited:
		h.collector.ApplyAgentExit(serverID, int(p.Exited.GetExitCode()), p.Exited.GetCrashed(),
			p.Exited.GetCrashReportPath(), p.Exited.GetCrashReportExcerpt())
		if p.Exited.GetCrashed() {
			if path := p.Exited.GetCrashReportPath(); path != "" {
				h.console(serverID, "server crashed (exit code %d, crash report: %s)", p.Exited.GetExitCode(), path)
			} else {
				h.console(serverID, "server exited abnormally (exit code %d)", p.Exited.GetExitCode())
			}
		}

	case *agentv1.AgentMessage_ProcSample:
		h.collector.ApplyAgentProc(serverID, p.ProcSample)

	case *agentv1.AgentMessage_TickSample:
		h.collector.ApplyAgentTick(serverID, p.TickSample)

	case *agentv1.AgentMessage_JvmSample:
		h.collector.ApplyAgentJvm(serverID, p.JvmSample)

	case *agentv1.AgentMessage_PlayerEvent:
		h.handlePlayerEvent(ctx, serverID, p.PlayerEvent)

	case *agentv1.AgentMessage_Roster:
		h.collector.ApplyAgentRoster(serverID, p.Roster.GetOnlinePlayers())
	}
}

// handlePlayerEvent updates the roster and emits the corresponding bus event,
// replacing SLP roster diffing for agent-connected servers.
func (h *Hub) handlePlayerEvent(ctx context.Context, serverID string, ev *agentv1.PlayerEvent) {
	player := ev.GetPlayer()
	data := map[string]any{"player": player}
	if d := ev.GetDetail(); d != "" {
		data["detail"] = d
	}

	var eventType v1.TriggeredEventType
	switch ev.GetType() {
	case agentv1.PlayerEventType_PLAYER_EVENT_TYPE_JOIN:
		h.collector.ApplyAgentPlayerChange(serverID, player, true, int(ev.GetPlayersOnline()))
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN
	case agentv1.PlayerEventType_PLAYER_EVENT_TYPE_LEAVE:
		h.collector.ApplyAgentPlayerChange(serverID, player, false, int(ev.GetPlayersOnline()))
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_LEAVE
	case agentv1.PlayerEventType_PLAYER_EVENT_TYPE_DEATH:
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_DEATH
	case agentv1.PlayerEventType_PLAYER_EVENT_TYPE_ADVANCEMENT:
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_ADVANCEMENT
	case agentv1.PlayerEventType_PLAYER_EVENT_TYPE_CHAT:
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_CHAT
	default:
		return
	}

	if h.bus != nil {
		h.bus.Emit(ctx, events.Event{Type: eventType, ServerID: serverID, Data: data})
	}
}

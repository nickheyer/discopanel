package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// MinecraftProxy accepts Minecraft connections on one listener port and
// routes them to backends by the hostname in the protocol handshake.
type MinecraftProxy struct {
	listenAddr string
	logger     *logger.Logger

	routes   map[string]*Route
	routesMu sync.RWMutex

	gate   ServerGate
	gateMu sync.RWMutex

	listener net.Listener
	running  bool
	stateMu  sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewMinecraftProxy creates a new Minecraft proxy instance
func NewMinecraftProxy(cfg *Config) *MinecraftProxy {
	ctx, cancel := context.WithCancel(context.Background())
	return &MinecraftProxy{
		routes:     make(map[string]*Route),
		logger:     cfg.Logger,
		listenAddr: cfg.ListenAddr,
		ctx:        ctx,
		cancel:     cancel,
		gate:       cfg.Gate,
	}
}

// SetGate registers the wake gate for paused servers.
func (p *MinecraftProxy) SetGate(gate ServerGate) {
	p.gateMu.Lock()
	p.gate = gate
	p.gateMu.Unlock()
}

func (p *MinecraftProxy) getGate() ServerGate {
	p.gateMu.RLock()
	defer p.gateMu.RUnlock()
	return p.gate
}

// normalizeHostname lowercases a hostname and strips ports, FML markers
// appended by modded clients, and a trailing FQDN dot.
func normalizeHostname(hostname string) string {
	if idx := strings.IndexByte(hostname, 0); idx != -1 {
		hostname = hostname[:idx]
	}
	hostname, _, _ = strings.Cut(hostname, ":")
	return strings.ToLower(strings.TrimSuffix(hostname, "."))
}

// AddRoute adds a new routing rule
func (p *MinecraftProxy) AddRoute(serverID, hostname, backendHost string, backendPort int) {
	hostname = normalizeHostname(hostname)

	p.routesMu.Lock()
	p.routes[hostname] = &Route{
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		Active:      true,
	}
	p.routesMu.Unlock()

	p.logger.Info("Added route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
}

// RemoveRoute removes a routing rule
func (p *MinecraftProxy) RemoveRoute(hostname string) {
	hostname = normalizeHostname(hostname)

	p.routesMu.Lock()
	delete(p.routes, hostname)
	p.routesMu.Unlock()

	p.logger.Info("Removed route: hostname=%s", hostname)
}

// UpdateRoute updates the backend for a route
func (p *MinecraftProxy) UpdateRoute(hostname, backendHost string, backendPort int) {
	hostname = normalizeHostname(hostname)

	p.routesMu.Lock()
	if route, exists := p.routes[hostname]; exists {
		route.BackendHost = backendHost
		route.BackendPort = backendPort
		p.logger.Info("Updated route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
	}
	p.routesMu.Unlock()
}

// SetRouteActive enables or disables a route
func (p *MinecraftProxy) SetRouteActive(hostname string, active bool) {
	hostname = normalizeHostname(hostname)

	p.routesMu.Lock()
	if route, exists := p.routes[hostname]; exists {
		route.Active = active
		p.logger.Info("Set route active: hostname=%s active=%v", hostname, active)
	}
	p.routesMu.Unlock()
}

// lookupRoute returns a snapshot of the active route for a hostname.
func (p *MinecraftProxy) lookupRoute(hostname string) (Route, bool) {
	p.routesMu.RLock()
	defer p.routesMu.RUnlock()
	route, exists := p.routes[hostname]
	if !exists || !route.Active {
		return Route{}, false
	}
	return *route, true
}

// GetRoutes returns a copy of all current routes
func (p *MinecraftProxy) GetRoutes() map[string]*Route {
	p.routesMu.RLock()
	defer p.routesMu.RUnlock()

	routes := make(map[string]*Route, len(p.routes))
	for k, v := range p.routes {
		routeCopy := *v
		routes[k] = &routeCopy
	}
	return routes
}

// Start starts the proxy server
func (p *MinecraftProxy) Start() error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	if p.running {
		return fmt.Errorf("proxy already running")
	}

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}

	p.listener = listener
	p.running = true

	go acceptLoop(p.ctx, listener, p.logger, p.handleConnection)

	p.logger.Info("Minecraft proxy started on %s", p.listenAddr)
	return nil
}

// Stop stops the proxy server. Established player connections are left to
// drain; only the listener is closed.
func (p *MinecraftProxy) Stop() error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	if !p.running {
		return nil
	}

	p.cancel()
	p.running = false

	if p.listener != nil {
		if err := p.listener.Close(); err != nil {
			return fmt.Errorf("failed to close listener: %w", err)
		}
	}

	p.logger.Info("Minecraft proxy stopped")
	return nil
}

// IsRunning returns whether the proxy is running
func (p *MinecraftProxy) IsRunning() bool {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return p.running
}

// handleConnection routes one client connection: parse the handshake, find
// the backend by hostname, handle sleeping servers, then relay.
func (p *MinecraftProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	br := bufio.NewReaderSize(clientConn, maxHandshakeLength)

	// Pre-1.7 clients open with a legacy ping instead of a framed handshake;
	// there is no hostname to route on, so drop them cleanly. 0xFE alone is
	// ambiguous: it also opens the VarInt length of a 254/382/... byte modern
	// handshake, so look at the third byte - a modern handshake continues
	// with packet id 0x00 there, legacy pings send 0xFA or nothing more.
	if first, err := br.Peek(1); err != nil {
		return
	} else if first[0] == legacyPingByte {
		hdr, err := br.Peek(3)
		if err != nil || hdr[2] != 0x00 {
			p.logger.Debug("Dropping legacy (pre-1.7) ping from %s", clientConn.RemoteAddr())
			return
		}
	}

	handshake, err := ReadHandshakePacket(br)
	if err != nil {
		p.logger.Debug("Failed to read handshake from %s: %v", clientConn.RemoteAddr(), err)
		return
	}

	hostname := normalizeHostname(handshake.ServerAddress)
	route, ok := p.lookupRoute(hostname)
	if !ok {
		p.logger.Debug("No active route for hostname %q from %s", hostname, clientConn.RemoteAddr())
		if handshake.NextState == NextStateLogin {
			clientConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			WriteLoginDisconnect(clientConn, fmt.Sprintf("No server is available at %s", hostname))
		}
		return
	}

	// Paused (autopaused) servers: answer status pings without waking, wake on login.
	if gate := p.getGate(); gate != nil {
		if info, sleeping := gate.SleepingInfo(route.ServerID); sleeping {
			if handshake.NextState == NextStateStatus {
				p.serveSleepingStatus(clientConn, br, handshake, info)
				return
			}
			p.logger.Info("Waking sleeping server %s for incoming login", route.ServerID)
			wakeCtx, cancel := context.WithTimeout(p.ctx, 15*time.Second)
			err := gate.WakeServer(wakeCtx, route.ServerID)
			cancel()
			if err != nil {
				p.logger.Error("Failed to wake server %s: %v", route.ServerID, err)
				clientConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				WriteLoginDisconnect(clientConn, "The server is waking up, try again in a moment")
				return
			}
		}
	}

	backendAddr := net.JoinHostPort(route.BackendHost, fmt.Sprintf("%d", route.BackendPort))
	backendConn, err := dialBackendWithRetry(p.ctx, backendAddr, 10*time.Second)
	if err != nil {
		p.logger.Error("Failed to connect to backend %s for %s: %v", backendAddr, hostname, err)
		if handshake.NextState == NextStateLogin {
			clientConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			WriteLoginDisconnect(clientConn, "The server is not accepting connections yet, try again in a moment")
		}
		return
	}
	defer backendConn.Close()

	rewriteHandshakeAddress(handshake, route.BackendPort)

	backendConn.SetWriteDeadline(time.Now().Add(handshakeTimeout))
	if err := WriteHandshakePacket(backendConn, handshake); err != nil {
		p.logger.Error("Failed to write handshake to backend %s: %v", backendAddr, err)
		return
	}

	// Bytes the client sent after the handshake (status request, login start)
	// are already buffered; flush them before handing off to the relay.
	if buffered := br.Buffered(); buffered > 0 {
		pending, _ := br.Peek(buffered)
		if _, err := backendConn.Write(pending); err != nil {
			p.logger.Error("Failed to flush buffered client data to backend %s: %v", backendAddr, err)
			return
		}
		br.Discard(buffered)
	}

	// Clear deadlines and relay raw socket to raw socket (splice fast path).
	clientConn.SetDeadline(time.Time{})
	backendConn.SetDeadline(time.Time{})
	relay(clientConn, backendConn)
}

// rewriteHandshakeAddress points the handshake at the backend while
// preserving Forge FML data appended to the address field.
func rewriteHandshakeAddress(handshake *HandshakePacket, backendPort int) {
	addressParts := strings.Split(handshake.ServerAddress, "\x00")
	addressParts[0] = "localhost"
	handshake.ServerAddress = strings.Join(addressParts, "\x00")
	handshake.ServerPort = uint16(backendPort)
}

// serveSleepingStatus answers a status handshake for a paused server with a
// synthesized response, so server-list refreshes never wake the container.
func (p *MinecraftProxy) serveSleepingStatus(conn net.Conn, r io.Reader, handshake *HandshakePacket, info *SleepingServer) {
	conn.SetDeadline(time.Now().Add(handshakeTimeout))

	statusJSON, err := json.Marshal(map[string]any{
		"version": map[string]any{
			// Echo the client protocol so the entry renders as compatible.
			"name":     "Sleeping",
			"protocol": int(handshake.ProtocolVersion),
		},
		"players": map[string]any{
			"max":    info.MaxPlayers,
			"online": 0,
		},
		"description": map[string]any{
			"text": info.MOTD,
		},
	})
	if err != nil {
		return
	}

	for {
		// Read next packet: status request (0x00) or ping (0x01).
		length, err := ReadVarInt(r)
		if err != nil || length < 1 || length > 1024 {
			return
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return
		}
		reader := bytes.NewReader(data)
		packetID, err := ReadVarInt(reader)
		if err != nil {
			return
		}

		switch packetID {
		case 0x00: // status request -> status response
			var payload bytes.Buffer
			WriteVarInt(&payload, 0x00)
			WriteVarInt(&payload, VarInt(len(statusJSON)))
			payload.Write(statusJSON)
			if err := writeFramed(conn, payload.Bytes()); err != nil {
				return
			}
		case 0x01: // ping -> pong (echo the 8-byte payload)
			var payload bytes.Buffer
			WriteVarInt(&payload, 0x01)
			pingData := make([]byte, 8)
			if _, err := io.ReadFull(reader, pingData); err != nil {
				return
			}
			payload.Write(pingData)
			writeFramed(conn, payload.Bytes())
			return
		default:
			return
		}
	}
}

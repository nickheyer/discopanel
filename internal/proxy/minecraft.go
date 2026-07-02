package proxy

import (
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

// MinecraftProxy handles Minecraft protocol proxying with handshake parsing for hostname-based routing
type MinecraftProxy struct {
	listener     net.Listener
	routes       map[string]*Route
	routesMutex  sync.RWMutex
	logger       *logger.Logger
	listenAddr   string
	running      bool
	runningMutex sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	gate         ServerGate
	gateMutex    sync.RWMutex
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
	p.gateMutex.Lock()
	p.gate = gate
	p.gateMutex.Unlock()
}

func (p *MinecraftProxy) getGate() ServerGate {
	p.gateMutex.RLock()
	defer p.gateMutex.RUnlock()
	return p.gate
}

// AddRoute adds a new routing rule
func (p *MinecraftProxy) AddRoute(serverID, hostname, backendHost string, backendPort int) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])

	p.routes[hostname] = &Route{
		ServerID:    serverID,
		Hostname:    hostname,
		BackendHost: backendHost,
		BackendPort: backendPort,
		Active:      true,
	}

	p.logger.Info("Added route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
}

// RemoveRoute removes a routing rule
func (p *MinecraftProxy) RemoveRoute(hostname string) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	delete(p.routes, hostname)

	p.logger.Info("Removed route: hostname=%s", hostname)
}

// UpdateRoute updates the backend for a route
func (p *MinecraftProxy) UpdateRoute(hostname, backendHost string, backendPort int) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.BackendHost = backendHost
		route.BackendPort = backendPort
		p.logger.Info("Updated route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
	}
}

// SetRouteActive enables or disables a route
func (p *MinecraftProxy) SetRouteActive(hostname string, active bool) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.Active = active
		p.logger.Info("Set route active: hostname=%s active=%v", hostname, active)
	}
}

// Start starts the proxy server
func (p *MinecraftProxy) Start() error {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

	if p.running {
		return fmt.Errorf("proxy already running")
	}

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}

	p.listener = listener
	p.running = true

	go p.acceptLoop()

	p.logger.Info("Minecraft proxy started on %s", p.listenAddr)
	return nil
}

// Stop stops the proxy server
func (p *MinecraftProxy) Stop() error {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

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

// acceptLoop accepts incoming connections
func (p *MinecraftProxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return
			default:
				p.logger.Error("Failed to accept connection: %v", err)
				continue
			}
		}

		go p.handleConnection(conn)
	}
}

// handleConnection handles a single client connection with Minecraft protocol parsing
func (p *MinecraftProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	p.logger.Debug("Attempting to route incoming Minecraft connection!")

	// Set initial timeout for handshake
	clientConn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Read the handshake packet
	handshake, err := ReadHandshakePacket(clientConn)
	if err != nil {
		p.logger.Debug("Failed to read handshake from %s: %v", clientConn.RemoteAddr(), err)
		return
	}

	// Extract hostname from the handshake
	p.logger.Debug("Extracting hostname from: %s", handshake.ServerAddress)
	hostname := strings.ToLower(strings.Split(handshake.ServerAddress, ":")[0])
	if idx := strings.IndexByte(hostname, 0); idx != -1 {
		hostname = hostname[:idx]
		p.logger.Debug("Null byte(s) detected, trimmed suffix null termination: %s", hostname)
	}

	// Find the route
	p.routesMutex.RLock()
	route, exists := p.routes[hostname]
	p.routesMutex.RUnlock()

	if !exists || !route.Active {
		p.logger.Debug("No active route found for hostname: %s", hostname)
		p.routesMutex.RLock()
		p.logger.Debug("Available routes:")
		for r := range p.routes {
			p.logger.Debug("%s", r)
		}
		p.routesMutex.RUnlock()
		return
	}

	// Paused (autopaused) servers: answer status pings without waking, wake on login.
	if gate := p.getGate(); gate != nil {
		if info, sleeping := gate.SleepingInfo(route.ServerID); sleeping {
			if handshake.NextState == 1 {
				p.serveSleepingStatus(clientConn, handshake, info)
				return
			}
			p.logger.Info("Waking sleeping server %s for incoming login", route.ServerID)
			wakeCtx, cancel := context.WithTimeout(p.ctx, 15*time.Second)
			err := gate.WakeServer(wakeCtx, route.ServerID)
			cancel()
			if err != nil {
				p.logger.Error("Failed to wake server %s: %v", route.ServerID, err)
				return
			}
		}
	}

	// Connect to backend
	backendAddr := net.JoinHostPort(route.BackendHost, fmt.Sprintf("%d", route.BackendPort))
	backendConn, err := dialBackendWithRetry(backendAddr, 10*time.Second)
	if err != nil {
		p.logger.Error("Failed to connect to backend %s: %v", backendAddr, err)
		return
	}
	defer backendConn.Close()

	// Modify handshake packet to use backend's expected hostname
	// For Forge servers, we need to preserve any FML data in the address field
	addressParts := strings.Split(handshake.ServerAddress, "\x00")
	if len(addressParts) > 1 {
		// Forge client detected - preserve all FML protocol data
		originalHost := addressParts[0]
		addressParts[0] = "localhost"

		if len(addressParts) >= 2 {
			fmlVersion := addressParts[1]
			p.logger.Debug("Forge handshake detected - FML version: %s, original host: %s", fmlVersion, originalHost)

			if len(addressParts) > 2 {
				p.logger.Debug("Additional FML data segments: %d", len(addressParts)-2)
			}
		}

		handshake.ServerAddress = strings.Join(addressParts, "\x00")
	} else {
		handshake.ServerAddress = "localhost"
	}
	handshake.ServerPort = uint16(route.BackendPort)

	// Forward the modified handshake to the backend
	if err := WriteHandshakePacket(backendConn, handshake); err != nil {
		p.logger.Error("Failed to write handshake to backend: %v", err)
		return
	}

	// Clear timeouts for proxying
	clientConn.SetReadDeadline(time.Time{})
	backendConn.SetReadDeadline(time.Time{})

	// Start bidirectional proxying
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(backendConn, clientConn)
		backendConn.Close()
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, backendConn)
		clientConn.Close()
	}()

	wg.Wait()
}

// GetRoutes returns a copy of all current routes
func (p *MinecraftProxy) GetRoutes() map[string]*Route {
	p.routesMutex.RLock()
	defer p.routesMutex.RUnlock()

	routes := make(map[string]*Route)
	for k, v := range p.routes {
		routeCopy := *v
		routes[k] = &routeCopy
	}
	return routes
}

// IsRunning returns whether the proxy is running
func (p *MinecraftProxy) IsRunning() bool {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()
	return p.running
}

// dialBackendWithRetry dials a backend, retrying briefly. A just-woken
// container needs a moment before the JVM accepts connections again.
func dialBackendWithRetry(addr string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, lastErr
		}
		dialTimeout := 5 * time.Second
		if remaining < dialTimeout {
			dialTimeout = remaining
		}
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		time.Sleep(250 * time.Millisecond)
	}
}

// serveSleepingStatus answers a status handshake for a paused server with a
// synthesized response, so server-list refreshes never wake the container.
func (p *MinecraftProxy) serveSleepingStatus(conn net.Conn, handshake *HandshakePacket, info *SleepingServer) {
	conn.SetDeadline(time.Now().Add(10 * time.Second))

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
		length, err := ReadVarInt(conn)
		if err != nil || length < 1 || length > 1024 {
			return
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(conn, data); err != nil {
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

// writeFramed writes a length-prefixed Minecraft packet.
func writeFramed(w io.Writer, data []byte) error {
	var buf bytes.Buffer
	if err := WriteVarInt(&buf, VarInt(len(data))); err != nil {
		return err
	}
	buf.Write(data)
	_, err := w.Write(buf.Bytes())
	return err
}

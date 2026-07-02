package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// TCPProxy forwards raw TCP to a single backend without protocol parsing
// (module ports get a dedicated listener each, so no routing is needed).
type TCPProxy struct {
	listenAddr string
	logger     *logger.Logger

	backendHost string
	backendPort int
	serverID    string

	listener net.Listener
	running  bool
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewTCPProxy creates a new raw TCP proxy instance
func NewTCPProxy(cfg *Config) *TCPProxy {
	ctx, cancel := context.WithCancel(context.Background())
	return &TCPProxy{
		logger:     cfg.Logger,
		listenAddr: cfg.ListenAddr,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// AddRoute sets the backend for TCP proxy (only one route supported, like UDP)
func (p *TCPProxy) AddRoute(serverID, hostname, backendHost string, backendPort int) {
	p.mu.Lock()
	p.serverID = serverID
	p.backendHost = backendHost
	p.backendPort = backendPort
	p.mu.Unlock()
	p.logger.Info("TCP proxy route set: %s -> %s:%d", p.listenAddr, backendHost, backendPort)
}

// RemoveRoute removes the backend route
func (p *TCPProxy) RemoveRoute(hostname string) {
	p.mu.Lock()
	p.serverID = ""
	p.backendHost = ""
	p.backendPort = 0
	p.mu.Unlock()
	p.logger.Info("TCP proxy route removed: %s", p.listenAddr)
}

// UpdateRoute updates the backend address
func (p *TCPProxy) UpdateRoute(hostname, backendHost string, backendPort int) {
	p.mu.Lock()
	p.backendHost = backendHost
	p.backendPort = backendPort
	p.mu.Unlock()
	p.logger.Info("TCP proxy route updated: %s -> %s:%d", p.listenAddr, backendHost, backendPort)
}

// GetRoutes returns the current route (TCP only has one)
func (p *TCPProxy) GetRoutes() map[string]*Route {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.backendHost == "" {
		return make(map[string]*Route)
	}

	return map[string]*Route{
		"tcp": {
			ServerID:    p.serverID,
			Hostname:    "tcp",
			BackendHost: p.backendHost,
			BackendPort: p.backendPort,
			Active:      true,
		},
	}
}

// Start starts the TCP proxy server
func (p *TCPProxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("TCP proxy already running")
	}

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}

	p.listener = listener
	p.running = true

	go acceptLoop(p.ctx, listener, p.logger, p.handleConnection)

	p.logger.Info("TCP proxy started on %s", p.listenAddr)
	return nil
}

// Stop stops the TCP proxy server
func (p *TCPProxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

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

	p.logger.Info("TCP proxy stopped")
	return nil
}

// IsRunning returns whether the proxy is running
func (p *TCPProxy) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// handleConnection relays a single client connection to the backend.
func (p *TCPProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	p.mu.RLock()
	backendHost := p.backendHost
	backendPort := p.backendPort
	p.mu.RUnlock()

	if backendHost == "" || backendPort == 0 {
		p.logger.Debug("No backend configured for TCP proxy")
		return
	}

	backendAddr := net.JoinHostPort(backendHost, fmt.Sprintf("%d", backendPort))
	backendConn, err := dialBackend(p.ctx, backendAddr)
	if err != nil {
		p.logger.Error("Failed to connect to backend %s: %v", backendAddr, err)
		return
	}
	defer backendConn.Close()

	p.logger.Debug("TCP connection established: %s -> %s", clientConn.RemoteAddr(), backendAddr)
	relay(clientConn, backendConn)
}

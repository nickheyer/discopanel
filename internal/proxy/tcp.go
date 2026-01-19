package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// TCPProxy handles raw TCP forwarding without protocol parsing
type TCPProxy struct {
	listener     net.Listener
	backendHost  string
	backendPort  int
	serverID     string
	logger       *logger.Logger
	listenAddr   string
	running      bool
	runningMutex sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
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
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()
	p.serverID = serverID
	p.backendHost = backendHost
	p.backendPort = backendPort
	p.logger.Info("TCP proxy route set: %s -> %s:%d", p.listenAddr, backendHost, backendPort)
}

// RemoveRoute removes the backend route
func (p *TCPProxy) RemoveRoute(hostname string) {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()
	p.serverID = ""
	p.backendHost = ""
	p.backendPort = 0
	p.logger.Info("TCP proxy route removed: %s", p.listenAddr)
}

// UpdateRoute updates the backend address
func (p *TCPProxy) UpdateRoute(hostname, backendHost string, backendPort int) {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()
	p.backendHost = backendHost
	p.backendPort = backendPort
	p.logger.Info("TCP proxy route updated: %s -> %s:%d", p.listenAddr, backendHost, backendPort)
}

// GetRoutes returns the current route (TCP only has one)
func (p *TCPProxy) GetRoutes() map[string]*Route {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()

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
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

	if p.running {
		return fmt.Errorf("TCP proxy already running")
	}

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}

	p.listener = listener
	p.running = true

	go p.acceptLoop()

	p.logger.Info("TCP proxy started on %s", p.listenAddr)
	return nil
}

// Stop stops the TCP proxy server
func (p *TCPProxy) Stop() error {
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

	p.logger.Info("TCP proxy stopped")
	return nil
}

// IsRunning returns whether the proxy is running
func (p *TCPProxy) IsRunning() bool {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()
	return p.running
}

// acceptLoop accepts incoming connections
func (p *TCPProxy) acceptLoop() {
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

// handleConnection handles a single client connection with raw forwarding
func (p *TCPProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	p.runningMutex.RLock()
	backendHost := p.backendHost
	backendPort := p.backendPort
	p.runningMutex.RUnlock()

	if backendHost == "" || backendPort == 0 {
		p.logger.Debug("No backend configured for TCP proxy")
		return
	}

	// Connect to backend
	backendAddr := net.JoinHostPort(backendHost, fmt.Sprintf("%d", backendPort))
	backendConn, err := net.DialTimeout("tcp", backendAddr, 5*time.Second)
	if err != nil {
		p.logger.Error("Failed to connect to backend %s: %v", backendAddr, err)
		return
	}
	defer backendConn.Close()

	p.logger.Debug("TCP connection established: %s -> %s", clientConn.RemoteAddr(), backendAddr)

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

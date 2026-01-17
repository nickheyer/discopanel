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

// Forwarder is a simple TCP/UDP forwarder - no protocol detection, just forward traffic
type Forwarder struct {
	tcpListener net.Listener
	udpConn     *net.UDPConn
	backendAddr string
	logger      *logger.Logger
	listenAddr  string
	protocol    string // "tcp", "udp", or "both"
	running     bool
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// ForwarderConfig holds forwarder configuration
type ForwarderConfig struct {
	ListenAddr  string
	BackendAddr string
	Protocol    string // "tcp", "udp", or "both"
	Logger      *logger.Logger
}

// NewForwarder creates a new TCP/UDP forwarder
func NewForwarder(cfg *ForwarderConfig) *Forwarder {
	ctx, cancel := context.WithCancel(context.Background())
	protocol := cfg.Protocol
	if protocol == "" {
		protocol = "tcp"
	}
	return &Forwarder{
		listenAddr:  cfg.ListenAddr,
		backendAddr: cfg.BackendAddr,
		protocol:    protocol,
		logger:      cfg.Logger,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetBackend updates the backend address
func (f *Forwarder) SetBackend(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backendAddr = addr
	f.logger.Info("Updated forwarder backend to %s", addr)
}

// Start starts the forwarder
func (f *Forwarder) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.running {
		return fmt.Errorf("forwarder already running")
	}

	if f.protocol == "tcp" || f.protocol == "both" {
		listener, err := net.Listen("tcp", f.listenAddr)
		if err != nil {
			return fmt.Errorf("failed to listen TCP on %s: %w", f.listenAddr, err)
		}
		f.tcpListener = listener
		go f.acceptTCP()
	}

	if f.protocol == "udp" || f.protocol == "both" {
		addr, err := net.ResolveUDPAddr("udp", f.listenAddr)
		if err != nil {
			return fmt.Errorf("failed to resolve UDP address %s: %w", f.listenAddr, err)
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen UDP on %s: %w", f.listenAddr, err)
		}
		f.udpConn = conn
		go f.forwardUDP()
	}

	f.running = true
	f.logger.Info("Forwarder started on %s (%s) -> %s", f.listenAddr, f.protocol, f.backendAddr)
	return nil
}

// Stop stops the forwarder
func (f *Forwarder) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.running {
		return nil
	}

	f.cancel()
	f.running = false

	if f.tcpListener != nil {
		f.tcpListener.Close()
	}
	if f.udpConn != nil {
		f.udpConn.Close()
	}

	f.logger.Info("Forwarder stopped")
	return nil
}

func (f *Forwarder) acceptTCP() {
	for {
		conn, err := f.tcpListener.Accept()
		if err != nil {
			select {
			case <-f.ctx.Done():
				return
			default:
				f.logger.Error("Failed to accept TCP connection: %v", err)
				continue
			}
		}
		go f.handleTCP(conn)
	}
}

func (f *Forwarder) handleTCP(clientConn net.Conn) {
	defer clientConn.Close()

	f.mu.RLock()
	backendAddr := f.backendAddr
	f.mu.RUnlock()

	if backendAddr == "" {
		return
	}

	backendConn, err := net.DialTimeout("tcp", backendAddr, 5*time.Second)
	if err != nil {
		f.logger.Error("Failed to connect to TCP backend %s: %v", backendAddr, err)
		return
	}
	defer backendConn.Close()

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

func (f *Forwarder) forwardUDP() {
	buf := make([]byte, 65535)

	// Map to track client addresses for response routing
	clients := make(map[string]*net.UDPConn)
	var clientsMu sync.RWMutex

	for {
		select {
		case <-f.ctx.Done():
			return
		default:
		}

		n, clientAddr, err := f.udpConn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-f.ctx.Done():
				return
			default:
				continue
			}
		}

		f.mu.RLock()
		backendAddr := f.backendAddr
		f.mu.RUnlock()

		if backendAddr == "" {
			continue
		}

		// Get or create backend connection for this client
		clientKey := clientAddr.String()
		clientsMu.RLock()
		backendConn, exists := clients[clientKey]
		clientsMu.RUnlock()

		if !exists {
			addr, err := net.ResolveUDPAddr("udp", backendAddr)
			if err != nil {
				continue
			}
			backendConn, err = net.DialUDP("udp", nil, addr)
			if err != nil {
				continue
			}

			clientsMu.Lock()
			clients[clientKey] = backendConn
			clientsMu.Unlock()

			// Start goroutine to forward responses back to client
			go func(clientAddr *net.UDPAddr, backendConn *net.UDPConn) {
				defer func() {
					clientsMu.Lock()
					delete(clients, clientKey)
					clientsMu.Unlock()
					backendConn.Close()
				}()

				respBuf := make([]byte, 65535)
				backendConn.SetReadDeadline(time.Now().Add(30 * time.Second))

				for {
					n, err := backendConn.Read(respBuf)
					if err != nil {
						return
					}
					backendConn.SetReadDeadline(time.Now().Add(30 * time.Second))
					f.udpConn.WriteToUDP(respBuf[:n], clientAddr)
				}
			}(clientAddr, backendConn)
		}

		// Forward packet to backend
		backendConn.Write(buf[:n])
	}
}

// IsRunning returns whether the forwarder is running
func (f *Forwarder) IsRunning() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.running
}

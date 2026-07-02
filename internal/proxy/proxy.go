// Package proxy implements DiscoPanel's ingress layer: hostname-routed
// Minecraft proxies on shared listener ports, plus raw TCP/UDP/HTTP
// forwarders for module ports. All proxies relay with TCP half-close
// propagation and use the kernel splice fast path (io.Copy over *net.TCPConn)
// so steady-state throughput is not userspace-bound.
package proxy

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// Proxier is the interface for all proxy types (TCP, UDP, Minecraft, HTTP)
type Proxier interface {
	Start() error
	Stop() error
	AddRoute(serverID, hostname, backendHost string, backendPort int)
	RemoveRoute(hostname string)
	UpdateRoute(hostname, backendHost string, backendPort int)
	GetRoutes() map[string]*Route
	IsRunning() bool
}

// Route represents a routing rule from hostname to backend server
type Route struct {
	ServerID    string
	Hostname    string
	BackendHost string
	BackendPort int
	Active      bool
}

// SleepingServer carries the data needed to answer a status ping for a
// paused (autopaused) server without waking it.
type SleepingServer struct {
	MOTD       string
	MaxPlayers int
}

// ServerGate lets the proxy interact with paused servers: status pings get a
// synthesized "sleeping" response, logins wake the container.
type ServerGate interface {
	// SleepingInfo returns sleeping-status data when the server is paused.
	SleepingInfo(serverID string) (*SleepingServer, bool)
	// WakeServer resumes a paused server so an incoming login can proceed.
	WakeServer(ctx context.Context, serverID string) error
}

// Config holds proxy configuration
type Config struct {
	ListenAddr string // Address to listen on (e.g., ":25565" or ":8080")
	Logger     *logger.Logger
	Gate       ServerGate
}

const (
	// handshakeTimeout bounds how long a client may take to complete the
	// initial routing handshake before the connection is dropped.
	handshakeTimeout = 10 * time.Second

	// backendDialTimeout bounds a single backend connection attempt.
	backendDialTimeout = 5 * time.Second

	// halfCloseGrace is how long the second relay direction may continue
	// after the first direction finished (TCP half-close drain window).
	halfCloseGrace = 60 * time.Second
)

// relay copies data bidirectionally between two connections, propagating EOF
// as a TCP half-close so in-flight data in the opposite direction is never
// truncated. Whichever direction finishes first, the other gets
// halfCloseGrace to drain before both connections are force-unblocked, so a
// peer that ignores the half-close can never pin the relay forever. io.Copy
// dispatches to splice(2) when both ends are *net.TCPConn, so the
// steady-state data path stays in the kernel.
func relay(client, backend net.Conn) {
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src)
		closeWrite(dst)
		done <- struct{}{}
	}
	go pipe(backend, client)
	go pipe(client, backend)

	<-done
	timer := time.AfterFunc(halfCloseGrace, func() {
		now := time.Now()
		client.SetDeadline(now)
		backend.SetDeadline(now)
	})
	<-done
	timer.Stop()
}

// acceptLoop accepts connections until the listener closes or ctx is
// canceled, dispatching each connection to handle in its own goroutine.
// Persistent accept errors (closed fd, resource exhaustion) back off briefly
// instead of hot-looping.
func acceptLoop(ctx context.Context, listener net.Listener, log *logger.Logger, handle func(net.Conn)) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			log.Error("Failed to accept connection: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		go handle(conn)
	}
}

// closeWrite half-closes the write side when the transport supports it,
// falling back to a full close.
func closeWrite(conn net.Conn) {
	type closeWriter interface{ CloseWrite() error }
	if cw, ok := conn.(closeWriter); ok {
		cw.CloseWrite()
		return
	}
	conn.Close()
}

// dialBackend connects to a backend with a bounded timeout and keep-alive.
func dialBackend(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: backendDialTimeout, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}

// dialBackendWithRetry dials a backend, retrying until the deadline. A
// just-woken or just-started container needs a moment before the JVM accepts
// connections. Each attempt is clamped to the remaining budget so the total
// never overshoots timeout.
func dialBackendWithRetry(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, lastErr
		}
		d := net.Dialer{Timeout: min(backendDialTimeout, remaining), KeepAlive: 30 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

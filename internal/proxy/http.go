package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nickheyer/discopanel/pkg/logger"
)

// HTTPProxy handles HTTP reverse proxying with Host header based routing
type HTTPProxy struct {
	server       *http.Server
	routes       map[string]*Route
	routesMutex  sync.RWMutex
	logger       *logger.Logger
	listenAddr   string
	running      bool
	runningMutex sync.RWMutex
}

// NewHTTPProxy creates a new HTTP reverse proxy instance
func NewHTTPProxy(cfg *Config) *HTTPProxy {
	p := &HTTPProxy{
		routes:     make(map[string]*Route),
		logger:     cfg.Logger,
		listenAddr: cfg.ListenAddr,
	}

	p.server = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: p,
	}

	return p
}

// isWebSocketRequest checks if this is a WebSocket upgrade request
func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// ServeHTTP implements http.Handler for routing requests
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract hostname from Host header
	hostname := strings.ToLower(strings.Split(r.Host, ":")[0])

	p.logger.Debug("HTTP request: %s %s Host: %s", r.Method, r.URL.Path, hostname)

	// Find the route
	p.routesMutex.RLock()
	route, exists := p.routes[hostname]
	p.routesMutex.RUnlock()

	if !exists || !route.Active {
		p.logger.Debug("No active route found for hostname: %s", hostname)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Handle WebSocket upgrade separately
	if isWebSocketRequest(r) {
		p.handleWebSocket(w, r, route)
		return
	}

	// Regular HTTP request - use reverse proxy
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", route.BackendHost, route.BackendPort),
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = r.Host
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		p.logger.Error("Proxy error for %s: %v", hostname, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, r)
}

// handleWebSocket handles WebSocket upgrade requests
func (p *HTTPProxy) handleWebSocket(w http.ResponseWriter, r *http.Request, route *Route) {
	// Hijack the client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		p.logger.Error("WebSocket: ResponseWriter doesn't support hijacking")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		p.logger.Error("WebSocket: Failed to hijack connection: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Connect to backend
	backendAddr := net.JoinHostPort(route.BackendHost, fmt.Sprintf("%d", route.BackendPort))
	backendConn, err := net.DialTimeout("tcp", backendAddr, 5*time.Second)
	if err != nil {
		p.logger.Error("WebSocket: Failed to connect to backend %s: %v", backendAddr, err)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer backendConn.Close()

	// Forward the original HTTP upgrade request to backend
	if err := r.Write(backendConn); err != nil {
		p.logger.Error("WebSocket: Failed to forward upgrade request: %v", err)
		return
	}

	p.logger.Debug("WebSocket connection established: %s -> %s", r.RemoteAddr, backendAddr)

	// Bidirectional copy
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

// AddRoute adds a new routing rule
func (p *HTTPProxy) AddRoute(serverID, hostname, backendHost string, backendPort int) {
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

	p.logger.Info("HTTP proxy added route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
}

// RemoveRoute removes a routing rule
func (p *HTTPProxy) RemoveRoute(hostname string) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	delete(p.routes, hostname)

	p.logger.Info("HTTP proxy removed route: hostname=%s", hostname)
}

// UpdateRoute updates the backend for a route
func (p *HTTPProxy) UpdateRoute(hostname, backendHost string, backendPort int) {
	p.routesMutex.Lock()
	defer p.routesMutex.Unlock()

	hostname = strings.ToLower(strings.Split(hostname, ":")[0])
	if route, exists := p.routes[hostname]; exists {
		route.BackendHost = backendHost
		route.BackendPort = backendPort
		p.logger.Info("HTTP proxy updated route: hostname=%s backend=%s:%d", hostname, backendHost, backendPort)
	}
}

// GetRoutes returns a copy of all current routes
func (p *HTTPProxy) GetRoutes() map[string]*Route {
	p.routesMutex.RLock()
	defer p.routesMutex.RUnlock()

	routes := make(map[string]*Route)
	for k, v := range p.routes {
		routeCopy := *v
		routes[k] = &routeCopy
	}
	return routes
}

// Start starts the HTTP proxy server
func (p *HTTPProxy) Start() error {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

	if p.running {
		return fmt.Errorf("HTTP proxy already running")
	}

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}

	p.running = true

	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.logger.Error("HTTP proxy error: %v", err)
		}
	}()

	p.logger.Info("HTTP proxy started on %s", p.listenAddr)
	return nil
}

// Stop stops the HTTP proxy server
func (p *HTTPProxy) Stop() error {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()

	if !p.running {
		return nil
	}

	p.running = false

	if err := p.server.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("failed to shutdown HTTP proxy: %w", err)
	}

	p.logger.Info("HTTP proxy stopped")
	return nil
}

// IsRunning returns whether the proxy is running
func (p *HTTPProxy) IsRunning() bool {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()
	return p.running
}

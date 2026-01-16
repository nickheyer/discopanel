package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// HTTPRequest holds parsed HTTP request data needed for routing
type HTTPRequest struct {
	Method  string
	Path    string
	Host    string
	Headers map[string]string
	RawData []byte // The raw request data to forward
}

// ParseHTTPRequest parses enough of an HTTP request to extract routing info
// It reads from a PeekingConn and returns the parsed request
func ParseHTTPRequest(conn *PeekingConn) (*HTTPRequest, error) {
	// Peek enough bytes to find the end of headers (max 8KB)
	const maxHeaderSize = 8192
	data, err := conn.Peek(maxHeaderSize)
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("failed to peek HTTP headers: %w", err)
	}

	// Find end of headers (\r\n\r\n)
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		// Headers incomplete, try with what we have
		headerEnd = len(data)
	}

	headerData := data[:headerEnd]
	lines := bytes.Split(headerData, []byte("\r\n"))
	if len(lines) == 0 {
		return nil, fmt.Errorf("invalid HTTP request: no request line")
	}

	// Parse request line
	requestLine := string(lines[0])
	parts := strings.SplitN(requestLine, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid HTTP request line: %s", requestLine)
	}

	req := &HTTPRequest{
		Method:  parts[0],
		Path:    parts[1],
		Headers: make(map[string]string),
	}

	// Parse headers
	for _, line := range lines[1:] {
		if len(line) == 0 {
			continue
		}
		colonIdx := bytes.IndexByte(line, ':')
		if colonIdx == -1 {
			continue
		}
		key := strings.ToLower(string(bytes.TrimSpace(line[:colonIdx])))
		value := string(bytes.TrimSpace(line[colonIdx+1:]))
		req.Headers[key] = value
	}

	// Extract Host header
	if host, ok := req.Headers["host"]; ok {
		// Remove port from host if present
		if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
			req.Host = host[:colonIdx]
		} else {
			req.Host = host
		}
	}

	return req, nil
}

// handleHTTP handles HTTP protocol connections by routing based on Host header
func (p *Proxy) handleHTTP(conn *PeekingConn) {
	// Parse HTTP request to extract Host header
	httpReq, err := ParseHTTPRequest(conn)
	if err != nil {
		p.logger.Debug("Failed to parse HTTP request from %s: %v", conn.RemoteAddr(), err)
		p.sendHTTPError(conn, 400, "Bad Request")
		return
	}

	if httpReq.Host == "" {
		p.logger.Debug("HTTP request missing Host header from %s", conn.RemoteAddr())
		p.sendHTTPError(conn, 400, "Bad Request - Missing Host Header")
		return
	}

	hostname := strings.ToLower(httpReq.Host)

	// Look up route by hostname
	p.routesMutex.RLock()
	route, exists := p.routes[hostname]
	p.routesMutex.RUnlock()

	if !exists || !route.Active {
		p.logger.Debug("No route for HTTP hostname: %s", hostname)
		p.sendHTTPError(conn, 404, "Not Found")
		return
	}

	// Check if there's an HTTP backend for this route
	if route.HTTPBackend == nil || !route.HTTPBackend.Active {
		p.logger.Debug("No HTTP backend for hostname: %s", hostname)
		p.sendHTTPError(conn, 503, "Service Unavailable - No HTTP backend configured")
		return
	}

	// Connect to HTTP backend
	backend := route.HTTPBackend
	backendAddr := net.JoinHostPort(backend.BackendHost, fmt.Sprintf("%d", backend.BackendPort))
	backendConn, err := net.DialTimeout("tcp", backendAddr, 5*time.Second)
	if err != nil {
		p.logger.Error("Failed to connect to HTTP backend %s: %v", backendAddr, err)
		p.sendHTTPError(conn, 502, "Bad Gateway")
		return
	}
	defer backendConn.Close()

	// Clear timeouts for proxying
	conn.SetReadDeadline(time.Time{})
	backendConn.SetReadDeadline(time.Time{})

	// Proxy the connection bidirectionally
	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Backend
	go func() {
		defer wg.Done()
		io.Copy(backendConn, conn)
		backendConn.Close()
	}()

	// Backend -> Client
	go func() {
		defer wg.Done()
		io.Copy(conn, backendConn)
		conn.Close()
	}()

	wg.Wait()
}

// sendHTTPError sends an HTTP error response to the client
func (p *Proxy) sendHTTPError(conn net.Conn, code int, message string) {
	statusText := getHTTPStatusText(code)
	body := fmt.Sprintf("%d %s\n%s\n", code, statusText, message)
	response := fmt.Sprintf(
		"HTTP/1.1 %d %s\r\n"+
			"Content-Type: text/plain\r\n"+
			"Content-Length: %d\r\n"+
			"Connection: close\r\n"+
			"\r\n"+
			"%s",
		code, statusText, len(body), body,
	)
	conn.Write([]byte(response))
}

// getHTTPStatusText returns the status text for an HTTP status code
func getHTTPStatusText(code int) string {
	switch code {
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	default:
		return "Error"
	}
}

// ReadHandshakePacketFromReader reads a Minecraft handshake packet from a buffered reader
// This is used after protocol detection when the connection is already wrapped
func ReadHandshakePacketFromReader(r *bufio.Reader) (*HandshakePacket, error) {
	return ReadHandshakePacket(r)
}

package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net"
)

// Protocol represents a detected protocol type
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolMinecraft
	ProtocolHTTP
)

func (p Protocol) String() string {
	switch p {
	case ProtocolMinecraft:
		return "minecraft"
	case ProtocolHTTP:
		return "http"
	default:
		return "unknown"
	}
}

// PeekingConn wraps a net.Conn with buffered peeking capability
// This allows us to peek at the first bytes without consuming them
type PeekingConn struct {
	net.Conn
	reader *bufio.Reader
}

// NewPeekingConn creates a new connection wrapper with peek capability
func NewPeekingConn(conn net.Conn) *PeekingConn {
	return &PeekingConn{
		Conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

// Peek returns the next n bytes without advancing the reader
func (pc *PeekingConn) Peek(n int) ([]byte, error) {
	return pc.reader.Peek(n)
}

// Read reads from the buffered reader (preserves peeked data)
func (pc *PeekingConn) Read(p []byte) (int, error) {
	return pc.reader.Read(p)
}

// Reader returns the underlying buffered reader for use with protocol parsers
func (pc *PeekingConn) Reader() io.Reader {
	return pc.reader
}

// HTTP method prefixes for detection
// HTTP requests start with method names followed by a space
var httpMethods = [][]byte{
	[]byte("GET "),
	[]byte("PUT "),
	[]byte("POST "),
	[]byte("HEAD "),
	[]byte("PATCH "),
	[]byte("DELETE "),
	[]byte("OPTIONS "),
	[]byte("CONNECT "),
}

// DetectProtocol identifies the protocol from the connection by peeking at the first bytes
// HTTP requests start with ASCII method names (GET, POST, etc.)
// Minecraft handshakes start with a VarInt packet length (typically 0x00-0x7F for small packets)
func DetectProtocol(conn *PeekingConn) (Protocol, error) {
	// Peek enough bytes for the longest HTTP method (OPTIONS = 8 bytes including space)
	data, err := conn.Peek(8)
	if err != nil && len(data) == 0 {
		return ProtocolUnknown, err
	}

	// Check for HTTP methods
	for _, method := range httpMethods {
		if len(data) >= len(method) && bytes.HasPrefix(data, method) {
			return ProtocolHTTP, nil
		}
	}

	// Default to Minecraft - the existing protocol
	// Minecraft handshakes start with VarInt (packet length), which for typical
	// handshake packets is in the range 0x0A-0x50 (10-80 bytes), which doesn't
	// overlap with HTTP method first characters (G=0x47, P=0x50, H=0x48, etc.)
	return ProtocolMinecraft, nil
}

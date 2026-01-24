package minecraft

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Implements the Minecraft Server List Ping protocol
type SLPClient struct {
	timeout time.Duration
}

// Parsed response from a server list ping
type SLPResult struct {
	Version struct {
		Name     string `json:"name"`
		Protocol int    `json:"protocol"`
	} `json:"version"`
	Players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
		Sample []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"sample"`
	} `json:"players"`
	Description    json.RawMessage `json:"description"`
	Favicon        string          `json:"favicon,omitempty"`
	EnforcesSecure bool            `json:"enforcesSecureChat,omitempty"`
	LatencyMs      int64
	MOTD           string   // Parsed from Description
	PlayerNames    []string // Extracted player names
}

// SLP client with timeout
func NewSLPClient(timeout time.Duration) *SLPClient {
	return &SLPClient{
		timeout: timeout,
	}
}

// Server list ping to host and port
func (c *SLPClient) Ping(ctx context.Context, host string, port int, mcVersion string) (*SLPResult, error) {
	// Create connection
	var d net.Dialer
	d.Timeout = c.timeout

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.timeout)
	}

	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Get protocol version for the MC version
	protocolVersion := GetProtocolVersion(mcVersion)

	// Send handshake packet (NextState = 1 for status)
	if err := c.sendHandshake(conn, host, port, protocolVersion); err != nil {
		return nil, fmt.Errorf("failed to send handshake: %w", err)
	}

	// Send status request
	if err := c.sendStatusRequest(conn); err != nil {
		return nil, fmt.Errorf("failed to send status request: %w", err)
	}

	// Read status response
	jsonPayload, err := c.readStatusResponse(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read status response: %w", err)
	}

	// Send ping with timestamp
	pingTime := time.Now()
	pingPayload := pingTime.UnixMilli()
	if err := c.sendPing(conn, pingPayload); err != nil {
		return nil, fmt.Errorf("failed to send ping: %w", err)
	}

	// Read pong response
	pongPayload, err := c.readPong(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read pong: %w", err)
	}

	latency := time.Since(pingTime).Milliseconds()

	// Verify pong payload matches
	if pongPayload != pingPayload {
		// Some servers don't echo the payload correctly, just log and continue
		latency = time.Since(pingTime).Milliseconds()
	}

	// Parse JSON response
	var result SLPResult
	if err := json.Unmarshal([]byte(jsonPayload), &result); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	result.LatencyMs = latency
	result.MOTD = parseDescription(result.Description)

	// Extract player names
	for _, player := range result.Players.Sample {
		result.PlayerNames = append(result.PlayerNames, player.Name)
	}

	return &result, nil
}

// Send handshake packet
func (c *SLPClient) sendHandshake(conn net.Conn, host string, port int, protocolVersion int) error {
	var buf bytes.Buffer

	// Packet ID (0x00 for handshake)
	writeVarInt(&buf, 0x00)

	// Protocol version
	writeVarInt(&buf, int32(protocolVersion))

	// Server address (string)
	writeString(&buf, host)

	// Server port (unsigned short, big endian)
	if err := binary.Write(&buf, binary.BigEndian, uint16(port)); err != nil {
		return err
	}

	// Next state (1 for status)
	writeVarInt(&buf, 1)

	// Send packet with length prefix
	return c.sendPacket(conn, buf.Bytes())
}

// Send empty status request packet
func (c *SLPClient) sendStatusRequest(conn net.Conn) error {
	var buf bytes.Buffer
	// Packet ID (0x00 for status request)
	writeVarInt(&buf, 0x00)
	return c.sendPacket(conn, buf.Bytes())
}

// Read and parse status response
func (c *SLPClient) readStatusResponse(conn net.Conn) (string, error) {
	// Read packet length
	packetLen, err := readVarInt(conn)
	if err != nil {
		return "", fmt.Errorf("failed to read packet length: %w", err)
	}

	if packetLen < 1 || packetLen > 1024*1024 { // Max 1MB for safety
		return "", fmt.Errorf("invalid packet length: %d", packetLen)
	}

	// Read packet data
	data := make([]byte, packetLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return "", fmt.Errorf("failed to read packet data: %w", err)
	}

	reader := bytes.NewReader(data)

	// Read packet ID (should be 0x00)
	packetID, err := readVarInt(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read packet ID: %w", err)
	}

	if packetID != 0x00 {
		return "", fmt.Errorf("unexpected packet ID: %d", packetID)
	}

	// Read JSON string
	jsonStr, err := readString(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read JSON string: %w", err)
	}

	return jsonStr, nil
}

// Send ping packet with payload
func (c *SLPClient) sendPing(conn net.Conn, payload int64) error {
	var buf bytes.Buffer
	// Packet ID (0x01 for ping)
	writeVarInt(&buf, 0x01)
	// Payload (8 bytes, big endian)
	if err := binary.Write(&buf, binary.BigEndian, payload); err != nil {
		return err
	}
	return c.sendPacket(conn, buf.Bytes())
}

// Read pong response and return payload
func (c *SLPClient) readPong(conn net.Conn) (int64, error) {
	// Read packet length
	packetLen, err := readVarInt(conn)
	if err != nil {
		return 0, fmt.Errorf("failed to read packet length: %w", err)
	}

	if packetLen != 9 { // 1 byte VarInt (0x01) + 8 bytes payload
		return 0, fmt.Errorf("unexpected pong packet length: %d", packetLen)
	}

	// Read packet data
	data := make([]byte, packetLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return 0, fmt.Errorf("failed to read packet data: %w", err)
	}

	reader := bytes.NewReader(data)

	// Read packet ID (should be 0x01)
	packetID, err := readVarInt(reader)
	if err != nil {
		return 0, fmt.Errorf("failed to read packet ID: %w", err)
	}

	if packetID != 0x01 {
		return 0, fmt.Errorf("unexpected packet ID: %d", packetID)
	}

	// Read payload
	var payload int64
	if err := binary.Read(reader, binary.BigEndian, &payload); err != nil {
		return 0, fmt.Errorf("failed to read payload: %w", err)
	}

	return payload, nil
}

// Send packet with length prefix
func (c *SLPClient) sendPacket(conn net.Conn, data []byte) error {
	var buf bytes.Buffer
	writeVarInt(&buf, int32(len(data)))
	buf.Write(data)
	_, err := conn.Write(buf.Bytes())
	return err
}

// Extract plain text MOTD from description field
func parseDescription(desc json.RawMessage) string {
	if len(desc) == 0 {
		return ""
	}

	// Try parsing as plain string first
	var plainStr string
	if err := json.Unmarshal(desc, &plainStr); err == nil {
		return strings.TrimSpace(plainStr)
	}

	// Try parsing as chat component object
	var component struct {
		Text  string `json:"text"`
		Extra []struct {
			Text string `json:"text"`
		} `json:"extra"`
	}
	if err := json.Unmarshal(desc, &component); err == nil {
		var result strings.Builder
		result.WriteString(component.Text)
		for _, extra := range component.Extra {
			result.WriteString(extra.Text)
		}
		return strings.TrimSpace(result.String())
	}

	// Fallback: strip any JSON formatting and return raw
	return strings.TrimSpace(string(desc))
}

func writeVarInt(w io.Writer, value int32) error {
	for {
		if (value & ^0x7F) == 0 {
			return binary.Write(w, binary.BigEndian, byte(value))
		}
		if err := binary.Write(w, binary.BigEndian, byte((value&0x7F)|0x80)); err != nil {
			return err
		}
		value = int32(uint32(value) >> 7)
	}
}

func readVarInt(r io.Reader) (int32, error) {
	var value int32
	var position int
	buf := make([]byte, 1)

	for {
		n, err := r.Read(buf)
		if err != nil {
			return 0, err
		}
		if n != 1 {
			return 0, fmt.Errorf("failed to read byte")
		}
		currentByte := buf[0]

		value |= int32(currentByte&0x7F) << position

		if currentByte&0x80 == 0 {
			break
		}

		position += 7
		if position >= 32 {
			return 0, fmt.Errorf("VarInt is too big")
		}
	}

	return value, nil
}

func writeString(w io.Writer, s string) error {
	if err := writeVarInt(w, int32(len(s))); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func readString(r io.Reader) (string, error) {
	length, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if length < 0 || length > 32767 {
		return "", fmt.Errorf("string length out of bounds: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

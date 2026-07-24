// Basic Rcon implementation according to https://minecraft.wiki/Rcon
package rcon

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/nickheyer/discopanel/pkg/logger"
	"golang.org/x/text/encoding/charmap"
)

const (
	packetAuth          int32 = 3
	packetAuthResponse  int32 = 2
	packetExecCommand   int32 = 2
	packetResponseValue int32 = 0

	defaultTimeout = 3 * time.Second
)

type RconPacket struct {
	Size int32
	ID   int32
	Type int32
	Body string
}

type Client struct {
	host     string
	port     int
	password string
	log      *logger.Logger

	packetID atomic.Int32
	mu       sync.Mutex
	conn     net.Conn
	reader   *bufio.Reader
}

func NewClient(host string, port int, password string, log *logger.Logger) *Client {
	return &Client{
		host:     host,
		port:     port,
		password: password,
		log:      log,
	}
}

func (c *Client) Host() string     { return c.host }
func (c *Client) Port() int        { return c.port }
func (c *Client) Password() string { return c.password }

func (c *Client) nextPacketID() int32 {
	id := c.packetID.Add(1)

	if id <= 0 {
		c.packetID.Store(1)
		return 1
	}

	return id
}

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.connectLocked()
}

func (c *Client) connectLocked() error {
	c.closeLocked()

	addr := net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port))
	conn, err := net.DialTimeout("tcp", addr, defaultTimeout)
	if err != nil {
		return fmt.Errorf("failed to connect to RCON server: %w", err)
	}

	reader := bufio.NewReader(conn)

	// authenticate
	if err := c.authenticate(conn, reader); err != nil {
		conn.Close()
		return err
	}

	c.conn = conn
	c.reader = reader
	c.log.Info("RCON connection established successfully")
	return nil
}

func (c *Client) Execute(command string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	res, err := c.executeLocked(command)
	if err == nil {
		return res, nil
	}

	c.log.Warn("RCON command failed, retrying connection... Error: %v", err)
	if reconnected := c.connectLocked(); reconnected == nil {
		return c.executeLocked(command)
	}

	return "", err
}

func (c *Client) executeLocked(command string) (string, error) {

	if c.conn == nil {
		if err := c.connectLocked(); err != nil {
			return "", err
		}
	}

	_ = c.conn.SetDeadline(time.Now().Add(defaultTimeout))

	cmdPacket := RconPacket{
		ID:   c.nextPacketID(),
		Type: packetExecCommand,
		Body: command,
	}

	err := writePacket(c.conn, cmdPacket, c.log)
	if err != nil {
		c.closeLocked()
		return "", err
	}

	// dummy packet to detect end of output
	terminator := RconPacket{
		ID:   c.nextPacketID(),
		Type: packetExecCommand,
		Body: "",
	}

	err = writePacket(c.conn, terminator, c.log)
	if err != nil {
		c.closeLocked()
		return "", err
	}

	var response strings.Builder

	for {
		resp, err := readPacket(c.reader, c.log)
		if err != nil {
			c.closeLocked()
			return "", err
		}

		if resp.ID == cmdPacket.ID {
			response.WriteString(resp.Body)
		}

		if resp.ID == terminator.ID {
			break
		}
	}

	return response.String(), nil
}

func (c *Client) authenticate(conn net.Conn, reader *bufio.Reader) error {
	authPacket := RconPacket{
		ID:   1,
		Type: packetAuth,
		Body: c.password,
	}
	err := writePacket(conn, authPacket, c.log)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to send auth packet: %w", err)
	}

	response, err := readPacket(reader, c.log)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	if response.ID == -1 || response.Type != packetAuthResponse {
		return fmt.Errorf("authentication failed: invalid response")
	}
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.closeLocked()
}

func (c *Client) closeLocked() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.reader = nil
		return err
	}
	return nil
}

func writePacket(conn net.Conn, packet RconPacket, log *logger.Logger) error {
	// log.Debug("Sending Packet: ID=%d, Type=%d, Body=%q\n", packet.ID, packet.Type, packet.Body)
	bodyBytes := []byte(packet.Body)
	size := 4 + 4 + len(bodyBytes) + 2

	buf := make([]byte, 4+size)

	binary.LittleEndian.PutUint32(buf[0:4], uint32(size))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(packet.ID))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(packet.Type))

	copy(buf[12:], bodyBytes)

	buf[len(buf)-2] = 0x00
	buf[len(buf)-1] = 0x00

	_, err := conn.Write(buf)

	return err
}

func readPacket(reader *bufio.Reader, log *logger.Logger) (RconPacket, error) {
	var packet RconPacket

	sizeBytes := make([]byte, 4)
	_, err := io.ReadFull(reader, sizeBytes)
	if err != nil {
		return packet, fmt.Errorf("failed to read packet size: %w", err)
	}

	size := binary.LittleEndian.Uint32(sizeBytes)
	packetBytes := make([]byte, size)
	_, err = io.ReadFull(reader, packetBytes)
	if err != nil {
		return packet, fmt.Errorf("failed to read packet data: %w", err)
	}

	packet.Size = int32(size)
	packet.ID = int32(binary.LittleEndian.Uint32(packetBytes[0:4]))
	packet.Type = int32(binary.LittleEndian.Uint32(packetBytes[4:8]))
	packet.Body = decodeOutput(packetBytes[8 : len(packetBytes)-2])

	// log.Debug("Received Packet: ID=%d, Type=%d, Body=%q\n", packet.ID, packet.Type, packet.Body)

	return packet, nil
}

func decodeOutput(input []byte) string {
	// just return if valid UTF-8
	if utf8.Valid(input) {
		return string(input)
	}

	// convert ISO-8859_1 to UTF-8 (old server versions)
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(input)
	if err == nil {
		return string(decoded)
	}

	// fallback
	return string(bytes.ToValidUTF8(input, []byte("?")))
}

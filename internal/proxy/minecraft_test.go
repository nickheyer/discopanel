package proxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/nickheyer/discopanel/pkg/logger"
)

func newTestProxy(t *testing.T, gate ServerGate) (*MinecraftProxy, string) {
	t.Helper()
	p := NewMinecraftProxy(&Config{
		ListenAddr: "127.0.0.1:0",
		Logger:     logger.New(),
		Gate:       gate,
	})
	if err := p.Start(); err != nil {
		t.Fatalf("proxy start: %v", err)
	}
	t.Cleanup(func() { p.Stop() })
	return p, p.listener.Addr().String()
}

func newTestBackend(t *testing.T) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("backend listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln, ln.Addr().String()
}

func TestNormalizeHostname(t *testing.T) {
	cases := map[string]string{
		"Play.Example.COM":             "play.example.com",
		"play.example.com.":            "play.example.com",
		"play.example.com:25565":       "play.example.com",
		"play.example.com\x00FML3\x00": "play.example.com",
		"play.example.com\x00FML\x00":  "play.example.com",
	}
	for in, want := range cases {
		if got := normalizeHostname(in); got != want {
			t.Errorf("normalizeHostname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVarIntRoundTrip(t *testing.T) {
	for _, v := range []VarInt{0, 1, 127, 128, 255, 25565, 1<<28 - 1, -1, -25565, math.MinInt32, math.MaxInt32} {
		var buf bytes.Buffer
		if err := WriteVarInt(&buf, v); err != nil {
			t.Fatalf("write %d: %v", v, err)
		}
		if buf.Len() != v.Len() {
			t.Errorf("encoded %d as %d bytes, Len says %d", v, buf.Len(), v.Len())
		}
		got, err := ReadVarInt(&buf)
		if err != nil {
			t.Fatalf("read %d: %v", v, err)
		}
		if got != v {
			t.Errorf("round trip %d -> %d", v, got)
		}
	}
}

// Guards against negative protocol versions looping WriteVarInt forever
func TestNegativeVarIntHandshakeRewrite(t *testing.T) {
	var buf bytes.Buffer
	err := WriteHandshakePacket(&buf, &HandshakePacket{
		ProtocolVersion: -1,
		ServerAddress:   "mc.example.com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})
	if err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	hs, err := ReadHandshakePacket(&buf)
	if err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	if hs.ProtocolVersion != -1 {
		t.Errorf("protocol version round trip -1 -> %d", hs.ProtocolVersion)
	}
}

// Verifies routing, address rewrite, and buffered bytes reach backend
func TestRoutingAndLeftoverFlush(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	p.AddRoute("server-1", "mc.example.com", host, port)

	backendDone := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		defer conn.Close()

		hs, err := ReadHandshakePacket(conn)
		if err != nil {
			backendDone <- fmt.Errorf("backend handshake: %w", err)
			return
		}
		if hs.ServerAddress != "localhost" {
			backendDone <- fmt.Errorf("address not rewritten: %q", hs.ServerAddress)
			return
		}
		if int(hs.ServerPort) != port {
			backendDone <- fmt.Errorf("port not rewritten: %d", hs.ServerPort)
			return
		}

		// The status request the client pipelined after the handshake
		length, err := ReadVarInt(conn)
		if err != nil || length != 1 {
			backendDone <- fmt.Errorf("expected pipelined status request, len=%d err=%v", length, err)
			return
		}
		id, err := ReadVarInt(conn)
		if err != nil || id != 0x00 {
			backendDone <- fmt.Errorf("expected status request packet, id=%d err=%v", id, err)
			return
		}

		// Reply with something so the relay path back is exercised
		conn.Write([]byte("pong"))
		backendDone <- nil
	}()

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	// One write, handshake plus pipelined status request together
	var out bytes.Buffer
	WriteHandshakePacket(&out, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "MC.Example.Com",
		ServerPort:      25565,
		NextState:       NextStateStatus,
	})
	WriteVarInt(&out, 1)    // Length
	WriteVarInt(&out, 0x00) // Status request
	if _, err := client.Write(out.Bytes()); err != nil {
		t.Fatalf("client write: %v", err)
	}

	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 4)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(reply) != "pong" {
		t.Fatalf("unexpected reply %q", reply)
	}
}

// Verifies FML markers survive the address rewrite
func TestForgeAddressPreserved(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	p.AddRoute("server-1", "forge.example.com", host, port)

	backendDone := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		defer conn.Close()
		hs, err := ReadHandshakePacket(conn)
		if err != nil {
			backendDone <- err
			return
		}
		if hs.ServerAddress != "localhost\x00FML3\x00" {
			backendDone <- fmt.Errorf("FML marker lost: %q", hs.ServerAddress)
			return
		}
		backendDone <- nil
	}()

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	WriteHandshakePacket(client, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "forge.example.com\x00FML3\x00",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})

	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
}

// Verifies unrouted hostname logins get a disconnect reason
func TestUnknownHostnameLoginDisconnect(t *testing.T) {
	_, proxyAddr := newTestProxy(t, nil)

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	WriteHandshakePacket(client, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "nope.example.com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	length, err := ReadVarInt(client)
	if err != nil {
		t.Fatalf("read disconnect length: %v", err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(client, data); err != nil {
		t.Fatalf("read disconnect payload: %v", err)
	}
	buf := bytes.NewReader(data)
	id, _ := ReadVarInt(buf)
	if id != 0x00 {
		t.Fatalf("expected disconnect packet id 0x00, got %d", id)
	}
	msgLen, _ := ReadVarInt(buf)
	msg := make([]byte, msgLen)
	io.ReadFull(buf, msg)
	var reason map[string]string
	if err := json.Unmarshal(msg, &reason); err != nil {
		t.Fatalf("disconnect reason is not chat JSON: %v (%q)", err, msg)
	}
	if reason["text"] == "" {
		t.Fatalf("empty disconnect reason")
	}
}

type fakeGate struct {
	sleeping map[string]*SleepingServer
	woken    chan string
	started  chan string
}

func (g *fakeGate) SleepingInfo(serverID string) (*SleepingServer, bool) {
	info, ok := g.sleeping[serverID]
	return info, ok
}

func (g *fakeGate) WakeServer(ctx context.Context, serverID string) error {
	delete(g.sleeping, serverID)
	g.woken <- serverID
	return nil
}

func (g *fakeGate) StartServer(serverID string) error {
	if g.started != nil {
		g.started <- serverID
	}
	return nil
}

// Verifies status pings synthesize a reply without waking
func TestSleepingStatus(t *testing.T) {
	gate := &fakeGate{
		sleeping: map[string]*SleepingServer{
			"server-1": {MOTD: "zzz", MaxPlayers: 20},
		},
		woken: make(chan string, 1),
	}
	p, proxyAddr := newTestProxy(t, gate)
	p.AddRoute("server-1", "mc.example.com", "127.0.0.1", 1) // backend unreachable on purpose

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))

	var out bytes.Buffer
	WriteHandshakePacket(&out, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "mc.example.com",
		ServerPort:      25565,
		NextState:       NextStateStatus,
	})
	WriteVarInt(&out, 1)
	WriteVarInt(&out, 0x00)
	client.Write(out.Bytes())

	length, err := ReadVarInt(client)
	if err != nil {
		t.Fatalf("read status length: %v", err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(client, data); err != nil {
		t.Fatalf("read status payload: %v", err)
	}
	buf := bytes.NewReader(data)
	id, _ := ReadVarInt(buf)
	if id != 0x00 {
		t.Fatalf("expected status response, got packet %d", id)
	}
	jsonLen, _ := ReadVarInt(buf)
	payload := make([]byte, jsonLen)
	io.ReadFull(buf, payload)
	var status struct {
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatalf("status JSON: %v", err)
	}
	if status.Description.Text != "zzz" {
		t.Fatalf("wrong MOTD: %q", status.Description.Text)
	}

	select {
	case id := <-gate.woken:
		t.Fatalf("status ping woke server %s", id)
	default:
	}
}

// Exercises half-close, client still gets the response
func TestTCPHalfCloseThroughProxy(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Reads everything the client sends until half-close EOF
		data, _ := io.ReadAll(conn)
		// Replies, lost without half-close propagation
		conn.Write(append([]byte("echo:"), data...))
	}()

	tp := NewTCPProxy(&Config{ListenAddr: "127.0.0.1:0", Logger: logger.New()})
	if err := tp.Start(); err != nil {
		t.Fatalf("tcp proxy start: %v", err)
	}
	defer tp.Stop()

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	tp.AddRoute("server-1", "", host, port)

	client, err := net.DialTimeout("tcp", tp.listener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	client.Write([]byte("hello"))
	client.(*net.TCPConn).CloseWrite()

	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reply, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(reply) != "echo:hello" {
		t.Fatalf("unexpected reply %q", reply)
	}
}

// Encodes a string as big endian UTF-16 bytes
func utf16be(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		binary.BigEndian.PutUint16(b[2*i:], u)
	}
	return b
}

// Builds a 1.6 style ping carrying the hostname
func legacy16Ping(hostname string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xFE, 0x01, 0xFA})
	binary.Write(&buf, binary.BigEndian, uint16(len("MC|PingHost")))
	buf.Write(utf16be("MC|PingHost"))
	host := utf16be(hostname)
	binary.Write(&buf, binary.BigEndian, uint16(7+len(host)))
	buf.WriteByte(78)
	binary.Write(&buf, binary.BigEndian, uint16(len(hostname)))
	buf.Write(host)
	binary.Write(&buf, binary.BigEndian, uint32(25565))
	return buf.Bytes()
}

// Sends a legacy ping and decodes the kick payload
func legacyPingExchange(t *testing.T, addr string, ping []byte) string {
	t.Helper()
	client, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	client.Write(ping)
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reply, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if len(reply) < 3 || reply[0] != 0xFF {
		t.Fatalf("expected kick packet, got %x", reply)
	}
	if units := int(binary.BigEndian.Uint16(reply[1:3])); len(reply[3:]) != units*2 {
		t.Fatalf("length mismatch: header %d units, body %d bytes", units, len(reply[3:]))
	}
	return decodeUTF16BE(reply[3:])
}

// Verifies routed 1.6 pings get status without holding
func TestLegacyPingSynthesized(t *testing.T) {
	p, proxyAddr := newTestProxy(t, nil)
	p.UpsertServerRoute(Route{
		ServerID:    "server-1",
		Hostname:    "mc.example.com",
		BackendHost: "10.0.0.9",
		BackendPort: 25565,
		State:       RouteOffline,
		Wakeable:    true,
		MOTD:        "join to wake",
		MaxPlayers:  7,
	})

	fields := strings.Split(legacyPingExchange(t, proxyAddr, legacy16Ping("mc.example.com")), "\x00")
	if len(fields) != 6 || fields[0] != "§1" {
		t.Fatalf("unexpected legacy response fields %q", fields)
	}
	if fields[2] != "Offline" || fields[3] != "join to wake" || fields[5] != "7" {
		t.Fatalf("unexpected legacy status %q", fields)
	}

	stats := p.StatsSnapshots()["server-1"]
	if stats.TotalConns != 1 || stats.StatusPings != 1 {
		t.Fatalf("legacy ping not counted: %+v", stats)
	}
}

// Verifies bare 0xFE beta pings get the old format
func TestLegacyBetaPingSynthesized(t *testing.T) {
	p, proxyAddr := newTestProxy(t, nil)
	p.UpsertServerRoute(Route{
		ServerID:    "server-1",
		Hostname:    "mc.example.com",
		BackendHost: "10.0.0.9",
		BackendPort: 25565,
		State:       RouteOnline,
		MaxPlayers:  11,
	})

	payload := legacyPingExchange(t, proxyAddr, []byte{0xFE})
	if payload != "mc.example.com§0§11" {
		t.Fatalf("unexpected beta ping payload %q", payload)
	}
}

// Verifies unrouted legacy pings still get an answer
func TestLegacyPingUnrouted(t *testing.T) {
	_, proxyAddr := newTestProxy(t, nil)

	fields := strings.Split(legacyPingExchange(t, proxyAddr, []byte{0xFE, 0x01, 0xFA}), "\x00")
	if len(fields) != 6 || fields[0] != "§1" || fields[2] != "DiscoPanel" {
		t.Fatalf("unexpected legacy response fields %q", fields)
	}
}

// Covers a 254-byte handshake colliding with the legacy ping byte
func TestLargeHandshakeNotMistakenForLegacyPing(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	// Builds an address making the handshake exactly 254 bytes
	base := "big.example.com"
	pad := 254 - 1 - 2 - 2 - len(base) - 1 - 2 - 1 // -1 for the \x00 separator
	address := base + "\x00" + strings.Repeat("x", pad)

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	p.AddRoute("server-1", base, host, port)

	backendDone := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		defer conn.Close()
		if _, err := ReadHandshakePacket(conn); err != nil {
			backendDone <- err
			return
		}
		backendDone <- nil
	}()

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	var out bytes.Buffer
	WriteHandshakePacket(&out, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   address,
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})
	if out.Bytes()[0] != 0xFE || out.Bytes()[1] != 0x01 {
		t.Fatalf("test setup wrong: length prefix is % x, want fe 01", out.Bytes()[:2])
	}
	client.Write(out.Bytes())

	select {
	case err := <-backendDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("254-byte handshake was not routed (likely dropped as legacy ping)")
	}
}

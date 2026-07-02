package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

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
	for _, v := range []VarInt{0, 1, 127, 128, 255, 25565, 1<<28 - 1} {
		var buf bytes.Buffer
		if err := WriteVarInt(&buf, v); err != nil {
			t.Fatalf("write %d: %v", v, err)
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

// TestRoutingAndLeftoverFlush verifies a status handshake is routed by
// hostname, the address is rewritten for the backend, and bytes the client
// sent after the handshake (already buffered by the proxy) reach the backend.
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

		// The status request the client pipelined after the handshake.
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

		// Reply with something so the relay path back is exercised.
		conn.Write([]byte("pong"))
		backendDone <- nil
	}()

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	// Handshake + pipelined status request in one write so the proxy buffers
	// the request during handshake parsing.
	var out bytes.Buffer
	WriteHandshakePacket(&out, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "MC.Example.Com",
		ServerPort:      25565,
		NextState:       NextStateStatus,
	})
	WriteVarInt(&out, 1)    // length
	WriteVarInt(&out, 0x00) // status request
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

// TestForgeAddressPreserved verifies FML markers appended to the address
// survive the rewrite.
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

// TestUnknownHostnameLoginDisconnect verifies login attempts for unrouted
// hostnames receive a disconnect packet with a reason.
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

// TestSleepingStatus verifies status pings to a paused server get the
// synthesized response (and a pong) without waking it.
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

// TestTCPHalfCloseThroughProxy exercises the full half-close path over real
// TCP sockets: client sends, half-closes, and still receives the response.
func TestTCPHalfCloseThroughProxy(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read everything the client sends (until half-close EOF)...
		data, _ := io.ReadAll(conn)
		// ...then reply. Without half-close propagation this write is lost.
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

// TestLegacyPingDropped verifies pre-1.7 pings are dropped without hanging.
func TestLegacyPingDropped(t *testing.T) {
	_, proxyAddr := newTestProxy(t, nil)

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	client.Write([]byte{0xFE, 0x01, 0xFA})
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err != io.EOF {
		t.Fatalf("expected connection close for legacy ping, got %v", err)
	}
}

// TestLargeHandshakeNotMistakenForLegacyPing covers the 0xFE collision: a
// modern handshake whose packet length is exactly 254 encodes its length
// VarInt as 0xFE 0x01, the same opening bytes as a legacy ping.
func TestLargeHandshakeNotMistakenForLegacyPing(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	// Build an address that makes the handshake data exactly 254 bytes:
	// packetID(1) + protoVarInt(2 for 767) + addrLen(2) + addr + port(2) + state(1).
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

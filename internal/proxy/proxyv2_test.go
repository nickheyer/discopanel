package proxy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// Reads a framed status response and returns parsed JSON
func readSynthStatus(t *testing.T, conn net.Conn) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	length, err := ReadVarInt(conn)
	if err != nil {
		t.Fatalf("read status length: %v", err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		t.Fatalf("read status payload: %v", err)
	}
	buf := bytes.NewReader(data)
	id, _ := ReadVarInt(buf)
	if id != 0x00 {
		t.Fatalf("expected status response packet, got %d", id)
	}
	jsonLen, _ := ReadVarInt(buf)
	payload := make([]byte, jsonLen)
	if _, err := io.ReadFull(buf, payload); err != nil {
		t.Fatalf("read status JSON: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatalf("status is not JSON: %v (%q)", err, payload)
	}
	return status
}

// Digs the description text out of a status response
func statusMOTD(t *testing.T, status map[string]any) string {
	t.Helper()
	desc, ok := status["description"].(map[string]any)
	if !ok {
		t.Fatalf("status has no description: %v", status)
	}
	text, _ := desc["text"].(string)
	return text
}

// Reads a framed login disconnect and returns the text
func readDisconnectReason(t *testing.T, conn net.Conn) string {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	length, err := ReadVarInt(conn)
	if err != nil {
		t.Fatalf("read disconnect length: %v", err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
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
	return reason["text"]
}

func TestWriteProxyV2HeaderIPv4(t *testing.T) {
	src := &net.TCPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 54321}
	dst := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 25565}

	var buf bytes.Buffer
	if err := WriteProxyV2Header(&buf, src, dst); err != nil {
		t.Fatalf("write header: %v", err)
	}

	b := buf.Bytes()
	if len(b) != 16+proxyV2LenTCPv4 {
		t.Fatalf("header length %d, want %d", len(b), 16+proxyV2LenTCPv4)
	}
	if !bytes.Equal(b[:12], proxyV2Signature) {
		t.Fatalf("bad signature: %x", b[:12])
	}
	if b[12] != proxyV2CmdProxy || b[13] != proxyV2FamTCPv4 {
		t.Fatalf("bad cmd/family: %x %x", b[12], b[13])
	}
	if binary.BigEndian.Uint16(b[14:16]) != proxyV2LenTCPv4 {
		t.Fatalf("bad address block length")
	}
	if !bytes.Equal(b[16:20], []byte{203, 0, 113, 7}) {
		t.Fatalf("bad source IP: %v", b[16:20])
	}
	if !bytes.Equal(b[20:24], []byte{10, 0, 0, 1}) {
		t.Fatalf("bad dest IP: %v", b[20:24])
	}
	if binary.BigEndian.Uint16(b[24:26]) != 54321 {
		t.Fatalf("bad source port: %d", binary.BigEndian.Uint16(b[24:26]))
	}
	if binary.BigEndian.Uint16(b[26:28]) != 25565 {
		t.Fatalf("bad dest port: %d", binary.BigEndian.Uint16(b[26:28]))
	}
}

func TestWriteProxyV2HeaderIPv6(t *testing.T) {
	src := &net.TCPAddr{IP: net.ParseIP("2001:db8::7"), Port: 54321}
	dst := &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 25565}

	var buf bytes.Buffer
	if err := WriteProxyV2Header(&buf, src, dst); err != nil {
		t.Fatalf("write header: %v", err)
	}

	b := buf.Bytes()
	if len(b) != 16+proxyV2LenTCPv6 {
		t.Fatalf("header length %d, want %d", len(b), 16+proxyV2LenTCPv6)
	}
	if b[13] != proxyV2FamTCPv6 {
		t.Fatalf("bad family: %x", b[13])
	}
	if !bytes.Equal(b[16:32], src.IP.To16()) {
		t.Fatalf("bad source IP")
	}
	if binary.BigEndian.Uint16(b[48:50]) != 54321 {
		t.Fatalf("bad source port")
	}
}

func TestWriteProxyV2HeaderNonTCP(t *testing.T) {
	var buf bytes.Buffer
	src := &net.UnixAddr{Name: "@x", Net: "unix"}
	dst := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 25565}
	if err := WriteProxyV2Header(&buf, src, dst); err != nil {
		t.Fatalf("write header: %v", err)
	}
	b := buf.Bytes()
	if len(b) != 16 {
		t.Fatalf("LOCAL header length %d, want 16", len(b))
	}
	if b[12] != proxyV2CmdLocal || b[13] != proxyV2FamUnspec {
		t.Fatalf("bad LOCAL cmd/family: %x %x", b[12], b[13])
	}
}

// Verifies opted-in backends get a PROXY v2 header first
func TestProxyProtocolToBackend(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	p.UpsertServerRoute(Route{
		ServerID:      "server-1",
		Hostname:      "mc.example.com",
		BackendHost:   host,
		BackendPort:   port,
		ProxyProtocol: true,
	})

	backendDone := make(chan error, 1)
	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			backendDone <- err
			return
		}
		defer conn.Close()

		head := make([]byte, 16)
		if _, err := io.ReadFull(conn, head); err != nil {
			backendDone <- fmt.Errorf("read proxy header: %w", err)
			return
		}
		if !bytes.Equal(head[:12], proxyV2Signature) {
			backendDone <- fmt.Errorf("bad signature: %x", head[:12])
			return
		}
		if head[12] != proxyV2CmdProxy || head[13] != proxyV2FamTCPv4 {
			backendDone <- fmt.Errorf("bad cmd/family: %x %x", head[12], head[13])
			return
		}
		addrLen := binary.BigEndian.Uint16(head[14:16])
		addrs := make([]byte, addrLen)
		if _, err := io.ReadFull(conn, addrs); err != nil {
			backendDone <- fmt.Errorf("read address block: %w", err)
			return
		}
		if srcIP := net.IP(addrs[0:4]); !srcIP.Equal(net.IPv4(127, 0, 0, 1)) {
			backendDone <- fmt.Errorf("source IP %v, want 127.0.0.1", srcIP)
			return
		}

		hs, err := ReadHandshakePacket(conn)
		if err != nil {
			backendDone <- fmt.Errorf("handshake after header: %w", err)
			return
		}
		if hs.ServerAddress != "localhost" {
			backendDone <- fmt.Errorf("address not rewritten: %q", hs.ServerAddress)
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
		ServerAddress:   "mc.example.com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})

	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
}

// Verifies preserve-host routes keep hostname, rewrite port
func TestPreserveHostToBackend(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	p.UpsertServerRoute(Route{
		ServerID:     "server-1",
		Hostname:     "mc.example.com",
		BackendHost:  host,
		BackendPort:  port,
		PreserveHost: true,
	})

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
		if hs.ServerAddress != "MC.Example.Com" {
			backendDone <- fmt.Errorf("hostname not preserved: %q", hs.ServerAddress)
			return
		}
		if int(hs.ServerPort) != port {
			backendDone <- fmt.Errorf("port not rewritten: %d", hs.ServerPort)
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
		ServerAddress:   "MC.Example.Com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})

	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
}

// Verifies mistyped hostnames get a branded status reply
func TestUnknownHostStatusPing(t *testing.T) {
	_, proxyAddr := newTestProxy(t, nil)

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	var out bytes.Buffer
	WriteHandshakePacket(&out, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "typo.example.com",
		ServerPort:      25565,
		NextState:       NextStateStatus,
	})
	WriteVarInt(&out, 1)
	WriteVarInt(&out, 0x00)
	client.Write(out.Bytes())

	status := readSynthStatus(t, client)
	motd := statusMOTD(t, status)
	if !bytes.Contains([]byte(motd), []byte("DiscoPanel")) {
		t.Fatalf("MOTD not branded: %q", motd)
	}
	if !bytes.Contains([]byte(motd), []byte("typo.example.com")) {
		t.Fatalf("MOTD does not name the hostname: %q", motd)
	}
}

// Verifies offline route answers pings and wakes on login
func TestOfflineRouteStatusAndWake(t *testing.T) {
	gate := &fakeGate{started: make(chan string, 1)}
	p, proxyAddr := newTestProxy(t, gate)
	p.UpsertServerRoute(Route{
		ServerID:   "server-1",
		Hostname:   "mc.example.com",
		State:      RouteOffline,
		Wakeable:   true,
		MOTD:       "creative world is offline - join to start it up",
		MaxPlayers: 20,
	})

	// Status ping sees the offline MOTD without any start
	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
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
	motd := statusMOTD(t, readSynthStatus(t, client))
	if motd != "creative world is offline - join to start it up" {
		t.Fatalf("wrong offline MOTD: %q", motd)
	}
	client.Close()

	select {
	case id := <-gate.started:
		t.Fatalf("status ping started server %s", id)
	default:
	}

	// Login cold-starts the server and disconnects with a starting message
	login, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("login dial: %v", err)
	}
	defer login.Close()
	WriteHandshakePacket(login, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "mc.example.com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})

	reason := readDisconnectReason(t, login)
	if !bytes.Contains([]byte(reason), []byte("starting")) {
		t.Fatalf("disconnect does not mention starting: %q", reason)
	}

	select {
	case id := <-gate.started:
		if id != "server-1" {
			t.Fatalf("started wrong server: %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("login did not start the server")
	}
}

// Verifies a booting server reports progress, defers logins
func TestStartingRouteWithoutBackend(t *testing.T) {
	p, proxyAddr := newTestProxy(t, nil)
	p.UpsertServerRoute(Route{
		ServerID:   "server-1",
		Hostname:   "mc.example.com",
		State:      RouteStarting,
		MOTD:       "big modpack is installing server files - join in a moment",
		MaxPlayers: 20,
	})

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
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
	motd := statusMOTD(t, readSynthStatus(t, client))
	if !bytes.Contains([]byte(motd), []byte("installing server files")) {
		t.Fatalf("wrong starting MOTD: %q", motd)
	}
	client.Close()

	login, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("login dial: %v", err)
	}
	defer login.Close()
	WriteHandshakePacket(login, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "mc.example.com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})
	reason := readDisconnectReason(t, login)
	if !bytes.Contains([]byte(reason), []byte("starting")) {
		t.Fatalf("disconnect does not mention starting: %q", reason)
	}
}

// Verifies per-route counters move with traffic
func TestRouteStatsCounting(t *testing.T) {
	backendLn, backendAddr := newTestBackend(t)
	p, proxyAddr := newTestProxy(t, nil)

	host, portStr, _ := net.SplitHostPort(backendAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	p.AddRoute("server-1", "mc.example.com", host, port)

	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		ReadHandshakePacket(conn)
		conn.Write([]byte("pong"))
	}()

	client, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	WriteHandshakePacket(client, &HandshakePacket{
		ProtocolVersion: 767,
		ServerAddress:   "mc.example.com",
		ServerPort:      25565,
		NextState:       NextStateLogin,
	})
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 4)
	io.ReadFull(client, reply)
	client.Close()
	<-backendDone

	// The relay finishes asynchronously after the client closes
	deadline := time.Now().Add(2 * time.Second)
	for {
		stats := p.StatsSnapshots()["server-1"]
		if stats.TotalConns == 1 && stats.Logins == 1 &&
			stats.LastProtocol == 767 && stats.BytesToClient >= 4 && stats.ActiveConns == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats never settled: %+v", stats)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Removing the route clears its counters
	p.RemoveRoute("mc.example.com")
	if _, ok := p.StatsSnapshots()["server-1"]; ok {
		t.Fatalf("stats survived route removal")
	}
}

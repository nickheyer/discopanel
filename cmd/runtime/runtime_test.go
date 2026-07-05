package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"google.golang.org/protobuf/proto"
)

// Exercises the javaagent loopback wire format
func TestFrameRoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	want := &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_TickSample{TickSample: &agentv1.TickSample{
		Tps: 19.7, MsptAvg: 12.3, MsptMax: 48.1,
	}}}
	go func() {
		data, _ := proto.Marshal(want)
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(len(data)))
		_, _ = client.Write(header[:])
		_, _ = client.Write(data)
	}()

	got, err := readFrame(server)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.GetTickSample().GetTps() != 19.7 || got.GetTickSample().GetMsptMax() != 48.1 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], maxFrameSize+1)
		_, _ = client.Write(header[:])
	}()
	if _, err := readFrame(server); err == nil {
		t.Fatal("oversize frame must be rejected")
	}
}

func TestParseMemoryMB(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"4096M", 4096},
		{"12G", 12288},
		{"2048", 2048},
		{"  8g ", 8192},
		{"1048576K", 1024},
		{"", 0},
		{"lots", 0},
		{"12.5G", 0},
	}
	for _, c := range cases {
		if got := parseMemoryMB(c.in); got != c.want {
			t.Errorf("parseMemoryMB(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIsCrash(t *testing.T) {
	cases := []struct {
		exitCode      int
		stopRequested bool
		reportPath    string
		want          bool
	}{
		{0, false, "", false},
		{1, false, "", true},
		{143, false, "", true},
		{143, true, "", false},
		{130, true, "", false},
		{0, true, "crash-reports/crash.txt", true},
		{143, true, "crash-reports/crash.txt", true},
	}
	for _, c := range cases {
		if got := isCrash(c.exitCode, c.stopRequested, c.reportPath); got != c.want {
			t.Errorf("isCrash(%d, %v, %q) = %v, want %v",
				c.exitCode, c.stopRequested, c.reportPath, got, c.want)
		}
	}
}

func TestReadyPattern(t *testing.T) {
	ready := []string{
		`[12:34:56] [Server thread/INFO]: Done (9.418s)! For help, type "help"`,
		`[12:34:56] [Server thread/INFO] [minecraft/DedicatedServer]: Done (31.416s)! For help, type "help" or "?"`,
		`Done (0.5s)!`,
	}
	for _, line := range ready {
		if !readyPattern.MatchString(line) {
			t.Errorf("readyPattern missed ready line %q", line)
		}
	}
	notReady := []string{
		`[12:34:56] [Server thread/INFO]: Preparing spawn area: 95%`,
		`[12:34:56] [Server thread/INFO]: <Steve> Done (9.418)`,
		`Downloading Done file`,
	}
	for _, line := range notReady {
		if readyPattern.MatchString(line) {
			t.Errorf("readyPattern false positive on %q", line)
		}
	}
}

func TestGCPausePattern(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"[2026-07-02T10:00:00.000+0000][12.345s][info][gc] GC(5) Pause Young (Normal) (G1 Evacuation Pause) 1024M->512M(2048M) 3.456ms", "3.456"},
		{"[1.234s][info][gc] GC(0) Pause Full (System.gc()) 100M->50M(256M) 123.4ms", "123.4"},
		{"[5.0s][info][gc,phases] GC(3) Pause Mark Start 0.005ms", "0.005"},
	}
	for _, c := range cases {
		m := gcPausePattern.FindStringSubmatch(c.line)
		if m == nil {
			t.Errorf("gcPausePattern missed %q", c.line)
			continue
		}
		if m[1] != c.want {
			t.Errorf("gcPausePattern(%q) captured %q, want %q", c.line, m[1], c.want)
		}
	}
	noPause := []string{
		"[1.2s][info][gc] GC(1) Concurrent Mark Cycle 15.2ms",
		"[1.2s][info][gc,init] Heap Region Size: 8M",
	}
	for _, line := range noPause {
		if gcPausePattern.MatchString(line) {
			t.Errorf("gcPausePattern false positive on %q", line)
		}
	}
}

func TestLogPrefixPattern(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"[12:34:56] [Server thread/INFO]: Nick joined the game", "Nick joined the game"},
		{"[12Jul2026 21:57:22.786] [Server thread/INFO] [minecraft/MinecraftServer]: Nick left the game", "Nick left the game"},
		{"[21:57:22 INFO]: <Nick> hello", "<Nick> hello"},
	}
	for _, c := range cases {
		prefix := logPrefixPattern.FindString(c.line)
		if prefix == "" || c.line[len(prefix):] != c.want {
			t.Errorf("logPrefixPattern(%q) stripped to %q, want %q", c.line, c.line[len(prefix):], c.want)
		}
	}
	if logPrefixPattern.MatchString("Nick joined the game") {
		t.Error("logPrefixPattern matched a prefixless line")
	}
}

func TestConsoleEvents(t *testing.T) {
	sup := &supervisor{startedAt: time.Now()}
	sup.events = newConsoleEvents(sup)
	events := sup.events

	// No panel session, sends drop, roster state still tracks
	events.handleLine("[12:34:56] [User Authenticator #1/INFO]: UUID of player Nick is 3b9f1c2d-0000-0000-0000-000000000001")
	events.handleLine("[12:34:56] [Server thread/INFO]: Nick joined the game")
	if !events.isOnline("Nick") {
		t.Fatal("join line did not add Nick to the roster")
	}
	if got := events.uuids["Nick"]; got != "3b9f1c2d-0000-0000-0000-000000000001" {
		t.Errorf("uuid not captured, got %q", got)
	}
	events.handleLine("[12:34:56] [Server thread/INFO]: Nick left the game")
	if events.isOnline("Nick") {
		t.Fatal("leave line did not remove Nick from the roster")
	}
}

func TestMatchDeath(t *testing.T) {
	deaths := []string{
		"Nick was slain by Zombie",
		"Nick was shot by Skeleton using Bow of Doom",
		"Nick drowned",
		"Nick fell from a high place",
		"Nick fell off a ladder",
		"Nick tried to swim in lava to escape Blaze",
		"Nick was killed by [Intentional Game Design]",
		"Nick hit the ground too hard whilst trying to escape Spider",
		"Nick withered away",
		"Nick experienced kinetic energy",
		"Nick didn't want to live in the same world as Warden",
	}
	for _, msg := range deaths {
		if player, _ := matchDeath(msg); player != "Nick" {
			t.Errorf("matchDeath(%q) missed, got %q", msg, player)
		}
	}
	notDeaths := []string{
		"Nick lost connection: Disconnected",
		"Nick issued server command: /help",
		"Nick moved too quickly!",
		"Nick joined the game",
		"Preparing spawn area: 95%",
	}
	for _, msg := range notDeaths {
		if player, _ := matchDeath(msg); player != "" {
			t.Errorf("matchDeath(%q) false positive for %q", msg, player)
		}
	}
}

func TestLagLineParsing(t *testing.T) {
	sup := &supervisor{startedAt: time.Now()}
	sup.events = newConsoleEvents(sup)
	events := sup.events

	// One line alone gives no interval yet
	events.handleLine("[12:34:56] [Server thread/WARN]: Can't keep up! Did the system time change, or is the server overloaded?")
	if _, _, ok := events.lagDebt(); ok {
		t.Fatal("single lag line must not produce a debt interval")
	}

	// Second line with the modern quantifier completes the pair
	events.handleLine("[12:35:08] [Server thread/WARN]: Can't keep up! Is the server overloaded? Running 2354ms or 47 ticks behind")
	debt, interval, ok := events.lagDebt()
	if !ok {
		t.Fatal("two lag lines must produce a debt interval")
	}
	if debt != 2354 {
		t.Errorf("debt = %v, want 2354", debt)
	}
	if interval < 1 {
		t.Errorf("interval = %v, want clamped to >= 1s", interval)
	}
}

func TestAssembleTickSample(t *testing.T) {
	// Healthy server, busy 30% of the cadence
	s := assembleTickSample(0.3, 22.5, 0, 0, false)
	if s.GetTps() != 20 || s.GetMsptAvg() != 15 || s.GetMsptMax() != 22.5 {
		t.Errorf("healthy sample = %+v", s)
	}

	// Pegged thread without lag lines yet pins at the budget boundary
	s = assembleTickSample(1.0, 9800, 0, 0, false)
	if s.GetTps() != 20 || s.GetMsptAvg() != 50 || s.GetMsptMax() != 50 {
		t.Errorf("saturated unquantified sample = %+v", s)
	}

	// Pegged thread with 2000ms debt over 12s is 60ms ticks
	s = assembleTickSample(1.0, 9800, 2000, 12, true)
	if s.GetTps() < 16.6 || s.GetTps() > 16.7 {
		t.Errorf("saturated tps = %v, want ~16.67", s.GetTps())
	}
	if s.GetMsptAvg() < 59.9 || s.GetMsptAvg() > 60.1 {
		t.Errorf("saturated mspt = %v, want ~60", s.GetMsptAvg())
	}

	// Absurd debt rates clamp instead of reporting zero
	s = assembleTickSample(1.0, 9800, 60000, 2, true)
	if s.GetTps() < 1 {
		t.Errorf("clamped tps = %v, want >= 1", s.GetTps())
	}

	// A single long tick in a healthy window stays visible as the max
	s = assembleTickSample(0.5, 80, 0, 0, false)
	if s.GetMsptMax() != 80 || s.GetTps() != 20 {
		t.Errorf("spike sample = %+v", s)
	}
}

func TestChatAndAdvancementPatterns(t *testing.T) {
	if m := chatPattern.FindStringSubmatch("<Nick> hello world"); m == nil || m[1] != "Nick" || m[2] != "hello world" {
		t.Errorf("chatPattern missed plain chat: %v", m)
	}
	if m := chatPattern.FindStringSubmatch("[Not Secure] <Nick> hi"); m == nil || m[1] != "Nick" {
		t.Errorf("chatPattern missed unsigned chat: %v", m)
	}
	if m := advancementPattern.FindStringSubmatch("Nick has made the advancement [Stone Age]"); m == nil || m[2] != "Stone Age" {
		t.Errorf("advancementPattern missed: %v", m)
	}
	if m := advancementPattern.FindStringSubmatch("Nick has completed the challenge [Uneasy Alliance]"); m == nil || m[2] != "Uneasy Alliance" {
		t.Errorf("advancementPattern missed challenge: %v", m)
	}
}

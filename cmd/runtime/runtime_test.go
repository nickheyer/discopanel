package main

import (
	"encoding/binary"
	"net"
	"testing"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"google.golang.org/protobuf/proto"
)

// TestFrameRoundTrip exercises the loopback wire format the disco-agent mod
// speaks: 4-byte big-endian length prefix followed by an AgentMessage.
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

func TestAgentJarFor(t *testing.T) {
	cases := []struct {
		loader  string
		java    int
		wantJar string
		wantDir string
		ok      bool
	}{
		{"fabric", 21, "disco-agent-fabric.jar", "mods", true},
		{"quilt", 21, "disco-agent-fabric.jar", "mods", true},
		{"neoforge", 21, "disco-agent-neoforge.jar", "mods", true},
		{"forge", 17, "disco-agent-forge.jar", "mods", true},
		{"paper", 21, "disco-agent-paper.jar", "plugins", true},
		{"purpur", 21, "disco-agent-paper.jar", "plugins", true},
		{"folia", 21, "", "", false},
		{"vanilla", 21, "", "", false},
		{"forge", 8, "", "", false}, // legacy java servers get no mod
	}
	for _, c := range cases {
		spec := &runtimespec.LaunchSpec{Loader: c.loader, JavaMajor: c.java}
		jar, dir, ok := agentJarFor(spec)
		if ok != c.ok || jar != c.wantJar || dir != c.wantDir {
			t.Errorf("agentJarFor(%s, java%d) = (%q, %q, %v), want (%q, %q, %v)",
				c.loader, c.java, jar, dir, ok, c.wantJar, c.wantDir, c.ok)
		}
	}
}

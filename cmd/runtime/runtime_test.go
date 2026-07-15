package main

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
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

// Spoofable lifecycle messages must never cross the loopback boundary
func TestLoopbackAllowlist(t *testing.T) {
	sup := &supervisor{startedAt: time.Now()}
	sup.events = newConsoleEvents(sup)

	sess := &panelSession{sendCh: make(chan *agentv1.AgentMessage, 16)}
	sup.mu.Lock()
	sup.session = sess
	sup.mu.Unlock()

	blocked := []*agentv1.AgentMessage{
		{Payload: &agentv1.AgentMessage_Exited{Exited: &agentv1.Exited{ExitCode: 1, Crashed: true}}},
		{Payload: &agentv1.AgentMessage_Ready{Ready: &agentv1.Ready{}}},
		{Payload: &agentv1.AgentMessage_Roster{Roster: &agentv1.Roster{OnlinePlayers: []string{"fake"}}}},
		{Payload: &agentv1.AgentMessage_PlayerEvent{PlayerEvent: &agentv1.PlayerEvent{Player: "fake"}}},
		{Payload: &agentv1.AgentMessage_Stopping{Stopping: &agentv1.Stopping{}}},
		{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{Source: agentv1.HelloSource_HELLO_SOURCE_RUNTIME}}},
	}
	for _, msg := range blocked {
		sup.handleAgentMessage(msg, nil)
	}
	if n := len(sess.sendCh); n != 0 {
		t.Fatalf("%d spoofed loopback messages were relayed upstream", n)
	}

	sup.handleAgentMessage(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_JvmSample{JvmSample: &agentv1.JvmSample{HeapUsedMb: 1}}}, nil)
	sup.handleAgentMessage(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{Source: agentv1.HelloSource_HELLO_SOURCE_JVM}}}, nil)
	if n := len(sess.sendCh); n != 2 {
		t.Fatalf("expected 2 allowed messages relayed, got %d", n)
	}

	// Fatal errors held for the exit report, never relayed live
	fatal := &agentv1.FatalError{Thread: "main"}
	sup.handleAgentMessage(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_FatalError{FatalError: fatal}}, nil)
	if n := len(sess.sendCh); n != 2 {
		t.Fatalf("fatal error must stay local, got %d messages", n)
	}
	if sup.fatalError().GetThread() != "main" {
		t.Fatal("fatal error must be stored on the supervisor")
	}
}

func TestExitReportFatalRoundTrip(t *testing.T) {
	fatal := &agentv1.FatalError{
		Thread: "main",
		Causes: []*agentv1.CrashCause{{
			Type:    "java.lang.RuntimeException",
			Message: "boom",
			Frames: []*agentv1.CrashFrame{{
				ClassName:      "dev.example.Bad",
				MethodName:     "tick",
				SourceLocation: "union:/data/mods/bad.jar%231!/",
			}},
		}},
	}

	report := &exitReport{ExitCode: 1, Crashed: true, ExitedAtUnixMs: time.Now().UnixMilli()}
	report.setFatal(fatal)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, runtimespec.StateDir), 0755); err != nil {
		t.Fatal(err)
	}
	writeExitReport(dir, report)
	loaded := readExitReport(dir)
	if loaded == nil {
		t.Fatal("exit report must round trip")
	}

	got := loaded.fatal()
	if got.GetThread() != "main" || len(got.GetCauses()) != 1 {
		t.Fatalf("fatal error must survive persistence, got %+v", got)
	}
	if got.GetCauses()[0].GetFrames()[0].GetSourceLocation() != "union:/data/mods/bad.jar%231!/" {
		t.Fatal("frame location must survive persistence")
	}

	// The replayed proto message carries the fatal error too
	exited := msgExited(loaded).GetExited()
	if exited.GetFatalError().GetThread() != "main" {
		t.Fatal("msgExited must carry the fatal error")
	}
}

func TestSetFatalError(t *testing.T) {
	armedAt := func(s *supervisor) time.Time {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.bootFailedAt
	}

	s := &supervisor{}
	plain := &agentv1.FatalError{Causes: []*agentv1.CrashCause{{Type: "a.MixinError"}}}
	attributed := &agentv1.FatalError{
		Causes:     []*agentv1.CrashCause{{Type: "b.LoadingFailedException"}},
		FailedMods: []*agentv1.FailedMod{{ModId: "oculus"}},
	}

	s.setFatalError(plain)
	if !armedAt(s).IsZero() {
		t.Fatal("plain fatal must not arm the watchdog")
	}
	s.setFatalError(attributed)
	if len(s.fatalError().GetFailedMods()) != 1 {
		t.Fatal("attributed report must replace the plain one")
	}
	if armedAt(s).IsZero() {
		t.Fatal("pre-ready loader-blamed fatal must arm the watchdog")
	}

	s.setFatalError(plain)
	if len(s.fatalError().GetFailedMods()) != 1 {
		t.Fatal("plain report must not displace the attributed one")
	}

	ready := &supervisor{ready: true}
	ready.setFatalError(plain)
	if ready.fatalError() != nil {
		t.Fatal("post-ready logged fatal must be dropped")
	}
	uncaught := &agentv1.FatalError{Uncaught: true, Causes: []*agentv1.CrashCause{{Type: "c.NullPointerException"}}}
	ready.setFatalError(uncaught)
	if ready.fatalError() == nil {
		t.Fatal("post-ready uncaught fatal must be held")
	}
	if !armedAt(ready).IsZero() {
		t.Fatal("post-ready fatal must not arm the watchdog")
	}
}

func TestIsCrash(t *testing.T) {
	cases := []struct {
		exitCode      int
		stopRequested bool
		wasReady      bool
		reportPath    string
		want          bool
	}{
		{0, false, true, "", false},
		{1, false, true, "", true},
		{143, false, true, "", true},
		{143, true, false, "", false},
		{130, true, true, "", false},
		{0, true, true, "crash-reports/crash.txt", true},
		{143, true, false, "crash-reports/crash.txt", true},
		// Caught startup failures exit 0 without ever being ready
		{0, false, false, "", true},
		// A stop during boot is not a crash
		{0, true, false, "", false},
	}
	for _, c := range cases {
		if got := isCrash(c.exitCode, c.stopRequested, c.wasReady, c.reportPath); got != c.want {
			t.Errorf("isCrash(%d, %v, %v, %q) = %v, want %v",
				c.exitCode, c.stopRequested, c.wasReady, c.reportPath, got, c.want)
		}
	}
}

func TestExitReportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, runtimespec.StateDir), 0755); err != nil {
		t.Fatal(err)
	}

	if got := readExitReport(dir); got != nil {
		t.Fatalf("absent report must read nil, got %+v", got)
	}

	want := &exitReport{
		ExitCode:       137,
		Crashed:        true,
		OomKilled:      true,
		ReportPath:     "crash-reports/crash-2026-07-08.txt",
		Excerpt:        "---- Minecraft Crash Report ----",
		ExitedAtUnixMs: time.Now().UnixMilli(),
	}
	writeExitReport(dir, want)
	got := readExitReport(dir)
	if got == nil {
		t.Fatal("persisted report did not read back")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}

	msg := msgExited(got).GetExited()
	if msg.GetExitCode() != 137 || !msg.GetOomKilled() || msg.GetExitedAtUnixMs() != want.ExitedAtUnixMs {
		t.Fatalf("msgExited mismatch: %+v", msg)
	}

	// Reports without a timestamp are treated as absent
	writeExitReport(dir, &exitReport{ExitCode: 1})
	if got := readExitReport(dir); got != nil {
		t.Fatalf("timestampless report must read nil, got %+v", got)
	}
}

func TestReadyPattern(t *testing.T) {
	ready := []string{
		`[12:34:56] [Server thread/INFO]: Done (9.418s)! For help, type "help"`,
		`[12:34:56] [Server thread/INFO] [minecraft/DedicatedServer]: Done (31.416s)! For help, type "help" or "?"`,
		`Done (0.5s)!`,
		`2011-07-31 10:11:12 [INFO] Done (9714672000ns)! For help, type "help" or "?"`,
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

func TestStripLogPrefix(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"[12:34:56] [Server thread/INFO]: Nick joined the game", "Nick joined the game"},
		{"[12Jul2026 21:57:22.786] [Server thread/INFO] [minecraft/MinecraftServer]: Nick left the game", "Nick left the game"},
		{"[21:57:22 INFO]: <Nick> hello", "<Nick> hello"},
		{"2011-07-31 10:11:12 [INFO] steve [/127.0.0.1:52941] logged in with entity id 229 at (...)", "steve [/127.0.0.1:52941] logged in with entity id 229 at (...)"},
		{"2011-07-31 10:12:30 [INFO] steve lost connection: disconnect.quitting", "steve lost connection: disconnect.quitting"},
	}
	for _, c := range cases {
		msg, ok := stripLogPrefix(c.line)
		if !ok || msg != c.want {
			t.Errorf("stripLogPrefix(%q) = %q, %v, want %q", c.line, msg, ok, c.want)
		}
	}
	if _, ok := stripLogPrefix("Nick joined the game"); ok {
		t.Error("stripLogPrefix matched a prefixless line")
	}
}

func TestLoginAndDisconnectPatterns(t *testing.T) {
	logins := []struct {
		msg  string
		name string
	}{
		{"Nick[/172.18.0.5:53412] logged in with entity id 261 at (8.5, 65.0, 8.5)", "Nick"},
		{"steve [/127.0.0.1:52941] logged in with entity id 229 at (135.5, 63.0, 240.3)", "steve"},
		{".BedrockKid[/172.18.0.9:41234] logged in with entity id 512 at (0.5, 70.0, 0.5)", ".BedrockKid"},
		{"Player With Space[/10.0.0.2:60000] logged in with entity id 7 at (1.0, 2.0, 3.0)", "Player With Space"},
		{"Nick[local:E:1a2b3c4d] logged in with entity id 12 at (0.0, 0.0, 0.0)", ""},
	}
	for _, c := range logins {
		m := loginPattern.FindStringSubmatch(c.msg)
		if c.name == "" {
			if m != nil {
				t.Errorf("loginPattern false positive on %q: %v", c.msg, m)
			}
			continue
		}
		if m == nil || m[1] != c.name {
			t.Errorf("loginPattern(%q) captured %v, want %q", c.msg, m, c.name)
		}
	}

	disconnects := []struct {
		msg  string
		name string
	}{
		{"Nick lost connection: Disconnected", "Nick"},
		{".BedrockKid lost connection: Timed out", ".BedrockKid"},
		{"steve lost connection: disconnect.quitting", "steve"},
	}
	for _, c := range disconnects {
		m := disconnectPattern.FindStringSubmatch(c.msg)
		if m == nil || m[1] != c.name {
			t.Errorf("disconnectPattern(%q) captured %v, want %q", c.msg, m, c.name)
		}
	}
}

func TestConsoleEvents(t *testing.T) {
	sup := &supervisor{startedAt: time.Now()}
	sup.events = newConsoleEvents(sup)
	events := sup.events

	// No panel session, sends drop, roster state still tracks
	events.handleLine("[12:34:56] [User Authenticator #1/INFO]: UUID of player Nick is 3b9f1c2d-0000-0000-0000-000000000001")
	events.handleLine("[12:34:56] [Server thread/INFO]: Nick[/172.18.0.5:53412] logged in with entity id 261 at (8.5, 65.0, 8.5)")
	if !events.isOnline("Nick") {
		t.Fatal("login line did not add Nick to the roster")
	}
	if got := events.uuids["Nick"]; got != "3b9f1c2d-0000-0000-0000-000000000001" {
		t.Errorf("uuid not captured, got %q", got)
	}

	// The broadcast join line after is a duplicate
	events.handleLine("[12:34:56] [Server thread/INFO]: Nick joined the game")
	if got := events.roster(); len(got) != 1 {
		t.Fatalf("duplicate join changed the roster: %v", got)
	}

	events.handleLine("[12:40:00] [Server thread/INFO]: Nick lost connection: Disconnected")
	if events.isOnline("Nick") {
		t.Fatal("disconnect line did not remove Nick from the roster")
	}
	events.handleLine("[12:40:00] [Server thread/INFO]: Nick left the game")
	if events.isOnline("Nick") {
		t.Fatal("duplicate leave resurrected Nick")
	}

	// Pre-login noise never enters the roster
	events.handleLine("[12:41:00] [Server thread/INFO]: /172.18.0.9:41234 lost connection: Took too long to log in")
	if len(events.roster()) != 0 {
		t.Fatalf("pre-login disconnect polluted the roster: %v", events.roster())
	}

	// Plugin servers still roster via auth lines
	events.handleLine("[12:42:00] [Server thread/INFO]: .BedrockKid[/172.18.0.9:41234] logged in with entity id 512 at (0.5, 70.0, 0.5)")
	if !events.isOnline(".BedrockKid") {
		t.Fatal("Floodgate-prefixed name did not roster")
	}

	// Legacy servers roster through the same auth lines
	events.handleLine("2011-07-31 10:11:12 [INFO] steve [/127.0.0.1:52941] logged in with entity id 229 at (135.5, 63.0, 240.3)")
	if !events.isOnline("steve") {
		t.Fatal("legacy login line did not roster")
	}
	events.handleLine("2011-07-31 10:12:30 [INFO] steve lost connection: disconnect.quitting")
	if events.isOnline("steve") {
		t.Fatal("legacy disconnect did not remove steve")
	}

	if got := events.roster(); !slices.Equal(got, []string{".BedrockKid"}) {
		t.Fatalf("final roster = %v, want [.BedrockKid]", got)
	}
}

func TestMatchDeath(t *testing.T) {
	sup := &supervisor{startedAt: time.Now()}
	events := newConsoleEvents(sup)
	sup.events = events
	for _, name := range []string{"Nick", ".BedrockKid", "Player With Space"} {
		events.online[name] = true
	}

	deaths := []struct {
		msg    string
		victim string
	}{
		{"Nick was slain by Zombie", "Nick"},
		{"Nick was shot by Skeleton using Bow of Doom", "Nick"},
		{"Nick drowned", "Nick"},
		{"Nick fell from a high place", "Nick"},
		{"Nick fell off a ladder", "Nick"},
		{"Nick tried to swim in lava to escape Blaze", "Nick"},
		{"Nick was killed by [Intentional Game Design]", "Nick"},
		{"Nick hit the ground too hard whilst trying to escape Spider", "Nick"},
		{"Nick withered away", "Nick"},
		{"Nick experienced kinetic energy", "Nick"},
		{"Nick didn't want to live in the same world as Warden", "Nick"},
		{".BedrockKid was slain by Zombie", ".BedrockKid"},
		{"Player With Space blew up", "Player With Space"},
	}
	for _, c := range deaths {
		if player, ok := events.matchDeath(c.msg); !ok || player != c.victim {
			t.Errorf("matchDeath(%q) = %q, %v, want %q", c.msg, player, ok, c.victim)
		}
	}
	notDeaths := []string{
		"Nick lost connection: Disconnected",
		"Nick issued server command: /help",
		"Nick moved too quickly!",
		"Nick joined the game",
		"Preparing spawn area: 95%",
		"Ghost was slain by Zombie",
	}
	for _, msg := range notDeaths {
		if player, ok := events.matchDeath(msg); ok {
			t.Errorf("matchDeath(%q) false positive for %q", msg, player)
		}
	}
}

func TestAssembleTickSample(t *testing.T) {
	s := assembleTickSample(0.3, 22.5, 10, 0, false)
	if s.GetTps() != 20 || s.GetMsptAvg() != 15 || s.GetMsptMax() != 22.5 {
		t.Errorf("healthy sample = %+v", s)
	}

	s = assembleTickSample(1.0, 9800, 10, 0, false)
	if s.GetTps() != 18 {
		t.Errorf("saturated tps = %v, want 18", s.GetTps())
	}
	if s.GetMsptAvg() < 55.5 || s.GetMsptAvg() > 55.6 {
		t.Errorf("saturated mspt = %v, want ~55.56", s.GetMsptAvg())
	}
	if s.GetMsptMax() != 9800 {
		t.Errorf("saturated msptMax = %v, want the 9800ms stall visible", s.GetMsptMax())
	}

	s = assembleTickSample(0.5, 80, 10, 0, false)
	if s.GetMsptMax() != 80 || s.GetTps() != 20 {
		t.Errorf("spike sample = %+v", s)
	}

	// Pegged threads with lag debt report real TPS
	s = assembleTickSample(1.0, 400, 10, 0.75, true)
	if s.GetTps() != 5 {
		t.Errorf("lag debt tps = %v, want 5", s.GetTps())
	}
	if s.GetMsptAvg() != 200 {
		t.Errorf("lag debt mspt = %v, want 200", s.GetMsptAvg())
	}

	// Debt just over budget beats the pessimistic bound
	s = assembleTickSample(1.0, 100, 10, 0.012, true)
	if s.GetTps() < 19.75 || s.GetTps() > 19.77 {
		t.Errorf("mild debt tps = %v, want ~19.76", s.GetTps())
	}

	// Runaway debt rate clamps instead of reporting zero
	s = assembleTickSample(1.0, 100, 10, 4.0, true)
	if s.GetTps() < 0.99 || s.GetTps() > 1.01 {
		t.Errorf("clamped tps = %v, want ~1", s.GetTps())
	}

	// Debt is only consulted while the thread is pegged
	s = assembleTickSample(0.5, 80, 10, 0.75, true)
	if s.GetTps() != 20 {
		t.Errorf("unpegged debt tps = %v, want 20", s.GetTps())
	}
}

func TestLagDebtRate(t *testing.T) {
	s := &supervisor{}
	if _, ok := s.lagDebtRate(); ok {
		t.Fatal("no lag lines must mean no usable debt")
	}

	s.recordLagLine(2000)
	if _, ok := s.lagDebtRate(); ok {
		t.Fatal("a single lag line has no observed spacing yet")
	}

	// A 2000ms confession every 2.67s is a 5 TPS server
	s.mu.Lock()
	s.lagDebtMs = 2000
	s.lagSpacing = 2670 * time.Millisecond
	s.lagAt = time.Now()
	s.mu.Unlock()
	rate, ok := s.lagDebtRate()
	if !ok || rate < 0.74 || rate > 0.76 {
		t.Fatalf("lagDebtRate = %v, %v, want ~0.75", rate, ok)
	}

	// Tight spacing expires at the 45s floor
	s.mu.Lock()
	s.lagAt = time.Now().Add(-50 * time.Second)
	s.mu.Unlock()
	if _, ok := s.lagDebtRate(); ok {
		t.Fatal("debt past the floor must expire")
	}

	// Sparse lines stay usable for 1.5x their spacing
	s.mu.Lock()
	s.lagSpacing = 2 * time.Minute
	s.lagAt = time.Now().Add(-170 * time.Second)
	s.mu.Unlock()
	if _, ok := s.lagDebtRate(); !ok {
		t.Fatal("sparse debt inside 1.5x spacing must stay usable")
	}

	// Five minutes caps usability no matter the spacing
	s.mu.Lock()
	s.lagSpacing = 10 * time.Minute
	s.lagAt = time.Now().Add(-6 * time.Minute)
	s.mu.Unlock()
	if _, ok := s.lagDebtRate(); ok {
		t.Fatal("debt older than the cap must expire")
	}
}

func TestLagPattern(t *testing.T) {
	modern := "Can't keep up! Is the server overloaded? Running 2861ms or 57 ticks behind"
	if m := lagPattern.FindStringSubmatch(modern); m == nil || m[1] != "2861" {
		t.Errorf("lagPattern missed modern lag line: %v", m)
	}
	legacy := "Can't keep up! Did the system time change, or is the server overloaded? Running 2000ms behind, skipping 40 tick(s)"
	if m := lagPattern.FindStringSubmatch(legacy); m == nil || m[1] != "2000" {
		t.Errorf("lagPattern missed legacy lag line: %v", m)
	}
	if lagPattern.MatchString("<Nick> Running 5000ms behind") {
		t.Error("lagPattern matched a chat line")
	}
}

func TestChatAndAdvancementPatterns(t *testing.T) {
	if m := chatPattern.FindStringSubmatch("<Nick> hello world"); m == nil || m[1] != "Nick" || m[2] != "hello world" {
		t.Errorf("chatPattern missed plain chat: %v", m)
	}
	if m := chatPattern.FindStringSubmatch("[Not Secure] <Nick> hi"); m == nil || m[1] != "Nick" {
		t.Errorf("chatPattern missed unsigned chat: %v", m)
	}
	if m := chatPattern.FindStringSubmatch("<Player With Space> bedrock says hi"); m == nil || m[1] != "Player With Space" {
		t.Errorf("chatPattern missed spaced name: %v", m)
	}
	if m := advancementPattern.FindStringSubmatch("Nick has made the advancement [Stone Age]"); m == nil || m[2] != "Stone Age" {
		t.Errorf("advancementPattern missed: %v", m)
	}
	if m := advancementPattern.FindStringSubmatch("Nick has completed the challenge [Uneasy Alliance]"); m == nil || m[2] != "Uneasy Alliance" {
		t.Errorf("advancementPattern missed challenge: %v", m)
	}
	if m := advancementPattern.FindStringSubmatch(".BedrockKid has reached the goal [Acquire Hardware]"); m == nil || m[1] != ".BedrockKid" {
		t.Errorf("advancementPattern missed prefixed name: %v", m)
	}
}

// Containers are always headless with a runtime agent port
func TestBuildJavaArgs(t *testing.T) {
	for _, key := range []string{"MEMORY", "INIT_MEMORY", "MAX_MEMORY", "AUTO_MEMORY",
		"USE_ZGC_FLAGS", "USE_AIKAR_FLAGS", "USE_MEOWICE_FLAGS", "USE_FLARE_FLAGS",
		"USE_SIMD_FLAGS", "ENABLE_JMX", "JMX_HOST", "JVM_OPTS", "JVM_XX_OPTS",
		"JVM_DD_OPTS", "EXTRA_ARGS", "TZ"} {
		t.Setenv(key, "")
	}

	spec := &runtimespec.LaunchSpec{Kind: runtimespec.LaunchKindJar, Jar: "server.jar", JavaMajor: 21}
	args, err := buildJavaArgs(spec, 43210)
	if err != nil {
		t.Fatal(err)
	}
	if args[len(args)-1] != "nogui" {
		t.Errorf("jar launch must end with nogui, got %v", args[len(args)-3:])
	}
	if !slices.Contains(args, "-Ddiscopanel.agent.port=43210") {
		t.Error("agent port property missing from argv")
	}

	args, err = buildJavaArgs(spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if a == "-javaagent:"+agentJarPath || len(a) > 25 && a[:25] == "-Ddiscopanel.agent.port=" {
			t.Errorf("agent disabled but argv contains %q", a)
		}
	}

	spec = &runtimespec.LaunchSpec{Kind: runtimespec.LaunchKindArgsFile, ArgsFile: "libraries/net/args.txt", JavaMajor: 21}
	args, err = buildJavaArgs(spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if args[len(args)-1] != "nogui" {
		t.Errorf("args-file launch must end with nogui, got %v", args[len(args)-3:])
	}

	spec = &runtimespec.LaunchSpec{Kind: runtimespec.LaunchKindCustom, Exec: "run.sh --custom", JavaMajor: 21}
	args, err = buildJavaArgs(spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if args[len(args)-1] == "nogui" {
		t.Error("custom launch must not get nogui appended")
	}

	// A user picked collector keeps the default Aikar G1 out
	spec = &runtimespec.LaunchSpec{Kind: runtimespec.LaunchKindJar, Jar: "server.jar", JavaMajor: 21}
	t.Setenv("JVM_OPTS", "-XX:+UseZGC")
	args, err = buildJavaArgs(spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "-XX:+UseG1GC") {
		t.Error("user GC choice must skip the default Aikar block")
	}
	if !slices.Contains(args, "-XX:+UseZGC") {
		t.Error("user GC flag missing from argv")
	}
	t.Setenv("JVM_OPTS", "")

	// Explicit flag sets still win over user opts
	t.Setenv("USE_MEOWICE_FLAGS", "true")
	t.Setenv("JVM_OPTS", "-XX:+UseZGC")
	args, err = buildJavaArgs(spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "-XX:+UseG1GC") {
		t.Error("explicit MeowIce request must still apply")
	}
	t.Setenv("USE_MEOWICE_FLAGS", "")
	t.Setenv("JVM_OPTS", "")
}

func TestUserSelectsGC(t *testing.T) {
	if !userSelectsGC("-Xmx4G -XX:+UseShenandoahGC") {
		t.Error("Shenandoah selection missed")
	}
	if !userSelectsGC("-XX:+UseConcMarkSweepGC") {
		t.Error("CMS selection missed")
	}
	if userSelectsGC("-XX:+UseStringDeduplication -XX:MaxGCPauseMillis=100") {
		t.Error("non GC flags misread as a collector choice")
	}
	if userSelectsGC("") {
		t.Error("empty opts misread as a collector choice")
	}
}

func TestParseTHPMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"always [madvise] never\n", "madvise"},
		{"[always] madvise never\n", "always"},
		{"always madvise [never]\n", "never"},
		{"garbage\n", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := parseTHPMode(c.in); got != c.want {
			t.Errorf("parseTHPMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPsiAvg10(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.pressure")
	content := "some avg10=12.34 avg60=5.00 avg300=1.00 total=123456\n" +
		"full avg10=3.21 avg60=1.00 avg300=0.10 total=6543\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if v, ok := psiAvg10(path, "some"); !ok || v != 12.34 {
		t.Errorf("psiAvg10 some = (%v, %v), want (12.34, true)", v, ok)
	}
	if v, ok := psiAvg10(path, "full"); !ok || v != 3.21 {
		t.Errorf("psiAvg10 full = (%v, %v), want (3.21, true)", v, ok)
	}
	if _, ok := psiAvg10(filepath.Join(dir, "missing"), "some"); ok {
		t.Error("missing pressure file should report unavailable")
	}
}

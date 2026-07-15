// Ends boots that go idle forever without crashing
package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
)

const (
	stallIdleCores  = 0.10
	stallIdleWindow = 4 * time.Minute
	stallSampleTick = 10 * time.Second
	dumpWaitTimeout = 10 * time.Second
	maxDumpLines    = 20000
	maxDumpThreads  = 4
	maxDumpFrames   = 32
)

// Watches for a never-ready JVM burning no CPU
func (s *supervisor) runBootStallWatch() {
	ticker := time.NewTicker(stallSampleTick)
	defer ticker.Stop()
	type point struct {
		at    time.Time
		ticks int64
	}
	var window []point
	for {
		select {
		case <-s.done():
			return
		case <-ticker.C:
		}
		if s.isReady() {
			return
		}
		// A dying boot already has an owner
		s.mu.Lock()
		arming := !s.bootFailedAt.IsZero()
		s.mu.Unlock()
		if arming {
			return
		}
		now := time.Now()
		ticks, ok := cpuTimes(s.pid)
		if !ok {
			continue
		}
		window = append(window, point{at: now, ticks: ticks})
		for len(window) > 0 && now.Sub(window[0].at) > stallIdleWindow+stallSampleTick {
			window = window[1:]
		}
		span := window[len(window)-1].at.Sub(window[0].at)
		if span < stallIdleWindow {
			continue
		}
		cores := float64(window[len(window)-1].ticks-window[0].ticks) / clockTicksPerSecond / span.Seconds()
		if cores >= stallIdleCores {
			continue
		}
		s.endStalledBoot(int(span.Minutes()))
		return
	}
}

// Collects forensics then ends the stalled boot
func (s *supervisor) endStalledBoot(idleMinutes int) {
	fmt.Printf("[discopanel-runtime] boot stalled, the JVM has been idle for %d minutes, collecting a thread dump\n", idleMinutes)
	if fatal := stallFatal(s.captureThreadDump()); fatal != nil {
		s.setFatalError(fatal)
	}
	s.mu.Lock()
	if s.bootFailedAt.IsZero() {
		s.bootFailedAt = time.Now()
	}
	proc := s.proc
	s.mu.Unlock()
	if proc == nil {
		return
	}
	fmt.Printf("[discopanel-runtime] boot stalled and cannot finish, shutting the JVM down\n")
	_ = proc.Signal(syscall.SIGTERM)
	select {
	case <-s.done():
	case <-time.After(killEscalation):
		_ = proc.Kill()
	}
}

// Console lines of one in-flight thread dump
type dumpCapture struct {
	active bool // Saw the dump header
	closed bool
	lines  []string
	done   chan struct{}
}

// Asks the JVM for a thread dump off its own stdout
func (s *supervisor) captureThreadDump() string {
	done := make(chan struct{})
	s.mu.Lock()
	s.dump = &dumpCapture{done: done}
	proc := s.proc
	s.mu.Unlock()
	if proc == nil {
		return ""
	}
	_ = proc.Signal(syscall.SIGQUIT)
	select {
	case <-done:
	case <-time.After(dumpWaitTimeout):
	case <-s.done():
	}
	s.mu.Lock()
	d := s.dump
	s.dump = nil
	s.mu.Unlock()
	if d == nil {
		return ""
	}
	return strings.Join(d.lines, "\n")
}

// Routes console lines into an armed dump capture
func (s *supervisor) collectDumpLine(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.dump
	if d == nil || d.closed {
		return
	}
	line = strings.TrimRight(line, "\r")
	if !d.active {
		if strings.HasPrefix(line, "Full thread dump") {
			d.active = true
		}
		return
	}
	if strings.HasPrefix(line, "JNI global refs") || len(d.lines) >= maxDumpLines {
		d.closed = true
		close(d.done)
		return
	}
	d.lines = append(d.lines, line)
}

// One parsed thread from a SIGQUIT dump
type dumpThread struct {
	name   string
	state  string
	frames []*agentv1.CrashFrame
}

var dumpThreadHeader = regexp.MustCompile(`^"([^"]+)"`)

var dumpStatePattern = regexp.MustCompile(`java\.lang\.Thread\.State: ([A-Z_]+)`)

var dumpFramePattern = regexp.MustCompile(`^\s*at ([\w.$]+)\.([\w$<>]+)\((.*)\)`)

func parseDumpThreads(dump string) []dumpThread {
	var threads []dumpThread
	var cur *dumpThread
	for _, line := range strings.Split(dump, "\n") {
		if m := dumpThreadHeader.FindStringSubmatch(line); m != nil {
			threads = append(threads, dumpThread{name: m[1]})
			cur = &threads[len(threads)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if m := dumpStatePattern.FindStringSubmatch(line); m != nil {
			cur.state = m[1]
			continue
		}
		m := dumpFramePattern.FindStringSubmatch(line)
		if m == nil || len(cur.frames) >= maxDumpFrames {
			continue
		}
		frame := &agentv1.CrashFrame{ClassName: m[1], MethodName: m[2], FileName: m[3]}
		if idx := strings.LastIndexByte(m[3], ':'); idx > 0 {
			if n, err := strconv.Atoi(m[3][idx+1:]); err == nil {
				frame.FileName = m[3][:idx]
				frame.Line = int32(n)
			}
		}
		cur.frames = append(cur.frames, frame)
	}
	return threads
}

// The tick thread and stuck workers tell the stall story
func pickStallThreads(threads []dumpThread) []dumpThread {
	var picked []dumpThread
	for _, t := range threads {
		if t.name == tickThreadComm && len(t.frames) > 0 {
			picked = append(picked, t)
			break
		}
	}
	for _, t := range threads {
		if len(picked) >= maxDumpThreads {
			break
		}
		if t.name == tickThreadComm || len(t.frames) == 0 {
			continue
		}
		if t.state == "BLOCKED" || strings.HasPrefix(t.name, "Worker-Main") {
			picked = append(picked, t)
		}
	}
	return picked
}

// Builds structured stall evidence from the dump text
func stallFatal(dump string) *agentv1.FatalError {
	if dump == "" {
		return nil
	}
	picked := pickStallThreads(parseDumpThreads(dump))
	if len(picked) == 0 {
		return nil
	}
	fatal := &agentv1.FatalError{Thread: picked[0].name}
	for _, t := range picked {
		state := strings.ToLower(t.state)
		if state == "" {
			state = "stuck"
		}
		fatal.Causes = append(fatal.Causes, &agentv1.CrashCause{
			Type:    "BootStall",
			Message: t.name + " is " + state,
			Frames:  t.frames,
		})
	}
	return fatal
}

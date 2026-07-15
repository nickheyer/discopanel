// Ends boots that go idle forever without crashing
package main

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"google.golang.org/protobuf/proto"
)

const (
	stallIdleCores  = 0.10
	stallIdleWindow = 4 * time.Minute
	stallSampleTick = 10 * time.Second
	dumpWaitTimeout = 10 * time.Second
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
	fmt.Printf("[discopanel-runtime] boot stalled, the JVM has been idle for %d minutes, asking it for a thread dump\n", idleMinutes)
	s.awaitStallDump()
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

// Asks the JVM agent for stall evidence, waits briefly
func (s *supervisor) awaitStallDump() {
	done := make(chan struct{})
	s.mu.Lock()
	s.dumpWait = done
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.dumpWait == done {
			s.dumpWait = nil
		}
		s.mu.Unlock()
	}()
	req := &agentv1.PanelMessage{Payload: &agentv1.PanelMessage_ThreadDumpRequest{
		ThreadDumpRequest: &agentv1.ThreadDumpRequest{},
	}}
	if !s.sendToJVM(req) {
		return
	}
	select {
	case <-done:
	case <-time.After(dumpWaitTimeout):
	case <-s.done():
	}
}

// Writes one framed message down the JVM agent link
func (s *supervisor) sendToJVM(msg *agentv1.PanelMessage) bool {
	s.mu.Lock()
	conn := s.jvmConn
	s.mu.Unlock()
	if conn == nil {
		return false
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return false
	}
	frame := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(frame, uint32(len(data)))
	copy(frame[4:], data)
	_, err = conn.Write(frame)
	return err == nil
}

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"google.golang.org/protobuf/proto"
)

// maxFrameSize caps loopback frames from the disco-agent mod (1 MiB).
const maxFrameSize = 1 << 20

// localModConn is the loopback connection from the disco-agent mod inside the
// server JVM. Frames are 4-byte big-endian length-prefixed protobuf messages
// using the same AgentMessage/PanelMessage envelopes as the panel session.
type localModConn struct {
	conn    net.Conn
	writeMu sync.Mutex
}

// runLocalListener accepts disco-agent mod connections on the container-local
// loopback port. Only the most recent connection is kept.
func (s *supervisor) runLocalListener() {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localAgentPort))
	if err != nil {
		fmt.Printf("[discopanel-runtime] WARN: local agent listener failed (%v), mod telemetry disabled\n", err)
		return
	}
	go func() {
		<-s.done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serveModConn(conn)
	}
}

func (s *supervisor) serveModConn(conn net.Conn) {
	mc := &localModConn{conn: conn}
	s.mu.Lock()
	old := s.modConn
	s.modConn = mc
	s.mu.Unlock()
	if old != nil {
		_ = old.conn.Close()
	}
	defer func() {
		s.mu.Lock()
		if s.modConn == mc {
			s.modConn = nil
		}
		s.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		msg, err := readFrame(conn)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("[discopanel-runtime] disco-agent mod disconnected: %v\n", err)
			}
			return
		}
		s.handleModMessage(msg)
	}
}

// handleModMessage processes one game-sourced message: readiness and command
// lists update supervisor state, everything else is relayed upstream as-is.
func (s *supervisor) handleModMessage(msg *agentv1.AgentMessage) {
	switch p := msg.GetPayload().(type) {
	case *agentv1.AgentMessage_Hello:
		fmt.Printf("[discopanel-runtime] disco-agent mod connected (%s)\n", p.Hello.GetVersion())
	case *agentv1.AgentMessage_Ready:
		// The mod's lifecycle hook can beat the console Done line; markReady
		// dedupes and notifies the panel itself.
		s.markReady(p.Ready.GetStartupSeconds())
		return
	case *agentv1.AgentMessage_CommandList:
		s.mu.Lock()
		s.commandList = p.CommandList.GetCommands()
		s.mu.Unlock()
	}
	s.send(msg)
}

// relayToMod forwards a panel message to the connected mod, if any.
func (s *supervisor) relayToMod(msg *agentv1.PanelMessage) {
	s.mu.Lock()
	mc := s.modConn
	s.mu.Unlock()
	if mc == nil {
		return
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	mc.writeMu.Lock()
	defer mc.writeMu.Unlock()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := mc.conn.Write(header[:]); err != nil {
		return
	}
	_, _ = mc.conn.Write(data)
}

func readFrame(conn net.Conn) (*agentv1.AgentMessage, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > maxFrameSize {
		return nil, fmt.Errorf("invalid frame length %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	var msg agentv1.AgentMessage
	if err := proto.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("invalid frame: %w", err)
	}
	return &msg, nil
}

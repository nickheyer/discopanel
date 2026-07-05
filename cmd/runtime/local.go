package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"google.golang.org/protobuf/proto"
)

// maxFrameSize caps loopback frames from the telemetry javaagent (1 MiB).
const maxFrameSize = 1 << 20

// runLocalListener accepts loopback connections from the telemetry javaagent
// inside the server JVM. Frames are 4-byte big-endian length-prefixed
// AgentMessage protobufs, relayed upstream to the panel.
func (s *supervisor) runLocalListener() {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localAgentPort))
	if err != nil {
		fmt.Printf("[discopanel-runtime] WARN: local agent listener failed (%v), JVM telemetry disabled\n", err)
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
		go s.serveAgentConn(conn)
	}
}

func (s *supervisor) serveAgentConn(conn net.Conn) {
	defer conn.Close()
	for {
		msg, err := readFrame(conn)
		if err != nil {
			if err != io.EOF {
				fmt.Printf("[discopanel-runtime] telemetry javaagent disconnected: %v\n", err)
			}
			return
		}
		s.handleAgentMessage(msg)
	}
}

// handleAgentMessage relays one JVM-sourced message upstream. Raw tick
// thread measurements are consumed here and forwarded as tick samples.
func (s *supervisor) handleAgentMessage(msg *agentv1.AgentMessage) {
	switch p := msg.GetPayload().(type) {
	case *agentv1.AgentMessage_Hello:
		fmt.Printf("[discopanel-runtime] telemetry javaagent connected (%s)\n", p.Hello.GetVersion())
	case *agentv1.AgentMessage_TickThreadSample:
		s.emitTickSample(p.TickThreadSample)
		return
	}
	s.send(msg)
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

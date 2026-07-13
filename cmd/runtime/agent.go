package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1/agentv1connect"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/proto"
)

const maxFrameSize = 1 << 20

func (s *supervisor) runLocalListener(ln net.Listener) {
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

func (s *supervisor) handleAgentMessage(msg *agentv1.AgentMessage) {
	switch p := msg.GetPayload().(type) {
	case *agentv1.AgentMessage_Hello:
		if p.Hello.GetSource() != agentv1.HelloSource_HELLO_SOURCE_JVM {
			return
		}
		fmt.Printf("[discopanel-runtime] telemetry javaagent connected (%s)\n", p.Hello.GetVersion())
		s.send(msg)
	case *agentv1.AgentMessage_TickThreadSample:
		s.emitTickSample(p.TickThreadSample)
	case *agentv1.AgentMessage_JvmSample:
		s.send(msg)
	case *agentv1.AgentMessage_FatalError:
		s.setFatalError(p.FatalError)
		s.send(msg)
		fmt.Printf("[discopanel-runtime] captured structured fatal error from the JVM\n")
	case *agentv1.AgentMessage_CaptureArmed:
		s.markCaptureArmed(p.CaptureArmed.GetContextsHooked())
	}
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

// Bidi stream to panel, drops telemetry under backpressure
type panelSession struct {
	sendCh chan *agentv1.AgentMessage
}

// Enqueues message for active session, drops if buffer full
func (s *supervisor) send(msg *agentv1.AgentMessage) {
	if msg == nil {
		return
	}
	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()
	if sess == nil {
		return
	}
	select {
	case sess.sendCh <- msg:
	default:
	}
}

// Builds HTTP/2 transport, TLS for https or h2c for http
func newPanelTransport(panelURL string) *http2.Transport {
	t := &http2.Transport{
		ReadIdleTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}
	u, err := url.Parse(panelURL)
	if err == nil && strings.EqualFold(u.Scheme, "https") {
		return t
	}
	t.AllowHTTP = true
	t.DialTLSContext = func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	return t
}

// Failures logged verbosely before dropping to hourly logging
const reconnectLogLimit = 3

// Dials panel and holds telemetry stream open for server lifetime
func (s *supervisor) runPanelSession() {
	client := agentv1connect.NewAgentServiceClient(
		&http.Client{Transport: newPanelTransport(s.agentSpec.PanelURL)},
		s.agentSpec.PanelURL,
	)

	backoff := time.Second
	failures := 0
	var lastLogAt time.Time
	for {
		select {
		case <-s.done():
			return
		default:
		}

		start := time.Now()
		err := s.panelSessionOnce(client)
		if err != nil && !s.exiting() {
			failures++
			if failures <= reconnectLogLimit || time.Since(lastLogAt) >= time.Hour {
				fmt.Printf("[discopanel-runtime] panel session ended (%v), reconnecting...\n", err)
				lastLogAt = time.Now()
			}
			s.reloadRotatedToken(err)
		}

		// A session that survived a while earns a fresh backoff
		if time.Since(start) > time.Minute {
			backoff = time.Second
			failures = 0
		}
		select {
		case <-s.done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (s *supervisor) exiting() bool {
	select {
	case <-s.done():
		return true
	default:
		return false
	}
}

// Picks up a token the panel rotated since container start
func (s *supervisor) reloadRotatedToken(err error) {
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		return
	}
	spec, rerr := runtimespec.ReadAgentSpec(dataDir)
	if rerr != nil || spec == nil || spec.Token == "" || spec.Token == s.agentSpec.Token {
		return
	}
	fmt.Println("[discopanel-runtime] agent token rotated, reloading it")
	s.agentSpec.Token = spec.Token
}

func (s *supervisor) panelSessionOnce(client agentv1connect.AgentServiceClient) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream := client.Session(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+s.agentSpec.Token)

	if err := stream.Send(s.msgHello()); err != nil {
		_ = stream.CloseRequest()
		return err
	}

	sess := &panelSession{sendCh: make(chan *agentv1.AgentMessage, 256)}
	s.mu.Lock()
	s.session = sess
	ready, readySeconds := s.ready, s.readySeconds
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.session == sess {
			s.session = nil
		}
		s.mu.Unlock()
	}()

	// Replays persisted exit report until a send succeeds
	if s.prevExit != nil {
		if err := stream.Send(msgExited(s.prevExit)); err != nil {
			return err
		}
		s.prevExit = nil
	}

	// Reconnect loses panel-visible state, resend it
	if ready {
		if err := stream.Send(msgReady(readySeconds)); err != nil {
			return err
		}
		s.sendRoster()
	}

	errCh := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case <-s.done():
				// Flushes queued messages before closing the stream
				for {
					select {
					case m := <-sess.sendCh:
						if err := stream.Send(m); err != nil {
							errCh <- err
							return
						}
					default:
						errCh <- stream.CloseRequest()
						return
					}
				}
			case m := <-sess.sendCh:
				if err := stream.Send(m); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()
	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				errCh <- err
				return
			}
			s.handlePanelMessage(msg)
		}
	}()

	err := <-errCh
	cancel()
	return err
}

// Routes panel messages to java stdin or tellraw chat
func (s *supervisor) handlePanelMessage(msg *agentv1.PanelMessage) {
	switch p := msg.GetPayload().(type) {
	case *agentv1.PanelMessage_ConsoleCommand:
		if err := s.writeConsole(p.ConsoleCommand.GetCommand()); err != nil {
			fmt.Printf("[discopanel-runtime] failed to write console command: %v\n", err)
		}
	case *agentv1.PanelMessage_ChatMessage:
		if err := s.broadcastChat(p.ChatMessage.GetSender(), p.ChatMessage.GetMessage()); err != nil {
			fmt.Printf("[discopanel-runtime] failed to broadcast chat: %v\n", err)
		}
	}
}

func (s *supervisor) msgHello() *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{
		ServerId:    s.agentSpec.ServerID,
		Source:      agentv1.HelloSource_HELLO_SOURCE_RUNTIME,
		Version:     runtimeVersion,
		Loader:      s.spec.Loader,
		McVersion:   s.spec.MCVersion,
		JavaMajor:   int32(s.spec.JavaMajor),
		HostThpMode: readHostTHPMode(),
	}}}
}

const saturatedBusyFraction = 0.98

const saturatedTickDebtMs = 2000

func assembleTickSample(busyFraction, longestBusyMs, windowSec float64) *agentv1.TickSample {
	msptAvg := 50 * busyFraction
	tps := 20.0
	if busyFraction >= saturatedBusyFraction && windowSec > 0 {
		debtRate := min((saturatedTickDebtMs/2)/(windowSec*1000), 0.95)
		tps = 20 * (1 - debtRate)
		msptAvg = 1000 / tps
	}
	msptMax := max(longestBusyMs, msptAvg)
	return &agentv1.TickSample{Tps: tps, MsptAvg: msptAvg, MsptMax: msptMax}
}

func (s *supervisor) emitTickSample(t *agentv1.TickThreadSample) {
	s.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_TickSample{
		TickSample: assembleTickSample(t.GetBusyFraction(), t.GetLongestBusyMs(), t.GetWindowSec()),
	}})
}

func msgReady(startupSeconds float64) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Ready{Ready: &agentv1.Ready{
		StartupSeconds: startupSeconds,
	}}}
}

func msgStopping() *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Stopping{Stopping: &agentv1.Stopping{}}}
}

func msgExited(r *exitReport) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Exited{Exited: &agentv1.Exited{
		ExitCode:           int32(r.ExitCode),
		Crashed:            r.Crashed,
		CrashReportPath:    r.ReportPath,
		CrashReportExcerpt: r.Excerpt,
		ExitedAtUnixMs:     r.ExitedAtUnixMs,
		OomKilled:          r.OomKilled,
		BootFailed:         r.BootFailed,
		WasReady:           r.WasReady,
		FatalError:         r.fatal(),
	}}}
}

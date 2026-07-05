package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1/agentv1connect"
	"golang.org/x/net/http2"
)

// panelSession is one live bidi stream to the panel. Messages are pushed
// through a bounded channel; when the panel is slow or unreachable, telemetry
// is dropped rather than blocking the server.
type panelSession struct {
	sendCh chan *agentv1.AgentMessage
}

// send enqueues a message for the active panel session, dropping it when no
// session is connected or the outbound buffer is full.
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

// runPanelSession dials the panel and holds the telemetry stream open for the
// life of the server process, reconnecting with capped backoff.
func (s *supervisor) runPanelSession() {
	backoff := time.Second
	for {
		select {
		case <-s.done():
			return
		default:
		}

		start := time.Now()
		err := s.panelSessionOnce()
		if err != nil && !s.exiting() {
			fmt.Printf("[discopanel-runtime] panel session ended (%v), reconnecting...\n", err)
		}

		// A session that survived a while earns a fresh backoff.
		if time.Since(start) > time.Minute {
			backoff = time.Second
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

func (s *supervisor) panelSessionOnce() error {
	// The panel serves Connect over h2c; bidi streams need a prior-knowledge
	// HTTP/2 cleartext client. h2 pings detect half-dead connections so the
	// reconnect loop can redial instead of hanging on a silent stream.
	httpClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP:       true,
			ReadIdleTimeout: 30 * time.Second,
			PingTimeout:     15 * time.Second,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
	client := agentv1connect.NewAgentServiceClient(httpClient, s.agentSpec.PanelURL)

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
	commands := s.commandList
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.session == sess {
			s.session = nil
		}
		s.mu.Unlock()
	}()

	// Re-establish panel-visible state lost by the reconnect.
	if ready {
		if err := stream.Send(msgReady(readySeconds)); err != nil {
			return err
		}
	}
	if len(commands) > 0 {
		if err := stream.Send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_CommandList{
			CommandList: &agentv1.CommandList{Commands: commands},
		}}); err != nil {
			return err
		}
	}

	errCh := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case <-s.done():
				// Flush whatever is queued (the exit report in particular)
				// before closing our half of the stream.
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

// handlePanelMessage dispatches a panel-to-agent message: console commands go
// to java stdin, chat messages are relayed to the disco-agent mod.
func (s *supervisor) handlePanelMessage(msg *agentv1.PanelMessage) {
	switch p := msg.GetPayload().(type) {
	case *agentv1.PanelMessage_ConsoleCommand:
		if err := s.writeConsole(p.ConsoleCommand.GetCommand()); err != nil {
			fmt.Printf("[discopanel-runtime] failed to write console command: %v\n", err)
		}
	case *agentv1.PanelMessage_ChatMessage:
		s.relayToMod(msg)
	}
}

func (s *supervisor) msgHello() *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Hello{Hello: &agentv1.Hello{
		ServerId:  s.agentSpec.ServerID,
		Source:    agentv1.HelloSource_HELLO_SOURCE_RUNTIME,
		Version:   runtimeVersion,
		Loader:    s.spec.Loader,
		McVersion: s.spec.MCVersion,
		JavaMajor: int32(s.spec.JavaMajor),
	}}}
}

func msgReady(startupSeconds float64) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Ready{Ready: &agentv1.Ready{
		StartupSeconds: startupSeconds,
	}}}
}

func msgStopping() *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Stopping{Stopping: &agentv1.Stopping{}}}
}

func msgExited(exitCode int, crashed bool, reportPath, excerpt string) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Exited{Exited: &agentv1.Exited{
		ExitCode:           int32(exitCode),
		Crashed:            crashed,
		CrashReportPath:    reportPath,
		CrashReportExcerpt: excerpt,
	}}}
}

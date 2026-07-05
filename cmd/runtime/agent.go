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

// Dials panel and holds telemetry stream open for server lifetime
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
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.session == sess {
			s.session = nil
		}
		s.mu.Unlock()
	}()

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
		ServerId:  s.agentSpec.ServerID,
		Source:    agentv1.HelloSource_HELLO_SOURCE_RUNTIME,
		Version:   runtimeVersion,
		Loader:    s.spec.Loader,
		McVersion: s.spec.MCVersion,
		JavaMajor: int32(s.spec.JavaMajor),
	}}}
}

// saturatedBusyFraction marks the tick thread as pegged for the window.
const saturatedBusyFraction = 0.98

// assembleTickSample turns the javaagent's thread cadence measurement into
// the panel tick sample. Below saturation the game's fixed 50ms cadence
// makes the busy share the mean tick cost. A pegged thread hides tick
// boundaries, so there the lag-line debt rate quantifies the slowdown.
func assembleTickSample(busyFraction, longestBusyMs, debtMs, intervalSec float64, lagKnown bool) *agentv1.TickSample {
	msptAvg := 50 * busyFraction
	msptMax := longestBusyMs
	tps := 20.0
	if busyFraction >= saturatedBusyFraction {
		if lagKnown {
			debtRate := min(debtMs/(intervalSec*1000), 0.95)
			tps = 20 * (1 - debtRate)
			msptAvg = 1000 / tps
		} else {
			msptAvg = 50
		}
		msptMax = msptAvg
	}
	if msptMax < msptAvg {
		msptMax = msptAvg
	}
	return &agentv1.TickSample{Tps: tps, MsptAvg: msptAvg, MsptMax: msptMax}
}

// emitTickSample sends the assembled tick sample upstream.
func (s *supervisor) emitTickSample(t *agentv1.TickThreadSample) {
	debtMs, intervalSec, lagKnown := s.events.lagDebt()
	s.send(&agentv1.AgentMessage{Payload: &agentv1.AgentMessage_TickSample{
		TickSample: assembleTickSample(t.GetBusyFraction(), t.GetLongestBusyMs(), debtMs, intervalSec, lagKnown),
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

func msgExited(exitCode int, crashed bool, reportPath, excerpt string) *agentv1.AgentMessage {
	return &agentv1.AgentMessage{Payload: &agentv1.AgentMessage_Exited{Exited: &agentv1.Exited{
		ExitCode:           int32(exitCode),
		Crashed:            crashed,
		CrashReportPath:    reportPath,
		CrashReportExcerpt: excerpt,
	}}}
}

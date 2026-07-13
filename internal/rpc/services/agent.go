package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/agent"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	agentv1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/agent/v1"
	"gorm.io/gorm"
)

// Serves the telemetry stream from each runtime supervisor
type AgentService struct {
	store *storage.Store
	hub   *agent.Hub
	log   *logger.Logger
}

func NewAgentService(store *storage.Store, hub *agent.Hub, log *logger.Logger) *AgentService {
	return &AgentService{store: store, hub: hub, log: log}
}

// Holds the long-lived bidirectional stream from one container
func (s *AgentService) Session(ctx context.Context, stream *connect.BidiStream[agentv1.AgentMessage, agentv1.PanelMessage]) error {
	server, err := s.authenticate(ctx, stream.RequestHeader().Get("Authorization"))
	if err != nil {
		return err
	}

	first, err := stream.Receive()
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("expected hello: %w", err))
	}
	hello := first.GetHello()
	if hello == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("first message must be a hello"))
	}
	if hello.GetServerId() != server.ID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("hello server id does not match token"))
	}

	sess := s.hub.Attach(server.ID, hello)
	defer s.hub.Detach(server.ID, sess)

	// Pumps panel-to-agent messages while this goroutine consumes telemetry
	sendErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sess.Closed():
				sendErr <- nil
				return
			case msg := <-sess.Outbound():
				if err := stream.Send(msg); err != nil {
					sendErr <- err
					return
				}
			}
		}
	}()

	for {
		select {
		case err := <-sendErr:
			return err
		default:
		}
		msg, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) || connect.CodeOf(err) == connect.CodeCanceled {
				return nil
			}
			return err
		}
		s.hub.HandleMessage(ctx, server.ID, msg)
	}
}

func (s *AgentService) authenticate(ctx context.Context, authHeader string) (*storage.Server, error) {
	token := strings.TrimPrefix(strings.TrimPrefix(authHeader, "Bearer "), "bearer ")
	if token == "" || !strings.HasPrefix(token, "dpa_") {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing agent token"))
	}
	sum := sha256.Sum256([]byte(token))
	server, err := s.store.GetServerByAgentTokenHash(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid agent token"))
		}
		// Store hiccup is retryable, not a bad token
		s.log.Error("agent: token lookup failed: %v", err)
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("token lookup failed, retry"))
	}
	return server, nil
}

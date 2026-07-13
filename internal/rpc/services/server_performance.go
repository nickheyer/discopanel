package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/activity"
	"github.com/nickheyer/discopanel/internal/autopilot"
	"github.com/nickheyer/discopanel/internal/metrics"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Scopes a dismissal to the finding's current content
func findingHash(f *autopilot.Finding) string {
	sum := sha256.Sum256([]byte(f.ID + "\x00" + f.Epoch))
	return hex.EncodeToString(sum[:])
}

// Runs autopilot checks and returns the health check findings
func (s *ServerService) GetServerPerformanceReport(ctx context.Context, req *connect.Request[v1.GetServerPerformanceReportRequest]) (*connect.Response[v1.GetServerPerformanceReportResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	serverCfg, err := s.store.GetServerProperties(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load server config: %w", err))
	}

	var agentConnected bool
	var findings []autopilot.Finding
	if s.metricsCollector != nil {
		m := s.metricsCollector.GetMetrics(server.ID)
		if m != nil {
			agentConnected = m.AgentConnected
		}
		findings = autopilot.Analyze(server, serverCfg, m)
	} else {
		findings = autopilot.Analyze(server, serverCfg, nil)
	}

	dismissals, err := s.store.GetFindingDismissals(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load dismissals: %w", err))
	}

	protoFindings := make([]*v1.PerformanceFinding, 0, len(findings))
	for i := range findings {
		f := &findings[i]
		d, ok := dismissals[f.ID]
		protoFindings = append(protoFindings, &v1.PerformanceFinding{
			Id:               f.ID,
			Severity:         f.Severity,
			Title:            f.Title,
			Detail:           f.Detail,
			FixId:            f.FixID,
			FixLabel:         f.FixLabel,
			FixArgs:          f.FixArgs,
			Source:           f.Source,
			Evidence:         f.Evidence,
			Action:           f.Action,
			Dismissed:        ok && d.ContentHash == findingHash(f),
			ActionLogStartMs: f.LedgerMs,
		})
	}

	return connect.NewResponse(&v1.GetServerPerformanceReportResponse{
		Findings:       protoFindings,
		AgentConnected: agentConnected,
	}), nil
}

// Hides or restores one finding, scoped to its current content
func (s *ServerService) DismissPerformanceFinding(ctx context.Context, req *connect.Request[v1.DismissPerformanceFindingRequest]) (*connect.Response[v1.DismissPerformanceFindingResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	if req.Msg.Restore {
		if err := s.store.DeleteFindingDismissal(ctx, server.ID, req.Msg.FindingId); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore finding: %w", err))
		}
		return connect.NewResponse(&v1.DismissPerformanceFindingResponse{}), nil
	}

	serverCfg, err := s.store.GetServerProperties(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load server config: %w", err))
	}
	var m *metrics.ServerMetrics
	if s.metricsCollector != nil {
		m = s.metricsCollector.GetMetrics(server.ID)
	}
	findings := autopilot.Analyze(server, serverCfg, m)
	for i := range findings {
		if findings[i].ID != req.Msg.FindingId {
			continue
		}
		if err := s.store.UpsertFindingDismissal(ctx, server.ID, findings[i].ID, findingHash(&findings[i])); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to dismiss finding: %w", err))
		}
		return connect.NewResponse(&v1.DismissPerformanceFindingResponse{}), nil
	}
	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("finding not found"))
}

// Returns the activity ledger for one server
func (s *ServerService) GetServerActions(ctx context.Context, req *connect.Request[v1.GetServerActionsRequest]) (*connect.Response[v1.GetServerActionsResponse], error) {
	if _, err := s.store.GetServer(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	rows, err := s.store.GetServerActions(ctx, req.Msg.Id, uint(req.Msg.AfterId))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load actions: %w", err))
	}
	actions := make([]*v1.ServerAction, 0, len(rows))
	for i := range rows {
		actions = append(actions, &v1.ServerAction{
			Id:        int64(rows[i].ID),
			Timestamp: timestamppb.New(rows[i].Timestamp),
			Source:    rows[i].Source,
			Name:      rows[i].Name,
			Message:   rows[i].Message,
			Attrs:     rows[i].Attrs,
			TraceId:   rows[i].TraceID,
		})
	}
	return connect.NewResponse(&v1.GetServerActionsResponse{Actions: actions}), nil
}

// Applies a finding's one-click fix to server config
func (s *ServerService) ApplyPerformanceFix(ctx context.Context, req *connect.Request[v1.ApplyPerformanceFixRequest]) (*connect.Response[v1.ApplyPerformanceFixResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	serverCfg, err := s.store.GetServerProperties(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load server config: %w", err))
	}

	message, err := autopilot.ApplyFix(server, serverCfg, req.Msg.FixId, req.Msg.FixArgs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.SaveServerProperties(ctx, serverCfg); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save server config: %w", err))
	}
	if err := s.store.UpdateServer(ctx, server); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save server: %w", err))
	}

	// Restart required to propogate config to the container
	restarting := s.recreateAfterConfigChange(ctx, server)

	s.rec.Record(ctx, server.ID, "fix.apply", activity.Attrs{"fix": req.Msg.FixId}, "applied fix: %s", message)
	s.log.Info("autopilot: applied fix %s to server %s (restarting=%v)", req.Msg.FixId, server.Name, restarting)
	return connect.NewResponse(&v1.ApplyPerformanceFixResponse{
		Message:    message,
		Restarting: restarting,
	}), nil
}

package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"connectrpc.com/connect"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Scopes a dismissal to the finding's current content
func findingHash(f *v1.PerformanceFinding) string {
	sum := sha256.Sum256([]byte(f.GetId() + "\x00" + f.GetEpoch()))
	return hex.EncodeToString(sum[:])
}

// Serves findings the doctor module published, panel adds nothing
func (s *ServerService) GetServerPerformanceReport(ctx context.Context, req *connect.Request[v1.GetServerPerformanceReportRequest]) (*connect.Response[v1.GetServerPerformanceReportResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	var agentConnected bool
	if s.metricsCollector != nil {
		if m := s.metricsCollector.GetMetrics(server.Id); m != nil {
			agentConnected = m.AgentConnected
		}
	}

	rows, err := s.store.GetFindingDismissals(ctx, server.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load dismissals: %w", err))
	}
	dismissals := make(map[string]*v1.FindingDismissal, len(rows))
	for _, d := range rows {
		dismissals[d.FindingId] = d
	}

	findings := runtimespec.ReadFindings(server.DataPath)
	for _, f := range findings {
		d, ok := dismissals[f.GetId()]
		f.Dismissed = ok && d.ContentHash == findingHash(f)
	}

	return connect.NewResponse(&v1.GetServerPerformanceReportResponse{
		Findings:       findings,
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
		if err := s.store.DeleteFindingDismissal(ctx, server.Id, req.Msg.FindingId); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore finding: %w", err))
		}
		return connect.NewResponse(&v1.DismissPerformanceFindingResponse{}), nil
	}

	for _, f := range runtimespec.ReadFindings(server.DataPath) {
		if f.GetId() != req.Msg.FindingId {
			continue
		}
		dismissal := &v1.FindingDismissal{
			ServerId:    server.Id,
			FindingId:   f.GetId(),
			ContentHash: findingHash(f),
			DismissedAt: timestamppb.Now(),
		}
		if err := s.store.UpsertFindingDismissal(ctx, dismissal); err != nil {
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
			Id:        int64(rows[i].Id),
			Timestamp: rows[i].Timestamp,
			Source:    rows[i].Source,
			Name:      rows[i].Name,
			Message:   rows[i].Message,
			Attrs:     rows[i].Attrs,
			TraceId:   rows[i].TraceId,
		})
	}
	return connect.NewResponse(&v1.GetServerActionsResponse{Actions: actions}), nil
}

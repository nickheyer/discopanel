package services

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/autopilot"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Runs autopilot checks and returns the health check findings
func (s *ServerService) GetServerPerformanceReport(ctx context.Context, req *connect.Request[v1.GetServerPerformanceReportRequest]) (*connect.Response[v1.GetServerPerformanceReportResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	serverCfg, err := s.store.GetServerConfig(ctx, server.ID)
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

	protoFindings := make([]*v1.PerformanceFinding, 0, len(findings))
	for _, f := range findings {
		protoFindings = append(protoFindings, &v1.PerformanceFinding{
			Id:       f.ID,
			Severity: f.Severity,
			Title:    f.Title,
			Detail:   f.Detail,
			FixId:    f.FixID,
			FixLabel: f.FixLabel,
		})
	}

	return connect.NewResponse(&v1.GetServerPerformanceReportResponse{
		Findings:       protoFindings,
		AgentConnected: agentConnected,
	}), nil
}

// ApplyPerformanceFix applies a finding's one-click fix to the server config.
func (s *ServerService) ApplyPerformanceFix(ctx context.Context, req *connect.Request[v1.ApplyPerformanceFixRequest]) (*connect.Response[v1.ApplyPerformanceFixResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}
	serverCfg, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load server config: %w", err))
	}

	message, err := autopilot.ApplyFix(serverCfg, req.Msg.FixId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.SaveServerConfig(ctx, serverCfg); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save server config: %w", err))
	}

	s.log.Info("autopilot: applied fix %s to server %s", req.Msg.FixId, server.Name)
	return connect.NewResponse(&v1.ApplyPerformanceFixResponse{
		Message:         message,
		RestartRequired: true,
	}), nil
}

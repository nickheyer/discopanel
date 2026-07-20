package services

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Ranges longer than this are served bucketed
const rawHistoryWindow = 6 * time.Hour

// Returns stored metrics samples for one server's charts
func (s *ServerService) GetServerMetricsHistory(ctx context.Context, req *connect.Request[v1.GetServerMetricsHistoryRequest]) (*connect.Response[v1.GetServerMetricsHistoryResponse], error) {
	if _, err := s.store.GetServer(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	to := time.Now()
	if req.Msg.To != nil {
		to = req.Msg.To.AsTime()
	}
	from := to.Add(-time.Hour)
	if req.Msg.From != nil {
		from = req.Msg.From.AsTime()
	}
	if !from.Before(to) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("from must be before to"))
	}

	resolution := int(req.Msg.Resolution)
	if resolution == 0 && to.Sub(from) > rawHistoryWindow {
		resolution = 300
	}

	samples, err := s.store.GetMetricsHistory(ctx, req.Msg.Id, from, to, resolution)
	if err != nil {
		s.log.Error("Failed to load metrics history for %s: %v", req.Msg.Id, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to load metrics history"))
	}

	return connect.NewResponse(&v1.GetServerMetricsHistoryResponse{
		Samples:    samples,
		Resolution: int32(resolution),
	}), nil
}

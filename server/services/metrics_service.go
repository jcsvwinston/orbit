package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/orbit/server/alerts"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// MetricsService is the UI-facing surface for the host-metrics samples the
// server retains (ADR-013). Without a data directory it answers empty
// lists: the registry keeps the last sample only, and ListNodes carries it.
type MetricsService struct {
	state *State
}

// NewMetricsService constructs the service.
func NewMetricsService(state *State) *MetricsService { return &MetricsService{state: state} }

// defaultHostMetricsLimit bounds ListHostMetrics when the request leaves
// the limit unset: a week of ten-second heartbeats is 60480 samples, and a
// chart does not need them all.
const defaultHostMetricsLimit = 2000

// ListHostMetrics returns the node's samples, oldest first, within the
// retention window and since the instant asked for.
func (s *MetricsService) ListHostMetrics(ctx context.Context, req *connect.Request[adminv1.ListHostMetricsRequest]) (*connect.Response[adminv1.ListHostMetricsResponse], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	if req.Msg.GetNodeId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("admin server: node_id is required"))
	}
	out := &adminv1.ListHostMetricsResponse{}
	if s.state.Store == nil {
		return connect.NewResponse(out), nil
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 || limit > defaultHostMetricsLimit {
		limit = defaultHostMetricsLimit
	}
	var since time.Time
	if req.Msg.GetSince() != nil {
		since = req.Msg.GetSince().AsTime()
	}
	if err := s.state.Store.Flush(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("admin server: metrics store: %w", err))
	}
	samples, err := s.state.Store.HostMetricsHistory(req.Msg.GetNodeId(), since, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("admin server: metrics store: %w", err))
	}
	for _, smp := range samples {
		out.Samples = append(out.Samples, &adminv1.HostMetricsSample{Time: timestamppb.New(smp.Time), Metrics: smp.Metrics})
	}
	return connect.NewResponse(out), nil
}

// timeDuration converts the rules file duration.
func timeDuration(d alerts.Duration) time.Duration { return time.Duration(d) }

var _ adminv1connect.MetricsServiceHandler = (*MetricsService)(nil)

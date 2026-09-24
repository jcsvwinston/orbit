package services

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/orbit/server/alerts"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// AlertService is the UI-facing surface for alerts: the rules the server
// evaluates, the alerts they raised, and a stream of state changes. A
// server with no rules answers empty lists and a stream that carries
// nothing.
type AlertService struct {
	state *State
}

// NewAlertService constructs the service.
func NewAlertService(state *State) *AlertService { return &AlertService{state: state} }

// defaultAlertListLimit bounds ListAlerts when the request leaves the
// limit unset.
const defaultAlertListLimit = 500

// ListAlertRules returns the configured rules.
func (s *AlertService) ListAlertRules(_ context.Context, _ *connect.Request[adminv1.ListAlertRulesRequest]) (*connect.Response[adminv1.ListAlertRulesResponse], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	out := &adminv1.ListAlertRulesResponse{}
	for _, r := range s.state.Alerts.Rules() {
		out.Rules = append(out.Rules, ruleToProto(r))
	}
	return connect.NewResponse(out), nil
}

// ListAlerts returns the firing alerts, newest first, and the resolved
// ones after them when asked.
func (s *AlertService) ListAlerts(_ context.Context, req *connect.Request[adminv1.ListAlertsRequest]) (*connect.Response[adminv1.ListAlertsResponse], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 || limit > defaultAlertListLimit {
		limit = defaultAlertListLimit
	}
	out := &adminv1.ListAlertsResponse{}
	for _, a := range s.state.Alerts.Alerts(req.Msg.GetIncludeResolved(), limit) {
		out.Alerts = append(out.Alerts, alertToProto(a))
	}
	return connect.NewResponse(out), nil
}

// StreamAlerts sends every state change until the caller goes away.
func (s *AlertService) StreamAlerts(ctx context.Context, _ *connect.Request[adminv1.StreamAlertsRequest], stream *connect.ServerStream[adminv1.Alert]) error {
	if s == nil || s.state == nil {
		return connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	if s.state.Alerts == nil {
		<-ctx.Done()
		return nil
	}
	ch, cancel := s.state.Alerts.Subscribe(64)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case a := <-ch:
			if err := stream.Send(alertToProto(a)); err != nil {
				return err
			}
		}
	}
}

func ruleToProto(r alerts.Rule) *adminv1.AlertRule {
	return &adminv1.AlertRule{
		Id: r.ID, Name: r.Name, Metric: r.Metric, Op: r.Op, Threshold: r.Threshold,
		ForDuration: durationpb.New(timeDuration(r.For)),
		Severity:    severityToProto(r.Severity),
		NodeIds:     append([]string(nil), r.NodeIDs...),
		Channels:    append([]string(nil), r.Channels...),
	}
}

func alertToProto(a alerts.Alert) *adminv1.Alert {
	out := &adminv1.Alert{
		Id: a.ID, RuleId: a.RuleID, NodeId: a.NodeID, Value: a.Value, Message: a.Message,
		State: adminv1.AlertState_ALERT_STATE_FIRING,
	}
	if !a.FiredAt.IsZero() {
		out.FiredAt = timestamppb.New(a.FiredAt)
	}
	if a.State == alerts.Resolved {
		out.State = adminv1.AlertState_ALERT_STATE_RESOLVED
		if !a.ResolvedAt.IsZero() {
			out.ResolvedAt = timestamppb.New(a.ResolvedAt)
		}
	}
	return out
}

func severityToProto(s string) adminv1.AlertSeverity {
	switch s {
	case "info":
		return adminv1.AlertSeverity_ALERT_SEVERITY_INFO
	case "warning":
		return adminv1.AlertSeverity_ALERT_SEVERITY_WARNING
	case "critical":
		return adminv1.AlertSeverity_ALERT_SEVERITY_CRITICAL
	}
	return adminv1.AlertSeverity_ALERT_SEVERITY_UNSPECIFIED
}

var _ adminv1connect.AlertServiceHandler = (*AlertService)(nil)

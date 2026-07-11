package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/orbit/server/nodes"
	"github.com/jcsvwinston/orbit/server/routing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// defaultAuditListLimit bounds ListAudit when the request leaves the
// limit unset.
const defaultAuditListLimit = 500

// ManageService implements adminv1connect.ManageServiceHandler — the
// UI-facing surface for the Access control and Audit log screens.
//
// GetRbac routes to a connected agent (the application's authorizer is
// the source of truth); ListAudit reads the server's own fleet-plane
// audit ring.
type ManageService struct {
	state   *State
	Timeout time.Duration
}

// NewManageService constructs the handler. timeout <= 0 defaults to 10s.
func NewManageService(state *State, timeout time.Duration) *ManageService {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ManageService{state: state, Timeout: timeout}
}

// GetRbac snapshots the Casbin roles/policies of one connected agent.
// An empty node_id picks the first connected agent (single-node fleets
// need no selector; multi-node fleets usually share one policy store).
func (s *ManageService) GetRbac(_ context.Context, req *connect.Request[adminv1.GetRbacRequest]) (*connect.Response[adminv1.GetRbacResponse], error) {
	if s == nil || s.state == nil || s.state.Rbac == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: rbac routing not initialized"))
	}

	nodeID := req.Msg.GetNodeId()
	entry, ok := s.pickAgent(nodeID)
	if !ok {
		if nodeID != "" {
			return nil, connect.NewError(connect.CodeNotFound,
				fmt.Errorf("admin server: node %q is not connected", nodeID))
		}
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: no agents connected"))
	}

	id, ch, cancel, err := s.state.Rbac.Begin()
	if err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}
	frame := &adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_Rbac{Rbac: &adminv1.RbacRequest{RequestId: id}},
			},
		},
	}
	if !nodes.TryEnqueue(entry, frame) {
		cancel()
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: agent send buffer full or stream closing"))
	}

	resp, err := routing.WaitRbac(ch, cancel, s.Timeout)
	if err != nil {
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if resp.GetError() != "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(resp.GetError()))
	}
	return connect.NewResponse(&adminv1.GetRbacResponse{
		Roles:    resp.GetRoles(),
		Policies: resp.GetPolicies(),
	}), nil
}

// ListAudit returns the newest fleet-plane audit entries, newest first.
func (s *ManageService) ListAudit(_ context.Context, req *connect.Request[adminv1.ListAuditRequest]) (*connect.Response[adminv1.ListAuditResponse], error) {
	if s == nil || s.state == nil || s.state.Audit == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: audit ring not initialized"))
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 || limit > defaultAuditListLimit {
		limit = defaultAuditListLimit
	}
	entries := s.state.Audit.List(limit)
	out := &adminv1.ListAuditResponse{Entries: make([]*adminv1.AuditEntry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, &adminv1.AuditEntry{
			Time:   timestamppb.New(e.Time),
			Actor:  e.Actor,
			Action: e.Action,
			Target: e.Target,
			NodeId: e.NodeID,
		})
	}
	return connect.NewResponse(out), nil
}

// pickAgent mirrors DataStudioService.pickAgent for node addressing
// without a model constraint.
func (s *ManageService) pickAgent(nodeID string) (*nodes.Entry, bool) {
	if nodeID != "" {
		return s.state.Nodes.Lookup(nodeID)
	}
	// No constraint: first entry wins.
	var pick *nodes.Entry
	s.state.Nodes.ForEach(func(e *nodes.Entry) {
		if pick == nil {
			pick = e
		}
	})
	return pick, pick != nil
}

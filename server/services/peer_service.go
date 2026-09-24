package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/server/peers"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// PeerService is the inbound side of the mesh (ADR-014): what another
// admin server pushes here. It records the peer's nodes as remote entries
// of this registry, fans the peer's events out to this server's UI
// subscribers and replay ring, and keeps the peer's host-metrics samples
// on the remote entries. It retains nothing of it: the origin does.
type PeerService struct {
	state *State
}

// NewPeerService constructs the handler.
func NewPeerService(state *State) *PeerService { return &PeerService{state: state} }

// Sync is the bidi entry point for a peer. The first frame must be a
// PeerHello; the handler answers with its own and then reads until the
// stream ends, forgetting the peer's nodes when it does.
func (s *PeerService) Sync(ctx context.Context, stream *connect.BidiStream[adminv1.PeerFrame, adminv1.PeerFrame]) error {
	if s == nil || s.state == nil {
		return connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	first, err := stream.Receive()
	if err != nil {
		return connect.NewError(connect.CodeAborted, fmt.Errorf("admin server: peer hello: %w", err))
	}
	hello := first.GetHello()
	if hello == nil || strings.TrimSpace(hello.GetServerId()) == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("admin server: a peer's first frame must be a PeerHello with a server_id"))
	}
	via := strings.TrimSpace(hello.GetServerId())
	if via == s.state.ServerID {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("admin server: peer %q is this server; a server does not peer with itself", via))
	}
	if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Hello{Hello: &adminv1.PeerHello{
		ServerId: s.state.ServerID, AgentEndpoint: s.state.Peers.Self(),
	}}}); err != nil {
		return err
	}
	s.state.Logger.Info("admin server: peer connected", "peer", via, "agent_endpoint", hello.GetAgentEndpoint(), "nodes", len(hello.GetNodeIds()))
	defer func() {
		s.state.Nodes.RemoveRemote("", via)
		s.state.Logger.Info("admin server: peer disconnected", "peer", via)
	}()

	for {
		fr, err := stream.Receive()
		if err != nil {
			if ctx.Err() != nil || connect.CodeOf(err) == connect.CodeCanceled {
				return nil
			}
			return nil
		}
		switch body := fr.GetBody().(type) {
		case *adminv1.PeerFrame_Node:
			if body.Node != nil {
				s.state.Nodes.SetRemote(peers.NodeFromProto(body.Node), via)
			}
		case *adminv1.PeerFrame_NodeGone:
			s.state.Nodes.RemoveRemote(body.NodeGone.GetNodeId(), via)
		case *adminv1.PeerFrame_Event:
			if body.Event != nil {
				// A peer's event reaches this server's UIs and its replay
				// ring; it is not retained here — the origin retains it —
				// and it is not relayed on: the mesh is one hop.
				s.state.Replay.Push(body.Event)
				s.state.EventBus.Publish(body.Event)
				if c := s.state.Counters; c != nil {
					c.EventsRelayedInTotal.Inc()
				}
			}
		case *adminv1.PeerFrame_HostMetrics:
			if hm := body.HostMetrics; hm != nil && hm.GetSample() != nil {
				s.state.Nodes.SetHostMetrics(hm.GetNodeId(), hm.GetSample().GetMetrics())
				s.state.Nodes.Touch(hm.GetNodeId(), time.Now().UTC())
			}
		case *adminv1.PeerFrame_Hello:
			// A repeated hello refreshes nothing; the first one named the peer.
		}
	}
}

var _ adminv1connect.PeerServiceHandler = (*PeerService)(nil)

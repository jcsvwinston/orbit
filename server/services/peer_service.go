package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

	// warned holds the server_ids already logged as accepted although the
	// allowlist would refuse them: one WARN per peer per process.
	warnedMu sync.Mutex
	warned   map[string]bool
}

// NewPeerService constructs the handler.
func NewPeerService(state *State) *PeerService {
	return &PeerService{state: state, warned: map[string]bool{}}
}

// configuredPeerWait bounds how long Sync waits for this server's own link
// to a configured endpoint to hear who answers there, before it decides a
// peer is not configured. Two servers that start together dial each other
// at once, and each learns the other's server_id from that answer.
const configuredPeerWait = 5 * time.Second

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
	// The answer goes out before the decision below: the peer learns who
	// answers at the endpoint it dialled from this hello, and two servers
	// that start together each wait for that answer before accepting the
	// other. Deciding first would leave both waiting.
	if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Hello{Hello: &adminv1.PeerHello{
		ServerId: s.state.ServerID, AgentEndpoint: s.state.Peers.Self(),
	}}}); err != nil {
		return err
	}
	cn, hasCert := certificateNode(ctx)
	if s.state.PeerAllowlist {
		if reason := s.refusal(ctx, via, cn, hasCert); reason != "" {
			s.state.Logger.Warn("admin server: peer refused",
				"peer", via, "agent_endpoint", hello.GetAgentEndpoint(), "certificate_cn", cn, "reason", reason)
			return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("admin server: peer %q refused: %s (this server accepts only the peers it dials)", via, reason))
		}
	} else {
		go s.warnIfRefusedAtV2(via, hello.GetAgentEndpoint(), cn, hasCert)
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

// refusal says why the allowlist refuses the peer that announced via, or
// "" when it accepts it. A verified client certificate must name the
// server it announces; the server must be one this server dials — the
// server_id that answered on its own link to one of its --peers endpoints.
func (s *PeerService) refusal(ctx context.Context, via, cn string, hasCert bool) string {
	if hasCert && cn != via {
		return fmt.Sprintf("its client certificate names %q, not the server_id it announced", cn)
	}
	if !s.state.Peers.AwaitConfigured(ctx, via, configuredPeerWait) {
		return "it is not the server that answers at any of this server's --peers endpoints"
	}
	return ""
}

// warnIfRefusedAtV2 is the 1.x side of the allowlist: the peer was
// accepted, and if the allowlist would have refused it — the check Orbit
// 2.0 applies on every server — the deployment hears it once per peer, with
// the reason. It runs beside the stream: the wait for this server's own
// link to answer must not delay a peer that 1.x accepts anyway.
func (s *PeerService) warnIfRefusedAtV2(via, endpoint, cn string, hasCert bool) {
	s.warnedMu.Lock()
	seen := s.warned[via]
	s.warnedMu.Unlock()
	if seen {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), configuredPeerWait+time.Second)
	defer cancel()
	reason := s.refusal(ctx, via, cn, hasCert)
	if reason == "" {
		return
	}
	s.warnedMu.Lock()
	seen = s.warned[via]
	s.warned[via] = true
	s.warnedMu.Unlock()
	if seen {
		return
	}
	s.state.Logger.Warn("admin server: accepted a peer that Orbit 2.0 refuses",
		"peer", via, "agent_endpoint", endpoint, "certificate_cn", cn, "reason", reason,
		"hint", "Orbit 2.0 accepts PeerService.Sync only from the servers in --peers (DEP-2026-005): list this peer there, "+
			"name its client certificate after its --server-id under mutual TLS, and set --peer-allowlist to refuse the rest now")
}

var _ adminv1connect.PeerServiceHandler = (*PeerService)(nil)

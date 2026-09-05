package services

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/orbit/server/auth"
	"github.com/jcsvwinston/orbit/server/nodes"
	"github.com/jcsvwinston/orbit/server/routing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// ControlService implements adminv1connect.ControlServiceHandler.
type ControlService struct {
	state *State

	// EventChannelSize is the per-StreamEvents subscription buffer.
	EventChannelSize int

	// SnapshotTimeout caps how long GetSnapshot waits for the agent.
	SnapshotTimeout time.Duration

	// MaxStreamsPerOperator caps the StreamEvents subscriptions one
	// identity may hold at once; the next open is refused with
	// ResourceExhausted. Every open used to fan the whole ring out and
	// push a new aggregate filter to every agent, with no ceiling per
	// operator (OR-26). Zero means the default, 8.
	MaxStreamsPerOperator int

	// MaxReplayEvents caps how many ring-buffer events include_recent
	// replays into a fresh stream. Zero means the default, 500.
	MaxReplayEvents int

	streamsMu sync.Mutex
	streams   map[string]int // open StreamEvents per identity subject

	// The agent-side aggregate filter is recomputed when a subscription
	// opens or closes. A burst of opens (the SPA reconnecting every
	// panel at once) used to push it to every agent once per
	// subscription; the push is now coalesced within pushDebounce.
	pushMu    sync.Mutex
	pushTimer *time.Timer
}

const (
	defaultMaxStreamsPerOperator = 8
	defaultMaxReplayEvents       = 500
	pushDebounce                 = 100 * time.Millisecond
)

// NewControlService constructs the handler.
func NewControlService(state *State, eventChannelSize int, snapshotTimeout time.Duration) *ControlService {
	if eventChannelSize <= 0 {
		eventChannelSize = 256
	}
	if snapshotTimeout <= 0 {
		snapshotTimeout = 5 * time.Second
	}
	return &ControlService{
		state:            state,
		EventChannelSize: eventChannelSize,
		SnapshotTimeout:  snapshotTimeout,
	}
}

// GetSelf echoes the authenticated caller's identity (resolved by the UI auth
// chain, carried in the request context) plus the server's build version, so
// the UI can show who the operator is audited as and which server they hit.
func (s *ControlService) GetSelf(ctx context.Context, _ *connect.Request[adminv1.GetSelfRequest]) (*connect.Response[adminv1.SelfInfo], error) {
	id := auth.IdentityFromContext(ctx)
	return connect.NewResponse(&adminv1.SelfInfo{
		Subject:       id.Subject,
		Email:         id.Email,
		Role:          id.Role,
		ReadOnly:      id.ReadOnly,
		ServerVersion: serverVersion(),
	}), nil
}

// serverVersion returns the running binary's module version — the same value
// `admin-server --version` prints. "devel" for source builds.
func serverVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "devel"
}

// ListNodes returns a stable view of every connected agent.
func (s *ControlService) ListNodes(ctx context.Context, req *connect.Request[adminv1.ListNodesRequest]) (*connect.Response[adminv1.ListNodesResponse], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}

	infos := s.state.Nodes.List()
	out := &adminv1.ListNodesResponse{
		Nodes: make([]*adminv1.NodeInfo, 0, len(infos)),
	}
	for _, info := range infos {
		out.Nodes = append(out.Nodes, &adminv1.NodeInfo{
			NodeId:      info.NodeID,
			Version:     info.Version,
			Labels:      cloneLabels(info.Labels),
			StartedAt:   timestamppb.New(info.StartedAt),
			LastSeenAt:  timestamppb.New(info.LastSeenAt),
			Connected:   info.Connected,
			HostMetrics: info.HostMetrics,
		})
	}
	return connect.NewResponse(out), nil
}

// StreamEvents subscribes to live events. The handler:
//   - registers a new EventBus subscription with the requested filter,
//   - rebuilds and pushes the agent-side aggregate filter to every
//     connected agent (so they switch on/refine ingress as needed),
//   - optionally replays the recent ring buffer first when
//     include_recent is set,
//   - drains the subscription channel into the response stream until ctx
//     is cancelled.
func (s *ControlService) StreamEvents(ctx context.Context, req *connect.Request[adminv1.StreamEventsRequest], stream *connect.ServerStream[adminv1.Event]) error {
	if s == nil || s.state == nil {
		return connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	body := req.Msg

	release, err := s.acquireStreamSlot(ctx)
	if err != nil {
		return err
	}
	defer release()

	sub, cancel := s.state.EventBus.Subscribe(body.GetFilter(), body.GetSamplingRate(), s.EventChannelSize)
	defer cancel()

	// Recompute the agent-side aggregate Subscribe and push it to every
	// connected agent — coalesced, so a burst of opens is one push. The
	// agent replaces any previous server-aggregate sub atomically (proto
	// guarantees this on duplicate subscription_id).
	s.schedulePushAggregate()
	defer s.schedulePushAggregate()

	if body.GetIncludeRecent() && s.state.Replay != nil {
		limit := s.MaxReplayEvents
		if limit <= 0 {
			limit = defaultMaxReplayEvents
		}
		for _, ev := range s.state.Replay.Snapshot(body.GetFilter(), limit) {
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-sub.Ch():
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// GetSnapshot routes a snapshot request to the right agent and waits for
// the matching SnapshotResponse over its bidi stream.
func (s *ControlService) GetSnapshot(ctx context.Context, req *connect.Request[adminv1.GetSnapshotRequest]) (*connect.Response[adminv1.Snapshot], error) {
	if s == nil || s.state == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("admin server: state not initialized"))
	}
	body := req.Msg
	if body.GetNodeId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("admin server: node_id is required"))
	}

	entry, ok := s.state.Nodes.Lookup(body.GetNodeId())
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound,
			fmt.Errorf("admin server: node %q is not connected", body.GetNodeId()))
	}

	id, ch, cancel, err := s.state.Snapshots.Begin()
	if err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}

	frame := &adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_SnapshotRequest{
					SnapshotRequest: &adminv1.SnapshotRequest{
						RequestId: id,
						Type:      body.GetType(),
					},
				},
			},
		},
	}
	if !nodes.TryEnqueue(entry, frame) {
		cancel()
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("admin server: agent send buffer full or stream closing"))
	}

	resp, err := routing.Wait(ch, cancel, s.SnapshotTimeout)
	if err != nil {
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if resp == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("admin server: empty snapshot response"))
	}
	if resp.Error != "" {
		return nil, connect.NewError(connect.CodeUnknown, errors.New(resp.Error))
	}

	return connect.NewResponse(&adminv1.Snapshot{
		NodeId:      body.GetNodeId(),
		Type:        body.GetType(),
		GeneratedAt: timestamppb.Now(),
		PayloadJson: resp.PayloadJson,
	}), nil
}

// aggregateFrame builds the frame carrying the current agent-side
// aggregate demand: a Subscribe with the union filter + sampling rates,
// or an Unsubscribe when zero UI subscribers remain (clear ingress).
func aggregateFrame(bus *routing.EventBus) *adminv1.Frame {
	const aggregateID = "server-aggregate"

	agg := bus.AggregateFilter()
	if agg == nil {
		return &adminv1.Frame{
			Body: &adminv1.Frame_Command{
				Command: &adminv1.Command{
					Body: &adminv1.Command_Unsubscribe{
						Unsubscribe: &adminv1.Unsubscribe{
							SubscriptionId: aggregateID,
						},
					},
				},
			},
		}
	}
	return &adminv1.Frame{
		Body: &adminv1.Frame_Command{
			Command: &adminv1.Command{
				Body: &adminv1.Command_Subscribe{
					Subscribe: &adminv1.Subscribe{
						SubscriptionId: aggregateID,
						Filter:         agg,
						SamplingRate:   bus.AggregateSampling(),
					},
				},
			},
		},
	}
}

// PushAggregate ships the current aggregate demand to a single agent.
// server.New wires it as State.OnAgentSubMode so a (re)connecting agent
// starts shipping immediately when UI streams are already open — without
// it, an agent that restarts mid-stream stays silent until some UI
// reopens its subscription.
func PushAggregate(e *nodes.Entry, bus *routing.EventBus) {
	if e == nil || bus == nil {
		return
	}
	nodes.TryEnqueue(e, aggregateFrame(bus))
}

// pushAggregateToAgents recomputes the union filter every connected
// agent should apply, and pushes it to each one.
func (s *ControlService) pushAggregateToAgents() {
	frame := aggregateFrame(s.state.EventBus)
	s.state.Nodes.ForEach(func(e *nodes.Entry) {
		nodes.TryEnqueue(e, frame)
	})
}

// schedulePushAggregate coalesces pushes: the first call arms a timer,
// the calls that follow within pushDebounce ride on it. The aggregate is
// computed when the timer fires, so it reflects every subscription that
// opened or closed in the window.
func (s *ControlService) schedulePushAggregate() {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	if s.pushTimer != nil {
		return
	}
	s.pushTimer = time.AfterFunc(pushDebounce, func() {
		s.pushMu.Lock()
		s.pushTimer = nil
		s.pushMu.Unlock()
		s.pushAggregateToAgents()
	})
}

// acquireStreamSlot counts the open StreamEvents of the caller's identity
// and refuses the one past the cap. An unauthenticated context (tests,
// a proxy that stamps no identity) shares one anonymous bucket.
func (s *ControlService) acquireStreamSlot(ctx context.Context) (func(), error) {
	limit := s.MaxStreamsPerOperator
	if limit <= 0 {
		limit = defaultMaxStreamsPerOperator
	}
	subject := auth.IdentityFromContext(ctx).Subject
	if subject == "" {
		subject = "anonymous"
	}
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if s.streams == nil {
		s.streams = make(map[string]int)
	}
	if s.streams[subject] >= limit {
		return nil, connect.NewError(connect.CodeResourceExhausted,
			fmt.Errorf("admin server: %s already holds %d live event streams (the limit); close one before opening another", subject, limit))
	}
	s.streams[subject]++
	return func() {
		s.streamsMu.Lock()
		defer s.streamsMu.Unlock()
		s.streams[subject]--
		if s.streams[subject] <= 0 {
			delete(s.streams, subject)
		}
	}, nil
}

// Compile-time assertion.
var _ adminv1connect.ControlServiceHandler = (*ControlService)(nil)

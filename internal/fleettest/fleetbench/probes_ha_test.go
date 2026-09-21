// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	server "github.com/jcsvwinston/orbit/server"
)

// HA-01: an agent fails over to the next endpoint when the first is
// unreachable.
func probeEndpointFailover(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	// Port 1 on loopback refuses the connection at once; nothing listens there.
	ag := e.startAgent(t, agent.Config{
		Endpoints:      []string{"http://127.0.0.1:1", "http://" + srv.AgentAddr()},
		NodeIDOverride: "fb-failover",
	})
	if waitRegistered(srv.Server, ag.NodeID(), 6*time.Second) {
		return present
	}
	t.Log("the agent never reached the live endpoint behind a dead one")
	return absent
}

// twoServersOneAgent boots two independent servers and one agent that is
// connected to the first.
func (e *env) twoServersOneAgent(t *testing.T) (*runningServer, *runningServer, *runningAgent) {
	t.Helper()
	a := e.startServer(t, server.Config{})
	b := e.startServer(t, server.Config{})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + a.AgentAddr()}, NodeIDOverride: "fb-shared"})
	if !waitRegistered(a.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register with server A")
	}
	return a, b, ag
}

// HA-02: two servers share the node registry.
func probeSharedRegistry(t *testing.T, e *env) verdict {
	_, b, ag := e.twoServersOneAgent(t)
	resp, err := e.control(b.Server).ListNodes(ctxFor(t), connect.NewRequest(&adminv1.ListNodesRequest{}))
	if err != nil {
		t.Fatalf("ListNodes on B: %v", err)
	}
	for _, n := range resp.Msg.GetNodes() {
		if n.GetNodeId() == ag.NodeID() {
			return present
		}
	}
	t.Logf("server B lists %d nodes; the agent connected to A is not among them", len(resp.Msg.GetNodes()))
	return absent
}

// HA-03: a UI subscribed on server B receives events from an agent
// connected to server A.
func probeCrossServerEvents(t *testing.T, e *env) verdict {
	a, b, ag := e.twoServersOneAgent(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	liveA, _ := subscribeHTTP(ctxA, e.control(a.Server), false)
	liveB, _ := subscribeHTTP(ctxB, e.control(b.Server), false)
	if !waitDemand(ag.bus, 3*time.Second) {
		t.Fatal("the subscription never reached the agent's bus")
	}
	emitHTTP(ag.bus, ag.NodeID(), "/cross/1")
	if got := collectPaths(liveA, 1, 3*time.Second); len(got) != 1 {
		t.Fatalf("the event did not reach the server the agent is connected to: %v", got)
	}
	if got := collectPaths(liveB, 1, 700*time.Millisecond); len(got) >= 1 {
		return present
	}
	return absent
}

// HA-04: agents are assigned across servers deterministically. A surface
// that appears flips this probe to partial (red against the recorded
// verdict) and it then has to grow a check of the assignment itself.
func probeAgentSharding(t *testing.T, e *env) verdict {
	knobs := append(fieldsNamed(server.Config{}, "", "Shard", "Peer", "Cluster", "Assign"),
		fieldsNamed(agent.Config{}, "", "Shard", "Assign")...)
	wire := append(messagesContaining("Shard", "Assign"), methodsContaining("Shard", "Assign")...)
	if len(knobs) == 0 && len(wire) == 0 {
		return absent
	}
	t.Logf("assignment surface: config %v, wire %v — extend this probe to check the assignment", knobs, wire)
	return partial
}

// HA-05: a reconnect under the same node_id supersedes the previous
// stream. The registry is a map, so it cannot hold two entries for one
// name; what the control asks is whether the OLD stream is actually ended
// — its peer sees an error, and frames it still sends no longer reach a UI
// subscriber as the node. A raw stream plays the old peer, because a real
// agent that is evicted simply reconnects and evicts back, and the
// question is what happens to the stream that lost, not how often the two
// trade places.
func probeSameNodeIDSupersedes(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	const node = "fb-twin"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old, err := registerRaw(ctx, h2cClient(), "http://"+srv.AgentAddr(), node)
	if err != nil {
		t.Fatalf("raw registration: %v", err)
	}
	defer func() { _ = old.CloseRequest() }()
	if !waitRegistered(srv.Server, node, 3*time.Second) {
		t.Fatal("the raw registration was not accepted")
	}
	oldEntry, _ := srv.State().Nodes.Lookup(node)

	// The old peer's read loop: an ended stream reports an error here.
	oldErr := make(chan error, 1)
	go func() {
		for {
			if _, err := old.Receive(); err != nil {
				oldErr <- err
				return
			}
		}
	}()

	e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: node})
	if !pollUntil(4*time.Second, func() bool {
		entry, ok := srv.State().Nodes.Lookup(node)
		return ok && entry != oldEntry
	}) {
		t.Fatal("the real agent never took over the registry entry")
	}
	entries := 0
	for _, info := range srv.State().Nodes.List() {
		if info.NodeID == node {
			entries++
		}
	}
	if entries != 1 {
		t.Logf("the registry holds %d entries for %q", entries, node)
		return absent
	}

	// (a) the superseded peer is told.
	ended := false
	select {
	case err := <-oldErr:
		t.Logf("the old stream ended: %v", err)
		ended = true
	case <-time.After(3 * time.Second):
		t.Log("the old stream is still open 3s after it was superseded")
	}

	// (b) what the superseded peer still sends does not reach the UI: a
	// live subscriber (opened and registered before the frame is sent) and
	// the replay ring both stay clean.
	uiCtx, uiCancel := context.WithCancel(context.Background())
	defer uiCancel()
	live, _ := subscribeHTTP(uiCtx, e.control(srv.Server), false)
	if !pollUntil(3*time.Second, func() bool { return srv.State().EventBus.SubscriberCount() >= 1 }) {
		t.Fatal("the UI subscription never registered on the bus")
	}
	leaked := false
	if !ended {
		ev := &adminv1.Event{NodeId: node, Body: &adminv1.Event_HttpRequest{HttpRequest: &adminv1.HttpRequestEvent{
			Method: "GET", Path: "/superseded", Status: 200,
		}}}
		if err := old.Send(&adminv1.Frame{Body: &adminv1.Frame_Event{Event: ev}}); err != nil {
			t.Logf("the old stream refused the frame: %v", err)
		} else {
			if got := collectPaths(live, 1, 1500*time.Millisecond); len(got) == 1 {
				t.Logf("a frame sent on the superseded stream reached a UI subscriber: %v", got)
				leaked = true
			}
			for _, r := range srv.State().Replay.Snapshot(nil, 0) {
				if r.GetHttpRequest().GetPath() == "/superseded" {
					t.Log("a frame sent on the superseded stream was pushed to the replay ring as the node")
					leaked = true
				}
			}
		}
	}
	if ended && !leaked {
		return present
	}
	return partial
}

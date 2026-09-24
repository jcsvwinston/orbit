// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	server "github.com/jcsvwinston/orbit/server"
	"github.com/jcsvwinston/orbit/server/nodes"
	"github.com/jcsvwinston/orbit/server/peers"
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

// peered describes two servers that know each other: the knob the fleet
// controls asked for (server.Config.PeerAddrs), with the agent endpoints
// reserved up front so each can name the other before binding.
type peered struct {
	a, b       *runningServer
	epA, epB   string
	idA, idB   string
	assignment bool
}

// twoPeeredServers boots two servers peered with each other. With
// assignment, each also advertises its endpoint and assigns nodes.
func (e *env) twoPeeredServers(t *testing.T, assignment bool) *peered {
	t.Helper()
	pa, pb := freePort(t), freePort(t)
	epA, epB := "http://127.0.0.1:"+pa, "http://127.0.0.1:"+pb
	mk := func(id, self, addr, peer string) server.Config {
		return server.Config{
			AgentAddr:          addr,
			ServerID:           id,
			AgentAdvertiseAddr: self,
			PeerAddrs:          []string{peer},
			AssignNodes:        assignment,
		}
	}
	a := e.startServer(t, mk("fb-server-a", epA, "127.0.0.1:"+pa, epB))
	b := e.startServer(t, mk("fb-server-b", epB, "127.0.0.1:"+pb, epA))
	// Both links open before anything is measured; a probe that ran
	// during the first dial would measure the dial, not the fleet.
	if !pollUntil(5*time.Second, func() bool {
		return len(a.State().Peers.LivePeers()) == 1 && len(b.State().Peers.LivePeers()) == 1
	}) {
		t.Fatalf("the two servers did not peer in 5s (A sees %v, B sees %v)", a.State().Peers.LivePeers(), b.State().Peers.LivePeers())
	}
	return &peered{a: a, b: b, epA: epA, epB: epB, idA: "fb-server-a", idB: "fb-server-b", assignment: assignment}
}

// twoServersOneAgent boots two peered servers and one agent that is
// connected to the first.
func (e *env) twoServersOneAgent(t *testing.T) (*runningServer, *runningServer, *runningAgent) {
	t.Helper()
	p := e.twoPeeredServers(t, false)
	ag := e.startAgent(t, agent.Config{Endpoints: []string{p.epA}, NodeIDOverride: "fb-shared"})
	if !waitRegistered(p.a.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register with server A")
	}
	return p.a, p.b, ag
}

// HA-02: two servers share the node registry.
func probeSharedRegistry(t *testing.T, e *env) verdict {
	_, b, ag := e.twoServersOneAgent(t)
	var found *adminv1.NodeInfo
	pollUntil(4*time.Second, func() bool {
		resp, err := e.control(b.Server).ListNodes(ctxFor(t), connect.NewRequest(&adminv1.ListNodesRequest{}))
		if err != nil {
			return false
		}
		for _, n := range resp.Msg.GetNodes() {
			if n.GetNodeId() == ag.NodeID() {
				found = n
				return true
			}
		}
		return false
	})
	if found == nil {
		t.Log("server B does not list the agent connected to A")
		return absent
	}
	// B knows the node is A's: the origin rides as a reserved label, and a
	// Data Studio request for it on B is refused with the owner named.
	if found.GetLabels()["orbit.server"] != "fb-server-a" {
		t.Logf("B lists the node without naming the server it is connected to: labels=%v", found.GetLabels())
		return partial
	}
	_, err := e.dataStudio(b.Server).ListRecords(ctxFor(t), connect.NewRequest(&adminv1.ListRecordsRequest{NodeId: ag.NodeID(), ModelName: "X"}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Logf("a Data Studio request for A's node on B answered %v, want FailedPrecondition naming the owner", err)
		return partial
	}
	return present
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
	// With a peer connected the agent has demand from the start (it ships
	// everything), so demand on the agent no longer proves the UI
	// subscriptions are registered: wait for them on both buses.
	if !pollUntil(3*time.Second, func() bool {
		return a.State().EventBus.SubscriberCount() >= 1 && b.State().EventBus.SubscriberCount() >= 1
	}) {
		t.Fatal("the UI subscriptions never registered on the servers' buses")
	}
	if !waitDemand(ag.bus, 3*time.Second) {
		t.Fatal("the subscription never reached the agent's bus")
	}
	emitHTTP(ag.bus, ag.NodeID(), "/cross/1")
	if got := collectPaths(liveA, 1, 3*time.Second); len(got) != 1 {
		t.Fatalf("the event did not reach the server the agent is connected to: %v", got)
	}
	if got := collectPaths(liveB, 1, 1500*time.Millisecond); len(got) != 1 || got[0] != "/cross/1" {
		t.Logf("the subscriber on B saw %v", got)
		return absent
	}
	// And a subscriber that opens on B AFTER the event sees it replayed:
	// the relay feeds B's replay ring, not only its live subscribers.
	ctxR, cancelR := context.WithCancel(context.Background())
	defer cancelR()
	recent, _ := subscribeHTTP(ctxR, e.control(b.Server), true)
	if got := collectPaths(recent, 1, 1500*time.Millisecond); len(got) < 1 {
		t.Log("B relays live but replays nothing of what A's agent sent")
		return partial
	}
	return present
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
	p := e.twoPeeredServers(t, true)
	// Two nodes whose owners differ, so the probe sees both a stay and a
	// move; the agents list the servers in the SAME order, so where each
	// ends up is the assignment, not the order.
	var stay, move string
	for i := 0; i < 64 && (stay == "" || move == ""); i++ {
		id := fmt.Sprintf("fb-assign-%d", i)
		if peers.Owner(id, []string{p.epA, p.epB}) == p.epA {
			if stay == "" {
				stay = id
			}
		} else if move == "" {
			move = id
		}
	}
	if stay == "" || move == "" {
		t.Fatalf("no pair of node ids with different owners in 64 tries: the assignment is not spreading")
	}
	endpoints := []string{p.epA, p.epB}
	agStay := e.startAgent(t, agent.Config{Endpoints: endpoints, NodeIDOverride: stay})
	agMove := e.startAgent(t, agent.Config{Endpoints: endpoints, NodeIDOverride: move})
	// The owned node registers on A and stays; the other is redirected to
	// B, where it registers locally, and A holds it only as remote.
	local := func(srv *runningServer, node string) bool {
		en, ok := srv.State().Nodes.Lookup(node)
		return ok && !en.Remote() && en.Info.Connected
	}
	remote := func(srv *runningServer, node, via string) bool {
		en, ok := srv.State().Nodes.Lookup(node)
		return ok && en.Remote() && en.Info.Via == via
	}
	if !pollUntil(8*time.Second, func() bool {
		return local(p.a, agStay.NodeID()) && local(p.b, agMove.NodeID()) &&
			remote(p.b, agStay.NodeID(), p.idA) && remote(p.a, agMove.NodeID(), p.idB)
	}) {
		aSide, bSide := p.a.State().Nodes.List(), p.b.State().Nodes.List()
		t.Logf("assignment surface %v %v exists, but after 8s A holds %v and B holds %v", knobs, wire, describe(aSide), describe(bSide))
		return partial
	}
	// Deterministic: the same node id owned by the same endpoint every time.
	for i := 0; i < 3; i++ {
		if peers.Owner(move, []string{p.epB, p.epA}) != p.epB {
			t.Log("the owner depends on the order of the endpoints")
			return partial
		}
	}
	return present
}

func describe(infos []nodes.NodeInfo) []string {
	var out []string
	for _, n := range infos {
		via := "local"
		if n.Remote() {
			via = "via " + n.Via
		}
		out = append(out, n.NodeID+"("+via+")")
	}
	return out
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

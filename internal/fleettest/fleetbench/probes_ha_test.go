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
// stream: one node, no duplicates.
func probeSameNodeIDSupersedes(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	const node = "fb-twin"
	e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: node})
	e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: node})
	if !waitRegistered(srv.Server, node, 4*time.Second) {
		t.Fatal("neither agent registered")
	}
	// Two agents claiming one name keep evicting each other; at every
	// moment the registry must hold exactly one entry for the name.
	deadline := time.Now().Add(600 * time.Millisecond)
	for time.Now().Before(deadline) {
		n := 0
		for _, info := range srv.State().Nodes.List() {
			if info.NodeID == node {
				n++
			}
		}
		if n != 1 {
			t.Logf("the registry holds %d entries for %q", n, node)
			return absent
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := e.control(srv.Server).ListNodes(ctxFor(t), connect.NewRequest(&adminv1.ListNodesRequest{}))
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	listed := 0
	for _, n := range resp.Msg.GetNodes() {
		if n.GetNodeId() == node {
			listed++
		}
	}
	if listed != 1 {
		t.Logf("ListNodes shows %q %d times", node, listed)
		return partial
	}
	return present
}

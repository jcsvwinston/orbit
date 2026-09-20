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

// The three descriptor-and-config probes below share one shape: a surface
// that appears (a Config knob, a message, an RPC) flips the verdict to
// partial, which is red against the recorded absent, and the probe then
// has to grow the behaviour check the new surface makes possible.

// ALR-01: a threshold rule on a host metric raises an alert.
func probeThresholdRules(t *testing.T, e *env) verdict {
	knobs := fieldsNamed(server.Config{}, "", "Alert", "Rule", "Threshold")
	msgs := messagesContaining("Alert", "Rule", "Threshold")
	if len(knobs) == 0 && len(msgs) == 0 {
		return absent
	}
	t.Logf("rule surface: config %v, messages %v — extend this probe to breach a threshold", knobs, msgs)
	return partial
}

// ALR-02: alert channels (webhook, e-mail) exist.
func probeAlertChannels(t *testing.T, e *env) verdict {
	knobs := fieldsNamed(server.Config{}, "", "Webhook", "SMTP", "Slack", "Notif", "Pager", "Sink")
	msgs := messagesContaining("Webhook", "Notif", "Recipient")
	if len(knobs) == 0 && len(msgs) == 0 {
		return absent
	}
	t.Logf("channel surface: config %v, messages %v — extend this probe to deliver one", knobs, msgs)
	return partial
}

// ALR-03: the UI API exposes alert state.
func probeAlertStateInAPI(t *testing.T, e *env) verdict {
	methods := methodsContaining("Alert", "Incident")
	msgs := messagesContaining("Alert", "Incident")
	if len(methods) == 0 && len(msgs) == 0 {
		return absent
	}
	t.Logf("alert API surface: %v %v — extend this probe to read it", methods, msgs)
	return partial
}

// ALR-04: the server publishes its OWN Prometheus collectors on the
// metrics listener, beside the runtime's.
func probeServerOwnMetrics(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{MetricsAddr: "127.0.0.1:0"})
	addr := srv.MetricsAddr()
	if addr == "" {
		t.Fatal("the metrics listener has no address after Run bound the others")
	}
	families := scrapeFamilies(t, "http://"+addr+"/metrics")
	if !families["go_goroutines"] {
		t.Fatalf("the scrape carries no runtime collectors either: %d families", len(families))
	}
	own := familiesWithPrefix(families, "orbit_", "admin_server_", "nucleus_admin_", "orbit_server_")
	if len(own) > 0 {
		t.Logf("server collectors: %v", own)
		return present
	}
	return absent
}

// ALR-05: the agent publishes its own Prometheus collectors.
func probeAgentOwnMetrics(t *testing.T, e *env) verdict {
	port := freePort(t)
	srv := e.startServer(t, server.Config{})
	ag := e.startAgent(t, agent.Config{
		Endpoints:      []string{"http://" + srv.AgentAddr()},
		MetricsAddr:    "127.0.0.1:" + port,
		NodeIDOverride: "fb-agent-metrics",
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	url := "http://127.0.0.1:" + port
	if !pollUntil(3*time.Second, func() bool {
		resp, err := bareClient().Get(url + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == 200
	}) {
		t.Fatal("the agent's metrics listener never answered /healthz")
	}
	families := scrapeFamilies(t, url+"/metrics")
	own := familiesWithPrefix(families, "admin_agent_")
	if len(own) == 0 {
		return absent
	}
	if !families["admin_agent_connected"] || !families["admin_agent_heartbeats_sent_total"] {
		t.Logf("agent collectors: %v", own)
		return partial
	}
	return present
}

// ALR-06: a node that stops sending frames is marked not connected within
// the inactivity timeout. A raw registration that then goes silent is the
// only way to hold a stream open without an agent's heartbeat attached.
func probeStaleNodeMarked(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{AgentInactivityTimeout: 300 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const node = "fb-silent"
	stream, err := registerRaw(ctx, h2cClient(), "http://"+srv.AgentAddr(), node)
	if err != nil {
		t.Fatalf("raw registration: %v", err)
	}
	defer func() { _ = stream.CloseRequest() }()
	if !waitRegistered(srv.Server, node, 3*time.Second) {
		t.Fatal("the raw registration was not accepted")
	}
	ctl := e.control(srv.Server)
	connected := func() (bool, bool) {
		resp, err := ctl.ListNodes(ctxFor(t), connect.NewRequest(&adminv1.ListNodesRequest{}))
		if err != nil {
			return false, false
		}
		for _, n := range resp.Msg.GetNodes() {
			if n.GetNodeId() == node {
				return n.GetConnected(), true
			}
		}
		return false, false
	}
	if c, ok := connected(); !ok || !c {
		t.Fatalf("right after registration the node is listed connected=%v present=%v", c, ok)
	}
	// The janitor runs at most once a second, so the mark lands within
	// timeout + one tick.
	if pollUntil(4*time.Second, func() bool { c, ok := connected(); return ok && !c }) {
		return present
	}
	t.Log("the silent node is still listed as connected after the timeout")
	return absent
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
	server "github.com/jcsvwinston/orbit/server"
	"github.com/jcsvwinston/orbit/server/alerts"
)

// The three descriptor-and-config probes below share one shape: a surface
// that appears (a Config knob, a message, an RPC) flips the verdict to
// partial, which is red against the recorded absent, and the probe then
// has to grow the behaviour check the new surface makes possible.

// alertRule is the rule the alert probes use: every agent has goroutines,
// so "goroutines > 0" fires on the first heartbeat; a rule on CPU or RSS
// would depend on the machine the bench runs on.
func alertRule(channels ...string) alerts.Rule {
	return alerts.Rule{Name: "bench goroutines", Metric: "goroutines", Op: ">", Threshold: 0, Severity: "critical", Channels: channels}
}

// ALR-01: a threshold rule on a host metric raises an alert. The probe
// configures a rule every node breaches, connects an agent, and reads the
// alert through the API: firing, on that node, from that rule.
func probeThresholdRules(t *testing.T, e *env) verdict {
	knobs := fieldsNamed(server.Config{}, "", "Alert", "Rule", "Threshold")
	msgs := messagesContaining("Alert", "Rule", "Threshold")
	if len(knobs) == 0 && len(msgs) == 0 {
		return absent
	}
	srv := e.startServer(t, server.Config{AlertRules: []alerts.Rule{alertRule()}})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: "fb-alerts"})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	al := e.alerts(srv.Server)
	var firing *adminv1.Alert
	pollUntil(4*time.Second, func() bool {
		resp, err := al.ListAlerts(ctxFor(t), connect.NewRequest(&adminv1.ListAlertsRequest{}))
		if err != nil {
			return false
		}
		for _, a := range resp.Msg.GetAlerts() {
			if a.GetNodeId() == ag.NodeID() && a.GetState() == adminv1.AlertState_ALERT_STATE_FIRING {
				firing = a
				return true
			}
		}
		return false
	})
	if firing == nil {
		t.Logf("rule surface exists (config %v, messages %v) but no alert fired for the node in 4s", knobs, msgs)
		return partial
	}
	if firing.GetRuleId() != "bench-goroutines" || firing.GetValue() <= 0 || firing.GetFiredAt() == nil || firing.GetMessage() == "" {
		t.Logf("the alert is not attributed to the rule with its value and time: %+v", firing)
		return partial
	}
	return present
}

// ALR-02: alert channels exist and deliver. The probe stands up a webhook,
// names it in the rule, and waits for the POST that carries the alert.
func probeAlertChannels(t *testing.T, e *env) verdict {
	knobs := fieldsNamed(server.Config{}, "", "Webhook", "SMTP", "Slack", "Notif", "Pager", "Sink")
	msgs := messagesContaining("Webhook", "Notif", "Recipient")
	if len(knobs) == 0 && len(msgs) == 0 {
		return absent
	}
	got := make(chan map[string]any, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["_method"] = r.Method
		body["_ctype"] = r.Header.Get("Content-Type")
		got <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()
	srv := e.startServer(t, server.Config{
		AlertRules:    []alerts.Rule{alertRule("ops")},
		AlertWebhooks: map[string]string{"ops": hook.URL},
	})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: "fb-channel"})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	select {
	case body := <-got:
		if body["_method"] != http.MethodPost || !strings.Contains(fmt.Sprint(body["_ctype"]), "json") ||
			body["state"] != "firing" || body["node_id"] != ag.NodeID() || body["rule_id"] != "bench-goroutines" {
			t.Logf("the webhook was called but the payload does not carry the alert: %v", body)
			return partial
		}
		return present
	case <-time.After(4 * time.Second):
		t.Logf("channel surface exists (config %v) but the webhook was not called in 4s", knobs)
		return partial
	}
}

// ALR-03: the UI API exposes alert state: the rules the server evaluates
// and the alerts they raised, through the same auth chain as the rest.
func probeAlertStateInAPI(t *testing.T, e *env) verdict {
	methods := methodsContaining("Alert", "Incident")
	msgs := messagesContaining("Alert", "Incident")
	if len(methods) == 0 && len(msgs) == 0 {
		return absent
	}
	srv := e.startServer(t, server.Config{AlertRules: []alerts.Rule{alertRule()}})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: "fb-alert-api"})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	al := e.alerts(srv.Server)
	rules, err := al.ListAlertRules(ctxFor(t), connect.NewRequest(&adminv1.ListAlertRulesRequest{}))
	if err != nil {
		t.Logf("ListAlertRules: %v", err)
		return partial
	}
	if len(rules.Msg.GetRules()) != 1 || rules.Msg.GetRules()[0].GetMetric() != "goroutines" || rules.Msg.GetRules()[0].GetSeverity() != adminv1.AlertSeverity_ALERT_SEVERITY_CRITICAL {
		t.Logf("the API does not return the configured rule as configured: %v", rules.Msg.GetRules())
		return partial
	}
	fired := pollUntil(4*time.Second, func() bool {
		resp, err := al.ListAlerts(ctxFor(t), connect.NewRequest(&adminv1.ListAlertsRequest{}))
		return err == nil && len(resp.Msg.GetAlerts()) > 0
	})
	if !fired {
		t.Log("ListAlerts answered nothing in 4s")
		return partial
	}
	// The API is behind the UI auth chain: a credential-less caller from
	// outside the trusted range is refused, like every other RPC.
	if _, err := adminv1connect.NewAlertServiceClient(bareClient(), uiURL(srv.Server)).ListAlerts(ctxFor(t), connect.NewRequest(&adminv1.ListAlertsRequest{})); err == nil {
		t.Log("ListAlerts answered a caller with no credential")
		return partial
	}
	return present
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
	// Negative space, not an allowlist of names: whatever the runtime and
	// the handler do not publish is the server's own.
	var own []string
	for f := range families {
		if !strings.HasPrefix(f, "go_") && !strings.HasPrefix(f, "process_") && !strings.HasPrefix(f, "promhttp_") {
			own = append(own, f)
		}
	}
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

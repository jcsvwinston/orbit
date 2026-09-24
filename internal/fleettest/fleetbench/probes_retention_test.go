// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	server "github.com/jcsvwinston/orbit/server"
)

// restartable is a server the probe can restart under a connected agent:
// the agent dials a relay (see env_test.go) that forwards to the current
// server. restart brings a new server up on the same configuration,
// points the relay at it and severs the old connections — what a process
// that died and came back on the same address looks like from the agent.
type restartable struct {
	e     *env
	cfg   server.Config
	relay *relay
	srv   *runningServer
	ag    *runningAgent
}

func (e *env) startRestartable(t *testing.T, cfg server.Config, agentCfg agent.Config) *restartable {
	t.Helper()
	srv := e.startServer(t, cfg)
	rl := startRelay(t, srv.AgentAddr())
	agentCfg.Endpoints = []string{"http://" + rl.addr()}
	ag := e.startAgent(t, agentCfg)
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	return &restartable{e: e, cfg: cfg, relay: rl, srv: srv, ag: ag}
}

// restart replaces the server and returns the new one; the caller decides
// whether to wait for the agent to come back.
func (r *restartable) restart(t *testing.T) *runningServer {
	t.Helper()
	next := r.e.startServer(t, r.cfg)
	r.relay.retarget(next.AgentAddr())
	r.srv.stop()
	r.relay.dropConnections()
	r.srv = next
	return next
}

// waitAgentBack waits until the agent has re-registered on the current server.
func (r *restartable) waitAgentBack(t *testing.T) {
	t.Helper()
	if !waitRegistered(r.srv.Server, r.ag.NodeID(), 8*time.Second) {
		t.Fatal("the agent did not reconnect to the restarted server in 8s")
	}
}

// RET-01: events the server replays to a new UI subscriber survive a
// server restart.
func probeReplaySurvivesRestart(t *testing.T, e *env) verdict {
	// The knob this probe asked for: a data directory the restarted server
	// reopens (restartable starts the second server on the same Config).
	r := e.startRestartable(t, server.Config{DataDir: t.TempDir()}, agent.Config{NodeIDOverride: "fb-replay"})
	srv1, ag := r.srv, r.ag

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	live, _ := subscribeHTTP(ctx1, e.control(srv1.Server), false)
	if !waitDemand(ag.bus, 3*time.Second) {
		t.Fatal("the subscription never reached the agent's bus")
	}
	for i := 0; i < 3; i++ {
		emitHTTP(ag.bus, ag.NodeID(), fmt.Sprintf("/replay/%d", i))
	}
	if got := collectPaths(live, 3, 3*time.Second); len(got) != 3 {
		t.Fatalf("the events did not reach the first server: %v", got)
	}
	cancel1()
	// Positive control: the same server replays them to a fresh subscriber.
	ctx2, cancel2 := context.WithCancel(context.Background())
	recent, _ := subscribeHTTP(ctx2, e.control(srv1.Server), true)
	if got := collectPaths(recent, 3, 2*time.Second); len(got) < 3 {
		cancel2()
		t.Fatalf("replay on the same server answered %v: the buffer is off", got)
	}
	cancel2()

	srv2 := r.restart(t)
	r.waitAgentBack(t)
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	after, _ := subscribeHTTP(ctx3, e.control(srv2.Server), true)
	got := collectPaths(after, 3, 700*time.Millisecond)
	t.Logf("replay after restart: %v", got)
	switch {
	case len(got) >= 3:
		return present
	case len(got) > 0:
		return partial
	}
	return absent
}

// RET-02: events emitted while the agent has no stream are delivered once
// it reconnects.
func probeOfflineEventsDelivered(t *testing.T, e *env) verdict {
	r := e.startRestartable(t, server.Config{}, agent.Config{NodeIDOverride: "fb-offline"})
	srv1, ag := r.srv, r.ag

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	live, _ := subscribeHTTP(ctx1, e.control(srv1.Server), false)
	if !waitDemand(ag.bus, 3*time.Second) {
		t.Fatal("the subscription never reached the agent's bus")
	}
	emitHTTP(ag.bus, ag.NodeID(), "/before")
	if got := collectPaths(live, 1, 3*time.Second); len(got) != 1 {
		t.Fatalf("the event before the outage did not arrive: %v", got)
	}
	cancel1()
	// The outage: the old server is stopped and its connections severed;
	// the relay keeps forwarding to the dead target until restart repoints
	// it, so any dial in between fails and the events below are emitted
	// while the agent provably has no stream.
	srv1.stop()
	r.relay.dropConnections()
	// The agent drops its bus subscription when the stream dies; wait for
	// that so the events below are emitted into the outage, not the tail.
	// An agent that parks events keeps a bus subscription through the
	// outage; one that does not drops it. Either way the events below are
	// emitted while the agent provably has no stream, which is the test.
	if !pollUntil(3*time.Second, func() bool { return !ag.bus.HasSubscribers(observability.KindHTTPRequest) }) {
		t.Log("the agent kept a bus subscription through the outage (it parks events)")
	}
	for i := 0; i < 3; i++ {
		emitHTTP(ag.bus, ag.NodeID(), fmt.Sprintf("/offline/%d", i))
	}

	srv2 := r.restart(t)
	r.waitAgentBack(t)
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	after, _ := subscribeHTTP(ctx3, e.control(srv2.Server), true)
	var offline []string
	for _, p := range collectPaths(after, 3, 700*time.Millisecond) {
		if strings.HasPrefix(p, "/offline/") {
			offline = append(offline, p)
		}
	}
	t.Logf("offline events delivered after reconnect: %v", offline)
	switch {
	case len(offline) >= 3:
		return present
	case len(offline) > 0:
		return partial
	}
	return absent
}

// RET-03: host metrics have a history per node, not only the last sample.
// The probe retains (a data directory), lets the agent heartbeat a few
// times, and reads more than one sample back through the API, in order.
func probeHostMetricsHistory(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{DataDir: t.TempDir()})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: "fb-metrics-history"})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	nodeInfo := messageNamed(t, "NodeInfo")
	series := append(fieldsContaining(nodeInfo, "history", "samples", "series"), methodsContaining("History", "Series", "Metrics")...)
	if len(series) == 0 {
		return absent
	}
	api := e.metricsAPI(srv.Server)
	var samples []*adminv1.HostMetricsSample
	pollUntil(5*time.Second, func() bool {
		resp, err := api.ListHostMetrics(ctxFor(t), connect.NewRequest(&adminv1.ListHostMetricsRequest{NodeId: ag.NodeID()}))
		if err != nil {
			return false
		}
		samples = resp.Msg.GetSamples()
		return len(samples) >= 3
	})
	if len(samples) < 2 {
		t.Logf("a history surface exists (%v) but ListHostMetrics returned %d samples in 5s", series, len(samples))
		return partial
	}
	for i := 1; i < len(samples); i++ {
		if !samples[i].GetTime().AsTime().After(samples[i-1].GetTime().AsTime()) || samples[i].GetMetrics() == nil {
			t.Logf("samples are not a series in time order with metrics: %v", samples)
			return partial
		}
	}
	// The window bounds the series too: nothing before `since`.
	since := samples[len(samples)-1].GetTime()
	resp, err := api.ListHostMetrics(ctxFor(t), connect.NewRequest(&adminv1.ListHostMetricsRequest{NodeId: ag.NodeID(), Since: since}))
	if err != nil || len(resp.Msg.GetSamples()) == 0 || resp.Msg.GetSamples()[0].GetTime().AsTime().Before(since.AsTime()) {
		t.Logf("since is not honoured: err=%v samples=%v", err, resp.Msg.GetSamples())
		return partial
	}
	return present
}

// RET-04: the fleet audit trail survives a server restart.
func probeAuditSurvivesRestart(t *testing.T, e *env) verdict {
	d, reg := e.agentDB(t, false)
	r := e.startRestartable(t,
		server.Config{DataStudioAllowedModels: []string{"TestArticle"}, DataDir: t.TempDir()},
		agent.Config{Registry: reg, Databases: map[string]*db.DB{"default": d}})
	ctx := ctxFor(t)
	if _, err := createArticle(ctx, e.dataStudio(r.srv.Server), "to be remembered"); err != nil {
		t.Fatalf("create: %v", err)
	}
	before, err := e.manage(r.srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil || len(before.Msg.GetEntries()) == 0 {
		t.Fatalf("no audit entry to survive: err=%v", err)
	}

	srv2 := r.restart(t)
	after, err := e.manage(srv2.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil {
		t.Fatalf("ListAudit after restart: %v", err)
	}
	entries := after.Msg.GetEntries()
	switch {
	case len(entries) > 0 && entries[0].GetAction() == "datastudio.create":
		return present
	case len(entries) > 0:
		t.Logf("entries after restart: %v", entries)
		return partial
	}
	return absent
}

// RET-05: a retention window is configurable and enforced. The probe
// sets a window of one second, writes an audit entry, reads it back
// inside the window, and reads again once the window has passed: the
// entry must be gone from what the server serves, whether the janitor
// has run or not.
func probeRetentionWindow(t *testing.T, e *env) verdict {
	knobs := fieldsNamed(server.Config{}, "", "Retention", "TimeToLive", "Persist", "DataDir", "StateDir", "Database", "MaxAge", "Expir")
	if len(knobs) == 0 {
		return absent
	}
	srv := e.startServer(t, server.Config{DataDir: t.TempDir(), Retention: time.Second, DataStudioAllowedModels: []string{"TestArticle"}})
	d, reg := e.agentDB(t, false)
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, Registry: reg, Databases: map[string]*db.DB{"default": d}})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	ctx := ctxFor(t)
	if _, err := createArticle(ctx, e.dataStudio(srv.Server), "short-lived"); err != nil {
		t.Fatalf("create: %v", err)
	}
	inside, err := e.manage(srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
	if err != nil || len(inside.Msg.GetEntries()) == 0 {
		t.Fatalf("the entry is not served inside its window: err=%v entries=%d", err, len(inside.Msg.GetEntries()))
	}
	gone := pollUntil(4*time.Second, func() bool {
		after, err := e.manage(srv.Server).ListAudit(ctx, connect.NewRequest(&adminv1.ListAuditRequest{}))
		return err == nil && len(after.Msg.GetEntries()) == 0
	})
	if !gone {
		t.Logf("server.Config offers %v, but an entry older than a one-second window is still served after four seconds", knobs)
		return partial
	}
	return present
}

// RET-06: the fleet audit trail can be exported (a download from the UI
// listener, or an RPC that returns the file).
func probeAuditExport(t *testing.T, e *env) verdict {
	srv, _ := e.startPair(t, "TestArticle")
	if _, err := createArticle(ctxFor(t), e.dataStudio(srv.Server), "exported"); err != nil {
		t.Fatalf("create: %v", err)
	}
	client := uiClient(nil)
	for _, p := range []string{"/api/audit/export", "/api/audit.csv", "/audit/export", "/api/audit/export.csv"} {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			req, _ := http.NewRequest(m, uiURL(srv.Server)+p, strings.NewReader(""))
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", m, p, err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			ctype := resp.Header.Get("Content-Type")
			isShell := strings.Contains(ctype, "text/html") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(body))), "<!doctype html")
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || isShell {
				continue
			}
			if resp.StatusCode == http.StatusOK && (strings.Contains(ctype, "csv") || strings.Contains(ctype, "json")) && len(body) > 0 {
				t.Logf("%s %s answered %d %s", m, p, resp.StatusCode, ctype)
				return present
			}
			t.Logf("%s %s answered %d (%s): a surface exists", m, p, resp.StatusCode, ctype)
			return partial
		}
	}
	if methods := methodsContaining("Export", "Download"); len(methods) > 0 {
		t.Logf("the protocol declares %v: extend this probe to call it", methods)
		return partial
	}
	return absent
}

// RET-07: the replay buffer is bounded, drops the oldest, and its size
// and the bus counters are readable.
func probeReplayBounded(t *testing.T, e *env) verdict {
	const capacity = 8
	srv := e.startServer(t, server.Config{HTTPReplayBufferSize: capacity})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: "fb-ring"})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	live, _ := subscribeHTTP(ctx1, e.control(srv.Server), false)
	if !waitDemand(ag.bus, 3*time.Second) {
		t.Fatal("the subscription never reached the agent's bus")
	}
	const emitted = 12
	for i := 0; i < emitted; i++ {
		emitHTTP(ag.bus, ag.NodeID(), fmt.Sprintf("/ring/%d", i))
	}
	if got := collectPaths(live, emitted, 3*time.Second); len(got) != emitted {
		t.Fatalf("only %d of %d events reached the server: %v", len(got), emitted, got)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	recent, _ := subscribeHTTP(ctx2, e.control(srv.Server), true)
	replayed := collectPaths(recent, capacity+1, 700*time.Millisecond)
	switch {
	case len(replayed) == 0:
		return absent
	case len(replayed) != capacity:
		t.Logf("replayed %d events with a capacity of %d", len(replayed), capacity)
		return partial
	}
	if replayed[0] != fmt.Sprintf("/ring/%d", emitted-capacity) || replayed[len(replayed)-1] != fmt.Sprintf("/ring/%d", emitted-1) {
		t.Logf("the buffer did not keep the newest: %v", replayed)
		return partial
	}
	stats := srv.State().EventBus.Stats()
	sizes := srv.State().Replay.LenSnapshot()
	if stats.Published < emitted || sizes[adminv1.EventType_EVENT_TYPE_HTTP_REQUEST] != capacity {
		t.Logf("stats: published=%d replay sizes=%v", stats.Published, sizes)
		return partial
	}
	return present
}

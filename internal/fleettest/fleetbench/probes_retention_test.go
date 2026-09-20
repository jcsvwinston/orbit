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
	"google.golang.org/protobuf/reflect/protoreflect"

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
	r := e.startRestartable(t, server.Config{}, agent.Config{NodeIDOverride: "fb-replay"})
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
	t.Logf("replay after restart: %v (server.Config persistence knobs: %v)", got, fieldsNamed(server.Config{}, "", "Persist", "Store", "DataDir", "StateDir"))
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
	// The outage: the old server is gone and the new one is not accepting
	// the agent yet — the relay is closed for the duration, so the events
	// below are emitted while the agent provably has no stream.
	srv1.stop()
	r.relay.dropConnections()
	// The agent drops its bus subscription when the stream dies; wait for
	// that so the events below are emitted into the outage, not the tail.
	if !pollUntil(3*time.Second, func() bool { return !ag.bus.HasSubscribers(observability.KindHTTPRequest) }) {
		t.Log("the agent kept its bus subscription through the outage")
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
func probeHostMetricsHistory(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{})
	ag := e.startAgent(t, agent.Config{Endpoints: []string{"http://" + srv.AgentAddr()}, NodeIDOverride: "fb-metrics-history"})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatal("agent did not register")
	}
	ctl := e.control(srv.Server)
	sampled := pollUntil(3*time.Second, func() bool {
		resp, err := ctl.ListNodes(ctxFor(t), connect.NewRequest(&adminv1.ListNodesRequest{}))
		if err != nil {
			return false
		}
		for _, n := range resp.Msg.GetNodes() {
			if n.GetNodeId() == ag.NodeID() && n.GetHostMetrics() != nil {
				return true
			}
		}
		return false
	})
	if !sampled {
		t.Fatal("no host metrics sample reached the server in 3s")
	}

	nodeInfo := messageNamed(t, "NodeInfo")
	repeated := false
	fs := nodeInfo.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Kind() == protoreflect.MessageKind && string(f.Message().Name()) == "HostMetrics" && f.Cardinality() == protoreflect.Repeated {
			repeated = true
		}
	}
	series := append(fieldsContaining(nodeInfo, "history", "samples", "series"), methodsContaining("History", "Series", "Metrics")...)
	if repeated || len(series) > 0 {
		t.Logf("a history surface exists (repeated=%v, %v): extend this probe to read more than one sample", repeated, series)
		return present
	}
	return absent
}

// RET-04: the fleet audit trail survives a server restart.
func probeAuditSurvivesRestart(t *testing.T, e *env) verdict {
	d, reg := e.agentDB(t, false)
	r := e.startRestartable(t,
		server.Config{DataStudioAllowedModels: []string{"TestArticle"}},
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

// RET-05: a retention window is configurable and enforced. A knob that
// appears flips this probe to partial (red against the recorded verdict),
// and the probe then has to set it and watch an old entry go.
func probeRetentionWindow(t *testing.T, e *env) verdict {
	knobs := fieldsNamed(server.Config{}, "", "Retention", "TimeToLive", "Persist", "DataDir", "StateDir", "Database", "MaxAge", "Expir")
	if len(knobs) == 0 {
		return absent
	}
	t.Logf("server.Config offers %v: extend this probe to set it and observe eviction", knobs)
	return partial
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

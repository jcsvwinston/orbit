package server_test

// Integration tests for the v1.2.1 audit backlog items on the server:
//
//   - OR-SEC-P1-5: a (re)connecting agent receives the aggregate
//     Subscribe immediately when UI streams are already open — telemetry
//     resumes after an agent restart without any UI reopening.
//   - OR-SEC-P1-3: read-only operators (role header / --ui-read-only)
//     cannot mutate through Data Studio.
//   - OR-SEC-P1-4: the UI listener stamps security headers.
//   - OR-SEC-P2-4: a silent agent is marked stale after the inactivity
//     timeout and revives when frames resume.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	server "github.com/jcsvwinston/orbit/server"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// startServerCfg mirrors startServer but honours the caller's Config
// (addresses and logger are still forced to test-safe values).
func startServerCfg(t *testing.T, cfg server.Config) (*server.Server, func()) {
	t.Helper()
	cfg.AgentAddr = "127.0.0.1:0"
	cfg.UIAddr = "127.0.0.1:0"
	cfg.Logger = discardLogger()
	srv := server.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- srv.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (srv.AgentAddr() == "" || srv.UIAddr() == "") {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.AgentAddr() == "" || srv.UIAddr() == "" {
		cancel()
		<-doneCh
		t.Fatal("server did not bind listeners")
	}
	return srv, func() {
		cancel()
		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			t.Error("server did not shut down")
		}
	}
}

// connectAsAgentCapture is connectAsAgent with the inbound (server →
// agent) frames forwarded to a channel instead of discarded.
func connectAsAgentCapture(t *testing.T, agentURL, nodeID string) (*connect.BidiStreamForClient[adminv1.Frame, adminv1.Frame], <-chan *adminv1.Frame, context.CancelFunc) {
	t.Helper()
	client := adminv1connect.NewAgentServiceClient(h2cClient(), agentURL)

	ctx, cancel := context.WithCancel(context.Background())
	stream := client.Stream(ctx)

	if err := stream.Send(&adminv1.Frame{
		Body: &adminv1.Frame_Registration{
			Registration: &adminv1.NodeRegistration{
				NodeId:    nodeID,
				Version:   "test",
				StartedAt: timestamppb.Now(),
			},
		},
	}); err != nil {
		cancel()
		t.Fatalf("send registration: %v", err)
	}

	frames := make(chan *adminv1.Frame, 16)
	go func() {
		defer close(frames)
		for {
			f, err := stream.Receive()
			if err != nil {
				return
			}
			select {
			case frames <- f:
			default:
			}
		}
	}()

	return stream, frames, cancel
}

// waitForAggregateSubscribe drains frames until a Subscribe command with
// the server-aggregate id arrives, or the deadline elapses.
func waitForAggregateSubscribe(t *testing.T, frames <-chan *adminv1.Frame, d time.Duration) *adminv1.Subscribe {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("agent stream closed while waiting for aggregate Subscribe")
			}
			if sub := f.GetCommand().GetSubscribe(); sub != nil && sub.GetSubscriptionId() == "server-aggregate" {
				return sub
			}
		case <-deadline:
			t.Fatal("agent never received the server-aggregate Subscribe")
		}
	}
}

// TestServer_AgentReconnect_ResumesAggregatePush is the OR-SEC-P1-5
// regression: with a UI stream already open, an agent that connects (or
// reconnects after a restart) must immediately receive the aggregate
// Subscribe and resume shipping events.
func TestServer_AgentReconnect_ResumesAggregatePush(t *testing.T) {
	srv, stop := startServer(t)
	defer stop()

	agentURL := "http://" + srv.AgentAddr()
	uiURL := "http://" + srv.UIAddr()

	// 1) UI stream opens FIRST.
	uiClient := adminv1connect.NewControlServiceClient(uiH2CClient(), uiURL)
	uiCtx, uiCancel := context.WithCancel(context.Background())
	defer uiCancel()

	gotCh := make(chan *adminv1.Event, 8)
	go func() {
		uiStream, err := uiClient.StreamEvents(uiCtx, connect.NewRequest(&adminv1.StreamEventsRequest{
			Filter: &adminv1.Filter{Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST}},
		}))
		if err != nil {
			return
		}
		for uiStream.Receive() {
			gotCh <- uiStream.Msg()
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && srv.State().EventBus.SubscriberCount() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.State().EventBus.SubscriberCount() == 0 {
		t.Fatal("UI subscription never registered")
	}

	// 2) Agent connects → receives the aggregate Subscribe on connect.
	stream1, frames1, cancel1 := connectAsAgentCapture(t, agentURL, "node-a")
	waitForAggregateSubscribe(t, frames1, 2*time.Second)

	// 3) Agent "restarts": stream drops, a new one connects. Before the
	// fix the new stream received nothing until a UI reopened its
	// subscription — silent telemetry loss.
	cancel1()
	_ = stream1.CloseRequest()

	stream2, frames2, cancel2 := connectAsAgentCapture(t, agentURL, "node-a")
	defer cancel2()
	defer stream2.CloseRequest()
	waitForAggregateSubscribe(t, frames2, 2*time.Second)

	// 4) The reconnected agent ships an event; the ORIGINAL UI stream
	// (never reopened) receives it.
	if err := stream2.Send(&adminv1.Frame{
		Body: &adminv1.Frame_Event{Event: &adminv1.Event{
			NodeId:    "node-a",
			Timestamp: timestamppb.Now(),
			Body:      &adminv1.Event_HttpRequest{HttpRequest: &adminv1.HttpRequestEvent{Method: "GET", Path: "/after-restart", Status: 200}},
		}},
	}); err != nil {
		t.Fatalf("send event after reconnect: %v", err)
	}
	select {
	case ev := <-gotCh:
		if ev.GetHttpRequest().GetPath() != "/after-restart" {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UI did not receive events from the reconnected agent")
	}
}

// dataStudioClientWithHeaders builds a DataStudio client whose requests
// carry the given trusted-proxy headers.
func dataStudioClientWithHeaders(uiURL string, headers map[string]string) adminv1connect.DataStudioServiceClient {
	base := http.DefaultTransport.(*http.Transport).Clone()
	httpClient := &http.Client{Transport: &headerInjector{next: base, headers: headers}}
	return adminv1connect.NewDataStudioServiceClient(httpClient, uiURL)
}

// TestServer_ReadOnlyOperator_MutationsDenied covers the role-header leg
// of OR-SEC-P1-3.
func TestServer_ReadOnlyOperator_MutationsDenied(t *testing.T) {
	srv, stop := startServer(t)
	defer stop()
	uiURL := "http://" + srv.UIAddr()

	viewer := dataStudioClientWithHeaders(uiURL, map[string]string{
		"X-Auth-User": "viewer-op",
		"X-Auth-Role": "viewer",
	})
	_, err := viewer.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "articles",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"title": `"x"`}},
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("viewer CreateRecord error = %v, want PermissionDenied", err)
	}

	// Reads still work for the viewer (fast path, no agent needed).
	if _, err := viewer.ListModels(context.Background(), connect.NewRequest(&adminv1.ListModelsRequest{})); err != nil {
		t.Fatalf("viewer ListModels: %v", err)
	}

	// A read-write operator passes the gate: with no agents connected the
	// mutation fails LATER, at dispatch — proving the denial above came
	// from the role, not from the empty fleet.
	writer := dataStudioClientWithHeaders(uiURL, map[string]string{"X-Auth-User": "admin-op"})
	_, err = writer.CreateRecord(context.Background(), connect.NewRequest(&adminv1.CreateRecordRequest{
		ModelName: "articles",
		Record:    &adminv1.Record{ValuesJson: map[string]string{"title": `"x"`}},
	}))
	if code := connect.CodeOf(err); code == connect.CodePermissionDenied {
		t.Fatalf("read-write operator was denied: %v", err)
	} else if err == nil {
		t.Fatal("CreateRecord with no agents unexpectedly succeeded")
	}
}

// TestServer_UIReadOnlyConfig_ForcesViewer covers the global knob leg of
// OR-SEC-P1-3 (--ui-read-only).
func TestServer_UIReadOnlyConfig_ForcesViewer(t *testing.T) {
	srv, stop := startServerCfg(t, server.Config{UIReadOnly: true})
	defer stop()
	uiURL := "http://" + srv.UIAddr()

	client := dataStudioClientWithHeaders(uiURL, map[string]string{"X-Auth-User": "admin-op"})
	_, err := client.DeleteRecord(context.Background(), connect.NewRequest(&adminv1.DeleteRecordRequest{
		ModelName: "articles",
		Id:        "1",
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("DeleteRecord under UIReadOnly = %v, want PermissionDenied", err)
	}
}

// TestServer_UISecurityHeaders covers OR-SEC-P1-4: CSP, nosniff and
// anti-framing headers on the UI listener.
func TestServer_UISecurityHeaders(t *testing.T) {
	srv, stop := startServer(t)
	defer stop()

	req, err := http.NewRequest(http.MethodGet, "http://"+srv.UIAddr()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Auth-User", "test-operator")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

// TestServer_InactiveAgentMarkedStale covers OR-SEC-P2-4: a silent agent
// flips to disconnected after AgentInactivityTimeout and revives when
// frames resume.
func TestServer_InactiveAgentMarkedStale(t *testing.T) {
	srv, stop := startServerCfg(t, server.Config{AgentInactivityTimeout: 300 * time.Millisecond})
	defer stop()

	agentURL := "http://" + srv.AgentAddr()
	uiURL := "http://" + srv.UIAddr()

	stream, cancel := connectAsAgent(t, agentURL, "node-a")
	defer cancel()
	defer stream.CloseRequest()

	uiClient := adminv1connect.NewControlServiceClient(uiH2CClient(), uiURL)
	connected := func() (bool, bool) {
		resp, err := uiClient.ListNodes(context.Background(), connect.NewRequest(&adminv1.ListNodesRequest{}))
		if err != nil || len(resp.Msg.Nodes) != 1 {
			return false, false
		}
		return true, resp.Msg.Nodes[0].Connected
	}

	// Registered and connected.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok, conn := connected(); ok && conn {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Silence: the janitor (ticks every ~1s) marks it stale.
	deadline = time.Now().Add(5 * time.Second)
	stale := false
	for time.Now().Before(deadline) {
		if ok, conn := connected(); ok && !conn {
			stale = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !stale {
		t.Fatal("silent agent was never marked stale")
	}

	// A frame arrives → Touch revives it.
	if err := stream.Send(&adminv1.Frame{
		Body: &adminv1.Frame_Event{Event: &adminv1.Event{
			NodeId:    "node-a",
			Timestamp: timestamppb.Now(),
			Body:      &adminv1.Event_HttpRequest{HttpRequest: &adminv1.HttpRequestEvent{Method: "GET", Path: "/alive", Status: 200}},
		}},
	}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok, conn := connected(); ok && conn {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("agent did not revive after frames resumed")
}

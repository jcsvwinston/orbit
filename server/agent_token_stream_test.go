package server_test

// Regression tests for the shared bearer token on the AGENT side of the
// wire (OR-2).
//
// TestServer_AgentToken_RejectsBadToken already covers the server's auth
// middleware, but it does so with a hand-rolled HTTP GET. That leaves the
// half that actually broke untested: the agent's Connect-RPC *client*.
// The agent's only RPC is the bidi stream (AgentService.Stream), and a
// unary-only interceptor never runs for streaming calls — so the bearer
// was silently dropped and every tokened agent 401'd in a reconnect loop.
//
// These tests therefore drive the real agent.Agent (not a raw stream
// client) against a real server configured with AgentToken, and assert on
// observable fleet behaviour: the node registers, and its events reach the
// UI. The negative case pins the flip side — a wrong token must NOT
// register — so the positive case cannot pass by the token being ignored.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent"
	server "github.com/jcsvwinston/orbit/server"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// startTokenServerAndAgent boots a server that requires agentToken and an
// agent that presents agentToken. It does not wait for registration: the
// callers disagree on whether registration is expected. serverLogger lets
// a caller capture the server's log output; nil discards it.
func startTokenServerAndAgent(t *testing.T, serverToken, agentToken string, serverLogger *slog.Logger) (*server.Server, *agent.Agent, *observability.Bus, func()) {
	t.Helper()

	if serverLogger == nil {
		serverLogger = discardLogger()
	}
	srv := server.New(server.Config{
		AgentAddr:  "127.0.0.1:0",
		UIAddr:     "127.0.0.1:0",
		AgentToken: serverToken,
		Logger:     serverLogger,
	})
	srvCtx, srvCancel := context.WithCancel(context.Background())
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.Run(srvCtx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (srv.AgentAddr() == "" || srv.UIAddr() == "") {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.AgentAddr() == "" || srv.UIAddr() == "" {
		srvCancel()
		<-srvDone
		t.Fatal("server did not bind listeners")
	}

	bus := observability.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ag, err := agent.New(agent.Config{
		Endpoints:         []string{"http://" + srv.AgentAddr()},
		Token:             agentToken,
		Bus:               bus,
		StateDir:          t.TempDir(),
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      500 * time.Millisecond,
		Logger:            discardLogger(),
	})
	if err != nil {
		srvCancel()
		<-srvDone
		t.Fatalf("agent.New: %v", err)
	}

	agCtx, agCancel := context.WithCancel(context.Background())
	agDone := make(chan error, 1)
	go func() { agDone <- ag.Run(agCtx) }()

	stop := func() {
		agCancel()
		<-agDone
		srvCancel()
		<-srvDone
	}
	return srv, ag, bus, stop
}

// waitForRegistration polls the node registry, returning true as soon as
// the agent shows up.
func waitForRegistration(srv *server.Server, nodeID string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, ok := srv.State().Nodes.Lookup(nodeID); ok {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestServer_AgentToken_StreamAuthenticates is the OR-2 regression test:
// with a matching token the agent's bidi stream must authenticate, the
// node must appear in ListNodes as connected, and its telemetry must
// reach the UI. Before the fix the bearer never left the client and this
// failed at the registration step.
func TestServer_AgentToken_StreamAuthenticates(t *testing.T) {
	const token = "sekret"

	srv, ag, bus, stop := startTokenServerAndAgent(t, token, token, nil)
	defer stop()

	if !waitForRegistration(srv, ag.NodeID(), 5*time.Second) {
		t.Fatal("agent with the correct token did not register in 5s: " +
			"the bearer is not reaching the bidi stream")
	}

	// The UI must see it too — ListNodes is what an operator actually looks at.
	uiClient := adminv1connect.NewControlServiceClient(uiH2CClient(), "http://"+srv.UIAddr())
	resp, err := uiClient.ListNodes(context.Background(), connect.NewRequest(&adminv1.ListNodesRequest{}))
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(resp.Msg.Nodes) != 1 {
		t.Fatalf("ListNodes returned %d nodes, want 1", len(resp.Msg.Nodes))
	}
	if got := resp.Msg.Nodes[0].NodeId; got != ag.NodeID() {
		t.Errorf("node id = %q, want %q", got, ag.NodeID())
	}
	if !resp.Msg.Nodes[0].Connected {
		t.Error("node registered but reports connected = false")
	}

	// Telemetry must flow end to end: UI subscribes, the server pushes a
	// Subscribe command down the (authenticated) stream, the agent picks
	// it up on the bus, and the event comes back the same way.
	uiCtx, uiCancel := context.WithCancel(context.Background())
	defer uiCancel()

	gotCh := make(chan *adminv1.Event, 4)
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

	// The agent only forwards HTTP events once the server has relayed the
	// UI's subscription to it — wait for that to land on the agent's bus.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if bus.HasSubscribers(observability.KindHTTPRequest) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !bus.HasSubscribers(observability.KindHTTPRequest) {
		t.Fatal("subscription did not reach the agent's bus in 3s")
	}

	ev := observability.AcquireHTTPRequestEvent(time.Now(), ag.NodeID())
	ev.Method = "GET"
	ev.Path = "/api/x"
	ev.Status = 200
	bus.Emit(ev)

	select {
	case got := <-gotCh:
		body := got.GetHttpRequest()
		if body == nil {
			t.Fatalf("expected http_request body, got %T", got.GetBody())
		}
		if body.Method != "GET" || body.Path != "/api/x" || body.Status != 200 {
			t.Errorf("unexpected payload: %+v", body)
		}
		if got.NodeId != ag.NodeID() {
			t.Errorf("node = %q, want %q", got.NodeId, ag.NodeID())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event from a tokened agent did not reach the UI in 3s")
	}
}

// TestServer_AgentToken_WrongTokenNeverRegisters is the negative control.
// Without it, TestServer_AgentToken_StreamAuthenticates would also pass on
// a server that ignored the token entirely.
//
// It additionally pins the OR5-2 server half: the rejection must leave a
// rate-limited WARN with the remote IP in the SERVER log, so an operator
// can see that agents with a bad token are calling.
func TestServer_AgentToken_WrongTokenNeverRegisters(t *testing.T) {
	var serverLog safeLogBuffer
	logger := slog.New(slog.NewTextHandler(&serverLog, &slog.HandlerOptions{
		Level: slog.LevelInfo, // default operator visibility
	}))

	srv, ag, _, stop := startTokenServerAndAgent(t, "right-token", "wrong-token", logger)
	defer stop()

	if waitForRegistration(srv, ag.NodeID(), 2*time.Second) {
		t.Fatal("agent with the wrong token registered; the agent listener is not enforcing auth")
	}

	logs := serverLog.String()
	if !strings.Contains(logs, "rejected agent request") {
		t.Fatalf("server log has no WARN for the rejected agent:\n%s", logs)
	}
	warnLine := ""
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "rejected agent request") {
			warnLine = line
			break
		}
	}
	if !strings.Contains(warnLine, "level=WARN") {
		t.Errorf("rejection line is not WARN: %q", warnLine)
	}
	if !strings.Contains(warnLine, "remote_ip=127.0.0.1") {
		t.Errorf("rejection WARN does not carry the remote IP: %q", warnLine)
	}
	if !strings.Contains(warnLine, "token_presented=true") {
		t.Errorf("rejection WARN should mark the token as presented-and-wrong: %q", warnLine)
	}
	// Rate limiting: multiple rejected reconnect attempts inside the
	// same minute for the same IP must collapse into a single WARN.
	if n := strings.Count(logs, "rejected agent request"); n != 1 {
		t.Errorf("rejection WARN emitted %d times, want exactly 1 (rate limit 1/min per IP)", n)
	}
}

// safeLogBuffer is a goroutine-safe buffer for capturing slog output
// written from server goroutines while the test goroutine reads it.
type safeLogBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeLogBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeLogBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

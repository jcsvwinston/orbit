package agent

// Regression tests for the v1.2.1 audit backlog items that live on the
// agent side:
//
//   - OR-UX-P0-1: events shipped to the fleet must carry the agent's
//     registered NodeID, not the in-process bus NodeID (hostname), or
//     every per-node view in the fleet UI counts zero with real traffic.
//   - OR-FLEET-2: GetSnapshot used to be a stub ("snapshot providers not
//     implemented"); GO_RUNTIME and REGISTERED_MODELS now answer with
//     real payloads, and unsupported types get a per-type error.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent/internal/testserver"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// startTestAgent boots an agent against a fresh testserver and waits for
// its registration and first session.
func startTestAgent(t *testing.T, bus *observability.Bus) (*testserver.Server, *Agent, *testserver.StreamSession, context.CancelFunc) {
	t.Helper()
	srv := testserver.Start()
	t.Cleanup(srv.Close)

	agent, err := New(Config{
		Endpoints:         []string{srv.URL()},
		StateDir:          t.TempDir(),
		Bus:               bus,
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      500 * time.Millisecond,
		Logger:            discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = agent.Run(ctx) }()

	if _, err := srv.WaitForRegistration(2 * time.Second); err != nil {
		cancel()
		t.Fatalf("WaitForRegistration: %v", err)
	}
	sessions := srv.Sessions()
	if len(sessions) == 0 {
		cancel()
		t.Fatal("no session captured")
	}
	return srv, agent, sessions[0], cancel
}

// TestAgent_EventNodeID_MatchesRegistration asserts the wire event's
// node_id is the agent's fleet identity even when the bus event was
// emitted under a different (host-local) NodeID.
func TestAgent_EventNodeID_MatchesRegistration(t *testing.T) {
	bus := observability.NewBus(discardLogger())
	_, agent, sess, cancel := startTestAgent(t, bus)
	defer cancel()

	if err := sess.SendSubscribe("sub-1", &adminv1.Filter{
		Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST},
	}, nil); err != nil {
		t.Fatalf("SendSubscribe: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !bus.HasSubscribers(observability.KindHTTPRequest) {
		time.Sleep(20 * time.Millisecond)
	}

	// The bus stamps its own node label ("vm" here) — the pre-fix wire
	// event leaked it, so nothing correlated with the registry UUID.
	httpEv := observability.AcquireHTTPRequestEvent(time.Now(), "vm")
	httpEv.Method = "GET"
	httpEv.Path = "/api/x"
	httpEv.Status = 200
	bus.Emit(httpEv)

	got, err := sess.WaitForEvent(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForEvent: %v", err)
	}
	if got.NodeId != agent.NodeID() {
		t.Errorf("event node_id = %q, want the registered agent id %q", got.NodeId, agent.NodeID())
	}
	if got.NodeId == "vm" {
		t.Error("event still carries the bus NodeID; fleet per-node views cannot correlate it")
	}
}

// TestAgent_SnapshotProviders exercises the built-in snapshot providers
// end to end over the bidi stream.
func TestAgent_SnapshotProviders(t *testing.T) {
	bus := observability.NewBus(discardLogger())
	_, agent, sess, cancel := startTestAgent(t, bus)
	defer cancel()

	// GO_RUNTIME answers with a real payload.
	if err := sess.SendSnapshotRequest("snap-1", adminv1.SnapshotType_SNAPSHOT_TYPE_GO_RUNTIME); err != nil {
		t.Fatalf("SendSnapshotRequest: %v", err)
	}
	resp, err := sess.WaitForSnapshotResponse(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForSnapshotResponse: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("go_runtime snapshot returned error: %q", resp.Error)
	}
	payload := string(resp.PayloadJson)
	for _, want := range []string{"go_version", "goroutines", agent.NodeID()} {
		if !strings.Contains(payload, want) {
			t.Errorf("go_runtime payload missing %q: %s", want, payload)
		}
	}

	// An unsupported type gets an explicit per-type error, not a stub.
	if err := sess.SendSnapshotRequest("snap-2", adminv1.SnapshotType_SNAPSHOT_TYPE_FEATURE_FLAGS); err != nil {
		t.Fatalf("SendSnapshotRequest: %v", err)
	}
	resp, err = sess.WaitForSnapshotResponse(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForSnapshotResponse: %v", err)
	}
	if !strings.Contains(resp.Error, "no snapshot provider for SNAPSHOT_TYPE_FEATURE_FLAGS") {
		t.Errorf("unsupported type error = %q", resp.Error)
	}
}

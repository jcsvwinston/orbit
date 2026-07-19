package agent

// OR6-1 regression tests: the "connected" truth of the agent.
//
// Connected() — and with it the require_connection boot guard and the
// admin_agent_connected gauge — used to fire right after Dial, which
// only proves the auth-exempt /healthz probe answered. With a rejected
// token the guard printed a false OK INFO and boot proceeded. The honest
// signal is stream.Config.OnAccepted: the first frame the server sends
// back, which only happens after it authenticated and accepted the
// stream.
//
// Both tests use the start401Server fake from agent_auth_test.go:
// /healthz is open (200), everything else 401s, like the real admin
// server behind its auth middleware.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// TestExtension_RequireConnection_FailsBoot_BadToken is the OR6-1
// regression test at the operator-facing surface: with a token the admin
// server rejects, require_connection must FAIL the boot deadline instead
// of printing a false OK. Before the fix, Connected() closed right after
// Dial (the auth-exempt /healthz probe), Attach returned nil, and the
// guard logged "reached admin server within boot deadline".
func TestExtension_RequireConnection_FailsBoot_BadToken(t *testing.T) {
	url, _, stop := start401Server(t)
	defer stop()

	var logBuf safeBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelInfo, // default operator visibility
	}))
	bus := observability.NewBus(discardLogger())

	timeout := 1500 * time.Millisecond
	ext := NewExtension(ExtensionConfig{
		Endpoints:                []string{url},
		Token:                    "wrong-token",
		RequireConnection:        true,
		RequireConnectionTimeout: timeout,
	}, t.TempDir(), "v0.0.0-test")

	a := &app.App{Logger: logger, Observability: bus}

	start := time.Now()
	err := ext.Attach(a)
	if err == nil {
		_ = ext.Shutdown(context.Background())
		t.Fatal("Attach succeeded with a rejected token: require_connection accepted mere /healthz reachability as connected")
	}
	if !strings.Contains(err.Error(), "require_connection") {
		t.Errorf("boot error does not mention require_connection: %v", err)
	}
	// It must fail BY the deadline (waiting for acceptance that never
	// comes), not via some error-fast path that skips the guard.
	if elapsed := time.Since(start); elapsed < timeout {
		t.Errorf("Attach failed after %v, before the %v boot deadline", elapsed, timeout)
	}

	logs := logBuf.String()
	if strings.Contains(logs, "within boot deadline") {
		t.Errorf("boot-guard OK INFO logged although every stream was rejected with 401:\n%s", logs)
	}
	if strings.Contains(logs, "admin agent connected") {
		t.Errorf("agent logged \"connected\" although no stream was ever accepted:\n%s", logs)
	}
}

// TestAgent_BadToken_NoConnectedSignal pins the same OR6-1 truth at the
// Agent surface: with a rejected token, after multiple full dial+stream
// cycles, Connected() must still be open and the admin_agent_connected
// gauge must still read 0 for the endpoint.
func TestAgent_BadToken_NoConnectedSignal(t *testing.T) {
	url, streamTimes, stop := start401Server(t)
	defer stop()

	bus := observability.NewBus(discardLogger())
	ag, err := New(Config{
		Endpoints: []string{url},
		Token:     "wrong-token",
		Bus:       bus,
		StateDir:  t.TempDir(),
		Logger:    discardLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- ag.Run(ctx) }()

	// Wait for at least two rejected stream cycles so the agent has had
	// every opportunity to (wrongly) declare itself connected.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && len(streamTimes()) < 2 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(streamTimes()); n < 2 {
		t.Fatalf("saw only %d stream attempts in 10s", n)
	}

	select {
	case <-ag.Connected():
		t.Fatal("Connected() closed although the server rejected every stream with 401")
	default:
	}
	if v := testutil.ToFloat64(ag.Metrics().Connected.WithLabelValues(url)); v != 0 {
		t.Errorf("admin_agent_connected{endpoint=%q} = %v after rejected cycles, want 0", url, v)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after ctx cancel")
	}
}

package agent

// OR6-1 / OR6-2 regression tests: the "connected" truth of the agent.
//
// OR6-1: Connected() — and with it the require_connection boot guard and
// the admin_agent_connected gauge — used to fire right after Dial, which
// only proves the auth-exempt /healthz probe answered. With a rejected
// token the guard printed a false OK INFO and boot proceeded. The honest
// signal is stream.Config.OnAccepted: the first frame the server sends
// back, which only happens after it authenticated and accepted the
// stream.
//
// OR6-2: the "token rejected" WARN of OR5-2 keys on CodeUnauthenticated,
// but a connect-go race makes the stream error surface as a generic
// io.EOF on roughly alternate cycles, so that WARN misses ~half of them.
// The race-free suspicion signal is "N consecutive stream cycles ended
// without a single accepted frame".
//
// Both use the start401Server fake from agent_auth_test.go: /healthz is
// open (200), everything else 401s, like the real admin server behind
// its auth middleware.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent/internal/testserver"
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

// startBadTokenAgent runs an agent with a capture logger against the
// given endpoint and returns the log buffer plus a stop func that
// cancels Run and joins it.
func startBadTokenAgent(t *testing.T, endpoint string) (*safeBuffer, func()) {
	t.Helper()

	var logBuf safeBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelInfo, // default operator visibility: no Debug
	}))

	bus := observability.NewBus(discardLogger())
	ag, err := New(Config{
		Endpoints: []string{endpoint},
		Token:     "wrong-token",
		Bus:       bus,
		StateDir:  t.TempDir(),
		Logger:    logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- ag.Run(ctx) }()

	stop := func() {
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
	return &logBuf, stop
}

const noFrameWarnMarker = "without a single accepted frame"

// TestAgent_BadToken_SuspicionWarnAfterCyclesWithoutAcceptedFrame is the
// OR6-2 regression test: the "token rejected" WARN keys on
// CodeUnauthenticated, which a connect-go race replaces with a plain
// "write envelope: EOF" on roughly alternate cycles — so it cannot be
// the agent's only auth signal. After noFrameCycleThreshold consecutive
// cycles in which the server never accepted a single frame, a
// rate-limited WARN must fire regardless of which shape the 401 took.
func TestAgent_BadToken_SuspicionWarnAfterCyclesWithoutAcceptedFrame(t *testing.T) {
	url, streamTimes, stop := start401Server(t)
	defer stop()

	logBuf, stopAgent := startBadTokenAgent(t, url)

	// Wait for three stream cycles (the threshold). With InitialBackoff
	// 1s and jitter 0.5 they land around t=0, t≈1–1.5s, t≈3–4.5s.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && len(streamTimes()) < 3 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(streamTimes()); n < 3 {
		stopAgent()
		t.Fatalf("saw only %d stream attempts in 20s", n)
	}
	// The third arrival is recorded server-side before the agent's cycle
	// bookkeeping runs; poll for the WARN instead of sleeping blind.
	warnDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(warnDeadline) && !strings.Contains(logBuf.String(), noFrameWarnMarker) {
		time.Sleep(50 * time.Millisecond)
	}

	stopAgent()
	logs := logBuf.String()

	if !strings.Contains(logs, noFrameWarnMarker) {
		t.Fatalf("no-accepted-frame suspicion WARN not found after >=3 rejected cycles:\n%s", logs)
	}
	warnLine := ""
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, noFrameWarnMarker) {
			warnLine = line
			break
		}
	}
	if !strings.Contains(warnLine, "level=WARN") {
		t.Errorf("suspicion line is not WARN: %q", warnLine)
	}
	if !strings.Contains(warnLine, "--agent-token") {
		t.Errorf("suspicion WARN does not point at --agent-token: %q", warnLine)
	}
	if !strings.Contains(warnLine, url) {
		t.Errorf("suspicion WARN does not name the endpoint %q: %q", url, warnLine)
	}
	// Rate limiting: every cycle past the threshold within the same
	// minute must collapse into a single WARN.
	if n := strings.Count(logs, noFrameWarnMarker); n != 1 {
		t.Errorf("suspicion WARN emitted %d times, want exactly 1 (rate limit 1/min per endpoint)", n)
	}
}

// TestAgent_SuspicionCounter_ResetsOnAcceptedFrame pins the reset half
// of OR6-2: two rejected cycles, then one ACCEPTED cycle, then two more
// rejected cycles must NOT warn — the accepted frame resets the run of
// suspicion. Without the reset the counter reads 2+1=3 on the first
// post-accept rejection and the WARN fires spuriously on any flaky link.
func TestAgent_SuspicionCounter_ResetsOnAcceptedFrame(t *testing.T) {
	srv := testserver.Start()
	defer srv.Close()
	srv.SetAuthReject(true)

	logBuf, stopAgent := startBadTokenAgent(t, srv.URL())
	defer stopAgent()

	// Phase 1: two rejected cycles — one below the threshold of 3.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && srv.RejectedStreams() < 2 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := srv.RejectedStreams(); n < 2 {
		t.Fatalf("saw only %d rejected stream attempts in 15s", n)
	}

	// Phase 2: let the next cycle through; the testserver acks the
	// registration, so the agent gets an accepted frame.
	srv.SetAuthReject(false)
	if _, err := srv.WaitForRegistration(15 * time.Second); err != nil {
		t.Fatalf("agent did not register once auth accepted: %v", err)
	}
	acceptDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(acceptDeadline) && !strings.Contains(logBuf.String(), "admin agent connected") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(logBuf.String(), "admin agent connected") {
		t.Fatal("agent never logged the accepted-stream INFO on the good cycle")
	}
	// 3 literal (== noFrameCycleThreshold) so this file still compiles
	// against the pre-fix sources for the red-without-fix demonstration.
	base := srv.RejectedStreams()
	if base >= 3 {
		t.Fatalf("test premise broken: %d rejections before the accepted cycle (threshold 3)", base)
	}

	// Phase 3: reject again and kick the live stream so the agent
	// reconnects into 401s. Two more rejected cycles: 2 + 2 with a reset
	// in between must stay silent.
	srv.SetAuthReject(true)
	sessions := srv.Sessions()
	if len(sessions) == 0 {
		t.Fatal("no session captured for the accepted cycle")
	}
	if err := sessions[len(sessions)-1].SendGoodbye("test: flip back to reject"); err != nil {
		t.Fatalf("SendGoodbye: %v", err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && srv.RejectedStreams() < base+2 {
		time.Sleep(50 * time.Millisecond)
	}
	if n := srv.RejectedStreams(); n < base+2 {
		t.Fatalf("saw only %d rejected stream attempts after goodbye in 15s", n-base)
	}
	// Give the last cycle's bookkeeping a beat before reading the log.
	time.Sleep(300 * time.Millisecond)

	if logs := logBuf.String(); strings.Contains(logs, noFrameWarnMarker) {
		t.Fatalf("suspicion WARN fired although an accepted cycle reset the run (2 rejected + accept + 2 rejected):\n%s", logs)
	}
}

package agent

// OR5-2 regression tests: an agent whose token the admin server rejects
// must NOT fail silently. Before the fix, the dialer's /healthz probe —
// which the server exempts from auth — kept "succeeding", the agent
// logged INFO "connected", the backoff was reset on every Dial, and the
// stream's 401 was only visible at Debug. Net effect: a bad token
// hammered the server at ~1/s forever with logs claiming all was well.
//
// The fake server below mimics exactly that server shape: /healthz is
// open (200) and everything else — including the bidi stream — returns
// 401, like server/auth.AgentMiddleware behind the /healthz carve-out.

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/jcsvwinston/nucleus/pkg/observability"
)

// safeBuffer is a goroutine-safe bytes.Buffer for capturing slog output
// from the agent's goroutines while the test goroutine reads it.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// start401Server starts an h2c server whose /healthz is auth-exempt
// (200) and whose every other path 401s. It records the arrival time of
// each non-healthz request (i.e. each bidi stream attempt).
func start401Server(t *testing.T) (url string, streamTimes func() []time.Time, stop func()) {
	t.Helper()

	var mu sync.Mutex
	var times []time.Time

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(listener) }()

	snapshot := func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		out := make([]time.Time, len(times))
		copy(out, times)
		return out
	}
	stopFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return "http://" + listener.Addr().String(), snapshot, stopFn
}

// TestAgent_BadToken_WarnsAndBacksOff asserts the two observable halves
// of the fix with the real agent loop and default backoff settings:
//
//  1. a WARN naming the endpoint and --agent-token is emitted through
//     the agent's normal logger at INFO visibility, and exactly once
//     (rate-limited), even though several reconnect cycles happen;
//  2. the retry intervals GROW (the /healthz probe no longer resets the
//     backoff), instead of hammering at ~1/s.
func TestAgent_BadToken_WarnsAndBacksOff(t *testing.T) {
	url, streamTimes, stop := start401Server(t)
	defer stop()

	var logBuf safeBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelInfo, // default operator visibility: no Debug
	}))

	bus := observability.NewBus(discardLogger())
	ag, err := New(Config{
		Endpoints:         []string{url},
		Token:             "wrong-token",
		Bus:               bus,
		StateDir:          t.TempDir(),
		HeartbeatInterval: 100 * time.Millisecond,
		DrainTimeout:      200 * time.Millisecond,
		Logger:            logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- ag.Run(ctx) }()

	// Wait for three stream attempts. With InitialBackoff=1s and jitter
	// 0.5 they land around t=0, t≈1–1.5s, t≈3–4.5s.
	deadline := time.Now().Add(15 * time.Second)
	var ts []time.Time
	for time.Now().Before(deadline) {
		ts = streamTimes()
		if len(ts) >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(ts) < 3 {
		t.Fatalf("saw only %d stream attempts in 15s; agent stopped retrying?", len(ts))
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

	// (b) Honest backoff: intervals must grow. time.After never fires
	// early, so gap1 >= InitialBackoff (1s) and gap2 >= 2s; and gap2
	// must strictly exceed gap1 (max gap1 = 1.5s+ε < min gap2 = 2s).
	gap1 := ts[1].Sub(ts[0])
	gap2 := ts[2].Sub(ts[1])
	if gap1 < time.Second {
		t.Errorf("gap1 = %v, want >= 1s (agent is hammering: backoff reset by the /healthz probe?)", gap1)
	}
	if gap2 < 2*time.Second {
		t.Errorf("gap2 = %v, want >= 2s (backoff did not grow)", gap2)
	}
	if gap2 <= gap1 {
		t.Errorf("gap2 (%v) <= gap1 (%v): retry intervals are not growing", gap2, gap1)
	}

	logs := logBuf.String()

	// (a) The WARN is visible at INFO level, names the endpoint, and
	// points at --agent-token.
	if !strings.Contains(logs, "admin agent token rejected by admin server; check --agent-token") {
		t.Fatalf("token-rejected WARN not found in agent logs:\n%s", logs)
	}
	warnLine := ""
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "token rejected") {
			warnLine = line
			break
		}
	}
	if !strings.Contains(warnLine, "level=WARN") {
		t.Errorf("token-rejected line is not WARN: %q", warnLine)
	}
	if !strings.Contains(warnLine, url) {
		t.Errorf("token-rejected WARN does not name the endpoint %q: %q", url, warnLine)
	}

	// Rate limiting: >= 3 rejected cycles within the same minute must
	// produce exactly one WARN.
	if n := strings.Count(logs, "token rejected"); n != 1 {
		t.Errorf("token-rejected WARN emitted %d times for %d cycles, want exactly 1 (rate limit 1/min per endpoint)", n, len(ts))
	}

	// Honest logging: no INFO "connected" while the token is rejected.
	if strings.Contains(logs, "admin agent connected") {
		t.Errorf("agent logged \"connected\" although every stream was rejected with 401:\n%s", logs)
	}
}

package agent

// OR7-2 regression test: the auth-suspicion counter must be PER ENDPOINT.
//
// The dialer fails over across the configured endpoint list, but the
// counter behind the "consecutive stream cycles ended without a single
// accepted frame" WARN used to be a single global int. Two consequences,
// both wrong with more than one endpoint:
//
//   - Misattribution: two frameless cycles against A followed by one
//     against B tripped the threshold (3) on B's FIRST cycle, naming B in
//     a WARN whose evidence mostly came from A.
//   - Cross-endpoint reset: one accepted frame on A wiped the run
//     accumulated against B, although acceptance on A proves nothing
//     about B's auth path.
//
// The test drives the agent cycle by cycle through a-runOnce-at-a-time
// calls — no Run loop, no backoff sleeps, no timing races — steering the
// dialer between two fake servers via their /healthz probes (the dialer
// picks the first endpoint whose probe answers 200).
//
// It compiles against the pre-fix sources (it touches no per-endpoint
// identifier) so the red-without-fix run is a plain `git stash` of
// agent.go away.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent/internal/testserver"
)

// suspicionWarnLines returns the log lines carrying the no-accepted-frame
// suspicion WARN.
func suspicionWarnLines(logs string) []string {
	var out []string
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, noFrameWarnMarker) {
			out = append(out, line)
		}
	}
	return out
}

func TestAgent_NoFrameSuspicion_PerEndpoint(t *testing.T) {
	srvA := testserver.Start()
	defer srvA.Close()
	srvA.SetAuthReject(true)

	srvB := testserver.Start()
	defer srvB.Close()
	srvB.SetAuthReject(true)

	var logBuf safeBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelInfo, // default operator visibility
	}))
	bus := observability.NewBus(discardLogger())

	ag, err := New(Config{
		Endpoints: []string{srvA.URL(), srvB.URL()},
		Token:     "wrong-token",
		Bus:       bus,
		StateDir:  t.TempDir(),
		Logger:    logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// endpointAttr matches the slog attr exactly: both the suspicion WARN
	// and the connected INFO emit "endpoint" followed by "node_id", so
	// the value is always space-terminated. Plain Contains(URL) could
	// false-positive on port-prefix collisions (":5000" vs ":50001").
	endpointAttr := func(u string) string { return "endpoint=" + u + " " }

	// rejectedCycle runs exactly one dial→stream cycle, which the current
	// healthz routing sends to the expected server and which that server
	// rejects. runOnce is synchronous, so when it returns the cycle's
	// bookkeeping (counter increment, WARN or not) has already happened.
	rejectedCycle := func(onto *testserver.Server, want int) {
		t.Helper()
		if err := ag.runOnce(ctx); err == nil {
			t.Fatal("runOnce returned nil for a cycle the server rejects")
		}
		if got := onto.RejectedStreams(); got != want {
			t.Fatalf("rejected streams on the expected server = %d, want %d (healthz routing broken?)", got, want)
		}
	}

	// Phase 1 — two frameless cycles against A: below the threshold of 3,
	// so no suspicion WARN.
	rejectedCycle(srvA, 1)
	rejectedCycle(srvA, 2)
	if lines := suspicionWarnLines(logBuf.String()); len(lines) != 0 {
		t.Fatalf("suspicion WARN after only 2 frameless cycles on A:\n%s", strings.Join(lines, "\n"))
	}

	// Phase 2 — A leaves the rotation (healthz down); the dialer fails
	// over to B. Two frameless cycles against B: B's OWN run is 2, still
	// below the threshold. A global counter reads 2+2=4 here and fires
	// the WARN naming B on B's very first cycle — evidence from A,
	// attribution to B.
	srvA.SetHealthzFail(true)
	rejectedCycle(srvB, 1)
	rejectedCycle(srvB, 2)
	if lines := suspicionWarnLines(logBuf.String()); len(lines) != 0 {
		t.Fatalf("suspicion WARN fired although no endpoint has %d frameless cycles of its own (A=2, B=2): the counter is global, not per endpoint:\n%s",
			noFrameCycleThreshold, strings.Join(lines, "\n"))
	}

	// Phase 3 — A comes back and accepts: the accepted frame must reset
	// A's run and ONLY A's. B's two frameless cycles remain valid
	// evidence about B.
	srvA.SetHealthzFail(false)
	srvA.SetAuthReject(false)
	cycleDone := make(chan error, 1)
	go func() { cycleDone <- ag.runOnce(ctx) }()
	if _, err := srvA.WaitForRegistration(10 * time.Second); err != nil {
		t.Fatalf("agent did not register on A once auth accepted: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logBuf.String(), "admin agent connected") {
		time.Sleep(20 * time.Millisecond)
	}
	connectedLine := ""
	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "admin agent connected") {
			connectedLine = line
			break
		}
	}
	if connectedLine == "" {
		t.Fatal("agent never logged the accepted-stream INFO on A's good cycle")
	}
	if !strings.Contains(connectedLine, endpointAttr(srvA.URL())) {
		t.Fatalf("accepted cycle did not run against A: %q", connectedLine)
	}
	sessions := srvA.Sessions()
	if len(sessions) == 0 {
		t.Fatal("no session captured for the accepted cycle")
	}
	if err := sessions[len(sessions)-1].SendGoodbye("test: end the accepted cycle"); err != nil {
		t.Fatalf("SendGoodbye: %v", err)
	}
	select {
	case <-cycleDone:
	case <-time.After(10 * time.Second):
		t.Fatal("accepted cycle did not end after the server's goodbye")
	}
	if lines := suspicionWarnLines(logBuf.String()); len(lines) != 0 {
		t.Fatalf("suspicion WARN fired during the accepted cycle:\n%s", strings.Join(lines, "\n"))
	}

	// Phase 4 — A rejects again and leaves the rotation; ONE more
	// frameless cycle against B. B's run is 2+1=3: the WARN must fire
	// now, naming B, at B's own threshold. Had the accepted frame on A
	// reset every endpoint, B would read 1 here and stay silent.
	srvA.SetAuthReject(true)
	srvA.SetHealthzFail(true)
	rejectedCycle(srvB, 3)
	warns := suspicionWarnLines(logBuf.String())
	if len(warns) != 1 {
		t.Fatalf("suspicion WARNs after B's %dth frameless cycle = %d, want exactly 1 (did A's accepted frame reset B's run?):\n%s",
			noFrameCycleThreshold, len(warns), logBuf.String())
	}
	bWarn := warns[0]
	if !strings.Contains(bWarn, "level=WARN") {
		t.Errorf("suspicion line is not WARN: %q", bWarn)
	}
	if !strings.Contains(bWarn, endpointAttr(srvB.URL())) {
		t.Errorf("suspicion WARN does not name B (%s): %q", srvB.URL(), bWarn)
	}
	if strings.Contains(bWarn, endpointAttr(srvA.URL())) {
		t.Errorf("suspicion WARN names A (%s), the endpoint the cycles did NOT run against: %q", srvA.URL(), bWarn)
	}
	if want := "consecutive_cycles=3"; !strings.Contains(bWarn, want) {
		t.Errorf("suspicion WARN does not carry %s (B's own run): %q", want, bWarn)
	}

	// Phase 5 — A rejoins the rotation, still rejecting. Its run was
	// reset by the accepted frame, so it must take three fresh frameless
	// cycles — its own threshold — before the WARN for A fires. The
	// rate limiter is per endpoint, so B's recent WARN does not suppress
	// A's.
	srvA.SetHealthzFail(false)
	rejectedCycle(srvA, 3) // A run: 1 (phase-1 rejections were 2; accepted cycle was not rejected)
	rejectedCycle(srvA, 4) // A run: 2
	if lines := suspicionWarnLines(logBuf.String()); len(lines) != 1 {
		t.Fatalf("suspicion WARN for A fired before A's own run reached %d (reset did not clear A?):\n%s",
			noFrameCycleThreshold, strings.Join(lines, "\n"))
	}
	rejectedCycle(srvA, 5) // A run: 3 → WARN for A
	warns = suspicionWarnLines(logBuf.String())
	if len(warns) != 2 {
		t.Fatalf("suspicion WARNs after A's 3rd fresh frameless cycle = %d, want 2 (one for B, one for A):\n%s",
			len(warns), logBuf.String())
	}
	aWarn := warns[1]
	if !strings.Contains(aWarn, endpointAttr(srvA.URL())) {
		t.Errorf("second suspicion WARN does not name A (%s): %q", srvA.URL(), aWarn)
	}
	if strings.Contains(aWarn, endpointAttr(srvB.URL())) {
		t.Errorf("second suspicion WARN names B (%s), the endpoint the fresh cycles did NOT run against: %q", srvB.URL(), aWarn)
	}
	if want := "consecutive_cycles=3"; !strings.Contains(aWarn, want) {
		t.Errorf("second suspicion WARN does not carry %s (A's fresh run): %q", want, aWarn)
	}
}

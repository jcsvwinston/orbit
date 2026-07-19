package connection

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func startHealthyServer(t *testing.T) (url string, hits *atomic.Int32, stop func()) {
	t.Helper()
	var counter atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			counter.Add(1)
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	}))
	return srv.URL, &counter, srv.Close
}

func TestDial_NoEndpoints_Errors(t *testing.T) {
	d := NewDialer(Config{Logger: discardLogger()})
	_, err := d.Dial(context.Background())
	if err == nil {
		t.Fatal("expected error with no endpoints")
	}
}

func TestDial_FirstHealthyEndpointWins(t *testing.T) {
	good1, hits1, stop1 := startHealthyServer(t)
	defer stop1()
	good2, hits2, stop2 := startHealthyServer(t)
	defer stop2()

	d := NewDialer(Config{
		Endpoints: []string{good1, good2},
		Logger:    discardLogger(),
	})

	res, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if res.Endpoint != good1 {
		t.Errorf("dialed %s, want %s", res.Endpoint, good1)
	}
	if hits1.Load() != 1 {
		t.Errorf("good1 hits = %d, want 1", hits1.Load())
	}
	if hits2.Load() != 0 {
		t.Errorf("good2 should not be probed, got %d", hits2.Load())
	}
}

func TestDial_FailoverPastBrokenEndpoint(t *testing.T) {
	// First endpoint refuses connections. Bind+close to get a free port that
	// nothing listens on.
	bad := "http://127.0.0.1:1" // port 1 reliably refuses on darwin/linux

	good, hits, stop := startHealthyServer(t)
	defer stop()

	d := NewDialer(Config{
		Endpoints:          []string{bad, good},
		HealthCheckTimeout: 200 * time.Millisecond,
		Logger:             discardLogger(),
	})

	res, err := d.Dial(context.Background())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if res.Endpoint != good {
		t.Errorf("dialed %s, want %s (failover should pick second)", res.Endpoint, good)
	}
	if hits.Load() != 1 {
		t.Errorf("good hits = %d, want 1", hits.Load())
	}
}

func TestDial_AllEndpointsBroken_ReturnsError(t *testing.T) {
	d := NewDialer(Config{
		Endpoints:          []string{"http://127.0.0.1:1", "http://127.0.0.1:2"},
		HealthCheckTimeout: 100 * time.Millisecond,
		Logger:             discardLogger(),
	})

	_, err := d.Dial(context.Background())
	if err == nil {
		t.Fatal("expected error when every endpoint is unreachable")
	}
}

func TestBackoff_GrowsAndCaps(t *testing.T) {
	// BackoffJitter is the documented default of 0.5; with that, each step
	// can grow by up to 50% over the base value. We assert the lower bound
	// (>= base) and the upper bound (<= base * 1.5) for the un-capped
	// steps, and verify the cap is enforced for the saturated ones.
	d := NewDialer(Config{
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     800 * time.Millisecond,
		Logger:         discardLogger(),
	})

	bases := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		800 * time.Millisecond,
		800 * time.Millisecond,
	}
	for i, base := range bases {
		got := d.Backoff()
		// Allow up to 50 % above the base for the documented default
		// jitter. The cap on the upper end is base * 1.5; on the lower
		// end it is exactly base.
		upper := base + base/2
		if got < base || got > upper {
			t.Errorf("step %d: got %v, want in [%v, %v]", i, got, base, upper)
		}
	}
}

// TestBackoff_NotResetByDial_OnlyByResetBackoff pins the OR5-2 fix: a
// successful Dial proves only that the auth-exempt /healthz probe
// answered, so it must NOT reset the backoff — otherwise an agent whose
// token is being rejected retries at ~InitialBackoff forever. Only an
// explicit ResetBackoff (issued by the agent when the server accepts
// the stream's first frame) returns the schedule to InitialBackoff.
func TestBackoff_NotResetByDial_OnlyByResetBackoff(t *testing.T) {
	good, _, stop := startHealthyServer(t)
	defer stop()

	d := NewDialer(Config{
		Endpoints:      []string{good},
		InitialBackoff: 100 * time.Millisecond,
		BackoffJitter:  0, // still applies the documented 0.5 default
		Logger:         discardLogger(),
	})

	// Advance the schedule twice: bases 100ms then 200ms.
	if got := d.Backoff(); got < 100*time.Millisecond || got > 150*time.Millisecond {
		t.Fatalf("step 1 = %v, want in [100ms, 150ms]", got)
	}
	if got := d.Backoff(); got < 200*time.Millisecond || got > 300*time.Millisecond {
		t.Fatalf("step 2 = %v, want in [200ms, 300ms]", got)
	}

	// A successful Dial must NOT reset: the next base stays 400ms.
	if _, err := d.Dial(context.Background()); err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if got := d.Backoff(); got < 400*time.Millisecond || got > 600*time.Millisecond {
		t.Errorf("after Dial: backoff = %v, want in [400ms, 600ms] (Dial must not reset)", got)
	}

	// ResetBackoff (server accepted a frame) returns to InitialBackoff.
	d.ResetBackoff()
	if got := d.Backoff(); got < 100*time.Millisecond || got > 150*time.Millisecond {
		t.Errorf("after ResetBackoff: backoff = %v, want in [100ms, 150ms]", got)
	}
}

func TestDial_RespectsContextCancel(t *testing.T) {
	d := NewDialer(Config{
		Endpoints:          []string{"http://127.0.0.1:1", "http://127.0.0.1:2"},
		HealthCheckTimeout: 5 * time.Second, // long enough that ctx wins
		Logger:             discardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	_, err := d.Dial(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

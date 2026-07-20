package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// fakeEventBus implements nucleus.EventBus for tests: events written to sqlCh /
// httpCh reach the panel's ConsumeEventBus drain goroutines. cancel closes the
// corresponding channel, mirroring the contract documented on the interface.
type fakeEventBus struct {
	mu            sync.Mutex
	sqlCh         chan nucleus.SQLEvent
	httpCh        chan nucleus.HTTPEvent
	sqlCancelled  bool
	httpCancelled bool
}

func newFakeEventBus() *fakeEventBus {
	return &fakeEventBus{
		sqlCh:  make(chan nucleus.SQLEvent, 16),
		httpCh: make(chan nucleus.HTTPEvent, 16),
	}
}

func (b *fakeEventBus) SubscribeSQL() (<-chan nucleus.SQLEvent, func()) {
	return b.sqlCh, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.sqlCancelled {
			b.sqlCancelled = true
			close(b.sqlCh)
		}
	}
}

func (b *fakeEventBus) SubscribeHTTP() (<-chan nucleus.HTTPEvent, func()) {
	return b.httpCh, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.httpCancelled {
			b.httpCancelled = true
			close(b.httpCh)
		}
	}
}

func (b *fakeEventBus) EmitSQL(nucleus.SQLEvent) {}

func (b *fakeEventBus) cancelled() (sql, http bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sqlCancelled, b.httpCancelled
}

// waitForLiveRequests polls the panel's request ring until it holds at least
// want events or the timeout expires (the drain goroutines are asynchronous).
func waitForLiveRequests(t *testing.T, panel *Panel, want int) []liveRequestEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows := panel.live.requests.latest(want + 10)
		if len(rows) >= want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d live request events, got %d: %#v", want, len(rows), rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForLiveSQL(t *testing.T, panel *Panel, want int) []liveSQLEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows := panel.live.sql.latest(want + 10)
		if len(rows) >= want {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d live sql events, got %d: %#v", want, len(rows), rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The in-process regression behind issue #121: with real traffic on the host
// application, /admin/api/live/snapshot returned requests:0 while queries
// flowed, because ConsumeEventBus only drained SubscribeSQL and left
// SubscribeHTTP unconsumed. Publishing an HTTPEvent on the bus must surface it
// in the snapshot's requests lane.
func TestConsumeEventBusFeedsHTTPRequests(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	bus := newFakeEventBus()
	stop := panel.ConsumeEventBus(bus)
	defer stop()

	emitted := time.Date(2026, 7, 20, 10, 30, 0, 0, time.UTC)
	bus.httpCh <- nucleus.HTTPEvent{
		EmittedAt:      emitted,
		NodeID:         "node-app",
		Method:         http.MethodGet,
		Path:           "/api/products",
		Status:         http.StatusOK,
		Duration:       12 * time.Millisecond,
		RequestID:      "req-http-1",
		TraceID:        "trace-http-1",
		UserID:         "user-9",
		RemoteIP:       "10.0.0.7",
		UserAgent:      "orbit-test/1.0",
		PayloadPreview: "query:page=1",
	}

	rows := waitForLiveRequests(t, panel, 1)
	event := rows[0]
	if event.Path != "/api/products" {
		t.Fatalf("expected path /api/products, got %q", event.Path)
	}
	if event.Method != http.MethodGet || event.Status != http.StatusOK {
		t.Fatalf("unexpected method/status: %#v", event)
	}
	if event.NodeID != "node-app" {
		t.Fatalf("expected node_id=node-app, got %q", event.NodeID)
	}
	if event.DurationMS != 12 {
		t.Fatalf("expected duration_ms=12, got %d", event.DurationMS)
	}
	if event.RequestID != "req-http-1" || event.TraceID != "trace-http-1" || event.UserID != "user-9" {
		t.Fatalf("expected correlation ids preserved, got %#v", event)
	}
	if event.RemoteIP != "10.0.0.7" || event.UserAgent != "orbit-test/1.0" {
		t.Fatalf("expected client fields preserved, got %#v", event)
	}
	if event.PayloadPreview != "query:page=1" {
		t.Fatalf("expected payload preview preserved, got %q", event.PayloadPreview)
	}
	if event.Timestamp != emitted.Format(time.RFC3339) {
		t.Fatalf("expected timestamp %q, got %q", emitted.Format(time.RFC3339), event.Timestamp)
	}

	// End to end: the snapshot endpoint must report the request (requests > 0).
	h := panel.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/live/snapshot?limit=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload liveSnapshotResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rr.Body.String())
	}
	if len(payload.Requests) != 1 || payload.Requests[0].Path != "/api/products" {
		t.Fatalf("expected snapshot to carry the bus-sourced request, got %#v", payload.Requests)
	}
}

// The SQL lane must keep working next to the new HTTP drain.
func TestConsumeEventBusStillFeedsSQL(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	bus := newFakeEventBus()
	stop := panel.ConsumeEventBus(bus)
	defer stop()

	bus.sqlCh <- nucleus.SQLEvent{
		EmittedAt: time.Now().UTC(),
		NodeID:    "node-app",
		ModelName: "Product",
		Operation: "select.list",
		Query:     "SELECT id FROM products",
		Duration:  3 * time.Millisecond,
	}

	rows := waitForLiveSQL(t, panel, 1)
	if rows[0].Query != "SELECT id FROM products" {
		t.Fatalf("expected sql query recorded, got %#v", rows[0])
	}
}

// The panel's own surface stays the liveTrafficMiddleware's responsibility:
// bus events under the admin prefix are skipped so the two lanes never record
// the same request twice, and live_exclude_patterns apply to the bus lane the
// same way they apply to the middleware and the cluster relay ingest.
func TestConsumeEventBusSkipsAdminPrefixAndExcludedPaths(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	if _, err := panel.addLiveExcludePattern("/healthz"); err != nil {
		t.Fatalf("addLiveExcludePattern failed: %v", err)
	}

	bus := newFakeEventBus()
	stop := panel.ConsumeEventBus(bus)
	defer stop()

	now := time.Now().UTC()
	bus.httpCh <- nucleus.HTTPEvent{EmittedAt: now, Method: http.MethodGet, Path: "/admin/api/models", Status: http.StatusOK}
	bus.httpCh <- nucleus.HTTPEvent{EmittedAt: now, Method: http.MethodGet, Path: "/healthz", Status: http.StatusOK}
	bus.httpCh <- nucleus.HTTPEvent{EmittedAt: now, Method: http.MethodGet, Path: "/api/orders", Status: http.StatusOK}

	rows := waitForLiveRequests(t, panel, 1)
	if len(rows) != 1 || rows[0].Path != "/api/orders" {
		t.Fatalf("expected only /api/orders to be recorded, got %#v", rows)
	}
}

// A bus event without a node id falls back to the panel's own node identity,
// mirroring the SQL lane.
func TestConsumeEventBusHTTPFallsBackToPanelNodeID(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	bus := newFakeEventBus()
	stop := panel.ConsumeEventBus(bus)
	defer stop()

	bus.httpCh <- nucleus.HTTPEvent{
		EmittedAt: time.Now().UTC(),
		Method:    http.MethodGet,
		Path:      "/api/things",
		Status:    http.StatusOK,
	}

	rows := waitForLiveRequests(t, panel, 1)
	if rows[0].NodeID != panel.liveNodeID() {
		t.Fatalf("expected node fallback %q, got %q", panel.liveNodeID(), rows[0].NodeID)
	}
}

// stop (and Panel.Close via observCancel) must cancel BOTH subscriptions.
func TestConsumeEventBusStopCancelsBothLanes(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	bus := newFakeEventBus()
	stop := panel.ConsumeEventBus(bus)

	stop()
	stop() // idempotent

	deadline := time.Now().Add(2 * time.Second)
	for {
		sqlDone, httpDone := bus.cancelled()
		if sqlDone && httpDone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected both subscriptions cancelled, got sql=%v http=%v", sqlDone, httpDone)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

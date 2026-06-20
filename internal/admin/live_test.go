package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func TestRequestRingBufferLatest(t *testing.T) {
	ring := newRequestRingBuffer(3)
	ring.push(liveRequestEvent{Path: "/a"})
	ring.push(liveRequestEvent{Path: "/b"})
	ring.push(liveRequestEvent{Path: "/c"})
	ring.push(liveRequestEvent{Path: "/d"})

	rows := ring.latest(10)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Path != "/d" || rows[1].Path != "/c" || rows[2].Path != "/b" {
		t.Fatalf("unexpected order: %#v", rows)
	}
}

func TestLiveEventBusDropsWhenSubscriberIsSlow(t *testing.T) {
	bus := newLiveEventBus(1)
	_, unsubscribe := bus.subscribe()
	defer unsubscribe()

	bus.publish(liveEventEnvelope{Type: "a"})
	bus.publish(liveEventEnvelope{Type: "b"})
	bus.publish(liveEventEnvelope{Type: "c"})

	stats := bus.stats()
	if stats.Published != 3 {
		t.Fatalf("expected published=3, got %d", stats.Published)
	}
	if stats.Dropped == 0 {
		t.Fatalf("expected dropped > 0, got %d", stats.Dropped)
	}
}

func TestLiveTrafficMiddlewareRecordsRequestAndSession(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	mw := panel.liveTrafficMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodGet, "/products?token=abc123&name=john", nil)
	ctx := observe.CtxWithRequestID(req.Context(), "req-1")
	ctx = observe.CtxWithTraceID(ctx, "trace-1")
	ctx = observe.CtxWithUserID(ctx, "user-42")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rr.Code)
	}

	requests := panel.live.requests.latest(1)
	if len(requests) != 1 {
		t.Fatalf("expected one request event, got %d", len(requests))
	}
	event := requests[0]
	if event.Status != http.StatusCreated {
		t.Fatalf("expected status=%d, got %d", http.StatusCreated, event.Status)
	}
	if event.TraceID != "trace-1" {
		t.Fatalf("expected trace_id=trace-1, got %q", event.TraceID)
	}
	if event.UserID != "user-42" {
		t.Fatalf("expected user_id=user-42, got %q", event.UserID)
	}
	if event.PayloadPreview == "" || event.PayloadPreview == "query:token=abc123&name=john" {
		t.Fatalf("expected redacted payload preview, got %q", event.PayloadPreview)
	}

	sessions := panel.live.sessions.snapshot(10)
	if len(sessions) != 1 {
		t.Fatalf("expected one tracked session, got %d", len(sessions))
	}
	if sessions[0].UserID != "user-42" {
		t.Fatalf("expected session user_id=user-42, got %q", sessions[0].UserID)
	}
}

func TestLiveTrafficMiddlewareSkipsWebSocketUpgrade(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	mw := panel.liveTrafficMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/api/live/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusSwitchingProtocols {
		t.Fatalf("expected status 101, got %d", rr.Code)
	}
	if got := panel.live.requests.latest(10); len(got) != 0 {
		t.Fatalf("expected no recorded events for websocket upgrade, got %d", len(got))
	}
}

func TestLiveTrafficMiddlewareSkipsExcludedPath(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	mw := panel.liveTrafficMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/system", nil)
	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if got := panel.live.requests.latest(10); len(got) != 0 {
		t.Fatalf("expected excluded admin path to skip recording, got %d events", len(got))
	}
}

func TestPanelLiveSnapshotEndpoint(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health?password=secret", nil)
	req = req.WithContext(observe.CtxWithRequestID(req.Context(), "req-2"))
	panel.recordLiveRequest(req, http.StatusOK, 12*time.Millisecond)
	panel.onModelSQLQuery(observe.CtxWithTraceID(context.Background(), "trace-2"), model.SQLQueryEvent{
		ModelName: "AdminUser",
		Operation: "select.list",
		Query:     "SELECT id, email FROM admin_users WHERE email = ? LIMIT ?",
		Args:      []interface{}{"admin@example.com", 25},
		Duration:  9 * time.Millisecond,
	})

	h := panel.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/live/snapshot?limit=5", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var payload liveSnapshotResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rr.Body.String())
	}
	if !payload.Enabled {
		t.Fatalf("expected live snapshot enabled")
	}
	if len(payload.Requests) == 0 {
		t.Fatalf("expected at least one request event")
	}
	if len(payload.Queries) == 0 {
		t.Fatalf("expected at least one sql query event")
	}
	if payload.SQLBuffer.Stored == 0 {
		t.Fatalf("expected sql buffer to store events")
	}
	if len(payload.Queries[0].Args) == 0 || payload.Queries[0].Args[0] != "string(17):***" {
		t.Fatalf("expected redacted string sql args, got %#v", payload.Queries[0].Args)
	}
	if payload.Stream.Published == 0 {
		t.Fatalf("expected published events > 0")
	}
	if len(payload.ExcludePatterns) == 0 || payload.ExcludePatterns[0] != "/admin" {
		t.Fatalf("expected exclude patterns to include /admin, got %#v", payload.ExcludePatterns)
	}
}

func TestPanelLiveSnapshotFiltersExcludedPaths(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	adminReq := httptest.NewRequest(http.MethodGet, "/admin/system", nil)
	apiReq := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	panel.recordLiveRequest(adminReq, http.StatusOK, 5*time.Millisecond)
	panel.recordLiveRequest(apiReq, http.StatusOK, 6*time.Millisecond)

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
	if len(payload.Requests) != 1 || payload.Requests[0].Path != "/api/articles" {
		t.Fatalf("expected only non-excluded /api/articles request, got %#v", payload.Requests)
	}

	if _, err := panel.addLiveExcludePattern("/api/*"); err != nil {
		t.Fatalf("addLiveExcludePattern failed: %v", err)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/api/live/snapshot?limit=10", nil))
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	var payload2 liveSnapshotResponse
	if err := json.Unmarshal(rr2.Body.Bytes(), &payload2); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rr2.Body.String())
	}
	if len(payload2.Requests) != 0 {
		t.Fatalf("expected no requests after excluding /api/*, got %#v", payload2.Requests)
	}
}

func TestPanelLiveSnapshotSupportsIndependentLimits(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	reqA := httptest.NewRequest(http.MethodGet, "/a", nil).WithContext(observe.CtxWithRequestID(context.Background(), "req-a"))
	reqB := httptest.NewRequest(http.MethodGet, "/b", nil).WithContext(observe.CtxWithRequestID(context.Background(), "req-b"))
	reqC := httptest.NewRequest(http.MethodGet, "/c", nil).WithContext(observe.CtxWithRequestID(context.Background(), "req-c"))
	panel.recordLiveRequest(reqA, http.StatusOK, 3*time.Millisecond)
	panel.recordLiveRequest(reqB, http.StatusOK, 4*time.Millisecond)
	panel.recordLiveRequest(reqC, http.StatusOK, 5*time.Millisecond)

	panel.onModelSQLQuery(context.Background(), model.SQLQueryEvent{
		ModelName: "AdminUser",
		Operation: "select",
		Query:     "SELECT 1",
		Duration:  1 * time.Millisecond,
	})
	panel.onModelSQLQuery(context.Background(), model.SQLQueryEvent{
		ModelName: "AdminUser",
		Operation: "insert",
		Query:     "INSERT INTO admin_users (email,name,active) VALUES (?,?,?)",
		Args:      []interface{}{"a@example.com", "A", true},
		Duration:  2 * time.Millisecond,
	})
	panel.onModelSQLQuery(context.Background(), model.SQLQueryEvent{
		ModelName: "AdminUser",
		Operation: "update",
		Query:     "UPDATE admin_users SET name = ? WHERE id = ?",
		Args:      []interface{}{"B", 1},
		Duration:  3 * time.Millisecond,
	})

	h := panel.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/live/snapshot?requests_limit=1&sql_limit=2&sessions_limit=3", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload liveSnapshotResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rr.Body.String())
	}
	if payload.RequestLimit != 1 || payload.SQLLimit != 2 || payload.SessionLimit != 3 {
		t.Fatalf("unexpected independent limits in payload: %#v", payload)
	}
	if len(payload.Requests) != 1 {
		t.Fatalf("expected 1 request row, got %d", len(payload.Requests))
	}
	if len(payload.Queries) != 2 {
		t.Fatalf("expected 2 sql rows, got %d", len(payload.Queries))
	}
	if len(payload.Sessions) != 3 {
		t.Fatalf("expected 3 session rows, got %d", len(payload.Sessions))
	}
}

func TestPanelOnModelSQLQueryPublishesEvent(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	busCh, unsubscribe := panel.live.bus.subscribe()
	defer unsubscribe()

	ctx := observe.CtxWithRequestID(context.Background(), "req-sql-1")
	ctx = observe.CtxWithTraceID(ctx, "trace-sql-1")
	panel.onModelSQLQuery(ctx, model.SQLQueryEvent{
		ModelName: "AdminUser",
		Operation: "update",
		Query:     "UPDATE admin_users SET name = ? WHERE id = ?",
		Args:      []interface{}{"Alice", 7},
		Duration:  7 * time.Millisecond,
	})

	select {
	case event := <-busCh:
		if event.Type != "db.query" {
			t.Fatalf("expected db.query event type, got %q", event.Type)
		}
		if event.SQL == nil || event.SQL.TraceID != "trace-sql-1" {
			t.Fatalf("expected sql event with trace id, got %#v", event.SQL)
		}
		if len(event.SQL.Args) == 0 || event.SQL.Args[0] != "string(5):***" {
			t.Fatalf("expected first arg redacted, got %#v", event.SQL.Args)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected sql live event on bus")
	}
}

func TestHandleLiveWSRejectsInvalidOrigin(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/live/ws", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "admin.example"
	err := panel.handleLiveWS(router.NewContext(rr, req, nil))
	if err == nil {
		t.Fatalf("expected forbidden error for invalid origin")
	}
}

func TestPanelLiveExcludePatternEndpoints(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	h := panel.Handler()

	addBody := bytes.NewBufferString(`{"pattern":"/healthz"}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/live/excludes", addBody)
	addReq.Header.Set("Content-Type", "application/json")
	addRR := httptest.NewRecorder()
	h.ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusCreated {
		t.Fatalf("expected add exclude status 201, got %d body=%s", addRR.Code, addRR.Body.String())
	}

	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, httptest.NewRequest(http.MethodGet, "/api/live/excludes", nil))
	if listRR.Code != http.StatusOK {
		t.Fatalf("expected list exclude status 200, got %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listPayload struct {
		Patterns []string `json:"patterns"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list payload failed: %v body=%s", err, listRR.Body.String())
	}
	if !reflect.DeepEqual(listPayload.Patterns, []string{"/admin", "/healthz"}) {
		t.Fatalf("unexpected patterns: %#v", listPayload.Patterns)
	}

	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, httptest.NewRequest(http.MethodDelete, "/api/live/excludes?pattern=/admin", nil))
	if delRR.Code != http.StatusOK {
		t.Fatalf("expected delete exclude status 200, got %d body=%s", delRR.Code, delRR.Body.String())
	}

	if shouldExcludeLivePath("/admin/system", panel.liveExcludePatterns()) {
		t.Fatalf("expected /admin not excluded after delete")
	}
	if !shouldExcludeLivePath("/healthz", panel.liveExcludePatterns()) {
		t.Fatalf("expected /healthz to remain excluded")
	}
}

func TestNormalizeLiveExcludePatterns(t *testing.T) {
	cases := []struct {
		name        string
		adminPrefix string
		input       []string
		want        []string
	}{
		{
			name:        "defaults to admin prefix",
			adminPrefix: "/admin",
			input:       nil,
			want:        []string{"/admin"},
		},
		{
			name:        "trims and deduplicates",
			adminPrefix: "/admin",
			input:       []string{" /metrics ", "/admin", "/metrics"},
			want:        []string{"/admin", "/metrics"},
		},
		{
			name:        "fallback prefix when empty",
			adminPrefix: "",
			input:       nil,
			want:        []string{"/admin"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeLiveExcludePatterns(tc.adminPrefix, tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeLiveExcludePatterns() mismatch: got=%#v want=%#v", got, tc.want)
			}
		})
	}
}

func TestShouldExcludeLivePath(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{name: "exact", path: "/admin", patterns: []string{"/admin"}, want: true},
		{name: "prefix", path: "/admin/system", patterns: []string{"/admin"}, want: true},
		{name: "glob", path: "/internal/metrics", patterns: []string{"/internal/*"}, want: true},
		{name: "glob_prefix_nested", path: "/api/live/snapshot", patterns: []string{"/api/*"}, want: true},
		{name: "star", path: "/anything", patterns: []string{"*"}, want: true},
		{name: "not matched", path: "/api/articles", patterns: []string{"/admin", "/internal/*"}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldExcludeLivePath(tc.path, tc.patterns)
			if got != tc.want {
				t.Fatalf("shouldExcludeLivePath()=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestPanelIngestClusterLiveEvent_PopulatesBuffersAndNodeID(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.ingestClusterLiveEvent("node-b", liveEventEnvelope{
		Type: "http.request",
		Request: &liveRequestEvent{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Method:     http.MethodGet,
			Path:       "/api/orders",
			Status:     http.StatusOK,
			DurationMS: 12,
		},
	})
	panel.ingestClusterLiveEvent("node-b", liveEventEnvelope{
		Type: "db.query",
		SQL: &liveSQLEvent{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Operation:  "select",
			Query:      "SELECT 1",
			DurationMS: 2,
		},
	})
	panel.ingestClusterLiveEvent("node-b", liveEventEnvelope{
		Type: "session.activity",
		Session: &liveSessionActivity{
			SessionToken: "token-node-b",
			TokenShort:   "token-no",
			UserID:       "u-7",
			LastRoute:    "/api/orders",
			LastSeenAt:   time.Now().UTC().Format(time.RFC3339),
		},
	})

	requests := panel.live.requests.latest(5)
	if len(requests) != 1 {
		t.Fatalf("expected one request from cluster, got %d", len(requests))
	}
	if requests[0].NodeID != "node-b" {
		t.Fatalf("expected request node_id=node-b, got %q", requests[0].NodeID)
	}

	queries := panel.live.sql.latest(5)
	if len(queries) != 1 {
		t.Fatalf("expected one sql event from cluster, got %d", len(queries))
	}
	if queries[0].NodeID != "node-b" {
		t.Fatalf("expected sql node_id=node-b, got %q", queries[0].NodeID)
	}

	sessions := panel.live.sessions.snapshot(5)
	if len(sessions) != 1 {
		t.Fatalf("expected one session event from cluster, got %d", len(sessions))
	}
	if sessions[0].NodeID != "node-b" {
		t.Fatalf("expected session node_id=node-b, got %q", sessions[0].NodeID)
	}
}

func TestPanelIngestClusterLiveEvent_RespectsExcludePatterns(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.ingestClusterLiveEvent("node-c", liveEventEnvelope{
		Type: "http.request",
		Request: &liveRequestEvent{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Method:     http.MethodGet,
			Path:       "/admin/system",
			Status:     http.StatusOK,
			DurationMS: 3,
		},
	})
	panel.ingestClusterLiveEvent("node-c", liveEventEnvelope{
		Type: "http.request",
		Request: &liveRequestEvent{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Method:     http.MethodGet,
			Path:       "/health",
			Status:     http.StatusOK,
			DurationMS: 4,
		},
	})

	rows := panel.live.requests.latest(10)
	if len(rows) != 1 {
		t.Fatalf("expected only one non-excluded request, got %#v", rows)
	}
	if rows[0].Path != "/health" {
		t.Fatalf("expected /health request to be retained, got %#v", rows[0])
	}
}

func TestPanelLiveSnapshotSupportsNodeFilter(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.ingestClusterLiveEvent("node-a", liveEventEnvelope{
		Type: "http.request",
		Request: &liveRequestEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Method:    http.MethodGet,
			Path:      "/api/a",
			Status:    http.StatusOK,
		},
	})
	panel.ingestClusterLiveEvent("node-b", liveEventEnvelope{
		Type: "http.request",
		Request: &liveRequestEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Method:    http.MethodGet,
			Path:      "/api/b",
			Status:    http.StatusOK,
		},
	})

	panel.ingestClusterLiveEvent("node-a", liveEventEnvelope{
		Type: "db.query",
		SQL: &liveSQLEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Query:     "SELECT 1",
			Operation: "select",
		},
	})
	panel.ingestClusterLiveEvent("node-b", liveEventEnvelope{
		Type: "session.activity",
		Session: &liveSessionActivity{
			SessionToken: "token-b",
			TokenShort:   "token-b",
			LastRoute:    "/api/b",
			LastSeenAt:   time.Now().UTC().Format(time.RFC3339),
		},
	})

	h := panel.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/live/snapshot?node=node-a&requests_limit=10&sql_limit=10&sessions_limit=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload liveSnapshotResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, rr.Body.String())
	}
	if payload.NodeFilter != "node-a" {
		t.Fatalf("expected node filter node-a, got %q", payload.NodeFilter)
	}
	if len(payload.Requests) != 1 || payload.Requests[0].NodeID != "node-a" {
		t.Fatalf("expected one node-a request, got %#v", payload.Requests)
	}
	if len(payload.Queries) != 1 || payload.Queries[0].NodeID != "node-a" {
		t.Fatalf("expected one node-a sql row, got %#v", payload.Queries)
	}
	if len(payload.Sessions) != 0 {
		t.Fatalf("expected no node-a session rows, got %#v", payload.Sessions)
	}
}

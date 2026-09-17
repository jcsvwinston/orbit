package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// fakeCache is a declared cache, the way an application implements one.
type fakeCache struct {
	mu       sync.Mutex
	entries  int64
	known    bool
	statsErr error
	flushErr error
	flushed  int
}

func (c *fakeCache) CacheName() string { return "test cache" }

func (c *fakeCache) CacheEntries(context.Context) (int64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.statsErr != nil {
		return 0, false, c.statsErr
	}
	return c.entries, c.known, nil
}

func (c *fakeCache) FlushCache(context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flushErr != nil {
		return 0, c.flushErr
	}
	removed := c.entries
	c.entries = 0
	c.flushed++
	return removed, nil
}

// TestCacheViewWithoutAnyCacheSaysSo pins the OR-49 refusal: an application
// with no cache — the default — used to be told "redis url is not
// configured", which reads as a setting somebody forgot. There is no cache
// here, and no button either.
func TestCacheViewWithoutAnyCacheSaysSo(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	stats, status := doJSON(t, http.MethodGet, srv.URL+"/api/cache", nil)
	if status != http.StatusOK {
		t.Fatalf("cache view: got %d, want 200 body=%s", status, mustJSON(stats))
	}
	if kind, _ := stats["kind"].(string); kind != "none" {
		t.Errorf("kind = %q, want \"none\": %s", kind, mustJSON(stats))
	}
	if canFlush, _ := stats["can_flush"].(bool); canFlush {
		t.Errorf("an application with no cache must not be offered the flush: %s", mustJSON(stats))
	}
	if enabled, _ := stats["enabled"].(bool); enabled {
		t.Errorf("enabled = true with no cache: %s", mustJSON(stats))
	}

	body, status := doJSON(t, http.MethodPost, srv.URL+"/api/cache/flush", map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("flush with no cache: got %d, want 400 body=%s", status, mustJSON(body))
	}
	if msg := mustJSON(body); !strings.Contains(msg, "has not declared a cache") {
		t.Errorf("the refusal does not name the absence: %s", msg)
	}
}

// TestCacheViewUsesTheDeclaredCache is the other half of OR-49: the view
// reports, and the flush empties, the cache the application handed over.
func TestCacheViewUsesTheDeclaredCache(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	cache := &fakeCache{entries: 7, known: true}
	panel.config.Cache = cache

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	stats, status := doJSON(t, http.MethodGet, srv.URL+"/api/cache", nil)
	if status != http.StatusOK {
		t.Fatalf("cache view: got %d, want 200 body=%s", status, mustJSON(stats))
	}
	if kind, _ := stats["kind"].(string); kind != "declared" {
		t.Errorf("kind = %q, want \"declared\"", kind)
	}
	if entries, _ := stats["entries"].(float64); entries != 7 {
		t.Errorf("entries = %v, want 7: %s", stats["entries"], mustJSON(stats))
	}
	if known, _ := stats["entries_known"].(bool); !known {
		t.Errorf("entries_known = false on a cache that counts: %s", mustJSON(stats))
	}
	if canFlush, _ := stats["can_flush"].(bool); !canFlush {
		t.Errorf("can_flush = false on a cache that can be flushed: %s", mustJSON(stats))
	}

	flushed, status := doJSON(t, http.MethodPost, srv.URL+"/api/cache/flush", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("flush: got %d, want 200 body=%s", status, mustJSON(flushed))
	}
	if removed, _ := flushed["removed"].(float64); removed != 7 {
		t.Errorf("removed = %v, want 7: %s", flushed["removed"], mustJSON(flushed))
	}
	if cache.flushed != 1 {
		t.Errorf("the declared cache was flushed %d times, want 1", cache.flushed)
	}
	if count, _, _ := cache.CacheEntries(context.Background()); count != 0 {
		t.Errorf("the cache still holds %d entries after the flush", count)
	}
}

// TestCacheViewReportsAnUncountableCache: a backend that cannot count must
// not be shown as empty, which would read as a working, cold cache.
func TestCacheViewReportsAnUncountableCache(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.Cache = &fakeCache{known: false}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	stats, _ := doJSON(t, http.MethodGet, srv.URL+"/api/cache", nil)
	if known, _ := stats["entries_known"].(bool); known {
		t.Errorf("entries_known = true on a backend that cannot count: %s", mustJSON(stats))
	}
	if canFlush, _ := stats["can_flush"].(bool); !canFlush {
		t.Errorf("a cache that cannot be counted can still be emptied: %s", mustJSON(stats))
	}
}

// TestCacheViewSurfacesAnInspectionFailure: a cache that errors is degraded,
// not absent — the difference between "no cache" and "a broken one" is the
// whole point of the three postures.
func TestCacheViewSurfacesAnInspectionFailure(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.Cache = &fakeCache{statsErr: errors.New("backend unreachable")}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	stats, _ := doJSON(t, http.MethodGet, srv.URL+"/api/cache", nil)
	if got, _ := stats["status"].(string); got != "degraded" {
		t.Errorf("status = %q, want \"degraded\": %s", got, mustJSON(stats))
	}
	if kind, _ := stats["kind"].(string); kind != "declared" {
		t.Errorf("a failing declared cache is still the declared cache, got kind=%q", kind)
	}
}

// TestEmailViewReportsDeliveryNotJustConfiguration pins OR-50 for the
// application with no outbox: the view must say there is no queue rather
// than show zeros that read as "nothing pending".
func TestEmailViewReportsDeliveryNotJustConfiguration(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	body, status := doJSON(t, http.MethodGet, srv.URL+"/api/email", nil)
	if status != http.StatusOK {
		t.Fatalf("email view: got %d, want 200 body=%s", status, mustJSON(body))
	}

	delivery, ok := body["delivery"].(map[string]any)
	if !ok {
		t.Fatalf("the email view carries no delivery section: %s", mustJSON(body))
	}
	if enabled, _ := delivery["enabled"].(bool); enabled {
		t.Errorf("delivery.enabled = true with no outbox configured: %s", mustJSON(body))
	}
	if reason, _ := delivery["reason"].(string); reason == "" {
		t.Errorf("an absent queue with no reason reads as an empty one: %s", mustJSON(body))
	}
	if topic, _ := delivery["topic"].(string); topic == "" {
		t.Errorf("the view does not name the topic mail is queued under: %s", mustJSON(body))
	}

	health, ok := body["health"].(map[string]any)
	if !ok {
		t.Fatalf("the email view says nothing about sender health: %s", mustJSON(body))
	}
	// The default panel has no sender wired: "not checked" must be
	// distinguishable from "checked and healthy".
	if checked, _ := health["checked"].(bool); checked {
		t.Errorf("health.checked = true with no sender: %s", mustJSON(body))
	}
	if reason, _ := health["reason"].(string); reason == "" {
		t.Errorf("an unchecked sender with no reason cannot be told from a healthy one: %s", mustJSON(body))
	}
}

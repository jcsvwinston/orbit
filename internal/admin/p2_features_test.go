package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

type testHealthCheckRow struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func TestPanelHealthCheckReportsRedisHealthy(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	redisServer := miniredis.RunT(t)
	panel.config.RedisURL = "redis://" + redisServer.Addr()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}

	var payload struct {
		Status string               `json:"status"`
		Checks []testHealthCheckRow `json:"checks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Status != "healthy" {
		t.Fatalf("expected healthy status, got %q", payload.Status)
	}

	redisCheck := findHealthCheck(payload.Checks, "redis")
	if redisCheck == nil {
		t.Fatal("expected redis check in health response")
	}
	if redisCheck.Status != "healthy" {
		t.Fatalf("expected redis healthy, got %q", redisCheck.Status)
	}
	if redisCheck.Message != "connected" {
		t.Fatalf("expected redis connected message, got %q", redisCheck.Message)
	}
}

func TestPanelHealthCheckReportsRedisUnhealthy(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	redisServer := miniredis.RunT(t)
	panel.config.RedisURL = "redis://" + redisServer.Addr()
	redisServer.Close()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer res.Body.Close()

	var payload struct {
		Status string               `json:"status"`
		Checks []testHealthCheckRow `json:"checks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if payload.Status != "unhealthy" {
		t.Fatalf("expected unhealthy status, got %q", payload.Status)
	}

	redisCheck := findHealthCheck(payload.Checks, "redis")
	if redisCheck == nil {
		t.Fatal("expected redis check in health response")
	}
	if redisCheck.Status != "unhealthy" {
		t.Fatalf("expected redis unhealthy, got %q", redisCheck.Status)
	}
}

func TestPanelCacheStatsAndFlushUseRedisRuntime(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	redisServer := miniredis.RunT(t)
	panel.config.RedisURL = "redis://" + redisServer.Addr()
	redisServer.Set("cache:one", "1")
	redisServer.Set("cache:two", "2")

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	stats, status := doJSON(t, http.MethodGet, srv.URL+"/api/cache", nil)
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from cache stats, got %d body=%s", status, mustJSON(stats))
	}
	if enabled, _ := stats["enabled"].(bool); !enabled {
		t.Fatalf("expected cache stats enabled=true, got %#v", stats["enabled"])
	}
	if got := stats["status"]; got != "healthy" {
		t.Fatalf("expected healthy cache status, got %#v", got)
	}
	if got := int(stats["key_count"].(float64)); got != 2 {
		t.Fatalf("expected key_count=2, got %d", got)
	}

	flushed, status := doJSON(t, http.MethodPost, srv.URL+"/api/cache/flush", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from cache flush, got %d body=%s", status, mustJSON(flushed))
	}
	if got := int(flushed["key_count_before"].(float64)); got != 2 {
		t.Fatalf("expected key_count_before=2, got %d", got)
	}
	if got := int(flushed["key_count_after"].(float64)); got != 0 {
		t.Fatalf("expected key_count_after=0, got %d", got)
	}
	if redisServer.Exists("cache:one") || redisServer.Exists("cache:two") {
		t.Fatalf("expected redis database to be empty after flush")
	}
}

func TestPanelStorageListUsesConfiguredStore(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	store, err := storage.NewLocalStore(storage.LocalConfig{Path: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalStore failed: %v", err)
	}
	defer store.Close()

	panel.store = store
	panel.config.Store = store

	ctx := context.Background()
	if _, err := store.Put(ctx, "uploads/readme.txt", strings.NewReader("hello"), storage.PutOptions{}); err != nil {
		t.Fatalf("store.Put file failed: %v", err)
	}
	if _, err := store.Put(ctx, "uploads/images/logo.txt", strings.NewReader("logo"), storage.PutOptions{}); err != nil {
		t.Fatalf("store.Put nested file failed: %v", err)
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/storage?path=uploads", nil)
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from storage list, got %d body=%s", status, mustJSON(resp))
	}
	if got := resp["backend"]; got != "store" {
		t.Fatalf("expected backend=store, got %#v", got)
	}

	files, ok := resp["files"].([]interface{})
	if !ok {
		t.Fatalf("expected files array, got %#v", resp["files"])
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 storage entries, got %d", len(files))
	}

	entries := map[string]map[string]interface{}{}
	for _, raw := range files {
		item, _ := raw.(map[string]interface{})
		entries[item["name"].(string)] = item
	}

	if dir := entries["images"]; dir == nil || dir["is_dir"] != true {
		t.Fatalf("expected images directory entry, got %#v", dir)
	}
	if file := entries["readme.txt"]; file == nil || file["is_dir"] != false {
		t.Fatalf("expected readme.txt file entry, got %#v", file)
	}
}

func TestPanelStorageListRejectsPathOutsideRoot(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/storage?path=../secrets", nil)
	if status != http.StatusForbidden {
		t.Fatalf("expected status 403 from invalid storage path, got %d body=%s", status, mustJSON(resp))
	}
	errBody, _ := resp["error"].(map[string]interface{})
	if got := errBody["message"]; got != "access denied: path outside storage root" {
		t.Fatalf("unexpected storage path error message: %#v", got)
	}
}

func TestPanelEmailStatsReflectRuntimeConfiguration(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.MailDriver = "smtp"
	panel.config.MailFrom = "noreply@example.com"
	panel.config.SMTPHost = "smtp.example.com"

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/email", nil)
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from email stats, got %d body=%s", status, mustJSON(resp))
	}
	if enabled, _ := resp["enabled"].(bool); !enabled {
		t.Fatalf("expected email stats enabled=true, got %#v", resp["enabled"])
	}
	if got := resp["driver"]; got != "smtp" {
		t.Fatalf("expected driver=smtp, got %#v", got)
	}
	if got := resp["provider_type"]; got != "builtin" {
		t.Fatalf("expected provider_type=builtin, got %#v", got)
	}
	if got := resp["smtp_host"]; got != "smtp.example.com" {
		t.Fatalf("expected smtp_host, got %#v", got)
	}
}

func TestPanelEmailStatsReportsDisabledNoopDriver(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	panel.config.MailDriver = "noop"

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/email", nil)
	if status != http.StatusOK {
		t.Fatalf("expected status 200 from email stats, got %d body=%s", status, mustJSON(resp))
	}
	if enabled, _ := resp["enabled"].(bool); enabled {
		t.Fatalf("expected email stats enabled=false, got %#v", resp["enabled"])
	}
	if got := resp["status"]; got != "disabled" {
		t.Fatalf("expected status=disabled, got %#v", got)
	}
}

func findHealthCheck(checks []testHealthCheckRow, name string) *testHealthCheckRow {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

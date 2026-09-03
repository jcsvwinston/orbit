package admin

// Regression tests for the defects the 2026-09 maturity audit verified in
// the in-process panel: environment leakage in the system snapshot, live
// excludes that never matched the panel's own routes, a CSV export that
// truncated silently, an export download that reached the whole object
// store, an unbounded import upload, a nil dereference on custom data
// sources, non-numeric ids answering 500, and a health endpoint that
// reported the product name as its version.

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// --- OR-3: the system snapshot must not leak credentials --------------------

func TestSystemEnv_MasksConnectionStringsAndURLCredentials(t *testing.T) {
	rows := buildSystemEnvironmentRows([]string{
		"DATABASE_URL=postgres://app:S3cret@h/db",
		"NUCLEUS_DATABASES_DEFAULT_URL=postgres://app:S3cret@h/db?sslmode=disable",
		"REDIS_URL=redis://:p4ss@cache:6379/0",
		"SENTRY_DSN=https://k3y@o1.ingest.sentry.io/1",
		"BASIC_AUTH=user:pw",
		"AWS_CREDENTIALS_FILE=/etc/aws",
		"TLS_PRIVATE_PEM=/etc/tls/key.pem",
		"APP_BACKEND=postgres://u:p@h/db and mysql://root:root@m/x",
		"APP_MODE=dev",
	})
	index := map[string]systemEnvVar{}
	for _, row := range rows {
		index[row.Name] = row
	}
	for _, name := range []string{"DATABASE_URL", "NUCLEUS_DATABASES_DEFAULT_URL", "REDIS_URL", "SENTRY_DSN", "BASIC_AUTH", "AWS_CREDENTIALS_FILE", "TLS_PRIVATE_PEM"} {
		row := index[name]
		if !row.Masked || row.Value != "***" {
			t.Errorf("%s must be masked by name, got %+v", name, row)
		}
	}
	// A URL hiding behind an innocuous name keeps its shape but loses the
	// password — every occurrence.
	if got := index["APP_BACKEND"].Value; got != "postgres://u:***@h/db and mysql://root:***@m/x" {
		t.Errorf("APP_BACKEND userinfo not redacted: %q", got)
	}
	if index["APP_BACKEND"].Masked {
		t.Error("APP_BACKEND is not flagged by name; only its credentials are redacted")
	}
	if got := index["APP_MODE"].Value; got != "dev" {
		t.Errorf("APP_MODE must pass through, got %q", got)
	}
	for _, row := range rows {
		if strings.Contains(row.Value, "S3cret") || strings.Contains(row.Value, "p4ss") || strings.Contains(row.Value, "root:root") {
			t.Errorf("credential leaked in %s=%q", row.Name, row.Value)
		}
	}
}

// --- OR-15: default excludes must match the panel's own (stripped) routes ---

func TestPanelTrafficMiddleware_ExcludesPanelRoutesUnderStrippedPrefix(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	panelMW := panel.panelTrafficMiddleware(ok)
	hostMW := panel.liveTrafficMiddleware(ok)

	// The panel router sees "/api/audit" (Router.Mount strips "/admin");
	// the default exclude is "/admin". Before the fix nothing matched and
	// the feed was 100% the panel polling itself.
	for _, path := range []string{"/api/audit", "/api/live/snapshot", "/api/system/snapshot", "/"} {
		panelMW.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if got := panel.live.requests.latest(10); len(got) != 0 {
		t.Fatalf("panel routes must be excluded by the default /admin pattern, recorded %d: %+v", len(got), got)
	}

	// The host-router variant still records application traffic.
	hostMW.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/products", nil))
	if got := panel.live.requests.latest(10); len(got) != 1 || got[0].Path != "/products" {
		t.Fatalf("host traffic must be recorded as requested, got %+v", got)
	}

	// Drop the default exclude: the panel route is now recorded under the
	// path the browser requested, prefix included, so operators can filter
	// it back out with a full path.
	if _, err := panel.removeLiveExcludePattern("/admin"); err != nil {
		t.Fatalf("remove exclude: %v", err)
	}
	panelMW.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/audit", nil))
	got := panel.live.requests.latest(1)
	if len(got) != 1 || got[0].Path != "/admin/api/audit" {
		t.Fatalf("panel route must be recorded with its mount prefix, got %+v", got)
	}
	// And a full-path exclude written by the operator matches it again.
	if _, err := panel.addLiveExcludePattern("/admin/api/*"); err != nil {
		t.Fatalf("add exclude: %v", err)
	}
	panelMW.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/audit", nil))
	if got := panel.live.requests.latest(10); len(got) != 2 {
		t.Fatalf("full-path exclude must match the stripped route, recorded %d", len(got))
	}
}

// --- OR-17: CSV export walks the whole table --------------------------------

func TestExportCSV_PaginatesPastOnePage(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	sqlDB, err := panel.db.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	const rows = exportCSVPageSize + 5
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := tx.Exec(`INSERT INTO admin_users (email, name, active, created_at, updated_at) VALUES (?, ?, 1, ?, ?)`,
			fmt.Sprintf("u%d@example.com", i), fmt.Sprintf("User %d", i), time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/models/AdminUser/export")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status %d", resp.StatusCode)
	}
	records, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if got := len(records) - 1; got != rows {
		t.Fatalf("export returned %d data rows, want %d (one page used to truncate silently)", got, rows)
	}
}

// --- OR-18 / OR-21: storage-facing handlers ---------------------------------

// keyedStore is a storage.Store fake that serves only the objects it holds
// and records every key it is asked to Put or Get.
type keyedStore struct {
	mu      sync.Mutex
	objects map[string]string
	puts    []string
	gets    []string
}

func newKeyedStore() *keyedStore { return &keyedStore{objects: map[string]string{}} }

func (s *keyedStore) List(context.Context, storage.ListOptions) (storage.ListResult, error) {
	return storage.ListResult{}, nil
}

func (s *keyedStore) Put(_ context.Context, key string, r io.Reader, _ storage.PutOptions) (storage.ObjectInfo, error) {
	data, _ := io.ReadAll(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts = append(s.puts, key)
	s.objects[key] = string(data)
	return storage.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}

func (s *keyedStore) Get(_ context.Context, key string) (io.ReadCloser, storage.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets = append(s.gets, key)
	if strings.Contains(key, "..") {
		return nil, storage.ObjectInfo{}, storage.ErrInvalidKey("path traversal is not allowed")
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, storage.ObjectInfo{}, storage.ErrNotFound(key)
	}
	return io.NopCloser(strings.NewReader(data)), storage.ObjectInfo{Key: key, Size: int64(len(data)), ContentType: "text/csv"}, nil
}

func (s *keyedStore) Delete(context.Context, string) error         { return nil }
func (s *keyedStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (s *keyedStore) PublicURL(context.Context, string, storage.URLConfig) (string, error) {
	return "", nil
}
func (s *keyedStore) SignedURL(context.Context, string, time.Duration, storage.URLConfig) (string, error) {
	return "", nil
}
func (s *keyedStore) Copy(context.Context, string, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, nil
}
func (s *keyedStore) Close() error { return nil }

func TestExportDownload_ConfinedToExportKeys(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	store := newKeyedStore()
	store.objects["_tmp/export_20260903_x.csv"] = "id,name\n1,a\n"
	store.objects["uploads/private/contract.pdf"] = "secret"
	panel.store = store

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	cases := []struct {
		key  string
		want int
	}{
		{"_tmp/export_20260903_x.csv", http.StatusOK},
		{"_tmp/export_missing.csv", http.StatusNotFound},
		{"uploads/private/contract.pdf", http.StatusForbidden},
		{"../../etc/passwd", http.StatusForbidden},
		{"_tmp/export_../../etc/passwd", http.StatusForbidden},
		{"_tmp/export_a/b.csv", http.StatusForbidden},
		{"_tmp/import_20260903_data.csv", http.StatusForbidden},
	}
	for _, tc := range cases {
		resp, err := http.Get(srv.URL + "/api/exports/download?key=" + tc.key)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("download key=%q: status %d, want %d", tc.key, resp.StatusCode, tc.want)
		}
	}
	// Only the export namespace ever reached the store.
	for _, key := range store.gets {
		if !strings.HasPrefix(key, "_tmp/export_") {
			t.Errorf("store received a non-export key: %q", key)
		}
	}
}

func multipartUpload(t *testing.T, url, filename string, size int) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(bytes.Repeat([]byte("a,b\n"), size/4+1))
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, url+"/api/imports", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestImportUpload_SanitisesFilenameAndBoundsSize(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	store := newKeyedStore()
	panel.store = store

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// Traversal in the client filename is reduced to its base name.
	resp := multipartUpload(t, srv.URL, "../../../etc/evil.csv", 64)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("csv upload: status %d", resp.StatusCode)
	}
	if len(store.puts) != 1 || !strings.HasPrefix(store.puts[0], "_tmp/import_") || !strings.HasSuffix(store.puts[0], "_evil.csv") || strings.Contains(store.puts[0], "..") {
		t.Fatalf("stored key must be _tmp/import_<ts>_evil.csv, got %v", store.puts)
	}

	// Only the formats the importer parses are accepted.
	for _, name := range []string{"payload.exe", "dump.sql", "noext"} {
		resp := multipartUpload(t, srv.URL, name, 64)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("upload %q: status %d, want 400", name, resp.StatusCode)
		}
	}

	// The body is bounded: past the cap the upload is a 413, not a
	// memory-threshold detail of ParseMultipartForm.
	prev := importUploadMaxBytes
	importUploadMaxBytes = 4 << 10
	defer func() { importUploadMaxBytes = prev }()
	resp = multipartUpload(t, srv.URL, "big.json", 16<<10)
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: status %d, want 413", resp.StatusCode)
	}
	if len(store.puts) != 1 {
		t.Fatalf("rejected uploads must never reach the store, puts=%v", store.puts)
	}
}

// --- OR-19: custom data sources have no schema registry --------------------

func TestUpdateFieldMeta_WithoutRegistry_Is501NotPanic(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.registry = nil

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()
	_, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/AdminUser/schema/fields",
		map[string]any{"fields": map[string]any{"email": map[string]any{"label": "E-mail"}}})
	if status != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501", status)
	}
}

// --- OR-22: a non-numeric id is a client error ------------------------------

func TestRecordEndpoints_InvalidID_Is400(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	for _, tc := range []struct {
		method  string
		payload any
	}{
		{http.MethodGet, nil},
		{http.MethodPut, map[string]any{"name": "x"}},
		{http.MethodDelete, nil},
	} {
		body, status := doJSON(t, tc.method, srv.URL+"/api/models/AdminUser/abc", tc.payload)
		if status != http.StatusBadRequest {
			t.Errorf("%s /abc: status %d body=%s, want 400", tc.method, status, mustJSON(body))
		}
	}
}

// --- OR-40: /api/health reports a version and an uptime ---------------------

func TestOrbitVersionFromBuildInfo(t *testing.T) {
	dep := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app", Version: "(devel)"},
		Deps: []*debug.Module{{Path: orbitModulePath, Version: "v1.8.17"}},
	}
	if got := orbitVersionFromBuildInfo(dep); got != "v1.8.17" {
		t.Errorf("dependency version: got %q", got)
	}
	replaced := &debug.BuildInfo{
		Deps: []*debug.Module{{Path: orbitModulePath, Version: "v1.8.17", Replace: &debug.Module{Path: "../orbit", Version: "(devel)"}}},
	}
	if got := orbitVersionFromBuildInfo(replaced); got != "v1.8.17" {
		t.Errorf("replaced dependency: got %q", got)
	}
	main := &debug.BuildInfo{Main: debug.Module{Path: orbitModulePath, Version: "v1.9.0"}}
	if got := orbitVersionFromBuildInfo(main); got != "v1.9.0" {
		t.Errorf("main module: got %q", got)
	}
	if got := orbitVersionFromBuildInfo(&debug.BuildInfo{Main: debug.Module{Path: orbitModulePath, Version: "(devel)"}}); got != "devel" {
		t.Errorf("unstamped build: got %q", got)
	}
}

func TestHealth_ReportsVersionAndUptime(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.startedAt = time.Now().Add(-90 * time.Second)
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	body, status := doJSON(t, http.MethodGet, srv.URL+"/api/health", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if v, _ := body["version"].(string); v == "" || v == "Orbit" {
		t.Errorf("version must be a version or \"devel\", got %q", v)
	}
	up, _ := body["uptime"].(string)
	d, err := time.ParseDuration(up)
	if err != nil || d < 90*time.Second {
		t.Errorf("uptime must be a real duration >= 90s, got %q", up)
	}
}

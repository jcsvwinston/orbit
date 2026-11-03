package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// OR-79: under a multi-tenant application the store the panel is handed
// prefixes every key with the request's tenant. These tests mount the panel
// over that store — a real storage.TenantStore — with two tenants, the way
// nucleus wires one: the host resolves the tenant into the request scope,
// the store reads it from there, the panel's TenantResolver too. The panel's
// other tests hand it a bare store, which is why none of them saw the keys
// it emitted come back prefixed twice.

type storageTenantCtxKey struct{}

// twoTenantStoragePanel is scopedPanel with the tenant taken from the
// X-Tenant-ID header (acme or globex) and a tenant-prefixing store over a
// keyed base store, so the tests can see where each object physically is.
func twoTenantStoragePanel(t *testing.T) (*Panel, *sql.DB, *httptest.Server, *keyedStore) {
	t.Helper()
	panel, sqlDB, _ := scopedPanel(t, operatorAuth())
	base := newKeyedStore()
	panel.store = storage.NewTenantStore(base, func(ctx context.Context) string {
		tenant, _ := ctx.Value(storageTenantCtxKey{}).(string)
		return tenant
	})
	panel.config.TenantResolver = func(r *http.Request) (string, bool) {
		tenant, _ := r.Context().Value(storageTenantCtxKey{}).(string)
		return tenant, tenant != ""
	}
	// The host's request scope: resolved before the panel runs.
	handler := panel.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), storageTenantCtxKey{}, r.Header.Get("X-Tenant-ID"))
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv, base
}

// tenantRequest sends a JSON body (or none) as tenant and decodes the answer.
func tenantRequest(t *testing.T, tenant, method, rawURL string, payload any) (map[string]any, int, string) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Tenant-ID", tenant)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return sendTenantRequest(t, req)
}

func sendTenantRequest(t *testing.T, req *http.Request) (map[string]any, int, string) {
	t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out, res.StatusCode, string(raw)
}

// uploadAs posts a CSV to the import upload as tenant and returns the key
// the panel answered with.
func uploadAs(t *testing.T, srv *httptest.Server, tenant, filename, content string) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "ScopedNote")
	_ = mw.WriteField("format", "csv")
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(content))
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/imports", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Tenant-ID", tenant)
	resp, status, raw := sendTenantRequest(t, req)
	if status != http.StatusCreated {
		t.Fatalf("upload as %s: status %d body %s", tenant, status, raw)
	}
	key, _ := resp["key"].(string)
	return key
}

// assertLogicalKey fails unless key is the panel's own logical key and the
// object it names sits once under tenant's prefix in the base store.
func assertLogicalKey(t *testing.T, base *keyedStore, tenant, key, what string) {
	t.Helper()
	if !strings.HasPrefix(key, "_tmp/") {
		t.Fatalf("%s: the panel answered %q, want the logical key it wrote (_tmp/…), not the physical one", what, key)
	}
	base.mu.Lock()
	_, stored := base.objects[tenant+"/"+key]
	base.mu.Unlock()
	if !stored {
		t.Fatalf("%s: no object at %q in the base store", what, tenant+"/"+key)
	}
}

// keysRead are the physical keys the base store was asked for.
func keysRead(base *keyedStore) []string {
	base.mu.Lock()
	defer base.mu.Unlock()
	return append([]string(nil), base.gets...)
}

func TestTenantStorageKeys_ImportUploadValidateExecute(t *testing.T) {
	_, sqlDB, srv, base := twoTenantStoragePanel(t)

	for _, tenant := range []string{"acme", "globex"} {
		key := uploadAs(t, srv, tenant, "notes.csv", "title\nimported for "+tenant+"\n")
		assertLogicalKey(t, base, tenant, key, "upload as "+tenant)

		resp, status, raw := tenantRequest(t, tenant, http.MethodPost, srv.URL+"/api/import/validate?key="+url.QueryEscape(key),
			map[string]any{"model": "ScopedNote", "format": "csv"})
		if status != http.StatusOK || resp["can_proceed"] != true {
			t.Fatalf("validate as %s with the key the upload answered: status %d body %s", tenant, status, raw)
		}
		resp, status, raw = tenantRequest(t, tenant, http.MethodPost, srv.URL+"/api/import/execute?key="+url.QueryEscape(key),
			map[string]any{"model": "ScopedNote", "format": "csv"})
		if status != http.StatusOK || resp["imported"] != float64(1) {
			t.Fatalf("execute as %s with the key the upload answered: status %d body %s", tenant, status, raw)
		}
	}
	// Each file went into its own tenant: the seed note plus the import.
	if noteCount(t, sqlDB, "acme") != 2 || noteCount(t, sqlDB, "globex") != 2 {
		t.Fatalf("rows after the imports: acme=%d globex=%d, want 2 and 2", noteCount(t, sqlDB, "acme"), noteCount(t, sqlDB, "globex"))
	}
	for _, k := range keysRead(base) {
		if strings.HasPrefix(k, "acme/acme/") || strings.HasPrefix(k, "globex/globex/") {
			t.Fatalf("the store was asked for %q: a key prefixed twice", k)
		}
	}
}

func TestTenantStorageKeys_DumpdataLoaddata(t *testing.T) {
	_, _, srv, base := twoTenantStoragePanel(t)

	for _, tenant := range []string{"acme", "globex"} {
		resp, status, raw := tenantRequest(t, tenant, http.MethodPost, srv.URL+"/api/fixtures/dumpdata", map[string]any{"models": []string{"ScopedNote"}})
		if status != http.StatusOK {
			t.Fatalf("dumpdata as %s: status %d body %s", tenant, status, raw)
		}
		key, _ := resp["storage_key"].(string)
		assertLogicalKey(t, base, tenant, key, "dumpdata as "+tenant)

		// The dump holds the tenant's one note; loading it back skips it.
		resp, status, raw = tenantRequest(t, tenant, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": key, "on_conflict": "skip"})
		if status != http.StatusOK || resp["total"] != float64(1) || resp["skipped"] != float64(1) || resp["failed"] != float64(0) {
			t.Fatalf("loaddata as %s with the key dumpdata answered: status %d body %s", tenant, status, raw)
		}
	}
}

func TestTenantStorageKeys_ExportDownload(t *testing.T) {
	_, _, srv, base := twoTenantStoragePanel(t)

	for _, tc := range []struct{ tenant, note string }{{"acme", "Acme note"}, {"globex", "Globex note"}} {
		resp, status, raw := tenantRequest(t, tc.tenant, http.MethodPost, srv.URL+"/api/exports", map[string]any{"format": "csv", "models": []string{"ScopedNote"}})
		if status != http.StatusOK {
			t.Fatalf("export as %s: status %d body %s", tc.tenant, status, raw)
		}
		key, _ := resp["storage_key"].(string)
		assertLogicalKey(t, base, tc.tenant, key, "export as "+tc.tenant)
		if resp["id"] != key {
			t.Fatalf("export as %s: job id %v, want the logical key %q", tc.tenant, resp["id"], key)
		}

		if _, status, raw := tenantRequest(t, tc.tenant, http.MethodGet, srv.URL+"/api/exports/"+url.PathEscape(key), nil); status != http.StatusOK {
			t.Fatalf("export status as %s: %d body %s", tc.tenant, status, raw)
		}
		_, status, raw = tenantRequest(t, tc.tenant, http.MethodGet, srv.URL+"/api/exports/download?key="+url.QueryEscape(key), nil)
		if status != http.StatusOK || !strings.Contains(raw, tc.note) {
			t.Fatalf("download as %s with the key the export answered: status %d body %q", tc.tenant, status, raw)
		}
	}
}

// A client of an earlier version holds the PHYSICAL key it was handed
// ("acme/_tmp/…"). Sent back by the same tenant, it is read as the logical
// key it stands for, so the upgrade does not strand an upload or an export
// in flight.
func TestTenantStorageKeys_PhysicalKeyOfTheSameTenantStillWorks(t *testing.T) {
	_, _, srv, _ := twoTenantStoragePanel(t)

	key := uploadAs(t, srv, "acme", "notes.csv", "title\nlegacy\n")
	resp, status, raw := tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/import/validate?key="+url.QueryEscape("acme/"+key),
		map[string]any{"model": "ScopedNote", "format": "csv"})
	if status != http.StatusOK || resp["can_proceed"] != true {
		t.Fatalf("validate with the physical key of the own tenant: status %d body %s", status, raw)
	}

	resp, _, _ = tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/exports", map[string]any{"format": "csv", "models": []string{"ScopedNote"}})
	exportKey, _ := resp["storage_key"].(string)
	if _, status, raw := tenantRequest(t, "acme", http.MethodGet, srv.URL+"/api/exports/download?key="+url.QueryEscape("acme/"+exportKey), nil); status != http.StatusOK || !strings.Contains(raw, "Acme note") {
		t.Fatalf("download with the physical key of the own tenant: status %d body %q", status, raw)
	}

	resp, _, _ = tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/fixtures/dumpdata", map[string]any{"models": []string{"ScopedNote"}})
	fixtureKey, _ := resp["storage_key"].(string)
	if resp, status, raw := tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "acme/" + fixtureKey}); status != http.StatusOK || resp["failed"] != float64(0) {
		t.Fatalf("loaddata with the physical key of the own tenant: status %d body %s", status, raw)
	}
}

// Another tenant's key — logical or physical — reaches nothing: the import
// file and the fixture are not found, the export is not found (the logical
// key is not one of this tenant's jobs) or not an export key at all (the
// physical one), and the base store is never asked for the other tenant's
// object.
func TestTenantStorageKeys_AnotherTenantsKeyIsRefused(t *testing.T) {
	_, sqlDB, srv, base := twoTenantStoragePanel(t)

	importKey := uploadAs(t, srv, "acme", "notes.csv", "title\nacme only\n")
	resp, _, _ := tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/exports", map[string]any{"format": "csv", "models": []string{"ScopedNote"}})
	exportKey, _ := resp["storage_key"].(string)
	resp, _, _ = tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/fixtures/dumpdata", map[string]any{"models": []string{"ScopedNote"}})
	fixtureKey, _ := resp["storage_key"].(string)

	readsBefore := len(keysRead(base))
	for _, key := range []string{importKey, "acme/" + importKey} {
		for _, step := range []string{"validate", "execute"} {
			_, status, raw := tenantRequest(t, "globex", http.MethodPost, srv.URL+"/api/import/"+step+"?key="+url.QueryEscape(key),
				map[string]any{"model": "ScopedNote", "format": "csv"})
			if status != http.StatusNotFound {
				t.Errorf("import %s as globex with acme's key %q: status %d body %s, want 404", step, key, status, raw)
			}
			if strings.Contains(raw, "globex/") {
				t.Errorf("import %s as globex: the answer names a physical key: %s", step, raw)
			}
		}
	}
	for _, key := range []string{fixtureKey, "acme/" + fixtureKey} {
		if _, status, raw := tenantRequest(t, "globex", http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": key}); status != http.StatusNotFound {
			t.Errorf("loaddata as globex with acme's key %q: status %d body %s, want 404", key, status, raw)
		}
	}
	if _, status, raw := tenantRequest(t, "globex", http.MethodGet, srv.URL+"/api/exports/download?key="+url.QueryEscape(exportKey), nil); status != http.StatusNotFound {
		t.Errorf("download as globex with acme's logical export key: status %d body %s, want 404", status, raw)
	}
	if _, status, raw := tenantRequest(t, "globex", http.MethodGet, srv.URL+"/api/exports/download?key="+url.QueryEscape("acme/"+exportKey), nil); status != http.StatusForbidden {
		t.Errorf("download as globex with acme's physical export key: status %d body %s, want 403", status, raw)
	}

	for _, k := range keysRead(base)[readsBefore:] {
		if strings.HasPrefix(k, "acme/") {
			t.Fatalf("a globex request read %q from the base store", k)
		}
	}
	if noteCount(t, sqlDB, "globex") != 1 || noteCount(t, sqlDB, "acme") != 1 {
		t.Fatalf("rows after the refused requests: acme=%d globex=%d, want 1 and 1", noteCount(t, sqlDB, "acme"), noteCount(t, sqlDB, "globex"))
	}
}

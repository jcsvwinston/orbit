package adminbench

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit"
)

// Import, fixtures and the stored export under a tenant (OR-79).
//
// A multi-tenant application hands the panel a store that prefixes every
// key with the request's tenant. The bench's own application is not
// multi-tenant, so DS-13/14/15 never met that store, and the panel handed
// out keys that its next step could not read: the key it answered with was
// the physical one, already prefixed, and the store prefixed it again. Each
// of those probes now also runs its flow once more on a multi-tenant
// application whose host resolves the tenant from a header, and records
// partial if it does not work there.

const benchTenantHeader = "X-Tenant-ID"

// tenantHeader puts the tenant on every request the client makes, the way
// a browser on the tenant's host sends it.
type tenantHeader struct {
	tenant string
	next   http.RoundTripper
}

func (h tenantHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(benchTenantHeader, h.tenant)
	return h.next.RoundTrip(r)
}

// tenantedBench is the bench application as a multi-tenant one, and an
// operator signed in on tenant's host.
type tenantedBench struct {
	srv    *nucleustest.Server
	client *http.Client
}

func startTenantedBench(t *testing.T, tenant string) *tenantedBench {
	t.Helper()
	cfg := benchConfig(t)
	cfg.MultiTenant.Enabled = true
	cfg.MultiTenant.Resolver = "header"
	cfg.MultiTenant.Header = benchTenantHeader
	srv := nucleustest.StartApp(t, nucleus.App{
		Config: cfg,
		Modules: map[string]nucleus.ModuleSpec{
			"content": contentModule(),
			"orbit": orbit.Module(orbit.Config{
				Prefix:             "/admin",
				Title:              "Tenanted storage",
				BootstrapUsername:  "admin",
				BootstrapEmail:     "admin@example.test",
				BootstrapPassword:  bootstrapPassword,
				MultiTenantEnabled: true,
				MultiTenantIDs:     []string{"acme", "globex"},
			}),
		},
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar:       jar,
		Transport: tenantHeader{tenant: tenant, next: http.DefaultTransport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.PostForm(srv.URL("/admin/login"), url.Values{"username": {"admin"}, "password": {bootstrapPassword}})
	if err != nil {
		t.Fatalf("sign in on %s: %v", tenant, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		t.Fatalf("sign in on %s answered %d", tenant, resp.StatusCode)
	}
	return &tenantedBench{srv: srv, client: client}
}

// do sends a JSON body (or none) and returns the status and the body.
func (b *tenantedBench) do(t *testing.T, method, path string, payload any) (int, string) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, b.srv.URL(path), body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	return b.send(t, req)
}

func (b *tenantedBench) send(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func jsonField(raw, key string) string {
	var payload map[string]any
	_ = json.Unmarshal([]byte(raw), &payload)
	s, _ := payload[key].(string)
	return s
}

// importUnderATenant is DS-14's flow on a tenant's host: upload, validate
// and execute with the key the upload answered.
func importUnderATenant(t *testing.T) bool {
	b := startTenantedBench(t, "acme")
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "notes.csv")
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	_, _ = part.Write([]byte("title,status\ntenant-row,tenanted\n"))
	_ = w.Close()
	req, err := http.NewRequest(http.MethodPost, b.srv.URL("/admin/api/imports"), &buf)
	if err != nil {
		t.Fatalf("build upload: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	code, raw := b.send(t, req)
	key := jsonField(raw, "key")
	if code >= 400 || key == "" {
		t.Logf("under a tenant, the upload answered %d: %s", code, truncate(raw))
		return false
	}
	cfg := map[string]any{"model": "Note", "format": "csv", "on_conflict": "skip"}
	for _, step := range []string{"validate", "execute"} {
		if code, raw := b.do(t, http.MethodPost, "/admin/api/import/"+step+"?key="+url.QueryEscape(key), cfg); code != http.StatusOK {
			t.Logf("under a tenant, %s with the key the upload answered (%s) answered %d: %s", step, key, code, truncate(raw))
			return false
		}
	}
	if code, raw := b.do(t, http.MethodGet, "/admin/api/models/Note?status=tenanted", nil); code != http.StatusOK || !strings.Contains(raw, "tenant-row") {
		t.Logf("under a tenant, execute reported success and the row is not there: %d %s", code, truncate(raw))
		return false
	}
	return true
}

// fixturesUnderATenant is DS-15's flow on a tenant's host: dump, then load
// with the key the dump answered.
func fixturesUnderATenant(t *testing.T) bool {
	b := startTenantedBench(t, "acme")
	if code, raw := b.do(t, http.MethodPost, "/admin/api/models/Note", map[string]any{"title": "tenant-fixture", "status": "fixture"}); code != http.StatusCreated {
		t.Fatalf("under a tenant, seeding a note answered %d: %s", code, truncate(raw))
	}
	code, raw := b.do(t, http.MethodPost, "/admin/api/fixtures/dumpdata", map[string]any{"models": []string{"Note"}})
	key := jsonField(raw, "storage_key")
	if code != http.StatusOK || key == "" {
		t.Logf("under a tenant, dumpdata answered %d: %s", code, truncate(raw))
		return false
	}
	if code, raw := b.do(t, http.MethodPost, "/admin/api/fixtures/loaddata", map[string]any{"key": key, "on_conflict": "skip"}); code != http.StatusOK {
		t.Logf("under a tenant, loaddata with the key dumpdata answered (%s) answered %d: %s", key, code, truncate(raw))
		return false
	}
	return true
}

// storedExportUnderATenant is the stored export on a tenant's host: cut it,
// then download it with the key the export answered.
func storedExportUnderATenant(t *testing.T) bool {
	b := startTenantedBench(t, "acme")
	if code, raw := b.do(t, http.MethodPost, "/admin/api/models/Note", map[string]any{"title": "tenant-export", "status": "export"}); code != http.StatusCreated {
		t.Fatalf("under a tenant, seeding a note answered %d: %s", code, truncate(raw))
	}
	code, raw := b.do(t, http.MethodPost, "/admin/api/exports", map[string]any{"format": "csv", "models": []string{"Note"}})
	key := jsonField(raw, "storage_key")
	if code != http.StatusOK || key == "" {
		t.Logf("under a tenant, the stored export answered %d: %s", code, truncate(raw))
		return false
	}
	if code, raw := b.do(t, http.MethodGet, "/admin/api/exports/download?key="+url.QueryEscape(key), nil); code != http.StatusOK || !strings.Contains(raw, "tenant-export") {
		t.Logf("under a tenant, the download with the key the export answered (%s) answered %d: %s", key, code, truncate(raw))
		return false
	}
	return true
}

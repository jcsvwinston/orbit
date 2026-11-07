package admin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/storage"
)

// OR-141: under a multi-tenant application the store prefixes every key
// with the request's tenant, and the ObjectInfo it answers names that
// physical key. The file-widget upload answered with it, the form wrote it
// into the record, and every read of the file through the same store
// prefixed it a second time: "acme/acme/admin/uploads/…", which does not
// exist. The upload now answers the logical key, the one the import, the
// export and the fixture dump answer since OR-79.

// twoTenantFormsPanel is formsPanel (Album with a file widget on cover)
// over a tenant-prefixing store, the way storage_keys_tenant_test.go
// mounts the import: the tenant comes from the X-Tenant-ID header into the
// request scope, where the store and the panel's resolver read it.
func twoTenantFormsPanel(t *testing.T) (*Panel, *httptest.Server, *keyedStore) {
	t.Helper()
	panel, _, _ := formsPanel(t, nil)
	base := newKeyedStore()
	panel.store = storage.NewTenantStore(base, func(ctx context.Context) string {
		tenant, _ := ctx.Value(storageTenantCtxKey{}).(string)
		return tenant
	})
	panel.config.TenantResolver = func(r *http.Request) (string, bool) {
		tenant, _ := r.Context().Value(storageTenantCtxKey{}).(string)
		return tenant, tenant != ""
	}
	handler := panel.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), storageTenantCtxKey{}, r.Header.Get("X-Tenant-ID"))
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return panel, srv, base
}

// uploadCoverAs posts one file to Album's cover through the widget upload
// as tenant ("" for a request the host resolves no tenant for) and returns
// the key the panel answered.
func uploadCoverAs(t *testing.T, srv *httptest.Server, tenant, content string) string {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="cover.png"`)
	header.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(content))
	_ = mw.WriteField("field", "cover")
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/models/Album/upload", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	resp, status, raw := sendTenantRequest(t, req)
	if status != http.StatusCreated {
		t.Fatalf("upload as %q: status %d body %s", tenant, status, raw)
	}
	key, _ := resp["key"].(string)
	return key
}

// readAs reads key through the panel's store as tenant: the path the
// application takes to serve the file a record names.
func readAs(t *testing.T, panel *Panel, tenant, key string) (string, error) {
	t.Helper()
	ctx := context.WithValue(context.Background(), storageTenantCtxKey{}, tenant)
	reader, _, err := panel.store.Get(ctx, key)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), nil
}

func TestFieldUpload_AnswersTheLogicalKeyUnderATenant(t *testing.T) {
	panel, srv, base := twoTenantFormsPanel(t)

	key := uploadCoverAs(t, srv, "acme", "acme's cover")
	if !strings.HasPrefix(key, "admin/uploads/album/") {
		t.Fatalf("the upload answered %q, want the logical key it wrote (admin/uploads/…), not the physical one", key)
	}
	base.mu.Lock()
	_, stored := base.objects["acme/"+key]
	base.mu.Unlock()
	if !stored {
		t.Fatalf("no object at %q in the base store: %v", "acme/"+key, base.objects)
	}

	// The record stores the key the form was handed, and the file is read
	// back by it through the same store, once prefixed.
	resp, status, raw := tenantRequest(t, "acme", http.MethodPost, srv.URL+"/api/models/Album", map[string]any{"title": "With a cover", "cover": key})
	if status != http.StatusCreated {
		t.Fatalf("create with the cover: status %d body %s", status, raw)
	}
	id := fmt.Sprint(resp["id"])
	rec, status, raw := tenantRequest(t, "acme", http.MethodGet, srv.URL+"/api/models/Album/"+id, nil)
	if status != http.StatusOK || rec["cover"] != key {
		t.Fatalf("the record reads back cover=%v (status %d body %s), want the key the upload answered", rec["cover"], status, raw)
	}
	got, err := readAs(t, panel, "acme", key)
	if err != nil {
		t.Fatalf("the file the record names is not readable under its tenant: %v (base store reads: %v)", err, keysRead(base))
	}
	if got != "acme's cover" {
		t.Fatalf("read back %q, want the bytes uploaded", got)
	}

	// Another tenant reads nothing by the same key: the store keeps each
	// tenant inside its prefix.
	if _, err := readAs(t, panel, "globex", key); !storageNotFound(err) {
		t.Fatalf("globex read acme's file by its key: err=%v", err)
	}

	// The audit entry names the key the form holds.
	var uploads []map[string]any
	for _, e := range auditEntriesOf(t, srv.URL, "Album") {
		if e["action"] == "field.upload" {
			uploads = append(uploads, e)
		}
	}
	if len(uploads) != 1 || uploads[0]["record_id"] != key {
		t.Fatalf("field.upload audit entries = %v, want one naming %q", uploads, key)
	}
	if newValue, _ := uploads[0]["new_value"].(map[string]any); newValue["key"] != key {
		t.Fatalf("field.upload new_value = %v, want key %q", uploads[0]["new_value"], key)
	}
}

// Without a tenant the key is what it always was: the store adds nothing,
// and the logical key is the physical one.
func TestFieldUpload_WithoutATenantTheKeyIsUnchanged(t *testing.T) {
	panel, srv, base := twoTenantFormsPanel(t)

	key := uploadCoverAs(t, srv, "", "nobody's cover")
	if !strings.HasPrefix(key, "admin/uploads/album/") {
		t.Fatalf("the upload answered %q, want admin/uploads/…", key)
	}
	base.mu.Lock()
	_, stored := base.objects[key]
	base.mu.Unlock()
	if !stored {
		t.Fatalf("no object at %q in the base store: %v", key, base.objects)
	}
	if got, err := readAs(t, panel, "", key); err != nil || got != "nobody's cover" {
		t.Fatalf("read back %q, %v", got, err)
	}
}

package admin

// Regression tests for OR-23 of the 2026-09 maturity audit: the tenant was
// a view filter on the list. ?tenant=all switched it off for anyone, Get /
// Update / Delete / bulk reached any row by id, a create could name another
// tenant, and exports, imports and fixtures took tenant_id from the body.
// The "request's resolved tenant" the docs promised never arrived either:
// the middleware read a context key nothing wrote.
//
// Now the tenant comes from the host (PanelConfig.TenantResolver) and every
// Data Studio operation is confined to it; ?tenant= is gated to superusers
// (or the tenant_switch RBAC action) and audited.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	dsnucleus "github.com/jcsvwinston/orbit/internal/datasource/nucleus"
)

// ScopedNote is a tenant-scoped model: nucleus detects tenant_id by
// convention, so mi.TenantField is set without any panel override.
type ScopedNote struct {
	model.BaseModel
	TenantID string `db:"column:tenant_id;required" json:"tenant_id" admin:"list,filter"`
	Title    string `db:"column:title;required" json:"title" admin:"list,search"`
}

func (ScopedNote) TableName() string { return "scoped_notes" }

// ScopedCode is a tenant-scoped model whose unique index is global — one
// code across every tenant — the way importers detect an existing row.
type ScopedCode struct {
	model.BaseModel
	TenantID string `db:"column:tenant_id;required" json:"tenant_id" admin:"list,filter"`
	Code     string `db:"column:code;required;unique" json:"code" admin:"list,search"`
}

func (ScopedCode) TableName() string { return "scoped_codes" }

// Gadget declares no searchable field (OR-43).
type Gadget struct {
	model.BaseModel
	Label string `db:"column:label" json:"label" admin:"list"`
	Qty   int    `db:"column:qty" json:"qty" admin:"list,filter"`
}

func (Gadget) TableName() string { return "gadgets" }

const extraModelsDDL = `
CREATE TABLE IF NOT EXISTS scoped_notes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	tenant_id TEXT NOT NULL,
	title TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS scoped_codes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	tenant_id TEXT NOT NULL,
	code TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS gadgets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	label TEXT,
	qty INTEGER
);`

// setupPanelWithModels mirrors setupPanelForTestWithAuth (with-auth posture)
// but registers ScopedNote and Gadget next to AdminUser. It is kept apart
// from the shared helper so the fifty-odd flows built on that one stay
// untouched.
func setupPanelWithModels(t *testing.T, adminAuth AdminAuth) (*Panel, *sql.DB, func()) {
	t.Helper()

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine: db.EngineSQL, DatabaseURL: "sqlite://:memory:", DatabaseMaxOpen: 1, DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New failed: %v", err)
	}
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB failed: %v", err)
	}
	if err := ensureAdminUserSchema(sqlDB); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}
	if _, err := sqlDB.Exec(extraModelsDDL); err != nil {
		t.Fatalf("extra schema setup failed: %v", err)
	}

	registry := model.NewRegistry()
	for _, m := range []any{&AdminUser{}, &ScopedNote{}, &ScopedCode{}, &Gadget{}} {
		if err := registry.Register(m); err != nil {
			t.Fatalf("registry.Register failed: %v", err)
		}
	}

	var panel *Panel
	src := dsnucleus.New(dsnucleus.Config{
		Registry: registry,
		Resolve: func(alias string) (*db.DB, string, error) {
			h, err := panel.databaseHandle(alias)
			if err != nil {
				return nil, "", err
			}
			return h, h.System(), nil
		},
		BusConnected: func() bool { return true },
	})
	panel = NewPanel(src, logger, PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            adminAuth,
		SchemaRegistry:  registry,
		DatabaseHandles: map[string]*db.DB{"default": database},
		AuditEnabled:    true,
		AuditMaxSize:    100,
	})
	return panel, sqlDB, func() { _ = database.Close() }
}

// scopedPanel is a multi-tenant panel whose host resolves every request to
// "acme", seeded with one acme note (id 1) and one globex note (id 2), and
// one code per tenant (A-1 id 1, G-1 id 2).
func scopedPanel(t *testing.T, adminAuth AdminAuth) (*Panel, *sql.DB, *httptest.Server) {
	t.Helper()
	panel, sqlDB, cleanup := setupPanelWithModels(t, adminAuth)
	t.Cleanup(cleanup)
	panel.config.MultiTenantEnabled = true
	panel.config.MultiTenantAutoFilter = true
	panel.config.TenantResolver = func(*http.Request) (string, bool) { return "acme", true }
	panel.store = newKeyedStore()

	for _, row := range [][2]string{{"acme", "Acme note"}, {"globex", "Globex note"}} {
		if _, err := sqlDB.Exec(`INSERT INTO scoped_notes (tenant_id, title, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range [][2]string{{"acme", "A-1"}, {"globex", "G-1"}} {
		if _, err := sqlDB.Exec(`INSERT INTO scoped_codes (tenant_id, code, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

func noteCount(t *testing.T, sqlDB *sql.DB, tenant string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM scoped_notes WHERE tenant_id = ? AND deleted_at IS NULL`, tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func auditActions(t *testing.T, srv *httptest.Server, action string) int {
	t.Helper()
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/audit?action="+action, nil)
	if status != http.StatusOK {
		t.Fatalf("audit status %d body=%s", status, mustJSON(resp))
	}
	entries, _ := resp["entries"].([]interface{})
	return len(entries)
}

var (
	operatorAuth = func() AdminAuth {
		return &testAdminAuth{user: &auth.User{ID: "1", Username: "operator", Role: "admin"}}
	}
	superuserAuth = func() AdminAuth {
		return &testAdminAuth{user: &auth.User{ID: "2", Username: "root", Role: "admin", IsSuperuser: true}}
	}
)

func TestTenantScope_HostResolvedTenantConfinesTheList(t *testing.T) {
	_, _, srv := scopedPanel(t, operatorAuth())

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
	items, _ := resp["items"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["tenant_id"] != "acme" {
		t.Fatalf("items = %v, want only the acme row (the host-resolved tenant never reached the panel before)", resp["items"])
	}

	// Naming the tenant the host already resolved is a no-op, not a switch.
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=acme", nil)
	if status != http.StatusOK {
		t.Fatalf("?tenant=acme status %d body=%s", status, mustJSON(resp))
	}
	if auditActions(t, srv, "tenant.override") != 0 {
		t.Fatal("a no-op ?tenant= must not be audited as an override")
	}
}

func TestTenantScope_SwitchIsGatedAndAudited(t *testing.T) {
	// A plain operator cannot leave the tenant, and the refusal is not an
	// override to audit.
	_, _, srv := scopedPanel(t, operatorAuth())
	for _, q := range []string{"?tenant=all", "?tenant=globex", "?tenant=ALL"} {
		resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote"+q, nil)
		if status != http.StatusForbidden {
			t.Errorf("%s: status %d body=%s, want 403", q, status, mustJSON(resp))
		}
	}
	if auditActions(t, srv, "tenant.override") != 0 {
		t.Fatal("a refused switch must not be audited as an override")
	}

	// A superuser may, and every switch is audited.
	_, _, srv = scopedPanel(t, superuserAuth())
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=all", nil)
	if status != http.StatusOK {
		t.Fatalf("superuser ?tenant=all status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 2 {
		t.Fatalf("superuser ?tenant=all items = %v, want both rows", resp["items"])
	}
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=globex", nil)
	if status != http.StatusOK {
		t.Fatalf("superuser ?tenant=globex status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 1 || items[0].(map[string]interface{})["tenant_id"] != "globex" {
		t.Fatalf("superuser ?tenant=globex items = %v, want the globex row", resp["items"])
	}
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/audit?action=tenant.override", nil)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("audit entries = %v, want one tenant.override per switch", resp["entries"])
	}
	seen := map[string]bool{}
	for _, e := range entries {
		entry := e.(map[string]interface{})
		seen[fmt.Sprint(entry["record_id"])] = true
		if entry["username"] != "root" {
			t.Errorf("override entry must name the operator, got %v", entry)
		}
	}
	if !seen["all"] || !seen["globex"] {
		t.Fatalf("override entries must record the requested tenant, got %v", seen)
	}
}

func TestTenantScope_OtherTenantRowIsNotFound(t *testing.T) {
	_, sqlDB, srv := scopedPanel(t, operatorAuth())
	globexURL := srv.URL + "/api/models/ScopedNote/2"

	for _, tc := range []struct {
		method  string
		payload any
	}{
		{http.MethodGet, nil},
		{http.MethodPut, map[string]any{"title": "hijacked"}},
		{http.MethodDelete, nil},
	} {
		resp, status := doJSON(t, tc.method, globexURL, tc.payload)
		if status != http.StatusNotFound {
			t.Errorf("%s globex row: status %d body=%s, want 404", tc.method, status, mustJSON(resp))
		}
	}
	if noteCount(t, sqlDB, "globex") != 1 {
		t.Fatal("the globex row must be untouched")
	}
	var title string
	if err := sqlDB.QueryRow(`SELECT title FROM scoped_notes WHERE id = 2`).Scan(&title); err != nil || title != "Globex note" {
		t.Fatalf("globex title = %q err=%v, want unchanged", title, err)
	}

	// The own-tenant row is reachable as before.
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote/1", nil)
	if status != http.StatusOK || resp["tenant_id"] != "acme" {
		t.Fatalf("acme row: status %d body=%s", status, mustJSON(resp))
	}
}

func TestTenantScope_BulkDeleteSkipsOtherTenantRows(t *testing.T) {
	_, sqlDB, srv := scopedPanel(t, operatorAuth())

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/ScopedNote/bulk", map[string]any{
		"action": "delete", "ids": []string{"2", "1"},
	})
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["deleted"].(float64)) != 1 || int(resp["failed"].(float64)) != 1 {
		t.Fatalf("deleted/failed = %v/%v, want 1/1", resp["deleted"], resp["failed"])
	}
	errs := resp["errors"].([]interface{})
	if row := errs[0].(map[string]interface{}); row["id"] != "2" || !strings.Contains(fmt.Sprint(row["error"]), "not found") {
		t.Fatalf("error row = %v, want id 2 not found", row)
	}
	if noteCount(t, sqlDB, "globex") != 1 || noteCount(t, sqlDB, "acme") != 0 {
		t.Fatalf("rows after bulk: globex=%d acme=%d", noteCount(t, sqlDB, "globex"), noteCount(t, sqlDB, "acme"))
	}
}

func TestTenantScope_WritesCannotChangeTenant(t *testing.T) {
	_, sqlDB, srv := scopedPanel(t, operatorAuth())

	// Moving a row to another tenant.
	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/ScopedNote/1", map[string]any{"tenant_id": "globex"})
	if status != http.StatusBadRequest {
		t.Fatalf("PUT tenant_id=globex: status %d body=%s, want 400", status, mustJSON(resp))
	}
	// The Go field name is the same door.
	resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/ScopedNote/1", map[string]any{"TenantID": "globex"})
	if status != http.StatusBadRequest {
		t.Fatalf("PUT TenantID=globex: status %d body=%s, want 400", status, mustJSON(resp))
	}
	// Restating the own tenant is fine.
	resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/ScopedNote/1", map[string]any{"tenant_id": "acme", "title": "renamed"})
	if status != http.StatusOK {
		t.Fatalf("PUT tenant_id=acme: status %d body=%s", status, mustJSON(resp))
	}

	// Creating in another tenant.
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/models/ScopedNote", map[string]any{"tenant_id": "globex", "title": "smuggled"})
	if status != http.StatusBadRequest {
		t.Fatalf("POST tenant_id=globex: status %d body=%s, want 400", status, mustJSON(resp))
	}
	// Creating without a tenant stamps the request's.
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/models/ScopedNote", map[string]any{"title": "mine"})
	if status != http.StatusCreated || resp["tenant_id"] != "acme" {
		t.Fatalf("POST without tenant: status %d body=%s, want 201 in acme", status, mustJSON(resp))
	}
	if noteCount(t, sqlDB, "globex") != 1 || noteCount(t, sqlDB, "acme") != 2 {
		t.Fatalf("rows: globex=%d acme=%d", noteCount(t, sqlDB, "globex"), noteCount(t, sqlDB, "acme"))
	}
}

func TestTenantScope_ExportsImportsAndFixturesStayInTenant(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	store := panel.store.(*keyedStore)

	// Export: the body's tenant_id is overridden by the request's tenant.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/exports", map[string]any{
		"tenant_id": "globex", "format": "csv", "models": []string{"ScopedNote"},
	})
	if status != http.StatusOK {
		t.Fatalf("export status %d body=%s", status, mustJSON(resp))
	}
	csvBody := store.objects[fmt.Sprint(resp["storage_key"])]
	if !strings.Contains(csvBody, "Acme note") || strings.Contains(csvBody, "Globex note") {
		t.Fatalf("export body = %q, want only the acme row", csvBody)
	}

	// Dumpdata: same rule.
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/dumpdata", map[string]any{
		"tenant_id": "globex", "models": []string{"ScopedNote"},
	})
	if status != http.StatusOK {
		t.Fatalf("dumpdata status %d body=%s", status, mustJSON(resp))
	}
	fixture := store.objects[fmt.Sprint(resp["storage_key"])]
	if !strings.Contains(fixture, "Acme note") || strings.Contains(fixture, "Globex note") {
		t.Fatalf("fixture = %q, want only the acme row", fixture)
	}

	// Loaddata: a record naming another tenant fails, one naming none is
	// stamped with the request's tenant, whatever the body's tenant_id says.
	store.objects["_tmp/fixture.json"] = fixtureJSON(t,
		map[string]any{"model": "ScopedNote", "fields": map[string]any{"tenant_id": "globex", "title": "smuggled"}},
		map[string]any{"model": "ScopedNote", "fields": map[string]any{"title": "stamped"}},
	)
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{
		"key": "_tmp/fixture.json", "tenant_id": "globex",
	})
	if status != http.StatusOK {
		t.Fatalf("loaddata status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["failed"].(float64)) != 1 || int(resp["imported"].(float64)) != 1 {
		t.Fatalf("loaddata report = %s, want 1 failed / 1 imported", mustJSON(resp))
	}
	if noteCount(t, sqlDB, "globex") != 1 || noteCount(t, sqlDB, "acme") != 2 {
		t.Fatalf("rows after loaddata: globex=%d acme=%d", noteCount(t, sqlDB, "globex"), noteCount(t, sqlDB, "acme"))
	}

	// Import execute: same rule, through the CSV importer. Two files: the
	// validator refuses a blank required cell before the tenant is stamped,
	// so the stamped row comes from a file without the column.
	store.objects["_tmp/import_smuggled.csv"] = "tenant_id,title\nglobex,smuggled\n"
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/import/execute?key=_tmp/import_smuggled.csv", map[string]any{
		"model": "ScopedNote", "format": "csv", "tenant_id": "globex",
	})
	if status != http.StatusOK {
		t.Fatalf("import status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["failed"].(float64)) != 1 || int(resp["imported"].(float64)) != 0 {
		t.Fatalf("import report = %s, want the globex row refused", mustJSON(resp))
	}
	store.objects["_tmp/import_stamped.csv"] = "title\nstamped\n"
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/import/execute?key=_tmp/import_stamped.csv", map[string]any{
		"model": "ScopedNote", "format": "csv", "tenant_id": "globex",
	})
	if status != http.StatusOK {
		t.Fatalf("import status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["imported"].(float64)) != 1 {
		t.Fatalf("import report = %s, want the row imported", mustJSON(resp))
	}
	if noteCount(t, sqlDB, "globex") != 1 || noteCount(t, sqlDB, "acme") != 3 {
		t.Fatalf("rows after import: globex=%d acme=%d", noteCount(t, sqlDB, "globex"), noteCount(t, sqlDB, "acme"))
	}
}

func TestTenantScope_SingleTenantIsUnchanged(t *testing.T) {
	panel, _, srv := scopedPanel(t, operatorAuth())
	panel.config.MultiTenantEnabled = false

	for _, url := range []string{
		srv.URL + "/api/models/ScopedNote/2",
		srv.URL + "/api/models/ScopedNote?tenant=all",
		srv.URL + "/api/models/ScopedNote?tenant=globex",
	} {
		resp, status := doJSON(t, http.MethodGet, url, nil)
		if status != http.StatusOK {
			t.Errorf("%s: status %d body=%s, want 200 with multi-tenant off", url, status, mustJSON(resp))
		}
	}
	resp, _ := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote", nil)
	if items, _ := resp["items"].([]interface{}); len(items) != 2 {
		t.Fatalf("single-tenant list = %v, want every row", resp["items"])
	}
}

func TestTenantScope_ModelWithoutTenantColumnIsNotScoped(t *testing.T) {
	_, _, srv := scopedPanel(t, operatorAuth())
	created := createAdminUser(t, srv.URL, map[string]interface{}{"email": "u@example.com", "name": "U", "active": true})
	resp, status := doJSON(t, http.MethodGet, fmt.Sprintf("%s/api/models/AdminUser/%d", srv.URL, created.ID), nil)
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
}

func TestTenantScope_HelpersOnUnscopedRequest(t *testing.T) {
	panel, _, cleanup := setupPanelWithModels(t, operatorAuth())
	defer cleanup()
	mi, _ := panel.src.Get("ScopedNote")
	r := httptest.NewRequest(http.MethodGet, "/api/models/ScopedNote", nil)
	if scope := panel.requestTenantScope(r, mi); scope.Enforced() {
		t.Fatal("a request without tenant context must not be scoped")
	}
	if panel.enforcedTenantID(r) != "" {
		t.Fatal("a request without tenant context has no enforced tenant")
	}
	r = r.WithContext(context.WithValue(r.Context(), adminTenantCtxKey, &TenantContext{Enabled: true, AutoFilter: true, TenantID: "acme"}))
	scope := panel.requestTenantScope(r, mi)
	if !scope.Enforced() || scope.Column() != "tenant_id" || scope.Tenant != "acme" {
		t.Fatalf("scope = %+v", scope)
	}
	if !scope.contains(map[string]any{"tenant_id": "acme"}) || scope.contains(map[string]any{"tenant_id": "globex"}) || scope.contains(map[string]any{}) {
		t.Fatal("contains must compare the tenant column")
	}
	if got, ok, err := scope.payloadTenant(map[string]any{"TenantID": 7}); !ok || got != "7" || err != nil {
		t.Fatalf("payloadTenant by Go name = (%q, %v, %v)", got, ok, err)
	}
	if got, ok, err := scope.payloadTenant(map[string]any{"TENANT_ID": "globex"}); !ok || got != "globex" || err != nil {
		t.Fatalf("payloadTenant by upper-cased column = (%q, %v, %v)", got, ok, err)
	}
	if _, _, err := scope.payloadTenant(map[string]any{"tenant_id": "acme", "TenantID": "acme"}); err == nil {
		t.Fatal("payloadTenant must refuse the tenant under two spellings")
	}
	b, _ := json.Marshal(scope.Field)
	if !strings.Contains(string(b), `"tenant_id"`) {
		t.Fatalf("scope field = %s", b)
	}
}

// noteRow reads one scoped note's tenant and title.
func noteRow(t *testing.T, sqlDB *sql.DB, id int) (tenant, title string) {
	t.Helper()
	if err := sqlDB.QueryRow(`SELECT tenant_id, title FROM scoped_notes WHERE id = ?`, id).Scan(&tenant, &title); err != nil {
		t.Fatal(err)
	}
	return tenant, title
}

// getRaw performs a GET and returns the status and the raw body — for
// endpoints that answer a JSON array or a file, which doJSON cannot decode.
func getRaw(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestTenantScope_TenantKeySpellingsAreGuarded(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())

	// The Nucleus adapter resolves payload keys case-insensitively against
	// the column and the Go field name, so the guard must too: TENANT_ID
	// used to slip past the exact-match check and create the row in globex
	// (201) or move row 1 there (200).
	for _, key := range []string{"TENANT_ID", "Tenant_Id", "tenantid", "TenantID"} {
		resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/ScopedNote", map[string]any{key: "globex", "title": "smuggled"})
		if status != http.StatusBadRequest {
			t.Errorf("POST %s=globex: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
		resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/ScopedNote/1", map[string]any{key: "globex"})
		if status != http.StatusBadRequest {
			t.Errorf("PUT %s=globex: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
	}

	// The tenant under two spellings is ambiguous — the backend keeps the
	// one map order hands it last — so it is refused even when both name
	// the request's tenant.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/ScopedNote", map[string]any{"tenant_id": "acme", "TENANT_ID": "globex", "title": "ambiguous"})
	if status != http.StatusBadRequest {
		t.Errorf("POST two spellings: status %d body=%s, want 400", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/ScopedNote/1", map[string]any{"tenant_id": "acme", "TENANT_ID": "acme"})
	if status != http.StatusBadRequest {
		t.Errorf("PUT two spellings: status %d body=%s, want 400", status, mustJSON(resp))
	}

	// Loaddata: same door, same guard.
	store := panel.store.(*keyedStore)
	store.objects["_tmp/fixture_case.json"] = fixtureJSON(t,
		map[string]any{"model": "ScopedNote", "fields": map[string]any{"TENANT_ID": "globex", "title": "smuggled"}},
	)
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "_tmp/fixture_case.json"})
	if status != http.StatusOK || int(resp["failed"].(float64)) != 1 || int(resp["imported"].(float64)) != 0 {
		t.Fatalf("loaddata TENANT_ID: status %d report=%s, want the row failed", status, mustJSON(resp))
	}

	// Import execute: the validator refuses an unknown column spelling
	// before the importer runs, so the importer's own guard is exercised
	// directly.
	report, err := panel.ExecuteImport(context.Background(), ImportConfig{Model: "ScopedNote", TenantID: "acme", OnConflict: "skip"},
		[]map[string]interface{}{{"TENANT_ID": "globex", "title": "smuggled"}})
	if err != nil || report.Failed != 1 || report.Imported != 0 {
		t.Fatalf("ExecuteImport TENANT_ID: err=%v report=%+v, want the row failed", err, report)
	}

	if noteCount(t, sqlDB, "globex") != 1 || noteCount(t, sqlDB, "acme") != 1 {
		t.Fatalf("rows: globex=%d acme=%d, want 1/1 untouched", noteCount(t, sqlDB, "globex"), noteCount(t, sqlDB, "acme"))
	}
	if tenant, _ := noteRow(t, sqlDB, 1); tenant != "acme" {
		t.Fatalf("row 1 tenant = %q, want acme", tenant)
	}
}

func TestTenantScope_LoaddataCannotReachOtherTenantRow(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	store := panel.store.(*keyedStore)

	// pk 2 is the globex row. One record hides the tenant, the other
	// restates acme: on_conflict=update used to overwrite row 2 and move it
	// to acme (updated=2), and on_conflict=skip confirmed it existed.
	store.objects["_tmp/hijack.json"] = fixtureJSON(t,
		map[string]any{"model": "ScopedNote", "pk": 2, "fields": map[string]any{"title": "hijacked"}},
		map[string]any{"model": "ScopedNote", "pk": 2, "fields": map[string]any{"tenant_id": "acme", "title": "moved"}},
	)
	for _, mode := range []string{"update", "skip"} {
		resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "_tmp/hijack.json", "on_conflict": mode})
		if status != http.StatusOK {
			t.Fatalf("loaddata %s: status %d body=%s", mode, status, mustJSON(resp))
		}
		if int(resp["failed"].(float64)) != 2 || int(resp["updated"].(float64)) != 0 || int(resp["skipped"].(float64)) != 0 {
			t.Fatalf("loaddata %s report = %s, want both rows failed", mode, mustJSON(resp))
		}
		errs := resp["errors"].([]interface{})
		if msg := fmt.Sprint(errs[0].(map[string]interface{})["message"]); !strings.Contains(msg, "not found") {
			t.Fatalf("loaddata %s error = %q, want the row reported as not found", mode, msg)
		}
	}
	if tenant, title := noteRow(t, sqlDB, 2); tenant != "globex" || title != "Globex note" {
		t.Fatalf("globex row = %s/%s, want untouched", tenant, title)
	}

	// The own row still updates by pk.
	store.objects["_tmp/own.json"] = fixtureJSON(t,
		map[string]any{"model": "ScopedNote", "pk": 1, "fields": map[string]any{"title": "renamed"}},
	)
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "_tmp/own.json", "on_conflict": "update"})
	if status != http.StatusOK || int(resp["updated"].(float64)) != 1 {
		t.Fatalf("loaddata own row: status %d report=%s, want 1 updated", status, mustJSON(resp))
	}
	if tenant, title := noteRow(t, sqlDB, 1); tenant != "acme" || title != "renamed" {
		t.Fatalf("acme row = %s/%s, want acme/renamed", tenant, title)
	}
}

func TestTenantScope_ImportUpdateCannotReachOtherTenantRow(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	store := panel.store.(*keyedStore)

	// A JSON body naming the globex row by pk — under the Go name (which the
	// pk lookup matched: updated=1 and row 2 became acme's) and under the
	// column (which it missed, creating a fresh row instead).
	for _, key := range []string{"ID", "id"} {
		store.objects["_tmp/import_hijack.json"] = `[{"` + key + `":2,"title":"hijacked"}]`
		for _, mode := range []string{"update", "skip"} {
			resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/import/execute?key=_tmp/import_hijack.json", map[string]any{
				"model": "ScopedNote", "format": "json", "on_conflict": mode,
			})
			if status != http.StatusOK {
				t.Fatalf("import %s=2 %s: status %d body=%s", key, mode, status, mustJSON(resp))
			}
			if int(resp["failed"].(float64)) != 1 || int(resp["updated"].(float64)) != 0 || int(resp["skipped"].(float64)) != 0 || int(resp["imported"].(float64)) != 0 {
				t.Fatalf("import %s=2 %s report = %s, want the row failed", key, mode, mustJSON(resp))
			}
			errs := resp["errors"].([]interface{})
			if msg := fmt.Sprint(errs[0].(map[string]interface{})["message"]); !strings.Contains(msg, "not found") {
				t.Fatalf("import %s=2 %s error = %q, want not found", key, mode, msg)
			}
		}
	}
	if tenant, title := noteRow(t, sqlDB, 2); tenant != "globex" || title != "Globex note" {
		t.Fatalf("globex row = %s/%s, want untouched", tenant, title)
	}
	if noteCount(t, sqlDB, "acme") != 1 {
		t.Fatalf("acme rows = %d, want no fresh row from a refused pk", noteCount(t, sqlDB, "acme"))
	}

	// The own row updates by pk, under the column spelling too.
	store.objects["_tmp/import_own.json"] = `[{"id":1,"title":"renamed"}]`
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/import/execute?key=_tmp/import_own.json", map[string]any{
		"model": "ScopedNote", "format": "json", "on_conflict": "update",
	})
	if status != http.StatusOK || int(resp["updated"].(float64)) != 1 {
		t.Fatalf("import own row: status %d report=%s, want 1 updated", status, mustJSON(resp))
	}
	if tenant, title := noteRow(t, sqlDB, 1); tenant != "acme" || title != "renamed" {
		t.Fatalf("acme row = %s/%s, want acme/renamed", tenant, title)
	}

	// A unique index that is global: the lookup by index used to find the
	// globex row G-1 and update it with the stamped tenant, moving it to
	// acme. The lookup now stays in the tenant, so the row is not matched;
	// the create that follows hits the unique constraint and fails the row.
	mi, _ := panel.src.Get("ScopedCode")
	unique := false
	for _, idx := range mi.Indexes {
		if idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == "code" {
			unique = true
		}
	}
	if !unique {
		t.Fatalf("ScopedCode must expose a unique index on code for this test to mean anything, got %+v", mi.Indexes)
	}
	store.objects["_tmp/import_code.json"] = `[{"code":"G-1"}]`
	for _, mode := range []string{"update", "skip"} {
		resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/import/execute?key=_tmp/import_code.json", map[string]any{
			"model": "ScopedCode", "format": "json", "on_conflict": mode,
		})
		if status != http.StatusOK {
			t.Fatalf("import code %s: status %d body=%s", mode, status, mustJSON(resp))
		}
		if int(resp["updated"].(float64)) != 0 || int(resp["skipped"].(float64)) != 0 || int(resp["imported"].(float64)) != 0 {
			t.Fatalf("import code %s report = %s, want neither updated, skipped nor imported", mode, mustJSON(resp))
		}
	}
	var tenant string
	if err := sqlDB.QueryRow(`SELECT tenant_id FROM scoped_codes WHERE code = 'G-1'`).Scan(&tenant); err != nil || tenant != "globex" {
		t.Fatalf("G-1 tenant = %q err=%v, want globex untouched", tenant, err)
	}
}

func TestTenantScope_NoTenantResolvedFailsClosed(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	panel.config.TenantResolver = func(*http.Request) (string, bool) { return "", false }

	// A plain operator on a request the host resolved no tenant for is
	// refused: the panel used to drop the confinement (AutoFilter off) and
	// hand every tenant's rows to whoever reached the bare host.
	for _, path := range []string{
		"/api/models/ScopedNote",
		"/api/models/ScopedNote/2",
		"/api/models/ScopedNote?tenant=all",
		"/api/models/ScopedNote?tenant=globex",
		"/api/exports",
	} {
		resp, status := doJSON(t, http.MethodGet, srv.URL+path, nil)
		if status != http.StatusForbidden {
			t.Errorf("GET %s: status %d body=%s, want 403 without a resolved tenant", path, status, mustJSON(resp))
		}
	}
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/ScopedNote", map[string]any{"title": "orphan"})
	if status != http.StatusForbidden {
		t.Errorf("POST without a resolved tenant: status %d body=%s, want 403", status, mustJSON(resp))
	}
	if noteCount(t, sqlDB, "acme")+noteCount(t, sqlDB, "globex") != 2 {
		t.Fatal("no row may be written without a resolved tenant")
	}

	// A configured default restores the confinement.
	panel.config.MultiTenantDefault = "acme"
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("with a default tenant: status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 1 || items[0].(map[string]interface{})["tenant_id"] != "acme" {
		t.Fatalf("with a default tenant items = %v, want the acme row", resp["items"])
	}
	panel.config.MultiTenantDefault = ""

	// An operator who may switch tenants is not confined: no tenant means
	// every tenant, as with ?tenant=all.
	panel.config.Auth = superuserAuth()
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("superuser without a resolved tenant: status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 2 {
		t.Fatalf("superuser without a resolved tenant items = %v, want both rows", resp["items"])
	}
}

func TestTenantScope_ExportJobsAreScoped(t *testing.T) {
	panel, _, srv := scopedPanel(t, operatorAuth())
	store := panel.store.(*keyedStore)

	// The operator's own export records the tenant it was confined to.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/exports", map[string]any{"format": "csv", "models": []string{"ScopedNote"}})
	if status != http.StatusOK || resp["tenant"] != "acme" {
		t.Fatalf("export: status %d body=%s, want tenant acme recorded", status, mustJSON(resp))
	}
	own := fmt.Sprint(resp["storage_key"])

	// Exports a superuser produced for every tenant and for globex: before
	// this, export_data in any tenant listed and downloaded them.
	foreign := map[string]string{"_tmp/export_all.csv": "", "_tmp/export_globex.csv": "globex"}
	for key, tenant := range foreign {
		store.objects[key] = "id,tenant_id,title\n2,globex,Globex note\n"
		panel.exportMu.Lock()
		panel.exportResults[key] = ExportResult{ID: key, StorageKey: key, Status: "completed", Format: "csv", Tenant: tenant, CreatedAt: time.Now()}
		panel.exportMu.Unlock()
	}

	status, body := getRaw(t, srv.URL+"/api/exports")
	if status != http.StatusOK {
		t.Fatalf("list exports: status %d body=%s", status, body)
	}
	var jobs []map[string]interface{}
	if err := json.Unmarshal([]byte(body), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0]["id"] != own {
		t.Fatalf("scoped export list = %s, want only the acme export", body)
	}

	for key := range foreign {
		if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/exports/"+url.PathEscape(key), nil); status != http.StatusNotFound {
			t.Errorf("status of %s: %d body=%s, want 404", key, status, mustJSON(resp))
		}
		if status, body := getRaw(t, srv.URL+"/api/exports/download?key="+url.QueryEscape(key)); status != http.StatusNotFound {
			t.Errorf("download of %s: status %d body=%s, want 404", key, status, body)
		}
	}
	if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/exports/"+url.PathEscape(own), nil); status != http.StatusOK {
		t.Fatalf("status of the own export: %d body=%s", status, mustJSON(resp))
	}
	if status, body := getRaw(t, srv.URL+"/api/exports/download?key="+url.QueryEscape(own)); status != http.StatusOK || !strings.Contains(body, "Acme note") {
		t.Fatalf("download of the own export: status %d body=%q", status, body)
	}

	// Unscoped (a superuser on ?tenant=all) sees every job.
	panel.config.Auth = superuserAuth()
	status, body = getRaw(t, srv.URL+"/api/exports?tenant=all")
	if err := json.Unmarshal([]byte(body), &jobs); err != nil || status != http.StatusOK || len(jobs) != 3 {
		t.Fatalf("unscoped export list: status %d body=%s err=%v, want the three jobs", status, body, err)
	}
}

func TestTenantScope_OpenPostureSwitchDoesNotPanic(t *testing.T) {
	// The open posture (no auth provider) with the audit log on: a
	// ?tenant= switch passes the gate (nobody to gate) and is audited, and
	// recording the entry called Authenticate through the nil provider —
	// a panic that dropped the connection.
	panel, sqlDB, cleanup := setupPanelWithModels(t, nil)
	defer cleanup()
	panel.config.MultiTenantEnabled = true
	panel.config.MultiTenantAutoFilter = true
	panel.config.TenantResolver = func(*http.Request) (string, bool) { return "acme", true }
	for _, row := range [][2]string{{"acme", "Acme note"}, {"globex", "Globex note"}} {
		if _, err := sqlDB.Exec(`INSERT INTO scoped_notes (tenant_id, title, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=all", nil)
	if status != http.StatusOK {
		t.Fatalf("open posture ?tenant=all: status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 2 {
		t.Fatalf("open posture ?tenant=all items = %v, want both rows", resp["items"])
	}
	if auditActions(t, srv, "tenant.override") != 1 {
		t.Fatal("the switch must still be audited, with no operator to name")
	}
	// No tenant resolved is not a refusal either: there is no operator to gate.
	panel.config.TenantResolver = func(*http.Request) (string, bool) { return "", false }
	if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote", nil); status != http.StatusOK {
		t.Fatalf("open posture without a tenant: status %d body=%s", status, mustJSON(resp))
	}
}

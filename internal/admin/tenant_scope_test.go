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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/datasource"
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

// CamelNote is a tenant-scoped model whose tenant field marshals under a
// json tag that is neither the column nor the Go name. The Nucleus adapter
// keys its records by that tag and accepts it on input, so the guard has to
// know it too.
type CamelNote struct {
	model.BaseModel
	TenantID string `db:"column:tenant_id;required" json:"org" admin:"list,filter"`
	Title    string `db:"column:title;required" json:"title" admin:"list,search"`
}

func (CamelNote) TableName() string { return "camel_notes" }

// HiddenNote is a tenant-scoped model whose tenant field is excluded from
// JSON. Nucleus keeps it as the tenant column, but the records its adapter
// emits carry no tenant key under any spelling, so the scope cannot read
// the tenant off a record and confirms the row through the store instead.
type HiddenNote struct {
	model.BaseModel
	TenantID string `db:"column:tenant_id;required" json:"-" admin:"filter"`
	Title    string `db:"column:title;required" json:"title" admin:"list,search"`
}

func (HiddenNote) TableName() string { return "hidden_notes" }

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
CREATE TABLE IF NOT EXISTS camel_notes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	tenant_id TEXT NOT NULL,
	title TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS hidden_notes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	tenant_id TEXT NOT NULL,
	title TEXT NOT NULL
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
	for _, m := range []any{&AdminUser{}, &ScopedNote{}, &ScopedCode{}, &CamelNote{}, &HiddenNote{}, &Gadget{}} {
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
// one code per tenant (A-1 id 1, G-1 id 2), one camel note per tenant
// (acme id 1, globex id 2) and one hidden note per tenant (acme id 1,
// globex id 2).
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
	for _, row := range [][2]string{{"acme", "Acme camel"}, {"globex", "Globex camel"}} {
		if _, err := sqlDB.Exec(`INSERT INTO camel_notes (tenant_id, title, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range [][2]string{{"acme", "Acme hidden"}, {"globex", "Globex hidden"}} {
		if _, err := sqlDB.Exec(`INSERT INTO hidden_notes (tenant_id, title, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

func noteCount(t *testing.T, sqlDB *sql.DB, tenant string) int {
	t.Helper()
	return tenantRows(t, sqlDB, "scoped_notes", tenant)
}

// tenantRows counts the live rows of table that belong to tenant.
func tenantRows(t *testing.T, sqlDB *sql.DB, table, tenant string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE tenant_id = ? AND deleted_at IS NULL`, tenant).Scan(&n); err != nil {
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
	if tenant, present := scope.recordTenant(map[string]any{"tenant_id": "acme"}); !present || tenant != "acme" {
		t.Fatalf("recordTenant by column = (%q, %v), want (acme, true)", tenant, present)
	}
	if tenant, present := scope.recordTenant(map[string]any{"TenantID": "globex"}); !present || tenant != "globex" {
		t.Fatalf("recordTenant by Go name = (%q, %v), want (globex, true)", tenant, present)
	}
	if _, present := scope.recordTenant(map[string]any{"title": "no tenant key"}); present {
		t.Fatal("a record without the tenant field does not name a tenant")
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

// camelRow reads one camel note's tenant and title.
func camelRow(t *testing.T, sqlDB *sql.DB, id int) (tenant, title string) {
	t.Helper()
	if err := sqlDB.QueryRow(`SELECT tenant_id, title FROM camel_notes WHERE id = ? AND deleted_at IS NULL`, id).Scan(&tenant, &title); err != nil {
		t.Fatal(err)
	}
	return tenant, title
}

func TestTenantScope_JSONTaggedTenantFieldIsGuardedAndReadable(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	base := srv.URL + "/api/models/CamelNote"

	// The Nucleus adapter keys records by the field's json tag and accepts
	// that tag on input. The guard resolved only the column and the Go
	// name, so {"org": "globex"} created the row in globex (201, map order
	// deciding between the smuggled key and the stamp), and the tenant read
	// found no value in the operator's own records: every own row was 404.
	for _, key := range []string{"org", "ORG", "Org"} {
		resp, status := doJSON(t, http.MethodPost, base, map[string]any{key: "globex", "title": "smuggled"})
		if status != http.StatusBadRequest {
			t.Errorf("POST %s=globex: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
		resp, status = doJSON(t, http.MethodPut, base+"/1", map[string]any{key: "globex"})
		if status != http.StatusBadRequest {
			t.Errorf("PUT %s=globex: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
	}
	// Naming the field under the json key and the column at once is
	// ambiguous even when both agree.
	resp, status := doJSON(t, http.MethodPost, base, map[string]any{"org": "acme", "tenant_id": "acme", "title": "twice"})
	if status != http.StatusBadRequest {
		t.Errorf("POST org+tenant_id: status %d body=%s, want 400", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPut, base+"/1", map[string]any{"org": "acme", "TenantID": "acme"})
	if status != http.StatusBadRequest {
		t.Errorf("PUT org+TenantID: status %d body=%s, want 400", status, mustJSON(resp))
	}

	// Loaddata and import: same key, same guard.
	store := panel.store.(*keyedStore)
	store.objects["_tmp/fixture_json_tag.json"] = fixtureJSON(t,
		map[string]any{"model": "CamelNote", "fields": map[string]any{"org": "globex", "title": "smuggled"}},
	)
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "_tmp/fixture_json_tag.json"})
	if status != http.StatusOK || int(resp["failed"].(float64)) != 1 || int(resp["imported"].(float64)) != 0 {
		t.Fatalf("loaddata org=globex: status %d report=%s, want the row failed", status, mustJSON(resp))
	}
	report, err := panel.ExecuteImport(context.Background(), ImportConfig{Model: "CamelNote", TenantID: "acme", OnConflict: "skip"},
		[]map[string]interface{}{{"org": "globex", "title": "smuggled"}})
	if err != nil || report.Failed != 1 || report.Imported != 0 {
		t.Fatalf("ExecuteImport org=globex: err=%v report=%+v, want the row failed", err, report)
	}

	if g, a := tenantRows(t, sqlDB, "camel_notes", "globex"), tenantRows(t, sqlDB, "camel_notes", "acme"); g != 1 || a != 1 {
		t.Fatalf("rows: globex=%d acme=%d, want 1/1 untouched", g, a)
	}
	if tenant, _ := camelRow(t, sqlDB, 1); tenant != "acme" {
		t.Fatalf("row 1 tenant = %q, want acme", tenant)
	}

	// The operator's own row is reachable under the json key the records
	// carry; the other tenant's row stays not found.
	resp, status = doJSON(t, http.MethodGet, base+"/1", nil)
	if status != http.StatusOK || resp["org"] != "acme" {
		t.Fatalf("GET own row: status %d body=%s, want 200 with org=acme", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodGet, base+"/2", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET other tenant's row: status %d body=%s, want 404", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPut, base+"/1", map[string]any{"title": "renamed"})
	if status != http.StatusOK {
		t.Fatalf("PUT own row: status %d body=%s", status, mustJSON(resp))
	}
	if tenant, title := camelRow(t, sqlDB, 1); tenant != "acme" || title != "renamed" {
		t.Fatalf("row 1 = (%q, %q), want (acme, renamed)", tenant, title)
	}
	// Naming the own tenant under the json key is not a change.
	resp, status = doJSON(t, http.MethodPut, base+"/1", map[string]any{"org": "acme", "title": "kept"})
	if status != http.StatusOK {
		t.Fatalf("PUT org=acme: status %d body=%s", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPost, base, map[string]any{"org": "acme", "title": "mine"})
	if status != http.StatusCreated || resp["org"] != "acme" {
		t.Fatalf("POST org=acme: status %d body=%s, want 201 in acme", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPost, base, map[string]any{"title": "stamped"})
	if status != http.StatusCreated || resp["org"] != "acme" {
		t.Fatalf("POST without tenant: status %d body=%s, want 201 stamped acme", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodDelete, base+"/1", nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE own row: status %d body=%s", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodDelete, base+"/2", nil)
	if status != http.StatusNotFound {
		t.Fatalf("DELETE other tenant's row: status %d body=%s, want 404", status, mustJSON(resp))
	}
	if g, a := tenantRows(t, sqlDB, "camel_notes", "globex"), tenantRows(t, sqlDB, "camel_notes", "acme"); g != 1 || a != 2 {
		t.Fatalf("rows after: globex=%d acme=%d, want 1/2", g, a)
	}
}

func TestTenantScope_RBACTenantSwitchGrant(t *testing.T) {
	panel, _, srv := scopedPanel(t, operatorAuth())
	enforcer := func(policies ...[3]string) *authz.Enforcer {
		t.Helper()
		enf, err := authz.New(slog.Default())
		if err != nil {
			t.Fatalf("authz.New: %v", err)
		}
		for _, pol := range policies {
			if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
				t.Fatalf("AddPolicy %v: %v", pol, err)
			}
		}
		return enf
	}
	listAndAudit := [][3]string{{"admin", "admin:ScopedNote", "list"}, {"admin", "admin:*", "audit_view"}}

	// The operator's role may list the model but holds no tenant_switch:
	// the switch is refused and nothing is audited.
	panel.rbac = enforcer(listAndAudit...)
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=globex", nil)
	if status != http.StatusForbidden {
		t.Fatalf("without tenant_switch: status %d body=%s, want 403", status, mustJSON(resp))
	}
	if auditActions(t, srv, "tenant.override") != 0 {
		t.Fatal("a refused switch must not be audited")
	}

	// An explicit tenant_switch grant on admin:* opens the gate for the
	// role, and every switch is audited under the operator's name.
	panel.rbac = enforcer(append(listAndAudit, [3]string{"admin", "admin:*", tenantSwitchAction})...)
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=globex", nil)
	if status != http.StatusOK {
		t.Fatalf("with tenant_switch: status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 1 || items[0].(map[string]interface{})["tenant_id"] != "globex" {
		t.Fatalf("with tenant_switch items = %v, want the globex row", resp["items"])
	}
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=all", nil)
	if status != http.StatusOK {
		t.Fatalf("with tenant_switch ?tenant=all: status %d body=%s", status, mustJSON(resp))
	}
	if items, _ := resp["items"].([]interface{}); len(items) != 2 {
		t.Fatalf("with tenant_switch ?tenant=all items = %v, want both rows", resp["items"])
	}
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/audit?action=tenant.override", nil)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	entries, _ := resp["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("audit entries = %v, want one tenant.override per switch", resp["entries"])
	}
	for _, e := range entries {
		if entry := e.(map[string]interface{}); entry["username"] != "operator" {
			t.Errorf("override entry must name the operator, got %v", entry)
		}
	}

	// A wildcard-action grant on admin:* includes tenant_switch — the
	// casbin matcher accepts p.act == "*" — which the docs state.
	panel.rbac = enforcer([3]string{"admin", "admin:*", "*"})
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote?tenant=globex", nil)
	if status != http.StatusOK {
		t.Fatalf("wildcard grant: status %d body=%s, want 200", status, mustJSON(resp))
	}
}

// hiddenRow reads one hidden note's tenant and title.
func hiddenRow(t *testing.T, sqlDB *sql.DB, id int) (tenant, title string) {
	t.Helper()
	if err := sqlDB.QueryRow(`SELECT tenant_id, title FROM hidden_notes WHERE id = ? AND deleted_at IS NULL`, id).Scan(&tenant, &title); err != nil {
		t.Fatal(err)
	}
	return tenant, title
}

func TestTenantScope_HiddenJSONTenantFieldOwnRowsReachable(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	base := srv.URL + "/api/models/HiddenNote"

	// The records carry no tenant key at all. The tenant read took that
	// for a row of another tenant, so every own row answered 404 on get,
	// update, delete and bulk delete, and "not found in tenant" on loaddata
	// and import, while the list kept showing it.
	resp, status := doJSON(t, http.MethodGet, base, nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(resp))
	}
	items, _ := resp["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("list = %s, want the acme row only", mustJSON(resp))
	}
	own, _ := items[0].(map[string]interface{})
	for _, key := range []string{"tenant_id", "TenantID"} {
		if _, has := own[key]; has {
			t.Fatalf("a HiddenNote record must not carry %s for this test to mean anything: %s", key, mustJSON(own))
		}
	}
	if id, _ := canonicalID(own["id"]); id != "1" {
		t.Fatalf("list row id = %v, want 1", own["id"])
	}

	resp, status = doJSON(t, http.MethodGet, base+"/1", nil)
	if status != http.StatusOK || resp["title"] != "Acme hidden" {
		t.Fatalf("GET own row: status %d body=%s, want 200", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodGet, base+"/2", nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET other tenant's row: status %d body=%s, want 404", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPut, base+"/1", map[string]any{"title": "renamed"})
	if status != http.StatusOK {
		t.Fatalf("PUT own row: status %d body=%s, want 200", status, mustJSON(resp))
	}
	if tenant, title := hiddenRow(t, sqlDB, 1); tenant != "acme" || title != "renamed" {
		t.Fatalf("row 1 = (%q, %q), want (acme, renamed)", tenant, title)
	}
	resp, status = doJSON(t, http.MethodPut, base+"/2", map[string]any{"title": "hijacked"})
	if status != http.StatusNotFound {
		t.Fatalf("PUT other tenant's row: status %d body=%s, want 404", status, mustJSON(resp))
	}
	// The write guard still knows the hidden field by column and Go name.
	for _, key := range []string{"tenant_id", "TenantID"} {
		resp, status = doJSON(t, http.MethodPut, base+"/1", map[string]any{key: "globex"})
		if status != http.StatusBadRequest {
			t.Errorf("PUT %s=globex: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
		resp, status = doJSON(t, http.MethodPost, base, map[string]any{key: "globex", "title": "smuggled"})
		if status != http.StatusBadRequest {
			t.Errorf("POST %s=globex: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
	}
	// A create without a tenant is stamped, and the stamped row is the
	// operator's: reachable, and deletable, by id.
	resp, status = doJSON(t, http.MethodPost, base, map[string]any{"title": "stamped"})
	if status != http.StatusCreated {
		t.Fatalf("POST without tenant: status %d body=%s, want 201", status, mustJSON(resp))
	}
	createdID, _ := canonicalID(resp["id"])
	if createdID == "" || tenantRows(t, sqlDB, "hidden_notes", "acme") != 2 {
		t.Fatalf("POST without tenant: id=%v acme rows=%d, want a second acme row", resp["id"], tenantRows(t, sqlDB, "hidden_notes", "acme"))
	}
	resp, status = doJSON(t, http.MethodGet, base+"/"+createdID, nil)
	if status != http.StatusOK || resp["title"] != "stamped" {
		t.Fatalf("GET stamped row: status %d body=%s, want 200", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodDelete, base+"/"+createdID, nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE stamped row: status %d body=%s, want 200", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodDelete, base+"/2", nil)
	if status != http.StatusNotFound {
		t.Fatalf("DELETE other tenant's row: status %d body=%s, want 404", status, mustJSON(resp))
	}

	// Loaddata: the own pk updates or is skipped, the other tenant's pk
	// fails as not found.
	store := panel.store.(*keyedStore)
	store.objects["_tmp/hidden.json"] = fixtureJSON(t,
		map[string]any{"model": "HiddenNote", "pk": 1, "fields": map[string]any{"title": "via fixture"}},
		map[string]any{"model": "HiddenNote", "pk": 2, "fields": map[string]any{"title": "hijacked"}},
	)
	for _, mode := range []string{"update", "skip"} {
		resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "_tmp/hidden.json", "on_conflict": mode})
		if status != http.StatusOK {
			t.Fatalf("loaddata %s: status %d body=%s", mode, status, mustJSON(resp))
		}
		wantUpdated, wantSkipped := 1, 0
		if mode == "skip" {
			wantUpdated, wantSkipped = 0, 1
		}
		if int(resp["failed"].(float64)) != 1 || int(resp["updated"].(float64)) != wantUpdated || int(resp["skipped"].(float64)) != wantSkipped {
			t.Fatalf("loaddata %s report = %s, want own pk %s and the other pk failed", mode, mustJSON(resp), mode+"d")
		}
		errs := resp["errors"].([]interface{})
		if msg := fmt.Sprint(errs[0].(map[string]interface{})["message"]); !strings.Contains(msg, "pk=2") || !strings.Contains(msg, "not found") {
			t.Fatalf("loaddata %s error = %q, want pk 2 reported as not found", mode, msg)
		}
	}
	if tenant, title := hiddenRow(t, sqlDB, 1); tenant != "acme" || title != "via fixture" {
		t.Fatalf("row 1 = (%q, %q), want (acme, via fixture)", tenant, title)
	}

	// Import: same rule under on_conflict=update.
	store.objects["_tmp/hidden_import.json"] = `[{"id":1,"title":"via import"},{"id":2,"title":"hijacked"}]`
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/import/execute?key=_tmp/hidden_import.json", map[string]any{
		"model": "HiddenNote", "format": "json", "on_conflict": "update",
	})
	if status != http.StatusOK {
		t.Fatalf("import: status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["updated"].(float64)) != 1 || int(resp["failed"].(float64)) != 1 || int(resp["imported"].(float64)) != 0 {
		t.Fatalf("import report = %s, want own row updated and the other failed", mustJSON(resp))
	}
	if tenant, title := hiddenRow(t, sqlDB, 1); tenant != "acme" || title != "via import" {
		t.Fatalf("row 1 = (%q, %q), want (acme, via import)", tenant, title)
	}
	if tenant, title := hiddenRow(t, sqlDB, 2); tenant != "globex" || title != "Globex hidden" {
		t.Fatalf("row 2 = (%q, %q), want untouched", tenant, title)
	}

	// Bulk delete: the own row goes, the other tenant's is a per-id failure.
	resp, status = doJSON(t, http.MethodPost, base+"/bulk", map[string]any{"action": "delete", "ids": []string{"1", "2"}})
	if status != http.StatusOK {
		t.Fatalf("bulk: status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["deleted"].(float64)) != 1 || int(resp["failed"].(float64)) != 1 {
		t.Fatalf("bulk report = %s, want 1 deleted / 1 failed", mustJSON(resp))
	}
	errs := resp["errors"].([]interface{})
	if row := errs[0].(map[string]interface{}); row["id"] != "2" || !strings.Contains(fmt.Sprint(row["error"]), "not found") {
		t.Fatalf("bulk error row = %v, want id 2 not found", row)
	}
	if g, a := tenantRows(t, sqlDB, "hidden_notes", "globex"), tenantRows(t, sqlDB, "hidden_notes", "acme"); g != 1 || a != 0 {
		t.Fatalf("rows after: globex=%d acme=%d, want 1/0", g, a)
	}
}

// firstRowStore lists the scope's tenant rows while ignoring every other
// filter — what the Nucleus backend does with a filter column it cannot
// resolve (it is dropped, not refused). Only List is called.
type firstRowStore struct {
	datasource.RecordStore
	rows []datasource.Record
	err  error
}

func (s firstRowStore) List(_ context.Context, q datasource.Query) (datasource.Page, error) {
	if s.err != nil {
		return datasource.Page{}, s.err
	}
	for _, rec := range s.rows {
		if rec["tenant_id"] == q.Filters["tenant_id"] {
			return datasource.Page{Items: []datasource.Record{rec}, Total: 1, Page: 1, PageSize: 1}, nil
		}
	}
	return datasource.Page{Page: 1, PageSize: 1}, nil
}

func TestTenantScope_OwnsRequiresTheRowItAskedFor(t *testing.T) {
	ctx := context.Background()
	mi := datasource.ModelInfo{Name: "Hidden", PrimaryKey: "ID", Fields: []datasource.FieldInfo{
		{Column: "id", Name: "ID", IsPK: true},
		{Column: "tenant_id", Name: "TenantID"},
	}}
	scope := newTenantScope(mi.Fields[1], "acme", "")
	st := firstRowStore{rows: []datasource.Record{
		{"id": float64(1), "tenant_id": "acme"},
		{"id": float64(2), "tenant_id": "globex"},
	}}

	// A record that carries the tenant is compared in place.
	for _, tc := range []struct {
		id   string
		rec  datasource.Record
		want bool
	}{
		{"1", datasource.Record{"tenant_id": "acme"}, true},
		{"2", datasource.Record{"tenant_id": "globex"}, false},
		{"1", datasource.Record{"tenant_id": nil}, false},
		// One that carries none is confirmed through the store, which
		// must answer the row asked about — not the tenant's first row.
		{"1", datasource.Record{"title": "hidden"}, true},
		{"2", datasource.Record{"title": "hidden"}, false},
		{"zz", datasource.Record{}, false},
	} {
		got, err := scope.owns(ctx, st, mi, tc.id, tc.rec)
		if err != nil || got != tc.want {
			t.Errorf("owns(%s, %v) = (%v, %v), want %v", tc.id, tc.rec, got, err, tc.want)
		}
	}
	// A store failure is an error, not a verdict.
	if _, err := scope.owns(ctx, firstRowStore{err: fmt.Errorf("boom")}, mi, "1", datasource.Record{}); err == nil {
		t.Fatal("a failing lookup must surface its error")
	}
	// A model whose primary key the scope cannot name never confirms a
	// row through the store.
	if got, err := scope.owns(ctx, st, datasource.ModelInfo{Name: "NoPK", Fields: mi.Fields[1:]}, "1", datasource.Record{}); err != nil || got {
		t.Fatalf("owns without a primary key = (%v, %v), want false", got, err)
	}
}

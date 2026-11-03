// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

// Regression tests for OR-80 of the 2026-10-23 re-audit: the operator was
// not bound to a tenant. The panel confined each request to the tenant the
// host resolved for it, and with Nucleus' header resolver that is the
// tenant the client names — so a plain operator of one tenant, without
// tenant_switch, was refused ?tenant=<other> but listed, exported, read by
// id and CREATED rows in the other tenant by sending its id in the header.
//
// Now an operator account can carry a tenant (nucleus_admin_users.tenant_id)
// and an operator who may not switch tenants is confined to it whatever the
// request resolves; a request that resolves another tenant is refused the
// way ?tenant= naming it is. An operator with no tenant keeps the v1
// behaviour — the resolver decides — with a WARN, until v2.0.0 refuses them
// (DEP-2026-003).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// headerTenant is Nucleus' header resolver as the panel sees it: the tenant
// is whatever X-Tenant-ID the request carries.
func headerTenant(r *http.Request) (string, bool) {
	tenant := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	return tenant, tenant != ""
}

// lockedBuffer is a log sink the request goroutines write while the test
// reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// bindingEnv is the multi-tenant panel of tenant_scope_test.go (one acme
// note, id 1, and one globex note, id 2) behind a header resolver, whose
// auth provider owns an operator table — the shape of the default
// provider — with the acting operator seeded as a superuser.
type bindingEnv struct {
	panel *Panel
	sqlDB *sql.DB
	srv   *httptest.Server
	auth  *testAdminAuth
	logs  *lockedBuffer
}

func newBindingEnv(t *testing.T) *bindingEnv {
	t.Helper()
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", Role: "admin", IsSuperuser: true}}
	panel, sqlDB, srv := scopedPanel(t, provider)
	panel.config.TenantResolver = headerTenant
	logs := &lockedBuffer{}
	panel.logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	if err := EnsureBootstrapAdminUsersSchema(context.Background(), sqlDB, "sqlite"); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	seedOperatorRow(t, sqlDB, "actor", "actor", "actor@example.com", true)
	provider.operators = &operatorStore{db: sqlDB, table: defaultAdminUsersTable, system: "sqlite"}
	return &bindingEnv{panel: panel, sqlDB: sqlDB, srv: srv, auth: provider, logs: logs}
}

// createOperator creates a non-superuser operator through the panel's own
// operator API, as the acting superuser, and returns their id.
func (e *bindingEnv) createOperator(t *testing.T, username, tenant string) string {
	t.Helper()
	payload := map[string]any{
		"username": username, "email": username + "@example.com",
		"password": "a-long-enough-password",
	}
	if tenant != "" {
		payload["tenant_id"] = tenant
	}
	resp, status := e.do(t, "", http.MethodPost, "/api/admin-users", payload)
	if status != http.StatusCreated {
		t.Fatalf("create operator %q: status %d body=%s", username, status, mustJSON(resp))
	}
	if got := resp["tenant_id"]; got != tenant {
		t.Fatalf("created operator tenant_id = %v, want %q", got, tenant)
	}
	return resp["id"].(string)
}

// actAs makes every following request come from the operator with this
// id, as the provider would after their sign-in.
func (e *bindingEnv) actAs(id, username string, superuser bool) {
	e.auth.user = &auth.User{ID: id, Username: username, Role: "admin", IsSuperuser: superuser}
}

// do sends one JSON request, with X-Tenant-ID when tenant is set, and
// decodes a JSON answer; a body that is not JSON (the CSV export) comes
// back under "raw".
func (e *bindingEnv) do(t *testing.T, tenant, method, path string, payload any) (map[string]any, int) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenant != "" {
		req.Header.Set("X-Tenant-ID", tenant)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		out = map[string]any{"raw": string(raw)}
	}
	return out, res.StatusCode
}

func itemTenants(resp map[string]any) []string {
	items, _ := resp["items"].([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		row, _ := item.(map[string]any)
		out = append(out, row["tenant_id"].(string))
	}
	return out
}

// TestTenantBinding_BoundOperatorCannotReachAnotherTenantThroughTheResolver
// is OR-80: a non-superuser operator bound to acme, without tenant_switch,
// asks for globex through the header the resolver reads. Every read, the
// export and the write are refused, as ?tenant=globex is; nothing of globex
// leaves and nothing reaches it. Without the header, or with their own
// tenant in it, they work in acme.
func TestTenantBinding_BoundOperatorCannotReachAnotherTenantThroughTheResolver(t *testing.T) {
	e := newBindingEnv(t)
	id := e.createOperator(t, "ana", "acme")
	e.actAs(id, "ana", false)

	refused := []struct{ method, path string }{
		{http.MethodGet, "/api/models/ScopedNote"},
		{http.MethodGet, "/api/models/ScopedNote/2"},
		{http.MethodGet, "/api/models/ScopedNote/1"},
		{http.MethodGet, "/api/models/ScopedNote/export"},
		{http.MethodGet, "/api/exports"},
	}
	for _, rq := range refused {
		resp, status := e.do(t, "globex", rq.method, rq.path, nil)
		if status != http.StatusForbidden {
			t.Errorf("%s %s with X-Tenant-ID: globex: status %d, want 403 (body %s)", rq.method, rq.path, status, mustJSON(resp))
		}
		if strings.Contains(mustJSON(resp), "Globex note") {
			t.Errorf("%s %s with X-Tenant-ID: globex disclosed a globex row: %s", rq.method, rq.path, mustJSON(resp))
		}
	}
	resp, status := e.do(t, "globex", http.MethodPost, "/api/exports", map[string]any{"format": "csv", "models": []string{"ScopedNote"}})
	if status != http.StatusForbidden {
		t.Errorf("POST /api/exports with X-Tenant-ID: globex: status %d, want 403 (body %s)", status, mustJSON(resp))
	}

	// The write the adversarial review found: 201 with tenant_id globex.
	resp, status = e.do(t, "globex", http.MethodPost, "/api/models/ScopedNote", map[string]any{"title": "smuggled"})
	if status != http.StatusForbidden {
		t.Errorf("POST with X-Tenant-ID: globex: status %d, want 403 (body %s)", status, mustJSON(resp))
	}
	resp, status = e.do(t, "globex", http.MethodPut, "/api/models/ScopedNote/2", map[string]any{"title": "overwritten"})
	if status != http.StatusForbidden {
		t.Errorf("PUT globex row with X-Tenant-ID: globex: status %d, want 403 (body %s)", status, mustJSON(resp))
	}
	if n := noteCount(t, e.sqlDB, "globex"); n != 1 {
		t.Fatalf("globex rows after the refused writes = %d, want the 1 seeded", n)
	}
	var title string
	if err := e.sqlDB.QueryRow(`SELECT title FROM scoped_notes WHERE id = 2`).Scan(&title); err != nil || title != "Globex note" {
		t.Fatalf("globex row 2 title = %q (%v), want it untouched", title, err)
	}

	// ?tenant= was already refused; it still is, with or without a header.
	for _, header := range []string{"", "acme", "globex"} {
		if _, status := e.do(t, header, http.MethodGet, "/api/models/ScopedNote?tenant=globex", nil); status != http.StatusForbidden {
			t.Errorf("?tenant=globex (X-Tenant-ID %q): status %d, want 403", header, status)
		}
	}

	// Inside their own tenant the operator works as before — and on a
	// request that resolves no tenant at all, the account's tenant is the
	// one applied (it used to be a 403 for want of a tenant).
	for _, header := range []string{"acme", ""} {
		resp, status := e.do(t, header, http.MethodGet, "/api/models/ScopedNote", nil)
		if status != http.StatusOK {
			t.Fatalf("list with X-Tenant-ID %q: status %d body=%s", header, status, mustJSON(resp))
		}
		if got := itemTenants(resp); len(got) != 1 || got[0] != "acme" {
			t.Fatalf("list with X-Tenant-ID %q: tenants %v, want only acme", header, got)
		}
		if _, status := e.do(t, header, http.MethodGet, "/api/models/ScopedNote/2", nil); status != http.StatusNotFound {
			t.Errorf("globex row by id with X-Tenant-ID %q: status %d, want 404", header, status)
		}
		resp, status = e.do(t, header, http.MethodGet, "/api/models/ScopedNote/export", nil)
		if status != http.StatusOK {
			t.Fatalf("CSV export with X-Tenant-ID %q: status %d body=%s", header, status, mustJSON(resp))
		}
		if csv := resp["raw"].(string); !strings.Contains(csv, "Acme note") || strings.Contains(csv, "Globex note") {
			t.Fatalf("CSV export with X-Tenant-ID %q = %q, want only the acme row", header, csv)
		}
	}
	resp, status = e.do(t, "", http.MethodPost, "/api/models/ScopedNote", map[string]any{"title": "stamped"})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create without a header: status %d body=%s", status, mustJSON(resp))
	}
	if n := noteCount(t, e.sqlDB, "acme"); n != 2 {
		t.Fatalf("acme rows after the create = %d, want 2: the account's tenant is stamped", n)
	}
	if n := noteCount(t, e.sqlDB, "globex"); n != 1 {
		t.Fatalf("globex rows after the create = %d, want 1", n)
	}

	// A refusal is not a switch: nothing to audit as tenant.override.
	e.actAs("actor", "actor", true)
	if n := auditActions(t, e.srv, "tenant.override"); n != 0 {
		t.Fatalf("tenant.override entries = %d, want 0", n)
	}
	if strings.Contains(e.logs.String(), "DEP-2026-003") {
		t.Fatalf("a bound operator produced the unbound-operator WARN:\n%s", e.logs.String())
	}
}

// TestTenantBinding_SwitchersAreNotConfinedByTheirAccountTenant: the
// binding is for operators who may not switch tenants. A superuser or a
// tenant_switch holder keeps the existing rules — the resolver's tenant,
// ?tenant= gated and audited — whatever tenant their account names.
func TestTenantBinding_SwitchersAreNotConfinedByTheirAccountTenant(t *testing.T) {
	e := newBindingEnv(t)
	if _, err := e.sqlDB.Exec(`UPDATE nucleus_admin_users SET tenant_id = 'acme' WHERE id = 'actor'`); err != nil {
		t.Fatal(err)
	}
	resp, status := e.do(t, "globex", http.MethodGet, "/api/models/ScopedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("superuser bound to acme, X-Tenant-ID: globex: status %d body=%s", status, mustJSON(resp))
	}
	if got := itemTenants(resp); len(got) != 1 || got[0] != "globex" {
		t.Fatalf("superuser bound to acme, X-Tenant-ID: globex: tenants %v, want globex", got)
	}

	id := e.createOperator(t, "switcher", "acme")
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, act := range []string{"list", "audit_view"} {
		if err := enf.AddPolicy("admin", "admin:*", act); err != nil {
			t.Fatal(err)
		}
	}
	if err := enf.AddPolicy("admin", "admin:*", tenantSwitchAction); err != nil {
		t.Fatal(err)
	}
	e.panel.rbac = enf
	e.actAs(id, "switcher", false)
	resp, status = e.do(t, "globex", http.MethodGet, "/api/models/ScopedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("tenant_switch holder bound to acme, X-Tenant-ID: globex: status %d body=%s", status, mustJSON(resp))
	}
	if got := itemTenants(resp); len(got) != 1 || got[0] != "globex" {
		t.Fatalf("tenant_switch holder, X-Tenant-ID: globex: tenants %v, want globex", got)
	}
	resp, status = e.do(t, "globex", http.MethodGet, "/api/models/ScopedNote?tenant=all", nil)
	if status != http.StatusOK || len(itemTenants(resp)) != 2 {
		t.Fatalf("tenant_switch holder ?tenant=all: status %d tenants %v, want both", status, itemTenants(resp))
	}
	if n := auditActions(t, e.srv, "tenant.override"); n != 1 {
		t.Fatalf("tenant.override entries = %d, want the one switch", n)
	}
}

// TestTenantBinding_UnboundOperatorFollowsTheResolverAndWarns pins the v1
// behaviour that DEP-2026-003 changes at v2.0.0: an operator with no tenant,
// who is not a superuser and holds no tenant_switch, is confined to the
// tenant each request resolves — with a header resolver, the one the client
// names — and the panel says so in a WARN, once per operator per process.
// At v2.0.0 this operator is refused instead; this test changes with the
// default.
func TestTenantBinding_UnboundOperatorFollowsTheResolverAndWarns(t *testing.T) {
	e := newBindingEnv(t)
	id := e.createOperator(t, "unbound", "")
	e.actAs(id, "unbound", false)

	for _, tenant := range []string{"acme", "globex"} {
		resp, status := e.do(t, tenant, http.MethodGet, "/api/models/ScopedNote", nil)
		if status != http.StatusOK {
			t.Fatalf("unbound operator, X-Tenant-ID: %s: status %d body=%s", tenant, status, mustJSON(resp))
		}
		if got := itemTenants(resp); len(got) != 1 || got[0] != tenant {
			t.Fatalf("unbound operator, X-Tenant-ID: %s: tenants %v, want %s (v1: the resolver decides)", tenant, got, tenant)
		}
	}
	// With nothing resolved and no default, the existing refusal stands.
	if _, status := e.do(t, "", http.MethodGet, "/api/models/ScopedNote", nil); status != http.StatusForbidden {
		t.Fatalf("unbound operator, no tenant resolved: status %d, want 403", status)
	}

	logs := e.logs.String()
	if n := strings.Count(logs, "DEP-2026-003"); n != 1 {
		t.Fatalf("WARN naming DEP-2026-003 logged %d times over three requests, want once per operator:\n%s", n, logs)
	}
	for _, want := range []string{"level=WARN", "operator=unbound", "operator_id=" + id, "/admin/api/admin-users/{id}", "tenant_id", tenantSwitchAction} {
		if !strings.Contains(logs, want) {
			t.Errorf("the WARN does not carry %q:\n%s", want, logs)
		}
	}

	// A superuser with no tenant is not affected at v2.0.0 and is not
	// warned.
	e.logs = &lockedBuffer{}
	e.panel.logger = slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	e.actAs("actor", "actor", true)
	if _, status := e.do(t, "globex", http.MethodGet, "/api/models/ScopedNote", nil); status != http.StatusOK {
		t.Fatalf("superuser: status %d, want 200", status)
	}
	if strings.Contains(e.logs.String(), "DEP-2026-003") {
		t.Fatalf("a superuser with no tenant was warned:\n%s", e.logs.String())
	}

	// Single-tenant applications have nothing to bind and are not warned.
	e.panel.config.MultiTenantEnabled = false
	e.actAs(id, "unbound", false)
	e.panel.unboundOperators.Delete(id)
	if _, status := e.do(t, "", http.MethodGet, "/api/models/ScopedNote", nil); status != http.StatusOK {
		t.Fatalf("single-tenant: status %d, want 200", status)
	}
	if strings.Contains(e.logs.String(), "DEP-2026-003") {
		t.Fatalf("an operator of a single-tenant application was warned:\n%s", e.logs.String())
	}
}

// An operator deleted between authentication and the tenant read is refused,
// not let through unbound.
func TestTenantBinding_OperatorMissingFromTheStoreIsRefused(t *testing.T) {
	e := newBindingEnv(t)
	e.actAs("ghost", "ghost", false)
	if _, status := e.do(t, "acme", http.MethodGet, "/api/models/ScopedNote", nil); status != http.StatusForbidden {
		t.Fatalf("operator absent from the store: status %d, want 403", status)
	}
}

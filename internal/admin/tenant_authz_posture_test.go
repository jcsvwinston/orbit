package admin

// AO-4: tenant-flow coverage in the WITH-AUTH posture. Before the fix,
// the authenticated mounting branch did not apply
// tenantContextMiddleware to /api/* routes, so under auth — the
// production posture — the ?tenant= scope silently resolved nothing and
// auto-filtering never happened. This test drives the tenant filter
// end-to-end through the authenticated stack (setupPanelForTest is
// with-auth by default).

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

func TestPanel_ListRecords_TenantAutoFilter_UnderAuth(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// Seed both tenants before the scope is on: a scoped request can no
	// longer create a row in another tenant.
	createAdminUser(t, srv.URL, map[string]interface{}{"email": "acme", "name": "Tenant A", "active": true})
	createAdminUser(t, srv.URL, map[string]interface{}{"email": "globex", "name": "Tenant B", "active": true})

	// AdminUser has no dedicated tenant column; the override knob lets
	// the test reuse it by declaring "email" as the tenant field. The host
	// resolves the tenant (PanelConfig.TenantResolver); a plain operator
	// cannot pick one with ?tenant=.
	panel.config.MultiTenantEnabled = true
	panel.config.MultiTenantAutoFilter = true
	panel.config.MultiTenantField = "email"
	panel.config.TenantResolver = func(*http.Request) (string, bool) { return "acme", true }

	// Scoped to the resolved tenant: only its row comes back.
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser", nil)
	if status != http.StatusOK {
		t.Fatalf("tenant-scoped list status=%d body=%s", status, mustJSON(resp))
	}
	items, ok := resp["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected exactly the acme row under the resolved tenant, got %#v", resp["items"])
	}
	row, _ := items[0].(map[string]interface{})
	if row["email"] != "acme" {
		t.Fatalf("expected the acme row, got %#v", row)
	}

	// ?tenant=all is a way out of the confinement, so it is gated: the test
	// operator is not a superuser and has no tenant_switch grant, so the
	// switch is refused rather than showing every tenant's rows (OR-23; the
	// superuser side lives in tenant_scope_test.go).
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser?tenant=all", nil)
	if status != http.StatusForbidden {
		t.Fatalf("tenant=all list status=%d body=%s, want 403 for a non-superuser", status, mustJSON(resp))
	}
}

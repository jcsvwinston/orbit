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

	// AdminUser has no dedicated tenant column; the override knob lets
	// the test reuse it by declaring "email" as the tenant field.
	panel.config.MultiTenantEnabled = true
	panel.config.MultiTenantAutoFilter = true
	panel.config.MultiTenantField = "email"

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	createAdminUser(t, srv.URL, map[string]interface{}{"email": "acme", "name": "Tenant A", "active": true})
	createAdminUser(t, srv.URL, map[string]interface{}{"email": "globex", "name": "Tenant B", "active": true})

	// Scoped to one tenant: only its row comes back.
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser?tenant=acme", nil)
	if status != http.StatusOK {
		t.Fatalf("tenant-scoped list status=%d body=%s", status, mustJSON(resp))
	}
	items, ok := resp["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected exactly the acme row under ?tenant=acme, got %#v", resp["items"])
	}
	row, _ := items[0].(map[string]interface{})
	if row["email"] != "acme" {
		t.Fatalf("expected the acme row, got %#v", row)
	}

	// tenant=all disables auto-filtering: both rows visible.
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser?tenant=all", nil)
	if status != http.StatusOK {
		t.Fatalf("tenant=all list status=%d body=%s", status, mustJSON(resp))
	}
	if items, ok := resp["items"].([]interface{}); !ok || len(items) != 2 {
		t.Fatalf("expected both rows under ?tenant=all, got %#v", resp["items"])
	}
}

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// The runtime field-metadata editor left at 2.0 with the Nucleus adapter
// (DEP-2026-002): PUT /api/models/{name}/schema/fields is not a route any
// more. It answers the API's own not-found — 404 and JSON, not the 501 a
// panel without a schema registry used to answer and not the single-page
// fallback — and the schema offers nobody update_schema, the verb no
// handler asks since 2.0.
func TestUpdateFieldMeta_RouteIsGone(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()
	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/AdminUser/schema/fields",
		map[string]any{"fields": map[string]any{"email": map[string]any{"label": "E-mail"}}})
	if status != http.StatusNotFound {
		t.Fatalf("status %d body=%s, want 404: the route left at 2.0", status, mustJSON(resp))
	}
	if msg := errorMessage(resp); !strings.Contains(msg, "no admin API endpoint") {
		t.Fatalf("body = %s, want the API's not-found naming the path", mustJSON(resp))
	}

	// The route is gone for every method, not just PUT.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if _, status := doJSON(t, method, srv.URL+"/api/models/AdminUser/schema/fields", nil); status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", method, status)
		}
	}

	// And the screen is not offered the dialog that would ask: the verb is
	// neither asked nor answered.
	schema, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema: status %d", status)
	}
	perms, _ := schema["permissions"].(map[string]interface{})
	if _, has := perms["update_schema"]; has {
		t.Fatalf("the schema still answers update_schema on a panel with no editor: %v", perms)
	}
}

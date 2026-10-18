package admin

// Regression tests for OR-43 of the 2026-09 maturity audit: a model with no
// searchable field answered ?search= with 200 and every row — the backend
// dropped the text — which reads as "no match" while showing everything.
// The panel now answers 400 naming the model and how to enable search;
// the check reads the live ModelInfo, so a source that declares a field
// searchable lifts it without a restart.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

func TestModelSearchable(t *testing.T) {
	if modelSearchable(datasource.ModelInfo{Fields: []datasource.FieldInfo{{Column: "qty"}}}) {
		t.Error("no is_search field: not searchable")
	}
	if modelSearchable(datasource.ModelInfo{Fields: []datasource.FieldInfo{{Column: "secret", IsSearch: true, IsExcluded: true}}}) {
		t.Error("an excluded field does not make the model searchable")
	}
	if !modelSearchable(datasource.ModelInfo{Fields: []datasource.FieldInfo{{Column: "name", IsSearch: true}}}) {
		t.Error("an is_search field makes the model searchable")
	}
}

func TestListRecords_SearchWithoutSearchableFieldsIs400(t *testing.T) {
	panel, _, cleanup := setupPanelWithModels(t, operatorAuth())
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Gadget?search=x", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("Gadget?search=x: status %d body=%s, want 400 (it used to answer 200 with every row)", status, mustJSON(resp))
	}
	errMap, _ := resp["error"].(map[string]interface{})
	msg, _ := errMap["message"].(string)
	if errMap["code"] != "BAD_REQUEST" || !strings.Contains(msg, "Gadget") || !strings.Contains(msg, "no searchable fields") {
		t.Fatalf("error = %s, want BAD_REQUEST naming the model", mustJSON(resp))
	}

	// Without ?search= the model lists as before.
	if _, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Gadget", nil); status != http.StatusOK {
		t.Fatalf("Gadget list: status %d", status)
	}
	// A model with search fields is unaffected.
	if _, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser?search=x", nil); status != http.StatusOK {
		t.Fatalf("AdminUser?search=x: status %d", status)
	}

	// The check reads the live catalogue, not a snapshot taken at mount: a
	// source that stops declaring any field of AdminUser searchable gets
	// the same 400 for it.
	off := false
	for _, column := range []string{"email", "name"} {
		setFieldFlags(panel, "AdminUser", column, &off, nil)
	}
	if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser?search=x", nil); status != http.StatusBadRequest {
		t.Fatalf("AdminUser?search=x once no field is searchable: status %d body=%s, want 400", status, mustJSON(resp))
	}
}

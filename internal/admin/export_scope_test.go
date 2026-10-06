package admin

// Tests for OR-66 and OR-68.
//
// OR-66: the panel's export (POST /api/exports, export_data on admin:*) and
// the fixture dump that rides on the same grant walked every model with no
// confinement but the tenant's. An operator granted export_data and confined
// to their own rows, or kept off a field, exported every row and every field
// anyway — of the model they were looking at and of every model they could
// not open. They now carry what `list` would show that operator: the models
// they may list, the rows of their tenant and their own, and the fields they
// may read, in every format. An export once cut is a copy of what its
// producer could read, so it is handed to that producer (and a superuser),
// not to whoever else holds export_data.
//
// OR-68: a fixture load writes rows, and a read-only model refuses every
// write the panel makes; the load did not.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// exportScopePanel is ownedPanel with a store to export into and a row in a
// model the operator below is never granted (AdminUser).
func exportScopePanel(t *testing.T, policies ...[3]string) (*Panel, *sql.DB, *httptest.Server, *keyedStore) {
	t.Helper()
	panel, sqlDB, srv := ownedPanel(t, policies...)
	store := newKeyedStore()
	panel.store = store
	if _, err := sqlDB.Exec(`INSERT INTO admin_users (email, name, active, created_at, updated_at)
		VALUES ('closed@example.com', 'closed model row', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	return panel, sqlDB, srv, store
}

// confinedExporter holds the panel's export and a list of OwnedNote confined
// to their own rows, with the secret column kept from them.
func confinedExporter() [][3]string {
	return [][3]string{
		{"operator", "admin:OwnedNote#own", "list"},
		{"operator", "admin:OwnedNote.secret", "deny"},
		{"operator", "admin:*", "export_data"},
		{"operator", "admin:*", "list_models"},
	}
}

// exportedBody cuts an export through the panel's own endpoint and returns
// what landed in the store.
func exportedBody(t *testing.T, srv *httptest.Server, store *keyedStore, path string, body map[string]any) string {
	t.Helper()
	resp, status := doJSON(t, http.MethodPost, srv.URL+path, body)
	if status != http.StatusOK {
		t.Fatalf("POST %s %v: status %d body=%s", path, body, status, mustJSON(resp))
	}
	key := fmt.Sprint(resp["storage_key"])
	store.mu.Lock()
	defer store.mu.Unlock()
	out, ok := store.objects[key]
	if !ok {
		t.Fatalf("POST %s %v: the export %q is not in the store: %s", path, body, key, mustJSON(resp))
	}
	return out
}

// Every format the panel's export offers, and the fixture dump.
var panelExports = []struct {
	name string
	path string
	body func(models []string) map[string]any
}{
	{"csv", "/api/exports", func(m []string) map[string]any { return map[string]any{"format": "csv", "models": m} }},
	{"json", "/api/exports", func(m []string) map[string]any { return map[string]any{"format": "json", "models": m} }},
	{"sql", "/api/exports", func(m []string) map[string]any { return map[string]any{"format": "sql", "models": m} }},
	{"dumpdata", "/api/fixtures/dumpdata", func(m []string) map[string]any { return map[string]any{"models": m} }},
}

func TestPanelExport_CarriesOnlyTheOperatorsRowsAndFields(t *testing.T) {
	_, _, srv, store := exportScopePanel(t, confinedExporter()...)

	// The grid this operator is shown, for comparison: their row, no secret.
	list, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote", nil)
	if status != http.StatusOK || strings.Contains(mustJSON(list), "Theirs") || strings.Contains(mustJSON(list), "secret") {
		t.Fatalf("precondition: the operator's list is not confined: status %d body=%s", status, mustJSON(list))
	}

	for _, ex := range panelExports {
		for _, models := range [][]string{{"OwnedNote"}, {}} {
			t.Run(fmt.Sprintf("%s/models=%v", ex.name, models), func(t *testing.T) {
				body := exportedBody(t, srv, store, ex.path, ex.body(models))
				if !strings.Contains(body, "Mine") {
					t.Errorf("the export lost the operator's own row: %s", body)
				}
				if strings.Contains(body, "Theirs") || strings.Contains(body, "somebody-else") {
					t.Errorf("the export carries another operator's row: %s", body)
				}
				// Neither the value nor the column: a header naming a field
				// the operator may not read is the field's existence.
				if strings.Contains(body, "secret") {
					t.Errorf("the export carries a field the operator may not read: %s", body)
				}
				// A model the operator may not list is not in an export of
				// every model — not its rows, not its schema.
				if strings.Contains(body, "closed@example.com") || strings.Contains(body, "admin_users") || strings.Contains(body, "AdminUser") {
					t.Errorf("the export carries a model the operator may not list: %s", body)
				}
			})
		}
	}
}

// The export's own filters can neither widen the scope nor read a hidden
// field through the number of rows they leave.
func TestPanelExport_FiltersCannotWidenTheScopeOrProbeAHiddenField(t *testing.T) {
	_, _, srv, store := exportScopePanel(t, confinedExporter()...)

	// A filter on the owner column, under each spelling the model resolves,
	// is replaced by the scope's: the operator's own row, never another's.
	for _, key := range []string{"owner", "Owner", "OWNER"} {
		body := exportedBody(t, srv, store, "/api/exports", map[string]any{
			"format": "json", "models": []string{"OwnedNote"}, "filters": map[string]string{key: "somebody-else"},
		})
		if strings.Contains(body, "Theirs") {
			t.Errorf("a filter %s=somebody-else widened the export to another operator's row: %s", key, body)
		}
	}

	// A filter on the field the operator may not read is refused: the
	// count of rows it leaves would say what the field holds.
	for _, key := range []string{"secret", "Secret"} {
		resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/exports", map[string]any{
			"format": "json", "models": []string{"OwnedNote"}, "filters": map[string]string{key: "mine-secret"},
		})
		if status != http.StatusBadRequest {
			t.Errorf("an export filtered by %s, which the operator may not read: status %d body=%s, want 400", key, status, mustJSON(resp))
		}
	}

	// A filter on a field the operator reads still narrows.
	body := exportedBody(t, srv, store, "/api/exports", map[string]any{
		"format": "json", "models": []string{"OwnedNote"}, "filters": map[string]string{"title": "nothing like it"},
	})
	if strings.Contains(body, "Mine") {
		t.Errorf("a filter on a readable field did not narrow the export: %s", body)
	}
}

// A model named explicitly that the operator may not list is the answer list
// gives: 403, and nothing is cut.
func TestPanelExport_ModelTheOperatorMayNotListIsRefused(t *testing.T) {
	panel, _, srv, store := exportScopePanel(t, confinedExporter()...)

	for _, ex := range panelExports {
		resp, status := doJSON(t, http.MethodPost, srv.URL+ex.path, ex.body([]string{"AdminUser"}))
		if status != http.StatusForbidden {
			t.Errorf("%s of a model the operator may not list: status %d body=%s, want 403", ex.name, status, mustJSON(resp))
		}
		resp, status = doJSON(t, http.MethodPost, srv.URL+ex.path, ex.body([]string{"OwnedNote", "AdminUser"}))
		if status != http.StatusForbidden {
			t.Errorf("%s naming one model the operator may list and one they may not: status %d body=%s, want 403", ex.name, status, mustJSON(resp))
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.puts) != 0 {
		t.Fatalf("a refused export was written to the store anyway: %v", store.puts)
	}
	_ = panel
}

// An ownership grant the panel cannot honour is refused, never widened —
// on the export as on the list (ADR-007 point 3).
func TestPanelExport_OwnGrantWithoutAnOwnerColumnIsRefused(t *testing.T) {
	panel, _, srv, store := exportScopePanel(t, confinedExporter()...)
	panel.config.RowOwnerFields = nil

	for _, ex := range panelExports {
		resp, status := doJSON(t, http.MethodPost, srv.URL+ex.path, ex.body([]string{"OwnedNote"}))
		if status != http.StatusForbidden || !strings.Contains(mustJSON(resp), "own rows") {
			t.Errorf("%s under an #own grant with no owner column: status %d body=%s, want 403 saying why", ex.name, status, mustJSON(resp))
		}
		// An export of every model leaves the model out instead.
		body := exportedBody(t, srv, store, ex.path, ex.body(nil))
		if strings.Contains(body, "Mine") || strings.Contains(body, "Theirs") {
			t.Errorf("%s of every model carries a model whose #own grant cannot be honoured: %s", ex.name, body)
		}
	}
}

// A superuser exports what it always exported: every row, every field, every
// model.
func TestPanelExport_SuperuserIsUnconfined(t *testing.T) {
	panel, _, srv, store := exportScopePanel(t, confinedExporter()...)
	panel.config.Auth = superuserAuth()

	for _, ex := range panelExports {
		body := exportedBody(t, srv, store, ex.path, ex.body(nil))
		for _, want := range []string{"Mine", "Theirs", "mine-secret", "their-secret", "closed@example.com"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s by a superuser lost %q: %s", ex.name, want, body)
			}
		}
	}
}

// A full grant with no field policy is unconfined too: the export of a model
// the operator lists in full carries every row and field of it.
func TestPanelExport_FullGrantIsUnconfined(t *testing.T) {
	_, _, srv, store := exportScopePanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:*", "export_data"},
	)
	for _, ex := range panelExports {
		body := exportedBody(t, srv, store, ex.path, ex.body([]string{"OwnedNote"}))
		for _, want := range []string{"Mine", "Theirs", "mine-secret", "their-secret"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s under a full grant lost %q: %s", ex.name, want, body)
			}
		}
	}
}

// An export is a copy of what its producer could read. It is listed, polled
// and downloaded by that producer and a superuser — not by another operator
// who holds export_data, who would otherwise read through it what their own
// export leaves out.
func TestPanelExport_JobsAreHandedToTheirProducer(t *testing.T) {
	panel, _, srv, _ := exportScopePanel(t, confinedExporter()...)

	panel.config.Auth = superuserAuth()
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/exports", map[string]any{"format": "csv", "models": []string{"OwnedNote"}})
	if status != http.StatusOK {
		t.Fatalf("superuser export: status %d body=%s", status, mustJSON(resp))
	}
	full := fmt.Sprint(resp["storage_key"])

	panel.config.Auth = operatorAuth()
	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/exports", map[string]any{"format": "csv", "models": []string{"OwnedNote"}})
	if status != http.StatusOK {
		t.Fatalf("operator export: status %d body=%s", status, mustJSON(resp))
	}
	own := fmt.Sprint(resp["storage_key"])
	if own == full {
		t.Fatalf("two exports share the storage key %q: the second overwrote the first", own)
	}

	status, body := getRaw(t, srv.URL+"/api/exports")
	if status != http.StatusOK {
		t.Fatalf("list exports: status %d body=%s", status, body)
	}
	var jobs []map[string]any
	if err := json.Unmarshal([]byte(body), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0]["id"] != own {
		t.Errorf("the operator's export list = %s, want only their own export", body)
	}
	if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/exports/"+url.PathEscape(full), nil); status != http.StatusNotFound {
		t.Errorf("status of the superuser's export: %d body=%s, want 404", status, mustJSON(resp))
	}
	if status, body := getRaw(t, srv.URL+"/api/exports/download?key="+url.QueryEscape(full)); status != http.StatusNotFound {
		t.Errorf("download of the superuser's export: status %d body=%q, want 404", status, body)
	}
	// A key the panel holds no job for (a restart empties the registry) is
	// not served to a confined operator either.
	if status, body := getRaw(t, srv.URL+"/api/exports/download?key="+url.QueryEscape("_tmp/export_unknown.csv")); status != http.StatusNotFound {
		t.Errorf("download of an unregistered export: status %d body=%q, want 404", status, body)
	}
	if status, body := getRaw(t, srv.URL+"/api/exports/download?key="+url.QueryEscape(own)); status != http.StatusOK || !strings.Contains(body, "Mine") || strings.Contains(body, "Theirs") {
		t.Errorf("download of the operator's own export: status %d body=%q", status, body)
	}

	// A superuser sees both.
	panel.config.Auth = superuserAuth()
	status, body = getRaw(t, srv.URL+"/api/exports")
	if err := json.Unmarshal([]byte(body), &jobs); err != nil || status != http.StatusOK || len(jobs) != 2 {
		t.Errorf("the superuser's export list: status %d body=%s err=%v, want both exports", status, body, err)
	}
	if status, body := getRaw(t, srv.URL+"/api/exports/download?key="+url.QueryEscape(own)); status != http.StatusOK {
		t.Errorf("a superuser's download of the operator's export: status %d body=%q", status, body)
	}
}

// The schema's export_data is the handler's answer for that model: the
// export of a model asks export_data of the panel AND list of the model.
func TestCapabilityHints_ExportDataNeedsTheModelsList(t *testing.T) {
	_, _, srv, _ := exportScopePanel(t, confinedExporter()...)

	models, status := doJSON(t, http.MethodGet, srv.URL+"/api/models", nil)
	if status != http.StatusOK {
		t.Fatalf("models: status %d body=%s", status, mustJSON(models))
	}
	listed, _ := models["models"].([]interface{})
	got := map[string]bool{}
	for _, raw := range listed {
		m, _ := raw.(map[string]interface{})
		perms, _ := m["permissions"].(map[string]interface{})
		held, _ := perms["export_data"].(bool)
		got[fmt.Sprint(m["name"])] = held
	}
	if !got["OwnedNote"] {
		t.Errorf("export_data on OwnedNote, which the operator lists and exports: %v", got)
	}
	if held, ok := got["AdminUser"]; !ok || held {
		t.Errorf("export_data on AdminUser, which the operator may not list and whose export is refused: %v (listed %v)", held, ok)
	}
}

// ---- OR-68: a fixture load into a read-only model ------------------------

func TestLoaddata_ReadOnlyModelIsRefused(t *testing.T) {
	panel, sqlDB, srv, store := exportScopePanel(t)
	panel.config.Auth = superuserAuth()
	meta, ok := panel.registry.Get("OwnedNote")
	if !ok {
		t.Fatal("OwnedNote is not registered")
	}

	store.objects["_tmp/fixture_ro.json"] = fixtureJSON(t,
		map[string]any{"model": "OwnedNote", "pk": 1, "fields": map[string]any{"title": "Overwritten"}},
		map[string]any{"model": "OwnedNote", "pk": 99, "fields": map[string]any{"title": "Created", "owner": "operator"}},
	)
	// A fixture that names a writable model beside the read-only one is
	// refused whole: half a fixture loaded is worse than none.
	store.objects["_tmp/fixture_mixed.json"] = fixtureJSON(t,
		map[string]any{"model": "AdminUser", "fields": map[string]any{"email": "loaded@example.com", "name": "loaded", "active": true}},
		map[string]any{"model": "OwnedNote", "pk": 1, "fields": map[string]any{"title": "Overwritten"}},
	)

	meta.Config.ReadOnly = true
	for _, key := range []string{"_tmp/fixture_ro.json", "_tmp/fixture_mixed.json"} {
		resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": key, "on_conflict": "update"})
		if status != http.StatusForbidden || !strings.Contains(mustJSON(resp), "read-only") {
			t.Errorf("loaddata %s into a read-only model: status %d body=%s, want 403 saying read-only", key, status, mustJSON(resp))
		}
	}
	if got := ownedTitle(t, sqlDB, 1); got != "Mine" {
		t.Errorf("a refused load rewrote a row of the read-only model: title %q", got)
	}
	var notes, users int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM owned_notes`).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_users WHERE email = 'loaded@example.com'`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if notes != 2 || users != 0 {
		t.Errorf("a refused load wrote rows anyway: %d notes (want 2), %d loaded users (want 0)", notes, users)
	}

	// Writable again, the same fixture loads: the refusal was the model's.
	meta.Config.ReadOnly = false
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": "_tmp/fixture_ro.json", "on_conflict": "update"})
	if status != http.StatusOK {
		t.Fatalf("loaddata into a writable model: status %d body=%s", status, mustJSON(resp))
	}
	if got := ownedTitle(t, sqlDB, 1); got != "Overwritten" {
		t.Errorf("the writable load did not land: title %q", got)
	}
}

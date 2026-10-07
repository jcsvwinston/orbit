package admin

// Tests for OR-70.
//
// GET /api/models carries a record count per model, and counted every row of
// every model for anyone holding list_models: a tenant's operator read how
// many rows the other tenants had, an #own operator the size of the whole
// table, and an operator with no list on a model how many rows it held — the
// numbers the list itself would never answer them. The count is now what
// the operator's list would answer: confined to the request's tenant and,
// under an #own grant, to their own rows, and unknown (-1, count_known
// false, as the light mode reports every count) for a model they may not
// list. The model stays listed and attributed to its database either way.

import (
	"net/http"
	"testing"
)

// modelsWithCounts reads GET /api/models with counts and returns each model's
// entry by name, plus runtime.records_total.
func modelsWithCounts(t *testing.T, base string) (map[string]map[string]any, float64) {
	t.Helper()
	resp, status := doJSON(t, http.MethodGet, base+"/api/models?counts=1", nil)
	if status != http.StatusOK {
		t.Fatalf("models: status %d body=%s", status, mustJSON(resp))
	}
	raw, _ := resp["models"].([]any)
	byName := make(map[string]map[string]any, len(raw))
	for _, m := range raw {
		if entry, ok := m.(map[string]any); ok {
			name, _ := entry["name"].(string)
			byName[name] = entry
		}
	}
	runtime, _ := resp["runtime"].(map[string]any)
	total, _ := runtime["records_total"].(float64)
	return byName, total
}

// An #own operator counts their own rows: one of ownedPanel's two.
func TestModelCounts_FollowTheOwnGrant(t *testing.T) {
	_, _, srv := ownedPanel(t,
		[3]string{"operator", "admin:*", "list_models"},
		[3]string{"operator", "admin:OwnedNote#own", "list"},
	)
	models, total := modelsWithCounts(t, srv.URL)

	note := models["OwnedNote"]
	if note["count_known"] != true || note["count"] != float64(1) {
		t.Fatalf("OwnedNote count = %v (known %v), want 1: the operator's own row, not the table's two", note["count"], note["count_known"])
	}
	counts, _ := note["counts"].(map[string]any)
	if counts["default"] != float64(1) {
		t.Errorf("the per-database count is not the scoped one: %v", note["counts"])
	}
	// The sum the dashboard shows adds up only the counts the operator reads:
	// OwnedNote's one, and nothing for AdminUser, which they may not list.
	if total != 1 {
		t.Fatalf("records_total = %v, want 1", total)
	}
}

// Without the model's list, the count is unknown — not zero, which would
// say the table is empty, and not the table's size. The model is still
// listed and still attributed to its database.
func TestModelCounts_AreUnknownWithoutTheModelsList(t *testing.T) {
	_, _, srv := ownedPanel(t, [3]string{"operator", "admin:*", "list_models"})
	models, total := modelsWithCounts(t, srv.URL)

	note, ok := models["OwnedNote"]
	if !ok {
		t.Fatalf("OwnedNote is not listed: %v", models)
	}
	if note["count_known"] != false || note["count"] != float64(-1) {
		t.Fatalf("OwnedNote count = %v (known %v), want unknown (-1)", note["count"], note["count_known"])
	}
	if _, has := note["counts"]; has {
		t.Errorf("the per-database counts of a model the operator may not list are reported: %v", note["counts"])
	}
	if dbs, _ := note["databases"].([]any); len(dbs) == 0 {
		t.Errorf("the model lost its database attribution: %v", note)
	}
	if total != 0 {
		t.Fatalf("records_total = %v, want 0: no count the operator reads", total)
	}
}

// In a request the host resolved to a tenant, every count is that tenant's —
// a superuser's included, as on the list, until ?tenant= switches it. The
// tenant column spelled by its json key (CamelNote) or hidden from JSON
// (HiddenNote) confines the count the same way it confines the list.
func TestModelCounts_StayInTheRequestsTenant(t *testing.T) {
	_, _, srv := scopedPanel(t, superuserAuth())
	models, _ := modelsWithCounts(t, srv.URL)

	for _, name := range []string{"ScopedNote", "ScopedCode", "CamelNote", "HiddenNote"} {
		m := models[name]
		if m["count_known"] != true || m["count"] != float64(1) {
			t.Errorf("%s count = %v (known %v), want 1: the acme row, not both tenants'", name, m["count"], m["count_known"])
		}
	}
}

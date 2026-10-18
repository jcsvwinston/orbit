package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleListModels_DatabasesReflectPresence is the regression guard for
// fleetdesk finding #11: model→database attribution used only the DECLARED
// alias, so tenant-isolated topologies (same schema replicated across tenant
// databases, declared alias empty/default) reported every model as living in
// "default" and the Data Studio sidebar's database view collapsed to a
// single group. Attribution must reflect probed table PRESENCE — in fast
// (no-counts) mode too. Since 2.0 the fixture is the Quark one an
// application composes: one adapter per database, each probed for the
// table.
func TestHandleListModels_DatabasesReflectPresence(t *testing.T) {
	panel, _, _ := twoAliasQuarkPanel(t)
	panel.config.Databases = []DatabaseRuntimeInfo{
		{Alias: "default", Engine: "sql", Dialect: "sqlite", IsDefault: true},
		{Alias: "tenant_other", Engine: "sql", Dialect: "sqlite"},
		{Alias: "audit", Engine: "sql", Dialect: "sqlite"},
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// FAST mode (no include_counts): presence must still be probed.
	res, err := http.Get(srv.URL + "/api/models")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name      string   `json:"name"`
			Databases []string `json:"databases"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var got []string
	for _, m := range payload.Models {
		if m.Name == "AdminUser" {
			got = m.Databases
		}
	}
	// Both databases that hold the table, and not the one no adapter
	// serves: the sweep asks every alias, and an adapter that does not
	// serve one is the model being absent there.
	if len(got) != 2 || got[0] != "default" || got[1] != "tenant_other" {
		t.Fatalf("AdminUser databases = %v, want [default tenant_other] (probed presence on both adapters)", got)
	}
}

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestHandleListRecords_DBAliasSelectsDatabase is the regression guard for
// fleetdesk finding #10: the admin UI's Data Studio sends `db_alias=<alias>`
// when a database pill is selected, but collectFilters treated it as a
// column filter — every non-default-database listing failed with
// `invalid filter field "db_alias"`. The parameter must (a) be reserved,
// and (b) actually route the query to the selected database handle.
func TestHandleListRecords_DBAliasSelectsDatabase(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	// Second, isolated database that holds one AdminUser row the default DB
	// does not have.
	logger := observe.NewLogger("error", "text")
	other, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New(other): %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	otherSQL, err := other.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB(other): %v", err)
	}
	if err := ensureAdminUserSchema(otherSQL); err != nil {
		t.Fatalf("schema(other): %v", err)
	}
	if _, err := otherSQL.Exec(
		`INSERT INTO admin_users (email, name, active, created_at, updated_at) VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"tenant-only@example.com", "Tenant Only"); err != nil {
		t.Fatalf("insert(other): %v", err)
	}

	panel.config.DatabaseHandles = map[string]*db.DB{
		"default":      panel.db,
		"tenant_other": other,
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// 1. The reserved parameter must not be rejected as a filter: the
	// alias reaches the source. The panel's source (a Quark adapter) is
	// bound to ONE database, so an alias the panel knows and the source
	// does not serve is refused by the source, as a 400 naming it — never
	// answered from the default database under the other name.
	res, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=tenant_other")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an alias the source does not serve, got %d", res.StatusCode)
	}
	var refusal map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&refusal); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body := mustJSON(refusal); !strings.Contains(body, "tenant_other") {
		t.Fatalf("the refusal does not name the alias: %s", body)
	}

	// 2. The alias the source serves lists its rows, and only its rows.
	res1, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=default")
	if err != nil {
		t.Fatalf("request(default): %v", err)
	}
	defer res1.Body.Close()
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing via db_alias=default, got %d", res1.StatusCode)
	}
	var payload struct {
		Items []struct {
			Email string `json:"email"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res1.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, item := range payload.Items {
		if item.Email == "tenant-only@example.com" {
			t.Fatalf("the default database listing carries the other database's row: %+v", payload.Items)
		}
	}

	// 3. An unknown alias is a clean 400, not a fall-through to default.
	res2, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=ghost")
	if err != nil {
		t.Fatalf("request(ghost): %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown alias, got %d", res2.StatusCode)
	}
}

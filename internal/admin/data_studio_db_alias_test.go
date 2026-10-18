package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/orbit/datasource"
	"github.com/jcsvwinston/orbit/quarkdatasource"
)

// aliasedQuarkSources is what an application with several databases hands
// the panel since 2.0: one quarkdatasource adapter per database alias (a
// Quark client is bound to one database, and the adapter to one alias —
// "use one adapter per client if you browse several"), composed behind the
// contract. The catalogue is the default alias's; Store routes each request
// to the adapter serving its alias. An alias the panel knows but no adapter
// serves falls through to the default adapter, which refuses it naming the
// alias (never answering from the default database under another name).
type aliasedQuarkSources struct {
	defaultAlias string
	byAlias      map[string]*quarkdatasource.Adapter
}

func (s aliasedQuarkSources) All() []datasource.ModelInfo {
	return s.byAlias[s.defaultAlias].All()
}

func (s aliasedQuarkSources) Get(name string) (datasource.ModelInfo, bool) {
	return s.byAlias[s.defaultAlias].Get(name)
}

func (s aliasedQuarkSources) Store(modelName, dbAlias string) (datasource.RecordStore, error) {
	alias := strings.TrimSpace(dbAlias)
	if alias == "" {
		alias = s.defaultAlias
	}
	adapter, ok := s.byAlias[alias]
	if !ok {
		adapter = s.byAlias[s.defaultAlias]
	}
	return adapter.Store(modelName, alias)
}

// twoAliasQuarkPanel mounts the panel on two SQLite databases, "default"
// and "tenant_other", each with its own admin_users table and its own
// quarkdatasource adapter registering AdminUser; a third handle, "audit",
// is a database the panel knows and no adapter serves. It returns the
// panel and the two aliased databases' SQL handles.
func twoAliasQuarkPanel(t *testing.T) (*Panel, *sql.DB, *sql.DB) {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	handles := map[string]*db.DB{}
	sqlDBs := map[string]*sql.DB{}
	for _, alias := range []string{"default", "tenant_other", "audit"} {
		database, err := db.New(db.Config{
			Engine:          db.EngineSQL,
			DatabaseURL:     "sqlite://:memory:",
			DatabaseMaxOpen: 1,
			DatabaseMaxIdle: 1,
		}, logger)
		if err != nil {
			t.Fatalf("db.New(%s): %v", alias, err)
		}
		t.Cleanup(func() { _ = database.Close() })
		sqlDB, err := database.SqlDB()
		if err != nil {
			t.Fatalf("SqlDB(%s): %v", alias, err)
		}
		handles[alias] = database
		sqlDBs[alias] = sqlDB
	}

	src := aliasedQuarkSources{defaultAlias: "default", byAlias: map[string]*quarkdatasource.Adapter{}}
	for _, alias := range []string{"default", "tenant_other"} {
		if err := ensureAdminUserSchema(sqlDBs[alias]); err != nil {
			t.Fatalf("schema(%s): %v", alias, err)
		}
		client, err := quark.NewWithDB("sqlite", sqlDBs[alias])
		if err != nil {
			t.Fatalf("quark.NewWithDB(%s): %v", alias, err)
		}
		adapter := quarkdatasource.New(client, quarkdatasource.WithDatabaseAlias(alias))
		if err := quarkdatasource.Register[AdminUser](adapter); err != nil {
			t.Fatalf("quarkdatasource.Register(%s): %v", alias, err)
		}
		src.byAlias[alias] = adapter
	}

	panel := NewPanel(src, logger, PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin"}},
		DatabaseHandles: handles,
	})
	return panel, sqlDBs["default"], sqlDBs["tenant_other"]
}

// TestHandleListRecords_DBAliasSelectsDatabase is the regression guard for
// fleetdesk finding #10: the admin UI's Data Studio sends `db_alias=<alias>`
// when a database pill is selected, but collectFilters treated it as a
// column filter — every non-default-database listing failed with
// `invalid filter field "db_alias"`. The parameter must (a) be reserved,
// and (b) actually route the query to the selected database. Since 2.0 the
// fixture is the Quark one an application composes: one adapter per
// database, so the routing is proved end to end through quarkdatasource.
func TestHandleListRecords_DBAliasSelectsDatabase(t *testing.T) {
	panel, _, otherSQL := twoAliasQuarkPanel(t)

	// The second database holds one AdminUser row the default one does not.
	if _, err := otherSQL.Exec(
		`INSERT INTO admin_users (email, name, active, created_at, updated_at) VALUES (?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"tenant-only@example.com", "Tenant Only"); err != nil {
		t.Fatalf("insert(other): %v", err)
	}

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	listEmails := func(alias string) (int, []string) {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=" + alias)
		if err != nil {
			t.Fatalf("request(%s): %v", alias, err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return res.StatusCode, nil
		}
		var payload struct {
			Items []struct {
				Email string `json:"email"`
			} `json:"items"`
		}
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatalf("decode(%s): %v", alias, err)
		}
		emails := make([]string, 0, len(payload.Items))
		for _, item := range payload.Items {
			emails = append(emails, item.Email)
		}
		return res.StatusCode, emails
	}

	// 1. The reserved parameter must not be rejected as a filter, and must
	// actually select the aliased database: the row only that database
	// holds is listed, through the adapter bound to it.
	status, emails := listEmails("tenant_other")
	if status != http.StatusOK {
		t.Fatalf("expected 200 listing via db_alias=tenant_other, got %d", status)
	}
	if len(emails) != 1 || emails[0] != "tenant-only@example.com" {
		t.Fatalf("expected the tenant_other row, got %v", emails)
	}

	// 2. The default alias lists its own database — which does not hold
	// that row.
	status, emails = listEmails("default")
	if status != http.StatusOK {
		t.Fatalf("expected 200 listing via db_alias=default, got %d", status)
	}
	for _, email := range emails {
		if email == "tenant-only@example.com" {
			t.Fatalf("the default database listing carries the other database's row: %v", emails)
		}
	}

	// 3. An alias the panel knows and no adapter serves is refused by the
	// adapter it reaches, as a 400 naming the alias — never answered from
	// the default database under the other name.
	res, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=audit")
	if err != nil {
		t.Fatalf("request(audit): %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an alias no adapter serves, got %d", res.StatusCode)
	}
	var refusal map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&refusal); err != nil {
		t.Fatalf("decode(audit): %v", err)
	}
	if body := mustJSON(refusal); !strings.Contains(body, "audit") {
		t.Fatalf("the refusal does not name the alias: %s", body)
	}

	// 4. An unknown alias is a clean 400, not a fall-through to default.
	res2, err := http.Get(srv.URL + "/api/models/AdminUser?db_alias=ghost")
	if err != nil {
		t.Fatalf("request(ghost): %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown alias, got %d", res2.StatusCode)
	}
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"

	"log/slog"
)

// operatorEnv is a panel whose auth provider owns an admin-users table, with
// the acting operator seeded as an active superuser — the shape every one of
// these tests starts from.
type operatorEnv struct {
	panel *Panel
	srv   *httptest.Server
	sqlDB *sql.DB
	auth  *testAdminAuth
}

func newOperatorEnv(t *testing.T) *operatorEnv {
	t.Helper()
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	panel.rbac = enf
	panel.audit = newAuditStore(100)

	sqlDB, err := panel.config.DatabaseHandles["default"].SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	if err := EnsureBootstrapAdminUsersSchema(context.Background(), sqlDB, "sqlite"); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	seedOperatorRow(t, sqlDB, "actor", "actor", "actor@example.com", true)
	provider.operators = &operatorStore{db: sqlDB, table: defaultAdminUsersTable, system: "sqlite"}

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return &operatorEnv{panel: panel, srv: srv, sqlDB: sqlDB, auth: provider}
}

func (e *operatorEnv) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload
}

// An operator is created, listed, and can be found by the very provider that
// authenticates requests — which is the whole point of OR-4: the account
// exists without anyone opening a shell on the server.
func TestOperators_CreateThenListThenAuthenticate(t *testing.T) {
	env := newOperatorEnv(t)

	status, created := env.do(t, http.MethodPost, "/api/admin-users",
		`{"username":"ana","email":"ana@example.com","password":"a-long-enough-password","roles":["editors"]}`)
	if status != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %v", status, created)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}
	if _, leaked := created["password_hash"]; leaked {
		t.Error("the created operator carries its password hash to the client")
	}

	status, list := env.do(t, http.MethodGet, "/api/admin-users", "")
	if status != http.StatusOK {
		t.Fatalf("list = %d, want 200", status)
	}
	operators, _ := list["operators"].([]any)
	found := false
	for _, item := range operators {
		rec, _ := item.(map[string]any)
		if rec["username"] == "ana" {
			found = true
			roles, _ := rec["roles"].([]any)
			if len(roles) != 1 || roles[0] != "editors" {
				t.Errorf("ana's roles = %v, want the role the create granted", roles)
			}
		}
	}
	if !found {
		t.Fatalf("the created operator is not in the list: %v", list)
	}

	// The provider that authenticates every request resolves the new
	// account: a row the panel wrote is an account the panel accepts.
	provider := NewDatabaseAdminAuth(env.sqlDB, nil, "/admin").WithSystem("sqlite")
	rec, ok, err := provider.findUserByID(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("findUserByID(%q) = ok %v err %v, want the new operator", id, ok, err)
	}
	if !auth.CheckPassword("a-long-enough-password", rec.PasswordHash) {
		t.Error("the password the panel set does not verify against the stored hash")
	}
}

// Deactivating is the panel's answer to "this person is gone": the account
// and its trail stay, and the provider stops resolving it — so the session
// dies on the next request.
func TestOperators_DeactivateRevokesTheAccount(t *testing.T) {
	env := newOperatorEnv(t)
	status, created := env.do(t, http.MethodPost, "/api/admin-users",
		`{"username":"leaver","email":"leaver@example.com","password":"a-long-enough-password"}`)
	if status != http.StatusCreated {
		t.Fatalf("create = %d", status)
	}
	id, _ := created["id"].(string)

	if status, body := env.do(t, http.MethodPost, "/api/admin-users/"+id+"/disable", `{}`); status != http.StatusOK {
		t.Fatalf("disable = %d: %v", status, body)
	}

	provider := NewDatabaseAdminAuth(env.sqlDB, nil, "/admin").WithSystem("sqlite")
	if _, ok, err := provider.findUserByID(context.Background(), id); ok || err != nil {
		t.Fatalf("a deactivated operator still resolves (ok=%v err=%v)", ok, err)
	}
	if _, ok, err := provider.findUserByLogin(context.Background(), "leaver"); ok || err != nil {
		t.Fatalf("a deactivated operator can still be looked up by login (ok=%v err=%v)", ok, err)
	}

	// It is still there, and still listed: the panel deactivates people, it
	// does not erase them.
	_, list := env.do(t, http.MethodGet, "/api/admin-users", "")
	operators, _ := list["operators"].([]any)
	for _, item := range operators {
		rec, _ := item.(map[string]any)
		if rec["username"] == "leaver" {
			if rec["is_active"] != false {
				t.Errorf("the deactivated operator lists as active: %v", rec)
			}
			return
		}
	}
	t.Fatalf("the deactivated operator vanished from the list: %v", list)
}

// Re-enabling puts the account back.
func TestOperators_EnableRestoresTheAccount(t *testing.T) {
	env := newOperatorEnv(t)
	_, created := env.do(t, http.MethodPost, "/api/admin-users",
		`{"username":"backagain","email":"backagain@example.com","password":"a-long-enough-password"}`)
	id, _ := created["id"].(string)

	env.do(t, http.MethodPost, "/api/admin-users/"+id+"/disable", `{}`)
	if status, body := env.do(t, http.MethodPost, "/api/admin-users/"+id+"/enable", `{}`); status != http.StatusOK {
		t.Fatalf("enable = %d: %v", status, body)
	}

	provider := NewDatabaseAdminAuth(env.sqlDB, nil, "/admin").WithSystem("sqlite")
	if _, ok, _ := provider.findUserByID(context.Background(), id); !ok {
		t.Fatal("a re-enabled operator does not resolve")
	}
}

// The two guards that keep a panel administrable, and the reason they live in
// the handler rather than in the UI: the UI is not the only client.
func TestOperators_GuardsRefuseLockout(t *testing.T) {
	env := newOperatorEnv(t)

	t.Run("cannot deactivate yourself", func(t *testing.T) {
		status, body := env.do(t, http.MethodPost, "/api/admin-users/actor/disable", `{}`)
		if status != http.StatusConflict {
			t.Fatalf("disable self = %d, want 409: %v", status, body)
		}
	})

	t.Run("cannot delete yourself", func(t *testing.T) {
		status, body := env.do(t, http.MethodDelete, "/api/admin-users/actor", "")
		if status != http.StatusConflict {
			t.Fatalf("delete self = %d, want 409: %v", status, body)
		}
	})

	t.Run("cannot demote the last active superuser", func(t *testing.T) {
		// A second superuser, so the guard has something to count, then
		// the acting one is demoted through another account.
		seedOperatorRow(t, env.sqlDB, "second-super", "second", "second@example.com", true)
		if status, body := env.do(t, http.MethodPut, "/api/admin-users/second-super",
			`{"is_superuser":false}`); status != http.StatusOK {
			t.Fatalf("demoting one of two superusers = %d, want 200: %v", status, body)
		}
		// Now "actor" is the only active superuser left.
		status, body := env.do(t, http.MethodPut, "/api/admin-users/actor", `{"is_superuser":false}`)
		if status != http.StatusConflict {
			t.Fatalf("demoting the last superuser = %d, want 409: %v", status, body)
		}
		if msg, _ := body["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "last active superuser") {
			t.Errorf("the refusal does not say why: %v", body)
		}
	})
}

// Input the panel refuses, and the reasons a client can act on.
func TestOperators_RejectsBadInput(t *testing.T) {
	env := newOperatorEnv(t)

	t.Run("short password", func(t *testing.T) {
		status, body := env.do(t, http.MethodPost, "/api/admin-users",
			`{"username":"short","email":"short@example.com","password":"tiny"}`)
		if status != http.StatusBadRequest {
			t.Fatalf("short password = %d, want 400: %v", status, body)
		}
	})

	t.Run("duplicate username", func(t *testing.T) {
		body := `{"username":"twice","email":"twice@example.com","password":"a-long-enough-password"}`
		if status, _ := env.do(t, http.MethodPost, "/api/admin-users", body); status != http.StatusCreated {
			t.Fatalf("first create = %d, want 201", status)
		}
		status, payload := env.do(t, http.MethodPost, "/api/admin-users", body)
		if status != http.StatusConflict {
			t.Fatalf("duplicate = %d, want 409: %v", status, payload)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		status, _ := env.do(t, http.MethodGet, "/api/admin-users/nobody", "")
		if status != http.StatusNotFound {
			t.Fatalf("unknown operator = %d, want 404", status)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		status, _ := env.do(t, http.MethodPost, "/api/admin-users", `{"password":"a-long-enough-password"}`)
		if status != http.StatusBadRequest {
			t.Fatalf("no username = %d, want 400", status)
		}
	})
}

// An application that authenticates its operators somewhere else (ADR-004)
// gets an honest refusal rather than a panel that invents an account store.
func TestOperators_ProviderWithoutStoreSaysSo(t *testing.T) {
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/admin-users")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("list without a store = %d, want 501", resp.StatusCode)
	}
}

// The store binds its values on every dialect it claims to support, and
// refuses the ones it does not — operator management writes end-user input,
// so there is no inline-literal fallback here.
func TestOperatorStore_BindsPerDialect(t *testing.T) {
	for _, system := range []string{"sqlite", "mysql", "postgresql", "mssql", "oracle"} {
		s := &operatorStore{system: system}
		if _, err := s.binds(3); err != nil {
			t.Errorf("binds(%q) = %v, want placeholders", system, err)
		}
	}
	s := &operatorStore{system: "cassandra"}
	if _, err := s.binds(1); err == nil {
		t.Error("an unknown dialect returned placeholders; it must refuse instead")
	} else if !strings.Contains(err.Error(), "cassandra") {
		t.Errorf("the refusal does not name the dialect: %v", err)
	}
}

// The schema gains is_active on a table created before the column existed —
// an upgrade path a deployment takes without noticing.
func TestEnsureAdminUsersActiveColumn_AddsItToAnOldTable(t *testing.T) {
	sqlDB := openAdminUsersTestDB(t)
	if _, err := sqlDB.Exec(`CREATE TABLE nucleus_admin_users (
		id TEXT PRIMARY KEY, username TEXT, email TEXT,
		password_hash TEXT, is_superuser INTEGER,
		created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO nucleus_admin_users VALUES ('1','old','old@example.com','h',1,'','')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := EnsureBootstrapAdminUsersSchema(context.Background(), sqlDB, "sqlite"); err != nil {
		t.Fatalf("ensure schema on an old table: %v", err)
	}

	var active int
	if err := sqlDB.QueryRow(`SELECT is_active FROM nucleus_admin_users WHERE id = '1'`).Scan(&active); err != nil {
		t.Fatalf("read is_active: %v", err)
	}
	if active != 1 {
		t.Errorf("the existing operator came back deactivated (is_active=%d): an upgrade must not lock anyone out", active)
	}

	// Idempotent: the next boot runs the same code.
	if err := EnsureBootstrapAdminUsersSchema(context.Background(), sqlDB, "sqlite"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func openAdminUsersTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", fmt.Sprintf("file:operators_%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

// An operator's tenant (OR-80, DEP-2026-003) is set at creation, kept by an
// edit that does not name it — the panel's own screen sends email and
// is_superuser only — changed or cleared by one that does, and audited.
func TestOperators_TenantIsSetKeptAndCleared(t *testing.T) {
	env := newOperatorEnv(t)

	status, created := env.do(t, http.MethodPost, "/api/admin-users",
		`{"username":"ana","email":"ana@example.com","password":"a-long-enough-password","tenant_id":" acme "}`)
	if status != http.StatusCreated {
		t.Fatalf("create = %d, want 201: %v", status, created)
	}
	if created["tenant_id"] != "acme" {
		t.Fatalf("created tenant_id = %v, want the trimmed acme", created["tenant_id"])
	}
	id := created["id"].(string)
	tenantColumn := func() sql.NullString {
		t.Helper()
		var v sql.NullString
		if err := env.sqlDB.QueryRow(`SELECT tenant_id FROM nucleus_admin_users WHERE id = ?`, id).Scan(&v); err != nil {
			t.Fatalf("read tenant_id: %v", err)
		}
		return v
	}
	if got := tenantColumn(); !got.Valid || got.String != "acme" {
		t.Fatalf("stored tenant_id = %+v, want acme", got)
	}

	status, got := env.do(t, http.MethodGet, "/api/admin-users/"+id, "")
	if status != http.StatusOK || got["tenant_id"] != "acme" {
		t.Fatalf("get = %d %v, want tenant_id acme", status, got)
	}

	status, got = env.do(t, http.MethodPut, "/api/admin-users/"+id, `{"email":"ana@acme.example"}`)
	if status != http.StatusOK || got["tenant_id"] != "acme" {
		t.Fatalf("an edit that does not name the tenant = %d %v, want tenant_id kept", status, got)
	}

	status, got = env.do(t, http.MethodPut, "/api/admin-users/"+id, `{"tenant_id":"globex"}`)
	if status != http.StatusOK || got["tenant_id"] != "globex" {
		t.Fatalf("rebind = %d %v, want tenant_id globex", status, got)
	}
	entries := env.panel.audit.list(auditQueryOpts{Action: "operator.update", RecordID: id})
	if len(entries) == 0 || entries[0].OldValue["tenant_id"] != "acme" || entries[0].NewValue["tenant_id"] != "globex" {
		t.Fatalf("operator.update entry = %+v, want tenant_id acme -> globex", entries)
	}

	status, got = env.do(t, http.MethodPut, "/api/admin-users/"+id, `{"tenant_id":""}`)
	if status != http.StatusOK || got["tenant_id"] != "" {
		t.Fatalf("clear = %d %v, want tenant_id empty", status, got)
	}
	if v := tenantColumn(); v.Valid {
		t.Fatalf("cleared tenant_id = %q, want NULL (an unbound operator reads like one from before the column)", v.String)
	}

	// "all" is every tenant on this panel, not a tenant to bind to.
	for _, body := range []string{`{"tenant_id":"all"}`, `{"tenant_id":" ALL "}`} {
		if status, got := env.do(t, http.MethodPut, "/api/admin-users/"+id, body); status != http.StatusBadRequest {
			t.Errorf("PUT %s = %d %v, want 400", body, status, got)
		}
	}
	if status, got := env.do(t, http.MethodPost, "/api/admin-users",
		`{"username":"bob","email":"bob@example.com","password":"a-long-enough-password","tenant_id":"All"}`); status != http.StatusBadRequest {
		t.Errorf("create with tenant_id All = %d %v, want 400", status, got)
	}
	if status, got := env.do(t, http.MethodPut, "/api/admin-users/"+id,
		`{"tenant_id":"`+strings.Repeat("t", operatorTenantMaxLength+1)+`"}`); status != http.StatusBadRequest {
		t.Errorf("an over-long tenant = %d %v, want 400", status, got)
	}
}

// The schema gains tenant_id on a table created before the column existed,
// and every operator already in it comes out unbound: the upgrade changes no
// one's access.
func TestEnsureAdminUsersTenantColumn_AddsItToAnOldTable(t *testing.T) {
	sqlDB := openAdminUsersTestDB(t)
	if _, err := sqlDB.Exec(`CREATE TABLE nucleus_admin_users (
		id TEXT PRIMARY KEY, username TEXT, email TEXT,
		password_hash TEXT, is_superuser INTEGER,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO nucleus_admin_users VALUES ('1','old','old@example.com','h',0,1,'','')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := EnsureBootstrapAdminUsersSchema(context.Background(), sqlDB, "sqlite"); err != nil {
		t.Fatalf("ensure schema on an old table: %v", err)
	}
	store := &operatorStore{db: sqlDB, table: defaultAdminUsersTable, system: "sqlite"}
	tenant, err := store.tenantOf(context.Background(), "1")
	if err != nil || tenant != "" {
		t.Fatalf("tenantOf(existing operator) = %q, %v; want unbound", tenant, err)
	}
	if _, err := store.tenantOf(context.Background(), "nobody"); err != errOperatorNotFound {
		t.Fatalf("tenantOf(unknown id) error = %v, want errOperatorNotFound", err)
	}
	records, err := store.list(context.Background())
	if err != nil || len(records) != 1 || records[0].TenantID != "" {
		t.Fatalf("list after the upgrade = %+v, %v; want the operator, unbound", records, err)
	}

	// The fallback for a dialect without a placeholder style reads the same.
	if _, err := sqlDB.Exec(`UPDATE nucleus_admin_users SET tenant_id = 'acme' WHERE id = '1'`); err != nil {
		t.Fatal(err)
	}
	unbound := &operatorStore{db: sqlDB, table: defaultAdminUsersTable, system: ""}
	if tenant, err := unbound.tenantOf(context.Background(), "1"); err != nil || tenant != "acme" {
		t.Fatalf("tenantOf without a dialect = %q, %v; want acme", tenant, err)
	}

	if err := EnsureBootstrapAdminUsersSchema(context.Background(), sqlDB, "sqlite"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

// The column is in the CREATE of every dialect, nullable, so a fresh
// install does not need the ALTER.
func TestBootstrapAdminUsersTableDDL_TenantColumn(t *testing.T) {
	for system, want := range map[string]string{
		"sqlite":     "tenant_id VARCHAR(191)\n",
		"postgresql": "tenant_id VARCHAR(191)\n",
		"mysql":      "tenant_id VARCHAR(191)\n",
		"mssql":      "tenant_id NVARCHAR(191) NULL",
		"oracle":     "tenant_id VARCHAR2(191)\n",
	} {
		if ddl := bootstrapAdminUsersTableDDL(system); !strings.Contains(ddl, want) {
			t.Errorf("%s DDL does not declare %q:\n%s", system, want, ddl)
		}
	}
}

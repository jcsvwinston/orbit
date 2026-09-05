package admin

// Audit coverage of the panel's mutating routes.
//
// TestAuditCoverage_EveryMutatingRouteLeavesAnEntry walks the registered
// routes and, for every non-GET route, requires a probe in the table below
// and asserts that firing the probe leaves exactly the expected audit entry.
// A new mutating route without a probe fails the test — the coverage cannot
// regress silently, which is how adding an RBAC policy, toggling a flag,
// applying a migration or clearing the audit log all went unrecorded before.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alicebob/miniredis/v2"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/tasks"
)

// fakeTaskInspector answers every queue operation as applied.
type fakeTaskInspector struct{}

func (fakeTaskInspector) InspectRuntime() tasks.RuntimeSnapshot {
	return tasks.RuntimeSnapshot{Enabled: true}
}

func (fakeTaskInspector) OperateQueue(queue, action string) (tasks.QueueActionResult, error) {
	return tasks.QueueActionResult{Enabled: true, Queue: queue, Action: action, Applied: true, Affected: 1}, nil
}

// auditProbeEnv is the fixture set the mutating routes need to answer 2xx:
// a storage fake, a Redis, a migrations directory, a task inspector, an RBAC
// enforcer and a session manager wrapped around the live server.
type auditProbeEnv struct {
	panel *Panel
	srv   *httptest.Server
	store *keyedStore
	sm    *auth.SessionManager
	vars  map[string]string
}

func newAuditProbeEnv(t *testing.T) *auditProbeEnv {
	t.Helper()
	authProvider := &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, authProvider)
	t.Cleanup(cleanup)

	panel.audit = newAuditStore(1000)
	store := newKeyedStore()
	panel.store = store

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	panel.rbac = enf

	redisServer := miniredis.RunT(t)
	panel.config.RedisURL = "redis://" + redisServer.Addr()
	redisServer.Set("cache:one", "1")

	migrationsDir := t.TempDir()
	panel.config.MigrationsPath = migrationsDir
	writeAdminTestFile(t, filepath.Join(migrationsDir, "20260905090000_create_audit_probe.up.sql"), `
CREATE TABLE audit_probe (id INTEGER PRIMARY KEY);
`)
	writeAdminTestFile(t, filepath.Join(migrationsDir, "20260905090000_create_audit_probe.down.sql"), `
DROP TABLE IF EXISTS audit_probe;
`)

	panel.config.TaskInspector = fakeTaskInspector{}

	sm := auth.NewSessionManager(auth.SessionConfig{})
	panel.config.Session = sm

	srv := httptest.NewServer(sm.Middleware()(panel.Handler()))
	t.Cleanup(srv.Close)

	return &auditProbeEnv{panel: panel, srv: srv, store: store, sm: sm, vars: map[string]string{}}
}

// createRecord inserts an AdminUser through the API and returns its id.
func (env *auditProbeEnv) createRecord(t *testing.T, name string) string {
	t.Helper()
	created := createAdminUser(t, env.srv.URL, map[string]any{
		"email": strings.ToLower(name) + "@example.com", "name": name, "active": true,
	})
	return fmt.Sprint(created.ID)
}

// auditProbeResult is what a probe run reports back to the common checks.
type auditProbeResult struct {
	status   int
	panel    *Panel
	beforeID uint
}

// auditProbe drives one mutating route and names the entry it must leave.
type auditProbe struct {
	name string
	// setup prepares state the request needs (a record to update, a flag
	// to delete). It may itself leave audit entries; the probe's own entry
	// is checked against the ids assigned after setup.
	setup func(t *testing.T, env *auditProbeEnv)
	// path and body build the request; run overrides both for probes that
	// need a different transport (multipart) or harness (login).
	path func(env *auditProbeEnv) string
	body func(env *auditProbeEnv) string
	run  func(t *testing.T, env *auditProbeEnv) auditProbeResult

	wantAction string
	wantOld    bool
	wantNew    bool
	wantRecord bool
	// check runs extra assertions on the newest entry.
	check func(t *testing.T, env *auditProbeEnv, newest AuditEntry)
}

func literalPath(p string) func(*auditProbeEnv) string {
	return func(*auditProbeEnv) string { return p }
}
func literalBody(b string) func(*auditProbeEnv) string {
	return func(*auditProbeEnv) string { return b }
}

func newestAuditID(p *Panel) uint {
	entries := p.audit.list(auditQueryOpts{PageSize: 1})
	if len(entries) == 0 {
		return 0
	}
	return entries[0].ID
}

// auditProbes maps "METHOD PATTERN" (as Walk reports it) to the probes that
// exercise the route. Every non-GET route of the panel must appear here.
func auditProbes() map[string][]auditProbe {
	jsonUpdateFlag := func(t *testing.T, env *auditProbeEnv) {
		env.panel.flags.set("walk_put", false, "test")
	}
	return map[string][]auditProbe{
		"PUT /api/models/{name}/schema/fields": {{
			name:       "schema.update",
			path:       literalPath("/api/models/AdminUser/schema/fields"),
			body:       literalBody(`{"fields":{"email":{"label":"E-mail address","is_filter":true}}}`),
			wantAction: "schema.update",
			wantOld:    true,
			wantNew:    true,
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if e.ModelName != "AdminUser" {
					t.Errorf("schema.update model_name = %q, want the model, not a record", e.ModelName)
				}
				after, _ := e.NewValue["email"].(map[string]any)
				if after["label"] != "E-mail address" {
					t.Errorf("schema.update new_value = %v, want the new label", e.NewValue)
				}
			},
		}},
		"POST /api/models/{name}": {{
			name:       "create",
			path:       literalPath("/api/models/AdminUser"),
			body:       literalBody(`{"email":"walk@example.com","name":"Walk","active":true}`),
			wantAction: "create",
			wantNew:    true,
			wantRecord: true,
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if e.NewValue["name"] != "Walk" {
					t.Errorf("create new_value = %v, want the created record", e.NewValue)
				}
			},
		}},
		"PUT /api/models/{name}/{id}": {{
			name:       "update",
			setup:      func(t *testing.T, env *auditProbeEnv) { env.vars["update_id"] = env.createRecord(t, "Before") },
			path:       func(env *auditProbeEnv) string { return "/api/models/AdminUser/" + env.vars["update_id"] },
			body:       literalBody(`{"name":"After"}`),
			wantAction: "update",
			wantOld:    true,
			wantNew:    true,
			wantRecord: true,
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if e.OldValue["name"] != "Before" || e.NewValue["name"] != "After" {
					t.Errorf("update old/new = %v / %v, want Before / After", e.OldValue, e.NewValue)
				}
			},
		}},
		"DELETE /api/models/{name}/{id}": {{
			name:       "delete",
			setup:      func(t *testing.T, env *auditProbeEnv) { env.vars["delete_id"] = env.createRecord(t, "Doomed") },
			path:       func(env *auditProbeEnv) string { return "/api/models/AdminUser/" + env.vars["delete_id"] },
			wantAction: "delete",
			wantOld:    true,
			wantRecord: true,
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if e.OldValue["name"] != "Doomed" {
					t.Errorf("delete old_value = %v, want the deleted record", e.OldValue)
				}
			},
		}},
		"POST /api/models/{name}/bulk": {
			{
				name: "bulk delete",
				setup: func(t *testing.T, env *auditProbeEnv) {
					env.vars["bulk_a"] = env.createRecord(t, "BulkA")
					env.vars["bulk_b"] = env.createRecord(t, "BulkB")
				},
				path: literalPath("/api/models/AdminUser/bulk"),
				body: func(env *auditProbeEnv) string {
					return fmt.Sprintf(`{"action":"delete","ids":[%s,%s]}`, env.vars["bulk_a"], env.vars["bulk_b"])
				},
				wantAction: "bulk_delete",
				wantNew:    true,
				check: func(t *testing.T, env *auditProbeEnv, e AuditEntry) {
					if got, _ := e.NewValue["deleted"].(int); got != 2 {
						t.Errorf("bulk_delete new_value = %v, want deleted=2", e.NewValue)
					}
					// Each row that went is also its own delete entry with its values.
					deletes := env.panel.audit.list(auditQueryOpts{Action: "delete", ModelName: "AdminUser", PageSize: 10})
					seen := map[string]bool{}
					for _, d := range deletes {
						if d.RecordID == env.vars["bulk_a"] || d.RecordID == env.vars["bulk_b"] {
							if d.OldValue == nil {
								t.Errorf("bulk delete of %s has no old_value", d.RecordID)
							}
							seen[d.RecordID] = true
						}
					}
					if len(seen) != 2 {
						t.Errorf("bulk delete left per-record delete entries for %v, want both ids", seen)
					}
				},
			},
			{
				name:       "bulk export",
				path:       literalPath("/api/models/AdminUser/bulk"),
				body:       literalBody(`{"action":"export","ids":[1]}`),
				wantAction: "bulk_export",
				wantNew:    true,
			},
		},
		"GET /api/models/{name}/export": {{
			name:       "export csv",
			path:       literalPath("/api/models/AdminUser/export"),
			wantAction: "export.csv",
			wantNew:    true,
		}},
		"POST /api/logout": {{
			name:       "logout",
			path:       literalPath("/api/logout"),
			wantAction: "logout",
		}},
		"DELETE /api/sessions/{token}": {{
			name: "session.terminate",
			setup: func(t *testing.T, env *auditProbeEnv) {
				if err := env.sm.SCS().Store.Commit("walk-session-token-1234567890", []byte("payload"), time.Now().Add(time.Hour)); err != nil {
					t.Fatalf("seed session: %v", err)
				}
			},
			path:       literalPath("/api/sessions/walk-session-token-1234567890"),
			wantAction: "session.terminate",
			wantRecord: true,
		}},
		"POST /api/live/excludes": {{
			name:       "live.exclude.add",
			path:       literalPath("/api/live/excludes"),
			body:       literalBody(`{"pattern":"/healthz"}`),
			wantAction: "live.exclude.add",
			wantNew:    true,
			wantRecord: true,
		}},
		"DELETE /api/live/excludes": {{
			name: "live.exclude.remove",
			setup: func(t *testing.T, env *auditProbeEnv) {
				if _, err := env.panel.addLiveExcludePattern("/metrics"); err != nil {
					t.Fatal(err)
				}
			},
			path:       literalPath("/api/live/excludes?pattern=/metrics"),
			wantAction: "live.exclude.remove",
			wantOld:    true,
			wantRecord: true,
		}},
		"POST /api/system/flags": {{
			name:       "flag.create",
			path:       literalPath("/api/system/flags"),
			body:       literalBody(`{"name":"walk_flag","enabled":true}`),
			wantAction: "flag.create",
			wantNew:    true,
			wantRecord: true,
		}},
		"PUT /api/system/flags/{name}": {{
			name:       "flag.set",
			setup:      jsonUpdateFlag,
			path:       literalPath("/api/system/flags/walk_put"),
			body:       literalBody(`{"enabled":true}`),
			wantAction: "flag.set",
			wantOld:    true,
			wantNew:    true,
			wantRecord: true,
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if e.OldValue["enabled"] != false || e.NewValue["enabled"] != true {
					t.Errorf("flag.set old/new = %v / %v, want enabled false → true", e.OldValue, e.NewValue)
				}
			},
		}},
		"DELETE /api/system/flags/{name}": {{
			name:       "flag.delete",
			setup:      jsonUpdateFlag,
			path:       literalPath("/api/system/flags/walk_put"),
			wantAction: "flag.delete",
			wantOld:    true,
			wantRecord: true,
		}},
		"PUT /api/features/{name}": {{
			name:       "flag.set via /api/features",
			setup:      jsonUpdateFlag,
			path:       literalPath("/api/features/walk_put"),
			body:       literalBody(`{"enabled":true}`),
			wantAction: "flag.set",
			wantOld:    true,
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/system/jobs/queues/{name}/actions/{action}": {{
			name:       "jobs.queue.pause",
			path:       literalPath("/api/system/jobs/queues/critical/actions/pause"),
			body:       literalBody(`{"confirm_queue":"critical","acknowledge":"I_UNDERSTAND_RUNTIME_OPERATION"}`),
			wantAction: "jobs.queue.pause",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/rbac/policies": {{
			name:       "rbac.policy.add",
			path:       literalPath("/api/rbac/policies"),
			body:       literalBody(`{"sub":"alice","obj":"admin:*","act":"read"}`),
			wantAction: "rbac.policy.add",
			wantNew:    true,
			wantRecord: true,
		}},
		"DELETE /api/rbac/policies": {{
			name: "rbac.policy.remove",
			setup: func(t *testing.T, env *auditProbeEnv) {
				if err := env.panel.rbac.AddPolicy("bob", "admin:*", "write"); err != nil {
					t.Fatal(err)
				}
			},
			path:       literalPath("/api/rbac/policies"),
			body:       literalBody(`{"sub":"bob","obj":"admin:*","act":"write"}`),
			wantAction: "rbac.policy.remove",
			wantOld:    true,
			wantRecord: true,
		}},
		"POST /api/rbac/roles/assign": {{
			name:       "rbac.role.assign",
			path:       literalPath("/api/rbac/roles/assign"),
			body:       literalBody(`{"user":"alice","role":"viewer"}`),
			wantAction: "rbac.role.assign",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/rbac/roles/remove": {{
			name: "rbac.role.remove",
			setup: func(t *testing.T, env *auditProbeEnv) {
				if err := env.panel.rbac.AddRole("carol", "editor"); err != nil {
					t.Fatal(err)
				}
			},
			path:       literalPath("/api/rbac/roles/remove"),
			body:       literalBody(`{"user":"carol","role":"editor"}`),
			wantAction: "rbac.role.remove",
			wantOld:    true,
			wantRecord: true,
		}},
		"POST /api/audit/clear": {{
			name:       "audit.clear",
			path:       literalPath("/api/audit/clear"),
			body:       literalBody(`{}`),
			wantAction: "audit.clear",
			wantNew:    true,
			check: func(t *testing.T, env *auditProbeEnv, e AuditEntry) {
				if got := env.panel.audit.count(auditQueryOpts{}); got != 1 {
					t.Errorf("after clear the ring holds %d entries, want exactly the audit.clear one", got)
				}
				if n, _ := e.NewValue["cleared"].(int); n == 0 {
					t.Errorf("audit.clear new_value = %v, want the number of entries dropped", e.NewValue)
				}
			},
		}},
		"POST /api/migrations/apply": {{
			name:       "migration.apply",
			path:       literalPath("/api/migrations/apply"),
			body:       literalBody(`{"steps":0}`),
			wantAction: "migration.apply",
			wantNew:    true,
		}},
		"POST /api/cache/flush": {{
			name:       "cache.flush",
			path:       literalPath("/api/cache/flush"),
			body:       literalBody(`{}`),
			wantAction: "cache.flush",
			wantNew:    true,
		}},
		"POST /api/exports": {{
			name:       "export.create",
			path:       literalPath("/api/exports"),
			body:       literalBody(`{"models":["AdminUser"],"format":"csv"}`),
			wantAction: "export.create",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/imports": {{
			name: "import.upload",
			run: func(t *testing.T, env *auditProbeEnv) auditProbeResult {
				before := newestAuditID(env.panel)
				resp := multipartUpload(t, env.srv.URL, "walk.csv", 64)
				resp.Body.Close()
				return auditProbeResult{status: resp.StatusCode, panel: env.panel, beforeID: before}
			},
			wantAction: "import.upload",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/import/validate": {{
			name: "import.validate",
			setup: func(t *testing.T, env *auditProbeEnv) {
				env.store.objects["_tmp/import_walk_validate.csv"] = "email,name,active\nvalid@example.com,Valid,true\n"
			},
			path:       literalPath("/api/import/validate?key=_tmp/import_walk_validate.csv"),
			body:       literalBody(`{"model":"AdminUser","format":"csv"}`),
			wantAction: "import.validate",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/import/execute": {{
			name: "import.execute",
			setup: func(t *testing.T, env *auditProbeEnv) {
				env.store.objects["_tmp/import_walk_execute.csv"] = "email,name,active\nexec@example.com,Exec,true\n"
			},
			path:       literalPath("/api/import/execute?key=_tmp/import_walk_execute.csv"),
			body:       literalBody(`{"model":"AdminUser","format":"csv"}`),
			wantAction: "import.execute",
			wantNew:    true,
			wantRecord: true,
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if got, _ := e.NewValue["imported"].(int); got != 1 {
					t.Errorf("import.execute new_value = %v, want imported=1", e.NewValue)
				}
			},
		}},
		"POST /api/fixtures/dumpdata": {{
			name:       "fixtures.dumpdata",
			path:       literalPath("/api/fixtures/dumpdata"),
			body:       literalBody(`{"models":["AdminUser"]}`),
			wantAction: "fixtures.dumpdata",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /api/fixtures/loaddata": {{
			name: "fixtures.loaddata",
			setup: func(t *testing.T, env *auditProbeEnv) {
				env.store.objects["_tmp/fixture_walk.json"] = `[{"model":"AdminUser","pk":900,"fields":{"email":"fx@example.com","name":"Fixture","active":true}}]`
			},
			path:       literalPath("/api/fixtures/loaddata"),
			body:       literalBody(`{"key":"_tmp/fixture_walk.json"}`),
			wantAction: "fixtures.loaddata",
			wantNew:    true,
			wantRecord: true,
		}},
		"POST /login": {{
			name: "login",
			run: func(t *testing.T, env *auditProbeEnv) auditProbeResult {
				h := newLoginAuditHarness(t)
				resp := h.login(t, loginHarnessUser, loginHarnessPassword)
				resp.Body.Close()
				return auditProbeResult{status: resp.StatusCode, panel: h.panel}
			},
			wantAction: "login",
			check: func(t *testing.T, _ *auditProbeEnv, e AuditEntry) {
				if e.Username != loginHarnessUser || e.UserID != "1" {
					t.Errorf("login entry actor = %q/%q, want %s/1", e.Username, e.UserID, loginHarnessUser)
				}
			},
		}},
	}
}

// runAuditProbe fires one probe and returns its result.
func runAuditProbe(t *testing.T, env *auditProbeEnv, method string, probe auditProbe) auditProbeResult {
	t.Helper()
	if probe.setup != nil {
		probe.setup(t, env)
	}
	if probe.run != nil {
		return probe.run(t, env)
	}

	before := newestAuditID(env.panel)
	var body io.Reader
	var payload string
	if probe.body != nil {
		payload = probe.body(env)
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequest(method, env.srv.URL+probe.path(env), body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: status %d body=%s", method, probe.path(env), resp.StatusCode, raw)
	}
	return auditProbeResult{status: resp.StatusCode, panel: env.panel, beforeID: before}
}

func TestAuditCoverage_EveryMutatingRouteLeavesAnEntry(t *testing.T) {
	env := newAuditProbeEnv(t)
	probes := auditProbes()

	// Walk the router: every non-GET route needs a probe.
	type route struct{ method, pattern string }
	var mutating []route
	walked := map[string]bool{}
	err := env.panel.Handler().Walk(func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + pattern
		walked[key] = true
		if method != http.MethodGet {
			mutating = append(mutating, route{method, pattern})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(mutating) == 0 {
		t.Fatal("no mutating routes discovered — route shape changed; update this guard")
	}

	var unprobed []string
	for _, rt := range mutating {
		if _, ok := probes[rt.method+" "+rt.pattern]; !ok {
			unprobed = append(unprobed, rt.method+" "+rt.pattern)
		}
	}
	if len(unprobed) > 0 {
		t.Fatalf("%d mutating route(s) have no audit probe — add one to auditProbes so the route is proven to leave an audit entry: %v", len(unprobed), unprobed)
	}
	for key := range probes {
		if !walked[key] {
			t.Errorf("probe %q names a route the router does not register (typo, or the route moved)", key)
		}
	}

	// Fire every probe (the GET ones too — an export is data leaving the
	// system) and check the entry it leaves.
	for key, list := range probes {
		method := strings.Fields(key)[0]
		for _, probe := range list {
			t.Run(key+" "+probe.name, func(t *testing.T) {
				res := runAuditProbe(t, env, method, probe)
				if res.status >= 400 {
					t.Fatalf("status %d", res.status)
				}
				newest := res.panel.audit.list(auditQueryOpts{PageSize: 1})
				if len(newest) == 0 || newest[0].ID <= res.beforeID {
					t.Fatalf("no audit entry recorded (newest before=%d, after=%v)", res.beforeID, newest)
				}
				e := newest[0]
				if e.Action != probe.wantAction {
					t.Fatalf("newest entry action = %q, want %q (entry: %+v)", e.Action, probe.wantAction, e)
				}
				if e.UserID == "" && e.Username == "" {
					t.Errorf("entry has no actor: %+v", e)
				}
				if probe.wantOld && e.OldValue == nil {
					t.Errorf("entry has no old_value: %+v", e)
				}
				if probe.wantNew && e.NewValue == nil {
					t.Errorf("entry has no new_value: %+v", e)
				}
				if probe.wantRecord && e.RecordID == "" {
					t.Errorf("entry has no record_id: %+v", e)
				}
				if probe.check != nil {
					probe.check(t, env, e)
				}
			})
		}
	}
}

// auditSecretModel is a model with credential-shaped columns, registered
// on top of the test fixture to prove the before/after values the Data
// Studio entries carry are redacted.
type auditSecretModel struct {
	model.BaseModel
	Email        string `db:"column:email;required" json:"email"`
	PasswordHash string `db:"column:password_hash" json:"password_hash"`
	APIToken     string `db:"column:api_token" json:"api_token"`
}

func (auditSecretModel) TableName() string { return "audit_secret_models" }

func TestAuditValues_DataStudioBeforeAfterAreRedacted(t *testing.T) {
	panel, srv := adminTestServer(t)
	if err := panel.registry.Register(&auditSecretModel{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	sqlDB, err := panel.db.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE audit_secret_models (
		id INTEGER PRIMARY KEY AUTOINCREMENT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
		email TEXT NOT NULL, password_hash TEXT, api_token TEXT)`); err != nil {
		t.Fatal(err)
	}

	created, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/auditSecretModel",
		map[string]any{"email": "s@example.com", "password_hash": "hash-1", "api_token": "tok-1"})
	if status != http.StatusCreated {
		t.Fatalf("create status %d body=%s", status, mustJSON(created))
	}
	id := fmt.Sprint(int(created["id"].(float64)))

	if _, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/auditSecretModel/"+id,
		map[string]any{"email": "s2@example.com", "password_hash": "hash-2"}); status != http.StatusOK {
		t.Fatalf("update status %d", status)
	}
	if _, status := doJSON(t, http.MethodDelete, srv.URL+"/api/models/auditSecretModel/"+id, nil); status != http.StatusOK {
		t.Fatalf("delete status %d", status)
	}

	entries := panel.audit.list(auditQueryOpts{ModelName: "auditSecretModel"})
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want create+update+delete", len(entries))
	}
	byAction := map[string]AuditEntry{}
	for _, e := range entries {
		byAction[e.Action] = e
	}

	assertRedacted := func(label string, values map[string]any, wantEmail string) {
		t.Helper()
		if values == nil {
			t.Fatalf("%s: values are nil, want the (redacted) record", label)
		}
		if values["email"] != wantEmail {
			t.Errorf("%s: email = %v, want %q (plain fields are kept)", label, values["email"], wantEmail)
		}
		for _, k := range []string{"password_hash", "api_token"} {
			if values[k] != redactedPlaceholder {
				t.Errorf("%s: %s = %v, want %q", label, k, values[k], redactedPlaceholder)
			}
		}
	}
	assertRedacted("create new_value", byAction["create"].NewValue, "s@example.com")
	assertRedacted("update old_value", byAction["update"].OldValue, "s@example.com")
	assertRedacted("update new_value", byAction["update"].NewValue, "s2@example.com")
	assertRedacted("delete old_value", byAction["delete"].OldValue, "s2@example.com")
	if byAction["create"].RecordID != id || byAction["update"].RecordID != id || byAction["delete"].RecordID != id {
		t.Errorf("record ids = %q/%q/%q, want %q", byAction["create"].RecordID, byAction["update"].RecordID, byAction["delete"].RecordID, id)
	}

	// The secrets never reach the wire either.
	resp, err := http.Get(srv.URL + "/api/audit?model=auditSecretModel")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, secret := range []string{"hash-1", "hash-2", "tok-1"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("/api/audit leaks %q", secret)
		}
	}
	var payload struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Entries) != 3 {
		t.Fatalf("audit payload: %v (%d entries)", err, len(payload.Entries))
	}
}

func TestBoundAuditValues_TruncatesLongStrings(t *testing.T) {
	long := strings.Repeat("€", auditValueMaxLen) // 3 bytes per rune: byte 4096 lands mid-rune
	got := boundAuditValues(map[string]any{"body": long, "short": "x", "n": 3})
	s, _ := got["body"].(string)
	if !strings.HasSuffix(s, "…[truncated]") || len(s) > auditValueMaxLen+len("…[truncated]") {
		t.Fatalf("long value not truncated: len=%d", len(s))
	}
	if !strings.HasPrefix(s, "€") || !utf8.ValidString(s) {
		t.Fatalf("truncation broke a rune: %q", s[:8])
	}
	if got["short"] != "x" || got["n"] != 3 {
		t.Fatalf("short values altered: %v", got)
	}
}

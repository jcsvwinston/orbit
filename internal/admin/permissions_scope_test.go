package admin

// Tests for A6 S2: the policy vocabulary reaches the FIELD and the ROW.
//
// Both are additions to a frozen grammar (QADR-0010 of the suite): the object
// of a policy may now name a field (admin:Model.field) or carry the #own
// qualifier (admin:Model#own), and a subject that names neither behaves
// exactly as it did. What these tests hold down is the part that cannot be
// seen from a status code — the value the refused write did NOT change, the
// row another operator cannot reach by id, and the grant the panel refuses
// instead of widening.

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	dsnucleus "github.com/jcsvwinston/orbit/internal/datasource/nucleus"
)

// OwnedNote is the model row ownership is measured on: `owner` says which
// operator a row belongs to.
type OwnedNote struct {
	model.BaseModel
	Owner  string `db:"column:owner" json:"owner" admin:"list,filter"`
	Title  string `db:"column:title;required" json:"title" admin:"list,search"`
	Secret string `db:"column:secret" json:"secret" admin:"list"`
}

func (OwnedNote) TableName() string { return "owned_notes" }

const ownedNotesDDL = `
CREATE TABLE IF NOT EXISTS owned_notes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	owner TEXT,
	title TEXT NOT NULL,
	secret TEXT
);`

// ownedPanel is a panel holding OwnedNote with two rows: id 1 owned by
// "operator" (the user operatorAuth authenticates as) and id 2 owned by
// "somebody-else".
func ownedPanel(t *testing.T, policies ...[3]string) (*Panel, *sql.DB, *httptest.Server) {
	t.Helper()

	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine: db.EngineSQL, DatabaseURL: "sqlite://:memory:", DatabaseMaxOpen: 1, DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	if err := ensureAdminUserSchema(sqlDB); err != nil {
		t.Fatalf("admin schema: %v", err)
	}
	if _, err := sqlDB.Exec(ownedNotesDDL); err != nil {
		t.Fatalf("owned_notes schema: %v", err)
	}

	registry := model.NewRegistry()
	for _, m := range []any{&AdminUser{}, &OwnedNote{}} {
		if err := registry.Register(m); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	var panel *Panel
	src := dsnucleus.New(dsnucleus.Config{
		Registry: registry,
		Resolve: func(alias string) (*db.DB, string, error) {
			h, err := panel.databaseHandle(alias)
			if err != nil {
				return nil, "", err
			}
			return h, h.System(), nil
		},
		BusConnected: func() bool { return true },
	})
	panel = NewPanel(src, logger, PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            operatorAuth(),
		SchemaRegistry:  registry,
		DatabaseHandles: map[string]*db.DB{"default": database},
		AuditEnabled:    true,
		AuditMaxSize:    100,
		RowOwnerFields:  map[string]string{"OwnedNote": "owner"},
	})

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	for _, pol := range policies {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatalf("AddPolicy %v: %v", pol, err)
		}
	}
	panel.rbac = enf

	for _, row := range [][3]string{
		{"operator", "Mine", "mine-secret"},
		{"somebody-else", "Theirs", "their-secret"},
	} {
		if _, err := sqlDB.Exec(
			`INSERT INTO owned_notes (owner, title, secret, created_at, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

// ownedTitle reads a row's title straight from the table: what the panel
// answered is one thing, what it wrote is another.
func ownedTitle(t *testing.T, sqlDB *sql.DB, id int) string {
	t.Helper()
	var title string
	if err := sqlDB.QueryRow(`SELECT title FROM owned_notes WHERE id = ?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	return title
}

func ownedOwner(t *testing.T, sqlDB *sql.DB, id int) string {
	t.Helper()
	var owner string
	if err := sqlDB.QueryRow(`SELECT owner FROM owned_notes WHERE id = ?`, id).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	return owner
}

// ---- row scope ----------------------------------------------------------

// full is every verb over the whole model; own is the same verbs confined to
// the operator's rows.
func ownGrants(subject string) [][3]string {
	var out [][3]string
	for _, act := range []string{"list", "retrieve", "create", "update", "delete", "export_csv", "bulk_delete"} {
		out = append(out, [3]string{subject, "admin:OwnedNote#own", act})
	}
	return out
}

func TestRowScope_ListCarriesOnlyTheOperatorsRows(t *testing.T) {
	_, _, srv := ownedPanel(t, ownGrants("operator")...)

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(resp))
	}
	body := mustJSON(resp)
	if !strings.Contains(body, "Mine") {
		t.Errorf("the operator cannot see their own row: %s", body)
	}
	if strings.Contains(body, "Theirs") {
		t.Errorf("another operator's row is listed: %s", body)
	}
}

// A filter the client supplies on the owner column cannot lift the scope:
// the confinement is written after the query filters, on purpose.
func TestRowScope_ClientFilterCannotWidenIt(t *testing.T) {
	_, _, srv := ownedPanel(t, ownGrants("operator")...)

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote?owner=somebody-else", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(resp))
	}
	if strings.Contains(mustJSON(resp), "Theirs") {
		t.Fatalf("?owner= widened the scope: %s", mustJSON(resp))
	}
}

func TestRowScope_AnotherOperatorsRowIsNotFoundByID(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t, ownGrants("operator")...)

	for _, tc := range []struct {
		method string
		body   map[string]any
	}{
		{http.MethodGet, nil},
		{http.MethodPut, map[string]any{"title": "taken over"}},
		{http.MethodDelete, nil},
	} {
		resp, status := doJSON(t, tc.method, srv.URL+"/api/models/OwnedNote/2", tc.body)
		if status != http.StatusNotFound {
			t.Errorf("%s row 2: status %d body=%s, want 404", tc.method, status, mustJSON(resp))
		}
	}
	if got := ownedTitle(t, sqlDB, 2); got != "Theirs" {
		t.Fatalf("row 2 title = %q after the refused writes, want Theirs", got)
	}
}

func TestRowScope_OwnRowStaysEditable(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t, ownGrants("operator")...)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1", map[string]any{"title": "Mine, edited"})
	if status != http.StatusOK {
		t.Fatalf("update own row: status %d body=%s", status, mustJSON(resp))
	}
	if got := ownedTitle(t, sqlDB, 1); got != "Mine, edited" {
		t.Fatalf("row 1 title = %q, want the edit to have landed", got)
	}
}

// A create stamps the operator; a create that names somebody else is refused
// rather than silently re-owned, so a client learns its payload was wrong.
func TestRowScope_CreateStampsTheOperatorAndRefusesAnotherOwner(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t, ownGrants("operator")...)

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote", map[string]any{"title": "Fresh"})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}
	if got := ownedOwner(t, sqlDB, 3); got != "operator" {
		t.Fatalf("the created row is owned by %q, want operator", got)
	}

	resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote",
		map[string]any{"title": "Planted", "owner": "somebody-else"})
	if status != http.StatusBadRequest {
		t.Fatalf("create for another owner: status %d body=%s, want 400", status, mustJSON(resp))
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM owned_notes WHERE title = 'Planted'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the refused create wrote %d row(s)", n)
	}
}

// An update cannot hand a row over to somebody else.
func TestRowScope_UpdateCannotChangeTheOwner(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t, ownGrants("operator")...)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1",
		map[string]any{"owner": "somebody-else"})
	if status != http.StatusBadRequest {
		t.Fatalf("handing the row over: status %d body=%s, want 400", status, mustJSON(resp))
	}
	if got := ownedOwner(t, sqlDB, 1); got != "operator" {
		t.Fatalf("row 1 owner = %q after the refused update", got)
	}
}

func TestRowScope_BulkDeleteSkipsRowsOfOthers(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t, ownGrants("operator")...)

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote/bulk",
		map[string]any{"action": "delete", "ids": []int{1, 2}})
	if status != http.StatusOK {
		t.Fatalf("bulk delete: status %d body=%s", status, mustJSON(resp))
	}
	if deleted, _ := resp["deleted"].(float64); deleted != 1 {
		t.Errorf("deleted = %v, want 1 (only the operator's row)", resp["deleted"])
	}
	if got := ownedTitle(t, sqlDB, 2); got != "Theirs" {
		t.Fatalf("another operator's row was deleted in a batch")
	}
}

func TestRowScope_ExportCarriesOnlyTheOperatorsRows(t *testing.T) {
	_, _, srv := ownedPanel(t, ownGrants("operator")...)

	body := getText(t, srv.URL+"/api/models/OwnedNote/export")
	if !strings.Contains(body, "Mine") {
		t.Errorf("the export lost the operator's own row: %s", body)
	}
	if strings.Contains(body, "Theirs") {
		t.Errorf("the export carries another operator's row: %s", body)
	}
}

// A #own grant on a model the application declared no owner column for is
// refused. The alternative — serving every row — is the failure this exists
// to prevent, and it would be invisible.
func TestRowScope_GrantWithoutAnOwnerColumnIsRefused(t *testing.T) {
	panel, _, srv := ownedPanel(t, ownGrants("operator")...)
	panel.config.RowOwnerFields = nil

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote", nil)
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(resp))
	}
	if !strings.Contains(mustJSON(resp), "own rows") {
		t.Errorf("the refusal does not say why: %s", mustJSON(resp))
	}
}

// A full grant on the model is unchanged by any of this: it sees every row.
func TestRowScope_FullGrantIsUnconfined(t *testing.T) {
	_, _, srv := ownedPanel(t, [3]string{"operator", "admin:OwnedNote", "list"})

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(resp))
	}
	if !strings.Contains(mustJSON(resp), "Theirs") {
		t.Fatalf("a full grant lost rows it always saw: %s", mustJSON(resp))
	}
}

// The owner value follows RowOwnerSubject: an application whose column holds
// the operator's id says so, and the scope compares ids.
func TestRowScope_OwnerSubjectCanBeTheID(t *testing.T) {
	panel, sqlDB, srv := ownedPanel(t, ownGrants("operator")...)
	panel.config.RowOwnerSubject = "id"
	if _, err := sqlDB.Exec(`UPDATE owned_notes SET owner = '1' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(resp))
	}
	if !strings.Contains(mustJSON(resp), "Mine") {
		t.Fatalf("the row owned by the operator's id is not listed: %s", mustJSON(resp))
	}
}

// ---- field permissions --------------------------------------------------

func TestFieldPerms_DeniedFieldIsRefusedAndUnchanged(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote", "retrieve"},
		[3]string{"operator", "admin:OwnedNote", "update"},
		[3]string{"operator", "admin:OwnedNote.secret", "deny"},
	)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1",
		map[string]any{"secret": "rewritten"})
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(resp))
	}
	if !strings.Contains(mustJSON(resp), "secret") {
		t.Errorf("the refusal does not name the field: %s", mustJSON(resp))
	}
	var got string
	if err := sqlDB.QueryRow(`SELECT secret FROM owned_notes WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "mine-secret" {
		t.Fatalf("the refused write changed the field anyway: %q", got)
	}

	// The rest of the model is still writable.
	resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1", map[string]any{"title": "Renamed"})
	if status != http.StatusOK {
		t.Fatalf("a field that was never denied: status %d body=%s", status, mustJSON(resp))
	}
}

// A denied field is not read either: not in the record, not in the list, not
// in the schema, and not in the CSV — a column an operator may not see is
// easiest of all to read off an export.
func TestFieldPerms_DeniedFieldIsNotReadable(t *testing.T) {
	_, _, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote", "retrieve"},
		[3]string{"operator", "admin:OwnedNote", "get_schema"},
		[3]string{"operator", "admin:OwnedNote", "export_csv"},
		[3]string{"operator", "admin:OwnedNote.secret", "deny"},
	)

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/1", nil)
	if status != http.StatusOK {
		t.Fatalf("get: status %d body=%s", status, mustJSON(resp))
	}
	if strings.Contains(mustJSON(resp), "mine-secret") {
		t.Errorf("the record carries a field the operator may not read: %s", mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(resp))
	}
	if strings.Contains(mustJSON(resp), "mine-secret") {
		t.Errorf("the list carries a field the operator may not read: %s", mustJSON(resp))
	}
	if body := getText(t, srv.URL+"/api/models/OwnedNote/export"); strings.Contains(body, "mine-secret") {
		t.Errorf("the CSV export carries a field the operator may not read: %s", body)
	}
	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema: status %d body=%s", status, mustJSON(resp))
	}
	if strings.Contains(mustJSON(resp), `"secret"`) {
		t.Errorf("the schema still describes a field the operator may not read: %s", mustJSON(resp))
	}
}

// An allow-list is the other shape an admin needs: "the title, and nothing
// else". It only applies to the subject that holds one — everybody else
// keeps the model-level grant they always had.
func TestFieldPerms_AllowListConfinesWritesToTheNamedFields(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote", "update"},
		[3]string{"operator", "admin:OwnedNote.title", "update"},
	)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1", map[string]any{"title": "Allowed"})
	if status != http.StatusOK {
		t.Fatalf("the allow-listed field: status %d body=%s", status, mustJSON(resp))
	}
	resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1", map[string]any{"secret": "not allowed"})
	if status != http.StatusForbidden {
		t.Fatalf("a field outside the allow-list: status %d body=%s, want 403", status, mustJSON(resp))
	}
	if got := ownedTitle(t, sqlDB, 1); got != "Allowed" {
		t.Fatalf("title = %q, want the allowed write to have landed", got)
	}
}

// A subject with no field policy at all writes what it always wrote: the
// addition narrows nobody who did not ask for it.
func TestFieldPerms_NoFieldPolicyChangesNothing(t *testing.T) {
	_, _, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote", "update"},
	)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1",
		map[string]any{"title": "Still fine", "secret": "still fine"})
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s, want the write to go through untouched", status, mustJSON(resp))
	}
}

// A field policy written for a role reaches the operator who holds the role:
// the panel asks the enforcer, so inheritance is Casbin's answer and not a
// string comparison of its own.
func TestFieldPerms_PolicyOnTheRoleApplies(t *testing.T) {
	_, sqlDB, srv := ownedPanel(t,
		[3]string{"admin", "admin:OwnedNote", "list"},
		[3]string{"admin", "admin:OwnedNote", "update"},
		[3]string{"admin", "admin:OwnedNote.secret", "deny"},
	)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1", map[string]any{"secret": "via role"})
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(resp))
	}
	var got string
	if err := sqlDB.QueryRow(`SELECT secret FROM owned_notes WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "mine-secret" {
		t.Fatalf("the refused write landed: %q", got)
	}
}

// A superuser is never narrowed: the bypass is the same one authorizeAction
// has always had.
func TestFieldPerms_SuperuserIsNotNarrowed(t *testing.T) {
	panel, _, srv := ownedPanel(t, [3]string{"root", "admin:OwnedNote.secret", "deny"})
	panel.config.Auth = superuserAuth()

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/OwnedNote/1", map[string]any{"secret": "root wrote this"})
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s, want the superuser write to go through", status, mustJSON(resp))
	}
}

// ---- capability hints ---------------------------------------------------

// The hints have to agree with the enforcer: a UI that draws itself from a
// hint the panel then refuses is worse off than one that draws every button.
func TestCapabilityHints_MatchWhatThePanelDoes(t *testing.T) {
	_, _, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote", "get_schema"},
		[3]string{"operator", "admin:OwnedNote", "update"},
		[3]string{"operator", "admin:OwnedNote.secret", "deny"},
	)

	schema, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema: status %d body=%s", status, mustJSON(schema))
	}
	perms, ok := schema["permissions"].(map[string]interface{})
	if !ok {
		t.Fatalf("the schema carries no permissions map: %s", mustJSON(schema))
	}
	if allowed, _ := perms["update"].(bool); !allowed {
		t.Errorf("the hints deny an update the operator holds: %v", perms)
	}
	if allowed, _ := perms["create"].(bool); allowed {
		t.Errorf("the hints claim a create the operator was never granted: %v", perms)
	}
	if canUpdate, _ := schema["can_update"].(bool); !canUpdate {
		t.Errorf("can_update = false for an operator granted update")
	}

	// And the refusal the hints promise is the one the panel gives.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote", map[string]any{"title": "nope"})
	if status != http.StatusForbidden {
		t.Fatalf("create: status %d body=%s, want 403 as the hints said", status, mustJSON(resp))
	}

	// A field the operator may not read is not offered as editable either:
	// it is not in the schema at all.
	fields, _ := schema["fields"].([]interface{})
	for _, raw := range fields {
		f, _ := raw.(map[string]interface{})
		if name, _ := f["name"].(string); name == "Secret" {
			t.Errorf("a denied field is still described by the schema: %v", f)
		}
		if name, _ := f["name"].(string); name == "Title" {
			if canEdit, _ := f["can_edit"].(bool); !canEdit {
				t.Errorf("Title should be editable for this operator: %v", f)
			}
		}
	}
}

// A row-scoped grant says so in the hints, so a screen can tell the person
// which rows they are looking at.
func TestCapabilityHints_NameTheRowScope(t *testing.T) {
	_, _, srv := ownedPanel(t, append(ownGrants("operator"),
		[3]string{"operator", "admin:OwnedNote", "get_schema"})...)

	schema, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema: status %d body=%s", status, mustJSON(schema))
	}
	scope, _ := schema["row_scope"].([]interface{})
	if len(scope) == 0 {
		t.Fatalf("the hints do not say the operator is confined to their own rows: %s", mustJSON(schema))
	}
	var got []string
	for _, s := range scope {
		if name, ok := s.(string); ok {
			got = append(got, name)
		}
	}
	if !strings.Contains(strings.Join(got, ","), "list") {
		t.Errorf("row_scope = %v, want it to name the confined list", got)
	}
}

// getText reads a non-JSON body (the CSV export).
func getText(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d body=%s", url, resp.StatusCode, string(body))
	}
	return string(body)
}

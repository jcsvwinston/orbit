package admin

// Tests for OR-67.
//
// The import (upload, validate, execute) and the fixture load are granted by
// import_data on admin:*, and wrote whatever the file said: any model, by
// create or by update, without the model's create or update, outside the
// operator's own rows and through a field kept from them. Whoever could
// import could write what they could not write by hand. The owner's decision
// (2026-10-06): an import writes what the record form would — the model's
// create for a new row, its update for an existing one, inside the tenant and
// the operator's own rows, and no field the operator may not write — and a
// file with one row the operator may not write is refused whole, before any
// row of it is written. The validate step refuses what the execute step
// would, and a superuser is unchanged.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// importer holds the import and nothing else: every grant a test adds is the
// one it is about.
func importer(policies ...[3]string) [][3]string {
	return append([][3]string{{"operator", "admin:*", "import_data"}}, policies...)
}

// importScopePanel is ownedPanel with a store to upload files into.
func importScopePanel(t *testing.T, policies ...[3]string) (*Panel, *sql.DB, *httptest.Server, *keyedStore) {
	t.Helper()
	panel, sqlDB, srv := ownedPanel(t, policies...)
	store := newKeyedStore()
	panel.store = store
	return panel, sqlDB, srv, store
}

// importStep runs one step of the import (validate or execute) of rows into
// model, as a JSON file the upload step parked in the store.
func importStep(t *testing.T, srv *httptest.Server, store *keyedStore, step, model string, rows []map[string]any, onConflict string) (map[string]interface{}, int) {
	t.Helper()
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("_tmp/import_%s_%d.json", step, len(store.objects))
	store.mu.Lock()
	store.objects[key] = string(b)
	store.mu.Unlock()
	return doJSON(t, http.MethodPost, srv.URL+"/api/import/"+step+"?key="+key, map[string]any{
		"model": model, "format": "json", "on_conflict": onConflict,
	})
}

// errorMessage is the message of the error a handler answered.
func errorMessage(resp map[string]interface{}) string {
	body, _ := resp["error"].(map[string]interface{})
	msg, _ := body["message"].(string)
	return msg
}

// assertImportRefused runs validate and then execute of the same file and
// expects both to refuse it with the same 403, whose message holds every one
// of wants. The caller checks that nothing was written.
func assertImportRefused(t *testing.T, srv *httptest.Server, store *keyedStore, model string, rows []map[string]any, onConflict string, wants ...string) {
	t.Helper()
	var messages []string
	for _, step := range []string{"validate", "execute"} {
		resp, status := importStep(t, srv, store, step, model, rows, onConflict)
		if status != http.StatusForbidden {
			t.Errorf("%s of %v (on_conflict=%q): status %d body=%s, want 403", step, rows, onConflict, status, mustJSON(resp))
			continue
		}
		msg := errorMessage(resp)
		for _, want := range wants {
			if !strings.Contains(msg, want) {
				t.Errorf("%s of %v: the refusal %q does not name %q", step, rows, msg, want)
			}
		}
		messages = append(messages, msg)
	}
	if len(messages) == 2 && messages[0] != messages[1] {
		t.Errorf("validate and execute refuse the same file differently:\n validate: %s\n execute:  %s", messages[0], messages[1])
	}
}

// assertImported runs validate and then execute of the same file and expects
// the first to let it through and the second to write it.
func assertImported(t *testing.T, srv *httptest.Server, store *keyedStore, model string, rows []map[string]any, onConflict string) map[string]interface{} {
	t.Helper()
	resp, status := importStep(t, srv, store, "validate", model, rows, onConflict)
	if status != http.StatusOK || resp["can_proceed"] != true {
		t.Fatalf("validate of %v: status %d body=%s, want 200 and can_proceed", rows, status, mustJSON(resp))
	}
	resp, status = importStep(t, srv, store, "execute", model, rows, onConflict)
	if status != http.StatusOK {
		t.Fatalf("execute of %v: status %d body=%s, want 200", rows, status, mustJSON(resp))
	}
	if failed, _ := resp["failed"].(float64); failed != 0 {
		t.Fatalf("execute of %v failed rows: %s", rows, mustJSON(resp))
	}
	return resp
}

// ownedNotes counts the live rows of owned_notes.
func ownedNotes(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM owned_notes WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ownedSecret(t *testing.T, sqlDB *sql.DB, id int) string {
	t.Helper()
	var secret string
	if err := sqlDB.QueryRow(`SELECT secret FROM owned_notes WHERE id = ?`, id).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

// assertUntouched checks that owned_notes holds the two rows ownedPanel
// seeds, as it seeded them.
func assertUntouched(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if n := ownedNotes(t, sqlDB); n != 2 {
		t.Errorf("a refused file wrote rows: %d notes, want the 2 seeded", n)
	}
	if got := ownedTitle(t, sqlDB, 1); got != "Mine" {
		t.Errorf("a refused file rewrote row 1: title %q", got)
	}
	if got := ownedTitle(t, sqlDB, 2); got != "Theirs" {
		t.Errorf("a refused file rewrote row 2: title %q", got)
	}
	if got := ownedSecret(t, sqlDB, 1); got != "mine-secret" {
		t.Errorf("a refused file rewrote row 1's secret: %q", got)
	}
}

// operatorRequest is the request a direct call of the import or the load
// runs for: no tenant of its own and the panel's auth provider's operator.
func operatorRequest() *router.Context {
	return &router.Context{Request: httptest.NewRequest(http.MethodPost, "/api/import/execute", nil)}
}

// isImportRefusal reports whether err is the 403 that refuses a file whole.
func isImportRefusal(err error) bool {
	var domErr *gferrors.DomainError
	return errors.As(err, &domErr) && domErr.StatusCode == http.StatusForbidden
}

// ---- the import -----------------------------------------------------------

func TestImport_WithoutCreateIsRefusedAndWritesNothing(t *testing.T) {
	_, sqlDB, srv, store := importScopePanel(t, importer()...)

	assertImportRefused(t, srv, store, "OwnedNote",
		[]map[string]any{{"title": "Imported", "owner": "operator"}}, "",
		"row 1", "OwnedNote", "create")
	assertUntouched(t, sqlDB)
}

func TestImport_UpdatingAnExistingRowNeedsUpdate(t *testing.T) {
	_, sqlDB, srv, store := importScopePanel(t, importer(
		[3]string{"operator", "admin:OwnedNote", "create"},
	)...)

	// Row 1 exists: on_conflict=update would update it, and the operator
	// may not update. Named by the column and by the Go name alike.
	for _, key := range []string{"id", "ID"} {
		assertImportRefused(t, srv, store, "OwnedNote",
			[]map[string]any{{key: 1, "title": "Overwritten"}}, "update",
			"row 1", "OwnedNote", "update")
	}
	assertUntouched(t, sqlDB)

	// Skipped, the existing row is not written, and needs no update.
	resp := assertImported(t, srv, store, "OwnedNote", []map[string]any{{"id": 1, "title": "Overwritten"}}, "skip")
	if resp["skipped"] != float64(1) {
		t.Errorf("skip of an existing row: %s, want 1 skipped", mustJSON(resp))
	}
	assertUntouched(t, sqlDB)

	// A new row is a create, which the operator holds.
	resp = assertImported(t, srv, store, "OwnedNote", []map[string]any{{"title": "Created"}}, "update")
	if resp["imported"] != float64(1) || ownedNotes(t, sqlDB) != 3 {
		t.Errorf("a create the operator holds: %s, %d notes", mustJSON(resp), ownedNotes(t, sqlDB))
	}
}

func TestImport_ARowForAnotherTenantIsRefused(t *testing.T) {
	panel, sqlDB, srv := scopedPanel(t, operatorAuth())
	store := panel.store.(*keyedStore)

	// One row naming another tenant refuses the whole file, the row for the
	// operator's own tenant with it.
	assertImportRefused(t, srv, store, "ScopedNote",
		[]map[string]any{{"title": "fine"}, {"tenant_id": "globex", "title": "smuggled"}}, "",
		"row 2", "ScopedNote", "tenant")
	// An existing row of another tenant, named by its key, is not found —
	// updated or skipped alike: a skip would confirm it exists.
	for _, mode := range []string{"update", "skip"} {
		assertImportRefused(t, srv, store, "ScopedNote",
			[]map[string]any{{"title": "fine"}, {"id": 2, "title": "hijacked"}}, mode,
			"row 2", "ScopedNote", "not found")
	}
	if noteCount(t, sqlDB, "acme") != 1 || noteCount(t, sqlDB, "globex") != 1 {
		t.Fatalf("a refused file wrote rows: acme=%d globex=%d, want 1/1", noteCount(t, sqlDB, "acme"), noteCount(t, sqlDB, "globex"))
	}
	if tenant, title := noteRow(t, sqlDB, 2); tenant != "globex" || title != "Globex note" {
		t.Fatalf("globex row = %s/%s, want untouched", tenant, title)
	}
}

func TestImport_OwnRowsOnlyForAnOwnGrant(t *testing.T) {
	_, sqlDB, srv, store := importScopePanel(t, importer(
		[3]string{"operator", "admin:OwnedNote#own", "create"},
		[3]string{"operator", "admin:OwnedNote#own", "update"},
	)...)

	// Row 2 is somebody else's: it is not one of the rows this operator
	// updates.
	assertImportRefused(t, srv, store, "OwnedNote",
		[]map[string]any{{"id": 1, "title": "Renamed"}, {"id": 2, "title": "Hijacked"}}, "update",
		"row 2", "OwnedNote", "not found")
	// A new row naming another owner is not one this operator creates, and
	// an update cannot hand their own row over.
	assertImportRefused(t, srv, store, "OwnedNote",
		[]map[string]any{{"title": "Planted", "owner": "somebody-else"}}, "",
		"row 1", "OwnedNote", "owner")
	assertImportRefused(t, srv, store, "OwnedNote",
		[]map[string]any{{"id": 1, "owner": "somebody-else"}}, "update",
		"row 1", "OwnedNote", "owner")
	assertUntouched(t, sqlDB)
	if got := ownedOwner(t, sqlDB, 1); got != "operator" {
		t.Fatalf("row 1 was handed over: owner %q", got)
	}

	// Their own row updates, and a new row is stamped as theirs — what the
	// record form does.
	resp := assertImported(t, srv, store, "OwnedNote",
		[]map[string]any{{"id": 1, "title": "Renamed"}, {"title": "Stamped"}}, "update")
	if resp["updated"] != float64(1) || resp["imported"] != float64(1) {
		t.Fatalf("own rows: %s, want 1 updated and 1 imported", mustJSON(resp))
	}
	if got := ownedTitle(t, sqlDB, 1); got != "Renamed" {
		t.Errorf("row 1 title %q, want Renamed", got)
	}
	var owner string
	if err := sqlDB.QueryRow(`SELECT owner FROM owned_notes WHERE title = 'Stamped'`).Scan(&owner); err != nil || owner != "operator" {
		t.Errorf("the created row's owner = %q (%v), want the operator stamped", owner, err)
	}
}

func TestImport_ADeniedFieldIsRefusedNotDropped(t *testing.T) {
	_, sqlDB, srv, store := importScopePanel(t, importer(
		[3]string{"operator", "admin:OwnedNote", "create"},
		[3]string{"operator", "admin:OwnedNote", "update"},
		[3]string{"operator", "admin:OwnedNote.secret", "deny"},
	)...)

	assertImportRefused(t, srv, store, "OwnedNote",
		[]map[string]any{{"title": "New", "secret": "planted"}}, "",
		"row 1", "OwnedNote", "secret")
	assertImportRefused(t, srv, store, "OwnedNote",
		[]map[string]any{{"id": 1, "secret": "leaked"}}, "update",
		"row 1", "OwnedNote", "secret")
	assertUntouched(t, sqlDB)

	// Without the field, the same rows are written.
	assertImported(t, srv, store, "OwnedNote", []map[string]any{{"title": "New"}, {"id": 1, "title": "Renamed"}}, "update")
	if ownedNotes(t, sqlDB) != 3 || ownedTitle(t, sqlDB, 1) != "Renamed" {
		t.Errorf("the import without the denied field did not land: %d notes, row 1 %q", ownedNotes(t, sqlDB), ownedTitle(t, sqlDB, 1))
	}
}

func TestImport_AMixedFileWritesNothing(t *testing.T) {
	_, sqlDB, srv, store := importScopePanel(t, importer(
		[3]string{"operator", "admin:OwnedNote", "create"},
		[3]string{"operator", "admin:OwnedNote", "update"},
		[3]string{"operator", "admin:OwnedNote.secret", "deny"},
	)...)

	// Two rows the operator may write, then one they may not: the refusal
	// comes before the first row is written, not after the second.
	assertImportRefused(t, srv, store, "OwnedNote", []map[string]any{
		{"title": "First"},
		{"id": 1, "title": "Renamed"},
		{"title": "Third", "secret": "planted"},
	}, "update", "row 3", "OwnedNote", "secret")
	assertUntouched(t, sqlDB)
}

func TestImport_SuperuserIsUnchanged(t *testing.T) {
	panel, sqlDB, srv, store := importScopePanel(t)
	panel.config.Auth = superuserAuth()

	// No policy at all: the superuser bypasses the model's grants, the row
	// scope and the field policies, as on the record form.
	resp := assertImported(t, srv, store, "OwnedNote", []map[string]any{
		{"title": "Root's", "owner": "anyone", "secret": "s"},
		{"id": 2, "title": "Theirs, renamed"},
	}, "update")
	if resp["imported"] != float64(1) || resp["updated"] != float64(1) {
		t.Fatalf("the superuser's import: %s, want 1 imported and 1 updated", mustJSON(resp))
	}
	if got := ownedTitle(t, sqlDB, 2); got != "Theirs, renamed" {
		t.Errorf("row 2 title %q", got)
	}
}

// ---- the fixture load -------------------------------------------------------

// loadStep loads records as a fixture through the panel's endpoint.
func loadStep(t *testing.T, srv *httptest.Server, store *keyedStore, onConflict string, records ...map[string]any) (map[string]interface{}, int) {
	t.Helper()
	key := fmt.Sprintf("_tmp/fixture_%d.json", len(store.objects))
	store.mu.Lock()
	store.objects[key] = fixtureJSON(t, records...)
	store.mu.Unlock()
	return doJSON(t, http.MethodPost, srv.URL+"/api/fixtures/loaddata", map[string]any{"key": key, "on_conflict": onConflict})
}

func assertLoadRefused(t *testing.T, srv *httptest.Server, store *keyedStore, onConflict string, records []map[string]any, wants ...string) {
	t.Helper()
	resp, status := loadStep(t, srv, store, onConflict, records...)
	if status != http.StatusForbidden {
		t.Errorf("loaddata of %v (on_conflict=%q): status %d body=%s, want 403", records, onConflict, status, mustJSON(resp))
		return
	}
	msg := errorMessage(resp)
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Errorf("loaddata of %v: the refusal %q does not name %q", records, msg, want)
		}
	}
}

func note(pk any, fields map[string]any) map[string]any {
	rec := map[string]any{"model": "OwnedNote", "fields": fields}
	if pk != nil {
		rec["pk"] = pk
	}
	return rec
}

func TestLoaddata_WritesWhatTheOperatorCouldWriteByHand(t *testing.T) {
	t.Run("no create", func(t *testing.T) {
		_, sqlDB, srv, store := importScopePanel(t, importer()...)
		assertLoadRefused(t, srv, store, "skip", []map[string]any{note(nil, map[string]any{"title": "Loaded"})},
			"row 1", "OwnedNote", "create")
		// A pk the store does not hold is a create too.
		assertLoadRefused(t, srv, store, "skip", []map[string]any{note(99, map[string]any{"title": "Loaded"})},
			"row 1", "OwnedNote", "create")
		assertUntouched(t, sqlDB)
	})

	t.Run("create and no update", func(t *testing.T) {
		_, sqlDB, srv, store := importScopePanel(t, importer([3]string{"operator", "admin:OwnedNote", "create"})...)
		assertLoadRefused(t, srv, store, "update", []map[string]any{
			note(nil, map[string]any{"title": "Loaded"}),
			note(1, map[string]any{"title": "Overwritten"}),
		}, "row 2", "OwnedNote", "update")
		assertUntouched(t, sqlDB)
		// Skipped, the existing row needs no update.
		resp, status := loadStep(t, srv, store, "skip", note(1, map[string]any{"title": "Overwritten"}))
		if status != http.StatusOK || resp["skipped"] != float64(1) {
			t.Errorf("skip of an existing row: status %d body=%s, want 1 skipped", status, mustJSON(resp))
		}
		assertUntouched(t, sqlDB)
	})

	t.Run("own rows", func(t *testing.T) {
		_, sqlDB, srv, store := importScopePanel(t, importer(
			[3]string{"operator", "admin:OwnedNote#own", "create"},
			[3]string{"operator", "admin:OwnedNote#own", "update"},
		)...)
		assertLoadRefused(t, srv, store, "update", []map[string]any{
			note(1, map[string]any{"title": "Renamed"}),
			note(2, map[string]any{"title": "Hijacked"}),
		}, "row 2", "OwnedNote", "not found")
		assertLoadRefused(t, srv, store, "skip", []map[string]any{note(nil, map[string]any{"title": "Planted", "owner": "somebody-else"})},
			"row 1", "OwnedNote", "owner")
		assertUntouched(t, sqlDB)

		resp, status := loadStep(t, srv, store, "update",
			note(1, map[string]any{"title": "Renamed"}),
			note(nil, map[string]any{"title": "Stamped"}))
		if status != http.StatusOK || resp["updated"] != float64(1) || resp["imported"] != float64(1) {
			t.Fatalf("own rows: status %d body=%s, want 1 updated and 1 imported", status, mustJSON(resp))
		}
		var owner string
		if err := sqlDB.QueryRow(`SELECT owner FROM owned_notes WHERE title = 'Stamped'`).Scan(&owner); err != nil || owner != "operator" {
			t.Errorf("the loaded row's owner = %q (%v), want the operator stamped", owner, err)
		}
	})

	t.Run("denied field and a mixed fixture", func(t *testing.T) {
		_, sqlDB, srv, store := importScopePanel(t, importer(
			[3]string{"operator", "admin:OwnedNote", "create"},
			[3]string{"operator", "admin:OwnedNote", "update"},
			[3]string{"operator", "admin:OwnedNote.secret", "deny"},
		)...)
		assertLoadRefused(t, srv, store, "update", []map[string]any{
			note(nil, map[string]any{"title": "First"}),
			note(1, map[string]any{"title": "Renamed"}),
			note(1, map[string]any{"secret": "leaked"}),
		}, "row 3", "OwnedNote", "secret")
		assertLoadRefused(t, srv, store, "skip", []map[string]any{note(nil, map[string]any{"title": "New", "secret": "planted"})},
			"row 1", "OwnedNote", "secret")
		assertUntouched(t, sqlDB)
	})

	t.Run("another tenant", func(t *testing.T) {
		panel, sqlDB, srv := scopedPanel(t, operatorAuth())
		store := panel.store.(*keyedStore)
		scoped := func(pk any, fields map[string]any) map[string]any {
			rec := note(pk, fields)
			rec["model"] = "ScopedNote"
			return rec
		}
		assertLoadRefused(t, srv, store, "skip", []map[string]any{
			scoped(nil, map[string]any{"title": "fine"}),
			scoped(nil, map[string]any{"tenant_id": "globex", "title": "smuggled"}),
		}, "row 2", "ScopedNote", "tenant")
		for _, mode := range []string{"update", "skip"} {
			assertLoadRefused(t, srv, store, mode, []map[string]any{
				scoped(nil, map[string]any{"title": "fine"}),
				scoped(2, map[string]any{"title": "hijacked"}),
			}, "row 2", "ScopedNote", "not found")
		}
		if noteCount(t, sqlDB, "acme") != 1 || noteCount(t, sqlDB, "globex") != 1 {
			t.Fatalf("a refused fixture wrote rows: acme=%d globex=%d", noteCount(t, sqlDB, "acme"), noteCount(t, sqlDB, "globex"))
		}
	})

	t.Run("superuser", func(t *testing.T) {
		panel, sqlDB, srv, store := importScopePanel(t)
		panel.config.Auth = superuserAuth()
		resp, status := loadStep(t, srv, store, "update",
			note(nil, map[string]any{"title": "Root's", "owner": "anyone", "secret": "s"}),
			note(2, map[string]any{"title": "Theirs, renamed"}))
		if status != http.StatusOK || resp["imported"] != float64(1) || resp["updated"] != float64(1) {
			t.Fatalf("the superuser's load: status %d body=%s, want 1 imported and 1 updated", status, mustJSON(resp))
		}
		if got := ownedTitle(t, sqlDB, 2); got != "Theirs, renamed" {
			t.Errorf("row 2 title %q", got)
		}
	})
}

// ---- what the screen is told ----------------------------------------------

// The schema's import_data is the handler's answer for that model: an import
// writes rows by create or by update, so an operator who holds neither on
// the model is refused every file of it.
func TestCapabilityHints_ImportDataNeedsAWriteOfTheModel(t *testing.T) {
	panel, _, srv, _ := importScopePanel(t, importer(
		[3]string{"operator", "admin:OwnedNote", "get_schema"},
		[3]string{"operator", "admin:OwnedNote", "list"},
	)...)
	held := func() bool {
		t.Helper()
		schema, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
		if status != http.StatusOK {
			t.Fatalf("schema: status %d body=%s", status, mustJSON(schema))
		}
		perms, _ := schema["permissions"].(map[string]interface{})
		got, _ := perms["import_data"].(bool)
		return got
	}
	if held() {
		t.Error("import_data on OwnedNote, which the operator may neither create nor update rows of")
	}
	for _, verb := range []string{"update", "create"} {
		if err := panel.rbac.AddPolicy("operator", "admin:OwnedNote#own", verb); err != nil {
			t.Fatal(err)
		}
		if !held() {
			t.Errorf("no import_data on OwnedNote with %s on the operator's own rows", verb)
		}
		if err := panel.rbac.RemovePolicy("operator", "admin:OwnedNote#own", verb); err != nil {
			t.Fatal(err)
		}
	}
}

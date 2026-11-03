package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/datasource"
)

// trustingStore is a data source that checks nothing — the shape of
// quarkdatasource before OR-78 and of an application's own source: every
// record it is handed it stores. It answers a missing row the contract's
// way (404), which is what tells the panel a mutation did not happen.
type trustingStore struct {
	mu        sync.Mutex
	rows      map[string]datasource.Record
	next      int
	creates   int
	updates   int
	createErr error
}

func (s *trustingStore) List(context.Context, datasource.Query) (datasource.Page, error) {
	return datasource.Page{}, nil
}

func (s *trustingStore) Get(_ context.Context, id string) (datasource.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.rows[id]; ok {
		return rec, nil
	}
	return nil, gferrors.NotFound("Note", id)
}

func (s *trustingStore) Create(_ context.Context, rec datasource.Record) (datasource.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.next++
	out := datasource.Record{"id": s.next}
	for k, v := range rec {
		out[k] = v
	}
	s.rows[strconv.Itoa(s.next)] = out
	return out, nil
}

func (s *trustingStore) Update(_ context.Context, id string, rec datasource.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates++
	row, ok := s.rows[id]
	if !ok {
		return gferrors.NotFound("Note", id)
	}
	for k, v := range rec {
		row[k] = v
	}
	return nil
}

func (s *trustingStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return gferrors.NotFound("Note", id)
	}
	delete(s.rows, id)
	return nil
}

func (s *trustingStore) Count(context.Context) (datasource.CountResult, error) {
	return datasource.CountResult{Present: true}, nil
}

func (s *trustingStore) TableExists(context.Context) bool { return true }

type trustingSource struct {
	info  datasource.ModelInfo
	store *trustingStore
}

func (s *trustingSource) All() []datasource.ModelInfo { return []datasource.ModelInfo{s.info} }

func (s *trustingSource) Get(name string) (datasource.ModelInfo, bool) {
	return s.info, name == s.info.Name
}

func (s *trustingSource) Store(string, string) (datasource.RecordStore, error) { return s.store, nil }

// newTrustingPanel mounts the panel over a source that validates nothing,
// with a Note model whose catalogue says what quarkdatasource says of a
// starter model: a read-only key, a required title, typed optional fields.
func newTrustingPanel(t *testing.T) (*httptest.Server, *trustingStore, *Panel) {
	t.Helper()
	st := &trustingStore{rows: map[string]datasource.Record{}}
	src := &trustingSource{store: st, info: datasource.ModelInfo{
		Name: "Note", Plural: "Notes", Table: "notes", PrimaryKey: "ID",
		Fields: []datasource.FieldInfo{
			{Name: "ID", Column: "id", GoType: "int64", IsPK: true, IsReadOnly: true, IsRequired: true},
			{Name: "Title", Column: "title", GoType: "string", IsRequired: true},
			{Name: "Views", Column: "views", GoType: "int32"},
			{Name: "Score", Column: "score", GoType: "float64"},
			{Name: "Active", Column: "active", GoType: "bool"},
			{Name: "PublishedAt", Column: "published_at", GoType: "*time.Time"},
			{Name: "Meta", Column: "meta", GoType: "quark.Nullable[string]"},
		},
	}}
	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	panel := NewPanel(src, logger, PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin", IsSuperuser: true}},
		DatabaseHandles: map[string]*db.DB{"default": database},
	})
	panel.audit = newAuditStore(100)
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return srv, st, panel
}

// problemsOf reads the per-field messages of a 422.
func problemsOf(t *testing.T, resp map[string]any, status int, what string) map[string]any {
	t.Helper()
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("%s: status %d, want 422 (body %s)", what, status, mustJSON(resp))
	}
	body, _ := resp["error"].(map[string]any)
	if body["code"] != "VALIDATION_FAILED" {
		t.Fatalf("%s: code %v, want VALIDATION_FAILED (body %s)", what, body["code"], mustJSON(resp))
	}
	details, _ := body["details"].(map[string]any)
	if len(details) == 0 {
		t.Fatalf("%s: no per-field details (body %s)", what, mustJSON(resp))
	}
	return details
}

// OR-78: the record form's promise is kept by the panel, not only by the
// source. Over a source that checks nothing, a create that leaves the
// required title out — what the SPA sends for a blank input — or sends a
// value of the wrong kind is a 422 per field in the Nucleus adapter's words,
// and the store is never asked.
func TestRecordCreate_PanelValidatesBeforeTheStore(t *testing.T) {
	srv, st, _ := newTrustingPanel(t)
	url := srv.URL + "/api/models/Note"

	cases := []struct {
		name    string
		payload map[string]any
		want    map[string]string
	}{
		{"required left out", map[string]any{"views": 3}, map[string]string{"title": "this field is required"}},
		{"required empty", map[string]any{"title": ""}, map[string]string{"title": "this field is required"}},
		{"required null, by Go name", map[string]any{"Title": nil}, map[string]string{"Title": "this field is required"}},
		{"number into text", map[string]any{"title": 5}, map[string]string{"title": "must be a string"}},
		{"text into an integer", map[string]any{"title": "t", "views": "many"}, map[string]string{"views": "must be an integer"}},
		{"fraction into an integer", map[string]any{"title": "t", "views": 1.5}, map[string]string{"views": "must be an integer"}},
		{"text into a number", map[string]any{"title": "t", "score": "high"}, map[string]string{"score": "must be a number"}},
		{"not a boolean", map[string]any{"title": "t", "active": "maybe"}, map[string]string{"active": "must be a boolean"}},
		{"not a timestamp", map[string]any{"title": "t", "published_at": "yesterday"},
			map[string]string{"published_at": "must be a timestamp (RFC 3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD)"}},
		{"every problem at once", map[string]any{"views": "x", "active": 7},
			map[string]string{"title": "this field is required", "views": "must be an integer", "active": "must be a boolean"}},
	}
	for _, tc := range cases {
		resp, status := doJSON(t, http.MethodPost, url, tc.payload)
		got := problemsOf(t, resp, status, tc.name)
		if len(got) != len(tc.want) {
			t.Errorf("%s: problems = %v, want %v", tc.name, got, tc.want)
			continue
		}
		for k, msg := range tc.want {
			if got[k] != msg {
				t.Errorf("%s: problems[%q] = %v, want %q", tc.name, k, got[k], msg)
			}
		}
	}
	if st.creates != 0 {
		t.Fatalf("the store was asked to write %d refused records", st.creates)
	}

	// What both built-in adapters read, the panel lets through: a number as
	// text, a bare date, a type the panel does not judge, the primary key
	// (the source ignores it), metadata.
	resp, status := doJSON(t, http.MethodPost, url, map[string]any{
		"id": 99, "title": "ok", "views": "7", "score": 1, "active": "on",
		"published_at": "2026-10-23", "meta": map[string]any{"V": "x", "Valid": true}, "_model": "Note",
	})
	if status != http.StatusCreated {
		t.Fatalf("valid create: status %d body %s", status, mustJSON(resp))
	}
	// A key the catalogue does not carry is the source's to judge — the
	// Nucleus adapter refuses one, quarkdatasource refuses one, a source may
	// spell a field in a way the catalogue does not say — so the panel
	// passes it on, and does not claim a required field is missing when the
	// key might be naming it.
	resp, status = doJSON(t, http.MethodPost, url, map[string]any{"heading": "a third spelling"})
	if status != http.StatusCreated {
		t.Fatalf("a key the panel cannot resolve is the source's to judge: status %d body %s", status, mustJSON(resp))
	}
	if st.creates != 2 {
		t.Fatalf("store creates = %d, want the two accepted records", st.creates)
	}
}

// An update is held to the same rules for what it names: it cannot empty a
// required field or write a value of the wrong kind, and a field it leaves
// out is left as it is.
func TestRecordUpdate_PanelValidatesBeforeTheStore(t *testing.T) {
	srv, st, _ := newTrustingPanel(t)
	st.rows["1"] = datasource.Record{"id": 1, "title": "first"}
	url := srv.URL + "/api/models/Note/1"

	for _, tc := range []struct {
		name    string
		payload map[string]any
		key     string
		msg     string
	}{
		{"empty a required field", map[string]any{"title": ""}, "title", "this field is required"},
		{"null a required field", map[string]any{"title": nil}, "title", "this field is required"},
		{"text into an integer", map[string]any{"views": "x"}, "views", "must be an integer"},
	} {
		resp, status := doJSON(t, http.MethodPut, url, tc.payload)
		if got := problemsOf(t, resp, status, tc.name); got[tc.key] != tc.msg {
			t.Errorf("%s: problems = %v, want %q: %q", tc.name, got, tc.key, tc.msg)
		}
	}
	if st.updates != 0 {
		t.Fatalf("the store was asked to apply %d refused updates", st.updates)
	}
	if resp, status := doJSON(t, http.MethodPut, url, map[string]any{"views": 3}); status != http.StatusOK {
		t.Fatalf("a partial update that leaves the title out: status %d body %s", status, mustJSON(resp))
	}
}

// A conflict the source answers (a unique key, OR-78) reaches the client as
// the source said it, not as a 500.
func TestRecordCreate_SourceConflictPassesThrough(t *testing.T) {
	srv, st, _ := newTrustingPanel(t)
	st.createErr = gferrors.Conflict("Note: a record with this title already exists")
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Note", map[string]any{"title": "dup"})
	if status != http.StatusConflict {
		t.Fatalf("status %d, want 409 (body %s)", status, mustJSON(resp))
	}
}

// OR-83: the audit trail records mutations that happened. A source that
// answers a missing row with the contract's not-found makes the update and
// the delete a 404 with no entry, and a batch delete counts only the rows
// it removed and records the batch only when it removed one.
func TestMutationsOfAMissingRow_LeaveNoAuditEntry(t *testing.T) {
	srv, st, panel := newTrustingPanel(t)
	st.rows["1"] = datasource.Record{"id": 1, "title": "real"}
	base := srv.URL + "/api/models/Note"

	if resp, status := doJSON(t, http.MethodPut, base+"/999999", map[string]any{"title": "ghost"}); status != http.StatusNotFound {
		t.Fatalf("update of a missing row: status %d body %s", status, mustJSON(resp))
	}
	if resp, status := doJSON(t, http.MethodDelete, base+"/999999", nil); status != http.StatusNotFound {
		t.Fatalf("delete of a missing row: status %d body %s", status, mustJSON(resp))
	}
	resp, status := doJSON(t, http.MethodPost, base+"/bulk", map[string]any{"action": "delete", "ids": []string{"777777", "888888"}})
	if status != http.StatusOK || resp["deleted"] != float64(0) || resp["failed"] != float64(2) {
		t.Fatalf("bulk delete of missing rows: status %d body %s, want deleted 0 failed 2", status, mustJSON(resp))
	}
	if entries := panel.audit.list(auditQueryOpts{Page: 1, PageSize: 100}); len(entries) != 0 {
		t.Fatalf("nothing happened, yet the trail holds %d entries: %s", len(entries), mustJSON(entries))
	}

	resp, status = doJSON(t, http.MethodPost, base+"/bulk", map[string]any{"action": "delete", "ids": []string{"1", "777777"}})
	if status != http.StatusOK || resp["deleted"] != float64(1) || resp["failed"] != float64(1) {
		t.Fatalf("bulk delete of one real row: status %d body %s, want deleted 1 failed 1", status, mustJSON(resp))
	}
	actions := map[string]int{}
	for _, e := range panel.audit.list(auditQueryOpts{Page: 1, PageSize: 100}) {
		actions[e.Action+" "+e.RecordID]++
	}
	if len(actions) != 2 || actions["delete 1"] != 1 || actions["bulk_delete "] != 1 {
		t.Fatalf("trail = %v, want the one delete and its batch", actions)
	}
}

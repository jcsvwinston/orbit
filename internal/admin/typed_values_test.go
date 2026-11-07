package admin

// OR-144 and the typed writes of the import and the inline form.
//
// A source that decodes a record into its model — the Quark adapter, an
// application's own — takes a value in the field's Go type and refuses a
// string where the field is a number; the Nucleus adapter used to convert
// on its own, which hid two things the panel did: it handed a store every
// cell of a CSV as text, and it stamped the parent's key into a child's
// foreign-key field as text. The same source declares its primary key
// read-only (the database assigns it), which made every row of an import
// that carried its id invalid before the store saw it.

import (
	"context"
	"fmt"
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

// typedStore is the shape of a source that decodes a record into its
// model: a value must already be of the kind the field declares — a JSON
// number for a numeric field, as both built-in adapters read one — and a
// string where the field is a number, a float or a bool is refused: the
// store converts no text. The key the record carries is ignored on a
// create, the way both built-in adapters ignore it (the database owns it).
type typedStore struct {
	info datasource.ModelInfo
	mu   sync.Mutex
	rows map[string]datasource.Record
	next int
}

func newTypedStore(info datasource.ModelInfo) *typedStore {
	return &typedStore{info: info, rows: map[string]datasource.Record{}}
}

// check refuses a value that is not of the field's declared kind.
func (s *typedStore) check(rec datasource.Record) error {
	for _, f := range s.info.Fields {
		v, ok := rec[f.Column]
		if !ok || v == nil || f.IsPK {
			continue
		}
		var fits bool
		switch f.GoType {
		case "int", "int8", "int16", "int32", "int64":
			switch n := v.(type) {
			case int, int8, int16, int32, int64:
				fits = true
			case float64:
				fits = n == float64(int64(n)) // a JSON number
			}
		case "uint", "uint8", "uint16", "uint32", "uint64":
			switch n := v.(type) {
			case uint, uint8, uint16, uint32, uint64:
				fits = true
			case int, int64:
				fits = fmt.Sprint(n)[0] != '-'
			case float64:
				fits = n >= 0 && n == float64(uint64(n))
			}
		case "float32", "float64":
			switch v.(type) {
			case float32, float64, int, int64:
				fits = true
			}
		case "bool":
			_, fits = v.(bool)
		default:
			_, fits = v.(string)
		}
		if !fits {
			return gferrors.BadRequest(fmt.Sprintf("%s.%s: a %T is not a %s (the store converts nothing)", s.info.Name, f.Column, v, f.GoType))
		}
	}
	return nil
}

func (s *typedStore) List(_ context.Context, q datasource.Query) (datasource.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []datasource.Record
	for _, rec := range s.rows {
		keep := true
		for col, want := range q.Filters {
			if fmt.Sprint(rec[col]) != fmt.Sprint(want) {
				keep = false
			}
		}
		if keep {
			items = append(items, rec)
		}
	}
	return datasource.Page{Items: items, Total: int64(len(items)), Page: 1, PageSize: len(items) + 1, TotalPages: 1}, nil
}

func (s *typedStore) Get(_ context.Context, id string) (datasource.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.rows[id]; ok {
		return rec, nil
	}
	return nil, gferrors.NotFound(s.info.Name, id)
}

func (s *typedStore) Create(_ context.Context, rec datasource.Record) (datasource.Record, error) {
	if err := s.check(rec); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	out := datasource.Record{}
	for k, v := range rec {
		out[k] = v
	}
	out["id"] = int64(s.next)
	s.rows[strconv.Itoa(s.next)] = out
	return out, nil
}

func (s *typedStore) Update(_ context.Context, id string, rec datasource.Record) error {
	if err := s.check(rec); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return gferrors.NotFound(s.info.Name, id)
	}
	for k, v := range rec {
		if k != "id" {
			row[k] = v
		}
	}
	return nil
}

func (s *typedStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return gferrors.NotFound(s.info.Name, id)
	}
	delete(s.rows, id)
	return nil
}

func (s *typedStore) Count(context.Context) (datasource.CountResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return datasource.CountResult{Count: int64(len(s.rows)), Present: true}, nil
}

func (s *typedStore) TableExists(context.Context) bool { return true }

// row is the stored record id, for a test to read what the store was handed.
func (s *typedStore) row(t *testing.T, id string) datasource.Record {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.rows[id]
	if !ok {
		t.Fatalf("%s %s: no such row (rows: %v)", s.info.Name, id, s.rows)
	}
	return rec
}

// typedSource serves one typedStore per model, in catalogue order.
type typedSource struct {
	infos  []datasource.ModelInfo
	stores map[string]*typedStore
}

func newTypedSource(infos ...datasource.ModelInfo) *typedSource {
	src := &typedSource{infos: infos, stores: map[string]*typedStore{}}
	for _, mi := range infos {
		src.stores[mi.Name] = newTypedStore(mi)
	}
	return src
}

func (s *typedSource) All() []datasource.ModelInfo { return s.infos }

func (s *typedSource) Get(name string) (datasource.ModelInfo, bool) {
	for _, mi := range s.infos {
		if mi.Name == name {
			return mi, true
		}
	}
	return datasource.ModelInfo{}, false
}

func (s *typedSource) Store(name, _ string) (datasource.RecordStore, error) {
	st, ok := s.stores[name]
	if !ok {
		return nil, fmt.Errorf("typed source: no model %q", name)
	}
	return st, nil
}

// readOnlyKey is the primary key as the Quark adapter catalogues it: the
// database assigns it, so it is read-only.
var readOnlyKey = datasource.FieldInfo{Name: "ID", Column: "id", GoType: "int64", IsPK: true, IsReadOnly: true, IsRequired: true}

// gadgetInfo is a model with one field of every kind a CSV cell can stand
// for, and a read-only version column beside the read-only key.
func gadgetInfo() datasource.ModelInfo {
	return datasource.ModelInfo{
		Name: "Gadget", Plural: "Gadgets", Table: "gadgets", PrimaryKey: "ID",
		Fields: []datasource.FieldInfo{
			readOnlyKey,
			{Name: "Name", Column: "name", GoType: "string", IsRequired: true},
			{Name: "Qty", Column: "qty", GoType: "int32"},
			{Name: "Weight", Column: "weight", GoType: "float64"},
			{Name: "Active", Column: "active", GoType: "bool"},
			{Name: "Version", Column: "version", GoType: "int64", IsReadOnly: true},
		},
	}
}

// typedPanel mounts the panel over src with a superuser, a store for the
// import's files and an in-memory audit trail.
func typedPanel(t *testing.T, src datasource.DataSource) (*httptest.Server, *keyedStore) {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine: db.EngineSQL, DatabaseURL: "sqlite://:memory:", DatabaseMaxOpen: 1, DatabaseMaxIdle: 1,
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
	store := newKeyedStore()
	panel.store = store
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return srv, store
}

// importFile parks body in the store under a key of the import's and runs
// one step of the import on it.
func importFile(t *testing.T, srv *httptest.Server, store *keyedStore, step, model, format, body, onConflict string) (map[string]interface{}, int) {
	t.Helper()
	key := fmt.Sprintf("_tmp/import_%s_%d.%s", step, len(store.objects), format)
	store.mu.Lock()
	store.objects[key] = body
	store.mu.Unlock()
	return doJSON(t, http.MethodPost, srv.URL+"/api/import/"+step+"?key="+key, map[string]any{
		"model": model, "format": format, "on_conflict": onConflict,
	})
}

// OR-144: the primary key names the row an update targets and is never
// written as a field, so a source that declares it read-only does not make
// a row that carries it invalid. Any other read-only column, and an
// excluded one, still does.
func TestImportValidation_AReadOnlyKeyDoesNotInvalidateARow(t *testing.T) {
	mi := gadgetInfo()
	mi.Fields = append(mi.Fields, datasource.FieldInfo{Name: "Secret", Column: "secret", GoType: "string", IsExcluded: true})

	if errs := ValidateImportData(mi, []map[string]interface{}{{"id": "7", "name": "seven"}}, ""); len(errs) != 0 {
		t.Fatalf("a row carrying the read-only key: %v, want no error", errs)
	}
	errs := ValidateImportData(mi, []map[string]interface{}{{"id": "7", "name": "seven", "version": "2", "secret": "x"}}, "")
	if len(errs) != 2 {
		t.Fatalf("a row carrying a read-only version and an excluded column: %v, want two errors", errs)
	}
	for _, e := range errs {
		if e.Field != "version" && e.Field != "secret" {
			t.Errorf("the error names %q, want version or secret: %v", e.Field, e)
		}
	}
}

// Through the panel: an export of the Quark adapter carries the key of
// every row, and its import under on_conflict=update is what an operator
// round-trips. The validate step lets the file through and the execute step
// writes it: the row it names is updated, the one it does not is created —
// and the key it carries is the store's to ignore.
func TestImport_AFileCarryingTheReadOnlyKeyIsWritten(t *testing.T) {
	src := newTypedSource(gadgetInfo())
	srv, store := typedPanel(t, src)
	st := src.stores["Gadget"]
	if _, err := st.Create(context.Background(), datasource.Record{"name": "first", "qty": int32(1)}); err != nil {
		t.Fatal(err)
	}

	rows := `[{"id": 1, "name": "first, renamed", "qty": 5}, {"id": 2, "name": "second", "qty": 9}]`
	resp, status := importFile(t, srv, store, "validate", "Gadget", "json", rows, "update")
	if status != http.StatusOK || resp["can_proceed"] != true {
		t.Fatalf("validate: status %d body=%s, want 200 and can_proceed", status, mustJSON(resp))
	}
	resp, status = importFile(t, srv, store, "execute", "Gadget", "json", rows, "update")
	if status != http.StatusOK {
		t.Fatalf("execute: status %d body=%s", status, mustJSON(resp))
	}
	if updated, _ := resp["updated"].(float64); updated != 1 {
		t.Errorf("updated = %v, want 1: %s", resp["updated"], mustJSON(resp))
	}
	if imported, _ := resp["imported"].(float64); imported != 1 {
		t.Errorf("imported = %v, want 1: %s", resp["imported"], mustJSON(resp))
	}
	if failed, _ := resp["failed"].(float64); failed != 0 {
		t.Errorf("failed = %v: %s", resp["failed"], mustJSON(resp))
	}
	if got := st.row(t, "1")["name"]; got != "first, renamed" {
		t.Errorf("row 1 name = %v, want the update", got)
	}
	if got := st.row(t, "2")["name"]; got != "second" {
		t.Errorf("row 2 name = %v, want the create", got)
	}
}

// Every cell of a CSV is text. The store is handed what a form would hand
// it: the number, the float and the bool parsed to the kind the field
// declares; a string column as it came. A source that converts nothing —
// the Quark adapter before OR-78, an application's own — would have refused
// every numeric column of every CSV.
func TestImport_CSVCellsReachTheStoreTyped(t *testing.T) {
	src := newTypedSource(gadgetInfo())
	srv, store := typedPanel(t, src)
	st := src.stores["Gadget"]

	csvFile := "name,qty,weight,active\nwidget,42,1.5,true\ngizmo,7,0.25,0\n"
	resp, status := importFile(t, srv, store, "execute", "Gadget", "csv", csvFile, "")
	if status != http.StatusOK {
		t.Fatalf("execute: status %d body=%s", status, mustJSON(resp))
	}
	if imported, _ := resp["imported"].(float64); imported != 2 {
		t.Fatalf("imported = %v, want 2 (the store refused a cell?): %s", resp["imported"], mustJSON(resp))
	}
	widget := st.row(t, "1")
	if got, ok := widget["qty"].(int64); !ok || got != 42 {
		t.Errorf("qty = %v (%T), want int64 42", widget["qty"], widget["qty"])
	}
	if got, ok := widget["weight"].(float64); !ok || got != 1.5 {
		t.Errorf("weight = %v (%T), want float64 1.5", widget["weight"], widget["weight"])
	}
	if got, ok := widget["active"].(bool); !ok || !got {
		t.Errorf("active = %v (%T), want true", widget["active"], widget["active"])
	}
	if got, ok := widget["name"].(string); !ok || got != "widget" {
		t.Errorf("name = %v (%T), want the string as it came", widget["name"], widget["name"])
	}
	gizmo := st.row(t, "2")
	if got, ok := gizmo["active"].(bool); !ok || got {
		t.Errorf("active = %v (%T), want false for a 0 cell", gizmo["active"], gizmo["active"])
	}

	// A cell that is not of the kind is refused by the validation, before
	// any row is written; the coercion never produces a value for it.
	resp, status = importFile(t, srv, store, "execute", "Gadget", "csv", "name,qty\nbroken,many\n", "")
	if status != http.StatusOK {
		t.Fatalf("execute of a file with a bad cell: status %d body=%s", status, mustJSON(resp))
	}
	if failed, _ := resp["failed"].(float64); failed != 1 {
		t.Errorf("a non-numeric cell in an integer column: failed = %v, want 1: %s", resp["failed"], mustJSON(resp))
	}
	if n, _ := st.Count(context.Background()); n.Count != 2 {
		t.Errorf("rows = %d after the refused file, want the 2 of before", n.Count)
	}
}

func TestCoerceImportValue(t *testing.T) {
	cases := []struct {
		goType string
		in     string
		want   any
		ok     bool
	}{
		{"int64", "42", int64(42), true},
		{"int32", "-7", int64(-7), true},
		{"int", "3", int64(3), true},
		{"int8", "300", nil, false}, // past the declared width
		{"uint", "9", uint64(9), true},
		{"uint16", "-1", nil, false},
		{"float64", "1.25", 1.25, true},
		{"float32", "2", float64(2), true},
		{"bool", "TRUE", true, true},
		{"bool", "0", false, true},
		{"bool", "yes", nil, false},
		{"string", "42", nil, false},             // text stays text
		{"*time.Time", "2026-01-02", nil, false}, // the store's to read
		{"int64", "", nil, false},
		{"int64", "4.0", nil, false},
	}
	for _, tc := range cases {
		got, ok := coerceImportValue(datasource.FieldInfo{GoType: tc.goType}, tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("coerceImportValue(%s, %q) = %v (%T), %v; want %v (%T), %v", tc.goType, tc.in, got, got, ok, tc.want, tc.want, tc.ok)
		}
	}
	// A record keeps a cell the kind does not parse, and a cell that is not
	// a string, as they came.
	mi := gadgetInfo()
	rec := coerceImportRecord(mi, map[string]interface{}{"qty": "many", "weight": 2.5, "active": "1", "name": "n", "other": "x"})
	if rec["qty"] != "many" || rec["weight"] != 2.5 || rec["active"] != true || rec["name"] != "n" || rec["other"] != "x" {
		t.Errorf("coerceImportRecord = %v", rec)
	}
}

// albumInfo and trackInfo are the parent and its child, catalogued as the
// Quark adapter catalogues a belongs_to: the child's key column is a
// numeric field, and the relation names the parent.
func albumInfo() datasource.ModelInfo {
	return datasource.ModelInfo{
		Name: "Album", Plural: "Albums", Table: "albums", PrimaryKey: "ID",
		Fields: []datasource.FieldInfo{
			readOnlyKey,
			{Name: "Title", Column: "title", GoType: "string", IsRequired: true},
		},
	}
}

func trackInfo(keyType string) datasource.ModelInfo {
	return datasource.ModelInfo{
		Name: "Track", Plural: "Tracks", Table: "tracks", PrimaryKey: "ID",
		Fields: []datasource.FieldInfo{
			readOnlyKey,
			{Name: "AlbumID", Column: "album_id", GoType: keyType, IsForeignKey: true, ForeignModel: "Album"},
			{Name: "Title", Column: "title", GoType: "string", IsRequired: true},
		},
		ForeignKeys: []datasource.ForeignKey{{FieldName: "AlbumID", Column: "album_id", ForeignModel: "Album", ForeignTable: "albums", ForeignColumn: "id"}},
	}
}

// The parent's key is stamped into the child's foreign-key field in that
// field's Go type: a number for a numeric column, whether the parent is
// being created (its key known only after its own write) or edited (its
// key in the request). A source that converts nothing refused the text
// the panel used to stamp, and the form answered that the parent was saved
// and its lines were not.
func TestInlines_ParentKeyIsStampedInTheChildFieldsType(t *testing.T) {
	for _, keyType := range []string{"int64", "uint", "float64", "string"} {
		t.Run(keyType, func(t *testing.T) {
			src := newTypedSource(albumInfo(), trackInfo(keyType))
			srv, _ := typedPanel(t, src)
			tracks := src.stores["Track"]

			// A parent created with its children.
			resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Album", map[string]any{
				"title":  "Nested",
				"tracks": []map[string]any{{"title": "One"}},
			})
			if status != http.StatusCreated {
				t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
			}
			if inlines, _ := resp["inlines"].([]interface{}); len(inlines) != 1 || inlines[0].(map[string]interface{})["created"].(float64) != 1 {
				t.Fatalf("the child was not written: %s", mustJSON(resp))
			}
			assertStamp(t, keyType, tracks.row(t, "1")["album_id"], "a child of a parent being created")

			// A parent edited with a new child, its key in the request.
			resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/Album/1", map[string]any{
				"title":  "Nested",
				"tracks": []map[string]any{{"title": "Two"}},
			})
			if status != http.StatusOK {
				t.Fatalf("update: status %d body=%s", status, mustJSON(resp))
			}
			assertStamp(t, keyType, tracks.row(t, "2")["album_id"], "a child of a parent being edited")

			// A child that names the parent itself, as the form sends a
			// line it loaded: the value is re-stamped in the field's type.
			resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/Album/1", map[string]any{
				"title":  "Nested",
				"tracks": []map[string]any{{"title": "Three", "album_id": "1"}},
			})
			if status != http.StatusOK {
				t.Fatalf("update with the key named: status %d body=%s", status, mustJSON(resp))
			}
			assertStamp(t, keyType, tracks.row(t, "3")["album_id"], "a child naming its parent as text")
		})
	}
}

// assertStamp checks that got is the parent key 1 in the Go type keyType.
func assertStamp(t *testing.T, keyType string, got any, what string) {
	t.Helper()
	var want any
	switch keyType {
	case "int64", "uint":
		// An integer field of either sign takes an int64: what both
		// built-in adapters read (the Nucleus adapter reads no uint64).
		want = int64(1)
	case "float64":
		want = float64(1)
	default:
		want = "1"
	}
	if got != want {
		t.Errorf("%s: album_id = %v (%T), want %v (%T)", what, got, got, want, want)
	}
}

package admin

// Tests for A6 S4: what a FORM needs — the candidates a foreign key may point
// at, the children edited in place, and the fields a scalar input cannot hold.
//
// The three share one harness because they share one question: whether the
// panel can be used to edit a real model, rather than a table of scalars with
// ids typed in by hand.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/datasource"
	dsnucleus "github.com/jcsvwinston/orbit/datasource/nucleus"
)

// Album is the parent, Track the child that points at it: the smallest pair
// that makes "a record with its lines" a real question.
type Album struct {
	model.BaseModel
	Title string `db:"column:title;required" json:"title" admin:"list,search"`
	Notes string `db:"column:notes" json:"notes"`
	Cover string `db:"column:cover" json:"cover"`
	Meta  string `db:"column:meta" json:"meta"`
}

func (Album) TableName() string { return "albums" }

type Track struct {
	model.BaseModel
	AlbumID uint   `json:"album_id" db:"column:album_id;fk:model=Album,table=albums,column=id"`
	Title   string `db:"column:title;required" json:"title" admin:"list,search"`
}

func (Track) TableName() string { return "tracks" }

const albumDDL = `
CREATE TABLE IF NOT EXISTS albums (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	title TEXT NOT NULL, notes TEXT, cover TEXT, meta TEXT
);
CREATE TABLE IF NOT EXISTS tracks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	album_id INTEGER,
	title TEXT NOT NULL
);`

func formsPanel(t *testing.T, tune func(*PanelConfig)) (*Panel, *sql.DB, *httptest.Server) {
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
	if _, err := sqlDB.Exec(albumDDL); err != nil {
		t.Fatalf("album schema: %v", err)
	}

	registry := model.NewRegistry()
	for _, m := range []any{&AdminUser{}, &Album{}, &Track{}} {
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
	cfg := PanelConfig{
		Prefix:          "/admin",
		Title:           "Test Admin",
		Auth:            operatorAuth(),
		SchemaRegistry:  registry,
		DatabaseHandles: map[string]*db.DB{"default": database},
		AuditEnabled:    true,
		AuditMaxSize:    200,
		AuditStore:      auditStoreMemory,
		FieldWidgets: map[string]string{
			"Album.Notes": "richtext",
			"Album.cover": "image",
			"Album.Meta":  "json",
		},
	}
	if tune != nil {
		tune(&cfg)
	}
	panel = NewPanel(src, logger, cfg)
	panel.store = newKeyedStore()

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

func albumSchema(t *testing.T, srv *httptest.Server) map[string]interface{} {
	t.Helper()
	schema, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Album/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema: status %d body=%s", status, mustJSON(schema))
	}
	return schema
}

// ---- relation lookup ----------------------------------------------------

func TestRelationLookup_AnswersCandidatesWithAReadableLabel(t *testing.T) {
	_, sqlDB, srv := formsPanel(t, nil)
	if _, err := sqlDB.Exec(`INSERT INTO albums (title, created_at, updated_at) VALUES ('Kind of Blue', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/models/Album/options", "/api/models/Track/fields/album_id/options"} {
		resp, status := doJSON(t, http.MethodGet, srv.URL+path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d body=%s", path, status, mustJSON(resp))
		}
		options, _ := resp["options"].([]interface{})
		if len(options) != 1 {
			t.Fatalf("%s: %d options, want the one album", path, len(options))
		}
		option, _ := options[0].(map[string]interface{})
		if option["value"] != "1" {
			t.Errorf("%s: value = %v, want the primary key", path, option["value"])
		}
		if option["label"] != "Kind of Blue" {
			t.Errorf("%s: label = %v, want something a person reads", path, option["label"])
		}
	}
}

func TestRelationLookup_SearchNarrowsAndLimitBounds(t *testing.T) {
	_, sqlDB, srv := formsPanel(t, nil)
	for _, title := range []string{"Blue Train", "Giant Steps", "Blue Note"} {
		if _, err := sqlDB.Exec(`INSERT INTO albums (title, created_at, updated_at) VALUES (?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, title); err != nil {
			t.Fatal(err)
		}
	}

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Album/options?q=Blue", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
	options, _ := resp["options"].([]interface{})
	if len(options) != 2 {
		t.Fatalf("?q=Blue answered %d options, want the two blue ones: %s", len(options), mustJSON(resp))
	}

	resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/Album/options?limit=1", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
	if options, _ := resp["options"].([]interface{}); len(options) != 1 {
		t.Fatalf("?limit=1 answered %d options", len(options))
	}
	if truncated, _ := resp["truncated"].(bool); !truncated {
		t.Error("a bounded page does not say there may be more")
	}
}

// Resolving what an id MEANS is reading the target, so it is the target's
// permission — not the permission of the model holding the key.
func TestRelationLookup_IsTheTargetsPermission(t *testing.T) {
	panel, sqlDB, srv := formsPanel(t, nil)
	if _, err := sqlDB.Exec(`INSERT INTO albums (title, created_at, updated_at) VALUES ('Hidden', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	// The operator may write Track — the model with the key — and may not
	// read Album.
	for _, pol := range [][3]string{
		{"operator", "admin:Track", "create"},
		{"operator", "admin:Track", "update"},
	} {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatal(err)
		}
	}
	panel.rbac = enf

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Track/fields/album_id/options", nil)
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(resp))
	}
	if strings.Contains(mustJSON(resp), "Hidden") {
		t.Fatal("the refusal leaked the candidate it refused to list")
	}
}

func TestRelationLookup_RefusesAFieldThatIsNotAKey(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Track/fields/title/options", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status %d body=%s, want 400", status, mustJSON(resp))
	}
}

// ---- inlines ------------------------------------------------------------

func TestInlines_SchemaPublishesTheChildren(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	schema := albumSchema(t, srv)
	inlines, _ := schema["inlines"].([]interface{})
	if len(inlines) != 1 {
		t.Fatalf("inlines = %v, want the one child model", schema["inlines"])
	}
	inline, _ := inlines[0].(map[string]interface{})
	if inline["model"] != "Track" || inline["key"] != "tracks" {
		t.Fatalf("inline = %v, want Track under the key \"tracks\"", inline)
	}
}

func trackTitles(t *testing.T, sqlDB *sql.DB, albumID int) []string {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT title FROM tracks WHERE album_id = ? AND deleted_at IS NULL ORDER BY id`, albumID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		out = append(out, title)
	}
	return out
}

func TestInlines_CreateWritesTheChildrenUnderTheParent(t *testing.T) {
	_, sqlDB, srv := formsPanel(t, nil)

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Album", map[string]any{
		"title": "Nested",
		"tracks": []map[string]any{
			{"title": "One"},
			{"title": "Two"},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}
	if got := trackTitles(t, sqlDB, 1); len(got) != 2 || got[0] != "One" || got[1] != "Two" {
		t.Fatalf("tracks under album 1 = %v, want the two nested children", got)
	}
	// The response says what the children did, because they are not part of
	// the parent's own record.
	inlines, _ := resp["inlines"].([]interface{})
	if len(inlines) != 1 {
		t.Fatalf("the answer does not report the children: %s", mustJSON(resp))
	}
	if created, _ := inlines[0].(map[string]interface{})["created"].(float64); created != 2 {
		t.Errorf("inline result = %v, want created 2", inlines[0])
	}
}

func TestInlines_UpdateEditsAndDeletesButNeverByOmission(t *testing.T) {
	_, sqlDB, srv := formsPanel(t, nil)
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Album", map[string]any{
		"title":  "Nested",
		"tracks": []map[string]any{{"title": "One"}, {"title": "Two"}, {"title": "Three"}},
	})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}

	// One edited, one removed, one never mentioned — and the one never
	// mentioned stays. A form that sends the lines it loaded must not delete
	// the ones it did not.
	resp, status = doJSON(t, http.MethodPut, srv.URL+"/api/models/Album/1", map[string]any{
		"tracks": []map[string]any{
			{"id": 1, "title": "One, edited"},
			{"id": 2, "_delete": true},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("update: status %d body=%s", status, mustJSON(resp))
	}
	got := trackTitles(t, sqlDB, 1)
	if len(got) != 2 || got[0] != "One, edited" || got[1] != "Three" {
		t.Fatalf("tracks = %v, want the edit applied, the deletion applied and the untouched one kept", got)
	}
}

// A child cannot be filed under another parent by naming one: the key is
// stamped from the record being edited.
func TestInlines_ChildCannotNameAnotherParent(t *testing.T) {
	_, sqlDB, srv := formsPanel(t, nil)
	for _, title := range []string{"First", "Second"} {
		if _, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Album", map[string]any{"title": title}); status != http.StatusCreated {
			t.Fatalf("create %s: status %d", title, status)
		}
	}

	if _, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/Album/1", map[string]any{
		"tracks": []map[string]any{{"title": "Planted", "album_id": 2}},
	}); status != http.StatusOK {
		t.Fatal("update failed")
	}
	if got := trackTitles(t, sqlDB, 2); len(got) != 0 {
		t.Fatalf("album 2 gained %v from a child edited under album 1", got)
	}
	if got := trackTitles(t, sqlDB, 1); len(got) != 1 || got[0] != "Planted" {
		t.Fatalf("album 1 tracks = %v, want the child stamped to it", got)
	}
}

// Writing a child needs the child's own permission, and it is checked BEFORE
// the parent is written — the datasource has no transaction, so a refusal
// discovered afterwards would leave a saved parent and a "forbidden" reply.
func TestInlines_ChildPermissionIsCheckedBeforeTheParentIsWritten(t *testing.T) {
	panel, sqlDB, srv := formsPanel(t, nil)
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, pol := range [][3]string{
		{"operator", "admin:Album", "create"},
		{"operator", "admin:Album", "list"},
	} {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatal(err)
		}
	}
	panel.rbac = enf

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Album", map[string]any{
		"title":  "Refused",
		"tracks": []map[string]any{{"title": "child"}},
	})
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(resp))
	}
	var albums int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums); err != nil {
		t.Fatal(err)
	}
	if albums != 0 {
		t.Fatalf("the refused write left %d album(s) behind", albums)
	}
}

// A payload with no children behaves exactly as it did before inlines
// existed: nothing in this changes a plain write.
func TestInlines_PlainWriteIsUnchanged(t *testing.T) {
	_, sqlDB, srv := formsPanel(t, nil)
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Album", map[string]any{"title": "Plain"})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}
	if _, ok := resp["inlines"]; ok {
		t.Errorf("a write with no children reported inlines: %s", mustJSON(resp))
	}
	var title string
	if err := sqlDB.QueryRow(`SELECT title FROM albums WHERE id = 1`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "Plain" {
		t.Fatalf("title = %q", title)
	}
}

// ---- widgets and uploads ------------------------------------------------

func TestFieldWidgets_DeclaredAndInferred(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	schema := albumSchema(t, srv)
	fields, _ := schema["fields"].([]interface{})

	widgets := map[string]string{}
	for _, raw := range fields {
		f, _ := raw.(map[string]interface{})
		name, _ := f["name"].(string)
		html, _ := f["html_type"].(string)
		widgets[name] = html
	}
	for field, want := range map[string]string{
		"Notes": widgetRichText,
		"Cover": widgetImage, // declared by column name, not by Go name
		"Meta":  widgetJSON,
		"Title": "text", // untouched
	} {
		if got := widgets[field]; got != want {
			t.Errorf("%s renders as %q, want %q", field, got, want)
		}
	}
}

func TestFieldWidgets_JSONIsInferredFromTheType(t *testing.T) {
	for goType, want := range map[string]string{
		"map[string]interface {}": widgetJSON,
		"[]string":                widgetJSON,
		"json.RawMessage":         widgetJSON,
		"string":                  "",
		"[]byte":                  "",
		"int":                     "",
	} {
		if got := inferredWidget(datasourceField(goType)); got != want {
			t.Errorf("%s infers %q, want %q", goType, got, want)
		}
	}
}

func uploadTo(t *testing.T, srv *httptest.Server, model, field, filename string) (map[string]interface{}, int) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	header.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("the bytes of a cover"))
	if field != "" {
		if err := mw.WriteField("field", field); err != nil {
			t.Fatal(err)
		}
	}
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/models/"+model+"/upload", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return payload, resp.StatusCode
}

func TestFieldUpload_StoresTheFileAndAnswersTheKey(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)

	payload, status := uploadTo(t, srv, "Album", "cover", "../../etc/passwd")
	if status != http.StatusCreated {
		t.Fatalf("upload: status %d body=%s", status, mustJSON(payload))
	}
	key, _ := payload["key"].(string)
	if !strings.HasPrefix(key, "admin/uploads/album/") {
		t.Fatalf("key = %q, want it under the panel's own prefix", key)
	}
	// The client-supplied name is untrusted: no directory of it survives.
	if strings.Contains(key, "..") || strings.Contains(strings.TrimPrefix(key, "admin/uploads/album/"), "/") {
		t.Fatalf("key = %q, want the traversal stripped", key)
	}
	store, ok := panel.store.(*keyedStore)
	if !ok {
		t.Fatalf("unexpected store %T", panel.store)
	}
	if _, ok := store.objects[key]; !ok {
		t.Fatalf("the key is not in storage: %v", store.objects)
	}
}

func TestFieldUpload_RefusesAFieldThatDoesNotHoldAFile(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	payload, status := uploadTo(t, srv, "Album", "title", "cover.png")
	if status != http.StatusBadRequest {
		t.Fatalf("status %d body=%s, want 400", status, mustJSON(payload))
	}
}

func TestFieldUpload_NeedsPermissionToWriteTheModel(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := enf.AddPolicy("operator", "admin:Album", "list"); err != nil {
		t.Fatal(err)
	}
	panel.rbac = enf

	payload, status := uploadTo(t, srv, "Album", "cover", "cover.png")
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(payload))
	}
}

// datasourceField is a FieldInfo with just the type set, for the inference
// table above.
func datasourceField(goType string) datasource.FieldInfo {
	return datasource.FieldInfo{GoType: goType}
}

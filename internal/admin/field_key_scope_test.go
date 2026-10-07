package admin

// A field policy (admin:Model.field) resolved the field it names, and the
// keys of the records it masks and of the payloads it guards, by the field's
// column and Go name only. The Nucleus adapter emits a record keyed by each
// field's json tag and accepts a payload under the same key, so a field
// whose json tag is a third spelling — neither the column nor the Go name —
// was known to the backend and unknown to the policy: a deny did not mask it
// on the record, the list, the export, the history or the trail, and a
// write naming it was written where the same write under the column was
// refused. The rules now resolve a key the way the adapter does: column and
// Go name first, the json key after (fieldRules.resolve, Panel.fieldJSONKeys).

import (
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	dsnucleus "github.com/jcsvwinston/orbit/datasource/nucleus"
)

// TaggedNote is a model with a field spelled three ways: column secret_note,
// Go name Secret, json key hidden. Its records carry "hidden".
type TaggedNote struct {
	model.BaseModel
	Title  string `db:"column:title;required" json:"title" admin:"list,search"`
	Secret string `db:"column:secret_note" json:"hidden" admin:"list"`
}

func (TaggedNote) TableName() string { return "tagged_notes" }

const taggedNotesDDL = `
CREATE TABLE IF NOT EXISTS tagged_notes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	title TEXT NOT NULL,
	secret_note TEXT
);`

// taggedPanel is a panel holding TaggedNote with one row, id 1, whose
// secret is "mine-secret"; the operator operatorAuth authenticates as holds
// policies.
func taggedPanel(t *testing.T, policies ...[3]string) (*Panel, *sql.DB, *httptest.Server) {
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
	if _, err := sqlDB.Exec(taggedNotesDDL); err != nil {
		t.Fatalf("tagged_notes schema: %v", err)
	}

	registry := model.NewRegistry()
	for _, m := range []any{&AdminUser{}, &TaggedNote{}} {
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

	if _, err := sqlDB.Exec(
		`INSERT INTO tagged_notes (title, secret_note, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		"Mine", "mine-secret"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

func taggedSecret(t *testing.T, sqlDB *sql.DB, id int) string {
	t.Helper()
	var secret string
	if err := sqlDB.QueryRow(`SELECT secret_note FROM tagged_notes WHERE id = ?`, id).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

// taggedGrants is every verb the tests below read and write with, over the
// whole model, plus the trail.
func taggedGrants() [][3]string {
	out := [][3]string{{"operator", "admin:*", "audit_view"}}
	for _, act := range []string{"list", "retrieve", "get_schema", "create", "update", "export_csv"} {
		out = append(out, [3]string{"operator", "admin:TaggedNote", act})
	}
	return out
}

// A denied field is masked under the key the records carry it by, on every
// surface that shows a row: the record, the list, the CSV export, the
// record's history and the trail — each of which showed "hidden" when the
// deny named secret_note.
func TestFieldPerms_DenyMasksTheFieldUnderItsJSONKey(t *testing.T) {
	for _, spelling := range []string{"secret_note", "Secret", "hidden"} {
		t.Run("policy names "+spelling, func(t *testing.T) {
			_, _, srv := taggedPanel(t, append(taggedGrants(),
				[3]string{"operator", "admin:TaggedNote." + spelling, "deny"})...)

			// A write the operator may make, so the history and the trail
			// hold an entry whose before and after carry the row as the
			// adapter emits it — "hidden" included.
			if resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/TaggedNote/1",
				map[string]any{"title": "Mine, renamed"}); status != http.StatusOK {
				t.Fatalf("a field that was never denied: status %d body=%s", status, mustJSON(resp))
			}

			for name, url := range map[string]string{
				"record":  srv.URL + "/api/models/TaggedNote/1",
				"list":    srv.URL + "/api/models/TaggedNote",
				"history": srv.URL + "/api/models/TaggedNote/1/history",
				"trail":   srv.URL + "/api/audit?page_size=200&model=TaggedNote",
			} {
				resp, status := doJSON(t, http.MethodGet, url, nil)
				if status != http.StatusOK {
					t.Fatalf("%s: status %d body=%s", name, status, mustJSON(resp))
				}
				body := mustJSON(resp)
				if strings.Contains(body, "mine-secret") || strings.Contains(body, `"hidden"`) {
					t.Errorf("the %s carries a field the operator may not read: %s", name, body)
				}
				if !strings.Contains(body, "Mine") {
					t.Errorf("the %s lost the fields the operator reads: %s", name, body)
				}
			}
			for name, url := range map[string]string{
				"CSV export":  srv.URL + "/api/models/TaggedNote/export",
				"trail's CSV": srv.URL + "/api/audit?format=csv",
			} {
				if body := getText(t, url); strings.Contains(body, "mine-secret") {
					t.Errorf("the %s carries a field the operator may not read: %s", name, body)
				}
			}
			resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/TaggedNote/schema", nil)
			if status != http.StatusOK {
				t.Fatalf("schema: status %d body=%s", status, mustJSON(resp))
			}
			if body := mustJSON(resp); strings.Contains(body, `"secret_note"`) {
				t.Errorf("the schema still describes a field the operator may not read: %s", body)
			}
		})
	}
}

// A write naming a denied field under its json key is refused like one
// naming it under its column — a 403 naming the field, nothing written —
// on a create and on an update. Before, the update answered 200 and wrote.
func TestFieldPerms_DenyRefusesAWriteUnderAnyKey(t *testing.T) {
	_, sqlDB, srv := taggedPanel(t, append(taggedGrants(),
		[3]string{"operator", "admin:TaggedNote.secret_note", "deny"})...)

	for _, key := range []string{"hidden", "Hidden", "Secret", "secret_note"} {
		resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/TaggedNote/1",
			map[string]any{key: "rewritten"})
		if status != http.StatusForbidden {
			t.Fatalf("update under %q: status %d body=%s, want 403", key, status, mustJSON(resp))
		}
		if !strings.Contains(mustJSON(resp), "secret_note") {
			t.Errorf("the refusal under %q does not name the field: %s", key, mustJSON(resp))
		}
		if got := taggedSecret(t, sqlDB, 1); got != "mine-secret" {
			t.Fatalf("the refused write under %q changed the field anyway: %q", key, got)
		}

		resp, status = doJSON(t, http.MethodPost, srv.URL+"/api/models/TaggedNote",
			map[string]any{"title": "New", key: "planted"})
		if status != http.StatusForbidden {
			t.Fatalf("create under %q: status %d body=%s, want 403", key, status, mustJSON(resp))
		}
	}
	var rows int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM tagged_notes`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("%d rows, want the refused creates to have written none", rows)
	}

	// The rest of the model is still writable, and the json key of an
	// allowed field is still accepted.
	if resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/TaggedNote/1",
		map[string]any{"title": "Renamed"}); status != http.StatusOK {
		t.Fatalf("a field that was never denied: status %d body=%s", status, mustJSON(resp))
	}
}

// An allow-list confines a write whichever key names the field outside it.
func TestFieldPerms_AllowListHoldsUnderTheJSONKey(t *testing.T) {
	_, sqlDB, srv := taggedPanel(t,
		[3]string{"operator", "admin:TaggedNote", "update"},
		[3]string{"operator", "admin:TaggedNote.title", "update"},
	)

	for _, key := range []string{"hidden", "secret_note"} {
		resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/TaggedNote/1",
			map[string]any{key: "not allowed"})
		if status != http.StatusForbidden {
			t.Fatalf("a field outside the allow-list, under %q: status %d body=%s, want 403", key, status, mustJSON(resp))
		}
	}
	if got := taggedSecret(t, sqlDB, 1); got != "mine-secret" {
		t.Fatalf("the refused write changed the field anyway: %q", got)
	}
	if resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/models/TaggedNote/1",
		map[string]any{"title": "Allowed"}); status != http.StatusOK {
		t.Fatalf("the allow-listed field: status %d body=%s", status, mustJSON(resp))
	}
}

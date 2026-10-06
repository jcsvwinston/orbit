package admin

// Tests for OR-72.
//
// The record form carries the children of a record in the parent's payload
// (inlines.go). Each child was written with the child model's verb checked
// and nothing else: not the request's tenant, not an #own grant, not the
// field policies — and a child named by id was any row of the child model,
// not one of the record being edited. Every child now asks what the child's
// own form would ask (requestWriteScope), a child named by id has to be one
// of the parent's, and one child refused refuses the whole save before
// anything is written. The last rule is integrity, not authorization: it
// holds for a superuser too.

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

// Shelf is the parent and Book its child. Book carries everything a write
// scope confines: a tenant, an owner and a field a policy can keep.
type Shelf struct {
	model.BaseModel
	TenantID string `db:"column:tenant_id" json:"tenant_id"`
	Name     string `db:"column:name;required" json:"name" admin:"list,search"`
}

func (Shelf) TableName() string { return "shelves" }

type Book struct {
	model.BaseModel
	TenantID string `db:"column:tenant_id" json:"tenant_id"`
	Owner    string `db:"column:owner" json:"owner"`
	ShelfID  uint   `json:"shelf_id" db:"column:shelf_id;fk:model=Shelf,table=shelves,column=id"`
	Title    string `db:"column:title;required" json:"title" admin:"list,search"`
	Secret   string `db:"column:secret" json:"secret"`
}

func (Book) TableName() string { return "books" }

const shelvesDDL = `
CREATE TABLE IF NOT EXISTS shelves (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	tenant_id TEXT,
	name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS books (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
	tenant_id TEXT,
	owner TEXT,
	shelf_id INTEGER,
	title TEXT NOT NULL,
	secret TEXT
);`

// inlineScopePanel is a panel holding Shelf and Book, seeded with:
//
//	shelf 1 "Mine"   acme      books 1 "One"   (operator)      and 2 "Two" (somebody-else)
//	shelf 2 "Theirs" acme      books 3 "Three" (operator)      and 4 "Four" (operator)
//	shelf 3 "Away"   globex    —
//
// and book 5 "Stray", a globex row filed under shelf 1. Policies, when
// given, are enforced through an RBAC enforcer; without them the auth
// provider allows everything.
func inlineScopePanel(t *testing.T, adminAuth AdminAuth, tune func(*PanelConfig), policies ...[3]string) (*Panel, *sql.DB, *httptest.Server) {
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
	if _, err := sqlDB.Exec(shelvesDDL); err != nil {
		t.Fatalf("shelves schema: %v", err)
	}

	registry := model.NewRegistry()
	for _, m := range []any{&AdminUser{}, &Shelf{}, &Book{}} {
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
		Auth:            adminAuth,
		SchemaRegistry:  registry,
		DatabaseHandles: map[string]*db.DB{"default": database},
		AuditEnabled:    true,
		AuditMaxSize:    200,
		AuditStore:      auditStoreMemory,
		RowOwnerFields:  map[string]string{"Book": "owner"},
	}
	if tune != nil {
		tune(&cfg)
	}
	panel = NewPanel(src, logger, cfg)

	if len(policies) > 0 {
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
	}

	for _, row := range [][2]string{{"acme", "Mine"}, {"acme", "Theirs"}, {"globex", "Away"}} {
		if _, err := sqlDB.Exec(`INSERT INTO shelves (tenant_id, name, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		tenant, owner string
		shelf         int
		title         string
	}{
		{"acme", "operator", 1, "One"},
		{"acme", "somebody-else", 1, "Two"},
		{"acme", "operator", 2, "Three"},
		{"acme", "operator", 2, "Four"},
		{"globex", "operator", 1, "Stray"},
	} {
		if _, err := sqlDB.Exec(`INSERT INTO books (tenant_id, owner, shelf_id, title, secret, created_at, updated_at) VALUES (?, ?, ?, ?, 'sealed', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			row.tenant, row.owner, row.shelf, row.title); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, sqlDB, srv
}

// acmeTenant confines every request of the panel to acme, the way a host
// that resolves the tenant per request does.
func acmeTenant(cfg *PanelConfig) {
	cfg.MultiTenantEnabled = true
	cfg.MultiTenantAutoFilter = true
	cfg.TenantResolver = func(*http.Request) (string, bool) { return "acme", true }
}

// book reads one book straight from the table, deleted or not.
type bookRow struct {
	tenant, owner, title, secret string
	shelf                        int
	deleted                      bool
}

func readBook(t *testing.T, sqlDB *sql.DB, id int) bookRow {
	t.Helper()
	var (
		b       bookRow
		deleted sql.NullString
	)
	if err := sqlDB.QueryRow(`SELECT tenant_id, owner, shelf_id, title, secret, deleted_at FROM books WHERE id = ?`, id).
		Scan(&b.tenant, &b.owner, &b.shelf, &b.title, &b.secret, &deleted); err != nil {
		t.Fatalf("book %d: %v", id, err)
	}
	b.deleted = deleted.Valid
	return b
}

func bookCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM books WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func shelfName(t *testing.T, sqlDB *sql.DB, id int) string {
	t.Helper()
	var name string
	if err := sqlDB.QueryRow(`SELECT name FROM shelves WHERE id = ?`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func shelfCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM shelves WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// assertBooksUntouched checks that the seeded books are as they were seeded
// and that no book was added.
func assertBooksUntouched(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	want := map[int]bookRow{
		1: {tenant: "acme", owner: "operator", shelf: 1, title: "One", secret: "sealed"},
		2: {tenant: "acme", owner: "somebody-else", shelf: 1, title: "Two", secret: "sealed"},
		3: {tenant: "acme", owner: "operator", shelf: 2, title: "Three", secret: "sealed"},
		4: {tenant: "acme", owner: "operator", shelf: 2, title: "Four", secret: "sealed"},
		5: {tenant: "globex", owner: "operator", shelf: 1, title: "Stray", secret: "sealed"},
	}
	for id, w := range want {
		if got := readBook(t, sqlDB, id); got != w {
			t.Errorf("book %d = %+v, want it untouched: %+v", id, got, w)
		}
	}
	if n := bookCount(t, sqlDB); n != len(want) {
		t.Errorf("%d live books, want the %d seeded", n, len(want))
	}
}

func putShelf(t *testing.T, srv *httptest.Server, id string, payload map[string]any) (map[string]interface{}, int) {
	t.Helper()
	return doJSON(t, http.MethodPut, srv.URL+"/api/models/Shelf/"+id, payload)
}

// ---- the child belongs to the record being edited ----------------------

// A child named by id is one of the parent's, or it is not found — for
// every operator, a superuser included: which record a child belongs to is
// integrity, not a grant.
func TestInlineScope_AChildOfAnotherRecordIsNotFound(t *testing.T) {
	for name, adminAuth := range map[string]AdminAuth{"operator": operatorAuth(), "superuser": superuserAuth()} {
		t.Run(name, func(t *testing.T) {
			_, sqlDB, srv := inlineScopePanel(t, adminAuth, nil)

			for _, child := range []map[string]any{
				{"id": 3, "title": "Taken"},   // an edit of shelf 2's book
				{"id": "3", "title": "Taken"}, // the same id as text
				{"id": 4, "_delete": true},    // a deletion of shelf 2's book
				{"id": 99, "title": "Nowhere"},
			} {
				resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{child}})
				if status != http.StatusNotFound {
					t.Errorf("child %v of shelf 1: status %d body=%s, want 404", child, status, mustJSON(resp))
				}
			}
			assertBooksUntouched(t, sqlDB)

			// The same edits of shelf 2, whose books they are, land.
			if resp, status := putShelf(t, srv, "2", map[string]any{"books": []map[string]any{
				{"id": 3, "title": "Three, edited"}, {"id": 4, "_delete": true},
			}}); status != http.StatusOK {
				t.Fatalf("shelf 2's own books: status %d body=%s", status, mustJSON(resp))
			}
			if got := readBook(t, sqlDB, 3); got.title != "Three, edited" || got.shelf != 2 {
				t.Errorf("book 3 = %+v, want edited and still on shelf 2", got)
			}
			if got := readBook(t, sqlDB, 4); !got.deleted {
				t.Errorf("book 4 = %+v, want deleted", got)
			}
		})
	}
}

// A record being created has no children yet, so a child it names by id is
// not one of its own.
func TestInlineScope_ANewRecordHasNoChildrenToEditOrDelete(t *testing.T) {
	_, sqlDB, srv := inlineScopePanel(t, superuserAuth(), nil)

	for _, child := range []map[string]any{
		{"id": 1, "title": "Moved"},
		{"id": 2, "_delete": true},
	} {
		resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Shelf", map[string]any{
			"name": "New", "books": []map[string]any{child},
		})
		if status != http.StatusNotFound {
			t.Errorf("new shelf with child %v: status %d body=%s, want 404", child, status, mustJSON(resp))
		}
	}
	assertBooksUntouched(t, sqlDB)
	if n := shelfCount(t, sqlDB); n != 3 {
		t.Errorf("a refused create left a shelf behind: %d shelves", n)
	}
}

// The key to the parent is the record being edited: a child that names
// another one, under any spelling the backend accepts, is refused — not
// moved, and not quietly re-stamped.
func TestInlineScope_AChildIsNotReparented(t *testing.T) {
	for name, adminAuth := range map[string]AdminAuth{"operator": operatorAuth(), "superuser": superuserAuth()} {
		t.Run(name, func(t *testing.T) {
			_, sqlDB, srv := inlineScopePanel(t, adminAuth, nil)

			for _, child := range []map[string]any{
				{"id": 1, "shelf_id": 2},
				{"id": 1, "ShelfID": 2},
				{"id": 1, "SHELF_ID": "2"},
				{"id": 1, "shelf_id": nil},
				{"title": "Elsewhere", "shelf_id": 2},
				{"title": "Elsewhere", "ShelfID": 2},
				{"title": "Twice", "shelf_id": 1, "ShelfID": 1},
			} {
				resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{child}})
				if status != http.StatusBadRequest {
					t.Errorf("child %v of shelf 1: status %d body=%s, want 400", child, status, mustJSON(resp))
				}
			}
			assertBooksUntouched(t, sqlDB)

			// Naming the record being edited is not a move.
			resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{
				{"id": 1, "shelf_id": 1, "title": "One, edited"},
				{"title": "Added", "ShelfID": "1"},
			}})
			if status != http.StatusOK {
				t.Fatalf("children naming their own shelf: status %d body=%s", status, mustJSON(resp))
			}
			if got := readBook(t, sqlDB, 1); got.title != "One, edited" || got.shelf != 1 {
				t.Errorf("book 1 = %+v", got)
			}
			if got := readBook(t, sqlDB, 6); got.title != "Added" || got.shelf != 1 {
				t.Errorf("book 6 = %+v, want added to shelf 1", got)
			}
		})
	}
}

// ---- the child model's own write scope ---------------------------------

// shelfEditor may update shelves and holds the given grants on Book.
func shelfEditor(book ...[3]string) [][3]string {
	return append([][3]string{
		{"operator", "admin:Shelf", "retrieve"},
		{"operator", "admin:Shelf", "create"},
		{"operator", "admin:Shelf", "update"},
	}, book...)
}

func fullBookGrants() [][3]string {
	return [][3]string{
		{"operator", "admin:Book", "create"},
		{"operator", "admin:Book", "update"},
		{"operator", "admin:Book", "delete"},
	}
}

// A field the operator may not write is refused by name on a child as on
// the child's own form, and nothing of the save is written.
func TestInlineScope_AChildFieldTheOperatorMayNotWriteIsRefused(t *testing.T) {
	_, sqlDB, srv := inlineScopePanel(t, operatorAuth(), nil,
		shelfEditor(append(fullBookGrants(), [3]string{"operator", "admin:Book.secret", "deny"})...)...)

	for _, child := range []map[string]any{
		{"id": 1, "secret": "leaked"},
		{"id": 1, "Secret": "leaked"},
		{"title": "New", "secret": "leaked"},
	} {
		resp, status := putShelf(t, srv, "1", map[string]any{"name": "Renamed", "books": []map[string]any{child}})
		if status != http.StatusForbidden {
			t.Errorf("child %v: status %d body=%s, want 403", child, status, mustJSON(resp))
			continue
		}
		if msg := errorMessage(resp); !strings.Contains(msg, "secret") || !strings.Contains(msg, "books[0]") {
			t.Errorf("child %v: the refusal %q does not name the field and the child", child, msg)
		}
	}
	assertBooksUntouched(t, sqlDB)
	if got := shelfName(t, sqlDB, 1); got != "Mine" {
		t.Errorf("a refused save renamed the shelf: %q", got)
	}

	// The fields the operator does hold are written.
	if resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{
		{"id": 1, "title": "One, edited"}, {"title": "Added"},
	}}); status != http.StatusOK {
		t.Fatalf("children without the denied field: status %d body=%s", status, mustJSON(resp))
	}
	if got := readBook(t, sqlDB, 1); got.title != "One, edited" || got.secret != "sealed" {
		t.Errorf("book 1 = %+v", got)
	}
}

// Under an #own grant the children the operator edits or deletes are their
// own, and a child they add is stamped as theirs.
func TestInlineScope_AnOwnGrantReachesOnlyTheOperatorsChildren(t *testing.T) {
	_, sqlDB, srv := inlineScopePanel(t, operatorAuth(), nil, shelfEditor(
		[3]string{"operator", "admin:Book#own", "create"},
		[3]string{"operator", "admin:Book#own", "update"},
		[3]string{"operator", "admin:Book#own", "delete"},
	)...)

	for _, child := range []map[string]any{
		{"id": 2, "title": "Taken"},
		{"id": 2, "_delete": true},
	} {
		resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{child}})
		if status != http.StatusNotFound {
			t.Errorf("somebody else's child %v: status %d body=%s, want 404", child, status, mustJSON(resp))
		}
	}
	resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{
		{"title": "Handed over", "owner": "somebody-else"},
	}})
	if status != http.StatusBadRequest {
		t.Errorf("a child created for somebody else: status %d body=%s, want 400", status, mustJSON(resp))
	}
	assertBooksUntouched(t, sqlDB)

	resp, status = putShelf(t, srv, "1", map[string]any{"books": []map[string]any{
		{"id": 1, "title": "One, edited"}, {"title": "Mine too"},
	}})
	if status != http.StatusOK {
		t.Fatalf("the operator's own children: status %d body=%s", status, mustJSON(resp))
	}
	if got := readBook(t, sqlDB, 1); got.title != "One, edited" {
		t.Errorf("book 1 = %+v", got)
	}
	if got := readBook(t, sqlDB, 6); got.owner != "operator" || got.shelf != 1 {
		t.Errorf("the added child = %+v, want it stamped as the operator's, on shelf 1", got)
	}
}

// A scoped request reaches the children of its own tenant only, and a child
// it adds is stamped with that tenant — a superuser's request included, as
// on the child's own form.
func TestInlineScope_AChildStaysInTheRequestsTenant(t *testing.T) {
	for name, adminAuth := range map[string]AdminAuth{"operator": operatorAuth(), "superuser": superuserAuth()} {
		t.Run(name, func(t *testing.T) {
			_, sqlDB, srv := inlineScopePanel(t, adminAuth, acmeTenant)

			// Book 5 is a globex row filed under acme's shelf 1.
			for _, child := range []map[string]any{
				{"id": 5, "title": "Taken"},
				{"id": 5, "_delete": true},
			} {
				resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{child}})
				if status != http.StatusNotFound {
					t.Errorf("another tenant's child %v: status %d body=%s, want 404", child, status, mustJSON(resp))
				}
			}
			for _, child := range []map[string]any{
				{"title": "Smuggled", "tenant_id": "globex"},
				{"id": 1, "TenantID": "globex"},
			} {
				resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{child}})
				if status != http.StatusBadRequest {
					t.Errorf("child %v naming another tenant: status %d body=%s, want 400", child, status, mustJSON(resp))
				}
			}
			assertBooksUntouched(t, sqlDB)

			if resp, status := putShelf(t, srv, "1", map[string]any{"books": []map[string]any{{"title": "Local"}}}); status != http.StatusOK {
				t.Fatalf("a child of the tenant: status %d body=%s", status, mustJSON(resp))
			}
			if got := readBook(t, sqlDB, 6); got.tenant != "acme" {
				t.Errorf("the added child = %+v, want it stamped acme", got)
			}
		})
	}
}

// ---- all or nothing ----------------------------------------------------

// One child refused refuses the save: the parent's own change and the
// children before and after the refused one are not written either.
func TestInlineScope_OneRefusedChildWritesNothing(t *testing.T) {
	_, sqlDB, srv := inlineScopePanel(t, operatorAuth(), nil, shelfEditor(
		[3]string{"operator", "admin:Book", "create"},
		[3]string{"operator", "admin:Book", "update"},
	)...)

	for _, tc := range []struct {
		name   string
		books  []map[string]any
		status int
	}{
		{"a delete the operator does not hold", []map[string]any{
			{"title": "Added"}, {"id": 1, "title": "One, edited"}, {"id": 2, "_delete": true},
		}, http.StatusForbidden},
		{"a child of another record", []map[string]any{
			{"title": "Added"}, {"id": 1, "title": "One, edited"}, {"id": 3, "title": "Taken"},
		}, http.StatusNotFound},
		{"a child naming another record", []map[string]any{
			{"title": "Added"}, {"id": 1, "title": "One, edited"}, {"title": "Moved", "shelf_id": 2},
		}, http.StatusBadRequest},
	} {
		resp, status := putShelf(t, srv, "1", map[string]any{"name": "Renamed", "books": tc.books})
		if status != tc.status {
			t.Errorf("%s: status %d body=%s, want %d", tc.name, status, mustJSON(resp), tc.status)
		}
		if got := shelfName(t, sqlDB, 1); got != "Mine" {
			t.Fatalf("%s: the refused save renamed the shelf: %q", tc.name, got)
		}
		assertBooksUntouched(t, sqlDB)
	}

	// Creating the parent is refused whole the same way.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/Shelf", map[string]any{
		"name": "New", "books": []map[string]any{{"title": "Added"}, {"id": 3, "title": "Taken"}},
	})
	if status != http.StatusNotFound {
		t.Errorf("create with a foreign child: status %d body=%s, want 404", status, mustJSON(resp))
	}
	if n := shelfCount(t, sqlDB); n != 3 {
		t.Errorf("a refused create left a shelf behind: %d shelves", n)
	}
	assertBooksUntouched(t, sqlDB)
}

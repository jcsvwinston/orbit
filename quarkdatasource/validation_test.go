package quarkdatasource

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/orbit/datasource"
)

// QDAuthor and QDArticle are the suite starter's two models (nucleus new
// --template suite), with what the starter's Article does not declare but
// an application's does: a declared size, a CHECK, an optional timestamp
// and the created_at Quark stamps.
type QDAuthor struct {
	ID   int64  `db:"id" pk:"true"`
	Name string `db:"name" quark:"not_null"`
}

type QDArticle struct {
	ID          int64      `db:"id" pk:"true"`
	AuthorID    int64      `db:"author_id" quark:"not_null"`
	Title       string     `db:"title,size=20" quark:"not_null,unique"`
	Body        string     `db:"body"`
	Views       int32      `db:"views" quark:"check=views >= 0"`
	PublishedAt *time.Time `db:"published_at"`
	CreatedAt   time.Time  `db:"created_at" quark:"not_null"`

	Author QDAuthor `rel:"belongs_to" join:"author_id"`
}

func setupArticles(t *testing.T) (*Adapter, datasource.RecordStore, context.Context) {
	t.Helper()
	client, err := quark.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("quark.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.RegisterModel(&QDAuthor{}, &QDArticle{}); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	if err := client.MigrateRegistered(ctx); err != nil {
		t.Fatalf("MigrateRegistered: %v", err)
	}
	a := New(client)
	if err := Register[QDAuthor](a); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := Register[QDArticle](a); err != nil {
		t.Fatalf("Register: %v", err)
	}
	st, err := a.Store("QDArticle", "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	return a, st, ctx
}

// domainStatus asserts err is a domain error with the given status.
func domainStatus(t *testing.T, err error, status int, what string) *gferrors.DomainError {
	t.Helper()
	var de *gferrors.DomainError
	if !errors.As(err, &de) || de.StatusCode != status {
		t.Fatalf("%s: err = %v (%T), want a %d domain error", what, err, err, status)
	}
	return de
}

// noEngineText fails when an answer carries the database's own words: the
// driver's message is what reached the operator as a 500 before.
func noEngineText(t *testing.T, err error, what string) {
	t.Helper()
	msg := strings.ToLower(err.Error())
	for _, leak := range []string{"unique constraint", "constraint failed", "sqlstate", "duplicate", "sqlite", "(2067)", "(275)", "json:", "strconv"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("%s: the answer carries engine or decoder text %q: %v", what, leak, err)
		}
	}
}

func countRows(t *testing.T, st datasource.RecordStore, ctx context.Context) int64 {
	t.Helper()
	page, err := st.List(ctx, datasource.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return page.Total
}

// The catalogue says which fields are required, and that is what the store
// enforces: NOT NULL fields, but not the created_at Quark stamps itself.
func TestCatalogue_RequiredFollowsNotNullButNotStampedTimestamps(t *testing.T) {
	a, _, _ := setupArticles(t)
	mi, _ := a.Get("QDArticle")
	want := map[string]bool{"author_id": true, "title": true, "body": false, "views": false, "published_at": false, "created_at": false}
	for _, f := range mi.Fields {
		if got, ok := want[f.Column]; ok && f.IsRequired != got {
			t.Errorf("%s: IsRequired = %v, want %v", f.Column, f.IsRequired, got)
		}
	}
}

// OR-78: the starter's own repro. A required field left out used to be
// stored empty (201 with title ""); a value of the wrong type was a 500
// with the decoder's text; a key naming no field was dropped. All three are
// now the Nucleus adapter's 422, one message per key, and nothing is
// written.
func TestCreate_ValidatesBeforeWriting(t *testing.T) {
	_, st, ctx := setupArticles(t)

	cases := []struct {
		name string
		rec  datasource.Record
		want map[string]string
	}{
		{"required left out", datasource.Record{"author_id": 1, "body": "sin titulo"},
			map[string]string{"title": msgRequired}},
		{"required empty", datasource.Record{"author_id": 1, "title": ""},
			map[string]string{"title": msgRequired}},
		{"required null", datasource.Record{"author_id": nil, "title": "t"},
			map[string]string{"author_id": msgRequired}},
		{"text into an integer", datasource.Record{"author_id": "x", "title": "t"},
			map[string]string{"author_id": "must be an integer"}},
		{"fraction into an integer", datasource.Record{"author_id": 1.5, "title": "t"},
			map[string]string{"author_id": "must be an integer"}},
		{"number into text", datasource.Record{"author_id": 1, "title": 123},
			map[string]string{"title": "must be a string"}},
		{"wider than the column", datasource.Record{"author_id": 1, "title": strings.Repeat("x", 21)},
			map[string]string{"title": "must be at most 20 characters"}},
		{"out of the type's range", datasource.Record{"author_id": 1, "title": "t", "views": 1 << 40},
			map[string]string{"views": "out of range for int32"}},
		{"not a timestamp", datasource.Record{"author_id": 1, "title": "t", "published_at": "yesterday"},
			map[string]string{"published_at": "must be a timestamp (RFC 3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD)"}},
		{"unknown key", datasource.Record{"author_id": 1, "title": "t", "nosuchfield": "x"},
			map[string]string{"nosuchfield": msgUnknown}},
		{"a relation is not a field", datasource.Record{"author_id": 1, "title": "t", "Author": map[string]any{"Name": "x"}},
			map[string]string{"Author": msgUnknown}},
		{"every problem at once", datasource.Record{"author_id": "x", "colour": "red"},
			map[string]string{"author_id": "must be an integer", "colour": msgUnknown, "title": msgRequired}},
	}
	for _, tc := range cases {
		_, err := st.Create(ctx, tc.rec)
		got := fieldProblems(t, err, tc.name)
		noEngineText(t, err, tc.name)
		if len(got) != len(tc.want) {
			t.Errorf("%s: problems = %v, want %v", tc.name, got, tc.want)
			continue
		}
		for k, msg := range tc.want {
			if got[k] != msg {
				t.Errorf("%s: problems[%q] = %q, want %q (all: %v)", tc.name, k, got[k], msg, got)
			}
		}
	}
	if n := countRows(t, st, ctx); n != 0 {
		t.Fatalf("a refused record must write nothing, found %d rows", n)
	}

	// What the Nucleus adapter accepts, this one accepts: a number as text
	// (a CSV cell), a metadata key, an empty cell for an optional number, a
	// bare date, and no created_at (Quark stamps it).
	created, err := st.Create(ctx, datasource.Record{
		"author_id": "7", "title": "Hello, Quantum", "views": "", "published_at": "2026-10-23", "_model": "QDArticle",
	})
	if err != nil {
		t.Fatalf("valid create: %v", err)
	}
	if created["author_id"] != float64(7) || created["title"] != "Hello, Quantum" {
		t.Fatalf("created = %v", created)
	}
	if ts, _ := created["created_at"].(string); ts == "" || strings.HasPrefix(ts, "0001-") {
		t.Fatalf("created_at = %v, want the stamp Quark writes", created["created_at"])
	}
	if ts, _ := created["published_at"].(string); !strings.HasPrefix(ts, "2026-10-23") {
		t.Fatalf("published_at = %v, want the bare date read", created["published_at"])
	}
}

// OR-78: a value a unique key already holds was a 500 carrying the driver's
// text ("UNIQUE constraint failed … (2067)" on SQLite, "SQLSTATE 23505" on
// PostgreSQL). It is a 409 that names the field and says nothing of the
// engine, on a create and on an update alike.
func TestWrite_UniqueViolationIsConflict(t *testing.T) {
	_, st, ctx := setupArticles(t)
	first, err := st.Create(ctx, datasource.Record{"author_id": 1, "title": "Hello, Quantum"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	second, err := st.Create(ctx, datasource.Record{"author_id": 1, "title": "Second"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err = st.Create(ctx, datasource.Record{"author_id": 1, "title": "Hello, Quantum", "body": "x"})
	de := domainStatus(t, err, http.StatusConflict, "create with a repeated title")
	noEngineText(t, err, "create with a repeated title")
	if details, _ := de.Details.(map[string]string); details["title"] == "" || !strings.Contains(de.Message, "title") {
		t.Fatalf("the conflict must name the field: message %q details %#v", de.Message, de.Details)
	}

	err = st.Update(ctx, fmtID(second["id"]), datasource.Record{"title": "Hello, Quantum"})
	domainStatus(t, err, http.StatusConflict, "update onto a repeated title")
	noEngineText(t, err, "update onto a repeated title")

	if got, _ := st.Get(ctx, fmtID(second["id"])); got["title"] != "Second" {
		t.Fatalf("a refused update must not write: %v", got)
	}
	if got, _ := st.Get(ctx, fmtID(first["id"])); got["title"] != "Hello, Quantum" {
		t.Fatalf("the first row must be untouched: %v", got)
	}
}

// A constraint that is not a unique key — here a CHECK — is a 409 too, with
// the adapter's words, not the engine's.
func TestWrite_OtherConstraintIsConflict(t *testing.T) {
	_, st, ctx := setupArticles(t)
	_, err := st.Create(ctx, datasource.Record{"author_id": 1, "title": "t", "views": -1})
	domainStatus(t, err, http.StatusConflict, "create breaking a CHECK")
	noEngineText(t, err, "create breaking a CHECK")
	if n := countRows(t, st, ctx); n != 0 {
		t.Fatalf("found %d rows after a refused create", n)
	}
}

// An update is held to the same rules as a create for what it names: it
// cannot empty a required field, store a value of the wrong type or name a
// field that does not exist; and one that names nothing writable is a 400,
// as on the Nucleus adapter, instead of a success that changed nothing.
func TestUpdate_ValidatesBeforeWriting(t *testing.T) {
	_, st, ctx := setupArticles(t)
	created, err := st.Create(ctx, datasource.Record{"author_id": 1, "title": "first"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id := fmtID(created["id"])

	cases := []struct {
		name string
		rec  datasource.Record
		key  string
		msg  string
	}{
		{"empty a required field", datasource.Record{"title": ""}, "title", msgRequired},
		{"null a required field", datasource.Record{"title": nil}, "title", msgRequired},
		{"text into an integer", datasource.Record{"author_id": "x"}, "author_id", "must be an integer"},
		{"number into text", datasource.Record{"title": 5}, "title", "must be a string"},
		{"unknown key", datasource.Record{"title": "ok", "nosuchfield": 1}, "nosuchfield", msgUnknown},
	}
	for _, tc := range cases {
		err := st.Update(ctx, id, tc.rec)
		got := fieldProblems(t, err, tc.name)
		noEngineText(t, err, tc.name)
		if got[tc.key] != tc.msg {
			t.Errorf("%s: problems = %v, want %q: %q", tc.name, got, tc.key, tc.msg)
		}
	}
	if got, _ := st.Get(ctx, id); got["title"] != "first" || got["author_id"] != float64(1) {
		t.Fatalf("refused updates must not write: %v", got)
	}

	// Only the primary key: nothing to write.
	domainStatus(t, st.Update(ctx, id, datasource.Record{"id": 99}), http.StatusBadRequest, "update naming only the primary key")

	// A partial update that keeps the row valid goes through; an optional
	// field can be cleared.
	if err := st.Update(ctx, id, datasource.Record{"views": "9", "published_at": "2026-10-23T10:30", "body": ""}); err != nil {
		t.Fatalf("valid partial update: %v", err)
	}
	got, _ := st.Get(ctx, id)
	if got["views"] != float64(9) || got["title"] != "first" {
		t.Fatalf("after update = %v", got)
	}
	if err := st.Update(ctx, id, datasource.Record{"published_at": nil}); err != nil {
		t.Fatalf("clearing an optional timestamp: %v", err)
	}
	if got, _ := st.Get(ctx, id); got["published_at"] != nil {
		t.Fatalf("published_at = %v, want null", got["published_at"])
	}
	// Writing the values a row already holds is not a missing row, on an
	// engine that counts changed rows rather than matched ones either.
	if err := st.Update(ctx, id, datasource.Record{"title": "first"}); err != nil {
		t.Fatalf("an update that changes nothing on an existing row: %v", err)
	}
}

// OR-83 and the M0-E fixes: a row that does not exist is the contract's
// not-found on a read, an update and a delete. The update and the delete
// used to answer success — the panel then answered {"updated": true} and
// {"deleted": true} and recorded both in the audit trail — and the read
// was a 500.
func TestMissingRow_IsNotFound(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDWidget", "")

	created, err := st.Create(ctx, datasource.Record{"name": "gone"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := fmtID(created["id"])
	if _, err := st.Get(ctx, id); err != nil {
		t.Fatalf("Get of the row just created: %v", err)
	}

	_, err = st.Get(ctx, "424242")
	domainStatus(t, err, http.StatusNotFound, "Get of an id that does not exist")
	domainStatus(t, st.Update(ctx, "424242", datasource.Record{"name": "ghost"}), http.StatusNotFound, "Update of an id that does not exist")
	domainStatus(t, st.Delete(ctx, "424242"), http.StatusNotFound, "Delete of an id that never existed")

	if err := st.Delete(ctx, id); err != nil {
		t.Fatalf("Delete of an existing row: %v", err)
	}
	domainStatus(t, st.Delete(ctx, id), http.StatusNotFound, "Delete of the row already deleted")
	_, err = st.Get(ctx, id)
	domainStatus(t, err, http.StatusNotFound, "Get after the delete")
	domainStatus(t, st.Update(ctx, id, datasource.Record{"name": "ghost"}), http.StatusNotFound, "Update after the delete")
	if n := countRows(t, st, ctx); n != 0 {
		t.Fatalf("an update of a missing row must not create one, found %d rows", n)
	}
}

// A database alias this adapter does not serve is the request's mistake,
// answered as one (400 naming the alias), not a plain error the panel
// reports as its own failure (500). The panel's presence sweep already
// treated the refusal as absence.
func TestStore_ForeignAliasIsBadRequest(t *testing.T) {
	a, _ := setup(t)
	if _, err := a.Store("QDWidget", ""); err != nil {
		t.Fatalf("Store on the adapter's own alias (empty): %v", err)
	}
	if _, err := a.Store("QDWidget", DefaultDatabaseAlias); err != nil {
		t.Fatalf("Store on the adapter's own alias (%q): %v", DefaultDatabaseAlias, err)
	}
	_, err := a.Store("QDWidget", "warehouse")
	assertBadRequest(t, err, "Store on an alias the adapter does not serve")
	if !strings.Contains(err.Error(), `"warehouse"`) || !strings.Contains(err.Error(), `"default"`) {
		t.Fatalf("the refusal names neither the alias asked nor the one served: %v", err)
	}
}

package quarkdatasource

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/datasource"
)

// assertNotFound expects the contract's not-found: the 404 domain error the
// panel answers a row out of scope with, so a missing row reaches the
// operator as "not found" and a batch counts it as a failure.
func assertNotFound(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error", what)
	}
	var domErr *gferrors.DomainError
	if !errors.As(err, &domErr) || domErr.StatusCode != http.StatusNotFound {
		t.Fatalf("%s: err = %v (%T), want a 404 domain error", what, err, err)
	}
}

// A Get of an id that does not exist is the contract's not-found, not
// Quark's ErrNotFound unmapped: the panel answered it as a 500 that named
// nothing, and a record action on a missing row failed as a server error
// instead of "not found". Found by the panel's own tests when they moved
// onto this adapter (2.0); a 1.x application on quarkdatasource has the
// defect today.
func TestGet_MissingRowIsNotFound(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDWidget", "")

	created, err := st.Create(ctx, datasource.Record{"name": "one"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Get(ctx, fmtID(created["id"])); err != nil {
		t.Fatalf("Get of the row just created: %v", err)
	}
	_, err = st.Get(ctx, "424242")
	assertNotFound(t, err, "Get of an id that does not exist")
}

// A Delete that deleted nothing is a not-found, not a success: the row
// does not exist (or is already soft-deleted), and a batch delete that
// counted it as deleted reported two rows gone where one was. Soft-deleted
// models (deleted_at) and hard-deleted ones answer the same.
func TestDelete_ZeroRowsIsNotFound(t *testing.T) {
	a, ctx := setup(t)
	st, _ := a.Store("QDWidget", "")

	created, err := st.Create(ctx, datasource.Record{"name": "gone"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := fmtID(created["id"])
	if err := st.Delete(ctx, id); err != nil {
		t.Fatalf("Delete of an existing row: %v", err)
	}
	assertNotFound(t, st.Delete(ctx, id), "Delete of the row already deleted")
	assertNotFound(t, st.Delete(ctx, "424242"), "Delete of an id that never existed")
	// The first delete happened: the row is gone for Get too.
	_, err = st.Get(ctx, id)
	assertNotFound(t, err, "Get after the delete")
}

// A database alias this adapter does not serve is the request's mistake,
// answered as one (400 naming the alias), not a plain error the panel
// reports as its own failure (500). The panel's presence sweep already
// treated the refusal as absence; a listing that names the alias now gets
// the refusal as a bad request.
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
	// An unknown model on the served alias is still a plain error: that one
	// is the application's wiring, not the request's.
	if _, err := a.Store("Nope", ""); err == nil {
		t.Fatal("Store of an unregistered model: expected an error")
	}
}

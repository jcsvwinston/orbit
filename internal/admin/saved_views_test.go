package admin

// Saved views: the filter set an operator returns to.
//
// What these hold down is the ownership, which is the part that is easy to get
// wrong: a view belongs to whoever saved it, a shared one is visible to
// everybody and still editable only by its owner, and neither can be created
// for a model the operator may not list.

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

func saveView(t *testing.T, srv *httptest.Server, body map[string]any) (map[string]interface{}, int) {
	t.Helper()
	return doJSON(t, http.MethodPost, srv.URL+"/api/views", body)
}

func TestSavedViews_SurviveAndComeBackWithTheirQuery(t *testing.T) {
	_, _, srv := formsPanel(t, nil)

	resp, status := saveView(t, srv, map[string]any{
		"model": "Album", "name": "Recent", "query": "?status=open&order_by=title+asc",
	})
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body=%s", status, mustJSON(resp))
	}
	id, _ := resp["id"].(string)
	// The leading "?" is not part of a query string.
	if got, _ := resp["query"].(string); got != "status=open&order_by=title+asc" {
		t.Errorf("query = %q, want it stored without the leading ?", got)
	}

	list, status := doJSON(t, http.MethodGet, srv.URL+"/api/views?model=Album", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(list))
	}
	if !strings.Contains(mustJSON(list), id) {
		t.Fatalf("the saved view is not in the list: %s", mustJSON(list))
	}

	// A view for another model is not in this model's list.
	other, status := doJSON(t, http.MethodGet, srv.URL+"/api/views?model=AdminUser", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d", status)
	}
	if strings.Contains(mustJSON(other), id) {
		t.Errorf("a view of Album showed up under AdminUser: %s", mustJSON(other))
	}
}

func TestSavedViews_UpdateAndDelete(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	resp, _ := saveView(t, srv, map[string]any{"model": "Album", "name": "Before", "query": "a=1"})
	id, _ := resp["id"].(string)

	resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/views/"+id,
		map[string]any{"name": "After", "query": "b=2", "is_shared": true})
	if status != http.StatusOK {
		t.Fatalf("update: status %d body=%s", status, mustJSON(resp))
	}
	if resp["name"] != "After" || resp["query"] != "b=2" || resp["is_shared"] != true {
		t.Fatalf("the update did not land: %s", mustJSON(resp))
	}

	resp, status = doJSON(t, http.MethodDelete, srv.URL+"/api/views/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("delete: status %d body=%s", status, mustJSON(resp))
	}
	list, _ := doJSON(t, http.MethodGet, srv.URL+"/api/views", nil)
	if strings.Contains(mustJSON(list), id) {
		t.Fatalf("the deleted view is still listed: %s", mustJSON(list))
	}
}

// A view belongs to whoever saved it: another operator does not see a private
// one, sees a shared one, and cannot edit either.
func TestSavedViews_OwnershipIsEnforced(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)

	private, _ := saveView(t, srv, map[string]any{"model": "Album", "name": "Mine", "query": "a=1"})
	shared, _ := saveView(t, srv, map[string]any{"model": "Album", "name": "Ours", "query": "b=2", "is_shared": true})
	privateID, _ := private["id"].(string)
	sharedID, _ := shared["id"].(string)

	// A second operator signs in.
	panel.config.Auth = &testAdminAuth{user: &auth.User{ID: "9", Username: "someone-else", Role: "admin"}}

	list, status := doJSON(t, http.MethodGet, srv.URL+"/api/views", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(list))
	}
	body := mustJSON(list)
	if strings.Contains(body, privateID) {
		t.Errorf("another operator sees a private view: %s", body)
	}
	if !strings.Contains(body, sharedID) {
		t.Errorf("a shared view is invisible to another operator: %s", body)
	}

	for _, id := range []string{privateID, sharedID} {
		resp, status := doJSON(t, http.MethodPut, srv.URL+"/api/views/"+id, map[string]any{"name": "taken over"})
		if status != http.StatusForbidden {
			t.Errorf("editing %s: status %d body=%s, want 403", id, status, mustJSON(resp))
		}
		resp, status = doJSON(t, http.MethodDelete, srv.URL+"/api/views/"+id, nil)
		if status != http.StatusForbidden {
			t.Errorf("deleting %s: status %d body=%s, want 403", id, status, mustJSON(resp))
		}
	}
}

// A superuser may tidy up anybody's view: somebody has to be able to remove
// the shared one whose owner left.
func TestSavedViews_SuperuserMayEditAnyone(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)
	view, _ := saveView(t, srv, map[string]any{"model": "Album", "name": "Theirs", "query": "a=1"})
	id, _ := view["id"].(string)

	panel.config.Auth = superuserAuth()
	resp, status := doJSON(t, http.MethodDelete, srv.URL+"/api/views/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s, want the superuser to be able to remove it", status, mustJSON(resp))
	}
}

// A view points at a model's list, so it needs the permission that list needs.
func TestSavedViews_NeedThePermissionOfTheListTheyPointAt(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := enf.AddPolicy("operator", "admin:Track", "list"); err != nil {
		t.Fatal(err)
	}
	panel.rbac = enf

	resp, status := saveView(t, srv, map[string]any{"model": "Album", "name": "Nope", "query": "a=1"})
	if status != http.StatusForbidden {
		t.Fatalf("status %d body=%s, want 403", status, mustJSON(resp))
	}
	resp, status = saveView(t, srv, map[string]any{"model": "Track", "name": "Fine", "query": "a=1"})
	if status != http.StatusCreated {
		t.Fatalf("a model the operator may list: status %d body=%s", status, mustJSON(resp))
	}
}

// A view of a model this operator cannot list is not theirs to see — not even
// their own, and least of all a shared one: the row would tell them the model
// exists and what somebody filters it by, which is what the list permission
// withholds.
func TestSavedViews_ListHidesViewsOfModelsTheOperatorCannotList(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)
	album, _ := saveView(t, srv, map[string]any{"model": "Album", "name": "Albums", "query": "a=1", "is_shared": true})
	track, _ := saveView(t, srv, map[string]any{"model": "Track", "name": "Tracks", "query": "b=2"})
	albumID, _ := album["id"].(string)
	trackID, _ := track["id"].(string)

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := enf.AddPolicy("operator", "admin:Track", "list"); err != nil {
		t.Fatal(err)
	}
	panel.rbac = enf

	list, status := doJSON(t, http.MethodGet, srv.URL+"/api/views", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(list))
	}
	body := mustJSON(list)
	if strings.Contains(body, albumID) {
		t.Errorf("a view of a model the operator cannot list is in the list: %s", body)
	}
	if !strings.Contains(body, trackID) {
		t.Errorf("the view of the model they CAN list disappeared too: %s", body)
	}
}

func TestSavedViews_RefuseNonsense(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	cases := []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"model": "Album", "query": "a=1"}, http.StatusBadRequest},                   // no name
		{map[string]any{"name": "No model", "query": "a=1"}, http.StatusBadRequest},                 // no model
		{map[string]any{"model": "Ghost", "name": "Ghost", "query": "a=1"}, http.StatusNotFound},    // no such model
		{map[string]any{"model": "Album", "name": strings.Repeat("x", 200)}, http.StatusBadRequest}, // name too long
		{map[string]any{"model": "Album", "name": "Big", "query": strings.Repeat("q", 5000)}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		resp, status := saveView(t, srv, tc.body)
		if status != tc.want {
			t.Errorf("%v: status %d body=%s, want %d", tc.body, status, mustJSON(resp), tc.want)
		}
	}
}

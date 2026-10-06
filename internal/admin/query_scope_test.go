package admin

// Tests for OR-69: a field the operator may not read is not a field they can
// ask about either.
//
// The list masked a field a policy kept from the operator, and then filtered
// by it, sorted by it and searched in it for them: with a deny on `owner`,
// ?owner=operator answered one row and ?owner=nobody none, which is the
// value, one guess at a time. The export refused such a filter since OR-66;
// the list, the relation lookups, the saved views and the records card took
// the same question their own way. They now ask the read scope
// (requestReadScope) — the same one the export asks — what the operator may
// name, and a field they may not read is answered as if the model had no
// such field.

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/model"
)

// enforcerWith is an enforcer holding exactly policies.
func enforcerWith(t *testing.T, policies ...[3]string) *authz.Enforcer {
	t.Helper()
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, pol := range policies {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatalf("AddPolicy %v: %v", pol, err)
		}
	}
	return enf
}

// hiddenOwnerPolicies grant the list of OwnedNote whole (no #own), and keep
// two fields from the operator: owner, which the model offers as a filter,
// and secret, which it does not.
func hiddenOwnerPolicies() [][3]string {
	return [][3]string{
		{"operator", "admin:OwnedNote", "get_schema"},
		{"operator", "admin:OwnedNote", "list"},
		{"operator", "admin:OwnedNote.owner", "deny"},
		{"operator", "admin:OwnedNote.secret", "deny"},
	}
}

func listQuery(t *testing.T, srv *httptest.Server, query string) (map[string]interface{}, int) {
	t.Helper()
	return doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote?"+query, nil)
}

// The finding as it was reported: a filter on a field the operator may not
// read answered, by the rows it left, what the field held.
func TestListQuery_AFilterOnAHiddenFieldIsRefused(t *testing.T) {
	panel, _, srv := ownedPanel(t, hiddenOwnerPolicies()...)

	// What the operator is shown: both rows, neither owner nor secret.
	list, status := listQuery(t, srv, "")
	if status != http.StatusOK || !strings.Contains(mustJSON(list), "Theirs") || strings.Contains(mustJSON(list), "somebody-else") {
		t.Fatalf("precondition: the operator's list: status %d body=%s", status, mustJSON(list))
	}

	// The answer for a field the model does not have, which is what a field
	// the operator may not read must be indistinguishable from.
	unknown, status := listQuery(t, srv, "nosuchfield=x")
	if status != http.StatusBadRequest || !strings.Contains(mustJSON(unknown), `invalid filter field`) {
		t.Fatalf("precondition: an unknown field: status %d body=%s", status, mustJSON(unknown))
	}

	for _, query := range []string{
		"owner=operator", "owner=nobody", "Owner=operator", "OWNER=operator",
		"owner__eq=operator", "owner__ne=operator", "owner__in=operator,nobody",
		"owner__not_in=nobody", "owner__contains=oper", "owner__startswith=op",
		"owner__endswith=tor", "owner__gt=a", "owner__isnull=false",
		// secret is not a filter of the model: "filter is not enabled"
		// would say the field exists, which an unknown one does not.
		"secret=mine-secret", "secret__contains=mine",
	} {
		resp, status := listQuery(t, srv, query)
		if status != http.StatusBadRequest {
			t.Errorf("?%s on a field the operator may not read: status %d body=%s, want 400", query, status, mustJSON(resp))
			continue
		}
		if !strings.Contains(mustJSON(resp), "invalid filter field") {
			t.Errorf("?%s is refused with an answer an unknown field does not get: %s", query, mustJSON(resp))
		}
	}

	// The superuser reads every field, and filters by every one.
	panel.config.Auth = superuserAuth()
	resp, status := listQuery(t, srv, "owner=operator")
	if status != http.StatusOK || !strings.Contains(mustJSON(resp), "Mine") || strings.Contains(mustJSON(resp), "Theirs") {
		t.Fatalf("the superuser's filter on owner: status %d body=%s", status, mustJSON(resp))
	}
}

// A sort on a field the operator may not read pages the rows in that field's
// order: a comparison oracle, one page at a time.
func TestListQuery_ASortOnAHiddenFieldIsRefused(t *testing.T) {
	panel, _, srv := ownedPanel(t, hiddenOwnerPolicies()...)

	for _, order := range []string{"owner", "owner desc", "Secret asc", "title asc, secret desc"} {
		resp, status := listQuery(t, srv, "order_by="+url.QueryEscape(order))
		if status != http.StatusBadRequest || !strings.Contains(mustJSON(resp), "invalid order_by") {
			t.Errorf("order_by=%s on a field the operator may not read: status %d body=%s, want 400 invalid order_by", order, status, mustJSON(resp))
		}
	}
	// A field they read still sorts.
	if resp, status := listQuery(t, srv, "order_by="+url.QueryEscape("title desc")); status != http.StatusOK {
		t.Errorf("order_by=title desc: status %d body=%s", status, mustJSON(resp))
	}

	panel.config.Auth = superuserAuth()
	if resp, status := listQuery(t, srv, "order_by="+url.QueryEscape("secret desc")); status != http.StatusOK {
		t.Errorf("the superuser's sort on secret: status %d body=%s", status, mustJSON(resp))
	}
}

// makeSearchable turns is_search on for fields of OwnedNote, the way Field
// settings does at runtime.
func makeSearchable(t *testing.T, panel *Panel, fields ...string) {
	t.Helper()
	on := true
	updates := make(map[string]model.FieldMetaUpdate, len(fields))
	for _, f := range fields {
		updates[f] = model.FieldMetaUpdate{IsSearch: &on}
	}
	if err := panel.registry.BulkUpdateFieldMeta("OwnedNote", updates); err != nil {
		t.Fatal(err)
	}
}

// ?search= looks in every searchable field the backend knows, and the panel
// cannot tell it which: a search that reaches a field the operator may not
// read finds a row by that field's value. It is refused, and the schema says
// so, so the grid does not offer the box.
func TestListSearch_ThatWouldReachAHiddenFieldIsRefused(t *testing.T) {
	panel, _, srv := ownedPanel(t, hiddenOwnerPolicies()...)
	makeSearchable(t, panel, "Secret")

	for _, text := range []string{"mine-secret", "Mine"} {
		resp, status := listQuery(t, srv, "search="+text)
		if status != http.StatusBadRequest {
			t.Errorf("?search=%s reaches secret, which the operator may not read: status %d body=%s, want 400", text, status, mustJSON(resp))
		}
	}
	schema, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
	if status != http.StatusOK || schema["searchable"] != false {
		t.Errorf("the operator's schema does not say search is not theirs: status %d searchable=%v", status, schema["searchable"])
	}

	// The superuser reads secret: their search is the search it was.
	panel.config.Auth = superuserAuth()
	resp, status := listQuery(t, srv, "search=mine-secret")
	if status != http.StatusOK || !strings.Contains(mustJSON(resp), "Mine") {
		t.Errorf("the superuser's search: status %d body=%s", status, mustJSON(resp))
	}
	schema, _ = doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
	if schema["searchable"] != true {
		t.Errorf("the superuser's schema does not say search is theirs: searchable=%v", schema["searchable"])
	}
}

// The search an operator keeps is the one that looks only where they read.
func TestListSearch_OverReadableFieldsStillAnswers(t *testing.T) {
	_, _, srv := ownedPanel(t, hiddenOwnerPolicies()...)
	resp, status := listQuery(t, srv, "search=Mine")
	if status != http.StatusOK || !strings.Contains(mustJSON(resp), "Mine") || strings.Contains(mustJSON(resp), "Theirs") {
		t.Fatalf("a search over title, which the operator reads: status %d body=%s", status, mustJSON(resp))
	}
	schema, _ := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/schema", nil)
	if schema["searchable"] != true {
		t.Errorf("the schema says the operator may not search, and they may: searchable=%v", schema["searchable"])
	}
}

// The only searchable field kept from the operator: no search at all.
func TestListSearch_TheOnlySearchableFieldHiddenIsRefused(t *testing.T) {
	_, _, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote.title", "deny"},
	)
	resp, status := listQuery(t, srv, "search=Mine")
	if status != http.StatusBadRequest {
		t.Fatalf("?search= over title, which the operator may not read: status %d body=%s, want 400", status, mustJSON(resp))
	}
}

// A field the panel excludes is no operator's to read — a superuser's
// neither — and a search that looks in it finds a row by its value all the
// same. Nucleus searches every field tagged searchable, excluded or not.
func TestListSearch_ThatWouldReachAnExcludedFieldIsRefused(t *testing.T) {
	panel, _, srv := ownedPanel(t)
	panel.config.Auth = superuserAuth()
	on := true
	if err := panel.registry.BulkUpdateFieldMeta("OwnedNote", map[string]model.FieldMetaUpdate{
		"Secret": {IsSearch: &on, IsExcluded: &on},
	}); err != nil {
		t.Fatal(err)
	}
	resp, status := listQuery(t, srv, "search=mine-secret")
	if status != http.StatusBadRequest {
		t.Fatalf("?search= reaching an excluded field: status %d body=%s, want 400", status, mustJSON(resp))
	}
}

// A relation lookup reads the target the way its list does: the label is a
// field the operator reads, and ?q= is the list's search.
func TestModelOptions_StayInTheFieldsTheOperatorReads(t *testing.T) {
	panel, _, srv := ownedPanel(t,
		[3]string{"operator", "admin:OwnedNote", "list"},
		[3]string{"operator", "admin:OwnedNote.title", "deny"},
	)
	options := func() (map[string]interface{}, int) {
		return doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/options", nil)
	}

	resp, status := options()
	if status != http.StatusOK {
		t.Fatalf("options: status %d body=%s", status, mustJSON(resp))
	}
	if body := mustJSON(resp); strings.Contains(body, "Mine") || strings.Contains(body, "Theirs") || resp["label_field"] == "Title" {
		t.Errorf("the options label rows with a field the operator may not read: %s", body)
	}
	if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/options?q=Mine", nil); status != http.StatusBadRequest {
		t.Errorf("?q= over title, which the operator may not read: status %d body=%s, want 400", status, mustJSON(resp))
	}

	panel.config.Auth = superuserAuth()
	resp, _ = options()
	if !strings.Contains(mustJSON(resp), "Mine") || resp["label_field"] != "Title" {
		t.Errorf("the superuser's options: %s", mustJSON(resp))
	}
}

// What a FIELD may point at is asked of the field: one the operator may not
// read is not a field of the model for them, as the schema already says.
func TestFieldOptions_AFieldTheOperatorMayNotReadIsNotFound(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)
	panel.rbac = enforcerWith(t,
		[3]string{"operator", "admin:Album", "list"},
		[3]string{"operator", "admin:Track", "list"},
		[3]string{"operator", "admin:Track.album_id", "deny"},
	)
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Track/fields/album_id/options", nil)
	if status != http.StatusNotFound {
		t.Errorf("the options of a field the operator may not read: status %d body=%s, want 404", status, mustJSON(resp))
	}
	panel.rbac = enforcerWith(t,
		[3]string{"operator", "admin:Album", "list"},
		[3]string{"operator", "admin:Track", "list"},
	)
	if resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/Track/fields/album_id/options", nil); status != http.StatusOK {
		t.Errorf("the options of a field the operator reads: status %d body=%s", status, mustJSON(resp))
	}
}

// A shared view that filters or sorts by a field this operator may not read
// is not theirs to see: it names the field and what somebody looks for in
// it, and the list would refuse it anyway.
func TestSavedViews_AViewNamingAHiddenFieldIsNotListed(t *testing.T) {
	panel, _, srv := ownedPanel(t, hiddenOwnerPolicies()...)

	panel.config.Auth = superuserAuth()
	ids := map[string]string{}
	for name, query := range map[string]string{
		"plain":        "order_by=title+desc",
		"by-owner":     "owner=operator",
		"by-owner-op":  "owner__in=operator",
		"sorted-by-it": "order_by=title+asc,secret+desc",
	} {
		resp, status := saveView(t, srv, map[string]any{"model": "OwnedNote", "name": name, "query": query, "is_shared": true})
		if status != http.StatusCreated {
			t.Fatalf("save %s: status %d body=%s", name, status, mustJSON(resp))
		}
		ids[name], _ = resp["id"].(string)
	}
	all, _ := doJSON(t, http.MethodGet, srv.URL+"/api/views?model=OwnedNote", nil)
	for name, id := range ids {
		if !strings.Contains(mustJSON(all), id) {
			t.Errorf("the superuser does not see the view %s: %s", name, mustJSON(all))
		}
	}

	panel.config.Auth = operatorAuth()
	list, status := doJSON(t, http.MethodGet, srv.URL+"/api/views?model=OwnedNote", nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d body=%s", status, mustJSON(list))
	}
	body := mustJSON(list)
	if !strings.Contains(body, ids["plain"]) {
		t.Errorf("a view over fields the operator reads is not listed: %s", body)
	}
	for _, name := range []string{"by-owner", "by-owner-op", "sorted-by-it"} {
		if strings.Contains(body, ids[name]) {
			t.Errorf("the view %s, which names a field the operator may not read, is listed: %s", name, body)
		}
	}
	if strings.Contains(body, "secret") || strings.Contains(body, "owner=") {
		t.Errorf("the views listed name a field the operator may not read: %s", body)
	}
}

// A records card in an order the operator may not read is the ranking the
// field holds; it is not shown, as a card of a model they may not list is
// not.
func TestRecordsCard_InAnOrderTheOperatorMayNotReadIsNotShown(t *testing.T) {
	_, provider, srv := dashboardPanel(t, nil, []Dashboard{{
		ID: "people",
		Widgets: []Widget{
			{ID: "by-email", Kind: "records", Records: RecordList{Model: "AdminUser", Fields: []string{"Name"}, OrderBy: "email desc", Limit: 3}},
			{ID: "by-id", Kind: "records", Records: RecordList{Model: "AdminUser", Fields: []string{"Name"}, OrderBy: "id desc", Limit: 3}},
		},
	}},
		[3]string{"reader", "admin:dashboard:people", "view"},
		[3]string{"reader", "admin:AdminUser", "list"},
		[3]string{"reader", "admin:AdminUser.email", "deny"},
	)
	for _, name := range []string{"b", "a", "c"} {
		createAdminUser(t, srv, map[string]interface{}{"email": name + "@example.test", "name": "user " + name, "active": true})
	}
	cards := func() []string {
		t.Helper()
		payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboards/people", nil)
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%s", status, mustJSON(payload))
		}
		var ids []string
		for _, w := range payload["widgets"].([]any) {
			ids = append(ids, w.(map[string]any)["id"].(string))
		}
		return ids
	}
	if got := strings.Join(cards(), ","); got != "by-email,by-id" {
		t.Fatalf("the superuser's cards: %s", got)
	}
	provider.user = &auth.User{ID: "reader", Username: "reader", Role: "reader"}
	if got := strings.Join(cards(), ","); got != "by-id" {
		t.Fatalf("the reader's cards: %s, want only by-id — by-email ranks the rows by a field the reader may not read", got)
	}
}

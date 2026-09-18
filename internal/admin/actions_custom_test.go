// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"

	"log/slog"
)

// knownModels answers for a fixed set, so the validation tests do not need a
// data source to say whether "Post" exists.
func knownModels(names ...string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		for _, known := range names {
			if strings.EqualFold(known, name) {
				return known, true
			}
		}
		return "", false
	}
}

func noopRun(context.Context, ActionRequest) (ActionResult, error) { return ActionResult{}, nil }

// TestValidateModelActionsRefusesWhatWouldBeSilent covers the declarations
// that would otherwise produce a button nobody ever sees, or two functions
// behind one verb.
func TestValidateModelActionsRefusesWhatWouldBeSilent(t *testing.T) {
	cases := []struct {
		name    string
		actions []ModelAction
		want    string
	}{
		{name: "no name", actions: []ModelAction{{Model: "Post", Run: noopRun}}, want: "Name is required"},
		{name: "no model", actions: []ModelAction{{Name: "publish", Run: noopRun}}, want: "Model is required"},
		{name: "no run", actions: []ModelAction{{Name: "publish", Model: "Post"}}, want: "Run is required"},
		{name: "unknown model", actions: []ModelAction{{Name: "publish", Model: "Ghost", Run: noopRun}}, want: "no model named"},
		{
			name:    "reserved verb",
			actions: []ModelAction{{Name: "delete", Model: "Post", Run: noopRun}},
			want:    "verb the panel already uses",
		},
		{
			name:    "reserved record verb",
			actions: []ModelAction{{Name: "update", Model: "Post", Run: noopRun}},
			want:    "verb the panel already uses",
		},
		{
			name: "duplicate",
			actions: []ModelAction{
				{Name: "publish", Model: "Post", Run: noopRun},
				{Name: "Publish", Model: "Post", Run: noopRun},
			},
			want: "already declares an action",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateModelActions(tc.actions, knownModels("Post"))
			if err == nil {
				t.Fatalf("expected a refusal, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not explain %q", err, tc.want)
			}
		})
	}
}

// TestValidateModelActionsNormalises checks the two defaults a declaration
// may leave out, and that the verb is case-insensitive on the wire.
func TestValidateModelActionsNormalises(t *testing.T) {
	table, err := validateModelActions([]ModelAction{
		{Name: "Publish", Model: "post", Run: noopRun},
	}, knownModels("Post"))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	action, ok := table[actionKey{model: "Post", name: "publish"}]
	if !ok {
		t.Fatalf("action not keyed by its canonical model and lowercase verb: %+v", table)
	}
	if action.Label != "publish" {
		t.Fatalf("label should fall back to the verb, got %q", action.Label)
	}
}

// actionPanel is a panel with one declared action over the AdminUser model
// the test registry has, plus an RBAC enforcer, an audit ring and a server.
type actionPanel struct {
	panel *Panel
	srv   *httptest.Server
	calls *actionCalls
	auth  *testAdminAuth
}

type actionCalls struct {
	mu       sync.Mutex
	requests []ActionRequest
	fail     error
}

func (c *actionCalls) run(_ context.Context, req ActionRequest) (ActionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	if c.fail != nil {
		return ActionResult{}, c.fail
	}
	return ActionResult{Message: "done", Affected: len(req.IDs)}, nil
}

func (c *actionCalls) seen() []ActionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ActionRequest(nil), c.requests...)
}

func newActionPanel(t *testing.T, action ModelAction, calls *actionCalls) *actionPanel {
	t.Helper()
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	panel.rbac = enf
	panel.audit = newAuditStore(100)

	action.Run = calls.run
	table, err := validateModelActions([]ModelAction{action}, modelResolver(panel.src))
	if err != nil {
		t.Fatalf("validate declared action: %v", err)
	}
	panel.modelActions = table

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return &actionPanel{panel: panel, srv: srv, calls: calls, auth: provider}
}

// TestDeclaredActionRunsAndIsAudited is the happy path: the verb reaches the
// application's function with the selected ids, the answer reports what it
// changed, and the trail records it under its own verb.
func TestDeclaredActionRunsAndIsAudited(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser", Label: "Publish"}, &actionCalls{})
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "publish@example.com", "name": "Publish", "active": true,
	})

	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "publish", "ids": []interface{}{created.ID},
	})
	if status != http.StatusOK {
		t.Fatalf("declared action status=%d body=%v", status, resp)
	}
	if resp["ran"] != true || int(resp["affected"].(float64)) != 1 {
		t.Fatalf("the action did not report what it did: %v", resp)
	}
	if resp["message"] != "done" {
		t.Fatalf("the action's own message does not reach the operator: %v", resp)
	}

	seen := env.calls.seen()
	if len(seen) != 1 || len(seen[0].IDs) != 1 || seen[0].Model != "AdminUser" || seen[0].Actor != "actor" {
		t.Fatalf("the action was told the wrong thing about the call: %+v", seen)
	}

	entries := env.panel.audit.list(auditQueryOpts{PageSize: 200})
	found := false
	for _, entry := range entries {
		if entry.Action == "action.publish" && entry.ModelName == "AdminUser" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the trail has no entry for the action: %+v", entries)
	}
}

// TestDeclaredActionRefusedWithoutItsVerb: the action's name IS the
// permission, so an operator who holds every record verb but not this one is
// refused — and the application's function is never called.
func TestDeclaredActionRefusedWithoutItsVerb(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, &actionCalls{})
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "denied@example.com", "name": "Denied", "active": true,
	})
	// A plain operator with every record verb on the model, and nothing
	// about publish.
	env.auth.user = &auth.User{ID: "editor", Username: "editor", Role: "editor"}
	for _, verb := range recordActions {
		if err := env.panel.rbac.AddPolicy("editor", "admin:AdminUser", verb); err != nil {
			t.Fatalf("AddPolicy: %v", err)
		}
	}

	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "publish", "ids": []interface{}{created.ID},
	})
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for a verb the operator does not hold, got %d: %v", status, resp)
	}
	if calls := env.calls.seen(); len(calls) != 0 {
		t.Fatalf("a refused action still reached the application: %+v", calls)
	}
}

// TestDeclaredActionNeedsASelection keeps the default honest: a button on a
// grid means "these rows", so an empty selection is a bad request rather
// than a call over the whole table.
func TestDeclaredActionNeedsASelection(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, &actionCalls{})
	_, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "publish", "ids": []interface{}{},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty selection, got %d", status)
	}
	if calls := env.calls.seen(); len(calls) != 0 {
		t.Fatalf("the action ran with nothing selected: %+v", calls)
	}
}

// TestDeclaredActionOverTheWholeTable is the other half of that default: an
// action that says so runs with no ids.
func TestDeclaredActionOverTheWholeTable(t *testing.T) {
	env := newActionPanel(t, ModelAction{
		Name: "reindex", Model: "AdminUser", AllowEmptySelection: true,
	}, &actionCalls{})
	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "reindex",
	})
	if status != http.StatusOK {
		t.Fatalf("expected 200 for a selection-free action, got %d: %v", status, resp)
	}
	if calls := env.calls.seen(); len(calls) != 1 {
		t.Fatalf("the selection-free action did not run: %+v", calls)
	}
}

// TestUnknownActionIsStillABadRequest: the dispatch did not turn every typo
// into a 500 or a silent success.
func TestUnknownActionIsStillABadRequest(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, &actionCalls{})
	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "publsh", "ids": []interface{}{"1"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown verb, got %d: %v", status, resp)
	}
}

// TestDeclaredActionSurfacesItsOwnRefusal: the application's error reaches
// the operator, because "publish needs a publication date" is worth more
// than a generic failure on a console.
func TestDeclaredActionSurfacesItsOwnRefusal(t *testing.T) {
	calls := &actionCalls{fail: errActionTest}
	env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser", Label: "Publish"}, calls)
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "fails@example.com", "name": "Fails", "active": true,
	})
	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "publish", "ids": []interface{}{created.ID},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("expected the application's refusal as a 400, got %d: %v", status, resp)
	}
	if !strings.Contains(mustJSON(resp), "needs a publication date") {
		t.Fatalf("the application's reason did not reach the operator: %v", resp)
	}
	// It ran: the trail has to be able to tell "it errored" from "it never
	// started", because a half-done action still touched rows.
	var recorded bool
	for _, entry := range env.panel.audit.list(auditQueryOpts{PageSize: 200}) {
		if entry.Action == "action.publish" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("a failed action left no trace in the trail")
	}
}

// TestDeclaredActionAppearsInTheSchema: the grid learns about the button
// from the schema, and only when the operator may press it.
func TestDeclaredActionAppearsInTheSchema(t *testing.T) {
	env := newActionPanel(t, ModelAction{
		Name: "publish", Model: "AdminUser", Label: "Publish", Confirm: "Sure?", Destructive: true,
	}, &actionCalls{})

	schema, status := doJSON(t, http.MethodGet, env.srv.URL+"/api/models/AdminUser/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema status=%d", status)
	}
	raw := mustJSON(schema)
	for _, want := range []string{`"name":"publish"`, `"label":"Publish"`, `"confirm":"Sure?"`, `"requires_selection":true`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("the schema does not carry %s: %s", want, raw)
		}
	}

	// An operator without the verb is not told about the button at all: a
	// disabled control for a capability they cannot have is an invitation
	// to ask for it.
	env.auth.user = &auth.User{ID: "editor", Username: "editor", Role: "editor"}
	for _, verb := range append([]string{"get_schema"}, recordActions...) {
		if err := env.panel.rbac.AddPolicy("editor", "admin:AdminUser", verb); err != nil {
			t.Fatalf("AddPolicy: %v", err)
		}
	}
	schema, status = doJSON(t, http.MethodGet, env.srv.URL+"/api/models/AdminUser/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema status=%d", status)
	}
	if _, offered := schema["actions"]; offered {
		t.Fatalf("an operator without the verb is offered the action: %s", mustJSON(schema))
	}
	// The verb is still in the permission hints, as false, exactly like
	// every record verb they do not hold: that is what lets a screen say
	// "you cannot do this" instead of pretending the action does not
	// exist for anyone.
	perms, _ := schema["permissions"].(map[string]interface{})
	if held, listed := perms["publish"]; !listed || held != false {
		t.Fatalf("the hint map should carry publish=false: %v", perms)
	}
}

// errActionTest is the application's own refusal, in the application's own
// words.
var errActionTest = &testError{msg: "publish needs a publication date"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// TestDeclaredActionIsConfinedToTheOperatorsRows is the reason an action
// runs through the panel at all: an operator granted the verb over their OWN
// rows selects two, and the application's function is handed only theirs.
// Without this, a declared action would be the way around every row policy
// the panel enforces.
func TestDeclaredActionIsConfinedToTheOperatorsRows(t *testing.T) {
	panel, sqlDB, srv := ownedPanel(t, append(ownGrants("operator"),
		[3]string{"operator", "admin:OwnedNote#own", "publish"})...)

	calls := &actionCalls{}
	table, err := validateModelActions([]ModelAction{{
		Name: "publish", Model: "OwnedNote", Run: calls.run,
	}}, modelResolver(panel.src))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	panel.modelActions = table

	// Row 1 is the operator's, row 2 belongs to somebody else. The
	// selection asks for both.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote/bulk", map[string]interface{}{
		"action": "publish", "ids": []interface{}{"1", "2"},
	})
	if status != http.StatusOK {
		t.Fatalf("scoped action status=%d body=%v", status, resp)
	}
	seen := calls.seen()
	if len(seen) != 1 {
		t.Fatalf("expected one call, got %+v", seen)
	}
	if len(seen[0].IDs) != 1 || seen[0].IDs[0] != "1" {
		t.Fatalf("the action was handed rows outside the operator's scope: %+v", seen[0].IDs)
	}
	if int(resp["failed"].(float64)) != 1 {
		t.Fatalf("the refused row is not reported: %v", resp)
	}
	if ownedOwner(t, sqlDB, 2) != "somebody-else" {
		t.Fatalf("the other operator's row was touched")
	}
}

// TestDeclaredActionOverNothingItMayTouchDoesNotRun: a selection that is
// refused whole never reaches the application. An action told "no ids" would
// otherwise be indistinguishable from one invoked over the entire table.
func TestDeclaredActionOverNothingItMayTouchDoesNotRun(t *testing.T) {
	panel, _, srv := ownedPanel(t, append(ownGrants("operator"),
		[3]string{"operator", "admin:OwnedNote#own", "publish"})...)

	calls := &actionCalls{}
	table, err := validateModelActions([]ModelAction{{
		Name: "publish", Model: "OwnedNote", Run: calls.run,
	}}, modelResolver(panel.src))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	panel.modelActions = table

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote/bulk", map[string]interface{}{
		"action": "publish", "ids": []interface{}{"2"},
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, resp)
	}
	if resp["ran"] != false {
		t.Fatalf("the response should say the action did not run: %v", resp)
	}
	if calls := calls.seen(); len(calls) != 0 {
		t.Fatalf("the action ran over rows the operator may not touch: %+v", calls)
	}
}

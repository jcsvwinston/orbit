// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// recordActionURL is the record endpoint of an action on one AdminUser.
func recordActionURL(base string, id any, action string) string {
	return fmt.Sprintf("%s/api/models/AdminUser/actions/%s/%v", base, action, id)
}

// TestValidateActionPlacement covers what a placement may say: the zero
// value keeps an action where every action was before placements existed,
// a placement the panel cannot offer refuses to start, and so does a record
// action that claims it may run on nothing.
func TestValidateActionPlacement(t *testing.T) {
	table, err := validateModelActions([]ModelAction{
		{Name: "publish", Model: "Post", Run: noopRun},
		{Name: "refund", Model: "Post", Run: noopRun, Placement: "Record"},
		{Name: "archive", Model: "Post", Run: noopRun, Placement: ActionOnSelectionAndRecord},
	}, knownModels("Post"))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	for name, want := range map[string]ActionPlacement{
		"publish": ActionOnSelection, "refund": ActionOnRecord, "archive": ActionOnSelectionAndRecord,
	} {
		if got := table[actionKey{model: "Post", name: name}].Placement; got != want {
			t.Errorf("%s: placement %q, want %q", name, got, want)
		}
	}

	for label, action := range map[string]ModelAction{
		"an unknown placement": {Name: "refund", Model: "Post", Run: noopRun, Placement: "toolbar"},
		"a record action that may run on nothing": {
			Name: "refund", Model: "Post", Run: noopRun, Placement: ActionOnRecord, AllowEmptySelection: true,
		},
	} {
		_, err := validateModelActions([]ModelAction{action}, knownModels("Post"))
		if err == nil {
			t.Errorf("%s started", label)
			continue
		}
		if !strings.Contains(err.Error(), "Post.refund") {
			t.Errorf("%s was refused without naming the action: %v", label, err)
		}
	}
}

// TestActionPlacementOffers is the table the server enforces.
func TestActionPlacementOffers(t *testing.T) {
	cases := []struct {
		placement             ActionPlacement
		onSelection, onRecord bool
	}{
		{"", true, false},
		{ActionOnSelection, true, false},
		{ActionOnRecord, false, true},
		{ActionOnSelectionAndRecord, true, true},
	}
	for _, tc := range cases {
		if got := tc.placement.offers(ActionOnSelection); got != tc.onSelection {
			t.Errorf("%q offers selection = %v, want %v", tc.placement, got, tc.onSelection)
		}
		if got := tc.placement.offers(ActionOnRecord); got != tc.onRecord {
			t.Errorf("%q offers record = %v, want %v", tc.placement, got, tc.onRecord)
		}
	}
}

// TestRecordActionRunsOnThatRecord is the record placement's happy path: the
// function receives that one id, and the trail names the record, so its own
// history shows what was done to it.
func TestRecordActionRunsOnThatRecord(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "refund", Model: "AdminUser", Label: "Refund", Placement: ActionOnRecord}, &actionCalls{})
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "record@example.com", "name": "Record", "active": true,
	})
	id := fmt.Sprint(created.ID)

	resp, status := doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, id, "refund"), nil)
	if status != http.StatusOK {
		t.Fatalf("record action status=%d body=%v", status, resp)
	}
	if resp["ran"] != true || resp["message"] != "done" || resp["result"] != "message" {
		t.Fatalf("the record action did not report what it did: %v", resp)
	}
	seen := env.calls.seen()
	if len(seen) != 1 || len(seen[0].IDs) != 1 || seen[0].IDs[0] != id {
		t.Fatalf("the action was not handed exactly its record: %+v", seen)
	}

	var entry *AuditEntry
	for _, e := range env.panel.audit.list(auditQueryOpts{PageSize: 200}) {
		if e.Action == "action.refund" {
			e := e
			entry = &e
		}
	}
	if entry == nil {
		t.Fatal("the trail has no entry for the record action")
	}
	if entry.RecordID != id || entry.NewValue["on"] != "record" || entry.NewValue["result"] != "message" {
		t.Fatalf("the entry does not name the record, the placement and the answer: %+v", entry)
	}
	history, status := doJSON(t, http.MethodGet, env.srv.URL+"/api/models/AdminUser/"+id+"/history", nil)
	if status != http.StatusOK || !strings.Contains(mustJSON(history), "action.refund") {
		t.Fatalf("the record's history does not show the action (%d): %v", status, history)
	}
}

// TestActionPlacementIsEnforced: the placement is a declaration the server
// holds the action to. An action written for one record is never handed a
// selection, and one written for a selection is not reachable from a record.
func TestActionPlacementIsEnforced(t *testing.T) {
	t.Run("a record action at the bulk endpoint", func(t *testing.T) {
		env := newActionPanel(t, ModelAction{Name: "refund", Model: "AdminUser", Placement: ActionOnRecord}, &actionCalls{})
		created := createAdminUser(t, env.srv.URL, map[string]interface{}{"email": "r1@example.com", "name": "R1", "active": true})
		resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
			"action": "refund", "ids": []interface{}{created.ID},
		})
		if status != http.StatusBadRequest || !strings.Contains(mustJSON(resp), "offered on one record") {
			t.Fatalf("expected a 400 saying where the action is offered, got %d: %v", status, resp)
		}
		if calls := env.calls.seen(); len(calls) != 0 {
			t.Fatalf("a record action ran over a selection: %+v", calls)
		}
	})
	t.Run("a selection action at the record endpoint", func(t *testing.T) {
		env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, &actionCalls{})
		created := createAdminUser(t, env.srv.URL, map[string]interface{}{"email": "r2@example.com", "name": "R2", "active": true})
		resp, status := doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, created.ID, "publish"), nil)
		if status != http.StatusBadRequest || !strings.Contains(mustJSON(resp), "offered on a selection") {
			t.Fatalf("expected a 400 saying where the action is offered, got %d: %v", status, resp)
		}
		if calls := env.calls.seen(); len(calls) != 0 {
			t.Fatalf("a selection action ran from a record: %+v", calls)
		}
	})
	t.Run("an action offered in both places runs from both", func(t *testing.T) {
		env := newActionPanel(t, ModelAction{Name: "archive", Model: "AdminUser", Placement: ActionOnSelectionAndRecord}, &actionCalls{})
		created := createAdminUser(t, env.srv.URL, map[string]interface{}{"email": "r3@example.com", "name": "R3", "active": true})
		if _, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
			"action": "archive", "ids": []interface{}{created.ID},
		}); status != http.StatusOK {
			t.Fatalf("bulk: %d", status)
		}
		if _, status := doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, created.ID, "archive"), nil); status != http.StatusOK {
			t.Fatalf("record: %d", status)
		}
		if calls := env.calls.seen(); len(calls) != 2 {
			t.Fatalf("expected two calls, got %+v", calls)
		}
	})
}

// TestRecordActionPlacementInTheSchema: the schema says where each action
// belongs, always — an action declared before placements existed reads as
// the selection it has always been.
func TestRecordActionPlacementInTheSchema(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "refund", Model: "AdminUser", Placement: ActionOnRecord}, &actionCalls{})
	schema, status := doJSON(t, http.MethodGet, env.srv.URL+"/api/models/AdminUser/schema", nil)
	if status != http.StatusOK || !strings.Contains(mustJSON(schema), `"placement":"record"`) {
		t.Fatalf("the schema does not carry the placement (%d): %v", status, mustJSON(schema))
	}
	env = newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, &actionCalls{})
	schema, _ = doJSON(t, http.MethodGet, env.srv.URL+"/api/models/AdminUser/schema", nil)
	if !strings.Contains(mustJSON(schema), `"placement":"selection"`) {
		t.Fatalf("an action with no placement does not read as a selection action: %v", mustJSON(schema))
	}
}

// TestRecordActionGoesThroughTheSameChecks: the record endpoint is not a
// second, laxer door. The verb is the permission, a record that is not
// there is a 404, and the input is checked before the function runs.
func TestRecordActionGoesThroughTheSameChecks(t *testing.T) {
	t.Run("without its verb", func(t *testing.T) {
		env := newActionPanel(t, ModelAction{Name: "refund", Model: "AdminUser", Placement: ActionOnRecord}, &actionCalls{})
		created := createAdminUser(t, env.srv.URL, map[string]interface{}{"email": "c1@example.com", "name": "C1", "active": true})
		env.auth.user = &auth.User{ID: "editor", Username: "editor", Role: "editor"}
		for _, verb := range recordActions {
			if err := env.panel.rbac.AddPolicy("editor", "admin:AdminUser", verb); err != nil {
				t.Fatalf("AddPolicy: %v", err)
			}
		}
		if _, status := doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, created.ID, "refund"), nil); status != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", status)
		}
		if calls := env.calls.seen(); len(calls) != 0 {
			t.Fatalf("a refused record action reached the application: %+v", calls)
		}
	})
	t.Run("a record that is not there", func(t *testing.T) {
		env := newActionPanel(t, ModelAction{Name: "refund", Model: "AdminUser", Placement: ActionOnRecord}, &actionCalls{})
		resp, status := doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, 4242, "refund"), nil)
		if status != http.StatusNotFound {
			t.Fatalf("expected 404 for a record that does not exist, got %d: %v", status, resp)
		}
		if calls := env.calls.seen(); len(calls) != 0 {
			t.Fatalf("the action ran on a record that does not exist: %+v", calls)
		}
	})
	t.Run("its input", func(t *testing.T) {
		env := newActionPanel(t, ModelAction{
			Name: "refund", Model: "AdminUser", Placement: ActionOnRecord,
			Fields: []ActionField{{Name: "reason", Required: true}},
		}, &actionCalls{})
		created := createAdminUser(t, env.srv.URL, map[string]interface{}{"email": "c3@example.com", "name": "C3", "active": true})
		resp, status := doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, created.ID, "refund"), map[string]interface{}{
			"input": map[string]interface{}{},
		})
		if status != http.StatusUnprocessableEntity || !strings.Contains(mustJSON(resp), `"reason":"is required"`) {
			t.Fatalf("expected a 422 naming the field, got %d: %v", status, resp)
		}
		if calls := env.calls.seen(); len(calls) != 0 {
			t.Fatalf("a refused input reached the application: %+v", calls)
		}
		resp, status = doJSON(t, http.MethodPost, recordActionURL(env.srv.URL, created.ID, "refund"), map[string]interface{}{
			"input": map[string]interface{}{"reason": "parcel lost"},
		})
		if status != http.StatusOK {
			t.Fatalf("a valid input answered %d: %v", status, resp)
		}
		if calls := env.calls.seen(); len(calls) != 1 || calls[0].Input.String("reason") != "parcel lost" {
			t.Fatalf("the input did not reach the action: %+v", calls)
		}
	})
}

// TestRecordActionIsConfinedToTheOperatorsRows: an operator granted the verb
// over their OWN rows runs it on theirs, and on somebody else's gets the
// same 404 the record view would give them.
func TestRecordActionIsConfinedToTheOperatorsRows(t *testing.T) {
	panel, sqlDB, srv := ownedPanel(t, append(ownGrants("operator"),
		[3]string{"operator", "admin:OwnedNote#own", "refund"})...)
	calls := &actionCalls{}
	table, err := validateModelActions([]ModelAction{{
		Name: "refund", Model: "OwnedNote", Run: calls.run, Placement: ActionOnRecord,
	}}, modelResolver(panel.src))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	panel.modelActions = table

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote/actions/refund/2", nil)
	if status != http.StatusNotFound {
		t.Fatalf("somebody else's row answered %d: %v", status, resp)
	}
	if len(calls.seen()) != 0 {
		t.Fatalf("the action ran on a row the operator may not touch: %+v", calls.seen())
	}
	if ownedOwner(t, sqlDB, 2) != "somebody-else" {
		t.Fatal("the other operator's row was touched")
	}
	if _, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/OwnedNote/actions/refund/1", nil); status != http.StatusOK {
		t.Fatalf("the operator's own row answered %d", status)
	}
	if seen := calls.seen(); len(seen) != 1 || seen[0].IDs[0] != "1" {
		t.Fatalf("the action was not handed the operator's row: %+v", seen)
	}
}

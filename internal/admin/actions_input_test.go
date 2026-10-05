// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// scheduleFields is the form the tests declare: one input of each type the
// panel draws.
func scheduleFields() []ActionField {
	return []ActionField{
		{Name: "reason", Label: "Reason", Required: true, Help: "Shown in the audit trail"},
		{Name: "priority", Type: ActionFieldNumber},
		{Name: "notify", Type: ActionFieldBoolean},
		{Name: "channel", Type: ActionFieldSelect, Required: true, Options: []ActionOption{
			{Value: "web", Label: "Website"}, {Value: "email"},
		}},
		{Name: "publish_on", Label: "Publish on", Type: ActionFieldDate},
	}
}

// TestValidateActionFieldsRefusesWhatItCannotDraw: a field the panel cannot
// draw, or whose choices make no sense, stops the application at startup —
// naming the action and the field — rather than becoming a form that cannot
// be filled in.
func TestValidateActionFieldsRefusesWhatItCannotDraw(t *testing.T) {
	cases := []struct {
		name   string
		fields []ActionField
		want   string
	}{
		{name: "unknown type", fields: []ActionField{{Name: "when", Type: "datetime"}}, want: `field "when": unknown type "datetime"`},
		{name: "no name", fields: []ActionField{{Type: ActionFieldText}}, want: "fields[0]: Name is required"},
		{name: "a name a form cannot carry", fields: []ActionField{{Name: "two words"}}, want: `field "two words": a name starts with a letter`},
		{name: "declared twice", fields: []ActionField{{Name: "reason"}, {Name: "reason"}}, want: `field "reason" is declared twice`},
		{name: "select with no options", fields: []ActionField{{Name: "channel", Type: ActionFieldSelect}}, want: `field "channel": a select needs at least one option`},
		{name: "option with no value", fields: []ActionField{{Name: "channel", Type: ActionFieldSelect, Options: []ActionOption{{Label: "Web"}}}}, want: `field "channel": options[0] has no Value`},
		{name: "option declared twice", fields: []ActionField{{Name: "channel", Type: ActionFieldSelect, Options: []ActionOption{{Value: "web"}, {Value: "web"}}}}, want: `field "channel": option "web" is declared twice`},
		{name: "options on a text field", fields: []ActionField{{Name: "reason", Options: []ActionOption{{Value: "a"}}}}, want: `field "reason": options belong to a select`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateModelActions([]ModelAction{
				{Name: "schedule", Model: "Post", Run: noopRun, Fields: tc.fields},
			}, knownModels("Post"))
			if err == nil {
				t.Fatalf("expected a refusal, got none")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Post.schedule") {
				t.Fatalf("error %q does not name the action and explain %q", err, tc.want)
			}
		})
	}
}

// TestValidateActionFieldsNormalises: the defaults a declaration may leave
// out — a type, a label, an option's label — are filled in once, at startup.
func TestValidateActionFieldsNormalises(t *testing.T) {
	table, err := validateModelActions([]ModelAction{
		{Name: "schedule", Model: "Post", Run: noopRun, Fields: []ActionField{
			{Name: "reason"},
			{Name: "channel", Type: "Select", Options: []ActionOption{{Value: "web"}}},
		}},
	}, knownModels("Post"))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	fields := table[actionKey{model: "Post", name: "schedule"}].Fields
	if fields[0].Type != ActionFieldText || fields[0].Label != "reason" {
		t.Fatalf("an untyped field should be text labelled by its name: %+v", fields[0])
	}
	if fields[1].Type != ActionFieldSelect || fields[1].Options[0].Label != "web" {
		t.Fatalf("the type should be read case-insensitively and the option labelled by its value: %+v", fields[1])
	}
}

// TestActionFieldsAreInTheSchema: the form is drawn from the schema, so the
// schema carries every field with what a form needs — and an action that
// asks for nothing carries no fields key at all, which is the UI's cue to
// keep the plain confirmation.
func TestActionFieldsAreInTheSchema(t *testing.T) {
	env := newActionPanel(t, ModelAction{Name: "schedule", Model: "AdminUser", Fields: scheduleFields()}, &actionCalls{})
	schema, status := doJSON(t, http.MethodGet, env.srv.URL+"/api/models/AdminUser/schema", nil)
	if status != http.StatusOK {
		t.Fatalf("schema status=%d", status)
	}
	actions, _ := schema["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("the schema should offer one action: %v", schema["actions"])
	}
	fields, _ := actions[0].(map[string]any)["fields"].([]any)
	got := map[string]string{}
	for _, entry := range fields {
		got[entry.(map[string]any)["name"].(string)] = mustJSON(entry)
	}
	// mustJSON writes keys in order, which is what makes these comparable.
	for name, want := range map[string]string{
		"reason":     `{"help":"Shown in the audit trail","label":"Reason","name":"reason","required":true,"type":"text"}`,
		"priority":   `{"label":"priority","name":"priority","required":false,"type":"number"}`,
		"notify":     `{"label":"notify","name":"notify","required":false,"type":"boolean"}`,
		"channel":    `{"label":"channel","name":"channel","options":[{"label":"Website","value":"web"},{"label":"email","value":"email"}],"required":true,"type":"select"}`,
		"publish_on": `{"label":"Publish on","name":"publish_on","required":false,"type":"date"}`,
	} {
		if got[name] != want {
			t.Fatalf("field %s is published as %s, want %s", name, got[name], want)
		}
	}
	if order := mustJSON(fields); strings.Index(order, `"reason"`) > strings.Index(order, `"publish_on"`) {
		t.Fatalf("the fields should keep their declared order: %s", order)
	}

	plain := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, &actionCalls{})
	schema, _ = doJSON(t, http.MethodGet, plain.srv.URL+"/api/models/AdminUser/schema", nil)
	if strings.Contains(mustJSON(schema["actions"]), `"fields"`) {
		t.Fatalf("an action with no fields publishes a fields key: %s", mustJSON(schema["actions"]))
	}
}

// TestActionInputIsRefusedBeforeRun: what the declaration refuses is
// answered as a 422 naming every field at fault, and the application's
// function is never called — the form is a rendering of this check, not a
// substitute for it.
func TestActionInputIsRefusedBeforeRun(t *testing.T) {
	calls := &actionCalls{}
	env := newActionPanel(t, ModelAction{Name: "schedule", Model: "AdminUser", Label: "Schedule", Fields: scheduleFields()}, calls)
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "schedule@example.com", "name": "Schedule", "active": true,
	})

	cases := []struct {
		name  string
		input any
		want  map[string]string
	}{
		{name: "nothing posted", input: nil, want: map[string]string{"reason": "is required", "channel": "is required"}},
		{name: "blank text", input: map[string]any{"reason": "   ", "channel": "web"}, want: map[string]string{"reason": "is required"}},
		{name: "a number that is not one", input: map[string]any{"reason": "r", "channel": "web", "priority": "high"}, want: map[string]string{"priority": "must be a number"}},
		{name: "an option that is not offered", input: map[string]any{"reason": "r", "channel": "fax"}, want: map[string]string{"channel": "must be one of web, email"}},
		{name: "a date that is not one", input: map[string]any{"reason": "r", "channel": "web", "publish_on": "05/10/2026"}, want: map[string]string{"publish_on": "must be a date (YYYY-MM-DD)"}},
		{name: "a boolean that is not one", input: map[string]any{"reason": "r", "channel": "web", "notify": "maybe"}, want: map[string]string{"notify": "must be true or false"}},
		{name: "text that is not text", input: map[string]any{"reason": 42, "channel": "web"}, want: map[string]string{"reason": "must be text"}},
		{name: "a key nobody declared", input: map[string]any{"reason": "r", "channel": "web", "force": true}, want: map[string]string{"force": "is not a field of this action"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"action": "schedule", "ids": []any{created.ID}}
			if tc.input != nil {
				body["input"] = tc.input
			}
			resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", body)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %v", status, resp)
			}
			errBody, _ := resp["error"].(map[string]any)
			details, _ := errBody["details"].(map[string]any)
			if len(details) != len(tc.want) {
				t.Fatalf("details name %v, want exactly %v", details, tc.want)
			}
			for field, problem := range tc.want {
				if details[field] != problem {
					t.Fatalf("details[%s] = %v, want %q (all: %v)", field, details[field], problem, details)
				}
				if msg, _ := errBody["message"].(string); !strings.Contains(msg, field+" "+problem) {
					t.Fatalf("the message does not name %s: %q", field, msg)
				}
			}
		})
	}
	if seen := calls.seen(); len(seen) != 0 {
		t.Fatalf("a refused input reached the application: %+v", seen)
	}

	// Not an object at all is the same refusal at the level of the body.
	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]any{
		"action": "schedule", "ids": []any{created.ID}, "input": "reason=r",
	})
	if status != http.StatusBadRequest || !strings.Contains(mustJSON(resp), "input must be an object") {
		t.Fatalf("a non-object input should be a 400 that says so, got %d: %v", status, resp)
	}
}

// TestActionInputReachesRunTyped: what passes the check arrives with the
// type its field declared, so the application's function never parses a
// string the panel already parsed — and the trail records what was entered.
func TestActionInputReachesRunTyped(t *testing.T) {
	calls := &actionCalls{}
	env := newActionPanel(t, ModelAction{Name: "schedule", Model: "AdminUser", Label: "Schedule", Fields: scheduleFields()}, calls)
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "typed@example.com", "name": "Typed", "active": true,
	})

	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]any{
		"action": "schedule", "ids": []any{created.ID},
		"input": map[string]any{
			"reason": "the parcel never arrived", "priority": "2.5", "notify": true,
			"channel": "email", "publish_on": "2026-10-05",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("a valid input was refused: %d %v", status, resp)
	}
	seen := calls.seen()
	if len(seen) != 1 {
		t.Fatalf("the action did not run once: %+v", seen)
	}
	in := seen[0].Input
	if in.String("reason") != "the parcel never arrived" || in.String("channel") != "email" {
		t.Fatalf("text and select should arrive as strings: %#v", in)
	}
	if n, ok := in.Number("priority"); !ok || n != 2.5 {
		t.Fatalf("a number should arrive as float64 2.5: %#v", in["priority"])
	}
	if !in.Bool("notify") {
		t.Fatalf("a ticked box should arrive as true: %#v", in["notify"])
	}
	if d, ok := in.Date("publish_on"); !ok || !d.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("a date should arrive as midnight UTC of that day: %#v", in["publish_on"])
	}

	var recorded map[string]any
	for _, entry := range env.panel.audit.list(auditQueryOpts{PageSize: 200}) {
		if entry.Action == "action.schedule" {
			recorded = entry.NewValue
		}
	}
	entered, _ := recorded["input"].(map[string]any)
	if entered["reason"] != "the parcel never arrived" || entered["publish_on"] != "2026-10-05" {
		t.Fatalf("the trail should record what was entered, dates as the day: %v", recorded)
	}
}

// TestActionInputOptionalFieldsMayBeLeftEmpty: an optional field left empty
// is absent, not a zero the application cannot tell from a real zero — and
// an unticked box is false.
func TestActionInputOptionalFieldsMayBeLeftEmpty(t *testing.T) {
	calls := &actionCalls{}
	env := newActionPanel(t, ModelAction{Name: "schedule", Model: "AdminUser", Fields: scheduleFields()}, calls)
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "optional@example.com", "name": "Optional", "active": true,
	})
	_, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]any{
		"action": "schedule", "ids": []any{created.ID},
		"input": map[string]any{"reason": "r", "channel": "web", "priority": "", "publish_on": nil},
	})
	if status != http.StatusOK {
		t.Fatalf("empty optional fields were refused: %d", status)
	}
	in := calls.seen()[0].Input
	if _, ok := in.Number("priority"); ok {
		t.Fatalf("an empty number should be absent: %#v", in)
	}
	if _, ok := in.Date("publish_on"); ok {
		t.Fatalf("an empty date should be absent: %#v", in)
	}
	if v, present := in["notify"]; !present || v != false {
		t.Fatalf("an unticked box should be present and false: %#v", in)
	}
}

// TestRequiredBooleanMeansTicked: on a yes/no, "required" is the box an
// operator must tick before a dangerous action runs.
func TestRequiredBooleanMeansTicked(t *testing.T) {
	calls := &actionCalls{}
	env := newActionPanel(t, ModelAction{Name: "purge", Model: "AdminUser", Fields: []ActionField{
		{Name: "understood", Type: ActionFieldBoolean, Required: true},
	}}, calls)
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "purge@example.com", "name": "Purge", "active": true,
	})
	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]any{
		"action": "purge", "ids": []any{created.ID}, "input": map[string]any{"understood": false},
	})
	if status != http.StatusUnprocessableEntity || !strings.Contains(mustJSON(resp), "must be ticked") {
		t.Fatalf("an unticked required box should be refused: %d %v", status, resp)
	}
	if len(calls.seen()) != 0 {
		t.Fatal("the action ran without the box ticked")
	}
}

// TestActionWithoutFieldsIgnoresInput is the compatibility promise: an
// action that declares no fields runs exactly as it did before actions
// could ask — what a client posts beside the call is not its business, and
// Run sees no input.
func TestActionWithoutFieldsIgnoresInput(t *testing.T) {
	calls := &actionCalls{}
	env := newActionPanel(t, ModelAction{Name: "publish", Model: "AdminUser"}, calls)
	created := createAdminUser(t, env.srv.URL, map[string]interface{}{
		"email": "plain@example.com", "name": "Plain", "active": true,
	})
	resp, status := doJSON(t, http.MethodPost, env.srv.URL+"/api/models/AdminUser/bulk", map[string]any{
		"action": "publish", "ids": []any{created.ID}, "input": map[string]any{"anything": "at all"},
	})
	if status != http.StatusOK {
		t.Fatalf("an action without fields refused a call it used to run: %d %v", status, resp)
	}
	if seen := calls.seen(); len(seen) != 1 || seen[0].Input != nil {
		t.Fatalf("an action without fields should run with no input: %+v", seen)
	}
}

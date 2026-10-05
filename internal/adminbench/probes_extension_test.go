// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jcsvwinston/orbit"
)

// The extension family (A11, "extensibility and catalog"): what an
// application adds to the panel beyond what A6 shipped.
//
// A6 left four extension points, each measured present: an action of the
// application's own over a selection (DS-09), the brand's logo, favicon and
// accent (CUST-02), cards on the overview (CUST-03) and a screen of the
// application's own (CUST-04). This family asks what an application that
// leans on those points asks next — a form before the action, the action on
// the record it is about, a chart, a second screen of cards, its own script
// and its own field renderer, the theme its operators open on — and two
// properties every extension point should have and these may not: a
// declaration the panel cannot honour stops the application, and what the
// configuration accepts actually reaches the browser.
//
// Most of these are absences on the day of the baseline. Each probe reads
// two things: what the panel SERVES (a payload, a document, a header, the
// application's own function being called or not) and the contract types an
// application writes against. When a contract type or the mount surface
// grows a member that could carry the capability, the probe answers partial
// — the suite goes red against the recorded absent — and that is the moment
// to grow it into the behaviour check the new surface makes possible. An
// absence is never decided by the missing name alone.

// The members of the contract types as A6 left them. A field outside these
// sets is a surface that moved. The action's own lists went when EXT-01,
// EXT-02 and EXT-03 grew into behaviour checks (O3, O4), and the widget and
// page lists when EXT-04 and EXT-05 did (O5); knownActionResult stays as A6
// left it, because EXT-03 starts by asking what grew beyond it.
var knownActionResult = []string{"Message", "Affected", "Data"}

// noteActionDescriptor returns one of Note's actions as its schema offers it
// to the superuser.
func noteActionDescriptor(t *testing.T, e *env, name string) (map[string]any, bool) {
	t.Helper()
	schema := e.get(t, "/admin/api/models/Note/schema")
	if schema.code != http.StatusOK {
		t.Fatalf("Note schema answered %d: %s", schema.code, schema.text())
	}
	actions, _ := schema.json(t)["actions"].([]any)
	for _, entry := range actions {
		if d, ok := entry.(map[string]any); ok && d["name"] == name {
			return d, true
		}
	}
	return nil, false
}

// EXT-01: an action asks the operator for something before it runs — the
// reason for a refund, the date to publish on. Django's intermediate pages
// and Filament's action forms both do it; a verb with no input is the half
// of an action that needs nothing from the person running it.
//
// Measured as four things an author relies on, each by effect: the form a
// UI draws is in the schema (every type the panel offers, with the select's
// options); a field the panel cannot draw stops the application, naming the
// field; what the declaration refuses is answered as a 4xx naming each field
// at fault and never reaches the function; and what it accepts reaches the
// function with the type its field declared. The browser half — the dialog
// that draws the form and shows the error on the field — is UIX-09.
func probeActionInput(t *testing.T, e *env) verdict {
	d, ok := noteActionDescriptor(t, e, "schedule")
	if !ok {
		t.Fatalf("the schema offers no schedule action, which the bench's application declares; DS-09 should be red too")
	}
	fields, _ := d["fields"].([]any)
	if len(fields) == 0 {
		t.Logf("the schedule action declares five fields and its descriptor publishes none (%v): nothing for a UI to draw", topLevelKeys(d))
		return absent
	}

	// 1. The form, as a UI reads it.
	types := map[string]string{}
	var options []any
	for _, entry := range fields {
		f, _ := entry.(map[string]any)
		types[fmt.Sprint(f["name"])] = fmt.Sprint(f["type"])
		if f["name"] == "channel" {
			options, _ = f["options"].([]any)
		}
	}
	want := map[string]string{"reason": "text", "priority": "number", "notify": "boolean", "channel": "select", "publish_on": "date"}
	if !reflect.DeepEqual(types, want) || len(options) != 2 {
		t.Logf("the descriptor publishes %v with %d option(s); the declaration is %v with 2", types, len(options), want)
		return partial
	}

	// 2. A field the panel cannot draw refuses to start, naming it.
	for label, bad := range map[string]orbit.ActionField{
		"an unknown type":         {Name: "when", Type: "datetime"},
		"a select with no option": {Name: "when", Type: orbit.ActionFieldSelect},
	} {
		action := scheduleAction()
		action.Fields = []orbit.ActionField{bad}
		_, err := tryStart(t, extensionApp(t, orbit.Config{Title: "Admin Bench (action fields)", Actions: []orbit.ModelAction{action}}))
		if err == nil {
			t.Logf("the application started with %s on an action field: the form would be one nobody can fill", label)
			return partial
		}
		if !strings.Contains(err.Error(), `"when"`) {
			t.Logf("the panel refused %s without naming the field: %v", label, err)
			return partial
		}
	}

	// 3. What the declaration refuses never reaches the function, and the
	// answer names every field at fault.
	id := e.createNote(t, map[string]any{"title": "action-input", "status": "draft"})
	_, before := benchExtensions.lastRequest()
	refused := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk", map[string]any{
		"action": "schedule", "ids": []string{id},
		"input": map[string]any{"priority": "high", "channel": "fax", "publish_on": "tomorrow"},
	})
	if _, after := benchExtensions.lastRequest(); after != before {
		t.Logf("an input with four mistakes reached the action (answered %d): the check, if any, runs after the function", refused.code)
		return partial
	}
	if refused.code < 400 || refused.code >= 500 {
		t.Logf("an input with four mistakes answered %d: %s", refused.code, refused.text())
		return partial
	}
	errBody, _ := refused.json(t)["error"].(map[string]any)
	details, _ := errBody["details"].(map[string]any)
	for _, field := range []string{"reason", "priority", "channel", "publish_on"} {
		if _, named := details[field]; !named {
			t.Logf("the refusal (%d) does not name %s: %s", refused.code, field, refused.text())
			return partial
		}
	}

	// 4. What it accepts reaches the function typed.
	const marker = "ext01-operator-typed-this"
	ran := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk", map[string]any{
		"action": "schedule", "ids": []string{id},
		"input": map[string]any{"reason": marker, "priority": "3", "notify": true, "channel": "email", "publish_on": "2026-10-05"},
	})
	if ran.code != http.StatusOK {
		t.Logf("a valid input answered %d: %s", ran.code, ran.text())
		return partial
	}
	call, after := benchExtensions.lastRequest()
	if after == before {
		t.Logf("a valid input did not reach the action: %s", ran.text())
		return partial
	}
	in := call.Input
	day, isDate := in.Date("publish_on")
	priority, isNumber := in.Number("priority")
	if in.String("reason") != marker || in.String("channel") != "email" || !in.Bool("notify") ||
		!isNumber || priority != 3 || !isDate || day.Format("2006-01-02") != "2026-10-05" {
		t.Logf("the action received %#v: not the values posted, in their declared types", in)
		return partial
	}
	return present
}

// EXT-02: the action is offered on the record it is about. Selecting one
// row in a grid to press a button is the workaround; the record view is
// where an operator already is when they decide to refund this order.
//
// Measured as what the drawing half (UIX-08) relies on and what a UI cannot
// fake: the schema says where each action belongs; a record action runs on
// that record alone, through an endpoint of its own; that endpoint asks the
// same questions as the bulk one — the verb, the input, the trail — and the
// placement is enforced, so a record action is never handed a selection and
// a selection action is not reachable from a record. An action declared
// before placements existed keeps its place (publish).
func probeActionOnRecord(t *testing.T, e *env) verdict {
	// 1. Where each action belongs, as the schema a UI reads says it.
	schema := e.get(t, "/admin/api/models/Note/schema")
	if schema.code != http.StatusOK {
		t.Fatalf("Note schema answered %d: %s", schema.code, schema.text())
	}
	placements := map[string]string{}
	actions, _ := schema.json(t)["actions"].([]any)
	for _, entry := range actions {
		if d, ok := entry.(map[string]any); ok {
			if placement, ok := d["placement"].(string); ok {
				placements[fmt.Sprint(d["name"])] = placement
			}
		}
	}
	if len(placements) == 0 {
		t.Logf("no action descriptor says where it belongs (%d actions): a record view cannot tell which are its own", len(actions))
		return absent
	}
	want := map[string]string{"publish": "selection", "schedule": "selection_and_record", "duplicate": "record", "download_text": "selection_and_record"}
	if !reflect.DeepEqual(placements, want) {
		t.Logf("the schema places the actions %v; the application declared %v", placements, want)
		return partial
	}

	// 2. A record action runs on that record, alone.
	id := e.createNote(t, map[string]any{"title": "action-on-record", "status": "draft"})
	_, before := benchExtensions.lastRequest()
	ran := e.do(t, http.MethodPost, "/admin/api/models/Note/actions/duplicate/"+id, nil)
	call, after := benchExtensions.lastRequest()
	if ran.code != http.StatusOK || after == before {
		t.Logf("the record endpoint answered %d and the action ran %d time(s): %s", ran.code, after-before, ran.text())
		return partial
	}
	if len(call.IDs) != 1 || call.IDs[0] != id {
		t.Logf("the record action was handed %v, not the record %s", call.IDs, id)
		return partial
	}

	// 3. The placement is enforced both ways, and an action declared before
	// placements existed is still a selection action.
	_, before = benchExtensions.lastRequest()
	onBulk := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk", map[string]any{"action": "duplicate", "ids": []string{id}})
	onRecord := e.do(t, http.MethodPost, "/admin/api/models/Note/actions/publish/"+id, nil)
	if _, after := benchExtensions.lastRequest(); after != before || onBulk.code < 400 || onRecord.code < 400 {
		t.Logf("a record action over a selection answered %d, a selection action on a record %d, and %d of them ran: the placement is drawn, not enforced",
			onBulk.code, onRecord.code, after-before)
		return partial
	}
	if published := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk", map[string]any{"action": "publish", "ids": []string{id}}); published.code != http.StatusOK {
		t.Logf("publish, declared with no placement, no longer runs over a selection (%d): %s", published.code, published.text())
		return partial
	}

	// 4. The same input check: what the declaration refuses never reaches
	// the function, and the answer names the fields.
	_, before = benchExtensions.lastRequest()
	refused := e.do(t, http.MethodPost, "/admin/api/models/Note/actions/schedule/"+id,
		map[string]any{"input": map[string]any{"channel": "fax"}})
	if _, after := benchExtensions.lastRequest(); after != before || refused.code != http.StatusUnprocessableEntity {
		t.Logf("a bad input on the record endpoint answered %d and reached the action %d time(s): %s", refused.code, after-before, refused.text())
		return partial
	}
	errBody, _ := refused.json(t)["error"].(map[string]any)
	details, _ := errBody["details"].(map[string]any)
	if _, named := details["channel"]; !named {
		t.Logf("the record endpoint's refusal does not name the field: %s", refused.text())
		return partial
	}

	// 5. The same verb: an operator without it is refused and the function
	// never runs; the grant is what lets them.
	op := e.operatorNamed(t, "record-action-operator")
	_, before = benchExtensions.lastRequest()
	denied := e.asOperator(t, op, http.MethodPost, "/admin/api/models/Note/actions/duplicate/"+id, nil)
	if _, after := benchExtensions.lastRequest(); after != before || denied.code != http.StatusForbidden {
		t.Logf("an operator with no grant ran a record action: answered %d, ran %d time(s)", denied.code, after-before)
		return partial
	}
	e.grant(t, op.username, "admin:Note", "duplicate")
	if granted := e.asOperator(t, op, http.MethodPost, "/admin/api/models/Note/actions/duplicate/"+id, nil); granted.code != http.StatusOK {
		t.Logf("the grant did not open the record action (%d): %s", granted.code, granted.text())
		return partial
	}

	// 6. The same trail, read where it matters: the record's own history
	// says what was done to it, from where, and by whom.
	history := e.get(t, "/admin/api/models/Note/"+id+"/history")
	for _, raw := range historyEntries(t, history) {
		if raw["action"] != "action.duplicate" {
			continue
		}
		values, _ := raw["new_value"].(map[string]any)
		if values["on"] == "record" && raw["record_id"] == id {
			return present
		}
	}
	t.Logf("the record's history has no entry for the action run on it: %s", history.text())
	return partial
}

// historyEntries reads a record's history payload.
func historyEntries(t *testing.T, r response) []map[string]any {
	t.Helper()
	if r.code != http.StatusOK {
		t.Logf("the record's history answered %d: %s", r.code, r.text())
		return nil
	}
	raw, _ := r.json(t)["entries"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

// EXT-03: the action answers with something to download or a page to open —
// "export these invoices as PDF", "open the reconciliation for this batch".
//
// Measured by effect, both ways. What an action is allowed to answer arrives:
// the redirect is a page of the panel that exists (the copy the action
// made), the file is an attachment with the declared name, type and bytes,
// from one record and from a selection, and the trail records which kind of
// answer each call gave. What it is not allowed to answer does not: a
// redirect out of the panel, steered by what an operator typed, and a file
// over the ceiling are refused — after the action ran, and saying so.
func probeActionResultKinds(t *testing.T, e *env) verdict {
	if hits := fieldsBeyond(reflect.TypeOf(orbit.ActionResult{}), knownActionResult,
		"redirect", "download", "url", "file", "location", "navigate", "open", "link", "attachment"); len(hits) == 0 {
		t.Log("ActionResult is a message, a count and an untyped Data map: no member a screen could follow")
		return absent
	}
	id := e.createNote(t, map[string]any{"title": "ext03-source", "body": "ext03 body", "status": "draft"})

	// 1. A page of the panel: the copy the action made.
	r := e.do(t, http.MethodPost, "/admin/api/models/Note/actions/duplicate/"+id, nil)
	if r.code != http.StatusOK {
		t.Logf("duplicate answered %d: %s", r.code, r.text())
		return partial
	}
	answer := r.json(t)
	target, _ := answer["redirect"].(string)
	const page = "/data-studio?model=Note&record="
	if answer["result"] != "redirect" || !strings.HasPrefix(target, page) {
		t.Logf("the answer is %v: no redirect a screen could follow", answer)
		return partial
	}
	copied := e.get(t, "/admin/api/models/Note/"+strings.TrimPrefix(target, page))
	if copied.code != http.StatusOK || !strings.Contains(copied.raw(), "Copy of ext03-source") {
		t.Logf("the redirect %q names no record the panel serves (%d): %s", target, copied.code, copied.text())
		return partial
	}

	// 2. A file, from the record and from a selection.
	second := e.createNote(t, map[string]any{"title": "ext03-second", "status": "draft"})
	for _, call := range []struct {
		path, body, name string
		holds            []string
	}{
		{"/admin/api/models/Note/actions/download_text/" + id, `{}`, "note-" + id + ".txt", []string{"ext03-source", "ext03 body"}},
		{"/admin/api/models/Note/bulk", `{"action":"download_text","ids":["` + id + `","` + second + `"]}`, "notes.txt", []string{"ext03-source", "ext03-second"}},
	} {
		file, header := fetchPost(t, e, call.path, call.body)
		disposition, params, err := mime.ParseMediaType(header.Get("Content-Disposition"))
		if file.code != http.StatusOK || err != nil || disposition != "attachment" || params["filename"] != call.name {
			t.Logf("%s answered %d with Content-Disposition %q: not the attachment %s", call.path, file.code, header.Get("Content-Disposition"), call.name)
			return partial
		}
		if !strings.HasPrefix(header.Get("Content-Type"), "text/plain") || header.Get("X-Content-Type-Options") != "nosniff" {
			t.Logf("the file is served as %q (nosniff %q): not the type the action declared", header.Get("Content-Type"), header.Get("X-Content-Type-Options"))
			return partial
		}
		for _, want := range call.holds {
			if !strings.Contains(file.raw(), want) {
				t.Logf("the file does not hold %q: %s", want, file.text())
				return partial
			}
		}
	}

	// 3. The trail records which kind of answer each call gave.
	kinds := map[string]string{}
	for _, entry := range historyEntries(t, e.get(t, "/admin/api/models/Note/"+id+"/history")) {
		values, _ := entry["new_value"].(map[string]any)
		if action, _ := entry["action"].(string); strings.HasPrefix(action, "action.") {
			kinds[action] = fmt.Sprint(values["result"])
		}
	}
	if kinds["action.duplicate"] != "redirect" || kinds["action.download_text"] != "download" {
		t.Logf("the record's history records the answers as %v", kinds)
		return partial
	}

	// 4. What the panel will not send, from an application that tries.
	return refusedAnswers(t)
}

// refusedAnswers boots an application whose actions answer with what the
// panel must not send — a redirect whose target the operator typed, and a
// file over the ceiling — and checks each is refused after it ran, with
// nothing of it reaching the client.
func refusedAnswers(t *testing.T) verdict {
	var ran atomic.Int32
	steered := orbit.ModelAction{
		Name: "open", Model: "Note", Label: "Open", Placement: orbit.ActionOnRecord,
		Fields: []orbit.ActionField{{Name: "target", Required: true}},
		Run: func(_ context.Context, req orbit.ActionRequest) (orbit.ActionResult, error) {
			ran.Add(1)
			return orbit.ActionResult{Redirect: req.Input.String("target")}, nil
		},
	}
	oversized := orbit.ModelAction{
		Name: "dump", Model: "Note", Label: "Dump", Placement: orbit.ActionOnRecord,
		Run: func(context.Context, orbit.ActionRequest) (orbit.ActionResult, error) {
			ran.Add(1)
			return orbit.ActionResult{Download: &orbit.ActionDownload{
				Filename: "dump.bin", ContentType: "application/octet-stream",
				Body: io.LimitReader(zeroes{}, 32<<20+1),
			}}, nil
		},
	}
	app, err := tryStart(t, extensionApp(t, orbit.Config{Title: "Admin Bench (answers)", Actions: []orbit.ModelAction{steered, oversized}}))
	if err != nil {
		t.Fatalf("the application with two valid actions refused to start: %v", err)
	}
	client := signInAt(t, app, "admin", bootstrapPassword)
	created, _ := postAt(t, client, app.URL("/admin/api/models/Note"), map[string]any{"title": "answers", "status": "draft"})
	if created.code >= 300 {
		t.Fatalf("create Note answered %d: %s", created.code, created.text())
	}
	id := recordID(t, created.json(t))

	// "///host" is the one a URL parser and a browser disagree on: Go
	// reads no host in it, a browser reads evil.example.
	for _, target := range []string{"https://evil.example/login", "//evil.example/login", "///evil.example/login", `/\evil.example`, "/../../elsewhere"} {
		before := ran.Load()
		r, header := postAt(t, client, app.URL("/admin/api/models/Note/actions/open/"+id), map[string]any{"input": map[string]any{"target": target}})
		if ran.Load() == before {
			t.Fatalf("the open action did not run for %q (%d): %s", target, r.code, r.text())
		}
		if r.code < 500 || strings.Contains(r.raw(), `"redirect"`) || header.Get("Location") != "" {
			t.Logf("a redirect to %q the operator typed answered %d: %s", target, r.code, r.text())
			return partial
		}
		if !strings.Contains(r.raw(), "ran, and its answer was refused") {
			t.Logf("the refusal of %q does not say the action ran: %s", target, r.text())
			return partial
		}
	}
	r, header := postAt(t, client, app.URL("/admin/api/models/Note/actions/dump/"+id), nil)
	if r.code < 500 || header.Get("Content-Disposition") != "" || !strings.Contains(r.raw(), "larger than") {
		t.Logf("a file over the ceiling answered %d with Content-Disposition %q (%d bytes)", r.code, header.Get("Content-Disposition"), len(r.body))
		return partial
	}
	return present
}

// zeroes is an endless body that allocates nothing until it is read.
type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// postAt issues one JSON POST with the client it is given, against an
// application tryStart booted, and keeps the headers.
func postAt(t *testing.T, client *http.Client, target string, payload any) (response, http.Header) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode payload: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, target, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{code: resp.StatusCode, ctype: resp.Header.Get("Content-Type"), body: raw}, resp.Header
}

// EXT-04: a widget draws a series — signups per day, failed payments per
// hour — and not only a headline value or a short list.
//
// Present since A11 O5, and measured by effect, in four parts: the series
// the bench application declares reaches the payload with one value per
// label; it MOVES when the data does (a note created now is one more today),
// which a chart drawn from a constant would not; a series its card cannot
// draw is that card's error and never the screen's, on the overview and on
// a dashboard; and a series declaration the panel cannot draw stops the
// application, naming the widget. Whether the browser draws it is UIX-11.
func probeWidgetSeries(t *testing.T, e *env) verdict {
	read := func() (labels []string, values []float64, ok bool) {
		r := e.get(t, "/admin/api/ui/dashboards/"+trendsDashboard)
		if r.code != http.StatusOK || r.servedTheShell() {
			t.Logf("the dashboard that carries the series answered %d %s: %s", r.code, r.ctype, r.text())
			return nil, nil, false
		}
		cards, _ := r.json(t)["widgets"].([]any)
		card := cardByID(cards, "notes-per-day")
		if card == nil {
			t.Logf("the dashboard carries no notes-per-day card: %s", r.text())
			return nil, nil, false
		}
		if card["kind"] != "line" {
			t.Logf("the series card is served as kind %v: %v", card["kind"], card)
			return nil, nil, false
		}
		labels, series := chartOf(card)
		if len(series) != 1 || len(series[0]) != len(labels) || len(labels) != seriesDays {
			t.Logf("the series card carries %d labels and %d series: %v", len(labels), len(series), card)
			return nil, nil, false
		}
		return labels, series[0], true
	}

	labels, before, ok := read()
	if !ok {
		if v := e.unrouted(t, "/admin/api/ui/dashboards/"+trendsDashboard); v == absent {
			return absent
		}
		return partial
	}
	if today := time.Now().UTC().Format("2006-01-02"); labels[len(labels)-1] != today {
		t.Logf("the series ends on %q, and today is %s: the labels did not come from the application's function", labels[len(labels)-1], today)
		return partial
	}
	e.createNote(t, map[string]any{"title": "ext04-series", "status": "draft"})
	_, after, ok := read()
	if !ok {
		return partial
	}
	if got, want := after[len(after)-1], before[len(before)-1]+1; got != want {
		t.Logf("a note created today moved today's reading from %v to %v, not to %v: the chart is not fed by the function", before[len(before)-1], got, want)
		return partial
	}

	// A series its card cannot draw is the card's error, on the overview
	// and on a dashboard. NaN is the sharpest case: JSON cannot carry it, so
	// a panel that did not check it would fail the whole screen.
	steady := func(context.Context) (orbit.SeriesValue, error) {
		return orbit.SeriesValue{Labels: []string{"a", "b"}, Series: []orbit.Series{{Name: "n", Values: []float64{1, 2}}}}, nil
	}
	app, err := tryStart(t, extensionApp(t, orbit.Config{
		Title: "Admin Bench (series)",
		Widgets: []orbit.Widget{
			{ID: "good-line", Kind: orbit.WidgetLine, Series: steady},
			{ID: "nan-line", Kind: orbit.WidgetLine, Series: func(context.Context) (orbit.SeriesValue, error) {
				return orbit.SeriesValue{Labels: []string{"a", "b"}, Series: []orbit.Series{{Values: []float64{1, math.NaN()}}}}, nil
			}},
		},
		Dashboards: []orbit.Dashboard{{ID: "charts", Widgets: []orbit.Widget{
			{ID: "ragged-bar", Kind: orbit.WidgetBar, Series: func(context.Context) (orbit.SeriesValue, error) {
				return orbit.SeriesValue{Labels: []string{"a", "b", "c"}, Series: []orbit.Series{{Values: []float64{1}}}}, nil
			}},
			{ID: "good-bar", Kind: orbit.WidgetBar, Series: steady},
		}}},
	}))
	if err != nil {
		t.Logf("an application declaring series cards on the overview and a dashboard refused to start: %v", err)
		return partial
	}
	client := signInAt(t, app, "admin", bootstrapPassword)
	for path, want := range map[string][2]string{
		"/admin/api/ui/dashboard":         {"good-line", "nan-line"},
		"/admin/api/ui/dashboards/charts": {"good-bar", "ragged-bar"},
	} {
		r, _ := fetch(t, client, app.URL(path))
		var screen map[string]any
		// A 200 is not enough: a value JSON cannot carry fails the encoder
		// after the status is written, and the screen arrives empty.
		if r.code != http.StatusOK || r.servedTheShell() || json.Unmarshal(r.body, &screen) != nil {
			t.Logf("%s answered %d %q with one card that cannot be drawn: the card took the screen down", path, r.code, r.text())
			return partial
		}
		cards, _ := screen["widgets"].([]any)
		good, bad := cardByID(cards, want[0]), cardByID(cards, want[1])
		if good == nil || good["error"] != nil || good["series"] == nil {
			t.Logf("%s: the card that can be drawn is not: %v", path, good)
			return partial
		}
		if bad == nil || bad["error"] == nil || bad["series"] != nil {
			t.Logf("%s: the card that cannot be drawn was served as data, or dropped: %v", path, bad)
			return partial
		}
	}

	// A series declaration the panel cannot draw refuses to start, naming
	// the widget.
	for label, w := range map[string]orbit.Widget{
		"a line with no series":          {ID: "series-missing", Kind: orbit.WidgetLine},
		"a kind the panel does not draw": {ID: "pie-chart", Kind: "pie", Series: steady},
	} {
		_, err := tryStart(t, extensionApp(t, orbit.Config{Title: "Admin Bench (series refused)", Widgets: []orbit.Widget{w}}))
		if err == nil {
			t.Logf("%s started", label)
			return partial
		}
		if !strings.Contains(err.Error(), w.ID) {
			t.Logf("%s was refused, but the error does not name the widget: %v", label, err)
			return partial
		}
	}
	return present
}

// EXT-05: cards somewhere other than the overview — a second dashboard for
// finance, made of the panel's own cards and drawn inside its chrome.
//
// Present since A11 O5. The probe asks what makes it a second dashboard and
// not a second copy of the first: the navigation lists it where the panel
// serves it; it carries its own cards, in its own order and spans, and none
// of them reaches the overview; an unknown one is a 404 in JSON; a grant on
// the overview opens no dashboard, and the dashboard's own grant does; and
// a dashboard the panel cannot draw stops the application, naming it.
func probeWidgetPlacement(t *testing.T, e *env) verdict {
	nav := e.get(t, "/admin/api/ui/extensions")
	if nav.code != http.StatusOK || nav.servedTheShell() {
		t.Fatalf("the navigation answered %d; CUST-04 measures that and should be red too", nav.code)
	}
	url, listed := dashboardURLFor(nav.json(t), trendsDashboard)
	if !listed {
		if v := e.unrouted(t, "/admin/api/ui/dashboards/"+trendsDashboard); v == absent {
			t.Logf("one dashboard, the overview: the navigation lists none (%s) and no route serves one", nav.text())
			return absent
		}
		t.Logf("a dashboard is served, and the navigation does not list it: %s", nav.text())
		return partial
	}
	if url != "/admin/dashboards/"+trendsDashboard {
		t.Logf("the navigation links the dashboard at %q", url)
		return partial
	}

	r := e.get(t, "/admin/api/ui/dashboards/"+trendsDashboard)
	if r.code != http.StatusOK || r.servedTheShell() {
		t.Logf("the dashboard the navigation lists answered %d %s", r.code, r.ctype)
		return partial
	}
	board := r.json(t)
	cards, _ := board["widgets"].([]any)
	var ids []string
	spans := map[string]float64{}
	for _, entry := range cards {
		card, _ := entry.(map[string]any)
		id, _ := card["id"].(string)
		ids = append(ids, id)
		spans[id], _ = card["span"].(float64)
	}
	if want := []string{"notes-per-day", "published-notes", "notes-by-status", "recent-notes"}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Logf("the dashboard carries %v, declared %v: its order is its layout", ids, want)
		return partial
	}
	if board["title"] != "Bench Trends" || board["columns"] != float64(2) || spans["notes-per-day"] != 2 || spans["notes-by-status"] != 1 || spans["recent-notes"] != 2 {
		t.Logf("the dashboard's layout is not the one declared: title %v, columns %v, spans %v", board["title"], board["columns"], spans)
		return partial
	}
	overview := e.get(t, "/admin/api/ui/dashboard")
	for _, id := range ids {
		if strings.Contains(overview.raw(), `"`+id+`"`) {
			t.Logf("the dashboard's card %s reached the overview: %s", id, overview.text())
			return partial
		}
	}
	if unknown := e.get(t, "/admin/api/ui/dashboards/nowhere"); unknown.code != http.StatusNotFound || unknown.servedTheShell() {
		t.Logf("an unknown dashboard answered %d %s", unknown.code, unknown.ctype)
		return partial
	}

	// A grant on the overview opens the overview, and no dashboard; the
	// dashboard's own grant opens it.
	op := e.operatorNamed(t, "overview-only")
	e.grant(t, op.username, "admin:dashboard", "view")
	listedFor := func() bool {
		n := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/extensions", nil)
		_, ok := dashboardURLFor(n.json(t), trendsDashboard)
		return ok
	}
	if code := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/dashboards/"+trendsDashboard, nil).code; listedFor() || code != http.StatusForbidden {
		t.Logf("an operator who holds only the overview is shown the dashboard (listed=%v) and its API answers %d", listedFor(), code)
		return partial
	}
	e.grant(t, op.username, "admin:dashboard:"+trendsDashboard, "view")
	if code := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/dashboards/"+trendsDashboard, nil).code; !listedFor() || code != http.StatusOK {
		t.Logf("the dashboard's own grant does not open it: listed=%v, API %d", listedFor(), code)
		return partial
	}

	// A dashboard the panel cannot draw refuses to start, naming it.
	_, err := tryStart(t, extensionApp(t, orbit.Config{
		Title:      "Admin Bench (an empty dashboard)",
		Dashboards: []orbit.Dashboard{{ID: "empty-board", Title: "Nothing here"}},
	}))
	if err == nil || !strings.Contains(err.Error(), "empty-board") {
		t.Logf("a dashboard with no cards: %v", err)
		return partial
	}
	return present
}

// cardByID finds one card of a widgets payload.
func cardByID(cards []any, id string) map[string]any {
	for _, entry := range cards {
		if card, _ := entry.(map[string]any); card != nil && card["id"] == id {
			return card
		}
	}
	return nil
}

// chartOf reads a chart card's labels and series values.
func chartOf(card map[string]any) ([]string, [][]float64) {
	var labels []string
	raw, _ := card["labels"].([]any)
	for _, l := range raw {
		s, _ := l.(string)
		labels = append(labels, s)
	}
	var series [][]float64
	list, _ := card["series"].([]any)
	for _, entry := range list {
		s, _ := entry.(map[string]any)
		values, _ := s["values"].([]any)
		var out []float64
		for _, v := range values {
			f, _ := v.(float64)
			out = append(out, f)
		}
		series = append(series, out)
	}
	return labels, series
}

// dashboardURLFor finds a dashboard in the navigation payload.
func dashboardURLFor(payload map[string]any, id string) (string, bool) {
	list, _ := payload["dashboards"].([]any)
	for _, entry := range list {
		d, _ := entry.(map[string]any)
		if d != nil && d["id"] == id {
			url, _ := d["url"].(string)
			return url, url != ""
		}
	}
	return "", false
}

// EXT-06: the application's own script runs in the panel — a client-side
// hook that adds a button, a renderer, a keyboard shortcut. The panel's CSP
// (script-src 'self') is what makes this a contract and not a tag anyone can
// paste: a script has to be declared, served and allowed.
func probeClientScript(t *testing.T, e *env) verdict {
	if hits := knobsNamed("script", "hook", "plugin", "client_extension", "uiextension"); len(hits) > 0 {
		t.Logf("the mount surface grew %v: grow this probe to declare a script, read the tag the document carries and the CSP that allows it", knobPaths(hits))
		return partial
	}
	doc, header := fetch(t, e.operator(t), e.server().URL("/admin/"))
	if doc.code != http.StatusOK {
		t.Fatalf("GET /admin/ answered %d", doc.code)
	}
	scripts := scriptSources(doc.raw())
	for _, src := range scripts {
		if !strings.Contains(src, "/assets/") {
			t.Logf("the document loads %q, which is not the panel's own bundle: grow this probe", src)
			return partial
		}
	}
	t.Logf("the document loads only the panel's bundle %v, under script-src %v", scripts,
		cspSources(header.Get("Content-Security-Policy"), "script-src"))
	return absent
}

// EXT-07: a field drawn by a renderer the application provides — a colour,
// a map pin, a money amount in its currency — beyond the four the panel
// ships (json, richtext, file, image).
func probeCustomFieldRenderer(t *testing.T, e *env) verdict {
	if hits := knobsNamed("renderer", "custom_widget", "widget_registry", "field_renderer"); len(hits) > 0 {
		t.Logf("the mount surface grew %v: grow this probe to register a renderer and read the schema", knobPaths(hits))
		return partial
	}
	app, err := tryStart(t, extensionApp(t, orbit.Config{
		Title:        "Admin Bench (renderer)",
		FieldWidgets: map[string]string{"Note.Status": "colour"},
	}))
	if err != nil {
		t.Logf("the panel refused a widget it does not ship (%v) and offers nothing to register one", err)
		return absent
	}
	client := signInAt(t, app, "admin", bootstrapPassword)
	schema, _ := fetch(t, client, app.URL("/admin/api/models/Note/schema"))
	if schema.code != http.StatusOK {
		t.Fatalf("Note schema answered %d: %s", schema.code, schema.text())
	}
	fields, _ := schema.json(t)["fields"].([]any)
	for _, entry := range fields {
		f, _ := entry.(map[string]any)
		if f["column"] != "status" {
			continue
		}
		if f["html_type"] == "colour" {
			t.Log("the schema publishes the application's widget name: grow this probe to check the SPA has a renderer for it")
			return partial
		}
		t.Logf("Note.status declared as \"colour\" is published as %q: the declaration was dropped", f["html_type"])
		return absent
	}
	t.Fatalf("the schema has no status field: %s", schema.text())
	return absent
}

// EXT-08: a field widget the panel cannot draw stops the application. The
// panel refuses at startup an action on an unknown model, a page with no
// handler, a widget with no loader and a logo with a javascript: URL — "a
// declaration the panel cannot honour refuses to start". A field widget
// misspelled, or on a field that does not exist, is the same mistake.
//
// A refusal counts only when it names the entry: an application that failed
// to start for any other reason would otherwise read as one. And a
// declaration the panel can draw has to start, or a check that refused every
// field_widgets entry would read as present too.
func probeFieldWidgetRefusal(t *testing.T, e *env) verdict {
	refused := []string{}
	started := []string{}
	for label, c := range map[string]struct {
		widgets map[string]string
		names   string
	}{
		"a widget the panel does not ship (Note.Status: colour)": {map[string]string{"Note.Status": "colour"}, "Note.Status"},
		"a field that does not exist (Note.Nothing: json)":       {map[string]string{"Note.Nothing": "json"}, "Note.Nothing"},
	} {
		_, err := tryStart(t, extensionApp(t, orbit.Config{Title: "Admin Bench (widgets)", FieldWidgets: c.widgets}))
		if err == nil {
			started = append(started, label)
			continue
		}
		if !strings.Contains(err.Error(), c.names) {
			t.Logf("%s: the application did not start, but the error does not name the entry: %v", label, err)
			started = append(started, label+" (refused for another reason)")
			continue
		}
		refused = append(refused, label)
	}
	sort.Strings(refused)
	sort.Strings(started)
	switch {
	case len(started) == 0:
		if _, err := tryStart(t, extensionApp(t, orbit.Config{
			Title:        "Admin Bench (drawable widget)",
			FieldWidgets: map[string]string{"Note.Body": "richtext"},
		})); err != nil {
			t.Logf("both entries were refused, and so is one the panel can draw (Note.Body: richtext): %v", err)
			return partial
		}
		return present
	case len(refused) == 0:
		t.Logf("the application started with %v: the declaration is dropped in silence", started)
		return absent
	}
	t.Logf("refused %v, started with %v", refused, started)
	return partial
}

// EXT-09: a default theme — dark, light, or the operator's system — set by
// the application and honoured on the FIRST frame, before any operator has
// toggled anything. A control room whose screens open dark is a
// configuration, not a preference each operator rediscovers.
//
// This probe measures the server's half: what the document carries, and
// what it loads before the bundle runs. Whether the browser paints the
// first frame in that theme — and keeps the operator's own choice over it on
// reload — is UIX-10's question, which only a browser can answer.
func probeDefaultTheme(t *testing.T, e *env) verdict {
	knobs := knobsNamed("theme", "color_scheme", "colour_scheme", "appearance")
	if len(knobs) == 0 {
		doc := e.get(t, "/admin/")
		if doc.code != http.StatusOK {
			t.Fatalf("GET /admin/ answered %d", doc.code)
		}
		if hints := themeHints(doc.raw()); len(hints) > 0 {
			t.Logf("no knob, but the document carries %v: grow this probe", hints)
			return partial
		}
		t.Logf("no knob, and the document carries no theme hint (meta: %v): the first frame is decided by the browser's preference and the operator's last toggle", metaNames(doc.raw()))
		return absent
	}
	if len(knobs) != 1 || !knobs[0].bindable || knobs[0].typ.Kind() != reflect.String {
		t.Logf("theme knobs %v: grow this probe to set each one", knobPaths(knobs))
		return partial
	}
	k := knobs[0]

	// Nothing configured, nothing changed (QADR-0010): the document the
	// bench's application serves carries no theme and loads no script but
	// the bundle.
	doc := e.get(t, "/admin/")
	if doc.code != http.StatusOK {
		t.Fatalf("GET /admin/ answered %d", doc.code)
	}
	if hints := themeHints(doc.raw()); len(hints) > 0 {
		t.Logf("no theme configured, and the document carries %v", hints)
		return partial
	}

	// A value that is not a theme stops the application, by name.
	bad := orbit.Config{Title: "Admin Bench (theme: blue)"}
	setKnob(&bad, k, "blue")
	if _, err := tryStart(t, extensionApp(t, bad)); err == nil {
		t.Logf("%s: blue started", k.path)
		return partial
	} else if !strings.Contains(err.Error(), k.path) {
		t.Logf("%s: blue was refused, but the error does not name the key: %v", k.path, err)
		return partial
	}

	// A configured theme reaches the first screen — the login page, before
	// there is a session — and the panel's own document.
	cfg := orbit.Config{Title: "Admin Bench (theme: dark)"}
	setKnob(&cfg, k, "dark")
	app, err := tryStart(t, extensionApp(t, cfg))
	if err != nil {
		t.Fatalf("%s: dark refused to start: %v", k.path, err)
	}
	pages := []struct {
		name   string
		client *http.Client
		path   string
	}{
		{"the login page", &http.Client{}, "/admin/login"},
		{"the panel's document", signInAt(t, app, "admin", bootstrapPassword), "/admin/"},
	}
	for _, page := range pages {
		body, header := fetch(t, page.client, app.URL(page.path))
		if body.code != http.StatusOK {
			t.Fatalf("%s answered %d", page.name, body.code)
		}
		if problem := firstFrameProblem(t, page.client, app, body.raw(), header, "dark"); problem != "" {
			t.Logf("%s: %s", page.name, problem)
			return partial
		}
	}
	return present
}

// EXT-10: a palette and not one accent — the colours a product's own design
// system names, each validated the way the accent is, and each checked
// against the ground it is drawn on in ITS theme: a dark navy accent reads
// on white and disappears on the dark ground.
func probePaletteTokens(t *testing.T, e *env) verdict {
	colours := knobsNamed("color", "colour", "palette", "design_token")
	if len(colours) == 0 {
		t.Log("the mount surface has no colour at all")
		return absent
	}
	// Each colour knob must refuse a value that is not a colour: the value
	// is written into the page, so one that is a stylesheet fragment is an
	// injection a line of YAML would open.
	unvalidated := []string{}
	for _, k := range colours {
		cfg := orbit.Config{Title: "Admin Bench (palette)"}
		if !setKnob(&cfg, k, "red;} body{display:none") {
			t.Logf("%s is a %s, not a string: grow this probe to write an invalid entry into it", k.path, k.typ)
			return partial
		}
		if _, err := tryStart(t, extensionApp(t, cfg)); err == nil {
			unvalidated = append(unvalidated, k.path)
		}
	}
	if len(unvalidated) > 0 {
		t.Logf("colour knobs that accept a value that is not a colour: %v", unvalidated)
		return partial
	}
	// The accent reaches the document the browser paints.
	doc := e.get(t, "/admin/")
	if !strings.Contains(doc.raw(), benchBranding().PrimaryColor) {
		t.Logf("the declared accent does not reach the document; CUST-02 measures that and should be red too")
		return partial
	}

	// A palette: an accent, a surface and a text colour, for each theme.
	palette, missing := paletteKnobs(colours)
	if len(missing) > 0 {
		t.Logf("%d colour knob(s) %v, each validated: no %v", len(colours), knobPaths(colours), missing)
		return partial
	}

	// Checked per theme. The same navy accent is about 1.1:1 on the dark
	// ground and 18:1 on the light one: refused in the first, by theme and
	// by key, and accepted in the second — a check that refused it in both
	// would be one rule for the palette, not one per theme.
	const navy = "#0b1530"
	darkAccent := palette["dark"]["accent"]
	onDark := orbit.Config{Title: "Admin Bench (palette: navy on dark)"}
	setKnob(&onDark, darkAccent, navy)
	_, err := tryStart(t, extensionApp(t, onDark))
	if err == nil {
		t.Logf("%s = %s, about 1.1:1 on the dark ground, started", darkAccent.path, navy)
		return partial
	}
	if !strings.Contains(err.Error(), darkAccent.path) || !strings.Contains(err.Error(), "dark") {
		t.Logf("%s = %s was refused, but the error names neither the key nor the theme: %v", darkAccent.path, navy, err)
		return partial
	}
	onLight := orbit.Config{Title: "Admin Bench (palette: navy on light)"}
	setKnob(&onLight, palette["light"]["accent"], navy)
	if _, err := tryStart(t, extensionApp(t, onLight)); err != nil {
		t.Logf("%s = %s reads at 18:1 on the light ground and was refused: the check is not per theme: %v", palette["light"]["accent"].path, navy, err)
		return partial
	}
	// The accent an application could set before the palette existed takes
	// any hex colour. One that falls short in a theme still starts: refusing
	// it would stop an application that changed nothing (QADR-0010).
	legacy := orbit.Config{Title: "Admin Bench (palette: the old accent)", Branding: orbit.Branding{PrimaryColor: navy}}
	if _, err := tryStart(t, extensionApp(t, legacy)); err != nil {
		t.Logf("branding.primary_color = %s started before the palette was checked per theme and is refused now: %v", navy, err)
		return partial
	}

	// What the configuration accepts reaches the stylesheet, per theme: the
	// first document a browser opens carries each theme's colours as the
	// custom properties the panel's stylesheet reads, scoped to that theme.
	declared := map[string]map[string]string{
		"light": {"accent": "#0b5fff", "surface": "#fafafa", "text": "#111827"},
		"dark":  {"accent": "#60a5fa", "surface": "#111827", "text": "#e5e7eb"},
	}
	cfg := orbit.Config{Title: "Admin Bench (palette: both themes)"}
	for theme, roles := range declared {
		for role, value := range roles {
			setKnob(&cfg, palette[theme][role], value)
		}
	}
	app, err := tryStart(t, extensionApp(t, cfg))
	if err != nil {
		t.Logf("a palette that reads in both themes was refused: %v", err)
		return partial
	}
	login, _ := fetch(t, &http.Client{}, app.URL("/admin/login"))
	painted := paletteOnDocument(login.raw())
	property := map[string]string{"accent": "--primary", "surface": "--background", "text": "--foreground"}
	for theme, roles := range declared {
		for role, value := range roles {
			got, ok := painted[theme][property[role]]
			if !ok {
				t.Logf("the %s theme's %s (%s) does not reach the document's stylesheet as %s: %v", theme, role, value, property[role], painted)
				return partial
			}
			if want := hexRGB(t, value); !sameColour(got, want) {
				t.Logf("the %s theme's %s is %s in the configuration and %v on the document", theme, role, value, got)
				return partial
			}
		}
	}
	return present
}

// EXT-11: what the configuration accepts as branding is something the
// browser will load. The panel's validation accepts a same-site path or an
// absolute http(s) URL for the logo and the favicon; the panel's own CSP
// decides whether the browser fetches it.
func probeBrandingUnderCSP(t *testing.T, e *env) verdict {
	// Same-site, as the bench's application declares its logo.
	sameSite := benchBranding().LogoURL
	_, header := fetch(t, e.operator(t), e.server().URL("/admin/"))
	host := mustHost(t, e.server().URL(""))
	if !cspAllows(cspSources(header.Get("Content-Security-Policy"), "img-src"), sameSite, host) {
		t.Logf("a same-site logo %q is outside img-src %v", sameSite, cspSources(header.Get("Content-Security-Policy"), "img-src"))
		return absent
	}

	// Absolute, as a product whose logo lives on its CDN declares it. The
	// application has to START with it — that is the validation accepting
	// it — and its login page, the first screen that shows a logo, is where
	// the policy is read.
	const absoluteLogo = "https://cdn.example.test/brand/logo.svg"
	app, err := tryStart(t, extensionApp(t, orbit.Config{
		Title:    "Admin Bench (absolute logo)",
		Branding: orbit.Branding{LogoURL: absoluteLogo},
	}))
	if err != nil {
		t.Logf("the panel refuses an absolute logo URL (%v): the configuration and the CSP agree", err)
		return present
	}
	login, loginHeader := fetch(t, &http.Client{}, app.URL("/admin/login"))
	if !strings.Contains(login.raw(), absoluteLogo) {
		t.Logf("the login page does not carry the declared logo: %s", login.text())
		return partial
	}
	sources := cspSources(loginHeader.Get("Content-Security-Policy"), "img-src")
	if cspAllows(sources, absoluteLogo, mustHost(t, app.URL(""))) {
		return present
	}
	t.Logf("the configuration accepts %q and the page carries it, but img-src %v refuses it: the browser draws no logo", absoluteLogo, sources)
	return partial
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}

// EXT-12: the configuration reference documents every key an application
// can bind. The keys are read from the binding contract itself — the koanf
// tags nucleus binds modules.orbit.* with, nested structs included — and
// compared with the keys the reference's tables list.
func probeConfigReference(t *testing.T, e *env) verdict {
	documented := documentedKeys(t, "website/docs/configuration.md")
	var bindable, missing []string
	for _, k := range mountKnobs() {
		if !k.bindable {
			continue
		}
		bindable = append(bindable, k.path)
		if !documented[k.path] {
			missing = append(missing, k.path)
		}
	}
	if len(bindable) == 0 {
		t.Fatal("the mount surface has no bindable key: the reader is broken, not the reference")
	}
	switch {
	case len(missing) == 0:
		return present
	case len(missing) == len(bindable):
		t.Log("the reference documents none of the bindable keys")
		return absent
	}
	t.Logf("%d of %d bindable keys documented; missing %v", len(bindable)-len(missing), len(bindable), missing)
	return partial
}

// EXT-13: what an application adds answers to the panel's RBAC like the
// panel's own screens do. An extension point that showed a card, a screen
// or a verb to an operator who holds no grant for it would be the way
// around every policy the panel enforces — and every surface this family
// asks for will be one more place to forget it.
//
// O5 added two such places, and the probe asks about both: a dashboard
// (listed and served only to an operator granted view on
// admin:dashboard:<id>), and a "records" card on it, which lists a model's
// rows and so is the model's to show — granted the dashboard, an operator
// still does not see that card until they may list the model.
func probeExtensionsAuthorized(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "extension-viewer")
	e.grant(t, op.username, "admin:Note", "get_schema")

	type sight struct {
		widget, page, action, dashboard, records bool
		pageCode, dashboardCode                  int
	}
	seen := func() (s sight) {
		dash := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/dashboard", nil)
		if dash.code == http.StatusOK && !dash.servedTheShell() {
			cards, _ := dash.json(t)["widgets"].([]any)
			s.widget = widgetValue(t, cards, "pending-notes") != "" || widgetError(t, cards, "pending-notes") != ""
		}
		nav := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/extensions", nil)
		if nav.code == http.StatusOK && !nav.servedTheShell() {
			_, s.page = pageURLFor(nav.json(t), "reports")
			_, s.dashboard = dashboardURLFor(nav.json(t), trendsDashboard)
		}
		schema := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note/schema", nil)
		s.action = strings.Contains(schema.raw(), `"name":"publish"`)
		s.pageCode = e.asOperator(t, op, http.MethodGet, "/admin/x/reports/", nil).code
		board := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/dashboards/"+trendsDashboard, nil)
		s.dashboardCode = board.code
		if board.code == http.StatusOK && !board.servedTheShell() {
			cards, _ := board.json(t)["widgets"].([]any)
			s.records = cardByID(cards, "recent-notes") != nil
		}
		return
	}

	s := seen()
	if s.widget || s.page || s.action || s.dashboard || s.pageCode == http.StatusOK || s.dashboardCode == http.StatusOK {
		t.Logf("an operator with no grant is shown %+v", s)
		return absent
	}

	// And the grant is what opens each one — otherwise the probe above
	// measured an operator who can see nothing at all.
	e.grant(t, op.username, "admin:dashboard", "view")
	e.grant(t, op.username, "admin:page:reports", "view")
	e.grant(t, op.username, "admin:Note", "publish")
	e.grant(t, op.username, "admin:dashboard:"+trendsDashboard, "view")
	s = seen()
	if !s.widget || !s.page || !s.action || !s.dashboard || s.pageCode != http.StatusOK || s.dashboardCode != http.StatusOK {
		t.Logf("after the grants: %+v", s)
		return partial
	}
	if s.records {
		t.Log("granted the dashboard and not the model, the operator is shown the model's rows")
		return partial
	}
	e.grant(t, op.username, "admin:Note", "list")
	if s = seen(); !s.records {
		t.Logf("granted the model's list, the operator is still not shown its rows: %+v", s)
		return partial
	}
	return present
}

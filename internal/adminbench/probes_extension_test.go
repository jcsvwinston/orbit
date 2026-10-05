// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
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
// sets is a surface that moved. (The widget and page lists went when EXT-04
// and EXT-05 became behaviour checks in O5.)
var (
	knownModelAction  = []string{"Name", "Model", "Label", "Description", "Confirm", "Destructive", "AllowEmptySelection", "Run"}
	knownActionResult = []string{"Message", "Affected", "Data"}

	knownActionDescriptor = []string{"name", "label", "description", "confirm", "destructive", "requires_selection"}
	knownActionResponse   = []string{"action", "ran", "requested", "affected", "failed", "errors", "message", "data"}
)

// publishDescriptor returns the publish action as the schema of Note offers
// it to the superuser.
func publishDescriptor(t *testing.T, e *env) map[string]any {
	t.Helper()
	schema := e.get(t, "/admin/api/models/Note/schema")
	if schema.code != http.StatusOK {
		t.Fatalf("Note schema answered %d: %s", schema.code, schema.text())
	}
	actions, _ := schema.json(t)["actions"].([]any)
	for _, entry := range actions {
		if d, ok := entry.(map[string]any); ok && d["name"] == "publish" {
			return d
		}
	}
	t.Fatalf("the schema offers no publish action; DS-09 measures that and should be red too: %s", schema.text())
	return nil
}

// EXT-01: an action asks the operator for something before it runs — the
// reason for a refund, the date to publish on. Django's intermediate pages
// and Filament's action forms both do it; a verb with no input is the half
// of an action that needs nothing from the person running it.
func probeActionInput(t *testing.T, e *env) verdict {
	if hits := fieldsBeyond(reflect.TypeOf(orbit.ModelAction{}), knownModelAction,
		"input", "field", "form", "param", "prompt", "arg"); len(hits) > 0 {
		t.Logf("orbit.ModelAction grew %v: grow this probe to declare an input, check the descriptor publishes it and that Run receives what was posted", hits)
		return partial
	}
	if hits := keysBeyond(publishDescriptor(t, e), knownActionDescriptor,
		"input", "field", "form", "param", "prompt", "arg"); len(hits) > 0 {
		t.Logf("the action descriptor grew %v: a UI could draw a form from it; grow this probe", hits)
		return partial
	}

	// The behaviour: what an operator would type, posted the way a form
	// would post it, under every name a contract might choose. If it reaches
	// the application's function at all, the server half exists.
	const marker = "ext01-operator-typed-this"
	id := e.createNote(t, map[string]any{"title": "action-input", "status": "draft"})
	_, before := benchExtensions.lastRequest()
	r := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk", map[string]any{
		"action": "publish", "ids": []string{id},
		"input": map[string]any{"reason": marker}, "fields": map[string]any{"reason": marker},
		"params": map[string]any{"reason": marker}, "form": map[string]any{"reason": marker},
	})
	if r.code >= 400 {
		t.Fatalf("publish with an input answered %d: %s", r.code, r.text())
	}
	call, after := benchExtensions.lastRequest()
	if after == before {
		t.Fatalf("the action did not run, so the probe cannot ask what it received: %s", r.text())
	}
	if containsValue(reflect.ValueOf(call), marker) {
		t.Logf("the posted input reached the action (%+v) with no declaration to say it may: grow this probe", call)
		return partial
	}
	t.Logf("the action ran and received %+v: the descriptor names no input, and what was posted with the call is dropped", call)
	return absent
}

// EXT-02: the action is offered on the record it is about. Selecting one
// row in a grid to press a button is the workaround; the record view is
// where an operator already is when they decide to refund this order.
func probeActionOnRecord(t *testing.T, e *env) verdict {
	if hits := fieldsBeyond(reflect.TypeOf(orbit.ModelAction{}), knownModelAction,
		"record", "placement", "scope", "single", "detail", "where", "location", "view"); len(hits) > 0 {
		t.Logf("orbit.ModelAction grew %v: grow this probe to declare a record action and read it where the record view reads", hits)
		return partial
	}
	if hits := keysBeyond(publishDescriptor(t, e), knownActionDescriptor,
		"record", "placement", "scope", "single", "detail", "where", "location", "view"); len(hits) > 0 {
		t.Logf("the action descriptor grew %v: grow this probe", hits)
		return partial
	}

	// The record payload is what the record view loads; an action offered
	// there would travel with it or be named by it.
	id := e.createNote(t, map[string]any{"title": "action-on-record", "status": "draft"})
	record := e.get(t, "/admin/api/models/Note/"+id)
	if record.code != http.StatusOK {
		t.Fatalf("reading the record answered %d: %s", record.code, record.text())
	}
	if hits := keysBeyond(record.json(t), []string{"data"}, "action"); len(hits) > 0 {
		t.Logf("the record payload carries %v: grow this probe", hits)
		return partial
	}

	// What does exist: the bulk endpoint runs an action over exactly one
	// id. It is the grid's surface, reached by selecting that row there.
	r := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk",
		map[string]any{"action": "publish", "ids": []string{id}})
	if r.code >= 400 {
		t.Logf("running the action over one id answered %d: %s", r.code, r.text())
		return absent
	}
	t.Logf("one id through the bulk endpoint runs (%s); nothing tells a record view which actions belong on one record, and the record payload names none", r.text())
	return absent
}

// EXT-03: the action answers with something to download or a page to open —
// "export these invoices as PDF", "open the reconciliation for this batch".
func probeActionResultKinds(t *testing.T, e *env) verdict {
	if hits := fieldsBeyond(reflect.TypeOf(orbit.ActionResult{}), knownActionResult,
		"redirect", "download", "url", "file", "location", "navigate", "open", "link", "attachment"); len(hits) > 0 {
		t.Logf("orbit.ActionResult grew %v: grow this probe to return one and read what the panel does with it", hits)
		return partial
	}
	id := e.createNote(t, map[string]any{"title": "action-result", "status": "draft"})
	resp, header := fetchPost(t, e, "/admin/api/models/Note/bulk",
		`{"action":"publish","ids":["`+id+`"]}`)
	if resp.code >= 400 {
		t.Fatalf("publish answered %d: %s", resp.code, resp.text())
	}
	if loc := header.Get("Location"); loc != "" {
		t.Logf("the action's answer carries Location %q: grow this probe", loc)
		return partial
	}
	if cd := header.Get("Content-Disposition"); cd != "" {
		t.Logf("the action's answer carries Content-Disposition %q: grow this probe", cd)
		return partial
	}
	if hits := keysBeyond(resp.json(t), knownActionResponse,
		"redirect", "download", "url", "file", "location", "navigate", "open", "link"); len(hits) > 0 {
		t.Logf("the action's answer grew %v: grow this probe", hits)
		return partial
	}
	t.Logf("the answer is JSON with %v; Data is echoed as an untyped map the panel attaches no meaning to", topLevelKeys(resp.json(t)))
	return absent
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

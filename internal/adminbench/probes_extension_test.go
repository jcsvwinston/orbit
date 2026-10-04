// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

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
// sets is a surface that moved.
var (
	knownModelAction  = []string{"Name", "Model", "Label", "Description", "Confirm", "Destructive", "AllowEmptySelection", "Run"}
	knownActionResult = []string{"Message", "Affected", "Data"}
	knownWidget       = []string{"ID", "Title", "Description", "Permission", "Link", "Load"}
	knownWidgetValue  = []string{"Value", "Detail", "Items"}
	knownPage         = []string{"ID", "Title", "Description", "Icon", "Permission", "Handler"}

	knownActionDescriptor = []string{"name", "label", "description", "confirm", "destructive", "requires_selection"}
	knownActionResponse   = []string{"action", "ran", "requested", "affected", "failed", "errors", "message", "data"}
	knownWidgetPayload    = []string{"id", "title", "description", "link", "value", "detail", "items", "error"}
	knownPageDescriptor   = []string{"id", "title", "description", "icon", "url"}
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
func probeWidgetSeries(t *testing.T, e *env) verdict {
	for typ, known := range map[reflect.Type][]string{
		reflect.TypeOf(orbit.WidgetValue{}): knownWidgetValue,
		reflect.TypeOf(orbit.Widget{}):      knownWidget,
	} {
		if hits := fieldsBeyond(typ, known, "series", "point", "chart", "kind", "type", "trend", "spark", "data"); len(hits) > 0 {
			t.Logf("%s grew %v: grow this probe to declare a series and read it back", typ.Name(), hits)
			return partial
		}
	}
	r := e.get(t, "/admin/api/ui/dashboard")
	if r.code != http.StatusOK || r.servedTheShell() {
		t.Fatalf("the dashboard answered %d %s; CUST-03 measures that and should be red too", r.code, r.ctype)
	}
	cards, _ := r.json(t)["widgets"].([]any)
	for _, entry := range cards {
		card, _ := entry.(map[string]any)
		if hits := keysBeyond(card, knownWidgetPayload, "series", "point", "chart", "kind", "type", "trend", "spark", "data"); len(hits) > 0 {
			t.Logf("a card carries %v: grow this probe", hits)
			return partial
		}
	}
	t.Logf("%d card(s), each a value, a detail line or a list of rows: nothing a chart could be drawn from", len(cards))
	return absent
}

// EXT-05: cards somewhere other than the overview — a second dashboard for
// finance, or a page of the application's made of the panel's own cards
// and drawn inside its chrome, which is what "pages declared in Go" means
// when the page is not a handler that writes its own HTML.
func probeWidgetPlacement(t *testing.T, e *env) verdict {
	if hits := fieldsBeyond(reflect.TypeOf(orbit.Widget{}), knownWidget,
		"dashboard", "page", "placement", "screen", "group", "section", "slot", "area", "region"); len(hits) > 0 {
		t.Logf("orbit.Widget grew %v: grow this probe to place a card off the overview and read it there", hits)
		return partial
	}
	if hits := fieldsBeyond(reflect.TypeOf(orbit.Page{}), knownPage,
		"widget", "layout", "component", "block", "section", "kind", "card"); len(hits) > 0 {
		t.Logf("orbit.Page grew %v: grow this probe to declare a page of cards and read it", hits)
		return partial
	}
	nav := e.get(t, "/admin/api/ui/extensions")
	pages, _ := nav.json(t)["pages"].([]any)
	for _, entry := range pages {
		page, _ := entry.(map[string]any)
		if hits := keysBeyond(page, knownPageDescriptor, "widget", "layout", "kind", "card", "component"); len(hits) > 0 {
			t.Logf("a page descriptor carries %v: grow this probe", hits)
			return partial
		}
	}
	// A dashboard other than the overview would be addressable. An unknown
	// path under /api answers 404 JSON since A6 S9, so a 200 here is a
	// route and not the SPA's fallback.
	if v := e.unrouted(t, "/admin/api/ui/dashboards", "/admin/api/ui/dashboard/finance"); v != absent {
		t.Log("a route for another dashboard answered: grow this probe")
		return partial
	}
	t.Log("one dashboard, the overview; a page is an http.Handler that writes its own document, so a screen of cards is HTML the application writes by hand")
	return absent
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
func probeFieldWidgetRefusal(t *testing.T, e *env) verdict {
	refused := []string{}
	started := []string{}
	for label, widgets := range map[string]map[string]string{
		"a widget the panel does not ship (Note.Status: colour)": {"Note.Status": "colour"},
		"a field that does not exist (Note.Nothing: json)":       {"Note.Nothing": "json"},
	} {
		if _, err := tryStart(t, extensionApp(t, orbit.Config{Title: "Admin Bench (widgets)", FieldWidgets: widgets})); err != nil {
			refused = append(refused, label)
			continue
		}
		started = append(started, label)
	}
	sort.Strings(refused)
	sort.Strings(started)
	switch {
	case len(started) == 0:
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
func probeDefaultTheme(t *testing.T, e *env) verdict {
	if hits := knobsNamed("theme", "color_scheme", "colour_scheme", "appearance", "dark"); len(hits) > 0 {
		t.Logf("the mount surface grew %v: grow this probe to set it, read the document and check the bundle's first frame reads it", knobPaths(hits))
		return partial
	}
	doc := e.get(t, "/admin/")
	if doc.code != http.StatusOK {
		t.Fatalf("GET /admin/ answered %d", doc.code)
	}
	for _, name := range metaNames(doc.raw()) {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "theme") || strings.Contains(lower, "color-scheme") {
			t.Logf("the document carries meta %q: grow this probe", name)
			return partial
		}
	}
	if strings.Contains(doc.raw(), `<html class="dark"`) {
		t.Log("the document opens with the dark class: grow this probe")
		return partial
	}
	t.Logf("no knob, and the document carries no theme hint (meta: %v): the first frame is decided by the browser's preference and the operator's last toggle", metaNames(doc.raw()))
	return absent
}

// EXT-10: a palette and not one accent — the colours a product's own design
// system names, each validated the way the accent is.
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
	if len(colours) < 3 {
		t.Logf("%d colour knob(s) %v, each validated: an accent, not a palette — no surface, text, border or per-theme value", len(colours), knobPaths(colours))
		return partial
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
func probeExtensionsAuthorized(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "extension-viewer")
	e.grant(t, op.username, "admin:Note", "get_schema")

	seen := func() (widget, page, action bool, pageCode int) {
		dash := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/dashboard", nil)
		if dash.code == http.StatusOK && !dash.servedTheShell() {
			cards, _ := dash.json(t)["widgets"].([]any)
			widget = widgetValue(t, cards, "pending-notes") != "" || widgetError(t, cards, "pending-notes") != ""
		}
		nav := e.asOperator(t, op, http.MethodGet, "/admin/api/ui/extensions", nil)
		if nav.code == http.StatusOK && !nav.servedTheShell() {
			_, page = pageURLFor(nav.json(t), "reports")
		}
		schema := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note/schema", nil)
		action = strings.Contains(schema.raw(), `"name":"publish"`)
		pageCode = e.asOperator(t, op, http.MethodGet, "/admin/x/reports/", nil).code
		return
	}

	widget, page, action, code := seen()
	if widget || page || action || code == http.StatusOK {
		t.Logf("an operator with no grant is shown widget=%v page=%v action=%v, and the page answers %d", widget, page, action, code)
		return absent
	}

	// And the grant is what opens each one — otherwise the probe above
	// measured an operator who can see nothing at all.
	e.grant(t, op.username, "admin:dashboard", "view")
	e.grant(t, op.username, "admin:page:reports", "view")
	e.grant(t, op.username, "admin:Note", "publish")
	widget, page, action, code = seen()
	if !widget || !page || !action || code != http.StatusOK {
		t.Logf("after the grants: widget=%v page=%v action=%v, page answers %d", widget, page, action, code)
		return partial
	}
	return present
}

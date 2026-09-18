// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit"
	"github.com/jcsvwinston/orbit/datasource"
)

// probeTitle checks the one branding knob the mount surface has.
func probeTitle(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/")
	if r.code != http.StatusOK {
		t.Logf("GET /admin/ answered %d", r.code)
		return absent
	}
	if !strings.Contains(r.raw(), "Admin Bench") {
		t.Logf("the configured title does not reach the served page: %s", r.text())
		return partial
	}
	return present
}

// probeBranding asks for what a product team asks for on day one: our logo,
// our colours, our icon in the tab.
//
// It used to read the NAMES of the mount surface and call a matching key a
// capability — the trap this bench wrote down at CUST-03. So it measures the
// page instead: the panel serves the declared logo, favicon and colour on
// the document itself, which is what makes them available on the LOGIN
// screen, before any API call could carry them.
func probeBranding(t *testing.T, e *env) verdict {
	page := e.get(t, "/admin/")
	if page.code != http.StatusOK {
		t.Logf("GET /admin/ answered %d", page.code)
		return absent
	}
	declared := benchBranding()
	missing := []string{}
	for label, value := range map[string]string{
		"logo":    declared.LogoURL,
		"favicon": declared.FaviconURL,
		"colour":  declared.PrimaryColor,
	} {
		if !strings.Contains(page.raw(), value) {
			missing = append(missing, label)
		}
	}
	if len(missing) == 3 {
		t.Logf("the served page carries none of the declared branding: %s", page.text())
		return absent
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Logf("the served page carries only part of the declared branding (missing %v)", missing)
		return partial
	}

	// The login screen is where a logo matters most: it is the page an
	// operator sees before they are anybody, and the one that tells them
	// whose product this is.
	login := e.get(t, "/admin/login")
	if login.code != http.StatusOK || !strings.Contains(login.raw(), declared.LogoURL) {
		t.Logf("the login page does not carry the declared logo (%d)", login.code)
		return partial
	}
	return present
}

// probeDashboardWidgets asks for the landing screen an admin product opens
// on: the counters a team chooses.
//
// It used to match "widget" anywhere in a config key, and `field_widgets` —
// which says how a FIELD is edited and has nothing to do with a landing
// screen — moved this control from absent to partial on nothing but a
// substring. This one declares two cards and reads what the panel answers:
// the value the application computed, and the broken card reported as broken
// rather than dropped.
func probeDashboardWidgets(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/ui/dashboard")
	if r.code != http.StatusOK || r.servedTheShell() {
		t.Logf("the panel has no dashboard endpoint (%d %s)", r.code, r.ctype)
		return absent
	}
	cards, ok := r.json(t)["widgets"].([]any)
	if !ok || len(cards) == 0 {
		t.Logf("the dashboard carries no declared card: %s", r.text())
		return absent
	}

	// The count has to be the application's own reading, not a number the
	// panel could have produced: the probe writes a draft and asks for the
	// card again.
	before := widgetValue(t, cards, "pending-notes")
	e.createNote(t, map[string]any{"title": "widget-probe", "status": "draft"})
	after := widgetValue(t, e.get(t, "/admin/api/ui/dashboard").json(t)["widgets"].([]any), "pending-notes")
	if before == "" || after == "" {
		t.Logf("the declared card carries no value: %s", r.text())
		return partial
	}
	if before == after {
		t.Logf("the card did not move when the application's data did (%s -> %s)", before, after)
		return partial
	}

	// A card that cannot be read says so. A screen that dropped it would
	// report a broken query as "nothing to see".
	if widgetError(t, cards, "broken-card") == "" {
		t.Logf("a failing widget is not reported as failing: %s", r.text())
		return partial
	}
	return present
}

// widgetValue reads one card's headline from the dashboard payload.
func widgetValue(t *testing.T, cards []any, id string) string {
	t.Helper()
	for _, entry := range cards {
		card, _ := entry.(map[string]any)
		if card != nil && card["id"] == id {
			value, _ := card["value"].(string)
			return value
		}
	}
	return ""
}

// widgetError reads one card's failure from the dashboard payload.
func widgetError(t *testing.T, cards []any, id string) string {
	t.Helper()
	for _, entry := range cards {
		card, _ := entry.(map[string]any)
		if card != nil && card["id"] == id {
			value, _ := card["error"].(string)
			return value
		}
	}
	return ""
}

// probeUIExtension asks whether an application can put its own screen into
// the panel — not whether the mount surface has a key called "plugin".
//
// Three things make it a screen OF THE PANEL rather than a URL the
// application also serves: the navigation lists it, it is served under the
// panel's prefix, and it knows which operator is reading it because the
// panel authenticated them. The probe asks for all three, and refuses the
// SPA shell as an answer: this bench has already been fooled once by a 200
// that was the fallback's HTML (OPS-15).
func probeUIExtension(t *testing.T, e *env) verdict {
	nav := e.get(t, "/admin/api/ui/extensions")
	if nav.code != http.StatusOK || nav.servedTheShell() {
		t.Logf("the panel lists no application screens (%d %s)", nav.code, nav.ctype)
		return absent
	}
	url, ok := pageURLFor(nav.json(t), "reports")
	if !ok {
		t.Logf("the declared page is not in the navigation: %s", nav.text())
		return absent
	}

	page := e.get(t, url)
	if page.code != http.StatusOK {
		t.Logf("the declared page answered %d: %s", page.code, page.text())
		return absent
	}
	if !strings.Contains(page.raw(), reportsMarker) {
		t.Logf("%s served something that is not the application's page: %s", url, page.text())
		return absent
	}
	if !strings.Contains(page.raw(), "operator=admin") {
		t.Logf("the page runs, but the panel does not tell it who is reading: %s", page.text())
		return partial
	}
	return present
}

// probeI18n asks whether the panel can speak anything but English — which is
// three things, and a language menu is none of them: the document has to
// declare the language it is in (that is what a screen reader pronounces it
// with), the chrome's phrases have to arrive translated, and an application
// has to be able to add or override one.
func probeI18n(t *testing.T, e *env) verdict {
	page := e.get(t, "/admin/")
	if page.code != http.StatusOK {
		t.Logf("GET /admin/ answered %d", page.code)
		return absent
	}
	if !strings.Contains(page.raw(), `<html lang="es"`) {
		t.Logf("the served document does not declare the configured language: %s", page.text())
		return absent
	}

	catalogue := e.get(t, "/admin/ui/messages.json")
	if catalogue.code != http.StatusOK || catalogue.servedTheShell() {
		t.Logf("the panel serves no message catalogue (%d %s)", catalogue.code, catalogue.ctype)
		return absent
	}
	payload := catalogue.json(t)
	if payload["locale"] != "es" {
		t.Logf("the catalogue is not in the configured language: %v", payload["locale"])
		return partial
	}
	messages, _ := payload["messages"].(map[string]any)
	if messages["nav.audit"] != "Auditoría" {
		t.Logf("the panel's own phrases are not translated: %v", messages["nav.audit"])
		return partial
	}
	// A phrase the panel ships in English and nobody translated still
	// reads as a sentence: a catalogue that answered keys would be worse
	// than English.
	if text, _ := messages["nav.overview"].(string); text == "" || text == "nav.overview" {
		t.Logf("an untranslated key does not fall back to English: %v", messages["nav.overview"])
		return partial
	}
	// And the application's own override wins over the panel's phrase.
	if messages["dashboard.widgets"] != "Tu aplicación del banco" {
		t.Logf("the application's own phrase does not override the panel's: %v", messages["dashboard.widgets"])
		return partial
	}
	return present
}

// memoryStore is the smallest RecordStore that works: enough for a probe to
// check that a panel mounted over a foreign backend actually browses it.
type memoryStore struct {
	rows map[string]datasource.Record
	next int
}

func (m *memoryStore) List(_ context.Context, q datasource.Query) (datasource.Page, error) {
	items := make([]datasource.Record, 0, len(m.rows))
	for _, rec := range m.rows {
		items = append(items, rec)
	}
	return datasource.Page{Items: items, Total: int64(len(items)), Page: 1, PageSize: q.PageSize, TotalPages: 1}, nil
}

func (m *memoryStore) Get(_ context.Context, id string) (datasource.Record, error) {
	rec, ok := m.rows[id]
	if !ok {
		return nil, fmt.Errorf("gadget %q not found", id)
	}
	return rec, nil
}

func (m *memoryStore) Create(_ context.Context, rec datasource.Record) (datasource.Record, error) {
	m.next++
	id := fmt.Sprintf("%d", m.next)
	rec["id"] = id
	m.rows[id] = rec
	return rec, nil
}

func (m *memoryStore) Update(_ context.Context, id string, rec datasource.Record) error {
	rec["id"] = id
	m.rows[id] = rec
	return nil
}

func (m *memoryStore) Delete(_ context.Context, id string) error {
	delete(m.rows, id)
	return nil
}

func (m *memoryStore) Count(context.Context) (datasource.CountResult, error) {
	return datasource.CountResult{Count: int64(len(m.rows)), Present: true}, nil
}

func (m *memoryStore) TableExists(context.Context) bool { return true }

type memorySource struct{ store *memoryStore }

func (s *memorySource) All() []datasource.ModelInfo {
	return []datasource.ModelInfo{s.model()}
}

func (s *memorySource) Get(name string) (datasource.ModelInfo, bool) {
	if strings.EqualFold(name, "Gadget") {
		return s.model(), true
	}
	return datasource.ModelInfo{}, false
}

func (s *memorySource) Store(string, string) (datasource.RecordStore, error) { return s.store, nil }

func (s *memorySource) model() datasource.ModelInfo {
	return datasource.ModelInfo{
		Name: "Gadget", Plural: "Gadgets", Table: "gadgets", PrimaryKey: "id",
		Fields: []datasource.FieldInfo{
			{Name: "ID", Column: "id", Label: "ID", GoType: "string", HTMLType: "text", IsPK: true},
			{Name: "Name", Column: "name", Label: "Name", GoType: "string", HTMLType: "text", IsList: true},
		},
	}
}

// probeCustomDataSource mounts the panel over a backend that is not the
// framework's — the claim that Orbit can be the admin of Quark, or of
// someone else's storage, without a fork.
func probeCustomDataSource(t *testing.T, e *env) verdict {
	src := &memorySource{store: &memoryStore{rows: map[string]datasource.Record{}}}

	cfg := benchConfig(t)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config: cfg,
		Modules: map[string]nucleus.ModuleSpec{
			"orbit": orbit.Module(orbit.Config{
				Prefix:            "/admin",
				Title:             "Foreign Backend",
				BootstrapUsername: "admin",
				BootstrapEmail:    "admin@example.test",
				BootstrapPassword: bootstrapPassword,
				DataSource:        src,
			}),
		},
	})
	client := signInTo(t, srv, "admin", bootstrapPassword)

	resp, err := client.Get(srv.URL("/admin/api/models"))
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload map[string]any
	decodeInto(t, resp.Body, &payload)
	if !strings.Contains(mapText(payload), "Gadget") {
		t.Logf("a panel mounted over a foreign source does not list its model: %v", payload)
		return absent
	}
	return present
}

// probeMultiTenant confines the panel to the tenant the host resolved. The
// probe boots its own application because multi-tenancy is a mount-time
// posture, not something a request turns on.
func probeMultiTenant(t *testing.T, e *env) verdict {
	cfg := benchConfig(t)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config: cfg,
		Modules: map[string]nucleus.ModuleSpec{
			"content": contentModule(),
			"orbit": orbit.Module(orbit.Config{
				Prefix:             "/admin",
				Title:              "Tenanted",
				BootstrapUsername:  "admin",
				BootstrapEmail:     "admin@example.test",
				BootstrapPassword:  bootstrapPassword,
				MultiTenantEnabled: true,
				MultiTenantDefault: "acme",
				MultiTenantIDs:     []string{"acme", "globex"},
			}),
		},
	})
	client := signInTo(t, srv, "admin", bootstrapPassword)

	resp, err := client.Get(srv.URL("/admin/api/models/Note?tenant=globex"))
	if err != nil {
		t.Fatalf("tenant-scoped list: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Logf("a tenant-scoped list answered %d", resp.StatusCode)
		return partial
	}
	return present
}

// probeServesUI checks the panel actually ships a built interface, not just
// an API.
func probeServesUI(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/")
	if r.code != http.StatusOK {
		return absent
	}
	// raw(), not text(): text() truncates for logs, and this assertion is a
	// MEASUREMENT. The verdict flipped to partial the day the panel started
	// injecting branding and locale meta tags — the head grew and pushed
	// `<div id="root"` past the truncation, with nothing about the built
	// interface having changed. It is the same trap this bench recorded in
	// its first run, found again from the other side.
	if !strings.Contains(r.raw(), "<div id=\"root\"") && !strings.Contains(r.raw(), "assets/") {
		t.Logf("the page served is not a built SPA: %s", r.text())
		return partial
	}
	return present
}

// probeUILanguageTag reads the one accessibility fact the served document can
// be asked for from here: whether it declares the language a screen reader
// should pronounce it in.
func probeUILanguageTag(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/")
	if r.code != http.StatusOK {
		return absent
	}
	if strings.Contains(r.raw(), "<html lang=") {
		return present
	}
	t.Logf("the served document declares no language: %s", r.text())
	return absent
}

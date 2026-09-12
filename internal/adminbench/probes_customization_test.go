// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"fmt"
	"net/http"
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
// our colours. The mount surface is the whole of what an application can set,
// so its key list is the measurement.
func probeBranding(t *testing.T, e *env) verdict {
	keys := configKeysMatching("logo", "brand", "theme", "color", "colour", "favicon", "css")
	if len(keys) > 0 {
		t.Logf("branding keys on the mount surface: %v", keys)
		return partial
	}
	t.Logf("the mount surface carries no branding key beyond title")
	return absent
}

// probeDashboardWidgets asks for the landing screen an admin product opens
// on: the counters and charts a team chooses.
func probeDashboardWidgets(t *testing.T, e *env) verdict {
	if keys := configKeysMatching("dashboard", "widget", "card"); len(keys) > 0 {
		t.Logf("dashboard keys on the mount surface: %v", keys)
		return partial
	}
	return e.unrouted(t, "/admin/api/dashboard", "/admin/api/widgets")
}

// probeUIExtension asks whether an application can put its own screen, panel
// or button into the UI.
func probeUIExtension(t *testing.T, e *env) verdict {
	keys := configKeysMatching("hook", "plugin", "extension", "page", "nav", "menu")
	if len(keys) > 0 {
		t.Logf("extension keys on the mount surface: %v", keys)
		return partial
	}
	return absent
}

// probeI18n asks whether the panel can speak anything but English.
func probeI18n(t *testing.T, e *env) verdict {
	if keys := configKeysMatching("locale", "lang", "i18n", "translation"); len(keys) > 0 {
		t.Logf("language keys on the mount surface: %v", keys)
		return partial
	}
	english := e.get(t, "/admin/")
	spanish := e.do(t, http.MethodGet, "/admin/?lang=es", nil)
	if english.text() != spanish.text() {
		t.Logf("the page changes with ?lang=es: a language surface exists")
		return partial
	}
	return absent
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
	body := r.text()
	if !strings.Contains(body, "<div id=\"root\"") && !strings.Contains(body, "assets/") {
		t.Logf("the page served is not a built SPA: %s", body)
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

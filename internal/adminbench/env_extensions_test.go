// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	"github.com/jcsvwinston/orbit"
)

// What an application adds to the panel, as the bench's application adds it
// (S7): one action of its own on one of its models, and one screen of its
// own. Both are declared the way an author writes them — through
// orbit.Config, with a plain function and a plain http.Handler — so the
// probes measure the product's extension points and not a hook the bench
// reached in and installed.

// extensions holds what the probed application needs to run its own action:
// the database handle, taken from the runtime in OnStart, and the last call
// the action received, so a probe can ask what the panel told it.
type extensions struct {
	mu       sync.Mutex
	db       *sql.DB
	lastCall orbit.ActionRequest
	calls    int
}

// benchExtensions is the application's, not the bench's: the module below
// fills it at startup and the action closes over it, which is what an
// application does with its own dependencies.
var benchExtensions = &extensions{}

// extensionsModule lends the application's database handle to the action and
// the widget this application declares. It is mounted only on the shared
// bench application (env_test.go).
func extensionsModule() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "benchext",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			benchExtensions.captureRuntime(rt)
			return nil
		},
	}.Build()
}

// captureRuntime records the handle the action and the widget read through.
// It runs before any request.
func (x *extensions) captureRuntime(rt nucleus.Runtime) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if h := rt.DatabaseHandle(); h != nil {
		if sqlDB, err := h.SqlDB(); err == nil {
			x.db = sqlDB
		}
	}
}

func (x *extensions) handle() *sql.DB {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.db
}

func (x *extensions) record(req orbit.ActionRequest) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.lastCall = req
	x.calls++
}

func (x *extensions) lastRequest() (orbit.ActionRequest, int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.lastCall, x.calls
}

// publishAction is the "publish these three" every admin product grows: a
// verb the panel cannot infer from a column called status, over the rows the
// operator selected.
func publishAction() orbit.ModelAction {
	return orbit.ModelAction{
		Name:        "publish",
		Model:       "Note",
		Label:       "Publish",
		Description: "Mark the selected notes as published",
		Confirm:     "Publish the selected notes?",
		Destructive: true,
		Run: func(ctx context.Context, req orbit.ActionRequest) (orbit.ActionResult, error) {
			benchExtensions.record(req)
			handle := benchExtensions.handle()
			if handle == nil {
				return orbit.ActionResult{}, fmt.Errorf("no database handle")
			}
			affected := 0
			for _, id := range req.IDs {
				res, err := handle.ExecContext(ctx, "UPDATE notes SET status = 'published' WHERE id = ?", id)
				if err != nil {
					return orbit.ActionResult{Affected: affected}, err
				}
				if n, err := res.RowsAffected(); err == nil {
					affected += int(n)
				}
			}
			return orbit.ActionResult{
				Message:  fmt.Sprintf("%d note(s) published", affected),
				Affected: affected,
				Data:     map[string]any{"status": "published"},
			}, nil
		},
	}
}

// reportsPage is the screen an application has that the panel could never
// ship: it writes its own document and names the operator reading it, which
// is what makes it a page of the panel and not a public URL.
const reportsMarker = "bench-extension-report"

func reportsPage() orbit.Page {
	return orbit.Page{
		ID:          "reports",
		Title:       "Bench Reports",
		Description: "A screen this application added",
		Icon:        "chart",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operator, ok := orbit.OperatorFromContext(r.Context())
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// The path the handler sees is its own, so a page can route
			// below its root without knowing where the panel is mounted.
			fmt.Fprintf(w, "<!doctype html><title>%s</title><p>%s</p><p>operator=%s authenticated=%t</p><p>path=%s</p>",
				reportsMarker, reportsMarker, operator.Username, ok, r.URL.Path)
		}),
	}
}

// benchBranding is what a product team asks for on day one: our logo, our
// colour, our icon in the tab.
func benchBranding() orbit.Branding {
	return orbit.Branding{
		LogoURL:      "/static/bench-logo.svg",
		FaviconURL:   "https://example.test/bench.ico",
		PrimaryColor: "#0b5fff",
	}
}

// benchWidgets are the application's own numbers on the panel's landing
// screen: one card that counts something of the application's, and one that
// fails, because a screen that drops a broken card reports a broken query as
// "nothing to see".
func benchWidgets() []orbit.Widget {
	return []orbit.Widget{
		{
			ID:          "pending-notes",
			Title:       "Pending notes",
			Description: "Drafts nobody has published yet",
			Link:        "/data-studio",
			Load: func(ctx context.Context) (orbit.WidgetValue, error) {
				handle := benchExtensions.handle()
				if handle == nil {
					return orbit.WidgetValue{}, fmt.Errorf("no database handle")
				}
				var count int
				if err := handle.QueryRowContext(ctx,
					"SELECT COUNT(*) FROM notes WHERE status <> 'published'").Scan(&count); err != nil {
					return orbit.WidgetValue{}, err
				}
				return orbit.WidgetValue{
					Value:  fmt.Sprintf("%d", count),
					Detail: "since the bench started",
				}, nil
			},
		},
		{
			ID:    "broken-card",
			Title: "Broken card",
			Load: func(context.Context) (orbit.WidgetValue, error) {
				return orbit.WidgetValue{}, fmt.Errorf("this reading is unavailable")
			},
		},
	}
}

// benchMessages is an application adding to the panel's own phrases — and
// translating one of its own words, in a locale the panel ships.
func benchMessages() map[string]map[string]string {
	return map[string]map[string]string{
		"es": {"dashboard.widgets": "Tu aplicación del banco"},
	}
}

// benchOrbitConfig is the mounted panel's configuration, in one place so
// every probe that boots its own application declares the same extension
// points as the shared one.
func benchOrbitConfig() orbit.Config {
	return orbit.Config{
		Prefix:            "/admin",
		Title:             "Admin Bench",
		BootstrapUsername: "admin",
		BootstrapEmail:    "admin@example.test",
		BootstrapPassword: bootstrapPassword,
		RowOwnerFields:    map[string]string{"Article": "owner"},
		FieldWidgets: map[string]string{
			"Note.Body":  "richtext",
			"Note.Cover": "image",
			"Note.Meta":  "json",
		},
		Actions:  []orbit.ModelAction{publishAction()},
		Pages:    []orbit.Page{reportsPage()},
		Branding: benchBranding(),
		Widgets:  benchWidgets(),
		Locale:   "es",
		Messages: benchMessages(),
	}
}

// pageURLFor finds a declared page in the navigation payload.
func pageURLFor(payload map[string]any, id string) (string, bool) {
	pages, _ := payload["pages"].([]any)
	for _, entry := range pages {
		page, _ := entry.(map[string]any)
		if page == nil {
			continue
		}
		if fmt.Sprintf("%v", page["id"]) != id {
			continue
		}
		url := strings.TrimSpace(fmt.Sprintf("%v", page["url"]))
		return url, url != ""
	}
	return "", false
}

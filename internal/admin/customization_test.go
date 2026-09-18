// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"

	"log/slog"
)

// TestBrandingRefusesWhatWouldBeAnInjection is the one that matters: the
// values are written into the document, so a `javascript:` logo would be
// script execution on every page of the panel, granted by a line of YAML.
func TestBrandingRefusesWhatWouldBeAnInjection(t *testing.T) {
	cases := []struct {
		name string
		in   Branding
		want string
	}{
		{name: "javascript url", in: Branding{LogoURL: "javascript:alert(1)"}, want: "scheme"},
		{name: "data url", in: Branding{FaviconURL: "data:image/svg+xml,<svg onload=alert(1)>"}, want: "scheme"},
		{name: "protocol relative", in: Branding{LogoURL: "//evil.example/logo.svg"}, want: "protocol-relative"},
		{name: "bare host", in: Branding{LogoURL: "evil.example/logo.svg"}, want: "neither an absolute"},
		{name: "colour that is css", in: Branding{PrimaryColor: "red;} body{display:none"}, want: "hex colour"},
		{name: "colour by name", in: Branding{PrimaryColor: "rebeccapurple"}, want: "hex colour"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateBranding(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected a refusal explaining %q, got %v", tc.want, err)
			}
		})
	}
}

// TestBrandingAcceptsWhatABrowserCanLoad: an absolute https URL, a path the
// application already serves, and the two hex forms.
func TestBrandingAcceptsWhatABrowserCanLoad(t *testing.T) {
	b, err := validateBranding(Branding{
		LogoURL:      "/static/logo.svg",
		FaviconURL:   "https://cdn.example.test/favicon.ico",
		PrimaryColor: "#0b5fff",
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !b.declared() {
		t.Fatalf("branding reports itself undeclared: %+v", b)
	}
	for _, colour := range []string{"#fff", "#0B5FFF"} {
		if _, err := validateBranding(Branding{PrimaryColor: colour}); err != nil {
			t.Fatalf("colour %s refused: %v", colour, err)
		}
	}
}

// TestBrandingReachesTheLoginPage is where it matters most: the login screen
// renders before there is a session, so it cannot ask an API whose product
// this is.
func TestBrandingReachesTheDocument(t *testing.T) {
	content := []byte("<html lang=\"en\"><head></head><body><div id=\"root\"></div></body></html>")
	out := string(injectBranding(content, Branding{
		LogoURL: "/static/logo.svg", FaviconURL: "/favicon.ico", PrimaryColor: "#0b5fff",
	}))
	for _, want := range []string{"nucleus-admin-logo", "/static/logo.svg", "nucleus-admin-primary-color", "#0b5fff"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the document does not carry %q: %s", want, out)
		}
	}
	// The document keeps its own body: branding is injected, not templated.
	if !strings.Contains(out, `<div id="root">`) {
		t.Fatalf("injection damaged the document: %s", out)
	}
}

// TestLocaleReachesTheDocument: the lang attribute is what a screen reader
// pronounces the page with, so it is set on the element and not only in a
// meta tag.
func TestLocaleReachesTheDocument(t *testing.T) {
	content := []byte(`<html lang="en"><head></head><body></body></html>`)
	out := string(injectLocale(content, "es"))
	if !strings.Contains(out, `<html lang="es"`) {
		t.Fatalf("the document still declares English: %s", out)
	}
	if !strings.Contains(out, "nucleus-admin-locale") {
		t.Fatalf("the locale does not reach the SPA: %s", out)
	}
}

func TestValidateLocaleRefusesWhatIsNotALanguageTag(t *testing.T) {
	for _, bad := range []string{"es-ES-extra-long-subtag-here", "es_ES", "<script>", "e s"} {
		if _, err := validateLocale(bad); err == nil {
			t.Fatalf("%q was accepted as a language tag", bad)
		}
	}
	for in, want := range map[string]string{"": "en", "ES": "es", "pt-br": "pt-BR", "es": "es"} {
		got, err := validateLocale(in)
		if err != nil || got != want {
			t.Fatalf("locale %q resolved to %q (%v), want %q", in, got, err, want)
		}
	}
}

// TestCatalogueMergesRatherThanChooses is what makes a partial translation
// useful — and what lets an application translate the panel into a language
// the panel does not ship.
func TestCatalogueMergesRatherThanChooses(t *testing.T) {
	p := &Panel{
		locale: "es",
		messages: map[string]map[string]string{
			"es": {"nav.audit": "Registro"},
			"eu": {"nav.audit": "Erregistroa"},
		},
	}

	locale, catalogue := p.catalogueFor("es")
	if locale != "es" {
		t.Fatalf("locale = %q", locale)
	}
	// The application's phrase wins over the panel's translation…
	if catalogue["nav.audit"] != "Registro" {
		t.Fatalf("the application's override lost: %q", catalogue["nav.audit"])
	}
	// …the panel's translation over English…
	if catalogue["nav.overview"] != "Resumen" {
		t.Fatalf("the panel's own translation is missing: %q", catalogue["nav.overview"])
	}
	// …and English is there for everything nobody translated.
	if catalogue["state.empty"] == "" {
		t.Fatalf("an untranslated phrase has no fallback: %+v", catalogue)
	}

	// A language the panel does not ship is still a language.
	_, basque := p.catalogueFor("eu")
	if basque["nav.audit"] != "Erregistroa" {
		t.Fatalf("an application-only locale was not served: %q", basque["nav.audit"])
	}
	if basque["nav.overview"] != "Overview" {
		t.Fatalf("the rest of an application-only locale does not fall back to English: %q", basque["nav.overview"])
	}
}

// widgetPanel is a panel with the given widgets, RBAC and a server.
func widgetPanel(t *testing.T, widgets []Widget, policies ...[3]string) (*Panel, *testAdminAuth, string) {
	t.Helper()
	provider := &testAdminAuth{user: &auth.User{ID: "actor", Username: "actor", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)

	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	for _, pol := range policies {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatalf("AddPolicy: %v", err)
		}
	}
	panel.rbac = enf

	compiled, err := validateWidgets(widgets)
	if err != nil {
		t.Fatalf("validate widgets: %v", err)
	}
	panel.widgets = compiled

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, provider, srv.URL
}

// TestWidgetsDegradeOneCardAtATime: a widget that fails, and one that takes
// too long, are reported as themselves. A screen that dropped them would
// report a broken query as "nothing to see", and one that waited would make
// the overview feel broken instead of the card.
func TestWidgetsDegradeOneCardAtATime(t *testing.T) {
	_, _, srv := widgetPanel(t, []Widget{
		{ID: "good", Title: "Good", Load: func(context.Context) (WidgetValue, error) {
			return WidgetValue{Value: "42", Detail: "answers"}, nil
		}},
		{ID: "bad", Title: "Bad", Load: func(context.Context) (WidgetValue, error) {
			return WidgetValue{}, fmt.Errorf("the report is unavailable")
		}},
		{ID: "panicky", Title: "Panicky", Load: func(context.Context) (WidgetValue, error) {
			panic("nil map")
		}},
	})

	payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboard", nil)
	if status != http.StatusOK {
		t.Fatalf("dashboard status=%d", status)
	}
	raw := mustJSON(payload)
	for _, want := range []string{`"value":"42"`, "the report is unavailable", "widget panicked"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("the dashboard does not carry %q: %s", want, raw)
		}
	}
}

// TestWidgetTimeoutBoundsTheScreen: the overview is a glance, and one
// application query must not hold it open.
func TestWidgetTimeoutBoundsTheScreen(t *testing.T) {
	_, _, srv := widgetPanel(t, []Widget{
		{ID: "slow", Title: "Slow", Load: func(ctx context.Context) (WidgetValue, error) {
			select {
			case <-ctx.Done():
				return WidgetValue{}, ctx.Err()
			case <-time.After(30 * time.Second):
				return WidgetValue{Value: "eventually"}, nil
			}
		}},
	})

	started := time.Now()
	payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboard", nil)
	if status != http.StatusOK {
		t.Fatalf("dashboard status=%d", status)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("the screen waited %s for one card", elapsed)
	}
	if !strings.Contains(mustJSON(payload), "deadline exceeded") {
		t.Fatalf("the slow card is not reported as timed out: %s", mustJSON(payload))
	}
}

// TestWidgetsAreAuthorized: a card is a reading of the application, so an
// operator who may not have that reading is not shown it — and the screen
// does not tell them it exists.
func TestWidgetsAreAuthorized(t *testing.T) {
	_, provider, srv := widgetPanel(t, []Widget{
		{ID: "finance", Title: "Revenue", Permission: "finance", Load: func(context.Context) (WidgetValue, error) {
			return WidgetValue{Value: "1.2M"}, nil
		}},
		{ID: "open", Title: "Open", Load: func(context.Context) (WidgetValue, error) {
			return WidgetValue{Value: "7"}, nil
		}},
	}, [3]string{"cfo", "admin:dashboard", "finance"}, [3]string{"cfo", "admin:dashboard", "view"},
		[3]string{"staff", "admin:dashboard", "view"})

	provider.user = &auth.User{ID: "staff", Username: "staff", Role: "staff"}
	payload, status := doJSON(t, http.MethodGet, srv+"/api/ui/dashboard", nil)
	if status != http.StatusOK {
		t.Fatalf("dashboard status=%d", status)
	}
	if strings.Contains(mustJSON(payload), "1.2M") || strings.Contains(mustJSON(payload), "Revenue") {
		t.Fatalf("an operator without the grant was shown the card: %s", mustJSON(payload))
	}
	if !strings.Contains(mustJSON(payload), `"value":"7"`) {
		t.Fatalf("the card they do hold is missing: %s", mustJSON(payload))
	}

	provider.user = &auth.User{ID: "cfo", Username: "cfo", Role: "cfo"}
	payload, status = doJSON(t, http.MethodGet, srv+"/api/ui/dashboard", nil)
	if status != http.StatusOK || !strings.Contains(mustJSON(payload), "1.2M") {
		t.Fatalf("the granted operator does not see the card (%d): %s", status, mustJSON(payload))
	}
}

// TestValidateWidgetsRefusesWhatWouldBeSilent covers the declarations that
// would otherwise be a card nobody ever sees.
func TestValidateWidgetsRefusesWhatWouldBeSilent(t *testing.T) {
	load := func(context.Context) (WidgetValue, error) { return WidgetValue{}, nil }
	cases := []struct {
		name string
		in   []Widget
		want string
	}{
		{name: "no id", in: []Widget{{Load: load}}, want: "must be lowercase"},
		{name: "no load", in: []Widget{{ID: "cards"}}, want: "Load is required"},
		{name: "duplicate", in: []Widget{{ID: "a", Load: load}, {ID: "A", Load: load}}, want: "already declared"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateWidgets(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected a refusal explaining %q, got %v", tc.want, err)
			}
		})
	}
}

// TestMessagesCatalogueIsServedBeforeSignIn: the login screen renders before
// there is a session, so a catalogue behind the session would leave the one
// page everybody sees in English.
func TestMessagesCatalogueIsServedBeforeSignIn(t *testing.T) {
	provider := &testAdminAuth{user: nil} // Authenticate fails: nobody is signed in
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)
	panel.locale = "es"
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)

	payload, status := doJSON(t, http.MethodGet, srv.URL+"/ui/messages.json?locale=es", nil)
	if status != http.StatusOK {
		t.Fatalf("the catalogue is not reachable before sign-in: %d", status)
	}
	messages, _ := payload["messages"].(map[string]interface{})
	if messages["login.submit"] != "Entrar" {
		t.Fatalf("the login phrases are not translated: %v", messages["login.submit"])
	}
}

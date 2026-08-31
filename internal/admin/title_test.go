package admin

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// The product must say its own name (OH-2/PR-ORB-01): the default title is
// "Orbit" — not the framework's name — and the configured Title reaches the
// served pages as the nucleus-admin-title meta tag the SPA renders in the
// login screen and the sidebar.

func titleTestSnippet(s string) string {
	if len(s) > 400 {
		return s[:400]
	}
	return s
}

func TestNewPanel_DefaultTitleIsOrbit(t *testing.T) {
	p := NewPanel(nil, nil, PanelConfig{})
	if p.config.Title != "Orbit" {
		t.Errorf("default Title = %q, want Orbit", p.config.Title)
	}
}

func TestHandleSPA_InjectsConfiguredTitle(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	panel.config.Title = "Acme Admin"

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `<meta name="nucleus-admin-title" content="Acme Admin">`) {
		t.Errorf("SPA shell does not carry the configured title meta:\n%s", titleTestSnippet(body))
	}
}

func TestLoginPage_RendersConfiguredTitle(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	a := NewDatabaseAdminAuth(sqlDB, nil, "/admin").WithTitle("Acme Admin")
	rec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/login", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "Acme Admin") {
		t.Errorf("login page does not render the configured title:\n%s", titleTestSnippet(body))
	}
	if strings.Contains(body, "Nucleus Admin") {
		t.Errorf("login page still says Nucleus Admin:\n%s", titleTestSnippet(body))
	}
}

func TestLoginPage_DefaultTitleIsOrbit(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	a := NewDatabaseAdminAuth(sqlDB, nil, "/admin")
	rec := httptest.NewRecorder()
	a.LoginHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/login", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "Orbit") {
		t.Errorf("login page does not carry the Orbit product name:\n%s", titleTestSnippet(body))
	}
}

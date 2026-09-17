package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// TestAPIPrefixAnswersJSONNotTheSPA pins OR-48: an unrouted path under the
// API prefix used to reach the single-page fallback, so a client asking for
// an endpoint that does not exist got 200 text/html and could not tell the
// difference between "no such endpoint" and "here is a web page".
func TestAPIPrefixAnswersJSONNotTheSPA(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	} {
		req, err := http.NewRequest(method, srv.URL+"/api/there-is-no-such-endpoint", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s /api/there-is-no-such-endpoint: got %d, want 404 (body=%s)",
				method, resp.StatusCode, body)
		}
		if ctype := resp.Header.Get("Content-Type"); !strings.Contains(ctype, "application/json") {
			t.Errorf("%s /api/there-is-no-such-endpoint: content-type %q, want JSON — the SPA fallback is still covering the API prefix",
				method, ctype)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("%s /api/there-is-no-such-endpoint: body is not JSON: %s", method, body)
		}
	}
}

// TestAPIPrefixKeepsMethodNotAllowed is the other half of OR-48. A catch-all
// registered for every method would answer 404 for a POST to a GET-only
// endpoint — claiming the endpoint does not exist when it does. The route
// map keeps the two apart.
func TestAPIPrefixKeepsMethodNotAllowed(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	// /api/models is registered for GET only.
	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /api/models: got %d, want 405 — a path that exists for another method must not report as missing (body=%s)",
			resp.StatusCode, body)
	}
}

// TestSPAStillServesItsOwnRoutes guards the blast radius of the change
// above: the fallback must keep serving the panel's client-side routes.
func TestSPAStillServesItsOwnRoutes(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/anything")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /dashboard/anything: got %d, want 200 — the SPA fallback must still answer outside /api/", resp.StatusCode)
	}
	if ctype := resp.Header.Get("Content-Type"); !strings.Contains(ctype, "text/html") {
		t.Fatalf("GET /dashboard/anything: content-type %q, want text/html", ctype)
	}
}

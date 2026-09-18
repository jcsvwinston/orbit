// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"

	"log/slog"
)

// TestValidatePagesRefusesWhatWouldBeSilent: every one of these would
// otherwise be a navigation entry that never appears, or a link that leads
// nowhere.
func TestValidatePagesRefusesWhatWouldBeSilent(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	cases := []struct {
		name  string
		pages []Page
		want  string
	}{
		{name: "no id", pages: []Page{{Handler: handler}}, want: "must be lowercase"},
		{name: "path in id", pages: []Page{{ID: "a/b", Handler: handler}}, want: "must be lowercase"},
		{name: "spaces", pages: []Page{{ID: "my page", Handler: handler}}, want: "must be lowercase"},
		{name: "no handler", pages: []Page{{ID: "reports"}}, want: "Handler is required"},
		{
			name:  "duplicate",
			pages: []Page{{ID: "reports", Handler: handler}, {ID: "Reports", Handler: handler}},
			want:  "already declared",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validatePages(tc.pages); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected a refusal explaining %q, got %v", tc.want, err)
			}
		})
	}
}

// pagePanel is a panel serving one application page, with RBAC in place.
func pagePanel(t *testing.T, page Page, policies ...[3]string) (*Panel, *httptest.Server, *testAdminAuth) {
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
			t.Fatalf("AddPolicy %v: %v", pol, err)
		}
	}
	panel.rbac = enf

	pages, err := validatePages([]Page{page})
	if err != nil {
		t.Fatalf("validate pages: %v", err)
	}
	panel.pages = pages

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, srv, provider
}

// echoPage is an application's own screen: it writes who is reading it and
// what path it was given, which is everything a page needs from the panel.
func echoPage() Page {
	return Page{
		ID:    "reports",
		Title: "Reports",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operator, ok := OperatorFromContext(r.Context())
			fmt.Fprintf(w, "page-body operator=%s known=%t path=%s", operator.Username, ok, r.URL.Path)
		}),
	}
}

// TestApplicationPageIsServedInsideThePanel: under the panel's prefix, with
// the operator on the context and the page's own path — not the panel's.
func TestApplicationPageIsServedInsideThePanel(t *testing.T) {
	_, srv, _ := pagePanel(t, echoPage())

	body, status := getPage(t, srv.URL+"/x/reports/")
	if status != http.StatusOK {
		t.Fatalf("page status=%d body=%s", status, body)
	}
	if !strings.Contains(body, "page-body") {
		t.Fatalf("the panel served something else: %s", body)
	}
	if !strings.Contains(body, "operator=actor known=true") {
		t.Fatalf("the page was not told who is reading it: %s", body)
	}
	// The handler sees its own root, so it can build links without
	// knowing where the panel is mounted.
	if !strings.Contains(body, "path=/") {
		t.Fatalf("the page sees the panel's path instead of its own: %s", body)
	}

	// A path below the page's root reaches it too: a screen that serves
	// its own sub-pages is a screen, not a single document.
	body, status = getPage(t, srv.URL+"/x/reports/monthly")
	if status != http.StatusOK || !strings.Contains(body, "path=/monthly") {
		t.Fatalf("a sub-path of the page answered %d: %s", status, body)
	}
}

// TestApplicationPageIsAuthorized: a page is gated on admin:page:<id>, in
// its own namespace, so a grant about a model can never open a screen.
func TestApplicationPageIsAuthorized(t *testing.T) {
	_, srv, provider := pagePanel(t, echoPage(),
		[3]string{"viewer", "admin:page:reports", "view"})

	provider.user = &auth.User{ID: "stranger", Username: "stranger", Role: "stranger"}
	body, status := getPage(t, srv.URL+"/x/reports/")
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for an operator without the grant, got %d: %s", status, body)
	}

	provider.user = &auth.User{ID: "viewer", Username: "viewer", Role: "viewer"}
	body, status = getPage(t, srv.URL+"/x/reports/")
	if status != http.StatusOK || !strings.Contains(body, "page-body") {
		t.Fatalf("the granted operator was refused (%d): %s", status, body)
	}
}

// TestApplicationPageNavigationListsOnlyWhatIsOpenable: the list IS the
// navigation, so a page this operator cannot open is not in it. A link that
// refuses is a worse answer than no link.
func TestApplicationPageNavigationListsOnlyWhatIsOpenable(t *testing.T) {
	_, srv, provider := pagePanel(t, echoPage(),
		[3]string{"viewer", "admin:page:reports", "view"})

	payload, status := doJSON(t, http.MethodGet, srv.URL+"/api/ui/extensions", nil)
	if status != http.StatusOK {
		t.Fatalf("extensions status=%d", status)
	}
	raw := mustJSON(payload)
	if !strings.Contains(raw, `"id":"reports"`) || !strings.Contains(raw, `"url":"/admin/x/reports/"`) {
		t.Fatalf("the navigation payload does not carry the page and its URL: %s", raw)
	}

	provider.user = &auth.User{ID: "stranger", Username: "stranger", Role: "stranger"}
	payload, status = doJSON(t, http.MethodGet, srv.URL+"/api/ui/extensions", nil)
	if status != http.StatusOK {
		t.Fatalf("extensions status=%d", status)
	}
	if strings.Contains(mustJSON(payload), "reports") {
		t.Fatalf("a page this operator cannot open is listed for them: %s", mustJSON(payload))
	}
}

// TestApplicationPageDoesNotShadowTheSPA: the panel's own routes keep
// working with a page mounted, including the fallback the SPA is served by.
func TestApplicationPageDoesNotShadowTheSPA(t *testing.T) {
	_, srv, _ := pagePanel(t, echoPage())

	if _, status := doJSON(t, http.MethodGet, srv.URL+"/api/models", nil); status != http.StatusOK {
		t.Fatalf("the model list answered %d with a page mounted", status)
	}
	body, status := getPage(t, srv.URL+"/data-studio")
	if status != http.StatusOK || strings.Contains(body, "page-body") {
		t.Fatalf("the SPA fallback answered %d: %s", status, body)
	}
}

// getPage reads a page's body and status. getText, next door, drops the
// status: a page test that cannot see a 403 measures nothing.
func getPage(t *testing.T, url string) (string, int) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	return string(body), res.StatusCode
}

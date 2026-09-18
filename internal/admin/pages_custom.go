// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// Screens an application adds to the panel (CUST-04).
//
// The panel's screens are the ones every application has. A product always
// grows one that is only its own — a reconciliation report, a switchboard
// for a batch nobody else runs — and until now the only extension point was
// DataSource, which replaces the backend and leaves the interface alone.
//
// A page is an ordinary http.Handler the application writes, mounted INSIDE
// the panel: under the panel's prefix, behind the panel's session, gated by
// the panel's RBAC, listed in the panel's navigation. That is the whole of
// what it buys — the application still writes the page. What it saves is
// everything around it: an application that served the same handler from its
// own router would have to re-implement the sign-in, the authorization and
// the link.
//
// It is a link and not a frame on purpose. The panel sends X-Frame-Options:
// DENY and frame-ancestors 'none' on every response, so a page embedded in
// the SPA would be blocked by the browser while every Go test still read a
// 200 — and relaxing that header for the whole panel to embed one screen
// trades a clickjacking defence for a layout. The same hardening applies to
// the page's own response: its scripts come from files, not from inline
// <script> (script-src 'self').
type Page struct {
	// ID is the path segment the page is served under (<prefix>/x/<id>/)
	// and the key the navigation uses. Lowercase letters, digits and
	// dashes: it is a URL, not a title.
	ID string
	// Title is what the navigation entry reads.
	Title string
	// Description is the one line a UI can show under the title.
	Description string
	// Icon names an icon the SPA already ships, as the model screens do.
	// Unknown or empty falls back to a default.
	Icon string
	// Permission is the RBAC action required on resource admin:page:<id>.
	// Empty means "view". A superuser always passes, as everywhere else in
	// the panel.
	Permission string
	// Handler serves the page. It receives the request with the panel's
	// prefix already stripped ("/" is the page's own root) and with the
	// operator in its context — see OperatorFromContext.
	Handler http.Handler
}

// pageOperatorKey is the context key the operator travels under.
type pageOperatorKey struct{}

// Operator is who is looking at an application's page: enough to greet them,
// scope what the page shows and write the application's own log line. It is
// a copy, so a page cannot change the session by writing to it.
type Operator struct {
	ID          string
	Username    string
	Email       string
	IsSuperuser bool
}

// OperatorFromContext returns the panel operator the request was
// authenticated as. ok is false on a panel with no auth provider (the
// development posture of ADR-016), which is the only way a page is reached
// by nobody.
func OperatorFromContext(ctx context.Context) (Operator, bool) {
	if ctx == nil {
		return Operator{}, false
	}
	op, ok := ctx.Value(pageOperatorKey{}).(Operator)
	return op, ok
}

// withOperator puts the authenticated operator on the request context for an
// application page.
func withOperator(ctx context.Context, user *auth.User) context.Context {
	if user == nil {
		return ctx
	}
	return context.WithValue(ctx, pageOperatorKey{}, Operator{
		ID: user.ID, Username: user.Username, Email: user.Email, IsSuperuser: user.IsSuperuser,
	})
}

// pageIDPattern is what an ID may contain. It is checked at startup: an ID
// with a slash in it would mount a subtree nobody asked for, and one with a
// space would be a link that never resolves.
func validPageID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// validatePages checks the declared pages before the panel serves them, for
// the same reason the actions are checked: every one of these mistakes is
// otherwise a screen that silently never appears.
func validatePages(pages []Page) ([]Page, error) {
	seen := make(map[string]struct{}, len(pages))
	out := make([]Page, 0, len(pages))
	for i, page := range pages {
		id := strings.ToLower(strings.TrimSpace(page.ID))
		if !validPageID(id) {
			return nil, fmt.Errorf("pages[%d]: ID %q must be lowercase letters, digits or dashes", i, page.ID)
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("pages[%d]: a page with ID %q is already declared", i, id)
		}
		if page.Handler == nil {
			return nil, fmt.Errorf("pages[%d] (%s): Handler is required", i, id)
		}
		seen[id] = struct{}{}
		page.ID = id
		if strings.TrimSpace(page.Title) == "" {
			page.Title = id
		}
		if strings.TrimSpace(page.Permission) == "" {
			page.Permission = "view"
		}
		out = append(out, page)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ValidatePages is validatePages for the module wiring, which reports the
// error as a refusal to start.
func ValidatePages(pages []Page) error {
	_, err := validatePages(pages)
	return err
}

// pageResource is the RBAC resource one page is authorized under. It is
// namespaced away from the models ("admin:page:reports"), so a policy about
// a model can never accidentally grant a screen and the other way round.
func pageResource(id string) string { return "page:" + id }

// pagePath is where a page is served, relative to the panel prefix.
func pagePath(id string) string { return "/x/" + id }

// mountPageRoutes serves each declared page under the panel's prefix. It is
// registered inside the authenticated group, so a page is never reachable
// without a session — the handler an application wrote does not have to
// check.
func (p *Panel) mountPageRoutes(m *router.Mux) {
	for _, page := range p.pages {
		page := page
		handler := router.FromHandler(p.servePage(page))
		// Both the bare path and the subtree: the link in the navigation
		// points at <prefix>/x/<id>, and a page that serves its own
		// assets or sub-paths gets them under the same root.
		//
		// The methods are spelled out rather than registered with Handle
		// for all of them, because the SPA fallback next to these is a
		// GET-only catch-all: a method-less pattern here is AMBIGUOUS
		// against it for Go's ServeMux, which refuses to build the router
		// at all — the application failed to start, which is how this was
		// found.
		for _, path := range []string{pagePath(page.ID), pagePath(page.ID) + "/{path...}"} {
			m.Get(path, handler)
			m.Post(path, handler)
			m.Put(path, handler)
			m.Patch(path, handler)
			m.Delete(path, handler)
		}
	}
}

// servePage wraps an application's handler with the authorization the panel
// owes it and the operator its context carries. Authentication is already
// done: the page is mounted inside the panel's authenticated group, so a
// browser with no session is redirected to the login before it gets here.
func (p *Panel) servePage(page Page) http.Handler {
	// The panel's own mount already stripped the panel prefix (nucleus's
	// Router.Mount does it), so what is left to strip here is the page's
	// own segment and nothing else.
	mountPoint := pagePath(page.ID)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.config.Auth != nil {
			user, err := p.authenticatedUser(r)
			if err != nil {
				writeErr(w, r, p.authErrorToDomain(err))
				return
			}
			if err := p.authorizePage(user, page); err != nil {
				writeErr(w, r, err)
				return
			}
			r = r.WithContext(withOperator(r.Context(), user))
		}
		// The page's own root is "/": it is mounted, not routed, so it
		// sees the path below its mount point and can build links
		// without knowing where the panel lives.
		http.StripPrefix(mountPoint, page.Handler).ServeHTTP(w, r)
	})
}

// authorizePage answers whether this operator may open the page.
func (p *Panel) authorizePage(user *auth.User, page Page) error {
	if p.rbac == nil {
		// No enforcer: the panel's auth provider is the only opinion
		// there is, and it answers about models. A page is not a model,
		// so an authenticated operator opens it — the same posture the
		// panel's own screens have in that configuration.
		return nil
	}
	if user != nil && user.IsSuperuser {
		return nil
	}
	resource := "admin:" + pageResource(page.ID)
	for _, subject := range subjectsOf(user) {
		if p.rbac.Can(subject, resource, page.Permission) {
			return nil
		}
	}
	return authDeniedDomain(pageResource(page.ID), page.Permission)
}

// pageDescriptor is one page as the navigation payload carries it.
type pageDescriptor struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	// URL is absolute from the site root, so the SPA links to it without
	// having to know the panel prefix it was served under.
	URL string `json:"url"`
}

// handleListUIExtensions lists the pages this operator may open. A page they
// may not open is absent from the list, not greyed out in it: the list IS
// the navigation, and a link that refuses is a worse answer than no link.
func (p *Panel) handleListUIExtensions(c *router.Context) error {
	prefix := strings.TrimSuffix(NormalizePrefix(p.config.Prefix), "/")
	var user *auth.User
	if p.config.Auth != nil {
		user, _ = p.authenticatedUser(c.Request)
	}
	pages := make([]pageDescriptor, 0, len(p.pages))
	for _, page := range p.pages {
		if err := p.authorizePage(user, page); err != nil {
			continue
		}
		pages = append(pages, pageDescriptor{
			ID: page.ID, Title: page.Title, Description: page.Description,
			Icon: page.Icon, URL: prefix + pagePath(page.ID) + "/",
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"pages": pages})
}

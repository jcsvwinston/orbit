// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestBrandingImageOriginsAreWhatTheBrowserFetches: an absolute logo or
// favicon contributes its origin — scheme, host and port, never the path or
// the query — a same-site path contributes nothing ('self' covers it), and an
// internationalised host is named in the ASCII form a browser matches the
// policy against.
func TestBrandingImageOriginsAreWhatTheBrowserFetches(t *testing.T) {
	cases := []struct {
		name string
		in   Branding
		want []string
	}{
		{name: "nothing declared", in: Branding{}, want: nil},
		{name: "same-site paths", in: Branding{LogoURL: "/static/logo.svg", FaviconURL: "/favicon.ico"}, want: nil},
		{name: "an absolute logo",
			in:   Branding{LogoURL: "https://cdn.example.test/brand/logo.svg?v=2"},
			want: []string{"https://cdn.example.test"}},
		{name: "a port and a case the policy keeps",
			in:   Branding{FaviconURL: "HTTP://CDN.Example.Test:8443/f.ico"},
			want: []string{"http://cdn.example.test:8443"}},
		{name: "one origin for both, once",
			in:   Branding{LogoURL: "https://cdn.example.test/a.svg", FaviconURL: "https://cdn.example.test/b.ico"},
			want: []string{"https://cdn.example.test"}},
		{name: "two origins, sorted",
			in:   Branding{LogoURL: "https://static.example.test/a.svg", FaviconURL: "https://cdn.example.test/b.ico"},
			want: []string{"https://cdn.example.test", "https://static.example.test"}},
		{name: "an internationalised host",
			in:   Branding{LogoURL: "https://bücher.example/logo.svg"},
			want: []string{"https://xn--bcher-kva.example"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateBranding(tc.in); err != nil {
				t.Fatalf("the branding does not validate: %v", err)
			}
			if got := tc.in.imageOrigins(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("image origins = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBrandingRefusesAHostThePolicyCannotName: the origin goes into a
// response header. A host with a ";" would end img-src and begin another
// directive, a wildcard would widen it, and an IPv6 literal or a host with
// no name is not something a CSP source can express — the browser would
// refuse each of them anyway, so the panel refuses them at startup.
func TestBrandingRefusesAHostThePolicyCannotName(t *testing.T) {
	for raw, want := range map[string]string{
		"https://cdn.example.test;script-src/logo.svg": "Content-Security-Policy can name",
		"https://*.example.test/logo.svg":              "Content-Security-Policy can name",
		"https://cdn,example.test/logo.svg":            "Content-Security-Policy can name",
		"https://[::1]:8443/logo.svg":                  "IPv6",
		"https:///logo.svg":                            "names no host",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := validateBranding(Branding{LogoURL: raw})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected a refusal explaining %q, got %v", want, err)
			}
			if got := (Branding{LogoURL: raw}).imageOrigins(); got != nil {
				t.Fatalf("a refused URL still reaches the policy: %v", got)
			}
		})
	}
}

// TestCSPLoadsTheBrandingTheConfigurationAccepted is the property the
// branding promises: an absolute logo the panel starts with is one the
// browser is allowed to fetch — on the login page, the first screen that
// shows it, and on every other response — and a panel with no absolute
// branding keeps the policy it always had.
func TestCSPLoadsTheBrandingTheConfigurationAccepted(t *testing.T) {
	imgSrc := func(t *testing.T, header http.Header) string {
		t.Helper()
		for _, directive := range strings.Split(header.Get("Content-Security-Policy"), ";") {
			if d := strings.TrimSpace(directive); strings.HasPrefix(d, "img-src ") {
				return d
			}
		}
		t.Fatalf("no img-src in %q", header.Get("Content-Security-Policy"))
		return ""
	}

	_, _, plain := formsPanel(t, nil)
	resp, err := http.Get(plain.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := imgSrc(t, resp.Header); got != "img-src 'self' data:" {
		t.Fatalf("a panel with no absolute branding widened its policy: %q", got)
	}

	_, _, branded := formsPanel(t, func(c *PanelConfig) {
		c.Branding = Branding{
			LogoURL:    "https://cdn.example.test/brand/logo.svg",
			FaviconURL: "/static/favicon.ico",
		}
	})
	for _, path := range []string{"/login", "/", "/api/models"} {
		resp, err := http.Get(branded.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if got := imgSrc(t, resp.Header); got != "img-src 'self' data: https://cdn.example.test" {
			t.Errorf("%s: %q does not name the logo's origin, and only it", path, got)
		}
	}
}

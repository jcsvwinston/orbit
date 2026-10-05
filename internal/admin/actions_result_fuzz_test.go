// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"net/url"
	"strings"
	"testing"
)

// FuzzPanelRedirect states the property an action's redirect has to keep
// (EXT-03): whatever the panel accepts, a browser that follows it stays on
// the panel's host and under the panel's prefix. It is written from the
// rule, not from panelRedirect's own checks — a property built from the
// checks would bless whatever they missed.
//
// The SPA follows an accepted redirect either through its router, which
// resolves it under the prefix, or by loading prefix+target as a document;
// both are checked, against an origin whose prefix is /admin. Go's URL
// resolver is not a browser's, so the shapes where the two disagree —
// backslashes, control characters, "///host" — are asserted absent
// outright rather than left to the resolver.
func FuzzPanelRedirect(f *testing.F) {
	for _, seed := range []string{
		"/", "/data-studio", "/data-studio?model=Note&record=7", "/x/reports/batch/7#totals",
		"https://evil.example", "//evil.example", "///evil.example", `/\evil.example`, "/\t/evil.example",
		"javascript:alert(1)", "/a/../../b", "/a/%2e%2e/%2e%2e/b", "/a%2f..%2f..%2fb", "/%2F%2Fevil.example",
		"/@evil.example", "/:80", "/?next=https://evil.example", "", " ", "/x\x00y",
	} {
		f.Add(seed)
	}
	base, err := url.Parse("https://panel.example/admin/")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		target, err := panelRedirect(raw)
		if err != nil {
			return
		}
		if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
			t.Fatalf("accepted %q (from %q): not one leading slash", target, raw)
		}
		for _, r := range target {
			if r == '\\' || r < 0x20 || r == 0x7f {
				t.Fatalf("accepted %q (from %q): holds %q, which a browser reads differently", target, raw, r)
			}
		}
		for _, candidate := range []string{target, "/admin" + target} {
			ref, err := url.Parse(candidate)
			if err != nil {
				t.Fatalf("accepted %q, and %q does not parse: %v", target, candidate, err)
			}
			resolved := base.ResolveReference(ref)
			if resolved.Scheme != "https" || resolved.Host != "panel.example" || resolved.User != nil {
				t.Fatalf("accepted %q, and %q leaves the panel: %s", target, candidate, resolved)
			}
		}
		// Under the prefix, nothing climbs out of it. Go's resolver keeps
		// "%2e%2e" as a name; a browser's URL parser reads ".", "..",
		// "%2e", ".%2e", "%2e." and "%2e%2e", in any case, as dot
		// segments. That is the rule asserted here.
		path := strings.SplitN(strings.SplitN(target, "#", 2)[0], "?", 2)[0]
		for _, segment := range strings.Split(path, "/") {
			if dot := strings.ReplaceAll(strings.ToLower(segment), "%2e", "."); dot == "." || dot == ".." {
				t.Fatalf("accepted %q, whose segment %q a browser reads as %q", target, segment, dot)
			}
		}
	})
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"crypto/sha512"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// clientFiles is an application's file system: two declared files, others it
// holds and never declares, and directories.
func clientFiles() fstest.MapFS {
	return fstest.MapFS{
		"orbit/badge.js":   {Data: []byte(`window.orbit.registerFieldRenderer("badge", function (v) { return String(v) })`)},
		"orbit/badge.css":  {Data: []byte(`.badge { border-radius: 9999px }`)},
		"orbit/private.js": {Data: []byte(`// never declared`)},
		"orbit/dir/x.js":   {Data: []byte(`// inside a directory`)},
		// A directory whose name ends like a script.
		"orbit/folder.js/inner.js": {Data: []byte(`// inside a directory`)},
	}
}

func badgeClient() ClientCode {
	return ClientCode{
		Files:          clientFiles(),
		Scripts:        []string{"orbit/badge.js"},
		Stylesheets:    []string{"orbit/badge.css"},
		FieldRenderers: []string{"badge"},
	}
}

// TestClientCodeRefusesWhatItCannotServe: every one of these would be a
// script the document names and the browser never runs, a renderer nothing
// registers, or a path the panel should never open. Each refusal names the
// entry; all of them are reported at once.
func TestClientCodeRefusesWhatItCannotServe(t *testing.T) {
	files := clientFiles()
	cases := []struct {
		name string
		c    ClientCode
		want []string
	}{
		{name: "a file that is not there", c: ClientCode{Files: files, Scripts: []string{"orbit/missing.js"}},
			want: []string{`client.scripts[0] "orbit/missing.js": cannot be read from client.files`}},
		{name: "a path that climbs out", c: ClientCode{Files: files, Scripts: []string{"../secrets.js"}},
			want: []string{`client.scripts[0] "../secrets.js": climbs out of client.files`}},
		{name: "a path that climbs out halfway", c: ClientCode{Files: files, Stylesheets: []string{"orbit/../../x.css"}},
			want: []string{`client.stylesheets[0] "orbit/../../x.css": climbs out of client.files`}},
		{name: "an absolute path", c: ClientCode{Files: files, Scripts: []string{"/etc/passwd.js"}},
			want: []string{`client.scripts[0] "/etc/passwd.js": is absolute`}},
		{name: "a backslash", c: ClientCode{Files: files, Scripts: []string{`orbit\badge.js`}},
			want: []string{`has a backslash`}},
		{name: "a dot segment", c: ClientCode{Files: files, Scripts: []string{"./orbit/badge.js"}},
			want: []string{`has an empty or "." segment`}},
		{name: "a character a URL would have to escape", c: ClientCode{Files: files, Scripts: []string{"orbit/my badge.js"}},
			want: []string{`segment "my badge.js" is not letters`}},
		{name: "a hidden file", c: ClientCode{Files: files, Scripts: []string{"orbit/.env.js"}},
			want: []string{`segment ".env.js"`}},
		{name: "a stylesheet declared as a script", c: ClientCode{Files: files, Scripts: []string{"orbit/badge.css"}},
			want: []string{`client.scripts[0] "orbit/badge.css": does not end in .js`}},
		{name: "a directory", c: ClientCode{Files: files, Scripts: []string{"orbit/folder.js"}},
			want: []string{`client.scripts[0] "orbit/folder.js": cannot be read`}},
		{name: "a file declared twice", c: ClientCode{Files: files, Scripts: []string{"orbit/badge.js", "orbit/badge.js"}},
			want: []string{`client.scripts[1] "orbit/badge.js": is declared twice`}},
		{name: "files to read and nothing to read them from", c: ClientCode{Scripts: []string{"orbit/badge.js"}},
			want: []string{`client.files is nil`}},
		{name: "a renderer name a widget value cannot carry", c: ClientCode{Files: files, Scripts: []string{"orbit/badge.js"}, FieldRenderers: []string{"Badge"}},
			want: []string{`client.field_renderers[0] "Badge": a renderer name is lowercase`}},
		{name: "a renderer named like a widget the panel draws", c: ClientCode{Files: files, Scripts: []string{"orbit/badge.js"}, FieldRenderers: []string{"json"}},
			want: []string{`client.field_renderers[0] "json": is a widget the panel draws itself`}},
		{name: "a renderer declared twice", c: ClientCode{Files: files, Scripts: []string{"orbit/badge.js"}, FieldRenderers: []string{"badge", "badge"}},
			want: []string{`client.field_renderers[1] "badge": is declared twice`}},
		{name: "a renderer no script registers", c: ClientCode{FieldRenderers: []string{"badge"}},
			want: []string{`client.field_renderers declares "badge", and client.scripts declares no script to register it`}},
		{name: "every problem at once", c: ClientCode{Files: files, Scripts: []string{"../a.js", "orbit/missing.js"}, FieldRenderers: []string{"file"}},
			want: []string{`client.scripts[0] "../a.js"`, `; client.scripts[1] "orbit/missing.js"`, `; client.field_renderers[0] "file"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateClientCode(tc.c)
			if err == nil {
				t.Fatalf("%+v was accepted", tc.c)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not say %q: %v", want, err)
				}
			}
		})
	}
}

// TestClientCodeIsReadOnce: what the panel serves is what it read and
// digested at startup, stylesheets ahead of scripts, each in the order
// declared; nothing declared is nothing loaded.
func TestClientCodeIsReadOnce(t *testing.T) {
	if c, err := loadClientCode(ClientCode{Files: clientFiles()}); err != nil || c != nil {
		t.Fatalf("files and nothing declared: %+v, %v", c, err)
	}
	c, err := loadClientCode(badgeClient())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.assets) != 2 || c.assets[0].path != "orbit/badge.css" || c.assets[1].path != "orbit/badge.js" || !c.assets[1].script {
		t.Fatalf("assets %+v", c.assets)
	}
	for _, a := range c.assets {
		sum := sha512.Sum384(clientFiles()[a.path].Data)
		if want := "sha384-" + base64.StdEncoding.EncodeToString(sum[:]); a.integrity != want {
			t.Errorf("%s: integrity %s, the bytes digest to %s", a.path, a.integrity, want)
		}
	}
	if !c.hasRenderer("badge") || c.hasRenderer("money") {
		t.Fatalf("renderers %v", c.renderers)
	}
}

// TestClientCodeOnTheDocument: after the bundle and the panel's stylesheet,
// each with its digest, the script deferred so it runs after the bundle;
// nothing declared leaves the document as it was, byte for byte.
func TestClientCodeOnTheDocument(t *testing.T) {
	shell := []byte(`<!doctype html><html><head><meta charset="UTF-8"><script type="module" crossorigin src="./assets/index-x.js"></script><link rel="stylesheet" crossorigin href="./assets/index-x.css"></head><body><div id="root"></div></body></html>`)
	if got := injectClientCode(shell, nil, "/admin"); string(got) != string(shell) {
		t.Fatalf("nothing declared, and the document changed:\n%s", got)
	}
	c, err := loadClientCode(badgeClient())
	if err != nil {
		t.Fatal(err)
	}
	doc := string(injectClientCode(shell, c, "/ops/"))
	css, js := c.assets[0], c.assets[1]
	link := strings.Index(doc, `<link rel="stylesheet" href="/ops/client/orbit/badge.css?v=`+css.version+`" integrity="`+css.integrity+`">`)
	script := strings.Index(doc, `<script defer src="/ops/client/orbit/badge.js?v=`+js.version+`" integrity="`+js.integrity+`"></script>`)
	bundle := strings.Index(doc, `type="module"`)
	panelCSS := strings.Index(doc, `./assets/index-x.css`)
	head := strings.Index(doc, `</head>`)
	if link < 0 || script < 0 {
		t.Fatalf("link %d, script %d:\n%s", link, script, doc)
	}
	if !(bundle < script && panelCSS < link && link < script && script < head) {
		t.Fatalf("the application's files must come after the panel's, inside <head>:\n%s", doc)
	}
}

// TestClientCodeIsServed: as the kind the declaration said, behind the
// session, cacheable for as long as the version the document names is the
// one served — and only what was declared.
func TestClientCodeIsServed(t *testing.T) {
	_, _, srv := formsPanel(t, func(c *PanelConfig) { c.Client = badgeClient() })
	loaded, err := loadClientCode(badgeClient())
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) (*http.Response, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(body)
	}
	for _, a := range loaded.assets {
		kind := "text/css"
		if a.script {
			kind = "text/javascript"
		}
		for query, cache := range map[string]string{"?v=" + a.version: "immutable", "": "no-cache", "?v=stale": "no-cache"} {
			resp, body := get("/client/" + a.path + query)
			if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), kind) {
				t.Fatalf("%s%s: %d %q", a.path, query, resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if body != string(a.body) {
				t.Fatalf("%s%s: served something other than the file", a.path, query)
			}
			if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, cache) || (cache == "immutable" && !strings.Contains(cc, "private")) {
				t.Errorf("%s%s: Cache-Control %q", a.path, query, cc)
			}
			if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s: no nosniff", a.path)
			}
		}
	}
	for _, path := range []string{"/client/orbit/private.js", "/client/orbit/dir/x.js", "/client/orbit/missing.js"} {
		if resp, _ := get(path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404 — the panel serves what was declared, not the application's file system", path, resp.StatusCode)
		}
	}
}

// TestClientCodeNeedsASession: an operator who is not signed in is not
// served the application's code, any more than the document naming it.
func TestClientCodeNeedsASession(t *testing.T) {
	_, _, srv := formsPanel(t, func(c *PanelConfig) {
		c.Client = badgeClient()
		c.Auth = &testAdminAuth{}
	})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/client/orbit/badge.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK || strings.Contains(string(body), "registerFieldRenderer") {
		t.Fatalf("served without a session: %d %s", resp.StatusCode, body)
	}
}

// TestClientCodeLeavesThePolicyAlone: the application's files are 'self',
// so the panel that declares them sends the policy a panel without them
// sends. The login page names none of them: nothing on it draws a record.
// A panel that declares nothing has no route for them either.
func TestClientCodeLeavesThePolicyAlone(t *testing.T) {
	get := func(srv *httptest.Server, path string) (string, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return strings.ReplaceAll(resp.Header.Get("Content-Security-Policy"), strings.TrimPrefix(srv.URL, "http://"), "HOST"), string(body)
	}
	_, _, plain := formsPanel(t, nil)
	_, _, declared := formsPanel(t, func(c *PanelConfig) { c.Client = badgeClient() })
	for _, path := range []string{"/login", "/"} {
		want, _ := get(plain, path)
		got, body := get(declared, path)
		if got != want {
			t.Errorf("%s: the panel with client code sends %q, the plain one %q", path, got, want)
		}
		if !strings.Contains(got, "script-src 'self';") {
			t.Errorf("%s: script-src is not 'self' alone: %q", path, got)
		}
		if path == "/" && !strings.Contains(body, `/admin/client/orbit/badge.js?v=`) {
			t.Errorf("the panel's document does not load the application's script:\n%s", body)
		}
	}
	if _, body := get(plain, "/client/orbit/badge.js"); !strings.Contains(body, `<div id="root">`) {
		t.Errorf("a panel that declared nothing answers its client path with something other than the document it served before: %s", body)
	}
}

// TestFieldRendererOnTheSchema: a field_widgets value naming a declared
// renderer reaches the schema as the field's renderer, next to the panel's
// own widget, which is what the form edits with and what draws the field
// when the renderer fails. A name nobody declared reaches nothing — the
// module refuses it at startup; a panel wired by hand drops it.
func TestFieldRendererOnTheSchema(t *testing.T) {
	field := func(schema map[string]interface{}, column string) map[string]interface{} {
		for _, entry := range schema["fields"].([]interface{}) {
			if f := entry.(map[string]interface{}); f["column"] == column {
				return f
			}
		}
		t.Fatalf("no field %s", column)
		return nil
	}
	_, _, srv := formsPanel(t, func(c *PanelConfig) {
		c.Client = badgeClient()
		c.FieldWidgets["album.title"] = "Badge"
	})
	schema := albumSchema(t, srv)
	title := field(schema, "title")
	if title["renderer"] != "badge" || title["html_type"] != "text" {
		t.Fatalf("Album.title: renderer %v, html_type %v", title["renderer"], title["html_type"])
	}
	if _, ok := field(schema, "notes")["renderer"]; ok {
		t.Fatal("a field with a widget and no renderer publishes a renderer")
	}

	_, _, undeclared := formsPanel(t, func(c *PanelConfig) { c.FieldWidgets["Album.Title"] = "badge" })
	if r, ok := field(albumSchema(t, undeclared), "title")["renderer"]; ok {
		t.Fatalf("a renderer nobody declared reached the schema: %v", r)
	}
}

// TestFieldWidgetsNameOnlyDeclaredRenderers: the startup check accepts a
// renderer the application declared, refuses one it did not — saying which
// it did — and treats a renderer like a widget when two spellings of one
// field disagree.
func TestFieldWidgetsNameOnlyDeclaredRenderers(t *testing.T) {
	renderers := map[string]bool{"badge": true, "money": true}
	if err := validateFieldWidgets(map[string]string{"Album.Notes": "badge", "Album.Title": " Money "}, fieldWidgetModels(), renderers); err != nil {
		t.Fatalf("declared renderers were refused: %v", err)
	}
	err := validateFieldWidgets(map[string]string{"Album.Notes": "colour"}, fieldWidgetModels(), renderers)
	if err == nil || !strings.Contains(err.Error(), `field_widgets["Album.Notes"]: "colour" is not a widget`) || !strings.Contains(err.Error(), "client.field_renderers declares badge, money") {
		t.Fatalf("an undeclared renderer: %v", err)
	}
	err = validateFieldWidgets(map[string]string{"Album.Notes": "badge"}, fieldWidgetModels(), nil)
	if err == nil || !strings.Contains(err.Error(), "client.field_renderers declares none") {
		t.Fatalf("a renderer with none declared: %v", err)
	}
	err = validateFieldWidgets(map[string]string{"Album.Cover": "badge", "Album.cover_key": "image"}, fieldWidgetModels(), renderers)
	if err == nil || !strings.Contains(err.Error(), "both name Album.Cover") {
		t.Fatalf("a renderer and a widget for one field: %v", err)
	}
}

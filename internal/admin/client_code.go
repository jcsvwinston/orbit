// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// The application's own code on the browser side of the panel (EXT-06), and
// the field renderers that code registers (EXT-07).
//
// Until A11 the panel's document loaded one script, its own bundle, and the
// field widgets were the four the bundle ships. An application that wanted a
// status drawn as a badge or an amount in its currency had no way in short of
// forking the bundle — and the panel's Content-Security-Policy (script-src
// 'self') is what makes "paste a <script> tag" not an answer either.
//
// What this adds keeps that policy exactly as it is. The application hands
// the panel files, not markup: the panel reads them once at startup, serves
// them itself under its own prefix — so they are 'self' — and names each one
// on its document with the digest of the bytes it read (Subresource
// Integrity), after its own bundle. Nothing an application declares here is
// written into the document as typed: a path is checked against a strict
// alphabet before it becomes a URL, and the digest is computed.

// ClientCode is the application's own code for the browser side of the
// panel: scripts and stylesheets read from Files, and the names of the field
// renderers those scripts register. Every field is optional; with nothing
// declared, the panel's document is the one it served before.
type ClientCode struct {
	// Files holds the scripts and stylesheets — typically an embed.FS of
	// the application's, or os.DirFS. The panel reads the declared files
	// once, when it mounts, and serves those bytes: a file that changes on
	// disk afterwards is not what the panel serves until it restarts.
	Files fs.FS

	// Scripts are the paths, inside Files, of the JavaScript the panel's
	// document loads after the panel's own bundle, in this order. Each is a
	// classic script, deferred, so it runs once the bundle has set up
	// window.orbit. Inline script stays refused by the panel's policy.
	Scripts []string

	// Stylesheets are the paths, inside Files, of the stylesheets the
	// panel's document links after its own.
	Stylesheets []string

	// FieldRenderers are the names of the field renderers the scripts
	// register (window.orbit.registerFieldRenderer). A field_widgets entry
	// may name one of them instead of a widget the panel ships; the panel
	// then draws that field with it in the list and on the record view, and
	// falls back to its own drawing when the renderer fails.
	FieldRenderers []string
}

// declared reports whether the application declared anything at all.
func (c ClientCode) declared() bool {
	return len(c.Scripts) > 0 || len(c.Stylesheets) > 0 || len(c.FieldRenderers) > 0
}

// clientAsset is one declared file, as the panel serves it.
type clientAsset struct {
	// path is the declared path inside Files, which is also the path the
	// panel serves it under (<prefix>/client/<path>).
	path   string
	script bool
	body   []byte
	// integrity is the Subresource Integrity value the document names it
	// with: the browser runs or applies the file only if what it received
	// has this digest.
	integrity string
	// version changes with the bytes, so the URL the document names can be
	// cached for as long as this process serves it.
	version string
}

// clientCode is the application's client code, checked and read.
type clientCode struct {
	// assets are the stylesheets, then the scripts, each in the order
	// declared — the order the document names them in.
	assets    []clientAsset
	byPath    map[string]int
	renderers map[string]bool
}

func (c *clientCode) asset(path string) (clientAsset, bool) {
	if c == nil {
		return clientAsset{}, false
	}
	i, ok := c.byPath[path]
	if !ok {
		return clientAsset{}, false
	}
	return c.assets[i], true
}

// hasRenderer reports whether the application declared a field renderer by
// that name.
func (c *clientCode) hasRenderer(name string) bool {
	return c != nil && c.renderers[name]
}

// clientPathSegment is one segment of a declared path: letters, digits,
// dots, dashes and underscores, never starting with a dot — which rules out
// "." and ".." as well as hidden files. A path made of these is a URL path
// and an HTML attribute value as it stands.
var clientPathSegment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// clientPathProblem says what keeps raw from being a path inside Files the
// panel can serve as a file of that extension, or "" when nothing does.
func clientPathProblem(raw, ext string) string {
	switch {
	case raw == "":
		return "is empty"
	case strings.Contains(raw, `\`):
		return `has a backslash; a path inside client.files is separated by "/"`
	case strings.HasPrefix(raw, "/"):
		return `is absolute; give the path inside client.files, without a leading "/"`
	}
	for _, segment := range strings.Split(raw, "/") {
		switch {
		case segment == "..":
			return `climbs out of client.files with a ".." segment`
		case segment == "." || segment == "":
			return `has an empty or "." segment; give the path as it is inside client.files`
		case !clientPathSegment.MatchString(segment):
			return fmt.Sprintf("segment %q is not letters, digits, '.', '-' or '_' (and does not start with a dot)", segment)
		}
	}
	if !fs.ValidPath(raw) {
		return "is not a path inside client.files"
	}
	if !strings.HasSuffix(raw, ext) {
		return fmt.Sprintf("does not end in %s", ext)
	}
	return ""
}

// rendererName is what a field renderer may be called: what a field_widgets
// value can carry, and what a script passes to registerFieldRenderer.
var rendererName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// loadClientCode checks the declaration and reads the declared files. It
// reports every problem at once, each naming the entry, in the order the
// declaration lists them. A nil result with a nil error means nothing was
// declared.
func loadClientCode(c ClientCode) (*clientCode, error) {
	if !c.declared() {
		return nil, nil
	}
	out := &clientCode{byPath: map[string]int{}, renderers: map[string]bool{}}
	var problems []string
	if c.Files == nil && len(c.Scripts)+len(c.Stylesheets) > 0 {
		problems = append(problems, fmt.Sprintf("client.files is nil, and client.scripts and client.stylesheets declare %d file(s) to read from it", len(c.Scripts)+len(c.Stylesheets)))
	}
	load := func(list string, paths []string, ext string, script bool) {
		for i, raw := range paths {
			label := fmt.Sprintf("client.%s[%d] %q", list, i, raw)
			if problem := clientPathProblem(raw, ext); problem != "" {
				problems = append(problems, label+": "+problem)
				continue
			}
			if _, dup := out.byPath[raw]; dup {
				problems = append(problems, label+": is declared twice")
				continue
			}
			if c.Files == nil {
				continue
			}
			body, err := fs.ReadFile(c.Files, raw)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: cannot be read from client.files: %v", label, err))
				continue
			}
			sum := sha512.Sum384(body)
			out.byPath[raw] = len(out.assets)
			out.assets = append(out.assets, clientAsset{
				path:      raw,
				script:    script,
				body:      body,
				integrity: "sha384-" + base64.StdEncoding.EncodeToString(sum[:]),
				version:   hex.EncodeToString(sum[:6]),
			})
		}
	}
	load("stylesheets", c.Stylesheets, ".css", false)
	load("scripts", c.Scripts, ".js", true)

	for i, name := range c.FieldRenderers {
		label := fmt.Sprintf("client.field_renderers[%d] %q", i, name)
		switch {
		case !rendererName.MatchString(name):
			problems = append(problems, label+": a renderer name is lowercase letters, digits or dashes, starting with a letter")
		case normalizeWidget(name) != "":
			problems = append(problems, label+": is a widget the panel draws itself (json, richtext, file or image); give the renderer a name of its own")
		case out.renderers[name]:
			problems = append(problems, label+": is declared twice")
		default:
			out.renderers[name] = true
		}
	}
	if len(c.FieldRenderers) > 0 && len(c.Scripts) == 0 {
		problems = append(problems, fmt.Sprintf("client.field_renderers declares %s, and client.scripts declares no script to register it", strings.Join(quoted(c.FieldRenderers), ", ")))
	}

	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "; "))
	}
	return out, nil
}

// ValidateClientCode is loadClientCode for the module wiring, which reports
// the error as a refusal to start.
func ValidateClientCode(c ClientCode) error {
	_, err := loadClientCode(c)
	return err
}

// clientRenderers lists the renderer names a declaration carries, for the
// field_widgets check. It does not validate them: ValidateClientCode does.
func clientRenderers(c ClientCode) map[string]bool {
	out := make(map[string]bool, len(c.FieldRenderers))
	for _, name := range c.FieldRenderers {
		out[name] = true
	}
	return out
}

// clientAssetPath is where the panel serves the application's files,
// relative to its prefix.
const clientAssetPath = "/client/"

func clientAssetURL(prefix string, a clientAsset) string {
	return NormalizePrefix(prefix) + clientAssetPath + a.path + "?v=" + a.version
}

// injectClientCode names the application's stylesheets and scripts on the
// panel's document, at the end of <head>: after the panel's own bundle and
// stylesheet, so a stylesheet of the application's wins a tie with the
// panel's and a script runs after the bundle — the bundle is a module, which
// the browser runs when the document is parsed, and a deferred classic
// script after it in the document runs after it. Each carries the digest of
// what the panel serves, so the browser runs or applies exactly that. With
// nothing declared, the document is unchanged.
func injectClientCode(content []byte, c *clientCode, prefix string) []byte {
	if c == nil || len(c.assets) == 0 {
		return content
	}
	var tags strings.Builder
	for _, a := range c.assets {
		url := html.EscapeString(clientAssetURL(prefix, a))
		if a.script {
			fmt.Fprintf(&tags, `<script defer src="%s" integrity="%s"></script>`, url, a.integrity)
			continue
		}
		fmt.Fprintf(&tags, `<link rel="stylesheet" href="%s" integrity="%s">`, url, a.integrity)
	}
	return injectHeadEnd(content, tags.String())
}

// serveClientAsset answers <prefix>/client/<path> with one declared file:
// the bytes read at startup, as JavaScript or CSS by what the declaration
// said it is, never by guessing. A path nobody declared is a 404 even when
// Files holds it — the panel does not become a file server for whatever the
// application's file system contains. It is mounted behind the panel's
// session: the document that names these files is served to signed-in
// operators only, and so are they.
func (p *Panel) serveClientAsset(c *router.Context) error {
	w, r := c.Writer, c.Request
	asset, ok := p.client.asset(c.Param("path"))
	if !ok {
		http.NotFound(w, r)
		return nil
	}
	if asset.script {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	if r.URL.Query().Get("v") == asset.version {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write(asset.body)
	return nil
}

// mountClientRoutes serves the application's files, when it declared any.
// With none, there is no route: <prefix>/client/… stays what it was.
func (p *Panel) mountClientRoutes(m *router.Mux) {
	if p.client == nil || len(p.client.assets) == 0 {
		return
	}
	m.Get(clientAssetPath+"{path...}", p.serveClientAsset)
}

// fieldRenderer is the application's renderer a field is drawn with, or ""
// for none: the field_widgets entry that names the field — through the same
// spellings declaredWidget reads, the first that matches deciding — when its
// value is a renderer the application declared.
func (p *Panel) fieldRenderer(modelName string, f datasource.FieldInfo) string {
	if p.client == nil || len(p.client.renderers) == 0 || len(p.config.FieldWidgets) == 0 {
		return ""
	}
	for _, key := range fieldWidgetKeys(modelName, f) {
		for configured, value := range p.config.FieldWidgets {
			if !strings.EqualFold(configured, key) {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(value))
			if p.client.hasRenderer(name) {
				return name
			}
			return ""
		}
	}
	return ""
}

func quoted(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = fmt.Sprintf("%q", name)
	}
	return out
}

// sortedRenderers lists the declared renderer names, for messages.
func sortedRenderers(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

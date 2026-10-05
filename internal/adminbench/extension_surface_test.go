// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	"github.com/jcsvwinston/orbit"
)

// The instruments the extension family (EXT-xx, A11) needs and the rest of
// the bench did not: a reading of the WHOLE mount surface, nested structs
// included; a boot that can be refused without failing the probe; and a
// reader for the Content-Security-Policy the panel sends.
//
// The surface readers decide nothing on their own. A probe uses them to
// notice that a surface APPEARED — a knob, a field on a contract type, a key
// in a payload — and answers partial when it does, so the suite goes red and
// the probe is grown into the behaviour check the new surface makes
// possible. An absence is never decided by them alone: every absent verdict
// in the family also reads what the panel serves.

// knob is one thing an application can set on the mounted panel.
type knob struct {
	// path is the dotted koanf key ("branding.primary_color"), or, for a
	// Go-only field (koanf:"-"), its Go name.
	path string
	// bindable is false for Go-only wiring, which nucleus.yml cannot set.
	bindable bool
	// index is the field path from orbit.Config, for setting it.
	index []int
	typ   reflect.Type
}

// mountKnobs walks orbit.Config and the plain structs it nests (Branding),
// which is the whole of what an application configures the panel with. The
// freeze test in contracts/ makes Config that surface; this is a reading of
// it, not a guess at names.
func mountKnobs() []knob {
	var out []knob
	var walk func(typ reflect.Type, prefix string, index []int)
	walk = func(typ reflect.Type, prefix string, index []int) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.PkgPath != "" {
				continue
			}
			idx := append(append([]int{}, index...), i)
			tag := strings.Split(f.Tag.Get("koanf"), ",")[0]
			if tag == "-" {
				out = append(out, knob{path: f.Name, bindable: false, index: idx, typ: f.Type})
				continue
			}
			if tag == "" {
				tag = strings.ToLower(f.Name)
			}
			path := tag
			if prefix != "" {
				path = prefix + "." + tag
			}
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, path, idx)
				continue
			}
			out = append(out, knob{path: path, bindable: true, index: idx, typ: f.Type})
		}
	}
	walk(reflect.TypeOf(orbit.Config{}), "", nil)
	return out
}

// knobsNamed returns the knobs whose path contains any fragment, compared
// case-insensitively.
func knobsNamed(fragments ...string) []knob {
	var hits []knob
	for _, k := range mountKnobs() {
		name := strings.ToLower(k.path)
		for _, fragment := range fragments {
			if strings.Contains(name, fragment) {
				hits = append(hits, k)
				break
			}
		}
	}
	return hits
}

func knobPaths(ks []knob) []string {
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, k.path)
	}
	return out
}

// setKnob writes a string into the knob at that path of cfg, and reports
// whether the knob was a string it could write.
func setKnob(cfg *orbit.Config, k knob, value string) bool {
	v := reflect.ValueOf(cfg).Elem().FieldByIndex(k.index)
	if v.Kind() != reflect.String || !v.CanSet() {
		return false
	}
	v.SetString(value)
	return true
}

// fieldsBeyond lists the exported fields of a contract type that are not in
// the set the bench knows, filtered to the ones whose name contains any
// fragment. A new field on ModelAction, ActionResult, Widget or Page is how
// most of the extension family's capabilities would first appear.
func fieldsBeyond(typ reflect.Type, known []string, fragments ...string) []string {
	knownSet := map[string]bool{}
	for _, k := range known {
		knownSet[k] = true
	}
	var hits []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" || knownSet[f.Name] {
			continue
		}
		name := strings.ToLower(f.Name)
		for _, fragment := range fragments {
			if strings.Contains(name, fragment) {
				hits = append(hits, f.Name)
				break
			}
		}
	}
	return hits
}

// keysBeyond does the same for a decoded payload: keys the bench does not
// know whose name contains any fragment.
func keysBeyond(payload map[string]any, known []string, fragments ...string) []string {
	knownSet := map[string]bool{}
	for _, k := range known {
		knownSet[k] = true
	}
	var hits []string
	for key := range payload {
		if knownSet[key] {
			continue
		}
		name := strings.ToLower(key)
		for _, fragment := range fragments {
			if strings.Contains(name, fragment) {
				hits = append(hits, key)
				break
			}
		}
	}
	sort.Strings(hits)
	return hits
}

// startedApp is an application tryStart booted.
type startedApp struct{ base string }

func (s startedApp) URL(path string) string { return s.base + path }

// tryStart boots an application and returns it, or the error it refused to
// start with.
//
// nucleustest.StartApp fails the test when an application does not start,
// which is right for every probe that needs one and wrong for the probes
// whose question IS whether the panel refuses a declaration. This one runs
// the same loop — a free loopback port, RunContext, /healthz — and hands the
// refusal back as a value.
func tryStart(t *testing.T, a nucleus.App) (startedApp, error) {
	t.Helper()
	a.Config.Host = "127.0.0.1"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	a.Config.Port = ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- nucleus.RunContext(ctx, a) }()

	base := fmt.Sprintf("http://127.0.0.1:%d", a.Config.Port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-done:
			cancel()
			if err == nil {
				err = errors.New("the application returned before serving")
			}
			return startedApp{}, err
		default:
		}
		if resp, err := client.Get(base + "/healthz"); err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("the application neither started nor refused within 15s")
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	return startedApp{base: base}, nil
}

// signInAt signs in to an application tryStart booted.
func signInAt(t *testing.T, app startedApp, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.PostForm(app.URL("/admin/login"),
		url.Values{"username": {username}, "password": {password}})
	if err != nil {
		t.Fatalf("sign in as %q: %v", username, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if !signedIn(resp.StatusCode) {
		t.Fatalf("sign in as %q answered %d", username, resp.StatusCode)
	}
	return client
}

// fetch issues one GET and keeps the headers, which the rest of the bench's
// helpers drop: the CSP a probe reads is a header, not a body.
func fetch(t *testing.T, client *http.Client, target string) (response, http.Header) {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{code: resp.StatusCode, ctype: resp.Header.Get("Content-Type"), body: raw}, resp.Header
}

// fetchPost issues one JSON POST as the superuser and keeps the headers.
func fetchPost(t *testing.T, e *env, path, body string) (response, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.server().URL(path), strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.operator(t).Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{code: resp.StatusCode, ctype: resp.Header.Get("Content-Type"), body: raw}, resp.Header
}

// extensionApp is the shape every second application of the family boots
// from: the bench's own models, and the panel configured by the caller.
func extensionApp(t *testing.T, cfg orbit.Config) nucleus.App {
	t.Helper()
	cfg.Prefix = "/admin"
	cfg.BootstrapUsername = "admin"
	cfg.BootstrapEmail = "admin@example.test"
	cfg.BootstrapPassword = bootstrapPassword
	return nucleus.App{
		Config: benchConfig(t),
		Modules: map[string]nucleus.ModuleSpec{
			"content": contentModule(),
			"orbit":   orbit.Module(cfg),
		},
	}
}

// cspSources returns the sources one directive of a Content-Security-Policy
// lists, falling back to default-src as a browser does.
func cspSources(policy, directive string) []string {
	var fallback []string
	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case directive:
			return fields[1:]
		case "default-src":
			fallback = fields[1:]
		}
	}
	return fallback
}

// cspAllows answers whether a URL the panel's document references would be
// loaded under those sources: 'self' for the panel's own origin, a scheme
// source, a host source with an optional leading wildcard, or '*'. It is
// the subset of CSP source matching the panel's own policy uses, which is
// enough to decide the question for a branding URL.
func cspAllows(sources []string, raw, panelHost string) bool {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		raw = "http://" + panelHost + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	for _, src := range sources {
		s := strings.ToLower(strings.Trim(src, " "))
		switch {
		case s == "'self'":
			if host == strings.ToLower(panelHost) {
				return true
			}
		case s == "*":
			if scheme == "http" || scheme == "https" {
				return true
			}
		case strings.HasSuffix(s, ":") && !strings.Contains(s, "/"):
			if scheme == strings.TrimSuffix(s, ":") {
				return true
			}
		case !strings.HasPrefix(s, "'"):
			srcHost := s
			if i := strings.Index(srcHost, "://"); i >= 0 {
				if srcHost[:i] != scheme {
					continue
				}
				srcHost = srcHost[i+3:]
			}
			srcHost = strings.SplitN(srcHost, "/", 2)[0]
			if strings.HasPrefix(srcHost, "*.") {
				if strings.HasSuffix(host, srcHost[1:]) {
					return true
				}
				continue
			}
			if srcHost == host {
				return true
			}
		}
	}
	return false
}

// metaNames lists the names of the <meta> tags a served document carries.
var metaName = regexp.MustCompile(`<meta[^>]*\bname="([^"]+)"`)

func metaNames(doc string) []string {
	var out []string
	for _, m := range metaName.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1])
	}
	return out
}

// scriptSources lists the src of every <script> a served document loads.
var scriptSrc = regexp.MustCompile(`<script[^>]*\bsrc="([^"]+)"`)

func scriptSources(doc string) []string {
	var out []string
	for _, m := range scriptSrc.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1])
	}
	return out
}

// repoRoot is the root of the orbit module this package sits in.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the bench's own source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// documentedKeys reads the first cell of every table row of the
// configuration reference — the keys it documents, as `code`.
var documentedKey = regexp.MustCompile("^\\|\\s*`([a-z0-9_.]+)`\\s*\\|")

func documentedKeys(t *testing.T, rel string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := documentedKey.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out[m[1]] = true
		}
	}
	return out
}

// ---- the theme and the palette (EXT-09, EXT-10) ---------------------------

// headOf is the document's <head>, where whatever decides the first frame
// has to be: the body is parsed after it, and painted after it.
func headOf(doc string) string {
	if i := strings.Index(doc, "</head>"); i >= 0 {
		return doc[:i]
	}
	return doc
}

var (
	scriptTag = regexp.MustCompile(`<script\b([^>]*)>`)
	srcAttr   = regexp.MustCompile(`\bsrc="([^"]+)"`)
	themeMeta = regexp.MustCompile(`<meta[^>]*\bname="([^"]*(?:theme|color-scheme)[^"]*)"[^>]*\bcontent="([^"]*)"`)
)

// themeHints lists what in a document's head could set the first frame's
// theme: a meta tag naming a theme, a dark class on the root, or a script
// that is not the bundle. The panel without a configured theme carries none.
func themeHints(doc string) []string {
	head := headOf(doc)
	var hints []string
	for _, m := range themeMeta.FindAllStringSubmatch(head, -1) {
		hints = append(hints, "meta "+m[1])
	}
	if strings.Contains(doc, `<html class="dark"`) {
		hints = append(hints, "a dark class on <html>")
	}
	for _, m := range scriptTag.FindAllStringSubmatch(head, -1) {
		if !strings.Contains(m[1], `type="module"`) {
			hints = append(hints, "a classic script "+strings.TrimSpace(m[1]))
		}
	}
	return hints
}

// firstFrameProblem says what in a served document keeps the configured
// theme from deciding the first frame, or "" when nothing does: the value
// travels in the head, and a script that can apply it runs before the bundle
// and before the body exists — a classic script (not a module, not async,
// not deferred, so the parser stops for it), placed after the value and
// ahead of the bundle, from the panel's own origin, under a script-src that
// is still 'self' alone, and served as JavaScript.
func firstFrameProblem(t *testing.T, client *http.Client, app startedApp, doc string, header http.Header, want string) string {
	t.Helper()
	head := headOf(doc)
	meta := themeMeta.FindStringSubmatchIndex(head)
	if meta == nil {
		return "the document carries no theme"
	}
	if value := head[meta[4]:meta[5]]; value != want {
		return fmt.Sprintf("the document carries the theme %q, the configuration %q", value, want)
	}
	bundle, classic, classicSrc := -1, -1, ""
	for _, m := range scriptTag.FindAllStringSubmatchIndex(head, -1) {
		attrs := head[m[2]:m[3]]
		src := srcAttr.FindStringSubmatch(attrs)
		switch {
		case strings.Contains(attrs, `type="module"`):
			if bundle < 0 {
				bundle = m[0]
			}
		case src == nil:
			return "an inline script, which script-src 'self' refuses to run"
		case strings.Contains(attrs, "async") || strings.Contains(attrs, "defer"):
			return fmt.Sprintf("the script %s does not stop the parser, so the browser may paint before it runs", src[1])
		case classic < 0:
			classic, classicSrc = m[0], src[1]
		}
	}
	switch {
	case classic < 0:
		return "nothing ahead of the bundle can apply it: the head loads no classic script"
	case bundle >= 0 && classic > bundle:
		return fmt.Sprintf("the script %s comes after the bundle", classicSrc)
	case classic < meta[0]:
		return fmt.Sprintf("the script %s comes before the value it would read", classicSrc)
	}
	policy := header.Get("Content-Security-Policy")
	scriptSources := cspSources(policy, "script-src")
	if len(scriptSources) != 1 || scriptSources[0] != "'self'" {
		return fmt.Sprintf("script-src is %v, not 'self' alone", scriptSources)
	}
	host := mustHost(t, app.URL(""))
	if !cspAllows(scriptSources, classicSrc, host) {
		return fmt.Sprintf("script-src %v refuses %s", scriptSources, classicSrc)
	}
	target := classicSrc
	if strings.HasPrefix(target, "/") {
		target = app.URL(target)
	}
	script, scriptHeader := fetch(t, client, target)
	if script.code != http.StatusOK || !strings.Contains(scriptHeader.Get("Content-Type"), "javascript") {
		return fmt.Sprintf("%s answers %d %q, which the browser will not run under nosniff", classicSrc, script.code, scriptHeader.Get("Content-Type"))
	}
	return ""
}

// paletteKnobs finds, among the colour knobs, an accent, a surface and a
// text colour for each theme, and lists what is missing.
func paletteKnobs(colours []knob) (map[string]map[string]knob, []string) {
	roles := map[string][]string{
		"accent":  {"primary", "accent"},
		"surface": {"surface", "background"},
		"text":    {"text", "foreground"},
	}
	out := map[string]map[string]knob{}
	var missing []string
	for _, theme := range []string{"light", "dark"} {
		out[theme] = map[string]knob{}
		for role, fragments := range roles {
			for _, k := range colours {
				path := strings.ToLower(k.path)
				if !strings.Contains(path, theme) {
					continue
				}
				for _, fragment := range fragments {
					if strings.Contains(path, fragment) {
						out[theme][role] = k
					}
				}
			}
			if _, ok := out[theme][role]; !ok {
				missing = append(missing, theme+" "+role)
			}
		}
	}
	sort.Strings(missing)
	return out, missing
}

var (
	styleBlock = regexp.MustCompile(`(?s)<style\b[^>]*>(.*?)</style>`)
	cssRule    = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
)

// paletteOnDocument reads the custom properties the document's own <style>
// elements set, by theme: a rule whose selector names .dark (and not
// :not(.dark)) is the dark theme's, one on :root otherwise is the light
// theme's. Values are the stylesheet's "H S% L%" triples, read as colours.
func paletteOnDocument(doc string) map[string]map[string][3]float64 {
	out := map[string]map[string][3]float64{"light": {}, "dark": {}}
	for _, block := range styleBlock.FindAllStringSubmatch(headOf(doc), -1) {
		for _, rule := range cssRule.FindAllStringSubmatch(block[1], -1) {
			selector := strings.TrimSpace(rule[1])
			theme := ""
			switch {
			case strings.Contains(selector, ".dark") && !strings.Contains(selector, ":not(.dark)"):
				theme = "dark"
			case strings.Contains(selector, ":root"):
				theme = "light"
			default:
				continue
			}
			for _, decl := range strings.Split(rule[2], ";") {
				name, value, ok := strings.Cut(decl, ":")
				if !ok {
					continue
				}
				if c, ok := hslTriple(strings.TrimSpace(value)); ok {
					out[theme][strings.TrimSpace(name)] = c
				}
			}
		}
	}
	return out
}

// hslTriple reads "H S% L%" into channels in [0, 1].
func hslTriple(v string) ([3]float64, bool) {
	parts := strings.Fields(v)
	if len(parts) != 3 {
		return [3]float64{}, false
	}
	var n [3]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
		if err != nil {
			return [3]float64{}, false
		}
		n[i] = f
	}
	h, s, l := n[0], n[1]/100, n[2]/100
	k := func(m float64) float64 { return math.Mod(m+h/30, 12) }
	a := s * math.Min(l, 1-l)
	f := func(m float64) float64 {
		return l - a*math.Max(-1, math.Min(math.Min(k(m)-3, 9-k(m)), 1))
	}
	return [3]float64{f(0), f(8), f(4)}, true
}

func hexRGB(t *testing.T, hex string) [3]float64 {
	t.Helper()
	var out [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("parse %s: %v", hex, err)
		}
		out[i] = float64(v) / 255
	}
	return out
}

// sameColour allows the rounding a triple written to a tenth carries.
func sameColour(a, b [3]float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i])*255 > 1.5 {
			return false
		}
	}
	return true
}

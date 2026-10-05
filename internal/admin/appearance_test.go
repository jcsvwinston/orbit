// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestThemeIsOneOfThree: the theme an application opens the panel in is
// dark, light or the operator's system, spelled in any case; anything else
// refuses to start, naming the key.
func TestThemeIsOneOfThree(t *testing.T) {
	for raw, want := range map[string]string{
		"": "", "dark": ThemeDark, " Light ": ThemeLight, "SYSTEM": ThemeSystem,
	} {
		b, err := validateBranding(Branding{Theme: raw})
		if err != nil {
			t.Fatalf("%q refused: %v", raw, err)
		}
		if b.Theme != want {
			t.Fatalf("%q normalised to %q, want %q", raw, b.Theme, want)
		}
	}
	for _, raw := range []string{"blue", "auto", "dark;", "dark light"} {
		if _, err := validateBranding(Branding{Theme: raw}); err == nil || !strings.Contains(err.Error(), "branding.theme") {
			t.Fatalf("%q: expected a refusal naming branding.theme, got %v", raw, err)
		}
	}
}

// TestPaletteColoursAreHexColours: every per-theme colour is written into
// the page, so one that is not a colour is refused, naming the theme and the
// key — and every one of them at once.
func TestPaletteColoursAreHexColours(t *testing.T) {
	_, err := validateBranding(Branding{
		Light: Palette{SurfaceColor: "red;} body{display:none"},
		Dark:  Palette{TextColor: "white", PrimaryColor: "#12345"},
	})
	if err == nil {
		t.Fatal("colours that are not hex colours were accepted")
	}
	for _, key := range []string{"branding.light.surface_color", "branding.dark.text_color", "branding.dark.primary_color"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the refusal does not name %s: %v", key, err)
		}
	}
}

// TestPaletteIsCheckedPerTheme is the point of a palette per theme: a colour
// is legible or not against the ground of the theme it is drawn in. A dark
// navy accent reads on the light surface and disappears on the dark one.
func TestPaletteIsCheckedPerTheme(t *testing.T) {
	const navy = "#0b1530"

	if _, err := validateBranding(Branding{Light: Palette{PrimaryColor: navy}}); err != nil {
		t.Fatalf("a navy accent in the light theme was refused: %v", err)
	}
	_, err := validateBranding(Branding{Dark: Palette{PrimaryColor: navy}})
	if err == nil {
		t.Fatal("a navy accent in the dark theme was accepted")
	}
	for _, want := range []string{"branding.dark.primary_color", "dark theme", "3.0:1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}

	cases := []struct {
		name string
		in   Branding
		keys []string
	}{
		{"text that does not read on the surface",
			Branding{Light: Palette{TextColor: "#aaaaaa"}}, []string{"branding.light.text_color"}},
		{"a surface the panel's secondary text does not read on",
			Branding{Light: Palette{SurfaceColor: "#cbd5e1"}}, []string{"branding.light.surface_color"}},
		{"a light surface under the dark theme's light text",
			Branding{Dark: Palette{SurfaceColor: "#f1f5f9"}}, []string{"branding.dark.surface_color"}},
		{"an accent no ink reads on",
			Branding{Dark: Palette{PrimaryColor: "#777777"}}, []string{"branding.dark.primary_color"}},
		{"the old accent against a new surface",
			Branding{PrimaryColor: "#0b5fff", Dark: Palette{SurfaceColor: "#1e40af"}},
			[]string{"branding.primary_color", "branding.dark.surface_color"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateBranding(tc.in)
			if err == nil {
				t.Fatal("accepted")
			}
			for _, key := range tc.keys {
				if !strings.Contains(err.Error(), key) {
					t.Errorf("the refusal does not name %s: %v", key, err)
				}
			}
		})
	}

	// A palette that keeps its contrast in both themes starts.
	if _, err := validateBranding(Branding{
		PrimaryColor: "#0b5fff",
		Light:        Palette{SurfaceColor: "#fafafa", TextColor: "#111827"},
		Dark:         Palette{PrimaryColor: "#60a5fa", SurfaceColor: "#111827", TextColor: "#e5e7eb"},
	}); err != nil {
		t.Fatalf("a legible palette was refused: %v", err)
	}
}

// TestTheOldAccentWarnsAndStarts: branding.primary_color was accepted on any
// hex colour before the palette was checked per theme. One that falls short
// in a theme still starts — refusing it would stop an application that
// changed nothing (QADR-0010) — and the warning names the theme and the key
// that fixes it.
func TestTheOldAccentWarnsAndStarts(t *testing.T) {
	b := Branding{PrimaryColor: "#0b1530"}
	if _, err := validateBranding(b); err != nil {
		t.Fatalf("an accent that started before is refused now: %v", err)
	}
	warnings := BrandingWarnings(b)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one for the dark theme", warnings)
	}
	for _, want := range []string{"branding.primary_color", "dark theme", "branding.dark.primary_color"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("the warning does not say %q: %s", want, warnings[0])
		}
	}
	// The per-theme key is the fix, and it silences the warning.
	b.Dark.PrimaryColor = "#60a5fa"
	if w := BrandingWarnings(b); len(w) != 0 {
		t.Fatalf("still warned after the dark theme got its own accent: %v", w)
	}
	// And the accent the bench and the docs use warns about nothing.
	if w := BrandingWarnings(Branding{PrimaryColor: "#0b5fff"}); len(w) != 0 {
		t.Fatalf("a legible accent warned: %v", w)
	}
}

// TestInkIsTheOneThatReads: the text on an accent is whichever of the two
// inks reads better on it. Chosen by lightness, a saturated yellow got white
// text at 1.07:1 — the failure the docs said the panel prevents.
func TestInkIsTheOneThatReads(t *testing.T) {
	for hex, want := range map[string]string{
		"#ffff00": inkOnLight, "#00ffff": inkOnLight, "#00ff00": inkOnLight,
		"#0b5fff": inkOnDark, "#0b1530": inkOnDark, "#b91c1c": inkOnDark,
	} {
		c, _ := parseHex(hex)
		if got := foregroundOn(c); got != want {
			t.Errorf("%s: ink %q, want %q", hex, got, want)
		}
	}
}

// TestHSLTokenRoundTrips: what the palette writes into the stylesheet is the
// colour that was configured, in the stylesheet's own notation.
func TestHSLTokenRoundTrips(t *testing.T) {
	for _, hex := range []string{"#0b5fff", "#111827", "#ffffff", "#000000", "#f0a", "#e5e7eb"} {
		c, ok := parseHex(hex)
		if !ok {
			t.Fatalf("%s did not parse", hex)
		}
		back, ok := parseHSL(c.hslToken())
		if !ok {
			t.Fatalf("%s: token %q did not parse", hex, c.hslToken())
		}
		for i := range c {
			if math.Abs(back[i]-c[i])*255 > 1 {
				t.Fatalf("%s → %q → %s", hex, c.hslToken(), back.hex())
			}
		}
	}
}

// TestThemeDefaultsAreTheStylesheets: the palette is checked against the
// panel's own colours wherever the application leaves one unset, so the
// copy here has to be the stylesheet's. A token edited in
// ui/src/shared/tokens.css and not here would check every palette against a
// ground the panel no longer paints.
func TestThemeDefaultsAreTheStylesheets(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "ui", "src", "shared", "tokens.css"))
	if err != nil {
		t.Fatalf("read the stylesheet's tokens: %v", err)
	}
	blocks := map[string]string{}
	for _, m := range regexp.MustCompile(`(?s)(:root|\.dark)\s*\{(.*?)\}`).FindAllStringSubmatch(string(raw), -1) {
		theme := ThemeLight
		if m[1] == ".dark" {
			theme = ThemeDark
		}
		blocks[theme] = m[2]
	}
	token := func(theme, name string) string {
		m := regexp.MustCompile(`--` + name + `:\s*([^;]+);`).FindStringSubmatch(blocks[theme])
		if m == nil {
			t.Fatalf("tokens.css has no --%s in the %s theme", name, theme)
		}
		return strings.TrimSpace(m[1])
	}
	for theme, want := range panelTokens {
		for name, got := range map[string]string{
			"background": want.surface, "foreground": want.text, "muted-foreground": want.mutedText,
			"primary": want.primary, "primary-foreground": want.primaryText,
		} {
			if css := token(theme, name); css != got {
				t.Errorf("%s --%s: the stylesheet says %q, the check uses %q", theme, name, css, got)
			}
		}
	}

	// The two inks are the bundle's too (src/lib/branding.ts).
	ts, err := os.ReadFile(filepath.Join("..", "..", "ui", "src", "lib", "branding.ts"))
	if err != nil {
		t.Fatalf("read the bundle's branding: %v", err)
	}
	for _, ink := range []string{inkOnDark, inkOnLight} {
		if !strings.Contains(string(ts), "'"+ink+"'") {
			t.Errorf("src/lib/branding.ts does not draw with %q", ink)
		}
	}
}

// TestPaletteCSS: nothing per theme, nothing written — the accent keeps the
// channel it always had. A per-theme palette is written as the stylesheet's
// own custom properties, one rule per theme, with the old accent as the
// accent of a theme that sets none, and never a configured value as typed.
func TestPaletteCSS(t *testing.T) {
	if css := (Branding{PrimaryColor: "#0b5fff", Theme: ThemeDark}).paletteCSS(); css != "" {
		t.Fatalf("no per-theme colour, and a palette was written: %s", css)
	}
	b, err := validateBranding(Branding{
		PrimaryColor: "#0b5fff",
		Dark:         Palette{SurfaceColor: "#111827", TextColor: "#E5E7EB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	css := b.paletteCSS()
	surface, _ := parseHex("#111827")
	accent, _ := parseHex("#0b5fff")
	for _, want := range []string{
		":root.dark{--background:" + surface.hslToken() + ";--card:" + surface.hslToken(),
		":root:not(.dark){--primary:" + accent.hslToken() + ";--primary-foreground:" + inkOnDark + ";--ring:" + accent.hslToken() + "}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("the palette does not carry %q:\n%s", want, css)
		}
	}
	if strings.Contains(strings.ToLower(css), "#") {
		t.Errorf("a configured value reached the stylesheet as typed:\n%s", css)
	}
}

// TestAppearanceOnTheDocument: with nothing configured the document is the
// one the panel served before, byte for byte (QADR-0010). A configured theme
// puts its meta ahead of the script that reads it, both ahead of the bundle,
// and the script comes from the panel's own origin.
func TestAppearanceOnTheDocument(t *testing.T) {
	shell := []byte(`<!doctype html><html lang="en"><head><meta charset="UTF-8"><script type="module" crossorigin src="./assets/index-x.js"></script></head><body><div id="root"></div></body></html>`)

	if got := injectAppearance(shell, Branding{PrimaryColor: "#0b5fff"}, "/admin"); string(got) != string(shell) {
		t.Fatalf("nothing new configured, and the document changed:\n%s", got)
	}

	b, err := validateBranding(Branding{Theme: "Dark", Dark: Palette{SurfaceColor: "#111827"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := string(injectAppearance(shell, b, "/ops/"))
	meta := strings.Index(doc, `<meta name="nucleus-admin-theme" content="dark">`)
	script := strings.Index(doc, `<script src="/ops/theme.js?v=`+themeScriptVersion+`"></script>`)
	style := strings.Index(doc, `<style id="orbit-palette">:root.dark{`)
	bundle := strings.Index(doc, `type="module"`)
	if meta < 0 || script < 0 || style < 0 {
		t.Fatalf("meta %d, script %d, style %d:\n%s", meta, script, style, doc)
	}
	if !(meta < script && script < bundle) {
		t.Fatalf("the meta must precede the script, and the script the bundle:\n%s", doc)
	}
}

// TestThemeScriptIsServed: the script is JavaScript the browser will run
// under nosniff, cacheable for as long as the URL the document names is the
// one that version serves.
func TestThemeScriptIsServed(t *testing.T) {
	_, _, srv := formsPanel(t, nil)
	for query, cache := range map[string]string{
		"?v=" + themeScriptVersion: "immutable",
		"":                         "no-cache",
		"?v=stale":                 "no-cache",
	} {
		resp, err := http.Get(srv.URL + "/theme.js" + query)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
			t.Fatalf("%s: %d %q", query, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		if !strings.Contains(resp.Header.Get("Cache-Control"), cache) {
			t.Errorf("%s: Cache-Control %q, want %s", query, resp.Header.Get("Cache-Control"), cache)
		}
		if string(body) != string(themeScript) {
			t.Fatalf("%s: served something other than the script", query)
		}
	}
}

// TestThemeLeavesTheScriptPolicyAlone: the theme and the palette ride on
// what the policy already allowed — a script from 'self' and an inline
// style — so a panel that configures both sends the policy a panel without
// them sends, on the login page and on the panel's own document.
func TestThemeLeavesTheScriptPolicyAlone(t *testing.T) {
	policy := func(srv *httptest.Server, path string) string {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.Header.Get("Content-Security-Policy")
	}
	_, _, plain := formsPanel(t, nil)
	_, _, themed := formsPanel(t, func(c *PanelConfig) {
		c.Branding = Branding{Theme: ThemeDark, Dark: Palette{SurfaceColor: "#111827"}}
	})
	for _, path := range []string{"/login", "/"} {
		want := strings.ReplaceAll(policy(plain, path), strings.TrimPrefix(plain.URL, "http://"), "HOST")
		got := strings.ReplaceAll(policy(themed, path), strings.TrimPrefix(themed.URL, "http://"), "HOST")
		if got != want {
			t.Errorf("%s: the themed panel sends %q, the plain one %q", path, got, want)
		}
		if !strings.Contains(got, "script-src 'self';") {
			t.Errorf("%s: script-src is not 'self' alone: %q", path, got)
		}
	}
}

// TestLoginPageWearsTheTheme: the login screen is served before there is a
// session, by the auth provider, and it is the first frame most operators
// see. It carries the theme and the palette the panel's document carries.
func TestLoginPageWearsTheTheme(t *testing.T) {
	distDir := t.TempDir()
	shell := `<!doctype html><html lang="en"><head><title>x</title></head><body></body></html>`
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(shell), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(adminUIDirEnv, distDir)

	a := (&DatabaseAdminAuth{prefix: "/admin"}).WithBranding(Branding{Theme: " System ", Light: Palette{SurfaceColor: "#fafafa"}})
	w := httptest.NewRecorder()
	a.renderLoginPage(w, http.StatusOK, "/admin/", "", "")
	body := w.Body.String()
	for _, want := range []string{
		`<meta name="nucleus-admin-theme" content="system">`,
		`<script src="/admin/theme.js?v=` + themeScriptVersion + `"></script>`,
		`<style id="orbit-palette">:root:not(.dark){--background:`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the login page does not carry %s:\n%s", want, body)
		}
	}

	// A branding the panel would refuse is not half-applied to the login
	// screen either.
	a = (&DatabaseAdminAuth{prefix: "/admin"}).WithBranding(Branding{Theme: "blue", LogoURL: "/static/logo.svg"})
	w = httptest.NewRecorder()
	a.renderLoginPage(w, http.StatusOK, "/admin/", "", "")
	if strings.Contains(w.Body.String(), "logo.svg") || strings.Contains(w.Body.String(), "theme.js") {
		t.Errorf("a branding that does not validate reached the login page:\n%s", w.Body.String())
	}
}

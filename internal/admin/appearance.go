// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"html"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// The theme the panel opens in and the palette it wears in each theme
// (EXT-09, EXT-10).
//
// Until A11 the theme belonged to each operator: the first frame followed
// the browser's preference, then the operator's last toggle, and an
// application could not say "this control room opens dark". Its palette was
// one accent colour, applied the same way to both themes — a dark navy brand
// that reads well on white is close to invisible on the dark ground.
//
// Both are configuration, both are decided before the first frame, and
// neither changes anything when it is not set: with no theme and no
// per-theme palette the document is the one the panel served before.

// Themes an application may open the panel in. "system" follows the
// operator's system preference each time the panel opens.
const (
	ThemeDark   = "dark"
	ThemeLight  = "light"
	ThemeSystem = "system"
)

// Palette is the colours of one theme. Every field is optional and is a CSS
// hex colour (#0b5fff or #05f); an empty one keeps the panel's own.
type Palette struct {
	// PrimaryColor is the accent in this theme: the buttons, the active
	// navigation entry and the focus ring. It overrides
	// Branding.PrimaryColor in this theme only.
	PrimaryColor string `yaml:"primary_color" koanf:"primary_color"`
	// SurfaceColor is the ground the panel is drawn on: the page, the
	// cards and the menus.
	SurfaceColor string `yaml:"surface_color" koanf:"surface_color"`
	// TextColor is the text drawn on that ground.
	TextColor string `yaml:"text_color" koanf:"text_color"`
}

func (p Palette) declared() bool {
	return p.PrimaryColor != "" || p.SurfaceColor != "" || p.TextColor != ""
}

func (p Palette) trimmed() Palette {
	return Palette{
		PrimaryColor: strings.TrimSpace(p.PrimaryColor),
		SurfaceColor: strings.TrimSpace(p.SurfaceColor),
		TextColor:    strings.TrimSpace(p.TextColor),
	}
}

// The contrast a palette has to keep, from WCAG 2.2: 4.5:1 for text
// (1.4.3) and 3:1 for what identifies a control — the accent is the focus
// ring and the fill of a button (1.4.11).
const (
	textContrast   = 4.5
	accentContrast = 3.0
)

// The panel's own tokens for each theme, as ui/src/shared/tokens.css writes
// them (H S% L%). The palette is checked against these wherever the
// application leaves a colour unset; TestThemeDefaultsAreTheStylesheets
// keeps the two in step.
type themeTokens struct {
	surface, text, mutedText, primary, primaryText string
}

var panelTokens = map[string]themeTokens{
	ThemeLight: {surface: "0 0% 100%", text: "222.2 84% 4.9%", mutedText: "215.4 16.3% 46.9%", primary: "222.2 47.4% 11.2%", primaryText: "210 40% 98%"},
	ThemeDark:  {surface: "222.2 84% 4.9%", text: "210 40% 98%", mutedText: "215 20.2% 65.1%", primary: "210 40% 98%", primaryText: "222.2 47.4% 11.2%"},
}

// The two texts the panel can draw on an accent: white, or the dark ink of
// its light theme. Whichever reads better on the colour is the one drawn
// (foregroundOn) — the same choice the bundle makes (src/lib/branding.ts).
const (
	inkOnDark  = "0 0% 100%"
	inkOnLight = "222.2 47.4% 11.2%"
)

// rgb is a colour as three channels in [0, 1].
type rgb [3]float64

func parseHex(raw string) (rgb, bool) {
	if !hexColor.MatchString(raw) {
		return rgb{}, false
	}
	digits := raw[1:]
	if len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}
	var out rgb
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(digits[2*i:2*i+2], 16, 8)
		if err != nil {
			return rgb{}, false
		}
		out[i] = float64(v) / 255
	}
	return out, true
}

// parseHSL reads a token written the way the stylesheet writes it: "H S% L%".
func parseHSL(token string) (rgb, bool) {
	parts := strings.Fields(token)
	if len(parts) != 3 {
		return rgb{}, false
	}
	h, err1 := strconv.ParseFloat(parts[0], 64)
	s, err2 := strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
	l, err3 := strconv.ParseFloat(strings.TrimSuffix(parts[2], "%"), 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return rgb{}, false
	}
	s, l = s/100, l/100
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(h/60, 6)
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return rgb{r + m, g + m, b + m}, true
}

func mustHSL(token string) rgb {
	c, ok := parseHSL(token)
	if !ok {
		panic("admin: malformed theme token " + token)
	}
	return c
}

// hslToken writes a colour the way the stylesheet's custom properties are
// written, rounded to a tenth as src/lib/branding.ts rounds it.
func (c rgb) hslToken() string {
	r, g, b := c[0], c[1], c[2]
	maxC := math.Max(r, math.Max(g, b))
	minC := math.Min(r, math.Min(g, b))
	l := (maxC + minC) / 2
	var h, s float64
	if maxC != minC {
		d := maxC - minC
		if l > 0.5 {
			s = d / (2 - maxC - minC)
		} else {
			s = d / (maxC + minC)
		}
		switch maxC {
		case r:
			h = (g - b) / d
			if g < b {
				h += 6
			}
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h /= 6
	}
	round := func(v float64) string {
		return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64)
	}
	return round(h*360) + " " + round(s*100) + "% " + round(l*100) + "%"
}

// hex writes the colour back as #rrggbb, for messages.
func (c rgb) hex() string {
	return fmt.Sprintf("#%02x%02x%02x",
		int(math.Round(c[0]*255)), int(math.Round(c[1]*255)), int(math.Round(c[2]*255)))
}

// luminance is WCAG's relative luminance.
func (c rgb) luminance() float64 {
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

// contrast is WCAG's contrast ratio between two colours, from 1 to 21.
func contrast(a, b rgb) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// foregroundOn picks the text the panel draws on an accent: whichever of its
// two inks reads better on that colour.
func foregroundOn(accent rgb) string {
	if contrast(accent, mustHSL(inkOnLight)) > contrast(accent, mustHSL(inkOnDark)) {
		return inkOnLight
	}
	return inkOnDark
}

// themeColour is one colour of a theme as the panel will paint it: the
// configured value, or the panel's own when nothing was configured, and the
// key that set it ("" for the panel's own).
type themeColour struct {
	key    string
	colour rgb
}

func (c themeColour) describe(theme string) string {
	if c.key == "" {
		return fmt.Sprintf("the %s theme's own %s", theme, c.colour.hex())
	}
	return fmt.Sprintf("%s %s", c.key, c.colour.hex())
}

// themePalette resolves what one theme paints with: the per-theme value,
// then (for the accent) Branding.PrimaryColor, then the panel's own token.
// It assumes the colours have already been checked as hex colours.
func (b Branding) themePalette(theme string) (surface, text, primary themeColour) {
	tokens := panelTokens[theme]
	p := b.palette(theme)
	pick := func(value, key, token string) themeColour {
		if c, ok := parseHex(value); ok {
			return themeColour{key: key, colour: c}
		}
		return themeColour{colour: mustHSL(token)}
	}
	prefix := "branding." + theme + "."
	surface = pick(p.SurfaceColor, prefix+"surface_color", tokens.surface)
	text = pick(p.TextColor, prefix+"text_color", tokens.text)
	primary = pick(p.PrimaryColor, prefix+"primary_color", tokens.primary)
	if primary.key == "" {
		primary = pick(b.PrimaryColor, "branding.primary_color", tokens.primary)
	}
	return surface, text, primary
}

func (b Branding) palette(theme string) Palette {
	if theme == ThemeDark {
		return b.Dark
	}
	return b.Light
}

// paletteFinding is one pair of colours that does not keep the contrast it
// needs in one theme.
type paletteFinding struct {
	theme   string
	keys    []string // the configured keys involved; never empty
	message string
}

// legacyOnly reports whether the only key involved is branding.primary_color,
// the one colour an application could set before the per-theme palette.
func (f paletteFinding) legacyOnly() bool {
	for _, k := range f.keys {
		if k != "branding.primary_color" {
			return false
		}
	}
	return true
}

// paletteFindings checks, for each theme, the colours the panel will paint
// with — the configured ones over its own — and reports every pair that
// involves a configured key and falls short: text on the surface, the
// panel's secondary text on the surface, the accent against the surface,
// and the text the panel draws on the accent.
func (b Branding) paletteFindings() []paletteFinding {
	var out []paletteFinding
	for _, theme := range []string{ThemeLight, ThemeDark} {
		surface, text, primary := b.themePalette(theme)
		muted := themeColour{colour: mustHSL(panelTokens[theme].mutedText)}
		check := func(fg, bg themeColour, need float64, what string) {
			var keys []string
			for _, c := range []themeColour{fg, bg} {
				if c.key != "" {
					keys = append(keys, c.key)
				}
			}
			if len(keys) == 0 {
				return
			}
			ratio := contrast(fg.colour, bg.colour)
			if ratio >= need {
				return
			}
			out = append(out, paletteFinding{
				theme: theme,
				keys:  keys,
				message: fmt.Sprintf("in the %s theme, %s on %s is %.2f:1, below the %.1f:1 %s",
					theme, fg.describe(theme), bg.describe(theme), ratio, need, what),
			})
		}
		check(text, surface, textContrast, "text needs")
		check(muted, surface, textContrast, "the panel's secondary text needs")
		check(primary, surface, accentContrast, "an accent needs to be seen")
		if primary.key != "" {
			ink := themeColour{colour: mustHSL(foregroundOn(primary.colour))}
			ratio := contrast(ink.colour, primary.colour)
			if ratio < textContrast {
				out = append(out, paletteFinding{
					theme: theme,
					keys:  []string{primary.key},
					message: fmt.Sprintf("in the %s theme, neither white nor dark text reads on %s (best %.2f:1, text needs %.1f:1)",
						theme, primary.describe(theme), ratio, textContrast),
				})
			}
		}
	}
	return out
}

// BrandingWarnings lists what the panel will paint and does not keep its
// contrast, but starts anyway: a branding.primary_color that falls short in
// one theme. That key was accepted on any hex colour before the palette was
// checked per theme, and refusing a value that started yesterday would stop
// an application that changed nothing (QADR-0010); the warning names the
// theme and the per-theme key that fixes it. A failure that involves a
// per-theme key refuses to start instead (ValidateBranding).
func BrandingWarnings(b Branding) []string {
	nb, err := validateBranding(b)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range nb.paletteFindings() {
		if f.legacyOnly() {
			out = append(out, fmt.Sprintf("branding.primary_color: %s; set branding.%s.primary_color for that theme", f.message, f.theme))
		}
	}
	return out
}

// validateAppearance checks the theme and the per-theme palette. It runs
// after the colours of validateBranding were trimmed.
func validateAppearance(b Branding) (Branding, error) {
	b.Theme = strings.ToLower(strings.TrimSpace(b.Theme))
	switch b.Theme {
	case "", ThemeDark, ThemeLight, ThemeSystem:
	default:
		return b, fmt.Errorf("branding.theme: %q is not a theme (dark, light or system)", b.Theme)
	}
	b.Light, b.Dark = b.Light.trimmed(), b.Dark.trimmed()
	var problems []string
	for _, theme := range []string{ThemeLight, ThemeDark} {
		p := b.palette(theme)
		for key, value := range map[string]string{
			"primary_color": p.PrimaryColor, "surface_color": p.SurfaceColor, "text_color": p.TextColor,
		} {
			if value != "" && !hexColor.MatchString(value) {
				problems = append(problems, fmt.Sprintf("branding.%s.%s: %q is not a CSS hex colour (#0b5fff or #05f)", theme, key, value))
			}
		}
	}
	if len(problems) == 0 {
		for _, f := range b.paletteFindings() {
			if !f.legacyOnly() {
				problems = append(problems, strings.Join(f.keys, ", ")+": "+f.message)
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return b, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return b, nil
}

// paletteCSS writes the per-theme palette as the custom properties the
// stylesheet reads, one rule per theme, or "" when no per-theme colour was
// configured — in which case Branding.PrimaryColor keeps travelling the way
// it always has (the meta tag the bundle paints from). When it is written,
// it carries Branding.PrimaryColor too, as the accent of a theme that does
// not set its own, and the bundle leaves the accent to it.
//
// The selectors are more specific than the stylesheet's own (:root and
// .dark), so the rules win wherever the document puts them. Every value is a
// number the panel computed from a colour it parsed: nothing configured is
// written into the stylesheet as it was typed.
func (b Branding) paletteCSS() string {
	if !b.Light.declared() && !b.Dark.declared() {
		return ""
	}
	var css strings.Builder
	for _, theme := range []string{ThemeLight, ThemeDark} {
		surface, text, primary := b.themePalette(theme)
		var decls []string
		if surface.key != "" {
			t := surface.colour.hslToken()
			decls = append(decls, "--background:"+t, "--card:"+t, "--popover:"+t)
		}
		if text.key != "" {
			t := text.colour.hslToken()
			decls = append(decls, "--foreground:"+t, "--card-foreground:"+t, "--popover-foreground:"+t)
		}
		if primary.key != "" {
			t := primary.colour.hslToken()
			decls = append(decls, "--primary:"+t, "--primary-foreground:"+foregroundOn(primary.colour), "--ring:"+t)
		}
		if len(decls) == 0 {
			continue
		}
		selector := ":root:not(.dark)"
		if theme == ThemeDark {
			selector = ":root.dark"
		}
		css.WriteString(selector + "{" + strings.Join(decls, ";") + "}")
	}
	return css.String()
}

// themeScript is what applies the configured theme before the first frame:
// a classic script the document loads from the panel's own origin, in
// <head>, ahead of the bundle (first_frame_theme.js says what it decides).
//
//go:embed first_frame_theme.js
var themeScript []byte

// themeScriptVersion changes with the script, so the URL the document names
// can be cached for as long as the binary serves it.
var themeScriptVersion = func() string {
	sum := sha256.Sum256(themeScript)
	return hex.EncodeToString(sum[:6])
}()

func themeScriptURL(prefix string) string {
	return NormalizePrefix(prefix) + "/theme.js?v=" + themeScriptVersion
}

// serveThemeScript answers <prefix>/theme.js. It is served before sign-in,
// like the bundle's own assets: the login screen is the first frame most
// operators see.
func serveThemeScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	if r.URL.Query().Get("v") == themeScriptVersion {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write(themeScript)
}

// injectAppearance puts on the served document what decides the panel's
// first frame: the palette per theme (an inline <style>, which the policy's
// style-src already allows), the configured theme (a meta tag) and the
// script that applies it (from the panel's own origin, so script-src stays
// 'self'). The meta precedes the script, so the script can read it while
// the document is still being parsed. Nothing is injected for what was not
// configured.
func injectAppearance(content []byte, b Branding, prefix string) []byte {
	if b.Theme != "" {
		content = injectHeadFragment(content, `<script src="`+html.EscapeString(themeScriptURL(prefix))+`"></script>`)
		content = injectHeadMeta(content, "nucleus-admin-theme", b.Theme)
	}
	if css := b.paletteCSS(); css != "" {
		content = injectHeadFragment(content, `<style id="orbit-palette">`+css+`</style>`)
	}
	return content
}

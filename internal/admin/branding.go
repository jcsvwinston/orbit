// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/idna"
)

// The panel wearing the product's clothes (CUST-02).
//
// Until now an application could set exactly one thing about how the panel
// looks: its title. Everything else — the logo on the login screen, the
// colour of a button, the icon in the browser tab — was Orbit's, on a screen
// the operator of a product opens every day and that their colleagues
// recognise as part of that product.
//
// Branding is configuration and not code: it is three strings, so unlike the
// actions and pages of ADR-010 it binds from nucleus.yml as well.

// Branding is what an application says about how the panel should look.
// Every field is optional; an empty one keeps Orbit's own.
type Branding struct {
	// LogoURL is the image shown in the sidebar and on the login screen. It
	// is a URL the BROWSER fetches, so it is either absolute (https://…) or
	// a path the application already serves ("/static/logo.svg"); the panel
	// does not become a file server for it. An absolute URL's origin is added
	// to the img-src of the panel's Content-Security-Policy, so the browser
	// loads what the configuration accepted.
	LogoURL string `yaml:"logo_url" koanf:"logo_url"`
	// FaviconURL replaces the icon in the browser tab, same rules.
	FaviconURL string `yaml:"favicon_url" koanf:"favicon_url"`
	// PrimaryColor is the accent colour, as a CSS hex colour (#0b5fff).
	// It lands in a custom property the stylesheet already reads, so it
	// colours the buttons, the active navigation entry and the focus ring
	// together rather than one of them.
	PrimaryColor string `yaml:"primary_color" koanf:"primary_color"`

	// Theme is the theme the panel opens in before an operator has chosen
	// one: "dark", "light", or "system" for the operator's system
	// preference. It decides the first frame — the document applies it
	// before the bundle runs — and an operator who uses the panel's toggle
	// keeps their choice over it. Empty keeps the panel's own behaviour:
	// the operator's last theme, or else the browser's preference
	// (appearance.go).
	Theme string `yaml:"theme" koanf:"theme"`
	// Light and Dark are the palette of each theme: the accent, the
	// surface and the text. Each colour is checked at startup against the
	// ground it is drawn on in THAT theme, and one that falls short of the
	// contrast it needs refuses to start, naming the theme and the key.
	Light Palette `yaml:"light" koanf:"light"`
	Dark  Palette `yaml:"dark" koanf:"dark"`
}

// declared reports whether anything was set at all.
func (b Branding) declared() bool {
	return strings.TrimSpace(b.LogoURL) != "" ||
		strings.TrimSpace(b.FaviconURL) != "" ||
		strings.TrimSpace(b.PrimaryColor) != "" ||
		strings.TrimSpace(b.Theme) != "" ||
		b.Light.trimmed().declared() || b.Dark.trimmed().declared()
}

// hexColor is the whole of what PrimaryColor may be. The value is injected
// into the page, so it is validated rather than escaped: a colour that is not
// a colour is a mistake worth stopping for, and a "colour" that is a
// stylesheet fragment is an injection.
var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// validateBranding refuses a declaration the panel cannot honour, at startup,
// for the same reason the actions of ADR-010 are refused there: a logo that
// silently never appears is worse than an application that will not start
// until the URL is spelled correctly.
func validateBranding(b Branding) (Branding, error) {
	b.LogoURL = strings.TrimSpace(b.LogoURL)
	b.FaviconURL = strings.TrimSpace(b.FaviconURL)
	b.PrimaryColor = strings.TrimSpace(b.PrimaryColor)

	for name, raw := range map[string]string{"logo_url": b.LogoURL, "favicon_url": b.FaviconURL} {
		if raw == "" {
			continue
		}
		if err := validateAssetURL(raw); err != nil {
			return b, fmt.Errorf("branding.%s: %w", name, err)
		}
	}
	if b.PrimaryColor != "" && !hexColor.MatchString(b.PrimaryColor) {
		return b, fmt.Errorf("branding.primary_color: %q is not a CSS hex colour (#0b5fff or #05f)", b.PrimaryColor)
	}
	return validateAppearance(b)
}

// validateAssetURL accepts what a browser may safely be asked to load from
// the panel's own page: a same-site path, or an absolute http(s) URL.
//
// It exists because the value is written into the document. A `javascript:`
// or `data:` URL in the logo would be script execution on every page of the
// panel, granted by a line of YAML — the kind of hole a configuration file
// should not be able to open.
func validateAssetURL(raw string) error {
	_, err := assetOrigin(raw)
	return err
}

// assetOrigin validates raw as validateAssetURL describes and returns the
// origin a browser fetches it from: "" for a same-site path, which the
// panel's own 'self' already covers, and scheme://host[:port] for an
// absolute URL — the source the panel adds to its Content-Security-Policy
// so the image the configuration accepted is one the browser will load.
//
// The host goes into a response header, so it has to be one a CSP source
// can name: letters, digits, hyphens and dots, after an internationalised
// name is converted to its ASCII form. Anything else — an IPv6 literal, a
// wildcard, a host with no name — is refused at startup, because the
// browser would refuse to draw it anyway, and a host that carried a ";"
// would end the img-src directive and begin another.
func assetOrigin(raw string) (string, error) {
	if strings.HasPrefix(raw, "//") {
		return "", fmt.Errorf("%q is protocol-relative; give the scheme or a path", raw)
	}
	if strings.HasPrefix(raw, "/") {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https":
	case "":
		return "", fmt.Errorf("%q is neither an absolute http(s) URL nor a path starting with /", raw)
	default:
		return "", fmt.Errorf("%q uses the %q scheme; only http, https and same-site paths are allowed", raw, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("%q names no host", raw)
	}
	if strings.Contains(host, ":") {
		return "", fmt.Errorf("%q names an IPv6 address, which a Content-Security-Policy source cannot express; use a host name or a same-site path", raw)
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || !cspHost.MatchString(ascii) {
		return "", fmt.Errorf("%q: %q is not a host name the panel's Content-Security-Policy can name (letters, digits, hyphens and dots)", raw, host)
	}
	origin := scheme + "://" + ascii
	if port := parsed.Port(); port != "" {
		origin += ":" + port
	}
	return origin, nil
}

// cspHost is a host-source a Content-Security-Policy can carry, without the
// wildcard the panel never needs: dot-separated labels of letters, digits
// and hyphens, with the trailing dot of a fully qualified name allowed.
var cspHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.?$`)

// imageOrigins lists the origins the branding asks a browser to load images
// from — the logo and the favicon — sorted and without repeats, for the
// panel's img-src. Same-site paths contribute nothing: 'self' covers them.
// The values were validated at startup; one that does not validate here
// contributes nothing either, which is what a panel that refused it would do.
func (b Branding) imageOrigins() []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range []string{strings.TrimSpace(b.LogoURL), strings.TrimSpace(b.FaviconURL)} {
		if raw == "" {
			continue
		}
		origin, err := assetOrigin(raw)
		if err != nil || origin == "" || seen[origin] {
			continue
		}
		seen[origin] = true
		out = append(out, origin)
	}
	sort.Strings(out)
	return out
}

// ValidateBranding is validateBranding for the module wiring, which reports
// the error as a refusal to start.
func ValidateBranding(b Branding) error {
	_, err := validateBranding(b)
	return err
}

// injectBranding puts the declared branding on the served document, through
// the same meta channel as the prefix and the title — which is what makes it
// available on the LOGIN page too, before any API call could carry it. The
// values are escaped by injectHeadMeta and validated at startup.
func injectBranding(content []byte, b Branding) []byte {
	if b.LogoURL != "" {
		content = injectHeadMeta(content, "nucleus-admin-logo", b.LogoURL)
	}
	if b.FaviconURL != "" {
		content = injectHeadMeta(content, "nucleus-admin-favicon", b.FaviconURL)
	}
	if b.PrimaryColor != "" {
		content = injectHeadMeta(content, "nucleus-admin-primary-color", b.PrimaryColor)
	}
	return content
}

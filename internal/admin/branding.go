// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
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
	// does not become a file server for it.
	LogoURL string `yaml:"logo_url" koanf:"logo_url"`
	// FaviconURL replaces the icon in the browser tab, same rules.
	FaviconURL string `yaml:"favicon_url" koanf:"favicon_url"`
	// PrimaryColor is the accent colour, as a CSS hex colour (#0b5fff).
	// It lands in a custom property the stylesheet already reads, so it
	// colours the buttons, the active navigation entry and the focus ring
	// together rather than one of them.
	PrimaryColor string `yaml:"primary_color" koanf:"primary_color"`
}

// declared reports whether anything was set at all.
func (b Branding) declared() bool {
	return strings.TrimSpace(b.LogoURL) != "" ||
		strings.TrimSpace(b.FaviconURL) != "" ||
		strings.TrimSpace(b.PrimaryColor) != ""
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
	return b, nil
}

// validateAssetURL accepts what a browser may safely be asked to load from
// the panel's own page: a same-site path, or an absolute http(s) URL.
//
// It exists because the value is written into the document. A `javascript:`
// or `data:` URL in the logo would be script execution on every page of the
// panel, granted by a line of YAML — the kind of hole a configuration file
// should not be able to open.
func validateAssetURL(raw string) error {
	if strings.HasPrefix(raw, "//") {
		return fmt.Errorf("%q is protocol-relative; give the scheme or a path", raw)
	}
	if strings.HasPrefix(raw, "/") {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	case "":
		return fmt.Errorf("%q is neither an absolute http(s) URL nor a path starting with /", raw)
	default:
		return fmt.Errorf("%q uses the %q scheme; only http, https and same-site paths are allowed", raw, parsed.Scheme)
	}
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

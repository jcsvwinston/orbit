// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// The panel speaking another language (CUST-05).
//
// The interface was English, with no way to say otherwise: an operations
// team that works in Spanish read an English panel over their own Spanish
// product. What makes that fixable rather than endless is deciding what is
// translated and saying so — the panel's own chrome is a closed set of
// phrases, while the DATA is the application's and is never translated by
// anyone but the application.
//
// So: the panel ships catalogues for its own chrome, an application declares
// which locale the panel opens in, and it can add to or override any phrase
// — including for a language the panel does not ship, which is what keeps
// this from being a list of languages we bless.

// defaultLocale is what the panel falls back to, phrase by phrase. An
// untranslated key reads in English rather than as its key: a screen that
// shows "nav.audit" is worse than one that shows "Audit Log".
const defaultLocale = "en"

// localeTag is what a locale may look like: "es", "pt-BR". It is injected
// into the document's lang attribute and used to pick a catalogue, so it is
// validated rather than trusted.
func validLocale(locale string) bool {
	if locale == "" || len(locale) > 12 {
		return false
	}
	for i, part := range strings.Split(locale, "-") {
		if part == "" || len(part) > 8 {
			return false
		}
		for _, r := range part {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9' && i > 0:
			default:
				return false
			}
		}
	}
	return true
}

// normalizeLocale lowercases the language and uppercases the region, so
// "pt-br", "PT-BR" and "pt-BR" pick the same catalogue.
func normalizeLocale(locale string) string {
	parts := strings.Split(strings.TrimSpace(locale), "-")
	parts[0] = strings.ToLower(parts[0])
	if len(parts) > 1 {
		parts[1] = strings.ToUpper(parts[1])
	}
	return strings.Join(parts, "-")
}

// validateLocale refuses a locale the panel cannot honour, at startup.
func validateLocale(locale string) (string, error) {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return defaultLocale, nil
	}
	if !validLocale(locale) {
		return "", fmt.Errorf("locale: %q is not a language tag (en, es, pt-BR)", locale)
	}
	return normalizeLocale(locale), nil
}

// validateMessages checks an application's own catalogues.
func validateMessages(messages map[string]map[string]string) (map[string]map[string]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]string, len(messages))
	for locale, catalogue := range messages {
		trimmed := strings.TrimSpace(locale)
		if !validLocale(trimmed) {
			return nil, fmt.Errorf("messages: %q is not a language tag", locale)
		}
		if len(catalogue) == 0 {
			continue
		}
		out[normalizeLocale(trimmed)] = catalogue
	}
	return out, nil
}

// ValidateLocaleConfig is the module wiring's check for both halves.
func ValidateLocaleConfig(locale string, messages map[string]map[string]string) error {
	if _, err := validateLocale(locale); err != nil {
		return err
	}
	_, err := validateMessages(messages)
	return err
}

// catalogueFor builds the catalogue served for a locale: the panel's English
// underneath, the panel's own translation of that locale over it, and the
// application's on top.
//
// Merging rather than choosing is what makes a partial translation useful:
// an application that only needs its five own words says those five, and
// everything else keeps the panel's. It is also what lets an application
// translate the panel into a language the panel does not ship.
func (p *Panel) catalogueFor(locale string) (string, map[string]string) {
	locale = normalizeLocale(locale)
	if !validLocale(locale) {
		locale = p.locale
	}
	merged := make(map[string]string, len(panelMessages[defaultLocale])+8)
	for key, value := range panelMessages[defaultLocale] {
		merged[key] = value
	}
	if locale != defaultLocale {
		for key, value := range panelMessages[locale] {
			merged[key] = value
		}
	}
	for key, value := range p.messages[locale] {
		merged[key] = value
	}
	return locale, merged
}

// availableLocales lists what the panel can be asked for: what it ships plus
// what the application declared.
func (p *Panel) availableLocales() []string {
	seen := map[string]struct{}{}
	for locale := range panelMessages {
		seen[locale] = struct{}{}
	}
	for locale := range p.messages {
		seen[locale] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for locale := range seen {
		out = append(out, locale)
	}
	sort.Strings(out)
	return out
}

// handleUIMessages serves the catalogue the interface renders with. It is
// unauthenticated-safe in content (panel chrome, no application data), but
// it lives inside the authenticated group like every other API route; the
// login screen gets its phrases through the injected locale and its own
// bundled catalogue.
func (p *Panel) handleUIMessages(c *router.Context) error {
	requested := strings.TrimSpace(c.Query("locale"))
	if requested == "" {
		requested = p.locale
	}
	locale, catalogue := p.catalogueFor(requested)
	return c.JSON(http.StatusOK, map[string]any{
		"locale":    locale,
		"default":   defaultLocale,
		"available": p.availableLocales(),
		"messages":  catalogue,
	})
}

// injectLocale tells the document — and therefore the login screen, before
// any API call — which language it is in. The lang attribute is what a
// screen reader pronounces the page with, so it is set on the element and
// not only in a meta tag.
func injectLocale(content []byte, locale string) []byte {
	if locale == "" {
		locale = defaultLocale
	}
	out := injectHeadMeta(content, "nucleus-admin-locale", locale)
	text := string(out)
	// The built document declares lang="en"; rewriting that one attribute
	// keeps the served page honest about itself.
	if strings.Contains(text, `<html lang="en"`) {
		text = strings.Replace(text, `<html lang="en"`, fmt.Sprintf(`<html lang=%q`, locale), 1)
	}
	return []byte(text)
}

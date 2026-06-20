package admin

import "strings"

const DefaultPrefix = "/admin"

// NormalizePrefix canonicalizes the admin mount path.
func NormalizePrefix(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = DefaultPrefix
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	value = strings.TrimRight(value, "/")
	if value == "" {
		value = DefaultPrefix
	}
	return value
}

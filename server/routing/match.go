package routing

import (
	"path"
	"strings"
)

// methodMatches reports whether method passes an allow-list. Empty
// allow-list = match-everything.
func methodMatches(allow []string, method string) bool {
	if len(allow) == 0 {
		return true
	}
	target := strings.TrimSpace(method)
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), target) {
			return true
		}
	}
	return false
}

// pathMatches reports whether requestPath passes a glob allow-list.
// Patterns ending in "/*" match the prefix; patterns with "*" or "?"
// elsewhere use path.Match; all other patterns are plain prefix or
// exact matches.
func pathMatches(globs []string, requestPath string) bool {
	if len(globs) == 0 {
		return true
	}
	value := strings.TrimSpace(requestPath)
	for _, glob := range globs {
		glob = strings.TrimSpace(glob)
		if glob == "" {
			continue
		}
		if glob == "*" {
			return true
		}
		if strings.HasSuffix(glob, "/*") {
			// The separators are normalised away, so "/api//*" is the
			// subtree "/api/*" and not a glob that matches "/api/" alone and
			// nothing under it — which is what a trailing "//" used to mean,
			// while "//*" already meant "everything". (FuzzEventFilterMatches
			// found the inconsistency, arc A3.)
			prefix := strings.TrimRight(strings.TrimSuffix(glob, "/*"), "/")
			if prefix == "" {
				return true
			}
			if value == prefix || strings.HasPrefix(value, prefix+"/") {
				return true
			}
			continue
		}
		if strings.ContainsAny(glob, "*?") {
			if matched, _ := path.Match(glob, value); matched {
				return true
			}
			continue
		}
		if value == glob || strings.HasPrefix(value, strings.TrimRight(glob, "/")+"/") {
			return true
		}
	}
	return false
}

// statusClassMatches reports whether status passes a leading-digit class
// allow-list, e.g. "5" matches all 5xx, "503" matches both 5xx and the
// exact 503.
//
// An entry has to be a digit string, which is what the proto says the field
// carries ("each entry is a digit string"). It used to be enough for the
// FIRST byte to be a digit, and the exact-status branch then did arithmetic
// on the other two whatever they were: the class "0:0" is three characters,
// so it computed 0*100 + (':'-'0')*10 + 0 = 100 and matched status 100 — a
// class the operator never named, on a filter that arrives from the UI as
// untrusted wire text. (FuzzEventFilterMatches, arc A3.)
func statusClassMatches(classes []string, status int) bool {
	if len(classes) == 0 {
		return true
	}
	for _, c := range classes {
		c = strings.TrimSpace(c)
		if !isDigits(c) {
			continue
		}
		if int(c[0]-'0') == status/100 {
			return true
		}
		if len(c) == 3 {
			exact := int(c[0]-'0')*100 + int(c[1]-'0')*10 + int(c[2]-'0')
			if exact == status {
				return true
			}
		}
	}
	return false
}

// isDigits reports whether s is a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// sqlModelMatches reports whether modelName passes an allow-list. Empty
// allow-list = match-everything.
func sqlModelMatches(allow []string, modelName string) bool {
	if len(allow) == 0 {
		return true
	}
	target := strings.TrimSpace(modelName)
	for _, a := range allow {
		if strings.EqualFold(strings.TrimSpace(a), target) {
			return true
		}
	}
	return false
}

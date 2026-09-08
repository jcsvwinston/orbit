// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package quarkdatasource

import (
	"strings"
	"testing"
)

// FuzzEscapeLike states the property behind F13 (maturity audit 2026-09-03):
// operator-typed search text is data, not a LIKE pattern. applyQuery wraps the
// escaped text in "%…%" and hands it to the engine, so the pattern must match
// a value exactly when that value contains the text literally — never more.
//
// The seed corpus is the table of TestEscapeLike plus the shapes that widened
// a search before the fix: a bare "%", a lone "_", a trailing backslash and
// the bracket class SQL Server reads as a character set.
func FuzzEscapeLike(f *testing.F) {
	seeds := []struct{ needle, haystack string }{
		{"50%_off\\", "50%_off\\ this week"}, // TestEscapeLike, postgres row
		{"a_b", "aXb"},                       // F13: "_" matched any character
		{"50%_[x]", "50%_[x] only"},          // TestEscapeLike, mssql row
		{"%", "anything at all"},
		{"", ""},
		{"\\", "a\\b"},
		{"[", "[bracket]"},
		{"100%", "100% cotton"},
	}
	for _, s := range seeds {
		f.Add(s.needle, s.haystack)
	}

	f.Fuzz(func(t *testing.T, needle, haystack string) {
		for _, dialect := range []string{
			"postgres", "postgresql", "pgx", "mysql", "mariadb",
			"mssql", "sqlserver", "sqlite", "oracle", "",
		} {
			escaped := escapeLike(needle, dialect)
			pattern := "%" + escaped + "%"
			literal := strings.Contains(haystack, needle)

			if likeEscapeStyle(dialect) == "" {
				// SQLite and Oracle have no default escape character, so the
				// text goes through as typed (escapeLike says so): a "%"
				// widens the match. The bound that still holds there is that
				// the pattern never MISSES the literal.
				if escaped != needle {
					t.Fatalf("dialect %q escaped %q to %q, but it has no default escape character",
						dialect, needle, escaped)
				}
				if literal && !likeMatch(pattern, haystack, dialect) {
					t.Fatalf("dialect %q: %q contains %q but pattern %q did not match",
						dialect, haystack, needle, pattern)
				}
				continue
			}

			// Round trip: the escaping is reversible, so it neither drops nor
			// duplicates a byte of what the operator typed.
			if back := unescapeLike(escaped, dialect); back != needle {
				t.Fatalf("dialect %q: unescapeLike(escapeLike(%q)) = %q", dialect, needle, back)
			}

			// The property: the escaped pattern matches exactly the values
			// that contain the text literally.
			if got := likeMatch(pattern, haystack, dialect); got != literal {
				t.Fatalf("dialect %q: pattern %q over %q matched %v, want %v (literal containment of %q)",
					dialect, pattern, haystack, got, literal, needle)
			}
		}
	})
}

// likeEscapeStyle names how a dialect neutralises wildcards, mirroring the
// switch in escapeLike. It is deliberately a second copy: a dialect added
// there without a decision here fails the identity assertion above instead of
// being escaped by a rule no test describes.
func likeEscapeStyle(dialect string) string {
	switch dialect {
	case "postgres", "postgresql", "pgx", "mysql", "mariadb":
		return "backslash"
	case "mssql", "sqlserver":
		return "bracket"
	}
	return ""
}

// unescapeLike is the reader for escapeLike's output: it undoes a backslash
// prefix, or a "[x]" single-character class, and leaves everything else alone.
func unescapeLike(s, dialect string) string {
	var b strings.Builder
	b.Grow(len(s))
	switch likeEscapeStyle(dialect) {
	case "backslash":
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
			}
			b.WriteByte(s[i])
		}
	case "bracket":
		for i := 0; i < len(s); i++ {
			if s[i] == '[' && i+2 < len(s) && s[i+2] == ']' {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			b.WriteByte(s[i])
		}
	default:
		return s
	}
	return b.String()
}

// likeToken is one element of a compiled LIKE pattern.
type likeToken struct {
	kind byte   // '%' any run, '_' any single byte, 'c' literal, 's' character set
	b    byte   // for 'c'
	set  string // for 's'
}

// likeTokens compiles a LIKE pattern over BYTES: "%" any sequence, "_" any
// single byte, a backslash prefix or a "[…]" class a literal. Bytes, not
// runes, because the engine compares the stored value byte for byte and the
// search text is arbitrary input — invalid UTF-8 included.
func likeTokens(pattern, style string) []likeToken {
	toks := make([]likeToken, 0, len(pattern))
	for i := 0; i < len(pattern); i++ {
		switch {
		case pattern[i] == '%':
			toks = append(toks, likeToken{kind: '%'})
		case pattern[i] == '_':
			toks = append(toks, likeToken{kind: '_'})
		case style == "backslash" && pattern[i] == '\\' && i+1 < len(pattern):
			toks = append(toks, likeToken{kind: 'c', b: pattern[i+1]})
			i++
		case style == "bracket" && pattern[i] == '[':
			end := strings.IndexByte(pattern[i:], ']')
			if end < 0 {
				// An unterminated "[" is not a class; SQL Server would refuse
				// the pattern, and escapeLike never emits one.
				toks = append(toks, likeToken{kind: 'c', b: '['})
				continue
			}
			toks = append(toks, likeToken{kind: 's', set: pattern[i+1 : i+end]})
			i += end
		default:
			toks = append(toks, likeToken{kind: 'c', b: pattern[i]})
		}
	}
	return toks
}

// likeMatch evaluates a LIKE pattern against a value. The match is a
// tabulation over (token, position) rather than backtracking, so an adversarial
// pattern of many "%" costs O(len(pattern)·len(value)) instead of exploding.
func likeMatch(pattern, value, dialect string) bool {
	toks := likeTokens(pattern, likeEscapeStyle(dialect))
	prev := make([]bool, len(value)+1)
	cur := make([]bool, len(value)+1)
	prev[0] = true
	for _, tk := range toks {
		for j := range cur {
			cur[j] = false
		}
		switch tk.kind {
		case '%':
			reached := false
			for j := 0; j <= len(value); j++ {
				if prev[j] {
					reached = true
				}
				cur[j] = reached
			}
		case '_':
			for j := 1; j <= len(value); j++ {
				cur[j] = prev[j-1]
			}
		case 'c':
			for j := 1; j <= len(value); j++ {
				cur[j] = prev[j-1] && value[j-1] == tk.b
			}
		case 's':
			for j := 1; j <= len(value); j++ {
				cur[j] = prev[j-1] && strings.IndexByte(tk.set, value[j-1]) >= 0
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(value)]
}

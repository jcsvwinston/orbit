// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package quarkdatasource

import "testing"

// F13 (maturity audit 2026-09-03): operator-typed search text is data, not
// a LIKE pattern.
func TestEscapeLike(t *testing.T) {
	cases := []struct{ dialect, in, want string }{
		{"postgres", "50%_off\\", `50\%\_off\\`},
		{"mysql", "a_b", `a\_b`},
		{"mssql", "50%_[x]", `50[%][_][[]x]`},
		{"sqlite", "50%_off", "50%_off"},
		{"oracle", "a_b", "a_b"},
	}
	for _, tc := range cases {
		if got := escapeLike(tc.in, tc.dialect); got != tc.want {
			t.Errorf("escapeLike(%q, %s) = %q, want %q", tc.in, tc.dialect, got, tc.want)
		}
	}
}

// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package datastudio

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// FuzzParseID drives the agent's record-id narrowing. The id arrives as a
// string on a DataStudioRequest frame (ADR-001 D1: ids are strings at the
// boundary) and parseID narrows it to the primary key's Go kind before it
// reaches the typed CRUD call — so the value the query interpolates has to be
// a number when the key is numeric, never a fragment of what came in.
//
// Properties, per primary-key kind:
//
//   - an error carries no value, and a value carries no error;
//   - a numeric key narrows to a number whose decimal spelling parses back to
//     itself through parseID (round trip);
//   - a textual key narrows to the trimmed id and to nothing else, and an id
//     that is empty once trimmed is refused rather than turned into a
//     "match everything" key.
func FuzzParseID(f *testing.F) {
	for _, seed := range []string{
		"7", " 7 ", "0", "-1", "+7", "007",
		"abc", "0b1c2d3e-0000-4000-8000-000000000001",
		"", "   ",
		"9223372036854775807", "9223372036854775808", // int64 max and past it
		"18446744073709551615", "-0",
		"0x7", "7 OR 1=1", "7; DROP TABLE users", "١٢٣",
	} {
		f.Add(seed)
	}

	kinds := []string{"int", "int64", "uint", "uint64", "string", "uuid.UUID"}

	f.Fuzz(func(t *testing.T, raw string) {
		for _, kind := range kinds {
			meta := &model.ModelMeta{
				Name:       "User",
				Table:      "users",
				PrimaryKey: "ID",
				Fields: []model.FieldMeta{
					{Name: "ID", Column: "id", GoType: kind, IsPK: true},
					{Name: "Email", Column: "email", GoType: "string"},
				},
			}

			got, err := parseID(raw, meta)
			if err != nil {
				if got != nil {
					t.Fatalf("kind %s: parseID(%q) returned %v alongside %v", kind, raw, got, err)
				}
				continue
			}
			if got == nil {
				t.Fatalf("kind %s: parseID(%q) returned no value and no error", kind, raw)
			}
			if strings.TrimSpace(raw) == "" {
				t.Fatalf("kind %s: parseID(%q) accepted an empty id as %v", kind, raw, got)
			}

			switch kind {
			case "int", "int64":
				n, ok := got.(int64)
				if !ok {
					t.Fatalf("kind %s: parseID(%q) returned %T, not int64", kind, raw, got)
				}
				if want, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err != nil || want != n {
					t.Fatalf("kind %s: parseID(%q) = %d, but the id reads as (%d, %v)", kind, raw, n, want, err)
				}
				again, err := parseID(strconv.FormatInt(n, 10), meta)
				if err != nil || again != got {
					t.Fatalf("kind %s: %d does not round trip: (%v, %v)", kind, n, again, err)
				}
			case "uint", "uint64":
				n, ok := got.(uint64)
				if !ok {
					t.Fatalf("kind %s: parseID(%q) returned %T, not uint64", kind, raw, got)
				}
				if want, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err != nil || want != n {
					t.Fatalf("kind %s: parseID(%q) = %d, but the id reads as (%d, %v)", kind, raw, n, want, err)
				}
				again, err := parseID(strconv.FormatUint(n, 10), meta)
				if err != nil || again != got {
					t.Fatalf("kind %s: %d does not round trip: (%v, %v)", kind, n, again, err)
				}
			default:
				// A key of any other kind stays the boundary string, trimmed
				// and nothing else: no case folding, no unquoting, no
				// truncation on a byte the wire happens to carry.
				s, ok := got.(string)
				if !ok {
					t.Fatalf("kind %s: parseID(%q) returned %T, not string", kind, raw, got)
				}
				if s != strings.TrimSpace(raw) {
					t.Fatalf("kind %s: parseID(%q) rewrote the id to %q", kind, raw, s)
				}
			}
		}
	})
}

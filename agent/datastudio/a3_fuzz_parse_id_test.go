// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package datastudio

import (
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// idReading is what a record id means at this boundary, read off the contract
// rather than off the code: ADR-001 D1 says an id arrives as a string, and
// parseID's job is to narrow it to the primary key's Go kind before it reaches
// the typed CRUD call.
//
// So this function is written from the rule, in another vocabulary — math/big
// over the bytes, an explicit sign policy, an explicit width — precisely so it
// is not strconv.ParseInt spelled twice. The rules it states:
//
//   - an id is the trimmed request string, and an id that is empty once
//     trimmed names no row;
//   - a signed key takes an optional '+' or '-' and then one or more ASCII
//     digits, nothing else: no underscores, no 0x, no Unicode digits, no
//     inner space;
//   - an unsigned key takes no sign at all;
//   - the number has to fit the width the key declares — an int8 key holds
//     -128..127, and 300 is not a row of that table;
//   - a key of any other kind is the trimmed string itself, byte for byte.
//
// It returns the value that should reach the query, or nil if the id names no
// row of this model.
func idReading(kind, raw string) any {
	id := strings.TrimSpace(raw)
	if id == "" {
		return nil
	}
	kind = strings.ToLower(kind)

	signed := map[string]bool{"int": true, "int8": true, "int16": true, "int32": true, "int64": true}
	unsigned := map[string]bool{"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true}
	if !signed[kind] && !unsigned[kind] {
		return id
	}

	digits := id
	negative := false
	switch digits[0] {
	case '+':
		if unsigned[kind] {
			return nil // "a sign prefix is not permitted" for an unsigned key
		}
		digits = digits[1:]
	case '-':
		if unsigned[kind] {
			return nil
		}
		negative = true
		digits = digits[1:]
	}
	if digits == "" {
		return nil
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return nil
		}
	}

	n, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil
	}
	if negative {
		n.Neg(n)
	}

	bits := declaredBits(kind)
	if signed[kind] {
		limit := new(big.Int).Lsh(big.NewInt(1), bits-1) // 2^(bits-1)
		if n.Cmp(new(big.Int).Neg(limit)) < 0 || n.Cmp(new(big.Int).Sub(limit, big.NewInt(1))) > 0 {
			return nil
		}
		return n.Int64()
	}
	limit := new(big.Int).Lsh(big.NewInt(1), bits) // 2^bits
	if n.Cmp(new(big.Int).Sub(limit, big.NewInt(1))) > 0 {
		return nil
	}
	return n.Uint64()
}

// declaredBits is the width of a Go integer kind, from the language spec — the
// test keeps its own table so that a change to the one in datastudio.go is a
// disagreement the target reports, not a fact it inherits.
func declaredBits(kind string) uint {
	switch kind {
	case "int8", "uint8":
		return 8
	case "int16", "uint16":
		return 16
	case "int32", "uint32":
		return 32
	case "int64", "uint64":
		return 64
	case "int", "uint":
		// The platform's word size; strconv.IntSize is how Go spells it.
		return uint(strconv.IntSize)
	}
	return 64
}

// decimalSpelling is the canonical decimal form of an id string, computed by
// hand: drop a '+' and the leading zeros, and keep the '-' only in front of a
// non-zero number. The value that reaches the query has to render to exactly
// this, which is how the target says "no byte was invented and none was lost"
// without asking strconv to agree with itself.
func decimalSpelling(id string) string {
	id = strings.TrimSpace(id)
	sign := ""
	if id != "" && (id[0] == '+' || id[0] == '-') {
		if id[0] == '-' {
			sign = "-"
		}
		id = id[1:]
	}
	id = strings.TrimLeft(id, "0")
	if id == "" {
		return "0"
	}
	return sign + id
}

// FuzzParseID drives the agent's record-id narrowing against that reading.
// Both the id and the primary key's declared kind come from the fuzz input:
// the kind is a wire fact too (the model registry the agent serves is the
// host application's, and a legacy schema keys on int32 as readily as on
// uint64), and fixing it to a list would have made the target blind to
// exactly the branch it found.
//
// The property is idReading: for every kind and every id, parseID either
// returns the value that reading names, or refuses. It is not the
// implementation restated — where the two disagreed, the code was wrong: it
// parsed every integer kind at 64 bits, signed and unsigned alike, so an id
// of 300 was accepted for an int8 key and sent to the store as a row it
// cannot hold, against a doc comment that says the id is narrowed to the
// key's Go kind.
//
// Also asserted: an error carries no value and a value carries no error; the
// value reaching the query renders to the canonical decimal spelling of the
// bytes that came in (decimalSpelling), so a numeric id is never invented,
// truncated, or re-based; and the rendering parses back to the same value.
func FuzzParseID(f *testing.F) {
	seeds := []struct{ raw, kind string }{
		{"7", "int64"}, {" 7 ", "int64"}, {"0", "int"}, {"-1", "int32"},
		{"+7", "int64"}, {"+7", "uint64"}, // a sign is a key's business
		{"007", "uint"}, {"-0", "int64"},
		{"abc", "string"}, {"0b1c2d3e-0000-4000-8000-000000000001", "uuid.UUID"},
		{"", "int64"}, {"   ", "string"},
		{"9223372036854775807", "int64"}, {"9223372036854775808", "int64"},
		{"18446744073709551615", "uint64"}, {"18446744073709551616", "uint64"},
		{"300", "int8"},         // fits int64, is not a row of an int8-keyed table
		{"70000", "uint16"},     // the same, unsigned
		{"5000000000", "int32"}, // a legacy 32-bit key
		{"127", "int8"}, {"128", "int8"}, {"-128", "int8"},
		{"0x7", "int64"}, {"7 OR 1=1", "int64"}, {"7; DROP TABLE users", "int64"},
		{"١٢٣", "int64"}, {"7_0", "int64"}, {"1e3", "int64"},
		{"7", "INT64"}, {"7", ""}, {"7", "sql.NullInt64"},
	}
	for _, s := range seeds {
		f.Add(s.raw, s.kind)
	}

	f.Fuzz(func(t *testing.T, raw, kind string) {
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
		want := idReading(kind, raw)

		if err != nil {
			if got != nil {
				t.Fatalf("kind %q: parseID(%q) returned %v alongside %v", kind, raw, got, err)
			}
			if want != nil {
				t.Fatalf("kind %q: parseID(%q) refused (%v) an id that names the row %v (%T)",
					kind, raw, err, want, want)
			}
			return
		}
		if got == nil {
			t.Fatalf("kind %q: parseID(%q) returned no value and no error", kind, raw)
		}
		if want == nil {
			t.Fatalf("kind %q: parseID(%q) = %v (%T), but that id names no row of a %s key",
				kind, raw, got, got, kind)
		}
		if got != want {
			t.Fatalf("kind %q: parseID(%q) = %v (%T); the id reads as %v (%T)",
				kind, raw, got, got, want, want)
		}

		// What reaches the query is the id that came in, in its canonical
		// spelling — no byte invented, none dropped — and it reads back as
		// the same value through parseID itself.
		switch n := got.(type) {
		case int64:
			if s := strconv.FormatInt(n, 10); s != decimalSpelling(raw) {
				t.Fatalf("kind %q: id %q reached the query as %q", kind, raw, s)
			}
			again, err := parseID(strconv.FormatInt(n, 10), meta)
			if err != nil || again != got {
				t.Fatalf("kind %q: %d does not round trip: (%v, %v)", kind, n, again, err)
			}
		case uint64:
			if s := strconv.FormatUint(n, 10); s != decimalSpelling(raw) {
				t.Fatalf("kind %q: id %q reached the query as %q", kind, raw, s)
			}
			again, err := parseID(strconv.FormatUint(n, 10), meta)
			if err != nil || again != got {
				t.Fatalf("kind %q: %d does not round trip: (%v, %v)", kind, n, again, err)
			}
		case string:
			if n != strings.TrimSpace(raw) {
				t.Fatalf("kind %q: parseID(%q) rewrote the id to %q", kind, raw, n)
			}
		default:
			t.Fatalf("kind %q: parseID(%q) returned %T, which is neither a number nor the boundary string",
				kind, raw, got)
		}
	})
}

package nucleus

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/model"
)

// The property the fleet agent's own id narrowing used to carry (A3): a
// boundary id is a string, and narrowing it to the primary key's kind must
// never panic and must accept exactly the canonical unsigned decimals — no
// sign, no underscores, no 0x, no Unicode digits, no inner space. The agent
// serves through this adapter now (ADR-002), so the property lives here.
func FuzzStoreParseID(f *testing.F) {
	for _, seed := range []string{"1", " 42 ", "", "-1", "+7", "0x10", "1_000", "９", "1 2", "18446744073709551615", "18446744073709551616", "abc"} {
		f.Add(seed)
	}
	s := &store{meta: &model.ModelMeta{Name: "Fuzzed"}}
	f.Fuzz(func(t *testing.T, id string) {
		got, err := s.parseID(id)
		trimmed := strings.TrimSpace(id)
		want, perr := strconv.ParseUint(trimmed, 10, 64)
		canonical := perr == nil
		for _, r := range trimmed {
			if r < '0' || r > '9' {
				canonical = false
			}
		}
		switch {
		case canonical && err != nil:
			t.Fatalf("%q is a canonical unsigned decimal and was refused: %v", id, err)
		case !canonical && err == nil:
			t.Fatalf("%q is not a canonical unsigned decimal and was accepted as %d", id, got)
		case canonical && uint64(got) != want:
			t.Fatalf("%q narrowed to %d, want %d", id, got, want)
		}
	})
}

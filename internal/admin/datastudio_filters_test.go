// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"net/url"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

// TestCollectFiltersOperatorForms states what ?field__op=value parses to, and
// what it refuses. The refusals matter as much as the parses: an operator the
// panel does not know must not fall back to equality, and a hidden column must
// not become filterable just because it was named through a different form.
func TestCollectFiltersOperatorForms(t *testing.T) {
	mi := fuzzModelInfo()

	cases := []struct {
		name      string
		query     string
		wantWhere []datasource.Filter
		wantExact map[string]string
		wantErr   bool
	}{
		{
			name:      "a range is two filters on one column",
			query:     "age__gte=18&age__lte=65",
			wantWhere: []datasource.Filter{{Column: "age", Op: datasource.OpGreaterEqual, Value: "18"}, {Column: "age", Op: datasource.OpLessEqual, Value: "65"}},
		},
		{
			name:      "a pattern operator keeps the value as typed",
			query:     "name__contains=%25off",
			wantWhere: []datasource.Filter{{Column: "name", Op: datasource.OpContains, Value: "%off"}},
		},
		{
			name:      "a set is split on commas and trimmed",
			query:     "level__in=1,+2%2C3",
			wantWhere: []datasource.Filter{{Column: "level", Op: datasource.OpIn, Values: []string{"1", "2", "3"}}},
		},
		{
			// An empty in is a question with an answer (no rows). Dropping it
			// would answer every row and look like a filter had run.
			name:      "an empty set survives as an empty set",
			query:     "level__in=",
			wantWhere: []datasource.Filter{{Column: "level", Op: datasource.OpIn, Values: []string{}}},
		},
		{
			name:      "isnull takes a boolean whatever the column's type is",
			query:     "name__isnull=0",
			wantWhere: []datasource.Filter{{Column: "name", Op: datasource.OpIsNull, Value: "false"}},
		},
		{
			// The bool normalisation the equality path does, on the operator
			// path too: what reaches the store is "1"/"0", never "yes".
			name:      "a bool value is normalised",
			query:     "is_active__ne=yes",
			wantWhere: []datasource.Filter{{Column: "is_active", Op: datasource.OpNotEqual, Value: "1"}},
		},
		{
			name:      "equality still lands in Filters, not Where",
			query:     "name=ada",
			wantExact: map[string]string{"name": "ada"},
		},
		{
			name:    "an unknown operator is an unknown field, not equality",
			query:   "age__nope=1",
			wantErr: true,
		},
		{
			name:    "a listable column that is not filterable is refused",
			query:   "visits__gt=1",
			wantErr: true,
		},
		{
			// The excluded column is answered as if it did not exist — the
			// same non-oracle the equality path keeps.
			name:    "an excluded column is refused through the operator form",
			query:   "secret_token__contains=x",
			wantErr: true,
		},
		{
			name:    "a bad boolean is named, not coerced",
			query:   "is_active__eq=perhaps",
			wantErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			values, err := url.ParseQuery(c.query)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}
			filters, where, err := dsCollectFilters(mi, values)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want a refusal, got filters=%v where=%v", filters, where)
				}
				return
			}
			if err != nil {
				t.Fatalf("collect: %v", err)
			}
			if !sameFilters(where, c.wantWhere) {
				t.Fatalf("where = %+v, want %+v", where, c.wantWhere)
			}
			for col, want := range c.wantExact {
				if filters[col] != want {
					t.Fatalf("filters[%q] = %q, want %q", col, filters[col], want)
				}
			}
		})
	}
}

// TestSplitFilterKeyPrefersARealColumn pins the rule that keeps a column whose
// own name ends in something operator-shaped working: the whole key wins.
func TestSplitFilterKeyPrefersARealColumn(t *testing.T) {
	mi := fuzzModelInfo()
	mi.Fields = append(mi.Fields, datasource.FieldInfo{
		Name: "AgeGt", Column: "age__gt", GoType: "int", IsList: true, IsFilter: true,
	})
	if _, _, ok := dsSplitFilterKey(mi, "age__gt"); ok {
		t.Fatalf("age__gt was split into an operator even though it names a column")
	}
	_, where, err := dsCollectFilters(mi, url.Values{"age__gt": {"7"}})
	if err != nil || len(where) != 0 {
		t.Fatalf("collect: where=%+v err=%v, want the column read as equality", where, err)
	}
}

func sameFilters(got, want []datasource.Filter) bool {
	if len(got) != len(want) {
		return false
	}
	used := make([]bool, len(want))
	for _, g := range got {
		matched := false
		for i, w := range want {
			if used[i] || g.Column != w.Column || g.Op != w.Op || g.Value != w.Value {
				continue
			}
			if len(g.Values) != len(w.Values) {
				continue
			}
			same := true
			for j := range g.Values {
				if g.Values[j] != w.Values[j] {
					same = false
					break
				}
			}
			if !same {
				continue
			}
			used[i], matched = true, true
			break
		}
		if !matched {
			return false
		}
	}
	return true
}

// storeWithoutOperators is a RecordStore written before Query.Where existed:
// it compiles against the frozen contract and ignores the field entirely.
type storeWithoutOperators struct {
	datasource.RecordStore
	got datasource.Query
}

func (s *storeWithoutOperators) List(_ context.Context, q datasource.Query) (datasource.Page, error) {
	s.got = q
	return datasource.Page{Items: []datasource.Record{}, Total: -1, IsEstimated: true}, nil
}

// TestHonoursFilterOperators states the promise the panel checks before it
// sends an operator filter. The alternative to checking is the failure this
// whole feature exists to avoid: a store that ignores Where answers every row
// and the screen reads it as a filtered result.
func TestHonoursFilterOperators(t *testing.T) {
	var old datasource.RecordStore = &storeWithoutOperators{}
	if datasource.HonoursFilterOperators(old) {
		t.Fatal("a store that does not implement the interface was reported as honouring operators")
	}
	if datasource.HonoursFilterOperators(nil) {
		t.Fatal("a nil store was reported as honouring operators")
	}
	if !datasource.HonoursFilterOperators(&declaredOperatorStore{}) {
		t.Fatal("a store that declares it does was reported as not")
	}
	if datasource.HonoursFilterOperators(&decliningOperatorStore{}) {
		t.Fatal("a store that DECLINES was reported as honouring operators: returning false has to mean no")
	}
}

type declaredOperatorStore struct{ storeWithoutOperators }

func (*declaredOperatorStore) HonoursFilterOperators() bool { return true }

type decliningOperatorStore struct{ storeWithoutOperators }

func (*decliningOperatorStore) HonoursFilterOperators() bool { return false }

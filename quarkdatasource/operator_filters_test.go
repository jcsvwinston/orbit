// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package quarkdatasource

import (
	"context"
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

// The operator filters over a Quark-backed model (OR-51).
//
// Every case here asserts WHICH ROWS come back, not that the call succeeded:
// a store that dropped a clause would answer every row and still return 200,
// which is the single failure this contract exists to prevent — and the
// reason the panel asks HonoursFilterOperators() before it sends anything.

// filterStore seeds four widgets and returns the store to query them with.
func filterStore(t *testing.T) (datasource.RecordStore, context.Context) {
	t.Helper()
	a, ctx := setup(t)
	store, err := a.Store("QDWidget", "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	for _, w := range []datasource.Record{
		{"name": "alpha", "qty": 1, "active": true},
		{"name": "beta", "qty": 5, "active": true},
		{"name": "gamma", "qty": 10, "active": false},
		{"name": "100% cotton", "qty": 20, "active": false},
	} {
		if _, err := store.Create(ctx, w); err != nil {
			t.Fatalf("seed %v: %v", w, err)
		}
	}
	return store, ctx
}

// names lists the name column of a page, sorted-insensitively by the order
// the store returned, so an assertion reads as the set it is.
func names(t *testing.T, page datasource.Page) []string {
	t.Helper()
	out := make([]string, 0, len(page.Items))
	for _, rec := range page.Items {
		name, _ := rec["name"].(string)
		out = append(out, name)
	}
	return out
}

func listWhere(t *testing.T, store datasource.RecordStore, ctx context.Context, filters ...datasource.Filter) datasource.Page {
	t.Helper()
	page, err := store.List(ctx, datasource.Query{Page: 1, PageSize: 50, Where: filters})
	if err != nil {
		t.Fatalf("List(%v): %v", filters, err)
	}
	return page
}

func TestOperatorFilters_Comparisons(t *testing.T) {
	store, ctx := filterStore(t)

	cases := []struct {
		name   string
		filter datasource.Filter
		want   []string
	}{
		{"eq", datasource.Filter{Column: "qty", Op: datasource.OpEqual, Value: "5"}, []string{"beta"}},
		{"ne", datasource.Filter{Column: "name", Op: datasource.OpNotEqual, Value: "alpha"}, []string{"beta", "gamma", "100% cotton"}},
		{"gt", datasource.Filter{Column: "qty", Op: datasource.OpGreater, Value: "5"}, []string{"gamma", "100% cotton"}},
		{"gte", datasource.Filter{Column: "qty", Op: datasource.OpGreaterEqual, Value: "5"}, []string{"beta", "gamma", "100% cotton"}},
		{"lt", datasource.Filter{Column: "qty", Op: datasource.OpLess, Value: "5"}, []string{"alpha"}},
		{"lte", datasource.Filter{Column: "qty", Op: datasource.OpLessEqual, Value: "5"}, []string{"alpha", "beta"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := names(t, listWhere(t, store, ctx, tc.filter))
			if len(got) != len(tc.want) {
				t.Fatalf("%s answered %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestOperatorFilters_Sets(t *testing.T) {
	store, ctx := filterStore(t)

	in := listWhere(t, store, ctx, datasource.Filter{
		Column: "name", Op: datasource.OpIn, Values: []string{"alpha", "gamma"},
	})
	if got := names(t, in); len(got) != 2 {
		t.Fatalf("in answered %v, want alpha and gamma", got)
	}

	notIn := listWhere(t, store, ctx, datasource.Filter{
		Column: "name", Op: datasource.OpNotIn, Values: []string{"alpha", "gamma"},
	})
	if got := names(t, notIn); len(got) != 2 {
		t.Fatalf("not_in answered %v, want the other two", got)
	}

	// An empty set matches NOTHING. Dropping the clause would answer every
	// row and look like a result — the one way a filter silently becomes no
	// filter.
	empty := listWhere(t, store, ctx, datasource.Filter{Column: "name", Op: datasource.OpIn})
	if got := names(t, empty); len(got) != 0 {
		t.Fatalf("an empty in answered %v, want nothing", got)
	}
	if empty.Total != 0 {
		t.Fatalf("an empty in reports total %d", empty.Total)
	}
}

func TestOperatorFilters_Patterns(t *testing.T) {
	store, ctx := filterStore(t)

	for _, tc := range []struct {
		name   string
		filter datasource.Filter
		want   int
	}{
		{"contains", datasource.Filter{Column: "name", Op: datasource.OpContains, Value: "amm"}, 1},
		{"startswith", datasource.Filter{Column: "name", Op: datasource.OpStartsWith, Value: "al"}, 1},
		{"endswith", datasource.Filter{Column: "name", Op: datasource.OpEndsWith, Value: "ta"}, 1},
		{"no match", datasource.Filter{Column: "name", Op: datasource.OpContains, Value: "zzz"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := names(t, listWhere(t, store, ctx, tc.filter)); len(got) != tc.want {
				t.Fatalf("%s answered %v, want %d row(s)", tc.name, got, tc.want)
			}
		})
	}
}

// TestOperatorFilters_WildcardIsRefusedWhereItWouldWiden is QK-25 seen from
// this side: Quark's builder cannot emit `LIKE … ESCAPE`, and SQLite's LIKE
// has no default escape character, so a value carrying % would match every
// row while looking like a filter. It is refused, by name, rather than
// answered.
func TestOperatorFilters_WildcardIsRefusedWhereItWouldWiden(t *testing.T) {
	store, ctx := filterStore(t)

	_, err := store.List(ctx, datasource.Query{Page: 1, PageSize: 50, Where: []datasource.Filter{
		{Column: "name", Op: datasource.OpContains, Value: "100%"},
	}})
	if err == nil {
		t.Fatal("a % in the value was answered on SQLite: it would have widened the match")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

// TestOperatorFilters_Nullness: isnull is a question about the column, and
// its value is the question and not the operand.
func TestOperatorFilters_Nullness(t *testing.T) {
	store, ctx := filterStore(t)

	present := listWhere(t, store, ctx, datasource.Filter{Column: "name", Op: datasource.OpIsNull, Value: "false"})
	if len(present.Items) != 4 {
		t.Fatalf("isnull=false answered %d rows, want all four", len(present.Items))
	}
	absent := listWhere(t, store, ctx, datasource.Filter{Column: "name", Op: datasource.OpIsNull, Value: "true"})
	if len(absent.Items) != 0 {
		t.Fatalf("isnull=true answered %d rows, want none", len(absent.Items))
	}
}

// TestOperatorFilters_Compose: the operator filters AND with each other and
// with the equality filters, and the total counts the same rows the page
// carries.
func TestOperatorFilters_Compose(t *testing.T) {
	store, ctx := filterStore(t)

	page, err := store.List(ctx, datasource.Query{
		Page: 1, PageSize: 50,
		Filters: map[string]string{"active": "true"},
		Where: []datasource.Filter{
			{Column: "qty", Op: datasource.OpGreaterEqual, Value: "5"},
		},
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := names(t, page); len(got) != 1 || got[0] != "beta" {
		t.Fatalf("composed filters answered %v, want just beta", got)
	}
	if page.Total != 1 {
		t.Fatalf("total = %d, want 1: the count must be over the same filters as the page", page.Total)
	}
}

// TestOperatorFilters_UnknownOperatorIsRefused: falling back to equality
// would answer a different question than the one asked.
func TestOperatorFilters_UnknownOperatorIsRefused(t *testing.T) {
	store, ctx := filterStore(t)

	_, err := store.List(ctx, datasource.Query{Page: 1, PageSize: 50, Where: []datasource.Filter{
		{Column: "name", Op: datasource.FilterOp("regex"), Value: "^a"},
	}})
	if err == nil {
		t.Fatal("an operator this store cannot express was accepted")
	}
}

// TestStoreAnnouncesItHonoursOperators is the contract question the panel
// asks before it sends anything: a store that does not implement this gets
// no operator filters and the request is refused.
func TestStoreAnnouncesItHonoursOperators(t *testing.T) {
	store, _ := filterStore(t)
	if !datasource.HonoursFilterOperators(store) {
		t.Fatal("the Quark-backed store does not announce operator support: the panel would refuse every operator filter")
	}
}

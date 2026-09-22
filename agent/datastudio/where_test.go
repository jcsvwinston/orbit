package datastudio

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func TestWhereFromWire_MapsEveryOperator(t *testing.T) {
	in := []*adminv1.RecordFilter{
		{Column: "Title", Op: "contains", Value: "article"},
		{Column: "Views", Op: "GTE", Value: "10"}, // case-insensitive
		{Column: "Status", Op: "in", Values: []string{"draft", "live"}},
		{Column: "DeletedAt", Op: "isnull", Value: "true"},
	}
	got, err := whereFromWire(in)
	if err != nil {
		t.Fatalf("whereFromWire: %v", err)
	}
	want := []datasource.Filter{
		{Column: "Title", Op: datasource.OpContains, Value: "article"},
		{Column: "Views", Op: datasource.OpGreaterEqual, Value: "10"},
		{Column: "Status", Op: datasource.OpIn, Values: []string{"draft", "live"}},
		{Column: "DeletedAt", Op: datasource.OpIsNull, Value: "true"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d filters, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Column != want[i].Column || got[i].Op != want[i].Op || got[i].Value != want[i].Value || strings.Join(got[i].Values, ",") != strings.Join(want[i].Values, ",") {
			t.Errorf("filter %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWhereFromWire_RefusesRatherThanDrops(t *testing.T) {
	cases := map[string][]*adminv1.RecordFilter{
		"unknown_operator": {{Column: "Title", Op: "like", Value: "x"}},
		"empty_operator":   {{Column: "Title", Value: "x"}},
		"no_column":        {{Op: "eq", Value: "x"}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := whereFromWire(in)
			if err == nil {
				t.Fatalf("accepted %+v as %+v: a filter that cannot be applied must be refused, not dropped", in, got)
			}
			if !strings.Contains(err.Error(), "where[0]") {
				t.Fatalf("the refusal must name the filter: %v", err)
			}
		})
	}
	if got, err := whereFromWire(nil); err != nil || got != nil {
		t.Fatalf("no filters must map to no clause, got %v, %v", got, err)
	}
	if got, err := whereFromWire([]*adminv1.RecordFilter{nil}); err != nil || len(got) != 0 {
		t.Fatalf("a nil entry is skipped, got %v, %v", got, err)
	}
}

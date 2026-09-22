package services

import (
	"encoding/json"
	"strings"
	"testing"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func TestRecordJSON(t *testing.T) {
	if got := recordJSON(nil); got != "" {
		t.Fatalf("no record renders as nothing, got %q", got)
	}
	rec := &adminv1.Record{ValuesJson: map[string]string{
		"Title": `"hello"`, "ID": "7", "Tags": `["a","b"]`, "Odd": `not json`,
	}}
	got := recordJSON(rec)
	want := `{"ID":7,"Odd":"not json","Tags":["a","b"],"Title":"hello"}`
	if got != want {
		t.Fatalf("recordJSON = %s, want %s (sorted keys, fragments as they are, an invalid fragment kept as a string)", got, want)
	}
	if !json.Valid([]byte(got)) {
		t.Fatal("the rendering must be valid JSON")
	}
}

func TestPreviousJSON(t *testing.T) {
	one := &adminv1.Record{ValuesJson: map[string]string{"ID": "1"}}
	two := &adminv1.Record{ValuesJson: map[string]string{"ID": "2"}}
	if got := previousJSON(&adminv1.DataStudioResponse{}); got != "" {
		t.Fatalf("an agent that returns nothing previous leaves the side empty, got %q", got)
	}
	if got := previousJSON(&adminv1.DataStudioResponse{Previous: []*adminv1.Record{one}}); got != `{"ID":1}` {
		t.Fatalf("one previous record is an object, got %s", got)
	}
	bulk := &adminv1.DataStudioResponse{Previous: []*adminv1.Record{one, two},
		Body: &adminv1.DataStudioResponse_BulkAction{BulkAction: &adminv1.BulkActionResponse{Affected: 2}}}
	if got := previousJSON(bulk); got != `[{"ID":1},{"ID":2}]` {
		t.Fatalf("a bulk action's previous records are an array, got %s", got)
	}
	single := &adminv1.DataStudioResponse{Previous: []*adminv1.Record{one},
		Body: &adminv1.DataStudioResponse_BulkAction{BulkAction: &adminv1.BulkActionResponse{Affected: 1}}}
	if got := previousJSON(single); got != `[{"ID":1}]` {
		t.Fatalf("a bulk action of one is still an array, got %s", got)
	}
}

func TestBoundSide(t *testing.T) {
	small := `{"a":1}`
	if got := boundSide(small); got != small {
		t.Fatalf("a side within the bound is kept as is, got %s", got)
	}
	big := `{"blob":"` + strings.Repeat("x", maxAuditSideBytes) + `"}`
	got := boundSide(big)
	var marker struct {
		Truncated bool `json:"truncated"`
		Bytes     int  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(got), &marker); err != nil || !marker.Truncated || marker.Bytes != len(big) {
		t.Fatalf("a side over the bound is replaced by a marker that says so and how large it was, got %s", got)
	}
}

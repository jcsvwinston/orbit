package services

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func TestStartedAt_NilTimestampIsZeroNotEpoch(t *testing.T) {
	if got := startedAt(&adminv1.NodeRegistration{}); !got.IsZero() {
		t.Fatalf("missing started_at must be the zero time, got %v", got)
	}
	want := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	if got := startedAt(&adminv1.NodeRegistration{StartedAt: timestamppb.New(want)}); !got.Equal(want) {
		t.Fatalf("started_at = %v, want %v", got, want)
	}
}

// The agent keys record values by Go field name, so BaseModel's primary
// key arrives as "ID"; an exact "id" lookup left every create audit entry
// without an id.
func TestRecordID_CaseInsensitiveAndPrimaryKey(t *testing.T) {
	rec := &adminv1.Record{ValuesJson: map[string]string{"ID": "42", "Title": `"x"`}}
	if got := recordID(rec, ""); got != "42" {
		t.Fatalf("recordID = %q, want 42", got)
	}
	rec = &adminv1.Record{ValuesJson: map[string]string{"OrderNo": `"A-7"`, "id": "1"}}
	if got := recordID(rec, "OrderNo"); got != "A-7" {
		t.Fatalf("recordID with primary key = %q, want A-7", got)
	}
	if got := recordID(nil, ""); got != "" {
		t.Fatalf("nil record must yield \"\", got %q", got)
	}
}

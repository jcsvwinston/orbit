package store

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func httpEvent(node, path string, at time.Time) *adminv1.Event {
	return &adminv1.Event{Timestamp: timestamppb.New(at), NodeId: node,
		Body: &adminv1.Event_HttpRequest{HttpRequest: &adminv1.HttpRequestEvent{Method: "GET", Path: path, Status: 200}}}
}

func open(t *testing.T, retention time.Duration) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), retention)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_EventsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now()
	for i, p := range []string{"/a", "/b", "/c"} {
		s.AppendEvent(httpEvent("n1", p, now.Add(time.Duration(i)*time.Millisecond)))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	again, err := Open(dir, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	got, err := again.RecentEvents(adminv1.EventType_EVENT_TYPE_HTTP_REQUEST, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].GetHttpRequest().GetPath() != "/b" || got[1].GetHttpRequest().GetPath() != "/c" {
		t.Fatalf("the newest two, oldest first, after a reopen: got %v", paths(got))
	}
	if other, _ := again.RecentEvents(adminv1.EventType_EVENT_TYPE_SQL_STATEMENT, 0); len(other) != 0 {
		t.Fatalf("a kind nothing was written to answers nothing, got %d", len(other))
	}
}

func paths(evs []*adminv1.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.GetHttpRequest().GetPath())
	}
	return out
}

func TestStore_WindowBoundsReadsAndPurge(t *testing.T) {
	s := open(t, time.Minute)
	clock := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	s.AppendAudit(AuditEntry{Actor: "old", Action: "datastudio.create", Target: "A #1"})
	s.AppendHostMetrics("n1", &adminv1.HostMetrics{CpuPercent: 1})
	s.AppendEvent(httpEvent("n1", "/old", clock))
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute) // the window has moved past everything above
	s.AppendAudit(AuditEntry{Actor: "new", Action: "datastudio.delete", Target: "A #1"})
	s.AppendHostMetrics("n1", &adminv1.HostMetrics{CpuPercent: 2})
	s.AppendEvent(httpEvent("n1", "/new", clock))
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	audit, err := s.ListAudit(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Actor != "new" {
		t.Fatalf("a read is bounded by the window before any purge: got %+v", audit)
	}
	hist, err := s.HostMetricsHistory("n1", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Metrics.GetCpuPercent() != 2 {
		t.Fatalf("metrics history is bounded by the window too: got %+v", hist)
	}
	evs, _ := s.RecentEvents(adminv1.EventType_EVENT_TYPE_HTTP_REQUEST, 0)
	if len(evs) != 1 || evs[0].GetHttpRequest().GetPath() != "/new" {
		t.Fatalf("events too: got %v", paths(evs))
	}

	n, err := s.Purge()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("the purge removes the three rows outside the window, removed %d", n)
	}
	if n, _ := s.Purge(); n != 0 {
		t.Fatalf("a second purge has nothing to remove, removed %d", n)
	}
}

func TestStore_NoWindowKeepsEverything(t *testing.T) {
	s := open(t, 0)
	s.AppendAudit(AuditEntry{Time: time.Now().Add(-400 * 24 * time.Hour), Actor: "ancient", Action: "x", Target: "y"})
	_ = s.Flush(context.Background())
	audit, _ := s.ListAudit(0)
	if len(audit) != 1 {
		t.Fatalf("without a window nothing is out of it, got %d", len(audit))
	}
	if n, _ := s.Purge(); n != 0 {
		t.Fatalf("and nothing is purged, removed %d", n)
	}
}

func TestStore_ListAuditNewestFirst(t *testing.T) {
	s := open(t, time.Hour)
	for _, a := range []string{"first", "second", "third"} {
		s.AppendAudit(AuditEntry{Actor: a, Action: "datastudio.create", Target: "T", Before: "", After: `{"a":1}`})
	}
	_ = s.Flush(context.Background())
	got, _ := s.ListAudit(2)
	if len(got) != 2 || got[0].Actor != "third" || got[1].Actor != "second" || got[0].After != `{"a":1}` {
		t.Fatalf("newest first, limited, with both sides: %+v", got)
	}
}

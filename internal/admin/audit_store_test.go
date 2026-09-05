package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func fillAuditStore(s *auditStore, n int) {
	for i := 1; i <= n; i++ {
		action := "create"
		user := "u1"
		if i%3 == 0 {
			action = "update"
		}
		if i%2 == 0 {
			user = "u2"
		}
		s.add(AuditEntry{UserID: user, Action: action, ModelName: "M", RecordID: fmt.Sprint(i)})
	}
}

func TestAuditStore_ListNewestFirstPaginated(t *testing.T) {
	s := newAuditStore(1000)
	fillAuditStore(s, 250)

	// Entries are added faster than the clock ticks, so CreatedAt ties are
	// the norm here; the order must still be newest first, by id.
	var seen []uint
	for page := 1; page <= 3; page++ {
		got := s.list(auditQueryOpts{Page: page, PageSize: 100})
		want := 100
		if page == 3 {
			want = 50
		}
		if len(got) != want {
			t.Fatalf("page %d: %d entries, want %d", page, len(got), want)
		}
		for _, e := range got {
			seen = append(seen, e.ID)
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] != seen[i-1]-1 {
			t.Fatalf("ids not strictly descending at %d: %d after %d", i, seen[i], seen[i-1])
		}
	}
	if seen[0] != 250 || seen[len(seen)-1] != 1 {
		t.Fatalf("pages span %d..%d, want 250..1", seen[0], seen[len(seen)-1])
	}
	if got := s.list(auditQueryOpts{Page: 4, PageSize: 100}); got == nil || len(got) != 0 {
		t.Fatalf("page past the end = %#v, want an empty (non-nil) slice", got)
	}
	if got := s.list(auditQueryOpts{PageSize: 500}); len(got) != 200 {
		t.Fatalf("page_size is capped at 200, got %d", len(got))
	}
}

func TestAuditStore_FiltersAndFilteredCount(t *testing.T) {
	s := newAuditStore(1000)
	fillAuditStore(s, 250)

	cases := []auditQueryOpts{
		{Action: "update"},
		{UserID: "u2"},
		{UserID: "u1", Action: "update"},
		{ModelName: "M", UserID: "u2", Action: "create"},
		{ModelName: "none"},
	}
	for _, opts := range cases {
		total := s.count(opts)
		var collected int
		for page := 1; ; page++ {
			opts.Page, opts.PageSize = page, 40
			got := s.list(opts)
			if len(got) == 0 {
				break
			}
			for _, e := range got {
				if !e.matches(opts) {
					t.Fatalf("%+v: entry %+v does not match the filter", opts, e)
				}
			}
			collected += len(got)
		}
		if collected != total {
			t.Fatalf("%+v: pages hold %d entries, count says %d", opts, collected, total)
		}
	}
	if s.count(auditQueryOpts{}) != 250 {
		t.Fatalf("unfiltered count = %d, want 250", s.count(auditQueryOpts{}))
	}
}

func TestAuditStore_ClearLeavesIdsMonotonic(t *testing.T) {
	s := newAuditStore(10)
	fillAuditStore(s, 7)
	if n := s.clear(); n != 7 {
		t.Fatalf("clear dropped %d, want 7", n)
	}
	if s.count(auditQueryOpts{}) != 0 {
		t.Fatal("store not empty after clear")
	}
	s.add(AuditEntry{Action: "audit.clear"})
	got := s.list(auditQueryOpts{})
	if len(got) != 1 || got[0].ID != 8 {
		t.Fatalf("entry after clear = %+v, want id 8 (ids keep growing)", got)
	}
}

func TestAuditStore_RingTrimsOldest(t *testing.T) {
	s := newAuditStore(5)
	fillAuditStore(s, 8)
	got := s.list(auditQueryOpts{})
	if len(got) != 5 || got[0].ID != 8 || got[4].ID != 4 {
		t.Fatalf("ring = %v, want ids 8..4", got)
	}
}

// TestAuditLog_TotalRespectsFilters pins the HTTP contract: total and
// total_pages count the entries that match the filters, not the whole ring
// (the SPA paginates from total_pages, so it used to page into nothing).
func TestAuditLog_TotalRespectsFilters(t *testing.T) {
	panel, srv := adminTestServer(t)
	fillAuditStore(panel.audit, 30) // 10 updates, 20 creates

	resp, err := http.Get(srv.URL + "/api/audit?action=update&page_size=4")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload struct {
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
		Entries    []struct {
			Action string `json:"action"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 10 || payload.TotalPages != 3 {
		t.Fatalf("total/total_pages = %d/%d, want 10/3 (filtered)", payload.Total, payload.TotalPages)
	}
	if len(payload.Entries) != 4 {
		t.Fatalf("entries = %d, want the page size", len(payload.Entries))
	}
	for _, e := range payload.Entries {
		if e.Action != "update" {
			t.Fatalf("entry %q leaked through the action filter", e.Action)
		}
	}
}

// BenchmarkAuditStoreList measures one page read with a full ring, the
// shape the SPA produces on every filter change.
func BenchmarkAuditStoreList(b *testing.B) {
	s := newAuditStore(10000)
	fillAuditStore(s, 10000)
	opts := auditQueryOpts{Action: "update", Page: 3, PageSize: 50}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := s.list(opts); len(got) != 50 {
			b.Fatalf("page = %d", len(got))
		}
	}
}

// TestAuditStore_AddBoundsStringFields pins that the store, not the caller,
// bounds the string fields of every entry: the identity and request fields
// at auditFieldMaxLen, the User-Agent at auditUserAgentMaxLen, on a rune
// boundary and with the marker; Action and short fields are kept as is.
func TestAuditStore_AddBoundsStringFields(t *testing.T) {
	s := newAuditStore(10)
	long := strings.Repeat("ü", 8*1024) // two bytes per rune
	s.add(AuditEntry{
		UserID: long, Username: long, ModelName: long, RecordID: long, IP: long,
		UserAgent: long, Action: "login.failed",
	})
	got := s.list(auditQueryOpts{})[0]

	for field, value := range map[string]string{
		"user_id": got.UserID, "username": got.Username, "model_name": got.ModelName,
		"record_id": got.RecordID, "ip": got.IP,
	} {
		if len(value) > auditFieldMaxLen+len(auditTruncatedMarker) || !strings.HasSuffix(value, auditTruncatedMarker) {
			t.Fatalf("%s not bounded at %d: len=%d", field, auditFieldMaxLen, len(value))
		}
		if len(value) != auditFieldMaxLen+len(auditTruncatedMarker) {
			t.Fatalf("%s cut off the rune boundary: len=%d", field, len(value)) // 256 is a rune boundary for a 2-byte rune
		}
	}
	if len(got.UserAgent) != auditUserAgentMaxLen+len(auditTruncatedMarker) || !strings.HasSuffix(got.UserAgent, auditTruncatedMarker) {
		t.Fatalf("user_agent not bounded at %d: len=%d", auditUserAgentMaxLen, len(got.UserAgent))
	}
	if got.Action != "login.failed" {
		t.Fatalf("action altered: %q", got.Action)
	}

	s.add(AuditEntry{Username: "root", UserAgent: "curl/8", Action: "logout"})
	short := s.list(auditQueryOpts{})[0]
	if short.Username != "root" || short.UserAgent != "curl/8" {
		t.Fatalf("short fields altered: %+v", short)
	}
}

func TestTruncateAuditString_CutsOnRuneBoundary(t *testing.T) {
	s := strings.Repeat("€", 100) // 3 bytes per rune: byte 256 lands mid-rune
	got := truncateAuditString(s, 256)
	if len(got) != 255+len(auditTruncatedMarker) || !strings.HasSuffix(got, auditTruncatedMarker) {
		t.Fatalf("len=%d, want a cut at byte 255 plus the marker", len(got))
	}
	if got := truncateAuditString("short", 256); got != "short" {
		t.Fatalf("a string within the bound was altered: %q", got)
	}
	if got := truncateAuditString(strings.Repeat("a", 256), 256); len(got) != 256 {
		t.Fatalf("a string at the bound was cut: len=%d", len(got))
	}
}

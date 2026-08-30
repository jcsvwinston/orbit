package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// TestAuditLog_JSONContract covers the /api/audit envelope defects measured in
// OH-6: entries always reported "id":0 (add() never assigned the ID), a
// request without page_size overflowed total_pages to MaxInt64 (division by
// zero → +Inf → int overflow), and create entries carried an empty record_id
// (the id is assigned by the database after the middleware read the path).
func TestAuditLog_JSONContract(t *testing.T) {
	panel, srv := adminTestServer(t)
	_ = panel

	// One create (no id in the path — the DB assigns it)...
	body, _ := json.Marshal(map[string]any{"email": "c@example.com", "name": "Contract", "active": true})
	resp, err := http.Post(srv.URL+"/api/models/AdminUser", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	createdID, ok := created["id"].(float64)
	if !ok || createdID == 0 {
		t.Fatalf("create response has no id: %#v", created)
	}

	// ...and one more create so ids can be checked for uniqueness.
	body2, _ := json.Marshal(map[string]any{"email": "d@example.com", "name": "Second", "active": false})
	resp2, err := http.Post(srv.URL+"/api/models/AdminUser", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	// Fetch the audit log WITHOUT page_size — the overflow trigger.
	auditResp, err := http.Get(srv.URL + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer auditResp.Body.Close()
	var payload struct {
		Enabled    bool `json:"enabled"`
		Total      int  `json:"total"`
		Page       int  `json:"page"`
		PageSize   int  `json:"page_size"`
		TotalPages int  `json:"total_pages"`
		Entries    []struct {
			ID       uint   `json:"id"`
			Action   string `json:"action"`
			RecordID string `json:"record_id"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(auditResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	if !payload.Enabled || len(payload.Entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %#v", payload)
	}

	// total_pages is sane when page_size is omitted (defaults applied).
	if payload.PageSize != 50 || payload.Page != 1 {
		t.Errorf("normalized page/page_size = %d/%d, want 1/50", payload.Page, payload.PageSize)
	}
	if payload.TotalPages != 1 {
		t.Errorf("total_pages = %d, want 1", payload.TotalPages)
	}

	// Every entry carries a real, unique id.
	seen := map[uint]bool{}
	for _, e := range payload.Entries {
		if e.ID == 0 {
			t.Errorf("entry has id 0: %+v", e)
		}
		if seen[e.ID] {
			t.Errorf("duplicate entry id %d", e.ID)
		}
		seen[e.ID] = true
	}

	// Create entries carry the record id the database assigned.
	wantIDs := map[string]bool{}
	for _, e := range payload.Entries {
		if e.Action != "create" {
			t.Errorf("unexpected action %q", e.Action)
			continue
		}
		if e.RecordID == "" {
			t.Errorf("create entry has empty record_id: %+v", e)
			continue
		}
		wantIDs[e.RecordID] = true
	}
	if !wantIDs["1"] || !wantIDs["2"] {
		t.Errorf("create record_ids = %v, want the DB-assigned ids 1 and 2", wantIDs)
	}
}

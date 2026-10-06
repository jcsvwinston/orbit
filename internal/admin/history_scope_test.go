package admin

// Tests for OR-73.
//
// The history of a record (GET /api/models/{name}/{id}/history) is the audit
// trail read by record, with the values of every write. It asked the row's
// #own scope and not its tenant: a row of another tenant answered 404 to the
// record view and 200 to its history, values included. It reads the record's
// scope the way the record view does now (requestReadScope): the same 404 for
// a row out of the scope, and the fields the operator may not read masked in
// every entry.
//
// The trail itself (GET /api/audit and its CSV copy) holds the same values,
// and was read by audit_view alone: an entry about a row showed what the row
// held to an operator whose own history of that row would show nothing. An
// entry that records a row's values (create, update, delete) shows them now
// only as the row's history would: with the model's retrieve, inside the
// request's tenant and, under an #own grant, the operator's own rows — read
// from the values the entry recorded — and with the fields the operator may
// not read masked. The entry itself is still listed.

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// seedRowEntry records an audit entry about one row the way a write of the
// record form does, with the values given as the row's before and after.
func seedRowEntry(panel *Panel, action, model, id string, before, after map[string]any) {
	panel.audit.add(AuditEntry{
		UserID: "9", Username: "somebody-else", Action: action,
		ModelName: model, RecordID: id, OldValue: before, NewValue: after,
	})
}

// enforce replaces the panel's policies with these.
func enforce(t *testing.T, panel *Panel, policies ...[3]string) {
	t.Helper()
	enf, err := authz.New(slog.Default())
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	for _, pol := range policies {
		if err := enf.AddPolicy(pol[0], pol[1], pol[2]); err != nil {
			t.Fatalf("AddPolicy %v: %v", pol, err)
		}
	}
	panel.rbac = enf
}

// auditEntriesOf lists the trail's entries about model, as the operator
// reads them.
func auditEntriesOf(t *testing.T, base, model string) []map[string]any {
	t.Helper()
	resp, status := doJSON(t, http.MethodGet, base+"/api/audit?page_size=200&model="+model, nil)
	if status != http.StatusOK {
		t.Fatalf("audit: status %d body=%s", status, mustJSON(resp))
	}
	raw, _ := resp["entries"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		if entry, ok := e.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

// entryOfRecord picks the entry about record id.
func entryOfRecord(t *testing.T, entries []map[string]any, id string) map[string]any {
	t.Helper()
	for _, e := range entries {
		if e["record_id"] == id {
			return e
		}
	}
	t.Fatalf("no entry about record %s among %v", id, entries)
	return nil
}

// ---- the history of one record -----------------------------------------

// seedTenantHistory records an edit of each of scopedPanel's two notes: the
// acme note (1) and the globex note (2).
func seedTenantHistory(panel *Panel) {
	seedRowEntry(panel, "update", "ScopedNote", "1",
		map[string]any{"id": 1, "tenant_id": "acme", "title": "Acme note"},
		map[string]any{"id": 1, "tenant_id": "acme", "title": "Acme plan"})
	seedRowEntry(panel, "update", "ScopedNote", "2",
		map[string]any{"id": 2, "tenant_id": "globex", "title": "Globex note"},
		map[string]any{"id": 2, "tenant_id": "globex", "title": "Globex plan"})
}

// A row of another tenant has no history in this request, the answer the
// record view gives the row itself — for a superuser too, until they leave
// the tenant through ?tenant=, as on every record surface.
func TestRecordHistory_IsConfinedToTheRequestsTenant(t *testing.T) {
	for name, adminAuth := range map[string]AdminAuth{"operator": operatorAuth(), "superuser": superuserAuth()} {
		t.Run(name, func(t *testing.T) {
			panel, _, srv := scopedPanel(t, adminAuth)
			seedTenantHistory(panel)

			view, viewStatus := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote/2", nil)
			resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote/2/history", nil)
			if status != http.StatusNotFound || viewStatus != http.StatusNotFound {
				t.Fatalf("another tenant's row: history %d body=%s, record %d body=%s, want 404 for both",
					status, mustJSON(resp), viewStatus, mustJSON(view))
			}
			if strings.Contains(mustJSON(resp), "Globex") {
				t.Fatalf("the refusal carries the row's values: %s", mustJSON(resp))
			}
			if mustJSON(resp) != mustJSON(view) {
				t.Errorf("the history and the record view refuse the row differently:\n history: %s\n record:  %s", mustJSON(resp), mustJSON(view))
			}

			resp, status = doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote/1/history", nil)
			if status != http.StatusOK || !strings.Contains(mustJSON(resp), "Acme plan") {
				t.Fatalf("the tenant's own row: status %d body=%s, want its history", status, mustJSON(resp))
			}
		})
	}

	// Leaving the tenant is the switch every record surface gates.
	panel, _, srv := scopedPanel(t, superuserAuth())
	seedTenantHistory(panel)
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/ScopedNote/2/history?tenant=all", nil)
	if status != http.StatusOK || !strings.Contains(mustJSON(resp), "Globex plan") {
		t.Fatalf("a superuser on ?tenant=all: status %d body=%s, want the row's history", status, mustJSON(resp))
	}
}

// A field the operator may not read is masked in every entry of the history,
// on both sides of the edit.
func TestRecordHistory_MasksTheFieldsTheOperatorMayNotRead(t *testing.T) {
	panel, _, srv := ownedPanel(t)
	seedRowEntry(panel, "update", "OwnedNote", "1",
		map[string]any{"id": 1, "owner": "operator", "title": "Mine"},
		map[string]any{"id": 1, "owner": "operator", "title": "Mine, renamed"})
	enforce(t, panel,
		[3]string{"operator", "admin:OwnedNote", "retrieve"},
		[3]string{"operator", "admin:OwnedNote.title", "deny"})

	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/models/OwnedNote/1/history", nil)
	if status != http.StatusOK {
		t.Fatalf("history: status %d body=%s", status, mustJSON(resp))
	}
	body := mustJSON(resp)
	if strings.Contains(body, "Mine") {
		t.Errorf("the history carries a field the operator may not read: %s", body)
	}
	if !strings.Contains(body, `"owner":"operator"`) {
		t.Errorf("the history lost the fields the operator reads: %s", body)
	}
}

// ---- the trail ----------------------------------------------------------

// The trail lists every entry to whoever holds audit_view; the values of a
// row of another tenant are not among what it shows this request — a
// superuser's included, until ?tenant= switches it.
func TestAuditTrail_RowValuesStayInTheRequestsTenant(t *testing.T) {
	for name, adminAuth := range map[string]AdminAuth{"operator": operatorAuth(), "superuser": superuserAuth()} {
		t.Run(name, func(t *testing.T) { assertTrailInTenant(t, adminAuth) })
	}

	panel, _, srv := scopedPanel(t, superuserAuth())
	seedTenantHistory(panel)
	resp, status := doJSON(t, http.MethodGet, srv.URL+"/api/audit?model=ScopedNote&tenant=all", nil)
	if status != http.StatusOK || !strings.Contains(mustJSON(resp), "Globex plan") {
		t.Fatalf("a superuser on ?tenant=all: status %d body=%s, want every row's values", status, mustJSON(resp))
	}
}

func assertTrailInTenant(t *testing.T, adminAuth AdminAuth) {
	panel, _, srv := scopedPanel(t, adminAuth)
	seedTenantHistory(panel)
	panel.audit.add(AuditEntry{Action: "flag.set", ModelName: "feature_flag", RecordID: "beta",
		NewValue: map[string]any{"enabled": true}})

	entries := auditEntriesOf(t, srv.URL, "ScopedNote")
	if len(entries) != 2 {
		t.Fatalf("%d entries about ScopedNote, want both still listed: %v", len(entries), entries)
	}
	globex := entryOfRecord(t, entries, "2")
	if globex["old_value"] != nil || globex["new_value"] != nil {
		t.Errorf("the entry about another tenant's row carries its values: %v", globex)
	}
	acme := entryOfRecord(t, entries, "1")
	if nu, _ := acme["new_value"].(map[string]any); nu["title"] != "Acme plan" {
		t.Errorf("the entry about the tenant's own row lost its values: %v", acme)
	}

	csv := getText(t, srv.URL+"/api/audit?format=csv")
	if strings.Contains(csv, "Globex") {
		t.Errorf("the trail's CSV copy carries another tenant's row: %s", csv)
	}
	if !strings.Contains(csv, "Acme plan") {
		t.Errorf("the trail's CSV copy lost the tenant's own row: %s", csv)
	}

	// An entry that records no row is not a row's to scope.
	flags := auditEntriesOf(t, srv.URL, "feature_flag")
	if len(flags) != 1 || flags[0]["new_value"] == nil {
		t.Errorf("an entry about a feature flag changed: %v", flags)
	}
}

// Under an #own grant the trail shows the values of the operator's own rows
// only, and a field the operator may not read is masked in every entry.
func TestAuditTrail_RowValuesFollowTheOwnGrantAndTheFieldPolicies(t *testing.T) {
	panel, _, srv := ownedPanel(t)
	seedRowEntry(panel, "update", "OwnedNote", "1",
		map[string]any{"id": 1, "owner": "operator", "title": "Mine"},
		map[string]any{"id": 1, "owner": "operator", "title": "Mine, renamed"})
	seedRowEntry(panel, "delete", "OwnedNote", "2",
		map[string]any{"id": 2, "owner": "somebody-else", "title": "Theirs"}, nil)
	seedRowEntry(panel, "create", "OwnedNote", "3",
		nil, map[string]any{"id": 3, "title": "Nobody's"})

	enforce(t, panel,
		[3]string{"operator", "admin:*", "audit_view"},
		[3]string{"operator", "admin:OwnedNote#own", "retrieve"},
		[3]string{"operator", "admin:OwnedNote.owner", "deny"})

	entries := auditEntriesOf(t, srv.URL, "OwnedNote")
	if len(entries) != 3 {
		t.Fatalf("%d entries, want all three still listed: %v", len(entries), entries)
	}
	if theirs := entryOfRecord(t, entries, "2"); theirs["old_value"] != nil {
		t.Errorf("the entry about another operator's row carries its values: %v", theirs)
	}
	// A row whose values do not say whose it was is not shown as the
	// operator's.
	if unowned := entryOfRecord(t, entries, "3"); unowned["new_value"] != nil {
		t.Errorf("the entry about a row with no owner carries its values: %v", unowned)
	}
	mine := entryOfRecord(t, entries, "1")
	old, _ := mine["old_value"].(map[string]any)
	nu, _ := mine["new_value"].(map[string]any)
	if old["title"] != "Mine" || nu["title"] != "Mine, renamed" {
		t.Errorf("the entry about the operator's own row lost its values: %v", mine)
	}
	if _, ok := nu["owner"]; ok {
		t.Errorf("the entry carries a field the operator may not read: %v", mine)
	}

	csv := getText(t, srv.URL+"/api/audit?format=csv")
	for _, hidden := range []string{"Theirs", "Nobody's", "somebody-else\"\"", `""owner""`} {
		if strings.Contains(csv, hidden) {
			t.Errorf("the trail's CSV copy carries %s: %s", hidden, csv)
		}
	}
}

// audit_view says who reads the trail, not whose rows: without the model's
// retrieve, the entries about its rows are listed without their values. A
// superuser reads them all.
func TestAuditTrail_RowValuesNeedTheModelsRetrieve(t *testing.T) {
	panel, _, srv := ownedPanel(t)
	seedRowEntry(panel, "update", "OwnedNote", "1",
		map[string]any{"id": 1, "owner": "operator", "title": "Mine"},
		map[string]any{"id": 1, "owner": "operator", "title": "Mine, renamed"})
	panel.audit.add(AuditEntry{Action: "rbac.policy.add", ModelName: "rbac", RecordID: "editors",
		NewValue: map[string]any{"object": "admin:OwnedNote"}})

	enforce(t, panel, [3]string{"operator", "admin:*", "audit_view"})
	entries := auditEntriesOf(t, srv.URL, "OwnedNote")
	if len(entries) != 1 {
		t.Fatalf("%d entries, want the entry still listed: %v", len(entries), entries)
	}
	if entries[0]["old_value"] != nil || entries[0]["new_value"] != nil {
		t.Errorf("an operator who may not open the model reads its rows in the trail: %v", entries[0])
	}
	if rbac := auditEntriesOf(t, srv.URL, "rbac"); len(rbac) != 1 || rbac[0]["new_value"] == nil {
		t.Errorf("an entry that records no row changed: %v", rbac)
	}

	panel.config.Auth.(*testAdminAuth).user = superuserAuth().(*testAdminAuth).user
	entries = auditEntriesOf(t, srv.URL, "OwnedNote")
	if len(entries) != 1 || entries[0]["new_value"] == nil {
		t.Errorf("a superuser does not read the trail whole: %v", entries)
	}
}

package admin

// Regression tests for OR-14 of the 2026-09 maturity audit: record ids are
// strings end to end (ADR-001 D1). The bulk endpoint decoded ids as []uint,
// so {"ids":["abc"]} — and even {"ids":["7"]} — answered 400 "invalid JSON";
// the CSV export parsed ?ids= as unsigned integers and dropped rows with any
// other key silently; a fixture pk that was not an unsigned integer was
// discarded without a word and the record created afresh under a new key.

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

func TestCanonicalID(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{nil, "", false},
		{"", "", false},
		{"  ", "", false},
		{" 7 ", "7", true},
		{"0b1c2d3e-0000-4000-8000-000000000001", "0b1c2d3e-0000-4000-8000-000000000001", true},
		{float64(3), "3", true},
		{float64(3.5), "3.5", true},
		{float64(1e15), "1000000000000000", true},
		{float32(2), "2", true},
		{int(4), "4", true},
		{int64(-5), "-5", true},
		{uint(6), "6", true},
		{uint64(7), "7", true},
		{json.Number("8"), "8", true},
		{[]byte("9"), "9", true},
		{fmt.Stringer(stringerValue{"abc"}), "abc", true},
	}
	for _, tc := range cases {
		got, ok := canonicalID(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("canonicalID(%#v) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

type stringerValue struct{ s string }

func (v stringerValue) String() string { return v.s }

func TestDecodeRecordIDs(t *testing.T) {
	raw := func(tokens ...string) []json.RawMessage {
		out := make([]json.RawMessage, 0, len(tokens))
		for _, tok := range tokens {
			out = append(out, json.RawMessage(tok))
		}
		return out
	}
	ids, err := decodeRecordIDs(raw(`"7"`, `8`, ` "0b1c2d3e-0000-4000-8000-000000000001" `, `-1`, `1.5`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := []string{"7", "8", "0b1c2d3e-0000-4000-8000-000000000001", "-1", "1.5"}; strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Errorf("ids = %v, want %v", ids, want)
	}

	for _, tc := range []struct {
		name   string
		tokens []string
		msg    string
	}{
		{"null", []string{`null`}, "ids[0] must be a string or a number"},
		{"object", []string{`"1"`, `{}`}, "ids[1] must be a string or a number"},
		{"array", []string{`[1]`}, "ids[0] must be a string or a number"},
		{"bool", []string{`true`}, "ids[0] must be a string or a number"},
		{"empty", []string{`""`}, "ids[0] must not be empty"},
		{"blank", []string{`"  "`}, "ids[0] must not be empty"},
	} {
		_, err := decodeRecordIDs(raw(tc.tokens...))
		if err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.msg)
		}
	}

	tooMany := make([]json.RawMessage, bulkMaxIDs+1)
	for i := range tooMany {
		tooMany[i] = json.RawMessage(`1`)
	}
	if _, err := decodeRecordIDs(tooMany); err == nil || !strings.Contains(err.Error(), "too many ids") {
		t.Errorf("over the cap: err = %v", err)
	}
}

func TestPanel_Bulk_AcceptsStringAndNumberIDs(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	a := createAdminUser(t, srv.URL, map[string]interface{}{"email": "a@example.com", "name": "A", "active": true})
	b := createAdminUser(t, srv.URL, map[string]interface{}{"email": "b@example.com", "name": "B", "active": true})

	// A string id and a numeric id in the same request: both delete.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "delete",
		"ids":    []interface{}{fmt.Sprint(a.ID), b.ID},
	})
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s (string ids used to be 400 invalid JSON)", status, mustJSON(resp))
	}
	if int(resp["deleted"].(float64)) != 2 || int(resp["failed"].(float64)) != 0 {
		t.Fatalf("deleted/failed = %v/%v, want 2/0", resp["deleted"], resp["failed"])
	}
}

func TestPanel_Bulk_RejectsNonScalarIDs(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	for _, tc := range []struct {
		name string
		body string
		msg  string
	}{
		{"null", `{"action":"delete","ids":[null]}`, "ids[0] must be a string or a number"},
		{"object", `{"action":"delete","ids":[{"id":1}]}`, "ids[0] must be a string or a number"},
		{"empty string", `{"action":"delete","ids":[""]}`, "ids[0] must not be empty"},
		{"export with comma", `{"action":"export","ids":["1,2"]}`, "contains a comma"},
	} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d body=%s, want 400", tc.name, res.StatusCode, raw)
			continue
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		errMap, _ := body["error"].(map[string]any)
		if errMap["code"] != "BAD_REQUEST" || !strings.Contains(fmt.Sprint(errMap["message"]), tc.msg) {
			t.Errorf("%s: error = %s, want code BAD_REQUEST and %q", tc.name, raw, tc.msg)
		}
	}
}

func TestPanel_BulkDelete_InvalidIDReportedPerRow(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	created := createAdminUser(t, srv.URL, map[string]interface{}{"email": "x@example.com", "name": "X", "active": true})

	// "abc" is a valid boundary id; it is the Nucleus backend that cannot
	// narrow it to its integer keys — a per-id failure with the backend's
	// own message, while the good id still deletes.
	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "delete",
		"ids":    []string{"abc", fmt.Sprint(created.ID)},
	})
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
	if int(resp["deleted"].(float64)) != 1 || int(resp["failed"].(float64)) != 1 {
		t.Fatalf("deleted/failed = %v/%v, want 1/1", resp["deleted"], resp["failed"])
	}
	errs := resp["errors"].([]interface{})
	row := errs[0].(map[string]interface{})
	if row["id"] != "abc" || !strings.Contains(fmt.Sprint(row["error"]), "positive integers") {
		t.Fatalf("error row = %#v, want id abc with the backend's message", row)
	}
}

func TestPanel_BulkExport_StringIDsRoundTrip(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	one := createAdminUser(t, srv.URL, map[string]interface{}{"email": "one@example.com", "name": "One", "active": true})
	_ = createAdminUser(t, srv.URL, map[string]interface{}{"email": "two@example.com", "name": "Two", "active": true})
	three := createAdminUser(t, srv.URL, map[string]interface{}{"email": "three@example.com", "name": "Three", "active": true})

	resp, status := doJSON(t, http.MethodPost, srv.URL+"/api/models/AdminUser/bulk", map[string]interface{}{
		"action": "export",
		"ids":    []interface{}{fmt.Sprint(one.ID), three.ID},
	})
	if status != http.StatusOK {
		t.Fatalf("status %d body=%s", status, mustJSON(resp))
	}
	// The echo is the boundary form: strings, whatever the client sent.
	if ids, _ := resp["ids"].([]interface{}); len(ids) != 2 || ids[0] != fmt.Sprint(one.ID) || ids[1] != fmt.Sprint(three.ID) {
		t.Fatalf("ids echo = %#v, want the two ids as strings", resp["ids"])
	}
	exportRes, err := http.Get(srv.URL + resp["export_url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer exportRes.Body.Close()
	rows, err := csv.NewReader(exportRes.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprint(rows)
	if len(rows)-1 != 2 || !strings.Contains(body, "one@example.com") || !strings.Contains(body, "three@example.com") || strings.Contains(body, "two@example.com") {
		t.Fatalf("csv rows = %v, want exactly one and three", rows)
	}
}

func TestExportCSV_IDsQuery_StringAndUnknown(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	one := createAdminUser(t, srv.URL, map[string]interface{}{"email": "one@example.com", "name": "One", "active": true})
	_ = createAdminUser(t, srv.URL, map[string]interface{}{"email": "two@example.com", "name": "Two", "active": true})

	// An id that matches no row selects nothing; it is not a 400.
	res, err := http.Get(fmt.Sprintf("%s/api/models/AdminUser/export?ids=%d,zzz", srv.URL, one.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 (a non-numeric id used to be 400)", res.StatusCode)
	}
	rows, err := csv.NewReader(res.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows)-1 != 1 || !strings.Contains(fmt.Sprint(rows), "one@example.com") {
		t.Fatalf("csv rows = %v, want only the first record", rows)
	}
}

// --- fixtures: pk is a string at the boundary ------------------------------

func fixtureJSON(t *testing.T, records ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoaddata_PKAsNumberOrString(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	store := newKeyedStore()
	panel.store = store
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	created := createAdminUser(t, srv.URL, map[string]interface{}{"email": "fx@example.com", "name": "Before", "active": true})
	ctx := context.Background()

	for _, pk := range []any{float64(created.ID), fmt.Sprint(created.ID)} {
		store.objects["_tmp/fixture.json"] = fixtureJSON(t, map[string]any{
			"model": "AdminUser", "pk": pk,
			"fields": map[string]any{"email": "fx@example.com", "name": fmt.Sprintf("After %v", pk), "active": true},
		})
		report, err := panel.Loaddata(ctx, LoaddataConfig{StorageKey: "_tmp/fixture.json", OnConflict: "update"})
		if err != nil {
			t.Fatalf("pk=%#v: %v", pk, err)
		}
		if report.Updated != 1 || report.Failed != 0 || report.Imported != 0 {
			t.Fatalf("pk=%#v: report = %+v, want one update", pk, report)
		}
		got, status := doJSON(t, http.MethodGet, fmt.Sprintf("%s/api/models/AdminUser/%d", srv.URL, created.ID), nil)
		if status != http.StatusOK || got["name"] != fmt.Sprintf("After %v", pk) {
			t.Fatalf("pk=%#v: record after loaddata = %v", pk, got)
		}
	}
}

func TestLoaddata_InvalidPKIsAFailedRow(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	store := newKeyedStore()
	panel.store = store
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()
	ctx := context.Background()

	countUsers := func() int {
		resp, _ := doJSON(t, http.MethodGet, srv.URL+"/api/models/AdminUser", nil)
		return int(resp["total"].(float64))
	}

	for _, pk := range []any{"abc", "", true, map[string]any{"id": 1}} {
		before := countUsers()
		store.objects["_tmp/fixture.json"] = fixtureJSON(t, map[string]any{
			"model": "AdminUser", "pk": pk,
			"fields": map[string]any{"email": "bad@example.com", "name": "Bad", "active": true},
		})
		report, err := panel.Loaddata(ctx, LoaddataConfig{StorageKey: "_tmp/fixture.json", OnConflict: "skip"})
		if err != nil {
			t.Fatalf("pk=%#v: %v", pk, err)
		}
		if report.Failed != 1 || report.Imported != 0 || len(report.Errors) != 1 {
			t.Fatalf("pk=%#v: report = %+v, want one failed row (the pk used to be dropped silently)", pk, report)
		}
		if !strings.Contains(report.Errors[0].Message, "pk") {
			t.Fatalf("pk=%#v: error %q does not name the pk", pk, report.Errors[0].Message)
		}
		if after := countUsers(); after != before {
			t.Fatalf("pk=%#v: a record was created (%d -> %d)", pk, before, after)
		}
	}
}

func TestDumpdata_Loaddata_RoundTrip(t *testing.T) {
	panel, cleanup := setupPanelForTest(t, db.EngineSQL)
	defer cleanup()
	store := newKeyedStore()
	panel.store = store
	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()
	ctx := context.Background()

	createAdminUser(t, srv.URL, map[string]interface{}{"email": "rt1@example.com", "name": "RT1", "active": true})
	createAdminUser(t, srv.URL, map[string]interface{}{"email": "rt2@example.com", "name": "RT2", "active": false})

	result, err := panel.Dumpdata(ctx, DumpdataConfig{Models: []string{"AdminUser"}})
	if err != nil {
		t.Fatal(err)
	}
	var records []DjangoFixtureRecord
	if err := json.Unmarshal([]byte(store.objects[result.StorageKey]), &records); err != nil {
		t.Fatalf("fixture is not JSON: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("dumped %d records, want 2", len(records))
	}

	// Loading the dump back finds every record by its pk: nothing is
	// created and, with on_conflict=skip, nothing changes.
	report, err := panel.Loaddata(ctx, LoaddataConfig{StorageKey: result.StorageKey, OnConflict: "skip"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 2 || report.Imported != 0 || report.Failed != 0 {
		t.Fatalf("round trip report = %+v, want 2 skipped", report)
	}
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strings"
	"testing"
)

// probeModelList asks the panel what it can browse. An application that
// registers models with the framework gets them listed without wiring
// anything else.
func probeModelList(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/models")
	if r.code != http.StatusOK {
		t.Logf("GET /admin/api/models answered %d: %s", r.code, r.text())
		return absent
	}
	models, _ := r.json(t)["models"].([]any)
	for _, m := range models {
		if entry, ok := m.(map[string]any); ok && entry["name"] == "Note" {
			return present
		}
	}
	t.Logf("Note is registered with the framework but the panel does not list it: %s", r.text())
	return absent
}

// probeCRUD walks one record through create, read, update and delete — the
// four verbs the whole product is named after.
func probeCRUD(t *testing.T, e *env) verdict {
	id := e.createNote(t, map[string]any{"title": "crud", "body": "first", "status": "draft"})

	got := e.get(t, "/admin/api/models/Note/"+id)
	if got.code != http.StatusOK {
		t.Logf("read back answered %d: %s", got.code, got.text())
		return partial
	}

	upd := e.do(t, http.MethodPut, "/admin/api/models/Note/"+id, map[string]any{"body": "second"})
	if upd.code >= 400 {
		t.Logf("update answered %d: %s", upd.code, upd.text())
		return partial
	}
	// Read the field back, not the payload: asking whether the new value
	// appears anywhere in the answer is how AUD-05 reported a trail as
	// surviving a restart because a record id matched a timestamp.
	after := e.get(t, "/admin/api/models/Note/"+id)
	body, _ := after.json(t)["body"].(string)
	if body == "" {
		if data, ok := after.json(t)["data"].(map[string]any); ok {
			body, _ = data["body"].(string)
		}
	}
	if body != "second" {
		t.Logf("update did not stick: body reads %q — %s", body, after.text())
		return partial
	}

	del := e.do(t, http.MethodDelete, "/admin/api/models/Note/"+id, nil)
	if del.code >= 400 {
		t.Logf("delete answered %d: %s", del.code, del.text())
		return partial
	}
	if gone := e.get(t, "/admin/api/models/Note/"+id); gone.code != http.StatusNotFound {
		t.Logf("deleted record still answers %d", gone.code)
		return partial
	}
	return present
}

// probeValidation posts a record the model forbids. An admin that writes
// whatever it is handed is an admin that corrupts data, so the control is
// not "it rejects" but "it rejects and says which field".
func probeValidation(t *testing.T, e *env) verdict {
	r := e.do(t, http.MethodPost, "/admin/api/models/Note", map[string]any{"body": "no title"})
	if r.code < 400 {
		t.Logf("a Note without its required title was accepted with %d: %s", r.code, r.text())
		return absent
	}
	if !strings.Contains(strings.ToLower(r.raw()), "title") {
		t.Logf("rejected with %d but without naming the field: %s", r.code, r.text())
		return partial
	}
	if r.code != http.StatusUnprocessableEntity {
		t.Logf("rejected with %d (not 422) naming the field: %s", r.code, r.text())
	}
	return present
}

// probePagination asks for one page of two and checks the envelope carries a
// total a UI can page on — a count of -1 or an estimate is a pager that
// cannot say how many pages there are.
func probePagination(t *testing.T, e *env) verdict {
	for i := 0; i < 3; i++ {
		e.createNote(t, map[string]any{"title": fmt.Sprintf("page-%d", i), "status": "open"})
	}
	plain := e.get(t, "/admin/api/models/Note?page=1&page_size=2")
	filtered := e.get(t, "/admin/api/models/Note?page=1&page_size=2&status=open")
	if plain.code != http.StatusOK || filtered.code != http.StatusOK {
		t.Logf("list answered %d / %d", plain.code, filtered.code)
		return absent
	}
	items, _ := filtered.json(t)["items"].([]any)
	if len(items) != 2 {
		t.Logf("page_size=2 returned %d rows", len(items))
		return partial
	}
	plainTotal, _ := plain.json(t)["total"].(float64)
	plainEstimated, _ := plain.json(t)["is_estimated"].(bool)
	filteredTotal, _ := filtered.json(t)["total"].(float64)
	filteredEstimated, _ := filtered.json(t)["is_estimated"].(bool)
	t.Logf("total unfiltered=%v estimated=%v · filtered=%v estimated=%v",
		plainTotal, plainEstimated, filteredTotal, filteredEstimated)
	switch {
	case plainTotal >= 3 && !plainEstimated && filteredTotal >= 3 && !filteredEstimated:
		return present
	case plainTotal >= 3 && !plainEstimated:
		return partial
	default:
		return partial
	}
}

// probeFilterOperators asks for a range, a substring, a set and a null, and
// checks that each ANSWER is narrower than the unfiltered list. Accepting the
// form is not the measurement: a filter that parses and then matches every row
// is worse than one that is rejected, because it looks like a result.
func probeFilterOperators(t *testing.T, e *env) verdict {
	// Every case is scoped to a status only these three rows carry: the bench
	// shares one application across probes, so a query that asked the whole
	// table would be answered by whatever ran first. Scoping also puts the two
	// halves of the contract in the same request — an exact-match Filter and an
	// operator Where — which is the composition an operator actually types.
	const scope = "operator-probe"
	e.createNote(t, map[string]any{"title": "cheap", "status": scope, "views": 1})
	e.createNote(t, map[string]any{"title": "popular", "status": scope, "views": 500})
	e.createNote(t, map[string]any{"title": "hidden", "status": scope, "views": 50})

	exact := e.get(t, "/admin/api/models/Note?status="+scope)
	if exact.code != http.StatusOK {
		t.Logf("exact-match filter answered %d: %s", exact.code, exact.text())
		return absent
	}

	// Each case names the titles the query must return, in any order.
	cases := []struct {
		query string
		want  []string
	}{
		{"views__gt=100", []string{"popular"}},
		{"views__gte=50&views__lte=500", []string{"popular", "hidden"}},
		{"title__contains=opula", []string{"popular"}},
		{"title__startswith=che", []string{"cheap"}},
		{"title__endswith=den", []string{"hidden"}},
		{"title__in=cheap,hidden", []string{"cheap", "hidden"}},
		{"title__not_in=cheap", []string{"popular", "hidden"}},
		{"views__ne=50", []string{"cheap", "popular"}},
		{"views__isnull=false", []string{"cheap", "popular", "hidden"}},
		{"title__in=", nil},
		// A wildcard in the VALUE is data, not a pattern. Without an explicit
		// ESCAPE this would match every row and look like a filter for the
		// per-cent sign — the same class of lie as a dropped filter.
		{"title__contains=%25", nil},
		{"title__contains=_", nil},
	}

	accepted, correct := 0, 0
	for _, c := range cases {
		r := e.get(t, "/admin/api/models/Note?status="+scope+"&"+c.query)
		if r.code != http.StatusOK {
			t.Logf("?%s answered %d: %s", c.query, r.code, r.text())
			continue
		}
		accepted++
		got := noteTitles(t, r)
		if sameSet(got, c.want) {
			correct++
			continue
		}
		t.Logf("?%s returned %v, want %v", c.query, got, c.want)
	}

	// A filter nobody can express is not silently equality: an operator this
	// build does not know has to be refused, or ?views__nope=1 would list
	// every row while reading as a filter.
	if bogus := e.get(t, "/admin/api/models/Note?status="+scope+"&views__nope=1"); bogus.code == http.StatusOK {
		t.Logf("an unknown operator was accepted and answered 200: the form is not validated")
		return partial
	}

	t.Logf("%d of %d operator forms accepted, %d filtered correctly", accepted, len(cases), correct)
	switch {
	case correct == len(cases):
		return present
	case accepted == 0:
		return absent
	default:
		return partial
	}
}

// noteTitles pulls the title of every row in a list response.
func noteTitles(t *testing.T, r response) []string {
	t.Helper()
	items, _ := r.json(t)["items"].([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		row, _ := it.(map[string]any)
		if title, ok := row["title"].(string); ok {
			out = append(out, title)
		}
	}
	return out
}

// sameSet compares two title lists ignoring order.
func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

// probeSearch types into the search box.
func probeSearch(t *testing.T, e *env) verdict {
	e.createNote(t, map[string]any{"title": "needle in the haystack", "status": "draft"})
	r := e.get(t, "/admin/api/models/Note?search=needle")
	if r.code != http.StatusOK {
		t.Logf("search answered %d: %s", r.code, r.text())
		return absent
	}
	items, _ := r.json(t)["items"].([]any)
	if len(items) == 0 {
		t.Logf("search returned nothing for a title that contains the term")
		return absent
	}
	// A search that ignores the term would return everything; check it does
	// not match a row it should not.
	none := e.get(t, "/admin/api/models/Note?search=zzz-no-such-title")
	if other, _ := none.json(t)["items"].([]any); len(other) > 0 {
		t.Logf("search for a term nothing contains returned %d rows: the term is ignored", len(other))
		return partial
	}
	return present
}

// probeOrdering asks the server to sort, which is the only sort that is
// correct across pages.
func probeOrdering(t *testing.T, e *env) verdict {
	e.createNote(t, map[string]any{"title": "sort-a", "status": "sorted", "views": 10})
	e.createNote(t, map[string]any{"title": "sort-b", "status": "sorted", "views": 90})

	django := e.get(t, "/admin/api/models/Note?status=sorted&order_by=-views")
	desc := e.get(t, "/admin/api/models/Note?status=sorted&order_by=views%20desc")
	if desc.code != http.StatusOK {
		t.Logf("order_by answered %d: %s", desc.code, desc.text())
		return absent
	}
	if django.code != http.StatusOK {
		t.Logf("the panel orders by \"views desc\"; the \"-views\" form every other admin accepts answers %d", django.code)
	}
	items, _ := desc.json(t)["items"].([]any)
	if len(items) < 2 {
		t.Logf("not enough rows to judge ordering: %s", desc.text())
		return partial
	}
	first, _ := items[0].(map[string]any)
	if views, ok := first["views"].(float64); !ok || views != 90 {
		t.Logf("order_by=-views did not put the highest first: %s", desc.text())
		return partial
	}
	return present
}

// probeBulkActions selects rows and acts on the selection.
func probeBulkActions(t *testing.T, e *env) verdict {
	a := e.createNote(t, map[string]any{"title": "bulk-a", "status": "bulk"})
	b := e.createNote(t, map[string]any{"title": "bulk-b", "status": "bulk"})

	r := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk",
		map[string]any{"action": "delete", "ids": []string{a, b}})
	if r.code >= 400 {
		t.Logf("bulk delete answered %d: %s", r.code, r.text())
		return absent
	}
	left := e.get(t, "/admin/api/models/Note?status=bulk")
	if rows, _ := left.json(t)["items"].([]any); len(rows) != 0 {
		t.Logf("bulk delete left %d rows behind: %s", len(rows), left.text())
		return partial
	}
	return present
}

// probeCustomActions asks for an action this application defined — the
// "publish these three" button every admin product has. The panel's bulk verb
// list is closed (delete/export), and nothing in the mount surface registers
// one, so the probe asks for a named action and reads the refusal.
func probeCustomActions(t *testing.T, e *env) verdict {
	id := e.createNote(t, map[string]any{"title": "custom-action", "status": "draft"})
	r := e.do(t, http.MethodPost, "/admin/api/models/Note/bulk",
		map[string]any{"action": "publish", "ids": []string{id}})
	if r.code < 400 {
		t.Logf("an application-defined action was accepted: %s", r.text())
		return present
	}
	schema := e.get(t, "/admin/api/models/Note/schema")
	if strings.Contains(schema.raw(), `"actions"`) {
		t.Logf("the schema advertises actions: %s", schema.text())
		return partial
	}
	t.Logf("bulk rejects an application verb (%d) and the schema advertises none", r.code)
	return absent
}

// probeRelationLookup follows a foreign key the way a form does: the field is
// marked, so the form needs the list of what it may point at.
func probeRelationLookup(t *testing.T, e *env) verdict {
	schema := e.get(t, "/admin/api/models/Comment/schema")
	if schema.code != http.StatusOK {
		t.Logf("schema answered %d: %s", schema.code, schema.text())
		return absent
	}
	marked := strings.Contains(schema.raw(), `"is_fk":true`)
	if !marked {
		t.Logf("the schema marks no field of Comment as a foreign key, note_id included")
	}

	// A candidate that exists, with a name a person would recognise: what
	// the lookup has to come back with is THIS row, not a 200.
	author := e.do(t, http.MethodPost, "/admin/api/models/Author", map[string]any{"name": "Ursula Lookup"})
	if author.code != http.StatusCreated {
		t.Fatalf("create Author answered %d: %s", author.code, author.text())
	}
	authorID := recordID(t, author.json(t))

	for _, path := range []string{
		"/admin/api/models/Comment/fields/author_id/options",
		"/admin/api/models/Author/options",
	} {
		r := e.get(t, path)
		if r.code != http.StatusOK || r.servedTheShell() {
			continue
		}
		// The endpoint answers; now ask whether it answers with something a
		// form can render — the id to store and the label to show.
		options, _ := r.json(t)["options"].([]any)
		for _, raw := range options {
			option, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if fmt.Sprint(option["value"]) != authorID {
				continue
			}
			if label := fmt.Sprint(option["label"]); label != "Ursula Lookup" {
				t.Logf("%s returns the candidate with %q as its label, not a name a person reads", path, label)
				return partial
			}
			return present
		}
		t.Logf("%s answers 200 but the candidate (Author %s) is not among its %d options: %s",
			path, authorID, len(options), r.text())
		return partial
	}
	if marked {
		t.Logf("the schema marks the key (is_fk) but no endpoint resolves what it points at")
		return partial
	}
	t.Logf("no foreign key metadata and no lookup endpoint: %s", schema.text())
	return absent
}

// probeNestedEditing asks for the shape every "order with its lines" screen
// needs: a parent and its children in one form.
func probeNestedEditing(t *testing.T, e *env) verdict {
	note := e.createNote(t, map[string]any{"title": "with-comments", "status": "draft"})
	nested := e.do(t, http.MethodPost, "/admin/api/models/Note", map[string]any{
		"title":    "nested",
		"status":   "draft",
		"comments": []map[string]any{{"body": "inline child"}},
	})
	if nested.code < 400 {
		list := e.get(t, "/admin/api/models/Comment")
		if strings.Contains(list.raw(), "inline child") {
			return present
		}
		t.Logf("a nested payload was accepted and the child was dropped: %s", nested.text())
	}
	schema := e.get(t, "/admin/api/models/Note/schema")
	if strings.Contains(schema.raw(), `"inlines"`) || strings.Contains(schema.raw(), `"children"`) {
		return partial
	}
	t.Logf("no inline metadata in the schema and nested children are not written (note %s)", note)
	return absent
}

// probeFieldTypes reads the widget vocabulary the schema publishes. Scalars
// are covered; the question is whether a form can hold a document, a file or
// rich text.
func probeFieldTypes(t *testing.T, e *env) verdict {
	r := e.get(t, "/admin/api/models/Note/schema")
	if r.code != http.StatusOK {
		return absent
	}
	body := r.raw()
	rich := 0
	for _, kind := range []string{`"html_type":"json"`, `"html_type":"file"`, `"html_type":"image"`, `"html_type":"richtext"`} {
		if strings.Contains(body, kind) {
			rich++
		}
	}
	scalars := strings.Contains(body, `"html_type":"text"`) && strings.Contains(body, `"html_type":"number"`)
	if rich == 0 {
		t.Logf("the schema publishes no widget a scalar input cannot be: %s", r.text())
		if scalars {
			return partial
		}
		return absent
	}

	// A widget for a file is only half of it: the file has to have somewhere
	// to go. The probe uploads one and then asks whether the panel answered
	// with a key a form could write into the record.
	key, status := uploadFieldFile(t, e, "Note", "cover", "cover.png", "image/png")
	if key == "" {
		t.Logf("rich widgets in schema: %d, but the field upload answered %d with no key", rich, status)
		if scalars {
			return partial
		}
		return absent
	}
	// And a field that is NOT a file is refused: an upload route that takes
	// anything is a way to fill a text column with a storage key nobody
	// declared.
	if _, status := uploadFieldFile(t, e, "Note", "title", "title.png", "image/png"); status < 400 {
		t.Logf("uploading into a scalar field answered %d: the route does not check the widget", status)
		return partial
	}
	t.Logf("rich widgets in schema: %d; an upload for Note.cover stored %s", rich, key)
	return present
}

// uploadFieldFile posts one file to a model's field-upload route and returns
// the stored key (empty when the panel refused) and the status.
func uploadFieldFile(t *testing.T, e *env, model, field, filename, contentType string) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	header.Set("Content-Type", contentType)
	part, err := mw.CreatePart(header)
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	_, _ = part.Write([]byte("bytes of a file"))
	if err := mw.WriteField("field", field); err != nil {
		t.Fatalf("multipart field: %v", err)
	}
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost,
		e.server().URL("/admin/api/models/"+model+"/upload"), &buf)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := e.operator(t).Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return "", resp.StatusCode
	}
	var payload map[string]any
	decodeInto(t, resp.Body, &payload)
	key, _ := payload["key"].(string)
	return key, resp.StatusCode
}

// probeExport downloads the table an operator selected.
func probeExport(t *testing.T, e *env) verdict {
	e.createNote(t, map[string]any{"title": "exported", "status": "export"})
	r := e.get(t, "/admin/api/models/Note/export?format=csv")
	if r.code != http.StatusOK {
		t.Logf("export answered %d: %s", r.code, r.text())
		return absent
	}
	if !strings.Contains(r.raw(), "exported") {
		t.Logf("export body does not contain the row: %s", r.text())
		return partial
	}
	return present
}

// probeImport uploads a file and lands its rows — upload, validate, execute,
// the three steps the UI wires to one button.
func probeImport(t *testing.T, e *env) verdict {
	csv := "title,status\nimported-row,imported\n"
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "notes.csv")
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write([]byte(csv)); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, e.server().URL("/admin/api/imports"), &buf)
	if err != nil {
		t.Fatalf("build upload: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := e.operator(t).Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		t.Logf("upload answered %d", resp.StatusCode)
		return absent
	}
	var payload map[string]any
	decodeInto(t, resp.Body, &payload)
	key, _ := payload["key"].(string)
	if key == "" {
		if data, ok := payload["data"].(map[string]any); ok {
			key, _ = data["key"].(string)
		}
	}
	if key == "" {
		t.Logf("upload gave no storage key: %v", payload)
		return partial
	}

	cfg := map[string]any{"model": "Note", "format": "csv", "on_conflict": "skip"}
	valid := e.do(t, http.MethodPost, "/admin/api/import/validate?key="+key, cfg)
	if valid.code != http.StatusOK {
		t.Logf("validate answered %d: %s", valid.code, valid.text())
		return partial
	}
	exec := e.do(t, http.MethodPost, "/admin/api/import/execute?key="+key, cfg)
	if exec.code != http.StatusOK {
		t.Logf("execute answered %d: %s", exec.code, exec.text())
		return partial
	}
	if landed := e.get(t, "/admin/api/models/Note?status=imported"); !strings.Contains(landed.raw(), "imported-row") {
		t.Logf("execute reported success and the row is not there: %s", landed.text())
		return partial
	}
	return present
}

// probeFixtures dumps and reloads, the Django dumpdata/loaddata pair.
func probeFixtures(t *testing.T, e *env) verdict {
	e.createNote(t, map[string]any{"title": "fixture-row", "status": "fixture"})
	dump := e.do(t, http.MethodPost, "/admin/api/fixtures/dumpdata", map[string]any{"models": []string{"Note"}})
	if dump.code != http.StatusOK {
		t.Logf("dumpdata answered %d: %s", dump.code, dump.text())
		return absent
	}
	key, _ := dump.json(t)["storage_key"].(string)
	if key == "" {
		key, _ = dump.json(t)["id"].(string)
	}
	if key == "" {
		t.Logf("dumpdata gave no key: %s", dump.text())
		return partial
	}
	load := e.do(t, http.MethodPost, "/admin/api/fixtures/loaddata",
		map[string]any{"key": key, "on_conflict": "skip"})
	if load.code != http.StatusOK {
		t.Logf("loaddata answered %d: %s", load.code, load.text())
		return partial
	}
	return present
}

// probeRecordHistory asks what an operator asks after a bad edit: what did
// this row look like before, and who changed it.
//
// The answer has to carry the VALUES, not just the fact that an edit
// happened: a history that says "somebody updated this row" is a timestamp,
// and what the operator needs is the title it used to have.
func probeRecordHistory(t *testing.T, e *env) verdict {
	const (
		before = "historic"
		after  = "historic (edited)"
	)
	id := e.createNote(t, map[string]any{"title": before, "status": "draft"})
	e.do(t, http.MethodPut, "/admin/api/models/Note/"+id, map[string]any{"title": after})

	history := e.get(t, "/admin/api/models/Note/"+id+"/history")
	if history.code == http.StatusNotFound || history.code == http.StatusMethodNotAllowed || history.servedTheShell() {
		return e.unrouted(t,
			"/admin/api/models/Note/"+id+"/versions",
			"/admin/api/models/Note/"+id+"/revisions")
	}
	if history.code != http.StatusOK {
		t.Logf("the history of Note %s answered %d: %s", id, history.code, history.text())
		return partial
	}

	entries, _ := history.json(t)["entries"].([]any)
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok || entry["action"] != "update" {
			continue
		}
		old, _ := entry["old_value"].(map[string]any)
		nu, _ := entry["new_value"].(map[string]any)
		if old == nil || nu == nil {
			continue
		}
		if fmt.Sprint(old["title"]) != before || fmt.Sprint(nu["title"]) != after {
			continue
		}
		if fmt.Sprint(entry["username"]) == "" {
			t.Logf("the history says what changed and not who changed it: %v", entry)
			return partial
		}
		return present
	}
	t.Logf("the history of Note %s (%d entries) does not carry the edit's before and after: %s",
		id, len(entries), history.text())
	return partial
}

// probeSavedViews asks for the filter set an operator uses every morning: it
// saves one, reads it back, and checks that what comes back is the query the
// grid was showing — a name with no query is a bookmark to nothing.
func probeSavedViews(t *testing.T, e *env) verdict {
	const (
		name  = "Bench view"
		query = "status=open&order_by=title+asc"
	)
	created := e.do(t, http.MethodPost, "/admin/api/views", map[string]any{
		"model": "Note", "name": name, "query": query,
	})
	if created.code == http.StatusNotFound || created.code == http.StatusMethodNotAllowed ||
		created.code == http.StatusNotImplemented || created.servedTheShell() {
		return e.unrouted(t, "/admin/api/views", "/admin/api/saved-searches")
	}
	if created.code != http.StatusCreated {
		t.Logf("saving a view answered %d: %s", created.code, created.text())
		return partial
	}
	id, _ := created.json(t)["id"].(string)

	list := e.get(t, "/admin/api/views?model=Note")
	if list.code != http.StatusOK {
		t.Logf("listing views answered %d: %s", list.code, list.text())
		return partial
	}
	views, _ := list.json(t)["views"].([]any)
	for _, raw := range views {
		view, ok := raw.(map[string]any)
		if !ok || view["id"] != id {
			continue
		}
		if fmt.Sprint(view["name"]) != name {
			t.Logf("the stored view came back as %q", view["name"])
			return partial
		}
		if fmt.Sprint(view["query"]) != query {
			t.Logf("the view lost the query it was saved with: %q", view["query"])
			return partial
		}
		// And it can be removed again: a list that only grows is not a
		// place an operator will keep their views.
		if del := e.do(t, http.MethodDelete, "/admin/api/views/"+id, nil); del.code != http.StatusOK {
			t.Logf("deleting the view answered %d: %s", del.code, del.text())
			return partial
		}
		return present
	}
	t.Logf("the saved view (%s) is not in the list of %d: %s", id, len(views), list.text())
	return partial
}

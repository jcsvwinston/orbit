// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
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

// probeFilterOperators asks for a range. Exact equality is the only operator
// the query contract has (datasource.Query.Filters is column→value), so this
// probe measures what an operator gets when they need "views greater than".
func probeFilterOperators(t *testing.T, e *env) verdict {
	e.createNote(t, map[string]any{"title": "cheap", "status": "published", "views": 1})
	e.createNote(t, map[string]any{"title": "popular", "status": "published", "views": 500})

	exact := e.get(t, "/admin/api/models/Note?status=published")
	if exact.code != http.StatusOK {
		t.Logf("exact-match filter answered %d: %s", exact.code, exact.text())
		return absent
	}
	for _, form := range []string{
		"/admin/api/models/Note?views__gt=100",
		"/admin/api/models/Note?views%5Bgt%5D=100",
		"/admin/api/models/Note?filter=views>100",
		"/admin/api/models/Note?title__contains=pop",
	} {
		r := e.get(t, form)
		if r.code == http.StatusOK {
			items, _ := r.json(t)["items"].([]any)
			t.Logf("%s answered 200 with %d items — an operator form exists", form, len(items))
			return partial
		}
	}
	t.Logf("every operator form is rejected; equality is the whole filter language")
	return partial
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
	for _, path := range []string{
		"/admin/api/models/Comment/fields/note_id/options",
		"/admin/api/models/Note/lookup?q=",
		"/admin/api/models/Note/options",
	} {
		if r := e.get(t, path); r.code == http.StatusOK && !r.servedTheShell() {
			t.Logf("%s answers 200: a lookup endpoint exists", path)
			return present
		}
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
	upload := e.do(t, http.MethodPost, "/admin/api/models/Note/upload", map[string]any{})
	uploadRouted := upload.code != http.StatusNotFound &&
		upload.code != http.StatusMethodNotAllowed && !upload.servedTheShell()
	t.Logf("rich widgets in schema: %d; field upload route answers %d (routed: %v)", rich, upload.code, uploadRouted)
	switch {
	case rich > 0 && uploadRouted:
		return present
	case scalars:
		return partial
	default:
		return absent
	}
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
func probeRecordHistory(t *testing.T, e *env) verdict {
	id := e.createNote(t, map[string]any{"title": "historic", "status": "draft"})
	e.do(t, http.MethodPut, "/admin/api/models/Note/"+id, map[string]any{"title": "historic (edited)"})
	return e.unrouted(t,
		"/admin/api/models/Note/"+id+"/history",
		"/admin/api/models/Note/"+id+"/versions",
		"/admin/api/models/Note/"+id+"/revisions")
}

// probeSavedViews asks for the filter set an operator uses every morning.
func probeSavedViews(t *testing.T, e *env) verdict {
	// Only paths the record route cannot claim: /api/models/Note/<anything>
	// matches the get-one-record pattern and answers 400 for a non-numeric id.
	return e.unrouted(t, "/admin/api/views", "/admin/api/saved-searches")
}

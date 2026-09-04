package nucleus

import (
	"context"
	"errors"
	"net/http"
	"testing"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/datasource"
)

// validationDetails asserts err is the 422 the framework validator produces
// and returns its per-field messages.
func validationDetails(t *testing.T, err error) map[string]string {
	t.Helper()
	var de *gferrors.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("want *DomainError, got %T: %v", err, err)
	}
	if de.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%v)", de.StatusCode, de)
	}
	fields, ok := de.Details.(map[string]string)
	if !ok {
		t.Fatalf("details = %#v, want map[string]string", de.Details)
	}
	return fields
}

func TestCreate_RunsModelValidation(t *testing.T) {
	st, _ := setupAdapter(t).Store("DSPost", "")
	ctx := context.Background()

	// A required field missing used to produce a 201 with an empty title.
	_, err := st.Create(ctx, datasource.Record{"body": "b"})
	if fields := validationDetails(t, err); fields["title"] == "" {
		t.Fatalf("missing required title must be reported per field, got %v", fields)
	}
	// Two rules at once: both fields come back in one 422.
	_, err = st.Create(ctx, datasource.Record{"title": "this title is far too long", "views": -1})
	fields := validationDetails(t, err)
	if fields["title"] == "" || fields["views"] == "" {
		t.Fatalf("want title and views reported, got %v", fields)
	}
	page, _ := st.List(ctx, datasource.Query{})
	if len(page.Items) != 0 {
		t.Fatalf("nothing may be written when validation fails, got %d rows", len(page.Items))
	}

	created, err := st.Create(ctx, datasource.Record{"title": "ok", "views": 3})
	if err != nil {
		t.Fatalf("valid create: %v", err)
	}
	if created["title"] != "ok" {
		t.Fatalf("created = %v", created)
	}
}

func TestCreate_RejectsTypeLossAndUnknownFields(t *testing.T) {
	st, _ := setupAdapter(t).Store("DSPost", "")
	ctx := context.Background()

	// 123 for a string field used to be stored as "123.0".
	_, err := st.Create(ctx, datasource.Record{"title": 123})
	if fields := validationDetails(t, err); fields["title"] != "must be a string" {
		t.Fatalf("number into string field: got %v", fields)
	}
	// A key naming no field used to be dropped silently.
	_, err = st.Create(ctx, datasource.Record{"title": "ok", "colour": "red"})
	if fields := validationDetails(t, err); fields["colour"] != "unknown field" {
		t.Fatalf("unknown key: got %v", fields)
	}
	// Non-numeric text into an int field.
	_, err = st.Create(ctx, datasource.Record{"title": "ok", "views": "many"})
	if fields := validationDetails(t, err); fields["views"] == "" {
		t.Fatalf("text into int field must be reported, got %v", fields)
	}
	// Metadata keys (multi-model exports) and numeric strings stay accepted.
	if _, err := st.Create(ctx, datasource.Record{"title": "ok", "views": "7", "_model": "DSPost"}); err != nil {
		t.Fatalf("numeric string into int and _model metadata must pass: %v", err)
	}
}

func TestUpdate_MergesThenValidates(t *testing.T) {
	st, _ := setupAdapter(t).Store("DSPost", "")
	ctx := context.Background()
	created, err := st.Create(ctx, datasource.Record{"title": "first", "views": 1})
	if err != nil {
		t.Fatal(err)
	}
	id := recordID(t, created)

	// Clearing a required field through a partial update is refused.
	err = st.Update(ctx, id, datasource.Record{"title": ""})
	if fields := validationDetails(t, err); fields["title"] == "" {
		t.Fatalf("emptying a required field must fail validation, got %v", fields)
	}
	// Wrong type on update is refused too, and nothing is written.
	err = st.Update(ctx, id, datasource.Record{"title": 123})
	if fields := validationDetails(t, err); fields["title"] != "must be a string" {
		t.Fatalf("number into string on update: got %v", fields)
	}
	got, _ := st.Get(ctx, id)
	if got["title"] != "first" {
		t.Fatalf("failed updates must not write, title = %v", got["title"])
	}
	// A partial update that keeps the entity valid goes through, and the
	// untouched required field survives.
	if err := st.Update(ctx, id, datasource.Record{"views": 9}); err != nil {
		t.Fatalf("valid partial update: %v", err)
	}
	got, _ = st.Get(ctx, id)
	if got["title"] != "first" || asInt(got["views"]) != 9 {
		t.Fatalf("after update = %v", got)
	}
	// Only the primary key or read-only fields: nothing to update → 400.
	err = st.Update(ctx, id, datasource.Record{"id": 99})
	var de *gferrors.DomainError
	if !errors.As(err, &de) || de.StatusCode != http.StatusBadRequest {
		t.Fatalf("pk-only update: want 400, got %v", err)
	}
}

// A non-numeric id is the caller's mistake: 400, not a 500 that reads as an
// outage.
func TestInvalidID_IsBadRequest(t *testing.T) {
	st, _ := setupAdapter(t).Store("DSPost", "")
	ctx := context.Background()
	for name, fn := range map[string]func() error{
		"get":    func() error { _, err := st.Get(ctx, "abc"); return err },
		"update": func() error { return st.Update(ctx, "abc", datasource.Record{"title": "x"}) },
		"delete": func() error { return st.Delete(ctx, "abc") },
	} {
		var de *gferrors.DomainError
		if err := fn(); !errors.As(err, &de) || de.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s with id abc: want 400 DomainError, got %v", name, err)
		}
	}
}

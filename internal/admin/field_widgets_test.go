// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

// fieldWidgetModels is the shape the startup check reads: a model whose Go
// names and columns differ in case, and one whose column differs in name.
func fieldWidgetModels() []datasource.ModelInfo {
	return []datasource.ModelInfo{
		{Name: "Album", Fields: []datasource.FieldInfo{
			{Name: "Title", Column: "title"},
			{Name: "Notes", Column: "notes"},
			{Name: "Cover", Column: "cover_key"},
		}},
		{Name: "Track", Fields: []datasource.FieldInfo{{Name: "Title", Column: "title"}}},
	}
}

// TestValidateFieldWidgetsRefusesWhatWouldBeDropped: each of these started
// before the check existed, and the schema published the field as if nothing
// had been declared. The refusal names the entry.
func TestValidateFieldWidgetsRefusesWhatWouldBeDropped(t *testing.T) {
	cases := []struct {
		name    string
		widgets map[string]string
		want    []string
	}{
		{name: "a widget the panel does not ship", widgets: map[string]string{"Album.Notes": "colour"},
			want: []string{`field_widgets["Album.Notes"]`, `"colour" is not a widget`}},
		{name: "a misspelled widget", widgets: map[string]string{"Album.Notes": "rich-text"},
			want: []string{`"rich-text" is not a widget`}},
		{name: "a field that does not exist", widgets: map[string]string{"Album.Nothing": "json"},
			want: []string{`field_widgets["Album.Nothing"]`, `model Album has no field or column named "Nothing"`}},
		{name: "a model that does not exist", widgets: map[string]string{"Record.Notes": "richtext"},
			want: []string{`no model named "Record"`}},
		{name: "a key that is not Model.Field", widgets: map[string]string{"Notes": "richtext"},
			want: []string{`field_widgets["Notes"]: a key is Model.Field or Model.column`}},
		{name: "a wildcard, which was never applied", widgets: map[string]string{"Album.*": "json"},
			want: []string{`model Album has no field or column named "*"`}},
		{name: "two spellings of one field with different widgets",
			widgets: map[string]string{"Album.Cover": "image", "Album.cover_key": "file"},
			want:    []string{`field_widgets["Album.Cover"] and field_widgets["Album.cover_key"] both name Album.Cover`}},
		{name: "every problem at once, in key order",
			widgets: map[string]string{"Album.Notes": "colour", "Album.Nothing": "json", "Album.Title": "richtext"},
			want:    []string{`field_widgets["Album.Notes"]`, `; field_widgets["Album.Nothing"]`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFieldWidgets(tc.widgets, fieldWidgetModels(), nil)
			if err == nil {
				t.Fatalf("%v was accepted", tc.widgets)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not say %q: %v", want, err)
				}
			}
		})
	}
}

// TestValidateFieldWidgetsAcceptsWhatTheSchemaApplies: the spellings
// declaredWidget reads — the Go name, the column, either case, the aliases
// normalizeWidget folds — and two spellings of one field that agree.
func TestValidateFieldWidgetsAcceptsWhatTheSchemaApplies(t *testing.T) {
	for _, widgets := range []map[string]string{
		nil,
		{"Album.Notes": "richtext", "Album.cover_key": "image", "Track.Title": "json"},
		{"album.notes": "HTML", "ALBUM.COVER": "file"},
		{"Album.Notes": "rich_text", "Album.notes": "richtext"},
	} {
		if err := validateFieldWidgets(widgets, fieldWidgetModels(), nil); err != nil {
			t.Errorf("%v was refused: %v", widgets, err)
		}
	}
	if err := ValidateFieldWidgets(nil, nil, ClientCode{}); err != nil {
		t.Errorf("no declaration on a panel with no source was refused: %v", err)
	}
	if err := ValidateFieldWidgets(nil, map[string]string{"Album.Notes": "json"}, ClientCode{}); err == nil {
		t.Error("a declaration on a panel with no models was accepted")
	}
}

// TestValidateFieldWidgetsAgreesWithTheSchema closes the loop over a real
// panel: what the forms harness declares is accepted by the check, and is
// what the schema then publishes (TestFieldWidgets_DeclaredAndInferred).
// A check that accepted a key the schema does not apply would be the
// silent drop again, one step later.
func TestValidateFieldWidgetsAgreesWithTheSchema(t *testing.T) {
	panel, _, srv := formsPanel(t, nil)
	if err := ValidateFieldWidgets(panel.src, panel.config.FieldWidgets, panel.config.Client); err != nil {
		t.Fatalf("the harness's own declaration is refused: %v", err)
	}
	schema := albumSchema(t, srv)
	fields, _ := schema["fields"].([]interface{})
	applied := 0
	for _, raw := range fields {
		f, _ := raw.(map[string]interface{})
		switch f["html_type"] {
		case widgetRichText, widgetImage, widgetJSON:
			applied++
		}
	}
	if applied != len(panel.config.FieldWidgets) {
		t.Fatalf("%d declared widgets accepted, %d applied by the schema", len(panel.config.FieldWidgets), applied)
	}
}

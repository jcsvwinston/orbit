// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

// TestMask_DropsExcludedFieldsWithoutFieldPolicies pins what the admin bench
// found (DS-18): a field the model declares excluded is not in the schema,
// and used to travel in every row anyway unless a field policy happened to
// apply to the operator — the only case mask looked at a record. The three
// masks drop it for every operator, and keep the key.
func TestMask_DropsExcludedFieldsWithoutFieldPolicies(t *testing.T) {
	mi := datasource.ModelInfo{Name: "Credential", PrimaryKey: "ID", Fields: []datasource.FieldInfo{
		{Name: "ID", Column: "id", IsPK: true},
		{Name: "Label", Column: "label"},
		{Name: "Recovery", Column: "recovery", IsExcluded: true},
	}}
	var none fieldRules
	if none.enforced() {
		t.Fatal("the zero rules enforce nothing; the test measures the mask without a policy")
	}

	rec := none.mask(mi, datasource.Record{"id": 1, "label": "x", "recovery": "codes"})
	if _, leaked := rec["recovery"]; leaked || rec["id"] != 1 || rec["label"] != "x" {
		t.Errorf("mask = %v, want the excluded field gone and the rest kept", rec)
	}
	rec = none.mask(mi, datasource.Record{"id": 1, "Recovery": "by Go name"})
	if _, leaked := rec["Recovery"]; leaked {
		t.Errorf("mask resolves the field under its Go name too: %v", rec)
	}

	items := []datasource.Record{{"id": 1, "recovery": "a"}, {"id": 2, "recovery": "b"}}
	none.maskAll(mi, items)
	for _, it := range items {
		if _, leaked := it["recovery"]; leaked {
			t.Errorf("maskAll left the excluded field in a row: %v", it)
		}
	}

	values := none.maskValues(mi, map[string]any{"id": 1, "recovery": "a", "label": "x"})
	if _, leaked := values["recovery"]; leaked || values["label"] != "x" {
		t.Errorf("maskValues = %v", values)
	}

	// A model that hides nothing costs nothing: the record comes back as it
	// is, the same map.
	plain := datasource.ModelInfo{Name: "Note", Fields: []datasource.FieldInfo{{Name: "ID", Column: "id", IsPK: true}, {Name: "Title", Column: "title"}}}
	in := datasource.Record{"id": 1, "title": "t"}
	if out := none.mask(plain, in); len(out) != 2 {
		t.Errorf("mask over a model with nothing hidden = %v", out)
	}
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// Inline editing: the parent and its children in one form.
//
// "An order with its lines", "a post with its comments" — the shape every
// admin needs and the one the panel could not do: the children were a second
// model, edited on a second screen, related by an id the person had to carry
// across in their head. A nested payload was not refused either; it was
// DROPPED, which is worse, because the form looked like it had saved.
//
// The relationship is already known: a child declares the key it points at
// (`fk:model=…`), so the parent's inlines are the models whose foreign key
// names it. The schema publishes them; create and update write them.
//
// What this deliberately does NOT do: pretend to be transactional. The
// datasource contract (ADR-001) has no transaction — a RecordStore writes one
// row — so the parent is written first and the children after it, each with
// its own result. A child that fails is reported BY id in the response
// instead of rolling back a parent that is already there, and the response
// says how many went in. A form that needs all-or-nothing needs a
// transactional datasource first; saying so is better than implying it.

// inlineSpec is one child relation of a parent model.
type inlineSpec struct {
	// Model is the child model, Field/Column the key that points at the
	// parent, and Key what a payload names the collection by.
	Model  string `json:"model"`
	Field  string `json:"field"`
	Column string `json:"column"`
	Key    string `json:"key"`
	Label  string `json:"label"`
}

// inlineDeleteKey marks a child the payload wants gone. Absence is never
// deletion: a form that sends the two lines it edited must not remove the
// third one it never loaded.
const inlineDeleteKey = "_delete"

// inlinesFor lists the child relations of mi: every model with a foreign key
// pointing at it. The catalogue is read once per call — it is a handful of
// models in memory, and a cache here would be one more thing to invalidate
// when a registry changes at runtime.
func (p *Panel) inlinesFor(mi datasource.ModelInfo) []inlineSpec {
	var out []inlineSpec
	for _, child := range p.src.All() {
		if child.Name == mi.Name {
			continue
		}
		for _, fk := range child.ForeignKeys {
			if !strings.EqualFold(fk.ForeignModel, mi.Name) {
				continue
			}
			out = append(out, inlineSpec{
				Model:  child.Name,
				Field:  fk.FieldName,
				Column: runtimeColumn(fk.Column),
				Key:    inlineKey(child),
				Label:  child.Plural,
			})
		}
	}
	return out
}

// inlineKey is what a payload calls the collection: the child's plural, in
// lower case ("comments"). A payload may also name the model itself
// ("Comment"), which matchInline accepts.
func inlineKey(child datasource.ModelInfo) string {
	plural := strings.TrimSpace(child.Plural)
	if plural == "" {
		plural = child.Name + "s"
	}
	return strings.ToLower(plural)
}

// takeInlinePayloads removes from data every key that names an inline
// collection and returns them. They are taken OUT of the payload because the
// parent's own store must not receive them: a backend handed an unknown key
// either refuses the write or ignores it, and the second is how a nested
// payload disappeared without a word.
func takeInlinePayloads(data map[string]any, specs []inlineSpec) map[string][]map[string]any {
	if len(specs) == 0 || len(data) == 0 {
		return nil
	}
	out := map[string][]map[string]any{}
	for _, spec := range specs {
		for key := range data {
			if !matchInline(spec, key) {
				continue
			}
			children, ok := asRecordSlice(data[key])
			delete(data, key)
			if ok {
				out[spec.Model] = children
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func matchInline(spec inlineSpec, key string) bool {
	return strings.EqualFold(key, spec.Key) || strings.EqualFold(key, spec.Model)
}

// asRecordSlice reads a JSON array of objects. Anything else is not an inline
// collection and is left alone.
func asRecordSlice(v any) ([]map[string]any, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		rec, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, rec)
	}
	return out, true
}

// inlineResult is what one child collection did.
type inlineResult struct {
	Model   string   `json:"model"`
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Deleted int      `json:"deleted"`
	Errors  []string `json:"errors,omitempty"`
}

// writeInlines applies the child collections of one parent write. It returns
// one result per collection; an error in a child is reported there, not
// raised — the parent is already written, and pretending otherwise would be
// the roll-back this cannot do.
func (p *Panel) writeInlines(c *router.Context, parent datasource.ModelInfo, parentID string, payloads map[string][]map[string]any, specs []inlineSpec, databaseAlias string) ([]inlineResult, error) {
	if len(payloads) == 0 {
		return nil, nil
	}
	r := c.Request
	results := make([]inlineResult, 0, len(payloads))

	for _, spec := range specs {
		children, ok := payloads[spec.Model]
		if !ok {
			continue
		}
		childInfo, ok := p.src.Get(spec.Model)
		if !ok {
			continue
		}
		st, err := p.src.Store(childInfo.Name, databaseAlias)
		if err != nil {
			return nil, err
		}

		result := inlineResult{Model: spec.Model}
		for i, child := range children {
			if err := p.writeInlineChild(r.Context(), r, childInfo, st, spec, parent, parentID, child, &result); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s[%d]: %s", spec.Key, i, err.Error()))
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// authorizeInlines checks every verb the nested payload needs BEFORE the
// parent is written. The order matters: the datasource has no transaction
// (ADR-001), so a refusal discovered after the parent was written would leave
// the parent saved and the form told "forbidden" — the one outcome nobody can
// act on.
func (p *Panel) authorizeInlines(c *router.Context, payloads map[string][]map[string]any, specs []inlineSpec) error {
	for _, spec := range specs {
		children, ok := payloads[spec.Model]
		if !ok {
			continue
		}
		childInfo, ok := p.src.Get(spec.Model)
		if !ok {
			return gferrors.NotFound("model", spec.Model)
		}
		if childInfo.ReadOnly {
			return gferrors.Forbidden(spec.Model + " is read-only")
		}
		if err := p.authorizeInline(c, childInfo, children); err != nil {
			return err
		}
	}
	return nil
}

// authorizeInline checks the verbs this collection needs before any of them
// runs: a batch that is half refused is worse than one that is refused.
func (p *Panel) authorizeInline(c *router.Context, child datasource.ModelInfo, children []map[string]any) error {
	needed := map[string]bool{}
	for _, rec := range children {
		switch {
		case isInlineDelete(rec):
			needed["delete"] = true
		case inlineChildID(rec) != "":
			needed["update"] = true
		default:
			needed["create"] = true
		}
	}
	for _, action := range []string{"create", "update", "delete"} {
		if !needed[action] {
			continue
		}
		if _, err := p.authorizeRecordAction(c, child, action); err != nil {
			return err
		}
	}
	return nil
}

func isInlineDelete(rec map[string]any) bool {
	v, ok := rec[inlineDeleteKey]
	if !ok {
		return false
	}
	deleted, _ := v.(bool)
	return deleted
}

// inlineChildID reads the child's primary key from its payload, which is what
// tells an edit from an insert.
func inlineChildID(rec map[string]any) string {
	for _, key := range []string{"id", "ID", "Id"} {
		if v, ok := rec[key]; ok {
			if id, ok := canonicalID(v); ok {
				return id
			}
		}
	}
	return ""
}

// writeInlineChild applies one child: delete, update or create, with the
// parent key stamped so a child can never be filed under another parent.
func (p *Panel) writeInlineChild(ctx context.Context, r *http.Request, childInfo datasource.ModelInfo, st datasource.RecordStore, spec inlineSpec, parent datasource.ModelInfo, parentID string, child map[string]any, result *inlineResult) error {
	id := inlineChildID(child)

	if isInlineDelete(child) {
		if id == "" {
			return fmt.Errorf("%s is required to delete a child", childInfo.PrimaryKey)
		}
		before := auditRecordSnapshot(r, st, id)
		if err := st.Delete(ctx, id); err != nil {
			return err
		}
		result.Deleted++
		p.recordAuditEntry(r, AuditEntry{
			Action: "delete", ModelName: childInfo.Name, RecordID: id,
			OldValue: auditValues(childInfo, before),
		})
		return nil
	}

	// The parent key is stamped, not taken from the payload: a nested child
	// that named another parent would be a write into a record the operator
	// was not editing.
	values := map[string]any{}
	for k, v := range child {
		if strings.EqualFold(k, inlineDeleteKey) {
			continue
		}
		values[k] = v
	}
	delete(values, "id")
	delete(values, "ID")
	delete(values, "Id")
	values[spec.Column] = parentValue(parent, parentID)

	if id != "" {
		before := auditRecordSnapshot(r, st, id)
		if err := st.Update(ctx, id, datasource.Record(values)); err != nil {
			return err
		}
		after := auditRecordSnapshot(r, st, id)
		result.Updated++
		p.recordAuditEntry(r, AuditEntry{
			Action: "update", ModelName: childInfo.Name, RecordID: id,
			OldValue: auditValues(childInfo, before), NewValue: auditValues(childInfo, after),
		})
		return nil
	}

	created, err := st.Create(ctx, datasource.Record(values))
	if err != nil {
		return err
	}
	result.Created++
	p.recordAuditEntry(r, AuditEntry{
		Action: "create", ModelName: childInfo.Name, RecordID: auditRecordID(childInfo, created),
		NewValue: auditValues(childInfo, created),
	})
	return nil
}

// parentValue renders the parent's key the way the child's column holds it.
// Ids are strings at the boundary (ADR-001 D1) and the adapters accept the
// text of a number for a numeric column, so the text is what is stamped.
func parentValue(parent datasource.ModelInfo, parentID string) any {
	_ = parent
	return parentID
}

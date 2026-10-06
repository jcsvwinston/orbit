// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"errors"
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
// A child is written with the scope its own form would write it with (OR-72):
// the child model's verb, the request's tenant, the operator's own rows under
// an #own grant and the fields they may write — and a child named by id is
// one of the record being edited, or it is not found. Before OR-72 only the
// verb was asked, and a child named by id was any row of the child model.
//
// What this deliberately does NOT do: pretend to be transactional. The
// datasource contract (ADR-001) has no transaction — a RecordStore writes one
// row — so the parent is written first and the children after it, each with
// its own result. What CAN be decided before anything is written is: every
// child is planned first (planInlines), and one the operator may not write
// refuses the whole save. A child the store then fails is reported by its
// position in the response instead of rolling back a parent that is already
// there, and the response says how many went in. A form that needs
// all-or-nothing for the store's own failures needs a transactional
// datasource first; saying so is better than implying it.

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

// inlinePlan is one child collection of a parent write, planned: each child
// has been asked what it may write before the parent or any child is.
type inlinePlan struct {
	spec     inlineSpec
	child    datasource.ModelInfo
	store    datasource.RecordStore
	children []inlineChild
}

// inlineChild is one planned child write.
type inlineChild struct {
	// index is the child's position in its collection, which is how a
	// refusal and a failure name it.
	index int
	// verb is "create", "update" or "delete".
	verb string
	// id is the child's primary key; empty for a create.
	id string
	// values is what a create or an update writes, already guarded.
	values map[string]any
	// parentKey is the payload key a create's parent key is stamped under
	// once the parent's own key is known.
	parentKey string
}

// inlineParent is how the children of one collection point at the record
// being edited: the key column, every key the backend resolves to it, and
// that record's key — empty while it is being created.
type inlineParent struct {
	column string
	keys   []string
	key    string
}

// inlineParentFor resolves spec's key on the child model. The keys are the
// ones the tenant guard knows a field by (fieldKeys plus the json key), so a
// spelling the backend honours is never one the guard does not know.
func (p *Panel) inlineParentFor(child datasource.ModelInfo, spec inlineSpec, parentKey string) inlineParent {
	ref := inlineParent{column: spec.Column, key: parentKey}
	for _, name := range []string{spec.Field, spec.Column} {
		if _, field, ok := dsResolveField(child, name); ok {
			ref.keys = fieldKeys(field, p.fieldJSONKey(child.Name, field))
			return ref
		}
	}
	ref.keys = fieldKeys(datasource.FieldInfo{Column: spec.Column, Name: spec.Field})
	return ref
}

// inlineParentKey is the key of the record being edited the way a child's
// column holds it: the record's own primary key when it carries one, else
// the id the request named. Ids are strings at the boundary (ADR-001 D1)
// and the adapters accept the text of a number for a numeric column, so the
// text is what is stamped and compared.
func inlineParentKey(mi datasource.ModelInfo, rec datasource.Record, id string) string {
	if key, ok := canonicalID(auditRecordID(mi, rec)); ok {
		return key
	}
	key, _ := canonicalID(id)
	return key
}

// planInlines asks every child of a nested payload what the child's own form
// would ask of it, BEFORE the parent or any child is written (OR-72): the
// child model's create, update or delete, the request's tenant, the
// operator's own rows under an #own grant and the fields they may write —
// one write scope per verb (requestWriteScope), the one the record form, the
// import and the fixture load ask. A child named by id must also be one of
// the record being edited; a record being created (parentKey "") has none.
//
// One child refused refuses the save whole, with the refusal naming it: the
// datasource has no transaction (ADR-001), so a refusal found after the
// parent was written would leave it saved and the form told "forbidden".
func (p *Panel) planInlines(c *router.Context, payloads map[string][]map[string]any, specs []inlineSpec, parentKey, databaseAlias string) ([]inlinePlan, error) {
	if len(payloads) == 0 {
		return nil, nil
	}
	ctx := c.Request.Context()
	plans := make([]inlinePlan, 0, len(payloads))
	planned := map[string]bool{}
	for _, spec := range specs {
		children, ok := payloads[spec.Model]
		// A model pointing at the parent twice has one collection, which
		// takeInlinePayloads gave to its first spec: it is planned once.
		if !ok || planned[spec.Model] {
			continue
		}
		planned[spec.Model] = true
		childInfo, ok := p.src.Get(spec.Model)
		if !ok {
			return nil, gferrors.NotFound("model", spec.Model)
		}
		if childInfo.ReadOnly {
			return nil, gferrors.Forbidden(spec.Model + " is read-only")
		}
		st, err := p.src.Store(childInfo.Name, databaseAlias)
		if err != nil {
			return nil, err
		}
		parent := p.inlineParentFor(childInfo, spec, parentKey)
		scopes := p.writeScopesFor(c, childInfo)
		plan := inlinePlan{spec: spec, child: childInfo, store: st}
		for i, rec := range children {
			child, err := planInlineChild(ctx, scopes, st, childInfo, parent, rec)
			if err != nil {
				return nil, inlineRefusal(spec, i, err)
			}
			child.index = i
			plan.children = append(plan.children, child)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// planInlineChild plans one child: delete, update or create.
func planInlineChild(ctx context.Context, scopes *writeScopes, st datasource.RecordStore, mi datasource.ModelInfo, parent inlineParent, rec map[string]any) (inlineChild, error) {
	id := inlineChildID(rec)

	if isInlineDelete(rec) {
		scope, err := scopes.get("delete")
		if err != nil {
			return inlineChild{}, err
		}
		// A deletion without a key is the payload's mistake, reported with
		// the child as it always was: there is no row to ask about.
		if id != "" {
			if err := parent.reaches(ctx, scope, st, mi, id); err != nil {
				return inlineChild{}, err
			}
		}
		return inlineChild{verb: "delete", id: id}, nil
	}

	values := inlineValues(rec)
	if id != "" {
		scope, err := scopes.get(fieldActionUpdate)
		if err != nil {
			return inlineChild{}, err
		}
		if _, err := parent.guardPayload(mi, values, false); err != nil {
			return inlineChild{}, err
		}
		if err := scope.guardPayload(mi, values); err != nil {
			return inlineChild{}, err
		}
		if err := parent.reaches(ctx, scope, st, mi, id); err != nil {
			return inlineChild{}, err
		}
		return inlineChild{verb: fieldActionUpdate, id: id, values: values}, nil
	}

	scope, err := scopes.get(fieldActionCreate)
	if err != nil {
		return inlineChild{}, err
	}
	// The parent key is in the payload before the scope's guard reads it,
	// as the tenant and the owner it stamps are: it is a field the create
	// writes, and one the operator may not write refuses the child.
	key, err := parent.guardPayload(mi, values, true)
	if err != nil {
		return inlineChild{}, err
	}
	if err := scope.guardPayload(mi, values); err != nil {
		return inlineChild{}, err
	}
	return inlineChild{verb: fieldActionCreate, values: values, parentKey: key}, nil
}

// inlineValues is a child payload without the keys that steer the write
// rather than say what it writes: the deletion mark and the id.
func inlineValues(rec map[string]any) map[string]any {
	values := make(map[string]any, len(rec))
	for k, v := range rec {
		if strings.EqualFold(k, inlineDeleteKey) {
			continue
		}
		values[k] = v
	}
	delete(values, "id")
	delete(values, "ID")
	delete(values, "Id")
	return values
}

// guardPayload holds a child payload to the record being edited. A payload
// naming another record under any key the backend resolves to the parent
// key — or the key twice — is refused, never moved and never quietly
// re-stamped: a form told it filed a child where it did not is the failure
// fieldRules.guardPayload refuses a field for. A create has the parent key
// stamped, under the key the payload named it by or the column; an update
// that does not name it leaves the row's own alone, which reaches confirms
// is this record. It returns the key a create is stamped under.
func (ref inlineParent) guardPayload(mi datasource.ModelInfo, values map[string]any, create bool) (string, error) {
	keys := matchingKeys(ref.keys, values)
	switch len(keys) {
	case 0:
		if !create {
			return "", nil
		}
		values[ref.column] = ref.key
		return ref.column, nil
	case 1:
		got, named := canonicalID(values[keys[0]])
		// A create may leave the key empty for the panel to fill; an update
		// may not empty it, which would file the child under no record.
		if (named || !create) && got != ref.key {
			return "", gferrors.BadRequest(fmt.Sprintf("%s field %s must name the record being edited, got %q: a child is written under the record it is edited with",
				mi.Name, ref.column, got))
		}
		values[keys[0]] = ref.key
		return keys[0], nil
	default:
		return "", gferrors.BadRequest(fmt.Sprintf("%s field %s appears more than once in the payload (%s)",
			mi.Name, ref.column, strings.Join(keys, ", ")))
	}
}

// reaches confirms that the child id names may be written with scope and is
// one of the record being edited. A row that does not exist, one of another
// tenant, of another owner under an #own grant and one of another record
// are all the same 404 — the answer the record endpoints give a row out of
// scope, so the payload learns nothing about rows it may not reach. The
// last rule is integrity, not a grant: a superuser is held to it too.
func (ref inlineParent) reaches(ctx context.Context, scope writeScope, st datasource.RecordStore, mi datasource.ModelInfo, id string) error {
	if ref.key == "" {
		// The record is being created: no row is its child yet.
		return gferrors.NotFound(mi.Name, id)
	}
	rec, err := st.Get(ctx, id)
	if err != nil {
		return err
	}
	reached, err := scope.reaches(ctx, st, mi, id, rec)
	if err == nil && reached {
		reached, err = columnScopeOwns(ctx, st, mi, id, ref.column, ref.keys, ref.key, rec)
	}
	if err != nil {
		return err
	}
	if !reached {
		return gferrors.NotFound(mi.Name, id)
	}
	return nil
}

// inlineRefusal names the child a refusal is about — its collection and its
// position in it — keeping the refusal's status.
func inlineRefusal(spec inlineSpec, index int, err error) error {
	var domErr *gferrors.DomainError
	if !errors.As(err, &domErr) {
		return err
	}
	named := *domErr
	named.Message = fmt.Sprintf("%s[%d]: %s", spec.Key, index, domErr.Message)
	return &named
}

// writeInlines writes the planned children of a parent write, after the
// parent: parentKey is the parent's key, which a create could not know while
// it was planned. Nothing here asks a permission — planInlines asked them
// all. A child the store fails is reported by position in its collection's
// result instead of raised: the parent is already written, and pretending
// otherwise would be the roll-back this cannot do.
func (p *Panel) writeInlines(r *http.Request, plans []inlinePlan, parentKey string) []inlineResult {
	if len(plans) == 0 {
		return nil
	}
	results := make([]inlineResult, 0, len(plans))
	for _, plan := range plans {
		result := inlineResult{Model: plan.spec.Model}
		for _, child := range plan.children {
			if err := p.writeInlineChild(r, plan, child, parentKey, &result); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s[%d]: %s", plan.spec.Key, child.index, err.Error()))
			}
		}
		results = append(results, result)
	}
	return results
}

// writeInlineChild applies one planned child.
func (p *Panel) writeInlineChild(r *http.Request, plan inlinePlan, child inlineChild, parentKey string, result *inlineResult) error {
	ctx := r.Context()
	st, mi := plan.store, plan.child

	switch child.verb {
	case "delete":
		if child.id == "" {
			return fmt.Errorf("%s is required to delete a child", mi.PrimaryKey)
		}
		before := auditRecordSnapshot(r, st, child.id)
		if err := st.Delete(ctx, child.id); err != nil {
			return err
		}
		result.Deleted++
		p.recordAuditEntry(r, AuditEntry{
			Action: "delete", ModelName: mi.Name, RecordID: child.id,
			OldValue: auditValues(mi, before),
		})
		return nil

	case fieldActionUpdate:
		before := auditRecordSnapshot(r, st, child.id)
		if err := st.Update(ctx, child.id, datasource.Record(child.values)); err != nil {
			return err
		}
		after := auditRecordSnapshot(r, st, child.id)
		result.Updated++
		p.recordAuditEntry(r, AuditEntry{
			Action: "update", ModelName: mi.Name, RecordID: child.id,
			OldValue: auditValues(mi, before), NewValue: auditValues(mi, after),
		})
		return nil
	}

	child.values[child.parentKey] = parentKey
	created, err := st.Create(ctx, datasource.Record(child.values))
	if err != nil {
		return err
	}
	result.Created++
	p.recordAuditEntry(r, AuditEntry{
		Action: "create", ModelName: mi.Name, RecordID: auditRecordID(mi, created),
		NewValue: auditValues(mi, created),
	})
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

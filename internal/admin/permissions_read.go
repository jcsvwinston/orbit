// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"net/url"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// readScope is what one operator may read of one model in one request: the
// rows (the tenant confinement and the #own one, as filters on the runtime
// column each names) and the fields (the columns the record views mask).
//
// Every surface that reads rows on an operator's behalf takes it from
// requestReadScope — the list, the per-model CSV export, the panel's export
// in every format, the fixture dump, a dashboard's records card and a
// relation lookup — so none of them can show more than the grid does. Before
// OR-66 each assembled its own, and the panel's export and the dump assembled
// only the tenant. What a query may name is the same scope's fields (OR-69,
// below).
type readScope struct {
	// confine maps a runtime column to the value every row read must carry.
	// Empty when the request is not confined.
	confine map[string]string
	// tenant is the tenant the rows are confined to, "" when they are not;
	// the audit entries name it.
	tenant string
	// fields is the operator's field policy over the model.
	fields fieldRules
}

// requestReadScope authorizes action on mi for c's operator and resolves what
// that grant reaches: a full grant every row, an #own grant the operator's
// own (or the 403 that says why it cannot be honoured), the request's tenant
// when it is confined to one, and the fields the operator's policies leave
// readable. The superuser and the open posture get a zero scope.
func (p *Panel) requestReadScope(c *router.Context, mi datasource.ModelInfo, action string) (readScope, error) {
	rowScope, err := p.authorizeRecordAction(c, mi, action)
	if err != nil {
		return readScope{}, err
	}
	s := readScope{fields: p.requestFieldRules(c.Request, mi)}
	if scope := p.requestTenantScope(c.Request, mi); scope.Enforced() {
		s.confineTo(scope.Column(), scope.Tenant)
		s.tenant = scope.Tenant
	}
	if rowScope.Enforced() {
		s.confineTo(rowScope.Column(), rowScope.Owner)
	}
	return s, nil
}

func (s *readScope) confineTo(column, value string) {
	if s.confine == nil {
		s.confine = map[string]string{}
	}
	s.confine[column] = value
}

// filters returns base with the scope's confinement laid over it. A filter
// the caller supplied on a confined column is overwritten, not merged: the
// scope is not negotiable. base is not modified, and nil comes back when
// there is nothing to filter by.
func (s readScope) filters(base map[string]string) map[string]string {
	if len(base) == 0 && len(s.confine) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(s.confine))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range s.confine {
		out[k] = v
	}
	return out
}

// readableFields lists the fields of mi an export may write for this scope:
// the ones the operator reads (fieldRules.readsField).
func (s readScope) readableFields(mi datasource.ModelInfo) []datasource.FieldInfo {
	out := make([]datasource.FieldInfo, 0, len(mi.Fields))
	for _, f := range mi.Fields {
		if s.fields.readsField(f) {
			out = append(out, f)
		}
	}
	return out
}

// What an operator may ASK about (OR-69).
//
// A field the operator may not read was masked out of every row, and then
// filtered by, sorted by and searched in on their behalf: with a deny on
// `owner`, ?owner=operator answered one row and ?owner=nobody none, which is
// the value, one guess at a time — and a sort pages the rows in the hidden
// field's order. A field the operator may not read is not one they can name
// in a query either, on any surface that reads rows for them: the list (its
// filters, its sort and its search), the panel's export (its filters), a
// relation lookup (its label and its search), a records card (its order) and
// the saved views listed. Each asks the predicate below, the one the columns
// of an export are chosen by, so what may be read and what may be asked
// cannot drift apart.

// readsField reports whether the operator reads field f: never one the panel
// excludes, always the primary key — a row whose key is hidden cannot be
// opened, exported or told apart from another, which is what mask keeps it
// for too — and otherwise what their field policies say of its column.
func (fr fieldRules) readsField(f datasource.FieldInfo) bool {
	if f.IsExcluded {
		return false
	}
	return f.IsPK || fr.readable(runtimeColumn(f.Column))
}

// queryModel is mi as this operator may name its fields in a query: each one
// they may not read is marked excluded, so the helpers that validate a filter
// (dsCollectFilters), a sort (dsSanitizeOrderBy) or a relation's label
// (optionLabelField) refuse it the way they refuse a field the panel never
// shows — with the answer a field the model does not have gets, so the
// refusal does not say the field exists either. mi is not modified, and comes
// back as it is when no field policy applies.
func (fr fieldRules) queryModel(mi datasource.ModelInfo) datasource.ModelInfo {
	if !fr.enforced() {
		return mi
	}
	fields := make([]datasource.FieldInfo, len(mi.Fields))
	for i, f := range mi.Fields {
		if !fr.readsField(f) {
			f.IsExcluded = true
		}
		fields[i] = f
	}
	mi.Fields = fields
	return mi
}

// searchesHidden reports whether ?search= would look in a field this
// operator does not read. The backend searches every field it marks
// searchable and datasource.Query has no way to say which, so the panel
// cannot narrow a search to the readable ones: one that would reach a hidden
// field is refused instead, since the rows it finds say what that field
// holds. An excluded field counts, for every operator: Nucleus searches a
// searchable field whether the panel shows it or not.
func (fr fieldRules) searchesHidden(mi datasource.ModelInfo) bool {
	for _, f := range mi.Fields {
		if f.IsSearch && !fr.readsField(f) {
			return true
		}
	}
	return false
}

// searchable reports whether ?search= is answered for this operator: the
// model has a field to search in, and no field the search would reach is
// hidden from them. The schema carries it, so the grid does not offer a box
// the list refuses.
func (fr fieldRules) searchable(mi datasource.ModelInfo) bool {
	return modelSearchable(mi) && !fr.searchesHidden(mi)
}

// hiddenSearchError is the 400 a search that would reach a hidden field
// gets. It names no field: the field's name is what the schema withholds.
func hiddenSearchError(mi datasource.ModelInfo) error {
	return gferrors.BadRequest(fmt.Sprintf("search is not available for %s: it would look in fields hidden from this operator", mi.Name))
}

// namesHiddenField reports whether a list query string — a saved view's —
// filters or sorts by a field this operator does not read. It reads the
// query the way the list does (dsCollectFilters, dsSanitizeOrderBy) and
// answers only that: a query wrong for any other reason is the list's to
// refuse.
func (fr fieldRules) namesHiddenField(mi datasource.ModelInfo, values url.Values) bool {
	hidden := func(key string) bool {
		_, f, ok := dsResolveField(mi, key)
		return ok && !fr.readsField(f)
	}
	for key := range values {
		if listReservedParams[key] {
			continue
		}
		if hidden(key) {
			return true
		}
		if field, _, ok := dsSplitFilterKey(mi, key); ok && hidden(field) {
			return true
		}
	}
	for _, clause := range strings.Split(values.Get("order_by"), ",") {
		if words := strings.Fields(clause); len(words) > 0 && hidden(words[0]) {
			return true
		}
	}
	return false
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// readScope is what one operator may read of one model in one request: the
// rows (the tenant confinement and the #own one, as filters on the runtime
// column each names) and the fields (the columns the record views mask).
//
// Every surface that reads rows on an operator's behalf takes it from
// requestReadScope — the list, the per-model CSV export, the panel's export
// in every format, the fixture dump and a dashboard's records card — so none
// of them can show more than the grid does. Before OR-66 each assembled its
// own, and the panel's export and the dump assembled only the tenant.
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
// every field the panel shows (not excluded) that the operator may read. The
// primary key always stays — a row whose key is hidden cannot be opened,
// re-imported or told apart from another, which is what fieldRules.mask
// keeps it for too.
func (s readScope) readableFields(mi datasource.ModelInfo) []datasource.FieldInfo {
	out := make([]datasource.FieldInfo, 0, len(mi.Fields))
	for _, f := range mi.Fields {
		if f.IsExcluded {
			continue
		}
		if !f.IsPK && !s.fields.readable(runtimeColumn(f.Column)) {
			continue
		}
		out = append(out, f)
	}
	return out
}

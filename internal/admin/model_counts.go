// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"net/http"
	"slices"

	"github.com/jcsvwinston/nucleus/pkg/auth"

	"github.com/jcsvwinston/orbit/datasource"
)

// countScope is what GET /api/models counts of one model for one operator:
// nothing, when the operator may not list the model (the count is left
// unknown, as the light mode leaves every count); every row; or the rows the
// operator's list reaches — the request's tenant and, under an #own grant,
// their own.
//
// Before OR-70 the model list counted every row of every model for anyone
// holding list_models: a tenant's operator read how many rows the other
// tenants had, an #own operator the size of the whole table, and an operator
// with no list on a model how many rows it held. The count a model card
// shows is now the total its list would answer the same operator.
type countScope struct {
	listed  bool
	filters map[string]string
}

func (s *countScope) confine(column, value string) {
	if s.filters == nil {
		s.filters = map[string]string{}
	}
	s.filters[column] = value
}

// modelCountScope resolves the count scope of mi for operator from the
// capabilities the model list already computed for them — the same answer
// authorizeRecordAction gives the list (a full grant, an #own grant, or
// neither), without authenticating once more per model.
func (p *Panel) modelCountScope(r *http.Request, mi datasource.ModelInfo, operator *auth.User, caps modelCapabilities) countScope {
	if !caps.Permissions["list"] {
		return countScope{}
	}
	scope := countScope{listed: true}
	if ts := p.requestTenantScope(r, mi); ts.Enforced() {
		scope.confine(ts.Column(), ts.Tenant)
	}
	if slices.Contains(caps.RowScope, "list") {
		own, err := p.ownerScopeFor(mi, operator)
		if err != nil {
			// An #own grant the panel cannot honour is refused on the list
			// itself (ownerScopeFor); the count is as unknown as the list
			// is unreachable.
			return countScope{}
		}
		scope.confine(own.Column(), own.Owner)
	}
	return scope
}

// count asks st how many rows the scope reaches. It returns the store's own
// count when the scope reaches every row, and otherwise the total of a list
// confined by the scope's filters — the filters the list applies
// (readScope.filters), so the number here is the number the grid shows. known
// is false when the operator may not list the model: the table's presence is
// still probed, so the model keeps its database attribution, but no count
// comes back for it.
func (s countScope) count(ctx context.Context, st datasource.RecordStore) (cr datasource.CountResult, known bool, err error) {
	if !s.listed {
		return datasource.CountResult{Present: st.TableExists(ctx)}, false, nil
	}
	if len(s.filters) == 0 {
		cr, err = st.Count(ctx)
		return cr, err == nil, err
	}
	if !st.TableExists(ctx) {
		return datasource.CountResult{}, false, nil
	}
	// ExactTotal is what the list asks for its pager: without it a source
	// may answer Total -1 and leave the count unknown.
	page, err := st.List(ctx, datasource.Query{Page: 1, PageSize: 1, Filters: s.filters, ExactTotal: true})
	if err != nil {
		return datasource.CountResult{}, false, err
	}
	if page.Total < 0 {
		return datasource.CountResult{Present: true}, false, nil
	}
	return datasource.CountResult{Count: page.Total, IsEstimated: page.IsEstimated, Present: true}, true, nil
}

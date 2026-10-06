// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"

	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// writeScope is what one operator may write of one model with one verb —
// create or update — in one request: the tenant the row must be in, the
// #own confinement of the verb's grant, and the fields the operator's
// policies leave writable for that verb.
//
// Every surface that writes rows on an operator's behalf takes it from
// requestWriteScope — the record form's create and update, the children
// that form writes with its record (inlines.go), the import (validate and
// execute) and the fixture load — so an import or a child cannot write what
// the form would refuse. Before OR-67 the import and the load were granted
// by import_data on admin:* alone and wrote any model, by create or by
// update, outside the operator's own rows and through a field kept from
// them; before OR-72 a child was granted by the child model's verb alone.
// It is the write twin of readScope (permissions_read.go).
type writeScope struct {
	// action is the verb the scope was granted for: fieldActionCreate or
	// fieldActionUpdate — or "delete" for a child the record form deletes,
	// whose scope says which rows it reaches and guards no payload.
	action string
	// tenant is the request's tenant confinement; zero when it has none.
	tenant tenantScope
	// owner is the #own confinement of the verb's grant; zero for a full
	// grant, a superuser and the open posture.
	owner ownerScope
	// fields is the operator's field policy over the model.
	fields fieldRules
}

// requestWriteScope authorizes action ("create", "update" or "delete") on
// mi for c's operator and resolves what that grant reaches: a full grant
// every row, an #own grant the operator's own (or the 403 that says why it
// cannot be honoured), the request's tenant when it is confined to one, and
// the fields the operator's policies leave writable. The superuser and the
// open posture get a scope that confines nothing but the tenant, as the
// record form always did.
func (p *Panel) requestWriteScope(c *router.Context, mi datasource.ModelInfo, action string) (writeScope, error) {
	owner, err := p.authorizeRecordAction(c, mi, action)
	if err != nil {
		return writeScope{}, err
	}
	return writeScope{
		action: action,
		tenant: p.requestTenantScope(c.Request, mi),
		owner:  owner,
		fields: p.requestFieldRules(c.Request, mi),
	}, nil
}

// guardPayload confines a write payload to the scope, in place: a payload
// naming another tenant or another owner is refused, one naming neither has
// them stamped on a create (an update leaves the row's own alone), and a
// field this operator may not write is refused by name — never dropped, as
// fieldRules.guardPayload explains.
func (s writeScope) guardPayload(mi datasource.ModelInfo, data map[string]any) error {
	stamp := s.action == fieldActionCreate
	if s.tenant.Enforced() {
		if err := s.tenant.guardPayload(data, stamp); err != nil {
			return err
		}
	}
	if s.owner.Enforced() {
		if err := s.owner.guardPayload(data, stamp); err != nil {
			return err
		}
	}
	return s.fields.guardPayload(mi, data, s.action)
}

// confined reports whether the scope reaches only some rows, so an update
// has to confirm the row it names is one of them.
func (s writeScope) confined() bool { return s.tenant.Enforced() || s.owner.Enforced() }

// reaches reports whether the row id names — rec being the record st
// returned for it — is one this scope may write: of the request's tenant
// and, under an #own grant, the operator's. A row it does not reach is
// answered as not found by the caller, the record endpoints' answer for a
// row of another tenant or another owner.
func (s writeScope) reaches(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id string, rec datasource.Record) (bool, error) {
	if s.tenant.Enforced() {
		owned, err := s.tenant.owns(ctx, st, mi, id, rec)
		if err != nil || !owned {
			return false, err
		}
	}
	if s.owner.Enforced() {
		return s.owner.owns(ctx, st, mi, id, rec)
	}
	return true, nil
}

// writeScopes resolves an operator's write scope over one model once per
// verb: an import asks it of every row, and the answer does not change
// within a request.
type writeScopes struct {
	panel  *Panel
	c      *router.Context
	mi     datasource.ModelInfo
	byVerb map[string]writeScopeAnswer
}

type writeScopeAnswer struct {
	scope writeScope
	err   error
}

func (p *Panel) writeScopesFor(c *router.Context, mi datasource.ModelInfo) *writeScopes {
	return &writeScopes{panel: p, c: c, mi: mi, byVerb: map[string]writeScopeAnswer{}}
}

func (w *writeScopes) get(action string) (writeScope, error) {
	if a, ok := w.byVerb[action]; ok {
		return a.scope, a.err
	}
	scope, err := w.panel.requestWriteScope(w.c, w.mi, action)
	w.byVerb[action] = writeScopeAnswer{scope: scope, err: err}
	return scope, err
}

// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"context"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// Per-row permissions: "this operator edits their own records".
//
// The policy object grows a qualifier instead of a column: a grant on
// admin:<Model> is the full model, as it always was, and a grant on
// admin:<Model>#own is the same verb confined to the rows that belong to the
// operator:
//
//	(editors, admin:Post,      list)    every post
//	(authors, admin:Post#own,  list)    the rows whose owner column names them
//
// Which column says who owns a row is the application's answer, not a guess:
// PanelConfig.RowOwnerFields maps a model to it, with "*" as a default for
// every model that has that column. A #own grant on a model with no owner
// column is REFUSED (403) rather than served in full — an ownership rule that
// silently degrades to "everything" is the failure mode this exists to
// prevent.
//
// The confinement rides on the same machinery as the tenant one: a filter on
// list, a membership check on the row endpoints (another operator's row is
// reported as not found, so ids are not disclosed), and a stamp on create.
const ownScopeSuffix = "#own"

// rowOwnerSubject picks which name of the operator the owner column holds.
const (
	rowOwnerSubjectUsername = "username"
	rowOwnerSubjectID       = "id"
)

// ownerScope is the row confinement of one request for one model: the owner
// column, the operator it is confined to, and the keys the backend emits or
// accepts for that column. A zero scope (Enforced() false) sees every row.
type ownerScope struct {
	Field datasource.FieldInfo
	Owner string
	Keys  []string
}

// Enforced reports whether the scope confines the request.
func (s ownerScope) Enforced() bool { return s.Owner != "" && s.Field.Column != "" }

// Column is the runtime column name the list filter uses.
func (s ownerScope) Column() string { return runtimeColumn(s.Field.Column) }

// owns reports whether the row id names belongs to the scope's operator.
func (s ownerScope) owns(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id string, rec datasource.Record) (bool, error) {
	return columnScopeOwns(ctx, st, mi, id, s.Column(), s.Keys, s.Owner, rec)
}

// guardPayload confines a write payload to the scope's operator: a payload
// naming another owner is refused, one naming none has the owner stamped on
// create. An update leaves the column alone — the row already belongs to
// somebody, and the membership check has already confirmed it is this
// operator.
func (s ownerScope) guardPayload(data map[string]any, stamp bool) error {
	keys := matchingKeys(s.Keys, data)
	switch len(keys) {
	case 0:
		if stamp {
			data[s.Field.Column] = s.Owner
		}
		return nil
	case 1:
		got, _ := canonicalTenant(data[keys[0]])
		if got != s.Owner {
			return gferrors.BadRequest("owner field " + s.Field.Column + " must be " + s.Owner +
				" in this request, got " + got + " (this operator is granted " + ownScopeSuffix + " rows only)")
		}
		data[keys[0]] = s.Owner
		return nil
	default:
		return gferrors.BadRequest("owner field " + s.Field.Column + " appears more than once in the payload (" +
			strings.Join(keys, ", ") + ")")
	}
}

// rowOwnerField returns the column that says who owns a row of modelName:
// the model's own entry in RowOwnerFields, or the "*" default. Empty when
// the application declared none.
func (p *Panel) rowOwnerField(modelName string) string {
	if len(p.config.RowOwnerFields) == 0 {
		return ""
	}
	if column, ok := p.config.RowOwnerFields[modelName]; ok {
		return strings.TrimSpace(column)
	}
	return strings.TrimSpace(p.config.RowOwnerFields["*"])
}

// rowOwnerValue is the name of user the owner column is expected to hold.
func (p *Panel) rowOwnerValue(user *auth.User) string {
	if user == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(p.config.RowOwnerSubject), rowOwnerSubjectID) {
		return user.ID
	}
	return user.Username
}

// ownerScopeFor builds the confinement of user over mi, or the 403 that says
// why it cannot: an ownership grant the panel cannot honour is refused, never
// widened.
func (p *Panel) ownerScopeFor(mi datasource.ModelInfo, user *auth.User) (ownerScope, error) {
	column := p.rowOwnerField(mi.Name)
	if column == "" {
		return ownerScope{}, ownershipUnavailable(mi.Name,
			"no owner column is configured for it (PanelConfig.RowOwnerFields)")
	}
	_, field, ok := dsResolveField(mi, column)
	if !ok {
		return ownerScope{}, ownershipUnavailable(mi.Name,
			"its configured owner column "+column+" is not a field of the model")
	}
	owner := p.rowOwnerValue(user)
	if owner == "" {
		return ownerScope{}, ownershipUnavailable(mi.Name, "this operator has no name to own rows by")
	}
	return ownerScope{Field: field, Owner: owner, Keys: fieldKeys(field, p.fieldJSONKey(mi.Name, field))}, nil
}

// ownershipUnavailable is the 403 a #own grant gets when the panel cannot
// resolve what ownership means for that model.
func ownershipUnavailable(modelName, why string) error {
	return &gferrors.DomainError{
		Code:       "PERMISSION_DENIED",
		Message:    "not allowed to work on " + modelName + " rows: the grant is for own rows and " + why,
		StatusCode: http.StatusForbidden,
	}
}

// authorizeRecordAction is authorizeAction for the record surfaces, and it
// answers one more question: WHICH rows. A full grant on the model answers
// "every row" with a zero scope; a #own grant answers "the operator's own"
// with an enforced one, and the handler confines itself to it.
//
// Model-level behaviour is unchanged: the same three subjects, the same
// superuser bypass, the same fallback to the auth provider when no enforcer
// is configured.
func (p *Panel) authorizeRecordAction(c *router.Context, mi datasource.ModelInfo, action string) (ownerScope, error) {
	if p.config.Auth == nil {
		return ownerScope{}, nil
	}
	user, err := p.authenticatedUser(c.Request)
	if err != nil {
		return ownerScope{}, p.authErrorToDomain(err)
	}
	if p.rbac == nil {
		if !p.config.Auth.Authorize(user, mi.Name, action) {
			return ownerScope{}, authDeniedDomain(mi.Name, action)
		}
		return ownerScope{}, nil
	}
	if user != nil && user.IsSuperuser {
		return ownerScope{}, nil
	}
	subjects := subjectsOf(user)
	resource := "admin:" + mi.Name
	if p.rbacCan(subjects, resource, action) {
		return ownerScope{}, nil
	}
	if p.rbacCan(subjects, resource+ownScopeSuffix, action) {
		return p.ownerScopeFor(mi, user)
	}
	return ownerScope{}, authDeniedDomain(mi.Name, action)
}

// columnScopeOwns reports whether the row id names — rec being the record st
// returned for it — carries want in the scoped column.
//
// A record that carries the column is compared in place. One that carries it
// under none of the keys (a field hidden from JSON has no key in the record
// at all) is confirmed through the store: a list filtered by the column AND
// the primary key answers the row only when it is the scope's. The row that
// list returns must name id as its primary key — the Nucleus backend drops a
// filter column it cannot resolve instead of refusing it, and a list confined
// by the scope column alone would answer its first row for any id.
func columnScopeOwns(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id, column string, keys []string, want string, rec datasource.Record) (bool, error) {
	if v, ok := recordValueByKeys(rec, keys); ok {
		got, _ := canonicalTenant(v)
		return got == want, nil
	}
	wantID, ok := canonicalID(id)
	if !ok {
		return false, nil
	}
	pkColumn, _, ok := dsResolveField(mi, mi.PrimaryKey)
	if !ok {
		return false, nil
	}
	page, err := st.List(ctx, datasource.Query{
		Page:     1,
		PageSize: 1,
		Filters:  map[string]string{column: want, pkColumn: wantID},
	})
	if err != nil {
		return false, err
	}
	for _, item := range page.Items {
		if got, ok := canonicalID(recordPKValue(item, mi)); ok && got == wantID {
			return true, nil
		}
	}
	return false, nil
}

// scopedOwnedRecord loads id and confirms it belongs to the request's
// operator. Another operator's row is reported as not found — the same answer
// as a row that does not exist.
func scopedOwnedRecord(ctx context.Context, st datasource.RecordStore, mi datasource.ModelInfo, id string, scope ownerScope) error {
	if !scope.Enforced() {
		return nil
	}
	rec, err := st.Get(ctx, id)
	if err != nil {
		return err
	}
	owned, err := scope.owns(ctx, st, mi, id, rec)
	if err != nil {
		return err
	}
	if !owned {
		return gferrors.NotFound(mi.Name, id)
	}
	return nil
}

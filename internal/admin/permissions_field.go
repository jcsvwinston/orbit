// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"net/http"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"

	"github.com/jcsvwinston/orbit/datasource"
)

// Per-field permissions, added to the policy vocabulary without changing it.
//
// Until now the object of a policy named a MODEL — (subject, admin:Note,
// update) — so "this editor may fix a typo in the title but may not touch the
// price" had nowhere to go. The object now also names a FIELD:
//
//	admin:<Model>.<field>   deny     the field is neither read nor written
//	admin:<Model>.<field>   read     read allow-list: only the named fields are emitted
//	admin:<Model>.<field>   create   write allow-list for creates
//	admin:<Model>.<field>   update   write allow-list for updates
//	admin:<Model>.<field>   write    both of the two above
//
// The two shapes are deliberate, because admins need both: a DENY names the
// exception ("everything except the price"), an ALLOW-LIST names the whole
// permitted set ("the title, and nothing else"). An allow-list only applies
// when the subject holds at least one field policy of that action for the
// model — a subject with none keeps the model-level grant it always had, so
// nothing that worked before changes.
//
// What this does NOT do, on purpose: it does not narrow the model-level
// grant. A subject who cannot update Note at all is refused before any field
// is looked at; a field policy widens nothing.
const (
	fieldActionDeny   = "deny"
	fieldActionRead   = "read"
	fieldActionCreate = "create"
	fieldActionUpdate = "update"
	fieldActionWrite  = "write"
)

// fieldRules is the per-field permission of one subject over one model,
// resolved for one request. The zero value enforces nothing, which is what
// every posture that has no RBAC enforcer — and every superuser — gets.
type fieldRules struct {
	// denied holds runtime columns the subject may neither read nor write.
	denied map[string]bool
	// allow holds, per action (read/create/update), the allow-list of runtime
	// columns. A nil entry means "no allow-list for this action": every
	// column the model-level grant covers stays reachable.
	allow map[string]map[string]bool
}

// enforced reports whether any field policy applies to this request.
func (fr fieldRules) enforced() bool { return len(fr.denied) > 0 || len(fr.allow) > 0 }

// readable reports whether the subject may see runtime column col.
func (fr fieldRules) readable(col string) bool {
	if fr.denied[col] {
		return false
	}
	if list, ok := fr.allow[fieldActionRead]; ok {
		return list[col]
	}
	return true
}

// writable reports whether the subject may write runtime column col with
// action ("create" or "update").
func (fr fieldRules) writable(action, col string) bool {
	if fr.denied[col] {
		return false
	}
	if list, ok := fr.allow[action]; ok {
		return list[col]
	}
	return true
}

// guardPayload refuses a write that touches a field this subject may not
// write, naming every offending field. It is a 403 and not a silent drop:
// a form that believes it saved a price it did not save is worse than one
// that is told it may not.
//
// A payload key that resolves to no field at all is left alone — the backends
// already decide what to do with an unknown key, and a guard that refused it
// here would change what a full-access operator can send.
func (fr fieldRules) guardPayload(mi datasource.ModelInfo, data map[string]any, action string) error {
	if !fr.enforced() || len(data) == 0 {
		return nil
	}
	var refused []string
	for key := range data {
		col, _, ok := dsResolveField(mi, key)
		if !ok {
			continue
		}
		if !fr.writable(action, col) {
			refused = append(refused, col)
		}
	}
	if len(refused) == 0 {
		return nil
	}
	sort.Strings(refused)
	return fieldDeniedError(mi.Name, action, refused)
}

// mask removes from a record every field the subject may not read. The
// primary key stays: a row whose key is hidden cannot be opened, exported or
// audited, which turns a field permission into a broken grid.
func (fr fieldRules) mask(mi datasource.ModelInfo, rec datasource.Record) datasource.Record {
	if !fr.enforced() || rec == nil {
		return rec
	}
	for key := range rec {
		col, f, ok := dsResolveField(mi, key)
		if !ok || f.IsPK {
			continue
		}
		if !fr.readable(col) {
			delete(rec, key)
		}
	}
	return rec
}

// maskValues masks a plain value map — an audit entry's before/after side,
// which is keyed by the same columns a record is. It returns a copy: the
// entry it came from may be a shared one (the in-memory ring hands out
// values, but a future store may not), and masking in place would edit the
// trail itself.
func (fr fieldRules) maskValues(mi datasource.ModelInfo, values map[string]any) map[string]any {
	if !fr.enforced() || len(values) == 0 {
		return values
	}
	out := make(map[string]any, len(values))
	for key, v := range values {
		col, f, ok := dsResolveField(mi, key)
		if ok && !f.IsPK && !fr.readable(col) {
			continue
		}
		out[key] = v
	}
	return out
}

// maskAll masks every record of a page in place.
func (fr fieldRules) maskAll(mi datasource.ModelInfo, items []datasource.Record) {
	if !fr.enforced() {
		return
	}
	for _, rec := range items {
		fr.mask(mi, rec)
	}
}

// fieldDeniedError is the 403 a write gets when it names a field this
// operator may not write. It names the fields, because a form that is told
// only "forbidden" cannot tell the person which input to undo.
func fieldDeniedError(modelName, action string, columns []string) error {
	return &gferrors.DomainError{
		Code:       "PERMISSION_DENIED",
		Message:    "not allowed to " + action + " " + modelName + " field(s): " + strings.Join(columns, ", "),
		StatusCode: http.StatusForbidden,
	}
}

// requestFieldRules resolves the field policies that apply to r's operator
// over model mi.
//
// It reads the policy set once and asks the enforcer about the policies it
// found, rather than asking about every field: a question per policy resolves
// role inheritance and deny-override through Casbin itself, and a panel with
// no field policy at all pays one GetPolicy and nothing else.
func (p *Panel) requestFieldRules(r *http.Request, mi datasource.ModelInfo) fieldRules {
	if p.config.Auth == nil || p.rbac == nil {
		return fieldRules{}
	}
	user, err := p.authenticatedUser(r)
	if err != nil || user == nil || user.IsSuperuser {
		return fieldRules{}
	}
	policies, err := p.rbac.GetPolicy()
	if err != nil || len(policies) == 0 {
		return fieldRules{}
	}

	prefix := "admin:" + mi.Name + "."
	subjects := subjectsOf(user)
	rules := fieldRules{}
	for _, pol := range policies {
		if len(pol) < 3 {
			continue
		}
		obj, act := pol[1], strings.ToLower(strings.TrimSpace(pol[2]))
		if !strings.HasPrefix(obj, prefix) {
			continue
		}
		col, _, ok := dsResolveField(mi, strings.TrimPrefix(obj, prefix))
		if !ok {
			// A policy naming a field the model does not have restricts
			// nothing. It is not an error either: a model loses a column
			// long before anybody revisits the policies written about it.
			continue
		}
		if !p.rbacCan(subjects, obj, pol[2]) {
			continue
		}
		switch act {
		case fieldActionDeny:
			if rules.denied == nil {
				rules.denied = map[string]bool{}
			}
			rules.denied[col] = true
		case fieldActionRead, fieldActionCreate, fieldActionUpdate:
			rules.addAllow(act, col)
		case fieldActionWrite:
			rules.addAllow(fieldActionCreate, col)
			rules.addAllow(fieldActionUpdate, col)
		}
	}
	return rules
}

func (fr *fieldRules) addAllow(action, col string) {
	if fr.allow == nil {
		fr.allow = map[string]map[string]bool{}
	}
	if fr.allow[action] == nil {
		fr.allow[action] = map[string]bool{}
	}
	fr.allow[action][col] = true
}

// subjectsOf lists the names a policy can address this operator by — the
// same three authorizeAction consults, in the same order.
func subjectsOf(user *auth.User) []string {
	if user == nil {
		return nil
	}
	subjects := make([]string, 0, 3)
	for _, s := range []string{user.ID, user.Role, user.Username} {
		if s != "" {
			subjects = append(subjects, s)
		}
	}
	return subjects
}

// rbacCan reports whether any of subjects holds (obj, act).
func (p *Panel) rbacCan(subjects []string, obj, act string) bool {
	if p.rbac == nil {
		return false
	}
	for _, sub := range subjects {
		if p.rbac.Can(sub, obj, act) {
			return true
		}
	}
	return false
}

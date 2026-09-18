// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"net/http"
	"sort"

	"github.com/jcsvwinston/nucleus/pkg/auth"

	"github.com/jcsvwinston/orbit/datasource"
)

// Capability hints: what a screen is told about what its operator may do.
//
// Until now a payload said nothing about permission, so a UI could only find
// out by acting and being refused — every screen drew every button, and a
// person learned which ones were not theirs by pressing them. The model list
// and the schema now carry the verbs this operator holds on that model, and
// the schema carries them per field too, so a form can disable an input
// instead of failing a save.
//
// The hints are a RENDERING aid and nothing more: every one of them is the
// answer the enforcer gives, and the enforcer is asked again on the request
// that follows. A client that ignores them is refused exactly as before.

// recordActions are the verbs a model screen can hold. They are the actions
// the record handlers authorize, so a hint is never a verb no handler checks.
var recordActions = []string{
	"list", "retrieve", "create", "update", "delete",
	"export_csv", "bulk_delete", "bulk_export",
}

// modelCapabilities is what one operator may do with one model.
type modelCapabilities struct {
	// Permissions is action -> whether this operator holds it.
	Permissions map[string]bool `json:"permissions"`
	// RowScope lists the actions this operator holds only over their OWN
	// rows (an admin:<Model>#own grant). Empty when nothing is confined.
	RowScope []string `json:"row_scope,omitempty"`

	// The three the UI asks about most, spelled out so a template does not
	// have to index a map.
	CanCreate bool `json:"can_create"`
	CanUpdate bool `json:"can_update"`
	CanDelete bool `json:"can_delete"`
}

// capabilitiesFor resolves what r's operator may do with mi.
//
// The open posture (no auth provider) and a superuser hold everything; a
// panel with an auth provider but no enforcer asks the provider, which is the
// same question its handlers ask.
func (p *Panel) capabilitiesFor(r *http.Request, mi datasource.ModelInfo) modelCapabilities {
	if p.config.Auth == nil {
		return p.capabilitiesForUser(nil, mi)
	}
	user, err := p.authenticatedUser(r)
	if err != nil {
		user = nil
	}
	return p.capabilitiesForUser(user, mi)
}

// capabilitiesForUser is capabilitiesFor with the operator already resolved,
// so the model list — which asks the same question once per model —
// authenticates once instead of once per row.
// verbsFor lists every action verb that can be held on a model: the record
// actions the panel enforces, then the ones this application declared.
func (p *Panel) verbsFor(model string) []string {
	declared := p.actionsForModel(model)
	if len(declared) == 0 {
		return recordActions
	}
	verbs := make([]string, 0, len(recordActions)+len(declared))
	verbs = append(verbs, recordActions...)
	for _, action := range declared {
		verbs = append(verbs, action.Name)
	}
	return verbs
}

func (p *Panel) capabilitiesForUser(user *auth.User, mi datasource.ModelInfo) modelCapabilities {
	// The verbs of a model are the panel's own plus whatever actions this
	// application declared for it: an application verb is authorized like
	// any other, so it belongs in the same hint map — that is what lets a
	// grid know whether to draw the button at all.
	verbs := p.verbsFor(mi.Name)
	caps := modelCapabilities{Permissions: make(map[string]bool, len(verbs))}
	grantAll := func() modelCapabilities {
		for _, action := range verbs {
			caps.Permissions[action] = true
		}
		caps.CanCreate, caps.CanUpdate, caps.CanDelete = true, true, true
		return caps
	}
	if p.config.Auth == nil {
		return grantAll()
	}
	if user == nil {
		for _, action := range verbs {
			caps.Permissions[action] = false
		}
		return caps
	}
	if user.IsSuperuser {
		return grantAll()
	}
	if p.rbac == nil {
		for _, action := range verbs {
			caps.Permissions[action] = p.config.Auth.Authorize(user, mi.Name, action)
		}
		caps.fill()
		return caps
	}

	subjects := subjectsOf(user)
	resource := "admin:" + mi.Name
	for _, action := range verbs {
		full := p.rbacCan(subjects, resource, action)
		own := !full && p.rbacCan(subjects, resource+ownScopeSuffix, action)
		caps.Permissions[action] = full || own
		if own {
			caps.RowScope = append(caps.RowScope, action)
		}
	}
	sort.Strings(caps.RowScope)
	caps.fill()
	return caps
}

func (c *modelCapabilities) fill() {
	c.CanCreate = c.Permissions["create"]
	c.CanUpdate = c.Permissions["update"]
	c.CanDelete = c.Permissions["delete"]
}

// fieldCapabilities is what one operator may do with one field.
type fieldCapabilities struct {
	CanRead bool `json:"can_read"`
	CanEdit bool `json:"can_edit"`
}

// fieldCapabilitiesFor resolves the per-field hints for one column.
//
// can_edit answers the FIELD question only — "may this operator write this
// column in an update" — and not the model one. The model verdict travels
// beside it as can_update, and a screen that may not update at all never
// offers the form: folding the two together would have drawn an empty create
// form for an operator who may create but not update.
func (fr fieldRules) fieldCapabilitiesFor(col string) fieldCapabilities {
	return fieldCapabilities{
		CanRead: fr.readable(col),
		CanEdit: fr.writable(fieldActionUpdate, col),
	}
}

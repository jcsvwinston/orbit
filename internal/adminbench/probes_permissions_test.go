// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"net/http"
	"strings"
	"testing"
)

// probeModelPermissions is the control the whole RBAC surface rests on: a
// second operator, who is not a superuser, is refused until a policy names
// what they may do, and served afterwards.
func probeModelPermissions(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-model")

	before := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note", nil)
	if before.code != http.StatusForbidden {
		t.Logf("an operator with no policy read the model anyway (%d): %s", before.code, before.text())
		return absent
	}

	e.grant(t, op.username, "admin:Note", "list")
	after := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note", nil)
	if after.code != http.StatusOK {
		t.Logf("the granted operator is still refused (%d): %s", after.code, after.text())
		return partial
	}

	// The grant was for one model and one verb; the neighbouring model stays
	// closed, or the permission is not per-model at all.
	other := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Author", nil)
	if other.code == http.StatusOK {
		t.Logf("granting admin:Note also opened Author: the check is not per model")
		return partial
	}
	return present
}

// probeFieldPermissions grants an operator a policy that names a FIELD and
// then writes that field. A per-field permission model would refuse; this
// one does not know what a field is.
func probeFieldPermissions(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-field")
	e.grant(t, op.username, "admin:Note", "list")
	e.grant(t, op.username, "admin:Note", "create")
	e.grant(t, op.username, "admin:Note", "update")

	id := e.createNote(t, map[string]any{"title": "field-perms", "status": "draft"})
	// Nothing grants this operator the title field; the only policy that
	// could say so is the one below, in the grammar a field policy would use.
	e.grant(t, op.username, "admin:Note.title", "deny")

	r := e.asOperator(t, op, http.MethodPut, "/admin/api/models/Note/"+id,
		map[string]any{"title": "rewritten by an operator who should not"})
	if r.code == http.StatusForbidden {
		return present
	}
	schema := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note/schema", nil)
	if strings.Contains(schema.raw(), `"can_edit"`) || strings.Contains(schema.raw(), `"permissions"`) {
		t.Logf("the schema carries per-field permission hints: %s", schema.text())
		return partial
	}
	t.Logf("a field-level policy changes nothing: the write answered %d", r.code)
	return absent
}

// probeRowPermissions asks for "this operator may edit their own rows", the
// permission every editorial admin needs. The policy vocabulary is
// (subject, object, action) with the object naming a MODEL, so there is
// nowhere to put the row.
func probeRowPermissions(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-row")
	e.grant(t, op.username, "admin:Note", "list")
	mine := e.createNote(t, map[string]any{"title": "row-mine", "status": "rows"})
	theirs := e.createNote(t, map[string]any{"title": "row-theirs", "status": "rows"})

	list := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note?status=rows", nil)
	if list.code != http.StatusOK {
		t.Logf("list answered %d: %s", list.code, list.text())
		return absent
	}
	body := list.text()
	if strings.Contains(body, "row-mine") && !strings.Contains(body, "row-theirs") {
		return present
	}
	t.Logf("both rows (%s, %s) are visible to an operator granted the model: no row scope exists", mine, theirs)
	return absent
}

// probeAdminUserManagement is OR-4, the oldest P1 in the registry: an
// operator who needs a colleague to have access has to leave the panel.
func probeAdminUserManagement(t *testing.T, e *env) verdict {
	models := e.get(t, "/admin/api/models")
	if strings.Contains(models.raw(), `"name":"AdminUser"`) || strings.Contains(models.raw(), `"table":"nucleus_admin_users"`) {
		t.Logf("admin users are a browsable model: %s", models.text())
		return partial
	}
	return e.unrouted(t, "/admin/api/users", "/admin/api/admins", "/admin/api/admin-users")
}

// probeOperatorCredentialReset asks for the other half of operator
// management: the password an operator forgot, or the account a leaver keeps.
func probeOperatorCredentialReset(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-reset")
	return e.unrouted(t,
		"/admin/api/users/"+op.id+"/password",
		"/admin/api/users/"+op.id+"/disable",
		"/admin/api/account/password")
}

// probeRolesFromPanel assigns a role and reads it back — the part of operator
// management that DOES exist.
func probeRolesFromPanel(t *testing.T, e *env) verdict {
	assign := e.do(t, http.MethodPost, "/admin/api/rbac/roles/assign",
		map[string]any{"user": "adminbench-role-subject", "role": "editors"})
	if assign.code >= 400 {
		t.Logf("assign answered %d: %s", assign.code, assign.text())
		return absent
	}
	roles := e.get(t, "/admin/api/rbac/roles?user=adminbench-role-subject")
	if roles.code != http.StatusOK || !strings.Contains(roles.raw(), "editors") {
		t.Logf("roles read back as %d: %s", roles.code, roles.text())
		return partial
	}
	remove := e.do(t, http.MethodPost, "/admin/api/rbac/roles/remove",
		map[string]any{"user": "adminbench-role-subject", "role": "editors"})
	if remove.code >= 400 {
		t.Logf("remove answered %d: %s", remove.code, remove.text())
		return partial
	}
	return present
}

// probePolicyManagement lists, adds and removes policies from the panel.
func probePolicyManagement(t *testing.T, e *env) verdict {
	add := e.do(t, http.MethodPost, "/admin/api/rbac/policies",
		map[string]any{"sub": "adminbench-policy", "obj": "admin:Note", "act": "list"})
	if add.code >= 400 {
		t.Logf("add answered %d: %s", add.code, add.text())
		return absent
	}
	list := e.get(t, "/admin/api/rbac/policies")
	if list.code != http.StatusOK || !strings.Contains(list.raw(), "adminbench-policy") {
		t.Logf("policies read back as %d: %s", list.code, list.text())
		return partial
	}
	del := e.do(t, http.MethodDelete, "/admin/api/rbac/policies",
		map[string]any{"sub": "adminbench-policy", "obj": "admin:Note", "act": "list"})
	if del.code >= 400 {
		t.Logf("remove answered %d: %s", del.code, del.text())
		return partial
	}
	return present
}

// probeDeniedActionVisible asks whether the UI can grey out what the
// operator may not do. The panel answers the question one call at a time
// (/api/rbac/check), but nothing in what a screen loads — the model list,
// the schema — says which verbs this operator holds, so a UI can only find
// out by trying and being refused.
func probeDeniedActionVisible(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-hints")
	e.grant(t, op.username, "admin:Note", "list")

	models := e.asOperator(t, op, http.MethodGet, "/admin/api/models", nil)
	schema := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note/schema", nil)
	hints := 0
	for _, key := range []string{`"can_create"`, `"can_update"`, `"can_delete"`, `"permissions"`, `"allowed_actions"`} {
		if strings.Contains(models.raw(), key) || strings.Contains(schema.raw(), key) {
			hints++
		}
	}
	check := e.get(t, "/admin/api/rbac/check?sub="+op.username+"&obj=admin:Note&act=delete")
	t.Logf("capability hints in the payloads a screen loads: %d; /api/rbac/check answers %d", hints, check.code)
	switch {
	case hints > 0:
		return present
	case check.code == http.StatusOK:
		return partial
	default:
		return absent
	}
}

// probeReadOnlyOperator is the viewer role every admin ships with: allowed to
// look, refused to write.
func probeReadOnlyOperator(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-viewer")
	e.grant(t, op.username, "admin:Note", "list")

	write := e.asOperator(t, op, http.MethodPost, "/admin/api/models/Note",
		map[string]any{"title": "viewer should not write", "status": "draft"})
	if write.code != http.StatusForbidden {
		t.Logf("an operator granted only list created a record (%d): %s", write.code, write.text())
		return absent
	}
	read := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note", nil)
	if read.code != http.StatusOK {
		t.Logf("the same operator cannot read either (%d)", read.code)
		return partial
	}
	return present
}

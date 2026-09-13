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
// then writes that field. It is measured by EFFECT and in both directions:
// the denied field must still hold its old value afterwards, and a field the
// same operator DOES hold must still be writable — a panel that refused every
// write would pass a one-sided probe.
func probeFieldPermissions(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-field")
	e.grant(t, op.username, "admin:Note", "list")
	e.grant(t, op.username, "admin:Note", "retrieve")
	e.grant(t, op.username, "admin:Note", "create")
	e.grant(t, op.username, "admin:Note", "update")

	const original = "field-perms"
	id := e.createNote(t, map[string]any{"title": original, "status": "draft"})
	// Nothing grants this operator the title field; the only policy that
	// could say so is the one below, in the grammar a field policy would use.
	e.grant(t, op.username, "admin:Note.title", "deny")

	r := e.asOperator(t, op, http.MethodPut, "/admin/api/models/Note/"+id,
		map[string]any{"title": "rewritten by an operator who should not"})
	if r.code != http.StatusForbidden {
		schema := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Note/schema", nil)
		if strings.Contains(schema.raw(), `"can_edit"`) || strings.Contains(schema.raw(), `"permissions"`) {
			t.Logf("the schema carries per-field permission hints but the write went through (%d): %s", r.code, r.text())
			return partial
		}
		t.Logf("a field-level policy changes nothing: the write answered %d", r.code)
		return absent
	}

	// A 403 that wrote the row anyway would be worse than no permission at
	// all, so the value is read back rather than assumed.
	after := e.get(t, "/admin/api/models/Note/"+id)
	if after.code != http.StatusOK {
		t.Logf("the record could not be read back (%d): %s", after.code, after.text())
		return partial
	}
	if got, _ := after.json(t)["title"].(string); got != original {
		t.Logf("the refused write still changed the field: title is %q", got)
		return absent
	}

	// The rest of the model stays writable: a field permission narrows one
	// field, it does not turn the operator read-only.
	allowed := e.asOperator(t, op, http.MethodPut, "/admin/api/models/Note/"+id,
		map[string]any{"status": "reviewed"})
	if allowed.code != http.StatusOK {
		t.Logf("a field the operator DOES hold was refused too (%d): %s", allowed.code, allowed.text())
		return partial
	}
	return present
}

// probeRowPermissions asks for "this operator may work on their own rows",
// the permission every editorial admin needs. It is measured on Article,
// the one model of this application that says who owns a row.
//
// Everything here is by effect: the operator's OWN row is created through the
// panel (so the panel decides who owns it), somebody else's is written by the
// superuser, and the probe asks what the operator can see, edit and delete.
func probeRowPermissions(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-row")
	for _, act := range []string{"list", "retrieve", "create", "update", "delete"} {
		e.grant(t, op.username, "admin:Article#own", act)
	}

	theirs := e.createArticle(t, map[string]any{"title": "row-theirs", "owner": "somebody-else"})

	created := e.asOperator(t, op, http.MethodPost, "/admin/api/models/Article",
		map[string]any{"title": "row-mine"})
	if created.code != http.StatusCreated {
		t.Logf("a row-scoped operator could not create their own row (%d): %s", created.code, created.text())
		return absent
	}
	mine := recordID(t, created.json(t))

	list := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Article", nil)
	if list.code != http.StatusOK {
		t.Logf("list answered %d: %s", list.code, list.text())
		return absent
	}
	body := list.raw()
	switch {
	case !strings.Contains(body, "row-mine"):
		t.Logf("the operator cannot see their own row (%s): %s", mine, list.text())
		return absent
	case strings.Contains(body, "row-theirs"):
		t.Logf("another operator's row (%s) is visible: no row scope exists", theirs)
		return absent
	}

	// The confinement has to hold on the row endpoints too, or the grid
	// hides what the URL still serves.
	if got := e.asOperator(t, op, http.MethodGet, "/admin/api/models/Article/"+theirs, nil); got.code != http.StatusNotFound {
		t.Logf("another operator's row is readable by id (%d): %s", got.code, got.text())
		return partial
	}
	if put := e.asOperator(t, op, http.MethodPut, "/admin/api/models/Article/"+theirs,
		map[string]any{"title": "taken over"}); put.code != http.StatusNotFound {
		t.Logf("another operator's row is editable by id (%d): %s", put.code, put.text())
		return partial
	}
	if del := e.asOperator(t, op, http.MethodDelete, "/admin/api/models/Article/"+theirs, nil); del.code != http.StatusNotFound {
		t.Logf("another operator's row is deletable by id (%d): %s", del.code, del.text())
		return partial
	}
	// And their own row is theirs to edit.
	if put := e.asOperator(t, op, http.MethodPut, "/admin/api/models/Article/"+mine,
		map[string]any{"title": "row-mine, edited"}); put.code != http.StatusOK {
		t.Logf("the operator cannot edit their OWN row (%d): %s", put.code, put.text())
		return partial
	}
	return present
}

// probeAdminUserManagement is OR-4, the oldest P1 in the registry: an
// operator who needs a colleague to have access should not have to leave the
// panel. The probe does what that colleague's first day looks like — an
// account is created from the panel, it shows up in the list with the role it
// was given, and the person signs in with it.
func probeAdminUserManagement(t *testing.T, e *env) verdict {
	const (
		username = "bench-hired"
		password = "a-long-enough-bench-password"
	)
	created := e.do(t, http.MethodPost, "/admin/api/admin-users", map[string]any{
		"username": username,
		"email":    username + "@example.test",
		"password": password,
		"roles":    []string{"viewers"},
	})
	if created.code == http.StatusNotFound || created.code == http.StatusMethodNotAllowed ||
		created.code == http.StatusNotImplemented || created.servedTheShell() {
		t.Logf("no route creates an operator (%d): accounts live in the table and the CLI", created.code)
		return absent
	}
	if created.code != http.StatusCreated {
		t.Logf("create operator answered %d: %s", created.code, created.text())
		return partial
	}
	id, _ := created.json(t)["id"].(string)

	list := e.get(t, "/admin/api/admin-users")
	if list.code != http.StatusOK || !strings.Contains(list.raw(), username) {
		t.Logf("the created operator is not in the list (%d): %s", list.code, list.text())
		return partial
	}
	if !strings.Contains(list.raw(), "viewers") {
		t.Logf("the role granted at creation is not on the record: %s", list.text())
		return partial
	}

	// The account the panel wrote is an account the panel accepts.
	if status := trySignIn(t, e.server(), username, password); !signedIn(status) {
		t.Logf("the operator created from the panel cannot sign in (login answered %d)", status)
		return partial
	}
	t.Logf("operator %s created, listed with its role, and signed in", id)
	return present
}

// probeOperatorCredentialReset is the other half of OR-4: the password
// somebody forgot, and the account somebody left behind. Both are measured by
// their effect on signing in, not by the status code of the call that changes
// them.
func probeOperatorCredentialReset(t *testing.T, e *env) verdict {
	const (
		username = "bench-rotated"
		first    = "the-first-bench-password"
		second   = "the-second-bench-password"
	)
	created := e.do(t, http.MethodPost, "/admin/api/admin-users", map[string]any{
		"username": username,
		"email":    username + "@example.test",
		"password": first,
	})
	if created.code != http.StatusCreated {
		t.Logf("the operator this control needs could not be created (%d): %s", created.code, created.text())
		return absent
	}
	id, _ := created.json(t)["id"].(string)

	reset := e.do(t, http.MethodPost, "/admin/api/admin-users/"+id+"/password",
		map[string]any{"password": second})
	if reset.code == http.StatusNotFound || reset.code == http.StatusMethodNotAllowed || reset.servedTheShell() {
		t.Logf("no route resets an operator's password (%d)", reset.code)
		return absent
	}
	if reset.code != http.StatusOK {
		t.Logf("password reset answered %d: %s", reset.code, reset.text())
		return partial
	}
	if status := trySignIn(t, e.server(), username, second); !signedIn(status) {
		t.Logf("the new password does not work (login answered %d)", status)
		return partial
	}
	if status := trySignIn(t, e.server(), username, first); signedIn(status) {
		t.Logf("the OLD password still works after the reset")
		return partial
	}

	disable := e.do(t, http.MethodPost, "/admin/api/admin-users/"+id+"/disable", map[string]any{})
	if disable.code == http.StatusNotFound || disable.code == http.StatusMethodNotAllowed || disable.servedTheShell() {
		t.Logf("the password can be reset but no route deactivates an operator (%d)", disable.code)
		return partial
	}
	if disable.code != http.StatusOK {
		t.Logf("deactivate answered %d: %s", disable.code, disable.text())
		return partial
	}
	if status := trySignIn(t, e.server(), username, second); signedIn(status) {
		t.Logf("a deactivated operator can still sign in (login answered %d)", status)
		return partial
	}
	return present
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

// probeDeniedActionVisible asks whether the UI can grey out what the operator
// may not do — without trying it. A hint is only worth anything if it agrees
// with what the panel actually does, so the probe reads the hints from the
// two payloads every model screen loads and then CHECKS them against the
// answers: the verb the hints call true must succeed and the one they call
// false must be refused.
func probeDeniedActionVisible(t *testing.T, e *env) verdict {
	op := e.operatorNamed(t, "perm-hints")
	e.grant(t, op.username, "admin:Note", "list")
	e.grant(t, op.username, "admin:Note", "get_schema")
	e.grant(t, op.username, "admin:*", "list_models")

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
	if hints == 0 {
		if check.code == http.StatusOK {
			return partial
		}
		return absent
	}

	// A hint that does not match the enforcer is worse than none: the UI
	// would hide a button that works, or offer one that does not.
	perms, ok := schema.json(t)["permissions"].(map[string]any)
	if !ok {
		t.Logf("the schema carries hints but no permissions map: %s", schema.text())
		return partial
	}
	if allowed, _ := perms["list"].(bool); !allowed {
		t.Logf("the hints say this operator may not list, but the grant says otherwise: %v", perms)
		return partial
	}
	if allowed, _ := perms["create"].(bool); allowed {
		t.Logf("the hints claim a create this operator was never granted: %v", perms)
		return partial
	}
	write := e.asOperator(t, op, http.MethodPost, "/admin/api/models/Note",
		map[string]any{"title": "hint check", "status": "draft"})
	if write.code != http.StatusForbidden {
		t.Logf("the hints say create is refused and the panel answered %d", write.code)
		return partial
	}
	return present
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

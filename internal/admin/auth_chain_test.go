// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Arco C: el panel autentica por la cadena declarada, pero NO delega la
// autorización.
//
// Delegating authentication without keeping authorization would turn an
// LDAP integration into a privilege escalation: every account in the
// corporate directory would become an administrator of this panel. These
// tests pin that boundary.
package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

type dirBackend struct{ name, user, pass string }

func (d *dirBackend) Name() string { return d.name }
func (d *dirBackend) Authenticate(_ context.Context, username, password string) (*auth.User, error) {
	if username == d.user && password == d.pass {
		return &auth.User{ID: username, Username: username}, nil
	}
	return nil, auth.ErrInvalidCredentials
}

// chainWith registers the backend under a name unique to the calling test.
// The registry is process-wide, so a shared name would let the first test
// to run decide what every other test authenticates against — which is
// exactly what happened before this helper generated one per test.
func chainWith(t *testing.T, b *dirBackend) *auth.Chain {
	t.Helper()
	b.name = "dir-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	name := b.Name()
	if err := auth.RegisterBackend(name, func() (auth.Backend, error) { return b, nil }); err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	chain, err := auth.NewChain(name)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	return chain
}

// The boundary: correct directory credentials, but the person is not an
// administrator of this panel.
func TestCheckCredentials_DirectoryUserWhoIsNotAnAdminIsRefused(t *testing.T) {
	a := &DatabaseAdminAuth{}
	a.WithAuthChain(chainWith(t, &dirBackend{user: "empleado", pass: "correcta"}))

	req := httptest.NewRequest("POST", "/admin/login", nil)
	if a.checkCredentials(req, "empleado", "correcta", adminLoginUserRecord{}, false) {
		t.Fatal("a directory account absent from the admin table must NOT get in — delegating authentication does not delegate authorization")
	}
}

// The same credentials, for someone who IS in the admin table.
func TestCheckCredentials_DirectoryUserWhoIsAnAdminGetsIn(t *testing.T) {
	a := &DatabaseAdminAuth{}
	a.WithAuthChain(chainWith(t, &dirBackend{user: "jefa", pass: "correcta"}))

	req := httptest.NewRequest("POST", "/admin/login", nil)
	if !a.checkCredentials(req, "jefa", "correcta", adminLoginUserRecord{Username: "jefa"}, true) {
		t.Fatal("an admin whose credentials the chain accepts must get in")
	}
}

// Being in the admin table is not enough either: the chain still has to
// accept. Otherwise a stale local row would outlive a revoked directory
// account.
func TestCheckCredentials_AdminWithWrongDirectoryPasswordIsRefused(t *testing.T) {
	a := &DatabaseAdminAuth{}
	a.WithAuthChain(chainWith(t, &dirBackend{user: "jefa", pass: "correcta"}))

	req := httptest.NewRequest("POST", "/admin/login", nil)
	if a.checkCredentials(req, "jefa", "equivocada", adminLoginUserRecord{Username: "jefa"}, true) {
		t.Fatal("the chain must still verify the password; a local row is authorization, not authentication")
	}
}

// Without a chain, nothing changes: the admin table's own hash decides.
func TestCheckCredentials_NoChainKeepsTheLocalPath(t *testing.T) {
	a := &DatabaseAdminAuth{}
	hash, err := auth.HashPassword("secreta")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	req := httptest.NewRequest("POST", "/admin/login", nil)

	if !a.checkCredentials(req, "root", "secreta", adminLoginUserRecord{Username: "root", PasswordHash: hash}, true) {
		t.Error("the local path must still work when no chain is configured")
	}
	if a.checkCredentials(req, "root", "otra", adminLoginUserRecord{Username: "root", PasswordHash: hash}, true) {
		t.Error("a wrong password must be refused on the local path")
	}
	if a.checkCredentials(req, "fantasma", "loquesea", adminLoginUserRecord{}, false) {
		t.Error("an absent user must be refused on the local path")
	}
}

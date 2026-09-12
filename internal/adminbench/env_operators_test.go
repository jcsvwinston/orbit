// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package adminbench

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	"github.com/jcsvwinston/orbit"
)

// The bench needs a SECOND operator — one who is not a superuser — because
// every permission question ("can this person edit that model?") is answered
// `true` for a superuser before any policy is consulted (panel.go
// authorizeAction). Measuring authorization with the bootstrap admin measures
// the bypass.
//
// Creating that operator is where the bench has to reach past the product and
// write the row itself: the panel has no route that creates an admin user, so
// there is no way to ask it for one. That absence is not an inconvenience of
// the harness — it is control PERM-02, and this function is the evidence for
// it. When the panel grows the route, this helper calls it instead.
const limitedPassword = "adminbench-limited-password"

type operator struct {
	id       string
	username string
	client   *http.Client
}

// operatorNamed returns a non-superuser operator of its own. Each permission
// probe takes its OWN: grants accumulate on a subject, so a shared operator
// turns "an operator granted only list created a record" into a finding about
// a grant a previous probe made. The bench's first run reported exactly that.
func (e *env) operatorNamed(t *testing.T, name string) *operator {
	t.Helper()
	if op, ok := e.operators[name]; ok {
		return op
	}
	if e.operators == nil {
		e.operators = map[string]*operator{}
	}
	op := e.newOperator(t, name, false)
	e.operators[name] = op
	return op
}

// newOperator inserts an admin user directly into the table the panel
// authenticates against and signs in as them.
func (e *env) newOperator(t *testing.T, username string, superuser bool) *operator {
	t.Helper()
	srv := e.server()
	hash, err := auth.HashPassword(limitedPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	id := "adminbench-" + username
	now := time.Now().UTC().Format(time.RFC3339)
	su := 0
	if superuser {
		su = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := srv.DB().ExecContext(ctx,
		`INSERT INTO nucleus_admin_users (id, username, email, password_hash, is_superuser, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, username, username+"@example.test", hash, su, now, now); err != nil {
		t.Fatalf("insert operator %q: %v", username, err)
	}
	return &operator{id: id, username: username, client: e.signIn(t, username, limitedPassword)}
}

// signIn returns a client holding that operator's admin session.
func (e *env) signIn(t *testing.T, username, password string) *http.Client {
	t.Helper()
	return signInTo(t, e.server(), username, password)
}

// asOperator issues one request with that operator's session instead of the
// superuser's.
func (e *env) asOperator(t *testing.T, op *operator, method, path string, payload any) response {
	t.Helper()
	return e.request(t, op.client, method, path, payload)
}

// grant adds one RBAC policy through the panel's own management API — the way
// an operator grants a permission today.
func (e *env) grant(t *testing.T, sub, obj, act string) {
	t.Helper()
	r := e.do(t, http.MethodPost, "/admin/api/rbac/policies",
		map[string]any{"sub": sub, "obj": obj, "act": act})
	if r.code >= 400 {
		t.Fatalf("grant %s %s %s answered %d: %s", sub, obj, act, r.code, r.text())
	}
}

func (e *env) db() *sql.DB { return e.server().DB() }

// migrationsApp boots an application that ships one migration, so the
// migrations view has something to list and apply. It is a second application
// because MigrationsPath is a mount-time setting.
func (e *env) migrationsApp(t *testing.T) (*nucleustest.Server, *http.Client) {
	t.Helper()
	if e.migrations != nil {
		return e.migrations, e.migrationsClient
	}
	dir := t.TempDir()
	const up = "CREATE TABLE adminbench_migrated (id INTEGER PRIMARY KEY);\n"
	if err := os.WriteFile(filepath.Join(dir, "20260912000000_adminbench.up.sql"), []byte(up), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260912000000_adminbench.down.sql"),
		[]byte("DROP TABLE adminbench_migrated;\n"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	cfg := benchConfig(t)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config: cfg,
		Modules: map[string]nucleus.ModuleSpec{
			"content": contentModule(),
			"orbit": orbit.Module(orbit.Config{
				Prefix:            "/admin",
				Title:             "Admin Bench (migrations)",
				BootstrapUsername: "admin",
				BootstrapEmail:    "admin@example.test",
				BootstrapPassword: bootstrapPassword,
				MigrationsPath:    dir,
			}),
		},
	})
	e.migrations = srv
	e.migrationsClient = signInTo(t, srv, "admin", bootstrapPassword)
	return e.migrations, e.migrationsClient
}

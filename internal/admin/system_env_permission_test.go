package admin

// The process environment in the system snapshot, and who reads it
// (DEP-2026-004). The snapshot is gated by system_pulse; system_env is the
// action that names the environment. In 1.x an operator holding system_pulse
// alone still gets it, with one WARN per operator per process; at Orbit 2.0
// that operator gets an empty list. The tests named "_V1" pin the 1.x half:
// the 2.0 change is a diff to them.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

const snapshotOperatorHeader = "X-Test-Operator"

// snapshotOperatorAuth authenticates the operator the request names in
// snapshotOperatorHeader, so one server can be asked by several operators
// without mutating the provider between requests. Authorize answers from
// allow ("<username>:<model>:<action>"), denying what it does not list: it is
// the provider path, used when the panel has no RBAC enforcer.
type snapshotOperatorAuth struct {
	users map[string]*auth.User
	allow map[string]bool
}

func (a *snapshotOperatorAuth) Authenticate(r *http.Request) (*auth.User, error) {
	user, ok := a.users[r.Header.Get(snapshotOperatorHeader)]
	if !ok {
		return nil, fmt.Errorf("unknown test operator %q", r.Header.Get(snapshotOperatorHeader))
	}
	return user, nil
}

func (a *snapshotOperatorAuth) Authorize(user *auth.User, model, action string) bool {
	if user == nil {
		return false
	}
	if user.IsSuperuser {
		return true
	}
	return a.allow[user.Username+":"+model+":"+action]
}

func (a *snapshotOperatorAuth) LoginHandler() http.Handler { return http.NotFoundHandler() }

// lockedLog is a log sink the handler goroutines write to while the test
// reads it.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) lines(substr string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return out
}

type systemEnvEnv struct {
	panel *Panel
	srv   *httptest.Server
	log   *lockedLog
}

// newSystemEnvPanel builds a panel whose operators are ops (system_pulse and
// system_env), pulse (system_pulse only), pulse2 (the same, a second
// operator), envonly (system_env only), nobody (neither) and root (a
// superuser with no grant). withRBAC puts the grants in a Casbin enforcer;
// without it the provider answers them.
func newSystemEnvPanel(t *testing.T, withRBAC bool) *systemEnvEnv {
	t.Helper()
	users := map[string]*auth.User{}
	for _, name := range []string{"ops", "pulse", "pulse2", "envonly", "nobody"} {
		users[name] = &auth.User{ID: "id-" + name, Username: name, Role: "role-" + name}
	}
	users["root"] = &auth.User{ID: "id-root", Username: "root", Role: "root", IsSuperuser: true}
	grants := map[string][]string{
		"ops":     {"system_pulse", systemEnvAction},
		"pulse":   {"system_pulse"},
		"pulse2":  {"system_pulse"},
		"envonly": {systemEnvAction},
	}

	provider := &snapshotOperatorAuth{users: users, allow: map[string]bool{}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)

	if withRBAC {
		enf, err := authz.New(slog.Default())
		if err != nil {
			t.Fatalf("authz.New: %v", err)
		}
		for name, actions := range grants {
			for _, action := range actions {
				if err := enf.AddPolicy(name, "admin:*", action); err != nil {
					t.Fatalf("AddPolicy(%s, %s): %v", name, action, err)
				}
			}
		}
		panel.rbac = enf
	} else {
		for name, actions := range grants {
			for _, action := range actions {
				provider.allow[name+":*:"+action] = true
			}
		}
	}

	panel.bootEnv = buildSystemEnvironmentRows([]string{
		"APP_ENV=test",
		"HOME=/home/app",
		"SERVICE_TOKEN=token-value",
	})
	sink := &lockedLog{}
	panel.logger = slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelWarn}))

	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return &systemEnvEnv{panel: panel, srv: srv, log: sink}
}

// snapshot asks for the system snapshot as operator and returns the status
// and, on a 200, the environment rows by name.
func (e *systemEnvEnv) snapshot(t *testing.T, operator string) (int, map[string]systemEnvVar, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+"/api/system/snapshot", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(snapshotOperatorHeader, operator)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("snapshot request as %s: %v", operator, err)
	}
	defer res.Body.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(res.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, nil, raw.String()
	}
	var payload systemSnapshotResponse
	if err := json.Unmarshal(raw.Bytes(), &payload); err != nil {
		t.Fatalf("decode snapshot as %s: %v (%s)", operator, err, raw.String())
	}
	if payload.Environment == nil {
		t.Fatalf("snapshot as %s: environment is null, want a list", operator)
	}
	env := make(map[string]systemEnvVar, len(payload.Environment))
	for _, row := range payload.Environment {
		env[row.Name] = row
	}
	return res.StatusCode, env, raw.String()
}

func (e *systemEnvEnv) deprecationWarnings() []string {
	return e.log.lines("DEP-2026-004")
}

func assertServesEnvironment(t *testing.T, operator string, env map[string]systemEnvVar) {
	t.Helper()
	if got := env["APP_ENV"]; got.Value != "test" || got.Masked {
		t.Fatalf("as %s: APP_ENV = %#v, want the value in clear", operator, got)
	}
	if got := env["HOME"]; got.Value != "/home/app" {
		t.Fatalf("as %s: HOME = %#v, want the value in clear", operator, got)
	}
	if got := env["SERVICE_TOKEN"]; got.Value != "***" || !got.Masked {
		t.Fatalf("as %s: SERVICE_TOKEN = %#v, want it masked as before", operator, got)
	}
}

func forEachAuthorizer(t *testing.T, run func(t *testing.T, e *systemEnvEnv)) {
	for _, mode := range []struct {
		name string
		rbac bool
	}{{"rbac", true}, {"provider", false}} {
		t.Run(mode.name, func(t *testing.T) {
			run(t, newSystemEnvPanel(t, mode.rbac))
		})
	}
}

// TestSystemSnapshot_SystemEnv_ServesEnvironment: an operator holding
// system_pulse and system_env reads the environment, masked as before, and
// the panel logs nothing about it. This is the grant that keeps the
// environment at Orbit 2.0.
func TestSystemSnapshot_SystemEnv_ServesEnvironment(t *testing.T) {
	forEachAuthorizer(t, func(t *testing.T, e *systemEnvEnv) {
		status, env, body := e.snapshot(t, "ops")
		if status != http.StatusOK {
			t.Fatalf("snapshot as ops: %d %s", status, body)
		}
		assertServesEnvironment(t, "ops", env)
		if warns := e.deprecationWarnings(); len(warns) != 0 {
			t.Fatalf("an operator holding system_env produced the deprecation WARN: %v", warns)
		}
	})
}

// TestSystemSnapshot_SystemPulseOnly_ServesEnvironmentWithWarning_V1 pins
// the 1.x behaviour DEP-2026-004 announces: system_pulse alone still serves
// the environment, and the panel logs one WARN per operator per process
// naming the notice and the operator. The snapshot is polled by the
// overview and the system page, so a second request by the same operator
// logs nothing more; another operator gets their own line.
//
// At Orbit 2.0 this test changes: the same operator gets an empty
// environment list, and the WARN is gone.
func TestSystemSnapshot_SystemPulseOnly_ServesEnvironmentWithWarning_V1(t *testing.T) {
	forEachAuthorizer(t, func(t *testing.T, e *systemEnvEnv) {
		for i := 0; i < 3; i++ {
			status, env, body := e.snapshot(t, "pulse")
			if status != http.StatusOK {
				t.Fatalf("snapshot %d as pulse: %d %s", i, status, body)
			}
			assertServesEnvironment(t, "pulse", env)
		}
		warns := e.deprecationWarnings()
		if len(warns) != 1 {
			t.Fatalf("three snapshots by one operator logged %d deprecation WARNs, want 1: %v", len(warns), warns)
		}
		line := warns[0]
		for _, want := range []string{"level=WARN", "system_env", "Orbit 2.0", "operator=pulse", "operator_id=id-pulse"} {
			if !strings.Contains(line, want) {
				t.Fatalf("the deprecation WARN does not say %q: %s", want, line)
			}
		}

		if status, env, body := e.snapshot(t, "pulse2"); status != http.StatusOK {
			t.Fatalf("snapshot as pulse2: %d %s", status, body)
		} else {
			assertServesEnvironment(t, "pulse2", env)
		}
		warns = e.deprecationWarnings()
		if len(warns) != 2 || !strings.Contains(warns[1], "operator=pulse2") {
			t.Fatalf("a second operator through system_pulse alone did not get its own WARN: %v", warns)
		}
	})
}

// TestSystemSnapshot_Superuser_ServesEnvironmentWithoutWarning: a superuser
// holds every action, system_env included, with or without a grant.
func TestSystemSnapshot_Superuser_ServesEnvironmentWithoutWarning(t *testing.T) {
	forEachAuthorizer(t, func(t *testing.T, e *systemEnvEnv) {
		status, env, body := e.snapshot(t, "root")
		if status != http.StatusOK {
			t.Fatalf("snapshot as root: %d %s", status, body)
		}
		assertServesEnvironment(t, "root", env)
		if warns := e.deprecationWarnings(); len(warns) != 0 {
			t.Fatalf("a superuser produced the deprecation WARN: %v", warns)
		}
	})
}

// TestSystemSnapshot_WithoutSystemPulse_Refused: the snapshot is still
// system_pulse's door. Without it the operator gets a 403 and no
// environment, whether or not they hold system_env — system_env widens the
// snapshot and opens nothing on its own.
func TestSystemSnapshot_WithoutSystemPulse_Refused(t *testing.T) {
	forEachAuthorizer(t, func(t *testing.T, e *systemEnvEnv) {
		for _, operator := range []string{"nobody", "envonly"} {
			status, _, body := e.snapshot(t, operator)
			if status != http.StatusForbidden {
				t.Fatalf("snapshot as %s: %d %s, want 403", operator, status, body)
			}
			if strings.Contains(body, "/home/app") || strings.Contains(body, "APP_ENV") {
				t.Fatalf("the refusal for %s carries the environment: %s", operator, body)
			}
		}
		if warns := e.deprecationWarnings(); len(warns) != 0 {
			t.Fatalf("a refused snapshot produced the deprecation WARN: %v", warns)
		}
	})
}

// TestSystemSnapshot_OpenPosture_ServesEnvironment: with no auth provider
// (the open posture, warned at mount) there is no operator to gate or to
// name, so the environment is served as before and nothing is logged about
// it. Orbit 2.0 does not change this.
func TestSystemSnapshot_OpenPosture_ServesEnvironment(t *testing.T) {
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, nil)
	t.Cleanup(cleanup)
	panel.bootEnv = buildSystemEnvironmentRows([]string{"APP_ENV=test", "HOME=/home/app", "SERVICE_TOKEN=token-value"})
	sink := &lockedLog{}
	panel.logger = slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)

	e := &systemEnvEnv{panel: panel, srv: srv, log: sink}
	status, env, body := e.snapshot(t, "")
	if status != http.StatusOK {
		t.Fatalf("snapshot in the open posture: %d %s", status, body)
	}
	assertServesEnvironment(t, "the open posture", env)
	if warns := e.deprecationWarnings(); len(warns) != 0 {
		t.Fatalf("the open posture produced the deprecation WARN: %v", warns)
	}
}

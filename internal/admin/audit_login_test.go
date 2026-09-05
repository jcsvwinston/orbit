package admin

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	_ "modernc.org/sqlite"
)

const (
	loginHarnessUser     = "root"
	loginHarnessPassword = "correct-horse-battery-staple"
)

// loginAuditHarness is a panel whose auth provider is the real
// DatabaseAdminAuth over a SQLite admin table, with the session middleware
// wrapped around it — the production login path end to end.
type loginAuditHarness struct {
	panel  *Panel
	srv    *httptest.Server
	client *http.Client
}

func newLoginAuditHarness(t *testing.T) *loginAuditHarness {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// One connection: a :memory: database is per connection.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	if _, err := sqlDB.Exec(`CREATE TABLE nucleus_admin_users (
		id TEXT PRIMARY KEY, username TEXT, email TEXT,
		password_hash TEXT, is_superuser INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	hash, err := auth.HashPassword(loginHarnessPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO nucleus_admin_users VALUES ('1',?,'root@example.com',?,1)`, loginHarnessUser, hash,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	sm := auth.NewSessionManager(auth.SessionConfig{})
	provider := NewDatabaseAdminAuth(sqlDB, sm, "/admin").WithSystem("sqlite")
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, provider)
	t.Cleanup(cleanup)
	panel.audit = newAuditStore(100)
	panel.config.Session = sm

	srv := httptest.NewServer(sm.Middleware()(panel.Handler()))
	t.Cleanup(srv.Close)

	client := newCookieClient(t)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &loginAuditHarness{panel: panel, srv: srv, client: client}
}

// login posts the login form and returns the raw response (redirects are
// not followed: a 303 is the success signal).
func (h *loginAuditHarness) login(t *testing.T, username, password string) *http.Response {
	t.Helper()
	form := url.Values{"username": {username}, "password": {password}, "next": {"/admin/"}}
	resp, err := h.client.Post(h.srv.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (h *loginAuditHarness) newest(t *testing.T) AuditEntry {
	t.Helper()
	entries := h.panel.audit.list(auditQueryOpts{PageSize: 1})
	if len(entries) == 0 {
		t.Fatal("no audit entry recorded")
	}
	return entries[0]
}

func TestAuditLogin_SuccessFailureAndLockoutAreRecordedWithoutSecrets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping login audit test in short mode (a dozen bcrypt compares)")
	}
	h := newLoginAuditHarness(t)

	// A wrong password: login.failed, attributed to the attempted username,
	// with no user id (nobody was authenticated) and no password anywhere.
	resp := h.login(t, loginHarnessUser, "nope")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", resp.StatusCode)
	}
	failed := h.newest(t)
	if failed.Action != "login.failed" || failed.Username != loginHarnessUser || failed.UserID != "" {
		t.Fatalf("failed login entry = %+v, want login.failed by %q with no user id", failed, loginHarnessUser)
	}

	// An unknown username is recorded the same way (the entry does not
	// reveal whether the account exists).
	resp = h.login(t, "ghost", "nope")
	resp.Body.Close()
	if e := h.newest(t); e.Action != "login.failed" || e.Username != "ghost" {
		t.Fatalf("unknown user entry = %+v, want login.failed by ghost", e)
	}

	// Success: login, attributed to the user, with the session's user id.
	resp = h.login(t, loginHarnessUser, loginHarnessPassword)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", resp.StatusCode)
	}
	ok := h.newest(t)
	if ok.Action != "login" || ok.Username != loginHarnessUser || ok.UserID != "1" {
		t.Fatalf("login entry = %+v, want login by %s/1", ok, loginHarnessUser)
	}

	// Lockout: after loginFailureLimit failures the limiter answers 429,
	// recorded as login.locked.
	for i := 0; i < loginFailureLimit; i++ {
		r := h.login(t, "locked-out", "nope")
		r.Body.Close()
	}
	resp = h.login(t, "locked-out", "nope")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("lockout status = %d, want 429", resp.StatusCode)
	}
	if e := h.newest(t); e.Action != "login.locked" || e.Username != "locked-out" {
		t.Fatalf("lockout entry = %+v, want login.locked by locked-out", e)
	}

	// No entry carries a password, in any field.
	for _, e := range h.panel.audit.list(auditQueryOpts{PageSize: 200}) {
		raw := mustJSON(e)
		for _, secret := range []string{loginHarnessPassword, "nope", "password"} {
			if strings.Contains(raw, secret) {
				t.Errorf("audit entry %d leaks %q: %s", e.ID, secret, raw)
			}
		}
	}

	// A malformed form (no credentials checked) records nothing.
	before := h.newest(t).ID
	resp, err := h.client.Post(h.srv.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader("username=&password="))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty form status = %d, want 400", resp.StatusCode)
	}
	if h.newest(t).ID != before {
		t.Errorf("a 400 login form left an audit entry: %+v", h.newest(t))
	}
}

// TestAuditLogin_FailedAttemptDoesNotQueryTheProvider pins that a failed
// login is attributed from the form and never resolved through
// Auth.Authenticate (a database round-trip per attempt).
func TestAuditLogin_FailedAttemptDoesNotQueryTheProvider(t *testing.T) {
	counting := &countingAdminAuth{}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, counting)
	defer cleanup()
	panel.audit = newAuditStore(10)

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/login", "application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"username": {"eve"}, "password": {"x"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want the provider's 401", resp.StatusCode)
	}
	entries := panel.audit.list(auditQueryOpts{})
	if len(entries) != 1 || entries[0].Action != "login.failed" || entries[0].Username != "eve" {
		t.Fatalf("entries = %+v, want one login.failed by eve", entries)
	}
	if counting.authenticateCalls != 0 {
		t.Fatalf("Authenticate called %d time(s) while auditing a failed login, want 0", counting.authenticateCalls)
	}
}

// countingAdminAuth rejects every login with 401 and counts Authenticate.
type countingAdminAuth struct {
	authenticateCalls int
}

func (a *countingAdminAuth) Authenticate(*http.Request) (*auth.User, error) {
	a.authenticateCalls++
	return nil, http.ErrNoCookie
}

func (a *countingAdminAuth) Authorize(*auth.User, string, string) bool { return true }

func (a *countingAdminAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.WriteHeader(http.StatusUnauthorized)
	})
}

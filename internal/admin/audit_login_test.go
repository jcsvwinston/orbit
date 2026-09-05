package admin

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

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

// postForm posts a raw urlencoded body to /login and returns the response.
func (h *loginAuditHarness) postForm(t *testing.T, body string) *http.Response {
	t.Helper()
	resp, err := h.client.Post(h.srv.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// assertBoundedAuditString checks that got was cut at bound: it ends with
// the marker, is no longer than bound plus the marker and is still valid
// UTF-8 (the cut lands on a rune boundary).
func assertBoundedAuditString(t *testing.T, field, got string, bound int) {
	t.Helper()
	if !strings.HasSuffix(got, auditTruncatedMarker) || len(got) > bound+len(auditTruncatedMarker) {
		t.Fatalf("%s not bounded: len=%d, want <= %d with the marker", field, len(got), bound+len(auditTruncatedMarker))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("%s truncation broke a rune", field)
	}
}

// TestAuditLogin_EntriesAreBoundedAndTheBodyIsCapped pins that the
// unauthenticated login route cannot grow the ring: an oversized username
// and User-Agent are cut to their bounds, and a body past loginBodyMaxBytes
// is rejected before any credential is checked and leaves no entry.
func TestAuditLogin_EntriesAreBoundedAndTheBodyIsCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping login audit test in short mode (bcrypt compares)")
	}
	h := newLoginAuditHarness(t)

	// A 3 KB username (9 KB urlencoded: under the body cap, far past the
	// field bound, three bytes per rune so the cut lands mid-rune) and a
	// 200 KB User-Agent (under the server's default 1 MB header limit).
	longUser := strings.Repeat("€", 1024)
	longUA := strings.Repeat("a", 200*1024)
	form := url.Values{"username": {longUser}, "password": {"nope"}}
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", longUA)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	e := h.newest(t)
	if e.Action != "login.failed" {
		t.Fatalf("entry = %+v, want login.failed", e)
	}
	assertBoundedAuditString(t, "username", e.Username, auditFieldMaxLen)
	assertBoundedAuditString(t, "user_agent", e.UserAgent, auditUserAgentMaxLen)
	if !strings.HasPrefix(e.Username, "€") || !strings.HasPrefix(e.UserAgent, "aaaa") {
		t.Fatalf("bounded fields lost their prefix: username=%q user_agent=%q", e.Username[:8], e.UserAgent[:8])
	}

	// A body past loginBodyMaxBytes: the provider's form parser fails and
	// answers 400 before any credential is checked, and no entry is
	// recorded.
	before := e.ID
	over := url.Values{"username": {strings.Repeat("a", loginBodyMaxBytes+4096)}, "password": {"nope"}}
	resp = h.postForm(t, over.Encode())
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("over-cap body status = %d, want 400", resp.StatusCode)
	}
	if h.newest(t).ID != before {
		t.Fatalf("an over-cap login body left an audit entry: %+v", h.newest(t))
	}

	// The 2 MB username of the review's probe is rejected the same way.
	// The server closes the connection after the 400 instead of draining
	// the body, so the client may see the response or a transport error;
	// either way the ring is untouched.
	huge := url.Values{"username": {strings.Repeat("a", 2<<20)}, "password": {"nope"}}
	resp, err = h.client.Post(h.srv.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(huge.Encode()))
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("2 MB body status = %d, want 400", resp.StatusCode)
		}
	}
	if h.newest(t).ID != before {
		t.Fatalf("a 2 MB login body left an audit entry: %+v", h.newest(t))
	}
}

// TestAuditLogin_OneClientCannotEvictTheRing pins the per-IP budget on the
// production login path: with a ring of 100 and 130 failed attempts from
// one client, the log keeps loginFailureLimit login.failed entries and one
// login.locked (the lockout keeps answering 429 without a new entry), the
// entry recorded before the attack survives, and a successful login resets
// the client's budget as it resets the lockout.
func TestAuditLogin_OneClientCannotEvictTheRing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping login audit test in short mode (bcrypt compares)")
	}
	h := newLoginAuditHarness(t)
	h.panel.audit.add(AuditEntry{Action: "rbac.policy.add", ModelName: "rbac", Username: "root"})

	// Five failures, then a success: the lockout's IP window and the
	// budget start over (the lockout's per-username window does not, so
	// the attack below uses another name).
	for i := 0; i < 5; i++ {
		r := h.login(t, "mallory", "nope")
		r.Body.Close()
	}
	resp := h.login(t, loginHarnessUser, loginHarnessPassword)
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", resp.StatusCode)
	}

	const attempts = 130
	statuses := map[int]int{}
	for i := 0; i < attempts; i++ {
		r := h.login(t, "eve", "nope")
		statuses[r.StatusCode]++
		r.Body.Close()
	}
	if statuses[http.StatusUnauthorized] != loginFailureLimit || statuses[http.StatusTooManyRequests] != attempts-loginFailureLimit {
		t.Fatalf("statuses = %v, want %d x 401 and %d x 429", statuses, loginFailureLimit, attempts-loginFailureLimit)
	}

	count := func(action string) int { return h.panel.audit.count(auditQueryOpts{Action: action}) }
	if n := count("rbac.policy.add"); n != 1 {
		t.Fatalf("the entry recorded before the attack was evicted (count %d)", n)
	}
	if n := count("login.failed"); n != 5+loginFailureLimit {
		t.Fatalf("login.failed entries = %d, want %d (5 before the success, %d after)", n, 5+loginFailureLimit, loginFailureLimit)
	}
	if n := count("login.locked"); n != 1 {
		t.Fatalf("login.locked entries = %d, want 1 (the transition into lockout)", n)
	}
	if n := count("login"); n != 1 {
		t.Fatalf("login entries = %d, want 1", n)
	}
	if total := h.panel.audit.count(auditQueryOpts{}); total != 1+5+1+loginFailureLimit+1 {
		t.Fatalf("ring holds %d entries, want %d", total, 1+5+1+loginFailureLimit+1)
	}
}

// TestAuditLogin_ProviderWithoutLockoutIsStillBounded pins that the budget
// does not depend on the provider's lockout: against a provider that
// answers 401 forever, one client leaves loginFailureLimit login.failed
// entries per window and nothing more.
func TestAuditLogin_ProviderWithoutLockoutIsStillBounded(t *testing.T) {
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, &countingAdminAuth{})
	defer cleanup()
	panel.audit = newAuditStore(100)
	panel.audit.add(AuditEntry{Action: "flag.set", ModelName: "feature_flag", RecordID: "beta"})

	srv := httptest.NewServer(panel.Handler())
	defer srv.Close()

	for i := 0; i < 3*loginFailureLimit; i++ {
		resp, err := http.Post(srv.URL+"/login", "application/x-www-form-urlencoded",
			strings.NewReader(url.Values{"username": {"eve"}, "password": {"x"}}.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, resp.StatusCode)
		}
	}
	if n := panel.audit.count(auditQueryOpts{Action: "login.failed"}); n != loginFailureLimit {
		t.Fatalf("login.failed entries = %d, want %d", n, loginFailureLimit)
	}
	if n := panel.audit.count(auditQueryOpts{Action: "flag.set"}); n != 1 {
		t.Fatalf("the entry recorded before the attempts was evicted (count %d)", n)
	}
}

// TestLoginLimiter_FailReportsTheWindowCount pins the contract the login
// audit budget gates on: fail returns the count of the current window, a
// reset starts it over, and an untrackable key reports 0 (fail-open).
func TestLoginLimiter_FailReportsTheWindowCount(t *testing.T) {
	l := newLoginLimiter()
	for i := 1; i <= 3; i++ {
		if n := l.fail("k"); n != i {
			t.Fatalf("fail #%d returned %d", i, n)
		}
	}
	l.reset("k")
	if n := l.fail("k"); n != 1 {
		t.Fatalf("fail after reset returned %d, want 1", n)
	}
	if n := l.fail(""); n != 0 {
		t.Fatalf("fail with an empty key returned %d, want 0", n)
	}
	for i := 0; l.fail(fmt.Sprintf("fill-%d", i)) != 0; i++ {
		if i > loginLimiterCap {
			t.Fatalf("limiter tracked more than loginLimiterCap keys")
		}
	}
	if n := l.fail("k"); n != 2 {
		t.Fatalf("a tracked key keeps counting at capacity: got %d, want 2", n)
	}
	var nilLimiter *loginLimiter
	if n := nilLimiter.fail("k"); n != 0 {
		t.Fatalf("nil limiter returned %d, want 0", n)
	}
}

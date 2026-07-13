package admin

// Tests for the v1.2.1 audit backlog items on the in-process panel:
//
//   - OR-SEC-P1-1: authenticated /api writes ARE audited (the middleware
//     used to hang only off the GET-only SPA branch, so the production
//     posture never audited Data Studio writes).
//   - OR-SEC-P1-2: audit OldValue is redacted.
//   - OR-SEC-P2-1: login brute force is rate limited.
//   - OR-SEC-P2-2: form-submittable content types are rejected on writes.
//   - OR-SEC-P1-4: security headers on panel responses.
//   - OR-UX-P0-2: DELETE /api/sessions/{token} exists and destroys the
//     session (the SPA button used to 404 on every click).

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	_ "modernc.org/sqlite"

	"github.com/jcsvwinston/orbit/datasource"
)

func adminTestServer(t *testing.T) (*Panel, *httptest.Server) {
	t.Helper()
	authProvider := &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin"}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, authProvider)
	t.Cleanup(cleanup)
	// The test setup does not enable auditing; give the panel a store so
	// the middleware has somewhere to record.
	panel.audit = newAuditStore(100)
	srv := httptest.NewServer(panel.Handler())
	t.Cleanup(srv.Close)
	return panel, srv
}

func TestAuditMiddleware_RecordsAuthenticatedAPIWrites(t *testing.T) {
	panel, srv := adminTestServer(t)

	body, _ := json.Marshal(map[string]any{"email": "a@example.com", "name": "Alpha", "active": true})
	resp, err := http.Post(srv.URL+"/api/models/AdminUser", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("create status = %d", resp.StatusCode)
	}

	entries := panel.audit.list(auditQueryOpts{})
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1 (authenticated API writes must be audited)", len(entries))
	}
	e := entries[0]
	if e.Action != "create" || e.ModelName != "AdminUser" {
		t.Errorf("entry = %+v, want create AdminUser", e)
	}
	if e.Username != "admin" {
		t.Errorf("entry username = %q, want the authenticated operator", e.Username)
	}
}

func TestCSRFContentType_RejectsFormWrites(t *testing.T) {
	_, srv := adminTestServer(t)

	// A cross-site form's content type is rejected...
	form := url.Values{"email": {"a@example.com"}}
	resp, err := http.Post(srv.URL+"/api/models/AdminUser",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form POST status = %d, want 415", resp.StatusCode)
	}

	// ...and multipart is rejected outside the import route.
	resp, err = http.Post(srv.URL+"/api/models/AdminUser",
		"multipart/form-data; boundary=x", strings.NewReader("--x--"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("multipart POST status = %d, want 415", resp.StatusCode)
	}

	// Reads are untouched.
	resp, err = http.Get(srv.URL + "/api/models/AdminUser")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET status = %d, want 200", resp.StatusCode)
	}
}

func TestSecurityHeaders_OnPanelResponses(t *testing.T) {
	_, srv := adminTestServer(t)

	resp, err := http.Get(srv.URL + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
}

func TestRedactAuditValues_MasksExcludedAndCredentialFields(t *testing.T) {
	mi := datasource.ModelInfo{Fields: []datasource.FieldInfo{
		{Name: "Email", Column: "email"},
		{Name: "Notes", Column: "notes", IsExcluded: true},
	}}
	got := redactAuditValues(mi, map[string]any{
		"email":         "a@example.com",
		"notes":         "private",
		"password_hash": "$2a$12$...",
		"api_token":     "tok_123",
	})
	if got["email"] != "a@example.com" {
		t.Errorf("email was redacted: %v", got["email"])
	}
	for _, k := range []string{"notes", "password_hash", "api_token"} {
		if got[k] != redactedPlaceholder {
			t.Errorf("%s = %v, want %q", k, got[k], redactedPlaceholder)
		}
	}
}

func TestLoginRateLimit_BlocksAfterRepeatedFailures(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec(`CREATE TABLE nucleus_admin_users (
		id TEXT PRIMARY KEY, username TEXT, email TEXT,
		password_hash TEXT, is_superuser INTEGER)`); err != nil {
		t.Fatal(err)
	}

	a := NewDatabaseAdminAuth(sqlDB, nil, "/admin")
	h := a.LoginHandler()

	post := func(user string) int {
		form := url.Values{"username": {user}, "password": {"nope"}}
		req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	var saw429 bool
	for i := 0; i < loginFailureLimit+2; i++ {
		code := post("ghost")
		if code == http.StatusTooManyRequests {
			saw429 = true
			break
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d", i, code)
		}
	}
	if !saw429 {
		t.Fatal("repeated failed logins never hit 429")
	}
}

func TestTerminateSession_DestroysAndAudits(t *testing.T) {
	panel, srv := adminTestServer(t)

	sm := auth.NewSessionManager(auth.SessionConfig{})
	panel.config.Session = sm

	const token = "test-session-token-1234567890"
	deadline := time.Now().Add(time.Hour)
	if err := sm.SCS().Store.Commit(token, []byte("payload"), deadline); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	doDelete := func(tok string) *http.Response {
		req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/sessions/"+tok, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := doDelete(token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terminate status = %d", resp.StatusCode)
	}
	if _, found, _ := sm.SCS().Store.Find(token); found {
		t.Fatal("session still exists after terminate")
	}

	// A second delete 404s (honest: nothing was terminated).
	resp2 := doDelete(token)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("second terminate status = %d, want 404", resp2.StatusCode)
	}

	// The action is audited with the SHORT token only.
	entries := panel.audit.list(auditQueryOpts{Action: "session.terminate"})
	if len(entries) != 1 {
		t.Fatalf("audit entries for session.terminate = %d, want 1", len(entries))
	}
	if entries[0].RecordID == token || !strings.Contains(entries[0].RecordID, "...") {
		t.Errorf("audit RecordID = %q — must be the shortened token, never the credential", entries[0].RecordID)
	}
}

var _ = fmt.Sprintf // keep fmt for debugging convenience

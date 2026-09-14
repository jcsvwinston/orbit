package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// The session viewer after A6 S6: a row says whose session it is and from
// what device, marks the caller's own, and every session of one account can
// be ended at once without ending the one doing it.

func TestDescribeDevice(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"Go-http-client/1.1":    "Go-http-client",
		"curl/8.7.1":            "curl",
		"PostmanRuntime/7.39.0": "PostmanRuntime",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36":                         "Chrome on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36 Edg/128.0.0.0":           "Edge on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15":                      "Safari on macOS",
		"Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0":                                                                  "Firefox on Linux",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1": "Safari on iOS",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Mobile Safari/537.36":                   "Chrome on Android",
		"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36":                          "Chrome on ChromeOS",
	}
	for ua, want := range cases {
		if got := describeDevice(ua); got != want {
			t.Errorf("describeDevice(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestSanitizeUserAgent_StripsControlCharactersAndCaps(t *testing.T) {
	if got := sanitizeUserAgent(" Mozilla/5.0\r\n (X11)\x00 "); got != "Mozilla/5.0 (X11)" {
		t.Errorf("control characters survived: %q", got)
	}
	long := strings.Repeat("a", maxStoredUserAgent+50)
	if got := sanitizeUserAgent(long); len(got) != maxStoredUserAgent {
		t.Errorf("len = %d, want the cap %d", len(got), maxStoredUserAgent)
	}
}

func TestDetectSessionUser_PrefersThePanelsOwnOperator(t *testing.T) {
	values := map[string]interface{}{
		adminSessionUserIDKey:   "op-7",
		adminSessionUsernameKey: "ana",
		adminSessionEmailKey:    "ana@example.com",
		"user_id":               "42", // an application key must not shadow the operator
	}
	if got := detectSessionUser(values); got != "ana" {
		t.Errorf("user = %q, want the operator's username", got)
	}
	// An application session with no operator keys still names its user.
	if got := detectSessionUser(map[string]interface{}{"user_id": "42"}); got != "42" {
		t.Errorf("user = %q, want the application's key", got)
	}
}

// sessionViewerServer wires the panel UNDER the framework's session
// middleware, the way an application mounts it, so a request carries a real
// session the panel can recognise as its own.
func sessionViewerServer(t *testing.T) (*Panel, *auth.SessionManager, *httptest.Server) {
	t.Helper()
	authProvider := &testAdminAuth{user: &auth.User{ID: "1", Username: "admin", Role: "admin", IsSuperuser: true}}
	panel, cleanup := setupPanelForTestWithAuth(t, db.EngineSQL, authProvider)
	t.Cleanup(cleanup)
	panel.audit = newAuditStore(100)

	sm := auth.NewSessionManager(auth.SessionConfig{Lifetime: 2 * time.Hour})
	panel.config.Session = sm
	panel.config.SessionStore = "memory"

	root := router.NewMux()
	root.Use(sm.Middleware())
	root.Mount("/admin", panel.Handler())
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)
	return panel, sm, srv
}

func seedSession(t *testing.T, sm *auth.SessionManager, token string, values map[string]interface{}) {
	t.Helper()
	deadline := time.Now().Add(time.Hour)
	payload, err := sm.SCS().Codec.Encode(deadline, values)
	if err != nil {
		t.Fatalf("encode session %q: %v", token, err)
	}
	if err := sm.SCS().Store.Commit(token, payload, deadline); err != nil {
		t.Fatalf("seed session %q: %v", token, err)
	}
}

func sessionExists(t *testing.T, sm *auth.SessionManager, token string) bool {
	t.Helper()
	_, found, err := sm.SCS().Store.Find(token)
	if err != nil {
		t.Fatalf("find %q: %v", token, err)
	}
	return found
}

// requestWithSession issues one request that presents the given session
// token as its cookie — the request of an operator holding that session.
func requestWithSession(t *testing.T, sm *auth.SessionManager, method, url, token string, body string) (int, map[string]any, string) {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0")
	req.AddCookie(&http.Cookie{Name: sm.SCS().Cookie.Name, Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	raw := new(strings.Builder)
	dec := json.NewDecoder(io.TeeReader(resp.Body, raw))
	_ = dec.Decode(&decoded)
	return resp.StatusCode, decoded, raw.String()
}

func TestListSessions_RowNamesItsOperatorDeviceAndTheCallersOwn(t *testing.T) {
	_, sm, srv := sessionViewerServer(t)
	const mine = "ana-laptop-token-1234567890"
	seedSession(t, sm, mine, map[string]interface{}{adminSessionUsernameKey: "ana"})
	seedSession(t, sm, "ana-phone-token-1234567890", map[string]interface{}{
		adminSessionUsernameKey:      "ana",
		auth.SessionMetaUserAgentKey: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
	})
	seedSession(t, sm, "bob-desk-token-1234567890", map[string]interface{}{adminSessionUsernameKey: "bob"})

	// The panel records the device of a request when that request goes
	// through it, and the store sees it once the response is committed: the
	// first request a session makes is the one that stamps it, the second
	// is the one that lists it.
	if status, _, raw := requestWithSession(t, sm, http.MethodGet, srv.URL+"/admin/api/health", mine, ""); status != http.StatusOK {
		t.Fatalf("warm-up status = %d body=%s", status, raw)
	}
	status, payload, raw := requestWithSession(t, sm, http.MethodGet, srv.URL+"/admin/api/sessions", mine, "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d body=%s", status, raw)
	}
	for _, token := range []string{mine, "ana-phone-token-1234567890", "bob-desk-token-1234567890"} {
		if strings.Contains(raw, token) {
			t.Fatalf("a session token leaked into the list: %s", raw)
		}
	}
	rows, _ := payload["sessions"].([]any)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %s", len(rows), raw)
	}
	byHandle := map[string]map[string]any{}
	for _, row := range rows {
		entry := row.(map[string]any)
		byHandle[entry["id"].(string)] = entry
	}

	own := byHandle[sessionHandle(mine)]
	if own == nil {
		t.Fatalf("the caller's own session is not listed: %s", raw)
	}
	if own["user"] != "ana" {
		t.Errorf("own row user = %v, want ana", own["user"])
	}
	if own["current"] != true {
		t.Errorf("own row is not marked current: %v", own)
	}
	// The panel recorded the device of the request that listed — the agent
	// this request sent — under the framework's key.
	if own["device"] != "Firefox on Linux" {
		t.Errorf("own row device = %v, want Firefox on Linux (user_agent %v)", own["device"], own["user_agent"])
	}

	phone := byHandle[sessionHandle("ana-phone-token-1234567890")]
	if phone["user"] != "ana" || phone["device"] != "Safari on iOS" {
		t.Errorf("phone row = %v, want ana on Safari on iOS", phone)
	}
	if _, marked := phone["current"]; marked {
		t.Errorf("a session other than the caller's is marked current: %v", phone)
	}
	if bob := byHandle[sessionHandle("bob-desk-token-1234567890")]; bob["user"] != "bob" {
		t.Errorf("bob's row user = %v", bob["user"])
	}
}

func TestRevokeUserSessions_EndsEveryOtherSessionOfTheUserAndAudits(t *testing.T) {
	panel, sm, srv := sessionViewerServer(t)
	const mine = "ana-laptop-token-1234567890"
	seedSession(t, sm, mine, map[string]interface{}{adminSessionUsernameKey: "ana"})
	seedSession(t, sm, "ana-phone-token-1234567890", map[string]interface{}{adminSessionUsernameKey: "ana"})
	seedSession(t, sm, "ana-tablet-token-1234567890", map[string]interface{}{adminSessionUsernameKey: "ana"})
	seedSession(t, sm, "bob-desk-token-1234567890", map[string]interface{}{adminSessionUsernameKey: "bob"})

	status, payload, raw := requestWithSession(t, sm, http.MethodPost, srv.URL+"/admin/api/sessions/revoke-all", mine, `{"user":"ana"}`)
	if status != http.StatusOK {
		t.Fatalf("revoke-all status = %d body=%s", status, raw)
	}
	if fmt.Sprint(payload["revoked"]) != "2" || payload["kept_current"] != true {
		t.Fatalf("revoke-all answered %s, want revoked 2 and kept_current true", raw)
	}
	if strings.Contains(raw, "token-1234567890") {
		t.Fatalf("a session token leaked into the revoke-all answer: %s", raw)
	}

	// The two other devices are gone, the caller's own stays, and bob's
	// was never a match.
	if sessionExists(t, sm, "ana-phone-token-1234567890") || sessionExists(t, sm, "ana-tablet-token-1234567890") {
		t.Error("a session of the revoked user survived")
	}
	if !sessionExists(t, sm, mine) {
		t.Error("the request revoked the session it was made with")
	}
	if !sessionExists(t, sm, "bob-desk-token-1234567890") {
		t.Error("another user's session was revoked")
	}

	entries := panel.audit.list(auditQueryOpts{Action: "session.revoke_all"})
	if len(entries) != 1 {
		t.Fatalf("audit entries for session.revoke_all = %d, want 1", len(entries))
	}
	if entries[0].RecordID != "ana" {
		t.Errorf("audit record_id = %q, want the user", entries[0].RecordID)
	}
	if fmt.Sprint(entries[0].NewValue["revoked"]) != "2" || entries[0].NewValue["kept_current"] != true {
		t.Errorf("audit new_value = %v, want revoked 2 kept_current true", entries[0].NewValue)
	}
}

func TestRevokeUserSessions_RefusesAnEmptyUser(t *testing.T) {
	_, sm, srv := sessionViewerServer(t)
	seedSession(t, sm, "ana-laptop-token-1234567890", map[string]interface{}{adminSessionUsernameKey: "ana"})

	for _, body := range []string{`{}`, `{"user":"  "}`, `not json`} {
		status, _, raw := requestWithSession(t, sm, http.MethodPost, srv.URL+"/admin/api/sessions/revoke-all", "ana-laptop-token-1234567890", body)
		if status != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400 (body=%s)", body, status, raw)
		}
	}
	if !sessionExists(t, sm, "ana-laptop-token-1234567890") {
		t.Error("a refused request revoked something")
	}
}

// A user with no other sessions is an honest zero, not an error: the outcome
// asked for — no other session of theirs is open — already holds.
func TestRevokeUserSessions_NothingToRevokeIsZero(t *testing.T) {
	_, sm, srv := sessionViewerServer(t)
	seedSession(t, sm, "ana-laptop-token-1234567890", map[string]interface{}{adminSessionUsernameKey: "ana"})

	status, payload, raw := requestWithSession(t, sm, http.MethodPost, srv.URL+"/admin/api/sessions/revoke-all", "ana-laptop-token-1234567890", `{"user":"nobody"}`)
	if status != http.StatusOK || fmt.Sprint(payload["revoked"]) != "0" || payload["kept_current"] != false {
		t.Fatalf("status %d body=%s, want 200 with revoked 0", status, raw)
	}
}

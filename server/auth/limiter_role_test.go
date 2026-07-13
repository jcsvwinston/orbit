package auth

// Tests for the auth additions of the v1.2.1 audit backlog:
//
//   - OR-SEC-P1-3: the trusted proxy's role header ("viewer") and the
//     ForceReadOnly knob mark identities read-only.
//   - OR-SEC-P2-1: presented-and-wrong credentials are rate limited per
//     IP; credential-less requests never count.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// (captureIdentity and requestFrom live in ui_proxy_secret_test.go.)

func TestUIMiddleware_RoleHeaderViewer_IsReadOnly(t *testing.T) {
	var got Identity
	var called bool
	h := UIMiddleware(UIConfig{})(captureIdentity(&got, &called))

	// Default trusted CIDR is loopback; default role header X-Auth-Role.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestFrom("127.0.0.1:9999", map[string]string{
		"X-Auth-User": "carol",
		"X-Auth-Role": "Viewer",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !got.ReadOnly {
		t.Error("viewer role did not mark the identity read-only")
	}

	// No role header → read-write (existing deployments unchanged).
	w = httptest.NewRecorder()
	h.ServeHTTP(w, requestFrom("127.0.0.1:9999", map[string]string{"X-Auth-User": "carol"}))
	if got.ReadOnly {
		t.Error("absent role header marked the identity read-only")
	}
}

func TestUIMiddleware_ForceReadOnly_AppliesToBearer(t *testing.T) {
	var got Identity
	var called bool
	h := UIMiddleware(UIConfig{BearerToken: "tok", ForceReadOnly: true})(captureIdentity(&got, &called))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestFrom("10.1.1.1:9999", map[string]string{"Authorization": "Bearer tok"}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !got.ReadOnly {
		t.Error("ForceReadOnly did not mark the bearer identity read-only")
	}
}

func TestUIMiddleware_BadBearer_RateLimited(t *testing.T) {
	var got Identity
	var called bool
	h := UIMiddleware(UIConfig{BearerToken: "tok"})(captureIdentity(&got, &called))

	// Wrong bearers from one IP: 401 until the window fills, then 429.
	var saw429 bool
	for i := 0; i < failureLimit+2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestFrom("10.0.0.7:1234", map[string]string{"Authorization": "Bearer nope"}))
		if w.Code == http.StatusTooManyRequests {
			saw429 = true
			break
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d", i, w.Code)
		}
	}
	if !saw429 {
		t.Fatal("brute-force bearer attempts never hit 429")
	}

	// A valid bearer from ANOTHER IP still works (per-IP windows).
	w := httptest.NewRecorder()
	h.ServeHTTP(w, requestFrom("10.0.0.8:1234", map[string]string{"Authorization": "Bearer tok"}))
	if w.Code != http.StatusOK {
		t.Fatalf("valid bearer from clean IP: status = %d", w.Code)
	}
}

func TestUIMiddleware_CredentiallessRequests_NeverLockOut(t *testing.T) {
	var got Identity
	var called bool
	h := UIMiddleware(UIConfig{BearerToken: "tok"})(captureIdentity(&got, &called))

	// A browser without credentials hammering the SPA gets 401s, never
	// 429 — it is not presenting candidate credentials.
	for i := 0; i < failureLimit*2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestFrom("10.0.0.9:1234", nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, w.Code)
		}
	}
}

func TestAgentMiddleware_BadToken_RateLimited(t *testing.T) {
	h := AgentMiddleware("tok")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var saw429 bool
	for i := 0; i < failureLimit+2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestFrom("10.0.0.10:1234", map[string]string{"Authorization": "Bearer nope"}))
		if w.Code == http.StatusTooManyRequests {
			saw429 = true
			break
		}
	}
	if !saw429 {
		t.Fatal("brute-force agent tokens never hit 429")
	}
}

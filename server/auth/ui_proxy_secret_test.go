package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureIdentity is a terminal handler that records the identity the
// middleware injected (if it let the request through).
func captureIdentity(seen *Identity, called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		*seen = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func requestFrom(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/nucleus.admin.v1.ManageService/ListAudit", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestUIMiddleware_ProxySecret(t *testing.T) {
	const secret = "proxy-shared-secret"
	const trusted = "127.0.0.1:40000" // inside the default trusted CIDR

	t.Run("correct_secret_honours_identity", func(t *testing.T) {
		var id Identity
		var called bool
		h := UIMiddleware(UIConfig{ProxySecret: secret})(captureIdentity(&id, &called))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, requestFrom(trusted, map[string]string{
			"X-Auth-User":     "alice",
			ProxySecretHeader: secret,
		}))
		if !called || rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with identity, got called=%v code=%d", called, rec.Code)
		}
		if id.Subject != "alice" {
			t.Errorf("subject = %q, want alice", id.Subject)
		}
	})

	t.Run("missing_secret_is_rejected", func(t *testing.T) {
		var id Identity
		var called bool
		h := UIMiddleware(UIConfig{ProxySecret: secret})(captureIdentity(&id, &called))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, requestFrom(trusted, map[string]string{
			"X-Auth-User": "eve", // forged, no secret
		}))
		if called || rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 without secret, got called=%v code=%d", called, rec.Code)
		}
	})

	t.Run("wrong_secret_falls_through_to_bearer", func(t *testing.T) {
		var id Identity
		var called bool
		h := UIMiddleware(UIConfig{ProxySecret: secret, BearerToken: "btok"})(captureIdentity(&id, &called))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, requestFrom(trusted, map[string]string{
			"X-Auth-User":     "eve",
			ProxySecretHeader: "nope",
			"Authorization":   "Bearer btok",
		}))
		if !called || rec.Code != http.StatusOK {
			t.Fatalf("expected 200 via bearer, got called=%v code=%d", called, rec.Code)
		}
		if id.Subject != "ui-bearer" {
			t.Errorf("subject = %q, want ui-bearer (header identity must not win with a bad secret)", id.Subject)
		}
	})

	t.Run("no_secret_configured_keeps_cidr_only_behaviour", func(t *testing.T) {
		var id Identity
		var called bool
		h := UIMiddleware(UIConfig{})(captureIdentity(&id, &called))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, requestFrom(trusted, map[string]string{
			"X-Auth-User": "alice",
		}))
		if !called || id.Subject != "alice" {
			t.Fatalf("cidr-only path should authenticate alice, got called=%v subject=%q", called, id.Subject)
		}
	})

	t.Run("untrusted_cidr_never_trusts_header", func(t *testing.T) {
		var id Identity
		var called bool
		h := UIMiddleware(UIConfig{ProxySecret: secret})(captureIdentity(&id, &called))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, requestFrom("10.9.9.9:5000", map[string]string{
			"X-Auth-User":     "eve",
			ProxySecretHeader: secret, // secret known but source not trusted
		}))
		if called || rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 from untrusted CIDR, got called=%v code=%d", called, rec.Code)
		}
	})
}

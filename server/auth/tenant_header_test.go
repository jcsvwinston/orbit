package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The tenant the operator is scoped to rides the trusted-proxy path with the
// identity, and nowhere else: a bearer caller has no tenant, and a header
// from outside the trusted range is not read.
func TestUIMiddleware_TenantHeader(t *testing.T) {
	var seen Identity
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	mw := UIMiddleware(UIConfig{BearerToken: "secret"})(next)

	t.Run("trusted_proxy_carries_the_tenant", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Auth-User", "alice")
		req.Header.Set("X-Auth-Tenant", " acme ")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d", rec.Code)
		}
		if seen.Subject != "alice" || seen.Tenant != "acme" {
			t.Fatalf("identity = %+v, want alice scoped to acme (trimmed)", seen)
		}
	})

	t.Run("custom_header_name", func(t *testing.T) {
		custom := UIMiddleware(UIConfig{TenantHeader: "X-Org"})(next)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Auth-User", "alice")
		req.Header.Set("X-Org", "globex")
		req.Header.Set("X-Auth-Tenant", "ignored")
		custom.ServeHTTP(httptest.NewRecorder(), req)
		if seen.Tenant != "globex" {
			t.Fatalf("tenant = %q, want the configured header's value", seen.Tenant)
		}
	})

	t.Run("bearer_caller_has_no_tenant", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:1234" // outside the trusted range
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("X-Auth-Tenant", "acme")
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d", rec.Code)
		}
		if seen.Subject != "ui-bearer" || seen.Tenant != "" {
			t.Fatalf("identity = %+v: a tenant header outside the trusted-proxy path must not be read", seen)
		}
	})
}

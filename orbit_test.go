package orbit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Module is a nucleus ModuleSpec with orbit's identity and the default prefix.
func TestModule_Identity(t *testing.T) {
	spec := Module(Config{})
	if spec.Name() != "orbit" {
		t.Errorf("Name = %q, want orbit", spec.Name())
	}
	if spec.Prefix() != DefaultPrefix {
		t.Errorf("default Prefix = %q, want %q", spec.Prefix(), DefaultPrefix)
	}
}

// A custom prefix is honoured.
func TestModule_CustomPrefix(t *testing.T) {
	if got := Module(Config{Prefix: "/backoffice"}).Prefix(); got != "/backoffice" {
		t.Errorf("Prefix = %q, want /backoffice", got)
	}
}

// The panel's tenant comes from nucleus' request scope. Without one — no
// multi-tenant resolution ran for the request — the resolver says so, and
// the panel falls back to the configured default rather than to a value
// nothing set.
func TestResolvedTenant_WithoutRequestScope(t *testing.T) {
	if tenant, ok := resolvedTenant(nil); ok || tenant != "" {
		t.Errorf("nil request: (%q, %v), want (\"\", false)", tenant, ok)
	}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/models", nil)
	if tenant, ok := resolvedTenant(r); ok || tenant != "" {
		t.Errorf("unscoped request: (%q, %v), want (\"\", false)", tenant, ok)
	}
}

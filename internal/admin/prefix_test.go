package admin

import (
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

func TestNormalizePrefix(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		if result := NormalizePrefix(""); result != DefaultPrefix {
			t.Errorf("expected %s, got %s", DefaultPrefix, result)
		}
	})

	t.Run("default prefix", func(t *testing.T) {
		if result := NormalizePrefix(DefaultPrefix); result != DefaultPrefix {
			t.Errorf("expected %s, got %s", DefaultPrefix, result)
		}
	})

	t.Run("without leading slash", func(t *testing.T) {
		if result := NormalizePrefix("admin"); result != "/admin" {
			t.Errorf("expected /admin, got %s", result)
		}
	})

	t.Run("with trailing slash", func(t *testing.T) {
		if result := NormalizePrefix("/admin/"); result != "/admin" {
			t.Errorf("expected /admin, got %s", result)
		}
	})

	t.Run("with multiple trailing slashes", func(t *testing.T) {
		if result := NormalizePrefix("/admin///"); result != "/admin" {
			t.Errorf("expected /admin, got %s", result)
		}
	})

	t.Run("with whitespace", func(t *testing.T) {
		if result := NormalizePrefix("  admin  "); result != "/admin" {
			t.Errorf("expected /admin, got %s", result)
		}
	})

	t.Run("custom path", func(t *testing.T) {
		if result := NormalizePrefix("/dashboard"); result != "/dashboard" {
			t.Errorf("expected /dashboard, got %s", result)
		}
	})

	t.Run("slash only", func(t *testing.T) {
		if result := NormalizePrefix("/"); result != DefaultPrefix {
			t.Errorf("expected %s, got %s", DefaultPrefix, result)
		}
	})
}

func TestAdminLoginURL(t *testing.T) {
	config := PanelConfig{Prefix: "/admin"}
	panel := &Panel{config: config}

	t.Run("nil request", func(t *testing.T) {
		url := panel.adminLoginURL(nil)
		expected := "/admin/login"
		if url != expected {
			t.Errorf("expected %s, got %s", expected, url)
		}
	})

	t.Run("nil URL", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.URL = nil
		url := panel.adminLoginURL(req)
		expected := "/admin/login"
		if url != expected {
			t.Errorf("expected %s, got %s", expected, url)
		}
	})

	t.Run("with path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users", nil)
		url := panel.adminLoginURL(req)
		// The function adds the prefix to the path
		if !contains(url, "/admin/login") {
			t.Errorf("expected /admin/login in URL, got %s", url)
		}
	})

	t.Run("with query params", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users?page=1", nil)
		url := panel.adminLoginURL(req)
		if !contains(url, "/admin/login") {
			t.Errorf("expected /admin/login in URL, got %s", url)
		}
		// Query params are URL-encoded in the next parameter
		if !contains(url, "page%3D1") {
			t.Errorf("expected encoded page param in URL, got %s", url)
		}
	})

	t.Run("custom prefix", func(t *testing.T) {
		config := PanelConfig{Prefix: "/dashboard"}
		panel := &Panel{config: config}
		req := httptest.NewRequest("GET", "/users", nil)
		url := panel.adminLoginURL(req)
		if !contains(url, "/dashboard/login") {
			t.Errorf("expected /dashboard/login in URL, got %s", url)
		}
	})
}

func TestHandleLogout(t *testing.T) {
	t.Run("nil panel", func(t *testing.T) {
		c := &router.Context{Request: httptest.NewRequest("POST", "/logout", nil)}
		err := (*Panel)(nil).handleLogout(c)
		if err == nil {
			t.Error("expected error for nil panel")
		}
	})

	t.Run("nil session", func(t *testing.T) {
		config := PanelConfig{Session: nil}
		panel := &Panel{config: config}
		c := &router.Context{Request: httptest.NewRequest("POST", "/logout", nil)}
		err := panel.handleLogout(c)
		if err == nil {
			t.Error("expected error for nil session")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 1; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

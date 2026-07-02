package admin

import "testing"

func TestNormalizeStorageBrowsePath(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != adminStorageBrowseRoot {
			t.Errorf("expected %s, got %s", adminStorageBrowseRoot, result)
		}
	})

	t.Run("slash", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("/")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != adminStorageBrowseRoot {
			t.Errorf("expected %s, got %s", adminStorageBrowseRoot, result)
		}
	})

	t.Run("root path", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("uploads")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != adminStorageBrowseRoot {
			t.Errorf("expected %s, got %s", adminStorageBrowseRoot, result)
		}
	})

	t.Run("valid subdirectory", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("uploads/images")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "uploads/images" {
			t.Errorf("expected uploads/images, got %s", result)
		}
	})

	t.Run("with leading slash", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("/uploads/images")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "uploads/images" {
			t.Errorf("expected uploads/images, got %s", result)
		}
	})

	t.Run("with trailing slash", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("uploads/images/")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "uploads/images" {
			t.Errorf("expected uploads/images, got %s", result)
		}
	})

	t.Run("with backslashes", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("uploads\\images")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "uploads/images" {
			t.Errorf("expected uploads/images, got %s", result)
		}
	})

	t.Run("path outside root - denied", func(t *testing.T) {
		_, err := normalizeStorageBrowsePath("other")
		if err == nil {
			t.Error("expected error for path outside root")
		}
	})

	t.Run("path outside root with leading slash - denied", func(t *testing.T) {
		_, err := normalizeStorageBrowsePath("/other")
		if err == nil {
			t.Error("expected error for path outside root")
		}
	})

	t.Run("path traversal attempt - denied", func(t *testing.T) {
		_, err := normalizeStorageBrowsePath("../etc")
		if err == nil {
			t.Error("expected error for path traversal")
		}
	})

	t.Run("with whitespace", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath("  uploads/images  ")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "uploads/images" {
			t.Errorf("expected uploads/images, got %s", result)
		}
	})

	t.Run("dot after clean", func(t *testing.T) {
		result, err := normalizeStorageBrowsePath(".")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != adminStorageBrowseRoot {
			t.Errorf("expected %s, got %s", adminStorageBrowseRoot, result)
		}
	})
}

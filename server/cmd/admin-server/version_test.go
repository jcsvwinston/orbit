package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildVersionFallbacks pins the order buildVersion answers in. The
// interesting case is the empty stamp: a source build must say "devel" rather
// than print an empty version string after the program name.
func TestBuildVersionFallbacks(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v9.9.9"
	if got := buildVersion(); got != "v9.9.9" {
		t.Fatalf("stamped build: got %q, want %q", got, "v9.9.9")
	}

	version = "  v9.9.9\n"
	if got := buildVersion(); got != "v9.9.9" {
		t.Fatalf("stamped build with whitespace: got %q, want %q", got, "v9.9.9")
	}

	// No stamp and no module version in build info (which is what a `go test`
	// binary has): the honest answer, not an empty string.
	version = ""
	if got := buildVersion(); got == "" {
		t.Fatal("unstamped build returned an empty version")
	}
}

// TestVersionFlagUsesLinkerStamp is the regression test for the symbol name in
// the release build's ldflags. The Go linker names the main package `main`
// whatever its import path is, so
// `-X github.com/jcsvwinston/orbit/server/cmd/admin-server.version=v1.2.3`
// links happily and stamps nothing — every published binary would report
// "devel" and nothing in the toolchain would complain. The only way to know
// the flag in .goreleaser.yaml still works is to link with it and ask the
// binary.
func TestVersionFlagUsesLinkerStamp(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}

	binary := filepath.Join(t.TempDir(), "admin-server")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	// Keep this ldflags string identical in shape to the one in
	// .goreleaser.yaml; that file is what this test is guarding.
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v9.9.9", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("admin-server --version: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "nucleus-admin-server v9.9.9" {
		t.Fatalf("--version printed %q, want %q — the linker stamp in .goreleaser.yaml no longer reaches the binary", got, "nucleus-admin-server v9.9.9")
	}
}

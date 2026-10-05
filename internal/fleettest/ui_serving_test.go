// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package fleettest

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	orbitui "github.com/jcsvwinston/orbit/ui"
)

// The panel and the admin server serve the ui module's encoded files with
// two copies of one file (internal/admin/precompressed.go and
// server/precompressed.go): the two modules share no package, and the one
// they both require would have to be released before either could call
// into it (ADR-006). This module sees both trees, so it is where the two
// copies are held to the same text: everything from the import block down.
func TestPrecompressedServing_TwoCopiesStayOne(t *testing.T) {
	body := func(rel string) string {
		raw, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		src := string(raw)
		i := strings.Index(src, "\nimport (")
		if i < 0 {
			t.Fatalf("%s has no import block", rel)
		}
		return src[i:]
	}
	panel := body("internal/admin/precompressed.go")
	srv := body("server/precompressed.go")
	if panel != srv {
		pl, sl := strings.Split(panel, "\n"), strings.Split(srv, "\n")
		for i := 0; i < len(pl) && i < len(sl); i++ {
			if pl[i] != sl[i] {
				t.Fatalf("the two copies differ from the import block down, first at line %d of it:\n  internal/admin: %q\n  server:         %q", i, pl[i], sl[i])
			}
		}
		t.Fatalf("the two copies differ in length from the import block down (%d and %d lines)", len(pl), len(sl))
	}
}

// The admin server serves the fleet's entry and never the panel's, and its
// binary carries only the one it serves: the ui module embeds each entry in
// a variable of its own, and the linker keeps only the one the server
// reads. Built here, from this workspace, and read.
func TestAdminServerBinary_CarriesTheFleetEntryOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the admin server; skipped with -short")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	marker := func(dist fs.FS, which string) []byte {
		if dist == nil {
			t.Fatalf("the ui module embeds no %s entry", which)
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`src="\./(assets/[^"]+\.js)"`).FindSubmatch(index)
		if m == nil {
			t.Fatalf("the %s entry's index.html names no script", which)
		}
		return m[1]
	}
	panelScript, fleetScript := marker(orbitui.Panel(), "panel"), marker(orbitui.Fleet(), "fleet")

	out := filepath.Join(t.TempDir(), "admin-server")
	cmd := exec.Command(goBin, "build", "-o", out, "./cmd/admin-server")
	cmd.Dir = filepath.Join("..", "..", "server")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the admin server: %v\n%s", err, output)
	}
	bin, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("admin-server: %d bytes", len(bin))
	if !bytes.Contains(bin, fleetScript) {
		t.Fatalf("the admin server does not carry the fleet's entry (%s): the marker sees nothing", fleetScript)
	}
	if bytes.Contains(bin, panelScript) {
		t.Errorf("the admin server carries the panel's entry (%s), which it never serves", panelScript)
	}
}

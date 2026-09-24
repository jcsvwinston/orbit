// Package ui embeds the built frontend of Orbit: one project, two entries
// (ADR-015). dist/panel is the in-process panel, served by the root module
// under its prefix; dist/fleet is the fleet plane, served by the admin
// server at its UI root. Both binaries embed this one dist, so the two
// planes are built from the same sources, tokens, tests and budget.
//
// The dist is committed: a consumer that requires the module gets the
// built frontend as a normal Go dependency, with no asset deployment of
// its own. CI rebuilds it and fails when the committed copy is stale.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist is the whole embedded dist: panel/ and fleet/.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// Panel is the in-process panel's entry, or nil when the dist lacks it.
func Panel() fs.FS { return entry("dist/panel") }

// Fleet is the fleet plane's entry, or nil when the dist lacks it.
func Fleet() fs.FS { return entry("dist/fleet") }

func entry(dir string) fs.FS {
	sub, err := fs.Sub(distFS, dir)
	if err != nil {
		return nil
	}
	if info, err := fs.Stat(sub, "index.html"); err != nil || info.IsDir() {
		return nil
	}
	return sub
}
